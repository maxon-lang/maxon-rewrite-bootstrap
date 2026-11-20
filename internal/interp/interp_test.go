package interp_test

import (
	"strings"
	"testing"

	"minimaxon/internal/ast"
	"minimaxon/internal/interp"
	"minimaxon/internal/ir"
)

// The programs here are written as IR, because the checker refuses every one of them: each breaks a rule that the
// interpreter has to catch, which is what makes it trustworthy on the programs the checker accepts.

type builder struct {
	ir  ir.Builder
	cur ir.BlockID
	// f is the finished function, once ret has been called.
	f *ir.IrFunction
}

func newFunction(name string, conv ...ast.Convention) (*builder, *ast.FuncDecl) {
	decl := &ast.FuncDecl{}
	decl.Name = name
	for _, c := range conv {
		decl.Pass.Specs = append(decl.Pass.Specs, ast.ParamSpec{Conv: c})
	}
	b := &builder{}
	b.ir.Start(decl, "", len(conv))
	b.cur = b.ir.NewBlock()
	return b, decl
}

func (b *builder) reg(kind ir.IrKind) ir.V { return b.ir.F.NewReg(kind) }

func (b *builder) emit(i ir.Instr) { b.ir.Emit(b.cur, i) }

func (b *builder) konst(n int64) ir.V {
	d := b.reg(ir.KindScalar)
	b.emit(ir.Const(d, n))
	return d
}

func (b *builder) alloc(words int) ir.V {
	d := b.reg(ir.KindManaged)
	b.emit(ir.Alloc(d, 8*words))
	return d
}

func (b *builder) slot(kind ir.IrKind) int { return b.ir.F.NewSlot(kind) }

func (b *builder) param(i int, kind ir.IrKind) ir.V {
	d := b.reg(kind)
	b.emit(ir.LoadParam(d, i))
	return d
}

func (b *builder) load(slot int) ir.V {
	d := b.reg(b.ir.F.Slots[slot])
	b.emit(ir.LoadSlot(d, slot))
	return d
}

func (b *builder) addr(slot int) ir.V {
	d := b.reg(ir.KindScalar)
	b.emit(ir.SlotAddr(d, slot))
	return d
}

func (b *builder) store(slot int, v ir.V) { b.emit(ir.StoreSlot(slot, v, false)) }

func (b *builder) runtime(symbol ir.Rt, kind ir.IrKind, args ...ir.V) ir.V {
	d := b.reg(kind)
	b.emit(b.ir.CallRuntime(d, symbol, args, nil))
	return d
}

func (b *builder) call(target *ast.FuncDecl, args ...ir.V) {
	b.emit(b.ir.Call(ir.NoReg, target, args))
}

func (b *builder) ret(v ir.V) *ir.IrFunction {
	b.ir.Terminate(b.cur, ir.Ret(v))
	b.f = b.ir.Finish(nil)
	return b.f
}

// run executes main with the other functions available, and gives the violations it found.
func run(main *ir.IrFunction, others ...*ir.IrFunction) *interp.Result {
	fns := append([]*ir.IrFunction{main}, others...)
	return interp.Run(interp.NewProgram(fns, main.Decl), interp.DefaultStepLimit)
}

func expectViolation(t *testing.T, r *interp.Result, kind interp.ViolationKind, fragments ...string) {
	t.Helper()
	if len(r.Violations) == 0 {
		t.Fatalf("no violation; wanted %s", kind)
	}
	v := r.Violations[0]
	if v.Kind != kind {
		t.Fatalf("got %s, wanted %s:\n%s", v.Kind, kind, v)
	}
	for _, f := range fragments {
		if !strings.Contains(v.String(), f) {
			t.Errorf("the report lacks %q:\n%s", f, v)
		}
	}
}

func TestACleanProgramHasNoViolation(t *testing.T) {
	b, _ := newFunction("main")
	s := b.slot(ir.KindManaged)
	b.store(s, b.alloc(2))
	b.emit(b.ir.DropSlot(s, ast.StringTy))
	r := run(b.ret(b.konst(7)))
	if len(r.Violations) != 0 || r.ExitCode != 7 {
		t.Fatalf("%+v", r)
	}
}

func TestUseAfterFree(t *testing.T) {
	b, _ := newFunction("main")
	box := b.alloc(1)
	b.emit(b.ir.Drop(box, ast.StringTy))
	d := b.reg(ir.KindScalar)
	b.emit(ir.LoadField(d, box, 0))
	expectViolation(t, run(b.ret(b.konst(0))), interp.UseAfterFree, "function main", "block b0", "instruction #2", "freed at main b0 #1")
}

func TestDoubleFree(t *testing.T) {
	b, _ := newFunction("main")
	box := b.alloc(1)
	b.emit(b.ir.Drop(box, ast.StringTy))
	b.emit(b.ir.Drop(box, ast.StringTy))
	expectViolation(t, run(b.ret(b.konst(0))), interp.DoubleFree, "instruction #2")
}

func TestFreeingAnOwnerFreesWhatItOwnsOnceAndNeverTwice(t *testing.T) {
	b, _ := newFunction("main")
	outer, inner := b.alloc(1), b.alloc(1)
	b.emit(ir.StoreField(outer, 0, inner))
	b.emit(b.ir.Drop(outer, ast.StringTy))
	b.emit(b.ir.Drop(inner, ast.StringTy))
	expectViolation(t, run(b.ret(b.konst(0))), interp.DoubleFree)
}

func TestLeakAtTheEndOfTheProgram(t *testing.T) {
	b, _ := newFunction("main")
	b.alloc(1)
	expectViolation(t, run(b.ret(b.konst(0))), interp.Leak, "still live at the end of the program", "made in main b0 #0")
}

func TestASlotThatStillOwnsWhenTheFunctionReturns(t *testing.T) {
	b, _ := newFunction("main")
	s := b.slot(ir.KindManaged)
	b.store(s, b.alloc(1))
	expectViolation(t, run(b.ret(b.konst(0))), interp.Leak, "slot s0 still owns")
}

func TestUsingAMovedValueThroughItsOldOwner(t *testing.T) {
	b, _ := newFunction("main")
	s := b.slot(ir.KindManaged)
	b.store(s, b.alloc(1))
	moved := b.reg(ir.KindManaged)
	b.emit(ir.MoveSlot(moved, s))
	b.emit(b.ir.Drop(moved, ast.StringTy))
	b.load(s)
	expectViolation(t, run(b.ret(b.konst(0))), interp.MovedUse, "moved out or never assigned")
}

func TestAValueWithTwoOwners(t *testing.T) {
	b, _ := newFunction("main")
	s, u := b.slot(ir.KindManaged), b.slot(ir.KindManaged)
	box := b.alloc(1)
	b.store(s, box)
	b.store(u, box)
	expectViolation(t, run(b.ret(b.konst(0))), interp.Ownership, "still owns it", "must be moved first")
}

func TestOverwritingAnOwnedValueWithoutDroppingIt(t *testing.T) {
	b, _ := newFunction("main")
	s := b.slot(ir.KindManaged)
	b.store(s, b.alloc(1))
	b.store(s, b.alloc(1))
	expectViolation(t, run(b.ret(b.konst(0))), interp.Leak, "lost without being dropped")
}

func TestReturningAValueThatAnotherOwnerHolds(t *testing.T) {
	b, _ := newFunction("main")
	s := b.slot(ir.KindManaged)
	box := b.alloc(1)
	b.store(s, box)
	expectViolation(t, run(b.ret(box)), interp.Ownership, "still owns")
}

// Two arguments reach the same record, and one of them is passed mutable: the callee would reach one place through two
// names.
func TestOverlappingArgumentsAtACall(t *testing.T) {
	callee, calleeDecl := newFunction("touch", ast.ConvMutates, ast.ConvLet)
	callee.ret(callee.konst(0))
	b, _ := newFunction("main")
	s := b.slot(ir.KindManaged)
	b.store(s, b.alloc(1))
	b.call(calleeDecl, b.addr(s), b.load(s))
	b.emit(b.ir.DropSlot(s, ast.StringTy))
	expectViolation(t, run(b.ret(b.konst(0)), callee.f), interp.Exclusivity, "arguments 0 and 1 of the call to touch")
}

func TestDisjointArgumentsAtACallAreFine(t *testing.T) {
	callee, calleeDecl := newFunction("touch", ast.ConvMutates, ast.ConvLet)
	callee.ret(callee.konst(0))
	b, _ := newFunction("main")
	s, u := b.slot(ir.KindManaged), b.slot(ir.KindManaged)
	b.store(s, b.alloc(1))
	b.store(u, b.alloc(1))
	b.call(calleeDecl, b.addr(s), b.load(u))
	b.emit(b.ir.DropSlot(s, ast.StringTy))
	b.emit(b.ir.DropSlot(u, ast.StringTy))
	if r := run(b.ret(b.konst(0)), callee.f); len(r.Violations) != 0 {
		t.Fatal(r.Violations[0])
	}
}

// A borrow is taken when its argument is evaluated and used when the call runs. Changing what it borrows in between
// leaves it pointing at what moved.
func TestChangingWhatAnArgumentBorrowsBeforeTheCallRuns(t *testing.T) {
	callee, calleeDecl := newFunction("look", ast.ConvLet, ast.ConvLet)
	callee.ret(callee.konst(0))
	b, _ := newFunction("main")
	s := b.slot(ir.KindManaged)
	box := b.alloc(1)
	b.store(s, box)
	first := b.load(s)
	b.emit(ir.StoreField(box, 0, b.konst(3)))
	b.call(calleeDecl, first, b.konst(0))
	b.emit(b.ir.DropSlot(s, ast.StringTy))
	expectViolation(t, run(b.ret(b.konst(0)), callee.f), interp.Exclusivity, "still waiting to use")
}

func TestAnAddressIsStaleOnceTheArrayChanges(t *testing.T) {
	b, _ := newFunction("main")
	s := b.slot(ir.KindManaged)
	a := b.runtime(ir.RtArrayNew, ir.KindManaged)
	b.store(s, a)
	b.runtime(ir.RtArrayInsert, ir.KindScalar, a, b.konst(0), b.konst(1))
	element := b.runtime(ir.RtArrayElem, ir.KindScalar, a, b.konst(0))
	b.runtime(ir.RtArrayInsert, ir.KindScalar, a, b.konst(0), b.konst(2))
	loaded := b.reg(ir.KindScalar)
	b.emit(ir.LoadAt(loaded, element))
	expectViolation(t, run(b.ret(b.konst(0))), interp.StaleBorrow, "before the structure changed")
}

func TestAnArrayThatIsDroppedFreesItsElements(t *testing.T) {
	b, _ := newFunction("main")
	a := b.runtime(ir.RtArrayNew, ir.KindManaged)
	e := b.alloc(1)
	b.runtime(ir.RtArrayInsert, ir.KindScalar, a, b.konst(0), e)
	b.emit(b.ir.Drop(a, ast.StringTy))
	d := b.reg(ir.KindScalar)
	b.emit(ir.LoadField(d, e, 0))
	expectViolation(t, run(b.ret(b.konst(0))), interp.UseAfterFree)
}

func TestAMovedElementIsOwnedByTheCaller(t *testing.T) {
	b, _ := newFunction("main")
	a := b.runtime(ir.RtArrayNew, ir.KindManaged)
	b.runtime(ir.RtArrayInsert, ir.KindScalar, a, b.konst(0), b.alloc(1))
	removed := b.runtime(ir.RtArrayRemove, ir.KindManaged, a, b.konst(0))
	b.emit(b.ir.Drop(a, ast.StringTy))
	// The element is the caller's now and leaks unless dropped.
	_ = removed
	expectViolation(t, run(b.ret(b.konst(0))), interp.Leak)
}

// A slot that held an element and was reused for a new one does not answer to the old handle: the handle carries the
// slot's generation.
func TestAPoolSlotThatIsReusedDoesNotAnswerToTheOldHandle(t *testing.T) {
	b, _ := newFunction("main")
	p := b.runtime(ir.RtPoolNew, ir.KindManaged)
	pool := b.slot(ir.KindManaged)
	b.store(pool, p)
	old := b.runtime(ir.RtPoolInsert, ir.KindScalar, p, b.alloc(1))
	gone := b.runtime(ir.RtPoolRemove, ir.KindManaged, p, old)
	b.emit(b.ir.Drop(gone, ast.StringTy))
	fresh := b.runtime(ir.RtPoolInsert, ir.KindScalar, p, b.alloc(1))
	same := b.reg(ir.KindScalar)
	b.emit(ir.Cmp(same, ir.CmpEq, old, fresh, false))
	found := b.runtime(ir.RtPoolContains, ir.KindScalar, p, old)
	live := b.runtime(ir.RtPoolContains, ir.KindScalar, p, fresh)
	// The answer is 100*same + 10*found + live, which must be 1.
	hundred := b.reg(ir.KindScalar)
	b.emit(ir.Bin(hundred, ir.BinMul, same, b.konst(100), false, false))
	ten := b.reg(ir.KindScalar)
	b.emit(ir.Bin(ten, ir.BinMul, found, b.konst(10), false, false))
	sum := b.reg(ir.KindScalar)
	b.emit(ir.Bin(sum, ir.BinAdd, hundred, ten, false, false))
	answer := b.reg(ir.KindScalar)
	b.emit(ir.Bin(answer, ir.BinAdd, sum, live, false, false))
	b.emit(b.ir.DropSlot(pool, ast.StringTy))
	r := run(b.ret(answer))
	if len(r.Violations) != 0 || r.ExitCode != 1 {
		t.Fatalf("exit %d, %v", r.ExitCode, r.Violations)
	}
}

func TestAStaleHandleTrapsLikeTheCompiledProgram(t *testing.T) {
	b, _ := newFunction("main")
	p := b.runtime(ir.RtPoolNew, ir.KindManaged)
	pool := b.slot(ir.KindManaged)
	b.store(pool, p)
	old := b.runtime(ir.RtPoolInsert, ir.KindScalar, p, b.alloc(1))
	gone := b.runtime(ir.RtPoolRemove, ir.KindManaged, p, old)
	b.emit(b.ir.Drop(gone, ast.StringTy))
	b.runtime(ir.RtPoolElem, ir.KindScalar, p, old)
	r := run(b.ret(b.konst(0)))
	if len(r.Violations) != 0 || r.ExitCode != 1 || !strings.Contains(string(r.Stderr), "panic: handle names no element") {
		t.Fatalf("exit %d, %v, %q", r.ExitCode, r.Violations, r.Stderr)
	}
}

func TestOverflowAndPanicsExitWithOne(t *testing.T) {
	b, _ := newFunction("main")
	max := b.konst(1<<63 - 1)
	sum := b.reg(ir.KindScalar)
	b.emit(ir.Bin(sum, ir.BinAdd, max, b.konst(1), false, true))
	r := run(b.ret(sum))
	if r.ExitCode != 1 || len(r.Violations) != 0 {
		t.Fatalf("exit %d, %v", r.ExitCode, r.Violations)
	}
}
