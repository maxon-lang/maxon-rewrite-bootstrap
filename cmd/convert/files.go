package main

import (
	"slices"
	"strings"
)

// The rules of maxon's file streams, File.maxon's FileSource and helpers/file/fileWriter.maxon's FileWriter, over the library's
// __ManagedFile (stdlib/file/managedFile.maxon): a file is a linear value here, which no drop closes, and its close throws.

// fileEnding is a method of maxon's that ends the value of a type over an open file. Here a file is a linear value, ended
// once by one of its destructors, so the method takes `destructs self`. A close throws what the system's close reports, as
// Go's Close does: the error throws names, of which mapper makes the error __ManagedFile.close throws, as the type's other
// failures are made.
type fileEnding struct {
	owner, method, throws, mapper string
}

var fileEndings = []fileEnding{
	{"FileWriter", "close", "FileWriteError", "writeFailureOf"},
	{"FileWriter", "writeAll", "", ""},
	{"FileWriter", "writeAllText", "", ""},
	{"FileWriter", "writeAllManaged", "", ""},
	{"FileSource", "close", "FileReadError", "readFailureOf"},
}

// fileCloseLabel is the label of the handler that rewriteFileEndings writes around a close of an open file.
const fileCloseLabel = "'unclosed'"

// rewriteFileEndings makes each method of fileEndings a destructor, `destructs self` its first parameter, and a close one
// that throws: the close of the type's __ManagedFile in its body, `file.close()`, becomes a try whose handler throws the
// type's error that mapper makes of it.
func rewriteFileEndings(lines []sourceLine) []sourceLine {
	owner := ""
	var out []sourceLine
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if l.removed || len(l.code) == 0 {
			out = append(out, l)
			continue
		}
		if l.indent == "" {
			owner = declaredTypeName(l.code)
		}
		ending, ok := fileEndingOf(owner, l)
		if !ok {
			out = append(out, l)
			continue
		}
		l.code = endingSignature(l.code, ending)
		out = append(out, l)
		if ending.mapper == "" {
			continue
		}
		for i+1 < len(lines) && !(lines[i+1].indent == l.indent && len(lines[i+1].code) > 0 && lines[i+1].code[0].text == "end") {
			i++
			if isFileClose(lines[i]) {
				out = append(out, throwingClose(lines[i], ending.mapper)...)
				continue
			}
			out = append(out, lines[i])
		}
	}
	return out
}

// declaredTypeName is the name of the type a top-level line declares, or "".
func declaredTypeName(code []token) string {
	at := skipModifiers(code)
	if at+1 < len(code) && code[at].text == "type" {
		return code[at+1].text
	}
	return ""
}

// fileEndingOf is the ending that a member line of owner declares, if it declares one.
func fileEndingOf(owner string, l sourceLine) (fileEnding, bool) {
	if owner == "" || l.indent != "\t" {
		return fileEnding{}, false
	}
	fn := funcKeywordIndex(l.code)
	if fn < 0 || fn+1 >= len(l.code) {
		return fileEnding{}, false
	}
	at := slices.IndexFunc(fileEndings, func(e fileEnding) bool { return e.owner == owner && e.method == l.code[fn+1].text })
	if at < 0 {
		return fileEnding{}, false
	}
	return fileEndings[at], true
}

// endingSignature is a method's signature with `destructs self` its first parameter, and the ending's throws where it has one.
func endingSignature(code []token, ending fileEnding) []token {
	lp := findToken(code, funcKeywordIndex(code)+1, "(")
	receiver := []token{{kind: tokIdent, text: "destructs"}, {kind: tokIdent, text: "self"}}
	if lp+1 < len(code) && code[lp+1].text != ")" {
		receiver = append(receiver, token{kind: tokPunct, text: ","})
	}
	out := slices.Concat(code[:lp+1], receiver, code[lp+1:])
	if ending.throws != "" && !slices.ContainsFunc(out, func(t token) bool { return t.kind == tokIdent && t.text == "throws" }) {
		out = append(out, token{kind: tokIdent, text: "throws"}, token{kind: tokIdent, text: ending.throws})
	}
	return out
}

// isFileClose is whether a line is the close of the type's file, `file.close()`.
func isFileClose(l sourceLine) bool {
	if l.removed || len(l.code) != 5 {
		return false
	}
	texts := make([]string, len(l.code))
	for k, t := range l.code {
		texts[k] = t.text
	}
	return slices.Equal(texts, []string{"file", ".", "close", "(", ")"})
}

// throwingClose is a close of the type's file whose failure throws the type's error, mapped by mapper.
func throwingClose(l sourceLine, mapper string) []sourceLine {
	text := strings.Join([]string{
		l.indent + "try file.close() otherwise (e) " + fileCloseLabel,
		l.indent + "\tthrow " + mapper + "(e)",
		l.indent + "end " + fileCloseLabel,
	}, "\n") + "\n"
	written := splitLines(text)
	for k := range written {
		written[k].origLine = l.origLine
	}
	return written
}

// fileBuffersAlias is the typealias of a buffer of bytes that the library declares beside __ManagedFile, through which a
// converted file calls a static of the buffer, since a static is called through a typealias of an instance.
const fileBuffersAlias = "ByteMemory"

// rewriteFileBuffers writes maxon's bare buffer, __ManagedMemory, as a buffer of bytes in a file that names __ManagedFile, whose
// rows read and write bytes: the type as `__ManagedMemory with Byte`, and the create of one of the element size 1 through
// the library's alias, `ByteMemory.create(n, 1)`. A bare buffer that some other code of the file names is no file's, and the
// file is left as it is.
func rewriteFileBuffers(lines []sourceLine) {
	if !slices.ContainsFunc(lines, func(l sourceLine) bool {
		return !l.removed && slices.ContainsFunc(l.code, func(t token) bool { return t.kind == tokIdent && t.text == "__ManagedFile" })
	}) {
		return
	}
	for i := range lines {
		if lines[i].removed {
			continue
		}
		lines[i].code = labelFileCalls(bytesBuffers(lines[i].code))
	}
}

// fileCallLabels are the labels of the second parameter of the methods of __ManagedFile that maxon's code gives by its place,
// which only a built-in's parameter may be given by here: `readAppending(managed, most)` is `readAppending(managed, most: most)`.
var fileCallLabels = map[string]string{"readAppending": "most"}

// labelFileCalls is a line's code with the second argument of each call of fileCallLabels given by its label.
func labelFileCalls(code []token) []token {
	for k := 1; k+1 < len(code); k++ {
		label, ok := fileCallLabels[code[k].text]
		if !ok || code[k-1].text != "." || code[k+1].text != "(" {
			continue
		}
		depth := 0
		for j := k + 1; j < len(code); j++ {
			switch code[j].text {
			case "(", "[":
				depth++
			case ")", "]":
				depth--
			}
			if depth == 0 {
				break
			}
			if depth == 1 && code[j].text == "," {
				if j+2 < len(code) && code[j+2].text == ":" {
					break
				}
				code = slices.Concat(code[:j+1], []token{{kind: tokIdent, text: label}, {kind: tokPunct, text: ":"}}, code[j+1:])
				break
			}
		}
	}
	return code
}

// bytesBuffers is a line's code with each bare __ManagedMemory a buffer of bytes.
func bytesBuffers(code []token) []token {
	var out []token
	for k := range code {
		t := code[k]
		if t.kind != tokIdent || t.text != "__ManagedMemory" || k+1 < len(code) && code[k+1].text == "with" {
			out = append(out, t)
			continue
		}
		if k+2 < len(code) && code[k+1].text == "." && code[k+2].text == "create" {
			out = append(out, token{kind: tokIdent, text: fileBuffersAlias})
			continue
		}
		out = append(out, t, token{kind: tokIdent, text: "with"}, token{kind: tokIdent, text: "Byte"})
	}
	return out
}
