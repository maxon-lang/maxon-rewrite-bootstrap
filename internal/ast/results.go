package ast

import "minimaxon/internal/diag"

// ModuleResult is what the phases produce for one module, as opposed to what describes it (Module). The driver owns one
// per module, indexed by ModuleID: each phase fills in the result of the module it works on and only reads the results
// of other modules that an earlier phase finished. It lives here because the resolver, the checker and the instance
// collector all use it and this is the lowest package they share.
type ModuleResult struct {
	Errors []*diag.CompileError
	// Deps are the modules this one names a declaration of, by handle in ascending order, each with the first place
	// it is named. The standard library is not a module of the project and is never a dependency.
	Deps      []ModuleDep
	Functions []*FuncDecl
	Pending   []PendingCall
	Bounds    []PendingBound
	// Callers are the functions that call a generic or go through a bound, which instances start from.
	Callers []*FuncDecl
}

// ModuleDep is an edge of the module graph: the module that is depended on, and a place that names it, which is where
// an import cycle through the edge is reported.
type ModuleDep struct {
	Module ModuleID
	Site   Node
}

type PendingCall struct {
	Call   *CallExpr
	Callee string
	Params []ParamSpec
}

// PendingBound is a type argument that its type parameter's bounds have not yet been checked against. The bounds
// belong to a declaration that another module may still be declaring, so the check waits for the verify stage.
type PendingBound struct {
	Site  Node
	Param *TypeParamTy
	Arg   Ty
	// Method is the method whose own bounds on Param apply, or nil where Param's bounds do.
	Method *FuncDecl
	// Within is the function the site is in when it has bounds of its own on its type's parameters, which the site may
	// rely on, or nil.
	Within *FuncDecl
}
