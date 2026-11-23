//go:build !unix

package main

import (
	"os"
	"os/exec"
)

// A system with no process groups kills the run alone, and has no signal that ends one apart from its exit code.

func inGroup(cmd *exec.Cmd) {}

func killGroup(cmd *exec.Cmd) {
	_ = cmd.Process.Kill()
}

func signalOf(state *os.ProcessState) (int, bool) {
	return 0, false
}

func writeSystemLog(path, name string) {}
