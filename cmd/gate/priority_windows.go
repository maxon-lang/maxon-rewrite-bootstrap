package main

import "fmt"

const belowNormalPriorityClass = 0x4000

// currentProcess is GetCurrentProcess's pseudo handle, which names the calling process and needs no closing.
const currentProcess = ^uintptr(0)

var setPriorityClass = kernel32.NewProc("SetPriorityClass")

// lowerPriority puts the gate at below-normal priority, as maxon's spec-test does, so that everything it starts, which inherits the
// class, leaves the machine usable for a foreground job.
func lowerPriority() error {
	ok, _, callErr := setPriorityClass.Call(currentProcess, belowNormalPriorityClass)
	if ok == 0 {
		return fmt.Errorf("gate: cannot lower the priority class: %w", callErr)
	}
	return nil
}
