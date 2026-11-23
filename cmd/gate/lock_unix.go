//go:build unix

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// heavyLockFile is the file whose lock is the machine's, in the system's directory of temporary files, which every
// checkout on the machine shares.
const heavyLockFile = "minimaxon-heavy.lock"

func takeHeavyLock(waiting func()) (func(), error) {
	file, err := os.OpenFile(filepath.Join(os.TempDir(), heavyLockFile), os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		return nil, err
	}
	fd := int(file.Fd())
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			file.Close()
			return nil, err
		}
		waiting()
		if err := syscall.Flock(fd, syscall.LOCK_EX); err != nil {
			file.Close()
			return nil, err
		}
	}
	return func() {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		file.Close()
	}, nil
}

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// startGroup starts cmd in a process group of its own, so that stopGroup can end it and every process it started.
func startGroup(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd.Start()
}

// stopGroup ends a process and the processes it started.
func stopGroup(cmd *exec.Cmd) {
	if cmd.Process == nil || cmd.ProcessState != nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
