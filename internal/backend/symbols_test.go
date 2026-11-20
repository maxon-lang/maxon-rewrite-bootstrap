package backend

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"minimaxon/internal/ast"
)

// testSymbols are symbols of each kind a unit defines: a name, a function (a pointer) and the routine of a type.
func testSymbols(n int) []any {
	symbols := make([]any, 0, n)
	pointers := make([]*ast.FuncDecl, n)
	for i := range n {
		pointers[i] = &ast.FuncDecl{}
		switch i % 3 {
		case 0:
			symbols = append(symbols, fmt.Sprintf("symbol %d", i))
		case 1:
			symbols = append(symbols, pointers[i])
		default:
			symbols = append(symbols, typeRoutine{routineKind(i % 2), ast.StringTy})
		}
	}
	return symbols
}

func TestSymbolTableFindsWhatWasDefinedInParallel(t *testing.T) {
	// Two routines of one type are the same symbol, so the routine symbols here repeat; keep one of each.
	var symbols []any
	seen := map[any]bool{}
	for _, s := range testSymbols(3000) {
		if !seen[s] {
			seen[s] = true
			symbols = append(symbols, s)
		}
	}
	table := newSymbolTable(len(symbols))
	var wg sync.WaitGroup
	const workers = 8
	for w := range workers {
		wg.Go(func() {
			for i := w; i < len(symbols); i += workers {
				table.define(symbolID(i+1), symbols[i])
			}
		})
	}
	wg.Wait()
	for i, s := range symbols {
		if got := table.lookup(s); got != symbolID(i+1) {
			t.Fatalf("symbol %d: found id %d", i+1, got)
		}
	}
	if got := table.lookup("symbol that nothing defines"); got != noSymbol {
		t.Errorf("found id %d for a symbol that nothing defines", got)
	}
	if got := table.lookup(&ast.FuncDecl{}); got != noSymbol {
		t.Errorf("found id %d for a function that nothing defines", got)
	}
}

func TestSymbolTableRefusesASymbolDefinedTwice(t *testing.T) {
	table := newSymbolTable(2)
	table.define(1, "twice")
	message := catchInternal(t, func() { table.define(2, "twice") })
	if !strings.Contains(message, "defined twice") || !strings.Contains(message, "twice") {
		t.Errorf("got %q", message)
	}
}

// Units define an instance at the same time, and the table ends up with the lowest id, however the definitions were
// scheduled; the other ids stay undefined as far as a lookup goes.
func TestSymbolTableKeepsTheLowestIdOfAnInstance(t *testing.T) {
	const definitions = 64
	instance := testInstance("swap")
	for range 100 {
		table := newSymbolTable(definitions + 1)
		var wg sync.WaitGroup
		for id := symbolID(definitions); id >= 1; id-- {
			wg.Go(func() { table.define(id, instance) })
		}
		wg.Go(func() { table.define(definitions+1, "own") })
		wg.Wait()
		if got := table.lookup(instance); got != 1 {
			t.Fatalf("a lookup of the instance found id %d, want 1", got)
		}
		if got := table.lookup("own"); got != definitions+1 {
			t.Fatalf("a lookup of the other symbol found id %d", got)
		}
	}
}

func TestSymbolTableRefusesAFunctionThatIsNotAnInstanceDefinedTwice(t *testing.T) {
	plain := &ast.FuncDecl{Name: "helper"}
	table := newSymbolTable(2)
	table.define(1, plain)
	message := catchInternal(t, func() { table.define(2, plain) })
	if !strings.Contains(message, "defined twice") || !strings.Contains(message, "helper") {
		t.Errorf("got %q", message)
	}
}
