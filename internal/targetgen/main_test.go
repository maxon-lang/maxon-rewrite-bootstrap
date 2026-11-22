package main

import (
	"bytes"
	"os"
	"slices"
	"testing"

	"minimaxon/internal/machines"
	"minimaxon/internal/targetgen/target"
)

func TestGeneratedFilesAreCurrent(t *testing.T) {
	outs, err := generateAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outs {
		got, err := os.ReadFile(o.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), []byte(o.code)) {
			t.Errorf("%s is not what the target table generates; run `go generate ./internal/targetgen`", o.path)
		}
	}
}

// The instruction sets of the pending targets fit the machine IR's tables beside the others', which is what selecting
// their code will ask of them.
func TestPendingMachinesFitTheMachineIR(t *testing.T) {
	targets := append(slices.Clone(target.Targets), target.Pending...)
	all := append(slices.Clone(machines.All), machines.Pending...)
	if _, err := generateMachine(targets, all); err != nil {
		t.Fatal(err)
	}
}
