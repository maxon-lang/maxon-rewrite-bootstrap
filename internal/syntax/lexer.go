package syntax

import (
	"fmt"
	"math/bits"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"strings"
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
	src  *Source
	text string
	file int
	pos  int
	end  int
	// inline is set for an interpolation, which starts at begin.
	inline bool
	begin  int
	depth  int
	// indent is the indentation, in tabs, of the last code line.
	indent int
	toks   []Token
}

// Lex lexes every file of src into buffer. Each code line ends with a TNewline; a line indented one tab more than
// the code line before it starts with a TIndent, and one indented less starts with a TDedent for each tab. Each
// file ends with a TFileEnd and the module with a TEof.
func Lex(src *Source, buffer []Token) []Token {
	lx := &lexer{src: src, text: src.Text, toks: buffer[:0]}
	for i, f := range src.Files {
		lx.lexFile(i, f.Begin, f.End)
	}
	return lx.finish(len(src.Text))
}

// LexInterpolation lexes the expression of an interpolation at text[begin:end] of file: part of one line, with no
// layout of its own.
func LexInterpolation(src *Source, file, begin, end int, buffer []Token) []Token {
	lx := &lexer{src: src, text: src.Text, file: file, pos: begin, end: end, inline: true, begin: begin, toks: buffer[:0]}
	lx.lexLine()
	lx.emit(TFileEnd, end, end)
	return lx.finish(end)
}

func (lx *lexer) finish(at int) []Token {
	lx.emit(TEof, at, at)
	return lx.toks
}

func (lx *lexer) lexFile(file, begin, end int) {
	lx.file, lx.pos, lx.end, lx.depth, lx.indent = file, begin, end, 0, 0
	if begin < end && lx.text[end-1] != '\n' {
		panic(lx.fail("the file does not end with a newline", end))
	}
	for lx.pos < end {
		lx.lineStart()
		if lx.pos < end {
			lx.lexLine()
		}
	}
	for ; lx.indent > 0; lx.indent-- {
		lx.emit(TDedent, end, end)
	}
	lx.emit(TFileEnd, end, end)
}

func (lx *lexer) emit(kind T, start, end int) {
	if end-start > MaxTokenLength {
		panic(lx.fail("a token is longer than 16 MB; split it", start))
	}
	lx.toks = append(lx.toks, Token{kindLength: uint32(kind) | uint32(end-start)<<tokenKindBits, Start: uint32(start)})
}

func isAsciiDigit(c byte) bool { return c >= '0' && c <= '9' }

func isAsciiHexDigit(c byte) bool {
	return isAsciiDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// lineStart reads the blank lines and comment lines before the next code line, and that line's indentation. A
// line is indented with tabs only, at most one more than the code line before it; a comment line is indented like
// the code line after it, or not at all at the end of the file.
func (lx *lexer) lineStart() {
	var comments []int
	for lx.pos < lx.end {
		lineBegin := lx.pos
		for lx.text[lx.pos] == '\t' {
			lx.pos++
		}
		tabs := lx.pos - lineBegin
		switch c := lx.text[lx.pos]; {
		case c == '\n':
			if tabs > 0 {
				panic(lx.fail("a blank line holds tabs; remove the trailing whitespace", lineBegin))
			}
			lx.pos++
			continue
		case c == ' ':
			panic(lx.fail("a line is indented with a space; indent with tabs only", lx.pos))
		case c == '/' && lx.text[lx.pos+1] == '/':
			comments = append(comments, lineBegin)
			lx.pos = lx.comment(lx.pos) + 1
			continue
		}
		for _, at := range comments {
			lx.checkCommentIndent(at, tabs)
		}
		switch {
		case tabs > lx.indent+1:
			panic(lx.fail("a line is indented more than one tab deeper than the line above", lineBegin))
		case tabs == lx.indent+1:
			lx.emit(TIndent, lx.pos, lx.pos)
		default:
			for range lx.indent - tabs {
				lx.emit(TDedent, lx.pos, lx.pos)
			}
		}
		lx.indent = tabs
		return
	}
	for _, at := range comments {
		lx.checkCommentIndent(at, 0)
	}
}

func (lx *lexer) checkCommentIndent(lineBegin, tabs int) {
	got := 0
	for lx.text[lineBegin+got] == '\t' {
		got++
	}
	if got != tabs {
		panic(lx.fail(fmt.Sprintf("a comment line is indented %d tabs, but the code line after it %d; indent them alike",
			got, tabs), lineBegin))
	}
}

// comment checks the comment at text[start:], which runs to the end of its line, and returns where the line ends.
func (lx *lexer) comment(start int) int {
	end := start + strings.IndexByte(lx.text[start:lx.end], '\n')
	if last := lx.text[end-1]; last == ' ' || last == '\t' || last == '\r' {
		panic(lx.fail("a comment ends with whitespace; remove it", end-1))
	}
	return end
}

// lexLine runs the generated DFA (tokens_gen.go) from each position to the longest token there, up to the end of
// the line. It never backs off: a match that stops in a state accepting no token is an error naming the
// unfinished token.
func (lx *lexer) lexLine() {
	text := lx.text[:lx.end]
	for lx.pos < len(text) {
		start := lx.pos
		// Most tokens follow one space; step over it here rather than in a round of the DFA. Any other
		// whitespace goes through the DFA, whose TWhitespace case checks it.
		if text[start] == ' ' && start+1 < len(text) && !(lx.inline && start == lx.begin) {
			if c := text[start+1]; c != ' ' && c != '\t' && c != '\r' && c != '\n' {
				start++
			}
		}
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
		case noToken:
			panic(lx.unfinished(state, start))
		case TWhitespace:
			panic(lx.badSpace(start, i))
		case TLineComment:
			if lx.inline {
				panic(lx.fail("a comment inside an interpolation; move it after the line", start))
			}
			if text[start-1] != ' ' {
				panic(lx.fail("a comment after code is separated from it by one space", start))
			}
			lx.pos = lx.comment(start)
			continue
		case TBlockComment:
			panic(lx.fail("a block comment is not allowed; write // comments on their own lines or after code", start))
		}
		if tokenScanned[kind] {
			i = lx.scanRest(kind, start)
		}
		lx.pos = i
		switch kind {
		case TNewline:
			if lx.depth > 0 {
				panic(lx.fail("a line breaks inside parentheses; keep what they hold on one line", start))
			}
			lx.emit(kind, start, i)
			return
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

// badSpace is the error for whitespace inside a line that is not the one space lexLine steps over between two
// tokens.
func (lx *lexer) badSpace(start, end int) *diag.CompileError {
	text := lx.text
	for at := start; at < end; at++ {
		switch text[at] {
		case '\t':
			return lx.fail("a tab inside a line; tabs only indent, and tokens are separated by one space", at)
		case '\r':
			return lx.fail("a carriage return; end lines with a newline alone", at)
		}
	}
	switch {
	case end == lx.end || text[end] == '\n':
		return lx.fail("trailing whitespace; remove it", start)
	case lx.inline && start == lx.begin:
		return lx.fail("a space at the start of an interpolation; remove it", start)
	case end-start > 1:
		return lx.fail("tokens are separated by one space, not several", start)
	}
	panic(diag.Internal("the space at %d is one space between two tokens, which lexLine steps over", start))
}

// unfinished is the error for input that stops in a state accepting no token.
func (lx *lexer) unfinished(state, start int) *diag.CompileError {
	if msg := dfaIncomplete[state/dfaClasses]; msg != "" {
		return lx.fail(msg, start)
	}
	r, _ := utf8.DecodeRuneInString(lx.text[start:lx.end])
	return lx.fail("unexpected character '"+string(r)+"'", start)
}

func (lx *lexer) scanString(start int) int { return lx.src.readString(lx.file, start, nil) }

func (lx *lexer) fail(msg string, at int) *diag.CompileError {
	return lx.src.errorAt(diag.ErrUnexpectedToken, msg, lx.file, at)
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
