package syntax

import (
	"strings"
	"testing"

	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"minimaxon/internal/srctest"
)

// sourceOfText is a source of files with the given texts, named a.maxon, b.maxon and so on.
func sourceOfText(files ...string) *Source {
	var buf []byte
	var slices []FileSlice
	for i, src := range files {
		name := string(rune('a'+i)) + ".maxon"
		slices = append(slices, FileSlice{File: name, Begin: len(buf), End: len(buf) + len(src)})
		buf = append(buf, src...)
	}
	return NewSource(string(buf), slices)
}

// sourceOf is a source of the files at paths in testdata, named a.maxon, b.maxon and so on, each a file of its own.
func sourceOf(t *testing.T, paths ...string) *Source {
	t.Helper()
	texts := make([]string, len(paths))
	for i, path := range paths {
		texts[i] = srctest.Read(t, path)
	}
	return sourceOfText(texts...)
}

func parseFiles(t *testing.T, paths ...string) (*ast.Program, []*diag.CompileError) {
	t.Helper()
	src := sourceOf(t, paths...)
	p := NewParser(src, Lex(src, nil))
	return p.ParseProgram(), p.Errors
}

func TestLexKindsAndPositions(t *testing.T) {
	src := sourceOf(t, "testdata/lex-kinds.maxon")
	toks := Lex(src, nil)
	p := NewParser(src, toks)
	want := []struct {
		kind T
		text string
		col  int
	}{
		{TKw_let, "", 1}, {TName, "x", 5}, {TAssign, "=", 7}, {TInteger, "0x1F", 9}, {TNewline, "", 13}, {TFileEnd, "", 1}, {TEof, "", 1},
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens: %+v", len(toks), toks)
	}
	for i, w := range want {
		_, col := p.lineCol(&toks[i])
		if toks[i].Kind() != w.kind || (w.text != "" && p.text(toks[i]) != w.text) || col != w.col {
			t.Errorf("token %d = kind %d text %q col %d, want kind %d text %q col %d", i, toks[i].Kind(), p.text(toks[i]), col, w.kind, w.text, w.col)
		}
	}
	if v := p.intLitMagnitude(toks[3]); v != ast.I64(31) {
		t.Errorf("0x1F = %v", v)
	}
}

func TestPositionsCountLinesPerFile(t *testing.T) {
	_, errs := parseFiles(t, "testdata/positions/a.maxon", "testdata/positions/b.maxon")
	if len(errs) != 1 || errs[0].File != "b.maxon" || errs[0].Line != 3 || errs[0].Col != 9 {
		t.Fatalf("errors: %v", errs)
	}
}

func TestStringPartsAreSlicesOfTheSource(t *testing.T) {
	src := sourceOf(t, "testdata/string-parts.maxon")
	p := NewParser(src, Lex(src, nil))
	parts := p.strLitParts(p.toks[3])
	if len(parts) != 3 || parts[0] != "a" || parts[2] != "b\tc" {
		t.Fatalf("parts: %#v", parts)
	}
	if _, ok := parts[1].(*ast.NameExpr); !ok {
		t.Errorf("interpolation: got %T, want a name", parts[1])
	}
}

func TestTokenLongerThanTheLengthBitsIsAnError(t *testing.T) {
	defer func() {
		ce, ok := recover().(*diag.CompileError)
		if !ok || !strings.Contains(ce.Message, "longer than 16 MB") || ce.Line != 1 || ce.Col != 5 {
			t.Fatalf("got %v, want the token length error at 1:5", ce)
		}
	}()
	Lex(sourceOfText("abc "+strings.Repeat("x", MaxTokenLength+1)+" = 1\n"), nil)
}

func TestCommentsAreSkippedAndKeepPositions(t *testing.T) {
	prog, errs := parseFiles(t, "testdata/comments.maxon")
	if len(errs) != 1 || errs[0].Line != 8 || errs[0].Col != 9 {
		t.Fatalf("errors: %v", errs)
	}
	if len(prog.Decls) != 2 {
		t.Fatalf("decls: %+v", prog.Decls)
	}
	parts := prog.Decls[1].(*ast.ConstDecl).Value.(*ast.StrLit).Parts
	if len(parts) != 2 || parts[1] != " // kept" {
		t.Errorf("string parts: %#v", parts)
	}
}

func TestTokenTextDoesNotAllocate(t *testing.T) {
	src := sourceOf(t, "testdata/token-text.maxon")
	p := NewParser(src, Lex(src, nil))
	var text string
	if n := testing.AllocsPerRun(100, func() { text = p.text(p.toks[1]) }); n != 0 || text != "name" {
		t.Errorf("text %q allocated %v times", text, n)
	}
}

func TestParseFunction(t *testing.T) {
	prog, errs := parseFiles(t, "testdata/parse-function.maxon")
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	f, ok := prog.Decls[0].(*ast.FuncDecl)
	if len(prog.Decls) != 1 || !ok || f.Name != "f" || len(f.Body) != 1 {
		t.Fatalf("decls: %+v", prog.Decls)
	}
}

func TestParseOperatorPrecedence(t *testing.T) {
	prog, errs := parseFiles(t, "testdata/operator-precedence.maxon")
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	cond, ok := prog.Decls[0].(*ast.ConstDecl).Value.(*ast.CondExpr)
	if !ok {
		t.Fatalf("got %T, want a conditional", prog.Decls[0].(*ast.ConstDecl).Value)
	}
	sum, ok := cond.Then.(*ast.BinaryExpr)
	if !ok || sum.Op != "+" {
		t.Fatalf("then: got %+v, want '+'", cond.Then)
	}
	if lit, ok := sum.L.(*ast.IntLit); !ok || lit.Value() != ast.I64(-1) {
		t.Errorf("left of '+': got %+v, want the literal -1", sum.L)
	}
	product, ok := sum.R.(*ast.BinaryExpr)
	if !ok || product.Op != "*" {
		t.Fatalf("right of '+': got %+v, want '*'", sum.R)
	}
	if _, ok := product.R.(*ast.CastExpr); !ok {
		t.Errorf("right of '*': got %T, want a cast", product.R)
	}
	if or, ok := cond.Cond.(*ast.BinaryExpr); !ok || or.Op != "or" {
		t.Errorf("condition: got %+v, want 'or'", cond.Cond)
	}
}

func TestParseAssignmentAndCallShareTheirStart(t *testing.T) {
	prog, errs := parseFiles(t, "testdata/assignment-and-call.maxon")
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	body := prog.Decls[0].(*ast.FuncDecl).Body
	if _, ok := body[0].(*ast.AssignStmt); !ok {
		t.Errorf("statement 1: got %T, want an assignment", body[0])
	}
	if _, ok := body[1].(*ast.ExprStmt); !ok {
		t.Errorf("statement 2: got %T, want an expression statement", body[1])
	}
}

func TestSyntaxErrorNamesWhatWasExpected(t *testing.T) {
	_, errs := parseFiles(t, "testdata/expected-token.maxon")
	if len(errs) != 1 || errs[0].Code != diag.ErrExpectedToken || errs[0].Line != 2 || !strings.Contains(errs[0].Message, "found ')'") {
		t.Fatalf("errors: %v", errs)
	}
}

func TestSyntaxErrorDescribesATokenByItsDescription(t *testing.T) {
	_, errs := parseFiles(t, "testdata/expected-end-of-line.maxon")
	if len(errs) != 1 || errs[0].Message != "expected the end of the line, found '2'" {
		t.Fatalf("errors: %v", errs)
	}
}

func TestParseRecoversAtTheNextFile(t *testing.T) {
	prog, errs := parseFiles(t, "testdata/recover/broken.maxon", "testdata/recover/fine.maxon")
	if len(errs) != 1 || errs[0].Code != diag.ErrUnexpectedEnd || errs[0].File != "a.maxon" {
		t.Fatalf("errors: %v", errs)
	}
	if len(prog.Decls) != 1 || prog.Decls[0].Base().Name != "fine" || fileOf(prog.Decls[0]) != "b.maxon" {
		t.Fatalf("decls: %+v", prog.Decls)
	}
}

// mutatesKeyword is the receiver convention of the second method of the interface in testdata/generics.maxon.
const mutatesKeyword = "mutates"

func TestParsesGenericDeclarationsAndUses(t *testing.T) {
	prog, errs := parseFiles(t, "testdata/generics.maxon")
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	iface := prog.Decls[0].(*ast.InterfaceDecl)
	if len(iface.Methods) != 2 || iface.Methods[1].Receiver != mutatesKeyword || len(iface.Methods[1].Params) != 1 {
		t.Fatalf("interface methods: %+v", iface.Methods)
	}
	box := prog.Decls[1].(*ast.TypeDecl)
	if len(box.TypeParams) != 2 || box.TypeParams[1].Name != "U" {
		t.Fatalf("type parameters: %+v", box.TypeParams)
	}
	if len(box.Where) != 1 || box.Where[0].Param != "T" || len(box.Where[0].Bounds) != 2 || box.Where[0].Bounds[1].Name != "Named" {
		t.Fatalf("constraints: %+v", box.Where)
	}
	if len(box.Conforms) != 1 || box.Conforms[0].Name != "Shape" {
		t.Fatalf("conforms: %+v", box.Conforms)
	}
	pick := prog.Decls[2].(*ast.FuncDecl)
	if len(pick.TypeParams) != 1 || len(pick.Where) != 1 {
		t.Fatalf("type parameters %+v, constraints %+v", pick.TypeParams, pick.Where)
	}
	// 'with' takes one argument bare, so a bare instance as the argument nests to the right.
	result := pick.Returns.TypeArgs
	if len(result) != 2 || result[1].Name != "Other" || result[1].Qualifier[0] != "lib" || len(result[1].TypeArgs) != 1 ||
		result[1].TypeArgs[0].Name != "Option" || len(result[1].TypeArgs[0].TypeArgs) != 1 {
		t.Fatalf("return type arguments: %+v", result)
	}
	alias := prog.Decls[3].(*ast.AliasDecl)
	if alias.Instance == nil || alias.Instance.Name != "array" || len(alias.Instance.TypeArgs[0].TypeArgs) != 2 {
		t.Fatalf("typealias: %+v", alias.Instance)
	}
}

func TestParsesSubscripts(t *testing.T) {
	prog, errs := parseFiles(t, "testdata/subscripts.maxon")
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	body := prog.Decls[0].(*ast.FuncDecl).Body
	assign := body[0].(*ast.AssignStmt)
	if target, ok := assign.Target.(*ast.IndexExpr); !ok || len(target.Args) != 1 {
		t.Fatalf("assignment target: %T", assign.Target)
	}
	if member, ok := assign.Value.(*ast.MemberExpr); !ok || member.Obj.(*ast.IndexExpr).Args[0].(*ast.BinaryExpr).Op != "+" {
		t.Fatalf("assignment value: %T", assign.Value)
	}
	call := body[1].(*ast.ExprStmt).E.(*ast.CallExpr)
	if len(call.Args) != 2 || call.Args[0].Value.(*ast.IndexExpr).Args[0].(*ast.BinaryExpr).Op != "-" {
		t.Fatalf("call arguments: %+v", call.Args)
	}
}

// The tests of the rewrite commands keep their source in Go strings: each names the keyword it renames and compares the
// result, so a file of testdata that rewrite renamed would change what they test.
func TestKeywordEditsRenameOnlyTheKeyword(t *testing.T) {
	src := sourceOfText("// an end comment\nfunction main() returns ExitCode\n\tlet end2 = \"end {f(end2)}\"\n\treturn 0\nend 'end'\n",
		"function f() returns ExitCode\n\treturn 0\nend 'f'\n")
	got := ApplyEdits(src.Text, KeywordEdits(src, "end", "fin"))
	want := "// an end comment\nfunction main() returns ExitCode\n\tlet end2 = \"end {f(end2)}\"\n\treturn 0\nfin 'end'\n" +
		"function f() returns ExitCode\n\treturn 0\nfin 'f'\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestKeywordEditsLookIntoInterpolations(t *testing.T) {
	src := sourceOfText("function main() returns ExitCode\n\tprint(\"{1 if true else 2}\")\n\treturn 0\nend 'main'\n")
	got := ApplyEdits(src.Text, KeywordEdits(src, "true", "false"))
	if !strings.Contains(got, "{1 if false else 2}") {
		t.Errorf("the interpolation was not renamed:\n%s", got)
	}
}

func TestMarkdownKeywordEditsRenameOnlyCode(t *testing.T) {
	doc := "A `let` binding; let prose stay.\n\n```maxon\nfunction f() returns ExitCode\n\tlet x = 1\n\treturn x\nend 'f'\n```\n\n" +
		"```powershell\nlet\n```\n\nIR: `%r = let s0`, and `x if true else y`.\n"
	edits, refused, _ := MarkdownKeywordEdits("d.md", doc, "let", "val")
	got := ApplyEdits(doc, edits)
	want := "A `val` binding; let prose stay.\n\n```maxon\nfunction f() returns ExitCode\n\tval x = 1\n\treturn x\nend 'f'\n```\n\n" +
		"```powershell\nlet\n```\n\nIR: `%r = let s0`, and `x if true else y`.\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if refused != 1 {
		t.Errorf("refused %d spans holding the keyword, want 1 (the IR span)", refused)
	}
}

func fileOf(n ast.Node) string {
	file, _, _ := n.P().Where()
	return file
}
