package main

import (
	"fmt"
	"slices"
	"strings"
)

// sharedArgumentFunction is a function of maxon's library that its callers pass one array as the array it writes and as
// arrays it reads (`limbCarryChain(ws, dstBase: d, a: ws, aBase: x, b: ws, bBase: y, ...)`, where the operands are slots of
// one workspace). maxon's arrays are shared references, so the function reads what it has written so far; here a call
// cannot borrow the array it writes as another argument, and a copy of the workspace at each call costs more than the
// arithmetic. So the conversion gives the function a form for each set of the read parameters that may share the written
// one, which reads that array where the parameter was read, and calls it where the arguments are the same: the same
// algorithm, reading what maxon's reads, with no copy.
type sharedArgumentFunction struct {
	name    string
	written string
	reads   []string
}

var sharedArgumentFunctions = []sharedArgumentFunction{
	{name: "limbBorrowChain", written: "dst", reads: []string{"a", "b"}},
	{name: "limbCarryChain", written: "dst", reads: []string{"a", "b"}},
}

// sharedFormName is the name of f's form in which the parameters shared read the array f writes: `limbCarryChainSharingAB`.
func sharedFormName(f sharedArgumentFunction, shared []string) string {
	name := f.name + "Sharing"
	for _, p := range shared {
		name += strings.ToUpper(p[:1]) + p[1:]
	}
	return name
}

// sharedSubsets is every non-empty set of f's read parameters, in the order of f.reads.
func sharedSubsets(f sharedArgumentFunction) [][]string {
	var subsets [][]string
	for mask := 1; mask < 1<<len(f.reads); mask++ {
		var s []string
		for i, p := range f.reads {
			if mask&(1<<i) != 0 {
				s = append(s, p)
			}
		}
		subsets = append(subsets, s)
	}
	return subsets
}

// rewriteSharedArguments adds the shared forms after the declaration of each function of sharedArgumentFunctions, and
// calls the form wherever a call passes its written argument again as read arguments.
func rewriteSharedArguments(lines []sourceLine) []sourceLine {
	var out []sourceLine
	for i := 0; i < len(lines); i++ {
		f, ok := sharedFunctionDeclaredAt(lines, i)
		if !ok {
			out = append(out, lines[i])
			continue
		}
		end, _ := declarationEnd(lines, i)
		out = append(out, lines[i:end+1]...)
		for _, shared := range sharedSubsets(f) {
			out = append(out, sourceLine{blank: true})
			out = append(out, sharedForm(lines[i:end+1], f, shared)...)
		}
		i = end
	}
	for i := range out {
		if !out[i].removed {
			out[i].code = callSharedForms(out[i].code)
		}
	}
	return out
}

// sharedFunctionDeclaredAt is the function of sharedArgumentFunctions whose top-level declaration line i opens.
func sharedFunctionDeclaredAt(lines []sourceLine, i int) (sharedArgumentFunction, bool) {
	l := lines[i]
	if l.removed || l.indent != "" || len(l.code) == 0 {
		return sharedArgumentFunction{}, false
	}
	at := skipModifiers(l.code)
	if at+1 >= len(l.code) || l.code[at].text != "function" {
		return sharedArgumentFunction{}, false
	}
	k := slices.IndexFunc(sharedArgumentFunctions, func(f sharedArgumentFunction) bool { return f.name == l.code[at+1].text })
	if k < 0 {
		return sharedArgumentFunction{}, false
	}
	return sharedArgumentFunctions[k], true
}

// sharedForm is a copy of the declaration decl of f named for shared, with the parameters shared left out of its header and
// each read of one in its body a read of the written parameter.
func sharedForm(decl []sourceLine, f sharedArgumentFunction, shared []string) []sourceLine {
	name := sharedFormName(f, shared)
	form := make([]sourceLine, len(decl))
	for n, l := range decl {
		l.code = slices.Clone(l.code)
		form[n] = l
	}
	header := form[0].code
	at := skipModifiers(header)
	header[at+1].text = name
	open := at + 2
	if header[open].text != "(" {
		panic(fmt.Sprintf("line %d: the header of %s has no parameter list", decl[0].origLine, f.name))
	}
	params := []token{header[open]}
	for k := open + 1; header[k-1].text != ")"; {
		next := argumentEnd(header, k)
		if !slices.Contains(shared, header[k].text) {
			params = append(params, header[k:next]...)
			params = append(params, header[next])
		} else if header[next].text == ")" {
			params[len(params)-1] = header[next]
		}
		k = next + 1
	}
	form[0].code = append(append(append([]token{}, header[:open]...), params...), header[findMatchingParen(header, open)+1:]...)
	for n := 1; n < len(form); n++ {
		code := form[n].code
		for k := range code {
			switch {
			case code[k].text == "'"+f.name+"'":
				code[k].text = "'" + name + "'"
			case code[k].kind == tokIdent && slices.Contains(shared, code[k].text) && (k == 0 || code[k-1].text != ".") && (k+1 >= len(code) || code[k+1].text != ":"):
				code[k].text = f.written
			}
		}
	}
	return form
}

// callSharedForms is code with each call of a function of sharedArgumentFunctions that passes its first argument again as
// read arguments a call of the form for those arguments, which leaves them out.
func callSharedForms(code []token) []token {
	for k := 0; k+1 < len(code); k++ {
		at := slices.IndexFunc(sharedArgumentFunctions, func(f sharedArgumentFunction) bool { return f.name == code[k].text })
		if at < 0 || code[k].kind != tokIdent || code[k+1].text != "(" || k > 0 && (code[k-1].text == "." || code[k-1].text == "function") {
			continue
		}
		f := sharedArgumentFunctions[at]
		closeAt := findMatchingParen(code, k+1)
		if closeAt < 0 {
			panic(fmt.Sprintf("line %d: the call of %s has no )", code[k].line, f.name))
		}
		written := code[k+2 : argumentEnd(code, k+2)]
		var shared []string
		var kept []token
		for a := k + 2; a < closeAt; {
			next := argumentEnd(code, a)
			arg := code[a:next]
			if len(arg) > 2 && arg[1].text == ":" && slices.Contains(f.reads, arg[0].text) && sameTokens(arg[2:], written) {
				shared = append(shared, arg[0].text)
			} else {
				if len(kept) > 0 {
					kept = append(kept, token{kind: tokPunct, text: ","})
				}
				kept = append(kept, arg...)
			}
			a = next + 1
		}
		if len(shared) == 0 {
			continue
		}
		slices.SortFunc(shared, func(x, y string) int { return slices.Index(f.reads, x) - slices.Index(f.reads, y) })
		call := append([]token{{kind: tokIdent, text: sharedFormName(f, shared)}, code[k+1]}, kept...)
		call = append(call, code[closeAt])
		code = append(append(append([]token{}, code[:k]...), call...), code[closeAt+1:]...)
	}
	return code
}

// sameTokens is whether a and b spell the same expression.
func sameTokens(a, b []token) bool {
	return slices.EqualFunc(a, b, func(x, y token) bool { return x.text == y.text })
}
