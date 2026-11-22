package x64table

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
)

// refEncode encodes a form the way the table says, independently of the assembler and of the Maxon that is generated
// from the table: vals are the values of the form's parameters, in order.
func refEncode(f form, vals []int64) []byte {
	var regField, rmField, indexField, opcodeReg, base, index, disp, imm int64
	var memory, indexed, ripMem, force bool
	var scale int
	var cc int64
	var trailing []operand
	next := 0
	take := func() int64 { v := vals[next]; next++; return v }
	for _, o := range f.operands {
		switch o.kind {
		case kindReg:
			r := take()
			force = force || (o.byteReg && r >= byteRegisters)
			switch o.field {
			case fieldReg:
				regField = r
			case fieldRM:
				rmField = r
			case fieldOpcode:
				opcodeReg = r
				rmField = r
			}
		case kindMem:
			base, disp = take(), take()
			rmField, memory = base, true
		case kindMemIndex:
			base, index, disp = take(), take(), take()
			rmField, indexField, indexed, scale = base, index, true, o.scale
		case kindRip:
			disp = take()
			ripMem = true
		case kindCond:
			cc = take()
		default:
			imm = take()
			trailing = append(trailing, o)
		}
	}
	out := append([]byte{}, f.prefix...)
	rex := byte(rexBase | (regField>>3)<<2 | (indexField>>3)<<1 | rmField>>3)
	if f.rexW {
		rex |= rexWide
	}
	if rex != rexBase || force {
		out = append(out, rex)
	}
	for i, op := range f.opcode {
		if i == len(f.opcode)-1 {
			if hasField(f, fieldOpcode) {
				op += byte(opcodeReg & (lowRegisters - 1))
			}
			if hasKind(f, kindCond) {
				op += byte(cc)
			}
		}
		out = append(out, op)
	}
	reg := regField & (lowRegisters - 1)
	if f.modrm == modDigit {
		reg = int64(f.digit)
	}
	switch {
	case f.modrm == modNone:
	case memory:
		out = append(out, refMem(reg, base, disp)...)
	case indexed:
		out = append(out, refMemIndex(reg, base, index, scale, disp)...)
	case ripMem:
		out = append(out, byte(reg*regFieldScale+ripRelative))
		out = binary.LittleEndian.AppendUint32(out, uint32(disp))
	default:
		out = append(out, byte(modRegister+reg*regFieldScale+rmField&(lowRegisters-1)))
	}
	for _, o := range trailing {
		switch o.kind {
		case kindImm8:
			out = append(out, byte(imm))
		case kindImm64:
			out = binary.LittleEndian.AppendUint64(out, uint64(imm))
		default:
			out = binary.LittleEndian.AppendUint32(out, uint32(imm))
		}
	}
	return out
}

func refMem(reg, base, disp int64) []byte {
	rm := base & (lowRegisters - 1)
	var out []byte
	switch {
	case disp == 0 && rm != rbpLowBits:
		out = append(out, byte(reg*regFieldScale+rm))
	case disp >= imm8Min && disp <= imm8Max:
		out = append(out, byte(modDisp8+reg*regFieldScale+rm))
	default:
		out = append(out, byte(modDisp32+reg*regFieldScale+rm))
	}
	if rm == rspLowBits {
		out = append(out, sibRsp)
	}
	switch {
	case disp == 0 && rm != rbpLowBits:
	case disp >= imm8Min && disp <= imm8Max:
		out = append(out, byte(disp))
	default:
		out = binary.LittleEndian.AppendUint32(out, uint32(disp))
	}
	return out
}

// refMemIndex is the ModRM byte, the SIB byte and the displacement of [base + index*scale + disp].
func refMemIndex(reg, base, index int64, scale int, disp int64) []byte {
	rm := base & (lowRegisters - 1)
	sib := byte(scaleBitsOf(scale)*sibScaleField + int(index&(lowRegisters-1))*sibIndexField + int(rm))
	switch {
	case disp == 0 && rm != rbpLowBits:
		return []byte{byte(reg*regFieldScale + sibFollows), sib}
	case disp >= imm8Min && disp <= imm8Max:
		return []byte{byte(modDisp8 + reg*regFieldScale + sibFollows), sib, byte(disp)}
	default:
		return binary.LittleEndian.AppendUint32([]byte{byte(modDisp32 + reg*regFieldScale + sibFollows), sib}, uint32(disp))
	}
}

// immRange is the values that an immediate kind holds.
func immRange(k kind) (lo, hi int64) {
	switch k {
	case kindImm8:
		return imm8Min, imm8Max
	case kindImm32:
		return imm32Min, imm32Max
	case kindImm32U:
		return 0, imm32UMax
	case kindImm64:
		return math.MinInt64, math.MaxInt64
	default:
		panic(fmt.Sprintf("kind %d is no immediate", k))
	}
}

// refChoose is the alternative of a choice that its value picks.
func refChoose(c choice, vals []int64) form {
	f := formByName(c.alternatives[0])
	imm := vals[len(vals)-1]
	for _, name := range c.alternatives {
		f = formByName(name)
		if lo, hi := immRange(immediateKindOf(f)); imm >= lo && imm <= hi {
			return f
		}
	}
	panic(fmt.Sprintf("no alternative of %s holds %d", c.name, imm))
}

func refSequence(s sequence, vals []int64) []byte {
	own := paramsOf(s.operands)
	var out []byte
	for _, st := range s.steps {
		args := make([]int64, len(st.args))
		for i, a := range st.args {
			if r := registerIndex(a); r >= 0 {
				args[i] = int64(r)
				continue
			}
			for j, p := range own {
				if p.name == a {
					args[i] = vals[j]
				}
			}
		}
		out = append(out, refEncode(formByName(st.form), args)...)
	}
	return out
}

var (
	regSpread    = []int64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	dispSpread   = []int64{0, 1, -1, 8, 127, -128, 128, -129, 255, 256, 0x1000, -0x1000, math.MaxInt32, math.MinInt32}
	imm8Spread   = []int64{-128, -1, 0, 1, 5, 127}
	imm32USpread = []int64{0, 1, 0x7FFFFFFF, 0x80000000, 0xFFFFFFFF}
	imm64Spread  = []int64{0, 1, 255, math.MaxInt32, 0x80000000, math.MaxUint32, 1 << 32, 1 << 40, -1, math.MinInt32, math.MinInt32 - 1, math.MaxInt64, math.MinInt64, 0x123456789ABCDEF0}
	immSpread    = []int64{math.MinInt32, -129, -128, -1, 0, 1, 127, 128, 0x1234, math.MaxInt32}
	condSpread   = regSpread
	ripSpread    = []int64{0}
)

// spreads are the values to try of each parameter of operands. A relative target is not one of them: where the backend
// writes it depends on its labels, so the branches have a test of their own.
func spreads(ops []operand) [][]int64 {
	var out [][]int64
	for _, o := range ops {
		switch o.kind {
		case kindReg:
			out = append(out, regSpread)
		case kindMem:
			out = append(out, regSpread, dispSpread)
		case kindRip:
			out = append(out, ripSpread)
		case kindCond:
			out = append(out, condSpread)
		case kindImm8:
			out = append(out, imm8Spread)
		case kindImm32:
			out = append(out, immSpread)
		case kindImm32U:
			out = append(out, imm32USpread)
		case kindImm64:
			out = append(out, imm64Spread)
		default:
			panic(fmt.Sprintf("operand kind %d has no spread", o.kind))
		}
	}
	return out
}

// product calls f with each combination of the values.
func product(sets [][]int64, f func(vals []int64)) {
	vals := make([]int64, len(sets))
	var rec func(i int)
	rec = func(i int) {
		if i == len(sets) {
			f(vals)
			return
		}
		for _, v := range sets[i] {
			vals[i] = v
			rec(i + 1)
		}
	}
	rec(0)
}

// literal are the forms that the backend writes as raw bytes, with the arguments of the one use it has.
var literal = []struct {
	form string
	vals []int64
	want []byte
}{
	{"Ud2", nil, []byte{0x0F, 0x0B}},
	{"RepeCmpsb", nil, []byte{0xF3, 0xA6}},
	{"StoreBaseDispImm8", []int64{6, 0, '-'}, []byte{0xC6, 0x06, '-'}},
	{"StoreBaseDispImm32", []int64{6, 0, -2}, []byte{0x48, 0xC7, 0x06, 0xFE, 0xFF, 0xFF, 0xFF}},
	{"StoreBaseDispImm32", []int64{5, -8, 0}, []byte{0x48, 0xC7, 0x45, 0xF8, 0x00, 0x00, 0x00, 0x00}},
	{"StoreBaseDispImm32", []int64{12, 0x100, 1}, []byte{0x49, 0xC7, 0x84, 0x24, 0x00, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00}},
	{"PushImm32", []int64{24}, []byte{0x68, 0x18, 0x00, 0x00, 0x00}},
	{"PushImm32", []int64{-1}, []byte{0x68, 0xFF, 0xFF, 0xFF, 0xFF}},
	{"SubRegImm32", []int64{4, 0}, []byte{0x48, 0x81, 0xEC, 0, 0, 0, 0}},
	{"CallReg", []int64{0}, []byte{0xFF, 0xD0}},
	{"CallReg", []int64{11}, []byte{0x41, 0xFF, 0xD3}},
	// The SSE2 forms, as the Intel manual encodes them: xmm registers in the reg and r/m fields, xmm8 to xmm15 by REX.R
	// and REX.B, and the mandatory prefix before REX.
	{"MovapsRegReg", []int64{1, 2}, []byte{0x0F, 0x28, 0xCA}},
	{"MovapsRegReg", []int64{9, 0}, []byte{0x44, 0x0F, 0x28, 0xC8}},
	{"MovapsRegReg", []int64{0, 15}, []byte{0x41, 0x0F, 0x28, 0xC7}},
	{"XorpsRegReg", []int64{0, 1}, []byte{0x0F, 0x57, 0xC1}},
	{"XorpsRegReg", []int64{8, 8}, []byte{0x45, 0x0F, 0x57, 0xC0}},
	{"AddsdRegReg", []int64{0, 1}, []byte{0xF2, 0x0F, 0x58, 0xC1}},
	{"AddsdRegReg", []int64{10, 3}, []byte{0xF2, 0x44, 0x0F, 0x58, 0xD3}},
	{"SubsdRegReg", []int64{2, 9}, []byte{0xF2, 0x41, 0x0F, 0x5C, 0xD1}},
	{"MulsdRegReg", []int64{0, 1}, []byte{0xF2, 0x0F, 0x59, 0xC1}},
	{"DivsdRegReg", []int64{7, 8}, []byte{0xF2, 0x41, 0x0F, 0x5E, 0xF8}},
	{"SqrtsdRegReg", []int64{1, 0}, []byte{0xF2, 0x0F, 0x51, 0xC8}},
	{"UcomisdRegReg", []int64{0, 1}, []byte{0x66, 0x0F, 0x2E, 0xC1}},
	{"UcomisdRegReg", []int64{12, 3}, []byte{0x66, 0x44, 0x0F, 0x2E, 0xE3}},
	{"RoundsdRegRegImm8", []int64{0, 1, 1}, []byte{0x66, 0x0F, 0x3A, 0x0B, 0xC1, 0x01}},
	{"RoundsdRegRegImm8", []int64{9, 10, 9}, []byte{0x66, 0x45, 0x0F, 0x3A, 0x0B, 0xCA, 0x09}},
	{"Cvtsi2sdRegReg", []int64{0, 0}, []byte{0xF2, 0x48, 0x0F, 0x2A, 0xC0}},
	{"Cvtsi2sdRegReg", []int64{9, 12}, []byte{0xF2, 0x4D, 0x0F, 0x2A, 0xCC}},
	{"Cvttsd2siRegReg", []int64{0, 0}, []byte{0xF2, 0x48, 0x0F, 0x2C, 0xC0}},
	{"Cvttsd2siRegReg", []int64{10, 11}, []byte{0xF2, 0x4D, 0x0F, 0x2C, 0xD3}},
	{"MovqXmmReg", []int64{0, 0}, []byte{0x66, 0x48, 0x0F, 0x6E, 0xC0}},
	{"MovqXmmReg", []int64{9, 1}, []byte{0x66, 0x4C, 0x0F, 0x6E, 0xC9}},
	{"MovqRegXmm", []int64{1, 0}, []byte{0x66, 0x48, 0x0F, 0x7E, 0xC1}},
	{"MovqRegXmm", []int64{8, 15}, []byte{0x66, 0x4D, 0x0F, 0x7E, 0xF8}},
	{"MovsdRegBaseDisp", []int64{0, 1, 0}, []byte{0xF2, 0x0F, 0x10, 0x01}},
	{"MovsdRegBaseDisp", []int64{1, 5, -8}, []byte{0xF2, 0x0F, 0x10, 0x4D, 0xF8}},
	{"MovsdRegBaseDisp", []int64{8, 12, 16}, []byte{0xF2, 0x45, 0x0F, 0x10, 0x44, 0x24, 0x10}},
	{"MovsdBaseDispReg", []int64{5, -16, 2}, []byte{0xF2, 0x0F, 0x11, 0x55, 0xF0}},
	{"MovsdBaseDispReg", []int64{13, 0, 9}, []byte{0xF2, 0x45, 0x0F, 0x11, 0x4D, 0x00}},
	{"MovsdRegBaseIndex8Disp", []int64{0, 1, 2, 0}, []byte{0xF2, 0x0F, 0x10, 0x04, 0xD1}},
	{"MovsdRegBaseIndex8Disp", []int64{9, 13, 12, 0}, []byte{0xF2, 0x47, 0x0F, 0x10, 0x4C, 0xE5, 0x00}},
	{"MovsdBaseIndex8DispReg", []int64{0, 1, 8, 1}, []byte{0xF2, 0x0F, 0x11, 0x4C, 0xC8, 0x08}},
	{"MovsdRegRip", []int64{3, 0}, []byte{0xF2, 0x0F, 0x10, 0x1D, 0x00, 0x00, 0x00, 0x00}},
	{"MovupsBaseDispReg", []int64{5, -32, 6}, []byte{0x0F, 0x11, 0x75, 0xE0}},
	{"MovupsBaseDispReg", []int64{5, -256, 15}, []byte{0x44, 0x0F, 0x11, 0xBD, 0x00, 0xFF, 0xFF, 0xFF}},
	{"MovupsRegBaseDisp", []int64{6, 5, -32}, []byte{0x0F, 0x10, 0x75, 0xE0}},
	// The atomic forms and the read of the thread's context, as the Intel manual encodes them.
	{"LockXaddBaseDispReg", []int64{1, 0, 0}, []byte{0xF0, 0x48, 0x0F, 0xC1, 0x01}},
	{"LockXaddBaseDispReg", []int64{12, 8, 9}, []byte{0xF0, 0x4D, 0x0F, 0xC1, 0x4C, 0x24, 0x08}},
	{"XchgBaseDispReg", []int64{1, 0, 0}, []byte{0x48, 0x87, 0x01}},
	{"XchgBaseDispReg", []int64{13, 0, 2}, []byte{0x49, 0x87, 0x55, 0x00}},
	{"LockCmpxchgBaseDispReg", []int64{1, 0, 2}, []byte{0xF0, 0x48, 0x0F, 0xB1, 0x11}},
	{"Pause", nil, []byte{0xF3, 0x90}},
	{"LoadRegGsBaseDisp", []int64{0, 1, 0x1480}, []byte{0x65, 0x48, 0x8B, 0x81, 0x80, 0x14, 0x00, 0x00}},
	{"LoadRegGsBaseDisp", []int64{9, 0, 0}, []byte{0x65, 0x4C, 0x8B, 0x08}},
	{"LoadRegFsBaseDisp", []int64{0, 1, 0}, []byte{0x64, 0x48, 0x8B, 0x01}},
	{"LoadRegFsBaseDisp", []int64{9, 0, 8}, []byte{0x64, 0x4C, 0x8B, 0x48, 0x08}},
	{"StoreGsBaseDispReg", []int64{11, 8, 0}, []byte{0x65, 0x49, 0x89, 0x43, 0x08}},
	{"StoreGsBaseDispReg", []int64{11, 0x1478, 0}, []byte{0x65, 0x49, 0x89, 0x83, 0x78, 0x14, 0x00, 0x00}},
	{"Pushfq", nil, []byte{0x9C}},
	{"Popfq", nil, []byte{0x9D}},
	{"Syscall", nil, []byte{0x0F, 0x05}},
	// The indexed forms, as LLVM's assembler encodes them: an index, a base whose low bits are rbp's or rsp's, extended
	// registers and the three lengths of displacement.
	{"LoadRegBaseIndex8Disp", []int64{0, 1, 2, 0}, []byte{0x48, 0x8B, 0x04, 0xD1}},
	{"LoadRegBaseIndex8Disp", []int64{9, 13, 12, 0}, []byte{0x4F, 0x8B, 0x4C, 0xE5, 0x00}},
	{"LoadRegBaseIndex8Disp", []int64{0, 5, 1, 16}, []byte{0x48, 0x8B, 0x44, 0xCD, 0x10}},
	{"LoadRegBaseIndex8Disp", []int64{3, 8, 15, 0x1000}, []byte{0x4B, 0x8B, 0x9C, 0xF8, 0x00, 0x10, 0x00, 0x00}},
	{"LoadRegBaseIndex8Disp", []int64{2, 4, 0, -8}, []byte{0x48, 0x8B, 0x54, 0xC4, 0xF8}},
	{"LeaRegBaseIndex8Disp", []int64{11, 4, 8, 8}, []byte{0x4E, 0x8D, 0x5C, 0xC4, 0x08}},
	{"LeaRegBaseIndex8Disp", []int64{0, 1, 2, 0}, []byte{0x48, 0x8D, 0x04, 0xD1}},
	{"MovzxRegByteBaseIndex1Disp", []int64{0, 1, 2, 8}, []byte{0x48, 0x0F, 0xB6, 0x44, 0x11, 0x08}},
	{"MovzxRegByteBaseIndex1Disp", []int64{10, 12, 15, 0}, []byte{0x4F, 0x0F, 0xB6, 0x14, 0x3C}},
	{"MovzxRegByteBaseIndex1Disp", []int64{1, 13, 0, 0}, []byte{0x49, 0x0F, 0xB6, 0x4C, 0x05, 0x00}},
	{"StoreBaseIndex8DispReg", []int64{0, 3, 0, 14}, []byte{0x4C, 0x89, 0x34, 0xD8}},
	{"StoreBaseIndex8DispReg", []int64{5, 9, -129, 1}, []byte{0x4A, 0x89, 0x8C, 0xCD, 0x7F, 0xFF, 0xFF, 0xFF}},
	{"StoreBaseIndex1DispReg8", []int64{7, 6, 0, 6}, []byte{0x40, 0x88, 0x34, 0x37}},
	{"StoreBaseIndex1DispReg8", []int64{8, 0, 1, 1}, []byte{0x41, 0x88, 0x4C, 0x00, 0x01}},
	{"StoreBaseIndex1DispReg8", []int64{3, 12, 200, 15}, []byte{0x46, 0x88, 0xBC, 0x23, 0xC8, 0x00, 0x00, 0x00}},
	// The forms instruction selection added after the assembler was removed, as the Intel manual encodes them and LLVM's
	// disassembler reads them back: or and xor with an immediate, imul with an immediate, the shifts by a constant, the
	// forms with a memory operand, the 32-bit zeroing xor and leave.
	{"OrRegImm8", []int64{0, 1}, []byte{0x48, 0x83, 0xC8, 0x01}},
	{"OrRegImm8", []int64{12, -2}, []byte{0x49, 0x83, 0xCC, 0xFE}},
	{"OrRegImm32", []int64{1, 0x12345}, []byte{0x48, 0x81, 0xC9, 0x45, 0x23, 0x01, 0x00}},
	{"XorRegImm8", []int64{3, 127}, []byte{0x48, 0x83, 0xF3, 0x7F}},
	{"XorRegImm32", []int64{8, -256}, []byte{0x49, 0x81, 0xF0, 0x00, 0xFF, 0xFF, 0xFF}},
	{"ImulRegRegImm8", []int64{0, 1, 5}, []byte{0x48, 0x6B, 0xC1, 0x05}},
	{"ImulRegRegImm8", []int64{9, 12, -3}, []byte{0x4D, 0x6B, 0xCC, 0xFD}},
	{"ImulRegRegImm32", []int64{2, 13, 1000}, []byte{0x49, 0x69, 0xD5, 0xE8, 0x03, 0x00, 0x00}},
	{"ImulRegRegImm32", []int64{5, 4, -70000}, []byte{0x48, 0x69, 0xEC, 0x90, 0xEE, 0xFE, 0xFF}},
	{"ShlRegImm8", []int64{0, 3}, []byte{0x48, 0xC1, 0xE0, 0x03}},
	{"ShlRegImm8", []int64{13, 63}, []byte{0x49, 0xC1, 0xE5, 0x3F}},
	{"ShrRegImm8", []int64{1, 1}, []byte{0x48, 0xC1, 0xE9, 0x01}},
	{"ShrRegImm8", []int64{15, 32}, []byte{0x49, 0xC1, 0xEF, 0x20}},
	{"CmpRegBaseDisp", []int64{1, 3, 0}, []byte{0x48, 0x3B, 0x0B}},
	{"CmpRegBaseDisp", []int64{9, 13, 8}, []byte{0x4D, 0x3B, 0x4D, 0x08}},
	{"CmpRegBaseDisp", []int64{0, 4, 16}, []byte{0x48, 0x3B, 0x44, 0x24, 0x10}},
	{"CmpRegBaseDisp", []int64{10, 0, 0x1000}, []byte{0x4C, 0x3B, 0x90, 0x00, 0x10, 0x00, 0x00}},
	{"LoadRegBaseIndex1Disp", []int64{0, 1, 2, 0}, []byte{0x48, 0x8B, 0x04, 0x11}},
	{"LoadRegBaseIndex1Disp", []int64{9, 13, 12, 0}, []byte{0x4F, 0x8B, 0x4C, 0x25, 0x00}},
	{"LoadRegBaseIndex1Disp", []int64{3, 4, 5, 16}, []byte{0x48, 0x8B, 0x5C, 0x2C, 0x10}},
	{"StoreBaseIndex1DispReg", []int64{0, 3, 0, 14}, []byte{0x4C, 0x89, 0x34, 0x18}},
	{"StoreBaseIndex1DispReg", []int64{5, 9, -129, 1}, []byte{0x4A, 0x89, 0x8C, 0x0D, 0x7F, 0xFF, 0xFF, 0xFF}},
	{"XorReg32Reg32", []int64{0, 0}, []byte{0x31, 0xC0}},
	{"XorReg32Reg32", []int64{7, 7}, []byte{0x31, 0xFF}},
	{"XorReg32Reg32", []int64{9, 9}, []byte{0x45, 0x31, 0xC9}},
	{"Leave", nil, []byte{0xC9}},
	// The one-operand signed multiply, which gives the high word of a product in rdx.
	{"ImulReg", []int64{1}, []byte{0x48, 0xF7, 0xE9}},
	{"ImulReg", []int64{11}, []byte{0x49, 0xF7, 0xEB}},
	// The fill between functions and the nops before a loop's head, as the Intel manual lists the recommended nop of each
	// length (vol. 2B, NOP).
	{"Int3", nil, []byte{0xCC}},
	{"Nop1", nil, []byte{0x90}},
	{"Nop2", nil, []byte{0x66, 0x90}},
	{"Nop3", nil, []byte{0x0F, 0x1F, 0x00}},
	{"Nop4", nil, []byte{0x0F, 0x1F, 0x40, 0x00}},
	{"Nop5", nil, []byte{0x0F, 0x1F, 0x44, 0x00, 0x00}},
	{"Nop6", nil, []byte{0x66, 0x0F, 0x1F, 0x44, 0x00, 0x00}},
	{"Nop7", nil, []byte{0x0F, 0x1F, 0x80, 0x00, 0x00, 0x00, 0x00}},
	{"Nop8", nil, []byte{0x0F, 0x1F, 0x84, 0x00, 0x00, 0x00, 0x00, 0x00}},
	{"Nop9", nil, []byte{0x66, 0x0F, 0x1F, 0x84, 0x00, 0x00, 0x00, 0x00, 0x00}},
}

func immediateSpread(k kind) []int64 {
	switch k {
	case kindImm8:
		return imm8Spread
	case kindImm32:
		return immSpread
	case kindImm32U:
		return imm32USpread
	case kindImm64:
		return append(append([]int64{}, imm64Spread...), immSpread...)
	default:
		panic(fmt.Sprintf("kind %d has no spread", k))
	}
}

// goldenFile holds the bytes that the Go compiler's assembler wrote for every encoding these tests compare the table
// with, captured from it before it was removed: a line is a key, " = " and the bytes in hex. The table was proved
// against that assembler, and stays proved against what it wrote.
const goldenFile = "testdata/assembler.golden"

// branchPads are the bytes put between a label and a branch back to it.
var branchPads = []int{0, 1, 5, 127, 128, 1000, 70000}

func formKey(name string, vals []int64) string     { return fmt.Sprintf("form %s %v", name, vals) }
func choiceKey(name string, vals []int64) string   { return fmt.Sprintf("choice %s %v", name, vals) }
func sequenceKey(name string, vals []int64) string { return fmt.Sprintf("sequence %s %v", name, vals) }
func branchKey(name string, pad int, cc int64) string {
	return fmt.Sprintf("branch %s pad %d cc %d", name, pad, cc)
}
func selfKey(call string) string { return "self " + call }

// readGolden is the golden file, by key.
func readGolden(t *testing.T) map[string][]byte {
	t.Helper()
	data, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for line := range strings.SplitSeq(strings.TrimRight(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n"), "\n") {
		key, text, ok := strings.Cut(line, " = ")
		if !ok {
			t.Fatalf("%s: %q is not a key, \" = \" and bytes", goldenFile, line)
		}
		b, err := hex.DecodeString(text)
		if err != nil {
			t.Fatalf("%s: %q: %v", goldenFile, line, err)
		}
		out[key] = b
	}
	return out
}

// against compares what the table encodes with what the assembler wrote, and counts the keys it used.
type against struct {
	t      *testing.T
	golden map[string][]byte
	used   map[string]bool
}

func (c *against) compare(key string, got []byte) {
	c.t.Helper()
	want, ok := c.golden[key]
	if !ok {
		c.t.Fatalf("%s holds nothing for %s: the assembler never wrote it, so the table cannot be proved there", goldenFile, key)
	}
	c.used[key] = true
	if !bytes.Equal(got, want) {
		c.t.Fatalf("%s: the table gives %x, the assembler wrote %x", key, got, want)
	}
}

func TestFormsEncodeAsTheAssemblerDid(t *testing.T) {
	check()
	c := &against{t: t, golden: readGolden(t), used: map[string]bool{}}
	tested := map[string]bool{}
	for _, f := range forms {
		if !assembled[f.name] {
			continue
		}
		tested[f.name] = true
		n := 0
		product(spreads(f.operands), func(vals []int64) {
			n++
			c.compare(formKey(f.name, vals), refEncode(f, vals))
		})
		if n == 0 {
			t.Errorf("%s was tried with no values", f.name)
		}
	}
	for _, l := range literal {
		tested[l.form] = true
		if got := refEncode(formByName(l.form), l.vals); !bytes.Equal(got, l.want) {
			t.Errorf("%s: the table gives %x, the backend writes %x", l.form, got, l.want)
		}
	}
	for _, ch := range choices {
		picked := map[string]bool{}
		first := formByName(ch.alternatives[0])
		product(spreads(withoutImmediate(first.operands)), func(base []int64) {
			product([][]int64{immediateSpread(ch.immediateKind())}, func(imm []int64) {
				vals := append(append([]int64{}, base...), imm[0])
				f := refChoose(ch, vals)
				picked[f.name] = true
				c.compare(choiceKey(ch.name, vals), refEncode(f, vals))
			})
		})
		for _, name := range ch.alternatives {
			tested[name] = true
			if !picked[name] {
				t.Errorf("no value picked %s in choice %s", name, ch.name)
			}
		}
	}
	for _, s := range sequences {
		for _, st := range s.steps {
			tested[st.form] = true
		}
		product(spreads(s.operands), func(vals []int64) {
			c.compare(sequenceKey(s.name, vals), refSequence(s, vals))
		})
	}
	for _, f := range forms {
		if !tested[f.name] && !isBranch(f) {
			t.Errorf("form %s is compared with nothing", f.name)
		}
	}
	// A branch to a label that is bound is written relative to the end of its own field, whatever came before it.
	for _, pad := range branchPads {
		for _, f := range forms {
			if !isBranch(f) {
				continue
			}
			ccs := []int64{0}
			if hasKind(f, kindCond) {
				ccs = condSpread
			}
			for _, cc := range ccs {
				var vals []int64
				if hasKind(f, kindCond) {
					vals = append(vals, cc)
				}
				vals = append(vals, int64(-(pad + len(f.opcode) + rel32Bytes)))
				c.compare(branchKey(f.name, pad, cc), refEncode(f, vals))
			}
		}
	}
	for _, call := range selfChecks {
		c.used[selfKey(call)] = true
	}
	for key := range c.golden {
		if !c.used[key] {
			t.Errorf("%s holds %s, which no test compares with: a form, a spread or a check changed since the assembler wrote it", goldenFile, key)
		}
	}
}

// assembled are the forms that the assembler made with a method of their own, and were compared with every value of
// their spreads.
var assembled = map[string]bool{
	"MovRegReg": true, "AddRegReg": true, "SubRegReg": true, "AndRegReg": true, "OrRegReg": true, "XorRegReg": true,
	"CmpRegReg": true, "TestRegReg": true, "ImulRegReg": true, "LoadRegBaseDisp": true, "StoreBaseDispReg": true,
	"LeaRegBaseDisp": true, "MovzxRegByteBaseDisp": true, "StoreBaseDispReg8": true, "MovzxRegWordBaseDisp": true,
	"LoadReg32BaseDisp": true, "StoreBaseDispReg16": true, "StoreBaseDispReg32": true, "NotReg": true, "NegReg": true,
	"MulReg": true, "DivReg": true, "IdivReg": true, "Cqo": true, "ShlRegCl": true, "ShrRegCl": true, "SarRegCl": true,
	"SarRegImm8": true, "PushReg": true, "PopReg": true, "Ret": true, "RepMovsb": true, "RepStosq": true,
	"LeaRegRip": true, "LoadRegRip": true, "StoreRipReg": true, "CallRip": true,
}

// selfChecks are encodings that self/backend/pe.maxtest checks the generated Maxon against, so that the Maxon is proved through
// these bytes against what the assembler wrote.
var selfChecks = []string{
	"movRegReg rax, rcx",
	"movRegReg r9, r12",
	"addRegReg rbx, r15",
	"loadRegBaseDisp rax, rsp, 8",
	"loadRegBaseDisp rax, rbp, 0",
	"loadRegBaseDisp r13, r13, -129",
	"storeBaseDispReg r12, 256, rax",
	"storeBaseDispReg8 rax, 0, rcx",
	"storeBaseDispReg8 rax, 0, rsi",
	"storeBaseDispReg8 r8, 0, r12",
	"movRegImm rax, 5",
	"movRegImm rax, -1",
	"movRegImm r10, 1 shl 40",
	"movRegImm rax, i64.min",
	"addRegImm rsp, -16",
	"subRegImm rsp, 4096",
	"cmpRegImm r8, 127",
	"pushReg r14",
	"popReg rbx",
	"setccRax less",
	"cqo",
	"repStosq",
	"leaRegRip r11, 0",
	"callRip 0",
	"jmpRel32 patched back to its own start",
	"jmpRel32 patched past ten bytes",
}

// The golden encodings in self/backend/pe.maxtest are these, written as the expected hex of each call.
func TestGoldenBytesAreInTheSelfTest(t *testing.T) {
	source, err := os.ReadFile("../../../self/backend/pe.maxtest")
	if err != nil {
		t.Fatal(err)
	}
	golden := readGolden(t)
	for _, call := range selfChecks {
		want, ok := golden[selfKey(call)]
		if !ok {
			t.Fatalf("%s holds nothing for %s", goldenFile, selfKey(call))
		}
		if !strings.Contains(string(source), `"`+hex.EncodeToString(want)+`"`) {
			t.Errorf("self/backend/pe.maxtest has no check for %s, %x", call, want)
		}
	}
}

func isBranch(f form) bool { return hasKind(f, kindRel32) }
