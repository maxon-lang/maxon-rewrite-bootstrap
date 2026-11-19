package backend

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"minimaxon/internal/ast"
	"minimaxon/internal/check"
	"minimaxon/internal/ir"
	"minimaxon/internal/project"
	"minimaxon/internal/resolve"
	"minimaxon/internal/srctest"
	"minimaxon/internal/syntax"
)

// lowered runs the front end on a one-module program in a file of testdata and lowers every function, which is what the
// code generator reads.
func lowered(t *testing.T, path string) []*ir.IrFunction {
	t.Helper()
	src := srctest.Read(t, path)
	source := syntax.NewSource(src, []syntax.FileSlice{{File: "t.maxon", Begin: 0, End: len(src)}})
	parser := syntax.NewParser(source, syntax.Lex(source, nil))
	prog := parser.ParseProgram()
	if len(parser.Errors) > 0 {
		t.Fatalf("parse errors: %v", parser.Errors)
	}
	root := ast.NewModule("", ".", false)
	root.Decls = prog.Decls
	proj := ast.NewProject(root)
	proj.Modules = []*ast.Module{root}
	project.BuildTables(proj, 1)
	resolve.NewResolver(proj, root).Run()
	check.DeclareAliases(proj)
	c := check.NewChecker(proj, root, &root.Errors)
	for _, d := range root.Decls {
		root.Functions = append(root.Functions, c.Declare(d)...)
	}
	for _, d := range root.Decls {
		switch d.(type) {
		case *ast.TypeDecl, *ast.UnionDecl, *ast.EnumDecl:
			c.CheckBody(d)
		}
	}
	for _, f := range root.Functions {
		c.CheckBody(f)
	}
	c.Verify(c.Pending)
	c.VerifyBounds(append(root.Bounds, c.Bounds...))
	root.Callers = c.Callers
	if len(root.Errors) == 0 {
		o := check.NewOwnership(&root.Errors)
		for _, d := range root.Decls {
			o.CheckValues(d)
		}
		for _, f := range root.Functions {
			o.Check(f)
		}
	}
	if len(root.Errors) > 0 {
		t.Fatalf("errors: %v", root.Errors)
	}
	var fns []*ir.IrFunction
	for _, f := range root.Functions {
		if len(f.Generics) == 0 {
			fns = append(fns, ir.Lower(f))
		}
	}
	for _, inst := range ir.Instances([]*ast.Module{root})[0] {
		fns = append(fns, ir.Lower(inst))
	}
	return fns
}

const routinesFile = "testdata/routines.maxon"

// routineNames is the text of every routine the units define.
func routineNames(units []*CodeUnit) []string {
	var names []string
	for _, u := range units {
		for sym := range u.CodeSymbols {
			if r, ok := sym.(typeRoutine); ok {
				names = append(names, r.String())
			}
		}
	}
	return names
}

func hasRoutine(units []*CodeUnit, name string) bool {
	return slices.Contains(routineNames(units), name)
}

// undefined lists the symbols that the units call or load and none of them defines, other than the ones given.
func undefined(units []*CodeUnit, external map[any]bool) []string {
	defined := map[any]bool{}
	for _, u := range units {
		for sym := range u.CodeSymbols {
			defined[sym] = true
		}
		for sym := range u.DataSymbols {
			defined[sym] = true
		}
	}
	var missing []string
	for _, u := range units {
		for _, f := range u.Asm.SymbolFixups {
			if !defined[f.Symbol] && !external[f.Symbol] {
				missing = append(missing, SymbolText(f.Symbol))
			}
		}
	}
	return missing
}

// The symbols that the runtime and the linker define, which no routine unit does.
var outsideTheRoutines = map[any]bool{
	ir.RtAlloc: true, ir.RtFree: true, ir.RtImageDataStart: true, ir.RtImageDataEnd: true,
}

// Every type the program drops or copies has a routine, and so does every type those reach: a record that holds an array of
// itself has routines that call each other, and a routine never calls one that no unit defines.
func TestRoutinesAreMadeForEveryTypeTheCodeReaches(t *testing.T) {
	module := EmitModule(lowered(t, routinesFile))
	routines := EmitTypeRoutines([]*CodeUnit{module}, 1)
	for _, want := range []string{
		"drop Tree", "copy Tree", "drop array with Tree", "copy array with Tree", "drop String", "copy String",
		"drop Shape", "copy Shape", "copy Plain",
	} {
		if !hasRoutine(routines, want) {
			t.Errorf("no routine %q in %v", want, routineNames(routines))
		}
	}
	external := map[any]bool{}
	for sym := range outsideTheRoutines {
		external[sym] = true
	}
	for _, f := range module.Asm.SymbolFixups {
		if _, isRoutine := f.Symbol.(typeRoutine); !isRoutine {
			external[f.Symbol] = true
		}
	}
	if missing := undefined(append([]*CodeUnit{module}, routines...), external); len(missing) > 0 {
		t.Errorf("symbols no unit defines: %v", missing)
	}
}

// A box that holds nothing to drop is dropped by freeing it, so a record of integers has no drop routine, while a copy of
// it still needs one to know its size.
func TestDropOfABoxWithNothingInItIsTheFree(t *testing.T) {
	module := EmitModule(lowered(t, routinesFile))
	routines := EmitTypeRoutines([]*CodeUnit{module}, 1)
	if hasRoutine(routines, "drop Plain") {
		t.Error("a record of integers has a drop routine")
	}
	if !hasRoutine(routines, "copy Plain") {
		t.Error("a record of integers has no copy routine")
	}
	called := false
	for _, f := range module.Asm.SymbolFixups {
		called = called || f.Symbol == ir.RtFree
	}
	if !called {
		t.Error("the drop of a record of integers does not call the free")
	}
}

// The routines are the same bytes for any number of jobs: they are made in waves, in the order they were asked for, and
// split into units of a fixed size.
func TestRoutinesAreIdenticalForAnyJobCount(t *testing.T) {
	module := EmitModule(lowered(t, routinesFile))
	var first []*CodeUnit
	for _, jobs := range []int{1, 2, 8} {
		made := EmitTypeRoutines([]*CodeUnit{module}, jobs)
		if first == nil {
			first = made
			continue
		}
		if len(made) != len(first) {
			t.Fatalf("jobs=%d made %d units, jobs=1 made %d", jobs, len(made), len(first))
		}
		for i := range made {
			if !bytes.Equal(made[i].Asm.Code, first[i].Asm.Code) {
				t.Errorf("jobs=%d: unit %d differs from jobs=1", jobs, i)
			}
		}
	}
}

// A drop of a string compares its address with the bounds of the data section, where every literal lives, so a literal is
// never given to the heap.
func TestDroppingAStringChecksForAnImmortalOne(t *testing.T) {
	module := EmitModule(lowered(t, routinesFile))
	for _, u := range EmitTypeRoutines([]*CodeUnit{module}, 1) {
		if _, ok := u.CodeSymbols[typeRoutine{dropRoutine, ast.StringTy}]; !ok {
			continue
		}
		seen := map[any]bool{}
		for _, f := range u.Asm.SymbolFixups {
			seen[f.Symbol] = true
		}
		if !seen[ir.RtImageDataStart] || !seen[ir.RtImageDataEnd] {
			t.Error("the drop of a string does not read the bounds of the data section")
		}
		return
	}
	t.Error("no drop routine for a string")
}

// A move leaves the place empty, and a frame starts with its managed slots empty: the code stores zero where the IR moves,
// and clears the slots at the start.
func TestMovesAndFramesEmptyTheirSlots(t *testing.T) {
	f := &ir.IrFunction{Decl: &ast.FuncDecl{Name: "f"}}
	slot := f.NewSlot(ir.KindManaged)
	b := f.NewBlock()
	moved := f.NewReg(ir.KindManaged)
	b.Instrs = append(b.Instrs, &ir.MoveSlotInstr{Dst: moved, Slot: slot})
	b.Term = &ir.RetTerm{Value: moved}
	code := EmitModule([]*ir.IrFunction{f}).Asm.Code
	// mov [rbp-8], rax stores the zero of the frame's start; mov [rbp-8], rcx stores the zero of the move.
	for name, store := range map[string][]byte{"the frame": {0x48, 0x89, 0x45, 0xF8}, "the move": {0x48, 0x89, 0x4D, 0xF8}} {
		if !bytes.Contains(code, store) {
			t.Errorf("%s does not store a zero in the slot", name)
		}
	}
}

// A frame with many managed slots clears them with one string instruction.
func TestAFrameWithManySlotsIsClearedInOneInstruction(t *testing.T) {
	f := &ir.IrFunction{Decl: &ast.FuncDecl{Name: "f"}}
	for range zeroInlineSlots + 2 {
		f.NewSlot(ir.KindManaged)
	}
	b := f.NewBlock()
	zero := f.NewReg(ir.KindScalar)
	b.Instrs = append(b.Instrs, &ir.ConstInstr{Dst: zero, Value: 0})
	b.Term = &ir.RetTerm{Value: zero}
	code := EmitModule([]*ir.IrFunction{f}).Asm.Code
	if !bytes.Contains(code, []byte{0xF3, 0x48, 0xAB}) {
		t.Error("the frame is not cleared with rep stosq")
	}
	if bytes.Contains(code, []byte{0x48, 0x89, 0x45, 0xF8}) {
		t.Error("the frame is cleared with a store for each slot as well")
	}
}

// nativeMain is a main of one block: an allocation, optionally given back, and a return of the code.
func nativeMain(free bool, code int64) *ir.IrFunction {
	f := &ir.IrFunction{Decl: &ast.FuncDecl{Name: "main"}}
	b := f.NewBlock()
	box := f.NewReg(ir.KindManaged)
	exit := f.NewReg(ir.KindScalar)
	b.Instrs = append(b.Instrs, &ir.AllocInstr{Dst: box, Bytes: 16})
	if free {
		b.Instrs = append(b.Instrs, &ir.CallRuntimeInstr{Symbol: ir.RtFree, Args: []ir.V{box}})
	}
	b.Instrs = append(b.Instrs, &ir.ConstInstr{Dst: exit, Value: code})
	b.Term = &ir.RetTerm{Value: exit}
	return f
}

// runNative links one function as a whole program and runs it, giving what it wrote to standard error and its exit code.
func runNative(t *testing.T, main *ir.IrFunction, options RuntimeOptions) (stderr string, code int) {
	t.Helper()
	units := []*CodeUnit{EmitRuntime(main.Decl, options), EmitModule([]*ir.IrFunction{main})}
	exe := filepath.Join(t.TempDir(), "main.exe")
	Link(exe, units, ir.RtEntry, 1)
	var errors bytes.Buffer
	cmd := exec.Command(exe)
	cmd.Stderr = &errors
	err := cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		return errors.String(), exitErr.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return errors.String(), 0
}

// An executable built to count allocations ends with a box still live by saying so and exiting with leakExitCode, and one
// that gave every box back exits with its own code.
func TestAnExecutableThatCountsFailsWhenABoxIsLive(t *testing.T) {
	counting := RuntimeOptions{CountAllocations: true}
	if message, code := runNative(t, nativeMain(false, 5), counting); code != leakExitCode || message != "leaked allocations: 1\n" {
		t.Errorf("a box that is live: exit %d, stderr %q", code, message)
	}
	if message, code := runNative(t, nativeMain(true, 5), counting); code != 5 || message != "" {
		t.Errorf("a box that was freed: exit %d, stderr %q", code, message)
	}
}

// An executable that does not count has no counter and no check, so a leak goes unseen and costs nothing.
func TestAnExecutableThatDoesNotCountNeverChecks(t *testing.T) {
	if message, code := runNative(t, nativeMain(false, 5), RuntimeOptions{}); code != 5 || message != "" {
		t.Errorf("exit %d, stderr %q", code, message)
	}
	if _, ok := EmitRuntime(nativeMain(false, 0).Decl, RuntimeOptions{}).DataSymbols[ir.RtLive]; ok {
		t.Error("the runtime keeps a count that was not asked for")
	}
}
