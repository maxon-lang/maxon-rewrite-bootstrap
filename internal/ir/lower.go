package ir

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"sort"
	"strings"
)

// WordBytes is the size of every value the IR holds, and of each field of a box: a scalar, or the address of a box, a
// string or an array.
const WordBytes = 8

// TagOffset is where a union box holds its case index; the payload follows it.
const TagOffset = 0

type jumpTargets struct {
	brk  *IrBlock
	cont *IrBlock
	// depth is the number of scopes open where the loop or match starts: a jump to it leaves the ones above.
	depth int
}

// ownerSlot is the slot that owns a sink parameter: the caller gives the value away, so the callee holds it in a slot
// of its own, which moving from it empties and the end of the function drops.
type ownerSlot struct {
	local *ast.Local
	slot  int
}

type Lowerer struct {
	fn *ast.FuncDecl
	f  *IrFunction
	// binding gives the type parameters of the function being lowered their arguments, when it is an instance of a
	// generic function. The body was checked once against the parameters, so every type it names is read through it.
	binding ast.Binding
	jumps   map[ast.Node]jumpTargets
	cur     *IrBlock
	scopes  []scope
	// ownerSlots are the slots that hold the sink parameters the body names.
	ownerSlots []ownerSlot
	// selfOwner is the slot that owns a sink self, or -1.
	selfOwner int
}

var lowerBinOps = map[string]BinOp{
	"+": OpAdd, "-": OpSub, "*": OpMul, "/": OpDiv, "mod": OpMod,
	"and": OpAnd, "or": OpOr, "xor": OpXor, "shl": OpShl, "shr": OpShr,
}

var lowerCmpOps = map[string]CmpOp{
	"==": CmpEq, "!=": CmpNe, "<": CmpLt, "<=": CmpLe, ">": CmpGt, ">=": CmpGe,
}

// newLowerer starts the IR of a function. binding gives the type parameters their arguments; it is the function's own
// when it is an instance of a generic function, and the one of the function around it for the body of a behaviour.
func newLowerer(fn *ast.FuncDecl, binding ast.Binding, name string) *Lowerer {
	paramCount := len(fn.Params)
	if fn.IsMethod() {
		paramCount++
	}
	l := &Lowerer{fn: fn, f: &IrFunction{Decl: fn, Name: name, ParamCount: paramCount}, jumps: map[ast.Node]jumpTargets{}, selfOwner: -1, binding: binding}
	var locals []*ast.Local
	for _, loc := range fn.Locals {
		if !loc.IsParam {
			locals = append(locals, loc)
		}
	}
	sort.SliceStable(locals, func(i, j int) bool { return locals[i].Slot < locals[j].Slot })
	for _, loc := range locals {
		if loc.Slot != len(l.f.Slots) {
			panic(diag.Internal("lower: local slots of %s are not dense", fn.Symbol()))
		}
		l.f.NewSlot(l.kind(loc.Type))
	}
	l.cur = l.f.NewBlock()
	return l
}

func Lower(fn *ast.FuncDecl) *IrFunction {
	var binding ast.Binding
	if fn.Generic != nil {
		binding = ast.Binding{Params: fn.Generic.Generics, Args: fn.TypeArgs}
	}
	return lowerBody(newLowerer(fn, binding, ""))
}

// lowerBody lowers the statements of the function a lowerer was made for.
func lowerBody(l *Lowerer) *IrFunction {
	fn := l.fn
	l.pushScope(true)
	l.adoptParams()
	l.block(fn.Body)
	l.popScope()
	if l.cur.Term == nil {
		zero := l.konst(0)
		l.cur.Term = &RetTerm{Value: zero}
	}
	l.f.RemoveUnreachable()
	return l.f
}

// adoptParams moves each sink parameter into a slot, which owns it from there on. A parameter the body does not name
// (`_`) is dropped all the same, because the caller gave it away.
func (l *Lowerer) adoptParams() {
	if l.fn.IsMethod() && l.fn.SelfLocal.Conv == ast.ConvSink && l.kind(l.fn.SelfLocal.Type) == KindManaged {
		l.selfOwner = l.adopt(l.fn.SelfLocal.ParamIndex, l.fn.SelfLocal.Type)
	}
	first := 0
	if l.fn.IsMethod() {
		first = 1
	}
	for i, prm := range l.fn.Params {
		if ast.ConventionOf(prm.Convention) != ast.ConvSink || l.kind(l.fn.ParamTypes[i]) != KindManaged {
			continue
		}
		slot := l.adopt(first+i, l.fn.ParamTypes[i])
		if prm.Sym != nil {
			l.ownerSlots = append(l.ownerSlots, ownerSlot{local: prm.Sym, slot: slot})
		}
	}
}

func (l *Lowerer) adopt(index int, t ast.Ty) int {
	slot := l.f.NewSlot(KindManaged)
	l.emit(&StoreSlot{Slot: slot, Src: l.param(index, KindManaged)})
	l.ownLocal(slot, t)
	return slot
}

// ownerOf is the slot that owns a sink parameter, or -1 for a local that no slot owns.
func (l *Lowerer) ownerOf(loc *ast.Local) int {
	if loc.IsSelf {
		return l.selfOwner
	}
	for _, o := range l.ownerSlots {
		if o.local == loc {
			return o.slot
		}
	}
	return -1
}

// ownedRead is the value of a slot that owns what it holds: a move out of it when the ownership pass found the
// expression consuming it, and a borrow otherwise.
func (l *Lowerer) ownedRead(slot int, e ast.Expr) V {
	if e.IsConsumed() && l.f.Slots[slot] == KindManaged {
		d := l.reg(KindManaged)
		l.emit(&MoveSlotInstr{Dst: d, Slot: slot})
		return d
	}
	return l.load(slot)
}

func kindOf(t ast.Ty) IrKind {
	if t.IsManaged() {
		return KindManaged
	}
	return KindScalar
}

// kind is the kind of a value of a type the body names, which may be a type parameter of the function.
func (l *Lowerer) kind(t ast.Ty) IrKind { return kindOf(l.binding.Apply(t)) }

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

// self is the receiver, borrowed: a sink self lives in the slot that owns it.
func (l *Lowerer) self() V {
	if l.selfOwner >= 0 {
		return l.load(l.selfOwner)
	}
	return l.param(l.fn.SelfLocal.ParamIndex, KindManaged)
}

func (l *Lowerer) runtime(symbol string, kind IrKind, args ...V) V {
	d := l.reg(kind)
	l.emit(&CallRuntimeInstr{Dst: &d, Symbol: symbol, Args: args})
	return d
}

// block lowers the statements of a block, which owns the locals they declare.
func (l *Lowerer) block(body []ast.Stmt) {
	l.pushScope(true)
	for _, s := range body {
		l.stmt(s)
	}
	l.popScope()
}

// stmt lowers a statement, which owns the temporaries it makes: they are dropped when it ends.
func (l *Lowerer) stmt(s ast.Stmt) {
	l.pushScope(false)
	ast.VisitStmt(l, s)
	l.popScope()
}

// condition lowers a condition, dropping its temporaries before the branch: the value it gives is a scalar.
func (l *Lowerer) condition(e ast.Expr) V {
	var v V
	l.scoped(func() { v = l.expr(e) })
	return v
}

func (l *Lowerer) VisitLetStmt(s *ast.LetStmt) {
	if s.Init != nil {
		v := l.expr(s.Init)
		l.emit(&StoreSlot{Slot: s.Sym.Slot, Src: v})
	}
	l.ownLocal(s.Sym.Slot, s.Sym.Type)
}

func (l *Lowerer) VisitDiscardStmt(s *ast.DiscardStmt) { l.expr(s.Value) }

func (l *Lowerer) VisitExprStmt(s *ast.ExprStmt) { l.expr(s.E) }

func (l *Lowerer) VisitIfStmt(s *ast.IfStmt) {
	then := l.f.NewBlock()
	end := l.f.NewBlock()
	els := end
	if s.HasElse {
		els = l.f.NewBlock()
	}
	cond := l.condition(s.Cond)
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
}

func (l *Lowerer) VisitWhileStmt(s *ast.WhileStmt) {
	top := l.f.NewBlock()
	body := l.f.NewBlock()
	end := l.f.NewBlock()
	l.jumps[s] = jumpTargets{brk: end, cont: top, depth: len(l.scopes)}
	l.goTo(top)
	cond := l.condition(s.Cond)
	l.cur.Term = &BranchTerm{Cond: cond, Then: body, Else: end}
	l.cur = body
	l.block(s.Body)
	l.goTo(top)
	l.cur = end
}

func (l *Lowerer) VisitMatchStmt(s *ast.MatchStmt) { l.match(s.Match, nil) }

func (l *Lowerer) VisitReturnStmt(s *ast.ReturnStmt) {
	var v V
	if s.Value == nil {
		v = l.konst(0)
	} else {
		v = l.expr(s.Value)
	}
	l.unwind(0)
	l.seal(&RetTerm{Value: v})
}

func (l *Lowerer) VisitBreakStmt(s *ast.BreakStmt) {
	target := l.jumps[s.Target]
	l.unwind(target.depth)
	l.seal(&JumpTerm{Target: target.brk})
}

func (l *Lowerer) VisitContinueStmt(s *ast.ContinueStmt) {
	target := l.jumps[s.Target]
	l.unwind(target.depth)
	l.seal(&JumpTerm{Target: target.cont})
}

// VisitAssignStmt stores a value into a place, dropping the value the place held: the assignment is where that
// owner's value dies. The value is computed first, so it may read the old one.
func (l *Lowerer) VisitAssignStmt(a *ast.AssignStmt) {
	switch t := a.Target.(type) {
	case *ast.NameExpr:
		switch t.Kind {
		case ast.NameLocal:
			v := l.expr(a.Value)
			loc := t.Local
			if loc.ByRef() {
				addr := l.param(loc.ParamIndex, KindScalar)
				l.dropAt(addr, loc.Type)
				l.emit(&StoreAt{Addr: addr, Src: v})
			} else {
				if l.kind(loc.Type) == KindManaged {
					l.emit(&DropSlotInstr{Slot: loc.Slot, Ty: l.dropType(loc.Type)})
				}
				l.emit(&StoreSlot{Slot: loc.Slot, Src: v})
			}
			return
		case ast.NameSelfField:
			v := l.expr(a.Value)
			obj := l.self()
			l.storeField(obj, t.Field.Index, t.Ty(), v)
			return
		}
	case *ast.MemberExpr:
		if t.Kind == ast.MemberField {
			obj := l.expr(t.Obj)
			v := l.expr(a.Value)
			l.storeField(obj, t.Field.Index, t.Ty(), v)

			return
		}

	case *ast.IndexExpr:
		l.assignElement(t, a.Value)
		return
	}
	panic(diag.Internal("lower: unhandled assignment target"))
}

func (l *Lowerer) VisitForStmt(s *ast.ForStmt) {
	slot := s.Sym.Slot
	limit := l.f.NewSlot(KindScalar)
	unsigned := isUnsignedInt(s.Sym.Type)
	body := l.f.NewBlock()
	cont := l.f.NewBlock()
	step := l.f.NewBlock()
	exit := l.f.NewBlock()
	l.jumps[s] = jumpTargets{brk: exit, cont: cont, depth: len(l.scopes)}

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

func isUnsignedInt(t ast.Ty) bool {
	it, ok := t.(*ast.IntTy)
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

// expr lowers an expression to the register that holds its value, and settles who owns that value. One that the
// ownership pass marked consumed passes to its new owner, and a place that is consumed is moved out of (a whole
// local; the pass refuses every other). One that is not consumed only lends its value: a fresh value there is a
// temporary, dropped when the statement or branch ends.
func (l *Lowerer) expr(e ast.Expr) V {
	if c, ok := e.Ty().(*ast.ConstIntTy); ok {
		return l.konst(c.Value.Bits())
	}
	v := ast.VisitExprResult[V](l, e)
	if l.f.Regs[v] != KindManaged {
		return v
	}
	switch {
	case !ast.IsStorage(e):
		if !e.IsConsumed() && !isImmortal(e) {
			l.ownTemp(v, e.Ty())
		}
	case e.IsConsumed() && !isWholeLocal(e):
		panic(diag.Internal("lower: a field or an element is moved out at %d:%d; the ownership pass should have refused it", e.P().Line, e.P().Col))
	}
	return v
}

func isWholeLocal(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.NameExpr:
		return e.Kind == ast.NameLocal
	case *ast.SelfExpr:
		return true
	}
	return false
}

// isImmortal is whether a value is a string that lives in the executable, which is never dropped: a literal, or the
// name of an enum or union case.
func isImmortal(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.StrLit:
		return e.IsPlain()
	case *ast.MemberExpr:
		return e.Kind == ast.MemberEnumName || e.Kind == ast.MemberUnionName
	case *ast.NameExpr:
		return e.Kind == ast.NameConst && isImmortal(e.Const.Value)
	}
	return false
}

func (l *Lowerer) VisitIntLit(e *ast.IntLit) V { return l.konst(e.Value().Bits()) }

func (l *Lowerer) VisitBoolLit(e *ast.BoolLit) V {
	if e.Value {
		return l.konst(1)
	}
	return l.konst(0)
}

func (l *Lowerer) VisitSelfExpr(e *ast.SelfExpr) V {
	if l.selfOwner >= 0 {
		return l.ownedRead(l.selfOwner, e)
	}
	return l.self()
}

func (l *Lowerer) VisitCondExpr(e *ast.CondExpr) V {
	cond := l.expr(e.Cond)
	return l.choose(e.Ty(), cond,
		func() V { return l.converted(e.Then, e.Ty()) },
		func() V { return l.converted(e.Else, e.Ty()) })
}

// choose lowers a value that one of two branches computes. A branch's value is stored into a slot of its own, which
// owns it until the join gives it to the expression's consumer, and each branch drops its own temporaries.

func (l *Lowerer) VisitStructLitExpr(e *ast.StructLitExpr) V {
	values := make([]ast.Expr, len(e.Ordered))
	for i, v := range e.Ordered {
		if v != nil {
			values[i] = v
		} else {
			values[i] = e.Record.FieldList()[i].Default
		}
	}
	return l.record(values, nil)
}

func (l *Lowerer) VisitCastExpr(e *ast.CastExpr) V {
	v := l.expr(e.E)
	l.fit(v, e.Ty().(*ast.IntTy), e.E.Ty())
	return v
}

func (l *Lowerer) VisitMatchExpr(e *ast.MatchExpr) V { return l.match(e.Match, e.Ty()) }

func (l *Lowerer) VisitPanicExpr(e *ast.PanicExpr) V {
	msg := l.expr(e.Message)
	l.emit(&CallRuntimeInstr{Symbol: RtPanic, Args: []V{msg}})
	l.seal(&TrapTerm{})
	return l.konst(0)
}

func (l *Lowerer) choose(typ ast.Ty, cond V, then, els func() V) V {
	result := l.f.NewSlot(l.kind(typ))
	a := l.f.NewBlock()
	b := l.f.NewBlock()
	end := l.f.NewBlock()
	l.cur.Term = &BranchTerm{Cond: cond, Then: a, Else: b}
	l.cur = a
	l.scoped(func() { l.emit(&StoreSlot{Slot: result, Src: then()}) })
	l.goTo(end)
	l.cur = b
	l.scoped(func() { l.emit(&StoreSlot{Slot: result, Src: els()}) })
	l.goTo(end)
	return l.fetch(result)
}

func (l *Lowerer) converted(e ast.Expr, target ast.Ty) V {
	v := l.expr(e)
	if ti, ok := target.(*ast.IntTy); ok {
		if _, isInt := e.Ty().(*ast.IntTy); isInt {
			l.fit(v, ti, e.Ty())
		}
	}
	// A frozen value given to a let parameter of the type it holds lends its contents.
	have, want := l.binding.Apply(e.Ty()), l.binding.Apply(target)
	for ast.IsFrozen(have) && have != want {
		v, have = l.contentOnce(e, v, have)
	}
	return v
}

func (l *Lowerer) VisitNameExpr(n *ast.NameExpr) V {
	switch n.Kind {
	case ast.NameLocal:
		loc := n.Local
		if !loc.IsParam {
			return l.ownedRead(loc.Slot, n)
		}
		if !loc.ByRef() {
			if owner := l.ownerOf(loc); owner >= 0 {
				return l.ownedRead(owner, n)
			}
			return l.param(loc.ParamIndex, l.kind(loc.Type))
		}

		d := l.reg(l.kind(loc.Type))
		addr := l.param(loc.ParamIndex, KindScalar)
		l.emit(&LoadAt{Dst: d, Addr: addr})
		return d
	case ast.NameSelfField:
		d := l.reg(l.kind(n.Ty()))
		obj := l.self()
		l.emit(&LoadField{Dst: d, Obj: obj, Offset: WordBytes * n.Field.Index})
		return d
	case ast.NameConst:
		return l.expr(n.Const.Value)
	}
	panic(diag.Internal("lower: unresolved name '%s'", n.Name))
}

func (l *Lowerer) address(e ast.Expr) V {
	switch e := e.(type) {
	case *ast.NameExpr:
		switch e.Kind {
		case ast.NameLocal:
			loc := e.Local
			if loc.ByRef() {
				return l.param(loc.ParamIndex, KindScalar)
			}
			d := l.reg(KindScalar)
			l.emit(&SlotAddr{Dst: d, Slot: loc.Slot})
			return d
		case ast.NameSelfField:
			d := l.reg(KindScalar)
			obj := l.self()
			l.emit(&FieldAddr{Dst: d, Obj: obj, Offset: WordBytes * e.Field.Index})
			return d
		}
	case *ast.MemberExpr:
		if e.Kind == ast.MemberField {
			obj := l.read(e.Obj)
			d := l.reg(KindScalar)
			l.emit(&FieldAddr{Dst: d, Obj: obj, Offset: WordBytes * e.Field.Index})
			return d
		}
	case *ast.IndexExpr:
		return l.elemAddr(e)
	}
	panic(diag.Internal("lower: no address for this expression"))
}

func (l *Lowerer) VisitCallExpr(c *ast.CallExpr) V {
	switch c.Kind {
	case ast.CallPrint, ast.CallPrintError:
		text := l.expr(c.Ordered[0])
		handle := l.reg(KindScalar)
		symbol := RtStderr
		if c.Kind == ast.CallPrint {
			symbol = RtStdout
		}
		l.emit(&LoadGlobal{Dst: handle, Symbol: symbol})
		l.emit(&CallRuntimeInstr{Symbol: RtWrite, Args: []V{handle, text}})
		return l.konst(0)
	case ast.CallFunction, ast.CallStatic, ast.CallMethod, ast.CallBound:
		if c.Kind != ast.CallBound && c.Target.Intrinsic != ast.NotIntrinsic {
			return l.intrinsic(c)
		}
		target := l.callee(c)
		var args []V
		if c.Receiver != nil {
			args = append(args, l.read(c.Receiver))
		}
		for i := range c.Ordered {
			if c.Pass.Specs[i].Conv.ByAddress() {
				args = append(args, l.address(c.Ordered[i]))
			} else {
				args = append(args, l.converted(c.Ordered[i], c.Pass.Specs[i].Type))
			}
		}
		if c.Ty() == ast.Ty(ast.VoidTy) {
			l.emit(&CallInstr{Target: target, Args: args})
			return l.konst(0)
		}
		d := l.reg(l.kind(c.Ty()))
		l.emit(&CallInstr{Dst: &d, Target: target, Args: args})
		return d
	case ast.CallCopy:
		return l.copyValue(c, l.expr(c.Receiver), c.Receiver.Ty())
	case ast.CallUnionCtor:
		tag := c.CaseIndex
		return l.record(c.Ordered, &tag)
	}
	panic(diag.Internal("lower: unresolved call"))
}

// callee is the function a call runs. Every instance a body reaches was collected before lowering, so a missing one
// is a compiler bug, and making it here would race with the other modules lowering.
func (l *Lowerer) callee(c *ast.CallExpr) *ast.FuncDecl {
	target, created := c.Resolved(l.binding)
	if created {
		panic(diag.Internal("lower: %s was not collected before lowering", target.Symbol()))
	}
	return target
}

// BoxBytes is the size of a record or union box: a word per field (one for a box with none), and a first word for a
// union's case index. The backend sizes the boxes it copies with it, so a copy has the layout lowering gives a value.
func BoxBytes(fields int, tagged bool) int {
	size := WordBytes * max(fields, 1)
	if tagged {
		size += WordBytes
	}
	return size
}

// FieldOffset is where the field with this index sits in a box: after the case index when the box is a union's.
func FieldOffset(index int, tagged bool) int {
	if tagged {
		return WordBytes + WordBytes*index
	}
	return WordBytes * index
}

func (l *Lowerer) record(values []ast.Expr, tag *int) V {
	baseOffset := 0
	if tag != nil {
		baseOffset = WordBytes
	}
	p := l.reg(KindManaged)
	l.emit(&AllocInstr{Dst: p, Bytes: BoxBytes(len(values), tag != nil)})
	if tag != nil {
		t := l.konst(int64(*tag))
		l.emit(&StoreField{Obj: p, Offset: TagOffset, Src: t})
	}
	for i := range values {
		v := l.expr(values[i])
		l.emit(&StoreField{Obj: p, Offset: baseOffset + WordBytes*i, Src: v})
	}
	return p
}

// copyValue is a new value equal to v, which every part of it owns: the deep copy that CopyInstr says. A scalar is
// the value itself. site is the source that needs the copy, kept when the type holds something that has no native code.
func (l *Lowerer) copyValue(site ast.Node, v V, t ast.Ty) V {
	ty := l.binding.Apply(t)
	if kindOf(ty) != KindManaged {
		return v
	}
	l.requireNative(site, ty)
	d := l.reg(KindManaged)
	l.emit(&CopyInstr{Dst: d, Src: v, Ty: ty})
	return d
}

func (l *Lowerer) tag(union V) V {
	d := l.reg(KindScalar)
	l.emit(&LoadField{Dst: d, Obj: union, Offset: TagOffset})
	return d
}

func unionCaseNames(ut *ast.UnionTy) []string {
	cases := ut.CaseList()
	names := make([]string, len(cases))
	for i, c := range cases {
		names[i] = c.Name
	}
	return names
}

func (l *Lowerer) VisitMemberExpr(m *ast.MemberExpr) V {
	switch m.Kind {
	case ast.MemberField:
		obj := l.read(m.Obj)
		d := l.reg(l.kind(m.Ty()))
		l.emit(&LoadField{Dst: d, Obj: obj, Offset: WordBytes * m.Field.Index})
		return d
	case ast.MemberEnumCase:
		return l.konst(int64(m.CaseIndex))
	case ast.MemberUnionCase:
		tag := m.CaseIndex
		return l.record(nil, &tag)
	case ast.MemberEnumOrdinal:
		return l.expr(m.Obj)
	case ast.MemberEnumName:
		v := l.expr(m.Obj)
		return l.names(v, m.Obj.Ty().(*ast.EnumTy).Cases)
	case ast.MemberEnumRaw:
		et := m.Obj.Ty().(*ast.EnumTy)
		v := l.expr(m.Obj)
		if et.HasExplicitRaw {
			return l.selectValue(v, KindScalar, len(et.Raw), func(i int) V { return l.konst(et.Raw[i].Bits()) })
		}
		return v
	case ast.MemberUnionOrdinal:
		return l.tag(l.expr(m.Obj))
	case ast.MemberUnionName:
		t := l.tag(l.expr(m.Obj))
		return l.names(t, unionCaseNames(m.Obj.Ty().(*ast.UnionTy)))
	}
	panic(diag.Internal("lower: unresolved member"))
}

func (l *Lowerer) names(index V, names []string) V {
	return l.selectValue(index, KindManaged, len(names), func(i int) V { return l.str(names[i]) })
}

func (l *Lowerer) selectValue(index V, kind IrKind, count int, value func(int) V) V {
	result := l.f.NewSlot(kind)
	end := l.f.NewBlock()
	targets := make([]*IrBlock, count)
	for i := range count {
		targets[i] = l.f.NewBlock()
	}
	values := make([]int64, count-1)
	for i := range values {
		values[i] = int64(i)
	}
	l.cur.Term = &SwitchTerm{Value: index, Values: values, Targets: targets[:count-1], Default: targets[count-1]}
	for i := range count {
		l.cur = targets[i]
		v := value(i)
		l.emit(&StoreSlot{Slot: result, Src: v})
		l.goTo(end)
	}
	return l.fetch(result)
}

// textOwner says who owns the string a part of an interpolated literal gives.
type textOwner int

const (
	// textBorrowed is a string some other owner holds, which the literal only reads.
	textBorrowed textOwner = iota
	// textImmortal is a string in the executable.
	textImmortal
	// textFresh is a string the literal made, which it owns until it joins it into the result.
	textFresh
)

// VisitStrLit joins the parts of an interpolated literal into a new string, dropping each intermediate string once it
// has been joined. A literal of a single borrowed string is a copy of it, because the literal's value is its own.
func (l *Lowerer) VisitStrLit(s *ast.StrLit) V {
	if s.IsPlain() {
		var text strings.Builder
		for _, p := range s.Parts {
			text.WriteString(p.(string))
		}
		return l.str(text.String())
	}
	acc, owner := l.part(s.Parts[0])
	if len(s.Parts) == 1 && owner == textBorrowed {
		return l.copyValue(s, acc, ast.StringTy)
	}
	for i := 1; i < len(s.Parts); i++ {
		next, nextOwner := l.part(s.Parts[i])
		joined := l.runtime(RtConcat, KindManaged, acc, next)
		if owner == textFresh {
			l.dropValue(acc, ast.StringTy)
		}
		if nextOwner == textFresh {
			l.dropValue(next, ast.StringTy)
		}
		acc, owner = joined, textFresh
	}
	return acc
}

func (l *Lowerer) part(part any) (V, textOwner) {
	if text, ok := part.(string); ok {
		return l.str(text), textImmortal
	}
	e := part.(ast.Expr)
	v := l.read(e)
	switch t := ast.Content(l.binding.Apply(e.Ty())).(type) {
	case *ast.PrimTy:
		if t == ast.StringTy {
			if isImmortal(e) {
				return v, textImmortal
			}
			return v, textBorrowed
		}
		if t == ast.BoolTy {
			return l.names(v, []string{"false", "true"}), textImmortal
		}
	case *ast.ConstIntTy:
		flag := l.konst(0)
		return l.runtime(RtItoa, KindManaged, v, flag), textFresh
	case *ast.IntTy:
		flag := int64(0)
		if t.Unsigned() {
			flag = 1
		}
		flagReg := l.konst(flag)
		return l.runtime(RtItoa, KindManaged, v, flagReg), textFresh
	case *ast.EnumTy:
		return l.names(v, t.Cases), textImmortal
	case *ast.UnionTy:
		return l.names(l.tag(v), unionCaseNames(t)), textImmortal
	}
	panic(diag.Internal("lower: cannot interpolate %s", e.Ty().Name()))
}

func (l *Lowerer) VisitBinaryExpr(b *ast.BinaryExpr) V {
	if (b.Op == "and" || b.Op == "or") && b.Ty() == ast.Ty(ast.BoolTy) {
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
		l.scoped(func() { l.emit(&StoreSlot{Slot: result, Src: l.expr(b.R)}) })
		l.goTo(end)
		return l.load(result)
	}

	if b.OperandType == ast.Ty(ast.StringTy) {
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
		panic(diag.Internal("lower: unknown operator '%s'", b.Op))
	}
	l.emit(&BinInstr{Dst: d, Op: op, A: a, B: c, Unsigned: unsigned, Checked: op == OpAdd || op == OpSub || op == OpMul})
	if t, ok := b.Ty().(*ast.IntTy); ok {
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

func (l *Lowerer) VisitUnaryExpr(u *ast.UnaryExpr) V {
	v := l.expr(u.E)
	if u.Op == "not" && u.Ty() == ast.Ty(ast.BoolTy) {
		return l.flip(v)
	}
	d := l.reg(KindScalar)
	if u.Op == "not" {
		l.emit(&NotInstr{Dst: d, Src: v})
	} else {
		l.emit(&NegInstr{Dst: d, Src: v})
	}
	l.rangeCheck(d, u.Ty().(*ast.IntTy))
	return d
}

func (l *Lowerer) rangeCheck(v V, t *ast.IntTy) {
	if t.Unsigned() {
		l.fit(v, t, &ast.IntTy{Min: ast.Zero, Max: ast.U64Max})
	} else {
		l.fit(v, t, ast.Anonymous64)
	}
}

func (l *Lowerer) fit(v V, target *ast.IntTy, source ast.Ty) {
	if _, ok := source.(*ast.ConstIntTy); ok {
		return
	}
	src := source.(*ast.IntTy)
	if target.Covers(src) && target.Unsigned() == src.Unsigned() {
		return
	}

	if target.Unsigned() {
		if !src.Unsigned() && src.Min.IsNeg() {
			l.emit(&GuardInstr{Src: v, Kind: GuardNonNegative, Bound: 0})
		}
		if target.Min.Gt(ast.MaxI(src.Min, ast.Zero)) {
			l.emit(&GuardInstr{Src: v, Kind: GuardUnsignedAtLeast, Bound: target.Min.Bits()})
		}
		if target.Max.Lt(src.Max) {
			l.emit(&GuardInstr{Src: v, Kind: GuardUnsignedAtMost, Bound: target.Max.Bits()})
		}
		return
	}

	srcMax := src.Max
	if src.Unsigned() && src.Max.Gt(ast.I64Max) {
		l.emit(&GuardInstr{Src: v, Kind: GuardNonNegative, Bound: 0})
		srcMax = ast.I64Max
	}
	if target.Min.Gt(src.Min) {
		l.emit(&GuardInstr{Src: v, Kind: GuardSignedAtLeast, Bound: target.Min.Bits()})
	}
	if target.Max.Lt(srcMax) {
		l.emit(&GuardInstr{Src: v, Kind: GuardSignedAtMost, Bound: target.Max.Bits()})
	}
}

func (l *Lowerer) match(m *ast.MatchCore, resultType ast.Ty) V {
	st := ast.Content(l.binding.Apply(m.Subject.Ty()))
	subject := l.read(m.Subject)
	end := l.f.NewBlock()
	l.jumps[m] = jumpTargets{brk: end, depth: len(l.scopes)}
	result := -1
	if resultType != nil {
		result = l.f.NewSlot(l.kind(resultType))
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
					kind := l.kind(local.Type)
					d := l.reg(kind)
					l.emit(&LoadField{Dst: d, Obj: subject, Offset: WordBytes + WordBytes*i})
					l.emit(&StoreSlot{Slot: local.Slot, Src: d, Borrow: kind == KindManaged})
				}
			}
		}

		switch arm.Kind() {
		case ast.ArmThen:
			l.stmt(arm.Action)
		case ast.ArmGives:
			l.scoped(func() { l.emit(&StoreSlot{Slot: result, Src: l.converted(arm.Value, resultType)}) })
		case ast.ArmPanic:
			l.scoped(func() { l.expr(arm.Value) })
		default:
			panic(diag.Internal("lower: unhandled match arm kind"))
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
	return l.fetch(result)
}

func (l *Lowerer) test(pat *ast.Pattern, st ast.Ty, subject V, body *IrBlock) {
	unsigned := isUnsignedInt(st)
	fail := l.f.NewBlock()
	switch {
	case pat.Kind() == ast.PatternCase:
		scrutinee := subject
		if _, ok := st.(*ast.UnionTy); ok {
			scrutinee = l.tag(subject)
		}
		idx := l.konst(int64(pat.CaseIndex))
		cmp := l.compare(CmpEq, scrutinee, idx, false)
		l.cur.Term = &BranchTerm{Cond: cmp, Then: body, Else: fail}
	case pat.Kind() == ast.PatternLiteral && st == ast.Ty(ast.StringTy):
		testBlock := l.cur
		lit := l.expr(pat.Value)
		eq := l.runtime(RtStreq, KindScalar, subject, lit)
		testBlock.Term = &BranchTerm{Cond: eq, Then: body, Else: fail}
	case pat.Kind() == ast.PatternLiteral:
		var value int64
		if bl, ok := pat.Value.(*ast.BoolLit); ok {
			if bl.Value {
				value = 1
			}
		} else {
			value = pat.Value.Ty().(*ast.ConstIntTy).Value.Bits()
		}
		c := l.konst(value)
		cmp := l.compare(CmpEq, subject, c, unsigned)
		l.cur.Term = &BranchTerm{Cond: cmp, Then: body, Else: fail}
	case pat.Kind() == ast.PatternRange:
		lo := pat.Value.Ty().(*ast.ConstIntTy).Value
		hi := pat.High.Ty().(*ast.ConstIntTy).Value
		if !pat.Inclusive {
			hi = hi.Sub(ast.One)
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
		panic(diag.Internal("lower: unhandled pattern kind"))
	}
	l.cur = fail
}
