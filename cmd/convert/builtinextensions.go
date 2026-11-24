package main

import (
	"slices"
	"strings"
)

// builtinTypeParams are the names the built-in table gives the type parameter of each generic type minimaxon builds in, where
// maxon's library names it `Element`: an extension of the type names it as the table does (README, "Extensions").
var builtinTypeParams = map[string]string{
	"Array": "T",
	"List":  "T",
}

// maxonElementParam is the name of the type parameter of maxon's generic collections.
const maxonElementParam = "Element"

// unavailableIntrinsicConditions are the conditions of an `if` of maxon's library that ask the compiler about its native
// storage hashing, which minimaxon has no counterpart of: the block takes a shortcut that the code after it must agree with, so
// the converter leaves the block out and the code after it is the whole of the method.
var unavailableIntrinsicConditions = []string{"__Builtins.elementsHashAsStorage()"}

// rewriteBuiltinExtensions writes each extension of a type that minimaxon builds in (Array and List) as minimaxon has it:
// the type parameter under the built-in table's name, the receiver's own storage as `self.managed`, the members the
// built-in declares left out (the table has them natively), no conformance where the extension has a `where` (minimaxon
// has no conditional conformance, so the methods stay and the interface is not implemented), and no block that asks for the
// native storage hash.
func rewriteBuiltinExtensions(lines []sourceLine) {
	for i := 0; i < len(lines); i++ {
		l := &lines[i]
		if l.removed || len(l.code) == 0 || l.indent != "" {
			continue
		}
		declIdx := skipModifiers(l.code)
		if declIdx+1 >= len(l.code) || l.code[declIdx].text != "extension" {
			continue
		}
		target := l.code[declIdx+1].text
		param, ok := builtinTypeParams[target]
		if !ok {
			continue
		}
		last := topLevelEnd(lines, i)
		dropConformanceBeforeWhere(l)
		for k := i; k <= last; k++ {
			renameInLine(&lines[k], param)
		}
		dropNativeMembers(lines, i, last, target)
		dropUnavailableIntrinsicBlocks(lines, i, last)
		i = last
	}
}

// renameInLine writes the type parameter of a maxon collection as param and the field `managed` as `self.managed`.
func renameInLine(l *sourceLine, param string) {
	var out []token
	for k, t := range l.code {
		afterDot := k > 0 && l.code[k-1].text == "."
		switch {
		case t.kind == tokIdent && t.text == maxonElementParam && !afterDot:
			t.text = param
		case t.kind == tokIdent && t.text == "managed" && !afterDot:
			out = append(out, token{kind: tokIdent, text: "self"}, token{kind: tokPunct, text: "."})
		}
		out = append(out, t)
	}
	l.code = out
}

// dropConformanceBeforeWhere removes the `implements` clause of an extension header that has a `where`.
func dropConformanceBeforeWhere(l *sourceLine) {
	impl := findToken(l.code, 0, "implements")
	where := findToken(l.code, 0, "where")
	if impl < 0 || where < impl {
		return
	}
	l.code = slices.Delete(l.code, impl, where)
}

// extendedBuiltinRecords are the record types minimaxon builds in whose library declaration has members that the built-in
// lacks (Clock's `nowMs`, Directory's `removeTree`): the declaration is kept as an extension of the built-in, which a
// program reaches as it reaches the type.
var extendedBuiltinRecords = []string{"Clock", "Directory"}

// rewriteBuiltinRecords writes the declaration of each of extendedBuiltinRecords as an extension of the built-in type, with
// the members that the built-in declares left out, and the fields, which the built-in's own storage replaces.
func rewriteBuiltinRecords(lines []sourceLine) {
	for i := 0; i < len(lines); i++ {
		l := &lines[i]
		if l.removed || len(l.code) == 0 || l.indent != "" {
			continue
		}
		declIdx := skipModifiers(l.code)
		if declIdx+1 >= len(l.code) || l.code[declIdx].text != "type" || !slices.Contains(extendedBuiltinRecords, l.code[declIdx+1].text) {
			continue
		}
		last := topLevelEnd(lines, i)
		target := l.code[declIdx+1].text
		l.code = slices.Concat([]token{{kind: tokIdent, text: "extension"}}, l.code[declIdx+1:])
		dropFields(lines, i, last)
		dropNativeMembers(lines, i, last, target)
		i = last
	}
}

// dropFields leaves out the fields of the type from header to last, each a line of its own.
func dropFields(lines []sourceLine, header, last int) {
	for k := header + 1; k < last; k++ {
		l := &lines[k]
		if l.removed || len(l.code) == 0 || l.indent != "\t" {
			continue
		}
		if word := l.code[skipModifiers(l.code)].text; word == "var" || word == "let" {
			l.removed = true
		}
	}
}

// dropNativeMembers leaves out the members of the extension from header to last that the built-in type of its name declares.
func dropNativeMembers(lines []sourceLine, header, last int, target string) {
	for k := header + 1; k < last; k++ {
		l := &lines[k]
		if l.removed || len(l.code) == 0 || l.indent != "\t" {
			continue
		}
		declIdx := skipModifiers(l.code)
		if declIdx+1 >= len(l.code) || l.code[declIdx].text != "function" || !slices.Contains(builtinMethods[target], l.code[declIdx+1].text) {
			continue
		}
		end := memberEnd(lines, k)
		for j := commentsAbove(lines, k); j <= end; j++ {
			lines[j].removed = true
		}
		k = end
	}
}

// dropUnavailableIntrinsicBlocks leaves out each `if` block between header and last whose condition is one of
// unavailableIntrinsicConditions.
func dropUnavailableIntrinsicBlocks(lines []sourceLine, header, last int) {
	for k := header + 1; k < last; k++ {
		l := &lines[k]
		if l.removed || len(l.code) < 2 || l.code[0].text != "if" || !slices.Contains(unavailableIntrinsicConditions, conditionText(l.code[1:])) {
			continue
		}
		end := memberEnd(lines, k)
		for j := k; j <= end; j++ {
			lines[j].removed = true
		}
		k = end
	}
}

// conditionText is the tokens of an `if` condition, up to its label, written without spaces.
func conditionText(toks []token) string {
	var text strings.Builder
	for _, t := range toks {
		if t.kind == tokLabel {
			break
		}
		text.WriteString(t.text)
	}
	return text.String()
}

// typeNamesHere are the types of maxon's that minimaxon types as another: the payload of WaitDeadline.within is a Count here,
// where maxon's is its own alias of that range.
var typeNamesHere = map[string]string{"WaitDeadlineMs": "Count"}

// rewriteTypeNames writes each name of typeNamesHere as minimaxon's.
func rewriteTypeNames(lines []sourceLine) {
	for i := range lines {
		l := &lines[i]
		for k, t := range l.code {
			here, ok := typeNamesHere[t.text]
			if ok && t.kind == tokIdent && (k == 0 || l.code[k-1].text != ".") {
				l.code[k].text = here
			}
		}
	}
}
