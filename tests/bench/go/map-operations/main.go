// The built-in map: the Go counterpart of tests/bench/map-operations.maxon, a million inserts, hits, misses and removes,
// and a walk of every entry, by integer keys and by string keys.
package main

import (
	"fmt"
	"strconv"

	"minimaxon/tests/bench/go/phase"
)

const (
	size = 1000000
	// spread scatters the integer keys, so they are not inserted in the order of their hashes.
	spread = 7919
)

func integerKeys(t *phase.Timer) uint64 {
	m := map[uint64]uint64{}
	for i := range uint64(size) {
		m[i*spread] = i
	}
	t.Lap("int insert")
	var sum uint64
	for i := range uint64(size) {
		sum += m[i*spread]
	}
	t.Lap("int hit")
	var misses uint64
	for i := range uint64(size) {
		if _, ok := m[i*spread+1]; !ok {
			misses++
		}
	}
	t.Lap("int miss")
	for k, v := range m {
		sum += v + k%2
	}
	t.Lap("int iterate")
	var removed uint64
	for i := range uint64(size) {
		before := len(m)
		delete(m, i*spread)
		if len(m) < before {
			removed++
		}
	}
	t.Lap("int remove")
	return sum + misses + removed + uint64(len(m))
}

func stringKeys(t *phase.Timer) uint64 {
	keys := make([]string, 0)
	absent := make([]string, 0)
	for i := range size {
		keys = append(keys, "key"+strconv.Itoa(i))
		absent = append(absent, "none"+strconv.Itoa(i))
	}
	t.Lap("string build")
	m := map[string]uint64{}
	for i := range size {
		m[keys[i]] = uint64(i)
	}
	t.Lap("string insert")
	built := map[string]uint64{}
	for i := range size {
		built["key"+strconv.Itoa(i)] = uint64(i)
	}
	t.Lap("string insert built")
	var sum uint64
	for i := range size {
		sum += m[keys[i]]
	}
	t.Lap("string hit")
	var misses uint64
	for i := range size {
		if _, ok := m[absent[i]]; !ok {
			misses++
		}
	}
	t.Lap("string miss")
	for k, v := range m {
		sum += v + uint64(len(k))
	}
	t.Lap("string iterate")
	var removed uint64
	for i := range size {
		before := len(m)
		delete(m, keys[i])
		if len(m) < before {
			removed++
		}
	}
	t.Lap("string remove")
	return sum + misses + removed + uint64(len(m)) + uint64(len(built))
}

func main() {
	t := phase.Start()
	integers := integerKeys(t)
	texts := stringKeys(t)
	fmt.Println(integers, texts)
	t.Finish()
}
