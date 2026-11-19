package syntax

import (
	"strings"
	"testing"

	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
)

func lexFiles(files ...string) ([]Token, []string) {
	var buf []byte
	var slices []FileSlice
	var names []string
	for i, src := range files {
		name := string(rune('a'+i)) + ".maxon"
		slices = append(slices, FileSlice{File: name, Begin: len(buf), End: len(buf) + len(src)})
		buf = append(buf, src...)
		names = append(names, name)
	}
	return Lex(buf, slices, nil, map[string]string{}), names
}

func parseFiles(t *testing.T, files ...string) (*ast.Program, []*diag.CompileError) {
	t.Helper()
	toks, names := lexFiles(files...)
	p := NewParser(toks, names)
	return p.ParseProgram(), p.Errors
}

func TestLexKindsAndPositions(t *testing.T) {
	toks, _ := lexFiles("let x = 0x1F\n")
	want := []struct {
		kind T
		text string
		col  int
	}{
		{TKw_let, "let", 1}, {TName, "x", 5}, {TAssign, "=", 7}, {TInteger, "0x1F", 9}, {TNewline, "", 0}, {TFileEnd, "", 0}, {TEof, "", 0},
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens: %+v", len(toks), toks)
	}
	for i, w := range want {
		if toks[i].Kind != w.kind || (w.text != "" && toks[i].Text != w.text) || (w.col != 0 && toks[i].Col != w.col) {
			t.Errorf("token %d = %+v, want kind %d text %q col %d", i, toks[i], w.kind, w.text, w.col)
		}
	}
	if v := toks[3].IntValue(); v != ast.I64(31) {
		t.Errorf("0x1F = %v", v)
	}
}

func TestStripCommentsKeepsColumns(t *testing.T) {
	src := []byte("let a = /* c */ 1 // trailing\n/// doc\n\"// kept\"\n")
	end := StripComments(src, 0, len(src))
	want := "let a = " + strings.Repeat(" ", 7) + " 1 \n/// doc\n\"// kept\"\n"
	if got := string(src[:end]); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestParseFunction(t *testing.T) {
	prog, errs := parseFiles(t, "function f() returns ExitCode\n\treturn 0\nend 'f'\n")
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	f, ok := prog.Decls[0].(*ast.FuncDecl)
	if len(prog.Decls) != 1 || !ok || f.Name != "f" || len(f.Body) != 1 {
		t.Fatalf("decls: %+v", prog.Decls)
	}
}

func TestParseOperatorPrecedence(t *testing.T) {
	prog, errs := parseFiles(t, "let c = -1 + 2 * 3 as T if a or b else 4\n")
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
	prog, errs := parseFiles(t, "function f()\n\tx.y = 1\n\tg(x)\nend 'f'\n")
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
	_, errs := parseFiles(t, "function f()\n\treturn )\nend 'f'\n")
	if len(errs) != 1 || errs[0].Code != diag.ErrExpectedToken || errs[0].Line != 2 || !strings.Contains(errs[0].Message, "found ')'") {
		t.Fatalf("errors: %v", errs)
	}
}

func TestParseRecoversAtTheNextFile(t *testing.T) {
	prog, errs := parseFiles(t,
		"function broken() returns ExitCode\n\treturn 1\n",
		"function fine() returns ExitCode\n\treturn 0\nend 'fine'\n")
	if len(errs) != 1 || errs[0].Code != diag.ErrUnexpectedEnd || errs[0].File != "a.maxon" {
		t.Fatalf("errors: %v", errs)
	}
	if len(prog.Decls) != 1 || prog.Decls[0].Base().Name != "fine" || prog.Decls[0].P().File != "b.maxon" {
		t.Fatalf("decls: %+v", prog.Decls)
	}
}
