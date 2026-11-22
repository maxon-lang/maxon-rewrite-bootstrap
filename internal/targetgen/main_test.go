package main

import (
	"bytes"
	"os"
	"testing"
)

func TestGeneratedFilesAreCurrent(t *testing.T) {
	outs, err := generateAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outs {
		got, err := os.ReadFile(o.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), []byte(o.code)) {
			t.Errorf("%s is not what the target table generates; run `go generate ./internal/targetgen`", o.path)
		}
	}
}
