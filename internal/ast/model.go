package ast

import "sort"

// ModuleID is a handle to a module: its index in Project.Modules, which lists the root first and then each module's
// children in name order, depth first, so the order is the same for every run.
// Anything that refers to a module without owning it, such as a dependency or a per-module result, holds this.
type ModuleID int

// StdlibID is the handle of the standard library, which is not one of the project's modules: no per-module result has
// an index for it, so using it as one fails instead of reading the root's.
const StdlibID ModuleID = -1

type Module struct {
	ID       ModuleID
	Path     string
	Dir      string
	IsStdlib bool
	Files    []string
	Decls    []Decl
	Names    map[string]Decl
	Children map[string]*Module
}

func NewModule(path, dir string, isStdlib bool) *Module {
	return &Module{
		Path: path, Dir: dir, IsStdlib: isStdlib,
		Names: map[string]Decl{}, Children: map[string]*Module{},
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

type Project struct {
	Root   *Module
	Stdlib *Module
	// Builtins are the runtime primitives and Option, declared in Stdlib.
	Builtins *Builtins
	// Modules lists every module of the project, indexed by ModuleID.
	Modules []*Module
	ByName  map[string][]Decl
}

func NewProject(root *Module) *Project {
	p := &Project{Root: root, Stdlib: NewModule("stdlib", "", true), ByName: map[string][]Decl{}}
	p.Stdlib.ID = StdlibID
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
		alias.Src = stdlibPositions
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
