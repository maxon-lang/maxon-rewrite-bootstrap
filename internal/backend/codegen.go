package backend

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"minimaxon/internal/ir"
)

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
	u             *CodeUnit
	asm           *Asm
	rtAlloc       *Label
	panicRange    *Label
	panicOverflow *Label
	panicDiv      *Label
	panicShift    *Label
	f             *ir.IrFunction
	labels        []Label
}

func EmitModule(fns []*ir.IrFunction) *CodeUnit {
	u := NewCodeUnit()
	instrs, symbols, data := 0, 0, 0
	for _, f := range fns {
		instrs += len(f.Instrs) + len(f.Blocks)
		for _, in := range f.Instrs {
			symbols += symbolFixups(in)
			if in.Op == ir.OpStr {
				data++
			}
		}
	}
	u.Asm.Code = make([]byte, 0, instrs*estimatedCodeBytesPerInstr)
	u.Asm.SymbolFixups = make([]SymbolFixup, 0, symbols)
	u.Asm.DataFixups = make([]DataFixup, 0, data)
	u.Sections = make([]Section, 0, len(fns))
	u.CodeSymbols = make([]SymbolDef, 0, len(fns))
	g := &codegen{
		u:             u,
		asm:           u.Asm,
		rtAlloc:       ExternLabel(ir.RtAlloc),
		panicRange:    ExternLabel(ir.RtPanicRange),
		panicOverflow: ExternLabel(ir.RtPanicOverflow),
		panicDiv:      ExternLabel(ir.RtPanicDiv),
		panicShift:    ExternLabel(ir.RtPanicShift),
	}
	for _, f := range fns {
		g.function(f)
	}
	return u
}

// symbolFixups is how many references to a symbol the code of an instruction holds: a call, a global, or a jump to a
// trap that the runtime defines. The unit's fixups are sized from it, and from the strings it loads, because a slice grown
// by appending allocates several times its final size.
func symbolFixups(in ir.Instr) int {
	switch in.Op {
	case ir.OpLoadGlobal, ir.OpCall, ir.OpCallRuntime, ir.OpAlloc, ir.OpDrop, ir.OpDropSlot, ir.OpDropAt, ir.OpCopy,
		ir.OpNeg, ir.OpGuard:
		return 1
	case ir.OpBin:
		switch in.BinOp() {
		case ir.BinAdd, ir.BinSub, ir.BinMul:
			if in.Has(ir.FlagChecked) {
				return 1
			}
			return 0
		case ir.BinDiv, ir.BinMod, ir.BinShl, ir.BinShr:
			return 1
		}
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
	g.u.DefineCode(fn.Decl)
	a.Push(Rbp)
	a.Mov(Rbp, Rsp)
	g.allocateFrame((len(fn.Slots) + len(fn.Regs)) * wordBytes)
	g.zeroManagedSlots()

	for i := range fn.Blocks {
		a.Bind(&g.labels[i])
		for _, instr := range fn.BlockInstrs(i) {
			g.instr(instr)
		}
		next := ir.BlockID(-1)
		if i+1 < len(fn.Blocks) {
			next = ir.BlockID(i + 1)
		}
		g.term(fn.Blocks[i].Term, next)
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

func (g *codegen) call(symbol any, args []ir.V, dst ir.V) {
	a := g.asm
	for _, arg := range args {
		g.get(Rax, arg)
		a.Push(Rax)
	}
	a.CallSymbol(symbol)
	if len(args) > 0 {
		a.AddImm(Rsp, len(args)*wordBytes)
	}
	if dst != ir.NoReg {
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

func (g *codegen) instr(x ir.Instr) {
	a := g.asm
	switch x.Op {
	case ir.OpConst:
		a.MovImm(Rax, x.Imm)
		g.put(x.Dst, Rax)
	case ir.OpStr:
		a.LeaData(Rax, g.u.Data.String(g.f.Text(x)))
		g.put(x.Dst, Rax)
	case ir.OpLoadSlot:
		a.Load(Rax, Rbp, slotOffset(x.Slot()))
		g.put(x.Dst, Rax)
	case ir.OpMoveSlot:
		a.Load(Rax, Rbp, slotOffset(x.Slot()))
		g.put(x.Dst, Rax)
		a.Xor(Rcx, Rcx)
		a.Store(Rbp, slotOffset(x.Slot()), Rcx)
	case ir.OpStoreSlot:
		g.get(Rax, x.A)
		a.Store(Rbp, slotOffset(x.Slot()), Rax)
	case ir.OpSlotAddr:
		a.Lea(Rax, Rbp, slotOffset(x.Slot()))
		g.put(x.Dst, Rax)
	case ir.OpLoadParam:
		a.Load(Rax, Rbp, g.paramOffset(int(x.Imm)))
		g.put(x.Dst, Rax)
	case ir.OpLoadField:
		g.get(Rax, x.A)
		a.Load(Rax, Rax, x.Offset())
		g.put(x.Dst, Rax)
	case ir.OpStoreField:
		g.get(Rcx, x.A)
		g.get(Rax, x.B)
		a.Store(Rcx, x.Offset(), Rax)
	case ir.OpFieldAddr:
		g.get(Rax, x.A)
		a.Lea(Rax, Rax, x.Offset())
		g.put(x.Dst, Rax)
	case ir.OpLoadAt:
		g.get(Rax, x.A)
		a.Load(Rax, Rax, 0)
		g.put(x.Dst, Rax)
	case ir.OpMoveAt:
		g.get(Rcx, x.A)
		a.Load(Rax, Rcx, 0)
		g.put(x.Dst, Rax)
		a.Xor(Rdx, Rdx)
		a.Store(Rcx, 0, Rdx)
	case ir.OpDrop:
		g.get(Rax, x.A)
		g.callRoutine(dropRoutine, g.f.TypeOf(x))
	case ir.OpDropSlot:
		a.Load(Rax, Rbp, slotOffset(x.Slot()))
		a.Xor(Rcx, Rcx)
		a.Store(Rbp, slotOffset(x.Slot()), Rcx)
		g.callRoutine(dropRoutine, g.f.TypeOf(x))
	case ir.OpDropAt:
		g.get(Rcx, x.A)
		a.Load(Rax, Rcx, 0)
		a.Xor(Rdx, Rdx)
		a.Store(Rcx, 0, Rdx)
		g.callRoutine(dropRoutine, g.f.TypeOf(x))
	case ir.OpCopy:
		g.get(Rax, x.A)
		g.callRoutine(copyRoutine, g.f.TypeOf(x))
		g.put(x.Dst, Rax)
	case ir.OpStoreAt:
		g.get(Rcx, x.A)
		g.get(Rax, x.B)
		a.Store(Rcx, 0, Rax)
	case ir.OpLoadGlobal:
		a.LoadSymbol(Rax, x.Symbol())
		g.put(x.Dst, Rax)
	case ir.OpBin:
		g.get(Rax, x.A)
		g.get(Rcx, x.B)
		g.binary(x)
		g.put(x.Dst, Rax)
	case ir.OpCmp:
		g.get(Rax, x.A)
		g.get(Rcx, x.B)
		a.Cmp(Rax, Rcx)
		a.SetccRax(g.condition(x))
		g.put(x.Dst, Rax)
	case ir.OpNeg:
		g.get(Rax, x.A)
		a.Neg(Rax)
		a.Jcc(CcO, g.panicOverflow)
		g.put(x.Dst, Rax)
	case ir.OpNot:
		g.get(Rax, x.A)
		a.Not(Rax)
		g.put(x.Dst, Rax)
	case ir.OpGuard:
		g.guard(x)
	case ir.OpAlloc:
		a.MovImm(Rax, x.Imm)
		a.Push(Rax)
		a.Call(g.rtAlloc)
		a.AddImm(Rsp, wordBytes)
		g.put(x.Dst, Rax)
	case ir.OpCall:
		g.call(g.f.Callee(x), g.f.CallArgs(x), x.Dst)
	case ir.OpCallRuntime:
		g.call(x.Symbol(), g.f.CallArgs(x), x.Dst)
	case ir.OpWhen:
		panic(diag.Internal("codegen: a behaviour has no native code, and the driver refuses it before code is generated"))
	default:
		panic(diag.Internal("codegen: unhandled opcode %d", int(x.Op)))
	}
}

func (g *codegen) condition(c ir.Instr) Cc {
	switch c.CmpOp() {
	case ir.CmpEq:
		return CcE
	case ir.CmpNe:
		return CcNE
	case ir.CmpLt:
		if c.Has(ir.FlagUnsigned) {
			return CcB
		}
		return CcL
	case ir.CmpLe:
		if c.Has(ir.FlagUnsigned) {
			return CcBE
		}
		return CcLE
	case ir.CmpGt:
		if c.Has(ir.FlagUnsigned) {
			return CcA
		}
		return CcG
	case ir.CmpGe:
		if c.Has(ir.FlagUnsigned) {
			return CcAE
		}
		return CcGE
	}
	panic(diag.Internal("codegen: comparison %d", int(c.Sub)))
}

func (g *codegen) binary(b ir.Instr) {
	a := g.asm
	unsigned := b.Has(ir.FlagUnsigned)
	checked := b.Has(ir.FlagChecked)
	switch b.BinOp() {
	case ir.BinAdd:
		a.Add(Rax, Rcx)
		g.overflow(unsigned, checked)
	case ir.BinSub:
		a.Sub(Rax, Rcx)
		g.overflow(unsigned, checked)
	case ir.BinMul:
		if unsigned {
			a.MulRax(Rcx)
			if checked {
				a.Jcc(CcB, g.panicOverflow)
			}
		} else {
			a.Imul(Rax, Rcx)
			if checked {
				a.Jcc(CcO, g.panicOverflow)
			}
		}
	case ir.BinDiv, ir.BinMod:
		a.Test(Rcx, Rcx)
		a.Jcc(CcE, g.panicDiv)
		if unsigned {
			a.Xor(Rdx, Rdx)
			a.Div(Rcx)
		} else {
			a.Cqo()
			a.Idiv(Rcx)
		}
		if b.BinOp() == ir.BinMod {
			a.Mov(Rax, Rdx)
		}
	case ir.BinAnd:
		a.And(Rax, Rcx)
	case ir.BinOr:
		a.Or(Rax, Rcx)
	case ir.BinXor:
		a.Xor(Rax, Rcx)
	case ir.BinShl, ir.BinShr:
		g.shift(b.BinOp() == ir.BinShl, unsigned)
	default:
		panic(diag.Internal("codegen: operator %d", int(b.Sub)))
	}
}

func (g *codegen) overflow(unsigned, checked bool) {
	if !checked {
		return
	}
	if unsigned {
		g.asm.Jcc(CcB, g.panicOverflow)
	} else {
		g.asm.Jcc(CcO, g.panicOverflow)
	}
}

func (g *codegen) shift(left, unsigned bool) {
	a := g.asm
	big := NewLabel()
	done := NewLabel()
	a.CmpImm(Rcx, 0)
	a.Jcc(CcL, g.panicShift)
	a.CmpImm(Rcx, shiftBitCount)
	a.Jcc(CcGE, big)
	if left {
		a.ShlCl(Rax)
	} else if unsigned {
		a.ShrCl(Rax)
	} else {
		a.SarCl(Rax)
	}
	a.Jmp(done)
	a.Bind(big)
	if !left && !unsigned {
		a.SarImm(Rax, signBitShift)
	} else {
		a.Xor(Rax, Rax)
	}
	a.Bind(done)
}

func (g *codegen) guard(x ir.Instr) {
	a := g.asm
	g.get(Rax, x.A)
	if x.GuardKind() == ir.GuardNonNegative {
		a.Test(Rax, Rax)
		a.Jcc(CcS, g.panicRange)
		return
	}
	a.MovImm(Rcx, x.Imm)
	a.Cmp(Rax, Rcx)
	var cc Cc
	switch x.GuardKind() {
	case ir.GuardSignedAtLeast:
		cc = CcL
	case ir.GuardSignedAtMost:
		cc = CcG
	case ir.GuardUnsignedAtLeast:
		cc = CcB
	case ir.GuardUnsignedAtMost:
		cc = CcA
	default:
		panic(diag.Internal("codegen: guard %d", int(x.Sub)))
	}
	a.Jcc(cc, g.panicRange)
}

func (g *codegen) term(t ir.Term, next ir.BlockID) {
	a := g.asm
	switch t.Kind {
	case ir.TermJump:
		g.jumpUnlessNext(t.Then, next)
	case ir.TermBranch:
		g.get(Rax, t.Value)
		a.Test(Rax, Rax)
		a.Jcc(CcE, &g.labels[t.Else])
		g.jumpUnlessNext(t.Then, next)
	case ir.TermSwitch:
		g.get(Rax, t.Value)
		for _, c := range g.f.Cases[t.First : t.First+t.Count] {
			a.MovImm(Rcx, c.Value)
			a.Cmp(Rax, Rcx)
			a.Jcc(CcE, &g.labels[c.Target])
		}
		g.jumpUnlessNext(t.Then, next)
	case ir.TermRet:
		g.get(Rax, t.Value)
		a.Mov(Rsp, Rbp)
		a.Pop(Rbp)
		a.Ret()
	case ir.TermTrap:
		a.Raw(trapOpcodeFirst, trapOpcodeNext)
	default:
		panic(diag.Internal("codegen: block of %s has no terminator", g.f.Decl.Symbol()))
	}
}

func (g *codegen) jumpUnlessNext(target, next ir.BlockID) {
	if target != next {
		g.asm.Jmp(&g.labels[target])
	}
}
