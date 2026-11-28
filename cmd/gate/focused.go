package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// The focused gate (`gate check --focused`, README "Tests", .claude/skills/land) is what a landing runs on its rebased tip,
// where CI runs the full gate on main. It is built from the gate's own step lists (checkSteps, the suites' steps and the
// proof that the compiler reproduces itself) and holds no list of steps of its own: focusedRules says, for each step by its
// name, whether the focused gate always runs it or only when a path that the change touches is one that the step reads.
// A step with no rule is left out and named as such (the cross-target lanes and compiles, fuzz, debug info, the cache). A
// test (focused_test.go) holds every name of the rules to a step of the lists, so a renamed step cannot leave a rule behind.

const (
	focusedFlag      = "--focused"
	sinceFlag        = "--since="
	maxonSpecsFlag   = "--maxon-specs="
	maxonStdlibFlag  = "--maxon-stdlib="
	maxonSpecsEnv    = "MAXON_SPECS"
	specsRegenerates = "specs regeneration"
)

// A trigger is a set of paths, the repository's from its root with slashes, that a step reads.
type trigger struct {
	// about says what the paths are, for the line that tells why a step was left out.
	about   string
	matches func(path string) bool
}

// under is the paths below the directories, or the files of that name.
func under(names ...string) trigger {
	return trigger{strings.Join(names, ", "), func(path string) bool {
		return slices.ContainsFunc(names, func(n string) bool { return strings.HasPrefix(path, n) })
	}}
}

// suffixed is the paths that end in one of the suffixes.
func suffixed(about string, suffixes ...string) trigger {
	return trigger{about, func(path string) bool {
		return slices.ContainsFunc(suffixes, func(s string) bool { return strings.HasSuffix(path, s) })
	}}
}

// either is the paths of any of the triggers.
func either(triggers ...trigger) trigger {
	var about []string
	for _, t := range triggers {
		about = append(about, t.about)
	}
	return trigger{strings.Join(about, ", "), func(path string) bool {
		return slices.ContainsFunc(triggers, func(t trigger) bool { return t.matches(path) })
	}}
}

// schedulerWords are the words of which a test program has one when it runs the scheduler, so that it runs under several
// worker counts: self/driver/test.maxon's `mentionsConcurrency` takes the same ones (whenWord and the others, which
// focused_test.go holds to this list).
var schedulerWords = []string{"when", "async", "sleep", "Scheduler", "parallel"}

// schedulerTest is a program or a test of the corpus (tests/) that has one of the words of a program that schedules behaviours.
var schedulerTest = trigger{"a program of tests/ that uses the scheduler", func(path string) bool {
	if !strings.HasPrefix(path, "tests/") || !strings.HasSuffix(path, ".maxon") {
		return false
	}
	text, err := os.ReadFile(path)
	return err == nil && slices.ContainsFunc(schedulerWords, func(w string) bool { return strings.Contains(string(text), w) })
}}

var (
	goChange         = either(under("internal/", "cmd/"), suffixed("any .go file", ".go"), trigger{"go.mod, go.sum", func(p string) bool { return p == "go.mod" || p == "go.sum" }})
	schedulerChange  = either(under("runtime/", "self/backend/", "self/ir/", "self/interp/"), schedulerTest)
	conversionChange = under("cmd/convert/", "self/driver/skips", "stdlib/", "specs/")
	interpretChange  = under("specs/", "stdlib/", "cmd/convert/", "self/interp/", "self/driver/")
	stdlibChange     = under("stdlib/")
)

// focusRule says when the focused gate runs a step: always, or when a changed path is one of the trigger's.
type focusRule struct {
	always bool
	when   trigger
}

var always = focusRule{always: true}

func when(t trigger) focusRule { return focusRule{when: t} }

// focusedRules are the steps of the gate's lists that the focused gate runs, by name, and when. The steps of the lists that
// are not here are left out.
var focusedRules = map[string]focusRule{
	"vet":                        when(goChange),
	"Go tests":                   when(goChange),
	"compiler from the seed":     always,
	"envrun":                     always,
	"layout":                     always,
	"self":                       always,
	"runtime":                    always,
	"stdlib":                     always,
	"selftest":                   always,
	"corpus":                     always,
	"specs":                      always,
	"compiler reproduces itself": always,
	"behaviours":                 when(schedulerChange),
	specsRegenerates:             when(conversionChange),
	"library regenerates":        when(stdlibChange),
}

// vetSystems are the systems whose Go the focused gate vets: the three that CI's lanes build for.
var vetSystems = []string{"windows", "linux", "darwin"}

// focusedOptions are how a focused gate runs.
type focusedOptions struct {
	since string
	// maxonSpecs is maxon's specs directory, which the regeneration of specs/ reads.
	maxonSpecs string
	// maxonStdlib is maxon's stdlib directory, for the check that stdlib/ regenerates as committed.
	maxonStdlib string
	// interpret is whether the spec step runs the tests under the interpreter too.
	interpret bool
}

// candidate is a step of the lists and what the focused gate makes of it.
type candidate struct {
	step
	rule focusRule
	// ruled is whether the focused gate has a rule for the step: one without is left out.
	ruled bool
}

// focusedCandidates are every step of the gate's lists, in the order the focused gate runs them: the steps of check, then
// the suites' steps (with the regeneration of the specs in front of the spec step), then the proof that the compiler
// reproduces itself.
func focusedCandidates(h host, o focusedOptions) []candidate {
	compiler := abs(h.compiler("minimaxon"))
	run := suiteRun{h: h, compiler: compiler, onlyHostTarget: true, plainSpecs: !o.interpret}
	var steps []step
	steps = append(steps, checkSteps(h, vetSystems)...)
	suiteSteps, _ := run.steps()
	for _, s := range suiteSteps {
		if s.name == "specs" {
			steps = append(steps, step{specsRegenerates, func() error { return regenerateSpecsReporting(h, o.maxonSpecs) }})
		}
		steps = append(steps, s.step)
	}
	steps = append(steps, chainStep(h))
	var all []candidate
	for _, s := range steps {
		rule, ok := focusedRules[s.name]
		all = append(all, candidate{s, rule, ok})
	}
	return all
}

// plannedStep is a step that a focused gate runs, and why.
type plannedStep struct {
	step
	why string
}

// plan chooses the steps from the candidates by the paths that changed. It gives what it runs, and the names of the steps
// with a rule that nothing changed calls for, each with what they would have needed, and of those with no rule.
func plan(all []candidate, changed []string, o focusedOptions) (run []plannedStep, notNeeded, leftOut []string) {
	for _, c := range all {
		switch {
		case !c.ruled:
			leftOut = append(leftOut, c.name)
		case c.rule.always:
			why := "always"
			if c.name == "specs" {
				why += specsInterpretWhy(changed, o.interpret)
			}
			run = append(run, plannedStep{c.step, why})
		default:
			if hit := matching(changed, c.rule.when); len(hit) > 0 {
				run = append(run, plannedStep{c.step, "changed: " + summarize(hit)})
			} else {
				notNeeded = append(notNeeded, c.name+" (needs a change under "+c.rule.when.about+")")
			}
		}
	}
	return run, notNeeded, leftOut
}

func specsInterpretWhy(changed []string, interpret bool) string {
	if !interpret {
		return ", without --interpret: nothing under " + interpretChange.about + " changed"
	}
	return ", with --interpret: changed " + summarize(matching(changed, interpretChange))
}

func matching(changed []string, t trigger) []string {
	return slices.DeleteFunc(slices.Clone(changed), func(p string) bool { return !t.matches(p) })
}

// summarize names the first of the paths and counts the rest.
func summarize(paths []string) string {
	const shown = 3
	if len(paths) <= shown {
		return strings.Join(paths, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(paths[:shown], ", "), len(paths)-shown)
}

// generalized is the names of the steps with a target's name written <target> and each name once, so that the steps that are
// for each of the targets are one in a list.
func generalized(names []string) []string {
	var out []string
	for _, n := range names {
		for _, t := range targets {
			n = strings.ReplaceAll(n, t, "<target>")
		}
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// parseFocused reads the arguments of `check --focused`.
func parseFocused(args []string) (focusedOptions, error) {
	o := focusedOptions{maxonSpecs: os.Getenv(maxonSpecsEnv), maxonStdlib: os.Getenv(maxonStdlibVariable)}
	for _, a := range args {
		switch {
		case a == focusedFlag:
		case strings.HasPrefix(a, sinceFlag):
			o.since = strings.TrimPrefix(a, sinceFlag)
		case strings.HasPrefix(a, maxonSpecsFlag):
			o.maxonSpecs = strings.TrimPrefix(a, maxonSpecsFlag)
		case strings.HasPrefix(a, maxonStdlibFlag):
			o.maxonStdlib = strings.TrimPrefix(a, maxonStdlibFlag)
		default:
			return o, fmt.Errorf("check --focused: no argument %q: it takes %s<rev>, %s<dir> and %s<dir>", a, sinceFlag, maxonSpecsFlag, maxonStdlibFlag)
		}
	}
	return o, nil
}

// changedPaths are the files that the change touches against the revision: those of its commits, the working tree's
// uncommitted changes and the files that git does not know yet, as the repository's paths with slashes. Without a revision it
// is the merge base with origin/main, which is what a rebased tip adds to main.
func changedPaths(since string) (string, []string, error) {
	if since == "" {
		out, err := output(nil, "", "git", "merge-base", "origin/main", "HEAD")
		if err != nil {
			return "", nil, fmt.Errorf("%s: name the revision the change is made on with %s<rev>, or fetch origin/main", out, sinceFlag)
		}
		since = strings.TrimSpace(out)
	}
	diff, err := output(nil, "", "git", "diff", "--name-only", "--no-renames", since)
	if err != nil {
		return since, nil, fmt.Errorf("%s%w", diff, err)
	}
	untracked, err := output(nil, "", "git", "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return since, nil, fmt.Errorf("%s%w", untracked, err)
	}
	var paths []string
	for _, line := range strings.Fields(diff + "\n" + untracked) {
		if !slices.Contains(paths, line) {
			paths = append(paths, line)
		}
	}
	slices.Sort(paths)
	return since, paths, nil
}

// mainCINote says what main's last finished CI run ended as, and for a red one that it is a note and not a stop: the owner's
// rule is that a landing goes ahead on a red main whose red has a known cause and an owner. A failure to ask is a note too.
func mainCINote() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "run", "list", "--branch", "main", "--status", "completed", "--limit", "1", "--json", "conclusion,headSha,workflowName,url")
	text, err := cmd.Output()
	var runs []struct {
		Conclusion   string `json:"conclusion"`
		HeadSha      string `json:"headSha"`
		WorkflowName string `json:"workflowName"`
		URL          string `json:"url"`
	}
	if err == nil {
		err = json.Unmarshal(text, &runs)
	}
	if err == nil && len(runs) == 0 {
		err = errors.New("no finished run")
	}
	if err != nil {
		fmt.Printf("NOTE: main's last finished CI run is not known (gh run list: %v): look it up before the report\n", err)
		return
	}
	r := runs[0]
	short := r.HeadSha
	if len(short) > 8 {
		short = short[:8]
	}
	if r.Conclusion == "success" {
		fmt.Printf("main's last finished CI run: %s %s (%s)\n", short, r.Conclusion, r.WorkflowName)
		return
	}
	fmt.Printf("NOTE: main is red: its last finished CI run is %s %s (%s, %s); a red main with a known owner does not stop a landing: name what is red and who has it in the report, and hold only if nobody owns it or this change touches what fails\n", short, r.Conclusion, r.WorkflowName, r.URL)
}

// regenerateSpecsReporting is `gate specs`, then the files it changed in specs/, which are the change's to commit.
func regenerateSpecsReporting(h host, maxonSpecs string) error {
	err := regenerateSpecs(h, maxonSpecs, false)
	if changes, _ := output(nil, "", "git", "status", "--short", "--", "specs"); strings.TrimSpace(changes) != "" {
		fmt.Printf("NOTE: the regeneration left these files of specs/ differing from the commit, which the landing carries:\n%s", changes)
	}
	return err
}

// focusedGate is `check --focused`: it chooses the steps from the paths that changed, runs them and says how long each took.
func focusedGate(h host, args []string) error {
	o, err := parseFocused(args)
	if err != nil {
		return err
	}
	// The other targets' executables are CI's, and a focused gate asks nothing of WSL to find that out.
	h = h.ownTargetOnly()
	pinHashSeed()
	mainCINote()
	since, changed, err := changedPaths(o.since)
	if err != nil {
		return err
	}
	o.interpret = len(matching(changed, interpretChange)) > 0
	chosen, notNeeded, leftOut := plan(focusedCandidates(h, o), changed, o)
	fmt.Printf("focused gate on %s since %s: %d changed paths\n", head(), shortRev(since), len(changed))
	for _, p := range chosen {
		fmt.Printf("  run  %-28s %s\n", p.name, p.why)
	}
	for _, n := range notNeeded {
		fmt.Printf("  skip %s\n", n)
	}
	fmt.Printf("left out, as CI runs them: %s\n", strings.Join(generalized(leftOut), ", "))
	if err := requireMaxon(chosen, o); err != nil {
		return err
	}
	var steps []step
	for _, p := range chosen {
		steps = append(steps, p.step)
	}
	err = runSteps(steps)
	printFocusedTimes()
	if err == nil {
		fmt.Println("focused gate passed")
	}
	return err
}

// requireMaxon refuses a run before it starts when a chosen step reads maxon's checkout and none is named: skipping the step
// would pass a gate that did not hold what the change touched.
func requireMaxon(chosen []plannedStep, o focusedOptions) error {
	needs := func(name string) bool {
		return slices.ContainsFunc(chosen, func(p plannedStep) bool { return p.name == name })
	}
	if needs(specsRegenerates) && !isDir(o.maxonSpecs) {
		return fmt.Errorf("the change calls for the regeneration of specs/: name maxon's specs directory with %s<dir> or %s (%q is not a directory)", maxonSpecsFlag, maxonSpecsEnv, o.maxonSpecs)
	}
	if needs("library regenerates") {
		if !isDir(o.maxonStdlib) {
			return fmt.Errorf("the change calls for the check that stdlib/ regenerates: name maxon's stdlib directory with %s<dir> or %s (%q is not a directory)", maxonStdlibFlag, maxonStdlibVariable, o.maxonStdlib)
		}
		// The step is the full gate's, which reads the directory from the environment.
		return os.Setenv(maxonStdlibVariable, o.maxonStdlib)
	}
	return nil
}

func head() string {
	out, err := output(nil, "", "git", "rev-parse", "--short=8", "HEAD")
	if err != nil {
		return "HEAD"
	}
	return strings.TrimSpace(out)
}

func shortRev(rev string) string {
	if len(rev) > 8 && !strings.ContainsAny(rev, "~^@:") {
		return rev[:8]
	}
	return rev
}

// printFocusedTimes ends the run with its total and slowest steps, and the time of each step in the order they ran.
func printFocusedTimes() {
	slowest := slices.Clone(stepTimes)
	slices.SortStableFunc(slowest, func(a, b stepTime) int { return int(b.elapsed - a.elapsed) })
	var top, all []string
	for i, s := range slowest {
		if i < 2 {
			top = append(top, fmt.Sprintf("%s %.1f min", s.name, s.elapsed.Minutes()))
		}
	}
	for _, s := range stepTimes {
		all = append(all, fmt.Sprintf("%s %.0f", s.name, s.elapsed.Seconds()))
	}
	fmt.Printf("gate time: total %.1f min; slowest: %s\n", sinceRun().Minutes(), strings.Join(top, ", "))
	fmt.Printf("step times (s): %s\n", strings.Join(all, "; "))
}
