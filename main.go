package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"minimaxon/internal/diag"
	"minimaxon/internal/driver"
	"minimaxon/internal/interp"

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
  minimaxon interp <file.maxon|dir>               run under the tracking interpreter (development only)

A directory is a project: each directory under it is a module. --jobs=N sets how many modules
compile at once (default: one fewer than the processor count). --timings prints each phase's time; --emit-ir writes the IR beside the executable, as <out>.ir.
--seed=N picks the schedule of behaviours that interp runs. --count-allocations builds executables that count their live
heap boxes and fail at exit with code 99 if any is left (development only).`

const (
	internalErrorExitCode = 3
	violationExitCode     = 4
	timeoutExitCode       = 5
	usageExitCode         = 2
	jobsFlagPrefix        = "--jobs="
	seedFlagPrefix        = "--seed="
	timingsFlag           = "--timings"
	emitIrFlag            = "--emit-ir"
	countAllocationsFlag  = "--count-allocations"
	modulesDir            = "modules"
	sourceExtension       = ".maxon"
	executableExtension   = ".exe"
	testRunTimeout        = 10 * time.Second
	testWaitDelay         = time.Second
	// scheduleSeeds is how many schedules of its behaviours the test runner runs a program under: seeds 0 up to this.
	scheduleSeeds      = 20
	expectErrorPrefix  = "// expect-error:"
	expectExitPrefix   = "// expect-exit:"
	expectStdoutPrefix = "//>"
	// expectMemoryPrefix caps the memory of the native run, in megabytes, and nativeOnlyDirective leaves out the run under the
	// interpreter: a program that is about what the executable does with memory is too long for the interpreter.
	expectMemoryPrefix  = "// memory-limit:"
	nativeOnlyDirective = "// native-only"
	errorLocationSplit  = " at "
	byteOrderMark       = "\xef\xbb\xbf"
)

var (
	jobs    = driver.DefaultJobs()
	timings bool
	emitIR  bool
	// countAllocations builds executables that count their live heap boxes (see the README, "Counting allocations").
	countAllocations bool
	seed             int64
)

func compileWithFlags(path, output string) []*diag.CompileError {
	return driver.Compile(path, output, driver.Options{Jobs: jobs, Timings: timings, EmitIr: emitIR, CountAllocations: countAllocations})
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
		if strings.HasPrefix(a, seedFlagPrefix) {
			n, err := strconv.ParseInt(a[len(seedFlagPrefix):], 10, 64)
			if err != nil {
				fmt.Fprintln(os.Stderr, "--seed takes a number")
				return usageExitCode
			}
			seed = n
			args = filterArgs(args, func(other string) bool { return other != a })
			break
		}
	}

	for _, a := range args {
		timings = timings || a == timingsFlag
		emitIR = emitIR || a == emitIrFlag
		countAllocations = countAllocations || a == countAllocationsFlag
	}
	args = filterArgs(args, func(a string) bool { return a != timingsFlag && a != emitIrFlag && a != countAllocationsFlag })
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
			panic(diag.Internal("cannot start %s: %v", exe, err))
		}
		_ = cmd.Wait()
		return exitCodeOf(cmd.ProcessState)
	case args[0] == "interp" && len(args) >= 2:
		return interpret(args[1])
	case args[0] == "test":
		if len(args) >= 2 {
			return runTests(args[1])
		}
		exe, err := os.Executable()
		if err != nil {
			panic(diag.Internal("cannot locate the executable: %v", err))
		}
		return runTests(filepath.Join(filepath.Dir(exe), "tests"))
	default:
		fmt.Fprintln(os.Stderr, usage)
		return usageExitCode
	}
}

// interpret is the development command: it runs a program under the tracking interpreter, which runs a map, a list, a
// pool, a frozen value, a cown and a behaviour, and reports a violation of the memory model. The program's output and
// exit code are its own, unless a violation or a timeout ends the run. A program that schedules behaviours runs under
// the schedule that --seed picks.
func interpret(path string) int {
	program, errs := driver.Prepare(path, jobs)
	if !report(errs) {
		return 1
	}
	result := interp.RunWith(program, interp.Options{StepLimit: interp.DefaultStepLimit, Seed: seed})
	os.Stdout.Write(result.Stdout)
	os.Stderr.Write(result.Stderr)
	for _, v := range result.Violations {
		fmt.Fprintln(os.Stderr, v)
	}
	switch {
	case len(result.Violations) > 0:
		return violationExitCode
	case result.TimedOut:
		fmt.Fprintln(os.Stderr, "timed out")
		return timeoutExitCode
	}
	return result.ExitCode
}

func exitCodeOf(state *os.ProcessState) int {
	return int(int32(state.ExitCode()))
}

func tempExe(source string) string {
	dir := filepath.Join(os.TempDir(), "minimaxon")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(diag.Internal("cannot create %s: %v", dir, err))
	}
	base := filepath.Base(strings.TrimRight(source, "/\\"))
	return filepath.Join(dir, strings.TrimSuffix(base, filepath.Ext(base))+executableExtension)
}

func report(errs []*diag.CompileError) bool {
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, e)
	}
	return len(errs) == 0
}

type expectation struct {
	Error         string
	HasError      bool
	Exit          int
	Stdout        string
	MemoryLimitMB int
	NativeOnly    bool
}

func readLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(diag.Internal("cannot read %s: %v", path, err))
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
					panic(diag.Internal("%s: malformed expect-exit: %v", file, err))
				}
				expect.Exit = n
			case strings.HasPrefix(line, expectMemoryPrefix):
				n, err := strconv.Atoi(strings.TrimSpace(line[len(expectMemoryPrefix):]))
				if err != nil || n < 1 {
					panic(diag.Internal("%s: malformed memory-limit: write a number of megabytes", file))
				}
				expect.MemoryLimitMB = n
			case line == nativeOnlyDirective:
				expect.NativeOnly = true
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
	// Every native run of the corpus must end with no live heap box, so the executables count them.
	countAllocations = true
	dir, err := filepath.Abs(dir)
	if err != nil {
		panic(diag.Internal("cannot resolve %s: %v", dir, err))
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
		panic(diag.Internal("cannot read %s: %v", dir, walkErr))
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
			panic(diag.Internal("cannot relativize %s: %v", file, err))
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

func compileForTest(file, output string) (errs []*diag.CompileError, internal string) {
	defer func() {
		if r := recover(); r != nil {
			internal = describeInternal(r, debug.Stack())
		}
	}()
	return compileWithFlags(file, output), ""
}

func joinErrors(errs []*diag.CompileError) string {
	parts := make([]string, len(errs))
	for i, e := range errs {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "; ")
}

func runTest(file string, expect expectation) (string, bool) {
	exe := tempExe(file)
	errs, internal := compileForTest(file, exe)
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
	interpreterOnly := onlyInterpreterErrors(errs)
	if len(errs) > 0 && !interpreterOnly {
		return joinErrors(errs), true
	}
	if interpreterOnly {
		return runInterpreted(file, expect)
	}
	if failure, failed := runNative(exe, expect); failed {
		return failure, true
	}
	if expect.NativeOnly {
		return "", false
	}
	return runInterpreted(file, expect)
}

// onlyInterpreterErrors is whether a program failed to compile only because it uses a map, a list, a pool, a frozen value,
// a cown or a behaviour, which the compiler has no code for yet. The test runner runs such a program under the
// interpreter alone.
func onlyInterpreterErrors(errs []*diag.CompileError) bool {
	for _, e := range errs {
		if e.Code != diag.ErrInterpreterOnly {
			return false
		}
	}
	return len(errs) > 0
}

// runInterpreted runs a program under the tracking interpreter, which must give what the program expects and break no
// rule of the memory model. A native run that passed has the same expectation, so the two agree. A program that schedules
// behaviours runs under several schedules, and each must give the same output, which the program's directives say, and
// break no rule.
func runInterpreted(file string, expect expectation) (failure string, failed bool) {
	defer func() {
		if r := recover(); r != nil {
			failure, failed = "the interpreter: "+describeInternal(r, debug.Stack()), true
		}
	}()
	program, errs := driver.Prepare(file, jobs)
	if len(errs) > 0 {
		return "the interpreter: " + joinErrors(errs), true
	}
	for schedule := range int64(scheduleSeeds) {
		result := interp.RunWith(program, interp.Options{StepLimit: interp.DefaultStepLimit, Seed: schedule})
		where := ""
		if result.Behaviours > 0 {
			where = fmt.Sprintf(" under the schedule of seed %d", schedule)
		}
		switch {
		case len(result.Violations) > 0:
			return "the interpreter found a violation of the memory model" + where + ":\n" + result.Report(), true
		case result.TimedOut:
			return "the interpreter timed out" + where, true
		case result.ExitCode != expect.Exit:
			return fmt.Sprintf("the interpreter exited %d, expected %d%s; stderr: %s", result.ExitCode, expect.Exit, where, strings.TrimSpace(string(result.Stderr))), true
		case string(result.Stdout) != expect.Stdout:
			return "the interpreter's stdout differs" + where + "\n--- expected\n" + expect.Stdout + "--- actual\n" + string(result.Stdout), true
		}
		if result.Behaviours == 0 {
			break
		}
	}
	return "", false
}

// runNative runs the executable and checks its output and exit code against what the program expects.
func runNative(exe string, expect expectation) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), testRunTimeout)

	defer cancel()
	cmd := exec.CommandContext(ctx, exe)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = testWaitDelay
	waitErr := runLimited(cmd, expect.MemoryLimitMB)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timed out", true
	}
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) {
		return "cannot run " + exe + ": " + waitErr.Error(), true
	}
	actual := strings.ReplaceAll(stdout.String(), "\r\n", "\n")
	if code := exitCodeOf(cmd.ProcessState); code != expect.Exit {
		limit := ""
		if expect.MemoryLimitMB > 0 {
			limit = fmt.Sprintf(" (the program may have used more than its %d MB, which fails an allocation and crashes it)", expect.MemoryLimitMB)
		}
		return fmt.Sprintf("exit %d, expected %d%s; stderr: %s", code, expect.Exit, limit, strings.TrimSpace(stderr.String())), true
	}
	if actual != expect.Stdout {
		return "stdout differs\n--- expected\n" + expect.Stdout + "--- actual\n" + actual, true
	}
	return "", false
}

// runLimited runs the command to its end, with its memory capped when a limit is given.
func runLimited(cmd *exec.Cmd, limitMB int) error {
	if limitMB == 0 {
		return cmd.Run()
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	release, err := limitMemory(cmd.Process, limitMB)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	defer release()
	return cmd.Wait()
}
