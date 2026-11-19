package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// The test runner puts the native run of a program that has a memory-limit directive in a job object that caps the
// process's memory, so a program that never gives memory back fails deterministically instead of by how much a machine has.
// The structures are Windows's JOBOBJECT_EXTENDED_LIMIT_INFORMATION, laid out as it declares them.

const (
	jobObjectExtendedLimitInformation = 9
	jobObjectLimitProcessMemory       = 0x100
	processSetQuota                   = 0x0100
	processTerminate                  = 0x0001
	bytesPerMegabyte                  = 1 << 20
)

type jobBasicLimits struct {
	perProcessUserTimeLimit int64
	perJobUserTimeLimit     int64
	limitFlags              uint32
	minimumWorkingSetSize   uintptr
	maximumWorkingSetSize   uintptr
	activeProcessLimit      uint32
	affinity                uintptr
	priorityClass           uint32
	schedulingClass         uint32
}

type jobExtendedLimits struct {
	basic                 jobBasicLimits
	ioCounters            [6]uint64
	processMemoryLimit    uintptr
	jobMemoryLimit        uintptr
	peakProcessMemoryUsed uintptr
	peakJobMemoryUsed     uintptr
}

var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObject       = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObj  = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObj = kernel32.NewProc("AssignProcessToJobObject")
	procCloseHandle           = kernel32.NewProc("CloseHandle")
)

// limitMemory caps the memory of a running process at megabytes, and returns what ends the cap: close it once the process
// has exited.
func limitMemory(process *os.Process, megabytes int) (release func(), err error) {
	job, _, callErr := procCreateJobObject.Call(0, 0)
	if job == 0 {
		return nil, fmt.Errorf("cannot create a job object: %v", callErr)
	}
	release = func() { procCloseHandle.Call(job) }
	limits := jobExtendedLimits{processMemoryLimit: uintptr(megabytes) * bytesPerMegabyte}
	limits.basic.limitFlags = jobObjectLimitProcessMemory
	if ok, _, callErr := procSetInformationJobObj.Call(job, jobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits)); ok == 0 {
		release()
		return nil, fmt.Errorf("cannot limit the job's memory: %v", callErr)
	}
	handle, openErr := syscall.OpenProcess(processSetQuota|processTerminate, false, uint32(process.Pid))
	if openErr != nil {
		release()
		return nil, fmt.Errorf("cannot open the process: %v", openErr)
	}
	defer syscall.CloseHandle(handle)
	if ok, _, callErr := procAssignProcessToJobObj.Call(job, uintptr(handle)); ok == 0 {
		release()
		return nil, fmt.Errorf("cannot put the process in the job: %v", callErr)
	}
	return release, nil
}
