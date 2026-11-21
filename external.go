package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"minimaxon/internal/diag"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	withFlagPrefix = "--with="
	// notImplementedExitCode is what a compiler exits with when it does not have the command it was asked for yet. The test
	// runner counts such a test apart from the failed ones, so that a compiler that is being ported is measured by what it
	// has.
	notImplementedExitCode = 6
	// externalCompileTimeout bounds one compile by an external compiler, which may be far slower than this one.
	externalCompileTimeout = time.Minute
)

// diagnosticLine is a located diagnostic as a compiler prints it: error CODE: path:L:C: message. The path is matched as
// little as it can be, since it may hold a drive's colon.
var diagnosticLine = regexp.MustCompile(`^error (E\d+): (.+?):(\d+):(\d+): (.*)$`)

// testCompiler compiles the programs of the corpus: with this compiler's driver, or with the executable of another one
// (the compiler that self/ makes), which is run as a subprocess.
type testCompiler struct {
	// external is the path of the compiler to run instead of the driver; empty means the driver.
	external string
}

// compileOutcome is what compiling a test program gave. A compiler that does not have the command yet is unsupported,
// which is neither a pass nor a failure.
type compileOutcome struct {
	errs        []*diag.CompileError
	internal    string
	unsupported bool
}

// compile makes the executable of file at output, as the driver does for every test, so that a test that expects an
// error the generate stage raises gets it. The executables count their live heap boxes (countAllocations).
func (c testCompiler) compile(file, output string) compileOutcome {
	if c.external == "" {
		errs, internal := compileForTest(file, output)
		return compileOutcome{errs: errs, internal: internal}
	}
	arguments := []string{file, "-o", output}
	if countAllocations {
		arguments = append(arguments, countAllocationsFlag)
	}
	return c.runExternal("build", arguments...)
}

// interprets is whether a program that compiled is also run under the interpreter. The test of an external compiler
// measures what that compiler makes, so the interpreter, which belongs to this one, has no part in it.
func (c testCompiler) interprets() bool { return c.external == "" }

func (c testCompiler) runExternal(command string, arguments ...string) compileOutcome {
	ctx, cancel := context.WithTimeout(context.Background(), externalCompileTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.external, append([]string{command}, arguments...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = testWaitDelay
	runErr := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return compileOutcome{internal: fmt.Sprintf("%s %s timed out", c.external, command)}
	}
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		return compileOutcome{internal: "cannot run " + c.external + ": " + runErr.Error()}
	}
	switch code := exitCodeOf(cmd.ProcessState); code {
	case 0:
		return compileOutcome{}
	case notImplementedExitCode:
		return compileOutcome{unsupported: true}
	case 1:
		errs := parseDiagnostics(stderr.String())
		if len(errs) == 0 {
			return compileOutcome{internal: fmt.Sprintf("%s %s exited 1 without a diagnostic; stderr: %s", c.external, command, strings.TrimSpace(stderr.String()))}
		}
		return compileOutcome{errs: errs}
	default:
		return compileOutcome{internal: fmt.Sprintf("%s %s exited %d; stderr: %s", c.external, command, code, strings.TrimSpace(stderr.String()))}
	}
}

// parseDiagnostics reads the located diagnostics a compiler wrote to its standard error.
func parseDiagnostics(text string) []*diag.CompileError {
	var errs []*diag.CompileError
	for line := range strings.SplitSeq(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		m := diagnosticLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		lineNumber, _ := strconv.Atoi(m[3])
		column, _ := strconv.Atoi(m[4])
		errs = append(errs, &diag.CompileError{Code: m[1], File: m[2], Line: lineNumber, Col: column, Message: m[5]})
	}
	return errs
}
