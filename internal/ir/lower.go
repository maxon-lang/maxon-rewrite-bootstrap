package ir

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"slices"
	"strings"
)

// WordBytes is the size of every value the IR holds, and of each field of a box: a scalar, or the address of a box, a
// string or an array.
const WordBytes = 8

// TagOffset is where a union box holds its case index; the payload follows it.
const TagOffset = 0

// jumpTargets are where a break and a continue of a loop or a match go.
type jumpTargets struct {
	brk  BlockID
	cont BlockID
	// depth is the number of scopes open where the loop or match starts: a jump to it leaves the ones above.
	depth int
}

// jumpFrame is the targets of a loop or a match that is being lowered. They nest, so a break finds its own from the top.
type jumpFrame struct {
	node    ast.NodeID
	targets jumpTargets
}

// ownerSlot is the slot that owns a consumes parameter: the caller gives the value away, so the callee holds it in a slot
// of its own, which moving from it empties and the end of the function drops.
type ownerSlot struct {
	local *ast.Local
	slot  int
}

type Lowerer struct {
	// typings are what the check pass found out about every module's bodies. A body that lowering reaches may be another
	// module's (an instance of its generic, a constant, a field's default), so a node is looked up in the typing of the
	// module it is in.
	typings ast.Typings
	fn      *ast.FuncDecl
	b       *Builder
	// f is the function under construction, which is the builder's.
	f     *IrFunction
	arena *Arena
	// instances are the ones the module needs, which every generic call in the body must be one of.
	instances *InstanceSet
	// binding gives the type parameters of the function being lowered their arguments, when it is an instance of a
	// generic function. The body was checked once against the parameters, so every type it names is read through it.
	binding ast.Binding
	jumps   []jumpFrame
	cur     BlockID
	scopes  []scope
	// ownerSlots are the slots that hold the consumes parameters the body names.
	ownerSlots []ownerSlot
	// selfOwner is the slot that owns a consumes self, or -1.
	selfOwner int
	// args holds the values of the arguments of the calls being lowered, the innermost call's last, so a call needs no list
	// of its own.
	args []V
	// handlers are the trys around the code being lowered, innermost last.
	handlers []tryHandler
	// pending are the values computed for something that takes them, which has not run yet: a failure before it runs
	// drops them, since nothing else would.
	pending []pendingDrop
}

var lowerBinOps = map[string]BinOp{
	"+": BinAdd, "-": BinSub, "*": BinMul, "/": BinDiv, "mod": BinMod,
	"and": BinAnd, "or": BinOr, "xor": BinXor, "shl": BinShl, "shr": BinShr,
}

var lowerCmpOps = map[string]CmpOp{
	"==": CmpEq, "!=": CmpNe, "<": CmpLt, "<=": CmpLe, ">": CmpGt, ">=": CmpGe,
}

// Lowering lowers functions into one arena with the buffers of one lowerer, which is what a worker does for each module it
// is given: Reset before a module forgets the IR of the one before it, and nothing is allocated once the buffers have grown
// to the largest module. It belongs to one goroutine.
type Lowering struct {
	arena *Arena
	l     Lowerer
	b     Builder
}

// NewLowering is a lowerer with an arena of its own, for a project whose bodies the typings describe.
func NewLowering(typings ast.Typings) *Lowering {
	w := &Lowering{arena: &Arena{}}
	w.l.b, w.l.arena, w.l.typings = &w.b, w.arena, typings
	return w
}

// t is the typing of the module a node is in.
func (l *Lowerer) t(n ast.Pos) *ast.Typing { return l.typings.For(n) }

// ty is the type the checker gave an expression.
func (l *Lowerer) ty(e ast.Expr) ast.Ty { return l.typings.Type(e) }

// consumed is whether the ownership pass found the expression consuming its value.
func (l *Lowerer) consumed(e ast.Expr) bool { return l.typings.Consumed(e) }

// Reset makes the IR of every function lowered so far invalid, so the arena's storage serves the next ones.
func (w *Lowering) Reset() { w.arena.Reset() }

// Lower is the IR of a function, or of an instance of a generic one. instances are the ones the function's module needs.
func (w *Lowering) Lower(fn *ast.FuncDecl, instances *InstanceSet) *IrFunction {
	var binding ast.Binding
	if fn.Generic != nil {
		binding = ast.Binding{Params: fn.Generic.Generics, Args: fn.TypeArgs}
	}
	w.l.begin(fn, binding, instances, "")
	return w.l.lowerBody()
}

// Lower is the IR of one function, in an arena of its own.
func Lower(typings ast.Typings, fn *ast.FuncDecl, instances *InstanceSet) *IrFunction {
	return NewLowering(typings).Lower(fn, instances)
}

// newLowerer starts the IR of the body of a behaviour, which is lowered while the function around it is, so it has a
// builder of its own and its function goes in the same arena. binding is the one of the function around it.
func (l *Lowerer) newLowerer(fn *ast.FuncDecl, name string) *Lowerer {
	inner := &Lowerer{typings: l.typings, b: &Builder{}, arena: l.arena}
	inner.begin(fn, l.binding, l.instances, name)
	return inner
}

// begin starts the IR of a function, forgetting the one before it. binding gives the type parameters their arguments; it is
// the function's own when it is an instance of a generic function, and the one of the function around it for the body of a
// behaviour.
func (l *Lowerer) begin(fn *ast.FuncDecl, binding ast.Binding, instances *InstanceSet, name string) {
	paramCount := len(fn.Params)
	if fn.IsMethod() {
		paramCount++
	}
	l.b.Start(fn, name, paramCount)
	l.fn, l.f, l.instances, l.binding = fn, &l.b.F, instances, binding
	l.jumps, l.scopes, l.ownerSlots, l.args = l.jumps[:0], l.scopes[:0], l.ownerSlots[:0], l.args[:0]
	l.handlers, l.pending = l.handlers[:0], l.pending[:0]
	l.selfOwner = -1
	l.declareSlots()
	l.cur = l.b.NewBlock()
}

// declareSlots makes a slot for each local that is not a parameter, at the index the checker gave it.
func (l *Lowerer) declareSlots() {
	count := 0
	for _, loc := range l.fn.Locals {
		if !loc.IsParam {
			count++
		}
	}
	const unset = IrKind(0xFF)
	for range count {
		l.f.Slots = append(l.f.Slots, unset)
	}
	for _, loc := range l.fn.Locals {
		if loc.IsParam {
			continue
		}
		if loc.Slot < 0 || loc.Slot >= count || l.f.Slots[loc.Slot] != unset {
			panic(diag.Internal("lower: local slots of %s are not dense", l.fn.Symbol()))
		}
		l.f.Slots[loc.Slot] = l.kind(loc.Type)
	}
}

// lowerBody lowers the statements of the function that begin started, and finishes it in the arena.
func (l *Lowerer) lowerBody() *IrFunction {
	fn := l.fn
	l.pushScope(true)
	l.adoptParams()
	l.block(fn.Body)
	l.popScope()
	if !l.b.Terminated(l.cur) {
		zero := l.konst(0)
		l.b.Terminate(l.cur, Ret(zero))
	}
	return l.b.Finish(l.arena)
}

// adoptParams moves each consumes parameter into a slot, which owns it from there on. A parameter the body does not name
// (`_`) is dropped all the same, because the caller gave it away.
func (l *Lowerer) adoptParams() {
	if l.fn.IsMethod() && l.fn.SelfLocal.Conv.Owns() && l.kind(l.fn.SelfLocal.Type) == KindManaged {
		l.selfOwner = l.adopt(l.fn.SelfLocal.ParamIndex, l.fn.SelfLocal.Type)
	}
	first := 0
	if l.fn.IsMethod() {
		first = 1
	}
	for i, prm := range l.fn.Params {
		if ast.ConventionOf(prm.Convention) != ast.ConvConsumes || l.kind(l.fn.ParamTypes[i]) != KindManaged {
			continue
		}
		slot := l.adopt(first+i, l.fn.ParamTypes[i])
		if sym := l.fn.LocalAt(l.fn.ParamLocals[i]); sym != nil {
			l.ownerSlots = append(l.ownerSlots, ownerSlot{local: sym, slot: slot})
		}
	}
}

func (l *Lowerer) adopt(index int, t ast.Ty) int {
	slot := l.f.NewSlot(KindManaged)
	l.emit(StoreSlot(slot, l.param(index, KindManaged), false))
	l.ownLocal(slot, t)
	return slot
}

// ownerOf is the slot that owns a consumes parameter, or -1 for a local that no slot owns.
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
	if l.consumed(e) && l.f.Slots[slot] == KindManaged {
		d := l.reg(KindManaged)
		l.emit(MoveSlot(d, slot))
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

func (l *Lowerer) pushJumps(node ast.NodeID, targets jumpTargets) {
	l.jumps = append(l.jumps, jumpFrame{node: node, targets: targets})
}

func (l *Lowerer) popJumps() { l.jumps = l.jumps[:len(l.jumps)-1] }

// jumpsOf is where a break or a continue of the loop or match goes.
func (l *Lowerer) jumpsOf(node ast.NodeID) jumpTargets {
	for _, v := range slices.Backward(l.jumps) {
		if v.node == node {
			return v.targets
		}
	}
	panic(diag.Internal("lower: a break or a continue names a loop or a match that is not being lowered"))
}

func (l *Lowerer) emit(i Instr) { l.b.Emit(l.cur, i) }

// push adds the value of an argument to the call being lowered. The value is computed before the push, because computing
// it can push too, and the stack may move.
func (l *Lowerer) push(v V) { l.args = append(l.args, v) }

func (l *Lowerer) seal(t Term) {
	if !l.b.Terminated(l.cur) {
		l.b.Terminate(l.cur, t)
	}
	l.cur = l.b.NewBlock()
}

func (l *Lowerer) goTo(target BlockID) {
	if !l.b.Terminated(l.cur) {
		l.b.Terminate(l.cur, Jump(target))
	}
	l.cur = target
}

func (l *Lowerer) reg(kind IrKind) V {
	return l.f.NewReg(kind)
}

func (l *Lowerer) konst(value int64) V {
	d := l.reg(KindScalar)
	l.emit(Const(d, value))
	return d
}

func (l *Lowerer) str(value string) V {
	d := l.reg(KindManaged)
	l.emit(l.b.Str(d, value))
	return d
}

func (l *Lowerer) param(index int, kind IrKind) V {
	d := l.reg(kind)
	l.emit(LoadParam(d, index))
	return d
}

// self is the receiver, borrowed: a consumes self lives in the slot that owns it.
func (l *Lowerer) self() V {
	if l.selfOwner >= 0 {
		return l.load(l.selfOwner)
	}
	return l.param(l.fn.SelfLocal.ParamIndex, KindManaged)
}

func (l *Lowerer) runtime(symbol Rt, kind IrKind, args ...V) V {
	d := l.reg(kind)
	l.emit(l.b.CallRuntime(d, symbol, args, nil))
	return d
}

// block lowers the live statements of a block, which owns the locals they declare.
func (l *Lowerer) block(body ast.StmtList) {
	l.pushScope(true)
	for _, s := range ast.Live(body).All() {
		l.stmt(s)
	}
	l.popScope()
}

// stmt lowers a statement, which owns the temporaries it makes: they are dropped when it ends, and every value it computes
// for something to take has been taken by then.
func (l *Lowerer) stmt(s ast.Stmt) {
	mark := len(l.pending)
	l.pushScope(false)
	ast.VisitStmt(l, s)
	l.popScope()
	l.pending = l.pending[:mark]
}

// condition lowers a condition, dropping its temporaries before the branch: the value it gives is a scalar.
func (l *Lowerer) condition(e ast.Expr) V {
	var v V
	l.scoped(func() { v = l.expr(e) })
	return v
}

func (l *Lowerer) VisitLetStmt(s ast.LetStmt) {
	sym := l.fn.LocalAt(l.typings.Of(s.T.Module).LetLocal(s))
	if !s.Init().IsNil() {
		v := l.expr(s.Init())
		l.emit(StoreSlot(sym.Slot, v, false))
	}
	l.ownLocal(sym.Slot, sym.Type)
}

func (l *Lowerer) VisitDiscardStmt(s ast.DiscardStmt) { l.expr(s.Value()) }

func (l *Lowerer) VisitExprStmt(s ast.ExprStmt) { l.expr(s.E()) }

func (l *Lowerer) VisitIfStmt(s ast.IfStmt) {
	then := l.b.NewBlock()
	end := l.b.NewBlock()
	els := end
	if s.HasElse() {
		els = l.b.NewBlock()
	}
	cond := l.condition(s.Cond())
	l.b.Terminate(l.cur, Branch(cond, then, els))
	l.cur = then
	l.block(s.Then())
	l.goTo(end)
	if s.HasElse() {
		l.cur = els
		l.block(s.Else())
		l.goTo(end)
	}
	l.cur = end
}

func (l *Lowerer) VisitWhileStmt(s ast.WhileStmt) {
	top := l.b.NewBlock()
	body := l.b.NewBlock()
	end := l.b.NewBlock()
	l.pushJumps(s.ID, jumpTargets{brk: end, cont: top, depth: len(l.scopes)})
	l.goTo(top)
	cond := l.condition(s.Cond())
	l.b.Terminate(l.cur, Branch(cond, body, end))
	l.cur = body
	l.block(s.Body())
	l.goTo(top)
	l.popJumps()
	l.cur = end
}

func (l *Lowerer) VisitMatchStmt(s ast.MatchStmt) { l.match(s.Match(), nil) }

func (l *Lowerer) VisitReturnStmt(s ast.ReturnStmt) {
	var v V
	if s.Value().IsNil() {
		v = l.konst(0)
	} else {
		v = l.expr(s.Value())
	}
	l.unwind(0)
	l.seal(Ret(v))
}

func (l *Lowerer) VisitBreakStmt(s ast.BreakStmt) {
	target := l.jumpsOf(l.typings.Of(s.T.Module).BreakTarget(s))
	l.unwind(target.depth)
	l.seal(Jump(target.brk))
}

func (l *Lowerer) VisitContinueStmt(s ast.ContinueStmt) {
	target := l.jumpsOf(l.typings.Of(s.T.Module).ContinueTarget(s))
	l.unwind(target.depth)
	l.seal(Jump(target.cont))
}

// VisitAssignStmt stores a value into a place, dropping the value the place held: the assignment is where that
// owner's value dies. The value is computed first, so it may read the old one.
func (l *Lowerer) VisitAssignStmt(a ast.AssignStmt) {
	switch a.Target().Kind() {
	case ast.KindNameExpr:
		t := a.Target().ToNameExpr()

		name := l.typings.Of(t.T.Module).Name(t)
		switch name.Kind() {
		case ast.NameLocal:
			v := l.expr(a.Value())
			loc := name.Local(l.fn)
			if loc.ByRef() {
				addr := l.param(loc.ParamIndex, KindScalar)
				l.dropAt(addr, loc.Type)
				l.emit(StoreAt(addr, v))
			} else {
				if l.kind(loc.Type) == KindManaged {
					l.emit(l.b.DropSlot(loc.Slot, l.dropType(loc.Type)))
				}
				l.emit(StoreSlot(loc.Slot, v, false))
			}
			return
		case ast.NameSelfField:
			v := l.expr(a.Value())
			obj := l.self()
			l.storeField(obj, name.FieldIndex(), l.ty(t.Expr()), v)
			return
		}
	case ast.KindMemberExpr:
		t := a.Target().ToMemberExpr()

		if member := l.typings.Of(t.T.Module).Member(t); member.Kind == ast.MemberField {
			obj := l.expr(t.Obj())
			v := l.expr(a.Value())
			l.storeField(obj, member.Field.Index, l.ty(t.Expr()), v)

			return
		}

	case ast.KindIndexExpr:
		t := a.Target().ToIndexExpr()

		l.assignElement(t, a.Value())
		return
	}
	panic(diag.Internal("lower: unhandled assignment target"))
}

func (l *Lowerer) VisitForStmt(s ast.ForStmt) {
	sym := l.fn.LocalAt(l.typings.Of(s.T.Module).ForLocal(s))
	slot := sym.Slot
	limit := l.f.NewSlot(KindScalar)
	unsigned := isUnsignedInt(sym.Type)
	body := l.b.NewBlock()
	cont := l.b.NewBlock()
	step := l.b.NewBlock()
	exit := l.b.NewBlock()
	l.pushJumps(s.ID, jumpTargets{brk: exit, cont: cont, depth: len(l.scopes)})

	from := l.expr(s.From())
	l.emit(StoreSlot(slot, from, false))
	to := l.expr(s.To())
	l.emit(StoreSlot(limit, to, false))
	pastOp := CmpGe
	if s.Inclusive() {
		pastOp = CmpGt
	}
	pastA := l.load(slot)
	pastB := l.load(limit)
	past := l.compare(pastOp, pastA, pastB, unsigned)
	l.b.Terminate(l.cur, Branch(past, exit, body))

	l.cur = body
	l.block(s.Body())
	l.goTo(cont)

	if s.Inclusive() {
		a := l.load(slot)
		b := l.load(limit)
		done := l.compare(CmpEq, a, b, unsigned)
		l.b.Terminate(l.cur, Branch(done, exit, step))
		l.cur = step
	} else {
		l.goTo(step)
	}
	next := l.reg(KindScalar)
	cur := l.load(slot)
	one := l.konst(1)
	l.emit(Bin(next, BinAdd, cur, one, unsigned, NoBlock))
	l.emit(StoreSlot(slot, next, false))
	if s.Inclusive() {
		l.b.Terminate(l.cur, Jump(body))
	} else {
		lim := l.load(limit)
		over := l.compare(CmpGe, next, lim, unsigned)
		l.b.Terminate(l.cur, Branch(over, exit, body))
	}
	l.popJumps()
	l.cur = exit
}

func isUnsignedInt(t ast.Ty) bool {
	it, ok := t.(*ast.IntTy)
	return ok && it.Unsigned()
}

func (l *Lowerer) load(slot int) V {
	d := l.reg(l.f.Slots[slot])
	l.emit(LoadSlot(d, slot))
	return d
}

func (l *Lowerer) compare(op CmpOp, a, b V, unsigned bool) V {
	d := l.reg(KindScalar)
	l.emit(Cmp(d, op, a, b, unsigned))
	return d
}

// expr lowers an expression to the register that holds its value, and settles who owns that value. One that the
// ownership pass marked consumed passes to its new owner, and a place that is consumed is moved out of (a whole
// local or a field of an owned one; the pass refuses every other). One that is not consumed only lends its value: a
// fresh value there is a temporary, dropped when the statement or branch ends.
//
// A value that is consumed is in flight until what takes it runs, which is the expression around it or the statement: a
// failure before then drops it (see failure).
func (l *Lowerer) expr(e ast.Expr) V {
	typing := l.typings.Of(e.T.Module)
	if c, ok := typing.Type(e).(*ast.ConstIntTy); ok {
		return l.konst(c.Value.Bits())
	}
	if t, ok := e.AsTryExpr(); ok && t.Value().IsNil() {
		return l.through(t)
	}
	mark := len(l.pending)
	v := ast.VisitExprResult[V](l, e)
	l.pending = l.pending[:mark]
	if l.f.Regs[v] != KindManaged {
		return v
	}
	switch {
	case !typing.IsStorage(e):
		if !typing.Consumed(e) && !l.isImmortal(e) {
			l.ownTemp(v, typing.Type(e))
		}
	case typing.Consumed(e) && !l.isWholeLocal(e) && !l.isField(e):
		panic(diag.Internal("lower: an element or a part of a temporary is moved out at offset %d of module %d; the ownership pass should have refused it", e.Position().Off, e.Position().Mod))
	}
	if typing.Consumed(e) && !l.isImmortal(e) {
		l.pending = append(l.pending, pendingDrop{reg: v, ty: l.dropType(typing.Type(e))})
	}
	return v
}

func (l *Lowerer) isWholeLocal(e ast.Expr) bool {
	e = ast.Through(e)
	switch e.Kind() {
	case ast.KindNameExpr:
		e := e.ToNameExpr()

		return l.typings.Of(e.T.Module).Name(e).Kind() == ast.NameLocal
	case ast.KindSelfExpr:
		return true
	}
	return false
}

// isField is whether a place is a field of a record, which a move leaves empty.
func (l *Lowerer) isField(e ast.Expr) bool {
	e = ast.Through(e)
	switch e.Kind() {
	case ast.KindNameExpr:
		e := e.ToNameExpr()

		return l.typings.Of(e.T.Module).Name(e).Kind() == ast.NameSelfField
	case ast.KindMemberExpr:
		e := e.ToMemberExpr()

		return l.typings.Of(e.T.Module).Member(e).Kind == ast.MemberField
	}
	return false
}

// isImmortal is whether a value is a string that lives in the executable, which is never dropped: a literal, or the
// name of an enum or union case.
func (l *Lowerer) isImmortal(e ast.Expr) bool {
	e = ast.Through(e)
	switch e.Kind() {
	case ast.KindStrLit:
		e := e.ToStrLit()

		return e.IsPlain()
	case ast.KindMemberExpr:
		e := e.ToMemberExpr()

		kind := l.typings.Of(e.T.Module).Member(e).Kind
		return kind == ast.MemberEnumName || kind == ast.MemberUnionName
	case ast.KindNameExpr:
		e := e.ToNameExpr()

		name := l.typings.Of(e.T.Module).Name(e)
		return name.Kind() == ast.NameConst && l.isImmortal(l.typings.Of(e.T.Module).Const(name).Value)
	}
	return false
}

func (l *Lowerer) VisitIntLit(e ast.IntLit) V { return l.konst(e.Value().Bits()) }

func (l *Lowerer) VisitBoolLit(e ast.BoolLit) V {
	if e.Value() {
		return l.konst(1)
	}
	return l.konst(0)
}

func (l *Lowerer) VisitSelfExpr(e ast.SelfExpr) V {
	if l.selfOwner >= 0 {
		return l.ownedRead(l.selfOwner, e.Expr())
	}
	return l.self()
}

func (l *Lowerer) VisitCondExpr(e ast.CondExpr) V {
	cond := l.expr(e.Cond())
	return l.choose(l.ty(e.Expr()), cond,
		func() V { return l.converted(e.Then(), l.ty(e.Expr())) },
		func() V { return l.converted(e.Else(), l.ty(e.Expr())) })
}

// choose lowers a value that one of two branches computes. A branch's value is stored into a slot of its own, which
// owns it until the join gives it to the expression's consumer, and each branch drops its own temporaries.

func (l *Lowerer) VisitStructLitExpr(e ast.StructLitExpr) V {
	lit := l.typings.Of(e.T.Module).StructLit(e)
	return l.record(l.ty(e.Expr()), len(lit.Ordered), nil, func(i int) ast.Expr {
		if v := lit.Ordered[i]; !v.IsNil() {
			return v
		}
		return lit.Record.FieldList()[i].Default
	})
}

func (l *Lowerer) VisitCastExpr(e ast.CastExpr) V {
	v := l.expr(e.E())
	fails := l.typings.Of(e.T.Module).Fails(e.Expr())
	l.fit(v, l.ty(e.Expr()).(*ast.IntTy), l.ty(e.E()), fails, l.overflowTo(fails))
	return v
}

func (l *Lowerer) VisitMatchExpr(e ast.MatchExpr) V { return l.match(e.Match(), l.ty(e.Expr())) }

func (l *Lowerer) VisitPanicExpr(e ast.PanicExpr) V {
	msg := l.expr(e.Message().Expr())
	l.emit(l.b.CallRuntime(NoReg, RtPanic, []V{msg}, nil))
	l.seal(Trap())
	return l.konst(0)
}

func (l *Lowerer) choose(typ ast.Ty, cond V, then, els func() V) V {
	result := l.f.NewSlot(l.kind(typ))
	a := l.b.NewBlock()
	b := l.b.NewBlock()
	end := l.b.NewBlock()
	l.b.Terminate(l.cur, Branch(cond, a, b))
	l.cur = a
	l.scoped(func() { l.store(result, then) })
	l.goTo(end)
	l.cur = b
	l.scoped(func() { l.store(result, els) })
	l.goTo(end)
	return l.fetch(result)
}

// store puts the value of one branch of an expression into the slot that holds it until the branches join, which takes
// the value: it is no longer in flight, and the other branch never had it.
func (l *Lowerer) store(slot int, value func() V) {
	mark := len(l.pending)
	l.emit(StoreSlot(slot, value(), false))
	l.pending = l.pending[:mark]
}

func (l *Lowerer) converted(e ast.Expr, target ast.Ty) V {
	v := l.expr(e)
	// A frozen value given to a let parameter of the type it holds lends its contents.
	have, want := l.binding.Apply(l.ty(e)), l.binding.Apply(target)
	for ast.IsFrozen(have) && have != want {
		v, have = l.contentOnce(e.Position(), v, have)
	}
	return v
}

func (l *Lowerer) VisitNameExpr(n ast.NameExpr) V {
	name := l.typings.Of(n.T.Module).Name(n)
	switch name.Kind() {
	case ast.NameLocal:
		loc := name.Local(l.fn)
		if !loc.IsParam {
			return l.ownedRead(loc.Slot, n.Expr())
		}
		if !loc.ByRef() {
			if owner := l.ownerOf(loc); owner >= 0 {
				return l.ownedRead(owner, n.Expr())
			}
			return l.param(loc.ParamIndex, l.kind(loc.Type))
		}

		d := l.reg(l.kind(loc.Type))
		addr := l.param(loc.ParamIndex, KindScalar)
		l.emit(LoadAt(d, addr))
		return d
	case ast.NameSelfField:
		d := l.reg(l.kind(l.ty(n.Expr())))
		l.loadField(d, l.self(), n.Expr(), name.FieldIndex())
		return d
	case ast.NameConst:
		return l.expr(l.typings.Of(n.T.Module).Const(name).Value)
	}
	panic(diag.Internal("lower: unresolved name '%s'", n.Name()))
}

// loadField is the value of a field of a record: a move out of it when the ownership pass found the expression consuming
// it, which leaves 0 in the field for the drop of the record to skip, and a borrow otherwise.
func (l *Lowerer) loadField(d, obj V, e ast.Expr, index int) {
	if l.consumed(e) && l.f.Regs[d] == KindManaged {
		addr := l.reg(KindScalar)
		l.emit(FieldAddr(addr, obj, WordBytes*index))
		l.emit(MoveAt(d, addr))
		return
	}
	l.emit(LoadField(d, obj, WordBytes*index))
}

func (l *Lowerer) address(e ast.Expr) V {
	switch e.Kind() {
	case ast.KindNameExpr:
		e := e.ToNameExpr()

		name := l.typings.Of(e.T.Module).Name(e)
		switch name.Kind() {
		case ast.NameLocal:
			loc := name.Local(l.fn)
			if loc.ByRef() {
				return l.param(loc.ParamIndex, KindScalar)
			}
			d := l.reg(KindScalar)
			l.emit(SlotAddr(d, loc.Slot))
			return d
		case ast.NameSelfField:
			d := l.reg(KindScalar)
			obj := l.self()
			l.emit(FieldAddr(d, obj, WordBytes*name.FieldIndex()))
			return d
		}
	case ast.KindMemberExpr:
		e := e.ToMemberExpr()

		if member := l.typings.Of(e.T.Module).Member(e); member.Kind == ast.MemberField {
			obj := l.read(e.Obj())
			d := l.reg(KindScalar)
			l.emit(FieldAddr(d, obj, WordBytes*member.Field.Index))
			return d
		}
	case ast.KindIndexExpr:
		e := e.ToIndexExpr()

		return l.elemAddr(e)
	}
	panic(diag.Internal("lower: no address for this expression"))
}

func (l *Lowerer) VisitCallExpr(node ast.CallExpr) V {
	mark := len(l.pending)
	c := l.call(node)
	switch c.Kind {
	case ast.CallPrint, ast.CallPrintError:
		text := l.expr(c.Ordered[0])
		handle := l.reg(KindScalar)
		symbol := RtStderr
		if c.Kind == ast.CallPrint {
			symbol = RtStdout
		}
		l.emit(LoadGlobal(handle, symbol))
		l.emit(l.b.CallRuntime(NoReg, RtWrite, []V{handle, text}, nil))
		return l.konst(0)
	case ast.CallFunction, ast.CallStatic, ast.CallMethod, ast.CallBound:
		if c.Kind != ast.CallBound && c.Target.Intrinsic != ast.NotIntrinsic {
			return l.intrinsic(c)
		}
		target := l.callee(ast.CallUse{Info: c.CallInfo, Receiver: c.Receiver, Site: c.Position()})
		base := len(l.args)
		if !c.Receiver.IsNil() {
			l.push(l.read(c.Receiver))
		}
		for i := range c.Ordered {
			if c.Pass.Specs[i].Conv.ByAddress() {
				l.push(l.address(c.Ordered[i]))
			} else {
				l.push(l.converted(c.Ordered[i], c.Pass.Specs[i].Type))
			}
		}
		dst := NoReg
		if l.ty(c.CallExpr.Expr()) != ast.Ty(ast.VoidTy) {
			dst = l.reg(l.kind(l.ty(c.CallExpr.Expr())))
		}
		if target.ErrorType == nil {
			l.emit(l.b.Call(dst, target, l.args[base:]))
			l.args = l.args[:base]
		} else {
			err := l.reg(KindScalar)
			l.emit(l.b.ThrowingCall(dst, target, l.args[base:], err))
			l.args = l.args[:base]
			// The callee has the arguments now, and drops them itself when it fails.
			l.pending = l.pending[:mark]
			ok := l.b.NewBlock()
			failed := l.compare(CmpNe, err, l.konst(0), false)
			l.b.Terminate(l.cur, Branch(failed, l.failure(0, err), ok))
			l.cur = ok
		}
		if dst == NoReg {
			return l.konst(0)
		}
		return dst
	case ast.CallCopy:
		return l.copyValue(c.CallExpr.Position(), l.expr(c.Receiver), l.ty(c.Receiver))
	case ast.CallUnionCtor:
		tag := int(c.CaseIndex)
		return l.record(l.ty(c.CallExpr.Expr()), len(c.Ordered), &tag, func(i int) ast.Expr { return c.Ordered[i] })
	}
	panic(diag.Internal("lower: unresolved call"))
}

// callee is the function a call runs. Every instance a body reaches was found for the module before lowering, so a
// missing one is a compiler bug: the executable would depend on another module having made the instance.
func (l *Lowerer) callee(use ast.CallUse) *ast.FuncDecl {
	target := l.typings.Of(use.Site.Mod).Resolved(use, l.binding)
	if target.Generic != nil && !l.instances.has(target) {
		panic(diag.Internal("lower: %s was not found among the instances of the module", target.Symbol()))
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

// record is a new box of count fields of the type t, which the tag, when it has one, makes a union: value is the expression
// of each field. The box is in flight while its fields are computed, and owns each one once it is stored.
func (l *Lowerer) record(t ast.Ty, count int, tag *int, value func(i int) ast.Expr) V {
	baseOffset := 0
	if tag != nil {
		baseOffset = WordBytes
	}
	p := l.reg(KindManaged)
	l.emit(Alloc(p, BoxBytes(count, tag != nil)))
	if tag != nil {
		t := l.konst(int64(*tag))
		l.emit(StoreField(p, TagOffset, t))
	}
	mark := len(l.pending)
	if count > 0 {
		l.pending = append(l.pending, pendingDrop{reg: p, ty: l.dropType(t)})
	}
	for i := range count {
		v := l.expr(value(i))
		l.emit(StoreField(p, baseOffset+WordBytes*i, v))
		l.pending = l.pending[:mark+1]
	}
	l.pending = l.pending[:mark]
	return p
}

// copyValue is a new value equal to v, which every part of it owns: the deep copy that CopyInstr says. A scalar is
// the value itself. site is the source that needs the copy, kept when the type holds something that has no native code.
func (l *Lowerer) copyValue(site ast.Pos, v V, t ast.Ty) V {
	ty := l.binding.Apply(t)
	if kindOf(ty) != KindManaged {
		return v
	}
	l.requireNative(site, ty)
	d := l.reg(KindManaged)
	l.emit(l.b.Copy(d, v, ty))
	return d
}

func (l *Lowerer) tag(union V) V {
	d := l.reg(KindScalar)
	l.emit(LoadField(d, union, TagOffset))
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

func (l *Lowerer) VisitMemberExpr(m ast.MemberExpr) V {
	member := l.typings.Of(m.T.Module).Member(m)
	switch member.Kind {
	case ast.MemberField:
		obj := l.read(m.Obj())
		d := l.reg(l.kind(l.ty(m.Expr())))
		l.loadField(d, obj, m.Expr(), member.Field.Index)
		return d
	case ast.MemberEnumCase:
		return l.konst(int64(member.CaseIndex))
	case ast.MemberUnionCase:
		tag := int(member.CaseIndex)
		return l.record(l.ty(m.Expr()), 0, &tag, nil)
	case ast.MemberEnumOrdinal:
		return l.expr(m.Obj())
	case ast.MemberEnumName:
		v := l.expr(m.Obj())
		return l.names(v, l.ty(m.Obj()).(*ast.EnumTy).Cases)
	case ast.MemberEnumRaw:
		et := l.ty(m.Obj()).(*ast.EnumTy)
		v := l.expr(m.Obj())
		if et.HasExplicitRaw {
			return l.selectValue(v, KindScalar, len(et.Raw), func(i int) V { return l.konst(et.Raw[i].Bits()) })
		}
		return v
	case ast.MemberUnionOrdinal:
		return l.tag(l.expr(m.Obj()))
	case ast.MemberUnionName:
		t := l.tag(l.expr(m.Obj()))
		return l.names(t, unionCaseNames(l.ty(m.Obj()).(*ast.UnionTy)))
	}
	panic(diag.Internal("lower: unresolved member"))
}

func (l *Lowerer) names(index V, names []string) V {
	return l.selectValue(index, KindManaged, len(names), func(i int) V { return l.str(names[i]) })
}

func (l *Lowerer) selectValue(index V, kind IrKind, count int, value func(int) V) V {
	result := l.f.NewSlot(kind)
	end := l.b.NewBlock()
	targets := make([]BlockID, count)
	for i := range count {
		targets[i] = l.b.NewBlock()
	}
	values := make([]int64, count-1)
	for i := range values {
		values[i] = int64(i)
	}
	l.b.Terminate(l.cur, l.b.Switch(index, values, targets[:count-1], targets[count-1]))
	for i := range count {
		l.cur = targets[i]
		v := value(i)
		l.emit(StoreSlot(result, v, false))
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
func (l *Lowerer) VisitStrLit(s ast.StrLit) V {
	parts := s.Parts()
	if parts.IsPlain() {
		if parts.Len() == 1 {
			return l.str(parts.At(0).Text)
		}
		var text strings.Builder
		for _, p := range parts.All() {
			text.WriteString(p.Text)
		}
		return l.str(text.String())
	}
	acc, owner := l.part(parts.At(0))
	if parts.Len() == 1 && owner == textBorrowed {
		return l.copyValue(s.Position(), acc, ast.StringTy)
	}
	for i := 1; i < parts.Len(); i++ {
		mark := len(l.pending)
		if owner == textFresh {
			l.pending = append(l.pending, pendingDrop{reg: acc, ty: ast.StringTy})
		}
		next, nextOwner := l.part(parts.At(i))
		l.pending = l.pending[:mark]
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

func (l *Lowerer) part(part ast.StrPart) (V, textOwner) {
	if part.Expr.IsNil() {
		return l.str(part.Text), textImmortal
	}
	e := part.Expr
	v := l.read(e)
	switch t := ast.Content(l.binding.Apply(l.ty(e))).(type) {
	case *ast.PrimTy:
		if t == ast.StringTy {
			if l.isImmortal(e) {
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
	panic(diag.Internal("lower: cannot interpolate %s", l.ty(e).Name()))
}

func (l *Lowerer) VisitBinaryExpr(b ast.BinaryExpr) V {
	if (b.Op() == "and" || b.Op() == "or") && l.ty(b.Expr()) == ast.Ty(ast.BoolTy) {
		result := l.f.NewSlot(KindScalar)
		rhs := l.b.NewBlock()
		end := l.b.NewBlock()
		lv := l.expr(b.L())
		l.emit(StoreSlot(result, lv, false))
		if b.Op() == "and" {
			l.b.Terminate(l.cur, Branch(lv, rhs, end))
		} else {
			l.b.Terminate(l.cur, Branch(lv, end, rhs))
		}
		l.cur = rhs
		l.scoped(func() { l.emit(StoreSlot(result, l.expr(b.R()), false)) })
		l.goTo(end)
		return l.load(result)
	}

	operand := l.typings.Of(b.T.Module).Operand(b)
	if operand == ast.OperandString {
		left := l.expr(b.L())
		right := l.expr(b.R())
		eq := l.runtime(RtStreq, KindScalar, left, right)
		if b.Op() == "!=" {
			return l.flip(eq)
		}
		return eq
	}

	a := l.expr(b.L())
	c := l.expr(b.R())
	unsigned := operand == ast.OperandUnsigned
	if cmp, ok := lowerCmpOps[b.Op()]; ok {
		return l.compare(cmp, a, c, unsigned)
	}
	d := l.reg(KindScalar)
	op, ok := lowerBinOps[b.Op()]
	if !ok {
		panic(diag.Internal("lower: unknown operator '%s'", b.Op()))
	}
	fails := l.typings.Of(b.T.Module).Fails(b.Expr())
	if fails&ast.FailZero != 0 {
		l.emit(Guard(c, GuardNonZero, 0, l.failure(ast.ErrorDivisionByZero, NoReg)))
	}
	if fails&ast.FailShift != 0 {
		l.emit(Guard(c, GuardNonNegative, 0, l.failure(ast.ErrorNegativeShift, NoReg)))
	}
	overflow := l.overflowTo(fails)
	wordFail := NoBlock
	if fails&ast.FailOverflow != 0 {
		wordFail = overflow
	}
	l.emit(Bin(d, op, a, c, unsigned, wordFail))
	if t, ok := l.ty(b.Expr()).(*ast.IntTy); ok {
		l.rangeCheck(d, t, unsigned, fails, overflow)
	}
	return d
}

// overflowTo is where an operation that can fail as fails says goes when its result does not fit, or NoBlock when it
// always fits.
func (l *Lowerer) overflowTo(fails ast.Fails) BlockID {
	if fails&(ast.FailOverflow|ast.FailBelow|ast.FailAbove) == 0 {
		return NoBlock
	}
	return l.failure(ast.ErrorOverflow, NoReg)
}

func (l *Lowerer) flip(boolean V) V {
	d := l.reg(KindScalar)
	one := l.konst(1)
	l.emit(Bin(d, BinXor, boolean, one, false, NoBlock))
	return d
}

func (l *Lowerer) VisitUnaryExpr(u ast.UnaryExpr) V {
	v := l.expr(u.E())
	if u.Op() == "not" && l.ty(u.Expr()) == ast.Ty(ast.BoolTy) {
		return l.flip(v)
	}
	fails := l.typings.Of(u.T.Module).Fails(u.Expr())
	overflow := l.overflowTo(fails)
	d := l.reg(KindScalar)
	if u.Op() == "not" {
		l.emit(Not(d, v))
	} else {
		wordFail := NoBlock
		if fails&ast.FailOverflow != 0 {
			wordFail = overflow
		}
		l.emit(Neg(d, v, wordFail))
	}
	t := l.ty(u.Expr()).(*ast.IntTy)
	l.rangeCheck(d, t, u.Op() == "not" && t.Unsigned(), fails, overflow)
	return d
}

// rangeCheck checks that a result computed in a word, unsigned or signed, is in its type's range, where fails says it
// may not be. The word is the operands', which the type of the result need not share.
func (l *Lowerer) rangeCheck(v V, t *ast.IntTy, unsigned bool, fails ast.Fails, fail BlockID) {
	if unsigned {
		l.fit(v, t, ast.AnonymousU64, fails, fail)
	} else {
		l.fit(v, t, ast.Anonymous64, fails, fail)
	}
}

// fit checks that a value of the type source is in the range of target, which goes to fail when it is not. It checks only
// the bounds that fails says the value may be past, below or above: the range analysis proved the others.
func (l *Lowerer) fit(v V, target *ast.IntTy, source ast.Ty, fails ast.Fails, fail BlockID) {
	if _, ok := source.(*ast.ConstIntTy); ok {
		return
	}
	src := source.(*ast.IntTy)
	if target.Covers(src) && target.Unsigned() == src.Unsigned() {
		return
	}
	below, above := fails&ast.FailBelow != 0, fails&ast.FailAbove != 0

	if target.Unsigned() {
		if below && !src.Unsigned() && src.Min.IsNeg() {
			l.emit(Guard(v, GuardNonNegative, 0, fail))
		}
		if below && target.Min.Gt(ast.MaxI(src.Min, ast.Zero)) {
			l.emit(Guard(v, GuardUnsignedAtLeast, target.Min.Bits(), fail))
		}
		if above && target.Max.Lt(src.Max) {
			l.emit(Guard(v, GuardUnsignedAtMost, target.Max.Bits(), fail))
		}
		return
	}

	srcMax := src.Max
	if src.Unsigned() && src.Max.Gt(ast.I64Max) {
		// A value past i64.max reads as negative in a signed word.
		if above {
			l.emit(Guard(v, GuardNonNegative, 0, fail))
		}
		srcMax = ast.I64Max
	}
	if below && target.Min.Gt(src.Min) {
		l.emit(Guard(v, GuardSignedAtLeast, target.Min.Bits(), fail))
	}
	if above && target.Max.Lt(srcMax) {
		l.emit(Guard(v, GuardSignedAtMost, target.Max.Bits(), fail))
	}
}

func (l *Lowerer) match(m ast.MatchCore, resultType ast.Ty) V {
	st := ast.Content(l.binding.Apply(l.ty(m.Subject())))
	mark := len(l.pending)
	subject := l.read(m.Subject())
	// A subject that the match consumes is the match's from here: each arm takes it apart before anything can fail.
	l.pending = l.pending[:mark]
	end := l.b.NewBlock()
	l.pushJumps(m.ID, jumpTargets{brk: end, depth: len(l.scopes)})
	result := -1
	if resultType != nil {
		result = l.f.NewSlot(l.kind(resultType))
	}

	for _, arm := range m.Arms().All() {
		body := l.b.NewBlock()
		next := l.b.NewBlock()
		if arm.IsDefault() {
			l.goTo(body)
		} else {
			for _, pat := range arm.Patterns().All() {
				l.test(pat, st, subject, body)
			}
			l.goTo(next)
		}
		l.cur = body
		if m.Consumes() {
			l.pushScope(true)
			l.takeApart(arm, subject, st)
		} else {
			l.borrowPayloads(arm, subject)
		}

		switch arm.Form() {
		case ast.ArmThen:
			l.stmt(arm.Action())
		case ast.ArmGives:
			l.scoped(func() { l.store(result, func() V { return l.converted(arm.Value(), resultType) }) })
		case ast.ArmPanic:
			l.scoped(func() { l.expr(arm.Value()) })
		default:
			panic(diag.Internal("lower: unhandled match arm kind"))
		}
		if m.Consumes() {
			l.popScope()
		}
		l.goTo(end)
		l.cur = next
	}
	if !l.b.Terminated(l.cur) {
		l.b.Terminate(l.cur, Trap())
	}
	l.popJumps()
	l.cur = end
	if result < 0 {
		return l.konst(0)
	}
	return l.fetch(result)
}

// borrowPayloads binds the payload an arm names to a borrow of the subject's box.
func (l *Lowerer) borrowPayloads(arm ast.MatchArm, subject V) {
	for _, pat := range arm.Patterns().All() {
		for i, id := range l.typings.Of(pat.T.Module).Pattern(pat).Locals {
			if local := l.fn.LocalAt(id); local != nil {
				kind := l.kind(local.Type)
				d := l.reg(kind)
				l.emit(LoadField(d, subject, WordBytes+WordBytes*i))
				l.emit(StoreSlot(local.Slot, d, kind == KindManaged))
			}
		}
	}
}

// takeApart is the start of an arm of a match that consumes its subject: each payload the arm names moves out of the
// box into a local that owns it, which leaves the field empty, and then the box is dropped, which skips the empty fields
// and drops the payloads the arm does not name.
func (l *Lowerer) takeApart(arm ast.MatchArm, subject V, st ast.Ty) {
	for _, pat := range arm.Patterns().All() {
		for i, id := range l.typings.Of(pat.T.Module).Pattern(pat).Locals {
			local := l.fn.LocalAt(id)
			if local == nil {
				continue
			}
			offset := WordBytes + WordBytes*i
			kind := l.kind(local.Type)
			d := l.reg(kind)
			if kind == KindManaged {
				addr := l.reg(KindScalar)
				l.emit(FieldAddr(addr, subject, offset))
				l.emit(MoveAt(d, addr))
			} else {
				l.emit(LoadField(d, subject, offset))
			}
			l.emit(StoreSlot(local.Slot, d, false))
			l.ownLocal(local.Slot, local.Type)
		}
	}
	l.dropValue(subject, st)
}

func (l *Lowerer) test(pat ast.Pattern, st ast.Ty, subject V, body BlockID) {
	unsigned := isUnsignedInt(st)
	fail := l.b.NewBlock()
	switch {
	case pat.Form() == ast.PatternCase:
		scrutinee := subject
		if _, ok := st.(*ast.UnionTy); ok {
			scrutinee = l.tag(subject)
		}
		idx := l.konst(int64(l.typings.Of(pat.T.Module).Pattern(pat).CaseIndex))
		cmp := l.compare(CmpEq, scrutinee, idx, false)
		l.b.Terminate(l.cur, Branch(cmp, body, fail))
	case pat.Form() == ast.PatternLiteral && st == ast.Ty(ast.StringTy):
		testBlock := l.cur
		lit := l.expr(pat.Value())
		eq := l.runtime(RtStreq, KindScalar, subject, lit)
		l.b.Terminate(testBlock, Branch(eq, body, fail))
	case pat.Form() == ast.PatternLiteral:
		var value int64
		if bl, ok := pat.Value().AsBoolLit(); ok {
			if bl.Value() {
				value = 1
			}
		} else {
			value = l.ty(pat.Value()).(*ast.ConstIntTy).Value.Bits()
		}
		c := l.konst(value)
		cmp := l.compare(CmpEq, subject, c, unsigned)
		l.b.Terminate(l.cur, Branch(cmp, body, fail))
	case pat.Form() == ast.PatternRange:
		lo := l.ty(pat.Value()).(*ast.ConstIntTy).Value
		hi := l.ty(pat.High()).(*ast.ConstIntTy).Value
		if !pat.Inclusive() {
			hi = hi.Sub(ast.One)
		}
		upper := l.b.NewBlock()
		loC := l.konst(lo.Bits())
		geCmp := l.compare(CmpGe, subject, loC, unsigned)
		l.b.Terminate(l.cur, Branch(geCmp, upper, fail))
		l.cur = upper
		hiC := l.konst(hi.Bits())
		leCmp := l.compare(CmpLe, subject, hiC, unsigned)
		l.b.Terminate(l.cur, Branch(leCmp, body, fail))
	default:
		panic(diag.Internal("lower: unhandled pattern kind"))
	}
	l.cur = fail
}
