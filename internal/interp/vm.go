// Package interp runs the lowered IR of a whole program while it tracks the heap the way the memory model describes
// it, and reports a violation where the program breaks a rule: a value used after it was freed, freed twice, leaked,
// used after it moved, owned twice, or reached through two overlapping borrows. A program that the checker accepted
// must run clean, so a violation is a hole in the checker or in lowering. It is for tests and development, and never
// part of a compiled program.
//
// It also gives the primitives that the backend has no code for (map, list and pool) their meaning, and a trap in
// the program behaves as it does natively: the same standard output and the same exit code.
package interp

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"minimaxon/internal/ir"
	"slices"
	"strconv"
	"strings"
)

const (
	// panicExitCode is what a panic, and every check that the compiled code makes, exits with.
	panicExitCode = 1
	// illegalInstructionExitCode is what a program that reaches a trap terminator exits with when compiled: the
	// ud2 instruction raises STATUS_ILLEGAL_INSTRUCTION.
	illegalInstructionExitCode = -1073741795
	// stackOverflowExitCode is STATUS_STACK_OVERFLOW.
	stackOverflowExitCode = -1073741571
	maxCallDepth          = 100000
	// DefaultStepLimit is how many instructions a program may run before it is called a timeout.
	DefaultStepLimit = 500_000_000
	// maxLeaksReported keeps a report of a program that leaks everything short.
	maxLeaksReported = 8

	stdoutHandle = 1
	stderrHandle = 2
	heapHandle   = 3
	wordBytes    = 8
)

// Program is the lowered IR of a whole program.
type Program struct {
	Functions map[*ast.FuncDecl]*ir.IrFunction
	Main      *ast.FuncDecl
}

// NewProgram indexes the functions of a program by their declarations.
func NewProgram(fns []*ir.IrFunction, main *ast.FuncDecl) *Program {
	p := &Program{Functions: make(map[*ast.FuncDecl]*ir.IrFunction, len(fns)), Main: main}
	for _, f := range fns {
		p.Functions[f.Decl] = f
	}
	return p
}

// Result is what a run did. A program that panics or traps has an exit code like the executable's; a violation is
// separate from that, and a run that has one stops there.
type Result struct {
	Stdout     []byte
	Stderr     []byte
	ExitCode   int
	TimedOut   bool
	Violations []Violation
	// Steps is how many instructions ran.
	Steps int64
	// Frees is every value freed, in the order it was freed: a string by its text, anything else by its kind.
	Frees []string
	// Behaviours is how many behaviours the program scheduled, so a caller knows whether the schedule mattered.
	Behaviours int
}

// exitSignal ends the program: a panic, a trap, or main returning.
type exitSignal struct{ code int }

type timeoutSignal struct{}

// window is a borrow that a call holds while the arguments after it are evaluated: the address or pointer is taken
// when its argument is, and used when the call runs.
type window struct {
	call *ir.Instr
	// block is the block the call is in, and def the block of the instruction whose borrow the window holds.
	block, def int
	frame      *frame
	region     region
	at         site
	taken      string
}

type vm struct {
	prog   *Program
	top    *frame
	depth  int
	steps  int64
	limit  int64
	nextID int
	all    []*object
	strs   map[string]*object
	stdout []byte
	stderr []byte
	frees  []string
	// endings are the destructors of cowns that were freed and still have to run, with the values they end, in the order
	// the cowns were freed. A cown can be freed deep inside the free of a record or an array, where a call cannot be made,
	// so the thread that dropped runs them once the instruction that dropped has finished.
	endings []ending
	windows []window
	infos   map[*ir.IrFunction]*info
	sched
}

// ending is a call of the destructor of a freed cown, with the value that was in the cown, which the destructor owns.
type ending struct {
	destructor *ir.IrFunction
	value      value
}

// info is what a function's IR says before it runs: for each instruction, the calls that hold its result as an argument
// while the arguments after it are evaluated.
type info struct {
	windowAt [][][]waitingCall
}

// waitingCall is a call that holds a borrow while its later arguments are evaluated, and the block it is in.
type waitingCall struct {
	call  *ir.Instr
	block int
}

// Options say how a program runs. The scheduler of behaviours is random, and a seed makes one schedule: the same seed
// always gives the same interleaving.
type Options struct {
	// StepLimit is how many instructions the program may run, all threads together, before it is called a timeout.
	StepLimit int64
	Seed      int64
}

// Run executes the program from main under the schedule of seed 0. A run that goes on for more than stepLimit
// instructions is stopped.
func Run(p *Program, stepLimit int64) *Result {
	return RunWith(p, Options{StepLimit: stepLimit})
}

// RunWith executes the program from main. main is the first thread; a behaviour it schedules is another, which runs once
// it has every cown it names, interleaved with the others where the seed says. The program ends when main has returned and
// every behaviour has run, with main's exit code, unless a trap or a violation ends it sooner.
func RunWith(p *Program, o Options) (result *Result) {
	vm := &vm{prog: p, limit: o.StepLimit, strs: map[string]*object{}, infos: map[*ir.IrFunction]*info{}}
	vm.sched = newSched(o.Seed)
	result = &Result{}
	defer func() {
		result.Stdout, result.Stderr, result.Steps, result.Frees, result.Behaviours = vm.stdout, vm.stderr, vm.steps, vm.frees, vm.scheduled
		switch sig := vm.fatal.(type) {
		case nil:
		case *exitSignal:
			result.ExitCode = sig.code
		case *violationSignal:
			result.Violations = append(result.Violations, sig.v)
		case *timeoutSignal:
			result.TimedOut = true
		case *threadPanic:
			// run is already raising it again.
		default:
			panic(sig)
		}
	}()
	main, ok := p.Functions[p.Main]
	if !ok {
		panic(diag.Internal("interp: main has no IR"))
	}
	vm.run(main)
	if vm.fatal != nil {
		return result
	}
	result.ExitCode = vm.mainExit
	result.Violations = vm.leaks()
	return result
}

// leaks are the values still live when the program has ended, which no owner freed.
func (vm *vm) leaks() []Violation {
	var out []Violation
	count := 0
	for _, o := range vm.all {
		if o.freed || o.immortal {
			continue
		}
		count++
		if count <= maxLeaksReported {
			out = append(out, Violation{Kind: Leak, Message: describeLeak(o)})
		}
	}
	if count > maxLeaksReported {
		out = append(out, Violation{Kind: Leak, Message: "and " + strconv.Itoa(count-maxLeaksReported) + " more values leaked"})
	}
	return out
}

func (vm *vm) infoOf(fn *ir.IrFunction) *info {
	if in, ok := vm.infos[fn]; ok {
		return in
	}
	in := &info{windowAt: make([][][]waitingCall, len(fn.Blocks))}
	type position struct{ block, pc int }
	defs := map[ir.V]position{}
	for bi := range fn.Blocks {
		instrs := fn.BlockInstrs(bi)
		in.windowAt[bi] = make([][]waitingCall, len(instrs))
		for pc, ins := range instrs {
			if dst, ok := destination(ins); ok {
				defs[dst] = position{bi, pc}
			}
		}
	}
	for bi := range fn.Blocks {
		instrs := fn.BlockInstrs(bi)
		for pc := range instrs {
			call := &instrs[pc]
			if call.Op != ir.OpCall || call.B < 2 {
				continue
			}
			args := fn.CallArgs(*call)
			target := fn.Callee(*call)
			for i, arg := range args[:len(args)-1] {
				if argumentConv(target, i).Owns() {
					continue
				}
				if def, ok := defs[arg]; ok && borrowsStorage(fn.BlockInstrs(def.block)[def.pc]) {
					in.windowAt[def.block][def.pc] = append(in.windowAt[def.block][def.pc], waitingCall{call: call, block: bi})
				}
			}
		}
	}
	vm.infos[fn] = in
	return in
}

// borrowsStorage is whether an instruction gives a pointer or an address into storage that already exists, which a
// change would leave dangling, and not a value that is new and only being built.
func borrowsStorage(i ir.Instr) bool {
	switch i.Op {
	case ir.OpLoadSlot, ir.OpLoadParam, ir.OpLoadField, ir.OpLoadAt, ir.OpSlotAddr, ir.OpFieldAddr:
		return true
	case ir.OpCallRuntime:
		return strings.HasSuffix(i.Symbol().String(), ".elem")
	}
	return false
}

// argumentConv is how a callee takes its i-th argument, the receiver of a method counting as the first.

func argumentConv(target *ast.FuncDecl, i int) ast.Convention {
	if target.IsMethod() {
		if i == 0 {
			return target.Pass.SelfConv
		}
		i--
	}
	return target.Pass.Specs[i].Conv
}

// destination is the register an instruction writes.
func destination(i ir.Instr) (ir.V, bool) {
	switch i.Op {
	case ir.OpConst, ir.OpStr, ir.OpLoadSlot, ir.OpSlotAddr, ir.OpLoadParam, ir.OpLoadField, ir.OpFieldAddr, ir.OpLoadAt,
		ir.OpLoadGlobal, ir.OpBin, ir.OpCmp, ir.OpNeg, ir.OpNot, ir.OpAlloc, ir.OpCopy, ir.OpMoveSlot, ir.OpMoveAt:
		return i.Dst, true
	case ir.OpCall, ir.OpCallRuntime:
		return i.Dst, i.Dst != ir.NoReg
	case ir.OpStoreSlot, ir.OpStoreField, ir.OpStoreAt, ir.OpGuard, ir.OpDrop, ir.OpDropSlot, ir.OpDropAt, ir.OpWhen:
		return 0, false
	}
	panic(diag.Internal("interp: unhandled opcode %d", int(i.Op)))
}

// call runs a function, and gives its value and its error: 0 when it returned, and for a function that throws, the error's
// case plus 1 when it failed.
func (vm *vm) call(fn *ir.IrFunction, args []value) (value, int64) {
	if vm.depth >= maxCallDepth {
		vm.stderr = append(vm.stderr, "stack overflow\n"...)
		panic(&exitSignal{code: stackOverflowExitCode})
	}
	in := vm.infoOf(fn)
	fr := &frame{fn: fn, thread: vm.cur, params: args, slots: make([]cell, len(fn.Slots)), regs: make([]value, len(fn.Regs)), active: true}
	for i := range fr.slots {
		fr.slots[i].frame = fr
		fr.slots[i].index = i
	}
	caller := vm.top
	vm.top = fr
	vm.depth++
	defer func() {
		fr.active = false
		vm.depth--
		vm.top = caller
	}()
	block := 0
	for {
		fr.block = block
		instrs := fn.BlockInstrs(block)
		failed := false
		for pc := range instrs {
			ins := &instrs[pc]
			fr.pc = pc
			vm.tick()
			if vm.exec(fr, ins) {
				block, failed = int(ins.C), true
				vm.skipWindows(fr, block)
				break
			}
			vm.runEndings()
			for _, w := range in.windowAt[block][pc] {
				vm.openWindow(fr, ins, w, block)
			}
		}
		if failed {
			continue
		}
		fr.pc = -1
		vm.tick()
		switch t := fn.Blocks[block].Term; t.Kind {
		case ir.TermJump:
			block = int(t.Then)
		case ir.TermBranch:
			if fr.regs[t.Value].n != 0 {
				block = int(t.Then)
			} else {
				block = int(t.Else)
			}
		case ir.TermSwitch:
			block = int(t.Then)
			for _, c := range fn.Cases[t.First : t.First+t.Count] {
				if fr.regs[t.Value].n == c.Value {
					block = int(c.Target)
					break
				}
			}
		case ir.TermRet:
			vm.endWindows(fr)
			return vm.ret(fr, t), 0
		case ir.TermThrow:
			vm.endWindows(fr)
			vm.requireSlotsDropped(fr)
			return value{}, fr.regs[t.Value].n
		case ir.TermTrap:
			vm.stderr = append(vm.stderr, "trap\n"...)
			panic(&exitSignal{code: illegalInstructionExitCode})
		default:
			panic(diag.Internal("interp: block b%d of %s has no terminator", block, fn.Symbol()))
		}
	}
}

func (vm *vm) tick() {
	vm.steps++
	if vm.steps > vm.limit {
		panic(&timeoutSignal{})
	}
	if vm.concurrent {
		vm.cur.burst--
		if vm.cur.burst <= 0 {
			vm.yield()
		}
	}
}

// ret hands the result to the caller. It must be a value that nothing owns, and no slot may still own anything: an
// owner that dies without dropping what it holds leaks it.
func (vm *vm) ret(fr *frame, t ir.Term) value {
	v := fr.regs[t.Value]
	if fr.fn.Regs[t.Value] == ir.KindManaged {
		if v.ref == nil {
			vm.violate(MovedUse, "the function returns a value that was moved out or never assigned")
		}
		vm.requireLive(v.ref, "returned")
		if !v.ref.immortal && v.ref.owner != nil {
			vm.violate(Ownership, "the function returns %s, which %s still owns: a return moves its value", v.ref, v.ref.owner)
		}
	}
	vm.requireSlotsDropped(fr)
	return v
}

// requireSlotsDropped is a violation for each slot that still owns a value when its function returns or fails.
func (vm *vm) requireSlotsDropped(fr *frame) {
	for i := range fr.slots {
		c := &fr.slots[i]
		if o := c.v.ref; o != nil && !o.immortal && !o.freed && o.heldBy(c) {
			vm.violate(Leak, "slot s%d still owns %s when the function returns: no drop frees it", i, o)
		}
	}
}

func (vm *vm) openWindow(fr *frame, ins *ir.Instr, waiting waitingCall, def int) {
	dst, _ := destination(*ins)
	v := fr.regs[dst]
	var r region
	switch {
	case v.adr != nil:
		r = region{cell: v.adr.cell}
	case v.ref != nil && !v.ref.immortal && !v.ref.shared():
		r = region{obj: v.ref}
	default:
		return
	}
	vm.windows = append(vm.windows, window{call: waiting.call, block: waiting.block, def: def, frame: fr, region: r, at: fr.site(), taken: fr.fn.InstrString(*ins)})
}

// skipWindows closes the windows of the calls that a failure, which goes to the block target, skips. A call that a path
// from there reaches without computing its argument again, such as the one an 'otherwise' value is an argument of, still
// runs and keeps its window; one that runs again only after its argument is computed again gets a new window then.
func (vm *vm) skipWindows(fr *frame, target int) {
	vm.windows = slices.DeleteFunc(vm.windows, func(w window) bool {
		return w.frame == fr && !pathEnters(fr.fn, target, w.block, w.def)
	})
}

// endWindows closes the windows of a frame that ends: a return or a failure out of it skips the calls still waiting.
func (vm *vm) endWindows(fr *frame) {
	vm.windows = slices.DeleteFunc(vm.windows, func(w window) bool { return w.frame == fr })
}

// pathEnters is whether a path from the block from enters the block to without entering the block avoid first.
func pathEnters(fn *ir.IrFunction, from, to, avoid int) bool {
	if from == to {
		return true
	}
	reached := make([]bool, len(fn.Blocks))
	work := []int{from}
	reached[from] = true
	visit := func(b int) {
		if !reached[b] && b != avoid {
			reached[b] = true
			work = append(work, b)
		}
	}
	for len(work) > 0 {
		b := work[len(work)-1]
		work = work[:len(work)-1]
		for _, in := range fn.BlockInstrs(b) {
			if fail, ok := in.Fail(); ok {
				visit(int(fail))
			}
		}
		switch t := fn.Blocks[b].Term; t.Kind {
		case ir.TermJump:
			visit(int(t.Then))
		case ir.TermBranch:
			visit(int(t.Then))
			visit(int(t.Else))
		case ir.TermSwitch:
			for _, c := range fn.Cases[t.First : t.First+t.Count] {
				visit(int(c.Target))
			}
			visit(int(t.Then))
		case ir.TermRet, ir.TermTrap, ir.TermThrow:
		default:
			panic(diag.Internal("interp: block b%d of %s has no terminator", b, fn.Symbol()))
		}
	}
	return reached[to]
}

func (vm *vm) closeWindows(fr *frame, call *ir.Instr) {
	n := len(vm.windows)
	for n > 0 && vm.windows[n-1].call == call && vm.windows[n-1].frame == fr {
		n--
	}
	vm.windows = vm.windows[:n]
}

// changed is called wherever a place is written or a structure changes: no borrow that a call is waiting to use may
// reach it, because the call would then use what moved.
func (vm *vm) changed(r region) {
	if vm.tracking {
		vm.requireMutable(r)
	}
	for i := range vm.windows {
		w := &vm.windows[i]
		if overlaps(w.region, r) {
			vm.violate(Exclusivity, "%s is borrowed by an argument (`%s` at %s) that a call is still waiting to use, and this changes %s",
				w.region, w.taken, w.at, r)
		}
	}
}

func (vm *vm) requireLive(o *object, what string) {
	if o.freed {
		vm.violate(UseAfterFree, "%s is %s after it was freed at %s", o, what, o.freedAt)
	}
}

// live is the object a managed register points to, which must exist and must not have been freed.
func (vm *vm) live(v value, what string) *object {
	if v.ref == nil {
		vm.violate(MovedUse, "%s uses a value that was moved out or never assigned", what)
	}
	vm.requireLive(v.ref, what)
	if vm.tracking {
		vm.requireReachable(v.ref, what)
	}
	return v.ref
}

func (vm *vm) newObject(kind objectKind) *object {
	vm.nextID++
	o := &object{id: vm.nextID, kind: kind, allocated: vm.top.site()}
	if kind == kindFrozen || kind == kindCown {
		o.count = 1
		o.holders = map[*cell]bool{}
	}
	vm.all = append(vm.all, o)
	return o
}

func (vm *vm) newBox(words int) *object {
	o := vm.newObject(kindBox)
	o.fields = make([]cell, words)
	for i := range o.fields {
		o.fields[i].holder = o
		o.fields[i].index = i
	}
	return o
}

func (vm *vm) newString(text string) *object {
	o := vm.newObject(kindString)
	o.text = text
	return o
}

// literal is a string in the executable: it is never freed, and no one owns it.
func (vm *vm) literal(text string) *object {
	if o, ok := vm.strs[text]; ok {
		return o
	}
	o := vm.newString(text)
	o.immortal = true
	vm.strs[text] = o
	return o
}

// adopt stores a value into a place that owns what it holds. The value must be in flight, owned by no one: a value
// that has an owner is moved first, and one that is stored twice would be freed twice. What the place held must be
// gone already, or it is leaked.
func (vm *vm) adopt(c *cell, v value, what string) {
	if old := c.v.ref; old != nil && !old.immortal && !old.freed && old.heldBy(c) {
		vm.violate(Leak, "%s overwrites %s, which %s still owns: the old value is lost without being dropped", what, old, c)
	}
	o := v.ref
	if o == nil {
		vm.violate(MovedUse, "%s stores a value that was moved out or never assigned", what)
	}
	vm.requireLive(o, "stored")
	switch {
	case o.immortal:
	case o.shared():
		o.holders[c] = true
	default:
		if o.owner != nil {
			vm.violate(Ownership, "%s stores %s into %s, but %s still owns it: a value has one owner, so it must be moved first", what, o, c, o.owner)
		}
		o.owner = c
	}
	c.v = v
}

// store puts a register's value in a place, as an owner when the register is managed.
func (vm *vm) store(fr *frame, c *cell, src ir.V, what string) {
	if fr.fn.Regs[src] == ir.KindManaged {
		vm.adopt(c, fr.regs[src], what)
		return
	}
	c.v = fr.regs[src]
}

// release takes the value out of a place and leaves it empty, as its owner gives it away or frees it.
func (vm *vm) release(c *cell, what string) *object {
	o := c.v.ref
	if o == nil {
		return nil
	}
	c.v = value{}
	if o.immortal {
		return o
	}
	if o.freed {
		vm.violate(UseAfterFree, "%s: %s was freed at %s but %s still holds it", what, o, o.freedAt, c)
	}
	if !o.heldBy(c) {
		vm.violate(Ownership, "%s: %s holds %s, but it is not that place's to give up: %s owns it", what, c, o, ownerText(o))
	}
	if o.shared() {
		delete(o.holders, c)
	} else {
		o.owner = nil
	}
	return o
}

func ownerText(o *object) string {
	if o.owner == nil {
		return "no one"
	}
	return o.owner.String()
}

// drop frees a value and everything it owns, or lowers the count of a shared value by the one owner that is dropping it.
// The value must be in flight: its owner has given it up.
func (vm *vm) drop(o *object) {
	if o.immortal {
		return
	}
	if o.shared() {
		vm.unref(o)
		return
	}
	if o.freed {
		vm.violate(DoubleFree, "%s was already freed at %s", o, o.freedAt)
	}
	if o.owner != nil {
		vm.violate(Ownership, "%s is dropped while %s still owns it", o, o.owner)
	}
	vm.changed(region{obj: o})
	vm.free(o)
}

// unref is one owner of a frozen value or a cown letting go: the value is freed when the last one does.
func (vm *vm) unref(o *object) {
	if o.freed {
		vm.violate(DoubleFree, "%s was already freed at %s", o, o.freedAt)
	}
	o.count--
	switch {
	case o.count < 0:
		vm.violate(Count, "the count of %s fell below 0", o)
	case o.count == 0:
		vm.free(o)
	}
}

func (vm *vm) free(o *object) {
	if o.freed {
		vm.violate(DoubleFree, "%s was already freed at %s while freeing what contains it", o, o.freedAt)
	}
	if o.shared() && (len(o.holders) > 0 || len(o.queue) > 0 || o.running != nil) {
		vm.violate(Count, "%s is freed while it is still referenced: %d place(s) hold it and %d behaviour(s) wait for it", o, len(o.holders), len(o.queue))
	}
	o.freed = true
	o.freedAt = vm.top.site()
	o.owner = nil
	vm.frees = append(vm.frees, o.freeLabel())
	switch o.kind {
	case kindString:
	case kindBox:
		for i := range o.fields {
			vm.freeCell(&o.fields[i])
		}
	case kindFrozen:
		vm.freeCell(&o.fields[0])
	case kindCown:
		if o.ending == nil {
			vm.freeCell(&o.fields[0])
			break
		}
		// The value leaves the cown and is in flight until its destructor takes it.
		if v := o.fields[0].v; v.ref != nil {
			vm.release(&o.fields[0], "freeing a cown")
			vm.endings = append(vm.endings, ending{destructor: o.ending, value: v})
		}
	case kindArray:
		for _, c := range o.elems {
			vm.freeCell(c)
		}
	case kindMap:
		for _, e := range o.mapped.entries {
			vm.freeCell(&e.key)
			vm.freeCell(&e.val)
		}
	case kindList:
		for _, slot := range o.table.order {
			vm.freeCell(o.table.cells[slot])
		}
	case kindPool:
		for slot, live := range o.table.live {
			if live {
				vm.freeCell(o.table.cells[slot])
			}
		}
	default:
		panic(diag.Internal("interp: unknown object kind %d", int(o.kind)))
	}
}

// runEndings calls the destructors of the cowns that the last instruction freed, in the order they were freed. A
// destructor that frees more cowns runs their destructors in its own instructions, so the list is taken before any runs.
func (vm *vm) runEndings() {
	if len(vm.endings) == 0 {
		return
	}
	due := vm.endings
	vm.endings = nil
	for _, e := range due {
		vm.call(e.destructor, []value{e.value})
	}
}

// freeCell frees what a place owns, as part of freeing the object the place belongs to.
func (vm *vm) freeCell(c *cell) {
	if o := vm.release(c, "freeing its owner"); o != nil && !o.immortal {
		if o.shared() {
			vm.unref(o)
		} else {
			vm.free(o)
		}
	}
}
