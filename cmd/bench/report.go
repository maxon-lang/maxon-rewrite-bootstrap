package main

import (
	"cmp"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
)

const (
	nanosPerMilli = 1e6
	kilobyte      = 1024
	// minPhaseMillis is the shortest best time, of either implementation, that a phase needs to count in a mean of ratios:
	// below it the clocks' granularity decides the ratio.
	minPhaseMillis = 1.0
	// worstPhases is how many phases of each target the summary names.
	worstPhases = 3
)

// ratioNote says how to read a ratio.
const ratioNote = "Ratio is minimaxon's best time over Go's, so below 1 minimaxon is faster. Every time is the best of the rounds."

// comparison is one phase of one benchmark on one target.
type comparison struct {
	benchmark string
	phase     string
	minimaxon float64
	goTime    float64
}

func (c comparison) ratio() float64 { return c.minimaxon / c.goTime }

// bestMillis is the best time of a phase in milliseconds, and whether the run measured it.
func bestMillis(m *measure, phase string) (float64, bool) {
	nanos, ok := best(m.phaseTimes(phase))
	return float64(nanos) / nanosPerMilli, ok
}

// comparisons lists the phases that both implementations measured, benchmark by benchmark, the whole run last.
func comparisons(r *results) []comparison {
	var all []comparison
	for _, b := range r.benchmarks {
		for _, phase := range phaseOrder(b) {
			mx, mxOK := bestMillis(b.measures[minimaxonAt], phase)
			g, gOK := bestMillis(b.measures[goAt], phase)
			if mxOK && gOK && g > 0 && mx > 0 {
				all = append(all, comparison{benchmark: b.name, phase: phase, minimaxon: mx, goTime: g})
			}
		}
	}
	return all
}

// phaseOrder is the phases of a benchmark in the order minimaxon printed them, then Go's others, with the whole run last.
func phaseOrder(b *benchmark) []string {
	var phases []string
	for _, m := range b.measures {
		for _, p := range m.phases {
			if p != workPhase && !slices.Contains(phases, p) {
				phases = append(phases, p)
			}
		}
	}
	return append(phases, workPhase)
}

func geometricMean(ratios []float64) float64 {
	sum := 0.0
	for _, r := range ratios {
		sum += math.Log(r)
	}
	return math.Exp(sum / float64(len(ratios)))
}

func formatMillis(v float64, ok bool) string {
	if !ok {
		return missing
	}
	return fmt.Sprintf("%.2f", v)
}

func formatRatio(mx, g float64, ok bool) string {
	if !ok || g == 0 {
		return missing
	}
	return fmt.Sprintf("%.2f", mx/g)
}

// writeReport writes the Markdown report of the results of several targets, side by side.
func writeReport(w io.Writer, all []*results) {
	fmt.Fprintf(w, "# minimaxon against Go\n\n%s\n\n", ratioNote)
	writeSystems(w, all)
	writeSummary(w, all)
	for _, name := range benchmarkNames(all) {
		writeBenchmark(w, all, name)
	}
	writeFootprint(w, all)
}

func benchmarkNames(all []*results) []string {
	var names []string
	for _, r := range all {
		for _, b := range r.benchmarks {
			if !slices.Contains(names, b.name) {
				names = append(names, b.name)
			}
		}
	}
	slices.Sort(names)
	return names
}

func (r *results) find(name string) *benchmark {
	for _, b := range r.benchmarks {
		if b.name == name {
			return b
		}
	}
	return nil
}

func writeSystems(w io.Writer, all []*results) {
	fmt.Fprint(w, "## Systems\n\n| target | CPU | cores | OS | rounds | compiler | Go |\n|---|---|---|---|---|---|---|\n")
	for _, r := range all {
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s | %s |\n", r.target, r.metaValue("cpu"), r.metaValue("cores"),
			r.metaValue("uname"), r.metaValue("rounds"), r.metaValue("compiler"), r.metaValue("go"))
	}
	fmt.Fprintln(w)
}

func writeSummary(w io.Writer, all []*results) {
	fmt.Fprint(w, "## Summary\n\n")
	fmt.Fprint(w, "| target | geometric-mean ratio of the programs | of the phases of 1 ms or more | worst phases |\n|---|---|---|---|\n")
	for _, r := range all {
		var programs, phases []float64
		var counted []comparison
		for _, c := range comparisons(r) {
			if c.phase == workPhase {
				programs = append(programs, c.ratio())
			}
			if min(c.minimaxon, c.goTime) >= minPhaseMillis {
				phases = append(phases, c.ratio())
				counted = append(counted, c)
			}
		}
		slices.SortStableFunc(counted, func(a, b comparison) int { return -cmp.Compare(a.ratio(), b.ratio()) })
		var worst []string
		for _, c := range counted[:min(worstPhases, len(counted))] {
			worst = append(worst, fmt.Sprintf("%s / %s %.2f", c.benchmark, c.phase, c.ratio()))
		}
		fmt.Fprintf(w, "| %s | %s | %s | %s |\n", r.target, formatMean(programs), formatMean(phases), strings.Join(worst, "; "))
	}
	fmt.Fprintln(w)
}

func formatMean(ratios []float64) string {
	if len(ratios) == 0 {
		return missing
	}
	return fmt.Sprintf("%.2f (%d)", geometricMean(ratios), len(ratios))
}

func writeBenchmark(w io.Writer, all []*results, name string) {
	fmt.Fprintf(w, "## %s\n\n| phase |", name)
	for _, r := range all {
		fmt.Fprintf(w, " %[1]s minimaxon ms | %[1]s Go ms | %[1]s ratio |", r.target)
	}
	fmt.Fprint(w, "\n|---|")
	fmt.Fprint(w, strings.Repeat("---:|---:|---:|", len(all)))
	fmt.Fprintln(w)
	var phases []string
	for _, r := range all {
		if b := r.find(name); b != nil {
			for _, p := range phaseOrder(b) {
				if p != workPhase && !slices.Contains(phases, p) {
					phases = append(phases, p)
				}
			}
		}
	}
	for _, p := range append(phases, workPhase) {
		fmt.Fprintf(w, "| %s |", p)
		for _, r := range all {
			var mx, g float64
			var mxOK, gOK bool
			if b := r.find(name); b != nil {
				mx, mxOK = bestMillis(b.measures[minimaxonAt], p)
				g, gOK = bestMillis(b.measures[goAt], p)
			}
			fmt.Fprintf(w, " %s | %s | %s |", formatMillis(mx, mxOK), formatMillis(g, gOK), formatRatio(mx, g, mxOK && gOK))
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w)
	for _, r := range all {
		if b := r.find(name); b != nil {
			for _, problem := range problems(b) {
				fmt.Fprintf(w, "- %s: %s\n", r.target, problem)
			}
		}
	}
	fmt.Fprintln(w)
}

// problems lists what is wrong with the runs of a benchmark: a run that failed, or one that printed other than the
// expected result, which makes its times meaningless.
func problems(b *benchmark) []string {
	var found []string
	for i, m := range b.measures {
		if len(m.exits) == 0 {
			found = append(found, fmt.Sprintf("%s did not run", impls[i]))
			continue
		}
		if slices.ContainsFunc(m.exits, func(code int) bool { return code != 0 }) {
			found = append(found, fmt.Sprintf("%s exited with a failure (exit codes %v)", impls[i], m.exits))
		}
		if len(b.expected) > 0 && !slices.Equal(m.result, b.expected) {
			found = append(found, fmt.Sprintf("%s printed %q, not the expected %q", impls[i], m.result, b.expected))
		}
	}
	return found
}

func writeFootprint(w io.Writer, all []*results) {
	fmt.Fprint(w, "## Peak memory and executable size\n\nPeak memory is the largest resident set of the runs, in MiB; size is the file's, in KiB.\n\n| program |")
	for _, r := range all {
		fmt.Fprintf(w, " %[1]s minimaxon MiB | %[1]s Go MiB | %[1]s minimaxon KiB | %[1]s Go KiB |", r.target)
	}
	fmt.Fprint(w, "\n|---|")
	fmt.Fprint(w, strings.Repeat("---:|---:|---:|---:|", len(all)))
	fmt.Fprintln(w)
	for _, name := range benchmarkNames(all) {
		fmt.Fprintf(w, "| %s |", name)
		for _, r := range all {
			b := r.find(name)
			if b == nil {
				fmt.Fprint(w, " - | - | - | - |")
				continue
			}
			fmt.Fprintf(w, " %s | %s | %s | %s |", peakMiB(b.measures[minimaxonAt]), peakMiB(b.measures[goAt]), sizeKiB(b.measures[minimaxonAt]), sizeKiB(b.measures[goAt]))
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w)
}

func peakMiB(m *measure) string {
	if len(m.rssKB) == 0 {
		return missing
	}
	return fmt.Sprintf("%.1f", float64(slices.Max(m.rssKB))/kilobyte)
}

func sizeKiB(m *measure) string {
	if m.size == 0 {
		return missing
	}
	return fmt.Sprintf("%.0f", float64(m.size)/kilobyte)
}
