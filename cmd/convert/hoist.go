package main

import (
	"slices"
	"strconv"
)

// genericElementParams are the names of the type parameter of the generic library types in an extension header: maxon's and
// the built-in table's.
var genericElementParams = []string{"Element", "T"}

// hoistRangedTypealiases moves a ranged typealias that no type parameter appears in out of the type or the extension of a
// generic type that declares it, to a top-level declaration of its name before that type: minimaxon has no ranged typealias
// inside a generic type, since each instance would have a type of its own, and one that names no type parameter has the same
// range in every instance. A reference written `Owner.Name` is written `Name`.
func hoistRangedTypealiases(lines []sourceLine) []sourceLine {
	generics := genericTypes(lines)
	var out []sourceLine
	hoisted := map[string][]string{}
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if l.removed || len(l.code) == 0 || l.indent != "" {
			out = append(out, l)
			continue
		}
		declIdx := skipModifiers(l.code)
		if declIdx+1 >= len(l.code) || l.code[declIdx].kind != tokIdent {
			out = append(out, l)
			continue
		}
		word, owner := l.code[declIdx].text, l.code[declIdx+1].text
		if (word != "type" && word != "extension") || !slices.Contains(generics, owner) {
			out = append(out, l)
			continue
		}
		last := topLevelEnd(lines, i)
		params := append(slices.Clone(genericElementParams), usesNames(l.code[declIdx:])...)
		var lifted []sourceLine
		body := []sourceLine{l}
		for k := i + 1; k <= last; k++ {
			m := lines[k]
			if !m.removed && len(m.code) > 0 && m.indent == "\t" && namesRangedTypealias(m.code, params) {
				name := m.code[skipModifiers(m.code)+1].text
				hoisted[owner] = append(hoisted[owner], name)
				m.indent = ""
				lifted = append(lifted, m)
				continue
			}
			body = append(body, m)
		}
		out = append(out, lifted...)
		out = append(out, body...)
		i = last
	}
	for k := range out {
		out[k].code = unqualifyHoisted(out[k].code, hoisted)
	}
	return out
}

// namesRangedTypealias is whether the line is `typealias Name = int(...)` with no type parameter in its range.
func namesRangedTypealias(code []token, params []string) bool {
	declIdx := skipModifiers(code)
	if declIdx+1 >= len(code) || code[declIdx].text != "typealias" || !isRangeTypealias(code, declIdx) {
		return false
	}
	return !slices.ContainsFunc(code[declIdx+2:], func(t token) bool { return t.kind == tokIdent && slices.Contains(params, t.text) })
}

// unqualifyHoisted writes `Owner.Name` as `Name` for each typealias hoisted out of Owner.
func unqualifyHoisted(code []token, hoisted map[string][]string) []token {
	var out []token
	for k := 0; k < len(code); k++ {
		if k+2 < len(code) && code[k+1].text == "." && code[k].kind == tokIdent && slices.Contains(hoisted[code[k].text], code[k+2].text) && (k == 0 || code[k-1].text != ".") {
			out = append(out, code[k+2])
			k += 2
			continue
		}
		out = append(out, code[k])
	}
	return out
}

// blockTemporarySuffix names the variable that holds the value of a block hoisted out of the initializer of the `let` or
// `var` of a name: the name, then this. A block of any other statement is held in `hoisted` and its line.
const (
	blockTemporarySuffix = "Block"
	blockTemporaryPrefix = "hoisted"
)

// hoistBlocksInBrackets binds the value of a block that is the first operand inside brackets
// (`let n = (try f() otherwise 'e' ... end 'e') as T`, `xs.push(match x 'm' ... end 'm')`) on lines of its own and reads
// it where the block was: minimaxon reads no line break inside brackets. Only names and dots stand before the block's
// bracket, so the block runs before everything else the line does, as it does hoisted; a block that something
// conditional may skip (`... if c else ...`) stays, and so does one whose temporary's name is taken, which the warning for
// a block inside brackets then marks.
func hoistBlocksInBrackets(lines []sourceLine) []sourceLine {
	var out []sourceLine
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		closeAt, open, ok := blockInBracketsClose(lines, i)
		if !ok {
			out = append(out, l)
			continue
		}
		prefix, last := l.code[:open], l.code[open-1]
		temp := blockTemporaryPrefix + strconv.Itoa(l.origLine)
		binds := prefix[0].text == "let" || prefix[0].text == "var"
		if binds {
			temp = prefix[1].text + blockTemporarySuffix
		}
		if linesNameIdent(lines, temp) {
			out = append(out, l)
			continue
		}
		bound := []token{{kind: tokIdent, text: "let"}, {kind: tokIdent, text: temp}, {kind: tokPunct, text: "="}}
		l.code = append(bound, l.code[open+1:]...)
		out = append(out, l)
		out = append(out, lines[i+1:closeAt]...)
		end := lines[closeAt]
		suffix := end.code[3:]
		end.code = end.code[:2]
		out = append(out, end)
		use := sourceLine{indent: l.indent, origLine: end.origLine,
			code: slices.Clone(prefix)}
		if last.text != "=" && last.text != "return" {
			use.code = append(use.code, token{kind: tokPunct, text: "("}, token{kind: tokIdent, text: temp}, token{kind: tokPunct, text: ")"})
		} else {
			use.code = append(use.code, token{kind: tokIdent, text: temp})
		}
		use.code = append(use.code, suffix...)
		out = append(out, use)
		i = closeAt
	}
	return out
}

// blockInBracketsClose is the index of the line that closes the block that line i opens inside a bracket, and the index of
// that bracket in the line, when only names and dots stand before the bracket and the block's closing line is
// `end 'label')` and what follows.
func blockInBracketsClose(lines []sourceLine, i int) (int, int, bool) {
	l := lines[i]
	if l.removed || len(l.code) < 3 || !opensBlockInBrackets(l.code) {
		return 0, 0, false
	}
	c := l.code
	// The bracket that stays open to the end of the line is the first one, and nothing but names stands before it.
	open := slices.IndexFunc(c, func(t token) bool { return t.kind == tokPunct && t.text == "(" })
	if open < 1 || open+1 >= len(c) {
		return 0, 0, false
	}
	first := c[0].text
	if first != "let" && first != "var" && first != "return" && (c[0].kind != tokIdent || reservedWords[first] && first != "self") {
		return 0, 0, false
	}
	for k, t := range c[:open] {
		plain := t.kind == tokIdent && (!reservedWords[t.text] || t.text == "self" || k == 0)
		if !plain && t.text != "." && t.text != "=" {
			return 0, 0, false
		}
	}
	if opener := c[open+1].text; opener != "try" && opener != "match" {
		return 0, 0, false
	}
	depth := 0
	for k, t := range c {
		if t.kind != tokPunct {
			continue
		}
		switch t.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
		if depth == 0 && k > open {
			return 0, 0, false
		}
	}
	label := c[len(c)-1].text
	for j := i + 1; j < len(lines); j++ {
		m := lines[j]
		if m.removed || len(m.code) == 0 || m.indent != l.indent || m.code[0].text != "end" {
			continue
		}
		if len(m.code) < 3 || m.code[1].kind != tokLabel || m.code[1].text != label || m.code[2].text != ")" {
			return 0, 0, false
		}
		if slices.ContainsFunc(m.code[3:], func(t token) bool { return t.text == "if" }) {
			return 0, 0, false
		}
		return j, open, true
	}
	return 0, 0, false
}

// linesNameIdent is whether a line of the file has a name token of that text.
func linesNameIdent(lines []sourceLine, name string) bool {
	return slices.ContainsFunc(lines, func(l sourceLine) bool {
		return slices.ContainsFunc(l.code, func(t token) bool { return t.kind == tokIdent && t.text == name })
	})
}
