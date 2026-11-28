package main

import "slices"

// maxon has only `type`: its `spawn` starts an instance of any type. minimaxon's `spawn` starts a `service` alone
// (README, "Services"), so a type that a program spawns is declared with `service` in the converted text.

const (
	spawnKeyword   = "spawn"
	typeKeyword    = "type"
	serviceKeyword = "service"
)

// spawnedTypesOf are the types that a source file spawns, as spawnedTypesIn reads them.
func spawnedTypesOf(src string) []string {
	text := joinBracketLines(convertBlockComments(normalizeText(src)))
	return spawnedTypesIn(splitLines(text))
}

// spawnedTypesIn are the types that the lines spawn: `spawn T.f(...)`, `spawn(capacity: n) T.f(...)` and `spawn module.T.f(...)`
// each name the type T that owns the factory f. A type is named by its name alone, so another type of that name that a
// program of the run declares in another module is written as a service too.
func spawnedTypesIn(lines []sourceLine) []string {
	var spawned []string
	for _, l := range lines {
		if l.removed {
			continue
		}
		for i, t := range l.code {
			if t.kind != tokIdent || t.text != spawnKeyword || (i > 0 && l.code[i-1].text == ".") {
				continue
			}
			if name := spawnedTypeAfter(l.code, i+1); name != "" && !slices.Contains(spawned, name) {
				spawned = append(spawned, name)
			}
		}
	}
	return spawned
}

// spawnedTypeAfter is the type that the spawn whose target starts at from names, or "" for a target that is no factory.
func spawnedTypeAfter(toks []token, from int) string {
	if from < len(toks) && toks[from].text == "(" {
		closing := findMatchingParen(toks, from)
		if closing < 0 {
			return ""
		}
		from = closing + 1
	}
	var chain []string
	for i := from; i < len(toks); i += 2 {
		if toks[i].kind != tokIdent {
			break
		}
		chain = append(chain, toks[i].text)
		if i+1 >= len(toks) || toks[i+1].text != "." {
			break
		}
	}
	if len(chain) < 2 {
		return ""
	}
	return chain[len(chain)-2]
}

// writeSpawnedTypesAsServices writes `service` for the `type` of each declaration of a type in spawned that has no type
// parameters, which a service has none of yet (README, "Services"). It runs after the conversion's other rules, which read the
// declaration as a `type`.
func writeSpawnedTypesAsServices(lines []sourceLine, spawned []string) {
	for i := range lines {
		l := &lines[i]
		at := skipModifiers(l.code)
		if l.removed || at+1 >= len(l.code) || l.code[at].kind != tokIdent || l.code[at].text != typeKeyword {
			continue
		}
		if slices.Contains(spawned, l.code[at+1].text) && !hasUsesClause(l.code[at:]) {
			l.code[at].text = serviceKeyword
		}
	}
}
