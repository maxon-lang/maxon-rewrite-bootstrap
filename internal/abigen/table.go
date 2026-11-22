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
	// A service is a cown with its mailbox's gate after the cown's words, so a behaviour, a share and a drop of the handle
	// are a cown's.
	{"serviceGateOffset", 4, false, toRuntime,
		"where a service holds its mailbox's gate, stepped atomically: how many messages are queued and not yet started, plus serviceClosed once it is shut down."},
	{"serviceCapacityOffset", 5, false, toRuntime,
		"where a service holds how many messages its mailbox holds at most."},
	{"serviceBytes", 6, false, toRuntime,
		"the size of a service."},

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
	{"contextSchedulerOffset", 0, false, toRuntime | toSelfBack,
		"where a thread's context holds the address of the scheduler's record, 0 on the main thread until the first behaviour or green thread is made."},
	{"contextAllNextOffset", 1, false, toRuntime,
		"where a thread's context holds the next of every thread that runs green threads (Go's allm), a list that only grows."},
	{"contextParkEventOffset", 2, false, toRuntime,
		"where a thread's context holds the event it waits on while it has no processor, on Windows."},
	{"contextHeapOffset", 3, false, toRuntime,
		"where a thread's context holds the address of its heap's record, 0 until the thread's first alloc makes it."},
	{"contextRandomOffset", 4, false, toRuntime,
		"where a thread's context holds the state of its random numbers, which pick the processor it steals from first."},
	{"contextProcOffset", 5, false, toRuntime,
		"where a thread's context holds the address of the processor (Go's P) whose queues it runs, 0 while it has none; in a blocking call the processor it left (Go's m.oldp)."},
	{"contextGreenOffset", 6, false, toRuntime,
		"where a thread's context holds the green thread it runs (Go's m.curg), 0 while it runs its scheduler."},
	{"contextSchedSwitchOffset", 7, false, toRuntime,
		"where a thread's context holds the address of its block (threadBlock rows), which starts with the switch record of its scheduler (Go's g0) that a green thread switches back to."},
	{"contextSwitchFromOffset", 8, false, toRuntime | toSelfBack,
		"where a thread's context holds the switch record that rt.stackSwitch saves the running stack's registers into."},
	{"contextSwitchToOffset", 9, false, toRuntime | toSelfBack,
		"where a thread's context holds the switch record that rt.stackSwitch loads the registers of the stack it goes on from: the word after contextSwitchFromOffset, so the pair is the switch's one argument."},
	{"contextIntentOffset", 10, false, toRuntime,
		"where a thread's context holds what the green thread that switched to its scheduler asks of it: to park it, requeue it, or finish it."},
	{"contextSyscallOffset", 11, false, toRuntime,
		"where a thread's context holds 1 while its green thread is in a blocking call (Go's entersyscall to exitsyscall), which the monitor never stops."},
	{"contextSpinningOffset", 12, false, toRuntime,
		"where a thread's context holds 1 while it looks for work with no green thread to run (Go's m.spinning)."},
	{"contextDrainsOffset", 13, false, toRuntime,
		"where a thread's context holds 1 on the main thread while it runs the scheduler after main, whose search for work ends once no behaviour or green thread is left."},
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
	{"contextHashOffset", 22, false, toRuntime,
		"where the main thread's context holds the address of the process's hash state (hashStateBytes), 0 until the first hash that needs the seed makes it; every other thread reaches it through the scheduler's main thread."},
	{"contextBytes", 23, false, toRuntime | toSelfBack,
		"the size of a thread's context."},

	// The process's hash state (runtime/hash.maxon): the secret every seeded hash is keyed by, drawn from the system or
	// pinned by MAXON_HASH_SEED, the fold of no bytes under it that a Hasher starts from, whether the secret is pinned,
	// and how many seeds a pinned process has drawn.
	{"hashSecretOffset", 0, false, toRuntime,
		"where the hash state holds the process's secret."},
	{"hashSeedOffset", 1, false, toRuntime,
		"where the hash state holds the fold of no bytes under the secret, which __Builtins.hashSeed gives."},
	{"hashPinnedOffset", 2, false, toRuntime,
		"where the hash state holds 1 when MAXON_HASH_SEED pinned the secret."},
	{"hashDrawsOffset", 3, false, toRuntime,
		"where the hash state holds how many seeds __Builtins.drawHashSeed has drawn in a pinned process, stepped atomically."},
	{"hashStateBytes", 4, false, toRuntime,
		"the size of the hash state."},

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

	// The scheduler's record (Go's sched), made when the first behaviour or green thread is made or an exit code set: the
	// counts every thread reads, the global run queue, the idle processors and threads, the free green threads and stacks,
	// the timers, the poller, then the address of each processor, the main thread's first. The words that several fields
	// change together are under its lock.
	{"schedProcsOffset", 0, false, toRuntime,
		"where the scheduler's record holds how many processors there are (Go's gomaxprocs): the processors of the machine, or what MAXON_MAX_PROCS asks."},
	{"schedLiveOffset", 1, false, toRuntime,
		"where the scheduler's record holds how many behaviours and green threads have been made and not yet finished, stepped atomically: the program ends when that is 0 after main."},
	{"schedLockOffset", 2, false, toRuntime,
		"where the scheduler's record holds its lock (Go's sched.lock): 1 while a thread holds it."},
	{"schedStartedOffset", 3, false, toRuntime,
		"where the scheduler's record holds 1 once the monitor's thread is started, which the first behaviour or green thread does."},
	{"schedExitSetOffset", 4, false, toRuntime,
		"where the scheduler's record holds 1 once Process.setExitCode has been called."},
	{"schedExitCodeOffset", 5, false, toRuntime,
		"where the scheduler's record holds the code that Process.setExitCode set last, which the process ends with in place of main's."},
	{"schedRunqHeadOffset", 6, false, toRuntime,
		"where the scheduler's record holds the oldest green thread of the global run queue, linked through greenLinkOffset, under the lock."},
	{"schedRunqTailOffset", 7, false, toRuntime,
		"where the scheduler's record holds the newest green thread of the global run queue, under the lock."},
	{"schedRunqSizeOffset", 8, false, toRuntime,
		"where the scheduler's record holds how many green threads the global run queue holds, written under the lock and read without it to choose whether to look."},
	{"schedIdleProcsOffset", 9, false, toRuntime,
		"where the scheduler's record holds the processors no thread holds (Go's pidle), linked through procLinkOffset, under the lock."},
	{"schedIdleProcCountOffset", 10, false, toRuntime,
		"where the scheduler's record holds how many processors are idle (Go's npidle), written under the lock and read without it."},
	{"schedIdleThreadsOffset", 11, false, toRuntime,
		"where the scheduler's record holds the threads that wait with no processor (Go's midle), linked through contextIdleNextOffset, under the lock."},
	{"schedSpinningOffset", 12, false, toRuntime,
		"where the scheduler's record holds how many threads look for work to steal (Go's nmspinning), stepped atomically."},
	{"schedAllThreadsOffset", 13, false, toRuntime,
		"where the scheduler's record holds the newest thread that runs green threads (Go's allm), linked through contextAllNextOffset."},
	{"schedFreeGreensOffset", 14, false, toRuntime,
		"where the scheduler's record holds the green threads' records that are free beyond what the processors keep, linked through greenLinkOffset, under the lock."},
	{"schedFreeStacksOffset", 15, false, toRuntime,
		"where the scheduler's record holds the stacks that are free beyond what the processors keep, linked through pooledStackNextOffset, under the lock."},
	{"schedTimersOffset", 16, false, toRuntime,
		"where the scheduler's record holds the address of the timers' heap, the sleeping green threads by when they wake, under the lock."},
	{"schedTimerCountOffset", 17, false, toRuntime,
		"where the scheduler's record holds how many timers the heap holds, under the lock."},
	{"schedTimerCapacityOffset", 18, false, toRuntime,
		"where the scheduler's record holds how many timers the heap has room for, under the lock."},
	{"schedTimerNextOffset", 19, false, toRuntime,
		"where the scheduler's record holds when the earliest timer is due, in nanoseconds, or 0 when there is none: written under the lock and read without it."},
	{"schedPollerOffset", 20, false, toRuntime,
		"where the scheduler's record holds the poller: the completion port on Windows, the epoll fd on Linux."},
	{"schedPollBreakOffset", 21, false, toRuntime,
		"where the scheduler's record holds the eventfd that breaks the poller's wait, on Linux."},
	{"schedLastPollOffset", 22, false, toRuntime,
		"where the scheduler's record holds when the poller was last polled, in nanoseconds, and 0 while a thread waits in it (Go's sched.lastpoll)."},
	{"schedPollUntilOffset", 23, false, toRuntime,
		"where the scheduler's record holds when the thread that waits in the poller wakes for a timer, 0 for none (Go's sched.pollUntil)."},
	{"schedPollWakeSigOffset", 24, false, toRuntime,
		"where the scheduler's record holds 1 while a break of the poller's wait is sent and not yet taken (Go's netpollWakeSig)."},
	{"schedPollWaitersOffset", 25, false, toRuntime,
		"where the scheduler's record holds how many green threads wait on a poll descriptor, stepped atomically."},
	{"schedExitClaimOffset", 26, false, toRuntime | toSelfBack,
		"where the scheduler's record holds the context of the thread that holds the claim of exiting and stopping threads (Go's suspendLock), 0 for none: the monitor holds it while a thread it stops is suspended, and a thread that ends the process takes it for good, so ExitProcess never meets a thread the monitor suspended."},
	{"schedMainThreadOffset", 27, false, toRuntime,
		"where the scheduler's record holds the main thread's context."},
	{"schedMonitorOffset", 28, false, toRuntime,
		"where the scheduler's record holds the monitor's context (Go's sysmon)."},
	{"schedRegistersOffset", 29, false, toRuntime,
		"where the scheduler's record holds the CONTEXT the monitor reads and sets a thread's registers in, on Windows."},
	{"schedCarveNextOffset", 30, false, toRuntime,
		"where the scheduler's record holds the next free byte of the block the green threads' records and the stacks' records are carved from, under the lock."},
	{"schedCarveEndOffset", 31, false, toRuntime,
		"where the scheduler's record holds the end of that block."},
	{"schedCarveBytesOffset", 32, false, toRuntime,
		"where the scheduler's record holds the size of the next block that records are carved from, 0 before the first, under the lock."},
	{"schedMadeProcsOffset", 33, false, toRuntime,
		"where the scheduler's record holds how many processors are made: a processor is made when a thread first takes it, and those made are the first of the ids, under the lock."},
	{"schedProcessorsOffset", 34, false, toRuntime,
		"where the scheduler's record holds the address of each processor (Go's allp), a word each, by its id, 0 until it is made."},

	// A processor (Go's P): what a thread must hold to run green threads, its local run queue (a ring that its owner puts
	// at the tail of and that the owner and thieves take from the head of, and runnext, the green thread readied last),
	// its caches of free green threads and stacks, and the ticks the monitor reads.
	{"procIdOffset", 0, false, toRuntime,
		"where a processor holds its index among the processors."},
	{"procStatusOffset", 1, false, toRuntime,
		"where a processor holds what it is doing (procIdle, procRunning, procSyscall), which the monitor takes it from a blocking call by with a compare-exchange."},
	{"procThreadOffset", 2, false, toRuntime,
		"where a processor holds the context of the thread that holds it (Go's p.m), 0 while none or while that thread is in a blocking call."},
	{"procLinkOffset", 3, false, toRuntime,
		"where a processor holds the next idle processor, under the scheduler's lock."},
	{"procSchedTickOffset", 4, false, toRuntime,
		"where a processor holds how many green threads it has started running (Go's schedtick)."},
	{"procSyscallTickOffset", 5, false, toRuntime,
		"where a processor holds how many blocking calls its threads have entered (Go's syscalltick)."},
	{"procSeenTickOffset", 6, false, toRuntime,
		"where a processor holds the schedtick the monitor saw last (Go's sysmontick.schedtick), which only the monitor reads and writes."},
	{"procSeenWhenOffset", 7, false, toRuntime,
		"where a processor holds when the monitor first saw that schedtick, in nanoseconds."},
	{"procSeenSyscallTickOffset", 8, false, toRuntime,
		"where a processor holds the syscalltick the monitor saw last."},
	{"procSeenSyscallWhenOffset", 9, false, toRuntime,
		"where a processor holds when the monitor first saw that syscalltick, in nanoseconds."},
	{"procRunqHeadOffset", 10, false, toRuntime,
		"where a processor holds the index of the oldest green thread of its run queue, which a taker advances with a compare-exchange."},
	{"procRunqTailOffset", 11, false, toRuntime,
		"where a processor holds the index past the newest green thread of its run queue, which only its owner writes."},
	{"procRunNextOffset", 12, false, toRuntime,
		"where a processor holds the green thread readied last, which runs next (Go's runnext), 0 for none."},
	{"procFreeGreensOffset", 13, false, toRuntime,
		"where a processor holds its free green threads' records, linked through greenLinkOffset, which only its owner reads and writes."},
	{"procFreeGreenCountOffset", 14, false, toRuntime,
		"where a processor holds how many free green threads it keeps."},
	{"procFreeStacksOffset", 15, false, toRuntime,
		"where a processor holds its free stacks, linked through pooledStackNextOffset, which only its owner reads and writes."},
	{"procFreeStackCountOffset", 16, false, toRuntime,
		"where a processor holds how many free stacks it keeps."},
	{"procRunqOffset", 17, false, toRuntime,
		"where a processor's run queue begins, procRunqSlots green threads; index i is slot i modulo procRunqSlots."},
	{"procRunqSlots", 256, true, toRuntime,
		"how many green threads a processor's run queue holds, Go's: a full one sends half of them to the global run queue."},
	{"procBytes", 273, false, toRuntime,
		"the size of a processor."},

	// A green thread (Go's g): its state, the code it runs and the word it runs it with, the arguments the code reads, the
	// word the code gave back, the stack it runs on once it runs and the registers of that stack while it does not.
	{"greenStatusOffset", 0, false, toRuntime,
		"where a green thread holds its status (greenIdle, greenRunnable, ...), changed with compare-exchanges where a reader of another thread may race."},
	{"greenLinkOffset", 1, false, toRuntime,
		"where a green thread holds the next of the list it is on: the global run queue or a list of free records."},
	{"greenStackOffset", 2, false, toRuntime,
		"where a green thread holds its pooled stack, 0 until it first runs and after it finishes."},
	{"greenCodeOffset", 3, false, toRuntime,
		"where a green thread holds the address of the code it runs, which is called with greenArgOffset's word."},
	{"greenArgOffset", 4, false, toRuntime,
		"where a green thread holds the word its code is called with: a behaviour's record, or the green thread itself."},
	{"greenResultOffset", 5, false, toRuntime,
		"where a green thread holds the word its code gave back once it finished."},
	{"greenWaiterOffset", 6, false, toRuntime,
		"where a green thread holds the green thread that waits for it to finish, or greenFinished once it has, exchanged atomically."},
	{"greenKindOffset", 7, false, toRuntime,
		"where a green thread holds whether it runs a behaviour, whose record the scheduler frees when it finishes, or is awaited."},
	{"greenWhenOffset", 8, false, toRuntime,
		"where a green thread holds when its sleep ends, in nanoseconds, while it is on the timers' heap."},
	{"greenPreemptOffset", 9, false, toRuntime,
		"where a green thread holds 1 while the monitor asks that it be stopped (Go's g.preempt)."},
	{"greenArgsOffset", 10, false, toRuntime | toSelfBack,
		"where a green thread's arguments begin, greenArgWords words that its code reads: an async call's arguments, which its thunk passes to the function."},
	{"greenArgWords", 8, true, toRuntime | toSelfBack,
		"how many words of arguments a green thread holds, and so how many an async call passes."},
	{"greenSwitchOffset", 18, false, toRuntime,
		"where a green thread holds the switch record of its stack while it does not run."},
	// A strand is a green thread that made a coroutine, its root, and the coroutines it and they make, which run one at a
	// time: the root's record holds the strand, and every member names the root.
	{"greenStrandOffset", 48, false, toRuntime,
		"where a green thread holds the root of the strand it is a member of, 0 for one that has made no coroutine and is none."},
	{"greenStrandHolderOffset", 49, false, toRuntime,
		"where the root of a strand holds the member that may run, 0 while each waits for something other than the strand, under the strand's lock."},
	{"greenStrandHeadOffset", 50, false, toRuntime,
		"where the root of a strand holds the first of the members that wait to run, linked through greenLinkOffset, under the strand's lock."},
	{"greenStrandTailOffset", 51, false, toRuntime,
		"where the root of a strand holds the last of the members that wait to run, under the strand's lock."},
	{"greenStrandLockOffset", 52, false, toRuntime,
		"where the root of a strand holds the lock of its holder and its queue, 1 while it is held."},
	{"greenErrorOffset", 53, false, toRuntime | toSelfBack,
		"where a coroutine holds what its function failed with, as a call that throws gives it: 0 when it returned."},
	// A stop has the stopped green thread write its own return into rt.asyncPreempt: Windows grows a stack through its
	// guard page only for the stack's own thread, so a write from the monitor's thread at the frontier would fault.
	{"greenResumeOffset", 54, false, toRuntime,
		"where a green thread holds the address the monitor stopped it at, which rt.asyncPreempt returns to, from the stop until asyncPreempted writes it into the slot of greenResumeSlotOffset."},
	{"greenResumeSlotOffset", 55, false, toRuntime,
		"where a green thread holds the address of the word on its stack that rt.asyncPreempt returns through, below the stack pointer it was stopped at, which the stop leaves unwritten."},
	{"greenCancelOffset", 56, false, toRuntime | toSelfBack,
		"where a coroutine holds 1 once its promise is cancelled: its thunk does not start a function that has not started, and its sleeps end at once."},
	{"greenBytes", 57, false, toRuntime,
		"the size of a green thread's record."},

	// A pooled stack: a reserve of stackReserveBytes that the system commits a page at a time, its lowest page the guard.
	{"pooledStackLowOffset", 0, false, toRuntime,
		"where a pooled stack holds the lowest address of its reserve."},
	{"pooledStackHighOffset", 1, false, toRuntime,
		"where a pooled stack holds the address past its top."},
	{"pooledStackLimitOffset", 2, false, toRuntime,
		"where a pooled stack holds the lowest address committed, the TEB's StackLimit on Windows, as its last green thread left it."},
	{"pooledStackNextOffset", 3, false, toRuntime,
		"where a pooled stack holds the next free stack of the list it is on."},
	{"pooledStackBytes", 4, false, toRuntime,
		"the size of a pooled stack's record."},

	// A thread's block, which its context names: the switch record of its scheduler, the timespec its clock and sleeps go
	// through on Linux, and the room the poller writes its events into, so that neither allocates.
	{"threadBlockClockOffset", 30, false, toRuntime,
		"where a thread's block holds a timespec, two words."},
	{"threadBlockEventsOffset", 32, false, toRuntime,
		"where a thread's block holds the poller's events, pollEventSlots of them."},
	{"pollEventSlots", 64, true, toRuntime,
		"how many events a thread takes from the poller at once."},
	{"threadBlockBytes", 288, false, toRuntime,
		"the size of a thread's block: room for pollEventSlots events of windowsEntryBytes each, the larger of the two systems'."},

	// A poll descriptor: what a green thread waits on in the poller, readied by a completion whose key is the descriptor.
	{"pollStateOffset", 0, false, toRuntime,
		"where a poll descriptor holds the green thread that waits on it, or pollSignalled once a completion came and was not yet taken, exchanged atomically."},
	{"pollFdOffset", 1, false, toRuntime,
		"where a poll descriptor holds the eventfd that signals it, on Linux."},
	{"pollDescriptorBytes", 2, false, toRuntime,
		"the size of a poll descriptor."},

	// A Windows OVERLAPPED_ENTRY, which GetQueuedCompletionStatusEx fills one of for each completion.
	{"windowsEntryKeyOffset", 0, false, toRuntime,
		"where an OVERLAPPED_ENTRY holds the completion key."},
	{"windowsEntryBytes", 4, false, toRuntime,
		"the size of an OVERLAPPED_ENTRY."},

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
	{"heapChunkBytesOffset", 260, false, toRuntime,
		"where a heap's record holds the size of its next chunk, 0 before its first."},
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
