package check

import (
	"math/bits"
	"slices"

	"minimaxon/internal/ast"
)

// An accumulator of a for loop is a mutable integer local that the loop's body changes only by statements of the shape
// 'v = v + e', 'v = e + v' and 'v = v - e'. Each execution of such a statement moves v by at most what e's span allows, and
// the loop runs a bounded number of times, so v stays within a span that the loop's entry and its trip count decide. That
// span holds at every point of the loop and after it, which lets a sum that the loop builds be proven against its type
// instead of being forgotten at the top of the loop like any other variable.
//
// What e's span is cannot be known without walking the body, and walking it from the state that its top has once v is
// forgotten gives a span that holds whatever v is. That walk, the probe, changes nothing that the real walk after it reads.

// pastEveryWord is a count or a distance that no integer type can hold. Every bound the loop analysis computes stops here,
// so a product or a sum of them never overflows an I128, and a bound that reaches it is no bound at all.
var pastEveryWord = ast.U64Max.Add(ast.One)

// capped is v cut to pastEveryWord.
func capped(v ast.I128) ast.I128 { return ast.MinI(v, pastEveryWord) }

// scaled is a * b for two counts that are not negative, capped. ast.MulWide takes only operands that a word holds, and a
// count of nested loops can pass that.
func scaled(a, b ast.I128) ast.I128 {
	if a.IsZero() || b.IsZero() {
		return ast.Zero
	}
	if a.Hi != 0 || b.Hi != 0 {
		return pastEveryWord
	}
	high, low := bits.Mul64(a.Lo, b.Lo)
	if high != 0 {
		return pastEveryWord
	}
	return ast.U64(low)
}

// tripBound is the most iterations of 'for i in from to|upto to'. The bound is evaluated once, so the spans are those of the
// operands where the loop is entered.
func tripBound(from, to span, inclusive bool) ast.I128 {
	trips := to.hi.Sub(from.lo)
	if inclusive {
		trips = trips.Add(ast.One)
	}
	return capped(ast.MaxI(trips, ast.Zero))
}

// accBound is the span that an accumulator has at every point of the loop being walked and after it, which may reach past
// the type's: an operation whose exact result would leave the type is not stored, so it only has to be judged against the
// bound.
type accBound struct {
	local ast.LocalID
	span  span
}

// boundOf is the bound of an accumulator of a loop that is being walked, the tightest when loops are nested.
func (r *Ranges) boundOf(local ast.LocalID) (span, bool) {
	for _, v := range slices.Backward(r.bounds) {
		if v.local == local {
			return v.span, true
		}
	}
	return span{}, false
}

// bind makes bound the bound of an accumulator for the loop being walked, within the one an enclosing loop gave it, and
// narrows its span in the current state to it. A bound past the type on both sides says no more than the type does.
func (r *Ranges) bind(local ast.LocalID, bound span) {
	if outer, ok := r.boundOf(local); ok {
		if tighter := (span{lo: ast.MaxI(bound.lo, outer.lo), hi: ast.MinI(bound.hi, outer.hi)}); !tighter.empty() {
			bound = tighter
		}
	}
	t := typeSpan(r.fn.LocalAt(local).Type)
	if bound.lo.Lt(t.lo) && bound.hi.Gt(t.hi) {
		return
	}
	r.bounds = append(r.bounds, accBound{local: local, span: bound})
	r.st.spans[local] = bound.clamp(t)
}

// accumulation is a statement 'v = v + e', 'v = e + v' or 'v = v - e'.
type accumulation struct {
	place    place
	sum      ast.BinaryExpr
	subtract bool
	// eIsLeft is whether e is the left operand of the sum.
	eIsLeft bool
}

// accumulation is the shape of an assignment, if it is one that an accumulator is changed by.
func (r *Ranges) accumulation(s ast.AssignStmt) (accumulation, bool) {
	if s.Target().Kind() != ast.KindNameExpr || s.Value().Kind() != ast.KindBinaryExpr {
		return accumulation{}, false
	}
	p, ok := r.placeOf(s.Target())
	if !ok || p.depth != 0 || !isIntTy(r.typing.Type(s.Target())) {
		return accumulation{}, false
	}
	sum := s.Value().ToBinaryExpr()
	subtract := sum.Op() == "-"
	if sum.Op() != "+" && !subtract {
		return accumulation{}, false
	}
	names := func(e ast.Expr) bool {
		q, ok := r.placeOf(e)
		return ok && q == p
	}
	switch {
	case names(sum.L()):
		return accumulation{place: p, sum: sum, subtract: subtract}, true
	case !subtract && names(sum.R()):
		return accumulation{place: p, sum: sum, eIsLeft: true}, true
	}
	return accumulation{}, false
}

// accumulate is an assignment that changes an accumulator. Where a loop bounds it, the result is judged against that bound,
// which holds however many times the loop has run, and the probe of a loop learns from it how far the loop can move it.
func (r *Ranges) accumulate(s ast.AssignStmt, a accumulation) {
	l := r.eval(a.sum.L())
	rs := r.eval(a.sum.R())
	var bound *span
	if b, ok := r.boundOf(a.place.root); ok {
		bound = &b
	}
	v := r.operate(a.sum, l, rs, bound)
	if r.probe != nil && !r.st.dead {
		e := rs
		if a.eIsLeft {
			e = l
		}
		r.probe.record(a, e)
	}
	r.write(a.place, v, r.typing.Type(s.Target()))
}

// accumulated is what the probe of a loop learned of an accumulator: how its statements can move it in one iteration of
// the loop, and whether the body changes it in any other way.
type accumulated struct {
	// statements is how many accumulator statements the walk met, and writes how many times it saw the local written: they
	// are the same when no other way to change it exists.
	statements, writes int
	// raise and lower are how far one iteration can move it up and down.
	raise, lower ast.I128
}

// loopProbe is the walk of a loop's body that learns how its accumulators move.
type loopProbe struct {
	accumulated []accumulated
	// times is how often at most the code being walked runs in one iteration of the loop: the trips of the for loops around
	// it inside the body, and pastEveryWord inside a while loop.
	times ast.I128
	// opaque is whether the body holds code that may change a local without a statement that names it.
	opaque bool
	// handlers and frames are what the walk had when the probe began, which it puts back: the probe records its failures and
	// breaks in them as the real walk will.
	handlers []rangeHandler
	frames   []rangeFrame
}

func (p *loopProbe) reset(locals int) {
	p.accumulated = slices.Grow(p.accumulated[:0], locals)[:locals]
	clear(p.accumulated)
	p.times, p.opaque = ast.One, false
}

// record notes an accumulator statement that moves its local by a value of the span e.
func (p *loopProbe) record(a accumulation, e span) {
	delta := e
	if a.subtract {
		delta = span{lo: e.hi.Neg(), hi: e.lo.Neg()}
	}
	acc := &p.accumulated[a.place.root]
	acc.statements++
	acc.raise = capped(acc.raise.Add(scaled(p.times, ast.MaxI(delta.hi, ast.Zero))))
	acc.lower = capped(acc.lower.Add(scaled(p.times, ast.MaxI(delta.lo.Neg(), ast.Zero))))
}

// noteWrite counts a write of a local.
func (p *loopProbe) noteWrite(local ast.LocalID) { p.accumulated[local].writes++ }

// scaleTimes makes the code of a loop run as often as its trips say inside the probed body, and gives what to restore.
func (r *Ranges) scaleTimes(trips ast.I128) ast.I128 {
	if r.probe == nil {
		return ast.Zero
	}
	outer := r.probe.times
	r.probe.times = scaled(outer, trips)
	return outer
}

func (r *Ranges) restoreTimes(outer ast.I128) {
	if r.probe != nil {
		r.probe.times = outer
	}
}

// candidate is an accumulator that a loop may bound, with the span it has where the loop is entered.
type candidate struct {
	local ast.LocalID
	entry span
}

// accumulatorScan finds the locals that a loop's statements assign by an accumulator statement. It reads statements and not
// expressions, which cannot assign, so it is far cheaper than the walk it decides on.
type accumulatorScan struct {
	r     *Ranges
	found []candidate
}

func (c *accumulatorScan) block(body ast.StmtList) {
	for _, s := range body.All() {
		ast.VisitStmt(c, s)
	}
}

func (c *accumulatorScan) VisitAssignStmt(s ast.AssignStmt) {
	a, ok := c.r.accumulation(s)
	if !ok || slices.ContainsFunc(c.found, func(f candidate) bool { return f.local == a.place.root }) {
		return
	}
	c.found = append(c.found, candidate{local: a.place.root, entry: c.r.st.spans[a.place.root]})
}

func (c *accumulatorScan) VisitIfStmt(s ast.IfStmt) {
	c.block(s.Then())
	if s.HasElse() {
		c.block(s.Else())
	}
}

func (c *accumulatorScan) VisitWhileStmt(s ast.WhileStmt) { c.block(s.Body()) }

func (c *accumulatorScan) VisitForStmt(s ast.ForStmt) { c.block(s.Body()) }

func (c *accumulatorScan) VisitTryStmt(s ast.TryStmt) {
	c.block(s.Body())
	if s.HasOtherwise() {
		c.block(s.Handler())
	}
}

func (c *accumulatorScan) VisitMatchStmt(s ast.MatchStmt) {
	for _, arm := range s.Match().Arms().All() {
		if arm.Form() == ast.ArmThen {
			ast.VisitStmt(c, arm.Action())
		}
	}
}

func (*accumulatorScan) VisitLetStmt(ast.LetStmt)           {}
func (*accumulatorScan) VisitDiscardStmt(ast.DiscardStmt)   {}
func (*accumulatorScan) VisitWhenStmt(ast.WhenStmt)         {}
func (*accumulatorScan) VisitReturnStmt(ast.ReturnStmt)     {}
func (*accumulatorScan) VisitThrowStmt(ast.ThrowStmt)       {}
func (*accumulatorScan) VisitBreakStmt(ast.BreakStmt)       {}
func (*accumulatorScan) VisitContinueStmt(ast.ContinueStmt) {}
func (*accumulatorScan) VisitExprStmt(ast.ExprStmt)         {}

// candidates are the accumulators of a loop that the loop's body may bound. None is found in a loop that is walked inside a
// probe, which does not bound anything, and none where the code cannot run.
func (r *Ranges) candidates(body ast.StmtList) []candidate {
	if r.probe != nil || r.fn == nil || r.st.dead {
		return nil
	}
	scan := accumulatorScan{r: r, found: r.scanned[:0]}
	scan.block(body)
	r.scanned = scan.found
	return scan.found
}

// loopHead makes the state at the top of a for loop: what may be assigned is forgotten, except the accumulators of the
// body that the loop bounds.
func (r *Ranges) loopHead(s ast.ForStmt, from, to span, trips ast.I128) {
	cands := r.candidates(s.Body())
	r.forget()
	if len(cands) == 0 {
		return
	}
	probe := r.probeLoop(s, from, to)
	if probe.opaque {
		return
	}
	for _, c := range cands {
		a := probe.accumulated[c.local]
		if a.statements == 0 || a.statements != a.writes {
			continue
		}
		r.bind(c.local, span{lo: c.entry.lo.Sub(scaled(trips, a.lower)), hi: c.entry.hi.Add(scaled(trips, a.raise))})
	}
}

// probeLoop walks the body of a for loop once from the state at its top, and gives what it learned, until the next probe. It
// leaves the walk's state as it found it: the states it made are released, and nothing it recorded outside them is kept,
// because the errors and the Fails of the real walk must come from the real walk alone.
func (r *Ranges) probeLoop(s ast.ForStmt, from, to span) *loopProbe {
	head, written, mark := r.st, len(r.written), r.states.mark()
	probe := &r.probeScratch
	probe.reset(len(r.fn.Locals) + 1)
	probe.handlers = append(probe.handlers[:0], r.handlers...)
	probe.frames = append(probe.frames[:0], r.frames...)
	r.probe = probe
	r.st = r.states.clone(head)
	r.frames = append(r.frames, rangeFrame{node: s.ID})
	r.branch(r.loopBody(s, r.st, from, to), func() { r.block(s.Body()) })
	r.probe = nil
	r.st, r.written = head, r.written[:written]
	r.handlers = append(r.handlers[:0], probe.handlers...)
	r.frames = append(r.frames[:0], probe.frames...)
	r.states.release(mark)
	return probe
}
