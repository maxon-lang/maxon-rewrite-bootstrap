package main

import (
	"fmt"
	"slices"
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

// memberEnd is the line of the `end` that closes the member that line start opens, at its indentation.
func memberEnd(lines []sourceLine, start int) int {
	for k := start + 1; k < len(lines); k++ {
		if len(lines[k].code) > 0 && lines[k].indent == lines[start].indent && lines[k].code[0].text == "end" {
			return k
		}
	}
	panic(fmt.Sprintf("line %d: the member has no end", lines[start].origLine))
}
