package irtable

// The table of IR operations: every instruction and terminator, what each of its operands is and which field of the
// record holds it, what it gives, what it does besides, and how the printer writes it. Everything that restates one of
// these facts (the records' accessors and constructors, the printer, the cache codec, the visitors, in Go and in Maxon) is
// generated from here.

// kind is what an operand is, which says how it is read, written, printed and encoded.
type kind int

const (
	kReg      kind = iota // a register the operation reads
	kRegs                 // registers in the function's Args, from the field to the count in Count
	kMoreRegs             // registers in Args right after the operand before, as many as the field says
	kSlot                 // a slot
	kInt                  // a small number: a parameter index, a byte offset or size
	kWord                 // a 64-bit number
	kSymbol               // a runtime symbol (Rt)
	kEnum                 // a value of one of the enums below, in Sub
	kFlag                 // a bit of Flags
	kBlock                // a block the operation can go to
	kFail                 // the block an instruction goes to when its check fails
	kText                 // a string of the function's Strings
	kType                 // a type of the function's Types
	kCallee               // a function of the function's Callees
	kRoutine              // a function of Callees that a runtime entry stores to call later, or none
	kErrorReg             // the register that receives the error of a call of a function that throws
	kBody                 // a body of the function's Bodies: a behaviour's or a closure's
	kDatum                // a datum of the function's Datums
	kFrameBox             // a frame box of the function's frame
	kCases                // cases of the function's Cases, from the field to the count in Count, each with the arguments it passes
	kTable                // a table of the function's Tables: the table of an interface value
	// kFloats says which of a call's Args are floats, a bit for each by its place among them, which native code passes
	// in xmm registers; the interpreter, to which a float is a word like any other, never reads it. It is no field of
	// the record: the function's float pool holds it by where the call's Args start, the field of the list it follows,
	// and holds nothing for a call that passes no float.
	kFloats
	// kEdgeArgs are the registers in Args, from the field to the count in Count, that an edge passes to the parameters of
	// the block it goes to; kMoreEdgeArgs are those of a later edge, right after the kEdgeArgs before it, as many as the
	// field says. An edge that a terminator's plain constructor makes passes none.
	kEdgeArgs
	kMoreEdgeArgs
)

// field is a field of a record. An instruction has dst, a, b, c, imm, sub and flags; a terminator value, target, other,
// first and count.
type field string

const (
	fDst   field = "dst"
	fA     field = "a"
	fB     field = "b"
	fC     field = "c"
	fImm   field = "imm"
	fSub   field = "sub"
	fFlags field = "flags"

	fValue field = "value"
	fThen  field = "target"
	fElse  field = "other"
	fFirst field = "first"
	fCount field = "count"
	// fArgs is where a terminator's edge arguments start in Args; fThenArgs and fElseArgs are how many its edges to
	// target and to other pass.
	fArgs     field = "args"
	fThenArgs field = "targetArgs"
	fElseArgs field = "otherArgs"
)

// operand is one operand of an operation.
type operand struct {
	// Name names the accessor (Go `x.Left()`, Maxon `binLeft(x)`) and the constructor's parameter.
	Name  string
	Kind  kind
	Field field
	// Count is the field that holds how many there are, for a list.
	Count field
	// Enum is the enum of a kEnum.
	Enum string
	// Flag is the flag of a kFlag, or the flag that says a kFail or kErrorReg is there; an operand with no flag of
	// either kind is always there, except a kRoutine, which is absent when its field is NoRoutine.
	Flag string
	// Text is what the printer writes for a flag that is set, or before an optional operand that is there.
	Text string
}

// result is what an operation gives.
type result int

const (
	noResult       result = iota
	valueResult           // a register, always
	optionalResult        // a register, or NoReg for a call whose value nothing reads
)

// effect is what an operation does besides reading its operands and writing its result, which a pass that moves,
// removes or merges operations asks. An operation with a kFail operand may also fail, which the generator adds.
type effect string

const (
	effReads     effect = "Reads"     // reads memory: a slot, a field, a place, a global
	effWrites    effect = "Writes"    // writes memory, or leaves a place empty
	effCalls     effect = "Calls"     // runs other code: a function, a runtime entry, a routine
	effAllocates effect = "Allocates" // allocates a box
	effFrees     effect = "Frees"     // may free a box
	effExits     effect = "Exits"     // leaves the function (a terminator)
)

// effects lists every effect in the order of its bit; effFails, which the generator derives, comes last.
var effects = []effect{effReads, effWrites, effCalls, effAllocates, effFrees, effExits}

const effFails effect = "Fails"

// op is one row: an instruction, or a terminator when Term is set.
type op struct {
	Name     string
	Term     bool
	Doc      string
	Result   result
	Operands []operand
	Effects  []effect
	// Text is the printer's template: `{Name}` is an operand. The result, when there is one, comes before it as `%3 = `.
	Text string
	// Eval says what the operation computes from its operands' words.
	Eval eval
}

// enum is a small closed set an operand names, held in Sub.
type enum struct {
	Name string
	// Prefix is what its constants start with, BinAdd for BinOp's Add.
	Prefix string
	Doc    string
	Cases  []enumCase
}

// enumCase is a value of an enum: its Go name after the prefix, how the printer writes it, and what it computes when
// the operation that has the enum says its cases do.
type enumCase struct {
	Name, Text string
	Eval       caseEval
}

var enums = []enum{
	{Name: "BinOp", Prefix: "Bin", Doc: "BinOp is the operation of a Bin.", Cases: []enumCase{
		{"Add", "add", wrapping("wrapAdd", "addFitsUnsigned", "addFitsSigned")},
		{"Sub", "sub", wrapping("wrapSub", "subFitsUnsigned", "subFitsSigned")},
		{"Mul", "mul", wrapping("wrapMul", "mulFitsUnsigned", "mulFitsSigned")},
		{"Div", "div", quotient()}, {"Mod", "mod", remainder()},
		{"And", "and", bitwise(bitAnd)}, {"Or", "or", bitwise(bitOr)}, {"Xor", "xor", bitwise(bitXor)},
		{"Shl", "shl", shift(true)}, {"Shr", "shr", shift(false)},
		{"MulHi", "mulhi", highProduct("mulHigh")},
	}},
	{Name: "CmpOp", Prefix: "Cmp", Doc: "CmpOp is the comparison of a Cmp.", Cases: []enumCase{
		{"Eq", "eq", holdsFor(ordEqual)}, {"Ne", "ne", holdsFor(ordLess, ordGreater, ordUnordered)},
		{"Lt", "lt", holdsFor(ordLess)}, {"Le", "le", holdsFor(ordLess, ordEqual)},
		{"Gt", "gt", holdsFor(ordGreater)}, {"Ge", "ge", holdsFor(ordGreater, ordEqual)},
	}},
	{Name: "FBinOp", Prefix: "FBin", Doc: "FBinOp is the operation of an FBin.", Cases: []enumCase{
		{"Add", "add", double("ast.floatAdd")}, {"Sub", "sub", double("ast.floatSub")},
		{"Mul", "mul", double("ast.floatMul")}, {"Div", "div", double("ast.floatDiv")},
	}},
	{Name: "FUnaryOp", Prefix: "FUnary", Doc: "FUnaryOp is the operation of an FUnary: the square root, and the rounding to an integer\n// to the nearest (ties to even), toward negative infinity and toward positive infinity.", Cases: []enumCase{
		{"Sqrt", "sqrt", double("ast.floatSqrt")}, {"Round", "round", double("roundNearest")},
		{"Floor", "floor", double("roundDown")}, {"Ceil", "ceil", double("roundUp")},
	}},
	{Name: "GuardKind", Prefix: "Guard", Doc: "GuardKind is the range a Guard checks, against its bound.", Cases: []enumCase{
		{"NonNegative", "NonNegative", inRange(true, false, ordGreater, ordEqual)},
		{"SignedAtLeast", "SignedAtLeast", inRange(false, false, ordGreater, ordEqual)},
		{"SignedAtMost", "SignedAtMost", inRange(false, false, ordLess, ordEqual)},
		{"UnsignedAtLeast", "UnsignedAtLeast", inRange(false, true, ordGreater, ordEqual)},
		{"UnsignedAtMost", "UnsignedAtMost", inRange(false, true, ordLess, ordEqual)},
		{"NonZero", "NonZero", inRange(true, false, ordLess, ordGreater)},
	}},
	{Name: "ReceiverConv", Prefix: "Receiver", Doc: "ReceiverConv is how a CallWitness gives the method the value the box holds: lent, as a receiver that is let\n// or mutates takes a value, or moved out of the box, which is freed.", Cases: []enumCase{
		{"Lent", "lent", noCase}, {"Moved", "moved", noCase},
	}},
	{Name: "RawWidth", Prefix: "Width", Doc: "RawWidth is how many bits a LoadRaw or a StoreRaw moves.", Cases: []enumCase{
		{"8", "8", noCase}, {"16", "16", noCase}, {"32", "32", noCase}, {"64", "64", noCase},
	}},
	{Name: "Collection", Prefix: "Collection", Doc: "Collection is the box a Length or a LoadByte reads: an array's or a byte array's, which share one layout, or a\n// string's.", Cases: []enumCase{
		{"Array", "array", noCase}, {"String", "string", noCase},
	}},
}

// flag is a bit of an instruction's Flags.
type flag struct {
	Name, Doc string
}

var flags = []flag{
	{"Borrow", "Borrow is on a StoreSlot whose slot only names a part of the match subject for the arm, and owns nothing, and on a\n// Box that only lends the value it holds."},
	{"Unsigned", "Unsigned is on a Bin or a Cmp that treats its operands as unsigned."},
	{"Checked", "Checked is on a Bin or a Neg whose result may not fit the word: when it does not, the instruction goes to its\n// failure block instead of to the next instruction."},
	{"Throws", "Throws is on a Call of a function that throws: its error register receives 0 when it returned and the\n// error's case plus 1 when it failed."},
	{"Proven", "Proven is on a CallRuntime of an entry that reads or addresses an element at an index that the checker or a\n// check before it has proved inside the collection: its expansion is the access alone, with no bounds check."},
	{"FloatResult", "FloatResult is on a call whose result is a float, which native code returns in an xmm register."},
}

func reg(name string, f field) operand { return operand{Name: name, Kind: kReg, Field: f} }

func num(name string) operand { return operand{Name: name, Kind: kInt, Field: fImm} }

var (
	slotOp    = operand{Name: "Slot", Kind: kSlot, Field: fImm}
	offsetOp  = num("Offset")
	typeOp    = operand{Name: "Ty", Kind: kType, Field: fC}
	argsOp    = operand{Name: "Args", Kind: kRegs, Field: fA, Count: fB}
	symbolOp  = operand{Name: "Symbol", Kind: kSymbol, Field: fImm}
	checkedOp = operand{Name: "Fail", Kind: kFail, Field: fC, Flag: "Checked", Text: " else "}
	unsignOp  = operand{Name: "Unsigned", Kind: kFlag, Field: fFlags, Flag: "Unsigned", Text: ".u"}
	widthOp   = operand{Name: "Width", Kind: kEnum, Field: fSub, Enum: "RawWidth"}
	// collectionOp is the box a collection operation reads.
	collectionOp = operand{Name: "Of", Kind: kEnum, Field: fSub, Enum: "Collection"}
	// floatsOp and floatResultOp are a call's float signature: which of its arguments and whether its result are floats,
	// as the static signature of what it calls says (README, "The machine IR").
	floatsOp      = operand{Name: "Floats", Kind: kFloats, Field: fA, Text: " floats "}
	floatResultOp = operand{Name: "FloatResult", Kind: kFlag, Field: fFlags, Flag: "FloatResult", Text: " returns float"}
)

var ops = []op{
	{Name: "Const", Eval: noEval, Doc: "Const gives a number.", Result: valueResult,
		Operands: []operand{{Name: "Value", Kind: kWord, Field: fImm}}, Text: "const {Value}"},
	{Name: "Str", Eval: noEval, Doc: "Str gives a string literal.", Result: valueResult,
		Operands: []operand{{Name: "Text", Kind: kText, Field: fC}}, Text: "str {Text}"},
	{Name: "Datum", Eval: noEval, Doc: "Datum gives the constant Item of the function's Datums, laid out once in the executable's data: no one owns\n// it, and it is never written or dropped.", Result: valueResult,
		Operands: []operand{{Name: "Item", Kind: kDatum, Field: fC}}, Text: "datum {Item}"},
	{Name: "LoadSlot", Eval: noEval, Doc: "LoadSlot reads a slot.", Result: valueResult,
		Operands: []operand{slotOp}, Effects: []effect{effReads}, Text: "load {Slot}"},
	{Name: "StoreSlot", Eval: noEval, Doc: "StoreSlot puts a value in a slot, which owns it unless Borrow is set.",
		Operands: []operand{slotOp, reg("Src", fA), {Name: "Borrow", Kind: kFlag, Field: fFlags, Flag: "Borrow", Text: " borrow"}},
		Effects:  []effect{effWrites}, Text: "store {Slot}, {Src}{Borrow}"},
	{Name: "SlotAddr", Eval: noEval, Doc: "SlotAddr gives the address of a slot.", Result: valueResult,
		Operands: []operand{slotOp}, Text: "addr {Slot}"},
	{Name: "LoadParam", Eval: noEval, Doc: "LoadParam reads a parameter.", Result: valueResult,
		Operands: []operand{num("Param")}, Text: "param {Param}"},
	{Name: "LoadField", Eval: noEval, Doc: "LoadField reads the word at Obj + Offset.", Result: valueResult,
		Operands: []operand{reg("Obj", fA), offsetOp}, Effects: []effect{effReads}, Text: "field {Obj}+{Offset}"},
	{Name: "StoreField", Eval: noEval, Doc: "StoreField writes Src to the word at Obj + Offset.",
		Operands: []operand{reg("Obj", fA), offsetOp, reg("Src", fB)}, Effects: []effect{effWrites},
		Text: "setfield {Obj}+{Offset}, {Src}"},
	{Name: "FieldAddr", Eval: noEval, Doc: "FieldAddr gives Obj + Offset.", Result: valueResult,
		Operands: []operand{reg("Obj", fA), offsetOp}, Text: "fieldaddr {Obj}+{Offset}"},
	{Name: "LoadAt", Eval: noEval, Doc: "LoadAt reads the word at an address.", Result: valueResult,
		Operands: []operand{reg("Addr", fA)}, Effects: []effect{effReads}, Text: "loadat {Addr}"},
	{Name: "StoreAt", Eval: noEval, Doc: "StoreAt writes Src to the word at an address.",
		Operands: []operand{reg("Addr", fA), reg("Src", fB)}, Effects: []effect{effWrites}, Text: "storeat {Addr}, {Src}"},
	{Name: "LoadGlobal", Eval: noEval, Doc: "LoadGlobal reads a global of the runtime.", Result: valueResult,
		Operands: []operand{symbolOp}, Effects: []effect{effReads}, Text: "global {Symbol}"},
	// The raw operations are what the runtime's source lowers its __Raw intrinsics to (README, "The runtime"); no other
	// code has them.
	{Name: "LoadRaw", Eval: noEval, Doc: "LoadRaw reads Width bits at an address, zero-extended to the word.", Result: valueResult,
		Operands: []operand{reg("Addr", fA), widthOp}, Effects: []effect{effReads}, Text: "loadraw.{Width} {Addr}"},
	{Name: "StoreRaw", Eval: noEval, Doc: "StoreRaw writes the low Width bits of Src at an address.",
		Operands: []operand{reg("Addr", fA), reg("Src", fB), widthOp}, Effects: []effect{effWrites},
		Text: "storeraw.{Width} {Addr}, {Src}"},
	{Name: "GlobalAddr", Eval: noEval, Doc: "GlobalAddr gives the address of a global of the runtime.", Result: valueResult,
		Operands: []operand{symbolOp}, Text: "globaladdr {Symbol}"},
	{Name: "CallImport", Eval: noEval, Doc: "CallImport calls the DLL import Name, a string of the function, with the words Args, by the\n// Win64 convention, and gives what it returns.",
		Result: valueResult, Operands: []operand{{Name: "Name", Kind: kText, Field: fC}, argsOp},
		Effects: []effect{effReads, effWrites, effCalls}, Text: "import {Name}({Args})"},
	// The atomic operations and the thread's context are what the runtime's scheduler is written on. Each reads and
	// writes memory, so no pass moves another memory operation across one.
	{Name: "AtomicAdd", Eval: noEval, Doc: "AtomicAdd adds Src to the word at an address as one indivisible step, a full fence, and gives the word it found.",
		Result: valueResult, Operands: []operand{reg("Addr", fA), reg("Src", fB)}, Effects: []effect{effReads, effWrites},
		Text: "atomicadd {Addr}, {Src}"},
	{Name: "AtomicExchange", Eval: noEval, Doc: "AtomicExchange puts Src in the word at an address as one indivisible step, a full fence, and gives the word it\n// found.",
		Result: valueResult, Operands: []operand{reg("Addr", fA), reg("Src", fB)}, Effects: []effect{effReads, effWrites},
		Text: "atomicxchg {Addr}, {Src}"},
	{Name: "CompareExchange", Eval: noEval, Doc: "CompareExchange is given the address, the word expected and the word desired in Args: as one indivisible step,\n// a full fence, it puts the desired word at the address when the expected one is there, and gives the word it found.",
		Result: valueResult, Operands: []operand{argsOp}, Effects: []effect{effReads, effWrites}, Text: "cmpxchg({Args})"},
	{Name: "SpinPause", Eval: noEval, Doc: "SpinPause tells the processor that the thread is waiting in a loop for another one.",
		Effects: []effect{effReads, effWrites}, Text: "spinpause"},
	{Name: "ThreadContext", Eval: noEval, Doc: "ThreadContext gives the word the running thread keeps in the runtime's thread-local slot: its context.",
		Result: valueResult, Effects: []effect{effReads}, Text: "threadcontext"},
	{Name: "CallCode", Eval: noEval, Doc: "CallCode calls the code at the address that is the first of Args with the rest of Args, by the internal\n// convention, and gives what it returns.",
		Result: valueResult, Operands: []operand{argsOp}, Effects: []effect{effReads, effWrites, effCalls, effAllocates, effFrees},
		Text: "callcode({Args})"},
	{Name: "CodeAddress", Eval: noEval, Doc: "CodeAddress gives the address of the code of the function Callee, which a CallCode calls: the run of a\n// behaviour's record that a test of the runtime builds by hand.",
		Result: valueResult, Operands: []operand{{Name: "Callee", Kind: kCallee, Field: fC}}, Text: "codeaddr {Callee}"},
	{Name: "Bin", Eval: byCase("Op", overWords), Doc: "Bin is Left Op Right; when Checked, it goes to Fail when the result does not fit, which for a\n// signed '/' is i64.min / -1. MulHi is the high word of the 128-bit product, which a division by a constant is\n// made of, and always fits.",
		Result:   valueResult,
		Operands: []operand{{Name: "Op", Kind: kEnum, Field: fSub, Enum: "BinOp"}, reg("Left", fA), reg("Right", fB), unsignOp, checkedOp},
		Text:     "{Op}{Unsigned} {Left}, {Right}{Fail}"},
	{Name: "Cmp", Eval: byCase("Op", overWords), Doc: "Cmp gives 1 when Left Op Right holds and 0 when it does not.", Result: valueResult,
		Operands: []operand{{Name: "Op", Kind: kEnum, Field: fSub, Enum: "CmpOp"}, reg("Left", fA), reg("Right", fB), unsignOp},
		Text:     "cmp.{Op}{Unsigned} {Left}, {Right}"},
	// A float is the 64 bits of its double in a register like any other word; these operate on the doubles.
	{Name: "FBin", Eval: byCase("Op", overDoubles), Doc: "FBin is Left Op Right on two doubles, rounded to the nearest double, ties to even.", Result: valueResult,
		Operands: []operand{{Name: "Op", Kind: kEnum, Field: fSub, Enum: "FBinOp"}, reg("Left", fA), reg("Right", fB)},
		Text:     "f{Op} {Left}, {Right}"},
	{Name: "FCmp", Eval: byOrder("Op", "ast.floatCompare"), Doc: "FCmp gives 1 when Left Op Right holds of two doubles and 0 when it does not; every comparison with a\n// NaN but ne does not hold.",
		Result:   valueResult,
		Operands: []operand{{Name: "Op", Kind: kEnum, Field: fSub, Enum: "CmpOp"}, reg("Left", fA), reg("Right", fB)},
		Text:     "fcmp.{Op} {Left}, {Right}"},
	{Name: "FUnary", Eval: byCase("Op", overDoubles), Doc: "FUnary is Op of a double.", Result: valueResult,
		Operands: []operand{{Name: "Op", Kind: kEnum, Field: fSub, Enum: "FUnaryOp"}, reg("Src", fA)}, Text: "f{Op} {Src}"},
	{Name: "IntToFloat", Eval: call("intToFloat"), Doc: "IntToFloat is the double nearest the integer Src, read as unsigned when Unsigned is set.", Result: valueResult,
		Operands: []operand{reg("Src", fA), unsignOp}, Text: "itof{Unsigned} {Src}"},
	{Name: "FloatToInt", Eval: call("floatToInt"), Doc: "FloatToInt is the double Src truncated toward zero, or i64.min for a NaN or a double past the word.",
		Result: valueResult, Operands: []operand{reg("Src", fA)}, Text: "ftoi {Src}"},
	{Name: "Neg", Eval: checkedCall("negateWord", "negateFits"), Doc: "Neg negates; when Checked, it goes to Fail for i64.min.", Result: valueResult,
		Operands: []operand{reg("Src", fA), checkedOp}, Text: "neg {Src}{Fail}"},
	{Name: "Not", Eval: call("notWord"), Doc: "Not inverts every bit.", Result: valueResult,
		Operands: []operand{reg("Src", fA)}, Text: "not {Src}"},
	{Name: "Guard", Eval: byCase("Kind", overWords), Doc: "Guard goes to Fail unless Src is in the range Kind of Bound.",
		Operands: []operand{reg("Src", fA), {Name: "Kind", Kind: kEnum, Field: fSub, Enum: "GuardKind"},
			{Name: "Bound", Kind: kWord, Field: fImm}, {Name: "Fail", Kind: kFail, Field: fC}},
		Text: "guard {Src} {Kind} {Bound} else {Fail}"},
	// The collection operations are the common case of the runtime entries that read a length or an element (README,
	// "Primitives"): a Bounds checks the index, and its failure block calls the entry, which traps there.
	{Name: "Length", Eval: noEval, Doc: "Length reads the count of the collection Src: of the elements of an array or a byte array, or of the bytes of a\n// string.",
		Result: valueResult, Operands: []operand{reg("Src", fA), collectionOp}, Effects: []effect{effReads}, Text: "length.{Of} {Src}"},
	{Name: "Bounds", Eval: call("indexBelow"), Doc: "Bounds goes to Fail unless Index is below Length, both read as unsigned: the check of an index of a collection.",
		Operands: []operand{reg("Index", fA), reg("Length", fB), {Name: "Fail", Kind: kFail, Field: fC}},
		Text:     "bounds {Index}, {Length} else {Fail}"},
	{Name: "ElemAddr", Eval: noEval, Doc: "ElemAddr gives the address of the element at Index of Arr, an array of words, where a Bounds has checked the\n// index.",
		Result: valueResult, Operands: []operand{reg("Arr", fA), reg("Index", fB)}, Effects: []effect{effReads}, Text: "elemaddr {Arr}, {Index}"},
	{Name: "ElemLoad", Eval: noEval, Doc: "ElemLoad reads the element at Index of Arr, an array of words, where a Bounds has checked the index: a scalar,\n// or a managed element lent as LoadAt lends it.",
		Result: valueResult, Operands: []operand{reg("Arr", fA), reg("Index", fB)}, Effects: []effect{effReads}, Text: "elem {Arr}, {Index}"},
	{Name: "LoadByte", Eval: noEval, Doc: "LoadByte reads the byte at Index of Src, a byte array or a string, where a Bounds has checked the index.",
		Result: valueResult, Operands: []operand{reg("Src", fA), reg("Index", fB), collectionOp}, Effects: []effect{effReads},
		Text: "byte.{Of} {Src}, {Index}"},
	// A native build splits an element read in a loop that nothing writes memory in (self/opt/licm.maxon) into the read
	// of the array's buffer, which then leaves the loop, and the read at the index; the interpreter never sees them.
	{Name: "Buffer", Eval: noEval, Doc: "Buffer reads the address of the elements of Src, an array or a byte array, which a push that grows it moves.",
		Result: valueResult, Operands: []operand{reg("Src", fA)}, Effects: []effect{effReads}, Text: "buffer {Src}"},
	{Name: "WordAt", Eval: noEval, Doc: "WordAt reads the word at Index of Data, the elements of an array that Buffer gave, where a Bounds has checked the\n// index: a scalar, or a managed element lent as LoadAt lends it.",
		Result: valueResult, Operands: []operand{reg("Data", fA), reg("Index", fB)}, Effects: []effect{effReads}, Text: "wordat {Data}, {Index}"},
	{Name: "ByteAt", Eval: noEval, Doc: "ByteAt reads the byte at Index of Data, the elements of a byte array that Buffer gave, where a Bounds has\n// checked the index.",
		Result: valueResult, Operands: []operand{reg("Data", fA), reg("Index", fB)}, Effects: []effect{effReads}, Text: "byteat {Data}, {Index}"},
	{Name: "Alloc", Eval: noEval, Doc: "Alloc gives a new box of Bytes bytes.", Result: valueResult,
		Operands: []operand{num("Bytes")}, Effects: []effect{effCalls, effAllocates}, Text: "alloc {Bytes}"},
	{Name: "Call", Eval: noEval, Doc: "Call applies Callee to Args.", Result: optionalResult,
		Operands: []operand{{Name: "Callee", Kind: kCallee, Field: fC}, argsOp,
			{Name: "ErrReg", Kind: kErrorReg, Field: fImm, Flag: "Throws", Text: " error "}, floatsOp, floatResultOp},
		Effects: []effect{effReads, effWrites, effCalls, effAllocates, effFrees}, Text: "call {Callee}({Args}){ErrReg}{Floats}{FloatResult}"},
	{Name: "CallRuntime", Eval: noEval, Doc: "CallRuntime applies the runtime entry Symbol to Args; Routine is what the entry stores to call\n// later (the destructor of a cown).",
		Result: optionalResult,
		Operands: []operand{symbolOp, argsOp,
			{Name: "Routine", Kind: kRoutine, Field: fC, Text: " routine "},
			{Name: "Proven", Kind: kFlag, Field: fFlags, Flag: "Proven", Text: " proven"}},
		Effects: []effect{effReads, effWrites, effCalls, effAllocates, effFrees}, Text: "runtime {Symbol}({Args}){Routine}{Proven}"},
	{Name: "When", Eval: noEval, Doc: "When schedules the behaviour Body on the cowns Cowns, capturing Captures.",
		Operands: []operand{{Name: "Body", Kind: kBody, Field: fImm}, {Name: "Cowns", Kind: kRegs, Field: fA, Count: fB},
			{Name: "Captures", Kind: kMoreRegs, Field: fC}},
		Effects: []effect{effReads, effWrites, effCalls, effAllocates}, Text: "when {Body} cowns [{Cowns}] captures [{Captures}]"},
	{Name: "Closure", Eval: noEval, Doc: "Closure gives a new closure of the body Body, holding Captures: it owns each capture that Body says it owns, and\n// borrows the others.",
		Result: valueResult, Operands: []operand{{Name: "Body", Kind: kBody, Field: fImm}, {Name: "Captures", Kind: kRegs, Field: fA, Count: fB}},
		Effects: []effect{effReads, effCalls, effAllocates}, Text: "closure {Body} [{Captures}]"},
	{Name: "FuncValue", Eval: noEval, Doc: "FuncValue gives a new closure of the function Callee, which captures nothing.", Result: valueResult,
		Operands: []operand{{Name: "Callee", Kind: kCallee, Field: fC}}, Effects: []effect{effCalls, effAllocates}, Text: "funcvalue {Callee}"},
	{Name: "CallValue", Eval: noEval, Doc: "CallValue calls the closure that is the first of Args with all of Args: the code it holds takes the closure\n// first, to read what it captured, and a named function's code ignores it.",
		Result: optionalResult, Operands: []operand{argsOp, floatsOp, floatResultOp}, Effects: []effect{effReads, effWrites, effCalls, effAllocates, effFrees},
		Text: "callvalue({Args}){Floats}{FloatResult}"},
	{Name: "Box", Eval: noEval, Doc: "Box gives a new interface value: a box on the heap of the address of the table Table and the value Value, which\n// the box owns.",
		Result: valueResult, Operands: []operand{{Name: "Table", Kind: kTable, Field: fC}, reg("Value", fA)},
		Effects: []effect{effCalls, effAllocates}, Text: "box {Table}, {Value}"},
	{Name: "FrameBox", Eval: noEval, Doc: "FrameBox gives an interface value whose box is the function's frame box Box, two words of its frame, with the\n// address of the table Table and the value Value: a value only lent to what borrows the interface value, or a scalar,\n// which the box owns nothing of. The box lives as long as the frame, and a borrow cannot outlive the call it is lent to.",
		Result: valueResult, Operands: []operand{{Name: "Table", Kind: kTable, Field: fC}, reg("Value", fA), {Name: "Box", Kind: kFrameBox, Field: fImm}},
		Effects: []effect{effWrites}, Text: "framebox {Table}, {Value} in frame box {Box}"},
	{Name: "CallWitness", Eval: noEval, Doc: "CallWitness calls the method in place Slot of the table of the interface value that is the first of Args, with\n// the value the box holds, given as Recv says, and the rest of Args.",
		Result: optionalResult,
		Operands: []operand{{Name: "Recv", Kind: kEnum, Field: fSub, Enum: "ReceiverConv"}, {Name: "Slot", Kind: kInt, Field: fC}, argsOp,
			{Name: "ErrReg", Kind: kErrorReg, Field: fImm, Flag: "Throws", Text: " error "}, floatsOp, floatResultOp},
		Effects: []effect{effReads, effWrites, effCalls, effAllocates, effFrees}, Text: "callwitness.{Recv} {Slot}({Args}){ErrReg}{Floats}{FloatResult}"},
	{Name: "IsType", Eval: noEval, Doc: "IsType gives 1 when the interface value Src holds a value of the type Ty, and 0 when it holds one of another type.",
		Result: valueResult, Operands: []operand{reg("Src", fA), typeOp}, Effects: []effect{effReads, effCalls}, Text: "istype {Src} {Ty}"},
	{Name: "Drop", Eval: noEval, Doc: "Drop drops the value in Src, of type Ty.",
		Operands: []operand{reg("Src", fA), typeOp}, Effects: []effect{effCalls, effFrees}, Text: "drop {Src} {Ty}"},
	{Name: "DropSlot", Eval: noEval, Doc: "DropSlot drops what a slot owns, of type Ty, and leaves it empty.",
		Operands: []operand{slotOp, typeOp}, Effects: []effect{effReads, effWrites, effCalls, effFrees}, Text: "dropslot {Slot} {Ty}"},
	{Name: "DropAt", Eval: noEval, Doc: "DropAt drops what an address holds, of type Ty, and leaves it empty.",
		Operands: []operand{reg("Addr", fA), typeOp}, Effects: []effect{effReads, effWrites, effCalls, effFrees},
		Text: "dropat {Addr} {Ty}"},
	{Name: "MoveSlot", Eval: noEval, Doc: "MoveSlot reads a slot and leaves it empty.", Result: valueResult,
		Operands: []operand{slotOp}, Effects: []effect{effReads, effWrites}, Text: "move {Slot}"},
	{Name: "MoveAt", Eval: noEval, Doc: "MoveAt reads the value at an address and leaves the place empty.", Result: valueResult,
		Operands: []operand{reg("Addr", fA)}, Effects: []effect{effReads, effWrites}, Text: "moveat {Addr}"},
	{Name: "Copy", Eval: noEval, Doc: "Copy gives a deep copy of Src, of type Ty.", Result: valueResult,
		Operands: []operand{reg("Src", fA), typeOp}, Effects: []effect{effReads, effCalls, effAllocates}, Text: "copy {Src} {Ty}"},

	{Name: "Jump", Term: true, Eval: noEval, Doc: "Jump goes to Target, passing Args to its parameters.",
		Operands: []operand{{Name: "Target", Kind: kBlock, Field: fThen}, {Name: "Args", Kind: kEdgeArgs, Field: fArgs, Count: fThenArgs}},
		Text:     "jump {Target}{Args}"},
	{Name: "Branch", Term: true, Eval: noEval, Doc: "Branch goes to IfTrue, passing TrueArgs, when Cond is not 0, else to IfFalse, passing FalseArgs.",
		Operands: []operand{reg("Cond", fValue), {Name: "IfTrue", Kind: kBlock, Field: fThen},
			{Name: "TrueArgs", Kind: kEdgeArgs, Field: fArgs, Count: fThenArgs}, {Name: "IfFalse", Kind: kBlock, Field: fElse},
			{Name: "FalseArgs", Kind: kMoreEdgeArgs, Field: fElseArgs}},
		Text: "branch {Cond}, {IfTrue}{TrueArgs}, {IfFalse}{FalseArgs}"},
	{Name: "Switch", Term: true, Eval: noEval, Doc: "Switch goes to the target of the case that Value equals, passing the case's arguments, else to\n// Fallback, passing FallbackArgs.",
		Operands: []operand{reg("Value", fValue), {Name: "Cases", Kind: kCases, Field: fFirst, Count: fCount},
			{Name: "Fallback", Kind: kBlock, Field: fThen}, {Name: "FallbackArgs", Kind: kEdgeArgs, Field: fArgs, Count: fThenArgs}},
		Text: "switch {Value} [{Cases}] default {Fallback}{FallbackArgs}"},
	{Name: "Ret", Term: true, Eval: noEval, Doc: "Ret returns Value.",
		Operands: []operand{reg("Value", fValue)}, Effects: []effect{effExits}, Text: "ret {Value}"},
	{Name: "Trap", Term: true, Eval: noEval, Doc: "Trap stops the program.", Effects: []effect{effExits}, Text: "trap"},
	{Name: "Throw", Term: true, Eval: noEval, Doc: "Throw returns the error Err, a case of the function's error plus 1, from a function that\n// throws.",
		Operands: []operand{reg("Err", fValue)}, Effects: []effect{effExits}, Text: "throw {Err}"},
}
