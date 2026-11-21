package main

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
	kCases                // cases of the function's Cases, from the field to the count in Count
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
}

// enum is a small closed set an operand names, held in Sub.
type enum struct {
	Name string
	// Prefix is what its constants start with, BinAdd for BinOp's Add.
	Prefix string
	Doc    string
	Cases  []enumCase
}

// enumCase is a value of an enum: its Go name after the prefix, and how the printer writes it.
type enumCase struct {
	Name, Text string
}

var enums = []enum{
	{Name: "BinOp", Prefix: "Bin", Doc: "BinOp is the operation of a Bin.", Cases: []enumCase{
		{"Add", "add"}, {"Sub", "sub"}, {"Mul", "mul"}, {"Div", "div"}, {"Mod", "mod"},
		{"And", "and"}, {"Or", "or"}, {"Xor", "xor"}, {"Shl", "shl"}, {"Shr", "shr"},
	}},
	{Name: "CmpOp", Prefix: "Cmp", Doc: "CmpOp is the comparison of a Cmp.", Cases: []enumCase{
		{"Eq", "eq"}, {"Ne", "ne"}, {"Lt", "lt"}, {"Le", "le"}, {"Gt", "gt"}, {"Ge", "ge"},
	}},
	{Name: "GuardKind", Prefix: "Guard", Doc: "GuardKind is the range a Guard checks, against its bound.", Cases: []enumCase{
		{"NonNegative", "NonNegative"}, {"SignedAtLeast", "SignedAtLeast"}, {"SignedAtMost", "SignedAtMost"},
		{"UnsignedAtLeast", "UnsignedAtLeast"}, {"UnsignedAtMost", "UnsignedAtMost"}, {"NonZero", "NonZero"},
	}},
	{Name: "RawWidth", Prefix: "Width", Doc: "RawWidth is how many bits a LoadRaw or a StoreRaw moves.", Cases: []enumCase{
		{"8", "8"}, {"16", "16"}, {"32", "32"}, {"64", "64"},
	}},
}

// flag is a bit of an instruction's Flags.
type flag struct {
	Name, Doc string
}

var flags = []flag{
	{"Borrow", "Borrow is on a StoreSlot whose slot only names a part of the match subject for the arm, and owns nothing."},
	{"Unsigned", "Unsigned is on a Bin or a Cmp that treats its operands as unsigned."},
	{"Checked", "Checked is on a Bin or a Neg whose result may not fit the word: when it does not, the instruction goes to its\n// failure block instead of to the next instruction."},
	{"Throws", "Throws is on a Call of a function that throws: its error register receives 0 when it returned and the\n// error's case plus 1 when it failed."},
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
)

var ops = []op{
	{Name: "Const", Doc: "Const gives a number.", Result: valueResult,
		Operands: []operand{{Name: "Value", Kind: kWord, Field: fImm}}, Text: "const {Value}"},
	{Name: "Str", Doc: "Str gives a string literal.", Result: valueResult,
		Operands: []operand{{Name: "Text", Kind: kText, Field: fC}}, Text: "str {Text}"},
	{Name: "LoadSlot", Doc: "LoadSlot reads a slot.", Result: valueResult,
		Operands: []operand{slotOp}, Effects: []effect{effReads}, Text: "load {Slot}"},
	{Name: "StoreSlot", Doc: "StoreSlot puts a value in a slot, which owns it unless Borrow is set.",
		Operands: []operand{slotOp, reg("Src", fA), {Name: "Borrow", Kind: kFlag, Field: fFlags, Flag: "Borrow", Text: " borrow"}},
		Effects:  []effect{effWrites}, Text: "store {Slot}, {Src}{Borrow}"},
	{Name: "SlotAddr", Doc: "SlotAddr gives the address of a slot.", Result: valueResult,
		Operands: []operand{slotOp}, Text: "addr {Slot}"},
	{Name: "LoadParam", Doc: "LoadParam reads a parameter.", Result: valueResult,
		Operands: []operand{num("Param")}, Text: "param {Param}"},
	{Name: "LoadField", Doc: "LoadField reads the word at Obj + Offset.", Result: valueResult,
		Operands: []operand{reg("Obj", fA), offsetOp}, Effects: []effect{effReads}, Text: "field {Obj}+{Offset}"},
	{Name: "StoreField", Doc: "StoreField writes Src to the word at Obj + Offset.",
		Operands: []operand{reg("Obj", fA), offsetOp, reg("Src", fB)}, Effects: []effect{effWrites},
		Text: "setfield {Obj}+{Offset}, {Src}"},
	{Name: "FieldAddr", Doc: "FieldAddr gives Obj + Offset.", Result: valueResult,
		Operands: []operand{reg("Obj", fA), offsetOp}, Text: "fieldaddr {Obj}+{Offset}"},
	{Name: "LoadAt", Doc: "LoadAt reads the word at an address.", Result: valueResult,
		Operands: []operand{reg("Addr", fA)}, Effects: []effect{effReads}, Text: "loadat {Addr}"},
	{Name: "StoreAt", Doc: "StoreAt writes Src to the word at an address.",
		Operands: []operand{reg("Addr", fA), reg("Src", fB)}, Effects: []effect{effWrites}, Text: "storeat {Addr}, {Src}"},
	{Name: "LoadGlobal", Doc: "LoadGlobal reads a global of the runtime.", Result: valueResult,
		Operands: []operand{symbolOp}, Effects: []effect{effReads}, Text: "global {Symbol}"},
	// The raw operations are what the runtime's source lowers its __Raw intrinsics to (README, "The runtime"); no other
	// code has them.
	{Name: "LoadRaw", Doc: "LoadRaw reads Width bits at an address, zero-extended to the word.", Result: valueResult,
		Operands: []operand{reg("Addr", fA), widthOp}, Effects: []effect{effReads}, Text: "loadraw.{Width} {Addr}"},
	{Name: "StoreRaw", Doc: "StoreRaw writes the low Width bits of Src at an address.",
		Operands: []operand{reg("Addr", fA), reg("Src", fB), widthOp}, Effects: []effect{effWrites},
		Text: "storeraw.{Width} {Addr}, {Src}"},
	{Name: "GlobalAddr", Doc: "GlobalAddr gives the address of a global of the runtime.", Result: valueResult,
		Operands: []operand{symbolOp}, Text: "globaladdr {Symbol}"},
	{Name: "CallImport", Doc: "CallImport calls the DLL import Name, a string of the function, with the words Args, by the\n// Win64 convention, and gives what it returns.",
		Result: valueResult, Operands: []operand{{Name: "Name", Kind: kText, Field: fC}, argsOp},
		Effects: []effect{effReads, effWrites, effCalls}, Text: "import {Name}({Args})"},
	{Name: "Bin", Doc: "Bin is Left Op Right; when Checked, it goes to Fail when the result does not fit, which for a\n// signed '/' is i64.min / -1.",
		Result:   valueResult,
		Operands: []operand{{Name: "Op", Kind: kEnum, Field: fSub, Enum: "BinOp"}, reg("Left", fA), reg("Right", fB), unsignOp, checkedOp},
		Text:     "{Op}{Unsigned} {Left}, {Right}{Fail}"},
	{Name: "Cmp", Doc: "Cmp gives 1 when Left Op Right holds and 0 when it does not.", Result: valueResult,
		Operands: []operand{{Name: "Op", Kind: kEnum, Field: fSub, Enum: "CmpOp"}, reg("Left", fA), reg("Right", fB), unsignOp},
		Text:     "cmp.{Op}{Unsigned} {Left}, {Right}"},
	{Name: "Neg", Doc: "Neg negates; when Checked, it goes to Fail for i64.min.", Result: valueResult,
		Operands: []operand{reg("Src", fA), checkedOp}, Text: "neg {Src}{Fail}"},
	{Name: "Not", Doc: "Not inverts every bit.", Result: valueResult,
		Operands: []operand{reg("Src", fA)}, Text: "not {Src}"},
	{Name: "Guard", Doc: "Guard goes to Fail unless Src is in the range Kind of Bound.",
		Operands: []operand{reg("Src", fA), {Name: "Kind", Kind: kEnum, Field: fSub, Enum: "GuardKind"},
			{Name: "Bound", Kind: kWord, Field: fImm}, {Name: "Fail", Kind: kFail, Field: fC}},
		Text: "guard {Src} {Kind} {Bound} else {Fail}"},
	{Name: "Alloc", Doc: "Alloc gives a new box of Bytes bytes.", Result: valueResult,
		Operands: []operand{num("Bytes")}, Effects: []effect{effCalls, effAllocates}, Text: "alloc {Bytes}"},
	{Name: "Call", Doc: "Call applies Callee to Args.", Result: optionalResult,
		Operands: []operand{{Name: "Callee", Kind: kCallee, Field: fC}, argsOp,
			{Name: "ErrReg", Kind: kErrorReg, Field: fImm, Flag: "Throws", Text: " error "}},
		Effects: []effect{effReads, effWrites, effCalls, effAllocates, effFrees}, Text: "call {Callee}({Args}){ErrReg}"},
	{Name: "CallRuntime", Doc: "CallRuntime applies the runtime entry Symbol to Args; Routine is what the entry stores to call\n// later (the destructor of a cown).",
		Result: optionalResult,
		Operands: []operand{symbolOp, argsOp,
			{Name: "Routine", Kind: kRoutine, Field: fC, Text: " routine "}},
		Effects: []effect{effReads, effWrites, effCalls, effAllocates, effFrees}, Text: "runtime {Symbol}({Args}){Routine}"},
	{Name: "When", Doc: "When schedules the behaviour Body on the cowns Cowns, capturing Captures.",
		Operands: []operand{{Name: "Body", Kind: kBody, Field: fImm}, {Name: "Cowns", Kind: kRegs, Field: fA, Count: fB},
			{Name: "Captures", Kind: kMoreRegs, Field: fC}},
		Effects: []effect{effReads, effWrites, effCalls, effAllocates}, Text: "when {Body} cowns [{Cowns}] captures [{Captures}]"},
	{Name: "Closure", Doc: "Closure gives a new closure of the body Body, holding Captures: it owns each capture that Body says it owns, and\n// borrows the others.",
		Result: valueResult, Operands: []operand{{Name: "Body", Kind: kBody, Field: fImm}, {Name: "Captures", Kind: kRegs, Field: fA, Count: fB}},
		Effects: []effect{effReads, effCalls, effAllocates}, Text: "closure {Body} [{Captures}]"},
	{Name: "FuncValue", Doc: "FuncValue gives a new closure of the function Callee, which captures nothing.", Result: valueResult,
		Operands: []operand{{Name: "Callee", Kind: kCallee, Field: fC}}, Effects: []effect{effCalls, effAllocates}, Text: "funcvalue {Callee}"},
	{Name: "CallValue", Doc: "CallValue calls the closure that is the first of Args with all of Args: the code it holds takes the closure\n// first, to read what it captured, and a named function's code ignores it.",
		Result: optionalResult, Operands: []operand{argsOp}, Effects: []effect{effReads, effWrites, effCalls, effAllocates, effFrees},
		Text: "callvalue({Args})"},
	{Name: "Drop", Doc: "Drop drops the value in Src, of type Ty.",
		Operands: []operand{reg("Src", fA), typeOp}, Effects: []effect{effCalls, effFrees}, Text: "drop {Src} {Ty}"},
	{Name: "DropSlot", Doc: "DropSlot drops what a slot owns, of type Ty, and leaves it empty.",
		Operands: []operand{slotOp, typeOp}, Effects: []effect{effReads, effWrites, effCalls, effFrees}, Text: "dropslot {Slot} {Ty}"},
	{Name: "DropAt", Doc: "DropAt drops what an address holds, of type Ty, and leaves it empty.",
		Operands: []operand{reg("Addr", fA), typeOp}, Effects: []effect{effReads, effWrites, effCalls, effFrees},
		Text: "dropat {Addr} {Ty}"},
	{Name: "MoveSlot", Doc: "MoveSlot reads a slot and leaves it empty.", Result: valueResult,
		Operands: []operand{slotOp}, Effects: []effect{effReads, effWrites}, Text: "move {Slot}"},
	{Name: "MoveAt", Doc: "MoveAt reads the value at an address and leaves the place empty.", Result: valueResult,
		Operands: []operand{reg("Addr", fA)}, Effects: []effect{effReads, effWrites}, Text: "moveat {Addr}"},
	{Name: "Copy", Doc: "Copy gives a deep copy of Src, of type Ty.", Result: valueResult,
		Operands: []operand{reg("Src", fA), typeOp}, Effects: []effect{effReads, effCalls, effAllocates}, Text: "copy {Src} {Ty}"},

	{Name: "Jump", Term: true, Doc: "Jump goes to Target.",
		Operands: []operand{{Name: "Target", Kind: kBlock, Field: fThen}}, Text: "jump {Target}"},
	{Name: "Branch", Term: true, Doc: "Branch goes to IfTrue when Cond is not 0, else to IfFalse.",
		Operands: []operand{reg("Cond", fValue), {Name: "IfTrue", Kind: kBlock, Field: fThen}, {Name: "IfFalse", Kind: kBlock, Field: fElse}},
		Text:     "branch {Cond}, {IfTrue}, {IfFalse}"},
	{Name: "Switch", Term: true, Doc: "Switch goes to the target of the case that Value equals, else to Fallback.",
		Operands: []operand{reg("Value", fValue), {Name: "Cases", Kind: kCases, Field: fFirst, Count: fCount},
			{Name: "Fallback", Kind: kBlock, Field: fThen}},
		Text: "switch {Value} [{Cases}] default {Fallback}"},
	{Name: "Ret", Term: true, Doc: "Ret returns Value.",
		Operands: []operand{reg("Value", fValue)}, Effects: []effect{effExits}, Text: "ret {Value}"},
	{Name: "Trap", Term: true, Doc: "Trap stops the program.", Effects: []effect{effExits}, Text: "trap"},
	{Name: "Throw", Term: true, Doc: "Throw returns the error Err, a case of the function's error plus 1, from a function that\n// throws.",
		Operands: []operand{reg("Err", fValue)}, Effects: []effect{effExits}, Text: "throw {Err}"},
}
