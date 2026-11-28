package main

import (
	"strings"
	"testing"
)

func TestConvertCallsTheSharedFormWhereTheWrittenArrayIsAlsoRead(t *testing.T) {
	in := "function limbCarryChain(dst Limbs, dstBase Count, a Limbs, aBase Count, b Limbs, bBase Count, bMask Limb, count Count) returns Limb\n\treturn a[aBase] + b[bBase]\nend 'limbCarryChain'\n\nfunction add(ws Limbs, other Limbs)\n\t_ = limbCarryChain(ws, dstBase: 0, a: ws, aBase: 1, b: other, bBase: 0, bMask: 1, count: 2)\n\t_ = limbCarryChain(ws, dstBase: 0, a: other, aBase: 1, b: other, bBase: 0, bMask: 1, count: 2)\nend 'add'\n"
	res := Convert("x.maxon", in, sourceFile)
	for _, want := range []string{
		"function limbCarryChainSharingA(dst Limbs, dstBase Count, aBase Count, b Limbs, bBase Count, bMask Limb, count Count) returns Limb\n\treturn dst[aBase] + b[bBase]\nend 'limbCarryChainSharingA'\n",
		"function limbCarryChainSharingAB(dst Limbs, dstBase Count, aBase Count, bBase Count, bMask Limb, count Count) returns Limb\n\treturn dst[aBase] + dst[bBase]\nend 'limbCarryChainSharingAB'\n",
		"_ = limbCarryChainSharingA(ws, dstBase: 0, aBase: 1, b: other, bBase: 0, bMask: 1, count: 2)\n",
		"_ = limbCarryChain(ws, dstBase: 0, a: other, aBase: 1, b: other, bBase: 0, bMask: 1, count: 2)\n",
	} {
		if !strings.Contains(res.Text, want) {
			t.Fatalf("missing %q in:\n%s", want, res.Text)
		}
	}
}

func TestConvertCallsTheGeneratedConstantOfARegistryCache(t *testing.T) {
	in := "function table(curve CombCurve) returns WorkspaceLimbs\n\treturn CombCache.table(curve, build: function() gives buildTable(curve))\nend 'table'\n"
	res := Convert("x.maxon", in, sourceFile)
	if !strings.Contains(res.Text, "\treturn combTable(curve)\n") {
		t.Fatalf("the cache's call is the generated function's:\n%s", res.Text)
	}
}
