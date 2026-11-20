package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"minimaxon/internal/ast"
	"minimaxon/internal/cache"
	"minimaxon/internal/diag"
	"minimaxon/internal/driver"
	"minimaxon/internal/interp"
	"minimaxon/internal/syntax"

	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
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
  minimaxon rewrite --keyword=old=new <path>...   rename a keyword in the .maxon files and the code in the .md files under the paths
  minimaxon fmt [--check] <path>...               put the .maxon files and the Maxon code in the .md files under the paths in the one layout

A directory is a project: each directory under it is a module. --jobs=N sets how many modules
compile at once (default: one fewer than the processor count). --timings prints each phase's time and busy cores;
--trace=FILE writes an execution trace of the compile for go tool trace. --emit-ir writes the IR beside the
executable, as <out>.ir.
--no-cache does not use the build cache (see MINIMAXON_CACHE in the README).
--seed=N picks the schedule of behaviours that interp runs. --count-allocations builds executables that count their live
heap boxes and fail at exit with code 99 if any is left (development only). --update-positions makes test rewrite
the position of an expect-error whose code was reported at another place.`

const (
	internalErrorExitCode = 3
	violationExitCode     = 4
	timeoutExitCode       = 5
	usageExitCode         = 2
	jobsFlagPrefix        = "--jobs="
	seedFlagPrefix        = "--seed="
	traceFlagPrefix       = "--trace="
	timingsFlag           = "--timings"
	emitIrFlag            = "--emit-ir"
	countAllocationsFlag  = "--count-allocations"
	noCacheFlag           = "--no-cache"
	cacheEnvironment      = "MINIMAXON_CACHE"
	updatePositionsFlag   = "--update-positions"
	keywordFlagPrefix     = "--keyword="
	checkFlag             = "--check"
	modulesDir            = "modules"
	sourceExtension       = ".maxon"
	markdownExtension     = ".md"
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
	// tracePath is where build, run and check write an execution trace of the compile, when it is not empty.
	tracePath string
	emitIR    bool
	// countAllocations builds executables that count their live heap boxes (see the README, "Counting allocations").
	countAllocations bool
	// noCache turns the build cache off for a build or a run.
	noCache bool
	// updatePositions makes the test runner rewrite an expect-error's position when the expected code was reported
	// somewhere else, as it is after a change that only moves code, such as renaming a keyword.
	updatePositions bool
	seed            int64
)

func compileWithFlags(path, output string) []*diag.CompileError {
	options := flagOptions()
	if output != "" {
		options.Cache = buildCache()
	}
	return driver.Compile(path, output, options)
}

// buildCache is the cache that building an executable reads and writes: the directory MINIMAXON_CACHE names, by default one
// in the user's cache directory. It is nil when --no-cache or MINIMAXON_CACHE=off turn it off, or when there is no
// directory to keep it in or no way to tell which compiler is running, since then no entry can be trusted to be this
// compiler's.
func buildCache() *cache.Cache {
	if noCache {
		return nil
	}
	dir, enabled := cache.Dir(os.Getenv(cacheEnvironment))
	if !enabled {
		return nil
	}
	compiler, err := cache.Executable()
	if err != nil {
		return nil
	}
	return cache.Open(dir, compiler)
}

func flagOptions() driver.Options {
	return driver.Options{Jobs: jobs, Timings: timings, Trace: tracePath, EmitIr: emitIR, CountAllocations: countAllocations}
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
		if strings.HasPrefix(a, traceFlagPrefix) {
			tracePath = a[len(traceFlagPrefix):]
			if tracePath == "" {
				fmt.Fprintln(os.Stderr, "--trace takes a file name")
				return usageExitCode
			}
			args = filterArgs(args, func(other string) bool { return other != a })
			break
		}
	}

	for _, a := range args {
		timings = timings || a == timingsFlag
		emitIR = emitIR || a == emitIrFlag
		countAllocations = countAllocations || a == countAllocationsFlag
		noCache = noCache || a == noCacheFlag
		updatePositions = updatePositions || a == updatePositionsFlag
	}
	args = filterArgs(args, func(a string) bool {
		return a != timingsFlag && a != emitIrFlag && a != countAllocationsFlag && a != noCacheFlag && a != updatePositionsFlag
	})
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
	case args[0] == "rewrite" && len(args) >= 3 && strings.HasPrefix(args[1], keywordFlagPrefix):
		old, new, ok := strings.Cut(args[1][len(keywordFlagPrefix):], "=")
		if !ok || old == "" || new == "" {
			fmt.Fprintln(os.Stderr, usage)
			return usageExitCode
		}
		return renameKeyword(old, new, args[2:])
	case args[0] == "fmt" && len(args) >= 2:
		check := slices.Contains(args[1:], checkFlag)
		paths := filterArgs(args[1:], func(a string) bool { return a != checkFlag })
		if len(paths) == 0 {
			fmt.Fprintln(os.Stderr, usage)
			return usageExitCode
		}
		return formatFiles(paths, check)
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
	options := flagOptions()
	// One trace is written per process, and the corpus compiles many programs at once.
	options.Trace = ""
	return driver.Compile(file, output, options), ""
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
			if e.Code == code && (!hasAt || at == positionText(e)) {
				return "", false
			}
		}
		if updatePositions && hasAt {
			for _, e := range errs {
				if e.Code == code {
					moveExpectedError(file, expect.Error, code+errorLocationSplit+positionText(e))
					return "", false
				}
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

func positionText(e *diag.CompileError) string {
	return strconv.Itoa(e.Line) + ":" + strconv.Itoa(e.Col)
}

// moveExpectedError rewrites the expect-error directive of program, a file or a project directory, from old to new.
func moveExpectedError(program, old, new string) {
	files := []string{program}
	if info, err := os.Stat(program); err == nil && info.IsDir() {
		files, _ = filepath.Glob(filepath.Join(program, "*"+sourceExtension))
	}
	oldLine, newLine := expectErrorPrefix+" "+old, expectErrorPrefix+" "+new
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			panic(diag.Internal("cannot read %s: %v", file, err))
		}
		text := string(data)
		if !strings.Contains(text, oldLine) {
			continue
		}
		if err := os.WriteFile(file, []byte(strings.Replace(text, oldLine, newLine, 1)), 0o644); err != nil {
			panic(diag.Internal("cannot write %s: %v", file, err))
		}
		fmt.Printf("moved  %s: %s -> %s\n", file, old, new)
		return
	}
	panic(diag.Internal("no file of %s holds '%s'", program, oldLine))
}

// renameKeyword is the rewrite command: it renames the keyword old to new in every .maxon file under paths, by token,
// and reports the files it could not lex, which it leaves alone. It runs before Maxon.g4 changes, while old is still
// a keyword.
func renameKeyword(old, new string, paths []string) int {
	if !ast.IsReserved(old) {
		fmt.Fprintf(os.Stderr, "'%s' is not a keyword of the current grammar: rename it before changing Maxon.g4\n", old)
		return usageExitCode
	}
	found, err := collectSourceFiles(paths)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	files := make([]string, len(found))
	for i, f := range found {
		files[i] = f.path
	}
	renamed, changedFiles, skipped := 0, 0, 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			panic(diag.Internal("cannot read %s: %v", file, err))
		}
		text := string(data)
		var edits []syntax.Edit
		if strings.HasSuffix(file, markdownExtension) {
			var refused int
			var first *diag.CompileError
			edits, refused, first = syntax.MarkdownKeywordEdits(file, text, old, new)
			if refused > 0 {
				fmt.Printf("skipped  %d code spans or blocks that do not lex, first %s\n", refused, first)
				skipped += refused
			}
		} else {
			var errs []*diag.CompileError
			diag.Guard(&errs, func() {
				edits = syntax.KeywordEdits(syntax.NewSource(text, []syntax.FileSlice{{File: file, Begin: 0, End: len(text)}}), old, new)
			})
			if len(errs) > 0 {
				// A file the lexer refuses, such as a test of a lexer error, matters only if it may hold the keyword.
				if strings.Contains(text, old) {
					fmt.Printf("skipped  %s\n", errs[0])
					skipped++
				}
				continue
			}
		}
		if len(edits) == 0 {
			continue
		}
		if err := os.WriteFile(file, []byte(syntax.ApplyEdits(text, edits)), 0o644); err != nil {
			panic(diag.Internal("cannot write %s: %v", file, err))
		}
		renamed += len(edits)
		changedFiles++
	}
	fmt.Printf("renamed %d uses of '%s' in %d files; %d files or Markdown code spans that may hold it could not be lexed\n", renamed, old, changedFiles, skipped)
	return 0
}

// sourceFile is a file of Maxon or of Markdown that a command reads: named on the command line, or found under a
// directory that was.
type sourceFile struct {
	path     string
	explicit bool
}

// collectSourceFiles lists the .maxon and .md files at paths, a file or a directory to search, sorted by path.
func collectSourceFiles(paths []string) ([]sourceFile, error) {
	var files []sourceFile
	for _, root := range paths {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && (strings.HasSuffix(path, sourceExtension) || strings.HasSuffix(path, markdownExtension)) {
				files = append(files, sourceFile{path: path, explicit: path == root})
			}
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", root, err)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	return files, nil
}

// layoutErrorCodes are the errors of the layout rules. A test that expects one is a program laid out wrongly on purpose,
// so fmt over a directory leaves it alone: it would put the layout right and the test would no longer test the rule.
var layoutErrorCodes = []string{diag.ErrUnexpectedToken, diag.ErrExpectedToken}

func expectsLayoutError(text string) bool {
	for _, code := range layoutErrorCodes {
		if strings.Contains(text, expectErrorPrefix+" "+code) {
			return true
		}
	}
	return false
}

// formatFiles is the fmt command: it puts every .maxon file under paths, and the Maxon code blocks of every .md file,
// in the one layout, and with check only lists the files that are not. A file that does not parse is left alone: one
// found under a directory is reported and skipped, since a corpus holds programs that are refused on purpose, and one
// named on the command line is an error.
func formatFiles(paths []string, check bool) int {
	files, err := collectSourceFiles(paths)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	changed, refused, layoutTests, failed := 0, 0, 0, false
	for _, file := range files {
		data, err := os.ReadFile(file.path)
		if err != nil {
			panic(diag.Internal("cannot read %s: %v", file.path, err))
		}
		text := string(data)
		formatted := text
		if strings.HasSuffix(file.path, markdownExtension) {
			formatted = syntax.FormatMarkdown(file.path, text)
		} else {
			if !file.explicit && expectsLayoutError(text) {
				layoutTests++
				continue
			}
			var errs []*diag.CompileError
			formatted, errs = syntax.Format(file.path, text)
			if len(errs) > 0 {
				refused++
				if file.explicit {
					failed = true
					report(errs)
				} else {
					fmt.Printf("skipped  %s\n", errs[0])
				}
				continue
			}
		}
		if formatted == text {
			continue
		}
		changed++
		if check {
			fmt.Printf("not formatted  %s\n", file.path)
			continue
		}
		if err := os.WriteFile(file.path, []byte(formatted), 0o644); err != nil {
			panic(diag.Internal("cannot write %s: %v", file.path, err))
		}
		fmt.Printf("formatted  %s\n", file.path)
	}
	verb := "formatted"
	if check {
		verb = "not formatted"
	}
	fmt.Printf("%d files %s, %d left alone because they do not parse, %d skipped because they test the layout rules\n",
		changed, verb, refused, layoutTests)
	if failed || check && changed > 0 {
		return 1
	}
	return 0
}
