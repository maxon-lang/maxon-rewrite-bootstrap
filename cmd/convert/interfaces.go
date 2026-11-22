package main

import (
	"fmt"
	"slices"
	"strings"
)

// interfaceWheres are the relations between the associated types of maxon's library interfaces that maxon's source does
// not state, since maxon checks an extension of an interface again for each conformer, and that minimaxon, which checks it
// once against the interface, needs written (README, "Where it differs from Maxon"): the 'where' clause the converter
// gives the interface's declaration.
var interfaceWheres = map[string]string{
	"Iterable": "where Iter implements Iterator with Element",
}

// replacedMembers are the members of extensions of maxon's library interfaces that a form of minimaxon's own replaces, so
// the converter leaves them out: `withIterator()` and its typealias, for which a for walks `enumerated()`.
var replacedMembers = map[string][]string{
	"Iterable": {"withIterator", "WithIterSelf"},
}

// lentRequirements are the requirements of maxon's library interfaces that hand out an element of what the receiver walks,
// which minimaxon's declare `returns borrowed`, so that an iterator lends an element that is not Copyable rather than giving a
// copy of it (README, "Where it differs from Maxon"): a copy is an explicit `clone()`.
var lentRequirements = map[string][]string{
	"Iterator": {"current"},
}

// lendRequirements writes `borrowed` after the `returns` of each requirement of lentRequirements.
func lendRequirements(lines []sourceLine) {
	lent := []string(nil)
	for i := range lines {
		l := &lines[i]
		if l.removed || len(l.code) == 0 {
			continue
		}
		declIdx := skipModifiers(l.code)
		if l.indent == "" {
			lent = nil
			if declIdx+1 < len(l.code) && l.code[declIdx].text == "interface" {
				lent = lentRequirements[l.code[declIdx+1].text]
			}
			continue
		}
		if len(lent) == 0 || declIdx+1 >= len(l.code) || l.code[declIdx].text != "function" || !slices.Contains(lent, l.code[declIdx+1].text) {
			continue
		}
		for k, t := range l.code {
			if t.text == "returns" && (k+1 >= len(l.code) || l.code[k+1].text != "borrowed") {
				l.code = slices.Insert(l.code, k+1, token{kind: tokIdent, text: "borrowed"})
				break
			}
		}
	}
}

// addInterfaceWheres gives each declaration of an interface of interfaceWheres that states no 'where' the one it needs.
func addInterfaceWheres(lines []sourceLine) {
	for i := range lines {
		l := &lines[i]
		if l.removed || len(l.code) == 0 || l.indent != "" {
			continue
		}
		declIdx := skipModifiers(l.code)
		if declIdx+1 >= len(l.code) || l.code[declIdx].text != "interface" || hasToken(l.code, "where") {
			continue
		}
		clause, ok := interfaceWheres[l.code[declIdx+1].text]
		if !ok {
			continue
		}
		for _, t := range lex(clause) {
			if t.kind != tokSpace && t.kind != tokNewline {
				l.code = append(l.code, t)
			}
		}
	}
}

// dropReplacedMembers leaves out each member of replacedMembers from the extensions of its interface, with the comment above
// it.
func dropReplacedMembers(lines []sourceLine) {
	members := []string(nil)
	for i := 0; i < len(lines); i++ {
		l := &lines[i]
		if l.removed || len(l.code) == 0 {
			continue
		}
		if l.indent == "" {
			members = nil
			declIdx := skipModifiers(l.code)
			if declIdx+1 < len(l.code) && l.code[declIdx].text == "extension" {
				members = replacedMembers[l.code[declIdx+1].text]
			}
			continue
		}
		declIdx := skipModifiers(l.code)
		if len(members) == 0 || declIdx+1 >= len(l.code) || !slices.Contains(members, l.code[declIdx+1].text) {
			continue
		}
		last := i
		if l.code[declIdx].text == "function" {
			last = memberEnd(lines, i)
		}
		for k := i - 1; k >= 0 && len(lines[k].code) == 0 && lines[k].comment != ""; k-- {
			lines[k].removed = true
		}
		for k := i; k <= last; k++ {
			lines[k].removed = true
		}
		i = last
	}
}

// dropSelfClones leaves out a `clone()` method whose body is `return self`, with the comment above it: every Copyable type
// has that clone built in (README, "Where it differs from Maxon"), which no declaration may declare again.
func dropSelfClones(lines []sourceLine) {
	for i := 0; i < len(lines); i++ {
		l := &lines[i]
		if l.removed || l.indent == "" || len(l.code) == 0 {
			continue
		}
		declIdx := skipModifiers(l.code)
		if declIdx+3 >= len(l.code) || l.code[declIdx].text != "function" || l.code[declIdx+1].text != "clone" || l.code[declIdx+2].text != "(" || l.code[declIdx+3].text != ")" {
			continue
		}
		last := memberEnd(lines, i)
		body := []string(nil)
		for k := i + 1; k < last; k++ {
			if len(lines[k].code) > 0 {
				body = append(body, tokenText(lines[k].code))
			}
		}
		if !slices.Equal(body, []string{"return self"}) {
			continue
		}
		for k := i - 1; k >= 0 && len(lines[k].code) == 0 && lines[k].comment != ""; k-- {
			lines[k].removed = true
		}
		for k := i; k <= last; k++ {
			lines[k].removed = true
		}
		i = last
	}
}

// tokenText is the tokens of a line joined by single spaces.
func tokenText(toks []token) string {
	words := make([]string, len(toks))
	for i, t := range toks {
		words[i] = t.text
	}
	return strings.Join(words, " ")
}

// memberEnd is the line of the `end` that closes the member that line start opens, at its indentation.
func memberEnd(lines []sourceLine, start int) int {
	for k := start + 1; k < len(lines); k++ {
		if len(lines[k].code) > 0 && lines[k].indent == lines[start].indent && lines[k].code[0].text == "end" {
			return k
		}
	}
	panic(fmt.Sprintf("line %d: the member has no end", lines[start].origLine))
}
