package main

import "strings"

import "slices"

// dropUnprovidedMembers leaves out each method of maxon's library that calls a static of a raw type that minimaxon does
// not provide, since what it reads has no counterpart here (README, "Where it differs from Maxon": HeapStats' slab, census
// and raw-allocation figures), so a call of it is the unknown-member error. A top-level declaration that only such methods
// named goes with them, and so on, so the file declares nothing that nothing reaches.
func dropUnprovidedMembers(lines []sourceLine) {
	named := map[string]bool{}
	inInterface := false
	for i := range lines {
		l := &lines[i]
		if !l.removed && l.indent == "" && len(l.code) > 0 {
			declIdx := skipModifiers(l.code)
			inInterface = declIdx < len(l.code) && l.code[declIdx].text == "interface"
		}
		if inInterface || l.removed || l.indent != "\t" || len(l.code) == 0 {
			continue
		}
		declIdx := skipModifiers(l.code)
		if declIdx >= len(l.code) || l.code[declIdx].text != "function" {
			continue
		}
		end := memberEnd(lines, i)
		if !callsUnprovidedBuiltin(lines[i : end+1]) {
			i = end
			continue
		}
		dropLines(lines, commentsAbove(lines, i), end, named)
		i = end
	}
	for dropped := true; dropped; {
		dropped = false
		inInterface := false
		for i := range lines {
			l := &lines[i]
			if l.removed || len(l.code) == 0 {
				continue
			}
			if l.indent == "" {
				declIdx := skipModifiers(l.code)
				inInterface = declIdx < len(l.code) && l.code[declIdx].text == "interface"
			}
			if inInterface {
				continue
			}
			first, end, name := reachedOnlyByDropped(lines, i, named)
			if name == "" {
				continue
			}
			dropLines(lines, first, end, named)
			dropped = true
		}
	}
}

// dropLines leaves out the lines from first to last, and notes the names they write.
func dropLines(lines []sourceLine, first, last int, named map[string]bool) {
	for j := first; j <= last; j++ {
		noteNames(lines[j].code, named)
		lines[j].removed = true
	}
}

// reachedOnlyByDropped says, for the declaration that line i opens, which lines it spans (comments above included) and its
// name when that name was written by a dropped line and no kept line writes it: a top-level declaration, or a private member
// of a type, which only a dropped method reached. It gives "" for any other line.
func reachedOnlyByDropped(lines []sourceLine, i int, named map[string]bool) (first, last int, name string) {
	l := lines[i]
	var end int
	switch l.indent {
	case "":
		name = topLevelName(l.code)
		if name == "" || !named[name] {
			return 0, 0, ""
		}
		var ok bool
		if end, ok = declarationEnd(lines, i); !ok {
			return 0, 0, ""
		}
	case "\t":
		declIdx := skipModifiers(l.code)
		if declIdx+1 >= len(l.code) || l.code[declIdx].text != "function" || hasModifier(l.code[:declIdx], "public") {
			return 0, 0, ""
		}
		name = l.code[declIdx+1].text
		if !named[name] {
			return 0, 0, ""
		}
		end = memberEnd(lines, i)
	default:
		return 0, 0, ""
	}
	if namedOutside(lines, name, i, end) {
		return 0, 0, ""
	}
	return commentsAbove(lines, i), end, name
}

// rawPrefix starts the name of a type of maxon's raw surface, which the built-in table declares without it.
const rawPrefix = "__"

// callsUnprovidedBuiltin reports whether the lines call a static of a raw type (__Builtins, __ManagedDirectory) that the
// built-in table does not declare.
func callsUnprovidedBuiltin(lines []sourceLine) bool {
	for _, l := range lines {
		if l.removed {
			continue
		}
		for k := 0; k+2 < len(l.code); k++ {
			name := l.code[k].text
			if l.code[k].kind == tokIdent && strings.HasPrefix(name, rawPrefix) && l.code[k+1].text == "." && l.code[k+2].kind == tokIdent && !provides(rawMembers(name), l.code[k+2].text) {
				return true
			}
		}
	}
	return false
}

// rawMembers are the members the built-in table declares on a raw type, which it names with its prefix (__ManagedMemory)
// or, for __Builtins, without.
func rawMembers(name string) []string {
	if members, ok := builtinMethods[name]; ok {
		return members
	}
	return builtinMethods[strings.TrimPrefix(name, rawPrefix)]
}

func provides(names []string, name string) bool {
	return slices.Contains(names, name)
}

// noteNames adds the names a line's code writes to named.
func noteNames(toks []token, named map[string]bool) {
	for _, t := range toks {
		if t.kind == tokIdent {
			named[t.text] = true
		}
	}
}

// topLevelName is the name a top-level type, enum, union, typealias or constant declares, or "".
func topLevelName(toks []token) string {
	i := skipModifiers(toks)
	if i+1 >= len(toks) {
		return ""
	}
	switch toks[i].text {
	case "type", "enum", "union", "typealias", "let", "var":
		return toks[i+1].text
	}
	return ""
}

// namedOutside reports whether a line that is kept, outside the lines from first to last, names name.
func namedOutside(lines []sourceLine, name string, first, last int) bool {
	for k, l := range lines {
		if l.removed || (k >= first && k <= last) {
			continue
		}
		for _, t := range l.code {
			if t.kind == tokIdent && t.text == name {
				return true
			}
		}
	}
	return false
}

// hasModifier says whether the modifiers of a declaration (the tokens before its word) include the word.
func hasModifier(modifiers []token, word string) bool {
	return slices.ContainsFunc(modifiers, func(t token) bool { return t.kind == tokIdent && t.text == word })
}
