package syntax

import (
	"bytes"
	"minimaxon/internal/diag"
	"strings"
)

const (
	lineEnd      = "\n"
	indentTab    = '\t'
	commentStart = "//"
	// lineBreak is the CRLF that a document written on Windows may use; the formatter writes LF and the file's own
	// ending is put back by the caller that knows it.
	lineBreak = "\r\n"
	// sourceWhitespace is what a line's ends may hold that the formatter removes.
	sourceWhitespace = " \t\r"
)

// formatter is the output of a parser that formats: the tokens of the program, each after the gap the layout puts
// before it.
type formatter struct {
	out []byte
	// depth is the indentation of the line being written, in tabs.
	depth int
	// tokenStart is where the last token's text begins in out.
	tokenStart int
}

func (f *formatter) indent(depth int) {
	for range depth {
		f.out = append(f.out, indentTab)
	}
}

func (f *formatter) blankLine() { f.out = append(f.out, lineEnd...) }

// dropBlankLine takes back the blank line just written.
func (f *formatter) dropBlankLine() {
	if !bytes.HasSuffix(f.out, []byte(lineEnd+lineEnd)) && string(f.out) != lineEnd {
		panic(diag.Internal("the formatter is asked to drop a blank line after %q", f.out))
	}
	f.out = f.out[:len(f.out)-len(lineEnd)]
}

// gapLine writes a line of the gap before a line of code: a blank line, or a comment line indented like the code.
func (f *formatter) gapLine(line string, blank bool, depth int) {
	if blank {
		f.blankLine()
		return
	}
	f.indent(depth)
	f.out = append(f.out, strings.Trim(line, sourceWhitespace)...)
	f.out = append(f.out, lineEnd...)
}

// replaceString rewrites the string literal t, the last token written, with each interpolation formatted: formatted
// holds the text of each of the string's interpolations, in order, and parts are its parts.
func (f *formatter) replaceString(text string, t Token, parts []StrPart, formatted []string) {
	if len(formatted) == 0 {
		return
	}
	var literal []byte
	at, next := int(t.Start), 0
	for _, part := range parts {
		switch part := part.(type) {
		case TextPart:
		case ExprPart:
			literal = append(literal, text[at:part.Begin]...)
			literal = append(literal, formatted[next]...)
			at, next = part.End, next+1
		default:
			panic(diag.Internal("unknown string part %T", part))
		}
	}
	literal = append(literal, text[at:t.End()]...)
	f.out = append(f.out[:f.tokenStart], literal...)
}

// formatToken writes the gap the layout puts before t, and then t. The parser calls it where a checking parser calls
// checkLayout, so the gap comes from the same decisions.
func (p *Parser) formatToken(t *Token) {
	f := p.format
	kind := t.Kind()
	start := lexemeStart(*t)
	switch p.gapBefore(kind) {
	case gapLines:
		p.blankLines(start)
		switch kind {
		case TIndent:
			f.depth++
		case TDedent:
			f.depth--
		case TFileEnd, TEof:
		default:
			f.indent(f.depth)
		}
	case gapEndOfLine:
		if comment, ok := strings.CutPrefix(strings.TrimLeft(p.src.Text[p.prevEnd:start], sourceWhitespace), commentStart); ok {
			f.out = append(f.out, ' ')
			f.out = append(f.out, strings.TrimRight(commentStart+comment, sourceWhitespace)...)
		}
	case gapNone:
	case gapSpace:
		f.out = append(f.out, ' ')
	}
	f.tokenStart = len(f.out)
	if kind == TNewline {
		f.out = append(f.out, lineEnd...)
		return
	}
	f.out = append(f.out, p.src.Text[start:lexemeEnd(*t)]...)
}

// depthAfterIndentTokens is the indentation of the code line that starts at the current token, which is the first of
// the Indent or Dedent tokens that open it, or the line's first token.
func (p *Parser) depthAfterIndentTokens() int {
	depth := p.format.depth
	for _, t := range p.toks[p.p:] {
		switch t.Kind() {
		case TIndent:
			depth++
		case TDedent:
			depth--
		default:
			return depth
		}
	}
	return depth
}

// Format returns text, a Maxon source file, in the one layout the parser accepts: one space or none between tokens,
// one tab per indentation level, and blank lines exactly where the rules put them. It changes only the whitespace
// between tokens, the blank lines and the position of comments; every token, and every comment's text, stays. Line
// endings become LF, a byte order mark goes, and the file ends with a newline.
//
// Source whose layout is wrong still formats when the right layout is unambiguous. Whatever the lexer or the parser
// refuses beyond layout, such as indentation with spaces or a syntax error, is returned as errors with text unchanged.
func Format(file, text string) (string, []*diag.CompileError) {
	normal := normalizeLines(text)
	src := NewSource(normal, []FileSlice{{File: file, Begin: 0, End: len(normal)}})
	var errs []*diag.CompileError
	var toks []Token
	diag.Guard(&errs, func() { toks = lex(src, nil, true) })
	if len(errs) > 0 {
		return text, errs
	}
	p := NewParser(src, toks)
	p.format = &formatter{}
	p.ParseProgram()
	if len(p.Errors) > 0 {
		return text, p.Errors
	}
	out := string(p.format.out)
	verifyFormat(file, src, toks, out)
	return out, nil
}

// normalizeLines drops a byte order mark, ends every line with a newline alone and ends the text with one.
func normalizeLines(text string) string {
	text = strings.TrimPrefix(text, string(utf8Bom))
	text = strings.ReplaceAll(text, lineBreak, lineEnd)
	if text != "" && !strings.HasSuffix(text, lineEnd) {
		text += lineEnd
	}
	return text
}

// verifyFormat checks the formatter's work, which a bug must never put in a file: the output has to pass the strict
// parser, and hold the tokens of the input, apart from the layout inside the interpolations of a string.
func verifyFormat(file string, src *Source, toks []Token, out string) {
	formatted := NewSource(out, []FileSlice{{File: file, Begin: 0, End: len(out)}})
	var errs []*diag.CompileError
	var again []Token
	diag.Guard(&errs, func() {
		again = Lex(formatted, nil)
		p := NewParser(formatted, again)
		p.ParseProgram()
		errs = append(errs, p.Errors...)
	})
	if len(errs) > 0 {
		panic(diag.Internal("the formatted %s does not parse: %v", file, errs[0]))
	}
	if len(again) != len(toks) {
		panic(diag.Internal("formatting %s changed its %d tokens into %d", file, len(toks), len(again)))
	}
	for i, t := range toks {
		before, after := src.Text[t.Start:t.End()], formatted.Text[again[i].Start:again[i].End()]
		if t.Kind() != again[i].Kind() || before != after && !(t.Kind() == TString && strings.Contains(before, "{")) {
			panic(diag.Internal("formatting %s changed token %d from kind %d %q to kind %d %q", file, i,
				t.Kind(), before, again[i].Kind(), after))
		}
	}
}

// FormatMarkdown returns the Markdown document text with the Maxon in its fenced blocks formatted: a block tagged maxon
// or not tagged that is a whole program. Prose, other blocks and blocks that do not parse stay as they are.
func FormatMarkdown(file, text string) string {
	var edits []Edit
	scanMarkdown(text, func(begin, end int) {
		block := text[begin:end]
		if strings.TrimSpace(block) == "" {
			return
		}
		formatted, errs := Format(file, block)
		if len(errs) > 0 {
			return
		}
		if !strings.HasSuffix(block, lineEnd) {
			formatted = strings.TrimSuffix(formatted, lineEnd)
		}
		if strings.Contains(block, lineBreak) {
			formatted = strings.ReplaceAll(formatted, lineEnd, lineBreak)
		}
		if formatted != block {
			edits = append(edits, Edit{Begin: begin, End: end, New: formatted})
		}
	}, nil)
	return ApplyEdits(text, edits)
}
