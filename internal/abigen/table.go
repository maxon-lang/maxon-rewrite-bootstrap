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
	// runtime's. It is Go's M, the machine: the fields of the OS thread whatever processor (P) it holds. The main thread's
	// is reserved by the entry of the compiler that built the program, so a field the runtime writes is in the layout
	// from the seed that compiles the runtime on.
	{"contextSchedulerOffset", 0, false, toRuntime,
		"where a thread's context holds the address of the scheduler's record, 0 on the main thread until the first behaviour is scheduled."},
	{"contextQueueOffset", 1, false, toRuntime,
		"where a thread's context holds the address of its worker's queue of runnable behaviours."},
	{"contextParkEventOffset", 2, false, toRuntime,
		"where a thread's context holds the event its worker waits on while it is parked."},
	{"contextHeapOffset", 3, false, toRuntime,
		"where a thread's context holds the address of its heap's record, 0 until the thread's first alloc makes it."},
	{"contextRandomOffset", 4, false, toRuntime,
		"where a thread's context holds the state of its random numbers, which pick the queue it steals from first."},
	{"contextProcOffset", 5, false, toRuntime,
		"where a thread's context holds the address of the processor (Go's P) whose queues it runs, 0 while it has none: its queue is that processor's, kept beside it."},
	{"contextGreenOffset", 6, false, toRuntime,
		"where a thread's context holds the green thread it runs (Go's m.curg), 0 while it runs its scheduler or a behaviour."},
	{"contextSchedSwitchOffset", 7, false, toRuntime,
		"where a thread's context holds the address of the switch record of its scheduler (Go's g0), which a green thread switches back to."},
	{"contextSwitchFromOffset", 8, false, toRuntime | toSelfBack,
		"where a thread's context holds the switch record that rt.stackSwitch saves the running stack's registers into."},
	{"contextSwitchToOffset", 9, false, toRuntime | toSelfBack,
		"where a thread's context holds the switch record that rt.stackSwitch loads the registers of the stack it goes on from: the word after contextSwitchFromOffset, so the pair is the switch's one argument."},
	{"contextIntentOffset", 10, false, toRuntime,
		"where a thread's context holds what the green thread that switched to its scheduler asks of it: to park it, requeue it, or finish it."},
	{"contextSyscallOffset", 11, false, toRuntime,
		"where a thread's context holds the state of its blocking call: bit 0 while it is in one, bit 1 once the monitor has taken its processor, and a generation above them that each call steps, so that one value names one call."},
	{"contextSysmonWordOffset", 12, false, toRuntime,
		"where a thread's context holds the blocking call's state the monitor saw last, which only the monitor reads and writes."},
	{"contextSysmonWhenOffset", 13, false, toRuntime,
		"where a thread's context holds when the monitor first saw that state, in nanoseconds."},
	{"contextSignalPendingOffset", 14, false, toRuntime,
		"where a thread's context holds 1 while a preemption the monitor sent it is not yet answered (Go's m.signalPending)."},
	{"contextThreadOffset", 15, false, toRuntime,
		"where a thread's context holds what the monitor stops the thread by: on Windows a handle that may suspend it and read and set its registers, on Linux its thread id."},
	{"contextPreemptCodeOffset", 16, false, toRuntime | toSelfBack,
		"where a thread's context holds the address of the runtime's code that rt.asyncPreempt calls with the context once it has saved every register, which parks the green thread it stopped."},
	{"contextSignalCodeOffset", 17, false, toRuntime | toSelfBack,
		"where a thread's context holds the address of the runtime's code that the Linux preemption signal's handler calls with the signal's context."},
	{"contextNextProcOffset", 18, false, toRuntime,
		"where a thread's context holds the processor that the thread that woke it hands it (Go's m.nextp), 0 for none."},
	{"contextStoppedOffset", 19, false, toRuntime,
		"where a thread's context holds 1 while it waits with no processor for one, the word a Linux thread waits on with a futex."},
	{"contextIdleNextOffset", 20, false, toRuntime,
		"where a thread's context holds the next of the threads that wait with no processor, a list the scheduler's lock guards."},
	{"contextStartCodeOffset", 21, false, toRuntime,
		"where a thread's context holds the address of the code that the thread the runtime starts with it runs, called with the context: the monitor's loop, or 0 for a worker's."},
	{"contextBytes", 22, false, toRuntime | toSelfBack,
		"the size of a thread's context."},

	// A switch record: the registers of a stack that is not running, which rt.stackSwitch saves when it leaves the stack
	// and loads when it goes back on it. The registers are those the internal convention keeps across a call, which is
	// what the switch is to the code that calls it, and the frame pointer; on Windows also the stack's bounds in the TEB,
	// which the kernel reads to grow a stack and to raise its overflow.
	{"switchSpOffset", 0, false, toRuntime | toSelfBack,
		"where a switch record holds the stack pointer, at the word the stack goes on at: the return address of the switch that left it, or of rt.greenStart on a stack that has not run."},
	{"switchRbxOffset", 1, false, toRuntime | toSelfBack,
		"where a switch record holds rbx, which rt.greenStart passes to the code it calls."},
	{"switchRbpOffset", 2, false, toRuntime | toSelfBack,
		"where a switch record holds rbp."},
	{"switchR12Offset", 3, false, toRuntime | toSelfBack,
		"where a switch record holds r12, the address of the code that rt.greenStart calls; r13, r14 and r15 follow it."},
	{"switchStackBaseOffset", 7, false, toRuntime | toSelfBack,
		"where a switch record holds the top of its stack, the TEB's StackBase on Windows."},
	{"switchStackLimitOffset", 8, false, toRuntime | toSelfBack,
		"where a switch record holds the lowest committed address of its stack, the TEB's StackLimit on Windows."},
	{"switchStackLowOffset", 9, false, toRuntime | toSelfBack,
		"where a switch record holds the lowest address of its stack's reserve, the TEB's DeallocationStack on Windows."},
	{"switchXmmOffset", 10, false, toRuntime | toSelfBack,
		"where a switch record holds xmm6 to xmm15, two words each."},
	{"switchBytes", 30, false, toRuntime | toSelfBack,
		"the size of a switch record."},
	{"switchKeptGprs", 6, true, toSelfBack,
		"how many general registers a switch record holds: those the internal convention keeps and the frame pointer."},
	{"switchKeptXmms", 10, true, toSelfBack,
		"how many xmm registers a switch record holds, those the internal convention keeps."},

	// The words of a Windows thread's TEB that bound the stack it runs on, which the kernel reads to grow the stack a page
	// at a time and to raise a stack overflow, and which a switch to another stack therefore moves with it.
	{"tebStackBaseOffset", 1, false, toSelfBack,
		"where the TEB holds the top of the running stack (NT_TIB.StackBase)."},
	{"tebStackLimitOffset", 2, false, toSelfBack,
		"where the TEB holds the lowest committed address of the running stack (NT_TIB.StackLimit)."},
	{"tebDeallocationStackOffset", 0x28F, false, toSelfBack,
		"where the TEB holds the lowest address of the running stack's reserve (DeallocationStack, 0x1478)."},

	// The Windows CONTEXT that GetThreadContext and SetThreadContext read and write, of which the monitor moves the stack
	// pointer and the instruction pointer of a thread it stops; the record is aligned to 16 bytes.
	{"windowsContextFlagsOffset", 0x30, true, toRuntime,
		"where a CONTEXT holds which of its parts a call reads or writes (ContextFlags, 32 bits)."},
	{"windowsContextRspOffset", 0x98, true, toRuntime,
		"where a CONTEXT holds Rsp."},
	{"windowsContextRipOffset", 0xF8, true, toRuntime,
		"where a CONTEXT holds Rip."},
	{"windowsContextBytes", 0x4D0, true, toRuntime,
		"the size of a CONTEXT."},
	{"windowsContextAlignment", 16, true, toRuntime,
		"the alignment that GetThreadContext asks of a CONTEXT."},
	{"windowsContextControl", 0x100001, true, toRuntime,
		"CONTEXT_AMD64 | CONTEXT_CONTROL: the part of a CONTEXT that holds the segments, the flags, Rsp and Rip."},

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

	// A thread's heap (runtime/heap.maxon): a record of the size classes, each named by its key, a multiple of a word below
	// heapAlignBytes. At a class's key the record holds the top of the class's stack of freed boxes, and past each table's
	// offset, at the key, the end of that stack, its start and the size of the class's boxes. The record is aligned to
	// heapAlignBytes, so a box's header, the record's address plus the key, names both.
	{"heapOutOffset", 53, false, toRuntime,
		"where a heap's record holds how many boxes it carved from chunks plus how many large boxes it holds, less those it freed."},
	{"heapLargeKey", 63, false, toRuntime,
		"the key in the header of a large box, a block of the process heap of its own; its stack is always full, so a free of it takes the slow path."},
	{"heapLimitsOffset", 64, false, toRuntime,
		"where a heap's record holds, at a class's key past it, the end of the class's stack."},
	{"heapBasesOffset", 128, false, toRuntime,
		"where a heap's record holds, at a class's key past it, the start of the class's stack."},
	{"heapBoxBytesOffset", 192, false, toRuntime,
		"where a heap's record holds, at a class's key past it, the size of the class's boxes."},
	{"heapNextOffset", 256, false, toRuntime,
		"where a heap's record holds the address of the next slot to carve from its current chunk."},
	{"heapEndOffset", 257, false, toRuntime,
		"where a heap's record holds the end of its current chunk."},
	{"heapEmptyStackOffset", 258, false, toRuntime,
		"where a heap's record holds the word 0, the stack of every class that has none of its own yet."},
	{"heapRemoteOffset", 264, false, toRuntime,
		"where a heap's record holds the list of the boxes that other threads freed, linked through their first words, on a cache line of its own since other threads write it."},
	{"heapLargeKeptOffset", 272, false, toRuntime,
		"where a heap's record holds how many bytes of the large blocks that its thread freed it keeps for large boxes to come."},
	{"heapLargeVictimOffset", 273, false, toRuntime,
		"where a heap's record holds the offset of the slot of its kept large blocks that it gives back next when it needs room, round the slots."},
	{"heapLargeSlotsOffset", 274, false, toRuntime,
		"where a heap's record holds its kept large blocks, a block's address or 0 a word each, to the end of the record."},
	{"heapRecordBytes", 306, false, toRuntime,
		"the size of a heap's record."},
	{"heapAlignBytes", 512, true, toRuntime,
		"the alignment of a heap's record, above every key."},

	{"stackReserveBytes", 268435456, true, toRuntime | toSelfBack,
		"the stack each thread reserves, the main thread's in the executable's header and a worker's when the runtime starts it, so a behaviour has the same stack on every thread. The compiler's passes recurse once per level of an expression, and a left-leaning chain of operators is as deep as it is long: a sum of 4,000 names takes about 22 MB. A reserve is only address space, committed a page at a time as the stack grows, so it costs a program nothing that it does not use."},

	{"byteBytes", 1, true, toRuntime | toSelfBack,
		"the size of an element of a byte array."},
	{"bytesFirstCapacity", 16, true, toRuntime,
		"the capacity of an empty byte array's first buffer, which it doubles from."},
}
