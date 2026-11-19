package backend

import (
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
	labels        []*Label
}

func EmitModule(fns []*ir.IrFunction) *CodeUnit {
	u := NewCodeUnit()
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
	g.labels = make([]*Label, len(fn.Blocks))
	for i := range g.labels {
		g.labels[i] = NewLabel()
	}
	g.u.DefineCode(fn.Decl)
	a.Push(Rbp)
	a.Mov(Rbp, Rsp)
	g.allocateFrame((len(fn.Slots) + len(fn.Regs)) * wordBytes)

	for i, b := range fn.Blocks {
		a.Bind(g.labels[i])
		for _, instr := range b.Instrs {
			g.instr(instr)
		}
		if b.Term == nil {
			panic(diag.Internal("codegen: block b%d of %s has no terminator", b.ID, fn.Decl.Symbol()))
		}
		var next *ir.IrBlock
		if i+1 < len(fn.Blocks) {
			next = fn.Blocks[i+1]
		}
		g.term(b.Term, next)
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

func (g *codegen) call(target *Label, args []ir.V, dst *ir.V) {
	a := g.asm
	for _, arg := range args {
		g.get(Rax, arg)
		a.Push(Rax)
	}
	a.Call(target)
	if len(args) > 0 {
		a.AddImm(Rsp, len(args)*wordBytes)
	}
	if dst != nil {
		g.put(*dst, Rax)
	}
}

func (g *codegen) instr(i ir.Instr) {
	a := g.asm
	switch x := i.(type) {
	case *ir.ConstInstr:
		a.MovImm(Rax, x.Value)
		g.put(x.Dst, Rax)
	case *ir.StrInstr:
		a.LeaData(Rax, g.u.Data.String(x.Value))
		g.put(x.Dst, Rax)
	case *ir.LoadSlot:
		a.Load(Rax, Rbp, slotOffset(x.Slot))
		g.put(x.Dst, Rax)
	case *ir.StoreSlot:
		g.get(Rax, x.Src)
		a.Store(Rbp, slotOffset(x.Slot), Rax)
	case *ir.SlotAddr:
		a.Lea(Rax, Rbp, slotOffset(x.Slot))
		g.put(x.Dst, Rax)
	case *ir.LoadParam:
		a.Load(Rax, Rbp, g.paramOffset(x.Param))
		g.put(x.Dst, Rax)
	case *ir.LoadField:
		g.get(Rax, x.Obj)
		a.Load(Rax, Rax, x.Offset)
		g.put(x.Dst, Rax)
	case *ir.StoreField:
		g.get(Rcx, x.Obj)
		g.get(Rax, x.Src)
		a.Store(Rcx, x.Offset, Rax)
	case *ir.FieldAddr:
		g.get(Rax, x.Obj)
		a.Lea(Rax, Rax, x.Offset)
		g.put(x.Dst, Rax)
	case *ir.LoadAt:
		g.get(Rax, x.Addr)
		a.Load(Rax, Rax, 0)
		g.put(x.Dst, Rax)
	case *ir.StoreAt:
		g.get(Rcx, x.Addr)
		g.get(Rax, x.Src)
		a.Store(Rcx, 0, Rax)
	case *ir.LoadGlobal:
		a.LoadSymbol(Rax, x.Symbol)
		g.put(x.Dst, Rax)
	case *ir.BinInstr:
		g.get(Rax, x.A)
		g.get(Rcx, x.B)
		g.binary(x)
		g.put(x.Dst, Rax)
	case *ir.CmpInstr:
		g.get(Rax, x.A)
		g.get(Rcx, x.B)
		a.Cmp(Rax, Rcx)
		a.SetccRax(g.condition(x))
		g.put(x.Dst, Rax)
	case *ir.NegInstr:
		g.get(Rax, x.Src)
		a.Neg(Rax)
		a.Jcc(CcO, g.panicOverflow)
		g.put(x.Dst, Rax)
	case *ir.NotInstr:
		g.get(Rax, x.Src)
		a.Not(Rax)
		g.put(x.Dst, Rax)
	case *ir.GuardInstr:
		g.guard(x)
	case *ir.AllocInstr:
		a.MovImm(Rax, int64(x.Bytes))
		a.Push(Rax)
		a.Call(g.rtAlloc)
		a.AddImm(Rsp, wordBytes)
		g.put(x.Dst, Rax)
	case *ir.CallInstr:
		g.call(ExternLabel(x.Target), x.Args, x.Dst)
	case *ir.CallRuntimeInstr:
		g.call(ExternLabel(x.Symbol), x.Args, x.Dst)
	default:
		panic(diag.Internal("codegen: unhandled instruction %T", i))
	}
}

func (g *codegen) condition(c *ir.CmpInstr) Cc {
	switch c.Op {
	case ir.CmpEq:
		return CcE
	case ir.CmpNe:
		return CcNE
	case ir.CmpLt:
		if c.Unsigned {
			return CcB
		}
		return CcL
	case ir.CmpLe:
		if c.Unsigned {
			return CcBE
		}
		return CcLE
	case ir.CmpGt:
		if c.Unsigned {
			return CcA
		}
		return CcG
	case ir.CmpGe:
		if c.Unsigned {
			return CcAE
		}
		return CcGE
	}
	panic(diag.Internal("codegen: comparison %d", int(c.Op)))
}

func (g *codegen) binary(b *ir.BinInstr) {
	a := g.asm
	switch b.Op {
	case ir.OpAdd:
		a.Add(Rax, Rcx)
		g.overflow(b)
	case ir.OpSub:
		a.Sub(Rax, Rcx)
		g.overflow(b)
	case ir.OpMul:
		if b.Unsigned {
			a.MulRax(Rcx)
			if b.Checked {
				a.Jcc(CcB, g.panicOverflow)
			}
		} else {
			a.Imul(Rax, Rcx)
			if b.Checked {
				a.Jcc(CcO, g.panicOverflow)
			}
		}
	case ir.OpDiv, ir.OpMod:
		a.Test(Rcx, Rcx)
		a.Jcc(CcE, g.panicDiv)
		if b.Unsigned {
			a.Xor(Rdx, Rdx)
			a.Div(Rcx)
		} else {
			a.Cqo()
			a.Idiv(Rcx)
		}
		if b.Op == ir.OpMod {
			a.Mov(Rax, Rdx)
		}
	case ir.OpAnd:
		a.And(Rax, Rcx)
	case ir.OpOr:
		a.Or(Rax, Rcx)
	case ir.OpXor:
		a.Xor(Rax, Rcx)
	case ir.OpShl, ir.OpShr:
		g.shift(b.Op == ir.OpShl, b.Unsigned)
	default:
		panic(diag.Internal("codegen: operator %d", int(b.Op)))
	}
}

func (g *codegen) overflow(b *ir.BinInstr) {
	if !b.Checked {
		return
	}
	if b.Unsigned {
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

func (g *codegen) guard(x *ir.GuardInstr) {
	a := g.asm
	g.get(Rax, x.Src)
	if x.Kind == ir.GuardNonNegative {
		a.Test(Rax, Rax)
		a.Jcc(CcS, g.panicRange)
		return
	}
	a.MovImm(Rcx, x.Bound)
	a.Cmp(Rax, Rcx)
	var cc Cc
	switch x.Kind {
	case ir.GuardSignedAtLeast:
		cc = CcL
	case ir.GuardSignedAtMost:
		cc = CcG
	case ir.GuardUnsignedAtLeast:
		cc = CcB
	case ir.GuardUnsignedAtMost:
		cc = CcA
	default:
		panic(diag.Internal("codegen: guard %d", int(x.Kind)))
	}
	a.Jcc(cc, g.panicRange)
}

func (g *codegen) term(t ir.Terminator, next *ir.IrBlock) {
	a := g.asm
	switch x := t.(type) {
	case *ir.JumpTerm:
		g.jumpUnlessNext(x.Target, next)
	case *ir.BranchTerm:
		g.get(Rax, x.Cond)
		a.Test(Rax, Rax)
		a.Jcc(CcE, g.labels[x.Else.ID])
		g.jumpUnlessNext(x.Then, next)
	case *ir.SwitchTerm:
		g.get(Rax, x.Value)
		for i, v := range x.Values {
			a.MovImm(Rcx, v)
			a.Cmp(Rax, Rcx)
			a.Jcc(CcE, g.labels[x.Targets[i].ID])
		}
		g.jumpUnlessNext(x.Default, next)
	case *ir.RetTerm:
		g.get(Rax, x.Value)
		a.Mov(Rsp, Rbp)
		a.Pop(Rbp)
		a.Ret()
	case *ir.TrapTerm:
		a.Raw(trapOpcodeFirst, trapOpcodeNext)
	default:
		panic(diag.Internal("codegen: unhandled terminator %T", t))
	}
}

func (g *codegen) jumpUnlessNext(target, next *ir.IrBlock) {
	if target != next {
		g.asm.Jmp(g.labels[target.ID])
	}
}
