package main

import (
	"fmt"
	"strings"

	"minimaxon/internal/irgen/irtable"
)

// rule is one rewrite rule: a pattern over an operation and the instructions that define its registers, a condition,
// and what the operation is rewritten to.
type rule struct {
	file string
	line int
	// text is the rule as written, on one line, for the errors and the comment of its code.
	text  string
	match *term
	// cond is a Maxon expression over the rule's variables, or "" for none.
	cond    string
	results []*term
}

type termKind int

const (
	termWild termKind = iota // _: anything
	termVar                  // a variable: anything, and what the rest of the rule names it by; again, only that value
	termOp                   // (op.suffix... operand...): an operation
	termAux                  // [text]: an operand that is no register: in a pattern _, a variable or a number, in a result an expression
)

// term is a node of a pattern or a result.
type term struct {
	kind termKind
	// name is the operation of a termOp and the variable of a termVar.
	name string
	// bind is the variable that the register a termOp defines is bound to, written name:(op ...).
	bind string
	// suffixes are a termOp's enum case and flags: .add, .unsigned, .!checked.
	suffixes []string
	args     []*term
	// text is what a termAux holds.
	text string

	// Set by the check of a termOp of the IR: the operation, the case of its enum operand by its Go name ("" for none), and
	// the flags it names by the table's name, each with whether it is set.
	op    irtable.Op
	cas   string
	flags map[string]bool
}

// String is the term as a rule writes it.
func (t *term) String() string {
	switch t.kind {
	case termWild:
		return "_"
	case termVar:
		return t.name
	case termAux:
		return "[" + t.text + "]"
	case termOp:
		var b strings.Builder
		if t.bind != "" {
			b.WriteString(t.bind + ":")
		}
		b.WriteString("(" + t.name)
		for _, s := range t.suffixes {
			b.WriteString("." + s)
		}
		for _, a := range t.args {
			b.WriteString(" " + a.String())
		}
		b.WriteString(")")
		return b.String()
	}
	panic(fmt.Sprintf("rulegen: a term of kind %d", t.kind))
}

// parseRules reads the rules of a file: a rule is a pattern, `&&` and a condition if it has one, `=>` and what it is
// rewritten to, and a line that starts with `&&` or `=>` goes on with the rule before it. `//` starts a comment.
func parseRules(file, src string) []*rule {
	var rules []*rule
	var text string
	line := 0
	flush := func() {
		if text != "" {
			rules = append(rules, parseRule(file, line, text))
		}
		text = ""
	}
	for i, l := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		if c := strings.Index(l, "//"); c >= 0 {
			l = l[:c]
		}
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "&&") || strings.HasPrefix(l, "=>") {
			if text == "" {
				failAt(file, i+1, l, "a line that starts with %s goes on with a rule, and there is none before it", l[:2])
			}
			text += " " + l
			continue
		}
		flush()
		text, line = l, i+1
	}
	flush()
	return rules
}

const (
	arrow = "=>"
	and   = "&&"
)

func parseRule(file string, line int, text string) *rule {
	r := &rule{file: file, line: line, text: text}
	lhs, rhs, ok := strings.Cut(text, arrow)
	if !ok {
		r.fail("a rule is a pattern, %s and what it is rewritten to", arrow)
	}
	pattern, cond, hasCond := strings.Cut(lhs, and)
	if hasCond {
		r.cond = strings.TrimSpace(cond)
		if r.cond == "" {
			r.fail("%s is followed by no condition", and)
		}
	}
	matches := parseTerms(r, pattern)
	if len(matches) != 1 {
		r.fail("a rule matches one operation, and this pattern is %d terms", len(matches))
	}
	r.match = matches[0]
	r.results = parseTerms(r, rhs)
	if len(r.results) == 0 {
		r.fail("%s is followed by nothing to rewrite to", arrow)
	}
	return r
}

// token is a piece of a pattern: one of ( ) : as itself, a word, or a bracket and what it holds.
type token struct {
	kind byte // '(', ')', ':', 'w' for a word, '[' for a bracket
	text string
}

func isWordByte(c byte) bool {
	return c == '_' || c == '.' || c == '!' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func tokenize(r *rule, s string) []token {
	var out []token
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t':
			i++
		case c == '(' || c == ')' || c == ':':
			out = append(out, token{c, string(c)})
			i++
		case c == '[':
			depth, j := 0, i
			for ; j < len(s); j++ {
				if s[j] == '[' {
					depth++
				} else if s[j] == ']' {
					depth--
					if depth == 0 {
						break
					}
				}
			}
			if j == len(s) {
				r.fail("a [ is not closed")
			}
			out = append(out, token{'[', strings.TrimSpace(s[i+1 : j])})
			i = j + 1
		case isWordByte(c):
			j := i
			for j < len(s) && isWordByte(s[j]) {
				j++
			}
			out = append(out, token{'w', s[i:j]})
			i = j
		default:
			r.fail("%q is not part of a pattern", c)
		}
	}
	return out
}

func parseTerms(r *rule, s string) []*term {
	p := &termParser{r: r, toks: tokenize(r, s)}
	var out []*term
	for p.at < len(p.toks) {
		out = append(out, p.term())
	}
	return out
}

type termParser struct {
	r    *rule
	toks []token
	at   int
}

func (p *termParser) next() token {
	if p.at == len(p.toks) {
		p.r.fail("the rule ends inside a term")
	}
	t := p.toks[p.at]
	p.at++
	return t
}

func (p *termParser) peek(kind byte) bool { return p.at < len(p.toks) && p.toks[p.at].kind == kind }

func (p *termParser) term() *term {
	t := p.next()
	switch t.kind {
	case '(':
		return p.op("")
	case '[':
		return &term{kind: termAux, text: t.text}
	case 'w':
		if t.text == "_" {
			return &term{kind: termWild}
		}
		p.variable(t.text)
		if p.peek(':') {
			p.at++
			if p.next().kind != '(' {
				p.r.fail("%s: binds an operation, and an operation is in parentheses", t.text)
			}
			return p.op(t.text)
		}
		return &term{kind: termVar, name: t.text}
	}
	p.r.fail("%q cannot start a term", t.text)
	return nil
}

// variable refuses a name that is not a variable's: a lowercase letter and then letters and digits.
func (p *termParser) variable(name string) {
	if name[0] < 'a' || name[0] > 'z' || strings.ContainsAny(name, "._!") {
		p.r.fail("%s is no variable: a variable is a lowercase letter and then letters and digits", name)
	}
}

func (p *termParser) op(bind string) *term {
	head := p.next()
	if head.kind != 'w' {
		p.r.fail("an operation starts with its name")
	}
	parts := strings.Split(head.text, ".")
	t := &term{kind: termOp, name: parts[0], bind: bind, suffixes: parts[1:]}
	for !p.peek(')') {
		t.args = append(t.args, p.term())
	}
	p.at++
	return t
}
