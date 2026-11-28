package main

// rewriteLibraryAliasChecks makes each identity comparison of two bare names in a library file, `a is b`, the constant
// `false`, and `a is not b` the constant `true`. maxon's library writes one to find an argument that is also the array a
// call writes (AEAD's `aeadSharesOutput`), so as to copy it first; a value has one owner here and a call cannot borrow an
// array it writes as another argument, so the names never stand for one object and the copy is never needed.
func rewriteLibraryAliasChecks(lines []sourceLine) {
	for i := range lines {
		if lines[i].removed {
			continue
		}
		code := lines[i].code
		if len(code) == 0 || code[0].text[0] == '#' {
			continue
		}
		var out []token
		for k := 0; k < len(code); k++ {
			if constant, width := aliasCheckAt(code, k); width > 0 {
				out = append(out, token{kind: tokIdent, text: constant})
				k += width - 1
				continue
			}
			out = append(out, code[k])
		}
		lines[i].code = out
	}
}

// aliasCheckAt is the constant that the identity comparison of two bare names starting at code[k] becomes and how many tokens
// it spans, or a width of 0 when none starts there.
func aliasCheckAt(code []token, k int) (string, int) {
	if k+2 >= len(code) || !isBareName(code, k) || code[k+1].kind != tokIdent || code[k+1].text != "is" || isHeaderIs(code[:k+1]) {
		return "", 0
	}
	if code[k+2].text == "not" {
		if k+3 < len(code) && isBareName(code, k+3) {
			return "true", 4
		}
		return "", 0
	}
	if isBareName(code, k+2) {
		return "false", 3
	}
	return "", 0
}

// isBareName is whether code[k] is a name that is neither a member, a call nor a qualified name's first part.
func isBareName(code []token, k int) bool {
	if code[k].kind != tokIdent || reservedWords[code[k].text] {
		return false
	}
	if k > 0 && code[k-1].text == "." {
		return false
	}
	return k+1 >= len(code) || code[k+1].text != "." && code[k+1].text != "("
}
