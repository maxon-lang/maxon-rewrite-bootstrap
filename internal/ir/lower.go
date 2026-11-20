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
	node    ast.Node
	targets jumpTargets
}

// ownerSlot is the slot that owns a consumes parameter: the caller gives the value away, so the callee holds it in a slot
// of its own, which moving from it empties and the end of the function drops.
type ownerSlot struct {
	local *ast.Local
	slot  int
}

type Lowerer struct {
	fn *ast.FuncDecl
	b  *Builder
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

// NewLowering is a lowerer with an arena of its own.
func NewLowering() *Lowering {
	w := &Lowering{arena: &Arena{}}
	w.l.b, w.l.arena = &w.b, w.arena
	return w
}

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
func Lower(fn *ast.FuncDecl, instances *InstanceSet) *IrFunction {
	return NewLowering().Lower(fn, instances)
}

// newLowerer starts the IR of the body of a behaviour, which is lowered while the function around it is, so it has a
// builder of its own and its function goes in the same arena. binding is the one of the function around it.
func (l *Lowerer) newLowerer(fn *ast.FuncDecl, name string) *Lowerer {
	inner := &Lowerer{b: &Builder{}, arena: l.arena}
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
		if prm.Sym != nil {
			l.ownerSlots = append(l.ownerSlots, ownerSlot{local: prm.Sym, slot: slot})
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
	if e.IsConsumed() && l.f.Slots[slot] == KindManaged {
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

func (l *Lowerer) pushJumps(node ast.Node, targets jumpTargets) {
	l.jumps = append(l.jumps, jumpFrame{node: node, targets: targets})
}

func (l *Lowerer) popJumps() { l.jumps = l.jumps[:len(l.jumps)-1] }

// jumpsOf is where a break or a continue of the loop or match goes.
func (l *Lowerer) jumpsOf(node ast.Node) jumpTargets {
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
func (l *Lowerer) block(body []ast.Stmt) {
	l.pushScope(true)
	for _, s := range ast.Live(body) {
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
		l.emit(StoreSlot(s.Sym.Slot, v, false))
	}
	l.ownLocal(s.Sym.Slot, s.Sym.Type)
}

func (l *Lowerer) VisitDiscardStmt(s *ast.DiscardStmt) { l.expr(s.Value) }

func (l *Lowerer) VisitExprStmt(s *ast.ExprStmt) { l.expr(s.E) }

func (l *Lowerer) VisitIfStmt(s *ast.IfStmt) {
	then := l.b.NewBlock()
	end := l.b.NewBlock()
	els := end
	if s.HasElse {
		els = l.b.NewBlock()
	}
	cond := l.condition(s.Cond)
	l.b.Terminate(l.cur, Branch(cond, then, els))
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
	top := l.b.NewBlock()
	body := l.b.NewBlock()
	end := l.b.NewBlock()
	l.pushJumps(s, jumpTargets{brk: end, cont: top, depth: len(l.scopes)})
	l.goTo(top)
	cond := l.condition(s.Cond)
	l.b.Terminate(l.cur, Branch(cond, body, end))
	l.cur = body
	l.block(s.Body)
	l.goTo(top)
	l.popJumps()
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
	l.seal(Ret(v))
}

func (l *Lowerer) VisitBreakStmt(s *ast.BreakStmt) {
	target := l.jumpsOf(s.Target)
	l.unwind(target.depth)
	l.seal(Jump(target.brk))
}

func (l *Lowerer) VisitContinueStmt(s *ast.ContinueStmt) {
	target := l.jumpsOf(s.Target)
	l.unwind(target.depth)
	l.seal(Jump(target.cont))
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
				l.emit(StoreAt(addr, v))
			} else {
				if l.kind(loc.Type) == KindManaged {
					l.emit(l.b.DropSlot(loc.Slot, l.dropType(loc.Type)))
				}
				l.emit(StoreSlot(loc.Slot, v, false))
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
	body := l.b.NewBlock()
	cont := l.b.NewBlock()
	step := l.b.NewBlock()
	exit := l.b.NewBlock()
	l.pushJumps(s, jumpTargets{brk: exit, cont: cont, depth: len(l.scopes)})

	from := l.expr(s.From)
	l.emit(StoreSlot(slot, from, false))
	to := l.expr(s.To)
	l.emit(StoreSlot(limit, to, false))
	pastOp := CmpGe
	if s.Inclusive {
		pastOp = CmpGt
	}
	pastA := l.load(slot)
	pastB := l.load(limit)
	past := l.compare(pastOp, pastA, pastB, unsigned)
	l.b.Terminate(l.cur, Branch(past, exit, body))

	l.cur = body
	l.block(s.Body)
	l.goTo(cont)

	if s.Inclusive {
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
	l.emit(Bin(next, BinAdd, cur, one, unsigned, false))
	l.emit(StoreSlot(slot, next, false))
	if s.Inclusive {
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
	case e.IsConsumed() && !isWholeLocal(e) && !isField(e):
		panic(diag.Internal("lower: an element or a part of a temporary is moved out at %d:%d; the ownership pass should have refused it", e.P().Line(), e.P().Col()))
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

// isField is whether a place is a field of a record, which a move leaves empty.
func isField(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.NameExpr:
		return e.Kind == ast.NameSelfField
	case *ast.MemberExpr:
		return e.Kind == ast.MemberField
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
	return l.record(len(e.Ordered), nil, func(i int) ast.Expr {
		if v := e.Ordered[i]; v != nil {
			return v
		}
		return e.Record.FieldList()[i].Default
	})
}

func (l *Lowerer) VisitCastExpr(e *ast.CastExpr) V {
	v := l.expr(e.E)
	l.fit(v, e.Ty().(*ast.IntTy), e.E.Ty())
	return v
}

func (l *Lowerer) VisitMatchExpr(e *ast.MatchExpr) V { return l.match(e.Match, e.Ty()) }

func (l *Lowerer) VisitPanicExpr(e *ast.PanicExpr) V {
	msg := l.expr(e.Message)
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
	l.scoped(func() { l.emit(StoreSlot(result, then(), false)) })
	l.goTo(end)
	l.cur = b
	l.scoped(func() { l.emit(StoreSlot(result, els(), false)) })
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
		l.emit(LoadAt(d, addr))
		return d
	case ast.NameSelfField:
		d := l.reg(l.kind(n.Ty()))
		l.loadField(d, l.self(), n, n.Field.Index)
		return d
	case ast.NameConst:
		return l.expr(n.Const.Value)
	}
	panic(diag.Internal("lower: unresolved name '%s'", n.Name))
}

// loadField is the value of a field of a record: a move out of it when the ownership pass found the expression consuming
// it, which leaves 0 in the field for the drop of the record to skip, and a borrow otherwise.
func (l *Lowerer) loadField(d, obj V, e ast.Expr, index int) {
	if e.IsConsumed() && l.f.Regs[d] == KindManaged {
		addr := l.reg(KindScalar)
		l.emit(FieldAddr(addr, obj, WordBytes*index))
		l.emit(MoveAt(d, addr))
		return
	}
	l.emit(LoadField(d, obj, WordBytes*index))
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
			l.emit(SlotAddr(d, loc.Slot))
			return d
		case ast.NameSelfField:
			d := l.reg(KindScalar)
			obj := l.self()
			l.emit(FieldAddr(d, obj, WordBytes*e.Field.Index))
			return d
		}
	case *ast.MemberExpr:
		if e.Kind == ast.MemberField {
			obj := l.read(e.Obj)
			d := l.reg(KindScalar)
			l.emit(FieldAddr(d, obj, WordBytes*e.Field.Index))
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
		l.emit(LoadGlobal(handle, symbol))
		l.emit(l.b.CallRuntime(NoReg, RtWrite, []V{handle, text}, nil))
		return l.konst(0)
	case ast.CallFunction, ast.CallStatic, ast.CallMethod, ast.CallBound:
		if c.Kind != ast.CallBound && c.Target.Intrinsic != ast.NotIntrinsic {
			return l.intrinsic(c)
		}
		target := l.callee(c)
		base := len(l.args)
		if c.Receiver != nil {
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
		if c.Ty() != ast.Ty(ast.VoidTy) {
			dst = l.reg(l.kind(c.Ty()))
		}
		l.emit(l.b.Call(dst, target, l.args[base:]))
		l.args = l.args[:base]
		if dst == NoReg {
			return l.konst(0)
		}
		return dst
	case ast.CallCopy:
		return l.copyValue(c, l.expr(c.Receiver), c.Receiver.Ty())
	case ast.CallUnionCtor:
		tag := c.CaseIndex
		return l.record(len(c.Ordered), &tag, func(i int) ast.Expr { return c.Ordered[i] })
	}
	panic(diag.Internal("lower: unresolved call"))
}

// callee is the function a call runs. Every instance a body reaches was found for the module before lowering, so a
// missing one is a compiler bug: the executable would depend on another module having made the instance.
func (l *Lowerer) callee(c *ast.CallExpr) *ast.FuncDecl {
	target := c.Resolved(l.binding)
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

// record is a new box of count fields, which the tag, when it has one, makes a union: value is the expression of each field.
func (l *Lowerer) record(count int, tag *int, value func(i int) ast.Expr) V {
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
	for i := range count {
		v := l.expr(value(i))
		l.emit(StoreField(p, baseOffset+WordBytes*i, v))
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

func (l *Lowerer) VisitMemberExpr(m *ast.MemberExpr) V {
	switch m.Kind {
	case ast.MemberField:
		obj := l.read(m.Obj)
		d := l.reg(l.kind(m.Ty()))
		l.loadField(d, obj, m, m.Field.Index)
		return d
	case ast.MemberEnumCase:
		return l.konst(int64(m.CaseIndex))
	case ast.MemberUnionCase:
		tag := m.CaseIndex
		return l.record(0, &tag, nil)
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
func (l *Lowerer) VisitStrLit(s *ast.StrLit) V {
	if s.IsPlain() {
		if len(s.Parts) == 1 {
			return l.str(s.Parts[0].(string))
		}
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
		rhs := l.b.NewBlock()
		end := l.b.NewBlock()
		lv := l.expr(b.L)
		l.emit(StoreSlot(result, lv, false))
		if b.Op == "and" {
			l.b.Terminate(l.cur, Branch(lv, rhs, end))
		} else {
			l.b.Terminate(l.cur, Branch(lv, end, rhs))
		}
		l.cur = rhs
		l.scoped(func() { l.emit(StoreSlot(result, l.expr(b.R), false)) })
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
	l.emit(Bin(d, op, a, c, unsigned, op == BinAdd || op == BinSub || op == BinMul))
	if t, ok := b.Ty().(*ast.IntTy); ok {
		l.rangeCheck(d, t)
	}
	return d
}

func (l *Lowerer) flip(boolean V) V {
	d := l.reg(KindScalar)
	one := l.konst(1)
	l.emit(Bin(d, BinXor, boolean, one, false, false))
	return d
}

func (l *Lowerer) VisitUnaryExpr(u *ast.UnaryExpr) V {
	v := l.expr(u.E)
	if u.Op == "not" && u.Ty() == ast.Ty(ast.BoolTy) {
		return l.flip(v)
	}
	d := l.reg(KindScalar)
	if u.Op == "not" {
		l.emit(Not(d, v))
	} else {
		l.emit(Neg(d, v))
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
			l.emit(Guard(v, GuardNonNegative, 0))
		}
		if target.Min.Gt(ast.MaxI(src.Min, ast.Zero)) {
			l.emit(Guard(v, GuardUnsignedAtLeast, target.Min.Bits()))
		}
		if target.Max.Lt(src.Max) {
			l.emit(Guard(v, GuardUnsignedAtMost, target.Max.Bits()))
		}
		return
	}

	srcMax := src.Max
	if src.Unsigned() && src.Max.Gt(ast.I64Max) {
		l.emit(Guard(v, GuardNonNegative, 0))
		srcMax = ast.I64Max
	}
	if target.Min.Gt(src.Min) {
		l.emit(Guard(v, GuardSignedAtLeast, target.Min.Bits()))
	}
	if target.Max.Lt(srcMax) {
		l.emit(Guard(v, GuardSignedAtMost, target.Max.Bits()))
	}
}

func (l *Lowerer) match(m *ast.MatchCore, resultType ast.Ty) V {
	st := ast.Content(l.binding.Apply(m.Subject.Ty()))
	subject := l.read(m.Subject)
	end := l.b.NewBlock()
	l.pushJumps(m, jumpTargets{brk: end, depth: len(l.scopes)})
	result := -1
	if resultType != nil {
		result = l.f.NewSlot(l.kind(resultType))
	}

	for _, arm := range m.Arms {
		body := l.b.NewBlock()
		next := l.b.NewBlock()
		if arm.IsDefault {
			l.goTo(body)
		} else {
			for _, pat := range arm.Patterns {
				l.test(pat, st, subject, body)
			}
			l.goTo(next)
		}
		l.cur = body
		if m.Consumes {
			l.pushScope(true)
			l.takeApart(arm, subject, st)
		} else {
			l.borrowPayloads(arm, subject)
		}

		switch arm.Kind() {
		case ast.ArmThen:
			l.stmt(arm.Action)
		case ast.ArmGives:
			l.scoped(func() { l.emit(StoreSlot(result, l.converted(arm.Value, resultType), false)) })
		case ast.ArmPanic:
			l.scoped(func() { l.expr(arm.Value) })
		default:
			panic(diag.Internal("lower: unhandled match arm kind"))
		}
		if m.Consumes {
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
func (l *Lowerer) borrowPayloads(arm *ast.MatchArm, subject V) {
	for _, pat := range arm.Patterns {
		for i, local := range pat.BindingLocals {
			if local != nil {
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
func (l *Lowerer) takeApart(arm *ast.MatchArm, subject V, st ast.Ty) {
	for _, pat := range arm.Patterns {
		for i, local := range pat.BindingLocals {
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

func (l *Lowerer) test(pat *ast.Pattern, st ast.Ty, subject V, body BlockID) {
	unsigned := isUnsignedInt(st)
	fail := l.b.NewBlock()
	switch {
	case pat.Kind() == ast.PatternCase:
		scrutinee := subject
		if _, ok := st.(*ast.UnionTy); ok {
			scrutinee = l.tag(subject)
		}
		idx := l.konst(int64(pat.CaseIndex))
		cmp := l.compare(CmpEq, scrutinee, idx, false)
		l.b.Terminate(l.cur, Branch(cmp, body, fail))
	case pat.Kind() == ast.PatternLiteral && st == ast.Ty(ast.StringTy):
		testBlock := l.cur
		lit := l.expr(pat.Value)
		eq := l.runtime(RtStreq, KindScalar, subject, lit)
		l.b.Terminate(testBlock, Branch(eq, body, fail))
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
		l.b.Terminate(l.cur, Branch(cmp, body, fail))
	case pat.Kind() == ast.PatternRange:
		lo := pat.Value.Ty().(*ast.ConstIntTy).Value
		hi := pat.High.Ty().(*ast.ConstIntTy).Value
		if !pat.Inclusive {
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
