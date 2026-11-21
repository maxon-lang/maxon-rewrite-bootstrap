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

func readMessages(t *testing.T, rows []row) []message {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(diagDir, messageTableFile))
	if err != nil {
		t.Fatal(err)
	}
	messages, err := parseMessages(string(src), rows)
	if err != nil {
		t.Fatal(err)
	}
	return messages
}

func TestGeneratedFilesAreCurrent(t *testing.T) {
	rows := readTable(t)
	outs, err := generate(rows, readMessages(t, rows))
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

func TestParseMessagesRefusesBadRows(t *testing.T) {
	codes := []row{{"ErrA", "E2001", noMaxonName, "m"}}
	for name, tc := range map[string]struct{ src, want string }{
		"columns":     {"f\tErrA\n", "3 tab-separated columns"},
		"name":        {"F\tErrA\tt\n", "not a function name"},
		"duplicate":   {"f\tErrA\tt\nf\tErrA\tt\n", "declared twice"},
		"constant":    {"f\tErrB\tt\n", "not a constant of"},
		"template":    {"f\tErrA\t\n", "no template"},
		"quote":       {"f\tErrA\tsay \"x\"\n", "quote or a backslash"},
		"keyword":     {"f\tErrA\t{func}\n", "not a parameter name"},
		"placeholder": {"f\tErrA\t{Name}\n", "not a parameter name"},
		"brace":       {"f\tErrA\tuse {x} and {\n", "not part of a {placeholder}"},
		"empty":       {"# only a comment\n", "no rows"},
		"ok":          {"f\tErrA\t'{a}' and {b} and {a}\n", ""},
	} {
		_, err := parseMessages(tc.src, codes)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: %v", name, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: error %v, want one naming %q", name, err, tc.want)
		}
	}
}

func TestMessageFunctions(t *testing.T) {
	messages, err := parseMessages("f\tErrA\tgot '{a}' for {b}, not {a}\ng\tErrA\tnone\n", []row{{"ErrA", "E2001", noMaxonName, "m"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(messages[0].params, ","); got != "a,b" {
		t.Errorf("parameters %q, want a,b: each placeholder once, in the order of first use", got)
	}
	if got := emitGoMessages(messages); !strings.Contains(got, "func F(a, b string) string {\n\treturn \"got '\" + a + \"' for \" + b + \", not \" + a\n}") || !strings.Contains(got, "func G() string") {
		t.Errorf("Go messages:\n%s", got)
	}
	if got := emitMaxonMessages(messages); !strings.Contains(got, "export function f(a String, b String) returns String\n\treturn \"got '{a}' for {b}, not {a}\"\nend 'f'") || !strings.Contains(got, "export function g() returns String") {
		t.Errorf("Maxon messages:\n%s", got)
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
