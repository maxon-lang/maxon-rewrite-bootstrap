package ir

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"slices"
)

const (
	dropEmptyMessage       = "dropEmpty of a collection that still holds an element"
	interpreterOnlyMessage = "a map, a list, a pool, a frozen value, a cown or a behaviour runs only under the interpreter for now, and the compiler has no code for it; use an array, or run the program with the interpreter"
)

// entry calls a runtime entry of a primitive that returns a value. site is the source that needs it, kept when the
// backend has no code for the entry.
func (l *Lowerer) entry(site ast.Pos, symbol Rt, kind IrKind, args ...V) V {
	d := l.reg(kind)
	l.emitEntry(site, d, symbol, args, nil)
	return d
}

// entryVoid calls a runtime entry that returns nothing.
func (l *Lowerer) entryVoid(site ast.Pos, symbol Rt, args ...V) {
	l.emitEntry(site, NoReg, symbol, args, nil)
}

func (l *Lowerer) emitEntry(site ast.Pos, dst V, symbol Rt, args []V, routine *ast.FuncDecl) {
	switch symbol.Unit() {
	case RtUnitOs:
		l.f.UsesOs = true
	case RtUnitBytes:
		l.f.UsesBytes = true
	case RtUnitArrays:
		l.f.UsesArrays = true
	case RtUnitInterpreter:
		l.refuse(site, diag.ErrInterpreterOnly, interpreterOnlyMessage)
	case RtUnitRuntime, RtUnitLinker:
		panic(diag.Internal("lower: %s is no entry of a primitive", symbol))
	}
	l.emit(l.b.CallRuntime(dst, symbol, args, routine))
}

// refuse notes a place that cannot be compiled yet, once for each site.
func (l *Lowerer) refuse(site ast.Pos, code, message string) {
	if n := len(l.f.Refusals); n == 0 || l.f.Refusals[n-1].Site != site {
		l.f.Refusals = append(l.f.Refusals, Refusal{Site: site, Code: code, Message: message})
	}
}

// requireOrTrap continues only when cond holds, and panics with the message otherwise.
func (l *Lowerer) requireOrTrap(cond V, message string) {
	ok := l.b.NewBlock()
	bad := l.b.NewBlock()
	l.b.Terminate(l.cur, Branch(cond, ok, bad))
	l.cur = bad
	l.emit(l.b.CallRuntime(NoReg, RtPanic, []V{l.str(message)}, nil))
	l.seal(Trap())
	l.cur = ok
}

// failUnless continues only when cond holds, and otherwise fails with the case errorCase of the error the operation
// throws, after drop, when it is not nil, has dropped what the operation was given and does not keep on that path.
func (l *Lowerer) failUnless(cond V, errorCase int, drop func()) {
	ok := l.b.NewBlock()
	bad := l.b.NewBlock()
	l.b.Terminate(l.cur, Branch(cond, ok, bad))
	l.cur = bad
	if drop != nil {
		drop()
	}
	l.seal(Jump(l.failure(errorCase, NoReg)))
	l.cur = ok
}

// countedLoop runs body once for each index from 0 to below limit.
func (l *Lowerer) countedLoop(limit V, body func(index V)) {
	index := l.f.NewSlot(KindScalar)
	bound := l.f.NewSlot(KindScalar)
	top := l.b.NewBlock()
	inside := l.b.NewBlock()
	exit := l.b.NewBlock()
	l.emit(StoreSlot(index, l.konst(0), false))
	l.emit(StoreSlot(bound, limit, false))
	l.goTo(top)
	i := l.load(index)
	n := l.load(bound)
	l.b.Terminate(l.cur, Branch(l.compare(CmpLt, i, n, true), inside, exit))
	l.cur = inside
	body(l.load(index))
	next := l.reg(KindScalar)
	l.emit(Bin(next, BinAdd, l.load(index), l.konst(1), true, NoBlock))
	l.emit(StoreSlot(index, next, false))
	l.goTo(top)
	l.cur = exit
}

// call is a call being lowered: its node, which is where a refusal is reported, and what the checker found out about it.
// The node's own fields and methods are promoted next to the call's. Receiver is the object it is made on, found once.
type call struct {
	ast.CallExpr
	*ast.CallInfo
	Receiver ast.Expr
}

// call is the call a node is, with what its module's checker found out about it.
func (l *Lowerer) call(c ast.CallExpr) call {
	info := l.typings.Of(c.T.Module).Call(c)
	return call{c, info, c.ReceiverFor(info.Kind)}
}

// elemPlace is the address of a[i] that a call taking it by address is given: the element of a primitive that a
// subscript names, lent for as long as the primitive is not changed, or for a byte array a slot that holds the element
// (arrayBox.place).
func (l *Lowerer) elemPlace(e ast.IndexExpr) V {
	coll := l.read(e.Obj())
	index := l.indexOf(e)
	if l.isArray(l.ty(e.Obj())) {
		return l.arrayOf(e.Position(), l.ty(e.Obj())).place(coll, index)
	}
	return l.elemEntry(e.Position(), l.ty(e.Obj()), coll, index)
}

func (l *Lowerer) indexOf(e ast.IndexExpr) V {
	bt := ast.Content(l.binding.Apply(l.ty(e.Obj()))).(*ast.BuiltinTy)
	return l.converted(e.Args().At(0), bt.Index())
}

func (l *Lowerer) isArray(collection ast.Ty) bool {
	return ast.Content(l.binding.Apply(collection)).(*ast.BuiltinTy).Kind == ast.BuiltinArray
}

// elemEntry is the address of an element of a map, a list or a pool; an array's goes through arrayOf.
func (l *Lowerer) elemEntry(site ast.Pos, collection ast.Ty, coll, index V) V {
	bt := ast.Content(l.binding.Apply(collection)).(*ast.BuiltinTy)
	var symbol Rt
	switch bt.Kind {
	case ast.BuiltinMap:
		symbol = RtMapElem
	case ast.BuiltinList:
		symbol = RtListElem
	case ast.BuiltinPool:
		symbol = RtPoolElem
	default:
		panic(diag.Internal("lower: %s has no elements to address", bt.Name()))
	}
	return l.entry(site, symbol, KindScalar, coll, index)
}

func (l *Lowerer) VisitIndexExpr(e ast.IndexExpr) V {
	coll := l.read(e.Obj())
	index := l.indexOf(e)
	if l.isArray(l.ty(e.Obj())) {
		return l.arrayOf(e.Position(), l.ty(e.Obj())).load(coll, index)
	}
	addr := l.elemEntry(e.Position(), l.ty(e.Obj()), coll, index)
	d := l.reg(l.kind(l.ty(e.Expr())))
	l.emit(LoadAt(d, addr))
	return d
}

// assignElement stores into a[i]: the collection and the index are evaluated, then the value, and only then is the
// element's address taken, so a value that changes the collection cannot leave the address stale.
func (l *Lowerer) assignElement(t ast.IndexExpr, value ast.Expr) {
	coll := l.expr(t.Obj())
	index := l.indexOf(t)
	v := l.expr(value)
	if l.isArray(l.ty(t.Obj())) {
		l.arrayOf(t.Position(), l.ty(t.Obj())).store(coll, index, v)
		return
	}
	addr := l.elemEntry(t.Position(), l.ty(t.Obj()), coll, index)
	l.dropAt(addr, l.ty(t.Expr()))
	l.emit(StoreAt(addr, v))
}

// intrinsic lowers a call of a built-in method into the runtime entries of its primitive.
func (l *Lowerer) intrinsic(c call) V {
	var recv V
	if ownsOperands(c.Target.Intrinsic) {
		return l.intrinsicCall(c, recv, nil)
	}
	if !c.Receiver.IsNil() {
		recv = l.read(c.Receiver)
	}
	base := len(l.args)
	for i, a := range c.Ordered {
		l.push(l.converted(a, c.Pass.Specs[i].Type))
	}
	result := l.intrinsicCall(c, recv, l.args[base:])
	l.args = l.args[:base]
	return result
}

// lowerArrayCreate is an array of count zero elements. An element with no zero value is refused unless count is 0, so
// only an array of one with a zero value has slots to fill.
// array is the runtime of the array a call makes or is made on.
func (l *Lowerer) array(c call) arrayBox {
	if c.Receiver.IsNil() {
		return l.arrayOf(c.CallExpr.Position(), l.ty(c.CallExpr.Expr()))
	}
	return l.arrayOf(c.CallExpr.Position(), l.ty(c.Receiver))
}

func (l *Lowerer) lowerArrayCreate(c call, _ V, args []V) V {
	a := l.array(c)
	out := a.newBox()
	if ast.HasZero(a.elem()) {
		l.countedLoop(args[0], func(i V) {
			a.insert(out, i, l.konst(0))
		})
	}
	return out
}

func (l *Lowerer) lowerArrayFilled(c call, _ V, args []V) V {
	return l.arrayFilled(c, args[0], args[1])
}

func (l *Lowerer) lowerArrayCount(c call, recv V, _ []V) V {
	return l.array(c).length(recv)
}

func (l *Lowerer) lowerArrayIsEmpty(c call, recv V, _ []V) V {
	return l.isZero(l.array(c).length(recv))
}

// lowerArrayGet is a copy of the element at index, which fails with indexOutOfBounds past the end, as maxon's get does.
func (l *Lowerer) lowerArrayGet(c call, recv V, args []V) V {
	a := l.array(c)
	a.inBounds(recv, args[0], nil)
	return l.copyValue(c.CallExpr.Position(), a.load(recv, args[0]), a.elem())
}

// lowerArraySet replaces the element at index, and fails with indexOutOfBounds past the end, dropping the value it was
// given, as maxon's set does.
func (l *Lowerer) lowerArraySet(c call, recv V, args []V) V {
	a := l.array(c)
	a.inBounds(recv, args[0], func() { l.dropValue(args[1], a.elem()) })
	a.store(recv, args[0], args[1])
	return l.konst(0)
}

func (l *Lowerer) lowerArrayPush(c call, recv V, args []V) V {
	l.array(c).push(recv, args[0])
	return l.konst(0)
}

// lowerArrayAppend pushes a copy of each element of other, which the call only reads.
func (l *Lowerer) lowerArrayAppend(c call, recv V, args []V) V {
	l.array(c).appendCopy(recv, args[0])
	return l.konst(0)
}

// lowerArrayInsert inserts at an index above the count at the end, as maxon's insert clamps it.
func (l *Lowerer) lowerArrayInsert(c call, recv V, args []V) V {
	a := l.array(c)
	n := a.length(recv)
	at := l.f.NewSlot(KindScalar)
	l.emit(StoreSlot(at, args[0], false))
	clamp := l.b.NewBlock()
	insert := l.b.NewBlock()
	l.b.Terminate(l.cur, Branch(l.compare(CmpGt, args[0], n, true), clamp, insert))
	l.cur = clamp
	l.emit(StoreSlot(at, n, false))
	l.goTo(insert)
	a.insert(recv, l.load(at), args[1])
	return l.konst(0)
}

func (l *Lowerer) lowerArrayRemove(c call, recv V, args []V) V {
	a := l.array(c)
	a.inBounds(recv, args[0], nil)
	return a.remove(recv, args[0])
}

func (l *Lowerer) lowerArrayDropEmpty(c call, recv V, _ []V) V {
	return l.dropEmpty(c, l.array(c).length(recv), recv)
}

func (l *Lowerer) lowerArrayPop(c call, recv V, _ []V) V {
	a := l.array(c)
	n := a.length(recv)
	l.failUnless(l.nonZero(n), ast.ErrorIndexOutOfBounds, nil)
	last := l.reg(KindScalar)
	l.emit(Bin(last, BinSub, n, l.konst(1), true, NoBlock))
	return a.remove(recv, last)
}

func (l *Lowerer) lowerMapCreate(c call, _ V, _ []V) V {
	return l.entry(c.CallExpr.Position(), RtMapNew, KindManaged)
}

func (l *Lowerer) lowerMapCount(c call, recv V, _ []V) V {
	return l.entry(c.CallExpr.Position(), RtMapLen, KindScalar, recv)
}

func (l *Lowerer) lowerMapIsEmpty(c call, recv V, _ []V) V {
	return l.isZero(l.entry(c.CallExpr.Position(), RtMapLen, KindScalar, recv))
}

func (l *Lowerer) lowerMapContains(c call, recv V, args []V) V {
	return l.entry(c.CallExpr.Position(), RtMapHas, KindScalar, recv, args[0])
}

// lowerMapInsert adds a key the map does not have, and fails with keyAlreadyExists for one it has, dropping the key and
// the value it was given.
func (l *Lowerer) lowerMapInsert(c call, recv V, args []V) V {
	bt := ast.Content(l.binding.Apply(l.ty(c.Receiver))).(*ast.BuiltinTy)
	has := l.entry(c.CallExpr.Position(), RtMapHas, KindScalar, recv, args[0])
	l.failUnless(l.isZero(has), ast.ErrorKeyAlreadyExists, func() {
		l.dropValue(args[0], bt.Index())
		l.dropValue(args[1], bt.Element())
	})
	l.entryVoid(c.CallExpr.Position(), RtMapInsert, recv, args[0], args[1])
	return l.konst(0)
}

func (l *Lowerer) lowerMapUpsert(c call, recv V, args []V) V {
	l.entryVoid(c.CallExpr.Position(), RtMapInsert, recv, args[0], args[1])
	return l.konst(0)
}

func (l *Lowerer) lowerMapReplace(c call, recv V, args []V) V {
	return l.mapReplace(c, recv, args[0], args[1])
}

func (l *Lowerer) lowerMapDropEmpty(c call, recv V, _ []V) V {
	return l.dropEmpty(c, l.entry(c.CallExpr.Position(), RtMapLen, KindScalar, recv), recv)
}

// lowerMapRemove drops the value of a key the map has, and gives whether it had one.
func (l *Lowerer) lowerMapRemove(c call, recv V, args []V) V {
	value := ast.Content(l.binding.Apply(l.ty(c.Receiver))).(*ast.BuiltinTy).Element()
	present := l.entry(c.CallExpr.Position(), RtMapHas, KindScalar, recv, args[0])
	remove := l.b.NewBlock()
	end := l.b.NewBlock()
	l.b.Terminate(l.cur, Branch(present, remove, end))
	l.cur = remove
	l.dropValue(l.entry(c.CallExpr.Position(), RtMapRemove, l.kind(value), recv, args[0]), value)
	l.goTo(end)
	return present
}

func (l *Lowerer) lowerListCreate(c call, _ V, _ []V) V {
	return l.entry(c.CallExpr.Position(), RtListNew, KindManaged)
}

func (l *Lowerer) lowerListCount(c call, recv V, _ []V) V {
	return l.entry(c.CallExpr.Position(), RtListLen, KindScalar, recv)
}

func (l *Lowerer) lowerListIsEmpty(c call, recv V, _ []V) V {
	return l.isZero(l.entry(c.CallExpr.Position(), RtListLen, KindScalar, recv))
}

func (l *Lowerer) lowerListContains(c call, recv V, args []V) V {
	return l.entry(c.CallExpr.Position(), RtListContains, KindScalar, recv, args[0])
}

func (l *Lowerer) lowerListPrepend(c call, recv V, args []V) V {
	return l.entry(c.CallExpr.Position(), RtListPushFront, KindScalar, recv, args[0])
}

func (l *Lowerer) lowerListAppend(c call, recv V, args []V) V {
	return l.entry(c.CallExpr.Position(), RtListPushBack, KindScalar, recv, args[0])
}

func (l *Lowerer) lowerListRemoveFirst(c call, recv V, _ []V) V {
	return l.removeEnd(c, RtListPopFront, recv)
}

func (l *Lowerer) lowerListRemoveLast(c call, recv V, _ []V) V {
	return l.removeEnd(c, RtListPopBack, recv)
}

func (l *Lowerer) lowerListFirst(c call, recv V, _ []V) V {
	return l.endHandle(l.entry(c.CallExpr.Position(), RtListFirst, KindScalar, recv))
}

func (l *Lowerer) lowerListLast(c call, recv V, _ []V) V {
	return l.endHandle(l.entry(c.CallExpr.Position(), RtListLast, KindScalar, recv))
}

func (l *Lowerer) lowerListNext(c call, recv V, args []V) V {
	return l.endHandle(l.entry(c.CallExpr.Position(), RtListNext, KindScalar, recv, args[0]))
}

func (l *Lowerer) lowerListPrev(c call, recv V, args []V) V {
	return l.endHandle(l.entry(c.CallExpr.Position(), RtListPrev, KindScalar, recv, args[0]))
}

func (l *Lowerer) lowerListRemove(c call, recv V, args []V) V {
	return l.removeHandle(c, RtListContains, RtListRemove, recv, args[0])
}

func (l *Lowerer) lowerListDropEmpty(c call, recv V, _ []V) V {
	return l.dropEmpty(c, l.entry(c.CallExpr.Position(), RtListLen, KindScalar, recv), recv)
}

func (l *Lowerer) lowerListMoveToFront(c call, recv V, args []V) V {
	l.entryVoid(c.CallExpr.Position(), RtListMoveToFront, recv, args[0])
	return l.konst(0)
}

func (l *Lowerer) lowerPoolCreate(c call, _ V, _ []V) V {
	return l.entry(c.CallExpr.Position(), RtPoolNew, KindManaged)
}

func (l *Lowerer) lowerPoolCount(c call, recv V, _ []V) V {
	return l.entry(c.CallExpr.Position(), RtPoolLen, KindScalar, recv)
}

func (l *Lowerer) lowerPoolIsEmpty(c call, recv V, _ []V) V {
	return l.isZero(l.entry(c.CallExpr.Position(), RtPoolLen, KindScalar, recv))
}

func (l *Lowerer) lowerPoolContains(c call, recv V, args []V) V {
	return l.entry(c.CallExpr.Position(), RtPoolContains, KindScalar, recv, args[0])
}

func (l *Lowerer) lowerPoolInsert(c call, recv V, args []V) V {
	return l.entry(c.CallExpr.Position(), RtPoolInsert, KindScalar, recv, args[0])
}

func (l *Lowerer) lowerPoolDropEmpty(c call, recv V, _ []V) V {
	return l.dropEmpty(c, l.entry(c.CallExpr.Position(), RtPoolLen, KindScalar, recv), recv)
}

func (l *Lowerer) lowerPoolRemove(c call, recv V, args []V) V {
	return l.removeHandle(c, RtPoolContains, RtPoolRemove, recv, args[0])
}

func (l *Lowerer) lowerStringByteLength(c call, recv V, _ []V) V {
	return l.runtime(RtStrLen, KindScalar, recv)
}

func (l *Lowerer) lowerStringByteAt(c call, recv V, args []V) V {
	return l.runtime(RtStrByteAt, KindScalar, recv, args[0])
}

func (l *Lowerer) lowerStringSliceBytes(c call, recv V, args []V) V {
	return l.runtime(RtStrSlice, KindManaged, recv, args[0], args[1])
}

// lowerStringToByteArray is a new byte array of the string's UTF-8 bytes.
func (l *Lowerer) lowerStringToByteArray(c call, recv V, _ []V) V {
	a := l.arrayOf(c.CallExpr.Position(), l.ty(c.CallExpr.Expr()))
	out := a.newBox()
	a.appendText(out, recv)
	return out
}

// lowerStringFrom is a string of a copy of the bytes, which the call only reads.
func (l *Lowerer) lowerStringFrom(c call, _ V, args []V) V {
	return l.arrayOf(c.CallExpr.Position(), c.Pass.Specs[0].Type).text(args[0])
}

// dropEmpty ends a collection of the given length that the call consumes, and traps if an element is left: dropping it
// would drop the element, which a type with a destructor must never be left to do.
func (l *Lowerer) dropEmpty(c call, length, collection V) V {
	l.requireOrTrap(l.isZero(length), dropEmptyMessage)
	l.dropValue(collection, l.ty(c.Receiver))
	return l.konst(0)
}

// moveOutAt reads the value at an address of the type t and leaves the place empty when t is owned, so that the place
// can be written without dropping what was there.
func (l *Lowerer) moveOutAt(addr V, t ast.Ty) V {
	d := l.reg(l.kind(t))
	if l.kind(t) == KindManaged {
		l.emit(MoveAt(d, addr))
	} else {
		l.emit(LoadAt(d, addr))
	}
	return d
}

// exchange moves the new value into a place and gives the value that was there to the caller, so the place is never
// dropped and a type without Droppable can be replaced. The value is computed before the place is addressed, so that
// computing it cannot leave the address stale.
func (l *Lowerer) lowerExchange(c call, _ V, _ []V) V {
	value := l.converted(c.Ordered[1], c.Pass.Specs[1].Type)
	places := len(l.writeBacks)
	addr := l.address(c.Ordered[0])
	old := l.moveOutAt(addr, l.ty(c.CallExpr.Expr()))
	l.emit(StoreAt(addr, value))
	l.writeBack(places)
	return old
}

// mapReplace is Map.upsert that gives back the value the key had: it moves that value out of its entry, and the insert
// then finds the entry empty, keeps its place and drops nothing. A key the map does not have fails with keyNotFound,
// dropping the key and the value it was given.
func (l *Lowerer) mapReplace(c call, m, key, value V) V {
	elem := ast.Content(l.binding.Apply(l.ty(c.Receiver))).(*ast.BuiltinTy).Element()
	bt := ast.Content(l.binding.Apply(l.ty(c.Receiver))).(*ast.BuiltinTy)
	l.failUnless(l.entry(c.CallExpr.Position(), RtMapHas, KindScalar, m, key), ast.ErrorKeyNotFound, func() {
		l.dropValue(key, bt.Index())
		l.dropValue(value, elem)
	})
	addr := l.entry(c.CallExpr.Position(), RtMapElem, KindScalar, m, key)
	old := l.moveOutAt(addr, elem)
	l.entryVoid(c.CallExpr.Position(), RtMapInsert, m, key, value)
	return old
}

func (l *Lowerer) isZero(v V) V { return l.compare(CmpEq, v, l.konst(0), false) }

func (l *Lowerer) nonZero(v V) V { return l.compare(CmpNe, v, l.konst(0), false) }

// removeEnd moves the element at an end of a list out, and fails with indexOutOfBounds on an empty list, as maxon's
// List does.
func (l *Lowerer) removeEnd(c call, pop Rt, list V) V {
	l.failUnless(l.nonZero(l.entry(c.CallExpr.Position(), RtListLen, KindScalar, list)), ast.ErrorIndexOutOfBounds, nil)
	return l.entry(c.CallExpr.Position(), pop, l.kind(l.ty(c.CallExpr.Expr())), list)
}

// endHandle is the handle of a list's first or last node, or of the node after or before one, which the entry gives
// as 0 when there is none, where the operation fails with indexOutOfBounds.
func (l *Lowerer) endHandle(handle V) V {
	l.failUnless(l.nonZero(handle), ast.ErrorIndexOutOfBounds, nil)
	return handle
}

// removeHandle moves the element a handle names out of a list or a pool, and fails with emptySlot for a handle that
// names none, as maxon's emptied slot does.
func (l *Lowerer) removeHandle(c call, contains, remove Rt, collection, handle V) V {
	l.failUnless(l.entry(c.CallExpr.Position(), contains, KindScalar, collection, handle), ast.ErrorEmptySlot, nil)
	return l.entry(c.CallExpr.Position(), remove, l.kind(l.ty(c.CallExpr.Expr())), collection, handle)
}

// arrayFilled is an array of count copies of value: the value is copied into every slot and then dropped, so that
// every element has an owner of its own.
func (l *Lowerer) arrayFilled(c call, count, value V) V {
	a := l.array(c)
	out := a.newBox()
	l.countedLoop(count, func(i V) {
		a.insert(out, i, l.copyValue(c.CallExpr.Position(), value, a.elem()))
	})
	l.dropValue(value, a.elem())
	return out
}

// requireNative refuses, once for each site, a value of a type that holds a primitive the backend has no code for. A copy
// or a drop of such a value is code that only the interpreter has, as the entries of the primitive itself are.
func (l *Lowerer) requireNative(site ast.Pos, t ast.Ty) {
	var visiting [walkDepthHint]ast.Ty
	if holdsInterpreterOnly(t, visiting[:0]) {
		l.refuse(site, diag.ErrInterpreterOnly, interpreterOnlyMessage)
	}
}

// walkDepthHint is how many records and unions a walk of a type usually has open at once, so its list of them lives on the
// stack.
const walkDepthHint = 8

// holdsInterpreterOnly is whether a value of type t is, or holds at any depth, a map, a list, a pool, a frozen value or a
// cown. visiting are the records and unions being walked, so a type that contains itself through an array ends.
func holdsInterpreterOnly(t ast.Ty, visiting []ast.Ty) bool {
	switch t := t.(type) {
	case *ast.PrimTy, *ast.IntTy, *ast.ConstIntTy, *ast.EnumTy:
		return false
	case *ast.BuiltinTy:
		switch t.Kind {
		case ast.BuiltinHandle:
			return false
		case ast.BuiltinArray:
			return holdsInterpreterOnly(t.Element(), visiting)
		case ast.BuiltinMap, ast.BuiltinList, ast.BuiltinPool, ast.BuiltinFrozen, ast.BuiltinCown:
			return true
		}
	case *ast.RecordTy:
		if slices.Contains(visiting, ast.Ty(t)) {
			return false
		}
		visiting = append(visiting, t)
		for _, f := range t.FieldList() {
			if holdsInterpreterOnly(t.FieldType(f), visiting) {
				return true
			}
		}
		return false
	case *ast.UnionTy:
		if slices.Contains(visiting, ast.Ty(t)) {
			return false
		}
		visiting = append(visiting, t)
		for i := range t.CaseList() {
			for _, f := range t.Payload(i) {
				if holdsInterpreterOnly(f.Type, visiting) {
					return true
				}
			}
		}
		return false
	}
	panic(diag.Internal("lower: %s has no layout the backend knows", t.Name()))
}
