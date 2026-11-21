package driver

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"minimaxon/internal/interp"
	"minimaxon/internal/par"
	"minimaxon/internal/project"
	"sync"
)

func buildProject(t *testing.T, project string, jobs int) (exe, ir []byte) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.exe")
	if errs := Compile(project, out, Options{Jobs: jobs, EmitIr: true}); len(errs) > 0 {
		t.Fatalf("jobs=%d: %v", jobs, errs)
	}
	exe, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	ir, err = os.ReadFile(out + ".ir")
	if err != nil {
		t.Fatal(err)
	}
	return exe, ir
}

// requireSameForAnyJobCount compiles a project with several numbers of jobs and refuses a difference in the
// executable or the IR. It returns what jobs=1 made.
func requireSameForAnyJobCount(t *testing.T, project string) (exe, ir []byte) {
	t.Helper()
	exe1, ir1 := buildProject(t, project, 1)
	for _, jobs := range []int{2, 5, 16} {
		exe, ir := buildProject(t, project, jobs)
		if !bytes.Equal(exe, exe1) {
			t.Errorf("the executable differs between jobs=1 and jobs=%d", jobs)
		}
		if !bytes.Equal(ir, ir1) {
			t.Errorf("the IR differs between jobs=1 and jobs=%d", jobs)
		}
	}
	return exe1, ir1
}

// The executable and the IR of a program with generics in several modules do not depend on how many modules compile
// at once: instances are found in one serial phase, in source order.
func TestGenericProgramIsIdenticalForAnyJobCount(t *testing.T) {
	_, ir := requireSameForAnyJobCount(t, filepath.Join("..", "..", "tests", "modules", "generics"))
	if !bytes.Contains(ir, []byte("function total[Square]")) {
		t.Error("the IR has no instance of total")
	}
}

// The same holds for arrays, whose runtime is a unit of its own that the link adds when a program calls it.
func TestArrayProgramIsIdenticalForAnyJobCount(t *testing.T) {
	_, ir := requireSameForAnyJobCount(t, filepath.Join("..", "..", "tests", "modules", "arrays"))
	if !bytes.Contains(ir, []byte("runtime rt.array.elem(")) {
		t.Error("the IR does not address an element through the runtime")
	}
	if !bytes.Contains(ir, []byte("function firstOr[Count]")) {
		t.Error("the IR has no instance of firstOr")
	}
}

// The array runtime is linked only into a program that uses an array, so every other executable is what it was.
func TestOnlyAnArrayProgramHoldsTheArrayRuntime(t *testing.T) {
	const text = "index out of range"
	arrays, _ := buildProject(t, filepath.Join("..", "..", "tests", "run", "arrays.maxon"), 1)
	if !bytes.Contains(arrays, []byte(text)) {
		t.Errorf("a program that uses an array lacks %q", text)
	}
	plain, _ := buildProject(t, filepath.Join("..", "..", "tests", "run", "records.maxon"), 1)
	if bytes.Contains(plain, []byte(text)) {
		t.Errorf("a program with no array holds %q", text)
	}
}

// A map, a list and a pool run only under the interpreter for now: compiling one is an error that names where it is
// used, the same one for any number of jobs, and checking the program is not.
func TestInterpreterOnlyPrimitivesAreRefusedBeforeCodeGeneration(t *testing.T) {
	program := filepath.Join("..", "..", "tests", "errors", "native-pool-element.maxon")
	for _, jobs := range []int{1, 4} {
		errs := Compile(program, filepath.Join(t.TempDir(), "out.exe"), Options{Jobs: jobs})
		if len(errs) != 1 || errs[0].Code != diag.ErrInterpreterOnly || errs[0].Line != 3 {
			t.Errorf("jobs=%d: got %v", jobs, errs)
		}
	}
	if errs := Compile(program, "", Options{Jobs: 1}); len(errs) != 0 {
		t.Errorf("checking the program: %v", errs)
	}
}

// The drops that lowering emits where an owner dies are part of the IR, so they are the same for any number of jobs too.
func TestDropsAreIdenticalForAnyJobCount(t *testing.T) {
	_, ir := requireSameForAnyJobCount(t, filepath.Join("..", "..", "tests", "modules", "basic"))
	for _, want := range []string{"dropslot s", "drop %", "= move s"} {
		if !bytes.Contains(ir, []byte(want)) {
			t.Errorf("the IR has no %q", want)
		}
	}
}

// A program with a map, a list and a pool runs under the interpreter, which finds no violation of the memory model.
func TestInterpretRunsAProgramThatNeedsAMap(t *testing.T) {
	result, errs := Interpret(filepath.Join("..", "..", "tests", "run", "lru-cache.maxon"), 1, interp.DefaultStepLimit)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if len(result.Violations) > 0 || result.ExitCode != 0 || !bytes.HasPrefix(result.Stdout, []byte("1 -> one\n2 missing\n")) {
		t.Errorf("exit %d, violations %v, stdout %q", result.ExitCode, result.Violations, result.Stdout)
	}
}

// Interpreting a program that does not compile reports the errors and runs nothing.
func TestInterpretReportsCompileErrors(t *testing.T) {
	result, errs := Interpret(filepath.Join("..", "..", "tests", "errors", "let-reassigned.maxon"), 1, interp.DefaultStepLimit)
	if result != nil || len(errs) == 0 {
		t.Errorf("result %v, errors %v", result, errs)
	}
}

// A behaviour is lowered to a function of its own, which is part of the IR, so the IR is the same for any number of jobs
// too; the compile is refused, as only the interpreter runs it.
func TestBehavioursAreIdenticalForAnyJobCount(t *testing.T) {
	project := filepath.Join("..", "..", "tests", "modules", "behaviours")
	var first []byte
	for _, jobs := range []int{1, 2, 5, 16} {
		out := filepath.Join(t.TempDir(), "out.exe")
		errs := Compile(project, out, Options{Jobs: jobs, EmitIr: true})
		if len(errs) == 0 || errs[0].Code != diag.ErrInterpreterOnly {
			t.Fatalf("jobs=%d: got %v", jobs, errs)
		}
		ir, err := os.ReadFile(out + ".ir")
		if err != nil {
			t.Fatal(err)
		}
		if first == nil {
			first = ir
			for _, want := range []string{"function credit$when0", "when credit$when0 cowns [", "function report$when0"} {
				if !bytes.Contains(ir, []byte(want)) {
					t.Errorf("the IR has no %q", want)
				}
			}
		} else if !bytes.Equal(ir, first) {
			t.Errorf("the IR differs between jobs=1 and jobs=%d", jobs)
		}
	}
}

// A program is compiled once and run under many schedules, and a program whose behaviours the cowns put in order gives one
// output under all of them.
func TestPreparedProgramRunsUnderManySchedules(t *testing.T) {
	program, errs := Prepare(filepath.Join("..", "..", "tests", "run", "bank-transfers.maxon"), 1)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	for seed := range int64(40) {
		result := interp.RunWith(program, interp.Options{StepLimit: interp.DefaultStepLimit, Seed: seed})
		if len(result.Violations) > 0 || result.Behaviours != 6 || string(result.Stdout) != "a 55 b 5 c 90 total 150\n" {
			t.Fatalf("seed %d: %v, %d behaviours, %q", seed, result.Violations, result.Behaviours, result.Stdout)
		}
	}
}

// Counting allocations is a flag: the executable of a program is built with the count and the check only when it is asked
// for, so a normal executable pays nothing for it, and the counting one still gives the program's own result.
func TestOnlyAnExecutableBuiltToCountHoldsTheCount(t *testing.T) {
	const text = "leaked allocations"
	program := filepath.Join("..", "..", "tests", "run", "records.maxon")
	plain := filepath.Join(t.TempDir(), "plain.exe")
	counting := filepath.Join(t.TempDir(), "counting.exe")
	for exe, count := range map[string]bool{plain: false, counting: true} {
		if errs := Compile(program, exe, Options{Jobs: 1, CountAllocations: count}); len(errs) > 0 {
			t.Fatal(errs)
		}
	}
	for exe, want := range map[string]bool{plain: false, counting: true} {
		data, err := os.ReadFile(exe)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(text)) != want {
			t.Errorf("%s: holds %q is %v, want %v", filepath.Base(exe), text, !want, want)
		}
	}
	out, err := exec.Command(counting).Output()
	if err != nil {
		t.Fatalf("the counting executable fails: %v", err)
	}
	want, err := exec.Command(plain).Output()
	if err != nil || !bytes.Equal(out, want) {
		t.Errorf("the two executables give different output: %q and %q (%v)", out, want, err)
	}
}

// The link keeps only the code and the data that the entry reaches, so a function that nothing calls, the instance only
// it needs, and the strings only they use are compiled but not in the executable.
func TestTheLinkDropsWhatTheEntryDoesNotReach(t *testing.T) {
	exe, ir := requireSameForAnyJobCount(t, filepath.Join("..", "..", "tests", "modules", "dead-code"))
	if !bytes.Contains(ir, []byte("function keep[")) {
		t.Error("the IR has no instance of keep: it should still be compiled")
	}
	for _, text := range []string{"dead main literal", "dead lib literal"} {
		if bytes.Contains(exe, []byte(text)) {
			t.Errorf("the executable holds %q", text)
		}
	}
}

// A compile with a trace path writes a Go execution trace there, which `go tool trace` opens.
func TestTraceWritesAnExecutionTrace(t *testing.T) {
	dir := t.TempDir()
	trace := filepath.Join(dir, "compile.trace")
	project := filepath.Join("..", "..", "tests", "modules", "generics")
	if errs := Compile(project, filepath.Join(dir, "out.exe"), Options{Jobs: 4, Trace: trace}); len(errs) > 0 {
		t.Fatal(errs)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("go 1.")) {
		t.Errorf("the trace does not start with Go's trace header: %q", data[:min(len(data), 16)])
	}
}

// writeProject writes each file of a project, named by its path under the project directory, and returns the directory.
func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const (
	useAfterMoveMain = `typealias Score = int(i64.min to i64.max)

type Box
	export var v as Score

	static function create(v Score) returns Box
		return Box{v: v}
	end 'create'
end 'Box'

function main() returns ExitCode
	var a = Box.create(1)
	let b = a
	print("{b.v} {a.v}\n")
	a = Box.create(3)
	print("{a.v}\n")
	return 0
end 'main'
`
	bodyErrorModule = `export function bad() returns String
	return 1
end 'bad'
`
)

// Bodies and verify run as one pass, so a module's verify can run while another module's bodies have errors. The program
// gets the body errors alone, whichever worker got there first, because verify is a stage after bodies, not beside them.
func TestBodyErrorsHideVerifyErrorsOfOtherModules(t *testing.T) {
	verifyOnly := writeProject(t, map[string]string{"main.maxon": useAfterMoveMain})
	for _, jobs := range []int{1, 4} {
		errs := Compile(verifyOnly, "", Options{Jobs: jobs})
		if len(errs) != 1 || errs[0].Code != "E3102" {
			t.Fatalf("jobs=%d: the program with only the ownership error got %v", jobs, errs)
		}
	}
	both := writeProject(t, map[string]string{"main.maxon": useAfterMoveMain, "broken/broken.maxon": bodyErrorModule})
	for _, jobs := range []int{1, 2, 4, 16} {
		for range 20 {
			errs := Compile(both, "", Options{Jobs: jobs})
			if len(errs) != 1 || errs[0].Code != "E3005" || filepath.Base(errs[0].File) != "broken.maxon" {
				t.Fatalf("jobs=%d: got %v, want the body error of the other module alone", jobs, errs)
			}
		}
	}
}

// A module with a syntax error stops the compile before the declaration tables count: a duplicate declaration in another
// module is not reported beside it.
func TestSyntaxErrorHidesDuplicateDeclarationsOfOtherModules(t *testing.T) {
	const duplicate = "typealias Apples = int(0 to 1)\n\ntypealias Apples = int(0 to 2)\n"
	const syntax = "function g(\n"
	dup := writeProject(t, map[string]string{"main.maxon": duplicate})
	if errs := Compile(dup, "", Options{Jobs: 1}); len(errs) != 1 || errs[0].Code != diag.ErrDuplicateDefinition {
		t.Fatalf("the program with only a duplicate got %v", errs)
	}
	both := writeProject(t, map[string]string{"main.maxon": duplicate, "broken/broken.maxon": syntax})
	for _, jobs := range []int{1, 4} {
		errs := Compile(both, "", Options{Jobs: jobs})
		if len(errs) != 1 || filepath.Base(errs[0].File) != "broken.maxon" {
			t.Errorf("jobs=%d: got %v, want the syntax error alone", jobs, errs)
		}
	}
}

const (
	instanceLibrary = `export type Box uses T
	export var item as T

	export static function make(consumes item T) returns Self
		return Self{item: item}
	end 'make'
end 'Box'

export function wrap(consumes x T) uses T returns Box with T
	return Box.make(x)
end 'wrap'

export function deep(consumes x T) uses T returns Box with T
	return wrap(x)
end 'deep'
`
	instanceUser = `export function userRun() returns Count
	let b = lib.deep(3 as Count)
	return b.item
end 'userRun'
`
	instanceOther = `export function otherRun() returns Count
	let a = lib.deep(4 as Count)
	let b = lib.deep("text")
	print("{b.item}\n")
	return a.item
end 'otherRun'
`
	instanceMain = `function main() returns ExitCode
	print("{user.userRun()}\n")
	return 0
end 'main'
`
	instanceMainWithOther = `function main() returns ExitCode
	print("{try user.userRun() + other.otherRun() otherwise 0}\n")
	return 0
end 'main'
`
)

// irFrom is the IR text from the first function whose header starts with the prefix to the end, which is one module's
// IR when that module is the last in module order.
func irFrom(t *testing.T, ir []byte, prefix string) string {
	t.Helper()
	text := string(ir)
	at := strings.Index(text, prefix)
	if at < 0 {
		t.Fatalf("the IR has no %q", prefix)
	}
	return text[at:]
}

// A module's instances are the ones it needs, found from its own functions through the generics it reaches. They do not
// depend on which other modules exist or what they use: the module `user` is the last in module order, so its IR is the
// end of the program's, and it is the same with another module that uses the same generics at other types.
func TestAModulesInstancesDoNotDependOnOtherModules(t *testing.T) {
	alone := writeProject(t, map[string]string{
		"main.maxon": instanceMain, "lib/lib.maxon": instanceLibrary, "user/user.maxon": instanceUser,
	})
	crowded := writeProject(t, map[string]string{
		"main.maxon": instanceMainWithOther, "lib/lib.maxon": instanceLibrary, "user/user.maxon": instanceUser,
		"other/other.maxon": instanceOther,
	})
	for _, jobs := range []int{1, 4} {
		_, aloneIr := buildProject(t, alone, jobs)
		_, crowdedIr := buildProject(t, crowded, jobs)
		got := irFrom(t, crowdedIr, "function userRun params")
		want := irFrom(t, aloneIr, "function userRun params")
		if got != want {
			t.Errorf("jobs=%d: the IR of the module differs when another module uses the generics:\n%s\nwant\n%s", jobs, got, want)
		}
		for _, instance := range []string{"function deep[Count] params", "function wrap[Count] params", "function (Box with Count).make params"} {
			if !strings.Contains(want, instance) {
				t.Errorf("jobs=%d: the module's IR has no %q:\n%s", jobs, instance, want)
			}
		}
		if strings.Contains(want, "[String]") {
			t.Errorf("jobs=%d: the module has an instance at a type that only another module uses", jobs)
		}
	}
}

// Two modules that need the same instance each lower it, and the link keeps one copy: the program has it once, whichever
// number of jobs lowered the modules.
func TestAnInstanceTwoModulesNeedIsInTheExecutableOnce(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"main.maxon": instanceMainWithOther, "lib/lib.maxon": instanceLibrary, "user/user.maxon": instanceUser,
		"other/other.maxon": instanceOther,
	})
	exe, ir := requireSameForAnyJobCount(t, dir)
	if got := bytes.Count(ir, []byte("function deep[Count] params")); got != 2 {
		t.Errorf("the IR lists deep[Count] %d times, once for each module that needs it", got)
	}
	if got := bytes.Count(ir, []byte("function deep[String] params")); got != 1 {
		t.Errorf("the IR lists deep[String] %d times, want 1", got)
	}
	out := filepath.Join(t.TempDir(), "run.exe")
	if err := os.WriteFile(out, exe, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := exec.Command(out).Output(); err != nil || string(got) != "text\n7\n" {
		t.Errorf("the program printed %q (%v), want text and 7", got, err)
	}
}

// Every module that reaches a generic that grows its type argument without end finds the runaway for itself, and the
// program still gets one error at the call that asks for the too deep instance, for any number of jobs.
func TestARunawayGenericIsReportedOnceForAnyJobCount(t *testing.T) {
	project := filepath.Join("..", "..", "tests", "modules", "generic-too-deep-in-two-modules")
	for _, output := range []string{"", filepath.Join(t.TempDir(), "out.exe")} {
		for _, jobs := range []int{1, 2, 5, 16} {
			for range 10 {
				errs := Compile(project, output, Options{Jobs: jobs})
				if len(errs) != 1 || errs[0].Code != diag.ErrInstantiationTooDeep || filepath.Base(errs[0].File) != "lib.maxon" || errs[0].Line != 13 {
					t.Fatalf("output=%q jobs=%d: got %v, want the one E3906 at the recursive call in lib.maxon", output, jobs, errs)
				}
			}
		}
	}
}

// chainDeps are the edges of a program whose modules each depend on the next, and the first also on the last, like a
// program whose main calls a module that calls a module that holds a generic.
func chainDeps(modules int) *project.Graph {
	results := make([]ast.ModuleResult, modules)
	for i := range modules - 1 {
		results[i].Deps = append(results[i].Deps, ast.ModuleDep{Module: ast.ModuleID(i + 1)})
	}
	if modules > 2 {
		results[0].Deps = append(results[0].Deps, ast.ModuleDep{Module: ast.ModuleID(modules - 1)})
	}
	return project.NewGraph(results)
}

// reaches is every module that the module depends on, directly or not.
func reaches(graph *project.Graph, from ast.ModuleID) map[ast.ModuleID]bool {
	seen := map[ast.ModuleID]bool{}
	var visit func(m ast.ModuleID)
	visit = func(m ast.ModuleID) {
		for _, d := range graph.Deps[m] {
			if !seen[d.Module] {
				seen[d.Module] = true
				visit(d.Module)
			}
		}
	}
	visit(from)
	return seen
}

// A module's generate reads the bodies and the check results of the generics it reaches, so it waits for the check of
// every module it reaches, not only of those it names: the first module of the chain lowers the instance that the last holds
// through the one between them. The check of a module waits for nothing.
func TestAModulesGenerateWaitsForTheChecksOfEveryModuleItReaches(t *testing.T) {
	const modules = 8
	graph := chainDeps(modules)
	for _, runtime := range []bool{false, true} {
		layout := taskLayout{modules: modules, runtime: runtime}
		plan := modulePlan(graph, layout)
		for _, jobs := range []int{1, 2, 8} {
			var mu sync.Mutex
			checked := map[ast.ModuleID]bool{}
			par.Run(plan, jobs, func() struct{} { return struct{}{} }, func(_ struct{}, task int) {
				m, kind, ofModule := layout.module(task)
				if !ofModule {
					return
				}
				mu.Lock()
				defer mu.Unlock()
				switch kind {
				case taskCheck:
					checked[m] = true
				case taskGenerate:
					if !checked[m] {
						t.Errorf("jobs=%d: module %d is generated before it is checked", jobs, m)
					}
					for r := range reaches(graph, m) {
						if !checked[r] {
							t.Errorf("jobs=%d: module %d is generated before module %d, which it reaches, is checked", jobs, m, r)
						}
					}
				}
			})
			if len(checked) != modules {
				t.Errorf("jobs=%d: %d modules were checked, want %d", jobs, len(checked), modules)
			}
		}
	}
}

// The tasks of one module are a group, so a worker that has checked a module goes on to generate it, and the check of
// every module comes before the generate of any that waits for one.
func TestAWorkerGeneratesTheModuleItHasJustChecked(t *testing.T) {
	// Modules 0 and 1 depend on nothing, so each is generated as soon as it is checked.
	graph := project.NewGraph(make([]ast.ModuleResult, 2))
	layout := taskLayout{modules: 2}
	var ran []string
	par.Run(modulePlan(graph, layout), 1, func() struct{} { return struct{}{} }, func(_ struct{}, task int) {
		m, kind, _ := layout.module(task)
		if kind != taskClosure {
			ran = append(ran, fmt.Sprintf("%d:%d", m, kind))
		}
	})
	if got, want := strings.Join(ran, " "), "0:0 0:2 1:0 1:2"; got != want {
		t.Errorf("the tasks ran in the order %s, want %s", got, want)
	}
}

const (
	chainLowest = `export type Box uses T
	export var item as T

	export static function make(consumes item T) returns Self
		return Self{item: item}
	end 'make'
end 'Box'

export function wrap(consumes x T) uses T returns Box with T
	return Box.make(x)
end 'wrap'
`
	chainMiddle = `export function viaMiddle() returns Count
	let b = lowest.wrap(5 as Count)
	return b.item
end 'viaMiddle'
`
	chainUpper = `export function viaUpper() returns Count
	return try middle.viaMiddle() + 1 otherwise 0
end 'viaUpper'
`
	chainMain = `function main() returns ExitCode
	print("{upper.viaUpper()}\n")
	return 0
end 'main'
`
)

// A generic in the last module of a chain is instantiated by the module in the middle, and the first module reaches it
// through two others: each is generated after the checks of the modules behind it, for any number of jobs, whichever
// worker is free first.
func TestAGenericAtTheEndOfAChainOfModulesIsLoweredAfterItsCheck(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"main.maxon": chainMain, "upper/upper.maxon": chainUpper, "middle/middle.maxon": chainMiddle, "lowest/lowest.maxon": chainLowest,
	})
	for range 20 {
		exe, ir := requireSameForAnyJobCount(t, dir)
		if !bytes.Contains(ir, []byte("function wrap[Count] params")) {
			t.Fatal("the IR has no instance of wrap")
		}
		out := filepath.Join(t.TempDir(), "run.exe")
		if err := os.WriteFile(out, exe, 0o755); err != nil {
			t.Fatal(err)
		}
		if got, err := exec.Command(out).Output(); err != nil || string(got) != "6\n" {
			t.Fatalf("the program printed %q (%v), want 6", got, err)
		}
	}
}

// An error in the module that holds a generic is the program's one error, and the modules that use the generic are not
// lowered, whichever of them a worker reaches first: a body error leaves the generic untyped, which lowering would fail
// on, and an ownership error leaves its drops unplaced.
func TestAnErrorInAModuleHidesWhatIsGeneratedFromItsDependents(t *testing.T) {
	const broken = `export function broken() returns ExitCode
	var a = Box.make(1 as Count)
	let b = a
	print("{b.item} {a.item}\n")
	return 0
end 'broken'
`
	cases := []struct{ name, library, code string }{
		{"ownership", instanceLibrary + "\n" + broken, "E3102"},
		{"body", strings.Replace(instanceLibrary, "return Box.make(x)\nend 'wrap'", "return 1\nend 'wrap'", 1), "E3005"},
	}
	for _, c := range cases {
		dir := writeProject(t, map[string]string{
			"main.maxon": instanceMain, "lib/lib.maxon": c.library, "user/user.maxon": instanceUser,
		})
		for _, output := range []string{"", filepath.Join(t.TempDir(), "out.exe")} {
			for _, jobs := range []int{1, 2, 4, 16} {
				for range 10 {
					errs := Compile(dir, output, Options{Jobs: jobs})
					if len(errs) != 1 || errs[0].Code != c.code || filepath.Base(errs[0].File) != "lib.maxon" {
						t.Fatalf("%s: output=%q jobs=%d: got %v, want the one %s in lib.maxon", c.name, output, jobs, errs, c.code)
					}
				}
			}
		}
	}
}
