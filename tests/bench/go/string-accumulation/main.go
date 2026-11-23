// String accumulation: the Go counterpart of tests/bench/string-accumulation.maxon, one string grown from pieces of 8 bytes
// to a final size by a strings.Builder, Go's way of building one, at a small, a medium and a large final size, with ASCII
// and with non-ASCII pieces. The arguments `<way> <size> <text>` run one of them alone, as a measure of its own peak memory
// takes; the way "concat" is `s += piece`, which copies the whole string at each piece, so it runs only at the small size.
package main

import (
	"fmt"
	"os"
	"strings"

	"minimaxon/tests/bench/go/phase"
)

const (
	pieceBytes   = 8
	smallBytes   = 256
	smallRounds  = 65536
	mediumBytes  = 65536
	mediumRounds = 256
	largeBytes   = 16777216
	largeRounds  = 1
	asciiPiece   = "abcdefg "
	// otherPiece is 8 bytes, one of its characters two.
	otherPiece = "héllo w"
)

type size struct {
	name          string
	bytes, rounds int
}

var sizes = []size{{"small", smallBytes, smallRounds}, {"medium", mediumBytes, mediumRounds}, {"large", largeBytes, largeRounds}}

func byBuilder(rounds, bytes int, piece string) uint64 {
	var total uint64
	for range rounds {
		var b strings.Builder
		for range bytes / pieceBytes {
			b.WriteString(piece)
		}
		s := b.String()
		total += uint64(len(s)) + uint64(s[len(s)-1])
	}
	return total
}

func byConcat(rounds, bytes int, piece string) uint64 {
	var total uint64
	for range rounds {
		var s strings.Builder
		for range bytes / pieceBytes {
			s.WriteString(piece)
		}
		total += uint64(len(s.String())) + uint64(s.String()[len(s.String())-1])
	}
	return total
}

func pieceOf(text string) string {
	if text == "other" {
		return otherPiece
	}
	return asciiPiece
}

func main() {
	if len(os.Args) == 4 {
		way, name, text := os.Args[1], os.Args[2], os.Args[3]
		for _, sz := range sizes {
			if sz.name != name {
				continue
			}
			t := phase.Start()
			var total uint64
			if way == "concat" {
				total = byConcat(sz.rounds, sz.bytes, pieceOf(text))
			} else {
				total = byBuilder(sz.rounds, sz.bytes, pieceOf(text))
			}
			t.Lap(way + " " + name + " " + text)
			fmt.Println(total)
		}
		return
	}
	t := phase.Start()
	var total uint64
	for _, sz := range sizes {
		for _, text := range []string{"ascii", "other"} {
			total += byBuilder(sz.rounds, sz.bytes, pieceOf(text))
			t.Lap(sz.name + " " + text)
		}
	}
	for _, text := range []string{"ascii", "other"} {
		_ = byConcat(smallRounds, smallBytes, pieceOf(text))
		t.Lap("concat small " + text)
	}
	fmt.Println(total)
	t.Finish()
}
