package ast

import "testing"

func TestSlabNodesKeepTheirAddressAsItGrows(t *testing.T) {
	var arena Arena
	var nodes []*NameExpr
	for i := range 10 * maxSlabNodes {
		n := arena.NewNameExpr()
		n.Name = string(rune('a' + i%26))
		nodes = append(nodes, n)
	}
	for i, n := range nodes {
		if n.Name != string(rune('a'+i%26)) {
			t.Fatalf("node %d was overwritten: %q", i, n.Name)
		}
		if n.Ty() != VoidTy {
			t.Fatalf("node %d does not start with the type %v", i, VoidTy)
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

func TestArenaNumbersNodesFromOneInTheOrderTheyAreMade(t *testing.T) {
	arena := NewArena(7)
	name := arena.NewNameExpr()
	decl := arena.NewFuncDecl()
	call := arena.NewCallExpr()
	for i, head := range []Head{name.Head, decl.Head, call.Head} {
		if head.ID != NodeID(i+1) || head.Mod != 7 {
			t.Errorf("node %d is %d of module %d, want %d of module 7", i, head.ID, head.Mod, i+1)
		}
	}
	if arena.Count() != 3 {
		t.Errorf("the arena made 3 nodes and counts %d", arena.Count())
	}
}

func TestSealedArenaMakesSmallChunks(t *testing.T) {
	var arena Arena
	for range 3 * maxSlabNodes {
		arena.NewNameExpr()
	}
	arena.Seal()
	late := arena.NewNameExpr()
	if cap(arena.nameExpr.chunk) != firstSlabNodes {
		t.Errorf("a node made after the parse took a chunk of %d, want %d", cap(arena.nameExpr.chunk), firstSlabNodes)
	}
	if late.ID != NodeID(3*maxSlabNodes+1) {
		t.Errorf("the node made after the parse is %d, which should continue the numbering", late.ID)
	}
}
