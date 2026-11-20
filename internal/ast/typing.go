package ast

import (
	"sync"

	"minimaxon/internal/diag"
)

// LocalID is a local of a function: its place in the function's Locals, plus one, so that 0 is no local. A node that
// declares or names a local holds this and not the local, and it is read through the function whose body the node is
// in, which is the one that lists the local.
type LocalID uint32

// ID is the handle of the local in the function that lists it, or 0 for a local no function lists.
func (l *Local) ID() LocalID { return l.id }

// AddLocal lists a local of the function, which gives it its handle.
func (f *FuncDecl) AddLocal(l *Local) {
	f.Locals = append(f.Locals, l)
	l.id = LocalID(len(f.Locals))
}

// LocalAt is the local a handle names in the function, or nil for no local.
func (f *FuncDecl) LocalAt(id LocalID) *Local {
	if id == 0 {
		return nil
	}
	if int(id) > len(f.Locals) {
		panic(diag.Internal("typing: local %d is not among the %d locals of %s", id, len(f.Locals), f.Name))
	}
	return f.Locals[id-1]
}

// OwnerField is a field of the record that a method belongs to, by its index in the record.
func (f *FuncDecl) OwnerField(index int) *FieldInfo {
	rt, ok := f.Owner.(*RecordTy)
	if !ok {
		panic(diag.Internal("typing: %s has no record to take field %d from", f.Name, index))
	}
	return rt.FieldList()[index]
}

// Operand is what lowering needs to know about the operands of a binary operator beyond their value: how to compare and
// divide them.
type Operand uint8

const (
	// OperandWord is a value held as a signed word: a signed integer, a bool, an enum or a handle.
	OperandWord Operand = iota
	// OperandUnsigned is an integer of an unsigned alias.
	OperandUnsigned
	// OperandString is a String, which is compared by its text.
	OperandString
)

// OperandOf is the operand kind of a value of type t.
func OperandOf(t Ty) Operand {
	switch x := t.(type) {
	case *IntTy:
		if x.Unsigned() {
			return OperandUnsigned
		}
	case *PrimTy:
		if x == StringTy {
			return OperandString
		}
	}
	return OperandWord
}

// NameInfo is what a name written as an expression stands for, in 4 bytes: a local variable of the function the name is
// in, a constant, or a field of the receiver. What it says is a handle, and the accessors give the thing itself. The kind
// is the two top bits, which NameKind's values fit, and the rest is the local's LocalID, the field's index in its record,
// or the constant's place in the Typing's list of the constants its names stand for.
type NameInfo struct{ packed uint32 }

const (
	nameKindShift = 30
	nameIndexMask = 1<<nameKindShift - 1
)

func nameOf(kind NameKind, index uint32) NameInfo {
	if index > nameIndexMask {
		panic(diag.Internal("typing: %d is too many locals, fields or constants for one name to point at", index))
	}
	return NameInfo{packed: uint32(kind)<<nameKindShift | index}
}

func (i NameInfo) index() uint32 { return i.packed & nameIndexMask }

// LocalName is the name of a local variable.
func LocalName(l *Local) NameInfo {
	if l.id == 0 {
		panic(diag.Internal("typing: the local '%s' is listed by no function", l.Name))
	}
	return nameOf(NameLocal, uint32(l.id))
}

// FieldName is the name of a field of the receiver's record.
func FieldName(f *FieldInfo) NameInfo { return nameOf(NameSelfField, uint32(f.Index)) }

// Kind is which of the three the name stands for.
func (i NameInfo) Kind() NameKind { return NameKind(i.packed >> nameKindShift) }

// Local is the variable a name of a local stands for, in the function the name is in.
func (i NameInfo) Local(fn *FuncDecl) *Local {
	if i.Kind() != NameLocal {
		panic(diag.Internal("typing: a name that is not a local has no local"))
	}
	return fn.LocalAt(LocalID(i.index()))
}

// FieldIndex is the index of the field a name of the receiver's field stands for, which is all lowering needs of it.
func (i NameInfo) FieldIndex() int {
	if i.Kind() != NameSelfField {
		panic(diag.Internal("typing: a name that is not a field has no field"))
	}
	return int(i.index())
}

// Field is the field a name of the receiver's field stands for, in the method the name is in.
func (i NameInfo) Field(fn *FuncDecl) *FieldInfo {
	if i.Kind() != NameSelfField {
		panic(diag.Internal("typing: a name that is not a field has no field"))
	}
	return fn.OwnerField(int(i.index()))
}

// MemberInfo is what 'object.name' is: a field, a case of an enum or a union, or the name or ordinal of one.
type MemberInfo struct {
	Kind MemberKind
	// CaseIndex is the case of a MemberEnumCase or a MemberUnionCase.
	CaseIndex int32
	// Field is the field of a MemberField.
	Field *FieldInfo
}

// CallInfo is what a call is and what it runs.
type CallInfo struct {
	Kind CallKind
	// CaseIndex is the case a CallUnionCtor makes, of Union.
	CaseIndex int32
	// Target is the function a call of a function, a method or a static function runs; for a call of a generic it is the
	// generic itself, and Generic says which instance.
	Target *FuncDecl
	// Pass is how the callee takes its arguments: the target's own, or for a generic call or one through a bound, the
	// callee's with the type parameters replaced.
	Pass    *Passing
	Generic *GenericCall
	// Union is the union a CallUnionCtor makes.
	Union *UnionTy
	// Ordered are the arguments in the order of the parameters, which labels may have changed.
	Ordered []Expr
	// Drop is the destructor call of a cown that was given 'dropWith:'.
	Drop *CallExpr
}

// ReceiverFor is the object a call of the kind is made on, or nil for a call that has none. A method call, a call through
// a bound and a copy are written 'object.name(...)', so it is the object of the member the call names.
func (c *CallExpr) ReceiverFor(kind CallKind) Expr {
	switch kind {
	case CallMethod, CallBound, CallCopy:
		if m, named := c.Callee.(*MemberExpr); named {
			return m.Obj
		}
	}
	return nil
}

// StructLitInfo is the record a struct literal makes and its field values in the order of the record's fields; a field
// the literal leaves to its default is nil.
type StructLitInfo struct {
	Record  *RecordTy
	Ordered []Expr
}

// PatternInfo is what a case pattern names: the case, and the local each binding declares, 0 for '_'.
type PatternInfo struct {
	CaseIndex int
	Locals    []LocalID
}

// WhenInfo is a behaviour: the function its body became and the variables of the enclosing function it takes.
type WhenInfo struct {
	Fn       *FuncDecl
	Captures []*Capture
}

// Typing is what the check pass decided about the nodes of one module's bodies: the type of every expression, whether the
// ownership pass found it consumed, what each name, member, call, struct literal and pattern stands for, the local that
// each let, for and parameter declares, where each break and continue goes, and the function and captures of each
// behaviour. The nodes keep only what the author wrote.
//
// A node finds its entry by a slot that the module's arena gave it when the node was made (see Slots). What nearly every
// expression has, a type, a consumed flag and the operand kind of a binary operator, is indexed by the expression's own
// slot, so a statement or a declaration pays nothing for it. What only some kinds of expression have is indexed by a slot
// that only the nodes of that kind count (a call has one of 'Call', so the table of calls has as many entries as the
// module has calls, not as many as it has expressions). Slot 0 is no slot, so a node that no arena made has no entry and
// a read of it is an internal error.
//
// It is written only by the checker of its own module: the constants one at a time (each writes the entries of its
// own expressions, and the tables were sized before the first), then the bodies and the ownership pass of the module's
// worker. Every module's check pass completes before the generate pass starts, which is the only reader of another
// module's Typing, so no table is read while it is written.
type Typing struct {
	types    []Ty
	consumed []uint64
	operands []Operand

	names      []NameInfo
	members    []MemberInfo
	calls      []CallInfo
	structLits []StructLitInfo
	patterns   []PatternInfo
	whens      []WhenInfo
	locals     []LocalID
	jumps      []NodeID

	// consts lists the distinct constants that the module's names stand for, which a NameInfo points into. The constants
	// are checked at the same time as each other, so naming one takes the lock.
	constMu sync.Mutex
	consts  []*ConstDecl
	constOf map[*ConstDecl]uint32
}

// Reset sizes the tables for the nodes the module's arena has made so far and empties them.
func (t *Typing) Reset(s Slots) {
	t.types, t.consumed, t.operands = nil, nil, nil
	t.names, t.members, t.calls, t.structLits, t.patterns, t.whens, t.locals, t.jumps = nil, nil, nil, nil, nil, nil, nil, nil
	t.consts, t.constOf = nil, nil
	t.Sync(s)
}

// Sync gives the tables room for the nodes made since they were sized, which is how a node that a phase makes while it
// checks has its entries. It keeps what the tables hold, and is for the one worker that checks the module's bodies: the
// constants are checked at the same time as each other, so nothing may make a node meanwhile.
func (t *Typing) Sync(s Slots) {
	t.types = grow(t.types, s.Expr)
	t.operands = grow(t.operands, s.Expr)
	if words := int(s.Expr)/64 + 1; len(t.consumed) < words {
		t.consumed = append(t.consumed, make([]uint64, words-len(t.consumed))...)
	}
	t.names = grow(t.names, s.Name)
	t.members = grow(t.members, s.Member)
	t.calls = grow(t.calls, s.Call)
	t.structLits = grow(t.structLits, s.StructLit)
	t.patterns = grow(t.patterns, s.Pattern)
	t.whens = grow(t.whens, s.When)
	t.locals = grow(t.locals, s.Local)
	t.jumps = grow(t.jumps, s.Jump)
}

// grow makes the table hold the slots 1 to count.
func grow[T any](table []T, count uint32) []T {
	if want := int(count) + 1; len(table) < want {
		return append(table, make([]T, want-len(table))...)
	}
	return table
}

// at is the entry of a slot, which must be one the table has.
func at[T any](table []T, slot uint32, what string) *T {
	if slot == 0 || int(slot) >= len(table) {
		noEntry(slot, len(table)-1, what)
	}
	return &table[slot]
}

// noEntry is the error for a slot that the table has no entry for, kept out of line so that at is small enough to inline.
//
//go:noinline
func noEntry(slot uint32, entries int, what string) {
	panic(diag.Internal("typing: slot %d of the %s table is not among its %d entries", slot, what, entries))
}

// Type is the type of an expression the checker typed.
func (t *Typing) Type(e Expr) Ty {
	return t.TypeAt(e.Base())
}

// TypeAt is the type of the expression that starts with the base, for code that has asked an expression for its base once
// and reads several things about it.
func (t *Typing) TypeAt(e *ExprBase) Ty {
	ty := *at(t.types, e.Ord, "type")
	if ty == nil {
		noType(e)
	}
	return ty
}

// noType is the error for an expression the checker never typed, kept out of line so that TypeAt is small enough to inline.
//
//go:noinline
func noType(e *ExprBase) {
	panic(diag.Internal("typing: the expression at %d has no type", e.Off))
}

func (t *Typing) SetType(e Expr, ty Ty) { *at(t.types, e.Base().Ord, "type") = ty }

// Consumed is whether the ownership pass found the expression in a position that takes ownership of its value: a binding,
// an assignment, a consumes argument, a return, a field or payload, or a branch of a conditional or a match. A place there
// is moved out of; a fresh value there needs no one to drop it. Lowering reads this to place drops, so the pass is the
// only place that decides what a move is.
func (t *Typing) Consumed(e Expr) bool { return t.ConsumedAt(e.Base()) }

// ConsumedAt is Consumed for an expression's base (see TypeAt).
func (t *Typing) ConsumedAt(e *ExprBase) bool {
	slot := e.Ord
	at(t.types, slot, "consumed")
	return t.consumed[slot/64]&(1<<(slot%64)) != 0
}

func (t *Typing) MarkConsumed(e Expr) {
	slot := e.Base().Ord
	at(t.types, slot, "consumed")
	t.consumed[slot/64] |= 1 << (slot % 64)
}

// Operand is how the operands of a binary operator are compared and divided.
func (t *Typing) Operand(b *BinaryExpr) Operand { return *at(t.operands, b.Ord, "operand") }

func (t *Typing) SetOperand(b *BinaryExpr, o Operand) { *at(t.operands, b.Ord, "operand") = o }

func (t *Typing) Name(n *NameExpr) NameInfo { return *at(t.names, n.Info, "name") }

func (t *Typing) SetName(n *NameExpr, info NameInfo) { *at(t.names, n.Info, "name") = info }

// ConstName is the name of a constant.
func (t *Typing) ConstName(d *ConstDecl) NameInfo {
	t.constMu.Lock()
	defer t.constMu.Unlock()
	slot, listed := t.constOf[d]
	if !listed {
		if t.constOf == nil {
			t.constOf = map[*ConstDecl]uint32{}
		}
		t.consts = append(t.consts, d)
		slot = uint32(len(t.consts))
		t.constOf[d] = slot
	}
	return nameOf(NameConst, slot)
}

// Const is the constant a name of a constant stands for.
func (t *Typing) Const(i NameInfo) *ConstDecl {
	if i.Kind() != NameConst || int(i.index()) > len(t.consts) {
		panic(diag.Internal("typing: a name that is not a constant has no constant"))
	}
	return t.consts[i.index()-1]
}

func (t *Typing) Member(m *MemberExpr) MemberInfo { return *at(t.members, m.Info, "member") }

func (t *Typing) SetMember(m *MemberExpr, info MemberInfo) { *at(t.members, m.Info, "member") = info }

// Call is what a call is. It is the entry itself and not a copy, which a call's size would make worth avoiding: the checker
// fills it in as it learns more about the call, and everything else only reads it. Like the other Edit pointers it is into
// the table, so it is good only until the next Sync.
func (t *Typing) Call(c *CallExpr) *CallInfo { return at(t.calls, c.Info, "call") }

func (t *Typing) StructLit(s *StructLitExpr) StructLitInfo {
	return *at(t.structLits, s.Info, "struct literal")
}

func (t *Typing) SetStructLit(s *StructLitExpr, info StructLitInfo) {
	*at(t.structLits, s.Info, "struct literal") = info
}

func (t *Typing) Pattern(p *Pattern) PatternInfo { return *at(t.patterns, p.Info, "pattern") }

// EditPattern is the entry of a pattern, for the checker to fill in.
func (t *Typing) EditPattern(p *Pattern) *PatternInfo { return at(t.patterns, p.Info, "pattern") }

func (t *Typing) When(w *WhenStmt) WhenInfo { return *at(t.whens, w.Info, "behaviour") }

// EditWhen is the entry of a behaviour, for the checker to fill in.
func (t *Typing) EditWhen(w *WhenStmt) *WhenInfo { return at(t.whens, w.Info, "behaviour") }

// LetLocal, ForLocal and ParamLocal are the local a let, a for or a parameter declares, in the function it is in: 0 for a
// parameter named '_'.
func (t *Typing) LetLocal(l *LetStmt) LocalID { return *at(t.locals, l.Local, "local") }

func (t *Typing) ForLocal(f *ForStmt) LocalID { return *at(t.locals, f.Local, "local") }

func (t *Typing) ParamLocal(p *Param) LocalID { return *at(t.locals, p.Local, "local") }

func (t *Typing) SetLetLocal(l *LetStmt, local *Local) { *at(t.locals, l.Local, "local") = local.id }

func (t *Typing) SetForLocal(f *ForStmt, local *Local) { *at(t.locals, f.Local, "local") = local.id }

func (t *Typing) SetParamLocal(p *Param, local *Local) { *at(t.locals, p.Local, "local") = local.id }

// BreakTarget and ContinueTarget are the loop or match that a break or a continue leaves or repeats.
func (t *Typing) BreakTarget(b *BreakStmt) NodeID { return *at(t.jumps, b.Jump, "jump") }

func (t *Typing) ContinueTarget(c *ContinueStmt) NodeID { return *at(t.jumps, c.Jump, "jump") }

func (t *Typing) SetBreakTarget(b *BreakStmt, target NodeID) { *at(t.jumps, b.Jump, "jump") = target }

func (t *Typing) SetContinueTarget(c *ContinueStmt, target NodeID) {
	*at(t.jumps, c.Jump, "jump") = target
}

// IsStorage is whether an expression reads a value that already lives somewhere, in a variable, a field or an element, and
// so borrows it, as opposed to making a new value. It asks about four members of Expr, so it is a type switch and not a
// visitor.
func (t *Typing) IsStorage(e Expr) bool {
	switch e := e.(type) {
	case *NameExpr:
		kind := t.Name(e).Kind()
		return kind == NameLocal || kind == NameSelfField
	case *SelfExpr, *IndexExpr:
		return true
	case *MemberExpr:
		return t.Member(e).Kind == MemberField
	}
	return false
}

// Resolved is the function a call runs once the type parameters of the code it is in are bound: the target itself, or
// the instance of a generic one, or for a call through a bound the method of the type that stands in for the
// parameter.
func (t *Typing) Resolved(c *CallExpr, b Binding) *FuncDecl {
	info := t.Call(c)
	switch info.Kind {
	case CallFunction, CallStatic, CallMethod:
		if len(info.Target.Generics) == 0 {
			return info.Target
		}
		return info.Target.Instance(b.applyAll(info.Generic.TypeArgs))
	case CallBound:
		// A frozen receiver lends its contents, so the method is the one of the type it holds.
		receiver := b.Apply(t.Type(c.ReceiverFor(info.Kind)))
		recv, ok := Content(receiver).(NominalTy)
		if !ok {
			panic(diag.Internal("a call through a bound has a receiver of %s, which has no methods", receiver.Name()))
		}
		method, found := recv.MethodTable()[info.Generic.Method.Name]
		if !found {
			panic(diag.Internal("%s has no method '%s' for the bound that declares it", recv.Name(), info.Generic.Method.Name))
		}
		if len(method.Generics) == 0 {
			return method
		}
		return method.Instance(recv.Base().Args)
	}
	panic(diag.Internal("call kind %d has no function to resolve", int(info.Kind)))
}

// Typings are the Typing of every module of a project, by handle, for code that follows a call, a constant or a field
// default into another module's bodies. They are complete once the check pass is, and only read from then on.
type Typings []*Typing

// Of is the typing of a module.
func (ts Typings) Of(m ModuleID) *Typing {
	if uint32(m) >= uint32(len(ts)) {
		noTyping(m, len(ts))
	}
	return ts[m]
}

// noTyping is the error for a module with no typing: the standard library, which has no bodies, or a module beyond the
// project's. It is a function of its own, kept out of line, so that Of is small enough to inline.
//
//go:noinline
func noTyping(m ModuleID, count int) {
	panic(diag.Internal("typing: module %d has no typing among %d", m, count))
}

// Type is the type of an expression, wherever its module is.
func (ts Typings) Type(e Expr) Ty {
	b := e.Base()
	return ts.Of(b.Mod).TypeAt(b)
}

// Consumed is whether the ownership pass found the expression consuming its value, wherever its module is.
func (ts Typings) Consumed(e Expr) bool {
	b := e.Base()
	return ts.Of(b.Mod).ConsumedAt(b)
}

// For is the typing of the module that the node is in.
func (ts Typings) For(n Node) *Typing { return ts.Of(n.P().Mod) }
