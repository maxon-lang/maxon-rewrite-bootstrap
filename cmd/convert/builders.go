package main

import "strings"

// minimaxon has no StringBuilder: a String is built by an interpolation assigned back to it, which appends in place
// (README, "Where it differs from Maxon"). rewriteStringBuilders writes a builder that only gathers text as that String:
// a local made by `StringBuilder.create()` whose every later use in its scope is a statement `b.append(x)` or the one
// `b.build()`, which is its last, becomes a String local that each append extends, `b = "{b}x"`, and the build becomes
// the local itself. A builder used any other way is left as it is.
func rewriteStringBuilders(lines []sourceLine) {
	for i := range lines {
		if lines[i].removed {
			continue
		}
		name, ok := builderDeclName(lines[i].code)
		if !ok {
			continue
		}
		appends, build, ok := builderUses(lines, i, name)
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
func builderUses(lines []sourceLine, decl int, name string) ([]int, tokenAt, bool) {
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
