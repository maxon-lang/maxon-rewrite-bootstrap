package backend

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/ir"
)

const (
	impGetCommandLineA         = 6
	impCreateFileA             = 7
	impReadFile                = 8
	impGetFileSizeEx           = 9
	impCloseHandle             = 10
	impMoveFileExA             = 11
	impDeleteFileA             = 12
	impFindFirstFileA          = 13
	impFindNextFileA           = 14
	impFindClose               = 15
	impGetFileAttributesA      = 16
	impCreateDirectoryA        = 17
	impGetEnvironmentVariableA = 18
	impCreateProcessA          = 19
	impWaitForSingleObject     = 20
	impGetExitCodeProcess      = 21
	impGetLastError            = 22
	impCreatePipe              = 23
	impSetHandleInformation    = 24
	impPeekNamedPipe           = 25

	winGenericRead             = 0x80000000
	winGenericWrite            = 0x40000000
	winFileShareRead           = 1
	winCreateAlways            = 2
	winOpenExisting            = 3
	winFileAttributeNormal     = 0x80
	winFileAttributeDirectory  = 0x10
	winInvalidHandle           = -1
	winInvalidFileAttributes   = 0xFFFFFFFF
	winMoveFileReplaceExisting = 1
	winInfinite                = 0xFFFFFFFF
	winStdInputHandle          = -10
	winHandleFlagInherit       = 1
	winStartfUseStdHandles     = 0x100
	// winPollMillis is how long a run waits for its child when neither of the child's pipes had anything to read.
	winPollMillis = 1

	winFindDataBytes      = 320
	winFindDataNameOffset = 44
	winStartupInfoBytes   = 104
	winStartupFlagsOffset = 60
	winStartupStdIn       = 80
	winStartupStdOut      = 88
	winStartupStdErr      = 96
	winProcessInfoBytes   = 24
	winProcessThread      = 8
	winSecurityAttrBytes  = 24
	winSecurityInherit    = 16
	winOsCallHomeBytes    = 96

	// errorArg is where an entry that can fail finds the address of its error word: its last argument, the one pushed
	// nearest, above the saved rbp and the return address.
	errorArg = 2 * wordBytes
)

// osArg is where an entry of count arguments finds the one at index: the last pushed is nearest.
func osArg(index, count int) int { return errorArg + (count-1-index)*wordBytes }

// EmitOsRuntime is the unit with the minimal Windows OS and CLI entries. It is linked only into a program that calls one.
func EmitOsRuntime() *CodeUnit {
	u := NewCodeUnit()
	g := &osUnit{
		runtimeUnit:   newRuntimeUnit(u),
		rtArrayNew:    ExternLabel(ir.RtArrayNew.String()),
		rtArrayLen:    ExternLabel(ir.RtArrayLen.String()),
		rtArrayElem:   ExternLabel(ir.RtArrayElem.String()),
		rtArrayInsert: ExternLabel(ir.RtArrayInsert.String()),
		toCStr:        NewLabel(),
		fromCStr:      NewLabel(),
		writeAll:      NewLabel(),
		appendArg:     NewLabel(),
		drainPipe:     NewLabel(),
	}
	g.emitHelpers()
	g.defineEntries()
	return u
}

// osUnit writes the OS unit.
type osUnit struct {
	*runtimeUnit
	rtArrayNew, rtArrayLen, rtArrayElem, rtArrayInsert *Label
	toCStr, fromCStr, writeAll, appendArg, drainPipe   *Label
}

// emitHelpers writes the code that the entries of the unit share.
func (g *osUnit) emitHelpers() {
	a := g.asm
	// Helper: toCStr(Rax = String) -> Rax = heap null-terminated C string. Clobbers Rax, Rcx, Rsi, Rdi, R8.
	a.Bind(g.toCStr)
	a.Push(Rax) // save String
	a.Load(Rcx, Rax, stringLengthOffset)
	a.Lea(Rax, Rcx, 1)
	a.Push(Rax)
	g.callRuntime(g.rtAlloc, 1)
	a.Pop(Rsi) // restore String
	a.Push(Rax)
	a.Mov(Rdi, Rax)
	a.Load(Rcx, Rsi, stringLengthOffset)
	a.AddImm(Rsi, wordBytes)
	a.RepMovsb()
	a.Pop(Rax)
	a.Ret()

	// Helper: fromCStr(Rsi = bytes, Rbx = len) -> Rax = heap String. Clobbers Rax, Rcx, Rsi, Rdi.
	a.Bind(g.fromCStr)
	a.Push(Rsi)
	a.Lea(Rax, Rbx, wordBytes)
	a.Push(Rax)
	g.callRuntime(g.rtAlloc, 1)
	a.Pop(Rsi)
	a.Store(Rax, stringLengthOffset, Rbx)
	a.Push(Rax)
	a.Lea(Rdi, Rax, wordBytes)
	a.Mov(Rcx, Rbx)
	a.RepMovsb()
	a.Pop(Rax)
	a.Ret()

	g.emitWriteAll()
	g.emitAppendArg()
	g.emitDrainPipe()
}

// emitWriteAll writes the helper writeAll(Rbx = path String, R12 = data ptr, R13 = data len) -> Rax = 0, or the Win32
// error that stopped it.
func (g *osUnit) emitWriteAll() {
	a := g.asm
	a.Bind(g.writeAll)
	created := NewLabel()
	writeLoop := NewLabel()
	writeClose := NewLabel()
	writeOk := NewLabel()
	a.Push(Rbp)
	a.Mov(Rbp, Rsp)
	a.AndImm(Rsp, stackAlignMask)
	a.SubImm(Rsp, winOsCallHomeBytes)
	a.Mov(Rax, Rbx)
	a.Call(g.toCStr)
	a.Mov(R14, Rax) // R14 = cPath
	a.Mov(Rcx, R14)
	a.MovImm(Rdx, winGenericWrite)
	a.Xor(R8, R8)
	a.Xor(R9, R9)
	a.MovImm(Rax, winCreateAlways)
	a.Store(Rsp, 32, Rax)
	a.MovImm(Rax, winFileAttributeNormal)
	a.Store(Rsp, 40, Rax)
	a.Xor(Rax, Rax)
	a.Store(Rsp, 48, Rax)
	a.CallImport(impCreateFileA)
	a.Mov(R15, Rax) // R15 = handle
	a.CmpImm(R15, winInvalidHandle)
	a.Jcc(CcNE, created)
	a.CallImport(impGetLastError)
	a.Mov(R15, Rax) // R15 = the error
	a.Push(R14)
	g.callRuntime(g.rtFree, 1)
	a.Mov(Rax, R15)
	g.epilogue()
	a.Bind(created)
	a.Push(R14)
	g.callRuntime(g.rtFree, 1)
	a.Xor(R14, R14) // R14 = the error, none yet
	a.Bind(writeLoop)
	a.Test(R13, R13)
	a.Jcc(CcE, writeClose)
	a.Mov(Rcx, R15)
	a.Mov(Rdx, R12)
	a.Mov(R8, R13)
	a.Lea(R9, Rsp, 80) // &bytesWritten (DWORD at [Rsp+80])
	a.Xor(Rax, Rax)
	a.Store(Rsp, 80, Rax)
	a.Store(Rsp, 32, Rax) // lpOverlapped = 0
	a.CallImport(impWriteFile)
	a.Test(Rax, Rax)
	a.Jcc(CcNE, writeOk)
	a.CallImport(impGetLastError)
	a.Mov(R14, Rax)
	a.Jmp(writeClose)
	a.Bind(writeOk)
	a.Load(Rax, Rsp, 80)
	a.Add(R12, Rax)
	a.Sub(R13, Rax)
	a.Jmp(writeLoop)
	a.Bind(writeClose)
	a.Mov(Rcx, R15)
	a.CallImport(impCloseHandle)
	a.Mov(Rax, R14)
	g.epilogue()
}

// emitAppendArg writes the helper appendArg(Rsi = String, Rdi = cursor) -> Rdi past what it wrote: the argument as
// CommandLineToArgvW reads it back, Go's syscall.EscapeArg. An argument with a space or a tab is quoted; a quote is
// escaped with a backslash, after doubling the backslashes before it, and so are the backslashes before the closing
// quote. Writes at most twice the argument's length and 2 bytes. Clobbers Rax, Rcx, Rdx, R8, R9, R10, Rsi.
func (g *osUnit) emitAppendArg() {
	a := g.asm
	a.Bind(g.appendArg)
	nonEmpty := NewLabel()
	scan := NewLabel()
	quoted := NewLabel()
	scanned := NewLabel()
	noOpen := NewLabel()
	each := NewLabel()
	notSlash := NewLabel()
	notQuote := NewLabel()
	put := NewLabel()
	slashes := NewLabel()
	slashesDone := NewLabel()
	done := NewLabel()
	a.Load(Rcx, Rsi, stringLengthOffset) // Rcx = length
	a.AddImm(Rsi, wordBytes)             // Rsi = bytes
	a.Test(Rcx, Rcx)
	a.Jcc(CcNE, nonEmpty)
	g.putByte('"')
	g.putByte('"')
	a.Ret()
	a.Bind(nonEmpty)
	a.Xor(R8, R8) // R8 = whether it is quoted
	a.Xor(Rdx, Rdx)
	a.Bind(scan)
	a.Cmp(Rdx, Rcx)
	a.Jcc(CcE, scanned)
	a.Mov(R9, Rsi)
	a.Add(R9, Rdx)
	a.LoadByte(Rax, R9, 0)
	a.AddImm(Rdx, 1)
	a.CmpImm(Rax, ' ')
	a.Jcc(CcE, quoted)
	a.CmpImm(Rax, '\t')
	a.Jcc(CcNE, scan)
	a.Bind(quoted)
	a.MovImm(R8, 1)
	a.Bind(scanned)
	a.Test(R8, R8)
	a.Jcc(CcE, noOpen)
	g.putByte('"')
	a.Bind(noOpen)
	a.Xor(Rdx, Rdx) // Rdx = index
	a.Xor(R10, R10) // R10 = backslashes just written
	a.Bind(each)
	a.Cmp(Rdx, Rcx)
	a.Jcc(CcE, done)
	a.Mov(R9, Rsi)
	a.Add(R9, Rdx)
	a.LoadByte(Rax, R9, 0)
	a.AddImm(Rdx, 1)
	a.CmpImm(Rax, '\\')
	a.Jcc(CcNE, notSlash)
	a.AddImm(R10, 1)
	a.Jmp(put)
	a.Bind(notSlash)
	a.CmpImm(Rax, '"')
	a.Jcc(CcNE, notQuote)
	// The backslashes before a quote are doubled, and one more escapes the quote.
	a.Mov(R9, R10)
	a.AddImm(R9, 1)
	g.putBackslashes(R9)
	a.Bind(notQuote)
	a.Xor(R10, R10)
	a.Bind(put)
	a.StoreByte(Rdi, 0, Rax)
	a.AddImm(Rdi, 1)
	a.Jmp(each)
	a.Bind(done)
	a.Test(R8, R8)
	a.Jcc(CcE, slashesDone)
	a.Bind(slashes)
	g.putBackslashes(R10)
	g.putByte('"')
	a.Bind(slashesDone)
	a.Ret()
}

// putByte writes a byte at Rdi and moves past it. Clobbers R9.
func (g *osUnit) putByte(b byte) {
	a := g.asm
	a.MovImm(R9, int64(b))
	a.StoreByte(Rdi, 0, R9)
	a.AddImm(Rdi, 1)
}

// putBackslashes writes as many backslashes as count holds at Rdi, counting it down to 0, and moves past them.
// Clobbers R11.
func (g *osUnit) putBackslashes(count R) {
	a := g.asm
	loop := NewLabel()
	end := NewLabel()
	a.Bind(loop)
	a.Test(count, count)
	a.Jcc(CcE, end)
	a.MovImm(R11, '\\')
	a.StoreByte(Rdi, 0, R11)
	a.AddImm(Rdi, 1)
	a.AddImm(count, -1)
	a.Jmp(loop)
	a.Bind(end)
}

// The state of one of a child's pipes in a run's frame, which drainPipe is given the address of.
const (
	pipeHandle = 0             // the read end
	pipeText   = wordBytes     // the String read so far
	pipeOpen   = 2 * wordBytes // whether the child may still write to it
	pipeAvail  = 3 * wordBytes // a DWORD PeekNamedPipe sets
	pipeRead   = 4 * wordBytes // a DWORD ReadFile sets
	pipeBytes  = 5 * wordBytes
)

// emitDrainPipe writes the helper drainPipe(Rbx = the address of a pipe's state) -> Rax = whether it read anything. It
// reads only what the pipe holds, so it never waits, and the child cannot block on one pipe while the run reads the
// other. A pipe whose writer is gone is no longer open. Clobbers everything but Rbx and Rbp.
func (g *osUnit) emitDrainPipe() {
	a := g.asm
	a.Bind(g.drainPipe)
	nothing := NewLabel()
	closed := NewLabel()
	g.startWinFrame(0)
	a.Load(Rax, Rbx, pipeOpen)
	a.Test(Rax, Rax)
	a.Jcc(CcE, nothing)
	a.Xor(Rax, Rax)
	a.Store(Rbx, pipeAvail, Rax)
	a.Load(Rcx, Rbx, pipeHandle)
	a.Xor(Rdx, Rdx)
	a.Xor(R8, R8)
	a.Xor(R9, R9)
	a.Lea(Rax, Rbx, pipeAvail)
	a.Store(Rsp, 32, Rax)
	a.Xor(Rax, Rax)
	a.Store(Rsp, 40, Rax)
	a.CallImport(impPeekNamedPipe)
	a.Test(Rax, Rax)
	a.Jcc(CcE, closed)
	a.Load(R12, Rbx, pipeAvail) // R12 = bytes to read
	a.Test(R12, R12)
	a.Jcc(CcE, nothing)
	a.Load(R13, Rbx, pipeText)           // R13 = the String so far
	a.Load(R14, R13, stringLengthOffset) // R14 = its length
	a.Lea(Rax, R14, wordBytes)
	a.Add(Rax, R12)
	a.Push(Rax)
	g.callRuntime(g.rtAlloc, 1)
	a.Mov(R15, Rax) // R15 = the longer String
	a.Lea(Rsi, R13, wordBytes)
	a.Lea(Rdi, R15, wordBytes)
	a.Mov(Rcx, R14)
	a.RepMovsb()
	a.Push(R13)
	g.callRuntime(g.rtFree, 1)
	a.Store(Rbx, pipeText, R15)
	a.Xor(Rax, Rax)
	a.Store(Rbx, pipeRead, Rax)
	a.Load(Rcx, Rbx, pipeHandle)
	a.Lea(Rdx, R15, wordBytes)
	a.Add(Rdx, R14)
	a.Mov(R8, R12)
	a.Lea(R9, Rbx, pipeRead)
	a.Store(Rsp, 32, Rax) // lpOverlapped = 0
	a.CallImport(impReadFile)
	a.Load(Rax, Rbx, pipeRead)
	a.Add(Rax, R14)
	a.Store(R15, stringLengthOffset, Rax)
	a.MovImm(Rax, 1)
	g.epilogue()
	a.Bind(closed)
	a.Xor(Rax, Rax)
	a.Store(Rbx, pipeOpen, Rax)
	a.Bind(nothing)
	a.Xor(Rax, Rax)
	g.epilogue()
}

// startWinFrame reserves locals, aligns the stack and reserves winOsCallHomeBytes.
func (g *osUnit) startWinFrame(locals int) {
	a := g.asm
	a.Push(Rbp)
	a.Mov(Rbp, Rsp)
	if locals > 0 {
		a.SubImm(Rsp, locals)
	}
	a.AndImm(Rsp, stackAlignMask)
	a.SubImm(Rsp, winOsCallHomeBytes)
}

// succeed ends an entry that can fail and did not: it clears the error word and returns rax. Clobbers Rcx, Rdx.
func (g *osUnit) succeed() {
	a := g.asm
	a.Xor(Rdx, Rdx)
	a.Load(Rcx, Rbp, errorArg)
	a.Store(Rcx, 0, Rdx)
	g.epilogue()
}

// failWith ends an entry that failed with the case of its error.
func (g *osUnit) failWith(errorCase int) {
	a := g.asm
	a.MovImm(Rdx, int64(errorCase+1))
	a.Load(Rcx, Rbp, errorArg)
	a.Store(Rcx, 0, Rdx)
	a.Xor(Rax, Rax)
	g.epilogue()
}

// failWithError ends an entry that failed with the Win32 error in rax, as the case of its error that the failure is.
func (g *osUnit) failWithError(cases ir.FailureCases) {
	a := g.asm
	var labels [ir.OsFailureCount]*Label
	for f := range labels {
		labels[f] = NewLabel()
	}
	for _, w := range ir.Win32Failures {
		a.CmpImm(Rax, int(w.Code))
		a.Jcc(CcE, labels[w.Failure])
	}
	a.Jmp(labels[ir.FailureOther])
	for f, l := range labels {
		a.Bind(l)
		g.failWith(cases[f])
	}
}

// failWithLastError ends an entry whose Windows call just failed.
func (g *osUnit) failWithLastError(cases ir.FailureCases) {
	g.asm.CallImport(impGetLastError)
	g.failWithError(cases)
}

// rt.os.args() -> Array with String
func (g *osUnit) emitOsArgs() {
	a := g.asm
	g.startWinFrame(16) // [Rbp-8] = array, [Rbp-16] = cursor
	cliSkipWs := NewLabel()
	cliDone := NewLabel()
	cliQuoted := NewLabel()
	cliUnquoted := NewLabel()
	cliQuotedEnd := NewLabel()
	cliPush := NewLabel()
	g.callRuntime(g.rtArrayNew, 0)
	a.Store(Rbp, -8, Rax)
	a.CallImport(impGetCommandLineA)
	a.Store(Rbp, -16, Rax)
	a.Bind(cliSkipWs)
	a.Load(R13, Rbp, -16)
	a.LoadByte(Rax, R13, 0)
	a.Test(Rax, Rax)
	a.Jcc(CcE, cliDone)
	a.CmpImm(Rax, ' ')
	cliCheckTab := NewLabel()
	a.Jcc(CcNE, cliCheckTab)
	a.AddImm(R13, 1)
	a.Store(Rbp, -16, R13)
	a.Jmp(cliSkipWs)
	a.Bind(cliCheckTab)
	a.CmpImm(Rax, '\t')
	cliTokenStart := NewLabel()
	a.Jcc(CcNE, cliTokenStart)
	a.AddImm(R13, 1)
	a.Store(Rbp, -16, R13)
	a.Jmp(cliSkipWs)
	a.Bind(cliTokenStart)
	a.CmpImm(Rax, '"')
	a.Jcc(CcNE, cliUnquoted)
	a.AddImm(R13, 1)
	a.Mov(R14, R13) // start of token
	a.Bind(cliQuoted)
	a.LoadByte(Rax, R13, 0)
	a.Test(Rax, Rax)
	a.Jcc(CcE, cliQuotedEnd)
	a.CmpImm(Rax, '"')
	a.Jcc(CcE, cliQuotedEnd)
	a.AddImm(R13, 1)
	a.Jmp(cliQuoted)
	a.Bind(cliQuotedEnd)
	a.Mov(Rbx, R13)
	a.Sub(Rbx, R14) // length
	a.LoadByte(Rax, R13, 0)
	a.CmpImm(Rax, '"')
	a.Jcc(CcNE, cliPush)
	a.AddImm(R13, 1)
	a.Jmp(cliPush)
	a.Bind(cliUnquoted)
	a.Mov(R14, R13) // start of token
	cliUnqLoop := NewLabel()
	a.Bind(cliUnqLoop)
	a.LoadByte(Rax, R13, 0)
	a.Test(Rax, Rax)
	cliUnqEnd := NewLabel()
	a.Jcc(CcE, cliUnqEnd)
	a.CmpImm(Rax, ' ')
	a.Jcc(CcE, cliUnqEnd)
	a.CmpImm(Rax, '\t')
	a.Jcc(CcE, cliUnqEnd)
	a.AddImm(R13, 1)
	a.Jmp(cliUnqLoop)
	a.Bind(cliUnqEnd)
	a.Mov(Rbx, R13)
	a.Sub(Rbx, R14)
	a.Bind(cliPush)
	a.Store(Rbp, -16, R13)
	a.Mov(Rsi, R14)
	a.Call(g.fromCStr)
	a.Mov(R15, Rax) // R15 = arg string
	a.Load(R12, Rbp, -8)
	g.pushString(R12, R15)
	a.Jmp(cliSkipWs)
	a.Bind(cliDone)
	a.Load(Rax, Rbp, -8)
	g.epilogue()
}

// pushString moves the String in text to the end of the array in array. Clobbers what an array entry does.
func (g *osUnit) pushString(array, text R) {
	a := g.asm
	a.Push(array)
	g.callRuntime(g.rtArrayLen, 1)
	a.Push(array)
	a.Push(Rax)
	a.Push(text)
	g.callRuntime(g.rtArrayInsert, 3)
}

// rt.os.environmentVariable(name String, error) -> String
func (g *osUnit) emitOsEnvironmentVariable() {
	a := g.asm
	g.startWinFrame(0)
	envMiss := NewLabel()
	a.Load(Rax, Rbp, osArg(0, 2))
	a.Call(g.toCStr)
	a.Mov(R12, Rax) // R12 = cName
	a.Mov(Rcx, R12)
	a.Xor(Rdx, Rdx)
	a.Xor(R8, R8)
	// The size it asks for counts the terminating 0, so only a variable that is not set asks for none.
	a.CallImport(impGetEnvironmentVariableA)
	a.Test(Rax, Rax)
	a.Jcc(CcE, envMiss)
	a.Mov(R13, Rax) // R13 = required buffer size (includes null)
	a.Lea(Rax, R13, wordBytes)
	a.Push(Rax)
	g.callRuntime(g.rtAlloc, 1)
	a.Mov(R14, Rax) // R14 = String box
	a.Mov(Rcx, R12)
	a.Lea(Rdx, R14, wordBytes)
	a.Mov(R8, R13)
	a.CallImport(impGetEnvironmentVariableA)
	a.Store(R14, stringLengthOffset, Rax)
	a.Push(R12)
	g.callRuntime(g.rtFree, 1)
	a.Mov(Rax, R14)
	g.succeed()
	a.Bind(envMiss)
	a.Push(R12)
	g.callRuntime(g.rtFree, 1)
	g.failWith(ast.ErrorVariableUnset)
}

// rt.os.pathFrom(path String, error) -> String
func (g *osUnit) emitOsPathFrom() {
	a := g.asm
	g.startWinFrame(0)
	scan := NewLabel()
	scanned := NewLabel()
	invalid := NewLabel()
	copyLoop := NewLabel()
	keep := NewLabel()
	copied := NewLabel()
	a.Load(R12, Rbp, osArg(0, 2))        // R12 = path
	a.Load(R13, R12, stringLengthOffset) // R13 = length
	a.Xor(Rbx, Rbx)
	a.Bind(scan)
	a.Cmp(Rbx, R13)
	a.Jcc(CcE, scanned)
	a.Lea(Rax, R12, wordBytes)
	a.Add(Rax, Rbx)
	a.LoadByte(Rax, Rax, 0)
	a.AddImm(Rbx, 1)
	a.CmpImm(Rax, ir.LastControlByte)
	a.Jcc(CcBE, invalid)
	for i := range len(ir.InvalidPathBytes) {
		a.CmpImm(Rax, int(ir.InvalidPathBytes[i]))
		a.Jcc(CcE, invalid)
	}
	a.Jmp(scan)
	a.Bind(scanned)
	a.Lea(Rax, R13, wordBytes)
	a.Push(Rax)
	g.callRuntime(g.rtAlloc, 1)
	a.Mov(R14, Rax) // R14 = the native path
	a.Store(R14, stringLengthOffset, R13)
	a.Xor(Rbx, Rbx)
	a.Bind(copyLoop)
	a.Cmp(Rbx, R13)
	a.Jcc(CcE, copied)
	a.Lea(Rcx, R12, wordBytes)
	a.Add(Rcx, Rbx)
	a.LoadByte(Rax, Rcx, 0)
	a.CmpImm(Rax, ir.OtherSeparator)
	a.Jcc(CcNE, keep)
	a.MovImm(Rax, ir.PathSeparator)
	a.Bind(keep)
	a.Lea(Rcx, R14, wordBytes)
	a.Add(Rcx, Rbx)
	a.StoreByte(Rcx, 0, Rax)
	a.AddImm(Rbx, 1)
	a.Jmp(copyLoop)
	a.Bind(copied)
	a.Mov(Rax, R14)
	g.succeed()
	a.Bind(invalid)
	g.failWith(ast.ErrorPathInvalidCharacter)
}

// rt.os.readText(path String, error) -> String
func (g *osUnit) emitOsReadText() {
	a := g.asm
	const (
		offFileSize  = -8
		offBytesRead = -16
		offError     = -24
		locals       = 24
	)
	g.startWinFrame(locals)
	opened := NewLabel()
	readFail := NewLabel()
	readCloseFail := NewLabel()
	readLoop := NewLabel()
	readDone := NewLabel()
	a.Load(Rax, Rbp, osArg(0, 2))
	a.Call(g.toCStr)
	a.Mov(R12, Rax) // R12 = cPath
	a.Mov(Rcx, R12)
	a.MovImm(Rdx, winGenericRead)
	a.MovImm(R8, winFileShareRead)
	a.Xor(R9, R9)
	a.MovImm(Rax, winOpenExisting)
	a.Store(Rsp, 32, Rax)
	a.MovImm(Rax, winFileAttributeNormal)
	a.Store(Rsp, 40, Rax)
	a.Xor(Rax, Rax)
	a.Store(Rsp, 48, Rax)
	a.CallImport(impCreateFileA)
	a.Mov(R13, Rax) // R13 = handle
	a.CmpImm(R13, winInvalidHandle)
	a.Jcc(CcNE, opened)
	a.CallImport(impGetLastError)
	a.Store(Rbp, offError, Rax)
	a.Push(R12)
	g.callRuntime(g.rtFree, 1)
	a.Load(Rax, Rbp, offError)
	g.failWithError(ir.ReadFailures)
	a.Bind(opened)
	a.Push(R12)
	g.callRuntime(g.rtFree, 1)
	a.Mov(Rcx, R13)
	a.Lea(Rdx, Rbp, offFileSize)
	a.CallImport(impGetFileSizeEx)
	a.Test(Rax, Rax)
	a.Jcc(CcE, readCloseFail)
	a.Load(R14, Rbp, offFileSize) // R14 = fileSize
	a.Lea(Rax, R14, wordBytes)
	a.Push(Rax)
	g.callRuntime(g.rtAlloc, 1)
	a.Mov(R15, Rax) // R15 = String box
	a.Store(R15, stringLengthOffset, R14)
	a.Lea(Rbx, R15, wordBytes) // Rbx = dst cursor
	a.Mov(R12, R14)            // R12 = remaining
	a.Bind(readLoop)
	a.Test(R12, R12)
	a.Jcc(CcE, readDone)
	a.Mov(Rcx, R13)
	a.Mov(Rdx, Rbx)
	a.Mov(R8, R12)
	a.Lea(R9, Rbp, offBytesRead)
	a.Xor(Rax, Rax)
	a.Store(Rbp, offBytesRead, Rax)
	a.Store(Rsp, 32, Rax)
	a.CallImport(impReadFile)
	a.Test(Rax, Rax)
	a.Jcc(CcE, readFail)
	a.Load(Rax, Rbp, offBytesRead)
	a.Test(Rax, Rax)
	a.Jcc(CcE, readDone)
	a.Add(Rbx, Rax)
	a.Sub(R12, Rax)
	a.Jmp(readLoop)
	a.Bind(readDone)
	// A file that shrank while it was read holds what was read.
	a.Sub(R14, R12)
	a.Store(R15, stringLengthOffset, R14)
	a.Mov(Rcx, R13)
	a.CallImport(impCloseHandle)
	a.Mov(Rax, R15)
	g.succeed()
	a.Bind(readFail)
	a.CallImport(impGetLastError)
	a.Store(Rbp, offError, Rax)
	a.Push(R15)
	g.callRuntime(g.rtFree, 1)
	g.closeAndFail(offError)
	a.Bind(readCloseFail)
	a.CallImport(impGetLastError)
	a.Store(Rbp, offError, Rax)
	g.closeAndFail(offError)
}

// closeAndFail closes the handle in R13 and fails with the Win32 error saved at offError, as a read does.
func (g *osUnit) closeAndFail(offError int) {
	a := g.asm
	a.Mov(Rcx, R13)
	a.CallImport(impCloseHandle)
	a.Load(Rax, Rbp, offError)
	g.failWithError(ir.ReadFailures)
}

// writeEntry ends a write that writeAll did: it fails with the Win32 error it gives, if any.
func (g *osUnit) writeEntry() {
	a := g.asm
	failed := NewLabel()
	a.Call(g.writeAll)
	a.Test(Rax, Rax)
	a.Jcc(CcNE, failed)
	g.succeed()
	a.Bind(failed)
	g.failWithError(ir.WriteFailures)
}

// rt.os.writeText(path String, content String, error)
func (g *osUnit) emitOsWriteText() {
	a := g.asm
	g.startWinFrame(0)
	a.Load(Rbx, Rbp, osArg(0, 3))
	a.Load(Rax, Rbp, osArg(1, 3))
	a.Load(R13, Rax, stringLengthOffset)
	a.Lea(R12, Rax, wordBytes)
	g.writeEntry()
}

// rt.os.writeBinary(path String, content ByteArray, error)
func (g *osUnit) emitOsWriteBinary() {
	a := g.asm
	g.startWinFrame(0)
	a.Load(Rbx, Rbp, osArg(0, 3))
	a.Load(Rax, Rbp, osArg(1, 3))
	a.Load(R13, Rax, arrayLengthOffset)
	a.Load(R12, Rax, arrayDataOffset)
	g.writeEntry()
}

// rt.os.rename(from String, to String, error)
func (g *osUnit) emitOsRename() {
	a := g.asm
	g.startWinFrame(0)
	failed := NewLabel()
	a.Load(Rax, Rbp, osArg(0, 3))
	a.Call(g.toCStr)
	a.Mov(R12, Rax)
	a.Load(Rax, Rbp, osArg(1, 3))
	a.Call(g.toCStr)
	a.Mov(R13, Rax)
	a.Mov(Rcx, R12)
	a.Mov(Rdx, R13)
	a.MovImm(R8, winMoveFileReplaceExisting)
	a.CallImport(impMoveFileExA)
	a.Xor(R14, R14) // R14 = the error, none
	a.Test(Rax, Rax)
	a.Jcc(CcNE, failed)
	a.CallImport(impGetLastError)
	a.Mov(R14, Rax)
	a.Bind(failed)
	a.Push(R12)
	g.callRuntime(g.rtFree, 1)
	a.Push(R13)
	g.callRuntime(g.rtFree, 1)
	g.endWithError(R14, ir.RenameFailures)
}

// endWithError ends an entry with the Win32 error in err: it failed when that is not 0.
func (g *osUnit) endWithError(err R, cases ir.FailureCases) {
	a := g.asm
	failed := NewLabel()
	a.Test(err, err)
	a.Jcc(CcNE, failed)
	g.succeed()
	a.Bind(failed)
	a.Mov(Rax, err)
	g.failWithError(cases)
}

// rt.os.delete(path String, error)
func (g *osUnit) emitOsDelete() {
	a := g.asm
	g.startWinFrame(0)
	deleted := NewLabel()
	a.Load(Rax, Rbp, osArg(0, 2))
	a.Call(g.toCStr)
	a.Mov(R12, Rax)
	a.Mov(Rcx, R12)
	a.CallImport(impDeleteFileA)
	a.Xor(R13, R13) // R13 = the error, none
	a.Test(Rax, Rax)
	a.Jcc(CcNE, deleted)
	a.CallImport(impGetLastError)
	a.Mov(R13, Rax)
	a.Bind(deleted)
	a.Push(R12)
	g.callRuntime(g.rtFree, 1)
	g.endWithError(R13, ir.DeleteFailures)
}

// isDirectoryAt sets Rax to whether the C string in R12 names a directory.
func (g *osUnit) isDirectoryAt() {
	a := g.asm
	missing := NewLabel()
	done := NewLabel()
	a.Mov(Rcx, R12)
	a.CallImport(impGetFileAttributesA)
	a.MovImm(Rcx, winInvalidFileAttributes)
	a.Cmp(Rax, Rcx)
	a.Jcc(CcE, missing)
	a.AndImm(Rax, winFileAttributeDirectory)
	a.Test(Rax, Rax)
	a.SetccRax(CcNE)
	a.Jmp(done)
	a.Bind(missing)
	a.Xor(Rax, Rax)
	a.Bind(done)
}

// rt.os.isDirectory(path String) -> bool
func (g *osUnit) emitOsIsDirectory() {
	a := g.asm
	g.startWinFrame(0)
	a.Load(Rax, Rbp, osArg(0, 1))
	a.Call(g.toCStr)
	a.Mov(R12, Rax)
	g.isDirectoryAt()
	a.Mov(R13, Rax)
	a.Push(R12)
	g.callRuntime(g.rtFree, 1)
	a.Mov(Rax, R13)
	g.epilogue()
}

// rt.os.createDirectory(path String) -> bool: each prefix that ends before a separator is created first, as maxon's
// create does for each missing parent; one that is there already fails, which is no matter.
func (g *osUnit) emitOsCreateDirectory() {
	a := g.asm
	g.startWinFrame(0)
	scan := NewLabel()
	notSeparator := NewLabel()
	next := NewLabel()
	scanned := NewLabel()
	a.Load(Rax, Rbp, osArg(0, 1))
	a.Load(R13, Rax, stringLengthOffset) // R13 = length
	a.Call(g.toCStr)
	a.Mov(R12, Rax) // R12 = cPath, which the loop cuts at each separator in turn
	a.MovImm(Rbx, 1)
	a.Bind(scan)
	a.Cmp(Rbx, R13)
	a.Jcc(CcAE, scanned)
	a.Mov(R14, R12)
	a.Add(R14, Rbx) // R14 = the byte
	a.LoadByte(R15, R14, 0)
	a.CmpImm(R15, ir.PathSeparator)
	a.Jcc(CcE, notSeparator)
	a.CmpImm(R15, ir.OtherSeparator)
	a.Jcc(CcNE, next)
	a.Bind(notSeparator)
	a.Xor(Rax, Rax)
	a.StoreByte(R14, 0, Rax)
	a.Mov(Rcx, R12)
	a.Xor(Rdx, Rdx)
	a.CallImport(impCreateDirectoryA)
	a.StoreByte(R14, 0, R15)
	a.Bind(next)
	a.AddImm(Rbx, 1)
	a.Jmp(scan)
	a.Bind(scanned)
	a.Mov(Rcx, R12)
	a.Xor(Rdx, Rdx)
	a.CallImport(impCreateDirectoryA)
	g.isDirectoryAt()
	a.Mov(R13, Rax)
	a.Push(R12)
	g.callRuntime(g.rtFree, 1)
	a.Mov(Rax, R13)
	g.epilogue()
}

// rt.os.list(path String, error) -> Array with String
func (g *osUnit) emitOsList() {
	a := g.asm
	const (
		listOffArray  = -8
		listOffHandle = -16
		listOffPath   = -24
		listOffSep    = -32 // whether a separator goes between the path and a name
		listOffFind   = -32 - winFindDataBytes
	)
	g.startWinFrame(32 + winFindDataBytes)
	found := NewLabel()
	listLoop := NewLabel()
	listNext := NewLabel()
	noSeparator := NewLabel()
	sepKnown := NewLabel()
	// Build "<path>\\*\0"
	a.Load(R14, Rbp, osArg(0, 2)) // R14 = path String
	a.Store(Rbp, listOffPath, R14)
	a.Load(R13, R14, stringLengthOffset)
	a.Lea(Rax, R13, 3) // + "\\*\0"
	a.Push(Rax)
	g.callRuntime(g.rtAlloc, 1)
	a.Mov(R12, Rax) // R12 = pattern C string
	a.Mov(Rdi, R12)
	a.Lea(Rsi, R14, wordBytes)
	a.Mov(Rcx, R13)
	a.RepMovsb()
	a.Mov(Rax, R12)
	a.Add(Rax, R13)
	a.MovImm(Rcx, ir.PathSeparator)
	a.StoreByte(Rax, 0, Rcx)
	a.MovImm(Rcx, '*')
	a.StoreByte(Rax, 1, Rcx)
	// maxon's FilePath.join puts no separator after an empty path or one that ends with one.
	a.Xor(Rax, Rax)
	a.Store(Rbp, listOffSep, Rax)
	a.Test(R13, R13)
	a.Jcc(CcE, sepKnown)
	a.Lea(Rax, R14, wordBytes-1)
	a.Add(Rax, R13)
	a.LoadByte(Rax, Rax, 0)
	a.CmpImm(Rax, ir.PathSeparator)
	a.Jcc(CcE, sepKnown)
	a.MovImm(Rax, 1)
	a.Store(Rbp, listOffSep, Rax)
	a.Bind(sepKnown)
	a.Mov(Rcx, R12)
	a.Lea(Rdx, Rbp, listOffFind)
	a.CallImport(impFindFirstFileA)
	a.Store(Rbp, listOffHandle, Rax)
	a.CmpImm(Rax, winInvalidHandle)
	a.Jcc(CcNE, found)
	a.CallImport(impGetLastError)
	a.Mov(R13, Rax)
	a.Push(R12)
	g.callRuntime(g.rtFree, 1)
	a.Mov(Rax, R13)
	g.failWithError(ir.ListFailures)
	a.Bind(found)
	a.Push(R12)
	g.callRuntime(g.rtFree, 1)
	g.callRuntime(g.rtArrayNew, 0)
	a.Store(Rbp, listOffArray, Rax)
	a.Bind(listLoop)
	// Measure cFileName length at [Rbp + listOffFind + winFindDataNameOffset]
	a.Lea(R14, Rbp, listOffFind+winFindDataNameOffset)
	a.Xor(Rbx, Rbx)
	listStrlen := NewLabel()
	listStrlenDone := NewLabel()
	a.Bind(listStrlen)
	a.Mov(Rax, R14)
	a.Add(Rax, Rbx)
	a.LoadByte(Rcx, Rax, 0)
	a.Test(Rcx, Rcx)
	a.Jcc(CcE, listStrlenDone)
	a.AddImm(Rbx, 1)
	a.Jmp(listStrlen)
	a.Bind(listStrlenDone)
	// Skip "." and ".."
	listKeep := NewLabel()
	a.LoadByte(Rax, R14, 0)
	a.CmpImm(Rax, '.')
	a.Jcc(CcNE, listKeep)
	a.CmpImm(Rbx, 1)
	a.Jcc(CcE, listNext)
	a.CmpImm(Rbx, 2)
	a.Jcc(CcNE, listKeep)
	a.LoadByte(Rax, R14, 1)
	a.CmpImm(Rax, '.')
	a.Jcc(CcE, listNext)
	a.Bind(listKeep)
	// The String of the path, the separator and the name.
	a.Load(R13, Rbp, listOffPath)
	a.Load(R12, R13, stringLengthOffset) // R12 = the path's length
	a.Lea(Rax, R12, wordBytes)
	a.Add(Rax, Rbx)
	a.Load(Rcx, Rbp, listOffSep)
	a.Add(Rax, Rcx)
	a.Push(Rax)
	g.callRuntime(g.rtAlloc, 1)
	a.Mov(R15, Rax) // R15 = the entry's path
	a.Mov(Rax, R12)
	a.Add(Rax, Rbx)
	a.Load(Rcx, Rbp, listOffSep)
	a.Add(Rax, Rcx)
	a.Store(R15, stringLengthOffset, Rax)
	a.Lea(Rdi, R15, wordBytes)
	a.Lea(Rsi, R13, wordBytes)
	a.Mov(Rcx, R12)
	a.RepMovsb()
	a.Load(Rcx, Rbp, listOffSep)
	a.Test(Rcx, Rcx)
	a.Jcc(CcE, noSeparator)
	g.putByte(ir.PathSeparator)
	a.Bind(noSeparator)
	a.Mov(Rsi, R14)
	a.Mov(Rcx, Rbx)
	a.RepMovsb()
	a.Load(R12, Rbp, listOffArray)
	g.pushString(R12, R15)
	a.Bind(listNext)
	a.Load(Rcx, Rbp, listOffHandle)
	a.Lea(Rdx, Rbp, listOffFind)
	a.CallImport(impFindNextFileA)
	a.Test(Rax, Rax)
	a.Jcc(CcNE, listLoop)
	a.Load(Rcx, Rbp, listOffHandle)
	a.CallImport(impFindClose)
	a.Load(Rax, Rbp, listOffArray)
	g.succeed()
}

// rt.os.run(executable String, arguments Array with String, stdout, stderr, error) -> exit code. The child's standard
// output and error are pipes, which the run drains in turn (drainPipe) until the child has closed both, so a child that
// fills one cannot block while the run waits on the other; its standard input is the run's own.
func (g *osUnit) emitOsRun() {
	a := g.asm
	const (
		runArgs     = 5
		offOut      = -pipeBytes
		offErr      = offOut - pipeBytes
		offOutWrite = offErr - wordBytes
		offErrWrite = offOutWrite - wordBytes
		offCmd      = offErrWrite - wordBytes
		offCursor   = offCmd - wordBytes
		offIndex    = offCursor - wordBytes
		offMoved    = offIndex - wordBytes
		offExitCode = offMoved - wordBytes
		offError    = offExitCode - wordBytes
		offSecurity = offError - winSecurityAttrBytes
		offProcess  = offSecurity - winProcessInfoBytes
		offStartup  = offProcess - winStartupInfoBytes
		runLocals   = -offStartup
	)
	g.startWinFrame(runLocals)
	sizeLoop := NewLabel()
	sized := NewLabel()
	argLoop := NewLabel()
	built := NewLabel()
	started := NewLabel()
	drain := NewLabel()
	drained := NewLabel()
	pipeFailed := NewLabel()
	a.Lea(Rdi, Rbp, offStartup)
	a.Xor(Rax, Rax)
	a.MovImm(Rcx, runLocals/wordBytes)
	a.RepStosq()

	// The command line: each String quoted, with a space between them, in a buffer long enough for the worst quoting.
	a.Load(R12, Rbp, osArg(0, runArgs)) // R12 = executable
	a.Load(R13, Rbp, osArg(1, runArgs)) // R13 = arguments
	a.Load(R14, R12, stringLengthOffset)
	a.Add(R14, R14)
	a.AddImm(R14, 3) // R14 = the length, and the terminating 0
	a.Push(R13)
	g.callRuntime(g.rtArrayLen, 1)
	a.Mov(R15, Rax) // R15 = the count of arguments
	a.Xor(Rax, Rax)
	a.Store(Rbp, offIndex, Rax)
	a.Bind(sizeLoop)
	a.Load(Rbx, Rbp, offIndex)
	a.Cmp(Rbx, R15)
	a.Jcc(CcE, sized)
	a.Push(R13)
	a.Push(Rbx)
	g.callRuntime(g.rtArrayElem, 2)
	a.Load(Rax, Rax, 0)
	a.Load(Rax, Rax, stringLengthOffset)
	a.Add(Rax, Rax)
	a.AddImm(Rax, 3)
	a.Add(R14, Rax)
	a.Load(Rbx, Rbp, offIndex)
	a.AddImm(Rbx, 1)
	a.Store(Rbp, offIndex, Rbx)
	a.Jmp(sizeLoop)
	a.Bind(sized)
	a.Push(R14)
	g.callRuntime(g.rtAlloc, 1)
	a.Store(Rbp, offCmd, Rax)
	a.Mov(Rdi, Rax)
	a.Mov(Rsi, R12)
	a.Call(g.appendArg)
	a.Store(Rbp, offCursor, Rdi)
	a.Xor(Rax, Rax)
	a.Store(Rbp, offIndex, Rax)
	a.Bind(argLoop)
	a.Load(Rbx, Rbp, offIndex)
	a.Cmp(Rbx, R15)
	a.Jcc(CcE, built)
	a.Push(R13)
	a.Push(Rbx)
	g.callRuntime(g.rtArrayElem, 2)
	a.Load(Rsi, Rax, 0)
	a.Load(Rdi, Rbp, offCursor)
	g.putByte(' ')
	a.Call(g.appendArg)
	a.Store(Rbp, offCursor, Rdi)
	a.Load(Rbx, Rbp, offIndex)
	a.AddImm(Rbx, 1)
	a.Store(Rbp, offIndex, Rbx)
	a.Jmp(argLoop)
	a.Bind(built)

	// What each pipe has read so far: nothing, an empty String.
	for _, pipe := range []int{offOut, offErr} {
		a.MovImm(Rax, wordBytes)
		a.Push(Rax)
		g.callRuntime(g.rtAlloc, 1)
		a.Store(Rbp, pipe+pipeText, Rax)
	}
	// The pipes, whose write ends the child inherits and whose read ends it does not.
	a.MovImm(Rax, winSecurityAttrBytes)
	a.Store(Rbp, offSecurity, Rax)
	a.MovImm(Rax, 1)
	a.Store(Rbp, offSecurity+winSecurityInherit, Rax)
	for _, pipe := range [][2]int{{offOut, offOutWrite}, {offErr, offErrWrite}} {
		a.Lea(Rcx, Rbp, pipe[0]+pipeHandle)
		a.Lea(Rdx, Rbp, pipe[1])
		a.Lea(R8, Rbp, offSecurity)
		a.Xor(R9, R9)
		a.CallImport(impCreatePipe)
		a.Test(Rax, Rax)
		a.Jcc(CcE, pipeFailed)
		a.Load(Rcx, Rbp, pipe[0]+pipeHandle)
		a.MovImm(Rdx, winHandleFlagInherit)
		a.Xor(R8, R8)
		a.CallImport(impSetHandleInformation)
	}
	a.MovImm(Rax, winStartupInfoBytes)
	a.Store(Rbp, offStartup, Rax)
	a.MovImm(Rax, winStartfUseStdHandles)
	a.Store(Rbp, offStartup+winStartupFlagsOffset, Rax)
	a.MovImm(Rcx, winStdInputHandle)
	a.CallImport(impGetStdHandle)
	a.Store(Rbp, offStartup+winStartupStdIn, Rax)
	a.Load(Rax, Rbp, offOutWrite)
	a.Store(Rbp, offStartup+winStartupStdOut, Rax)
	a.Load(Rax, Rbp, offErrWrite)
	a.Store(Rbp, offStartup+winStartupStdErr, Rax)
	a.Xor(Rcx, Rcx) // lpApplicationName = 0: the first word of the command line, searched for
	a.Load(Rdx, Rbp, offCmd)
	a.Xor(R8, R8)
	a.Xor(R9, R9)
	a.MovImm(Rax, 1)
	a.Store(Rsp, 32, Rax) // bInheritHandles = TRUE
	a.Xor(Rax, Rax)
	a.Store(Rsp, 40, Rax) // dwCreationFlags = 0
	a.Store(Rsp, 48, Rax) // lpEnvironment = 0
	a.Store(Rsp, 56, Rax) // lpCurrentDirectory = 0
	a.Lea(Rax, Rbp, offStartup)
	a.Store(Rsp, 64, Rax)
	a.Lea(Rax, Rbp, offProcess)
	a.Store(Rsp, 72, Rax)
	a.CallImport(impCreateProcessA)
	a.Test(Rax, Rax)
	a.Jcc(CcNE, started)
	a.CallImport(impGetLastError)
	a.Store(Rbp, offError, Rax)
	g.abandonRun([]int{offOutWrite, offErrWrite}, []int{offOut, offErr}, offCmd)
	a.Load(Rax, Rbp, offError)
	g.failWithError(ir.SpawnFailures)

	a.Bind(pipeFailed)
	g.abandonRun([]int{offOutWrite, offErrWrite}, []int{offOut, offErr}, offCmd)
	g.failWith(ast.ErrorIoFailed)

	a.Bind(started)
	for _, end := range []int{offOutWrite, offErrWrite, offProcess + winProcessThread} {
		a.Load(Rcx, Rbp, end)
		a.CallImport(impCloseHandle)
	}
	a.Load(Rax, Rbp, offCmd)
	a.Push(Rax)
	g.callRuntime(g.rtFree, 1)
	a.MovImm(Rax, 1)
	a.Store(Rbp, offOut+pipeOpen, Rax)
	a.Store(Rbp, offErr+pipeOpen, Rax)
	a.Bind(drain)
	a.Lea(Rbx, Rbp, offOut)
	a.Call(g.drainPipe)
	a.Store(Rbp, offMoved, Rax)
	a.Lea(Rbx, Rbp, offErr)
	a.Call(g.drainPipe)
	a.Load(Rcx, Rbp, offMoved)
	a.Or(Rax, Rcx)
	a.Test(Rax, Rax)
	a.Jcc(CcNE, drain)
	a.Load(Rax, Rbp, offOut+pipeOpen)
	a.Load(Rcx, Rbp, offErr+pipeOpen)
	a.Or(Rax, Rcx)
	a.Test(Rax, Rax)
	a.Jcc(CcE, drained)
	a.Load(Rcx, Rbp, offProcess)
	a.MovImm(Rdx, winPollMillis)
	a.CallImport(impWaitForSingleObject)
	a.Jmp(drain)
	a.Bind(drained)
	a.Load(Rcx, Rbp, offProcess)
	a.MovImm(Rdx, winInfinite)
	a.CallImport(impWaitForSingleObject)
	a.Load(Rcx, Rbp, offProcess)
	a.Lea(Rdx, Rbp, offExitCode)
	a.CallImport(impGetExitCodeProcess)
	for _, end := range []int{offProcess, offOut + pipeHandle, offErr + pipeHandle} {
		a.Load(Rcx, Rbp, end)
		a.CallImport(impCloseHandle)
	}
	for _, out := range [][2]int{{offOut, osArg(2, runArgs)}, {offErr, osArg(3, runArgs)}} {
		a.Load(Rcx, Rbp, out[1])
		a.Load(Rax, Rbp, out[0]+pipeText)
		a.Store(Rcx, 0, Rax)
	}
	a.Load(Rax, Rbp, offExitCode)
	g.succeed()
}

// abandonRun undoes a run that could not start its child: it closes the ends of the pipes it made, and frees the texts
// and the command line.
func (g *osUnit) abandonRun(writeEnds, pipes []int, offCmd int) {
	a := g.asm
	for _, end := range writeEnds {
		g.closeIfOpen(end)
	}
	for _, pipe := range pipes {
		g.closeIfOpen(pipe + pipeHandle)
	}
	for _, pipe := range pipes {
		a.Load(Rax, Rbp, pipe+pipeText)
		a.Push(Rax)
		g.callRuntime(g.rtFree, 1)
	}
	a.Load(Rax, Rbp, offCmd)
	a.Push(Rax)
	g.callRuntime(g.rtFree, 1)
}

// closeIfOpen closes the handle at the frame offset, unless it is 0.
func (g *osUnit) closeIfOpen(offset int) {
	a := g.asm
	skip := NewLabel()
	a.Load(Rcx, Rbp, offset)
	a.Test(Rcx, Rcx)
	a.Jcc(CcE, skip)
	a.CallImport(impCloseHandle)
	a.Bind(skip)
}
