package main

import "sort"

const lowerWord = 8

type jumpTargets struct {
	brk  *IrBlock
	cont *IrBlock
}

type Lowerer struct {
	fn    *FuncDecl
	f     *IrFunction
	jumps map[Node]jumpTargets
	cur   *IrBlock
}

var lowerBinOps = map[string]BinOp{
	"+": OpAdd, "-": OpSub, "*": OpMul, "/": OpDiv, "mod": OpMod,
	"and": OpAnd, "or": OpOr, "xor": OpXor, "shl": OpShl, "shr": OpShr,
}

var lowerCmpOps = map[string]CmpOp{
	"==": CmpEq, "!=": CmpNe, "<": CmpLt, "<=": CmpLe, ">": CmpGt, ">=": CmpGe,
}

func newLowerer(fn *FuncDecl) *Lowerer {
	paramCount := len(fn.Params)
	if fn.IsMethod() {
		paramCount++
	}
	l := &Lowerer{fn: fn, f: &IrFunction{Decl: fn, ParamCount: paramCount}, jumps: map[Node]jumpTargets{}}
	var locals []*Local
	for _, loc := range fn.Locals {
		if !loc.IsParam {
			locals = append(locals, loc)
		}
	}
	sort.SliceStable(locals, func(i, j int) bool { return locals[i].Slot < locals[j].Slot })
	for _, loc := range locals {
		if loc.Slot != len(l.f.Slots) {
			panic(Internal("lower: local slots of %s are not dense", fn.Symbol()))
		}
		l.f.NewSlot(kindOf(loc.Type))
	}
	l.cur = l.f.NewBlock()
	return l
}

func Lower(fn *FuncDecl) *IrFunction {
	l := newLowerer(fn)
	l.block(fn.Body)
	if l.cur.Term == nil {
		zero := l.konst(0)
		l.cur.Term = &RetTerm{Value: zero}
	}
	l.f.RemoveUnreachable()
	return l.f
}

func kindOf(t Ty) IrKind {
	if t.IsManaged() {
		return KindManaged
	}
	return KindScalar
}

func (l *Lowerer) emit(i Instr) {
	l.cur.Instrs = append(l.cur.Instrs, i)
}

func (l *Lowerer) seal(t Terminator) {
	if l.cur.Term == nil {
		l.cur.Term = t
	}
	l.cur = l.f.NewBlock()
}

func (l *Lowerer) goTo(target *IrBlock) {
	if l.cur.Term == nil {
		l.cur.Term = &JumpTerm{Target: target}
	}
	l.cur = target
}

func (l *Lowerer) reg(kind IrKind) V {
	return l.f.NewReg(kind)
}

func (l *Lowerer) konst(value int64) V {
	d := l.reg(KindScalar)
	l.emit(&ConstInstr{Dst: d, Value: value})
	return d
}

func (l *Lowerer) str(value string) V {
	d := l.reg(KindManaged)
	l.emit(&StrInstr{Dst: d, Value: value})
	return d
}

func (l *Lowerer) param(index int, kind IrKind) V {
	d := l.reg(kind)
	l.emit(&LoadParam{Dst: d, Param: index})
	return d
}

func (l *Lowerer) self() V {
	return l.param(l.fn.SelfLocal.ParamIndex, KindManaged)
}

func (l *Lowerer) runtime(symbol string, kind IrKind, args ...V) V {
	d := l.reg(kind)
	l.emit(&CallRuntimeInstr{Dst: &d, Symbol: symbol, Args: args})
	return d
}

func (l *Lowerer) block(body []Stmt) {
	for _, s := range body {
		l.stmt(s)
	}
}

func (l *Lowerer) stmt(s Stmt) {
	switch s := s.(type) {
	case *LetStmt:
		v := l.expr(s.Init)
		l.emit(&StoreSlot{Slot: s.Sym.Slot, Src: v})
	case *AssignStmt:
		l.assign(s)
	case *DiscardStmt:
		l.expr(s.Value)
	case *ExprStmt:
		l.expr(s.E)
	case *IfStmt:
		then := l.f.NewBlock()
		end := l.f.NewBlock()
		els := end
		if s.HasElse {
			els = l.f.NewBlock()
		}
		cond := l.expr(s.Cond)
		l.cur.Term = &BranchTerm{Cond: cond, Then: then, Else: els}
		l.cur = then
		l.block(s.Then)
		l.goTo(end)
		if s.HasElse {
			l.cur = els
			l.block(s.Else)
			l.goTo(end)
		}
		l.cur = end
	case *WhileStmt:
		top := l.f.NewBlock()
		body := l.f.NewBlock()
		end := l.f.NewBlock()
		l.jumps[s] = jumpTargets{brk: end, cont: top}
		l.goTo(top)
		cond := l.expr(s.Cond)
		l.cur.Term = &BranchTerm{Cond: cond, Then: body, Else: end}
		l.cur = body
		l.block(s.Body)
		l.goTo(top)
		l.cur = end
	case *ForStmt:
		l.forLoop(s)
	case *MatchStmt:
		l.match(s.Match, nil)
	case *ReturnStmt:
		var v V
		if s.Value == nil {
			v = l.konst(0)
		} else {
			v = l.expr(s.Value)
		}
		l.seal(&RetTerm{Value: v})
	case *BreakStmt:
		l.seal(&JumpTerm{Target: l.jumps[s.Target].brk})
	case *ContinueStmt:
		l.seal(&JumpTerm{Target: l.jumps[s.Target].cont})
	default:
		panic(Internal("lower: unhandled statement %T", s))
	}
}

func (l *Lowerer) assign(a *AssignStmt) {
	switch t := a.Target.(type) {
	case *NameExpr:
		switch t.Kind {
		case NameLocal:
			v := l.expr(a.Value)
			loc := t.Local
			if loc.ByRef {
				addr := l.param(loc.ParamIndex, KindScalar)
				l.emit(&StoreAt{Addr: addr, Src: v})
			} else {
				l.emit(&StoreSlot{Slot: loc.Slot, Src: v})
			}
			return
		case NameSelfField:
			v := l.expr(a.Value)
			obj := l.self()
			l.emit(&StoreField{Obj: obj, Offset: lowerWord * t.Field.Index, Src: v})
			return
		}
	case *MemberExpr:
		if t.Kind == MemberField {
			obj := l.expr(t.Obj)
			v := l.expr(a.Value)
			l.emit(&StoreField{Obj: obj, Offset: lowerWord * t.Field.Index, Src: v})
			return
		}
	}
	panic(Internal("lower: unhandled assignment target"))
}

func (l *Lowerer) forLoop(s *ForStmt) {
	slot := s.Sym.Slot
	limit := l.f.NewSlot(KindScalar)
	unsigned := isUnsignedInt(s.Sym.Type)
	body := l.f.NewBlock()
	cont := l.f.NewBlock()
	step := l.f.NewBlock()
	exit := l.f.NewBlock()
	l.jumps[s] = jumpTargets{brk: exit, cont: cont}

	from := l.expr(s.From)
	l.emit(&StoreSlot{Slot: slot, Src: from})
	to := l.expr(s.To)
	l.emit(&StoreSlot{Slot: limit, Src: to})
	pastOp := CmpGe
	if s.Inclusive {
		pastOp = CmpGt
	}
	pastA := l.load(slot)
	pastB := l.load(limit)
	past := l.compare(pastOp, pastA, pastB, unsigned)
	l.cur.Term = &BranchTerm{Cond: past, Then: exit, Else: body}

	l.cur = body
	l.block(s.Body)
	l.goTo(cont)

	if s.Inclusive {
		a := l.load(slot)
		b := l.load(limit)
		done := l.compare(CmpEq, a, b, unsigned)
		l.cur.Term = &BranchTerm{Cond: done, Then: exit, Else: step}
		l.cur = step
	} else {
		l.goTo(step)
	}
	next := l.reg(KindScalar)
	cur := l.load(slot)
	one := l.konst(1)
	l.emit(&BinInstr{Dst: next, Op: OpAdd, A: cur, B: one, Unsigned: unsigned, Checked: false})
	l.emit(&StoreSlot{Slot: slot, Src: next})
	if s.Inclusive {
		l.cur.Term = &JumpTerm{Target: body}
	} else {
		lim := l.load(limit)
		over := l.compare(CmpGe, next, lim, unsigned)
		l.cur.Term = &BranchTerm{Cond: over, Then: exit, Else: body}
	}
	l.cur = exit
}

func isUnsignedInt(t Ty) bool {
	it, ok := t.(*IntTy)
	return ok && it.Unsigned()
}

func (l *Lowerer) load(slot int) V {
	d := l.reg(l.f.Slots[slot])
	l.emit(&LoadSlot{Dst: d, Slot: slot})
	return d
}

func (l *Lowerer) compare(op CmpOp, a, b V, unsigned bool) V {
	d := l.reg(KindScalar)
	l.emit(&CmpInstr{Dst: d, Op: op, A: a, B: b, Unsigned: unsigned})
	return d
}

func (l *Lowerer) expr(e Expr) V {
	if c, ok := e.Ty().(*ConstIntTy); ok {
		return l.konst(c.Value.Bits())
	}
	switch e := e.(type) {
	case *IntLit:
		return l.konst(e.Value.Bits())
	case *BoolLit:
		if e.Value {
			return l.konst(1)
		}
		return l.konst(0)
	case *StrLit:
		return l.stringLit(e)
	case *NameExpr:
		return l.name(e)
	case *SelfExpr:
		return l.self()
	case *BinaryExpr:
		return l.binary(e)
	case *UnaryExpr:
		return l.unary(e)
	case *CondExpr:
		cond := l.expr(e.Cond)
		return l.choose(e.Ty(), cond,
			func() V { return l.converted(e.Then, e.Ty()) },
			func() V { return l.converted(e.Else, e.Ty()) })
	case *CallExpr:
		return l.call(e)
	case *MemberExpr:
		return l.member(e)
	case *StructLitExpr:
		values := make([]Expr, len(e.Ordered))
		for i, v := range e.Ordered {
			if v != nil {
				values[i] = v
			} else {
				values[i] = e.Record.Fields[i].Default
			}
		}
		return l.record(values, nil)
	case *CastExpr:
		v := l.expr(e.E)
		l.fit(v, e.Ty().(*IntTy), e.E.Ty())
		return v
	case *MatchExpr:
		return l.match(e.Match, e.Ty())
	case *PanicExpr:
		msg := l.expr(e.Message)
		l.emit(&CallRuntimeInstr{Symbol: RtPanic, Args: []V{msg}})
		l.seal(&TrapTerm{})
		return l.konst(0)
	}
	panic(Internal("lower: unhandled expression %T", e))
}

func (l *Lowerer) choose(typ Ty, cond V, then, els func() V) V {
	result := l.f.NewSlot(kindOf(typ))
	a := l.f.NewBlock()
	b := l.f.NewBlock()
	end := l.f.NewBlock()
	l.cur.Term = &BranchTerm{Cond: cond, Then: a, Else: b}
	l.cur = a
	tv := then()
	l.emit(&StoreSlot{Slot: result, Src: tv})
	l.goTo(end)
	l.cur = b
	ev := els()
	l.emit(&StoreSlot{Slot: result, Src: ev})
	l.goTo(end)
	return l.load(result)
}

func (l *Lowerer) converted(e Expr, target Ty) V {
	v := l.expr(e)
	if ti, ok := target.(*IntTy); ok {
		if _, isInt := e.Ty().(*IntTy); isInt {
			l.fit(v, ti, e.Ty())
		}
	}
	return v
}

func (l *Lowerer) name(n *NameExpr) V {
	switch n.Kind {
	case NameLocal:
		loc := n.Local
		if !loc.IsParam {
			return l.load(loc.Slot)
		}
		if !loc.ByRef {
			return l.param(loc.ParamIndex, kindOf(loc.Type))
		}
		d := l.reg(kindOf(loc.Type))
		addr := l.param(loc.ParamIndex, KindScalar)
		l.emit(&LoadAt{Dst: d, Addr: addr})
		return d
	case NameSelfField:
		d := l.reg(kindOf(n.Field.Type))
		obj := l.self()
		l.emit(&LoadField{Dst: d, Obj: obj, Offset: lowerWord * n.Field.Index})
		return d
	case NameConst:
		return l.expr(n.Const.Value)
	}
	panic(Internal("lower: unresolved name '%s'", n.Name))
}

func (l *Lowerer) address(e Expr) V {
	switch e := e.(type) {
	case *NameExpr:
		switch e.Kind {
		case NameLocal:
			loc := e.Local
			if loc.ByRef {
				return l.param(loc.ParamIndex, KindScalar)
			}
			d := l.reg(KindScalar)
			l.emit(&SlotAddr{Dst: d, Slot: loc.Slot})
			return d
		case NameSelfField:
			d := l.reg(KindScalar)
			obj := l.self()
			l.emit(&FieldAddr{Dst: d, Obj: obj, Offset: lowerWord * e.Field.Index})
			return d
		}
	case *MemberExpr:
		if e.Kind == MemberField {
			obj := l.expr(e.Obj)
			d := l.reg(KindScalar)
			l.emit(&FieldAddr{Dst: d, Obj: obj, Offset: lowerWord * e.Field.Index})
			return d
		}
	}
	panic(Internal("lower: no address for this expression"))
}

func (l *Lowerer) call(c *CallExpr) V {
	switch c.Kind {
	case CallPrint, CallPrintError:
		text := l.expr(c.Ordered[0])
		handle := l.reg(KindScalar)
		symbol := RtStderr
		if c.Kind == CallPrint {
			symbol = RtStdout
		}
		l.emit(&LoadGlobal{Dst: handle, Symbol: symbol})
		l.emit(&CallRuntimeInstr{Symbol: RtWrite, Args: []V{handle, text}})
		return l.konst(0)
	case CallFunction, CallStatic, CallMethod:
		target := c.Target
		var args []V
		if c.Receiver != nil {
			args = append(args, l.expr(c.Receiver))
		}
		for i := range c.Ordered {
			if target.Params[i].Mutable {
				args = append(args, l.address(c.Ordered[i]))
			} else {
				args = append(args, l.converted(c.Ordered[i], target.ParamTypes[i]))
			}
		}
		if target.ReturnType == Ty(VoidTy) {
			l.emit(&CallInstr{Target: target, Args: args})
			return l.konst(0)
		}
		d := l.reg(kindOf(target.ReturnType))
		l.emit(&CallInstr{Dst: &d, Target: target, Args: args})
		return d
	case CallUnionCtor:
		tag := c.CaseIndex
		return l.record(c.Ordered, &tag)
	}
	panic(Internal("lower: unresolved call"))
}

func (l *Lowerer) record(values []Expr, tag *int) V {
	baseOffset := 0
	if tag != nil {
		baseOffset = lowerWord
	}
	p := l.reg(KindManaged)
	l.emit(&AllocInstr{Dst: p, Bytes: baseOffset + lowerWord*max(len(values), 1)})
	if tag != nil {
		t := l.konst(int64(*tag))
		l.emit(&StoreField{Obj: p, Offset: 0, Src: t})
	}
	for i := range values {
		v := l.expr(values[i])
		l.emit(&StoreField{Obj: p, Offset: baseOffset + lowerWord*i, Src: v})
	}
	return p
}

func (l *Lowerer) tag(union V) V {
	d := l.reg(KindScalar)
	l.emit(&LoadField{Dst: d, Obj: union, Offset: 0})
	return d
}

func unionCaseNames(ut *UnionTy) []string {
	names := make([]string, len(ut.Cases))
	for i, c := range ut.Cases {
		names[i] = c.Name
	}
	return names
}

func (l *Lowerer) member(m *MemberExpr) V {
	switch m.Kind {
	case MemberField:
		obj := l.expr(m.Obj)
		d := l.reg(kindOf(m.Field.Type))
		l.emit(&LoadField{Dst: d, Obj: obj, Offset: lowerWord * m.Field.Index})
		return d
	case MemberEnumCase:
		return l.konst(int64(m.CaseIndex))
	case MemberUnionCase:
		tag := m.CaseIndex
		return l.record(nil, &tag)
	case MemberEnumOrdinal:
		return l.expr(m.Obj)
	case MemberEnumName:
		v := l.expr(m.Obj)
		return l.names(v, m.Obj.Ty().(*EnumTy).Cases)
	case MemberEnumRaw:
		et := m.Obj.Ty().(*EnumTy)
		v := l.expr(m.Obj)
		if et.HasExplicitRaw {
			return l.selectValue(v, KindScalar, len(et.Raw), func(i int) V { return l.konst(et.Raw[i].Bits()) })
		}
		return v
	case MemberUnionOrdinal:
		return l.tag(l.expr(m.Obj))
	case MemberUnionName:
		t := l.tag(l.expr(m.Obj))
		return l.names(t, unionCaseNames(m.Obj.Ty().(*UnionTy)))
	}
	panic(Internal("lower: unresolved member"))
}

func (l *Lowerer) names(index V, names []string) V {
	return l.selectValue(index, KindManaged, len(names), func(i int) V { return l.str(names[i]) })
}

func (l *Lowerer) selectValue(index V, kind IrKind, count int, value func(int) V) V {
	result := l.f.NewSlot(kind)
	end := l.f.NewBlock()
	targets := make([]*IrBlock, count)
	for i := 0; i < count; i++ {
		targets[i] = l.f.NewBlock()
	}
	values := make([]int64, count-1)
	for i := range values {
		values[i] = int64(i)
	}
	l.cur.Term = &SwitchTerm{Value: index, Values: values, Targets: targets[:count-1], Default: targets[count-1]}
	for i := 0; i < count; i++ {
		l.cur = targets[i]
		v := value(i)
		l.emit(&StoreSlot{Slot: result, Src: v})
		l.goTo(end)
	}
	return l.load(result)
}

func (l *Lowerer) stringLit(s *StrLit) V {
	if s.IsPlain() {
		text := ""
		for _, p := range s.Parts {
			text += p.(string)
		}
		return l.str(text)
	}
	acc := l.part(s.Parts[0])
	for i := 1; i < len(s.Parts); i++ {
		next := l.part(s.Parts[i])
		acc = l.runtime(RtConcat, KindManaged, acc, next)
	}
	return acc
}

func (l *Lowerer) part(part any) V {
	if text, ok := part.(string); ok {
		return l.str(text)
	}
	e := part.(Expr)
	v := l.expr(e)
	switch t := e.Ty().(type) {
	case *PrimTy:
		if t == StringTy {
			return v
		}
		if t == BoolTy {
			return l.names(v, []string{"false", "true"})
		}
	case *ConstIntTy:
		flag := l.konst(0)
		return l.runtime(RtItoa, KindManaged, v, flag)
	case *IntTy:
		flag := int64(0)
		if t.Unsigned() {
			flag = 1
		}
		flagReg := l.konst(flag)
		return l.runtime(RtItoa, KindManaged, v, flagReg)
	case *EnumTy:
		return l.names(v, t.Cases)
	case *UnionTy:
		return l.names(l.tag(v), unionCaseNames(t))
	}
	panic(Internal("lower: cannot interpolate %s", e.Ty().Name()))
}

func (l *Lowerer) binary(b *BinaryExpr) V {
	if (b.Op == "and" || b.Op == "or") && b.Ty() == Ty(BoolTy) {
		result := l.f.NewSlot(KindScalar)
		rhs := l.f.NewBlock()
		end := l.f.NewBlock()
		lv := l.expr(b.L)
		l.emit(&StoreSlot{Slot: result, Src: lv})
		if b.Op == "and" {
			l.cur.Term = &BranchTerm{Cond: lv, Then: rhs, Else: end}
		} else {
			l.cur.Term = &BranchTerm{Cond: lv, Then: end, Else: rhs}
		}
		l.cur = rhs
		rv := l.expr(b.R)
		l.emit(&StoreSlot{Slot: result, Src: rv})
		l.goTo(end)
		return l.load(result)
	}

	if b.OperandType == Ty(StringTy) {
		left := l.expr(b.L)
		right := l.expr(b.R)
		eq := l.runtime(RtStreq, KindScalar, left, right)
		if b.Op == "!=" {
			return l.flip(eq)
		}
		return eq
	}

	a := l.expr(b.L)
	c := l.expr(b.R)
	unsigned := isUnsignedInt(b.OperandType)
	if cmp, ok := lowerCmpOps[b.Op]; ok {
		return l.compare(cmp, a, c, unsigned)
	}
	d := l.reg(KindScalar)
	op, ok := lowerBinOps[b.Op]
	if !ok {
		panic(Internal("lower: unknown operator '%s'", b.Op))
	}
	l.emit(&BinInstr{Dst: d, Op: op, A: a, B: c, Unsigned: unsigned, Checked: op == OpAdd || op == OpSub || op == OpMul})
	if t, ok := b.Ty().(*IntTy); ok {
		l.rangeCheck(d, t)
	}
	return d
}

func (l *Lowerer) flip(boolean V) V {
	d := l.reg(KindScalar)
	one := l.konst(1)
	l.emit(&BinInstr{Dst: d, Op: OpXor, A: boolean, B: one, Unsigned: false, Checked: false})
	return d
}

func (l *Lowerer) unary(u *UnaryExpr) V {
	v := l.expr(u.E)
	if u.Op == "not" && u.Ty() == Ty(BoolTy) {
		return l.flip(v)
	}
	d := l.reg(KindScalar)
	if u.Op == "not" {
		l.emit(&NotInstr{Dst: d, Src: v})
	} else {
		l.emit(&NegInstr{Dst: d, Src: v})
	}
	l.rangeCheck(d, u.Ty().(*IntTy))
	return d
}

func (l *Lowerer) rangeCheck(v V, t *IntTy) {
	if t.Unsigned() {
		l.fit(v, t, &IntTy{Min: Zero, Max: U64Max})
	} else {
		l.fit(v, t, Anonymous64)
	}
}

func (l *Lowerer) fit(v V, target *IntTy, source Ty) {
	if _, ok := source.(*ConstIntTy); ok {
		return
	}
	src := source.(*IntTy)
	if target.Covers(src) && target.Unsigned() == src.Unsigned() {
		return
	}

	if target.Unsigned() {
		if !src.Unsigned() && src.Min.IsNeg() {
			l.emit(&GuardInstr{Src: v, Kind: GuardNonNegative, Bound: 0})
		}
		if target.Min.Gt(MaxI(src.Min, Zero)) {
			l.emit(&GuardInstr{Src: v, Kind: GuardUnsignedAtLeast, Bound: target.Min.Bits()})
		}
		if target.Max.Lt(src.Max) {
			l.emit(&GuardInstr{Src: v, Kind: GuardUnsignedAtMost, Bound: target.Max.Bits()})
		}
		return
	}

	srcMax := src.Max
	if src.Unsigned() && src.Max.Gt(I64Max) {
		l.emit(&GuardInstr{Src: v, Kind: GuardNonNegative, Bound: 0})
		srcMax = I64Max
	}
	if target.Min.Gt(src.Min) {
		l.emit(&GuardInstr{Src: v, Kind: GuardSignedAtLeast, Bound: target.Min.Bits()})
	}
	if target.Max.Lt(srcMax) {
		l.emit(&GuardInstr{Src: v, Kind: GuardSignedAtMost, Bound: target.Max.Bits()})
	}
}

func (l *Lowerer) match(m *MatchCore, resultType Ty) V {
	st := m.Subject.Ty()
	subject := l.expr(m.Subject)
	end := l.f.NewBlock()
	l.jumps[m] = jumpTargets{brk: end}
	result := -1
	if resultType != nil {
		result = l.f.NewSlot(kindOf(resultType))
	}

	for _, arm := range m.Arms {
		body := l.f.NewBlock()
		next := l.f.NewBlock()
		if arm.IsDefault {
			l.goTo(body)
		} else {
			for _, pat := range arm.Patterns {
				l.test(pat, st, subject, body)
			}
			l.goTo(next)
		}
		l.cur = body

		for _, pat := range arm.Patterns {
			for i, local := range pat.BindingLocals {
				if local != nil {
					d := l.reg(kindOf(local.Type))
					l.emit(&LoadField{Dst: d, Obj: subject, Offset: lowerWord + lowerWord*i})
					l.emit(&StoreSlot{Slot: local.Slot, Src: d})
				}
			}
		}

		switch arm.Kind {
		case ArmThen:
			l.stmt(arm.Action)
		case ArmGives:
			v := l.converted(arm.Value, resultType)
			l.emit(&StoreSlot{Slot: result, Src: v})
		case ArmPanic:
			l.expr(arm.Value)
		default:
			panic(Internal("lower: unhandled match arm kind"))
		}
		l.goTo(end)
		l.cur = next
	}
	if l.cur.Term == nil {
		l.cur.Term = &TrapTerm{}
	}
	l.cur = end
	if result < 0 {
		return l.konst(0)
	}
	return l.load(result)
}

func (l *Lowerer) test(pat *Pattern, st Ty, subject V, body *IrBlock) {
	unsigned := isUnsignedInt(st)
	fail := l.f.NewBlock()
	switch {
	case pat.Kind == PatternCase:
		scrutinee := subject
		if _, ok := st.(*UnionTy); ok {
			scrutinee = l.tag(subject)
		}
		idx := l.konst(int64(pat.CaseIndex))
		cmp := l.compare(CmpEq, scrutinee, idx, false)
		l.cur.Term = &BranchTerm{Cond: cmp, Then: body, Else: fail}
	case pat.Kind == PatternLiteral && st == Ty(StringTy):
		testBlock := l.cur
		lit := l.expr(pat.Value)
		eq := l.runtime(RtStreq, KindScalar, subject, lit)
		testBlock.Term = &BranchTerm{Cond: eq, Then: body, Else: fail}
	case pat.Kind == PatternLiteral:
		var value int64
		if bl, ok := pat.Value.(*BoolLit); ok {
			if bl.Value {
				value = 1
			}
		} else {
			value = pat.Value.Ty().(*ConstIntTy).Value.Bits()
		}
		c := l.konst(value)
		cmp := l.compare(CmpEq, subject, c, unsigned)
		l.cur.Term = &BranchTerm{Cond: cmp, Then: body, Else: fail}
	case pat.Kind == PatternRange:
		lo := pat.Value.Ty().(*ConstIntTy).Value
		hi := pat.High.Ty().(*ConstIntTy).Value
		if !pat.Inclusive {
			hi = hi.Sub(One)
		}
		upper := l.f.NewBlock()
		loC := l.konst(lo.Bits())
		geCmp := l.compare(CmpGe, subject, loC, unsigned)
		l.cur.Term = &BranchTerm{Cond: geCmp, Then: upper, Else: fail}
		l.cur = upper
		hiC := l.konst(hi.Bits())
		leCmp := l.compare(CmpLe, subject, hiC, unsigned)
		l.cur.Term = &BranchTerm{Cond: leCmp, Then: body, Else: fail}
	default:
		panic(Internal("lower: unhandled pattern kind"))
	}
	l.cur = fail
}
