package check

import (
	"slices"
	"strings"

	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
)

// aliasCycle spells the loop that visiting a closes, from the first alias of path that is a: one name for an alias that
// names itself, else each alias in turn and the first again.
func aliasCycle(path []*ast.AliasDecl, a *ast.AliasDecl) string {
	loop := path[slices.Index(path, a):]
	names := make([]string, len(loop), len(loop)+1)
	for i, l := range loop {
		names[i] = l.Name
	}
	if len(names) > 1 {
		names = append(names, names[0])
	}
	return strings.Join(names, " -> ")
}

// DeclareAliases gives each typealias of an instance that the module declares its type, after the typealiases it names.
// A typealias of another module that it names is done already: the module graph has no cycle, and the module's aliases
// stage waits for those of the modules it names. The module's own go in source order, each after the ones it names, and a
// typealias that names itself through others is refused. Errors and the type arguments' pending bounds go to the module's
// result.
func DeclareAliases(proj *ast.Project, m *ast.Module, res *ast.ModuleResult) {
	const visiting, done = 1, 2
	// state is by the typealias's place among the module's declarations.
	state := make([]byte, len(m.Decls))
	var checker *Checker
	var path []*ast.AliasDecl
	var visit func(a *ast.AliasDecl)
	visit = func(a *ast.AliasDecl) {
		if a.Module != m {
			return
		}
		switch state[a.Ordinal] {
		case done:
			return
		case visiting:
			panic(a.Error(diag.ErrDependsOnItself, diag.CircularTypealias(aliasCycle(path, a))))
		}
		state[a.Ordinal] = visiting
		path = append(path, a)
		defer func() {
			state[a.Ordinal] = done
			path = path[:len(path)-1]
		}()
		for _, dep := range namedAliases(proj, &res.Resolution, a.Instance, nil) {
			visit(dep)
			if dep.Ty == nil {
				return
			}
		}
		if checker == nil {
			checker = NewChecker(proj, m, &res.Resolution, &res.Typing, &res.Errors)
		}
		checker.DeclareAlias(a)
	}
	for _, d := range m.Decls {
		if a, ok := d.(*ast.AliasDecl); ok && !a.Instance.IsNil() {
			diag.Guard(&res.Errors, func() { visit(a) })
		}
	}
	if checker != nil {
		res.Bounds = append(res.Bounds, checker.Bounds...)
	}
}

// namedAliases appends the typealiases of instances that a type reference names, itself or in its type arguments.
func namedAliases(proj *ast.Project, resolution *ast.Resolution, tr ast.TypeRef, out []*ast.AliasDecl) []*ast.AliasDecl {
	if a, ok := resolution.Decl(proj, tr.ID).(*ast.AliasDecl); ok && !a.Instance.IsNil() {
		out = append(out, a)
	}
	for _, arg := range tr.TypeArgs().All() {
		out = namedAliases(proj, resolution, arg, out)
	}
	return out
}
