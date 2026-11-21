package main

// The ABI layouts table: what a box of each kind holds and at which offset, which the compiler's code (self/ir and
// self/backend) and the runtime (runtime/) must agree on. Each row says which of them get the constant, so none keeps a
// copy that can drift.

// dest is a place a constant is written to.
type dest uint8

const (
	toRuntime  dest = 1 << iota // runtime/layout_gen.maxon
	toSelfIR                    // self/ir/layout_gen.maxon
	toSelfBack                  // self/backend/layout_gen.maxon
)

// layoutConst is one named number of the ABI.
type layoutConst struct {
	name string
	// words is the value in words, or the value itself when plain is set.
	value int
	// plain is a number that is not a multiple of the word size: the word size itself, a count of elements.
	plain bool
	to    dest
	doc   string
}

// wordBytesName is the row that every other row's words are counted in.
const wordBytesName = "wordBytes"

// wordSize is the size of a word in bytes.
const wordSize = 8

var layouts = []layoutConst{
	{wordBytesName, wordSize, true, toRuntime | toSelfIR | toSelfBack,
		"the size of a word: an address, a length, an integer, a handle, and each field of a box."},

	{"noBox", 0, true, toRuntime,
		"the empty place: a box address that names nothing, which a move leaves behind and a free skips."},

	{"tagOffset", 0, false, toSelfIR,
		"where a union box holds its case index; the payload follows it."},

	{"stringLengthOffset", 0, false, toRuntime | toSelfBack,
		"where a String's box holds its length in bytes, a word."},
	{"stringBytesOffset", 1, false, toRuntime | toSelfBack,
		"where a String's bytes begin, UTF-8 with no terminator."},

	// A byte array's box is an array's box, so its routines read the same offsets; only its buffer differs.
	{"arrayLengthOffset", 0, false, toRuntime | toSelfBack,
		"where the box of an array, and of a byte array, holds its length, a count of elements."},
	{"arrayCapacityOffset", 1, false, toRuntime | toSelfBack,
		"where the box of an array, and of a byte array, holds its capacity, a count of elements."},
	{"arrayDataOffset", 2, false, toRuntime | toSelfBack,
		"where the box of an array, and of a byte array, holds the address of its buffer, which has a word of 8 bytes to an element of an array and a byte to an element of a byte array."},
	{"arrayHeaderBytes", 3, false, toRuntime | toSelfBack,
		"the size of the box of an array and of a byte array."},
	{"arrayFirstCapacity", 4, true, toRuntime,
		"the capacity of an empty array's first buffer, which it doubles from."},

	// A closure is a counted box: a function value is one word, the address of it, and copying the value shares the box.
	{"closureCountOffset", 0, false, toSelfBack,
		"where the box of a closure holds how many function values share it, which it is freed with when that reaches 0."},
	{"closureCodeOffset", 1, false, toSelfBack,
		"where the box of a closure holds the address of its code, which a call through the value calls with the box first."},
	{"closureDropOffset", 2, false, toSelfBack,
		"where the box of a closure holds the address of the routine that drops what it owns, or 0 when it owns nothing."},
	{"closureCapturesOffset", 3, false, toSelfIR | toSelfBack,
		"where the box of a closure holds what it captured, a word each, in the order of its body's captures."},

	// An interface value is the box of a value of a type that implements the interface, and the address of the table of
	// that type's code for the interface: a word each, the box owned like a record's.
	{"ifaceTableOffset", 0, false, toSelfIR | toSelfBack,
		"where the box of an interface value holds the address of its table."},
	{"ifaceValueOffset", 1, false, toSelfIR | toSelfBack,
		"where the box of an interface value holds the value, which it owns unless it lends it to a call."},
	{"ifaceBoxBytes", 2, false, toSelfIR | toSelfBack,
		"the size of the box of an interface value."},
	// A table is code: an entry is a jump to the routine or the method, padded to a word, so calling an entry is calling
	// what it jumps to, with the arguments as they are.
	{"tableEntryBytes", 1, false, toSelfBack,
		"the size of an entry of the table of an interface value."},
	{"tableDropEntry", 0, true, toSelfBack,
		"the entry of a table that drops the value the box holds."},
	{"tableCopyEntry", 1, true, toSelfBack,
		"the entry of a table that copies the value the box holds, for an interface whose values are Copyable."},
	{"tableTypeEntry", 2, true, toSelfBack,
		"the entry of a table that gives the address that names the type of the value the box holds, which a cast to a type compares."},
	{"tableFirstMethod", 3, true, toSelfBack,
		"the entry of the first method of a table, the methods following in the order the interface requires them."},

	// A frozen value is a counted box that holds the value, read in place by every owner and borrow.
	{"frozenCountOffset", 0, false, toRuntime | toSelfBack,
		"where the box of a frozen value holds how many owners share it, stepped atomically; the value is dropped and the box freed when it reaches 0."},
	{"frozenValueOffset", 1, false, toRuntime | toSelfBack,
		"where the box of a frozen value holds the value: a scalar, or the address of its box."},
	{"frozenBoxBytes", 2, false, toRuntime | toSelfBack,
		"the size of the box of a frozen value."},

	// A cown has no lock word: the behaviours waiting on it are a chain of their request slots, the newest at tail.
	{"cownCountOffset", 0, false, toRuntime | toSelfBack,
		"where a cown holds how many owners share it, stepped atomically."},
	{"cownTailOffset", 1, false, toRuntime | toSelfBack,
		"where a cown holds the address of the newest request slot queued on it, or 0 when no behaviour holds or waits for it."},
	{"cownEndingOffset", 2, false, toRuntime | toSelfBack,
		"where a cown holds the address of the code that ends its value in place of a drop (`dropWith:`), or 0."},
	{"cownValueOffset", 3, false, toRuntime | toSelfBack,
		"where a cown holds its value, a word: a behaviour that mutates it is given this field's address."},
	{"cownBytes", 4, false, toRuntime | toSelfBack,
		"the size of a cown."},

	// A behaviour's record: its header, its request slots in the order of their cowns' addresses, its cowns in the order
	// the source names them, and its captures.
	{"behaviourPendingOffset", 0, false, toRuntime | toSelfBack,
		"where a behaviour's record holds how many cowns it still waits for, plus one while it is being queued, stepped atomically: it runs when that reaches 0."},
	{"behaviourRunOffset", 1, false, toRuntime | toSelfBack,
		"where a behaviour's record holds the address of its thunk, which is called with the record."},
	{"behaviourCownCountOffset", 2, false, toRuntime | toSelfBack,
		"where a behaviour's record holds how many cowns it names."},
	{"behaviourSlotsOffset", 3, false, toRuntime | toSelfBack,
		"where a behaviour's request slots begin, one for each cown; its cowns follow them, and its captures, a word each, follow those."},
	{"requestCownOffset", 0, false, toRuntime | toSelfBack,
		"where a request slot holds the cown it asks for."},
	{"requestNextOffset", 1, false, toRuntime | toSelfBack,
		"where a request slot holds the address of the slot queued on the same cown after it, or 0."},
	{"requestBlockedOffset", 2, false, toRuntime | toSelfBack,
		"where a request slot holds 1 while its behaviour is still being queued on its cowns, which a later slot waits out before it links itself."},
	{"requestOwnerOffset", 3, false, toRuntime | toSelfBack,
		"where a request slot holds the address of the record of its behaviour."},
	{"requestSlotBytes", 4, false, toRuntime | toSelfBack,
		"the size of a request slot."},

	// A thread's context is what the thread-local slot holds: the main thread's is a global of the entry, a worker's the
	// runtime's. It stays five words while the seed's entry reserves that many for the main thread's.
	{"contextSchedulerOffset", 0, false, toRuntime,
		"where a thread's context holds the address of the scheduler's record, 0 on the main thread until the first behaviour is scheduled."},
	{"contextQueueOffset", 1, false, toRuntime,
		"where a thread's context holds the address of its worker's queue of runnable behaviours."},
	{"contextParkEventOffset", 2, false, toRuntime,
		"where a thread's context holds the event its worker waits on while it is parked."},
	{"contextLiveOffset", 3, false, toRuntime,
		"where a thread's context holds how many boxes the thread allocated less how many it freed, a wrapping word that the leak check sums over the threads."},
	{"contextRandomOffset", 4, false, toRuntime,
		"where a thread's context holds the state of its random numbers, which pick the queue it steals from first."},
	{"contextBytes", 5, false, toRuntime | toSelfBack,
		"the size of a thread's context."},

	// The scheduler's record, made when the first behaviour is scheduled or an exit code set: the counts every worker
	// reads, the exit code, then the address of each worker's context, the main thread's first.
	{"schedWorkersOffset", 0, false, toRuntime,
		"where the scheduler's record holds how many workers there are, the main thread among them."},
	{"schedLiveOffset", 1, false, toRuntime,
		"where the scheduler's record holds how many behaviours have been scheduled and not yet finished, stepped atomically: the program ends when that is 0 after main."},
	{"schedSleepersOffset", 2, false, toRuntime,
		"where the scheduler's record holds how many workers are parked or about to park, stepped atomically, which a push reads to know whether to wake one."},
	{"schedStartedOffset", 3, false, toRuntime,
		"where the scheduler's record holds 1 once the workers' threads are started, which the first behaviour does."},
	{"schedExitSetOffset", 4, false, toRuntime,
		"where the scheduler's record holds 1 once Process.setExitCode has been called."},
	{"schedExitCodeOffset", 5, false, toRuntime,
		"where the scheduler's record holds the code that Process.setExitCode set last, which the process ends with in place of main's."},
	{"schedContextsOffset", 6, false, toRuntime,
		"where the scheduler's record holds the address of each worker's context, a word each, by worker index."},

	// A worker's queue of runnable behaviours: a Chase-Lev deque, whose ring is a box of its own that grows.
	{"queueTopOffset", 0, false, toRuntime,
		"where a queue holds the index of its oldest behaviour, which a thief takes by advancing it with a compare-exchange."},
	{"queueBottomOffset", 1, false, toRuntime,
		"where a queue holds the index past its newest behaviour, which only its owner writes."},
	{"queueRingOffset", 2, false, toRuntime,
		"where a queue holds the address of its ring, which the owner replaces with a larger one when it is full."},
	{"queueSleepingOffset", 3, false, toRuntime,
		"where a queue holds 1 while its owner is parked or about to park, which a waker claims by putting 0 there with a compare-exchange."},
	{"queueBytes", 4, false, toRuntime,
		"the size of a queue."},
	{"ringCapacityOffset", 0, false, toRuntime,
		"where a ring holds how many behaviours it has room for, a power of two."},
	{"ringSlotsOffset", 1, false, toRuntime,
		"where a ring's slots begin, a behaviour's record address each; index i of the queue is slot i modulo the capacity."},

	{"stackReserveBytes", 268435456, true, toRuntime | toSelfBack,
		"the stack each thread reserves, the main thread's in the executable's header and a worker's when the runtime starts it, so a behaviour has the same stack on every thread. The compiler's passes recurse once per level of an expression, and a left-leaning chain of operators is as deep as it is long: a sum of 4,000 names takes about 22 MB. A reserve is only address space, committed a page at a time as the stack grows, so it costs a program nothing that it does not use."},

	{"byteBytes", 1, true, toRuntime | toSelfBack,
		"the size of an element of a byte array."},
	{"bytesFirstCapacity", 16, true, toRuntime,
		"the capacity of an empty byte array's first buffer, which it doubles from."},
}
