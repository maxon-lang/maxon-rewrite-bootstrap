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
	{"Void", "b.voidTy"},
	{"Count", "b.countTy"},
	{"ElementIndex", "b.elementIndexTy"},
	{"Byte", "b.byteTy"},
	{"FullRangeInt", "b.fullRangeIntTy"},
	{"Codepoint", "b.codepointTy"},
	{"CodeUnit16", "b.codeUnit16Ty"},
	{"ExitCode", "b.exitCodeTy"},
	{"Milliseconds", "b.millisecondsTy"},
	{"MailboxCapacity", "b.mailboxCapacityTy"},
	{"FullRangeInt", "b.fullRangeIntTy"},
	// Float is the type of a float whose range is not known, which maxon names FullRangeFloat.
	{"Float", "b.anonFloatTy"},
	{"OrderedWord", "b.orderedWordTy"},
	{"HashDigest", "b.hashDigestTy"},
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
	{Name: "FullRangeInt", Field: "fullRangeIntTy", Lo: "I128.i64Min()", Hi: "I128.i64Max()"},
	{Name: "Byte", Field: "byteTy", Lo: "I128.zero()", Hi: "I128.fromCount(255 as Count)"},
	{Name: "FullRangeFloat", Field: "fullRangeFloatTy", Lo: "floatNegate(floatMaxBits)", Hi: "floatMaxBits", Float: true},
	{Name: "Codepoint", Field: "codepointTy", Lo: "I128.zero()", Hi: "I128.fromCount(1114111 as Count)"},
	{Name: "CodeUnit16", Field: "codeUnit16Ty", Lo: "I128.zero()", Hi: "I128.fromCount(65535 as Count)"},
	{Name: "Milliseconds", Field: "millisecondsTy", Lo: "I128.zero()", Hi: "I128.u64Max()"},
	// OrderedWord is the word maxon's library splits into bytes and assembles from them (maxon's stdlib/Builtins.maxon),
	// and HashDigest a seeded 64-bit digest (maxon's stdlib/Hasher.maxon), which the hash intrinsics give and take.
	{Name: "OrderedWord", Field: "orderedWordTy", Bits: 64},
	{Name: "HashDigest", Field: "hashDigestTy", Bits: 64},
	// MailboxCapacity is maxon's: how many messages a service's mailbox holds at most, `spawn(capacity: n)`.
	{Name: "MailboxCapacity", Field: "mailboxCapacityTy", Lo: "I128.fromCount(1 as Count)", Hi: "I128.fromCount(2147483648 as Count)"},
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
	{"anonU64Ty", "TypeID", "noType"},
	{"fullRangeFloatTy", "TypeID", "noType"},
	{"stringMethods", "FuncIDMap", "FuncIDMap.new()"},
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
	// GroupMemory is a raw buffer of the groups of a swiss table, laid out inline: each group is a control word, which the
	// buffer gives and takes as it is, then eight pairs, two words to a pair. A half holds a value of its type or is empty,
	// 0 for a managed half, so a table keeps a key and its value side by side, next to the word that says which of the
	// group's pairs hold one. Its drop and its copy go through every half by its type.
	{Key: "GroupMemory", Name: "__GroupMemory", Params: []string{"A", "B"}, Kind: "Groups", Managed: true, Raw: &rawType{
		Named:   rawSurface{What: "the raw buffer of a table's groups is nameable", Instead: "use 'Map'"},
		Methods: rawSurface{What: "a raw '__GroupMemory' buffer is reachable", Instead: "use 'Map'"},
	}},
	{Key: "List", Name: "List", Params: []string{"T"}, Managed: true, Subscriptable: true},
	{Key: "Pool", Name: "Pool", Params: []string{"T"}, Managed: true, Subscriptable: true},
	{Key: "Handle", Name: "Handle", Params: []string{"T"}},
	{Key: "Frozen", Name: "frozen", Params: []string{"T"}, Managed: true, Shared: true},
	{Key: "Cown", Name: "cown", Params: []string{"T"}, Managed: true, Shared: true},
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
	// SendError is what a send to a service fails with: maxon's cases of `<Service>.<message>.sendError`, in its order, whose
	// index plus 1 is the word rt.service.reserve gives for each.
	{Key: "SendError", Name: "SendError", Cases: []enumCase{
		{"mailboxFull", "SendMailboxFull"},
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
	// even where it is the first argument. A let parameter of an enum row may end in " = case", the case a call that leaves the
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
	// Lends is whether the method gives a place in its receiver rather than a value, as a function that returns borrowed
	// does.
	Lends bool
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
		raw("heapAddress", nil, "Count", "RawHeapAddress"),
		raw("stdoutAddress", nil, "Count", "RawStdoutAddress"),
		raw("stderrAddress", nil, "Count", "RawStderrAddress"),
		// The atomic operations are full fences and give the word they found at the address.
		raw("atomicAdd", append(slices.Clone(at), "value Count"), "Count", "RawAtomicAdd"),
		raw("atomicExchange", append(slices.Clone(at), "value Count"), "Count", "RawAtomicExchange"),
		raw("compareExchange", append(slices.Clone(at), "expected Count", "desired Count"), "Count", "RawCompareExchange"),
		raw("spinPause", nil, "", "RawSpinPause"),
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
	{Owner: "Array", Name: "filled", Static: true, Params: []string{"count ElementIndex", "consumes value T"}, Result: "Self", Bounds: []string{"T Copyable"}, Intrinsic: "ArrayFilled"},
	{Owner: "Array", Name: "count", Result: "Count", Intrinsic: "ArrayCount"},
	{Owner: "Array", Name: "isEmpty", Result: "Bool", Intrinsic: "ArrayIsEmpty"},
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
	// contains, containsSequence and equals are maxon's for an element that is Equatable; the checker holds them to the
	// elements this compiler compares itself (checkElementsComparable).
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
	// A cursor starts at the first element, so an empty buffer has none; a move that would leave the elements fails and
	// leaves it where it was, so a cursor always names an element, which current() lends.
	{Owner: "ManagedMemory", Name: "createCursor", Result: "ManagedMemoryCursor with T", Throws: "ManagedMemoryError", Intrinsic: "CursorCreate"},
	{Owner: "ManagedMemoryCursor", Name: "current", Result: "T", Lends: true, Intrinsic: "CursorCurrent"},
	{Owner: "ManagedMemoryCursor", Name: "index", Result: "ElementIndex", Intrinsic: "CursorIndex"},
	{Owner: "ManagedMemoryCursor", Name: "advance", Self: "mutates", Throws: "ManagedMemoryError", Intrinsic: "CursorAdvance"},
	{Owner: "ManagedMemoryCursor", Name: "retreat", Self: "mutates", Throws: "ManagedMemoryError", Intrinsic: "CursorRetreat"},
	{Owner: "ManagedMemoryCursor", Name: "seek", Self: "mutates", Params: []string{"index ElementIndex"}, Throws: "ManagedMemoryError", Intrinsic: "CursorSeek"},
	{Owner: "ManagedMemoryCursor", Name: "peek", Params: []string{"ahead ElementIndex"}, Result: "T", Bounds: []string{"T Copyable"}, Throws: "ManagedMemoryError", Intrinsic: "CursorPeek"},

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

	{Owner: "List", Name: "create", Static: true, Result: "Self", Intrinsic: "ListCreate"},
	{Owner: "List", Name: "count", Result: "Count", Intrinsic: "ListCount"},
	{Owner: "List", Name: "isEmpty", Result: "Bool", Intrinsic: "ListIsEmpty"},
	{Owner: "List", Name: "contains", Params: []string{"handle Handle with T"}, Result: "Bool", Intrinsic: "ListContains"},
	{Owner: "List", Name: "prepend", Self: "mutates", Params: []string{"consumes value T"}, Result: "Handle with T", Intrinsic: "ListPrepend"},
	{Owner: "List", Name: "append", Self: "mutates", Params: []string{"consumes value T"}, Result: "Handle with T", Intrinsic: "ListAppend"},
	{Owner: "List", Name: "removeFirst", Self: "mutates", Result: "T", Throws: "ArrayError", Intrinsic: "ListRemoveFirst"},
	{Owner: "List", Name: "removeLast", Self: "mutates", Result: "T", Throws: "ArrayError", Intrinsic: "ListRemoveLast"},
	{Owner: "List", Name: "first", Result: "Handle with T", Throws: "ArrayError", Intrinsic: "ListFirst"},
	{Owner: "List", Name: "last", Result: "Handle with T", Throws: "ArrayError", Intrinsic: "ListLast"},
	{Owner: "List", Name: "next", Params: []string{"handle Handle with T"}, Result: "Handle with T", Throws: "ArrayError", Intrinsic: "ListNext"},
	{Owner: "List", Name: "prev", Params: []string{"handle Handle with T"}, Result: "Handle with T", Throws: "ArrayError", Intrinsic: "ListPrev"},
	{Owner: "List", Name: "remove", Self: "mutates", Params: []string{"handle Handle with T"}, Result: "T", Throws: "ArrayError", Intrinsic: "ListRemove"},
	{Owner: "List", Name: "moveToFront", Self: "mutates", Params: []string{"handle Handle with T"}, Intrinsic: "ListMoveToFront"},
	{Owner: "List", Name: "dropEmpty", Self: "consumes", Intrinsic: "ListDropEmpty"},

	{Owner: "Pool", Name: "create", Static: true, Result: "Self", Intrinsic: "PoolCreate"},
	{Owner: "Pool", Name: "count", Result: "Count", Intrinsic: "PoolCount"},
	{Owner: "Pool", Name: "isEmpty", Result: "Bool", Intrinsic: "PoolIsEmpty"},
	{Owner: "Pool", Name: "contains", Params: []string{"handle Handle with T"}, Result: "Bool", Intrinsic: "PoolContains"},
	{Owner: "Pool", Name: "insert", Self: "mutates", Params: []string{"consumes value T"}, Result: "Handle with T", Intrinsic: "PoolInsert"},
	{Owner: "Pool", Name: "remove", Self: "mutates", Params: []string{"handle Handle with T"}, Result: "T", Throws: "ArrayError", Intrinsic: "PoolRemove"},
	{Owner: "Pool", Name: "dropEmpty", Self: "consumes", Intrinsic: "PoolDropEmpty"},

	{Owner: "String", Name: "byteLength", Result: "Count", Intrinsic: "StringByteLength"},
	{Owner: "String", Name: "byteAt", Params: []string{"index Count"}, Result: "Byte", Intrinsic: "StringByteAt"},
	// sliceBytes is maxon's name for the byte range, which keeps slice for a range of graphemes.
	{Owner: "String", Name: "sliceBytes", Params: []string{"start Count", "endPos Count"}, Result: "String", Intrinsic: "StringSliceBytes"},
	{Owner: "String", Name: "toByteArray", Result: "Array with Byte", Intrinsic: "StringToByteArray"},
	{Owner: "String", Name: "from", Static: true, Params: []string{"bytes Array with Byte"}, Result: "String", Intrinsic: "StringFrom"},
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
	{Owner: "String", Name: "append", Self: "mutates", Params: []string{"other String"}, Intrinsic: "StringAppend", OwnsOperands: true, MayOverlap: true},
	// count and every position in a string are of grapheme clusters; startIndex, endIndex and the searches give a StringIndex.
	{Owner: "String", Name: "count", Result: "Count", Intrinsic: "StringCount"},
	{Owner: "String", Name: "contains", Params: []string{"needle String"}, Result: "Bool", Intrinsic: "StringContains"},
	{Owner: "String", Name: "startsWith", Params: []string{"prefix String"}, Result: "Bool", Intrinsic: "StringStartsWith"},
	{Owner: "String", Name: "endsWith", Params: []string{"suffix String"}, Result: "Bool", Intrinsic: "StringEndsWith"},
	{Owner: "String", Name: "findFirst", Params: []string{"needle String"}, Result: "StringIndex", Throws: "StringError", Intrinsic: "StringFindFirst"},
	{Owner: "String", Name: "findLast", Params: []string{"needle String"}, Result: "StringIndex", Throws: "StringError", Intrinsic: "StringFindLast"},
	{Owner: "String", Name: "startIndex", Result: "StringIndex", Intrinsic: "StringStartIndex"},
	{Owner: "String", Name: "endIndex", Result: "StringIndex", Intrinsic: "StringEndIndex"},
	{Owner: "String", Name: "slice", Params: []string{"start StringIndex", "endIndex StringIndex"}, Result: "String", Intrinsic: "StringSlice"},
	{Owner: "String", Name: "sliceLength", Params: []string{"start StringIndex", "length Count"}, Result: "String", Intrinsic: "StringSliceLength"},
	{Owner: "String", Name: "replace", Params: []string{"old String", "with String"}, Result: "String", Intrinsic: "StringReplace"},
	{Owner: "String", Name: "replaceFirst", Params: []string{"old String", "with String"}, Result: "String", Intrinsic: "StringReplaceFirst"},
	// trim takes no CharacterSet: it removes whitespace and newlines, maxon's default.
	{Owner: "String", Name: "trim", Result: "String", Intrinsic: "StringTrim"},
	{Owner: "String", Name: "trimStart", Result: "String", Intrinsic: "StringTrimStart"},
	{Owner: "String", Name: "trimEnd", Result: "String", Intrinsic: "StringTrimEnd"},
	{Owner: "String", Name: "toLower", Result: "String", Intrinsic: "StringToLower"},
	{Owner: "String", Name: "toUpper", Result: "String", Intrinsic: "StringToUpper"},
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
	// nowNanos is maxon's Clock.nowNanos, whose InstantNanos is an unsigned 64-bit integer, which Count is.
	{Owner: "Clock", Name: "nowNanos", Static: true, Result: "Count", Intrinsic: "ClockNowNanos"},
	// processorCount is maxon's Scheduler.processorCount, whose SchedulerProcessorCount is an unsigned 64-bit integer, which Count is.
	{Owner: "Scheduler", Name: "processorCount", Static: true, Result: "Count", Intrinsic: "SchedulerProcessorCount"},
	// yield is maxon's Scheduler.yield: the coroutines of the caller's strand that wait to run, else the green threads that
	// are runnable, run before the caller goes on.
	{Owner: "Scheduler", Name: "yield", Static: true, Intrinsic: "SchedulerYield"},
	// sleep is maxon's: the running green thread parks for at least the milliseconds, and its strand's other coroutines run
	// meanwhile.
	{Name: "sleep", Params: []string{"milliseconds Milliseconds"}, Intrinsic: "Sleep"},
	// shutdown is maxon's: the service's mailbox closes, so every later send fails with serviceStopped, and the messages
	// queued already run. It always succeeds.
	{Owner: "Service", Name: "shutdown", Intrinsic: "ServiceShutdown"},
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
	{Owner: "File", Name: "writeText", Static: true, Params: []string{"path FilePath", "content String", "mode FilePermission = normal"}, Throws: "FileWriteError", Intrinsic: "FileWriteText"},
	{Owner: "File", Name: "writeBinary", Static: true, Params: []string{"path FilePath", "content Array with Byte", "mode FilePermission = normal"}, Throws: "FileWriteError", Intrinsic: "FileWriteBinary"},
	{Owner: "File", Name: "rename", Static: true, Params: []string{"from FilePath", "to FilePath"}, Throws: "FileRenameError", Intrinsic: "FileRename"},
	{Owner: "File", Name: "delete", Static: true, Params: []string{"path FilePath"}, Throws: "FileDeleteError", Intrinsic: "FileDelete"},
	// exists is maxon's File.exists: whether a file or a directory is at the path.
	{Owner: "File", Name: "exists", Static: true, Params: []string{"path FilePath"}, Result: "Bool", Intrinsic: "FileExists"},
	{Owner: "File", Name: "info", Static: true, Params: []string{"path FilePath"}, Result: "FileInfo", Throws: "FileInfoError", Intrinsic: "FileInfo"},
	{Owner: "Directory", Name: "isDirectory", Static: true, Params: []string{"path FilePath"}, Result: "Bool", Intrinsic: "DirectoryIsDirectory"},
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
	{Owner: "CollectedOutput", Name: "exitCode", Result: "ExitCode", Intrinsic: "CollectedOutputExitCode"},
	{Owner: "CollectedOutput", Name: "succeeded", Result: "Bool", Intrinsic: "CollectedOutputSucceeded"},

	{Owner: "Builtins", Name: "currentProcessId", Static: true, Result: "Count", Intrinsic: "ProcessId", Instead: "Process.id()"},
	{Owner: "Builtins", Name: "currentTimeNanos", Static: true, Result: "Count", Intrinsic: "ClockNowNanos", Instead: "Clock.nowNanos()"},
	// floatToBits is the 64 bits of a double read as an integer, as maxon's is: a reinterpretation, which the library's
	// float hash folds.
	{Owner: "Builtins", Name: "floatToBits", Static: true, Params: []string{"_ Float"}, Result: "FullRangeInt", Intrinsic: "FloatToBits", Instead: "the float's own operations"},
	// The seeded hash of maxon's runtime, its software fold: hashSeed is the fold of no bytes under the process's secret,
	// which a Hasher starts from; hashWord and hashBytes fold a word or the bytes of a span of an array under a seed;
	// drawHashSeed draws a fresh secret from the system, or the next of the pinned process's sequence.
	// trailingZeros is the number of 0 bits of a word below its lowest 1 bit, and 64 for 0: the place of the first match
	// in a word of matches, one instruction where the machine has one.
	{Owner: "Builtins", Name: "trailingZeros", Static: true, Params: []string{"_ Count"}, Result: "Count", Intrinsic: "TrailingZeros", Instead: "a loop over the word's bits"},
	{Owner: "Builtins", Name: "hashSeed", Static: true, Result: "HashDigest", Intrinsic: "HashSeed", Instead: "Hasher.create()"},
	{Owner: "Builtins", Name: "drawHashSeed", Static: true, Result: "HashDigest", Intrinsic: "HashSeedDraw", Instead: "HashSeed.random()"},
	{Owner: "Builtins", Name: "hashWord", Static: true, Params: []string{"_ HashDigest", "_ HashDigest"}, Result: "HashDigest", Intrinsic: "HashWord", Instead: "a Hasher's combine(value)"},
	{Owner: "Builtins", Name: "hashBytes", Static: true, Params: []string{"_ Array with Byte", "_ ElementIndex", "_ ElementIndex", "_ HashDigest"}, Result: "HashDigest", Intrinsic: "HashBytes", Instead: "a Hasher's combineBytes(bytes)"},
	// hashBytes of a String, whose box holds its bytes inline: the call of hashBytes whose first argument is a String.
	{Owner: "Builtins", Name: "hashBytes", Static: true, Params: []string{"_ String", "_ ElementIndex", "_ ElementIndex", "_ HashDigest"}, Result: "HashDigest", Intrinsic: "HashStringBytes", Field: "HashStringBytes", Instead: "a Hasher's combineBytes(bytes)"},
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
	{Key: "Interpreter", Doc: "no unit: only the interpreter runs the entry, and the compiler refuses a call of it"},
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
// An entry given an index, a key or a handle that names nothing traps. A handle is a word and never 0, so an entry that
// returns one returns 0 for none. A frozen value and a cown have a count of owners: an entry that makes another owner
// raises it, dropping an owner lowers it, and the box is freed when it reaches 0.
var entries = []entry{
	{"Entry", "rt.entry", "Runtime", roleInternal, "the executable's entry point, which calls main"},
	{"Alloc", "rt.alloc", "Runtime", roleInternal, "(ByteArray) -> a zeroed box"},
	{"Free", "rt.free", "Runtime", roleInternal, "(box): 0 is skipped"},
	{"Write", "rt.write", "Runtime", roleCall, "(handle, text String)"},
	{"Panic", "rt.panic", "Runtime", roleCall, "(message String): exits"},
	{"PanicAt", "rt.panicAt", "Runtime", roleCall, "(location String, message String): exits, saying where the program's panic is"},
	{"Concat", "rt.concat", "Runtime", roleCall, "(String, String) -> String"},
	{"Itoa", "rt.itoa", "Runtime", roleCall, "(n, unsigned) -> String"},
	{"Ftoa", "rt.ftoa", "Runtime", roleCall, "(bits) -> String: the text of the double of the bits"},
	{"FtoaExact", "rt.ftoaExact", "Runtime", roleCall, "(bits) -> String: the exact decimal of the magnitude of the double of the bits, or inf or nan"},
	{"FmtInt", "rt.fmt.int", "Runtime", roleCall, "(n, radix, flags, width) -> String: the word in the base, in a field"},
	{"FmtPad", "rt.fmt.pad", "Runtime", roleCall, "(text String, width, zeroFill) -> String: the text of a number in a field"},
	{"FmtQuoted", "rt.fmt.quoted", "Runtime", roleCall, "(text String, single, width) -> String: the text as a literal, in a field"},
	{"FmtFixed", "rt.fmt.fixed", "Runtime", roleCall, "(magnitude String, negative, precision) -> String: the exact decimal rounded to the places"},
	{"Streq", "rt.streq", "Runtime", roleCall, "(String, String) -> bool"},
	{"StrLen", "rt.str.len", "Runtime", roleCall, "(String) -> count of bytes"},
	{"StrByteAt", "rt.str.byteAt", "Runtime", roleCall, "(String, index) -> byte"},
	{"StrSlice", "rt.str.slice", "Runtime", roleCall, "(String, from, until) -> String"},
	{"StrCount", "rt.str.count", "Runtime", roleCall, "(String) -> count of its grapheme clusters"},
	{"StrGraphemeIndex", "rt.str.graphemeIndex", "Runtime", roleCall, "(String, byte position) -> count of the clusters that start before it"},
	{"StrGraphemeBytePos", "rt.str.graphemeBytePos", "Runtime", roleCall, "(String, from, count) -> the byte position that count clusters after from end at, or the end"},
	{"StrFind", "rt.str.find", "Runtime", roleCall, "(String, needle String, from) -> the position of the first match at or after from, plus 1, or 0"},
	{"StrFindLast", "rt.str.findLast", "Runtime", roleCall, "(String, needle String) -> the position of the last match, plus 1, or 0"},
	{"StrHasAffix", "rt.str.hasAffix", "Runtime", roleCall, "(String, affix String, atEnd) -> whether it starts with the affix, or ends with it where atEnd"},
	{"StrReplace", "rt.str.replace", "Runtime", roleCall, "(String, old String, replacement String, all) -> String with the first match, or each, replaced"},
	{"StrTrim", "rt.str.trim", "Runtime", roleCall, "(String, trimStart, trimEnd) -> String without the whitespace at those ends"},
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
	{"ImageDataStart", "g.imageDataStart", "Linker", roleGlobal, "the start of the data section, where every string literal lives, so the runtime can tell an immortal string from one on the heap"},
	{"ImageDataEnd", "g.imageDataEnd", "Linker", roleGlobal, "the end of the data section"},
	{"CheckLeaks", "rt.checkLeaks", "Source", roleInternal, "(exit code) -> exit code: what the entry of an executable built to count its allocations calls after main, which exits with a leak report when a box is still live"},
	{"TlsSlot", "g.tlsSlot", "Runtime", roleGlobal, "the thread-local slot that each thread keeps its context in, which on Windows the entry takes with TlsAlloc and a thread sets with TlsSetValue, and on Linux is the main thread's word that fs points at"},
	{"TlsOffset", "g.tlsOffset", "Runtime", roleGlobal, "where the thread-local slot is in a thread's TEB, which a read of the thread's context loads at"},
	{"MainContext", "g.mainContext", "Runtime", roleGlobal, "the context of the main thread, which the entry stores in the thread-local slot"},
	{"ProcessStart", "g.processStart", "Runtime", roleGlobal, "the stack pointer a Linux process starts with, at the argument count, then the arguments and the environment, which the entry keeps"},
	{"ThreadStart", "rt.threadStart", "Runtime", roleInternal, "(context) by Win64: the start of a thread the runtime makes, which keeps stack for the fault handler, stores the context in the thread-local slot and calls workerMain"},
	{"StackSwitch", "rt.stackSwitch", "Runtime", roleInternal, "(pair) -> the record it left: saves the registers of the running stack into the switch record at the pair's first word and goes on the stack of the record at its second, by the internal convention (README, \"Green threads\")"},
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
	{"CownRelease", "rt.cown.release", "Source", roleInternal, "(behaviour record): what a behaviour's thunk calls after its body, which hands each cown to the behaviour queued after it"},
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
	{"HashSeed", "rt.hash.seed", "Source", roleCall, "() -> the fold of no bytes under the process's secret, which the first hash of the process draws or reads from MAXON_HASH_SEED"},
	{"HashSeedDraw", "rt.hash.draw", "Source", roleCall, "() -> a fresh secret from the system, or the next of the sequence a pinned process draws"},
	{"HashWord", "rt.hash.word", "Source", roleCall, "(word, seed) -> the fold of the word's 8 bytes under the seed"},
	{"HashBytes", "rt.hash.bytes", "Source", roleCall, "(ByteArray, at, count, seed) -> the fold of the count bytes from at under the seed; a span past the end panics"},
	{"HashStringBytes", "rt.hash.string", "Source", roleCall, "(String, at, count, seed) -> the fold of the count bytes of the string from at under the seed; a span past the end panics"},
	{"BytesReadWord", "rt.bytes.readWord", "Source", roleCall, "(ByteArray, at, width, bigEndian, error) -> the width bytes from at as a word, the first the most significant where bigEndian; past the end sets the error to indexOutOfBounds"},
	{"BytesAppendWord", "rt.bytes.appendWord", "Source", roleCall, "(ByteArray, word, width, bigEndian): appends the word's low width bytes, the most significant first where bigEndian"},
	{"ServiceNew", "rt.service.new", "Source", roleCall, "(state, capacity) -> handle: a service, a cown that holds its state and its mailbox's gate, whose state the factory's behaviour stores when it is 0"},
	{"ServiceReserve", "rt.service.reserve", "Source", roleCall, "(handle) -> 0, or the SendError case plus 1: takes a place in the service's mailbox for a message, unless the mailbox holds its capacity or the service is shut down"},
	{"ServiceTaken", "rt.service.taken", "Source", roleCall, "(state address): what the behaviour of a message calls as it starts, which gives its place in the mailbox back"},
	{"ServiceShutdown", "rt.service.shutdown", "Source", roleCall, "(handle): closes the service's mailbox, so every later send fails with serviceStopped; the messages queued still run"},
	{"ReplyNew", "rt.reply.new", "Source", roleCall, "() -> promise: the cell a message's reply comes back in, which an await of a promise waits for as it waits for a coroutine"},
	{"ReplyGive", "rt.reply.give", "Source", roleCall, "(promise, value, error): what the behaviour of a message calls with what its handler returned and failed with, which readies the green thread that awaits the reply"},

	{"ArrayNew", "rt.array.new", "Arrays", roleCall, "() -> Array"},
	{"ArrayLen", "rt.array.len", "Arrays", roleCall, "(array) -> count"},
	{"ArrayElem", "rt.array.elem", "Arrays", roleCall, "(array, index) -> address of the element"},
	{"ArrayInsert", "rt.array.insert", "Arrays", roleCall, "(array, index, element): moves it in at index, which may be the length"},
	{"ArrayRemove", "rt.array.remove", "Arrays", roleCall, "(array, index) -> element: moves it out"},
	{"ArrayFill", "rt.array.fill", "Arrays", roleCall, "(array, count, word): appends count copies of a word that needs no copy of its own, a scalar or the 0 of an empty slot, as as many inserts at the end would"},
	{"ArrayReserve", "rt.array.reserve", "Arrays", roleCall, "(array, count): makes the buffer hold count elements, so as many inserts after it never grow it; the length and the elements stay"},
	// An array of strings holds two words to an element, a String's, which the code moves in and out with stores and loads of
	// the addresses these give: the runtime moves only bytes.
	{"StrsElem", "rt.strs.elem", "Arrays", roleCall, "(array of strings, index) -> address of the element"},
	{"StrsOpen", "rt.strs.open", "Arrays", roleCall, "(array of strings, index) -> address of a new empty element at index, which may be the length, after what follows moves up by one"},
	{"StrsClose", "rt.strs.close", "Arrays", roleCall, "(array of strings, index): takes the element at index, which the code moved out, out of the buffer"},
	{"StrsFill", "rt.strs.fill", "Arrays", roleCall, "(array of strings, count): appends count empty elements"},
	{"StrsReserve", "rt.strs.reserve", "Arrays", roleCall, "(array of strings, count): makes the buffer hold count elements"},
	{"ArrayShrink", "rt.array.shrink", "Arrays", roleCall, "(array or byte array, length): sets the length to one that is not above it, keeping the buffer; what was above it was moved out or needs no drop"},

	{"ListNew", "rt.list.new", "Interpreter", roleCall, "() -> list"},
	{"ListLen", "rt.list.len", "Interpreter", roleCall, "(list) -> count"},
	{"ListContains", "rt.list.contains", "Interpreter", roleCall, "(list, handle) -> bool"},
	{"ListElem", "rt.list.elem", "Interpreter", roleCall, "(list, handle) -> address of the element"},
	{"ListPushFront", "rt.list.pushFront", "Interpreter", roleCall, "(list, element) -> handle: moves it in"},
	{"ListPushBack", "rt.list.pushBack", "Interpreter", roleCall, "(list, element) -> handle: moves it in"},
	{"ListPopFront", "rt.list.popFront", "Interpreter", roleCall, "(list) -> element: moves it out; traps if empty"},
	{"ListPopBack", "rt.list.popBack", "Interpreter", roleCall, "(list) -> element: moves it out; traps if empty"},
	{"ListFirst", "rt.list.first", "Interpreter", roleCall, "(list) -> handle, or 0 if empty"},
	{"ListLast", "rt.list.last", "Interpreter", roleCall, "(list) -> handle, or 0 if empty"},
	{"ListNext", "rt.list.next", "Interpreter", roleCall, "(list, handle) -> handle, or 0 after the last"},
	{"ListPrev", "rt.list.prev", "Interpreter", roleCall, "(list, handle) -> handle, or 0 before the first"},
	{"ListRemove", "rt.list.remove", "Interpreter", roleCall, "(list, handle) -> element: moves it out"},
	{"ListMoveToFront", "rt.list.moveToFront", "Interpreter", roleCall, "(list, handle)"},
	{"ListCopy", "rt.list.copy", "Interpreter", roleCall, "(list) -> list: every element copied, handles still valid in the copy"},

	{"PoolNew", "rt.pool.new", "Interpreter", roleCall, "() -> pool"},
	{"PoolLen", "rt.pool.len", "Interpreter", roleCall, "(pool) -> count"},
	{"PoolContains", "rt.pool.contains", "Interpreter", roleCall, "(pool, handle) -> bool"},
	{"PoolElem", "rt.pool.elem", "Interpreter", roleCall, "(pool, handle) -> address of the element"},
	{"PoolInsert", "rt.pool.insert", "Interpreter", roleCall, "(pool, element) -> handle: moves it in"},
	{"PoolRemove", "rt.pool.remove", "Interpreter", roleCall, "(pool, handle) -> element: moves it out"},
	{"PoolCopy", "rt.pool.copy", "Interpreter", roleCall, "(pool) -> pool: every element copied, handles still valid in the copy"},

	{"FrozenFreeze", "rt.frozen.freeze", "Shared", roleCall, "(value) -> frozen: moves the value into a new box with a count of 1; the backend stores a String's word after it"},
	{"FrozenGet", "rt.frozen.get", "Shared", roleCall, "(frozen) -> what it holds, lent: an immutable borrow, valid while the frozen value is; the backend writes it in place, a load that leaves the count alone"},
	{"Share", "rt.share", "Shared", roleCall, "(frozen or cown) -> the same box: another owner, with the count raised by 1 in one locked step"},
	{"CownNew", "rt.cown.new", "Shared", roleCall, "(value) -> cown: moves the value into a new cown with a count of 1; the instruction's Routine, when it has one, ends the value instead of a drop, and the backend passes its code's address, or 0, as a second word"},

	{"BytesNew", "rt.bytes.new", "Bytes", roleCall, "() -> ByteArray"},
	{"BytesLen", "rt.bytes.len", "Bytes", roleCall, "(ByteArray) -> count"},
	{"BytesGet", "rt.bytes.get", "Bytes", roleCall, "(ByteArray, index) -> byte"},
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
	{"OsClockNanos", "rt.os.clockNanos", "Os", roleCall, "() -> Count: the monotonic clock in nanoseconds, from QueryPerformanceCounter"},
	{"OsProcessorCount", "rt.os.processorCount", "Os", roleCall, "() -> Count: how many workers the runtime runs behaviours on, as the scheduler counts them: the processors, or what MAXON_MAX_PROCS asks"},
	{"OsPathFrom", "rt.os.pathFrom", "Os", roleCall, "(path String, error) -> String: the path with the native separator for the other one"},
	{"OsReadText", "rt.os.readText", "Os", roleCall, "(path String, error) -> String"},
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
	{"OsRun", "rt.os.run", "Os", roleCall, "(executable String, arguments Array with String, deadlineMs, stdout, stderr, error) -> exit code: runs the executable with the arguments quoted as the C runtime splits them, and stores what the child wrote to each stream as a String at the address given for it; a child still running deadlineMs milliseconds after it started, when that is not 0, is terminated and the run fails with the timeout case"},
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
	{"Concat", "concat(left String, right String) String"},
	{"Itoa", "itoa(n Count, unsigned Bool) String"},
	{"Ftoa", "ftoa(bits Count) String"},
	{"FtoaExact", "ftoaExact(bits Count) String"},
	{"FmtInt", "formatInteger(n Count, radix Count, flags Count, width Count) String"},
	{"FmtPad", "formatPadded(text String, width Count, zeroFill Bool) String"},
	{"FmtQuoted", "formatQuoted(text String, single Bool, width Count) String"},
	{"FmtFixed", "formatFixed(magnitude String, negative Bool, precision Count) String"},
	{"Streq", "streq(left String, right String) Bool"},
	{"StrLen", "stringLength(text String) Count"},
	{"StrByteAt", "stringByteAt(text String, index Count) Byte"},
	{"StrSlice", "stringSlice(text String, from Count, until Count) String"},
	{"StrCount", "stringCount(text String) Count"},
	{"StrGraphemeIndex", "stringGraphemeIndex(text String, bytePos Count) Count"},
	{"StrGraphemeBytePos", "stringGraphemeBytePos(text String, from Count, count Count) Count"},
	{"StrFind", "stringFind(text String, needle String, from Count) Count"},
	{"StrFindLast", "stringFindLast(text String, needle String) Count"},
	{"StrHasAffix", "stringHasAffix(text String, affix String, atEnd Bool) Bool"},
	{"StrReplace", "stringReplace(text String, old String, replacement String, all Bool) String"},
	{"StrTrim", "stringTrim(text String, trimStart Bool, trimEnd Bool) String"},
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
	{"CownRelease", "cownRelease(record Count)"},
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
	{"HashSeed", "hashSeed() Count"},
	{"HashSeedDraw", "drawHashSeed() Count"},
	{"HashWord", "hashWord(word Count, seed Count) Count"},
	{"HashBytes", "hashBytes(array Count, at Count, count Count, seed Count) Count"},
	{"HashStringBytes", "hashStringBytes(text String, at Count, count Count, seed Count) Count"},
	{"BytesReadWord", "bytesReadWord(array Count, at Count, width Count, bigEndian Bool, errorAddress Count) Count"},
	{"BytesAppendWord", "bytesAppendWord(array Count, word Count, width Count, bigEndian Bool)"},
	{"ServiceNew", "serviceNew(state Count, capacity Count) Count"},
	{"ServiceReserve", "serviceReserve(handle Count) Count"},
	{"ServiceTaken", "serviceTaken(state Count)"},
	{"ServiceShutdown", "serviceShutdown(handle Count)"},
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
	{"ArrayFill", "arrayFill(array Count, count Count, word Count)"},
	{"ArrayReserve", "arrayReserve(array Count, count Count)"},
	{"StrsElem", "stringsElem(array Count, index Count) Count"},
	{"StrsOpen", "stringsOpen(array Count, index Count) Count"},
	{"StrsClose", "stringsClose(array Count, index Count)"},
	{"StrsFill", "stringsFill(array Count, count Count)"},
	{"StrsReserve", "stringsReserve(array Count, count Count)"},
	{"ArrayShrink", "arrayShrink(array Count, length Count)"},
	{"BytesNew", "bytesNew() Count"},
	{"BytesLen", "bytesLength(array Count) Count"},
	{"BytesGet", "bytesGet(array Count, index Count) Byte"},
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
	{"OsClockNanos", "osClockNanos() Count"},
	{"OsProcessorCount", "osProcessorCount() Count"},
	{"OsPathFrom", "osPathFrom(path String, errorAddress Count) String"},
	{"OsReadText", "osReadText(path String, errorAddress Count) String"},
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
}
