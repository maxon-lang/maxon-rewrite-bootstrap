package check

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
)

// DeclareAliases gives each typealias of an instance its type, after the typealiases it names. It is serial and cheap:
// there are few such typealiases, and one may name another's in any module. Modules and declarations go in source
// order, so the instances are interned in the same order whatever --jobs is.
func DeclareAliases(proj *ast.Project) {
	const visiting, done = 1, 2
	state := map[*ast.AliasDecl]byte{}
	checkers := map[*ast.Module]*Checker{}
	checkerOf := func(m *ast.Module) *Checker {
		if checkers[m] == nil {
			checkers[m] = NewChecker(proj, m, &m.Errors)
		}
		return checkers[m]
	}
	var visit func(a *ast.AliasDecl)
	visit = func(a *ast.AliasDecl) {
		switch state[a] {
		case done:
			return
		case visiting:
			panic(a.Error(diag.ErrDependsOnItself, "typealias '"+a.Name+"' names itself, through the typealiases it names: an instance cannot be its own type argument"))
		}
		state[a] = visiting
		defer func() { state[a] = done }()
		for _, dep := range namedAliases(a.Instance, nil) {
			visit(dep)
			if dep.Ty == nil {
				return
			}
		}
		checkerOf(a.Module).DeclareAlias(a)
	}
	for _, m := range proj.Modules {
		for _, d := range m.Decls {
			if a, ok := d.(*ast.AliasDecl); ok && a.Instance != nil {
				diag.Guard(&m.Errors, func() { visit(a) })
			}
		}
	}
	for _, m := range proj.Modules {
		if c := checkers[m]; c != nil {
			m.Bounds = append(m.Bounds, c.Bounds...)
		}
	}
}

// namedAliases appends the typealiases of instances that a type reference names, itself or in its type arguments.
func namedAliases(tr *ast.TypeRef, out []*ast.AliasDecl) []*ast.AliasDecl {
	if a, ok := tr.Target.(*ast.AliasDecl); ok && a.Instance != nil {
		out = append(out, a)
	}
	for _, arg := range tr.TypeArgs {
		out = namedAliases(arg, out)
	}
	return out
}
