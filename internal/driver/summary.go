package driver

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/cache"
	"minimaxon/internal/diag"
	"minimaxon/internal/par"
	"minimaxon/internal/project"
)

// A module's export summary is a digest of everything that another module's output can depend on, and nothing else. The
// build cache keys a module by its own source and the summaries of the modules it depends on, so an edit that leaves a
// module's summary as it was, such as the body of a function that is not generic, makes a miss of that module alone
// (README, "Build cache"). This file is the one place that says what a module shows to the others.
//
// What a module shows is what the front (declare and constants) made of its declarations, plus the source of what is
// lowered or checked again in the module that reaches it:
//
//   - every type declaration, whoever may name it: a type that a signature of the module mentions can be reached by a
//     module that cannot name it, which then reads its layout, its members and what it implements. A type is written whole:
//     its parameters and their bounds, what it implements, its fields (name, visibility, mutability, type, default), its cases
//     and their raw values and payloads, and the signature of each method;
//   - each function, a constant and a field's default that other modules can use: the signature and conventions of a function,
//     the type and the source of a constant, and the source of a field's default, which a module that builds a record
//     lowers in its own code;
//   - the source of each generic function and method, which a module that calls one lowers an instance of, so the check
//     results the instance is made from follow from that source and from what the module declares and reaches.
//
// A declaration that nothing outside the module can reach is left out: a function that is not exported, in a module that
// has no generic and no field default, and a constant that is not exported, in a module that has no exported constant and no
// generic or default. A module that shows a constant shows every constant it declares, because the value of one that is exported
// may be written with another that is not, and a module that lowers it lowers that one too. A module that has a generic or a
// default shows every function as well, because the body of a generic or a default may call any of them and a module that
// lowers it then reads the callee's signature.
//
// The digest is of values that a program's text decides and no handle or address does: a type is its link name, a module
// its link path, and every list is written in the order the source gives it, with its length, so no map's order reaches it.
// A module that depends on others folds their summaries in, so a summary holds what the module itself shows and what it
// can show through its dependencies (see foldSummary).

const (
	ownSummaryDomain  = "minimaxon export summary"
	foldSummaryDomain = "minimaxon export summary with dependencies"
)

// summarySection tags what is written next, so that two different declarations never write the same bytes.
type summarySection int

const (
	sectionFunction summarySection = iota + 1
	sectionConstant
	sectionRecord
	sectionEnum
	sectionUnion
	sectionAlias
	sectionInterface
)

// exposure says how much of a declaration other modules' generate reads beyond its signature: nothing, or the check results
// of a constant, or those of anything the module declares (a generic, which is lowered in another module, or a field's
// default, which a record literal there lowers).
type exposure int

const (
	exposesNothing exposure = iota
	exposesConstant
	exposesBodies
)

// moduleSummary is what a module shows. ReadThrough is whether another module's generate reads the module's check
// results (the bodies of generics, the marks on constants, the checked defaults of fields), which says whether the module
// has to be checked for a module that reaches it and is built.
type moduleSummary struct {
	digest      cache.Key
	readThrough bool
}

// summarize makes the summary of every module: the own part of each in parallel, then the dependencies folded in, in the
// order of the module graph so that each module's dependencies are done first.
func summarize(proj *ast.Project, graph *project.Graph, jobs int) []moduleSummary {
	n := len(proj.Modules)
	own := make([]moduleSummary, n)
	par.ForEachIndex(n, jobs, func(i int) { own[i] = ownSummary(proj.Modules[i]) })
	folded := make([]moduleSummary, n)
	for _, id := range graph.Order() {
		deps := graph.Deps[id]
		digests := make([]cache.Key, len(deps))
		for i, d := range deps {
			digests[i] = folded[d.Module].digest
		}
		folded[id] = moduleSummary{digest: foldSummary(own[id].digest, digests), readThrough: own[id].readThrough}
	}
	return folded
}

// foldSummary is the summary of a module that shows own and depends on modules with the summaries deps, in module order.
// It is transitive: a dependency's summary holds its own dependencies', so a module's summary changes when anything it can
// show through them does. That is needed because a module's signatures name the types of the modules it depends on, and a
// module that uses them reads those types' layouts and members though it names none of their modules, and because the
// check results of a generic that it reads are made from what the generic's module reaches.
func foldSummary(own cache.Key, deps []cache.Key) cache.Key {
	h := cache.NewHasher()
	h.String(foldSummaryDomain)
	h.Key(own)
	h.Int(len(deps))
	for _, d := range deps {
		h.Key(d)
	}
	return h.Sum()
}

// ownSummary is what the module shows by itself, before its dependencies are folded in.
func ownSummary(m *ast.Module) moduleSummary {
	var widest exposure
	for _, d := range m.Decls {
		widest = max(widest, ast.VisitDeclResult[exposure](exposures{}, d))
	}
	s := &shape{h: cache.NewHasher(), m: m, open: widest == exposesBodies, constants: widest != exposesNothing}
	s.h.String(ownSummaryDomain)
	s.str(m.LinkPath())
	for _, d := range m.Decls {
		ast.VisitDecl(s, d)
	}
	return moduleSummary{digest: s.h.Sum(), readThrough: widest != exposesNothing}
}

// exposures says what each declaration lets other modules read of its check results.
type exposures struct{}

func (exposures) VisitFuncDecl(f *ast.FuncDecl) exposure { return methodsExposure([]*ast.FuncDecl{f}) }

func (exposures) VisitConstDecl(c *ast.ConstDecl) exposure {
	if c.IsExported() {
		return exposesConstant
	}
	return exposesNothing
}

func (exposures) VisitTypeDecl(t *ast.TypeDecl) exposure {
	for _, f := range t.Fields {
		if !f.Default.IsNil() {
			return exposesBodies
		}
	}
	return methodsExposure(t.Methods)
}

func (exposures) VisitEnumDecl(t *ast.EnumDecl) exposure { return methodsExposure(t.Methods) }

func (exposures) VisitUnionDecl(t *ast.UnionDecl) exposure { return methodsExposure(t.Methods) }

func (exposures) VisitAliasDecl(*ast.AliasDecl) exposure { return exposesNothing }

func (exposures) VisitInterfaceDecl(*ast.InterfaceDecl) exposure { return exposesNothing }

// methodsExposure is exposesBodies if one of the functions is generic.
func methodsExposure(fs []*ast.FuncDecl) exposure {
	for _, f := range fs {
		if len(f.Generics) > 0 {
			return exposesBodies
		}
	}
	return exposesNothing
}

// shape writes the part of a module that other modules can depend on. open says that the module has a generic or a default,
// so that every function is written, not only the exported ones; constants says that it has an exported constant too, or
// is open, so that every constant is written.
type shape struct {
	h         *cache.Hasher
	m         *ast.Module
	open      bool
	constants bool
}

func (s *shape) str(text string)          { s.h.String(text) }
func (s *shape) int(n int)                { s.h.Int(n) }
func (s *shape) bool(b bool)              { s.h.Bool(b) }
func (s *shape) i128(v ast.I128)          { s.h.Int(int(v.Hi)); s.h.Int(int(v.Lo)) }
func (s *shape) section(k summarySection) { s.h.Int(int(k)) }

// source is the text of a declaration, as written.
func (s *shape) source(h ast.Head) { s.str(h.Source(s.m)) }

func (s *shape) header(d *ast.DeclBase) {
	s.str(d.Name)
	s.str(d.Visibility)
}

// ty writes a type by what generated code calls it, which is its identity: a declared type is its module and name, an
// instance its arguments too, an integer type its alias and range.
func (s *shape) ty(t ast.Ty) {
	switch x := t.(type) {
	case *ast.InterfaceTy:
		// An interface's own link name is only its name, which two modules may share.
		s.str(x.Decl.Module.LinkPath() + "::" + x.Name())
	case *ast.IntTy, *ast.ConstIntTy, *ast.PrimTy, *ast.RecordTy, *ast.EnumTy, *ast.UnionTy, *ast.BuiltinTy, *ast.TypeParamTy:
		s.str(t.LinkName())
	default:
		panic(diag.Internal("the export summary has no way to write the type %T", t))
	}
}

func (s *shape) interfaces(is []*ast.InterfaceTy) {
	s.int(len(is))
	for _, i := range is {
		s.ty(i)
	}
}

func (s *shape) typeParams(params []*ast.TypeParamTy) {
	s.int(len(params))
	for _, p := range params {
		s.str(p.Name())
		s.bool(p.Bounded())
		s.interfaces(p.Interfaces)
	}
}

func (s *shape) specs(specs []ast.ParamSpec) {
	s.int(len(specs))
	for _, p := range specs {
		s.str(p.Name)
		s.int(int(p.Conv))
		s.ty(p.Type)
	}
}

// function writes a function or a method as another module sees it. A generic is written with its source.
func (s *shape) function(f *ast.FuncDecl) {
	s.section(sectionFunction)
	s.header(&f.DeclBase)
	s.bool(f.IsStatic)
	s.str(f.Receiver)
	s.int(int(f.Intrinsic))
	s.str(f.LinkName)
	s.typeParams(f.Generics)
	s.int(len(f.Bounds))
	for _, b := range f.Bounds {
		s.str(b.Param.Name())
		s.ty(b.Iface)
	}
	s.int(int(f.Pass.SelfConv))
	s.specs(f.Pass.Specs)
	s.ty(f.ReturnType)
	s.bool(f.ErrorType != nil)
	generic := len(f.Generics) > 0
	s.bool(generic)
	if generic {
		s.source(f.Head)
	}
}

func (s *shape) methods(methods []*ast.FuncDecl) {
	s.int(len(methods))
	for _, m := range methods {
		s.function(m)
	}
}

func (s *shape) VisitFuncDecl(f *ast.FuncDecl) {
	if s.open || f.IsExported() {
		s.function(f)
	}
}

func (s *shape) VisitConstDecl(c *ast.ConstDecl) {
	if !s.constants {
		return
	}
	s.section(sectionConstant)
	s.header(&c.DeclBase)
	s.ty(c.Type)
	s.source(c.Head)
}

func (s *shape) VisitTypeDecl(t *ast.TypeDecl) {
	rt := t.Ty.(*ast.RecordTy)
	if len(rt.Fields) != len(t.Fields) {
		panic(diag.Internal("record %s declares %d fields and has %d", t.Name, len(t.Fields), len(rt.Fields)))
	}
	s.section(sectionRecord)
	s.header(&t.DeclBase)
	s.bool(t.Noncopyable)
	s.typeParams(rt.Params)
	s.interfaces(rt.Conforms)
	s.int(len(rt.Fields))
	for i, f := range rt.Fields {
		s.str(f.Name)
		s.bool(f.Exported)
		s.bool(f.IsVar)
		s.ty(f.Type)
		hasDefault := !f.Default.IsNil()
		s.bool(hasDefault)
		if hasDefault {
			s.source(t.Fields[i].Head)
		}
	}
	s.methods(t.Methods)
}

func (s *shape) VisitEnumDecl(t *ast.EnumDecl) {
	et := t.Ty.(*ast.EnumTy)
	s.section(sectionEnum)
	s.header(&t.DeclBase)
	s.interfaces(et.Conforms)
	s.bool(et.HasExplicitRaw)
	s.int(len(et.Cases))
	for i, c := range et.Cases {
		s.str(c)
		s.i128(et.Raw[i])
	}
	s.methods(t.Methods)
}

func (s *shape) VisitUnionDecl(t *ast.UnionDecl) {
	ut := t.Ty.(*ast.UnionTy)
	s.section(sectionUnion)
	s.header(&t.DeclBase)
	s.bool(t.Noncopyable)
	s.typeParams(ut.Params)
	s.interfaces(ut.Conforms)
	s.int(len(ut.Cases))
	for _, c := range ut.Cases {
		s.str(c.Name)
		s.int(len(c.Payload))
		for _, p := range c.Payload {
			s.str(p.Name)
			s.ty(p.Type)
		}
	}
	s.methods(t.Methods)
}

func (s *shape) VisitAliasDecl(t *ast.AliasDecl) {
	s.section(sectionAlias)
	s.header(&t.DeclBase)
	s.ty(t.Ty)
}

func (s *shape) VisitInterfaceDecl(t *ast.InterfaceDecl) {
	s.section(sectionInterface)
	s.header(&t.DeclBase)
	s.int(len(t.Methods))
	for _, m := range t.Methods {
		s.str(m.Name)
		s.int(int(m.Pass.SelfConv))
		s.specs(m.Pass.Specs)
		s.ty(m.ReturnType)
		s.bool(m.ErrorType != nil)
	}
}
