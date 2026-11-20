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
func (l *Lowerer) entry(site ast.Node, symbol Rt, kind IrKind, args ...V) V {
	d := l.reg(kind)
	l.emitEntry(site, d, symbol, args, nil)
	return d
}

// entryVoid calls a runtime entry that returns nothing.
func (l *Lowerer) entryVoid(site ast.Node, symbol Rt, args ...V) {
	l.emitEntry(site, NoReg, symbol, args, nil)
}

func (l *Lowerer) emitEntry(site ast.Node, dst V, symbol Rt, args []V, routine *ast.FuncDecl) {
	if IsNativeEntry(symbol) {
		l.f.UsesArrays = true
	} else {
		l.refuse(site, diag.ErrInterpreterOnly, interpreterOnlyMessage)
	}
	l.emit(l.b.CallRuntime(dst, symbol, args, routine))
}

// refuse notes a place that cannot be compiled yet, once for each site.
func (l *Lowerer) refuse(site ast.Node, code, message string) {
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
	l.emit(Bin(next, BinAdd, l.load(index), l.konst(1), true, false))
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

// elemAddr is the address of a[i]: the element of a primitive that a subscript names, lent for as long as the
// primitive is not changed.
func (l *Lowerer) elemAddr(e *ast.IndexExpr) V {
	coll := l.read(e.Obj)
	index := l.indexOf(e)
	return l.elemEntry(e, e.Obj.Ty(), coll, index)
}

func (l *Lowerer) indexOf(e *ast.IndexExpr) V {
	bt := ast.Content(l.binding.Apply(e.Obj.Ty())).(*ast.BuiltinTy)
	return l.converted(e.Args[0], bt.Index())
}

func (l *Lowerer) elemEntry(site ast.Node, collection ast.Ty, coll, index V) V {
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

func (l *Lowerer) VisitIndexExpr(e *ast.IndexExpr) V {
	addr := l.elemAddr(e)
	d := l.reg(l.kind(e.Ty()))
	l.emit(LoadAt(d, addr))
	return d
}

// assignElement stores into a[i]: the collection and the index are evaluated, then the value, and only then is the
// element's address taken, so a value that changes the collection cannot leave the address stale.
func (l *Lowerer) assignElement(t *ast.IndexExpr, value ast.Expr) {
	coll := l.expr(t.Obj)
	index := l.indexOf(t)
	v := l.expr(value)
	addr := l.elemEntry(t, t.Obj.Ty(), coll, index)
	l.dropAt(addr, t.Ty())
	l.emit(StoreAt(addr, v))
}

// intrinsic lowers a call of a built-in method into the runtime entries of its primitive.
func (l *Lowerer) intrinsic(c *ast.CallExpr) V {
	if c.Target.Intrinsic == ast.IntrinsicExchange {
		return l.exchange(c)
	}
	var recv V
	if c.Receiver != nil {
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

// intrinsicCall lowers a built-in method whose receiver and arguments are lowered.
func (l *Lowerer) intrinsicCall(c *ast.CallExpr, recv V, args []V) V {
	switch c.Target.Intrinsic {
	case ast.IntrinsicArrayNew:
		return l.entry(c, RtArrayNew, KindManaged)
	case ast.IntrinsicArrayFilled:
		return l.arrayFilled(c, args[0], args[1])
	case ast.IntrinsicArrayCount:
		return l.entry(c, RtArrayLen, KindScalar, recv)
	case ast.IntrinsicArrayIsEmpty:
		return l.isZero(l.entry(c, RtArrayLen, KindScalar, recv))
	case ast.IntrinsicArrayAppend:
		l.entryVoid(c, RtArrayInsert, recv, l.entry(c, RtArrayLen, KindScalar, recv), args[0])
		return l.konst(0)
	case ast.IntrinsicArrayInsert:
		l.entryVoid(c, RtArrayInsert, recv, args[1], args[0])
		return l.konst(0)
	case ast.IntrinsicArrayRemove:
		return l.entry(c, RtArrayRemove, l.kind(c.Ty()), recv, args[0])
	case ast.IntrinsicArrayDropEmpty:
		return l.dropEmpty(c, RtArrayLen, recv)
	case ast.IntrinsicArrayPop:
		n := l.entry(c, RtArrayLen, KindScalar, recv)
		return l.optional(l.nonZero(n), func() V {
			last := l.reg(KindScalar)
			l.emit(Bin(last, BinSub, n, l.konst(1), true, false))
			return l.entry(c, RtArrayRemove, l.payloadKind(c.Ty()), recv, last)
		})

	case ast.IntrinsicMapNew:
		return l.entry(c, RtMapNew, KindManaged)
	case ast.IntrinsicMapCount:
		return l.entry(c, RtMapLen, KindScalar, recv)
	case ast.IntrinsicMapIsEmpty:
		return l.isZero(l.entry(c, RtMapLen, KindScalar, recv))
	case ast.IntrinsicMapHas:
		return l.entry(c, RtMapHas, KindScalar, recv, args[0])
	case ast.IntrinsicMapInsert:
		l.entryVoid(c, RtMapInsert, recv, args[0], args[1])
		return l.konst(0)
	case ast.IntrinsicMapReplace:
		return l.mapReplace(c, recv, args[0], args[1])
	case ast.IntrinsicMapDropEmpty:
		return l.dropEmpty(c, RtMapLen, recv)
	case ast.IntrinsicMapRemove:
		return l.optional(l.entry(c, RtMapHas, KindScalar, recv, args[0]), func() V {
			return l.entry(c, RtMapRemove, l.payloadKind(c.Ty()), recv, args[0])
		})

	case ast.IntrinsicListNew:
		return l.entry(c, RtListNew, KindManaged)
	case ast.IntrinsicListCount:
		return l.entry(c, RtListLen, KindScalar, recv)
	case ast.IntrinsicListIsEmpty:
		return l.isZero(l.entry(c, RtListLen, KindScalar, recv))
	case ast.IntrinsicListContains:
		return l.entry(c, RtListContains, KindScalar, recv, args[0])
	case ast.IntrinsicListPushFront:
		return l.entry(c, RtListPushFront, KindScalar, recv, args[0])
	case ast.IntrinsicListPushBack:
		return l.entry(c, RtListPushBack, KindScalar, recv, args[0])
	case ast.IntrinsicListPopFront:
		return l.popped(c, RtListLen, RtListPopFront, recv)
	case ast.IntrinsicListPopBack:
		return l.popped(c, RtListLen, RtListPopBack, recv)
	case ast.IntrinsicListFirst:
		return l.handleOrNone(l.entry(c, RtListFirst, KindScalar, recv))
	case ast.IntrinsicListLast:
		return l.handleOrNone(l.entry(c, RtListLast, KindScalar, recv))
	case ast.IntrinsicListNext:
		return l.handleOrNone(l.entry(c, RtListNext, KindScalar, recv, args[0]))
	case ast.IntrinsicListPrev:
		return l.handleOrNone(l.entry(c, RtListPrev, KindScalar, recv, args[0]))
	case ast.IntrinsicListRemove:
		return l.optional(l.entry(c, RtListContains, KindScalar, recv, args[0]), func() V {
			return l.entry(c, RtListRemove, l.payloadKind(c.Ty()), recv, args[0])
		})
	case ast.IntrinsicListDropEmpty:
		return l.dropEmpty(c, RtListLen, recv)
	case ast.IntrinsicListMoveToFront:
		l.entryVoid(c, RtListMoveToFront, recv, args[0])
		return l.konst(0)

	case ast.IntrinsicPoolNew:
		return l.entry(c, RtPoolNew, KindManaged)
	case ast.IntrinsicPoolCount:
		return l.entry(c, RtPoolLen, KindScalar, recv)
	case ast.IntrinsicPoolIsEmpty:
		return l.isZero(l.entry(c, RtPoolLen, KindScalar, recv))
	case ast.IntrinsicPoolContains:
		return l.entry(c, RtPoolContains, KindScalar, recv, args[0])
	case ast.IntrinsicPoolInsert:
		return l.entry(c, RtPoolInsert, KindScalar, recv, args[0])
	case ast.IntrinsicPoolDropEmpty:
		return l.dropEmpty(c, RtPoolLen, recv)
	case ast.IntrinsicPoolRemove:
		return l.optional(l.entry(c, RtPoolContains, KindScalar, recv, args[0]), func() V {
			return l.entry(c, RtPoolRemove, l.payloadKind(c.Ty()), recv, args[0])
		})

	case ast.IntrinsicOptionIsSome:
		return l.compare(CmpEq, l.tag(recv), l.konst(ast.OptionSome), false)
	case ast.IntrinsicOptionUnwrap:
		return l.unwrap(c, recv)

	case ast.IntrinsicFreeze, ast.IntrinsicShare, ast.IntrinsicThaw, ast.IntrinsicCown:
		return l.intrinsicSharing(c, args)
	}
	panic(diag.Internal("lower: unhandled intrinsic %d", int(c.Target.Intrinsic)))
}

// unwrap takes the value out of an Option that the call consumes. A payload that is owned leaves the box, which is
// then dropped empty; the box is the call's to drop either way.
func (l *Lowerer) unwrap(c *ast.CallExpr, option V) V {
	l.requireOrTrap(l.compare(CmpEq, l.tag(option), l.konst(ast.OptionSome), false), unwrapNoneMessage)
	kind := l.kind(c.Ty())
	d := l.reg(kind)
	if kind == KindManaged {
		addr := l.reg(KindScalar)
		l.emit(FieldAddr(addr, option, optionPayloadOffset))
		l.emit(MoveAt(d, addr))
	} else {
		l.emit(LoadField(d, option, optionPayloadOffset))
	}
	l.dropValue(option, c.Receiver.Ty())
	return d
}

// dropEmpty ends a collection that the call consumes, and traps if an element is left: dropping it would drop the
// element, which a type with a destructor must never be left to do.
func (l *Lowerer) dropEmpty(c *ast.CallExpr, length Rt, collection V) V {
	l.requireOrTrap(l.isZero(l.entry(c, length, KindScalar, collection)), dropEmptyMessage)
	l.dropValue(collection, c.Receiver.Ty())
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
func (l *Lowerer) exchange(c *ast.CallExpr) V {
	value := l.converted(c.Ordered[1], c.Pass.Specs[1].Type)
	addr := l.address(c.Ordered[0])
	old := l.moveOutAt(addr, c.Ty())
	l.emit(StoreAt(addr, value))
	return old
}

// mapReplace is map.insert that gives back the value the key had: it moves that value out of its entry, and the insert
// then finds the entry empty, keeps its place and drops nothing.
func (l *Lowerer) mapReplace(c *ast.CallExpr, m, key, value V) V {
	elem := ast.Content(l.binding.Apply(c.Receiver.Ty())).(*ast.BuiltinTy).Element()
	old := l.optional(l.entry(c, RtMapHas, KindScalar, m, key), func() V {
		addr := l.entry(c, RtMapElem, KindScalar, m, key)
		return l.moveOutAt(addr, elem)
	})
	l.entryVoid(c, RtMapInsert, m, key, value)
	return old
}

func (l *Lowerer) isZero(v V) V { return l.compare(CmpEq, v, l.konst(0), false) }

func (l *Lowerer) nonZero(v V) V { return l.compare(CmpNe, v, l.konst(0), false) }

// popped is some(element) from the end of a list that has one, and none from an empty list.
func (l *Lowerer) popped(c *ast.CallExpr, length, pop Rt, list V) V {
	n := l.entry(c, length, KindScalar, list)
	return l.optional(l.nonZero(n), func() V {
		return l.entry(c, pop, l.payloadKind(c.Ty()), list)
	})
}

// handleOrNone is some(handle) for a handle that names a node, and none for the 0 that an entry returns for no node.
func (l *Lowerer) handleOrNone(handle V) V {
	return l.optional(l.nonZero(handle), func() V { return handle })
}

// arrayFilled is an array of count copies of value: the value is copied into every slot and then dropped, so that
// every element has an owner of its own.
func (l *Lowerer) arrayFilled(c *ast.CallExpr, count, value V) V {
	out := l.entry(c, RtArrayNew, KindManaged)
	elem := l.binding.Apply(c.Ty()).(*ast.BuiltinTy).Element()
	l.countedLoop(count, func(i V) {
		l.entryVoid(c, RtArrayInsert, out, i, l.copyValue(c, value, elem))
	})
	l.dropValue(value, elem)
	return out
}

// requireNative refuses, once for each site, a value of a type that holds a primitive the backend has no code for. A copy
// or a drop of such a value is code that only the interpreter has, as the entries of the primitive itself are.
func (l *Lowerer) requireNative(site ast.Node, t ast.Ty) {
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
