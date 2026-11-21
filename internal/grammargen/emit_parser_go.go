package main

import (
	"fmt"
	"strings"
)

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
	tailsOpen(all []string)
	tailOpen(i int, terms []string)
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

// goLang writes the Go parser, internal/syntax/parser_gen.go.
type goLang struct {
	g *pgen
	w *writer
}

func (l *goLang) start(g *pgen, body *writer) { l.g, l.w = g, body }

func (l *goLang) p(format string, args ...any) { l.w.p(format, args...) }

func (l *goLang) file(body string) string {
	g := l.g
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
	w.b.WriteString(body)
	return w.b.String()
}

func (l *goLang) at(term string) string     { return "p.at(" + term + ")" }
func (l *goLang) in(set string) string      { return "p.in(&" + set + ")" }
func (l *goLang) peekIs(term string) string { return "p.peekIs(" + term + ")" }
func (l *goLang) peekIn(set string) string  { return "p.peekIn(&" + set + ")" }
func (l *goLang) and(a, b string) string    { return a + " && " + b }
func (l *goLang) or(parts []cond) string    { return joinConds(parts, " || ") }
func (l *goLang) not(c string) string       { return "!" + c }

func (l *goLang) less(prec int, v string) string { return fmt.Sprintf("%d < %s", prec, v) }

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

func (l *goLang) hasSwitch() bool { return true }
func (l *goLang) switchOpen()     { l.p("switch p.cur().Kind() {\n") }
func (l *goLang) caseOpen(terms []string) {
	l.p("case %s:\n", strings.Join(terms, ", "))
}
func (l *goLang) defaultOpen()        { l.p("default:\n") }
func (l *goLang) switchClose()        { l.p("}\n") }
func (l *goLang) ifOpen(c string)     { l.p("if %s {\n", c) }
func (l *goLang) elseIfOpen(c string) { l.p("} else if %s {\n", c) }
func (l *goLang) elseOpen()           { l.p("} else {\n") }
func (l *goLang) whileOpen(c string)  { l.p("for %s {\n", c) }
func (l *goLang) close()              { l.p("}\n") }
func (l *goLang) tailsOpen([]string)  { l.p("for {\nswitch p.cur().Kind() {\n") }
func (l *goLang) tailOpen(_ int, terms []string) {
	l.caseOpen(terms)
}
func (l *goLang) tailsClose(left string) { l.p("default:\nreturn %s\n}\n}\n", left) }
func (l *goLang) refuse(desc string)     { l.p("panic(p.expected(%q))\n", desc) }

func (l *goLang) expect(term, desc string) string { return fmt.Sprintf("p.expect(%s, %q)", term, desc) }
func (l *goLang) next() string                    { return "p.next()" }
func (l *goLang) call(r *rule) string             { return "p." + parseFunc(r) + "()" }
func (l *goLang) callAfter(r *rule, first string) string {
	return fmt.Sprintf("p.%sAfter(t, %s)", parseFunc(r), first)
}
func (l *goLang) callPrec(fn, prec string) string { return fmt.Sprintf("p.%s(%s)", fn, prec) }

func (l *goLang) stmt(expr string)                    { l.p("%s\n", expr) }
func (l *goLang) line(stmt string)                    { l.p("%s\n", stmt) }
func (l *goLang) assign(dst, value string)            { l.p("%s = %s\n", dst, value) }
func (l *goLang) assignText(dst, value string) string { return dst + " = " + value }
func (l *goLang) setJoin()                            { l.p("p.joinNext = true\n") }
func (l *goLang) joinsOpen(c string) string {
	l.p("{\njoins := %s\n", c)
	return "joins"
}
func (l *goLang) joinsClose(joins string) { l.p("p.joinNext = p.joinNext || %s\n}\n", joins) }
func (l *goLang) bind(expr string) string { return expr }
func (l *goLang) text(tok string) string  { return "p.text(" + tok + ")" }
func (l *goLang) textRef(tok string) string {
	return "p.textRef(" + tok + ")"
}
func (l *goLang) conversion(n *node, f *field, tok string) string {
	return fmt.Sprintf("p.%s%s(%s)", lowerFirst(n.name), f.name, tok)
}
func (l *goLang) setStructField(_ *node, f *field, value string) string {
	return fmt.Sprintf("n.%s = %s", f.name, value)
}
func (l *goLang) setRecordField(_ *node, fp *fieldPlan, value string) string {
	switch fp.st {
	case stText, stLabel, stWord:
		return fmt.Sprintf("n.Set%sRef(%s)", fp.f.name, value)
	}
	return fmt.Sprintf("n.Set%s(%s)", fp.f.name, value)
}
func (l *goLang) setRecordFlag(_ *node, fp *fieldPlan) string {
	return fmt.Sprintf("n.Set%s()", fp.f.name)
}

// convert is the view of a record as its set, where the set is what a field, a list or a variable holds; a struct is
// its set's interface as it is.
func (l *goLang) convert(n *node, dst, value string) string {
	if n.record && n.set != nil && n.set.name == dst && n.name != dst {
		return value + "." + dst + "()"
	}
	return value
}

func (l *goLang) asSet(n *node, value string) string {
	if n.record && n.set != nil {
		return value + "." + n.set.name + "()"
	}
	return value
}

// pushed is the element a list of the destination holds, for the value of a rule: a record is its number.
func (l *goLang) pushed(typ, value string) string {
	if _, _, ok := l.g.m.refersToRecord(typ); ok {
		return "uint32(" + value + ".ID)"
	}
	return value
}

func (l *goLang) push(stack, value string) { l.p("%s.push(%s)\n", stack, value) }

func (l *goLang) mark(name, stack string) { l.p("%s := %s.mark()\n", name, stack) }
func (l *goLang) takeRecords(elem, stack, mark string) string {
	return fmt.Sprintf("takeList[%s](p, &%s, %s)", qualify(l.g.m.goType(elem)), stack, mark)
}
func (l *goLang) takeTexts(stack, mark string) string {
	return fmt.Sprintf("takeTexts(p, &%s, %s)", stack, mark)
}
func (l *goLang) takeStructs(stack, mark string) string {
	return fmt.Sprintf("%s.take(%s)", stack, mark)
}
func (l *goLang) pushedSince(stack, mark string) string {
	return fmt.Sprintf("%s.pushedSince(%s)", stack, mark)
}

func (l *goLang) funcOpen(name string, params []param, ret string) {
	ps := make([]string, len(params))
	for i, p := range params {
		ps[i] = p.name + " " + p.typ
	}
	l.p("func (p *Parser) %s(%s) %s {\n", name, strings.Join(ps, ", "), ret)
}
func (l *goLang) funcClose()        { l.p("}\n\n") }
func (l *goLang) startType() string { return "*Token" }
func (l *goLang) valueType(r *rule) string {
	return qualify(l.g.m.goType(l.g.m.valueType(r)))
}

// nodeType is the Go type that the function parsing a node returns: the view of a record, or a pointer to a struct.
func (l *goLang) nodeType(n *node) string {
	if n.record {
		return "ast." + n.name
	}
	return "*ast." + n.name
}
func (l *goLang) setType(s *set) string        { return "ast." + s.name }
func (l *goLang) helperType(typ string) string { return qualify(l.g.m.goType(typ)) }
func (l *goLang) precType() string             { return "int" }
func (l *goLang) startVar()                    { l.p("t := p.cur()\n") }
func (l *goLang) startOf() string              { return "t.Start" }
func (l *goLang) curStart() string             { return "p.cur().Start" }
func (l *goLang) newNode(n *node, start string) {
	l.p("n := p.tree.New%s(%s)\n", n.name, start)
}

// endOfNode records, for a node that is a struct, how much of the text it covers: from where it began to where the last token
// it consumed ends. A record in the tree has no room for it, and nothing reads it there.
func (l *goLang) endOfNode(n *node) {
	if !n.record {
		l.p("n.Len = uint32(p.prevEnd) - n.Off\n")
	}
}
func (l *goLang) ret(value string) { l.p("return %s\n", value) }
func (l *goLang) helperVar(name, typ string) {
	l.p("var %s %s\n", name, l.helperType(typ))
}
func (l *goLang) firstVar(call string)       { l.p("first := %s\n", call) }
func (l *goLang) setVar(name string, s *set) { l.p("var %s %s\n", name, l.setType(s)) }
