package main

import (
	"bytes"
	"os"
	"testing"

	"minimaxon/internal/targetgen/target"
)

func TestGeneratedFilesAreCurrent(t *testing.T) {
	for _, tg := range target.Targets {
		code, err := generate(tg)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(outputPath(tg))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), []byte(code)) {
			t.Errorf("%s is not what the target table generates; run `go generate ./internal/targetgen`", outputPath(tg))
		}
	}
}
