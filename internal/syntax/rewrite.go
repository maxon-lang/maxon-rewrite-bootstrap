package syntax

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"strings"
)

// Edit replaces Text[Begin:End] of a source with New. Refactorings produce edits rather than rewritten text, so the
// command line applies them to files and a language server can send them to an editor as they are.
type Edit struct {
	Begin int
	End   int
	New   string
}

// KeywordEdits renames every use of the keyword old in src to new, in text order. It works on tokens, so a name, a
// label, a comment or a string's text that happens to read old is left alone, while a keyword inside an
// interpolation is renamed. old must be a keyword of the current grammar: once the grammar no longer reserves it, a
// use of it cannot be told from a name. A file the lexer refuses panics with its CompileError.
func KeywordEdits(src *Source, old, new string) []Edit {
	if !ast.IsReserved(old) {
		panic(diag.Internal("'%s' is not a keyword of the current grammar: rename it before changing Maxon.g4", old))
	}
	var edits []Edit
	collectKeyword(src, 0, Lex(src, nil), old, new, &edits)
	return edits
}

// collectKeyword appends an edit for every keyword token in toks, which start in file, reading old, looking into
// interpolations.
func collectKeyword(src *Source, file int, toks []Token, old, new string, edits *[]Edit) {
	for _, t := range toks {
		switch k := t.Kind(); {
		case k == TFileEnd:
			file++
		case k > TEof && k < numTokens:
			if src.Text[t.Start:t.End()] == old {
				*edits = append(*edits, Edit{Begin: int(t.Start), End: t.End(), New: new})
			}
		case k == TString:
			var parts []StrPart
			src.readString(file, int(t.Start), &parts)
			for _, p := range parts {
				if e, ok := p.(ExprPart); ok {
					collectKeyword(src, file, LexInterpolation(src, file, e.Begin, e.End, nil), old, new, edits)
				}
			}
		}
	}
}

// ApplyEdits returns text with edits, which are in text order and do not overlap, applied.
func ApplyEdits(text string, edits []Edit) string {
	var out strings.Builder
	at := 0
	for _, e := range edits {
		if e.Begin < at {
			panic(diag.Internal("edits overlap or are out of order at offset %d", e.Begin))
		}
		out.WriteString(text[at:e.Begin])
		out.WriteString(e.New)
		at = e.End
	}
	out.WriteString(text[at:])
	return out.String()
}
