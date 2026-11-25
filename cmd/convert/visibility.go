package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// In maxon an unmarked declaration is visible to the subtree of the directory that declares it, so a file at the root of the
// library lets every other file of the library name its unmarked declarations. Here an unmarked declaration is the declaring
// module's only, and the library is laid out in modules (stdlib/files.txt), so a declaration that a file of another module
// names becomes `export`, which reaches the whole library and no program. A file that is not converted yet counts as another
// module: it may land in any, and an export that its module did not need reaches nothing more.

// libraryListing maps each file of maxon's library that the listing names, by its path from the library's root in slashes, to
// the library module it goes to; it is read from --listing and nil without it, which leaves the rule off.
var libraryListing map[string]string

// readLibraryListing reads stdlib/files.txt: a line is `file module [name]`, as cmd/gate's listing is.
func readLibraryListing(listing string) (map[string]string, error) {
	text, err := os.ReadFile(listing)
	if err != nil {
		return nil, err
	}
	modules := map[string]string{}
	for line := range strings.SplitSeq(string(text), "\n") {
		words := strings.Fields(line)
		if len(words) == 0 {
			continue
		}
		if len(words) < 2 {
			return nil, fmt.Errorf("%s: '%s' names no module", listing, line)
		}
		modules[filepath.ToSlash(words[0])] = words[1]
	}
	return modules, nil
}

// libraryWordsByRoot holds, for each library root read so far, the words each of its files writes (libraryWords).
var libraryWordsByRoot = map[string]map[string]map[string]bool{}

// libraryWords are the identifiers that each file of the library at root writes in code, with those of the interpolations of
// its strings, by the file's path from root in slashes.
func libraryWords(root string) map[string]map[string]bool {
	if found, ok := libraryWordsByRoot[root]; ok {
		return found
	}
	found := map[string]map[string]bool{}
	files := libraryFiles(root)
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		found[rel] = wordsOf(strings.ReplaceAll(string(data), "\r\n", "\n"))
	}
	libraryWordsByRoot[root] = found
	return found
}

// libraryFiles are the files of the library at root, by their path from root in slashes, in order.
func libraryFiles(root string) []string {
	var files []string
	_ = filepath.WalkDir(root, func(file string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(file) != sourceExt {
			return nil
		}
		if rel, err := filepath.Rel(root, file); err == nil {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	slices.Sort(files)
	return files
}

// wordsOf are the identifiers of src outside comments, with those inside the braces of its string literals.
func wordsOf(src string) map[string]bool {
	words := map[string]bool{}
	for _, t := range lex(src) {
		switch t.kind {
		case tokIdent:
			words[t.text] = true
		case tokString:
			for _, w := range interpolatedWords(t.text) {
				words[w] = true
			}
		}
	}
	return words
}

// interpolatedWords are the identifiers in the braces of a string literal's text.
func interpolatedWords(literal string) []string {
	var words []string
	depth := 0
	for i := 0; i < len(literal); i++ {
		switch c := literal[i]; {
		case c == '\\' && depth == 0:
			i++
		case c == '{':
			depth++
		case c == '}' && depth > 0:
			depth--
		case depth > 0 && isIdentStart(c):
			start := i
			for i+1 < len(literal) && isIdentPart(literal[i+1]) {
				i++
			}
			words = append(words, literal[start:i+1])
		}
	}
	return words
}

// namedByOtherModules are the words that a file of the library which may name file's unmarked declarations writes, and
// whose module is not file's: those under the directory that declares file (maxon's rule for an unmarked declaration), other
// than file's own, that the listing puts in another module or does not list.
func namedByOtherModules(root, file string) map[string]bool {
	rel, err := filepath.Rel(root, file)
	if err != nil {
		panic(fmt.Sprintf("convert: %s is not under the library at %s: %v", file, root, err))
	}
	rel = filepath.ToSlash(rel)
	module, listed := libraryListing[rel]
	if !listed {
		return nil
	}
	dir := path.Dir(rel)
	words := map[string]bool{}
	for other, found := range libraryWords(root) {
		if other == rel || (dir != "." && !strings.HasPrefix(other, dir+"/")) {
			continue
		}
		if otherModule, ok := libraryListing[other]; ok && otherModule == module {
			continue
		}
		for w := range found {
			words[w] = true
		}
	}
	return words
}

// exportDeclarationsNamedElsewhere writes export before each unmarked declaration of the file that another module names: a
// top-level declaration, or a member of a type, an enum, a union or an extension, other than the cases of an enum. A
// signature may not name a less visible type (E3167), so each type of the file that an exported declaration's signature
// names is exported too.
func exportDeclarationsNamedElsewhere(lines []sourceLine, named map[string]bool) {
	if named == nil {
		return
	}
	declared := unmarkedDeclarations(lines)
	typesByName := map[string][]int{}
	for i, d := range declared {
		if d.isType {
			typesByName[d.name] = append(typesByName[d.name], i)
		}
	}
	exported := make([]bool, len(declared))
	var pending []int
	mark := func(i int) {
		if !exported[i] {
			exported[i] = true
			pending = append(pending, i)
		}
	}
	for i, d := range declared {
		if named[d.name] {
			mark(i)
		}
	}
	for len(pending) > 0 {
		d := declared[pending[0]]
		pending = pending[1:]
		for _, t := range signatureWords(lines[d.line].code, d.at) {
			for _, i := range typesByName[t] {
				mark(i)
			}
		}
	}
	for i, d := range declared {
		if exported[i] {
			l := &lines[d.line]
			at := 0
			for l.code[at].kind == tokAnnot {
				at++
			}
			l.code = slices.Insert(l.code, at, token{kind: tokIdent, text: "export"})
		}
	}
}

// declaration is an unmarked declaration of a file that another module could name: its line, the index in the line's code
// of the word that starts it, its name, and whether it declares a type.
type declaration struct {
	line   int
	at     int
	name   string
	isType bool
}

// typeWords start the declaration of a type, whose name a signature may write.
var typeWords = []string{"type", "enum", "union", "interface", "typealias"}

// unmarkedDeclarations are the declarations of lines that no `public` or `export` marks and that another module could name,
// in the order of the file.
func unmarkedDeclarations(lines []sourceLine) []declaration {
	type frame struct {
		tabs int
		word string
	}
	var found []declaration
	var open []frame
	for i := range lines {
		l := &lines[i]
		if l.removed || len(l.code) == 0 {
			continue
		}
		tabs := len(l.indent)
		for len(open) > 0 && open[len(open)-1].tabs >= tabs {
			open = open[:len(open)-1]
		}
		var enclosing []string
		for _, f := range open {
			if !isConditionWord(f.word) {
				enclosing = append(enclosing, f.word)
			}
		}
		at := min(skipModifiers(l.code), len(l.code)-1)
		open = append(open, frame{tabs: tabs, word: l.code[at].text})
		if !isDeclaredHere(enclosing) {
			continue
		}
		if name, ok := unmarkedDeclarationName(l.code); ok {
			found = append(found, declaration{line: i, at: at, name: name, isType: slices.Contains(typeWords, l.code[at].text)})
		}
	}
	return found
}

// signatureWords are the identifiers that the line of a declaration writes after its name, other than in the result of a
// function, which a less visible type may be.
func signatureWords(code []token, at int) []string {
	var words []string
	inResult := false
	for _, t := range code[at+2:] {
		switch {
		case t.text == "returns":
			inResult = true
		case t.text == "throws" || t.text == "where":
			inResult = false
		case t.kind == tokIdent && !inResult:
			words = append(words, t.text)
		}
	}
	return words
}

// isConditionWord is whether a line that opens a block starts a conditional compilation, whose lines are those of the
// declaration level it sits in.
func isConditionWord(word string) bool {
	return word == "#if" || word == "#else" || word == "#elseif"
}

// isDeclaredHere is whether lines inside the blocks of enclosing declare a name that other modules may name: the top level,
// or the body of a type, enum, union or extension that sits there. An interface's requirements have the interface's own
// visibility.
func isDeclaredHere(enclosing []string) bool {
	switch len(enclosing) {
	case 0:
		return true
	case 1:
		return slices.Contains([]string{"type", "enum", "union", "extension"}, enclosing[0])
	}
	return false
}

// declaringWords start a declaration that has a name.
var declaringWords = []string{"function", "type", "enum", "union", "interface", "typealias", "let", "var"}

// unmarkedDeclarationName is the name a line declares when the line has neither `public` nor `export`.
func unmarkedDeclarationName(code []token) (string, bool) {
	at := skipModifiers(code)
	if at+1 >= len(code) || code[at].kind != tokIdent || !slices.Contains(declaringWords, code[at].text) || code[at+1].kind != tokIdent {
		return "", false
	}
	for _, t := range code[:at] {
		if t.text == "public" || t.text == "export" {
			return "", false
		}
	}
	return code[at+1].text, true
}
