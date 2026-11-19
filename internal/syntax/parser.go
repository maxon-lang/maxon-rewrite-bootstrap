package syntax

import (
	"fmt"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
)

//go:generate go run minimaxon/internal/grammargen

// Parser holds what the generated parse functions (parser_gen.go) share: the token stream, the position in it,
// and the file each token came from.
type Parser struct {
	toks      []Token
	files     []string
	p         int
	fileIndex int
	Errors    []*diag.CompileError
	// spare is a token buffer an interpolation's parser borrows, so each one does not allocate its own.
	spare []Token
}

func NewParser(toks []Token, files []string) *Parser {
	return &Parser{toks: toks, files: files}
}

func (p *Parser) file() string { return p.files[min(p.fileIndex, len(p.files)-1)] }

func (p *Parser) cur() *Token { return &p.toks[p.p] }

func (p *Parser) peekTok() *Token { return &p.toks[min(p.p+1, len(p.toks)-1)] }

func (p *Parser) next() Token {
	t := p.toks[p.p]
	if p.p < len(p.toks)-1 {
		p.p++
	}
	return t
}

func (p *Parser) at(k T) bool { return p.toks[p.p].Kind == k }

func (p *Parser) in(set *[numTokens]bool) bool { return set[p.toks[p.p].Kind] }

func (p *Parser) peekIs(k T) bool { return p.peekTok().Kind == k }

func (p *Parser) peekIn(set *[numTokens]bool) bool { return set[p.peekTok().Kind] }

func (p *Parser) atEndOfInput() bool { return p.at(TFileEnd) || p.at(TEof) }

// expected is the syntax error at the current token, naming what the grammar allows there.
func (p *Parser) expected(what string) *diag.CompileError {
	t := p.cur()
	if p.atEndOfInput() {
		return diag.NewError(diag.ErrUnexpectedEnd, "unexpected end of file, expected "+what, p.file(), t.Line, t.Col)
	}
	return diag.NewError(diag.ErrExpectedToken, fmt.Sprintf("expected %s, found '%s'", what, t.Text), p.file(), t.Line, t.Col)
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
	pos.Line = t.Line
	pos.Col = t.Col
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

func (p *Parser) intLitMagnitude(t Token) ast.I128 { return t.IntValue() }

func (p *Parser) boundValue(t Token) ast.I128 { return t.IntValue() }

func (p *Parser) strLitParts(t Token) []any {
	var parts []any
	for _, part := range t.Parts() {
		switch part := part.(type) {
		case TextPart:
			parts = append(parts, part.Text)
		case ExprPart:
			// The sub-parser borrows the spare buffer; an interpolation nested inside it finds none and allocates.
			toks := LexText(p.file(), part.Source, part.Line, part.Col, p.spare)
			p.spare = nil
			sub := NewParser(toks, p.files[p.fileIndex:p.fileIndex+1])
			parts = append(parts, sub.parseInterpolation())
			p.spare = sub.toks[:0]
		default:
			panic(diag.Internal("unknown string part %T", part))
		}
	}
	return parts
}

func (p *Parser) parseInterpolation() ast.Expr {
	e := p.parseExpr()
	if !p.at(TNewline) && !p.atEndOfInput() {
		panic(p.expected("the end of the interpolation"))
	}
	return e
}
