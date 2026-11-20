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
	Drop *CallInfo
}

// ReceiverFor is the object a call of the kind is made on, or nil for a call that has none. A method call, a call through
// a bound and a copy are written 'object.name(...)', so it is the object of the member the call names.
func (c CallExpr) ReceiverFor(kind CallKind) Expr {
	switch kind {
	case CallMethod, CallBound, CallCopy:
		if m, named := c.Callee().AsMemberExpr(); named {
			return m.Obj()
		}
	}
	return Expr{}
}

// CallUse is a call a function records, for finding the instances of generics it needs: what the call is, and the object it
// is made on, which a call through a bound needs to know the type of.
type CallUse struct {
	Info     *CallInfo
	Receiver Expr
	// Site is where the call is, which an error about the instance it needs is reported at.
	Site Pos
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
// each let and for declares, where each break and continue goes, and the function and captures of each behaviour. The
// nodes keep only what the author wrote.
//
// What nearly every expression has, a type and a consumed flag, is indexed by the node's own NodeID. What only some kinds
// of node have is indexed by a slot that only the nodes of that kind count (a call has one of 'Call', so the table of calls
// has as many entries as the module has calls, not as many as it has nodes), and the slot of a node is found by its NodeID
// in one table, slots. Slot 0 is no slot, so a node of a kind with no table has no entry and a read of it is an internal
// error.
//
// It is written only by the checker of its own module: the constants one at a time (each writes the entries of its
// own expressions, and the tables were sized before the first), then the bodies and the ownership pass of the module's
// worker. Every module's check pass completes before the generate pass starts, which is the only reader of another
// module's Typing, so no table is read while it is written.
type Typing struct {
	// slots is the slot of each node by its NodeID, and count how many slots of each table there are.
	slots []uint32
	count Slots

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

// Size makes the tables that have an entry for every node, for the nodes the tree has now. It needs nothing that
// resolution decides, so the driver does it while the serial work of the project leaves the other cores idle: a table of this
// size is allocated on one core at a time, and the check pass would make every worker wait for the heap.
func (t *Typing) Size(tree *Tree) {
	t.slots = make([]uint32, len(tree.Records))
	t.types = make([]Ty, len(tree.Records))
	t.consumed = make([]uint64, len(tree.Records)/64+1)
}

// Reset numbers the slots of the module's tree as it is now, after resolution made its last node, and sizes the tables of
// the kinds that only some nodes have. It grows what Size made for a node that resolution added, and makes it if Size was
// not called.
func (t *Typing) Reset(tree *Tree) {
	if len(t.slots) < len(tree.Records) {
		t.slots = append(t.slots, make([]uint32, len(tree.Records)-len(t.slots))...)
		t.types = append(t.types, make([]Ty, len(tree.Records)-len(t.types))...)
		t.consumed = append(t.consumed, make([]uint64, len(tree.Records)/64+1-len(t.consumed))...)
	}
	clear(t.slots)
	t.count = tree.NumberSlots(t.slots)
	t.operands = make([]Operand, t.count.Binary+1)
	t.names = make([]NameInfo, t.count.Name+1)
	t.members = make([]MemberInfo, t.count.Member+1)
	t.calls = make([]CallInfo, t.count.Call+1)
	t.structLits = make([]StructLitInfo, t.count.StructLit+1)
	t.patterns = make([]PatternInfo, t.count.Pattern+1)
	t.whens = make([]WhenInfo, t.count.When+1)
	t.locals = make([]LocalID, t.count.Local+1)
	t.jumps = make([]NodeID, t.count.Jump+1)
	t.consts, t.constOf = nil, nil
}

// entry is the entry of a node in the table, which must be one the table has.
func entry[T any](t *Typing, table []T, id NodeID, what string) *T {
	if int(id) >= len(t.slots) {
		noEntry(uint32(id), len(t.slots), what)
	}
	s := t.slots[id]
	if s == 0 || int(s) >= len(table) {
		noEntry(s, len(table)-1, what)
	}
	return &table[s]
}

// noEntry is the error for a node that the table has no entry for, kept out of line so that entry is small enough to inline.
//
//go:noinline
func noEntry(slot uint32, entries int, what string) {
	panic(diag.Internal("typing: slot %d of the %s table is not among its %d entries", slot, what, entries))
}

// Type is the type of an expression the checker typed.
func (t *Typing) Type(e Expr) Ty {
	if uint(e.ID) >= uint(len(t.types)) {
		noEntry(uint32(e.ID), len(t.types)-1, "type")
	}
	ty := t.types[e.ID]
	if ty == nil {
		noType(e.Ref)
	}
	return ty
}

// noType is the error for an expression the checker never typed, kept out of line so that Type is small enough to inline.
//
//go:noinline
func noType(e Ref) {
	panic(diag.Internal("typing: the expression at %d has no type", e.Position().Off))
}

func (t *Typing) SetType(e Expr, ty Ty) {
	if uint(e.ID) >= uint(len(t.types)) {
		noEntry(uint32(e.ID), len(t.types)-1, "type")
	}
	t.types[e.ID] = ty
}

// Consumed is whether the ownership pass found the expression in a position that takes ownership of its value: a binding,
// an assignment, a consumes argument, a return, a field or payload, or a branch of a conditional or a match. A place there
// is moved out of; a fresh value there needs no one to drop it. Lowering reads this to place drops, so the pass is the
// only place that decides what a move is.
func (t *Typing) Consumed(e Expr) bool {
	if uint(e.ID) >= uint(len(t.types)) {
		noEntry(uint32(e.ID), len(t.types)-1, "consumed")
	}
	return t.consumed[e.ID/64]&(1<<(e.ID%64)) != 0
}

func (t *Typing) MarkConsumed(e Expr) {
	if uint(e.ID) >= uint(len(t.types)) {
		noEntry(uint32(e.ID), len(t.types)-1, "consumed")
	}
	t.consumed[e.ID/64] |= 1 << (e.ID % 64)
}

// Operand is how the operands of a binary operator are compared and divided.
func (t *Typing) Operand(b BinaryExpr) Operand { return *entry(t, t.operands, b.ID, "operand") }

func (t *Typing) SetOperand(b BinaryExpr, o Operand) { *entry(t, t.operands, b.ID, "operand") = o }

func (t *Typing) Name(n NameExpr) NameInfo { return *entry(t, t.names, n.ID, "name") }

func (t *Typing) SetName(n NameExpr, info NameInfo) { *entry(t, t.names, n.ID, "name") = info }

// ConstName is the name of a constant.
func (t *Typing) ConstName(d *ConstDecl) NameInfo {
	t.constMu.Lock()
	defer t.constMu.Unlock()
	s, listed := t.constOf[d]
	if !listed {
		if t.constOf == nil {
			t.constOf = map[*ConstDecl]uint32{}
		}
		t.consts = append(t.consts, d)
		s = uint32(len(t.consts))
		t.constOf[d] = s
	}
	return nameOf(NameConst, s)
}

// Const is the constant a name of a constant stands for.
func (t *Typing) Const(i NameInfo) *ConstDecl {
	if i.Kind() != NameConst || int(i.index()) > len(t.consts) {
		panic(diag.Internal("typing: a name that is not a constant has no constant"))
	}
	return t.consts[i.index()-1]
}

func (t *Typing) Member(m MemberExpr) MemberInfo { return *entry(t, t.members, m.ID, "member") }

func (t *Typing) SetMember(m MemberExpr, info MemberInfo) {
	*entry(t, t.members, m.ID, "member") = info
}

// Call is what a call is. It is the entry itself and not a copy, which a call's size would make worth avoiding: the checker
// fills it in as it learns more about the call, and everything else only reads it.
func (t *Typing) Call(c CallExpr) *CallInfo { return entry(t, t.calls, c.ID, "call") }

func (t *Typing) StructLit(s StructLitExpr) StructLitInfo {
	return *entry(t, t.structLits, s.ID, "struct literal")
}

func (t *Typing) SetStructLit(s StructLitExpr, info StructLitInfo) {
	*entry(t, t.structLits, s.ID, "struct literal") = info
}

func (t *Typing) Pattern(p Pattern) PatternInfo { return *entry(t, t.patterns, p.ID, "pattern") }

// EditPattern is the entry of a pattern, for the checker to fill in.
func (t *Typing) EditPattern(p Pattern) *PatternInfo { return entry(t, t.patterns, p.ID, "pattern") }

func (t *Typing) When(w WhenStmt) WhenInfo { return *entry(t, t.whens, w.ID, "behaviour") }

// EditWhen is the entry of a behaviour, for the checker to fill in.
func (t *Typing) EditWhen(w WhenStmt) *WhenInfo { return entry(t, t.whens, w.ID, "behaviour") }

// LetLocal and ForLocal are the local a let or a for declares, in the function it is in: 0 for a 'for' named '_'.
func (t *Typing) LetLocal(l LetStmt) LocalID { return *entry(t, t.locals, l.ID, "local") }

func (t *Typing) ForLocal(f ForStmt) LocalID { return *entry(t, t.locals, f.ID, "local") }

func (t *Typing) SetLetLocal(l LetStmt, local *Local) { *entry(t, t.locals, l.ID, "local") = local.id }

func (t *Typing) SetForLocal(f ForStmt, local *Local) { *entry(t, t.locals, f.ID, "local") = local.id }

// BreakTarget and ContinueTarget are the loop or match that a break or a continue leaves or repeats.
func (t *Typing) BreakTarget(b BreakStmt) NodeID { return *entry(t, t.jumps, b.ID, "jump") }

func (t *Typing) ContinueTarget(c ContinueStmt) NodeID { return *entry(t, t.jumps, c.ID, "jump") }

func (t *Typing) SetBreakTarget(b BreakStmt, target NodeID) {
	*entry(t, t.jumps, b.ID, "jump") = target
}

func (t *Typing) SetContinueTarget(c ContinueStmt, target NodeID) {
	*entry(t, t.jumps, c.ID, "jump") = target
}

// IsStorage is whether an expression reads a value that already lives somewhere, in a variable, a field or an element, and
// so borrows it, as opposed to making a new value. It asks about four members of Expr, so it is a type switch and not a
// visitor.
func (t *Typing) IsStorage(e Expr) bool {
	switch e.Kind() {
	case KindNameExpr:
		kind := t.Name(NameExpr{e.Ref}).Kind()
		return kind == NameLocal || kind == NameSelfField
	case KindSelfExpr, KindIndexExpr:
		return true
	case KindMemberExpr:
		return t.Member(MemberExpr{e.Ref}).Kind == MemberField
	}
	return false
}

// Resolved is the function a call runs once the type parameters of the code it is in are bound: the target itself, or
// the instance of a generic one, or for a call through a bound the method of the type that stands in for the
// parameter.
func (t *Typing) Resolved(use CallUse, b Binding) *FuncDecl {
	info := use.Info
	switch info.Kind {
	case CallFunction, CallStatic, CallMethod:
		if len(info.Target.Generics) == 0 {
			return info.Target
		}
		return info.Target.Instance(b.applyAll(info.Generic.TypeArgs))
	case CallBound:
		// A frozen receiver lends its contents, so the method is the one of the type it holds.
		receiver := b.Apply(t.Type(use.Receiver))
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
func (ts Typings) Type(e Expr) Ty { return ts.Of(e.T.Module).Type(e) }

// Consumed is whether the ownership pass found the expression consuming its value, wherever its module is.
func (ts Typings) Consumed(e Expr) bool { return ts.Of(e.T.Module).Consumed(e) }

// For is the typing of the module that the node is in.
func (ts Typings) For(pos Pos) *Typing { return ts.Of(pos.Mod) }
