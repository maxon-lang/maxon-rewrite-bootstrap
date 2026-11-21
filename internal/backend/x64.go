package backend

import (
	"encoding/binary"
	"math"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
)

type R int

const (
	Rax R = iota
	Rcx
	Rdx
	Rbx
	Rsp
	Rbp
	Rsi
	Rdi
	R8
	R9
	R10
	R11
	R12
	R13
	R14
	R15
)

type Cc int

const (
	CcO  Cc = 0
	CcNO Cc = 1
	CcB  Cc = 2
	CcAE Cc = 3
	CcE  Cc = 4
	CcNE Cc = 5
	CcBE Cc = 6
	CcA  Cc = 7
	CcS  Cc = 8
	CcNS Cc = 9
	CcL  Cc = 0xC
	CcGE Cc = 0xD
	CcLE Cc = 0xE
	CcG  Cc = 0xF
)

const (
	wordBytes     = 8
	rexBase       = 0x40
	rexWide       = 8
	modRegister   = 0xC0
	modDisp8      = 0x40
	modDisp32     = 0x80
	ripRelative   = 5
	sibRsp        = 0x24
	rspLowBits    = 4
	rbpLowBits    = 5
	imm8Min       = -128
	imm8Max       = 127
	opJmpRel32    = 0xE9
	opCallRel32   = 0xE8
	opJccBase     = 0x80
	opSetccBase   = 0x90
	opPushBase    = 0x50
	opPopBase     = 0x58
	opMovImmBase  = 0xB8
	rel32Bytes    = 4
	opRet         = 0xC3
	opPrefixTwo   = 0x0F
	opImulSecond  = 0xAF
	opMovzxSecond = 0xB6
	// opMovzxWordSecond is movzx of a 16-bit source, and opOperandSize the prefix of a 16-bit operand.
	opMovzxWordSecond = 0xB7
	opOperandSize     = 0x66
)

type Label struct {
	Pos  int
	Uses []int
	// Symbol is the symbol of a label that another unit defines; a label of this unit has none.
	Symbol string
}

func NewLabel() *Label {
	return &Label{Pos: -1}
}

func ExternLabel(symbol string) *Label {
	return &Label{Pos: -1, Symbol: symbol}
}

type DataFixup struct {
	Pos        int
	DataOffset int
}

type ImportFixup struct {
	Pos    int
	Import int
}

type SymbolFixup struct {
	Pos    int
	Symbol string
}

// SymbolDef is a symbol that a unit defines and where: an offset in its code or in its data. A symbol is the text that
// the unit's code and the link name it by (see "symbol text" in README, "Build cache"), so a unit does not depend on the
// compile that made it.
type SymbolDef struct {
	Symbol string
	Offset int
	// Instance says the symbol is an instance of a generic function, which every unit that needs it defines, and the link
	// keeps one of; any other symbol is defined once.
	Instance bool
}

type CodeUnit struct {
	Asm  *Asm
	Data *DataSection
	// CodeSymbols and DataSymbols are in the order the unit defined them. A symbol that is defined twice, and is not
	// an instance of a generic, is found by the link, which meets every definition of every unit.
	CodeSymbols []SymbolDef
	DataSymbols []SymbolDef
	// Sections are the parts of the code that the link keeps or drops whole, in order: a function or a routine each. A
	// unit that starts none is one section, so its code may jump between its own symbols.
	Sections []Section
	// needed are the drop and copy routines this unit's code calls, in the order its code first does.
	needed routineSet
}

func NewCodeUnit() *CodeUnit {
	return &CodeUnit{
		Asm:  &Asm{},
		Data: &DataSection{strings: map[string]int{}},
	}
}

func (u *CodeUnit) DefineCode(symbol string) {
	u.CodeSymbols = append(u.CodeSymbols, SymbolDef{Symbol: symbol, Offset: u.Asm.Pos()})
}

// DefineFunction defines the code of a function by its link name.
func (u *CodeUnit) DefineFunction(f *ast.FuncDecl) {
	u.CodeSymbols = append(u.CodeSymbols, SymbolDef{Symbol: functionSymbol(f), Offset: u.Asm.Pos(), Instance: f.Generic != nil})
}

// functionSymbol is the symbol that a function's code is defined and called by.
func functionSymbol(f *ast.FuncDecl) string {
	if f.LinkName == "" {
		panic(diag.Internal("%s has no link name, so no code can be defined or called for it", f.Symbol()))
	}
	return f.LinkName
}

// Section is where a part of a unit's code begins, and where its fixups of each kind begin, which are in the order of
// their positions.
type Section struct {
	Code, SymbolFixups, DataFixups, ImportFixups int
}

// BeginSection starts a part of the code that the link keeps only if the entry reaches it. Code in it reaches other
// sections only through symbols.
func (u *CodeUnit) BeginSection() {
	a := u.Asm
	u.Sections = append(u.Sections, Section{a.Pos(), len(a.SymbolFixups), len(a.DataFixups), len(a.ImportFixups)})
}

func (u *CodeUnit) DefineData(symbol string, size int) {
	u.DataSymbols = append(u.DataSymbols, SymbolDef{Symbol: symbol, Offset: u.Data.Reserve(size)})
}

type DataSection struct {
	bytes   []byte
	strings map[string]int
	// pieces are the offsets where each string and each reserved value begins, which the link keeps or drops whole.
	pieces []int
}

func (d *DataSection) Bytes() []byte { return d.bytes }

func (d *DataSection) align8() {
	for len(d.bytes)%8 != 0 {
		d.bytes = append(d.bytes, 0)
	}
}

func (d *DataSection) Reserve(size int) int {
	d.align8()
	off := len(d.bytes)
	d.pieces = append(d.pieces, off)
	d.bytes = append(d.bytes, make([]byte, size)...)
	return off
}

func (d *DataSection) String(s string) int {
	if off, ok := d.strings[s]; ok {
		return off
	}
	d.align8()
	off := len(d.bytes)
	d.pieces = append(d.pieces, off)
	d.bytes = binary.LittleEndian.AppendUint64(d.bytes, uint64(len(s)))
	d.bytes = append(d.bytes, s...)
	d.strings[s] = off
	return off
}

type Asm struct {
	Code         []byte
	DataFixups   []DataFixup
	ImportFixups []ImportFixup
	SymbolFixups []SymbolFixup
}

func (a *Asm) Pos() int {
	return len(a.Code)
}

func (a *Asm) Raw(bs ...byte) {
	a.Code = append(a.Code, bs...)
}

func (a *Asm) d32(v int32) {
	a.Code = binary.LittleEndian.AppendUint32(a.Code, uint32(v))
}

func (a *Asm) d64(v int64) {
	a.Code = binary.LittleEndian.AppendUint64(a.Code, uint64(v))
}

func (a *Asm) rex(w bool, reg, rm int) {
	rex := rexBase | ((reg >> 3) << 2) | (rm >> 3)
	if w {
		rex |= rexWide
	}
	if rex != rexBase {
		a.Code = append(a.Code, byte(rex))
	}
}

func (a *Asm) modRR(reg, rm int) {
	a.Code = append(a.Code, byte(modRegister|((reg&7)<<3)|(rm&7)))
}

func (a *Asm) mem(reg int, b R, disp int) {
	rm := int(b) & 7
	r := (reg & 7) << 3
	small := disp >= imm8Min && disp <= imm8Max
	noDisp := disp == 0 && rm != rbpLowBits
	if noDisp {
		a.Code = append(a.Code, byte(r|rm))
	} else if small {
		a.Code = append(a.Code, byte(modDisp8|r|rm))
	} else {
		a.Code = append(a.Code, byte(modDisp32|r|rm))
	}
	if rm == rspLowBits {
		a.Code = append(a.Code, sibRsp)
	}
	if noDisp {
		return
	}
	if small {
		a.Code = append(a.Code, byte(int8(disp)))
	} else {
		a.d32(int32(disp))
	}
}

func (a *Asm) ripMem(reg int) {
	a.Code = append(a.Code, byte(((reg&7)<<3)|ripRelative))
}

func (a *Asm) opRR(op byte, dst, src R) {
	a.rex(true, int(src), int(dst))
	a.Code = append(a.Code, op)
	a.modRR(int(src), int(dst))
}

func (a *Asm) Mov(dst, src R) { a.opRR(0x89, dst, src) }
func (a *Asm) Add(dst, src R) { a.opRR(0x01, dst, src) }
func (a *Asm) Sub(dst, src R) { a.opRR(0x29, dst, src) }
func (a *Asm) And(dst, src R) { a.opRR(0x21, dst, src) }
func (a *Asm) Or(dst, src R)  { a.opRR(0x09, dst, src) }
func (a *Asm) Xor(dst, src R) { a.opRR(0x31, dst, src) }
func (a *Asm) Cmp(x, y R)     { a.opRR(0x39, x, y) }
func (a *Asm) Test(x, y R)    { a.opRR(0x85, x, y) }

func (a *Asm) Imul(dst, src R) {
	a.rex(true, int(dst), int(src))
	a.Raw(opPrefixTwo, opImulSecond)
	a.modRR(int(dst), int(src))
}

func (a *Asm) Load(dst, b R, disp int) {
	a.rex(true, int(dst), int(b))
	a.Code = append(a.Code, 0x8B)
	a.mem(int(dst), b, disp)
}

func (a *Asm) Store(b R, disp int, src R) {
	a.rex(true, int(src), int(b))
	a.Code = append(a.Code, 0x89)
	a.mem(int(src), b, disp)
}

func (a *Asm) Lea(dst, b R, disp int) {
	a.rex(true, int(dst), int(b))
	a.Code = append(a.Code, 0x8D)
	a.mem(int(dst), b, disp)
}

func (a *Asm) LoadByte(dst, b R, disp int) {
	a.rex(true, int(dst), int(b))
	a.Raw(opPrefixTwo, opMovzxSecond)
	a.mem(int(dst), b, disp)
}

func (a *Asm) StoreByte(b R, disp int, src R) {
	if src >= 4 || b >= 8 {
		rex := byte(0x40)
		if src >= 8 {
			rex |= 0x04
		}
		if b >= 8 {
			rex |= 0x01
		}
		a.Code = append(a.Code, rex)
	}
	a.Code = append(a.Code, 0x88)
	a.mem(int(src), b, disp)
}

func (a *Asm) MovImm(dst R, imm int64) {
	if imm >= 0 && imm <= math.MaxUint32 {
		a.rex(false, 0, int(dst))
		a.Code = append(a.Code, byte(opMovImmBase+(int(dst)&7)))
		a.d32(int32(uint32(imm)))
	} else if imm >= math.MinInt32 && imm <= math.MaxInt32 {
		a.rex(true, 0, int(dst))
		a.Code = append(a.Code, 0xC7)
		a.modRR(0, int(dst))
		a.d32(int32(imm))
	} else {
		a.rex(true, 0, int(dst))
		a.Code = append(a.Code, byte(opMovImmBase+(int(dst)&7)))
		a.d64(imm)
	}
}

func (a *Asm) group1(ext int, dst R, imm int) {
	a.rex(true, 0, int(dst))
	if imm >= imm8Min && imm <= imm8Max {
		a.Code = append(a.Code, 0x83)
		a.modRR(ext, int(dst))
		a.Code = append(a.Code, byte(int8(imm)))
	} else {
		a.Code = append(a.Code, 0x81)
		a.modRR(ext, int(dst))
		a.d32(int32(imm))
	}
}

func (a *Asm) AddImm(dst R, imm int) { a.group1(0, dst, imm) }
func (a *Asm) AndImm(dst R, imm int) { a.group1(4, dst, imm) }
func (a *Asm) SubImm(dst R, imm int) { a.group1(5, dst, imm) }
func (a *Asm) CmpImm(dst R, imm int) { a.group1(7, dst, imm) }

func (a *Asm) groupF7(ext int, r R) {
	a.rex(true, 0, int(r))
	a.Code = append(a.Code, 0xF7)
	a.modRR(ext, int(r))
}

func (a *Asm) Not(r R)    { a.groupF7(2, r) }
func (a *Asm) Neg(r R)    { a.groupF7(3, r) }
func (a *Asm) MulRax(r R) { a.groupF7(4, r) }
func (a *Asm) Div(r R)    { a.groupF7(6, r) }
func (a *Asm) Idiv(r R)   { a.groupF7(7, r) }
func (a *Asm) Cqo()       { a.Raw(0x48, 0x99) }

func (a *Asm) shiftCl(ext int, r R) {
	a.rex(true, 0, int(r))
	a.Code = append(a.Code, 0xD3)
	a.modRR(ext, int(r))
}

func (a *Asm) ShlCl(r R) { a.shiftCl(4, r) }
func (a *Asm) ShrCl(r R) { a.shiftCl(5, r) }
func (a *Asm) SarCl(r R) { a.shiftCl(7, r) }

func (a *Asm) SarImm(r R, n byte) {
	a.rex(true, 0, int(r))
	a.Code = append(a.Code, 0xC1)
	a.modRR(7, int(r))
	a.Code = append(a.Code, n)
}

func (a *Asm) Push(r R) {
	a.rex(false, 0, int(r))
	a.Code = append(a.Code, byte(opPushBase+(int(r)&7)))
}

func (a *Asm) Pop(r R) {
	a.rex(false, 0, int(r))
	a.Code = append(a.Code, byte(opPopBase+(int(r)&7)))
}

func (a *Asm) SetccRax(cc Cc) {
	a.Raw(opPrefixTwo, byte(opSetccBase+int(cc)), 0xC0)
	a.Raw(0x48, opPrefixTwo, opMovzxSecond, 0xC0)
}

func (a *Asm) Ret() { a.Code = append(a.Code, opRet) }

func (a *Asm) RepMovsb() { a.Raw(0xF3, 0xA4) }

// RepStosq stores rax in rcx words from rdi.
func (a *Asm) RepStosq() { a.Raw(0xF3, 0x48, 0xAB) }

func (a *Asm) ripRelativeData(op byte, reg R, dataOffset int) {
	a.rex(true, int(reg), 0)
	a.Code = append(a.Code, op)
	a.ripMem(int(reg))
	a.DataFixups = append(a.DataFixups, DataFixup{a.Pos(), dataOffset})
	a.d32(0)
}

func (a *Asm) ripRelativeSymbol(op byte, reg R, symbol string) {
	a.rex(true, int(reg), 0)
	a.Code = append(a.Code, op)
	a.ripMem(int(reg))
	a.SymbolFixups = append(a.SymbolFixups, SymbolFixup{a.Pos(), symbol})
	a.d32(0)
}

func (a *Asm) LeaData(dst R, dataOffset int)    { a.ripRelativeData(0x8D, dst, dataOffset) }
func (a *Asm) LoadData(dst R, dataOffset int)   { a.ripRelativeData(0x8B, dst, dataOffset) }
func (a *Asm) StoreData(dataOffset int, src R)  { a.ripRelativeData(0x89, src, dataOffset) }
func (a *Asm) LoadSymbol(dst R, symbol string)  { a.ripRelativeSymbol(0x8B, dst, symbol) }
func (a *Asm) StoreSymbol(symbol string, src R) { a.ripRelativeSymbol(0x89, src, symbol) }
func (a *Asm) LeaSymbol(dst R, symbol string)   { a.ripRelativeSymbol(0x8D, dst, symbol) }

func (a *Asm) CallImport(index int) {
	a.Raw(0xFF, 0x15)
	a.ImportFixups = append(a.ImportFixups, ImportFixup{a.Pos(), index})
	a.d32(0)
}

func (a *Asm) rel32(l *Label) {
	if l.Symbol != "" {
		a.SymbolFixups = append(a.SymbolFixups, SymbolFixup{a.Pos(), l.Symbol})
		a.d32(0)
	} else if l.Pos >= 0 {
		a.d32(int32(l.Pos - (a.Pos() + rel32Bytes)))
	} else {
		l.Uses = append(l.Uses, a.Pos())
		a.d32(0)
	}
}

func (a *Asm) Jmp(l *Label) {
	a.Code = append(a.Code, opJmpRel32)
	a.rel32(l)
}

func (a *Asm) Jcc(cc Cc, l *Label) {
	a.Raw(opPrefixTwo, byte(opJccBase+int(cc)))
	a.rel32(l)
}

// CallSymbol calls a function that another unit, or this one, defines, which the linker finds by its symbol.
func (a *Asm) CallSymbol(symbol string) {
	a.Code = append(a.Code, opCallRel32)
	a.SymbolFixups = append(a.SymbolFixups, SymbolFixup{a.Pos(), symbol})
	a.d32(0)
}

func (a *Asm) Call(l *Label) {
	a.Code = append(a.Code, opCallRel32)
	a.rel32(l)
}

func (a *Asm) Bind(l *Label) {
	if l.Symbol != "" {
		panic(diag.Internal("external label %s cannot be bound in a unit", l.Symbol))
	}
	l.Pos = a.Pos()
	for _, use := range l.Uses {
		a.Patch32(use, int32(l.Pos-(use+rel32Bytes)))
	}
	l.Uses = l.Uses[:0]
}

func (a *Asm) Patch32(pos int, value int32) {
	binary.LittleEndian.PutUint32(a.Code[pos:], uint32(value))
}

func (a *Asm) SubRspPlaceholder() int {
	a.Raw(0x48, 0x81, 0xEC)
	at := a.Pos()
	a.d32(0)
	return at
}

// The sized loads and stores are the encodings self's codegen gives the runtime's raw memory operations; the Go backend
// emits none of them, and has them so that the encoder table of self is checked against them.

func (a *Asm) LoadWord16(dst, b R, disp int) {
	a.rex(true, int(dst), int(b))
	a.Raw(opPrefixTwo, opMovzxWordSecond)
	a.mem(int(dst), b, disp)
}

func (a *Asm) Load32(dst, b R, disp int) {
	a.rex(false, int(dst), int(b))
	a.Code = append(a.Code, 0x8B)
	a.mem(int(dst), b, disp)
}

func (a *Asm) Store16(b R, disp int, src R) {
	a.Code = append(a.Code, opOperandSize)
	a.rex(false, int(src), int(b))
	a.Code = append(a.Code, 0x89)
	a.mem(int(src), b, disp)
}

func (a *Asm) Store32(b R, disp int, src R) {
	a.rex(false, int(src), int(b))
	a.Code = append(a.Code, 0x89)
	a.mem(int(src), b, disp)
}
