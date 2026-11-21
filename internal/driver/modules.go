package driver

import (
	"context"
	"fmt"
	"minimaxon/internal/ast"
	"minimaxon/internal/backend"
	"minimaxon/internal/cache"
	"minimaxon/internal/check"
	"minimaxon/internal/diag"
	"minimaxon/internal/interp"
	"minimaxon/internal/ir"
	"minimaxon/internal/par"
	"minimaxon/internal/project"
	"minimaxon/internal/syntax"
	"os"
	"runtime/trace"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

// taskKind is one of the tasks a module has. Parse, resolve, aliases, declare and constants are the front; check types
// the module's bodies and verifies it. Closure is the join of the module's check and the closures of the modules it
// depends on: it is where "everything this module reaches is checked" is known, without any module holding the set of the
// modules it reaches. Generate is what the module needs that for: it lowers the module's functions and its instances and
// emits its code, or, for a compile that makes nothing, only finds the instances. Probe and demand are the build cache's
// (see buildcache.go), and only a compile with a cache has them.
type taskKind int

const (
	taskParse taskKind = iota
	taskResolve
	taskAliases
	taskDeclare
	taskConstants
	taskCheck
	taskClosure
	taskGenerate
	taskProbe
	taskDemand
	taskKinds
)

// tasksWithoutCache is how many tasks a module has in a compile with no build cache: those before the cache's.
const tasksWithoutCache = int(taskProbe)

var taskNames = [taskKinds]string{"parse", "resolve", "aliases", "declare", "constants", "check", "closure", "generate", "probe", "demand"}

// stage is a step of a module's tasks that can report errors, in the order a program's errors are chosen by: the errors
// a program gets are those of the first stage that has any, from every module, as if each stage were a pass over every
// module that stopped the compile when it failed. A task holds one stage or two run one after the other (parse holds the
// syntax and the table, constants their order and their types, check the bodies and verify). The import cycle is a stage
// of no task, found from the module graph when tasks are left unrun.
type stage int32

const (
	stageSyntax stage = iota
	stageTable
	stageResolve
	stageCycle
	stageAliases
	stageDeclare
	stageConstOrder
	stageConstants
	stageBodies
	stageVerify
	stageGenerate
	stageCount
)

// taskStages is the stage each kind of task starts in, which a panic before its first stage is reported with. Closure,
// which only joins, counts as the generate it is for, and the cache's tasks as the check they decide on.
var taskStages = [taskKinds]stage{stageSyntax, stageResolve, stageAliases, stageDeclare, stageConstOrder, stageBodies, stageGenerate, stageGenerate, stageBodies, stageBodies}

// stageEnds is how many errors a module had when each of its stages ended, by stage; a stage that did not run has 0.
type stageEnds [stageCount]int

// lowestStage is the first stage at which something happened, such as an error, kept by any number of workers at once.
// It holds the stage plus one, so that its zero value says nothing has happened.
type lowestStage struct{ at atomic.Int32 }

func (l *lowestStage) lower(s stage) {
	for {
		held := l.at.Load()
		if held != 0 && held <= int32(s)+1 {
			return
		}
		if l.at.CompareAndSwap(held, int32(s)+1) {
			return
		}
	}
}

func (l *lowestStage) get() (s stage, ok bool) {
	held := l.at.Load()
	return stage(held - 1), held != 0
}

// before is whether it happened at a stage before s.
func (l *lowestStage) before(s stage) bool {
	at, ok := l.get()
	return ok && at < s
}

// taskLayout numbers the tasks of the phase. A compile that makes an executable has a task that emits the runtime unit,
// and a compile with a build cache one that joins every module's resolve to find who depends on whom; they come before the
// modules' own, so a worker takes them as soon as they are ready. A module's tasks are numbered together, in module order,
// so the lowest ready task is the lowest module's.
type taskLayout struct {
	modules int
	runtime bool
	cache   bool
}

func (l taskLayout) first() int {
	n := 0
	if l.runtime {
		n++
	}
	if l.cache {
		n++
	}
	return n
}

func (l taskLayout) perModule() int {
	if l.cache {
		return int(taskKinds)
	}
	return tasksWithoutCache
}

func (l taskLayout) count() int { return l.first() + l.modules*l.perModule() }

func (l taskLayout) task(m ast.ModuleID, kind taskKind) int {
	return l.first() + int(m)*l.perModule() + int(kind)
}

// tasksOf are the tasks of one kind of the modules.
func (l taskLayout) tasksOf(deps []ast.ModuleDep, kind taskKind) []int {
	tasks := make([]int, len(deps))
	for i, d := range deps {
		tasks[i] = l.task(d.Module, kind)
	}
	return tasks
}

// dependOn gives the tasks of module id that read what other modules made the same tasks of the modules it depends on to
// wait for: its aliases, its constants (which waited for their module's declare), its closure and, with a cache, its
// probe. Its resolve calls it, while those tasks wait for it.
func (l taskLayout) dependOn(edges par.Edges, id ast.ModuleID, deps []ast.ModuleDep) {
	if len(deps) == 0 {
		return
	}
	kinds := []taskKind{taskAliases, taskConstants, taskClosure}
	if l.cache {
		kinds = append(kinds, taskProbe)
	}
	for _, kind := range kinds {
		edges.Add(l.task(id, kind), l.tasksOf(deps, kind)...)
	}
}

// runtimeTask is the task that emits the runtime unit, and dependentsTask the cache's join of every resolve.
func (l taskLayout) runtimeTask() int { return 0 }

func (l taskLayout) dependentsTask() int { return l.first() - 1 }

// module is the module a task works on and what it does there. The runtime task and the join of the resolves are none of
// the modules.
func (l taskLayout) module(task int) (m ast.ModuleID, kind taskKind, ofModule bool) {
	if task < l.first() {
		return 0, 0, false
	}
	at := task - l.first()
	return ast.ModuleID(at / l.perModule()), taskKind(at % l.perModule()), true
}

// modulePlan is the order of the tasks, as far as it is known before any module is parsed: each task of a module after
// the one before it. What a module waits for in other modules is added as the module's tasks find it (see the stages in
// front.go): its parse gives its resolve the parses of the modules it names, and its resolve gives its aliases, constants
// and closure those of the modules it depends on, and so on. Generate needs the module's closure: it reads the bodies and
// the check results of the generics it reaches in other modules, and the marks that verify leaves on their constants, but
// nothing of a module that depends on it, so the modules that depend on it can start before it does. A module is the group
// of its tasks, so a worker that has parsed a module goes on to resolve it, and so on to its generate, while the module is
// in cache.
func modulePlan(layout taskLayout) *par.Plan {
	n := layout.count()
	plan := &par.Plan{Needs: make([][]int, n), Group: make([]int, n)}
	backing := make([]int, 0, n+layout.modules)
	need := func(task int, needs ...int) {
		start := len(backing)
		backing = append(backing, needs...)
		plan.Needs[task] = backing[start:len(backing):len(backing)]
	}
	for i := range layout.modules {
		m := ast.ModuleID(i)
		for kind := range taskKind(layout.perModule()) {
			plan.Group[layout.task(m, kind)] = i
		}
		for kind := taskResolve; kind < taskProbe; kind++ {
			need(layout.task(m, kind), layout.task(m, kind-1))
		}
		if layout.cache {
			need(layout.task(m, taskProbe), layout.task(m, taskConstants))
			need(layout.task(m, taskDemand), layout.dependentsTask())
			need(layout.task(m, taskCheck), layout.task(m, taskConstants), layout.task(m, taskProbe))
		}
	}
	if layout.cache {
		resolves := make([]int, layout.modules)
		for i := range resolves {
			resolves[i] = layout.task(ast.ModuleID(i), taskResolve)
		}
		plan.Needs[layout.dependentsTask()] = resolves
		plan.Group[layout.dependentsTask()] = layout.modules
	}
	if layout.runtime {
		// The runtime calls main, which the root module's declare finds.
		need(layout.runtimeTask(), layout.task(0, taskDeclare))
		plan.Group[layout.runtimeTask()] = layout.modules + 1
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
	usesBytes  bool
	usesOs     bool
}

// modulePhase is the compile after discovery: every task of every module, each as soon as what it needs is done. Every
// task writes the slots of its own module, so the phase needs no locks beyond the scheduler's, and what follows reads the
// slots in module order, so nothing it reports depends on the schedule.
//
// A stage runs only while no stage before it has failed in any module, so a module's stage never reads a result that has
// an error, and a stage that ran meanwhile is dropped with what it found: the program's errors are those of the first
// stage that has any (stageEnds), whichever worker got where first. A module is generated only if its own check and the
// check of every module it reaches found nothing, so a module that has an error is never lowered.
type modulePhase struct {
	proj    *ast.Project
	results []ast.ModuleResult
	typings ast.Typings
	options Options
	layout  taskLayout
	// capture is where the IR goes for the interpreter, if it is run, and emit whether an executable is made; with
	// neither, a module's generate only finds its instances.
	capture **interp.Program
	emit    bool
	matters func(code string) bool

	// main is the root module's entry, which its declare finds.
	main *ast.FuncDecl
	ends []stageEnds
	// failed is the first stage that had an error or a panic; errored and crashed say which.
	failed, errored, crashed lowestStage
	// crashes are the panics of the tasks, by task, with the stage each was in. A panic is raised only if no stage before
	// its own has an error, which is when every module ran that stage, so it does not depend on the schedule.
	crashes     []*par.WorkerPanic
	crashStages []stage
	// ran is how many tasks ran: a task that is left over waits on a cycle.
	ran atomic.Int64

	// verified is whether each module's verify ran and found nothing.
	verified []bool
	// reached is whether the module and every module it reaches is verified.
	reached  []bool
	outputs  []moduleOutput
	unfinite [][]*diag.CompileError
	units    []*backend.CodeUnit

	// graph, summaries, keys, cached, missReaches and needCheck are the build cache's part of the phase, for a compile that
	// makes an executable with a cache: the module graph once every module is resolved, what each module shows to the
	// modules that reach it, the key of each module's entry, the entry that was found (nil for a miss), whether a miss
	// reaches the module and whether the module's check has to run. Without a cache they are nil.
	graph       *project.Graph
	summaries   []moduleSummary
	keys        []cache.Key
	cached      []*cachedModule
	missReaches []bool
	needCheck   []bool
	// cacheRead and cacheWritten are the sizes the cache had read and written when the phase began, so the phase can say
	// how much it did itself.
	cacheRead, cacheWritten int64
	writeFailures           atomic.Int64

	refused atomic.Bool

	timings bool
	times   [taskKinds]atomic.Int64
}

func newModulePhase(proj *ast.Project, results []ast.ModuleResult, output string, o Options, capture **interp.Program) *modulePhase {
	n := len(proj.Modules)
	p := &modulePhase{
		proj: proj, results: results, options: o,
		capture: capture, emit: output != "",
		matters:  func(string) bool { return true },
		ends:     make([]stageEnds, n),
		verified: make([]bool, n),
		reached:  make([]bool, n),
		unfinite: make([][]*diag.CompileError, n),
		timings:  o.Timings,
	}
	p.typings = make(ast.Typings, n)
	for i := range results {
		p.typings[i] = &results[i].Typing
	}
	if p.capture != nil {
		p.matters = func(code string) bool { return code != diag.ErrInterpreterOnly }
	}
	p.layout = taskLayout{modules: n, runtime: p.emit && p.capture == nil, cache: o.Cache != nil && p.emit && p.capture == nil}
	if p.capture != nil || p.emit {
		p.outputs = make([]moduleOutput, n)
	}
	if p.layout.runtime {
		p.units = make([]*backend.CodeUnit, n+1)
	}
	if p.layout.cache {
		p.summaries = make([]moduleSummary, n)
		p.keys = make([]cache.Key, n)
		p.cached = make([]*cachedModule, n)
		p.missReaches = make([]bool, n)
		p.needCheck = make([]bool, n)
		p.cacheRead, p.cacheWritten = o.Cache.BytesRead(), o.Cache.BytesWritten()
	}
	tasks := p.layout.count()
	p.crashes = make([]*par.WorkerPanic, tasks)
	p.crashStages = make([]stage, tasks)
	return p
}

// run schedules every task over the jobs.
func (p *modulePhase) run() {
	par.Run(modulePlan(p.layout), p.options.Jobs, func() *workerScratch { return &workerScratch{} }, p.task)
}

// workerScratch is what a worker reuses from one module to the next, so reading a module, lowering it and following the
// values of its functions in verify allocate nothing once the buffers have grown to the largest one, and the stage the
// worker is in, which a panic is reported with. A worker makes what it reuses when it first needs it.
type workerScratch struct {
	syntax   *syntax.Scratch
	lowering *ir.Lowering
	fns      []*ir.IrFunction
	flow     check.FlowScratch
	stage    stage
}

func (p *modulePhase) task(w *workerScratch, task int, edges par.Edges) {
	p.ran.Add(1)
	defer func() {
		if r := recover(); r != nil {
			p.crashes[task], p.crashStages[task] = par.Wrap(r), w.stage
			p.crashed.lower(w.stage)
			p.failed.lower(w.stage)
		}
	}()
	m, kind, ofModule := p.layout.module(task)
	switch {
	case !ofModule && p.layout.runtime && task == p.layout.runtimeTask():
		w.stage = stageGenerate
		p.emitRuntime()
		return
	case !ofModule:
		w.stage = stageBodies
		p.findDependents(edges)
		return
	}
	w.stage = taskStages[kind]
	defer trace.StartRegion(context.Background(), taskNames[kind]).End()
	defer p.stopwatch(&p.times[kind])()
	switch kind {
	case taskParse:
		p.parse(w, m, edges)
	case taskResolve:
		p.resolve(w, m, edges)
	case taskAliases:
		p.stage(w, m, stageAliases, func() { check.DeclareAliases(p.proj, p.proj.Modules[m], &p.results[m]) })
	case taskDeclare:
		p.declare(w, m)
	case taskConstants:
		p.constants(w, m)
	case taskProbe:
		p.probeTask(m, edges)
	case taskDemand:
		p.demand(m)
	case taskCheck:
		p.check(w, m)
	case taskClosure:
		p.closure(m)
	case taskGenerate:
		p.generateTask(w, m)
	default:
		panic(diag.Internal("task %d of module %d has no kind", int(kind), m))
	}
}

// stage runs one stage of a module, unless a stage before it has failed in some module, and counts the errors the module
// has when it ends. Since no stage before it failed, the module had none before, so any it has are this stage's.
func (p *modulePhase) stage(w *workerScratch, id ast.ModuleID, s stage, do func()) {
	if p.failed.before(s) {
		return
	}
	w.stage = s
	do()
	n := len(p.results[id].Errors)
	p.ends[id][s] = n
	if n > 0 {
		p.errored.lower(s)
		p.failed.lower(s)
	}
}

func (p *modulePhase) emitRuntime() {
	if p.failed.before(stageGenerate) {
		return
	}
	defer trace.StartRegion(context.Background(), "runtime").End()
	p.units[0] = backend.EmitRuntime(p.main, backend.RuntimeOptions{CountAllocations: p.options.CountAllocations})
}

// check types a module's bodies and then verifies it, while its AST and types are in cache. Verify does not run on a module
// that has body errors, nor once any module has had them.
func (p *modulePhase) check(w *workerScratch, id ast.ModuleID) {
	if !p.needsCheck(id) {
		p.verified[id] = true
		return
	}
	m, res := p.proj.Modules[id], &p.results[id]
	p.stage(w, id, stageBodies, func() { checkBodies(p.proj, m, res) })
	p.stage(w, id, stageVerify, func() {
		verifyModule(p.proj, m, res, &w.flow)
		p.verified[id] = len(res.Errors) == 0
	})
}

// closure joins the checks of the modules the module reaches, which have all finished before it runs.
func (p *modulePhase) closure(id ast.ModuleID) {
	reached := p.verified[id]
	for _, d := range p.results[id].Deps {
		reached = reached && p.reached[d.Module]
	}
	p.reached[id] = reached
}

// generateTask is a module's last task. With an error anywhere, or in what the module reaches, there is nothing to make of
// the module, and lowering what has an error could fail on what the checker would have refused.
func (p *modulePhase) generateTask(w *workerScratch, id ast.ModuleID) {
	if p.hit(id) || p.failed.before(stageGenerate) || !p.reached[id] {
		return
	}
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
func (p *modulePhase) generate(w *workerScratch, id ast.ModuleID) {
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
	out.usesBytes = usesBytes(fns)
	out.usesOs = usesOs(fns)
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

// errors are the program's errors once the phase is done, or nil if it has none, and raise the panic of a task that
// had one where it counts. The errors are those of the first stage that has any, from every module, in source order. A
// task left unrun waits on a cycle of the module graph, which is reported unless a stage before it (resolve, which the
// graph is made of) has errors; the graph is only looked at then, so a program without a cycle pays nothing for the check.
func (p *modulePhase) errors() []*diag.CompileError {
	at, failed := p.errored.get()
	var cycle *diag.CompileError
	if p.ran.Load() < int64(p.layout.count()) && (!failed || at > stageResolve) {
		if cycle = project.NewGraph(p.results).CheckAcyclic(p.proj); cycle == nil {
			panic(diag.Internal("tasks of the compile did not run, though the module graph has no cycle"))
		}
		at, failed = stageCycle, true
	}
	if crashAt, crashed := p.crashed.get(); crashed && (!failed || crashAt <= at) {
		p.raiseCrash(crashAt)
	}
	switch {
	case cycle != nil:
		return sortedErrors(p.proj, []*diag.CompileError{cycle})
	case !failed:
		return nil
	}
	var all []*diag.CompileError
	for i := range p.results {
		all = append(all, p.results[i].Errors[:p.ends[i][at]]...)
	}
	return sortedErrors(p.proj, all)
}

// raiseCrash raises the panic of the lowest task that had one at the stage.
func (p *modulePhase) raiseCrash(at stage) {
	for task, crash := range p.crashes {
		if crash != nil && p.crashStages[task] == at {
			panic(crash)
		}
	}
}

// frontSucceeded is whether every module got through the front with no error, which is when the build cache's part of the
// phase has run in full.
func (p *modulePhase) frontSucceeded() bool { return !p.failed.before(stageBodies) }

// conclude is everything after the phase of a program that has no error: what the modules found is reported, and the
// units are linked into the executable, or the IR is handed to the interpreter.
func (p *modulePhase) conclude(output string, mark func(string)) []*diag.CompileError {
	if failure := firstFailure(p.unfinite); failure != nil {
		return failure
	}
	if p.capture == nil && !p.emit {
		if p.proj.Root.IsRuntime {
			return checkRuntimeFunctions(p.proj)
		}
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
	needsOs := slices.ContainsFunc(p.outputs, func(out moduleOutput) bool { return out.usesOs })
	if needsOs || slices.ContainsFunc(p.outputs, func(out moduleOutput) bool { return out.usesArrays }) {
		units = append(units, backend.EmitArrayRuntime())
	}
	if slices.ContainsFunc(p.outputs, func(out moduleOutput) bool { return out.usesBytes }) {
		units = append(units, backend.EmitBytesRuntime())
	}
	if needsOs {
		units = append(units, backend.EmitOsRuntime())
	}
	mark("routines")
	symbolMap := ""
	if p.options.EmitMap {
		symbolMap = output + ".map"
	}
	backend.Link(output, units, ir.RtEntry.String(), p.options.Jobs, symbolMap)
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

// reportTimings prints the time each kind of task took, summed over the workers, beside the phase's line with its busy
// cores.
func (p *modulePhase) reportTimings() {
	if !p.timings {
		return
	}
	var parts []string
	for kind := range taskKind(p.layout.perModule()) {
		parts = append(parts, fmt.Sprintf("%s %.1f ms", taskNames[kind], float64(p.times[kind].Load())/nanosPerMillisecond))
	}
	fmt.Fprintf(os.Stderr, "  %s of task time\n", strings.Join(parts, ", "))
	if p.keys != nil && p.frontSucceeded() {
		s := p.cacheStats()
		fmt.Fprintf(os.Stderr, "  cache: %d hits, %d misses, %d checks skipped, %.1f MB read, %.1f MB written\n",
			s.Hits, s.Misses, s.ChecksSkipped, float64(s.BytesRead)/bytesPerMegabyte, float64(s.BytesWritten)/bytesPerMegabyte)
		if s.WriteFailures > 0 {
			fmt.Fprintf(os.Stderr, "  cache: %d entries could not be written\n", s.WriteFailures)
		}
	}
}
