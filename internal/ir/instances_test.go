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

// frontEnd runs the phases before lowering on the program in a file of testdata and returns the result of its one
// module.
func frontEnd(t *testing.T, path string) []ast.ModuleResult {
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
	results := make([]ast.ModuleResult, len(proj.Modules))
	res := &results[root.ID]
	project.BuildTables(proj, results, 1)
	resolve.NewResolver(proj, root, res).Run()
	check.DeclareAliases(proj, results)
	c := check.NewChecker(proj, root, &res.Errors)
	for _, d := range root.Decls {
		res.Functions = append(res.Functions, c.Declare(d)...)
	}
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
	res.Callers = c.Callers
	if len(res.Errors) == 0 {
		o := check.NewOwnership(&res.Errors, c)
		for _, d := range root.Decls {
			o.CheckValues(d)
		}
		for _, f := range res.Functions {
			o.Check(f)
		}
	}
	if len(res.Errors) > 0 {
		t.Fatalf("errors: %v", res.Errors)
	}
	return results
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
	results := frontEnd(t, genericsFile)
	got := strings.Join(instanceNames(Instances(results[0].Callers).List), " ")
	// main's calls come first, in the order they are written, then the calls inside each instance.
	want := "swap[Score] swap[Square] (Box with Score).make both[Square] total[Square] (Box with Score).peek"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if again := strings.Join(instanceNames(Instances(results[0].Callers).List), " "); again != want {
		t.Fatalf("the second walk found %s, not %s", again, want)
	}
}

func TestInstancesAreLoweredWithTheirOwnKinds(t *testing.T) {
	results := frontEnd(t, genericsFile)
	slots := map[string][]IrKind{}
	instances := Instances(results[0].Callers)
	for _, f := range instances.List {
		slots[f.Symbol()] = Lower(f, instances).Slots
	}
	if got := slots["swap[Score]"]; len(got) != 1 || got[0] != KindScalar {
		t.Errorf("swap at a scalar has slots %v", got)
	}
	if got := slots["swap[Square]"]; len(got) != 1 || got[0] != KindManaged {
		t.Errorf("swap at a record has slots %v", got)
	}
}

func TestNothingIsInstantiatedForAProgramWithoutGenerics(t *testing.T) {
	results := frontEnd(t, "testdata/no-generics.maxon")
	if found := Instances(results[0].Callers).List; len(found) != 0 {
		t.Fatalf("found %v", instanceNames(found))
	}
}

func TestStatementsAfterATerminatingStatementInstantiateNothing(t *testing.T) {
	results := frontEnd(t, "testdata/dead-statements.maxon")
	if found := Instances(results[0].Callers).List; len(found) != 0 {
		t.Fatalf("found %v", instanceNames(found))
	}
}

// A module that lowers a call of an instance it did not find would depend on another module having made it, so the
// lowering refuses, however the instance came to exist.
func TestLoweringACallOfAnInstanceTheModuleDidNotFindIsRefused(t *testing.T) {
	results := frontEnd(t, genericsFile)
	found := Instances(results[0].Callers)
	var main *ast.FuncDecl
	for _, f := range results[0].Functions {
		if f.Name == "main" {
			main = f
		}
	}
	defer func() {
		message, _ := recover().(error)
		if message == nil || !strings.Contains(message.Error(), "was not found among the instances of the module") {
			t.Errorf("got %v, want an internal error that names the instance", message)
		}
	}()
	Lower(main, &InstanceSet{})
	t.Errorf("main was lowered without the %d instances it needs", len(found.List))
}
