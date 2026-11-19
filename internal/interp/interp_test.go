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
	f   *ir.IrFunction
	cur *ir.IrBlock
}

func newFunction(name string, conv ...ast.Convention) (*builder, *ast.FuncDecl) {
	decl := &ast.FuncDecl{}
	decl.Name = name
	for _, c := range conv {
		decl.Pass.Specs = append(decl.Pass.Specs, ast.ParamSpec{Conv: c})
	}
	f := &ir.IrFunction{Decl: decl, ParamCount: len(conv)}
	b := &builder{f: f}
	b.cur = f.NewBlock()
	return b, decl
}

func (b *builder) reg(kind ir.IrKind) ir.V { return b.f.NewReg(kind) }

func (b *builder) emit(i ir.Instr) { b.cur.Instrs = append(b.cur.Instrs, i) }

func (b *builder) konst(n int64) ir.V {
	d := b.reg(ir.KindScalar)
	b.emit(&ir.ConstInstr{Dst: d, Value: n})
	return d
}

func (b *builder) alloc(words int) ir.V {
	d := b.reg(ir.KindManaged)
	b.emit(&ir.AllocInstr{Dst: d, Bytes: 8 * words})
	return d
}

func (b *builder) slot(kind ir.IrKind) int { return b.f.NewSlot(kind) }

func (b *builder) param(i int, kind ir.IrKind) ir.V {
	d := b.reg(kind)
	b.emit(&ir.LoadParam{Dst: d, Param: i})
	return d
}

func (b *builder) load(slot int) ir.V {
	d := b.reg(b.f.Slots[slot])
	b.emit(&ir.LoadSlot{Dst: d, Slot: slot})
	return d
}

func (b *builder) addr(slot int) ir.V {
	d := b.reg(ir.KindScalar)
	b.emit(&ir.SlotAddr{Dst: d, Slot: slot})
	return d
}

func (b *builder) store(slot int, v ir.V) { b.emit(&ir.StoreSlot{Slot: slot, Src: v}) }

func (b *builder) runtime(symbol string, kind ir.IrKind, args ...ir.V) ir.V {
	d := b.reg(kind)
	b.emit(&ir.CallRuntimeInstr{Dst: &d, Symbol: symbol, Args: args})
	return d
}

func (b *builder) call(target *ast.FuncDecl, args ...ir.V) {
	b.emit(&ir.CallInstr{Target: target, Args: args})
}

func (b *builder) ret(v ir.V) *ir.IrFunction {
	b.cur.Term = &ir.RetTerm{Value: v}
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
	b.emit(&ir.DropSlotInstr{Slot: s, Ty: ast.StringTy})
	r := run(b.ret(b.konst(7)))
	if len(r.Violations) != 0 || r.ExitCode != 7 {
		t.Fatalf("%+v", r)
	}
}

func TestUseAfterFree(t *testing.T) {
	b, _ := newFunction("main")
	box := b.alloc(1)
	b.emit(&ir.DropInstr{Src: box, Ty: ast.StringTy})
	d := b.reg(ir.KindScalar)
	b.emit(&ir.LoadField{Dst: d, Obj: box})
	expectViolation(t, run(b.ret(b.konst(0))), interp.UseAfterFree, "function main", "block b0", "instruction #2", "freed at main b0 #1")
}

func TestDoubleFree(t *testing.T) {
	b, _ := newFunction("main")
	box := b.alloc(1)
	b.emit(&ir.DropInstr{Src: box, Ty: ast.StringTy})
	b.emit(&ir.DropInstr{Src: box, Ty: ast.StringTy})
	expectViolation(t, run(b.ret(b.konst(0))), interp.DoubleFree, "instruction #2")
}

func TestFreeingAnOwnerFreesWhatItOwnsOnceAndNeverTwice(t *testing.T) {
	b, _ := newFunction("main")
	outer, inner := b.alloc(1), b.alloc(1)
	b.emit(&ir.StoreField{Obj: outer, Src: inner})
	b.emit(&ir.DropInstr{Src: outer, Ty: ast.StringTy})
	b.emit(&ir.DropInstr{Src: inner, Ty: ast.StringTy})
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
	b.emit(&ir.MoveSlotInstr{Dst: moved, Slot: s})
	b.emit(&ir.DropInstr{Src: moved, Ty: ast.StringTy})
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

// Two arguments reach the same record, and one of them is passed inout: the callee would reach one place through two
// names.
func TestOverlappingArgumentsAtACall(t *testing.T) {
	callee, calleeDecl := newFunction("touch", ast.ConvInout, ast.ConvLet)
	callee.ret(callee.konst(0))
	b, _ := newFunction("main")
	s := b.slot(ir.KindManaged)
	b.store(s, b.alloc(1))
	b.call(calleeDecl, b.addr(s), b.load(s))
	b.emit(&ir.DropSlotInstr{Slot: s, Ty: ast.StringTy})
	expectViolation(t, run(b.ret(b.konst(0)), callee.f), interp.Exclusivity, "arguments 0 and 1 of the call to touch")
}

func TestDisjointArgumentsAtACallAreFine(t *testing.T) {
	callee, calleeDecl := newFunction("touch", ast.ConvInout, ast.ConvLet)
	callee.ret(callee.konst(0))
	b, _ := newFunction("main")
	s, u := b.slot(ir.KindManaged), b.slot(ir.KindManaged)
	b.store(s, b.alloc(1))
	b.store(u, b.alloc(1))
	b.call(calleeDecl, b.addr(s), b.load(u))
	b.emit(&ir.DropSlotInstr{Slot: s, Ty: ast.StringTy})
	b.emit(&ir.DropSlotInstr{Slot: u, Ty: ast.StringTy})
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
	b.emit(&ir.StoreField{Obj: box, Src: b.konst(3)})
	b.call(calleeDecl, first, b.konst(0))
	b.emit(&ir.DropSlotInstr{Slot: s, Ty: ast.StringTy})
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
	b.emit(&ir.LoadAt{Dst: loaded, Addr: element})
	expectViolation(t, run(b.ret(b.konst(0))), interp.StaleBorrow, "before the structure changed")
}

func TestAnArrayThatIsDroppedFreesItsElements(t *testing.T) {
	b, _ := newFunction("main")
	a := b.runtime(ir.RtArrayNew, ir.KindManaged)
	e := b.alloc(1)
	b.runtime(ir.RtArrayInsert, ir.KindScalar, a, b.konst(0), e)
	b.emit(&ir.DropInstr{Src: a, Ty: ast.StringTy})
	d := b.reg(ir.KindScalar)
	b.emit(&ir.LoadField{Dst: d, Obj: e})
	expectViolation(t, run(b.ret(b.konst(0))), interp.UseAfterFree)
}

func TestAMovedElementIsOwnedByTheCaller(t *testing.T) {
	b, _ := newFunction("main")
	a := b.runtime(ir.RtArrayNew, ir.KindManaged)
	b.runtime(ir.RtArrayInsert, ir.KindScalar, a, b.konst(0), b.alloc(1))
	removed := b.runtime(ir.RtArrayRemove, ir.KindManaged, a, b.konst(0))
	b.emit(&ir.DropInstr{Src: a, Ty: ast.StringTy})
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
	b.emit(&ir.DropInstr{Src: gone, Ty: ast.StringTy})
	fresh := b.runtime(ir.RtPoolInsert, ir.KindScalar, p, b.alloc(1))
	same := b.reg(ir.KindScalar)
	b.emit(&ir.CmpInstr{Dst: same, Op: ir.CmpEq, A: old, B: fresh})
	found := b.runtime(ir.RtPoolContains, ir.KindScalar, p, old)
	live := b.runtime(ir.RtPoolContains, ir.KindScalar, p, fresh)
	// The answer is 100*same + 10*found + live, which must be 1.
	hundred := b.reg(ir.KindScalar)
	b.emit(&ir.BinInstr{Dst: hundred, Op: ir.OpMul, A: same, B: b.konst(100)})
	ten := b.reg(ir.KindScalar)
	b.emit(&ir.BinInstr{Dst: ten, Op: ir.OpMul, A: found, B: b.konst(10)})
	sum := b.reg(ir.KindScalar)
	b.emit(&ir.BinInstr{Dst: sum, Op: ir.OpAdd, A: hundred, B: ten})
	answer := b.reg(ir.KindScalar)
	b.emit(&ir.BinInstr{Dst: answer, Op: ir.OpAdd, A: sum, B: live})
	b.emit(&ir.DropSlotInstr{Slot: pool, Ty: ast.StringTy})
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
	b.emit(&ir.DropInstr{Src: gone, Ty: ast.StringTy})
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
	b.emit(&ir.BinInstr{Dst: sum, Op: ir.OpAdd, A: max, B: b.konst(1), Checked: true})
	r := run(b.ret(sum))
	if r.ExitCode != 1 || len(r.Violations) != 0 {
		t.Fatalf("exit %d, %v", r.ExitCode, r.Violations)
	}
}
