package main

// This file is the source of truth for minimaxon's built-ins: the types every project has before it declares
// anything, the functions and methods on them, the intrinsic each lowers to, and the runtime entries those intrinsics
// call. `go generate ./internal/ast` writes from it the declarations, the enums and the dispatch in every package.
//
// A type in a signature is written as a program spells an instance, with the table's keys as names: a key of
// primTypes, a type row's Key, the parameters of the owner or of the function, Self for the owner's own instance,
// and 'X with Y' for an instance of a generic type of one parameter.

// primType is a type the table names but other code declares.
type primType struct {
	Key   string
	Go    string // the Go expression of it inside ast.Builtins
	Maxon string // the Maxon expression of it inside self's declaration code
}

var primTypes = []primType{
	{"Bool", "BoolTy", "b.boolTy"},
	{"String", "StringTy", "b.stringTy"},
	{"Void", "VoidTy", "b.voidTy"},
	{"Count", "b.Count", "b.countTy"},
	{"Byte", "b.Byte", "b.byteTy"},
	{"ExitCode", "b.exitCode()", "b.exitCodeTy"},
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
	{Key: "Bytes", Name: "Bytes", Managed: true},
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
	{Key: "Option", Name: "Option", Params: []string{"T"}, Cases: []unionCase{
		{Name: "some", Const: "OptionSome", Payload: []string{"value T"}},
		{Name: "none", Const: "OptionNone"},
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
	// ArrayError and MapError are maxon's stdlib errors of its collections, cases and all; emptySlot is a case no
	// operation here fails with, kept so a match on the error is the one maxon's needs.
	{Key: "ArrayError", Name: "ArrayError", Cases: []enumCase{
		{"indexOutOfBounds", "ErrorIndexOutOfBounds"},
		{"emptySlot", "ErrorEmptySlot"},
	}},
	{Key: "MapError", Name: "MapError", Cases: []enumCase{
		{"keyNotFound", "ErrorKeyNotFound"},
		{"keyAlreadyExists", "ErrorKeyAlreadyExists"},
	}},
}

// capabilities are the interfaces a bound can name and no declaration implements: a type has one by its parts.
var capabilities = []string{"Copyable", "Droppable"}

// markers are the interfaces with no methods that a declaration says it implements: Error, which an error enum does.
var markers = []string{"Error"}

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
}

var functions = []function{
	{Owner: "Option", Name: "isSome", Result: "Bool", Intrinsic: "OptionIsSome"},
	{Owner: "Option", Name: "unwrap", Self: "consumes", Result: "T", Intrinsic: "OptionUnwrap"},

	{Owner: "Array", Name: "create", Static: true, Params: []string{"count Count"}, Result: "Self", Intrinsic: "ArrayCreate", Checked: true},
	{Owner: "Array", Name: "filled", Static: true, Params: []string{"count Count", "consumes value T"}, Result: "Self", Bounds: []string{"T Copyable"}, Intrinsic: "ArrayFilled"},
	{Owner: "Array", Name: "count", Result: "Count", Intrinsic: "ArrayCount"},
	{Owner: "Array", Name: "isEmpty", Result: "Bool", Intrinsic: "ArrayIsEmpty"},
	{Owner: "Array", Name: "push", Self: "mutates", Params: []string{"consumes value T"}, Intrinsic: "ArrayPush"},
	{Owner: "Array", Name: "append", Self: "mutates", Params: []string{"other Self"}, Bounds: []string{"T Copyable"}, Intrinsic: "ArrayAppend"},
	{Owner: "Array", Name: "insert", Self: "mutates", Params: []string{"at Count", "consumes value T"}, Intrinsic: "ArrayInsert"},
	{Owner: "Array", Name: "remove", Self: "mutates", Params: []string{"at Count"}, Result: "T", Throws: "ArrayError", Intrinsic: "ArrayRemove"},
	{Owner: "Array", Name: "pop", Self: "mutates", Result: "T", Throws: "ArrayError", Intrinsic: "ArrayPop"},
	{Owner: "Array", Name: "dropEmpty", Self: "consumes", Intrinsic: "ArrayDropEmpty"},

	{Owner: "Map", Name: "create", Static: true, Result: "Self", Intrinsic: "MapCreate"},
	{Owner: "Map", Name: "count", Result: "Count", Intrinsic: "MapCount"},
	{Owner: "Map", Name: "isEmpty", Result: "Bool", Intrinsic: "MapIsEmpty"},
	{Owner: "Map", Name: "contains", Params: []string{"key K"}, Result: "Bool", Intrinsic: "MapContains"},
	{Owner: "Map", Name: "insert", Self: "mutates", Params: []string{"consumes key K", "consumes value V"}, Bounds: []string{"V Droppable"}, Throws: "MapError", Intrinsic: "MapInsert"},
	{Owner: "Map", Name: "upsert", Self: "mutates", Params: []string{"consumes key K", "consumes value V"}, Bounds: []string{"V Droppable"}, Intrinsic: "MapUpsert"},
	{Owner: "Map", Name: "replace", Self: "mutates", Params: []string{"consumes key K", "consumes value V"}, Result: "Option with V", Intrinsic: "MapReplace"},
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
	{Owner: "List", Name: "next", Params: []string{"handle Handle with T"}, Result: "Option with Handle with T", Intrinsic: "ListNext"},
	{Owner: "List", Name: "prev", Params: []string{"handle Handle with T"}, Result: "Option with Handle with T", Intrinsic: "ListPrev"},
	{Owner: "List", Name: "remove", Self: "mutates", Params: []string{"handle Handle with T"}, Result: "Option with T", Intrinsic: "ListRemove"},
	{Owner: "List", Name: "moveToFront", Self: "mutates", Params: []string{"handle Handle with T"}, Intrinsic: "ListMoveToFront"},
	{Owner: "List", Name: "dropEmpty", Self: "consumes", Intrinsic: "ListDropEmpty"},

	{Owner: "Pool", Name: "create", Static: true, Result: "Self", Intrinsic: "PoolCreate"},
	{Owner: "Pool", Name: "count", Result: "Count", Intrinsic: "PoolCount"},
	{Owner: "Pool", Name: "isEmpty", Result: "Bool", Intrinsic: "PoolIsEmpty"},
	{Owner: "Pool", Name: "contains", Params: []string{"handle Handle with T"}, Result: "Bool", Intrinsic: "PoolContains"},
	{Owner: "Pool", Name: "insert", Self: "mutates", Params: []string{"consumes value T"}, Result: "Handle with T", Intrinsic: "PoolInsert"},
	{Owner: "Pool", Name: "remove", Self: "mutates", Params: []string{"handle Handle with T"}, Result: "Option with T", Intrinsic: "PoolRemove"},
	{Owner: "Pool", Name: "dropEmpty", Self: "consumes", Intrinsic: "PoolDropEmpty"},

	{Owner: "Bytes", Name: "new", Static: true, Result: "Bytes", Intrinsic: "BytesNew"},
	{Owner: "Bytes", Name: "count", Result: "Count", Intrinsic: "BytesCount"},
	{Owner: "Bytes", Name: "isEmpty", Result: "Bool", Intrinsic: "BytesIsEmpty"},
	{Owner: "Bytes", Name: "byteAt", Params: []string{"index Count"}, Result: "Byte", Intrinsic: "BytesByteAt"},
	{Owner: "Bytes", Name: "setByte", Self: "mutates", Params: []string{"index Count", "value Byte"}, Intrinsic: "BytesSetByte"},
	{Owner: "Bytes", Name: "push", Self: "mutates", Params: []string{"value Byte"}, Intrinsic: "BytesPush"},
	{Owner: "Bytes", Name: "append", Self: "mutates", Params: []string{"other Bytes"}, Intrinsic: "BytesAppend"},
	{Owner: "Bytes", Name: "appendString", Self: "mutates", Params: []string{"text String"}, Intrinsic: "BytesAppendString"},
	{Owner: "Bytes", Name: "toString", Result: "String", Intrinsic: "BytesToString"},

	{Owner: "String", Name: "byteLength", Result: "Count", Intrinsic: "StringByteLength"},
	{Owner: "String", Name: "byteAt", Params: []string{"index Count"}, Result: "Byte", Intrinsic: "StringByteAt"},
	// sliceBytes is maxon's name for the byte range, which keeps slice for a range of graphemes.
	{Owner: "String", Name: "sliceBytes", Params: []string{"start Count", "endPos Count"}, Result: "String", Intrinsic: "StringSliceBytes"},

	// freeze takes a value and gives an immutable frozen value, which the last owner drops; share gives another owner
	// of the same frozen value or cown, and is declared over any type because it takes either; thaw gives an owned
	// copy of what a frozen value holds. cown is the constructor of the type of that name, so it is no name of its own:
	// cown(x) calls CownNew, and cown(x, dropWith: d), whose content its destructor d ends, calls CownNewDropWith.
	{Name: "freeze", Generic: "T Droppable", Params: []string{"consumes x T"}, Result: "Frozen with T", Intrinsic: "Freeze", Checked: true},
	{Name: "share", Generic: "T", Params: []string{"x T"}, Result: "T", Intrinsic: "Share", Checked: true},
	{Name: "thaw", Generic: "T Copyable", Params: []string{"x Frozen with T"}, Result: "T", Intrinsic: "Thaw", Checked: true},
	{Name: "cown", Generic: "T Droppable", Params: []string{"consumes x T"}, Result: "Cown with T", Intrinsic: "Cown", Field: "CownNew"},
	{Name: "cown", Generic: "T", Params: []string{"consumes x T"}, Result: "Cown with T", Intrinsic: "Cown", Field: "CownNewDropWith"},
	// exchange moves a value into a place and gives back the one that was there, so the place is never dropped.
	{Name: "exchange", Generic: "T", Params: []string{"mutates place T", "consumes value T"}, Result: "T", Intrinsic: "Exchange", OwnsOperands: true},

	{Name: "cliArgs", Result: "Array with String", Intrinsic: "OsCliArgs"},
	{Name: "getEnv", Params: []string{"name String"}, Result: "Option with String", Intrinsic: "OsGetEnv"},
	{Name: "readFile", Params: []string{"path String"}, Result: "Option with String", Intrinsic: "OsReadFile"},
	{Name: "writeFile", Params: []string{"path String", "text String"}, Result: "Bool", Intrinsic: "OsWriteFile"},
	{Name: "writeBytes", Params: []string{"path String", "data Bytes"}, Result: "Bool", Intrinsic: "OsWriteBytes"},
	{Name: "renameFile", Params: []string{"from String", "dest String"}, Result: "Bool", Intrinsic: "OsRenameFile"},
	{Name: "removeFile", Params: []string{"path String"}, Result: "Bool", Intrinsic: "OsRemoveFile"},
	{Name: "isDir", Params: []string{"path String"}, Result: "Bool", Intrinsic: "OsIsDir"},
	{Name: "mkdir", Params: []string{"path String"}, Result: "Bool", Intrinsic: "OsMkdir"},
	{Name: "listDir", Params: []string{"path String"}, Result: "Option with Array with String", Intrinsic: "OsListDir"},
	{Name: "runProcess", Params: []string{"commandLine String"}, Result: "Option with ExitCode", Intrinsic: "OsRunProcess"},
}

// unit is a code unit of the backend's runtime, or a place an entry lives that has no code of its own.
type unit struct {
	Key string // ir.RtUnit+Key
	// Emitter is the backend type whose emit+Entry methods write the unit's entries, in defineEntries; "" for a unit
	// the backend writes no code for.
	Emitter string
	Doc     string
}

var units = []unit{
	{Key: "Runtime", Emitter: "runtimeUnit", Doc: "the runtime unit every executable links"},
	{Key: "Arrays", Emitter: "arraysUnit", Doc: "the array unit, linked into a program that calls an array entry"},
	{Key: "Bytes", Emitter: "bytesUnit", Doc: "the Bytes unit, linked into a program that calls a Bytes entry"},
	{Key: "Os", Emitter: "osUnit", Doc: "the OS unit, linked into a program that calls an OS entry"},
	{Key: "Linker", Doc: "the linker, which defines the symbol itself"},
	{Key: "Interpreter", Doc: "no unit: only the interpreter runs the entry, and the compiler refuses a call of it"},
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
	{"Alloc", "rt.alloc", "Runtime", roleInternal, "(bytes) -> a zeroed box"},
	{"Free", "rt.free", "Runtime", roleInternal, "(box): 0 is skipped"},
	{"Write", "rt.write", "Runtime", roleCall, "(handle, text String)"},
	{"Panic", "rt.panic", "Runtime", roleCall, "(message String): exits"},
	{"Concat", "rt.concat", "Runtime", roleCall, "(String, String) -> String"},
	{"Itoa", "rt.itoa", "Runtime", roleCall, "(n, unsigned) -> String"},
	{"Streq", "rt.streq", "Runtime", roleCall, "(String, String) -> bool"},
	{"StrLen", "rt.str.len", "Runtime", roleCall, "(String) -> count of bytes"},
	{"StrByteAt", "rt.str.byteAt", "Runtime", roleCall, "(String, index) -> byte"},
	{"StrSlice", "rt.str.slice", "Runtime", roleCall, "(String, from, until) -> String"},
	{"PanicIndex", "rt.panicIndex", "Arrays", roleInternal, "panics with an index out of range"},
	{"Heap", "g.heap", "Runtime", roleGlobal, "the process heap"},
	{"Stdout", "g.stdout", "Runtime", roleGlobal, "the handle of standard output"},
	{"Stderr", "g.stderr", "Runtime", roleGlobal, "the handle of standard error"},
	{"Live", "g.live", "Runtime", roleGlobal, "the number of heap boxes allocated and not yet freed, kept only by an executable built to count them"},
	{"ImageDataStart", "g.imageDataStart", "Linker", roleGlobal, "the start of the data section, where every string literal lives, so the runtime can tell an immortal string from one on the heap"},
	{"ImageDataEnd", "g.imageDataEnd", "Linker", roleGlobal, "the end of the data section"},

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

	{"BytesNew", "rt.bytes.new", "Bytes", roleCall, "() -> Bytes"},
	{"BytesLen", "rt.bytes.len", "Bytes", roleCall, "(Bytes) -> count"},
	{"BytesGet", "rt.bytes.get", "Bytes", roleCall, "(Bytes, index) -> byte"},
	{"BytesSet", "rt.bytes.set", "Bytes", roleCall, "(Bytes, index, byte)"},
	{"BytesPush", "rt.bytes.push", "Bytes", roleCall, "(Bytes, byte)"},
	{"BytesAppend", "rt.bytes.append", "Bytes", roleCall, "(Bytes, other Bytes)"},
	{"BytesAppendStr", "rt.bytes.appendStr", "Bytes", roleCall, "(Bytes, text String)"},
	{"BytesToString", "rt.bytes.toString", "Bytes", roleCall, "(Bytes) -> String"},

	{"OsCliArgs", "rt.os.cliArgs", "Os", roleCall, "() -> Array with String"},
	{"OsGetEnv", "rt.os.getEnv", "Os", roleCall, "(name String) -> String or 0"},
	{"OsReadFile", "rt.os.readFile", "Os", roleCall, "(path String) -> String or 0"},
	{"OsWriteFile", "rt.os.writeFile", "Os", roleCall, "(path String, text String) -> bool"},
	{"OsWriteBytes", "rt.os.writeBytes", "Os", roleCall, "(path String, data Bytes) -> bool"},
	{"OsRenameFile", "rt.os.renameFile", "Os", roleCall, "(from String, dest String) -> bool"},
	{"OsRemoveFile", "rt.os.removeFile", "Os", roleCall, "(path String) -> bool"},
	{"OsIsDir", "rt.os.isDir", "Os", roleCall, "(path String) -> bool"},
	{"OsMkdir", "rt.os.mkdir", "Os", roleCall, "(path String) -> bool"},
	{"OsListDir", "rt.os.listDir", "Os", roleCall, "(path String) -> Array with String or 0"},
	{"OsRunProcess", "rt.os.runProcess", "Os", roleCall, "(commandLine String) -> (exitCode<<1)|1 or 0"},
}
