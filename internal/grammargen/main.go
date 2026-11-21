// Command grammargen generates the parser, the token kinds, and the AST nodes, keywords and visitors of self/ from
// self/syntax/Maxon.g4, and the keywords cmd/convert reserves. `go generate ./internal/grammargen` runs it.
package main

//go:generate go run .

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
)

const grammarFile = "../../self/syntax/Maxon.g4"

// grammarName is how the generated files name the grammar, whatever directory they are generated from.
var grammarName = filepath.Base(grammarFile)

type output struct {
	path string
	code []byte
}

func main() {
	src, err := os.ReadFile(grammarFile)
	if err != nil {
		fail(err)
	}
	outs, err := generate(grammarName, src)
	if err != nil {
		fail(err)
	}
	for _, o := range outs {
		if err := os.WriteFile(o.path, o.code, 0o644); err != nil {
			fail(err)
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "grammargen:", err)
	os.Exit(1)
}

func generate(file string, src []byte) (outs []output, err error) {
	defer func() {
		if r := recover(); r != nil {
			ge, ok := r.(*grammarError)
			if !ok {
				panic(r)
			}
			err = ge
		}
	}()
	g := parseGrammar(file, string(src))
	m := buildModel(g)
	a := analyze(m)
	reserved, err := format.Source([]byte(emitReserved(m)))
	if err != nil {
		return nil, fmt.Errorf("%s does not parse as Go: %w", reservedFile, err)
	}
	return []output{
		{maxonNodesFile, []byte(emitMaxonNodes(m))},
		{maxonTokensFile, []byte(emitMaxonTokens(m))},
		{maxonParserFile, []byte(emitMaxonParser(m, a))},
		{reservedFile, reserved},
	}, nil
}
