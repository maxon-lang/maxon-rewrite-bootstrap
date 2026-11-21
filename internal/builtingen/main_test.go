package main

import (
	"bytes"
	"os"
	"testing"
)

func TestGeneratedFilesAreCurrent(t *testing.T) {
	outs, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outs {
		got, err := os.ReadFile(o.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), o.code) {
			t.Errorf("%s is not what the built-in table generates; run `go generate ./internal/builtingen`", o.path)
		}
	}
}
