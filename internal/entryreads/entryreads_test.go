// Package entryreads holds the test that keeps a handle from being used as an index outside module ast.
package entryreads

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// site matches the start of an index into the entries of a type table or a function table.
var site = regexp.MustCompile(`(?:^|[^\w.])((?:\w+\.)*(?:types|funcs|tt|ft))\.entries\[`)

// accessor matches an index written through the one place that relates a handle to a position.
var accessor = regexp.MustCompile(`^ast\.(?:typeIndex|funcIndex)\(`)

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

// badSites lists the indexes into a type or function table in src that do not go through ast.typeIndex or ast.funcIndex.
func badSites(src string) []string {
	var bad []string
	for from := 0; from < len(src); {
		m := site.FindStringSubmatchIndex(src[from:])
		if m == nil {
			break
		}
		open := from + m[1] - 1
		end := closing(src, open)
		if end < 0 {
			bad = append(bad, src[open:])
			break
		}
		index := src[open+1 : end]
		if loc := accessor.FindStringIndex(index); loc == nil || closing(index, loc[1]-1) != len(index)-1 {
			bad = append(bad, src[from+m[2]:end+1])
		}
		from = open + 1
	}
	return bad
}

func TestNoHandleIsAnIndexOutsideAst(t *testing.T) {
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
		for _, b := range badSites(string(data)) {
			t.Errorf("%s: %s indexes a type or function table without ast.typeIndex or ast.funcIndex: a handle is not an index outside module ast; run phase3/rewrite-entry-reads.py or write the accessor", path, b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBadSitesFindsWhatIsNotAnAccessor(t *testing.T) {
	good := "p.types.entries[ast.typeIndex(ast.originOf(p.types, t: t))].name + ft.entries[ast.funcIndex(f)].x"
	if got := badSites(good); len(got) != 0 {
		t.Errorf("accessor sites reported: %v", got)
	}
	for _, bad := range []string{"p.types.entries[t as Count]", "x.funcs.entries[i].y", "tt.entries[ast.typeIndex(a) + 1]", "ft.entries[sig]"} {
		if got := badSites(bad); len(got) != 1 {
			t.Errorf("%q: got %v, want one site", bad, got)
		}
	}
	if got := badSites("vm.h.maps[data].entries[i] and entries[i]"); len(got) != 0 {
		t.Errorf("another table reported: %v", got)
	}
}
