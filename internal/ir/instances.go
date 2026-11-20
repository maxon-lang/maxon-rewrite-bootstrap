package ir

import (
	"fmt"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
)

// Instances finds the instances of generic functions that one module needs, which is the code that lowering has to make
// once for each, in that module's unit. It starts from the calls in the module's functions that are not generic, and
// follows the calls in each instance's body, whichever module declares the generic, with the instance's type arguments
// bound, until no new instance turns up. The result depends only on the module and on the generics it reaches, never on
// which other modules exist or what they use, so a module's output is the same in any project that gives its imports
// the same declarations; another module that needs the same instance finds it for itself and the link keeps one copy.
// The instances are in the order they are found, so the result does not depend on how many modules lower at once.
//
// callers are the module's functions that call a generic or go through a bound, as the check pass recorded them.
func Instances(callers []*ast.FuncDecl) *InstanceSet {
	found := &InstanceSet{}
	visit := func(uses []*ast.CallExpr, b ast.Binding) {
		for _, call := range uses {
			target := call.Resolved(b)
			if target.Generic == nil || found.has(target) {
				continue
			}
			found.add(target)
			requireFinite(call, target)
			if target.Module.IsStdlib {
				panic(diag.Internal("%s is a generic of the standard library, which has no body to lower", target.Symbol()))
			}
		}
	}
	for _, f := range callers {
		if len(f.Generics) == 0 {
			visit(f.Uses, ast.Binding{})
		}
	}
	for next := 0; next < len(found.List); next++ {
		instance := found.List[next]
		visit(instance.Generic.Uses, ast.Binding{Params: instance.Generic.Generics, Args: instance.TypeArgs})
	}
	return found
}

// InstanceSet is the instances one module needs, in the order they were found.
type InstanceSet struct {
	List []*ast.FuncDecl
	set  map[*ast.FuncDecl]struct{}
}

// add notes an instance as found. The set is made when the first one is, so a module with no generics allocates nothing.
func (s *InstanceSet) add(instance *ast.FuncDecl) {
	if s.set == nil {
		s.set = map[*ast.FuncDecl]struct{}{}
	}
	s.set[instance] = struct{}{}
	s.List = append(s.List, instance)
}

// has is whether the module found the instance. A module that lowers a call of an instance it did not find would
// depend on another module having made it, so lowering refuses it.
func (s *InstanceSet) has(instance *ast.FuncDecl) bool {
	if s == nil {
		return false
	}
	_, found := s.set[instance]
	return found
}

// requireFinite refuses an instance whose type arguments nest without end: a generic function that calls itself with
// a larger type each time would otherwise never run out of instances. Every module that reaches such an instance
// refuses it at the call that asks for the too deep one, so which module found it first does not matter.
func requireFinite(call *ast.CallExpr, instance *ast.FuncDecl) {
	for _, a := range instance.TypeArgs {
		if ast.TypeDepth(a) > ast.MaxTypeDepth {
			panic(call.Error(diag.ErrInstantiationTooDeep, fmt.Sprintf(
				"'%s' needs a type that nests more than %d levels deep, so the generic grows its own type argument without end; pass the same type argument on, or end the recursion",
				instance.Generic.Symbol(), ast.MaxTypeDepth)))
		}
	}
}
