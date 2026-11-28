package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func testCandidates(interpret bool) []candidate {
	h := host{target: "x64-windows", exe: ".exe", seed: filepath.Join("seed", "minimaxon-x64-windows.exe")}.ownTargetOnly()
	return focusedCandidates(h, focusedOptions{interpret: interpret})
}

func chosenNames(t *testing.T, changed ...string) []string {
	t.Helper()
	run, _, _ := plan(testCandidates(false), changed, focusedOptions{})
	var names []string
	for _, p := range run {
		names = append(names, p.name)
	}
	return names
}

// Every rule names a step of the lists: a step that is renamed does not leave its rule behind to match nothing.
func TestEveryFocusedRuleNamesAStep(t *testing.T) {
	var names []string
	for _, c := range testCandidates(false) {
		names = append(names, c.name)
	}
	for name := range focusedRules {
		if !slices.Contains(names, name) {
			t.Errorf("the rule for %q names no step of the gate's lists: %s", name, strings.Join(names, ", "))
		}
	}
}

// The steps that a landing always runs are there whatever changed, and in the order of the lists.
func TestFocusedAlwaysRunsTheCoreSteps(t *testing.T) {
	got := chosenNames(t, "README.md")
	want := []string{"compiler from the seed", "envrun", "selftest", "runtime", "stdlib", "self", "layout", "specs", "corpus", "compiler reproduces itself"}
	if !slices.Equal(got, want) {
		t.Fatalf("a change of README.md chose %v, want %v", got, want)
	}
}

func TestFocusedChoosesGoStepsForGoChanges(t *testing.T) {
	for _, path := range []string{"cmd/gate/main.go", "internal/hosts/hosts.go", "tools/x.go", "go.mod"} {
		got := chosenNames(t, path)
		if !slices.Contains(got, "vet") || !slices.Contains(got, "Go tests") || slices.Contains(got, "behaviours") {
			t.Errorf("%s chose %v, want vet and Go tests, and no behaviours", path, got)
		}
	}
	if got := chosenNames(t, ".claude/skills/land/SKILL.md", "README.md"); slices.Contains(got, "vet") || slices.Contains(got, "Go tests") {
		t.Errorf("documents chose %v, want no Go step", got)
	}
}

func TestFocusedChoosesBehavioursForTheRuntimeAndTheBackEnd(t *testing.T) {
	for _, path := range []string{"runtime/sched.maxon", "self/backend/x64.maxon", "self/ir/ir.maxon", "self/interp/os.maxon"} {
		if got := chosenNames(t, path); !slices.Contains(got, "behaviours") {
			t.Errorf("%s chose %v, want behaviours", path, got)
		}
	}
	if got := chosenNames(t, "self/driver/test.maxon", "self/interpreter-notes.md"); slices.Contains(got, "behaviours") {
		t.Errorf("chose %v, want no behaviours", got)
	}
}

func TestFocusedChoosesBehavioursForATestThatUsesTheScheduler(t *testing.T) {
	t.Chdir(t.TempDir())
	write := func(path, text string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("tests/run/waits.maxon", "function main()\n\tsleep(10)\nend 'main'\n")
	write("tests/run/adds.maxon", "function main()\n\tprint(1)\nend 'main'\n")
	if got := chosenNames(t, "tests/run/waits.maxon"); !slices.Contains(got, "behaviours") {
		t.Errorf("a test that sleeps chose %v, want behaviours", got)
	}
	if got := chosenNames(t, "tests/run/adds.maxon", "tests/run/removed.maxon"); slices.Contains(got, "behaviours") {
		t.Errorf("a test that does not schedule, and a removed one, chose %v, want no behaviours", got)
	}
}

func TestFocusedChoosesTheRegenerationForTheConversionsInputs(t *testing.T) {
	for _, path := range []string{"cmd/convert/main.go", "self/driver/skips.tsv", "stdlib/bytes/bytes.maxon", "specs/ints.md"} {
		if got := chosenNames(t, path); !slices.Contains(got, specsRegenerates) {
			t.Errorf("%s chose %v, want the regeneration of the specs", path, got)
		}
	}
	if got := chosenNames(t, "self/driver/test.maxon"); slices.Contains(got, specsRegenerates) {
		t.Errorf("a change of the driver chose %v: only its skip table is read by the conversion", got)
	}
	got := chosenNames(t, "stdlib/bytes/bytes.maxon")
	if !slices.Contains(got, "library regenerates") {
		t.Errorf("a change of stdlib/ chose %v, want the check that it regenerates", got)
	}
	if got := chosenNames(t, "specs/ints.md"); slices.Contains(got, "library regenerates") {
		t.Errorf("a change of specs/ chose %v, want no check of stdlib/", got)
	}
}

// The regeneration runs before the spec step that replays its results.
func TestFocusedRegeneratesBeforeTheSpecStep(t *testing.T) {
	got := chosenNames(t, "specs/ints.md")
	if slices.Index(got, specsRegenerates) < 0 || slices.Index(got, specsRegenerates) > slices.Index(got, "specs") {
		t.Errorf("chose %v, want %q before \"specs\"", got, specsRegenerates)
	}
}

func TestFocusedLeavesOutWhatCIRuns(t *testing.T) {
	_, _, left := plan(testCandidates(false), []string{"README.md"}, focusedOptions{})
	for _, name := range []string{"fuzz", "debug info", "cache", "corpus on x64-linux", "compiles for arm64-macos", "self compiles for arm64"} {
		if !slices.Contains(left, name) {
			t.Errorf("%q is not left out: %v", name, left)
		}
	}
	general := generalized(left)
	if !slices.Contains(general, "corpus on <target>") || slices.Contains(general, "corpus on x64-linux") {
		t.Errorf("the steps that are for each target are not one: %v", general)
	}
}

// The spec step says whether it runs under the interpreter, by the paths that changed.
func TestFocusedInterpretsSpecsOnlyForTheirInputs(t *testing.T) {
	for path, want := range map[string]bool{"README.md": false, "runtime/x.maxon": false, "specs/ints.md": true, "stdlib/a/b.maxon": true, "cmd/convert/x.go": true, "self/interp/os.maxon": true, "self/driver/test.maxon": true} {
		if got := len(matching([]string{path}, interpretChange)) > 0; got != want {
			t.Errorf("%s: interprets %v, want %v", path, got, want)
		}
	}
}

// The words that make a program one that schedules are the compiler's own (self/driver/test.maxon).
func TestSchedulerWordsAreTheTestRunners(t *testing.T) {
	text, err := os.ReadFile(filepath.Join("..", "..", "self", "driver", "test.maxon"))
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range schedulerWords {
		if !strings.Contains(string(text), fmt.Sprintf("Word = %q", word)) {
			t.Errorf("self/driver/test.maxon has no `Word = %q`: schedulerWords no longer follows mentionsConcurrency", word)
		}
	}
}

func TestParseFocused(t *testing.T) {
	o, err := parseFocused([]string{"--focused", "--since=HEAD~5", "--maxon-specs=/m/specs", "--maxon-stdlib=/m/stdlib"})
	if err != nil || o.since != "HEAD~5" || o.maxonSpecs != "/m/specs" || o.maxonStdlib != "/m/stdlib" {
		t.Fatalf("got %+v, %v", o, err)
	}
	if _, err := parseFocused([]string{"--focused", "--behaviours"}); err == nil {
		t.Fatal("an argument that the focused gate does not take was accepted")
	}
}
