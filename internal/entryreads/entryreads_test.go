// Package entryreads holds the tests that keep a handle from being used as an index outside module ast, and an entry
// from being written by a stage after its module's declare stage.
package entryreads

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// site matches the start of an index into the lists of a type or a function slot, or into the slots of a function table.
var site = regexp.MustCompile(`(?:^|[^\w.])((?:\w+\.)*(?:types|tt|funcs|ft)\.lists|(?:\w+\.)*(?:funcs|ft)\.slots)\[`)

// listAccessor and indexAccessor match the indexes of an entry, written through the one place that relates a handle to a
// list of the slot a task holds and a position there.
var (
	listAccessor  = regexp.MustCompile(`^ast\.(typeList|funcList)\(`)
	indexAccessor = regexp.MustCompile(`^ast\.(typeIndex|funcIndex)\(`)
)

// closing is the index of the bracket that closes the one at s[open], or -1.
func closing(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// accessorArg is the handle that index, the text between two brackets, passes to the accessor re, or false when index is
// more than one call of it.
func accessorArg(re *regexp.Regexp, index string) (fn, arg string, ok bool) {
	loc := re.FindStringSubmatchIndex(index)
	if loc == nil || closing(index, loc[1]-1) != len(index)-1 {
		return "", "", false
	}
	return index[loc[2]:loc[3]], index[loc[1] : len(index)-1], true
}

// pairs are the accessor of the index that goes with each accessor of the list.
var pairs = map[string]string{"typeList": "typeIndex", "funcList": "funcIndex"}

// entryEnd is the index of the bracket that closes the index of an entry whose first bracket is at src[open], and whether
// it goes through the accessors of one handle: X.lists[ast.typeList(X, t: h)][ast.typeIndex(X, t: h)] for a type and
// X.lists[ast.funcList(X, f: h)][ast.funcIndex(X, f: h)] for a function.
func entryEnd(src string, open int) (int, bool) {
	end := closing(src, open)
	if end < 0 {
		return -1, false
	}
	if end+1 >= len(src) || src[end+1] != '[' {
		return end, false
	}
	second := closing(src, end+1)
	if second < 0 {
		return -1, false
	}
	listFn, listArg, ok := accessorArg(listAccessor, src[open+1:end])
	if !ok {
		return second, false
	}
	indexFn, indexArg, ok := accessorArg(indexAccessor, src[end+2:second])
	return second, ok && indexFn == pairs[listFn] && listArg == indexArg
}

// badSites lists the indexes into a type slot or a function table in src that do not go through ast.typeList and
// ast.typeIndex (or ast.funcIndex) of one handle, or that read the entry, which goes through ast.typeEntry or
// ast.funcEntry: only a write indexes the slot a task holds.
func badSites(src string) []string {
	var bad []string
	for from := 0; from < len(src); {
		m := site.FindStringSubmatchIndex(src[from:])
		if m == nil {
			break
		}
		open := from + m[1] - 1
		end, ok := entryEnd(src, open)
		if end < 0 {
			bad = append(bad, src[open:])
			break
		}
		if !ok || !write.MatchString(src[end+1:]) {
			bad = append(bad, src[from+m[2]:end+1])
		}
		from = open + 1
	}
	return bad
}

// write matches what follows an entry when the site changes it: an assignment to a field or an element of one, or a call
// that changes a list it holds.
var write = regexp.MustCompile(`^(?:\.\w+|\[[^\]\n]*\])*(?:\s+=\s|\s+\+=|\.(?:push|insert|append|remove|pop|clear)\()`)

// writeSites lists the sites in src that write a field of a type or function entry.
func writeSites(src string) []string {
	var sites []string
	for from := 0; from < len(src); {
		m := site.FindStringSubmatchIndex(src[from:])
		if m == nil {
			break
		}
		open := from + m[1] - 1
		end, _ := entryEnd(src, open)
		if end < 0 {
			break
		}
		if loc := write.FindStringIndex(src[end+1:]); loc != nil {
			sites = append(sites, src[from+m[2]:end+1+loc[1]])
		}
		from = open + 1
	}
	return sites
}

// entryWriters are the files outside module ast that may write an entry: those of the stages of a module up to and
// including declare (parse, aliases, declare), which fill the entries of what the module declares, and those of the
// bodies stage that make the closures and behaviours, which fill the entries they make. A later stage keeps what it
// decides about an entry in its module's ast.ModuleTables, so that the entry is frozen once the declare stage ends.
var entryWriters = map[string]bool{
	"project/project.maxon":  true,
	"check/aliases.maxon":    true,
	"check/declare.maxon":    true,
	"check/members.maxon":    true,
	"check/functions.maxon":  true,
	"check/extensions.maxon": true,
	"check/enums.maxon":      true,
	"check/closures.maxon":   true,
	"check/behaviours.maxon": true,
}

// walkSelf calls visit with the path below self/ and the text of every source file of self/ outside module ast.
func walkSelf(t *testing.T, visit func(rel, src string)) {
	root := filepath.Join("..", "..", "self")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path == filepath.Join(root, "ast") {
			return filepath.SkipDir
		}
		if d.IsDir() || !(strings.HasSuffix(path, ".maxon") || strings.HasSuffix(path, ".maxtest")) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		visit(filepath.ToSlash(rel), string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNoHandleIsAnIndexOutsideAst(t *testing.T) {
	walkSelf(t, func(rel, src string) {
		for _, b := range badSites(src) {
			t.Errorf("%s: %s indexes a type or function table without the accessors of one handle: a handle is not an index outside module ast; read through ast.typeEntry or ast.funcEntry, and write through X.lists[ast.typeList(X, t: h)][ast.typeIndex(X, t: h)] or X.lists[ast.funcList(X, f: h)][ast.funcIndex(X, f: h)] of the slot the task holds", rel, b)
		}
	})
}

func TestEntriesAreWrittenOnlyUpToDeclare(t *testing.T) {
	walkSelf(t, func(rel, src string) {
		if entryWriters[rel] || strings.HasSuffix(rel, ".maxtest") {
			return
		}
		for _, w := range writeSites(src) {
			t.Errorf("%s: %s writes an entry after its module's declare stage; keep the result in the module's ast.ModuleTables, behind an accessor", rel, w)
		}
	})
}

func TestWriteSitesFindsWrites(t *testing.T) {
	for _, w := range []string{"p.types.lists[ast.typeList(p.types, t: t)][ast.typeIndex(p.types, t: t)].rawType = r", "ms.funcs.lists[ast.funcList(ms.funcs, f: f)][ast.funcIndex(ms.funcs, f: f)].params.push(x)", "tt.lists[ast.typeList(tt, t: t)][ast.typeIndex(tt, t: t)].cases[i].rawValue = v"} {
		if got := writeSites(w); len(got) != 1 {
			t.Errorf("%q: got %v, want one write", w, got)
		}
	}
	for _, r := range []string{"p.types.lists[ast.typeList(p.types, t: t)][ast.typeIndex(p.types, t: t)].rawKind == k", "p.types.lists[ast.typeList(p.types, t: t)][ast.typeIndex(p.types, t: t)].findMethod(m).isSome()", "let x = ms.funcs.lists[ast.funcList(ms.funcs, f: f)][ast.funcIndex(ms.funcs, f: f)].params[i].conv"} {
		if got := writeSites(r); len(got) != 0 {
			t.Errorf("%q: got %v, want none", r, got)
		}
	}
}

func TestBadSitesFindsWhatIsNotAnAccessor(t *testing.T) {
	good := "p.types.lists[ast.typeList(p.types, t: ast.originOf(p.types, t: t))][ast.typeIndex(p.types, t: ast.originOf(p.types, t: t))].name = n + ft.lists[ast.funcList(ft, f: f)][ast.funcIndex(ft, f: f)].x.push(y)"
	if got := badSites(good); len(got) != 0 {
		t.Errorf("accessor sites reported: %v", got)
	}
	for _, bad := range []string{"p.types.lists[0][t as Count]", "x.funcs.slots[s][i].y", "tt.lists[ast.typeList(tt, t: a)][ast.typeIndex(tt, t: a) + 1]", "ft.slots[s].lists[i]", "ft.lists[ast.funcList(ft, f: f)][ast.funcIndex(ft, f: f) + 1].x = y", "let n = ft.lists[ast.funcList(ft, f: f)][ast.funcIndex(ft, f: f)].name", "ft.lists[ast.typeList(ft, t: f)][ast.funcIndex(ft, f: f)].x = y", "tt.lists[ast.typeList(tt, t: t)][ast.funcIndex(tt, f: t)]", "tt.lists[ast.typeList(tt, t: t)]"} {
		if got := badSites(bad); len(got) != 1 {
			t.Errorf("%q: got %v, want one site", bad, got)
		}
	}
	if got := badSites("vm.h.maps[data].slots[i] and slots[i]"); len(got) != 0 {
		t.Errorf("another table reported: %v", got)
	}
}
