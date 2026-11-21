package backend

// A byte array, an array that packs bytes (ast.BuiltinTy.PacksBytes), has the array's box (layout_gen.go) with a buffer
// of one byte to an element. It starts empty with no buffer, and grows by doubling from bytesFirstCapacity; the old
// buffer is freed.

// EmitBytesRuntime is the unit with the byte array's runtime entries. It is linked only into a program that calls one.
func EmitBytesRuntime() *CodeUnit {
	u := NewCodeUnit()
	g := &bytesUnit{runtimeUnit: newRuntimeUnit(u), outOfRange: NewLabel(), ensureCap: NewLabel()}
	g.emitHelpers()
	g.defineEntries()
	return u
}

// bytesUnit writes the byte array unit.
type bytesUnit struct {
	*runtimeUnit
	outOfRange, ensureCap *Label
}

// emitHelpers writes the code that the entries of the unit share.
func (g *bytesUnit) emitHelpers() {
	a := g.asm
	// Helper: ensureCap(Rbx = header, R14 = current len, R15 = needed cap). Preserves Rbx, R12, R13, R14, R15.
	// Uses Rax, Rcx, Rdx, Rsi, Rdi, R8.
	a.Bind(g.ensureCap)
	room := NewLabel()
	growLoop := NewLabel()
	sized := NewLabel()
	a.Load(Rax, Rbx, arrayCapacityOffset)
	a.Cmp(R15, Rax)
	a.Jcc(CcBE, room)
	a.Test(Rax, Rax)
	a.Jcc(CcNE, growLoop)
	a.MovImm(Rax, bytesFirstCapacity)
	a.Bind(growLoop)
	a.Cmp(Rax, R15)
	a.Jcc(CcAE, sized)
	a.Add(Rax, Rax)
	a.Jmp(growLoop)
	a.Bind(sized)
	a.Push(Rax) // save new capacity on stack across rt.alloc/rt.free
	a.Push(Rax)
	g.callRuntime(g.rtAlloc, 1)
	a.Mov(Rdi, Rax)
	a.Load(Rsi, Rbx, arrayDataOffset)
	a.Mov(Rcx, R14)
	a.RepMovsb()
	a.Load(Rcx, Rbx, arrayDataOffset)
	a.Store(Rbx, arrayDataOffset, Rax)
	a.Pop(Rax)
	a.Store(Rbx, arrayCapacityOffset, Rax)
	a.Push(Rcx)
	g.callRuntime(g.rtFree, 1)
	a.Bind(room)
	a.Ret()

	a.Bind(g.outOfRange)
	a.LeaData(Rax, g.dataBytes.String(indexOutOfRange))
	a.Push(Rax)
	a.Call(g.rtPanic)
}

func (g *bytesUnit) emitBytesNew() {
	a := g.asm
	g.prologue(0, false)
	a.MovImm(Rax, arrayHeaderBytes)
	a.Push(Rax)
	g.callRuntime(g.rtAlloc, 1)
	g.epilogue()
}

func (g *bytesUnit) emitBytesLen() {
	a := g.asm
	g.prologue(0, false)
	a.Load(Rax, Rbp, 16)
	a.Load(Rax, Rax, arrayLengthOffset)
	g.epilogue()
}

func (g *bytesUnit) emitBytesGet() {
	a := g.asm
	g.prologue(0, false)
	a.Load(Rax, Rbp, 24)
	a.Load(Rcx, Rbp, 16)
	a.Load(Rdx, Rax, arrayLengthOffset)
	a.Cmp(Rcx, Rdx)
	a.Jcc(CcAE, g.outOfRange)
	a.Load(Rax, Rax, arrayDataOffset)
	a.Add(Rax, Rcx)
	a.LoadByte(Rax, Rax, 0)
	g.epilogue()
}

func (g *bytesUnit) emitBytesSet() {
	a := g.asm
	g.prologue(0, false)
	a.Load(Rax, Rbp, 32)
	a.Load(Rcx, Rbp, 24)
	a.Load(R8, Rbp, 16)
	a.Load(Rdx, Rax, arrayLengthOffset)
	a.Cmp(Rcx, Rdx)
	a.Jcc(CcAE, g.outOfRange)
	a.Load(Rax, Rax, arrayDataOffset)
	a.Add(Rax, Rcx)
	a.StoreByte(Rax, 0, R8)
	g.epilogue()
}

// insert(bytes, index, byte): the byte goes in at index, which may be the length, after what follows moves up by one.
func (g *bytesUnit) emitBytesInsert() {
	a := g.asm
	g.prologue(0, false)
	shift := NewLabel()
	shifted := NewLabel()
	a.Load(Rbx, Rbp, 32)
	a.Load(R12, Rbp, 24)
	a.Load(R13, Rbp, 16)
	a.Load(R14, Rbx, arrayLengthOffset)
	a.Cmp(R12, R14)
	a.Jcc(CcA, g.outOfRange)
	a.Lea(R15, R14, 1)
	a.Call(g.ensureCap)
	a.Load(Rdx, Rbx, arrayDataOffset)
	a.Mov(Rcx, R14)
	a.Bind(shift)
	a.Cmp(Rcx, R12)
	a.Jcc(CcBE, shifted)
	a.Mov(R8, Rdx)
	a.Add(R8, Rcx)
	a.LoadByte(Rax, R8, -1)
	a.StoreByte(R8, 0, Rax)
	a.SubImm(Rcx, 1)
	a.Jmp(shift)
	a.Bind(shifted)
	a.Mov(R8, Rdx)
	a.Add(R8, R12)
	a.StoreByte(R8, 0, R13)
	a.Store(Rbx, arrayLengthOffset, R15)
	g.epilogue()
}

// remove(bytes, index) -> byte: the byte at index, after what follows moves down by one.
func (g *bytesUnit) emitBytesRemove() {
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
	a.Mov(R8, Rdx)
	a.Add(R8, R12)
	a.LoadByte(R13, R8, 0)
	a.Mov(Rcx, Rdx)
	a.Add(Rcx, R14)
	a.Bind(closeUp)
	a.Lea(R9, R8, 1)
	a.Cmp(R9, Rcx)
	a.Jcc(CcAE, closed)
	a.LoadByte(Rax, R9, 0)
	a.StoreByte(R8, 0, Rax)
	a.Mov(R8, R9)
	a.Jmp(closeUp)
	a.Bind(closed)
	a.SubImm(R14, 1)
	a.Store(Rbx, arrayLengthOffset, R14)
	a.Mov(Rax, R13)
	g.epilogue()
}

func (g *bytesUnit) emitBytesAppend() {
	a := g.asm
	g.prologue(0, false)
	appendDone := NewLabel()
	a.Load(Rbx, Rbp, 24)
	a.Load(R12, Rbp, 16)
	a.Load(R13, R12, arrayLengthOffset)
	a.Test(R13, R13)
	a.Jcc(CcE, appendDone)
	a.Load(R14, Rbx, arrayLengthOffset)
	a.Mov(R15, R14)
	a.Add(R15, R13)
	a.Call(g.ensureCap)
	a.Load(Rdi, Rbx, arrayDataOffset)
	a.Add(Rdi, R14)
	a.Load(Rsi, R12, arrayDataOffset)
	a.Mov(Rcx, R13)
	a.RepMovsb()
	a.Store(Rbx, arrayLengthOffset, R15)
	a.Bind(appendDone)
	g.epilogue()
}

func (g *bytesUnit) emitBytesAppendStr() {
	a := g.asm
	g.prologue(0, false)
	appendStrDone := NewLabel()
	a.Load(Rbx, Rbp, 24)
	a.Load(R12, Rbp, 16)
	a.Load(R13, R12, stringLengthOffset)
	a.Test(R13, R13)
	a.Jcc(CcE, appendStrDone)
	a.Load(R14, Rbx, arrayLengthOffset)
	a.Mov(R15, R14)
	a.Add(R15, R13)
	a.Call(g.ensureCap)
	a.Load(Rdi, Rbx, arrayDataOffset)
	a.Add(Rdi, R14)
	a.Lea(Rsi, R12, wordBytes)
	a.Mov(Rcx, R13)
	a.RepMovsb()
	a.Store(Rbx, arrayLengthOffset, R15)
	a.Bind(appendStrDone)
	g.epilogue()
}

func (g *bytesUnit) emitBytesToString() {
	a := g.asm
	g.prologue(0, false)
	toStrEmpty := NewLabel()
	a.Load(Rbx, Rbp, 16)
	a.Load(R14, Rbx, arrayLengthOffset)
	a.Lea(Rax, R14, wordBytes)
	a.Push(Rax)
	g.callRuntime(g.rtAlloc, 1)
	a.Mov(R12, Rax)
	a.Store(R12, stringLengthOffset, R14)
	a.Test(R14, R14)
	a.Jcc(CcE, toStrEmpty)
	a.Lea(Rdi, R12, wordBytes)
	a.Load(Rsi, Rbx, arrayDataOffset)
	a.Mov(Rcx, R14)
	a.RepMovsb()
	a.Bind(toStrEmpty)
	a.Mov(Rax, R12)
	g.epilogue()
}
