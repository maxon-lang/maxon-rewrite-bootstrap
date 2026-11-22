package x64table

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
	// operandSizePrefix makes an instruction's operand 16 bits wide, and is the mandatory prefix of the packed-double
	// forms of SSE2 (movq, ucomisd, roundsd).
	operandSizePrefix = 0x66
	// scalarDoublePrefix is the mandatory prefix of the scalar-double forms of SSE2 (addsd, cvtsi2sd).
	scalarDoublePrefix = 0xF2
	// lockPrefix makes the read, change and write of a memory operand one indivisible step, and a full fence.
	lockPrefix = 0xF0
	// gsSegmentPrefix addresses memory from the base of gs, which on Windows x64 is the running thread's TEB.
	gsSegmentPrefix = 0x65
	// repPrefix is also the mandatory prefix of pause, which is a rep nop.
	repPrefix = 0xF3
	// The ModRM bytes of the forms that name xmm registers only: xmm0 with xmm1, and xmm0 with itself, register to register.
	modrmXmm0Xmm1 = 0xC1
	modrmXmm0Xmm0 = 0xC0
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

// vectorRegisters are the xmm registers, which the register allocator numbers after the general ones: a register's
// number is its index in registers, or len(registers) plus its index here.
var vectorRegisters = []string{
	"xmm0", "xmm1", "xmm2", "xmm3", "xmm4", "xmm5", "xmm6", "xmm7",
	"xmm8", "xmm9", "xmm10", "xmm11", "xmm12", "xmm13", "xmm14", "xmm15",
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
	// access is what the instruction does with a register operand; the base of a memory operand is always read.
	access access
}

// access is whether an instruction reads a register operand, writes it, or both: a two-address form, whose result the
// machine IR gives a register of its own that the allocator must make the same as the operand's (a reuse).
type access int

const (
	accessNone access = iota // not a register operand
	read
	write
	readWrite
)

// alloc is what the register allocator needs of a form beyond its operands: the registers it reads or writes that no
// operand names, which instruction selection moves values into and out of, and whether the calling convention says
// what it reads and clobbers.
type alloc struct {
	// fixedUses are registers the form reads, fixedDefs registers it writes a result to, and clobbers registers it
	// leaves holding nothing.
	fixedUses, fixedDefs, clobbers []string
	// call clobbers what the calling convention of the call does not keep; ret reads the convention's result registers.
	call, ret bool
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
	alloc
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

func reg(name string, f field, a access) operand {
	return operand{name: name, kind: kindReg, field: f, access: a}
}

func byteReg(name string, f field, a access) operand {
	return operand{name: name, kind: kindReg, field: f, byteReg: true, access: a}
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
	// divides reads the dividend from rdx:rax and writes the quotient to rax and the remainder to rdx.
	divides = alloc{fixedUses: []string{"rax", "rdx"}, fixedDefs: []string{"rax", "rdx"}}
	// stringScan walks rsi and rdi for rcx bytes, and leaves the three past where it stopped.
	stringScan = alloc{fixedUses: []string{"rsi", "rdi", "rcx"}, clobbers: []string{"rsi", "rdi", "rcx"}}
	flags      = effects{setsFlags: true}
	noEffects  = effects{}
	reads      = effects{readsMemory: true}
	writes     = effects{writesMemory: true}
	readsFlag  = effects{readsFlags: true}
)

// aluRegReg are the instructions of the shape op r/m64, r64: the destination is the r/m operand.
func aluRegReg(name, mnemonic string, opcode byte, left, right string, leftAccess access, e effects) form {
	return form{
		name: name, mnemonic: mnemonic, operands: []operand{reg(left, fieldRM, leftAccess), reg(right, fieldReg, read)},
		rexW: true, opcode: []byte{opcode}, modrm: modOperands, effects: e,
	}
}

// group1 are add, and, sub and cmp with an immediate: one opcode for a byte and one for a doubleword, and a /digit.
func group1(mnemonic string, digit int, a access) []form {
	return []form{
		{
			name: capitalize(mnemonic) + "RegImm8", mnemonic: mnemonic, operands: []operand{reg("dest", fieldRM, a), imm8()},
			rexW: true, opcode: []byte{0x83}, modrm: modDigit, digit: digit, effects: flags,
		},
		{
			name: capitalize(mnemonic) + "RegImm32", mnemonic: mnemonic, operands: []operand{reg("dest", fieldRM, a), imm32()},
			rexW: true, opcode: []byte{0x81}, modrm: modDigit, digit: digit, effects: flags,
		},
	}
}

// groupF7 are the instructions of one operand with opcode F7: not, neg, mul, div and idiv.
func groupF7(name, mnemonic string, digit int, a access, e effects, al alloc) form {
	return form{
		name: name, mnemonic: mnemonic, operands: []operand{reg("reg", fieldRM, a)},
		rexW: true, opcode: []byte{0xF7}, modrm: modDigit, digit: digit, effects: e, alloc: al,
	}
}

// shiftCl are the shifts by cl.
func shiftCl(name, mnemonic string, digit int) form {
	return form{
		name: name, mnemonic: mnemonic, operands: []operand{reg("reg", fieldRM, readWrite)},
		rexW: true, opcode: []byte{0xD3}, modrm: modDigit, digit: digit, effects: flags,
		fixedUses: []string{"rcx"},
	}
}

// ripReg are the instructions that move between a register and [rip + disp32].
func ripReg(name, mnemonic string, opcode byte, regIsFirst bool, e effects) form {
	ops := []operand{reg("dest", fieldReg, write), rip()}
	if !regIsFirst {
		ops = []operand{rip(), reg("src", fieldReg, read)}
	}
	return form{name: name, mnemonic: mnemonic, operands: ops, rexW: true, opcode: []byte{opcode}, modrm: modOperands, effects: e}
}

var forms = buildForms()

func buildForms() []form {
	var fs []form
	fs = append(fs,
		aluRegReg("MovRegReg", "mov", 0x89, "dest", "src", write, noEffects),
		aluRegReg("AddRegReg", "add", 0x01, "dest", "src", readWrite, flags),
		aluRegReg("SubRegReg", "sub", 0x29, "dest", "src", readWrite, flags),
		aluRegReg("AndRegReg", "and", 0x21, "dest", "src", readWrite, flags),
		aluRegReg("OrRegReg", "or", 0x09, "dest", "src", readWrite, flags),
		aluRegReg("XorRegReg", "xor", 0x31, "dest", "src", readWrite, flags),
		aluRegReg("CmpRegReg", "cmp", 0x39, "left", "right", read, flags),
		aluRegReg("TestRegReg", "test", 0x85, "left", "right", read, flags),
		form{
			name: "ImulRegReg", mnemonic: "imul", operands: []operand{reg("dest", fieldReg, readWrite), reg("src", fieldRM, read)},
			rexW: true, opcode: []byte{opcodeHigh, 0xAF}, modrm: modOperands, effects: flags,
		},
		form{
			name: "LoadRegBaseDisp", mnemonic: "mov", operands: []operand{reg("dest", fieldReg, write), mem()},
			rexW: true, opcode: []byte{0x8B}, modrm: modOperands, effects: reads,
		},
		form{
			name: "StoreBaseDispReg", mnemonic: "mov", operands: []operand{mem(), reg("src", fieldReg, read)},
			rexW: true, opcode: []byte{0x89}, modrm: modOperands, effects: writes,
		},
		form{
			name: "LeaRegBaseDisp", mnemonic: "lea", operands: []operand{reg("dest", fieldReg, write), mem()},
			rexW: true, opcode: []byte{0x8D}, modrm: modOperands, effects: noEffects,
		},
		form{
			name: "MovzxRegByteBaseDisp", mnemonic: "movzx", operands: []operand{reg("dest", fieldReg, write), mem()},
			rexW: true, opcode: []byte{opcodeHigh, 0xB6}, modrm: modOperands, effects: reads,
		},
		form{
			name: "StoreBaseDispReg8", mnemonic: "mov", operands: []operand{mem(), byteReg("src", fieldReg, read)},
			opcode: []byte{0x88}, modrm: modOperands, effects: writes,
		},
		form{
			name: "MovzxRegWordBaseDisp", mnemonic: "movzx", operands: []operand{reg("dest", fieldReg, write), mem()},
			rexW: true, opcode: []byte{opcodeHigh, 0xB7}, modrm: modOperands, effects: reads,
		},
		// A 32-bit load clears the upper half of its register, which is the zero extension a raw load gives.
		form{
			name: "LoadReg32BaseDisp", mnemonic: "mov", operands: []operand{reg("dest", fieldReg, write), mem()},
			opcode: []byte{0x8B}, modrm: modOperands, effects: reads,
		},
		form{
			name: "StoreBaseDispReg16", mnemonic: "mov", operands: []operand{mem(), reg("src", fieldReg, read)},
			prefix: []byte{operandSizePrefix}, opcode: []byte{0x89}, modrm: modOperands, effects: writes,
		},
		form{
			name: "StoreBaseDispReg32", mnemonic: "mov", operands: []operand{mem(), reg("src", fieldReg, read)},
			opcode: []byte{0x89}, modrm: modOperands, effects: writes,
		},
		form{
			name: "StoreBaseDispImm8", mnemonic: "mov", operands: []operand{mem(), imm8()},
			opcode: []byte{0xC6}, modrm: modDigit, digit: 0, effects: writes,
		},
		// The doubleword is sign-extended to the word it stores.
		form{
			name: "StoreBaseDispImm32", mnemonic: "mov", operands: []operand{mem(), imm32()},
			rexW: true, opcode: []byte{0xC7}, modrm: modDigit, digit: 0, effects: writes,
		},
		form{
			name: "MovzxRegReg8", mnemonic: "movzx", operands: []operand{reg("dest", fieldReg, write), byteReg("src", fieldRM, read)},
			rexW: true, opcode: []byte{opcodeHigh, 0xB6}, modrm: modOperands, effects: noEffects,
		},
		form{
			name: "SetccReg", mnemonic: "setcc", operands: []operand{cond(), byteReg("dest", fieldRM, write)},
			opcode: []byte{opcodeHigh, 0x90}, modrm: modDigit, digit: 0, effects: readsFlag,
		},
		form{
			name: "MovRegImm32", mnemonic: "mov", operands: []operand{reg("dest", fieldOpcode, write), imm32U()},
			opcode: []byte{0xB8}, effects: noEffects,
		},
		form{
			name: "MovRegImm32Signed", mnemonic: "mov", operands: []operand{reg("dest", fieldRM, write), imm32()},
			rexW: true, opcode: []byte{0xC7}, modrm: modDigit, digit: 0, effects: noEffects,
		},
		form{
			name: "MovRegImm64", mnemonic: "mov", operands: []operand{reg("dest", fieldOpcode, write), imm64()},
			rexW: true, opcode: []byte{0xB8}, effects: noEffects,
		},
	)
	for _, g := range []struct {
		mnemonic string
		digit    int
		access   access
	}{{"add", 0, readWrite}, {"and", 4, readWrite}, {"sub", 5, readWrite}, {"cmp", 7, read}} {
		fs = append(fs, group1(g.mnemonic, g.digit, g.access)...)
	}
	fs = append(fs,
		groupF7("NotReg", "not", 2, readWrite, noEffects, alloc{}),
		groupF7("NegReg", "neg", 3, readWrite, flags, alloc{}),
		groupF7("MulReg", "mul", 4, read, flags, alloc{fixedUses: []string{"rax"}, fixedDefs: []string{"rax", "rdx"}}),
		groupF7("DivReg", "div", 6, read, flags, divides),
		groupF7("IdivReg", "idiv", 7, read, flags, divides),
		form{
			name: "Cqo", mnemonic: "cqo", rexW: true, opcode: []byte{0x99}, effects: noEffects,
			fixedUses: []string{"rax"}, fixedDefs: []string{"rdx"},
		},
		shiftCl("ShlRegCl", "shl", 4),
		shiftCl("ShrRegCl", "shr", 5),
		shiftCl("SarRegCl", "sar", 7),
		form{
			name: "SarRegImm8", mnemonic: "sar", operands: []operand{reg("reg", fieldRM, readWrite), imm8()},
			rexW: true, opcode: []byte{0xC1}, modrm: modDigit, digit: 7, effects: flags,
		},
		form{name: "PushReg", mnemonic: "push", operands: []operand{reg("reg", fieldOpcode, read)}, opcode: []byte{0x50}, effects: writes},
		// The doubleword is sign-extended to the word it pushes.
		form{name: "PushImm32", mnemonic: "push", operands: []operand{imm32()}, opcode: []byte{0x68}, effects: writes},
		form{name: "PopReg", mnemonic: "pop", operands: []operand{reg("reg", fieldOpcode, write)}, opcode: []byte{0x58}, effects: reads},
		form{name: "Ret", mnemonic: "ret", opcode: []byte{0xC3}, effects: reads, ret: true},
		form{name: "Ud2", mnemonic: "ud2", opcode: []byte{opcodeHigh, 0x0B}, effects: noEffects},
		form{
			name: "RepMovsb", mnemonic: "rep movsb", prefix: []byte{0xF3}, opcode: []byte{0xA4},
			readsMemory: true, writesMemory: true, alloc: stringScan,
		},
		form{
			name: "RepStosq", mnemonic: "rep stosq", prefix: []byte{0xF3}, rexW: true, opcode: []byte{0xAB}, effects: writes,
			fixedUses: []string{"rdi", "rcx", "rax"}, clobbers: []string{"rdi", "rcx"},
		},
		form{
			name: "RepeCmpsb", mnemonic: "repe cmpsb", prefix: []byte{0xF3}, opcode: []byte{0xA6},
			setsFlags: true, readsMemory: true, alloc: stringScan,
		},
		ripReg("LeaRegRip", "lea", 0x8D, true, noEffects),
		ripReg("LoadRegRip", "mov", 0x8B, true, reads),
		ripReg("StoreRipReg", "mov", 0x89, false, writes),
		form{
			name: "CallRip", mnemonic: "call", operands: []operand{rip()},
			opcode: []byte{0xFF}, modrm: modDigit, digit: 2, readsMemory: true, writesMemory: true,
			call: true,
		},
		form{
			name: "CallReg", mnemonic: "call", operands: []operand{reg("reg", fieldRM, read)},
			opcode: []byte{0xFF}, modrm: modDigit, digit: 2, readsMemory: true, writesMemory: true,
			call: true,
		},
		// The atomic forms and the thread's context, which the runtime's scheduler is written on (README, "The runtime").
		form{
			name: "LockXaddBaseDispReg", mnemonic: "lock xadd", operands: []operand{mem(), reg("src", fieldReg, readWrite)},
			prefix: []byte{lockPrefix}, rexW: true, opcode: []byte{opcodeHigh, 0xC1}, modrm: modOperands,
			setsFlags: true, readsMemory: true, writesMemory: true,
		},
		// xchg with a memory operand is locked whether or not it has the prefix.
		form{
			name: "XchgBaseDispReg", mnemonic: "xchg", operands: []operand{mem(), reg("src", fieldReg, readWrite)},
			rexW: true, opcode: []byte{0x87}, modrm: modOperands, readsMemory: true, writesMemory: true,
		},
		form{
			name: "LockCmpxchgBaseDispReg", mnemonic: "lock cmpxchg", operands: []operand{mem(), reg("src", fieldReg, read)},
			prefix: []byte{lockPrefix}, rexW: true, opcode: []byte{opcodeHigh, 0xB1}, modrm: modOperands,
			setsFlags: true, readsMemory: true, writesMemory: true,
			fixedUses: []string{"rax"}, fixedDefs: []string{"rax"},
		},
		form{name: "Pause", mnemonic: "pause", prefix: []byte{repPrefix}, opcode: []byte{0x90}, effects: noEffects},
		form{
			name: "LoadRegGsBaseDisp", mnemonic: "mov", operands: []operand{reg("dest", fieldReg, write), mem()},
			prefix: []byte{gsSegmentPrefix}, rexW: true, opcode: []byte{0x8B}, modrm: modOperands, effects: reads,
		},
		form{name: "JmpRel32", mnemonic: "jmp", operands: []operand{rel32()}, opcode: []byte{0xE9}, effects: noEffects},
		form{name: "CallRel32", mnemonic: "call", operands: []operand{rel32()}, opcode: []byte{0xE8}, effects: writes, call: true},
		form{
			name: "JccRel32", mnemonic: "jcc", operands: []operand{cond(), rel32()},
			opcode: []byte{opcodeHigh, 0x80}, effects: readsFlag,
		},
	)
	return append(fs, sseForms()...)
}

// sseForms are the SSE2 forms a float operation is made of. A float is a word in a general register, so an operation
// moves its operands into xmm0 and xmm1 and its result back: the xmm register is fixed, the reg field of a form that
// also names a general register (a /digit), or a ModRM byte of its own after the opcode for one that names only xmm
// registers.
func sseForms() []form {
	toXmm := func(name string, xmm int) form {
		return form{
			name: name, mnemonic: "movq", operands: []operand{reg("src", fieldRM, read)}, prefix: []byte{operandSizePrefix},
			rexW: true, opcode: []byte{opcodeHigh, 0x6E}, modrm: modDigit, digit: xmm, effects: noEffects,
			fixedDefs: []string{vectorRegisters[xmm]},
		}
	}
	xmmOnly := func(name, mnemonic string, prefix byte, op byte, rm byte, e effects, al alloc) form {
		return form{name: name, mnemonic: mnemonic, prefix: []byte{prefix}, opcode: []byte{opcodeHigh, op, rm}, effects: e, alloc: al}
	}
	binary := alloc{fixedUses: []string{"xmm0", "xmm1"}, fixedDefs: []string{"xmm0"}}
	unary := alloc{fixedUses: []string{"xmm0"}, fixedDefs: []string{"xmm0"}}
	return []form{
		toXmm("MovqXmm0Reg", 0),
		toXmm("MovqXmm1Reg", 1),
		{
			name: "MovqRegXmm0", mnemonic: "movq", operands: []operand{reg("dest", fieldRM, write)}, prefix: []byte{operandSizePrefix},
			rexW: true, opcode: []byte{opcodeHigh, 0x7E}, modrm: modDigit, digit: 0, effects: noEffects,
			fixedUses: []string{"xmm0"},
		},
		xmmOnly("AddsdXmm0Xmm1", "addsd", scalarDoublePrefix, 0x58, modrmXmm0Xmm1, noEffects, binary),
		xmmOnly("SubsdXmm0Xmm1", "subsd", scalarDoublePrefix, 0x5C, modrmXmm0Xmm1, noEffects, binary),
		xmmOnly("MulsdXmm0Xmm1", "mulsd", scalarDoublePrefix, 0x59, modrmXmm0Xmm1, noEffects, binary),
		xmmOnly("DivsdXmm0Xmm1", "divsd", scalarDoublePrefix, 0x5E, modrmXmm0Xmm1, noEffects, binary),
		xmmOnly("SqrtsdXmm0Xmm0", "sqrtsd", scalarDoublePrefix, 0x51, modrmXmm0Xmm0, noEffects, unary),
		xmmOnly("UcomisdXmm0Xmm1", "ucomisd", operandSizePrefix, 0x2E, modrmXmm0Xmm1, flags, alloc{fixedUses: []string{"xmm0", "xmm1"}}),
		{
			name: "RoundsdXmm0Xmm0Imm8", mnemonic: "roundsd", operands: []operand{imm8()}, prefix: []byte{operandSizePrefix},
			opcode: []byte{opcodeHigh, 0x3A, 0x0B, modrmXmm0Xmm0}, effects: noEffects, alloc: unary,
		},
		{
			name: "Cvtsi2sdXmm0Reg", mnemonic: "cvtsi2sd", operands: []operand{reg("src", fieldRM, read)}, prefix: []byte{scalarDoublePrefix},
			rexW: true, opcode: []byte{opcodeHigh, 0x2A}, modrm: modDigit, digit: 0, effects: noEffects,
			fixedDefs: []string{"xmm0"},
		},
		{
			name: "Cvttsd2siRaxXmm0", mnemonic: "cvttsd2si", prefix: []byte{scalarDoublePrefix}, rexW: true,
			opcode: []byte{opcodeHigh, 0x2C, modrmXmm0Xmm0}, effects: noEffects,
			fixedUses: []string{"xmm0"}, fixedDefs: []string{"rax"},
		},
	}
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
