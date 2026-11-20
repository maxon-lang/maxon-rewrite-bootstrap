package driver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The modules of the early cutoff tests. The root calls mid; mid calls core, which has no generic, and gen, which has a
// generic, a constant and a field with a default, so the modules are of every kind an edit can reach.
const (
	cutoffMain = `function main() returns ExitCode
	print("{mid.run()}\n")
	return 0
end 'main'
`
	cutoffMid = `export function run() returns Count
	let p = core.Point.make(2, y: 3)
	let c = gen.Config.make()
	return core.scale(p.sum()) + gen.keep(c.level) + gen.BASE
end 'run'
`
	cutoffCore = `export type Point
	export var x as Count
	export var y as Count

	export static function make(x Count, y Count) returns Point
		return Point{x: x, y: y}
	end 'make'

	export function sum() returns Count
		return x + y
	end 'sum'

	export function twice() returns Count
		return x * 2
	end 'twice'
end 'Point'

export typealias Small = int(0 to 99)

export function width() returns Small
	return 4
end 'width'

export function scale(n Count) returns Count
	return helper(n) * 2
end 'scale'

function helper(n Count) returns Count
	return n + 1
end 'helper'
`
	cutoffGen = `export type Config
	export var level = 3 as Count

	export static function make() returns Config
		return Config{}
	end 'make'
end 'Config'

export let BASE = 20

export function keep(consumes x T) uses T returns T
	return x
end 'keep'

export function plain() returns Count
	return 1
end 'plain'

function secret(n Count) returns Count
	return n
end 'secret'
`
)

func cutoffProject(t *testing.T) string {
	t.Helper()
	return writeProject(t, map[string]string{
		"main.maxon": cutoffMain, "mid/mid.maxon": cutoffMid, "core/core.maxon": cutoffCore, "gen/gen.maxon": cutoffGen,
	})
}

// A scenario is an edit to a module of the project, and what a build after it does with the cache: how many of the four
// modules (the root, core, gen and mid) come from the cache and how many checks it skips. The root depends on mid, mid on core
// and gen, and gen and core on nothing, so a change that other modules can see makes a miss of the module and of every
// module that reaches it.
type scenario struct {
	name             string
	file             string
	edit             edit
	hits, checkSkips int
}

func (s scenario) misses() int { return 4 - s.hits }

func TestAnEditThatNoOtherModuleCanSeeRebuildsOneModule(t *testing.T) {
	scenarios := []scenario{
		{"the body of an exported function of a module with no generic", "core/core.maxon", edit{{"return helper(n) * 2", "return helper(n) * 3"}}, 3, 3},
		{"the body of a method", "core/core.maxon", edit{{"return x + y", "return y + x"}}, 3, 3},
		{"a private helper's body", "core/core.maxon", edit{{"return n + 1", "return n + 2"}}, 3, 3},
		{"a private helper's signature", "core/core.maxon", edit{{"helper(n Count) returns Count\n\treturn n + 1", "helper(k Count) returns Count\n\treturn k + 1"}}, 3, 3},
		{"a comment in a function", "core/core.maxon", edit{{"\treturn helper(n) * 2\n", "\t// Twice.\n\treturn helper(n) * 2\n"}}, 3, 3},
		{"a function of a module that has a generic, which is not generic", "gen/gen.maxon", edit{{"return 1\nend 'plain'", "return 2\nend 'plain'"}}, 3, 3},
		{"a comment above a generic function", "gen/gen.maxon", edit{{"export function keep", "// Keeps a value.\nexport function keep"}}, 3, 3},
		{"the body of the module that calls the others", "mid/mid.maxon", edit{{"gen.BASE", "gen.BASE + 1"}}, 3, 2},
		{"the body of the root", "main.maxon", edit{{"print(\"{mid.run()}\\n\")", "print(\"{mid.run() + 1}\\n\")"}}, 3, 2},
	}
	runScenarios(t, scenarios)
}

func TestAnEditThatOtherModulesCanSeeRebuildsEveryModuleThatReachesIt(t *testing.T) {
	scenarios := []scenario{
		// core is reached by mid and the root.
		{"an exported function's parameter", "core/core.maxon", edit{{"scale(n Count) returns Count\n\treturn helper(n)", "scale(m Count) returns Count\n\treturn helper(m)"}}, 1, 0},
		{"an exported function's return type", "core/core.maxon", edit{{"width() returns Small", "width() returns Count"}}, 1, 0},
		{"an export added", "core/core.maxon", edit{{"function helper", "export function more() returns Count\n\treturn 1\nend 'more'\n\nfunction helper"}}, 1, 0},
		{"a field added to a type", "core/core.maxon", edit{{"export var y as Count\n", "export var y as Count\n\texport var z as Count\n"}, {"Point{x: x, y: y}", "Point{x: x, y: y, z: 1}"}}, 1, 0},
		{"a method's signature", "core/core.maxon", edit{{"export function twice() returns Count", "export function twice(extra Count) returns Count"}}, 1, 0},
		{"the visibility of a field", "core/core.maxon", edit{{"export var y as Count", "var y as Count"}}, 1, 0},
		// gen is reached by mid and the root, and shows its generic, its constant and its default.
		{"the body of a generic function", "gen/gen.maxon", edit{{"\treturn x\n", "\tlet y = x\n\treturn y\n"}}, 1, 1},
		{"a constant's value", "gen/gen.maxon", edit{{"BASE = 20", "BASE = 21"}}, 1, 1},
		{"a field's default", "gen/gen.maxon", edit{{"level = 3 as Count", "level = 4 as Count"}}, 1, 1},
		{"a comment in a generic", "gen/gen.maxon", edit{{"\treturn x\n", "\t// Keep it.\n\treturn x\n"}}, 1, 1},
		// A module that shows its generics shows what they could call: a helper's signature is shown, since a generic may
		// call it.
		{"the signature of a private function that a generic could call", "gen/gen.maxon", edit{{"secret(n Count) returns Count\n\treturn n", "secret(k Count) returns Count\n\treturn k"}}, 1, 1},
	}
	runScenarios(t, scenarios)
}

// runScenarios builds the project, edits it and builds it again, for each scenario, at one job and at several; the result is
// that of a build with no cache, and the cache did what the scenario says.
func runScenarios(t *testing.T, scenarios []scenario) {
	t.Helper()
	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			for _, jobs := range []int{1, 4} {
				project, dir := cutoffProject(t), t.TempDir()
				cold := cachedBuild(t, project, dir, "c1", jobs, nil)
				requireStats(t, "cold", cold.stats, 0, 4, 0)
				warm := cachedBuild(t, project, dir, "c1", jobs, nil)
				requireStats(t, "warm", warm.stats, 4, 0, 4)
				text, err := os.ReadFile(filepath.Join(project, filepath.FromSlash(s.file)))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(project, filepath.FromSlash(s.file)), []byte(s.edit.apply(t, string(text))), 0o644); err != nil {
					t.Fatal(err)
				}
				got := cachedBuild(t, project, dir, "c1", jobs, nil)
				if got.errs != "" {
					t.Fatalf("jobs=%d: the edited program does not compile:\n%s", jobs, got.errs)
				}
				requireStats(t, "edited", got.stats, s.hits, s.misses(), s.checkSkips)
				requireSameAsWithoutCache(t, project, got, jobs)
				if strings.Contains(s.name, "comment") {
					continue
				}
				requireStats(t, "edited, again", cachedBuild(t, project, dir, "c1", jobs, nil).stats, 4, 0, 4)
			}
		})
	}
}

// An export that is added and then taken away leaves the summaries as they were, so the entries of the modules that reach it,
// which the build before the addition wrote, are found again.
func TestAnExportThatIsAddedAndRemovedIsCutOffAgain(t *testing.T) {
	project, dir := cutoffProject(t), t.TempDir()
	cachedBuild(t, project, dir, "c1", 2, nil)
	add := edit{{"function helper", "export function more() returns Count\n\treturn 1\nend 'more'\n\nfunction helper"}}
	undo := edit{{"export function more() returns Count\n\treturn 1\nend 'more'\n\nfunction helper", "function helper"}}
	for _, step := range []struct {
		what          string
		edit          edit
		hits, skipped int
	}{{"added", add, 1, 0}, {"removed", undo, 4, 4}} {
		path := filepath.Join(project, "core", "core.maxon")
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(step.edit.apply(t, string(text))), 0o644); err != nil {
			t.Fatal(err)
		}
		got := cachedBuild(t, project, dir, "c1", 2, nil)
		requireStats(t, "the export "+step.what, got.stats, step.hits, 4-step.hits, step.skipped)
		requireSameAsWithoutCache(t, project, got, 2)
	}
}

// A module that depends on a module that changed so that it no longer compiles is a miss and is checked, whether the change
// is one that its own check finds (a label, a visibility) or one that the front finds (a name that is no longer exported):
// the errors are those of a build with no cache, and no executable is made.
func TestAChangeThatBreaksAnUnchangedModuleReportsTheErrorsOfABuildWithNoCache(t *testing.T) {
	breaks := map[string]struct {
		file string
		edit edit
	}{
		"a parameter that is renamed, which a call names":           {"core/core.maxon", edit{{"make(x Count, y Count)", "make(x Count, z Count)"}, {"Point{x: x, y: y}", "Point{x: x, y: z}"}}},
		"a field that is no longer exported, which another reads":   {"gen/gen.maxon", edit{{"export var level", "var level"}}},
		"a function that is no longer exported, which one calls":    {"core/core.maxon", edit{{"export function scale", "function scale"}}},
		"a function that is removed, which another calls":           {"gen/gen.maxon", edit{{"export function keep(consumes x T) uses T returns T\n\treturn x\nend 'keep'\n\n", ""}}},
		"a generic that no longer takes what a module gives it":     {"gen/gen.maxon", edit{{"keep(consumes x T) uses T returns T", "keep(consumes x T, extra T) uses T returns T"}}},
		"a method that is no longer there, which a module calls":    {"core/core.maxon", edit{{"\texport function sum() returns Count\n\t\treturn x + y\n\tend 'sum'\n", ""}}},
		"a type that no longer has a field a module's literal sets": {"gen/gen.maxon", edit{{"export var level = 3 as Count\n\n", ""}}},
	}
	for name, b := range breaks {
		t.Run(name, func(t *testing.T) {
			for _, jobs := range []int{1, 4} {
				project, dir := cutoffProject(t), t.TempDir()
				cachedBuild(t, project, dir, "c1", jobs, nil)
				path := filepath.Join(project, filepath.FromSlash(b.file))
				text, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(b.edit.apply(t, string(text))), 0o644); err != nil {
					t.Fatal(err)
				}
				got := cachedBuild(t, project, dir, "c1", jobs, nil)
				if got.errs == "" || got.exe != nil {
					t.Fatalf("jobs=%d: the program that no longer compiles gave an executable, errors %q", jobs, got.errs)
				}
				requireSameAsWithoutCache(t, project, got, jobs)
			}
		})
	}
}

// requireChecked refuses a compile in which any modules but these, by link path, were checked.
func requireChecked(t *testing.T, what string, got CacheStats, checked ...string) {
	t.Helper()
	var actual []string
	for _, m := range got.Modules {
		if m.Checked {
			actual = append(actual, m.Module)
		}
	}
	if strings.Join(actual, " ") != strings.Join(checked, " ") {
		t.Errorf("%s: the checks that ran are of %v, want %v", what, actual, checked)
	}
}

// A hit that a miss reaches is checked only if the miss reads its check results, which a module that declares a generic, an
// exported constant or a field's default lets it do: a miss is checked, a module that nothing that is built reaches is not,
// and one that is reached and shows nothing is not either, however many modules lie between.
func TestAHitThatAMissReachesIsCheckedOnlyIfItShowsItsCheckResults(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		edit    edit
		checked []string
	}{
		{"nothing is built", "", nil, nil},
		{"core is built, and reaches nothing", "core/core.maxon", edit{{"return n + 1", "return n + 2"}}, []string{"core"}},
		{"gen is built, and reaches nothing", "gen/gen.maxon", edit{{"return 1\nend 'plain'", "return 2\nend 'plain'"}}, []string{"gen"}},
		{"mid is built, and reaches core, which shows nothing, and gen, which shows its generic", "mid/mid.maxon", edit{{"gen.BASE", "gen.BASE + 1"}}, []string{"gen", "mid"}},
		{"the root is built, and reaches gen through mid, which shows nothing", "main.maxon", edit{{"mid.run()", "mid.run() + 1"}}, []string{"export", "gen"}},
		{"core's summary changed, so mid and the root are built and reach gen", "core/core.maxon", edit{{"export function scale", "export function more() returns Count\n\treturn 1\nend 'more'\n\nexport function scale"}}, []string{"export", "core", "gen", "mid"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			project, dir := cutoffProject(t), t.TempDir()
			cachedBuild(t, project, dir, "c1", 2, nil)
			if c.file != "" {
				path := filepath.Join(project, filepath.FromSlash(c.file))
				text, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(c.edit.apply(t, string(text))), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got := cachedBuild(t, project, dir, "c1", 2, nil)
			requireChecked(t, c.name, got.stats, c.checked...)
			requireSameAsWithoutCache(t, project, got, 2)
		})
	}
}
