package main

import (
	"slices"
	"strconv"
	"strings"
)

// The rules of a spec program's sockets. maxon closes a TcpClient or a TcpListener when the value dies, and its close throws
// nothing; here a socket is a linear value whose close throws NetworkError (stdlib/net, README "Where it differs from
// Maxon"), so the program is written as maxon's runs it: a socket the program lets die is closed where it dies, a socket a
// discard drops is closed at once, and a close is a try that ignores its failure, as maxon's close has none. A return that
// leaves a socket open is ended by the compiler's ending fix, except in the body of an `if let`, which it does not reach and
// which this conversion closes before each return. A socket that the scope moves on (returned, put in a record, adopted or
// consumed) is left to its new owner.

// socketClose is the close of a socket whose failure is ignored, as maxon's close throws nothing.
func socketClose(name string) string { return "try " + name + ".close() otherwise ignore" }

// discardedSocket is the name a socket that maxon's program discards is given here, so that it is closed.
const discardedSocket = "discardedSocket"

// socketResult is the name a return's value is given where it reads a socket that is closed before the return.
const socketResult = "socketResult"

// socketTypes are the library's socket types, which a parameter of the program may be declared as.
var socketTypes = []string{"TcpClient", "TcpListener", "__ManagedSocket"}

// opensSocket is whether code makes a socket: a connect, a bind, an accept or an adoption.
func opensSocket(code []token) bool {
	for k := 0; k+2 < len(code); k++ {
		owner, dot, member := code[k].text, code[k+1].text, code[k+2].text
		if dot != "." {
			continue
		}
		switch {
		case owner == "TcpClient" && (member == "connect" || member == "adopt"),
			owner == "TcpListener" && member == "bind",
			owner == "__ManagedSocket" && (member == "tcpConnect" || member == "bindListener"),
			member == "accept" && k+3 < len(code) && code[k+3].text == "(":
			return true
		}
	}
	return false
}

// codeTexts is a line's tokens' texts.
func codeTexts(code []token) []string {
	texts := make([]string, len(code))
	for k, t := range code {
		texts[k] = t.text
	}
	return texts
}

// indentDepth is how many tabs a line is indented by.
func indentDepth(l sourceLine) int { return strings.Count(l.indent, "\t") }

// exits is whether a line leaves its block: a return, a throw, a break, a continue or a panic.
func exits(l sourceLine) bool {
	return len(l.code) > 0 && slices.Contains([]string{"return", "throw", "break", "continue", "panic"}, l.code[0].text)
}

// isCode is whether a line holds code.
func isCode(l sourceLine) bool { return !l.removed && len(l.code) > 0 }

// mentions is whether code reads a name: as a token, or in an interpolation of a string literal.
func mentions(code []token, name string) bool {
	for _, t := range code {
		if t.kind == tokIdent && t.text == name {
			return true
		}
		if t.kind == tokString && interpolates(t.text, name) {
			return true
		}
	}
	return false
}

// interpolates is whether a string literal reads a name in one of its interpolations, `{name}`, `{name.f()}` or `{await name}`.
func interpolates(text, name string) bool {
	for _, part := range strings.Split(text, "{")[1:] {
		inner, _, _ := strings.Cut(part, "}")
		if slices.Contains(strings.FieldsFunc(inner, func(r rune) bool {
			return !(r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
		}), name) {
			return true
		}
	}
	return false
}

// closes is whether a line closes a socket of the name.
func closes(l sourceLine, name string) bool {
	texts := codeTexts(l.code)
	at := slices.Index(texts, name)
	return at >= 0 && at+2 < len(texts) && texts[at+1] == "." && texts[at+2] == "close"
}

// movesOn is whether a line gives the socket of the name to another owner: returns it, puts it in a record, adopts it or
// passes it consumes.
func movesOn(l sourceLine, name string) bool {
	texts := codeTexts(l.code)
	for k, t := range texts {
		if t != name || k+1 < len(texts) && texts[k+1] == "." {
			continue
		}
		if k > 0 && (texts[k-1] == "return" || texts[k-1] == ":" || texts[k-1] == "consumes") {
			return true
		}
		if slices.Contains(texts, "adopt") || slices.Contains(texts, "{") {
			return true
		}
	}
	return false
}

// written is the lines of a text, each counted at a line of the source.
func written(text string, origLine int) []sourceLine {
	lines := splitLines(text + "\n")
	for k := range lines {
		lines[k].origLine = origLine
	}
	return lines
}

// socketDeclaration is the name a line binds a new socket to, `let NAME = try TcpClient.connect(...)` or `if let NAME = ...`,
// and whether it is an `if let`, or "".
func socketDeclaration(l sourceLine) (string, bool) {
	c := l.code
	if len(c) >= 4 && (c[0].text == "let" || c[0].text == "var") && c[1].kind == tokIdent && c[2].text == "=" && opensSocket(c) {
		return c[1].text, false
	}
	if len(c) >= 5 && c[0].text == "if" && c[1].text == "let" && c[2].kind == tokIdent && c[3].text == "=" && opensSocket(c) {
		return c[2].text, true
	}
	return "", false
}

// socketParameters are the names of the parameters a function line declares of a socket type.
func socketParameters(l sourceLine) []string {
	if funcKeywordIndex(l.code) < 0 {
		return nil
	}
	var names []string
	texts := codeTexts(l.code)
	for k := 1; k < len(texts); k++ {
		if slices.Contains(socketTypes, texts[k]) && l.code[k-1].kind == tokIdent && (texts[k-2] == "(" || texts[k-2] == "," || texts[k-2] == "mutates" || texts[k-2] == "consumes") {
			names = append(names, texts[k-1])
		}
	}
	return names
}

// rewriteSocketEndings writes a spec program's socket closes (see above), in a program that names a socket type.
// expectsError is whether the test expects a compile error: the compiler's fixes stop once the expected error is the first,
// so the sockets' other endings are this conversion's to write, and a socket is ended at each exit (endAtExits).
func rewriteSocketEndings(lines []sourceLine, expectsError bool) []sourceLine {
	if !slices.ContainsFunc(lines, func(l sourceLine) bool {
		return isCode(l) && slices.ContainsFunc(l.code, func(t token) bool { return t.kind == tokIdent && slices.Contains(socketTypes, t.text) })
	}) {
		return lines
	}
	lines = discardSockets(lines)
	lines = hoistInlineExits(lines)
	var names []string
	for _, l := range lines {
		if !isCode(l) {
			continue
		}
		if name, _ := socketDeclaration(l); name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
		for _, p := range socketParameters(l) {
			if !slices.Contains(names, p) {
				names = append(names, p)
			}
		}
	}
	tryBareCloses(lines, names)
	lines = mutateSocketParameters(lines)
	lines = cancelBeforeLentExits(lines, names)
	if expectsError {
		lines = endAtExits(lines)
	}
	return closeAtScopeEnds(lines)
}

// socketParameter is a parameter of a function of the program, and whether it is passed consumes or mutates.
type socketParameter struct {
	name     string
	consumes bool
	mutates  bool
}

// programParameters are the parameters of each function the program declares, by the function's name, in order.
func programParameters(lines []sourceLine) map[string][]socketParameter {
	params := map[string][]socketParameter{}
	for _, l := range lines {
		fn := funcKeywordIndex(l.code)
		if !isCode(l) || fn < 0 || fn+2 >= len(l.code) || l.code[fn+2].text != "(" {
			continue
		}
		closeAt := findMatchingParen(l.code, fn+2)
		if closeAt < 0 {
			continue
		}
		var list []socketParameter
		part := []string{}
		flush := func() {
			if len(part) >= 2 {
				at := 0
				if part[0] == "consumes" || part[0] == "mutates" {
					at = 1
				}
				list = append(list, socketParameter{name: part[at], consumes: part[0] == "consumes", mutates: part[0] == "mutates"})
			}
			part = part[:0]
		}
		depth := 0
		for _, t := range l.code[fn+3 : closeAt] {
			switch {
			case t.text == "(":
				depth++
			case t.text == ")":
				depth--
			case t.text == "," && depth == 0:
				flush()
				continue
			}
			part = append(part, t.text)
		}
		flush()
		params[l.code[fn+1].text] = list
	}
	return params
}

// passedAt is how the token at k of a line passes its name to a call: the called function's parameter, and whether the
// token is a whole argument of a call of the program's.
func passedAt(texts []string, k int, params map[string][]socketParameter) (socketParameter, bool) {
	if k+1 >= len(texts) || texts[k+1] != ")" && texts[k+1] != "," {
		return socketParameter{}, false
	}
	label := ""
	if k >= 2 && texts[k-1] == ":" {
		label = texts[k-2]
	} else if k < 1 || texts[k-1] != "(" && texts[k-1] != "," {
		return socketParameter{}, false
	}
	position, depth := 0, 0
	for j := k - 1; j >= 1; j-- {
		switch texts[j] {
		case ")":
			depth++
		case ",":
			if depth == 0 {
				position++
			}
		case "(":
			if depth > 0 {
				depth--
				continue
			}
			list, ok := params[texts[j-1]]
			if !ok {
				return socketParameter{}, false
			}
			for n, p := range list {
				if label == "" && n == position || label != "" && p.name == label {
					return p, true
				}
			}
			return socketParameter{}, false
		}
	}
	return socketParameter{}, false
}

// endAtExits ends each socket that a `let` or a `var` binds at each exit of its scope that leaves it open, as the compiler's
// ending fix would: a close before a return (whose value, where it reads the socket, is taken into socketResult first), and
// a try's `otherwise return v` made a block that closes it and returns. A socket passed to a consumes parameter of a function
// of the program is moved on there, and one passed to a mutates parameter is bound by a var. The block of the binding's own
// try is not in its scope, as the socket is not bound there.
func endAtExits(lines []sourceLine) []sourceLine {
	params := programParameters(lines)
	before := map[int][]string{}
	blocks := map[int][]string{}
	results := map[int][]string{}
	for i, l := range lines {
		if !isCode(l) {
			continue
		}
		name, ifLet := socketDeclaration(l)
		if name == "" || ifLet {
			continue
		}
		depth := indentDepth(l)
		from := i + 1
		if last := l.code[len(l.code)-1]; len(l.code) >= 2 && l.code[len(l.code)-2].text == "otherwise" && last.kind == tokLabel {
			for from < len(lines) && !(isCode(lines[from]) && indentDepth(lines[from]) == depth && lines[from].code[0].text == "end") {
				from++
			}
			from++
		}
		for k := from; k < len(lines) && (!isCode(lines[k]) || indentDepth(lines[k]) >= depth); k++ {
			r := lines[k]
			if !isCode(r) {
				continue
			}
			texts := codeTexts(r.code)
			moved := closes(r, name) || movesOn(r, name)
			for j, t := range texts {
				if t != name {
					continue
				}
				if p, ok := passedAt(texts, j, params); ok {
					if p.consumes {
						moved = true
					} else if p.mutates && lines[i].code[0].text == "let" {
						lines[i].code[0].text = "var"
					}
				}
			}
			if moved {
				break
			}
			if texts[0] == "return" {
				if mentions(r.code[1:], name) {
					results[k] = append(results[k], name)
				} else {
					before[k] = append(before[k], name)
				}
				continue
			}
			if other := slices.Index(texts, "otherwise"); other >= 0 && other+1 < len(texts) && texts[other+1] == "return" {
				blocks[k] = append(blocks[k], name)
			}
		}
	}
	var out []sourceLine
	serial := 0
	for k, l := range lines {
		for _, name := range before[k] {
			out = append(out, written(l.indent+socketClose(name), l.origLine)...)
		}
		if names, ok := results[k]; ok {
			out = append(out, written(l.indent+"let "+socketResult+" = "+renderCode(l.code[1:]), l.origLine)...)
			for _, name := range names {
				out = append(out, written(l.indent+socketClose(name), l.origLine)...)
			}
			out = append(out, written(l.indent+"return "+socketResult, l.origLine)...)
			continue
		}
		names, ok := blocks[k]
		if !ok {
			out = append(out, l)
			continue
		}
		texts := codeTexts(l.code)
		other := slices.Index(texts, "otherwise")
		serial++
		label := "'" + closedExit + strconv.Itoa(serial) + "'"
		out = append(out, written(l.indent+renderCode(l.code[:other])+" otherwise "+label, l.origLine)...)
		for _, name := range names {
			out = append(out, written(l.indent+"\t"+socketClose(name), l.origLine)...)
		}
		out = append(out, written(l.indent+"\t"+renderCode(l.code[other+1:]), l.origLine)...)
		out = append(out, written(l.indent+"end "+label, l.origLine)...)
	}
	return out
}

// closedExit starts the label of the block that endAtExits makes of an `otherwise return`.
const closedExit = "closedExit"

// cancelledExit is the label of the block that cancelBeforeLentExits makes of an `otherwise return`.
const cancelledExit = "beforeExit"

// cancelBeforeLentExits cancels the promise of an async call that is lent a socket of the names before each exit that would
// leave it unawaited, between the call and the first line that awaits, cancels or otherwise names the promise: maxon cancels
// a promise that is dropped, where here a promise that holds the caller's socket must be awaited or cancelled before an exit
// (E3212). A return or a throw gets `p.cancel()` before it, and a try's `otherwise return v` becomes a block that cancels and
// returns.
func cancelBeforeLentExits(lines []sourceLine, names []string) []sourceLine {
	var out []sourceLine
	cancelAt := map[int]string{}
	for i, l := range lines {
		c := l.code
		if !isCode(l) || len(c) < 5 || c[0].text != "let" || c[1].kind != tokIdent || c[2].text != "=" || c[3].text != "async" {
			continue
		}
		lent := false
		texts := codeTexts(c)
		for k := 4; k+1 < len(texts); k++ {
			if slices.Contains(names, texts[k]) && (texts[k-1] == "(" || texts[k-1] == "," || texts[k-1] == ":") && (texts[k+1] == ")" || texts[k+1] == ",") {
				lent = true
			}
		}
		if !lent {
			continue
		}
		promise := c[1].text
		for k := i + 1; k < len(lines) && (!isCode(lines[k]) || indentDepth(lines[k]) >= indentDepth(l)); k++ {
			if !isCode(lines[k]) {
				continue
			}
			if mentions(lines[k].code, promise) {
				break
			}
			cancelAt[k] = promise
		}
	}
	serial := 0
	for k, l := range lines {
		promise, ok := cancelAt[k]
		if !ok {
			out = append(out, l)
			continue
		}
		texts := codeTexts(l.code)
		if texts[0] == "return" || texts[0] == "throw" {
			out = append(out, written(l.indent+promise+".cancel()", l.origLine)...)
			out = append(out, l)
			continue
		}
		other := slices.Index(texts, "otherwise")
		if other < 0 || other+1 >= len(texts) || texts[other+1] != "return" {
			out = append(out, l)
			continue
		}
		serial++
		label := "'" + cancelledExit + "'"
		if serial > 1 {
			label = "'" + cancelledExit + strconv.Itoa(serial) + "'"
		}
		out = append(out, written(l.indent+renderCode(l.code[:other])+" otherwise "+label, l.origLine)...)
		out = append(out, written(l.indent+"\t"+promise+".cancel()", l.origLine)...)
		out = append(out, written(l.indent+"\t"+renderCode(l.code[other+1:]), l.origLine)...)
		out = append(out, written(l.indent+"end "+label, l.origLine)...)
	}
	return out
}

// socketChanges are the methods of a socket that take it mutates here, where maxon's take it as they would any value.
var socketChanges = []string{"accept", "send", "readSome", "setReadDeadline", "setWriteDeadline", "setAcceptDeadline", "shutdownWrite"}

// mutateSocketParameters passes a parameter of a socket type consumes where the function closes it or moves it on, since
// the socket is then the function's own in maxon's program, and mutates where the function only changes it, by a method of
// socketChanges, or passes it on, as an argument, to a call that may. A mutates parameter is borrowed, so nothing closes it:
// a close of it would move it out of its borrow. A consumes parameter that the function also changes is taken into a var of
// its name on the body's first line, as a consumes parameter is not changed in place.
func mutateSocketParameters(lines []sourceLine) []sourceLine {
	owned := map[int][]string{}
	for i, l := range lines {
		fn := funcKeywordIndex(l.code)
		if !isCode(l) || fn < 0 {
			continue
		}
		end := i + 1
		for end < len(lines) && (!isCode(lines[end]) || indentDepth(lines[end]) > indentDepth(l)) {
			end++
		}
		var code []token
		for k := 0; k < len(l.code); k++ {
			t := l.code[k]
			if k+1 < len(l.code) && t.kind == tokIdent && slices.Contains(socketTypes, l.code[k+1].text) && k > 0 && (l.code[k-1].text == "(" || l.code[k-1].text == ",") {
				switch {
				case ownsIn(lines[i+1:end], t.text):
					code = append(code, token{kind: tokIdent, text: "consumes"})
					if changesIn(lines[i+1:end], t.text) {
						owned[i] = append(owned[i], t.text)
					}
				case changesIn(lines[i+1:end], t.text):
					code = append(code, token{kind: tokIdent, text: "mutates"})
				}
			}
			code = append(code, t)
		}
		lines[i].code = code
	}
	var out []sourceLine
	for i, l := range lines {
		out = append(out, l)
		for _, name := range owned[i] {
			out = append(out, written(l.indent+"\t"+"var "+name+" = "+name, l.origLine)...)
		}
	}
	return out
}

// ownsIn is whether lines end a socket of the name, a parameter: close it, return it, pass it consumes, adopt it or put it in
// a record.
func ownsIn(lines []sourceLine, name string) bool {
	for _, l := range lines {
		if !isCode(l) {
			continue
		}
		if closes(l, name) {
			return true
		}
		texts := codeTexts(l.code)
		for k, t := range texts {
			if t != name || k+1 < len(texts) && texts[k+1] == "." {
				continue
			}
			if k > 0 && (texts[k-1] == "return" || texts[k-1] == "consumes") || slices.Contains(texts, "adopt") || slices.Contains(texts, "{") {
				return true
			}
		}
	}
	return false
}

// changesIn is whether lines change a socket of the name: call a method of socketChanges on it or pass it on as an argument.
func changesIn(lines []sourceLine, name string) bool {
	for _, l := range lines {
		if !isCode(l) {
			continue
		}
		texts := codeTexts(l.code)
		for k, t := range texts {
			if t != name {
				continue
			}
			if k+2 < len(texts) && texts[k+1] == "." && slices.Contains(socketChanges, texts[k+2]) {
				return true
			}
			if k > 0 && (texts[k-1] == "(" || texts[k-1] == ",") && k+1 < len(texts) && (texts[k+1] == ")" || texts[k+1] == ",") {
				return true
			}
		}
	}
	return false
}

// discardSockets makes a discard of a new socket, `_ = try TcpClient.connect(...)`, a binding that the next line closes.
func discardSockets(lines []sourceLine) []sourceLine {
	var out []sourceLine
	for _, l := range lines {
		if !isCode(l) || len(l.code) < 3 || l.code[0].text != "_" || l.code[1].text != "=" || !opensSocket(l.code) {
			out = append(out, l)
			continue
		}
		l.code = slices.Concat([]token{{kind: tokIdent, text: "var"}, {kind: tokIdent, text: discardedSocket}}, l.code[1:])
		out = append(out, l)
		out = append(out, written(l.indent+socketClose(discardedSocket), l.origLine)...)
	}
	return out
}

// tryBareCloses makes each close of a socket of the names that is a statement of its own a try that ignores its failure.
func tryBareCloses(lines []sourceLine, names []string) {
	for i, l := range lines {
		if !isCode(l) || len(l.code) != 5 || !slices.Contains(names, l.code[0].text) {
			continue
		}
		if slices.Equal(codeTexts(l.code)[1:], []string{".", "close", "(", ")"}) {
			lines[i].code = written(socketClose(l.code[0].text), l.origLine)[0].code
		}
	}
}

// closeAtScopeEnds closes each socket a line binds where its scope ends with the socket still the scope's, and, in the body of
// an `if let`, before each return.
func closeAtScopeEnds(lines []sourceLine) []sourceLine {
	type insertion struct {
		before int
		text   []string
	}
	var inserts []insertion
	replaced := map[int][]string{}
	for i, l := range lines {
		if !isCode(l) {
			continue
		}
		name, ifLet := socketDeclaration(l)
		if name == "" {
			continue
		}
		depth := indentDepth(l)
		bodyDepth := depth
		if ifLet {
			bodyDepth = depth + 1
		}
		end := i + 1
		for end < len(lines) && (!isCode(lines[end]) || indentDepth(lines[end]) > depth || !ifLet && indentDepth(lines[end]) == depth) {
			end++
		}
		closed := false
		last := -1
		for k := i + 1; k < end; k++ {
			if !isCode(lines[k]) {
				continue
			}
			if closes(lines[k], name) || movesOn(lines[k], name) {
				closed = true
			}
			if indentDepth(lines[k]) == bodyDepth {
				last = k
			}
		}
		if closed {
			continue
		}
		if ifLet {
			for k := i + 1; k < end; k++ {
				r := lines[k]
				if !isCode(r) || r.code[0].text != "return" {
					continue
				}
				if !mentions(r.code[1:], name) {
					inserts = append(inserts, insertion{k, []string{r.indent + socketClose(name)}})
					continue
				}
				value := renderCode(r.code[1:])
				replaced[k] = []string{r.indent + "let " + socketResult + " = " + value, r.indent + socketClose(name), r.indent + "return " + socketResult}
			}
		}
		if last >= 0 && !exits(lines[last]) && !endsEndlessLoop(lines, last) && end <= len(lines) {
			inserts = append(inserts, insertion{end, []string{strings.Repeat("\t", bodyDepth) + socketClose(name)}})
		}
	}
	var out []sourceLine
	for i := 0; i <= len(lines); i++ {
		for _, ins := range inserts {
			if ins.before == i {
				origLine := 0
				if i < len(lines) {
					origLine = lines[i].origLine
				}
				for _, t := range ins.text {
					out = append(out, written(t, origLine)...)
				}
			}
		}
		if i == len(lines) {
			break
		}
		if texts, ok := replaced[i]; ok {
			for _, t := range texts {
				out = append(out, written(t, lines[i].origLine)...)
			}
			continue
		}
		out = append(out, lines[i])
	}
	return out
}

// endsEndlessLoop is whether line last ends a `while true` loop that no break leaves, after which a scope's code is never
// reached, so a close written there would be the statement a missing return is reported for.
func endsEndlessLoop(lines []sourceLine, last int) bool {
	end := lines[last]
	if len(end.code) != 2 || end.code[0].text != "end" || end.code[1].kind != tokLabel {
		return false
	}
	for k := last - 1; k >= 0; k-- {
		l := lines[k]
		if !isCode(l) || indentDepth(l) > indentDepth(end) {
			if isCode(l) && hasToken(l.code, "break") {
				return false
			}
			continue
		}
		texts := codeTexts(l.code)
		return indentDepth(l) == indentDepth(end) && slices.Equal(texts, []string{"while", "true", end.code[1].text})
	}
	return false
}

// renderCode is the source of a line's tokens, spaced as the conversion spaces them.
func renderCode(code []token) string {
	var b strings.Builder
	for k, t := range code {
		if k > 0 && needsSpace(code, k-1, k) {
			b.WriteString(" ")
		}
		b.WriteString(t.text)
	}
	return b.String()
}

// hoistedValue starts the name of the value of a try that hoistInlineExits takes out of a call.
const hoistedValue = "socketValue"

// hoistInlineExits takes a try whose failure returns out of the call it is the one argument of, `F(try x.readSome(n) otherwise
// return 0)`, into a binding of its own before the line, so that the compiler's ending fix can close the sockets the return
// would drop: a return inside a call's brackets can hold no block.
func hoistInlineExits(lines []sourceLine) []sourceLine {
	var out []sourceLine
	serial := 0
	for _, l := range lines {
		if !isCode(l) {
			out = append(out, l)
			continue
		}
		for {
			at := -1
			for k := 1; k+1 < len(l.code); k++ {
				if l.code[k].text == "(" && l.code[k-1].kind == tokIdent && l.code[k+1].text == "try" {
					at = k
					break
				}
			}
			if at < 0 {
				break
			}
			closeAt := findMatchingParen(l.code, at)
			inner := l.code[at+1 : max(closeAt, at+1)]
			texts := codeTexts(inner)
			other := slices.Index(texts, "otherwise")
			if closeAt < 0 || other < 0 || other+1 >= len(texts) || texts[other+1] != "return" || slices.Contains(texts, ",") {
				break
			}
			serial++
			name := hoistedValue + strconv.Itoa(serial)
			out = append(out, written(l.indent+"let "+name+" = "+renderCode(inner), l.origLine)...)
			l.code = slices.Concat(l.code[:at+1], []token{{kind: tokIdent, text: name}}, l.code[closeAt:])
		}
		out = append(out, l)
	}
	return out
}
