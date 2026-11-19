package main

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
	f             *IrFunction
	labels        []*Label
}

func EmitModule(fns []*IrFunction) *CodeUnit {
	u := NewCodeUnit()
	g := &codegen{
		u:             u,
		asm:           u.Asm,
		rtAlloc:       ExternLabel(RtAlloc),
		panicRange:    ExternLabel(RtPanicRange),
		panicOverflow: ExternLabel(RtPanicOverflow),
		panicDiv:      ExternLabel(RtPanicDiv),
		panicShift:    ExternLabel(RtPanicShift),
	}
	for _, f := range fns {
		g.function(f)
	}
	return u
}

func slotOffset(slot int) int {
	return -wordBytes * (slot + 1)
}

func (g *codegen) regOffset(v V) int {
	return -wordBytes * (len(g.f.Slots) + int(v) + 1)
}

func (g *codegen) paramOffset(index int) int {
	return 16 + wordBytes*(g.f.ParamCount-1-index)
}

func (g *codegen) get(r R, v V) {
	g.asm.Load(r, Rbp, g.regOffset(v))
}

func (g *codegen) put(v V, r R) {
	g.asm.Store(Rbp, g.regOffset(v), r)
}

func (g *codegen) function(fn *IrFunction) {
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
			panic(Internal("codegen: block b%d of %s has no terminator", b.ID, fn.Decl.Symbol()))
		}
		var next *IrBlock
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

func (g *codegen) call(target *Label, args []V, dst *V) {
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

func (g *codegen) instr(i Instr) {
	a := g.asm
	switch x := i.(type) {
	case *ConstInstr:
		a.MovImm(Rax, x.Value)
		g.put(x.Dst, Rax)
	case *StrInstr:
		a.LeaData(Rax, g.u.Data.String(x.Value))
		g.put(x.Dst, Rax)
	case *LoadSlot:
		a.Load(Rax, Rbp, slotOffset(x.Slot))
		g.put(x.Dst, Rax)
	case *StoreSlot:
		g.get(Rax, x.Src)
		a.Store(Rbp, slotOffset(x.Slot), Rax)
	case *SlotAddr:
		a.Lea(Rax, Rbp, slotOffset(x.Slot))
		g.put(x.Dst, Rax)
	case *LoadParam:
		a.Load(Rax, Rbp, g.paramOffset(x.Param))
		g.put(x.Dst, Rax)
	case *LoadField:
		g.get(Rax, x.Obj)
		a.Load(Rax, Rax, x.Offset)
		g.put(x.Dst, Rax)
	case *StoreField:
		g.get(Rcx, x.Obj)
		g.get(Rax, x.Src)
		a.Store(Rcx, x.Offset, Rax)
	case *FieldAddr:
		g.get(Rax, x.Obj)
		a.Lea(Rax, Rax, x.Offset)
		g.put(x.Dst, Rax)
	case *LoadAt:
		g.get(Rax, x.Addr)
		a.Load(Rax, Rax, 0)
		g.put(x.Dst, Rax)
	case *StoreAt:
		g.get(Rcx, x.Addr)
		g.get(Rax, x.Src)
		a.Store(Rcx, 0, Rax)
	case *LoadGlobal:
		a.LoadSymbol(Rax, x.Symbol)
		g.put(x.Dst, Rax)
	case *BinInstr:
		g.get(Rax, x.A)
		g.get(Rcx, x.B)
		g.binary(x)
		g.put(x.Dst, Rax)
	case *CmpInstr:
		g.get(Rax, x.A)
		g.get(Rcx, x.B)
		a.Cmp(Rax, Rcx)
		a.SetccRax(g.condition(x))
		g.put(x.Dst, Rax)
	case *NegInstr:
		g.get(Rax, x.Src)
		a.Neg(Rax)
		a.Jcc(CcO, g.panicOverflow)
		g.put(x.Dst, Rax)
	case *NotInstr:
		g.get(Rax, x.Src)
		a.Not(Rax)
		g.put(x.Dst, Rax)
	case *GuardInstr:
		g.guard(x)
	case *AllocInstr:
		a.MovImm(Rax, int64(x.Bytes))
		a.Push(Rax)
		a.Call(g.rtAlloc)
		a.AddImm(Rsp, wordBytes)
		g.put(x.Dst, Rax)
	case *CallInstr:
		g.call(ExternLabel(x.Target), x.Args, x.Dst)
	case *CallRuntimeInstr:
		g.call(ExternLabel(x.Symbol), x.Args, x.Dst)
	default:
		panic(Internal("codegen: unhandled instruction %T", i))
	}
}

func (g *codegen) condition(c *CmpInstr) Cc {
	switch c.Op {
	case CmpEq:
		return CcE
	case CmpNe:
		return CcNE
	case CmpLt:
		if c.Unsigned {
			return CcB
		}
		return CcL
	case CmpLe:
		if c.Unsigned {
			return CcBE
		}
		return CcLE
	case CmpGt:
		if c.Unsigned {
			return CcA
		}
		return CcG
	case CmpGe:
		if c.Unsigned {
			return CcAE
		}
		return CcGE
	}
	panic(Internal("codegen: comparison %d", int(c.Op)))
}

func (g *codegen) binary(b *BinInstr) {
	a := g.asm
	switch b.Op {
	case OpAdd:
		a.Add(Rax, Rcx)
		g.overflow(b)
	case OpSub:
		a.Sub(Rax, Rcx)
		g.overflow(b)
	case OpMul:
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
	case OpDiv, OpMod:
		a.Test(Rcx, Rcx)
		a.Jcc(CcE, g.panicDiv)
		if b.Unsigned {
			a.Xor(Rdx, Rdx)
			a.Div(Rcx)
		} else {
			a.Cqo()
			a.Idiv(Rcx)
		}
		if b.Op == OpMod {
			a.Mov(Rax, Rdx)
		}
	case OpAnd:
		a.And(Rax, Rcx)
	case OpOr:
		a.Or(Rax, Rcx)
	case OpXor:
		a.Xor(Rax, Rcx)
	case OpShl, OpShr:
		g.shift(b.Op == OpShl, b.Unsigned)
	default:
		panic(Internal("codegen: operator %d", int(b.Op)))
	}
}

func (g *codegen) overflow(b *BinInstr) {
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

func (g *codegen) guard(x *GuardInstr) {
	a := g.asm
	g.get(Rax, x.Src)
	if x.Kind == GuardNonNegative {
		a.Test(Rax, Rax)
		a.Jcc(CcS, g.panicRange)
		return
	}
	a.MovImm(Rcx, x.Bound)
	a.Cmp(Rax, Rcx)
	var cc Cc
	switch x.Kind {
	case GuardSignedAtLeast:
		cc = CcL
	case GuardSignedAtMost:
		cc = CcG
	case GuardUnsignedAtLeast:
		cc = CcB
	case GuardUnsignedAtMost:
		cc = CcA
	default:
		panic(Internal("codegen: guard %d", int(x.Kind)))
	}
	a.Jcc(cc, g.panicRange)
}

func (g *codegen) term(t Terminator, next *IrBlock) {
	a := g.asm
	switch x := t.(type) {
	case *JumpTerm:
		g.jumpUnlessNext(x.Target, next)
	case *BranchTerm:
		g.get(Rax, x.Cond)
		a.Test(Rax, Rax)
		a.Jcc(CcE, g.labels[x.Else.ID])
		g.jumpUnlessNext(x.Then, next)
	case *SwitchTerm:
		g.get(Rax, x.Value)
		for i, v := range x.Values {
			a.MovImm(Rcx, v)
			a.Cmp(Rax, Rcx)
			a.Jcc(CcE, g.labels[x.Targets[i].ID])
		}
		g.jumpUnlessNext(x.Default, next)
	case *RetTerm:
		g.get(Rax, x.Value)
		a.Mov(Rsp, Rbp)
		a.Pop(Rbp)
		a.Ret()
	case *TrapTerm:
		a.Raw(trapOpcodeFirst, trapOpcodeNext)
	default:
		panic(Internal("codegen: unhandled terminator %T", t))
	}
}

func (g *codegen) jumpUnlessNext(target, next *IrBlock) {
	if target != next {
		g.asm.Jmp(g.labels[target.ID])
	}
}
