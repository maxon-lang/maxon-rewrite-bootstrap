package main

import (
	"slices"
	"strconv"
	"strings"
)

// A discard of a value that already exists is E3067 in minimaxon (README, "Where it differs from Maxon": A discard takes a
// value something made). maxon writes `_ = name` to name a binding that its unused-name rule (E3012) would refuse otherwise,
// which does nothing else, and `let _ = value` as a statement that reads a value: a use that a test of a moved value
// refuses, or the statement of a block that has nothing else to do (`let _ = 0 as Num`). minimaxon checks no unused name,
// so the line goes, and the statement of a match arm becomes the `break` that leaves the match. Where the reading is the
// point, at a line that a test's expected error names, the discard binds the value to a name of its own that nothing
// reads, which reads it as maxon does; so does `let _ = value` where the block needs a statement, the one statement of its
// block. A bare `_ = name` there, which maxon writes only to name a binding, stays, with this warning, which skips its test.
const discardWarning = "language difference: a discard of a value that already exists is E3067, and the block whose one statement it is may not be empty (README, Where it differs from Maxon: A discard takes a value something made)"

// discardEffectWords are the words of an operand that do something, or that end the value of a match arm's statement.
var discardEffectWords = map[string]bool{
	"await": true, "async": true, "spawn": true, "panic": true, "try": true, "function": true, "match": true, "if": true,
	"else": true, "and": true, "or": true, "fallthrough": true, "gives": true, "freeze": true, "merge": true, "swap": true,
	"destruct": true, "modify": true, "acquire": true, "with": true,
}

// ignoredName is the start of the name that a read which a discard wrote is bound to.
const ignoredName = "ignored"

// dropDiscardsOfExistingValues removes or rewrites each discard of a value that already exists, in maxon's spelling (before
// the `let` of `let _ =` goes).
func dropDiscardsOfExistingValues(lines []sourceLine, names programNames, warn func(*sourceLine, string)) {
	fresh := freshIgnoredNames(lines)
	for i := range lines {
		line := &lines[i]
		if line.removed {
			continue
		}
		at, bound, ok := existingDiscardAt(line.code)
		if !ok {
			continue
		}
		// maxon numbers the lines of a fragment from the line of its opening fence.
		named := names.expectsError && slices.Contains(names.errorLines, names.lineOffset+line.origLine+1)
		switch {
		case named:
			line.code = bindDiscard(line.code, at, bound, fresh())
		case at > 0:
			line.code = append(line.code[:at:at], token{kind: tokIdent, text: "break", line: line.code[at].line})
		case isSoleStatement(lines, i):
			if !bound {
				warn(line, discardWarning)
				continue
			}
			line.code = bindDiscard(line.code, at, bound, fresh())
		default:
			line.removed = true
		}
	}
}

// bindDiscard writes the discard at at, `let _ = value` or `_ = value`, as `let name = value`.
func bindDiscard(toks []token, at int, bound bool, name string) []token {
	value := toks[at+2:]
	if bound {
		value = toks[at+3:]
	}
	head := append(toks[:at:at], token{kind: tokIdent, text: "let"}, token{kind: tokIdent, text: name}, token{kind: tokPunct, text: "="})
	return append(head, value...)
}

// existingDiscardAt is where a line's discard of a value that already exists starts, at its `let` if it has one, and
// whether it has one: the start of the line, or the statement after an arm's `then`. The value is names joined by dots,
// literals and casts, which call, send and await nothing; a value of any other form is left to the compiler, which decides
// by what each call resolves to.
func existingDiscardAt(toks []token) (int, bool, bool) {
	for i := 0; i+2 < len(toks); i++ {
		if i > 0 && toks[i-1].text != "then" {
			continue
		}
		bound := toks[i].text == "let"
		at := i
		if bound {
			at++
		}
		if at+2 >= len(toks) || toks[at].text != "_" || toks[at+1].text != "=" {
			continue
		}
		if isExistingValue(toks[at+2:]) {
			return i, bound, true
		}
		return 0, false, false
	}
	return 0, false, false
}

// isExistingValue is whether an operand's tokens are names joined by dots, literals and casts alone.
func isExistingValue(toks []token) bool {
	for _, t := range toks {
		switch t.kind {
		case tokIdent:
			if discardEffectWords[t.text] {
				return false
			}
		case tokInt, tokFloat:
		case tokString:
			if strings.Contains(t.text, "{") {
				return false
			}
		case tokPunct:
			if t.text != "." && t.text != "-" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// freshIgnoredNames gives the names that discards are bound to, `ignored1` on, each one that the program names nowhere.
func freshIgnoredNames(lines []sourceLine) func() string {
	taken := map[string]bool{}
	for _, l := range lines {
		for _, t := range l.code {
			if t.kind == tokIdent && strings.HasPrefix(t.text, ignoredName) {
				taken[t.text] = true
			}
		}
	}
	n := 0
	return func() string {
		for {
			n++
			name := ignoredName + strconv.Itoa(n)
			if !taken[name] {
				return name
			}
		}
	}
}

// isSoleStatement is whether the line at i is the one statement of a block: the line of code before it opens the block,
// ending with its label, and the one after it ends one.
func isSoleStatement(lines []sourceLine, i int) bool {
	prev, next := -1, -1
	for j := i - 1; j >= 0; j-- {
		if !lines[j].removed && len(lines[j].code) > 0 {
			prev = j
			break
		}
	}
	for j := i + 1; j < len(lines); j++ {
		if !lines[j].removed && len(lines[j].code) > 0 {
			next = j
			break
		}
	}
	if prev < 0 || next < 0 {
		return false
	}
	opener := lines[prev].code
	return opener[len(opener)-1].kind == tokLabel && lines[next].code[0].text == "end"
}
