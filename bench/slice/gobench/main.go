// Command gobench is the baseline of bench/slice: a new slice made and the whole array copied into it, as Array.slice does, of a 64 KiB byte
// slice and of 8192 ints.
package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

const spanBytes = 65536

const spanWords = 8192

var sink []byte

var sinkWords []int

func lane(name string, rounds int, started time.Time, sum int) {
	perRound := time.Since(started).Nanoseconds() / int64(rounds)
	fmt.Printf("result %s %d ns %d ps/byte  (sum %d)\n", name, perRound, perRound*1000/spanBytes, sum)
}

func main() {
	rounds := 100000
	if len(os.Args) > 1 {
		rounds, _ = strconv.Atoi(os.Args[1])
	}
	bytes := make([]byte, spanBytes)
	for i := range bytes {
		bytes[i] = byte(i % 251)
	}
	words := make([]int, spanWords)
	for i := range words {
		words[i] = i
	}
	sum, started := 0, time.Now()
	for r := 0; r < rounds; r++ {
		c := make([]byte, len(bytes))
		copy(c, bytes)
		sink = c
		sum += int(c[r%spanBytes])
	}
	lane("slice-bytes-64k", rounds, started, sum)
	sum, started = 0, time.Now()
	for r := 0; r < rounds; r++ {
		c := make([]int, len(words))
		copy(c, words)
		sinkWords = c
		sum += c[r%spanWords]
	}
	lane("slice-words-64k", rounds, started, sum)
}
