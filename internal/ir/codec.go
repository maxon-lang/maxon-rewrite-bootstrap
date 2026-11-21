package ir

import (
	"math"
	"minimaxon/internal/cache"
)

// The fields of a record are written as zigzag varints, so that NoReg, NoBlock and NoRoutine (-1) and a negative
// constant take one byte or a few, like the small numbers that most fields hold.

const zigzagSignShift = 63

func putSigned(e *cache.Encoder, v int64) { e.Uvarint(uint64(v<<1) ^ uint64(v>>zigzagSignShift)) }

func signed(d *cache.Decoder) int64 {
	u := d.Uvarint()
	return int64(u>>1) ^ -int64(u&1)
}

// signedIndex is a field that holds a register, a block or an index into a pool, each at least -1 and an int32.
func signedIndex(d *cache.Decoder) int32 {
	v := signed(d)
	if v < -1 || v > math.MaxInt32 {
		d.Fail()
		return 0
	}
	return int32(v)
}
