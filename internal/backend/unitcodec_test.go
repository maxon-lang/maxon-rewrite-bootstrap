package backend

import (
	"bytes"
	"slices"
	"testing"

	"minimaxon/internal/ast"
	"minimaxon/internal/cache"
)

func encodedUnit(u *CodeUnit) []byte {
	var e cache.Encoder
	u.Encode(&e)
	return e.Bytes
}

func decodedUnit(t *testing.T, data []byte, load func(*cache.Decoder) *CodeUnit) *CodeUnit {
	t.Helper()
	d := cache.NewDecoder(data)
	u := load(d)
	if err := d.Done(); err != nil {
		t.Fatalf("a unit that was written does not read: %v", err)
	}
	return u
}

// A unit that is written and read is the unit: the same code, the same references and definitions to link, the same data,
// and the same routines asked for, so a program links to the same bytes whether a module was emitted or read.
func TestAUnitReadsBackAsItWasWritten(t *testing.T) {
	proj, fns := loweredProject(t, routinesFile)
	module := EmitModule(fns)
	data := encodedUnit(module)
	back := decodedUnit(t, data, func(d *cache.Decoder) *CodeUnit { return DecodeUnit(d, proj) })
	if !bytes.Equal(back.Asm.Code, module.Asm.Code) || !bytes.Equal(back.Data.Bytes(), module.Data.Bytes()) {
		t.Error("the code or the data differ")
	}
	if !slices.Equal(back.Asm.SymbolFixups, module.Asm.SymbolFixups) || !slices.Equal(back.Asm.DataFixups, module.Asm.DataFixups) ||
		!slices.Equal(back.Asm.ImportFixups, module.Asm.ImportFixups) {
		t.Error("the fixups differ")
	}
	if !slices.Equal(back.CodeSymbols, module.CodeSymbols) || !slices.Equal(back.DataSymbols, module.DataSymbols) ||
		!slices.Equal(back.Sections, module.Sections) || !slices.Equal(back.Data.pieces, module.Data.pieces) {
		t.Error("the definitions, sections or pieces differ")
	}
	if len(module.needed.list) == 0 || len(back.needed.list) != len(module.needed.list) {
		t.Fatalf("the routines asked for: %d, read back %d", len(module.needed.list), len(back.needed.list))
	}
	for i, r := range module.needed.list {
		if back.needed.list[i].symbol != r.symbol || back.needed.list[i].kind != r.kind {
			t.Errorf("routine %d is %q, read back as %q", i, r.symbol, back.needed.list[i].symbol)
		}
	}
	// What the routines are made from, and so what is linked, is the same.
	want, got := EmitTypeRoutines([]*CodeUnit{module}, 2), EmitTypeRoutines([]*CodeUnit{back}, 2)
	if len(want) != len(got) {
		t.Fatalf("%d routine units, read back %d", len(want), len(got))
	}
	for i := range want {
		if !bytes.Equal(want[i].Asm.Code, got[i].Asm.Code) || !slices.Equal(want[i].CodeSymbols, got[i].CodeSymbols) {
			t.Errorf("routine unit %d differs", i)
		}
	}
	if again := encodedUnit(back); !bytes.Equal(again, data) {
		t.Error("a unit that was read does not write what it was read from")
	}
}

// Whatever happens to the bytes, reading them is an error or a unit, never a crash: an entry the cache hands over is whole,
// but the reader must not trust that with its memory.
func TestAUnitThatIsCutOrDamagedIsAnErrorOrAUnit(t *testing.T) {
	proj, fns := loweredProject(t, routinesFile)
	data := encodedUnit(EmitModule(fns))
	read := func(data []byte) (u *CodeUnit, err error) {
		d := cache.NewDecoder(data)
		u = DecodeUnit(d, proj)
		return u, d.Done()
	}
	for cut := range len(data) {
		if _, err := read(data[:cut]); err == nil {
			t.Fatalf("a unit cut to %d of %d bytes read without an error", cut, len(data))
		}
	}
	damaged := bytes.Clone(data)
	for at := range damaged {
		for _, flip := range []byte{0x01, 0x80, 0xFF} {
			damaged[at] ^= flip
			read(damaged)
			damaged[at] ^= flip
		}
	}
	if _, err := read(append(bytes.Clone(data), 0)); err == nil {
		t.Error("a byte after the unit was accepted")
	}
}

// A routine that names a type the project does not declare means the entry was made for other source: it is an error, and
// the module is generated again.
func TestAUnitThatAsksForAnUnknownTypeIsAnError(t *testing.T) {
	proj, fns := loweredProject(t, routinesFile)
	data := encodedUnit(EmitModule(fns))
	other := bytes.ReplaceAll(data, []byte("export::Tree"), []byte("export::Twee"))
	d := cache.NewDecoder(other)
	if u := DecodeUnit(d, proj); u != nil || d.Err() == nil {
		t.Error("a unit that asks for a type that is not declared was read")
	}
}

// What marks a definition as an instance, and what a unit holds of data, survive the round trip: a unit with no generic function
// in it would not show a flag that was lost.
func TestAnInstanceAndTheDataOfAUnitReadBack(t *testing.T) {
	proj, _ := loweredProject(t, routinesFile)
	generic := &ast.FuncDecl{Name: "swap", LinkName: "export::swap"}
	instance := &ast.FuncDecl{Name: "swap", LinkName: "export::swap[stdlib::Count]", Generic: generic}
	u := NewCodeUnit()
	u.BeginSection()
	u.DefineFunction(instance)
	u.Asm.CallSymbol("export::other")
	u.Asm.LeaData(Rax, u.Data.String("text"))
	u.Asm.CallImport(1)
	u.Asm.Raw(0xC3)
	u.DefineData("rt.heap", 8)
	back := decodedUnit(t, encodedUnit(u), func(d *cache.Decoder) *CodeUnit { return DecodeUnit(d, proj) })
	if len(back.CodeSymbols) != 1 || !back.CodeSymbols[0].Instance || back.CodeSymbols[0].Symbol != "export::swap[stdlib::Count]" {
		t.Errorf("the instance reads back as %+v", back.CodeSymbols)
	}
	if len(back.DataSymbols) != 1 || back.DataSymbols[0].Instance || back.DataSymbols[0].Symbol != "rt.heap" {
		t.Errorf("the data symbol reads back as %+v", back.DataSymbols)
	}
	if !slices.Equal(back.Data.pieces, u.Data.pieces) || !bytes.Equal(back.Data.Bytes(), u.Data.Bytes()) {
		t.Error("the data differs")
	}
	if !slices.Equal(back.Asm.DataFixups, u.Asm.DataFixups) || !slices.Equal(back.Asm.ImportFixups, u.Asm.ImportFixups) ||
		!slices.Equal(back.Asm.SymbolFixups, u.Asm.SymbolFixups) {
		t.Error("the fixups differ")
	}
}
