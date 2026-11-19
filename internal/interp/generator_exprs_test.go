package interp_test

import (
	"fmt"
	"strings"
)

// Scopes and places

func (g *generator) push() { g.scopes = append(g.scopes, nil) }

func (g *generator) pop() { g.scopes = g.scopes[:len(g.scopes)-1] }

func (g *generator) declare(name string, t *ty, owned, mutable bool) *variable {
	v := &variable{name: name, t: t, owned: owned, mutable: mutable, loop: g.loop}
	g.scopes[len(g.scopes)-1] = append(g.scopes[len(g.scopes)-1], v)
	return v
}

func (g *generator) visible() []*variable {
	var out []*variable
	for _, s := range g.scopes {
		out = append(out, s...)
	}
	return out
}

func (g *generator) moveState() map[*variable]bool {
	state := map[*variable]bool{}
	for _, v := range g.visible() {
		state[v] = v.moved
	}
	return state
}

func (g *generator) restoreMoves(state map[*variable]bool) {
	for v, moved := range state {
		v.moved = moved
	}
}

// mergeMoves marks a variable moved when any path moved it.
func (g *generator) mergeMoves(states ...map[*variable]bool) {
	for _, v := range g.visible() {
		v.moved = false
		for _, s := range states {
			v.moved = v.moved || s[v]
		}
	}
}

// place is an expression that names storage: a variable, or fields of one.
type place struct {
	src      string
	t        *ty
	root     *variable
	mutable  bool
	whole    bool
	elements bool
}

func (g *generator) walk(out []place, p place, want *ty, depth int) []place {
	if p.t == want {
		out = append(out, p)
	}
	if depth == 0 {
		return out
	}
	if p.t.kind == kFrozen {
		return g.walkFrozen(out, p, want, depth)
	}
	r := g.recordOf(p.t)
	if r == nil {
		return out
	}
	for _, f := range r.fields {
		next := place{src: p.src + "." + f.name, t: f.t, root: p.root, mutable: p.mutable && f.isVar}
		out = g.walk(out, next, want, depth-1)
	}
	return out
}

// places are the places of type want that can be read, or changed when mutable is set.
func (g *generator) places(want *ty, mutable bool) []place {
	var out []place
	// Now and then a variable that was moved is read again, to see that the checker notices.
	careless := g.careless()
	for _, v := range g.visible() {
		if v.moved && !careless {
			continue
		}
		if mutable && (!v.mutable || (v.frozen > 0 && !careless)) {
			continue
		}
		root := place{src: v.name, t: v.t, root: v, mutable: v.mutable && (v.frozen == 0 || careless), whole: true}
		for _, p := range g.walk(nil, root, want, 3) {
			if mutable && !p.mutable {
				continue
			}
			out = append(out, p)
		}
	}
	return out
}

func (g *generator) place(want *ty, mutable bool) (place, bool) {
	ps := g.places(want, mutable)
	if len(ps) == 0 {
		return place{}, false
	}
	return ps[g.pick(len(ps))], true
}

// movable are the variables that own a value of type want and may give it away here.
func (g *generator) movable(want *ty) []*variable {
	var out []*variable
	for _, v := range g.visible() {
		if v.owned && !v.moved && v.t == want && ((v.frozen == 0 && v.loop == g.loop) || g.careless()) {
			out = append(out, v)
		}

	}
	return out
}

// Expressions

func (g *generator) literal(n int) string { return fmt.Sprintf("%d", n) }

func (g *generator) countExpr(depth int) string {
	if depth <= 0 || g.chance(40) {
		if p, ok := g.place(tCount, false); ok && g.chance(60) {
			return p.src
		}
		return g.literal(g.pick(10))
	}
	switch n := g.pick(12); {
	case n < 3:
		return fmt.Sprintf("%s + %s", g.countExpr(depth-1), g.countExpr(depth-1))
	case n < 7:
		if c, ok := g.countOfCollection(); ok {
			return c
		}
	case n < 9:
		if c, ok := g.callReturning(tCount, depth); ok {
			return c
		}
	case n < 10:
		return g.condExpr(tCount, depth)
	}
	return g.literal(g.pick(10))
}

func (g *generator) countOfCollection() (string, bool) {
	var found []string
	for _, v := range g.visible() {
		if v.moved {
			continue
		}
		switch v.t.kind {
		case kArray, kPool, kList, kMap:
			found = append(found, v.name+".count()")
		}
	}
	if len(found) == 0 {
		return "", false
	}
	return found[g.pick(len(found))], true
}

func (g *generator) boolExpr(depth int) string {
	if depth <= 0 || g.chance(30) {
		if g.chance(30) {
			return []string{kwTrue, kwFalse}[g.pick(2)]
		}
		if p, ok := g.place(tBool, false); ok && g.chance(60) {
			return p.src
		}
		return fmt.Sprintf("%s < %s", g.countExpr(0), g.countExpr(0))
	}
	switch n := g.pick(8); {
	case n < 3:
		return fmt.Sprintf("%s %s %s", g.countExpr(depth-1), []string{"<", "<=", "==", "!=", ">"}[g.pick(5)], g.countExpr(depth-1))
	case n < 4:
		return fmt.Sprintf(kwNot+" (%s)", g.boolExpr(depth-1))
	case n < 5:
		return fmt.Sprintf("(%s) %s (%s)", g.boolExpr(depth-1), []string{kwAnd, kwOr}[g.pick(2)], g.boolExpr(depth-1))
	case n < 6:
		if c, ok := g.queryExpr(); ok {
			return c
		}
	case n < 7:
		if c, ok := g.callReturning(tBool, depth); ok {
			return c
		}
	}
	return kwTrue
}

// queryExpr is a bool that asks a collection or an option a question.
func (g *generator) queryExpr() (string, bool) {
	var found []string
	for _, v := range g.visible() {
		if v.moved {
			continue
		}
		switch v.t.kind {
		case kArray, kPool, kList, kMap:
			found = append(found, v.name+".isEmpty()")
		case kOption:
			found = append(found, v.name+".isSome()")
		}
	}
	if len(found) == 0 {
		return "", false
	}
	return found[g.pick(len(found))], true
}

func (g *generator) stringExpr(depth int) string {
	switch n := g.pick(10); {
	case n < 3:
		return fmt.Sprintf("\"s%d\"", g.pick(5))
	case n < 5:
		return fmt.Sprintf("\"{%s}x\"", g.countExpr(depth-1))
	}
	return g.owned(tString, depth)
}

// ownedPlaceValue is a value of type t taken from storage, as an owned copy or by moving a whole variable.
func (g *generator) ownedFromStorage(t *ty) (string, bool) {
	if g.careless() {
		// A move out of a place that is not a whole local that owns its value: a field, a borrow, an element.
		if p, ok := g.place(t, false); ok && !p.whole {
			return p.src, true
		}
		for _, v := range g.visible() {
			if v.t == t && !v.owned && g.chance(50) {
				return v.name, true
			}
		}
		if c, ok := g.collectionOf(t); ok {
			if p, ok := g.elementOf(c, false); ok {
				return p.src, true
			}
		}
		// A field of a value that is only a temporary.
		for _, r := range g.records {
			for _, f := range r.fields {
				if f.t == t && r.make != nil && !strings.HasPrefix(r.t.name, "Box") && g.chance(40) {
					return g.recordExpr(r, 1) + "." + f.name, true
				}
			}
		}
	}
	if opts := g.movable(g.option(t)); len(opts) > 0 && t.managed() && g.chance(30) {
		v := opts[g.pick(len(opts))]
		v.moved = true
		return v.name + ".unwrap()", true
	}
	moves := g.movable(t)
	if len(moves) > 0 && g.chance(55) {

		v := moves[g.pick(len(moves))]
		v.moved = true
		return v.name, true
	}
	if p, ok := g.place(t, false); ok {
		return p.src + ".copy()", true
	}
	return "", false
}

// owned is an expression of type t whose value the receiver owns: it is fresh, a copy, or a variable moved.
func (g *generator) owned(t *ty, depth int) string {
	if depth < 0 {
		depth = 0
	}
	if t.kind == kCount {
		return g.countExpr(depth)
	}
	if t.kind == kBool {
		return g.boolExpr(depth)
	}
	if depth > 0 && g.chance(15) {
		if s, ok := g.ownedFromStorage(t); ok {
			return s
		}
	}
	if depth > 0 && g.chance(12) {
		if c, ok := g.callReturning(t, depth); ok {
			return c
		}
	}
	if depth > 0 && g.chance(8) && t.kind != kHandle {
		return g.condExpr(t, depth)
	}
	switch t.kind {
	case kString:
		switch n := g.pick(6); {
		case n < 2:
			return fmt.Sprintf("\"s%d\"", g.pick(5))
		case n < 4:
			return fmt.Sprintf("\"{%s}x\"", g.countExpr(depth-1))
		case n < 5:
			if s, ok := g.ownedFromStorage(t); ok {
				return s
			}
		}
		return fmt.Sprintf("\"s%d\"", g.pick(5))
	case kRecord:
		return g.recordExpr(g.recordOf(t), depth)
	case kUnion:
		return g.unionExpr(g.unionOf(t), depth)
	case kArray:
		switch n := g.pick(6); {
		case n < 2:
			return fmt.Sprintf("%s.new()", g.typeExpr(t))
		case n < 4:
			return fmt.Sprintf("%s.filled(%d, value: %s)", g.typeExpr(t), g.pick(4), g.owned(t.args[0], depth-1))
		}
		if s, ok := g.ownedFromStorage(t); ok {
			return s
		}
		return fmt.Sprintf("%s.new()", g.typeExpr(t))
	case kPool, kList, kMap:
		if g.chance(30) {
			if s, ok := g.ownedFromStorage(t); ok {
				return s
			}
		}
		return fmt.Sprintf("%s.new()", g.typeExpr(t))
	case kOption:
		return g.optionExpr(t, depth)
	case kHandle:
		return g.handleExpr(t, depth)
	case kFrozen, kCown:
		return g.sharedExpr(t, depth)
	}
	panic("owned: unhandled type " + t.src())
}

func (g *generator) recordExpr(r *record, depth int) string {
	if strings.HasPrefix(r.t.name, "Box") {
		return fmt.Sprintf("%s.make(%s)", g.typeExpr(r.t), g.owned(r.fields[0].t, depth-1))
	}
	return g.callArgsText(r.t.name+".make", r.make.params, depth)

}

func (g *generator) unionExpr(u *union, depth int) string {
	c := u.cases[g.pick(len(u.cases))]
	if len(c.payload) == 0 {
		return u.t.name + "." + c.name
	}
	params := make([]param, len(c.payload))
	for i, p := range c.payload {
		params[i] = param{name: p.name, t: p.t, conv: kwConsumes}
	}
	return g.callArgsText(u.t.name+"."+c.name, params, depth)
}

// callArgsText is a call whose arguments are owned values: the first is positional and the others are labelled.
func (g *generator) callArgsText(callee string, params []param, depth int) string {
	parts := make([]string, len(params))
	for i, p := range params {
		v := g.owned(p.t, depth-1)
		if i == 0 {
			parts[i] = v
		} else {
			parts[i] = p.name + ": " + v
		}
	}
	return callee + "(" + strings.Join(parts, ", ") + ")"
}

func (g *generator) optionExpr(t *ty, depth int) string {
	elem := t.args[0]
	switch n := g.pick(8); {
	case n < 2:
		return fmt.Sprintf("%s.none", g.typeExpr(t))
	case n < 4:
		return fmt.Sprintf("%s.some(%s)", g.typeExpr(t), g.owned(elem, depth-1))
	case n < 6:
		if a, ok := g.place(g.array(elem), true); ok {
			return a.src + ".pop()"
		}
	case n < 7:
		if a, ok := g.place(g.array(elem), false); ok {
			return fmt.Sprintf("firstOf(%s)", a.src)
		}
	}
	if s, ok := g.ownedFromStorage(t); ok {
		return s
	}
	return fmt.Sprintf("%s.none", g.typeExpr(t))
}

// handleExpr is the handle of a new element of a pool or a list, or one that a variable already holds.
func (g *generator) handleExpr(t *ty, depth int) string {
	elem := t.args[0]
	if g.chance(40) {
		if p, ok := g.place(t, false); ok {
			return p.src
		}
	}
	if p, ok := g.place(g.pool(elem), true); ok && g.chance(60) {
		return fmt.Sprintf("%s.insert(%s)", p.src, g.owned(elem, depth-1))
	}
	if l, ok := g.place(g.list(elem), true); ok {
		return fmt.Sprintf("%s.%s(%s)", l.src, []string{"pushBack", "pushFront"}[g.pick(2)], g.owned(elem, depth-1))
	}
	if p, ok := g.place(t, false); ok {
		return p.src
	}
	return g.freshHandle(elem, depth)
}

// freshHandle needs a pool to put something in, so it makes one inside an expression it cannot: callers declare it.
func (g *generator) freshHandle(elem *ty, depth int) string {
	return "mint(" + typed(elem, g.owned(elem, depth-1)) + ")"
}

// condExpr is a conditional whose branches give owned values, so a place in one is moved.
func (g *generator) condExpr(t *ty, depth int) string {
	state := g.moveState()
	a := g.owned(t, depth-1)
	stateA := g.moveState()
	g.restoreMoves(state)
	b := g.owned(t, depth-1)
	stateB := g.moveState()
	g.mergeMoves(stateA, stateB)
	if t == tCount {
		// Two constants unify to a plain integer, which does not fit Count without a cast.
		return fmt.Sprintf("((%s "+kwIf+" %s "+kwElse+" %s) "+kwAs+" Count)", a, g.boolExpr(depth-1), b)
	}
	return fmt.Sprintf("(%s "+kwIf+" %s "+kwElse+" %s)", a, g.boolExpr(depth-1), b)

}

// matchExpr is a match over a union or an option that gives a value of type t in each arm. It spans lines, so it is
// only the initial value of a binding.
func (g *generator) matchExpr(t *ty, depth int) string {
	subject, tys, ok := g.matchSubject(depth)
	if !ok {
		return g.owned(t, depth-1)
	}

	label := g.label()
	var arms []string
	state := g.moveState()
	var states []map[*variable]bool
	for _, arm := range tys {
		g.restoreMoves(state)
		g.push()
		pattern, bound := g.armPattern(arm, false)
		for _, b := range bound {
			g.declare(b.name, b.t, false, false)
		}
		arms = append(arms, fmt.Sprintf("%s "+kwGives+" %s", pattern, g.owned(t, depth-1)))
		states = append(states, g.moveState())
		g.pop()
	}
	g.mergeMoves(states...)
	return kwMatch + " " + subject + " " + label + "\n" + g.armLines(arms) + kwEnd + " " + label
}

func (g *generator) armLines(arms []string) string {
	var b strings.Builder
	for _, a := range arms {
		b.WriteString(strings.Repeat("\t", g.indent+1) + a + "\n")
	}
	b.WriteString(strings.Repeat("\t", g.indent))
	return b.String()
}

// arm is one case of a union or an option that a match covers, with the names its payload is bound to.
type arm struct {
	name    string
	payload []field
}

// matchSubject is a place or a fresh value of a union or option type, with the arms that cover it.
func (g *generator) matchSubject(depth int) (string, []arm, bool) {
	var candidates []place
	for _, u := range g.unions {
		candidates = append(candidates, g.places(u.t, false)...)
	}
	for _, v := range g.visible() {
		if v.t.kind == kOption && !v.moved {
			candidates = append(candidates, place{src: v.name, t: v.t, root: v})
		}
		// A frozen union is matched through its contents, which the arms borrow.
		if v.t.kind == kFrozen && !v.moved && g.unionOf(v.t.args[0]) != nil {
			candidates = append(candidates, place{src: v.name, t: v.t.args[0], root: v})
		}
	}
	if len(candidates) == 0 {
		return "", nil, false
	}
	p := candidates[g.pick(len(candidates))]
	return p.src, g.arms(p.t), true
}

func (g *generator) arms(t *ty) []arm {
	if t.kind == kOption {
		return []arm{{name: "some", payload: []field{{name: "v", t: t.args[0]}}}, {name: "none"}}
	}
	var out []arm
	for _, c := range g.unionOf(t).cases {
		out = append(out, arm{name: c.name, payload: c.payload})
	}
	return out
}

// armPattern is a case pattern with a fresh name for each payload field, or an underscore for some.
func (g *generator) armPattern(a arm, allowUnderscore bool) (string, []param) {
	if len(a.payload) == 0 {
		return a.name, nil
	}
	var bound []param
	names := make([]string, len(a.payload))
	for i, f := range a.payload {
		if g.chance(25) {
			names[i] = "_"
			continue
		}
		n := g.fresh("b")
		names[i] = n
		bound = append(bound, param{name: n, t: f.t})
	}
	return a.name + "(" + strings.Join(names, ", ") + ")", bound
}

// Calls

// callReturning is a call of a function that returns t, or a method; its arguments follow the parameters' conventions.
func (g *generator) callReturning(t *ty, depth int) (string, bool) {
	var candidates []*fn
	for _, f := range g.fns {
		if f.ret == t {
			candidates = append(candidates, f)
		}
	}
	if t == tCount {
		if g.chance(30) {
			if s, ok := g.libraryCount(depth); ok {
				return s, true
			}
		}
		if m, ok := g.methodCall(tCount, depth); ok {
			return m, true
		}
	}
	if len(candidates) == 0 {
		return g.libraryCall(t, depth)
	}
	f := candidates[g.pick(len(candidates))]
	return g.callText(f.name, f.params, depth)
}

// callText writes a call with an argument for each parameter, following its convention.
func (g *generator) callText(callee string, params []param, depth int) (string, bool) {
	return g.callTextRoots(callee, params, depth, nil)
}

// callTextRoots is a call whose arguments may reuse the variables of earlier ones, such as the receiver.
func (g *generator) callTextRoots(callee string, params []param, depth int, earlier []*variable) (string, bool) {
	parts := make([]string, len(params))
	roots := append([]*variable(nil), earlier...)
	for i, p := range params {
		arg, ok := g.argument(p, depth, roots)
		if !ok {
			return "", false
		}
		if pl, isPlace := g.lastPlace(); isPlace && pl.root != nil {
			roots = append(roots, pl.root)
		}
		if i == 0 {
			parts[i] = arg
		} else {
			parts[i] = p.name + ": " + arg
		}
	}
	return callee + "(" + strings.Join(parts, ", ") + ")", true
}

// callTextFirst is a call whose first argument is given.
func (g *generator) callTextFirst(f *fn, first string, depth int) (string, bool) {
	parts := []string{first}
	for _, p := range f.params[1:] {
		arg, ok := g.argument(p, depth, nil)
		if !ok {
			return "", false
		}
		parts = append(parts, p.name+": "+arg)
	}
	return f.name + "(" + strings.Join(parts, ", ") + ")", true
}

func (g *generator) lastPlace() (place, bool) { return g.recent, g.recentOK }

// argument is an argument for a parameter: a read for a let, a place that can change for mutates, an owned value for
// consumes. Sometimes it is a place the arguments before it already used, to try the exclusivity rule.
func (g *generator) argument(p param, depth int, earlier []*variable) (string, bool) {
	g.recentOK = false
	switch p.conv {
	case kwMutates:
		ps := g.notSelf(g.places(p.t, true))
		if len(ps) == 0 {
			return "", false
		}
		pl := ps[g.pick(len(ps))]
		if len(earlier) > 0 && g.adversarial() {
			for _, c := range ps {
				if c.root == earlier[g.pick(len(earlier))] {
					pl = c
					break
				}
			}
		}
		g.recent, g.recentOK = pl, true
		return pl.src, true
	case kwConsumes:
		return g.owned(p.t, depth-1), true
	case kwSets:
		// A variable with no value, which the call gives one.
		name := g.fresh("v")
		g.line(kwVar+" %s "+kwAs+" %s", name, p.t.src())
		v := g.declare(name, p.t, true, true)
		v.moved = true
		g.pendingInit = append(g.pendingInit, v)
		return name, true
	}
	if g.chance(60) {
		ps := g.places(p.t, false)
		if len(ps) > 0 {
			pl := ps[g.pick(len(ps))]
			if len(earlier) > 0 && g.adversarial() {
				for _, c := range ps {
					if c.root == earlier[g.pick(len(earlier))] {
						pl = c
						break
					}
				}
			}
			g.recent, g.recentOK = pl, true
			return pl.src, true
		}
	}
	return g.owned(p.t, depth-1), true
}

// methodCall calls a method of a record that returns a Count: one that reads, one that changes, one that consumes.
func (g *generator) methodCall(t *ty, depth int) (string, bool) {
	var recs []*record
	for _, r := range g.records {
		if len(r.methods) > 0 {
			recs = append(recs, r)
		}
	}
	if len(recs) == 0 {
		return "", false
	}
	r := recs[g.pick(len(recs))]
	m := r.methods[g.pick(len(r.methods))]
	if m.ret != t || len(m.params) > 0 {
		return "", false
	}
	switch m.recv {
	case kwConsumes:
		moves := g.movable(r.t)
		if len(moves) == 0 || !g.chance(60) {
			return fmt.Sprintf("%s.%s()", g.recordExpr(r, depth-1), m.name), true
		}
		v := moves[g.pick(len(moves))]
		v.moved = true
		return v.name + "." + m.name + "()", true
	}
	if pl, ok := g.place(r.t, false); ok {
		return pl.src + "." + m.name + "()", true
	}
	return "", false
}

// libraryCount is a call of the library's generic functions that gives a Count.
func (g *generator) libraryCount(depth int) (string, bool) {
	var meters []place
	meters = append(meters, g.places(g.meterType(), false)...)
	if len(meters) > 0 && g.chance(60) {
		return fmt.Sprintf("total(%s)", meters[g.pick(len(meters))].src), true
	}
	if a, ok := g.place(g.array(tCount), true); ok {
		return fmt.Sprintf("shrink(%s)", a.src), true
	}
	if g.sharing {
		for _, v := range g.varsOf(kFrozen) {
			if v.t.args[0] == g.meterType() && !v.moved {
				return fmt.Sprintf("totalFrozen(%s)", v.name), true
			}
		}
	}
	return "", false
}

func (g *generator) meterType() *ty { return g.intern(&ty{kind: kRecord, name: "Meter"}) }

// libraryCall is a call of a library function that gives a value of type t.
func (g *generator) libraryCall(t *ty, depth int) (string, bool) {
	switch {
	case t.kind == kRecord && t.name == "Meter":
		return fmt.Sprintf("Meter.make(%s)", g.countExpr(depth-1)), true
	case t.kind == kOption:
		if a, ok := g.place(g.array(t.args[0]), false); ok {
			return fmt.Sprintf("firstOf(%s)", a.src), true
		}
	}
	if !t.managed() {
		return "", false
	}
	switch g.pick(4) {
	case 0:
		return fmt.Sprintf("keep(%s)", typed(t, g.owned(t, depth-1))), true
	case 1:
		return fmt.Sprintf("pass2(%s, b: %s, first: %s)", typed(t, g.owned(t, depth-1)), typed(t, g.owned(t, depth-1)), g.boolExpr(depth-1)), true
	case 2:
		if p, ok := g.place(t, false); ok {
			return fmt.Sprintf("dup(%s)", p.src), true
		}
	}
	return "", false
}

// careless says whether to break a rule of ownership here, on purpose. Most programs do not, so most are accepted; the
// others break one often, to see that the checker refuses what it must.
func (g *generator) careless() bool { return g.chance(g.carelessPercent) }

// collectionOf is a collection in scope whose elements are of type t.
func (g *generator) collectionOf(t *ty) (*variable, bool) {
	var found []*variable
	for _, v := range g.visible() {
		if v.moved || v.frozen > 0 {
			continue
		}
		switch v.t.kind {
		case kArray, kPool, kList:
			if v.t.args[0] == t {
				found = append(found, v)
			}
		case kMap:
			if v.t.args[1] == t {
				found = append(found, v)
			}
		}
	}
	if len(found) == 0 {
		return nil, false
	}
	return found[g.pick(len(found))], true
}

// notSelf leaves out 'self' itself: a method's receiver cannot be passed as a mutates argument, and a field of it can.
func (g *generator) notSelf(ps []place) []place {
	if g.careless() {
		return ps
	}
	var out []place
	for _, p := range ps {
		if p.src != kwSelf {
			out = append(out, p)
		}
	}
	return out
}

// adversarial says whether to try an overlap that the checker should refuse, in a program that does so.
func (g *generator) adversarial() bool { return g.carelessPercent > 0 && g.chance(40) }
