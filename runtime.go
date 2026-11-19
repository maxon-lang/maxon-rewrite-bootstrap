package main

const (
	impGetProcessHeap     = 0
	impHeapAlloc          = 1
	impGetStdHandle       = 2
	impWriteFile          = 3
	impExitProcess        = 4
	stdOutputHandle       = -11
	stdErrorHandle        = -12
	heapZeroMemory        = 8
	panicExitCode         = 1
	windowsHomeBytes      = 48
	windowsShadowSize     = 40
	panicShadowBytes      = 32
	stackAlignMask        = -16
	writeBytesWrittenSlot = 40
	writeOverlappedSlot   = 32
)

type runtimeUnit struct {
	u         *CodeUnit
	rtAlloc   *Label
	rtWrite   *Label
	rtPanic   *Label
	asm       *Asm
	dataBytes *DataSection
}

func EmitRuntime(main *FuncDecl) *CodeUnit {
	u := NewCodeUnit()
	g := &runtimeUnit{
		u:         u,
		rtAlloc:   ExternLabel(RtAlloc),
		rtWrite:   ExternLabel(RtWrite),
		rtPanic:   ExternLabel(RtPanic),
		asm:       u.Asm,
		dataBytes: u.Data,
	}
	u.DefineData(RtHeap, wordBytes)
	u.DefineData(RtStdout, wordBytes)
	u.DefineData(RtStderr, wordBytes)
	g.emitEntry(main)
	g.emitRuntime()
	return u
}

func (g *runtimeUnit) callRuntime(l *Label, args int) {
	g.asm.Call(l)
	if args > 0 {
		g.asm.AddImm(Rsp, args*wordBytes)
	}
}

func (g *runtimeUnit) emitEntry(main *FuncDecl) {
	a := g.asm
	g.u.DefineCode(RtEntry)
	a.SubImm(Rsp, windowsShadowSize)
	a.CallImport(impGetProcessHeap)
	a.StoreSymbol(RtHeap, Rax)
	a.MovImm(Rcx, stdOutputHandle)
	a.CallImport(impGetStdHandle)
	a.StoreSymbol(RtStdout, Rax)
	a.MovImm(Rcx, stdErrorHandle)
	a.CallImport(impGetStdHandle)
	a.StoreSymbol(RtStderr, Rax)
	a.Call(ExternLabel(main))
	a.Mov(Rcx, Rax)
	a.CallImport(impExitProcess)
}

func (g *runtimeUnit) prologue(locals int, callsWindows bool) {
	a := g.asm
	a.Push(Rbp)
	a.Mov(Rbp, Rsp)
	if locals > 0 {
		a.SubImm(Rsp, locals)
	}
	if callsWindows {
		a.AndImm(Rsp, stackAlignMask)
		a.SubImm(Rsp, windowsHomeBytes)
	}
}

func (g *runtimeUnit) epilogue() {
	a := g.asm
	a.Mov(Rsp, Rbp)
	a.Pop(Rbp)
	a.Ret()
}

type panicPiece struct {
	text   int
	isText bool
}

func (g *runtimeUnit) emitRuntime() {
	a := g.asm
	u := g.u
	data := g.dataBytes

	u.DefineCode(RtAlloc)
	g.prologue(0, true)
	a.LoadSymbol(Rcx, RtHeap)
	a.MovImm(Rdx, heapZeroMemory)
	a.Load(R8, Rbp, 16)
	a.CallImport(impHeapAlloc)
	g.epilogue()

	u.DefineCode(RtWrite)
	g.prologue(0, true)
	a.Load(Rcx, Rbp, 24)
	a.Load(Rdx, Rbp, 16)
	a.Load(R8, Rdx, 0)
	a.AddImm(Rdx, wordBytes)
	a.Lea(R9, Rsp, writeBytesWrittenSlot)
	a.Xor(Rax, Rax)
	a.Store(Rsp, writeOverlappedSlot, Rax)
	a.CallImport(impWriteFile)
	g.epilogue()

	u.DefineCode(RtPanic)
	g.prologue(0, false)
	pieces := []panicPiece{{data.String("panic: "), true}, {0, false}}
	pieces = append(pieces, panicPiece{data.String("\n"), true})
	for _, piece := range pieces {
		a.LoadSymbol(Rax, RtStderr)
		a.Push(Rax)
		if piece.isText {
			a.LeaData(Rax, piece.text)
		} else {
			a.Load(Rax, Rbp, 16)
		}
		a.Push(Rax)
		g.callRuntime(g.rtWrite, 2)
	}
	a.AndImm(Rsp, stackAlignMask)
	a.SubImm(Rsp, panicShadowBytes)
	a.MovImm(Rcx, panicExitCode)
	a.CallImport(impExitProcess)

	u.DefineCode(RtConcat)
	g.prologue(0, false)
	a.Load(Rsi, Rbp, 24)
	a.Load(Rdi, Rbp, 16)
	a.Load(Rbx, Rsi, 0)
	a.Load(Rax, Rdi, 0)
	a.Add(Rbx, Rax)
	a.Lea(Rax, Rbx, wordBytes)
	a.Push(Rax)
	g.callRuntime(g.rtAlloc, 1)
	a.Store(Rax, 0, Rbx)
	a.Mov(R12, Rax)
	a.Lea(Rdi, R12, wordBytes)
	for _, argOff := range []int{24, 16} {
		a.Load(Rsi, Rbp, argOff)
		a.Load(Rcx, Rsi, 0)
		a.AddImm(Rsi, wordBytes)
		a.RepMovsb()
	}
	a.Mov(Rax, R12)
	g.epilogue()

	u.DefineCode(RtItoa)
	g.prologue(64, false)
	digits := NewLabel()
	loop := NewLabel()
	noSign := NewLabel()
	a.Load(Rax, Rbp, 24)
	a.Load(R8, Rbp, 16)
	a.Xor(R9, R9)
	a.Mov(Rsi, Rbp)
	a.Test(R8, R8)
	a.Jcc(CcNE, digits)
	a.Test(Rax, Rax)
	a.Jcc(CcNS, digits)
	a.Neg(Rax)
	a.MovImm(R9, 1)
	a.Bind(digits)
	a.MovImm(Rcx, 10)
	a.Bind(loop)
	a.Xor(Rdx, Rdx)
	a.Div(Rcx)
	a.AddImm(Rdx, '0')
	a.SubImm(Rsi, 1)
	a.Raw(0x88, 0x16)
	a.Test(Rax, Rax)
	a.Jcc(CcNE, loop)
	a.Test(R9, R9)
	a.Jcc(CcE, noSign)
	a.SubImm(Rsi, 1)
	a.Raw(0xC6, 0x06, '-')
	a.Bind(noSign)
	a.Mov(Rbx, Rbp)
	a.Sub(Rbx, Rsi)
	a.Lea(Rax, Rbx, wordBytes)
	a.Push(Rax)
	g.callRuntime(g.rtAlloc, 1)
	a.Store(Rax, 0, Rbx)
	a.Mov(R12, Rax)
	a.Lea(Rdi, Rax, wordBytes)
	a.Mov(Rcx, Rbx)
	a.RepMovsb()
	a.Mov(Rax, R12)
	g.epilogue()

	u.DefineCode(RtStreq)
	g.prologue(0, false)
	yes := NewLabel()
	no := NewLabel()
	done := NewLabel()
	a.Load(Rsi, Rbp, 24)
	a.Load(Rdi, Rbp, 16)
	a.Load(Rcx, Rsi, 0)
	a.Load(Rax, Rdi, 0)
	a.Cmp(Rcx, Rax)
	a.Jcc(CcNE, no)
	a.Test(Rcx, Rcx)
	a.Jcc(CcE, yes)
	a.AddImm(Rsi, wordBytes)
	a.AddImm(Rdi, wordBytes)
	a.Raw(0xF3, 0xA6)
	a.Jcc(CcNE, no)
	a.Bind(yes)
	a.MovImm(Rax, 1)
	a.Jmp(done)
	a.Bind(no)
	a.MovImm(Rax, 0)
	a.Bind(done)
	g.epilogue()

	panics := []struct{ symbol, message string }{
		{RtPanicRange, "integer value out of range"},
		{RtPanicOverflow, "integer overflow"},
		{RtPanicDiv, "division by zero"},
		{RtPanicShift, "negative shift count"},
	}
	for _, p := range panics {
		u.DefineCode(p.symbol)
		a.LeaData(Rax, data.String(p.message))
		a.Push(Rax)
		a.Call(g.rtPanic)
	}
}
