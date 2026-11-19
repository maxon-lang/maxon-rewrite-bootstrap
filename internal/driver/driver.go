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
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	bytesAllocatedPerSourceByte = 100
	minNoGcBudget               = int64(256) << 20
	bytesPerMegabyte            = 1048576.0
	initialTokenCapacity        = 1 << 16
	heapAllocsMetric            = "/gc/heap/allocs:bytes"
)

var tokenBuffers = sync.Pool{New: func() any {
	buffer := make([]syntax.Token, 0, initialTokenCapacity)
	return &buffer
}}

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
	// Timings prints each phase's wall time and allocation to standard error.
	Timings bool
	// EmitIr writes the IR of every function beside the executable, as <output>.ir.
	EmitIr bool
	// CountAllocations makes the executable count its live heap boxes and fail at exit if any is left (README, "Counting
	// allocations"): a development flag, so a normal executable pays nothing for it.
	CountAllocations bool
}

func Compile(path, output string, o Options) (errs []*diag.CompileError) {
	defer recoverCompileError(&errs)
	return compileProject(path, output, o, nil)
}

func recoverCompileError(errs *[]*diag.CompileError) {
	if r := recover(); r != nil {
		ce, ok := r.(*diag.CompileError)
		if !ok {
			panic(r)
		}
		*errs = []*diag.CompileError{ce}
	}
}

// Prepare compiles a program as far as IR, which the tracking interpreter (internal/interp) runs. The program can be run
// more than once, under different schedules of its behaviours.
func Prepare(path string, jobs int) (program *interp.Program, errs []*diag.CompileError) {
	defer recoverCompileError(&errs)
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
func compileProject(path, output string, o Options, capture **interp.Program) []*diag.CompileError {
	jobs, timings, emitIr := o.Jobs, o.Timings, o.EmitIr
	var errors []*diag.CompileError
	clock := time.Now()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	allocated, collections, paused := stats.TotalAlloc, stats.NumGC, stats.PauseTotalNs
	mark := func(phase string) {
		if timings {
			runtime.ReadMemStats(&stats)
			fmt.Fprintf(os.Stderr, "%-10s %8.1f ms %8.1f MB %4d GCs %7.1f ms paused\n", phase,
				float64(time.Since(clock).Nanoseconds())/1e6,
				float64(stats.TotalAlloc-allocated)/bytesPerMegabyte,
				stats.NumGC-collections,
				float64(stats.PauseTotalNs-paused)/1e6)
			allocated, collections, paused = stats.TotalAlloc, stats.NumGC, stats.PauseTotalNs
		}
		clock = time.Now()
	}

	proj := project.Discover(path, &errors)
	budget, restore := suspendCollection(proj, jobs)
	defer restore()
	if timings {
		fmt.Fprintf(os.Stderr, "no-GC budget %d MB\n", budget>>20)
	}
	mark("discover")
	if len(errors) > 0 {
		return sortedErrors(errors)
	}
	modules := proj.Modules

	phase := func(work func(*ast.Module)) { par.ForEach(modules, jobs, work) }

	failed := func() []*diag.CompileError {
		all := append([]*diag.CompileError{}, errors...)
		for _, m := range modules {
			all = append(all, m.Errors...)
		}
		if len(all) == 0 {
			return nil
		}
		return sortedErrors(all)
	}

	var readBytes, lexBytes, parseBytes atomic.Uint64
	parseStart := heapAllocated()
	phase(func(m *ast.Module) {
		diag.Guard(&m.Errors, func() {
			a0 := stepAllocated(timings)
			src := syntax.LoadModuleSource(m.Files)
			a1 := stepAllocated(timings)
			scratch := tokenBuffers.Get().(*[]syntax.Token)
			tokens := syntax.Lex(src, *scratch)
			a2 := stepAllocated(timings)
			parser := syntax.NewParser(src, tokens)
			m.Decls = append(m.Decls, parser.ParseProgram().Decls...)
			m.Errors = append(m.Errors, parser.Errors...)
			a3 := stepAllocated(timings)
			*scratch = tokens[:0]
			tokenBuffers.Put(scratch)
			readBytes.Add(a1 - a0)
			lexBytes.Add(a2 - a1)
			parseBytes.Add(a3 - a2)
		})
	})
	if timings {
		reportParseAllocation(heapAllocated()-parseStart, readBytes.Load(), lexBytes.Load(), parseBytes.Load())
	}
	mark("parse")
	if parseErrors := failed(); parseErrors != nil {
		return parseErrors
	}

	project.BuildTables(proj, jobs)
	mark("tables")
	if tableErrors := failed(); tableErrors != nil {
		return tableErrors
	}

	phase(func(m *ast.Module) { resolve.NewResolver(proj, m).Run() })
	mark("resolve")
	if resolveErrors := failed(); resolveErrors != nil {
		return resolveErrors
	}
	if !project.DependencyOrder(proj, &errors) {
		return sortedErrors(errors)
	}
	mark("cycles")

	check.DeclareAliases(proj)
	mark("aliases")
	if aliasErrors := failed(); aliasErrors != nil {
		return aliasErrors
	}

	var main *ast.FuncDecl
	phase(func(m *ast.Module) {
		checker := check.NewChecker(proj, m, &m.Errors)
		for _, d := range m.Decls {
			m.Functions = append(m.Functions, checker.Declare(d)...)
		}
		m.Bounds = append(m.Bounds, checker.Bounds...)
		if m == proj.Root {
			main = checker.CheckMain()
		}
	})
	mark("declare")
	if declareErrors := failed(); declareErrors != nil {
		return declareErrors
	}

	checkConstants(proj, jobs, &errors)
	mark("constants")
	if constErrors := failed(); constErrors != nil {
		return constErrors
	}

	phase(func(m *ast.Module) {
		checker := check.NewChecker(proj, m, &m.Errors)
		for _, d := range m.Decls {
			switch d.(type) {
			case *ast.TypeDecl, *ast.UnionDecl, *ast.EnumDecl:
				checker.CheckBody(d)
			}
		}
		for _, f := range m.Functions {
			checker.CheckBody(f)
		}
		m.Pending = append(m.Pending, checker.Pending...)
		m.Bounds = append(m.Bounds, checker.Bounds...)
		m.Callers = append(m.Callers, checker.Callers...)
	})
	mark("bodies")
	if bodyErrors := failed(); bodyErrors != nil {
		return bodyErrors
	}

	phase(func(m *ast.Module) {
		verifier := check.NewChecker(proj, m, &m.Errors)
		verifier.Verify(m.Pending)
		verifier.VerifyBounds(m.Bounds)
		if len(m.Errors) == 0 {
			ownership := check.NewOwnership(&m.Errors, verifier)
			for _, d := range m.Decls {
				ownership.CheckValues(d)
			}
			for _, f := range m.Functions {
				ownership.Check(f)
			}
		}
	})
	mark("verify")
	if verifyErrors := failed(); verifyErrors != nil {
		return verifyErrors
	}

	var instances [][]*ast.FuncDecl
	diag.Guard(&errors, func() { instances = ir.Instances(modules) })
	mark("instances")
	if len(errors) > 0 {
		return sortedErrors(errors)
	}

	if output != "" || capture != nil {
		irs := make([][]*ir.IrFunction, len(modules))
		par.ForEachIndex(len(modules), jobs, func(i int) {
			fns := make([]*ir.IrFunction, 0, len(modules[i].Functions)+len(instances[i]))
			for _, f := range modules[i].Functions {
				if len(f.Generics) == 0 {
					fns = append(fns, ir.Lower(f))
				}
			}
			for _, inst := range instances[i] {
				fns = append(fns, ir.Lower(inst))
			}
			irs[i] = fns
		})
		mark("lower")
		if emitIr {
			var text strings.Builder
			for _, fns := range irs {
				for _, fn := range fns {
					ir.PrintIr(fn, &text)
				}
			}
			if err := os.WriteFile(output+".ir", []byte(text.String()), 0o644); err != nil {
				panic(diag.Internal("cannot write %s.ir: %v", output, err))
			}
		}
		if capture != nil {
			if blocked := refusals(irs, func(code string) bool { return code != diag.ErrInterpreterOnly }); len(blocked) > 0 {
				return sortedErrors(blocked)
			}
			var all []*ir.IrFunction
			for _, fns := range irs {
				all = append(all, fns...)
			}
			*capture = interp.NewProgram(all, main)
			return nil
		}
		if blocked := refusals(irs, func(string) bool { return true }); len(blocked) > 0 {
			return sortedErrors(blocked)
		}
		units := make([]*backend.CodeUnit, len(modules)+1)
		emitModules := func() { par.ForEachIndex(len(modules), jobs, func(i int) { units[i+1] = backend.EmitModule(irs[i]) }) }
		if jobs <= 1 {
			units[0] = backend.EmitRuntime(main, backend.RuntimeOptions{CountAllocations: o.CountAllocations})
			emitModules()
		} else {
			var failure any
			var done sync.WaitGroup
			done.Go(func() {
				defer func() { failure = recover() }()
				units[0] = backend.EmitRuntime(main, backend.RuntimeOptions{CountAllocations: o.CountAllocations})
			})
			emitModules()
			done.Wait()
			if failure != nil {
				panic(failure)
			}
		}
		mark("codegen")
		units = append(units, backend.EmitTypeRoutines(units, jobs)...)
		if usesArrays(irs) {
			units = append(units, backend.EmitArrayRuntime())
		}
		mark("routines")
		backend.Link(output, units, ir.RtEntry, jobs)
		mark("link")
	}
	return nil
}

// refusals are the places where a program needs something the backend cannot run yet, such as a map, a list, a pool, a
// frozen value, a cown or a behaviour, which only the interpreter runs. One is reported for each function, at its first that matters, so the errors are a
// property of the program and not of how many modules compile at once.
func refusals(irs [][]*ir.IrFunction, matters func(code string) bool) []*diag.CompileError {
	var errs []*diag.CompileError
	for _, fns := range irs {
		for _, fn := range fns {
			for _, r := range fn.Refusals {
				if matters(r.Code) {
					errs = append(errs, r.Site.P().Error(r.Code, r.Message))
					break
				}
			}
		}
	}
	return errs
}

func usesArrays(irs [][]*ir.IrFunction) bool {
	for _, fns := range irs {
		for _, fn := range fns {
			if fn.UsesArrays {
				return true
			}
		}
	}
	return false
}

func stepAllocated(timings bool) uint64 {
	if !timings {
		return 0
	}
	return heapAllocated()
}

func reportParseAllocation(phaseTotal, read, lex, parse uint64) {
	sum := read + lex + parse
	scale := 1.0
	if sum > 0 {
		scale = float64(phaseTotal) / float64(sum)
	}
	fmt.Fprintf(os.Stderr, "  read %.1f MB, lex %.1f MB, parse %.1f MB\n",
		float64(read)*scale/bytesPerMegabyte, float64(lex)*scale/bytesPerMegabyte, float64(parse)*scale/bytesPerMegabyte)
}

type constEntry struct {
	module *ast.Module
	decl   *ast.ConstDecl
}

func checkConstants(proj *ast.Project, jobs int, errs *[]*diag.CompileError) {
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
		for _, c := range referencedConsts(consts[i].decl.Value, nil) {
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
			check.NewChecker(proj, entry.module, &waveErrors[k]).CheckConst(entry.decl)
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
func referencedConsts(e ast.Expr, into constList) constList {
	return ast.VisitExprState[constList, constList](constRefs{}, e, into)
}

type constRefs struct{}

func (constRefs) VisitIntLit(_ *ast.IntLit, into constList) constList { return into }

func (constRefs) VisitBoolLit(_ *ast.BoolLit, into constList) constList { return into }

func (constRefs) VisitSelfExpr(_ *ast.SelfExpr, into constList) constList { return into }

func (constRefs) VisitStrLit(e *ast.StrLit, into constList) constList {
	for _, p := range e.Parts {
		if part, ok := p.(ast.Expr); ok {
			into = referencedConsts(part, into)
		}
	}
	return into
}

func (constRefs) VisitNameExpr(e *ast.NameExpr, into constList) constList {
	if c, ok := e.Target.(*ast.ConstDecl); ok {
		return append(into, c)
	}
	return into
}

func (constRefs) VisitBinaryExpr(e *ast.BinaryExpr, into constList) constList {
	return referencedConsts(e.R, referencedConsts(e.L, into))
}

func (constRefs) VisitUnaryExpr(e *ast.UnaryExpr, into constList) constList {
	return referencedConsts(e.E, into)
}

func (constRefs) VisitCondExpr(e *ast.CondExpr, into constList) constList {
	return referencedConsts(e.Else, referencedConsts(e.Then, referencedConsts(e.Cond, into)))
}

func (constRefs) VisitCastExpr(e *ast.CastExpr, into constList) constList {
	return referencedConsts(e.E, into)
}

func (constRefs) VisitIndexExpr(e *ast.IndexExpr, into constList) constList {
	into = referencedConsts(e.Obj, into)
	for _, a := range e.Args {
		into = referencedConsts(a, into)
	}
	return into
}

func (constRefs) VisitMemberExpr(e *ast.MemberExpr, into constList) constList {
	return referencedConsts(e.Obj, into)
}

func (constRefs) VisitCallExpr(e *ast.CallExpr, into constList) constList {
	into = referencedConsts(e.Callee, into)
	for _, a := range e.Args {
		into = referencedConsts(a.Value, into)
	}
	return into
}

func (constRefs) VisitStructLitExpr(e *ast.StructLitExpr, into constList) constList {
	for _, f := range e.Fields {
		into = referencedConsts(f.Value, into)
	}
	return into
}

// A match expression's arms all give a value; a 'then' arm, the only one holding a statement, is refused in one.
func (constRefs) VisitMatchExpr(e *ast.MatchExpr, into constList) constList {
	into = referencedConsts(e.Match.Subject, into)
	for _, arm := range e.Match.Arms {
		for _, p := range arm.Patterns {
			if p.Value != nil {
				into = referencedConsts(p.Value, into)
			}
			if p.High != nil {
				into = referencedConsts(p.High, into)
			}
		}
		if arm.Value != nil {
			into = referencedConsts(arm.Value, into)
		}
	}
	return into
}

func (constRefs) VisitPanicExpr(e *ast.PanicExpr, into constList) constList {
	return referencedConsts(e.Message, into)
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

func sortedErrors(errs []*diag.CompileError) []*diag.CompileError {
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
