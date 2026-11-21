package interp

import (
	"fmt"
	"math/rand/v2"
	"runtime"
	"runtime/debug"

	"minimaxon/internal/diag"
	"minimaxon/internal/ir"
)

// The scheduler runs the threads of a program the way Verona's behaviour-oriented concurrency describes it, and decides
// by a seeded random choice which of them takes the next step. main is the first thread. A `when` makes a behaviour: it
// is queued on every cown it names, in the order the behaviours were scheduled, and it runs once it is first in all of
// those queues. Behaviours on disjoint cowns can run at once and in any order, and several that run at once are
// interleaved a few instructions at a time. Only one thread executes at any moment, so the run is deterministic for a
// seed; what is simulated is the interleaving, which is where a data race would show.
//
// A thread is a goroutine that runs only while the scheduler has handed it the turn. It gives the turn back after a
// random number of instructions (its burst), and again when it finishes.

const (
	// shortBurst, mediumBurst and longBurst are the longest stretches of instructions a thread runs without giving the turn
	// back. Each stretch is a random length up to one of them, so a run has a few very fine interleavings and a few coarse
	// ones.
	shortBurst  = 2
	mediumBurst = 8
	longBurst   = 64
)

var burstLimits = [...]int{shortBurst, mediumBurst, longBurst}

// seedStream is the second word of the generator's state, fixed so that a seed alone names a schedule.
const seedStream = 0x9e3779b97f4a7c15

// thread is one running line of execution: main, or the body of a behaviour.
type thread struct {
	id   int
	name string
	// behaviour is the behaviour this thread runs, or nil for main.
	behaviour *behaviour
	resume    chan struct{}
	done      bool
	burst     int
	// The registers of the interpreter that belong to the thread, kept here while another thread runs.
	top     *frame
	depth   int
	windows []window
}

// behaviour is a scheduled `when`: its body, what it captured, and the cowns it waits for.
type behaviour struct {
	body     *ir.IrFunction
	captures []value
	cowns    []*object
	seq      int
	started  bool
}

// sched is the scheduler's state, which the interpreter embeds.
type sched struct {
	rng *rand.Rand
	cur *thread
	// threads are the threads that have started, in the order they did.
	threads []*thread
	// pending are the behaviours that have not started, in the order they were scheduled.
	pending   []*behaviour
	scheduled int
	// concurrent is whether a behaviour has ever been scheduled: until then there is one thread and nothing to interleave.
	concurrent bool
	// tracking is whether the heap has anything that only some threads may touch: a frozen value or a cown has been made.
	tracking bool
	back     chan struct{}
	fatal    any
	aborting bool
	mainExit int
}

func newSched(seed int64) sched {
	return sched{rng: rand.New(rand.NewPCG(uint64(seed), seedStream)), back: make(chan struct{})}
}

// run executes the program: main first, then behaviours until none is left. A signal that ends the program (a trap, a
// violation, a timeout) or an internal error ends it at once and is left in vm.fatal.
func (vm *vm) run(main *ir.IrFunction) {
	vm.start(&thread{name: main.Symbol()}, func() {
		exit, _ := vm.call(main, nil)
		vm.mainExit = int(int32(exit.n))
	})
	for vm.fatal == nil {
		candidates := vm.candidates()
		if len(candidates) == 0 {
			break
		}
		vm.resume(candidates[vm.rng.IntN(len(candidates))])
	}
	if vm.fatal != nil {
		vm.abort()
		if signal, ok := vm.fatal.(*threadPanic); ok {
			panic(signal.err)
		}
		return
	}
	if len(vm.pending) > 0 {
		panic(diag.Internal("interp: %d behaviours are queued and none can run", len(vm.pending)))
	}
}

// threadPanic is an internal error raised in a thread, which the goroutine that called Run raises again, with the stack
// of the thread that failed.
type threadPanic struct{ err *diag.InternalError }

// start makes the goroutine of a thread. It waits for its first turn.
func (vm *vm) start(t *thread, body func()) {
	t.id = len(vm.threads)
	t.resume = make(chan struct{})
	vm.threads = append(vm.threads, t)
	go func() {
		defer func() {
			if vm.aborting {
				vm.back <- struct{}{}
				return
			}
			if r := recover(); r != nil {
				switch r.(type) {
				case *exitSignal, *violationSignal, *timeoutSignal:
					vm.fatal = r
				default:
					vm.fatal = &threadPanic{diag.Internal("%v\n%s", r, debug.Stack())}
				}
			}
			t.done = true
			vm.back <- struct{}{}
		}()
		<-t.resume
		if vm.aborting {
			runtime.Goexit()
		}
		body()
	}()
}

// candidates are the threads that can take the next step: every one that has started and not finished, and every behaviour
// that has not started and holds the front of the queue of each cown it names.
func (vm *vm) candidates() []any {
	var out []any
	for _, t := range vm.threads {
		if !t.done {
			out = append(out, t)
		}
	}
	for _, b := range vm.pending {
		if b.runnable() {
			out = append(out, b)
		}
	}
	return out
}

func (b *behaviour) runnable() bool {
	for _, c := range b.cowns {
		if c.queue[0] != b {
			return false
		}
	}
	return true
}

// resume gives the turn to a thread, or starts a behaviour that has become runnable and gives it the turn, and returns when
// it gives the turn back.
func (vm *vm) resume(next any) {
	var t *thread
	switch n := next.(type) {
	case *thread:
		t = n
	case *behaviour:
		t = vm.begin(n)
	default:
		panic(diag.Internal("interp: cannot schedule %T", next))
	}
	vm.cur = t
	vm.top, vm.depth, vm.windows = t.top, t.depth, t.windows
	t.burst = 1 + vm.rng.IntN(burstLimits[vm.rng.IntN(len(burstLimits))])
	t.resume <- struct{}{}
	<-vm.back
}

// begin starts the thread of a behaviour that has every cown it named. The body takes access to each cown's value, and then
// what was captured.
func (vm *vm) begin(b *behaviour) *thread {
	b.started = true
	for i, p := range vm.pending {
		if p == b {
			vm.pending = append(vm.pending[:i], vm.pending[i+1:]...)
			break
		}
	}
	t := &thread{name: b.body.Symbol(), behaviour: b}
	for _, c := range b.cowns {
		c.running = b
	}
	args := make([]value, 0, len(b.cowns)+len(b.captures))
	for _, c := range b.cowns {
		args = append(args, value{adr: &address{cell: &c.fields[0], version: c.version}})
	}
	args = append(args, b.captures...)
	vm.start(t, func() {
		vm.call(b.body, args)
		vm.finish(b)
	})
	return t
}

// finish releases the cowns of a behaviour that has run: the next behaviour in each queue may now start, and the count
// the behaviour held on each is given back, which frees a cown that nothing else refers to.
func (vm *vm) finish(b *behaviour) {
	for _, c := range b.cowns {
		if c.queue[0] != b {
			panic(diag.Internal("interp: a behaviour finished that was not first in the queue of its cown"))
		}
		c.queue = c.queue[1:]
		c.running = nil
		vm.unref(c)
	}
	vm.runEndings()
}

// yield gives the turn back to the scheduler and waits for the next one. The interpreter's registers belong to the thread
// that holds the turn, so they are saved here and restored by whoever resumes this thread.
func (vm *vm) yield() {
	t := vm.cur
	t.top, t.depth, t.windows = vm.top, vm.depth, vm.windows
	vm.back <- struct{}{}
	<-t.resume
	if vm.aborting {
		runtime.Goexit()
	}
}

// abort ends the goroutines of threads that are waiting for a turn that will not come.
func (vm *vm) abort() {
	vm.aborting = true
	for _, t := range vm.threads {
		if !t.done {
			close(t.resume)
			<-vm.back
		}
	}
}

// schedule is a `when`: the behaviour joins the queue of each cown it names and holds a count on each until it has run.
func (vm *vm) schedule(fr *frame, w *ir.WhenInstr) {
	vm.tracking = true
	vm.concurrent = true
	body, cownRegs, captureRegs := w.Body(fr.fn), w.Cowns(fr.fn), w.Captures(fr.fn)
	cowns := make([]*object, len(cownRegs))
	for i, r := range cownRegs {
		o := vm.live(fr.regs[r], "a behaviour's cown")
		if o.kind != kindCown {
			panic(diag.Internal("interp: a behaviour names %s, which is not a cown", o))
		}
		for _, other := range cowns[:i] {
			if other == o {
				vm.trap("a behaviour names the same cown twice")
			}
		}
		cowns[i] = o
	}
	captures := make([]value, len(captureRegs))
	for i, r := range captureRegs {
		v := fr.regs[r]
		if fr.fn.Regs[r] == ir.KindManaged {
			o := vm.live(v, "a captured value")
			if !o.immortal && !o.shared() && o.owner != nil {
				vm.violate(Race, "the behaviour captures %s, which %s still owns: the enclosing thread and the behaviour would both reach it", o, o.owner)
			}
		}
		captures[i] = v
	}
	b := &behaviour{body: body, captures: captures, cowns: cowns, seq: vm.scheduled}
	vm.scheduled++
	for _, o := range cowns {
		o.count++
		o.queue = append(o.queue, b)
	}
	vm.pending = append(vm.pending, b)
}

// A value belongs to the thread, the cown or the frozen value that owns it, found by following its owners up to a
// frame's slot, the value of a cown, or the contents of a frozen value. A value that nothing owns yet, such as the result
// of a call, belongs to whoever holds it.

type domainKind uint8

const (
	domainNone domainKind = iota
	domainThread
	domainCown
	domainFrozen
)

type domain struct {
	kind   domainKind
	thread *thread
	cown   *object
}

// domainOfObject is where an object belongs. A frozen value or a cown, which any thread may hold a reference to, belongs
// to no one: it is what is inside that belongs to it.
func (vm *vm) domainOfObject(o *object) domain {
	if o.immortal || o.shared() {
		return domain{}
	}
	for {
		c := o.owner
		if c == nil {
			return domain{}
		}
		if d, ended := vm.domainOfCell(c); ended {
			return d
		}
		o = c.holder
	}
}

// domainOfCell is where a place belongs, when that is settled by the place alone: a slot of a frame, or a place in a
// frozen value or a cown. A place in any other object belongs wherever that object does, which is for the caller to ask.
func (vm *vm) domainOfCell(c *cell) (domain, bool) {
	switch {
	case c.frame != nil:
		return domain{kind: domainThread, thread: c.frame.thread}, true
	case c.holder == nil:
		return domain{}, true
	case c.holder.kind == kindFrozen:
		return domain{kind: domainFrozen}, true
	case c.holder.kind == kindCown:
		return domain{kind: domainCown, cown: c.holder}, true
	}
	return domain{}, false
}

func (vm *vm) domainOfPlace(c *cell) domain {
	if d, ended := vm.domainOfCell(c); ended {
		return d
	}
	return vm.domainOfObject(c.holder)
}

// requireReachable is the rule against a data race: a thread touches only what it owns, the value of a cown while its own
// behaviour holds the cown, and what is frozen.
func (vm *vm) requireReachable(o *object, what string) {
	vm.requireDomain(vm.domainOfObject(o), o.String(), what)
}

func (vm *vm) requireReachableCell(c *cell, what string) {
	vm.requireDomain(vm.domainOfPlace(c), c.String(), what)
}

func (vm *vm) requireDomain(d domain, what, use string) {
	switch d.kind {
	case domainThread:
		if d.thread != vm.cur {
			vm.violate(Race, "%s uses %s, which belongs to the thread %s, and this is the thread %s: a data race", use, what, d.thread.describe(), vm.cur.describe())
		}
	case domainCown:
		if d.cown.running == nil || d.cown.running != vm.cur.behaviour {
			vm.violate(Race, "%s uses %s, the value of %s, but the thread %s holds no behaviour that has the cown: a data race", use, what, d.cown, vm.cur.describe())
		}
	}
}

func (t *thread) describe() string {
	if t.behaviour == nil {
		return t.name
	}
	return fmt.Sprintf("%s (behaviour #%d)", t.name, t.behaviour.seq)
}

// requireMutable refuses a change to what is frozen.
func (vm *vm) requireMutable(r region) {
	var d domain
	if r.cell != nil {
		d = vm.domainOfPlace(r.cell)
	} else {
		d = vm.domainOfObject(r.obj)
	}
	if d.kind == domainFrozen {
		vm.violate(Immutability, "%s is part of a frozen value, which nothing may change", r)
	}
}
