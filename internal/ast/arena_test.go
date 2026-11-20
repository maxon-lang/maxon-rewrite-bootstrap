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
