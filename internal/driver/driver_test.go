package driver

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"minimaxon/internal/diag"
	"minimaxon/internal/interp"
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
