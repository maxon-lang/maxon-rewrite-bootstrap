package main

// minimaxon's library may not grow an array of a type parameter (resize would publish slots that hold no element), and
// maxon's driftsort grows its merge scratch so, then writes it as a prefix from slot 0 before it reads it: every writer
// (mergeBufferedLeft and mergeBufferedRight, stablePartition) stores at 0, 1, 2 and so on, and reads only what it stored.
// rewriteMergeScratch says the same with the scratch's count: the resize that makes the scratch becomes a reserve of its
// room, and a function that stores to a scratch it is given, `try s.set(i, value: v) otherwise panic(...)`, starts by
// emptying it, and each store becomes the push of v, which lands at the same index.
func rewriteMergeScratch(lines []sourceLine) []sourceLine {
	made := map[string]bool{}
	for i := range lines {
		if name, ok := scratchDeclName(lines[i].code); ok {
			made[name] = true
		}
		if name, ok := scratchResizeName(lines[i].code); ok && made[name] {
			lines[i].code[2].text = "reserve"
		}
	}
	var out []sourceLine
	for i := range lines {
		out = append(out, lines[i])
		names := scratchParamNames(lines[i].code)
		if len(names) == 0 {
			continue
		}
		end := blockEnd(lines, i, lines[i].indent)
		body := nextCodeLine(lines, i+1)
		for _, name := range names {
			stored := false
			for j := i + 1; j < end; j++ {
				if value, ok := scratchStoreValue(lines[j].code, name); ok {
					lines[j].code = append(callTokens(name, "push"), append(value, token{kind: tokPunct, text: ")"})...)
					stored = true
				}
			}
			if stored {
				code := append(callTokens(name, "truncate"), token{kind: tokInt, text: "0"}, token{kind: tokPunct, text: ")"})
				out = append(out, sourceLine{indent: lines[body].indent, code: code, origLine: lines[i].origLine})
			}
		}
	}
	return out
}

// mergeScratchType is the typealias of maxon's merge scratch, `Array with Element` (helpers/sort/mergeSort.maxon).
const mergeScratchType = "MergeScratchArray"

// callTokens is `receiver.method(`, the start of a call.
func callTokens(receiver, method string) []token {
	return []token{{kind: tokIdent, text: receiver}, {kind: tokPunct, text: "."}, {kind: tokIdent, text: method}, {kind: tokPunct, text: "("}}
}

// scratchDeclName is the name a line `let s = MergeScratchArray.create(0)`, or `var s = ...`, declares.
func scratchDeclName(code []token) (string, bool) {
	want := []string{"", "", "=", mergeScratchType, ".", "create", "(", "0", ")"}
	if len(code) != len(want) || (code[0].text != "let" && code[0].text != "var") || code[1].kind != tokIdent {
		return "", false
	}
	for k, word := range want {
		if word != "" && code[k].text != word {
			return "", false
		}
	}
	return code[1].text, true
}

// scratchResizeName is the receiver of a statement `s.resize(n)`.
func scratchResizeName(code []token) (string, bool) {
	if len(code) < 5 || code[0].kind != tokIdent || code[1].text != "." || code[2].text != "resize" || code[3].text != "(" || findMatchingParen(code, 3) != len(code)-1 {
		return "", false
	}
	return code[0].text, true
}

// scratchParamNames are the parameters of type MergeScratchArray of a function header.
func scratchParamNames(code []token) []string {
	fn := funcKeywordIndex(code)
	if fn < 0 {
		return nil
	}
	open := findToken(code, fn, "(")
	if open < 0 {
		return nil
	}
	var names []string
	for k := open + 2; k < findMatchingParen(code, open); k++ {
		if code[k].text == mergeScratchType && code[k-1].kind == tokIdent && (code[k-2].text == "(" || code[k-2].text == "," || code[k-2].text == "mutates") {
			names = append(names, code[k-1].text)
		}
	}
	return names
}

// scratchStoreValue is v of a statement `try s.set(i, value: v) otherwise panic(message)`.
func scratchStoreValue(code []token, name string) ([]token, bool) {
	head := []string{"try", name, ".", "set", "("}
	if len(code) < len(head) {
		return nil, false
	}
	for k, word := range head {
		if code[k].text != word {
			return nil, false
		}
	}
	closing := findMatchingParen(code, len(head)-1)
	if closing < 0 {
		return nil, false
	}
	label := findToken(code, len(head), "value")
	if label < 0 || label+2 >= closing || code[label-1].text != "," || code[label+1].text != ":" {
		return nil, false
	}
	tail := code[closing+1:]
	if len(tail) != 5 || tail[0].text != "otherwise" || tail[1].text != "panic" || tail[2].text != "(" || tail[3].kind != tokString || tail[4].text != ")" {
		return nil, false
	}
	return code[label+2 : closing], true
}
