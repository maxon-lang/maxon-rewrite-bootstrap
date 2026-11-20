package ast

import (
	"testing"

	"minimaxon/internal/diag"
)

// requireInternalError fails unless f panics with an internal compiler error, which is what a read of an entry that does
// not exist must be.
func requireInternalError(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if _, ok := recover().(*diag.InternalError); !ok {
			t.Errorf("%s did not panic with an internal error", what)
		}
	}()
	f()
}

func TestTypingGivesBackTheTypeOfAnExpression(t *testing.T) {
	arena := NewArena(0)
	lit, name, call := arena.NewIntLit(), arena.NewNameExpr(), arena.NewCallExpr()
	var typing Typing
	typing.Reset(arena.Slots())
	typing.SetType(lit, BoolTy)
	typing.SetType(name, StringTy)
	typing.SetType(call, VoidTy)
	if typing.Type(lit) != Ty(BoolTy) || typing.Type(name) != Ty(StringTy) || typing.Type(call) != Ty(VoidTy) {
		t.Errorf("the types are %v, %v and %v, want bool, String and void", typing.Type(lit).Name(), typing.Type(name).Name(), typing.Type(call).Name())
	}
}

func TestTypingSizesASparseTableByTheNodesOfItsKind(t *testing.T) {
	arena := NewArena(0)
	var calls []*CallExpr
	for range 3 {
		for range 20 {
			arena.NewIntLit()
		}
		calls = append(calls, arena.NewCallExpr())
	}
	var typing Typing
	typing.Reset(arena.Slots())
	if len(typing.types) != 63+1 || len(typing.calls) != 3+1 {
		t.Fatalf("63 expressions of which 3 are calls have %d type entries and %d call entries, want 64 and 4", len(typing.types), len(typing.calls))
	}
	if len(typing.names) != 1 || len(typing.members) != 1 {
		t.Errorf("a module with no names or members has %d and %d entries, want none (slot 0 only)", len(typing.names), len(typing.members))
	}
	for i, call := range calls {
		typing.Call(call).Kind = CallKind(i + 1)
	}
	for i, call := range calls {
		if got := typing.Call(call).Kind; got != CallKind(i+1) {
			t.Errorf("call %d reads back kind %d, want %d", i, got, i+1)
		}
	}
}

func TestTypingKeepsEachKindOfEntryApart(t *testing.T) {
	arena := NewArena(0)
	name, member := arena.NewNameExpr(), arena.NewMemberExpr()
	letStmt, forStmt, param := arena.NewLetStmt(), arena.NewForStmt(), arena.NewParam()
	brk, cont := arena.NewBreakStmt(), arena.NewContinueStmt()
	when, pattern := arena.NewWhenStmt(), arena.NewPattern()
	lit := arena.NewStructLitExpr()
	var typing Typing
	typing.Reset(arena.Slots())

	fn := &FuncDecl{Name: "f"}
	local, counter := NewLocal("x", BoolTy, false), NewLocal("i", BoolTy, false)
	fn.AddLocal(local)
	fn.AddLocal(counter)
	typing.SetName(name, LocalName(local))
	typing.SetMember(member, MemberInfo{Kind: MemberEnumCase, CaseIndex: 4})
	typing.SetLetLocal(letStmt, local)
	typing.SetForLocal(forStmt, counter)
	typing.SetBreakTarget(brk, 7)
	typing.SetContinueTarget(cont, 9)
	typing.EditWhen(when).Fn = &FuncDecl{Name: "body"}
	typing.EditPattern(pattern).CaseIndex = 2
	typing.SetStructLit(lit, StructLitInfo{Record: &RecordTy{}})

	if typing.Name(name).Kind() != NameLocal || typing.Name(name).Local(fn) != local {
		t.Error("the name does not stand for its local")
	}
	if got := typing.Member(member); got.Kind != MemberEnumCase || got.CaseIndex != 4 {
		t.Errorf("the member is %+v", got)
	}
	if fn.LocalAt(typing.LetLocal(letStmt)) != local || fn.LocalAt(typing.ForLocal(forStmt)) != counter || fn.LocalAt(typing.ParamLocal(param)) != nil {
		t.Error("a let, a for and a parameter that declares nothing do not read back their locals")
	}
	if typing.BreakTarget(brk) != 7 || typing.ContinueTarget(cont) != 9 {
		t.Errorf("the jumps go to %d and %d, want 7 and 9", typing.BreakTarget(brk), typing.ContinueTarget(cont))
	}
	if typing.When(when).Fn.Name != "body" || typing.Pattern(pattern).CaseIndex != 2 || typing.StructLit(lit).Record == nil {
		t.Error("a behaviour, a pattern or a struct literal does not read back")
	}
}

func TestANameStandsForALocalAFieldOrAConstantByHandle(t *testing.T) {
	record := &RecordTy{Fields: []*FieldInfo{{Name: "a", Index: 0}, {Name: "b", Index: 1}}}
	method := &FuncDecl{Name: "m", Owner: record}
	first, second := NewLocal("first", BoolTy, false), NewLocal("second", StringTy, false)
	method.AddLocal(first)
	method.AddLocal(second)
	var typing Typing
	typing.Reset(Slots{})

	if first.ID() != 1 || second.ID() != 2 {
		t.Errorf("the locals have the handles %d and %d, want 1 and 2", first.ID(), second.ID())
	}
	if got := LocalName(second).Local(method); got != second {
		t.Errorf("the name of the second local reads back %v", got)
	}
	if got := FieldName(record.Fields[1]).Field(method); got != record.Fields[1] {
		t.Errorf("the name of the second field reads back %v", got)
	}
	one, another := &ConstDecl{}, &ConstDecl{}
	a, b, again := typing.ConstName(one), typing.ConstName(another), typing.ConstName(one)
	if a != again || a == b || typing.Const(a) != one || typing.Const(b) != another {
		t.Error("a constant that is named twice has two entries, or an entry reads back another constant")
	}
	if LocalName(first).Kind() != NameLocal || FieldName(record.Fields[0]).Kind() != NameSelfField || a.Kind() != NameConst || (NameInfo{}).Kind() != NameUnresolved {
		t.Error("a name does not say what it stands for")
	}
	requireInternalError(t, "a local that no function lists", func() { LocalName(NewLocal("loose", BoolTy, false)) })
	requireInternalError(t, "a local beyond the function's", func() { method.LocalAt(3) })
	requireInternalError(t, "the local of a name of a field", func() { FieldName(record.Fields[0]).Local(method) })
	requireInternalError(t, "the field of a name of a local", func() { LocalName(first).Field(method) })
	requireInternalError(t, "the constant of a name of a local", func() { typing.Const(LocalName(first)) })
}

func TestANameKeepsItsKindAndIndexInOneWord(t *testing.T) {
	for _, kind := range []NameKind{NameLocal, NameConst, NameSelfField} {
		name := nameOf(kind, nameIndexMask)
		if name.Kind() != kind || name.index() != nameIndexMask {
			t.Errorf("kind %d with the largest index reads back as kind %d, index %d", kind, name.Kind(), name.index())
		}
	}
	requireInternalError(t, "an index that does not fit beside the kind", func() { nameOf(NameLocal, nameIndexMask+1) })
}

func TestTheReceiverOfACallIsTheObjectOfTheMemberItNames(t *testing.T) {
	arena := NewArena(0)
	member, object := arena.NewMemberExpr(), arena.NewNameExpr()
	member.Obj = object
	method := arena.NewCallExpr()
	method.Callee = member
	plain := arena.NewCallExpr()
	plain.Callee = arena.NewNameExpr()
	for _, kind := range []CallKind{CallMethod, CallBound, CallCopy} {
		if got := method.ReceiverFor(kind); got != Expr(object) {
			t.Errorf("a call of kind %d has the receiver %v, want the member's object", kind, got)
		}
	}
	if method.ReceiverFor(CallFunction) != nil || plain.ReceiverFor(CallMethod) != nil || (&CallExpr{}).ReceiverFor(CallMethod) != nil {
		t.Error("a function, a method named by a bare name, or a call with no callee has a receiver")
	}
}

func TestTypingMarksConsumedAcrossWordBoundaries(t *testing.T) {
	arena := NewArena(0)
	var lits []*IntLit
	for range 130 {
		lits = append(lits, arena.NewIntLit())
	}
	var typing Typing
	typing.Reset(arena.Slots())
	for i, lit := range lits {
		if i%3 == 0 {
			typing.MarkConsumed(lit)
		}
	}
	for i, lit := range lits {
		if typing.Consumed(lit) != (i%3 == 0) {
			t.Errorf("expression %d (slot %d) is consumed: %v", i, lit.Ord, typing.Consumed(lit))
		}
	}
}

func TestTypingGivesTheOperandOfABinaryOperator(t *testing.T) {
	arena := NewArena(0)
	plain, unsigned, text := arena.NewBinaryExpr(), arena.NewBinaryExpr(), arena.NewBinaryExpr()
	var typing Typing
	typing.Reset(arena.Slots())
	typing.SetOperand(unsigned, OperandOf(&IntTy{Alias: "u8", Max: U64Max}))
	typing.SetOperand(text, OperandOf(StringTy))
	if typing.Operand(plain) != OperandWord || typing.Operand(unsigned) != OperandUnsigned || typing.Operand(text) != OperandString {
		t.Errorf("the operands are %d, %d and %d", typing.Operand(plain), typing.Operand(unsigned), typing.Operand(text))
	}
	if OperandOf(Anonymous64) != OperandWord || OperandOf(BoolTy) != OperandWord {
		t.Error("a signed integer and a bool are held as words")
	}
}

func TestTypingGrowsForNodesMadeAfterItWasSized(t *testing.T) {
	arena := NewArena(0)
	early := arena.NewNameExpr()
	var typing Typing
	typing.Reset(arena.Slots())
	typing.SetType(early, BoolTy)
	typing.SetName(early, typing.ConstName(&ConstDecl{}))
	typing.MarkConsumed(early)

	late := arena.NewNameExpr()
	requireInternalError(t, "a read of an expression made after the tables were sized", func() { typing.Type(late) })
	typing.Sync(arena.Slots())
	typing.SetType(late, StringTy)
	typing.SetName(late, FieldName(&FieldInfo{}))
	if typing.Type(late) != Ty(StringTy) || typing.Name(late).Kind() != NameSelfField {
		t.Error("the node made late does not read back")
	}
	if typing.Type(early) != Ty(BoolTy) || typing.Name(early).Kind() != NameConst || !typing.Consumed(early) || typing.Consumed(late) {
		t.Error("growing the tables lost what the earlier node had, or marked the new one consumed")
	}
}

func TestReadingAnEntryThatDoesNotExistIsAnInternalError(t *testing.T) {
	arena := NewArena(0)
	typed := arena.NewIntLit()
	var typing Typing
	typing.Reset(arena.Slots())
	typing.SetType(typed, BoolTy)

	notMadeByAnArena := &NameExpr{}
	requireInternalError(t, "a read of a name that no arena made, which has slot 0", func() { typing.Name(notMadeByAnArena) })
	requireInternalError(t, "a read of the type of an expression that no arena made", func() { typing.Type(notMadeByAnArena) })
	requireInternalError(t, "a write to an expression that no arena made", func() { typing.SetType(notMadeByAnArena, BoolTy) })

	other := NewArena(1)
	other.NewIntLit()
	late := other.NewIntLit()
	requireInternalError(t, "a read of a slot beyond the table", func() { typing.Type(late) })

	untyped := NewArena(0).NewIntLit()
	var empty Typing
	empty.Reset(Slots{Expr: 1})
	requireInternalError(t, "a read of the type of an expression the checker never typed", func() { empty.Type(untyped) })
}

func TestTypingsFindTheTypingOfANodesModule(t *testing.T) {
	first, second := NewArena(0), NewArena(1)
	a, b := first.NewIntLit(), second.NewIntLit()
	a.Pos.Mod, b.Pos.Mod = 0, 1
	typings := Typings{new(Typing), new(Typing)}
	typings[0].Reset(first.Slots())
	typings[1].Reset(second.Slots())
	typings.For(a).SetType(a, BoolTy)
	typings.For(b).SetType(b, StringTy)
	if typings.For(a).Type(a) != Ty(BoolTy) || typings.For(b).Type(b) != Ty(StringTy) {
		t.Error("a node does not find its own module's entry")
	}
	requireInternalError(t, "a typing of the standard library, which has none", func() { typings.Of(StdlibID) })
	requireInternalError(t, "a typing of a module beyond the project's", func() { typings.Of(2) })
}
