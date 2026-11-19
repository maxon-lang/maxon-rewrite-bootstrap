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
		{TIdent, "let", 1}, {TIdent, "x", 5}, {TAssign, "=", 7}, {TInt, "0x1F", 9}, {TNewline, "", 0}, {TFileEnd, "", 0}, {TEof, "", 0},
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

func TestParseMismatchedEndLabel(t *testing.T) {
	_, errs := parseFiles(t, "function f() returns ExitCode\n\treturn 0\nend 'g'\n")
	if len(errs) != 1 || errs[0].Code != diag.ErrMismatchedEndLabel || errs[0].Line != 3 {
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
