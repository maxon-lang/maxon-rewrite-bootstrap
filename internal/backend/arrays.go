package backend

import "minimaxon/internal/ir"

// An array's box (layout_gen.go) starts empty with no elements, and grows by doubling; the buffer it leaves is freed, and
// the elements are not, because they moved to the new one.
const indexOutOfRange = "index out of range"

// EmitArrayRuntime is the unit with the array's runtime entries. It is linked only into a program that calls one, so
// the executable of any other program does not hold it.
func EmitArrayRuntime() *CodeUnit {
	u := NewCodeUnit()
	g := &arraysUnit{runtimeUnit: newRuntimeUnit(u), outOfRange: ExternLabel(ir.RtPanicIndex.String())}
	g.defineEntries()
	return u
}

// arraysUnit writes the array unit.
type arraysUnit struct {
	*runtimeUnit
	outOfRange *Label
}

func (g *arraysUnit) emitPanicIndex() {
	a := g.asm
	a.LeaData(Rax, g.dataBytes.String(indexOutOfRange))
	a.Push(Rax)
	a.Call(g.rtPanic)
}

func (g *arraysUnit) emitArrayNew() {
	a := g.asm
	g.prologue(0, false)
	a.MovImm(Rax, arrayHeaderBytes)
	a.Push(Rax)
	g.callRuntime(g.rtAlloc, 1)
	g.epilogue()
}

func (g *arraysUnit) emitArrayLen() {
	a := g.asm
	g.prologue(0, false)
	a.Load(Rax, Rbp, 16)
	a.Load(Rax, Rax, arrayLengthOffset)
	g.epilogue()
}

// elem(array, index): the address of the element, after checking the index against the length.
func (g *arraysUnit) emitArrayElem() {
	a := g.asm
	g.prologue(0, false)
	a.Load(Rax, Rbp, 24)
	a.Load(Rcx, Rbp, 16)
	a.Load(Rdx, Rax, arrayLengthOffset)
	a.Cmp(Rcx, Rdx)
	a.Jcc(CcAE, g.outOfRange)
	a.Load(Rax, Rax, arrayDataOffset)
	a.MovImm(Rdx, wordBytes)
	a.Imul(Rcx, Rdx)
	a.Add(Rax, Rcx)
	g.epilogue()
}

// insert(array, index, element): moves the element in at index, which may be the length, shifting what follows.
func (g *arraysUnit) emitArrayInsert() {
	a := g.asm
	g.prologue(0, false)
	room := NewLabel()
	sized := NewLabel()
	shift := NewLabel()
	shifted := NewLabel()
	a.Load(Rbx, Rbp, 32)
	a.Load(R12, Rbp, 24)
	a.Load(R13, Rbp, 16)
	a.Load(R14, Rbx, arrayLengthOffset)
	a.Cmp(R12, R14)
	a.Jcc(CcA, g.outOfRange)
	a.Load(Rax, Rbx, arrayCapacityOffset)
	a.Cmp(R14, Rax)
	a.Jcc(CcB, room)
	a.Add(Rax, Rax)
	a.Test(Rax, Rax)
	a.Jcc(CcNE, sized)
	a.MovImm(Rax, arrayFirstCapacity)
	a.Bind(sized)
	a.Mov(R15, Rax)
	a.MovImm(Rcx, wordBytes)
	a.Imul(Rax, Rcx)
	a.Push(Rax)
	g.callRuntime(g.rtAlloc, 1)
	a.Mov(Rdi, Rax)
	a.Load(Rsi, Rbx, arrayDataOffset)
	a.Mov(Rcx, R14)
	a.MovImm(Rdx, wordBytes)
	a.Imul(Rcx, Rdx)
	a.RepMovsb()
	a.Load(Rcx, Rbx, arrayDataOffset)
	a.Store(Rbx, arrayDataOffset, Rax)
	a.Store(Rbx, arrayCapacityOffset, R15)
	a.Push(Rcx)
	g.callRuntime(g.rtFree, 1)
	a.Bind(room)
	a.Load(Rdx, Rbx, arrayDataOffset)
	a.MovImm(Rax, wordBytes)
	a.Mov(Rcx, R14)
	a.Imul(Rcx, Rax)
	a.Add(Rcx, Rdx)
	a.Mov(R8, R12)
	a.Imul(R8, Rax)
	a.Add(R8, Rdx)
	a.Bind(shift)
	a.Cmp(Rcx, R8)
	a.Jcc(CcBE, shifted)
	a.Load(Rax, Rcx, -wordBytes)
	a.Store(Rcx, 0, Rax)
	a.SubImm(Rcx, wordBytes)
	a.Jmp(shift)
	a.Bind(shifted)
	a.Store(R8, 0, R13)
	a.AddImm(R14, 1)
	a.Store(Rbx, arrayLengthOffset, R14)
	g.epilogue()
}

// remove(array, index) -> element: moves it out, shifting what follows down.
func (g *arraysUnit) emitArrayRemove() {
	a := g.asm
	g.prologue(0, false)
	closeUp := NewLabel()
	closed := NewLabel()
	a.Load(Rbx, Rbp, 24)
	a.Load(R12, Rbp, 16)
	a.Load(R14, Rbx, arrayLengthOffset)
	a.Cmp(R12, R14)
	a.Jcc(CcAE, g.outOfRange)
	a.Load(Rdx, Rbx, arrayDataOffset)
	a.MovImm(Rax, wordBytes)
	a.Mov(R8, R12)
	a.Imul(R8, Rax)
	a.Add(R8, Rdx)
	a.Load(R13, R8, 0)
	a.Mov(Rcx, R14)
	a.Imul(Rcx, Rax)
	a.Add(Rcx, Rdx)
	a.Bind(closeUp)
	a.Lea(R9, R8, wordBytes)
	a.Cmp(R9, Rcx)
	a.Jcc(CcAE, closed)
	a.Load(Rax, R9, 0)
	a.Store(R8, 0, Rax)
	a.Mov(R8, R9)
	a.Jmp(closeUp)
	a.Bind(closed)
	a.SubImm(R14, 1)
	a.Store(Rbx, arrayLengthOffset, R14)
	a.Mov(Rax, R13)
	g.epilogue()
}
