package ast

import (
	"math/big"
	"testing"
)

func TestI128CarryAndBorrow(t *testing.T) {
	top := U64(^uint64(0))
	sum := top.Add(One)
	if sum.Hi != 1 || sum.Lo != 0 {
		t.Fatalf("u64.max + 1 = %v", sum)
	}
	if back := sum.Sub(One); back != top {
		t.Fatalf("(u64.max + 1) - 1 = %v", back)
	}
	if neg := One.Neg(); neg != I64(-1) {
		t.Fatalf("-1 = %+v", neg)
	}
}

func TestI128Bits(t *testing.T) {
	cases := []struct {
		v    I128
		want int64
	}{
		{I64(-1), -1},
		{U64Max, -1},
		{I64Min, -1 << 63},
		{I64(42), 42},
	}
	for _, c := range cases {
		if got := c.v.Bits(); got != c.want {
			t.Errorf("%v.Bits() = %d, want %d", c.v, got, c.want)
		}
	}
}

func TestI128BigRoundTrip(t *testing.T) {
	for _, v := range []I128{I64Min, I64Max, U64Max, I64(-42), Zero} {
		got, ok := FromBig(v.Big())
		if !ok || got != v {
			t.Errorf("FromBig(%v.Big()) = %v, %v", v, got, ok)
		}
	}
	tooBig := new(big.Int).Lsh(big.NewInt(1), 127)
	if _, ok := FromBig(tooBig); ok {
		t.Error("2^127 should not fit")
	}
}

func TestIntTyRanges(t *testing.T) {
	port := &IntTy{Alias: "Port", Min: Zero, Max: U64(65535)}
	count := &IntTy{Alias: "Count", Min: Zero, Max: U64Max}
	if !port.Holds(U64(80)) || port.Holds(I64(-1)) || port.Holds(U64(65536)) {
		t.Error("Port holds the wrong values")
	}
	if !Anonymous64.Covers(port) || port.Covers(Anonymous64) {
		t.Error("Covers is wrong between Port and the full signed range")
	}
	if port.Unsigned() || !count.Unsigned() || !count.IsFullWidth() {
		t.Error("signedness is wrong")
	}
}
