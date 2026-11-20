package driver

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/trace"
	"time"
)

const (
	percent             = 100
	nanosPerMillisecond = 1e6
)

// phaseTimer measures each phase for --timings. Busy cores are the processor time the phase used over its wall time, so
// a phase that keeps every core working reads as the processor count; the kernel's share is mostly the operating system
// committing and zeroing fresh pages, which the collector being off makes common (README, "Allocation"). Every phase
// also leaves a mark in an execution trace, so `go tool trace` shows where each one ends.
type phaseTimer struct {
	on                             bool
	start, clock                   time.Time
	startUser, startKernel         time.Duration
	user, kernel                   time.Duration
	allocated, collections, paused uint64
}

func newPhaseTimer(on bool) *phaseTimer {
	t := &phaseTimer{on: on, start: time.Now()}
	t.clock = t.start
	if on {
		t.startUser, t.startKernel = processCPU()
		t.user, t.kernel = t.startUser, t.startKernel
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		t.allocated, t.collections, t.paused = stats.TotalAlloc, uint64(stats.NumGC), stats.PauseTotalNs
	}
	return t
}

func (t *phaseTimer) mark(phase string) {
	trace.Log(context.Background(), "phase", phase)
	now := time.Now()
	if t.on {
		user, kernel := processCPU()
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		cores, kernelShare := busyCores(user-t.user, kernel-t.kernel, now.Sub(t.clock))
		fmt.Fprintf(os.Stderr, "%-10s %8.1f ms %5.1f cores %3.0f%% kernel %8.1f MB %4d GCs %7.1f ms paused\n", phase,
			float64(now.Sub(t.clock).Nanoseconds())/nanosPerMillisecond,
			cores, kernelShare,
			float64(stats.TotalAlloc-t.allocated)/bytesPerMegabyte,
			uint64(stats.NumGC)-t.collections,
			float64(stats.PauseTotalNs-t.paused)/nanosPerMillisecond)
		t.user, t.kernel = user, kernel
		t.allocated, t.collections, t.paused = stats.TotalAlloc, uint64(stats.NumGC), stats.PauseTotalNs
	}
	t.clock = now
}

func (t *phaseTimer) total() {
	if !t.on {
		return
	}
	user, kernel := processCPU()
	wall := time.Since(t.start)
	cores, kernelShare := busyCores(user-t.startUser, kernel-t.startKernel, wall)
	fmt.Fprintf(os.Stderr, "%-10s %8.1f ms %5.1f cores %3.0f%% kernel: %.0f%% of %d processors busy\n", "total",
		float64(wall.Nanoseconds())/nanosPerMillisecond, cores, kernelShare,
		percent*cores/float64(runtime.NumCPU()), runtime.NumCPU())
}

// busyCores is how many cores were working on average over wall, and the percentage of that work the kernel did.
func busyCores(user, kernel, wall time.Duration) (cores, kernelShare float64) {
	if wall <= 0 {
		return 0, 0
	}
	cores = float64(user+kernel) / float64(wall)
	if user+kernel > 0 {
		kernelShare = percent * float64(kernel) / float64(user+kernel)
	}
	return cores, kernelShare
}
