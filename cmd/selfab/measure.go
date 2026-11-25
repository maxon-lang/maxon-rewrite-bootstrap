package main

import (
	"bufio"
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

	// boundsPrefix starts the line of the IR operation that tests an index against a length (irtable's Bounds), and
	// emptySlotCall the line that fails a read of an empty slot of an array (the runtime's panicEmptySlot).
	boundsPrefix  = "bounds "
	emptySlotCall = "runtime rt.panicEmptySlot("

	// The stages of --timings that are the check of function bodies, the range analysis among them, and the line that
	// ends the stage table.
	timingsTotalStage = "total"
	timingsHeader     = "compile timings"
	timingsHeaderRow  = "stage"
	timingsStageCols  = 3

	// scannerLimit is the longest line of an IR file that the scanner takes.
	scannerLimit = 1 << 20
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

// inspect compiles self/ twice more with the side's compiler: once for the time of its stages and once for its IR.
func (s side) inspect() (report, error) {
	timings, err := capture(s.tree, s.compiler, "build", "self", "--no-cache", "--timings", "-o", s.output())
	if err != nil {
		return report{}, err
	}
	share, err := checkShare(timings)
	if err != nil {
		return report{}, err
	}
	if err := execute(s.tree, nil, s.compiler, "build", "self", "--no-cache", "--emit-ir", "-o", s.output()); err != nil {
		return report{}, err
	}
	bounds, emptySlots, err := countChecks(s.output() + irSuffix)
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

// countChecks counts the lines of an IR file that are a bounds test or the failure of an empty slot.
func countChecks(path string) (bounds, emptySlots int, err error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, scannerLimit)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, boundsPrefix):
			bounds++
		case strings.HasPrefix(line, emptySlotCall):
			emptySlots++
		}
	}
	return bounds, emptySlots, scanner.Err()
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

func printReport(a, b side, timesA, timesB []time.Duration, reportA, reportB report) {
	medianA, medianB := median(timesA), median(timesB)
	low, high := ratioRange(timesA, timesB)
	fmt.Printf("self-build, %d runs each, interleaved, no cache\n", len(timesA))
	for _, row := range []struct {
		s      side
		times  []time.Duration
		median time.Duration
		r      report
	}{{a, timesA, medianA, reportA}, {b, timesB, medianB, reportB}} {
		fastest, slowest := slices.Min(row.times), slices.Max(row.times)
		fmt.Printf("  %-28s median %7.1f ms  range %7.1f-%7.1f ms  check %5.1f%%  bounds %d  empty-slot %d\n",
			row.s.name, milliseconds(row.median), milliseconds(fastest), milliseconds(slowest), row.r.checkShare, row.r.bounds, row.r.emptySlots)
	}
	fmt.Printf("  B/A of the medians %.4f; B/A by pair of runs %.4f-%.4f\n", float64(medianB)/float64(medianA), low, high)
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
func capture(dir string, name string, args ...string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, out.String())
	}
	return out.String(), nil
}
