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

// sized is a typing for the nodes the tree has now.
func sized(tree *Tree) *Typing {
	typing := new(Typing)
	typing.Reset(tree)
	return typing
}

func TestTypingGivesBackTheTypeOfAnExpression(t *testing.T) {
	tree := NewTree(0, "", 0)
	lit, name, call := tree.NewIntLit(0), tree.NewNameExpr(0), tree.NewCallExpr(0)
	typing := sized(tree)
	typing.SetType(lit.Expr(), BoolTy)
	typing.SetType(name.Expr(), StringTy)
	typing.SetType(call.Expr(), VoidTy)
	if typing.Type(lit.Expr()) != Ty(BoolTy) || typing.Type(name.Expr()) != Ty(StringTy) || typing.Type(call.Expr()) != Ty(VoidTy) {
		t.Errorf("the types are %v, %v and %v, want bool, String and void", typing.Type(lit.Expr()).Name(), typing.Type(name.Expr()).Name(), typing.Type(call.Expr()).Name())
	}
}

func TestTypingSizesASparseTableByTheNodesOfItsKind(t *testing.T) {
	tree := NewTree(0, "", 0)
	var calls []CallExpr
	for range 3 {
		for range 20 {
			tree.NewIntLit(0)
		}
		calls = append(calls, tree.NewCallExpr(0))
	}
	typing := sized(tree)
	if len(typing.types) != 63+1 || len(typing.calls) != 3+1 {
		t.Fatalf("63 nodes of which 3 are calls have %d type entries and %d call entries, want 64 and 4", len(typing.types), len(typing.calls))
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
	tree := NewTree(0, "", 0)
	name, member := tree.NewNameExpr(0), tree.NewMemberExpr(0)
	letStmt, forStmt := tree.NewLetStmt(0), tree.NewForStmt(0)
	brk, cont := tree.NewBreakStmt(0), tree.NewContinueStmt(0)
	when, pattern := tree.NewWhenStmt(0), tree.NewPattern(0)
	lit := tree.NewStructLitExpr(0)
	typing := sized(tree)

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
	if fn.LocalAt(typing.LetLocal(letStmt)) != local || fn.LocalAt(typing.ForLocal(forStmt)) != counter {
		t.Error("a let and a for do not read back their locals")
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
	typing := sized(NewTree(0, "", 0))

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
	tree := NewTree(0, "", 0)
	member, object := tree.NewMemberExpr(0), tree.NewNameExpr(0)
	member.SetObj(object.Expr())
	method := tree.NewCallExpr(0)
	method.SetCallee(member.Expr())
	plain := tree.NewCallExpr(0)
	plain.SetCallee(tree.NewNameExpr(0).Expr())
	for _, kind := range []CallKind{CallMethod, CallBound, CallCopy} {
		if got := method.ReceiverFor(kind); got != object.Expr() {
			t.Errorf("a call of kind %d has the receiver %v, want the member's object", kind, got)
		}
	}
	if !method.ReceiverFor(CallFunction).IsNil() || !plain.ReceiverFor(CallMethod).IsNil() || !tree.NewCallExpr(0).ReceiverFor(CallMethod).IsNil() {
		t.Error("a function, a method named by a bare name, or a call with no callee has a receiver")
	}
}

func TestTypingMarksConsumedAcrossWordBoundaries(t *testing.T) {
	tree := NewTree(0, "", 0)
	var lits []IntLit
	for range 130 {
		lits = append(lits, tree.NewIntLit(0))
	}
	typing := sized(tree)
	for i, lit := range lits {
		if i%3 == 0 {
			typing.MarkConsumed(lit.Expr())
		}
	}
	for i, lit := range lits {
		if typing.Consumed(lit.Expr()) != (i%3 == 0) {
			t.Errorf("expression %d (node %d) is consumed: %v", i, lit.ID, typing.Consumed(lit.Expr()))
		}
	}
}

func TestTypingGivesTheOperandOfABinaryOperator(t *testing.T) {
	tree := NewTree(0, "", 0)
	plain, unsigned, text := tree.NewBinaryExpr(0), tree.NewBinaryExpr(0), tree.NewBinaryExpr(0)
	typing := sized(tree)
	typing.SetOperand(unsigned, OperandOf(&IntTy{Alias: "u8", Max: U64Max}))
	typing.SetOperand(text, OperandOf(StringTy))
	typing.SetOperand(plain, OperandOf(BoolTy))
	if typing.Operand(plain) != OperandWord || typing.Operand(unsigned) != OperandUnsigned || typing.Operand(text) != OperandString {
		t.Errorf("the operands are %d, %d and %d", typing.Operand(plain), typing.Operand(unsigned), typing.Operand(text))
	}
	if OperandOf(Anonymous64) != OperandWord || OperandOf(BoolTy) != OperandWord {
		t.Error("a signed integer and a bool are held as words")
	}
}

func TestTypingNumbersAMemberThatBecameANameAsAName(t *testing.T) {
	tree := NewTree(0, "geometry.area", 0)
	head := tree.NewNameExpr(0)
	head.SetNameRef(TextRef(0))
	member := tree.NewMemberExpr(0)
	member.SetObj(head.Expr())
	member.SetNameRef(TextRef(9))
	other := tree.NewNameExpr(0)
	member.BecomeName(head)
	typing := sized(tree)
	typing.SetName(NameExpr{member.Ref}, LocalName(func() *Local {
		fn := &FuncDecl{}
		l := NewLocal("area", BoolTy, false)
		fn.AddLocal(l)
		return l
	}()))
	typing.SetName(other, FieldName(&FieldInfo{}))
	if typing.Name(NameExpr{member.Ref}).Kind() != NameLocal || typing.Name(other).Kind() != NameSelfField || len(typing.members) != 1 {
		t.Errorf("a rewritten name has no entry of its own: %d member entries", len(typing.members)-1)
	}
}

func TestReadingAnEntryThatDoesNotExistIsAnInternalError(t *testing.T) {
	tree := NewTree(0, "", 0)
	typed := tree.NewIntLit(0)
	typing := sized(tree)
	typing.SetType(typed.Expr(), BoolTy)

	call := tree.NewCallExpr(0)
	requireInternalError(t, "a read of a call that was made after the tables were sized, which has no slot", func() { typing.Call(call) })
	requireInternalError(t, "a read of a name that is not in the numbering", func() { typing.Name(NameExpr{Ref{tree, 99}}) })
	requireInternalError(t, "a read of the type of a node beyond the table", func() { typing.Type(Expr{Ref{tree, 99}}) })
	requireInternalError(t, "a write to a node beyond the table", func() { typing.SetType(Expr{Ref{tree, 99}}, BoolTy) })
	requireInternalError(t, "a read of the consumed flag of a node beyond the table", func() { typing.Consumed(Expr{Ref{tree, 99}}) })

	untyped := tree.NewIntLit(0)
	requireInternalError(t, "a read of the type of an expression the checker never typed", func() { sized(tree).Type(untyped.Expr()) })
}

func TestTypingsFindTheTypingOfANodesModule(t *testing.T) {
	first, second := NewTree(0, "", 0), NewTree(1, "", 0)
	a, b := first.NewIntLit(0), second.NewIntLit(0)
	typings := Typings{sized(first), sized(second)}
	typings.For(a.Position()).SetType(a.Expr(), BoolTy)
	typings.For(b.Position()).SetType(b.Expr(), StringTy)
	if typings.Type(a.Expr()) != Ty(BoolTy) || typings.Type(b.Expr()) != Ty(StringTy) {
		t.Error("a node does not find its own module's entry")
	}
	requireInternalError(t, "a typing of the standard library, which has none", func() { typings.Of(StdlibID) })
	requireInternalError(t, "a typing of a module beyond the project's", func() { typings.Of(2) })
}
