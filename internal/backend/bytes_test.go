package backend

import (
	"testing"

	"minimaxon/internal/ir"
)

func TestBytesRuntimeDefinesItsEntries(t *testing.T) {
	u := EmitBytesRuntime()
	for _, entry := range []ir.Rt{
		ir.RtBytesNew,
		ir.RtBytesLen,
		ir.RtBytesGet,
		ir.RtBytesSet,
		ir.RtBytesInsert,
		ir.RtBytesRemove,
		ir.RtBytesAppend,
		ir.RtBytesAppendStr,
		ir.RtBytesToString,
	} {
		if !definesCode(u, entry.String()) {
			t.Errorf("the bytes runtime does not define %s", entry)
		}
		if !ir.IsNativeEntry(entry) || !ir.IsBytesEntry(entry) {
			t.Errorf("%s is not a native bytes entry", entry)
		}
	}
	external := map[string]bool{ir.RtAlloc.String(): true, ir.RtFree.String(): true, ir.RtPanic.String(): true}
	for _, f := range u.Asm.SymbolFixups {
		if !definesCode(u, f.Symbol) && !external[f.Symbol] {
			t.Errorf("the bytes runtime refers to %v, which no unit it links with defines", f.Symbol)
		}
	}
}

func TestBytesRuntimeIsDeterministic(t *testing.T) {
	a, b := EmitBytesRuntime(), EmitBytesRuntime()
	if string(a.Asm.Code) != string(b.Asm.Code) || string(a.Data.Bytes()) != string(b.Data.Bytes()) {
		t.Error("two builds of the bytes runtime differ")
	}
}
