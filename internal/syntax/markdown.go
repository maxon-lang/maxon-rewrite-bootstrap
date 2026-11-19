package syntax

import (
	"minimaxon/internal/diag"
	"strings"
)

const (
	codeFence = "```"
	codeSpan  = "`"
	// maxonFenceTag marks a fenced block as Maxon; a block with no tag is taken to be Maxon too, and one with any other
	// tag (powershell, an IR listing) is left alone.
	maxonFenceTag = "maxon"
)

// MarkdownKeywordEdits renames the keyword old to new in the Maxon of a Markdown document: its inline code spans and
// its fenced blocks tagged maxon or not tagged. Prose is left alone, since a word there may be English. A span or block
// the lexer refuses, such as IR text or a command, is left alone too; refused counts those that contain old, and
// firstRefusal is the first of them, so the caller can point at what it may have to change by hand.
func MarkdownKeywordEdits(file, text, old, new string) (edits []Edit, refused int, firstRefusal *diag.CompileError) {
	src := NewSource(text, []FileSlice{{File: file, Begin: 0, End: len(text)}})
	attempt := func(begin, end, firstLine int, rename func() []Edit) {
		var errs []*diag.CompileError
		var found []Edit
		diag.Guard(&errs, func() { found = rename() })
		if len(errs) == 0 {
			edits = append(edits, found...)
			return
		}
		if strings.Contains(text[begin:end], old) {
			refused++
			if firstRefusal == nil {
				firstRefusal = errs[0]
				// A block is lexed as a file of its own, so its lines count from its start.
				firstRefusal.Line += firstLine - 1
			}
		}
	}
	scanMarkdown(text,
		func(begin, end int) {
			attempt(begin, end, strings.Count(text[:begin], "\n")+1, func() []Edit {
				block := NewSource(text[begin:end], []FileSlice{{File: file, Begin: 0, End: end - begin}})
				found := KeywordEdits(block, old, new)
				for i := range found {
					found[i].Begin += begin
					found[i].End += begin
				}
				return found
			})
		},
		func(begin, end int) {
			attempt(begin, end, 1, func() []Edit { return KeywordEditsInLine(src, 0, begin, end, old, new) })
		})
	return edits, refused, firstRefusal
}

// scanMarkdown finds the Maxon in a Markdown document: it calls block with the text of each fenced block that is tagged
// maxon or not tagged, and span with each non-empty inline code span (when span is not nil), both as offsets into text.
func scanMarkdown(text string, block, span func(begin, end int)) {
	at := 0
	for at < len(text) {
		lineEnd := strings.IndexByte(text[at:], '\n')
		if lineEnd < 0 {
			lineEnd = len(text)
		} else {
			lineEnd += at
		}
		line := text[at:lineEnd]
		if fence, ok := strings.CutPrefix(strings.TrimLeft(line, " 	"), codeFence); ok {
			blockBegin := min(lineEnd+1, len(text))
			blockEnd, after := closingFence(text, blockBegin)
			if tag := strings.TrimSpace(fence); tag == "" || tag == maxonFenceTag {
				block(blockBegin, blockEnd)
			}
			at = after
			continue
		}
		for col := 0; span != nil && col < len(line); {
			open := strings.Index(line[col:], codeSpan)
			if open < 0 {
				break
			}
			begin := at + col + open + len(codeSpan)
			close := strings.Index(text[begin:lineEnd], codeSpan)
			if close < 0 {
				break
			}
			end := begin + close
			if end > begin {
				span(begin, end)
			}
			col = end + len(codeSpan) - at
		}
		at = lineEnd + 1
	}
}

// closingFence finds the fence that closes a block whose text starts at begin: it returns where the block's text ends
// (the start of the closing fence's line) and where the text after the fence's line starts. An unclosed block runs to
// the end of the document.
func closingFence(text string, begin int) (end, after int) {
	for at := begin; at < len(text); {
		lineEnd := strings.IndexByte(text[at:], '\n')
		next := len(text)
		if lineEnd >= 0 {
			next = at + lineEnd + 1
		}
		if strings.HasPrefix(strings.TrimLeft(text[at:next], " \t"), codeFence) {
			return at, next
		}
		at = next
	}
	return len(text), len(text)
}
