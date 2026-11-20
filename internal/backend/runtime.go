package backend

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/ir"
)

const (
	impGetProcessHeap     = 0
	impHeapAlloc          = 1
	impGetStdHandle       = 2
	impWriteFile          = 3
	impExitProcess        = 4
	impHeapFree           = 5
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
	// leakExitCode is what an executable built to count its allocations exits with when main returned and a box is still
	// live. It differs from every code a program or a trap gives.
	leakExitCode   = 99
	leakMessage    = "leaked allocations: "
	leakMessageEnd = "\n"
)

// RuntimeOptions say how the runtime unit is built.
type RuntimeOptions struct {
	// CountAllocations keeps the number of live heap boxes, and has the entry fail when main returns with any left.
	CountAllocations bool
}

type runtimeUnit struct {
	u         *CodeUnit
	count     bool
	rtAlloc   *Label
	rtFree    *Label
	rtWrite   *Label
	rtPanic   *Label
	asm       *Asm
	dataBytes *DataSection
}

func EmitRuntime(main *ast.FuncDecl, o RuntimeOptions) *CodeUnit {
	u := NewCodeUnit()
	g := &runtimeUnit{
		u:         u,
		count:     o.CountAllocations,
		rtAlloc:   ExternLabel(ir.RtAlloc.String()),
		rtFree:    ExternLabel(ir.RtFree.String()),
		rtWrite:   ExternLabel(ir.RtWrite.String()),
		rtPanic:   ExternLabel(ir.RtPanic.String()),
		asm:       u.Asm,
		dataBytes: u.Data,
	}
	u.DefineData(ir.RtHeap.String(), wordBytes)
	u.DefineData(ir.RtStdout.String(), wordBytes)
	u.DefineData(ir.RtStderr.String(), wordBytes)
	if o.CountAllocations {
		u.DefineData(ir.RtLive.String(), wordBytes)
	}
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

func (g *runtimeUnit) emitEntry(main *ast.FuncDecl) {
	a := g.asm
	g.u.DefineCode(ir.RtEntry.String())
	a.SubImm(Rsp, windowsShadowSize)
	a.CallImport(impGetProcessHeap)
	a.StoreSymbol(ir.RtHeap.String(), Rax)
	a.MovImm(Rcx, stdOutputHandle)
	a.CallImport(impGetStdHandle)
	a.StoreSymbol(ir.RtStdout.String(), Rax)
	a.MovImm(Rcx, stdErrorHandle)
	a.CallImport(impGetStdHandle)
	a.StoreSymbol(ir.RtStderr.String(), Rax)
	a.Call(ExternLabel(functionSymbol(main)))
	if g.count {
		g.exitIfLeaking()
	}
	a.Mov(Rcx, Rax)
	a.CallImport(impExitProcess)
}

// exitIfLeaking is the end of main in an executable that counts its allocations: a box still live is a drop that the
// program lacks, so it says so on standard error and exits with leakExitCode. rax, main's exit code, is untouched when none
// is live. A panic or a trap exits without this check.
func (g *runtimeUnit) exitIfLeaking() {
	a := g.asm
	clean := NewLabel()
	a.LoadSymbol(Rcx, ir.RtLive.String())
	a.Test(Rcx, Rcx)
	a.Jcc(CcE, clean)
	g.writeError(func() { a.LeaData(Rax, g.dataBytes.String(leakMessage)) })
	a.LoadSymbol(Rax, ir.RtLive.String())
	a.Push(Rax)
	a.MovImm(Rax, 0)
	a.Push(Rax)
	g.callRuntime(ExternLabel(ir.RtItoa.String()), 2)
	a.Mov(Rbx, Rax)
	g.writeError(func() { a.Mov(Rax, Rbx) })
	g.writeError(func() { a.LeaData(Rax, g.dataBytes.String(leakMessageEnd)) })
	a.MovImm(Rcx, leakExitCode)
	a.CallImport(impExitProcess)
	a.Bind(clean)
}

// writeError writes the string that text puts in rax to standard error.
func (g *runtimeUnit) writeError(text func()) {
	a := g.asm
	a.LoadSymbol(Rax, ir.RtStderr.String())
	a.Push(Rax)
	text()
	a.Push(Rax)
	g.callRuntime(g.rtWrite, 2)
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

	u.DefineCode(ir.RtAlloc.String())
	g.prologue(0, true)
	a.LoadSymbol(Rcx, ir.RtHeap.String())
	a.MovImm(Rdx, heapZeroMemory)
	a.Load(R8, Rbp, 16)
	a.CallImport(impHeapAlloc)
	if g.count {
		g.countLive(1)
	}
	g.epilogue()

	// free(box): gives a box back to the heap. A box that is 0 is a place that held no value, which HeapFree does not
	// accept, so it is skipped.
	u.DefineCode(ir.RtFree.String())
	g.prologue(0, true)
	freed := NewLabel()
	a.Load(R8, Rbp, 16)
	a.Test(R8, R8)
	a.Jcc(CcE, freed)
	if g.count {
		g.countLive(-1)
	}
	a.LoadSymbol(Rcx, ir.RtHeap.String())
	a.Xor(Rdx, Rdx)
	a.CallImport(impHeapFree)
	a.Bind(freed)
	g.epilogue()

	u.DefineCode(ir.RtWrite.String())
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

	u.DefineCode(ir.RtPanic.String())
	g.prologue(0, false)
	pieces := []panicPiece{{data.String("panic: "), true}, {0, false}}
	pieces = append(pieces, panicPiece{data.String("\n"), true})
	for _, piece := range pieces {
		a.LoadSymbol(Rax, ir.RtStderr.String())
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

	u.DefineCode(ir.RtConcat.String())
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

	u.DefineCode(ir.RtItoa.String())
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

	u.DefineCode(ir.RtStreq.String())
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
}

// countLive adds to the number of live boxes, keeping every register but rcx.
func (g *runtimeUnit) countLive(delta int) {
	a := g.asm
	a.LoadSymbol(Rcx, ir.RtLive.String())
	a.AddImm(Rcx, delta)
	a.StoreSymbol(ir.RtLive.String(), Rcx)
}
