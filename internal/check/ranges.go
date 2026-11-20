package check

import (
	"fmt"
	"slices"

	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
)

// rangeHandler is a try that catches the failures of what it covers: the state where its handler starts is the join of
// the states at those failures. A try with no handler passes them to the caller.
type rangeHandler struct {
	propagates bool
	entry      *rangeState
}

// rangeFrame is a loop or a match being walked, which a break leaves for the state after it.
type rangeFrame struct {
	node   ast.NodeID
	breaks *rangeState
}

// Ranges is the range analysis of the bodies: it follows the span of each integer local through the code, and of each field
// that a condition narrows, and how places compare, and decides for each operator and cast how it can still fail
// (ast.Fails). An operation that can fail must be covered by a try, and so must a call of a function that throws; lowering
// checks only what can fail.
//
// The analysis is one walk, linear in the code. What may be assigned (a var, a mutates or sets parameter, and their
// fields) loses what is known of it at the top of a loop, and a place loses it when it is assigned or a call may change it,
// so no loop is walked twice.
type Ranges struct {
	project  *ast.Project
	typing   *ast.Typing
	errs     *[]*diag.CompileError
	fn       *ast.FuncDecl
	st       *rangeState
	handlers []rangeHandler
	frames   []rangeFrame
	// behaviour is whether fn is the body of a behaviour, which nothing waits for.
	behaviour bool
	// written are the places the walk has seen assigned or given to a call that may change them, in order: what a condition
	// says of a place it changes itself, as 'a > 5 and reset(a)' does, is not known afterwards.
	written []place
	states  *states
	// bounds are the accumulators of the loops being walked, which keep a span through every loop inside theirs.
	bounds []accBound
	// probe is set while the body of a loop is walked to learn how its accumulators move: the walk records no error and no
	// Fails, and the state it leaves behind is discarded.
	probe        *loopProbe
	probeScratch loopProbe
	// scanned is the room the scan for accumulators reuses.
	scanned []candidate
}

// FlowScratch is what following the values of a module's functions reuses for the next module.
type FlowScratch struct{ states states }

// newRanges is the analysis of a module's bodies, which makes its states in the arena a.
func newRanges(p *ast.Project, typing *ast.Typing, errs *[]*diag.CompileError, a *states) *Ranges {
	return &Ranges{project: p, typing: typing, errs: errs, states: a}
}

// keep marks the current state as one that something comes back to, and gives it.
func (r *Ranges) keep() *rangeState {
	r.st.kept = true
	return r.st
}

// own makes the current state one that can be changed in place.
func (r *Ranges) own() {
	if r.st.kept {
		r.st = r.states.clone(r.st)
	}
}

// Check analyses a function's body.
func (r *Ranges) Check(f *ast.FuncDecl) {
	diag.Guard(r.errs, func() { r.function(f, false) })
}

func (r *Ranges) function(f *ast.FuncDecl, behaviour bool) {
	outerFn, outerSt, outerHandlers, outerFrames, outerBounds, outerBehaviour, outerChanged := r.fn, r.st, r.handlers, r.frames, r.bounds, r.behaviour, len(r.written)
	r.fn, r.handlers, r.frames, r.bounds, r.behaviour = f, nil, nil, nil, behaviour
	m := r.states.mark()
	r.st = r.states.state(len(f.Locals) + 1)
	clear(r.st.spans)
	for _, l := range f.Locals {
		if isIntTy(l.Type) {
			r.st.spans[l.ID()] = typeSpan(l.Type)
		}
	}
	r.block(f.Body)
	r.states.release(m)
	r.fn, r.st, r.handlers, r.frames, r.bounds, r.behaviour, r.written = outerFn, outerSt, outerHandlers, outerFrames, outerBounds, outerBehaviour, r.written[:outerChanged]
}

// CheckValues analyses the defaults of a record's fields, which no function holds.
func (r *Ranges) CheckValues(d ast.Decl) {
	td, ok := d.(*ast.TypeDecl)
	if !ok {
		return
	}
	for _, f := range td.Fields {
		if !f.Default.IsNil() {
			diag.Guard(r.errs, func() { r.value(f.Default) })
		}
	}
}

// value analyses an expression outside any function, a constant's value or a field's default, and gives its span.
func (r *Ranges) value(e ast.Expr) span {
	r.fn, r.handlers, r.frames, r.bounds = nil, nil, nil, nil
	r.st = &rangeState{}
	return r.eval(e)
}

// Constant analyses a constant's value and records its span, which a name of the constant has wherever it is used.
func (r *Ranges) Constant(cd *ast.ConstDecl) {
	s := r.value(cd.Value)
	if isIntTy(cd.Type) {
		cd.Low, cd.High = s.lo, s.hi
	}
}

func (r *Ranges) block(body ast.StmtList) {
	for _, s := range body.All() {
		ast.VisitStmt(r, s)
	}
}

// branch walks one way the code can go from a state of its own, and gives the state it ends in.
func (r *Ranges) branch(from *rangeState, walk func()) *rangeState {
	r.st = from
	walk()
	return r.st
}

// forget makes what is known of what may be assigned its type's span again: the state at the top of a loop, which the end of
// the body comes back to. What may be assigned is a var, a mutates or sets parameter, and the fields of either. The
// accumulators of the loops around keep their bounds.
func (r *Ranges) forget() {
	if r.fn == nil || r.st.dead {
		return
	}
	r.own()
	for _, l := range r.fn.Locals {
		if l.Mutable() && isIntTy(l.Type) {
			r.st.spans[l.ID()] = typeSpan(l.Type)
		}
	}
	for _, b := range r.bounds {
		r.st.spans[b.local] = r.st.spans[b.local].clamp(b.span)
	}
	r.st.drop(func(p place) bool { return r.fn.LocalAt(p.root).Mutable() })
}

// write gives a place the value of the span s, after which nothing known of the place before holds.
func (r *Ranges) write(p place, s span, t ast.Ty) {
	if r.st.dead {
		return
	}
	r.own()
	r.written = append(r.written, p)
	if r.probe != nil && p.depth == 0 {
		r.probe.noteWrite(p.root)
	}
	r.st.drop(p.overlaps)
	if isIntTy(t) {
		r.st.refine(p, s.clamp(typeSpan(t)))
	}
}

// changed is a place that a call may change: what was known of it no longer holds.
func (r *Ranges) changed(p place, t ast.Ty) {
	if isIntTy(t) {
		r.write(p, typeSpan(t), t)
	} else {
		r.write(p, span{}, t)
	}
}

// placeOf is the place an expression names, if it names one the analysis follows: a local, a field of the receiver, or a
// field of a place that is not too deep already.
func (r *Ranges) placeOf(e ast.Expr) (place, bool) {
	if r.fn == nil {
		return place{}, false
	}
	switch e.Kind() {
	case ast.KindNameExpr:
		name := r.typing.Name(e.ToNameExpr())
		switch name.Kind() {
		case ast.NameLocal:
			return place{root: name.Local(r.fn).ID()}, true
		case ast.NameSelfField:
			return place{root: r.fn.SelfLocal.ID()}.field(name.Field(r.fn))
		}
	case ast.KindSelfExpr:
		return place{root: r.fn.SelfLocal.ID()}, true
	case ast.KindMemberExpr:
		m := e.ToMemberExpr()
		if info := r.typing.Member(m); info.Kind == ast.MemberField {
			if p, ok := r.placeOf(m.Obj()); ok {
				return p.field(info.Field)
			}
		}
	}
	return place{}, false
}

// spanAt is what is known of an integer place.
func (r *Ranges) spanAt(st *rangeState, p place, t ast.Ty) span {
	if st.dead {
		return typeSpan(t)
	}
	if s, ok := st.at(p); ok {
		return s
	}
	return typeSpan(t)
}

func (r *Ranges) VisitLetStmt(s ast.LetStmt) {
	l := r.fn.LocalAt(r.typing.LetLocal(s))
	var v span
	switch {
	case !s.Init().IsNil():
		v = r.eval(s.Init())
	case isIntTy(l.Type):
		v = typeSpan(l.Type)
	}
	r.write(place{root: l.ID()}, v, l.Type)
}

func (r *Ranges) VisitAssignStmt(s ast.AssignStmt) {
	if a, ok := r.accumulation(s); ok {
		r.accumulate(s, a)
		return
	}
	target := s.Target()
	p, ok := r.placeOf(target)
	if !ok {
		r.eval(target)
	}
	v := r.eval(s.Value())
	if ok {
		r.write(p, v, r.typing.Type(target))
	}
}

func (r *Ranges) VisitDiscardStmt(s ast.DiscardStmt) { r.eval(s.Value()) }

func (r *Ranges) VisitExprStmt(s ast.ExprStmt) {
	r.eval(s.E())
	if _, ok := s.E().AsPanicExpr(); ok {
		r.st = deadState
	}
}

func (r *Ranges) VisitIfStmt(s ast.IfStmt) {
	since := r.condition(s.Cond())
	start := r.keep()
	then := r.branch(r.narrowed(start, s.Cond(), true, since), func() { r.block(s.Then()) })
	els := r.branch(r.narrowed(start, s.Cond(), false, since), func() {
		if s.HasElse() {
			r.block(s.Else())
		}
	})
	r.st = r.states.joined(then, els)
}

func (r *Ranges) VisitWhileStmt(s ast.WhileStmt) {
	r.forget()
	since := r.condition(s.Cond())
	top := r.keep()
	r.frames = append(r.frames, rangeFrame{node: s.ID})
	times := r.scaleTimes(pastEveryWord)
	r.branch(r.narrowed(top, s.Cond(), true, since), func() { r.block(s.Body()) })
	r.restoreTimes(times)
	breaks := r.frames[len(r.frames)-1].breaks
	r.frames = r.frames[:len(r.frames)-1]
	exit := r.narrowed(top, s.Cond(), false, since)
	if isTrue(s.Cond()) {
		exit = deadState
	}
	r.st = r.states.joined(exit, breaks)
}

func (r *Ranges) VisitForStmt(s ast.ForStmt) {
	from := r.eval(s.From())
	to := r.eval(s.To())
	trips := tripBound(from, to, s.Inclusive())
	times := r.scaleTimes(trips)
	bounded := len(r.bounds)
	r.loopHead(s, from, to, trips)
	top := r.keep()
	r.frames = append(r.frames, rangeFrame{node: s.ID})
	r.branch(r.loopBody(s, top, from, to), func() { r.block(s.Body()) })
	breaks := r.frames[len(r.frames)-1].breaks
	r.frames = r.frames[:len(r.frames)-1]
	r.st = r.states.joined(top, breaks)
	r.bounds = r.bounds[:bounded]
	r.restoreTimes(times)
}

// loopBody is the state in the body of a for loop, whose variable lies between the bounds: the state at the top of the loop
// when the variable has no span to know.
func (r *Ranges) loopBody(s ast.ForStmt, top *rangeState, from, to span) *rangeState {
	l := r.fn.LocalAt(r.typing.ForLocal(s))
	if l == nil || !isIntTy(l.Type) || top.dead {
		return top
	}
	hi := to.hi
	if !s.Inclusive() {
		hi = hi.Sub(ast.One)
	}
	body := r.states.clone(top)
	v := span{lo: from.lo, hi: hi}.clamp(typeSpan(l.Type))
	if v.empty() {
		body.dead = true
	} else {
		body.refine(place{root: l.ID()}, v)
	}
	return body
}

func (r *Ranges) VisitMatchStmt(s ast.MatchStmt) { r.match(s.Match()) }

// VisitWhenStmt analyses the body of a behaviour as a function of its own: it runs later, so neither what is known here
// nor a try around it reaches into it.
func (r *Ranges) VisitWhenStmt(s ast.WhenStmt) {
	if r.probe != nil {
		r.probe.opaque = true
		return
	}
	r.function(r.typing.When(s).Fn, true)
}

// enterTry starts a try. One with a handler catches what it covers; one without passes it to the try around it, and with
// none around it to the caller, which only a function that throws has. It gives whether the try added a handler.
func (r *Ranges) enterTry(site ast.Pos, handles bool) bool {
	if !handles && len(r.handlers) > 0 {
		return false
	}
	if !handles {
		r.requirePropagates(site)
	}
	r.handlers = append(r.handlers, rangeHandler{propagates: !handles})
	return true
}

// leaveTry ends a try that enterTry added a handler for, and gives the handler.
func (r *Ranges) leaveTry() rangeHandler {
	h := r.handlers[len(r.handlers)-1]
	r.handlers = r.handlers[:len(r.handlers)-1]
	return h
}

// requirePropagates refuses a try with no handler where there is no caller that takes its failure.
func (r *Ranges) requirePropagates(site ast.Pos) {
	switch {
	case r.probe != nil:
	case r.fn == nil:
		panic(site.Error(diag.ErrNothingToPropagate, "a 'try' without 'otherwise' passes a failure to the caller, and a constant or a default has none: give 'otherwise' the value to use"))
	case r.behaviour:
		panic(site.Error(diag.ErrNothingToPropagate, "a 'try' without 'otherwise' passes a failure to the caller, and nothing waits for a behaviour: give 'otherwise' what to do"))
	case r.fn.ErrorType == nil:
		panic(site.Error(diag.ErrNothingToPropagate, fmt.Sprintf("a 'try' without 'otherwise' passes a failure to the caller, but '%s' does not throw: give 'otherwise' what to do, or declare '%s' 'throws ArithmeticError'", r.fn.Name, r.fn.Name)))
	}
}

func (r *Ranges) VisitTryStmt(s ast.TryStmt) {
	added := r.enterTry(s.Position(), s.HasOtherwise())
	r.block(s.Body())
	if !added {
		return
	}
	h := r.leaveTry()
	if !s.HasOtherwise() {
		return
	}
	end := r.keep()
	entry := h.entry
	if entry == nil {
		entry = deadState
	}
	r.st = r.states.joined(end, r.branch(entry, func() { r.block(s.Handler()) }))
}

func (r *Ranges) VisitReturnStmt(s ast.ReturnStmt) {
	if !s.Value().IsNil() {
		r.eval(s.Value())
	}
	r.st = deadState
}

func (r *Ranges) VisitBreakStmt(s ast.BreakStmt) {
	target := r.typing.BreakTarget(s)
	for i := len(r.frames) - 1; i >= 0; i-- {
		if r.frames[i].node == target {
			r.frames[i].breaks = r.states.joined(r.frames[i].breaks, r.keep())
			break
		}
	}
	r.st = deadState
}

func (r *Ranges) VisitContinueStmt(ast.ContinueStmt) { r.st = deadState }

// match walks each arm from the state after the subject, narrowed by the arm's patterns when the subject is an integer
// local, and gives the span of the values the arms give.
func (r *Ranges) match(m ast.MatchCore) span {
	r.eval(m.Subject())
	start := r.keep()
	subject, isPlace := r.placeOf(m.Subject())
	isPlace = isPlace && isIntTy(r.typing.Type(m.Subject()))
	r.frames = append(r.frames, rangeFrame{node: m.ID})
	var end *rangeState
	var given span
	gives := false
	for _, arm := range m.Arms().All() {
		from := start
		if isPlace && !arm.IsDefault() {
			from = r.armState(start, subject, r.typing.Type(m.Subject()), arm)
		}
		r.st = from
		switch arm.Form() {
		case ast.ArmThen:
			ast.VisitStmt(r, arm.Action())
		case ast.ArmGives:
			v := r.eval(arm.Value())
			if !r.st.dead && isIntTy(r.typing.Type(arm.Value())) {
				if gives {
					given = given.union(v)
				} else {
					given, gives = v, true
				}
			}
		case ast.ArmPanic:
			r.eval(arm.Value())
			r.st = deadState
		}
		end = r.states.joined(end, r.keep())
	}
	breaks := r.frames[len(r.frames)-1].breaks
	r.frames = r.frames[:len(r.frames)-1]
	end = r.states.joined(end, breaks)
	if end == nil {
		end = deadState
	}
	r.st = end
	return given
}

// armState is the state in an arm of a match on an integer place: the place holds one of the values the patterns name.
func (r *Ranges) armState(start *rangeState, subject place, t ast.Ty, arm ast.MatchArm) *rangeState {
	if start.dead {
		return start
	}
	var values span
	for i, pat := range arm.Patterns().All() {
		var s span
		switch pat.Form() {
		case ast.PatternLiteral:
			s = typeSpan(r.typing.Type(pat.Value()))
		case ast.PatternRange:
			s = span{lo: typeSpan(r.typing.Type(pat.Value())).lo, hi: typeSpan(r.typing.Type(pat.High())).hi}
			if !pat.Inclusive() {
				s.hi = s.hi.Sub(ast.One)
			}
		default:
			return start
		}
		if i == 0 {
			values = s
		} else {
			values = values.union(s)
		}
	}
	out := r.states.clone(start)
	narrowedTo := r.spanAt(start, subject, t).clamp(values)
	if narrowedTo.empty() {
		out.dead = true
	} else {
		out.refine(subject, narrowedTo)
	}
	return out
}

// condition evaluates a condition, and gives where the places it changes begin in written.
func (r *Ranges) condition(c ast.Expr) int {
	since := len(r.written)
	r.eval(c)
	return since
}

// narrowed is the state in which the condition c had the value truth, except for the places that changed since, which
// the condition itself changed after it read them.
func (r *Ranges) narrowed(from *rangeState, c ast.Expr, truth bool, since int) *rangeState {
	if from.dead {
		return from
	}
	out := from
	r.narrow(&out, from, c, truth, r.written[since:])
	return out
}

// changedIn is whether a place overlaps one of the changed ones.
func changedIn(p place, changed []place) bool {
	return slices.ContainsFunc(changed, p.overlaps)
}

// narrow narrows *st, which is from or a copy of it made on the first change, by c having the value truth.
func (r *Ranges) narrow(st **rangeState, from *rangeState, c ast.Expr, truth bool, changed []place) {
	switch c.Kind() {
	case ast.KindUnaryExpr:
		u := c.ToUnaryExpr()
		if u.Op() == "not" && r.typing.Type(c) == ast.Ty(ast.BoolTy) {
			r.narrow(st, from, u.E(), !truth, changed)
		}
	case ast.KindBinaryExpr:
		b := c.ToBinaryExpr()
		switch {
		case b.Op() == "and" && truth, b.Op() == "or" && !truth:
			if r.typing.Type(c) == ast.Ty(ast.BoolTy) {
				r.narrow(st, from, b.L(), truth, changed)
				r.narrow(st, from, b.R(), truth, changed)
			}
		case checkerComparison[b.Op()] && isIntTy(r.typing.Type(b.L())) && isIntTy(r.typing.Type(b.R())):
			op := b.Op()
			if !truth {
				op = negatedComparison[op]
			}
			r.compare(st, from, b.L(), op, b.R(), changed)
			r.compare(st, from, b.R(), flippedComparison[op], b.L(), changed)
		}
	}
}

var (
	negatedComparison = map[string]string{"==": "!=", "!=": "==", "<": ">=", "<=": ">", ">": "<=", ">=": "<"}
	flippedComparison = map[string]string{"==": "==", "!=": "!=", "<": ">", "<=": ">=", ">": "<", ">=": "<="}
)

// compare narrows x, if it is a place, by 'x op y', and records how x and y compare when both are places.
func (r *Ranges) compare(st **rangeState, from *rangeState, x ast.Expr, op string, y ast.Expr, changed []place) {
	px, ok := r.placeOf(x)
	if !ok || (*st).dead || changedIn(px, changed) {
		return
	}
	if py, isPlace := r.placeOf(y); isPlace && !changedIn(py, changed) {
		r.relate(st, from, px, op, py)
	}
	cur := r.spanAt(*st, px, r.typing.Type(x))
	other := r.peek(*st, y)
	next := cur
	switch op {
	case "<":
		next.hi = ast.MinI(cur.hi, other.hi.Sub(ast.One))
	case "<=":
		next.hi = ast.MinI(cur.hi, other.hi)
	case ">":
		next.lo = ast.MaxI(cur.lo, other.lo.Add(ast.One))
	case ">=":
		next.lo = ast.MaxI(cur.lo, other.lo)
	case "==":
		next = cur.clamp(other)
		next.nonZero = cur.nonZero || other.excludesZero()
	case "!=":
		if other.lo != other.hi {
			return
		}
		switch v := other.lo; {
		case v == cur.lo:
			next.lo = cur.lo.Add(ast.One)
		case v == cur.hi:
			next.hi = cur.hi.Sub(ast.One)
		case v.IsZero():
			next.nonZero = true
		}
	}
	if next == cur {
		return
	}
	if *st == from {
		*st = r.states.clone(from)
	}
	if next.empty() || (next.lo.IsZero() && next.hi.IsZero() && next.nonZero) {
		(*st).dead = true
		return
	}
	(*st).refine(px, next)
}

// relate records what 'x op y' says of the difference of two places, which a subtraction of them reads.
func (r *Ranges) relate(st **rangeState, from *rangeState, x place, op string, y place) {
	var gap ast.I128
	switch op {
	case ">":
		gap = ast.One
	case ">=", "==":
	default:
		return
	}
	if *st == from {
		*st = r.states.clone(from)
	}
	(*st).relate(x, y, gap)
}

// peek is the span of an operand of a condition that has been evaluated already: a local's, a constant's, or else its
// type's.
func (r *Ranges) peek(st *rangeState, e ast.Expr) span {
	t := r.typing.Type(e)
	if _, ok := t.(*ast.ConstIntTy); ok {
		return typeSpan(t)
	}
	if p, ok := r.placeOf(e); ok {
		return r.spanAt(st, p, t)
	}
	if n, ok := e.AsNameExpr(); ok {
		if name := r.typing.Name(n); name.Kind() == ast.NameConst {
			return constSpan(r.typing.Const(name))
		}
	}
	return typeSpan(t)
}

func constSpan(cd *ast.ConstDecl) span { return span{lo: cd.Low, hi: cd.High} }

// eval walks an expression and gives its span, which means nothing for a value that is not an integer.
func (r *Ranges) eval(e ast.Expr) span {
	t := r.typing.Type(e)
	if _, ok := t.(*ast.ConstIntTy); ok {
		return typeSpan(t)
	}
	return ast.VisitExprResult[span](r, e)
}

// typed is the span of a value that the analysis knows nothing more of than its type.
func (r *Ranges) typed(e ast.Expr) span {
	if t := r.typing.Type(e); isIntTy(t) {
		return typeSpan(t)
	}
	return span{}
}

func (r *Ranges) VisitIntLit(e ast.IntLit) span { return exactly(e.Value()) }

func (r *Ranges) VisitBoolLit(ast.BoolLit) span { return span{} }

func (r *Ranges) VisitSelfExpr(ast.SelfExpr) span { return span{} }

func (r *Ranges) VisitStrLit(e ast.StrLit) span {
	for _, part := range e.Parts().All() {
		if !part.Expr.IsNil() {
			r.eval(part.Expr)
		}
	}
	return span{}
}

func (r *Ranges) VisitStructLitExpr(e ast.StructLitExpr) span {
	for _, v := range r.typing.StructLit(e).Ordered {
		if !v.IsNil() {
			r.eval(v)
		}
	}
	return span{}
}

func (r *Ranges) VisitNameExpr(e ast.NameExpr) span {
	if p, ok := r.placeOf(e.Expr()); ok && isIntTy(r.typing.Type(e.Expr())) {
		return r.spanAt(r.st, p, r.typing.Type(e.Expr()))
	}
	if name := r.typing.Name(e); name.Kind() == ast.NameConst {
		if cd := r.typing.Const(name); isIntTy(cd.Type) {
			return constSpan(cd)
		}
	}
	return r.typed(e.Expr())
}

func (r *Ranges) VisitPanicExpr(e ast.PanicExpr) span {
	r.eval(e.Message())
	return span{}
}

func (r *Ranges) VisitMatchExpr(e ast.MatchExpr) span { return r.match(e.Match()) }

func (r *Ranges) VisitMemberExpr(e ast.MemberExpr) span {
	switch r.typing.Member(e).Kind {
	case ast.MemberEnumCase, ast.MemberUnionCase:
	default:
		r.eval(e.Obj())
	}
	if p, ok := r.placeOf(e.Expr()); ok && isIntTy(r.typing.Type(e.Expr())) {
		return r.spanAt(r.st, p, r.typing.Type(e.Expr()))
	}
	return r.typed(e.Expr())
}

func (r *Ranges) VisitIndexExpr(e ast.IndexExpr) span {
	r.eval(e.Obj())
	for _, a := range e.Args().All() {
		r.eval(a)
	}
	return r.typed(e.Expr())
}

func (r *Ranges) VisitCallExpr(e ast.CallExpr) span {
	call := r.typing.Call(e)
	if receiver := e.ReceiverFor(call.Kind); !receiver.IsNil() {
		r.eval(receiver)
	}
	for _, a := range call.Ordered {
		r.eval(a)
	}
	if call.Pass != nil {
		for i, a := range call.Ordered {
			if p, ok := r.placeOf(a); ok && call.Pass.Specs[i].Conv.ByAddress() {
				r.changed(p, r.typing.Type(a))
			}
		}
		if receiver := e.ReceiverFor(call.Kind); !receiver.IsNil() && call.Pass.SelfConv != ast.ConvLet {
			if p, ok := r.placeOf(receiver); ok {
				r.changed(p, r.typing.Type(receiver))
			}
		}
	}
	if callThrows(call) {
		r.fallible(e.Position(), func() string {
			name := call.Target.Name
			if call.Kind == ast.CallBound {
				name = call.Generic.Method.Name
			}
			return fmt.Sprintf("'%s' throws ArithmeticError, so the call can fail; handle the failure with 'try'", name)
		})
	}
	return r.typed(e.Expr())
}

// callThrows is whether a call runs a function that throws.
func callThrows(call *ast.CallInfo) bool {
	switch call.Kind {
	case ast.CallFunction, ast.CallStatic, ast.CallMethod:
		return call.Target.ErrorType != nil
	case ast.CallBound:
		return call.Generic.Method.ErrorType != nil
	}
	return false
}

func (r *Ranges) VisitCondExpr(e ast.CondExpr) span {
	since := r.condition(e.Cond())
	start := r.keep()
	var then, els span
	thenEnd := r.branch(r.narrowed(start, e.Cond(), true, since), func() { then = r.eval(e.Then()) })
	elseEnd := r.branch(r.narrowed(start, e.Cond(), false, since), func() { els = r.eval(e.Else()) })
	r.st = r.states.joined(thenEnd, elseEnd)
	switch {
	case thenEnd.dead:
		return els
	case elseEnd.dead:
		return then
	}
	return then.union(els)
}

// VisitTryExpr covers the failures of its expression: a try with 'otherwise' catches them, and one without passes them to
// the caller.
func (r *Ranges) VisitTryExpr(e ast.TryExpr) span {
	added := r.enterTry(e.Position(), !isPropagating(e))
	v := r.eval(e.E())
	if !added {
		return v
	}
	h := r.leaveTry()
	if h.propagates {
		return v
	}
	end := r.keep()
	entry := h.entry
	if entry == nil {
		entry = deadState
	}
	var other span
	failed := r.branch(entry, func() {
		switch {
		case !e.Value().IsNil():
			other = r.eval(e.Value())
		case !e.Ret().IsNil():
			r.VisitReturnStmt(e.Ret())
		case !e.Brk().IsNil():
			r.VisitBreakStmt(e.Brk())
		case !e.Cont().IsNil():
			r.VisitContinueStmt(e.Cont())
		}
	})
	r.st = r.states.joined(end, failed)
	switch {
	case failed.dead:
		return v
	case end.dead:
		return other
	}
	return v.union(other)
}

// VisitBinaryExpr is an 'and' or an 'or' that evaluates its right operand only when the left one allows it, a comparison,
// or an operator on integers.
func (r *Ranges) VisitBinaryExpr(e ast.BinaryExpr) span {
	if (e.Op() == "and" || e.Op() == "or") && r.typing.Type(e.Expr()) == ast.Ty(ast.BoolTy) {
		since := r.condition(e.L())
		start := r.keep()
		end := r.branch(r.narrowed(start, e.L(), e.Op() == "and", since), func() { r.eval(e.R()) })
		r.st = r.states.joined(start, end)
		return span{}
	}
	l := r.eval(e.L())
	rs := r.eval(e.R())
	return r.operate(e, l, rs, nil)
}

// operate is the span of an operator on operands of the spans l and rs. An exact result outside bound, if there is one, is
// one the code cannot reach.
func (r *Ranges) operate(e ast.BinaryExpr, l, rs span, bound *span) span {
	t := r.typing.Type(e.Expr())
	if checkerComparison[e.Op()] || !isIntTy(t) {
		return span{}
	}
	word := signedWord
	if r.typing.Operand(e) == ast.OperandUnsigned {
		word = unsignedWord
	}
	exact, fails := arithmetic(e.Op(), l, rs, word)
	if bound != nil {
		if within := exact.clamp(*bound); !within.empty() {
			exact = within
		}
	}
	if e.Op() == "-" && !r.st.dead {
		px, okL := r.placeOf(e.L())
		py, okR := r.placeOf(e.R())
		if gap, related := r.st.gap(px, py); okL && okR && related {
			exact.lo = ast.MaxI(exact.lo, gap)
		}
	}
	return r.result(e.Expr(), e.Op(), exact, fails, word)
}

// result judges an operation whose exact result has the span exact: it fails where that leaves the word it computes in or
// the range of its type, and gives the span of the values it can give.
func (r *Ranges) result(e ast.Expr, op string, exact span, fails ast.Fails, word span) span {
	t := typeSpan(r.typing.Type(e))
	if !exact.empty() {
		if !exact.within(word) {
			fails |= ast.FailOverflow
		}
		inWord := exact.clamp(word)
		if inWord.lo.Lt(t.lo) {
			fails |= ast.FailBelow
		}
		if inWord.hi.Gt(t.hi) {
			fails |= ast.FailAbove
		}
	}
	r.fail(e, fails, func() string {
		return failureText(op, fails, t, r.typing.Type(e)) + operandFix(guardAdvice(op, fails, e))
	})
	return exact.clamp(t)
}

// failureText says how an operation can fail, for the error that it is not caught.
func failureText(op string, fails ast.Fails, t span, ty ast.Ty) string {
	switch {
	case fails&ast.FailZero != 0:
		return fmt.Sprintf("the divisor of '%s' may be 0", op)
	case fails&ast.FailShift != 0:
		return fmt.Sprintf("the count of '%s' may be negative", op)
	case fails&(ast.FailOverflow|ast.FailAbove) != 0 && fails&ast.FailBelow != 0:
		return fmt.Sprintf("the result of '%s' may not fit %s (%s to %s)", op, ty.Name(), t.lo, t.hi)
	case fails&ast.FailBelow != 0 || (fails&ast.FailOverflow != 0 && op == "-"):
		return fmt.Sprintf("the result of '%s' may be below the minimum of %s, %s", op, ty.Name(), t.lo)
	}
	return fmt.Sprintf("the result of '%s' may be above the maximum of %s, %s", op, ty.Name(), t.hi)
}

// guardAdvice is a condition that would rule the failure out, when one is short to say.
func guardAdvice(op string, fails ast.Fails, e ast.Expr) string {
	b, ok := e.AsBinaryExpr()
	if !ok {
		return ""
	}
	n, named := b.R().AsNameExpr()
	switch {
	case !named:
		return ""
	case fails&ast.FailZero != 0:
		return fmt.Sprintf("if %s != 0", n.Name())
	case fails&ast.FailShift != 0:
		return fmt.Sprintf("if %s >= 0", n.Name())
	}
	return ""
}

// operandFix is the end of the error for an operation that can fail because of its operands: a try, or the condition
// that guard says rules the failure out, or a general one.
func operandFix(guard string) string {
	if guard == "" {
		return "; handle the failure with 'try', or check the operands first with an 'if' that rules it out"
	}
	return fmt.Sprintf("; handle the failure with 'try', or guard it with '%s'", guard)
}

// fail records how the operation e can fail, if it can, and that the innermost try covers it. The probe of a loop records
// nothing, as the real walk of the loop will.
func (r *Ranges) fail(e ast.Expr, fails ast.Fails, message func() string) {
	if fails == 0 || r.st.dead {
		return
	}
	if r.probe == nil {
		r.typing.SetFails(e, fails)
	}
	r.fallible(e.Position(), message)
}

// fallible is an operation that can fail: the innermost try covers it, and its handler can start in the state here. No
// try is an error, which message says.
func (r *Ranges) fallible(site ast.Pos, message func() string) {
	if r.st.dead {
		return
	}
	if len(r.handlers) == 0 {
		if r.probe != nil {
			return
		}
		panic(site.Error(diag.ErrUncheckedFailure, message()))
	}
	h := &r.handlers[len(r.handlers)-1]
	if !h.propagates {
		h.entry = r.states.joined(h.entry, r.keep())
	}
}

func (r *Ranges) VisitUnaryExpr(e ast.UnaryExpr) span {
	v := r.eval(e.E())
	t := r.typing.Type(e.Expr())
	if !isIntTy(t) {
		return span{}
	}
	// A negation is computed in a signed word whatever type the context gives its result.
	word := signedWord
	if it, ok := t.(*ast.IntTy); ok && it.Unsigned() && e.Op() == "not" {
		word = unsignedWord
	}
	var exact span
	if e.Op() == "not" {
		// The bits of a value flipped: -v - 1 in a signed word, and u64.max - v in an unsigned one.
		if word == unsignedWord {
			exact = span{lo: ast.U64Max.Sub(v.hi), hi: ast.U64Max.Sub(v.lo)}
		} else {
			exact = span{lo: v.hi.Neg().Sub(ast.One), hi: v.lo.Neg().Sub(ast.One)}
		}
	} else {
		exact = span{lo: v.hi.Neg(), hi: v.lo.Neg()}
	}
	return r.result(e.Expr(), e.Op(), exact, 0, word)
}

// VisitCastExpr fails where the value may lie outside the target's range.
func (r *Ranges) VisitCastExpr(e ast.CastExpr) span {
	v := r.eval(e.E())
	t := typeSpan(r.typing.Type(e.Expr()))
	var fails ast.Fails
	if v.lo.Lt(t.lo) {
		fails |= ast.FailBelow
	}
	if v.hi.Gt(t.hi) {
		fails |= ast.FailAbove
	}
	ty := r.typing.Type(e.Expr())
	r.fail(e.Expr(), fails, func() string {
		return fmt.Sprintf("the value may not fit %s (%s to %s)", ty.Name(), t.lo, t.hi) + operandFix("")
	})
	return v.clamp(t)
}
