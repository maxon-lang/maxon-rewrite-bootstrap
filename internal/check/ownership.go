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
	site ast.Node
	kind ownFactKind
	dest ownDest
	// parts is set for a partly moved local only.
	parts *ownParts
}

// ownDropKind is how a value that nothing moved comes to be dropped, for the message of E3136.
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
	}
	panic(diag.Internal("ownership: unknown drop %d", int(d.kind)))
}

// ownObliged is a local that owns a value which cannot be dropped, so it must be moved or ended before its scope
// does. site is where it is declared.
type ownObliged struct {
	local *ast.Local
	site  ast.Node
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
	target    ast.Node
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
)

// ownProjection is a place that stays borrowed while code runs which must not change it: a match subject whose
// payload bindings borrow from it while an arm runs, an argument of a call while the arguments after it are
// evaluated, or the target of an assignment while its value is computed. The last two are borrows taken before the
// code runs, which lasts through the call or the store, so what the code changes would leave them pointing at what
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
}

const ownStepCapacity = 8

func NewOwnership(errs *[]*diag.CompileError, checker *Checker) *Ownership {
	return &Ownership{errors: errs, checker: checker, steps: make([]pathStep, 0, ownStepCapacity)}
}

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
	s := ownClean
	switch {
	case f.IsDestructor():
		o.obliged = append(o.obliged, ownObliged{local: f.SelfLocal, site: f, destroys: true})
	case f.IsMethod() && f.SelfLocal.Conv == ast.ConvConsumes:
		o.oblige(f.SelfLocal, f)
	}
	for i, prm := range f.Params {
		switch ast.ConventionOf(prm.Convention) {
		case ast.ConvSets:
			o.setParams = append(o.setParams, prm.Sym)
			s = s.with(prm.Sym, ownFact{site: prm, kind: factUninitialised})
		case ast.ConvConsumes:
			if prm.Sym == nil {
				o.requireDroppable(prm, f.ParamTypes[i], ownDrop{kind: dropParameter, name: prm.Name})
			} else {
				o.oblige(prm.Sym, prm)
			}
		}
	}
	s = o.walk(f.Body, s)
	o.endScope(0, s)
	o.requireSetsInitialised(s, f)
}

// oblige records a local that owns a value of a type that cannot be dropped.
func (o *Ownership) oblige(l *ast.Local, site ast.Node) {
	if l.Type.IsManaged() && o.checker.lacks(droppable, l.Type) != nil {
		o.obliged = append(o.obliged, ownObliged{local: l, site: site})
	}
}

// requireDroppable is E3136: a value of type t is dropped implicitly, which only a type with Droppable allows.
func (o *Ownership) requireDroppable(site ast.Node, t ast.Ty, d ownDrop) {
	if !t.IsManaged() {
		return
	}
	if why, fix, lacking := o.checker.whyNot(droppable, t); lacking {
		o.fail(site, diag.ErrNotDroppable, "%s, but '%s' cannot be dropped (%s); %s", d, t.Name(), why, fix)
	}
}

// requireTemporaryDroppable is the drop of a fresh value that the statement does not give to anything.
func (o *Ownership) requireTemporaryDroppable(e ast.Expr, s *ownState, kind ownDropKind) {
	if !s.dead && e.Ty().IsManaged() && !ast.IsStorage(e) {
		o.requireDroppable(e, e.Ty(), ownDrop{kind: kind})
	}
}

// requireEnded checks the obliged locals from index from on at a place where they are dropped: each must hold nothing.
// The scope's end reports at the declaration, an exit at its own statement.
func (o *Ownership) requireEnded(from int, s *ownState, site ast.Node, kind ownDropKind) {
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
	o.requireEnded(mark, s, nil, dropScopeEnd)
	s = o.strip(mark, s)
	o.obliged = o.obliged[:mark]
	return s
}

// leave is a jump out of the scopes above from: their obliged locals must hold nothing, and their facts do not travel
// on to where the jump lands.
func (o *Ownership) leave(from int, s *ownState, site ast.Node, kind ownDropKind) *ownState {
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

// disagreeWhole is E3136 for a local that holds nothing on one path only; fact is that path's.
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
		if e.Code == err.Code && e.File == err.File && e.Line == err.Line && e.Col == err.Col {
			return
		}
	}
	*o.errors = append(*o.errors, err)
}

func (o *Ownership) fail(site ast.Node, code, format string, args ...any) {
	o.report(site.P().Error(code, fmt.Sprintf(format, args...)))
}

// pushFrame opens a frame for a loop or match, reusing the frames of earlier ones: a function with loops would
// otherwise allocate one for each.
func (o *Ownership) pushFrame(target ast.Node) *ownFrame {
	depth := len(o.frames)
	if depth == len(o.framePool) {
		o.framePool = append(o.framePool, &ownFrame{})
	}
	frame := o.framePool[depth]
	*frame = ownFrame{target: target, breaks: ownUnreachable, continues: ownUnreachable, obliged: len(o.obliged)}
	o.frames = append(o.frames, frame)
	return frame
}

func (o *Ownership) frameOf(target ast.Node) *ownFrame {
	for _, v := range slices.Backward(o.frames) {
		if v.target == target {
			return v
		}
	}
	panic(diag.Internal("ownership: a jump has no enclosing frame"))
}

// walk checks a block, which is a scope: the locals it declares end with it.
func (o *Ownership) walk(body []ast.Stmt, s *ownState) *ownState {
	mark := len(o.obliged)
	for _, st := range body {
		s = o.stmt(st, s)
	}
	return o.endScope(mark, s)
}

func (o *Ownership) stmt(st ast.Stmt, s *ownState) *ownState {
	return ast.VisitStmtState[*ownState, *ownState](o, st, s)
}

func (o *Ownership) VisitLetStmt(st *ast.LetStmt, s *ownState) *ownState {
	if st.Init == nil {
		o.oblige(st.Sym, st)
		return s.with(st.Sym, ownFact{site: st, kind: factUninitialised})
	}
	s = o.consume(st.Init, ownDest{kind: destVariable, name: st.Name}, s)
	o.oblige(st.Sym, st)
	return s.without(st.Sym)
}

func (o *Ownership) VisitAssignStmt(st *ast.AssignStmt, s *ownState) *ownState {
	switch target := st.Target.(type) {
	case *ast.NameExpr:
		switch target.Kind {
		case ast.NameLocal:
			s = o.consume(st.Value, ownDest{kind: destVariable, name: target.Name}, s)
			o.requireWritable(st, accessPath{root: target.Local})
			if !s.dead {
				o.requireHeld(st, ownObliged{local: target.Local}, s, dropOverwrite)
			}
			return s.without(target.Local)
		case ast.NameSelfField:
			s = o.consumeInto(target, st.Value, ownDest{kind: destField, name: target.Name}, s)
			p, _ := pathOf(target, o.fn.SelfLocal, o.steps[:0])
			o.requireWritable(st, p)
			if next, done := o.assignPart(st, target, p, s); done {
				return next
			}
			o.requireOverwriteDroppable(st, target, s)
			return o.readRoot(o.fn.SelfLocal, target, s)
		case ast.NameConst:
			s = o.consume(st.Value, ownDest{kind: destVariable, name: target.Name}, s)
			o.fail(st, diag.ErrAssignToImmutable, "'%s' is a constant and cannot be reassigned", target.Name)
			return s
		}
	case *ast.MemberExpr:
		s = o.consumeInto(target, st.Value, ownDest{kind: destField, name: target.Name}, s)
		p, ok := pathOf(target, o.fn.SelfLocal, o.steps[:0])
		if !ok {
			o.fail(st, diag.ErrAssignToImmutable, "the field '%s' belongs to a temporary value, so assigning it changes nothing; assign it on a variable", target.Name)
			return s
		}
		o.requireWritable(st, p)
		if next, done := o.assignPart(st, target, p, s); done {
			return next
		}
		o.requireOverwriteDroppable(st, target, s)
		return o.eval(target.Obj, s)
	case *ast.IndexExpr:
		s = o.consumeInto(target, st.Value, ownDest{kind: destElement, name: placeText(target.Obj)}, s)
		p, ok := pathOf(target, o.fn.SelfLocal, o.steps[:0])
		if !ok {
			o.fail(st, diag.ErrAssignToImmutable, "the element belongs to a temporary collection, so assigning it changes nothing; assign it on a variable")
			return s
		}
		o.requireWritable(st, p)
		o.requireOverwriteDroppable(st, target, s)
		return o.expr(target.Args[0], o.eval(target.Obj, s))
	}
	panic(diag.Internal("ownership: unhandled assignment target"))
}

// requireOverwriteDroppable is the drop of the value a field or an element holds when an assignment replaces it.
func (o *Ownership) requireOverwriteDroppable(st *ast.AssignStmt, target ast.Expr, s *ownState) {
	if !s.dead {
		o.requireDroppable(st, target.Ty(), ownDrop{kind: dropOverwrite, name: placeText(target)})
	}
}

// consumeInto is the value stored into a part of a variable. The part's address is taken before the value is computed,
// so the target stays borrowed while it is: a call in the value that changes the variable could otherwise make the
// store land where the part used to be.
func (o *Ownership) consumeInto(target ast.Expr, value ast.Expr, dest ownDest, s *ownState) *ownState {
	held := len(o.projections)
	o.projections = append(o.projections, ownProjection{place: target, kind: projectTarget, ty: target.Ty()})
	s = o.consume(value, dest, s)
	o.projections = o.projections[:held]
	return s
}

func (o *Ownership) VisitDiscardStmt(st *ast.DiscardStmt, s *ownState) *ownState {
	s = o.eval(st.Value, s)
	o.requireTemporaryDroppable(st.Value, s, dropDiscard)
	return s
}

func (o *Ownership) VisitExprStmt(st *ast.ExprStmt, s *ownState) *ownState {
	s = o.expr(st.E, s)
	if _, ok := st.E.(*ast.PanicExpr); ok {
		return ownUnreachable
	}
	return s
}

func (o *Ownership) VisitIfStmt(st *ast.IfStmt, s *ownState) *ownState {
	s = o.expr(st.Cond, s)
	a := o.walk(st.Then, s)
	b := s
	if st.HasElse {
		b = o.walk(st.Else, s)
	}
	return o.join(a, b)
}

func (o *Ownership) VisitWhileStmt(st *ast.WhileStmt, s *ownState) *ownState {
	return o.loop(st, s, st.Body, st.Cond)
}

func (o *Ownership) VisitForStmt(st *ast.ForStmt, s *ownState) *ownState {
	return o.loop(st, o.expr(st.To, o.expr(st.From, s)), st.Body, nil)
}

// VisitWhenStmt schedules a behaviour. The cowns it names are only read: the behaviour takes a count of its own on each
// when it is scheduled. Each variable its body uses moves into it, so the enclosing function cannot use it again, and
// the body is checked as a function of its own.
func (o *Ownership) VisitWhenStmt(st *ast.WhenStmt, s *ownState) *ownState {
	for _, cn := range st.Cowns {
		s = o.expr(cn, s)
	}
	for _, cp := range st.Captures {
		s = o.consume(cp.Ref, ownDest{kind: destBehaviour}, s)
	}
	NewOwnership(o.errors, o.checker).run(st.Fn)
	return s
}

func (o *Ownership) VisitMatchStmt(st *ast.MatchStmt, s *ownState) *ownState {
	return o.match(st.Match, s)
}

func (o *Ownership) VisitReturnStmt(st *ast.ReturnStmt, s *ownState) *ownState {
	if st.Value != nil {
		s = o.consume(st.Value, ownDest{kind: destReturn}, s)
	}
	o.requireSetsInitialised(s, st)
	o.requireEnded(0, s, st, dropReturn)
	return ownUnreachable
}

func (o *Ownership) VisitBreakStmt(st *ast.BreakStmt, s *ownState) *ownState {
	frame := o.frameOf(st.Target)
	frame.breaks = o.join(frame.breaks, o.leave(frame.obliged, s, st, dropBreak))
	return ownUnreachable
}

func (o *Ownership) VisitContinueStmt(st *ast.ContinueStmt, s *ownState) *ownState {
	frame := o.frameOf(st.Target)
	frame.continues = o.join(frame.continues, o.leave(frame.obliged, s, st, dropContinue))
	return ownUnreachable
}

// loop walks the body again from what the first walk added to the state, which is enough for a fact to reach the
// top of the next iteration; a first walk that adds nothing needs no second.
func (o *Ownership) loop(loop ast.Node, s *ownState, body []ast.Stmt, cond ast.Expr) *ownState {
	// A loop whose condition is the literal true is left by 'break' alone, so no path leaves it from the top or the
	// bottom of the body.
	frame := o.pushFrame(loop)
	entry := s
	if cond != nil {
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
		if cond != nil {
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
	b, ok := e.(*ast.BoolLit)
	return ok && b.Value
}

func (o *Ownership) match(m *ast.MatchCore, s *ownState) *ownState {
	if m.Consumes {
		o.requireMayTakeApart(m.Subject, ast.Content(m.Subject.Ty()), "a 'match consumes'")
		s = o.consume(m.Subject, ownDest{kind: destMatch}, s)
	} else {
		s = o.expr(m.Subject, s)
	}
	frame := o.pushFrame(m)
	subject, subjectIsPath, subjectKnown := accessPath{}, false, false
	result := ownUnreachable
	for _, arm := range m.Arms {
		a := s
		mark := len(o.obliged)
		if m.Consumes {
			o.takeApart(m, arm)
		}
		borrows := armBorrows(arm)
		if borrows && !subjectKnown {
			subject, subjectIsPath = pathOf(m.Subject, o.fn.SelfLocal, nil)
			subjectKnown = true
		}
		if borrows && subjectIsPath {
			o.projections = append(o.projections, ownProjection{subject: subject, kind: projectMatch, ty: m.Subject.Ty()})
		}
		switch arm.Kind() {
		case ast.ArmThen:
			a = o.stmt(arm.Action, a)
		case ast.ArmGives:
			a = o.consume(arm.Value, ownDest{kind: destResult}, a)
		case ast.ArmPanic:
			o.expr(arm.Value, a)
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
func armBorrows(arm *ast.MatchArm) bool {
	for _, pat := range arm.Patterns {
		for _, l := range pat.BindingLocals {
			if l != nil && l.Projected {
				return true
			}
		}
	}
	return false
}

// takeApart is the start of an arm of a match that consumes its subject: each payload the arm names is an owned local of
// the arm, and the payloads it does not name are dropped with the subject.
func (o *Ownership) takeApart(m *ast.MatchCore, arm *ast.MatchArm) {
	u, ok := ast.Content(m.Subject.Ty()).(*ast.UnionTy)
	if !ok {
		return
	}
	for _, pat := range arm.Patterns {
		if pat.Kind() != ast.PatternCase {
			continue
		}
		for i, p := range u.Payload(pat.CaseIndex) {
			if i < len(pat.BindingLocals) && pat.BindingLocals[i] != nil {
				o.oblige(pat.BindingLocals[i], pat)
			} else {
				o.requireDroppable(pat, p.Type, ownDrop{kind: dropPayload, name: p.Name})
			}
		}
	}
	if arm.IsDefault {
		o.requireUnnamedCasesDroppable(m, arm, u)
	}
}

// requireUnnamedCasesDroppable is the drop of what a default arm leaves in the subject: the payloads of the cases that
// no arm names.
func (o *Ownership) requireUnnamedCasesDroppable(m *ast.MatchCore, arm *ast.MatchArm, u *ast.UnionTy) {
	named := make([]bool, len(u.CaseList()))
	for _, other := range m.Arms {
		for _, pat := range other.Patterns {
			if pat.Kind() == ast.PatternCase {
				named[pat.CaseIndex] = true
			}
		}
	}
	for i, isNamed := range named {
		if isNamed {
			continue
		}
		for _, p := range u.Payload(i) {
			o.requireDroppable(arm, p.Type, ownDrop{kind: dropPayload, name: p.Name})
		}
	}
}

// readRoot is a use of a local's value, which needs one to exist.
func (o *Ownership) readRoot(l *ast.Local, site ast.Node, s *ownState) *ownState {
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
			l.Name, fact.dest, fact.site.P().Line, o.checker.copyAdvice(l.Type, fmt.Sprintf("move a copy instead: '%s.copy()'", l.Name), "assign it again before reading it"))
	case factUninitialised:
		o.fail(site, diag.ErrUseBeforeInit,
			"'%s' has no value here; assign it on every path before reading it", l.Name)
	case factPartlyMoved:
		m := fact.parts.moves[0]
		o.fail(site, diag.ErrUseAfterMove,
			"'%s' lost '%s', which was moved into %s at line %d, so it cannot be used as a whole; use its other fields, or assign '%s' again first",
			l.Name, m.text(l), m.dest, m.site.P().Line, m.text(l))
		return s
	}
	return s.without(l)
}

// consume is an expression whose value is stored, passed to a consumes parameter or returned. Code that can run marks the expression
// consumed, which is how lowering learns where a value changes owner. A scalar is copied, and a fresh value needs
// nothing. A place holding a managed value is moved out of, which only a whole local that owns its value allows;
// everything else is refused and says to copy.
func (o *Ownership) consume(e ast.Expr, dest ownDest, s *ownState) *ownState {
	s = o.eval(e, s)
	if s.dead {
		return s
	}
	e.MarkConsumed()
	if !e.Ty().IsManaged() {
		return s
	}
	p, ok := pathOf(e, o.fn.SelfLocal, o.steps[:0])
	if !ok {
		switch {
		case projectsElement(e):
			o.fail(e, diag.ErrMoveOutOfField,
				"an element of a collection that is not stored in a variable cannot be moved out of it into %s; bind the collection to a variable and take the element out of it, or copy it",
				dest)
		case ast.IsStorage(e):
			o.fail(e, diag.ErrMoveOutOfField,
				"a part of a temporary value cannot be moved out of it into %s, because the temporary is dropped when the statement ends; %s",
				dest, o.checker.copyAdvice(e.Ty(), "copy it with '.copy()'", ""))
		}
		return s
	}

	switch {
	case p.crossesFrozen():
		o.fail(e, diag.ErrMoveOutOfField,
			"'%s' is part of a frozen value, which keeps its contents and shares them with its other owners, so it cannot be moved out of it into %s; %s",
			p.text(), dest, o.checker.copyAdvice(e.Ty(), fmt.Sprintf("use '%s.copy()'", p.text()), ""))
		return s
	case p.root.Borrowed():
		o.fail(e, diag.ErrMoveOutOfBorrow,
			"'%s' is borrowed, not owned, so it cannot be moved into %s; %s",
			p.text(), dest, o.checker.copyAdvice(e.Ty(), fmt.Sprintf("use '%s.copy()' to give it its own copy", p.text()), ""))
		return s
	case p.crossesElement():
		o.fail(e, diag.ErrMoveOutOfField,
			"'%s' is, or lies inside, an element of a collection, which owns it, so it cannot be moved out of it into %s; %s",
			p.text(), dest, o.checker.copyAdvice(e.Ty(), fmt.Sprintf("use '%s.copy()'", p.text()), "take it out of the collection with its remove method"))
		return s
	case len(p.steps) > 0:
		return o.movePart(e, p, dest, s)
	}
	if p.root.IsSelf && o.fn.IsDestructor() && dest.kind != destDestructor && dest.kind != destMatch {
		o.fail(e, diag.ErrDestructorMisuse,
			"'self' is moved into %s, but inside a destructor it may only be passed whole to another destructor of its type, which ends it there; move its fields out instead, or call a destructor",
			dest)
		return s
	}
	o.requireNotProjected(p, e)
	return s.with(p.root, ownFact{site: e, kind: factMoved, dest: dest})
}

// requireWritable refuses a change to a place that is not mutable, and one that a match binding still borrows.
func (o *Ownership) requireWritable(site ast.Node, p accessPath) {
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

// requireNotProjected refuses a change or a move of a place while a binding of an enclosing match, an argument of an
// enclosing call, or the target of an enclosing assignment still borrows part of it.
func (o *Ownership) requireNotProjected(p accessPath, site ast.Node) {
	for i := range o.projections {
		pr := &o.projections[i]
		if pr.place != nil {
			pr.subject, _ = pathOf(pr.place, o.fn.SelfLocal, nil)
			pr.place = nil
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
		default:
			panic(diag.Internal("ownership: unknown projection kind %d", int(pr.kind)))
		}
		return
	}
}

// requireSetsInitialised is checked wherever the function returns: each sets parameter holds a value by then.
func (o *Ownership) requireSetsInitialised(s *ownState, site ast.Node) {
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
	return ast.VisitExprState[*ownState, *ownState](o, e, s)
}

func (o *Ownership) VisitIntLit(_ *ast.IntLit, s *ownState) *ownState { return s }

func (o *Ownership) VisitBoolLit(_ *ast.BoolLit, s *ownState) *ownState { return s }

func (o *Ownership) VisitSelfExpr(e *ast.SelfExpr, s *ownState) *ownState {
	return o.readRoot(e.Local, e, s)
}

func (o *Ownership) VisitStrLit(e *ast.StrLit, s *ownState) *ownState {
	for _, part := range e.Parts {
		if pe, ok := part.(ast.Expr); ok {
			s = o.expr(pe, s)
		}
	}
	return s
}

func (o *Ownership) VisitNameExpr(e *ast.NameExpr, s *ownState) *ownState {
	switch e.Kind {
	case ast.NameLocal:
		return o.readRoot(e.Local, e, s)
	case ast.NameSelfField:
		if o.partials > 0 {
			if next, done := o.readPart(e, s); done {
				return next
			}
		}
		return o.readRoot(o.fn.SelfLocal, e, s)
	case ast.NameConst:
		return s
	}
	panic(diag.Internal("ownership: unresolved name '%s'", e.Name))
}

func (o *Ownership) VisitBinaryExpr(e *ast.BinaryExpr, s *ownState) *ownState {
	return o.expr(e.R, o.expr(e.L, s))
}

func (o *Ownership) VisitUnaryExpr(e *ast.UnaryExpr, s *ownState) *ownState {
	return o.expr(e.E, s)
}

// VisitCondExpr moves from each branch alone: the value it gives is a fresh one, so a place in a branch is moved.
func (o *Ownership) VisitCondExpr(e *ast.CondExpr, s *ownState) *ownState {
	s = o.expr(e.Cond, s)
	dest := ownDest{kind: destResult}
	return o.join(o.consume(e.Then, dest, s), o.consume(e.Else, dest, s))
}

func (o *Ownership) VisitCallExpr(e *ast.CallExpr, s *ownState) *ownState {
	switch e.Kind {
	case ast.CallCopy:
		return o.expr(e.Receiver, s)
	case ast.CallPrint, ast.CallPrintError:
		return o.expr(e.Ordered[0], s)
	case ast.CallUnionCtor:
		payload := e.Union.Payload(e.CaseIndex)
		for i, arg := range e.Ordered {
			s = o.consume(arg, ownDest{kind: destField, name: payload[i].Name}, s)
		}
		return s
	case ast.CallFunction, ast.CallStatic, ast.CallMethod, ast.CallBound:
		return o.userCall(e, s)
	}
	panic(diag.Internal("ownership: unresolved call"))
}

func (o *Ownership) userCall(e *ast.CallExpr, s *ownState) *ownState {
	exclusive := e.Receiver != nil && e.Pass.SelfConv != ast.ConvLet
	for _, spec := range e.Pass.Specs {
		exclusive = exclusive || spec.Conv != ast.ConvLet
	}
	if exclusive {
		o.requireExclusive(e)
	}
	held := len(o.projections)
	outer := o.initialising
	o.initialising = nil
	if e.Receiver != nil {
		s = o.argument(e.Receiver, e.Pass.SelfConv, ownDest{kind: destParameter, name: "self"}, s)
		o.hold(e.Receiver, e.Pass.SelfConv, len(e.Ordered) > 0)
	}
	for i, arg := range e.Ordered {
		spec := e.Pass.Specs[i]
		if spec.Conv == ast.ConvMutates {
			o.requireNotReceiver(arg, spec.Name)
		}
		s = o.argument(arg, spec.Conv, ownDest{kind: destParameter, name: spec.Name}, s)
		o.hold(arg, spec.Conv, i+1 < len(e.Ordered))
	}
	o.projections = o.projections[:held]
	for _, l := range o.initialising {
		s = s.without(l)
	}
	o.initialising = outer
	return s
}

// requireNotReceiver refuses 'self' as a mutates argument. A mutates parameter may be assigned as a whole, which replaces
// the caller's value, and 'self' is the receiver of the call that is running, not a place that can be replaced.
func (o *Ownership) requireNotReceiver(arg ast.Expr, param string) {
	if self, ok := arg.(*ast.SelfExpr); ok && self.Local.Mutable() {
		o.fail(arg, diag.ErrAssignToImmutable,
			"'self' cannot be passed to the 'mutates' parameter '%s', which could replace the whole value; pass a field of it, or call a method on it", param)
	}
}

// hold keeps an argument borrowed while the arguments after it are evaluated. A call nested in one of them that
// changes an overlapping place would otherwise leave the borrow pointing at what moved: a borrow of a collection's
// element is taken when its argument is evaluated, and a later argument may remove that element. A scalar read by a
// let parameter is a value by then and needs no hold.
func (o *Ownership) hold(arg ast.Expr, conv ast.Convention, laterArguments bool) {
	if !laterArguments || (conv == ast.ConvLet && !arg.Ty().IsManaged()) {
		return
	}
	o.projections = append(o.projections, ownProjection{place: arg, kind: projectArgument, ty: arg.Ty()})
}

// argument passes an expression as the convention says: a let borrows it, mutates borrows it to change, consumes
// takes it, and sets initialises it.
func (o *Ownership) argument(e ast.Expr, conv ast.Convention, dest ownDest, s *ownState) *ownState {
	switch conv {
	case ast.ConvLet:
		return o.expr(e, s)
	case ast.ConvMutates:
		s = o.eval(e, s)
		p, ok := pathOf(e, o.fn.SelfLocal, o.steps[:0])
		if !ok {
			o.fail(e, diag.ErrAssignToImmutable,
				"the 'mutates' parameter '%s' changes its argument, so pass a variable, or a field or an element of one", dest.name)
			return s
		}
		o.requireWritable(e, p)
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
	n, ok := e.(*ast.NameExpr)
	if !ok || n.Kind != ast.NameLocal || !n.Local.Mutable() {
		o.fail(e, diag.ErrSetArgument,
			"the 'sets' parameter '%s' initialises its argument, so pass a variable declared without a value: 'var x as T'", dest.name)
		return s
	}
	if s.dead {
		return s
	}
	if fact, found := s.tryGet(n.Local); !found || fact.kind != factUninitialised {
		o.fail(e, diag.ErrSetArgument,
			"'%s' already has a value, but the 'sets' parameter '%s' initialises its argument; declare a variable without a value: 'var x as T'",
			n.Name, dest.name)
		return s
	}
	o.initialising = append(o.initialising, n.Local)
	return s
}

// requireExclusive is the law of exclusivity for one call: an argument passed mutates, consumes or sets must not overlap
// any other argument, receiver included, because the callee would then reach one place through two names.
func (o *Ownership) requireExclusive(call *ast.CallExpr) {
	o.accesses = o.accesses[:0]
	if call.Receiver != nil {
		o.addAccess(call.Receiver, call.Pass.SelfConv)
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
			o.fail(b.expr, diag.ErrExclusivity, "%s", o.overlapMessage(a, b))
		}
	}
}

func (o *Ownership) addAccess(e ast.Expr, conv ast.Convention) {
	if p, ok := pathOf(e, o.fn.SelfLocal, nil); ok {
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
		return fmt.Sprintf("%s; %s", message, o.checker.copyAdvice(other.expr.Ty(), fmt.Sprintf("pass a copy of the borrowed one: '%s.copy()'", other.path.text()), "pass separate values"))
	}
	return message + "; pass separate values"
}

func (o *Ownership) VisitIndexExpr(e *ast.IndexExpr, s *ownState) *ownState {
	return o.expr(e.Args[0], o.expr(e.Obj, s))
}

func (o *Ownership) VisitMemberExpr(e *ast.MemberExpr, s *ownState) *ownState {
	if e.Kind == ast.MemberEnumCase || e.Kind == ast.MemberUnionCase {
		return s
	}
	if o.partials > 0 && e.Kind == ast.MemberField {
		if next, done := o.readPart(e, s); done {
			return next
		}
	}
	return o.expr(e.Obj, s)
}

func (o *Ownership) VisitStructLitExpr(e *ast.StructLitExpr, s *ownState) *ownState {
	for i, v := range e.Ordered {
		if v != nil {
			s = o.consume(v, ownDest{kind: destField, name: e.Record.FieldList()[i].Name}, s)
		}
	}
	return s
}

func (o *Ownership) VisitCastExpr(e *ast.CastExpr, s *ownState) *ownState {
	return o.expr(e.E, s)
}

func (o *Ownership) VisitMatchExpr(e *ast.MatchExpr, s *ownState) *ownState {
	return o.match(e.Match, s)
}

func (o *Ownership) VisitPanicExpr(e *ast.PanicExpr, s *ownState) *ownState {
	return o.expr(e.Message, s)
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
		if f.Default != nil {
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
	o.consume(e, ownDest{kind: destField, name: name}, ownClean)
}
