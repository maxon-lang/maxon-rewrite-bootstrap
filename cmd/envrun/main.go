// Command envrun runs a command with environment variables set for it alone, so that the test runner can start the same
// executable under another worker count (MAXON_MAX_PROCS) without the runner's own environment, and so its own width,
// changing. It also has WSL pass the variables on (WSLENV names the ones it forwards), which a Linux executable run through
// wsl.exe needs, and ends the command when the deadline passes, since the runner can end only the process it started and not
// the one that process started:
//
//	envrun [--deadline=MS] NAME=VALUE... command [argument...]
//
// The command's output and exit code are envrun's. When the deadline passes envrun ends the command and then waits, so that
// the runner, whose deadline it is, ends envrun and reports the run as timed out as it would for a command with no envrun.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	exitUsage      = 2
	exitFailed     = 125
	deadlineFlag   = "--deadline="
	wslEnvironment = "WSLENV"
	wslSeparator   = ":"
)

// assignment splits arg into the name and value of a variable, or reports that it is not one.
func assignment(arg string) (name, value string, ok bool) {
	name, value, ok = strings.Cut(arg, "=")
	if !ok || name == "" {
		return "", "", false
	}
	for i, c := range name {
		letter := c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
		if !letter && (i == 0 || c < '0' || c > '9') {
			return "", "", false
		}
	}
	return name, value, true
}

// forwardedToWSL is the value of WSLENV with names added to those it already forwards.
func forwardedToWSL(current string, names []string) string {
	var forwarded []string
	if current != "" {
		forwarded = strings.Split(current, wslSeparator)
	}
	for _, name := range names {
		found := false
		for _, entry := range forwarded {
			if before, _, _ := strings.Cut(entry, "/"); before == name {
				found = true
			}
		}
		if !found {
			forwarded = append(forwarded, name)
		}
	}
	return strings.Join(forwarded, wslSeparator)
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var deadline time.Duration
	if len(args) > 0 && strings.HasPrefix(args[0], deadlineFlag) {
		ms, err := strconv.Atoi(strings.TrimPrefix(args[0], deadlineFlag))
		if err != nil || ms < 1 {
			fmt.Fprintf(os.Stderr, "envrun: %s takes a number of milliseconds from 1\n", deadlineFlag)
			return exitUsage
		}
		deadline = time.Duration(ms) * time.Millisecond
		args = args[1:]
	}
	environment := os.Environ()
	var names []string
	for len(args) > 0 {
		name, value, ok := assignment(args[0])
		if !ok {
			break
		}
		environment = append(environment, name+"="+value)
		names = append(names, name)
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: envrun [--deadline=MS] NAME=VALUE... command [argument...]")
		return exitUsage
	}
	environment = append(environment, wslEnvironment+"="+forwardedToWSL(os.Getenv(wslEnvironment), names))
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = environment
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "envrun: %v\n", err)
		return exitFailed
	}
	ended := make(chan error, 1)
	go func() { ended <- cmd.Wait() }()
	var expired <-chan time.Time
	if deadline > 0 {
		timer := time.NewTimer(deadline)
		defer timer.Stop()
		expired = timer.C
	}
	select {
	case err := <-ended:
		if err == nil {
			return 0
		}
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() >= 0 {
			return exit.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "envrun: %v\n", err)
		return exitFailed
	case <-expired:
		_ = cmd.Process.Kill()
		time.Sleep(time.Hour)
		return exitFailed
	}
}
