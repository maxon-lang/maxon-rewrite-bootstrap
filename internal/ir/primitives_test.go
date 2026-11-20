package ir

import (
	"strings"
	"testing"

	"minimaxon/internal/ast"
)

const primitivesFile = "testdata/primitives.maxon"

func lowerNamed(t *testing.T, results []ast.ModuleResult, name string) *IrFunction {
	t.Helper()
	for _, f := range results[0].Functions {
		if f.Name == name {
			return Lower(f, Instances(results[0].Callers))
		}
	}
	t.Fatalf("no function %s", name)
	return nil
}

func irText(f *IrFunction) string {
	var sb strings.Builder
	PrintIr(f, &sb)
	return sb.String()
}

// An array is lowered to the array's own runtime entries, which the backend implements, so nothing is refused.
func TestArrayOperationsLowerToNativeEntries(t *testing.T) {
	results := frontEnd(t, primitivesFile)
	sum := lowerNamed(t, results, "sumOf")
	text := irText(sum)
	for _, entry := range []string{"runtime rt.array.len(", "runtime rt.array.elem("} {
		if !strings.Contains(text, entry) {
			t.Errorf("sumOf does not call %s:\n%s", entry, text)
		}
	}
	if !sum.UsesArrays || len(sum.Refusals) != 0 {
		t.Errorf("sumOf: UsesArrays=%v refusals=%v", sum.UsesArrays, sum.Refusals)
	}
	drop := irText(lowerNamed(t, results, "drop"))
	for _, entry := range []string{"rt.array.remove(", "rt.array.elem(", "storeat", "alloc 16"} {
		if !strings.Contains(drop, entry) {
			t.Errorf("drop does not contain %s:\n%s", entry, drop)
		}
	}
}

// A map, a pool and a list are lowered to runtime entries that only the interpreter has. The IR is whole, and each
// place that needs one is recorded for the driver to refuse, once for each place and in source order.
func TestInterpreterOnlyEntriesAreRecordedAsRefusals(t *testing.T) {
	results := frontEnd(t, primitivesFile)
	lookup := lowerNamed(t, results, "lookup")
	if len(lookup.Refusals) != 1 || lookup.UsesArrays || !strings.Contains(irText(lookup), "runtime rt.map.elem(") {
		t.Fatalf("lookup: refusals=%v\n%s", lookup.Refusals, irText(lookup))
	}
	if r := lookup.Refusals[0]; r.Code != "E9002" || r.Site.P().Line != 15 {
		t.Errorf("refusal %+v at line %d", r, r.Site.P().Line)
	}
	build := lowerNamed(t, results, "build")
	lines := make([]int, len(build.Refusals))
	for i, r := range build.Refusals {
		lines[i] = r.Site.P().Line
	}
	if len(lines) != 4 || lines[0] >= lines[1] || lines[1] >= lines[2] || lines[2] >= lines[3] {
		t.Errorf("refusal lines %v", lines)
	}
	if !strings.Contains(irText(build), "copy %") {
		t.Errorf("a pool is copied by the interpreter:\n%s", irText(build))
	}
}

func TestOnlyTheArrayEntriesAreNative(t *testing.T) {
	for _, symbol := range []string{RtArrayNew, RtArrayLen, RtArrayElem, RtArrayInsert, RtArrayRemove} {
		if !IsNativeEntry(symbol) {
			t.Errorf("%s should be native", symbol)
		}
	}
	for _, symbol := range []string{RtMapElem, RtListPushBack, RtPoolInsert, RtPoolCopy, RtAlloc} {
		if IsNativeEntry(symbol) {
			t.Errorf("%s should not be a native primitive entry", symbol)
		}
	}
}
