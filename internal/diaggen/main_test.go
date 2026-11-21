package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var diagDir = filepath.Join("..", "diag")

func readTable(t *testing.T) []row {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(diagDir, tableFile))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := parseTable(string(src))
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestGeneratedFilesAreCurrent(t *testing.T) {
	outs, err := generate(readTable(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outs {
		got, err := os.ReadFile(filepath.Join(diagDir, o.path))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), o.code) {
			t.Errorf("%s is not what %s generates; run `go generate ./internal/diag`", o.path, tableFile)
		}
	}
}

func TestParseTableRefusesBadRows(t *testing.T) {
	for name, tc := range map[string]struct{ src, want string }{
		"columns":    {"ErrA\tE2001\t-\n", "4 tab-separated columns"},
		"constant":   {"A\tE2001\t-\tm\n", "not a constant name"},
		"code":       {"ErrA\t2001\t-\tm\n", "not a code"},
		"maxon":      {"ErrA\tE2001\tNo\tm\n", "neither a case name"},
		"meaning":    {"ErrA\tE2001\t-\t\n", "no meaning"},
		"duplicate":  {"ErrA\tE2001\t-\tm\nErrA\tE2002\t-\tm\n", "declared twice"},
		"order":      {"ErrA\tE2002\t-\tm\nErrB\tE2001\t-\tm\n", "ascending order"},
		"same code":  {"ErrA\tE2001\t-\tm\nErrB\tE2001\t-\tm\n", "ascending order"},
		"empty":      {"# only a comment\n", "no rows"},
		"crlf":       {"ErrA\tE2001\t-\tm\r\nErrB\tE2002\t-\tm\r\n", ""},
		"commentary": {"# note\n\nErrA\tE2001\t-\tm\n", ""},
	} {
		_, err := parseTable(tc.src)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: %v", name, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: error %v, want one naming %q", name, err, tc.want)
		}
	}
}

const registry = `export enum ErrorCode
	// first
	lexerUnterminatedString = "E1002"

	// second
	parserUnexpectedToken = "E2001"
end 'ErrorCode'
`

func TestCheckRegistry(t *testing.T) {
	for name, tc := range map[string]struct {
		rows []row
		want string
	}{
		"agrees":         {[]row{{"ErrA", "E2001", "parserUnexpectedToken", "m"}, {"ErrB", "E3900", noMaxonName, "m"}}, ""},
		"wrong number":   {[]row{{"ErrA", "E2002", "parserUnexpectedToken", "m"}}, "is E2001, not E2002"},
		"unknown name":   {[]row{{"ErrA", "E2001", "parserNothing", "m"}}, "maxon has no parserNothing"},
		"number is used": {[]row{{"ErrA", "E1002", noMaxonName, "m"}}, "is maxon's lexerUnterminatedString"},
	} {
		err := checkRegistry(tc.rows, registry)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: %v", name, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: error %v, want one naming %q", name, err, tc.want)
		}
	}
	if err := checkRegistry(nil, "nothing here"); err == nil {
		t.Error("a registry without cases was accepted")
	}
}
