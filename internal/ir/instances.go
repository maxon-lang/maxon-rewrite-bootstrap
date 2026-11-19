package ir

import (
	"fmt"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
)

// Instances finds every instance of a generic function that the program reaches, which is the code that lowering has
// to make once for each. It starts from the calls in the functions that are not generic, and follows the calls in
// each instance's body with the instance's type arguments bound, until no new instance turns up. An instance is
// listed under the module that declares the generic, in the order it was found, so the result does not depend on
// how many modules lower at once.
func Instances(modules []*ast.Module) [][]*ast.FuncDecl {
	index := make(map[*ast.Module]int, len(modules))
	for i, m := range modules {
		index[m] = i
	}
	found := make([][]*ast.FuncDecl, len(modules))
	var queue []*ast.FuncDecl
	visit := func(uses []*ast.CallExpr, b ast.Binding) {
		for _, call := range uses {
			target, created := call.Resolved(b)
			if !created {
				continue
			}
			requireFinite(call, target)
			i := index[target.Module]
			found[i] = append(found[i], target)
			queue = append(queue, target)
		}
	}
	for _, m := range modules {
		for _, f := range m.Callers {
			if len(f.Generics) == 0 {
				visit(f.Uses, ast.Binding{})
			}
		}
	}
	for next := 0; next < len(queue); next++ {
		instance := queue[next]
		visit(instance.Generic.Uses, ast.Binding{Params: instance.Generic.Generics, Args: instance.TypeArgs})
	}
	return found
}

// requireFinite refuses an instance whose type arguments nest without end: a generic function that calls itself with
// a larger type each time would otherwise never run out of instances.
func requireFinite(call *ast.CallExpr, instance *ast.FuncDecl) {
	for _, a := range instance.TypeArgs {
		if ast.TypeDepth(a) > ast.MaxTypeDepth {
			panic(call.Error(diag.ErrInstantiationTooDeep, fmt.Sprintf(
				"'%s' needs a type that nests more than %d levels deep, so the generic grows its own type argument without end; pass the same type argument on, or end the recursion",
				instance.Generic.Symbol(), ast.MaxTypeDepth)))
		}
	}
}
