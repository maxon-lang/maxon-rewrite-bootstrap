package backend

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"minimaxon/internal/ir"
)

// errorReg is where a function that throws returns its error: 0 when it returned, and the error's case plus 1 when it
// failed.
const errorReg = Rdx

const (
	pageSize        = 4096
	frameAlignment  = 16
	shiftBitCount   = 64
	signBitShift    = 63
	trapOpcodeFirst = 0x0F
	trapOpcodeNext  = 0x0B

	// zeroInlineSlots is how many managed slots a frame clears with one store each; a frame with more clears all its slots
	// with one string instruction, which is shorter from there on.
	zeroInlineSlots = 6

	// estimatedCodeBytesPerInstr sizes a unit's code buffer before codegen. Go grows a large slice by about
	// a quarter at a time, so a buffer left to grow allocates several times its final size; this is the
	// measured average (15.2 bytes over a generated 200-module project), rounded up so the buffer rarely
	// regrows.
	estimatedCodeBytesPerInstr = 16
)

type codegen struct {
	u       *CodeUnit
	asm     *Asm
	rtAlloc *Label
	f       *ir.IrFunction
	labels  []Label
	// next is the block whose code follows the block being written, which a jump to it falls through to, or NoBlock.
	next ir.BlockID
}

func EmitModule(fns []*ir.IrFunction) *CodeUnit {
	u := NewCodeUnit()
	instrs, symbols, data := 0, 0, 0
	for _, f := range fns {
		instrs += len(f.Instrs) + len(f.Blocks)
		for _, in := range f.Instrs {
			symbols += symbolFixups(in)
			if in.Op() == ir.OpStr {
				data++
			}
		}
	}
	u.Asm.Code = make([]byte, 0, instrs*estimatedCodeBytesPerInstr)
	u.Asm.SymbolFixups = make([]SymbolFixup, 0, symbols)
	u.Asm.DataFixups = make([]DataFixup, 0, data)
	u.Sections = make([]Section, 0, len(fns))
	u.CodeSymbols = make([]SymbolDef, 0, len(fns))
	g := &codegen{u: u, asm: u.Asm, rtAlloc: ExternLabel(ir.RtAlloc.String())}
	for _, f := range fns {
		g.function(f)
	}
	return u
}

// symbolFixups is how many references to a symbol the code of an instruction holds: a call, a global, or a routine. The
// unit's fixups are sized from it, and from the strings it loads, because a slice grown by appending allocates several
// times its final size.
func symbolFixups(in ir.Instr) int {
	switch in.Op() {
	case ir.OpLoadGlobal, ir.OpCall, ir.OpCallRuntime, ir.OpAlloc, ir.OpDrop, ir.OpDropSlot, ir.OpDropAt, ir.OpCopy:
		return 1
	}
	return 0
}

func slotOffset(slot int) int {
	return -wordBytes * (slot + 1)
}

func (g *codegen) regOffset(v ir.V) int {
	return -wordBytes * (len(g.f.Slots) + int(v) + 1)
}

func (g *codegen) paramOffset(index int) int {
	return 16 + wordBytes*(g.f.ParamCount-1-index)
}

func (g *codegen) get(r R, v ir.V) {
	g.asm.Load(r, Rbp, g.regOffset(v))
}

func (g *codegen) put(v ir.V, r R) {
	g.asm.Store(Rbp, g.regOffset(v), r)
}

func (g *codegen) function(fn *ir.IrFunction) {
	a := g.asm
	g.f = fn
	if cap(g.labels) < len(fn.Blocks) {
		g.labels = make([]Label, len(fn.Blocks))
	}
	g.labels = g.labels[:len(fn.Blocks)]
	for i := range g.labels {
		g.labels[i].Pos = -1
		g.labels[i].Uses = g.labels[i].Uses[:0]
	}
	g.u.BeginSection()
	g.u.DefineFunction(fn.Decl)
	a.Push(Rbp)
	a.Mov(Rbp, Rsp)
	g.allocateFrame((len(fn.Slots) + len(fn.Regs)) * wordBytes)
	g.zeroManagedSlots()

	for i := range fn.Blocks {
		a.Bind(&g.labels[i])
		instrs := fn.BlockInstrs(i)
		for k := range instrs {
			ir.VisitInstr(g, &instrs[k])
		}
		g.next = ir.NoBlock
		if i+1 < len(fn.Blocks) {
			g.next = ir.BlockID(i + 1)
		}
		ir.VisitTerm(g, &fn.Blocks[i].Term)
	}
}

// zeroManagedSlots empties every slot that can own a value, so a drop of a slot that was moved from, or never assigned, finds
// 0 and does nothing. A scalar slot is never dropped and is left as it is, and so is a register, which is always written
// before it is read.
func (g *codegen) zeroManagedSlots() {
	a := g.asm
	managed := 0
	for _, kind := range g.f.Slots {
		if kind == ir.KindManaged {
			managed++
		}
	}
	if managed == 0 {
		return
	}
	a.Xor(Rax, Rax)
	if managed > zeroInlineSlots {
		slots := len(g.f.Slots)
		a.Lea(Rdi, Rbp, slotOffset(slots-1))
		a.MovImm(Rcx, int64(slots))
		a.RepStosq()
		return
	}
	for slot, kind := range g.f.Slots {
		if kind == ir.KindManaged {
			a.Store(Rbp, slotOffset(slot), Rax)
		}
	}
}

func (g *codegen) allocateFrame(bytes int) {
	a := g.asm
	frame := (bytes + frameAlignment - 1) &^ (frameAlignment - 1)
	for frame > pageSize {
		a.SubImm(Rsp, pageSize)
		a.Store(Rsp, 0, Rax)
		frame -= pageSize
	}
	if frame > 0 {
		a.SubImm(Rsp, frame)
	}
}

func (g *codegen) call(symbol string, args []ir.V, dst ir.V, gives bool) {
	a := g.asm
	for _, arg := range args {
		g.get(Rax, arg)
		a.Push(Rax)
	}
	a.CallSymbol(symbol)
	if len(args) > 0 {
		a.AddImm(Rsp, len(args)*wordBytes)
	}
	if gives {
		g.put(dst, Rax)
	}
}

// callRoutine calls the drop or copy routine of a type with the value in rax. The routine is generated once for the
// program, after every module, from the list each unit keeps of the ones it asked for.
func (g *codegen) callRoutine(kind routineKind, t ast.Ty) {
	g.asm.Push(Rax)
	g.asm.CallSymbol(g.u.needed.symbol(kind, t))
	g.asm.AddImm(Rsp, wordBytes)
}

// The Visit methods are codegen's ir.InstrVisitor and ir.TermVisitor: each writes the code of one operation.

func (g *codegen) VisitConst(x *ir.ConstInstr) {
	g.asm.MovImm(Rax, x.Value())
	g.put(x.Dst(), Rax)
}

func (g *codegen) VisitStr(x *ir.StrInstr) {
	g.asm.LeaData(Rax, g.u.Data.String(x.Text(g.f)))
	g.put(x.Dst(), Rax)
}

func (g *codegen) VisitLoadSlot(x *ir.LoadSlotInstr) {
	g.asm.Load(Rax, Rbp, slotOffset(x.Slot()))
	g.put(x.Dst(), Rax)
}

func (g *codegen) VisitMoveSlot(x *ir.MoveSlotInstr) {
	a := g.asm
	a.Load(Rax, Rbp, slotOffset(x.Slot()))
	g.put(x.Dst(), Rax)
	a.Xor(Rcx, Rcx)
	a.Store(Rbp, slotOffset(x.Slot()), Rcx)
}

func (g *codegen) VisitStoreSlot(x *ir.StoreSlotInstr) {
	g.get(Rax, x.Src())
	g.asm.Store(Rbp, slotOffset(x.Slot()), Rax)
}

func (g *codegen) VisitSlotAddr(x *ir.SlotAddrInstr) {
	g.asm.Lea(Rax, Rbp, slotOffset(x.Slot()))
	g.put(x.Dst(), Rax)
}

func (g *codegen) VisitLoadParam(x *ir.LoadParamInstr) {
	g.asm.Load(Rax, Rbp, g.paramOffset(x.Param()))
	g.put(x.Dst(), Rax)
}

func (g *codegen) VisitLoadField(x *ir.LoadFieldInstr) {
	g.get(Rax, x.Obj())
	g.asm.Load(Rax, Rax, x.Offset())
	g.put(x.Dst(), Rax)
}

func (g *codegen) VisitStoreField(x *ir.StoreFieldInstr) {
	g.get(Rcx, x.Obj())
	g.get(Rax, x.Src())
	g.asm.Store(Rcx, x.Offset(), Rax)
}

func (g *codegen) VisitFieldAddr(x *ir.FieldAddrInstr) {
	g.get(Rax, x.Obj())
	g.asm.Lea(Rax, Rax, x.Offset())
	g.put(x.Dst(), Rax)
}

func (g *codegen) VisitLoadAt(x *ir.LoadAtInstr) {
	g.get(Rax, x.Addr())
	g.asm.Load(Rax, Rax, 0)
	g.put(x.Dst(), Rax)
}

func (g *codegen) VisitMoveAt(x *ir.MoveAtInstr) {
	a := g.asm
	g.get(Rcx, x.Addr())
	a.Load(Rax, Rcx, 0)
	g.put(x.Dst(), Rax)
	a.Xor(Rdx, Rdx)
	a.Store(Rcx, 0, Rdx)
}

func (g *codegen) VisitDrop(x *ir.DropInstr) {
	g.get(Rax, x.Src())
	g.callRoutine(dropRoutine, x.Ty(g.f))
}

func (g *codegen) VisitDropSlot(x *ir.DropSlotInstr) {
	a := g.asm
	a.Load(Rax, Rbp, slotOffset(x.Slot()))
	a.Xor(Rcx, Rcx)
	a.Store(Rbp, slotOffset(x.Slot()), Rcx)
	g.callRoutine(dropRoutine, x.Ty(g.f))
}

func (g *codegen) VisitDropAt(x *ir.DropAtInstr) {
	a := g.asm
	g.get(Rcx, x.Addr())
	a.Load(Rax, Rcx, 0)
	a.Xor(Rdx, Rdx)
	a.Store(Rcx, 0, Rdx)
	g.callRoutine(dropRoutine, x.Ty(g.f))
}

func (g *codegen) VisitCopy(x *ir.CopyInstr) {
	g.get(Rax, x.Src())
	g.callRoutine(copyRoutine, x.Ty(g.f))
	g.put(x.Dst(), Rax)
}

func (g *codegen) VisitStoreAt(x *ir.StoreAtInstr) {
	g.get(Rcx, x.Addr())
	g.get(Rax, x.Src())
	g.asm.Store(Rcx, 0, Rax)
}

func (g *codegen) VisitLoadGlobal(x *ir.LoadGlobalInstr) {
	g.asm.LoadSymbol(Rax, x.Symbol().String())
	g.put(x.Dst(), Rax)
}

func (g *codegen) VisitBin(x *ir.BinInstr) {
	g.get(Rax, x.Left())
	g.get(Rcx, x.Right())
	g.binary(x)
	g.put(x.Dst(), Rax)
}

func (g *codegen) VisitCmp(x *ir.CmpInstr) {
	g.get(Rax, x.Left())
	g.get(Rcx, x.Right())
	g.asm.Cmp(Rax, Rcx)
	g.asm.SetccRax(condition(x))
	g.put(x.Dst(), Rax)
}

func (g *codegen) VisitNeg(x *ir.NegInstr) {
	g.get(Rax, x.Src())
	g.asm.Neg(Rax)
	if fail, ok := x.Fail(); ok {
		g.asm.Jcc(CcO, &g.labels[fail])
	}
	g.put(x.Dst(), Rax)
}

func (g *codegen) VisitNot(x *ir.NotInstr) {
	g.get(Rax, x.Src())
	g.asm.Not(Rax)
	g.put(x.Dst(), Rax)
}

func (g *codegen) VisitAlloc(x *ir.AllocInstr) {
	a := g.asm
	a.MovImm(Rax, int64(x.Bytes()))
	a.Push(Rax)
	a.Call(g.rtAlloc)
	a.AddImm(Rsp, wordBytes)
	g.put(x.Dst(), Rax)
}

func (g *codegen) VisitCall(x *ir.CallInstr) {
	dst, gives := x.Dst()
	g.call(functionSymbol(x.Callee(g.f)), x.Args(g.f), dst, gives)
	if err, throws := x.ErrReg(); throws {
		g.put(err, errorReg)
	}
}

func (g *codegen) VisitCallRuntime(x *ir.CallRuntimeInstr) {
	dst, gives := x.Dst()
	g.call(x.Symbol().String(), x.Args(g.f), dst, gives)
}

func (g *codegen) VisitWhen(*ir.WhenInstr) {
	panic(diag.Internal("codegen: a behaviour has no native code, and the driver refuses it before code is generated"))
}

func condition(c *ir.CmpInstr) Cc {
	unsigned := c.Unsigned()
	switch c.Op() {
	case ir.CmpEq:
		return CcE
	case ir.CmpNe:
		return CcNE
	case ir.CmpLt:
		if unsigned {
			return CcB
		}
		return CcL
	case ir.CmpLe:
		if unsigned {
			return CcBE
		}
		return CcLE
	case ir.CmpGt:
		if unsigned {
			return CcA
		}
		return CcG
	case ir.CmpGe:
		if unsigned {
			return CcAE
		}
		return CcGE
	}
	panic(diag.Internal("codegen: comparison %d", int(c.Op())))
}

// binary computes an operation on rax and rcx into rax. A checked one jumps to its failure block when the result does not
// fit: the carry flag says so for an unsigned word and the overflow flag for a signed one.
func (g *codegen) binary(b *ir.BinInstr) {
	a := g.asm
	unsigned := b.Unsigned()
	overflow := CcO
	if unsigned {
		overflow = CcB
	}
	fail, checked := b.Fail()
	switch b.Op() {
	case ir.BinAdd:
		a.Add(Rax, Rcx)
	case ir.BinSub:
		a.Sub(Rax, Rcx)
	case ir.BinMul:
		if unsigned {
			a.MulRax(Rcx)
		} else {
			a.Imul(Rax, Rcx)
		}
	case ir.BinDiv, ir.BinMod:
		g.divide(b.Op() == ir.BinMod, unsigned, fail, checked)
		return
	case ir.BinAnd:
		a.And(Rax, Rcx)
	case ir.BinOr:
		a.Or(Rax, Rcx)
	case ir.BinXor:
		a.Xor(Rax, Rcx)
	case ir.BinShl, ir.BinShr:
		g.shift(b.Op() == ir.BinShl, unsigned, fail, checked)
		return
	default:
		panic(diag.Internal("codegen: operator %d", int(b.Op())))
	}
	if checked {
		a.Jcc(overflow, &g.labels[fail])
	}
}

// divide divides rax by rcx, which a guard or the range analysis has found is not 0, into rax: the quotient, or the
// remainder when mod is set. A signed division by -1 is the one that the processor faults on, for i64.min: the quotient is
// then the failure of a checked division, and the remainder is 0.
func (g *codegen) divide(mod, unsigned bool, fail ir.BlockID, checked bool) {
	a := g.asm
	if unsigned {
		a.Xor(Rdx, Rdx)
		a.Div(Rcx)
		if mod {
			a.Mov(Rax, Rdx)
		}
		return
	}
	divide := NewLabel()
	done := NewLabel()
	a.CmpImm(Rcx, -1)
	a.Jcc(CcNE, divide)
	if mod {
		a.Xor(Rax, Rax)
	} else {
		a.Neg(Rax)
		if checked {
			a.Jcc(CcO, &g.labels[fail])
		}
	}
	a.Jmp(done)
	a.Bind(divide)
	a.Cqo()
	a.Idiv(Rcx)
	if mod {
		a.Mov(Rax, Rdx)
	}
	a.Bind(done)
}

// shift shifts rax by rcx, which a guard or the range analysis has found is not negative. A checked left shift goes to its
// failure block when a bit that is not 0, or the sign, leaves the word: shifting the result back must give the value.
func (g *codegen) shift(left, unsigned bool, fail ir.BlockID, checked bool) {
	a := g.asm
	big := NewLabel()
	done := NewLabel()
	checkLeft := left && checked
	if checkLeft {
		a.Mov(Rdx, Rax)
	}
	a.CmpImm(Rcx, shiftBitCount)
	a.Jcc(CcGE, big)
	switch {
	case left:
		a.ShlCl(Rax)
		if checkLeft {
			a.Mov(R8, Rax)
			if unsigned {
				a.ShrCl(R8)
			} else {
				a.SarCl(R8)
			}
			a.Cmp(R8, Rdx)
			a.Jcc(CcNE, &g.labels[fail])
		}
	case unsigned:
		a.ShrCl(Rax)
	default:
		a.SarCl(Rax)
	}
	a.Jmp(done)
	a.Bind(big)
	if checkLeft {
		a.Test(Rdx, Rdx)
		a.Jcc(CcNE, &g.labels[fail])
	}
	if !left && !unsigned {
		a.SarImm(Rax, signBitShift)
	} else {
		a.Xor(Rax, Rax)
	}
	a.Bind(done)
}

// guard jumps to the guard's failure block unless the value is in its range.
func (g *codegen) VisitGuard(x *ir.GuardInstr) {
	a := g.asm
	fail := x.Fail()
	g.get(Rax, x.Src())
	switch x.Kind() {
	case ir.GuardNonNegative:
		a.Test(Rax, Rax)
		a.Jcc(CcS, &g.labels[fail])
		return
	case ir.GuardNonZero:
		a.Test(Rax, Rax)
		a.Jcc(CcE, &g.labels[fail])
		return
	}
	a.MovImm(Rcx, x.Bound())
	a.Cmp(Rax, Rcx)
	var cc Cc
	switch x.Kind() {
	case ir.GuardSignedAtLeast:
		cc = CcL
	case ir.GuardSignedAtMost:
		cc = CcG
	case ir.GuardUnsignedAtLeast:
		cc = CcB
	case ir.GuardUnsignedAtMost:
		cc = CcA
	default:
		panic(diag.Internal("codegen: guard %d", int(x.Kind())))
	}
	a.Jcc(cc, &g.labels[fail])
}

func (g *codegen) VisitJump(t *ir.JumpTerm) { g.jumpUnlessNext(t.Target()) }

func (g *codegen) VisitBranch(t *ir.BranchTerm) {
	g.get(Rax, t.Cond())
	g.asm.Test(Rax, Rax)
	g.asm.Jcc(CcE, &g.labels[t.IfFalse()])
	g.jumpUnlessNext(t.IfTrue())
}

func (g *codegen) VisitSwitch(t *ir.SwitchTerm) {
	a := g.asm
	g.get(Rax, t.Value())
	for _, c := range t.Cases(g.f) {
		a.MovImm(Rcx, c.Value)
		a.Cmp(Rax, Rcx)
		a.Jcc(CcE, &g.labels[c.Target])
	}
	g.jumpUnlessNext(t.Fallback())
}

func (g *codegen) VisitRet(t *ir.RetTerm) {
	a := g.asm
	g.get(Rax, t.Value())
	if g.f.Decl.ErrorType != nil {
		a.Xor(errorReg, errorReg)
	}
	a.Mov(Rsp, Rbp)
	a.Pop(Rbp)
	a.Ret()
}

func (g *codegen) VisitThrow(t *ir.ThrowTerm) {
	a := g.asm
	g.get(errorReg, t.Err())
	a.Xor(Rax, Rax)
	a.Mov(Rsp, Rbp)
	a.Pop(Rbp)
	a.Ret()
}

func (g *codegen) VisitTrap(*ir.TrapTerm) { g.asm.Raw(trapOpcodeFirst, trapOpcodeNext) }

func (g *codegen) jumpUnlessNext(target ir.BlockID) {
	if target != g.next {
		g.asm.Jmp(&g.labels[target])
	}
}

// The raw operations are the runtime's source's (ir.RuntimeSourceOnly), which no IR the Go compiler lowers has.

func (g *codegen) VisitLoadRaw(*ir.LoadRawInstr) { panic(ir.RuntimeSourceOnly("codegen", "loadraw")) }

func (g *codegen) VisitStoreRaw(*ir.StoreRawInstr) {
	panic(ir.RuntimeSourceOnly("codegen", "storeraw"))
}

func (g *codegen) VisitGlobalAddr(*ir.GlobalAddrInstr) {
	panic(ir.RuntimeSourceOnly("codegen", "globaladdr"))
}

func (g *codegen) VisitCallImport(*ir.CallImportInstr) {
	panic(ir.RuntimeSourceOnly("codegen", "import"))
}
