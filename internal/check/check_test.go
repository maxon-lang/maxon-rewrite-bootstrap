package check

import (
	"testing"

	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"minimaxon/internal/project"
	"minimaxon/internal/resolve"
	"minimaxon/internal/srctest"
	"minimaxon/internal/syntax"
)

func checkSource(t *testing.T, src string) []*diag.CompileError {
	t.Helper()
	source := syntax.NewSource(src, []syntax.FileSlice{{File: "t.maxon", Begin: 0, End: len(src)}})
	parser := syntax.NewParser(source, syntax.Lex(source, nil))
	prog := parser.ParseProgram()
	if len(parser.Errors) > 0 {
		t.Fatalf("parse errors: %v", parser.Errors)
	}

	root := ast.NewModule("", ".", false)
	root.Decls = prog.Decls
	proj := ast.NewProject(root)
	proj.Modules = []*ast.Module{root}
	results := make([]ast.ModuleResult, len(proj.Modules))
	res := &results[root.ID]
	project.BuildTables(proj, results, 1)
	resolve.NewResolver(proj, root, res).Run()
	DeclareAliases(proj, results)
	if len(res.Errors) > 0 {
		return res.Errors
	}

	c := NewChecker(proj, root, &res.Errors)
	for _, d := range root.Decls {
		res.Functions = append(res.Functions, c.Declare(d)...)
	}
	c.CheckMain()
	for _, d := range root.Decls {
		switch d.(type) {
		case *ast.TypeDecl, *ast.UnionDecl, *ast.EnumDecl:
			c.CheckBody(d)
		}
	}
	for _, f := range res.Functions {
		c.CheckBody(f)
	}
	c.Verify(c.Pending)
	c.VerifyBounds(append(res.Bounds, c.Bounds...))
	if len(res.Errors) == 0 {
		o := NewOwnership(&res.Errors, c)
		for _, d := range root.Decls {
			o.CheckValues(d)
		}
		for _, f := range res.Functions {
			o.Check(f)
		}
	}
	return res.Errors
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

// The Maxon source of these tests is in testdata, where minimaxon rewrite reaches it. A file of declarations that several
// tests share is their prelude, and a program is the prelude followed by a file that holds the rest.

// checkFile checks the files, joined as srctest.Read does, as one program.
func checkFile(t *testing.T, paths ...string) []*diag.CompileError {
	t.Helper()
	return checkSource(t, srctest.Read(t, paths...))
}

// checkCases checks each program of a file of cases, which srctest.Cases describes, and expects the diagnostics that the
// program says.
func checkCases(t *testing.T, path string, prelude ...string) {
	t.Helper()
	for _, c := range srctest.Cases(t, path, prelude...) {
		t.Run(c.Name, func(t *testing.T) { expectCodes(t, checkSource(t, c.Source), c.Codes...) })
	}
}

const boxPrelude = "testdata/basics/box.maxon"

func TestAcceptsACorrectProgram(t *testing.T) {
	expectCodes(t, checkFile(t, boxPrelude, "testdata/basics/accepts-a-correct-program.maxon"))
}

func TestMixedAliases(t *testing.T) {
	expectCodes(t, checkFile(t, "testdata/basics/mixed-aliases.maxon"), diag.ErrTypeMismatch)
}

func TestNonExhaustiveMatch(t *testing.T) {
	expectCodes(t, checkFile(t, "testdata/basics/non-exhaustive-match.maxon"), diag.ErrMatchNotExhaustive)
}

func TestUseAfterMove(t *testing.T) {
	expectCodes(t, checkFile(t, boxPrelude, "testdata/basics/use-after-move.maxon"), diag.ErrUseAfterMove)
}

func TestEndLabelMustNameItsBlock(t *testing.T) {
	expectCodes(t, checkFile(t, "testdata/basics/end-label-must-name-its-block.maxon"), diag.ErrMismatchedEndLabel)
}

const ownedPrelude = "testdata/ownership/prelude.maxon"

func TestOwnershipAccepts(t *testing.T) {
	checkCases(t, "testdata/ownership/accepts.maxon", ownedPrelude)
}

func TestOwnershipRejects(t *testing.T) {
	checkCases(t, "testdata/ownership/rejects.maxon", ownedPrelude)
}
