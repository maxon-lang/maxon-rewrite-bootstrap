package arm64table

// The instruction table of the arm64 encoder: the source of truth for self/backend/arm64_gen.maxon. A row is a form, one
// instruction with one shape of operands, whose encoding is one 32-bit word: its opcode, the bits every instance has,
// with each operand's field added. The opcodes are maxon's (Arm64Backend.maxon's Arm64Op words), and the fields those
// of the Arm Architecture Reference Manual. A direct form is one that only hand-written code writes, with the registers
// it names itself (the frame's saves and restores, a multiply-add of four registers): it has an encoder, and is no form
// of the machine IR.

import "minimaxon/internal/targetgen/target"

// The fields of an instruction word: the bit each starts at, and how many bits it has.
const (
	wordBytes = 4
	// regBits is the width of a register's field, and zeroRegister the number that is sp where an operand names the
	// stack and the zero register everywhere else.
	regBits      = 5
	zeroRegister = 31
	// rdAt is the field of the register an instruction writes (Rd, or Rt of a load), rnAt of its first source or a
	// memory operand's base, raAt of a multiply-add's addend or a pair's second register, rmAt of its second source, a
	// register offset or an atomic's source (Rm, Rs).
	rdAt = 0
	rnAt = 5
	raAt = 10
	rmAt = 16
	// condAt is the condition of a conditional select, and branchCondAt the one of b.cond.
	condAt       = 12
	branchCondAt = 0
	condBits     = 4
	// imm12At is the 12 bits of an add or sub immediate and of a scaled memory offset, simm9At the 9 bits of an
	// unscaled offset, imm7At the 7 bits of a pair's offset.
	imm12At   = 10
	imm12Bits = 12
	simm9At   = 12
	simm9Bits = 9
	imm7At    = 15
	imm7Bits  = 7
	// imm16At is a move-wide immediate's halfword and hwAt the field that says which halfword of the register it is.
	imm16At   = 5
	imm16Bits = 16
	hwAt      = 21
	hwBits    = 2
	// immrAt and immsAt are a bitfield's rotation and its top bit.
	immrAt    = 16
	immsAt    = 10
	sixBits   = 6
	logicalAt = 10
	// imm19At and imm26At are a branch's distance in words: a conditional one's, and b's and bl's.
	imm19At   = 5
	imm19Bits = 19
	imm26At   = 0
	imm26Bits = 26
	// pageOffsetBits are the low bits of an address that the instruction after an adrp adds to its page.
	pageOffsetBits = 12
	// immloAt and immhiAt are where adr and adrp keep the low two bits and the rest of their 21-bit distance.
	immloAt   = 29
	immloBits = 2
	immhiAt   = 5
	address21 = 21
)

// registers are the general registers in the order of their encoding numbers, which is the order of maxon's
// Arm64Register, and vectorRegisters the d registers, which the register allocator numbers after the general ones: a
// register's number is its index in registers, or len(registers) plus its index here. Both are the target table's.
var (
	registers       = target.Arm64.Names(target.General)
	vectorRegisters = target.Arm64.Names(target.Vector)
)

// condition is a condition of the machine IR (isa.Conditions, in order) and the arm64 condition code that tests it.
// After a subs or a cmp, x64's below is arm64's lo (carry clear) and its aboveEqual hs. After an fcmp, unordered sets V,
// so parity, which x64's ucomisd sets for unordered, is vs.
type condition struct {
	name string
	code int
}

var conditions = []condition{
	{"overflow", 0x6},     // vs
	{"noOverflow", 0x7},   // vc
	{"below", 0x3},        // lo
	{"aboveEqual", 0x2},   // hs
	{"equal", 0x0},        // eq
	{"notEqual", 0x1},     // ne
	{"belowEqual", 0x9},   // ls
	{"above", 0x8},        // hi
	{"sign", 0x4},         // mi
	{"noSign", 0x5},       // pl
	{"parity", 0x6},       // vs
	{"noParity", 0x7},     // vc
	{"less", 0xB},         // lt
	{"greaterEqual", 0xA}, // ge
	{"lessEqual", 0xD},    // le
	{"greater", 0xC},      // gt
}

// alwaysCode is the first of the codes al and nv, which test nothing and have no opposite for cset to invert to.
const alwaysCode = 0xE

// kind is what an operand is.
type kind int

const (
	kindReg kind = iota // a register in a 5-bit field
	kindImm             // an unsigned field, given as its value
	// kindDisp is a memory offset in bytes, which the field holds in units of scale, unsigned or in two's complement.
	kindDisp
	kindMask // a logical immediate, given as its value and held as N:immr:imms
	kindCond // a condition, held as its code
	// kindRel is a branch's distance in bytes from the instruction, held in words, which a label patches.
	kindRel
	// kindAddress is adr's distance in bytes or adrp's in pages, held in 21 bits split into immhi and immlo, which a
	// reference patches.
	kindAddress
	// kindPageOffset is the low 12 bits of an address in units of the access, which the reference that patches the
	// adrp before it patches too.
	kindPageOffset
)

// access is whether an instruction reads a register operand, writes it, or both: a form whose result the machine IR
// gives a register of its own that the allocator must make the same as the operand's (a reuse).
type access int

const (
	accessNone access = iota // not a register operand
	read
	write
	readWrite
)

// operand is one operand of a form: its parameter's name, what it is, and its field.
type operand struct {
	name string
	kind kind
	at   int
	bits int
	// access is what the instruction does with a register operand.
	access access
	// vector says that the register is a d register, which the encoder is given as the general register of the same
	// encoding number and the allocator gives a value of the vector class.
	vector bool
	// stack says that the register's 31 is sp; elsewhere it is the zero register.
	stack bool
	// scale is the bytes of a unit of an offset's or a distance's field.
	scale int
	// signed says that an offset's or a distance's field is two's complement.
	signed bool
	// invert says that a condition's field holds the opposite condition, as cset's does.
	invert bool
}

// alloc is what the register allocator needs of a form beyond its operands: the registers it reads or writes that no
// operand names, which instruction selection moves values into and out of, and whether the calling convention says
// what it reads and clobbers.
type alloc struct {
	fixedUses, fixedDefs, clobbers []string
	call, ret                      bool
}

// effects are what the instruction selection asks of a form.
type effects struct {
	setsFlags, readsFlags, readsMemory, writesMemory bool
}

// form is one instruction with one shape of operands.
type form struct {
	name     string // "AddRegReg": the encoder is encodeArm64AddRegReg
	mnemonic string
	opcode   uint32
	operands []operand
	// direct says that only hand-written code writes the form, so it is no form of the machine IR.
	direct bool
	effects
	alloc
}

func reg(name string, at int, a access) operand {
	return operand{name: name, kind: kindReg, at: at, bits: regBits, access: a}
}

// stackReg is a register whose 31 is sp.
func stackReg(name string, at int, a access) operand {
	o := reg(name, at, a)
	o.stack = true
	return o
}

func vec(name string, at int, a access) operand {
	o := reg(name, at, a)
	o.vector = true
	return o
}

// base is the base register of a memory operand, which may be sp.
func base() operand { return stackReg("base", rnAt, read) }

func imm(name string, at, bits int) operand {
	return operand{name: name, kind: kindImm, at: at, bits: bits}
}

// offset is a memory operand's unsigned offset of 12 bits in units of the access, scaled.
func offset(size int) operand {
	return operand{name: "disp", kind: kindDisp, at: imm12At, bits: imm12Bits, scale: size}
}

// unscaled is a memory operand's signed offset of 9 bits in bytes.
func unscaled() operand {
	return operand{name: "disp", kind: kindDisp, at: simm9At, bits: simm9Bits, scale: 1, signed: true}
}

// pairOffset is a pair's signed offset of 7 bits in units of one register of the pair.
func pairOffset(size int) operand {
	return operand{name: "disp", kind: kindDisp, at: imm7At, bits: imm7Bits, scale: size, signed: true}
}

func mask() operand { return operand{name: "imm", kind: kindMask, at: logicalAt, bits: logicalBits} }

func cond(at int) operand { return operand{name: "cond", kind: kindCond, at: at, bits: condBits} }

func rel(at, bits int) operand {
	return operand{name: "rel", kind: kindRel, at: at, bits: bits, scale: wordBytes, signed: true}
}

func address(name string) operand {
	return operand{name: name, kind: kindAddress, bits: address21, scale: 1, signed: true}
}

func pageOffset(size int) operand {
	return operand{name: "offset", kind: kindPageOffset, at: imm12At, bits: pageOffsetBits, scale: size}
}

var (
	flags     = effects{setsFlags: true}
	noEffects = effects{}
	reads     = effects{readsMemory: true}
	writes    = effects{writesMemory: true}
	readsFlag = effects{readsFlags: true}
	memory    = effects{readsMemory: true, writesMemory: true}
)

// threeReg is an instruction of the shape op rd, rn, rm.
func threeReg(name, mnemonic string, opcode uint32, e effects) form {
	return form{name: name, mnemonic: mnemonic, opcode: opcode, effects: e,
		operands: []operand{reg("dest", rdAt, write), reg("left", rnAt, read), reg("right", rmAt, read)}}
}

// twoReg is an instruction of the shape op rd, rn.
func twoReg(name, mnemonic string, opcode uint32, dest, src operand) form {
	return form{name: name, mnemonic: mnemonic, opcode: opcode, operands: []operand{dest, src}}
}

// compareReg is an instruction of the shape op zr, rn, rm, which only sets the flags.
func compareReg(name, mnemonic string, opcode uint32) form {
	return form{name: name, mnemonic: mnemonic, opcode: opcode, effects: flags,
		operands: []operand{reg("left", rnAt, read), reg("right", rmAt, read)}}
}

// addImm is an add or sub of a 12-bit immediate, shifted left by 12 for an opcode with the shift bit set.
func addImm(name, mnemonic string, opcode uint32, dest operand, e effects) form {
	return form{name: name, mnemonic: mnemonic, opcode: opcode, effects: e,
		operands: []operand{dest, stackReg("src", rnAt, read), imm("imm", imm12At, imm12Bits)}}
}

// logical is an and, orr, eor or ands of a logical immediate.
func logical(name, mnemonic string, opcode uint32, dest operand, e effects) form {
	return form{name: name, mnemonic: mnemonic, opcode: opcode, effects: e,
		operands: []operand{dest, reg("src", rnAt, read), mask()}}
}

// moveWide is a movz, movn or movk of a halfword at a shift of 16 times shift.
func moveWide(name, mnemonic string, opcode uint32, a access) form {
	return form{name: name, mnemonic: mnemonic, opcode: opcode,
		operands: []operand{reg("dest", rdAt, a), imm("imm", imm16At, imm16Bits), imm("shift", hwAt, hwBits)}}
}

// load and store are the forms of a memory operand of one addressing: dest or src is the register, at rdAt, and at
// are the operands of the address.
func load(name, mnemonic string, opcode uint32, dest operand, at ...operand) form {
	return form{name: name, mnemonic: mnemonic, opcode: opcode, effects: reads, operands: append([]operand{dest}, at...)}
}

func store(name, mnemonic string, opcode uint32, src operand, at ...operand) form {
	return form{name: name, mnemonic: mnemonic, opcode: opcode, effects: writes, operands: append([]operand{src}, at...)}
}

// width is one access size of the loads and stores, with the opcodes of each addressing: a scaled unsigned offset, an
// unscaled signed one, and a register index scaled by the size. A form's name is its mnemonic and the width.
type width struct {
	load, store         string
	size                int
	vector              bool
	offLoad, offStore   uint32
	unscLoad, unscStore uint32
	idxLoad, idxStore   uint32
}

// The register-offset forms add the index shifted left by the log of the size (option LSL, S=1); a byte's is not
// shifted (S=0), and Idx1 is a word's at an index of bytes.
var widths = []width{
	{"Ldr64", "Str64", 8, false, 0xF9400000, 0xF9000000, 0xF8400000, 0xF8000000, 0xF8607800, 0xF8207800},
	{"Ldr32", "Str32", 4, false, 0xB9400000, 0xB9000000, 0xB8400000, 0xB8000000, 0xB8607800, 0xB8207800},
	{"Ldrh", "Strh", 2, false, 0x79400000, 0x79000000, 0x78400000, 0x78000000, 0x78607800, 0x78207800},
	{"Ldrb", "Strb", 1, false, 0x39400000, 0x39000000, 0x38400000, 0x38000000, 0x38606800, 0x38206800},
	{"LdrD", "StrD", 8, true, 0xFD400000, 0xFD000000, 0xFC400000, 0xFC000000, 0xFC607800, 0xFC207800},
}

func memoryForms() []form {
	var fs []form
	for _, w := range widths {
		r := reg
		if w.vector {
			r = vec
		}
		fs = append(fs,
			load(w.load+"Off", "ldr", w.offLoad, r("dest", rdAt, write), base(), offset(w.size)),
			store(w.store+"Off", "str", w.offStore, r("src", rdAt, read), base(), offset(w.size)),
			load(w.load+"Unscaled", "ldur", w.unscLoad, r("dest", rdAt, write), base(), unscaled()),
			store(w.store+"Unscaled", "stur", w.unscStore, r("src", rdAt, read), base(), unscaled()),
			load(w.load+"Idx", "ldr", w.idxLoad, r("dest", rdAt, write), base(), reg("index", rmAt, read)),
			store(w.store+"Idx", "str", w.idxStore, r("src", rdAt, read), base(), reg("index", rmAt, read)),
		)
	}
	return append(fs,
		// The signed loads extend to the whole register.
		load("LdrswOff", "ldrsw", 0xB9800000, reg("dest", rdAt, write), base(), offset(4)),
		load("LdrshOff", "ldrsh", 0x79800000, reg("dest", rdAt, write), base(), offset(2)),
		load("LdrsbOff", "ldrsb", 0x39800000, reg("dest", rdAt, write), base(), offset(1)),
		// A word at a byte offset, base plus index, which an address the runtime adds up is.
		load("Ldr64Idx1", "ldr", 0xF8606800, reg("dest", rdAt, write), base(), reg("index", rmAt, read)),
		store("Str64Idx1", "str", 0xF8206800, reg("src", rdAt, read), base(), reg("index", rmAt, read)),
	)
}

// pairForms are ldp and stp of two words or two doubles at a signed offset, before which the base is written back
// (pre), or after which it is (post): the frame's saves and restores, which only the encoder writes.
func pairForms() []form {
	var fs []form
	for _, p := range []struct {
		suffix               string
		vector               bool
		size                 int
		stp, stpPre, stpPost uint32
		ldp, ldpPre, ldpPost uint32
	}{
		{"64", false, 8, 0xA9000000, 0xA9800000, 0xA8800000, 0xA9400000, 0xA9C00000, 0xA8C00000},
		{"D", true, 8, 0x6D000000, 0x6D800000, 0x6C800000, 0x6D400000, 0x6DC00000, 0x6CC00000},
	} {
		r := reg
		if p.vector {
			r = vec
		}
		pair := func(name, mnemonic string, opcode uint32, a access, baseAccess access, e effects) form {
			return form{name: name, mnemonic: mnemonic, opcode: opcode, direct: true, effects: e, operands: []operand{
				r("first", rdAt, a), r("second", raAt, a), stackReg("base", rnAt, baseAccess), pairOffset(p.size),
			}}
		}
		fs = append(fs,
			pair("Stp"+p.suffix, "stp", p.stp, read, read, writes),
			pair("Stp"+p.suffix+"Pre", "stp", p.stpPre, read, readWrite, writes),
			pair("Stp"+p.suffix+"Post", "stp", p.stpPost, read, readWrite, writes),
			pair("Ldp"+p.suffix, "ldp", p.ldp, write, read, reads),
			pair("Ldp"+p.suffix+"Pre", "ldp", p.ldpPre, write, readWrite, reads),
			pair("Ldp"+p.suffix+"Post", "ldp", p.ldpPost, write, readWrite, reads),
		)
	}
	return fs
}

// floatForms are the scalar double forms, which are in d registers, and the moves and conversions between the
// classes.
func floatForms() []form {
	three := func(name, mnemonic string, opcode uint32) form {
		return form{name: name, mnemonic: mnemonic, opcode: opcode,
			operands: []operand{vec("dest", rdAt, write), vec("left", rnAt, read), vec("right", rmAt, read)}}
	}
	two := func(name, mnemonic string, opcode uint32) form {
		return twoReg(name, mnemonic, opcode, vec("dest", rdAt, write), vec("src", rnAt, read))
	}
	return []form{
		three("Fadd", "fadd", 0x1E602800),
		three("Fsub", "fsub", 0x1E603800),
		three("Fmul", "fmul", 0x1E600800),
		three("Fdiv", "fdiv", 0x1E601800),
		two("Fsqrt", "fsqrt", 0x1E61C000),
		two("Fneg", "fneg", 0x1E614000),
		two("Fabs", "fabs", 0x1E60C000),
		two("FmovDD", "fmov", 0x1E604000),
		two("Frintn", "frintn", 0x1E644000),
		two("Frintm", "frintm", 0x1E654000),
		two("Frintp", "frintp", 0x1E64C000),
		two("Frintz", "frintz", 0x1E65C000),
		{name: "Fcmp", mnemonic: "fcmp", opcode: 0x1E602000, effects: flags,
			operands: []operand{vec("left", rnAt, read), vec("right", rmAt, read)}},
		{name: "FcmpZero", mnemonic: "fcmp", opcode: 0x1E602008, effects: flags,
			operands: []operand{vec("left", rnAt, read)}},
		{name: "Fcsel", mnemonic: "fcsel", opcode: 0x1E600C00, effects: readsFlag,
			operands: []operand{vec("dest", rdAt, write), vec("left", rnAt, read), vec("right", rmAt, read), cond(condAt)}},
		twoReg("Fcvtzs", "fcvtzs", 0x9E780000, reg("dest", rdAt, write), vec("src", rnAt, read)),
		twoReg("Scvtf", "scvtf", 0x9E620000, vec("dest", rdAt, write), reg("src", rnAt, read)),
		twoReg("Ucvtf", "ucvtf", 0x9E630000, vec("dest", rdAt, write), reg("src", rnAt, read)),
		twoReg("FmovDX", "fmov", 0x9E670000, vec("dest", rdAt, write), reg("src", rnAt, read)),
		twoReg("FmovXD", "fmov", 0x9E660000, reg("dest", rdAt, write), vec("src", rnAt, read)),
		// The exclusive or of the low 64 bits of two vector registers, which clears the rest of the destination.
		{name: "EorVec", mnemonic: "eor", opcode: 0x2E201C00,
			operands: []operand{vec("dest", rdAt, write), vec("left", rnAt, read), vec("right", rmAt, read)}},
	}
}

var forms = buildForms()

func buildForms() []form {
	fs := []form{
		threeReg("AddRegReg", "add", 0x8B000000, noEffects),
		threeReg("SubRegReg", "sub", 0xCB000000, noEffects),
		threeReg("AddsRegReg", "adds", 0xAB000000, flags),
		threeReg("SubsRegReg", "subs", 0xEB000000, flags),
		compareReg("CmpRegReg", "cmp", 0xEB00001F),
		twoReg("NegReg", "neg", 0xCB0003E0, reg("dest", rdAt, write), reg("src", rmAt, read)),
		threeReg("AndRegReg", "and", 0x8A000000, noEffects),
		threeReg("OrrRegReg", "orr", 0xAA000000, noEffects),
		threeReg("EorRegReg", "eor", 0xCA000000, noEffects),
		threeReg("AndsRegReg", "ands", 0xEA000000, flags),
		compareReg("TstRegReg", "tst", 0xEA00001F),
		twoReg("MovRegReg", "mov", 0xAA0003E0, reg("dest", rdAt, write), reg("src", rmAt, read)),
		twoReg("MvnRegReg", "mvn", 0xAA2003E0, reg("dest", rdAt, write), reg("src", rmAt, read)),
		threeReg("MulRegReg", "mul", 0x9B007C00, noEffects),
		threeReg("SmulhRegReg", "smulh", 0x9B407C00, noEffects),
		threeReg("UmulhRegReg", "umulh", 0x9BC07C00, noEffects),
		threeReg("SdivRegReg", "sdiv", 0x9AC00C00, noEffects),
		threeReg("UdivRegReg", "udiv", 0x9AC00800, noEffects),
		threeReg("LslvRegReg", "lslv", 0x9AC02000, noEffects),
		threeReg("LsrvRegReg", "lsrv", 0x9AC02400, noEffects),
		threeReg("AsrvRegReg", "asrv", 0x9AC02800, noEffects),
		threeReg("RorvRegReg", "rorv", 0x9AC02C00, noEffects),
		{name: "MaddRegRegRegReg", mnemonic: "madd", opcode: 0x9B000000, direct: true, operands: []operand{
			reg("dest", rdAt, write), reg("left", rnAt, read), reg("right", rmAt, read), reg("addend", raAt, read)}},
		{name: "MsubRegRegRegReg", mnemonic: "msub", opcode: 0x9B008000, direct: true, operands: []operand{
			reg("dest", rdAt, write), reg("left", rnAt, read), reg("right", rmAt, read), reg("minuend", raAt, read)}},
		// The immediate is the 12 bits of the field, or for High those bits shifted left by 12.
		addImm("AddRegImm", "add", 0x91000000, stackReg("dest", rdAt, write), noEffects),
		addImm("AddRegImmHigh", "add", 0x91400000, stackReg("dest", rdAt, write), noEffects),
		addImm("SubRegImm", "sub", 0xD1000000, stackReg("dest", rdAt, write), noEffects),
		addImm("SubRegImmHigh", "sub", 0xD1400000, stackReg("dest", rdAt, write), noEffects),
		addImm("AddsRegImm", "adds", 0xB1000000, reg("dest", rdAt, write), flags),
		addImm("SubsRegImm", "subs", 0xF1000000, reg("dest", rdAt, write), flags),
		{name: "CmpRegImm", mnemonic: "cmp", opcode: 0xF100001F, effects: flags,
			operands: []operand{stackReg("left", rnAt, read), imm("imm", imm12At, imm12Bits)}},
		{name: "CmnRegImm", mnemonic: "cmn", opcode: 0xB100001F, effects: flags,
			operands: []operand{stackReg("left", rnAt, read), imm("imm", imm12At, imm12Bits)}},
		logical("AndRegMask", "and", 0x92000000, stackReg("dest", rdAt, write), noEffects),
		logical("OrrRegMask", "orr", 0xB2000000, stackReg("dest", rdAt, write), noEffects),
		logical("EorRegMask", "eor", 0xD2000000, stackReg("dest", rdAt, write), noEffects),
		logical("AndsRegMask", "ands", 0xF2000000, reg("dest", rdAt, write), flags),
		{name: "TstRegMask", mnemonic: "tst", opcode: 0xF200001F, effects: flags,
			operands: []operand{reg("left", rnAt, read), mask()}},
		// The orr of a logical immediate with the zero register sets a register to the immediate in one instruction.
		{name: "MovRegMask", mnemonic: "mov", opcode: 0xB20003E0, operands: []operand{stackReg("dest", rdAt, write), mask()}},
		moveWide("MovzRegImm", "movz", 0xD2800000, write),
		moveWide("MovnRegImm", "movn", 0x92800000, write),
		moveWide("MovkRegImm", "movk", 0xF2800000, readWrite),
		// The shifts and extensions by a constant: lsl #s is ubfm #(-s mod 64), #(63-s), lsr #s ubfm #s, #63, asr #s
		// sbfm #s, #63, and a zero or sign extension ubfm or sbfm #0 and the top bit of the narrow value.
		{name: "UbfmRegReg", mnemonic: "ubfm", opcode: 0xD3400000, operands: []operand{
			reg("dest", rdAt, write), reg("src", rnAt, read), imm("immr", immrAt, sixBits), imm("imms", immsAt, sixBits)}},
		{name: "SbfmRegReg", mnemonic: "sbfm", opcode: 0x93400000, operands: []operand{
			reg("dest", rdAt, write), reg("src", rnAt, read), imm("immr", immrAt, sixBits), imm("imms", immsAt, sixBits)}},
		{name: "CselRegReg", mnemonic: "csel", opcode: 0x9A800000, effects: readsFlag, operands: []operand{
			reg("dest", rdAt, write), reg("left", rnAt, read), reg("right", rmAt, read), cond(condAt)}},
		{name: "CsincRegReg", mnemonic: "csinc", opcode: 0x9A800400, effects: readsFlag, operands: []operand{
			reg("dest", rdAt, write), reg("left", rnAt, read), reg("right", rmAt, read), cond(condAt)}},
		// cset is csinc of the zero register with the opposite condition.
		{name: "CsetReg", mnemonic: "cset", opcode: 0x9A9F07E0, effects: readsFlag, operands: []operand{
			reg("dest", rdAt, write), {name: "cond", kind: kindCond, at: condAt, bits: condBits, invert: true}}},
	}
	fs = append(fs, memoryForms()...)
	fs = append(fs, pairForms()...)
	fs = append(fs,
		// The address of a symbol, data or an import is the page adrp gives plus the low bits that the add or the load
		// after it adds, both of which the reference patches.
		form{name: "Adrp", mnemonic: "adrp", opcode: 0x90000000, operands: []operand{reg("dest", rdAt, write), address("pages")}},
		form{name: "Adr", mnemonic: "adr", opcode: 0x10000000, operands: []operand{reg("dest", rdAt, write), address("rel")}},
		form{name: "AddPageOff", mnemonic: "add", opcode: 0x91000000, operands: []operand{
			stackReg("dest", rdAt, write), stackReg("src", rnAt, read), pageOffset(1)}},
		load("Ldr64PageOff", "ldr", 0xF9400000, reg("dest", rdAt, write), base(), pageOffset(8)),
		store("Str64PageOff", "str", 0xF9000000, reg("src", rdAt, read), base(), pageOffset(8)),
		load("LdrDPageOff", "ldr", 0xFD400000, vec("dest", rdAt, write), base(), pageOffset(8)),
		form{name: "B", mnemonic: "b", opcode: 0x14000000, operands: []operand{rel(imm26At, imm26Bits)}},
		form{name: "Bl", mnemonic: "bl", opcode: 0x94000000, effects: writes, call: true,
			operands: []operand{rel(imm26At, imm26Bits)}},
		form{name: "BCond", mnemonic: "b.cond", opcode: 0x54000000, effects: readsFlag,
			operands: []operand{cond(branchCondAt), rel(imm19At, imm19Bits)}},
		form{name: "Cbz", mnemonic: "cbz", opcode: 0xB4000000, operands: []operand{reg("src", rdAt, read), rel(imm19At, imm19Bits)}},
		form{name: "Cbnz", mnemonic: "cbnz", opcode: 0xB5000000, operands: []operand{reg("src", rdAt, read), rel(imm19At, imm19Bits)}},
		form{name: "Br", mnemonic: "br", opcode: 0xD61F0000, operands: []operand{reg("target", rnAt, read)}},
		form{name: "Blr", mnemonic: "blr", opcode: 0xD63F0000, effects: memory, call: true,
			operands: []operand{reg("target", rnAt, read)}},
		form{name: "Ret", mnemonic: "ret", opcode: 0xD65F03C0, ret: true},
	)
	fs = append(fs, floatForms()...)
	fs = append(fs,
		// The ordered and exclusive accesses and the LSE atomics, which the runtime's scheduler is written on (README,
		// "The runtime"): each orders as acquire and release, so that the weakly ordered target keeps what x64's order
		// gives. casal compares x0 with the word and gives the word it found in x0, as x64's cmpxchg does in rax.
		load("Ldar", "ldar", 0xC8DFFC00, reg("dest", rdAt, write), base()),
		store("Stlr", "stlr", 0xC89FFC00, reg("src", rdAt, read), base()),
		load("Ldaxr", "ldaxr", 0xC85FFC00, reg("dest", rdAt, write), base()),
		form{name: "Stlxr", mnemonic: "stlxr", opcode: 0xC800FC00, effects: writes, operands: []operand{
			reg("status", rmAt, write), reg("src", rdAt, read), base()}},
		form{name: "CasalX0", mnemonic: "casal", opcode: 0xC8E0FC00, effects: memory,
			fixedUses: []string{"x0"}, fixedDefs: []string{"x0"},
			operands: []operand{reg("src", rdAt, read), base()}},
		form{name: "Ldaddal", mnemonic: "ldaddal", opcode: 0xF8E00000, effects: memory, operands: []operand{
			reg("dest", rdAt, write), reg("src", rmAt, read), base()}},
		form{name: "Swpal", mnemonic: "swpal", opcode: 0xF8E08000, effects: memory, operands: []operand{
			reg("dest", rdAt, write), reg("src", rmAt, read), base()}},
		form{name: "DmbIsh", mnemonic: "dmb ish", opcode: 0xD5033BBF, effects: memory},
		form{name: "Isb", mnemonic: "isb", opcode: 0xD5033FDF, effects: noEffects},
		form{name: "Yield", mnemonic: "yield", opcode: 0xD503203F, effects: noEffects},
		form{name: "Nop", mnemonic: "nop", opcode: 0xD503201F, effects: noEffects},
		form{name: "Brk", mnemonic: "brk", opcode: 0xD4200000, operands: []operand{imm("imm", imm16At, imm16Bits)}},
		// A system call is a call of the system call convention, which says what it reads and clobbers: svc #0, Linux's,
		// whose immediate the kernel ignores. A machine instruction of a call holds the registers of its arguments where an
		// immediate would be, so the form has none.
		form{name: "Svc", mnemonic: "svc #0", opcode: 0xD4000001, effects: memory, call: true},
		// The thread's registers: a read of one reads what the thread it runs on keeps, which a green thread that moves
		// between threads may not find again, so it is no value to make twice.
		form{name: "MrsTpidr", mnemonic: "mrs tpidr_el0", opcode: 0xD53BD040, effects: reads, operands: []operand{reg("dest", rdAt, write)}},
		form{name: "MrsTpidrro", mnemonic: "mrs tpidrro_el0", opcode: 0xD53BD060, effects: reads, operands: []operand{reg("dest", rdAt, write)}},
		form{name: "MsrTpidr", mnemonic: "msr tpidr_el0", opcode: 0xD51BD040, effects: writes, operands: []operand{reg("src", rdAt, read)}},
		form{name: "MrsNzcv", mnemonic: "mrs nzcv", opcode: 0xD53B4200, effects: readsFlag, operands: []operand{reg("dest", rdAt, write)}},
		form{name: "MsrNzcv", mnemonic: "msr nzcv", opcode: 0xD51B4200, effects: flags, operands: []operand{reg("src", rdAt, read)}},
	)
	return fs
}
