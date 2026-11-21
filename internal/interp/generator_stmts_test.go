package interp_test

import (
	"fmt"
	"strings"
)

// Statements

// guarded wraps a statement that reads an element, a handle's element or a key in the test that makes it safe, so the
// program does not stop at a trap before it has done anything: a trap is legitimate, but it ends the run early.
func (g *generator) guarded(body func()) {
	saved := g.guards
	g.guards = nil
	var lines []string
	start := len(g.out)
	g.indent++
	g.push()
	body()
	if len(g.out) == start {
		g.line("print(\"-\\n\")")
	}
	g.pop()
	g.indent--
	lines = append(lines, g.out[start:]...)
	g.out = g.out[:start]
	conds := g.guards
	g.guards = saved
	if len(conds) == 0 {
		for _, l := range lines {
			g.out = append(g.out, strings.TrimPrefix(l, "\t"))
		}
		return
	}
	label := g.label()
	g.line(kwIf+" %s %s", strings.Join(dedupe(conds), " "+kwAnd+" "), label)
	g.out = append(g.out, lines...)
	g.line(kwEnd+" %s", label)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// elementOf is a place for an element of a collection variable: it needs the guard that the element exists.
func (g *generator) elementOf(v *variable, mutable bool) (place, bool) {
	p, ok := g.elementPlace(v, mutable)
	if ok {
		g.guards = append(g.guards, p.guards...)
	}
	return p, ok
}

// elementPlace is a place for an element of a collection variable, with the guard that the element exists among its
// guards. Nothing is written for it, so a caller that does not use the place leaves no guard behind.
func (g *generator) elementPlace(v *variable, mutable bool) (place, bool) {
	if v.moved || (mutable && (!v.mutable || v.frozen > 0)) {
		return place{}, false
	}
	whole := place{src: v.name, t: v.t, root: v, mutable: v.mutable && v.frozen == 0, whole: true}
	switch v.t.kind {
	case kArray:
		return whole.inside(v.name+"[0]", v.t.args[0], mutable).element(v.name + ".count() > 0"), true
	case kPool, kList:
		h, ok := g.place(g.handle(v.t.args[0]), false)
		if !ok {
			return place{}, false
		}
		return whole.inside(fmt.Sprintf("%s[%s]", v.name, h.src), v.t.args[0], mutable).element(fmt.Sprintf("%s.contains(%s)", v.name, h.src)), true
	case kMap:
		key := keyOf(v.t.args[0], firstElementKey)
		return whole.inside(fmt.Sprintf("%s[%s]", v.name, key), v.t.args[1], mutable).element(fmt.Sprintf("%s.contains(%s)", v.name, key)), true
	case kFrozen:
		if v.t.args[0].kind == kArray {
			return whole.inside(v.name+"[0]", v.t.args[0].args[0], false).element(v.name + ".count() > 0"), true
		}
	}
	return place{}, false
}

func (g *generator) keyLiteral(k *ty) string { return keyOf(k, g.pick(3)) }

func (g *generator) stmt(depth int) {
	g.budget--
	g.guards = nil

	actions := []struct {
		weight int
		run    func(depth int) bool
	}{
		{10, g.actDeclare},
		{8, g.actAssign},
		{7, g.actCall},
		{5, g.actPrint},
		{6, g.actMove},
		{4, g.actIf},
		{3, g.actTry},
		{3, g.actWhile},
		{3, g.actFor},
		{5, g.actMatch},
		{9, g.actArray},
		{5, g.actPool},
		{4, g.actList},
		{4, g.actMap},
		{3, g.actReturn},
		{3, g.actLoopJump},
		{3, g.actElement},
		{3, g.actBox},
		{2, g.actDiscard},
		{3, g.actMatchLet},
		{4, g.actBoth},
		{3, g.actTargetHold},
		{2, g.actSet},

		{4, g.actBoth},
		{3, g.actTargetHold},
		{2, g.actSet},

		{2, g.actUninit},
		{3, g.actMethod},
		{3, g.actGeneric},
		{2, g.actFieldAssign},

		{3, g.actFreeze},
		{3, g.actShare},
		{2, g.actThaw},
		{3, g.actCown},
		{8, g.actWhen},
		{2, g.actFrozenMethod},
		{1, g.actCownPeek},
		{2, g.actBorrowedCapture},
		{2, g.actUseAfterCapture},
		{2, g.actFrozenAbuse},
		{3, g.actHeldCompare},
		{4, g.actHeldIndex},
		{3, g.actHeldInterpolation},
	}
	if g.sinsLeft > 0 && (g.inMain || !g.heldSin) && g.chance(targetedChance) {
		sins := []func(int) bool{g.actBorrowedCapture, g.actUseAfterCapture, g.actFrozenAbuse, g.actFrozenMethod, g.actCownPeek}
		if g.heldSin {
			sins = []func(int) bool{g.actHeldCompare, g.actHeldIndex, g.actHeldInterpolation}
		}
		g.sinning = true
		for _, i := range g.r.Perm(len(sins)) {
			if g.attempt(sins[i], depth) {
				g.sinning = false
				g.sinsLeft--
				return
			}
		}
		g.sinning = false
	}
	total := 0
	for _, a := range actions {
		total += a.weight
	}
	for range 8 {
		n := g.pick(total)
		for _, a := range actions {
			if n < a.weight {
				if g.attempt(a.run, depth) {
					return
				}
				break
			}
			n -= a.weight
		}
	}
	g.line("print(\"-\\n\")")
}

// targetedChance is how often a statement of a targeted program tries to be its one sin.
const targetedChance = 12

// attempt writes a statement, or takes back what it wrote and says it could not.
func (g *generator) attempt(run func(depth int) bool, depth int) bool {
	mark := len(g.out)
	state := g.moveState()
	top := len(g.scopes) - 1
	declared := len(g.scopes[top])
	g.pendingInit = nil
	if run(depth) {
		for _, v := range g.pendingInit {
			v.moved = false
		}
		g.pendingInit = nil
		return true
	}
	g.out = g.out[:mark]
	g.scopes[top] = g.scopes[top][:declared]
	g.restoreMoves(state)
	return false
}

// nonEmptyBlock writes the statements of a block, at least one.
func (g *generator) nonEmptyBlock(count, depth int) {
	g.indent++
	g.push()
	n := 1 + g.pick(count)
	start := len(g.out)
	for i := 0; i < n && g.budget > 0; i++ {
		g.stmt(depth)
	}
	if len(g.out) == start {
		g.line("print(\".\\n\")")
	}
	g.pop()
	g.indent--
}

func (g *generator) actDeclare(depth int) bool {
	t := g.anyType()
	if t.kind == kHandle {
		return false
	}
	name := g.fresh("v")
	isVar := g.chance(55)
	kw := kwLet
	if isVar {
		kw = kwVar
	}
	init := g.owned(t, 2)
	if t.kind == kCount {
		init = g.literal(g.pick(10)) + " " + kwAs + " Count"
	}
	g.line("%s %s = %s", kw, name, init)
	g.declare(name, t, true, isVar)
	return true
}

func (g *generator) actUninit(depth int) bool {
	t := g.anyType()
	if t.kind == kHandle {
		return false
	}
	name := g.fresh("v")
	g.line(kwVar+" %s "+kwAs+" %s", name, t.src())
	v := g.declare(name, t, true, true)
	v.moved = true
	label := g.label()
	label2 := g.label()
	g.line(kwIf+" %s %s", g.boolExpr(1), label)
	state := g.moveState()
	g.indent++
	g.line("%s = %s", name, g.owned(t, 1))
	g.indent--
	stateA := g.moveState()
	g.restoreMoves(state)
	g.line(kwEnd+" %s "+kwElse+" %s", label, label2)
	g.indent++
	g.line("%s = %s", name, g.owned(t, 1))
	g.indent--
	g.line(kwEnd+" %s", label2)
	stateB := g.moveState()
	g.mergeMoves(stateA, stateB)
	v.moved = false
	return true
}

func (g *generator) actAssign(depth int) bool {
	t := g.anyType()
	if t.kind == kHandle {
		return false
	}
	p, ok := g.place(t, true)
	if !ok || p.src == kwSelf {
		return false
	}
	value := g.owned(t, 2)
	g.line("%s = %s", p.src, value)
	if p.whole {
		p.root.moved = false
	}
	return true
}

func (g *generator) actFieldAssign(depth int) bool {
	var recs []place
	for _, r := range g.records {
		if len(r.fields) > 0 {
			recs = append(recs, g.places(r.t, true)...)
		}
	}
	if len(recs) == 0 {
		return false
	}
	p := recs[g.pick(len(recs))]
	r := g.recordOf(p.t)
	var vars []field
	for _, f := range r.fields {
		if f.isVar {
			vars = append(vars, f)
		}
	}
	if len(vars) == 0 {
		return false
	}
	f := vars[g.pick(len(vars))]
	g.line("%s.%s = %s", p.src, f.name, g.owned(f.t, 2))
	return true
}

func (g *generator) actMove(depth int) bool {
	var movable []*variable
	for _, v := range g.visible() {
		if v.owned && !v.moved && v.frozen == 0 && v.loop == g.loop && v.t.managed() {
			movable = append(movable, v)
		}
	}
	if len(movable) == 0 {
		return false
	}
	v := movable[g.pick(len(movable))]
	name := g.fresh("v")
	if g.chance(30) {
		g.line(kwLet+" %s = %s.clone()", name, v.name)
	} else {
		g.line(kwLet+" %s = %s", name, v.name)
		v.moved = true
	}
	g.declare(name, v.t, true, false)
	return true
}

func (g *generator) actPrint(depth int) bool {
	switch g.pick(3) {
	case 0:
		if p, ok := g.place(tString, false); ok {
			g.line("print(\"{%s}\\n\")", p.src)
			return true
		}
	case 1:
		if p, ok := g.place(tBool, false); ok {
			g.line("print(\"{%s}\\n\")", p.src)
			return true
		}
	}
	g.line("print(\"{%s}\\n\")", g.countExpr(2))
	return true
}

func (g *generator) actDiscard(depth int) bool {
	t := g.anyType()
	if t.kind == kHandle || t.scalar() {
		return false
	}
	g.line("_ = %s", g.owned(t, 2))
	return true
}

func (g *generator) actCall(depth int) bool {
	var candidates []*fn
	for _, f := range g.fns {
		candidates = append(candidates, f)
	}
	if len(candidates) == 0 {
		return false
	}
	f := candidates[g.pick(len(candidates))]
	text, ok := g.callText(f.name, f.params, 2)
	if !ok {
		return false
	}
	if f.ret != nil {
		g.line("_ = %s", text)
	} else {
		g.line("%s", text)
	}
	return true
}

// actMatchLet binds the value of a match, whose arms each give an owned value.
func (g *generator) actMatchLet(depth int) bool {
	t := g.anyType()
	if t.kind == kHandle || depth <= 0 {
		return false
	}
	name := g.fresh("v")
	text := g.matchExpr(t, 2)
	if !strings.HasPrefix(text, kwMatch+" ") {
		return false
	}
	g.line(kwLet+" %s = %s", name, text)
	g.declare(name, t, true, false)
	return true
}

func (g *generator) actMethod(depth int) bool {
	var recs []*record
	for _, r := range g.records {
		if len(r.methods) > 0 {
			recs = append(recs, r)
		}
	}
	if len(recs) == 0 {
		return false
	}
	r := recs[g.pick(len(recs))]
	m := r.methods[g.pick(len(r.methods))]
	var receiver string
	var root *variable
	switch m.recv {
	case kwMutates:
		p, ok := g.place(r.t, true)
		if !ok {
			return false
		}
		receiver, root = p.src, p.root
	case kwConsumes:
		moves := g.movable(r.t)
		if len(moves) == 0 {
			return false
		}
		v := moves[g.pick(len(moves))]
		v.moved = true
		receiver, root = v.name, v
	default:
		p, ok := g.place(r.t, false)
		if !ok {
			return false
		}
		receiver, root = p.src, p.root
	}
	text, ok := g.callTextRoots(receiver+"."+m.name, m.params, 2, []*variable{root})
	if !ok {
		return false
	}
	if m.ret != nil {
		g.line("_ = %s", text)
	} else {
		g.line("%s", text)
	}
	return true
}

func (g *generator) actIf(depth int) bool {
	if depth <= 0 {
		return false
	}
	label := g.label()
	g.line(kwIf+" %s %s", g.boolExpr(2), label)
	state := g.moveState()
	g.nonEmptyBlock(3, depth-1)
	stateA := g.moveState()
	if g.chance(50) {
		g.mergeMoves(stateA, state)
		g.line(kwEnd+" %s", label)
		return true
	}
	g.restoreMoves(state)
	label2 := g.label()
	g.line(kwEnd+" %s "+kwElse+" %s", label, label2)
	g.nonEmptyBlock(3, depth-1)
	g.line(kwEnd+" %s", label2)
	stateB := g.moveState()
	g.mergeMoves(stateA, stateB)
	return true
}

// actTry writes a try block, whose body may hold bare arithmetic, and its handler. The handler starts wherever the body
// failed, so it takes what the body moved as moved.
func (g *generator) actTry(depth int) bool {
	if depth <= 0 {
		return false
	}
	label := g.label()
	g.line(kwTry+" %s", label)
	state := g.moveState()
	g.tries++
	g.nonEmptyBlock(3, depth-1)
	g.tries--
	body := g.moveState()
	g.mergeMoves(state, body)
	handler := g.label()
	g.line(kwEnd+" %s "+kwOtherwise+" %s", label, handler)
	g.nonEmptyBlock(2, depth-1)
	g.line(kwEnd+" %s", handler)
	g.mergeMoves(body, g.moveState())
	return true
}

func (g *generator) actWhile(depth int) bool {
	if depth <= 0 {
		return false
	}
	counter := g.fresh("v")
	g.line(kwVar+" %s = 0 "+kwAs+" Count", counter)
	g.declare(counter, tCount, true, false)
	label := g.label()
	g.line(kwWhile+" %s < %d %s", counter, 1+g.pick(3), label)
	g.indent++
	g.line("%s = %s + 1", counter, counter)
	g.indent--
	g.loop++
	g.loopTags = append(g.loopTags, label)
	g.nonEmptyBlock(3, depth-1)
	g.loopTags = g.loopTags[:len(g.loopTags)-1]
	g.loop--
	g.line(kwEnd+" %s", label)
	return true
}

func (g *generator) actFor(depth int) bool {
	if depth <= 0 {
		return false
	}
	label := g.label()
	index := g.fresh("i")
	bound := fmt.Sprintf("%d", 1+g.pick(3))
	for _, v := range g.visible() {
		if (v.t.kind == kArray) && !v.moved && g.chance(40) {
			bound = v.name + ".count()"
		}
	}
	g.line(kwFor+" %s "+kwIn+" 0 "+kwUpto+" %s %s", index, bound, label)
	g.loop++
	g.loopTags = append(g.loopTags, label)
	g.push()
	g.declare(index, tCount, false, false)
	g.indent++
	g.push()
	n := 1 + g.pick(3)
	start := len(g.out)
	for i := 0; i < n && g.budget > 0; i++ {
		g.stmt(depth - 1)
	}
	if len(g.out) == start {
		g.line("print(\"{%s}\\n\")", index)
	}
	g.pop()
	g.indent--
	g.pop()
	g.loopTags = g.loopTags[:len(g.loopTags)-1]
	g.loop--
	g.line(kwEnd+" %s", label)
	return true
}

func (g *generator) actLoopJump(depth int) bool {
	if g.loop == 0 {
		return false
	}
	label := g.label()
	g.line(kwIf+" %s %s", g.boolExpr(1), label)
	g.indent++
	if g.chance(50) {
		g.line(kwBreak)
	} else {
		g.line(kwContinue)
	}
	g.indent--
	g.line(kwEnd+" %s", label)
	return true
}

func (g *generator) actReturn(depth int) bool {
	if depth <= 0 || g.ret == nil || g.loop > 0 && g.chance(50) {
		return false
	}
	label := g.label()
	g.line(kwIf+" %s %s", g.boolExpr(1), label)
	state := g.moveState()
	g.indent++
	g.line(kwReturn+" %s", g.owned(g.ret, 2))
	g.indent--
	g.restoreMoves(state)
	g.line(kwEnd+" %s", label)
	return true
}

func (g *generator) actMatch(depth int) bool {
	subject, arms, ok := g.matchSubject(depth)
	if !ok || depth <= 0 {
		return false
	}
	var root *variable
	for _, v := range g.visible() {
		if strings.HasPrefix(subject, v.name) && (len(subject) == len(v.name) || subject[len(v.name)] == '.') {
			root = v
		}
	}
	g.matchOn(subject, root, arms)
	return true
}

// matchOn writes a match over the subject, whose arms only borrow from the variable it lies in.
func (g *generator) matchOn(subject string, root *variable, arms []arm) {
	label := g.label()
	g.line(kwMatch+" %s %s", subject, label)
	if root != nil {
		root.frozen++
	}
	state := g.moveState()
	var states []map[*variable]bool
	g.indent++
	for _, a := range arms {
		g.restoreMoves(state)
		g.push()
		pattern, bound := g.armPattern(a, true)
		for _, b := range bound {
			g.declare(b.name, b.t, false, false)
		}
		g.line("%s "+kwThen+" %s", pattern, g.armAction(bound))
		states = append(states, g.moveState())
		g.pop()
	}
	g.indent--
	g.mergeMoves(states...)
	if root != nil {
		root.frozen--
	}
	g.line(kwEnd+" %s", label)
}

// armAction is a single statement for a match arm: what a binding can be used for.
func (g *generator) armAction(bound []param) string {
	for _, b := range g.shuffled(bound) {
		switch {
		case b.t == tCount:
			return fmt.Sprintf("print(\"{%s}\\n\")", b.name)
		case b.t == tString:
			return fmt.Sprintf("print(\"{%s}\\n\")", b.name)
		case b.t.managed() && g.chance(50):
			if target, ok := g.place(b.t, true); ok && target.src != kwSelf {
				return fmt.Sprintf("%s = %s.clone()", target.src, b.name)
			}
			return fmt.Sprintf("_ = %s.clone()", b.name)
		}
	}
	return "print(\"arm\\n\")"
}

func (g *generator) shuffled(in []param) []param {
	out := append([]param(nil), in...)
	g.r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// Collections

func (g *generator) collectionVar(kinds ...kind) (*variable, bool) {
	var found []*variable
	for _, v := range g.visible() {
		if v.moved || v.frozen > 0 {
			continue
		}
		for _, k := range kinds {
			if v.t.kind == k {
				found = append(found, v)
			}
		}
	}
	if len(found) == 0 {
		return nil, false
	}
	return found[g.pick(len(found))], true
}

func (g *generator) actArray(depth int) bool {
	v, ok := g.collectionVar(kArray)
	if !ok {
		return false
	}
	elem := v.t.args[0]
	mutable := v.mutable
	switch n := g.pick(9); {
	case n < 2 && mutable:
		g.line("%s.push(%s)", v.name, g.owned(elem, 2))
	case n < 3 && mutable:
		g.line("%s.insert(%s, value: %s)", v.name, []string{"0", v.name + ".count()"}[g.pick(2)], g.owned(elem, 2))
	case n < 4 && mutable:
		g.guarded(func() {
			g.guards = append(g.guards, v.name+".count() > 0")
			if g.chance(50) {
				g.line("try %s.remove(0) otherwise ignore", v.name)
			} else {
				name := g.fresh("v")
				g.line(kwLet+" %s = try %s.remove(0) otherwise panic(\"index out of range\")", name, v.name)
				g.declare(name, elem, true, false)
			}
		})
	case n < 5 && mutable:
		name := g.fresh("v")
		g.line(kwLet+" %s = try %s.pop() otherwise %s", name, v.name, g.owned(elem, 2))
		g.declare(name, elem, true, false)
	case n < 6:
		g.guarded(func() { g.elementStatement(v) })
	case n < 7:
		g.line("print(\"{%s.count()}\\n\")", v.name)
	case n < 8 && mutable:
		g.line("put(%s, x: %s)", v.name, g.owned(elem, 2))
	default:
		name := g.fresh("v")
		g.line(kwLet+" %s = %s.clone()", name, v.name)
		g.declare(name, v.t, true, false)
	}
	return true
}

// elementStatement does something with the first element of an array, which the caller guards.
func (g *generator) elementStatement(v *variable) {
	elem := v.t.args[0]
	p, _ := g.elementOf(v, v.mutable)
	switch n := g.pick(6); {
	case n < 1 && elem.scalar() && v.mutable:
		g.line("%s = %s", p.src, g.owned(elem, 1))
	case n < 2 && elem.managed() && v.mutable:
		g.line("%s = %s", p.src, g.owned(elem, 1))
	case n < 3 && elem.managed():
		name := g.fresh("v")
		g.line(kwLet+" %s = %s.clone()", name, p.src)
		g.declare(name, elem, true, false)
	case n < 4 && v.mutable && elem.managed():
		g.line("overwrite(%s, value: %s)", p.src, g.owned(elem, 1))
	case n < 5 && elem == tCount:
		g.line("print(\"{%s}\\n\")", p.src)
	default:
		if r := g.recordOf(elem); r != nil && v.mutable {
			for _, f := range r.fields {
				if f.isVar && f.t.scalar() && f.t == tCount {
					g.line("%s.%s = %s", p.src, f.name, g.countExpr(1))
					return
				}
			}
		}
		g.line("_ = %s.clone()", p.src)
	}
}

func (g *generator) actElement(depth int) bool {
	// An element of one collection passed with an argument that may change another, or the same one.
	v, ok := g.collectionVar(kArray, kPool, kList, kMap)
	if !ok {
		return false
	}
	elem := v.t.args[len(v.t.args)-1]
	if elem.kind == kHandle {
		return false
	}
	g.guarded(func() {
		p, ok := g.elementOf(v, false)
		if !ok {
			g.line("print(\"-\\n\")")
			return
		}
		if elem == tCount || elem == tString {
			g.line("print(\"{%s}\\n\")", p.src)
			return
		}
		for _, f := range g.fns {
			if len(f.params) >= 1 && f.params[0].t == elem && f.params[0].conv == "" {
				if text, ok := g.callTextFirst(f, p.src, 2); ok {
					if f.ret != nil {
						g.line("_ = %s", text)
					} else {
						g.line("%s", text)
					}
					return
				}
			}
		}
		g.line("_ = %s.clone()", p.src)
	})
	return true
}

func (g *generator) actPool(depth int) bool {
	v, ok := g.collectionVar(kPool)
	if !ok || !v.mutable {
		return false
	}
	elem := v.t.args[0]
	switch n := g.pick(6); {
	case n < 2:
		name := g.fresh("h")
		g.line(kwLet+" %s = %s.insert(%s)", name, v.name, g.owned(elem, 2))
		g.declare(name, g.handle(elem), false, false)
	case n < 3:
		if h, ok := g.place(g.handle(elem), false); ok {
			name := g.fresh("v")
			g.line(kwLet+" %s = %s.remove(%s)", name, v.name, h.src)
			g.declare(name, g.option(elem), true, false)
			return true
		}
		return false
	case n < 4:
		g.guarded(func() { g.elementStatement2(v) })
	case n < 5:
		if h, ok := g.place(g.handle(elem), false); ok {
			g.line("print(\"{%s.contains(%s)}\\n\")", v.name, h.src)
			return true
		}
		return false
	default:
		g.line("print(\"{%s.count()}\\n\")", v.name)
	}
	return true
}

// elementStatement2 does something with the element a handle names in a pool, a list or a map, which the caller guards.
func (g *generator) elementStatement2(v *variable) {
	elem := v.t.args[len(v.t.args)-1]
	p, ok := g.elementOf(v, v.mutable)
	if !ok {
		g.line("print(\"-\\n\")")
		return
	}
	switch n := g.pick(4); {
	case n < 1 && v.mutable:
		g.line("%s = %s", p.src, g.owned(elem, 1))
	case n < 2 && elem.managed():
		name := g.fresh("v")
		g.line(kwLet+" %s = %s.clone()", name, p.src)
		g.declare(name, elem, true, false)
	case n < 3 && elem == tCount:
		g.line("print(\"{%s}\\n\")", p.src)
	default:
		g.line("_ = %s.clone()", p.src)
	}
}

func (g *generator) actList(depth int) bool {
	v, ok := g.collectionVar(kList)
	if !ok || !v.mutable {
		return false
	}
	elem := v.t.args[0]
	switch n := g.pick(8); {
	case n < 2:
		name := g.fresh("h")
		g.line(kwLet+" %s = %s.%s(%s)", name, v.name, []string{"append", "prepend"}[g.pick(2)], g.owned(elem, 2))
		g.declare(name, g.handle(elem), false, false)
	case n < 3:
		name := g.fresh("v")
		g.line(kwLet+" %s = try %s.%s() otherwise %s", name, v.name, []string{"removeFirst", "removeLast"}[g.pick(2)], g.owned(elem, 2))
		g.declare(name, elem, true, false)
	case n < 4:
		if h, ok := g.place(g.handle(elem), false); ok {
			name := g.fresh("v")
			g.line(kwLet+" %s = %s.remove(%s)", name, v.name, h.src)
			g.declare(name, g.option(elem), true, false)
			return true
		}
		return false
	case n < 5:
		if h, ok := g.place(g.handle(elem), false); ok {
			g.line("%s.moveToFront(%s)", v.name, h.src)
			return true
		}
		return false
	case n < 6:
		g.guarded(func() { g.elementStatement2(v) })
	case n < 7:
		name := g.fresh("v")
		g.guarded(func() {
			g.guards = append(g.guards, "not "+v.name+".isEmpty()")
			g.line(kwLet+" %s = try %s.first() otherwise panic(\"the list is empty\")", name, v.name)
			g.declare(name, g.handle(elem), false, false)
		})
	default:
		g.line("print(\"{%s.count()}\\n\")", v.name)
	}
	return true
}

func (g *generator) actMap(depth int) bool {
	v, ok := g.collectionVar(kMap)
	if !ok || !v.mutable {
		return false
	}
	key, value := v.t.args[0], v.t.args[1]
	switch n := g.pick(6); {
	case n < 2:
		g.line("%s.upsert(%s, value: %s)", v.name, g.keyLiteral(key), g.owned(value, 2))
	case n < 3:
		name := g.fresh("v")
		g.line(kwLet+" %s = %s.remove(%s)", name, v.name, g.keyLiteral(key))
		g.declare(name, tBool, true, false)
	case n < 4:
		g.guarded(func() { g.elementStatement2(v) })
	case n < 5:
		g.line("print(\"{%s.contains(%s)}\\n\")", v.name, g.keyLiteral(key))
	default:
		g.line("print(\"{%s.count()}\\n\")", v.name)
	}
	return true
}

func (g *generator) actBox(depth int) bool {
	var boxes []place
	for _, r := range g.records {
		if strings.HasPrefix(r.t.name, "Box") {
			boxes = append(boxes, g.places(r.t, true)...)
		}
	}
	if len(boxes) == 0 {
		return false
	}
	p := boxes[g.pick(len(boxes))]
	item := g.recordOf(p.t).fields[0].t
	switch g.pick(3) {
	case 0:
		g.line("%s.replace(%s)", p.src, g.owned(item, 1))
	case 1:
		g.line("_ = %s.peek()", p.src)
	default:
		g.line("%s.item = %s", p.src, g.owned(item, 1))
	}
	return true
}

func (g *generator) actGeneric(depth int) bool {
	switch g.pick(4) {
	case 0:
		if m, ok := g.place(g.meterType(), true); ok && m.src != kwSelf {
			g.line("growTwice(%s)", m.src)
			return true
		}
	case 1:
		t := g.anyType()
		if t.kind == kHandle {
			return false
		}
		a, ok := g.place(t, true)
		if !ok || a.src == kwSelf {
			return false
		}
		g.line("overwrite(%s, value: %s)", a.src, g.owned(t, 1))
		return true
	case 2:
		t := g.anyType()
		if t.kind == kHandle || !t.managed() {
			return false
		}
		name := g.fresh("v")
		g.line(kwLet+" %s = keep(%s)", name, typed(t, g.owned(t, 2)))
		g.declare(name, t, true, false)
		return true
	default:
		t := g.anyType()
		if t.kind == kHandle {
			return false
		}
		name := g.fresh("v")
		g.line(kwLet+" %s = pass2(%s, b: %s, first: %s)", name, typed(t, g.owned(t, 1)), typed(t, g.owned(t, 1)), g.boolExpr(1))
		g.declare(name, t, true, false)
		return true
	}
	return false
}

// Functions and the program

func (g *generator) function(index int) {
	f := &fn{name: fmt.Sprintf("fn%d", index)}
	if g.chance(60) {
		f.ret = g.anyType()
		if f.ret.kind == kHandle {
			f.ret = tCount
		}
	}
	nparams := g.pick(4)
	var parts []string
	g.scopes = nil
	g.push()
	var sets []*variable
	candidates := []*ty{g.anyType(), g.anyType()}
	for i := range nparams {
		t := candidates[g.pick(len(candidates))]
		if t.kind == kHandle {
			t = tCount
		}
		conv := []string{"", "", kwMutates, kwConsumes, kwSets}[g.pick(5)]
		if t.scalar() && conv == kwConsumes {
			conv = ""
		}
		p := param{name: fmt.Sprintf("a%d", i), t: t, conv: conv}
		f.params = append(f.params, p)
		text := ""
		if conv != "" {
			text = conv + " "
		}
		parts = append(parts, text+p.name+" "+t.src())
		v := g.declare(p.name, t, conv == kwConsumes || conv == kwSets, conv == kwMutates || conv == kwSets)
		v.setParam = conv == kwSets
		if conv == kwSets {
			// A sets parameter starts with no value, and the function gives it one before it does anything else, except in
			// a program that is careless about it.
			v.moved = true
			sets = append(sets, v)
		}
	}
	header := fmt.Sprintf(kwFunction+" %s(%s)", f.name, strings.Join(parts, ", "))
	if f.ret != nil {
		header += " " + kwReturns + " " + f.ret.src()
	}
	g.line("%s", header)
	g.ret = f.ret
	g.loop = 0
	g.budget = 14
	g.indent++
	g.push()
	for _, v := range sets {
		if !g.careless() {
			g.line("%s = %s", v.name, g.owned(v.t, 1))
			v.moved = false
		}
	}
	if g.chance(60) {
		g.preamble()
	}
	for g.budget > 0 {
		g.stmt(3)
	}
	if f.ret != nil {
		g.line(kwReturn+" %s", g.owned(f.ret, 2))
	}
	g.pop()
	g.indent--
	g.line(kwEnd+" '%s'", f.name)
	g.blank()
	g.pop()
	g.fns = append(g.fns, f)
}

func (g *generator) mainFunction() {
	g.scopes = nil
	g.push()
	g.line(kwFunction + " main() " + kwReturns + " ExitCode")
	g.inMain = true
	g.ret = nil
	g.loop = 0
	g.budget = 40
	g.indent++
	g.push()
	g.preamble()
	for g.budget > 0 {
		g.stmt(3)
	}
	g.line(kwReturn + " 0")
	g.pop()
	g.indent--
	g.line(kwEnd + " 'main'")
}

// preamble declares what the statements work on: variables of the program's own types and a collection of each kind with
// something in it, so a statement that needs one has one.
func (g *generator) preamble() {
	managed := g.managedTypes()
	for i, n := 0, 2+g.pick(3); i < n; i++ {
		t := managed[g.pick(len(managed))]
		name := g.fresh("v")
		g.line(kwVar+" %s = %s", name, g.owned(t, 2))
		g.declare(name, t, true, true)
	}
	elem := g.elementType()
	name := g.fresh("v")
	arr := g.array(elem)
	g.line(kwVar+" %s = %s.create(0)", name, g.typeExpr(arr))
	g.declare(name, arr, true, true)
	g.line("%s.push(%s)", name, g.owned(elem, 2))
	g.line("%s.push(%s)", name, g.owned(elem, 2))
	if g.chance(80) && !g.arraysOnly {
		elem = g.elementType()
		p := g.fresh("v")
		pt := g.pool(elem)
		g.line(kwVar+" %s = %s.create()", p, g.typeExpr(pt))
		g.declare(p, pt, true, true)
		h := g.fresh("h")
		g.line(kwLet+" %s = %s.insert(%s)", h, p, g.owned(elem, 2))
		g.declare(h, g.handle(elem), false, false)
	}
	if g.chance(70) && !g.arraysOnly {
		elem = g.elementType()
		l := g.fresh("v")
		lt := g.list(elem)
		g.line(kwVar+" %s = %s.create()", l, g.typeExpr(lt))
		g.declare(l, lt, true, true)
		h := g.fresh("h")
		g.line(kwLet+" %s = %s.append(%s)", h, l, g.owned(elem, 2))
		g.declare(h, g.handle(elem), false, false)
	}
	if g.chance(70) && !g.arraysOnly {
		key, elem := g.keyType(), g.elementType()
		m := g.fresh("v")
		mt := g.mapOf(key, elem)
		g.line(kwVar+" %s = %s.create()", m, g.typeExpr(mt))
		g.declare(m, mt, true, true)
		g.line("%s.upsert(%s, value: %s)", m, keyOf(key, firstElementKey), g.owned(elem, 2))
	}
	g.sharingPreamble()
}

// carelessPrograms is the share of programs that break the rules of ownership on purpose.
const carelessPrograms = 40

// arraysOnlyPrograms is the share of programs with no map, list or pool, which can also run as executables.
const arraysOnlyPrograms = 30

// targetedPrograms is the share of the programs that are not careless which break one rule of sharing, once.
const targetedPrograms = 40

// Program writes the source of one random program for the seed.

func Program(seed int64) string {
	g := newGenerator(seed)
	if g.chance(carelessPrograms) {
		g.carelessPercent = 2 + g.pick(6)
	}
	g.arraysOnly = g.chance(arraysOnlyPrograms)
	if g.carelessPercent == 0 && g.chance(targetedPrograms) {
		g.sinsLeft = 1
		g.heldSin = g.chance(heldFamilyPercent)
	}
	g.sharing = !g.arraysOnly
	g.declareTypes()
	g.writeTypes()
	for i, n := 0, 1+g.pick(4); i < n; i++ {
		g.function(i)
	}
	g.mainFunction()
	return strings.TrimRight(library, "\n") + "\n\n" + g.aliasDecls() + strings.Join(g.out, "\n") + "\n"
}

// anyPlace is a place of any managed type that is in scope, to be borrowed.
func (g *generator) anyPlace(mutable bool) (place, bool) {
	var found []place
	for _, v := range g.visible() {
		if v.moved || !v.t.managed() || (mutable && !v.mutable) {
			continue
		}
		if v.name != kwSelf || !mutable {
			found = append(found, place{src: v.name, t: v.t, root: v, whole: true})
		}
		if r := g.recordOf(v.t); r != nil {
			for _, f := range r.fields {
				if f.t.managed() && (!mutable || f.isVar) {
					found = append(found, place{src: v.name + "." + f.name, t: f.t, root: v})
				}
			}
		}
	}
	if len(found) == 0 {
		return place{}, false
	}
	return found[g.pick(len(found))], true
}

// actBoth passes a borrowed place and a place that is changed to a generic function that reads the first after it
// changed the second: a borrow into what is changed is a use after free. The checker has to see the overlap.
func (g *generator) actBoth(depth int) bool {
	if v, found := g.collectionVar(kArray, kPool, kList, kMap); found && g.chance(40) {
		g.guarded(func() {
			p, okElement := g.elementOf(v, false)
			if !okElement || !p.t.managed() {
				g.line("print(\"-\\n\")")
				return
			}
			g.bothWith(p)
		})
		return true
	}
	borrowed, ok := g.anyPlace(false)
	if !ok {
		return false
	}
	return g.bothWith(borrowed)
}

func (g *generator) bothWith(borrowed place) bool {
	changed, ok := g.anyPlace(!g.careless())
	if !ok {
		return false
	}
	if g.adversarial() {
		if root, found := g.collectionOf(borrowed.t); found && root.mutable {
			changed = place{src: root.name, t: root.t, root: root}
		}
	}
	if borrowed.root == changed.root && !g.adversarial() {
		return false
	}
	if g.chance(50) {
		g.line("both(%s, b: %s)", borrowed.src, changed.src)
	} else {
		g.line("both2(%s, b: %s)", borrowed.src, typed(changed.t, g.owned(changed.t, 1)))
	}
	return true
}

// actSet declares a variable with no value and has a function that takes it as a sets parameter initialise it.
func (g *generator) actSet(depth int) bool {
	t := g.anyType()
	if t.kind == kHandle {
		return false
	}
	p, ok := g.place(t, false)
	if !ok {
		return false
	}
	name := g.fresh("v")
	g.line(kwVar+" %s "+kwAs+" %s", name, t.src())
	g.line("fill(%s, from: %s)", name, p.src)
	g.declare(name, t, true, true)
	return true
}

// actTargetHold assigns to a part of an element with a value that changes the collection: the address of the element is
// taken before the value is computed, so the store would land where the element used to be.
func (g *generator) actTargetHold(depth int) bool {
	v, ok := g.collectionVar(kArray, kPool, kList, kMap)
	if !ok || !v.mutable || g.carelessPercent == 0 {
		return false
	}
	elem := v.t.args[len(v.t.args)-1]
	g.guarded(func() {
		p, ok := g.elementOf(v, true)
		if !ok {
			g.line("print(\"-\\n\")")
			return
		}
		target := p.src
		if elem != tCount {
			r := g.recordOf(elem)
			if r == nil {
				g.line("print(\"-\\n\")")
				return
			}
			found := false
			for _, f := range r.fields {
				if f.t == tCount && f.isVar {
					target += "." + f.name
					found = true
					break
				}
			}
			if !found {
				g.line("print(\"-\\n\")")
				return
			}
		}
		var call string
		switch v.t.kind {
		case kArray:
			call = fmt.Sprintf("churn(%s)", v.name)
		case kList:
			call = fmt.Sprintf("churnList(%s)", v.name)
		case kMap:
			if v.t.args[0] != tString {
				g.line("print(\"-\\n\")")
				return
			}
			call = fmt.Sprintf("churnMap(%s)", v.name)
		default:
			call = fmt.Sprintf("churnPool(%s, x: %s)", v.name, g.owned(elem, 1))
		}
		g.line("%s = %s", target, call)
	})
	return true
}
