// Records in a slice and a union of shapes matched in a loop: the Go counterpart of tests/bench/records-and-unions.maxon.
// Go has no union, so the shapes are the interface and type switch that Go programs write for one; each shape is boxed in
// the interface where Maxon's union holds its payload inline, so this is not like for like. Go's arithmetic wraps where
// Maxon's is checked.
package main

import (
	"fmt"

	"minimaxon/tests/bench/go/phase"
)

const (
	count   = 1000
	rounds  = 8000
	modulus = 1000003
)

type point struct {
	x, y uint64
}

type shape any

type dot struct{ at point }

type box struct {
	corner point
	width  uint64
}

type line struct{ first, last point }

func weight(s shape) uint64 {
	switch s := s.(type) {
	case dot:
		return s.at.x + s.at.y
	case box:
		return (s.corner.x + s.corner.y) * s.width
	case line:
		return s.first.x*s.last.y + s.first.y*s.last.x
	}
	panic(fmt.Sprintf("weight of an unknown shape %T", s))
}

func main() {
	t := phase.Start()
	shapes := make([]shape, 0)
	for i := range uint64(count) {
		p := point{x: i, y: (i * 7) % modulus}
		switch i % 3 {
		case 0:
			shapes = append(shapes, dot{p})
		case 1:
			shapes = append(shapes, box{p, i % 17})
		default:
			shapes = append(shapes, line{p, point{i + 1, i % 11}})
		}
	}
	t.Lap("build")
	var checksum uint64
	for round := range uint64(rounds) {
		for i := range count {
			checksum = (checksum + weight(shapes[i]) + round) % modulus
		}
	}
	t.Lap("match")
	fmt.Println(checksum)
	t.Finish()
}
