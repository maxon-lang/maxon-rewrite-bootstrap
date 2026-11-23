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
	if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), []byte(importsMaxon(importNames, macosImports))) {
		t.Errorf("%s is not what the import list generates; run `go generate ./internal/abigen`", importsFile)
	}
}

// literalImport matches a library function named by a string literal: a call of the runtime's `__Raw.callImportN` or
// the entry stub's `importIndex`. The runtime's macOS source is in its files named macos*, and calls libSystem; every
// other file calls the DLL functions of Windows.
var literalImport = regexp.MustCompile(`(?:callImport(\d+)|importIndex)\("([A-Za-z0-9_]+)"`)

func TestEveryImportTheCodeCallsIsListed(t *testing.T) {
	listed := make(map[string]bool)
	for _, n := range importNames {
		if listed[n] {
			t.Errorf("import %s is listed twice", n)
		}
		listed[n] = true
	}
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
				if !macos {
					if !listed[m[2]] {
						t.Errorf("%s calls the DLL function %s, which the import list in imports.go lacks; add it at the end", file, m[2])
					}
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
