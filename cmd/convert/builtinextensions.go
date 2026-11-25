package main

import (
	"slices"
	"strings"
)

// builtinTypeParams are the names the built-in table gives the type parameter of each generic type minimaxon builds in, where
// maxon's library names it `Element`: an extension of the type names it as the table does (README, "Extensions").
var builtinTypeParams = map[string]string{
	"Array": "T",
}

// maxonElementParam is the name of the type parameter of maxon's generic collections.
const maxonElementParam = "Element"

// unavailableIntrinsicConditions are the conditions of an `if` of maxon's library that ask the compiler about its native
// storage hashing, which minimaxon has no counterpart of: the block takes a shortcut that the code after it must agree with, so
// the converter leaves the block out and the code after it is the whole of the method.
var unavailableIntrinsicConditions = []string{"__Builtins.elementsHashAsStorage()"}

// rewriteBuiltinExtensions writes each extension of a type that minimaxon builds in (Array) as minimaxon has it:
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
// lacks (Clock's `nowMs`, Directory's `removeTree`, Process's `currentEnvironmentEntries`): the declaration is kept as an
// extension of the built-in, which a program reaches as it reaches the type.
var extendedBuiltinRecords = []string{"Clock", "Directory", "FilePath", "Process"}

// builtinRecordConformances are the interfaces of a declaration of extendedBuiltinRecords that its extension leaves out:
// FilePath is a literal's type and has its toString as a built-in until the seed that compiles its library type.
var builtinRecordConformances = map[string][]string{"FilePath": {"InitableFromStringLiteral", "Stringable"}}

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
		for _, iface := range builtinRecordConformances[target] {
			dropConformance(l, iface)
		}
		dropEmptyImplements(l)
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

// libraryTypeNamesHere are the types of maxon's library that minimaxon names otherwise (README, "Where it differs from
// Maxon"): maxon's Slot, one value or none, is Cell here. stdlib/files.txt names the module and file it goes to.
var libraryTypeNamesHere = []struct{ maxon, here string }{{"Slot", "Cell"}}

// renameLibraryTypes writes each type of libraryTypeNamesHere that the text names as minimaxon's. A program or a helper of
// the library that declares a type of that name, or takes a type parameter of it, names its own and keeps it, as does a
// file of a program whose other files declare it (ownTypes); a file of the library's root that declares it is the type's
// own, whose comments and messages say its new name too.
func renameLibraryTypes(file string, lines []sourceLine, kind inputKind, ownTypes []string) {
	root := ""
	if kind == sourceFile && !isLibraryHelper(file) {
		root = libraryRoot(file)
	}
	for _, name := range libraryTypeNamesHere {
		old, here := name.maxon, name.here
		own := declaresType(lines, old)
		if (own || slices.Contains(ownTypes, old)) && root == "" || root != "" && helperTypes(root)[old] {
			continue
		}
		label := "'" + old + "'"
		for i := range lines {
			l := &lines[i]
			for k, t := range l.code {
				switch {
				case t.kind == tokIdent && t.text == old && (k == 0 || l.code[k-1].text != "."):
					l.code[k].text = here
				case t.kind == tokLabel && t.text == label:
					l.code[k].text = "'" + here + "'"
				case own && t.kind == tokString:
					l.code[k].text = renameWord(t.text, old, here)
				}
			}
			if own {
				l.comment = renameWord(l.comment, old, here)
				l.trailing = renameWord(l.trailing, old, here)
			}
		}
	}
}

// declaresType says whether the text declares a type, an enum, a union, an interface or a typealias named name, or takes
// a type parameter of that name.
func declaresType(lines []sourceLine, name string) bool {
	for _, l := range lines {
		at := skipModifiers(l.code)
		if at+1 < len(l.code) && slices.Contains([]string{"type", "enum", "union", "interface", "typealias"}, l.code[at].text) && l.code[at+1].text == name {
			return true
		}
		inUses := false
		for _, t := range l.code {
			switch {
			case t.kind == tokIdent && t.text == "uses":
				inUses = true
			case t.kind == tokIdent && (t.text == "implements" || t.text == "where"):
				inUses = false
			case inUses && t.kind == tokIdent && t.text == name:
				return true
			}
		}
	}
	return false
}

// ownLibraryTypes are the types of libraryTypeNamesHere that the program src declares itself.
func ownLibraryTypes(src string) []string {
	lines := splitLines(src)
	var own []string
	for _, name := range libraryTypeNamesHere {
		if declaresType(lines, name.maxon) {
			own = append(own, name.maxon)
		}
	}
	return own
}

// renameWord writes each whole word old in text, and the word with its first letter lower case, as here likewise.
func renameWord(text, old, here string) string {
	lowerOld, lowerHere := strings.ToLower(old[:1])+old[1:], strings.ToLower(here[:1])+here[1:]
	var b strings.Builder
	isWord := func(c byte) bool {
		return c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
	}
	for i := 0; i < len(text); {
		matched := false
		for _, w := range [][2]string{{old, here}, {lowerOld, lowerHere}} {
			end := i + len(w[0])
			if (i == 0 || !isWord(text[i-1])) && strings.HasPrefix(text[i:], w[0]) && (end == len(text) || !isWord(text[end])) {
				b.WriteString(w[1])
				i, matched = end, true
				break
			}
		}
		if !matched {
			b.WriteByte(text[i])
			i++
		}
	}
	return b.String()
}

// primitiveExtensionTargets are the built-in types whose text the interpolation writes itself, so maxon's library gives
// them a toString that minimaxon leaves out, with their Stringable conformance (README, "Where it differs from Maxon").
var primitiveExtensionTargets = []string{"int", "float", "bool"}

// dropPrimitiveToStrings leaves out the toString of each extension of a primitive type, and Stringable from its
// implements clause.
func dropPrimitiveToStrings(lines []sourceLine) {
	for i := 0; i < len(lines); i++ {
		l := &lines[i]
		if l.removed || len(l.code) == 0 || l.indent != "" {
			continue
		}
		declIdx := skipModifiers(l.code)
		if declIdx+1 >= len(l.code) || l.code[declIdx].text != "extension" || !slices.Contains(primitiveExtensionTargets, l.code[declIdx+1].text) {
			continue
		}
		dropConformance(l, "Stringable")
		last := topLevelEnd(lines, i)
		for k := i + 1; k < last; k++ {
			m := &lines[k]
			if m.removed || m.indent != "\t" || len(m.code) == 0 {
				continue
			}
			at := skipModifiers(m.code)
			if at+1 >= len(m.code) || m.code[at].text != "function" || m.code[at+1].text != "toString" {
				continue
			}
			end := memberEnd(lines, k)
			for j := commentsAbove(lines, k); j <= end; j++ {
				lines[j].removed = true
			}
			if k > 0 && lines[k-1].blank {
				lines[k-1].removed = true
			}
			k = end
		}
		i = last
	}
}

// dropConformance removes the interface name, and the comma that joins it to the list, from a header's implements
// clause.
func dropConformance(l *sourceLine, name string) {
	for k, t := range l.code {
		if t.kind != tokIdent || t.text != name {
			continue
		}
		from, to := k, k+1
		switch {
		case to < len(l.code) && l.code[to].text == ",":
			to++
			for to < len(l.code) && l.code[to].kind == tokSpace {
				to++
			}
		case from > 0 && l.code[from-1].text == ",":
			from--
		case from > 0 && l.code[from-1].kind == tokSpace && from > 1 && l.code[from-2].text == ",":
			from -= 2
		}
		l.code = slices.Delete(l.code, from, to)
		return
	}
}

// dropEmptyImplements removes an implements clause that names nothing.
func dropEmptyImplements(l *sourceLine) {
	impl := findToken(l.code, 0, "implements")
	if impl < 0 {
		return
	}
	k := impl + 1
	for k < len(l.code) && l.code[k].kind == tokSpace {
		k++
	}
	if k < len(l.code) && l.code[k].kind == tokIdent && l.code[k].text != "where" {
		return
	}
	from := impl
	for from > 0 && l.code[from-1].kind == tokSpace {
		from--
	}
	l.code = slices.Delete(l.code, from, k)
}
