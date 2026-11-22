package x64table

import (
	"fmt"
	"strings"
)

// emitMaxon writes the whole of x64_gen.maxon: the types and constants, the helpers every encoder is written with, one
// encoder per form, the choices and sequences, the effects of the forms, and what the register allocator and the
// encoder of the machine IR ask of them.
func emitMaxon() string {
	var b strings.Builder
	b.WriteString(header)
	emitTypes(&b)
	emitConstants(&b)
	b.WriteString(helpers)
	for _, f := range forms {
		emitForm(&b, f)
	}
	for _, c := range choices {
		emitChoice(&b, c)
	}
	for _, s := range sequences {
		emitSequence(&b, s)
	}
	emitFormEnum(&b)
	b.WriteString(formTableHelper)
	for _, e := range effectList {
		emitEffect(&b, e.name, e.of)
	}
	emitAlloc(&b)
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func emitTypes(b *strings.Builder) {
	fmt.Fprintf(b, "export typealias Imm8 = int(%d to %d)\n\n", imm8Min, imm8Max)
	fmt.Fprintf(b, "export typealias Imm32 = int(%d to %d)\n\n", imm32Min, imm32Max)
	fmt.Fprintf(b, "export typealias Imm32U = int(0 to %d)\n\n", imm32UMax)
	fmt.Fprintf(b, "typealias Low3 = int(0 to %d)\n\n", lowRegisters-1)
	fmt.Fprintf(b, "typealias Nibble = int(0 to %d)\n\n", conditionCodes-1)
	fmt.Fprintf(b, "typealias ScaleBits = int(0 to %d)\n\n", scaleBitsOf(wordScale))
	b.WriteString("export enum X64Register\n")
	for _, r := range registers {
		fmt.Fprintf(b, "\t%s\n", r)
	}
	b.WriteString("end 'X64Register'\n\n")
	b.WriteString("export enum X64CondCode\n")
	for _, c := range conditions {
		fmt.Fprintf(b, "\t%s = 0x%X\n", c.name, c.code)
	}
	b.WriteString("end 'X64CondCode'\n\n")
}

func emitConstants(b *strings.Builder) {
	for _, c := range constants {
		if c.value >= 0x10 && c.value <= 0xFF {
			fmt.Fprintf(b, "let %s = 0x%02X\n\n", c.name, c.value)
		} else {
			fmt.Fprintf(b, "let %s = %d\n\n", c.name, c.value)
		}
	}
}

// helpers are the functions every encoder is written with. Where a value is a register's number, the casts in them are
// to ranges that the arithmetic before them has proved, so none can fail.
const helpers = `function regLow3(r X64Register) returns Low3
	return (r.rawValue mod lowRegisters) as Low3
end 'regLow3'

function isExtended(r X64Register) returns bool
	return r.rawValue >= lowRegisters
end 'isExtended'

// Registers 4 to 7 are spl to dil only if a REX prefix is present, so a byte register needs one even with no bit set.
function needsByteRex(r X64Register) returns bool
	return r.rawValue >= byteRegisters
end 'needsByteRex'

function conditionBits(cond X64CondCode) returns Nibble
	return (cond.rawValue mod conditionCodes) as Nibble
end 'conditionBits'

function emitRex(mutates code ByteArray, wide bool, regField X64Register, indexField X64Register, rmField X64Register, force bool)
	var rex = rexBase as Byte
	if wide 'wide'
		rex = rex or rexWide
	end 'wide'
	if isExtended(regField) 'reg'
		rex = rex or rexReg
	end 'reg'
	if isExtended(indexField) 'index'
		rex = rex or rexIndex
	end 'index'
	if isExtended(rmField) 'rm'
		rex = rex or rexRm
	end 'rm'
	if force or rex != rexBase 'needed'
		code.push(rex)
	end 'needed'
end 'emitRex'

function writeModRmReg(mutates code ByteArray, regBits Low3, rm X64Register)
	code.push(modRegister + regBits * regFieldScale + regLow3(rm))
end 'writeModRmReg'

function writeModRmRip(mutates code ByteArray, regBits Low3)
	code.push(regBits * regFieldScale + ripRelative)
end 'writeModRmRip'

// A base whose low bits are those of rsp is written with a SIB byte that names no index.
function writeSib(mutates code ByteArray, rm Low3)
	if rm == rspLowBits 'sib'
		code.push(sibRsp)
	end 'sib'
end 'writeSib'

// The displacement is the shortest of none, a byte and a doubleword, and a base whose low bits are those of rbp has
// none shorter than a byte, since mod 00 with them means a RIP-relative address.
function writeMem(mutates code ByteArray, regBits Low3, base X64Register, disp Imm32)
	let rm = regLow3(base)
	let regField = regBits * regFieldScale
	if disp == 0 and rm != rbpLowBits 'none'
		code.push(regField + rm)
		writeSib(code, rm: rm)
	end 'none' else if disp >= imm8Min and disp <= imm8Max 'short'
		code.push(modDisp8 + regField + rm)
		writeSib(code, rm: rm)
		writeImm8(code, value: disp as Imm8)
	end 'short' else 'long'
		code.push(modDisp32 + regField + rm)
		writeSib(code, rm: rm)
		writeImm32(code, value: disp)
	end 'long'
end 'writeMem'

// An indexed operand is a SIB byte of its scale, index and base after the ModRM byte, and its displacement is the
// shortest writeMem would give the base. An index with the number of rsp means none, and rsp holds the stack, so it is
// never an index.
function writeMemIndex(mutates code ByteArray, regBits Low3, base X64Register, index X64Register, scaleBits ScaleBits, disp Imm32)
	if index == X64Register.rsp 'stack'
		panic("backend: rsp is the index of a memory operand")
	end 'stack'
	let rm = regLow3(base)
	let regField = regBits * regFieldScale
	let sib = scaleBits * sibScaleField + regLow3(index) * sibIndexField + rm
	if disp == 0 and rm != rbpLowBits 'none'
		code.push(regField + sibFollows)
		code.push(sib)
	end 'none' else if disp >= imm8Min and disp <= imm8Max 'short'
		code.push(modDisp8 + regField + sibFollows)
		code.push(sib)
		writeImm8(code, value: disp as Imm8)
	end 'short' else 'long'
		code.push(modDisp32 + regField + sibFollows)
		code.push(sib)
		writeImm32(code, value: disp)
	end 'long'
end 'writeMemIndex'

// Writes the low bytes of an unsigned word, little endian. Every byte is inverted with invert, which is how the
// two's complement of a negative quadword is written without a word that holds it.
function writeWord(mutates code ByteArray, word Count, bytes Count, invert Byte)
	var rest = word
	for i in 0 upto bytes 'bytes'
		code.push(((rest mod byteValues) as Byte) xor invert)
		rest = rest / byteValues
	end 'bytes'
end 'writeWord'

// The unsigned word with the bits of a signed doubleword.
function word32(value Imm32) returns Count
	return ((value + imm32Values) mod imm32Values) as Count
end 'word32'

function writeImm8(mutates code ByteArray, value Imm8)
	code.push(((value + byteValues) mod byteValues) as Byte)
end 'writeImm8'

function writeImm32(mutates code ByteArray, value Imm32)
	writeWord(code, word: word32(value), bytes: imm32Bytes, invert: 0)
end 'writeImm32'

function writeImm32U(mutates code ByteArray, value Imm32U)
	writeWord(code, word: value as Count, bytes: imm32Bytes, invert: 0)
end 'writeImm32U'

function writeImm64(mutates code ByteArray, value FullRangeInt)
	if value < 0 'negative'
		writeWord(code, word: (-1 - value) as Count, bytes: imm64Bytes, invert: 0xFF)
	end 'negative' else 'positive'
		writeWord(code, word: value as Count, bytes: imm64Bytes, invert: 0)
	end 'positive'
end 'writeImm64'

// The offset of the 32-bit field that the last encoder wrote at the end of its instruction: a displacement of a
// RIP-relative operand, a relative target or an immediate. A label, a call or a data reference writes 0 there and
// patches it when the target is known.
export function lastField32(code ByteArray) returns Count
	return try code.count() - rel32Bytes otherwise panic("no instruction is encoded")
end 'lastField32'

// The relative target of a branch whose 32-bit field is at offset at, which counts from the end of the field.
export function rel32To(at Count, target Count) returns Imm32
	let after = try at + rel32Bytes otherwise panic("a field is past the end of memory")
	if target >= after 'forward'
		return try target - after otherwise panic("a branch reaches past 2 GiB")
	end 'forward'
	let back = try (after - target) as FullRangeInt otherwise panic("a branch reaches past 2 GiB")
	return try 0 - back otherwise panic("a branch reaches past 2 GiB")
end 'rel32To'

export function patch32(mutates code ByteArray, at Count, value Imm32)
	var rest = word32(value)
	for i in 0 upto imm32Bytes 'bytes'
		let index = try at + i otherwise panic("a field is past the end of memory")
		code[index] = (rest mod byteValues) as Byte
		rest = rest / byteValues
	end 'bytes'
end 'patch32'

// Makes the branch or call whose 32-bit field is at offset at go to target.
export function patchRel32(mutates code ByteArray, at Count, target Count)
	patch32(code, at: at, value: rel32To(at, target: target))
end 'patchRel32'

`

// operandText is how a comment shows an operand.
func operandText(o operand) string {
	switch o.kind {
	case kindReg, kindCond:
		return o.name
	case kindMem:
		return "[base + disp]"
	case kindMemIndex:
		return fmt.Sprintf("[base + index*%d + disp]", o.scale)
	case kindRip:
		return "[rip + disp]"
	case kindImm8, kindImm32, kindImm32U, kindImm64, kindRel32:
		return o.name
	default:
		panic(fmt.Sprintf("operand kind %d has no text", o.kind))
	}
}

// encodingText is the bytes of a form as the manuals write them: prefix, REX.W, opcode and ModRM.
func encodingText(f form) string {
	var parts []string
	for _, p := range f.prefix {
		parts = append(parts, fmt.Sprintf("%02X", p))
	}
	if f.rexW {
		parts = append(parts, "REX.W")
	}
	for i, op := range f.opcode {
		text := fmt.Sprintf("%02X", op)
		if i == len(f.opcode)-1 {
			if _, ok := f.in(fieldOpcode); ok {
				text += "+r"
			} else if _, ok := f.withKind(kindCond); ok {
				text += "+cc"
			}
		}
		parts = append(parts, text)
	}
	switch f.modrm {
	case modOperands:
		parts = append(parts, "/r")
	case modDigit:
		parts = append(parts, fmt.Sprintf("/%d", f.digit))
	}
	return strings.Join(parts, " ")
}

func paramList(ps []param) string {
	var parts []string
	for _, p := range ps {
		parts = append(parts, p.name+" "+p.ty)
	}
	return strings.Join(parts, ", ")
}

const codeParam = "mutates code ByteArray"

func signature(b *strings.Builder, name string, ps []param) {
	list := codeParam
	if len(ps) > 0 {
		list += ", " + paramList(ps)
	}
	fmt.Fprintf(b, "export function encode%s(%s)\n", name, list)
}

func endFunction(b *strings.Builder, name string) {
	fmt.Fprintf(b, "end 'encode%s'\n\n", name)
}

// call is a call of an encoder with the given arguments, labelled by the parameter names.
func call(name string, ps []param, args []string) string {
	var text strings.Builder
	text.WriteString("encode" + name + "(code")
	for i, p := range ps {
		text.WriteString(", " + p.name + ": " + args[i])
	}
	return text.String() + ")"
}

func names(ps []param) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.name
	}
	return out
}

const rax = "X64Register.rax"

func emitForm(b *strings.Builder, f form) {
	var texts []string
	for _, o := range f.operands {
		texts = append(texts, operandText(o))
	}
	fmt.Fprintf(b, "// %s %s: %s.", f.mnemonic, strings.Join(texts, ", "), encodingText(f))
	b.WriteString("\n")
	signature(b, f.name, f.params())
	for _, p := range f.prefix {
		fmt.Fprintf(b, "\tcode.push(0x%02X)\n", p)
	}
	emitRexCall(b, f)
	emitOpcode(b, f)
	emitModRm(b, f)
	for _, o := range f.operands {
		switch {
		case isImmediate(o.kind):
			fmt.Fprintf(b, "\t%s(code, value: %s)\n", immediateOf(o.kind).writer, o.name)
		case o.kind == kindRel32:
			fmt.Fprintf(b, "\twriteImm32(code, value: %s)\n", o.name)
		}
	}
	endFunction(b, f.name)
}

// emitRexCall writes the REX prefix of a form, if any operand or the width can set a bit of it.
func emitRexCall(b *strings.Builder, f form) {
	regField, indexField, rmField := rax, rax, rax
	if o, ok := f.in(fieldReg); ok {
		regField = o.name
	}
	if o, ok := f.in(fieldRM); ok && o.kind != kindRip {
		rmField = o.name
		switch o.kind {
		case kindMem:
			rmField = "base"
		case kindMemIndex:
			rmField, indexField = "base", "index"
		}
	}
	if o, ok := f.in(fieldOpcode); ok {
		rmField = o.name
	}
	var forced []string
	for _, o := range f.operands {
		if o.byteReg {
			forced = append(forced, "needsByteRex("+o.name+")")
		}
	}
	force := "false"
	if len(forced) > 0 {
		force = strings.Join(forced, " or ")
	}
	if !f.rexW && regField == rax && indexField == rax && rmField == rax {
		return
	}
	fmt.Fprintf(b, "\temitRex(code, wide: %t, regField: %s, indexField: %s, rmField: %s, force: %s)\n", f.rexW, regField, indexField, rmField, force)
}

func emitOpcode(b *strings.Builder, f form) {
	last := len(f.opcode) - 1
	for i, op := range f.opcode {
		if i < last {
			fmt.Fprintf(b, "\tcode.push(0x%02X)\n", op)
			continue
		}
		switch {
		case hasField(f, fieldOpcode):
			o, _ := f.in(fieldOpcode)
			fmt.Fprintf(b, "\tcode.push(0x%02X + regLow3(%s))\n", op, o.name)
		case hasKind(f, kindCond):
			o, _ := f.withKind(kindCond)
			fmt.Fprintf(b, "\tcode.push(0x%02X + conditionBits(%s))\n", op, o.name)
		default:
			fmt.Fprintf(b, "\tcode.push(0x%02X)\n", op)
		}
	}
}

func hasField(f form, fl field) bool {
	_, ok := f.in(fl)
	return ok
}

func hasKind(f form, k kind) bool {
	_, ok := f.withKind(k)
	return ok
}

func emitModRm(b *strings.Builder, f form) {
	if f.modrm == modNone {
		return
	}
	regBits := fmt.Sprint(f.digit)
	if o, ok := f.in(fieldReg); ok {
		regBits = "regLow3(" + o.name + ")"
	}
	rm, _ := f.in(fieldRM)
	switch rm.kind {
	case kindReg:
		fmt.Fprintf(b, "\twriteModRmReg(code, regBits: %s, rm: %s)\n", regBits, rm.name)
	case kindMem:
		fmt.Fprintf(b, "\twriteMem(code, regBits: %s, base: base, disp: disp)\n", regBits)
	case kindMemIndex:
		fmt.Fprintf(b, "\twriteMemIndex(code, regBits: %s, base: base, index: index, scaleBits: %d, disp: disp)\n", regBits, scaleBitsOf(rm.scale))
	case kindRip:
		fmt.Fprintf(b, "\twriteModRmRip(code, regBits: %s)\n", regBits)
		b.WriteString("\twriteImm32(code, value: disp)\n")
	default:
		panic(fmt.Sprintf("form %s has an r/m operand of kind %d", f.name, rm.kind))
	}
}

func emitChoice(b *strings.Builder, c choice) {
	ps := c.params()
	fmt.Fprintf(b, "// %s is the first of %s whose immediate holds the value.\n", "encode"+c.name, strings.Join(c.alternatives, ", "))
	signature(b, c.name, ps)
	wide := immediateOf(c.immediateKind()).ty
	last := len(c.alternatives) - 1
	for i, name := range c.alternatives {
		f := formByName(name)
		imm := immediateOf(immediateKindOf(f))
		label := lowerFirst(name)
		args := names(ps)
		for j, p := range ps {
			if p.name == "imm" && imm.ty != wide {
				args[j] = "imm as " + imm.ty
			}
		}
		callText := call(name, f.params(), args)
		switch {
		case i == 0:
			fmt.Fprintf(b, "\tif %s '%s'\n\t\t%s\n\tend '%s'", guard(imm), label, callText, label)
		case i == last:
			fmt.Fprintf(b, " else '%s'\n\t\t%s\n\tend '%s'\n", label, callText, label)
		default:
			fmt.Fprintf(b, " else if %s '%s'\n\t\t%s\n\tend '%s'", guard(imm), label, callText, label)
		}
	}
	endFunction(b, c.name)
}

// guard is the test that the immediate holds the range of an alternative.
func guard(imm immediate) string {
	return fmt.Sprintf("imm >= %s and imm <= %s", imm.lo, imm.hi)
}

func emitSequence(b *strings.Builder, s sequence) {
	ps := paramsOf(s.operands)
	var forms []string
	for _, st := range s.steps {
		forms = append(forms, st.form)
	}
	fmt.Fprintf(b, "// encode%s is %s in a row.\n", s.name, strings.Join(forms, ", "))
	signature(b, s.name, ps)
	for _, st := range s.steps {
		f := formByName(st.form)
		args := make([]string, len(st.args))
		for i, a := range st.args {
			args[i] = a
			if registerIndex(a) >= 0 {
				args[i] = "X64Register." + a
			}
		}
		fmt.Fprintf(b, "\t%s\n", call(st.form, f.params(), args))
	}
	endFunction(b, s.name)
}

// emitFormEnum writes one case per form, which the effects are matches over, so a form added to the table fails the
// build of every match that does not name it.
func emitFormEnum(b *strings.Builder) {
	b.WriteString("// X64Form is every instruction form of the table, for the instruction selection to ask about.\n")
	b.WriteString("export enum X64Form\n")
	for _, f := range forms {
		fmt.Fprintf(b, "\t%s\n", lowerFirst(f.name))
	}
	b.WriteString("end 'X64Form'\n\n")
}

func emitEffect(b *strings.Builder, name string, of func(form) bool) {
	emitFormTable(b, name, "bool", func(f form) int {
		if of(f) {
			return 1
		}
		return 0
	})
}

// formTableHelper reads the tables that emitFormTable writes: a string of one digit per form, in the order of X64Form,
// so that what an allocator or an encoder asks of a form for every instruction is one read of data rather than a
// match over every form.
const formTableHelper = `// formTableZero is the byte of the digit 0, which a form table's digits count from.
let formTableZero = 48

// formTableDigit is the digit a form table holds for a form.
function formTableDigit(table String, form X64Form) returns Count
	return try (table.byteAt(form.rawValue) as Count) - formTableZero otherwise panic("backend: a form table holds a byte that is no digit")
end 'formTableDigit'

`

// emitFormTable writes a function of a form that gives a digit, or a bool that is the digit 1, from a form table.
func emitFormTable(b *strings.Builder, name, ty string, of func(form) int) {
	var digits strings.Builder
	for _, f := range forms {
		v := of(f)
		if v < 0 || v > 9 {
			panic(fmt.Sprintf("%s of %s is %d, which no digit is", name, f.name, v))
		}
		digits.WriteByte(byte('0' + v))
	}
	fmt.Fprintf(b, "export function %s(form X64Form) returns %s\n", name, ty)
	test := ""
	if ty == "bool" {
		test = " == 1"
	}
	fmt.Fprintf(b, "\treturn formTableDigit(\"%s\", form: form)%s\n", digits.String(), test)
	fmt.Fprintf(b, "end '%s'\n\n", name)
}
