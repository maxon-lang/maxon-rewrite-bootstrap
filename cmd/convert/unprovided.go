package main

import "slices"

// dropUnprovidedMembers leaves out each method of maxon's library that calls a static of __Builtins that minimaxon does
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
		for j := commentsAbove(lines, i); j <= end; j++ {
			noteNames(lines[j].code, named)
			lines[j].removed = true
		}
		i = end
	}
	for dropped := true; dropped; {
		dropped = false
		for i := range lines {
			l := &lines[i]
			if l.removed || l.indent != "" || len(l.code) == 0 {
				continue
			}
			name := topLevelName(l.code)
			if name == "" || !named[name] {
				continue
			}
			end := topLevelEnd(lines, i)
			if namedOutside(lines, name, i, end) {
				continue
			}
			for j := commentsAbove(lines, i); j <= end; j++ {
				noteNames(lines[j].code, named)
				lines[j].removed = true
			}
			dropped = true
		}
	}
}

// callsUnprovidedBuiltin reports whether the lines call a static of __Builtins that the built-in table does not declare.
func callsUnprovidedBuiltin(lines []sourceLine) bool {
	for _, l := range lines {
		if l.removed {
			continue
		}
		for k := 0; k+2 < len(l.code); k++ {
			if l.code[k].kind == tokIdent && l.code[k].text == "__Builtins" && l.code[k+1].text == "." && l.code[k+2].kind == tokIdent && !provides(builtinMethods["Builtins"], l.code[k+2].text) {
				return true
			}
		}
	}
	return false
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
