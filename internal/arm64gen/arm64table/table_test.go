package arm64table

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

// refLogical is the N:immr:imms of a logical immediate, found by trying every element size, run and rotation, apart
// from the generator's way of finding it.
func refLogical(v uint64) (uint32, bool) {
	for size := 2; size <= 64; size *= 2 {
		for run := 1; run < size; run++ {
			element := uint64(1)<<run - 1
			for rot := 0; rot < size; rot++ {
				// The element rotated right by rot, within size bits.
				rotated := element
				if rot > 0 {
					rotated = (element>>rot | element<<(size-rot)) & (uint64(1)<<size - 1)
				}
				pattern := uint64(0)
				for at := 0; at < 64; at += size {
					pattern |= rotated << at
				}
				if pattern == v {
					n := uint32(0)
					if size == 64 {
						n = 1
					}
					imms := uint32((^(size-1)<<1)|(run-1)) & (1<<sixBits - 1)
					return n<<(2*sixBits) | uint32(rot)<<sixBits | imms, true
				}
			}
		}
	}
	return 0, false
}

// refWord encodes a form the way the table says, apart from the Maxon that is generated from it: vals are the values
// of the form's parameters, in order.
func refWord(f form, vals []int64) uint32 {
	w := f.opcode
	for i, o := range f.operands {
		v := vals[i]
		switch o.kind {
		case kindReg, kindImm:
			w |= uint32(v) << o.at
		case kindDisp, kindRel, kindPageOffset:
			if v%int64(o.scale) != 0 {
				panic(fmt.Sprintf("%s: %d is no whole number of %d", f.name, v, o.scale))
			}
			w |= uint32(v/int64(o.scale)) & (1<<o.bits - 1) << o.at
		case kindAddress:
			field := uint32(v) & (1<<o.bits - 1)
			w |= field&(1<<immloBits-1)<<immloAt | field>>immloBits<<immhiAt
		case kindMask:
			field, ok := refLogical(uint64(v))
			if !ok {
				panic(fmt.Sprintf("%s: %#x is no logical immediate", f.name, v))
			}
			w |= field << o.at
		case kindCond:
			code := uint32(conditions[v].code)
			if o.invert {
				code ^= 1
			}
			w |= code << o.at
		default:
			panic(fmt.Sprintf("operand kind %d has no reference", o.kind))
		}
	}
	return w
}

func wordBytesOf(w uint32) []byte { return binary.LittleEndian.AppendUint32(nil, w) }

var (
	regSpread = []int64{0, 1, 30}
	// stackSpread is the spread of a register that may be sp and that the instruction only reads: the assembler keeps
	// account of what an instruction does to sp, and refuses some changes of it, so the literal words prove a write of it.
	stackSpread  = []int64{0, 1, 30, zeroRegister}
	vectorSpread = []int64{0, 1, 31}
	condSpread   = []int64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	maskSpread   = []int64{1, 0xFF00, 0x5555555555555555, -0x100000000, 0x7FFFFFFFFFFFFFFF, -2, 0x0F0F0F0F0F0F0F0F}
	// highSpread is a shifted immediate's: its 0 is the unshifted form's.
	highSpread = []int64{1, 4095}
	// wideSpread is a move-wide halfword's: the assembler writes the halfword at a shift, and has no way to write 0 at one.
	wideSpread = []int64{1, 0xFFFF}
	// fpImmSpread is a floating-point immediate's: 2.125, 1.0 and -1.9375. The assembler loads 2.0, whose 8 bits are 0,
	// from memory instead, so a literal word has that one.
	fpImmSpread = []int64{1, 0x70, 0xFF}
	// unscaledSpread are offsets that only the unscaled forms hold, so that an assembler that picks the form by the
	// offset picks them.
	unscaledSpread = []int64{-256, -1}
	patchedSpread  = []int64{0}
)

// spreads are the values to try of each parameter of a form. A branch's distance is not one of them: the assembler
// writes it from its labels, so the branches have a test of their own.
func spreads(f form) [][]int64 {
	var out [][]int64
	for _, o := range f.operands {
		switch o.kind {
		case kindReg:
			switch {
			case o.vector:
				out = append(out, vectorSpread)
			case o.stack && o.access == read:
				out = append(out, stackSpread)
			default:
				out = append(out, regSpread)
			}
		case kindImm:
			switch {
			case strings.HasSuffix(f.name, "High"):
				out = append(out, highSpread)
			case o.name == "imm" && o.at == imm16At && o.bits == imm16Bits && len(f.operands) == 3:
				out = append(out, wideSpread)
			case o.at == fpImmAt && o.bits == fpImmBits:
				out = append(out, fpImmSpread)
			default:
				out = append(out, []int64{0, 1, 1<<o.bits - 1})
			}
		case kindDisp:
			switch {
			case !o.signed:
				out = append(out, []int64{0, int64(o.scale), (1<<o.bits - 1) * int64(o.scale)})
			case o.scale == 1:
				out = append(out, unscaledSpread)
			default:
				lo, hi := unitRange(o)
				out = append(out, []int64{lo * int64(o.scale), 0, hi * int64(o.scale)})
			}
		case kindMask:
			out = append(out, maskSpread)
		case kindCond:
			out = append(out, condSpread)
		case kindAddress, kindPageOffset:
			out = append(out, patchedSpread)
		default:
			panic(fmt.Sprintf("%s: operand kind %d has no spread", f.name, o.kind))
		}
	}
	return out
}

// valid is whether the values are an instruction the architecture defines: a pair names two registers, a pair that
// writes its base back names it in neither, and a store-exclusive's status is neither its value nor its base.
func valid(f form, vals []int64) bool {
	regOf := func(name string) int64 {
		for i, o := range f.operands {
			if o.name == name {
				return vals[i]
			}
		}
		return -1
	}
	switch {
	case strings.HasPrefix(f.name, "Ldp") || strings.HasPrefix(f.name, "Stp"):
		first, second, base := regOf("first"), regOf("second"), regOf("base")
		if strings.HasPrefix(f.name, "Ldp") && first == second {
			return false
		}
		writesBack := strings.HasSuffix(f.name, "Pre") || strings.HasSuffix(f.name, "Post")
		return !writesBack || f.operands[0].vector || (base != first && base != second)
	case f.name == "Stlxr":
		return regOf("status") != regOf("src") && regOf("status") != regOf("base")
	}
	return true
}

// product calls f with each combination of the values.
func product(sets [][]int64, f func(vals []int64)) {
	vals := make([]int64, len(sets))
	var rec func(i int)
	rec = func(i int) {
		if i == len(sets) {
			f(slices.Clone(vals))
			return
		}
		for _, v := range sets[i] {
			vals[i] = v
			rec(i + 1)
		}
	}
	rec(0)
}

// isBranch is whether a form's distance is a label's, which the branch test compares.
func isBranch(f form) bool {
	_, ok := withKind(f, kindRel)
	return ok
}

// branchPads are the words between a label and a branch back to it.
var branchPads = []int{0, 1, 5}

func formKey(name string, vals []int64) string { return fmt.Sprintf("form %s %v", name, vals) }
func branchKey(name string, pad int, cond int64) string {
	return fmt.Sprintf("branch %s pad %d cond %d", name, pad, cond)
}

// branchVals are the values of a branch's parameters, its distance back over pad words, for each condition it tests.
func branchVals(f form, pad int) [][]int64 {
	back := int64(-pad * wordBytes)
	var out [][]int64
	switch {
	case f.operands[0].kind == kindCond:
		for _, c := range condSpread {
			out = append(out, []int64{c, back})
		}
	case f.operands[0].kind == kindReg:
		out = append(out, []int64{1, back})
	default:
		out = append(out, []int64{back})
	}
	return out
}

// goldenFile holds the words that the Go toolchain's arm64 assembler wrote for every encoding the test compares the
// table with (golden_test.go captures them): a line is a key, " = " and the bytes in hex.
const goldenFile = "testdata/assembler.golden"

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

// goldenCase is one encoding the test compares: its key in the golden file and the form's values.
type goldenCase struct {
	key  string
	form form
	vals []int64
}

// goldenCases are every encoding the assembler wrote: each form at every valid combination of its spreads, and each
// branch back over each pad.
func goldenCases() []goldenCase {
	var out []goldenCase
	for _, f := range forms {
		if isBranch(f) {
			for _, pad := range branchPads {
				for _, vals := range branchVals(f, pad) {
					c := int64(0)
					if f.operands[0].kind == kindCond {
						c = vals[0]
					}
					out = append(out, goldenCase{branchKey(f.name, pad, c), f, vals})
				}
			}
			continue
		}
		if assemblerless[f.name] {
			continue
		}
		product(spreads(f), func(vals []int64) {
			if valid(f, vals) {
				out = append(out, goldenCase{formKey(f.name, vals), f, vals})
			}
		})
	}
	return out
}

// assemblerless are the forms the assembler has no way to write with a distance of 0, which the literal words prove.
var assemblerless = map[string]bool{"Adrp": true, "Adr": true}

// literal are words of the Arm Architecture Reference Manual's encodings, written out by hand: the forms the assembler
// does not write, and the encodings that the generated Maxon is checked against in self/backend/arm64.maxtest.
var literal = []struct {
	form string
	vals []int64
	want uint32
}{
	{"Adrp", []int64{0, 0}, 0x90000000},
	{"Adrp", []int64{1, 1}, 0xB0000001},
	{"Adrp", []int64{30, -1}, 0xF0FFFFFE},
	{"Adrp", []int64{16, 4}, 0x90000030},
	{"Adr", []int64{0, 0}, 0x10000000},
	{"Adr", []int64{2, -4}, 0x10FFFFE2},
	{"AddRegReg", []int64{0, 1, 2}, 0x8B020020},
	{"Ldr64Off", []int64{0, 31, 8}, 0xF94007E0},
	{"Stp64Pre", []int64{29, 30, 31, -16}, 0xA9BF7BFD},
	{"Ldp64Post", []int64{29, 30, 31, 16}, 0xA8C17BFD},
	{"MovzRegImm", []int64{16, 0x1234, 1}, 0xD2A24690},
	{"AndRegMask", []int64{2, 1, 0xFF00}, 0x92781C22},
	{"CsetReg", []int64{0, 4}, 0x9A9F17E0},
	{"BCond", []int64{4, 8}, 0x54000040},
	{"B", []int64{-4}, 0x17FFFFFF},
	{"Bl", []int64{0x7FFFFFC}, 0x95FFFFFF},
	{"Cbnz", []int64{3, -8}, 0xB5FFFFC3},
	{"FmovDX", []int64{1, 2}, 0x9E670041},
	{"FmovDImm", []int64{0, 0}, 0x1E601000},
	{"AddRegImm", []int64{31, 31, 16}, 0x910043FF},
	{"SubRegImm", []int64{31, 31, 16}, 0xD10043FF},
	{"SubRegImmHigh", []int64{31, 31, 1}, 0xD14007FF},
	{"AddRegImm", []int64{29, 31, 0}, 0x910003FD},
	{"AndRegMask", []int64{31, 1, -16}, 0x927CEC3F},
	{"Ret", nil, 0xD65F03C0},
}

func TestFormsEncodeAsTheAssemblerDid(t *testing.T) {
	check()
	golden := readGolden(t)
	used := map[string]bool{}
	tested := map[string]bool{}
	for _, c := range goldenCases() {
		want, ok := golden[c.key]
		if !ok {
			t.Fatalf("%s holds nothing for %s: run the capture (golden_test.go) again", goldenFile, c.key)
		}
		used[c.key] = true
		tested[c.form.name] = true
		if got := wordBytesOf(refWord(c.form, c.vals)); string(got) != string(want) {
			t.Errorf("%s: the table gives %x, the assembler wrote %x", c.key, got, want)
		}
	}
	for _, l := range literal {
		tested[l.form] = true
		if got := refWord(formByName(l.form), l.vals); got != l.want {
			t.Errorf("%s %v: the table gives %08x, the manual says %08x", l.form, l.vals, got, l.want)
		}
	}
	for _, f := range forms {
		if !tested[f.name] {
			t.Errorf("form %s is compared with nothing", f.name)
		}
	}
	for key := range golden {
		if !used[key] {
			t.Errorf("%s holds %s, which no test compares with: a form or a spread changed since the assembler wrote it", goldenFile, key)
		}
	}
}

// The logical immediates the generator's Maxon encodes the way the reference does, by every element size: the Go of
// the reference is the oracle the Maxon's algorithm is transcribed against, and the assembler proves the reference.
func TestReferenceLogicalImmediates(t *testing.T) {
	for _, c := range []struct {
		value uint64
		ok    bool
	}{{0, false}, {^uint64(0), false}, {1, true}, {0xFF00, true}, {0x5555555555555555, true}, {10, false},
		{0x8000000000000001, true}, {0x00FF00FF00FF00FF, true}, {0x1234, false}} {
		if _, ok := refLogical(c.value); ok != c.ok {
			t.Errorf("%#x: the reference says %t, and it is %t", c.value, ok, c.ok)
		}
	}
}

// selfChecks are encodings that self/backend/arm64.maxtest checks the generated Maxon against: the hex of the words of
// literal rows, which the manual gives, and of golden ones, which the assembler wrote.
var selfChecks = []struct {
	form string
	vals []int64
}{
	{"AddRegReg", []int64{0, 1, 2}},
	{"Ldr64Off", []int64{0, 31, 8}},
	{"Stp64Pre", []int64{29, 30, 31, -16}},
	{"Ldp64Post", []int64{29, 30, 31, 16}},
	{"MovzRegImm", []int64{16, 0x1234, 1}},
	{"Ldr64Unscaled", []int64{1, 31, -256}},
	{"Ldaddal", []int64{1, 30, 0}},
	{"AndRegMask", []int64{1, 30, 0xFF00}},
	{"AndRegMask", []int64{1, 30, 0x5555555555555555}},
	{"AndRegMask", []int64{1, 30, -0x100000000}},
	{"AndRegMask", []int64{1, 30, -2}},
	{"AndRegMask", []int64{1, 30, 0x0F0F0F0F0F0F0F0F}},
	{"CsetReg", []int64{0, 4}},
	{"Adrp", []int64{30, -1}},
	{"BCond", []int64{4, 8}},
	{"FmovDX", []int64{1, 2}},
	{"Ret", nil},
}

// The words in self/backend/arm64.maxtest are these, written as the expected hex of each call.
func TestSelfChecksAreInTheSelfTest(t *testing.T) {
	source, err := os.ReadFile("../../../self/backend/arm64.maxtest")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range selfChecks {
		want := hex.EncodeToString(wordBytesOf(refWord(formByName(c.form), c.vals)))
		if !strings.Contains(string(source), `"`+want+`"`) {
			t.Errorf("self/backend/arm64.maxtest has no check for %s %v, %s", c.form, c.vals, want)
		}
	}
}
