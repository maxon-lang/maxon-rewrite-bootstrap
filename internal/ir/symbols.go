package ir

import "minimaxon/internal/diag"

// Rt names a symbol the runtime defines: an entry that code calls or a global it reads. The set is closed, so an
// instruction holds one in a byte and the backend keys a symbol by it without making a string.
type Rt uint8

const (
	RtEntry Rt = iota + 1
	RtAlloc
	RtFree
	RtWrite
	RtPanic
	RtConcat
	RtItoa
	RtStreq
	RtPanicIndex
	RtHeap
	RtStdout
	RtStderr
	// RtLive is the number of heap boxes that are allocated and not yet freed, kept only by an executable built to count
	// them.
	RtLive
	// RtImageDataStart and RtImageDataEnd are the bounds of the executable's data section, where every string literal
	// lives. The linker defines them, so the runtime can tell a string that is immortal from one on the heap.
	RtImageDataStart
	RtImageDataEnd
)

const RtDll = "kernel32.dll"

var RtImports = []string{"GetProcessHeap", "HeapAlloc", "GetStdHandle", "WriteFile", "ExitProcess", "HeapFree"}

// The entries of the runtime primitives (README, "The IR"). Each is a CallRuntimeInstr. A primitive owns its elements:
// an entry that takes one moves it in, an entry that returns one moves it out, and an entry that returns an address
// lends it, valid until the next call that changes the primitive. An entry that is given an index, a key or a handle
// that names nothing traps. A handle is a word and is never 0, so an entry that returns one returns 0 for none.
const (
	RtArrayNew    = RtImageDataEnd + 1 + iota // () -> array
	RtArrayLen                                // (array) -> count
	RtArrayElem                               // (array, index) -> address of the element
	RtArrayInsert                             // (array, index, element): moves it in at index, which may be the length
	RtArrayRemove                             // (array, index) -> element: moves it out

	RtMapNew    // () -> map
	RtMapLen    // (map) -> count
	RtMapHas    // (map, key) -> bool
	RtMapElem   // (map, key) -> address of the value
	RtMapInsert // (map, key, value): moves both in; the value of an existing key is dropped
	RtMapRemove // (map, key) -> value: moves it out; the key is dropped
	RtMapCopy   // (map) -> map: every key and value copied

	RtListNew         // () -> list
	RtListLen         // (list) -> count
	RtListContains    // (list, handle) -> bool
	RtListElem        // (list, handle) -> address of the element
	RtListPushFront   // (list, element) -> handle: moves it in
	RtListPushBack    // (list, element) -> handle: moves it in
	RtListPopFront    // (list) -> element: moves it out; traps if empty
	RtListPopBack     // (list) -> element: moves it out; traps if empty
	RtListFirst       // (list) -> handle, or 0 if empty
	RtListLast        // (list) -> handle, or 0 if empty
	RtListNext        // (list, handle) -> handle, or 0 after the last
	RtListPrev        // (list, handle) -> handle, or 0 before the first
	RtListRemove      // (list, handle) -> element: moves it out
	RtListMoveToFront // (list, handle)
	RtListCopy        // (list) -> list: every element copied, handles still valid in the copy

	RtPoolNew      // () -> pool
	RtPoolLen      // (pool) -> count
	RtPoolContains // (pool, handle) -> bool
	RtPoolElem     // (pool, handle) -> address of the element
	RtPoolInsert   // (pool, element) -> handle: moves it in
	RtPoolRemove   // (pool, handle) -> element: moves it out
	RtPoolCopy     // (pool) -> pool: every element copied, handles still valid in the copy
)

// The entries of the values that cross between threads (README, "Behaviours"). A frozen value and a cown are boxes that
// have a count of owners, not one owner: an entry that makes another owner raises the count, and dropping an owner
// lowers it, and the box and everything it holds is freed when the count reaches 0.
const (
	RtFrozenFreeze = RtPoolCopy + 1 + iota // (value) -> frozen: moves the value into a new box with a count of 1
	RtFrozenGet                            // (frozen) -> what it holds, lent: an immutable borrow, valid while the frozen value is
	RtShare                                // (frozen or cown) -> the same box: another owner, with the count raised by 1
	RtCownNew                              // (value) -> cown: moves the value into a new cown with a count of 1; the instruction's Routine, when it has one, ends the value instead of a drop
)

// IsNativeEntry is whether the backend implements a primitive's runtime entry. Only the array has native code; the
// others run under the interpreter, so a program that reaches one is refused before code generation.
func IsNativeEntry(symbol Rt) bool { return symbol >= RtArrayNew && symbol <= RtArrayRemove }

var rtNames = [...]string{
	RtEntry:           "rt.entry",
	RtAlloc:           "rt.alloc",
	RtFree:            "rt.free",
	RtWrite:           "rt.write",
	RtPanic:           "rt.panic",
	RtConcat:          "rt.concat",
	RtItoa:            "rt.itoa",
	RtStreq:           "rt.streq",
	RtPanicIndex:      "rt.panicIndex",
	RtHeap:            "g.heap",
	RtStdout:          "g.stdout",
	RtStderr:          "g.stderr",
	RtLive:            "g.live",
	RtImageDataStart:  "g.imageDataStart",
	RtImageDataEnd:    "g.imageDataEnd",
	RtArrayNew:        "rt.array.new",
	RtArrayLen:        "rt.array.len",
	RtArrayElem:       "rt.array.elem",
	RtArrayInsert:     "rt.array.insert",
	RtArrayRemove:     "rt.array.remove",
	RtMapNew:          "rt.map.new",
	RtMapLen:          "rt.map.len",
	RtMapHas:          "rt.map.has",
	RtMapElem:         "rt.map.elem",
	RtMapInsert:       "rt.map.insert",
	RtMapRemove:       "rt.map.remove",
	RtMapCopy:         "rt.map.copy",
	RtListNew:         "rt.list.new",
	RtListLen:         "rt.list.len",
	RtListContains:    "rt.list.contains",
	RtListElem:        "rt.list.elem",
	RtListPushFront:   "rt.list.pushFront",
	RtListPushBack:    "rt.list.pushBack",
	RtListPopFront:    "rt.list.popFront",
	RtListPopBack:     "rt.list.popBack",
	RtListFirst:       "rt.list.first",
	RtListLast:        "rt.list.last",
	RtListNext:        "rt.list.next",
	RtListPrev:        "rt.list.prev",
	RtListRemove:      "rt.list.remove",
	RtListMoveToFront: "rt.list.moveToFront",
	RtListCopy:        "rt.list.copy",
	RtPoolNew:         "rt.pool.new",
	RtPoolLen:         "rt.pool.len",
	RtPoolContains:    "rt.pool.contains",
	RtPoolElem:        "rt.pool.elem",
	RtPoolInsert:      "rt.pool.insert",
	RtPoolRemove:      "rt.pool.remove",
	RtPoolCopy:        "rt.pool.copy",
	RtFrozenFreeze:    "rt.frozen.freeze",
	RtFrozenGet:       "rt.frozen.get",
	RtShare:           "rt.share",
	RtCownNew:         "rt.cown.new",
}

func (r Rt) String() string {
	if int(r) >= len(rtNames) || rtNames[r] == "" {
		panic(diag.Internal("%d is not a runtime symbol", int(r)))
	}
	return rtNames[r]
}
