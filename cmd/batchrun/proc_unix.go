//go:build unix

package main

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

// inGroup starts the run in a process group of its own, which a kill at its deadline ends with every child it made.
func inGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killGroup(cmd *exec.Cmd) {
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

func signalOf(state *os.ProcessState) (int, bool) {
	ws, ok := state.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() {
		return 0, false
	}
	return int(ws.Signal()), true
}

// systemLogWindow is how far back the system log is read for a run that did not exit.
const systemLogWindow = "30s"

// writeSystemLog keeps what macOS's log says of a process of the name that did not exit, such as AMFI's refusal of an
// executable or a crash report's line, beside its run.
func writeSystemLog(path, name string) {
	if runtime.GOOS != "darwin" {
		return
	}
	text, err := exec.Command("log", "show", "--last", systemLogWindow, "--style", "compact", "--predicate", `process == "`+name+`" OR eventMessage CONTAINS "`+name+`"`).CombinedOutput()
	if err != nil {
		text = append(text, []byte("\nlog show: "+err.Error()+"\n")...)
	}
	_ = os.WriteFile(path, text, 0o644)
}
