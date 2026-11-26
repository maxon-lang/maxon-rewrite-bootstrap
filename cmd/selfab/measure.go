package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	irSuffix = ".ir"

	// The IR operation that tests an index against a length (irtable's Bounds), and the runtime entry that fails a read of an
	// empty slot of an array (panicEmptySlot): what the compiler's ircount command counts in an IR file.
	boundsArgument    = "op=bounds"
	emptySlotArgument = "call=rt.panicEmptySlot"

	// The stages of --timings that are the check of function bodies, the range analysis among them, and the line that
	// ends the stage table.
	timingsTotalStage = "total"
	timingsHeader     = "compile timings"
	timingsHeaderRow  = "stage"
	timingsStageCols  = 3
)

// checkStages are the stages of --timings whose time is the compiler checking function bodies.
var checkStages = []string{"bodies", "verify"}

var executableSuffix = func() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}()

// report is what a compile of self/ with the side's compiler says besides its time.
type report struct {
	// checkShare is the percentage of the compile's task time that the check stages took.
	checkShare float64
	// bounds and emptySlots count the run-time checks in the IR of the compiler that the side's compiler makes: tests of
	// an index against a length, and reads of an array's slot that fail when it is empty.
	bounds     int
	emptySlots int
}

// inspect compiles self/ twice more with the side's compiler: once for the time of its stages and once for its IR, whose
// operations the counter, a compiler that has the ircount command (the working tree's), counts: one way of counting for both
// sides, the one the corpus's IR expectations use.
func (s side) inspect(counter string) (report, error) {
	timings, err := capture(s.tree, cacheOffEnvironment, s.compiler, "build", "self", "--timings", "-o", s.output())
	if err != nil {
		return report{}, err
	}
	share, err := checkShare(timings)
	if err != nil {
		return report{}, err
	}
	if err := execute(s.tree, cacheOffEnvironment, s.compiler, "build", "self", "--emit-ir", "-o", s.output()); err != nil {
		return report{}, err
	}
	bounds, emptySlots, err := countChecks(counter, s.output()+irSuffix)
	if err != nil {
		return report{}, err
	}
	return report{checkShare: share, bounds: bounds, emptySlots: emptySlots}, nil
}

// checkShare reads the last table of stages that --timings prints, the program's, which follows the runtime's: rows of a
// name, milliseconds and a percentage, under a header row and ended by the row of the total.
func checkShare(timings string) (float64, error) {
	var total, check float64
	inTable := false
	for line := range strings.Lines(timings) {
		if strings.HasPrefix(line, timingsHeader) {
			total, check, inTable = 0, 0, true
			continue
		}
		fields := strings.Fields(line)
		if !inTable || len(fields) != timingsStageCols || fields[0] == timingsHeaderRow || !strings.HasSuffix(fields[2], "%") {
			continue
		}
		millis, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return 0, fmt.Errorf("--timings: the time %q of stage %s is not a number", fields[1], fields[0])
		}
		if fields[0] == timingsTotalStage {
			total, inTable = millis, false
		} else if slices.Contains(checkStages, fields[0]) {
			check += millis
		}
	}
	if total == 0 {
		return 0, fmt.Errorf("--timings: no table of stages ended by a %q row in\n%s", timingsTotalStage, timings)
	}
	return 100 * check / total, nil
}

// countChecks counts the lines of an IR file that are a bounds test or the failure of an empty slot, as the counter's ircount
// command does.
func countChecks(counter, path string) (bounds, emptySlots int, err error) {
	out, err := capture(".", nil, counter, "ircount", path, boundsArgument, emptySlotArgument)
	if err != nil {
		return 0, 0, err
	}
	return parseCounts(out)
}

// parseCounts reads the lines of ircount's output, `<argument> <count>`.
func parseCounts(out string) (bounds, emptySlots int, err error) {
	counts := map[string]int{}
	for line := range strings.Lines(out) {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return 0, 0, fmt.Errorf("ircount: unexpected line %q", line)
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil {
			return 0, 0, fmt.Errorf("ircount: the count in %q is not a number", line)
		}
		counts[fields[0]] = n
	}
	for _, want := range []string{boundsArgument, emptySlotArgument} {
		if _, ok := counts[want]; !ok {
			return 0, 0, fmt.Errorf("ircount: no count of %s in\n%s", want, out)
		}
	}
	return counts[boundsArgument], counts[emptySlotArgument], nil
}

func median(times []time.Duration) time.Duration {
	sorted := slices.Sorted(slices.Values(times))
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

// ratioRange is the lowest and the highest of the ratios B/A of the pairs of runs that followed each other.
func ratioRange(timesA, timesB []time.Duration) (low, high float64) {
	for i := range timesA {
		ratio := float64(timesB[i]) / float64(timesA[i])
		if i == 0 || ratio < low {
			low = ratio
		}
		if i == 0 || ratio > high {
			high = ratio
		}
	}
	return low, high
}

func milliseconds(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

// printTimes says what each side's runs took, in milliseconds, and how the medians compare.
func printTimes(title string, a, b side, timesA, timesB []time.Duration) {
	medianA, medianB := median(timesA), median(timesB)
	low, high := ratioRange(timesA, timesB)
	fmt.Printf("%s, %d runs each, interleaved\n", title, len(timesA))
	for _, row := range []struct {
		s     side
		times []time.Duration
	}{{a, timesA}, {b, timesB}} {
		fmt.Printf("  %-28s median %7.1f ms  range %7.1f-%7.1f ms\n",
			row.s.name, milliseconds(median(row.times)), milliseconds(slices.Min(row.times)), milliseconds(slices.Max(row.times)))
	}
	fmt.Printf("  B/A of the medians %.4f; B/A by pair of runs %.4f-%.4f\n", float64(medianB)/float64(medianA), low, high)
}

// printChecks says how much of a build of each side the check of bodies is and how many run-time checks its compiler holds.
func printChecks(a, b side, reportA, reportB report) {
	fmt.Println("the compiler that each makes of self/")
	for _, row := range []struct {
		s side
		r report
	}{{a, reportA}, {b, reportB}} {
		fmt.Printf("  %-28s check %5.1f%%  bounds %d  empty-slot %d\n", row.s.name, row.r.checkShare, row.r.bounds, row.r.emptySlots)
	}
}

// execute runs a command in dir with its output on the tool's standard error.
func execute(dir string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), env...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// capture runs a command in dir and gives what it wrote to its standard output and its standard error, which is where the
// compiler writes --timings.
func capture(dir string, env []string, name string, args ...string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, out.String())
	}
	return out.String(), nil
}
