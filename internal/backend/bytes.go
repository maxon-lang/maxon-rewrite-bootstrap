package backend

import "minimaxon/internal/ir"

// A Bytes buffer is a box of three words: its length in bytes, its capacity in bytes, and the address of its byte
// buffer. It starts empty with no buffer, and grows by doubling from bytesFirstCapacity; the old buffer is freed.
const (
	bytesHeaderBytes    = 3 * wordBytes
	bytesLengthOffset   = 0
	bytesCapacityOffset = wordBytes
	bytesDataOffset     = 2 * wordBytes
	bytesFirstCapacity  = 16
)

// EmitBytesRuntime is the unit with the Bytes runtime entries. It is linked only into a program that calls one.
func EmitBytesRuntime() *CodeUnit {
	u := NewCodeUnit()
	a := u.Asm
	g := &runtimeUnit{
		u:         u,
		rtAlloc:   ExternLabel(ir.RtAlloc.String()),
		rtFree:    ExternLabel(ir.RtFree.String()),
		rtPanic:   ExternLabel(ir.RtPanic.String()),
		asm:       a,
		dataBytes: u.Data,
	}
	outOfRange := NewLabel()
	ensureCap := NewLabel()

	// Helper: ensureCap(Rbx = header, R14 = current len, R15 = needed cap). Preserves Rbx, R12, R13, R14, R15.
	// Uses Rax, Rcx, Rdx, Rsi, Rdi, R8.
	a.Bind(ensureCap)
	room := NewLabel()
	growLoop := NewLabel()
	sized := NewLabel()
	a.Load(Rax, Rbx, bytesCapacityOffset)
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
	a.Load(Rsi, Rbx, bytesDataOffset)
	a.Mov(Rcx, R14)
	a.RepMovsb()
	a.Load(Rcx, Rbx, bytesDataOffset)
	a.Store(Rbx, bytesDataOffset, Rax)
	a.Pop(Rax)
	a.Store(Rbx, bytesCapacityOffset, Rax)
	a.Push(Rcx)
	g.callRuntime(g.rtFree, 1)
	a.Bind(room)
	a.Ret()

	a.Bind(outOfRange)
	a.LeaData(Rax, g.dataBytes.String(indexOutOfRange))
	a.Push(Rax)
	a.Call(g.rtPanic)

	u.DefineCode(ir.RtBytesNew.String())
	g.prologue(0, false)
	a.MovImm(Rax, bytesHeaderBytes)
	a.Push(Rax)
	g.callRuntime(g.rtAlloc, 1)
	g.epilogue()

	u.DefineCode(ir.RtBytesLen.String())
	g.prologue(0, false)
	a.Load(Rax, Rbp, 16)
	a.Load(Rax, Rax, bytesLengthOffset)
	g.epilogue()

	u.DefineCode(ir.RtBytesGet.String())
	g.prologue(0, false)
	a.Load(Rax, Rbp, 24)
	a.Load(Rcx, Rbp, 16)
	a.Load(Rdx, Rax, bytesLengthOffset)
	a.Cmp(Rcx, Rdx)
	a.Jcc(CcAE, outOfRange)
	a.Load(Rax, Rax, bytesDataOffset)
	a.Add(Rax, Rcx)
	a.LoadByte(Rax, Rax, 0)
	g.epilogue()

	u.DefineCode(ir.RtBytesSet.String())
	g.prologue(0, false)
	a.Load(Rax, Rbp, 32)
	a.Load(Rcx, Rbp, 24)
	a.Load(R8, Rbp, 16)
	a.Load(Rdx, Rax, bytesLengthOffset)
	a.Cmp(Rcx, Rdx)
	a.Jcc(CcAE, outOfRange)
	a.Load(Rax, Rax, bytesDataOffset)
	a.Add(Rax, Rcx)
	a.StoreByte(Rax, 0, R8)
	g.epilogue()

	u.DefineCode(ir.RtBytesPush.String())
	g.prologue(0, false)
	a.Load(Rbx, Rbp, 24)
	a.Load(R12, Rbp, 16)
	a.Load(R14, Rbx, bytesLengthOffset)
	a.Lea(R15, R14, 1)
	a.Call(ensureCap)
	a.Load(Rax, Rbx, bytesDataOffset)
	a.Add(Rax, R14)
	a.StoreByte(Rax, 0, R12)
	a.Store(Rbx, bytesLengthOffset, R15)
	g.epilogue()

	u.DefineCode(ir.RtBytesAppend.String())
	g.prologue(0, false)
	appendDone := NewLabel()
	a.Load(Rbx, Rbp, 24)
	a.Load(R12, Rbp, 16)
	a.Load(R13, R12, bytesLengthOffset)
	a.Test(R13, R13)
	a.Jcc(CcE, appendDone)
	a.Load(R14, Rbx, bytesLengthOffset)
	a.Mov(R15, R14)
	a.Add(R15, R13)
	a.Call(ensureCap)
	a.Load(Rdi, Rbx, bytesDataOffset)
	a.Add(Rdi, R14)
	a.Load(Rsi, R12, bytesDataOffset)
	a.Mov(Rcx, R13)
	a.RepMovsb()
	a.Store(Rbx, bytesLengthOffset, R15)
	a.Bind(appendDone)
	g.epilogue()

	u.DefineCode(ir.RtBytesAppendStr.String())
	g.prologue(0, false)
	appendStrDone := NewLabel()
	a.Load(Rbx, Rbp, 24)
	a.Load(R12, Rbp, 16)
	a.Load(R13, R12, stringLengthOffset)
	a.Test(R13, R13)
	a.Jcc(CcE, appendStrDone)
	a.Load(R14, Rbx, bytesLengthOffset)
	a.Mov(R15, R14)
	a.Add(R15, R13)
	a.Call(ensureCap)
	a.Load(Rdi, Rbx, bytesDataOffset)
	a.Add(Rdi, R14)
	a.Lea(Rsi, R12, wordBytes)
	a.Mov(Rcx, R13)
	a.RepMovsb()
	a.Store(Rbx, bytesLengthOffset, R15)
	a.Bind(appendStrDone)
	g.epilogue()

	u.DefineCode(ir.RtBytesToString.String())
	g.prologue(0, false)
	toStrEmpty := NewLabel()
	a.Load(Rbx, Rbp, 16)
	a.Load(R14, Rbx, bytesLengthOffset)
	a.Lea(Rax, R14, wordBytes)
	a.Push(Rax)
	g.callRuntime(g.rtAlloc, 1)
	a.Mov(R12, Rax)
	a.Store(R12, stringLengthOffset, R14)
	a.Test(R14, R14)
	a.Jcc(CcE, toStrEmpty)
	a.Lea(Rdi, R12, wordBytes)
	a.Load(Rsi, Rbx, bytesDataOffset)
	a.Mov(Rcx, R14)
	a.RepMovsb()
	a.Bind(toStrEmpty)
	a.Mov(Rax, R12)
	g.epilogue()

	return u
}
