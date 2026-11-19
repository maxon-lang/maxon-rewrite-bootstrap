// Package srctest reads the Maxon source that unit tests keep in testdata directories. Source lives in .maxon files
// rather than in Go strings so that minimaxon rewrite reaches it when the language changes.
package srctest

import (
	"os"
	"strings"
	"testing"
)

const (
	caseMarker  = "// case: "
	errorMarker = "// expect-error: "
	blankLine   = "\n"
)

// Case is one program of a file of cases.
type Case struct {
	Name string
	// Source is the program, with the declarations that precede it in the cases' prelude files.
	Source string
	// Codes are the diagnostics the program must produce, in order; none means it must be accepted.
	Codes []string
}

// Read returns the files at paths joined by a blank line, which is how the declarations of one file precede those of
// the next: a file ends with its last declaration and a newline, and holds nothing but whole declarations.
func Read(t testing.TB, paths ...string) string {
	t.Helper()
	texts := make([]string, len(paths))
	for i, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		texts[i] = string(data)
	}
	return strings.Join(texts, blankLine)
}

// Cases splits the file at path into its cases. A case starts at a "// case: name" line and runs to the next one, and
// may hold "// expect-error: CODE" lines. Each case's source starts with the prelude files, which are read as by Read.
func Cases(t testing.TB, path string, prelude ...string) []Case {
	t.Helper()
	head := ""
	if len(prelude) > 0 {
		head = Read(t, prelude...) + blankLine
	}
	var cases []Case
	var body []string
	flush := func() {
		if len(cases) == 0 {
			return
		}
		c := &cases[len(cases)-1]
		for len(body) > 0 && body[len(body)-1] == "" {
			body = body[:len(body)-1]
		}
		c.Source = head + strings.Join(body, "\n") + "\n"
		body = nil
	}
	for line := range strings.SplitSeq(Read(t, path), "\n") {
		switch name, ok := strings.CutPrefix(line, caseMarker); {
		case ok:
			flush()
			cases = append(cases, Case{Name: name})
		case len(cases) == 0:
			if line != "" {
				t.Fatalf("%s: text before the first %q line", path, strings.TrimSpace(caseMarker))
			}
		default:
			if code, ok := strings.CutPrefix(line, errorMarker); ok {
				cases[len(cases)-1].Codes = append(cases[len(cases)-1].Codes, code)
			}
			body = append(body, line)
		}
	}
	flush()
	if len(cases) == 0 {
		t.Fatalf("%s: no %q line", path, strings.TrimSpace(caseMarker))
	}
	return cases
}
