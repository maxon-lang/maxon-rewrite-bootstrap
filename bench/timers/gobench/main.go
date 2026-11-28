// Command gobench is the timers benchmark's Go baseline and its idle driver (bench/README.md, out/dev/timers-design.md §5).
//
//	gobench run <count> <workers> <stormMs> <churnServices> <churnTimers> <churnMs>
//	gobench idlechild <count>
//	gobench idle [-n count] [-idle d] [-label l] -- <program> [args...]
//
// run is bench/timers/timers.maxon's lanes with time.AfterFunc, Reset and Stop, on GOMAXPROCS processors, printing the
// same result lines: arms from every worker toward deadlines 1 to 2 hours out, a reset storm on random ones of them, their
// cancels, fires of count timers due at one instant timed from it to the last callback, stops of timers that another worker
// armed, and a churn of 1 ms one-shots each re-armed by its own callback. idlechild is the Go side of the idle lane, the
// protocol of `timers idle`: it says `ready`, arms count timers an hour out at `a` on its standard input, says `armed`, and
// ends at `q`. idle runs either program with the count appended and reads its memory before and after the arms and its CPU
// over an idle period.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"minimaxon/bench/procstat"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const far = time.Hour

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "run":
		run(os.Args[2:])
	case "idlechild":
		idleChild(os.Args[2:])
	case "idle":
		idle(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gobench run <count> <workers> <stormMs> <churnServices> <churnTimers> <churnMs> | gobench idlechild <count> | gobench idle [flags] -- <program> [args...]")
	os.Exit(2)
}

func atoi(s string) int {
	v, err := strconv.Atoi(s)
	check(err)
	return v
}

// counter is a padded count, one to a worker, so that the workers' callbacks do not share a cache line.
type counter struct {
	n    atomic.Int64
	last atomic.Int64
	_    [48]byte
}

// share is worker w's part of n among k workers.
func share(n, w, k int) int {
	if w < n%k {
		return n/k + 1
	}
	return n / k
}

// parallel runs f for each worker at once and gives the longest any took.
func parallel(k int, f func(w int)) time.Duration {
	took := make([]time.Duration, k)
	var wg sync.WaitGroup
	for w := range k {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			f(w)
			took[w] = time.Since(start)
		}()
	}
	wg.Wait()
	most := time.Duration(0)
	for _, t := range took {
		most = max(most, t)
	}
	return most
}

func run(args []string) {
	if len(args) != 6 {
		usage()
	}
	n, k, stormMs := atoi(args[0]), atoi(args[1]), atoi(args[2])
	churnServices, churnTimers, churnMs := atoi(args[3]), atoi(args[4]), atoi(args[5])
	timers := make([][]*time.Timer, k)
	counts := make([]counter, k)
	base := time.Now()

	// (a) arms toward far deadlines, from every worker at once.
	armNs := parallel(k, func(w int) {
		r := rand.New(rand.NewPCG(uint64(w), 17))
		m := share(n, w, k)
		ts := make([]*time.Timer, 0, m)
		c := &counts[w]
		for range m {
			d := far + time.Duration(r.Int64N(int64(far/time.Millisecond)))*time.Millisecond
			ts = append(ts, time.AfterFunc(d, func() { c.n.Add(1) }))
		}
		timers[w] = ts
	})
	fmt.Printf("result arm count=%d workers=%d ns=%d\n", n, k, armNs.Nanoseconds()/int64(n))

	// (b) a reset storm on the armed timers.
	if stormMs > 0 {
		resets := make([]int64, k)
		until := time.Now().Add(time.Duration(stormMs) * time.Millisecond)
		start := time.Now()
		parallel(k, func(w int) {
			r := rand.New(rand.NewPCG(uint64(w), 29))
			ts := timers[w]
			var done int64
			for time.Now().Before(until) {
				for range 256 {
					d := far + time.Duration(r.Int64N(int64(far/time.Millisecond)))*time.Millisecond
					ts[r.IntN(len(ts))].Reset(d)
				}
				done += 256
			}
			resets[w] = done
		})
		took := time.Since(start)
		var total int64
		for _, v := range resets {
			total += v
		}
		fmt.Printf("result reset count=%d workers=%d resets=%d perSec=%d\n", n, k, total, int64(float64(total)/took.Seconds()))
	}
	cancelNs := parallel(k, func(w int) {
		for _, t := range timers[w] {
			t.Stop()
		}
		timers[w] = nil
	})
	fmt.Printf("result cancel count=%d workers=%d ns=%d\n", n, k, cancelNs.Nanoseconds()/int64(n))

	// (a) fires: n timers due at one instant, far enough out that every arm is made before it.
	at := time.Since(base) + 50*time.Millisecond + 3*armNs
	for w := range k {
		counts[w].n.Store(0)
	}
	parallel(k, func(w int) {
		m := share(n, w, k)
		ts := make([]*time.Timer, 0, m)
		c := &counts[w]
		for range m {
			ts = append(ts, time.AfterFunc(at-time.Since(base), func() {
				c.n.Add(1)
				now := int64(time.Since(base))
				for {
					old := c.last.Load()
					if now <= old || c.last.CompareAndSwap(old, now) {
						break
					}
				}
			}))
		}
		timers[w] = ts
	})
	armedBy := time.Since(base)
	for {
		var total int64
		for w := range k {
			total += counts[w].n.Load()
		}
		if total >= int64(n) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	var last int64
	for w := range k {
		last = max(last, counts[w].last.Load())
	}
	late := max(0, (armedBy - at).Milliseconds())
	fmt.Printf("result fire count=%d workers=%d ns=%d armsPastDeadlineMs=%d\n", n, k, (last-int64(at))/int64(n), late)
	for w := range k {
		timers[w] = nil
	}

	// (e) cross-processor cancels: each worker arms its share far out, and each worker then stops the timers the one
	// before it armed, all at once.
	parallel(k, func(w int) {
		r := rand.New(rand.NewPCG(uint64(w), 31))
		m := share(n, w, k)
		ts := make([]*time.Timer, 0, m)
		c := &counts[w]
		for range m {
			d := far + time.Duration(r.Int64N(int64(far/time.Millisecond)))*time.Millisecond
			ts = append(ts, time.AfterFunc(d, func() { c.n.Add(1) }))
		}
		timers[w] = ts
	})
	xcancelNs := parallel(k, func(w int) {
		for _, t := range timers[(w+k-1)%k] {
			t.Stop()
		}
	})
	for w := range k {
		timers[w] = nil
	}
	fmt.Printf("result xcancel count=%d workers=%d ns=%d\n", n, k, xcancelNs.Nanoseconds()/int64(n))

	// (c) churn: one-shots re-armed by their own callbacks.
	if churnServices > 0 && churnTimers > 0 {
		var churning atomic.Bool
		churning.Store(true)
		cs := make([]counter, churnServices)
		all := make([][]*time.Timer, churnServices)
		for s := range churnServices {
			all[s] = make([]*time.Timer, churnTimers)
			c := &cs[s]
			for i := range churnTimers {
				slot := &all[s][i]
				*slot = time.AfterFunc(time.Millisecond, func() {
					c.n.Add(1)
					if churning.Load() {
						(*slot).Reset(time.Millisecond)
					}
				})
			}
		}
		sum := func() int64 {
			var total int64
			for s := range cs {
				total += cs[s].n.Load()
			}
			return total
		}
		time.Sleep(300 * time.Millisecond)
		before, from := sum(), time.Now()
		time.Sleep(time.Duration(churnMs) * time.Millisecond)
		after, took := sum(), time.Since(from)
		churning.Store(false)
		fires := after - before
		fmt.Printf("result churn services=%d timers=%d perSec=%d\n", churnServices, churnTimers, int64(float64(fires)/took.Seconds()))
		for s := range all {
			for _, t := range all[s] {
				t.Stop()
			}
		}
	}
}

// waitFor reads lines of r until one holds want, false at its end.
func waitFor(r *bufio.Reader, want string) bool {
	for {
		line, err := r.ReadString('\n')
		if strings.Contains(line, want) {
			return true
		}
		if err != nil {
			return false
		}
	}
}

func idleChild(args []string) {
	if len(args) != 1 {
		usage()
	}
	n := atoi(args[0])
	timers := make([]*time.Timer, 0, n)
	in := bufio.NewReader(os.Stdin)
	fmt.Println("ready")
	if !waitFor(in, "a") {
		os.Exit(1)
	}
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	start := time.Now()
	for range n {
		timers = append(timers, time.AfterFunc(far, func() {}))
	}
	took := time.Since(start)
	runtime.ReadMemStats(&m1)
	fmt.Printf("armed %d %d %d\n", took.Nanoseconds(), m1.Mallocs-m1.Frees-(m0.Mallocs-m0.Frees), int64(m1.HeapAlloc)-int64(m0.HeapAlloc))
	waitFor(in, "q")
	runtime.KeepAlive(timers)
	os.Exit(0)
}

// idle starts the program with the count appended, and reads its memory before and after its arms and its CPU over an
// idle period after them.
func idle(args []string) {
	fs := flag.NewFlagSet("idle", flag.ExitOnError)
	n := fs.Int("n", 1000000, "timers to arm")
	period := fs.Duration("idle", 5*time.Second, "the idle period over which the program's CPU is measured")
	label := fs.String("label", "", "the program's name in the result")
	check(fs.Parse(args))
	rest := fs.Args()
	if len(rest) == 0 {
		usage()
	}
	cmd := exec.Command(rest[0], append(rest[1:], strconv.Itoa(*n))...)
	stdin, err := cmd.StdinPipe()
	check(err)
	stdout, err := cmd.StdoutPipe()
	check(err)
	cmd.Stderr = os.Stderr
	check(cmd.Start())
	out := bufio.NewReader(stdout)
	if !waitFor(out, "ready") {
		fail("%s: no ready", *label)
	}
	time.Sleep(300 * time.Millisecond)
	before := measure(cmd.Process.Pid)
	_, err = io.WriteString(stdin, "a\n")
	check(err)
	line, err := out.ReadString('\n')
	check(err)
	var took, boxes, bytes int64
	if _, err := fmt.Sscanf(line, "armed %d %d %d", &took, &boxes, &bytes); err != nil {
		fail("%s: %q", *label, line)
	}
	time.Sleep(300 * time.Millisecond)
	after := measure(cmd.Process.Pid)
	time.Sleep(*period)
	idled := measure(cmd.Process.Pid)
	_, _ = io.WriteString(stdin, "q\n")
	stdin.Close()
	check(cmd.Wait())
	perTimer := func(kb int64) int64 { return kb * 1024 / int64(*n) }
	fmt.Printf("result idle label=%s count=%d armNs=%d heapBoxes=%d heapBytes=%d rssPerTimer=%d footprintPerTimer=%d idleCpuMs=%d idleSec=%.0f threads=%d\n",
		*label, *n, took/int64(*n), boxes, bytes, perTimer(after.RSSKB-before.RSSKB), perTimer(after.FootprintKB-before.FootprintKB),
		int64((idled.UserSec+idled.SysSec-after.UserSec-after.SysSec)*1000), period.Seconds(), idled.Threads)
}

func check(err error) {
	if err != nil {
		fail("%v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gobench: "+format+"\n", args...)
	os.Exit(1)
}

// sample is procstat's reading of the server, and measure takes one.
type sample = procstat.Sample

func measure(pid int) sample {
	s, err := procstat.Measure(pid)
	check(err)
	return s
}
