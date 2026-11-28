// Package procstat reads what a benchmark's server process costs: its memory, its CPU and its threads, and on Windows
// the system's nonpaged pool. bench/idle's peer and bench/timers' gobench share it.
package procstat

// Sample is a reading of a process: its resident memory (the working set on Windows), its footprint (macOS's, the private
// bytes on Windows), its CPU and its threads.
type Sample struct {
	RSSKB, FootprintKB int64
	UserSec, SysSec    float64
	Threads            int
	// NonpagedKB is the system's nonpaged pool, which the kernel's requests for a connection's readiness are charged to:
	// Windows only.
	NonpagedKB int64
}
