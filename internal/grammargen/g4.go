package main

import (
	"fmt"
	"strings"
)

// grammarError is a mistake in the grammar file, raised as a panic and returned by generate.
type grammarError struct {
	file string
	line int
	msg  string
}

func (e *grammarError) Error() string { return fmt.Sprintf("%s:%d: %s", e.file, e.line, e.msg) }

type tokKind int

const (
	tkIdent tokKind = iota
	tkLiteral
	tkBracket
	tkBrace
	tkDoc
	tkPunct
	tkEnd
)

type gtok struct {
	kind tokKind
	text string
	line int
}

func lexGrammar(file, src string) []gtok {
	var toks []gtok
	line := 1
	i := 0
	fail := func(msg string) { panic(&grammarError{file, line, msg}) }
	for i < len(src) {
		c := src[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case strings.HasPrefix(src[i:], "/**"):
			end := strings.Index(src[i+3:], "*/")
			if end < 0 {
				fail("unterminated comment")
			}
			text := src[i+3 : i+3+end]
			toks = append(toks, gtok{tkDoc, strings.TrimSpace(text), line})
			line += strings.Count(text, "\n")
			i += end + 5
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				fail("unterminated comment")
			}
			line += strings.Count(src[i:i+2+end], "\n")
			i += end + 4
		case strings.HasPrefix(src[i:], "//"):
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '\'':
			var text strings.Builder
			i++
			for {
				if i >= len(src) || src[i] == '\n' {
					fail("unterminated literal")
				}
				if src[i] == '\'' {
					i++
					break
				}
				if src[i] == '\\' && i+1 < len(src) {
					text.WriteByte(unescape(src[i+1]))
					i += 2
					continue
				}
				text.WriteByte(src[i])
				i++
			}
			toks = append(toks, gtok{tkLiteral, text.String(), line})
		case c == '[' || c == '{':
			open, close := c, byte(']')
			kind := tkBracket
			if c == '{' {
				close, kind = '}', tkBrace
			}
			start, startLine, depth := i+1, line, 0
			for ; ; i++ {
				if i >= len(src) {
					panic(&grammarError{file, startLine, fmt.Sprintf("unclosed '%c'", open)})
				}
				switch src[i] {
				case '\\':
					i++
				case '\n':
					line++
				case open:
					depth++
				case close:
					depth--
				}
				if depth == 0 {
					break
				}
			}
			toks = append(toks, gtok{kind, src[start:i], startLine})
			i++
		case isIdentStart(c):
			start := i
			for i < len(src) && isIdentPart(src[i]) {
				i++
			}
			toks = append(toks, gtok{tkIdent, src[start:i], line})
		default:
			p, ok := punctAt(src[i:])
			if !ok {
				fail(fmt.Sprintf("unexpected character '%c'", c))
			}
			toks = append(toks, gtok{tkPunct, p, line})
			i += len(p)
		}
	}
	return append(toks, gtok{tkEnd, "end of file", line})
}

var grammarPunctuation = []string{
	"->", "+=", "^", "::", ":", ";", "|", "(", ")", "?", "*", "+", "=", "#", "~", ".", "<", ">", ",", "@",
}

func punctAt(s string) (string, bool) {
	for _, p := range grammarPunctuation {
		if strings.HasPrefix(s, p) {
			return p, true
		}
	}
	return "", false
}

func isIdentStart(c byte) bool { return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func isIdentPart(c byte) bool { return isIdentStart(c) || c >= '0' && c <= '9' }

func isUpper(s string) bool { return s != "" && s[0] >= 'A' && s[0] <= 'Z' }

// The grammar as written.

type grammar struct {
	file      string
	name      string
	tokens    []*tokenDef
	rules     []*rule
	astBlocks []*astBlock
	// structs names the nodes that are Go structs: the declarations, which later phases fill in and other modules
	// read. Every other node is a record in the module's tree.
	structs []string
}

type tokenDef struct {
	name string
	// literal is the token's text when its rule is a single literal; a literal in a parser rule names it.
	literal string
	desc    string
	line    int
	// body is the rule's regular expression; nil for a token in the tokens {} block, which the lexer makes itself.
	body *rx
	// skip drops what the rule matches; scan has the DFA only start the token and lexer.scan<Name> finish it.
	skip bool
	scan bool
	// noSpaceBefore and noSpaceAfter join the token to the token before or after it wherever it appears.
	noSpaceBefore bool
	noSpaceAfter  bool
}

type rule struct {
	name    string
	desc    string
	line    int
	returns *goVar
	// builds names the node another rule defines, when this rule is a second way to write that node.
	builds string
	alts   []*alt
}

type goVar struct {
	name string
	typ  string
}

type alt struct {
	elems      []*elem
	label      string
	assocRight bool
	line       int
}

type elemKind int

const (
	ekLiteral elemKind = iota
	ekToken
	ekRule
	ekGroup
	// ekPredicate is {name}?: a condition that the hand-written parser method name decides, which starts the only alternative
	// of an optional group, where the tokens alone do not tell whether to enter it.
	ekPredicate
)

type elem struct {
	kind     elemKind
	label    string
	appendTo bool
	text     string
	group    []*alt
	suffix   byte
	line     int
	// joinBefore (written ^x) and joinAfter (x^) put no space between the token and the one before or after it.
	joinBefore bool
	joinAfter  bool
	// anyKeyword is set on a group that held the Keyword placeholder, which stands for every keyword of the grammar.
	anyKeyword bool
	// action (written elem{name}) is the hand-written parser method that takes the token a token element consumed, before
	// a label stores it, or that is called with nothing once a rule element is parsed; it may refuse what it was given.
	action string
}

type astBlock struct {
	name   string
	fields []*astField
	line   int
}

type astField struct {
	name string
	typ  string
	init string
	gap  bool
	line int
}

type grammarParser struct {
	file string
	toks []gtok
	pos  int
	doc  string
}

func parseGrammar(file, src string) *grammar {
	p := &grammarParser{file: file, toks: lexGrammar(file, src)}
	g := &grammar{file: file}
	p.expectIdent("grammar")
	g.name = p.ident()
	p.expectPunct(";")
	for p.cur().kind != tkEnd {
		if p.cur().kind == tkDoc {
			p.doc = p.next().text
			continue
		}
		switch {
		case p.isIdent("tokens"):
			p.next()
			body := p.expect(tkBrace)
			// Each entry is a name, after an optional /** description */.
			for entry := range strings.SplitSeq(body.text, ",") {
				entry = strings.TrimSpace(entry)
				desc := ""
				if rest, ok := strings.CutPrefix(entry, "/**"); ok {
					text, name, found := strings.Cut(rest, "*/")
					if !found {
						p.fail(body.line, "unterminated description in tokens {}")
					}
					desc, entry = strings.TrimSpace(text), strings.TrimSpace(name)
				}
				if entry != "" {
					g.tokens = append(g.tokens, &tokenDef{name: entry, desc: desc, line: body.line})
				}
			}
		case p.isPunct("@"):
			if names, ok := p.structsBlock(); ok {
				g.structs = append(g.structs, names...)
				continue
			}
			g.astBlocks = append(g.astBlocks, p.astBlock())
		case p.cur().kind == tkIdent && isUpper(p.cur().text):
			g.tokens = append(g.tokens, p.lexerRule())
		case p.cur().kind == tkIdent:
			g.rules = append(g.rules, p.parserRule())
		default:
			p.fail(p.cur().line, "expected a rule, found '%s'", p.cur().text)
		}
	}
	return g
}

func (p *grammarParser) cur() gtok { return p.toks[p.pos] }

func (p *grammarParser) peek() gtok { return p.toks[min(p.pos+1, len(p.toks)-1)] }

func (p *grammarParser) next() gtok {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *grammarParser) fail(line int, format string, args ...any) {
	panic(&grammarError{p.file, line, fmt.Sprintf(format, args...)})
}

func (p *grammarParser) isPunct(s string) bool { return p.cur().kind == tkPunct && p.cur().text == s }

func (p *grammarParser) isIdent(s string) bool { return p.cur().kind == tkIdent && p.cur().text == s }

func (p *grammarParser) expect(k tokKind) gtok {
	if p.cur().kind != k {
		p.fail(p.cur().line, "unexpected '%s'", p.cur().text)
	}
	return p.next()
}

func (p *grammarParser) expectPunct(s string) {
	if !p.isPunct(s) {
		p.fail(p.cur().line, "expected '%s', found '%s'", s, p.cur().text)
	}
	p.next()
}

func (p *grammarParser) expectIdent(s string) {
	if !p.isIdent(s) {
		p.fail(p.cur().line, "expected '%s', found '%s'", s, p.cur().text)
	}
	p.next()
}

func (p *grammarParser) ident() string {
	if p.cur().kind != tkIdent {
		p.fail(p.cur().line, "expected a name, found '%s'", p.cur().text)
	}
	return p.next().text
}

func (p *grammarParser) takeDoc() string {
	d := p.doc
	p.doc = ""
	return d
}

// structsBlock reads `@ast::Structs { Name Name ... }` if that is what comes next.
func (p *grammarParser) structsBlock() ([]string, bool) {
	at := p.pos
	if !p.isPunct("@") || p.peek().text != "ast" || p.toks[at+2].text != "::" || p.toks[at+3].text != "Structs" {
		return nil, false
	}
	p.pos += 4
	body := p.expect(tkBrace)
	p.takeDoc()
	return strings.Fields(body.text), true
}

func (p *grammarParser) astBlock() *astBlock {
	p.expectPunct("@")
	at := p.cur().line
	p.expectIdent("ast")
	p.expectPunct("::")
	b := &astBlock{name: p.ident(), line: at}
	body := p.expect(tkBrace)
	gap := false
	for i, raw := range strings.Split(body.text, "\n") {
		line := body.line + i
		text := strings.TrimSpace(raw)
		if text == "" {
			gap = len(b.fields) > 0
			continue
		}
		f := &astField{gap: gap, line: line}
		gap = false
		if eq := strings.Index(text, "="); eq >= 0 {
			f.init = strings.TrimSpace(text[eq+1:])
			text = strings.TrimSpace(text[:eq])
		}
		words := strings.Fields(text)
		if len(words) != 2 {
			p.fail(line, "a field is 'Name Type' or 'Name Type = initial'")
		}
		f.name, f.typ = words[0], words[1]
		b.fields = append(b.fields, f)
	}
	p.takeDoc()
	return b
}

func (p *grammarParser) lexerRule() *tokenDef {
	t := &tokenDef{line: p.cur().line, name: p.ident(), desc: p.takeDoc()}
	p.expectPunct(":")
	if p.cur().kind == tkLiteral && p.peek().kind == tkPunct && (p.peek().text == ";" || p.peek().text == "->") {
		t.literal = p.cur().text
	}
	t.body = p.lexAlts(t)
	if p.isPunct("->") {
		p.next()
		for {
			switch command := p.ident(); command {
			case "skip":
				t.skip = true
			case "scan":
				t.scan = true
			case "noSpaceBefore":
				t.noSpaceBefore = true
			case "noSpaceAfter":
				t.noSpaceAfter = true
			case "noSpace":
				t.noSpaceBefore, t.noSpaceAfter = true, true
			default:
				p.fail(t.line, "token '%s' has the command '%s'; a token takes skip, scan, noSpaceBefore, "+
					"noSpaceAfter or noSpace", t.name, command)
			}
			if !p.isPunct(",") {
				break
			}
			p.next()
		}
	}
	p.expectPunct(";")
	return t
}

func (p *grammarParser) parserRule() *rule {
	r := &rule{line: p.cur().line, name: p.ident(), desc: p.takeDoc()}
	if p.isIdent("builds") {
		p.next()
		r.builds = p.ident()
	}
	if p.isIdent("returns") {
		p.next()
		body := p.expect(tkBracket)
		text := strings.TrimSpace(body.text)
		sp := strings.LastIndexAny(text, " \t")
		if sp < 0 {
			p.fail(body.line, "returns is '[Type name]'")
		}
		r.returns = &goVar{name: strings.TrimSpace(text[sp+1:]), typ: strings.TrimSpace(text[:sp])}
	}
	p.expectPunct(":")
	r.alts = p.alts()
	p.expectPunct(";")
	return r
}

func (p *grammarParser) alts() []*alt {
	alts := []*alt{p.alt()}
	for p.isPunct("|") {
		p.next()
		alts = append(alts, p.alt())
	}
	return alts
}

func (p *grammarParser) alt() *alt {
	a := &alt{line: p.cur().line}
	if p.isPunct("<") {
		p.next()
		p.expectIdent("assoc")
		p.expectPunct("=")
		p.expectIdent("right")
		p.expectPunct(">")
		a.assocRight = true
	}
	for !p.isPunct("|") && !p.isPunct(")") && !p.isPunct(";") && !p.isPunct("#") {
		a.elems = append(a.elems, p.elem())
	}
	if p.isPunct("#") {
		p.next()
		a.label = p.ident()
	}
	return a
}

func (p *grammarParser) elem() *elem {
	e := &elem{line: p.cur().line}
	if p.cur().kind == tkIdent && p.peek().kind == tkPunct && (p.peek().text == "=" || p.peek().text == "+=") {
		e.label = p.next().text
		e.appendTo = p.next().text == "+="
	}
	if p.isPunct("^") {
		p.next()
		e.joinBefore = true
	}
	t := p.next()
	switch {
	case t.kind == tkLiteral:
		e.kind, e.text = ekLiteral, t.text
	case t.kind == tkIdent && isUpper(t.text):
		e.kind, e.text = ekToken, t.text
	case t.kind == tkIdent:
		e.kind, e.text = ekRule, t.text
	case t.kind == tkPunct && t.text == "(":
		e.kind = ekGroup
		e.group = p.alts()
		p.expectPunct(")")
	case t.kind == tkBrace:
		e.kind, e.text = ekPredicate, strings.TrimSpace(t.text)
		p.expectPunct("?")
		if e.label != "" || e.joinBefore || p.cur().kind == tkPunct && strings.Contains("*+?", p.cur().text) {
			p.fail(t.line, "a predicate {%s}? takes no label, join or suffix", e.text)
		}
	default:
		p.fail(t.line, "unexpected '%s'", t.text)
	}
	// A ^ between two elements joins the second to the first; it is the first's x^ only where no element follows.
	if next := p.peek(); p.isPunct("^") && next.kind == tkPunct && strings.Contains("|);?*+#", next.text) {
		p.next()
		e.joinAfter = true
	}
	if (e.joinBefore || e.joinAfter) && e.kind != ekLiteral && e.kind != ekToken {
		p.fail(e.line, "^ joins a token to its neighbour; put it on a literal or a token")
	}
	// A brace after an element is its action; one followed by ? is the predicate that starts the next element.
	if p.cur().kind == tkBrace && !(p.peek().kind == tkPunct && p.peek().text == "?") {
		t := p.next()
		if e.kind == ekPredicate || e.kind == ekGroup && e.label == "" {
			p.fail(t.line, "an action {%s} follows a token, a label of tokens or a rule", strings.TrimSpace(t.text))
		}
		e.action = strings.TrimSpace(t.text)
	}
	if p.cur().kind == tkPunct && (p.cur().text == "?" || p.cur().text == "*" || p.cur().text == "+") {
		e.suffix = p.next().text[0]
	}
	return e
}
