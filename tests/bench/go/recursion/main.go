// A recursive function: the Go counterpart of tests/bench/recursion.maxon. Go's add wraps where Maxon's is checked.
package main

import (
	"fmt"

	"minimaxon/tests/bench/go/phase"
)

const argument = 38

func fib(n uint64) uint64 {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func main() {
	t := phase.Start()
	fmt.Println(fib(argument))
	t.Finish()
}
