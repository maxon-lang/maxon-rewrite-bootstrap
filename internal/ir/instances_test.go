package ir

import (
	"strings"
	"testing"

	"minimaxon/internal/ast"
	"minimaxon/internal/check"
	"minimaxon/internal/project"
	"minimaxon/internal/resolve"
	"minimaxon/internal/srctest"
	"minimaxon/internal/syntax"
)

// frontEnd runs the phases before lowering on the program in a file of testdata and returns its module.
func frontEnd(t *testing.T, path string) *ast.Module {
	t.Helper()
	src := srctest.Read(t, path)
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
	project.BuildTables(proj, 1)
	resolve.NewResolver(proj, root).Run()
	c := check.NewChecker(proj, root, &root.Errors)
	for _, d := range root.Decls {
		root.Functions = append(root.Functions, c.Declare(d)...)
	}
	for _, d := range root.Decls {
		switch d.(type) {
		case *ast.TypeDecl, *ast.UnionDecl, *ast.EnumDecl:
			c.CheckBody(d)
		}
	}
	for _, f := range root.Functions {
		c.CheckBody(f)
	}
	c.Verify(c.Pending)
	c.VerifyBounds(c.Bounds)
	root.Callers = c.Callers
	if len(root.Errors) == 0 {
		o := check.NewOwnership(&root.Errors)
		for _, d := range root.Decls {
			o.CheckValues(d)
		}
		for _, f := range root.Functions {
			o.Check(f)
		}
	}
	if len(root.Errors) > 0 {
		t.Fatalf("errors: %v", root.Errors)
	}
	return root
}

const genericsFile = "testdata/generics.maxon"

func instanceNames(fns []*ast.FuncDecl) []string {
	names := make([]string, len(fns))
	for i, f := range fns {
		names[i] = f.Symbol()
	}
	return names
}

func TestEachInstanceIsFoundOnceInDiscoveryOrder(t *testing.T) {
	m := frontEnd(t, genericsFile)
	found := Instances([]*ast.Module{m})
	got := strings.Join(instanceNames(found[0]), " ")
	// main's calls come first, in the order they are written, then the calls inside each instance.
	want := "swap[Score] swap[Square] Box[Score].make both[Square] total[Square] Box[Score].peek"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if again := Instances([]*ast.Module{m}); len(again[0]) != 0 {
		t.Fatalf("the second walk found %v again", instanceNames(again[0]))
	}
}

func TestInstancesAreLoweredWithTheirOwnKinds(t *testing.T) {
	m := frontEnd(t, genericsFile)
	slots := map[string][]IrKind{}
	for _, f := range Instances([]*ast.Module{m})[0] {
		slots[f.Symbol()] = Lower(f).Slots
	}
	if got := slots["swap[Score]"]; len(got) != 1 || got[0] != KindScalar {
		t.Errorf("swap at a scalar has slots %v", got)
	}
	if got := slots["swap[Square]"]; len(got) != 1 || got[0] != KindManaged {
		t.Errorf("swap at a record has slots %v", got)
	}
}

func TestNothingIsInstantiatedForAProgramWithoutGenerics(t *testing.T) {
	m := frontEnd(t, "testdata/no-generics.maxon")
	if found := Instances([]*ast.Module{m}); len(found[0]) != 0 {
		t.Fatalf("found %v", instanceNames(found[0]))
	}
}
