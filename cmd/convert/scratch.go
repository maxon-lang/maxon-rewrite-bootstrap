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

// minimaxon's library may not grow an array of a type parameter with resize either where maxon's code resizes an array
// and then fills every slot from 0 in a loop: `a.resize(n)`, `var i = 0`, `while i < n` whose body stores
// `try a.set(i, value: v) otherwise ...` and steps `i = i + 1`. rewriteFilledPrefix says the same with the count: the
// array is emptied and given the room (`a.truncate(0)`, `a.reserve(n)`), and each store is the push of v, which lands at
// i; the store's handler goes, since a push does not fail.
func rewriteFilledPrefix(lines []sourceLine) []sourceLine {
	var out []sourceLine
	for i := range lines {
		name, ok := scratchResizeName(lines[i].code)
		if !ok || !fillsPrefix(lines, i, name) {
			out = append(out, lines[i])
			continue
		}
		size := lines[i].code[4 : len(lines[i].code)-1]
		out = append(out,
			sourceLine{indent: lines[i].indent, code: append(callTokens(name, "truncate"), token{kind: tokInt, text: "0"}, token{kind: tokPunct, text: ")"}), origLine: lines[i].origLine},
			sourceLine{indent: lines[i].indent, code: append(callTokens(name, "reserve"), append(append([]token{}, size...), token{kind: tokPunct, text: ")"})...), origLine: lines[i].origLine})
	}
	return out
}

// fillsPrefix is whether the resize of name at line at is followed by a counter from 0 and a loop over it up to the
// resize's size that stores to name at the counter alone, and steps the counter by one, and rewrites those stores to
// pushes when it is.
func fillsPrefix(lines []sourceLine, at int, name string) bool {
	size := lines[at].code[4 : len(lines[at].code)-1]
	decl := nextCodeLine(lines, at+1)
	if decl < 0 || len(lines[decl].code) < 4 || lines[decl].code[0].text != "var" || lines[decl].code[2].text != "=" || lines[decl].code[3].text != "0" || lines[decl].indent != lines[at].indent {
		return false
	}
	counter := lines[decl].code[1].text
	loop := nextCodeLine(lines, decl+1)
	if loop < 0 || lines[loop].indent != lines[at].indent || !isLoopUpTo(lines[loop].code, counter, size) {
		return false
	}
	end := blockEnd(lines, loop, lines[loop].indent)
	body := lines[loop].indent + "\t"
	stepped := false
	var stores []int
	for j := loop + 1; j < end; j++ {
		code := lines[j].code
		if lines[j].removed || len(code) == 0 {
			continue
		}
		if lines[j].indent == body && isStepOf(code, counter) {
			stepped = true
		}
		for k := range code {
			if code[k].text == name && k+2 < len(code) && code[k+1].text == "." && code[k+2].text == "set" {
				if k != 1 || code[0].text != "try" || len(code) < 7 || code[5].text != counter || code[6].text != "," {
					return false
				}
				stores = append(stores, j)
			}
		}
	}
	if !stepped || len(stores) == 0 {
		return false
	}
	for _, j := range stores {
		if !storeAsPush(lines, j, name, false) {
			return false
		}
	}
	for _, j := range stores {
		storeAsPush(lines, j, name, true)
	}
	return true
}

// isLoopUpTo is whether code is `while counter < size 'label'`.
func isLoopUpTo(code []token, counter string, size []token) bool {
	if len(code) != 4+len(size) || code[0].text != "while" || code[1].text != counter || code[2].text != "<" || code[len(code)-1].kind != tokLabel {
		return false
	}
	for k, t := range size {
		if code[3+k].text != t.text {
			return false
		}
	}
	return true
}

// isStepOf is whether code is `counter = counter + 1`.
func isStepOf(code []token, counter string) bool {
	return len(code) == 5 && code[0].text == counter && code[1].text == "=" && code[2].text == counter && code[3].text == "+" && code[4].text == "1"
}

// storeAsPush rewrites the store `try name.set(i, value: v) otherwise ...` at line j to `name.push(v)`, leaving out its
// handler, a panic on the line or a labelled block of lines after it, where write is set, and says whether it is such a
// store.
func storeAsPush(lines []sourceLine, j int, name string, write bool) bool {
	code := lines[j].code
	closing := findMatchingParen(code, 4)
	label := findToken(code, 5, "value")
	if closing < 0 || label < 0 || label+2 >= closing || code[label+1].text != ":" || closing+1 >= len(code) || code[closing+1].text != "otherwise" {
		return false
	}
	tail := code[closing+2:]
	push := append(callTokens(name, "push"), append(append([]token{}, code[label+2:closing]...), token{kind: tokPunct, text: ")"})...)
	switch {
	case len(tail) == 4 && tail[0].text == "panic" && tail[1].text == "(" && tail[2].kind == tokString && tail[3].text == ")":
	case len(tail) == 1 && tail[0].kind == tokLabel:
		if write {
			end := blockEnd(lines, j, lines[j].indent)
			for k := j + 1; k <= end; k++ {
				lines[k].removed = true
			}
		}
	default:
		return false
	}
	if write {
		lines[j].code = push
	}
	return true
}
