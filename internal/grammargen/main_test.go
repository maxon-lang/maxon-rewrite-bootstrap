package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestGeneratedFilesAreCurrent(t *testing.T) {
	src, err := os.ReadFile(grammarFile)
	if err != nil {
		t.Fatal(err)
	}
	outs, err := generate(grammarName, src)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outs {
		got, err := os.ReadFile(o.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), o.code) {
			t.Errorf("%s is not what %s generates; run `go generate ./internal/grammargen`", o.path, grammarName)
		}
	}
}

const tiny = `grammar Tiny;

@ast::Structs { Circle }

/** a shape */
shape
    : square
    | l=shape '+' r=shape # Union
    ;

square : 'square' side=Integer named='named'? tags+=Name* op=('big' | 'small')? ;

@ast::Square {
	Side I128
}

circle : 'circle' radius=Integer ;

@ast::Circle {
	Radius I128
	Area I128 = Zero
}

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
	nodes := generateTiny(t, tiny)[maxonNodesFile]
	for _, want := range []string{
		// A struct keeps the fields the parse fills, and what it starts with is its number.
		"export type Circle\n\texport var id as NodeID\n\texport var radius as I128\n",
		"export function newCircle(mutates t Tree, start diag.U32) returns Circle\n",
		// A record has an accessor for each field, and a constructor that makes its record.
		"export function newSquare(mutates t Tree, start diag.U32) returns NodeID\n\tlet id = t.alloc(NodeKind.square, start: start)\n",
		"export function squareSide(t Tree, id NodeID) returns I128\n",
		"export function squareNamed(t Tree, id NodeID) returns bool\n\treturn (t.records[id as Count].flags and (1 as Byte)) != 0\n",
		"export function squareTagsRef(t Tree, id NodeID) returns diag.U32\n\treturn t.blockWordAt(id, offset: 1 as Count)\n",
		"export function squareOp(t Tree, id NodeID) returns String\n\tlet code = (t.records[id as Count].aux and (255 as U16)) as Count\n",
		"export function unionL(t Tree, id NodeID) returns NodeID\n",
		"\t\t\"square\" or\n",
	} {
		if !strings.Contains(nodes, want) {
			t.Errorf("nodes lack %q:\n%s", want, nodes)
		}
	}
}

func TestARecordKeepsItsFirstWordAndABlockForTheRest(t *testing.T) {
	nodes := generateTiny(t, `grammar T;
@ast::Structs { }
three : 'three' a=Name b=Name c=Name ;
two : 'two' a=Name b=Name ;
Name : [a-z]+ ;
`)[maxonNodesFile]
	for _, want := range []string{
		"\tlet blk = t.reserve(2 as Count)\n",
		"export function threeA(t Tree, id NodeID) returns borrowed String\n\treturn t.nameAt(t.records[id as Count].a)\n",
		"export function threeC(t Tree, id NodeID) returns borrowed String\n\treturn t.nameAt(t.blockWordAt(id, offset: 1 as Count))\n",
		"export function twoB(t Tree, id NodeID) returns borrowed String\n\treturn t.nameAt(t.records[id as Count].b)\n",
	} {
		if !strings.Contains(nodes, want) {
			t.Errorf("nodes lack %q:\n%s", want, nodes)
		}
	}
	if strings.Contains(nodes, "let id = t.alloc(NodeKind.two, start: start)\n\tlet blk = t.reserve") {
		t.Errorf("a record of two words reserves a block:\n%s", nodes)
	}
}

func TestSlotFieldsAreNumberedAfterTheParseFromTheirCounter(t *testing.T) {
	nodes := generateTiny(t, `grammar T;
@ast::Structs { Circle }
shape
    : square
    | triangle
    ;
square : 'square' side=Name ;
triangle : 'triangle' side=Name ;
@ast::Square {
	Info Slot[Sides]
}
circle : 'circle' radius=Name ;
@ast::Circle {
	Info Slot[Sides]
	Ring Slot[Rings]
}
Name : [a-z]+ ;
`)[maxonNodesFile]
	for _, want := range []string{
		"export type Circle\n\texport var id as NodeID\n\texport var radius as String\n",
		"export type Slots\n\texport var sidesSlots as diag.U32\n\texport var ringsSlots as diag.U32\n",
		"export function numberSlots(tree Tree, mutates slots U32List) returns Slots\n",
		"\t\t\tcount.sidesSlots = try count.sidesSlots + 1 otherwise panic(\"slot overflow\")\n\t\t\tslots[id] = count.sidesSlots\n",
		"\t\t\tcount.ringsSlots = try count.ringsSlots + 1 otherwise panic(\"slot overflow\")\n\t\t\tslots[id] = count.ringsSlots\n",
	} {
		if !strings.Contains(nodes, want) {
			t.Errorf("nodes lack %q:\n%s", want, nodes)
		}
	}
}

func TestParserConvertsAndClimbs(t *testing.T) {
	parser := generateTiny(t, tiny)[maxonParserFile]
	for _, want := range []string{
		"\tlet v1 = try p.expect(TokenKind.integer, what: \"an integer\")\n\tlet v2 = try squareSide(p, t: v1)\n\tast.setSquareSide(p.tree, id: n, value: v2)\n",
		"\t\tast.setSquareNamed(p.tree, id: n)\n",
		"\t\tlet v4 = p.textRef(v3)\n\t\tp.lists.ids_Square_Tags.push(v4)\n",
		"\tlet v7 = takeIDs(p.tree, stack: p.lists.ids_Square_Tags, mark: mark_n_Tags)\n\tast.setSquareTagsRef(p.tree, id: n, ref: v7)\n",
		"\t\tlet v5 = try p.next()\n\t\tlet v6 = p.text(v5)\n\t\tast.setSquareOp(p.tree, id: n, text: v6)\n",
		"\tlet v2 = try circleRadius(p, t: v1)\n\tn.radius = v2\n",
		"\tlet n = ast.newSquare(p.tree, start: t)\n",
		"function parseShapePrec(mutates p Parser, minPrec Count) returns ast.NodeID throws SyntaxError\n",
		"\t\t\tlet v3 = try parseShapePrec(p, minPrec: 2)\n\t\t\tast.setUnionR(p.tree, id: n, value: v3)\n\t\t\tleft = n\n",
		"\t\tlet v1 = try parseSquare(p)\n\t\tleft = v1\n",
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
@ast::Neg {
	V I128
}
Name : [a-z]+ ;
Integer : [0-9]+ ;
LParen : '(' -> noSpaceAfter ;
RParen : ')' -> noSpaceBefore ;
Minus : '-' ;
`)
	parser, tokens := outs[maxonParserFile], outs[maxonTokensFile]
	for _, want := range []string{
		"\tp.joinNext = true\n\t_ = try p.expect(TokenKind.lParen",
		"\tlet joins1 = p.at(TokenKind.minus)\n",
		"\tp.joinNext = p.joinNext or joins1\n",
	} {
		if !strings.Contains(parser, want) {
			t.Errorf("parser lacks %q:\n%s", want, parser)
		}
	}
	for _, want := range []string{
		"export let tokenNoSpaceBefore = \"0001000\"\n",
		"export let tokenNoSpaceAfter = \"0010000\"\n",
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
`)[maxonParserFile]
	for _, want := range []string{
		"\t\tlet first = try parseExpr(p)\n",
		"try parseAssignAfter(p, t: t, first: first)\n",
		"try parseCallAfter(p, t: t, first: first)\n",
	} {
		if !strings.Contains(parser, want) {
			t.Errorf("parser lacks %q:\n%s", want, parser)
		}
	}
}

func TestAPredicateGuardsAnOptionalGroupWithAParserMethod(t *testing.T) {
	parser := generateTiny(t, `grammar T;
call : callee=Name ({atBinding}? 'with' bound=Name)? ;
Name : [a-z]+ ;
`)[maxonParserFile]
	want := "p.atBinding()"
	if !strings.Contains(parser, want) {
		t.Errorf("parser lacks %q:\n%s", want, parser)
	}
}

func TestGrammarErrorsNameTheLine(t *testing.T) {
	cases := map[string]string{
		"grammar G;\na : b ;\n":                             "g.g4:2: rule 'b' is not declared",
		"grammar G;\na : 'x' ;\na : 'y' ;\n":                "g.g4:3: rule 'a' is declared twice",
		"grammar G;\na : '$' ;\n":                           "g.g4:2: '$' is neither a keyword nor the text of a token",
		"grammar G;\na : x='p' | x=b ;\nb : 'q' ;\n":        "g.g4:2: label 'x' of A is B here and bool elsewhere; give its type in @ast::A",
		"grammar G;\na : 'p' | 'p' ;\n":                     "g.g4:2: alternative 2 can never be chosen: every way it can start, an earlier alternative starts",
		"grammar G;\na : x=a 'p' ;\n":                       "g.g4:2: only a set rule may be left-recursive, and 'a' is not one",
		"grammar G;\na : 'p' ;\n@ast::Nothing {\n}\n":       "g.g4:3: @ast::Nothing names no node and no set base",
		"grammar G;\na : 'p' ;\n@ast::A {\n\tX Slot[]\n}\n": "g.g4:4: the type of field X is Slot[]; a slot is written Slot[Counter]",
		"grammar G;\nh returns [string v] : w='p' ;\n":      "g.g4:2: helper rule 'h' returns 'v'; label 'w' names nothing",
		"grammar G;\nA : 'x' ;\nB : 'x' ;\n":                "g.g4:3: token 'B' can never be matched: an earlier token always wins",
		"grammar G;\nA : 'x'* ;\n":                          "g.g4:2: token 'A' matches the empty string, so the lexer would never move on",
		"grammar G;\nA : 'x' .*? 'y' ;\n":                   "g.g4:2: the DFA lexer takes the longest match; a non-greedy loop cannot be compiled",
		"grammar G;\na : W ;\nW : ' ' -> skip ;\n":          "g.g4:2: token 'W' is skipped, so no rule can use it",
		"grammar G;\na : b^ ;\nb : 'x' ;\n":                 "g.g4:2: ^ joins a token to its neighbour; put it on a literal or a token",
		"grammar G;\na : {ok}? 'p' ;\n":                     "g.g4:2: a predicate {ok}? belongs at the start of the only alternative of an optional group, or of an alternative of a set rule",
		"grammar G;\nA : 'x' -> more ;\n":                   "g.g4:2: token 'A' has the command 'more'; a token takes skip, scan, noSpaceBefore, noSpaceAfter or noSpace",
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
@ast::A {
	I I128
}
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
@ast::Structs { Holder }
holder : 'if' (rest+=Name | 'else' rest=names) ;
names returns [[]string list] : list+=Name (',' list+=Name)* ;
Name : [a-z]+ ;
Comma : ',' ;
`)[maxonParserFile]
	for _, want := range []string{
		"\tlet mark_n_Rest = p.lists.string.mark()\n",
		"\tif p.lists.string.pushedSince(mark_n_Rest) 'b4'\n\t\tlet v4 = p.lists.string.take(mark_n_Rest)\n\t\tn.rest = v4\n",
		"\tlet mark_list = p.lists.string.mark()\n",
		"\tlet v5 = p.lists.string.take(mark_list)\n\tlist = v5\n",
		"type ListStacks\n\tvar string as ListStack with String\n",
	} {
		if !strings.Contains(parser, want) {
			t.Errorf("parser lacks %q:\n%s", want, parser)
		}
	}
}

func TestEachListOfARecordHasAStackOfItsOwn(t *testing.T) {
	parser := generateTiny(t, `grammar T;
both : 'both' xs+=Name* 'and' ys+=Name* ;
Name : [a-z]+ ;
`)[maxonParserFile]
	for _, want := range []string{
		"type ListStacks\n\tvar ids_Both_Xs as ListStack with diag.U32\n\tvar ids_Both_Ys as ListStack with diag.U32\n",
		"\tlet mark_n_Xs = p.lists.ids_Both_Xs.mark()\n",
		"\tlet mark_n_Ys = p.lists.ids_Both_Ys.mark()\n",
		"takeIDs(p.tree, stack: p.lists.ids_Both_Xs, mark: mark_n_Xs)\n",
		"takeIDs(p.tree, stack: p.lists.ids_Both_Ys, mark: mark_n_Ys)\n",
	} {
		if !strings.Contains(parser, want) {
			t.Errorf("parser lacks %q:\n%s", want, parser)
		}
	}
}

func TestARecordHoldsOnlyWhatARecordCan(t *testing.T) {
	cases := map[string]string{
		"grammar G;\na : 'p' x=Digits ;\nDigits : [0-9]+ ;\n":         "g.g4:2: record A: field X is text made of the token Digits; only a Name, an Integer or a Label can be read back from the module's text",
		"grammar G;\na : 'p' ;\n@ast::A {\n\tX Go = 1\n}\n":           "g.g4:4: record A: field X has an initial value, which a record cannot hold",
		"grammar G;\n@ast::Structs { B }\na : 'p' b=b ;\nb : 'q' ;\n": "g.g4:3: record A: field B has the type *B, which a record cannot hold: it holds flags, texts, integers, nodes and lists of nodes",
		"grammar G;\n@ast::Structs { Nothing }\na : 'p' ;\n":          "g.g4:0: @ast::Structs names 'Nothing', which is no node",
	}
	for src, want := range cases {
		_, err := generate("g.g4", []byte(src))
		if err == nil || err.Error() != want {
			t.Errorf("%q: got %v, want %q", src, err, want)
		}
	}
}
