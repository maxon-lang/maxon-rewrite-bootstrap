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
	integerOverflowMessage = "integer overflow"
	rangeMessage           = "integer value out of range"
	divisionMessage        = "division by zero"
	shiftMessage           = "negative shift count"
	wordBits               = 64
	signShift              = 63
)

// trap is a panic of the program: the compiled code writes the message to standard error and exits with 1.
func (vm *vm) trap(message string) {
	vm.stderr = append(vm.stderr, "panic: "...)
	vm.stderr = append(vm.stderr, message...)
	vm.stderr = append(vm.stderr, '\n')
	panic(&exitSignal{code: panicExitCode})
}

func (vm *vm) exec(fr *frame, i ir.Instr) {
	switch x := i.(type) {
	case *ir.ConstInstr:
		fr.regs[x.Dst] = value{n: x.Value}
	case *ir.StrInstr:
		fr.regs[x.Dst] = value{ref: vm.literal(x.Value)}
	case *ir.LoadSlot:
		c := &fr.slots[x.Slot]
		if fr.fn.Slots[x.Slot] == ir.KindManaged {
			vm.live(c.v, "a read of the slot")
		}
		fr.regs[x.Dst] = c.v
	case *ir.StoreSlot:
		c := &fr.slots[x.Slot]
		switch {
		case fr.fn.Regs[x.Src] != ir.KindManaged:
			c.v = fr.regs[x.Src]
		case x.Borrow:
			vm.live(fr.regs[x.Src], "a binding")
			c.v = fr.regs[x.Src]
		default:
			vm.changed(region{cell: c})
			vm.adopt(c, fr.regs[x.Src], "the store")
		}
	case *ir.SlotAddr:
		fr.regs[x.Dst] = value{adr: &address{cell: &fr.slots[x.Slot]}}
	case *ir.LoadParam:
		fr.regs[x.Dst] = fr.params[x.Param]
	case *ir.LoadField:
		fr.regs[x.Dst] = vm.fieldOf(fr, x.Obj, x.Offset, "a field read").v
	case *ir.StoreField:
		c := vm.fieldOf(fr, x.Obj, x.Offset, "a field write")
		vm.changed(region{cell: c})
		vm.store(fr, c, x.Src, "the field store")
	case *ir.FieldAddr:
		fr.regs[x.Dst] = value{adr: &address{cell: vm.fieldOf(fr, x.Obj, x.Offset, "taking the address of a field")}}
	case *ir.LoadAt:
		fr.regs[x.Dst] = vm.target(fr, x.Addr, "a read through an address").v
	case *ir.StoreAt:
		c := vm.target(fr, x.Addr, "a write through an address")
		vm.changed(region{cell: c})
		vm.store(fr, c, x.Src, "the store")
	case *ir.LoadGlobal:
		fr.regs[x.Dst] = value{n: vm.global(x.Symbol)}
	case *ir.BinInstr:
		fr.regs[x.Dst] = value{n: vm.binary(x, fr.regs[x.A].n, fr.regs[x.B].n)}
	case *ir.CmpInstr:
		fr.regs[x.Dst] = value{n: compare(x, fr.regs[x.A].n, fr.regs[x.B].n)}
	case *ir.NegInstr:
		a := fr.regs[x.Src].n
		if a == math.MinInt64 {
			vm.trap(integerOverflowMessage)
		}
		fr.regs[x.Dst] = value{n: -a}
	case *ir.NotInstr:
		fr.regs[x.Dst] = value{n: ^fr.regs[x.Src].n}
	case *ir.GuardInstr:
		vm.guard(x, fr.regs[x.Src].n)
	case *ir.AllocInstr:
		fr.regs[x.Dst] = value{ref: vm.newBox(max(x.Bytes/wordBytes, 1))}
	case *ir.CallInstr:
		vm.execCall(fr, x)
	case *ir.CallRuntimeInstr:
		vm.runtimeEntry(fr, x)
	case *ir.WhenInstr:
		vm.schedule(fr, x)
	case *ir.DropInstr:
		// A register is written before it is dropped on every path, so one that holds nothing is a drop of a value that was
		// never made: a native frame does not zero its registers, and would free whatever they held.
		if o := fr.regs[x.Src].ref; o != nil {
			vm.drop(o)
		} else {
			vm.violate(MovedUse, "a drop of a register that holds no value: it was never assigned on this path")
		}
	case *ir.CopyInstr:
		fr.regs[x.Dst] = value{ref: vm.copyObject(vm.live(fr.regs[x.Src], "a copy"))}
	case *ir.DropSlotInstr:
		vm.dropCell(&fr.slots[x.Slot])
	case *ir.DropAtInstr:
		vm.dropCell(vm.target(fr, x.Addr, "a drop through an address"))
	case *ir.MoveSlotInstr:
		fr.regs[x.Dst] = vm.moveOut(&fr.slots[x.Slot], "a move out of the slot")
	case *ir.MoveAtInstr:
		fr.regs[x.Dst] = vm.moveOut(vm.target(fr, x.Addr, "a move through an address"), "a move out of the place")
	default:
		panic(diag.Internal("interp: unhandled instruction %T", i))
	}
}

func (vm *vm) global(symbol string) int64 {
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
	callee := vm.function(c.Target)
	args := make([]value, len(c.Args))
	for i, r := range c.Args {
		args[i] = fr.regs[r]
		vm.checkArgument(fr, c, i, r)
	}
	vm.requireDisjoint(c, args)
	vm.closeWindows(fr, c)
	result := vm.call(callee, args)
	if c.Dst != nil {
		fr.regs[*c.Dst] = result
	}
}

// checkArgument refuses an argument that cannot be used: a value that is gone, or a borrow that is stale.
func (vm *vm) checkArgument(fr *frame, c *ir.CallInstr, i int, r ir.V) {
	v := fr.regs[r]
	what := "argument " + strconv.Itoa(i) + " of the call to " + c.Target.Symbol()
	switch {
	case v.adr != nil:
		vm.requireValid(v.adr, what)
	case fr.fn.Regs[r] == ir.KindManaged:
		vm.live(v, what)
	}
}

// requireDisjoint is the law of exclusivity at one call: an argument that the callee may change, by address or by
// owning it, must not reach any place that another argument reaches.
func (vm *vm) requireDisjoint(c *ir.CallInstr, args []value) {
	type access struct {
		region  region
		mutable bool
		valid   bool
	}
	accesses := make([]access, len(args))
	for i, v := range args {
		conv := argumentConv(c.Target, i)
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
					i, j, c.Target.Symbol(), a.region, b.region)
			}
		}
	}
}

func compare(c *ir.CmpInstr, a, b int64) int64 {
	var less, equal bool
	equal = a == b
	if c.Unsigned {
		less = uint64(a) < uint64(b)
	} else {
		less = a < b
	}
	var result bool
	switch c.Op {
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
		panic(diag.Internal("interp: comparison %d", int(c.Op)))
	}
	if result {
		return 1
	}
	return 0
}

func (vm *vm) binary(b *ir.BinInstr, x, y int64) int64 {
	switch b.Op {
	case ir.OpAdd:
		r := x + y
		if b.Checked {
			if b.Unsigned && uint64(r) < uint64(x) || !b.Unsigned && (x^r)&(y^r) < 0 {
				vm.trap(integerOverflowMessage)
			}
		}
		return r
	case ir.OpSub:
		r := x - y
		if b.Checked {
			if b.Unsigned && uint64(x) < uint64(y) || !b.Unsigned && (x^y)&(x^r) < 0 {
				vm.trap(integerOverflowMessage)
			}
		}
		return r
	case ir.OpMul:
		r := x * y
		if b.Checked {
			if b.Unsigned {
				if hi, _ := bits.Mul64(uint64(x), uint64(y)); hi != 0 {
					vm.trap(integerOverflowMessage)
				}
			} else if x != 0 && (r/x != y || x == -1 && y == math.MinInt64) {
				vm.trap(integerOverflowMessage)
			}
		}
		return r
	case ir.OpDiv, ir.OpMod:
		if y == 0 {
			vm.trap(divisionMessage)
		}
		if b.Unsigned {
			if b.Op == ir.OpDiv {
				return int64(uint64(x) / uint64(y))
			}
			return int64(uint64(x) % uint64(y))
		}
		if x == math.MinInt64 && y == -1 {
			vm.stderr = append(vm.stderr, "integer overflow in a division\n"...)
			panic(&exitSignal{code: integerOverflowExitCode})
		}
		if b.Op == ir.OpDiv {
			return x / y
		}
		return x % y
	case ir.OpAnd:
		return x & y
	case ir.OpOr:
		return x | y
	case ir.OpXor:
		return x ^ y
	case ir.OpShl, ir.OpShr:
		return vm.shift(b.Op == ir.OpShl, b.Unsigned, x, y)
	}
	panic(diag.Internal("interp: operator %d", int(b.Op)))
}

func (vm *vm) shift(left, unsigned bool, x, count int64) int64 {
	if count < 0 {
		vm.trap(shiftMessage)
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

func (vm *vm) guard(g *ir.GuardInstr, v int64) {
	var bad bool
	switch g.Kind {
	case ir.GuardNonNegative:
		bad = v < 0
	case ir.GuardSignedAtLeast:
		bad = v < g.Bound
	case ir.GuardSignedAtMost:
		bad = v > g.Bound
	case ir.GuardUnsignedAtLeast:
		bad = uint64(v) < uint64(g.Bound)
	case ir.GuardUnsignedAtMost:
		bad = uint64(v) > uint64(g.Bound)
	default:
		panic(diag.Internal("interp: guard %d", int(g.Kind)))
	}
	if bad {
		vm.trap(rangeMessage)
	}
}
