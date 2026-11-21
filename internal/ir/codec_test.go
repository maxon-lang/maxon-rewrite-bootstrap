package ir

import (
	"testing"

	"minimaxon/internal/ast"
	"minimaxon/internal/cache"
)

// Nothing in the build cache holds IR yet, so this is what keeps the generated codec right.
func TestRecordsSurviveTheCodec(t *testing.T) {
	decl := &ast.FuncDecl{Name: "f"}
	var b Builder
	b.Start(decl, "", 1)
	entry, fail := b.NewBlock(), b.NewBlock()
	v, w := b.F.NewReg(KindScalar), b.F.NewReg(KindManaged)
	instrs := []Instr{
		Const(v, -5),
		b.Str(w, "text"),
		StoreSlot(3, w, true),
		LoadField(v, w, 16),
		Bin(v, BinShl, v, v, true, fail),
		Bin(v, BinAdd, v, v, false, NoBlock),
		Cmp(v, CmpGe, v, v, true),
		Guard(v, GuardSignedAtMost, -1, fail),
		b.ThrowingCall(NoReg, decl, []V{v, w}, v),
		b.Call(v, decl, nil),
		b.CallRuntime(w, RtAlloc, []V{v}, decl),
		b.CallRuntime(NoReg, RtAlloc, nil, nil),
		b.When(0, []V{w}, []V{v, v}),
		b.Copy(w, w, ast.Ty(ast.BoolTy)),
	}
	terms := []Term{Jump(fail), Branch(v, entry, fail), b.Switch(v, []int64{-1, 2}, []BlockID{entry, fail}, fail), Ret(v), Trap(), Throw(v)}

	var e cache.Encoder
	for _, i := range instrs {
		EncodeInstr(&e, i)
	}
	for _, term := range terms {
		EncodeTerm(&e, term)
	}
	d := cache.NewDecoder(e.Bytes)
	for _, want := range instrs {
		if got := DecodeInstr(d); got != want {
			t.Errorf("%s decodes as %+v, want %+v", want.op, got, want)
		}
	}
	for _, want := range terms {
		if got := DecodeTerm(d); got != want {
			t.Errorf("%s decodes as %+v, want %+v", want.kind, got, want)
		}
	}
	if err := d.Done(); err != nil {
		t.Fatal(err)
	}
}
