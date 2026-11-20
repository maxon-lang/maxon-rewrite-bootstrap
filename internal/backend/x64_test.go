package backend

import (
	"encoding/hex"
	"slices"
	"testing"
)

func TestEncodings(t *testing.T) {
	cases := []struct {
		name string
		emit func(a *Asm)
		want string
	}{
		{"mov rax, rcx", func(a *Asm) { a.Mov(Rax, Rcx) }, "4889c8"},
		{"mov eax, imm32", func(a *Asm) { a.MovImm(Rax, 5) }, "b805000000"},
		{"mov rax, sign-extended imm32", func(a *Asm) { a.MovImm(Rax, -1) }, "48c7c0ffffffff"},
		{"mov rax, imm64", func(a *Asm) { a.MovImm(Rax, 1<<40) }, "48b80000000000010000"},
		{"load through rsp needs a SIB", func(a *Asm) { a.Load(Rax, Rsp, 8) }, "488b442408"},
		{"load through rbp needs a displacement", func(a *Asm) { a.Load(Rax, Rbp, 0) }, "488b4500"},
		{"store through r12, disp32", func(a *Asm) { a.Store(R12, 0x100, Rax) }, "4989842400010000"},
	}
	for _, c := range cases {
		a := &Asm{}
		c.emit(a)
		if got := hex.EncodeToString(a.Code); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestLabelFixups(t *testing.T) {
	a := &Asm{}
	back := NewLabel()
	a.Bind(back)
	a.Jmp(back)
	forward := NewLabel()
	a.Jmp(forward)
	a.Bind(forward)
	if got := hex.EncodeToString(a.Code); got != "e9fbffffff"+"e900000000" {
		t.Fatalf("got %s", got)
	}
}

func definesCode(u *CodeUnit, symbol any) bool {
	return slices.ContainsFunc(u.CodeSymbols, func(d SymbolDef) bool { return d.Symbol == symbol })
}

func definesData(u *CodeUnit, symbol any) bool {
	return slices.ContainsFunc(u.DataSymbols, func(d SymbolDef) bool { return d.Symbol == symbol })
}
