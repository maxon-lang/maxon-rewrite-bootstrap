package ast

import (
	"testing"

	"minimaxon/internal/diag"
)

// resolutionProject is a project of a root module that declares f and g, which a Resolution can name by handle.
func resolutionProject() (proj *Project, f, g *FuncDecl) {
	root := NewModule("", ".", false)
	root.Nodes = NewArena(root.ID)
	proj = NewProject(root)
	proj.Modules = []*Module{root}
	declare := func(name string) *FuncDecl {
		d := root.Nodes.NewFuncDecl()
		d.Name, d.Module = name, root
		root.Declare(d)
		return d
	}
	return proj, declare("f"), declare("g")
}

func TestResolutionGivesBackTheDeclarationANodeNames(t *testing.T) {
	proj, f, g := resolutionProject()
	option := proj.Stdlib.Names["Option"]
	var res Resolution
	res.Reset(proj.Root.Nodes.Count())
	res.BindDecl(1, f)
	res.BindDecl(2, g)
	res.BindDecl(2, option)
	if res.Decl(proj, 1) != Decl(f) || res.Decl(proj, 2) != option {
		t.Errorf("a node names %v and %v, want f and the standard library's Option", res.Decl(proj, 1), res.Decl(proj, 2))
	}
	if res.TypeParam(1) != nil {
		t.Error("a declaration is not a type parameter")
	}
}

func TestANodeBoundToNothingNamesNothing(t *testing.T) {
	proj, _, _ := resolutionProject()
	var res Resolution
	res.Reset(proj.Root.Nodes.Count())
	if res.Decl(proj, 1) != nil || res.TypeParam(1) != nil {
		t.Error("a node that was never bound names something")
	}
}

func TestResolutionKeepsTypeParametersApartFromDeclarations(t *testing.T) {
	proj, f, _ := resolutionProject()
	param := proj.Root.Nodes.NewTypeParam()
	var res Resolution
	res.Reset(proj.Root.Nodes.Count())
	res.BindTypeParam(1, res.AddTypeParam(param))
	res.BindDecl(2, f)
	if res.TypeParam(1) != param || res.Decl(proj, 1) != nil {
		t.Errorf("node 1 binds %v and names %v, want the type parameter only", res.TypeParam(1), res.Decl(proj, 1))
	}
	if res.TypeParam(2) != nil || res.Decl(proj, 2) != Decl(f) {
		t.Errorf("node 2 binds %v and names %v, want f only", res.TypeParam(2), res.Decl(proj, 2))
	}
}

func TestNodesThatNameTheSameDeclarationShareAnEntry(t *testing.T) {
	proj, f, g := resolutionProject()
	var res Resolution
	res.Reset(proj.Root.Nodes.Count())
	for n := range NodeID(5) {
		res.BindDecl(n+1, f)
	}
	res.BindDecl(6, g)
	if len(res.bound) != 2 {
		t.Errorf("the table lists %d bindings for 2 declarations", len(res.bound))
	}
}

func TestResolutionGrowsForANodeMadeAfterItWasSized(t *testing.T) {
	proj, f, _ := resolutionProject()
	var res Resolution
	res.Reset(2)
	res.BindDecl(9, f)
	if res.Decl(proj, 9) != Decl(f) || res.Decl(proj, 3) != nil {
		t.Error("the node made late is not bound, or the ones before it are")
	}
}

func TestReadingANodeOutsideTheTableIsAnInternalError(t *testing.T) {
	proj, _, _ := resolutionProject()
	var res Resolution
	res.Reset(2)
	defer func() {
		if _, ok := recover().(*diag.InternalError); !ok {
			t.Error("a node outside the table did not panic with an internal error")
		}
	}()
	res.Decl(proj, 3)
}
