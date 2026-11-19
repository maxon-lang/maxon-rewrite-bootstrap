package check

import (
	"testing"

	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"minimaxon/internal/project"
	"minimaxon/internal/resolve"
	"minimaxon/internal/syntax"
)

func checkSource(t *testing.T, src string) []*diag.CompileError {
	t.Helper()
	buf := []byte(src)
	toks := syntax.Lex(buf, []syntax.FileSlice{{File: "t.maxon", Begin: 0, End: len(buf)}}, nil, map[string]string{})
	parser := syntax.NewParser(toks, []string{"t.maxon"})
	prog := parser.ParseProgram()
	if len(parser.Errors) > 0 {
		t.Fatalf("parse errors: %v", parser.Errors)
	}

	root := ast.NewModule("", ".", false)
	root.Decls = prog.Decls
	proj := ast.NewProject(root)
	proj.Modules = []*ast.Module{root}
	project.BuildTables(proj, 1)
	resolve.NewResolver(proj, root).Run()
	if len(root.Errors) > 0 {
		return root.Errors
	}

	c := NewChecker(proj, root, &root.Errors)
	for _, d := range root.Decls {
		root.Functions = append(root.Functions, c.Declare(d)...)
	}
	c.CheckMain()
	for _, d := range root.Decls {
		switch d.(type) {
		case *ast.TypeDecl, *ast.UnionDecl:
			c.CheckBody(d)
		}
	}
	for _, f := range root.Functions {
		c.CheckBody(f)
	}
	c.Verify(c.Pending)
	if len(root.Errors) == 0 {
		o := NewOwnership(&root.Errors)
		for _, f := range root.Functions {
			o.Check(f)
		}
	}
	return root.Errors
}

func expectCodes(t *testing.T, errs []*diag.CompileError, codes ...string) {
	t.Helper()
	if len(errs) != len(codes) {
		t.Fatalf("got %v, want codes %v", errs, codes)
	}
	for i, code := range codes {
		if errs[i].Code != code {
			t.Errorf("error %d: got %v, want %s", i, errs[i], code)
		}
	}
}

const box = `typealias Score = int(i64.min to i64.max)

type Box
	export var v as Score

	static function create(v Score) returns Box
		return Box{v: v}
	end 'create'
end 'Box'
`

func TestAcceptsACorrectProgram(t *testing.T) {
	expectCodes(t, checkSource(t, box+`
function main() returns ExitCode
	let b = Box.create(4)
	print("{b.v}\n")
	return 0
end 'main'
`))
}

func TestMixedAliases(t *testing.T) {
	expectCodes(t, checkSource(t, `typealias Apples = int(0 to 100)
typealias Pears = int(0 to 100)

function main() returns ExitCode
	let a = 3 as Apples
	let p = 4 as Pears
	print("{a + p}\n")
	return 0
end 'main'
`), diag.ErrTypeMismatch)
}

func TestNonExhaustiveMatch(t *testing.T) {
	expectCodes(t, checkSource(t, `enum Color
	red
	green
end 'Color'

function main() returns ExitCode
	match Color.red 'm'
		red then print("r\n")
	end 'm'
	return 0
end 'main'
`), diag.ErrMatchNotExhaustive)
}

func TestUseAfterMove(t *testing.T) {
	expectCodes(t, checkSource(t, box+`
function main() returns ExitCode
	var a = Box.create(1)
	let b = a
	print("{a.v} {b.v}\n")
	a = Box.create(2)
	return 0
end 'main'
`), diag.ErrUseAfterMove)
}
