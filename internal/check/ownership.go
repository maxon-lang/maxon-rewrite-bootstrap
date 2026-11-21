package check

import (
	"fmt"
	"maps"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"slices"
)

type ownFactKind uint8

const (
	factMoved ownFactKind = iota
	factUninitialised
	// factPartlyMoved is a local that lost some of its fields, which the fact's parts list.
	factPartlyMoved
)

type ownDestKind uint8

const (
	destVariable ownDestKind = iota
	destParameter
	destField
	destReturn
	destResult
	destElement
	destBehaviour
	destMatch
	destDestructor
)

// ownDest is where a moved value went, for the message that names the move.
type ownDest struct {
	kind ownDestKind
	name string
}

func (d ownDest) String() string {
	switch d.kind {
	case destVariable:
		return "variable '" + d.name + "'"
	case destParameter:
		return "parameter '" + d.name + "'"
	case destField:
		return "field '" + d.name + "'"
	case destReturn:
		return "the return value"
	case destResult:
		return "the result of a conditional or match"
	case destElement:
		return "an element of '" + d.name + "'"
	case destBehaviour:
		return "a behaviour"
	case destMatch:
		return "a 'match consumes'"
	case destDestructor:
		return "a destructor"
	}
	panic(diag.Internal("ownership: unknown move destination %d", int(d.kind)))
}

// ownFact is why a local holds no value, or only some of one: a move out of it, a declaration that left it
// uninitialised, or moves of some of its fields.
type ownFact struct {
	site ast.Pos
	kind ownFactKind
	dest ownDest
	// parts is set for a partly moved local only.
	parts *ownParts
}

// ownDropKind is how a value that nothing moved comes to be dropped, for the message of E3919.
type ownDropKind uint8

const (
	dropScopeEnd ownDropKind = iota
	dropReturn
	dropBreak
	dropContinue
	dropOverwrite
	dropTemporary
	dropDiscard
	dropParameter
	dropPayload
	dropOtherPath
	dropFailure
)

// ownDrop is an implicit drop: the kind, and the name of the variable, parameter or payload it concerns.
type ownDrop struct {
	kind ownDropKind
	name string
}

func (d ownDrop) String() string {
	switch d.kind {
	case dropScopeEnd:
		return "'" + d.name + "' still holds its value when its scope ends, which drops it"
	case dropReturn:
		return "'" + d.name + "' still holds its value at this 'return', which drops it"
	case dropBreak:
		return "'" + d.name + "' still holds its value at this 'break', which drops it"
	case dropContinue:
		return "'" + d.name + "' still holds its value at this 'continue', which drops it"
	case dropOverwrite:
		return "this assignment drops the value that '" + d.name + "' holds"
	case dropTemporary:
		return "this value is dropped when the statement ends, because nothing takes it"
	case dropDiscard:
		return "this value is dropped by the discard"
	case dropParameter:
		return "the parameter '" + d.name + "' drops the value it is given"
	case dropPayload:
		return "this arm drops the payload '" + d.name + "' that it does not name"
	case dropOtherPath:
		return "'" + d.name + "' is moved on one path only, so the other path drops it"
	case dropFailure:
		return "'" + d.name + "' still holds its value where this can fail, and a failure leaves its scope, which drops it"
	}
	panic(diag.Internal("ownership: unknown drop %d", int(d.kind)))
}

// ownObliged is a local that owns a value which cannot be dropped, so it must be moved or ended before its scope
// does. site is where it is declared.
type ownObliged struct {
	local *ast.Local
	site  ast.Pos
	// destroys is the self of a destructor: it is not dropped as a whole, it ends by dropping the fields that are left.
	destroys bool
}

type ownState struct {
	facts map[*ast.Local]ownFact
	dead  bool
}

var (
	ownUnreachable = &ownState{dead: true}
	ownClean       = &ownState{}
)

func (s *ownState) tryGet(l *ast.Local) (ownFact, bool) {
	if s.facts == nil {
		return ownFact{}, false
	}
	fact, ok := s.facts[l]
	return fact, ok
}

func (s *ownState) with(l *ast.Local, fact ownFact) *ownState {
	if s.dead {
		return s
	}
	m := make(map[*ast.Local]ownFact, len(s.facts)+1)
	maps.Copy(m, s.facts)
	m[l] = fact
	return &ownState{facts: m}
}

func (s *ownState) without(l *ast.Local) *ownState {
	if s.dead || s.facts == nil {
		return s
	}
	if _, ok := s.facts[l]; !ok {
		return s
	}
	if len(s.facts) == 1 {
		return ownClean
	}
	m := make(map[*ast.Local]ownFact, len(s.facts))
	for k, v := range s.facts {
		if k != l {
			m[k] = v
		}
	}
	return &ownState{facts: m}
}

func mergeOwn(a, b *ownState) *ownState {
	if a == b || b.dead || (b.facts == nil && !a.dead) {
		return a
	}
	if a.dead || a.facts == nil {
		return b
	}
	var m map[*ast.Local]ownFact
	for k, v := range b.facts {
		if av, found := a.facts[k]; found {
			if av.parts == nil && v.parts == nil {
				continue
			}
			if v = mergeFacts(av, v); v == av {
				continue
			}
		}
		if m == nil {
			m = make(map[*ast.Local]ownFact, len(a.facts)+1)
			maps.Copy(m, a.facts)
		}
		m[k] = v
	}
	if m == nil {
		return a
	}
	return &ownState{facts: m}
}

type ownFrame struct {
	target    ast.NodeID
	breaks    *ownState
	continues *ownState
	// obliged is the length of the obliged stack where the loop or match starts: a jump to it leaves the locals above.
	obliged int
}

type ownProjectionKind uint8

const (
	projectMatch ownProjectionKind = iota
	projectArgument
	projectTarget
	projectOperand
	projectCollection
	projectPart
)

// ownProjection is a place that stays borrowed while code runs which must not change it: a match subject whose
// payload bindings borrow from it while an arm runs, or a place that an expression has evaluated to a borrow and uses
// again once the rest of the expression has run: an argument of a call while the arguments after it are evaluated, the
// target of an assignment while its value is computed, the left operand of a string comparison, the collection of a
// subscript while its index is computed, the first part of an interpolated string while the second is. The borrow is
// taken before the code runs and lasts until it is used, so what the code changes would leave it pointing at what
// moved.
type ownProjection struct {
	subject accessPath
	// place is the expression whose path is the subject, until the path is needed: finding one allocates, and most
	// calls never meet a conflict. Once found, subject holds it, with no root if the expression is not a place.
	place ast.Expr
	kind  ownProjectionKind
	// ty is the type of the borrowed place, which a message needs to say whether a copy of it can be had.
	ty ast.Ty
}

// callAccess is an argument of a call that names a place, with how the callee takes it.
type callAccess struct {
	expr ast.Expr
	path accessPath
	conv ast.Convention
}

// Ownership checks a function's values after typing: that every value has one owner and is not used after it moved
// or before it exists, that each argument follows its parameter's convention, that a borrow is never kept, and that
// no two accesses to one place overlap.
type Ownership struct {
	errors *[]*diag.CompileError
	// typing is what the checker found out about the module's nodes, and where the pass marks the expressions it finds
	// consumed.
	typing *ast.Typing
	// checker answers which types can be copied, for the messages that suggest a copy.
	checker     *Checker
	fn          *ast.FuncDecl
	frames      []*ownFrame
	framePool   []*ownFrame
	projections []ownProjection
	setParams   []*ast.Local
	// initialising are the variables that the call being checked gives a value, through sets parameters. They have one
	// when the call has run, so the arguments of that same call still find them empty.
	initialising []*ast.Local
	steps        []pathStep
	accesses     []callAccess
	// obliged are the locals in scope that own a value which cannot be dropped, in declaration order. A scope ends
	// by checking and removing the ones it declared; the facts of an obliged local say that it holds nothing.
	obliged []ownObliged
	// partials counts the partly moved facts the function has made: while it is 0, a read or an assignment of a field
	// path needs no look at the facts.
	partials int
	// catches are the trys around the code being checked, innermost last.
	catches []ownCatch
	// inFlight are the values of the expression being checked that have been computed for something that takes them and
	// that cannot be dropped: a failure before the taker runs would drop them.
	inFlight []ast.Expr
}

// ownCatch is a try being checked. A failure in what it covers goes to its handler, leaving the scopes opened since the
// try began, or with no handler leaves the function; entry is the join of the states at those failures.
type ownCatch struct {
	propagates bool
	entry      *ownState
	obliged    int
}

const ownStepCapacity = 8

func NewOwnership(errs *[]*diag.CompileError, checker *Checker) *Ownership {
	return &Ownership{errors: errs, typing: checker.typing, checker: checker, steps: make([]pathStep, 0, ownStepCapacity)}
}

// ty is the type the checker gave an expression.
func (o *Ownership) ty(e ast.Expr) ast.Ty { return o.typing.Type(e) }

// Check checks a function. The checker answers for the function's own bounds on its type's parameters from here on.
func (o *Ownership) Check(f *ast.FuncDecl) {
	o.checker.enterBounds(f)
	o.run(f)
}

// run checks a function, or the body of a behaviour in it, which has the bounds of the function around it.
func (o *Ownership) run(f *ast.FuncDecl) {
	o.fn = f
	o.frames = o.frames[:0]
	o.projections = o.projections[:0]
	o.setParams = o.setParams[:0]
	o.obliged = o.obliged[:0]
	o.partials = 0
	o.catches = o.catches[:0]
	o.inFlight = o.inFlight[:0]
	s := ownClean
	switch {
	case f.IsDestructor():
		o.obliged = append(o.obliged, ownObliged{local: f.SelfLocal, site: f.Position(), destroys: true})
	case f.IsMethod() && f.SelfLocal.Conv == ast.ConvConsumes:
		o.oblige(f.SelfLocal, f.Position())
	}
	for i, prm := range f.Params {
		sym := f.LocalAt(f.ParamLocals[i])
		switch ast.ConventionOf(prm.Convention) {
		case ast.ConvSets:
			o.setParams = append(o.setParams, sym)
			s = s.with(sym, ownFact{site: prm.Position(), kind: factUninitialised})
		case ast.ConvConsumes:
			if sym == nil {
				o.requireDroppable(prm.Position(), f.ParamTypes[i], ownDrop{kind: dropParameter, name: prm.Name})
			} else {
				o.oblige(sym, prm.Position())
			}
		}
	}
	s = o.walk(f.Body, s)
	o.endScope(0, s)
	o.requireSetsInitialised(s, f.Position())
}

// oblige records a local that owns a value of a type that cannot be dropped.
func (o *Ownership) oblige(l *ast.Local, site ast.Pos) {
	if l.Type.IsManaged() && o.checker.lacks(droppable, l.Type) != nil {
		o.obliged = append(o.obliged, ownObliged{local: l, site: site})
	}
}

// requireDroppable is E3919: a value of type t is dropped implicitly, which only a type with Droppable allows.
func (o *Ownership) requireDroppable(site ast.Pos, t ast.Ty, d ownDrop) {
	if !t.IsManaged() {
		return
	}
	if why, fix, lacking := o.checker.whyNot(droppable, t); lacking {
		o.fail(site, diag.ErrNotDroppable, "%s, but '%s' cannot be dropped (%s); %s, or end it with one of its destructors or move it on", d, t.Name(), why, fix)
	}
}

// requireTemporaryDroppable is the drop of a fresh value that the statement does not give to anything.
func (o *Ownership) requireTemporaryDroppable(e ast.Expr, s *ownState, kind ownDropKind) {
	if !s.dead && o.ty(e).IsManaged() && !o.typing.IsStorage(e) {
		o.requireDroppable(e.Position(), o.ty(e), ownDrop{kind: kind})
	}
}

// requireEnded checks the obliged locals from index from on at a place where they are dropped: each must hold nothing.
// The scope's end reports at the declaration, an exit at its own statement.
func (o *Ownership) requireEnded(from int, s *ownState, site ast.Pos, kind ownDropKind) {
	if s.dead {
		return
	}
	for _, ob := range o.obliged[from:] {
		at := site
		if kind == dropScopeEnd {
			at = ob.site
		}
		o.requireHeld(at, ob, s, kind)
	}
}

// strip removes the facts of the obliged locals from index from on, so none outlives its local.
func (o *Ownership) strip(from int, s *ownState) *ownState {
	for _, ob := range o.obliged[from:] {
		s = s.without(ob.local)
	}
	return s
}

// endScope ends the scope that started at mark: its obliged locals must hold nothing.
func (o *Ownership) endScope(mark int, s *ownState) *ownState {
	if len(o.obliged) == mark {
		return s
	}
	o.requireEnded(mark, s, ast.Pos{}, dropScopeEnd)
	s = o.strip(mark, s)
	o.obliged = o.obliged[:mark]
	return s
}

// leave is a jump out of the scopes above from: their obliged locals must hold nothing, and their facts do not travel
// on to where the jump lands.
func (o *Ownership) leave(from int, s *ownState, site ast.Pos, kind ownDropKind) *ownState {
	if len(o.obliged) == from {
		return s
	}
	o.requireEnded(from, s, site, kind)
	return o.strip(from, s)
}

// join is the state where two paths meet. A local that owns a value that cannot be dropped must hold nothing on both
// paths or on neither: one path would drop it. The merged state keeps the fact, so a mistake is reported once.
func (o *Ownership) join(a, b *ownState) *ownState {
	merged := mergeOwn(a, b)
	if len(o.obliged) == 0 || a.dead || b.dead {
		return merged
	}
	for _, ob := range o.obliged {
		fa, inA := a.tryGet(ob.local)
		fb, inB := b.tryGet(ob.local)
		if inA == inB && (!inA || (fa.parts == nil && fb.parts == nil)) {
			continue
		}
		o.lostOnOnePath(ob, fa, inA, fb, inB)
		o.lostOnOnePath(ob, fb, inB, fa, inA)
	}
	return merged
}

// disagreeWhole is E3919 for a local that holds nothing on one path only; fact is that path's.
func (o *Ownership) disagreeWhole(ob ownObliged, fact ownFact) {
	switch fact.kind {
	case factMoved:
		o.fail(fact.site, diag.ErrNotDroppable,
			"'%s' is moved into %s here on one path only, so the other path would drop it, but '%s' cannot be dropped; move it on every path, or end it on every path",
			ob.local.Name, fact.dest, ob.local.Type.Name())
	case factUninitialised:
		o.fail(fact.site, diag.ErrNotDroppable,
			"'%s' is assigned on one path only, so the other path would drop or overwrite a value that it does not have, but '%s' cannot be dropped; assign it on every path",
			ob.local.Name, ob.local.Type.Name())
	default:
		panic(diag.Internal("ownership: unknown fact kind %d", int(fact.kind)))
	}
}

// report records an error once: a loop body is walked twice and would repeat a finding that does not depend on the
// state.
func (o *Ownership) report(err *diag.CompileError) {
	for _, e := range *o.errors {
		if e.Code == err.Code && e.Site == err.Site {
			return
		}
	}
	*o.errors = append(*o.errors, err)
}

// lineOf is the line a site is on, for a message that says where a value was moved to.
func (o *Ownership) lineOf(site ast.Pos) int { return o.checker.project.Line(site.Position()) }

func (o *Ownership) fail(site ast.Pos, code, format string, args ...any) {
	o.report(site.Position().Error(code, fmt.Sprintf(format, args...)))
}

// pushFrame opens a frame for a loop or match, reusing the frames of earlier ones: a function with loops would
// otherwise allocate one for each.
func (o *Ownership) pushFrame(target ast.NodeID) *ownFrame {
	depth := len(o.frames)
	if depth == len(o.framePool) {
		o.framePool = append(o.framePool, &ownFrame{})
	}
	frame := o.framePool[depth]
	*frame = ownFrame{target: target, breaks: ownUnreachable, continues: ownUnreachable, obliged: len(o.obliged)}
	o.frames = append(o.frames, frame)
	return frame
}

func (o *Ownership) frameOf(target ast.NodeID) *ownFrame {
	for _, v := range slices.Backward(o.frames) {
		if v.target == target {
			return v
		}
	}
	panic(diag.Internal("ownership: a jump has no enclosing frame"))
}

// walk checks a block, which is a scope: the locals it declares end with it.
func (o *Ownership) walk(body ast.StmtList, s *ownState) *ownState {
	mark := len(o.obliged)
	for _, st := range body.All() {
		s = o.stmt(st, s)
	}
	return o.endScope(mark, s)
}

func (o *Ownership) stmt(st ast.Stmt, s *ownState) *ownState {
	mark := len(o.inFlight)
	defer o.land(mark)
	return ast.VisitStmtState[*ownState, *ownState](o, st, s)
}

func (o *Ownership) VisitLetStmt(st ast.LetStmt, s *ownState) *ownState {
	sym := o.fn.LocalAt(o.typing.LetLocal(st))
	if st.Init().IsNil() {
		o.oblige(sym, st.Position())
		return s.with(sym, ownFact{site: st.Position(), kind: factUninitialised})
	}
	s = o.consume(st.Init(), ownDest{kind: destVariable, name: st.Name()}, s)
	o.oblige(sym, st.Position())
	return s.without(sym)
}

func (o *Ownership) VisitAssignStmt(st ast.AssignStmt, s *ownState) *ownState {
	switch st.Target().Kind() {
	case ast.KindNameExpr:
		target := st.Target().ToNameExpr()

		name := o.typing.Name(target)
		switch name.Kind() {
		case ast.NameLocal:
			local := name.Local(o.fn)
			s = o.consume(st.Value(), ownDest{kind: destVariable, name: target.Name()}, s)
			o.requireWritable(st.Position(), accessPath{root: local})
			if !s.dead {
				o.requireHeld(st.Position(), ownObliged{local: local}, s, dropOverwrite)
			}
			return s.without(local)
		case ast.NameSelfField:
			s = o.consumeInto(target.Expr(), st.Value(), ownDest{kind: destField, name: target.Name()}, s)
			p, _ := o.pathOf(target.Expr(), o.steps[:0])
			o.requireWritable(st.Position(), p)
			if next, done := o.assignPart(st, target.Expr(), p, s); done {
				return next
			}
			o.requireOverwriteDroppable(st, target.Expr(), s)
			return o.readRoot(o.fn.SelfLocal, target.Position(), s)
		case ast.NameConst:
			s = o.consume(st.Value(), ownDest{kind: destVariable, name: target.Name()}, s)
			o.fail(st.Position(), diag.ErrAssignToImmutable, "'%s' is a constant and cannot be reassigned", target.Name())
			return s
		}
	case ast.KindMemberExpr:
		target := st.Target().ToMemberExpr()

		s = o.consumeInto(target.Expr(), st.Value(), ownDest{kind: destField, name: target.Name()}, s)
		p, ok := o.pathOf(target.Expr(), o.steps[:0])
		if !ok {
			o.fail(st.Position(), diag.ErrAssignToImmutable, "the field '%s' belongs to a temporary value, so assigning it changes nothing; assign it on a variable", target.Name())
			return s
		}
		o.requireWritable(st.Position(), p)
		if next, done := o.assignPart(st, target.Expr(), p, s); done {
			return next
		}
		o.requireOverwriteDroppable(st, target.Expr(), s)
		return o.eval(target.Obj(), s)
	case ast.KindIndexExpr:
		target := st.Target().ToIndexExpr()

		s = o.consumeInto(target.Expr(), st.Value(), ownDest{kind: destElement, name: placeText(target.Obj())}, s)
		p, ok := o.pathOf(target.Expr(), o.steps[:0])
		if !ok {
			o.fail(st.Position(), diag.ErrAssignToImmutable, "the element belongs to a temporary collection, so assigning it changes nothing; assign it on a variable")
			return s
		}
		o.requireWritable(st.Position(), p)
		o.requireOverwriteDroppable(st, target.Expr(), s)
		return o.subscript(target, s)
	}
	panic(diag.Internal("ownership: unhandled assignment target"))
}

// requireOverwriteDroppable is the drop of the value a field or an element holds when an assignment replaces it.
func (o *Ownership) requireOverwriteDroppable(st ast.AssignStmt, target ast.Expr, s *ownState) {
	if !s.dead {
		o.requireDroppable(st.Position(), o.ty(target), ownDrop{kind: dropOverwrite, name: placeText(target)})
	}
}

// consumeInto is the value stored into a part of a variable. The part's address is taken before the value is computed,
// so the target stays borrowed while it is: a call in the value that changes the variable could otherwise make the
// store land where the part used to be.
func (o *Ownership) consumeInto(target ast.Expr, value ast.Expr, dest ownDest, s *ownState) *ownState {
	held := len(o.projections)
	o.hold(target, projectTarget, true)
	s = o.consume(value, dest, s)
	o.release(held)
	return s
}

func (o *Ownership) VisitDiscardStmt(st ast.DiscardStmt, s *ownState) *ownState {
	s = o.eval(st.Value(), s)
	o.requireTemporaryDroppable(st.Value(), s, dropDiscard)
	return s
}

func (o *Ownership) VisitExprStmt(st ast.ExprStmt, s *ownState) *ownState {
	s = o.expr(st.E(), s)
	if _, ok := st.E().AsPanicExpr(); ok {
		return ownUnreachable
	}
	return s
}

func (o *Ownership) VisitIfStmt(st ast.IfStmt, s *ownState) *ownState {
	s = o.expr(st.Cond(), s)
	a := o.walk(st.Then(), s)
	b := s
	if st.HasElse() {
		b = o.walk(st.Else(), s)
	}
	return o.join(a, b)
}

func (o *Ownership) VisitWhileStmt(st ast.WhileStmt, s *ownState) *ownState {
	return o.loop(st.ID, s, st.Body(), st.Cond())
}

func (o *Ownership) VisitForStmt(st ast.ForStmt, s *ownState) *ownState {
	return o.loop(st.ID, o.expr(st.To(), o.expr(st.From(), s)), st.Body(), ast.Expr{})
}

// VisitWhenStmt schedules a behaviour. The cowns it names are only read: the behaviour takes a count of its own on each
// when it is scheduled. Each variable its body uses moves into it, so the enclosing function cannot use it again, and
// the body is checked as a function of its own.
func (o *Ownership) VisitWhenStmt(st ast.WhenStmt, s *ownState) *ownState {
	for _, cn := range st.Cowns().All() {
		s = o.expr(cn.Expr(), s)
	}
	behaviour := o.typing.When(st)
	for _, cp := range behaviour.Captures {
		s = o.consume(cp.Ref.Expr(), ownDest{kind: destBehaviour}, s)
	}
	NewOwnership(o.errors, o.checker).run(behaviour.Fn)
	return s
}

func (o *Ownership) VisitMatchStmt(st ast.MatchStmt, s *ownState) *ownState {
	return o.match(st.Match(), s)
}

// VisitTryStmt checks the body of a try block with its failures going to the handler, which starts from the join of the
// states where they happen.
func (o *Ownership) VisitTryStmt(st ast.TryStmt, s *ownState) *ownState {
	if !o.enterTry(st.HasOtherwise()) {
		return o.walk(st.Body(), s)
	}
	end := o.walk(st.Body(), s)
	c := o.leaveTry()
	if !st.HasOtherwise() {
		return end
	}
	return o.join(end, o.walk(st.Handler(), c.entry))
}

// enterTry starts a try: one with a handler catches what it covers, and one without passes it to the try around it, or
// with none around it to the caller. It gives whether the try added a catch.
func (o *Ownership) enterTry(handles bool) bool {
	if !handles && len(o.catches) > 0 {
		return false
	}
	o.catches = append(o.catches, ownCatch{propagates: !handles, entry: ownUnreachable, obliged: len(o.obliged)})
	return true
}

// leaveTry ends a try that enterTry added a catch for, and gives the catch.
func (o *Ownership) leaveTry() ownCatch {
	c := o.catches[len(o.catches)-1]
	o.catches = o.catches[:len(o.catches)-1]
	return c
}

// failure is a place where the operation just checked can fail: the state there goes to the innermost try's handler, or
// out of the function. Either way the scopes the failure leaves are dropped, and so is what is in flight.
func (o *Ownership) failure(site ast.Pos, s *ownState) {
	if s.dead || len(o.catches) == 0 {
		return
	}
	if len(o.inFlight) > 0 {
		e := o.inFlight[len(o.inFlight)-1]
		o.fail(e.Position(), diag.ErrNotDroppable,
			"a failure at line %d would drop this value before what takes it runs, but '%s' cannot be dropped; compute what can fail first, into a variable",
			o.lineOf(site), o.ty(e).Name())
	}
	c := &o.catches[len(o.catches)-1]
	if c.propagates {
		o.requireEnded(0, s, site, dropFailure)
		return
	}
	c.entry = o.join(c.entry, o.leave(c.obliged, s, site, dropFailure))
}

// land forgets the values in flight since mark: what took them has run.
func (o *Ownership) land(mark int) { o.inFlight = o.inFlight[:mark] }

// try checks a try expression, whose expression check runs with the failures going to the try, and gives the state after
// it and the state where its handler starts.
func (o *Ownership) try(e ast.TryExpr, s *ownState, check func(*ownState) *ownState) (*ownState, *ownState) {
	if !o.enterTry(!e.Propagates()) {
		return check(s), ownUnreachable
	}
	s = check(s)
	return s, o.leaveTry().entry
}

// isTransparent is whether a try gives the value of its expression itself, with no other value on a failure: its
// expression is then read or moved as the try is.
func isTransparent(e ast.Expr) (ast.TryExpr, bool) {
	t, ok := e.AsTryExpr()
	return t, ok && t.Transparent()
}

// handled checks what a transparent try does on a failure: return, break, continue or throw, or nothing for a try that
// passes it to the caller.
func (o *Ownership) handled(e ast.TryExpr, entry *ownState) {
	mark := len(o.inFlight)
	switch {
	case !e.Ret().IsNil():
		o.VisitReturnStmt(e.Ret(), entry)
	case !e.Brk().IsNil():
		o.VisitBreakStmt(e.Brk(), entry)
	case !e.Cont().IsNil():
		o.VisitContinueStmt(e.Cont(), entry)
	case !e.Thr().IsNil():
		o.VisitThrowStmt(e.Thr(), entry)
	}
	o.land(mark)
}

// VisitTryExpr is a try whose value comes from its expression or from 'otherwise', like the branches of a conditional, or
// a transparent one, whose expression is only read.
func (o *Ownership) VisitTryExpr(e ast.TryExpr, s *ownState) *ownState {
	if _, ok := isTransparent(e.Expr()); ok {
		end, entry := o.try(e, s, func(s *ownState) *ownState { return o.eval(e.E(), s) })
		o.handled(e, entry)
		return end
	}
	if e.Ignore() {
		end, entry := o.try(e, s, func(s *ownState) *ownState { return o.eval(e.E(), s) })
		return o.join(end, entry)
	}
	dest := ownDest{kind: destResult}
	mark := len(o.inFlight)
	end, entry := o.try(e, s, func(s *ownState) *ownState { return o.consume(e.E(), dest, s) })
	o.land(mark)
	return o.join(end, o.consume(e.Value(), dest, entry))
}

func (o *Ownership) VisitReturnStmt(st ast.ReturnStmt, s *ownState) *ownState {
	if !st.Value().IsNil() {
		s = o.consume(st.Value(), ownDest{kind: destReturn}, s)
	}
	o.requireSetsInitialised(s, st.Position())
	o.requireEnded(0, s, st.Position(), dropReturn)
	return ownUnreachable
}

// VisitThrowStmt is a failure that always happens: it goes to the innermost try's handler, or with none out of the
// function.
func (o *Ownership) VisitThrowStmt(st ast.ThrowStmt, s *ownState) *ownState {
	s = o.eval(st.Value(), s)
	added := o.enterTry(false)
	o.failure(st.Position(), s)
	if added {
		o.leaveTry()
	}
	return ownUnreachable
}

func (o *Ownership) VisitBreakStmt(st ast.BreakStmt, s *ownState) *ownState {
	frame := o.frameOf(o.typing.BreakTarget(st))
	frame.breaks = o.join(frame.breaks, o.leave(frame.obliged, s, st.Position(), dropBreak))
	return ownUnreachable
}

func (o *Ownership) VisitContinueStmt(st ast.ContinueStmt, s *ownState) *ownState {
	frame := o.frameOf(o.typing.ContinueTarget(st))
	frame.continues = o.join(frame.continues, o.leave(frame.obliged, s, st.Position(), dropContinue))
	return ownUnreachable
}

// loop walks the body again from what the first walk added to the state, which is enough for a fact to reach the
// top of the next iteration; a first walk that adds nothing needs no second.
func (o *Ownership) loop(loop ast.NodeID, s *ownState, body ast.StmtList, cond ast.Expr) *ownState {
	// A loop whose condition is the literal true is left by 'break' alone, so no path leaves it from the top or the
	// bottom of the body.
	frame := o.pushFrame(loop)
	entry := s
	if !cond.IsNil() {
		entry = o.expr(cond, s)
	}
	end := ownUnreachable
	for range 2 {
		end = o.walk(body, entry)
		next := o.join(o.join(entry, end), frame.continues)
		if next == entry {
			break
		}
		entry = next
		if !cond.IsNil() {
			entry = o.expr(cond, entry)
		}
	}
	o.frames = o.frames[:len(o.frames)-1]
	if isTrue(cond) {
		return frame.breaks
	}
	return o.join(o.join(entry, end), frame.breaks)
}

func isTrue(e ast.Expr) bool {
	b, ok := e.AsBoolLit()
	return ok && b.Value()
}

func (o *Ownership) match(m ast.MatchCore, s *ownState) *ownState {
	if m.Consumes() {
		o.requireMayTakeApart(m.Subject().Position(), ast.Content(o.ty(m.Subject())), "a 'match consumes'")
		s = o.consume(m.Subject(), ownDest{kind: destMatch}, s)
	} else {
		s = o.expr(m.Subject(), s)
	}
	frame := o.pushFrame(m.ID)
	subject, subjectIsPath, subjectKnown := accessPath{}, false, false
	result := ownUnreachable
	for _, arm := range m.Arms().All() {
		a := s
		mark := len(o.obliged)
		if m.Consumes() {
			o.takeApart(m, arm)
		}
		borrows := o.armBorrows(arm)
		if borrows && !subjectKnown {
			subject, subjectIsPath = o.pathOf(m.Subject(), nil)
			subjectKnown = true
		}
		if borrows && subjectIsPath {
			o.projections = append(o.projections, ownProjection{subject: subject, kind: projectMatch, ty: o.ty(m.Subject())})
		}
		switch arm.Form() {
		case ast.ArmThen:
			a = o.stmt(arm.Action(), a)
		case ast.ArmGives:
			mark := len(o.inFlight)
			a = o.consume(arm.Value(), ownDest{kind: destResult}, a)
			o.land(mark)
		case ast.ArmPanic:
			o.expr(arm.Value(), a)
			a = ownUnreachable
		}
		if borrows && subjectIsPath {
			o.projections = o.projections[:len(o.projections)-1]
		}
		a = o.endScope(mark, a)
		result = o.join(result, a)
	}
	o.frames = o.frames[:len(o.frames)-1]
	return o.join(result, frame.breaks)
}

// armBorrows is whether an arm binds a payload that is not a scalar, which borrows from the subject.
func (o *Ownership) armBorrows(arm ast.MatchArm) bool {
	for _, pat := range arm.Patterns().All() {
		for _, id := range o.typing.Pattern(pat).Locals {
			if l := o.fn.LocalAt(id); l != nil && l.Projected {
				return true
			}
		}
	}
	return false
}

// takeApart is the start of an arm of a match that consumes its subject: each payload the arm names is an owned local of
// the arm, and the payloads it does not name are dropped with the subject.
func (o *Ownership) takeApart(m ast.MatchCore, arm ast.MatchArm) {
	u, ok := ast.Content(o.ty(m.Subject())).(*ast.UnionTy)
	if !ok {
		return
	}
	for _, pat := range arm.Patterns().All() {
		if pat.Form() != ast.PatternCase {
			continue
		}
		info := o.typing.Pattern(pat)
		for i, p := range u.Payload(info.CaseIndex) {
			if i < len(info.Locals) && info.Locals[i] != 0 {
				o.oblige(o.fn.LocalAt(info.Locals[i]), pat.Position())
			} else {
				o.requireDroppable(pat.Position(), p.Type, ownDrop{kind: dropPayload, name: p.Name})
			}
		}
	}
	if arm.IsDefault() {
		o.requireUnnamedCasesDroppable(m, arm, u)
	}
}

// requireUnnamedCasesDroppable is the drop of what a default arm leaves in the subject: the payloads of the cases that
// no arm names.
func (o *Ownership) requireUnnamedCasesDroppable(m ast.MatchCore, arm ast.MatchArm, u *ast.UnionTy) {
	named := make([]bool, len(u.CaseList()))
	for _, other := range m.Arms().All() {
		for _, pat := range other.Patterns().All() {
			if pat.Form() == ast.PatternCase {
				named[o.typing.Pattern(pat).CaseIndex] = true
			}
		}
	}
	for i, isNamed := range named {
		if isNamed {
			continue
		}
		for _, p := range u.Payload(i) {
			o.requireDroppable(arm.Position(), p.Type, ownDrop{kind: dropPayload, name: p.Name})
		}
	}
}

// readRoot is a use of a local's value, which needs one to exist.
func (o *Ownership) readRoot(l *ast.Local, site ast.Pos, s *ownState) *ownState {
	if s.dead {
		return s
	}
	fact, ok := s.tryGet(l)
	if !ok {
		return s
	}
	switch fact.kind {
	case factMoved:
		o.fail(site, diag.ErrUseAfterMove,
			"'%s' was moved into %s at line %d; %s",
			l.Name, fact.dest, o.lineOf(fact.site), o.checker.copyAdvice(l.Type, fmt.Sprintf("move a copy instead: '%s.copy()'", l.Name), "assign it again before reading it"))
	case factUninitialised:
		o.fail(site, diag.ErrUseBeforeInit,
			"'%s' has no value here; assign it on every path before reading it", l.Name)
	case factPartlyMoved:
		m := fact.parts.moves[0]
		o.fail(site, diag.ErrUseAfterMove,
			"'%s' lost '%s', which was moved into %s at line %d, so it cannot be used as a whole; use its other fields, or assign '%s' again first",
			l.Name, m.text(l), m.dest, o.lineOf(m.site), m.text(l))
		return s
	}
	return s.without(l)
}

// consume is an expression whose value is stored, passed to a consumes parameter or returned. Code that can run marks the expression
// consumed, which is how lowering learns where a value changes owner. A scalar is copied, and a fresh value needs
// nothing. A place holding a managed value is moved out of, which only a whole local that owns its value allows;
// everything else is refused and says to copy.
func (o *Ownership) consume(e ast.Expr, dest ownDest, s *ownState) *ownState {
	if t, ok := isTransparent(e); ok {
		end, entry := o.try(t, s, func(s *ownState) *ownState { return o.consume(t.E(), dest, s) })
		o.handled(t, entry)
		return end
	}
	s = o.eval(e, s)
	if s.dead {
		return s
	}
	o.typing.MarkConsumed(e)
	if !o.ty(e).IsManaged() {
		return s
	}
	if o.checker.lacks(droppable, o.ty(e)) != nil {
		o.inFlight = append(o.inFlight, e)
	}
	p, ok := o.pathOf(e, o.steps[:0])
	if !ok {
		switch {
		case o.projectsElement(e):
			o.fail(e.Position(), diag.ErrMoveOutOfField,
				"an element of a collection that is not stored in a variable cannot be moved out of it into %s; bind the collection to a variable and take the element out of it, or copy it",
				dest)
		case o.typing.IsStorage(e):
			o.fail(e.Position(), diag.ErrMoveOutOfField,
				"a part of a temporary value cannot be moved out of it into %s, because the temporary is dropped when the statement ends; %s",
				dest, o.checker.copyAdvice(o.ty(e), "copy it with '.copy()'", ""))
		}
		return s
	}

	switch {
	case p.crossesFrozen():
		o.fail(e.Position(), diag.ErrMoveOutOfField,
			"'%s' is part of a frozen value, which keeps its contents and shares them with its other owners, so it cannot be moved out of it into %s; %s",
			p.text(), dest, o.checker.copyAdvice(o.ty(e), fmt.Sprintf("use '%s.copy()'", p.text()), ""))
		return s
	case p.root.Borrowed():
		o.fail(e.Position(), diag.ErrMoveOutOfBorrow,
			"'%s' is borrowed, not owned, so it cannot be moved into %s; %s",
			p.text(), dest, o.checker.copyAdvice(o.ty(e), fmt.Sprintf("use '%s.copy()' to give it its own copy", p.text()), ""))
		return s
	case p.crossesElement():
		o.fail(e.Position(), diag.ErrMoveOutOfField,
			"'%s' is, or lies inside, an element of a collection, which owns it, so it cannot be moved out of it into %s; %s",
			p.text(), dest, o.checker.copyAdvice(o.ty(e), fmt.Sprintf("use '%s.copy()'", p.text()), "take it out of the collection with its remove method"))
		return s
	case len(p.steps) > 0:
		return o.movePart(e, p, dest, s)
	}
	if p.root.IsSelf && o.fn.IsDestructor() && dest.kind != destDestructor && dest.kind != destMatch {
		o.fail(e.Position(), diag.ErrDestructorMisuse,
			"'self' is moved into %s, but inside a destructor it may only be passed whole to another destructor of its type, which ends it there; move its fields out instead, or call a destructor",
			dest)
		return s
	}
	o.requireNotProjected(p, e.Position())
	return s.with(p.root, ownFact{site: e.Position(), kind: factMoved, dest: dest})
}

// requireWritable refuses a change to a place that is not mutable, and one that a match binding still borrows.
func (o *Ownership) requireWritable(site ast.Pos, p accessPath) {
	root := p.root
	switch {
	case p.crossesFrozen():
		o.fail(site, diag.ErrFrozenChange,
			"'%s' is part of a frozen value, whose contents are immutable and shared with its other owners; change a copy instead: 'var c = thaw(...)'", p.text())
		return
	case root.Projected:
		o.fail(site, diag.ErrAssignToImmutable,
			"'%s' borrows part of the match subject and cannot be changed; %s", root.Name, o.checker.copyAdvice(root.Type, fmt.Sprintf("copy it into a var first: 'var x = %s.copy()'", root.Name), "change the subject itself instead of a binding of it"))
		return
	case root.IsSelf && !root.Mutable():
		o.fail(site, diag.ErrAssignToImmutable,
			"'%s' cannot be changed because 'self' is not mutable here; declare the method with 'mutates self'", p.text())
		return
	case root.IsParam && root.Conv == ast.ConvLet:
		o.fail(site, diag.ErrAssignToImmutable,
			"'%s' cannot be changed because '%s' is a let parameter; write 'mutates' before it to change the caller's value, or copy it into a var",
			p.text(), root.Name)
		return
	case root.IsParam && root.Conv.Owns():
		o.fail(site, diag.ErrAssignToImmutable,
			"'%s' cannot be changed because '%s' is a '%s' parameter; move it into a var first", p.text(), root.Name, root.Conv)
		return
	case !root.Mutable():
		o.fail(site, diag.ErrAssignToImmutable,
			"'%s' cannot be changed because '%s' is a let; declare it with 'var'", p.text(), root.Name)
		return
	}
	for _, st := range p.steps {
		if !st.isElement() && !st.field.IsVar {
			o.fail(site, diag.ErrAssignToImmutable,
				"'%s' cannot be changed because the field '%s' is a let; declare it with 'var'", p.text(), st.field.Name)
			return
		}
	}
	o.requireNotProjected(p, site)
}

// requireNotProjected refuses a change or a move of a place while a binding of an enclosing match, or a place that an
// enclosing expression holds, still borrows part of it.
func (o *Ownership) requireNotProjected(p accessPath, site ast.Pos) {
	for i := range o.projections {
		pr := &o.projections[i]
		if !pr.place.IsNil() {
			pr.subject, _ = o.pathOf(pr.place, nil)
			pr.place = ast.Expr{}
		}
		if pr.subject.root == nil || !pr.subject.overlaps(p) {
			continue
		}
		switch pr.kind {
		case projectMatch:
			o.fail(site, diag.ErrExclusivity,
				"'%s' is borrowed by a binding of an enclosing match on '%s'; %s",
				p.text(), pr.subject.text(), o.checker.copyAdvice(pr.ty, "copy the payload out with '.copy()' before changing it", "change it after the match"))
		case projectArgument:
			o.fail(site, diag.ErrExclusivity,
				"'%s' is borrowed by the argument '%s', which the enclosing call holds while this runs; %s",
				p.text(), pr.subject.text(), o.checker.copyAdvice(pr.ty, fmt.Sprintf("pass a copy: '%s.copy()'", pr.subject.text()), "compute this first into a variable"))
		case projectTarget:
			o.fail(site, diag.ErrExclusivity,
				"'%s' is the target of an assignment whose value is still being computed, and this changes it; compute the value into a variable first",
				pr.subject.text())
		case projectOperand:
			o.fail(site, diag.ErrExclusivity,
				"'%s' is borrowed by the operand '%s', which the enclosing comparison holds while its other operand is evaluated; %s",
				p.text(), pr.subject.text(), o.checker.copyAdvice(pr.ty, fmt.Sprintf("compare a copy: '%s.copy()'", pr.subject.text()), "compute this first into a variable"))
		case projectCollection:
			o.fail(site, diag.ErrExclusivity,
				"'%s' is borrowed by '%s', the collection of an enclosing subscript, which holds it while the index is evaluated; compute the index first into a variable",
				p.text(), pr.subject.text())
		case projectPart:
			o.fail(site, diag.ErrExclusivity,
				"'%s' is borrowed by the part '%s' of an enclosing string, which holds it while the next part is evaluated; %s",
				p.text(), pr.subject.text(), o.checker.copyAdvice(pr.ty, fmt.Sprintf("interpolate a copy: '%s.copy()'", pr.subject.text()), "compute this first into a variable"))
		default:
			panic(diag.Internal("ownership: unknown projection kind %d", int(pr.kind)))
		}
		return
	}
}

// requireSetsInitialised is checked wherever the function returns: each sets parameter holds a value by then.
func (o *Ownership) requireSetsInitialised(s *ownState, site ast.Pos) {
	if s.dead {
		return
	}
	for _, l := range o.setParams {
		if _, uninitialised := s.tryGet(l); uninitialised {
			o.fail(site, diag.ErrSetNotInitialized,
				"the 'sets' parameter '%s' is not initialised on every path; assign it before the function returns", l.Name)
		}
	}
}

// expr is an expression whose value is only read: a fresh value is a temporary that is dropped when the statement ends.
func (o *Ownership) expr(e ast.Expr, s *ownState) *ownState {
	s = o.eval(e, s)
	o.requireTemporaryDroppable(e, s, dropTemporary)
	return s
}

// eval checks an expression and leaves what happens to its value to the caller.
func (o *Ownership) eval(e ast.Expr, s *ownState) *ownState {
	mark := len(o.inFlight)
	s = ast.VisitExprState[*ownState, *ownState](o, e, s)
	o.land(mark)
	return s
}

func (o *Ownership) VisitIntLit(_ ast.IntLit, s *ownState) *ownState { return s }

func (o *Ownership) VisitBoolLit(_ ast.BoolLit, s *ownState) *ownState { return s }

func (o *Ownership) VisitSelfExpr(e ast.SelfExpr, s *ownState) *ownState {
	return o.readRoot(o.fn.SelfLocal, e.Position(), s)
}

// VisitStrLit holds a first part that is a string while the second is evaluated: lowering joins the parts from the left,
// and joins the first with the second once both exist. Every later part is joined as soon as it is evaluated, so none of
// them is held, and a part that is not a string is turned into one as soon as it is evaluated.
func (o *Ownership) VisitStrLit(e ast.StrLit, s *ownState) *ownState {
	parts := e.Parts()
	held := len(o.projections)
	for i, part := range parts.All() {
		if !part.Expr.IsNil() {
			s = o.expr(part.Expr, s)
		}
		switch {
		case i == 0 && parts.Len() > 1:
			o.holdText(part.Expr, parts.At(1).Expr)
		case i == 1:
			o.release(held)
		}
	}
	return s
}

// holdText holds the first part of an interpolated string while the second is evaluated, when the first is a string
// and a second part runs after it.
func (o *Ownership) holdText(first, second ast.Expr) {
	if !first.IsNil() && !second.IsNil() && !runsNoCode(second) && ast.Content(o.ty(first)) == ast.StringTy {
		o.hold(first, projectPart, false)
	}
}

func (o *Ownership) VisitNameExpr(e ast.NameExpr, s *ownState) *ownState {
	name := o.typing.Name(e)
	switch name.Kind() {
	case ast.NameLocal:
		return o.readRoot(name.Local(o.fn), e.Position(), s)
	case ast.NameSelfField:
		if o.partials > 0 {
			if next, done := o.readPart(e.Expr(), s); done {
				return next
			}
		}
		return o.readRoot(o.fn.SelfLocal, e.Position(), s)
	case ast.NameConst:
		return s
	}
	panic(diag.Internal("ownership: unresolved name '%s'", e.Name()))
}

// VisitBinaryExpr holds a managed left operand, which only a string comparison has, while the right is evaluated:
// lowering compares the two strings once both are computed.
func (o *Ownership) VisitBinaryExpr(e ast.BinaryExpr, s *ownState) *ownState {
	if (e.Op() == "and" || e.Op() == "or") && o.ty(e.Expr()) == ast.Ty(ast.BoolTy) {
		// The right operand runs only when the left one does not decide.
		s = o.expr(e.L(), s)
		return o.join(s, o.expr(e.R(), s))
	}
	s = o.expr(e.L(), s)
	held := len(o.projections)
	o.holdAcross(e.L(), projectOperand, e.R())
	s = o.expr(e.R(), s)
	o.release(held)
	return o.failed(e.Expr(), s)
}

// failed is the failure of an operator or a cast that the range analysis could not rule out.
func (o *Ownership) failed(e ast.Expr, s *ownState) *ownState {
	if o.typing.Fails(e) != 0 {
		o.failure(e.Position(), s)
	}
	return s
}

func (o *Ownership) VisitUnaryExpr(e ast.UnaryExpr, s *ownState) *ownState {
	return o.failed(e.Expr(), o.expr(e.E(), s))
}

// VisitCondExpr moves from each branch alone: the value it gives is a fresh one, so a place in a branch is moved.
func (o *Ownership) VisitCondExpr(e ast.CondExpr, s *ownState) *ownState {
	s = o.expr(e.Cond(), s)
	dest := ownDest{kind: destResult}
	mark := len(o.inFlight)
	then := o.consume(e.Then(), dest, s)
	o.land(mark)
	return o.join(then, o.consume(e.Else(), dest, s))
}

func (o *Ownership) VisitCallExpr(e ast.CallExpr, s *ownState) *ownState {
	call := o.typing.Call(e)
	receiver := e.ReceiverFor(call.Kind)
	switch call.Kind {
	case ast.CallCopy:
		return o.expr(receiver, s)
	case ast.CallPrint, ast.CallPrintError:
		return o.expr(call.Ordered[0], s)
	case ast.CallUnionCtor:
		payload := call.Union.Payload(int(call.CaseIndex))
		for i, arg := range call.Ordered {
			s = o.consume(arg, ownDest{kind: destField, name: payload[i].Name}, s)
		}
		return s
	case ast.CallFunction, ast.CallStatic, ast.CallMethod, ast.CallBound:
		return o.userCall(e.Position(), call, receiver, s)
	}
	panic(diag.Internal("ownership: unresolved call"))
}

func (o *Ownership) userCall(site ast.Pos, e *ast.CallInfo, receiver ast.Expr, s *ownState) *ownState {
	exclusive := !receiver.IsNil() && e.Pass.SelfConv != ast.ConvLet
	for _, spec := range e.Pass.Specs {
		exclusive = exclusive || spec.Conv != ast.ConvLet
	}
	if exclusive {
		o.requireExclusive(e, receiver)
	}
	held := len(o.projections)
	arguments := len(o.inFlight)
	outer := o.initialising
	o.initialising = nil
	if !receiver.IsNil() {
		s = o.argument(receiver, e.Pass.SelfConv, ownDest{kind: destParameter, name: "self"}, s)
		if len(e.Ordered) > 0 {
			o.hold(receiver, projectArgument, e.Pass.SelfConv.ByAddress())
		}
	}
	for i, arg := range e.Ordered {
		spec := e.Pass.Specs[i]
		if spec.Conv == ast.ConvMutates {
			o.requireNotReceiver(arg, spec.Name)
		}
		s = o.argument(arg, spec.Conv, ownDest{kind: destParameter, name: spec.Name}, s)
		if i+1 < len(e.Ordered) {
			o.hold(arg, projectArgument, spec.Conv.ByAddress())
		}
	}
	o.release(held)
	if e.ErrorType() != nil {
		// The callee has its arguments by now, and ends them itself when it fails.
		o.land(arguments)
		o.failure(site, s)
	}
	for _, l := range o.initialising {
		s = s.without(l)
	}
	o.initialising = outer
	return s
}

// requireNotReceiver refuses 'self' as a mutates argument. A mutates parameter may be assigned as a whole, which replaces
// the caller's value, and 'self' is the receiver of the call that is running, not a place that can be replaced.
func (o *Ownership) requireNotReceiver(arg ast.Expr, param string) {
	if _, ok := arg.AsSelfExpr(); ok && o.fn.SelfLocal.Mutable() {
		o.fail(arg.Position(), diag.ErrAssignToImmutable,
			"'self' cannot be passed to the 'mutates' parameter '%s', which could replace the whole value; pass a field of it, or call a method on it", param)
	}
}

// hold keeps the place that an expression has evaluated to a borrow borrowed until release, for an expression that uses
// the borrow again after evaluating more: a call nested in the rest that changes an overlapping place would otherwise
// leave the borrow pointing at what moved. What lowering keeps across that evaluation is a handle for a managed value,
// which points into the place, and the place's address when it is passed by address. A scalar that is only read is a
// value by then and needs no hold, so a correct program pays a push for each borrow, and no look at its path: that
// is found only when something in the rest changes or moves a place.
func (o *Ownership) hold(place ast.Expr, kind ownProjectionKind, byAddress bool) {
	if t := o.ty(place); byAddress || t.IsManaged() {
		o.projections = append(o.projections, ownProjection{place: place, kind: kind, ty: t})
	}
}

// holdAcross is hold for a borrow that an expression uses again after evaluating next. Most operands are a name or a
// literal, which run no code, so a correct program rarely pays for more than the kind of next.
func (o *Ownership) holdAcross(place ast.Expr, kind ownProjectionKind, next ast.Expr) {
	if !runsNoCode(next) {
		o.hold(place, kind, false)
	}
}

// runsNoCode is whether evaluating e is the read of a name or a literal, which cannot change a place. It does not look
// into anything larger: holding is always right, and the walk costs more than the hold it would save.
func runsNoCode(e ast.Expr) bool {
	switch e.Kind() {
	case ast.KindIntLit, ast.KindBoolLit, ast.KindNameExpr, ast.KindSelfExpr:
		return true
	}
	return false
}

// release ends the holds taken since projections had mark entries.
func (o *Ownership) release(mark int) { o.projections = o.projections[:mark] }

// argument passes an expression as the convention says: a let borrows it, mutates borrows it to change, consumes
// takes it, and sets initialises it.
func (o *Ownership) argument(e ast.Expr, conv ast.Convention, dest ownDest, s *ownState) *ownState {
	switch conv {
	case ast.ConvLet:
		return o.expr(e, s)
	case ast.ConvMutates:
		s = o.eval(e, s)
		p, ok := o.pathOf(e, o.steps[:0])
		if !ok {
			o.fail(e.Position(), diag.ErrAssignToImmutable,
				"the 'mutates' parameter '%s' changes its argument, so pass a variable, or a field or an element of one", dest.name)
			return s
		}
		o.requireWritable(e.Position(), p)
		return s
	case ast.ConvConsumes:
		return o.consume(e, dest, s)
	case ast.ConvDestructs:
		return o.consume(e, ownDest{kind: destDestructor, name: dest.name}, s)
	case ast.ConvSets:
		return o.initialise(e, dest, s)
	}
	panic(diag.Internal("ownership: unknown convention %d", int(conv)))
}

// initialise is an argument for a sets parameter: a variable that has no value yet, which has one afterwards.
func (o *Ownership) initialise(e ast.Expr, dest ownDest, s *ownState) *ownState {
	n, ok := e.AsNameExpr()
	var name ast.NameInfo
	if ok {
		name = o.typing.Name(n)
	}
	var local *ast.Local
	if ok && name.Kind() == ast.NameLocal {
		local = name.Local(o.fn)
	}
	if local == nil || !local.Mutable() {
		o.fail(e.Position(), diag.ErrSetArgument,
			"the 'sets' parameter '%s' initialises its argument, so pass a variable declared without a value: 'var x as T'", dest.name)
		return s
	}
	if s.dead {
		return s
	}
	if fact, found := s.tryGet(local); !found || fact.kind != factUninitialised {
		o.fail(e.Position(), diag.ErrSetArgument,
			"'%s' already has a value, but the 'sets' parameter '%s' initialises its argument; declare a variable without a value: 'var x as T'",
			n.Name(), dest.name)
		return s
	}
	o.initialising = append(o.initialising, local)
	return s
}

// requireExclusive is the law of exclusivity for one call: an argument passed mutates, consumes or sets must not overlap
// any other argument, receiver included, because the callee would then reach one place through two names.
func (o *Ownership) requireExclusive(call *ast.CallInfo, receiver ast.Expr) {
	o.accesses = o.accesses[:0]
	if !receiver.IsNil() {
		o.addAccess(receiver, call.Pass.SelfConv)
	}
	for i, arg := range call.Ordered {
		o.addAccess(arg, call.Pass.Specs[i].Conv)
	}
	for j := 1; j < len(o.accesses); j++ {
		for i := range j {
			a, b := o.accesses[i], o.accesses[j]
			if (a.conv == ast.ConvLet && b.conv == ast.ConvLet) || !a.path.overlaps(b.path) {
				continue
			}
			o.fail(b.expr.Position(), diag.ErrExclusivity, "%s", o.overlapMessage(a, b))
		}
	}
}

func (o *Ownership) addAccess(e ast.Expr, conv ast.Convention) {
	if p, ok := o.pathOf(e, nil); ok {
		o.accesses = append(o.accesses, callAccess{expr: e, path: p, conv: conv})
	}
}

func (o *Ownership) overlapMessage(a, b callAccess) string {
	changing, other := a, b
	if a.conv == ast.ConvLet {
		changing, other = b, a
	}
	message := fmt.Sprintf("'%s' and '%s' overlap, and '%s' is passed %s, so the callee would reach one place through two names",
		a.path.text(), b.path.text(), changing.path.text(), changing.conv)
	if other.conv == ast.ConvLet {
		return fmt.Sprintf("%s; %s", message, o.checker.copyAdvice(o.ty(other.expr), fmt.Sprintf("pass a copy of the borrowed one: '%s.copy()'", other.path.text()), "pass separate values"))
	}
	return message + "; pass separate values"
}

func (o *Ownership) VisitIndexExpr(e ast.IndexExpr, s *ownState) *ownState { return o.subscript(e, s) }

// subscript evaluates the collection of a subscript and its index, which lowering takes the element address from only
// after both: the collection stays borrowed while the index is computed.
func (o *Ownership) subscript(e ast.IndexExpr, s *ownState) *ownState {
	s = o.expr(e.Obj(), s)
	held := len(o.projections)
	o.holdAcross(e.Obj(), projectCollection, e.Args().At(0))
	s = o.expr(e.Args().At(0), s)
	o.release(held)
	return s
}

func (o *Ownership) VisitMemberExpr(e ast.MemberExpr, s *ownState) *ownState {
	kind := o.typing.Member(e).Kind
	if kind == ast.MemberEnumCase || kind == ast.MemberUnionCase {
		return s
	}
	if o.partials > 0 && kind == ast.MemberField {
		if next, done := o.readPart(e.Expr(), s); done {
			return next
		}
	}
	return o.expr(e.Obj(), s)
}

func (o *Ownership) VisitStructLitExpr(e ast.StructLitExpr, s *ownState) *ownState {
	lit := o.typing.StructLit(e)
	for i, v := range lit.Ordered {
		if !v.IsNil() {
			s = o.consume(v, ownDest{kind: destField, name: lit.Record.FieldList()[i].Name}, s)
		}
	}
	return s
}

func (o *Ownership) VisitCastExpr(e ast.CastExpr, s *ownState) *ownState {
	return o.failed(e.Expr(), o.expr(e.E(), s))
}

func (o *Ownership) VisitMatchExpr(e ast.MatchExpr, s *ownState) *ownState {
	return o.match(e.Match(), s)
}

func (o *Ownership) VisitPanicExpr(e ast.PanicExpr, s *ownState) *ownState {
	return o.expr(e.Message().Expr(), s)
}

// valueContext stands for the function around an expression that is not in one: it has no receiver and no locals.
var valueContext = &ast.FuncDecl{}

// CheckValues checks the expressions a declaration holds outside any function body: a constant's value and the default
// of a field. Whatever holds the value owns it, so each is consumed, and a fresh value in one needs no drop.
func (o *Ownership) CheckValues(d ast.Decl) { ast.VisitDecl(declValues{o}, d) }

type declValues struct{ o *Ownership }

func (v declValues) VisitConstDecl(d *ast.ConstDecl) { v.o.checkValue(d.Value, d.Name) }

func (v declValues) VisitTypeDecl(d *ast.TypeDecl) {
	for _, f := range d.Fields {
		if !f.Default.IsNil() {
			v.o.checkValue(f.Default, f.Name)
		}
	}
}

func (declValues) VisitFuncDecl(*ast.FuncDecl) {}

func (declValues) VisitEnumDecl(*ast.EnumDecl) {}

func (declValues) VisitUnionDecl(*ast.UnionDecl) {}

func (declValues) VisitAliasDecl(*ast.AliasDecl) {}

func (declValues) VisitInterfaceDecl(*ast.InterfaceDecl) {}

func (o *Ownership) checkValue(e ast.Expr, name string) {
	o.fn = valueContext
	o.frames = o.frames[:0]
	o.projections = o.projections[:0]
	o.setParams = o.setParams[:0]
	o.obliged = o.obliged[:0]
	o.partials = 0
	o.catches = o.catches[:0]
	o.inFlight = o.inFlight[:0]
	o.consume(e, ownDest{kind: destField, name: name}, ownClean)
}
