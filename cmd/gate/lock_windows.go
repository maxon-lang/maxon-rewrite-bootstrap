package main

import (
	"errors"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"
	"unsafe"
)

// heavyMutexName is lock.ps1's named mutex, so that selftime.ps1 and the gate exclude each other.
const heavyMutexName = `Global\minimaxon-heavy`

const (
	waitObject0   = 0x0
	waitAbandoned = 0x80
	waitTimeout   = 0x102
	waitInfinite  = 0xFFFFFFFF
	noWait        = 0
	stillActive   = 259
	queryLimited  = 0x1000
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	createMutex        = kernel32.NewProc("CreateMutexW")
	waitForObject      = kernel32.NewProc("WaitForSingleObject")
	releaseMutex       = kernel32.NewProc("ReleaseMutex")
	getExitCodeProcess = kernel32.NewProc("GetExitCodeProcess")
)

// takeHeavyLock takes the named mutex on the thread that will let it go: a Windows mutex belongs to a thread, so the
// goroutine is wired to its thread until the release.
func takeHeavyLock(waiting func()) (func(), error) {
	runtime.LockOSThread()
	name, err := syscall.UTF16PtrFromString(heavyMutexName)
	if err != nil {
		runtime.UnlockOSThread()
		return nil, err
	}
	handle, _, callErr := createMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		runtime.UnlockOSThread()
		return nil, errors.Join(errors.New("gate: cannot open the machine's lock "+heavyMutexName), callErr)
	}
	got, _, _ := waitForObject.Call(handle, noWait)
	if got == waitTimeout {
		waiting()
		got, _, _ = waitForObject.Call(handle, waitInfinite)
	}
	if got != waitObject0 && got != waitAbandoned {
		syscall.CloseHandle(syscall.Handle(handle))
		runtime.UnlockOSThread()
		return nil, errors.New("gate: cannot take the machine's lock " + heavyMutexName)
	}
	return func() {
		releaseMutex.Call(handle)
		syscall.CloseHandle(syscall.Handle(handle))
		runtime.UnlockOSThread()
	}, nil
}

func processAlive(pid int) bool {
	handle, err := syscall.OpenProcess(queryLimited, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(handle)
	var code uint32
	ok, _, _ := getExitCodeProcess.Call(uintptr(handle), uintptr(unsafe.Pointer(&code)))
	return ok != 0 && code == stillActive
}

// startGroup starts cmd so that stopGroup can end it and every process it started.
func startGroup(cmd *exec.Cmd) error {
	return cmd.Start()
}

// stopGroup ends a process and the processes it started.
func stopGroup(cmd *exec.Cmd) {
	if cmd.Process == nil || cmd.ProcessState != nil {
		return
	}
	_ = exec.Command("taskkill.exe", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run()
}
