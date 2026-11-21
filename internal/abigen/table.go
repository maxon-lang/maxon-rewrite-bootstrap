package main

// The ABI layouts table: what a box of each kind holds and at which offset, which the compiler's code (self/ir and
// self/backend) and the runtime (runtime/) must agree on. Each row says which of them get the constant, so none keeps a
// copy that can drift.

// dest is a place a constant is written to.
type dest uint8

const (
	toRuntime  dest = 1 << iota // runtime/layout_gen.maxon
	toSelfIR                    // self/ir/layout_gen.maxon
	toSelfBack                  // self/backend/layout_gen.maxon
)

// layoutConst is one named number of the ABI.
type layoutConst struct {
	name string
	// words is the value in words, or the value itself when plain is set.
	value int
	// plain is a number that is not a multiple of the word size: the word size itself, a count of elements.
	plain bool
	to    dest
	doc   string
}

// wordBytesName is the row that every other row's words are counted in.
const wordBytesName = "wordBytes"

// wordSize is the size of a word in bytes.
const wordSize = 8

var layouts = []layoutConst{
	{wordBytesName, wordSize, true, toRuntime | toSelfIR | toSelfBack,
		"the size of a word: an address, a length, an integer, a handle, and each field of a box."},

	{"noBox", 0, true, toRuntime,
		"the empty place: a box address that names nothing, which a move leaves behind and a free skips."},

	{"tagOffset", 0, false, toSelfIR,
		"where a union box holds its case index; the payload follows it."},

	{"stringLengthOffset", 0, false, toRuntime | toSelfBack,
		"where a String's box holds its length in bytes, a word."},
	{"stringBytesOffset", 1, false, toRuntime | toSelfBack,
		"where a String's bytes begin, UTF-8 with no terminator."},

	// A byte array's box is an array's box, so its routines read the same offsets; only its buffer differs.
	{"arrayLengthOffset", 0, false, toRuntime | toSelfBack,
		"where the box of an array, and of a byte array, holds its length, a count of elements."},
	{"arrayCapacityOffset", 1, false, toRuntime | toSelfBack,
		"where the box of an array, and of a byte array, holds its capacity, a count of elements."},
	{"arrayDataOffset", 2, false, toRuntime | toSelfBack,
		"where the box of an array, and of a byte array, holds the address of its buffer, which has a word of 8 bytes to an element of an array and a byte to an element of a byte array."},
	{"arrayHeaderBytes", 3, false, toRuntime | toSelfBack,
		"the size of the box of an array and of a byte array."},
	{"arrayFirstCapacity", 4, true, toRuntime,
		"the capacity of an empty array's first buffer, which it doubles from."},

	// A closure is a counted box: a function value is one word, the address of it, and copying the value shares the box.
	{"closureCountOffset", 0, false, toSelfBack,
		"where the box of a closure holds how many function values share it, which it is freed with when that reaches 0."},
	{"closureCodeOffset", 1, false, toSelfBack,
		"where the box of a closure holds the address of its code, which a call through the value calls with the box first."},
	{"closureDropOffset", 2, false, toSelfBack,
		"where the box of a closure holds the address of the routine that drops what it owns, or 0 when it owns nothing."},
	{"closureCapturesOffset", 3, false, toSelfIR | toSelfBack,
		"where the box of a closure holds what it captured, a word each, in the order of its body's captures."},

	{"byteBytes", 1, true, toRuntime | toSelfBack,
		"the size of an element of a byte array."},
	{"bytesFirstCapacity", 16, true, toRuntime,
		"the capacity of an empty byte array's first buffer, which it doubles from."},
}
