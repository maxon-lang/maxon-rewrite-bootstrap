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
		"p.lists.string.push(p.text(p.expect(TName, \"a name\")))",
		"n.Tags = p.lists.string.take(mark_n_Tags)",
		"n := p.arena.NewSquare()",
		"func (p *Parser) parseShapePrec(minPrec int) ast.Shape {",
		"n.R = p.parseShapePrec(2)",
	} {
		if !strings.Contains(parser, want) {
			t.Errorf("parser lacks %q:\n%s", want, parser)
		}
	}
}

func TestJoinsComeFromTokenCommandsAndCarets(t *testing.T) {
	outs := generateTiny(t, `grammar T;
call : callee=Name ^'(' arg=neg ')' ;
neg : op=('-'^ | 'not') v=Integer ;
Name : [a-z]+ ;
Integer : [0-9]+ ;
LParen : '(' -> noSpaceAfter ;
RParen : ')' -> noSpaceBefore ;
Minus : '-' ;
`)
	parser, tokens := outs[parserFile], outs[tokensFile]
	for _, want := range []string{
		"p.joinNext = true\n\tp.expect(TLParen",
		"joins := p.at(TMinus)",
		"p.joinNext = p.joinNext || joins",
	} {
		if !strings.Contains(parser, want) {
			t.Errorf("parser lacks %q:\n%s", want, parser)
		}
	}
	for _, want := range []string{
		"var tokenNoSpaceBefore = [numTokens]bool{TRParen: true}",
		"var tokenNoSpaceAfter = [numTokens]bool{TLParen: true}",
	} {
		if !strings.Contains(tokens, want) {
			t.Errorf("tokens lack %q:\n%s", want, tokens)
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
		"grammar G;\nh returns [int v] : w='p' ;\n":   "g.g4:2: helper rule 'h' returns 'v'; label 'w' names nothing",
		"grammar G;\nA : 'x' ;\nB : 'x' ;\n":          "g.g4:3: token 'B' can never be matched: an earlier token always wins",
		"grammar G;\nA : 'x'* ;\n":                    "g.g4:2: token 'A' matches the empty string, so the lexer would never move on",
		"grammar G;\nA : 'x' .*? 'y' ;\n":             "g.g4:2: the DFA lexer takes the longest match; a non-greedy loop cannot be compiled",
		"grammar G;\na : W ;\nW : ' ' -> skip ;\n":    "g.g4:2: token 'W' is skipped, so no rule can use it",
		"grammar G;\na : b^ ;\nb : 'x' ;\n":           "g.g4:2: ^ joins a token to its neighbour; put it on a literal or a token",
		"grammar G;\nA : 'x' -> more ;\n":             "g.g4:2: token 'A' has the command 'more'; a token takes skip, scan, noSpaceBefore, noSpaceAfter or noSpace",
	}
	for src, want := range cases {
		_, err := generate("g.g4", []byte(src))
		if err == nil || err.Error() != want {
			t.Errorf("%q: got %v, want %q", src, err, want)
		}
	}
}

// run matches the longest token at the start of s the way the generated lexer does: it returns the accepted
// token's name and length, or the incomplete message of the state the match stopped in.
func (a *dfa) run(s string) (string, int, string) {
	state, i := 1, 0
	for i < len(s) && a.next[state][a.classes[s[i]]] != 0 {
		state = a.next[state][a.classes[s[i]]]
		i++
	}
	if t := a.accept[state]; t != noAccept {
		return a.tokens[t].name, i, ""
	}
	return "", i, a.incomplete[state]
}

func TestDfaTakesTheLongestMatchAndNeverBacksOff(t *testing.T) {
	g := parseGrammar("g.g4", `grammar G;
a : 'if' 'in' n=Name i=Integer '=' '==' ;
/** a name */
Name : [a-z]+ ;
/** an integer */
Integer : [0-9]+ | '0x' [0-9a-f]+ ;
Assign : '=' ;
EqEq : '==' ;
/** a comment */
Comment : '/*' (~'*' | '*'+ ~[*/])* '*'+ '/' -> skip ;
Slash : '/' ;
`)
	a := buildDfa(buildModel(g))
	cases := []struct {
		in, token  string
		n          int
		incomplete string
	}{
		{"if ", "TKw_if", 2, ""},
		{"iffy", "TName", 4, ""},
		{"i", "TName", 1, ""},
		{"===", "TEqEq", 2, ""},
		{"0x1f;", "TInteger", 4, ""},
		{"0x;", "", 2, "incomplete integer"},
		{"/* a **/b", "TComment", 8, ""},
		{"/* open", "", 7, "incomplete comment"},
		{"/ x", "TSlash", 1, ""},
	}
	for _, c := range cases {
		token, n, incomplete := a.run(c.in)
		if token != c.token || n != c.n || incomplete != c.incomplete {
			t.Errorf("%q: got %q %d %q, want %q %d %q", c.in, token, n, incomplete, c.token, c.n, c.incomplete)
		}
	}
}

func TestAListIsCopiedOutOnceAndNeverOverwritesAnAssignedField(t *testing.T) {
	parser := generateTiny(t, `grammar T;
holder : 'if' (rest+=Name | 'else' rest=names) ;
names returns [[]string list] : list+=Name (',' list+=Name)* ;
Name : [a-z]+ ;
Comma : ',' ;
`)[parserFile]
	for _, want := range []string{
		"mark_n_Rest := p.lists.string.mark()",
		"if p.lists.string.pushedSince(mark_n_Rest) {\n\t\tn.Rest = p.lists.string.take(mark_n_Rest)\n\t}",
		"mark_list := p.lists.string.mark()",
		"list = p.lists.string.take(mark_list)",
		"type listStacks struct {\n\tstring listStack[string]\n}",
	} {
		if !strings.Contains(parser, want) {
			t.Errorf("parser lacks %q:\n%s", want, parser)
		}
	}
}
