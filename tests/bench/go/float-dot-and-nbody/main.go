// Floats: the Go counterpart of tests/bench/float-dot-and-nbody.maxon, a dot product of two slices and steps of a small
// n-body simulation whose bodies are slices of coordinates, velocities and masses. A product is converted to float64
// before it is added, which is what stops arm64 from fusing the two into one instruction: a fused result is not the
// checksum the Maxon program prints.
package main

import (
	"fmt"
	"math"

	"minimaxon/tests/bench/go/phase"
)

const (
	size      = 1000
	dotRounds = 20000
	bodies    = 5
	steps     = 400000
	dt        = 0.01
	softening = 0.01
)

func dot(a, b []float64) float64 {
	sum := 0.0
	for i := range a {
		sum += float64(a[i] * b[i])
	}
	return sum
}

// state holds the coordinates, velocities and masses of the bodies, one slice for each.
type state struct {
	x, y, z, vx, vy, vz, mass []float64
}

// advance moves every body by its velocity after the pull of every other one, by the inverse square of their distance.
func (s *state) advance() {
	for i := range bodies {
		for j := i + 1; j < bodies; j++ {
			dx := s.x[i] - s.x[j]
			dy := s.y[i] - s.y[j]
			dz := s.z[i] - s.z[j]
			d2 := float64(dx*dx) + float64(dy*dy) + float64(dz*dz) + softening
			mag := dt / (d2 * math.Sqrt(d2))
			mi := s.mass[i] * mag
			mj := s.mass[j] * mag
			s.vx[i] -= float64(dx * mj)
			s.vy[i] -= float64(dy * mj)
			s.vz[i] -= float64(dz * mj)
			s.vx[j] += float64(dx * mi)
			s.vy[j] += float64(dy * mi)
			s.vz[j] += float64(dz * mi)
		}
	}
	for i := range bodies {
		s.x[i] += float64(dt * s.vx[i])
		s.y[i] += float64(dt * s.vy[i])
		s.z[i] += float64(dt * s.vz[i])
	}
}

func (s *state) energy() float64 {
	e := 0.0
	for i := range bodies {
		e += float64(0.5 * s.mass[i] * (float64(s.vx[i]*s.vx[i]) + float64(s.vy[i]*s.vy[i]) + float64(s.vz[i]*s.vz[i])))
		for j := i + 1; j < bodies; j++ {
			dx := s.x[i] - s.x[j]
			dy := s.y[i] - s.y[j]
			dz := s.z[i] - s.z[j]
			e -= s.mass[i] * s.mass[j] / math.Sqrt(float64(dx*dx)+float64(dy*dy)+float64(dz*dz)+softening)
		}
	}
	return e
}

func main() {
	t := phase.Start()
	a := make([]float64, 0)
	b := make([]float64, 0)
	for i := range size {
		a = append(a, float64(i%7)*0.5)
		b = append(b, float64(float64(i%11)*0.25)-1.0)
	}
	total := 0.0
	for range dotRounds {
		total += dot(a, b)
	}
	t.Lap("dot")
	var s state
	for i := range bodies {
		k := float64(i)
		s.x = append(s.x, k)
		s.y = append(s.y, float64(k*0.5)-1.0)
		s.z = append(s.z, 1.0-float64(k*0.25))
		s.vx = append(s.vx, 0.0)
		s.vy = append(s.vy, k*0.1)
		s.vz = append(s.vz, -0.05*k)
		s.mass = append(s.mass, 1.0+k)
	}
	for range steps {
		s.advance()
	}
	t.Lap("nbody")
	fmt.Println(phase.Float(total), phase.Float(s.energy()))
	t.Finish()
}
