package ir

import (
	"strings"
	"testing"

	"minimaxon/internal/ast"
)

const primitiveSource = `function sumOf(items array[Count]) returns Count
	var sum = 0 as Count
	for i in 0 upto items.count() 'each'
		sum = sum + items[i]
	end 'each'
	return sum
end 'sumOf'

function drop(items inout array[Count])
	let last = items.pop()
	items[0] = 1
end 'drop'

function lookup(m map[Count, Count], k Count) returns Count
	return m[k]
end 'lookup'

function build() returns Count
	var p = pool[Count].new()
	let h = p.insert(1)
	p[h] = 2
	let copied = p.copy()
	return 0
end 'build'

function main() returns ExitCode
	return 0
end 'main'
`

func lowerNamed(t *testing.T, module *ast.Module, name string) *IrFunction {
	t.Helper()
	for _, f := range module.Functions {
		if f.Name == name {
			return Lower(f)
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
	m := frontEnd(t, primitiveSource)
	sum := lowerNamed(t, m, "sumOf")
	text := irText(sum)
	for _, entry := range []string{"runtime rt.array.len(", "runtime rt.array.elem("} {
		if !strings.Contains(text, entry) {
			t.Errorf("sumOf does not call %s:\n%s", entry, text)
		}
	}
	if !sum.UsesArrays || len(sum.Refusals) != 0 {
		t.Errorf("sumOf: UsesArrays=%v refusals=%v", sum.UsesArrays, sum.Refusals)
	}
	drop := irText(lowerNamed(t, m, "drop"))
	for _, entry := range []string{"rt.array.remove(", "rt.array.elem(", "storeat", "alloc 16"} {
		if !strings.Contains(drop, entry) {
			t.Errorf("drop does not contain %s:\n%s", entry, drop)
		}
	}
}

// A map, a pool and a list are lowered to runtime entries that only the interpreter has. The IR is whole, and each
// place that needs one is recorded for the driver to refuse, once for each place and in source order.
func TestInterpreterOnlyEntriesAreRecordedAsRefusals(t *testing.T) {
	m := frontEnd(t, primitiveSource)
	lookup := lowerNamed(t, m, "lookup")
	if len(lookup.Refusals) != 1 || lookup.UsesArrays || !strings.Contains(irText(lookup), "runtime rt.map.elem(") {
		t.Fatalf("lookup: refusals=%v\n%s", lookup.Refusals, irText(lookup))
	}
	if r := lookup.Refusals[0]; r.Code != "E9002" || r.Site.P().Line != 15 {
		t.Errorf("refusal %+v at line %d", r, r.Site.P().Line)
	}
	build := lowerNamed(t, m, "build")
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
