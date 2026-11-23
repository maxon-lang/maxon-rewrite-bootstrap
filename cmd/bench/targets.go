package main

import (
	"fmt"
	"strings"
)

// target is a system the benchmarks are built for, named as the compiler's --target is: <arch>-<os>.
type target struct {
	name   string
	goos   string
	goarch string
	// exeSuffix ends the name of an executable of the system.
	exeSuffix string
	// emulatedHere is set where this repository's development machine can run the target only under an emulator, whose
	// timings say nothing about the target's hardware.
	emulatedHere bool
}

var targets = []target{
	{name: "x64-windows", goos: "windows", goarch: "amd64", exeSuffix: ".exe"},
	{name: "x64-linux", goos: "linux", goarch: "amd64"},
	{name: "arm64-linux", goos: "linux", goarch: "arm64", emulatedHere: true},
	{name: "arm64-macos", goos: "darwin", goarch: "arm64", emulatedHere: true},
}

func findTarget(name string) (target, error) {
	var names []string
	for _, t := range targets {
		if t.name == name {
			return t, nil
		}
		names = append(names, t.name)
	}
	return target{}, fmt.Errorf("unknown target %q; the targets are %s", name, strings.Join(names, ", "))
}
