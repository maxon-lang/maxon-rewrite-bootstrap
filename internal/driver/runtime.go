package driver

import (
	"fmt"
	"strings"

	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"minimaxon/internal/ir"
)

// checkRuntimeFunctions holds the runtime's source to the functions the compiler calls in it (ir.RtFunction): each is
// declared, with the table's signature, and is a plain function that throws nothing, so that a call the compiler emits,
// which passes words on the stack and takes a word back as a call of any function does, means what the function says.
// It runs when the project is the runtime itself (`minimaxon check runtime`), since the Go compiler links its own runtime
// and never this one.
func checkRuntimeFunctions(proj *ast.Project) []*diag.CompileError {
	var errs []*diag.CompileError
	for _, r := range ir.RtFunctions {
		want, _ := r.Function()
		diag.Guard(&errs, func() { checkRuntimeFunction(proj, r, want) })
	}
	return sortedErrors(proj, errs)
}

func checkRuntimeFunction(proj *ast.Project, r ir.Rt, want *ir.RtFunction) {
	root := proj.Root
	f, ok := root.Names[want.Name].(*ast.FuncDecl)
	if !ok {
		panic(diag.NewError(diag.ErrUnknownFunction, fmt.Sprintf("the runtime declares no function '%s', which the compiler calls for %s; declare it as '%s'", want.Name, r, runtimeSignature(proj.Builtins, want)), root.Dir, 1, 1))
	}
	if f.LinkName != want.Symbol {
		panic(diag.Internal("driver: the runtime's '%s' links as %s, where the compiler calls %s", f.Name, f.LinkName, want.Symbol))
	}
	result := f.ReturnType
	if result == nil {
		result = ast.VoidTy
	}
	matches := f.Generic == nil && f.ErrorType == nil && len(f.ParamTypes) == len(want.Params) &&
		result == proj.Builtins.TableType(want.Result)
	for i := 0; matches && i < len(want.Params); i++ {
		p := want.Params[i]
		matches = f.ParamTypes[i] == proj.Builtins.TableType(p.Type) && f.Pass.Specs[i].Conv == p.Conv
	}
	if !matches {
		panic(f.Error(diag.ErrTypeMismatch, fmt.Sprintf("the compiler calls '%s' for %s as '%s', which throws nothing; declare it with that signature", f.Name, r, runtimeSignature(proj.Builtins, want))))
	}
}

// runtimeSignature is how the runtime's source declares a function the compiler calls.
func runtimeSignature(b *ast.Builtins, f *ir.RtFunction) string {
	params := make([]string, len(f.Params))
	for i, p := range f.Params {
		conv := ""
		if p.Conv != ast.ConvLet {
			conv = p.Conv.String() + " "
		}
		params[i] = conv + p.Name + " " + b.TableType(p.Type).Name()
	}
	sig := "function " + f.Name + "(" + strings.Join(params, ", ") + ")"
	if result := b.TableType(f.Result); result != ast.VoidTy {
		sig += " returns " + result.Name()
	}
	return sig
}
