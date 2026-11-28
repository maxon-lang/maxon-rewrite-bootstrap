//go:build windows

package procstat

import (
	"fmt"
	"syscall"
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

// Measure reads a process's working set and private bytes, CPU and threads, and the system's nonpaged pool, which the
// system's requests for each connection's readiness are charged to (AFD poll, runtime/windows_poll.maxon).
func Measure(pid int) (Sample, error) {
	var s Sample
	process, err := syscall.OpenProcess(processQueryInformation|processVMRead, false, uint32(pid))
	if err != nil {
		return s, err
	}
	defer syscall.CloseHandle(process)

	// PROCESS_MEMORY_COUNTERS_EX: the working set is its third word and the private bytes its tenth.
	var counters [10]uint64
	counters[0] = uint64(len(counters) * 8)
	if r, _, err := getProcessMemoryInfo.Call(uintptr(process), uintptr(unsafe.Pointer(&counters[0])), uintptr(len(counters)*8)); r == 0 {
		return s, fmt.Errorf("GetProcessMemoryInfo: %v", err)
	}
	s.RSSKB = int64(counters[2] / 1024)
	s.FootprintKB = int64(counters[9] / 1024)

	var created, exited, kernel, user [2]uint32
	if r, _, err := getProcessTimes.Call(uintptr(process), uintptr(unsafe.Pointer(&created)), uintptr(unsafe.Pointer(&exited)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user))); r == 0 {
		return s, fmt.Errorf("GetProcessTimes: %v", err)
	}
	ticks := func(t [2]uint32) float64 { return float64(uint64(t[1])<<32|uint64(t[0])) / ticksPerSecond }
	s.UserSec, s.SysSec = ticks(user), ticks(kernel)

	// PERFORMANCE_INFORMATION: the kernel's nonpaged pages are its tenth word, the page size its eleventh.
	var info [13]uint64
	info[0] = uint64(len(info) * 8)
	if r, _, err := getPerformanceInfo.Call(uintptr(unsafe.Pointer(&info[0])), uintptr(len(info)*8)); r == 0 {
		return s, fmt.Errorf("GetPerformanceInfo: %v", err)
	}
	s.NonpagedKB = int64(info[9] * info[10] / 1024)

	snapshot, _, err := createToolhelp32Snapshot.Call(snapshotThreads, 0)
	if snapshot == ^uintptr(0) {
		return s, fmt.Errorf("CreateToolhelp32Snapshot: %v", err)
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
			s.Threads++
		}
		next = thread32Next
	}
	return s, nil
}
