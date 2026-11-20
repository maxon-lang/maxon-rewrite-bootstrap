package ast

import (
	"math"
	"math/big"
	"math/bits"
)

type I128 struct {
	Hi int64
	Lo uint64
}

func I64(v int64) I128 {
	if v < 0 {
		return I128{Hi: -1, Lo: uint64(v)}
	}
	return I128{Lo: uint64(v)}
}

func U64(v uint64) I128 { return I128{Lo: v} }

var (
	I64Min = I64(math.MinInt64)
	I64Max = I64(math.MaxInt64)
	U64Max = U64(math.MaxUint64)
	Zero   = I128{}
	One    = I64(1)
)

func (a I128) Cmp(b I128) int {
	switch {
	case a.Hi < b.Hi:
		return -1
	case a.Hi > b.Hi:
		return 1
	case a.Lo < b.Lo:
		return -1
	case a.Lo > b.Lo:
		return 1
	}
	return 0
}

func (a I128) Lt(b I128) bool { return a.Cmp(b) < 0 }
func (a I128) Le(b I128) bool { return a.Cmp(b) <= 0 }
func (a I128) Gt(b I128) bool { return a.Cmp(b) > 0 }
func (a I128) Ge(b I128) bool { return a.Cmp(b) >= 0 }
func (a I128) IsZero() bool   { return a.Hi == 0 && a.Lo == 0 }
func (a I128) IsNeg() bool    { return a.Hi < 0 }

func (a I128) Add(b I128) I128 {
	lo, carry := bits.Add64(a.Lo, b.Lo, 0)
	return I128{Hi: a.Hi + b.Hi + int64(carry), Lo: lo}
}

func (a I128) Sub(b I128) I128 {
	lo, borrow := bits.Sub64(a.Lo, b.Lo, 0)
	return I128{Hi: a.Hi - b.Hi - int64(borrow), Lo: lo}
}

func (a I128) Neg() I128 { return Zero.Sub(a) }

func MaxI(a, b I128) I128 {
	if a.Gt(b) {
		return a
	}
	return b
}

func MinI(a, b I128) I128 {
	if a.Lt(b) {
		return a
	}
	return b
}

func (a I128) Bits() int64 { return int64(a.Lo) }

func (a I128) Big() *big.Int {
	v := new(big.Int).SetUint64(a.Lo)
	if a.Hi != 0 {
		hi := new(big.Int).Lsh(big.NewInt(a.Hi), 64)
		v.Add(v, hi)
	}
	return v
}

var (
	bigI128Min = new(big.Int).Lsh(big.NewInt(-1), 127)
	bigI128Max = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 127), big.NewInt(1))
	bigTwo64   = new(big.Int).Lsh(big.NewInt(1), 64)
)

func FromBig(v *big.Int) (I128, bool) {
	if v.Cmp(bigI128Min) < 0 || v.Cmp(bigI128Max) > 0 {
		return I128{}, false
	}
	lo := new(big.Int).Mod(v, bigTwo64)
	hi := new(big.Int).Sub(v, lo)
	hi.Rsh(hi, 64)
	return I128{Hi: hi.Int64(), Lo: lo.Uint64()}, true
}

func (a I128) String() string { return a.Big().String() }

// saturation is where MulWide stops: past every value a word holds, and far enough inside the range of an I128 that a sum
// of two saturated values still fits.
var saturation = I128{Hi: 1 << 62}

// magnitude is |a| for a whose magnitude is below 2^64, which every value a word holds is.
func (a I128) magnitude() uint64 {
	if a.IsNeg() {
		return a.Neg().Lo
	}
	return a.Lo
}

// withSign is the magnitude made negative when neg is set.
func withSign(m I128, neg bool) I128 {
	if neg {
		return m.Neg()
	}
	return m
}

// MulWide is a * b for two values a word holds, saturated at ±2^126.
func MulWide(a, b I128) I128 {
	hi, lo := bits.Mul64(a.magnitude(), b.magnitude())
	m := I128{Hi: int64(hi), Lo: lo}
	if hi >= uint64(saturation.Hi) {
		m = saturation
	}
	return withSign(m, a.IsNeg() != b.IsNeg())
}

// QuoTrunc is a / b rounded toward zero, as a division of integers computes it, for two values a word holds and a b
// that is not 0.
func QuoTrunc(a, b I128) I128 {
	return withSign(U64(a.magnitude()/b.magnitude()), a.IsNeg() != b.IsNeg())
}
