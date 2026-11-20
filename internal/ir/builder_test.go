package ir

import (
	"slices"
	"testing"

	"minimaxon/internal/ast"
)

// Blocks are numbered after the unreachable ones are dropped, and the targets of a switch are renumbered with them, though
// its cases live in a pool and not in the terminator.
func TestFinishRenumbersTheTargetsOfASwitch(t *testing.T) {
	var b Builder
	b.Start(&ast.FuncDecl{Name: "f"}, "", 0)
	entry, dead, one, other := b.NewBlock(), b.NewBlock(), b.NewBlock(), b.NewBlock()
	v := b.F.NewReg(KindScalar)
	b.Emit(entry, Const(v, 1))
	b.Emit(dead, Const(v, 2))
	b.Emit(other, Const(v, 3))
	b.Emit(one, Const(v, 4))
	b.Terminate(entry, b.Switch(v, []int64{7}, []BlockID{one}, other))
	b.Terminate(dead, Trap())
	b.Terminate(one, Ret(v))
	b.Terminate(other, Ret(v))
	f := b.Finish(nil)

	want := "function f params 0\n" +
		"b0:\n  %0 = const 1\n  switch %0 [7: b1] default b2\n" +
		"b1:\n  %0 = const 4\n  ret %0\n" +
		"b2:\n  %0 = const 3\n  ret %0\n\n"
	if got := irText(f); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// A reachable block that nothing ended is a bug in whoever built the function, not a block to guess a terminator for.
func TestFinishRefusesABlockWithoutATerminator(t *testing.T) {
	var b Builder
	b.Start(&ast.FuncDecl{Name: "f"}, "", 0)
	b.NewBlock()
	defer func() {
		if recover() == nil {
			t.Fatal("a function with an unterminated block was finished")
		}
	}()
	b.Finish(nil)
}

// A worker lowers every function of every module over the same buffers, so what an earlier function left in them must
// never show in a later one: each function lowered after others, with the arena reset in between, is the IR it is alone.
func TestLoweringOverReusedBuffersGivesTheSameIr(t *testing.T) {
	results := frontEnd(t, "testdata/reuse.maxon")
	instances := Instances(results[0].Callers)
	var alone []string
	for _, f := range results[0].Functions {
		if len(f.Generics) == 0 {
			alone = append(alone, irText(Lower(f, instances)))
		}
	}
	w := NewLowering()
	for round := range 3 {
		var reused []string
		for _, f := range results[0].Functions {
			if len(f.Generics) == 0 {
				reused = append(reused, irText(w.Lower(f, instances)))
			}
		}
		if !slices.Equal(alone, reused) {
			t.Fatalf("round %d: the IR over reused buffers differs from the IR of each function alone", round)
		}
		w.Reset()
	}
}

// After its buffers have grown to a module, a lowerer allocates nothing for the same module again.
func TestLoweringAReusedModuleAllocatesNothing(t *testing.T) {
	results := frontEnd(t, "testdata/reuse.maxon")
	instances := Instances(results[0].Callers)
	w := NewLowering()
	lowerAll := func() {
		w.Reset()
		for _, f := range results[0].Functions {
			w.Lower(f, instances)
		}
	}
	lowerAll()
	if allocs := testing.AllocsPerRun(20, lowerAll); allocs > 0 {
		t.Errorf("lowering a module again allocated %v times", allocs)
	}
}
