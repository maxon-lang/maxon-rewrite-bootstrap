package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestGeneratedFilesAreCurrent(t *testing.T) {
	for _, o := range outputs {
		got, err := os.ReadFile(o.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), []byte(generate(o))) {
			t.Errorf("%s is not what the layouts table generates; run `go generate ./internal/abigen`", o.path)
		}
	}
}

func TestImportsFileIsCurrent(t *testing.T) {
	got, err := os.ReadFile(importsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), []byte(importsMaxon(importNames))) {
		t.Errorf("%s is not what the import list generates; run `go generate ./internal/abigen`", importsFile)
	}
}

// literalImport matches a DLL function named by a string literal: a call of the runtime's `__Raw.callImportN` or the
// entry stub's `importIndex`.
var literalImport = regexp.MustCompile(`(?:callImport\d+|importIndex)\("([A-Za-z0-9_]+)"`)

func TestEveryImportTheCodeCallsIsListed(t *testing.T) {
	listed := make(map[string]bool)
	for _, n := range importNames {
		if listed[n] {
			t.Errorf("import %s is listed twice", n)
		}
		listed[n] = true
	}
	for _, dir := range []string{"../../runtime", "../../self/backend", "../../tests/runtime"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.maxon"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range literalImport.FindAllStringSubmatch(string(src), -1) {
				if !listed[m[1]] {
					t.Errorf("%s calls the DLL function %s, which the import list in imports.go lacks; add it at the end", f, m[1])
				}
			}
		}
	}
}
