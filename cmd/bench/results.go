package main

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

// The two implementations of each benchmark, as the directories of a batch and the columns of a report name them.
const (
	implMinimaxon = "minimaxon"
	implGo        = "go"
	// workPhase is the phase that spans a whole program.
	workPhase = "work"
	// missing stands for a measurement that a system does not give.
	missing = "-"
)

var impls = [...]string{implMinimaxon, implGo}

// The positions of the implementations in impls, and so in a benchmark's measures.
const (
	minimaxonAt = 0
	goAt        = 1
)

// measure is what the runs of one executable of one benchmark gave.
type measure struct {
	// phases is the phase names in the order they were printed; times[i] is the nanoseconds of phases[i], one for each round.
	phases []string
	times  [][]int64
	// rssKB and wallNanos have an entry for each round in which the system gave one, exits for every round.
	rssKB     []int64
	wallNanos []int64
	exits     []int
	size      int64
	// result is the lines the first round printed besides its timings.
	result []string
}

func (m *measure) phaseTimes(phase string) []int64 {
	i := slices.Index(m.phases, phase)
	if i < 0 {
		return nil
	}
	return m.times[i]
}

func (m *measure) addPhase(phase string, nanos int64) {
	i := slices.Index(m.phases, phase)
	if i < 0 {
		i = len(m.phases)
		m.phases = append(m.phases, phase)
		m.times = append(m.times, nil)
	}
	m.times[i] = append(m.times[i], nanos)
}

// benchmark holds the measures of the two implementations of one program.
type benchmark struct {
	name     string
	expected []string
	measures [len(impls)]*measure
}

func implIndex(impl string) (int, error) {
	i := slices.Index(impls[:], impl)
	if i < 0 {
		return 0, fmt.Errorf("unknown implementation %q", impl)
	}
	return i, nil
}

// results is one results file: what run.sh or run.ps1 wrote for a target.
type results struct {
	file       string
	target     string
	meta       []metaLine
	benchmarks []*benchmark
}

type metaLine struct{ key, value string }

func (r *results) metaValue(key string) string {
	for _, m := range r.meta {
		if m.key == key {
			return m.value
		}
	}
	return missing
}

func (r *results) benchmark(name string) *benchmark {
	for _, b := range r.benchmarks {
		if b.name == name {
			return b
		}
	}
	b := &benchmark{name: name}
	for i := range b.measures {
		b.measures[i] = &measure{}
	}
	r.benchmarks = append(r.benchmarks, b)
	return b
}

func readResults(file string) (*results, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := &results{file: file}
	scanner := bufio.NewScanner(f)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		if err := r.addLine(strings.Split(line, "\t")); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", file, lineNumber, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if r.target == "" {
		return nil, fmt.Errorf("%s: no target line", file)
	}
	return r, nil
}

// fieldCounts is how many tab-separated fields a line of each kind has, its kind included.
var fieldCounts = map[string]int{"meta": 3, "expect": 3, "size": 4, "exit": 5, "wall": 5, "rss": 5, "phase": 6, "result": 4}

func (r *results) addLine(fields []string) error {
	kind := fields[0]
	want, known := fieldCounts[kind]
	if !known {
		return fmt.Errorf("unknown kind of line %q", kind)
	}
	if len(fields) != want {
		return fmt.Errorf("a %s line has %d fields, not %d", kind, want, len(fields))
	}
	switch kind {
	case "meta":
		r.meta = append(r.meta, metaLine{fields[1], fields[2]})
		if fields[1] == "target" {
			r.target = fields[2]
		}
	case "expect":
		b := r.benchmark(fields[1])
		b.expected = append(b.expected, fields[2])
	default:
		return r.addMeasurement(kind, fields)
	}
	return nil
}

func (r *results) addMeasurement(kind string, fields []string) error {
	i, err := implIndex(fields[1])
	if err != nil {
		return err
	}
	m := r.benchmark(fields[2]).measures[i]
	if kind == "result" {
		m.result = append(m.result, fields[3])
		return nil
	}
	last := fields[len(fields)-1]
	if last == missing {
		return nil
	}
	value, err := strconv.ParseInt(last, 10, 64)
	if err != nil {
		return fmt.Errorf("%s: %w", kind, err)
	}
	switch kind {
	case "size":
		m.size = value
	case "exit":
		m.exits = append(m.exits, int(value))
	case "wall":
		m.wallNanos = append(m.wallNanos, value)
	case "rss":
		m.rssKB = append(m.rssKB, value)
	case "phase":
		m.addPhase(fields[4], value)
	default:
		panic("addMeasurement: unhandled kind of line " + kind)
	}
	return nil
}

// best is the least of the values, and whether there is one.
func best(values []int64) (int64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	return slices.Min(values), true
}
