package interp

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"minimaxon/internal/ir"
	"strconv"
)

const (
	indexOutOfRangeMessage = "index out of range"
	missingKeyMessage      = "map has no such key"
	staleHandleMessage     = "handle names no element"
	emptyListMessage       = "pop from an empty list"
)

// runtimeCall is a call of a runtime entry with its arguments and its routine looked up.
type runtimeCall struct {
	ir.Instr
	args    []ir.V
	routine *ast.FuncDecl
}

func (vm *vm) runtimeEntry(fr *frame, in *ir.Instr) {
	c := runtimeCall{Instr: *in, args: fr.fn.CallArgs(*in), routine: fr.fn.Routine(*in)}
	arg := func(i int) value { return fr.regs[c.args[i]] }
	set := func(v value) { fr.regs[c.Dst] = v }
	num := func(n int64) { set(value{n: n}) }
	flag := func(b bool) {
		if b {
			num(1)
		} else {
			num(0)
		}
	}
	switch c.Symbol() {
	case ir.RtWrite:
		text := vm.text(arg(1), "a write")
		switch arg(0).n {
		case stdoutHandle:
			vm.stdout = append(vm.stdout, text...)
		case stderrHandle:
			vm.stderr = append(vm.stderr, text...)
		default:
			panic(diag.Internal("interp: write to handle %d", arg(0).n))
		}
	case ir.RtPanic:
		vm.trap(vm.text(arg(0), "a panic message"))
	case ir.RtConcat:
		a, b := vm.text(arg(0), "a concatenation"), vm.text(arg(1), "a concatenation")
		set(value{ref: vm.newString(a + b)})
	case ir.RtItoa:
		n := arg(0).n
		if arg(1).n != 0 {
			set(value{ref: vm.newString(strconv.FormatUint(uint64(n), 10))})
		} else {
			set(value{ref: vm.newString(strconv.FormatInt(n, 10))})
		}
	case ir.RtStreq:
		flag(vm.text(arg(0), "a comparison") == vm.text(arg(1), "a comparison"))

	case ir.RtFrozenFreeze:
		vm.tracking = true
		set(value{ref: vm.newShared(fr, kindFrozen, c.args[0], "the freeze")})
	case ir.RtCownNew:
		vm.tracking = true
		cown := vm.newShared(fr, kindCown, c.args[0], "the new cown")
		if c.routine != nil {
			cown.ending = vm.function(c.routine)
		}
		set(value{ref: cown})
	case ir.RtFrozenGet:
		set(vm.shared(arg(0), kindFrozen, "reading a frozen value").fields[0].v)
	case ir.RtShare:
		o := vm.live(arg(0), "a share")
		if !o.shared() {
			panic(diag.Internal("interp: a share of %s, which is not a frozen value or a cown", o))
		}
		o.count++
		set(value{ref: o})

	case ir.RtArrayNew:
		set(value{ref: vm.newObject(kindArray)})
	case ir.RtArrayLen:
		num(int64(len(vm.collection(arg(0), kindArray, "a length").elems)))
	case ir.RtArrayElem:
		a := vm.collection(arg(0), kindArray, "an element address")
		i := arg(1).n
		if uint64(i) >= uint64(len(a.elems)) {
			vm.trap(indexOutOfRangeMessage)
		}
		set(addressOf(a.elems[i]))
	case ir.RtArrayInsert:
		a := vm.collection(arg(0), kindArray, "an insert")
		i := arg(1).n
		if uint64(i) > uint64(len(a.elems)) {
			vm.trap(indexOutOfRangeMessage)
		}
		e := vm.moveIn(fr, a, c.args[2], "the insert")
		vm.restructure(a)
		a.elems = append(a.elems, nil)
		copy(a.elems[i+1:], a.elems[i:])
		a.elems[i] = e
	case ir.RtArrayRemove:
		a := vm.collection(arg(0), kindArray, "a remove")
		i := arg(1).n
		if uint64(i) >= uint64(len(a.elems)) {
			vm.trap(indexOutOfRangeMessage)
		}
		vm.restructure(a)
		e := a.elems[i]
		a.elems = append(a.elems[:i], a.elems[i+1:]...)
		set(vm.moveOutOfCollection(e, "the remove"))

	case ir.RtMapNew:
		m := vm.newObject(kindMap)
		m.mapped = &mapData{index: map[mapKey]*mapEntry{}}
		set(value{ref: m})
	case ir.RtMapLen:
		num(int64(len(vm.collection(arg(0), kindMap, "a length").mapped.entries)))
	case ir.RtMapHas:
		m := vm.collection(arg(0), kindMap, "a lookup")
		_, found := m.mapped.index[vm.keyOf(fr, c.args[1], "a lookup")]
		flag(found)
	case ir.RtMapElem:
		m := vm.collection(arg(0), kindMap, "an element address")
		e, found := m.mapped.index[vm.keyOf(fr, c.args[1], "a lookup")]
		if !found {
			vm.trap(missingKeyMessage)
		}
		set(addressOf(&e.val))
	case ir.RtMapInsert:
		vm.mapInsert(fr, c)
	case ir.RtMapRemove:
		m := vm.collection(arg(0), kindMap, "a remove")
		key := vm.keyOf(fr, c.args[1], "a remove")
		e, found := m.mapped.index[key]
		if !found {
			vm.trap(missingKeyMessage)
		}
		vm.restructure(m)
		delete(m.mapped.index, key)
		for i, other := range m.mapped.entries {
			if other == e {
				m.mapped.entries = append(m.mapped.entries[:i], m.mapped.entries[i+1:]...)
				break
			}
		}
		vm.freeCell(&e.key)
		set(vm.moveOutOfCollection(&e.val, "the remove"))
	case ir.RtMapCopy:
		set(value{ref: vm.copyObject(vm.collection(arg(0), kindMap, "a copy"))})

	case ir.RtListNew:
		l := vm.newObject(kindList)
		l.table = &table{}
		set(value{ref: l})
	case ir.RtListLen:
		num(int64(len(vm.collection(arg(0), kindList, "a length").table.order)))
	case ir.RtListContains:
		_, found := vm.collection(arg(0), kindList, "a lookup").table.lookup(arg(1).n)
		flag(found)
	case ir.RtListElem:
		l := vm.collection(arg(0), kindList, "an element address")
		set(addressOf(l.table.cells[vm.slotOf(l, arg(1).n)]))
	case ir.RtListPushFront, ir.RtListPushBack:
		l := vm.collection(arg(0), kindList, "a push")
		e := vm.moveIn(fr, l, c.args[1], "the push")
		vm.restructure(l)
		slot, h := l.table.acquire(e)
		if c.Symbol() == ir.RtListPushFront {
			l.table.order = append([]int{slot}, l.table.order...)
		} else {
			l.table.order = append(l.table.order, slot)
		}
		num(h)
	case ir.RtListPopFront, ir.RtListPopBack:
		l := vm.collection(arg(0), kindList, "a pop")
		if len(l.table.order) == 0 {
			vm.trap(emptyListMessage)
		}
		slot := l.table.order[0]
		if c.Symbol() == ir.RtListPopBack {
			slot = l.table.order[len(l.table.order)-1]
		}
		set(vm.takeSlot(l, slot, "the pop"))
	case ir.RtListFirst, ir.RtListLast:
		l := vm.collection(arg(0), kindList, "a walk")
		if len(l.table.order) == 0 {
			num(0)
			break
		}
		slot := l.table.order[0]
		if c.Symbol() == ir.RtListLast {
			slot = l.table.order[len(l.table.order)-1]
		}
		num(handleOf(l.table.gens[slot], slot))
	case ir.RtListNext, ir.RtListPrev:
		l := vm.collection(arg(0), kindList, "a walk")
		at := l.table.position(vm.slotOf(l, arg(1).n))
		if c.Symbol() == ir.RtListNext {
			at++
		} else {
			at--
		}
		if at < 0 || at >= len(l.table.order) {
			num(0)
			break
		}
		slot := l.table.order[at]
		num(handleOf(l.table.gens[slot], slot))
	case ir.RtListRemove:
		l := vm.collection(arg(0), kindList, "a remove")
		set(vm.takeSlot(l, vm.slotOf(l, arg(1).n), "the remove"))
	case ir.RtListMoveToFront:
		l := vm.collection(arg(0), kindList, "a move")
		slot := vm.slotOf(l, arg(1).n)
		vm.restructure(l)
		l.table.unlink(slot)
		l.table.order = append([]int{slot}, l.table.order...)
	case ir.RtListCopy:
		set(value{ref: vm.copyObject(vm.collection(arg(0), kindList, "a copy"))})

	case ir.RtPoolNew:
		p := vm.newObject(kindPool)
		p.table = &table{}
		set(value{ref: p})
	case ir.RtPoolLen:
		num(int64(vm.collection(arg(0), kindPool, "a length").table.count()))
	case ir.RtPoolContains:
		_, found := vm.collection(arg(0), kindPool, "a lookup").table.lookup(arg(1).n)
		flag(found)
	case ir.RtPoolElem:
		p := vm.collection(arg(0), kindPool, "an element address")
		set(addressOf(p.table.cells[vm.slotOf(p, arg(1).n)]))
	case ir.RtPoolInsert:
		p := vm.collection(arg(0), kindPool, "an insert")
		e := vm.moveIn(fr, p, c.args[1], "the insert")
		vm.restructure(p)
		_, h := p.table.acquire(e)
		num(h)
	case ir.RtPoolRemove:
		p := vm.collection(arg(0), kindPool, "a remove")
		set(vm.takeSlot(p, vm.slotOf(p, arg(1).n), "the remove"))
	case ir.RtPoolCopy:
		set(value{ref: vm.copyObject(vm.collection(arg(0), kindPool, "a copy"))})
	default:
		panic(diag.Internal("interp: unhandled runtime entry %s", c.Symbol()))
	}
}

// text is the contents of a string that must exist and be live.
func (vm *vm) text(v value, what string) string {
	o := vm.live(v, what)
	if o.kind != kindString {
		panic(diag.Internal("interp: %s reads %s as a string", what, o))
	}
	return o.text
}

// newShared makes a frozen value or a cown that owns a value the entry takes, with one owner: the reference it returns.
func (vm *vm) newShared(fr *frame, kind objectKind, src ir.V, what string) *object {
	box := vm.newObject(kind)
	box.fields = []cell{{holder: box}}
	vm.store(fr, &box.fields[0], src, what)
	return box
}

// shared is a frozen value or a cown an entry works on.
func (vm *vm) shared(v value, kind objectKind, what string) *object {
	o := vm.live(v, what)
	if o.kind != kind {
		panic(diag.Internal("interp: %s expects a %s, found %s", what, kindNames[kind], o))
	}
	return o
}

func (vm *vm) collection(v value, kind objectKind, what string) *object {
	o := vm.live(v, what)
	if o.kind != kind {
		panic(diag.Internal("interp: %s expects a %s, found %s", what, kindNames[kind], o))
	}
	return o
}

// restructure is a change to the structure of a collection, which ends every address taken into it.
func (vm *vm) restructure(o *object) {
	vm.changed(region{obj: o})
	o.version++
}

// moveIn puts an element that an entry takes, in a new place of a collection, which becomes its owner.
func (vm *vm) moveIn(fr *frame, holder *object, src ir.V, what string) *cell {
	c := &cell{holder: holder}
	vm.store(fr, c, src, what)
	return c
}

// moveOutOfCollection gives an element to the caller and empties its place.
func (vm *vm) moveOutOfCollection(c *cell, what string) value {
	v := c.v
	vm.release(c, what)
	return v
}

// takeSlot gives the element of a list or a pool slot to the caller and releases the slot, so a handle to it finds
// nothing from now on, even when the slot is used again.
func (vm *vm) takeSlot(o *object, slot int, what string) value {
	vm.restructure(o)
	c := o.table.cells[slot]
	o.table.release(slot)
	if o.kind == kindList {
		o.table.unlink(slot)
	}
	return vm.moveOutOfCollection(c, what)
}

func (vm *vm) slotOf(o *object, handle int64) int {
	slot, ok := o.table.lookup(handle)
	if !ok {
		vm.trap(staleHandleMessage)
	}
	return slot
}

func (vm *vm) keyOf(fr *frame, r ir.V, what string) mapKey {
	v := fr.regs[r]
	if fr.fn.Regs[r] == ir.KindManaged {
		return mapKey{text: vm.text(v, what), isText: true}
	}
	return mapKey{n: v.n}
}

// mapInsert adds a key and a value, which the map owns. A key that is there keeps its place: the value that was
// there is dropped, and so is the key that came in, since the map holds one.
func (vm *vm) mapInsert(fr *frame, c runtimeCall) {
	m := vm.collection(fr.regs[c.args[0]], kindMap, "an insert")
	key := vm.keyOf(fr, c.args[1], "an insert")
	vm.restructure(m)
	if e, found := m.mapped.index[key]; found {
		vm.dropCell(&e.val)
		vm.store(fr, &e.val, c.args[2], "the insert")
		if o := fr.regs[c.args[1]].ref; o != nil && !o.immortal {
			vm.drop(o)
		}
		return
	}
	e := &mapEntry{key: cell{holder: m}, val: cell{holder: m}}
	vm.store(fr, &e.key, c.args[1], "the insert")
	vm.store(fr, &e.val, c.args[2], "the insert")
	m.mapped.entries = append(m.mapped.entries, e)
	m.mapped.index[key] = e
}

// copyObject is a deep copy of an object: a new object for each one inside it, in the same order. A string literal is
// never freed, so it is shared.
func (vm *vm) copyObject(o *object) *object {
	vm.requireLive(o, "copied")
	switch o.kind {
	case kindFrozen, kindCown:
		// The copy of a shared value is another owner of it.
		o.count++
		return o
	case kindString:
		if o.immortal {
			return o
		}
		return vm.newString(o.text)
	case kindBox:
		out := vm.newBox(len(o.fields))
		for i := range o.fields {
			vm.copyCell(&out.fields[i], o.fields[i].v)
		}
		return out
	case kindArray:
		out := vm.newObject(kindArray)
		for _, e := range o.elems {
			dst := &cell{holder: out}
			vm.copyCell(dst, e.v)
			out.elems = append(out.elems, dst)
		}
		return out
	case kindMap:
		out := vm.newObject(kindMap)
		out.mapped = &mapData{index: map[mapKey]*mapEntry{}}
		for _, e := range o.mapped.entries {
			dst := &mapEntry{key: cell{holder: out}, val: cell{holder: out}}
			vm.copyCell(&dst.key, e.key.v)
			vm.copyCell(&dst.val, e.val.v)
			out.mapped.entries = append(out.mapped.entries, dst)
			out.mapped.index[vm.mapKeyOf(e.key.v)] = dst
		}
		return out
	case kindList, kindPool:
		out := vm.newObject(o.kind)
		src := o.table
		out.table = &table{
			gens:  append([]uint32(nil), src.gens...),
			live:  append([]bool(nil), src.live...),
			cells: make([]*cell, len(src.cells)),
			free:  append([]int(nil), src.free...),
			order: append([]int(nil), src.order...),
		}
		for slot, c := range src.cells {
			if c != nil {
				out.table.cells[slot] = &cell{holder: out}
				vm.copyCell(out.table.cells[slot], c.v)
			}
		}
		return out
	}
	panic(diag.Internal("interp: cannot copy %s", o))
}

func (vm *vm) mapKeyOf(v value) mapKey {
	if v.ref != nil {
		return mapKey{text: v.ref.text, isText: true}
	}
	return mapKey{n: v.n}
}

// copyCell fills a new place with a copy of a value: a scalar as it is, an object as a new one that the place owns.
func (vm *vm) copyCell(dst *cell, v value) {
	if v.ref == nil {
		dst.v = v
		return
	}
	vm.adopt(dst, value{ref: vm.copyObject(v.ref)}, "the copy")
}
