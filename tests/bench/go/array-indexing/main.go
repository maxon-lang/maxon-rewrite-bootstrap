// Array indexing: the Go counterpart of tests/bench/array-indexing.maxon. Go's sum wraps where Maxon's is checked.
package main

import (
	"fmt"
	"strconv"
	"strings"

	"minimaxon/tests/bench/go/phase"
)

const (
	size         = 10000
	rounds       = 20000
	spread       = 1000
	highestDigit = '4'
)

func main() {
	t := phase.Start()
	a := make([]uint64, 0)
	var text strings.Builder
	for i := range size {
		a = append(a, uint64(i%spread))
		text.WriteString(strconv.Itoa(i % 10))
	}
	scanned := text.String()
	t.Lap("fill")
	var sum, high uint64
	for range rounds {
		for i := range a {
			sum += a[i]
		}
		for at := range len(scanned) {
			if scanned[at] > highestDigit {
				high++
			}
		}
	}
	t.Lap("rounds")
	fmt.Printf("%d %d\n", sum, high)
	t.Finish()
}
