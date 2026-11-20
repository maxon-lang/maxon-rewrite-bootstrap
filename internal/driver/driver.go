package driver

import (
	"fmt"
	"minimaxon/internal/ast"
	"minimaxon/internal/backend"
	"minimaxon/internal/check"
	"minimaxon/internal/diag"
	"minimaxon/internal/interp"
	"minimaxon/internal/ir"

	"minimaxon/internal/par"
	"minimaxon/internal/project"
	"minimaxon/internal/resolve"
	"minimaxon/internal/syntax"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"runtime/trace"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	bytesAllocatedPerSourceByte = 100
	minNoGcBudget               = int64(256) << 20
	bytesPerMegabyte            = 1048576.0
	heapAllocsMetric            = "/gc/heap/allocs:bytes"
)

func DefaultJobs() int { return max(1, runtime.NumCPU()-1) }

func heapAllocated() uint64 {
	sample := []metrics.Sample{{Name: heapAllocsMetric}}
	metrics.Read(sample)
	return sample[0].Value.Uint64()
}

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

// compileProject runs the phases. With a non-empty output it generates an executable; with capture it lowers the program
// and hands the IR to the interpreter instead.
//
// An error raised at a node is located (given its file, line and column) here, the first time anything needs it, and an
// error that stops the compile outside a pass's own guard is the compile's only error.
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
	modules := proj.Modules
	results := make([]ast.ModuleResult, len(modules))

	phase := func(work func(*ast.Module, *ast.ModuleResult)) {
		par.ForEachIndex(len(modules), jobs, func(i int) { work(modules[i], &results[i]) })
	}

	failed := func() []*diag.CompileError { return collectErrors(proj, errors, results, nil) }

	// The parse pass builds each module's own declaration table straight after parsing it, while its AST is in cache. A
	// module's table is built only when it parsed cleanly, and the table's errors count only when every module did, so
	// the errors a program gets are the same as if the table had a pass of its own after the parse.
	var readBytes, lexBytes, parseBytes, tableBytes atomic.Uint64
	parseStart := heapAllocated()
	parsed := make([]int, len(modules))
	declared := make([][]ast.Decl, len(modules))
	par.ForEachIndexWith(len(modules), jobs, syntax.NewScratch, func(scratch *syntax.Scratch, i int) {
		m, res := modules[i], &results[i]
		diag.Guard(&res.Errors, func() {
			a0 := stepAllocated(timings)
			src := scratch.LoadModuleSource(m.Files)
			a1 := stepAllocated(timings)
			tokens := scratch.Lex(src)
			a2 := stepAllocated(timings)
			program, syntaxErrors := scratch.Parse(src, tokens, m.ID)
			m.Adopt(program)
			res.Errors = append(res.Errors, syntaxErrors...)
			a3 := stepAllocated(timings)
			readBytes.Add(a1 - a0)
			lexBytes.Add(a2 - a1)
			parseBytes.Add(a3 - a2)
		})
		parsed[m.ID] = len(res.Errors)
		if parsed[m.ID] == 0 {
			a0 := stepAllocated(timings)
			declared[m.ID] = project.BuildModuleTable(m, &res.Errors)
			tableBytes.Add(stepAllocated(timings) - a0)
		}
	})
	if timings {
		reportParseAllocation(heapAllocated()-parseStart, readBytes.Load(), lexBytes.Load(), parseBytes.Load(), tableBytes.Load())
	}
	mark("parse")
	if parseErrors := collectErrors(proj, errors, results, parsed); parseErrors != nil {
		return parseErrors
	}

	project.IndexNames(proj, declared)
	mark("names")

	phase(func(m *ast.Module, res *ast.ModuleResult) { resolve.NewResolver(proj, m, res).Run() })
	mark("resolve")
	if resolveErrors := failed(); resolveErrors != nil {
		return resolveErrors
	}
	if cycle := project.NewGraph(results).CheckAcyclic(proj); cycle != nil {
		return sortedErrors(proj, []*diag.CompileError{cycle})
	}
	mark("cycles")

	check.DeclareAliases(proj, results)
	mark("aliases")
	if aliasErrors := failed(); aliasErrors != nil {
		return aliasErrors
	}

	var main *ast.FuncDecl
	phase(func(m *ast.Module, res *ast.ModuleResult) {
		checker := check.NewChecker(proj, m, &res.Resolution, &res.Errors)
		for _, d := range m.Decls {
			res.Functions = append(res.Functions, checker.Declare(d)...)
		}
		res.Bounds = append(res.Bounds, checker.Bounds...)
		if m == proj.Root {
			main = checker.CheckMain()
		}
	})
	mark("declare")
	if declareErrors := failed(); declareErrors != nil {
		return declareErrors
	}

	checkConstants(proj, results, jobs, &errors)
	mark("constants")
	if constErrors := failed(); constErrors != nil {
		return constErrors
	}

	// The check pass types a module's bodies and then verifies it, while its AST and types are in cache. Verify does not
	// run on a module that has body errors, and when any module has body errors only those are reported: a verify that
	// another module's worker ran meanwhile is dropped, so the errors depend neither on --jobs nor on which worker got
	// there first.
	bodied := make([]int, len(modules))
	var bodiesFailed atomic.Bool
	phase(func(m *ast.Module, res *ast.ModuleResult) {
		checkBodies(proj, m, res)
		bodied[m.ID] = len(res.Errors)
		if bodied[m.ID] > 0 {
			bodiesFailed.Store(true)
		} else if !bodiesFailed.Load() {
			verifyModule(proj, m, res)
		}
	})
	mark("check")
	if checkErrors := collectErrors(proj, errors, results, bodied); checkErrors != nil {
		return checkErrors
	}

	if output != "" || capture != nil {
		return generate(proj, results, main, output, o, capture, mark)
	}
	return checkInstances(results, jobs, mark)
}

// checkInstances is what a compile that makes nothing runs in place of generate: each module finds its instances, which
// is where a generic function that grows its own type argument without end is refused (E3122).
func checkInstances(results []ast.ModuleResult, jobs int, mark func(string)) []*diag.CompileError {
	failures := make([][]*diag.CompileError, len(results))
	par.ForEachIndex(len(results), jobs, func(i int) {
		diag.Guard(&failures[i], func() { ir.Instances(results[i].Callers) })
	})
	mark("instances")
	return firstFailure(failures)
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
	checker := check.NewChecker(proj, m, &res.Resolution, &res.Errors)
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

// verifyModule checks the calls and type arguments that checkBodies recorded, then ownership, which only runs on a
// module that has no error yet.
func verifyModule(proj *ast.Project, m *ast.Module, res *ast.ModuleResult) {
	verifier := check.NewChecker(proj, m, &res.Resolution, &res.Errors)
	verifier.Verify(res.Pending)
	verifier.VerifyBounds(res.Bounds)
	if len(res.Errors) == 0 {
		ownership := check.NewOwnership(&res.Errors, verifier)
		for _, d := range m.Decls {
			ownership.CheckValues(d)
		}
		for _, f := range res.Functions {
			ownership.Check(f)
		}
	}
}

// collectErrors gathers the errors of the compile so far, sorted, or nil if there are none. A pass that fuses two
// stages gives stageEnds, how many errors each module had when its first stage ended: if any module had errors then,
// only those count, and what the second stage found is dropped.
func collectErrors(proj *ast.Project, errors []*diag.CompileError, results []ast.ModuleResult, stageEnds []int) []*diag.CompileError {
	firstStageFailed := slices.ContainsFunc(stageEnds, func(n int) bool { return n > 0 })
	all := append([]*diag.CompileError{}, errors...)
	for i := range results {
		if firstStageFailed {
			all = append(all, results[i].Errors[:stageEnds[i]]...)
		} else {
			all = append(all, results[i].Errors...)
		}
	}
	if len(all) == 0 {
		return nil
	}
	return sortedErrors(proj, all)
}

// moduleOutput is what the generate pass keeps of a module once its functions are lowered and its code is emitted.
type moduleOutput struct {
	// functions are kept only for the interpreter, which runs the IR instead of emitting code.
	functions  []*ir.IrFunction
	irText     string
	refusals   []*diag.CompileError
	usesArrays bool
}

// generate finds the instances a module needs, lowers its functions and those instances, and emits its code unit
// straight away, so the IR is consumed while it is in cache. A module that needs an instance that another module
// needs too lowers and emits it as well, and the link keeps one copy. With capture it hands the IR to the interpreter
// instead of emitting code. A program that has refusals gets no executable and the code emitted for it is dropped; a
// module with a refusal is not emitted at all, because its IR holds what codegen cannot handle.
func generate(proj *ast.Project, results []ast.ModuleResult, main *ast.FuncDecl, output string, o Options, capture **interp.Program, mark func(string)) []*diag.CompileError {
	modules := proj.Modules
	matters := func(string) bool { return true }
	if capture != nil {
		matters = func(code string) bool { return code != diag.ErrInterpreterOnly }
	}
	outputs := make([]moduleOutput, len(modules))
	unfinite := make([][]*diag.CompileError, len(modules))
	units := make([]*backend.CodeUnit, len(modules)+1)
	var refused atomic.Bool
	generateModules := func() {
		par.ForEachIndexWith(len(modules), o.Jobs, newGenerateScratch, func(w *generateScratch, i int) {
			out := &outputs[i]
			var instances *ir.InstanceSet
			diag.Guard(&unfinite[i], func() { instances = ir.Instances(results[i].Callers) })
			if len(unfinite[i]) > 0 {
				refused.Store(true)
				return
			}
			// What the interpreter runs is kept, so its IR stays where it is; the IR that is only emitted is dead once its
			// module is, and the next module lowers over it.
			if capture == nil {
				w.lowering.Reset()
			}
			fns := w.fns[:0]
			for _, f := range results[i].Functions {
				if len(f.Generics) == 0 {
					fns = append(fns, w.lowering.Lower(f, instances))
				}
			}
			for _, inst := range instances.List {
				fns = append(fns, w.lowering.Lower(inst, instances))
			}
			w.fns = fns
			out.refusals = refusals(fns, matters)
			out.usesArrays = usesArrays(fns)
			if o.EmitIr {
				var text strings.Builder
				for _, fn := range fns {
					ir.PrintIr(fn, &text)
				}
				out.irText = text.String()
			}
			switch {
			case capture != nil:
				out.functions = slices.Clone(fns)
			case len(out.refusals) > 0:
				refused.Store(true)
			case !refused.Load():
				units[i+1] = backend.EmitModule(fns)
			}
		})
	}
	emitRuntime := func() {
		units[0] = backend.EmitRuntime(main, backend.RuntimeOptions{CountAllocations: o.CountAllocations})
	}
	switch {
	case capture != nil:
		generateModules()
	case o.Jobs <= 1:
		emitRuntime()
		generateModules()
	default:
		var failure any
		var done sync.WaitGroup
		done.Go(func() {
			defer func() { failure = recover() }()
			emitRuntime()
		})
		generateModules()
		done.Wait()
		if failure != nil {
			panic(failure)
		}
	}
	mark("generate")
	if failure := firstFailure(unfinite); failure != nil {
		return failure
	}
	if o.EmitIr {
		writeIr(output+".ir", outputs)
	}
	var blocked []*diag.CompileError
	for i := range outputs {
		blocked = append(blocked, outputs[i].refusals...)
	}
	if len(blocked) > 0 {
		return withoutRepeats(sortedErrors(proj, blocked))
	}
	if capture != nil {
		*capture = interp.NewProgram(functionsOnce(outputs), main)
		return nil
	}
	units = append(units, backend.EmitTypeRoutines(units, o.Jobs)...)
	if slices.ContainsFunc(outputs, func(out moduleOutput) bool { return out.usesArrays }) {
		units = append(units, backend.EmitArrayRuntime())
	}
	mark("routines")
	backend.Link(output, units, ir.RtEntry, o.Jobs)
	mark("link")
	return nil
}

// generateScratch is what a worker of the generate pass reuses from one module to the next, so lowering a module
// allocates nothing once the buffers have grown to the largest one.
type generateScratch struct {
	lowering *ir.Lowering
	fns      []*ir.IrFunction
}

func newGenerateScratch() *generateScratch { return &generateScratch{lowering: ir.NewLowering()} }

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
				errs = append(errs, r.Site.P().Error(r.Code, r.Message))
				break
			}
		}
	}
	return errs
}

func usesArrays(fns []*ir.IrFunction) bool {
	return slices.ContainsFunc(fns, func(fn *ir.IrFunction) bool { return fn.UsesArrays })
}

func stepAllocated(timings bool) uint64 {
	if !timings {
		return 0
	}
	return heapAllocated()
}

func reportParseAllocation(phaseTotal, read, lex, parse, table uint64) {
	sum := read + lex + parse + table
	scale := 1.0
	if sum > 0 {
		scale = float64(phaseTotal) / float64(sum)
	}
	mb := func(n uint64) float64 { return float64(n) * scale / bytesPerMegabyte }
	fmt.Fprintf(os.Stderr, "  read %.1f MB, lex %.1f MB, parse %.1f MB, table %.1f MB\n", mb(read), mb(lex), mb(parse), mb(table))
}

type constEntry struct {
	module *ast.Module
	decl   *ast.ConstDecl
}

func checkConstants(proj *ast.Project, results []ast.ModuleResult, jobs int, errs *[]*diag.CompileError) {
	var consts []constEntry
	for _, m := range proj.Modules {
		for _, d := range m.Decls {
			if c, ok := d.(*ast.ConstDecl); ok {
				consts = append(consts, constEntry{m, c})
			}
		}
	}
	deps := make([][]*ast.ConstDecl, len(consts))
	par.ForEachIndex(len(consts), jobs, func(i int) {
		seen := map[*ast.ConstDecl]bool{}
		refs := constRefs{proj, &results[consts[i].module.ID].Resolution}
		for _, c := range refs.of(consts[i].decl.Value, nil) {
			if !seen[c] {
				seen[c] = true
				deps[i] = append(deps[i], c)
			}
		}
	})

	index := make(map[*ast.ConstDecl]int, len(consts))
	for i, c := range consts {
		index[c.decl] = i
	}
	depth := make([]int, len(consts))
	state := make([]byte, len(consts))
	const visiting, done = 1, 2

	var depthOf func(i int) int
	depthOf = func(i int) int {
		if state[i] == done {
			return depth[i]
		}
		if state[i] == visiting {
			panic(consts[i].decl.Error(diag.ErrDependsOnItself, "constant '"+consts[i].decl.Name+"' depends on itself"))
		}
		state[i] = visiting
		depth[i] = 0
		for _, d := range deps[i] {
			depth[i] = max(depth[i], depthOf(index[d])+1)
		}
		state[i] = done
		return depth[i]
	}

	for i := range consts {
		before := len(*errs)
		diag.Guard(errs, func() { depthOf(i) })
		if len(*errs) > before {
			return
		}
	}

	var waves [][]int
	for i := range consts {
		for len(waves) <= depth[i] {
			waves = append(waves, nil)
		}
		waves[depth[i]] = append(waves[depth[i]], i)
	}
	for _, items := range waves {
		waveErrors := make([][]*diag.CompileError, len(items))
		par.ForEachIndex(len(items), jobs, func(k int) {
			entry := consts[items[k]]
			check.NewChecker(proj, entry.module, &results[entry.module.ID].Resolution, &waveErrors[k]).CheckConst(entry.decl)
		})
		for _, e := range waveErrors {
			*errs = append(*errs, e...)
		}
	}
}

type constList = []*ast.ConstDecl

// referencedConsts appends the constants that e names, so the constants phase types them first. It walks every
// expression a constant could hold, including ones the checker then refuses, because a missed reference would
// check a constant in the same wave as one it reads.
func (r constRefs) of(e ast.Expr, into constList) constList {
	return ast.VisitExprState[constList, constList](r, e, into)
}

// constRefs reads what the module that holds the expression resolved its names to.
type constRefs struct {
	proj       *ast.Project
	resolution *ast.Resolution
}

func (r constRefs) VisitIntLit(_ *ast.IntLit, into constList) constList { return into }

func (r constRefs) VisitBoolLit(_ *ast.BoolLit, into constList) constList { return into }

func (r constRefs) VisitSelfExpr(_ *ast.SelfExpr, into constList) constList { return into }

func (r constRefs) VisitStrLit(e *ast.StrLit, into constList) constList {
	for _, p := range e.Parts {
		if part, ok := p.(ast.Expr); ok {
			into = r.of(part, into)
		}
	}
	return into
}

func (r constRefs) VisitNameExpr(e *ast.NameExpr, into constList) constList {
	if c, ok := r.resolution.Decl(r.proj, e.ID).(*ast.ConstDecl); ok {
		return append(into, c)
	}
	return into
}

func (r constRefs) VisitBinaryExpr(e *ast.BinaryExpr, into constList) constList {
	return r.of(e.R, r.of(e.L, into))
}

func (r constRefs) VisitUnaryExpr(e *ast.UnaryExpr, into constList) constList {
	return r.of(e.E, into)
}

func (r constRefs) VisitCondExpr(e *ast.CondExpr, into constList) constList {
	return r.of(e.Else, r.of(e.Then, r.of(e.Cond, into)))
}

func (r constRefs) VisitCastExpr(e *ast.CastExpr, into constList) constList {
	return r.of(e.E, into)
}

func (r constRefs) VisitIndexExpr(e *ast.IndexExpr, into constList) constList {
	into = r.of(e.Obj, into)
	for _, a := range e.Args {
		into = r.of(a, into)
	}
	return into
}

func (r constRefs) VisitMemberExpr(e *ast.MemberExpr, into constList) constList {
	return r.of(e.Obj, into)
}

func (r constRefs) VisitCallExpr(e *ast.CallExpr, into constList) constList {
	into = r.of(e.Callee, into)
	for _, a := range e.Args {
		into = r.of(a.Value, into)
	}
	return into
}

func (r constRefs) VisitStructLitExpr(e *ast.StructLitExpr, into constList) constList {
	for _, f := range e.Fields {
		into = r.of(f.Value, into)
	}
	return into
}

// A match expression's arms all give a value; a 'then' arm, the only one holding a statement, is refused in one.
func (r constRefs) VisitMatchExpr(e *ast.MatchExpr, into constList) constList {
	into = r.of(e.Match.Subject, into)
	for _, arm := range e.Match.Arms {
		for _, p := range arm.Patterns {
			if p.Value != nil {
				into = r.of(p.Value, into)
			}
			if p.High != nil {
				into = r.of(p.High, into)
			}
		}
		if arm.Value != nil {
			into = r.of(arm.Value, into)
		}
	}
	return into
}

func (r constRefs) VisitPanicExpr(e *ast.PanicExpr, into constList) constList {
	return r.of(e.Message, into)
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
