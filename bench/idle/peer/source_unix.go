//go:build !windows

package main

import (
	"net"
	"time"
)

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
