package driver

import (
	"strings"
	"testing"

	"minimaxon/internal/cache"
)

// summariesOf compiles the project with a build cache of its own and returns each module's export summary by link path.
func summariesOf(t *testing.T, files map[string]string, jobs int) map[string]ModuleCache {
	t.Helper()
	project := writeProject(t, files)
	got := cachedBuild(t, project, t.TempDir(), "c1", jobs, nil)
	if got.errs != "" {
		t.Fatalf("the program does not compile:\n%s", got.errs)
	}
	summaries := map[string]ModuleCache{}
	for _, s := range got.stats.Modules {
		summaries[s.Module] = s
	}
	return summaries
}

// The modules of the summary tests: the root calls user, which calls the module under test, m, so that a change to m shows
// in the summary of every module that reaches it.
const (
	summaryMain = `function main() returns ExitCode
	print("{user.run()}\n")
	return 0
end 'main'
`
	summaryUser = `export function run() returns Count
	return m.area(2, h: 3)
end 'run'
`
)

// summaryModule is a module with no generic, no default and one exported constant, so what it shows is its types and its
// exported functions and constants; what it keeps to itself is a private function, a private constant, and every body that
// is not generic.
const summaryModule = `export type Point
	export var px as Count
	var py as Count
	export var pn as Small

	export static function make(px Count, py Count) returns Point
		return Point{px: px, py: py, pn: 1}
	end 'make'

	export function sum() returns Count
		return try px + py otherwise 0
	end 'sum'

	function hidden() returns Count
		return py
	end 'hidden'
end 'Point'

type Secret
	var sq as Count
end 'Secret'

export enum Color
	red
	green = 5
end 'Color'

export union Shape
	circle(radius Count)
	none
end 'Shape'

export typealias Small = int(0 to 9)

export interface Sized
	function size() returns Count
end 'Sized'

export let LIMIT = stash + 2

export function width() returns Small
	return 4
end 'width'

export function area(w Count, h Count) returns Count
	return try w * h otherwise 0
end 'area'

export function span(lo Count, hi Count) returns Count
	return try hi - lo otherwise 0
end 'span'

function helper(n Count) returns Count
	return try n + 1 otherwise 0
end 'helper'

let stash = 7
`

// summaryOpenModule has a generic and a field's default, so what it shows is all it declares.
const summaryOpenModule = `export type Box uses T
	export var item as T

	export static function make(consumes item T) returns Self
		return Self{item: item}
	end 'make'
end 'Box'

export type Config
	export var level = 3 as Count
end 'Config'

export function keep(consumes x T) uses T returns T
	return x
end 'keep'

export function plain() returns Count
	return 1
end 'plain'

// A comment above a function.
function secret(n Count) returns Count
	return n
end 'secret'

let hidden = 5

export function area(w Count, h Count) returns Count
	return try w * h otherwise 0
end 'area'
`

// edit is a change to a module's source: every occurrence of each text is replaced by the one beside it.
type edit [][2]string

func (e edit) apply(t *testing.T, source string) string {
	t.Helper()
	for _, r := range e {
		if !strings.Contains(source, r[0]) {
			t.Fatalf("the module has no %q", r[0])
		}
		source = strings.ReplaceAll(source, r[0], r[1])
	}
	return source
}

func summaryProject(module string) map[string]string {
	return map[string]string{"main.maxon": summaryMain, "user/user.maxon": summaryUser, "m/m.maxon": module}
}

// Each thing another module can depend on is in the summary of the module that declares it, and of every module that reaches
// that module: changing it changes the digest.
func TestEachThingAModuleShowsIsInItsSummary(t *testing.T) {
	cases := map[string]edit{
		"the name of an exported function":                   {{"width", "breadth"}},
		"the parameter of an exported function":              {{"span(lo Count, hi Count) returns Count\n\treturn try hi - lo otherwise 0", "span(lo Count, up Count) returns Count\n\treturn try up - lo otherwise 0"}},
		"the convention of a parameter":                      {{"span(lo Count, hi Count)", "span(consumes lo Count, hi Count)"}},
		"the return type of a function":                      {{"width() returns Small", "width() returns Count"}},
		"a function that comes to throw":                     {{"span(lo Count, hi Count) returns Count\n\treturn try hi - lo otherwise 0", "span(lo Count, hi Count) returns Count throws ArithmeticError\n\treturn try hi - lo"}},
		"an interface's method that comes to throw":          {{"\tfunction size() returns Count\n", "\tfunction size() returns Count throws ArithmeticError\n"}},
		"a function that stops being exported":               {{"export function width", "function width"}},
		"a function that is exported":                        {{"function helper", "export function helper"}},
		"an exported function added":                         {{"let stash = 7", "let stash = 7\n\nexport function more() returns Count\n\treturn 1\nend 'more'"}},
		"the signature of a method":                          {{"export function sum() returns Count", "export function sum(extra Count) returns Count"}},
		"the visibility of a method":                         {{"function hidden()", "export function hidden()"}},
		"a method added":                                     {{"\tfunction hidden()", "\texport function twice() returns Count\n\t\treturn try px * 2 otherwise 0\n\tend 'twice'\n\n\tfunction hidden()"}},
		"a method that is static":                            {{"export function sum() returns Count\n\t\treturn try px + py otherwise 0", "export static function sum() returns Count\n\t\treturn 1"}},
		"the visibility of a field":                          {{"var py as Count", "export var py as Count"}},
		"the mutability of a field":                          {{"export var px as Count", "export let px as Count"}},
		"the name of a field":                                {{"py", "pz"}},
		"the type of a field":                                {{"export var pn as Small", "export var pn as Count"}},
		"a field added":                                      {{"export var pn as Small\n", "export var pn as Small\n\texport var pw as Count\n"}, {"py: py, pn: 1}", "py: py, pn: 1, pw: 1}"}},
		"the type that a type implements":                    {{"export type Point\n", "export type Point implements Sized\n"}, {"\tfunction hidden()", "\texport function size() returns Count\n\t\treturn 1\n\tend 'size'\n\n\tfunction hidden()"}},
		"a type that is noncopyable":                         {{"export type Point\n", "export type Point is noncopyable\n"}},
		"the layout of a type that is private":               {{"var sq as Count", "var sq as Small"}},
		"the raw value of an enum case":                      {{"green = 5", "green = 6"}},
		"an enum case added":                                 {{"green = 5", "green = 5\n\tblue"}},
		"the payload of a union case":                        {{"circle(radius Count)", "circle(radius Small)"}},
		"the range of a typealias":                           {{"int(0 to 9)", "int(0 to 10)"}},
		"the signature of an interface method":               {{"function size() returns Count", "function size() returns Small"}},
		"the value of an exported constant":                  {{"LIMIT = stash + 2", "LIMIT = stash + 3"}},
		"the type of an exported constant":                   {{"LIMIT = stash + 2", "LIMIT = (stash + 2) as Small"}},
		"a private constant that an exported constant reads": {{"let stash = 7", "let stash = 8"}},
		"an exported constant that is private":               {{"export let LIMIT", "let LIMIT"}},
	}
	for name, e := range cases {
		t.Run(name, func(t *testing.T) {
			before := summariesOf(t, summaryProject(summaryModule), 1)
			after := summaryProject(e.apply(t, summaryModule))
			got := summariesOf(t, after, 1)
			if got["m"].Summary == before["m"].Summary {
				t.Errorf("m shows the same after the change")
			}
			for _, dependent := range []string{"user", "export"} {
				if got[dependent].Summary == before[dependent].Summary {
					t.Errorf("the module that reaches m, %q, has the same summary after the change", dependent)
				}
			}
		})
	}
}

// What nothing outside a module can see, or what another module only calls by name, is not in the summary: the module is
// rebuilt and nothing that reaches it is.
func TestWhatAModuleKeepsToItselfIsNotInItsSummary(t *testing.T) {
	cases := map[string]edit{
		"the body of an exported function":                  {{"return try w * h otherwise 0", "return try w * h + 1 otherwise 0"}},
		"the body of a method":                              {{"return try px + py otherwise 0", "return try px + py + 1 otherwise 0"}},
		"the body of a private method":                      {{"\t\treturn py\n\tend 'hidden'", "\t\treturn try py + 1 otherwise 0\n\tend 'hidden'"}},
		"the body of a private function":                    {{"return try n + 1 otherwise 0", "return try n + 2 otherwise 0"}},
		"the signature of a private function":               {{"helper(n Count) returns Count\n\treturn try n + 1 otherwise 0", "helper(k Count) returns Count\n\treturn try k + 1 otherwise 0"}},
		"a private function added":                          {{"let stash = 7", "let stash = 7\n\nfunction another() returns Count\n\treturn 2\nend 'another'"}},
		"a comment between declarations":                    {{"export enum Color", "// Colors.\nexport enum Color"}},
		"a comment in a body":                               {{"\treturn try w * h otherwise 0\n", "\t// The area.\n\treturn try w * h otherwise 0\n"}},
		"the body of the function width, which is exported": {{"\treturn 4\n", "\treturn 5\n"}},
	}
	before := summariesOf(t, summaryProject(summaryModule), 1)
	for name, e := range cases {
		t.Run(name, func(t *testing.T) {
			got := summariesOf(t, summaryProject(e.apply(t, summaryModule)), 1)
			for _, module := range []string{"m", "user", "export"} {
				if got[module].Summary != before[module].Summary {
					t.Errorf("the summary of %q changed", module)
				}
			}
		})
	}
}

// A module that has no exported constant keeps its constants to itself, and one that has shows all of them, since the value of
// an exported constant may be written with one that is not.
func TestAPrivateConstantIsShownOnlyByAModuleThatShowsConstants(t *testing.T) {
	without := strings.ReplaceAll(summaryModule, "export let LIMIT = stash + 2\n\n", "")
	for name, e := range map[string]edit{
		"the value of a private constant": {{"let stash = 7", "let stash = 8"}},
		"the name of a private constant":  {{"stash", "reserve"}},
		"a private constant added":        {{"let stash = 7", "let stash = 7\n\nlet spare = 1"}},
	} {
		t.Run(name, func(t *testing.T) {
			before := summariesOf(t, summaryProject(without), 1)
			got := summariesOf(t, summaryProject(e.apply(t, without)), 1)
			if got["m"].Summary != before["m"].Summary {
				t.Error("the summary changed")
			}
		})
	}
	before := summariesOf(t, summaryProject(summaryModule), 1)
	got := summariesOf(t, summaryProject(edit{{"let stash = 7", "let stash = 7\n\nlet spare = 1"}}.apply(t, summaryModule)), 1)
	if got["m"].Summary == before["m"].Summary {
		t.Error("a private constant that an exported constant could read is not in the summary")
	}
}

// A module with a generic or a field's default shows more: every function and constant it declares, and the source of the
// generics, the constants and the defaults, since a module that reaches it lowers them.
func TestAModuleThatHasGenericsShowsTheirSourceAndEverythingTheyCanUse(t *testing.T) {
	changes := map[string]edit{
		"the body of a generic function":                                {{"\treturn x\n", "\tlet y = x\n\treturn y\n"}},
		"the body of a generic method":                                  {{"return Self{item: item}", "let made = Self{item: item}\n\t\treturn made"}},
		"the default of a field":                                        {{"level = 3 as Count", "level = 4 as Count"}},
		"a comment in a generic":                                        {{"\treturn x\n", "\t// Keep it.\n\treturn x\n"}},
		"the signature of a private function that a generic could call": {{"function secret(n Count)", "function secret(k Count)"}, {"return n\nend 'secret'", "return k\nend 'secret'"}},
	}
	same := map[string]edit{
		"the body of a function that is not generic": {{"return 1\nend 'plain'", "return 2\nend 'plain'"}},
		"the body of a private function":             {{"return n\nend 'secret'", "return try n + 1 otherwise 0\nend 'secret'"}},
		"a comment above a function":                 {{"// A comment above a function.\n", "// Another comment.\n\n// And one more.\n"}},
		"the body of an exported function":           {{"return try w * h otherwise 0", "return try w * h + 1 otherwise 0"}},
	}
	project := func(module string) map[string]string {
		return map[string]string{"main.maxon": strings.Replace(summaryMain, "user.run()", "m.area(1, h: 2)", 1), "m/m.maxon": module}
	}
	before := summariesOf(t, project(summaryOpenModule), 1)
	for name, e := range changes {
		t.Run("changes "+name, func(t *testing.T) {
			if got := summariesOf(t, project(e.apply(t, summaryOpenModule)), 1); got["m"].Summary == before["m"].Summary || got["export"].Summary == before["export"].Summary {
				t.Error("the summary is the same")
			}
		})
	}
	for name, e := range same {
		t.Run("keeps "+name, func(t *testing.T) {
			if got := summariesOf(t, project(e.apply(t, summaryOpenModule)), 1); got["m"].Summary != before["m"].Summary || got["export"].Summary != before["export"].Summary {
				t.Error("the summary changed")
			}
		})
	}
}

// A summary is of the module's own text and what it reaches, not of where it is or how many modules compile at once.
func TestASummaryDoesNotDependOnHandlesOrJobs(t *testing.T) {
	files := summaryProject(summaryModule)
	want := summariesOf(t, files, 1)
	for _, jobs := range []int{2, 7} {
		got := summariesOf(t, files, jobs)
		for module, s := range want {
			if got[module] != s {
				t.Errorf("jobs=%d: the summary of %q differs", jobs, module)
			}
		}
	}
	// A module that sorts before every other moves their handles, and changes no summary of a module that does not reach it.
	files["aaa/aaa.maxon"] = "export function unrelated() returns Count\n\treturn 1\nend 'unrelated'\n"
	got := summariesOf(t, files, 2)
	for module, s := range want {
		if got[module] != s {
			t.Errorf("a module that nothing reaches changed the summary of %q", module)
		}
	}
	if _, ok := got["aaa"]; !ok {
		t.Error("the added module has no summary")
	}
}

// A module's summary says whether the modules that reach it read its check results: it does if the module has a generic, an
// exported constant, or a field's default, and not otherwise (a private constant is not read by another module).
func TestASummarySaysWhetherTheModuleIsReadThrough(t *testing.T) {
	plain := strings.ReplaceAll(summaryModule, "export let LIMIT = stash + 2\n\n", "")
	if got := summariesOf(t, summaryProject(plain), 1); got["m"].ReadThrough || got["user"].ReadThrough || got["export"].ReadThrough {
		t.Errorf("a module with no generic and no constant is read through: %+v", got)
	}
	if got := summariesOf(t, summaryProject(summaryModule), 1); !got["m"].ReadThrough {
		t.Error("a module with an exported constant is not read through")
	}
	if got := summariesOf(t, map[string]string{"main.maxon": strings.Replace(summaryMain, "user.run()", "m.area(1, h: 2)", 1), "m/m.maxon": summaryOpenModule}, 1); !got["m"].ReadThrough {
		t.Error("a module with generics is not read through")
	}
}

func TestTheFoldOfASummaryHoldsEveryDependencyInOrder(t *testing.T) {
	own, a, b := cache.Digest([]byte("own")), cache.Digest([]byte("a")), cache.Digest([]byte("b"))
	base := foldSummary(own, []cache.Key{a, b})
	if foldSummary(own, []cache.Key{a, b}) != base {
		t.Error("the same inputs give two digests")
	}
	for name, other := range map[string]cache.Key{
		"its own part":      foldSummary(a, []cache.Key{a, b}),
		"a dependency":      foldSummary(own, []cache.Key{a, own}),
		"their order":       foldSummary(own, []cache.Key{b, a}),
		"a missing one":     foldSummary(own, []cache.Key{a}),
		"an added one":      foldSummary(own, []cache.Key{a, b, b}),
		"no dependency":     foldSummary(own, nil),
		"a dependency that": foldSummary(own, []cache.Key{{}, {}}),
	} {
		if other == base {
			t.Errorf("a different %s gives the same digest", name)
		}
	}
}
