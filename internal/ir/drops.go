package ir

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"slices"
)

// A scope is where owned values die. The scope of a block holds the locals declared in it, and the scope of a
// statement, or of one branch of a conditional, holds the temporaries it made: values that nothing took ownership of,
// such as the result of a call whose value is only borrowed. Leaving a scope drops what it holds, newest first. A
// jump that leaves several scopes drops all of them.
//
// Which values are temporaries, and which places are moved out of, is not decided here: the ownership pass marks each
// expression whose value changes owner (ast.Expr.IsConsumed), and lowering reads that mark.
type scope struct {
	block bool
	drops []pendingDrop
}

// pendingDrop is a value to drop when its scope ends: what a slot holds, or a register.
type pendingDrop struct {
	inSlot bool
	slot   int
	reg    V
	ty     ast.Ty
}

// pushScope opens a scope, reusing the storage of one closed earlier: a function has a scope for each statement and
// would otherwise allocate for each.
func (l *Lowerer) pushScope(block bool) {
	depth := len(l.scopes)
	if depth < cap(l.scopes) {
		l.scopes = l.scopes[:depth+1]
		l.scopes[depth].drops = l.scopes[depth].drops[:0]
	} else {
		l.scopes = append(l.scopes, scope{})
	}
	l.scopes[depth].block = block
}

// popScope leaves the innermost scope, dropping what it holds.
func (l *Lowerer) popScope() {
	l.unwind(len(l.scopes) - 1)
	l.scopes = l.scopes[:len(l.scopes)-1]
}

// unwind drops what the scopes from depth inwards hold, innermost first, and keeps them open: it is the code of a
// jump out of them, and the code after the jump is still inside.
func (l *Lowerer) unwind(depth int) {
	for i := len(l.scopes) - 1; i >= depth; i-- {
		drops := l.scopes[i].drops
		for _, drop := range slices.Backward(drops) {
			l.emitDrop(drop)
		}
	}
}

func (l *Lowerer) emitDrop(d pendingDrop) {
	if d.inSlot {
		l.emit(&DropSlotInstr{Slot: d.slot, Ty: d.ty})
	} else {
		l.emit(&DropInstr{Src: d.reg, Ty: d.ty})
	}
}

// scoped runs the lowering of one branch of an expression, so the temporaries it makes are dropped on that branch.
func (l *Lowerer) scoped(body func()) {
	l.pushScope(false)
	body()
	l.popScope()
}

// ownLocal makes the block that is open drop a local when it ends. The slot holds nothing until the local is given a
// value, and nothing again once it is moved from, and dropping an empty slot does nothing, so the drop needs no
// knowledge of which path ran.
func (l *Lowerer) ownLocal(slot int, t ast.Ty) {
	if l.kind(t) != KindManaged {
		return
	}
	for i := len(l.scopes) - 1; i >= 0; i-- {
		if l.scopes[i].block {
			l.scopes[i].drops = append(l.scopes[i].drops, pendingDrop{inSlot: true, slot: slot, ty: l.dropType(t)})
			return
		}
	}
	panic(diag.Internal("lower: a local outside any block"))
}

// ownTemp makes the innermost scope drop a fresh value that nothing owns.
func (l *Lowerer) ownTemp(v V, t ast.Ty) {
	top := len(l.scopes) - 1
	l.scopes[top].drops = append(l.scopes[top].drops, pendingDrop{reg: v, ty: l.dropType(t)})
}

// dropValue drops an owned value now.
func (l *Lowerer) dropValue(v V, t ast.Ty) {
	if l.f.Regs[v] == KindManaged {
		l.emit(&DropInstr{Src: v, Ty: l.dropType(t)})
	}
}

// dropAt drops the value held at an address that is about to be assigned: the old value of a field, an element or an
// mutates parameter.
func (l *Lowerer) dropAt(addr V, t ast.Ty) {
	if l.kind(t) == KindManaged {
		l.emit(&DropAtInstr{Addr: addr, Ty: l.dropType(t)})
	}
}

// storeField assigns a field of the type t, dropping its old value first.
func (l *Lowerer) storeField(obj V, index int, t ast.Ty, v V) {
	if l.kind(t) == KindManaged {
		addr := l.reg(KindScalar)
		l.emit(&FieldAddr{Dst: addr, Obj: obj, Offset: WordBytes * index})
		l.dropAt(addr, t)
	}
	l.emit(&StoreField{Obj: obj, Offset: WordBytes * index, Src: v})
}

// fetch reads a slot that held a value only on its way from one place to another, such as the result of a conditional.
// A managed value is moved out, so the slot is empty again and nothing drops it twice.
func (l *Lowerer) fetch(slot int) V {
	d := l.reg(l.f.Slots[slot])
	if l.f.Slots[slot] == KindManaged {
		l.emit(&MoveSlotInstr{Dst: d, Slot: slot})
	} else {
		l.emit(&LoadSlot{Dst: d, Slot: slot})
	}
	return d
}

// dropType is the type a drop names: the type the body writes, with the instance's arguments applied. A value that holds
// something only the interpreter has is dropped by the interpreter alone, so a function that drops one is refused the way
// one that makes it is. The refusal is at the function, and only when nothing else in it is refused, since a function that
// is refused at a place already is not made better by another and its refusals stay in source order.
func (l *Lowerer) dropType(t ast.Ty) ast.Ty {
	ty := l.binding.Apply(t)
	if len(l.f.Refusals) == 0 {
		l.requireNative(l.fn, ty)
	}
	return ty
}
