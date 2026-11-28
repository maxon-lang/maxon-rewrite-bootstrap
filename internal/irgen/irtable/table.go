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
	{Name: "VecBinOp", Prefix: "Vec", Doc: "VecBinOp is the operation of a VecBin on two 128-bit vectors: the bitwise xor, and and or, the sum of each\n// 32-bit lane (wrapping), and the bytes of Left picked by the bytes of Right (ShuffleBytes: a byte of Right below 16 gives\n// that byte of Left, and one of 128 or more gives 0; Right holds no other byte, where x64 and arm64 differ).", Cases: []enumCase{
		{"Xor", "xor", noCase}, {"And", "and", noCase}, {"Or", "or", noCase}, {"AddLanes32", "addlanes32", noCase},
		{"ShuffleBytes", "shufflebytes", noCase},
	}},
	{Name: "VecShiftOp", Prefix: "VecShift", Doc: "VecShiftOp is the operation of a VecShift: the whole vector shifted by Amount bytes toward its high or its low\n// end, zeros coming in, or each 64-bit lane shifted by Amount bits.", Cases: []enumCase{
		{"BytesUp", "bytesup", noCase}, {"BytesDown", "bytesdown", noCase},
		{"LanesLeft", "lanesleft", noCase}, {"LanesRight", "lanesright", noCase},
	}},
	{Name: "VecAesOp", Prefix: "VecAes", Doc: "VecAesOp is the round of a VecAes, as x64's AES instructions do it: Enc is ShiftRows, SubBytes and MixColumns\n// and then the key's exclusive or, EncLast the same without MixColumns, and Dec and DecLast their inverses (InvShiftRows,\n// InvSubBytes, InvMixColumns, the key), which arm64 makes of aese and aesd with a zero key, aesmc or aesimc and an eor.", Cases: []enumCase{
		{"Enc", "enc", noCase}, {"EncLast", "enclast", noCase}, {"Dec", "dec", noCase}, {"DecLast", "declast", noCase},
	}},
	{Name: "VecAesArmOp", Prefix: "VecAesArm", Doc: "VecAesArmOp is the round of a VecAesArm, as arm64's aese and aesd do it: the key's exclusive or first, and then\n// ShiftRows and SubBytes, or their inverses, with no MixColumns.", Cases: []enumCase{
		{"Encrypt", "aese", noCase}, {"Decrypt", "aesd", noCase},
	}},
	{Name: "VecShaOp", Prefix: "VecSha", Doc: "VecShaOp is the SHA-256 instruction of a VecSha: x64's Rnds2, Msg1 and Msg2, and arm64's H, H2, Su0 and Su1, each\n// of which only its own back end selects, since the two instruction sets split the rounds and the schedule differently.", Cases: []enumCase{
		{"Rnds2", "sha256rnds2", noCase}, {"Msg1", "sha256msg1", noCase}, {"Msg2", "sha256msg2", noCase},
		{"H", "sha256h", noCase}, {"H2", "sha256h2", noCase}, {"Su0", "sha256su0", noCase}, {"Su1", "sha256su1", noCase},
	}},
}

// flag is a bit of an instruction's Flags.
type flag struct {
	Name, Doc string
}

var flags = []flag{
	{"Borrow", "Borrow is on a StoreSlot whose slot only names a part of the match subject for the arm, and owns nothing, on a\n// Box that only lends the value it holds, and on a StoreField of a field that borrows what it holds."},
	{"Unsigned", "Unsigned is on a Bin or a Cmp that treats its operands as unsigned."},
	{"Checked", "Checked is on a Bin or a Neg whose result may not fit the word: when it does not, the instruction goes to its\n// failure block instead of to the next instruction."},
	{"Throws", "Throws is on a Call of a function that throws: its error register receives 0 when it returned and the\n// error's case plus 1 when it failed."},
	{"Proven", "Proven is on a CallRuntime of an entry that reads or addresses an element at an index that the checker or a\n// check before it has proved inside the collection: its expansion is the access alone, with no bounds check."},
	{"FloatResult", "FloatResult is on a call whose result is a float, which native code returns in an xmm register."},
	{"Filled", "Filled is on a LoadAt or an ElemLoad of an element of an array that is read with no test for an empty slot, since\n// no slot below the count of an array of its type is empty (ir.slotsMayBeEmpty): the interpreter reports one that is."},
	{"Character", "Character is on a Str of a character literal, which native code holds in its two words when it has few enough bytes\n// (runtime/characters.maxon)."},
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
	filledOp  = operand{Name: "Filled", Kind: kFlag, Field: fFlags, Flag: "Filled", Text: " filled"}
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
		Operands: []operand{{Name: "Text", Kind: kText, Field: fC}, {Name: "Character", Kind: kFlag, Field: fFlags, Flag: "Character", Text: " character"}}, Text: "str {Text}{Character}"},
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
	{Name: "StoreField", Eval: noEval, Doc: "StoreField writes Src to the word at Obj + Offset, which owns it unless Borrow is set: a cursor's\n// field that borrows the buffer it walks.",
		Operands: []operand{reg("Obj", fA), offsetOp, reg("Src", fB), {Name: "Borrow", Kind: kFlag, Field: fFlags, Flag: "Borrow", Text: " borrow"}}, Effects: []effect{effWrites},
		Text: "setfield {Obj}+{Offset}, {Src}{Borrow}"},
	{Name: "FieldAddr", Eval: noEval, Doc: "FieldAddr gives Obj + Offset.", Result: valueResult,
		Operands: []operand{reg("Obj", fA), offsetOp}, Text: "fieldaddr {Obj}+{Offset}"},
	{Name: "LoadAt", Eval: noEval, Doc: "LoadAt reads the word at an address.", Result: valueResult,
		Operands: []operand{reg("Addr", fA), filledOp}, Effects: []effect{effReads}, Text: "loadat {Addr}{Filled}"},
	{Name: "StoreAt", Eval: noEval, Doc: "StoreAt writes Src to the word at an address.",
		Operands: []operand{reg("Addr", fA), reg("Src", fB)}, Effects: []effect{effWrites}, Text: "storeat {Addr}, {Src}"},
	// A place of a byte-sized type that a call lends by address is read and written one byte wide (ir.lentWidth): a byte
	// of a byte array's buffer, or the low byte of a word that holds the value, which is the value on every target, since
	// each is little-endian, the value is from 0 to 255, and the word's upper bytes are never written.
	{Name: "LoadByteAt", Eval: noEval, Doc: "LoadByteAt reads the byte at an address, zero-extended to the word.", Result: valueResult,
		Operands: []operand{reg("Addr", fA)}, Effects: []effect{effReads}, Text: "loadbyteat {Addr}"},
	{Name: "StoreByteAt", Eval: noEval, Doc: "StoreByteAt writes the low byte of Src to the byte at an address.",
		Operands: []operand{reg("Addr", fA), reg("Src", fB)}, Effects: []effect{effWrites}, Text: "storebyteat {Addr}, {Src}"},
	{Name: "LoadGlobal", Eval: noEval, Doc: "LoadGlobal reads a global of the runtime.", Result: valueResult,
		Operands: []operand{symbolOp}, Effects: []effect{effReads}, Text: "global {Symbol}"},
	// The raw operations are what the runtime's source lowers its __Raw intrinsics to (README, "The runtime"); no other
	// code has them.
	{Name: "LoadRaw", Eval: noEval, Doc: "LoadRaw reads Width bits at an address, zero-extended to the word.", Result: valueResult,
		Operands: []operand{reg("Addr", fA), widthOp}, Effects: []effect{effReads}, Text: "loadraw.{Width} {Addr}"},
	{Name: "StoreRaw", Eval: noEval, Doc: "StoreRaw writes the low Width bits of Src at an address.",
		Operands: []operand{reg("Addr", fA), reg("Src", fB), widthOp}, Effects: []effect{effWrites},
		Text: "storeraw.{Width} {Addr}, {Src}"},
	// The ordered accesses are the word another thread hands over: no read or write after an acquire is done before it,
	// and none before a release is done after it, so a thread whose acquire sees a release sees what was written before
	// it. A weakly ordered processor needs that said, since its plain accesses may be seen out of order.
	{Name: "LoadAcquire", Eval: noEval, Doc: "LoadAcquire reads the word at an address, and nothing after it reads or writes memory before it.",
		Result: valueResult, Operands: []operand{reg("Addr", fA)}, Effects: []effect{effReads, effWrites},
		Text: "loadacquire {Addr}"},
	{Name: "StoreRelease", Eval: noEval, Doc: "StoreRelease writes Src to the word at an address after everything before it has read and written memory.",
		Operands: []operand{reg("Addr", fA), reg("Src", fB)}, Effects: []effect{effReads, effWrites},
		Text: "storerelease {Addr}, {Src}"},
	{Name: "GlobalAddr", Eval: noEval, Doc: "GlobalAddr gives the address of a global of the runtime.", Result: valueResult,
		Operands: []operand{symbolOp}, Text: "globaladdr {Symbol}"},
	{Name: "CallImport", Eval: noEval, Doc: "CallImport calls the DLL import Name, a string of the function, with the words Args, by the\n// Win64 convention, and gives what it returns.",
		Result: valueResult, Operands: []operand{{Name: "Name", Kind: kText, Field: fC}, argsOp},
		Effects: []effect{effReads, effWrites, effCalls}, Text: "import {Name}({Args})"},
	{Name: "Syscall", Eval: noEval, Doc: "Syscall calls the kernel with the number that is the first of Args and the words that follow it, by the\n// target's system call convention, and gives what it returns.",
		Result: valueResult, Operands: []operand{argsOp}, Effects: []effect{effReads, effWrites, effCalls}, Text: "syscall({Args})"},
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
	{Name: "CalleeStackDepth", Eval: noEval, Doc: "CalleeStackDepth gives the most of the stack that the calls of the function it is in may take below its stack\n// pointer, which the link finds from the frames of the functions they reach.",
		Result: valueResult, Text: "calleestackdepth"},
	{Name: "ThreadContext", Eval: noEval, Doc: "ThreadContext gives the word the running thread keeps in the runtime's thread-local slot: its context.",
		Result: valueResult, Effects: []effect{effReads}, Text: "threadcontext"},
	{Name: "CallCode", Eval: noEval, Doc: "CallCode calls the code at the address that is the first of Args with the rest of Args, by the internal\n// convention, and gives what it returns.",
		Result: valueResult, Operands: []operand{argsOp}, Effects: []effect{effReads, effWrites, effCalls, effAllocates, effFrees},
		Text: "callcode({Args})"},
	{Name: "CallHost", Eval: noEval, Doc: "CallHost calls the code at the address that is the first of Args with the rest of Args, by the host's\n// convention (Win64), as CallImport calls an import: a function of a library the import table does not name, found by\n// its address.",
		Result: valueResult, Operands: []operand{argsOp}, Effects: []effect{effReads, effWrites, effCalls},
		Text: "callhost({Args})"},
	{Name: "CodeAddress", Eval: noEval, Doc: "CodeAddress gives the address of the code of the function Callee, which a CallCode calls: the run of a\n// behaviour's record that a test of the runtime builds by hand.",
		Result: valueResult, Operands: []operand{{Name: "Callee", Kind: kCallee, Field: fC}}, Text: "codeaddr {Callee}"},
	{Name: "BodyAddress", Eval: noEval, Doc: "BodyAddress gives the address of the code of the body Body, which the runtime calls: the chunk of a parallel\n// loop and the drop of its errors.",
		Result: valueResult, Operands: []operand{{Name: "Body", Kind: kBody, Field: fImm}}, Text: "bodyaddr {Body}"},
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
	{Name: "TrailingZeros", Eval: call("trailingZeros"), Doc: "TrailingZeros is the number of 0 bits below the lowest 1 bit of Src, and 64 when Src is 0.",
		Result: valueResult, Operands: []operand{reg("Src", fA)}, Text: "ctz {Src}"},
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
	{Name: "ElemAddr", Eval: noEval, Doc: "ElemAddr gives the address of the element at Index of Arr, where a Bounds has checked the index: the elements\n// are 1 shl Shift bytes each, a word (wordElementShift) or a String (stringElementShift).",
		Result: valueResult, Operands: []operand{reg("Arr", fA), reg("Index", fB), num("Shift")}, Effects: []effect{effReads}, Text: "elemaddr {Arr}, {Index} shl {Shift}"},
	{Name: "ElemLoad", Eval: noEval, Doc: "ElemLoad reads the element at Index of Arr, of 1 shl Shift bytes, where a Bounds has checked the index: a\n// scalar, or a managed element or a String lent as LoadAt lends it.",
		Result: valueResult, Operands: []operand{reg("Arr", fA), reg("Index", fB), num("Shift"), filledOp}, Effects: []effect{effReads}, Text: "elem {Arr}, {Index} shl {Shift}{Filled}"},
	{Name: "LoadByte", Eval: noEval, Doc: "LoadByte reads the byte at Index of Src, a byte array or a string, where a Bounds has checked the index.",
		Result: valueResult, Operands: []operand{reg("Src", fA), reg("Index", fB), collectionOp}, Effects: []effect{effReads},
		Text: "byte.{Of} {Src}, {Index}"},
	{Name: "LoadWord", Eval: noEval, Doc: "LoadWord reads Width bits, little-endian, from byte Index of the byte array Src on, where a Bounds has checked\n// the last of their bytes: an ordered word of a constant width that the library reads (ir/hash.maxon).",
		Result: valueResult, Operands: []operand{reg("Src", fA), reg("Index", fB), widthOp}, Effects: []effect{effReads},
		Text: "word.{Width} {Src}, {Index}"},
	// The vector operations are on 128-bit values of the runtime's source, which a register of kind kindVector holds whole
	// (self/ir/function.maxon): byte i of a vector is byte i of the memory it is loaded from, and lane 0 its low end.
	{Name: "VecLoad", Eval: noEval, Doc: "VecLoad reads the 16 bytes at an address, which need no alignment.", Result: valueResult,
		Operands: []operand{reg("Addr", fA)}, Effects: []effect{effReads}, Text: "vecload {Addr}"},
	{Name: "VecStore", Eval: noEval, Doc: "VecStore writes the vector Src to the 16 bytes at an address, which need no alignment.",
		Operands: []operand{reg("Addr", fA), reg("Src", fB)}, Effects: []effect{effWrites}, Text: "vecstore {Addr}, {Src}"},
	{Name: "VecZero", Eval: noEval, Doc: "VecZero gives the vector of 16 zero bytes.", Result: valueResult, Text: "veczero"},
	{Name: "VecFromWords", Eval: noEval, Doc: "VecFromWords gives the vector whose low 64-bit lane is Low and whose high lane is High.",
		Result: valueResult, Operands: []operand{reg("Low", fA), reg("High", fB)}, Text: "vecwords {Low}, {High}"},
	{Name: "VecWord", Eval: noEval, Doc: "VecWord gives the 64-bit lane Lane (0, the low one, or 1) of the vector Src.", Result: valueResult,
		Operands: []operand{reg("Src", fA), num("Lane")}, Text: "vecword {Src}, {Lane}"},
	{Name: "VecBin", Eval: noEval, Doc: "VecBin is Left Op Right on two vectors.", Result: valueResult,
		Operands: []operand{{Name: "Op", Kind: kEnum, Field: fSub, Enum: "VecBinOp"}, reg("Left", fA), reg("Right", fB)},
		Text:     "vec{Op} {Left}, {Right}"},
	{Name: "VecShift", Eval: noEval, Doc: "VecShift is the vector Src shifted by Op by Amount, a constant: 1 to 15 bytes, or 1 to 63 bits.",
		Result: valueResult, Operands: []operand{{Name: "Op", Kind: kEnum, Field: fSub, Enum: "VecShiftOp"}, reg("Src", fA), num("Amount")},
		Text: "vec{Op} {Src}, {Amount}"},
	{Name: "VecShuffleDwords", Eval: noEval, Doc: "VecShuffleDwords gives the vector whose 32-bit lane i is lane (Order >> 2i) & 3 of Src, Order a constant\n// below 256: x64's pshufd.",
		Result: valueResult, Operands: []operand{reg("Src", fA), num("Order")}, Text: "vecshufdwords {Src}, {Order}"},
	{Name: "VecAlignBytes", Eval: noEval, Doc: "VecAlignBytes gives the 16 bytes from byte Amount on of the 32 that are Low and then High, Amount a constant\n// from 1 to 15: x64's palignr and arm64's ext.",
		Result: valueResult, Operands: []operand{reg("High", fA), reg("Low", fB), num("Amount")}, Text: "vecalignbytes {High}, {Low}, {Amount}"},
	{Name: "VecBlendWords", Eval: noEval, Doc: "VecBlendWords gives the vector whose 16-bit lane i is Right's where bit i of Mask is set and Left's where it\n// is clear, Mask a constant below 256: x64's pblendw.",
		Result: valueResult, Operands: []operand{reg("Left", fA), reg("Right", fB), num("Mask")}, Text: "vecblendwords {Left}, {Right}, {Mask}"},
	{Name: "VecAes", Eval: noEval, Doc: "VecAes is one AES round of State with the round key Key, by Op.", Result: valueResult,
		Operands: []operand{{Name: "Op", Kind: kEnum, Field: fSub, Enum: "VecAesOp"}, reg("State", fA), reg("Key", fB)},
		Text:     "vecaes.{Op} {State}, {Key}"},
	{Name: "VecAesInvMix", Eval: noEval, Doc: "VecAesInvMix is InvMixColumns of Src, which turns an encryption round key into the decryption key of the same\n// round: x64's aesimc and arm64's.",
		Result: valueResult, Operands: []operand{reg("Src", fA)}, Text: "vecaesinvmix {Src}"},
	{Name: "VecAesArm", Eval: noEval, Doc: "VecAesArm is arm64's aese or aesd of State with Key, by Op; only arm64's back end selects it.", Result: valueResult,
		Operands: []operand{{Name: "Op", Kind: kEnum, Field: fSub, Enum: "VecAesArmOp"}, reg("State", fA), reg("Key", fB)},
		Text:     "vecaesarm.{Op} {State}, {Key}"},
	{Name: "VecAesMix", Eval: noEval, Doc: "VecAesMix is MixColumns of Src, arm64's aesmc; only arm64's back end selects it.", Result: valueResult,
		Operands: []operand{reg("Src", fA)}, Text: "vecaesmix {Src}"},
	{Name: "VecClmul", Eval: noEval, Doc: "VecClmul is the carry-less product of a 64-bit lane of Left and one of Right, the 128 bits of it: Select is\n// 0x00, 0x01, 0x10 or 0x11, whose low digit picks Left's lane and whose high digit Right's, as pclmulqdq's immediate does.",
		Result: valueResult, Operands: []operand{reg("Left", fA), reg("Right", fB), num("Select")}, Text: "vecclmul {Left}, {Right}, {Select}"},
	{Name: "VecSha", Eval: noEval, Doc: "VecSha is the SHA-256 instruction Op of its two or three vectors in Args, the first of which the instruction\n// reads and gives the result in place of: x64's Rnds2 takes the state's C, D, G and H, then A, B, E and F, then the two\n// message words with their constants (which it reads in xmm0), Msg1 and Msg2 two vectors of the schedule; arm64's H and H2\n// take the half of the state they give, the other half and the message words with their constants, Su0 two vectors of the\n// schedule and Su1 three.",
		Result: valueResult, Operands: []operand{{Name: "Op", Kind: kEnum, Field: fSub, Enum: "VecShaOp"}, argsOp}, Text: "vecsha.{Op}({Args})"},
	{Name: "Cpuid", Eval: noEval, Doc: "Cpuid gives the word that x64's cpuid of the leaf Leaf and the subleaf Subleaf writes to the register Register,\n// a constant: 0 for eax, 1 for ebx, 2 for ecx and 3 for edx; only x64's back end selects it.", Result: valueResult,
		Operands: []operand{reg("Leaf", fA), reg("Subleaf", fB), num("Register")}, Text: "cpuid {Leaf}, {Subleaf}, {Register}"},
	// A native build splits every element access (self/opt/buffers.maxon) into the read of the array's buffer, which
	// common subexpression elimination and loop-invariant code motion then treat as they treat any read, and the access
	// at the index; the interpreter never sees them.
	{Name: "Buffer", Eval: noEval, Doc: "Buffer reads the address of the elements of Src, an array or a byte array, which a push that grows it moves.",
		Result: valueResult, Operands: []operand{reg("Src", fA)}, Effects: []effect{effReads}, Text: "buffer {Src}"},
	{Name: "ElemAt", Eval: noEval, Doc: "ElemAt reads the element at Index of Data, the elements of an array that Buffer gave, of 1 shl Shift bytes,\n// where a Bounds has checked the index: a scalar, or a managed element or a String lent as LoadAt lends it.",
		Result: valueResult, Operands: []operand{reg("Data", fA), reg("Index", fB), num("Shift")}, Effects: []effect{effReads}, Text: "elemat {Data}, {Index} shl {Shift}"},
	{Name: "ElemAddrAt", Eval: noEval, Doc: "ElemAddrAt gives the address of the element at Index of Data, the elements of an array that Buffer gave, of\n// 1 shl Shift bytes, where a Bounds has checked the index: Data plus the index shifted, which reads no memory.",
		Result: valueResult, Operands: []operand{reg("Data", fA), reg("Index", fB), num("Shift")}, Text: "elemaddrat {Data}, {Index} shl {Shift}"},
	{Name: "ByteAt", Eval: noEval, Doc: "ByteAt reads the byte at Index of Data, the elements of a byte array that Buffer gave, where a Bounds has checked the index.",
		Result: valueResult, Operands: []operand{reg("Data", fA), reg("Index", fB)}, Effects: []effect{effReads}, Text: "byteat {Data}, {Index}"},
	// A String is two words in one register (README, "The IR"): the address of its bytes and its word, which these give
	// and make, so they move no memory; the runtime's String intrinsics are made of them.
	{Name: "StrPtr", Eval: noEval, Doc: "StrPtr gives the address of the first byte of the string Src, its first word.",
		Result: valueResult, Operands: []operand{reg("Src", fA)}, Text: "strptr {Src}"},
	{Name: "StrBytes", Eval: noEval, Doc: "StrBytes gives the address of the first byte of the string Src: its first word, or where it holds its bytes in\n// its two words, the frame box Box, which it writes them to in order.",
		Result: valueResult, Operands: []operand{reg("Src", fA), {Name: "Box", Kind: kFrameBox, Field: fImm}}, Effects: []effect{effWrites}, Text: "strbytes {Src} in frame box {Box}"},
	{Name: "StrWord", Eval: noEval, Doc: "StrWord gives the word of the string Src: its length shifted up past its flags, and the flags.",
		Result: valueResult, Operands: []operand{reg("Src", fA)}, Text: "strword {Src}"},
	{Name: "StrMake", Eval: noEval, Doc: "StrMake gives the string of the address Bytes and the word Word.",
		Result: valueResult, Operands: []operand{reg("Bytes", fA), reg("Word", fB)}, Text: "strmake {Bytes}, {Word}"},
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
	{Name: "When", Eval: noEval, Doc: "When schedules the behaviour Body on the cowns Cowns, capturing Captures. One that gives a register only makes\n// its record, which it gives, for the runtime to queue later: a message the runtime posts (README, \"Stops\").",
		Result: optionalResult, Operands: []operand{{Name: "Body", Kind: kBody, Field: fImm}, {Name: "Cowns", Kind: kRegs, Field: fA, Count: fB},
			{Name: "Captures", Kind: kMoreRegs, Field: fC}},
		Effects: []effect{effReads, effWrites, effCalls, effAllocates}, Text: "when {Body} cowns [{Cowns}] captures [{Captures}]"},
	{Name: "Async", Eval: noEval, Doc: "Async starts Callee with the words Args as a coroutine of the running green thread's strand, and gives its\n// promise: the coroutine runs the thunk of Callee, which calls it with Args and keeps what it returns, and what it\n// failed with, for the await. The coroutine owns each argument a consumes parameter takes, and borrows the others.",
		Result: valueResult, Operands: []operand{{Name: "Callee", Kind: kCallee, Field: fC}, argsOp},
		Effects: []effect{effReads, effWrites, effCalls, effAllocates}, Text: "async {Callee}({Args})"},
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
	{Name: "FrameArea", Eval: noEval, Doc: "FrameArea gives the address of Words words of the function's frame, from its frame box Box on through as many\n// boxes as they fill: the box of a tuple that a function lends, which the call passes it to fill with a borrow of each\n// element. It owns nothing, lives as long as the frame, and a lent tuple cannot outlive the scope it is lent to.",
		Result: valueResult, Operands: []operand{{Name: "Words", Kind: kInt, Field: fC}, {Name: "Box", Kind: kFrameBox, Field: fImm}},
		Text: "framearea {Words} in frame box {Box}"},
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
