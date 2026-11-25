package arm64table

import (
	"fmt"
	"slices"
	"strings"
)

// emitMaxon writes the whole of arm64_gen.maxon: the types and constants, the helpers every encoder is written with,
// one encoder per form, the predicates of the offsets, the form enum and its FormIDs, and what the encoder of the
// machine IR asks of the forms. What the allocator asks of them is in the target-neutral tables (internal/targetgen's
// machine_gen.maxon).
func emitMaxon() string {
	var b strings.Builder
	b.WriteString(header)
	emitTypes(&b)
	emitConstants(&b)
	emitConditions(&b)
	b.WriteString(helpers)
	for _, f := range forms {
		emitForm(&b, f)
	}
	b.WriteString(ladder)
	for _, f := range forms {
		emitHolds(&b, f)
	}
	emitFormEnum(&b)
	emitAlloc(&b)
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func emitTypes(b *strings.Builder) {
	var widths []int
	for _, f := range forms {
		for _, o := range f.operands {
			if o.kind == kindImm && !slices.Contains(widths, o.bits) {
				widths = append(widths, o.bits)
			}
		}
	}
	slices.Sort(widths)
	for _, w := range widths {
		fmt.Fprintf(b, "export typealias %s = int(0 to %d)\n\n", immType(w), uint64(1)<<w-1)
	}
	b.WriteString("// Arm64Bits is an instruction word as it is put together, and a word as a logical immediate reads it.\n")
	b.WriteString("typealias Arm64Bits = bits(64)\n\n")
	b.WriteString("export enum Arm64Register\n")
	for _, r := range registers {
		fmt.Fprintf(b, "\t%s\n", r)
	}
	b.WriteString("end 'Arm64Register'\n\n")
}

// constant is a name and the value that the generated Maxon gives it, as a bit pattern where pattern says so.
type constant struct {
	name    string
	value   uint64
	pattern bool
}

var constants = []constant{
	{"arm64WordBytes", wordBytes, false},
	{"arm64ByteBits", 8, false},
	{"arm64ByteMask", 0xFF, true},
	{"arm64Halfwords", 4, false},
	{"arm64HalfwordBits", imm16Bits, false},
	{"arm64HalfwordMask", 0xFFFF, true},
	{"arm64WordBits", 64, false},
	{"arm64SignBit", 1 << 63, true},
	{"arm64AllOnes", 1<<64 - 1, true},
	{"arm64LowBit", 1, true},
	{"arm64SmallestElement", 2, false},
	{"arm64LogicalFieldMask", 1<<sixBits - 1, true},
	{"arm64LogicalNAt", 2 * sixBits, false},
	{"arm64LogicalImmrAt", sixBits, false},
	{"arm64ImmsBits", sixBits, false},
	{"arm64OppositeCondition", 1, true},
	{"arm64ImmloBits", immloBits, false},
	{"arm64ImmloMask", 1<<immloBits - 1, true},
	{"arm64ImmloAt", immloAt, false},
	{"arm64ImmhiAt", immhiAt, false},
	{"arm64AddressBits", address21, false},
	// The fields that a label or the link patches: a branch's distance in words, adrp's pages and the low bits of an address
	// after it, and the immediate of add and sub, which a wide one is split into.
	{"arm64Imm26At", imm26At, false},
	{"arm64Imm26Bits", imm26Bits, false},
	{"arm64Imm19At", imm19At, false},
	{"arm64Imm19Bits", imm19Bits, false},
	{"arm64Imm12At", imm12At, false},
	{"arm64Imm12Bits", imm12Bits, false},
	{"arm64PageOffsetBits", pageOffsetBits, false},
	{"arm64AddressMask", uint64(bitsAt(immloAt, immloBits) | bitsAt(immhiAt, address21-immloBits)), true},
}

func emitConstants(b *strings.Builder) {
	for _, c := range constants {
		if c.pattern {
			fmt.Fprintf(b, "let %s = 0x%X as Arm64Bits\n\n", c.name, c.value)
		} else {
			fmt.Fprintf(b, "let %s = %d\n\n", c.name, c.value)
		}
	}
	lo, hi := unitRange(address("rel"))
	fmt.Fprintf(b, "let arm64AddressLow = %d\n\nlet arm64AddressHigh = %d\n\n", lo, hi)
}

// emitConditions writes the arm64 code of each condition of the machine IR.
func emitConditions(b *strings.Builder) {
	b.WriteString("// arm64ConditionCode is the arm64 condition code that tests a condition of the machine IR.\n")
	b.WriteString("function arm64ConditionCode(cond MCond) returns Arm64Bits\n\treturn match cond 'cond'\n")
	for _, c := range conditions {
		fmt.Fprintf(b, "\t\t%s gives 0x%X\n", c.name, c.code)
	}
	b.WriteString("\tend 'cond'\nend 'arm64ConditionCode'\n\n")
}

// helpers are the functions every encoder is written with, and the encodings of the immediates that are not a plain
// field, transcribed from maxon's Arm64Backend.maxon.
const helpers = `// arm64Emit appends the word of an instruction, little endian.
function arm64Emit(mutates code ByteArray, word Arm64Bits)
	var rest = word
	for i in 0 upto arm64WordBytes 'bytes'
		code.push((rest and arm64ByteMask) as Byte)
		rest = rest shr arm64ByteBits
	end 'bytes'
end 'arm64Emit'

// arm64RegisterField is a register's encoding in the field at a bit.
function arm64RegisterField(r Arm64Register, at Count) returns Arm64Bits
	return (r.rawValue as Arm64Bits) shl at
end 'arm64RegisterField'

// arm64HoldsUnits is whether a value is a whole number of units of scale, from low to high units, which is what the
// field of an offset or a distance holds.
export function arm64HoldsUnits(value FullRangeInt, scale FullRangeInt, low FullRangeInt, high FullRangeInt) returns bool
	if (try value mod scale otherwise panic("backend: a field of no unit")) != 0 'part'
		return false
	end 'part'
	let units = try value / scale otherwise panic("backend: a field of no unit")
	return units >= low and units <= high
end 'arm64HoldsUnits'

// arm64UnitsField is the field at a bit, of bits bits, that holds a value in units of scale, in two's complement.
function arm64UnitsField(value FullRangeInt, scale FullRangeInt, low FullRangeInt, high FullRangeInt, bits Count, at Count) returns Arm64Bits
	if not arm64HoldsUnits(value, scale: scale, low: low, high: high) 'out'
		panic("backend: an arm64 offset or distance that its field does not hold")
	end 'out'
	let units = try value / scale otherwise panic("backend: a field of no unit")
	return ((units as Arm64Bits) and (((1 as Arm64Bits) shl bits) - 1)) shl at
end 'arm64UnitsField'

// arm64AddressField is the field of adr's distance in bytes or adrp's in pages: 21 bits, of which the low two are
// immlo and the rest immhi.
function arm64AddressField(distance Imm32) returns Arm64Bits
	let field = arm64UnitsField(distance as FullRangeInt, scale: 1, low: arm64AddressLow, high: arm64AddressHigh, bits: arm64AddressBits, at: 0)
	return ((field and arm64ImmloMask) shl arm64ImmloAt) or ((field shr arm64ImmloBits) shl arm64ImmhiAt)
end 'arm64AddressField'

// Arm64LogicalImmediate is what a word is as the immediate of and, orr, eor and ands: its N:immr:imms field, or none
// for a word that is not a rotated run of ones repeated across the register, which 0 and all ones are not.
union Arm64LogicalImmediate
	encodable(field Arm64Bits)
	notEncodable
end 'Arm64LogicalImmediate'

// arm64TrailingOnes is how many of the low bits of a word are set before the first clear one.
function arm64TrailingOnes(value Arm64Bits) returns Count
	for i in 0 upto arm64WordBits 'bits'
		if ((value shr i) and arm64LowBit) == 0 'clear'
			return i
		end 'clear'
	end 'bits'
	return arm64WordBits
end 'arm64TrailingOnes'

// arm64TrailingZeros is how many of the low bits of a word are clear before the first set one.
function arm64TrailingZeros(value Arm64Bits) returns Count
	for i in 0 upto arm64WordBits 'bits'
		if ((value shr i) and arm64LowBit) != 0 'set'
			return i
		end 'set'
	end 'bits'
	return arm64WordBits
end 'arm64TrailingZeros'

// arm64LeadingOnes is how many of the high bits of a word are set before the first clear one.
function arm64LeadingOnes(value Arm64Bits) returns Count
	for i in 0 upto arm64WordBits 'bits'
		if ((value shl i) and arm64SignBit) == 0 'clear'
			return i
		end 'clear'
	end 'bits'
	return arm64WordBits
end 'arm64LeadingOnes'

// arm64IsShiftedMask is whether the set bits of a word are one run that does not wrap: the bits below the run filled
// in make a run from bit 0.
function arm64IsShiftedMask(value Arm64Bits) returns bool
	if value == 0 'zero'
		return false
	end 'zero'
	let filled = (value - 1) or value
	return (filled and (filled + 1)) == 0
end 'arm64IsShiftedMask'

// arm64LogicalImmediate is the logical immediate of a word: the size of the element the word repeats, a power of two
// from 2 to 64, and the element as a rotation of a run of ones from bit 0, as LLVM's processLogicalImmediate finds it.
// imms is the run's length less 1 under a prefix that says the element's size, and N is set for an element of 64 bits.
function arm64LogicalImmediate(value FullRangeInt) returns Arm64LogicalImmediate
	let word = value as Arm64Bits
	if word == 0 or word == arm64AllOnes 'degenerate'
		return Arm64LogicalImmediate.notEncodable
	end 'degenerate'
	var size = arm64WordBits as Count
	while size > arm64SmallestElement 'shrink'
		let half = size / 2
		let halfMask = ((1 as Arm64Bits) shl half) - 1
		if (word and halfMask) != ((word shr half) and halfMask) 'differ'
			break
		end 'differ'
		size = half
	end 'shrink'
	var elementMask = arm64AllOnes
	if size < arm64WordBits 'narrow'
		elementMask = ((1 as Arm64Bits) shl size) - 1
	end 'narrow'
	var element = word and elementMask
	var rotation = 0 as Count
	var run = 0 as Count
	if arm64IsShiftedMask(element) 'direct'
		rotation = arm64TrailingZeros(element)
		run = arm64TrailingOnes(element shr rotation)
	end 'direct' else 'wrapped'
		element = element or (not elementMask)
		if not arm64IsShiftedMask(not element) 'none'
			return Arm64LogicalImmediate.notEncodable
		end 'none'
		let leading = arm64LeadingOnes(element)
		rotation = try arm64WordBits - leading otherwise panic("backend: a run longer than a word")
		run = try leading + arm64TrailingOnes(element) - (arm64WordBits - size) otherwise panic("backend: a run longer than a word")
	end 'wrapped'
	let sizeMask = (size as Arm64Bits) - 1
	let immr = ((size as Arm64Bits) - (rotation as Arm64Bits)) and sizeMask
	let prefix = (not sizeMask) shl 1
	let imms = (prefix or ((run as Arm64Bits) - 1)) and arm64LogicalFieldMask
	let n = ((prefix shr arm64ImmsBits) and arm64LowBit) xor arm64LowBit
	return Arm64LogicalImmediate.encodable((n shl arm64LogicalNAt) or (immr shl arm64LogicalImmrAt) or imms)
end 'arm64LogicalImmediate'

// arm64IsLogicalImmediate is whether a word is a logical immediate, which instruction selection asks before it picks
// a form of one.
export function arm64IsLogicalImmediate(value FullRangeInt) returns bool
	return match arm64LogicalImmediate(value) 'imm'
		encodable(_) gives true
		notEncodable gives false
	end 'imm'
end 'arm64IsLogicalImmediate'

// arm64LogicalField is the field at a bit of a logical immediate, which instruction selection has made sure the word
// is.
function arm64LogicalField(value FullRangeInt, at Count) returns Arm64Bits
	return match arm64LogicalImmediate(value) 'imm'
		encodable(field) gives field shl at
		notEncodable gives panic("backend: a word that is no logical immediate in a form of one")
	end 'imm'
end 'arm64LogicalField'

`

// ladder is the encoder that sets a register to any word, after the forms it writes, and the steps of it that
// instruction selection writes as machine instructions of those forms.
const ladder = `// The ladder sets a register to a word in the fewest moves of a halfword: a movz of its first halfword that is not 0
// and a movk of each later one that is not, or, where more of its halfwords are all ones than are 0, a movn of the first
// that is not all ones and a movk of each later one that is not. A word of halfwords all 0 or all ones is one movz or
// movn of 0.

// arm64LadderSteps is how many halfwords a word has, each of which is a step of the ladder.
export let arm64LadderSteps = arm64Halfwords

// arm64LadderInverted is whether the ladder of a word starts with a movn.
export function arm64LadderInverted(value FullRangeInt) returns bool
	let word = value as Arm64Bits
	var zeros = 0 as Count
	var ones = 0 as Count
	for hw in 0 upto arm64Halfwords 'count'
		let chunk = (word shr (hw * arm64HalfwordBits)) and arm64HalfwordMask
		if chunk == 0 'zero'
			zeros = zeros + 1
		end 'zero' else if chunk == arm64HalfwordMask 'ones'
			ones = ones + 1
		end 'ones'
	end 'count'
	return ones > zeros
end 'arm64LadderInverted'

// arm64LadderHalfword is the halfword hw of a word, from the lowest.
export function arm64LadderHalfword(value FullRangeInt, hw Count) returns Arm64Imm16
	let at = try hw * arm64HalfwordBits otherwise panic("backend: a halfword past a word")
	let chunk = ((value as Arm64Bits) shr at) and arm64HalfwordMask
	return chunk as Arm64Imm16
end 'arm64LadderHalfword'

// arm64LadderSkips is whether the ladder of a word writes nothing for its halfword hw: one that the first move leaves as
// the word has it, 0 after a movz and all ones after a movn.
export function arm64LadderSkips(value FullRangeInt, hw Count) returns bool
	let chunk = arm64LadderHalfword(value, hw: hw) as Arm64Bits
	if arm64LadderInverted(value) 'inverted'
		return chunk == arm64HalfwordMask
	end 'inverted'
	return chunk == 0
end 'arm64LadderSkips'

// arm64LadderFirst is the halfword of the first move of the ladder at halfword hw: the halfword itself for a movz, and
// its complement for a movn.
export function arm64LadderFirst(value FullRangeInt, hw Count) returns Arm64Imm16
	let chunk = arm64LadderHalfword(value, hw: hw)
	if arm64LadderInverted(value) 'inverted'
		return ((chunk as Arm64Bits) xor arm64HalfwordMask) as Arm64Imm16
	end 'inverted'
	return chunk
end 'arm64LadderFirst'

// arm64LadderShift is the shift field of the move of halfword hw.
export function arm64LadderShift(hw Count) returns Arm64Imm2
	return try hw as Arm64Imm2 otherwise panic("backend: a halfword past a word")
end 'arm64LadderShift'

// encodeArm64MovImm writes the ladder of a word into a register.
export function encodeArm64MovImm(mutates code ByteArray, dest Arm64Register, value FullRangeInt)
	let inverted = arm64LadderInverted(value)
	var first = true
	for hw in 0 upto arm64Halfwords 'halfwords'
		if not arm64LadderSkips(value, hw: hw) 'live'
			if not first 'later'
				encodeArm64MovkRegImm(code, dest: dest, imm: arm64LadderHalfword(value, hw: hw), shift: arm64LadderShift(hw))
			end 'later' else if inverted 'movn'
				encodeArm64MovnRegImm(code, dest: dest, imm: arm64LadderFirst(value, hw: hw), shift: arm64LadderShift(hw))
			end 'movn' else 'movz'
				encodeArm64MovzRegImm(code, dest: dest, imm: arm64LadderFirst(value, hw: hw), shift: arm64LadderShift(hw))
			end 'movz'
			first = false
		end 'live'
	end 'halfwords'
	if first 'uniform'
		if inverted 'allOnes'
			encodeArm64MovnRegImm(code, dest: dest, imm: 0, shift: 0)
		end 'allOnes' else 'allZero'
			encodeArm64MovzRegImm(code, dest: dest, imm: 0, shift: 0)
		end 'allZero'
	end 'uniform'
end 'encodeArm64MovImm'

`

// operandText is how a comment shows an operand.
func operandText(o operand) string {
	switch o.kind {
	case kindReg, kindImm, kindMask, kindWord, kindCond, kindRel, kindAddress:
		return o.name
	case kindDisp, kindPageOffset:
		return "[base + " + o.name + "]"
	default:
		panic(fmt.Sprintf("operand kind %d has no text", o.kind))
	}
}

func paramList(ps []param) string {
	var parts []string
	for _, p := range ps {
		parts = append(parts, p.name+" "+p.ty)
	}
	return strings.Join(parts, ", ")
}

// encoderName is the name of a form's encoder.
func encoderName(name string) string { return "encodeArm64" + name }

// call is a call of an encoder with the given arguments, labelled by the parameter names.
func call(name string, ps []param, args []string) string {
	var text strings.Builder
	text.WriteString(encoderName(name) + "(code")
	for i, p := range ps {
		text.WriteString(", " + p.name + ": " + args[i])
	}
	return text.String() + ")"
}

// fieldExpr is the Maxon of an operand's field, in place in the word.
func fieldExpr(o operand) string {
	switch o.kind {
	case kindReg:
		return fmt.Sprintf("arm64RegisterField(%s, at: %d)", o.name, o.at)
	case kindImm:
		return fmt.Sprintf("((%s as Arm64Bits) shl %d)", o.name, o.at)
	case kindDisp, kindRel, kindPageOffset:
		lo, hi := unitRange(o)
		return fmt.Sprintf("arm64UnitsField(%s as FullRangeInt, scale: %d, low: %d, high: %d, bits: %d, at: %d)", o.name, o.scale, lo, hi, o.bits, o.at)
	case kindMask:
		return fmt.Sprintf("arm64LogicalField(%s, at: %d)", o.name, o.at)
	case kindCond:
		if o.invert {
			return fmt.Sprintf("((arm64ConditionCode(%s) xor arm64OppositeCondition) shl %d)", o.name, o.at)
		}
		return fmt.Sprintf("(arm64ConditionCode(%s) shl %d)", o.name, o.at)
	case kindAddress:
		return fmt.Sprintf("arm64AddressField(%s)", o.name)
	default:
		panic(fmt.Sprintf("operand kind %d has no field", o.kind))
	}
}

func emitForm(b *strings.Builder, f form) {
	var texts []string
	_, disp := withKind(f, kindDisp)
	_, page := withKind(f, kindPageOffset)
	indexed := slices.ContainsFunc(f.operands, func(o operand) bool { return o.name == "index" })
	for _, o := range f.operands {
		switch {
		case o.kind == kindReg && o.name == "base" && (disp || page):
		case o.kind == kindReg && o.name == "base" && indexed:
			texts = append(texts, "[base + index]")
		case o.kind == kindReg && o.name == "base":
			texts = append(texts, "[base]")
		case o.kind == kindReg && o.name == "index":
		default:
			texts = append(texts, operandText(o))
		}
	}
	what := f.mnemonic
	if len(texts) > 0 {
		what += " " + strings.Join(texts, ", ")
	}
	direct := ""
	if f.direct {
		direct = " A direct form, which no machine instruction holds."
	}
	if f.isLadder() {
		fmt.Fprintf(b, "// %s: the ladder of the word, one to four moves.\n", what)
	} else {
		fmt.Fprintf(b, "// %s: 0x%08X.%s\n", what, f.opcode, direct)
	}
	list := "mutates code ByteArray"
	if ps := f.params(); len(ps) > 0 {
		list += ", " + paramList(ps)
	}
	fmt.Fprintf(b, "export function %s(%s)\n", encoderName(f.name), list)
	if f.isLadder() {
		fmt.Fprintf(b, "\tencodeArm64MovImm(code, dest: dest, value: imm)\n")
		fmt.Fprintf(b, "end '%s'\n\n", encoderName(f.name))
		return
	}
	var word strings.Builder
	word.WriteString(fmt.Sprintf("0x%08X", f.opcode))
	for _, o := range f.operands {
		word.WriteString(" or " + fieldExpr(o))
	}
	fmt.Fprintf(b, "\tarm64Emit(code, word: %s)\n", word.String())
	fmt.Fprintf(b, "end '%s'\n\n", encoderName(f.name))
}

// emitHolds writes, for a form with an offset, whether its field holds an offset, which instruction selection asks
// before it picks the form.
func emitHolds(b *strings.Builder, f form) {
	o, ok := withKind(f, kindDisp)
	if !ok {
		return
	}
	lo, hi := unitRange(o)
	name := "arm64" + f.name + "Holds"
	fmt.Fprintf(b, "// %s is whether the offset of %s holds disp.\n", name, f.name)
	fmt.Fprintf(b, "export function %s(disp FullRangeInt) returns bool\n", name)
	fmt.Fprintf(b, "\treturn arm64HoldsUnits(disp, scale: %d, low: %d, high: %d)\nend '%s'\n\n", o.scale, lo, hi, name)
}

// emitFormEnum writes one case per form of the machine IR, so a form added to the table fails the build of every match
// that does not name it.
func emitFormEnum(b *strings.Builder) {
	b.WriteString("// Arm64Form is every form of the table that a machine instruction holds, for the instruction selection to ask\n// about.\n")
	b.WriteString("export enum Arm64Form\n")
	for _, f := range machineForms() {
		fmt.Fprintf(b, "\t%s\n", lowerFirst(f.name))
	}
	b.WriteString("end 'Arm64Form'\n\n")
}
