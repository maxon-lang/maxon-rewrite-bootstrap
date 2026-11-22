package main

import (
	"bytes"
	"os"
	"testing"

	"minimaxon/internal/arm64gen/arm64table"
)

func TestGeneratedFileIsCurrent(t *testing.T) {
	code, err := arm64table.Generate()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(maxonFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), []byte(code)) {
		t.Errorf("%s is not what the instruction table generates; run `go generate ./internal/arm64gen`", maxonFile)
	}
}
