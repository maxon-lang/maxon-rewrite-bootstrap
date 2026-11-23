// String building: the Go counterpart of tests/bench/string-building.maxon. Go strings are immutable, so the append is a
// concatenation. Go's arithmetic wraps where Maxon's is checked.
package main

import (
	"fmt"
	"strconv"

	"minimaxon/tests/bench/go/phase"
)

const (
	items   = 300000
	modulus = 1000003
)

func main() {
	t := phase.Start()
	var checksum, total uint64
	for i := range items {
		line := "item " + strconv.Itoa(i)
		line += ", value " + strconv.Itoa(i%97)
		for at := range len(line) {
			checksum = (checksum*31 + uint64(line[at])) % modulus
		}
		total += uint64(len(line))
	}
	fmt.Println(total, checksum)
	t.Finish()
}
