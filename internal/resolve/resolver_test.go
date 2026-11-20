package resolve

import (
	"testing"

	"minimaxon/internal/ast"
	"minimaxon/internal/project"
	"minimaxon/internal/srctest"
	"minimaxon/internal/syntax"
)

// resolved parses the program in a file of testdata as the one module of a project and resolves it.
func resolved(t *testing.T, path string) (*ast.Project, *ast.Module, *ast.ModuleResult, int) {
	t.Helper()
	text := srctest.Read(t, path)
	source := syntax.NewSource(text, []syntax.FileSlice{{File: path, Begin: 0, End: len(text)}})
	parser := syntax.NewParser(source, syntax.Lex(source, nil))
	program := parser.ParseProgram()
	if len(parser.Errors) > 0 {
		t.Fatalf("parse errors: %v", parser.Errors)
	}
	root := ast.NewModule("", ".", false)
	root.Adopt(program)
	proj := ast.NewProject(root)
	proj.Modules = []*ast.Module{root}
	results := make([]ast.ModuleResult, len(proj.Modules))
	project.BuildTables(proj, results, 1)
	parsed := root.Tree.Count()
	NewResolver(proj, root, &results[root.ID]).Run()
	if len(results[root.ID].Errors) > 0 {
		t.Fatalf("resolve errors: %v", results[root.ID].Errors)
	}
	return proj, root, &results[root.ID], parsed
}

func TestResolutionBindsNamesAndTypeReferences(t *testing.T) {
	proj, root, res, _ := resolved(t, "testdata/bindings.maxon")
	small := root.Names["Small"]
	box := root.Names["Box"].(*ast.TypeDecl)
	area := root.Names["area"].(*ast.FuncDecl)

	if got := res.Resolution.Decl(proj, area.Params[0].Type.ID); got != small {
		t.Errorf("the type of extent names %v, want Small", got)
	}
	boxRef := area.Params[1].Type
	if got := res.Resolution.Decl(proj, boxRef.ID); got != ast.Decl(box) {
		t.Errorf("the type of box names %v, want Box", got)
	}
	if got := res.Resolution.Decl(proj, boxRef.TypeArgs().At(0).ID); got != small {
		t.Errorf("the type argument names %v, want Small", got)
	}
	if got := res.Resolution.Decl(proj, area.Returns.ID); got != proj.Stdlib.Names["Count"] {
		t.Errorf("the qualified return type names %v, want the standard library's Count", got)
	}

	field := box.Fields[0].Type
	param := res.Resolution.TypeParam(field.ID)
	if param == nil || param != box.TypeParams[0] || res.Resolution.Decl(proj, field.ID) != nil {
		t.Errorf("the type of the field binds %v, want the type parameter T and no declaration", param)
	}
	if bound := res.Resolution.TypeParam(box.Where[0].ID); bound != box.TypeParams[0] {
		t.Errorf("the constraint binds %v, want the type parameter T", bound)
	}
	if got := res.Resolution.Decl(proj, box.Where[0].Bounds.At(0).ID); got != proj.Stdlib.Names["Droppable"] {
		t.Errorf("the bound names %v, want the standard library's Droppable", got)
	}

	let, _ := area.Body.At(0).AsLetStmt()
	local, _ := let.Init().AsNameExpr()
	if res.Resolution.Decl(proj, local.ID) != nil {
		t.Error("a name that is a local names a declaration")
	}
}

// Resolving a qualified name makes the outermost member of its path the one name that is bound to the declaration, in
// place: the node keeps its number, so nothing that holds it changes and the module has no node more than the parser made.
func TestAQualifiedNameIsTheMemberItReplacesBoundToItsDeclaration(t *testing.T) {
	proj, root, res, parsed := resolved(t, "testdata/bindings.maxon")
	area := root.Names["area"].(*ast.FuncDecl)
	let, _ := area.Body.At(1).AsLetStmt()
	call, _ := let.Init().AsCallExpr()
	callee, ok := call.Callee().AsNameExpr()
	if !ok {
		t.Fatalf("the callee is a %v, want the name that replaced stdlib.exchange", call.Callee().Kind())
	}
	if root.Tree.Count() != parsed {
		t.Errorf("the module has %d nodes after resolution and the parser made %d", root.Tree.Count(), parsed)
	}
	if callee.Name() != "exchange" {
		t.Errorf("the replacing name is %q, want exchange", callee.Name())
	}
	if got := res.Resolution.Decl(proj, callee.ID); got != proj.Stdlib.Names["exchange"] {
		t.Errorf("the replacing name names %v, want the standard library's exchange", got)
	}
}
