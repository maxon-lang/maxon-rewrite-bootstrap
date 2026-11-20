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
	parsed := root.Nodes.Count()
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
	if got := res.Resolution.Decl(proj, boxRef.TypeArgs[0].ID); got != small {
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
	if got := res.Resolution.Decl(proj, box.Where[0].Bounds[0].ID); got != proj.Stdlib.Names["Droppable"] {
		t.Errorf("the bound names %v, want the standard library's Droppable", got)
	}

	local := area.Body[0].(*ast.LetStmt).Init.(*ast.NameExpr)
	if res.Resolution.Decl(proj, local.ID) != nil {
		t.Error("a name that is a local names a declaration")
	}
}

// Resolving a qualified name replaces the chain of member accesses with one name that is bound to the declaration, and
// that name is a node of the module like any other: it takes the next NodeID and has a row in the table.
func TestAQualifiedNameIsANewNodeBoundToItsDeclaration(t *testing.T) {
	proj, root, res, parsed := resolved(t, "testdata/bindings.maxon")
	area := root.Names["area"].(*ast.FuncDecl)
	call := area.Body[1].(*ast.LetStmt).Init.(*ast.CallExpr)
	callee, ok := call.Callee.(*ast.NameExpr)
	if !ok {
		t.Fatalf("the callee is a %T, want the name that replaced stdlib.exchange", call.Callee)
	}
	if int(callee.ID) <= parsed || int(callee.ID) > root.Nodes.Count() {
		t.Errorf("the replacing name is node %d, want one made after the %d the parser made", callee.ID, parsed)
	}
	if got := res.Resolution.Decl(proj, callee.ID); got != proj.Stdlib.Names["exchange"] {
		t.Errorf("the replacing name names %v, want the standard library's exchange", got)
	}
}
