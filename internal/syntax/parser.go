package syntax

import (
	"fmt"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
)

//go:generate go run minimaxon/internal/grammargen

// Parser holds what the generated parse functions (parser_gen.go) share: the module's source, its token stream,
// the position in it, and the file and line the parser is in.
type Parser struct {
	src       *Source
	toks      []Token
	p         int
	fileIndex int
	// line indexes a line near the last position asked for, so finding the next is a short walk.
	line   int
	Errors []*diag.CompileError
	// spare is a token buffer an interpolation's parser borrows, so each one does not allocate its own.
	spare []Token
	// parts is reused by each string literal's conversion.
	parts []StrPart
}

func NewParser(src *Source, toks []Token) *Parser {
	return &Parser{src: src, toks: toks}
}

func (p *Parser) fileNumber() int { return min(p.fileIndex, len(p.src.Files)-1) }

func (p *Parser) file() string { return p.src.Files[p.fileNumber()].File }

func (p *Parser) lineCol(t *Token) (int, int) {
	return p.src.lineCol(p.fileNumber(), int(t.Start), &p.line)
}

// text is a token's text: a substring of the module's source.
func (p *Parser) text(t Token) string { return p.src.Text[t.Start:t.End()] }

func (p *Parser) cur() *Token { return &p.toks[p.p] }

func (p *Parser) peekTok() *Token { return &p.toks[min(p.p+1, len(p.toks)-1)] }

func (p *Parser) next() Token {
	t := p.toks[p.p]
	if p.p < len(p.toks)-1 {
		p.p++
	}
	return t
}

func (p *Parser) at(k T) bool { return p.toks[p.p].Kind() == k }

func (p *Parser) in(set *[numTokens]bool) bool { return set[p.toks[p.p].Kind()] }

func (p *Parser) peekIs(k T) bool { return p.peekTok().Kind() == k }

func (p *Parser) peekIn(set *[numTokens]bool) bool { return set[p.peekTok().Kind()] }

func (p *Parser) atEndOfInput() bool { return p.at(TFileEnd) || p.at(TEof) }

// describe is how a syntax error shows a token: its text, or a name for a token whose text is not shown.
func (p *Parser) describe(t *Token) string {
	switch t.Kind() {
	case TNewline:
		return newlineText
	case TFileEnd:
		return fileEndText
	case TEof:
		return eofText
	case TString:
		return stringText
	}
	return p.text(*t)
}

// expected is the syntax error at the current token, naming what the grammar allows there.
func (p *Parser) expected(what string) *diag.CompileError {
	t := p.cur()
	line, col := p.lineCol(t)
	if p.atEndOfInput() {
		return diag.NewError(diag.ErrUnexpectedEnd, "unexpected end of file, expected "+what, p.file(), line, col)
	}
	return diag.NewError(diag.ErrExpectedToken, fmt.Sprintf("expected %s, found '%s'", what, p.describe(t)), p.file(), line, col)
}

func (p *Parser) expect(k T, what string) Token {
	if !p.at(k) {
		panic(p.expected(what))
	}
	return p.next()
}

func place[N ast.Node](p *Parser, node N, t *Token) {
	pos := node.P()
	pos.File = p.file()
	pos.Line, pos.Col = p.lineCol(t)
}

func (p *Parser) skipNewlines() {
	for p.at(TNewline) {
		p.next()
	}
}

// ParseProgram parses every declaration of a module's files. A syntax error abandons the rest of its file, so
// each file reports its own first error.
func (p *Parser) ParseProgram() *ast.Program {
	prog := &ast.Program{}
	p.skipNewlines()
	for !p.at(TEof) {
		if p.at(TFileEnd) {
			p.next()
			p.fileIndex++
			p.skipNewlines()
			continue
		}
		failed := len(p.Errors)
		diag.Guard(&p.Errors, func() {
			prog.Decls = append(prog.Decls, p.parseDecl())
			p.expect(TNewline, "the end of the line")
			p.skipNewlines()
		})
		if len(p.Errors) != failed {
			for !p.atEndOfInput() {
				p.next()
			}
		}
	}
	return prog
}

// The conversions below turn a token into a field that is not its text, as Maxon.g4 declares.

func (p *Parser) intLitMagnitude(t Token) ast.I128 { return p.integer(t) }

func (p *Parser) boundValue(t Token) ast.I128 { return p.integer(t) }

func (p *Parser) integer(t Token) ast.I128 {
	value, _ := p.src.readInteger(p.fileNumber(), int(t.Start), t.End())
	return value
}

func (p *Parser) strLitParts(t Token) []any {
	p.parts = p.parts[:0]
	p.src.readString(p.fileNumber(), int(t.Start), &p.parts)
	values := make([]any, 0, len(p.parts))
	for _, part := range p.parts {
		switch part := part.(type) {
		case TextPart:
			values = append(values, part.Text)
		case ExprPart:
			// The sub-parser borrows the spare buffer; an interpolation nested inside it finds none and allocates.
			toks := LexInterpolation(p.src, p.fileNumber(), part.Begin, part.End, p.spare)
			p.spare = nil
			sub := &Parser{src: p.src, toks: toks, fileIndex: p.fileNumber(), line: p.line}
			values = append(values, sub.parseInterpolation())
			p.spare = sub.toks[:0]
		default:
			panic(diag.Internal("unknown string part %T", part))
		}
	}
	return values
}

func (p *Parser) parseInterpolation() ast.Expr {
	e := p.parseExpr()
	if !p.at(TNewline) && !p.atEndOfInput() {
		panic(p.expected("the end of the interpolation"))
	}
	return e
}
