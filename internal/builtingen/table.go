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
	{"FullRangeInt", "b.fullRangeIntTy"},
	// Float is the type of a float whose range is not known, which maxon names FullRangeFloat.
	{"Float", "b.anonFloatTy"},
}

// builtinType is a runtime primitive: an ast.BuiltinTy of its own BuiltinKind.
type builtinType struct {
	Key    string // the BuiltinKind is Builtin+Key, the ast.Builtins field Key
	Name   string // the name a program writes
	Params []string
	// Managed is whether a value of it owns a box; Subscriptable whether t[i] names an element; Shared whether it is
	// counted and has many owners.
	Managed, Subscriptable, Shared bool
}

var builtinTypes = []builtinType{
	{Key: "Array", Name: "Array", Params: []string{"T"}, Managed: true, Subscriptable: true},
	{Key: "Map", Name: "Map", Params: []string{"K", "V"}, Managed: true, Subscriptable: true},
	{Key: "List", Name: "List", Params: []string{"T"}, Managed: true, Subscriptable: true},
	{Key: "Pool", Name: "Pool", Params: []string{"T"}, Managed: true, Subscriptable: true},
	{Key: "Handle", Name: "Handle", Params: []string{"T"}},
	{Key: "Frozen", Name: "frozen", Params: []string{"T"}, Managed: true, Shared: true},
	{Key: "Cown", Name: "cown", Params: []string{"T"}, Managed: true, Shared: true},
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
}

var recordTypes = []recordType{
	{Key: "CommandLine", Name: "CommandLine"},
	{Key: "Process", Name: "Process"},
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
	{Key: "Subprocess", Name: "Subprocess"},
	// Raw is maxon's __Raw: the closed set of raw machine and OS operations the runtime's source is written on, which
	// only the runtime module may call (runtimeOnly).
	{Key: "Raw", Name: "__Raw"},
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
	{Key: "MapError", Name: "MapError", Cases: []enumCase{
		{"keyNotFound", "ErrorKeyNotFound"},
		{"keyAlreadyExists", "ErrorKeyAlreadyExists"},
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
	{Key: "DirectoryListError", Name: "DirectoryListError", Cases: []enumCase{
		{"notFound", "ErrorListNotFound"},
		{"accessDenied", "ErrorListAccessDenied"},
		{"listFailed", "ErrorListFailed"},
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
	// Params are "convention name Type", the convention left out when it is let.
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
	// Field is the ast.Builtins field that holds a free function which is not declared by its name.
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
		raw("wrappingAdd", []string{"left Count", "right Count"}, "Count", "RawWrappingAdd"),
		raw("wrappingSub", []string{"left Count", "right Count"}, "Count", "RawWrappingSub"),
		own(raw("addressOf", []string{"mutates word Count"}, "Count", "RawAddressOf")),
		raw("stringAddress", []string{"text String"}, "Count", "RawStringAddress"),
		raw("stringAt", []string{"box Count"}, "String", "RawStringAt"),
		raw("heapAddress", nil, "Count", "RawHeapAddress"),
		raw("stdoutAddress", nil, "Count", "RawStdoutAddress"),
		raw("stderrAddress", nil, "Count", "RawStderrAddress"),
		raw("liveAddress", nil, "Count", "RawLiveAddress"),
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
	return fs
}

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
	// clear and truncate drop the elements they remove; reserve does nothing, since an array's capacity is no part of its value; resize
	// grows with zero elements, which the checker holds to an element that has one.
	{Owner: "Array", Name: "clear", Self: "mutates", Bounds: []string{"T Droppable"}, Intrinsic: "ArrayClear"},
	{Owner: "Array", Name: "truncate", Self: "mutates", Params: []string{"newLength ElementIndex"}, Bounds: []string{"T Droppable"}, Intrinsic: "ArrayTruncate"},
	{Owner: "Array", Name: "reserve", Self: "mutates", Params: []string{"minCapacity ElementIndex"}, Intrinsic: "ArrayReserve"},
	{Owner: "Array", Name: "resize", Self: "mutates", Params: []string{"newLength ElementIndex"}, Bounds: []string{"T Droppable"}, Intrinsic: "ArrayResize", Checked: true},
	{Owner: "Array", Name: "slice", Params: []string{"start ElementIndex", "endIndex ElementIndex"}, Result: "Self", Bounds: []string{"T Copyable"}, Throws: "ArrayError", Intrinsic: "ArraySlice"},
	{Owner: "Array", Name: "growFilled", Self: "mutates", Params: []string{"newLength ElementIndex", "value T"}, Bounds: []string{"T Copyable"}, Intrinsic: "ArrayGrowFilled"},
	{Owner: "Array", Name: "refill", Self: "mutates", Params: []string{"newLength ElementIndex", "value T"}, Bounds: []string{"T Copyable"}, Intrinsic: "ArrayRefill"},
	// contains, containsSequence and equals are maxon's for an element that is Equatable; the checker holds them to the
	// elements this compiler compares itself (checkElementsComparable).
	{Owner: "Array", Name: "contains", Params: []string{"element T"}, Result: "Bool", Intrinsic: "ArrayContains", Checked: true},
	{Owner: "Array", Name: "containsSequence", Params: []string{"sequence Self"}, Result: "Bool", Intrinsic: "ArrayContainsSequence", Checked: true},
	{Owner: "Array", Name: "equals", Params: []string{"other Self"}, Result: "Bool", Intrinsic: "ArrayEquals", Checked: true}, {Owner: "Array", Name: "first", Result: "T", Bounds: []string{"T Copyable"}, Throws: "ArrayError", Intrinsic: "ArrayFirst"},
	{Owner: "Array", Name: "last", Result: "T", Bounds: []string{"T Copyable"}, Throws: "ArrayError", Intrinsic: "ArrayLast"},

	{Owner: "Map", Name: "create", Static: true, Result: "Self", Intrinsic: "MapCreate"},
	{Owner: "Map", Name: "count", Result: "Count", Intrinsic: "MapCount"},
	{Owner: "Map", Name: "isEmpty", Result: "Bool", Intrinsic: "MapIsEmpty"},
	{Owner: "Map", Name: "contains", Params: []string{"key K"}, Result: "Bool", Intrinsic: "MapContains"},
	{Owner: "Map", Name: "insert", Self: "mutates", Params: []string{"consumes key K", "consumes value V"}, Bounds: []string{"V Droppable"}, Throws: "MapError", Intrinsic: "MapInsert"},
	{Owner: "Map", Name: "upsert", Self: "mutates", Params: []string{"consumes key K", "consumes value V"}, Bounds: []string{"V Droppable"}, Intrinsic: "MapUpsert"},
	{Owner: "Map", Name: "replace", Self: "mutates", Params: []string{"consumes key K", "consumes value V"}, Result: "V", Throws: "MapError", Intrinsic: "MapReplace"},
	{Owner: "Map", Name: "remove", Self: "mutates", Params: []string{"key K"}, Result: "Bool", Bounds: []string{"V Droppable"}, Intrinsic: "MapRemove"},
	{Owner: "Map", Name: "dropEmpty", Self: "consumes", Intrinsic: "MapDropEmpty"},

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
	{Owner: "FilePath", Name: "from", Static: true, Params: []string{"path String"}, Result: "Self", Throws: "FilePathError", Intrinsic: "FilePathFrom"},
	{Owner: "FilePath", Name: "init", Static: true, Params: []string{"value String"}, Result: "Self", Intrinsic: "FilePathInit"},
	{Owner: "FilePath", Name: "toString", Result: "String", Intrinsic: "FilePathToString"},
	{Owner: "File", Name: "readText", Static: true, Params: []string{"path FilePath"}, Result: "String", Throws: "FileReadError", Intrinsic: "FileReadText"},
	{Owner: "File", Name: "writeText", Static: true, Params: []string{"path FilePath", "content String"}, Throws: "FileWriteError", Intrinsic: "FileWriteText"},
	{Owner: "File", Name: "writeBinary", Static: true, Params: []string{"path FilePath", "content Array with Byte"}, Throws: "FileWriteError", Intrinsic: "FileWriteBinary"},
	{Owner: "File", Name: "rename", Static: true, Params: []string{"from FilePath", "to FilePath"}, Throws: "FileRenameError", Intrinsic: "FileRename"},
	{Owner: "File", Name: "delete", Static: true, Params: []string{"path FilePath"}, Throws: "FileDeleteError", Intrinsic: "FileDelete"},
	{Owner: "Directory", Name: "isDirectory", Static: true, Params: []string{"path FilePath"}, Result: "Bool", Intrinsic: "DirectoryIsDirectory"},
	// create makes the missing parents too, and gives whether the directory is there after it.
	{Owner: "Directory", Name: "create", Static: true, Params: []string{"path FilePath"}, Result: "Bool", Intrinsic: "DirectoryCreate"},
	// list gives the path of each entry, the directory's path joined to its name.
	{Owner: "Directory", Name: "currentPath", Static: true, Result: "FilePath", Intrinsic: "DirectoryCurrentPath"},
	{Owner: "Directory", Name: "list", Static: true, Params: []string{"path FilePath"}, Result: "Array with FilePath", Throws: "DirectoryListError", Intrinsic: "DirectoryList"},
	{Owner: "Subprocess", Name: "run", Static: true, Params: []string{"executable Executable", "arguments Array with String"}, Result: "CollectedOutput", Throws: "SubprocessError", Intrinsic: "SubprocessRun"},
	// minimaxon's built-ins have no default arguments, so maxon's run with a deadline is a function of its own. A child
	// still running when the deadline passes is terminated, and the run throws timeout.
	{Owner: "Subprocess", Name: "runWithDeadline", Static: true, Params: []string{"executable Executable", "arguments Array with String", "deadline WaitDeadline"}, Result: "CollectedOutput", Throws: "SubprocessError", Intrinsic: "SubprocessRunWithDeadline"},
	{Owner: "CollectedOutput", Name: "exitCode", Result: "ExitCode", Intrinsic: "CollectedOutputExitCode"},
	{Owner: "CollectedOutput", Name: "succeeded", Result: "Bool", Intrinsic: "CollectedOutputSucceeded"},
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
	{"Concat", "rt.concat", "Runtime", roleCall, "(String, String) -> String"},
	{"Itoa", "rt.itoa", "Runtime", roleCall, "(n, unsigned) -> String"},
	{"Ftoa", "rt.ftoa", "Runtime", roleCall, "(bits) -> String: the text of the double of the bits"},
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
	{"Live", "g.live", "Runtime", roleGlobal, "the number of heap boxes allocated and not yet freed, which the runtime always keeps"},
	{"ImageDataStart", "g.imageDataStart", "Linker", roleGlobal, "the start of the data section, where every string literal lives, so the runtime can tell an immortal string from one on the heap"},
	{"ImageDataEnd", "g.imageDataEnd", "Linker", roleGlobal, "the end of the data section"},
	{"CheckLeaks", "rt.checkLeaks", "Source", roleInternal, "(exit code) -> exit code: what the entry of an executable built to count its allocations calls after main, which exits with a leak report when a box is still live"},

	{"ArrayNew", "rt.array.new", "Arrays", roleCall, "() -> Array"},
	{"ArrayLen", "rt.array.len", "Arrays", roleCall, "(array) -> count"},
	{"ArrayElem", "rt.array.elem", "Arrays", roleCall, "(array, index) -> address of the element"},
	{"ArrayInsert", "rt.array.insert", "Arrays", roleCall, "(array, index, element): moves it in at index, which may be the length"},
	{"ArrayRemove", "rt.array.remove", "Arrays", roleCall, "(array, index) -> element: moves it out"},

	{"MapNew", "rt.map.new", "Interpreter", roleCall, "() -> map"},
	{"MapLen", "rt.map.len", "Interpreter", roleCall, "(map) -> count"},
	{"MapHas", "rt.map.has", "Interpreter", roleCall, "(map, key) -> bool"},
	{"MapElem", "rt.map.elem", "Interpreter", roleCall, "(map, key) -> address of the value"},
	{"MapInsert", "rt.map.insert", "Interpreter", roleCall, "(map, key, value): moves both in; the value of an existing key is dropped"},
	{"MapRemove", "rt.map.remove", "Interpreter", roleCall, "(map, key) -> value: moves it out; the key is dropped"},
	{"MapCopy", "rt.map.copy", "Interpreter", roleCall, "(map) -> map: every key and value copied"},

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

	{"FrozenFreeze", "rt.frozen.freeze", "Interpreter", roleCall, "(value) -> frozen: moves the value into a new box with a count of 1"},
	{"FrozenGet", "rt.frozen.get", "Interpreter", roleCall, "(frozen) -> what it holds, lent: an immutable borrow, valid while the frozen value is"},
	{"Share", "rt.share", "Interpreter", roleCall, "(frozen or cown) -> the same box: another owner, with the count raised by 1"},
	{"CownNew", "rt.cown.new", "Interpreter", roleCall, "(value) -> cown: moves the value into a new cown with a count of 1; the instruction's Routine, when it has one, ends the value instead of a drop"},

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
	{"OsPathFrom", "rt.os.pathFrom", "Os", roleCall, "(path String, error) -> String: the path with the native separator for the other one"},
	{"OsReadText", "rt.os.readText", "Os", roleCall, "(path String, error) -> String"},
	{"OsWriteText", "rt.os.writeText", "Os", roleCall, "(path String, content String, error)"},
	{"OsWriteBinary", "rt.os.writeBinary", "Os", roleCall, "(path String, content ByteArray, error)"},
	{"OsRename", "rt.os.rename", "Os", roleCall, "(from String, to String, error): replaces what is at to"},
	{"OsDelete", "rt.os.delete", "Os", roleCall, "(path String, error)"},
	{"OsIsDirectory", "rt.os.isDirectory", "Os", roleCall, "(path String) -> bool"},
	{"OsCreateDirectory", "rt.os.createDirectory", "Os", roleCall, "(path String) -> bool: makes the missing parents too, and is whether the directory is there after it"},
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
	{"Concat", "concat(left String, right String) String"},
	{"Itoa", "itoa(n Count, unsigned Bool) String"},
	{"Ftoa", "ftoa(bits Count) String"},
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
	{"ArrayNew", "arrayNew() Count"},
	{"ArrayLen", "arrayLength(array Count) Count"},
	{"ArrayElem", "arrayElem(array Count, index Count) Count"},
	{"ArrayInsert", "arrayInsert(array Count, index Count, element Count)"},
	{"ArrayRemove", "arrayRemove(array Count, index Count) Count"},
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
	{"OsPathFrom", "osPathFrom(path String, errorAddress Count) String"},
	{"OsReadText", "osReadText(path String, errorAddress Count) String"},
	{"OsWriteText", "osWriteText(path String, content String, errorAddress Count)"},
	{"OsWriteBinary", "osWriteBinary(path String, content Count, errorAddress Count)"},
	{"OsRename", "osRename(source String, destination String, errorAddress Count)"},
	{"OsDelete", "osDelete(path String, errorAddress Count)"},
	{"OsIsDirectory", "osIsDirectory(path String) Bool"},
	{"OsCreateDirectory", "osCreateDirectory(path String) Bool"},
	{"OsList", "osList(path String, errorAddress Count) Count"},
	{"OsRun", "osRun(executable String, arguments Count, deadlineMs Count, stdout Count, stderr Count, errorAddress Count) Count"},
}
