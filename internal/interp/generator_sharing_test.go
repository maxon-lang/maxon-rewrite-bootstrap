package interp_test

import (
	"fmt"
	"regexp"
	"strings"
)

// The part of the generator that writes frozen values, cowns and behaviours. A behaviour body is written as a function of
// its own: the generator hides the variables around it and exposes some of them by name, and whichever of those the body
// then mentions is what the behaviour captures. A program that is careless exposes a borrow, names a cown twice, reads a
// cown's value outside a behaviour, or changes the contents of a frozen value, to see that the checker refuses it.

func (g *generator) frozenOf(inner *ty) *ty { return g.intern(&ty{kind: kFrozen, args: []*ty{inner}}) }
func (g *generator) cownOf(inner *ty) *ty   { return g.intern(&ty{kind: kCown, args: []*ty{inner}}) }

// freezable is a type of a value that can be frozen: any that is not a plain value.
func (g *generator) freezable() *ty {
	if g.chance(20) {
		return g.array(g.elementType())
	}
	managed := g.managedTypes()
	return managed[g.pick(len(managed))]
}

// cownable is the type of what a cown owns: a plain count, or a value of any type.
func (g *generator) cownable() *ty {
	switch n := g.pick(10); {
	case n < 4:
		return tCount
	case n < 5:
		return g.frozenOf(g.freezable())
	case n < 6:
		return g.cownOf(g.cownable())
	}
	return g.freezable()
}

func (g *generator) sharedType() *ty {
	if g.chance(50) {
		return g.frozenOf(g.freezable())
	}
	return g.cownOf(g.cownable())
}

// sharedTypeBefore is a frozen value or a cown of a type declared earlier, for a field.
func (g *generator) sharedTypeBefore() *ty {
	inner := g.elementTypeBefore()
	if g.chance(50) {
		if inner.scalar() {
			inner = tString
		}
		return g.frozenOf(inner)
	}
	return g.cownOf(inner)
}

// sharedExpr is a new owner of a frozen value or a cown: a share of one that is in scope, or a new one.
func (g *generator) sharedExpr(t *ty, depth int) string {
	inner := t.args[0]
	if p, ok := g.place(t, false); ok && g.chance(35) {
		return "share(" + p.src + ")"
	}
	if t.kind == kCown {
		if inner == tCount {
			return fmt.Sprintf("cown(%s)", typed(inner, g.countExpr(depth-1)))
		}
		return fmt.Sprintf("cown(%s)", g.owned(inner, depth-1))
	}
	return fmt.Sprintf("freeze(%s)", g.owned(inner, depth-1))
}

// walkFrozen is the places inside a frozen value: its fields, which are immutable. A program that is careless tries to
// change one.
func (g *generator) walkFrozen(out []place, p place, want *ty, depth int) []place {
	r := g.recordOf(p.t.args[0])
	if r == nil {
		return out
	}
	for _, f := range r.fields {
		next := place{src: p.src + "." + f.name, t: f.t, root: p.root, mutable: f.isVar && g.careless()}
		out = g.walk(out, next, want, depth-1)
	}
	return out
}

// sharingPreamble gives the statements a cown and a frozen value to work with, in a program that shares.
func (g *generator) sharingPreamble() {
	if !g.sharing {
		return
	}
	for i, n := 0, 1+g.pick(3); i < n; i++ {
		t := g.cownOf(g.cownable())
		name := g.fresh("k")
		g.line(kwLet+" %s = %s", name, g.sharedExpr(t, 1))
		g.declare(name, t, true, false)
	}
	if g.chance(60) {
		t := g.frozenOf(g.freezable())
		name := g.fresh("z")
		isVar := g.chance(50)
		g.line("%s %s = %s", map[bool]string{true: kwVar, false: kwLet}[isVar], name, g.sharedExpr(t, 1))
		g.declare(name, t, true, isVar)
	}
	if g.sinful() {
		// A frozen record gives the careless statements something to change.
		var records []*ty
		for _, r := range g.records {
			if len(r.fields) > 0 && !strings.HasPrefix(r.t.name, "Box") {
				records = append(records, r.t)
			}
		}
		t := g.frozenOf(records[g.pick(len(records))])
		name := g.fresh("z")
		g.line(kwVar+" %s = %s", name, g.sharedExpr(t, 1))
		g.declare(name, t, true, true)
	}
}

// varsOf are the variables in scope of the kind, that still hold a value.
func (g *generator) varsOf(k kind) []*variable {
	var out []*variable
	for _, v := range g.visible() {
		if v.t.kind == k && (!v.moved || g.careless()) {
			out = append(out, v)
		}
	}
	return out
}

func (g *generator) actFreeze(depth int) bool {
	if !g.sharing {
		return false
	}
	t := g.frozenOf(g.freezable())
	name := g.fresh("z")
	isVar := g.chance(50)
	g.line("%s %s = %s", map[bool]string{true: kwVar, false: kwLet}[isVar], name, g.sharedExpr(t, 2))
	g.declare(name, t, true, isVar)
	return true
}

func (g *generator) actCown(depth int) bool {
	if !g.sharing {
		return false
	}
	t := g.cownOf(g.cownable())
	name := g.fresh("k")
	g.line(kwLet+" %s = %s", name, g.sharedExpr(t, 2))
	g.declare(name, t, true, false)
	return true
}

// actShare makes another owner of a frozen value or a cown that is in scope.
func (g *generator) actShare(depth int) bool {
	var found []place
	for _, v := range g.varsOf(kFrozen) {
		found = append(found, place{src: v.name, t: v.t, root: v, whole: true})
	}
	for _, v := range g.varsOf(kCown) {
		found = append(found, place{src: v.name, t: v.t, root: v, whole: true})
	}
	if len(found) == 0 {
		return false
	}
	p := found[g.pick(len(found))]
	name := g.fresh("sh")
	g.line(kwLet+" %s = share(%s)", name, p.src)
	g.declare(name, p.t, true, false)
	return true
}

// actThaw copies what a frozen value holds into an owned value, which can then change.
func (g *generator) actThaw(depth int) bool {
	frozen := g.varsOf(kFrozen)
	if len(frozen) == 0 {
		return false
	}
	v := frozen[g.pick(len(frozen))]
	name := g.fresh("v")
	g.line(kwVar+" %s = thaw(%s)", name, v.name)
	g.declare(name, v.t.args[0], true, true)
	return true
}

// actFrozenMethod calls a method of a record on a frozen value, which a method that only reads can do. A program that is
// careless calls one that changes or consumes it.
func (g *generator) actFrozenMethod(depth int) bool {
	for _, v := range g.shuffledVars(g.varsOf(kFrozen)) {
		r := g.recordOf(v.t.args[0])
		if r == nil || len(r.methods) == 0 {
			continue
		}
		m := r.methods[g.pick(len(r.methods))]
		abuse := g.sinful() && g.chance(60)
		if (m.recv != "" || len(m.params) > 0) && !abuse {
			continue
		}
		text, ok := g.callTextRoots(v.name+"."+m.name, m.params, 2, []*variable{v})
		if !ok {
			continue
		}
		if m.ret != nil {
			g.line("_ = %s", text)
		} else {
			g.line("%s", text)
		}
		return true
	}
	return false
}

func (g *generator) shuffledVars(in []*variable) []*variable {
	out := append([]*variable(nil), in...)
	g.r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// actCownPeek, in a careless program, reads the value of a cown outside any behaviour.
func (g *generator) actCownPeek(depth int) bool {
	if !g.sinful() || !g.chance(50) {
		return false
	}
	cowns := g.varsOf(kCown)
	if len(cowns) == 0 {
		return false
	}
	v := cowns[g.pick(len(cowns))]
	inner := v.t.args[0]
	switch {
	case inner.kind == kArray:
		g.line("print(\"{%s.count()}\\n\")", v.name)
	case g.recordOf(inner) != nil && len(g.recordOf(inner).fields) > 0:
		g.line("_ = %s.%s", v.name, g.recordOf(inner).fields[0].name)
	default:
		g.line("_ = %s.copy()", v.name)
		g.line("_ = %s.count()", v.name)
	}
	return true
}

// exposed is a variable of the function around a behaviour that the body may name, with the variable that stands for it in
// the body.
type exposed struct {
	outer, inner *variable
}

// mentions is whether the text names the variable.
func mentions(text, name string) bool {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`).MatchString(text)
}

// actWhen writes a behaviour on one or two cowns, whose body is written as a function of its own.
func (g *generator) actWhen(depth int) bool {
	if !g.sharing || depth <= 0 || g.behaviours >= 2 {
		return false
	}
	cowns := g.varsOf(kCown)
	if len(cowns) == 0 {
		return false
	}
	operands := []*variable{cowns[g.pick(len(cowns))]}
	if g.chance(40) && len(cowns) > 1 {
		second := cowns[g.pick(len(cowns))]
		if second != operands[0] || g.adversarial() {
			operands = append(operands, second)
		}
	}
	if g.adversarial() && g.chance(30) {
		// Two names for one cown: the checker cannot tell, and the behaviour traps when it is scheduled.
		alias := g.fresh("al")
		g.line(kwLet+" %s = share(%s)", alias, operands[0].name)
		aliasVar := g.declare(alias, operands[0].t, true, false)
		operands = append(operands, aliasVar)
	}

	var exposures []exposed
	for _, v := range g.visible() {
		if v.moved && !g.careless() {
			continue
		}
		switch {
		case v.name == kwSelf && !g.careless():
			continue
		case v.t.scalar():
		case v.owned && !v.setParam && v.frozen == 0 && v.loop == g.loop:
		case !v.owned && g.careless():
		default:
			continue
		}
		mutable := v.mutable && g.careless() && g.chance(30)
		exposures = append(exposures, exposed{outer: v, inner: &variable{name: v.name, t: v.t, owned: v.owned || v.t.scalar(), mutable: mutable}})
	}

	names := make([]string, len(operands))
	operandNames := make([]string, len(operands))
	for i, o := range operands {
		names[i] = g.fresh("w")
		operandNames[i] = o.name
	}
	label := g.label()
	g.line(kwWhen+" %s "+kwAs+" %s %s", strings.Join(operandNames, ", "), strings.Join(names, ", "), label)

	savedScopes, savedRet, savedLoop, savedBudget, savedGuards := g.scopes, g.ret, g.loop, g.budget, g.guards
	g.scopes = [][]*variable{nil}
	for _, e := range exposures {
		g.scopes[0] = append(g.scopes[0], e.inner)
	}
	g.ret, g.loop, g.budget = nil, 0, 2+g.pick(4)
	g.behaviours++
	g.indent++
	g.push()
	for i, o := range operands {
		g.declare(names[i], o.t.args[0], false, true)
	}
	start := len(g.out)
	for g.budget > 0 {
		g.stmt(depth - 1)
	}
	if len(g.out) == start {
		g.line("print(\"w\\n\")")
	}
	body := strings.Join(g.out[start:], "\n")
	g.pop()
	g.indent--
	g.behaviours--
	g.scopes, g.ret, g.loop, g.budget, g.guards = savedScopes, savedRet, savedLoop, savedBudget, savedGuards
	g.line(kwEnd+" %s", label)

	for _, e := range exposures {
		if mentions(body, e.outer.name) && e.outer.t.managed() && e.outer.owned {
			e.outer.moved = true
		}
	}
	return true
}

// The actions below only write what the checker must refuse, and with the least else in the way, so a program that is
// accepted anyway has found a hole. They are written in careless programs, and once in a targeted one.

// sinful says whether a rule of sharing is broken on purpose here: anywhere in a careless program, and in a targeted one
// until its one sin is written.
func (g *generator) sinful() bool { return g.carelessPercent > 0 || g.sinsLeft > 0 }

// aCown is a cown in scope for a behaviour that exists to hold one statement.
func (g *generator) aCown() (*variable, bool) {
	cowns := g.varsOf(kCown)
	if len(cowns) == 0 {
		return nil, false
	}
	return cowns[g.pick(len(cowns))], true
}

// holding writes a behaviour on a cown whose body is one statement.
func (g *generator) holding(c *variable, statement string) {
	label := g.label()
	g.line(kwWhen+" %s "+kwAs+" %s %s", c.name, g.fresh("w"), label)
	g.indent++
	g.line("%s", statement)
	g.indent--
	g.line(kwEnd+" %s", label)
}

// actBorrowedCapture has a behaviour use a variable that only borrows its value: a parameter that is not consumed, the
// receiver, or the access that an enclosing behaviour has to a cown's value.
func (g *generator) actBorrowedCapture(depth int) bool {
	if !g.sharing || !g.sinful() || g.behaviours >= 2 {
		return false
	}
	c, ok := g.aCown()
	if !ok {
		return false
	}
	var borrowed []*variable
	for _, v := range g.visible() {
		if !v.owned && v.t.managed() && !v.moved && v != c {
			borrowed = append(borrowed, v)
		}
	}
	if len(borrowed) == 0 {
		return false
	}
	v := borrowed[g.pick(len(borrowed))]
	g.holding(c, fmt.Sprintf("_ = %s.copy()", v.name))
	return true
}

// actUseAfterCapture uses a variable after a behaviour took it, which moved it away.
func (g *generator) actUseAfterCapture(depth int) bool {
	if !g.sharing || !g.sinful() || g.behaviours >= 2 {
		return false
	}
	c, ok := g.aCown()
	if !ok {
		return false
	}
	var owners []*variable
	for _, v := range g.visible() {
		if v.owned && !v.setParam && !v.moved && v.t.managed() && v.frozen == 0 && v.loop == g.loop && v.name != kwSelf && v != c {
			owners = append(owners, v)
		}
	}
	if len(owners) == 0 {
		return false
	}
	v := owners[g.pick(len(owners))]
	g.holding(c, fmt.Sprintf("_ = %s.copy()", v.name))
	g.line("_ = %s.copy()", v.name)
	v.moved = true
	return true
}

// actFrozenAbuse changes a frozen value's contents, or takes them out of it, in one of the ways that is refused.
func (g *generator) actFrozenAbuse(depth int) bool {
	if !g.sinful() {
		return false
	}
	for _, v := range g.shuffledVars(g.varsOf(kFrozen)) {
		r := g.recordOf(v.t.args[0])
		if r == nil || len(r.fields) == 0 {
			continue
		}
		f := r.fields[g.pick(len(r.fields))]
		target := v.name + "." + f.name
		switch g.pick(5) {
		case 0:
			if !f.isVar {
				continue
			}
			g.line("%s = %s", target, g.owned(f.t, 1))
		case 1:
			if f.t.kind != kArray {
				continue
			}
			g.line("%s.append(%s)", target, g.owned(f.t.args[0], 1))
		case 2:
			if f.t != tCount {
				continue
			}
			g.line("bump(%s)", target)
		case 3:
			if !f.t.managed() {
				continue
			}
			name := g.fresh("v")
			g.line(kwLet+" %s = %s", name, target)
			g.declare(name, f.t, true, false)
		default:
			if !f.t.managed() {
				continue
			}
			g.line("consume(%s)", target)
		}
		return true
	}
	return false
}
