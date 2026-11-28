//go:build !windows

package procstat

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// Measure reads a process's resident memory, CPU and threads: RssAnon and /proc's ticks on Linux, ps and footprint on macOS.
func Measure(pid int) (Sample, error) {
	var s Sample
	if runtime.GOOS == "linux" {
		status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if err != nil {
			return s, err
		}
		for line := range strings.SplitSeq(string(status), "\n") {
			f := strings.Fields(line)
			if len(f) >= 2 && f[0] == "RssAnon:" {
				s.RSSKB, _ = strconv.ParseInt(f[1], 10, 64)
			}
			if len(f) >= 2 && f[0] == "Threads:" {
				s.Threads, _ = strconv.Atoi(f[1])
			}
		}
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			return s, err
		}
		f := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+2:]))
		const ticks = 100 // USER_HZ
		u, _ := strconv.ParseFloat(f[11], 64)
		k, _ := strconv.ParseFloat(f[12], 64)
		s.UserSec, s.SysSec = u/ticks, k/ticks
		return s, nil
	}
	p := strconv.Itoa(pid)
	out, err := exec.Command("ps", "-o", "rss=,utime=,stime=", "-p", p).Output()
	if err != nil {
		return s, err
	}
	f := strings.Fields(string(out))
	if len(f) != 3 {
		return s, fmt.Errorf("ps: %q", out)
	}
	s.RSSKB, _ = strconv.ParseInt(f[0], 10, 64)
	s.UserSec, s.SysSec = cpuTime(f[1]), cpuTime(f[2])
	threads, err := exec.Command("ps", "-M", "-p", p).Output()
	if err != nil {
		return s, err
	}
	s.Threads = strings.Count(string(threads), "\n") - 1
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
			s.FootprintKB = int64(v)
		}
	}
	return s, nil
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
