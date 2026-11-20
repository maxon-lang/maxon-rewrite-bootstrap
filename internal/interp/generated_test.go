package interp_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"minimaxon/internal/diag"
	"minimaxon/internal/driver"
	"minimaxon/internal/interp"
)

const (
	defaultGeneratedPrograms = 300
	generatedStepLimit       = 20_000_000
	programsEnv              = "MINIMAXON_GEN_PROGRAMS"
	seedEnv                  = "MINIMAXON_GEN_SEED"
	keepEnv                  = "MINIMAXON_GEN_KEEP"
	nativeEnv                = "MINIMAXON_GEN_NATIVE"
	seedsEnv                 = "MINIMAXON_GEN_SEEDS"
	defaultScheduleSeeds     = 3
	nativeTimeout            = 10 * time.Second
	maxReportedFailures      = 3
	// leakMessage is what an executable that counts its allocations says when main returns with a heap box still live.
	leakMessage = "leaked allocations"
)

func envInt(name string, fallback int64) int64 {
	if v := os.Getenv(name); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			panic(fmt.Sprintf("%s: %v", name, err))
		}
		return n
	}
	return fallback
}

type outcome struct {
	rejected   []*diag.CompileError
	result     *interp.Result
	internal   any
	syntaxFail bool
	// seed is the schedule that gave result, which for a program that breaks a rule is the first that did.
	seed int64
}

func runGenerated(source, dir string) (out outcome) {
	path := filepath.Join(dir, "p.maxon")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		panic(err)
	}
	defer func() {
		if r := recover(); r != nil {
			out.internal = r
		}
	}()
	program, errs := driver.Prepare(path, 1)
	if len(errs) > 0 {
		out.rejected = errs
		for _, e := range errs {
			if syntaxCodes[e.Code] {
				out.syntaxFail = true
			}
		}
		return out
	}
	out.result = interp.RunWith(program, interp.Options{StepLimit: generatedStepLimit})
	if out.result.Behaviours > 0 {
		// The schedule of behaviours is random, so a program that has some runs under several, and a rule that only some
		// of them break is still broken.
		for seed := int64(1); seed < envInt(seedsEnv, defaultScheduleSeeds) && len(out.result.Violations) == 0; seed++ {
			if r := interp.RunWith(program, interp.Options{StepLimit: generatedStepLimit, Seed: seed}); len(r.Violations) > 0 {
				out.result, out.seed = r, seed
			}
		}
	}
	return out
}

// Every program the checker accepts runs under the tracking interpreter without a violation. Most of what the generator
// writes is refused, since it is written to reach the edges of the rules, and a refusal is not a failure; a program
// that is accepted and then breaks a rule is a hole in the checker, and a program that is not well formed is a bug in
// the generator.
func TestGeneratedProgramsRunClean(t *testing.T) {
	programs := envInt(programsEnv, defaultGeneratedPrograms)
	first := envInt(seedEnv, 1)
	dir := t.TempDir()
	keep := os.Getenv(keepEnv)

	accepted, trapped, timedOut, failures, compared, concurrent, behaviours := 0, 0, 0, 0, 0, 0, 0
	rejected := map[string]int{}
	for seed := first; seed < first+programs; seed++ {
		source := Program(seed)
		out := runGenerated(source, dir)
		fail := func(format string, args ...any) {
			failures++
			keepSource(keep, fmt.Sprintf("seed%d.maxon", seed), source)
			if failures <= maxReportedFailures {
				t.Errorf("seed %d: "+format+"\n%s", append(append([]any{seed}, args...), numbered(source))...)
			} else {
				t.Errorf("seed %d: "+format, append([]any{seed}, args...)...)
			}
		}
		switch {
		case out.internal != nil:
			fail("internal compiler error: %v", out.internal)
		case out.syntaxFail:
			fail("the generator wrote a program that does not parse: %v", out.rejected)
		case out.rejected != nil:
			rejected[out.rejected[0].Code]++
		case len(out.result.Violations) > 0:
			fail("a program the checker accepted breaks the memory model (schedule of seed %d):\n%s", out.seed, out.result.Report())
		case out.result.TimedOut:
			timedOut++
			keepSource(keep, fmt.Sprintf("timeout%d.maxon", seed), source)

		default:
			accepted++
			if out.result.ExitCode != 0 {
				trapped++
			}
			if out.result.Behaviours > 0 {
				concurrent++
				behaviours += out.result.Behaviours
			}
			if os.Getenv(nativeEnv) != "" {
				mismatch, ran := compareNative(dir, out.result)
				switch {
				case mismatch != "":
					fail("the executable and the interpreter differ: %s", mismatch)
				case ran:
					compared++
				}
			}
		}
	}
	codes := make([]string, 0, len(rejected))
	total := 0
	for c, n := range rejected {
		codes = append(codes, c)
		total += n
	}
	sort.Strings(codes)
	var parts []string
	for _, c := range codes {
		parts = append(parts, fmt.Sprintf("%s:%d", c, rejected[c]))
	}
	if compared > 0 {
		t.Logf("%d accepted programs also ran as executables with the same output and exit code", compared)
	}
	t.Logf("%d programs: %d accepted and clean (%d of them ended in a trap, %d ran out of steps), %d rejected by the checker [%s], %d failed",
		programs, accepted, trapped, timedOut, total, strings.Join(parts, " "), failures)
	t.Logf("%d of the accepted programs scheduled behaviours (%d in all), each under %d schedules", concurrent, behaviours, envInt(seedsEnv, defaultScheduleSeeds))
	if programs >= minProgramsForAcceptedShare && int64(accepted+failures)*minAcceptedDivisor < int64(programs) {
		t.Errorf("the checker accepted %d of %d programs, fewer than 1 in %d: a run that accepts almost nothing tests nothing, so the generator or its library no longer follows the language", accepted+failures, programs, minAcceptedDivisor)
	}
}

// A run of at least minProgramsForAcceptedShare programs must have 1 in minAcceptedDivisor accepted: only an accepted
// program reaches the interpreter, so a change to the language that the generator did not follow would otherwise pass
// with every program refused.
const (
	minProgramsForAcceptedShare = 100
	minAcceptedDivisor          = 10
)

// keepSource saves a program in the directory, when there is one, to look at it later.
func keepSource(dir, name, source string) {
	if dir == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644)
}

func numbered(source string) string {
	lines := strings.Split(source, "\n")
	for i, l := range lines {
		lines[i] = fmt.Sprintf("%4d  %s", i+1, l)
	}
	return strings.Join(lines, "\n")
}

// TestDumpProgram writes the program of one seed to the file in MINIMAXON_GEN_DUMP, for debugging the generator.
func TestDumpProgram(t *testing.T) {
	path := os.Getenv("MINIMAXON_GEN_DUMP")
	if path == "" {
		t.Skip("MINIMAXON_GEN_DUMP names the file to write")
	}
	if err := os.WriteFile(path, []byte(Program(envInt(seedEnv, 1))), 0o644); err != nil {
		t.Fatal(err)
	}
}

// syntaxCodes are the diagnostics that mean the program is not well formed, which the generator must never write.
var syntaxCodes = map[string]bool{
	diag.ErrUnexpectedToken:     true,
	diag.ErrUnexpectedEnd:       true,
	diag.ErrMismatchedEndLabel:  true,
	diag.ErrExpectedToken:       true,
	diag.ErrIntegerLiteralRange: true,
}

// compareNative compiles the program the interpreter just ran and runs the executable. A program that uses a map, a list or
// a pool has no executable yet and is skipped, and so is one that does not finish in time.
func compareNative(dir string, result *interp.Result) (mismatch string, ran bool) {
	path := filepath.Join(dir, "p.maxon")
	exe := filepath.Join(dir, "p.exe")
	if errs := driver.Compile(path, exe, driver.Options{Jobs: 1, CountAllocations: true}); len(errs) > 0 {
		for _, e := range errs {
			if e.Code != diag.ErrInterpreterOnly {
				return fmt.Sprintf("the program did not compile: %v", errs), false
			}
		}
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), nativeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", false
	}
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = int(int32(exitErr.ExitCode()))
	} else if err != nil {
		return err.Error(), false
	}
	switch {
	case strings.Contains(stderr.String(), leakMessage):
		// The interpreter found no leak, so a box the executable still holds is one that it did not free.
		return "the executable ended with live heap boxes: " + strings.TrimSpace(stderr.String()), true
	case code != result.ExitCode:
		return fmt.Sprintf("exit code %d against %d", code, result.ExitCode), true
	case stdout.String() != string(result.Stdout):
		return "standard output differs", true
	}
	return "", true
}
