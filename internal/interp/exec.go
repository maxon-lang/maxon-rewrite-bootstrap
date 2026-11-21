package interp

import (
	"math"
	"math/bits"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"minimaxon/internal/ir"
	"strconv"
)

const (
	wordBits  = 64
	signShift = 63
)

// trap is a panic of the program: the compiled code writes the message to standard error and exits with 1.
func (vm *vm) trap(message string) {
	vm.stderr = append(vm.stderr, "panic: "...)
	vm.stderr = append(vm.stderr, message...)
	vm.stderr = append(vm.stderr, '\n')
	panic(&exitSignal{code: panicExitCode})
}

// exec runs an instruction, and gives whether its check failed, which sends the code to the instruction's failure block.
func (vm *vm) exec(fr *frame, x *ir.Instr) (failed bool) {
	return ir.VisitInstrState[*frame, bool](vm, x, fr)
}

// The Visit methods of vm are its ir.InstrStateVisitor and ir.TermStateVisitor: each runs one operation in a frame.

func (vm *vm) VisitConst(x *ir.ConstInstr, fr *frame) bool {
	fr.regs[x.Dst()] = value{n: x.Value()}
	return false
}

func (vm *vm) VisitStr(x *ir.StrInstr, fr *frame) bool {
	fr.regs[x.Dst()] = value{ref: vm.literal(x.Text(fr.fn))}
	return false
}

func (vm *vm) VisitLoadSlot(x *ir.LoadSlotInstr, fr *frame) bool {
	c := &fr.slots[x.Slot()]
	if fr.fn.Slots[x.Slot()] == ir.KindManaged {
		vm.live(c.v, "a read of the slot")
	}
	fr.regs[x.Dst()] = c.v
	return false
}

func (vm *vm) VisitStoreSlot(x *ir.StoreSlotInstr, fr *frame) bool {
	c := &fr.slots[x.Slot()]
	src := x.Src()
	switch {
	case fr.fn.Regs[src] != ir.KindManaged:
		c.v = fr.regs[src]
	case x.Borrow():
		vm.live(fr.regs[src], "a binding")
		c.v = fr.regs[src]
	default:
		vm.changed(region{cell: c})
		vm.adopt(c, fr.regs[src], "the store")
	}
	return false
}

func (vm *vm) VisitSlotAddr(x *ir.SlotAddrInstr, fr *frame) bool {
	fr.regs[x.Dst()] = value{adr: &address{cell: &fr.slots[x.Slot()]}}
	return false
}

func (vm *vm) VisitLoadParam(x *ir.LoadParamInstr, fr *frame) bool {
	fr.regs[x.Dst()] = fr.params[x.Param()]
	return false
}

func (vm *vm) VisitLoadField(x *ir.LoadFieldInstr, fr *frame) bool {
	fr.regs[x.Dst()] = vm.fieldOf(fr, x.Obj(), x.Offset(), "a field read").v
	return false
}

func (vm *vm) VisitStoreField(x *ir.StoreFieldInstr, fr *frame) bool {
	c := vm.fieldOf(fr, x.Obj(), x.Offset(), "a field write")
	vm.changed(region{cell: c})
	vm.store(fr, c, x.Src(), "the field store")
	return false
}

func (vm *vm) VisitFieldAddr(x *ir.FieldAddrInstr, fr *frame) bool {
	fr.regs[x.Dst()] = value{adr: &address{cell: vm.fieldOf(fr, x.Obj(), x.Offset(), "taking the address of a field")}}
	return false
}

func (vm *vm) VisitLoadAt(x *ir.LoadAtInstr, fr *frame) bool {
	fr.regs[x.Dst()] = vm.target(fr, x.Addr(), "a read through an address").v
	return false
}

func (vm *vm) VisitStoreAt(x *ir.StoreAtInstr, fr *frame) bool {
	c := vm.target(fr, x.Addr(), "a write through an address")
	vm.changed(region{cell: c})
	vm.store(fr, c, x.Src(), "the store")
	return false
}

func (vm *vm) VisitLoadGlobal(x *ir.LoadGlobalInstr, fr *frame) bool {
	fr.regs[x.Dst()] = value{n: vm.global(x.Symbol())}
	return false
}

func (vm *vm) VisitBin(x *ir.BinInstr, fr *frame) bool {
	n, fits := vm.binary(x, fr.regs[x.Left()].n, fr.regs[x.Right()].n)
	if !fits {
		return true
	}
	fr.regs[x.Dst()] = value{n: n}
	return false
}

func (vm *vm) VisitCmp(x *ir.CmpInstr, fr *frame) bool {
	fr.regs[x.Dst()] = value{n: compare(x, fr.regs[x.Left()].n, fr.regs[x.Right()].n)}
	return false
}

func (vm *vm) VisitNeg(x *ir.NegInstr, fr *frame) bool {
	a := fr.regs[x.Src()].n
	if _, checked := x.Fail(); checked && a == math.MinInt64 {
		return true
	}
	fr.regs[x.Dst()] = value{n: -a}
	return false
}

func (vm *vm) VisitNot(x *ir.NotInstr, fr *frame) bool {
	fr.regs[x.Dst()] = value{n: ^fr.regs[x.Src()].n}
	return false
}

func (vm *vm) VisitGuard(x *ir.GuardInstr, fr *frame) bool {
	return !inRange(x, fr.regs[x.Src()].n)
}

func (vm *vm) VisitAlloc(x *ir.AllocInstr, fr *frame) bool {
	fr.regs[x.Dst()] = value{ref: vm.newBox(max(x.Bytes()/wordBytes, 1))}
	return false
}

func (vm *vm) VisitCall(x *ir.CallInstr, fr *frame) bool {
	vm.execCall(fr, x)
	return false
}

func (vm *vm) VisitCallRuntime(x *ir.CallRuntimeInstr, fr *frame) bool {
	vm.runtimeEntry(fr, x)
	return false
}

func (vm *vm) VisitWhen(x *ir.WhenInstr, fr *frame) bool {
	vm.schedule(fr, x)
	return false
}

func (vm *vm) VisitDrop(x *ir.DropInstr, fr *frame) bool {
	// A register is written before it is dropped on every path, so one that holds nothing is a drop of a value that was
	// never made: a native frame does not zero its registers, and would free whatever they held.
	if o := fr.regs[x.Src()].ref; o != nil {
		vm.drop(o)
	} else {
		vm.violate(MovedUse, "a drop of a register that holds no value: it was never assigned on this path")
	}
	return false
}

func (vm *vm) VisitCopy(x *ir.CopyInstr, fr *frame) bool {
	fr.regs[x.Dst()] = value{ref: vm.copyObject(vm.live(fr.regs[x.Src()], "a copy"))}
	return false
}

func (vm *vm) VisitDropSlot(x *ir.DropSlotInstr, fr *frame) bool {
	vm.dropCell(&fr.slots[x.Slot()])
	return false
}

func (vm *vm) VisitDropAt(x *ir.DropAtInstr, fr *frame) bool {
	vm.dropCell(vm.target(fr, x.Addr(), "a drop through an address"))
	return false
}

func (vm *vm) VisitMoveSlot(x *ir.MoveSlotInstr, fr *frame) bool {
	fr.regs[x.Dst()] = vm.moveOut(&fr.slots[x.Slot()], "a move out of the slot")
	return false
}

func (vm *vm) VisitMoveAt(x *ir.MoveAtInstr, fr *frame) bool {
	fr.regs[x.Dst()] = vm.moveOut(vm.target(fr, x.Addr(), "a move through an address"), "a move out of the place")
	return false
}

func (vm *vm) global(symbol ir.Rt) int64 {
	switch symbol {
	case ir.RtStdout:
		return stdoutHandle
	case ir.RtStderr:
		return stderrHandle
	case ir.RtHeap:
		return heapHandle
	}
	panic(diag.Internal("interp: unknown global %s", symbol))
}

// dropCell drops what a place owns, if it owns anything, and leaves it empty.
func (vm *vm) dropCell(c *cell) {
	if c.v.ref == nil {
		return
	}
	vm.changed(region{cell: c})
	if o := vm.release(c, "a drop"); o != nil {
		vm.drop(o)
	}
}

// moveOut reads the value of a place that owns one and leaves the place empty: the value is in flight until whatever
// receives it takes it.
func (vm *vm) moveOut(c *cell, what string) value {
	if c.v.ref == nil {
		vm.violate(MovedUse, "%s, which holds no value: it was moved out already or never assigned", what)
	}
	vm.changed(region{cell: c})
	v := c.v
	vm.release(c, what)
	return v
}

// fieldOf is the word of a box that an offset names.
func (vm *vm) fieldOf(fr *frame, box ir.V, offset int, what string) *cell {
	o := vm.live(fr.regs[box], what)
	if o.kind != kindBox || offset%wordBytes != 0 || offset/wordBytes >= len(o.fields) {
		panic(diag.Internal("interp: %s reaches outside %s at offset %d", what, o, offset))
	}
	return &o.fields[offset/wordBytes]
}

// target is the cell an address names, which must still be there: the structure that holds it must be alive and
// unchanged since the address was taken, and the frame of a slot must not have returned.
func (vm *vm) target(fr *frame, addr ir.V, what string) *cell {
	a := fr.regs[addr].adr
	if a == nil {
		vm.violate(MovedUse, "%s uses an address that was never taken", what)
	}
	vm.requireValid(a, what)
	if vm.tracking {
		vm.requireReachableCell(a.cell, what)
	}
	return a.cell
}

func (vm *vm) requireValid(a *address, what string) {
	c := a.cell
	if c.holder != nil {
		vm.requireLive(c.holder, what)
		if c.holder.version != a.version {
			vm.violate(StaleBorrow, "%s uses an address into %s that was taken before the structure changed", what, c.holder)
		}
	}
	if c.frame != nil && !c.frame.active {
		vm.violate(StaleBorrow, "%s uses the address of a slot of %s, which has returned", what, c.frame.fn.Symbol())
	}
}

// addressOf is the address of an element, valid until its structure changes.
func addressOf(c *cell) value {
	return value{adr: &address{cell: c, version: c.holder.version}}
}

// function is the IR of a function that the program calls.
func (vm *vm) function(f *ast.FuncDecl) *ir.IrFunction {
	fn, ok := vm.prog.Functions[f]
	if !ok {
		panic(diag.Internal("interp: %s has no IR", f.Symbol()))
	}
	return fn
}

func (vm *vm) execCall(fr *frame, c *ir.CallInstr) {
	target := c.Callee(fr.fn)
	callee := vm.function(target)
	regs := c.Args(fr.fn)
	args := make([]value, len(regs))
	for i, r := range regs {
		args[i] = fr.regs[r]
		vm.checkArgument(fr, target, i, r)
	}
	vm.requireDisjoint(target, args)
	vm.closeWindows(fr, (*ir.Instr)(c))
	result, err := vm.call(callee, args)
	if dst, gives := c.Dst(); gives {
		fr.regs[dst] = result
	}
	if errReg, throws := c.ErrReg(); throws {
		fr.regs[errReg] = value{n: err}
	}
}

// checkArgument refuses an argument that cannot be used: a value that is gone, or a borrow that is stale.
func (vm *vm) checkArgument(fr *frame, target *ast.FuncDecl, i int, r ir.V) {
	v := fr.regs[r]
	what := "argument " + strconv.Itoa(i) + " of the call to " + target.Symbol()
	switch {
	case v.adr != nil:
		vm.requireValid(v.adr, what)
	case fr.fn.Regs[r] == ir.KindManaged:
		vm.live(v, what)
	}
}

// requireDisjoint is the law of exclusivity at one call: an argument that the callee may change, by address or by
// owning it, must not reach any place that another argument reaches.
func (vm *vm) requireDisjoint(target *ast.FuncDecl, args []value) {
	type access struct {
		region  region
		mutable bool
		valid   bool
	}
	accesses := make([]access, len(args))
	for i, v := range args {
		conv := argumentConv(target, i)
		switch {
		case v.adr != nil:
			accesses[i] = access{region{cell: v.adr.cell}, true, true}
		case v.ref != nil && !v.ref.immortal && !v.ref.shared():
			accesses[i] = access{region{obj: v.ref}, conv != ast.ConvLet, true}
		}
	}
	for j := 1; j < len(accesses); j++ {
		for i := range j {
			a, b := accesses[i], accesses[j]
			if a.valid && b.valid && (a.mutable || b.mutable) && overlaps(a.region, b.region) {
				vm.violate(Exclusivity, "arguments %d and %d of the call to %s reach one place, %s and %s, and one of them is passed so that the callee may change it",
					i, j, target.Symbol(), a.region, b.region)
			}
		}
	}
}

func compare(c *ir.CmpInstr, a, b int64) int64 {
	var less, equal bool
	equal = a == b
	if c.Unsigned() {
		less = uint64(a) < uint64(b)
	} else {
		less = a < b
	}
	var result bool
	switch c.Op() {
	case ir.CmpEq:
		result = equal
	case ir.CmpNe:
		result = !equal
	case ir.CmpLt:
		result = less
	case ir.CmpLe:
		result = less || equal
	case ir.CmpGt:
		result = !less && !equal
	case ir.CmpGe:
		result = !less
	default:
		panic(diag.Internal("interp: comparison %d", int(c.Op())))
	}
	if result {
		return 1
	}
	return 0
}

// binary computes an operation in a word, and gives whether the result fits it, which only a checked operation asks.
// The range analysis proved that an operation without a check fits, and a guard before it rules out a divisor of 0 and a
// negative shift count, so finding one is a compiler bug.
func (vm *vm) binary(b *ir.BinInstr, x, y int64) (int64, bool) {
	_, checked := b.Fail()
	unsigned := b.Unsigned()
	switch b.Op() {
	case ir.BinAdd:
		r := x + y
		return r, !checked || !(unsigned && uint64(r) < uint64(x) || !unsigned && (x^r)&(y^r) < 0)
	case ir.BinSub:
		r := x - y
		return r, !checked || !(unsigned && uint64(x) < uint64(y) || !unsigned && (x^y)&(x^r) < 0)
	case ir.BinMul:
		r := x * y
		if !checked {
			return r, true
		}
		if unsigned {
			hi, _ := bits.Mul64(uint64(x), uint64(y))
			return r, hi == 0
		}
		return r, x == 0 || (r/x == y && !(x == -1 && y == math.MinInt64))
	case ir.BinDiv, ir.BinMod:
		if y == 0 {
			panic(diag.Internal("interp: a division by 0 that no guard checked"))
		}
		if unsigned {
			if b.Op() == ir.BinDiv {
				return int64(uint64(x) / uint64(y)), true
			}
			return int64(uint64(x) % uint64(y)), true
		}
		if b.Op() == ir.BinMod {
			return x % y, true
		}
		if x == math.MinInt64 && y == -1 {
			if !checked {
				panic(diag.Internal("interp: i64.min / -1 in a division that is not checked"))
			}
			return 0, false
		}
		return x / y, true
	case ir.BinAnd:
		return x & y, true
	case ir.BinOr:
		return x | y, true
	case ir.BinXor:
		return x ^ y, true
	case ir.BinShr:
		return shift(false, unsigned, x, y), true
	case ir.BinShl:
		r := shift(true, unsigned, x, y)
		if !checked {
			return r, true
		}
		if y >= wordBits {
			return r, x == 0
		}
		return r, shift(false, unsigned, r, y) == x
	}
	panic(diag.Internal("interp: operator %d", int(b.Op())))
}

func shift(left, unsigned bool, x, count int64) int64 {
	if count < 0 {
		panic(diag.Internal("interp: a negative shift count that no guard checked"))
	}
	switch {
	case left && count >= wordBits:
		return 0
	case left:
		return x << uint(count)
	case count >= wordBits && unsigned:
		return 0
	case count >= wordBits:
		return x >> signShift
	case unsigned:
		return int64(uint64(x) >> uint(count))
	}
	return x >> uint(count)
}

// inRange is whether a value passes a guard.
func inRange(g *ir.GuardInstr, v int64) bool {
	switch g.Kind() {
	case ir.GuardNonNegative:
		return v >= 0
	case ir.GuardSignedAtLeast:
		return v >= g.Bound()
	case ir.GuardSignedAtMost:
		return v <= g.Bound()
	case ir.GuardUnsignedAtLeast:
		return uint64(v) >= uint64(g.Bound())
	case ir.GuardUnsignedAtMost:
		return uint64(v) <= uint64(g.Bound())
	case ir.GuardNonZero:
		return v != 0
	}
	panic(diag.Internal("interp: guard %d", int(g.Kind())))
}
