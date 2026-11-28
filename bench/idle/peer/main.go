// Command peer is the idle-connection benchmark's client and its Go baseline server (bench/README.md).
//
//	peer run [flags] -- <server> [args...]   start the server, open the connections, measure, end it
//	peer goserver <port> <connections>      the Go baseline: a goroutine per connection doing conn.Read
//
// The server, minimaxon's (bench/idle/server.maxon) or the baseline, is started with its port and count appended to its
// arguments. It prints "listening <port> <pid>", accepts a control connection, then the connections, writing "a" on the
// control connection after each ackEvery accepts and, once all are in, "r <nanoseconds> <boxes> <bytes>\n": how long the
// accepts took, and what they left on the heap by the server's own counters. It exits at "q". The peer keeps fewer than
// the listen backlog (128) of its connects unaccepted, measures the server's resident memory before and after, its CPU
// over an idle period, and the round trip of 1-byte echoes on a random sample of the connections while the rest stay idle.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"minimaxon/bench/procstat"
	"net"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// ackEvery is how many accepts the server answers with one "a" on the control connection.
const ackEvery = 64

// maxUnaccepted keeps the connects in flight below the server's listen backlog of 128 (runtime/socket.maxon).
const maxUnaccepted = 120

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: peer run [flags] -- <server> [args...] | peer goserver <port> <connections>")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		run(os.Args[2:])
	case "goserver":
		goServer(os.Args[2:])
	default:
		fmt.Fprintln(os.Stderr, "peer: unknown command", os.Args[1])
		os.Exit(2)
	}
}

// goServer is the baseline: the same protocol, each connection a goroutine blocked in conn.Read that echoes what it reads.
func goServer(args []string) {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: peer goserver <port> <connections>")
		os.Exit(2)
	}
	port := args[0]
	n, err := strconv.Atoi(args[1])
	check(err)
	ln, err := net.Listen("tcp4", "127.0.0.1:"+port)
	check(err)
	fmt.Printf("listening %d %d\n", ln.Addr().(*net.TCPAddr).Port, os.Getpid())
	control, err := ln.Accept()
	check(err)
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	var started time.Time
	for i := range n {
		c, err := ln.Accept()
		check(err)
		if i == 0 {
			started = time.Now()
		}
		go func(c net.Conn) {
			buf := make([]byte, 16)
			for {
				k, err := c.Read(buf)
				if err != nil {
					c.Close()
					return
				}
				if _, err := c.Write(buf[:k]); err != nil {
					c.Close()
					return
				}
			}
		}(c)
		if (i+1)%ackEvery == 0 {
			_, err := control.Write([]byte("a"))
			check(err)
		}
	}
	took := time.Since(started).Nanoseconds()
	runtime.ReadMemStats(&m1)
	live := int64(m1.Mallocs-m1.Frees) - int64(m0.Mallocs-m0.Frees)
	bytes := int64(m1.HeapAlloc+m1.StackInuse) - int64(m0.HeapAlloc+m0.StackInuse)
	fmt.Fprintf(control, "r %d %d %d\n", took, live, bytes)
	buf := make([]byte, 16)
	for {
		k, err := control.Read(buf)
		if err != nil || strings.Contains(string(buf[:k]), "q") {
			os.Exit(0)
		}
	}
}

func run(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	n := fs.Int("n", 16000, "connections")
	port := fs.Int("port", 47001, "the server's port on 127.0.0.1")
	wake := fs.Int("wake", 10000, "connections in the wake sample")
	idle := fs.Duration("idle", 60*time.Second, "the idle period over which the server's CPU is measured")
	label := fs.String("label", "server", "the name of the server in the report")
	check(fs.Parse(args))
	cmdline := fs.Args()
	if len(cmdline) == 0 {
		fmt.Fprintln(os.Stderr, "peer run: no server command")
		os.Exit(2)
	}
	cmd := exec.Command(cmdline[0], append(cmdline[1:], strconv.Itoa(*port), strconv.Itoa(*n))...)
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	check(err)
	check(cmd.Start())
	child = cmd.Process
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	lines := bufio.NewReader(out)
	first, err := lines.ReadString('\n')
	check(err)
	go io.Copy(os.Stdout, lines)
	var listenPort, pid int
	if _, err := fmt.Sscanf(first, "listening %d %d", &listenPort, &pid); err != nil {
		fail("the server's first line: %q", first)
	}
	control, err := net.Dial("tcp4", fmt.Sprintf("127.0.0.1:%d", listenPort))
	check(err)
	time.Sleep(time.Second)
	before := measure(pid)

	var acked atomic.Int64
	ready := make(chan [3]int64, 1)
	go func() {
		r := bufio.NewReader(control)
		for {
			b, err := r.ReadByte()
			if err != nil {
				fail("the control connection: %v", err)
			}
			switch b {
			case 'a':
				acked.Add(1)
			case 'r':
				line, err := r.ReadString('\n')
				check(err)
				var r [3]int64
				if _, err := fmt.Sscanf(line, "%d %d %d", &r[0], &r[1], &r[2]); err != nil {
					fail("the server's ready line: %q", line)
				}
				ready <- r
				return
			}
		}
	}()
	conns := make([]net.Conn, 0, *n)
	retries := 0
	setupStart := time.Now()
	for len(conns) < *n {
		for int64(len(conns))-acked.Load()*ackEvery >= maxUnaccepted {
			runtime.Gosched()
		}
		dialer := net.Dialer{Timeout: 5 * time.Second, LocalAddr: sourceOf(len(conns))}
		c, err := dialer.Dial("tcp4", fmt.Sprintf("127.0.0.1:%d", listenPort))
		if err != nil {
			retries++
			if retries > 1000 {
				fail("connect %d: %v", len(conns), err)
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		conns = append(conns, c)
	}
	report := <-ready
	serverNs := report[0]
	setupWall := time.Since(setupStart)
	time.Sleep(3 * time.Second)
	after := measure(pid)
	time.Sleep(*idle)
	idled := measure(pid)

	order := rand.Perm(len(conns))
	k := min(*wake, len(conns))
	rtts := make([]time.Duration, 0, k)
	one, back := []byte{'x'}, make([]byte, 1)
	for _, i := range order[:k] {
		c := conns[i]
		c.SetDeadline(time.Now().Add(5 * time.Second))
		t := stamp()
		if _, err := c.Write(one); err != nil {
			fail("a wake write on connection %d: %v", i, err)
		}
		if _, err := io.ReadFull(c, back); err != nil {
			fail("a wake read on connection %d: %v", i, err)
		}
		rtts = append(rtts, elapsed(t))
	}
	time.Sleep(time.Second)
	woken := measure(pid)
	// The server ends first, so the TIME_WAIT of each connection is on its side and the peer's ephemeral ports are free
	// for the next run at once (macOS keeps one for 30 s).
	control.Write([]byte("q"))
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		<-exited
	}
	// A reset, not a FIN, so that no connection whose FIN from the server is still on its way leaves the peer's port in
	// TIME_WAIT.
	for _, c := range conns {
		c.(*net.TCPConn).SetLinger(0)
		c.Close()
	}
	control.Close()

	slices.Sort(rtts)
	q := func(p float64) time.Duration { return rtts[min(len(rtts)-1, int(p*float64(len(rtts))))] }
	perConn := func(a, b int64) float64 { return float64(b-a) * 1024 / float64(*n) }
	fmt.Printf("RESULT label=%s os=%s n=%d retries=%d\n", *label, runtime.GOOS, *n, retries)
	fmt.Printf("  rss KB: before %d, after accepts %d, after idle %d, after wakes %d\n", before.RSSKB, after.RSSKB, idled.RSSKB, woken.RSSKB)
	fmt.Printf("  bytes/conn (rss): %.0f after accepts, %.0f after idle, %.0f after wakes\n", perConn(before.RSSKB, after.RSSKB), perConn(before.RSSKB, idled.RSSKB), perConn(before.RSSKB, woken.RSSKB))
	if before.FootprintKB > 0 {
		fmt.Printf("  footprint KB: before %d, after accepts %d, after wakes %d; bytes/conn %.0f after accepts, %.0f after wakes\n", before.FootprintKB, after.FootprintKB, woken.FootprintKB, perConn(before.FootprintKB, after.FootprintKB), perConn(before.FootprintKB, woken.FootprintKB))
	}
	if before.NonpagedKB > 0 {
		fmt.Printf("  system nonpaged pool KB: before %d, after accepts %d, after wakes %d; bytes/conn %.0f after accepts, %.0f after wakes\n", before.NonpagedKB, after.NonpagedKB, woken.NonpagedKB, perConn(before.NonpagedKB, after.NonpagedKB), perConn(before.NonpagedKB, woken.NonpagedKB))
	}
	fmt.Printf("  heap: %.2f boxes live/conn, %.0f bytes/conn (minimaxon: bytes asked by the accepts; Go: heap live + stacks)\n", float64(report[1])/float64(*n), float64(report[2])/float64(*n))
	fmt.Printf("  OS threads: before %d, after accepts %d, after wakes %d\n", before.Threads, after.Threads, woken.Threads)
	fmt.Printf("  setup: %.0f conn/s accepted (server, %.3f s), %.0f conn/s end to end (peer, %.3f s)\n", float64(*n)/(float64(serverNs)/1e9), float64(serverNs)/1e9, float64(*n)/setupWall.Seconds(), setupWall.Seconds())
	fmt.Printf("  idle %v CPU: user %.2f s, sys %.2f s\n", *idle, idled.UserSec-after.UserSec, idled.SysSec-after.SysSec)
	fmt.Printf("  wake sample %d of %d: p50 %v, p99 %v, max %v\n", k, *n, q(0.50), q(0.99), rtts[len(rtts)-1])
}

func check(err error) {
	if err != nil {
		fail("%v", err)
	}
}

// child is the server the peer started, ended by a failure so that nothing is left running.
var child *os.Process

func fail(format string, args ...any) {
	if child != nil {
		child.Kill()
	}
	fmt.Fprintf(os.Stderr, "peer: "+format+"\n", args...)
	os.Exit(1)
}

// sample is procstat's reading of the server, and measure takes one.
type sample = procstat.Sample

func measure(pid int) sample {
	s, err := procstat.Measure(pid)
	check(err)
	return s
}
