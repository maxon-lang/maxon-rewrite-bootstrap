package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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
	if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), []byte(importsMaxon(macosImports))) {
		t.Errorf("%s is not what the import list generates; run `go generate ./internal/abigen`", importsFile)
	}
}

// literalImport matches a libSystem function named by a string literal: a call of the runtime's `__Raw.callImportN` in
// its macOS source, its files named macos*, or the arm64 entry stub's `arm64CallImport`. A DLL function of Windows needs
// no list: a PE imports those its live code calls, by their names.
var literalImport = regexp.MustCompile(`(?:callImport(\d+)|arm64CallImport)\("([A-Za-z0-9_]+)"`)

func TestEveryImportTheCodeCallsIsListed(t *testing.T) {
	libSystem := make(map[string]libSystemFunction)
	for _, f := range macosImports {
		if _, ok := libSystem[f.name]; ok {
			t.Errorf("libSystem function %s is listed twice", f.name)
		}
		libSystem[f.name] = f
	}
	for _, dir := range []string{"../../runtime", "../../self/backend", "../../tests/runtime"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.maxon"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			macos := strings.HasPrefix(filepath.Base(file), "macos")
			for _, m := range literalImport.FindAllStringSubmatch(string(src), -1) {
				if m[1] != "" && !macos {
					continue
				}
				f, ok := libSystem[m[2]]
				if !ok {
					t.Errorf("%s calls the libSystem function %s, which macosImports in imports.go lacks", file, m[2])
					continue
				}
				if words, _ := strconv.Atoi(m[1]); f.variadic && m[1] != "" && words < f.fixed {
					t.Errorf("%s calls the variadic %s with %d words, fewer than its %d fixed ones", file, f.name, words, f.fixed)
				}
			}
		}
	}
}
