package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The generator runs in internal/ast, so its output paths are relative to that directory.
var astDir = filepath.Join("..", "ast")

func TestGeneratedFilesAreCurrent(t *testing.T) {
	outs, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outs {
		got, err := os.ReadFile(filepath.Join(astDir, o.path))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), o.code) {
			t.Errorf("%s is not what the built-in table generates; run `go generate ./internal/ast`", o.path)
		}
	}
}
