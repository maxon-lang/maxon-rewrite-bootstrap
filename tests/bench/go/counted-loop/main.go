// A counted integer loop: the Go counterpart of tests/bench/counted-loop.maxon. Go's add wraps where Maxon's is checked.
package main

import (
	"fmt"

	"minimaxon/tests/bench/go/phase"
)

const (
	modulus    = 1000000007
	iterations = 100000000
)

func main() {
	t := phase.Start()
	var sum uint64
	for i := range uint64(iterations) {
		sum = (sum + (i ^ (i >> 3))) % modulus
	}
	fmt.Println(sum)
	t.Finish()
}
