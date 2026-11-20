package driver

import (
	"minimaxon/internal/diag"
	"syscall"
	"time"
)

// processCPU is the processor time the compiler has used so far, in user and kernel mode. Windows advances it once a
// clock tick (about 15.6 ms), so a phase shorter than a few ticks reads as noise.
func processCPU() (user, kernel time.Duration) {
	process, err := syscall.GetCurrentProcess()
	if err != nil {
		panic(diag.Internal("cannot read the process times: %v", err))
	}
	var created, exited, kernelTime, userTime syscall.Filetime
	if err := syscall.GetProcessTimes(process, &created, &exited, &kernelTime, &userTime); err != nil {
		panic(diag.Internal("cannot read the process times: %v", err))
	}
	return time.Duration(userTime.Nanoseconds()), time.Duration(kernelTime.Nanoseconds())
}
