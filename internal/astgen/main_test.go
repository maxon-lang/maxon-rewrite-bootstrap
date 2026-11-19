package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var astDir = filepath.Join("..", "ast")

func TestGeneratedFileIsCurrent(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(astDir, specFile))
	if err != nil {
		t.Fatal(err)
	}
	want, err := generate(specFile, src)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(astDir, outputFile))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
		t.Fatalf("internal/ast/%s is not what %s generates; run `go generate ./internal/ast`", outputFile, specFile)
	}
}

func TestVisitorsListEveryMember(t *testing.T) {
	out, err := generate("test.spec", []byte("set Shape shape\n\nnode Square in Shape\n\tSide int\n\nnode Circle in Shape\n\tRadius int\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"type Square struct {\n\tSide int\n}",
		"VisitCircle(n *Circle)\n",
		"VisitCircle(n *Circle) R\n",
		"VisitCircle(n *Circle, s S) R\n",
		"func VisitShapeState[S, R any](v ShapeStateVisitor[S, R], n Shape, s S) R {",
		"_ Shape = (*Circle)(nil)",
		"panic(diag.Internal(\"unhandled shape %T\", n))",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("generated code lacks %q:\n%s", want, out)
		}
	}
}

func TestSpecErrorsNameTheLine(t *testing.T) {
	cases := map[string]string{
		"node A in Missing\n":          "test.spec:1: 'Missing' is not a set declared above",
		"node A\nnode A\n":             "test.spec:2: 'A' is declared twice",
		"set S thing\n":                "test.spec: set 'S' has no members",
		"\tX int\n":                    "test.spec:1: an indented line belongs to a node or an enum",
		"node A\n\tX int extra\n":      "test.spec:2: a field is '<Name> <Type>' or a bare type to embed",
		"thing A\n":                    "test.spec:1: expected 'set', 'enum' or 'node', found 'thing'",
		"enum E\n":                     "test.spec: enum 'E' has no values",
		"set S thing\nnode A in S X\n": "test.spec:2: 'X' is not a set declared above",
	}
	for src, want := range cases {
		_, err := generate("test.spec", []byte(src))
		if err == nil {
			t.Errorf("%q: no error, want %q", src, want)
			continue
		}
		if err.Error() != want {
			t.Errorf("%q: got %q, want %q", src, err.Error(), want)
		}
	}
}
