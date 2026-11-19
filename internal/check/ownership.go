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
	}
	panic(diag.Internal("ownership: unknown move destination %d", int(d.kind)))
}

// ownFact is why a local holds no value: a move out of it, or a declaration that left it uninitialised.
type ownFact struct {
	site ast.Node
	kind ownFactKind
	dest ownDest
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
		if _, ok := a.facts[k]; !ok {
			if m == nil {
				m = make(map[*ast.Local]ownFact, len(a.facts)+1)
				maps.Copy(m, a.facts)
			}
			m[k] = v
		}
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
	errors      *[]*diag.CompileError
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
}

const ownStepCapacity = 8

func NewOwnership(errs *[]*diag.CompileError) *Ownership {
	return &Ownership{errors: errs, steps: make([]pathStep, 0, ownStepCapacity)}
}

func (o *Ownership) Check(f *ast.FuncDecl) {
	o.fn = f
	o.frames = o.frames[:0]
	o.projections = o.projections[:0]
	o.setParams = o.setParams[:0]
	s := ownClean
	for _, prm := range f.Params {
		if prm.Sym != nil && prm.Sym.Conv == ast.ConvSets {
			o.setParams = append(o.setParams, prm.Sym)
			s = s.with(prm.Sym, ownFact{site: prm, kind: factUninitialised})
		}
	}
	s = o.walk(f.Body, s)
	o.requireSetsInitialised(s, f)
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
	*frame = ownFrame{target: target, breaks: ownUnreachable, continues: ownUnreachable}
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

func (o *Ownership) walk(body []ast.Stmt, s *ownState) *ownState {
	for _, st := range body {
		s = o.stmt(st, s)
	}
	return s
}

func (o *Ownership) stmt(st ast.Stmt, s *ownState) *ownState {
	return ast.VisitStmtState[*ownState, *ownState](o, st, s)
}

func (o *Ownership) VisitLetStmt(st *ast.LetStmt, s *ownState) *ownState {
	if st.Init == nil {
		return s.with(st.Sym, ownFact{site: st, kind: factUninitialised})
	}
	s = o.consume(st.Init, ownDest{kind: destVariable, name: st.Name}, s)
	return s.without(st.Sym)
}

func (o *Ownership) VisitAssignStmt(st *ast.AssignStmt, s *ownState) *ownState {
	switch target := st.Target.(type) {
	case *ast.NameExpr:
		switch target.Kind {
		case ast.NameLocal:
			s = o.consume(st.Value, ownDest{kind: destVariable, name: target.Name}, s)
			o.requireWritable(st, accessPath{root: target.Local})
			return s.without(target.Local)
		case ast.NameSelfField:
			s = o.consumeInto(target, st.Value, ownDest{kind: destField, name: target.Name}, s)
			p, _ := pathOf(target, o.fn.SelfLocal, o.steps[:0])
			o.requireWritable(st, p)
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
		return o.expr(target.Obj, s)
	case *ast.IndexExpr:
		s = o.consumeInto(target, st.Value, ownDest{kind: destElement, name: placeText(target.Obj)}, s)
		p, ok := pathOf(target, o.fn.SelfLocal, o.steps[:0])
		if !ok {
			o.fail(st, diag.ErrAssignToImmutable, "the element belongs to a temporary collection, so assigning it changes nothing; assign it on a variable")
			return s
		}
		o.requireWritable(st, p)
		return o.expr(target.Args[0], o.expr(target.Obj, s))
	}
	panic(diag.Internal("ownership: unhandled assignment target"))
}

// consumeInto is the value stored into a part of a variable. The part's address is taken before the value is computed,
// so the target stays borrowed while it is: a call in the value that changes the variable could otherwise make the
// store land where the part used to be.
func (o *Ownership) consumeInto(target ast.Expr, value ast.Expr, dest ownDest, s *ownState) *ownState {
	held := len(o.projections)
	o.projections = append(o.projections, ownProjection{place: target, kind: projectTarget})
	s = o.consume(value, dest, s)
	o.projections = o.projections[:held]
	return s
}

func (o *Ownership) VisitDiscardStmt(st *ast.DiscardStmt, s *ownState) *ownState {
	return o.expr(st.Value, s)
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
	return mergeOwn(a, b)
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
	NewOwnership(o.errors).Check(st.Fn)
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
	return ownUnreachable
}

func (o *Ownership) VisitBreakStmt(st *ast.BreakStmt, s *ownState) *ownState {
	frame := o.frameOf(st.Target)
	frame.breaks = mergeOwn(frame.breaks, s)
	return ownUnreachable
}

func (o *Ownership) VisitContinueStmt(st *ast.ContinueStmt, s *ownState) *ownState {
	frame := o.frameOf(st.Target)
	frame.continues = mergeOwn(frame.continues, s)
	return ownUnreachable
}

// loop walks the body again from what the first walk added to the state, which is enough for a fact to reach the
// top of the next iteration; a first walk that adds nothing needs no second.
func (o *Ownership) loop(loop ast.Node, s *ownState, body []ast.Stmt, cond ast.Expr) *ownState {
	frame := o.pushFrame(loop)
	entry := s
	if cond != nil {
		entry = o.expr(cond, s)
	}
	end := ownUnreachable
	for range 2 {
		end = o.walk(body, entry)
		next := mergeOwn(mergeOwn(entry, end), frame.continues)
		if next == entry {
			break
		}
		entry = next
		if cond != nil {
			entry = o.expr(cond, entry)
		}
	}
	o.frames = o.frames[:len(o.frames)-1]
	return mergeOwn(mergeOwn(entry, end), frame.breaks)
}

func (o *Ownership) match(m *ast.MatchCore, s *ownState) *ownState {
	s = o.expr(m.Subject, s)
	frame := o.pushFrame(m)
	subject, subjectIsPath, subjectKnown := accessPath{}, false, false
	result := ownUnreachable
	for _, arm := range m.Arms {
		a := s
		borrows := armBorrows(arm)
		if borrows && !subjectKnown {
			subject, subjectIsPath = pathOf(m.Subject, o.fn.SelfLocal, nil)
			subjectKnown = true
		}
		if borrows && subjectIsPath {
			o.projections = append(o.projections, ownProjection{subject: subject, kind: projectMatch})
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
		result = mergeOwn(result, a)
	}
	o.frames = o.frames[:len(o.frames)-1]
	return mergeOwn(result, frame.breaks)
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
			"'%s' was moved into %s at line %d; assign it again before reading it, or move a copy instead: '%s.copy()'",
			l.Name, fact.dest, fact.site.P().Line, l.Name)
	case factUninitialised:
		o.fail(site, diag.ErrUseBeforeInit,
			"'%s' has no value here; assign it on every path before reading it", l.Name)
	}
	return s.without(l)
}

// consume is an expression whose value is stored, passed to a consumes parameter or returned. Code that can run marks the expression
// consumed, which is how lowering learns where a value changes owner. A scalar is copied, and a fresh value needs
// nothing. A place holding a managed value is moved out of, which only a whole local that owns its value allows;
// everything else is refused and says to copy.
func (o *Ownership) consume(e ast.Expr, dest ownDest, s *ownState) *ownState {
	s = o.expr(e, s)
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
				"a part of a temporary value cannot be moved out of it into %s, because the temporary is dropped when the statement ends; copy it with '.copy()'",
				dest)
		}
		return s
	}

	switch {
	case p.crossesFrozen():
		o.fail(e, diag.ErrMoveOutOfField,
			"'%s' is part of a frozen value, which keeps its contents and shares them with its other owners, so it cannot be moved out of it into %s; use '%s.copy()'",
			p.text(), dest, p.text())
		return s
	case p.root.Borrowed():
		o.fail(e, diag.ErrMoveOutOfBorrow,
			"'%s' is borrowed, not owned, so it cannot be moved into %s; use '%s.copy()' to give it its own copy",
			p.text(), dest, p.text())
		return s
	case len(p.steps) > 0 && p.steps[len(p.steps)-1].isElement():
		o.fail(e, diag.ErrMoveOutOfField,
			"'%s' is an element of a collection, which owns it, so it cannot be moved out of it into %s; use '%s.copy()', or take it out of the collection with its remove method",
			p.text(), dest, p.text())
		return s
	case len(p.steps) > 0:
		o.fail(e, diag.ErrMoveOutOfField,
			"'%s' is a field of '%s', so it cannot be moved out of it into %s; use '%s.copy()'",
			p.text(), p.root.Name, dest, p.text())
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
			"'%s' borrows part of the match subject and cannot be changed; copy it into a var first: 'var x = %s.copy()'", root.Name, root.Name)
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
	case root.IsParam && root.Conv == ast.ConvConsumes:
		o.fail(site, diag.ErrAssignToImmutable,
			"'%s' cannot be changed because '%s' is a 'consumes' parameter; move it into a var first", p.text(), root.Name)
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
				"'%s' is borrowed by a binding of an enclosing match on '%s'; copy the payload out with '.copy()' before changing it",
				p.text(), pr.subject.text())
		case projectArgument:
			o.fail(site, diag.ErrExclusivity,
				"'%s' is borrowed by the argument '%s', which the enclosing call holds while this runs; compute this first into a variable, or pass a copy: '%s.copy()'",
				p.text(), pr.subject.text(), pr.subject.text())
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

func (o *Ownership) expr(e ast.Expr, s *ownState) *ownState {
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
	return mergeOwn(o.consume(e.Then, dest, s), o.consume(e.Else, dest, s))
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
	o.projections = append(o.projections, ownProjection{place: arg, kind: projectArgument})
}

// argument passes an expression as the convention says: a let borrows it, mutates borrows it to change, consumes
// takes it, and sets initialises it.
func (o *Ownership) argument(e ast.Expr, conv ast.Convention, dest ownDest, s *ownState) *ownState {
	switch conv {
	case ast.ConvLet:
		return o.expr(e, s)
	case ast.ConvMutates:
		s = o.expr(e, s)
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
			o.fail(b.expr, diag.ErrExclusivity, "%s", overlapMessage(a, b))
		}
	}
}

func (o *Ownership) addAccess(e ast.Expr, conv ast.Convention) {
	if p, ok := pathOf(e, o.fn.SelfLocal, nil); ok {
		o.accesses = append(o.accesses, callAccess{expr: e, path: p, conv: conv})
	}
}

func overlapMessage(a, b callAccess) string {
	changing, other := a, b
	if a.conv == ast.ConvLet {
		changing, other = b, a
	}
	message := fmt.Sprintf("'%s' and '%s' overlap, and '%s' is passed %s, so the callee would reach one place through two names",
		a.path.text(), b.path.text(), changing.path.text(), changing.conv)
	if other.conv == ast.ConvLet {
		return fmt.Sprintf("%s; pass a copy of the borrowed one: '%s.copy()'", message, other.path.text())
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
	o.consume(e, ownDest{kind: destField, name: name}, ownClean)
}
