package main

// The instruction table of the x64 encoder: the source of truth for self/backend/x64_gen.maxon. A row is a form, one
// instruction with one shape of operands, named as maxon's X64Backend.maxon names its encoders (encodeMovRegReg,
// encodeLoadRegBaseDisp, encodePushReg). A Choice selects among forms by the value of an immediate, as the Go backend's
// AddImm and MovImm do, and a Sequence is several forms in a row that the backend always emits together.

// Encoding constants that the generated helpers and the reference encoder of the test both read.
const (
	rexBase        = 0x40
	rexWide        = 0x08
	rexReg         = 0x04
	rexRm          = 0x01
	modRegister    = 0xC0
	modDisp8       = 0x40
	modDisp32      = 0x80
	ripRelative    = 5
	sibRsp         = 0x24
	rspLowBits     = 4
	rbpLowBits     = 5
	regFieldScale  = 8
	lowRegisters   = 8
	byteRegisters  = 4
	conditionCodes = 16
	imm8Min        = -128
	imm8Max        = 127
	imm32Min       = -1 << 31
	imm32Max       = 1<<31 - 1
	imm32UMax      = 1<<32 - 1
	byteValues     = 256
	imm32Values    = 1 << 32
	imm32Bytes     = 4
	imm64Bytes     = 8
	rel32Bytes     = 4
	opcodeHigh     = 0x0F
)

// constant is a name and the value that the generated Maxon gives it.
type constant struct {
	name  string
	value int
}

var constants = []constant{
	{"rexBase", rexBase},
	{"rexWide", rexWide},
	{"rexReg", rexReg},
	{"rexRm", rexRm},
	{"modRegister", modRegister},
	{"modDisp8", modDisp8},
	{"modDisp32", modDisp32},
	{"ripRelative", ripRelative},
	{"sibRsp", sibRsp},
	{"rspLowBits", rspLowBits},
	{"rbpLowBits", rbpLowBits},
	{"regFieldScale", regFieldScale},
	{"lowRegisters", lowRegisters},
	{"byteRegisters", byteRegisters},
	{"conditionCodes", conditionCodes},
	{"imm8Min", imm8Min},
	{"imm8Max", imm8Max},
	{"imm32Min", imm32Min},
	{"imm32Max", imm32Max},
	{"imm32UMax", imm32UMax},
	{"byteValues", byteValues},
	{"imm32Values", imm32Values},
	{"imm32Bytes", imm32Bytes},
	{"imm64Bytes", imm64Bytes},
	{"rel32Bytes", rel32Bytes},
}

// registers are the general registers in the order of their encoding numbers, which is the order of maxon's X64Register.
var registers = []string{
	"rax", "rcx", "rdx", "rbx", "rsp", "rbp", "rsi", "rdi",
	"r8", "r9", "r10", "r11", "r12", "r13", "r14", "r15",
}

// condition is a condition code and the number that is added to the opcode of jcc and setcc. The names are maxon's
// X64CondCode, where it has one.
type condition struct {
	name string
	code int
}

var conditions = []condition{
	{"overflow", 0x0},
	{"noOverflow", 0x1},
	{"below", 0x2},
	{"aboveEqual", 0x3},
	{"equal", 0x4},
	{"notEqual", 0x5},
	{"belowEqual", 0x6},
	{"above", 0x7},
	{"sign", 0x8},
	{"noSign", 0x9},
	{"parity", 0xA},
	{"noParity", 0xB},
	{"less", 0xC},
	{"greaterEqual", 0xD},
	{"lessEqual", 0xE},
	{"greater", 0xF},
}

// kind is what an operand is.
type kind int

const (
	kindReg    kind = iota // a general register
	kindMem                // [base + disp], the displacement as short as it can be
	kindRip                // [rip + disp32], whose displacement a fixup patches
	kindImm8               // a signed byte
	kindImm32              // a signed doubleword
	kindImm32U             // an unsigned doubleword
	kindImm64              // a quadword
	kindRel32              // a signed doubleword relative to the end of the instruction
	kindCond               // a condition code, added to the last opcode byte
)

// field is where an operand's bits go.
type field int

const (
	fieldNone   field = iota // after the opcode and the ModRM bytes, as an immediate or a displacement
	fieldReg                 // the reg field of the ModRM byte
	fieldRM                  // the r/m field of the ModRM byte, and its SIB and displacement for a memory operand
	fieldOpcode              // the low three bits of the last opcode byte, with REX.B for r8 and up
)

// operand is one operand of a form. A register's name is its parameter in Maxon; a memory operand has the parameters
// base and disp, and every other kind a parameter named after it.
type operand struct {
	name  string
	kind  kind
	field field
	// byteReg says that the register is used as 8-bit register, where the numbers 4 to 7 are spl to dil only under a REX
	// prefix, which is then written even if it holds no bit.
	byteReg bool
}

// modrm says what a form's ModRM byte is.
type modrm int

const (
	modNone     modrm = iota // there is none
	modOperands              // the operands fill the reg and r/m fields
	modDigit                 // the reg field is the form's /digit, an extension of the opcode
)

// effects are what the instruction selection asks of a form.
type effects struct {
	setsFlags, readsFlags, readsMemory, writesMemory bool
}

// form is one instruction with one shape of operands.
type form struct {
	name     string // "MovRegReg": the encoder is encodeMovRegReg
	mnemonic string
	operands []operand
	// prefix are mandatory prefix bytes, which come before REX.
	prefix []byte
	rexW   bool
	// opcode includes the 0F escape. With an operand in fieldOpcode or of kindCond the last byte is the base that the
	// operand is added to.
	opcode []byte
	modrm  modrm
	digit  int
	effects
}

// choice is a form picked by the value of an immediate.
type choice struct {
	name         string
	alternatives []string // form names, the first whose immediate holds the value wins
}

// step is one form of a sequence and where each of its operands comes from: a parameter of the sequence, or a register.
type step struct {
	form string
	args []string
}

type sequence struct {
	name     string
	operands []operand
	steps    []step
}

func reg(name string, f field) operand { return operand{name: name, kind: kindReg, field: f} }

func byteReg(name string, f field) operand {
	return operand{name: name, kind: kindReg, field: f, byteReg: true}
}

func mem() operand { return operand{name: "base", kind: kindMem, field: fieldRM} }

func rip() operand { return operand{name: "disp", kind: kindRip, field: fieldRM} }

func imm8() operand { return operand{name: "imm", kind: kindImm8} }

func imm32() operand { return operand{name: "imm", kind: kindImm32} }

func imm32U() operand { return operand{name: "imm", kind: kindImm32U} }

func imm64() operand { return operand{name: "imm", kind: kindImm64} }

func rel32() operand { return operand{name: "rel", kind: kindRel32} }

func cond() operand { return operand{name: "cond", kind: kindCond, field: fieldNone} }

var (
	flags     = effects{setsFlags: true}
	noEffects = effects{}
	reads     = effects{readsMemory: true}
	writes    = effects{writesMemory: true}
	readsFlag = effects{readsFlags: true}
)

// aluRegReg are the instructions of the shape op r/m64, r64: the destination is the r/m operand.
func aluRegReg(name, mnemonic string, opcode byte, left, right string, e effects) form {
	return form{
		name: name, mnemonic: mnemonic, operands: []operand{reg(left, fieldRM), reg(right, fieldReg)},
		rexW: true, opcode: []byte{opcode}, modrm: modOperands, effects: e,
	}
}

// group1 are add, and, sub and cmp with an immediate: one opcode for a byte and one for a doubleword, and a /digit.
func group1(mnemonic string, digit int) []form {
	return []form{
		{
			name: capitalize(mnemonic) + "RegImm8", mnemonic: mnemonic, operands: []operand{reg("dest", fieldRM), imm8()},
			rexW: true, opcode: []byte{0x83}, modrm: modDigit, digit: digit, effects: flags,
		},
		{
			name: capitalize(mnemonic) + "RegImm32", mnemonic: mnemonic, operands: []operand{reg("dest", fieldRM), imm32()},
			rexW: true, opcode: []byte{0x81}, modrm: modDigit, digit: digit, effects: flags,
		},
	}
}

// groupF7 are the instructions of one operand with opcode F7: not, neg, mul, div and idiv.
func groupF7(name, mnemonic string, digit int, e effects) form {
	return form{
		name: name, mnemonic: mnemonic, operands: []operand{reg("reg", fieldRM)},
		rexW: true, opcode: []byte{0xF7}, modrm: modDigit, digit: digit, effects: e,
	}
}

// shiftCl are the shifts by cl.
func shiftCl(name, mnemonic string, digit int) form {
	return form{
		name: name, mnemonic: mnemonic, operands: []operand{reg("reg", fieldRM)},
		rexW: true, opcode: []byte{0xD3}, modrm: modDigit, digit: digit, effects: flags,
	}
}

// ripReg are the instructions that move between a register and [rip + disp32].
func ripReg(name, mnemonic string, opcode byte, regIsFirst bool, e effects) form {
	ops := []operand{reg("dest", fieldReg), rip()}
	if !regIsFirst {
		ops = []operand{rip(), reg("src", fieldReg)}
	}
	return form{name: name, mnemonic: mnemonic, operands: ops, rexW: true, opcode: []byte{opcode}, modrm: modOperands, effects: e}
}

var forms = buildForms()

func buildForms() []form {
	var fs []form
	fs = append(fs,
		aluRegReg("MovRegReg", "mov", 0x89, "dest", "src", noEffects),
		aluRegReg("AddRegReg", "add", 0x01, "dest", "src", flags),
		aluRegReg("SubRegReg", "sub", 0x29, "dest", "src", flags),
		aluRegReg("AndRegReg", "and", 0x21, "dest", "src", flags),
		aluRegReg("OrRegReg", "or", 0x09, "dest", "src", flags),
		aluRegReg("XorRegReg", "xor", 0x31, "dest", "src", flags),
		aluRegReg("CmpRegReg", "cmp", 0x39, "left", "right", flags),
		aluRegReg("TestRegReg", "test", 0x85, "left", "right", flags),
		form{
			name: "ImulRegReg", mnemonic: "imul", operands: []operand{reg("dest", fieldReg), reg("src", fieldRM)},
			rexW: true, opcode: []byte{opcodeHigh, 0xAF}, modrm: modOperands, effects: flags,
		},
		form{
			name: "LoadRegBaseDisp", mnemonic: "mov", operands: []operand{reg("dest", fieldReg), mem()},
			rexW: true, opcode: []byte{0x8B}, modrm: modOperands, effects: reads,
		},
		form{
			name: "StoreBaseDispReg", mnemonic: "mov", operands: []operand{mem(), reg("src", fieldReg)},
			rexW: true, opcode: []byte{0x89}, modrm: modOperands, effects: writes,
		},
		form{
			name: "LeaRegBaseDisp", mnemonic: "lea", operands: []operand{reg("dest", fieldReg), mem()},
			rexW: true, opcode: []byte{0x8D}, modrm: modOperands, effects: noEffects,
		},
		form{
			name: "MovzxRegByteBaseDisp", mnemonic: "movzx", operands: []operand{reg("dest", fieldReg), mem()},
			rexW: true, opcode: []byte{opcodeHigh, 0xB6}, modrm: modOperands, effects: reads,
		},
		form{
			name: "StoreBaseDispReg8", mnemonic: "mov", operands: []operand{mem(), byteReg("src", fieldReg)},
			opcode: []byte{0x88}, modrm: modOperands, effects: writes,
		},
		form{
			name: "StoreBaseDispImm8", mnemonic: "mov", operands: []operand{mem(), imm8()},
			opcode: []byte{0xC6}, modrm: modDigit, digit: 0, effects: writes,
		},
		form{
			name: "MovzxRegReg8", mnemonic: "movzx", operands: []operand{reg("dest", fieldReg), byteReg("src", fieldRM)},
			rexW: true, opcode: []byte{opcodeHigh, 0xB6}, modrm: modOperands, effects: noEffects,
		},
		form{
			name: "SetccReg", mnemonic: "setcc", operands: []operand{cond(), byteReg("dest", fieldRM)},
			opcode: []byte{opcodeHigh, 0x90}, modrm: modDigit, digit: 0, effects: readsFlag,
		},
		form{
			name: "MovRegImm32", mnemonic: "mov", operands: []operand{reg("dest", fieldOpcode), imm32U()},
			opcode: []byte{0xB8}, effects: noEffects,
		},
		form{
			name: "MovRegImm32Signed", mnemonic: "mov", operands: []operand{reg("dest", fieldRM), imm32()},
			rexW: true, opcode: []byte{0xC7}, modrm: modDigit, digit: 0, effects: noEffects,
		},
		form{
			name: "MovRegImm64", mnemonic: "mov", operands: []operand{reg("dest", fieldOpcode), imm64()},
			rexW: true, opcode: []byte{0xB8}, effects: noEffects,
		},
	)
	for _, g := range []struct {
		mnemonic string
		digit    int
	}{{"add", 0}, {"and", 4}, {"sub", 5}, {"cmp", 7}} {
		fs = append(fs, group1(g.mnemonic, g.digit)...)
	}
	fs = append(fs,
		groupF7("NotReg", "not", 2, noEffects),
		groupF7("NegReg", "neg", 3, flags),
		groupF7("MulReg", "mul", 4, flags),
		groupF7("DivReg", "div", 6, flags),
		groupF7("IdivReg", "idiv", 7, flags),
		form{name: "Cqo", mnemonic: "cqo", rexW: true, opcode: []byte{0x99}, effects: noEffects},
		shiftCl("ShlRegCl", "shl", 4),
		shiftCl("ShrRegCl", "shr", 5),
		shiftCl("SarRegCl", "sar", 7),
		form{
			name: "SarRegImm8", mnemonic: "sar", operands: []operand{reg("reg", fieldRM), imm8()},
			rexW: true, opcode: []byte{0xC1}, modrm: modDigit, digit: 7, effects: flags,
		},
		form{name: "PushReg", mnemonic: "push", operands: []operand{reg("reg", fieldOpcode)}, opcode: []byte{0x50}, effects: writes},
		form{name: "PopReg", mnemonic: "pop", operands: []operand{reg("reg", fieldOpcode)}, opcode: []byte{0x58}, effects: reads},
		form{name: "Ret", mnemonic: "ret", opcode: []byte{0xC3}, effects: reads},
		form{name: "Ud2", mnemonic: "ud2", opcode: []byte{opcodeHigh, 0x0B}, effects: noEffects},
		form{
			name: "RepMovsb", mnemonic: "rep movsb", prefix: []byte{0xF3}, opcode: []byte{0xA4},
			readsMemory: true, writesMemory: true,
		},
		form{name: "RepStosq", mnemonic: "rep stosq", prefix: []byte{0xF3}, rexW: true, opcode: []byte{0xAB}, effects: writes},
		form{name: "RepeCmpsb", mnemonic: "repe cmpsb", prefix: []byte{0xF3}, opcode: []byte{0xA6}, setsFlags: true, readsMemory: true},
		ripReg("LeaRegRip", "lea", 0x8D, true, noEffects),
		ripReg("LoadRegRip", "mov", 0x8B, true, reads),
		ripReg("StoreRipReg", "mov", 0x89, false, writes),
		form{
			name: "CallRip", mnemonic: "call", operands: []operand{rip()},
			opcode: []byte{0xFF}, modrm: modDigit, digit: 2, readsMemory: true, writesMemory: true,
		},
		form{name: "JmpRel32", mnemonic: "jmp", operands: []operand{rel32()}, opcode: []byte{0xE9}, effects: noEffects},
		form{name: "CallRel32", mnemonic: "call", operands: []operand{rel32()}, opcode: []byte{0xE8}, effects: writes},
		form{
			name: "JccRel32", mnemonic: "jcc", operands: []operand{cond(), rel32()},
			opcode: []byte{opcodeHigh, 0x80}, effects: readsFlag,
		},
	)
	return fs
}

// choices are what the backend asks for by the value of an immediate: the shortest form that holds it.
var choices = []choice{
	{"AddRegImm", []string{"AddRegImm8", "AddRegImm32"}},
	{"AndRegImm", []string{"AndRegImm8", "AndRegImm32"}},
	{"SubRegImm", []string{"SubRegImm8", "SubRegImm32"}},
	{"CmpRegImm", []string{"CmpRegImm8", "CmpRegImm32"}},
	{"MovRegImm", []string{"MovRegImm32", "MovRegImm32Signed", "MovRegImm64"}},
}

// sequences are forms the backend never emits one of alone.
var sequences = []sequence{
	{
		name:     "SetccRax",
		operands: []operand{cond()},
		steps: []step{
			{"SetccReg", []string{"cond", "rax"}},
			{"MovzxRegReg8", []string{"rax", "rax"}},
		},
	},
}
