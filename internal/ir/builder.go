package ir

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"slices"
)

// functionsPerChunk is how many IrFunction records an arena allocates at once, so a function's address stays the same
// while its arena grows.
const functionsPerChunk = 256

// Arena holds the IR of the functions that were finished in it: their instructions, blocks and pools, each function a
// view of a run of them. An arena that is reset and used again allocates only what a module larger than any before it
// needs, which is how a worker lowers every module it is given with one set of buffers. Functions made before a reset
// are invalid after it. An arena belongs to one goroutine.
type Arena struct {
	instrs  []Instr
	blocks  []Block
	slots   []IrKind
	regs    []IrKind
	args    []V
	types   []ast.Ty
	callees []*ast.FuncDecl
	strings []string
	cases   []SwitchCase

	chunks [][]IrFunction
	chunk  int
	used   int
}

// Reset forgets every function made in the arena, keeping its storage.
func (a *Arena) Reset() {
	a.instrs, a.blocks, a.slots, a.regs = a.instrs[:0], a.blocks[:0], a.slots[:0], a.regs[:0]
	a.args, a.types, a.callees, a.strings, a.cases = a.args[:0], a.types[:0], a.callees[:0], a.strings[:0], a.cases[:0]
	a.chunk, a.used = 0, 0
}

func (a *Arena) newFunction() *IrFunction {
	if a.chunk == len(a.chunks) {
		a.chunks = append(a.chunks, make([]IrFunction, functionsPerChunk))
	}
	f := &a.chunks[a.chunk][a.used]
	*f = IrFunction{}
	if a.used++; a.used == functionsPerChunk {
		a.chunk++
		a.used = 0
	}
	return f
}

// extend adds n elements to the end of a pool and gives them, without clearing them: the caller writes every one.
func extend[T any](pool *[]T, n int) []T {
	start := len(*pool)
	*pool = slices.Grow(*pool, n)[:start+n]
	return (*pool)[start:]
}

// keep appends the elements to a pool and gives the view of them, which stays valid when the pool grows.
func keep[T any](pool *[]T, elements []T) []T {
	start := len(*pool)
	*pool = append(*pool, elements...)
	return (*pool)[start:len(*pool):len(*pool)]
}

// Builder makes the IR of one function at a time. Instructions go into the block that is named, in any order of blocks,
// and Finish gives the function with its blocks in the order they were made, one after another in a single slice, minus
// the blocks nothing reaches. A builder is reused for the next function, keeping its buffers, and belongs to one
// goroutine.
type Builder struct {
	// F is the function under construction: its slots and registers are made as it goes, and the rest is filled in by
	// Finish.
	F IrFunction

	instrs     []Instr
	instrBlock []BlockID
	terms      []Term

	// reach and the counts are the scratch of Finish; edges are the blocks that the instructions of each block can fail
	// to, edgeStart where each block's begin.
	reached   []bool
	counts    []int32
	places    []int32
	newID     []BlockID
	work      []BlockID
	edges     []BlockID
	edgeStart []int32
}

// Start begins a function, forgetting the one before it.
func (b *Builder) Start(decl *ast.FuncDecl, name string, paramCount int) {
	f := &b.F
	*f = IrFunction{Decl: decl, Name: name, ParamCount: paramCount,
		Slots: f.Slots[:0], Regs: f.Regs[:0], Args: f.Args[:0], Types: f.Types[:0], Callees: f.Callees[:0],
		Strings: f.Strings[:0], Cases: f.Cases[:0]}
	b.instrs, b.instrBlock, b.terms = b.instrs[:0], b.instrBlock[:0], b.terms[:0]
}

// NewBlock makes a block, which comes after the blocks made before it.
func (b *Builder) NewBlock() BlockID {
	b.terms = append(b.terms, Term{})
	return BlockID(len(b.terms) - 1)
}

// Emit adds an instruction to the end of a block.
func (b *Builder) Emit(block BlockID, i Instr) {
	b.instrs = append(b.instrs, i)
	b.instrBlock = append(b.instrBlock, block)
}

// Terminate ends a block.
func (b *Builder) Terminate(block BlockID, t Term) { b.terms[block] = t }

// Terminated is whether a block has been ended.
func (b *Builder) Terminated(block BlockID) bool { return b.terms[block].kind != termNone }

func (b *Builder) Str(dst V, text string) Instr {
	b.F.Strings = append(b.F.Strings, text)
	return newStr(dst, int32(len(b.F.Strings)-1))
}

func (b *Builder) arguments(args []V) (start V, count V) {
	start = V(len(b.F.Args))
	b.F.Args = append(b.F.Args, args...)
	return start, V(len(args))
}

func (b *Builder) callee(target *ast.FuncDecl) int32 {
	b.F.Callees = append(b.F.Callees, target)
	return int32(len(b.F.Callees) - 1)
}

func (b *Builder) typed(ty ast.Ty) int32 {
	b.F.Types = append(b.F.Types, ty)
	return int32(len(b.F.Types) - 1)
}

// Call calls a function; dst is NoReg for one whose value nothing reads.
func (b *Builder) Call(dst V, target *ast.FuncDecl, args []V) Instr {
	return b.ThrowingCall(dst, target, args, NoReg)
}

// ThrowingCall calls a function that throws, whose error err receives.
func (b *Builder) ThrowingCall(dst V, target *ast.FuncDecl, args []V, err V) Instr {
	callee := b.callee(target)
	start, count := b.arguments(args)
	return newCall(dst, callee, start, count, err)
}

// CallRuntime calls a runtime entry. routine is the function the entry stores to call later, nil for every entry but the
// one that makes a cown.
func (b *Builder) CallRuntime(dst V, symbol Rt, args []V, routine *ast.FuncDecl) Instr {
	start, count := b.arguments(args)
	stored := int32(NoRoutine)
	if routine != nil {
		stored = b.callee(routine)
	}
	return newCallRuntime(dst, symbol, start, count, stored)
}

// Behaviour adds the body of a `when` to the function, which the instruction that schedules it names by the index this
// gives.
func (b *Builder) Behaviour(body *IrFunction) int {
	b.F.Behaviours = append(b.F.Behaviours, body)
	return len(b.F.Behaviours) - 1
}

// When schedules a behaviour that Behaviour added.
func (b *Builder) When(behaviour int, cowns, captures []V) Instr {
	start, count := b.arguments(cowns)
	b.F.Args = append(b.F.Args, captures...)
	return newWhen(behaviour, start, count, int32(len(captures)))
}

// Drop drops the owned value in a register, such as a temporary that nothing took, a removed element, or the old value of
// a place just read. Dropping a value drops what it owns, deeply by type, and a place that holds no value is dropped as a
// no-op, so a drop at the end of a scope needs no knowledge of the path that led there.
func (b *Builder) Drop(src V, ty ast.Ty) Instr {
	return newDrop(src, b.typed(ty))
}

// DropSlot drops what a slot owns, if anything, and leaves it empty.
func (b *Builder) DropSlot(slot int, ty ast.Ty) Instr {
	return newDropSlot(slot, b.typed(ty))
}

// DropAt drops what the address holds, if anything, and leaves it empty: the old value of a field, an element or a
// mutates parameter that is about to be assigned.
func (b *Builder) DropAt(addr V, ty ast.Ty) Instr {
	return newDropAt(addr, b.typed(ty))
}

// Copy is a deep copy of the owned value in src, which is left as it is: a new box for each record, union and array
// inside it, and a new string for each string that is not in the executable, so every part of the copy has an owner of
// its own to free it. A frozen value or a cown reference copies as another owner of the same box. ty is the type of the
// value, with an instance's type arguments applied, which is what tells the copy where the managed words of each box are.
func (b *Builder) Copy(dst, src V, ty ast.Ty) Instr {
	return newCopy(dst, src, b.typed(ty))
}

// Switch is a terminator that goes to the target of the value that matches, or to def.
func (b *Builder) Switch(value V, values []int64, targets []BlockID, def BlockID) Term {
	first := len(b.F.Cases)
	for i, v := range values {
		b.F.Cases = append(b.F.Cases, SwitchCase{Value: v, Target: targets[i]})
	}
	return newSwitch(value, int32(first), int32(len(values)), def)
}

// failEdges lists, for each block, the blocks its instructions can fail to.
func (b *Builder) failEdges() {
	n := len(b.terms)
	b.edgeStart = append(b.edgeStart[:0], make([]int32, n+1)...)
	for i, in := range b.instrs {
		if _, ok := in.Fail(); ok {
			b.edgeStart[b.instrBlock[i]+1]++
		}
	}
	for blk := range n {
		b.edgeStart[blk+1] += b.edgeStart[blk]
	}
	b.edges = append(b.edges[:0], make([]BlockID, b.edgeStart[n])...)
	fill := append(b.counts[:0], b.edgeStart[:n]...)
	for i, in := range b.instrs {
		if fail, ok := in.Fail(); ok {
			blk := b.instrBlock[i]
			b.edges[fill[blk]] = fail
			fill[blk]++
		}
	}
	b.counts = fill[:0]
}

// markReachable finds the blocks that the entry reaches.
func (b *Builder) markReachable() {
	n := len(b.terms)
	if n == 0 {
		panic(diag.Internal("a function has no blocks"))
	}
	b.reached = append(b.reached[:0], make([]bool, n)...)
	b.work = append(b.work[:0], 0)
	b.reached[0] = true
	visit := func(next BlockID) {
		if !b.reached[next] {
			b.reached[next] = true
			b.work = append(b.work, next)
		}
	}
	b.failEdges()
	for len(b.work) > 0 {
		blk := b.work[len(b.work)-1]
		b.work = b.work[:len(b.work)-1]
		b.F.Successors(b.terms[blk], visit)
		for _, fail := range b.edges[b.edgeStart[blk]:b.edgeStart[blk+1]] {
			visit(fail)
		}
	}
}

// Finish ends the function: the blocks that nothing reaches are dropped, the others keep their order and are numbered from
// 0, and the function is copied into the arena (a nil arena is a new one, for a function that is kept). The builder can
// start another function.
func (b *Builder) Finish(a *Arena) *IrFunction {
	if a == nil {
		a = &Arena{}
	}
	b.markReachable()
	n := len(b.terms)
	b.counts = append(b.counts[:0], make([]int32, n)...)
	for _, blk := range b.instrBlock {
		b.counts[blk]++
	}
	b.newID = append(b.newID[:0], make([]BlockID, n)...)
	b.places = append(b.places[:0], make([]int32, n)...)
	blockStart := len(a.blocks)
	instrStart := len(a.instrs)
	kept := 0
	total := 0
	for blk := range n {
		if !b.reached[blk] {
			continue
		}
		b.newID[blk] = BlockID(kept)
		b.places[blk] = int32(total)
		kept++
		total += int(b.counts[blk])
	}
	blocks := extend(&a.blocks, kept)
	instrs := extend(&a.instrs, total)
	for blk := range n {
		if b.reached[blk] {
			id := b.newID[blk]
			blocks[id].First = b.places[blk]
			blocks[id].End = b.places[blk] + b.counts[blk]
			blocks[id].Term = b.terms[blk].renumbered(b.newID)
		}
	}
	for i, in := range b.instrs {
		if blk := b.instrBlock[i]; b.reached[blk] {
			if fail, ok := in.Fail(); ok {
				in.setFail(b.newID[fail])
			}
			instrs[b.places[blk]] = in
			b.places[blk]++
		}
	}

	f := a.newFunction()
	*f = b.F
	f.Slots = keep(&a.slots, b.F.Slots)
	f.Regs = keep(&a.regs, b.F.Regs)
	f.Args = keep(&a.args, b.F.Args)
	f.Types = keep(&a.types, b.F.Types)
	f.Callees = keep(&a.callees, b.F.Callees)
	f.Strings = keep(&a.strings, b.F.Strings)
	f.Cases = keep(&a.cases, b.F.Cases)
	f.Instrs = a.instrs[instrStart:len(a.instrs):len(a.instrs)]
	f.Blocks = a.blocks[blockStart:len(a.blocks):len(a.blocks)]
	b.renumberCases(f)
	return f
}

// renumberCases numbers the targets of the cases that the kept switches use.
func (b *Builder) renumberCases(f *IrFunction) {
	for blk := range f.Blocks {
		t := &f.Blocks[blk].Term
		if t.kind != TermSwitch {
			continue
		}
		cases := (*SwitchTerm)(t).Cases(f)
		for i := range cases {
			cases[i].Target = b.newID[cases[i].Target]
		}
	}
}
