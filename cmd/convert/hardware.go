package main

import (
	"fmt"
	"strings"
)

// hardwareIntrinsics are the raw intrinsics of maxon's library that drive CPU-specific instructions, which minimaxon does not
// have: its library is software only, and the same code runs on every target.
var hardwareIntrinsics = []string{"__Builtins.cpuHasSha", "__Builtins.sha256Blocks"}

// dropHardwareRegions replaces a conditional region (`#if` ... `#endif`) whose first branch calls a hardware intrinsic by its
// `#else` branch, or by nothing when it has none: the software path that maxon's code takes where the processor lacks the
// instructions is the one every minimaxon program takes.
func dropHardwareRegions(lines []sourceLine) {
	for i := range lines {
		if lines[i].removed || len(lines[i].code) == 0 || lines[i].code[0].text != "#if" {
			continue
		}
		elseAt, end := conditionalRegion(lines, i)
		firstEnd := elseAt
		if elseAt < 0 {
			firstEnd = end
		}
		first := lines[i+1 : firstEnd]
		if !namesHardwareIntrinsic(first) {
			continue
		}
		lines[i].removed = true
		lines[end].removed = true
		if elseAt < 0 {
			for k := i + 1; k < end; k++ {
				lines[k].removed = true
			}
			continue
		}
		lines[elseAt].removed = true
		for k := i + 1; k < elseAt; k++ {
			lines[k].removed = true
		}
		for k := elseAt + 1; k < end; k++ {
			lines[k].indent = strings.TrimPrefix(lines[k].indent, "\t")
		}
	}
}

// conditionalRegion is the line of the `#else` of the region that the `#if` at line start opens (-1 when it has none) and the
// line of its `#endif`, the regions inside it passed over.
func conditionalRegion(lines []sourceLine, start int) (elseAt, end int) {
	elseAt = -1
	depth := 0
	for k := start + 1; k < len(lines); k++ {
		if len(lines[k].code) == 0 {
			continue
		}
		switch lines[k].code[0].text {
		case "#if":
			depth++
		case "#else":
			if depth == 0 {
				elseAt = k
			}
		case "#endif":
			if depth == 0 {
				return elseAt, k
			}
			depth--
		}
	}
	panic(fmt.Sprintf("line %d: the #if has no #endif", lines[start].origLine))
}

func namesHardwareIntrinsic(lines []sourceLine) bool {
	for _, l := range lines {
		text := strings.ReplaceAll(tokenText(l.code), " . ", ".")
		for _, name := range hardwareIntrinsics {
			if strings.Contains(text, name) {
				return true
			}
		}
	}
	return false
}
