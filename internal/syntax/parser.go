package syntax

import (
	"fmt"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"strings"
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

	// The layout: where the last token consumed ends and what it was, whether the next token joins it with no
	// space between them, and which blank lines may come before the next line.
	prevEnd  int
	prevKind T
	joinNext bool
	blanks   blankRule
}

// blankRule says which blank lines may come before a line: ParseProgram sets the rule between declarations, and a
// function inside a declaration has its own.
type blankRule int

const (
	// noBlanks allows none: the line is inside a declaration.
	noBlanks blankRule = iota
	// firstInFile allows blank lines between comment paragraphs, but not before the first line of the file.
	firstInFile
	// afterDecl requires one blank line right after the declaration before, and allows one between comment
	// paragraphs.
	afterDecl
	// beforeFunction is afterDecl for a method or an interface's function that is not the first line of its body: a
	// blank line sets each function apart, as between top-level declarations.
	beforeFunction
)

func NewParser(src *Source, toks []Token) *Parser {
	p := &Parser{src: src, toks: toks, prevKind: TFileEnd, blanks: firstInFile}
	if len(src.Files) > 0 {
		p.prevEnd = src.Files[0].Begin
	}
	return p
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

// next consumes the current token, after checking the layout before it.
func (p *Parser) next() Token {
	t := p.toks[p.p]
	p.checkLayout(&t)
	return p.skip()
}

// skip consumes the current token without checking its layout, as error recovery does.
func (p *Parser) skip() Token {
	t := p.toks[p.p]
	if p.p < len(p.toks)-1 {
		p.p++
	}
	p.prevEnd, p.prevKind = lexemeEnd(t), t.Kind()
	p.joinNext = t.Kind() < numTokens && tokenNoSpaceAfter[t.Kind()]
	return t
}

func (p *Parser) at(k T) bool { return p.toks[p.p].Kind() == k }

func (p *Parser) in(set *[numTokens]bool) bool { return set[p.toks[p.p].Kind()] }

func (p *Parser) peekIs(k T) bool { return p.peekTok().Kind() == k }

func (p *Parser) peekIn(set *[numTokens]bool) bool { return set[p.peekTok().Kind()] }

func (p *Parser) atEndOfInput() bool { return p.at(TFileEnd) || p.at(TEof) }

// lexemeStart and lexemeEnd are where a token's source text begins and ends: a label's token leaves out its
// quotes.
func lexemeStart(t Token) int {
	if t.Kind() == TLabel {
		return int(t.Start) - 1
	}
	return int(t.Start)
}

func lexemeEnd(t Token) int {
	if t.Kind() == TLabel {
		return t.End() + 1
	}
	return t.End()
}

// startsLine reports whether a token of kind is the last thing before a new line's first token.
func startsLine(kind T) bool {
	return kind == TNewline || kind == TIndent || kind == TDedent || kind == TFileEnd
}

// checkLayout checks the space before t. Within a line the lexer has made every gap one space or none; here the
// grammar decides which: none where t, or the token before it, joins its neighbour. At the start of a line the
// lexer has checked the indentation, and only the blank lines before it are left to check.
func (p *Parser) checkLayout(t *Token) {
	kind := t.Kind()
	start := lexemeStart(*t)
	switch {
	case startsLine(p.prevKind):
		p.checkBlankLines(start)
	case kind == TNewline:
		// The gap may hold a comment after the code, which the lexer has checked.
	case p.joinNext || tokenNoSpaceBefore[kind]:
		if start > p.prevEnd {
			msg := "expected no space before '" + p.describe(t) + "'"
			panic(p.src.errorAt(diag.ErrExpectedToken, msg, p.fileNumber(), p.prevEnd))
		}
	case start == p.prevEnd:
		panic(p.layoutError(t, "expected a space before '"+p.describe(t)+"'"))
	}
}

func (p *Parser) layoutError(t *Token, msg string) *diag.CompileError {
	line, col := p.lineCol(t)
	return diag.NewError(diag.ErrExpectedToken, msg, p.file(), line, col)
}

// checkBlankLines checks the blank lines and comment lines between the last line and the one that starts at
// lineStart, against the rule ParseProgram set for it.
func (p *Parser) checkBlankLines(lineStart int) {
	rule := p.blanks
	p.blanks = noBlanks
	if rule == noBlanks && p.prevKind == TNewline && p.startsFunction() {
		rule = beforeFunction
	}
	gap := p.src.Text[p.prevEnd:lineStart]
	at := p.prevEnd
	lastBlank := false
	for i := 0; ; i++ {
		end := strings.IndexByte(gap, '\n')
		if end < 0 {
			break
		}
		blank := end == 0
		switch {
		case blank && rule == noBlanks:
			panic(p.src.errorAt(diag.ErrExpectedToken, "a blank line is only allowed between declarations, and before a function of a type, an enum or a union", p.fileNumber(), at))
		case blank && i == 0 && rule == firstInFile:
			panic(p.src.errorAt(diag.ErrExpectedToken, "the file starts with a blank line", p.fileNumber(), at))
		case blank && lastBlank:
			panic(p.src.errorAt(diag.ErrExpectedToken, "two blank lines in a row; keep one", p.fileNumber(), at))
		case !blank && i == 0 && rule == afterDecl:
			panic(p.src.errorAt(diag.ErrExpectedToken, "expected a blank line after the declaration above", p.fileNumber(), at))
		case !blank && i == 0 && rule == beforeFunction:
			panic(p.src.errorAt(diag.ErrExpectedToken, blankBeforeFunction, p.fileNumber(), at))
		}
		lastBlank = blank
		gap, at = gap[end+1:], at+end+1
	}
	switch {
	case rule == afterDecl && at == p.prevEnd && !p.at(TFileEnd):
		panic(p.src.errorAt(diag.ErrExpectedToken, "expected a blank line after the declaration above", p.fileNumber(), at))
	case rule == beforeFunction && at == p.prevEnd:
		panic(p.src.errorAt(diag.ErrExpectedToken, blankBeforeFunction, p.fileNumber(), at))
	case lastBlank && p.at(TFileEnd):
		panic(p.src.errorAt(diag.ErrExpectedToken, "the file ends with a blank line", p.fileNumber(), at-1))
	}
}

const blankBeforeFunction = "expected a blank line before this function, which is not the first line of its type, enum or union"

// startsFunction is whether the line starting at the current token defines a function inside a declaration: a method
// or a static function. No statement starts with these words, so inside a function body it is never true. An
// interface's functions are signatures, one line each with no body, and are listed without blank lines between them.
func (p *Parser) startsFunction() bool {
	switch p.cur().Kind() {
	case TKw_function, TKw_static:
	case TKw_export, TKw_public:
		if next := p.peekTok().Kind(); next != TKw_function && next != TKw_static {
			return false
		}
	default:
		return false
	}
	return p.lineOpensBody()
}

// lineOpensBody is whether the line starting at the current token is followed by an indented body.
func (p *Parser) lineOpensBody() bool {
	for i := p.p; i+1 < len(p.toks); i++ {
		if p.toks[i].Kind() == TNewline {
			return p.toks[i+1].Kind() == TIndent
		}
	}
	return false
}

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
	if t.Len() == 0 {
		return tokenDescription[t.Kind()]
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
	msg := fmt.Sprintf("expected %s, found '%s'", what, p.describe(t))
	return diag.NewError(diag.ErrExpectedToken, msg, p.file(), line, col)
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

// ParseProgram parses every declaration of a module's files. A syntax error abandons the rest of its file, so
// each file reports its own first error.
func (p *Parser) ParseProgram() *ast.Program {
	prog := &ast.Program{}
	decls, failed := 0, false
	for !p.at(TEof) {
		if p.at(TFileEnd) {
			if !failed {
				diag.Guard(&p.Errors, func() {
					p.blanks = firstInFile
					if decls > 0 {
						p.blanks = afterDecl
					}
					p.checkLayout(p.cur())
				})
			}
			p.skip()
			p.fileIndex++
			decls, failed = 0, false
			continue
		}
		errors := len(p.Errors)
		diag.Guard(&p.Errors, func() {
			p.blanks = firstInFile
			if decls > 0 {
				p.blanks = afterDecl
			}
			prog.Decls = append(prog.Decls, p.parseDecl())
			p.expect(TNewline, "the end of the line")
		})
		decls++
		if len(p.Errors) != errors {
			failed = true
			for !p.atEndOfInput() {
				p.skip()
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
			// The interpolation's first token follows its '{' with no space between them.
			sub := &Parser{src: p.src, toks: toks, fileIndex: p.fileNumber(), line: p.line,
				prevEnd: part.Begin, prevKind: TLBrace, joinNext: true}
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
	if !p.atEndOfInput() {
		panic(p.expected("the end of the interpolation"))
	}
	return e
}
