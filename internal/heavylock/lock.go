// Package heavylock is the machine-wide lock of the heavy commands.
package heavylock

import (
	"fmt"
	"os"
	"strconv"
)

// The machine-wide lock of the heavy commands (those of cmd/gate, cmd/selfab, and selftime.ps1 through lock.ps1): one
// gate or build runs on a machine at a time, in whatever checkout, because each takes every core and a second one makes the
// timeouts of the tests fail and the machine unusable. A process that runs under one that holds it (heavyOwnerVariable names
// the holder, which its children inherit) does not wait for it, and a lock whose holder died is taken over.
const heavyOwnerVariable = "MINIMAXON_HEAVY_OWNER"

const waitingLine = "waiting for another gate/build on this machine"

// Hold takes the lock, waiting for it when another process holds it, and gives the function that lets it go.
func Hold() (func(), error) {
	if owner, err := strconv.Atoi(os.Getenv(heavyOwnerVariable)); err == nil && owner != 0 && processAlive(owner) {
		return func() {}, nil
	}
	release, err := takeHeavyLock(func() { fmt.Println(waitingLine) })
	if err != nil {
		return nil, err
	}
	os.Setenv(heavyOwnerVariable, strconv.Itoa(os.Getpid()))
	return func() {
		os.Unsetenv(heavyOwnerVariable)
		release()
	}, nil
}
