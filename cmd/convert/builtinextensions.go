package main

import (
	"regexp"
	"slices"
	"strings"
)

// builtinTypeParams are the names the built-in table gives the type parameter of each generic type minimaxon builds in, where
// maxon's library names it `Element`: an extension of the type names it as the table does (README, "Extensions").
var builtinTypeParams = map[string]string{
	"Array": "T",
}

// maxonElementParam is the name of the type parameter of maxon's generic collections.
const maxonElementParam = "Element"

// storageHashCalls are the calls of maxon's library that ask the compiler about an array's native storage hashing, and what
// minimaxon writes for each: the library's own members of the built-in Array that answer them (internal/builtingen),
// whether the array hashes as the bytes of its storage, and their fold under the process's seed.
var storageHashCalls = []struct{ maxon, here []token }{
	{
		maxon: []token{{kind: tokIdent, text: "__Builtins"}, {kind: tokPunct, text: "."}, {kind: tokIdent, text: "elementsHashAsStorage"}, {kind: tokPunct, text: "("}, {kind: tokPunct, text: ")"}},
		here:  []token{{kind: tokIdent, text: "self"}, {kind: tokPunct, text: "."}, {kind: tokIdent, text: "hashesAsStorage"}, {kind: tokPunct, text: "("}, {kind: tokPunct, text: ")"}},
	},
	{
		maxon: []token{{kind: tokIdent, text: "__Builtins"}, {kind: tokPunct, text: "."}, {kind: tokIdent, text: "hashArrayStorage"}, {kind: tokPunct, text: "("}, {kind: tokIdent, text: "self"}, {kind: tokPunct, text: ")"}},
		here: []token{
			{kind: tokIdent, text: "hashValueOfDigest"}, {kind: tokPunct, text: "("}, {kind: tokIdent, text: "self"}, {kind: tokPunct, text: "."}, {kind: tokIdent, text: "storageHash"}, {kind: tokPunct, text: "("},
			{kind: tokIdent, text: "processFoldStart"}, {kind: tokPunct, text: "("}, {kind: tokPunct, text: ")"}, {kind: tokPunct, text: ")"}, {kind: tokPunct, text: ")"},
		},
	},
}

// rewriteBuiltinExtensions writes each extension of a type that minimaxon builds in (Array) as minimaxon has it:
// the type parameter under the built-in table's name, the receiver's own storage as `self.managed`, the members the
// built-in declares left out (the table has them natively), and the calls that ask for the native storage hash as the
// members that answer them here.
func rewriteBuiltinExtensions(lines []sourceLine) {
	for i := 0; i < len(lines); i++ {
		l := &lines[i]
		if l.removed || len(l.code) == 0 || l.indent != "" {
			continue
		}
		declIdx := skipModifiers(l.code)
		if declIdx+1 >= len(l.code) || l.code[declIdx].text != "extension" {
			continue
		}
		target := l.code[declIdx+1].text
		param, ok := builtinTypeParams[target]
		if !ok {
			continue
		}
		last := topLevelEnd(lines, i)
		for k := i; k <= last; k++ {
			renameInLine(&lines[k], param)
		}
		dropNativeMembers(lines, i, last, target)
		for k := i; k <= last; k++ {
			rewriteStorageHashCalls(&lines[k])
		}
		i = last
	}
}

// renameInLine writes the type parameter of a maxon collection as param and the field `managed` as `self.managed`.
func renameInLine(l *sourceLine, param string) {
	var out []token
	for k, t := range l.code {
		afterDot := k > 0 && l.code[k-1].text == "."
		switch {
		case t.kind == tokIdent && t.text == maxonElementParam && !afterDot:
			t.text = param
		case t.kind == tokIdent && t.text == "managed" && !afterDot:
			out = append(out, token{kind: tokIdent, text: "self"}, token{kind: tokPunct, text: "."})
		}
		out = append(out, t)
	}
	l.code = out
}

// extendedBuiltinRecords are the record types minimaxon builds in whose library declaration has members that the built-in
// lacks (Clock's `nowMs`, Directory's `removeTree`, Process's `currentEnvironmentEntries`): the declaration is kept as an
// extension of the built-in, which a program reaches as it reaches the type.
var extendedBuiltinRecords = []string{"Clock", "Directory", "File", "FileInfo", "FilePath", "Process"}

// builtinRecordConformances are the interfaces of a declaration of extendedBuiltinRecords that its extension leaves out:
// FilePath is a literal's type and has its toString as a built-in until the seed that compiles its library type.
var builtinRecordConformances = map[string][]string{
	"FilePath":        {"InitableFromStringLiteral", "Stringable"},
	"FileReadError":   {"Error"},
	"FileWriteError":  {"Error"},
	"FileDeleteError": {"Error"},
	"FileRenameError": {"Error"},
	"FileInfoError":   {"Error"},
}

// extendedBuiltinEnums are the enums minimaxon builds in whose library declaration has members that the built-in lacks (the
// `failure()` that maxon's File.maxon gives each error of File): the declaration is kept as an extension of the built-in, with
// the cases, which the built-in declares, left out.
var extendedBuiltinEnums = []string{"FileReadError", "FileWriteError", "FileDeleteError", "FileRenameError", "FileInfoError"}

// rewriteBuiltinRecords writes the declaration of each of extendedBuiltinRecords and extendedBuiltinEnums as an extension of the
// built-in type, with the members that the built-in declares left out, and the fields or the cases, which the built-in's own
// declaration replaces.
func rewriteBuiltinRecords(lines []sourceLine) {
	for i := 0; i < len(lines); i++ {
		l := &lines[i]
		if l.removed || len(l.code) == 0 || l.indent != "" {
			continue
		}
		declIdx := skipModifiers(l.code)
		if declIdx+1 >= len(l.code) {
			continue
		}
		isRecord := l.code[declIdx].text == "type" && slices.Contains(extendedBuiltinRecords, l.code[declIdx+1].text)
		isEnum := l.code[declIdx].text == "enum" && slices.Contains(extendedBuiltinEnums, l.code[declIdx+1].text)
		if !isRecord && !isEnum {
			continue
		}
		last := topLevelEnd(lines, i)
		target := l.code[declIdx+1].text
		l.code = slices.Concat([]token{{kind: tokIdent, text: "extension"}}, l.code[declIdx+1:])
		for _, iface := range builtinRecordConformances[target] {
			dropConformance(l, iface)
		}
		dropEmptyImplements(l)
		if isEnum {
			dropCases(lines, i, last)
		} else {
			dropFields(lines, i, last)
		}
		dropNativeMembers(lines, i, last, target)
		if isRecord {
			dropOrphanedMembers(lines, i, last)
		}
		i = last
	}
}

// builtinParamTypes are the parameters of the members of an extension of a built-in record whose type minimaxon has otherwise
// than maxon's: the time a file was made is a FileCreatedTime here, where maxon's FileInfo.create takes a Timestamp, which is
// st_ctime on Linux (README, "Where it differs from Maxon").
var builtinParamTypes = []struct{ owner, param, maxon, here string }{
	{"FileInfo", "createdTime", "Timestamp", "FileCreatedTime"},
}

// rewriteBuiltinParamTypes writes the type of each parameter of builtinParamTypes in the extensions of its owner as minimaxon's.
func rewriteBuiltinParamTypes(lines []sourceLine) {
	for i := 0; i < len(lines); i++ {
		l := &lines[i]
		if l.removed || len(l.code) < 2 || l.indent != "" || l.code[0].text != "extension" {
			continue
		}
		last := topLevelEnd(lines, i)
		for _, p := range builtinParamTypes {
			if l.code[1].text != p.owner {
				continue
			}
			for k := i; k <= last; k++ {
				code := lines[k].code
				for t := 0; t+1 < len(code); t++ {
					if code[t].kind == tokIdent && code[t].text == p.param && code[t+1].kind == tokIdent && code[t+1].text == p.maxon {
						code[t+1].text = p.here
					}
				}
			}
		}
		i = last
	}
}

// dropOrphanedMembers leaves out each member of the extension from header to last that only its own file may call and that
// nothing left in it names: a helper of members that the built-in declares, which dropNativeMembers left out (File.maxon's
// readWhole, of readText and readBinary).
func dropOrphanedMembers(lines []sourceLine, header, last int) {
	for k := header + 1; k < last; k++ {
		l := &lines[k]
		if l.removed || len(l.code) == 0 || l.indent != "	" {
			continue
		}
		declIdx := skipModifiers(l.code)
		if declIdx+1 >= len(l.code) || l.code[declIdx].text != "function" || hasModifier(l.code[:declIdx], "public") || hasModifier(l.code[:declIdx], "export") {
			continue
		}
		end := memberEnd(lines, k)
		if namedOutside(lines, l.code[declIdx+1].text, k, end) {
			k = end
			continue
		}
		for j := commentsAbove(lines, k); j <= end; j++ {
			lines[j].removed = true
		}
		k = end
	}
}

// dropFields leaves out the fields of the type from header to last, each a line of its own.
func dropFields(lines []sourceLine, header, last int) {
	for k := header + 1; k < last; k++ {
		l := &lines[k]
		if l.removed || len(l.code) == 0 || l.indent != "\t" {
			continue
		}
		if word := l.code[skipModifiers(l.code)].text; word == "var" || word == "let" {
			l.removed = true
		}
	}
}

// dropCases leaves out the cases of the enum from header to last, each a line of its own that holds a name and nothing else
// (its comment goes with it).
func dropCases(lines []sourceLine, header, last int) {
	for k := header + 1; k < last; k++ {
		l := &lines[k]
		if l.removed || len(l.code) == 0 || l.indent != "\t" {
			continue
		}
		if len(l.code) == 1 && l.code[0].kind == tokIdent {
			l.removed = true
		}
	}
}

// dropNativeMembers leaves out the members of the extension from header to last that the built-in type of its name declares.
func dropNativeMembers(lines []sourceLine, header, last int, target string) {
	for k := header + 1; k < last; k++ {
		l := &lines[k]
		if l.removed || len(l.code) == 0 || l.indent != "\t" {
			continue
		}
		declIdx := skipModifiers(l.code)
		if declIdx+1 >= len(l.code) || l.code[declIdx].text != "function" || !slices.Contains(builtinMethods[target], l.code[declIdx+1].text) {
			continue
		}
		end := memberEnd(lines, k)
		for j := commentsAbove(lines, k); j <= end; j++ {
			lines[j].removed = true
		}
		k = end
	}
}

// rewriteStorageHashCalls writes each of storageHashCalls in the line as minimaxon has it.
func rewriteStorageHashCalls(l *sourceLine) {
	for _, call := range storageHashCalls {
		for k := 0; k+len(call.maxon) <= len(l.code); k++ {
			if !slices.EqualFunc(l.code[k:k+len(call.maxon)], call.maxon, func(a, b token) bool { return a.kind == b.kind && a.text == b.text }) {
				continue
			}
			l.code = slices.Concat(l.code[:k], call.here, l.code[k+len(call.maxon):])
			k += len(call.here) - 1
		}
	}
}

// typeNamesHere are the types of maxon's that minimaxon types as another: the payload of WaitDeadline.within is a Count here,
// where maxon's is its own alias of that range.
var typeNamesHere = map[string]string{"WaitDeadlineMs": "Count"}

// rewriteTypeNames writes each name of typeNamesHere as minimaxon's, but in a text that declares the type itself, as the
// library's Subprocess declares WaitDeadlineMs for its own WaitDeadline.
func rewriteTypeNames(lines []sourceLine) {
	for i := range lines {
		l := &lines[i]
		for k, t := range l.code {
			here, ok := typeNamesHere[t.text]
			if ok && t.kind == tokIdent && (k == 0 || l.code[k-1].text != ".") && !declaresType(lines, t.text) {
				l.code[k].text = here
			}
		}
	}
}

// libraryBesideBuiltins are the types of maxon's Subprocess library that minimaxon also builds in. The library declares each
// as its own, so that a program can name either: the library's is written with besideBuiltinPrefix until the built-ins go.
var libraryBesideBuiltins = []string{"CollectedOutput", "Executable", "Subprocess", "SubprocessError", "WaitDeadline"}

// besideBuiltinPrefix starts the name of a type of libraryBesideBuiltins that the library declares.
const besideBuiltinPrefix = "Mx"

// builtinsOfTheBuiltInTypes are the built-ins that take the built-in Executable (Debuggee.launch, runWithInput) or WaitDeadline
// (the waiting removeTree and removeContents), so that a program that reaches one cannot name the library's.
var builtinsOfTheBuiltInTypes = []string{"Debuggee", "runWithInput"}

// builtinsTakingWaitDeadline are the built-ins whose WaitDeadline is the built-in one, which a program names beside them.
var builtinsTakingWaitDeadline = []string{"removeTree", "removeContents"}

// libraryOnlySubprocessTypes are the types of the library's Subprocess that the built-ins have no counterpart of, besides
// Subprocess itself: a program that names one runs a child through the library.
var libraryOnlySubprocessTypes = []string{"Subprocess", "Configuration", "InputSource", "OutputDestination", "WorkingDirectory", "TerminationStatus", "PlatformOptions", "StreamingSubprocess", "StdioStream", "TreeEnd", "ExitPoll", "TerminationPoll"}

// buildsLibraryError matches a program's making of a case of SubprocessError that carries a payload, which the library's union
// has and the built-in enum, whose cases carry none, does not.
var buildsLibraryError = regexp.MustCompile(`(^|[^A-Za-z0-9_.])SubprocessError[.](executableNotFound|spawnFailed|ioFailed|timeout|treeNotEnded|detachRefused|statusLost)[(]`)

// programNames is what the whole of a program (every file of a spec test) says about the names its files write.
type programNames struct {
	// own are the types of libraryTypeNamesHere and libraryBesideBuiltins that the program declares itself, which it keeps.
	own []string
	// ownLog are the types of logLibraryTypes that the program declares itself, which it keeps.
	ownLog []string
	// usesLibrarySubprocess is whether the program runs a child through Subprocess and so names the library's types.
	usesLibrarySubprocess bool
	// expectsError is whether the program is a spec test's that expects a compile error; errorLines are the lines of the
	// test's block that its expectation names, and lineOffset is how many lines of the block stand before the program's.
	expectsError bool
	errorLines   []int
	lineOffset   int
}

// programNamesOf reads the names of a program from its source, all its files together.
func programNamesOf(src string) programNames {
	lines := splitLines(src)
	names := programNames{}
	for _, name := range libraryTypeNamesHere {
		if declaresType(lines, name.maxon) {
			names.own = append(names.own, name.maxon)
		}
	}
	for _, name := range libraryBesideBuiltins {
		if declaresType(lines, name) {
			names.own = append(names.own, name)
		}
	}
	names.ownLog = ownLogTypes(lines)
	words := wordsOf(src)
	names.usesLibrarySubprocess = (slices.ContainsFunc(libraryOnlySubprocessTypes, func(word string) bool { return words[word] && !declaresType(lines, word) }) || buildsLibraryError.MatchString(src) && !declaresType(lines, "SubprocessError")) &&
		!slices.ContainsFunc(builtinsOfTheBuiltInTypes, func(word string) bool { return words[word] }) &&
		!(words["WaitDeadline"] && slices.ContainsFunc(builtinsTakingWaitDeadline, func(word string) bool { return words[word] }))
	return names
}

// renameBesideBuiltins writes each type of libraryBesideBuiltins that the library file declares with its prefix, wherever the
// file writes it: in code, labels, strings and comments.
func renameBesideBuiltins(lines []sourceLine) {
	for _, name := range libraryBesideBuiltins {
		if !declaresType(lines, name) {
			continue
		}
		here := besideBuiltinPrefix + name
		label := "'" + name + "'"
		for i := range lines {
			l := &lines[i]
			for k, t := range l.code {
				switch {
				case t.kind == tokIdent && t.text == name && (k == 0 || l.code[k-1].text != "."):
					l.code[k].text = here
				case t.kind == tokLabel && t.text == label:
					l.code[k].text = "'" + here + "'"
				case t.kind == tokString:
					l.code[k].text = replaceWholeWord(t.text, name, here)
				}
			}
			l.comment = replaceWholeWord(l.comment, name, here)
			l.trailing = replaceWholeWord(l.trailing, name, here)
		}
	}
}

// renameProgramBesideBuiltins writes each type of libraryBesideBuiltins that a program names, and does not declare, as the
// library's.
func renameProgramBesideBuiltins(lines []sourceLine, own []string) {
	for i := range lines {
		l := &lines[i]
		for k, t := range l.code {
			if t.kind == tokIdent && slices.Contains(libraryBesideBuiltins, t.text) && !slices.Contains(own, t.text) && (k == 0 || l.code[k-1].text != ".") {
				l.code[k].text = besideBuiltinPrefix + t.text
			}
		}
	}
}

// libraryTypeNamesHere are the types of maxon's library that minimaxon names otherwise (README, "Where it differs from
// Maxon"): maxon's Slot, one value or none, is Cell here. stdlib/files.txt names the module and file it goes to.
var libraryTypeNamesHere = []struct{ maxon, here string }{{"Slot", "Cell"}}

// renameLibraryTypes writes each type of libraryTypeNamesHere that the text names as minimaxon's. A program or a helper of
// the library that declares a type of that name, or takes a type parameter of it, names its own and keeps it, as does a
// file of a program whose other files declare it (ownTypes); a file of the library's root that declares it is the type's
// own, whose comments and messages say its new name too.
func renameLibraryTypes(file string, lines []sourceLine, kind inputKind, ownTypes []string) {
	root := ""
	if kind == sourceFile && !isLibraryHelper(file) {
		root = libraryRoot(file)
	}
	for _, name := range libraryTypeNamesHere {
		old, here := name.maxon, name.here
		own := declaresType(lines, old)
		if (own || slices.Contains(ownTypes, old)) && root == "" || root != "" && helperTypes(root)[old] {
			continue
		}
		label := "'" + old + "'"
		for i := range lines {
			l := &lines[i]
			for k, t := range l.code {
				switch {
				case t.kind == tokIdent && t.text == old && (k == 0 || l.code[k-1].text != "."):
					l.code[k].text = here
				case t.kind == tokLabel && t.text == label:
					l.code[k].text = "'" + here + "'"
				case own && t.kind == tokString:
					l.code[k].text = renameWord(t.text, old, here)
				}
			}
			if own {
				l.comment = renameWord(l.comment, old, here)
				l.trailing = renameWord(l.trailing, old, here)
			}
		}
	}
}

// namesHidingModules are the names of library modules that a field, a parameter or a local of maxon's library would hide where
// it writes the module before a name of another module (stream.StreamError), and the name each is written as here. maxon's
// library writes no module, and its Subprocess has a field and parameters named stream.
var namesHidingModules = map[string]string{"stream": "which"}

// renameNamesHidingModules writes each name of namesHidingModules as its replacement, wherever the file writes it as a name:
// in code and in the interpolations of strings, and not in their text or in comments.
func renameNamesHidingModules(lines []sourceLine) {
	for i := range lines {
		l := &lines[i]
		for k, t := range l.code {
			here, ok := namesHidingModules[t.text]
			switch {
			case ok && t.kind == tokIdent:
				l.code[k].text = here
			case t.kind == tokString:
				l.code[k].text = renameInInterpolations(t.text, namesHidingModules)
			}
		}
	}
}

// localsHidingModules are the names of library modules that a local of maxon's library is named where its function also names
// a constant or a function of that module, which a module's name before the local's scope reaches no more (a module's name
// is read past a local only before a type of the module, README "Modules"): httpHelpers' httpBuildRequest has a local url
// and reads url.UrlOriginFormRoot. A local, unlike a parameter, is no label of a call, so it is renamed, from its
// declaration to the end of its function.
var localsHidingModules = map[string]string{"url": "address"}

// renameLocalsHidingModules writes each local of localsHidingModules as its replacement in the function that declares it.
func renameLocalsHidingModules(lines []sourceLine) {
	for i := range lines {
		l := &lines[i]
		if len(l.code) < 3 || (l.code[0].text != "let" && l.code[0].text != "var") || l.code[2].text != "=" {
			continue
		}
		here, ok := localsHidingModules[l.code[1].text]
		if !ok {
			continue
		}
		old := l.code[1].text
		depth := len(l.indent)
		for j := i; j < len(lines); j++ {
			m := &lines[j]
			if j > i && len(m.code) > 0 && len(m.indent) < depth {
				break
			}
			for k, t := range m.code {
				switch {
				case t.kind == tokIdent && t.text == old && (k == 0 || m.code[k-1].text != ".") && (k+1 >= len(m.code) || m.code[k+1].text != ":"):
					m.code[k].text = here
				case t.kind == tokString:
					m.code[k].text = renameInInterpolations(t.text, map[string]string{old: here})
				}
			}
		}
	}
}

// renameInInterpolations writes each whole word of an interpolation of a string that names is a key of names, as its value.
func renameInInterpolations(text string, names map[string]string) string {
	return interpolationPart.ReplaceAllStringFunc(text, func(part string) string {
		var pairs [][2]string
		for old, here := range names {
			pairs = append(pairs, [2]string{old, here})
		}
		return replaceWords(part, pairs)
	})
}

// declaresType says whether the text declares a type, an enum, a union, an interface or a typealias named name, or takes
// a type parameter of that name.
func declaresType(lines []sourceLine, name string) bool {
	for _, l := range lines {
		at := skipModifiers(l.code)
		if at+1 < len(l.code) && slices.Contains([]string{"type", "enum", "union", "interface", "typealias"}, l.code[at].text) && l.code[at+1].text == name {
			return true
		}
		inUses := false
		for _, t := range l.code {
			switch {
			case t.kind == tokIdent && t.text == "uses":
				inUses = true
			case t.kind == tokIdent && (t.text == "implements" || t.text == "where"):
				inUses = false
			case inUses && t.kind == tokIdent && t.text == name:
				return true
			}
		}
	}
	return false
}

// renameWord writes each whole word old in text, and the word with its first letter lower case, as here likewise.
func renameWord(text, old, here string) string {
	lowerOld, lowerHere := strings.ToLower(old[:1])+old[1:], strings.ToLower(here[:1])+here[1:]
	return replaceWords(text, [][2]string{{old, here}, {lowerOld, lowerHere}})
}

// replaceWholeWord writes each whole word old in text as here.
func replaceWholeWord(text, old, here string) string {
	return replaceWords(text, [][2]string{{old, here}})
}

// replaceWords writes each whole word of text that a pair names as the pair's second.
func replaceWords(text string, pairs [][2]string) string {
	var b strings.Builder
	isWord := func(c byte) bool {
		return c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
	}
	for i := 0; i < len(text); {
		matched := false
		for _, w := range pairs {
			end := i + len(w[0])
			if (i == 0 || !isWord(text[i-1])) && strings.HasPrefix(text[i:], w[0]) && (end == len(text) || !isWord(text[end])) {
				b.WriteString(w[1])
				i, matched = end, true
				break
			}
		}
		if !matched {
			b.WriteByte(text[i])
			i++
		}
	}
	return b.String()
}

// primitiveExtensionTargets are the built-in types whose text the interpolation writes itself, so maxon's library gives
// them a toString that minimaxon leaves out, with their Stringable conformance (README, "Where it differs from Maxon").
var primitiveExtensionTargets = []string{"int", "float", "bool"}

// dropPrimitiveToStrings leaves out the toString of each extension of a primitive type, and Stringable from its
// implements clause.
func dropPrimitiveToStrings(lines []sourceLine) {
	for i := 0; i < len(lines); i++ {
		l := &lines[i]
		if l.removed || len(l.code) == 0 || l.indent != "" {
			continue
		}
		declIdx := skipModifiers(l.code)
		if declIdx+1 >= len(l.code) || l.code[declIdx].text != "extension" || !slices.Contains(primitiveExtensionTargets, l.code[declIdx+1].text) {
			continue
		}
		dropConformance(l, "Stringable")
		last := topLevelEnd(lines, i)
		for k := i + 1; k < last; k++ {
			m := &lines[k]
			if m.removed || m.indent != "\t" || len(m.code) == 0 {
				continue
			}
			at := skipModifiers(m.code)
			if at+1 >= len(m.code) || m.code[at].text != "function" || m.code[at+1].text != "toString" {
				continue
			}
			end := memberEnd(lines, k)
			for j := commentsAbove(lines, k); j <= end; j++ {
				lines[j].removed = true
			}
			if k > 0 && lines[k-1].blank {
				lines[k-1].removed = true
			}
			k = end
		}
		i = last
	}
}

// dropConformance removes the interface name, and the comma that joins it to the list, from a header's implements
// clause.
func dropConformance(l *sourceLine, name string) {
	for k, t := range l.code {
		if t.kind != tokIdent || t.text != name {
			continue
		}
		from, to := k, k+1
		switch {
		case to < len(l.code) && l.code[to].text == ",":
			to++
			for to < len(l.code) && l.code[to].kind == tokSpace {
				to++
			}
		case from > 0 && l.code[from-1].text == ",":
			from--
		case from > 0 && l.code[from-1].kind == tokSpace && from > 1 && l.code[from-2].text == ",":
			from -= 2
		}
		l.code = slices.Delete(l.code, from, to)
		return
	}
}

// dropEmptyImplements removes an implements clause that names nothing.
func dropEmptyImplements(l *sourceLine) {
	impl := findToken(l.code, 0, "implements")
	if impl < 0 {
		return
	}
	k := impl + 1
	for k < len(l.code) && l.code[k].kind == tokSpace {
		k++
	}
	if k < len(l.code) && l.code[k].kind == tokIdent && l.code[k].text != "where" {
		return
	}
	from := impl
	for from > 0 && l.code[from-1].kind == tokSpace {
		from--
	}
	l.code = slices.Delete(l.code, from, k)
}
