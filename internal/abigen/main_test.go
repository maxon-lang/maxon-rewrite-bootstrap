package main

import (
	"bytes"
	"os"
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
