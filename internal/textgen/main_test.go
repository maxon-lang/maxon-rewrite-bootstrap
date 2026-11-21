package main

import (
	"bytes"
	"os"
	"testing"
)

func TestGeneratedFilesAreCurrent(t *testing.T) {
	want, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range outputs {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), []byte(want)) {
			t.Errorf("%s is not what the Unicode data generates; run `go generate ./internal/textgen`", path)
		}
	}
}
