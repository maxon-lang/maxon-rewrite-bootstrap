// Command selfab compares two versions of the compiler by what each costs to build self/ and by the run-time checks
// that the compiler it makes still holds. It builds C1 of each version (the version's own seed builds self/, so only
// the work the compiler does differs, not the quality of the code that built it), then runs `<C1> build self --no-cache`
// alternately on both, and reports the medians, their ratio, the share of the check in a build and the count of bounds
// tests and empty-slot tests in the IR of the compiler (README, "Tools").
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"minimaxon/internal/heavylock"
	"minimaxon/internal/hosts"
)

const (
	defaultRuns = 11
	exitFailure = 1
	exitUsage   = 2

	// compilerName and outputName are the files each version's bin/ holds while the tool runs.
	compilerName = "selfab-c1"
	outputName   = "selfab-out"
)

const usageText = `usage: selfab [--base=<git revision>] [--base-compiler=<C1 executable>] [--runs=N] [--root=.] [--note=TEXT]
  A is the version to compare with (default HEAD, built in a temporary worktree), B the working tree. Each is built as C1
  by its seed, and then <C1> build self --no-cache is timed N times (default 11) on both, interleaved with the order
  alternating, after one run of each that is not counted.
  --base-compiler uses an executable made of A's sources instead of building the revision; it finds runtime/ by its
  place, so it must lie in a checkout, and it builds the working tree's self/.
  --note adds a row for docs/self-compile-times.md, with the note as its description.
Run from the repository's root, on a machine with nothing else running.`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "selfab:", err)
		os.Exit(exitFailure)
	}
}

type options struct {
	base         string
	baseCompiler string
	runs         int
	root         string
	note         string
}

func run(args []string) error {
	var o options
	flags := flag.NewFlagSet("selfab", flag.ContinueOnError)
	flags.Usage = func() { fmt.Fprintln(os.Stderr, usageText) }
	flags.StringVar(&o.base, "base", "HEAD", "git revision of A")
	flags.StringVar(&o.baseCompiler, "base-compiler", "", "compiler executable of A")
	flags.IntVar(&o.runs, "runs", defaultRuns, "timed runs of each")
	flags.StringVar(&o.root, "root", ".", "the repository")
	flags.StringVar(&o.note, "note", "", "description of the row to print")
	if err := flags.Parse(args); err != nil {
		os.Exit(exitUsage)
	}
	if flags.NArg() != 0 || o.runs < 1 {
		flags.Usage()
		os.Exit(exitUsage)
	}
	root, err := filepath.Abs(o.root)
	if err != nil {
		return err
	}
	release, err := heavylock.Hold()
	if err != nil {
		return err
	}
	defer release()

	a, b, cleanup, err := prepare(root, o)
	defer cleanup()
	if err != nil {
		return err
	}
	timesA, timesB, err := interleave(a, b, o.runs)
	if err != nil {
		return err
	}
	reportA, err := a.inspect()
	if err != nil {
		return err
	}
	reportB, err := b.inspect()
	if err != nil {
		return err
	}
	printReport(a, b, timesA, timesB, reportA, reportB)
	if o.note != "" {
		row, err := tableRow(root, o.note, timesA, timesB, reportB)
		if err != nil {
			return err
		}
		fmt.Println()
		fmt.Println(row)
	}
	return nil
}

// side is one of the two versions compared: the checkout its sources are in and the C1 that builds them.
type side struct {
	name     string
	tree     string
	compiler string
}

// output is where a build of the side writes its executable, inside the checkout, which is where its runtime/ is.
func (s side) output() string {
	return filepath.Join(s.tree, "bin", outputName+executableSuffix)
}

// prepare builds the two compilers. cleanup removes what prepare made, whether or not it succeeded.
func prepare(root string, o options) (a, b side, cleanup func(), err error) {
	b = side{name: "B (working tree)", tree: root}
	var undo []func()
	cleanup = func() {
		for _, u := range slices.Backward(undo) {
			u()
		}
	}
	undo = append(undo, func() { removeBuilt(b) })
	if b.compiler, err = buildC1(b); err != nil {
		return
	}
	if o.baseCompiler != "" {
		compiler, absErr := filepath.Abs(o.baseCompiler)
		if absErr != nil {
			err = absErr
			return
		}
		a = side{name: "A (" + o.baseCompiler + ")", tree: root, compiler: compiler}
		return
	}
	tree, remove, err := checkoutRevision(root, o.base)
	if remove != nil {
		undo = append(undo, remove)
	}
	if err != nil {
		return
	}
	a = side{name: "A (" + o.base + ")", tree: tree}
	if a.compiler, err = buildC1(a); err != nil {
		return
	}
	return
}

// buildC1 builds self/ of the side's checkout with the checkout's own seed, into its bin/.
func buildC1(s side) (string, error) {
	target, err := hosts.Target()
	if err != nil {
		return "", err
	}
	seed := hosts.SeedPath(target)
	if seed == "" {
		return "", fmt.Errorf("%s has no committed seed to build C1 with", target)
	}
	compiler := filepath.Join(s.tree, "bin", compilerName+executableSuffix)
	if err := os.MkdirAll(filepath.Dir(compiler), 0o755); err != nil {
		return "", err
	}
	fmt.Fprintf(os.Stderr, "building C1 of %s\n", s.name)
	if err := execute(s.tree, nil, filepath.Join(s.tree, seed), "build", "self", "-o", compiler); err != nil {
		return "", err
	}
	return compiler, nil
}

func removeBuilt(s side) {
	for _, name := range []string{compilerName, outputName} {
		path := filepath.Join(s.tree, "bin", name+executableSuffix)
		os.Remove(path)
		os.Remove(path + irSuffix)
	}
}

// checkoutRevision makes a detached worktree of the revision in a temporary directory and gives the function that removes it.
func checkoutRevision(root, revision string) (string, func(), error) {
	parent, err := os.MkdirTemp("", "selfab-")
	if err != nil {
		return "", nil, err
	}
	tree := filepath.Join(parent, "base")
	remove := func() {
		_ = execute(root, nil, "git", "worktree", "remove", "--force", tree)
		os.RemoveAll(parent)
	}
	if err := execute(root, nil, "git", "worktree", "add", "--detach", tree, revision); err != nil {
		os.RemoveAll(parent)
		return "", nil, err
	}
	return tree, remove, nil
}

// interleave times builds of both sides, one of each in turn with the order alternating, so that a drift of the machine's
// speed reaches both the same.
func interleave(a, b side, runs int) (timesA, timesB []time.Duration, err error) {
	fmt.Fprintln(os.Stderr, "warming up")
	for _, s := range []side{a, b} {
		if _, err = timeBuild(s); err != nil {
			return
		}
	}
	for i := range runs {
		first, second := &timesA, &timesB
		sides := []side{a, b}
		if i%2 == 1 {
			first, second = second, first
			sides = []side{b, a}
		}
		for j, times := range []*[]time.Duration{first, second} {
			elapsed, buildErr := timeBuild(sides[j])
			if buildErr != nil {
				err = buildErr
				return
			}
			*times = append(*times, elapsed)
		}
		fmt.Fprintf(os.Stderr, "run %d of %d: A %v, B %v\n", i+1, runs, timesA[i].Round(time.Millisecond), timesB[i].Round(time.Millisecond))
	}
	return
}

// timeBuild is the wall time of the side's compiler building self/ without the build cache.
func timeBuild(s side) (time.Duration, error) {
	start := time.Now()
	if err := execute(s.tree, nil, s.compiler, "build", "self", "--no-cache", "-o", s.output()); err != nil {
		return 0, err
	}
	return time.Since(start), nil
}

func tableRow(root, note string, timesA, timesB []time.Duration, b report) (string, error) {
	commit, err := capture(root, "git", "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	dirty, err := capture(root, "git", "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return "", err
	}
	commit = strings.TrimSpace(commit)
	if strings.TrimSpace(dirty) != "" {
		commit += "+"
	}
	medianA, medianB := median(timesA), median(timesB)
	low, high := ratioRange(timesA, timesB)
	return fmt.Sprintf("| %s | %s | %s | %d | %.0f | %.0f | %.3f | %.3f-%.3f | %.1f%% | %d | %d |",
		time.Now().Format("2006-01-02"), commit, note, len(timesB), milliseconds(medianA), milliseconds(medianB),
		float64(medianB)/float64(medianA), low, high, b.checkShare, b.bounds, b.emptySlots), nil
}
