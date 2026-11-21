package ast

import (
	"minimaxon/internal/diag"
	"sort"
)

// ModuleID is a handle to a module: its index in Project.Modules, which lists the root first and then each module's
// children in name order, depth first, so the order is the same for every run.
// Anything that refers to a module without owning it, such as a dependency, a position or a per-module result, holds
// this. It is 32 bits, so a position names its module in half of its 8 bytes.
type ModuleID int32

// StdlibID is the handle of the standard library, which is not one of the project's modules: no per-module result has
// an index for it, so using it as one fails instead of reading the root's.
const StdlibID ModuleID = -1

type Module struct {
	ID       ModuleID
	Path     string
	Dir      string
	IsStdlib bool
	Files    []string
	// Decls are the module's top-level declarations in source order; a declaration's Ordinal is its index here.
	Decls []Decl
	// Tree holds the module's nodes, which are all there are: no phase adds one, so a table sized by Tree.Count has an
	// entry for every node.
	Tree *Tree
	// Positions locate the offsets of the module's nodes.
	Positions *Positions
	Names     map[string]Decl
	Children  map[string]*Module
}

// Adopt makes what parsing the module produced the module's own.
func (m *Module) Adopt(p *Program) {
	m.Decls, m.Tree, m.Positions = p.Decls, p.Tree, p.Positions
}

// Declare adds a declaration that is not parsed, such as the standard library's, at the end of the module's Decls.
func (m *Module) Declare(d Decl) {
	d.Base().Ordinal = uint32(len(m.Decls))
	m.Decls = append(m.Decls, d)
	m.Names[d.Base().Name] = d
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

// RootQualifier and StdlibQualifier start a qualified name of the root module's and of the standard library's
// declarations; every other module is qualified by its path.
const (
	RootQualifier   = "export"
	StdlibQualifier = "stdlib"
)

// Qualifier is what another module writes before the name of one of the module's declarations.
func (m *Module) Qualifier() string {
	switch {
	case m.IsStdlib:
		return StdlibQualifier
	case m.Path == "":
		return RootQualifier
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
}

// Module is the module a handle names, the standard library included.
func (p *Project) Module(id ModuleID) *Module {
	if id == StdlibID {
		return p.Stdlib
	}
	return p.Modules[id]
}

// Decl is the declaration a reference names.
func (p *Project) Decl(ref DeclRef) Decl { return p.Module(ref.Module).Decls[ref.Ordinal] }

// Where is the file, line and column of a position.
func (p *Project) Where(pos Pos) (file string, line, col int) {
	return p.Module(pos.Mod).Positions.Where(pos.Off)
}

// Line is the line of a position, for a message that names where something else is.
func (p *Project) Line(pos Pos) int {
	_, line, _ := p.Where(pos)
	return line
}

// Locate gives each error that was raised at a node its file, line and column.
func (p *Project) Locate(errs []*diag.CompileError) {
	for _, e := range errs {
		if !e.Located() {
			e.Locate(p.Where(Pos{Mod: ModuleID(e.Site.Module), Off: e.Site.Offset}))
		}
	}
}

func NewProject(root *Module) *Project {
	p := &Project{Root: root, Stdlib: NewModule("stdlib", "", true)}
	p.Stdlib.ID = StdlibID
	p.Stdlib.Tree = NewTree(StdlibID, "", 0)
	p.Stdlib.Positions = stdlibPositions
	for _, b := range []struct {
		name     string
		min, max I128
	}{
		{"ExitCode", Zero, U64(0xFFFFFFFF)},
		{"Count", Zero, U64Max},
		{"FullRangeInt", I64Min, I64Max},
		{"Byte", Zero, U64(255)},
	} {
		alias := p.Stdlib.Tree.NewAliasDecl(0)
		alias.Name = b.name
		alias.Visibility = "public"
		alias.Module = p.Stdlib
		alias.Ty = &IntTy{Alias: b.name, Min: b.min, Max: b.max}
		p.Stdlib.Declare(alias)
	}
	p.Builtins = NewBuiltins(p.Stdlib, p.Stdlib.Names["Count"].(*AliasDecl).Ty.(*IntTy), p.Stdlib.Names["Byte"].(*AliasDecl).Ty.(*IntTy))
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
