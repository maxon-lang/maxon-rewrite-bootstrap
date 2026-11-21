package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// pgen walks the grammar and makes every decision of the parser: which lookahead picks an alternative, where each
// value goes and which lists are built on which stack. It writes the parser through a lang, which spells the
// decisions in Maxon.
type pgen struct {
	m *model
	a *analysis
	l lang
	// after holds the node rules whose first element a set rule parses before choosing them, mapped to that
	// element's rule; each gets a parse<Node>After that continues from it.
	after   map[*rule]*rule
	sets    map[string]string
	setList []string
	// setTerms are the token kinds of each set, by name.
	setTerms map[string][]string
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

// cond is a condition the parser tests, and whether it joins two tests, so that a list of them is parenthesized.
type cond struct {
	text     string
	compound bool
}

// walkParser makes the decisions of the parser and has l write them; l writes the file around them.
func walkParser(m *model, a *analysis, l lang) string {
	body := &writer{}
	g := &pgen{m: m, a: a, l: l, after: map[*rule]*rule{}, sets: map[string]string{}, setTerms: map[string][]string{}}
	l.start(g, body)
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
	return l.file(body.b.String())
}

func (g *pgen) fail(line int, format string, args ...any) { g.m.fail(line, format, args...) }

func parseFunc(r *rule) string { return "parse" + capitalize(r.name) }

func lowerFirst(s string) string { return strings.ToLower(s[:1]) + s[1:] }

// tokenSet names a table of the given token kinds, shared by identical sets.
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
	g.setTerms[name] = terms
	g.setList = append(g.setList, name)
	return name
}

func (g *pgen) atAny(terms []string) string {
	if len(terms) == 1 {
		return g.l.at(terms[0])
	}
	return g.l.in(g.tokenSet(terms))
}

func (g *pgen) peekAny(terms []string) string {
	if len(terms) == 1 {
		return g.l.peekIs(terms[0])
	}
	return g.l.peekIn(g.tokenSet(terms))
}

// test builds the condition that the next tokens begin one of the strings in la. A first token that no later
// alternative can begin with is tested alone; the alternatives are tried in order, so earlier ones have taken
// what they share with it.
func (g *pgen) test(la laSet, later []laSet) string {
	var single []string
	var parts []cond
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
		parts = append(parts, cond{g.l.and(g.l.at(t), g.peekAny(seconds)), true})
	}
	if len(single) > 0 {
		parts = append([]cond{{g.atAny(single), false}}, parts...)
	}
	return g.l.or(parts)
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
	return joinOr(g.m.describeTerminals(firstTerminals(all)))
}

// choice emits a decision between alternatives with the given lookahead sets, calling branch for each.
func (g *pgen) choice(las []laSet, line int, desc string, branch func(i int)) {
	g.choicePredicated(las, nil, line, desc, branch)
}

// guardOf is the predicate that follows the operator of a tail, which the tail is entered only where it holds, or "".
func guardOf(a *alt) string {
	if len(a.elems) > 2 && a.elems[2].kind == ekPredicate {
		return a.elems[2].text
	}
	return ""
}

// predicateOf is the hand-written predicate that an alternative starts with, which decides it where the tokens cannot, or "".
func predicateOf(a *alt) string {
	if len(a.elems) > 0 && a.elems[0].kind == ekPredicate {
		return a.elems[0].text
	}
	return ""
}

// choicePredicated is choice where the alternative i is also entered only where the predicate preds[i] holds, if it has one.
// An alternative with a predicate may start as a later one does, and does not shadow it.
func (g *pgen) choicePredicated(las []laSet, preds []string, line int, desc string, branch func(i int)) {
	predicated := func(i int) bool { return i < len(preds) && preds[i] != "" }
	var unconditional []laSet
	for i, l := range las {
		shadowed := len(l) > 0
		for s := range l {
			shadowed = shadowed && covered(s, unconditional)
		}
		if shadowed {
			g.fail(line, "alternative %d can never be chosen: every way it can start, an earlier alternative starts", i+1)
		}
		if !predicated(i) {
			unconditional = append(unconditional, l)
		}
	}
	if desc == "" {
		desc = g.describe(las)
	}
	disjoint := len(preds) == 0
	seen := map[string]bool{}
	for _, l := range las {
		for _, t := range firstTerminals(l) {
			disjoint = disjoint && !seen[t]
			seen[t] = true
		}
	}
	if disjoint && g.l.hasSwitch() {
		g.l.switchOpen()
		for i, l := range las {
			g.l.caseOpen(firstTerminals(l))
			branch(i)
		}
		g.l.defaultOpen()
		g.l.refuse(desc)
		g.l.switchClose()
		return
	}
	for i, l := range las {
		test := g.test(l, las[i+1:])
		if predicated(i) {
			test = g.l.and(test, g.l.predicate(preds[i]))
		}
		if i == 0 {
			g.l.ifOpen(test)
		} else {
			g.l.elseIfOpen(test)
		}
		branch(i)
	}
	g.l.elseOpen()
	g.l.refuse(desc)
	g.l.close()
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
	test := g.test(enter, []laSet{exit})
	if e.kind == ekGroup && len(e.group) == 1 && e.group[0].elems[0].kind == ekPredicate {
		return g.l.and(test, g.l.predicate(e.group[0].elems[0].text))
	}
	return test
}

func (g *pgen) elem(e *elem, c cont, tg target) {
	switch e.suffix {
	case 0:
		g.once(e, c, tg)
	case '?':
		g.l.ifOpen(g.enterTest(e, c))
		g.once(e, c, tg)
		g.l.close()
	case '*':
		g.l.whileOpen(g.enterTest(e, c))
		g.once(e, c.then([]*elem{starOf(e)}), tg)
		g.l.close()
	case '+':
		g.once(e, c.then([]*elem{starOf(e)}), tg)
		g.l.whileOpen(g.enterTest(e, c))
		g.once(e, c.then([]*elem{starOf(e)}), tg)
		g.l.close()
	}
}

func (g *pgen) once(e *elem, c cont, tg target) {
	switch e.kind {
	case ekLiteral, ekToken:
		term := g.m.terminal(e)
		call := g.l.expect(term, g.m.describeTerminal(term))
		if e.joinBefore {
			g.l.setJoin()
		}
		if e.label == "" {
			g.l.stmt(call)
		} else {
			g.assignToken(tg, e, call)
		}
		if e.joinAfter {
			g.l.setJoin()
		}
	case ekRule:
		r := g.m.ruleByName[e.text]
		call := g.l.call(r)
		switch {
		case e.label != "":
			g.assignValue(tg, e, call)
		case tg.pass != "":
			g.l.assign(tg.pass, g.convert(g.l.bind(call), r, tg.passSet))
		default:
			g.l.stmt(call)
		}
	case ekPredicate:
		// enterTest has tested it.
	case ekGroup:
		if e.label != "" && g.m.groupType(e) == "string" {
			var terms []string
			for _, a := range e.group {
				terms = append(terms, g.m.terminal(a.elems[0]))
			}
			sort.Strings(terms)
			var named []string
			for _, t := range terms {
				if e.anyKeyword && strings.HasPrefix(t, keywordPrefix) {
					continue
				}
				named = append(named, t)
			}
			g.l.ifOpen(g.l.not(g.atAny(terms)))
			g.l.refuse(joinOr(g.m.describeTerminals(named)))
			g.l.close()
			// A ^ on one of the literals joins it, and only it, to its neighbour.
			for _, a := range e.group {
				if a.elems[0].joinBefore {
					g.l.ifOpen(g.l.at(g.m.terminal(a.elems[0])))
					g.l.setJoin()
					g.l.close()
				}
			}
			var joinsAfter []string
			for _, a := range e.group {
				if a.elems[0].joinAfter {
					joinsAfter = append(joinsAfter, g.m.terminal(a.elems[0]))
				}
			}
			if len(joinsAfter) == 0 {
				g.assignToken(tg, e, g.l.next())
				return
			}
			joins := g.l.joinsOpen(g.atAny(joinsAfter))
			g.assignToken(tg, e, g.l.next())
			g.l.joinsClose(joins)
			return
		}
		if len(e.group) == 1 && e.label == "" {
			g.seq(e.group[0].elems, c, tg)
			return
		}
		las := make([]laSet, len(e.group))
		var preds []string
		for i, a := range e.group {
			las[i] = g.a.lookahead(a.elems, c)
			if p := predicateOf(a); p != "" {
				if preds == nil {
					preds = make([]string, len(e.group))
				}
				preds[i] = p
			}
		}
		g.choicePredicated(las, preds, e.line, "", func(i int) {
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

// setField is the statement that stores value in a field of a record or a struct.
func (g *pgen) setField(tg target, f *field, value string) string {
	if !tg.node.record {
		return g.l.setStructField(tg.node, f, value)
	}
	return g.l.setRecordField(tg.node, g.fieldPlan(tg.node, f), value)
}

// elemName is the node or set a type names, without its list brackets or pointer: "*Arg", "[]Expr" and "Expr" name Arg,
// Expr and Expr.
func elemName(typ string) string {
	return strings.TrimPrefix(strings.TrimPrefix(typ, "[]"), "*")
}

// convert is the value of the rule's node as the set dst, when the node is a member of it and the set is what a field,
// a list or a variable holds.
func (g *pgen) convert(value string, src *rule, dst string) string {
	if src == nil || g.m.kind[src] != rkNode {
		return value
	}
	return g.l.convert(g.m.nodeOfRule[src], dst, value)
}

// inSet is whether the node is a member of the set of the name, directly or through a set that is part of it.
func (m *model) inSet(n *node, name string) bool {
	for s := n.set; s != nil; s = s.parent {
		if s.name == name {
			return true
		}
	}
	return false
}

func (g *pgen) assignToken(tg target, e *elem, call string) {
	if tg.helper != nil {
		g.checkHelperLabel(tg.helper, e)
		tok := g.l.bind(call)
		g.storeText(tg.helper.returns.name, tg.helper.returns.typ, e.appendTo, g.l.text(tok))
		return
	}
	if tg.node == nil {
		g.fail(e.line, "label '%s' has no node to go in", e.label)
	}
	f := g.m.fieldOf(tg.node, e.label, e.line)
	if !tg.node.record {
		switch strings.TrimPrefix(f.typ, "[]") {
		case "bool":
			if e.kind != ekLiteral || e.appendTo {
				g.fail(e.line, "a bool field takes a single literal")
			}
			g.l.stmt(call)
			g.l.line(g.setField(tg, f, "true"))
		case "string":
			g.storeStructText(tg, f, e.appendTo, g.l.text(g.l.bind(call)))
		default:
			g.storeStructText(tg, f, e.appendTo, g.l.bind(g.l.conversion(tg.node, f, g.l.bind(call))))
		}
		return
	}
	fp := g.fieldPlan(tg.node, f)
	switch fp.st {
	case stBool:
		if e.kind != ekLiteral || e.appendTo {
			g.fail(e.line, "a bool field takes a single literal")
		}
		g.l.stmt(call)
		g.l.line(g.l.setRecordFlag(tg.node, fp))
	case stEnum:
		g.l.line(g.setField(tg, f, g.l.bind(g.l.text(g.l.bind(call)))))
	case stText, stLabel:
		g.l.line(g.setField(tg, f, g.l.bind(g.l.textRef(g.l.bind(call)))))
	case stTexts:
		if !e.appendTo {
			g.fail(e.line, "a list of texts takes +=")
		}
		g.l.push(g.stackFor(tg, f.name, f.typ), g.l.bind(g.l.textRef(g.l.bind(call))))
	case stI128, stWord:
		g.l.line(g.setField(tg, f, g.l.bind(g.l.conversion(tg.node, f, g.l.bind(call)))))
	default:
		g.fail(e.line, "field %s of %s holds nodes, and a token is not one", f.name, tg.node.name)
	}
}

// storeStructText stores a token's value in a field of a struct, or pushes it on the stack of the field's list.
func (g *pgen) storeStructText(tg target, f *field, appendTo bool, value string) {
	if appendTo {
		g.l.push(g.stackFor(tg, f.name, f.typ), value)
		return
	}
	g.l.line(g.setField(tg, f, value))
}

// storeText assigns a token's text to a helper's variable, or pushes it on the stack of its list.
func (g *pgen) storeText(dst, typ string, appendTo bool, value string) {
	if appendTo {
		g.l.push("p.lists."+g.listOf(typ), value)
	} else {
		g.l.assign(dst, value)
	}
}

func (g *pgen) assignValue(tg target, e *elem, call string) {
	src := g.m.ruleByName[e.text]
	value := g.l.bind(call)
	if tg.helper != nil {
		g.checkHelperLabel(tg.helper, e)
		typ := tg.helper.returns.typ
		value = g.convert(value, src, elemName(typ))
		if e.appendTo {
			g.l.push(g.stackFor(tg, "", typ), g.l.pushed(typ, value))
		} else {
			g.l.assign(tg.helper.returns.name, value)
		}
		return
	}
	if tg.node == nil {
		g.fail(e.line, "label '%s' has no node to go in", e.label)
	}
	f := g.m.fieldOf(tg.node, e.label, e.line)
	value = g.convert(value, src, elemName(f.typ))
	if e.appendTo {
		g.l.push(g.stackFor(tg, f.name, f.typ), g.l.pushed(f.typ, value))
		return
	}
	g.l.line(g.setField(tg, f, value))
}

// stackFor is the stack that a list of the given type is built on: the one of its own for a list of records or texts in
// the tree, which holds their numbers, and for a list of structs the one for its element type. A field is named by its
// node, and a helper's list by the helper.
func (g *pgen) stackFor(tg target, field, typ string) string {
	_, _, records := g.m.refersToRecord(typ)
	if !records && !(typ == "[]string" && tg.node != nil && tg.node.record) {
		return "p.lists." + g.listOf(typ)
	}
	name := idStackPrefix
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

// idStackPrefix starts the name of a stack of the numbers of records or texts.
const idStackPrefix = "ids_"

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
			l = pendingList{dst: name, typ: tg.helper.returns.typ, set: func(x string) string { return g.l.assignText(name, x) }}
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
		g.l.mark(markName(lists[i].dst), lists[i].stack)
	}
	return lists
}

// takeList is the code that makes the list from what its stack holds above a mark.
func (g *pgen) takeList(l pendingList) func(mark string) string {
	_, _, records := g.m.refersToRecord(l.typ)
	switch {
	case records:
		elem := elemName(l.typ)
		return func(mark string) string { return g.l.takeRecords(elem, l.stack, mark) }
	case strings.HasPrefix(l.stack, "p.lists."+idStackPrefix):
		return func(mark string) string { return g.l.takeTexts(l.stack, mark) }
	}
	return func(mark string) string { return g.l.takeStructs(l.stack, mark) }
}

func markName(dst string) string { return "mark_" + strings.ReplaceAll(dst, ".", "_") }

// finishLists stores each pending list from its stack into its destination.
func (g *pgen) finishLists(lists []pendingList) {
	for _, l := range lists {
		stack, mark := l.stack, markName(l.dst)
		if l.mixed {
			g.l.ifOpen(g.l.pushedSince(stack, mark))
			g.l.line(l.set(g.l.bind(l.take(mark))))
			g.l.close()
		} else {
			g.l.line(l.set(g.l.bind(l.take(mark))))
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
		g.l.funcOpen(parseFunc(r)+"After", []param{{"t", g.l.startType()}, {"first", g.l.valueType(lead)}}, g.l.nodeType(n))
		g.l.newNode(n, g.l.startOf())
		lists := g.pendingLists(g.nodeTarget(n), r.alts)
		g.assignValue(g.nodeTarget(n), first, "first")
		g.seq(r.alts[0].elems[1:], c, g.nodeTarget(n))
		g.finishLists(lists)
		g.l.endOfNode(n)
		g.l.ret("n")
		g.l.funcClose()
		g.l.funcOpen(parseFunc(r), nil, g.l.nodeType(n))
		g.l.startVar()
		g.l.ret(g.l.callAfter(r, g.l.bind(g.l.call(lead))))
		g.l.funcClose()
		return
	}
	g.l.funcOpen(parseFunc(r), nil, g.l.nodeType(n))
	g.l.startVar()
	g.l.newNode(n, g.l.startOf())
	lists := g.pendingLists(g.nodeTarget(n), r.alts)
	g.alternatives(r.alts, c, r.desc, r.line, g.nodeTarget(n))
	g.finishLists(lists)
	g.l.endOfNode(n)
	g.l.ret("n")
	g.l.funcClose()
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
	g.l.funcOpen(parseFunc(r), nil, g.l.helperType(v.typ))
	g.l.helperVar(v.name, v.typ)
	lists := g.pendingLists(target{helper: r}, r.alts)
	g.alternatives(r.alts, cont{rule: r}, r.desc, r.line, target{helper: r})
	g.finishLists(lists)
	g.l.ret(v.name)
	g.l.funcClose()
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
	var preds []string
	needT := len(groups) > 0
	for i, a := range r.alts {
		grp := groupOf[i]
		if grp != nil && grp.members[0] != i {
			continue
		}
		options = append(options, option{alt: a, group: grp})
		if grp == nil {
			las = append(las, g.a.lookahead(a.elems, c))
			preds = append(preds, predicateOf(a))
			needT = needT || a.label != ""
			continue
		}
		la := laSet{}
		for _, m := range grp.members {
			la = union(la, g.a.lookahead(r.alts[m].elems, c))
		}
		las = append(las, la)
		preds = append(preds, "")
	}
	g.l.funcOpen(parseFunc(r), nil, g.l.setType(s))
	if needT {
		g.l.startVar()
	}
	g.choicePredicated(las, preds, r.line, r.desc, func(i int) {
		o := options[i]
		switch {
		case o.group != nil:
			g.l.firstVar(g.l.call(o.group.lead))
			rest := make([]laSet, len(o.group.members))
			for k, m := range o.group.members {
				ref, _ := g.m.passThrough(r.alts[m])
				rest[k] = g.a.lookahead(ref.alts[0].elems[1:], cont{rule: ref})
			}
			g.choice(rest, r.line, "", func(k int) {
				ref, _ := g.m.passThrough(r.alts[o.group.members[k]])
				g.l.ret(g.convert(g.l.bind(g.l.callAfter(ref, "first")), ref, s.name))
			})
		case o.alt.label != "":
			n := g.m.nodeByName[o.alt.label]
			g.l.newNode(n, g.l.startOf())
			lists := g.pendingLists(g.nodeTarget(n), []*alt{o.alt})
			g.seq(o.alt.elems, c, g.nodeTarget(n))
			g.finishLists(lists)
			g.l.ret(g.l.asSet(n, "n"))
		case len(o.alt.elems) == 1:
			ref, _ := g.m.passThrough(o.alt)
			g.l.ret(g.convert(g.l.bind(g.l.call(ref)), ref, s.name))
		default:
			g.l.setVar("v", s)
			g.seq(o.alt.elems, c, target{pass: "v", passSet: s.name})
			g.l.ret("v")
		}
	})
	g.l.funcClose()
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

	g.l.funcOpen(parseFunc(r), nil, g.l.setType(s))
	g.l.ret(g.l.callPrec(precFunc, "0"))
	g.l.funcClose()
	g.l.funcOpen(precFunc, []param{{"minPrec", g.l.precType()}}, g.l.setType(s))
	needT := false
	for _, a := range operands {
		needT = needT || a.label != ""
	}
	if needT {
		g.l.startVar()
	}
	g.l.setVar("left", s)
	las := make([]laSet, len(operands))
	preds := make([]string, len(operands))
	for i, a := range operands {
		las[i] = g.a.lookahead(a.elems, c)
		preds[i] = predicateOf(a)
	}
	g.choicePredicated(las, preds, r.line, r.desc, func(i int) {
		a := operands[i]
		switch {
		case a.label != "":
			n := g.m.nodeByName[a.label]
			g.l.newNode(n, g.l.startOf())
			lists := g.pendingLists(g.nodeTarget(n), []*alt{a})
			last := len(a.elems) - 1
			if prec[a] > 0 {
				g.seq(a.elems[:last], c.then(a.elems[last:]), g.nodeTarget(n))
				g.assignValue(g.nodeTarget(n), a.elems[last], g.l.callPrec(precFunc, fmt.Sprint(prec[a])))
			} else {
				g.seq(a.elems, c, g.nodeTarget(n))
			}
			g.finishLists(lists)
			g.l.assign("left", g.l.asSet(n, "n"))
		case len(a.elems) == 1:
			ref, _ := g.m.passThrough(a)
			g.l.assign("left", g.convert(g.l.bind(g.l.call(ref)), ref, s.name))
		default:
			g.seq(a.elems, c, target{pass: "left", passSet: s.name})
		}
	})

	owner := map[string]*alt{}
	tailTerms := make([][]string, len(tails))
	var all []string
	for i, a := range tails {
		first, nullable := g.a.firstOne(a.elems[1])
		if nullable {
			g.fail(a.line, "an operator tail must start with a token")
		}
		for t := range first {
			if owner[t] != nil {
				g.fail(a.line, "operator %s also starts an earlier operator alternative", g.m.describeTerminal(t))
			}
			owner[t] = a
			tailTerms[i] = append(tailTerms[i], t)
		}
		sort.Strings(tailTerms[i])
		all = append(all, tailTerms[i]...)
	}
	sort.Strings(all)
	// A tail whose operator is followed by a predicate is guarded: the operator starts a tail only where the predicate holds,
	// which the loop over the tails tests too, so that the operator can start whatever follows the expression.
	tailTests := make([]string, len(tails))
	var loopParts []cond
	var free []string
	for i, a := range tails {
		tailTests[i] = g.atAny(tailTerms[i])
		if guard := guardOf(a); guard != "" {
			tailTests[i] = g.l.and(tailTests[i], g.l.predicate(guard))
			loopParts = append(loopParts, cond{tailTests[i], true})
		} else {
			free = append(free, tailTerms[i]...)
		}
	}
	if len(free) > 0 {
		sort.Strings(free)
		loopParts = append([]cond{{g.atAny(free), false}}, loopParts...)
	}
	g.l.tailsOpen(g.l.or(loopParts))
	for i, a := range tails {
		n := g.m.nodeByName[a.label]
		tg := g.nodeTarget(n)
		g.l.tailOpen(i, tailTests[i])
		g.l.ifOpen(g.l.less(prec[a], "minPrec"))
		g.l.ret("left")
		g.l.close()
		g.l.newNode(n, g.l.curStart())
		lists := g.pendingLists(tg, []*alt{a})
		g.assignValue(tg, a.elems[0], "left")
		last := len(a.elems) - 1
		if isSelf(a.elems[last]) {
			right := prec[a] + 1
			if a.assocRight {
				right = prec[a]
			}
			g.seq(a.elems[1:last], c.then(a.elems[last:]), tg)
			g.assignValue(tg, a.elems[last], g.l.callPrec(precFunc, fmt.Sprint(right)))
		} else {
			g.seq(a.elems[1:], c, tg)
		}
		g.finishLists(lists)
		g.l.assign("left", g.l.asSet(n, "n"))
	}
	g.l.tailsClose("left")
	g.l.funcClose()
}

// lang writes the parser that pgen decides in one language. Expressions come back as text; statements are written
// to the body. pgen makes every decision, so a lang only spells them: what to test, where a value goes, which list
// it is pushed on.
type lang interface {
	// start gives the lang the walk it writes for and the body it writes to; file wraps the body in the rest of the file.
	start(g *pgen, body *writer)
	file(body string) string

	// Conditions on the current and the next token, and their combinations.
	at(term string) string
	in(set string) string
	peekIs(term string) string
	peekIn(set string) string
	and(a, b string) string
	// predicate is a call of the hand-written parser method name, which gives whether the parser is where a {name}? stands.
	predicate(name string) string
	or(parts []cond) string
	not(c string) string
	less(prec int, v string) string

	// Control flow. A lang without a switch on the token kind decides every choice with an if chain.
	hasSwitch() bool
	switchOpen()
	caseOpen(terms []string)
	defaultOpen()
	switchClose()
	ifOpen(c string)
	elseIfOpen(c string)
	elseOpen()
	whileOpen(c string)
	close()
	// tailsOpen starts the loop over operator tails, which start with the tokens all; tailOpen starts the i-th tail,
	// and tailsClose ends the loop and returns left.
	tailsOpen(test string)
	tailOpen(i int, test string)
	tailsClose(left string)
	refuse(desc string)

	// Calls that consume tokens or parse a rule.
	expect(term, desc string) string
	next() string
	call(r *rule) string
	callAfter(r *rule, first string) string
	callPrec(fn, prec string) string

	// Statements and the values they store.
	stmt(expr string)
	line(stmt string)
	assign(dst, value string)
	assignText(dst, value string) string
	setJoin()
	joinsOpen(c string) string
	joinsClose(joins string)
	// bind is a value as something that can be used where another value of the parser is being changed: in a
	// language that refuses overlapping access, a local that holds it.
	bind(expr string) string
	text(tok string) string
	textRef(tok string) string
	conversion(n *node, f *field, tok string) string
	setStructField(n *node, f *field, value string) string
	setRecordField(n *node, fp *fieldPlan, value string) string
	setRecordFlag(n *node, fp *fieldPlan) string
	convert(n *node, dst, value string) string
	asSet(n *node, value string) string
	pushed(typ, value string) string
	push(stack, value string)

	// The stacks lists are built on.
	mark(name, stack string)
	takeRecords(elem, stack, mark string) string
	takeTexts(stack, mark string) string
	takeStructs(stack, mark string) string
	pushedSince(stack, mark string) string

	// Functions, their types and their locals.
	funcOpen(name string, params []param, ret string)
	funcClose()
	startType() string
	valueType(r *rule) string
	nodeType(n *node) string
	setType(s *set) string
	helperType(typ string) string
	precType() string
	startVar()
	startOf() string
	curStart() string
	newNode(n *node, start string)
	endOfNode(n *node)
	ret(value string)
	helperVar(name, typ string)
	firstVar(call string)
	setVar(name string, s *set)
}

// param is a parameter of a generated function.
type param struct{ name, typ string }

// joinConds joins conditions with op, parenthesizing each that joins two tests when there is more than one.
func joinConds(parts []cond, op string) string {
	if len(parts) == 1 {
		return parts[0].text
	}
	texts := make([]string, len(parts))
	for i, c := range parts {
		texts[i] = c.text
		if c.compound {
			texts[i] = "(" + c.text + ")"
		}
	}
	return strings.Join(texts, op)
}
