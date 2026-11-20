package check

import (
	"testing"

	"minimaxon/internal/ast"
)

const rangesPrelude = "testdata/ranges/prelude.maxon"

func TestRangesAccept(t *testing.T) {
	checkCases(t, "testdata/ranges/accepts.maxon", rangesPrelude)
}

func TestRangesRejects(t *testing.T) {
	checkCases(t, "testdata/ranges/rejects.maxon", rangesPrelude)
}

func TestArithmeticFindsTheExactResultsAndTheFailures(t *testing.T) {
	digits := span{lo: ast.Zero, hi: ast.I64(9)}
	signed := span{lo: ast.I64(-4), hi: ast.I64(4)}
	for _, c := range []struct {
		op    string
		l, r  span
		want  span
		fails ast.Fails
	}{
		{"+", digits, digits, span{lo: ast.Zero, hi: ast.I64(18)}, 0},
		{"-", digits, digits, span{lo: ast.I64(-9), hi: ast.I64(9)}, 0},
		{"*", signed, digits, span{lo: ast.I64(-36), hi: ast.I64(36)}, 0},
		{"/", digits, signed, span{lo: ast.I64(-9), hi: ast.I64(9)}, ast.FailZero},
		{"/", signed, span{lo: ast.One, hi: ast.I64(2)}, signed, 0},
		{"mod", signed, span{lo: ast.I64(3), hi: ast.I64(3)}, span{lo: ast.I64(-2), hi: ast.I64(2)}, 0},
		{"shl", digits, span{lo: ast.Zero, hi: ast.One}, span{lo: ast.Zero, hi: ast.I64(18)}, 0},
		{"shr", signed, span{lo: ast.I64(-1), hi: ast.One}, signed, ast.FailShift},
		{"and", digits, span{lo: ast.I64(3), hi: ast.I64(3)}, span{lo: ast.Zero, hi: ast.I64(3)}, 0},
		{"or", digits, digits, span{lo: ast.Zero, hi: ast.I64(15)}, 0},
	} {
		got, fails := arithmetic(c.op, c.l, c.r, signedWord)
		if got.lo != c.want.lo || got.hi != c.want.hi || fails != c.fails {
			t.Errorf("%v %s %v = %v with fails %b, want %v with %b", c.l, c.op, c.r, got, fails, c.want, c.fails)
		}
	}
}

func TestAProductPastEveryWordSaturates(t *testing.T) {
	top := span{lo: ast.U64Max, hi: ast.U64Max}
	got, _ := arithmetic("*", top, top, unsignedWord)
	if !got.lo.Gt(ast.U64Max) || got.within(unsignedWord) {
		t.Errorf("u64.max * u64.max is %v, which should lie past the word", got)
	}
}
