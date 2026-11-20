package backend

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/cache"
)

// A code unit is written in the wire format of internal/cache so that a later compile can link what an earlier one
// emitted. Everything in it is bytes, offsets and text: a symbol is its text, a routine the unit asks for is its kind and
// the link name of its type (README, "Build cache"). The layout, in order:
//
//	code             bytes
//	symbols          the distinct symbol texts that follow, each once, in the order the lists below first use them
//	symbol fixups    count, then (position, symbol) each
//	data fixups      count, then (position, offset in the data) each
//	import fixups    count, then (position, import) each
//	code symbols     count, then (symbol, offset, instance) each; data symbols the same
//	sections         count, then the four positions of each
//	data             bytes, then the offsets where each piece begins
//	needed routines  count, then (kind, link name of the type) each

// Encode appends the unit to the encoder.
func (u *CodeUnit) Encode(e *cache.Encoder) {
	a := u.Asm
	table := symbolTexts{index: map[string]int{}}
	for _, f := range a.SymbolFixups {
		table.add(f.Symbol)
	}
	for _, d := range u.CodeSymbols {
		table.add(d.Symbol)
	}
	for _, d := range u.DataSymbols {
		table.add(d.Symbol)
	}
	e.Blob(a.Code)
	e.Int(len(table.list))
	for _, s := range table.list {
		e.Text(s)
	}
	e.Int(len(a.SymbolFixups))
	for _, f := range a.SymbolFixups {
		e.Int(f.Pos)
		e.Int(table.index[f.Symbol])
	}
	e.Int(len(a.DataFixups))
	for _, f := range a.DataFixups {
		e.Int(f.Pos)
		e.Int(f.DataOffset)
	}
	e.Int(len(a.ImportFixups))
	for _, f := range a.ImportFixups {
		e.Int(f.Pos)
		e.Int(f.Import)
	}
	for _, defs := range [][]SymbolDef{u.CodeSymbols, u.DataSymbols} {
		e.Int(len(defs))
		for _, d := range defs {
			e.Int(table.index[d.Symbol])
			e.Int(d.Offset)
			e.Bool(d.Instance)
		}
	}
	e.Int(len(u.Sections))
	for _, s := range u.Sections {
		e.Int(s.Code)
		e.Int(s.SymbolFixups)
		e.Int(s.DataFixups)
		e.Int(s.ImportFixups)
	}
	e.Blob(u.Data.bytes)
	e.Int(len(u.Data.pieces))
	for _, p := range u.Data.pieces {
		e.Int(p)
	}
	e.Int(len(u.needed.list))
	for _, r := range u.needed.list {
		e.Int(int(r.kind))
		e.Text(r.ty.LinkName())
	}
}

// symbolTexts numbers the distinct symbols of a unit.
type symbolTexts struct {
	index map[string]int
	list  []string
}

func (t *symbolTexts) add(s string) {
	if _, ok := t.index[s]; !ok {
		t.index[s] = len(t.list)
		t.list = append(t.list, s)
	}
}

// DecodeUnit reads a unit that Encode wrote. The types of the routines it asks for are found in the project's
// declarations, which are all made by the front. A failure is in the decoder's Err and the unit is nil.
func DecodeUnit(d *cache.Decoder, p *ast.Project) *CodeUnit {
	u := &CodeUnit{Asm: &Asm{}}
	a := u.Asm
	a.Code = d.Blob()
	codeLen := len(a.Code)
	symbols := make([]string, d.Count())
	for i := range symbols {
		symbols[i] = d.Text()
	}
	symbol := func() string {
		i := d.Int(len(symbols) - 1)
		if len(symbols) == 0 {
			d.Fail()
			return ""
		}
		return symbols[i]
	}
	a.SymbolFixups = make([]SymbolFixup, d.Count())
	for i := range a.SymbolFixups {
		a.SymbolFixups[i] = SymbolFixup{Pos: d.Int(codeLen), Symbol: symbol()}
	}
	a.DataFixups = make([]DataFixup, d.Count())
	for i := range a.DataFixups {
		a.DataFixups[i] = DataFixup{Pos: d.Int(codeLen), DataOffset: d.Int(maxDataOffset)}
	}
	a.ImportFixups = make([]ImportFixup, d.Count())
	for i := range a.ImportFixups {
		a.ImportFixups[i] = ImportFixup{Pos: d.Int(codeLen), Import: d.Int(maxImportIndex)}
	}
	for _, defs := range []*[]SymbolDef{&u.CodeSymbols, &u.DataSymbols} {
		*defs = make([]SymbolDef, d.Count())
		for i := range *defs {
			(*defs)[i] = SymbolDef{Symbol: symbol(), Offset: d.Int(maxDataOffset), Instance: d.Bool()}
		}
	}
	u.Sections = make([]Section, d.Count())
	for i := range u.Sections {
		u.Sections[i] = Section{
			Code: d.Int(codeLen), SymbolFixups: d.Int(len(a.SymbolFixups)),
			DataFixups: d.Int(len(a.DataFixups)), ImportFixups: d.Int(len(a.ImportFixups)),
		}
	}
	u.Data = &DataSection{bytes: d.Blob()}
	u.Data.pieces = make([]int, d.Count())
	for i := range u.Data.pieces {
		u.Data.pieces[i] = d.Int(len(u.Data.bytes))
	}
	needed := d.Count()
	for range needed {
		kind := d.Int(routineKindCount - 1)
		name := d.Text()
		if d.Err() != nil {
			break
		}
		t, err := ast.ParseLinkType(p, name)
		if err != nil {
			d.Fail()
			break
		}
		u.needed.list = append(u.needed.list, newTypeRoutine(routineKind(kind), t))
	}
	if d.Err() != nil {
		return nil
	}
	return u
}

const (
	// maxDataOffset and maxImportIndex bound what a decoder accepts for an offset into data it has not read yet, so that
	// a damaged entry cannot hold a number that is out of any range a unit can have.
	maxDataOffset  = 1 << 31
	maxImportIndex = 1 << 16
)
