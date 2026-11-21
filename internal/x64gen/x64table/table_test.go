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
	var regField, rmField, opcodeReg, base, disp, imm int64
	var memory, ripMem, force bool
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
	rex := byte(rexBase | (regField>>3)<<2 | rmField>>3)
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
	{"SubRegImm32", []int64{4, 0}, []byte{0x48, 0x81, 0xEC, 0, 0, 0, 0}},
	{"CallReg", []int64{0}, []byte{0xFF, 0xD0}},
	{"CallReg", []int64{11}, []byte{0x41, 0xFF, 0xD3}},
	// The SSE2 forms, as the Intel manual encodes them.
	{"MovqXmm0Reg", []int64{9}, []byte{0x66, 0x49, 0x0F, 0x6E, 0xC1}},
	{"MovqXmm1Reg", []int64{0}, []byte{0x66, 0x48, 0x0F, 0x6E, 0xC8}},
	{"MovqRegXmm0", []int64{1}, []byte{0x66, 0x48, 0x0F, 0x7E, 0xC1}},
	{"AddsdXmm0Xmm1", nil, []byte{0xF2, 0x0F, 0x58, 0xC1}},
	{"SubsdXmm0Xmm1", nil, []byte{0xF2, 0x0F, 0x5C, 0xC1}},
	{"MulsdXmm0Xmm1", nil, []byte{0xF2, 0x0F, 0x59, 0xC1}},
	{"DivsdXmm0Xmm1", nil, []byte{0xF2, 0x0F, 0x5E, 0xC1}},
	{"SqrtsdXmm0Xmm0", nil, []byte{0xF2, 0x0F, 0x51, 0xC0}},
	{"UcomisdXmm0Xmm1", nil, []byte{0x66, 0x0F, 0x2E, 0xC1}},
	{"RoundsdXmm0Xmm0Imm8", []int64{1}, []byte{0x66, 0x0F, 0x3A, 0x0B, 0xC0, 0x01}},
	{"Cvtsi2sdXmm0Reg", []int64{0}, []byte{0xF2, 0x48, 0x0F, 0x2A, 0xC0}},
	{"Cvttsd2siRaxXmm0", nil, []byte{0xF2, 0x48, 0x0F, 0x2C, 0xC0}},
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

// selfChecks are encodings that self/main.maxon checks the generated Maxon against, so that the Maxon is proved through
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

// The golden encodings in self/main.maxon are these, written as the expected hex of each call.
func TestGoldenBytesAreInTheSelfTest(t *testing.T) {
	source, err := os.ReadFile("../../../self/main.maxon")
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
			t.Errorf("self/main.maxon has no check for %s, %x", call, want)
		}
	}
}

func isBranch(f form) bool { return hasKind(f, kindRel32) }
