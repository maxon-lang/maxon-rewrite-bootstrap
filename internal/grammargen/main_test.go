package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var syntaxDir = filepath.Join("..", "syntax")

func TestGeneratedFilesAreCurrent(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(syntaxDir, grammarFile))
	if err != nil {
		t.Fatal(err)
	}
	outs, err := generate(grammarFile, src)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outs {
		got, err := os.ReadFile(filepath.Join(syntaxDir, o.path))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), o.code) {
			t.Errorf("%s is not what %s generates; run `go generate ./internal/syntax`", o.path, grammarFile)
		}
	}
}

const tiny = `grammar Tiny;

/** a shape */
shape
    : square
    | circle
    | l=shape '+' r=shape # Union
    ;

@ast::ShapeBase {
	Area I128 = Zero
}

square : 'square' side=Integer named='named'? tags+=Name* ;

@ast::Square {
	Side I128
}

circle : 'circle' radius=Integer ;

/** an integer */
Integer : [0-9]+ ;
/** a name */
Name : [a-z]+ ;
Plus : '+' ;
`

func generateTiny(t *testing.T, src string) map[string]string {
	t.Helper()
	outs, err := generate("tiny.g4", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]string{}
	for _, o := range outs {
		byPath[o.path] = string(o.code)
	}
	return byPath
}

func TestNodesTakeTheirFieldsFromLabels(t *testing.T) {
	nodes := generateTiny(t, tiny)[nodesFile]
	for _, want := range []string{
		"type ShapeBase struct {\n\tPos\n\tArea I128\n}",
		"type Square struct {\n\tShapeBase\n\tSide  I128\n\tNamed bool\n\tTags  []string\n}",
		"type Union struct {\n\tShapeBase\n\tL Shape\n\tR Shape\n}",
		"n.Area = Zero",
		"VisitUnion(n *Union, s S) R",
		`"square": true`,
	} {
		if !strings.Contains(nodes, want) {
			t.Errorf("nodes lack %q:\n%s", want, nodes)
		}
	}
}

func TestParserConvertsAndClimbs(t *testing.T) {
	parser := generateTiny(t, tiny)[parserFile]
	for _, want := range []string{
		"n.Side = p.squareSide(p.expect(TInteger, \"an integer\"))",
		"n.Named = true",
		"n.Tags = append(n.Tags, p.text(p.expect(TName, \"a name\")))",
		"func (p *Parser) parseShapePrec(minPrec int) ast.Shape {",
		"n.R = p.parseShapePrec(2)",
	} {
		if !strings.Contains(parser, want) {
			t.Errorf("parser lacks %q:\n%s", want, parser)
		}
	}
}

func TestSharedLeadingRuleIsParsedOnce(t *testing.T) {
	parser := generateTiny(t, `grammar T;
stmt : assign | call ;
assign : target=expr '=' value=expr ;
call : e=expr ;
expr : name=Name ;
Name : [a-z]+ ;
Assign : '=' ;
`)[parserFile]
	for _, want := range []string{"first := p.parseExpr()", "return p.parseAssignAfter(t, first)", "return p.parseCallAfter(t, first)"} {
		if !strings.Contains(parser, want) {
			t.Errorf("parser lacks %q:\n%s", want, parser)
		}
	}
}

func TestGrammarErrorsNameTheLine(t *testing.T) {
	cases := map[string]string{
		"grammar G;\na : b ;\n":                       "g.g4:2: rule 'b' is not declared",
		"grammar G;\na : 'x' ;\na : 'y' ;\n":          "g.g4:3: rule 'a' is declared twice",
		"grammar G;\na : '$' ;\n":                     "g.g4:2: '$' is neither a keyword nor the text of a token",
		"grammar G;\na : x='p' | x=b ;\nb : 'q' ;\n":  "g.g4:2: label 'x' of A is *B here and bool elsewhere; give its type in @ast::A",
		"grammar G;\na : 'p' | 'p' ;\n":               "g.g4:2: alternative 2 can never be chosen: every way it can start, an earlier alternative starts",
		"grammar G;\na : x=a 'p' ;\n":                 "g.g4:2: only a set rule may be left-recursive, and 'a' is not one",
		"grammar G;\na : 'p' ;\n@ast::Nothing {\n}\n": "g.g4:3: @ast::Nothing names no node and no set base",
		"grammar G;\nT : 'abc' ;\n":                   "g.g4:2: the lexer matches punctuation of one or two characters, not 'abc'",
		"grammar G;\nh returns [int v] : w='p' ;\n":   "g.g4:2: helper rule 'h' returns 'v'; label 'w' names nothing",
	}
	for src, want := range cases {
		_, err := generate("g.g4", []byte(src))
		if err == nil || err.Error() != want {
			t.Errorf("%q: got %v, want %q", src, err, want)
		}
	}
}
