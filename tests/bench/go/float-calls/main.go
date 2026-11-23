// Calls that pass and return floats: the Go counterpart of tests/bench/float-calls.maxon. Go's compiler may inline mix and
// devirtualize the call through the function value. A product is converted to float64 before it is added, which is what
// stops arm64 from fusing the two into one instruction: a fused result is not the checksum the Maxon program prints.
package main

import (
	"fmt"

	"minimaxon/tests/bench/go/phase"
)

const rounds = 20000000

func mix(a, b float64) float64 {
	return float64(a*0.75) + float64(b*0.25)
}

func direct() float64 {
	x, y := 1.0, 0.5
	for range rounds {
		x = mix(x, y)
		y = mix(y+1.0, x)
	}
	return x + y
}

func through(f func(float64, float64) float64) float64 {
	x, y := 1.0, 0.5
	for range rounds {
		x = f(x, y)
		y = f(y+1.0, x)
	}
	return x + y
}

func main() {
	t := phase.Start()
	called := direct()
	t.Lap("direct")
	passed := through(mix)
	t.Lap("through")
	fmt.Println(phase.Float(called), phase.Float(passed))
	t.Finish()
}
