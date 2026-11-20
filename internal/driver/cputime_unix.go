//go:build unix

package driver

import (
	"minimaxon/internal/diag"
	"syscall"
	"time"
)

// processCPU is the processor time the compiler has used so far, in user and kernel mode.
func processCPU() (user, kernel time.Duration) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		panic(diag.Internal("cannot read the process times: %v", err))
	}
	return time.Duration(usage.Utime.Nano()), time.Duration(usage.Stime.Nano())
}
