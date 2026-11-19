package ast

import (
	"minimaxon/internal/diag"
	"sort"
)

type Module struct {
	Path     string
	Dir      string
	IsStdlib bool
	Files    []string
	Decls    []Decl
	Names    map[string]Decl
	Children map[string]*Module
	DepSites map[*Module]Node
	Errors   []*diag.CompileError

	Functions []*FuncDecl
	Pending   []PendingCall
	Bounds    []PendingBound
	// Callers are the functions that call a generic or go through a bound, which instances start from.
	Callers []*FuncDecl
}

func NewModule(path, dir string, isStdlib bool) *Module {
	return &Module{
		Path: path, Dir: dir, IsStdlib: isStdlib,
		Names: map[string]Decl{}, Children: map[string]*Module{}, DepSites: map[*Module]Node{},
	}
}

func (m *Module) Display() string {
	switch {
	case m.IsStdlib:
		return "stdlib"
	case m.Path == "":
		return "the project root"
	}
	return m.Path
}

func (m *Module) ChildNames() []string {
	names := make([]string, 0, len(m.Children))
	for n := range m.Children {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

type PendingCall struct {
	Call   *CallExpr
	Callee string
	Params []ParamSpec
}

// PendingBound is a type argument that its type parameter's bounds have not yet been checked against. The bounds
// belong to a declaration that another module may still be declaring, so the check waits for the verify phase.
type PendingBound struct {
	Site  Node
	Param *TypeParamTy
	Arg   Ty
	// Method is the method whose own bounds on Param apply, or nil where Param's bounds do.
	Method *FuncDecl
	// Within is the function the site is in when it has bounds of its own on its type's parameters, which the site may
	// rely on, or nil.
	Within *FuncDecl
}

type Project struct {
	Root   *Module
	Stdlib *Module
	// Builtins are the runtime primitives and Option, declared in Stdlib.
	Builtins *Builtins
	Modules  []*Module
	ByName   map[string][]Decl
}

func NewProject(root *Module) *Project {
	p := &Project{Root: root, Stdlib: NewModule("stdlib", "", true), ByName: map[string][]Decl{}}
	for _, b := range []struct {
		name     string
		min, max I128
	}{
		{"ExitCode", Zero, U64(0xFFFFFFFF)},
		{"Count", Zero, U64Max},
		{"FullRangeInt", I64Min, I64Max},
	} {
		alias := NewAliasDecl()
		alias.Name = b.name
		alias.Visibility = "public"
		alias.Module = p.Stdlib
		alias.File = "<stdlib>"
		alias.Ty = &IntTy{Alias: b.name, Min: b.min, Max: b.max}
		p.Stdlib.Decls = append(p.Stdlib.Decls, alias)
		p.Stdlib.Names[b.name] = alias
	}
	p.Builtins = NewBuiltins(p.Stdlib, p.Stdlib.Names["Count"].(*AliasDecl).Ty.(*IntTy))
	return p
}

func Methods(d Decl) []*FuncDecl {
	return VisitDeclResult[[]*FuncDecl](methods{}, d)
}

type methods struct{}

func (methods) VisitTypeDecl(t *TypeDecl) []*FuncDecl { return t.Methods }

func (methods) VisitEnumDecl(t *EnumDecl) []*FuncDecl { return t.Methods }

func (methods) VisitUnionDecl(t *UnionDecl) []*FuncDecl { return t.Methods }

func (methods) VisitFuncDecl(*FuncDecl) []*FuncDecl { return nil }

func (methods) VisitConstDecl(*ConstDecl) []*FuncDecl { return nil }

func (methods) VisitAliasDecl(*AliasDecl) []*FuncDecl { return nil }

func (methods) VisitInterfaceDecl(*InterfaceDecl) []*FuncDecl { return nil }
