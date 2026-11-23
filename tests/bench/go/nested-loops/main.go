// Nested loops over an array: the Go counterpart of tests/bench/nested-loops.maxon. Go's arithmetic wraps where Maxon's is
// checked.
package main

import (
	"fmt"

	"minimaxon/tests/bench/go/phase"
)

const (
	size    = 2000
	rounds  = 1000
	modulus = 1000003
	// Each cell adds itself to reach cells, stride cells apart.
	reach  = 8
	stride = 251
)

func main() {
	t := phase.Start()
	cells := make([]uint64, size)
	for i := range cells {
		cells[i] = 1
	}
	for round := range uint64(rounds) {
		for i := 1; i < size; i++ {
			previous := cells[i-1]
			cells[i] = (previous*31 + cells[i] + round) % modulus
		}
		for i := range size {
			for j := range reach {
				at := (i + j*stride) % size
				cells[at] = (cells[at] + cells[i]) % modulus
			}
		}
	}
	var checksum uint64
	for i := range size {
		checksum = (checksum*7 + cells[i]) % modulus
	}
	fmt.Println(checksum)
	t.Finish()
}
