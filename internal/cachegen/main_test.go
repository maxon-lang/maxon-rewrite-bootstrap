package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestGeneratedFilesAreCurrent(t *testing.T) {
	files, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range layers {
		want, ok := files[m]
		got, err := os.ReadFile(outputPath(m))
		if !ok {
			if err == nil {
				t.Errorf("%s holds functions that no stored type needs; run `go generate ./internal/cachegen`", outputPath(m))
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), []byte(want)) {
			t.Errorf("%s is not what the declarations generate; run `go generate ./internal/cachegen`", outputPath(m))
		}
	}
}

// TestUnstorableFieldFails holds the generator to refusing a field whose type it cannot store, naming the field, rather
// than leaving it out.
func TestUnstorableFieldFails(t *testing.T) {
	w := &world{decls: map[string]map[string]*decl{"ast": {
		"Held": {module: "ast", name: "Held", kind: recordDecl, fields: []field{{"count", "Count"}, {"value", "cown with Count"}}},
	}}}
	for _, m := range layers {
		if w.decls[m] == nil {
			w.decls[m] = map[string]*decl{}
		}
	}
	_, err := newGen(w).run(map[string][]string{"ast": {"Held"}})
	if err == nil || !strings.Contains(err.Error(), "ast.Held.value") {
		t.Fatalf("a field of a type the cache cannot store is not refused by name: %v", err)
	}
}
