package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

type pgen struct {
	m *model
	a *analysis
	w *writer
	// after holds the node rules whose first element a set rule parses before choosing them, mapped to that
	// element's rule; each gets a parse<Node>After that continues from it.
	after   map[*rule]*rule
	sets    map[string]string
	setList []string
	// lists are the element types of the lists the parser builds, in the order they are first used; each has a
	// stack in listStacks.
	lists []string
	// idStacks are the stacks of the numbers that a list of records or of texts holds, one for each list of the grammar:
	// a node's lists are parsed one after the other, so each takes the numbers above its own mark.
	idStacks []string
}

// target says where the values a sequence parses go.
type target struct {
	node   *node
	helper *rule
	// pass names the variable that receives the value of a pass-through alternative, and passSet the set it is a view of.
	pass, passSet string
}

func emitParser(m *model, a *analysis) string {
	g := &pgen{m: m, a: a, w: &writer{}, after: map[*rule]*rule{}, sets: map[string]string{}}
	for _, r := range m.g.rules {
		if m.kind[r] == rkSet && !m.leftRec[r] {
			for _, grp := range g.factorGroups(r) {
				for _, i := range grp.members {
					ref, _ := m.passThrough(r.alts[i])
					g.after[ref] = grp.lead
				}
			}
		}
	}
	body := &writer{}
	g.w = body
	for _, r := range m.g.rules {
		switch m.kind[r] {
		case rkNode:
			g.nodeRule(r)
		case rkSet:
			if m.leftRec[r] {
				g.operatorRule(r)
			} else {
				g.setRule(r)
			}
		case rkHelper:
			g.helperRule(r)
		}
	}
	w := &writer{}
	header(w, "syntax", "minimaxon/internal/ast")
	for _, name := range g.setList {
		w.p("var %s = [numTokens]bool{%s}\n", name, g.sets[name])
	}
	w.p("\n// listStacks has a stack for each type of element that a list of structs in the grammar holds, and one for the\n")
	w.p("// numbers of the records and texts that each list of the tree holds.\ntype listStacks struct {\n")
	for _, name := range g.idStacks {
		w.p("\t%s listStack[uint32]\n", name)
	}
	for _, elem := range g.lists {
		w.p("\t%s listStack[%s]\n", listField(elem), qualify(elem))
	}
	w.p("}\n\nfunc (s *listStacks) reset() {\n")
	for _, name := range g.idStacks {
		w.p("\ts.%s.reset()\n", name)
	}
	for _, elem := range g.lists {
		w.p("\ts.%s.reset()\n", listField(elem))
	}
	w.p("}\n\n")
	w.b.WriteString(body.b.String())
	return w.b.String()
}

func (g *pgen) p(format string, args ...any) { g.w.p(format, args...) }

func (g *pgen) fail(line int, format string, args ...any) { g.m.fail(line, format, args...) }

// qualify writes a type of package ast as seen from package syntax.
func qualify(t string) string {
	var b strings.Builder
	for i := 0; i < len(t); {
		c := t[i]
		if isIdentStart(c) {
			j := i
			for j < len(t) && isIdentPart(t[j]) {
				j++
			}
			word := t[i:j]
			if isUpper(word) && (i == 0 || t[i-1] != '.') {
				b.WriteString("ast.")
			}
			b.WriteString(word)
			i = j
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

func parseFunc(r *rule) string { return "parse" + capitalize(r.name) }

func lowerFirst(s string) string { return strings.ToLower(s[:1]) + s[1:] }

func (g *pgen) call(r *rule) string { return "p." + parseFunc(r) + "()" }

// tokenSet names a package-level table of the given token kinds, shared by identical sets.
func (g *pgen) tokenSet(terms []string) string {
	entries := make([]string, len(terms))
	for i, t := range terms {
		entries[i] = t + ": true"
	}
	key := strings.Join(entries, ", ")
	for _, name := range g.setList {
		if g.sets[name] == key {
			return name
		}
	}
	name := fmt.Sprintf("lookahead%d", len(g.setList))
	g.sets[name] = key
	g.setList = append(g.setList, name)
	return name
}

func (g *pgen) atAny(terms []string) string {
	if len(terms) == 1 {
		return "p.at(" + terms[0] + ")"
	}
	return "p.in(&" + g.tokenSet(terms) + ")"
}

func (g *pgen) peekAny(terms []string) string {
	if len(terms) == 1 {
		return "p.peekIs(" + terms[0] + ")"
	}
	return "p.peekIn(&" + g.tokenSet(terms) + ")"
}

// test builds the condition that the next tokens begin one of the strings in la. A first token that no later
// alternative can begin with is tested alone; the alternatives are tried in order, so earlier ones have taken
// what they share with it.
func (g *pgen) test(la laSet, later []laSet) string {
	var single []string
	var parts []string
	for _, t := range firstTerminals(la) {
		shared := false
		for _, l := range later {
			for _, u := range firstTerminals(l) {
				shared = shared || u == t
			}
		}
		if !shared || la[t] {
			single = append(single, t)
			continue
		}
		var seconds []string
		for s := range la {
			f := strings.Fields(s)
			if len(f) == 2 && f[0] == t {
				seconds = append(seconds, f[1])
			}
		}
		sort.Strings(seconds)
		parts = append(parts, fmt.Sprintf("p.at(%s) && %s", t, g.peekAny(seconds)))
	}
	if len(single) > 0 {
		parts = append([]string{g.atAny(single)}, parts...)
	}
	if len(parts) == 1 {
		return parts[0]
	}
	for i, part := range parts {
		if strings.Contains(part, "&&") {
			parts[i] = "(" + part + ")"
		}
	}
	return strings.Join(parts, " || ")
}

func covered(s string, earlier []laSet) bool {
	f := strings.Fields(s)
	for _, e := range earlier {
		if e[s] || (len(f) == 2 && e[f[0]]) {
			return true
		}
	}
	return false
}

func (g *pgen) describe(las []laSet) string {
	all := laSet{}
	for _, l := range las {
		all = union(all, l)
	}
	var parts []string
	for _, t := range firstTerminals(all) {
		parts = append(parts, g.m.describeTerminal(t))
	}
	return joinOr(parts)
}

// choice emits a decision between alternatives with the given lookahead sets, calling branch for each.
func (g *pgen) choice(las []laSet, line int, desc string, branch func(i int)) {
	for i, l := range las {
		shadowed := len(l) > 0
		for s := range l {
			shadowed = shadowed && covered(s, las[:i])
		}
		if shadowed {
			g.fail(line, "alternative %d can never be chosen: every way it can start, an earlier alternative starts", i+1)
		}
	}
	if desc == "" {
		desc = g.describe(las)
	}
	disjoint := true
	seen := map[string]bool{}
	for _, l := range las {
		for _, t := range firstTerminals(l) {
			disjoint = disjoint && !seen[t]
			seen[t] = true
		}
	}
	if disjoint {
		g.p("switch p.cur().Kind() {\n")
		for i, l := range las {
			g.p("case %s:\n", strings.Join(firstTerminals(l), ", "))
			branch(i)
		}
		g.p("default:\npanic(p.expected(%q))\n}\n", desc)
		return
	}
	for i, l := range las {
		if i > 0 {
			g.p("} else ")
		}
		g.p("if %s {\n", g.test(l, las[i+1:]))
		branch(i)
	}
	g.p("} else {\npanic(p.expected(%q))\n}\n", desc)
}

func (g *pgen) seq(elems []*elem, c cont, tg target) {
	for i, e := range elems {
		g.elem(e, c.then(elems[i+1:]), tg)
	}
}

func starOf(e *elem) *elem {
	star := *e
	star.suffix = '*'
	return &star
}

func onceOf(e *elem) *elem {
	once := *e
	once.suffix = 0
	return &once
}

// enterTest is the condition for parsing an optional or repeated element once more rather than going on.
func (g *pgen) enterTest(e *elem, c cont) string {
	enter := g.a.lookahead([]*elem{onceOf(e)}, c.then([]*elem{starOf(e)}))
	exit := g.a.lookahead(nil, c)
	return g.test(enter, []laSet{exit})
}

func (g *pgen) elem(e *elem, c cont, tg target) {
	switch e.suffix {
	case 0:
		g.once(e, c, tg)
	case '?':
		g.p("if %s {\n", g.enterTest(e, c))
		g.once(e, c, tg)
		g.p("}\n")
	case '*':
		g.p("for %s {\n", g.enterTest(e, c))
		g.once(e, c.then([]*elem{starOf(e)}), tg)
		g.p("}\n")
	case '+':
		g.once(e, c.then([]*elem{starOf(e)}), tg)
		g.p("for %s {\n", g.enterTest(e, c))
		g.once(e, c.then([]*elem{starOf(e)}), tg)
		g.p("}\n")
	}
}

func (g *pgen) once(e *elem, c cont, tg target) {
	switch e.kind {
	case ekLiteral, ekToken:
		term := g.m.terminal(e)
		call := fmt.Sprintf("p.expect(%s, %q)", term, g.m.describeTerminal(term))
		if e.joinBefore {
			g.p("p.joinNext = true\n")
		}
		if e.label == "" {
			g.p("%s\n", call)
		} else {
			g.assignToken(tg, e, call)
		}
		if e.joinAfter {
			g.p("p.joinNext = true\n")
		}
	case ekRule:
		call := g.call(g.m.ruleByName[e.text])
		switch {
		case e.label != "":
			g.assignValue(tg, e, call)
		case tg.pass != "":
			g.p("%s = %s\n", tg.pass, g.convert(call, g.m.ruleByName[e.text], tg.passSet))
		default:
			g.p("%s\n", call)
		}
	case ekGroup:
		if e.label != "" && g.m.groupType(e) == "string" {
			var terms []string
			for _, a := range e.group {
				terms = append(terms, g.m.terminal(a.elems[0]))
			}
			sort.Strings(terms)
			var descs []string
			for _, t := range terms {
				descs = append(descs, g.m.describeTerminal(t))
			}
			g.p("if !%s {\npanic(p.expected(%q))\n}\n", g.atAny(terms), joinOr(descs))
			// A ^ on one of the literals joins it, and only it, to its neighbour.
			for _, a := range e.group {
				if a.elems[0].joinBefore {
					g.p("if p.at(%s) {\np.joinNext = true\n}\n", g.m.terminal(a.elems[0]))
				}
			}
			var joinsAfter []string
			for _, a := range e.group {
				if a.elems[0].joinAfter {
					joinsAfter = append(joinsAfter, g.m.terminal(a.elems[0]))
				}
			}
			if len(joinsAfter) == 0 {
				g.assignToken(tg, e, "p.next()")
				return
			}
			g.p("{\njoins := %s\n", g.atAny(joinsAfter))
			g.assignToken(tg, e, "p.next()")
			g.p("p.joinNext = p.joinNext || joins\n}\n")
			return
		}
		if len(e.group) == 1 && e.label == "" {
			g.seq(e.group[0].elems, c, tg)
			return
		}
		las := make([]laSet, len(e.group))
		for i, a := range e.group {
			las[i] = g.a.lookahead(a.elems, c)
		}
		g.choice(las, e.line, "", func(i int) {
			a := e.group[i]
			if e.label != "" {
				inner := *a.elems[0]
				inner.label, inner.appendTo = e.label, e.appendTo
				g.once(&inner, c, tg)
				return
			}
			g.seq(a.elems, c, tg)
		})
	}
}

// fieldPlan is where the record field f of n is kept.
func (g *pgen) fieldPlan(n *node, f *field) *fieldPlan {
	for _, fp := range g.m.planOf(n).fields {
		if fp.f == f {
			return fp
		}
	}
	panic(fmt.Sprintf("grammargen: %s has no plan for field %s", n.name, f.name))
}

// setField is the statement that stores expr, of the field's Go type, in a field of a record or a struct.
func (g *pgen) setField(tg target, f *field, expr string) string {
	if !tg.node.record {
		return fmt.Sprintf("n.%s = %s", f.name, expr)
	}
	switch fp := g.fieldPlan(tg.node, f); fp.st {
	case stText, stLabel, stWord:
		return fmt.Sprintf("n.Set%sRef(%s)", f.name, expr)
	}
	return fmt.Sprintf("n.Set%s(%s)", f.name, expr)
}

// elemName is the node or set a type names, without its list brackets or pointer: "*Arg", "[]Expr" and "Expr" name Arg,
// Expr and Expr.
func elemName(typ string) string {
	return strings.TrimPrefix(strings.TrimPrefix(typ, "[]"), "*")
}

// convert is the view of the rule's node as the set dst, when the node is a record that is a member of it and the set is
// what a field, a list or a variable holds.
func (g *pgen) convert(value string, src *rule, dst string) string {
	if src == nil || g.m.kind[src] != rkNode {
		return value
	}
	n := g.m.nodeOfRule[src]
	if n.record && n.set != nil && n.set.name == dst && n.name != dst {
		return value + "." + dst + "()"
	}
	return value
}

// pushed is the element a list of the destination holds, for the value of a rule: a record is its number.
func (g *pgen) pushed(typ, value string) string {
	if _, _, ok := g.m.refersToRecord(typ); ok {
		return "uint32(" + value + ".ID)"
	}
	return value
}

func (g *pgen) assignToken(tg target, e *elem, call string) {
	if tg.helper != nil {
		g.checkHelperLabel(tg.helper, e)
		g.storeText(tg.helper.returns.name, tg.helper.returns.typ, e.appendTo, "p.text("+call+")")
		return
	}
	if tg.node == nil {
		g.fail(e.line, "label '%s' has no node to go in", e.label)
	}
	f := g.m.fieldOf(tg.node, e.label, e.line)
	if !tg.node.record {
		dst := "n." + f.name
		switch strings.TrimPrefix(f.typ, "[]") {
		case "bool":
			if e.kind != ekLiteral || e.appendTo {
				g.fail(e.line, "a bool field takes a single literal")
			}
			g.p("%s\nn.%s = true\n", call, f.name)
		case "string":
			g.storeText(dst, f.typ, e.appendTo, "p.text("+call+")")
		default:
			g.storeText(dst, f.typ, e.appendTo, fmt.Sprintf("p.%s%s(%s)", lowerFirst(tg.node.name), f.name, call))
		}
		return
	}
	fp := g.fieldPlan(tg.node, f)
	conv := fmt.Sprintf("p.%s%s(%s)", lowerFirst(tg.node.name), f.name, call)
	switch fp.st {
	case stBool:
		if e.kind != ekLiteral || e.appendTo {
			g.fail(e.line, "a bool field takes a single literal")
		}
		g.p("%s\nn.Set%s()\n", call, f.name)
	case stEnum:
		g.p("%s\n", g.setField(tg, f, "p.text("+call+")"))
	case stText, stLabel:
		g.p("%s\n", g.setField(tg, f, "p.textRef("+call+")"))
	case stTexts:
		if !e.appendTo {
			g.fail(e.line, "a list of texts takes +=")
		}
		g.p("%s.push(p.textRef(%s))\n", g.stackFor(tg, f.name, f.typ), call)
	case stI128, stWord:
		g.p("%s\n", g.setField(tg, f, conv))
	default:
		g.fail(e.line, "field %s of %s holds nodes, and a token is not one", f.name, tg.node.name)
	}
}

// storeText assigns a token's text to a helper's variable or a struct's field, or pushes it on the stack of its list.
func (g *pgen) storeText(dst, typ string, appendTo bool, value string) {
	if appendTo {
		g.p("p.lists.%s.push(%s)\n", g.listOf(typ), value)
	} else {
		g.p("%s = %s\n", dst, value)
	}
}

func (g *pgen) assignValue(tg target, e *elem, value string) {
	src := g.m.ruleByName[e.text]
	if tg.helper != nil {
		g.checkHelperLabel(tg.helper, e)
		typ := tg.helper.returns.typ
		value = g.convert(value, src, elemName(typ))
		if e.appendTo {
			g.p("%s.push(%s)\n", g.stackFor(tg, "", typ), g.pushed(typ, value))
		} else {
			g.p("%s = %s\n", tg.helper.returns.name, value)
		}
		return
	}
	if tg.node == nil {
		g.fail(e.line, "label '%s' has no node to go in", e.label)
	}
	f := g.m.fieldOf(tg.node, e.label, e.line)
	value = g.convert(value, src, elemName(f.typ))
	if e.appendTo {
		g.p("%s.push(%s)\n", g.stackFor(tg, f.name, f.typ), g.pushed(f.typ, value))
		return
	}
	g.p("%s\n", g.setField(tg, f, value))
}

// stackFor is the stack, as Go code, that a list of the given type is built on: the one of its own for a list of records
// or texts in the tree, which holds their numbers, and for a list of structs the one for its element type. A field is
// named by its node, and a helper's list by the helper.
func (g *pgen) stackFor(tg target, field, typ string) string {
	_, _, records := g.m.refersToRecord(typ)
	if !records && !(typ == "[]string" && tg.node != nil && tg.node.record) {
		return "p.lists." + g.listOf(typ)
	}
	name := "ids_"
	if tg.helper != nil {
		name += tg.helper.name
	} else {
		name += tg.node.name + "_" + field
	}
	if !slices.Contains(g.idStacks, name) {
		g.idStacks = append(g.idStacks, name)
	}
	return "p.lists." + name
}

// listOf names the stack for lists of type typ, which is a slice type.
func (g *pgen) listOf(typ string) string {
	elem, ok := strings.CutPrefix(typ, "[]")
	if !ok {
		panic(fmt.Sprintf("grammargen: %s is not a list type", typ))
	}
	if !slices.Contains(g.lists, elem) {
		g.lists = append(g.lists, elem)
	}
	return listField(elem)
}

func listField(elem string) string {
	return strings.NewReplacer("*", "ptr_", ".", "_").Replace(elem)
}

// A list is built on its stack, not by append: the elements are pushed as they are parsed, and when the list ends
// they are copied out once, into a slice of exactly their count, or for the lists of the tree into the tree's list
// array. Parsing a list inside one pushes and pops above it.
type pendingList struct {
	dst, typ, stack string
	// set stores the finished list, and take makes it from the stack above the mark.
	set  func(expr string) string
	take func(mark string) string
	// mixed is a destination that something other than the list assigns too, so the list sets it only if it has
	// elements.
	mixed bool
}

// pendingLists finds the lists that the alternatives build in tg's destinations, and starts each: it records where on
// its stack the list begins.
func (g *pgen) pendingLists(tg target, alts []*alt) []pendingList {
	var lists []pendingList
	var assigned []string
	g.m.walkElems(alts, func(e *elem) {
		if e.label == "" {
			return
		}
		var l pendingList
		if tg.helper != nil {
			name := tg.helper.returns.name
			l = pendingList{dst: name, typ: tg.helper.returns.typ, set: func(x string) string { return name + " = " + x }}
			if e.appendTo {
				l.stack = g.stackFor(tg, "", l.typ)
			}
		} else {
			f := g.m.fieldOf(tg.node, e.label, e.line)
			l = pendingList{dst: "n." + f.name, typ: f.typ, set: func(x string) string { return g.setField(tg, f, x) }}
			if e.appendTo {
				l.stack = g.stackFor(tg, f.name, f.typ)
			}
		}
		if !e.appendTo {
			assigned = append(assigned, l.dst)
			return
		}
		for _, have := range lists {
			if have.dst == l.dst {
				return
			}
		}
		l.take = g.takeList(l)
		lists = append(lists, l)
	})
	for i := range lists {
		lists[i].mixed = slices.Contains(assigned, lists[i].dst)
		g.p("%s := %s.mark()\n", markName(lists[i].dst), lists[i].stack)
	}
	return lists
}

// takeList is the code that makes the list from what its stack holds above a mark.
func (g *pgen) takeList(l pendingList) func(mark string) string {
	_, _, records := g.m.refersToRecord(l.typ)
	switch {
	case records:
		elem := qualify(g.m.goType(elemName(l.typ)))
		return func(mark string) string { return fmt.Sprintf("takeList[%s](p, &%s, %s)", elem, l.stack, mark) }
	case strings.HasPrefix(l.stack, "p.lists.ids_"):
		return func(mark string) string { return fmt.Sprintf("takeTexts(p, &%s, %s)", l.stack, mark) }
	}
	return func(mark string) string { return fmt.Sprintf("%s.take(%s)", l.stack, mark) }
}

func markName(dst string) string { return "mark_" + strings.ReplaceAll(dst, ".", "_") }

// finishLists stores each pending list from its stack into its destination.
func (g *pgen) finishLists(lists []pendingList) {
	for _, l := range lists {
		stack, mark := l.stack, markName(l.dst)
		if l.mixed {
			g.p("if %s.pushedSince(%s) {\n%s\n}\n", stack, mark, l.set(l.take(mark)))
		} else {
			g.p("%s\n", l.set(l.take(mark)))
		}
	}
}

func (g *pgen) checkHelperLabel(r *rule, e *elem) {
	if e.label != r.returns.name {
		g.fail(e.line, "helper rule '%s' returns '%s'; label '%s' names nothing", r.name, r.returns.name, e.label)
	}
}

func (g *pgen) nodeTarget(n *node) target { return target{node: n} }

func (g *pgen) nodeRule(r *rule) {
	n := g.m.nodeOfRule[r]
	c := cont{rule: r}
	if lead := g.after[r]; lead != nil {
		first := r.alts[0].elems[0]
		g.p("func (p *Parser) %sAfter(t *Token, first %s) %s {\n", parseFunc(r), qualify(g.m.goType(g.m.valueType(lead))), g.nodeType(n))
		g.p("n := p.tree.New%s(t.Start)\n", n.name)
		lists := g.pendingLists(g.nodeTarget(n), r.alts)
		g.assignValue(g.nodeTarget(n), first, "first")
		g.seq(r.alts[0].elems[1:], c, g.nodeTarget(n))
		g.finishLists(lists)
		g.p("return n\n}\n\n")
		g.p("func (p *Parser) %s() %s {\nt := p.cur()\nreturn p.%sAfter(t, %s)\n}\n\n", parseFunc(r), g.nodeType(n), parseFunc(r), g.call(lead))
		return
	}
	g.p("func (p *Parser) %s() %s {\nt := p.cur()\nn := p.tree.New%s(t.Start)\n", parseFunc(r), g.nodeType(n), n.name)
	lists := g.pendingLists(g.nodeTarget(n), r.alts)
	g.alternatives(r.alts, c, r.desc, r.line, g.nodeTarget(n))
	g.finishLists(lists)
	g.p("return n\n}\n\n")
}

func (g *pgen) alternatives(alts []*alt, c cont, desc string, line int, tg target) {
	if len(alts) == 1 {
		g.seq(alts[0].elems, c, tg)
		return
	}
	las := make([]laSet, len(alts))
	for i, a := range alts {
		las[i] = g.a.lookahead(a.elems, c)
	}
	g.choice(las, line, desc, func(i int) { g.seq(alts[i].elems, c, tg) })
}

func (g *pgen) helperRule(r *rule) {
	v := r.returns
	typ := qualify(g.m.goType(v.typ))
	g.p("func (p *Parser) %s() %s {\nvar %s %s\n", parseFunc(r), typ, v.name, typ)
	lists := g.pendingLists(target{helper: r}, r.alts)
	g.alternatives(r.alts, cont{rule: r}, r.desc, r.line, target{helper: r})
	g.finishLists(lists)
	g.p("return %s\n}\n\n", v.name)
}

type factorGroup struct {
	lead    *rule
	members []int
}

// leadOf finds the rule an alternative's node rule starts with, when the set rule could parse it first.
func (g *pgen) leadOf(a *alt) (*rule, bool) {
	ref, ok := g.m.passThrough(a)
	if !ok || g.m.kind[ref] != rkNode || len(ref.alts) != 1 || len(ref.alts[0].elems) == 0 {
		return nil, false
	}
	first := ref.alts[0].elems[0]
	if first.kind != ekRule || first.label == "" || first.suffix != 0 {
		return nil, false
	}
	return g.m.ruleByName[first.text], true
}

func (g *pgen) factorGroups(r *rule) []*factorGroup {
	var groups []*factorGroup
	byLead := map[*rule]*factorGroup{}
	for i, a := range r.alts {
		if a.label != "" {
			continue
		}
		lead, ok := g.leadOf(a)
		if !ok {
			continue
		}
		grp := byLead[lead]
		if grp == nil {
			grp = &factorGroup{lead: lead}
			byLead[lead] = grp
			groups = append(groups, grp)
		}
		grp.members = append(grp.members, i)
	}
	var shared []*factorGroup
	for _, grp := range groups {
		if len(grp.members) > 1 {
			shared = append(shared, grp)
		}
	}
	return shared
}

func (g *pgen) setRule(r *rule) {
	s := g.m.setOfRule[r]
	c := cont{rule: r}
	groups := g.factorGroups(r)
	groupOf := map[int]*factorGroup{}
	for _, grp := range groups {
		for _, i := range grp.members {
			groupOf[i] = grp
		}
	}
	type option struct {
		alt   *alt
		group *factorGroup
	}
	var options []option
	var las []laSet
	needT := len(groups) > 0
	for i, a := range r.alts {
		grp := groupOf[i]
		if grp != nil && grp.members[0] != i {
			continue
		}
		options = append(options, option{alt: a, group: grp})
		if grp == nil {
			las = append(las, g.a.lookahead(a.elems, c))
			needT = needT || a.label != ""
			continue
		}
		la := laSet{}
		for _, m := range grp.members {
			la = union(la, g.a.lookahead(r.alts[m].elems, c))
		}
		las = append(las, la)
	}
	g.p("func (p *Parser) %s() ast.%s {\n", parseFunc(r), s.name)
	if needT {
		g.p("t := p.cur()\n")
	}
	g.choice(las, r.line, r.desc, func(i int) {
		o := options[i]
		switch {
		case o.group != nil:
			g.p("first := %s\n", g.call(o.group.lead))
			rest := make([]laSet, len(o.group.members))
			for k, m := range o.group.members {
				ref, _ := g.m.passThrough(r.alts[m])
				rest[k] = g.a.lookahead(ref.alts[0].elems[1:], cont{rule: ref})
			}
			g.choice(rest, r.line, "", func(k int) {
				ref, _ := g.m.passThrough(r.alts[o.group.members[k]])
				g.p("return %s\n", g.convert(fmt.Sprintf("p.%sAfter(t, first)", parseFunc(ref)), ref, s.name))
			})
		case o.alt.label != "":
			n := g.m.nodeByName[o.alt.label]
			g.p("n := p.tree.New%s(t.Start)\n", n.name)
			lists := g.pendingLists(g.nodeTarget(n), []*alt{o.alt})
			g.seq(o.alt.elems, c, g.nodeTarget(n))
			g.finishLists(lists)
			g.p("return %s\n", g.asSet(n, "n"))
		case len(o.alt.elems) == 1:
			ref, _ := g.m.passThrough(o.alt)
			g.p("return %s\n", g.convert(g.call(ref), ref, s.name))
		default:
			g.p("var v ast.%s\n", s.name)
			g.seq(o.alt.elems, c, target{pass: "v", passSet: s.name})
			g.p("return v\n")
		}
	})
	g.p("}\n\n")
}

// operatorRule emits a left-recursive set rule as precedence climbing: an operand, then operator tails while
// they bind at least as tightly as the caller requires. Earlier operator alternatives bind tighter.
func (g *pgen) operatorRule(r *rule) {
	s := g.m.setOfRule[r]
	isSelf := func(e *elem) bool { return e.kind == ekRule && e.text == r.name && e.suffix == 0 }
	var ops []*alt
	var operands, tails []*alt
	for _, a := range r.alts {
		left := g.m.isLeftRecursive(r, a)
		prefix := !left && len(a.elems) > 1 && isSelf(a.elems[len(a.elems)-1])
		if left || prefix {
			if a.label == "" {
				g.fail(a.line, "an operator alternative builds a node; label it with # Node")
			}
			ops = append(ops, a)
		}
		if left {
			tails = append(tails, a)
		} else {
			operands = append(operands, a)
		}
	}
	prec := map[*alt]int{}
	for i, a := range ops {
		prec[a] = len(ops) - i
	}
	loop := g.a.analysisBody(r)[0].elems[1]
	c := cont{rule: r, parts: [][]*elem{{loop}}}
	precFunc := parseFunc(r) + "Prec"

	g.p("func (p *Parser) %s() ast.%s {\nreturn p.%s(0)\n}\n\n", parseFunc(r), s.name, precFunc)
	g.p("func (p *Parser) %s(minPrec int) ast.%s {\n", precFunc, s.name)
	needT := false
	for _, a := range operands {
		needT = needT || a.label != ""
	}
	if needT {
		g.p("t := p.cur()\n")
	}
	g.p("var left ast.%s\n", s.name)
	las := make([]laSet, len(operands))
	for i, a := range operands {
		las[i] = g.a.lookahead(a.elems, c)
	}
	g.choice(las, r.line, r.desc, func(i int) {
		a := operands[i]
		switch {
		case a.label != "":
			n := g.m.nodeByName[a.label]
			g.p("n := p.tree.New%s(t.Start)\n", n.name)
			lists := g.pendingLists(g.nodeTarget(n), []*alt{a})
			last := len(a.elems) - 1
			if prec[a] > 0 {
				g.seq(a.elems[:last], c.then(a.elems[last:]), g.nodeTarget(n))
				g.assignValue(g.nodeTarget(n), a.elems[last], fmt.Sprintf("p.%s(%d)", precFunc, prec[a]))
			} else {
				g.seq(a.elems, c, g.nodeTarget(n))
			}
			g.finishLists(lists)
			g.p("left = %s\n", g.asSet(n, "n"))
		case len(a.elems) == 1:
			ref, _ := g.m.passThrough(a)
			g.p("left = %s\n", g.convert(g.call(ref), ref, s.name))
		default:
			g.seq(a.elems, c, target{pass: "left", passSet: s.name})
		}
	})

	g.p("for {\nswitch p.cur().Kind() {\n")
	owner := map[string]*alt{}
	for _, a := range tails {
		first, nullable := g.a.firstOne(a.elems[1])
		if nullable {
			g.fail(a.line, "an operator tail must start with a token")
		}
		var terms []string
		for t := range first {
			if owner[t] != nil {
				g.fail(a.line, "operator %s also starts an earlier operator alternative", g.m.describeTerminal(t))
			}
			owner[t] = a
			terms = append(terms, t)
		}
		sort.Strings(terms)
		n := g.m.nodeByName[a.label]
		tg := g.nodeTarget(n)
		g.p("case %s:\nif %d < minPrec {\nreturn left\n}\n", strings.Join(terms, ", "), prec[a])
		g.p("n := p.tree.New%s(p.cur().Start)\n", n.name)
		lists := g.pendingLists(tg, []*alt{a})
		g.assignValue(tg, a.elems[0], "left")
		last := len(a.elems) - 1
		if isSelf(a.elems[last]) {
			right := prec[a] + 1
			if a.assocRight {
				right = prec[a]
			}
			g.seq(a.elems[1:last], c.then(a.elems[last:]), tg)
			g.assignValue(tg, a.elems[last], fmt.Sprintf("p.%s(%d)", precFunc, right))
		} else {
			g.seq(a.elems[1:], c, tg)
		}
		g.finishLists(lists)
		g.p("left = %s\n", g.asSet(n, "n"))
	}
	g.p("default:\nreturn left\n}\n}\n}\n\n")
}

// nodeType is the Go type that the function parsing a node returns: the view of a record, or a pointer to a struct.
func (g *pgen) nodeType(n *node) string {
	if n.record {
		return "ast." + n.name
	}
	return "*ast." + n.name
}

// asSet is the value of the node n, which is in a set, as that set's type: a record is wrapped in the set's view.
func (g *pgen) asSet(n *node, value string) string {
	if n.record && n.set != nil {
		return value + "." + n.set.name + "()"
	}
	return value
}
