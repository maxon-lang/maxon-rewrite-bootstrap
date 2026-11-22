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

// site matches the start of an index into the slots of a type table or a function table.
var site = regexp.MustCompile(`(?:^|[^\w.])((?:\w+\.)*(?:types|funcs|tt|ft))\.slots\[`)

// slotAccessor and indexAccessor match the two indexes of an entry, written through the one place that relates a handle
// to a slot and a position there.
var (
	slotAccessor  = regexp.MustCompile(`^ast\.(typeSlot|funcSlot)\(`)
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

// pairs is the accessor of a slot and that of a position in it, for each table.
var pairs = map[string]string{"typeSlot": "typeIndex", "funcSlot": "funcIndex"}

// entryEnd is the index of the bracket that closes the read of an entry whose first bracket is at src[open], and whether
// the read goes through the accessors of one handle: X.slots[ast.typeSlot(h)][ast.typeIndex(h)].
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
	slotFn, slotArg, ok := accessorArg(slotAccessor, src[open+1:end])
	if !ok {
		return second, false
	}
	indexFn, indexArg, ok := accessorArg(indexAccessor, src[end+2:second])
	return second, ok && pairs[slotFn] == indexFn && slotArg == indexArg
}

// badSites lists the indexes into a type or function table in src that do not go through ast.typeSlot and ast.typeIndex
// (or ast.funcSlot and ast.funcIndex) of one handle.
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
		if !ok {
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
		if d.IsDir() || !strings.HasSuffix(path, ".maxon") {
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
			t.Errorf("%s: %s indexes a type or function table without the accessors of one handle (X.slots[ast.typeSlot(h)][ast.typeIndex(h)]): a handle is not an index outside module ast; run phase3/rewrite-slot-reads.py or write the accessors", rel, b)
		}
	})
}

func TestEntriesAreWrittenOnlyUpToDeclare(t *testing.T) {
	walkSelf(t, func(rel, src string) {
		if entryWriters[rel] || strings.HasSuffix(rel, "_test.maxon") {
			return
		}
		for _, w := range writeSites(src) {
			t.Errorf("%s: %s writes an entry after its module's declare stage; keep the result in the module's ast.ModuleTables, behind an accessor", rel, w)
		}
	})
}

func TestWriteSitesFindsWrites(t *testing.T) {
	for _, w := range []string{"p.types.slots[ast.typeSlot(t)][ast.typeIndex(t)].rawType = r", "p.funcs.slots[ast.funcSlot(f)][ast.funcIndex(f)].params.push(x)", "tt.slots[ast.typeSlot(t)][ast.typeIndex(t)].cases[i].rawValue = v"} {
		if got := writeSites(w); len(got) != 1 {
			t.Errorf("%q: got %v, want one write", w, got)
		}
	}
	for _, r := range []string{"p.types.slots[ast.typeSlot(t)][ast.typeIndex(t)].rawKind == k", "p.types.slots[ast.typeSlot(t)][ast.typeIndex(t)].findMethod(m).isSome()", "let x = p.funcs.slots[ast.funcSlot(f)][ast.funcIndex(f)].params[i].conv"} {
		if got := writeSites(r); len(got) != 0 {
			t.Errorf("%q: got %v, want none", r, got)
		}
	}
}

func TestBadSitesFindsWhatIsNotAnAccessor(t *testing.T) {
	good := "p.types.slots[ast.typeSlot(ast.originOf(p.types, t: t))][ast.typeIndex(ast.originOf(p.types, t: t))].name + ft.slots[ast.funcSlot(f)][ast.funcIndex(f)].x"
	if got := badSites(good); len(got) != 0 {
		t.Errorf("accessor sites reported: %v", got)
	}
	for _, bad := range []string{"p.types.slots[0][t as Count]", "x.funcs.slots[s][i].y", "tt.slots[ast.typeSlot(a)][ast.typeIndex(a) + 1]", "ft.slots[ast.funcSlot(f)][ast.funcIndex(g)]", "tt.slots[ast.typeSlot(t)][ast.funcIndex(t)]", "tt.slots[ast.typeSlot(t)]"} {
		if got := badSites(bad); len(got) != 1 {
			t.Errorf("%q: got %v, want one site", bad, got)
		}
	}
	if got := badSites("vm.h.maps[data].slots[i] and slots[i]"); len(got) != 0 {
		t.Errorf("another table reported: %v", got)
	}
}
