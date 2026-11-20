package backend

import (
	"testing"

	"minimaxon/internal/ir"
)

// The array runtime defines every entry the IR can call, and only calls out to the allocator and the panic routine
// that the runtime unit has, so linking it with that unit leaves nothing undefined.
func TestArrayRuntimeDefinesItsEntries(t *testing.T) {
	u := EmitArrayRuntime()
	for _, entry := range []ir.Rt{ir.RtArrayNew, ir.RtArrayLen, ir.RtArrayElem, ir.RtArrayInsert, ir.RtArrayRemove, ir.RtPanicIndex} {
		if !definesCode(u, entry) {
			t.Errorf("the array runtime does not define %s", entry)
		}
		if !ir.IsNativeEntry(entry) && entry != ir.RtPanicIndex {
			t.Errorf("%s is not a native entry", entry)
		}
	}
	external := map[any]bool{ir.RtAlloc: true, ir.RtFree: true, ir.RtPanic: true}
	for _, f := range u.Asm.SymbolFixups {
		if !definesCode(u, f.Symbol) && !external[f.Symbol] {
			t.Errorf("the array runtime refers to %v, which no unit it links with defines", f.Symbol)
		}
	}
}

// Building the unit twice gives the same bytes: nothing in it depends on the order of a map.
func TestArrayRuntimeIsDeterministic(t *testing.T) {
	a, b := EmitArrayRuntime(), EmitArrayRuntime()
	if string(a.Asm.Code) != string(b.Asm.Code) || string(a.Data.Bytes()) != string(b.Data.Bytes()) {
		t.Error("two builds of the array runtime differ")
	}
}
