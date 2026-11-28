//go:build windows

package main

import (
	"net"
	"syscall"
	"time"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")

// The peer opens each connection from a port of its own below the dynamic range, from one of the addresses of 127.0.0.0/8,
// which Windows routes to the loopback: the system gives a bind of port 0 one of the 16384 ports of its dynamic range, less
// those it holds, whatever the address, which fails a run of 16000 connections (WSAENOBUFS) before its end. Each address
// has connectionsPerSource ports from firstSourcePort.
const (
	connectionsPerSource = 8000
	firstSourcePort      = 20000
)

// sourceOf is the local address of the peer's connection number i.
func sourceOf(i int) net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, byte(2+i/connectionsPerSource)), Port: firstSourcePort + i%connectionsPerSource}
}

var (
	queryPerformanceCounter   = kernel32.NewProc("QueryPerformanceCounter")
	queryPerformanceFrequency = kernel32.NewProc("QueryPerformanceFrequency")
	counterTicksPerSecond     = counterFrequency()
)

func counterFrequency() int64 {
	var frequency int64
	queryPerformanceFrequency.Call(uintptr(unsafe.Pointer(&frequency)))
	return frequency
}

// stamp is a reading of the performance counter, and elapsed the time since one: Go's monotonic clock on Windows ticks with
// the system's timer, 0.5 ms at best, which is longer than a round trip of the loopback.
func stamp() int64 {
	var ticks int64
	queryPerformanceCounter.Call(uintptr(unsafe.Pointer(&ticks)))
	return ticks
}

func elapsed(since int64) time.Duration {
	return time.Duration((stamp() - since) * int64(time.Second) / counterTicksPerSecond)
}
