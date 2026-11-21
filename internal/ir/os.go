package ir

import (
	"slices"

	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
)

// OsFailure is why an OS operation failed, as the runtime tells it from the operating system's error; the entry of the
// operation reports it as the case of the error the operation throws (FailureCases).
type OsFailure uint8

const (
	FailureNotFound OsFailure = iota
	FailureAccessDenied
	FailureBusy
	FailureAlreadyExists
	FailureOther
	OsFailureCount
)

// The Win32 errors the runtime tells apart, the ones maxon's runtime does.
const (
	win32FileNotFound     = 2
	win32PathNotFound     = 3
	win32AccessDenied     = 5
	win32SharingViolation = 32
	win32FileExists       = 80
)

// Win32Failure is a Win32 error the runtime tells apart, and the failure it is.
type Win32Failure struct {
	Code    uint32
	Failure OsFailure
}

// Win32Failures are the Win32 errors the runtime tells apart; any other is FailureOther.
var Win32Failures = [...]Win32Failure{
	{win32FileNotFound, FailureNotFound},
	{win32PathNotFound, FailureNotFound},
	{win32AccessDenied, FailureAccessDenied},
	{win32SharingViolation, FailureBusy},
	{win32FileExists, FailureAlreadyExists},
}

// FailureOfWin32 is the failure a Win32 error is.
func FailureOfWin32(code uint32) OsFailure {
	for _, w := range Win32Failures {
		if w.Code == code {
			return w.Failure
		}
	}
	return FailureOther
}

// FailureCases is the case of an operation's error that each failure is.
type FailureCases [OsFailureCount]int

// The cases of the errors of the OS operations, by failure.
var (
	ReadFailures = FailureCases{
		FailureNotFound: ast.ErrorReadNotFound, FailureAccessDenied: ast.ErrorReadAccessDenied, FailureBusy: ast.ErrorReadBusy,
		FailureAlreadyExists: ast.ErrorReadFailed, FailureOther: ast.ErrorReadFailed,
	}
	WriteFailures = FailureCases{
		FailureNotFound: ast.ErrorWriteNotFound, FailureAccessDenied: ast.ErrorWriteAccessDenied, FailureBusy: ast.ErrorWriteBusy,
		FailureAlreadyExists: ast.ErrorWriteAlreadyExists, FailureOther: ast.ErrorWriteFailed,
	}
	RenameFailures = FailureCases{
		FailureNotFound: ast.ErrorRenameNotFound, FailureAccessDenied: ast.ErrorRenameAccessDenied, FailureBusy: ast.ErrorRenameBusy,
		FailureAlreadyExists: ast.ErrorRenameAlreadyExists, FailureOther: ast.ErrorRenameFailed,
	}
	DeleteFailures = FailureCases{
		FailureNotFound: ast.ErrorDeleteNotFound, FailureAccessDenied: ast.ErrorDeleteAccessDenied, FailureBusy: ast.ErrorDeleteBusy,
		FailureAlreadyExists: ast.ErrorDeleteFailed, FailureOther: ast.ErrorDeleteFailed,
	}
	ListFailures = FailureCases{
		FailureNotFound: ast.ErrorListNotFound, FailureAccessDenied: ast.ErrorListAccessDenied, FailureBusy: ast.ErrorListFailed,
		FailureAlreadyExists: ast.ErrorListFailed, FailureOther: ast.ErrorListFailed,
	}
	SpawnFailures = FailureCases{
		FailureNotFound: ast.ErrorExecutableNotFound, FailureAccessDenied: ast.ErrorSpawnFailed, FailureBusy: ast.ErrorSpawnBusy,
		FailureAlreadyExists: ast.ErrorSpawnFailed, FailureOther: ast.ErrorSpawnFailed,
	}
)

// payloadField is the index, in the payload of a case of Executable or WaitDeadline, of the one field it has.
const payloadField = 0

// Path characters that FilePath.from refuses besides the control characters, as maxon's does on Windows.
const (
	// LastControlByte is the highest byte that is a control character.
	LastControlByte = 31
	// InvalidPathBytes are the printable bytes Windows refuses in a path.
	InvalidPathBytes = `<>"|?*`
	// PathSeparator is the native separator, which FilePath.from writes for OtherSeparator.
	PathSeparator  = '\\'
	OtherSeparator = '/'
)

// failing calls an OS entry that can fail, giving it the address of its error word last, and goes on only when the
// entry did not fail; dst is NoReg for an entry that returns nothing.
func (l *Lowerer) failing(site ast.Pos, dst V, symbol Rt, args ...V) V {
	errSlot := l.f.NewSlot(KindScalar)
	l.emitEntry(site, dst, symbol, append(slices.Clip(args), l.slotAddr(errSlot)), nil)
	err := l.load(errSlot)
	ok := l.b.NewBlock()
	l.b.Terminate(l.cur, Branch(l.isZero(err), ok, l.failure(0, err)))
	l.cur = ok
	return dst
}

func (l *Lowerer) slotAddr(slot int) V {
	d := l.reg(KindScalar)
	l.emit(SlotAddr(d, slot))
	return d
}

// pathText is the String a FilePath holds, lent for as long as the path is.
func (l *Lowerer) pathText(path V) V {
	d := l.reg(KindManaged)
	l.emit(LoadField(d, path, FieldOffset(ast.FilePathPathField, false)))
	return d
}

// builtinRecord is a new box of a record of the table, whose fields are the values by index.
func (l *Lowerer) builtinRecord(t ast.Ty, values ...V) V {
	r, ok := l.binding.Apply(t).(*ast.RecordTy)
	if !ok || len(r.FieldList()) != len(values) {
		panic(diag.Internal("lower: %s is not a record of %d fields", t.Name(), len(values)))
	}
	p := l.reg(KindManaged)
	l.emit(Alloc(p, BoxBytes(len(values), false)))
	for i, v := range values {
		l.emit(StoreField(p, FieldOffset(i, false), v))
	}
	return p
}

func (l *Lowerer) lowerCommandLineArgs(c call, _ V, _ []V) V {
	return l.entry(c.CallExpr.Position(), RtOsArgs, KindManaged)
}

func (l *Lowerer) lowerProcessEnvironmentVariable(c call, _ V, args []V) V {
	return l.failing(c.CallExpr.Position(), l.reg(KindManaged), RtOsEnvironmentVariable, args[0])
}

func (l *Lowerer) lowerProcessExecutablePath(c call, _ V, _ []V) V {
	text := l.failing(c.CallExpr.Position(), l.reg(KindManaged), RtOsExecutablePath)
	return l.builtinRecord(l.ty(c.CallExpr.Expr()), text)
}

func (l *Lowerer) lowerProcessId(c call, _ V, _ []V) V {
	return l.entry(c.CallExpr.Position(), RtOsProcessId, KindScalar)
}

func (l *Lowerer) lowerDirectoryCurrentPath(c call, _ V, _ []V) V {
	return l.builtinRecord(l.ty(c.CallExpr.Expr()), l.entry(c.CallExpr.Position(), RtOsCurrentPath, KindManaged))
}

func (l *Lowerer) lowerFilePathFrom(c call, _ V, args []V) V {
	text := l.failing(c.CallExpr.Position(), l.reg(KindManaged), RtOsPathFrom, args[0])
	return l.builtinRecord(l.ty(c.CallExpr.Expr()), text)
}

func (l *Lowerer) lowerFilePathToString(c call, recv V, _ []V) V {
	return l.copyValue(c.CallExpr.Position(), l.pathText(recv), ast.StringTy)
}

func (l *Lowerer) lowerFileReadText(c call, _ V, args []V) V {
	return l.failing(c.CallExpr.Position(), l.reg(KindManaged), RtOsReadText, l.pathText(args[0]))
}

func (l *Lowerer) lowerFileWriteText(c call, _ V, args []V) V {
	l.failing(c.CallExpr.Position(), NoReg, RtOsWriteText, l.pathText(args[0]), args[1])
	return l.konst(0)
}

func (l *Lowerer) lowerFileWriteBinary(c call, _ V, args []V) V {
	l.failing(c.CallExpr.Position(), NoReg, RtOsWriteBinary, l.pathText(args[0]), args[1])
	return l.konst(0)
}

func (l *Lowerer) lowerFileRename(c call, _ V, args []V) V {
	l.failing(c.CallExpr.Position(), NoReg, RtOsRename, l.pathText(args[0]), l.pathText(args[1]))
	return l.konst(0)
}

func (l *Lowerer) lowerFileDelete(c call, _ V, args []V) V {
	l.failing(c.CallExpr.Position(), NoReg, RtOsDelete, l.pathText(args[0]))
	return l.konst(0)
}

func (l *Lowerer) lowerDirectoryIsDirectory(c call, _ V, args []V) V {
	return l.entry(c.CallExpr.Position(), RtOsIsDirectory, KindScalar, l.pathText(args[0]))
}

func (l *Lowerer) lowerDirectoryCreate(c call, _ V, args []V) V {
	return l.entry(c.CallExpr.Position(), RtOsCreateDirectory, KindScalar, l.pathText(args[0]))
}

// lowerDirectoryList makes a FilePath of each String the entry gives, in its place in the array, so the array the entry
// made is the one returned.
func (l *Lowerer) lowerDirectoryList(c call, _ V, args []V) V {
	site := c.CallExpr.Position()
	paths := l.failing(site, l.reg(KindManaged), RtOsList, l.pathText(args[0]))
	pathTy := l.binding.Apply(l.ty(c.CallExpr.Expr())).(*ast.BuiltinTy).Element()
	l.countedLoop(l.entry(site, RtArrayLen, KindScalar, paths), func(i V) {
		addr := l.entry(site, RtArrayElem, KindScalar, paths, i)
		text := l.reg(KindManaged)
		l.emit(MoveAt(text, addr))
		l.emit(StoreAt(addr, l.builtinRecord(pathTy, text)))
	})
	return paths
}

// NoDeadline is the deadline of rt.os.run that never passes.
const NoDeadline = 0

func (l *Lowerer) lowerSubprocessRun(c call, _ V, args []V) V {
	return l.runSubprocess(c, args[0], args[1], l.konst(NoDeadline))
}

func (l *Lowerer) lowerSubprocessRunWithDeadline(c call, _ V, args []V) V {
	return l.runSubprocess(c, args[0], args[1], l.deadlineMs(args[2]))
}

// deadlineMs is the milliseconds a WaitDeadline gives, NoDeadline for none.
func (l *Lowerer) deadlineMs(deadline V) V {
	ms := l.f.NewSlot(KindScalar)
	l.emit(StoreSlot(ms, l.konst(NoDeadline), false))
	within := l.b.NewBlock()
	end := l.b.NewBlock()
	l.b.Terminate(l.cur, Branch(l.compare(CmpEq, l.tag(deadline), l.konst(ast.WaitDeadlineWithin), false), within, end))
	l.cur = within
	payload := l.reg(KindScalar)
	l.emit(LoadField(payload, deadline, FieldOffset(payloadField, true)))
	l.emit(StoreSlot(ms, payload, false))
	l.goTo(end)
	return l.load(ms)
}

// runSubprocess runs the child and makes the CollectedOutput of what the entry stored and returned.
func (l *Lowerer) runSubprocess(c call, executable, arguments, deadlineMs V) V {
	site := c.CallExpr.Position()
	stdout := l.f.NewSlot(KindManaged)
	stderr := l.f.NewSlot(KindManaged)
	code := l.failing(site, l.reg(KindScalar), RtOsRun, l.executableText(executable), arguments, deadlineMs, l.slotAddr(stdout), l.slotAddr(stderr))
	fields := make([]V, ast.CollectedOutputCodeField+1)
	fields[ast.CollectedOutputStdoutField] = l.fetch(stdout)
	fields[ast.CollectedOutputStderrField] = l.fetch(stderr)
	fields[ast.CollectedOutputCodeField] = code
	return l.builtinRecord(l.ty(c.CallExpr.Expr()), fields...)
}

// executableText is the String an Executable names, a name or a path's, lent for as long as the Executable is.
func (l *Lowerer) executableText(exe V) V {
	text := l.f.NewSlot(KindManaged)
	payload := l.reg(KindManaged)
	l.emit(LoadField(payload, exe, FieldOffset(payloadField, true)))
	path := l.b.NewBlock()
	name := l.b.NewBlock()
	end := l.b.NewBlock()
	l.b.Terminate(l.cur, Branch(l.compare(CmpEq, l.tag(exe), l.konst(ast.ExecutablePath), false), path, name))
	l.cur = path
	l.emit(StoreSlot(text, l.pathText(payload), true))
	l.goTo(end)
	l.cur = name
	l.emit(StoreSlot(text, payload, true))
	l.goTo(end)
	l.cur = end
	return l.load(text)
}

func (l *Lowerer) lowerCollectedOutputExitCode(c call, recv V, _ []V) V {
	d := l.reg(KindScalar)
	l.emit(LoadField(d, recv, FieldOffset(ast.CollectedOutputCodeField, false)))
	return d
}

func (l *Lowerer) lowerCollectedOutputSucceeded(c call, recv V, args []V) V {
	return l.isZero(l.lowerCollectedOutputExitCode(c, recv, args))
}
