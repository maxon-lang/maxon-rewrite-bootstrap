//go:build !windows

package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// measure reads the server's resident memory, CPU and threads: RssAnon and /proc's ticks on Linux, ps and footprint on macOS.
func measure(pid int) sample {
	var s sample
	if runtime.GOOS == "linux" {
		status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		check(err)
		for line := range strings.SplitSeq(string(status), "\n") {
			f := strings.Fields(line)
			if len(f) >= 2 && f[0] == "RssAnon:" {
				s.rssKB, _ = strconv.ParseInt(f[1], 10, 64)
			}
			if len(f) >= 2 && f[0] == "Threads:" {
				s.threads, _ = strconv.Atoi(f[1])
			}
		}
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		check(err)
		f := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+2:]))
		const ticks = 100 // USER_HZ
		u, _ := strconv.ParseFloat(f[11], 64)
		k, _ := strconv.ParseFloat(f[12], 64)
		s.userSec, s.sysSec = u/ticks, k/ticks
		return s
	}
	p := strconv.Itoa(pid)
	out, err := exec.Command("ps", "-o", "rss=,utime=,stime=", "-p", p).Output()
	check(err)
	f := strings.Fields(string(out))
	if len(f) != 3 {
		fail("ps: %q", out)
	}
	s.rssKB, _ = strconv.ParseInt(f[0], 10, 64)
	s.userSec, s.sysSec = cpuTime(f[1]), cpuTime(f[2])
	threads, err := exec.Command("ps", "-M", "-p", p).Output()
	check(err)
	s.threads = strings.Count(string(threads), "\n") - 1
	if fp, err := exec.Command("footprint", p).Output(); err == nil {
		text := string(fp)
		if _, after, ok := strings.Cut(text, "Footprint: "); ok {
			var v float64
			var unit string
			fmt.Sscanf(after, "%f %s", &v, &unit)
			switch unit {
			case "MB":
				v *= 1024
			case "GB":
				v *= 1024 * 1024
			}
			s.footprintKB = int64(v)
		}
	}
	return s
}

// cpuTime reads ps's [[hh:]mm:]ss.cc.
func cpuTime(t string) float64 {
	var total float64
	for part := range strings.SplitSeq(t, ":") {
		v, _ := strconv.ParseFloat(part, 64)
		total = total*60 + v
	}
	return total
}

// sourceOf is the local address of the peer's connection number i: any, as the system chooses it.
func sourceOf(i int) net.Addr {
	return nil
}

// stamp is a reading of the clock, and elapsed the time since one: Go's monotonic clock, as precise as the system's.
func stamp() time.Time {
	return time.Now()
}

func elapsed(since time.Time) time.Duration {
	return time.Since(since)
}
