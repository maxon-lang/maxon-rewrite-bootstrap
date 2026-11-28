package main

import "slices"

// TLS is not brought over yet (README, "Where it differs from Maxon"): maxon's library has its sessions, TlsStream, TlsListener
// and TlsClient, which nothing here declares, and the library's tls module keeps TlsError and the options a program could
// name, which the facility table refuses on every system. So a converted library file leaves out what runs a session: a
// union's case that holds one, each arm of a match of `self` on that case in the union's methods, and each function and
// method that names a session type, and a declaration that only what was left out reached goes with it.

// tlsCases are the cases of a type of another library file that hold a TLS failure, which minimaxon's declaration lacks:
// an arm of a match of a parameter of the type on one is left out (HttpClient's clientReadFault on StreamError).
var tlsCases = map[string][]string{"StreamError": {"secure"}}

// tlsSessionTypes are maxon's types of a TLS session.
var tlsSessionTypes = []string{"TlsStream", "TlsListener", "TlsClient"}

// namesTlsSession reports whether the code names a TLS session type.
func namesTlsSession(code []token) bool {
	return slices.ContainsFunc(code, func(t token) bool { return t.kind == tokIdent && slices.Contains(tlsSessionTypes, t.text) })
}

// dropTlsSessions leaves out what runs a TLS session in a library file (see above).
func dropTlsSessions(lines []sourceLine) {
	named := map[string]bool{}
	droppedCases := map[string][]string{}
	owner, ownerWord := "", ""
	for i := 0; i < len(lines); i++ {
		l := &lines[i]
		if l.removed || len(l.code) == 0 {
			continue
		}
		if l.indent == "" {
			declIdx := skipModifiers(l.code)
			owner, ownerWord = declaredName(l.code), ""
			if declIdx < len(l.code) {
				ownerWord = l.code[declIdx].text
			}
		}
		declIdx := skipModifiers(l.code)
		isFunction := declIdx+1 < len(l.code) && l.code[declIdx].text == "function"
		switch {
		case isFunction && ownerWord != "interface" && (l.indent == "" || l.indent == "\t"):
			end := memberEnd(lines, i)
			if slices.ContainsFunc(lines[i:end+1], func(m sourceLine) bool { return !m.removed && namesTlsSession(m.code) }) {
				dropLines(lines, commentsAbove(lines, i), end, named)
			}
			i = end
		case l.indent == "\t" && ownerWord == "union" && namesTlsSession(l.code) && l.code[0].kind == tokIdent:
			droppedCases[owner] = append(droppedCases[owner], l.code[0].text)
			dropLines(lines, commentsAbove(lines, i), i, named)
		}
	}
	owner = ""
	var matchedCases []string
	params := map[string]string{}
	for i := range lines {
		l := &lines[i]
		if l.removed || len(l.code) == 0 {
			continue
		}
		if l.indent == "" {
			owner = declaredName(l.code)
		}
		if fn, ok := parameterTypes(l.code); ok {
			params = fn
		}
		if m := findToken(l.code, 0, "match"); m >= 0 && m+1 < len(l.code) && (m == 0 || l.code[0].text == "return") {
			matchedCases = nil
			if subject := l.code[m+1].text; subject == "self" {
				matchedCases = droppedCases[owner]
			} else {
				matchedCases = tlsCases[params[subject]]
			}
			continue
		}
		if slices.Contains(matchedCases, l.code[0].text) && len(l.code) > 1 && (l.code[1].text == "(" || l.code[1].text == "then" || l.code[1].text == "gives") {
			dropLines(lines, i, i, named)
		}
	}
	for dropped := true; dropped; {
		dropped = false
		for i := range lines {
			// Only a top-level declaration goes: a method's name is written by the dropped arms of other types' matches too.
			if lines[i].removed || len(lines[i].code) == 0 || lines[i].indent != "" {
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

// dropLibraryMerges writes maxon's `merge x` in a library file as `x`: a region merged into another is a value moved here,
// where an owned value is what maxon's isolated region is (README, "Where it differs from Maxon").
func dropLibraryMerges(lines []sourceLine) {
	for i := range lines {
		l := &lines[i]
		if l.removed {
			continue
		}
		var out []token
		for k, t := range l.code {
			if t.kind == tokIdent && t.text == "merge" && k+1 < len(l.code) && isRegionOperandStart(l.code[k+1]) && (k == 0 || l.code[k-1].text != ".") {
				continue
			}
			out = append(out, t)
		}
		l.code = out
	}
}

// closeOpenRanges writes the open end of a range pattern, maxon's `min upto 200` and `506 to max`, in a library file as the
// bound of the matched parameter's integer typealias, which the file declares (`typealias HttpStatusNumber = int(100 to
// 999)`): minimaxon's patterns name both ends.
func closeOpenRanges(lines []sourceLine) {
	bounds := map[string][2]string{}
	for _, l := range lines {
		if l.removed || l.indent != "" {
			continue
		}
		i := skipModifiers(l.code)
		if i+8 < len(l.code) && l.code[i].text == "typealias" && l.code[i+2].text == "=" && l.code[i+3].text == "int" && l.code[i+4].text == "(" && l.code[i+6].text == "to" && l.code[i+8].text == ")" {
			bounds[l.code[i+1].text] = [2]string{l.code[i+5].text, l.code[i+7].text}
		}
	}
	params := map[string]string{}
	subject := ""
	for i := range lines {
		l := &lines[i]
		if l.removed || len(l.code) == 0 {
			continue
		}
		if fn, ok := parameterTypes(l.code); ok {
			params = fn
			continue
		}
		if m := findToken(l.code, 0, "match"); m >= 0 && m+1 < len(l.code) {
			subject = l.code[m+1].text
			continue
		}
		b, ok := bounds[params[subject]]
		if !ok {
			continue
		}
		for k, t := range l.code {
			if t.kind != tokIdent {
				continue
			}
			if t.text == "min" && k+1 < len(l.code) && (l.code[k+1].text == "to" || l.code[k+1].text == "upto") && (k == 0 || l.code[k-1].text != ".") {
				l.code[k] = token{kind: tokInt, text: b[0]}
			}
			if t.text == "max" && k > 0 && l.code[k-1].text == "to" {
				l.code[k] = token{kind: tokInt, text: b[1]}
			}
		}
	}
}

// parameterTypes are the names of a function header's parameters with the first word of each one's type, and whether the
// line is a function header.
func parameterTypes(code []token) (map[string]string, bool) {
	fn := skipModifiers(code)
	if fn+1 >= len(code) || code[fn].text != "function" {
		return nil, false
	}
	params := map[string]string{}
	for k := fn + 3; k+1 < len(code); k++ {
		if code[k].kind == tokIdent && code[k+1].kind == tokIdent && (code[k-1].text == "(" || code[k-1].text == ",") {
			params[code[k].text] = code[k+1].text
		}
	}
	return params, true
}
