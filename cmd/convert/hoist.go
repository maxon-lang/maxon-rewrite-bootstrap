package main

import "slices"

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
