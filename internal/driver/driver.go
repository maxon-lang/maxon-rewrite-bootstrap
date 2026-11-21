package driver

import (
	"fmt"
	"minimaxon/internal/ast"
	"minimaxon/internal/cache"
	"minimaxon/internal/check"
	"minimaxon/internal/diag"
	"minimaxon/internal/interp"
	"minimaxon/internal/ir"

	"minimaxon/internal/par"
	"minimaxon/internal/project"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/trace"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
)

const (
	bytesAllocatedPerSourceByte = 100
	minNoGcBudget               = int64(256) << 20
	bytesPerMegabyte            = 1048576.0
)

func DefaultJobs() int { return max(1, runtime.NumCPU()-1) }

// Options say how a program is compiled to an executable.
type Options struct {
	// Jobs is how many modules compile at once; the executable is the same for any value.
	Jobs int
	// Timings prints each phase's wall time, busy cores and allocation to standard error.
	Timings bool
	// Trace, when not empty, is where a Go execution trace of the compile is written, for `go tool trace`.
	Trace string
	// EmitIr writes the IR of every function beside the executable, as <output>.ir.
	EmitIr bool
	// CountAllocations makes the executable count its live heap boxes and fail at exit if any is left (README, "Counting
	// allocations"): a development flag, so a normal executable pays nothing for it.
	CountAllocations bool
	// Cache is the build cache that a compile that makes an executable reads and writes (README, "Build cache"); nil means
	// there is none.
	Cache *cache.Cache
	// CacheStats, when not nil, is filled in with what the compile did with Cache.
	CacheStats *CacheStats
}

func Compile(path, output string, o Options) []*diag.CompileError {
	if o.Trace != "" {
		defer startTrace(o.Trace)()
	}
	return compileProject(path, output, o, nil)
}

func startTrace(path string) (stop func()) {
	file, err := os.Create(path)
	if err != nil {
		panic(diag.Internal("cannot create the trace %s: %v", path, err))
	}
	if err := trace.Start(file); err != nil {
		panic(diag.Internal("cannot start the trace: %v", err))
	}
	return func() {
		trace.Stop()
		if err := file.Close(); err != nil {
			panic(diag.Internal("cannot write the trace %s: %v", path, err))
		}
	}
}

// Prepare compiles a program as far as IR, which the tracking interpreter (internal/interp) runs. The program can be run
// more than once, under different schedules of its behaviours.
func Prepare(path string, jobs int) (program *interp.Program, errs []*diag.CompileError) {
	errs = compileProject(path, "", Options{Jobs: jobs}, &program)
	if len(errs) > 0 {
		return nil, errs
	}
	return program, nil
}

// Interpret compiles a program as far as IR and runs it under the tracking interpreter, which gives a map, a list, a
// pool, a frozen value, a cown and a behaviour their meaning and reports a violation of the memory model. It is for
// tests and development: the interpreter is never part of a compiled program. A program that does not compile has errors
// and no result. It runs under one schedule; a caller that wants more runs the program from Prepare.
func Interpret(path string, jobs int, stepLimit int64) (result *interp.Result, errs []*diag.CompileError) {
	program, errs := Prepare(path, jobs)
	if len(errs) > 0 {
		return nil, errs
	}
	return interp.Run(program, stepLimit), nil
}

// compileProject discovers the project and runs the modules phase. With a non-empty output it generates an executable; with capture it lowers the program
// and hands the IR to the interpreter instead.
//
// An error raised at a node is located (given its file, line and column) here, the first time anything needs it, and an
// error that stops the compile outside a stage's own guard is the compile's only error.
func compileProject(path, output string, o Options, capture **interp.Program) (errs []*diag.CompileError) {
	jobs, timings := o.Jobs, o.Timings
	var errors []*diag.CompileError
	timer := newPhaseTimer(timings)
	defer timer.total()
	mark := timer.mark

	var proj *ast.Project
	defer func() {
		if r := recover(); r != nil {
			ce, ok := r.(*diag.CompileError)
			if !ok {
				panic(r)
			}
			errs = []*diag.CompileError{ce}
		}
		if proj != nil {
			proj.Locate(errs)
		}
	}()
	proj = project.Discover(path, &errors)
	budget, restore := suspendCollection(proj, jobs)
	defer restore()
	if timings {
		fmt.Fprintf(os.Stderr, "no-GC budget %d MB\n", budget>>20)
	}
	mark("discover")
	if len(errors) > 0 {
		return sortedErrors(proj, errors)
	}
	results := make([]ast.ModuleResult, len(proj.Modules))
	phase := newModulePhase(proj, results, output, o, capture)
	phase.run()
	phase.reportTimings()
	mark("modules")
	if o.CacheStats != nil && phase.keys != nil && phase.frontSucceeded() {
		*o.CacheStats = phase.cacheStats()
	}
	if phaseErrors := phase.errors(); phaseErrors != nil {
		return phaseErrors
	}
	return phase.conclude(output, mark)
}

// firstFailure is the failure of the first module that has one, or nil. Every module that reaches a generic that grows
// its type argument without end finds the same runaway, so the program gets one error, from a module that is the same
// for any number of jobs, not one for each module.
func firstFailure(failures [][]*diag.CompileError) []*diag.CompileError {
	for _, failure := range failures {
		if len(failure) > 0 {
			return failure
		}
	}
	return nil
}

// checkBodies types the module's type declarations and functions, and records what verify and the instances need.
func checkBodies(proj *ast.Project, m *ast.Module, res *ast.ModuleResult) {
	checker := check.NewChecker(proj, m, &res.Resolution, &res.Typing, &res.Errors)
	for _, d := range m.Decls {
		switch d.(type) {
		case *ast.TypeDecl, *ast.UnionDecl, *ast.EnumDecl:
			checker.CheckBody(d)
		}
	}
	for _, f := range res.Functions {
		checker.CheckBody(f)
	}
	res.Pending = append(res.Pending, checker.Pending...)
	res.Bounds = append(res.Bounds, checker.Bounds...)
	res.Callers = append(res.Callers, checker.Callers...)
}

// verifyModule checks the calls and type arguments that checkBodies recorded, then follows the values of the module
// (check.VerifyFlow).
func verifyModule(proj *ast.Project, m *ast.Module, res *ast.ModuleResult, flow *check.FlowScratch) {
	verifier := check.NewChecker(proj, m, &res.Resolution, &res.Typing, &res.Errors)
	verifier.Verify(res.Pending)
	verifier.VerifyBounds(res.Bounds)
	verifier.VerifyFlow(m.Decls, res.Functions, flow)
}

// functionsOnce is the IR of every function the interpreter runs. An instance that several modules lowered is there once,
// as lowered by the first of them in module order.
func functionsOnce(outputs []moduleOutput) []*ir.IrFunction {
	var all []*ir.IrFunction
	seen := map[*ast.FuncDecl]struct{}{}
	for i := range outputs {
		for _, fn := range outputs[i].functions {
			if _, again := seen[fn.Decl]; !again {
				seen[fn.Decl] = struct{}{}
				all = append(all, fn)
			}
		}
	}
	return all
}

// withoutRepeats drops an error that an earlier one repeats exactly, which is what a refusal inside an instance becomes
// when every module that needs the instance lowers it.
func withoutRepeats(errs []*diag.CompileError) []*diag.CompileError {
	seen := map[diag.CompileError]struct{}{}
	var out []*diag.CompileError
	for _, e := range errs {
		if _, again := seen[*e]; !again {
			seen[*e] = struct{}{}
			out = append(out, e)
		}
	}
	return out
}

func writeIr(path string, outputs []moduleOutput) {
	var text strings.Builder
	for i := range outputs {
		text.WriteString(outputs[i].irText)
	}
	if err := os.WriteFile(path, []byte(text.String()), 0o644); err != nil {
		panic(diag.Internal("cannot write %s: %v", path, err))
	}
}

// refusals are the places in a module's functions where a program needs something the backend cannot run yet, such as a
// map, a list, a pool, a frozen value, a cown or a behaviour, which only the interpreter runs. One is reported for each
// function, at its first that matters, so the errors are a property of the program and not of how many modules compile
// at once.
func refusals(fns []*ir.IrFunction, matters func(code string) bool) []*diag.CompileError {
	var errs []*diag.CompileError
	for _, fn := range fns {
		for _, r := range fn.Refusals {
			if matters(r.Code) {
				errs = append(errs, r.Site.Error(r.Code, r.Message))
				break
			}
		}
	}
	return errs
}

func usesArrays(fns []*ir.IrFunction) bool {
	return slices.ContainsFunc(fns, func(fn *ir.IrFunction) bool { return fn.UsesArrays })
}

func usesBytes(fns []*ir.IrFunction) bool {
	return slices.ContainsFunc(fns, func(fn *ir.IrFunction) bool { return fn.UsesBytes })
}

func usesOs(fns []*ir.IrFunction) bool {
	return slices.ContainsFunc(fns, func(fn *ir.IrFunction) bool { return fn.UsesOs })
}

func suspendCollection(proj *ast.Project, jobs int) (int64, func()) {
	var source atomic.Int64
	par.ForEach(proj.Modules, jobs, func(m *ast.Module) {
		for _, f := range m.Files {
			if info, err := os.Stat(f); err == nil {
				source.Add(info.Size())
			}
		}
	})
	budget := max(source.Load()*bytesAllocatedPerSourceByte, minNoGcBudget)
	oldPercent := debug.SetGCPercent(-1)
	oldLimit := debug.SetMemoryLimit(budget)
	return budget, func() {
		debug.SetGCPercent(oldPercent)
		debug.SetMemoryLimit(oldLimit)
	}
}

func sortedErrors(proj *ast.Project, errs []*diag.CompileError) []*diag.CompileError {
	proj.Locate(errs)
	out := append([]*diag.CompileError{}, errs...)
	sort.SliceStable(out, func(a, b int) bool {
		x, y := out[a], out[b]
		if x.File != y.File {
			return x.File < y.File
		}
		if x.Line != y.Line {
			return x.Line < y.Line
		}
		if x.Col != y.Col {
			return x.Col < y.Col
		}
		return x.Code < y.Code
	})
	return out
}
