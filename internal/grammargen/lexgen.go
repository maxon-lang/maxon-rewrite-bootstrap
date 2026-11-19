package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// A token rule's body is a regular expression over bytes, compiled with every other token into one DFA.

type rxKind int

const (
	rxBytes rxKind = iota // one byte from set
	rxSeq
	rxAlt
	rxStar
	rxPlus
	rxOpt
)

type byteSet [256]bool

type rx struct {
	kind rxKind
	set  byteSet
	subs []*rx
}

func literalRx(text string) *rx {
	seq := &rx{kind: rxSeq}
	for i := 0; i < len(text); i++ {
		b := &rx{kind: rxBytes}
		b.set[text[i]] = true
		seq.subs = append(seq.subs, b)
	}
	return seq
}

func (p *grammarParser) lexAlts(t *tokenDef) *rx {
	alt := &rx{kind: rxAlt, subs: []*rx{p.lexSeq(t)}}
	for p.isPunct("|") {
		p.next()
		alt.subs = append(alt.subs, p.lexSeq(t))
	}
	if len(alt.subs) == 1 {
		return alt.subs[0]
	}
	return alt
}

func (p *grammarParser) lexSeq(t *tokenDef) *rx {
	seq := &rx{kind: rxSeq}
	for !p.isPunct("|") && !p.isPunct(")") && !p.isPunct(";") && !p.isPunct("->") {
		if p.cur().kind == tkEnd {
			p.fail(t.line, "token '%s' has no ';'", t.name)
		}
		seq.subs = append(seq.subs, p.lexElem(t))
	}
	return seq
}

func (p *grammarParser) lexElem(t *tokenDef) *rx {
	tok := p.next()
	var e *rx
	switch {
	case tok.kind == tkLiteral:
		if tok.text == "" {
			p.fail(tok.line, "an empty literal matches nothing")
		}
		e = literalRx(tok.text)
	case tok.kind == tkBracket:
		e = &rx{kind: rxBytes, set: p.parseByteSet(tok)}
	case tok.kind == tkPunct && tok.text == ".":
		e = &rx{kind: rxBytes}
		for b := range e.set {
			e.set[b] = true
		}
	case tok.kind == tkPunct && tok.text == "~":
		inner := p.next()
		var set byteSet
		switch {
		case inner.kind == tkBracket:
			set = p.parseByteSet(inner)
		case inner.kind == tkLiteral && len(inner.text) == 1:
			set[inner.text[0]] = true
		default:
			p.fail(inner.line, "~ takes a [set] or a one-character literal")
		}
		e = &rx{kind: rxBytes}
		for b := range set {
			e.set[b] = !set[b]
		}
	case tok.kind == tkPunct && tok.text == "(":
		e = p.lexAlts(t)
		p.expectPunct(")")
	default:
		p.fail(tok.line, "a token rule is made of literals, [sets], ~, ., groups and ? * +, not '%s'", tok.text)
	}
	if p.cur().kind == tkPunct {
		switch p.cur().text {
		case "?":
			e = &rx{kind: rxOpt, subs: []*rx{e}}
		case "*":
			e = &rx{kind: rxStar, subs: []*rx{e}}
		case "+":
			e = &rx{kind: rxPlus, subs: []*rx{e}}
		default:
			return e
		}
		p.next()
		if p.isPunct("?") {
			p.fail(p.cur().line, "the DFA lexer takes the longest match; a non-greedy loop cannot be compiled")
		}
	}
	return e
}

// parseByteSet reads the inside of a [set]: bytes, ranges a-z, and the escapes \n \r \t and \<byte>.
func (p *grammarParser) parseByteSet(tok gtok) byteSet {
	var set byteSet
	text := tok.text
	var chars []int
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == '-' && len(chars) > 0 && i+1 < len(text) {
			chars = append(chars, -1)
			continue
		}
		if c == '\\' && i+1 < len(text) {
			i++
			c = unescape(text[i])
		}
		chars = append(chars, int(c))
	}
	for i := 0; i < len(chars); i++ {
		if i+2 < len(chars) && chars[i+1] == -1 {
			if chars[i] > chars[i+2] {
				p.fail(tok.line, "the range in [%s] runs backwards", text)
			}
			for b := chars[i]; b <= chars[i+2]; b++ {
				set[b] = true
			}
			i += 2
			continue
		}
		if chars[i] == -1 {
			set['-'] = true
			continue
		}
		set[chars[i]] = true
	}
	return set
}

func unescape(c byte) byte {
	switch c {
	case 'n':
		return '\n'
	case 'r':
		return '\r'
	case 't':
		return '\t'
	}
	return c
}

func (r *rx) nullable() bool {
	switch r.kind {
	case rxBytes:
		return false
	case rxSeq:
		for _, s := range r.subs {
			if !s.nullable() {
				return false
			}
		}
		return true
	case rxAlt:
		return slices.ContainsFunc(r.subs, (*rx).nullable)
	case rxStar, rxOpt:
		return true
	case rxPlus:
		return r.subs[0].nullable()
	}
	panic(fmt.Sprintf("grammargen: unknown rx kind %d", r.kind))
}

// first is the set of bytes a match of r can start with.
func (r *rx) first() byteSet {
	var set byteSet
	add := func(s byteSet) {
		for b := range s {
			set[b] = set[b] || s[b]
		}
	}
	switch r.kind {
	case rxBytes:
		return r.set
	case rxSeq:
		for _, s := range r.subs {
			add(s.first())
			if !s.nullable() {
				break
			}
		}
	case rxAlt:
		for _, s := range r.subs {
			add(s.first())
		}
	case rxStar, rxPlus, rxOpt:
		return r.subs[0].first()
	}
	return set
}

// The NFA, built by Thompson's construction: each state has epsilon edges and at most one byte-set edge.

type nfaState struct {
	eps   []int
	set   *byteSet
	next  int
	token int // the lexToken index this state accepts, or noAccept
	owner int // the lexToken whose rule the state belongs to
}

const noAccept = -1

type nfa struct {
	states []nfaState
	owner  int
}

func (n *nfa) add() int {
	n.states = append(n.states, nfaState{token: noAccept, owner: n.owner})
	return len(n.states) - 1
}

// build adds r's states and returns its entry and exit.
func (n *nfa) build(r *rx) (int, int) {
	switch r.kind {
	case rxBytes:
		in, out := n.add(), n.add()
		set := r.set
		n.states[in].set, n.states[in].next = &set, out
		return in, out
	case rxSeq:
		in := n.add()
		at := in
		for _, s := range r.subs {
			sIn, sOut := n.build(s)
			n.states[at].eps = append(n.states[at].eps, sIn)
			at = sOut
		}
		return in, at
	case rxAlt:
		in, out := n.add(), n.add()
		for _, s := range r.subs {
			sIn, sOut := n.build(s)
			n.states[in].eps = append(n.states[in].eps, sIn)
			n.states[sOut].eps = append(n.states[sOut].eps, out)
		}
		return in, out
	case rxStar, rxPlus, rxOpt:
		in, out := n.add(), n.add()
		sIn, sOut := n.build(r.subs[0])
		n.states[in].eps = append(n.states[in].eps, sIn)
		n.states[sOut].eps = append(n.states[sOut].eps, out)
		if r.kind != rxPlus {
			n.states[in].eps = append(n.states[in].eps, out)
		}
		if r.kind != rxOpt {
			n.states[sOut].eps = append(n.states[sOut].eps, sIn)
		}
		return in, out
	}
	panic(fmt.Sprintf("grammargen: unknown rx kind %d", r.kind))
}

// lexToken is one thing the DFA matches: a keyword, or a token rule. Earlier entries win a tie.
type lexToken struct {
	name string // the Go token constant; empty for a skipped token
	def  *tokenDef
	re   *rx
}

// dfa is the lexer's automaton over byte classes. State 0 is dead: no token goes on from it. State 1 is the start.
type dfa struct {
	classes    [256]int
	numClasses int
	next       [][]int // per state, per class
	accept     []int   // per state, the lexToken it accepts, or noAccept
	incomplete []string
	tokens     []*lexToken
}

func (m *model) lexTokens() []*lexToken {
	var toks []*lexToken
	for _, k := range m.keywords {
		toks = append(toks, &lexToken{name: keywordConst(k), re: literalRx(k)})
	}
	for _, t := range m.g.tokens {
		if t.body == nil {
			continue
		}
		lt := &lexToken{def: t, re: t.body}
		if !t.skip {
			lt.name = "T" + t.name
		}
		if t.body.nullable() {
			m.fail(t.line, "token '%s' matches the empty string, so the lexer would never move on", t.name)
		}
		if t.scan {
			// The DFA only starts a scanned token; its first byte is the whole match.
			lt.re = &rx{kind: rxBytes, set: t.body.first()}
		}
		toks = append(toks, lt)
	}
	return toks
}

func buildDfa(m *model) *dfa {
	toks := m.lexTokens()
	var n nfa
	start := n.add()
	for i, t := range toks {
		n.owner = i
		in, out := n.build(t.re)
		n.states[out].token = i
		n.states[start].eps = append(n.states[start].eps, in)
	}

	closure := func(set []int) []int {
		seen := map[int]bool{}
		var stack, out []int
		for _, s := range set {
			if !seen[s] {
				seen[s] = true
				stack = append(stack, s)
			}
		}
		for len(stack) > 0 {
			s := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			out = append(out, s)
			for _, e := range n.states[s].eps {
				if !seen[e] {
					seen[e] = true
					stack = append(stack, e)
				}
			}
		}
		slices.Sort(out)
		return out
	}
	key := func(set []int) string {
		var b strings.Builder
		for _, s := range set {
			b.WriteString(strconv.Itoa(s))
			b.WriteByte(',')
		}
		return b.String()
	}

	// Subset construction, with transitions on every byte; byte classes are found afterwards.
	sets := [][]int{nil, closure([]int{start})}
	index := map[string]int{key(sets[1]): 1}
	var byByte [][256]int
	byByte = append(byByte, [256]int{}, [256]int{})
	for d := 1; d < len(sets); d++ {
		for b := range 256 {
			var moved []int
			for _, s := range sets[d] {
				if st := n.states[s]; st.set != nil && st.set[b] {
					moved = append(moved, st.next)
				}
			}
			if len(moved) == 0 {
				continue
			}
			c := closure(moved)
			k := key(c)
			to, ok := index[k]
			if !ok {
				to = len(sets)
				index[k] = to
				sets = append(sets, c)
				byByte = append(byByte, [256]int{})
			}
			byByte[d][b] = to
		}
	}

	a := &dfa{tokens: toks, accept: make([]int, len(sets)), incomplete: make([]string, len(sets))}
	accepted := make([]bool, len(toks))
	for d, set := range sets {
		a.accept[d] = noAccept
		alive := noAccept
		for _, s := range set {
			st := n.states[s]
			if st.token != noAccept && (a.accept[d] == noAccept || st.token < a.accept[d]) {
				a.accept[d] = st.token
			}
			described := toks[st.owner].def != nil && toks[st.owner].def.desc != ""
			if s != start && described && (alive == noAccept || st.owner < alive) {
				alive = st.owner
			}
		}
		if a.accept[d] != noAccept {
			accepted[a.accept[d]] = true
		} else if alive != noAccept && d > 1 {
			a.incomplete[d] = "incomplete " + noun(toks[alive].def.desc)
		}
	}
	for i, t := range toks {
		if !accepted[i] {
			line := 0
			name := t.name
			if t.def != nil {
				line, name = t.def.line, t.def.name
			}
			m.fail(line, "token '%s' can never be matched: an earlier token always wins", name)
		}
	}

	// Bytes that every state treats alike share a class.
	classOf := map[string]int{}
	for b := range 256 {
		var sig strings.Builder
		for d := range sets {
			sig.WriteString(strconv.Itoa(byByte[d][b]))
			sig.WriteByte(',')
		}
		c, ok := classOf[sig.String()]
		if !ok {
			c = len(classOf)
			classOf[sig.String()] = c
		}
		a.classes[b] = c
	}
	a.numClasses = len(classOf)
	a.next = make([][]int, len(sets))
	for d := range sets {
		a.next[d] = make([]int, a.numClasses)
		for b := range 256 {
			a.next[d][a.classes[b]] = byByte[d][b]
		}
	}
	return a
}

// noun is a token's description without its article: "a label" is "label".
func noun(desc string) string {
	for _, article := range []string{"a ", "an ", "the "} {
		if after, ok := strings.CutPrefix(desc, article); ok {
			return after
		}
	}
	return desc
}

// maxDfaTable is how many entries the transition table holds: an entry is a uint16 offset into it.
const maxDfaTable = 1 << 16

func emitDfa(w *writer, m *model) {
	a := buildDfa(m)
	if len(a.next)*a.numClasses > maxDfaTable {
		m.fail(0, "the lexer's DFA has %d states of %d byte classes, more than a uint16 table holds",
			len(a.next), a.numClasses)
	}
	w.p("// The lexer's DFA, compiled from every token rule and keyword of Maxon.g4.\n\n")
	w.p("const (\n\t// dfaClasses is how many byte classes there are: the width of a row of dfaNext.\n")
	w.p("\tdfaClasses = %d\n", a.numClasses)
	w.p("\t// dfaStart is the start state's row offset in dfaNext.\n\tdfaStart = dfaClasses\n")
	w.p("\t// noToken is the accepted token of a state that accepts none; skipToken that of a skipped token.\n")
	w.p("\tnoToken = numTokens\n\tskipToken = numTokens + 1\n)\n\n")
	w.p("// dfaClass is each byte's class: bytes in one class lead to the same state from every state.\n")
	w.p("var dfaClass = [256]uint8{")
	for b, c := range a.classes {
		if b%32 == 0 {
			w.p("\n\t")
		}
		w.p("%d, ", c)
	}
	w.p("\n}\n\n")
	w.p("// dfaNext is a row per state, indexed by byte class. An entry is the next state's row offset, or 0 when no\n")
	w.p("// token goes on; row 0 is that dead state and row 1 the start.\n")
	w.p("var dfaNext = [...]uint16{\n")
	for d, row := range a.next {
		w.p("\t")
		for _, to := range row {
			w.p("%d, ", to*a.numClasses)
		}
		w.p("// %d\n", d)
	}
	w.p("}\n\n")
	w.p("// dfaAccept is the token each state accepts, by state.\n")
	w.p("var dfaAccept = [...]T{\n")
	for d, t := range a.accept {
		switch {
		case t == noAccept:
			w.p("\tnoToken, // %d\n", d)
		case a.tokens[t].name == "":
			w.p("\tskipToken, // %d %s\n", d, a.tokens[t].def.name)
		default:
			w.p("\t%s, // %d\n", a.tokens[t].name, d)
		}
	}
	w.p("}\n\n")
	w.p("// dfaIncomplete names the token that input stopping in a state leaves unfinished, by state; empty where\n")
	w.p("// the input is an unexpected character instead.\n")
	w.p("var dfaIncomplete = [...]string{\n")
	for d, msg := range a.incomplete {
		if msg != "" {
			w.p("\t%d: %q,\n", d, msg)
		}
	}
	w.p("\t%d: \"\",\n}\n\n", len(a.incomplete)-1)

	var scanned []*tokenDef
	for _, t := range m.g.tokens {
		if t.scan {
			scanned = append(scanned, t)
		}
	}
	w.p("// tokenScanned marks the tokens the DFA only starts; scanRest reads the rest of each.\n")
	w.p("var tokenScanned = [numTokens]bool{")
	for _, t := range scanned {
		w.p("T%s: true, ", t.name)
	}
	w.p("}\n\n")
	w.p("func (lx *lexer) scanRest(kind T, start int) int {\n\tswitch kind {\n")
	for _, t := range scanned {
		w.p("\tcase T%s:\n\t\treturn lx.scan%s(start)\n", t.name, t.name)
	}
	w.p("\t}\n\tpanic(diag.Internal(\"token %%d is not scanned\", kind))\n}\n")
}
