package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func readTable(t *testing.T) []row {
	t.Helper()
	src, err := os.ReadFile(tableFile)
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
	src, err := os.ReadFile(messageTableFile)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := parseMessages(string(src), rows)
	if err != nil {
		t.Fatal(err)
	}
	return messages
}

func readSkips(t *testing.T, rows []row) []skipRow {
	t.Helper()
	src, err := os.ReadFile(skipTableFile)
	if err != nil {
		t.Fatal(err)
	}
	skips, err := parseSkips(string(src), rows)
	if err != nil {
		t.Fatal(err)
	}
	return skips
}

func TestGeneratedFilesAreCurrent(t *testing.T) {
	rows := readTable(t)
	for _, o := range generate(rows, readMessages(t, rows), readSkips(t, rows)) {
		got, err := os.ReadFile(o.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), o.code) {
			t.Errorf("%s is not what %s generates; run `go generate ./internal/diaggen`", o.path, tableFile)
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

func TestParseSkipsRefusesBadRows(t *testing.T) {
	codes := []row{{"ErrA", "E2001", noMaxonName, "m"}, {"ErrB", "E2002", noMaxonName, "m"}}
	const declared = "code\tE3223\tmeaning\n"
	for name, tc := range map[string]struct{ src, want string }{
		"kind":              {"other\tErrA\tErrB\tr\n", "want a code row"},
		"columns":           {"skip\tErrA\tErrB\n", "want a code row"},
		"code":              {"code\t3223\tm\n", "not a code"},
		"in the table":      {"code\tE2001\tm\n", "name its constant"},
		"twice":             {declared + declared, "declared twice"},
		"no meaning":        {"code\tE3223\t\n", "no meaning"},
		"unknown":           {"skip\tErrC\tErrB\tr\n", "neither a constant"},
		"undeclared":        {"skip\tE3223\tErrB\tr\n", "neither a constant"},
		"reported":          {declared + "skip\tErrA\tE3223\tr\n", "neither a constant"},
		"reason":            {"skip\tErrA\tErrB\t\n", "reason is empty"},
		"quote":             {"skip\tErrA\tErrB\tsay \"x\"\n", "reason is empty"},
		"empty":             {"# only a comment\n", "no skip rows"},
		"ok":                {declared + "skip\tErrA,E3223\tErrB\tr\n", ""},
		"run maxon":         {declared + "run\tE3223\tr\n", "neither a constant"},
		"expect ok":         {declared + "expect\tErrA,E3223\tr\n", ""},
		"run ok":            {"run\tErrB\tr\n", ""},
		"test ok":           {"test\ta.md\tone-test\tr\n", ""},
		"test file":         {"test\ta.txt\tone-test\tr\n", "not the name of a spec file"},
		"test path":         {"test\tspecs/a.md\tone-test\tr\n", "not the name of a spec file"},
		"test name":         {"test\ta.md\t\tr\n", "test's name is empty"},
		"test twice":        {"test\ta.md\tone-test\tr\ntest\ta.md\tone-test\ts\n", "named twice"},
		"test reason":       {"test\ta.md\tone-test\t\n", "reason is empty"},
		"manual ok":         {"manual\ta.md\tone-test\tr\n", ""},
		"manual file":       {"manual\ta.txt\tone-test\tr\n", "not the name of a spec file"},
		"manual twice":      {"manual\ta.md\tone-test\tr\nmanual\ta.md\tone-test\ts\n", "named twice"},
		"manual and a skip": {"test\ta.md\tone-test\tr\nmanual\ta.md\tone-test\ts\n", ""},
	} {
		_, err := parseSkips(tc.src, codes)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: %v", name, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: error %v, want one naming %q", name, err, tc.want)
		}
	}
}

func TestSkipTablePairsEveryExpectedWithEveryReported(t *testing.T) {
	codes := []row{{"ErrA", "E2001", noMaxonName, "m"}, {"ErrB", "E2002", noMaxonName, "m"}}
	skips, err := parseSkips("code\tE3223\tm\nskip\tErrA,E3223\tErrA,ErrB\twhy\nexpect\tE3223\tso\nrun\tErrB\tthus\ntest\ta.md\tone-test\tsince\nmanual\tb.md\tslow\tlong\n", codes)
	if err != nil {
		t.Fatal(err)
	}
	got := emitMaxonSkips(skips)
	for _, want := range []string{
		`CodeSkip.make(diag.errA.clone(), reported: diag.errA.clone(), line: "<!-- skip: why -->\n")`,
		`CodeSkip.make(diag.errA.clone(), reported: diag.errB.clone(), line: "<!-- skip: why -->\n")`,
		`CodeSkip.make("E3223", reported: diag.errA.clone(), line: "<!-- skip: why -->\n")`,
		`CodeSkip.make("E3223", reported: diag.errB.clone(), line: "<!-- skip: why -->\n")`,
		`CodeSkip.expecting("E3223", line: "<!-- skip: so -->\n")`,
		`CodeSkip.refusing(diag.errB.clone(), line: "<!-- skip: thus -->\n")`,
		`CodeSkip.naming("a.md", test: "one-test", line: "<!-- skip: since -->\n")`,
		`CodeSkip.byHand("b.md", test: "slow", line: "<!-- manual: long -->\n")`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated table lacks %s:\n%s", want, got)
		}
	}
}
