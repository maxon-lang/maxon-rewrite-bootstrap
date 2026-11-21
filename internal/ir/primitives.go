package ir

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"slices"
)

const (
	unwrapNoneMessage      = "unwrap of an Option that holds no value"
	dropEmptyMessage       = "dropEmpty of a collection that still holds an element"
	interpreterOnlyMessage = "a map, a list, a pool, a frozen value, a cown or a behaviour runs only under the interpreter for now, and the compiler has no code for it; use an array, or run the program with the interpreter"

	// optionPayloadOffset is where an Option's value sits in its box, after the case index.
	optionPayloadOffset = WordBytes
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

// union is a new box of a union value: the case index, then the payload.
func (l *Lowerer) union(tag int, payload ...V) V {
	p := l.reg(KindManaged)
	l.emit(Alloc(p, BoxBytes(len(payload), true)))
	t := l.konst(int64(tag))
	l.emit(StoreField(p, TagOffset, t))
	for i, v := range payload {
		l.emit(StoreField(p, WordBytes+WordBytes*i, v))
	}
	return p
}

// optional is an Option value: some of what value computes when present holds, none otherwise. value runs only on the
// path where it is present, because the entry it calls traps on a miss.
func (l *Lowerer) optional(present V, value func() V) V {
	result := l.f.NewSlot(KindManaged)
	some := l.b.NewBlock()
	none := l.b.NewBlock()
	end := l.b.NewBlock()
	l.b.Terminate(l.cur, Branch(present, some, none))
	l.cur = some
	v := value()
	l.emit(StoreSlot(result, l.union(ast.OptionSome, v), false))
	l.goTo(end)
	l.cur = none
	l.emit(StoreSlot(result, l.union(ast.OptionNone), false))
	l.goTo(end)
	return l.fetch(result)
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

// payloadKind is the kind of the value an Option type holds.
func (l *Lowerer) payloadKind(option ast.Ty) IrKind {
	u, ok := l.binding.Apply(option).(*ast.UnionTy)
	if !ok {
		panic(diag.Internal("lower: %s is not an Option", option.Name()))
	}
	return kindOf(u.Payload(ast.OptionSome)[0].Type)
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

// elemAddr is the address of a[i]: the element of a primitive that a subscript names, lent for as long as the
// primitive is not changed.
func (l *Lowerer) elemAddr(e ast.IndexExpr) V {
	coll := l.read(e.Obj())
	index := l.indexOf(e)
	return l.elemEntry(e.Position(), l.ty(e.Obj()), coll, index)
}

func (l *Lowerer) indexOf(e ast.IndexExpr) V {
	bt := ast.Content(l.binding.Apply(l.ty(e.Obj()))).(*ast.BuiltinTy)
	return l.converted(e.Args().At(0), bt.Index())
}

func (l *Lowerer) elemEntry(site ast.Pos, collection ast.Ty, coll, index V) V {
	bt := ast.Content(l.binding.Apply(collection)).(*ast.BuiltinTy)
	var symbol Rt
	switch bt.Kind {
	case ast.BuiltinArray:
		symbol = RtArrayElem
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
	addr := l.elemAddr(e)
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

func (l *Lowerer) lowerArrayNew(c call, _ V, _ []V) V {
	return l.entry(c.CallExpr.Position(), RtArrayNew, KindManaged)
}

func (l *Lowerer) lowerArrayFilled(c call, _ V, args []V) V {
	return l.arrayFilled(c, args[0], args[1])
}

func (l *Lowerer) lowerArrayCount(c call, recv V, _ []V) V {
	return l.entry(c.CallExpr.Position(), RtArrayLen, KindScalar, recv)
}

func (l *Lowerer) lowerArrayIsEmpty(c call, recv V, _ []V) V {
	return l.isZero(l.entry(c.CallExpr.Position(), RtArrayLen, KindScalar, recv))
}

func (l *Lowerer) lowerArrayAppend(c call, recv V, args []V) V {
	l.entryVoid(c.CallExpr.Position(), RtArrayInsert, recv, l.entry(c.CallExpr.Position(), RtArrayLen, KindScalar, recv), args[0])
	return l.konst(0)
}

func (l *Lowerer) lowerArrayInsert(c call, recv V, args []V) V {
	l.entryVoid(c.CallExpr.Position(), RtArrayInsert, recv, args[1], args[0])
	return l.konst(0)
}

func (l *Lowerer) lowerArrayRemove(c call, recv V, args []V) V {
	return l.entry(c.CallExpr.Position(), RtArrayRemove, l.kind(l.ty(c.CallExpr.Expr())), recv, args[0])
}

func (l *Lowerer) lowerArrayDropEmpty(c call, recv V, _ []V) V {
	return l.dropEmpty(c, RtArrayLen, recv)
}

func (l *Lowerer) lowerArrayPop(c call, recv V, _ []V) V {
	n := l.entry(c.CallExpr.Position(), RtArrayLen, KindScalar, recv)
	return l.optional(l.nonZero(n), func() V {
		last := l.reg(KindScalar)
		l.emit(Bin(last, BinSub, n, l.konst(1), true, NoBlock))
		return l.entry(c.CallExpr.Position(), RtArrayRemove, l.payloadKind(l.ty(c.CallExpr.Expr())), recv, last)
	})
}

func (l *Lowerer) lowerMapNew(c call, _ V, _ []V) V {
	return l.entry(c.CallExpr.Position(), RtMapNew, KindManaged)
}

func (l *Lowerer) lowerMapCount(c call, recv V, _ []V) V {
	return l.entry(c.CallExpr.Position(), RtMapLen, KindScalar, recv)
}

func (l *Lowerer) lowerMapIsEmpty(c call, recv V, _ []V) V {
	return l.isZero(l.entry(c.CallExpr.Position(), RtMapLen, KindScalar, recv))
}

func (l *Lowerer) lowerMapHas(c call, recv V, args []V) V {
	return l.entry(c.CallExpr.Position(), RtMapHas, KindScalar, recv, args[0])
}

func (l *Lowerer) lowerMapInsert(c call, recv V, args []V) V {
	l.entryVoid(c.CallExpr.Position(), RtMapInsert, recv, args[0], args[1])
	return l.konst(0)
}

func (l *Lowerer) lowerMapReplace(c call, recv V, args []V) V {
	return l.mapReplace(c, recv, args[0], args[1])
}

func (l *Lowerer) lowerMapDropEmpty(c call, recv V, _ []V) V {
	return l.dropEmpty(c, RtMapLen, recv)
}

func (l *Lowerer) lowerMapRemove(c call, recv V, args []V) V {
	return l.optional(l.entry(c.CallExpr.Position(), RtMapHas, KindScalar, recv, args[0]), func() V {
		return l.entry(c.CallExpr.Position(), RtMapRemove, l.payloadKind(l.ty(c.CallExpr.Expr())), recv, args[0])
	})
}

func (l *Lowerer) lowerListNew(c call, _ V, _ []V) V {
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

func (l *Lowerer) lowerListPushFront(c call, recv V, args []V) V {
	return l.entry(c.CallExpr.Position(), RtListPushFront, KindScalar, recv, args[0])
}

func (l *Lowerer) lowerListPushBack(c call, recv V, args []V) V {
	return l.entry(c.CallExpr.Position(), RtListPushBack, KindScalar, recv, args[0])
}

func (l *Lowerer) lowerListPopFront(c call, recv V, _ []V) V {
	return l.popped(c, RtListLen, RtListPopFront, recv)
}

func (l *Lowerer) lowerListPopBack(c call, recv V, _ []V) V {
	return l.popped(c, RtListLen, RtListPopBack, recv)
}

func (l *Lowerer) lowerListFirst(c call, recv V, _ []V) V {
	return l.handleOrNone(l.entry(c.CallExpr.Position(), RtListFirst, KindScalar, recv))
}

func (l *Lowerer) lowerListLast(c call, recv V, _ []V) V {
	return l.handleOrNone(l.entry(c.CallExpr.Position(), RtListLast, KindScalar, recv))
}

func (l *Lowerer) lowerListNext(c call, recv V, args []V) V {
	return l.handleOrNone(l.entry(c.CallExpr.Position(), RtListNext, KindScalar, recv, args[0]))
}

func (l *Lowerer) lowerListPrev(c call, recv V, args []V) V {
	return l.handleOrNone(l.entry(c.CallExpr.Position(), RtListPrev, KindScalar, recv, args[0]))
}

func (l *Lowerer) lowerListRemove(c call, recv V, args []V) V {
	return l.optional(l.entry(c.CallExpr.Position(), RtListContains, KindScalar, recv, args[0]), func() V {
		return l.entry(c.CallExpr.Position(), RtListRemove, l.payloadKind(l.ty(c.CallExpr.Expr())), recv, args[0])
	})
}

func (l *Lowerer) lowerListDropEmpty(c call, recv V, _ []V) V {
	return l.dropEmpty(c, RtListLen, recv)
}

func (l *Lowerer) lowerListMoveToFront(c call, recv V, args []V) V {
	l.entryVoid(c.CallExpr.Position(), RtListMoveToFront, recv, args[0])
	return l.konst(0)
}

func (l *Lowerer) lowerPoolNew(c call, _ V, _ []V) V {
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
	return l.dropEmpty(c, RtPoolLen, recv)
}

func (l *Lowerer) lowerPoolRemove(c call, recv V, args []V) V {
	return l.optional(l.entry(c.CallExpr.Position(), RtPoolContains, KindScalar, recv, args[0]), func() V {
		return l.entry(c.CallExpr.Position(), RtPoolRemove, l.payloadKind(l.ty(c.CallExpr.Expr())), recv, args[0])
	})
}

func (l *Lowerer) lowerOptionIsSome(c call, recv V, _ []V) V {
	return l.compare(CmpEq, l.tag(recv), l.konst(ast.OptionSome), false)
}

func (l *Lowerer) lowerOptionUnwrap(c call, recv V, _ []V) V {
	return l.unwrap(c, recv)
}

func (l *Lowerer) lowerStringByteCount(c call, recv V, _ []V) V {
	return l.runtime(RtStrLen, KindScalar, recv)
}

func (l *Lowerer) lowerStringByteAt(c call, recv V, args []V) V {
	return l.runtime(RtStrByteAt, KindScalar, recv, args[0])
}

func (l *Lowerer) lowerStringSlice(c call, recv V, args []V) V {
	return l.runtime(RtStrSlice, KindManaged, recv, args[0], args[1])
}

func (l *Lowerer) lowerBytesNew(c call, _ V, _ []V) V {
	return l.entry(c.CallExpr.Position(), RtBytesNew, KindManaged)
}

func (l *Lowerer) lowerBytesCount(c call, recv V, _ []V) V {
	return l.entry(c.CallExpr.Position(), RtBytesLen, KindScalar, recv)
}

func (l *Lowerer) lowerBytesIsEmpty(c call, recv V, _ []V) V {
	return l.isZero(l.entry(c.CallExpr.Position(), RtBytesLen, KindScalar, recv))
}

func (l *Lowerer) lowerBytesByteAt(c call, recv V, args []V) V {
	return l.entry(c.CallExpr.Position(), RtBytesGet, KindScalar, recv, args[0])
}

func (l *Lowerer) lowerBytesSetByte(c call, recv V, args []V) V {
	l.entryVoid(c.CallExpr.Position(), RtBytesSet, recv, args[0], args[1])
	return l.konst(0)
}

func (l *Lowerer) lowerBytesPush(c call, recv V, args []V) V {
	l.entryVoid(c.CallExpr.Position(), RtBytesPush, recv, args[0])
	return l.konst(0)
}

func (l *Lowerer) lowerBytesAppend(c call, recv V, args []V) V {
	l.entryVoid(c.CallExpr.Position(), RtBytesAppend, recv, args[0])
	return l.konst(0)
}

func (l *Lowerer) lowerBytesAppendString(c call, recv V, args []V) V {
	l.entryVoid(c.CallExpr.Position(), RtBytesAppendStr, recv, args[0])
	return l.konst(0)
}

func (l *Lowerer) lowerBytesToString(c call, recv V, _ []V) V {
	return l.entry(c.CallExpr.Position(), RtBytesToString, KindManaged, recv)
}

func (l *Lowerer) lowerOsCliArgs(c call, _ V, _ []V) V {
	return l.entry(c.CallExpr.Position(), RtOsCliArgs, KindManaged)
}

func (l *Lowerer) lowerOsGetEnv(c call, _ V, args []V) V {
	raw := l.entry(c.CallExpr.Position(), RtOsGetEnv, KindManaged, args[0])
	return l.optional(l.nonZero(raw), func() V { return raw })
}

func (l *Lowerer) lowerOsReadFile(c call, _ V, args []V) V {
	raw := l.entry(c.CallExpr.Position(), RtOsReadFile, KindManaged, args[0])
	return l.optional(l.nonZero(raw), func() V { return raw })
}

func (l *Lowerer) lowerOsWriteFile(c call, _ V, args []V) V {
	return l.entry(c.CallExpr.Position(), RtOsWriteFile, KindScalar, args[0], args[1])
}

func (l *Lowerer) lowerOsWriteBytes(c call, _ V, args []V) V {
	return l.entry(c.CallExpr.Position(), RtOsWriteBytes, KindScalar, args[0], args[1])
}

func (l *Lowerer) lowerOsRenameFile(c call, _ V, args []V) V {
	return l.entry(c.CallExpr.Position(), RtOsRenameFile, KindScalar, args[0], args[1])
}

func (l *Lowerer) lowerOsRemoveFile(c call, _ V, args []V) V {
	return l.entry(c.CallExpr.Position(), RtOsRemoveFile, KindScalar, args[0])
}

func (l *Lowerer) lowerOsIsDir(c call, _ V, args []V) V {
	return l.entry(c.CallExpr.Position(), RtOsIsDir, KindScalar, args[0])
}

func (l *Lowerer) lowerOsMkdir(c call, _ V, args []V) V {
	return l.entry(c.CallExpr.Position(), RtOsMkdir, KindScalar, args[0])
}

func (l *Lowerer) lowerOsListDir(c call, _ V, args []V) V {
	raw := l.entry(c.CallExpr.Position(), RtOsListDir, KindManaged, args[0])
	return l.optional(l.nonZero(raw), func() V { return raw })
}

func (l *Lowerer) lowerOsRunProcess(c call, _ V, args []V) V {
	raw := l.entry(c.CallExpr.Position(), RtOsRunProcess, KindScalar, args[0])
	return l.optional(l.nonZero(raw), func() V {
		out := l.reg(KindScalar)
		l.emit(Bin(out, BinShr, raw, l.konst(1), true, NoBlock))
		return out
	})
}

// unwrap takes the value out of an Option that the call consumes. A payload that is owned leaves the box, which is
// then dropped empty; the box is the call's to drop either way.
func (l *Lowerer) unwrap(c call, option V) V {
	l.requireOrTrap(l.compare(CmpEq, l.tag(option), l.konst(ast.OptionSome), false), unwrapNoneMessage)
	kind := l.kind(l.ty(c.CallExpr.Expr()))
	d := l.reg(kind)
	if kind == KindManaged {
		addr := l.reg(KindScalar)
		l.emit(FieldAddr(addr, option, optionPayloadOffset))
		l.emit(MoveAt(d, addr))
	} else {
		l.emit(LoadField(d, option, optionPayloadOffset))
	}
	l.dropValue(option, l.ty(c.Receiver))
	return d
}

// dropEmpty ends a collection that the call consumes, and traps if an element is left: dropping it would drop the
// element, which a type with a destructor must never be left to do.
func (l *Lowerer) dropEmpty(c call, length Rt, collection V) V {
	l.requireOrTrap(l.isZero(l.entry(c.CallExpr.Position(), length, KindScalar, collection)), dropEmptyMessage)
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
	addr := l.address(c.Ordered[0])
	old := l.moveOutAt(addr, l.ty(c.CallExpr.Expr()))
	l.emit(StoreAt(addr, value))
	return old
}

// mapReplace is map.insert that gives back the value the key had: it moves that value out of its entry, and the insert
// then finds the entry empty, keeps its place and drops nothing.
func (l *Lowerer) mapReplace(c call, m, key, value V) V {
	elem := ast.Content(l.binding.Apply(l.ty(c.Receiver))).(*ast.BuiltinTy).Element()
	old := l.optional(l.entry(c.CallExpr.Position(), RtMapHas, KindScalar, m, key), func() V {
		addr := l.entry(c.CallExpr.Position(), RtMapElem, KindScalar, m, key)
		return l.moveOutAt(addr, elem)
	})
	l.entryVoid(c.CallExpr.Position(), RtMapInsert, m, key, value)
	return old
}

func (l *Lowerer) isZero(v V) V { return l.compare(CmpEq, v, l.konst(0), false) }

func (l *Lowerer) nonZero(v V) V { return l.compare(CmpNe, v, l.konst(0), false) }

// popped is some(element) from the end of a list that has one, and none from an empty list.
func (l *Lowerer) popped(c call, length, pop Rt, list V) V {
	n := l.entry(c.CallExpr.Position(), length, KindScalar, list)
	return l.optional(l.nonZero(n), func() V {
		return l.entry(c.CallExpr.Position(), pop, l.payloadKind(l.ty(c.CallExpr.Expr())), list)
	})
}

// handleOrNone is some(handle) for a handle that names a node, and none for the 0 that an entry returns for no node.
func (l *Lowerer) handleOrNone(handle V) V {
	return l.optional(l.nonZero(handle), func() V { return handle })
}

// arrayFilled is an array of count copies of value: the value is copied into every slot and then dropped, so that
// every element has an owner of its own.
func (l *Lowerer) arrayFilled(c call, count, value V) V {
	out := l.entry(c.CallExpr.Position(), RtArrayNew, KindManaged)
	elem := l.binding.Apply(l.ty(c.CallExpr.Expr())).(*ast.BuiltinTy).Element()
	l.countedLoop(count, func(i V) {
		l.entryVoid(c.CallExpr.Position(), RtArrayInsert, out, i, l.copyValue(c.CallExpr.Position(), value, elem))
	})
	l.dropValue(value, elem)
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
		case ast.BuiltinHandle, ast.BuiltinBytes:
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
