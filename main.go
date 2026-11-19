package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const usage = `usage:
  minimaxon build <file.maxon|dir> [-o out.exe]   compile to a Windows x64 executable
  minimaxon run <file.maxon|dir>                  compile and run
  minimaxon check <file.maxon|dir>                report diagnostics only
  minimaxon test [dir]                            run the test corpus (default: tests/)

A directory is a project: each directory under it is a module. --jobs=N sets how many modules
compile at once (default: one fewer than the processor count). --timings prints each phase's time; --emit-ir writes the IR beside the executable, as <out>.ir.`

const (
	internalErrorExitCode = 3
	usageExitCode         = 2
	jobsFlagPrefix        = "--jobs="
	timingsFlag           = "--timings"
	emitIrFlag            = "--emit-ir"
	modulesDir            = "modules"
	sourceExtension       = ".maxon"
	executableExtension   = ".exe"
	testRunTimeout        = 10 * time.Second
	testWaitDelay         = time.Second
	expectErrorPrefix     = "// expect-error:"
	expectExitPrefix      = "// expect-exit:"
	expectStdoutPrefix    = "//>"
	errorLocationSplit    = " at "
	byteOrderMark         = "\xef\xbb\xbf"
)

var (
	jobs    = DefaultJobs()
	timings bool
	emitIR  bool
)

func compileWithFlags(path, output string) []*CompileError {
	return Compile(path, output, jobs, timings, emitIR)
}

func describeInternal(r any, stack []byte) string {
	return fmt.Sprintf("internal compiler error (a bug in minimaxon, not in the program): %v\n%s", r, stack)
}

func main() {
	os.Exit(runMain(os.Args[1:]))
}

func runMain(args []string) (code int) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		return usageExitCode
	}

	jobsFlag := ""
	for _, a := range args {
		if strings.HasPrefix(a, jobsFlagPrefix) {
			jobsFlag = a
			break
		}
	}
	if jobsFlag != "" {
		n, err := strconv.Atoi(jobsFlag[len(jobsFlagPrefix):])
		if err != nil || n < 1 {
			fmt.Fprintln(os.Stderr, "--jobs takes a positive number")
			return usageExitCode
		}
		jobs = n
		args = filterArgs(args, func(a string) bool { return a != jobsFlag })
	}

	for _, a := range args {
		timings = timings || a == timingsFlag
		emitIR = emitIR || a == emitIrFlag
	}
	args = filterArgs(args, func(a string) bool { return a != timingsFlag && a != emitIrFlag })
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		return usageExitCode
	}

	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(os.Stderr, describeInternal(r, debug.Stack()))
			code = internalErrorExitCode
		}
	}()
	return run(args)
}

func filterArgs(args []string, keep func(string) bool) []string {
	var out []string
	for _, a := range args {
		if keep(a) {
			out = append(out, a)
		}
	}
	return out
}

func run(args []string) int {
	switch {
	case args[0] == "build" && len(args) >= 2:
		var output string
		if len(args) >= 4 && args[2] == "-o" {
			output = args[3]
		} else {
			trimmed := strings.TrimRight(args[1], "/\\")
			output = strings.TrimSuffix(trimmed, filepath.Ext(trimmed)) + executableExtension
		}
		if report(compileWithFlags(args[1], output)) {
			return 0
		}
		return 1
	case args[0] == "check" && len(args) >= 2:
		if report(compileWithFlags(args[1], "")) {
			return 0
		}
		return 1
	case args[0] == "run" && len(args) >= 2:
		exe := tempExe(args[1])
		if !report(compileWithFlags(args[1], exe)) {
			return 1
		}
		cmd := exec.Command(exe)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			panic(Internal("cannot start %s: %v", exe, err))
		}
		_ = cmd.Wait()
		return exitCodeOf(cmd.ProcessState)
	case args[0] == "test":
		if len(args) >= 2 {
			return runTests(args[1])
		}
		exe, err := os.Executable()
		if err != nil {
			panic(Internal("cannot locate the executable: %v", err))
		}
		return runTests(filepath.Join(filepath.Dir(exe), "tests"))
	default:
		fmt.Fprintln(os.Stderr, usage)
		return usageExitCode
	}
}

func exitCodeOf(state *os.ProcessState) int {
	return int(int32(state.ExitCode()))
}

func tempExe(source string) string {
	dir := filepath.Join(os.TempDir(), "minimaxon")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(Internal("cannot create %s: %v", dir, err))
	}
	base := filepath.Base(strings.TrimRight(source, "/\\"))
	return filepath.Join(dir, strings.TrimSuffix(base, filepath.Ext(base))+executableExtension)
}

func report(errs []*CompileError) bool {
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, e)
	}
	return len(errs) == 0
}

type expectation struct {
	Error    string
	HasError bool
	Exit     int
	Stdout   string
}

func readLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(Internal("cannot read %s: %v", path, err))
	}
	text := strings.TrimPrefix(string(data), byteOrderMark)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.Split(text, "\n")
}

func readExpectation(program string) expectation {
	var expect expectation
	var stdout strings.Builder
	files := []string{program}
	if info, err := os.Stat(program); err == nil && info.IsDir() {
		matches, _ := filepath.Glob(filepath.Join(program, "*"+sourceExtension))
		sort.Strings(matches)
		files = matches
	}
	for _, file := range files {
		for _, raw := range readLines(file) {
			line := strings.TrimLeftFunc(raw, unicode.IsSpace)
			switch {
			case strings.HasPrefix(line, expectErrorPrefix):
				expect.Error = strings.TrimSpace(line[len(expectErrorPrefix):])
				expect.HasError = true
			case strings.HasPrefix(line, expectExitPrefix):
				n, err := strconv.Atoi(strings.TrimSpace(line[len(expectExitPrefix):]))
				if err != nil {
					panic(Internal("%s: malformed expect-exit: %v", file, err))
				}
				expect.Exit = n
			case strings.HasPrefix(line, expectStdoutPrefix):
				if len(line) > 3 && line[3] == ' ' {
					stdout.WriteString(line[4:])
				} else {
					stdout.WriteString(line[3:])
				}
				stdout.WriteByte('\n')
			}
		}
	}
	expect.Stdout = stdout.String()
	return expect
}

func runTests(dir string) int {
	dir, err := filepath.Abs(dir)
	if err != nil {
		panic(Internal("cannot resolve %s: %v", dir, err))
	}
	modulesRoot := filepath.Join(dir, modulesDir)
	var files []string
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, sourceExtension) && !strings.HasPrefix(path, modulesRoot+string(filepath.Separator)) {
			files = append(files, path)
		}
		return nil
	})
	if walkErr != nil {
		panic(Internal("cannot read %s: %v", dir, walkErr))
	}
	if entries, err := os.ReadDir(modulesRoot); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				files = append(files, filepath.Join(modulesRoot, e.Name()))
			}
		}
	}
	sort.Strings(files)

	var failures []string
	for _, file := range files {
		name, err := filepath.Rel(dir, file)
		if err != nil {
			panic(Internal("cannot relativize %s: %v", file, err))
		}
		failure, failed := runTest(file, readExpectation(file))
		status := "PASS"
		if failed {
			status = "FAIL"
			failures = append(failures, name+": "+failure)
		}
		fmt.Printf("%s  %s\n", status, name)
	}

	fmt.Println()
	for _, f := range failures {
		fmt.Println(f)
	}
	fmt.Printf("%d/%d passed\n", len(files)-len(failures), len(files))
	if len(failures) == 0 {
		return 0
	}
	return 1
}

func compileForTest(file, output string) (errs []*CompileError, internal string) {
	defer func() {
		if r := recover(); r != nil {
			internal = describeInternal(r, debug.Stack())
		}
	}()
	return compileWithFlags(file, output), ""
}

func joinErrors(errs []*CompileError) string {
	parts := make([]string, len(errs))
	for i, e := range errs {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "; ")
}

func runTest(file string, expect expectation) (string, bool) {
	exe := tempExe(file)
	output := exe
	if expect.HasError {
		output = ""
	}
	errs, internal := compileForTest(file, output)
	if internal != "" {
		return internal, true
	}
	if expect.HasError {
		code, at, hasAt := strings.Cut(expect.Error, errorLocationSplit)
		for _, e := range errs {
			if e.Code == code && (!hasAt || at == strconv.Itoa(e.Line)+":"+strconv.Itoa(e.Col)) {
				return "", false
			}
		}
		if len(errs) == 0 {
			return "expected " + expect.Error + ", but it compiled", true
		}
		return "expected " + expect.Error + ", got: " + joinErrors(errs), true
	}
	if len(errs) > 0 {
		return joinErrors(errs), true
	}

	ctx, cancel := context.WithTimeout(context.Background(), testRunTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = testWaitDelay
	waitErr := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timed out", true
	}
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) {
		return "cannot run " + exe + ": " + waitErr.Error(), true
	}
	actual := strings.ReplaceAll(stdout.String(), "\r\n", "\n")
	if code := exitCodeOf(cmd.ProcessState); code != expect.Exit {
		return fmt.Sprintf("exit %d, expected %d; stderr: %s", code, expect.Exit, strings.TrimSpace(stderr.String())), true
	}
	if actual != expect.Stdout {
		return "stdout differs\n--- expected\n" + expect.Stdout + "--- actual\n" + actual, true
	}
	return "", false
}
