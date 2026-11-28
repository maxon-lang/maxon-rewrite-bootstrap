package main

import (
	"strings"
	"testing"
)

// An x64 host never reruns arm64's behaviours, which it could run only under qemu (an arm64 host's are its native lane's).
func TestBehavioursNeverRunUnderQemu(t *testing.T) {
	for _, target := range []string{"x64-windows", "x64-linux"} {
		why, err := behavioursRunTarget(host{target: target}, "arm64-linux")
		if err != nil || !strings.Contains(why, "qemu") {
			t.Errorf("%s host: arm64-linux's behaviours gave %q, %v; want a reason naming qemu", target, why, err)
		}
	}
}
