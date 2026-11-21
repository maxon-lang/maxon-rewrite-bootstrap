package backend

import (
	"testing"

	"minimaxon/internal/ir"
)

func TestOsRuntimeDefinesItsEntries(t *testing.T) {
	u := EmitOsRuntime()
	for _, entry := range []ir.Rt{
		ir.RtOsArgs,
		ir.RtOsEnvironmentVariable,
		ir.RtOsPathFrom,
		ir.RtOsReadText,
		ir.RtOsWriteText,
		ir.RtOsWriteBinary,
		ir.RtOsRename,
		ir.RtOsDelete,
		ir.RtOsIsDirectory,
		ir.RtOsCreateDirectory,
		ir.RtOsList,
		ir.RtOsRun,
	} {
		if !definesCode(u, entry.String()) {
			t.Errorf("the OS runtime does not define %s", entry)
		}
		if !ir.IsNativeEntry(entry) || !ir.IsOsEntry(entry) {
			t.Errorf("%s is not a native OS entry", entry)
		}
	}
	external := map[string]bool{
		ir.RtAlloc.String():       true,
		ir.RtFree.String():        true,
		ir.RtPanic.String():       true,
		ir.RtArrayNew.String():    true,
		ir.RtArrayLen.String():    true,
		ir.RtArrayElem.String():   true,
		ir.RtArrayInsert.String(): true,
	}
	for _, f := range u.Asm.SymbolFixups {
		if !definesCode(u, f.Symbol) && !external[f.Symbol] {
			t.Errorf("the OS runtime refers to %v, which no unit it links with defines", f.Symbol)
		}
	}
}

func TestOsRuntimeIsDeterministic(t *testing.T) {
	a, b := EmitOsRuntime(), EmitOsRuntime()
	if string(a.Asm.Code) != string(b.Asm.Code) || string(a.Data.Bytes()) != string(b.Data.Bytes()) {
		t.Error("two builds of the OS runtime differ")
	}
}
