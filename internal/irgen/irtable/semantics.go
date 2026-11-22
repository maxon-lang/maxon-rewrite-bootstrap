package irtable

// What each operation computes, on the 64 bits of a word, in a closed form the generator writes self/ir/evaluate_gen.maxon
// from. An operation either computes nothing from its operands alone (noEval), is one hand-written function of them (a
// `call`), or is given by the case of one of its enum operands, each case stating what it computes (a caseEval). The
// algorithms the forms name (the wraps, the shift, the floats) stay in self/ir/evaluate.maxon, where the generated code
// calls them by name.

// evalForm is how an operation is computed.
type evalForm int

const (
	evalUnset  evalForm = iota // refused: every row says what it computes
	evalNone                   // computes nothing from its operand words alone: memory, calls, addresses, control
	evalByCase                 // the case of the enum operand Of says what it computes
	evalCall                   // a hand-written function of its operands
)

// domain is what the operands of an operation that its cases compute are.
type domain int

const (
	overWords   domain = iota // 64-bit integers, signed or unsigned as the operation's Unsigned flag says
	overDoubles               // the doubles whose bits the words hold
)

// eval says what an operation computes.
type eval struct {
	Form evalForm
	// Of is the enum operand whose case says what it computes, for evalByCase.
	Of   string
	Over domain
	// Func is the hand-written function of an evalCall that gives the value, with the operation's operands (the words as
	// numbers, the flags by name), and Fits, when there, the one that says whether the value fits, for an operation that
	// can be Checked. Of cases that hold for orders of doubles, Func is the function that gives the order of two doubles.
	Func, Fits string
}

var noEval = eval{Form: evalNone}

func byCase(of string, over domain) eval { return eval{Form: evalByCase, Of: of, Over: over} }

// byOrder is an operation whose cases are the orders of two doubles that the function fn gives.
func byOrder(of, fn string) eval { return eval{Form: evalByCase, Of: of, Over: overDoubles, Func: fn} }

func call(fn string) eval { return eval{Form: evalCall, Func: fn} }

func checkedCall(fn, fits string) eval { return eval{Form: evalCall, Func: fn, Fits: fits} }

// caseForm is what one case of an enum computes.
type caseForm int

const (
	caseNone    caseForm = iota // the enum's cases are not computed
	caseWrap                    // a op b modulo 2^64, whose exact result may not fit
	caseDivide                  // a quotient or a remainder, truncated toward zero
	caseBitwise                 // a bit by bit operation
	caseShift                   // a shift by a count from 0 on
	caseOrder                   // whether the order of a and b is one of those the case holds for
	caseDouble                  // a function of the doubles
	caseRange                   // whether a is in a range of the bound
	caseHigh                    // the high 64 bits of the 128-bit product, which always fit
)

// order is how two numbers compare; a double can also be unordered, when one is a NaN.
type order int

const (
	ordLess order = iota
	ordEqual
	ordGreater
	ordUnordered
)

// bitOp is a bit by bit operation.
type bitOp int

const (
	bitAnd bitOp = iota
	bitOr
	bitXor
)

// caseEval is what one case of an enum computes. A case of Form caseWrap has Wrap, the function of two 64-bit numbers
// that gives a op b modulo 2^64, and FitsUnsigned and FitsSigned, the functions of a, b and that result that say whether
// the exact result fits when the operands are read as unsigned or as signed. A caseOrder holds for the orders Holds. A
// caseRange holds when a compares to its bound, or to 0 when Zero is set, by one of the orders Holds, as unsigned
// numbers when Unsigned is set. A caseHigh is given by Func, of a, b and whether they are read as unsigned.
type caseEval struct {
	Form                     caseForm
	Wrap                     string
	FitsUnsigned, FitsSigned string
	Remainder                bool
	Bits                     bitOp
	Left                     bool
	Holds                    []order
	Func                     string
	Zero, Unsigned           bool
}

var noCase = caseEval{}

func wrapping(wrap, fitsUnsigned, fitsSigned string) caseEval {
	return caseEval{Form: caseWrap, Wrap: wrap, FitsUnsigned: fitsUnsigned, FitsSigned: fitsSigned}
}

func quotient() caseEval { return caseEval{Form: caseDivide} }

func remainder() caseEval { return caseEval{Form: caseDivide, Remainder: true} }

func bitwise(b bitOp) caseEval { return caseEval{Form: caseBitwise, Bits: b} }

func shift(left bool) caseEval { return caseEval{Form: caseShift, Left: left} }

func holdsFor(orders ...order) caseEval { return caseEval{Form: caseOrder, Holds: orders} }

func double(fn string) caseEval { return caseEval{Form: caseDouble, Func: fn} }

func highProduct(fn string) caseEval { return caseEval{Form: caseHigh, Func: fn} }

func inRange(zero, unsigned bool, orders ...order) caseEval {
	return caseEval{Form: caseRange, Zero: zero, Unsigned: unsigned, Holds: orders}
}
