package driver

import (
	"slices"
	"strings"

	"minimaxon/internal/ast"
	"minimaxon/internal/check"
	"minimaxon/internal/diag"
	"minimaxon/internal/par"
	"minimaxon/internal/project"
	"minimaxon/internal/resolve"
	"minimaxon/internal/syntax"
)

// The front of a module is five tasks of the phase, each waiting only for its own module's task before it and for the
// tasks of the modules whose results it reads (README, "The modules phase"):
//
//   - parse reads the module's files and builds its declaration table. It reads nothing of another module. It then knows the
//     modules whose tables resolving the module may read, which are the modules its qualified names pass through.
//   - resolve reads the tables of those modules, and so waits for their parse. What it binds gives the module's
//     dependencies, the modules whose declarations it names, which the tasks after it wait for.
//   - aliases gives each typealias of an instance its type, which reads the typealiases it names, in its own module (in
//     the order they name each other) or in the modules it depends on, whose aliases it waits for.
//   - declare reads only the module's own syntax, the type objects of the tables, and the typealiases' types: another
//     module's declare can still be running.
//   - constants types the module's constants, each after the ones of its module that it names; a constant can read the
//     declarations and constants of the modules the module depends on, whose constants it waits for, and they waited for
//     their declare.
//
// The check after them waits for the module's constants, so it reads the declarations and constants of every module it
// reaches.

// parse reads a module and builds its declaration table while its AST is in cache, then gives its resolve the parses of
// the modules it names to wait for. A module's table is built only when it parsed cleanly, and the table's errors count
// only when every module did, as stageTable comes after stageSyntax.
func (p *modulePhase) parse(w *workerScratch, id ast.ModuleID, edges par.Edges) {
	m, res := p.proj.Modules[id], &p.results[id]
	p.stage(w, id, stageSyntax, func() {
		if w.syntax == nil {
			w.syntax = syntax.NewScratch()
		}
		diag.Guard(&res.Errors, func() {
			src := w.syntax.LoadModuleSource(m.Files)
			program, syntaxErrors := w.syntax.Parse(src, w.syntax.Lex(src), m.ID)
			m.Adopt(program)
			res.Errors = append(res.Errors, syntaxErrors...)
		})
	})
	tabled := false
	p.stage(w, id, stageTable, func() {
		project.BuildModuleTable(m, &res.Errors)
		tabled = true
	})
	if !tabled {
		return
	}
	named := resolve.NamedModules(p.proj, m)
	parses := make([]int, len(named))
	for i, n := range named {
		parses[i] = p.layout.task(n, taskParse)
	}
	edges.Add(p.layout.task(id, taskResolve), parses...)
}

// resolve binds the module's names, and gives the module's tasks that read other modules the same tasks of the modules
// it depends on to wait for. It also sizes the tables of the module's Typing that the check fills in, while the module's
// tree is in cache: the bodies are checked many modules at a time, and allocating the tables of a large project there
// would make them wait for the heap one after another.
func (p *modulePhase) resolve(w *workerScratch, id ast.ModuleID, edges par.Edges) {
	m, res := p.proj.Modules[id], &p.results[id]
	p.stage(w, id, stageResolve, func() {
		res.Typing.Size(m.Tree)
		resolve.NewResolver(p.proj, m, res).Run()
	})
	p.layout.dependOn(edges, id, res.Deps)
}

// declare declares the module's members and signatures, and for the root finds main. It also numbers the module's nodes
// that have a slot, now that resolve has changed the kind of some, and sizes the tables of the Typing that only some
// nodes have.
func (p *modulePhase) declare(w *workerScratch, id ast.ModuleID) {
	m, res := p.proj.Modules[id], &p.results[id]
	p.stage(w, id, stageDeclare, func() {
		res.Typing.Reset(m.Tree)
		checker := check.NewChecker(p.proj, m, &res.Resolution, &res.Typing, &res.Errors)
		for _, d := range m.Decls {
			res.Functions = append(res.Functions, checker.Declare(d)...)
		}
		res.Bounds = append(res.Bounds, checker.Bounds...)
		if m == p.proj.Root {
			p.main = checker.CheckMain()
		}
	})
}

// constants orders the module's constants, then types them in that order.
func (p *modulePhase) constants(w *workerScratch, id ast.ModuleID) {
	m, res := p.proj.Modules[id], &p.results[id]
	var order []*ast.ConstDecl
	p.stage(w, id, stageConstOrder, func() {
		diag.Guard(&res.Errors, func() { order = constantOrder(p.proj, m, res) })
	})
	p.stage(w, id, stageConstants, func() {
		for _, c := range order {
			check.NewChecker(p.proj, m, &res.Resolution, &res.Typing, &res.Errors).CheckConst(c)
		}
	})
}

// constantOrder is the module's constants, each after those of the module that its value names, so that each is typed
// after them; a constant that names itself through others is refused. The constants of other modules that it names are
// typed already, since the module graph has no cycle and the module's constants wait for those of the modules it depends
// on.
func constantOrder(proj *ast.Project, m *ast.Module, res *ast.ModuleResult) []*ast.ConstDecl {
	const visiting, done = 1, 2
	// state is by the constant's place among the module's declarations.
	state := make([]byte, len(m.Decls))
	refs := constRefs{proj, &res.Resolution}
	var order []*ast.ConstDecl
	var path []*ast.ConstDecl
	var visit func(c *ast.ConstDecl)
	visit = func(c *ast.ConstDecl) {
		switch state[c.Ordinal] {
		case done:
			return
		case visiting:
			loop := path[slices.Index(path, c):]
			names := make([]string, len(loop))
			for i, l := range loop {
				names[i] = l.Name
			}
			panic(c.Error(diag.ErrDependsOnItself, diag.CircularConstants(strings.Join(names, ", "))))
		}
		state[c.Ordinal] = visiting
		path = append(path, c)
		for _, named := range refs.of(c.Value, nil) {
			if named.Module == m {
				visit(named)
			}
		}
		path = path[:len(path)-1]
		state[c.Ordinal] = done
		order = append(order, c)
	}
	for _, d := range m.Decls {
		if c, ok := d.(*ast.ConstDecl); ok {
			visit(c)
		}
	}
	return order
}

type constList = []*ast.ConstDecl

// of appends the constants that e names, so that they are typed first. It walks every expression a constant could hold,
// including ones the checker then refuses, because a missed reference would type a constant before one it reads.
func (r constRefs) of(e ast.Expr, into constList) constList {
	return ast.VisitExprState[constList, constList](r, e, into)
}

// constRefs reads what the module that holds the expression resolved its names to.
type constRefs struct {
	proj       *ast.Project
	resolution *ast.Resolution
}

func (r constRefs) VisitIntLit(_ ast.IntLit, into constList) constList { return into }

func (r constRefs) VisitBoolLit(_ ast.BoolLit, into constList) constList { return into }

func (r constRefs) VisitSelfExpr(_ ast.SelfExpr, into constList) constList { return into }

func (r constRefs) VisitStrLit(e ast.StrLit, into constList) constList {
	for _, p := range e.Parts().All() {
		if !p.Expr.IsNil() {
			into = r.of(p.Expr, into)
		}
	}
	return into
}

func (r constRefs) VisitNameExpr(e ast.NameExpr, into constList) constList {
	if c, ok := r.resolution.Decl(r.proj, e.ID).(*ast.ConstDecl); ok {
		return append(into, c)
	}
	return into
}

func (r constRefs) VisitBinaryExpr(e ast.BinaryExpr, into constList) constList {
	return r.of(e.R(), r.of(e.L(), into))
}

func (r constRefs) VisitUnaryExpr(e ast.UnaryExpr, into constList) constList {
	return r.of(e.E(), into)
}

func (r constRefs) VisitTryExpr(e ast.TryExpr, into constList) constList {
	into = r.of(e.E(), into)
	if !e.Value().IsNil() {
		into = r.of(e.Value(), into)
	}
	return into
}

func (r constRefs) VisitCondExpr(e ast.CondExpr, into constList) constList {
	return r.of(e.Else(), r.of(e.Then(), r.of(e.Cond(), into)))
}

func (r constRefs) VisitCastExpr(e ast.CastExpr, into constList) constList {
	return r.of(e.E(), into)
}

func (r constRefs) VisitIndexExpr(e ast.IndexExpr, into constList) constList {
	into = r.of(e.Obj(), into)
	for _, a := range e.Args().All() {
		into = r.of(a, into)
	}
	return into
}

func (r constRefs) VisitMemberExpr(e ast.MemberExpr, into constList) constList {
	return r.of(e.Obj(), into)
}

func (r constRefs) VisitCallExpr(e ast.CallExpr, into constList) constList {
	into = r.of(e.Callee(), into)
	for _, a := range e.Args().All() {
		into = r.of(a.Value(), into)
	}
	return into
}

func (r constRefs) VisitStructLitExpr(e ast.StructLitExpr, into constList) constList {
	for _, f := range e.Fields().All() {
		into = r.of(f.Value(), into)
	}
	return into
}

// A match expression's arms all give a value; a 'then' arm, the only one holding a statement, is refused in one.
func (r constRefs) VisitMatchExpr(e ast.MatchExpr, into constList) constList {
	into = r.of(e.Match().Subject(), into)
	for _, arm := range e.Match().Arms().All() {
		for _, p := range arm.Patterns().All() {
			if !p.Value().IsNil() {
				into = r.of(p.Value(), into)
			}
			if !p.High().IsNil() {
				into = r.of(p.High(), into)
			}
		}
		if !arm.Value().IsNil() {
			into = r.of(arm.Value(), into)
		}
	}
	return into
}

func (r constRefs) VisitPanicExpr(e ast.PanicExpr, into constList) constList {
	return r.of(e.Message().Expr(), into)
}
