package ast

import (
	"slices"
	"testing"
	"unsafe"
)

func TestARecordIsSixteenBytes(t *testing.T) {
	if got := unsafe.Sizeof(Record{}); got != 16 {
		t.Errorf("a record is %d bytes, want 16: four to a cache line", got)
	}
}

func TestSlabNodesKeepTheirAddressAsItGrows(t *testing.T) {
	var slab slab[FuncDecl]
	var nodes []*FuncDecl
	for i := range 10 * maxSlabNodes {
		n := slab.next()
		n.Name = string(rune('a' + i%26))
		nodes = append(nodes, n)
	}
	for i, n := range nodes {
		if n.Name != string(rune('a'+i%26)) {
			t.Fatalf("node %d was overwritten: %q", i, n.Name)
		}
	}
}

func TestSlabChunksGrowToTheBound(t *testing.T) {
	var s slab[int]
	capacities := map[int]bool{}
	for range 10 * maxSlabNodes {
		s.next()
		capacities[cap(s.chunk)] = true
	}
	for _, want := range []int{firstSlabNodes, 2 * firstSlabNodes, maxSlabNodes} {
		if !capacities[want] {
			t.Errorf("no chunk of %d nodes among %v", want, capacities)
		}
	}
	for c := range capacities {
		if c > maxSlabNodes {
			t.Errorf("a chunk of %d nodes is over the bound %d", c, maxSlabNodes)
		}
	}
}

func TestATreeNumbersNodesFromOneInTheOrderTheyAreMade(t *testing.T) {
	tree := NewTree(7, "", 0)
	name := tree.NewNameExpr(10)
	decl := tree.NewFuncDecl(20)
	call := tree.NewCallExpr(30)
	if name.ID != 1 || decl.ID != 2 || call.ID != 3 {
		t.Errorf("the nodes are %d, %d and %d, want 1, 2 and 3: a struct has a number too", name.ID, decl.ID, call.ID)
	}
	for _, c := range []struct {
		n    Node
		want Pos
	}{{name, Pos{7, 10}}, {decl, Pos{7, 20}}, {call, Pos{7, 30}}} {
		if got := c.n.Position(); got != c.want {
			t.Errorf("a node is at %v, want %v", got, c.want)
		}
	}
	if tree.Count() != 3 {
		t.Errorf("the tree made 3 nodes and counts %d", tree.Count())
	}
	if name.Kind() != KindNameExpr || tree.Records[decl.ID].Kind != KindFuncDecl {
		t.Errorf("the kinds are %v and %v", name.Kind(), tree.Records[decl.ID].Kind)
	}
}

func TestAViewOfNodeZeroIsNoNode(t *testing.T) {
	tree := NewTree(0, "", 0)
	if !(Expr{}).IsNil() || !(Expr{Ref{tree, 0}}).IsNil() || tree.NewNameExpr(0).IsNil() {
		t.Error("node 0 is a node, or a node made is none")
	}
	if _, ok := (Expr{}).AsNameExpr(); ok {
		t.Error("no node is a name")
	}
}

func TestARecordHoldsItsFlagsTextsAndChildrenInItsWords(t *testing.T) {
	text := "let total = 'sum'"
	tree := NewTree(0, text, 4)
	cond := tree.NewNameExpr(4)
	cond.SetNameRef(TextRef(4))
	end := tree.NewEndLabel(0)
	end.SetNameRef(TextRef(13))
	n := tree.NewIfStmt(0)
	n.SetCond(cond.Expr())
	n.SetLabelRef(TextRef(13))
	n.SetHasElse()
	n.SetEnd(end)
	if got := n.Cond(); got != cond.Expr() {
		t.Errorf("the condition is %v, want %v", got, cond.Expr())
	}
	if cond.Name() != "total" {
		t.Errorf("the name is %q, want \"total\": a name runs to the end of its identifier characters", cond.Name())
	}
	if n.Label() != "sum" || end.Name() != "sum" {
		t.Errorf("the label is %q and the end label %q, want both \"sum\"", n.Label(), end.Name())
	}
	if !n.HasElse() || n.End() != end || !n.ElseEnd().IsNil() || n.ElseLabel() != "" {
		t.Errorf("flags and optional children: else %v, end %v, else end %v, else label %q", n.HasElse(), n.End(), n.ElseEnd(), n.ElseLabel())
	}
	if n.Then().Len() != 0 {
		t.Errorf("a list that was never set has %d items", n.Then().Len())
	}
}

func TestAChoiceOfLiteralsIsAByteOfAux(t *testing.T) {
	tree := NewTree(0, "", 0)
	b := tree.NewBinaryExpr(0)
	if b.Op() != "" {
		t.Errorf("an operator that was never set is %q", b.Op())
	}
	for _, op := range []string{"+", "mod", ">=", "xor"} {
		b.SetOp(op)
		if b.Op() != op {
			t.Errorf("the operator is %q, want %q", b.Op(), op)
		}
	}
	defer func() {
		if recover() == nil {
			t.Error("setting an operator the grammar does not have did not fail")
		}
	}()
	b.SetOp("**")
}

func TestARecordKeepsMoreThanTwoWordsInABlock(t *testing.T) {
	tree := NewTree(0, "", 0)
	a, b, c := tree.NewIntLit(1), tree.NewIntLit(2), tree.NewIntLit(3)
	n := tree.NewCondExpr(0)
	n.SetThen(a.Expr())
	n.SetCond(b.Expr())
	n.SetElse(c.Expr())
	if n.Then() != a.Expr() || n.Cond() != b.Expr() || n.Else() != c.Expr() {
		t.Errorf("a conditional holds %v, %v and %v", n.Then(), n.Cond(), n.Else())
	}
	if tree.Records[n.ID].A != uint32(a.ID) {
		t.Error("the first word is not in the record")
	}
	other := tree.NewCondExpr(0)
	other.SetElse(a.Expr())
	if n.Else() != c.Expr() || other.Else() != a.Expr() || !other.Then().IsNil() {
		t.Error("two nodes share a block")
	}
}

func TestAnIntegerLiteralHoldsSixtyFourBits(t *testing.T) {
	tree := NewTree(0, "", 0)
	lit := tree.NewIntLit(0)
	lit.SetMagnitude(U64(0xFFFF_FFFF_0000_0001))
	lit.SetNegative()
	if lit.Magnitude() != U64(0xFFFF_FFFF_0000_0001) || !lit.Negative() || lit.Value() != U64(0xFFFF_FFFF_0000_0001).Neg() {
		t.Errorf("the literal is %v, negative %v, value %v", lit.Magnitude(), lit.Negative(), lit.Value())
	}
	defer func() {
		if recover() == nil {
			t.Error("a magnitude over 64 bits was stored")
		}
	}()
	lit.SetMagnitude(I128{Hi: 1})
}

func TestAListIsItsCountThenItsItems(t *testing.T) {
	tree := NewTree(0, "", 0)
	var items []uint32
	for range 3 {
		items = append(items, uint32(tree.NewIntLit(0).ID))
	}
	ref := tree.AppendList(items)
	list := ListAt[Expr](tree, ref)
	if list.Len() != 3 || list.Ref() != ref {
		t.Fatalf("the list has %d items and the name %d, want 3 and %d", list.Len(), list.Ref(), ref)
	}
	for i, id := range items {
		if list.At(i).ID != NodeID(id) {
			t.Errorf("item %d is %d, want %d", i, list.At(i).ID, id)
		}
	}
	if got := list.Prefix(2); got.Len() != 2 || got.At(1) != list.At(1) {
		t.Errorf("the first two items are %d long", got.Len())
	}
	var seen []NodeID
	for i, e := range list.All() {
		if i != len(seen) {
			t.Errorf("item %d came at index %d", len(seen), i)
		}
		seen = append(seen, e.ID)
	}
	if !slices.Equal(seen, []NodeID{1, 2, 3}) {
		t.Errorf("All gave %v", seen)
	}
}

func TestTheEmptyListIsTheZeroName(t *testing.T) {
	tree := NewTree(0, "", 0)
	if ref := tree.AppendList(nil); ref != 0 {
		t.Errorf("an empty list is named %d, want 0", ref)
	}
	list := ListAt[Stmt](tree, 0)
	if list.Len() != 0 || list.Ref() != 0 {
		t.Errorf("the empty list has %d items and the name %d", list.Len(), list.Ref())
	}
	if got := ListAt[Stmt](tree, 0).Prefix(0); got.Len() != 0 {
		t.Error("a prefix of nothing is something")
	}
}

func TestTextsAreNamesInTheModulesText(t *testing.T) {
	tree := NewTree(0, "geometry.units x 'a b'", 0)
	ref := tree.AppendList([]uint32{TextRef(0), TextRef(9), TextRef(15)})
	list := TextListAt(tree, ref)
	if got := list.Strings(); !slices.Equal(got, []string{"geometry", "units", "x"}) {
		t.Errorf("the names are %q", got)
	}
	if got := tree.LabelAt(TextRef(18)); got != "a b" {
		t.Errorf("the label is %q, want %q", got, "a b")
	}
	if tree.NameAt(0) != "" || tree.LabelAt(0) != "" {
		t.Error("no text is not empty text")
	}
}

func TestAStringLiteralKeepsRunsEscapesAndInterpolations(t *testing.T) {
	tree := NewTree(0, `"ab{x}"`, 0)
	x := tree.NewNameExpr(4)
	lit := tree.NewStrLit(0)
	var b StrPartsBuilder
	b.Run(1, 2)
	b.Interpolation(x.Expr())
	b.Decoded(tree, "tab\t")
	lit.SetPartsRef(b.Finish(tree))
	parts := lit.Parts()
	if parts.Len() != 3 || parts.IsPlain() || lit.IsPlain() {
		t.Fatalf("the literal has %d parts, plain %v", parts.Len(), parts.IsPlain())
	}
	if p := parts.At(0); p.Text != "ab" || !p.Expr.IsNil() {
		t.Errorf("the first part is %+v", p)
	}
	if p := parts.At(1); p.Expr != x.Expr() {
		t.Errorf("the second part is %+v", p)
	}
	if p := parts.At(2); p.Text != "tab\t" || !p.Expr.IsNil() {
		t.Errorf("the third part is %+v", p)
	}
	var empty StrPartsBuilder
	empty.Run(1, 0)
	plain := tree.NewStrLit(0)
	plain.SetPartsRef(empty.Finish(tree))
	if !plain.IsPlain() || plain.Parts().At(0).Text != "" {
		t.Error("an empty string is not a plain run of nothing")
	}
}

func TestAMemberBecomesTheNameThatItsPathEndsAtInPlace(t *testing.T) {
	text := "geometry.area(x)"
	tree := NewTree(0, text, 0)
	head := tree.NewNameExpr(0)
	head.SetNameRef(TextRef(0))
	member := tree.NewMemberExpr(8)
	member.SetObj(head.Expr())
	member.SetNameRef(TextRef(9))
	call := tree.NewCallExpr(13)
	call.SetCallee(member.Expr())
	before := tree.Count()

	name := member.BecomeName(head)
	if name.ID != member.ID || tree.Count() != before {
		t.Errorf("the rewrite made node %d among %d, want the member %d in place", name.ID, tree.Count(), member.ID)
	}
	if name.Kind() != KindNameExpr || name.Name() != "area" || name.Position().Off != 0 {
		t.Errorf("the name is a %v called %q at %d, want a name 'area' where the path began", name.Kind(), name.Name(), name.Position().Off)
	}
	if got, ok := call.Callee().AsNameExpr(); !ok || got.ID != member.ID {
		t.Error("what held the member no longer holds the name")
	}
	if _, ok := call.Callee().AsMemberExpr(); ok {
		t.Error("the callee is still a member")
	}
}

func TestCaptureRefsAreCopiesOfAName(t *testing.T) {
	tree := NewTree(0, "count", 0)
	use := tree.NewNameExpr(0)
	use.SetNameRef(TextRef(0))
	tree.AppendCaptureRefs(use, 2)
	first, second := use.CaptureRef(0), use.CaptureRef(1)
	if second.ID != first.ID+1 || first.Name() != "count" || second.Position().Off != 0 || first.ID == use.ID {
		t.Errorf("the copies are %d and %d of %d, named %q", first.ID, second.ID, use.ID, first.Name())
	}
	if tree.Count() != 3 {
		t.Errorf("the tree has %d nodes, want the name and two copies", tree.Count())
	}
	other := tree.NewNameExpr(0)
	defer func() {
		if recover() == nil {
			t.Error("a name that resolution made no copies for gave one")
		}
	}()
	other.CaptureRef(0)
}

func TestSlotsAreNumberedFromTheCounterOfTheirKindInTheOrderOfTheTree(t *testing.T) {
	tree := NewTree(0, "", 0)
	name := tree.NewNameExpr(0)
	call := tree.NewCallExpr(0)
	lit := tree.NewIntLit(0)
	decl := tree.NewFuncDecl(0)
	call2 := tree.NewCallExpr(0)
	member := tree.NewMemberExpr(0)
	slots := make([]uint32, len(tree.Records))
	count := tree.NumberSlots(slots)
	if slots[name.ID] != 1 || slots[call.ID] != 1 || slots[call2.ID] != 2 || slots[lit.ID] != 0 || slots[decl.ID] != 0 || slots[member.ID] != 1 {
		t.Errorf("the slots are %v", slots)
	}
	if count.Name != 1 || count.Call != 2 || count.Member != 1 || count.StructLit != 0 {
		t.Errorf("the counts are %+v, want 1 name, 2 calls and 1 member", count)
	}
	member.BecomeName(name)
	after := tree.NumberSlots(make([]uint32, len(tree.Records)))
	if after.Name != 2 || after.Member != 0 {
		t.Errorf("after a member became a name the counts are %+v, want 2 names and no member", after)
	}
}

func TestAViewOfASetKnowsTheKindOfItsNode(t *testing.T) {
	tree := NewTree(0, "", 0)
	lit := tree.NewIntLit(0)
	e := lit.Expr()
	if _, ok := e.AsNameExpr(); ok {
		t.Error("an integer is a name")
	}
	if got, ok := e.AsIntLit(); !ok || got.ID != lit.ID || e.Kind() != KindIntLit {
		t.Error("an integer is not an integer")
	}
	if k := tree.NewReturnStmt(0).Stmt().Kind(); k != KindReturnStmt {
		t.Errorf("a return is a %v", k)
	}
}
