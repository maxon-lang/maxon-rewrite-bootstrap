package main

import (
	"fmt"
	"slices"
)

// This file is the source of truth for minimaxon's built-ins: the types every project has before it declares
// anything, the functions and methods on them, the intrinsic each lowers to, and the runtime entries those intrinsics
// call. `go generate ./internal/builtingen` writes from it the declarations, the enums and the dispatch in self/.
//
// A type in a signature is written as a program spells an instance, with the table's keys as names: a key of
// primTypes, a type row's Key, the parameters of the owner or of the function, Self for the owner's own instance,
// and 'X with Y' for an instance of a generic type of one parameter.

// primType is a type the table names but other code declares.
type primType struct {
	Key   string
	Maxon string // the Maxon expression of it inside self's declaration code
}

var primTypes = []primType{
	{"Bool", "b.boolTy"},
	{"String", "b.stringTy"},
	{"Character", "b.characterTy"},
	{"CString", "b.cStringTy"},
	{"Void", "b.voidTy"},
	{"Count", "b.countTy"},
	{"ElementIndex", "b.elementIndexTy"},
	{"BytePos", "b.bytePosTy"},
	{"Byte", "b.byteTy"},
	{"FullRangeInt", "b.fullRangeIntTy"},
	{"Codepoint", "b.codepointTy"},
	{"AsciiValue", "b.asciiValueTy"},
	{"CodepointDelta", "b.codepointDeltaTy"},
	{"NetworkPort", "b.networkPortTy"},
	{"CodeUnit16", "b.codeUnit16Ty"},
	{"ExitCode", "b.exitCodeTy"},
	{"InstantNanos", "b.instantNanosTy"},
	{"InstantMs", "b.instantMsTy"},
	{"ThreadCpuTicks", "b.threadCpuTicksTy"},
	{"UnixSeconds", "b.unixSecondsTy"},
	{"UnixNanos", "b.unixNanosTy"},
	{"ReportedCapacity", "b.reportedCapacityTy"},
	{"FullRangeInt", "b.fullRangeIntTy"},
	// Float is the type of a float whose range is not known, which maxon names FullRangeFloat.
	{"Float", "b.anonFloatTy"},
	// Natural is the type of an integer whose alias is not known and that is not negative, the unsigned word, which any integer
	// alias whose range it holds goes into, Count among them.
	{"Natural", "b.anonU64Ty"},
	{"OrderedWord", "b.orderedWordTy"},
	{"HashDigest", "b.hashDigestTy"},
}

// rawPrimTypes are the primitives of maxon's raw surface, whose name only the library tiers may write (E3185).
var rawPrimTypes = []struct {
	Key   string
	Named rawSurface
}{
	{"CString", rawSurface{What: "the NUL-terminated pointer type is nameable", Instead: "use 'String'"}},
}

// stdAlias is a built-in ranged alias of maxon's stdlib (`Count`, `ExitCode`...), which the rows of the table name and
// so is declared before them: its name, the Builtins field that holds it, and the Maxon expressions of its bounds.
type stdAlias struct {
	Name, Field, Lo, Hi string
	// Float is an alias of a range of floats rather than of integers.
	Float bool
	// Bits is the width of an alias of a bit pattern, bits(Bits), whose range is that of the width; 0 for a range.
	Bits int
}

var stdAliases = []stdAlias{
	{Name: "ExitCode", Field: "exitCodeTy", Lo: "I128.zero()", Hi: "I128.fromCount(4294967295 as Count)"},
	{Name: "Count", Field: "countTy", Lo: "I128.zero()", Hi: "I128.u64Max()"},
	{Name: "ElementIndex", Field: "elementIndexTy", Lo: "I128.zero()", Hi: "I128.u64Max()"},
	// BytePos is maxon's (stdlib/String.maxon): a position in the bytes of a string or a buffer of them.
	{Name: "BytePos", Field: "bytePosTy", Lo: "I128.zero()", Hi: "I128.u64Max()"},
	{Name: "FullRangeInt", Field: "fullRangeIntTy", Lo: "I128.i64Min()", Hi: "I128.i64Max()"},
	// ReportedCapacity is maxon's (stdlib/Array.maxon): what an array's capacity() gives, signed since maxon's view of a buffer it
	// does not own answers with a negative flag, which an array here never is.
	{Name: "ReportedCapacity", Field: "reportedCapacityTy", Lo: "I128.i64Min()", Hi: "I128.i64Max()"},
	{Name: "Byte", Field: "byteTy", Lo: "I128.zero()", Hi: "I128.fromCount(255 as Count)"},
	{Name: "FullRangeFloat", Field: "fullRangeFloatTy", Lo: "floatNegate(floatMaxBits)", Hi: "floatMaxBits", Float: true},
	{Name: "Codepoint", Field: "codepointTy", Lo: "I128.zero()", Hi: "I128.fromCount(1114111 as Count)"},
	// AsciiValue is maxon's (stdlib/Character.maxon): what Character.asciiValue gives.
	{Name: "AsciiValue", Field: "asciiValueTy", Lo: "I128.zero()", Hi: "I128.fromCount(127 as Count)"},
	// CodepointDelta is maxon's (stdlib/Character.maxon): a step of Character.advanceBy along the scalar values, either way.
	{Name: "CodepointDelta", Field: "codepointDeltaTy", Lo: "I128.fromCount(1114111 as Count).neg()", Hi: "I128.fromCount(1114111 as Count)"},
	// NetworkPort is maxon's (stdlib/TcpClient.maxon): a TCP or UDP port, which URL's port() gives.
	{Name: "NetworkPort", Field: "networkPortTy", Lo: "I128.zero()", Hi: "I128.fromCount(65535 as Count)"},
	{Name: "CodeUnit16", Field: "codeUnit16Ty", Lo: "I128.zero()", Hi: "I128.fromCount(65535 as Count)"},
	// InstantNanos is maxon's (stdlib/Clock.maxon): a reading of the monotonic clock in nanoseconds.
	{Name: "InstantNanos", Field: "instantNanosTy", Lo: "I128.zero()", Hi: "I128.u64Max()"},
	{Name: "InstantMs", Field: "instantMsTy", Lo: "I128.zero()", Hi: "I128.u64Max()"},
	{Name: "ThreadCpuTicks", Field: "threadCpuTicksTy", Lo: "I128.zero()", Hi: "I128.u64Max()"},
	{Name: "UnixSeconds", Field: "unixSecondsTy", Lo: "I128.i64Min()", Hi: "I128.i64Max()"},
	{Name: "UnixNanos", Field: "unixNanosTy", Lo: "I128.i64Min()", Hi: "I128.i64Max()"},
	// OrderedWord is the word maxon's library splits into bytes and assembles from them (maxon's stdlib/Builtins.maxon),
	// and HashDigest a seeded 64-bit digest (maxon's stdlib/Hasher.maxon), which the hash intrinsics give and take.
	{Name: "OrderedWord", Field: "orderedWordTy", Bits: 64},
	{Name: "HashDigest", Field: "hashDigestTy", Bits: 64},
}

// extraField is a field of self's Builtins record that no row of the table gives it: a type that initBuiltins registers
// by hand because it is no row of the table (the types of an unsuffixed literal, never, FullRangeFloat), and the maps of
// the stdlib's declarations that it fills last. Zero is the value Builtins.empty() holds until it does.
type extraField struct {
	Name, Type, Zero string
}

var extraFields = []extraField{
	{"neverTy", "TypeID", "noType"},
	{"anon64Ty", "TypeID", "noType"},
	{"fullRangeFloatTy", "TypeID", "noType"},
	{"stringMethods", "FuncIDMap", "FuncIDMap.new()"},
	{"characterMethods", "FuncIDMap", "FuncIDMap.new()"},
	{"stdlibTypeMap", "TypeIDMap", "TypeIDMap.new()"},
	{"stdlibFuncMap", "FuncIDMap", "FuncIDMap.new()"},
}

// builtinType is a runtime primitive: an ast.BuiltinTy of its own BuiltinKind.
type builtinType struct {
	Key    string // the ast.Builtins field Key
	Name   string // the name a program writes
	Params []string
	// Kind is the TypeKind's builtin+Kind, Key when it is empty: a FixedArray is an array, whose instance may state its
	// count as a type argument after Params, which the checker reads.
	Kind string
	// Managed is whether a value of it owns a box; Subscriptable whether t[i] names an element; Shared whether it is
	// counted and has many owners.
	Managed, Subscriptable, Shared bool
	// Raw makes it a type of maxon's raw surface, which only the library tiers may name.
	Raw *rawType
}

// rawSurface is how a door of maxon's raw surface is refused outside the library tiers (E3185), in maxon's words: what it
// is or reaches, and what a program uses instead.
type rawSurface struct {
	What, Instead string
}

// rawType is a type of the raw surface: naming it is one door, and calling a method on a value of it is another.
type rawType struct {
	Named, Methods rawSurface
}

var builtinTypes = []builtinType{
	{Key: "Array", Name: "Array", Params: []string{"T"}, Managed: true, Subscriptable: true},
	{Key: "FixedArray", Name: "FixedArray", Params: []string{"T"}, Kind: "Array", Managed: true, Subscriptable: true},
	// ManagedMemory is maxon's raw buffer: the box of an array, seen through maxon's buffer operations, each of which is
	// the array operation that does the same.
	{Key: "ManagedMemory", Name: "__ManagedMemory", Params: []string{"T"}, Kind: "Array", Managed: true, Raw: &rawType{
		Named:   rawSurface{What: "the raw buffer type is nameable", Instead: "use 'Array'"},
		Methods: rawSurface{What: "a raw '__ManagedMemory' buffer is reachable", Instead: "use the 'Array' methods"},
	}},
	// ManagedMemoryCursor is maxon's position in a raw buffer: the buffer it borrows and the index of an element of it.
	// It holds a borrow, so a value of it and of any type that holds one may not outlive the buffer (README, "Borrowing
	// values").
	{Key: "ManagedMemoryCursor", Name: "__ManagedMemoryCursor", Params: []string{"T"}, Kind: "Cursor", Managed: true, Raw: &rawType{
		Named:   rawSurface{What: "the raw buffer cursor type is nameable", Instead: "walk an 'Array' with 'for'"},
		Methods: rawSurface{What: "a raw '__ManagedMemoryCursor' is reachable", Instead: "walk an 'Array' with 'for'"},
	}},
	// StringCursor is a position in a String's grapheme clusters: a view of the String, which it borrows, and the byte
	// positions where the cluster it is at starts and ends. It holds a borrow, as a buffer's cursor does.
	{Key: "StringCursor", Name: "__StringCursor", Kind: "Cursor", Managed: true, Raw: &rawType{
		Named:   rawSurface{What: "the raw string cursor type is nameable", Instead: "walk a 'String' with 'for'"},
		Methods: rawSurface{What: "a raw '__StringCursor' is reachable", Instead: "walk a 'String' with 'for'"},
	}},
	// GroupMemory is a raw buffer of the groups of a swiss table, laid out inline: each group is a control word, which the
	// buffer gives and takes as it is, then eight pairs, two words to a pair. A half holds a value of its type or is empty,
	// 0 for a managed half, so a table keeps a key and its value side by side, next to the word that says which of the
	// group's pairs hold one. Its drop and its copy go through every half by its type.
	{Key: "GroupMemory", Name: "__GroupMemory", Params: []string{"A", "B"}, Kind: "Groups", Managed: true, Raw: &rawType{
		Named:   rawSurface{What: "the raw buffer of a table's groups is nameable", Instead: "use 'Map'"},
		Methods: rawSurface{What: "a raw '__GroupMemory' buffer is reachable", Instead: "use 'Map'"},
	}},
	{Key: "SlotKey", Name: "SlotKey", Params: []string{"T"}},
	{Key: "Frozen", Name: "frozen", Params: []string{"T"}, Managed: true, Shared: true},
	// Cown is the library tiers' machinery of services (README, "Immutable values, cowns and behaviours"): a program keeps
	// shared state in a service, so naming the type, and making one, is a door of the raw surface.
	{Key: "Cown", Name: "cown", Params: []string{"T"}, Managed: true, Shared: true, Raw: &rawType{
		Named: rawSurface{What: "the cown type is nameable", Instead: "keep shared state in a service and send it messages"},
	}},
	// Promise is maxon's: what an async call gives, the coroutine it started, which an await consumes. `Promise with T` is of
	// a function that throws nothing, and `Promise with (T, E)` of one that throws E, whose E is then void.
	{Key: "Promise", Name: "Promise", Params: []string{"T", "E"}, Managed: true},
	// Service is the handle of a service of the type T, which a program writes `T.handle`: a counted reference to the
	// service's cown, whose value is the service's state.
	{Key: "Service", Name: "__ServiceHandle", Params: []string{"T"}, Managed: true, Shared: true, Raw: &rawType{
		Named: rawSurface{What: "the handle type of a service is nameable by its built-in name", Instead: "write 'T.handle'"},
	}},
}

// typeAlias is a built-in typealias of an instance: the name and the type it is, one type and not two.
type typeAlias struct {
	Name string
	Type string
}

var typeAliases = []typeAlias{
	// ByteArray is maxon's name for an array of bytes, whose box holds one byte to an element (ast.BuiltinTy.PacksBytes).
	{Name: "ByteArray", Type: "Array with Byte"},
	// ByteArrays is maxon's name for an array of byte arrays (stdlib/X509.maxon declares it; the hash functions take a list of messages).
	{Name: "ByteArrays", Type: "Array with Array with Byte"},
	// StringArray is maxon's name for an array of strings (stdlib/Json.maxon declares it, but programs use it everywhere).
	{Name: "StringArray", Type: "Array with String"},
	// FullRangeIntArray and FilePathArray are the stdlib's names for arrays of those elements.
	{Name: "FullRangeIntArray", Type: "Array with FullRangeInt"},
	{Name: "FilePathArray", Type: "Array with FilePath"},
}

// recordType is a built-in record: a type whose fields a program reads as it reads its own record's, with the methods
// and static functions the table gives it. One with no fields is maxon's stdlib type of only static functions.
type recordType struct {
	Key  string // the ast.Builtins field
	Name string
	// Fields are "var name Type" or "let name Type", public as maxon's are, or "private let name Type" for a field
	// only the lowering reads, each with the Go constant of its index Key+upper(name)+"Field".
	Fields []string
	// Implements are the marker interfaces the record conforms to.
	Implements []string
	// RawCalls makes its static functions maxon's raw intrinsics, which only the library tiers may call (E3185); each
	// row of it names what a program calls instead.
	RawCalls bool
}

var recordTypes = []recordType{
	{Key: "CommandLine", Name: "CommandLine"},
	{Key: "Process", Name: "Process"},
	{Key: "Clock", Name: "Clock"},
	{Key: "Scheduler", Name: "Scheduler"},
	{Key: "FilePath", Name: "FilePath", Fields: []string{"let path String"}, Implements: []string{"InitableFromStringLiteral"}},
	{Key: "File", Name: "File"},
	{Key: "Directory", Name: "Directory"},
	// StringIndex is where a position in a string is: the grapheme cluster it is the start of and the byte it is at.
	{Key: "StringIndex", Name: "StringIndex", Fields: []string{"private let charIdx Count", "private let bytePosition Count"}},
	// The views of a string's bytes, code points and UTF-16 code units are maxon's: each holds the string it reads, and
	// walks with the iteration protocol through an iterator that holds it and the position of the unit it is at (for
	// UTF-16, twice the byte position of the code point, plus 1 at the second unit of a pair).
	{Key: "ByteView", Name: "ByteView", Fields: []string{"private let text String"}},
	{Key: "ByteIterator", Name: "ByteIterator", Fields: []string{"private let text String", "private var position Count"}},
	{Key: "CodepointView", Name: "CodepointView", Fields: []string{"private let text String"}},
	{Key: "CodepointIterator", Name: "CodepointIterator", Fields: []string{"private let text String", "private var position Count"}},
	{Key: "Utf16View", Name: "UTF16View", Fields: []string{"private let text String"}},
	{Key: "Utf16Iterator", Name: "UTF16Iterator", Fields: []string{"private let text String", "private var position Count"}},
	{Key: "CollectedOutput", Name: "CollectedOutput", Fields: []string{"var stdout String", "var stderr String", "private let code ExitCode"}},
	// FileInfo is maxon's: the size and the times of a file, in Unix seconds on the file system's clock, and its attributes.
	{Key: "FileInfo", Name: "FileInfo", Fields: []string{"let size Count", "let modifiedTime Count", "let createdTime Count", "let accessedTime Count", "let isDirectory Bool", "let isReadOnly Bool"}},
	{Key: "Subprocess", Name: "Subprocess"},
	// Debuggee is the host's debug interface that the compiler's debugger drives a program through from outside it, maxon's
	// stdlib Debuggee.maxon: its DebugPort's Win32 verbs (maxon's __Builtins.debug* under it) as static functions over
	// handles, ids and byte arrays, since the compiler, which is no library tier, calls them.
	{Key: "Debuggee", Name: "Debuggee"},
	// Raw is maxon's __Raw: the closed set of raw machine and OS operations the runtime's source is written on, which
	// only the runtime module may call (runtimeOnly).
	{Key: "Raw", Name: "__Raw"},
	// Builtins is maxon's __Builtins: the runtime's operations that maxon's library is written on, each the built-in that
	// does the same.
	{Key: "Builtins", Name: "__Builtins", RawCalls: true},
}

// view is a member of a built-in generic type, read without parentheses, that is the same value seen as an instance of
// another type of the table at the same type arguments: maxon's `.managed`, the buffer of an array.
type view struct {
	Owner, Name, Type string
	Raw               rawSurface
}

var views = []view{
	{Owner: "Array", Name: "managed", Type: "ManagedMemory", Raw: rawSurface{What: "a value's raw '__ManagedMemory' buffer is reachable", Instead: "use the 'Array' methods"}},
}

// unionCase is a case of a built-in union; Payload are its fields as "name Type".
type unionCase struct {
	Name    string
	Const   string // the Go constant of its index; self's is the same name in lowerCamelCase
	Payload []string
}

type unionType struct {
	Key    string // the ast.Builtins field
	Name   string
	Params []string
	Cases  []unionCase
}

var unionTypes = []unionType{
	// Executable is maxon's: a name the host searches for when the child is spawned, or a path.
	{Key: "Executable", Name: "Executable", Cases: []unionCase{
		{Name: "name", Const: "ExecutableName", Payload: []string{"value String"}},
		{Name: "path", Const: "ExecutablePath", Payload: []string{"value FilePath"}},
	}},
	// WaitDeadline is maxon's: no deadline, or the milliseconds after which a child is terminated.
	{Key: "WaitDeadline", Name: "WaitDeadline", Cases: []unionCase{
		{Name: "none", Const: "WaitDeadlineNone"},
		{Name: "within", Const: "WaitDeadlineWithin", Payload: []string{"timeoutMs Count"}},
	}},
}

type enumCase struct {
	Name  string
	Const string // the Go constant of its index; self's is the same name in lowerCamelCase
}

type enumType struct {
	Key   string // the ast.Builtins field
	Name  string
	Cases []enumCase
}

var enumTypes = []enumType{
	{Key: "ArithmeticError", Name: "ArithmeticError", Cases: []enumCase{
		{"overflow", "ErrorOverflow"},
		{"divisionByZero", "ErrorDivisionByZero"},
		{"negativeShift", "ErrorNegativeShift"},
	}},
	// The rest are maxon's stdlib errors, cases and all, so a match on one is the one maxon's needs, though no operation
	// here fails with some of the cases.
	{Key: "ArrayError", Name: "ArrayError", Cases: []enumCase{
		{"indexOutOfBounds", "ErrorIndexOutOfBounds"},
		{"emptySlot", "ErrorEmptySlot"},
	}},
	// ManagedMemoryError is what maxon's buffer operations throw; its first cases are an array's, so an array operation
	// that a buffer's row lowers to fails with the same case of either.
	{Key: "ManagedMemoryError", Name: "__ManagedMemoryError", Cases: []enumCase{
		{"indexOutOfBounds", "ErrorBufferIndexOutOfBounds"},
		{"emptySlot", "ErrorBufferEmptySlot"},
		{"sliceOutOfBounds", "ErrorBufferSliceOutOfBounds"},
		{"invalidLength", "ErrorBufferInvalidLength"},
		{"invalidCapacity", "ErrorBufferInvalidCapacity"},
		{"shiftOutOfBounds", "ErrorBufferShiftOutOfBounds"},
		{"invalidByteRange", "ErrorBufferInvalidByteRange"},
		{"invalidAllocation", "ErrorBufferInvalidAllocation"},
	}},
	{Key: "FilePathError", Name: "FilePathError", Cases: []enumCase{
		{"invalidCharacter", "ErrorPathInvalidCharacter"},
		{"notFileURL", "ErrorPathNotFileURL"},
		{"malformedURL", "ErrorPathMalformedURL"},
		{"noParent", "ErrorPathNoParent"},
	}},
	{Key: "FileReadError", Name: "FileReadError", Cases: []enumCase{
		{"notFound", "ErrorReadNotFound"},
		{"accessDenied", "ErrorReadAccessDenied"},
		{"busy", "ErrorReadBusy"},
		{"failed", "ErrorReadFailed"},
	}},
	{Key: "FileWriteError", Name: "FileWriteError", Cases: []enumCase{
		{"failed", "ErrorWriteFailed"},
		{"notFound", "ErrorWriteNotFound"},
		{"accessDenied", "ErrorWriteAccessDenied"},
		{"alreadyExists", "ErrorWriteAlreadyExists"},
		{"busy", "ErrorWriteBusy"},
		{"partialFileLeft", "ErrorWritePartialFileLeft"},
	}},
	// FilePermission is maxon's: the mode of a file that a write creates, 0666 or 0755 on a system that has modes.
	{Key: "FilePermission", Name: "FilePermission", Cases: []enumCase{
		{"normal", "FilePermissionNormal"},
		{"executable", "FilePermissionExecutable"},
	}},
	// SendError is what a send to a service fails with: maxon's cases of `<Service>.<message>.sendError` but mailboxFull, since a
	// mailbox has no capacity, whose index plus 1 is the word rt.service.reserve gives for each.
	{Key: "SendError", Name: "SendError", Cases: []enumCase{
		{"serviceStopped", "SendServiceStopped"},
	}},
	{Key: "FileDeleteError", Name: "FileDeleteError", Cases: []enumCase{
		{"notFound", "ErrorDeleteNotFound"},
		{"accessDenied", "ErrorDeleteAccessDenied"},
		{"busy", "ErrorDeleteBusy"},
		{"failed", "ErrorDeleteFailed"},
	}},
	{Key: "FileRenameError", Name: "FileRenameError", Cases: []enumCase{
		{"failed", "ErrorRenameFailed"},
		{"notFound", "ErrorRenameNotFound"},
		{"accessDenied", "ErrorRenameAccessDenied"},
		{"busy", "ErrorRenameBusy"},
		{"alreadyExists", "ErrorRenameAlreadyExists"},
	}},
	{Key: "FileInfoError", Name: "FileInfoError", Cases: []enumCase{
		{"notFound", "ErrorInfoNotFound"},
		{"accessDenied", "ErrorInfoAccessDenied"},
		{"busy", "ErrorInfoBusy"},
		{"failed", "ErrorInfoFailed"},
	}},
	{Key: "DirectoryListError", Name: "DirectoryListError", Cases: []enumCase{
		{"notFound", "ErrorListNotFound"},
		{"accessDenied", "ErrorListAccessDenied"},
		{"listFailed", "ErrorListFailed"},
	}},
	{Key: "DirectoryDeleteError", Name: "DirectoryDeleteError", Cases: []enumCase{
		{"notFound", "ErrorDirectoryDeleteNotFound"},
		{"notEmpty", "ErrorDirectoryDeleteNotEmpty"},
		{"deleteFailed", "ErrorDirectoryDeleteFailed"},
	}},
	{Key: "ProcessIntrospectionError", Name: "ProcessIntrospectionError", Cases: []enumCase{
		{"pathUnavailable", "ErrorPathUnavailable"},
		{"variableUnset", "ErrorVariableUnset"},
		{"environmentUnreadable", "ErrorEnvironmentUnreadable"},
	}},
	// SubprocessError is maxon's union without its payloads, since an error here is an enum.
	{Key: "SubprocessError", Name: "SubprocessError", Cases: []enumCase{
		{"executableNotFound", "ErrorExecutableNotFound"},
		{"spawnFailed", "ErrorSpawnFailed"},
		{"busy", "ErrorSpawnBusy"},
		{"ioFailed", "ErrorIoFailed"},
		{"timeout", "ErrorTimeout"},
		{"treeNotEnded", "ErrorTreeNotEnded"},
		{"detachRefused", "ErrorDetachRefused"},
		{"inputTooLarge", "ErrorInputTooLarge"},
		{"statusLost", "ErrorStatusLost"},
	}},
	// SubprocessStreamState is maxon's (stdlib/Builtins.maxon): what a read of a streaming child's output or error answers next.
	{Key: "SubprocessStreamState", Name: "__SubprocessStreamState", Cases: []enumCase{
		{"open", "StreamStateOpen"},
		{"atEof", "StreamStateAtEof"},
		{"readFailed", "StreamStateReadFailed"},
		{"noSuchChild", "StreamStateNoSuchChild"},
	}},
	// DebuggeeError is maxon's: what a read or a write of a debuggee's memory or registers fails with.
	{Key: "DebuggeeError", Name: "DebuggeeError", Cases: []enumCase{
		{"unreadable", "ErrorDebuggeeUnreadable"},
		{"unwritable", "ErrorDebuggeeUnwritable"},
		{"outOfBounds", "ErrorDebuggeeOutOfBounds"},
	}},
	// DebugDisposition is maxon's: whether the debugger handled the event it continues past, or hands an exception on to
	// the program.
	{Key: "DebugDisposition", Name: "DebugDisposition", Cases: []enumCase{
		{"handled", "DispositionHandled"},
		{"unhandled", "DispositionUnhandled"},
	}},
	// CharacterError is maxon's (stdlib/Character.maxon): what Character.asciiValue throws for a character that is no ASCII byte.
	{Key: "CharacterError", Name: "CharacterError", Cases: []enumCase{
		{"notAscii", "ErrorCharacterNotAscii"},
	}},
	{Key: "StringError", Name: "StringError", Cases: []enumCase{
		{"notFound", "ErrorStringNotFound"},
		{"invalidIndex", "ErrorStringInvalidIndex"},
	}},
	// IterationError is what a type's iteration protocol throws (`createIterator()` on an empty collection, `advance()`
	// past the last element), which a for loop takes as its end.
	{Key: "IterationError", Name: "IterationError", Cases: []enumCase{
		{"exhausted", "ErrorIterationExhausted"},
		{"atStart", "ErrorIterationAtStart"},
	}},
	// CastError is minimaxon's own: what a cast of an interface value to a type fails with when the value holds one of
	// another type, where maxon has no such cast.
	{Key: "CastError", Name: "CastError", Cases: []enumCase{
		{"otherType", "ErrorCastOtherType"},
	}},
	// EnumLookupError is maxon's, declared by no library file: what an enum's fromRawValue throws, in maxon's order.
	{Key: "EnumLookupError", Name: "__EnumLookupError", Cases: []enumCase{
		{"noSuchRawValue", "ErrorNoSuchRawValue"},
		{"noSuchCaseName", "ErrorNoSuchCaseName"},
	}},
}

// capabilities are the interfaces a bound can name and no declaration implements: a type has one by its parts.
var capabilities = []string{"Copyable", "Droppable"}

// markers are the interfaces with no methods that a declaration says it implements: Error, which an error enum does, and
// InitableFromStringLiteral, which a type with a static `init(value String)` does to be written `Type from "text"`.
var markers = []string{"Error", "InitableFromStringLiteral"}

// function is a built-in function or method.
type function struct {
	// Owner is the Key of the type it is a method of, "String" for a method of String, or "" for a free function.
	Owner  string
	Name   string
	Static bool
	// Self is how a method takes its receiver: "", "mutates" or "consumes".
	Self string
	// Params are "convention name Type", the convention left out when it is let, and "name:" for one that a call labels
	// even where it is the first argument, "name?" for one after the first that a call may also give by its place. A let parameter of an enum row may end in " = case", the case a call that leaves the
	// argument out gives.
	Params []string
	// Result is the type it returns; "" is Void.
	Result string
	// Generic is the type parameter of a free function, "T" or "T Bound".
	Generic string
	// Bounds are bounds on the owner's parameters, "T Bound".
	Bounds []string
	// Throws is the Key of the enum row it throws, which its lowering fails with as a compiled function's code does.
	Throws string
	// Intrinsic is what it lowers to: ast.Intrinsic+Intrinsic, ir's lower+Intrinsic.
	Intrinsic string
	// Lends is whether the method gives a place in its receiver, or a view of a String's bytes, rather than a value, as a
	// function that returns borrowed does.
	Lends bool
	// Place is, for a method that Lends, whether what it lends is a place in its receiver with a word of its own, which it
	// lends with the access its caller has to the receiver, at its address (ast.lendsByAddress), rather than a view.
	Place bool
	// Field is the ast.Builtins field that holds a free function which is not declared by its name, or a method that the
	// checker chooses for a call of the name in place of the one declared by it: an overload by the arguments' form.
	Field string
	// Checked is whether the checker refuses arguments that the signature alone cannot, in check+Intrinsic+Args.
	Checked bool
	// OwnsOperands is whether the lowering evaluates the receiver and arguments itself rather than being given them.
	OwnsOperands bool
	// MayOverlap is whether an argument of a call may name the receiver's place (E3186 otherwise): the lowering takes the
	// arguments' values before it writes the place.
	MayOverlap bool
	// RuntimeOnly is whether only the runtime module may call it (E3152): a raw operation with no runtime between it and
	// the machine.
	RuntimeOnly bool
	// Instead is what a program calls in place of a static function of a record whose calls are raw (RawCalls), which
	// E3185 names.
	Instead string
	// Raw makes a method a door of maxon's raw surface, which only the library tiers may call (E3185).
	Raw *rawSurface
}

// synthesizedIntrinsics are what the methods that the checker gives a declaration of the program lower to, which no row
// declares: a payload-free enum's hash() and equals(other), on its ordinal (check/enums.maxon), and the static
// fromRawValue(raw) of an enum whose cases write raw values or none (check/enumlookup.maxon).
var synthesizedIntrinsics = []string{"EnumHash", "EnumEquals", "EnumFromRawValue"}

// maxImportArgs is the most words a DLL import of the runtime takes: CreateProcessA's ten.
const maxImportArgs = 10

var functions = append(slices.Clone(tableFunctions), rawFunctions()...)

// rawFunctions are the __Raw intrinsics (runtime/README.md). An address and a word are a Count, the unsigned 64-bit
// word; a load gives the bytes zero-extended and a store keeps the low bytes of its value. A DLL import is called by its
// name, a string literal, with one row for each number of word arguments, since a row has a fixed signature.
func rawFunctions() []function {
	raw := func(name string, params []string, result, intrinsic string) function {
		return function{Owner: "Raw", Name: name, Static: true, Params: params, Result: result, Intrinsic: intrinsic, RuntimeOnly: true}
	}
	// An operand that is a place, or a literal that the lowering reads as text, is not a value the caller evaluates.
	own := func(f function) function {
		f.OwnsOperands = true
		return f
	}
	checked := func(f function) function {
		f.Checked = true
		return f
	}
	at := []string{"address Count", "offset Count"}
	fs := []function{
		raw("loadByte", at, "Byte", "RawLoad8"),
		raw("loadWord16", at, "Count", "RawLoad16"),
		raw("loadWord32", at, "Count", "RawLoad32"),
		raw("loadWord", at, "Count", "RawLoad64"),
		raw("storeByte", append(slices.Clone(at), "value Byte"), "", "RawStore8"),
		raw("storeWord16", append(slices.Clone(at), "value Count"), "", "RawStore16"),
		raw("storeWord32", append(slices.Clone(at), "value Count"), "", "RawStore32"),
		raw("storeWord", append(slices.Clone(at), "value Count"), "", "RawStore64"),
		// The ordered word accesses are how a word that another thread reads or writes without a lock is handed over: an
		// acquire load is done before every access after it, and a release store after every access before it.
		raw("loadAcquire", at, "Count", "RawLoadAcquire"),
		raw("storeRelease", append(slices.Clone(at), "value Count"), "", "RawStoreRelease"),
		raw("wrappingAdd", []string{"left Count", "right Count"}, "Count", "RawWrappingAdd"),
		raw("wrappingSub", []string{"left Count", "right Count"}, "Count", "RawWrappingSub"),
		// mulHigh is the high word of the 128-bit product of two words of no sign, which the hash's fold is made of.
		raw("mulHigh", []string{"left Count", "right Count"}, "Count", "RawMulHigh"),
		own(raw("addressOf", []string{"mutates word Count"}, "Count", "RawAddressOf")),
		// The String operations that say nothing of its layout, which the compiler compiling the runtime lays out as it
		// lays out a String, so one runtime source serves the compiler of either layout (the seed's among them): the
		// address of a borrowed string's first byte, its length in bytes and whether each byte is known to be below
		// 0x80 (false where nothing says so); room for a number of bytes that a String will own, which no String
		// took until stringOf, and its free when none does; the String that owns the room, of its first bytes, ascii saying
		// that each is below 0x80; the bytes a String takes in memory, an element of an array of strings; that String
		// stored in memory; and the first byte and the length of a String held in memory, which stays there.
		raw("stringBytes", []string{"text String"}, "Count", "RawStringBytes"),
		raw("stringLength", []string{"text String"}, "Count", "RawStringLength"),
		raw("stringAscii", []string{"text String"}, "Bool", "RawStringAscii"),
		raw("stringRoom", []string{"capacity Count"}, "Count", "RawStringRoom"),
		raw("freeStringRoom", []string{"bytes Count"}, "", "RawFreeStringRoom"),
		raw("stringOf", []string{"bytes Count", "length Count", "ascii Bool"}, "String", "RawStringOf"),
		raw("stringSize", nil, "Count", "RawStringSize"),
		raw("storeString", append(slices.Clone(at), "bytes Count", "length Count", "ascii Bool"), "", "RawStoreString"),
		raw("stringBytesAt", at, "Count", "RawStringBytesAt"),
		raw("stringLengthAt", at, "Count", "RawStringLengthAt"),
		// The two words of a String as they are, and the String of two words, which only the runtime's source that knows
		// a String that holds its bytes in its words (runtime/inline.maxon) and a Character's (runtime/characters.maxon) say.
		raw("stringFirstWord", []string{"text String"}, "Count", "RawStringFirstWord"),
		raw("stringSecondWord", []string{"text String"}, "Count", "RawStringSecondWord"),
		raw("stringOfWords", []string{"first Count", "second Count"}, "String", "RawStringOfWords"),
		raw("heapAddress", nil, "Count", "RawHeapAddress"),
		raw("stdoutAddress", nil, "Count", "RawStdoutAddress"),
		raw("stderrAddress", nil, "Count", "RawStderrAddress"),
		// The atomic operations are full fences and give the word they found at the address.
		raw("atomicAdd", append(slices.Clone(at), "value Count"), "Count", "RawAtomicAdd"),
		raw("atomicExchange", append(slices.Clone(at), "value Count"), "Count", "RawAtomicExchange"),
		raw("compareExchange", append(slices.Clone(at), "expected Count", "desired Count"), "Count", "RawCompareExchange"),
		raw("spinPause", nil, "", "RawSpinPause"),
		// copyBytes copies count bytes from source to target with the copy routines' copy (rCopyBytes): count is above 0
		// (a caller with nothing to copy does not call), and the two do not overlap, so a move within one buffer, as an
		// array's gaps are moved, is not one.
		raw("copyBytes", []string{"source Count", "target Count", "count Count"}, "", "RawCopyBytes"),
		raw("threadContext", nil, "Count", "RawThreadContext"),
		// callCode1 calls the code at an address with one word, as a behaviour's thunk is called with its record.
		raw("callCode1", []string{"code Count", "arg1 Count"}, "Count", "RawCallCode"),
		// callHost2 calls the code at an address with two words by the host convention, Win64, as an import is called:
		// a DLL function the import table does not name, which the runtime found by its name.
		raw("callHost2", []string{"code Count", "arg1 Count", "arg2 Count"}, "Count", "RawCallHost"),
		raw("threadStartAddress", nil, "Count", "RawThreadStartAddress"),
		// The addresses of the code of the entry stub that green threads switch and are stopped through, which only a
		// callCode1 or the registers of a stack the runtime seeds reach, and of the link's table of preemptible code.
		raw("stackSwitchAddress", nil, "Count", "RawStackSwitchAddress"),
		raw("greenStartAddress", nil, "Count", "RawGreenStartAddress"),
		raw("asyncPreemptAddress", nil, "Count", "RawAsyncPreemptAddress"),
		raw("preemptibleCodeAddress", nil, "Count", "RawPreemptibleCodeAddress"),
		// codeAddress is the address of the code of the function of the runtime that a string literal names, which takes
		// one word: the run of a behaviour's record that a test of the scheduler builds by hand.
		checked(own(raw("codeAddress", []string{"name String"}, "Count", "RawCodeAddress"))),
	}
	for n := 0; n <= maxImportArgs; n++ {
		params := []string{"name String"}
		for i := 1; i <= n; i++ {
			params = append(params, fmt.Sprintf("arg%d Count", i))
		}
		f := raw(fmt.Sprintf("callImport%d", n), params, "Count", "RawCallImport")
		f.Checked = true
		fs = append(fs, own(f))
	}
	// A system call of a target whose programs call the kernel themselves takes its number and up to maxSyscallArgs
	// words, and gives what the kernel returns: a negative errno, as a word, when it failed.
	for n := 0; n <= maxSyscallArgs; n++ {
		params := []string{"number Count"}
		for i := 1; i <= n; i++ {
			params = append(params, fmt.Sprintf("arg%d Count", i))
		}
		fs = append(fs, raw(fmt.Sprintf("syscall%d", n), params, "Count", "RawSyscall"))
	}
	// processStartAddress is the address of the global where the entry of a Linux executable keeps the stack pointer the
	// kernel started the process with, which points at the argument count, the arguments and the environment.
	fs = append(fs, raw("processStartAddress", nil, "Count", "RawProcessStartAddress"))
	// preemptHandlerAddress and signalReturnAddress are the handler of the Linux preemption signal and the restorer it
	// returns through, which the runtime's rt_sigaction names.
	fs = append(fs, raw("preemptHandlerAddress", nil, "Count", "RawPreemptHandlerAddress"))
	fs = append(fs, raw("signalReturnAddress", nil, "Count", "RawSignalReturnAddress"))
	return fs
}

// maxSyscallArgs is the most words a system call takes, as many as the kernel's convention passes in registers.
const maxSyscallArgs = 6

var tableFunctions = []function{
	{Owner: "Array", Name: "create", Static: true, Params: []string{"count ElementIndex"}, Result: "Self", Intrinsic: "ArrayCreate", Checked: true},
	// create(n, zeroed:) is maxon's one allocation with a stated capacity: zeroed: true is create(n), and zeroed: false an empty
	// array with room for n, which is legal for every element. The flag is a literal.
	{Owner: "Array", Name: "create", Static: true, Params: []string{"count ElementIndex", "zeroed Bool"}, Result: "Self", Intrinsic: "ArrayCreateZeroed", Field: "ArrayCreateZeroed", Checked: true},
	// init makes the array of a raw buffer, which is an array's box. As in maxon the call is no door of its own: the buffer's
	// type is, which only the library tiers name.
	{Owner: "Array", Name: "init", Static: true, Params: []string{"consumes managed ManagedMemory with T"}, Result: "Self", Intrinsic: "ArrayFromMemory"},
	{Owner: "Array", Name: "filled", Static: true, Params: []string{"count ElementIndex", "consumes value T"}, Result: "Self", Bounds: []string{"T Copyable"}, Intrinsic: "ArrayFilled"},
	{Owner: "Array", Name: "count", Result: "Count", Intrinsic: "ArrayCount"},
	{Owner: "Array", Name: "isEmpty", Result: "Bool", Intrinsic: "ArrayIsEmpty"},
	{Owner: "Array", Name: "capacity", Result: "ReportedCapacity", Intrinsic: "ArrayCapacity"},
	{Owner: "Array", Name: "get", Params: []string{"index ElementIndex"}, Result: "T", Bounds: []string{"T Copyable"}, Throws: "ArrayError", Intrinsic: "ArrayGet"},
	{Owner: "Array", Name: "set", Self: "mutates", Params: []string{"index ElementIndex", "consumes value T"}, Bounds: []string{"T Droppable"}, Throws: "ArrayError", Intrinsic: "ArraySet"},
	{Owner: "Array", Name: "push", Self: "mutates", Params: []string{"consumes value T"}, Intrinsic: "ArrayPush"},
	{Owner: "Array", Name: "append", Self: "mutates", Params: []string{"other Self"}, Bounds: []string{"T Copyable"}, Intrinsic: "ArrayAppend"},
	{Owner: "Array", Name: "insert", Self: "mutates", Params: []string{"at ElementIndex", "consumes value T"}, Intrinsic: "ArrayInsert"},
	{Owner: "Array", Name: "remove", Self: "mutates", Params: []string{"at ElementIndex"}, Result: "T", Throws: "ArrayError", Intrinsic: "ArrayRemove"},
	{Owner: "Array", Name: "pop", Self: "mutates", Result: "T", Throws: "ArrayError", Intrinsic: "ArrayPop"},
	{Owner: "Array", Name: "dropEmpty", Self: "consumes", Intrinsic: "ArrayDropEmpty"},
	// clear and truncate drop the elements they remove; reserve makes the buffer hold a count, which no part of the array's value
	// shows; resize grows with zero elements, which the checker holds to an element that has one.
	{Owner: "Array", Name: "clear", Self: "mutates", Bounds: []string{"T Droppable"}, Intrinsic: "ArrayClear"},
	{Owner: "Array", Name: "truncate", Self: "mutates", Params: []string{"newLength ElementIndex"}, Bounds: []string{"T Droppable"}, Intrinsic: "ArrayTruncate"},
	{Owner: "Array", Name: "reserve", Self: "mutates", Params: []string{"minCapacity ElementIndex"}, Intrinsic: "ArrayReserve"},
	{Owner: "Array", Name: "resize", Self: "mutates", Params: []string{"newLength ElementIndex"}, Bounds: []string{"T Droppable"}, Intrinsic: "ArrayResize", Checked: true},
	{Owner: "Array", Name: "slice", Params: []string{"start ElementIndex", "endIndex ElementIndex"}, Result: "Self", Bounds: []string{"T Copyable"}, Throws: "ArrayError", Intrinsic: "ArraySlice"},
	{Owner: "Array", Name: "growFilled", Self: "mutates", Params: []string{"newLength ElementIndex", "value T"}, Bounds: []string{"T Copyable"}, Intrinsic: "ArrayGrowFilled"},
	{Owner: "Array", Name: "refill", Self: "mutates", Params: []string{"newLength ElementIndex", "value T"}, Bounds: []string{"T Copyable"}, Intrinsic: "ArrayRefill"},
	// clearSlot is maxon's tombstone of a slot table: the element at an index is dropped and its slot left empty, where
	// remove would close the gap. An empty slot of a managed element is 0, which get reads as emptySlot.
	{Owner: "Array", Name: "clearSlot", Self: "mutates", Params: []string{"at ElementIndex"}, Bounds: []string{"T Droppable"}, Throws: "ArrayError", Intrinsic: "ArrayClearSlot", Raw: &rawSurface{What: "an array's slot is emptied in place", Instead: "use 'remove(at)'"}},
	// fillSlot and takeSlot are clearSlot's other halves, which a slot table needs for an element that has no Droppable: fillSlot
	// moves a value into an empty slot and drops nothing, trapping where the index is past the end or the slot holds an
	// element, and takeSlot moves the element out and leaves the slot empty, failing with indexOutOfBounds past the end and
	// emptySlot at an empty slot.
	{Owner: "Array", Name: "fillSlot", Self: "mutates", Params: []string{"at ElementIndex", "consumes value T"}, Intrinsic: "ArrayFillSlot", Raw: &rawSurface{What: "an array's empty slot is filled in place", Instead: "use 'set(i, value: v)'"}},
	{Owner: "Array", Name: "takeSlot", Self: "mutates", Params: []string{"at ElementIndex"}, Result: "T", Throws: "ArrayError", Intrinsic: "ArrayTakeSlot", Raw: &rawSurface{What: "an element is taken out of an array's slot, which is left empty", Instead: "use 'remove(at)'"}},
	// lendSlot lends the element at an index, to read or to change, trapping past the end and at an empty slot as a subscript
	// does: the one unchecked access that a library collection's checked get lends through. It is provisional, the
	// library's alone, until the subscript goes and Array's own get lends.
	{Owner: "Array", Name: "lendSlot", Params: []string{"at ElementIndex"}, Result: "T", Lends: true, Place: true, Intrinsic: "ArrayLendSlot", Raw: &rawSurface{What: "an array's element is lent in place", Instead: "use a subscript, 'a[i]'"}},
	// hashesAsStorage and storageHash are maxon's __Builtins.elementsHashAsStorage() and __Builtins.hashArrayStorage(self),
	// which cmd/convert writes as them: whether the array hashes as the fold of the bytes of its storage (ast.storageHashes),
	// and that fold under a seed, the library's alone.
	{Owner: "Array", Name: "hashesAsStorage", Result: "Bool", Intrinsic: "ArrayHashesAsStorage", Raw: &rawSurface{What: "an array is asked whether it hashes as its storage", Instead: "use 'hash()'"}},
	{Owner: "Array", Name: "storageHash", Params: []string{"seed HashDigest"}, Result: "HashDigest", Intrinsic: "ArrayStorageHash", Raw: &rawSurface{What: "the storage of an array is hashed", Instead: "use 'hash()'"}},
	// contains, containsSequence and equals are maxon's for an element that is Equatable; the checker holds them to the
	// elements this compiler compares itself (checkElementsComparable), and the library's equalsByElements is an array's
	// equals of any other element (ast.runsBuiltin).
	{Owner: "Array", Name: "contains", Params: []string{"element T"}, Result: "Bool", Intrinsic: "ArrayContains", Checked: true},
	{Owner: "Array", Name: "containsSequence", Params: []string{"sequence Self"}, Result: "Bool", Intrinsic: "ArrayContainsSequence", Checked: true},
	{Owner: "Array", Name: "equals", Params: []string{"other Self"}, Result: "Bool", Intrinsic: "ArrayEquals", Checked: true}, {Owner: "Array", Name: "first", Result: "T", Bounds: []string{"T Copyable"}, Throws: "ArrayError", Intrinsic: "ArrayFirst"},
	{Owner: "Array", Name: "last", Result: "T", Bounds: []string{"T Copyable"}, Throws: "ArrayError", Intrinsic: "ArrayLast"},

	// A FixedArray is an array's box of its count of elements, which never grows. create() makes the count its type states,
	// zeroed, and create(value:) copies of the value; a runtime-sized one takes the count, create(n) and create(n, value:),
	// which the checker chooses for a call of create by its arguments. get and set at an index the checker proves inside
	// the count are the rows that cannot fail.
	{Owner: "FixedArray", Name: "create", Static: true, Result: "Self", Intrinsic: "FixedArrayCreate", Checked: true},
	{Owner: "FixedArray", Name: "create", Static: true, Params: []string{"consumes value: T"}, Result: "Self", Bounds: []string{"T Copyable"}, Intrinsic: "FixedArrayFilled", Field: "FixedArrayCreateFilled"},
	{Owner: "FixedArray", Name: "create", Static: true, Params: []string{"count ElementIndex"}, Result: "Self", Intrinsic: "FixedArrayCreateCounted", Field: "FixedArrayCreateCounted", Checked: true},
	{Owner: "FixedArray", Name: "create", Static: true, Params: []string{"count ElementIndex", "consumes value T"}, Result: "Self", Bounds: []string{"T Copyable"}, Intrinsic: "ArrayFilled", Field: "FixedArrayCreateCountedFilled"},
	{Owner: "FixedArray", Name: "count", Result: "Count", Intrinsic: "FixedArrayCount"},
	{Owner: "FixedArray", Name: "get", Params: []string{"index ElementIndex"}, Result: "T", Bounds: []string{"T Copyable"}, Throws: "ArrayError", Intrinsic: "ArrayGet"},
	{Owner: "FixedArray", Name: "get", Params: []string{"index ElementIndex"}, Result: "T", Bounds: []string{"T Copyable"}, Intrinsic: "FixedArrayGetProven", Field: "FixedArrayGetProven"},
	{Owner: "FixedArray", Name: "set", Self: "mutates", Params: []string{"index ElementIndex", "consumes value T"}, Bounds: []string{"T Droppable"}, Throws: "ArrayError", Intrinsic: "ArraySet"},
	{Owner: "FixedArray", Name: "set", Self: "mutates", Params: []string{"index ElementIndex", "consumes value T"}, Bounds: []string{"T Droppable"}, Intrinsic: "FixedArraySetProven", Field: "FixedArraySetProven"},
	{Owner: "FixedArray", Name: "refill", Self: "mutates", Params: []string{"value T"}, Bounds: []string{"T Copyable"}, Intrinsic: "FixedArrayRefill"},

	// A buffer's length is the count of its elements, and get gives a copy of the one at an index below it, as an array's do.
	{Owner: "ManagedMemory", Name: "length", Result: "Count", Intrinsic: "ArrayCount"},
	{Owner: "ManagedMemory", Name: "get", Params: []string{"index ElementIndex"}, Result: "T", Bounds: []string{"T Copyable"}, Throws: "ManagedMemoryError", Intrinsic: "ArrayGet"},
	// byteAt is maxon's name for get of a buffer of bytes, which the library reads an answer of the runtime by. maxon's reads the
	// bytes of any element that holds no address; here only a buffer of bytes has it, since every other element is read by get.
	{Owner: "ManagedMemory", Name: "byteAt", Params: []string{"index ElementIndex"}, Result: "Byte", Throws: "ManagedMemoryError", Intrinsic: "ManagedByteAt", Checked: true},
	// The buffer operations of maxon that an array operation does the same as, each of which throws where maxon's does. A
	// buffer is the array here, so set is bounded by the length where maxon's is by the capacity, slice copies where maxon's is
	// a view of the parent, and grow is reserve, which changes nothing a program sees. What has no counterpart is not
	// provided: setByte, which writes a buffer under another type than its element's, a buffer of no element type, a view that
	// shares a buffer, and a write that detaches a shared buffer.
	{Owner: "ManagedMemory", Name: "capacity", Result: "ReportedCapacity", Intrinsic: "ArrayCapacity"},
	// elementSize is the bytes an element takes, what sizeof gives for it: a constant of the instance, which maxon's driftsort
	// sizes its scratch by. No element is bit-packed here, so it is never maxon's negative bit width.
	{Owner: "ManagedMemory", Name: "elementSize", Result: "FullRangeInt", Intrinsic: "ArrayElementSize"},
	{Owner: "ManagedMemory", Name: "set", Self: "mutates", Params: []string{"index ElementIndex", "consumes value? T"}, Bounds: []string{"T Droppable"}, Throws: "ManagedMemoryError", Intrinsic: "ArraySet"},
	// swap exchanges two elements where they are: neither is copied or dropped, so no element needs a bound. It is the buffer's
	// row, and maxon's Array.swap, which its sorts call, is library code over it.
	{Owner: "ManagedMemory", Name: "swap", Self: "mutates", Params: []string{"i ElementIndex", "j? ElementIndex"}, Throws: "ManagedMemoryError", Intrinsic: "ArraySwap"},
	{Owner: "ManagedMemory", Name: "append", Self: "mutates", Params: []string{"other Self"}, Bounds: []string{"T Copyable"}, Intrinsic: "ArrayAppend"},
	{Owner: "ManagedMemory", Name: "remove", Self: "mutates", Params: []string{"at ElementIndex"}, Result: "T", Throws: "ManagedMemoryError", Intrinsic: "ArrayRemove"},
	{Owner: "ManagedMemory", Name: "clear", Self: "mutates", Bounds: []string{"T Droppable"}, Intrinsic: "ArrayClear"},
	{Owner: "ManagedMemory", Name: "slice", Params: []string{"start ElementIndex", "endIndex? ElementIndex"}, Result: "Self", Bounds: []string{"T Copyable"}, Throws: "ManagedMemoryError", Intrinsic: "ArraySlice"},
	{Owner: "ManagedMemory", Name: "grow", Self: "mutates", Params: []string{"newCapacity ElementIndex"}, Intrinsic: "ArrayReserve"},
	{Owner: "ManagedMemory", Name: "setLength", Self: "mutates", Params: []string{"newLength ElementIndex"}, Bounds: []string{"T Droppable"}, Intrinsic: "ArrayResize", Checked: true},
	// A cursor starts at the first element, so an empty buffer has none; a move that would leave the elements fails and
	// leaves it where it was, so a cursor always names an element, which current() lends.
	{Owner: "ManagedMemory", Name: "createCursor", Result: "ManagedMemoryCursor with T", Throws: "ManagedMemoryError", Intrinsic: "CursorCreate"},
	{Owner: "ManagedMemoryCursor", Name: "current", Result: "T", Lends: true, Intrinsic: "CursorCurrent"},
	{Owner: "ManagedMemoryCursor", Name: "index", Result: "ElementIndex", Intrinsic: "CursorIndex"},
	{Owner: "ManagedMemoryCursor", Name: "advance", Self: "mutates", Throws: "ManagedMemoryError", Intrinsic: "CursorAdvance"},
	{Owner: "ManagedMemoryCursor", Name: "retreat", Self: "mutates", Throws: "ManagedMemoryError", Intrinsic: "CursorRetreat"},
	{Owner: "ManagedMemoryCursor", Name: "seek", Self: "mutates", Params: []string{"index ElementIndex"}, Throws: "ManagedMemoryError", Intrinsic: "CursorSeek"},
	{Owner: "ManagedMemoryCursor", Name: "peek", Params: []string{"ahead ElementIndex"}, Result: "T", Bounds: []string{"T Copyable"}, Throws: "ManagedMemoryError", Intrinsic: "CursorPeek"},
	{Owner: "StringCursor", Name: "create", Static: true, Params: []string{"text String"}, Result: "Self", Throws: "ManagedMemoryError", Intrinsic: "StringCursorCreate"},
	{Owner: "StringCursor", Name: "current", Result: "String", Lends: true, Intrinsic: "StringCursorCurrent"},
	{Owner: "StringCursor", Name: "advance", Self: "mutates", Throws: "ManagedMemoryError", Intrinsic: "StringCursorAdvance"},

	// A buffer of groups is made of empty pairs, each group's control word the one given, and keeps its count of groups. A
	// pair is named by its index over every group, eight to a group. Each half of a pair is lent, written and taken on its
	// own; a write drops what the half held, a take leaves it empty, and an index past the groups traps, as a subscript of an
	// array does.
	{Owner: "GroupMemory", Name: "create", Static: true, Params: []string{"groups ElementIndex", "control Count"}, Result: "Self", Intrinsic: "GroupsCreate"},
	{Owner: "GroupMemory", Name: "groups", Result: "Count", Intrinsic: "GroupsCount"},
	{Owner: "GroupMemory", Name: "control", Params: []string{"group ElementIndex"}, Result: "Count", Intrinsic: "GroupsControl"},
	{Owner: "GroupMemory", Name: "setControl", Self: "mutates", Params: []string{"group ElementIndex", "value Count"}, Intrinsic: "GroupsSetControl"},
	{Owner: "GroupMemory", Name: "first", Params: []string{"at ElementIndex"}, Result: "A", Lends: true, Intrinsic: "GroupsFirst"},
	{Owner: "GroupMemory", Name: "second", Params: []string{"at ElementIndex"}, Result: "B", Lends: true, Intrinsic: "GroupsSecond"},
	{Owner: "GroupMemory", Name: "put", Self: "mutates", Params: []string{"at ElementIndex", "consumes first A", "consumes second B"}, Bounds: []string{"A Droppable", "B Droppable"}, Intrinsic: "GroupsPut"},
	{Owner: "GroupMemory", Name: "setSecond", Self: "mutates", Params: []string{"at ElementIndex", "consumes value B"}, Bounds: []string{"B Droppable"}, Intrinsic: "GroupsSetSecond"},
	{Owner: "GroupMemory", Name: "takeFirst", Self: "mutates", Params: []string{"at ElementIndex"}, Result: "A", Intrinsic: "GroupsTakeFirst"},
	{Owner: "GroupMemory", Name: "takeSecond", Self: "mutates", Params: []string{"at ElementIndex"}, Result: "B", Intrinsic: "GroupsTakeSecond"},
	{Owner: "GroupMemory", Name: "clear", Self: "mutates", Params: []string{"at ElementIndex"}, Bounds: []string{"A Droppable", "B Droppable"}, Intrinsic: "GroupsClear"},

	// fromWord and word are the library's door to a slot key's word, which a slot map of the library packs its slot and the
	// slot's generation into; each is the word itself.
	{Owner: "SlotKey", Name: "fromWord", Static: true, Params: []string{"word Count"}, Result: "Self", Intrinsic: "SlotKeyFromWord", Raw: &rawSurface{What: "a slot key is made of a word", Instead: "use the slot key a slot map's 'insert' gives"}},
	{Owner: "SlotKey", Name: "word", Result: "Count", Intrinsic: "SlotKeyWord", Raw: &rawSurface{What: "a slot key's word is read", Instead: "compare slot keys with '=='"}},

	{Owner: "String", Name: "byteLength", Result: "Count", Intrinsic: "StringByteLength"},
	{Owner: "String", Name: "byteAt", Params: []string{"index Count"}, Result: "Byte", Intrinsic: "StringByteAt"},
	// sliceBytes is maxon's name for the byte range, which keeps slice for a range of graphemes. It and the other slicing
	// methods (slice, sliceLength and the trims) lend a view of the receiver's bytes, which the caller copies to keep.
	{Owner: "String", Name: "sliceBytes", Params: []string{"start Count", "endPos Count"}, Result: "String", Lends: true, Intrinsic: "StringSliceBytes"},
	{Owner: "String", Name: "toByteArray", Result: "Array with Byte", Intrinsic: "StringToByteArray"},
	{Owner: "String", Name: "from", Static: true, Params: []string{"bytes Array with Byte"}, Result: "String", Intrinsic: "StringFrom"},
	// byteAtOrPanic is byteAt for a caller that checked the index, as maxon's library names it; byteAt here traps past the
	// end already.
	{Owner: "String", Name: "byteAtOrPanic", Params: []string{"index Count"}, Result: "Byte", Intrinsic: "StringByteAt"},
	// The library's doors to a String's bytes as a buffer: maxon's addressableBytes() lends the String's own buffer, and
	// init(managed) wraps a buffer as a String. A String here owns its bytes, which the String itself may hold where they
	// are few, so each is a copy, read through the String's bytes as every member reads them.
	{Owner: "String", Name: "addressableBytes", Result: "ManagedMemory with Byte", Intrinsic: "StringToByteArray", Raw: &rawSurface{What: "a String's bytes are taken as a raw '__ManagedMemory' buffer", Instead: "use 'toByteArray()' or 'bytes()'"}},
	{Owner: "String", Name: "init", Static: true, Params: []string{"managed ManagedMemory with Byte"}, Result: "String", Intrinsic: "StringFrom", Raw: &rawSurface{What: "a String is made of a raw '__ManagedMemory' buffer", Instead: "use 'String.from(bytes)'"}},
	// A cstring is the two words of the String it was made of, a distinct type that only the library tiers name: cstr() hands
	// them over as they are and costs nothing, and a runtime row that takes one makes the zero-terminated copy it needs
	// itself and frees it before it returns. fromCString is its inverse, a copy of the bytes before the first NUL.
	{Owner: "String", Name: "cstr", Result: "CString", Intrinsic: "StringCStr", Raw: &rawSurface{What: "a String's bytes are taken as a NUL-terminated 'cstring'", Instead: "pass the String itself"}},
	{Owner: "String", Name: "fromCString", Static: true, Params: []string{"cs CString"}, Result: "String", Intrinsic: "StringFromCString", Raw: &rawSurface{What: "a String is made of a NUL-terminated 'cstring'", Instead: "use 'String.from(bytes)'"}},
	{Owner: "String", Name: "bytes", Result: "ByteView", Intrinsic: "StringBytes"},
	{Owner: "String", Name: "codepoints", Result: "CodepointView", Intrinsic: "StringCodepoints"},
	{Owner: "String", Name: "utf16", Result: "Utf16View", Intrinsic: "StringUtf16"},
	{Owner: "ByteView", Name: "createIterator", Result: "ByteIterator", Throws: "IterationError", Intrinsic: "ByteViewCreateIterator"},
	{Owner: "ByteView", Name: "count", Result: "Count", Intrinsic: "ByteViewCount"},
	{Owner: "ByteIterator", Name: "current", Result: "Byte", Intrinsic: "ByteIteratorCurrent"},
	{Owner: "ByteIterator", Name: "advance", Self: "mutates", Throws: "IterationError", Intrinsic: "ByteIteratorAdvance"},
	{Owner: "CodepointView", Name: "createIterator", Result: "CodepointIterator", Throws: "IterationError", Intrinsic: "CodepointViewCreateIterator"},
	{Owner: "CodepointView", Name: "count", Result: "Count", Intrinsic: "CodepointViewCount"},
	{Owner: "CodepointIterator", Name: "current", Result: "Codepoint", Intrinsic: "CodepointIteratorCurrent"},
	{Owner: "CodepointIterator", Name: "advance", Self: "mutates", Throws: "IterationError", Intrinsic: "CodepointIteratorAdvance"},
	{Owner: "Utf16View", Name: "createIterator", Result: "Utf16Iterator", Throws: "IterationError", Intrinsic: "Utf16ViewCreateIterator"},
	{Owner: "Utf16View", Name: "count", Result: "Count", Intrinsic: "Utf16ViewCount"},
	{Owner: "Utf16Iterator", Name: "current", Result: "CodeUnit16", Intrinsic: "Utf16IteratorCurrent"},
	{Owner: "Utf16Iterator", Name: "advance", Self: "mutates", Throws: "IterationError", Intrinsic: "Utf16IteratorAdvance"},
	{Owner: "String", Name: "isEmpty", Result: "Bool", Intrinsic: "StringIsEmpty"},
	{Owner: "String", Name: "equals", Params: []string{"other String"}, Result: "Bool", Intrinsic: "StringEquals"},
	// append moves the string to a new one, joined, and stores that in the receiver's place.
	// count and every position in a string are of grapheme clusters; startIndex, endIndex and the searches give a StringIndex.
	{Owner: "String", Name: "count", Result: "Count", Intrinsic: "StringCount"},
	{Owner: "String", Name: "contains", Params: []string{"needle String"}, Result: "Bool", Intrinsic: "StringContains"},
	{Owner: "String", Name: "startsWith", Params: []string{"prefix String"}, Result: "Bool", Intrinsic: "StringStartsWith"},
	{Owner: "String", Name: "endsWith", Params: []string{"suffix String"}, Result: "Bool", Intrinsic: "StringEndsWith"},
	{Owner: "String", Name: "findFirst", Params: []string{"needle String"}, Result: "StringIndex", Throws: "StringError", Intrinsic: "StringFindFirst"},
	{Owner: "String", Name: "findLast", Params: []string{"needle String"}, Result: "StringIndex", Throws: "StringError", Intrinsic: "StringFindLast"},
	// indexAfter is the index of the grapheme cluster after the one at an index, which fails with invalidIndex at the end.
	{Owner: "String", Name: "indexAfter", Params: []string{"idx StringIndex"}, Result: "StringIndex", Throws: "StringError", Intrinsic: "StringIndexAfter"},
	{Owner: "String", Name: "startIndex", Result: "StringIndex", Intrinsic: "StringStartIndex"},
	{Owner: "String", Name: "endIndex", Result: "StringIndex", Intrinsic: "StringEndIndex"},
	{Owner: "String", Name: "slice", Params: []string{"start StringIndex", "endIndex StringIndex"}, Result: "String", Lends: true, Intrinsic: "StringSlice"},
	{Owner: "String", Name: "sliceLength", Params: []string{"start StringIndex", "length Count"}, Result: "String", Lends: true, Intrinsic: "StringSliceLength"},
	{Owner: "String", Name: "replace", Params: []string{"old String", "with String"}, Result: "String", Intrinsic: "StringReplace"},
	{Owner: "String", Name: "replaceFirst", Params: []string{"old String", "with String"}, Result: "String", Intrinsic: "StringReplaceFirst"},
	// trim takes no CharacterSet: it removes whitespace and newlines, maxon's default.
	{Owner: "String", Name: "toLower", Result: "String", Intrinsic: "StringToLower"},
	{Owner: "String", Name: "toUpper", Result: "String", Intrinsic: "StringToUpper"},
	// A Character's members that read its bytes, which are maxon's: equality and order are of the bytes, and codepoint decodes the
	// first code point of the cluster.
	{Owner: "Character", Name: "byteLength", Result: "Count", Intrinsic: "CharacterByteLength"},
	// bytes and codepoints are views of a String of the character's bytes, as maxon's are of its toString(), which the
	// interpolation "{c}" is here.
	{Owner: "Character", Name: "bytes", Result: "ByteView", Intrinsic: "CharacterBytes"},
	{Owner: "Character", Name: "codepoints", Result: "CodepointView", Intrinsic: "CharacterCodepoints"},
	{Owner: "Character", Name: "codepoint", Result: "Codepoint", Intrinsic: "CharacterCodepoint"},
	{Owner: "Character", Name: "asciiValue", Result: "AsciiValue", Throws: "CharacterError", Intrinsic: "CharacterAsciiValue"},
	{Owner: "Character", Name: "equals", Params: []string{"other Character"}, Result: "Bool", Intrinsic: "CharacterEquals"},
	// advanceBy steps over the scalar values, so the surrogate block is no step, and fromCodepoint encodes one; each panics, as
	// maxon's does, on a surrogate, which a Codepoint holds, and advanceBy past the first or the last scalar value.
	{Owner: "Character", Name: "advanceBy", Params: []string{"n CodepointDelta"}, Result: "Character", Intrinsic: "CharacterAdvanceBy"},
	{Owner: "Character", Name: "fromCodepoint", Static: true, Params: []string{"cp Codepoint"}, Result: "Character", Intrinsic: "CharacterFromCodepoint"},
	// charAt is the cluster that starts at an index, and containsCharacter whether the string holds the character's bytes.
	{Owner: "String", Name: "charAt", Params: []string{"idx StringIndex"}, Result: "Character", Intrinsic: "StringCharAt"},
	{Owner: "String", Name: "containsCharacter", Params: []string{"character Character"}, Result: "Bool", Intrinsic: "StringContainsCharacter"},
	{Owner: "StringIndex", Name: "charIndex", Result: "Count", Intrinsic: "StringIndexCharIndex"},
	{Owner: "StringIndex", Name: "bytePos", Result: "Count", Intrinsic: "StringIndexBytePos"},

	// freeze takes a value and gives an immutable frozen value, which the last owner drops; share gives another owner
	// of the same frozen value or cown, and is declared over any type because it takes either; thaw gives an owned
	// copy of what a frozen value holds. cown is the constructor of the type of that name, so it is no name of its own:
	// cown(x) calls CownNew, and cown(x, dropWith: d), whose content its destructor d ends, calls CownNewDropWith.
	{Name: "freeze", Generic: "T Droppable", Params: []string{"consumes x T"}, Result: "Frozen with T", Intrinsic: "Freeze", Checked: true},
	{Name: "share", Generic: "T", Params: []string{"x T"}, Result: "T", Intrinsic: "Share", Checked: true},
	{Name: "thaw", Generic: "T Copyable", Params: []string{"x Frozen with T"}, Result: "T", Intrinsic: "Thaw", Checked: true},
	{Name: "cown", Generic: "T Droppable", Params: []string{"consumes x T"}, Result: "Cown with T", Intrinsic: "Cown", Field: "CownNew"},
	{Name: "cown", Generic: "T", Params: []string{"consumes x T"}, Result: "Cown with T", Intrinsic: "Cown", Field: "CownNewDropWith"},
	// The float functions of maxon: trunc gives the integer toward zero and the others a float. Each takes a float of any
	// range, and an integer constant, which converts. A parameter named _ is positional, as a maxon builtin's are.
	{Name: "trunc", Params: []string{"_ Float"}, Result: "FullRangeInt", Intrinsic: "FloatTrunc"},
	{Name: "round", Params: []string{"_ Float"}, Result: "Float", Intrinsic: "FloatRound"},
	{Name: "floor", Params: []string{"_ Float"}, Result: "Float", Intrinsic: "FloatFloor"},
	{Name: "ceil", Params: []string{"_ Float"}, Result: "Float", Intrinsic: "FloatCeil"},
	{Name: "sqrt", Params: []string{"_ Float"}, Result: "Float", Intrinsic: "FloatSqrt"},
	{Name: "abs", Params: []string{"_ Float"}, Result: "Float", Intrinsic: "FloatAbs"},
	{Name: "min", Params: []string{"_ Float", "_ Float"}, Result: "Float", Intrinsic: "FloatMin"},
	{Name: "max", Params: []string{"_ Float", "_ Float"}, Result: "Float", Intrinsic: "FloatMax"},
	// exchange moves a value into a place and gives back the one that was there, so the place is never dropped.
	{Name: "exchange", Generic: "T", Params: []string{"mutates place T", "consumes value T"}, Result: "T", Intrinsic: "Exchange", OwnsOperands: true},

	// The OS functions are maxon's stdlib, names, labels, results and errors.
	{Owner: "CommandLine", Name: "args", Static: true, Result: "Array with String", Intrinsic: "CommandLineArgs"},
	{Owner: "Process", Name: "environmentVariable", Static: true, Params: []string{"name String"}, Result: "String", Throws: "ProcessIntrospectionError", Intrinsic: "ProcessEnvironmentVariable"},
	{Owner: "Process", Name: "executablePath", Static: true, Result: "FilePath", Throws: "ProcessIntrospectionError", Intrinsic: "ProcessExecutablePath"},
	// id is maxon's Process.id, whose Pid is an unsigned 64-bit integer, which Count is.
	{Owner: "Process", Name: "id", Static: true, Result: "Count", Intrinsic: "ProcessId"},
	// setExitCode is how a behaviour, which runs after main has returned, decides the code the process ends with.
	{Owner: "Process", Name: "setExitCode", Static: true, Params: []string{"code ExitCode"}, Intrinsic: "ProcessSetExitCode"},
	// exit ends the process at once with the code, as Rust's process::exit and Go's os.Exit do: nothing is dropped, no
	// other green thread or service finishes, and an executable that counts its boxes reports no leak. What was written
	// to standard output and error is out, since every write reaches them unbuffered. The checker takes a statement that
	// calls it for one that never completes, as a panic (check.exitsProcess). The ordinary way to end a program is to
	// return from main, with setExitCode for the code when behaviours run after it; maxon's library has no exit.
	{Owner: "Process", Name: "exit", Static: true, Params: []string{"code ExitCode"}, Intrinsic: "ProcessExitAtOnce"},
	// enterBackgroundPriority is maxon's Process.enterBackgroundPriority: this process, and the processes and threads it starts after it,
	// run at background scheduling priority, and the answer is the priority the system reports then, a second reading: Windows' priority
	// class (16384, BELOW_NORMAL_PRIORITY_CLASS) or a POSIX nice value (10). maxon's BackgroundPriority is a FullRangeInt.
	{Owner: "Process", Name: "enterBackgroundPriority", Static: true, Result: "FullRangeInt", Intrinsic: "ProcessEnterBackgroundPriority"},
	// nowNanos is maxon's Clock.nowNanos.
	{Owner: "Clock", Name: "nowNanos", Static: true, Result: "InstantNanos", Intrinsic: "ClockNowNanos"},
	// processorCount is maxon's Scheduler.processorCount, whose SchedulerProcessorCount is an unsigned 64-bit integer, which Count is.
	{Owner: "Scheduler", Name: "processorCount", Static: true, Result: "Count", Intrinsic: "SchedulerProcessorCount"},
	// yield is maxon's Scheduler.yield: the coroutines of the caller's strand that wait to run, else the green threads that
	// are runnable, run before the caller goes on.
	{Owner: "Scheduler", Name: "yield", Static: true, Intrinsic: "SchedulerYield"},
	// pinToMachine and unpinFromMachine are maxon's, Go's LockOSThread and UnlockOSThread: the calling green thread runs only
	// on the thread running it now, and that thread runs no other green thread, until as many unpins as pins. A pin gives
	// whether there is a machine to pin to, which off a green thread is the thread itself.
	{Owner: "Scheduler", Name: "pinToMachine", Static: true, Result: "Bool", Intrinsic: "SchedulerPinToMachine"},
	{Owner: "Scheduler", Name: "unpinFromMachine", Static: true, Intrinsic: "SchedulerUnpinFromMachine"},
	// pinDepth is how many pins the calling green thread holds, and currentMachine the machine (thread) running it, numbered
	// from 1 in the order the threads started; 0 is no machine.
	{Owner: "Scheduler", Name: "pinDepth", Static: true, Result: "Count", Intrinsic: "SchedulerPinDepth"},
	{Owner: "Scheduler", Name: "currentMachine", Static: true, Result: "Count", Intrinsic: "SchedulerCurrentMachine"},
	// machineCpuCount is maxon's: the machine's logical processors, at least 1, whatever MAXON_MAX_PROCS asks.
	{Owner: "Scheduler", Name: "machineCpuCount", Static: true, Result: "Count", Intrinsic: "SchedulerMachineCpuCount"},
	// shutdown is maxon's: the service's mailbox closes, so every later send fails with serviceStopped, and the messages
	// queued already run. It always succeeds.
	{Owner: "Service", Name: "shutdown", Intrinsic: "ServiceShutdown"},
	// mostQueued is the most messages the service has had queued at once, as its slowed sends counted them: exact once that
	// passed a turn's worth, so a test holds backpressure to its bound without timing it.
	{Owner: "Service", Name: "mostQueued", Result: "Count", Intrinsic: "ServiceMostQueued", Raw: &rawSurface{What: "a service's queue length is read", Instead: "let the runtime's backpressure bound it"}},
	// cancel is maxon's: it consumes the promise; a coroutine that has not started never runs, and one that has runs on with
	// each sleep it is in or makes ending at once, and the cancel waits for it to finish and drops what it gave.
	{Owner: "Promise", Name: "cancel", Self: "consumes", Intrinsic: "PromiseCancel"},
	// awaitAny is maxon's: it waits until a promise of an array of them has finished and gives the index of the first that has,
	// consuming none; a slot a take left empty is skipped.
	{Owner: "Array", Name: "awaitAny", Result: "ElementIndex", Intrinsic: "ArrayAwaitAny", Checked: true},
	// isComplete is maxon's: whether the coroutine or the reply has finished, which neither waits nor consumes the promise.
	{Owner: "Promise", Name: "isComplete", Result: "Bool", Intrinsic: "PromiseIsComplete"},
	{Owner: "FilePath", Name: "from", Static: true, Params: []string{"path String"}, Result: "Self", Throws: "FilePathError", Intrinsic: "FilePathFrom"},
	{Owner: "FilePath", Name: "init", Static: true, Params: []string{"value String"}, Result: "Self", Intrinsic: "FilePathInit"},
	{Owner: "FilePath", Name: "toString", Result: "String", Intrinsic: "FilePathToString"},
	{Owner: "File", Name: "readText", Static: true, Params: []string{"path FilePath"}, Result: "String", Throws: "FileReadError", Intrinsic: "FileReadText"},
	// readBinary is maxon's: the whole content of a file as bytes, read as readText reads it.
	{Owner: "File", Name: "readBinary", Static: true, Params: []string{"path FilePath"}, Result: "Array with Byte", Throws: "FileReadError", Intrinsic: "FileReadBinary"},
	{Owner: "File", Name: "writeText", Static: true, Params: []string{"path FilePath", "content String", "mode FilePermission = normal"}, Throws: "FileWriteError", Intrinsic: "FileWriteText"},
	{Owner: "File", Name: "writeBinary", Static: true, Params: []string{"path FilePath", "content Array with Byte", "mode FilePermission = normal"}, Throws: "FileWriteError", Intrinsic: "FileWriteBinary"},
	{Owner: "File", Name: "rename", Static: true, Params: []string{"from FilePath", "to FilePath"}, Throws: "FileRenameError", Intrinsic: "FileRename"},
	{Owner: "File", Name: "delete", Static: true, Params: []string{"path FilePath"}, Throws: "FileDeleteError", Intrinsic: "FileDelete"},
	// exists is maxon's File.exists: whether a file or a directory is at the path.
	{Owner: "File", Name: "exists", Static: true, Params: []string{"path FilePath"}, Result: "Bool", Intrinsic: "FileExists"},
	{Owner: "File", Name: "info", Static: true, Params: []string{"path FilePath"}, Result: "FileInfo", Throws: "FileInfoError", Intrinsic: "FileInfo"},
	{Owner: "Directory", Name: "isDirectory", Static: true, Params: []string{"path FilePath"}, Result: "Bool", Intrinsic: "DirectoryIsDirectory"},
	// exists is maxon's name for the same question: a path that names an entry that is not a directory is not one.
	{Owner: "Directory", Name: "exists", Static: true, Params: []string{"path FilePath"}, Result: "Bool", Intrinsic: "DirectoryIsDirectory"},
	// create makes the missing parents too, and gives whether the directory is there after it.
	{Owner: "Directory", Name: "create", Static: true, Params: []string{"path FilePath"}, Result: "Bool", Intrinsic: "DirectoryCreate"},
	// list gives the path of each entry, the directory's path joined to its name.
	{Owner: "Directory", Name: "currentPath", Static: true, Result: "FilePath", Intrinsic: "DirectoryCurrentPath"},
	// delete removes an empty directory: one that still holds an entry is refused (notEmpty), and a file or a link there is deleteFailed.
	{Owner: "Directory", Name: "delete", Static: true, Params: []string{"path FilePath"}, Throws: "DirectoryDeleteError", Intrinsic: "DirectoryDelete"},
	{Owner: "Directory", Name: "list", Static: true, Params: []string{"path FilePath"}, Result: "Array with FilePath", Throws: "DirectoryListError", Intrinsic: "DirectoryList"},
	{Owner: "Subprocess", Name: "run", Static: true, Params: []string{"executable Executable", "arguments Array with String"}, Result: "CollectedOutput", Throws: "SubprocessError", Intrinsic: "SubprocessRun"},
	// A built-in's only default argument is the case of an enum ("= case"), so maxon's run with a deadline is a function of its own. A child
	// still running when the deadline passes is terminated, and the run throws timeout.
	{Owner: "Subprocess", Name: "runWithDeadline", Static: true, Params: []string{"executable Executable", "arguments Array with String", "deadline WaitDeadline"}, Result: "CollectedOutput", Throws: "SubprocessError", Intrinsic: "SubprocessRunWithDeadline"},
	// maxon's run with workingDirectory: WorkingDirectory.at(directory), a function of its own for the same reason: the child
	// starts in directory, relative to the caller's working directory, and the run has the deadline's.
	{Owner: "Subprocess", Name: "runInDirectory", Static: true, Params: []string{"executable Executable", "arguments Array with String", "directory FilePath", "deadline WaitDeadline"}, Result: "CollectedOutput", Throws: "SubprocessError", Intrinsic: "SubprocessRunInDirectory"},
	{Owner: "CollectedOutput", Name: "exitCode", Result: "ExitCode", Intrinsic: "CollectedOutputExitCode"},
	// The debug port, maxon's DebugPort over the Win32 debug API, as Debuggee's static functions; Windows only, so a call of
	// one on another system panics, as maxon's windowsDebugCall does. launch starts the program as the caller's debuggee
	// (DEBUG_ONLY_THIS_PROCESS), its output and error the caller's own or both the caller's error, and gives its process id;
	// the thread that launched it is the one that may wait for its events, so the caller pins itself to its machine first.
	// waitEvent gives the DEBUG_EVENT that came within the milliseconds, its bytes as the system wrote them, or none when
	// none did. A process or thread is named by the handle its event gave.
	{Owner: "Debuggee", Name: "launch", Static: true, Params: []string{"executable Executable", "arguments Array with String", "outputToError Bool"}, Result: "Count", Throws: "SubprocessError", Intrinsic: "DebuggeeLaunch"},
	{Owner: "Debuggee", Name: "waitEvent", Static: true, Params: []string{"budgetMs Count"}, Result: "Array with Byte", Intrinsic: "DebuggeeWaitEvent"},
	{Owner: "Debuggee", Name: "continueEvent", Static: true, Params: []string{"processId Count", "thread Count", "disposition DebugDisposition"}, Result: "Bool", Intrinsic: "DebuggeeContinueEvent"},
	{Owner: "Debuggee", Name: "readMemory", Static: true, Params: []string{"process Count", "address Count", "count Count"}, Result: "Array with Byte", Throws: "DebuggeeError", Intrinsic: "DebuggeeReadMemory"},
	// writeCode lifts the protection of the code it writes and puts it back, and flushes the instruction cache over it.
	{Owner: "Debuggee", Name: "writeCode", Static: true, Params: []string{"process Count", "address Count", "content Array with Byte"}, Throws: "DebuggeeError", Intrinsic: "DebuggeeWriteCode"},
	// readContext gives a thread's CONTEXT, its control, integer and floating-point registers, and writeContext writes one back.
	{Owner: "Debuggee", Name: "readContext", Static: true, Params: []string{"thread Count"}, Result: "Array with Byte", Throws: "DebuggeeError", Intrinsic: "DebuggeeReadContext"},
	{Owner: "Debuggee", Name: "writeContext", Static: true, Params: []string{"thread Count", "context Array with Byte"}, Throws: "DebuggeeError", Intrinsic: "DebuggeeWriteContext"},
	{Owner: "Debuggee", Name: "suspend", Static: true, Params: []string{"thread Count"}, Result: "Bool", Intrinsic: "DebuggeeSuspend"},
	{Owner: "Debuggee", Name: "resume", Static: true, Params: []string{"thread Count"}, Result: "Bool", Intrinsic: "DebuggeeResume"},
	{Owner: "Debuggee", Name: "breakIn", Static: true, Params: []string{"process Count"}, Result: "Bool", Intrinsic: "DebuggeeBreakIn"},
	{Owner: "Debuggee", Name: "detach", Static: true, Params: []string{"processId Count"}, Result: "Bool", Intrinsic: "DebuggeeDetach"},
	{Owner: "Debuggee", Name: "closeHandle", Static: true, Params: []string{"handle Count"}, Intrinsic: "DebuggeeCloseHandle"},
	{Owner: "Debuggee", Name: "terminate", Static: true, Params: []string{"process Count", "exitCode Count"}, Result: "Bool", Intrinsic: "DebuggeeTerminate"},
	{Owner: "CollectedOutput", Name: "succeeded", Result: "Bool", Intrinsic: "CollectedOutputSucceeded"},

	// The floors of maxon's Sleep and Scheduler, which the library tiers call. sleep is maxon's: the running green thread
	// parks for at least the milliseconds, and its strand's other coroutines run meanwhile; the library's sleep calls it.
	{Owner: "Builtins", Name: "sleep", Static: true, Params: []string{"_ Natural"}, Intrinsic: "Sleep", Instead: "sleep(milliseconds)"},
	{Owner: "Builtins", Name: "cpuCount", Static: true, Result: "Count", Intrinsic: "SchedulerMachineCpuCount", Instead: "Scheduler.machineCpuCount()"},
	{Owner: "Builtins", Name: "currentProcessId", Static: true, Result: "Count", Intrinsic: "ProcessId", Instead: "Process.id()"},
	{Owner: "Builtins", Name: "currentTimeNanos", Static: true, Result: "Count", Intrinsic: "ClockNowNanos", Instead: "Clock.nowNanos()"},
	// The other readings of the clocks maxon's Clock and WallClock are written on. A wall-clock reading is signed: it is
	// before 1970 for a time that is.
	{Owner: "Builtins", Name: "currentTimeMs", Static: true, Result: "InstantMs", Intrinsic: "ClockNowMillis", Instead: "Clock.nowMs()"},
	{Owner: "Builtins", Name: "tickCountMs", Static: true, Result: "InstantMs", Intrinsic: "ClockTickMillis", Instead: "Clock.osTickMs()"},
	{Owner: "Builtins", Name: "threadCpuTicks", Static: true, Result: "ThreadCpuTicks", Intrinsic: "ClockThreadCpuTicks", Instead: "Clock.threadCpuTicks()"},
	{Owner: "Builtins", Name: "currentUnixTimeSeconds", Static: true, Result: "UnixSeconds", Intrinsic: "ClockUnixSeconds", Instead: "WallClock.nowUnixSeconds()"},
	{Owner: "Builtins", Name: "currentUnixTimeNanos", Static: true, Result: "UnixNanos", Intrinsic: "ClockUnixNanos", Instead: "WallClock.nowUnixNanos()"},
	// readStdin is maxon's: up to the count of bytes of standard input, of any integer type that is not negative, in a fresh buffer, empty only at the end.
	// stdoutWantsAnsiColor is whether standard output is a character device, NO_COLOR is unset and TERM is not dumb, as maxon's
	// runtime asks. runWithInput is Subprocess.runWithDeadline with the child's standard input a pipe that holds the input and
	// then ends: what maxon's Configuration.standardInput of InputSource.bytes gives, which the compiler's test runner and
	// interpreter run children with until maxon's Subprocess converts.
	{Owner: "Builtins", Name: "readStdin", Static: true, Params: []string{"_ Natural"}, Result: "ManagedMemory with Byte", Intrinsic: "ReadStdin", Instead: "Console.stdin().readBytes(n)"},
	// osEnvironmentEntry is maxon's: the NAME=VALUE entry of this process's environment at an index, in the order the system
	// reports them, in a fresh buffer of bytes: empty past the last entry, and one NUL byte where the system handed over no
	// environment, neither of which an entry can be.
	{Owner: "Builtins", Name: "osEnvironmentEntry", Static: true, Params: []string{"_ ElementIndex"}, Result: "ManagedMemory with Byte", Intrinsic: "OsEnvironmentEntry", Instead: "Process.currentEnvironmentEntries()"},
	// The subprocess rows are maxon's `__Builtins.subprocess*` (specs/subprocess-builtins.md), the floor of its Subprocess.maxon, but
	// for the kill and the signal that no corpus file calls: a spawn that gives a handle (or -1), or the id of a detached child, a wait
	// that collects the output of the child it names into a result, the readers of that result, the streaming child (a spawn whose
	// input is written and whose output is read by calls, its waits and polls) and the helpers. A handle and a result are each a word that names a slot of the
	// runtime's table and the generation the slot is in, so a word that names nothing live (a released one, one that was
	// never given, -1) is a failure and never a fault. A cstring is the two words of a String, which the row copies where it
	// hands the bytes to the system and frees on every way out. The results that are words are signed where maxon's are, and
	// subprocessResultDurationMs is a natural number, which maxon's library passes on as a DurationMs.
	{Owner: "Builtins", Name: "subprocessSpawn", Static: true, Params: []string{"_ ManagedMemory with Byte", "_ ElementIndex", "_ CString", "_ ManagedMemory with Byte", "_ Natural", "_ Natural", "_ CString", "_ Natural", "_ CString", "_ Natural", "_ Natural", "_ CString", "_ Natural", "_ Natural", "_ Natural"}, Result: "FullRangeInt", Intrinsic: "SubprocessSpawn", Instead: "Subprocess.run or a Configuration's run()"},
	{Owner: "Builtins", Name: "subprocessDetach", Static: true, Params: []string{"_ ManagedMemory with Byte", "_ ElementIndex", "_ CString", "_ ManagedMemory with Byte", "_ Natural", "_ Natural", "_ CString", "_ Natural", "_ CString", "_ Natural", "_ Natural", "_ CString", "_ Natural", "_ Natural", "_ Natural"}, Result: "FullRangeInt", Intrinsic: "SubprocessSpawn", Instead: "Subprocess.run or a Configuration's run()"},
	{Owner: "Builtins", Name: "subprocessGetPid", Static: true, Params: []string{"_ FullRangeInt"}, Result: "FullRangeInt", Intrinsic: "SubprocessGetPid", Instead: "Subprocess.run or a Configuration's run()"},
	{Owner: "Builtins", Name: "subprocessWaitCollect", Static: true, Params: []string{"_ FullRangeInt", "_ Natural"}, Result: "FullRangeInt", Intrinsic: "SubprocessWaitCollect", Instead: "Subprocess.run or a Configuration's run()"},
	{Owner: "Builtins", Name: "subprocessResultStatusKind", Static: true, Params: []string{"_ FullRangeInt"}, Result: "FullRangeInt", Intrinsic: "SubprocessResultStatusKind", Instead: "Subprocess.run or a Configuration's run()"},
	{Owner: "Builtins", Name: "subprocessResultStatusCode", Static: true, Params: []string{"_ FullRangeInt"}, Result: "FullRangeInt", Intrinsic: "SubprocessResultStatusCode", Instead: "Subprocess.run or a Configuration's run()"},
	{Owner: "Builtins", Name: "subprocessResultStdout", Static: true, Params: []string{"_ FullRangeInt"}, Result: "ManagedMemory with Byte", Intrinsic: "SubprocessResultStdout", Instead: "Subprocess.run or a Configuration's run()"},
	{Owner: "Builtins", Name: "subprocessResultStderr", Static: true, Params: []string{"_ FullRangeInt"}, Result: "ManagedMemory with Byte", Intrinsic: "SubprocessResultStderr", Instead: "Subprocess.run or a Configuration's run()"},
	{Owner: "Builtins", Name: "subprocessResultDurationMs", Static: true, Params: []string{"_ FullRangeInt"}, Result: "Natural", Intrinsic: "SubprocessResultDurationMs", Instead: "Subprocess.run or a Configuration's run()"},
	{Owner: "Builtins", Name: "subprocessResultRelease", Static: true, Params: []string{"_ FullRangeInt"}, Intrinsic: "SubprocessResultRelease", Instead: "Subprocess.run or a Configuration's run()"},
	{Owner: "Builtins", Name: "subprocessReleaseHandle", Static: true, Params: []string{"_ FullRangeInt"}, Intrinsic: "SubprocessReleaseHandle", Instead: "Subprocess.run or a Configuration's run()"},
	{Owner: "Builtins", Name: "subprocessEndTree", Static: true, Params: []string{"_ FullRangeInt"}, Result: "FullRangeInt", Intrinsic: "SubprocessEndTree", Instead: "Subprocess.run or a Configuration's run()"},
	{Owner: "Builtins", Name: "subprocessSpawnStreaming", Static: true, Params: []string{"_ ManagedMemory with Byte", "_ ElementIndex", "_ CString", "_ Natural", "_ ManagedMemory with Byte", "_ Natural"}, Result: "FullRangeInt", Intrinsic: "SubprocessSpawnStreaming", Instead: "StreamingSubprocess"},
	{Owner: "Builtins", Name: "subprocessReadAvailable", Static: true, Params: []string{"_ FullRangeInt", "_ FullRangeInt", "_ FullRangeInt"}, Result: "ManagedMemory with Byte", Intrinsic: "SubprocessReadAvailable", Instead: "StreamingSubprocess"},
	{Owner: "Builtins", Name: "subprocessStdoutState", Static: true, Params: []string{"_ FullRangeInt"}, Result: "SubprocessStreamState", Intrinsic: "SubprocessStdoutState", Instead: "StreamingSubprocess"},
	{Owner: "Builtins", Name: "subprocessStderrState", Static: true, Params: []string{"_ FullRangeInt"}, Result: "SubprocessStreamState", Intrinsic: "SubprocessStderrState", Instead: "StreamingSubprocess"},
	{Owner: "Builtins", Name: "subprocessWriteStdinAll", Static: true, Params: []string{"_ FullRangeInt", "_ CString"}, Result: "FullRangeInt", Intrinsic: "SubprocessWriteStdinAll", Instead: "StreamingSubprocess"},
	{Owner: "Builtins", Name: "subprocessWriteStdinBytes", Static: true, Params: []string{"_ FullRangeInt", "_ CString", "_ Natural"}, Result: "FullRangeInt", Intrinsic: "SubprocessWriteStdinBytes", Instead: "StreamingSubprocess"},
	{Owner: "Builtins", Name: "subprocessQueueStdin", Static: true, Params: []string{"_ FullRangeInt", "_ CString", "_ Natural"}, Result: "FullRangeInt", Intrinsic: "SubprocessQueueStdin", Instead: "StreamingSubprocess"},
	{Owner: "Builtins", Name: "subprocessPushStdin", Static: true, Params: []string{"_ FullRangeInt"}, Result: "FullRangeInt", Intrinsic: "SubprocessPushStdin", Instead: "StreamingSubprocess"},
	{Owner: "Builtins", Name: "subprocessAwaitStdinWritable", Static: true, Params: []string{"_ FullRangeInt", "_ Natural"}, Result: "FullRangeInt", Intrinsic: "SubprocessAwaitStdinWritable", Instead: "StreamingSubprocess"},
	{Owner: "Builtins", Name: "subprocessCloseStdin", Static: true, Params: []string{"_ FullRangeInt"}, Intrinsic: "SubprocessCloseStdin", Instead: "StreamingSubprocess"},
	{Owner: "Builtins", Name: "subprocessWaitExit", Static: true, Params: []string{"_ FullRangeInt", "_ Natural"}, Result: "FullRangeInt", Intrinsic: "SubprocessWaitExit", Instead: "StreamingSubprocess"},
	{Owner: "Builtins", Name: "subprocessPollExit", Static: true, Params: []string{"_ FullRangeInt"}, Result: "FullRangeInt", Intrinsic: "SubprocessPollExit", Instead: "StreamingSubprocess"},
	{Owner: "Builtins", Name: "subprocessExitSignal", Static: true, Params: []string{"_ FullRangeInt"}, Result: "FullRangeInt", Intrinsic: "SubprocessExitSignal", Instead: "StreamingSubprocess"},
	{Owner: "Builtins", Name: "subprocessLastErrorCode", Static: true, Result: "FullRangeInt", Intrinsic: "SubprocessLastErrorCode", Instead: "Subprocess.run or a Configuration's run()"},
	{Owner: "Builtins", Name: "subprocessLastErrorMessage", Static: true, Result: "ManagedMemory with Byte", Intrinsic: "SubprocessLastErrorMessage", Instead: "Subprocess.run or a Configuration's run()"},
	{Owner: "Builtins", Name: "subprocessResolveOnPath", Static: true, Params: []string{"_ CString"}, Result: "ManagedMemory with Byte", Intrinsic: "SubprocessResolveOnPath", Instead: "Subprocess.run or a Configuration's run()"},
	{Owner: "Builtins", Name: "runWithInput", Static: true, Params: []string{"executable Executable", "arguments Array with String", "input String", "deadline WaitDeadline"}, Result: "CollectedOutput", Throws: "SubprocessError", Intrinsic: "SubprocessRunWithInput", Instead: "a Configuration whose standardInput is InputSource.bytes(text)"},
	{Owner: "Builtins", Name: "stdoutWantsAnsiColor", Static: true, Result: "Bool", Intrinsic: "StdoutWantsColor", Instead: "Console.stdoutWantsColor()"},
	// floatToBits is the 64 bits of a double read as an integer, as maxon's is: a reinterpretation, which the library's
	// float hash folds.
	{Owner: "Builtins", Name: "floatToBits", Static: true, Params: []string{"_ Float"}, Result: "FullRangeInt", Intrinsic: "FloatToBits", Instead: "the float's own operations"},
	// fillRandom overwrites every byte of a buffer of bytes with the system's entropy, which Random.bytes asks for. It gives 0 where
	// maxon's gives a non-zero answer for a refusal, which here is a panic.
	{Owner: "Builtins", Name: "fillRandom", Static: true, Params: []string{"mutates _ ManagedMemory with Byte"}, Result: "Count", Intrinsic: "RandomFill", Instead: "Random.bytes(count)"},
	// bitsToFloat is its inverse: the double whose 64 bits are the integer, the only way to make an infinity or a NaN.
	{Owner: "Builtins", Name: "bitsToFloat", Static: true, Params: []string{"_ FullRangeInt"}, Result: "Float", Intrinsic: "BitsToFloat", Instead: "the float's own operations"},
	// The seeded hash of maxon's runtime, its software fold: hashSeed is the fold of no bytes under the process's secret,
	// which a Hasher starts from; hashWord and hashBytes fold a word or the bytes of a span of an array under a seed;
	// drawHashSeed draws a fresh secret from the system, or the next of the pinned process's sequence.
	// trailingZeros is the number of 0 bits of a word below its lowest 1 bit, and 64 for 0: the place of the first match
	// in a word of matches, one instruction where the machine has one.
	{Owner: "Builtins", Name: "trailingZeros", Static: true, Params: []string{"_ Count"}, Result: "Count", Intrinsic: "TrailingZeros", Instead: "a loop over the word's bits"},
	// The counters of the heap (runtime/heap.maxon) that maxon's HeapStats reads: the allocations, frees and bytes of the
	// running thread's heap and of every thread's; mm* are the process's, since every allocation here is a managed box.
	// The counts by tag answer 0, as maxon's do outside a --debugstream build, and scavengeMemory runs a release pass of the
	// page heap and answers the bytes it gave back.
	{Owner: "Builtins", Name: "mmAllocTotal", Static: true, Result: "Count", Intrinsic: "HeapProcessAllocs", Instead: "HeapStats.allocationsMade()"},
	{Owner: "Builtins", Name: "mmAllocLive", Static: true, Result: "Count", Intrinsic: "HeapProcessLive", Instead: "HeapStats.allocationsLive()"},
	{Owner: "Builtins", Name: "mmAllocBytes", Static: true, Result: "Count", Intrinsic: "HeapProcessBytes", Instead: "HeapStats.bytesAllocated()"},
	{Owner: "Builtins", Name: "mmAllocTotalByTag", Static: true, Params: []string{"_ Count"}, Result: "Count", Intrinsic: "HeapByTag", Instead: "HeapStats.allocationsTagged(tag)"},
	{Owner: "Builtins", Name: "mmAllocBytesByTag", Static: true, Params: []string{"_ Count"}, Result: "Count", Intrinsic: "HeapByTag", Instead: "HeapStats.bytesTagged(tag)"},
	{Owner: "Builtins", Name: "threadAllocTotal", Static: true, Result: "Count", Intrinsic: "HeapThreadAllocs", Instead: "HeapStats.threadAllocations()"},
	{Owner: "Builtins", Name: "threadFreeTotal", Static: true, Result: "Count", Intrinsic: "HeapThreadFrees", Instead: "HeapStats.threadFrees()"},
	{Owner: "Builtins", Name: "threadAllocBytes", Static: true, Result: "Count", Intrinsic: "HeapThreadBytes", Instead: "HeapStats.threadBytes()"},
	{Owner: "Builtins", Name: "processAllocTotal", Static: true, Result: "Count", Intrinsic: "HeapProcessAllocs", Instead: "HeapStats.processAllocations()"},
	{Owner: "Builtins", Name: "processFreeTotal", Static: true, Result: "Count", Intrinsic: "HeapProcessFrees", Instead: "HeapStats.processFrees()"},
	{Owner: "Builtins", Name: "processAllocBytes", Static: true, Result: "Count", Intrinsic: "HeapProcessBytes", Instead: "HeapStats.processBytes()"},
	{Owner: "Builtins", Name: "scavengeMemory", Static: true, Result: "Count", Intrinsic: "HeapScavenge", Instead: "HeapStats.scavenge()"},
	{Owner: "Builtins", Name: "hashSeed", Static: true, Result: "HashDigest", Intrinsic: "HashSeed", Instead: "Hasher.create()"},
	{Owner: "Builtins", Name: "drawHashSeed", Static: true, Result: "HashDigest", Intrinsic: "HashSeedDraw", Instead: "HashSeed.random()"},
	{Owner: "Builtins", Name: "hashWord", Static: true, Params: []string{"_ HashDigest", "_ HashDigest"}, Result: "HashDigest", Intrinsic: "HashWord", Instead: "a Hasher's combine(value)"},
	{Owner: "Builtins", Name: "hashBytes", Static: true, Params: []string{"_ Array with Byte", "_ ElementIndex", "_ ElementIndex", "_ HashDigest"}, Result: "HashDigest", Intrinsic: "HashBytes", Instead: "a Hasher's combineBytes(bytes)"},
	// hashBytes of a String, whose box holds its bytes inline: the call of hashBytes whose first argument is a String.
	{Owner: "Builtins", Name: "hashBytes", Static: true, Params: []string{"_ String", "_ ElementIndex", "_ ElementIndex", "_ HashDigest"}, Result: "HashDigest", Intrinsic: "HashStringBytes", Field: "HashStringBytes", Instead: "a Hasher's combineBytes(bytes)"},
	// hashCharacter is the fold of a Character's bytes under a seed, which the library's Character.hash is: minimaxon's, as a
	// Character here holds its bytes in its words where maxon's hashBytes reads its buffer.
	{Owner: "Builtins", Name: "hashCharacter", Static: true, Params: []string{"_ Character", "_ HashDigest"}, Result: "HashDigest", Intrinsic: "HashCharacter", Instead: "the character's hash()"},
	// The ordered words of maxon's library: a read of the 2, 4 or 8 bytes at an index of an array, the first the most
	// significant or the least, and an append of a word's low bytes in that order.
	{Owner: "Builtins", Name: "readBigEndian", Static: true, Params: []string{"_ Array with Byte", "_ ElementIndex", "_ Count"}, Result: "OrderedWord", Throws: "ArrayError", Intrinsic: "ReadBigEndian", Instead: "readOrderedWord", Checked: true},
	{Owner: "Builtins", Name: "readLittleEndian", Static: true, Params: []string{"_ Array with Byte", "_ ElementIndex", "_ Count"}, Result: "OrderedWord", Throws: "ArrayError", Intrinsic: "ReadLittleEndian", Instead: "readOrderedWord", Checked: true},
	{Owner: "Builtins", Name: "appendBigEndian", Static: true, Params: []string{"mutates _ Array with Byte", "_ OrderedWord", "_ Count"}, Intrinsic: "AppendBigEndian", Instead: "appendOrderedWord", Checked: true},
	{Owner: "Builtins", Name: "appendLittleEndian", Static: true, Params: []string{"mutates _ Array with Byte", "_ OrderedWord", "_ Count"}, Intrinsic: "AppendLittleEndian", Instead: "appendOrderedWord", Checked: true},
}

// unit is a code unit of the backend's runtime, or a place an entry lives that has no code of its own.
type unit struct {
	Key string // ir.RtUnit+Key
	Doc string
}

var units = []unit{
	{Key: "Runtime", Doc: "the runtime unit every executable links"},
	{Key: "Arrays", Doc: "the array unit, linked into a program that calls an array entry"},
	{Key: "Bytes", Doc: "the byte array unit, linked into a program that calls a byte array entry"},
	{Key: "Os", Doc: "the OS unit, linked into a program that calls an OS entry"},
	{Key: "Linker", Doc: "the linker, which defines the symbol itself"},
	{Key: "Shared", Doc: "the frozen values and cowns, in the runtime's source (runtime/frozen.maxon)"},
	{Key: "Source", Doc: "the runtime's Maxon source (runtime/), which the compiler compiles with every program"},
}

// The roles of a runtime symbol.
const (
	// roleCall is an entry the IR calls with a CallRuntimeInstr, which the interpreter runs in entry+Name.
	roleCall = "call"
	// roleInternal is code that only the backend's own code calls.
	roleInternal = "internal"
	// roleGlobal is a global the code reads, which no unit defines through the table.
	roleGlobal = "global"
)

// entry is a symbol the runtime defines (README, "The IR").
type entry struct {
	Name   string // ir.Rt+Name
	Symbol string
	Unit   string // a unit's Key
	Role   string
	Doc    string
}

// The entries of the primitives own their elements: an entry that takes one moves it in, an entry that returns one
// moves it out, and an entry that returns an address lends it, valid until the next call that changes the primitive.
// An entry given an index, a key or a slot key that names nothing traps. A slot key is a word and never 0, so an entry
// that returns one returns 0 for none. A frozen value and a cown have a count of owners: an entry that makes another owner
// raises it, dropping an owner lowers it, and the box is freed when it reaches 0.
var entries = []entry{
	{"Entry", "rt.entry", "Runtime", roleInternal, "the executable's entry point, which calls main"},
	{"Alloc", "rt.alloc", "Runtime", roleInternal, "(ByteArray) -> a zeroed box"},
	{"Free", "rt.free", "Runtime", roleInternal, "(box): 0 is skipped"},
	{"Write", "rt.write", "Runtime", roleCall, "(handle, text String)"},
	{"Panic", "rt.panic", "Runtime", roleCall, "(message String): exits"},
	{"PanicAt", "rt.panicAt", "Runtime", roleCall, "(location String, message String): exits, saying where the program's panic is"},
	{"CheckSimpleBytes", "rt.checkSimpleBytes", "Runtime", roleInternal, "(bytes, length): panics unless every byte is below 0x80 and none is a carriage return, the check of an owned String's ASCII bit that a build counting its allocations makes"},
	{"PanicEmptySlot", "rt.panicEmptySlot", "Runtime", roleCall, "(location String, index): exits as panicAt does, with maxon's message of a lent read of an empty slot at the index"},
	{"Concat", "rt.concat", "Runtime", roleCall, "(String, String) -> String"},
	{"Itoa", "rt.itoa", "Runtime", roleCall, "(n, unsigned) -> String"},
	// An interpolation is measured, given one buffer and has each part put into it (README, "Strings"); one that is assigned
	// back to the String it starts with appends the rest to that String's own buffer where it has room.
	{"DecimalLength", "rt.decimalLength", "Runtime", roleCall, "(n, unsigned) -> how many bytes the decimal text of n takes, its '-' among them, read as signed unless unsigned says otherwise"},
	{"StrJoinRoom", "rt.str.joinRoom", "Runtime", roleCall, "(length) -> String: an empty String that owns room for length bytes, which an interpolation puts its parts into"},
	{"StrJoinPut", "rt.str.joinPut", "Runtime", roleCall, "(String, part String) -> String: the String with the part's bytes after its own, in the room it has for them, its ASCII bit kept where both have it"},
	{"StrJoinDigits", "rt.str.joinDigits", "Runtime", roleCall, "(String, n, unsigned, length) -> String: the String with the length bytes of the decimal text of n after its own, in the room it has for them"},
	{"StrOwned", "rt.str.owned", "Runtime", roleCall, "(String) -> whether it owns its bytes, its owned bit, which the backend reads in place"},
	{"StrAscii", "rt.str.ascii", "Runtime", roleCall, "(String) -> whether each of its grapheme clusters is one byte, its ASCII bit, which the backend reads in place"},
	{"StrExtend", "rt.str.extend", "Runtime", roleCall, "(String, owned, extra) -> String: the String's bytes with room for extra more after them, in its own buffer where it owns one with the room and else in one the growth policy sizes, the old buffer kept for rt.str.release"},
	{"StrRelease", "rt.str.release", "Runtime", roleCall, "(old String, owned, grown String): frees the old String's buffer where it owned one and grown's bytes are elsewhere"},
	{"StrAppend", "rt.str.append", "Runtime", roleCall, "(String, owned, part String) -> String: rt.str.extend, rt.str.joinPut and rt.str.release of one part"},
	{"Ftoa", "rt.ftoa", "Runtime", roleCall, "(bits) -> String: the text of the double of the bits"},
	{"FtoaExact", "rt.ftoaExact", "Runtime", roleCall, "(bits) -> String: the exact decimal of the magnitude of the double of the bits, or inf or nan"},
	{"FmtInt", "rt.fmt.int", "Runtime", roleCall, "(n, radix, flags, width) -> String: the word in the base, in a field"},
	{"FmtPad", "rt.fmt.pad", "Runtime", roleCall, "(text String, width, zeroFill) -> String: the text of a number in a field"},
	{"FmtQuoted", "rt.fmt.quoted", "Runtime", roleCall, "(text String, single, width) -> String: the text as a literal, in a field"},
	{"FmtFixed", "rt.fmt.fixed", "Runtime", roleCall, "(magnitude String, negative, precision) -> String: the exact decimal rounded to the places"},
	{"Streq", "rt.streq", "Runtime", roleCall, "(String, String) -> bool"},
	{"StrLen", "rt.str.len", "Runtime", roleCall, "(String) -> count of bytes"},
	// A Character is two words laid out as a String is (runtime/characters.maxon); these are the entries that read its bytes.
	{"CharEquals", "rt.char.equals", "Runtime", roleCall, "(Character, Character) -> whether they have the same bytes"},
	{"CharCompare", "rt.char.compare", "Runtime", roleCall, "(Character, Character) -> 0, 1 or 2 where the first's bytes are before, the same as or after the second's"},
	{"CharLength", "rt.char.length", "Runtime", roleCall, "(Character) -> count of its bytes"},
	{"CharToString", "rt.char.toString", "Runtime", roleCall, "(Character) -> a new String of its bytes"},
	{"CharCodepoint", "rt.char.codepoint", "Runtime", roleCall, "(Character) -> its first code point"},
	{"CharHash", "rt.char.hash", "Runtime", roleCall, "(Character, seed) -> the fold of its bytes under the seed: of its two words where it holds them, which a cluster of so few bytes always does"},
	{"CharFromCodepoint", "rt.char.fromCodepoint", "Runtime", roleCall, "(code point) -> the Character of its UTF-8 bytes; a surrogate panics"},
	{"CharAdvanceBy", "rt.char.advanceBy", "Runtime", roleCall, "(Character, signed step) -> the Character of the scalar value that many after its first code point's; a surrogate or a step past the scalar values panics"},
	{"CharNextScalar", "rt.char.nextScalar", "Runtime", roleCall, "(code point) -> the scalar value after a scalar value, past the surrogates, which a character range steps through"},
	{"CharOfBytes", "rt.char.ofBytes", "Runtime", roleCall, "(String, from, length) -> a Character of the length bytes from from, held in its two words when they are few enough"},
	{"StrByteAt", "rt.str.byteAt", "Runtime", roleCall, "(String, index) -> byte"},
	{"StrView", "rt.str.view", "Runtime", roleCall, "(String, from, length) -> String: a view of the length bytes from from, which owns none and has the String's ASCII bit, the bounds checked before; the backend writes it in place where the String holds no bytes in its words and the view is too long to"},
	{"StrCount", "rt.str.count", "Runtime", roleCall, "(String) -> count of its grapheme clusters"},
	{"StrGraphemeIndex", "rt.str.graphemeIndex", "Runtime", roleCall, "(String, byte position) -> count of the clusters that start before it"},
	{"StrGraphemeBytePos", "rt.str.graphemeBytePos", "Runtime", roleCall, "(String, from, count) -> the byte position that count clusters after from end at, or the end"},
	{"StrCStr", "rt.str.cstr", "Runtime", roleCall, "(String) -> cstring: the String's own two words, which own nothing as a cstring (the drop and the copy of the type are the routines of a view), so the String must outlive it; the interpreter, which tracks each object's owner, gives a copy of the text"},
	{"StrFromCString", "rt.str.fromCString", "Runtime", roleCall, "(cs String) -> String: a new String of the bytes of a cstring before its first NUL, which its length bounds, the cstring's two words taken as a String's"},
	{"StrFind", "rt.str.find", "Runtime", roleCall, "(String, needle String, from) -> the position of the first match at or after from, plus 1, or 0"},
	{"StrFindLast", "rt.str.findLast", "Runtime", roleCall, "(String, needle String) -> the position of the last match, plus 1, or 0"},
	{"StrHasAffix", "rt.str.hasAffix", "Runtime", roleCall, "(String, affix String, atEnd) -> whether it starts with the affix, or ends with it where atEnd"},
	{"StrReplace", "rt.str.replace", "Runtime", roleCall, "(String, old String, replacement String, all) -> String with the first match, or each, replaced"},
	// StrTrimFirst and StrTrimLast are the seed's String.trim, which builds C1 with them: trim is the library's
	// (stdlib/characterset/StringTrim.maxon), and the two go with the next seed.
	{"StrTrimFirst", "rt.str.trimFirst", "Runtime", roleCall, "(String) -> the byte position of the first cluster that is not whitespace, or the length"},
	{"StrTrimLast", "rt.str.trimLast", "Runtime", roleCall, "(String, from) -> the byte position after the last cluster at or after from that is not whitespace, or from"},
	{"StrMapCase", "rt.str.mapCase", "Runtime", roleCall, "(String, upper) -> String with its ASCII letters in that case"},
	{"StrCodePointAt", "rt.str.codePointAt", "Runtime", roleCall, "(String, byte position) -> the code point that starts there"},
	{"StrCodePointNext", "rt.str.codePointNext", "Runtime", roleCall, "(String, byte position) -> the byte position of the next code point, plus 1, or 0 at the last"},
	{"StrCodePointCount", "rt.str.codePointCount", "Runtime", roleCall, "(String) -> count of its code points"},
	{"StrUtf16Unit", "rt.str.utf16Unit", "Runtime", roleCall, "(String, state) -> the UTF-16 code unit at a state: twice the byte position of a code point, plus 1 at the second unit of a pair"},
	{"StrUtf16Next", "rt.str.utf16Next", "Runtime", roleCall, "(String, state) -> the state of the next code unit, plus 1, or 0 at the last"},
	{"StrUtf16Count", "rt.str.utf16Count", "Runtime", roleCall, "(String) -> count of its UTF-16 code units"},
	{"PanicIndex", "rt.panicIndex", "Arrays", roleInternal, "panics with an index out of range"},
	{"Heap", "g.heap", "Runtime", roleGlobal, "the process heap"},
	{"Stdout", "g.stdout", "Runtime", roleGlobal, "the handle of standard output"},
	{"Stderr", "g.stderr", "Runtime", roleGlobal, "the handle of standard error"},
	{"CheckLeaks", "rt.checkLeaks", "Source", roleInternal, "(exit code) -> exit code: what the entry of an executable built to count its allocations calls after main, which exits with a leak report when a box is still live"},
	{"TlsSlot", "g.tlsSlot", "Runtime", roleGlobal, "the thread-local slot that each thread keeps its context in, which on Windows the entry takes with TlsAlloc and a thread sets with TlsSetValue, and on Linux is the main thread's word that fs points at"},
	{"TlsOffset", "g.tlsOffset", "Runtime", roleGlobal, "where the thread-local slot is in a thread's TEB, which a read of the thread's context loads at"},
	{"MainContext", "g.mainContext", "Runtime", roleGlobal, "the context of the main thread, which the entry stores in the thread-local slot"},
	{"ProcessStart", "g.processStart", "Runtime", roleGlobal, "the stack pointer a Linux process starts with, at the argument count, then the arguments and the environment, which the entry keeps"},
	{"ThreadStart", "rt.threadStart", "Runtime", roleInternal, "(context) by Win64: the start of a thread the runtime makes, which keeps stack for the fault handler, stores the context in the thread-local slot and calls workerMain"},
	{"StackSwitch", "rt.stackSwitch", "Runtime", roleInternal, "(pair) -> the record it left: saves the registers of the running stack into the switch record at the pair's first word and goes on the stack of the record at its second, by the internal convention (README, \"Green threads\")"},
	{"CopyBytes", "rt.copyBytes", "Runtime", roleInternal, "(source, target, count) by the internal convention: copies count bytes, count above 0, between places that do not overlap, with the copy routines' copy (__Raw.copyBytes)"},
	{"GreenStart", "rt.greenStart", "Runtime", roleInternal, "the code a green thread's stack starts at, which a switch onto a stack that has not run returns to: it calls the code in r12 with the word in rbx"},
	{"AsyncPreempt", "rt.asyncPreempt", "Runtime", roleInternal, "the code the monitor makes a green thread it stops return into, at the address it stopped at: it saves every register and the flags, calls the code at contextPreemptCodeOffset of the thread's context with the context, and gives them back"},
	{"PreemptHandler", "rt.preemptHandler", "Runtime", roleInternal, "(signal, siginfo, context) by System V: the handler of the Linux preemption signal, which calls the code at contextSignalCodeOffset of the thread's context with the signal's context"},
	{"Sigreturn", "rt.sigreturn", "Runtime", roleInternal, "the restorer that a Linux signal's handler returns through, which calls rt_sigreturn"},
	{"PreemptibleCode", "g.preemptibleCode", "Linker", roleGlobal, "the table of the code that the monitor may stop a green thread in: how many ranges, then each range's start and end as how far below the table they are, in the order of the code (README, \"Green threads\")"},
	{"SchedDrain", "rt.schedDrain", "Source", roleInternal, "(exit code) -> exit code: what the entry calls after main, which runs behaviours until none is left"},
	{"WorkerMain", "rt.workerMain", "Source", roleInternal, "(context): what a thread the runtime makes runs"},
	{"EnterBlocking", "rt.enterBlocking", "Source", roleInternal, "(): the start of a blocking call's bracket, which the monitor may take the processor from meanwhile; the link puts a routine that does nothing in its place in a program without the scheduler"},
	{"ExitBlocking", "rt.exitBlocking", "Source", roleInternal, "(): the end of a blocking call's bracket, which may wait for a processor; the link puts a routine that does nothing in its place in a program without the scheduler"},
	{"ExitClaim", "rt.exitClaim", "Source", roleInternal, "(context): what a thread that ends the process takes first, so that the monitor stops no thread again; the link puts a routine that does nothing in its place in a program without the scheduler"},
	{"CownAcquire", "rt.cown.acquire", "Source", roleInternal, "(behaviour record): what a when calls with the record it made, which queues the behaviour on each cown it names and makes it runnable once it holds them all"},
	{"BehaviourRelease", "rt.behaviour.release", "Source", roleInternal, "(behaviour record, finish) -> kept: what a behaviour's thunk calls after its body, which hands each cown to the behaviour queued after it, or gives 1 and keeps them while the behaviour is muted, the thunk's finish to run once it is unmuted"},
	{"BehaviourResumed", "rt.behaviour.resumed", "Source", roleInternal, "(behaviour record): what the finish of a behaviour's thunk calls once the behaviour is unmuted, which hands each cown it kept to the behaviour queued after it"},
	{"PinUnheld", "rt.pinUnheld", "Source", roleInternal, "(): the fatal error of an unpin by a thread that holds no pin, which the single-thread form of rt.unpin jumps to"},
	{"MainRun", "rt.mainRun", "Source", roleInternal, "(code) -> exit code: what the entry calls in place of main, with the address of rt.mainGreen: main runs as a green thread, and the main thread runs the scheduler until nothing is left"},
	{"MainGreen", "rt.mainGreen", "Runtime", roleInternal, "(green thread) -> exit code: the stub's code that main's green thread runs, which calls main, or each test of an executable of tests"},
	{"AsyncNew", "rt.async.new", "Source", roleInternal, "(thunk) -> coroutine: what an async call makes, a green thread of the caller's strand that runs the thunk of the function it starts, before its arguments are written"},
	{"AsyncStart", "rt.async.start", "Source", roleInternal, "(coroutine): what an async call calls once it has written the coroutine's arguments, which queues it to run in its strand"},
	{"AsyncAwait", "rt.async.await", "Source", roleCall, "(promise) -> value: await of a promise whose function throws nothing, which waits for the coroutine, gives what its function returned and frees it"},
	{"AsyncWait", "rt.async.wait", "Source", roleCall, "(promise): the wait of an await of a promise whose function throws"},
	{"AsyncError", "rt.async.error", "Source", roleCall, "(promise) -> error: what the function of a coroutine that rt.async.wait waited for failed with, 0 when it returned"},
	{"AsyncTake", "rt.async.take", "Source", roleCall, "(promise) -> value: what the function of a coroutine that rt.async.wait waited for returned, which frees the coroutine"},
	{"AsyncCancel", "rt.async.cancel", "Source", roleCall, "(promise): marks the coroutine of a promise cancelled, which ends the sleep it is in at once, before the cancel awaits it"},
	{"AsyncAwaitAny", "rt.async.awaitAny", "Source", roleCall, "(array) -> index: waits until a promise of an array of them has finished, a slot that holds none skipped, and gives the index of the first that has"},
	{"AsyncComplete", "rt.async.complete", "Source", roleCall, "(promise) -> bool: whether the coroutine or the reply of a promise has finished"},
	{"Sleep", "rt.sleep", "Source", roleCall, "(milliseconds): parks the running green thread for at least that long"},
	{"Yield", "rt.yield", "Source", roleCall, "(): lets the members of the strand that wait, else the runnable green threads, run first"},
	{"SchedPin", "rt.pin", "Source", roleCall, "() -> bool: pins the running green thread to its thread, Go's LockOSThread, one level deeper"},
	{"SchedUnpin", "rt.unpin", "Source", roleCall, "(): undoes one pin of the running green thread, Go's UnlockOSThread"},
	{"SchedPinDepth", "rt.pinDepth", "Source", roleCall, "() -> Count: how many pins the running green thread holds"},
	{"SchedCurrentMachine", "rt.currentMachine", "Source", roleCall, "() -> Count: the number of the thread running the green thread, from 1"},
	{"HashSeed", "rt.hash.seed", "Source", roleCall, "() -> the fold of no bytes under the process's secret, which the first hash of the process draws or reads from MAXON_HASH_SEED"},
	{"HashSeedDraw", "rt.hash.draw", "Source", roleCall, "() -> a fresh secret from the system, or the next of the sequence a pinned process draws"},
	{"HeapThreadAllocs", "rt.heap.threadAllocs", "Source", roleCall, "() -> how many boxes the running thread allocated"},
	{"HeapThreadFrees", "rt.heap.threadFrees", "Source", roleCall, "() -> how many boxes the running thread freed"},
	{"HeapThreadBytes", "rt.heap.threadBytes", "Source", roleCall, "() -> how many bytes the boxes the running thread allocated asked for"},
	{"HeapProcessAllocs", "rt.heap.processAllocs", "Source", roleCall, "() -> how many boxes every thread allocated"},
	{"HeapProcessFrees", "rt.heap.processFrees", "Source", roleCall, "() -> how many boxes every thread freed"},
	{"HeapProcessLive", "rt.heap.processLive", "Source", roleCall, "() -> how many boxes every thread allocated less those freed"},
	{"HeapProcessBytes", "rt.heap.processBytes", "Source", roleCall, "() -> how many bytes the boxes every thread allocated asked for"},
	{"HeapScavenge", "rt.heap.scavenge", "Source", roleCall, "() -> the bytes a release pass of the page heap gave back to the system"},
	{"RandomFill", "rt.random.fill", "Source", roleCall, "(ByteArray) -> 0: overwrites every byte of the array with the system's entropy"},
	{"HashWord", "rt.hash.word", "Source", roleCall, "(word, seed) -> the fold of the word's 8 bytes under the seed"},
	{"HashBytes", "rt.hash.bytes", "Source", roleCall, "(ByteArray, at, count, seed) -> the fold of the count bytes from at under the seed; a span past the end panics"},
	{"HashStringBytes", "rt.hash.string", "Source", roleCall, "(String, at, count, seed) -> the fold of the count bytes of the string from at under the seed; a span past the end panics"},
	{"BytesReadWord", "rt.bytes.readWord", "Source", roleCall, "(ByteArray, at, width, bigEndian, error) -> the width bytes from at as a word, the first the most significant where bigEndian; past the end sets the error to indexOutOfBounds"},
	{"BytesAppendWord", "rt.bytes.appendWord", "Source", roleCall, "(ByteArray, word, width, bigEndian): appends the word's low width bytes, the most significant first where bigEndian"},
	{"ServiceNew", "rt.service.new", "Source", roleCall, "(state) -> handle: a service, a cown that holds its state and its mailbox's gate, whose state the factory's behaviour stores when it is 0"},
	{"ServiceReserve", "rt.service.reserve", "Source", roleCall, "(handle) -> 0, or the SendError case plus 1: takes a place in the service's mailbox for a message, unless the service is shut down, and slows the sender when the service is behind"},
	{"ServiceStart", "rt.service.start", "Source", roleCall, "(state address): what the behaviour of a message calls as it starts, which gives its place in the mailbox back and makes the service the one its green thread runs a message of"},
	{"ServiceShutdown", "rt.service.shutdown", "Source", roleCall, "(handle): closes the service's mailbox, so every later send fails with serviceStopped; the messages queued still run"},
	{"ServiceMostQueued", "rt.service.mostQueued", "Source", roleCall, "(handle) -> count: the most messages a slowed send to the service found queued with its own"},
	{"ReplyNew", "rt.reply.new", "Source", roleCall, "() -> promise: the cell a message's reply comes back in, which an await of a promise waits for as it waits for a coroutine"},
	{"ReplyGive", "rt.reply.give", "Source", roleCall, "(promise, value, error): what the behaviour of a message calls with what its handler returned and failed with, which readies the green thread that awaits the reply"},

	{"ArrayNew", "rt.array.new", "Arrays", roleCall, "() -> Array"},
	{"ArrayLen", "rt.array.len", "Arrays", roleCall, "(array) -> count"},
	{"ArrayElem", "rt.array.elem", "Arrays", roleCall, "(array, index) -> address of the element"},
	{"ArrayInsert", "rt.array.insert", "Arrays", roleCall, "(array, index, element): moves it in at index, which may be the length"},
	{"ArrayRemove", "rt.array.remove", "Arrays", roleCall, "(array, index) -> element: moves it out"},
	{"ArrayPop", "rt.array.pop", "Arrays", roleCall, "(array of at least one element) -> element: moves the last out, with nothing after it to move"},
	{"ArrayFill", "rt.array.fill", "Arrays", roleCall, "(array, count, word): appends count copies of a word that needs no copy of its own, a scalar or the 0 of an empty slot, as as many inserts at the end would"},
	{"ArrayCapacity", "rt.array.capacity", "Arrays", roleCall, "(array or byte array) -> count: how many elements its buffer holds"},
	{"ArrayReserve", "rt.array.reserve", "Arrays", roleCall, "(array, count): makes the buffer hold count elements, so as many inserts after it never grow it; the length and the elements stay"},
	// An array of strings holds two words to an element, a String's, which the code moves in and out with stores and loads of
	// the addresses these give: the runtime moves only bytes.
	{"StrsElem", "rt.strs.elem", "Arrays", roleCall, "(array of strings, index) -> address of the element"},
	{"StrsOpen", "rt.strs.open", "Arrays", roleCall, "(array of strings, index) -> address of a new empty element at index, which may be the length, after what follows moves up by one"},
	{"StrsClose", "rt.strs.close", "Arrays", roleCall, "(array of strings, index): takes the element at index, which the code moved out, out of the buffer"},
	{"StrsFill", "rt.strs.fill", "Arrays", roleCall, "(array of strings, count): appends count empty elements"},
	{"StrsReserve", "rt.strs.reserve", "Arrays", roleCall, "(array of strings, count): makes the buffer hold count elements"},
	{"RecsElem", "rt.recs.elem", "Arrays", roleCall, "(array of records held in place, index, bytes) -> address of the element, of bytes bytes"},
	{"RecsOpen", "rt.recs.open", "Arrays", roleCall, "(array of records held in place, index, bytes) -> address of a new zeroed element at index, which may be the length, after what follows moves up by one"},
	{"RecsClose", "rt.recs.close", "Arrays", roleCall, "(array of records held in place, index, bytes): takes the element at index, whose words the code copied out, out of the buffer"},
	{"RecsReserve", "rt.recs.reserve", "Arrays", roleCall, "(array of records held in place, count, bytes): makes the buffer hold count elements"},
	{"RecsShrink", "rt.recs.shrink", "Arrays", roleCall, "(array of records held in place, length, bytes): sets the length to one that is not above it, keeping the buffer"},
	{"ArrayShrink", "rt.array.shrink", "Arrays", roleCall, "(array or byte array, length): sets the length to one that is not above it, keeping the buffer; what was above it was moved out or needs no drop"},

	{"FrozenFreeze", "rt.frozen.freeze", "Shared", roleCall, "(value) -> frozen: moves the value into a new box with a count of 1; the backend stores a String's word after it"},
	{"FrozenGet", "rt.frozen.get", "Shared", roleCall, "(frozen) -> what it holds, lent: an immutable borrow, valid while the frozen value is; the backend writes it in place, a load that leaves the count alone"},
	{"Share", "rt.share", "Shared", roleCall, "(frozen or cown) -> the same box: another owner, with the count raised by 1 in one locked step"},
	{"CownNew", "rt.cown.new", "Shared", roleCall, "(value) -> cown: moves the value into a new cown with a count of 1; the instruction's Routine, when it has one, ends the value instead of a drop, and the backend passes its code's address, or 0, as a second word"},

	{"BytesNew", "rt.bytes.new", "Bytes", roleCall, "() -> ByteArray"},
	{"BytesLen", "rt.bytes.len", "Bytes", roleCall, "(ByteArray) -> count"},
	{"BytesGet", "rt.bytes.get", "Bytes", roleCall, "(ByteArray, index) -> byte"},
	{"BytesElem", "rt.bytes.elem", "Bytes", roleCall, "(ByteArray, index) -> address of the byte, which a place of a byte-sized type lent by address is (ir.lentWidth)"},
	{"BytesSet", "rt.bytes.set", "Bytes", roleCall, "(ByteArray, index, byte)"},
	{"BytesInsert", "rt.bytes.insert", "Bytes", roleCall, "(ByteArray, index, byte): at index, which may be the length"},
	{"BytesRemove", "rt.bytes.remove", "Bytes", roleCall, "(ByteArray, index) -> byte"},
	{"BytesAppend", "rt.bytes.append", "Bytes", roleCall, "(ByteArray, other ByteArray)"},
	{"BytesAppendStr", "rt.bytes.appendStr", "Bytes", roleCall, "(ByteArray, text String)"},
	{"BytesToString", "rt.bytes.toString", "Bytes", roleCall, "(ByteArray) -> String"},

	// An OS entry that can fail takes last the address of a word, its error, which it sets to the case plus 1 of the
	// error its operation throws when it fails and to 0 when it does not; what it returns on a failure is nothing to read.
	// A path is a String, which lowering takes out of a FilePath and makes one of, so no entry knows a record's layout.
	{"OsArgs", "rt.os.args", "Os", roleCall, "() -> Array with String"},
	{"OsEnvironmentVariable", "rt.os.environmentVariable", "Os", roleCall, "(name String, error) -> String"},
	{"OsExecutablePath", "rt.os.executablePath", "Os", roleCall, "(error) -> String: the full path of the running executable"},
	{"OsCurrentPath", "rt.os.currentPath", "Os", roleCall, "() -> String: the working directory, panicking when Windows cannot give it"},
	{"OsProcessId", "rt.os.processId", "Os", roleCall, "() -> Count: the id of the running process"},
	{"OsSetExitCode", "rt.os.setExitCode", "Os", roleCall, "(code): the code the process ends with when main has returned and no behaviour is left, in place of main's"},
	{"OsExitAtOnce", "rt.os.exitAtOnce", "Os", roleCall, "(code): ends the process with the code at once, freeing nothing"},
	{"OsEnvironmentEntry", "rt.os.environmentEntry", "Os", roleCall, "(index) -> ByteArray: the NAME=VALUE entry of the process's environment at the index, in the order the system reports them; empty past the last entry, and one NUL byte where the system handed over no environment"},
	{"OsEnterBackgroundPriority", "rt.os.enterBackgroundPriority", "Os", roleCall, "() -> Count: puts the process at background priority (Windows' below-normal class, a POSIX nice of 10) and answers the priority the system reports afterwards"},
	{"OsClockNanos", "rt.os.clockNanos", "Os", roleCall, "() -> Count: the monotonic clock in nanoseconds, from QueryPerformanceCounter"},
	{"OsClockMillis", "rt.os.clockMillis", "Os", roleCall, "() -> Count: the monotonic clock in whole milliseconds, which is OsClockNanos divided by a million"},
	{"OsTickMillis", "rt.os.tickMillis", "Os", roleCall, "() -> Count: the milliseconds since the machine booted, from GetTickCount64 and the POSIX boot clock"},
	{"OsThreadCpuTicks", "rt.os.threadCpuTicks", "Os", roleCall, "() -> Count: the CPU time the calling green thread has used, on whichever threads it ran, in the cycles QueryThreadCycleTime counts on Windows and in nanoseconds under POSIX"},
	{"OsUnixNanos", "rt.os.unixNanos", "Os", roleCall, "() -> Count: the nanoseconds since 1970-01-01 UTC, in steps of the host calendar's own tick, the word of a signed 64-bit count"},
	{"OsUnixSeconds", "rt.os.unixSeconds", "Os", roleCall, "() -> Count: the whole seconds since 1970-01-01 UTC, which is OsUnixNanos divided by a billion, the word of a signed 64-bit count"},
	{"OsMachineCpuCount", "rt.os.machineCpuCount", "Os", roleCall, "() -> Count: the machine's logical processors, at least 1, whatever MAXON_MAX_PROCS asks"},
	{"OsProcessorCount", "rt.os.processorCount", "Os", roleCall, "() -> Count: how many workers the runtime runs behaviours on, as the scheduler counts them: the processors, or what MAXON_MAX_PROCS asks"},
	{"OsPathFrom", "rt.os.pathFrom", "Os", roleCall, "(path String, error) -> String: the path with the native separator for the other one"},
	{"OsReadText", "rt.os.readText", "Os", roleCall, "(path String, error) -> String"},
	{"OsReadBinary", "rt.os.readBinary", "Os", roleCall, "(path String, error) -> ByteArray: the whole content of a file, which osReadText reads as a String"},
	{"OsWriteTextMode", "rt.os.writeTextMode", "Os", roleCall, "(path String, content String, executable, error): executable is whether a file that the write creates gets the executable mode"},
	{"OsWriteBinaryMode", "rt.os.writeBinaryMode", "Os", roleCall, "(path String, content ByteArray, executable, error): executable is whether a file that the write creates gets the executable mode"},
	{"OsRename", "rt.os.rename", "Os", roleCall, "(from String, to String, error): replaces what is at to"},
	{"OsDelete", "rt.os.delete", "Os", roleCall, "(path String, error)"},
	{"OsFileInfo", "rt.os.fileInfo", "Os", roleCall, "(path String, size, created, accessed, attributes, error) -> modified: the times in Unix seconds, and the attributes as a word whose bit 0 is whether the path is a directory and bit 1 whether it is read-only"},
	{"OsIsDirectory", "rt.os.isDirectory", "Os", roleCall, "(path String) -> bool"},
	{"OsExists", "rt.os.exists", "Os", roleCall, "(path String) -> bool: whether a file or a directory is at the path"},
	{"OsCreateDirectory", "rt.os.createDirectory", "Os", roleCall, "(path String) -> bool: makes the missing parents too, and is whether the directory is there after it"},
	{"OsDeleteDirectory", "rt.os.deleteDirectory", "Os", roleCall, "(path String, error): removes an empty directory"},
	{"OsList", "rt.os.list", "Os", roleCall, "(path String, error) -> Array with String: the path joined to the name of each entry"},
	// The debug port's entries, on Windows; a read or a write that fails sets its error word as an OS entry's does.
	{"DebugLaunch", "rt.debug.launch", "Os", roleCall, "(executable String, arguments Array with String, outputToError, error) -> process id: starts the executable as the calling thread's debuggee"},
	{"DebugWaitEvent", "rt.debug.waitEvent", "Os", roleCall, "(budgetMs) -> ByteArray: the debug event that came within the milliseconds, empty for none"},
	{"DebugContinueEvent", "rt.debug.continueEvent", "Os", roleCall, "(processId, thread, unhandled) -> bool: unhandled hands the exception of the event on to the program"},
	{"DebugReadMemory", "rt.debug.readMemory", "Os", roleCall, "(process, address, count, error) -> ByteArray"},
	{"DebugWriteCode", "rt.debug.writeCode", "Os", roleCall, "(process, address, content ByteArray, error)"},
	{"DebugReadContext", "rt.debug.readContext", "Os", roleCall, "(thread, error) -> ByteArray: the thread's CONTEXT"},
	{"DebugWriteContext", "rt.debug.writeContext", "Os", roleCall, "(thread, context ByteArray, error)"},
	{"DebugSuspend", "rt.debug.suspend", "Os", roleCall, "(thread) -> bool"},
	{"DebugResume", "rt.debug.resume", "Os", roleCall, "(thread) -> bool"},
	{"DebugBreakIn", "rt.debug.breakIn", "Os", roleCall, "(process) -> bool"},
	{"DebugDetach", "rt.debug.detach", "Os", roleCall, "(processId) -> bool"},
	{"DebugCloseHandle", "rt.debug.closeHandle", "Os", roleCall, "(handle)"},
	{"DebugTerminate", "rt.debug.terminate", "Os", roleCall, "(process, exitCode) -> bool"},
	{"OsRun", "rt.os.run", "Os", roleCall, "(executable String, arguments Array with String, deadlineMs, stdout, stderr, error) -> exit code: runs the executable with the arguments quoted as the C runtime splits them, and stores what the child wrote to each stream as a String at the address given for it; a child still running deadlineMs milliseconds after it started, when that is not 0, is terminated and the run fails with the timeout case"},
	{"OsRunIn", "rt.os.runIn", "Os", roleCall, "(executable String, arguments Array with String, directory String, deadlineMs, stdout, stderr, error) -> exit code: rt.os.run with the child started in directory, relative to the caller's working directory"},
	{"OsRunInput", "rt.os.runInput", "Os", roleCall, "(executable String, arguments Array with String, input String, deadlineMs, stdout, stderr, error) -> exit code: rt.os.run with the child's standard input a pipe that holds the input and then ends"},
	{"SubprocessSpawn", "rt.subprocess.spawn", "Os", roleCall, "(argv ByteArray, argc, cwd cstring, env ByteArray, envInherit, stdinKind, stdinData cstring, stdoutKind, stdoutData cstring, stdoutLimit, stderrKind, stderrData cstring, stderrLimit, flags, stdinLength) -> handle or -1: starts the child that argc NUL-ended tokens of the argv blob name, the first the program, in the directory cwd names ('' for the caller's own) with the caller's environment or an env block that is all of it (envInherit 0), its standard input none, inherited, held open, the stdinLength bytes of stdinData or a file, and its standard output and error discarded, inherited, collected up to a limit (0 for none) or a file; a kind, a flag or a block that this step does not serve is refused, as a spawn that fails; the handle names a slot of the runtime's table and its generation; with the flag detach (subprocessDetach is the same entry) the child's streams are none, discarded, inherited or files, it leads a session of its own and, on Windows, leaves the jobs this process is in, and the answer is its process id, -1 for a spawn that failed, or -2 where the job this process is in does not allow the breakaway"},
	{"SubprocessGetPid", "rt.subprocess.getPid", "Os", roleCall, "(handle) -> the process id the spawn recorded, or -1 for a word that names no live child"},
	{"SubprocessWaitCollect", "rt.subprocess.waitCollect", "Os", roleCall, "(handle, ms) -> result or -1: parks until the child has exited and its output has ended, or until ms milliseconds (0 for none) have passed, which ends the child and keeps the output it had written; the result names what became of it, a word of the same kind as a handle, or is -1 for a word that names no child that no wait has taken"},
	{"SubprocessResultStatusKind", "rt.subprocess.resultStatusKind", "Os", roleCall, "(result) -> how the child ended: exited 0, signalled 1, timedOut 2, treeNotEnded 3, reapedElsewhere 4; 0 for a word that names no result"},
	{"SubprocessResultStatusCode", "rt.subprocess.resultStatusCode", "Os", roleCall, "(result) -> the exit code (a POSIX wait status gives 128 plus its signal), or the signal's number for a signalled child; 0 for a word that names no result"},
	{"SubprocessResultStdout", "rt.subprocess.resultStdout", "Os", roleCall, "(result) -> ByteArray: a fresh copy of what the child wrote to its standard output, empty for a stream that was not collected or a word that names no result"},
	{"SubprocessResultStderr", "rt.subprocess.resultStderr", "Os", roleCall, "(result) -> ByteArray: a fresh copy of what the child wrote to its standard error, empty for a stream that was not collected or a word that names no result"},
	{"SubprocessResultDurationMs", "rt.subprocess.resultDurationMs", "Os", roleCall, "(result) -> milliseconds from the spawn to the end of the wait; 0 for a word that names no result"},
	{"SubprocessResultRelease", "rt.subprocess.resultRelease", "Os", roleCall, "(result): gives back the result and the output it holds; a word that names no result is skipped"},
	{"SubprocessReleaseHandle", "rt.subprocess.releaseHandle", "Os", roleCall, "(handle): gives back the child's slot and what it still holds; a word that names no live child is skipped"},
	{"SubprocessEndTree", "rt.subprocess.endTree", "Os", roleCall, "(handle) -> 0 when the child and every process it started are gone, -3 when part of the tree is still running after the kill, -1 for a word that names no child that no wait has taken or a kill the system refused"},
	{"SubprocessSpawnStreaming", "rt.subprocess.spawnStreaming", "Os", roleCall, "(argv ByteArray, argc, cwd cstring, flags, env ByteArray, envInherit) -> handle or -1: starts the child as the attached spawn does, its standard input a pipe that the caller writes and holds open and its standard output and error pipes that the caller reads on demand, with no cap; a traced or a detached child is refused, as a spawn that fails; the handle names a slot of the runtime's table and its generation, as the attached spawn's does"},
	{"SubprocessReadAvailable", "rt.subprocess.readAvailable", "Os", roleCall, "(handle, stream, most) -> ByteArray: at most most bytes of the child's output (stream 0) or error (1) that were read from its pipe and not given yet, a fresh copy, and where there are none parks until there are some or the stream ends; empty at the end, for a stream that is neither, for most of 0 or less, for a failed read and for a word that names no live streaming child"},
	{"SubprocessStdoutState", "rt.subprocess.stdoutState", "Os", roleCall, "(handle) -> __SubprocessStreamState: what a read of the child's output answers next: open, atEof once a read found the end, readFailed once a read failed, noSuchChild for a word that names no live streaming child"},
	{"SubprocessStderrState", "rt.subprocess.stderrState", "Os", roleCall, "(handle) -> __SubprocessStreamState: stdoutState of the child's error"},
	{"SubprocessWriteStdinAll", "rt.subprocess.writeStdinAll", "Os", roleCall, "(handle, data cstring) -> 0 when every byte of the text up to its NUL was written to the child's input, parking while the pipe is full, 1 when the input is closed or the child no longer reads it, 2 while a payload queued for it is not written out, -1 for a word that names no live streaming child"},
	{"SubprocessWriteStdinBytes", "rt.subprocess.writeStdinBytes", "Os", roleCall, "(handle, data cstring, length) -> writeStdinAll of the length bytes of data, the NUL bytes among them"},
	{"SubprocessQueueStdin", "rt.subprocess.queueStdin", "Os", roleCall, "(handle, data cstring, length) -> 0 when a copy of the length bytes is queued, which the pushes write to the child's input and the last of them closes (nothing is written before a push), 2 while a payload is queued already, -1 for an input that is closed or a word that names no live streaming child"},
	{"SubprocessPushStdin", "rt.subprocess.pushStdin", "Os", roleCall, "(handle) -> how many bytes of the queued payload are not written yet (0 for none or once it is written out and the input closed), after one write that never waits; -1 for a word that names no live streaming child"},
	{"SubprocessAwaitStdinWritable", "rt.subprocess.awaitStdinWritable", "Os", roleCall, "(handle, passes) -> 0 once the input may take more (the event queue reported it writable, or the write in flight completed) or is closed, at once where the last push was not blocked; -1 for a word that names no live streaming child"},
	{"SubprocessCloseStdin", "rt.subprocess.closeStdin", "Os", roleCall, "(handle): closes the caller's end of the child's input, which ends a payload that is queued, and does nothing for an input that is closed or a word that names no live streaming child"},
	{"SubprocessWaitExit", "rt.subprocess.waitExit", "Os", roleCall, "(handle, ms) -> the child's exit code (a POSIX child that a signal ended gives 128 plus the signal), parking until it has exited or until ms milliseconds (0 for none) have passed, which ends the child's tree: -2 for a deadline that ended it, -3 for one after which part of the tree still ran, -1 for a word that names no live streaming child or a wait that another is parked in"},
	{"SubprocessPollExit", "rt.subprocess.pollExit", "Os", roleCall, "(handle) -> the child's exit code once it has exited, -1 while it runs, -5 for a word that names no live streaming child; never waits and never ends the child"},
	{"SubprocessExitSignal", "rt.subprocess.exitSignal", "Os", roleCall, "(handle) -> the number of the signal that ended the child (0 for one that exited, always on Windows) once it has exited, -1 while it runs, -5 for a word that names no live streaming child"},
	{"SubprocessLastErrorCode", "rt.subprocess.lastErrorCode", "Os", roleCall, "() -> the failed step of the last spawn above 32 bits and the Win32 code of its failure in them (a POSIX errno mapped to 2, 3, 5 or 31), 0 when nothing has failed"},
	{"SubprocessLastErrorMessage", "rt.subprocess.lastErrorMessage", "Os", roleCall, "() -> ByteArray: 'os error N' for the code of the last failure, empty when nothing has failed"},
	{"SubprocessResolveOnPath", "rt.subprocess.resolveOnPath", "Os", roleCall, "(name cstring) -> ByteArray: the path of the program that the name asks for, searched for as the system's own lookup does (PATH and PATHEXT on Windows, PATH on POSIX), or the name itself where none is found"},
	{"OsReadStdin", "rt.os.readStdin", "Os", roleCall, "(most) -> ByteArray: up to most bytes of standard input, parking until one arrives; empty only at its end or a failure"},
	{"OsStdoutWantsColor", "rt.os.stdoutWantsColor", "Os", roleCall, "() -> bool: whether standard output is a character device, NO_COLOR is unset and TERM is not dumb"},
}

// runtimeFunction is the function of the runtime's source (runtime/) that defines an entry, so that self links a call of
// the entry to it: Sig is its signature as the runtime declares it, "name(param Type, ...) Result", with a parameter's
// convention before its name when it is not let. Code calls it as it calls any function of the program, so the entry is
// called the same way whichever compiler emits the call; `minimaxon check runtime` holds the runtime to these signatures.
type runtimeFunction struct {
	Entry string // an entry's Name
	Sig   string
}

var runtimeFunctions = []runtimeFunction{
	{"Alloc", "alloc(bytes Count) Count"},
	{"Free", "free(box Count)"},
	{"Write", "write(handle Count, text String)"},
	{"Panic", "panicWith(message String)"},
	{"PanicAt", "panicAt(location String, message String)"},
	{"PanicEmptySlot", "panicEmptySlot(location String, index Count)"},
	{"CheckSimpleBytes", "checkSimpleBytes(bytes Count, length Count)"},
	{"Concat", "concat(left String, right String) String"},
	{"Itoa", "itoa(n Count, unsigned Bool) String"},
	{"DecimalLength", "decimalLength(n Count, unsigned Bool) Count"},
	{"StrJoinRoom", "stringJoinRoom(length Count) String"},
	{"StrJoinPut", "stringJoinPut(text String, part String) String"},
	{"StrJoinDigits", "stringJoinDigits(text String, n Count, unsigned Bool, length Count) String"},
	{"StrExtend", "stringExtend(text String, owned Bool, extra Count) String"},
	{"StrRelease", "stringRelease(text String, owned Bool, grown String)"},
	{"StrAppend", "stringAppend(text String, owned Bool, part String) String"},
	{"Ftoa", "ftoa(bits Count) String"},
	{"FtoaExact", "ftoaExact(bits Count) String"},
	{"FmtInt", "formatInteger(n Count, radix Count, flags Count, width Count) String"},
	{"FmtPad", "formatPadded(text String, width Count, zeroFill Bool) String"},
	{"FmtQuoted", "formatQuoted(text String, single Bool, width Count) String"},
	{"FmtFixed", "formatFixed(magnitude String, negative Bool, precision Count) String"},
	{"Streq", "streq(left String, right String) Bool"},
	{"StrLen", "stringLength(text String) Count"},
	// The runtime's source names no Character, so its entries take one as the String of its two words.
	{"CharEquals", "charEquals(left String, right String) Bool"},
	{"CharCompare", "charCompare(left String, right String) Count"},
	{"CharLength", "charLength(c String) Count"},
	{"CharToString", "charToString(c String) String"},
	{"CharCodepoint", "charCodepoint(c String) Count"},
	{"CharHash", "charHash(c String, seed Count) Count"},
	{"CharFromCodepoint", "charFromCodepoint(cp Count) String"},
	{"CharAdvanceBy", "charAdvanceBy(c String, step Count) String"},
	{"CharNextScalar", "charNextScalar(cp Count) Count"},
	{"CharOfBytes", "charOfBytes(text String, from Count, length Count) String"},
	{"StrByteAt", "stringByteAt(text String, index Count) Byte"},
	{"StrView", "stringView(text String, from Count, length Count) String"},
	{"StrCount", "stringCount(text String) Count"},
	{"StrGraphemeIndex", "stringGraphemeIndex(text String, bytePos Count) Count"},
	{"StrGraphemeBytePos", "stringGraphemeBytePos(text String, from Count, count Count) Count"},
	{"StrCStr", "stringCStr(text String) String"},
	{"StrFromCString", "stringFromCString(cs String) String"},
	{"StrFind", "stringFind(text String, needle String, from Count) Count"},
	{"StrFindLast", "stringFindLast(text String, needle String) Count"},
	{"StrHasAffix", "stringHasAffix(text String, affix String, atEnd Bool) Bool"},
	{"StrReplace", "stringReplace(text String, old String, replacement String, all Bool) String"},
	{"StrTrimFirst", "stringTrimFirst(text String) Count"},
	{"StrTrimLast", "stringTrimLast(text String, from Count) Count"},
	{"StrMapCase", "stringMapCase(text String, upper Bool) String"},
	{"StrCodePointAt", "stringCodePointAt(text String, bytePos Count) Count"},
	{"StrCodePointNext", "stringCodePointNext(text String, bytePos Count) Count"},
	{"StrCodePointCount", "stringCodePointCount(text String) Count"},
	{"StrUtf16Unit", "stringUtf16Unit(text String, state Count) Count"},
	{"StrUtf16Next", "stringUtf16Next(text String, state Count) Count"},
	{"StrUtf16Count", "stringUtf16Count(text String) Count"},
	{"PanicIndex", "panicIndex()"},
	{"CheckLeaks", "checkLeaks(code Count) Count"},
	{"SchedDrain", "schedDrain(code Count) Count"},
	{"WorkerMain", "workerMain(context Count)"},
	{"EnterBlocking", "enterBlocking()"},
	{"ExitBlocking", "exitBlocking()"},
	{"ExitClaim", "takeExitClaim(context Count)"},
	{"CownAcquire", "cownAcquire(record Count)"},
	{"BehaviourRelease", "behaviourRelease(record Count, finish Count) Count"},
	{"BehaviourResumed", "behaviourResumed(record Count)"},
	{"PinUnheld", "pinUnheld()"},
	{"MainRun", "mainRun(code Count) Count"},
	{"AsyncNew", "asyncNew(code Count) Count"},
	{"AsyncStart", "asyncStart(g Count)"},
	{"AsyncAwait", "asyncAwait(promise Count) Count"},
	{"AsyncWait", "asyncWait(promise Count)"},
	{"AsyncError", "asyncError(promise Count) Count"},
	{"AsyncTake", "asyncTake(promise Count) Count"},
	{"AsyncCancel", "asyncCancel(promise Count)"},
	{"AsyncAwaitAny", "asyncAwaitAny(array Count) Count"},
	{"AsyncComplete", "asyncComplete(promise Count) Bool"},
	{"Sleep", "sleepMs(milliseconds Count)"},
	{"Yield", "schedYield()"},
	{"SchedPin", "schedPin() Bool"},
	{"SchedUnpin", "schedUnpin()"},
	{"SchedPinDepth", "schedPinDepth() Count"},
	{"SchedCurrentMachine", "schedCurrentMachine() Count"},
	{"HashSeed", "hashSeed() Count"},
	{"HashSeedDraw", "drawHashSeed() Count"},
	{"HeapThreadAllocs", "heapThreadAllocs() Count"},
	{"HeapThreadFrees", "heapThreadFrees() Count"},
	{"HeapThreadBytes", "heapThreadBytes() Count"},
	{"HeapProcessAllocs", "heapProcessAllocs() Count"},
	{"HeapProcessFrees", "heapProcessFrees() Count"},
	{"HeapProcessLive", "heapProcessLive() Count"},
	{"HeapProcessBytes", "heapProcessBytes() Count"},
	{"HeapScavenge", "heapScavenge() Count"},
	{"RandomFill", "randomFill(array Count) Count"},
	{"HashWord", "hashWord(word Count, seed Count) Count"},
	{"HashBytes", "hashBytes(array Count, at Count, count Count, seed Count) Count"},
	{"HashStringBytes", "hashStringBytes(text String, at Count, count Count, seed Count) Count"},
	{"BytesReadWord", "bytesReadWord(array Count, at Count, width Count, bigEndian Bool, errorAddress Count) Count"},
	{"BytesAppendWord", "bytesAppendWord(array Count, word Count, width Count, bigEndian Bool)"},
	{"ServiceNew", "serviceCreate(state Count) Count"},
	{"ServiceReserve", "serviceReserve(handle Count) Count"},
	{"ServiceStart", "serviceStart(state Count)"},
	{"ServiceShutdown", "serviceShutdown(handle Count)"},
	{"ServiceMostQueued", "serviceMostQueued(handle Count) Count"},
	{"ReplyNew", "replyNew() Count"},
	{"ReplyGive", "replyGive(promise Count, value Count, error Count)"},
	{"FrozenFreeze", "frozenFreeze(value Count) Count"},
	{"FrozenGet", "frozenGet(frozen Count) Count"},
	{"Share", "share(box Count) Count"},
	{"CownNew", "cownNew(value Count, ending Count) Count"},
	{"ArrayNew", "arrayNew() Count"},
	{"ArrayLen", "arrayLength(array Count) Count"},
	{"ArrayElem", "arrayElem(array Count, index Count) Count"},
	{"ArrayInsert", "arrayInsert(array Count, index Count, element Count)"},
	{"ArrayRemove", "arrayRemove(array Count, index Count) Count"},
	{"ArrayPop", "arrayPop(array Count) Count"},
	{"ArrayFill", "arrayFill(array Count, count Count, word Count)"},
	{"ArrayCapacity", "arrayCapacity(array Count) Count"},
	{"ArrayReserve", "arrayReserve(array Count, count Count)"},
	{"StrsElem", "stringsElem(array Count, index Count) Count"},
	{"StrsOpen", "stringsOpen(array Count, index Count) Count"},
	{"StrsClose", "stringsClose(array Count, index Count)"},
	{"StrsFill", "stringsFill(array Count, count Count)"},
	{"StrsReserve", "stringsReserve(array Count, count Count)"},
	{"RecsElem", "recordsElem(array Count, index Count, bytes Count) Count"},
	{"RecsOpen", "recordsOpen(array Count, index Count, bytes Count) Count"},
	{"RecsClose", "recordsClose(array Count, index Count, bytes Count)"},
	{"RecsReserve", "recordsReserve(array Count, count Count, bytes Count)"},
	{"RecsShrink", "recordsShrink(array Count, length Count, bytes Count)"},
	{"ArrayShrink", "arrayShrink(array Count, length Count)"},
	{"BytesNew", "bytesNew() Count"},
	{"BytesLen", "bytesLength(array Count) Count"},
	{"BytesGet", "bytesGet(array Count, index Count) Byte"},
	{"BytesElem", "bytesElem(array Count, index Count) Count"},
	{"BytesSet", "bytesSet(array Count, index Count, value Byte)"},
	{"BytesInsert", "bytesInsert(array Count, index Count, value Byte)"},
	{"BytesRemove", "bytesRemove(array Count, index Count) Byte"},
	{"BytesAppend", "bytesAppend(array Count, other Count)"},
	{"BytesAppendStr", "bytesAppendStr(array Count, text String)"},
	{"BytesToString", "bytesToString(array Count) String"},
	{"OsArgs", "osArgs() Count"},
	{"OsEnvironmentVariable", "osEnvironmentVariable(name String, errorAddress Count) String"},
	{"OsExecutablePath", "osExecutablePath(errorAddress Count) String"},
	{"OsCurrentPath", "osCurrentPath() String"},
	{"OsProcessId", "osProcessId() Count"},
	{"OsSetExitCode", "osSetExitCode(code Count)"},
	{"OsExitAtOnce", "osExitAtOnce(code Count)"},
	{"OsEnvironmentEntry", "osEnvironmentEntry(index Count) Count"},
	{"OsEnterBackgroundPriority", "osEnterBackgroundPriority() Count"},
	{"OsClockNanos", "osClockNanos() Count"},
	{"OsClockMillis", "osClockMillis() Count"},
	{"OsTickMillis", "osTickMillis() Count"},
	{"OsThreadCpuTicks", "osThreadCpuTicks() Count"},
	{"OsUnixNanos", "osUnixNanos() Count"},
	{"OsUnixSeconds", "osUnixSeconds() Count"},
	{"OsMachineCpuCount", "machineCpuCount() Count"},
	{"OsProcessorCount", "osProcessorCount() Count"},
	{"OsPathFrom", "osPathFrom(path String, errorAddress Count) String"},
	{"OsReadText", "osReadText(path String, errorAddress Count) String"},
	{"OsReadBinary", "osReadBinary(path String, errorAddress Count) Count"},
	{"OsWriteTextMode", "osWriteTextMode(path String, content String, executable Bool, errorAddress Count)"},
	{"OsWriteBinaryMode", "osWriteBinaryMode(path String, content Count, executable Bool, errorAddress Count)"},
	{"OsRename", "osRename(source String, destination String, errorAddress Count)"},
	{"OsDelete", "osDelete(path String, errorAddress Count)"},
	{"OsFileInfo", "osFileInfo(path String, size Count, created Count, accessed Count, attributes Count, errorAddress Count) Count"},
	{"OsIsDirectory", "osIsDirectory(path String) Bool"},
	{"OsExists", "osExists(path String) Bool"},
	{"OsCreateDirectory", "osCreateDirectory(path String) Bool"},
	{"OsDeleteDirectory", "osDeleteDirectory(path String, errorAddress Count)"},
	{"OsList", "osList(path String, errorAddress Count) Count"},
	{"OsRun", "osRun(executable String, arguments Count, deadlineMs Count, stdout Count, stderr Count, errorAddress Count) Count"},
	{"OsRunIn", "osRunIn(executable String, arguments Count, directory String, deadlineMs Count, stdout Count, stderr Count, errorAddress Count) Count"},
	{"OsRunInput", "osRunInput(executable String, arguments Count, input String, deadlineMs Count, stdout Count, stderr Count, errorAddress Count) Count"},
	{"SubprocessSpawn", "subprocessSpawn(argv Count, argc Count, cwd String, env Count, envInherit Count, stdinKind Count, stdinData String, stdoutKind Count, stdoutData String, stdoutLimit Count, stderrKind Count, stderrData String, stderrLimit Count, flags Count, stdinLength Count) Count"},
	{"SubprocessGetPid", "subprocessGetPid(handle Count) Count"},
	{"SubprocessWaitCollect", "subprocessWaitCollect(handle Count, ms Count) Count"},
	{"SubprocessResultStatusKind", "subprocessResultStatusKind(result Count) Count"},
	{"SubprocessResultStatusCode", "subprocessResultStatusCode(result Count) Count"},
	{"SubprocessResultStdout", "subprocessResultStdout(result Count) Count"},
	{"SubprocessResultStderr", "subprocessResultStderr(result Count) Count"},
	{"SubprocessResultDurationMs", "subprocessResultDurationMs(result Count) Count"},
	{"SubprocessResultRelease", "subprocessResultRelease(result Count)"},
	{"SubprocessReleaseHandle", "subprocessReleaseHandle(handle Count)"},
	{"SubprocessEndTree", "subprocessEndTree(handle Count) Count"},
	{"SubprocessSpawnStreaming", "subprocessSpawnStreaming(argv Count, argc Count, cwd String, flags Count, env Count, envInherit Count) Count"},
	{"SubprocessReadAvailable", "subprocessReadAvailable(handle Count, stream Count, most Count) Count"},
	{"SubprocessStdoutState", "subprocessStdoutState(handle Count) Count"},
	{"SubprocessStderrState", "subprocessStderrState(handle Count) Count"},
	{"SubprocessWriteStdinAll", "subprocessWriteStdinAll(handle Count, data String) Count"},
	{"SubprocessWriteStdinBytes", "subprocessWriteStdinBytes(handle Count, data String, length Count) Count"},
	{"SubprocessQueueStdin", "subprocessQueueStdin(handle Count, data String, length Count) Count"},
	{"SubprocessPushStdin", "subprocessPushStdin(handle Count) Count"},
	{"SubprocessAwaitStdinWritable", "subprocessAwaitStdinWritable(handle Count, passes Count) Count"},
	{"SubprocessCloseStdin", "subprocessCloseStdin(handle Count)"},
	{"SubprocessWaitExit", "subprocessWaitExit(handle Count, ms Count) Count"},
	{"SubprocessPollExit", "subprocessPollExit(handle Count) Count"},
	{"SubprocessExitSignal", "subprocessExitSignal(handle Count) Count"},
	{"SubprocessLastErrorCode", "subprocessLastErrorCode() Count"},
	{"SubprocessLastErrorMessage", "subprocessLastErrorMessage() Count"},
	{"SubprocessResolveOnPath", "subprocessResolveOnPath(name String) Count"},
	{"OsReadStdin", "osReadStdin(most Count) Count"},
	{"OsStdoutWantsColor", "osStdoutWantsColor() Bool"},
	{"DebugLaunch", "debugLaunch(executable String, arguments Count, outputToError Bool, errorAddress Count) Count"},
	{"DebugWaitEvent", "debugWaitEvent(budgetMs Count) Count"},
	{"DebugContinueEvent", "debugContinueEvent(processId Count, thread Count, unhandled Bool) Bool"},
	{"DebugReadMemory", "debugReadMemory(process Count, address Count, count Count, errorAddress Count) Count"},
	{"DebugWriteCode", "debugWriteCode(process Count, address Count, content Count, errorAddress Count)"},
	{"DebugReadContext", "debugReadContext(thread Count, errorAddress Count) Count"},
	{"DebugWriteContext", "debugWriteContext(thread Count, context Count, errorAddress Count)"},
	{"DebugSuspend", "debugSuspend(thread Count) Bool"},
	{"DebugResume", "debugResume(thread Count) Bool"},
	{"DebugBreakIn", "debugBreakIn(process Count) Bool"},
	{"DebugDetach", "debugDetach(processId Count) Bool"},
	{"DebugCloseHandle", "debugCloseHandle(handle Count)"},
	{"DebugTerminate", "debugTerminate(process Count, exitCode Count) Bool"},
}
