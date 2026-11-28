package main

import (
	"fmt"
	"slices"
	"strings"
)

// registryCaches are the types of maxon's library that keep values in a program-wide registry (`default T = ...`, read with
// `T.current()`), which minimaxon has no counterpart of (README, "Where it differs from Maxon"), and the methods that return
// a cached value or build it with their `build` closure: `helpers/crypto/CombCache.maxon`'s, which hold the curves' fixed-base
// tables and moduli, derived from public constants only. Here each such value is a constant of the library that its
// regeneration computes once with maxon's build code (cmd/gate, combtables.go), so the file keeps only what the rest of the
// library names of it (CombCurve, CombModuli).
var registryCaches = map[string][]string{"CombCache": {"table", "moduli"}}

// registryCacheFunction is the function of the library that gives the constant a call of method of the registry type
// cache gives: `CombCache.table` is `combTable`.
func registryCacheFunction(cache, method string) string {
	prefix := strings.TrimSuffix(cache, "Cache")
	return strings.ToLower(prefix[:1]) + prefix[1:] + strings.ToUpper(method[:1]) + method[1:]
}

// rewriteRegistryCaches writes each call `Cache.method(key, build: function() gives value)` of registryCaches as
// `cacheMethod(key)`, the generated constant of that key.
func rewriteRegistryCaches(lines []sourceLine) {
	for i := range lines {
		if lines[i].removed {
			continue
		}
		code := lines[i].code
		for k := 0; k+3 < len(code); k++ {
			methods, ok := registryCaches[code[k].text]
			if !ok || code[k].kind != tokIdent || code[k+1].text != "." || !slices.Contains(methods, code[k+2].text) || code[k+3].text != "(" {
				continue
			}
			closeAt := findMatchingParen(code, k+3)
			if closeAt < 0 {
				panic(fmt.Sprintf("line %d: the call of %s.%s has no )", lines[i].origLine, code[k].text, code[k+2].text))
			}
			key := registryCacheKey(code[k+4 : closeAt])
			if key == nil {
				panic(fmt.Sprintf("line %d: the call of %s.%s has no `build: function() gives` argument after its key", lines[i].origLine, code[k].text, code[k+2].text))
			}
			out := append([]token{}, code[:k]...)
			out = append(out, token{kind: tokIdent, text: registryCacheFunction(code[k].text, code[k+2].text)}, token{kind: tokPunct, text: "("})
			out = append(out, key...)
			out = append(out, token{kind: tokPunct, text: ")"})
			out = append(out, code[closeAt+1:]...)
			code = out
		}
		lines[i].code = code
	}
}

// registryCacheKey is the key argument before `, build: function() gives <value>` that ends args, or nil.
func registryCacheKey(args []token) []token {
	for k := 1; k+6 < len(args); k++ {
		if args[k].text == "," && args[k+1].text == "build" && args[k+2].text == ":" && args[k+3].text == "function" && args[k+4].text == "(" && args[k+5].text == ")" && args[k+6].text == "gives" {
			return args[:k]
		}
	}
	return nil
}

// dropRegistryCaches leaves out of a file that declares a type of registryCaches that type and its registry declaration
// (`default T = ...`), and then what only they reached.
func dropRegistryCaches(lines []sourceLine) {
	named := map[string]bool{}
	for i := range lines {
		l := lines[i]
		if l.removed || l.indent != "" || len(l.code) < 2 {
			continue
		}
		if _, ok := registryCaches[l.code[1].text]; ok && l.code[0].text == "default" {
			dropLines(lines, commentsAbove(lines, i), i, named)
			continue
		}
		at := skipModifiers(l.code)
		if _, ok := registryCaches[declaredName(l.code)]; !ok || l.code[at].text != "type" {
			continue
		}
		end, ok := declarationEnd(lines, i)
		if !ok {
			panic(fmt.Sprintf("line %d: the type %s has no end", l.origLine, declaredName(l.code)))
		}
		dropLines(lines, commentsAbove(lines, i), end, named)
	}
	dropReachedOnlyByDropped(lines, named, map[string]bool{}, true)
}
