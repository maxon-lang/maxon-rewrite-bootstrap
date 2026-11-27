//go:build windows

package main

import (
	"net"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	psapi    = syscall.NewLazyDLL("psapi.dll")

	getProcessTimes          = kernel32.NewProc("GetProcessTimes")
	createToolhelp32Snapshot = kernel32.NewProc("CreateToolhelp32Snapshot")
	thread32First            = kernel32.NewProc("Thread32First")
	thread32Next             = kernel32.NewProc("Thread32Next")
	getProcessMemoryInfo     = psapi.NewProc("GetProcessMemoryInfo")
	getPerformanceInfo       = psapi.NewProc("GetPerformanceInfo")
)

const (
	processQueryInformation = 0x400
	processVMRead           = 0x10
	snapshotThreads         = 0x4
	// ticksPerSecond is how many 100-nanosecond FILETIME ticks make a second.
	ticksPerSecond = 1e7
)

// measure reads the server's working set and private bytes, CPU and threads, and the system's nonpaged pool, which the
// system's requests for each connection's readiness are charged to (AFD poll, runtime/windows_poll.maxon).
func measure(pid int) sample {
	var s sample
	process, err := syscall.OpenProcess(processQueryInformation|processVMRead, false, uint32(pid))
	check(err)
	defer syscall.CloseHandle(process)

	// PROCESS_MEMORY_COUNTERS_EX: the working set is its third word and the private bytes its tenth.
	var counters [10]uint64
	counters[0] = uint64(len(counters) * 8)
	if r, _, err := getProcessMemoryInfo.Call(uintptr(process), uintptr(unsafe.Pointer(&counters[0])), uintptr(len(counters)*8)); r == 0 {
		fail("GetProcessMemoryInfo: %v", err)
	}
	s.rssKB = int64(counters[2] / 1024)
	s.footprintKB = int64(counters[9] / 1024)

	var created, exited, kernel, user [2]uint32
	if r, _, err := getProcessTimes.Call(uintptr(process), uintptr(unsafe.Pointer(&created)), uintptr(unsafe.Pointer(&exited)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user))); r == 0 {
		fail("GetProcessTimes: %v", err)
	}
	ticks := func(t [2]uint32) float64 { return float64(uint64(t[1])<<32|uint64(t[0])) / ticksPerSecond }
	s.userSec, s.sysSec = ticks(user), ticks(kernel)

	// PERFORMANCE_INFORMATION: the kernel's nonpaged pages are its tenth word, the page size its eleventh.
	var info [13]uint64
	info[0] = uint64(len(info) * 8)
	if r, _, err := getPerformanceInfo.Call(uintptr(unsafe.Pointer(&info[0])), uintptr(len(info)*8)); r == 0 {
		fail("GetPerformanceInfo: %v", err)
	}
	s.nonpagedKB = int64(info[9] * info[10] / 1024)

	snapshot, _, err := createToolhelp32Snapshot.Call(snapshotThreads, 0)
	if snapshot == ^uintptr(0) {
		fail("CreateToolhelp32Snapshot: %v", err)
	}
	defer syscall.CloseHandle(syscall.Handle(snapshot))
	// THREADENTRY32: its size, usage count, thread id and owner process id, then three words that name nothing here.
	var entry [7]uint32
	entry[0] = uint32(len(entry) * 4)
	next := thread32First
	for {
		if r, _, _ := next.Call(snapshot, uintptr(unsafe.Pointer(&entry[0]))); r == 0 {
			break
		}
		if int(entry[3]) == pid {
			s.threads++
		}
		next = thread32Next
	}
	return s
}

// The peer opens each connection from a port of its own below the dynamic range, from one of the addresses of 127.0.0.0/8,
// which Windows routes to the loopback: the system gives a bind of port 0 one of the 16384 ports of its dynamic range, less
// those it holds, whatever the address, which fails a run of 16000 connections (WSAENOBUFS) before its end. Each address
// has connectionsPerSource ports from firstSourcePort.
const (
	connectionsPerSource = 8000
	firstSourcePort      = 20000
)

// sourceOf is the local address of the peer's connection number i.
func sourceOf(i int) net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, byte(2+i/connectionsPerSource)), Port: firstSourcePort + i%connectionsPerSource}
}

var (
	queryPerformanceCounter   = kernel32.NewProc("QueryPerformanceCounter")
	queryPerformanceFrequency = kernel32.NewProc("QueryPerformanceFrequency")
	counterTicksPerSecond     = counterFrequency()
)

func counterFrequency() int64 {
	var frequency int64
	queryPerformanceFrequency.Call(uintptr(unsafe.Pointer(&frequency)))
	return frequency
}

// stamp is a reading of the performance counter, and elapsed the time since one: Go's monotonic clock on Windows ticks with
// the system's timer, 0.5 ms at best, which is longer than a round trip of the loopback.
func stamp() int64 {
	var ticks int64
	queryPerformanceCounter.Call(uintptr(unsafe.Pointer(&ticks)))
	return ticks
}

func elapsed(since int64) time.Duration {
	return time.Duration((stamp() - since) * int64(time.Second) / counterTicksPerSecond)
}
