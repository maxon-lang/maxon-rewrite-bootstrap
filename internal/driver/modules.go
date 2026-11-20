package driver

import (
	"context"
	"fmt"
	"minimaxon/internal/ast"
	"minimaxon/internal/backend"
	"minimaxon/internal/cache"
	"minimaxon/internal/diag"
	"minimaxon/internal/interp"
	"minimaxon/internal/ir"
	"minimaxon/internal/par"
	"minimaxon/internal/project"
	"os"
	"runtime/trace"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

// taskKind is one of the three tasks a module has after the front. Check types its bodies and verifies it. Closure is
// the join of the module's check and the closures of the modules it depends on: it is where "everything this module
// reaches is checked" is known, without any module holding the set of the modules it reaches. Generate is what the
// module needs that for: it lowers the module's functions and its instances and emits its code, or, for a compile that
// makes nothing, only finds the instances.
type taskKind int

const (
	taskCheck taskKind = iota
	taskClosure
	taskGenerate
	tasksPerModule
)

// taskLayout numbers the tasks of the modules phase. A compile that makes an executable has one task before the modules'
// own, which emits the runtime unit; it needs nothing from the modules, and is first so a worker starts it at once.
type taskLayout struct {
	modules int
	runtime bool
}

func (l taskLayout) first() int {
	if l.runtime {
		return 1
	}
	return 0
}

func (l taskLayout) count() int { return l.first() + l.modules*int(tasksPerModule) }

func (l taskLayout) task(m ast.ModuleID, kind taskKind) int {
	return l.first() + int(m)*int(tasksPerModule) + int(kind)
}

// module is the module a task works on and what it does there. The runtime task is none of the modules.
func (l taskLayout) module(task int) (m ast.ModuleID, kind taskKind, ofModule bool) {
	if task < l.first() {
		return 0, 0, false
	}
	at := task - l.first()
	return ast.ModuleID(at / int(tasksPerModule)), taskKind(at % int(tasksPerModule)), true
}

// modulePlan is the order of the tasks. The check of a module needs nothing beyond the front, which every task runs
// after. The closure of a module needs the module's check and the closure of each module it depends on, so a closure is
// done exactly when the checks of every module the module reaches are, and the plan has an edge for each dependency, not
// for each pair of a module and a module it reaches. The generate of a module needs its closure: it reads the bodies and the
// check results of the generics it reaches in other modules, and the marks that verify leaves on their constants, but
// nothing of a module that depends on it, so the modules that depend on it can start before it does. A module is the
// group of its tasks, so a worker that has checked a module goes on to its closure and its generate while the module is in cache.
func modulePlan(graph *project.Graph, layout taskLayout) *par.Plan {
	plan := &par.Plan{Needs: make([][]int, layout.count()), Group: make([]int, layout.count())}
	edges := 0
	for _, deps := range graph.Deps {
		edges += len(deps)
	}
	backing := make([]int, 0, 2*layout.modules+edges)
	need := func(task int, needs ...int) {
		start := len(backing)
		backing = append(backing, needs...)
		plan.Needs[task] = backing[start:len(backing):len(backing)]
	}
	for i, deps := range graph.Deps {
		m := ast.ModuleID(i)
		for kind := range tasksPerModule {
			plan.Group[layout.task(m, kind)] = i
		}
		start := len(backing)
		backing = append(backing, layout.task(m, taskCheck))
		for _, d := range deps {
			backing = append(backing, layout.task(d.Module, taskClosure))
		}
		plan.Needs[layout.task(m, taskClosure)] = backing[start:len(backing):len(backing)]
		need(layout.task(m, taskGenerate), layout.task(m, taskClosure))
	}
	if layout.runtime {
		plan.Group[0] = layout.modules
	}
	return plan
}

// moduleOutput is what a module's generate keeps of a module once its functions are lowered and its code is emitted.
type moduleOutput struct {
	// functions are kept only for the interpreter, which runs the IR instead of emitting code.
	functions  []*ir.IrFunction
	irText     string
	refusals   []*diag.CompileError
	usesArrays bool
}

// modulePhase runs what is left of the compile once the front is done: the check and the generate of every module, each as
// soon as what it needs is done. Every task writes the slots of its own module, so the phase needs no locks beyond the
// scheduler's, and what follows reads the slots in module order, so nothing it reports depends on the schedule.
//
// A module is generated only if its own check and the check of every module it reaches found nothing, so a module that
// has an error is never lowered. When another module has errors the program gets those alone and the output is dropped, so
// a generate that started before the error was found does no harm; a body error, which hides verify errors, is reported
// as the front would, from the count of errors each module had when its bodies were done.
type modulePhase struct {
	proj    *ast.Project
	results []ast.ModuleResult
	typings ast.Typings
	graph   *project.Graph
	main    *ast.FuncDecl
	options Options
	layout  taskLayout
	// capture is where the IR goes for the interpreter, if it is run, and emit whether an executable is made; with
	// neither, a module's generate only finds its instances.
	capture **interp.Program
	emit    bool
	matters func(code string) bool

	// bodied is how many errors each module had when its bodies were done.
	bodied []int
	// verified is whether each module's verify ran and found nothing.
	verified []bool
	// reached is whether the module and every module it reaches is verified.
	reached  []bool
	outputs  []moduleOutput
	unfinite [][]*diag.CompileError
	// crashes are the panics of a generate, which are raised only if the program has no error: a generate that started
	// before another module's error was found is dropped with all it made, and so is what it did wrong.
	crashes []*par.WorkerPanic
	units   []*backend.CodeUnit

	// summaries, keys, cached and needCheck are the build cache's part of the phase, made by consultCache for a compile that
	// makes an executable with a cache: what each module shows to the modules that reach it, the key of each module's entry,
	// the entry that was found (nil for a miss) and whether the module's check has to run. Without a cache they are nil.
	summaries []moduleSummary
	keys      []cache.Key
	cached    []*cachedModule
	needCheck []bool
	// cacheRead and cacheWritten are the sizes the cache had read and written when the phase began, so the phase can say
	// how much it did itself.
	cacheRead, cacheWritten int64
	writeFailures           atomic.Int64

	bodiesFailed atomic.Bool
	anyFailed    atomic.Bool
	refused      atomic.Bool

	timings                            bool
	checkTime, generateTime, cacheTime atomic.Int64
}

func newModulePhase(proj *ast.Project, results []ast.ModuleResult, typings ast.Typings, graph *project.Graph, main *ast.FuncDecl, output string, o Options, capture **interp.Program) *modulePhase {
	n := len(proj.Modules)
	p := &modulePhase{
		proj: proj, results: results, typings: typings, graph: graph, main: main, options: o,
		capture: capture, emit: output != "",
		matters:  func(string) bool { return true },
		bodied:   make([]int, n),
		verified: make([]bool, n),
		reached:  make([]bool, n),
		timings:  o.Timings,
	}
	if p.capture != nil {
		p.matters = func(code string) bool { return code != diag.ErrInterpreterOnly }
	}
	p.layout = taskLayout{modules: n, runtime: p.emit && p.capture == nil}
	if p.capture != nil || p.emit {
		p.outputs = make([]moduleOutput, n)
	}
	p.unfinite = make([][]*diag.CompileError, n)
	p.crashes = make([]*par.WorkerPanic, n)
	if p.layout.runtime {
		p.units = make([]*backend.CodeUnit, n+1)
	}
	return p
}

// run schedules every task over the jobs.
func (p *modulePhase) run() {
	if p.options.Cache != nil && p.emit && p.capture == nil {
		p.cacheRead, p.cacheWritten = p.options.Cache.BytesRead(), p.options.Cache.BytesWritten()
		stop := p.stopwatch(&p.cacheTime)
		p.consultCache()
		stop()
	}
	plan := modulePlan(p.graph, p.layout)
	par.Run(plan, p.options.Jobs, func() *generateScratch { return &generateScratch{} }, p.task)
}

func (p *modulePhase) task(w *generateScratch, task int) {
	m, kind, ofModule := p.layout.module(task)
	if !ofModule {
		p.emitRuntime()
		return
	}
	switch kind {
	case taskCheck:
		p.check(m)
	case taskClosure:
		p.closure(m)
	case taskGenerate:
		p.generateTask(w, m)
	default:
		panic(diag.Internal("task %d of module %d has no kind", int(kind), m))
	}
}

func (p *modulePhase) emitRuntime() {
	defer trace.StartRegion(context.Background(), "runtime").End()
	p.units[0] = backend.EmitRuntime(p.main, backend.RuntimeOptions{CountAllocations: p.options.CountAllocations})
}

// check types a module's bodies and then verifies it, while its AST and types are in cache. Verify does not run on a module
// that has body errors, nor once any module has had them, which keeps the errors independent of which worker got there
// first: a verify that ran meanwhile is dropped by collectErrors.
func (p *modulePhase) check(id ast.ModuleID) {
	if p.skipsCheck(id) {
		p.verified[id] = true
		return
	}
	defer trace.StartRegion(context.Background(), "check").End()
	defer p.stopwatch(&p.checkTime)()
	m, res := p.proj.Modules[id], &p.results[id]
	checkBodies(p.proj, m, res)
	p.bodied[id] = len(res.Errors)
	switch {
	case p.bodied[id] > 0:
		p.anyFailed.Store(true)
		p.bodiesFailed.Store(true)
	case !p.bodiesFailed.Load():
		verifyModule(p.proj, m, res)
		p.verified[id] = len(res.Errors) == 0
		if !p.verified[id] {
			p.anyFailed.Store(true)
		}
	}
}

// closure joins the checks of the modules the module reaches, which have all finished before it runs.
func (p *modulePhase) closure(id ast.ModuleID) {
	reached := p.verified[id]
	for _, d := range p.graph.Deps[id] {
		reached = reached && p.reached[d.Module]
	}
	p.reached[id] = reached
}

// generateTask is a module's last task. With an error anywhere, or in what the module reaches, there is nothing to make of
// the module, and lowering what has an error could fail on what the checker would have refused.
func (p *modulePhase) generateTask(w *generateScratch, id ast.ModuleID) {
	if p.hit(id) || p.anyFailed.Load() || !p.reached[id] {
		return
	}
	defer trace.StartRegion(context.Background(), "generate").End()
	defer p.stopwatch(&p.generateTime)()
	defer func() {
		if r := recover(); r != nil {
			p.crashes[id] = par.Wrap(r)
		}
	}()
	if p.capture != nil || p.emit {
		p.generate(w, id)
	} else {
		diag.Guard(&p.unfinite[id], func() { ir.Instances(p.typings, p.results[id].Callers) })
	}
}

// generate finds the instances a module needs, lowers its functions and those instances, and emits its code unit
// straight away, so the IR is consumed while it is in cache. A module that needs an instance that another module
// needs too lowers and emits it as well, and the link keeps one copy. With capture it hands the IR to the interpreter
// instead of emitting code. A program that has refusals gets no executable and the code emitted for it is dropped; a
// module with a refusal is not emitted at all, because its IR holds what codegen cannot handle.
func (p *modulePhase) generate(w *generateScratch, id ast.ModuleID) {
	out := &p.outputs[id]
	var instances *ir.InstanceSet
	diag.Guard(&p.unfinite[id], func() { instances = ir.Instances(p.typings, p.results[id].Callers) })
	if len(p.unfinite[id]) > 0 {
		p.refused.Store(true)
		return
	}
	if w.lowering == nil {
		w.lowering = ir.NewLowering(p.typings)
	}
	// What the interpreter runs is kept, so its IR stays where it is; the IR that is only emitted is dead once its
	// module is, and the next module lowers over it.
	if p.capture == nil {
		w.lowering.Reset()
	}
	fns := w.fns[:0]
	for _, f := range p.results[id].Functions {
		if len(f.Generics) == 0 {
			fns = append(fns, w.lowering.Lower(f, instances))
		}
	}
	for _, inst := range instances.List {
		fns = append(fns, w.lowering.Lower(inst, instances))
	}
	w.fns = fns
	out.refusals = refusals(fns, p.matters)
	out.usesArrays = usesArrays(fns)
	if p.options.EmitIr {
		var text strings.Builder
		for _, fn := range fns {
			ir.PrintIr(fn, &text)
		}
		out.irText = text.String()
	}
	switch {
	case p.capture != nil:
		out.functions = slices.Clone(fns)
	case len(out.refusals) > 0:
		p.refused.Store(true)
	case !p.refused.Load() || p.keys != nil:
		unit := backend.EmitModule(fns)
		p.units[id+1] = unit
		p.remember(id, unit, out)
	}
}

// generateScratch is what a worker reuses from one module to the next, so lowering a module allocates nothing once the
// buffers have grown to the largest one. A worker that only checks never makes the lowering.
type generateScratch struct {
	lowering *ir.Lowering
	fns      []*ir.IrFunction
}

// raiseCrash raises the panic of the first module whose generate had one.
func (p *modulePhase) raiseCrash() {
	for _, crash := range p.crashes {
		if crash != nil {
			panic(crash)
		}
	}
}

// conclude is everything after the phase of a program that has no error: what the modules found is reported, and the
// units are linked into the executable, or the IR is handed to the interpreter.
func (p *modulePhase) conclude(output string, mark func(string)) []*diag.CompileError {
	if failure := firstFailure(p.unfinite); failure != nil {
		return failure
	}
	if p.capture == nil && !p.emit {
		return nil
	}
	if p.options.EmitIr {
		writeIr(output+".ir", p.outputs)
	}
	var blocked []*diag.CompileError
	for i := range p.outputs {
		blocked = append(blocked, p.outputs[i].refusals...)
	}
	if len(blocked) > 0 {
		return withoutRepeats(sortedErrors(p.proj, blocked))
	}
	if p.capture != nil {
		*p.capture = interp.NewProgram(functionsOnce(p.outputs), p.main)
		return nil
	}
	units := append(p.units, backend.EmitTypeRoutines(p.units, p.options.Jobs)...)
	if slices.ContainsFunc(p.outputs, func(out moduleOutput) bool { return out.usesArrays }) {
		units = append(units, backend.EmitArrayRuntime())
	}
	mark("routines")
	backend.Link(output, units, ir.RtEntry.String(), p.options.Jobs)
	mark("link")
	return nil
}

// stopwatch adds the time until the function it returns is called to total, if --timings asked for the times.
func (p *modulePhase) stopwatch(total *atomic.Int64) func() {
	if !p.timings {
		return func() {}
	}
	start := time.Now()
	return func() { total.Add(int64(time.Since(start))) }
}

// reportTimings prints the time the tasks took, summed over the workers, beside the phase's line with its busy cores.
func (p *modulePhase) reportTimings() {
	if !p.timings {
		return
	}
	ms := func(total *atomic.Int64) float64 { return float64(total.Load()) / nanosPerMillisecond }
	fmt.Fprintf(os.Stderr, "  check %.1f ms, generate %.1f ms of task time\n", ms(&p.checkTime), ms(&p.generateTime))
	if p.keys != nil {
		s := p.cacheStats()
		fmt.Fprintf(os.Stderr, "  cache: %d hits, %d misses, %d checks skipped, %.1f MB read, %.1f MB written, lookup %.1f ms of task time\n",
			s.Hits, s.Misses, s.ChecksSkipped, float64(s.BytesRead)/bytesPerMegabyte, float64(s.BytesWritten)/bytesPerMegabyte, ms(&p.cacheTime))
		if s.WriteFailures > 0 {
			fmt.Fprintf(os.Stderr, "  cache: %d entries could not be written\n", s.WriteFailures)
		}
	}
}
