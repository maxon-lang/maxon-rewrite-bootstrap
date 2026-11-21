package ir

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
)

// arrayBox is the runtime of one array type, which every operation on an array goes through: the array entries, whose
// box holds a word to an element, or for an array that packs bytes (ast.BuiltinTy.PacksBytes) the bytes entries, whose
// box holds one byte to an element. site is the source of the operation.
type arrayBox struct {
	l    *Lowerer
	site ast.Pos
	ty   *ast.BuiltinTy
}

// arrayOf is the runtime of the array type t, read through the binding and through a frozen value.
func (l *Lowerer) arrayOf(site ast.Pos, t ast.Ty) arrayBox {
	bt, ok := ast.Content(l.binding.Apply(t)).(*ast.BuiltinTy)
	if !ok || bt.Kind != ast.BuiltinArray {
		panic(diag.Internal("lower: %s is not an array", t.Name()))
	}
	return arrayBox{l: l, site: site, ty: bt}
}

func (a arrayBox) packed() bool { return a.ty.PacksBytes() }

func (a arrayBox) elem() ast.Ty { return a.ty.Element() }

func (a arrayBox) newBox() V {
	if a.packed() {
		return a.l.entry(a.site, RtBytesNew, KindManaged)
	}
	return a.l.entry(a.site, RtArrayNew, KindManaged)
}

func (a arrayBox) length(arr V) V {
	if a.packed() {
		return a.l.entry(a.site, RtBytesLen, KindScalar, arr)
	}
	return a.l.entry(a.site, RtArrayLen, KindScalar, arr)
}

// insert moves v in at index, which may be the length.
func (a arrayBox) insert(arr, index, v V) {
	if a.packed() {
		a.l.entryVoid(a.site, RtBytesInsert, arr, index, v)
		return
	}
	a.l.entryVoid(a.site, RtArrayInsert, arr, index, v)
}

func (a arrayBox) push(arr, v V) { a.insert(arr, a.length(arr), v) }

// remove moves the element at index out.
func (a arrayBox) remove(arr, index V) V {
	if a.packed() {
		return a.l.entry(a.site, RtBytesRemove, KindScalar, arr, index)
	}
	return a.l.entry(a.site, RtArrayRemove, a.l.kind(a.elem()), arr, index)
}

// load reads the element at index: a byte, a scalar, or a managed element lent for as long as the array is not
// changed.
func (a arrayBox) load(arr, index V) V {
	if a.packed() {
		return a.l.entry(a.site, RtBytesGet, KindScalar, arr, index)
	}
	d := a.l.reg(a.l.kind(a.elem()))
	a.l.emit(LoadAt(d, a.address(arr, index)))
	return d
}

// store writes v to the element at index, dropping the element it replaces.
func (a arrayBox) store(arr, index, v V) {
	if a.packed() {
		a.l.entryVoid(a.site, RtBytesSet, arr, index, v)
		return
	}
	addr := a.address(arr, index)
	a.l.dropAt(addr, a.elem())
	a.l.emit(StoreAt(addr, v))
}

// address is where the element at index of an array of words is. A byte has no address a word can be read through:
// its place is a slot instead.
func (a arrayBox) address(arr, index V) V {
	if a.packed() {
		panic(diag.Internal("lower: an element of %s has no word address", a.ty.Name()))
	}
	return a.l.entry(a.site, RtArrayElem, KindScalar, arr, index)
}

// writeBack is an element of a byte array that a call takes by address, through a slot that holds it: the slot is
// written back to the element when the call returns.
type writeBack struct {
	box        arrayBox
	arr, index V
	slot       int
}

// place is the address a call that takes the element at index by address is given: the element's own for an array of
// words, and for an array of bytes a slot that holds the element until writeBack stores it back.
func (a arrayBox) place(arr, index V) V {
	if !a.packed() {
		return a.address(arr, index)
	}
	slot := a.l.f.NewSlot(KindScalar)
	a.l.emit(StoreSlot(slot, a.load(arr, index), false))
	a.l.writeBacks = append(a.l.writeBacks, writeBack{box: a, arr: arr, index: index, slot: slot})
	d := a.l.reg(KindScalar)
	a.l.emit(SlotAddr(d, slot))
	return d
}

// writeBack stores the slots of the places taken since mark back into their elements.
func (l *Lowerer) writeBack(mark int) {
	for _, w := range l.writeBacks[mark:] {
		w.box.store(w.arr, w.index, l.load(w.slot))
	}
	l.writeBacks = l.writeBacks[:mark]
}

// appendCopy pushes a copy of each element of other, which is only read. The length of other is read once, before the
// first push.
func (a arrayBox) appendCopy(arr, other V) {
	if a.packed() {
		a.l.entryVoid(a.site, RtBytesAppend, arr, other)
		return
	}
	a.l.countedLoop(a.length(other), func(i V) {
		a.push(arr, a.l.copyValue(a.site, a.load(other, i), a.elem()))
	})
}

// appendText pushes the UTF-8 bytes of a String, which only an array of bytes holds as they are.
func (a arrayBox) appendText(arr, text V) {
	a.requirePacked()
	a.l.entryVoid(a.site, RtBytesAppendStr, arr, text)
}

// text is a String of the bytes of an array of bytes.
func (a arrayBox) text(arr V) V {
	a.requirePacked()
	return a.l.entry(a.site, RtBytesToString, KindManaged, arr)
}

func (a arrayBox) requirePacked() {
	if !a.packed() {
		panic(diag.Internal("lower: %s holds no bytes", a.ty.Name()))
	}
}

// inBounds fails with indexOutOfBounds unless index is below the length, after drop, when it is not nil, has dropped
// what the operation was given.
func (a arrayBox) inBounds(arr, index V, drop func()) {
	a.l.failUnless(a.l.compare(CmpLt, index, a.length(arr), true), ast.ErrorIndexOutOfBounds, drop)
}
