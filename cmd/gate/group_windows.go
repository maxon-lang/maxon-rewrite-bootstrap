package main

import (
	"os/exec"
	"strconv"
)

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
