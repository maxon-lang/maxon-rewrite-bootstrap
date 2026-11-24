//go:build unix

package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"syscall"
)

// backgroundNice is the nice value of maxon's background priority.
const backgroundNice = 10

// lowerPriority gives the gate a nice value of backgroundNice, as maxon's spec-test does, so that everything it starts, which
// inherits it, leaves the machine usable for a foreground job. Linux scopes nice to a thread, so every thread the gate has
// is lowered, and the ones it starts afterwards inherit from them; Darwin scopes it to the process.
func lowerPriority() error {
	if _, err := os.Stat(threadDirectory); err != nil {
		return setNice(0)
	}
	lowered := map[int]bool{}
	for {
		tasks, err := os.ReadDir(threadDirectory)
		if err != nil {
			return err
		}
		fresh := false
		for _, task := range tasks {
			tid, err := strconv.Atoi(task.Name())
			if err != nil {
				return fmt.Errorf("gate: unexpected thread entry %q in %s", task.Name(), threadDirectory)
			}
			if lowered[tid] {
				continue
			}
			lowered[tid] = true
			fresh = true
			// A thread that ended since the listing has nothing to lower.
			if err := setNice(tid); err != nil && !errors.Is(err, syscall.ESRCH) {
				return err
			}
		}
		if !fresh {
			return nil
		}
	}
}

const threadDirectory = "/proc/self/task"

// setNice lowers the priority of the thread or process who, where 0 is the caller's process.
func setNice(who int) error {
	if err := syscall.Setpriority(syscall.PRIO_PROCESS, who, backgroundNice); err != nil {
		return fmt.Errorf("gate: cannot lower the priority: %w", err)
	}
	return nil
}
