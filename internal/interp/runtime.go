package interp

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"minimaxon/internal/ir"
	"slices"
	"strconv"
)

const (
	indexOutOfRangeMessage = "index out of range"
	missingKeyMessage      = "map has no such key"
	staleHandleMessage     = "handle names no element"
	emptyListMessage       = "pop from an empty list"
)

// runtimeCall is a call of a runtime entry with its arguments and its routine looked up, in the frame that makes it.
type runtimeCall struct {
	ir.Instr
	fr      *frame
	args    []ir.V
	routine *ast.FuncDecl
}

func (vm *vm) runtimeEntry(fr *frame, in *ir.Instr) {
	vm.runEntry(&runtimeCall{Instr: *in, fr: fr, args: fr.fn.CallArgs(*in), routine: fr.fn.Routine(*in)})
}

func (c *runtimeCall) arg(i int) value { return c.fr.regs[c.args[i]] }

func (c *runtimeCall) set(v value) { c.fr.regs[c.Dst] = v }

func (c *runtimeCall) num(n int64) { c.set(value{n: n}) }

func (c *runtimeCall) flag(b bool) {
	if b {
		c.num(1)
	} else {
		c.num(0)
	}
}

func (vm *vm) entryWrite(c *runtimeCall) {
	text := vm.text(c.arg(1), "a write")
	switch c.arg(0).n {
	case stdoutHandle:
		vm.stdout = append(vm.stdout, text...)
	case stderrHandle:
		vm.stderr = append(vm.stderr, text...)
	default:
		panic(diag.Internal("interp: write to handle %d", c.arg(0).n))
	}
}

func (vm *vm) entryPanic(c *runtimeCall) {
	vm.trap(vm.text(c.arg(0), "a panic message"))
}

func (vm *vm) entryConcat(c *runtimeCall) {
	a, b := vm.text(c.arg(0), "a concatenation"), vm.text(c.arg(1), "a concatenation")
	c.set(value{ref: vm.newString(a + b)})
}

func (vm *vm) entryItoa(c *runtimeCall) {
	n := c.arg(0).n
	if c.arg(1).n != 0 {
		c.set(value{ref: vm.newString(strconv.FormatUint(uint64(n), 10))})
	} else {
		c.set(value{ref: vm.newString(strconv.FormatInt(n, 10))})
	}
}

func (vm *vm) entryStreq(c *runtimeCall) {
	c.flag(vm.text(c.arg(0), "a comparison") == vm.text(c.arg(1), "a comparison"))
}

func (vm *vm) entryStrLen(c *runtimeCall) {
	c.num(int64(len(vm.text(c.arg(0), "a string byteLength"))))
}

func (vm *vm) entryStrByteAt(c *runtimeCall) {
	s := vm.text(c.arg(0), "a string byteAt")
	i := c.arg(1).n
	if uint64(i) >= uint64(len(s)) {
		vm.trap("string byte position out of bounds")
	}
	c.num(int64(s[i]))
}

func (vm *vm) entryStrSlice(c *runtimeCall) {
	s := vm.text(c.arg(0), "a string slice")
	from, upto := uint64(c.arg(1).n), uint64(c.arg(2).n)
	if from > upto || upto > uint64(len(s)) {
		vm.trap("string slice out of bounds")
	}
	c.set(value{ref: vm.newString(s[from:upto])})
}

func (vm *vm) entryFrozenFreeze(c *runtimeCall) {
	vm.tracking = true
	c.set(value{ref: vm.newShared(c.fr, kindFrozen, c.args[0], "the freeze")})
}

func (vm *vm) entryCownNew(c *runtimeCall) {
	vm.tracking = true
	cown := vm.newShared(c.fr, kindCown, c.args[0], "the new cown")
	if c.routine != nil {
		cown.ending = vm.function(c.routine)
	}
	c.set(value{ref: cown})
}

func (vm *vm) entryFrozenGet(c *runtimeCall) {
	c.set(vm.shared(c.arg(0), kindFrozen, "reading a frozen value").fields[0].v)
}

func (vm *vm) entryShare(c *runtimeCall) {
	o := vm.live(c.arg(0), "a share")
	if !o.shared() {
		panic(diag.Internal("interp: a share of %s, which is not a frozen value or a cown", o))
	}
	o.count++
	c.set(value{ref: o})
}

func (vm *vm) entryArrayNew(c *runtimeCall) {
	c.set(value{ref: vm.newObject(kindArray)})
}

func (vm *vm) entryArrayLen(c *runtimeCall) {
	c.num(int64(len(vm.collection(c.arg(0), kindArray, "a length").elems)))
}

func (vm *vm) entryArrayElem(c *runtimeCall) {
	a := vm.collection(c.arg(0), kindArray, "an element address")
	i := c.arg(1).n
	if uint64(i) >= uint64(len(a.elems)) {
		vm.trap(indexOutOfRangeMessage)
	}
	c.set(addressOf(a.elems[i]))
}

func (vm *vm) entryArrayInsert(c *runtimeCall) {
	a := vm.collection(c.arg(0), kindArray, "an insert")
	i := c.arg(1).n
	if uint64(i) > uint64(len(a.elems)) {
		vm.trap(indexOutOfRangeMessage)
	}
	e := vm.moveIn(c.fr, a, c.args[2], "the insert")
	vm.restructure(a)
	a.elems = append(a.elems, nil)
	copy(a.elems[i+1:], a.elems[i:])
	a.elems[i] = e
}

func (vm *vm) entryArrayRemove(c *runtimeCall) {
	a := vm.collection(c.arg(0), kindArray, "a remove")
	i := c.arg(1).n
	if uint64(i) >= uint64(len(a.elems)) {
		vm.trap(indexOutOfRangeMessage)
	}
	vm.restructure(a)
	e := a.elems[i]
	a.elems = append(a.elems[:i], a.elems[i+1:]...)
	c.set(vm.moveOutOfCollection(e, "the remove"))
}

func (vm *vm) entryBytesNew(c *runtimeCall) {
	c.set(value{ref: vm.newObject(kindBytes)})
}

func (vm *vm) entryBytesLen(c *runtimeCall) {
	c.num(int64(len(vm.collection(c.arg(0), kindBytes, "a length").buf)))
}

func (vm *vm) entryBytesGet(c *runtimeCall) {
	b := vm.collection(c.arg(0), kindBytes, "a byte read")
	i := c.arg(1).n
	if uint64(i) >= uint64(len(b.buf)) {
		vm.trap(indexOutOfRangeMessage)
	}
	c.num(int64(b.buf[i]))
}

func (vm *vm) entryBytesSet(c *runtimeCall) {
	b := vm.collection(c.arg(0), kindBytes, "a byte write")
	i := c.arg(1).n
	if uint64(i) >= uint64(len(b.buf)) {
		vm.trap(indexOutOfRangeMessage)
	}
	vm.restructure(b)
	b.buf[i] = byte(c.arg(2).n)
}

func (vm *vm) entryBytesInsert(c *runtimeCall) {
	b := vm.collection(c.arg(0), kindBytes, "an insert")
	i := c.arg(1).n
	if uint64(i) > uint64(len(b.buf)) {
		vm.trap(indexOutOfRangeMessage)
	}
	vm.restructure(b)
	b.buf = slices.Insert(b.buf, int(i), byte(c.arg(2).n))
}

func (vm *vm) entryBytesRemove(c *runtimeCall) {
	b := vm.collection(c.arg(0), kindBytes, "a remove")
	i := c.arg(1).n
	if uint64(i) >= uint64(len(b.buf)) {
		vm.trap(indexOutOfRangeMessage)
	}
	vm.restructure(b)
	c.num(int64(b.buf[i]))
	b.buf = slices.Delete(b.buf, int(i), int(i)+1)
}

func (vm *vm) entryBytesAppend(c *runtimeCall) {
	b := vm.collection(c.arg(0), kindBytes, "an append")
	other := vm.collection(c.arg(1), kindBytes, "an append source")
	vm.restructure(b)
	b.buf = append(b.buf, other.buf...)
}

func (vm *vm) entryBytesAppendStr(c *runtimeCall) {
	b := vm.collection(c.arg(0), kindBytes, "an appendString")
	text := vm.text(c.arg(1), "an appendString source")
	vm.restructure(b)
	b.buf = append(b.buf, text...)
}

func (vm *vm) entryBytesToString(c *runtimeCall) {
	b := vm.collection(c.arg(0), kindBytes, "a toString")
	c.set(value{ref: vm.newString(string(b.buf))})
}

func (vm *vm) entryMapNew(c *runtimeCall) {
	m := vm.newObject(kindMap)
	m.mapped = &mapData{index: map[mapKey]*mapEntry{}}
	c.set(value{ref: m})
}

func (vm *vm) entryMapLen(c *runtimeCall) {
	c.num(int64(len(vm.collection(c.arg(0), kindMap, "a length").mapped.entries)))
}

func (vm *vm) entryMapHas(c *runtimeCall) {
	m := vm.collection(c.arg(0), kindMap, "a lookup")
	_, found := m.mapped.index[vm.keyOf(c.fr, c.args[1], "a lookup")]
	c.flag(found)
}

func (vm *vm) entryMapElem(c *runtimeCall) {
	m := vm.collection(c.arg(0), kindMap, "an element address")
	e, found := m.mapped.index[vm.keyOf(c.fr, c.args[1], "a lookup")]
	if !found {
		vm.trap(missingKeyMessage)
	}
	c.set(addressOf(&e.val))
}

func (vm *vm) entryMapInsert(c *runtimeCall) {
	vm.mapInsert(c)
}

func (vm *vm) entryMapRemove(c *runtimeCall) {
	m := vm.collection(c.arg(0), kindMap, "a remove")
	key := vm.keyOf(c.fr, c.args[1], "a remove")
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
	c.set(vm.moveOutOfCollection(&e.val, "the remove"))
}

func (vm *vm) entryMapCopy(c *runtimeCall) {
	c.set(value{ref: vm.copyObject(vm.collection(c.arg(0), kindMap, "a copy"))})
}

func (vm *vm) entryListNew(c *runtimeCall) {
	l := vm.newObject(kindList)
	l.table = &table{}
	c.set(value{ref: l})
}

func (vm *vm) entryListLen(c *runtimeCall) {
	c.num(int64(len(vm.collection(c.arg(0), kindList, "a length").table.order)))
}

func (vm *vm) entryListContains(c *runtimeCall) {
	_, found := vm.collection(c.arg(0), kindList, "a lookup").table.lookup(c.arg(1).n)
	c.flag(found)
}

func (vm *vm) entryListElem(c *runtimeCall) {
	l := vm.collection(c.arg(0), kindList, "an element address")
	c.set(addressOf(l.table.cells[vm.slotOf(l, c.arg(1).n)]))
}

func (vm *vm) entryListPushFront(c *runtimeCall) { vm.listPush(c, true) }

func (vm *vm) entryListPushBack(c *runtimeCall) { vm.listPush(c, false) }

// listPush adds an element at the front of a list or at its back.
func (vm *vm) listPush(c *runtimeCall, front bool) {
	l := vm.collection(c.arg(0), kindList, "a push")
	e := vm.moveIn(c.fr, l, c.args[1], "the push")
	vm.restructure(l)
	slot, h := l.table.acquire(e)
	if front {
		l.table.order = append([]int{slot}, l.table.order...)
	} else {
		l.table.order = append(l.table.order, slot)
	}
	c.num(h)
}

func (vm *vm) entryListPopFront(c *runtimeCall) { vm.listPop(c, false) }

func (vm *vm) entryListPopBack(c *runtimeCall) { vm.listPop(c, true) }

// listPop takes the element at the front of a list or at its back.
func (vm *vm) listPop(c *runtimeCall, back bool) {
	l := vm.collection(c.arg(0), kindList, "a pop")
	if len(l.table.order) == 0 {
		vm.trap(emptyListMessage)
	}
	slot := l.table.order[0]
	if back {
		slot = l.table.order[len(l.table.order)-1]
	}
	c.set(vm.takeSlot(l, slot, "the pop"))
}

func (vm *vm) entryListFirst(c *runtimeCall) { vm.listEnd(c, false) }

func (vm *vm) entryListLast(c *runtimeCall) { vm.listEnd(c, true) }

// listEnd is the handle of the first element of a list or of its last, or 0 for an empty list.
func (vm *vm) listEnd(c *runtimeCall, last bool) {
	l := vm.collection(c.arg(0), kindList, "a walk")
	if len(l.table.order) == 0 {
		c.num(0)
		return
	}
	slot := l.table.order[0]
	if last {
		slot = l.table.order[len(l.table.order)-1]
	}
	c.num(handleOf(l.table.gens[slot], slot))
}

func (vm *vm) entryListNext(c *runtimeCall) { vm.listStep(c, true) }

func (vm *vm) entryListPrev(c *runtimeCall) { vm.listStep(c, false) }

// listStep is the handle of the element after the one a handle names or before it, or 0 past the end.
func (vm *vm) listStep(c *runtimeCall, next bool) {
	l := vm.collection(c.arg(0), kindList, "a walk")
	at := l.table.position(vm.slotOf(l, c.arg(1).n))
	if next {
		at++
	} else {
		at--
	}
	if at < 0 || at >= len(l.table.order) {
		c.num(0)
		return
	}
	slot := l.table.order[at]
	c.num(handleOf(l.table.gens[slot], slot))
}

func (vm *vm) entryListRemove(c *runtimeCall) {
	l := vm.collection(c.arg(0), kindList, "a remove")
	c.set(vm.takeSlot(l, vm.slotOf(l, c.arg(1).n), "the remove"))
}

func (vm *vm) entryListMoveToFront(c *runtimeCall) {
	l := vm.collection(c.arg(0), kindList, "a move")
	slot := vm.slotOf(l, c.arg(1).n)
	vm.restructure(l)
	l.table.unlink(slot)
	l.table.order = append([]int{slot}, l.table.order...)
}

func (vm *vm) entryListCopy(c *runtimeCall) {
	c.set(value{ref: vm.copyObject(vm.collection(c.arg(0), kindList, "a copy"))})
}

func (vm *vm) entryPoolNew(c *runtimeCall) {
	p := vm.newObject(kindPool)
	p.table = &table{}
	c.set(value{ref: p})
}

func (vm *vm) entryPoolLen(c *runtimeCall) {
	c.num(int64(vm.collection(c.arg(0), kindPool, "a length").table.count()))
}

func (vm *vm) entryPoolContains(c *runtimeCall) {
	_, found := vm.collection(c.arg(0), kindPool, "a lookup").table.lookup(c.arg(1).n)
	c.flag(found)
}

func (vm *vm) entryPoolElem(c *runtimeCall) {
	p := vm.collection(c.arg(0), kindPool, "an element address")
	c.set(addressOf(p.table.cells[vm.slotOf(p, c.arg(1).n)]))
}

func (vm *vm) entryPoolInsert(c *runtimeCall) {
	p := vm.collection(c.arg(0), kindPool, "an insert")
	e := vm.moveIn(c.fr, p, c.args[1], "the insert")
	vm.restructure(p)
	_, h := p.table.acquire(e)
	c.num(h)
}

func (vm *vm) entryPoolRemove(c *runtimeCall) {
	p := vm.collection(c.arg(0), kindPool, "a remove")
	c.set(vm.takeSlot(p, vm.slotOf(p, c.arg(1).n), "the remove"))
}

func (vm *vm) entryPoolCopy(c *runtimeCall) {
	c.set(value{ref: vm.copyObject(vm.collection(c.arg(0), kindPool, "a copy"))})
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
func (vm *vm) mapInsert(c *runtimeCall) {
	m := vm.collection(c.fr.regs[c.args[0]], kindMap, "an insert")
	key := vm.keyOf(c.fr, c.args[1], "an insert")
	vm.restructure(m)
	if e, found := m.mapped.index[key]; found {
		vm.dropCell(&e.val)
		vm.store(c.fr, &e.val, c.args[2], "the insert")
		if o := c.fr.regs[c.args[1]].ref; o != nil && !o.immortal {
			vm.drop(o)
		}
		return
	}
	e := &mapEntry{key: cell{holder: m}, val: cell{holder: m}}
	vm.store(c.fr, &e.key, c.args[1], "the insert")
	vm.store(c.fr, &e.val, c.args[2], "the insert")
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
	case kindBytes:
		out := vm.newObject(kindBytes)
		out.buf = append([]byte(nil), o.buf...)
		return out
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
