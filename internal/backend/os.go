package backend

import "minimaxon/internal/ir"

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

	winFindDataBytes      = 320
	winFindDataNameOffset = 44
	winStartupInfoBytes   = 104
	winProcessInfoBytes   = 24
	winOsCallHomeBytes    = 96
)

// EmitOsRuntime is the unit with the minimal Windows OS and CLI entries. It is linked only into a program that calls one.
func EmitOsRuntime() *CodeUnit {
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
	rtArrayNew := ExternLabel(ir.RtArrayNew.String())
	rtArrayLen := ExternLabel(ir.RtArrayLen.String())
	rtArrayInsert := ExternLabel(ir.RtArrayInsert.String())

	// Helper: toCStr(Rax = String) -> Rax = heap null-terminated C string. Clobbers Rax, Rcx, Rsi, Rdi, R8.
	toCStr := NewLabel()
	a.Bind(toCStr)
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
	fromCStr := NewLabel()
	a.Bind(fromCStr)
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

	// Helper: writeAll(Rbx = path String, R12 = data ptr, R13 = data len) -> Rax = 1 on success, 0 on error.
	writeAll := NewLabel()
	a.Bind(writeAll)
	writeFail := NewLabel()
	writeLoop := NewLabel()
	writeClose := NewLabel()
	writeOk := NewLabel()
	a.Push(Rbp)
	a.Mov(Rbp, Rsp)
	a.AndImm(Rsp, stackAlignMask)
	a.SubImm(Rsp, winOsCallHomeBytes)
	a.Mov(Rax, Rbx)
	a.Call(toCStr)
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
	a.Push(R14)
	g.callRuntime(g.rtFree, 1)
	a.CmpImm(R15, winInvalidHandle)
	a.Jcc(CcE, writeFail)
	a.MovImm(R14, 1) // R14 = ok flag
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
	a.Xor(R14, R14)
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
	a.Bind(writeFail)
	a.Xor(Rax, Rax)
	g.epilogue()

	// Helper: startWinFrame reserves locals + aligns stack + reserves winOsCallHomeBytes.
	startWinFrame := func(locals int) {
		a.Push(Rbp)
		a.Mov(Rbp, Rsp)
		if locals > 0 {
			a.SubImm(Rsp, locals)
		}
		a.AndImm(Rsp, stackAlignMask)
		a.SubImm(Rsp, winOsCallHomeBytes)
	}

	// rt.os.cliArgs() -> array with String
	u.DefineCode(ir.RtOsCliArgs.String())
	startWinFrame(16) // [Rbp-8] = array, [Rbp-16] = cursor
	cliSkipWs := NewLabel()
	cliDone := NewLabel()
	cliQuoted := NewLabel()
	cliUnquoted := NewLabel()
	cliQuotedEnd := NewLabel()
	cliPush := NewLabel()
	g.callRuntime(rtArrayNew, 0)
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
	a.Call(fromCStr)
	a.Mov(R15, Rax) // R15 = arg string
	a.Load(R12, Rbp, -8)
	a.Push(R12)
	g.callRuntime(rtArrayLen, 1)
	a.Push(R12)
	a.Push(Rax)
	a.Push(R15)
	g.callRuntime(rtArrayInsert, 3)
	a.Jmp(cliSkipWs)
	a.Bind(cliDone)
	a.Load(Rax, Rbp, -8)
	g.epilogue()

	// rt.os.getEnv(name String) -> String or 0
	u.DefineCode(ir.RtOsGetEnv.String())
	startWinFrame(0)
	envMiss := NewLabel()
	a.Load(Rax, Rbp, 16)
	a.Call(toCStr)
	a.Mov(R12, Rax) // R12 = cName
	a.Mov(Rcx, R12)
	a.Xor(Rdx, Rdx)
	a.Xor(R8, R8)
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
	g.epilogue()
	a.Bind(envMiss)
	a.Push(R12)
	g.callRuntime(g.rtFree, 1)
	a.Xor(Rax, Rax)
	g.epilogue()

	// rt.os.readFile(path String) -> String or 0
	u.DefineCode(ir.RtOsReadFile.String())
	startWinFrame(16) // [Rbp-8] = fileSize, [Rbp-16] = bytesRead
	readFail := NewLabel()
	readCloseFail := NewLabel()
	readFreeFail := NewLabel()
	readLoop := NewLabel()
	readDone := NewLabel()
	a.Load(Rax, Rbp, 16)
	a.Call(toCStr)
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
	a.Push(R12)
	g.callRuntime(g.rtFree, 1)
	a.CmpImm(R13, winInvalidHandle)
	a.Jcc(CcE, readFail)
	a.Mov(Rcx, R13)
	a.Lea(Rdx, Rbp, -8)
	a.CallImport(impGetFileSizeEx)
	a.Test(Rax, Rax)
	a.Jcc(CcE, readCloseFail)
	a.Load(R14, Rbp, -8) // R14 = fileSize
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
	a.Lea(R9, Rbp, -16)
	a.Xor(Rax, Rax)
	a.Store(Rbp, -16, Rax)
	a.Store(Rsp, 32, Rax)
	a.CallImport(impReadFile)
	a.Test(Rax, Rax)
	a.Jcc(CcE, readFreeFail)
	a.Load(Rax, Rbp, -16)
	a.Test(Rax, Rax)
	a.Jcc(CcE, readDone)
	a.Add(Rbx, Rax)
	a.Sub(R12, Rax)
	a.Jmp(readLoop)
	a.Bind(readDone)
	a.Mov(Rcx, R13)
	a.CallImport(impCloseHandle)
	a.Mov(Rax, R15)
	g.epilogue()
	a.Bind(readFreeFail)
	a.Push(R15)
	g.callRuntime(g.rtFree, 1)
	a.Bind(readCloseFail)
	a.Mov(Rcx, R13)
	a.CallImport(impCloseHandle)
	a.Bind(readFail)
	a.Xor(Rax, Rax)
	g.epilogue()

	// rt.os.writeFile(path String, text String) -> bool
	u.DefineCode(ir.RtOsWriteFile.String())
	startWinFrame(0)
	a.Load(Rbx, Rbp, 24)
	a.Load(Rax, Rbp, 16)
	a.Load(R13, Rax, stringLengthOffset)
	a.Lea(R12, Rax, wordBytes)
	a.Call(writeAll)
	g.epilogue()

	// rt.os.writeBytes(path String, data Bytes) -> bool
	u.DefineCode(ir.RtOsWriteBytes.String())
	startWinFrame(0)
	a.Load(Rbx, Rbp, 24)
	a.Load(Rax, Rbp, 16)
	a.Load(R13, Rax, bytesLengthOffset)
	a.Load(R12, Rax, bytesDataOffset)
	a.Call(writeAll)
	g.epilogue()

	// rt.os.renameFile(from String, dest String) -> bool
	u.DefineCode(ir.RtOsRenameFile.String())
	startWinFrame(0)
	a.Load(Rax, Rbp, 24)
	a.Call(toCStr)
	a.Mov(R12, Rax)
	a.Load(Rax, Rbp, 16)
	a.Call(toCStr)
	a.Mov(R13, Rax)
	a.Mov(Rcx, R12)
	a.Mov(Rdx, R13)
	a.MovImm(R8, winMoveFileReplaceExisting)
	a.CallImport(impMoveFileExA)
	a.Test(Rax, Rax)
	a.SetccRax(CcNE)
	a.Mov(R14, Rax)
	a.Push(R12)
	g.callRuntime(g.rtFree, 1)
	a.Push(R13)
	g.callRuntime(g.rtFree, 1)
	a.Mov(Rax, R14)
	g.epilogue()

	// rt.os.removeFile(path String) -> bool
	u.DefineCode(ir.RtOsRemoveFile.String())
	startWinFrame(0)
	a.Load(Rax, Rbp, 16)
	a.Call(toCStr)
	a.Mov(R12, Rax)
	a.Mov(Rcx, R12)
	a.CallImport(impDeleteFileA)
	a.Test(Rax, Rax)
	a.SetccRax(CcNE)
	a.Mov(R13, Rax)
	a.Push(R12)
	g.callRuntime(g.rtFree, 1)
	a.Mov(Rax, R13)
	g.epilogue()

	// rt.os.isDir(path String) -> bool
	u.DefineCode(ir.RtOsIsDir.String())
	startWinFrame(0)
	isDirFalse := NewLabel()
	isDirDone := NewLabel()
	a.Load(Rax, Rbp, 16)
	a.Call(toCStr)
	a.Mov(R12, Rax)
	a.Mov(Rcx, R12)
	a.CallImport(impGetFileAttributesA)
	a.MovImm(Rcx, winInvalidFileAttributes)
	a.Cmp(Rax, Rcx)
	a.Jcc(CcE, isDirFalse)
	a.AndImm(Rax, winFileAttributeDirectory)
	a.Test(Rax, Rax)
	a.SetccRax(CcNE)
	a.Mov(R13, Rax)
	a.Jmp(isDirDone)
	a.Bind(isDirFalse)
	a.Xor(R13, R13)
	a.Bind(isDirDone)
	a.Push(R12)
	g.callRuntime(g.rtFree, 1)
	a.Mov(Rax, R13)
	g.epilogue()

	// rt.os.mkdir(path String) -> bool
	u.DefineCode(ir.RtOsMkdir.String())
	startWinFrame(0)
	a.Load(Rax, Rbp, 16)
	a.Call(toCStr)
	a.Mov(R12, Rax)
	a.Mov(Rcx, R12)
	a.Xor(Rdx, Rdx)
	a.CallImport(impCreateDirectoryA)
	a.Test(Rax, Rax)
	a.SetccRax(CcNE)
	a.Mov(R13, Rax)
	a.Push(R12)
	g.callRuntime(g.rtFree, 1)
	a.Mov(Rax, R13)
	g.epilogue()

	// rt.os.listDir(path String) -> array with String or 0
	u.DefineCode(ir.RtOsListDir.String())
	const (
		listOffArray  = -8
		listOffHandle = -16
		listOffFind   = -16 - winFindDataBytes
	)
	startWinFrame(16 + winFindDataBytes)
	listFail := NewLabel()
	listLoop := NewLabel()
	listNext := NewLabel()
	listDone := NewLabel()
	// Build "<path>\\*\0"
	a.Load(R14, Rbp, 16) // R14 = path String
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
	a.MovImm(Rcx, '\\')
	a.StoreByte(Rax, 0, Rcx)
	a.MovImm(Rcx, '*')
	a.StoreByte(Rax, 1, Rcx)
	a.Mov(Rcx, R12)
	a.Lea(Rdx, Rbp, listOffFind)
	a.CallImport(impFindFirstFileA)
	a.Store(Rbp, listOffHandle, Rax)
	a.Push(R12)
	g.callRuntime(g.rtFree, 1)
	a.Load(R13, Rbp, listOffHandle)
	a.CmpImm(R13, winInvalidHandle)
	a.Jcc(CcE, listFail)
	g.callRuntime(rtArrayNew, 0)
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
	a.Mov(Rsi, R14)
	a.Call(fromCStr)
	a.Mov(R15, Rax)
	a.Load(R12, Rbp, listOffArray)
	a.Push(R12)
	g.callRuntime(rtArrayLen, 1)
	a.Push(R12)
	a.Push(Rax)
	a.Push(R15)
	g.callRuntime(rtArrayInsert, 3)
	a.Bind(listNext)
	a.Load(Rcx, Rbp, listOffHandle)
	a.Lea(Rdx, Rbp, listOffFind)
	a.CallImport(impFindNextFileA)
	a.Test(Rax, Rax)
	a.Jcc(CcNE, listLoop)
	a.Bind(listDone)
	a.Load(Rcx, Rbp, listOffHandle)
	a.CallImport(impFindClose)
	a.Load(Rax, Rbp, listOffArray)
	g.epilogue()
	a.Bind(listFail)
	a.Xor(Rax, Rax)
	g.epilogue()

	// rt.os.runProcess(commandLine String) -> (exitCode << 1) | 1, or 0 on failure
	u.DefineCode(ir.RtOsRunProcess.String())
	const (
		procFrameBytes = 144 // 104 STARTUPINFOA + 24 PROCESS_INFORMATION + 8 exitCode + 8 pad
		offStartupInfo = -104
		offProcessInfo = -128
		offExitCode    = -136
	)
	startWinFrame(procFrameBytes)
	procFail := NewLabel()
	// Zero STARTUPINFOA and PROCESS_INFORMATION (128 bytes = 16 qwords)
	a.Lea(Rdi, Rbp, offProcessInfo)
	a.Xor(Rax, Rax)
	a.MovImm(Rcx, 16)
	a.RepStosq()
	a.MovImm(Rax, winStartupInfoBytes)
	a.Store(Rbp, offStartupInfo, Rax) // cb = 104 (upper 32 bits 0)
	a.Load(Rax, Rbp, 16)
	a.Call(toCStr)
	a.Mov(R12, Rax) // R12 = cCmdLine (writable buffer, as CreateProcessA requires)
	a.Xor(Rcx, Rcx) // lpApplicationName = 0
	a.Mov(Rdx, R12) // lpCommandLine
	a.Xor(R8, R8)   // lpProcessAttributes = 0
	a.Xor(R9, R9)   // lpThreadAttributes = 0
	a.Xor(Rax, Rax)
	a.Store(Rsp, 32, Rax) // bInheritHandles = FALSE
	a.Store(Rsp, 40, Rax) // dwCreationFlags = 0
	a.Store(Rsp, 48, Rax) // lpEnvironment = 0
	a.Store(Rsp, 56, Rax) // lpCurrentDirectory = 0
	a.Lea(Rax, Rbp, offStartupInfo)
	a.Store(Rsp, 64, Rax) // lpStartupInfo
	a.Lea(Rax, Rbp, offProcessInfo)
	a.Store(Rsp, 72, Rax) // lpProcessInformation
	a.CallImport(impCreateProcessA)
	a.Mov(R13, Rax)
	a.Push(R12)
	g.callRuntime(g.rtFree, 1)
	a.Test(R13, R13)
	a.Jcc(CcE, procFail)
	a.Load(Rcx, Rbp, offProcessInfo) // hProcess
	a.MovImm(Rdx, winInfinite)
	a.CallImport(impWaitForSingleObject)
	a.Xor(Rax, Rax)
	a.Store(Rbp, offExitCode, Rax)
	a.Load(Rcx, Rbp, offProcessInfo) // hProcess
	a.Lea(Rdx, Rbp, offExitCode)
	a.CallImport(impGetExitCodeProcess)
	a.Load(Rcx, Rbp, offProcessInfo) // hProcess
	a.CallImport(impCloseHandle)
	a.Load(Rcx, Rbp, offProcessInfo+8) // hThread
	a.CallImport(impCloseHandle)
	a.Load(Rax, Rbp, offExitCode)
	a.Add(Rax, Rax)
	a.Or(Rax, Rax)
	a.AddImm(Rax, 1) // (exitCode << 1) | 1
	g.epilogue()
	a.Bind(procFail)
	a.Xor(Rax, Rax)
	g.epilogue()

	return u
}
