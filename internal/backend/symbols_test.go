package backend

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// testSymbols are symbols of each kind a unit defines: a runtime name, a function and the routine of a type, as text.
func testSymbols(n int) []string {
	symbols := make([]string, 0, n)
	for i := range n {
		switch i % 3 {
		case 0:
			symbols = append(symbols, fmt.Sprintf("symbol %d", i))
		case 1:
			symbols = append(symbols, fmt.Sprintf("export::function%d", i))
		default:
			symbols = append(symbols, routineSymbol(routineKind(i%2), fmt.Sprintf("export::Type%d", i)))
		}
	}
	return symbols
}

func TestSymbolTableFindsWhatWasDefinedInParallel(t *testing.T) {
	symbols := testSymbols(3000)
	table := newSymbolTable(len(symbols))
	var wg sync.WaitGroup
	const workers = 8
	for w := range workers {
		wg.Go(func() {
			for i := w; i < len(symbols); i += workers {
				table.define(symbolID(i+1), symbols[i], false)
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
}

func TestSymbolTableRefusesASymbolDefinedTwice(t *testing.T) {
	table := newSymbolTable(2)
	table.define(1, "twice", false)
	message := catchInternal(t, func() { table.define(2, "twice", false) })
	if !strings.Contains(message, "defined twice") || !strings.Contains(message, "twice") {
		t.Errorf("got %q", message)
	}
}

// Units define an instance at the same time, and the table ends up with the lowest id, however the definitions were
// scheduled; the other ids stay undefined as far as a lookup goes.
func TestSymbolTableKeepsTheLowestIdOfAnInstance(t *testing.T) {
	const definitions = 64
	const instance = "export::swap[stdlib::Count]"
	for range 100 {
		table := newSymbolTable(definitions + 1)
		var wg sync.WaitGroup
		for id := symbolID(definitions); id >= 1; id-- {
			wg.Go(func() { table.define(id, instance, true) })
		}
		wg.Go(func() { table.define(definitions+1, "own", false) })
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
	const plain = "export::helper"
	table := newSymbolTable(2)
	table.define(1, plain, false)
	message := catchInternal(t, func() { table.define(2, plain, false) })
	if !strings.Contains(message, "defined twice") || !strings.Contains(message, "helper") {
		t.Errorf("got %q", message)
	}
}
