package ir

import "strings"

const (
	RtEntry         = "rt.entry"
	RtAlloc         = "rt.alloc"
	RtFree          = "rt.free"
	RtWrite         = "rt.write"
	RtPanic         = "rt.panic"
	RtConcat        = "rt.concat"
	RtItoa          = "rt.itoa"
	RtStreq         = "rt.streq"
	RtPanicRange    = "rt.panicRange"
	RtPanicOverflow = "rt.panicOverflow"
	RtPanicDiv      = "rt.panicDiv"
	RtPanicShift    = "rt.panicShift"
	RtPanicIndex    = "rt.panicIndex"
	RtHeap          = "g.heap"
	RtStdout        = "g.stdout"
	RtStderr        = "g.stderr"
	// RtLive is the number of heap boxes that are allocated and not yet freed, kept only by an executable built to count
	// them.
	RtLive = "g.live"
	// RtImageDataStart and RtImageDataEnd are the bounds of the executable's data section, where every string literal
	// lives. The linker defines them, so the runtime can tell a string that is immortal from one on the heap.
	RtImageDataStart = "g.imageDataStart"
	RtImageDataEnd   = "g.imageDataEnd"
	RtDll            = "kernel32.dll"
)

var RtImports = []string{"GetProcessHeap", "HeapAlloc", "GetStdHandle", "WriteFile", "ExitProcess", "HeapFree"}

// The entries of the runtime primitives (README, "The IR"). Each is a CallRuntimeInstr. A primitive owns its elements:
// an entry that takes one moves it in, an entry that returns one moves it out, and an entry that returns an address
// lends it, valid until the next call that changes the primitive. An entry that is given an index, a key or a handle
// that names nothing traps. A handle is a word and is never 0, so an entry that returns one returns 0 for none.
const (
	RtArrayNew    = "rt.array.new"    // () -> array
	RtArrayLen    = "rt.array.len"    // (array) -> count
	RtArrayElem   = "rt.array.elem"   // (array, index) -> address of the element
	RtArrayInsert = "rt.array.insert" // (array, index, element): moves it in at index, which may be the length
	RtArrayRemove = "rt.array.remove" // (array, index) -> element: moves it out

	RtMapNew    = "rt.map.new"    // () -> map
	RtMapLen    = "rt.map.len"    // (map) -> count
	RtMapHas    = "rt.map.has"    // (map, key) -> bool
	RtMapElem   = "rt.map.elem"   // (map, key) -> address of the value
	RtMapInsert = "rt.map.insert" // (map, key, value): moves both in; the value of an existing key is dropped
	RtMapRemove = "rt.map.remove" // (map, key) -> value: moves it out; the key is dropped
	RtMapCopy   = "rt.map.copy"   // (map) -> map: every key and value copied

	RtListNew         = "rt.list.new"         // () -> list
	RtListLen         = "rt.list.len"         // (list) -> count
	RtListContains    = "rt.list.contains"    // (list, handle) -> bool
	RtListElem        = "rt.list.elem"        // (list, handle) -> address of the element
	RtListPushFront   = "rt.list.pushFront"   // (list, element) -> handle: moves it in
	RtListPushBack    = "rt.list.pushBack"    // (list, element) -> handle: moves it in
	RtListPopFront    = "rt.list.popFront"    // (list) -> element: moves it out; traps if empty
	RtListPopBack     = "rt.list.popBack"     // (list) -> element: moves it out; traps if empty
	RtListFirst       = "rt.list.first"       // (list) -> handle, or 0 if empty
	RtListLast        = "rt.list.last"        // (list) -> handle, or 0 if empty
	RtListNext        = "rt.list.next"        // (list, handle) -> handle, or 0 after the last
	RtListPrev        = "rt.list.prev"        // (list, handle) -> handle, or 0 before the first
	RtListRemove      = "rt.list.remove"      // (list, handle) -> element: moves it out
	RtListMoveToFront = "rt.list.moveToFront" // (list, handle)
	RtListCopy        = "rt.list.copy"        // (list) -> list: every element copied, handles still valid in the copy

	RtPoolNew      = "rt.pool.new"      // () -> pool
	RtPoolLen      = "rt.pool.len"      // (pool) -> count
	RtPoolContains = "rt.pool.contains" // (pool, handle) -> bool
	RtPoolElem     = "rt.pool.elem"     // (pool, handle) -> address of the element
	RtPoolInsert   = "rt.pool.insert"   // (pool, element) -> handle: moves it in
	RtPoolRemove   = "rt.pool.remove"   // (pool, handle) -> element: moves it out
	RtPoolCopy     = "rt.pool.copy"     // (pool) -> pool: every element copied, handles still valid in the copy
)

// The entries of the values that cross between threads (README, "Behaviours"). A frozen value and a cown are boxes that
// have a count of owners, not one owner: an entry that makes another owner raises the count, and dropping an owner
// lowers it, and the box and everything it holds is freed when the count reaches 0.
const (
	RtFrozenFreeze = "rt.frozen.freeze" // (value) -> frozen: moves the value into a new box with a count of 1
	RtFrozenGet    = "rt.frozen.get"    // (frozen) -> what it holds, lent: an immutable borrow, valid while the frozen value is
	RtShare        = "rt.share"         // (frozen or cown) -> the same box: another owner, with the count raised by 1
	RtCownNew      = "rt.cown.new"      // (value) -> cown: moves the value into a new cown with a count of 1; the instruction's Routine, when it has one, ends the value instead of a drop
)

// IsNativeEntry is whether the backend implements a primitive's runtime entry. Only the array has native code; the
// others run under the interpreter, so a program that reaches one is refused before code generation.
func IsNativeEntry(symbol string) bool { return strings.HasPrefix(symbol, "rt.array.") }
