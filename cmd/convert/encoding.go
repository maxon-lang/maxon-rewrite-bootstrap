package main

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// The characters minimaxon refuses anywhere in a source file that maxon accepts in a literal or a comment (README,
// "Source text"): the control characters but the tab and the newline, the Unicode line terminators and the
// bidirectional controls.
const (
	asciiDelete        = 0x7F
	nextLine           = 0x85
	lineSeparator      = 0x2028
	paragraphSeparator = 0x2029
	bidiEmbeddingFirst = 0x202A
	bidiEmbeddingLast  = 0x202E
	bidiIsolateFirst   = 0x2066
	bidiIsolateLast    = 0x2069
	maxByte            = 0xFF
)

func isRefusedCharacter(r rune) bool {
	switch {
	case r == '\t' || r == '\n':
		return false
	case r < ' ' || r == asciiDelete:
		return true
	case r == nextLine || r == lineSeparator || r == paragraphSeparator:
		return true
	}
	return (r >= bidiEmbeddingFirst && r <= bidiEmbeddingLast) || (r >= bidiIsolateFirst && r <= bidiIsolateLast)
}

// escapeRefusedCharacters writes each refused character of a string literal as its escape and, in a byte string, a
// character that is one byte as that byte's escape. A comment, which has no escapes, names the character by its code
// point, and its malformed UTF-8, which maxon reads past, becomes U+FFFD. Code keeps what it holds: maxon refuses
// such a character there too. The source has LF line endings.
func escapeRefusedCharacters(src string) string {
	if !hasRefusedText(src) {
		return src
	}
	var b strings.Builder
	for _, t := range lex(src) {
		switch t.kind {
		case tokString:
			b.WriteString(mapRefused(t.text, func(r rune) string { return fmt.Sprintf(`\u%04x`, r) }))
		case tokByteString:
			b.WriteString(mapRefused(t.text, func(r rune) string {
				if r <= maxByte {
					return fmt.Sprintf(`\x%02x`, r)
				}
				return string(r)
			}))
		case tokLineComment, tokBlockComment:
			b.WriteString(strings.ToValidUTF8(mapRefused(t.text, func(r rune) string { return fmt.Sprintf("U+%04X", r) }), string(utf8.RuneError)))
		default:
			b.WriteString(t.text)
		}
	}
	return b.String()
}

// hasRefusedText reports whether src holds a refused character or malformed UTF-8 anywhere, so that a source with
// neither, which is nearly every one, is not lexed again.
func hasRefusedText(src string) bool {
	return !utf8.ValidString(src) || strings.ContainsFunc(src, isRefusedCharacter)
}

// mapRefused writes each refused character of text by escape, and keeps every other byte, malformed UTF-8 too.
func mapRefused(text string, escape func(rune) string) string {
	var b strings.Builder
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if isRefusedCharacter(r) {
			b.WriteString(escape(r))
		} else {
			b.WriteString(text[i : i+size])
		}
		i += size
	}
	return b.String()
}
