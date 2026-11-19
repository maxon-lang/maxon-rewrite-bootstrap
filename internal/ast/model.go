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

type Project struct {
	Root    *Module
	Stdlib  *Module
	Modules []*Module
	ByName  map[string][]Decl
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
		alias := &AliasDecl{Min: b.min, Max: b.max}
		alias.Name = b.name
		alias.Visibility = "public"
		alias.Module = p.Stdlib
		alias.File = "<stdlib>"
		alias.Ty = &IntTy{Alias: b.name, Min: b.min, Max: b.max}
		p.Stdlib.Decls = append(p.Stdlib.Decls, alias)
		p.Stdlib.Names[b.name] = alias
	}
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
