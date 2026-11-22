//go:build windows

// Command sample profiles an executable that minimaxon built: it runs a command line, samples its main thread's (or, with
// -threads, every running thread's) instruction pointer and its chain of rbp frames at a fixed interval, and prints the
// functions by samples, using the symbol map that `minimaxon build --emit-map` writes beside the executable.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const usageText = `usage:
  sample [-map FILE] [-interval DURATION] [-threads] [-top N] [-callers N] [-focus TEXT,...] [-o FILE] <exe> [arg...]

Runs <exe> with its arguments, samples where its main thread is (with -threads, each of its threads that ran since its
last sample, so that a parked or blocked thread adds nothing), and prints the functions by the samples they were
running in (self) and by the samples they were on the stack in (total), with the callers of the top functions. The map
defaults to <exe>.map, which minimaxon build --emit-map writes. The profile goes to standard error, or to -o FILE; the
program's own output goes where sample's does. sample exits with the program's exit code.`

const (
	exitUsage = 2
	exitError = 1

	createSuspended   = 0x00000004
	startfUseStdHands = 0x00000100
	waitObject0       = 0
	waitTimeout       = 0x102
	infinite          = 0xFFFFFFFF

	// contextControlInteger asks GetThreadContext for rip, rsp and rbp (CONTEXT_AMD64 | CONTEXT_CONTROL |
	// CONTEXT_INTEGER); the offsets are those of the x64 CONTEXT record.
	contextControlInteger = 0x00100003
	contextSize           = 1232
	contextAlign          = 16
	contextFlagsOffset    = 0x30
	contextRspOffset      = 0x98
	contextRbpOffset      = 0xA0
	contextRipOffset      = 0xF8

	// processBasicInformation is the class of NtQueryInformationProcess that gives the PEB, whose ImageBaseAddress is
	// where the loader put the executable: minimaxon's PE allows ASLR, so the base is read, never assumed.
	processBasicInformation = 0
	pebOffsetInBasicInfo    = 8
	pebImageBaseOffset      = 0x10
	basicInfoSize           = 48

	// A frame is the saved rbp at [rbp] and the return address at [rbp+8]. maxFrames bounds a walk, and maxFrameStep
	// stops it at a chain that leaves the stack.
	maxFrames      = 256
	maxFrameStep   = 64 << 20
	pointerSize    = 8
	frameWordCount = 2
	// minimaxon's prologue is push rbp (1 byte) then mov rbp, rsp (3 bytes): before it ends, rbp is still the caller's
	// frame, and the return address is at [rsp] or, after the push, at [rsp+8].
	pushRbpBytes = 1
	// The scan out of code outside the image reads the stack a page at a time, as far as stackScanBytes.
	stackScanChunk = 4096
	stackScanBytes = 256 << 10
	prologueBytes  = 4
	outsideImage   = "(outside the image)"
	unknownAddress = "(no symbol)"
	// callersListed is how many callers each of the top functions lists.
	callersListed = 12

	// With -threads, the threads are listed again every threadListEvery samples, and a thread is sampled only when it ran
	// more than ranCycles since its last sample: a thread that waits on a lock or an event is not running code.
	th32csSnapThread    = 0x00000004
	threadSuspendResume = 0x0002
	threadGetContext    = 0x0008
	threadQueryInfo     = 0x0040
	threadListEvery     = 16
	ranCycles           = 100000
	threadEntrySize     = 28
	ownerProcessOffset  = 12
	threadIDOffset      = 8
)

var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	ntdll                     = syscall.NewLazyDLL("ntdll.dll")
	procSuspendThread         = kernel32.NewProc("SuspendThread")
	procResumeThread          = kernel32.NewProc("ResumeThread")
	procGetThreadContext      = kernel32.NewProc("GetThreadContext")
	procReadProcessMemory     = kernel32.NewProc("ReadProcessMemory")
	procCreateToolhelp32      = kernel32.NewProc("CreateToolhelp32Snapshot")
	procThread32First         = kernel32.NewProc("Thread32First")
	procThread32Next          = kernel32.NewProc("Thread32Next")
	procOpenThread            = kernel32.NewProc("OpenThread")
	procQueryThreadCycleTime  = kernel32.NewProc("QueryThreadCycleTime")
	procNtQueryInformationPro = ntdll.NewProc("NtQueryInformationProcess")
)

// symbolEntry is one line of the map: a function or routine from its RVA for size bytes.
type symbolEntry struct {
	rva, size uint32
	name      string
}

type symbolMap struct{ entries []symbolEntry }

func readMap(path string) (*symbolMap, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m := &symbolMap{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<16), 1<<20)
	for line := 1; scanner.Scan(); line++ {
		fields := strings.SplitN(scanner.Text(), " ", 3)
		if len(fields) != 3 {
			return nil, fmt.Errorf("%s:%d: expected <rva> <size> <symbol>", path, line)
		}
		rva, err1 := strconv.ParseUint(fields[0], 16, 32)
		size, err2 := strconv.ParseUint(fields[1], 16, 32)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("%s:%d: the RVA and the size are hexadecimal", path, line)
		}
		m.entries = append(m.entries, symbolEntry{uint32(rva), uint32(size), fields[2]})
	}
	return m, scanner.Err()
}

// find is the index of the entry an RVA is in, or -1.
func (m *symbolMap) find(rva uint64) int {
	i := sort.Search(len(m.entries), func(i int) bool { return uint64(m.entries[i].rva) > rva }) - 1
	if i < 0 || rva >= uint64(m.entries[i].rva)+uint64(m.entries[i].size) {
		return -1
	}
	return i
}

// target is the process being sampled.
type target struct {
	process syscall.Handle
	base    uint64
	symbols *symbolMap
	context []byte
}

// sampledThread is a thread of the target that -threads samples, with its cycle count when it was last looked at.
type sampledThread struct {
	handle syscall.Handle
	cycles uint64
}

// threadIDs are the ids of the threads of the process now.
func threadIDs(pid uint32) []uint32 {
	snapshot, _, _ := procCreateToolhelp32.Call(th32csSnapThread, 0)
	if syscall.Handle(snapshot) == syscall.InvalidHandle {
		return nil
	}
	defer syscall.CloseHandle(syscall.Handle(snapshot))
	var entry [threadEntrySize]byte
	*(*uint32)(unsafe.Pointer(&entry[0])) = threadEntrySize
	var ids []uint32
	for r, _, _ := procThread32First.Call(snapshot, uintptr(unsafe.Pointer(&entry[0]))); r != 0; r, _, _ = procThread32Next.Call(snapshot, uintptr(unsafe.Pointer(&entry[0]))) {
		if *(*uint32)(unsafe.Pointer(&entry[ownerProcessOffset])) == pid {
			ids = append(ids, *(*uint32)(unsafe.Pointer(&entry[threadIDOffset])))
		}
	}
	return ids
}

// cyclesOf is the CPU cycles the thread has run.
func cyclesOf(thread syscall.Handle) uint64 {
	var cycles uint64
	procQueryThreadCycleTime.Call(uintptr(thread), uintptr(unsafe.Pointer(&cycles)))
	return cycles
}

func (t *target) read(address uint64, out []byte) bool {
	var done uintptr
	r, _, _ := procReadProcessMemory.Call(uintptr(t.process), uintptr(address), uintptr(unsafe.Pointer(&out[0])), uintptr(len(out)), uintptr(unsafe.Pointer(&done)))
	return r != 0 && int(done) == len(out)
}

func (t *target) readWord(address uint64) (uint64, bool) {
	var word [pointerSize]byte
	if !t.read(address, word[:]) {
		return 0, false
	}
	return *(*uint64)(unsafe.Pointer(&word[0])), true
}

func readImageBase(process syscall.Handle) (uint64, error) {
	var info [basicInfoSize]byte
	var length uint32
	status, _, _ := procNtQueryInformationPro.Call(uintptr(process), processBasicInformation, uintptr(unsafe.Pointer(&info[0])), basicInfoSize, uintptr(unsafe.Pointer(&length)))
	if status != 0 {
		return 0, fmt.Errorf("NtQueryInformationProcess failed with status %#x", status)
	}
	peb := *(*uint64)(unsafe.Pointer(&info[pebOffsetInBasicInfo]))
	t := &target{process: process}
	base, ok := t.readWord(peb + pebImageBaseOffset)
	if !ok {
		return 0, fmt.Errorf("cannot read the image base from the PEB")
	}
	return base, nil
}

// alignedContext is a CONTEXT record in a buffer with the alignment GetThreadContext requires.
func alignedContext() []byte {
	buf := make([]byte, contextSize+contextAlign)
	skip := (contextAlign - int(uintptr(unsafe.Pointer(&buf[0]))%contextAlign)) % contextAlign
	return buf[skip : skip+contextSize]
}

func (t *target) word(offset int) uint64 { return *(*uint64)(unsafe.Pointer(&t.context[offset])) }

// symbolOf is the index of the map entry an address is in, or -1 outside the image or between symbols.
func (t *target) symbolOf(address uint64) int {
	if address < t.base {
		return -1
	}
	return t.symbols.find(address - t.base)
}

// sample stops the thread, reads its stack of return addresses from the instruction pointer outwards, and lets it go.
// It says false once the thread can no longer be read, as when the process is ending.
func (t *target) sample(thread syscall.Handle, stack []uint64) ([]uint64, bool) {
	if r, _, _ := procSuspendThread.Call(uintptr(thread)); int32(r) == -1 {
		return stack, false
	}
	defer procResumeThread.Call(uintptr(thread))
	*(*uint32)(unsafe.Pointer(&t.context[contextFlagsOffset])) = contextControlInteger
	if r, _, _ := procGetThreadContext.Call(uintptr(thread), uintptr(unsafe.Pointer(&t.context[0]))); r == 0 {
		return stack, false
	}
	rip, rsp, rbp := t.word(contextRipOffset), t.word(contextRspOffset), t.word(contextRbpOffset)
	stack = append(stack[:0], rip)
	if i := t.symbolOf(rip); i < 0 {
		rbp = t.scanForFrame(rsp, &stack)
	} else {
		into := rip - t.base - uint64(t.symbols.entries[i].rva)
		if into < prologueBytes {
			slot := rsp
			if into >= pushRbpBytes {
				slot += pointerSize
			}
			if ret, ok := t.readWord(slot); ok {
				stack = append(stack, ret)
			}
		}
	}
	var frame [frameWordCount * pointerSize]byte
	for len(stack) < maxFrames && rbp != 0 {
		if !t.read(rbp, frame[:]) {
			break
		}
		next := *(*uint64)(unsafe.Pointer(&frame[0]))
		ret := *(*uint64)(unsafe.Pointer(&frame[pointerSize]))
		if t.symbolOf(ret) < 0 {
			break
		}
		stack = append(stack, ret)
		if next <= rbp || next-rbp > maxFrameStep {
			break
		}
		rbp = next
	}
	return stack, true
}

// scanForFrame finds the way back into the image from code outside it, such as a Windows DLL that the runtime called,
// which need not keep rbp frames: the first word on the stack that is a return address into the image is the call into
// that code, and the first frame record above it, a saved rbp that points further up the stack next to a return address
// into the image, is the frame of the function that made the call. It returns that frame, or 0 if there is none.
func (t *target) scanForFrame(rsp uint64, stack *[]uint64) uint64 {
	// The reads end on page boundaries, since the stack's base is one and the page past it is not mapped: a read that
	// crosses it fails whole.
	var page [stackScanChunk]byte
	words := make([]uint64, 0, stackScanChunk/pointerSize)
	at := rsp
	for read := 0; read < stackScanBytes; {
		chunk := stackScanChunk - int(at%stackScanChunk)
		if !t.read(at, page[:chunk]) {
			break
		}
		for i := 0; i+pointerSize <= chunk; i += pointerSize {
			words = append(words, *(*uint64)(unsafe.Pointer(&page[i])))
		}
		at += uint64(chunk)
		read += chunk
	}
	call := -1
	for i, w := range words {
		if t.symbolOf(w) >= 0 {
			call = i
			break
		}
	}
	if call < 0 {
		return 0
	}
	*stack = append(*stack, words[call])
	for j := call + 1; j+1 < len(words); j++ {
		at := rsp + uint64(j*pointerSize)
		if words[j] > at && words[j]-at <= maxFrameStep && t.symbolOf(words[j+1]) >= 0 {
			return at
		}
	}
	return 0
}

// profile is what the samples add up to, per map entry; the last index stands for addresses outside any symbol.
type profile struct {
	samples int
	// waiting counts the looks at a thread, with -threads, that found it had not run since the last.
	waiting     int
	self, total []int
	// callers counts, for each function, the samples in which it was called from each caller, once per sample.
	callers []map[int]int
}

func newProfile(symbols int) *profile {
	return &profile{self: make([]int, symbols+1), total: make([]int, symbols+1), callers: make([]map[int]int, symbols+1)}
}

func (p *profile) add(t *target, stack []uint64, seen []int) {
	p.samples++
	unknown := len(p.self) - 1
	ids := seen[:0]
	for _, address := range stack {
		id := t.symbolOf(address)
		if id < 0 {
			id = unknown
		}
		ids = append(ids, id)
	}
	p.self[ids[0]]++
	for i, id := range ids {
		if slices.Contains(ids[:i], id) {
			continue
		}
		p.total[id]++
		if i+1 < len(ids) {
			if p.callers[id] == nil {
				p.callers[id] = map[int]int{}
			}
			p.callers[id][ids[i+1]]++
		}
	}
}

func main() {
	os.Exit(run())
}

func run() int {
	flags := flag.NewFlagSet("sample", flag.ContinueOnError)
	flags.Usage = func() { fmt.Fprintln(os.Stderr, usageText) }
	mapPath := flags.String("map", "", "the symbol map (default <exe>.map)")
	interval := flags.Duration("interval", time.Millisecond, "the time between samples")
	allThreads := flags.Bool("threads", false, "sample every thread that ran since its last sample, not only the main thread")
	top := flags.Int("top", 40, "how many functions each table lists")
	callersOf := flags.Int("callers", 15, "how many of the top functions by self samples get their callers listed")
	focus := flags.String("focus", "", "a comma-separated list of texts: the functions whose symbol contains one also get their callers listed")
	outPath := flags.String("o", "", "where the profile goes (default standard error)")
	if err := flags.Parse(os.Args[1:]); err != nil || flags.NArg() == 0 {
		flags.Usage()
		return exitUsage
	}
	exe := flags.Arg(0)
	if filepath.Ext(exe) == "" {
		exe += ".exe"
	}
	if *mapPath == "" {
		*mapPath = exe + ".map"
	}
	symbols, err := readMap(*mapPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sample: %v (build the program with minimaxon build --emit-map)\n", err)
		return exitError
	}
	out := io.Writer(os.Stderr)
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sample: %v\n", err)
			return exitError
		}
		defer f.Close()
		out = f
	}

	var commandLine strings.Builder
	for i, a := range append([]string{exe}, flags.Args()[1:]...) {
		if i > 0 {
			commandLine.WriteByte(' ')
		}
		commandLine.WriteString(syscall.EscapeArg(a))
	}
	line, err := syscall.UTF16PtrFromString(commandLine.String())
	if err != nil {
		fmt.Fprintf(os.Stderr, "sample: %v\n", err)
		return exitError
	}
	startup := &syscall.StartupInfo{Flags: startfUseStdHands, StdInput: syscall.Stdin, StdOutput: syscall.Stdout, StdErr: syscall.Stderr}
	startup.Cb = uint32(unsafe.Sizeof(*startup))
	var info syscall.ProcessInformation
	start := time.Now()
	if err := syscall.CreateProcess(nil, line, nil, nil, true, createSuspended, nil, nil, startup, &info); err != nil {
		fmt.Fprintf(os.Stderr, "sample: cannot start %s: %v\n", exe, err)
		return exitError
	}
	defer syscall.CloseHandle(info.Process)
	defer syscall.CloseHandle(info.Thread)
	base, err := readImageBase(info.Process)
	if err != nil {
		syscall.TerminateProcess(info.Process, exitError)
		fmt.Fprintf(os.Stderr, "sample: %v\n", err)
		return exitError
	}
	t := &target{process: info.Process, base: base, symbols: symbols, context: alignedContext()}
	p := newProfile(len(symbols.entries))
	procResumeThread.Call(uintptr(info.Thread))

	var stack []uint64
	seen := make([]int, 0, maxFrames)
	threads := map[uint32]*sampledThread{}
	var order []uint32
	for round := 0; ; round++ {
		event, _ := syscall.WaitForSingleObject(info.Process, 0)
		if event == waitObject0 {
			break
		}
		if !*allThreads {
			var ok bool
			if stack, ok = t.sample(info.Thread, stack); ok {
				p.add(t, stack, seen)
			}
			time.Sleep(*interval)
			continue
		}
		if round%threadListEvery == 0 {
			order = order[:0]
			for _, id := range threadIDs(info.ProcessId) {
				if threads[id] == nil {
					h, _, _ := procOpenThread.Call(threadSuspendResume|threadGetContext|threadQueryInfo, 0, uintptr(id))
					if h == 0 {
						continue
					}
					threads[id] = &sampledThread{handle: syscall.Handle(h), cycles: cyclesOf(syscall.Handle(h))}
				}
				order = append(order, id)
			}
		}
		for _, id := range order {
			th := threads[id]
			now := cyclesOf(th.handle)
			ran := now-th.cycles > ranCycles
			th.cycles = now
			if !ran {
				p.waiting++
				continue
			}
			var ok bool
			if stack, ok = t.sample(th.handle, stack); ok {
				p.add(t, stack, seen)
			}
		}
		time.Sleep(*interval)
	}
	for _, th := range threads {
		syscall.CloseHandle(th.handle)
	}
	syscall.WaitForSingleObject(info.Process, infinite)
	wall := time.Since(start)
	var code uint32
	syscall.GetExitCodeProcess(info.Process, &code)

	report(out, p, symbols, wall, *interval, *top, *callersOf, *focus)
	return int(code)
}

func nameOf(symbols *symbolMap, id int) string {
	if id == len(symbols.entries) {
		return unknownAddress + " / " + outsideImage
	}
	return symbols.entries[id].name
}

// ranked is the ids with a count above zero, most first, then in address order.
func ranked(counts []int) []int {
	var ids []int
	for id, c := range counts {
		if c > 0 {
			ids = append(ids, id)
		}
	}
	sort.SliceStable(ids, func(i, j int) bool { return counts[ids[i]] > counts[ids[j]] })
	return ids
}

func percent(n, of int) float64 { return 100 * float64(n) / float64(max(of, 1)) }

func report(out io.Writer, p *profile, symbols *symbolMap, wall, interval time.Duration, top, callersOf int, focus string) {
	fmt.Fprintf(out, "wall %.3fs, %d samples at %v", wall.Seconds(), p.samples, interval)
	if p.waiting > 0 {
		fmt.Fprintf(out, ", %d looks at a thread that had not run", p.waiting)
	}
	fmt.Fprintf(out, "\n\n")
	fmt.Fprintf(out, "by self samples (self%%, total%%):\n")
	bySelf := ranked(p.self)
	for _, id := range bySelf[:min(top, len(bySelf))] {
		fmt.Fprintf(out, "%6.2f%% %6.2f%% %7d  %s\n", percent(p.self[id], p.samples), percent(p.total[id], p.samples), p.self[id], nameOf(symbols, id))
	}
	fmt.Fprintf(out, "\nby total samples, on the stack (total%%, self%%):\n")
	byTotal := ranked(p.total)
	for _, id := range byTotal[:min(top, len(byTotal))] {
		fmt.Fprintf(out, "%6.2f%% %6.2f%% %7d  %s\n", percent(p.total[id], p.samples), percent(p.self[id], p.samples), p.total[id], nameOf(symbols, id))
	}
	listed := bySelf[:min(callersOf, len(bySelf))]
	if focus != "" {
		for _, id := range byTotal {
			for text := range strings.SplitSeq(focus, ",") {
				if strings.Contains(nameOf(symbols, id), text) && !slices.Contains(listed, id) {
					listed = append(listed, id)
				}
			}
		}
	}
	fmt.Fprintf(out, "\ncallers of the top functions by self samples and of the focused ones (share of the function's total):\n")
	for _, id := range listed {
		fmt.Fprintf(out, "%s (self %d, total %d)\n", nameOf(symbols, id), p.self[id], p.total[id])
		callers := make([]int, len(p.self))
		for caller, n := range p.callers[id] {
			callers[caller] = n
		}
		byCount := ranked(callers)
		for _, caller := range byCount[:min(callersListed, len(byCount))] {
			fmt.Fprintf(out, "    %6.2f%% %7d  %s\n", percent(callers[caller], p.total[id]), callers[caller], nameOf(symbols, caller))
		}
	}
}
