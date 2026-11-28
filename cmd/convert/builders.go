package main

import "strings"

// minimaxon has no StringBuilder: a String is built by an interpolation assigned back to it, which appends in place
// (README, "Where it differs from Maxon"). rewriteStringBuilders writes a builder that only gathers text as that String:
// a local made by `StringBuilder.create()` whose every later use in its scope is a statement `b.append(x)` or the one
// `b.build()`, which is its last, becomes a String local that each append extends, `b = "{b}x"`, and the build becomes
// the local itself. A builder may also be passed to a function of the file whose parameter is a builder that it only appends
// to or passes on so (builderParams), which takes a String there, extended the same way, and the fix makes it `mutates`. A
// builder used any other way is left as it is.
func rewriteStringBuilders(lines []sourceLine) {
	takers := builderParams(lines)
	for i := range lines {
		if lines[i].removed {
			continue
		}
		fn, param, ok := builderParamOf(lines, i)
		if !ok || !takers[fn] {
			continue
		}
		for k := range lines[i].code {
			if lines[i].code[k].text == param && k+1 < len(lines[i].code) && lines[i].code[k+1].text == "StringBuilder" {
				lines[i].code[k+1].text = "String"
			}
		}
		end := memberEnd(lines, i)
		for j := i + 1; j < end; j++ {
			if !lines[j].removed && len(lines[j].code) > 0 && lines[j].code[0].text == param && isAppendStatement(lines[j].code) {
				lines[j].code = appendAsInterpolation(lines[j].code, param)
			}
		}
	}
	for i := range lines {
		if lines[i].removed {
			continue
		}
		name, ok := builderDeclName(lines[i].code)
		if !ok {
			continue
		}
		appends, build, ok := builderUses(lines, i, name, takers)
		if !ok {
			continue
		}
		keyword := "let"
		if len(appends) > 0 {
			keyword = "var"
		}
		lines[i].code = []token{
			{kind: tokIdent, text: keyword},
			{kind: tokIdent, text: name},
			{kind: tokPunct, text: "="},
			{kind: tokString, text: `""`},
		}
		for _, at := range appends {
			lines[at].code = appendAsInterpolation(lines[at].code, name)
		}
		lines[build.line].code = builtAsLocal(lines[build.line].code, build.token)
	}
}

// builderCreate is the call that makes a builder: `StringBuilder.create()`.
var builderCreate = []string{"StringBuilder", ".", "create", "(", ")"}

// builderDeclName is the name a line `let b = StringBuilder.create()` or `var b = ...` declares.
func builderDeclName(code []token) (string, bool) {
	if len(code) != 3+len(builderCreate) || (code[0].text != "let" && code[0].text != "var") || code[1].kind != tokIdent || code[2].text != "=" {
		return "", false
	}
	for k, word := range builderCreate {
		if code[3+k].text != word {
			return "", false
		}
	}
	return code[1].text, true
}

// tokenAt is a token of a line.
type tokenAt struct {
	line, token int
}

// builderUses finds the lines after the declaration at decl, in its scope, that append to the builder name and the one
// that builds it, and whether those are its only uses with the build last.
func builderUses(lines []sourceLine, decl int, name string, takers map[string]bool) ([]int, tokenAt, bool) {
	var appends []int
	build := tokenAt{line: -1}
	for j := decl + 1; j < len(lines); j++ {
		if lines[j].removed || len(lines[j].code) == 0 {
			continue
		}
		if len(lines[j].indent) < len(lines[decl].indent) {
			break
		}
		code := lines[j].code
		for k := range code {
			if code[k].kind != tokIdent || code[k].text != name || (k > 0 && code[k-1].text == ".") {
				continue
			}
			if build.line >= 0 {
				return nil, build, false
			}
			switch {
			case k == 0 && isAppendStatement(code):
				appends = append(appends, j)
			case isBuildCall(code[k:]):
				build = tokenAt{line: j, token: k}
			case passedToTaker(code, k, takers):
			default:
				return nil, build, false
			}
		}
	}
	return appends, build, build.line >= 0
}

// isAppendStatement is whether a line is `b.append(x)` and nothing more.
func isAppendStatement(code []token) bool {
	return len(code) > 4 && code[1].text == "." && code[2].text == "append" && code[3].text == "(" && findMatchingParen(code, 3) == len(code)-1
}

// isBuildCall is whether the tokens start with `b.build()`.
func isBuildCall(code []token) bool {
	return len(code) >= 5 && code[1].text == "." && code[2].text == "build" && code[3].text == "(" && code[4].text == ")"
}

// appendAsInterpolation writes `b.append(x)` as `b = "{b}x"`: a plain literal's text goes in as it is written, and any
// other argument as an interpolated part.
func appendAsInterpolation(code []token, name string) []token {
	arg := code[4 : len(code)-1]
	part := "{" + renderCodeTokens(arg) + "}"
	if len(arg) == 1 && arg[0].kind == tokString && len(arg[0].text) >= 2 && strings.HasPrefix(arg[0].text, `"`) && !strings.HasPrefix(arg[0].text, `"""`) {
		part = arg[0].text[1 : len(arg[0].text)-1]
	}
	return []token{
		{kind: tokIdent, text: name},
		{kind: tokPunct, text: "="},
		{kind: tokString, text: `"{` + name + `}` + part + `"`},
	}
}

// builtAsLocal replaces the call `b.build()` at index at with the String local b.
func builtAsLocal(code []token, at int) []token {
	out := append([]token{}, code[:at+1]...)
	return append(out, code[at+5:]...)
}

// builderParamOf is the function that line i opens at the top level and its parameter that is a StringBuilder, if it has one.
func builderParamOf(lines []sourceLine, i int) (fn, param string, ok bool) {
	l := lines[i]
	if l.indent != "" || len(l.code) == 0 {
		return "", "", false
	}
	declIdx := skipModifiers(l.code)
	if declIdx+2 >= len(l.code) || l.code[declIdx].text != "function" || l.code[declIdx+2].text != "(" {
		return "", "", false
	}
	close := findMatchingParen(l.code, declIdx+2)
	for k := declIdx + 3; k+1 < close; k++ {
		if l.code[k].kind == tokIdent && l.code[k+1].text == "StringBuilder" {
			return l.code[declIdx+1].text, l.code[k].text, true
		}
	}
	return "", "", false
}

// builderParams are the top-level functions of the file whose StringBuilder parameter is only appended to, by a statement
// `b.append(x)`, or passed on to another such function.
func builderParams(lines []sourceLine) map[string]bool {
	params := map[string]string{}
	spans := map[string][2]int{}
	for i := range lines {
		if lines[i].removed {
			continue
		}
		if fn, param, ok := builderParamOf(lines, i); ok {
			params[fn] = param
			spans[fn] = [2]int{i, memberEnd(lines, i)}
		}
	}
	takers := map[string]bool{}
	for fn := range params {
		takers[fn] = true
	}
	for changed := true; changed; {
		changed = false
		for fn, param := range params {
			if !takers[fn] {
				continue
			}
			for j := spans[fn][0] + 1; j < spans[fn][1] && takers[fn]; j++ {
				code := lines[j].code
				if lines[j].removed {
					continue
				}
				for k := range code {
					if code[k].kind != tokIdent || code[k].text != param || (k > 0 && code[k-1].text == ".") {
						continue
					}
					if !(k == 0 && isAppendStatement(code)) && !passedToTaker(code, k, takers) {
						takers[fn] = false
						changed = true
						break
					}
				}
			}
		}
	}
	return takers
}

// passedToTaker is whether the name at k is an argument, alone, of a call of a function of takers.
func passedToTaker(code []token, k int, takers map[string]bool) bool {
	if k == 0 || k+1 >= len(code) || (code[k+1].text != "," && code[k+1].text != ")") {
		return false
	}
	if code[k-1].text != "(" && code[k-1].text != "," && code[k-1].text != ":" {
		return false
	}
	depth := 0
	for j := k - 1; j > 0; j-- {
		switch code[j].text {
		case ")":
			depth++
		case "(":
			if depth == 0 {
				return takers[code[j-1].text]
			}
			depth--
		}
	}
	return false
}
