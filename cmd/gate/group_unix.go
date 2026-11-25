//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

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
