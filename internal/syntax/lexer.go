package syntax

import (
	"math/bits"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"unicode/utf8"
)

type StrPart interface {
	strPart()
}

type TextPart struct {
	Text string
}

// ExprPart is an interpolation: the offsets of its expression in the module text.
type ExprPart struct {
	Begin int
	End   int
}

func (TextPart) strPart() {}
func (ExprPart) strPart() {}

// Token is a token's kind and where it is in the module text: the kind in the low bits of kindLength and the length
// above them, so a token is 8 bytes. Its text, value and position are read from the text when they are needed. A
// label's token covers the text between its quotes.
type Token struct {
	kindLength uint32
	Start      uint32
}

const (
	tokenKindBits = 8
	tokenKindMask = 1<<tokenKindBits - 1
	// MaxTokenLength is the longest token the length bits hold.
	MaxTokenLength = 1<<(32-tokenKindBits) - 1
)

func (t Token) Kind() T { return T(t.kindLength & tokenKindMask) }

func (t Token) Len() int { return int(t.kindLength >> tokenKindBits) }

func (t Token) End() int { return int(t.Start) + t.Len() }

func init() {
	if numTokens > tokenKindMask+1 {
		panic(diag.Internal("%d token kinds do not fit in %d bits", numTokens, tokenKindBits))
	}
}

const (
	newlineText = "\\n"
	fileEndText = "<end of file>"
	eofText     = "<eof>"
	stringText  = "\"...\""
)

type lexer struct {
	src   *Source
	text  string
	file  int
	pos   int
	end   int
	depth int
	toks  []Token
}

// Lex lexes every file of src into buffer, ending each file with a TFileEnd and the module with a TEof.
func Lex(src *Source, buffer []Token) []Token {
	lx := &lexer{src: src, text: src.Text, toks: buffer[:0]}
	for i, f := range src.Files {
		lx.lexRange(i, f.Begin, f.End)
	}
	return lx.finish(len(src.Text))
}

// LexInterpolation lexes the expression of an interpolation at text[begin:end] of file.
func LexInterpolation(src *Source, file, begin, end int, buffer []Token) []Token {
	lx := &lexer{src: src, text: src.Text, toks: buffer[:0]}
	lx.lexRange(file, begin, end)
	return lx.finish(end)
}

func (lx *lexer) finish(at int) []Token {
	lx.emit(TEof, at, at)
	return lx.toks
}

func (lx *lexer) lexRange(file, begin, end int) {
	lx.file, lx.pos, lx.end, lx.depth = file, begin, end, 0
	lx.lexSlice()
	if n := len(lx.toks); n > 0 && lx.toks[n-1].Kind() != TNewline && lx.toks[n-1].Kind() != TFileEnd {
		lx.emit(TNewline, end, end)
	}
	lx.emit(TFileEnd, end, end)
}

func (lx *lexer) emit(kind T, start, end int) {
	if end-start > MaxTokenLength {
		panic(lx.fail(diag.ErrUnexpectedToken, "a token is longer than 16 MB; split it", start))
	}
	lx.toks = append(lx.toks, Token{kindLength: uint32(kind) | uint32(end-start)<<tokenKindBits, Start: uint32(start)})
}

func isAsciiDigit(c byte) bool { return c >= '0' && c <= '9' }

func isAsciiHexDigit(c byte) bool {
	return isAsciiDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// lexSlice runs the generated DFA (tokens_gen.go) from each position to the longest token there. It never backs
// off: a match that stops in a state accepting no token is an error naming the unfinished token.
func (lx *lexer) lexSlice() {
	text := lx.text[:lx.end]
	for lx.pos < len(text) {
		start := lx.pos
		state := dfaStart
		i := start
		for i < len(text) {
			next := int(dfaNext[state+int(dfaClass[text[i]])])
			if next == 0 {
				break
			}
			state = next
			i++
		}
		kind := dfaAccept[state/dfaClasses]
		switch kind {
		case skipToken:
			lx.pos = i
			continue
		case noToken:
			panic(lx.unfinished(state, start))
		}
		if tokenScanned[kind] {
			i = lx.scanRest(kind, start)
		}
		lx.pos = i
		switch kind {
		case TNewline:
			if n := len(lx.toks); lx.depth > 0 || n == 0 || lx.toks[n-1].Kind() == TNewline {
				continue
			}
		case TLParen, TLBrace:
			lx.depth++
		case TRParen, TRBrace:
			lx.depth = max(0, lx.depth-1)
		case TLabel:
			// A label's token is the text between its quotes.
			start, i = start+1, i-1
		}
		lx.emit(kind, start, i)
	}
}

// unfinished is the error for input that stops in a state accepting no token.
func (lx *lexer) unfinished(state, start int) *diag.CompileError {
	if msg := dfaIncomplete[state/dfaClasses]; msg != "" {
		return lx.fail(diag.ErrUnexpectedToken, msg, start)
	}
	r, _ := utf8.DecodeRuneInString(lx.text[start:lx.end])
	return lx.fail(diag.ErrUnexpectedToken, "unexpected character '"+string(r)+"'", start)
}

func (lx *lexer) scanString(start int) int { return lx.src.readString(lx.file, start, nil) }

func (lx *lexer) fail(code, msg string, at int) *diag.CompileError {
	return lx.src.errorAt(code, msg, lx.file, at)
}

// readInteger reads the integer literal at text[at:end], which the lexer's DFA matched, returning its value and its
// end.
func (s *Source) readInteger(file, at, end int) (ast.I128, int) {
	text := s.Text
	pos := at
	radix := uint64(10)
	if text[pos] == '0' && pos+1 < end {
		switch text[pos+1] {
		case 'x':
			radix = 16
		case 'b':
			radix = 2
		case 'o':
			radix = 8
		}
		if radix != 10 {
			pos += 2
		}
	}

	var value ast.I128
	digits := 0
	for ; pos < end && (isAsciiHexDigit(text[pos]) || text[pos] == '_'); pos++ {
		ch := text[pos]
		if ch == '_' {
			continue
		}
		d := hexDigit(ch)
		if d >= radix {
			break
		}
		hi, lo := bits.Mul64(value.Lo, radix)
		lo, carry := bits.Add64(lo, d, 0)
		value = ast.I128{Hi: int64(hi + carry), Lo: lo}
		if value.Gt(ast.U64Max) {
			panic(s.errorAt(diag.ErrExpressionError, "integer literal is larger than u64.max", file, at))
		}
		digits++
	}
	if digits == 0 {
		panic(diag.Internal("the integer literal at %d has no digits, but the lexer matched it", at))
	}
	return value, pos
}

func hexDigit(ch byte) uint64 {
	if isAsciiDigit(ch) {
		return uint64(ch - '0')
	}
	return uint64(ch|0x20-'a') + 10
}

// stringReader reads one string literal; the lexer uses it to find the literal's end and report its errors, and the
// parser to take its parts.
type stringReader struct {
	src   *Source
	text  string
	file  int
	open  int
	pos   int
	end   int
	parts *[]StrPart
	// The current text part is text[run:pos] until an escape forces it into buf.
	run     int
	buf     []byte
	escaped bool
}

// readString reads the string literal at text[open], returning its end. When parts is not nil it also appends the
// literal's text runs and interpolations to it.
func (s *Source) readString(file, open int, parts *[]StrPart) int {
	r := stringReader{src: s, text: s.Text, file: file, open: open, pos: open + 1, end: s.Files[file].End, parts: parts}
	return r.read()
}

func (r *stringReader) fail(msg string, at int) *diag.CompileError {
	return r.src.errorAt(diag.ErrUnexpectedToken, msg, r.file, at)
}

func (r *stringReader) read() int {
	r.run = r.pos
	added := false
	for {
		if r.pos >= r.end || r.text[r.pos] == '\n' {
			panic(r.fail("unterminated string literal", r.open))
		}
		c := r.text[r.pos]
		switch c {
		case '"':
			if r.pos > r.run || r.escaped || !added {
				r.flushText()
			}
			r.pos++
			return r.pos
		case '\\':
			r.escape()
		case '}':
			panic(r.fail("a '}' in a string is written '\\}'", r.pos))
		case '{':
			if r.pos > r.run || r.escaped {
				r.flushText()
			}
			r.interpolation()
			added = true
		default:
			if r.escaped {
				r.buf = append(r.buf, c)
			}
			r.pos++
		}
	}
}

func (r *stringReader) flushText() {
	if r.parts != nil {
		text := r.text[r.run:r.pos]
		if r.escaped {
			text = string(r.buf)
		}
		*r.parts = append(*r.parts, TextPart{Text: text})
	}
	r.escaped = false
	r.buf = r.buf[:0]
}

func (r *stringReader) escape() {
	if !r.escaped && r.parts != nil {
		r.buf = append(r.buf[:0], r.text[r.run:r.pos]...)
	}
	r.escaped = true
	r.pos++
	at := r.pos
	var e byte
	if r.pos < r.end {
		e = r.text[r.pos]
	}
	r.pos++
	var ch rune
	switch e {
	case 'n':
		ch = '\n'
	case 't':
		ch = '\t'
	case 'r':
		ch = '\r'
	case '0':
		ch = 0
	case '\\', '"', '\'', '{', '}':
		ch = rune(e)
	case 'x':
		ch = rune(r.readHex(2))
	case 'u':
		ch = rune(r.readHex(4))
	default:
		var escaped rune
		if at < r.end {
			escaped, _ = utf8.DecodeRuneInString(r.text[at:r.end])
		}
		panic(r.fail("unknown escape sequence '\\"+string(escaped)+"'", r.open))
	}
	if r.parts != nil {
		r.buf = utf8.AppendRune(r.buf, ch)
	}
}

func (r *stringReader) readHex(n int) int {
	v := 0
	for range n {
		if r.pos >= r.end || !isAsciiHexDigit(r.text[r.pos]) {
			panic(r.fail("malformed hex escape", r.open))
		}
		v = v*16 + int(hexDigit(r.text[r.pos]))
		r.pos++
	}
	return v
}

func (r *stringReader) interpolation() {
	r.pos++
	start := r.pos
	nest := 0
	parens := 0
	for r.pos < r.end && r.text[r.pos] != '\n' && !(r.text[r.pos] == '}' && nest == 0) {
		switch ch := r.text[r.pos]; {
		case ch == '{':
			nest++
		case ch == '}':
			nest--
		case ch == '(' || ch == '[':
			parens++
		case ch == ')' || ch == ']':
			parens--
		case ch == ':' && parens == 0 && nest == 0:
			panic(r.src.errorAt(diag.ErrUnsupported, "format specifiers are not supported by minimaxon", r.file, r.pos))
		case ch == '"':
			r.pos++
			for r.pos < r.end && r.text[r.pos] != '"' && r.text[r.pos] != '\n' {
				if r.text[r.pos] == '\\' {
					r.pos++
				}
				r.pos++
			}
		}
		r.pos++
	}
	if r.pos >= r.end || r.text[r.pos] != '}' {
		panic(r.fail("unterminated interpolation in string literal", start))
	}
	if r.parts != nil {
		*r.parts = append(*r.parts, ExprPart{Begin: start, End: r.pos})
	}
	r.pos++
	r.run = r.pos
}
