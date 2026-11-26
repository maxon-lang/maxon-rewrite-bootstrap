package main

import "strings"

import "slices"

// dropUnprovidedMembers leaves out each method and each top-level function of maxon's library that calls a static of a raw type
// that minimaxon does not provide, since what it reads has no counterpart here (README, "Where it differs from Maxon": HeapStats'
// slab, census and raw-allocation figures), so a call of it is the unknown-member error. A top-level declaration that only such code named goes with it, and so on, so the file declares nothing that nothing
// reaches.
func dropUnprovidedMembers(lines []sourceLine) {
	named := map[string]bool{}
	droppedMembers := map[string]bool{}
	inInterface := false
	owner := ""
	for i := range lines {
		l := &lines[i]
		if !l.removed && l.indent == "" && len(l.code) > 0 {
			declIdx := skipModifiers(l.code)
			inInterface = declIdx < len(l.code) && l.code[declIdx].text == "interface"
			owner = declaredName(l.code)
		}
		if inInterface || l.removed || len(l.code) == 0 || l.indent != "	" && l.indent != "" {
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
		dropMember(lines, i, end, owner, named, droppedMembers)
		i = end
	}
	for dropped := true; dropped; {
		dropped = false
		inInterface := false
		owner := ""
		for i := range lines {
			l := &lines[i]
			if l.removed || len(l.code) == 0 {
				continue
			}
			if l.indent == "" {
				declIdx := skipModifiers(l.code)
				inInterface = declIdx < len(l.code) && l.code[declIdx].text == "interface"
				owner = declaredName(l.code)
			}
			if inInterface {
				continue
			}
			if l.indent == "	" && callsDroppedMember(lines, i, droppedMembers) {
				dropMember(lines, i, memberEnd(lines, i), owner, named, droppedMembers)
				dropped = true
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

// dropMember leaves out the function that starts at line i and ends at line end, with its comments, and notes it as a dropped
// member of owner, so that a method that calls it goes too.
func dropMember(lines []sourceLine, i, end int, owner string, named, droppedMembers map[string]bool) {
	declIdx := skipModifiers(lines[i].code)
	if lines[i].indent == "	" && lines[i].code[declIdx].text == "function" && declIdx+1 < len(lines[i].code) {
		droppedMembers[owner+"."+lines[i].code[declIdx+1].text] = true
	}
	dropLines(lines, commentsAbove(lines, i), end, named)
}

// declaredName is the name that a top-level declaration's header writes after its word, or "".
func declaredName(code []token) string {
	declIdx := skipModifiers(code)
	if declIdx+1 >= len(code) {
		return ""
	}
	return code[declIdx+1].text
}

// callsDroppedMember says whether line i opens a method that calls a member that droppedMembers names, as `Owner.member`.
func callsDroppedMember(lines []sourceLine, i int, droppedMembers map[string]bool) bool {
	declIdx := skipModifiers(lines[i].code)
	if declIdx >= len(lines[i].code) || lines[i].code[declIdx].text != "function" {
		return false
	}
	for _, l := range lines[i : memberEnd(lines, i)+1] {
		if l.removed {
			continue
		}
		for k := 0; k+2 < len(l.code); k++ {
			if l.code[k].kind == tokIdent && l.code[k+1].text == "." && l.code[k+2].kind == tokIdent && droppedMembers[l.code[k].text+"."+l.code[k+2].text] {
				return true
			}
		}
	}
	return false
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

// callsUnprovidedBuiltin reports whether the lines call a static of a raw type (__Builtins, __ManagedDirectory) that neither
// the built-in table nor the library declares, or name a raw type that neither declares.
func callsUnprovidedBuiltin(lines []sourceLine) bool {
	for _, l := range lines {
		if l.removed {
			continue
		}
		for k := 0; k < len(l.code); k++ {
			name := l.code[k].text
			if l.code[k].kind != tokIdent || !strings.HasPrefix(name, rawPrefix) {
				continue
			}
			if k+2 < len(l.code) && l.code[k+1].text == "." && l.code[k+2].kind == tokIdent {
				if !provides(rawMembers(name), l.code[k+2].text) {
					return true
				}
			} else if _, declared := libraryRawMembers[name]; !builtinNames[name] && !declared {
				// A raw type that the table lacks, written as a type (maxon's __ManagedFileError, which File.maxon's failure
				// mappers take).
				return true
			}
		}
	}
	return false
}

// rawMembers are the members the built-in table declares on a raw type, which it names with its prefix (__ManagedMemory)
// or, for __Builtins, without, or that minimaxon's library declares on one of its raw types (__ManagedFile).
func rawMembers(name string) []string {
	if members, ok := libraryRawMembers[name]; ok {
		return members
	}
	if members, ok := builtinMethods[name]; ok {
		return members
	}
	return builtinMethods[strings.TrimPrefix(name, rawPrefix)]
}

func provides(names []string, name string) bool {
	return slices.Contains(names, name)
}

// noteNames adds the names a line's code writes to named, those inside the braces of its strings included.
func noteNames(toks []token, named map[string]bool) {
	for _, t := range toks {
		for _, word := range tokenWords(t) {
			named[word] = true
		}
	}
}

// tokenWords are the identifiers a token writes: itself when it is one, those of an interpolation when it is a string.
func tokenWords(t token) []string {
	switch t.kind {
	case tokIdent:
		return []string{t.text}
	case tokString:
		return interpolatedWords(t.text)
	}
	return nil
}

// topLevelName is the name a top-level type, enum, union, typealias, constant or function that is not public declares, or "".
// A public function is an entry point of the library, which a call from a dropped method does not make a helper of it.
func topLevelName(toks []token) string {
	i := skipModifiers(toks)
	if i+1 >= len(toks) {
		return ""
	}
	switch toks[i].text {
	case "type", "enum", "union", "typealias", "let", "var":
		return toks[i+1].text
	case "function":
		if !hasModifier(toks[:i], "public") {
			return toks[i+1].text
		}
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
			if slices.Contains(tokenWords(t), name) {
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
