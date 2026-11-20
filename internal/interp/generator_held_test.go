package interp_test

import (
	"fmt"
	"slices"
)

// The part of the generator that writes a borrowed place as one operand of an expression and, as an operand evaluated
// after it, a call that may change or consume what the place lies in: the second operand of a string comparison, and
// the index, key or handle of a subscript whose base is itself a field or an element. The first operand is loaded
// before the call runs, so a call that frees what it was loaded from leaves the expression reading freed memory. The
// same shapes are also written with a call that only reads the place or touches another variable, and with a scalar
// as the first operand, which the checker has to accept; a program that is accepted with a call that changes or
// consumes the place is a hole.

// heldBrokenPercent is how often a careless program writes the call that changes the place.
const heldBrokenPercent = 85

// heldFamilyPercent is the share of targeted programs whose one sin is one of these.
const heldFamilyPercent = 35

// heldBroken says whether the call written now changes or consumes the place it is held across: often in a careless
// program, and once in a targeted one, by the statement that is its sin.
func (g *generator) heldBroken() bool {
	if g.carelessPercent > 0 {
		return g.chance(heldBrokenPercent)
	}
	return g.heldSin && g.sinning
}

// The uses of the element of a subscript that the generator writes.
const (
	useRead = iota
	useAssign
	useMutate
	useMatch
	subscriptUses
)

// firstElementKey is the key the generator keeps in a map it needs an element of.
const firstElementKey = 0

func keyOf(k *ty, n int) string {
	if k == tCount {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("\"k%d\"", n)
}

func isCollection(t *ty) bool {
	return t.kind == kArray || t.kind == kPool || t.kind == kList || t.kind == kMap
}

// element marks the place as an element of a collection, which exists only where the guard holds.
func (p place) element(guard string) place {
	p.elements = true
	p.guards = append(p.guards[:len(p.guards):len(p.guards)], guard)
	return p
}

// elementTypeOf is the type of an element of a collection: the last of its arguments.
func elementTypeOf(t *ty) *ty { return t.args[len(t.args)-1] }

// indexTypeOf is the type of what a subscript of the collection takes.
func (g *generator) indexTypeOf(t *ty) *ty {
	switch t.kind {
	case kArray:
		return tCount
	case kMap:
		return t.args[0]
	case kPool, kList:
		return g.handle(t.args[0])
	}
	panic("indexTypeOf: not a collection: " + t.src())
}

// pathPlaces are the places whose type accept takes in variables, in their fields and, when elements is set, in the
// first element of a collection variable, so that a place can lie behind a subscript.
func (g *generator) pathPlaces(accept func(*ty) bool, mutable, elements bool) []place {
	out := g.placesWhere(accept, mutable)
	if !elements {
		return out
	}
	for _, v := range g.visible() {
		first, ok := g.elementPlace(v, v.mutable)
		if !ok {
			continue
		}
		for _, p := range g.walk(nil, first, accept, 2) {
			if !mutable || p.mutable {
				out = append(out, p)
			}
		}
	}
	return out
}

// siblingCall is a call that gives result, of type t, and is written after the borrowed place in one expression. With
// broken set it changes or consumes the place or what it lies in; otherwise it reads them or works on another
// variable.
func (g *generator) siblingCall(borrowed place, t *ty, result string, broken bool) (string, bool) {
	around := append(borrowed.outer[:len(borrowed.outer):len(borrowed.outer)], borrowed)
	var calls []func() string
	if broken {
		for _, q := range around {
			if !q.mutable || q.src == kwSelf {
				continue
			}
			calls = append(calls, func() string { return fmt.Sprintf("rebuildReturning(%s, result: %s)", q.src, result) })
			if q.t.kind == kArray {
				calls = append(calls, func() string { return fmt.Sprintf("eraseReturning(%s, result: %s)", q.src, result) })
			}
			if m := g.stringMethod(q.t, kwMutates); m != nil && t == tString {
				calls = append(calls, func() string { return q.src + "." + m.name + "()" })
			}
		}
		if root := borrowed.root; g.movesFrom(root) {
			calls = append(calls, func() string {
				root.moved = true
				return fmt.Sprintf("swallowReturning(%s, result: %s)", root.name, result)
			})
			if m := g.stringMethod(root.t, kwConsumes); m != nil && t == tString {
				calls = append(calls, func() string {
					root.moved = true
					return root.name + "." + m.name + "()"
				})
			}
		}
	} else {
		for _, q := range around {
			calls = append(calls, func() string { return fmt.Sprintf("lookReturning(%s, result: %s)", typed(q.t, q.src), result) })
		}
		for _, v := range g.visible() {
			if v == borrowed.root || v.moved || !v.t.managed() || v.name == kwSelf || v.t.kind == kFrozen || v.t.kind == kCown {
				continue
			}
			if v.mutable && v.frozen == 0 {
				calls = append(calls, func() string { return fmt.Sprintf("rebuildReturning(%s, result: %s)", v.name, result) })
			}
			if g.movesFrom(v) {
				calls = append(calls, func() string {
					v.moved = true
					return fmt.Sprintf("swallowReturning(%s, result: %s)", v.name, result)
				})
			}
		}
	}
	if len(calls) == 0 {
		return "", false
	}
	return calls[g.pick(len(calls))](), true
}

// movesFrom says whether the variable can be given away at this point.
func (g *generator) movesFrom(v *variable) bool {
	return slices.Contains(g.movable(v.t), v)
}

// stringMethod is the method of a record that gives a String and has the convention, if there is one.
func (g *generator) stringMethod(t *ty, recv string) *fn {
	r := g.recordOf(t)
	if r == nil {
		return nil
	}
	for _, m := range r.methods {
		if m.ret == tString && m.recv == recv && len(m.params) == 0 {
			return m
		}
	}
	return nil
}

// heldCompare is a comparison of a place with a later operand, which is a call, another place or a literal. A
// comparison written inside a condition has no guard to put elements behind, so it takes none.
func (g *generator) heldCompare(broken, elements bool) (string, bool) {
	accept := isType(tString)
	if g.chance(15) {
		accept = isType(tCount)
	}
	candidates := g.pathPlaces(accept, false, elements)
	if len(candidates) == 0 {
		return "", false
	}
	left := candidates[g.pick(len(candidates))]
	later, ok := g.laterOperand(left, broken)
	if !ok {
		return "", false
	}
	g.guards = append(g.guards, left.guards...)
	op := []string{"==", "!="}[g.pick(2)]
	if !left.root.moved && g.chance(20) {
		// The call is evaluated first, so the place is read after whatever it did, which is only a legal read when the
		// call did not give the place away.
		return fmt.Sprintf("%s %s %s", later, op, left.src), true
	}
	return fmt.Sprintf("%s %s %s", left.src, op, later), true
}

func (g *generator) laterOperand(left place, broken bool) (string, bool) {
	result := fmt.Sprintf("\"s%d\"", g.pick(3))
	if left.t == tCount {
		result = typed(tCount, g.literal(g.pick(3)))
	}
	switch n := g.pick(10); {
	case n < 6:
		return g.siblingCall(left, left.t, result, broken)
	case n < 8 && left.t == tString:
		if c, ok := g.callReturning(tString, 2); ok {
			return c, true
		}
	case n < 9:
		if p, ok := g.place(left.t, false); ok {
			return p.src, true
		}
	}
	return result, true
}

// actHeldCompare writes a comparison as a condition, as a printed value or as a bool that is kept.
func (g *generator) actHeldCompare(depth int) bool {
	broken := g.heldBroken()
	wrote := false
	g.guarded(func() {
		text, ok := g.heldCompare(broken, true)
		if !ok {
			return
		}
		wrote = true
		switch g.pick(3) {
		case 0:
			label := g.label()
			g.line(kwIf+" %s %s", text, label)
			g.indent++
			g.line("print(\"eq\\n\")")
			g.indent--
			g.line(kwEnd+" %s", label)
		case 1:
			g.line("print(\"{%s}\\n\")", text)
		default:
			name := g.fresh("v")
			g.line(kwLet+" %s = %s", name, text)
			g.declare(name, tBool, false, false)
		}
	})
	return wrote
}

// heldInterpolation is the text between the quotes of an interpolated string whose first two parts are a place and a
// call with no text between them. Lowering joins the parts from the left, so the first stays a borrow while the call
// runs, whereas a text between the parts is joined to the first at once. A scalar first part is converted at once as
// well, and a call that comes first leaves nothing borrowed.
func (g *generator) heldInterpolation(broken, elements bool) (string, bool) {
	accept := isType(tString)
	if g.chance(15) {
		accept = isType(tCount)
	}
	candidates := g.pathPlaces(accept, false, elements)
	if len(candidates) == 0 {
		return "", false
	}
	left := candidates[g.pick(len(candidates))]
	var call string
	var ok bool
	if g.chance(15) {
		call, ok = g.callReturning(tString, 2)
	}
	if !ok {
		call, ok = g.siblingCall(left, tString, fmt.Sprintf("\"s%d\"", g.pick(3)), broken)
	}
	if !ok {
		return "", false
	}
	g.guards = append(g.guards, left.guards...)
	switch {
	case !left.root.moved && g.chance(15):
		return fmt.Sprintf("{%s}{%s}", call, left.src), true
	case g.chance(15):
		return fmt.Sprintf("{%s} {%s}", left.src, call), true
	}
	return fmt.Sprintf("{%s}{%s}", left.src, call), true
}

// heldInterpolationCompare compares such a string with a literal.
func (g *generator) heldInterpolationCompare(broken, elements bool) (string, bool) {
	text, ok := g.heldInterpolation(broken, elements)
	if !ok {
		return "", false
	}
	op := []string{"==", "!="}[g.pick(2)]
	return fmt.Sprintf("\"%s\" %s \"s%d\"", text, op, g.pick(3)), true
}

// actHeldInterpolation uses such a string where strings are used: printed, kept, passed as an argument and compared.
func (g *generator) actHeldInterpolation(depth int) bool {
	broken := g.heldBroken()
	wrote := false
	g.guarded(func() {
		switch g.pick(4) {
		case 0:
			text, ok := g.heldInterpolation(broken, true)
			if !ok {
				return
			}
			g.line("print(\"%s\\n\")", text)
		case 1:
			text, ok := g.heldInterpolation(broken, true)
			if !ok {
				return
			}
			name := g.fresh("v")
			g.line(kwLet+" %s = \"%s\"", name, text)
			g.declare(name, tString, true, false)
		case 2:
			text, ok := g.heldInterpolation(broken, true)
			if !ok {
				return
			}
			name := g.fresh("v")
			g.line(kwLet+" %s = keep(\"%s\")", name, text)
			g.declare(name, tString, true, false)
		default:
			text, ok := g.heldInterpolationCompare(broken, true)
			if !ok {
				return
			}
			label := g.label()
			g.line(kwIf+" %s %s", text, label)
			g.indent++
			g.line("print(\"eq\\n\")")
			g.indent--
			g.line(kwEnd+" %s", label)
		}
		wrote = true
	})
	return wrote
}

// actHeldIndex writes a subscript whose base is a path, with an index that is a call, and uses its element.
func (g *generator) actHeldIndex(depth int) bool {
	broken := g.heldBroken()
	use := g.pick(subscriptUses)
	// A kind is picked before a place, since arrays are the most common and would leave the others few.
	byKind := map[kind][]place{}
	for _, p := range g.pathPlaces(isCollection, use == useAssign || use == useMutate, true) {
		if use == useMatch && g.unionOf(elementTypeOf(p.t)) == nil {
			continue
		}
		byKind[p.t.kind] = append(byKind[p.t.kind], p)
	}
	var kinds []kind
	for _, k := range []kind{kArray, kPool, kList, kMap} {
		if len(byKind[k]) > 0 {
			kinds = append(kinds, k)
		}
	}
	if len(kinds) == 0 {
		return false
	}
	bases := byKind[kinds[g.pick(len(kinds))]]
	var nested []place
	for _, p := range bases {
		if len(p.outer) > 0 {
			nested = append(nested, p)
		}
	}
	if len(nested) > 0 && g.chance(75) {
		bases = nested
	}
	base := bases[g.pick(len(bases))]
	wrote := false
	g.guarded(func() { wrote = g.heldSubscript(use, base, broken) })
	return wrote
}

func (g *generator) heldSubscript(use int, base place, broken bool) bool {
	g.guards = append(g.guards, base.guards...)
	plain, result, ok := g.ensureElement(base)
	if !ok {
		return false
	}
	index := plain
	if g.chance(80) {
		if call, ok := g.siblingCall(base, g.indexTypeOf(base.t), result, broken); ok {
			index = call
		}
	}
	element := fmt.Sprintf("%s[%s]", base.src, index)
	t := elementTypeOf(base.t)
	switch use {
	case useRead:
		switch {
		case t == tCount || t == tString:
			g.line("print(\"{%s}\\n\")", element)
		case g.chance(50):
			name := g.fresh("v")
			g.line(kwLet+" %s = %s.copy()", name, element)
			g.declare(name, t, true, false)
		default:
			g.line("_ = %s.copy()", element)
		}
	case useAssign:
		if f, found := g.countField(t); found && g.chance(40) {
			g.line("%s.%s = %s", element, f.name, typed(tCount, g.countExpr(0)))
		} else {
			g.line("%s = %s", element, typed(t, g.owned(t, 0)))
		}
	case useMutate:
		if t == tCount {
			g.line("bump(%s)", element)
		} else {
			g.line("overwrite(%s, value: %s)", element, g.owned(t, 0))
		}
	case useMatch:
		g.matchOn(element, base.root, g.arms(t))
	default:
		panic(fmt.Sprintf("heldSubscript: unknown use %d", use))
	}
	return true
}

// countField is a field of a record type that holds a Count and can be changed.
func (g *generator) countField(t *ty) (field, bool) {
	if r := g.recordOf(t); r != nil {
		for _, f := range r.fields {
			if f.t == tCount && f.isVar {
				return f, true
			}
		}
	}
	return field{}, false
}

// ensureElement makes an element of the collection exist, by adding one when it can be changed and otherwise by a
// guard, so the statement does not stop at a trap before it has done anything. It gives the index that names the
// element, and the same index written as an argument.
func (g *generator) ensureElement(b place) (index, result string, ok bool) {
	elem := elementTypeOf(b.t)
	switch b.t.kind {
	case kArray:
		if b.mutable {
			g.line("%s.append(%s)", b.src, typed(elem, g.owned(elem, 0)))
		} else {
			g.guards = append(g.guards, b.src+".count() > 0")
		}
		return "0", typed(tCount, "0"), true
	case kMap:
		key := keyOf(b.t.args[0], firstElementKey)
		if b.mutable {
			g.line("_ = %s.replace(%s, value: %s)", b.src, key, typed(elem, g.owned(elem, 0)))
		} else {
			g.guards = append(g.guards, fmt.Sprintf("%s.has(%s)", b.src, key))
		}
		return key, typed(b.t.args[0], key), true
	case kPool, kList:
		if !b.mutable {
			// A handle that is a field is a copy that the checker still sees as a place, so it is passed only by name.
			var named []place
			for _, h := range g.places(g.handle(elem), false) {
				if h.whole {
					named = append(named, h)
				}
			}
			if len(named) == 0 {
				return "", "", false
			}
			h := named[g.pick(len(named))]
			g.guards = append(g.guards, fmt.Sprintf("%s.contains(%s)", b.src, h.src))
			return h.src, h.src, true
		}
		name := g.fresh("h")
		add := "insert"
		if b.t.kind == kList {
			add = []string{"pushBack", "pushFront"}[g.pick(2)]
		}
		g.line(kwLet+" %s = %s.%s(%s)", name, b.src, add, typed(elem, g.owned(elem, 0)))
		g.declare(name, g.handle(elem), false, false)
		return name, name, true
	}
	panic("ensureElement: not a collection: " + b.t.src())
}
