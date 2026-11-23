// Package phase times the phases of a benchmark program and prints them as the Maxon programs of tests/bench do, so that
// cmd/bench reads the lines of both alike.
package phase

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// WorkName names the phase that spans the whole program, as the Maxon programs name it.
const WorkName = "work"

// Timer measures the phases of one run.
type Timer struct {
	begun time.Time
	lap   time.Time
}

// Start begins the whole run and its first phase.
func Start() *Timer {
	now := time.Now()
	return &Timer{begun: now, lap: now}
}

// Lap prints the time of the phase that ended now, `<name> ns <nanoseconds>`, and begins the next one.
func (t *Timer) Lap(name string) {
	now := time.Now()
	fmt.Printf("%s ns %d\n", name, now.Sub(t.lap).Nanoseconds())
	t.lap = now
}

// Finish prints the time of the whole run as the last phase.
func (t *Timer) Finish() {
	fmt.Printf("%s ns %d\n", WorkName, time.Since(t.begun).Nanoseconds())
}

// Float formats a float as Maxon prints one: the shortest digits that read back the same, and a decimal point.
func Float(x float64) string {
	text := strconv.FormatFloat(x, 'f', -1, 64)
	if !strings.ContainsRune(text, '.') {
		text += ".0"
	}
	return text
}
