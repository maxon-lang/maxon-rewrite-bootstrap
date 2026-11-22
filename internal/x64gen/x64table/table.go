package x64table

// The instruction table of the x64 encoder: the source of truth for self/backend/x64_gen.maxon. A row is a form, one
// instruction with one shape of operands, named as maxon's X64Backend.maxon names its encoders (encodeMovRegReg,
// encodeLoadRegBaseDisp, encodePushReg). A Choice selects among forms by the value of an immediate, as the Go backend's
// AddImm and MovImm do, and a Sequence is several forms in a row that the backend always emits together.

import "minimaxon/internal/targetgen/target"

// Encoding constants that the generated helpers and the reference encoder of the test both read.
const (
	rexBase     = 0x40
	rexWide     = 0x08
	rexReg      = 0x04
	rexRm       = 0x01
	rexIndex    = 0x02
	modRegister = 0xC0
	modDisp8    = 0x40
	modDisp32   = 0x80
	ripRelative = 5
	sibRsp      = 0x24
	// sibFollows is the r/m field that says a SIB byte follows the ModRM byte; sibScaleField and sibIndexField are what
	// the SIB byte's scale and index fields are multiplied by.
	sibFollows    = 4
	sibScaleField = 64
	sibIndexField = 8
	// wordScale and byteScale are the scales of an index of words and of bytes.
	wordScale      = 8
	byteScale      = 1
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
	// gsSegmentPrefix addresses memory from the base of gs, which on Windows x64 is the running thread's TEB, and
	// fsSegmentPrefix from the base of fs, which on Linux x64 is the thread pointer that arch_prctl and clone set.
	gsSegmentPrefix = 0x65
	fsSegmentPrefix = 0x64
	// repPrefix is also the mandatory prefix of pause, which is a rep nop.
	repPrefix = 0xF3
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
	{"rexIndex", rexIndex},
	{"modRegister", modRegister},
	{"modDisp8", modDisp8},
	{"modDisp32", modDisp32},
	{"ripRelative", ripRelative},
	{"sibRsp", sibRsp},
	{"sibFollows", sibFollows},
	{"sibScaleField", sibScaleField},
	{"sibIndexField", sibIndexField},
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

// registers are the general registers in the order of their encoding numbers, which is the order of maxon's X64Register,
// and vectorRegisters the xmm registers, which the register allocator numbers after the general ones: a register's
// number is its index in registers, or len(registers) plus its index here. Both are the target table's.
var (
	registers       = target.X64.Names(target.General)
	vectorRegisters = target.X64.Names(target.Vector)
)

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
	kindReg      kind = iota // a general register
	kindMem                  // [base + disp], the displacement as short as it can be
	kindMemIndex             // [base + index*scale + disp], through a SIB byte, the displacement as short as it can be
	kindRip                  // [rip + disp32], whose displacement a fixup patches
	kindImm8                 // a signed byte
	kindImm32                // a signed doubleword
	kindImm32U               // an unsigned doubleword
	kindImm64                // a quadword
	kindRel32                // a signed doubleword relative to the end of the instruction
	kindCond                 // a condition code, added to the last opcode byte
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
// base and disp, an indexed one base, index and disp, and every other kind a parameter named after it.
type operand struct {
	name  string
	kind  kind
	field field
	// byteReg says that the register is used as 8-bit register, where the numbers 4 to 7 are spl to dil only under a REX
	// prefix, which is then written even if it holds no bit.
	byteReg bool
	// vector says that the register is an xmm register, which the encoder is given as the general register of the same
	// encoding number and the allocator gives a value of the xmm class.
	vector bool
	// access is what the instruction does with a register operand; the base and the index of a memory operand are always
	// read.
	access access
	// scale is what an indexed memory operand multiplies its index by: 1, 2, 4 or 8.
	scale int
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
	// commutes says that a two-address form gives the same result with its two uses swapped, so the allocator may
	// reuse the second one's register when only that one dies.
	commutes bool
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

func xmm(name string, f field, a access) operand {
	return operand{name: name, kind: kindReg, field: f, vector: true, access: a}
}

func mem() operand { return operand{name: "base", kind: kindMem, field: fieldRM} }

// memIndex is [base + index*scale + disp]: an element of a buffer, at an index the scale makes a byte offset.
func memIndex(scale int) operand {
	return operand{name: "base", kind: kindMemIndex, field: fieldRM, scale: scale}
}

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
	// widens multiplies rax by its operand into the 128 bits of rdx:rax.
	widens = alloc{fixedUses: []string{"rax"}, fixedDefs: []string{"rax", "rdx"}}
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

// commutative marks a two-address form whose uses may be swapped.
func commutative(f form) form {
	f.commutes = true
	return f
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
		commutative(aluRegReg("AddRegReg", "add", 0x01, "dest", "src", readWrite, flags)),
		aluRegReg("SubRegReg", "sub", 0x29, "dest", "src", readWrite, flags),
		commutative(aluRegReg("AndRegReg", "and", 0x21, "dest", "src", readWrite, flags)),
		commutative(aluRegReg("OrRegReg", "or", 0x09, "dest", "src", readWrite, flags)),
		commutative(aluRegReg("XorRegReg", "xor", 0x31, "dest", "src", readWrite, flags)),
		aluRegReg("CmpRegReg", "cmp", 0x39, "left", "right", read, flags),
		aluRegReg("TestRegReg", "test", 0x85, "left", "right", read, flags),
		commutative(form{
			name: "ImulRegReg", mnemonic: "imul", operands: []operand{reg("dest", fieldReg, readWrite), reg("src", fieldRM, read)},
			rexW: true, opcode: []byte{opcodeHigh, 0xAF}, modrm: modOperands, effects: flags,
		}),
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
		// The indexed forms read and write an element of a buffer: a word at index*8, or a byte at index.
		form{
			name: "LoadRegBaseIndex8Disp", mnemonic: "mov", operands: []operand{reg("dest", fieldReg, write), memIndex(wordScale)},
			rexW: true, opcode: []byte{0x8B}, modrm: modOperands, effects: reads,
		},
		form{
			name: "StoreBaseIndex8DispReg", mnemonic: "mov", operands: []operand{memIndex(wordScale), reg("src", fieldReg, read)},
			rexW: true, opcode: []byte{0x89}, modrm: modOperands, effects: writes,
		},
		form{
			name: "LeaRegBaseIndex8Disp", mnemonic: "lea", operands: []operand{reg("dest", fieldReg, write), memIndex(wordScale)},
			rexW: true, opcode: []byte{0x8D}, modrm: modOperands, effects: noEffects,
		},
		// A word at a byte offset, base plus index, which an address the runtime adds up is.
		form{
			name: "LoadRegBaseIndex1Disp", mnemonic: "mov", operands: []operand{reg("dest", fieldReg, write), memIndex(byteScale)},
			rexW: true, opcode: []byte{0x8B}, modrm: modOperands, effects: reads,
		},
		form{
			name: "StoreBaseIndex1DispReg", mnemonic: "mov", operands: []operand{memIndex(byteScale), reg("src", fieldReg, read)},
			rexW: true, opcode: []byte{0x89}, modrm: modOperands, effects: writes,
		},
		form{
			name: "MovzxRegByteBaseIndex1Disp", mnemonic: "movzx", operands: []operand{reg("dest", fieldReg, write), memIndex(byteScale)},
			rexW: true, opcode: []byte{opcodeHigh, 0xB6}, modrm: modOperands, effects: reads,
		},
		form{
			name: "StoreBaseIndex1DispReg8", mnemonic: "mov", operands: []operand{memIndex(byteScale), byteReg("src", fieldReg, read)},
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
	}{{"add", 0, readWrite}, {"and", 4, readWrite}, {"sub", 5, readWrite}, {"cmp", 7, read}, {"or", 1, readWrite}, {"xor", 6, readWrite}} {
		fs = append(fs, group1(g.mnemonic, g.digit, g.access)...)
	}
	fs = append(fs,
		// imul with an immediate writes the product into a register of its own, which need not be the one it reads.
		form{
			name: "ImulRegRegImm8", mnemonic: "imul", operands: []operand{reg("dest", fieldReg, write), reg("src", fieldRM, read), imm8()},
			rexW: true, opcode: []byte{0x6B}, modrm: modOperands, effects: flags,
		},
		form{
			name: "ImulRegRegImm32", mnemonic: "imul", operands: []operand{reg("dest", fieldReg, write), reg("src", fieldRM, read), imm32()},
			rexW: true, opcode: []byte{0x69}, modrm: modOperands, effects: flags,
		},
		// The shifts by a count below the word's bits, which no branch need guard.
		form{
			name: "ShlRegImm8", mnemonic: "shl", operands: []operand{reg("reg", fieldRM, readWrite), imm8()},
			rexW: true, opcode: []byte{0xC1}, modrm: modDigit, digit: 4, effects: flags,
		},
		form{
			name: "ShrRegImm8", mnemonic: "shr", operands: []operand{reg("reg", fieldRM, readWrite), imm8()},
			rexW: true, opcode: []byte{0xC1}, modrm: modDigit, digit: 5, effects: flags,
		},
		// A comparison of a register with a word in memory, which need not be loaded into a register first.
		form{
			name: "CmpRegBaseDisp", mnemonic: "cmp", operands: []operand{reg("left", fieldReg, read), mem()},
			rexW: true, opcode: []byte{0x3B}, modrm: modOperands, setsFlags: true, readsMemory: true,
		},
		// The 32-bit exclusive or of a register with itself clears the whole register in two or three bytes; the encoder
		// writes it for a constant 0 where the flags it writes are dead.
		form{
			name: "XorReg32Reg32", mnemonic: "xor", operands: []operand{reg("dest", fieldRM, readWrite), reg("src", fieldReg, read)},
			opcode: []byte{0x31}, modrm: modOperands, effects: flags,
		},
		// leave drops a frame: it sets rsp to rbp and pops rbp.
		form{name: "Leave", mnemonic: "leave", opcode: []byte{0xC9}, effects: reads},
	)
	fs = append(fs,
		groupF7("NotReg", "not", 2, readWrite, noEffects, alloc{}),
		groupF7("NegReg", "neg", 3, readWrite, flags, alloc{}),
		groupF7("MulReg", "mul", 4, read, flags, widens),
		groupF7("ImulReg", "imul", 5, read, flags, widens),
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
		form{
			name: "LoadRegFsBaseDisp", mnemonic: "mov", operands: []operand{reg("dest", fieldReg, write), mem()},
			prefix: []byte{fsSegmentPrefix}, rexW: true, opcode: []byte{0x8B}, modrm: modOperands, effects: reads,
		},
		// A system call is a call of the system call convention, which says what it reads and clobbers: rcx and r11,
		// which the instruction itself overwrites, among them.
		form{
			name: "Syscall", mnemonic: "syscall", opcode: []byte{opcodeHigh, 0x05}, readsMemory: true, writesMemory: true,
			call: true,
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

// sseForms are the SSE2 forms of the floats, which are in xmm registers: a form that names only xmm registers has them
// in its ModRM byte as a general form has general ones, and a conversion or a movq names one of each class. A scalar
// double is the low 64 bits of its register; movaps copies the whole register, so a copy depends on nothing the
// register held, and movups saves and restores all 128 bits of a register the convention keeps.
func sseForms() []form {
	sse := func(name, mnemonic string, prefix []byte, op byte, operands []operand, e effects) form {
		return form{name: name, mnemonic: mnemonic, operands: operands, prefix: prefix, opcode: []byte{opcodeHigh, op}, modrm: modOperands, effects: e}
	}
	twoAddress := func(name, mnemonic string, op byte) form {
		return sse(name, mnemonic, []byte{scalarDoublePrefix}, op, []operand{xmm("dest", fieldReg, readWrite), xmm("src", fieldRM, read)}, noEffects)
	}
	oneAddress := func(name, mnemonic string, prefix []byte, op byte) form {
		return sse(name, mnemonic, prefix, op, []operand{xmm("dest", fieldReg, write), xmm("src", fieldRM, read)}, noEffects)
	}
	wide := func(f form) form {
		f.rexW = true
		return f
	}
	return []form{
		oneAddress("MovapsRegReg", "movaps", nil, 0x28),
		commutative(sse("XorpsRegReg", "xorps", nil, 0x57, []operand{xmm("dest", fieldReg, readWrite), xmm("src", fieldRM, read)}, noEffects)),
		commutative(twoAddress("AddsdRegReg", "addsd", 0x58)),
		twoAddress("SubsdRegReg", "subsd", 0x5C),
		commutative(twoAddress("MulsdRegReg", "mulsd", 0x59)),
		twoAddress("DivsdRegReg", "divsd", 0x5E),
		oneAddress("SqrtsdRegReg", "sqrtsd", []byte{scalarDoublePrefix}, 0x51),
		sse("UcomisdRegReg", "ucomisd", []byte{operandSizePrefix}, 0x2E, []operand{xmm("left", fieldReg, read), xmm("right", fieldRM, read)}, flags),
		{
			name: "RoundsdRegRegImm8", mnemonic: "roundsd", operands: []operand{xmm("dest", fieldReg, write), xmm("src", fieldRM, read), imm8()},
			prefix: []byte{operandSizePrefix}, opcode: []byte{opcodeHigh, 0x3A, 0x0B}, modrm: modOperands, effects: noEffects,
		},
		wide(sse("Cvtsi2sdRegReg", "cvtsi2sd", []byte{scalarDoublePrefix}, 0x2A, []operand{xmm("dest", fieldReg, write), reg("src", fieldRM, read)}, noEffects)),
		wide(sse("Cvttsd2siRegReg", "cvttsd2si", []byte{scalarDoublePrefix}, 0x2C, []operand{reg("dest", fieldReg, write), xmm("src", fieldRM, read)}, noEffects)),
		wide(sse("MovqXmmReg", "movq", []byte{operandSizePrefix}, 0x6E, []operand{xmm("dest", fieldReg, write), reg("src", fieldRM, read)}, noEffects)),
		wide(sse("MovqRegXmm", "movq", []byte{operandSizePrefix}, 0x7E, []operand{reg("dest", fieldRM, write), xmm("src", fieldReg, read)}, noEffects)),
		sse("MovsdRegBaseDisp", "movsd", []byte{scalarDoublePrefix}, 0x10, []operand{xmm("dest", fieldReg, write), mem()}, reads),
		sse("MovsdBaseDispReg", "movsd", []byte{scalarDoublePrefix}, 0x11, []operand{mem(), xmm("src", fieldReg, read)}, writes),
		sse("MovsdRegBaseIndex8Disp", "movsd", []byte{scalarDoublePrefix}, 0x10, []operand{xmm("dest", fieldReg, write), memIndex(wordScale)}, reads),
		sse("MovsdBaseIndex8DispReg", "movsd", []byte{scalarDoublePrefix}, 0x11, []operand{memIndex(wordScale), xmm("src", fieldReg, read)}, writes),
		sse("MovsdRegRip", "movsd", []byte{scalarDoublePrefix}, 0x10, []operand{xmm("dest", fieldReg, write), rip()}, reads),
		sse("MovupsRegBaseDisp", "movups", nil, 0x10, []operand{xmm("dest", fieldReg, write), mem()}, reads),
		sse("MovupsBaseDispReg", "movups", nil, 0x11, []operand{mem(), xmm("src", fieldReg, read)}, writes),
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
