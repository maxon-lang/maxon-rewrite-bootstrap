package main

import (
	"math/bits"
	"unicode/utf8"
)

type T int

const (
	TIdent T = iota
	TInt
	TStr
	TQuote
	TNewline
	TFileEnd
	TEof
	TLParen
	TRParen
	TLBrace
	TRBrace
	TLBracket
	TRBracket
	TComma
	TColon
	TDot
	TPlus
	TMinus
	TStar
	TSlash
	TAssign
	TEqEq
	TNotEq
	TLt
	TLe
	TGt
	TGe
	TAt
)

type StrPart interface {
	strPart()
}

type TextPart struct {
	Text string
}

type ExprPart struct {
	Source string
	Line   int
	Col    int
}

func (TextPart) strPart() {}
func (ExprPart) strPart() {}

type Token struct {
	Kind    T
	Text    string
	Line    int
	Col     int
	Payload any
}

func (t Token) IntValue() I128 { return t.Payload.(I128) }

func (t Token) Parts() []StrPart {
	parts, _ := t.Payload.([]StrPart)
	return parts
}

func (t Token) Is(word string) bool { return t.Kind == TIdent && t.Text == word }

type FileSlice struct {
	File  string
	Begin int
	End   int
}

const (
	newlineText = "\\n"
	fileEndText = "<end of file>"
	eofText     = "<eof>"
	stringText  = "\"...\""
)

type Lexer struct {
	slices []FileSlice
	file   string
	src    []byte
	end    int
	pos    int
	line   int
	col    int
	depth  int
	toks   []Token
	pool   map[string]string
}

func newLexer(src []byte, slices []FileSlice, buffer []Token, pool map[string]string, startLine, startCol int) *Lexer {
	lx := &Lexer{src: src, slices: slices, line: startLine, col: startCol}
	if len(slices) > 0 {
		lx.file = slices[0].File
	}
	lx.toks = buffer[:0]
	if pool == nil {
		pool = map[string]string{}
	}
	lx.pool = pool
	return lx
}

func Lex(src []byte, slices []FileSlice, buffer []Token, pool map[string]string) []Token {
	return newLexer(src, slices, buffer, pool, 1, 1).lex()
}

func LexText(file, text string, line, col int) []Token {
	return newLexer([]byte(text), []FileSlice{{File: file, Begin: 0, End: len(text)}}, nil, nil, line, col).lex()
}

func (lx *Lexer) intern(text []byte) string {
	if s, ok := lx.pool[string(text)]; ok {
		return s
	}
	s := string(text)
	lx.pool[s] = s
	return s
}

func (lx *Lexer) lex() []Token {
	for i, slice := range lx.slices {
		lx.file = slice.File
		lx.pos = slice.Begin
		lx.end = slice.End
		lx.depth = 0
		if i > 0 {
			lx.line = 1
			lx.col = 1
		}
		lx.lexSlice()
		if n := len(lx.toks); n > 0 && lx.toks[n-1].Kind != TNewline && lx.toks[n-1].Kind != TFileEnd {
			lx.toks = append(lx.toks, Token{Kind: TNewline, Text: newlineText, Line: lx.line, Col: lx.col})
		}
		lx.toks = append(lx.toks, Token{Kind: TFileEnd, Text: fileEndText, Line: lx.line, Col: lx.col})
	}
	lx.toks = append(lx.toks, Token{Kind: TEof, Text: eofText, Line: lx.line, Col: lx.col})
	return lx.toks
}

func isAsciiLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func isAsciiDigit(c byte) bool { return c >= '0' && c <= '9' }

func isAsciiHexDigit(c byte) bool {
	return isAsciiDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func (lx *Lexer) lexSlice() {
	for {
		lx.skipSpaceAndComments()
		if lx.pos >= lx.end {
			break
		}

		c := lx.src[lx.pos]
		l := lx.line
		cl := lx.col

		if c == '\n' {
			lx.advance()
			if n := len(lx.toks); lx.depth == 0 && n > 0 && lx.toks[n-1].Kind != TNewline {
				lx.toks = append(lx.toks, Token{Kind: TNewline, Text: newlineText, Line: l, Col: cl})
			}
			continue
		}

		if isAsciiLetter(c) || c == '_' {
			start := lx.pos
			for lx.pos < lx.end && (isAsciiLetter(lx.src[lx.pos]) || isAsciiDigit(lx.src[lx.pos]) || lx.src[lx.pos] == '_') {
				lx.advance()
			}
			lx.toks = append(lx.toks, Token{Kind: TIdent, Text: lx.intern(lx.src[start:lx.pos]), Line: l, Col: cl})
			continue
		}

		if isAsciiDigit(c) {
			lx.lexNumber(l, cl)
			continue
		}

		if c == '"' {
			lx.lexString(l, cl)
			continue
		}

		if c == '\'' {
			lx.lexQuote(l, cl)
			continue
		}

		lx.lexPunct(c, l, cl)
	}
}

func (lx *Lexer) fail(code, msg string, l, c int) *CompileError {
	return NewError(code, msg, lx.file, l, c)
}

func (lx *Lexer) advance() {
	if lx.pos >= len(lx.src) {
		return
	}
	if lx.src[lx.pos] == '\n' {
		lx.line++
		lx.col = 1
	} else {
		lx.col++
	}
	lx.pos++
}

func (lx *Lexer) peek(ahead int) byte {
	if lx.pos+ahead < lx.end {
		return lx.src[lx.pos+ahead]
	}
	return 0
}

func (lx *Lexer) runeAt(at int) rune {
	if at >= lx.end {
		return 0
	}
	r, _ := utf8.DecodeRune(lx.src[at:lx.end])
	return r
}

func (lx *Lexer) skipSpaceAndComments() {
	for lx.pos < lx.end {
		c := lx.src[lx.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			lx.advance()
		case c == '/' && lx.peek(1) == '/':
			for lx.pos < lx.end && lx.src[lx.pos] != '\n' {
				lx.advance()
			}
		case c == '/' && lx.peek(1) == '*':
			l := lx.line
			cl := lx.col
			lx.advance()
			lx.advance()
			for lx.pos < lx.end && !(lx.src[lx.pos] == '*' && lx.peek(1) == '/') {
				lx.advance()
			}
			if lx.pos >= lx.end {
				panic(lx.fail(ErrUnexpectedToken, "unterminated block comment", l, cl))
			}
			lx.advance()
			lx.advance()
		default:
			return
		}
	}
}

func (lx *Lexer) lexNumber(l, cl int) {
	radix := uint64(10)
	if lx.src[lx.pos] == '0' {
		switch lx.peek(1) {
		case 'x':
			radix = 16
		case 'b':
			radix = 2
		case 'o':
			radix = 8
		}
		if radix != 10 {
			lx.advance()
			lx.advance()
		}
	}

	start := lx.pos
	var value I128
	digits := 0
	for lx.pos < lx.end && (isAsciiHexDigit(lx.src[lx.pos]) || lx.src[lx.pos] == '_') {
		ch := lx.src[lx.pos]
		if ch == '_' {
			lx.advance()
			continue
		}
		var d uint64
		if isAsciiDigit(ch) {
			d = uint64(ch - '0')
		} else {
			d = uint64(ch|0x20-'a') + 10
		}
		if d >= radix {
			break
		}
		hi, lo := bits.Mul64(value.Lo, radix)
		lo, carry := bits.Add64(lo, d, 0)
		value = I128{Hi: int64(hi + carry), Lo: lo}
		if value.Gt(U64Max) {
			panic(lx.fail(ErrExpressionError, "integer literal is larger than u64.max", l, cl))
		}
		digits++
		lx.advance()
	}

	if digits == 0 {
		panic(lx.fail(ErrExpressionError, "malformed integer literal", l, cl))
	}
	if lx.pos < lx.end && lx.src[lx.pos] == '.' && isAsciiDigit(lx.peek(1)) {
		panic(lx.fail(ErrUnsupported, "float literals are not supported by minimaxon", l, cl))
	}

	lx.toks = append(lx.toks, Token{Kind: TInt, Text: lx.intern(lx.src[start:lx.pos]), Line: l, Col: cl, Payload: value})
}

func (lx *Lexer) readEscape(l, cl int) rune {
	lx.advance()
	at := lx.pos
	e := lx.peek(0)
	lx.advance()
	switch e {
	case 'n':
		return '\n'
	case 't':
		return '\t'
	case 'r':
		return '\r'
	case '0':
		return 0
	case '\\':
		return '\\'
	case '"':
		return '"'
	case '\'':
		return '\''
	case '{':
		return '{'
	case '}':
		return '}'
	case 'x':
		return rune(lx.readHex(2, l, cl))
	case 'u':
		return rune(lx.readHex(4, l, cl))
	}
	panic(lx.fail(ErrUnexpectedToken, "unknown escape sequence '\\"+string(lx.runeAt(at))+"'", l, cl))
}

func (lx *Lexer) readHex(n, l, cl int) int {
	v := 0
	for i := 0; i < n; i++ {
		ch := lx.peek(0)
		if !isAsciiHexDigit(ch) {
			panic(lx.fail(ErrUnexpectedToken, "malformed hex escape", l, cl))
		}
		var d int
		if isAsciiDigit(ch) {
			d = int(ch - '0')
		} else {
			d = int(ch|0x20-'a') + 10
		}
		v = v*16 + d
		lx.advance()
	}
	return v
}

func (lx *Lexer) lexString(l, cl int) {
	lx.advance()
	var parts []StrPart
	var text []byte

	for {
		if lx.pos >= lx.end || lx.src[lx.pos] == '\n' {
			panic(lx.fail(ErrUnexpectedToken, "unterminated string literal", l, cl))
		}
		c := lx.src[lx.pos]
		if c == '"' {
			lx.advance()
			break
		}
		if c == '\\' {
			text = utf8.AppendRune(text, lx.readEscape(l, cl))
			continue
		}
		if c == '}' {
			panic(lx.fail(ErrUnexpectedToken, "a '}' in a string is written '\\}'", lx.line, lx.col))
		}
		if c == '{' {
			if len(text) > 0 {
				parts = append(parts, TextPart{Text: string(text)})
				text = text[:0]
			}
			lx.advance()
			el := lx.line
			ec := lx.col
			start := lx.pos
			nest := 0
			parens := 0
			for lx.pos < lx.end && lx.src[lx.pos] != '\n' && !(lx.src[lx.pos] == '}' && nest == 0) {
				switch ch := lx.src[lx.pos]; {
				case ch == '{':
					nest++
				case ch == '}':
					nest--
				case ch == '(' || ch == '[':
					parens++
				case ch == ')' || ch == ']':
					parens--
				case ch == ':' && parens == 0 && nest == 0:
					panic(lx.fail(ErrUnsupported, "format specifiers are not supported by minimaxon", lx.line, lx.col))
				case ch == '"':
					lx.advance()
					for lx.pos < lx.end && lx.src[lx.pos] != '"' && lx.src[lx.pos] != '\n' {
						if lx.src[lx.pos] == '\\' {
							lx.advance()
						}
						lx.advance()
					}
				}
				lx.advance()
			}
			if lx.pos >= lx.end || lx.src[lx.pos] != '}' {
				panic(lx.fail(ErrUnexpectedToken, "unterminated interpolation in string literal", el, ec))
			}
			parts = append(parts, ExprPart{Source: string(lx.src[start:lx.pos]), Line: el, Col: ec})
			lx.advance()
			continue
		}
		text = append(text, c)
		lx.advance()
	}

	if len(text) > 0 || len(parts) == 0 {
		parts = append(parts, TextPart{Text: string(text)})
	}
	lx.toks = append(lx.toks, Token{Kind: TStr, Text: stringText, Line: l, Col: cl, Payload: parts})
}

func (lx *Lexer) lexQuote(l, cl int) {
	lx.advance()
	start := lx.pos
	for lx.pos < lx.end && lx.src[lx.pos] != '\'' && lx.src[lx.pos] != '\n' {
		lx.advance()
	}
	if lx.pos >= lx.end || lx.src[lx.pos] != '\'' {
		panic(lx.fail(ErrUnexpectedToken, "unterminated label", l, cl))
	}
	text := lx.intern(lx.src[start:lx.pos])
	lx.advance()
	lx.toks = append(lx.toks, Token{Kind: TQuote, Text: text, Line: l, Col: cl})
}

func (lx *Lexer) lexPunct(c byte, l, cl int) {
	var kind T
	length := 1
	switch c {
	case '(':
		kind = TLParen
		lx.depth++
	case ')':
		kind = TRParen
		lx.depth = max(0, lx.depth-1)
	case '{':
		kind = TLBrace
		lx.depth++
	case '}':
		kind = TRBrace
		lx.depth = max(0, lx.depth-1)
	case '[':
		kind = TLBracket
		lx.depth++
	case ']':
		kind = TRBracket
		lx.depth = max(0, lx.depth-1)
	case ',':
		kind = TComma
	case ':':
		kind = TColon
	case '.':
		kind = TDot
	case '+':
		kind = TPlus
	case '-':
		kind = TMinus
	case '*':
		kind = TStar
	case '/':
		kind = TSlash
	case '@':
		kind = TAt
	case '=':
		if lx.peek(1) == '=' {
			kind = TEqEq
			length = 2
		} else {
			kind = TAssign
		}
	case '!':
		if lx.peek(1) != '=' {
			panic(lx.fail(ErrUnexpectedToken, "unexpected character '!'", l, cl))
		}
		kind = TNotEq
		length = 2
	case '<':
		if lx.peek(1) == '=' {
			kind = TLe
			length = 2
		} else {
			kind = TLt
		}
	case '>':
		if lx.peek(1) == '=' {
			kind = TGe
			length = 2
		} else {
			kind = TGt
		}
	case '#':
		panic(lx.fail(ErrUnsupported, "conditional compilation is not supported by minimaxon", l, cl))
	default:
		panic(lx.fail(ErrUnexpectedToken, "unexpected character '"+string(lx.runeAt(lx.pos))+"'", l, cl))
	}

	text := lx.intern(lx.src[lx.pos : lx.pos+length])
	for i := 0; i < length; i++ {
		lx.advance()
	}
	lx.toks = append(lx.toks, Token{Kind: kind, Text: text, Line: l, Col: cl})
}
