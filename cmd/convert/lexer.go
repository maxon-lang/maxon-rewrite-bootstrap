package main

import "unicode/utf8"

type tokKind int

const (
	tokIdent tokKind = iota
	tokInt
	tokFloat
	tokString
	tokByteString
	tokLabel
	tokLineComment
	tokBlockComment
	tokShebang
	tokHashDirective
	tokAnnot
	tokPunct
	tokSpace
	tokNewline
	tokOther
)

type token struct {
	kind  tokKind
	text  string
	start int
	end   int
	line  int // 1-based line number where the token starts
	// unclosed marks a string literal that its line ends, which maxon's lexer refuses there.
	unclosed bool
}

// lex tokenizes an existing Maxon source file losslessly (after CRLF/BOM normalization).
func lex(src string) []token {
	var toks []token
	i, n := 0, len(src)
	line := 1

	for i < n {
		start := i
		startLine := line
		c := src[i]

		switch {
		case c == '\n':
			i++
			line++
			toks = append(toks, token{kind: tokNewline, text: "\n", start: start, end: i, line: startLine})

		case c == ' ' || c == '\t':
			for i < n && (src[i] == ' ' || src[i] == '\t') {
				i++
			}
			toks = append(toks, token{kind: tokSpace, text: src[start:i], start: start, end: i, line: startLine})

		case c == '/' && i+1 < n && src[i+1] == '/':
			for i < n && src[i] != '\n' {
				i++
			}
			toks = append(toks, token{kind: tokLineComment, text: src[start:i], start: start, end: i, line: startLine})

		case c == '/' && i+1 < n && src[i+1] == '*':
			i += 2
			for i < n {
				if src[i] == '*' && i+1 < n && src[i+1] == '/' {
					i += 2
					break
				}
				if src[i] == '\n' {
					line++
				}
				i++
			}
			toks = append(toks, token{kind: tokBlockComment, text: src[start:i], start: start, end: i, line: startLine})

		case c == '#' && i == 0 && i+1 < n && src[i+1] == '!':
			for i < n && src[i] != '\n' {
				i++
			}
			toks = append(toks, token{kind: tokShebang, text: src[start:i], start: start, end: i, line: startLine})

		case c == '#' && i+1 < n && isIdentStart(src[i+1]):
			i++
			for i < n && isIdentPart(src[i]) {
				i++
			}
			toks = append(toks, token{kind: tokHashDirective, text: src[start:i], start: start, end: i, line: startLine})

		case c == '@' && i+1 < n && isIdentStart(src[i+1]):
			i++
			for i < n && isIdentPart(src[i]) {
				i++
			}
			toks = append(toks, token{kind: tokAnnot, text: src[start:i], start: start, end: i, line: startLine})

		case c == 'b' && i+1 < n && src[i+1] == '"':
			i += 2
			for i < n && src[i] != '\n' {
				if src[i] == '\\' && i+1 < n && src[i+1] != '\n' {
					i += 2
					continue
				}
				if src[i] == '"' {
					i++
					break
				}
				i++
			}
			toks = append(toks, token{kind: tokByteString, text: src[start:i], start: start, end: i, line: startLine})

		case c == '"':
			var closed bool
			i, closed = scanLiteral(src, i)
			toks = append(toks, token{kind: tokString, text: src[start:i], start: start, end: i, line: startLine, unclosed: !closed})

		case c == '\'':
			i++
			for i < n && src[i] != '\n' {
				if src[i] == '\\' && i+1 < n && src[i+1] != '\n' {
					i += 2
					continue
				}
				if src[i] == '\'' {
					i++
					break
				}
				i++
			}
			toks = append(toks, token{kind: tokLabel, text: src[start:i], start: start, end: i, line: startLine})

		case isIdentStart(c):
			i++
			for i < n && isIdentPart(src[i]) {
				i++
			}
			toks = append(toks, token{kind: tokIdent, text: src[start:i], start: start, end: i, line: startLine})

		case isDigit(c):
			kind := tokInt
			if c == '0' && i+1 < n && (src[i+1] == 'x' || src[i+1] == 'b' || src[i+1] == 'o') {
				i += 2
				for i < n && (isHexDigit(src[i]) || src[i] == '_') {
					i++
				}
			} else {
				for i < n && (isDigit(src[i]) || src[i] == '_') {
					i++
				}
				if i+1 < n && src[i] == '.' && isDigit(src[i+1]) {
					kind = tokFloat
					i++
					for i < n && isDigit(src[i]) {
						i++
					}
					if i < n && (src[i] == 'e' || src[i] == 'E') {
						j := i + 1
						if j < n && (src[j] == '+' || src[j] == '-') {
							j++
						}
						if j < n && isDigit(src[j]) {
							i = j
							for i < n && isDigit(src[i]) {
								i++
							}
						}
					}
				}
			}
			toks = append(toks, token{kind: kind, text: src[start:i], start: start, end: i, line: startLine})

		case isTwoCharPunct(src, i):
			i += 2
			toks = append(toks, token{kind: tokPunct, text: src[start:i], start: start, end: i, line: startLine})

		case isSinglePunct(c):
			i++
			toks = append(toks, token{kind: tokPunct, text: src[start:i], start: start, end: i, line: startLine})

		default:
			// A character that starts no token stays whole, so a non-ASCII one is never split into bytes.
			_, size := utf8.DecodeRuneInString(src[i:])
			i += size
			toks = append(toks, token{kind: tokOther, text: src[start:i], start: start, end: i, line: startLine})
		}
	}

	return toks
}

// scanString scans a string literal starting at the opening '"' at i, handling escapes and nested {...} interpolations.
func scanString(src string, i int) int {
	end, _ := scanLiteral(src, i)
	return end
}

// scanLiteral is scanString, and reports whether the literal's closing quote ended it rather than its line.
func scanLiteral(src string, i int) (int, bool) {
	n := len(src)
	i++ // skip opening '"'
	for i < n && src[i] != '\n' {
		switch src[i] {
		case '\\':
			if i+1 < n && src[i+1] != '\n' {
				i += 2
			} else {
				i++
			}
		case '"':
			return i + 1, true
		case '{':
			i = scanInterp(src, i)
		default:
			i++
		}
	}
	return i, false
}

// scanInterp scans from '{' to its matching '}' inside a string literal, respecting nested braces and strings.
func scanInterp(src string, i int) int {
	n := len(src)
	depth := 1
	i++
	for i < n && src[i] != '\n' && depth > 0 {
		switch src[i] {
		case '"':
			i = scanString(src, i)
		case '{':
			depth++
			i++
		case '}':
			depth--
			i++
		default:
			i++
		}
	}
	return i
}

func isIdentStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || isDigit(c)
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isHexDigit(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isTwoCharPunct(src string, i int) bool {
	if i+1 >= len(src) {
		return false
	}
	switch src[i : i+2] {
	case "==", "!=", "<=", ">=":
		return true
	}
	return false
}

func isSinglePunct(c byte) bool {
	switch c {
	case '(', ')', '{', '}', '[', ']', ',', ':', '.', '+', '-', '*', '/', '=', '<', '>':
		return true
	}
	return false
}
