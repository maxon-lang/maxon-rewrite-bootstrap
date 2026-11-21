// Command grammargen generates the parser, the token kinds, and the AST nodes, keywords and visitors from
// internal/syntax/Maxon.g4. `go generate ./internal/syntax` runs it in that directory.
package main

import (
	"fmt"
	"go/format"
	"os"
)

const (
	grammarFile = "Maxon.g4"
	parserFile  = "parser_gen.go"
	tokensFile  = "tokens_gen.go"
	nodesFile   = "../ast/nodes_gen.go"
)

type output struct {
	path string
	code []byte
}

func main() {
	src, err := os.ReadFile(grammarFile)
	if err != nil {
		fail(err)
	}
	outs, err := generate(grammarFile, src)
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
	for _, o := range []struct {
		path string
		code string
	}{
		{nodesFile, emitNodes(m)},
		{tokensFile, emitTokens(m)},
		{parserFile, emitParser(m, a)},
	} {
		code, err := format.Source([]byte(o.code))
		if err != nil {
			return nil, fmt.Errorf("%s does not parse as Go: %w\n%s", o.path, err, o.code)
		}
		outs = append(outs, output{o.path, code})
	}
	outs = append(outs, output{maxonNodesFile, []byte(emitMaxonNodes(m))})
	return outs, nil
}
