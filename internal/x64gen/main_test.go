package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"minimaxon/internal/backend"
)

func TestGeneratedFileIsCurrent(t *testing.T) {
	code, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(maxonFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), []byte(code)) {
		t.Errorf("%s is not what the instruction table generates; run `go generate ./internal/x64gen`", maxonFile)
	}
}

// refEncode encodes a form the way the table says, independently of internal/backend and of the Maxon that is generated
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

func r(v int64) backend.R { return backend.R(v) }

// direct is the method of the backend's assembler that makes the same instruction as a form.
var direct = map[string]func(a *backend.Asm, v []int64){
	"MovRegReg":            func(a *backend.Asm, v []int64) { a.Mov(r(v[0]), r(v[1])) },
	"AddRegReg":            func(a *backend.Asm, v []int64) { a.Add(r(v[0]), r(v[1])) },
	"SubRegReg":            func(a *backend.Asm, v []int64) { a.Sub(r(v[0]), r(v[1])) },
	"AndRegReg":            func(a *backend.Asm, v []int64) { a.And(r(v[0]), r(v[1])) },
	"OrRegReg":             func(a *backend.Asm, v []int64) { a.Or(r(v[0]), r(v[1])) },
	"XorRegReg":            func(a *backend.Asm, v []int64) { a.Xor(r(v[0]), r(v[1])) },
	"CmpRegReg":            func(a *backend.Asm, v []int64) { a.Cmp(r(v[0]), r(v[1])) },
	"TestRegReg":           func(a *backend.Asm, v []int64) { a.Test(r(v[0]), r(v[1])) },
	"ImulRegReg":           func(a *backend.Asm, v []int64) { a.Imul(r(v[0]), r(v[1])) },
	"LoadRegBaseDisp":      func(a *backend.Asm, v []int64) { a.Load(r(v[0]), r(v[1]), int(v[2])) },
	"StoreBaseDispReg":     func(a *backend.Asm, v []int64) { a.Store(r(v[0]), int(v[1]), r(v[2])) },
	"LeaRegBaseDisp":       func(a *backend.Asm, v []int64) { a.Lea(r(v[0]), r(v[1]), int(v[2])) },
	"MovzxRegByteBaseDisp": func(a *backend.Asm, v []int64) { a.LoadByte(r(v[0]), r(v[1]), int(v[2])) },
	"StoreBaseDispReg8":    func(a *backend.Asm, v []int64) { a.StoreByte(r(v[0]), int(v[1]), r(v[2])) },
	"MovzxRegWordBaseDisp": func(a *backend.Asm, v []int64) { a.LoadWord16(r(v[0]), r(v[1]), int(v[2])) },
	"LoadReg32BaseDisp":    func(a *backend.Asm, v []int64) { a.Load32(r(v[0]), r(v[1]), int(v[2])) },
	"StoreBaseDispReg16":   func(a *backend.Asm, v []int64) { a.Store16(r(v[0]), int(v[1]), r(v[2])) },
	"StoreBaseDispReg32":   func(a *backend.Asm, v []int64) { a.Store32(r(v[0]), int(v[1]), r(v[2])) },
	"NotReg":               func(a *backend.Asm, v []int64) { a.Not(r(v[0])) },
	"NegReg":               func(a *backend.Asm, v []int64) { a.Neg(r(v[0])) },
	"MulReg":               func(a *backend.Asm, v []int64) { a.MulRax(r(v[0])) },
	"DivReg":               func(a *backend.Asm, v []int64) { a.Div(r(v[0])) },
	"IdivReg":              func(a *backend.Asm, v []int64) { a.Idiv(r(v[0])) },
	"Cqo":                  func(a *backend.Asm, v []int64) { a.Cqo() },
	"ShlRegCl":             func(a *backend.Asm, v []int64) { a.ShlCl(r(v[0])) },
	"ShrRegCl":             func(a *backend.Asm, v []int64) { a.ShrCl(r(v[0])) },
	"SarRegCl":             func(a *backend.Asm, v []int64) { a.SarCl(r(v[0])) },
	"SarRegImm8":           func(a *backend.Asm, v []int64) { a.SarImm(r(v[0]), byte(v[1])) },
	"PushReg":              func(a *backend.Asm, v []int64) { a.Push(r(v[0])) },
	"PopReg":               func(a *backend.Asm, v []int64) { a.Pop(r(v[0])) },
	"Ret":                  func(a *backend.Asm, v []int64) { a.Ret() },
	"RepMovsb":             func(a *backend.Asm, v []int64) { a.RepMovsb() },
	"RepStosq":             func(a *backend.Asm, v []int64) { a.RepStosq() },
	"LeaRegRip":            func(a *backend.Asm, v []int64) { a.LeaData(r(v[0]), 0) },
	"LoadRegRip":           func(a *backend.Asm, v []int64) { a.LoadData(r(v[0]), 0) },
	"StoreRipReg":          func(a *backend.Asm, v []int64) { a.StoreData(0, r(v[1])) },
	"CallRip":              func(a *backend.Asm, v []int64) { a.CallImport(0) },
}

// choiceOf are the methods of the backend that pick a form by the value of an immediate, as the choices do. These are the forms that the backend makes only as the alternative of a choice, by the value of the immediate.
var choiceOf = map[string]func(a *backend.Asm, v []int64){
	"AddRegImm": func(a *backend.Asm, v []int64) { a.AddImm(r(v[0]), int(v[1])) },
	"AndRegImm": func(a *backend.Asm, v []int64) { a.AndImm(r(v[0]), int(v[1])) },
	"SubRegImm": func(a *backend.Asm, v []int64) { a.SubImm(r(v[0]), int(v[1])) },
	"CmpRegImm": func(a *backend.Asm, v []int64) { a.CmpImm(r(v[0]), int(v[1])) },
	"MovRegImm": func(a *backend.Asm, v []int64) { a.MovImm(r(v[0]), v[1]) },
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
}

func asmBytes(emit func(a *backend.Asm)) []byte {
	a := &backend.Asm{}
	emit(a)
	return a.Code
}

func TestFormsEncodeAsTheBackendDoes(t *testing.T) {
	check()
	tested := map[string]bool{}
	for _, f := range forms {
		emit, ok := direct[f.name]
		if !ok {
			continue
		}
		tested[f.name] = true
		n := 0
		product(spreads(f.operands), func(vals []int64) {
			n++
			got := refEncode(f, vals)
			want := asmBytes(func(a *backend.Asm) { emit(a, vals) })
			if !bytes.Equal(got, want) {
				t.Fatalf("%s %v: the table gives %x, the backend %x", f.name, vals, got, want)
			}
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
	for _, c := range choices {
		emit, ok := choiceOf[c.name]
		if !ok {
			t.Errorf("choice %s has no method of the backend to compare with", c.name)
			continue
		}
		picked := map[string]bool{}
		first := formByName(c.alternatives[0])
		product(spreads(withoutImmediate(first.operands)), func(base []int64) {
			product([][]int64{immediateSpread(c.immediateKind())}, func(imm []int64) {
				vals := append(append([]int64{}, base...), imm[0])
				f := refChoose(c, vals)
				picked[f.name] = true
				got := refEncode(f, vals)
				want := asmBytes(func(a *backend.Asm) { emit(a, vals) })
				if !bytes.Equal(got, want) {
					t.Fatalf("%s %v: the table gives %x (%s), the backend %x", c.name, vals, got, f.name, want)
				}
			})
		})
		for _, name := range c.alternatives {
			tested[name] = true
			if !picked[name] {
				t.Errorf("no value picked %s in choice %s", name, c.name)
			}
		}
	}
	for _, s := range sequences {
		if s.name != "SetccRax" {
			t.Errorf("sequence %s has no method of the backend to compare with", s.name)
			continue
		}
		for _, st := range s.steps {
			tested[st.form] = true
		}
		product(spreads(s.operands), func(vals []int64) {
			got := refSequence(s, vals)
			want := asmBytes(func(a *backend.Asm) { a.SetccRax(backend.Cc(vals[0])) })
			if !bytes.Equal(got, want) {
				t.Fatalf("%s %v: the table gives %x, the backend %x", s.name, vals, got, want)
			}
		})
	}
	for _, f := range forms {
		if !tested[f.name] && !isBranch(f) {
			t.Errorf("form %s is compared with nothing", f.name)
		}
	}
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

func isBranch(f form) bool { return hasKind(f, kindRel32) }

// A branch to a label that is bound is written relative to the end of its own field, whatever came before it.
func TestBranchesAreRelative(t *testing.T) {
	for _, pad := range []int{0, 1, 5, 127, 128, 1000, 70000} {
		for _, f := range forms {
			if !isBranch(f) {
				continue
			}
			ccs := []int64{0}
			if hasKind(f, kindCond) {
				ccs = condSpread
			}
			for _, cc := range ccs {
				a := &backend.Asm{}
				l := backend.NewLabel()
				a.Bind(l)
				a.Raw(make([]byte, pad)...)
				opcodeLen := len(f.opcode)
				var vals []int64
				switch f.name {
				case "JmpRel32":
					a.Jmp(l)
				case "CallRel32":
					a.Call(l)
				case "JccRel32":
					a.Jcc(backend.Cc(cc), l)
				default:
					t.Fatalf("branch %s has no method of the backend to compare with", f.name)
				}
				if hasKind(f, kindCond) {
					vals = append(vals, cc)
				}
				vals = append(vals, int64(-(pad + opcodeLen + rel32Bytes)))
				want := a.Code[pad:]
				if got := refEncode(f, vals); !bytes.Equal(got, want) {
					t.Fatalf("%s pad %d: the table gives %x, the backend %x", f.name, pad, got, want)
				}
			}
		}
	}
}

// golden are encodings that self/main.maxon checks the generated Maxon against, so that the Maxon is proved against the
// backend through these bytes.
var golden = []struct {
	call string
	emit func(a *backend.Asm)
}{
	{"movRegReg rax, rcx", func(a *backend.Asm) { a.Mov(backend.Rax, backend.Rcx) }},
	{"movRegReg r9, r12", func(a *backend.Asm) { a.Mov(backend.R9, backend.R12) }},
	{"addRegReg rbx, r15", func(a *backend.Asm) { a.Add(backend.Rbx, backend.R15) }},
	{"loadRegBaseDisp rax, rsp, 8", func(a *backend.Asm) { a.Load(backend.Rax, backend.Rsp, 8) }},
	{"loadRegBaseDisp rax, rbp, 0", func(a *backend.Asm) { a.Load(backend.Rax, backend.Rbp, 0) }},
	{"loadRegBaseDisp r13, r13, -129", func(a *backend.Asm) { a.Load(backend.R13, backend.R13, -129) }},
	{"storeBaseDispReg r12, 256, rax", func(a *backend.Asm) { a.Store(backend.R12, 0x100, backend.Rax) }},
	{"storeBaseDispReg8 rax, 0, rcx", func(a *backend.Asm) { a.StoreByte(backend.Rax, 0, backend.Rcx) }},
	{"storeBaseDispReg8 rax, 0, rsi", func(a *backend.Asm) { a.StoreByte(backend.Rax, 0, backend.Rsi) }},
	{"storeBaseDispReg8 r8, 0, r12", func(a *backend.Asm) { a.StoreByte(backend.R8, 0, backend.R12) }},
	{"movRegImm rax, 5", func(a *backend.Asm) { a.MovImm(backend.Rax, 5) }},
	{"movRegImm rax, -1", func(a *backend.Asm) { a.MovImm(backend.Rax, -1) }},
	{"movRegImm r10, 1 shl 40", func(a *backend.Asm) { a.MovImm(backend.R10, 1<<40) }},
	{"movRegImm rax, i64.min", func(a *backend.Asm) { a.MovImm(backend.Rax, math.MinInt64) }},
	{"addRegImm rsp, -16", func(a *backend.Asm) { a.AddImm(backend.Rsp, -16) }},
	{"subRegImm rsp, 4096", func(a *backend.Asm) { a.SubImm(backend.Rsp, 4096) }},
	{"cmpRegImm r8, 127", func(a *backend.Asm) { a.CmpImm(backend.R8, 127) }},
	{"pushReg r14", func(a *backend.Asm) { a.Push(backend.R14) }},
	{"popReg rbx", func(a *backend.Asm) { a.Pop(backend.Rbx) }},
	{"setccRax less", func(a *backend.Asm) { a.SetccRax(backend.CcL) }},
	{"cqo", func(a *backend.Asm) { a.Cqo() }},
	{"repStosq", func(a *backend.Asm) { a.RepStosq() }},
	{"leaRegRip r11, 0", func(a *backend.Asm) { a.LeaData(backend.R11, 0) }},
	{"callRip 0", func(a *backend.Asm) { a.CallImport(0) }},
	{"jmpRel32 patched back to its own start", func(a *backend.Asm) {
		l := backend.NewLabel()
		a.Bind(l)
		a.Jmp(l)
	}},
	{"jmpRel32 patched past ten bytes", func(a *backend.Asm) {
		l := backend.NewLabel()
		a.Jmp(l)
		a.Raw(make([]byte, 10)...)
		a.Bind(l)
	}},
}

// The golden encodings in self/main.maxon are these, written as the expected hex of each call.
func TestGoldenBytesAreInTheSelfTest(t *testing.T) {
	source, err := os.ReadFile("../../self/main.maxon")
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range golden {
		want := hex.EncodeToString(asmBytes(g.emit))
		if !strings.Contains(string(source), `"`+want+`"`) {
			t.Errorf("self/main.maxon has no check for %s, %s", g.call, want)
		}
	}
}
