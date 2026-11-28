package main

import (
	"fmt"
	"strings"
)

// hardwareIntrinsics are the raw intrinsics of maxon's library that drive CPU-specific instructions which minimaxon's runtime
// does not have yet: SHA-256's. AES and GCM's are runtime/aes.maxon's, so their regions convert as maxon wrote them.
var hardwareIntrinsics = []string{"__Builtins.cpuHasSha", "__Builtins.sha256Blocks"}

// hardwareDeclarations are the names that a hardware-only file declares and other files name; none is left out now.
var hardwareDeclarations = []string{}

// dropHardwareRegions replaces a conditional region (`#if` ... `#endif`) whose first branch calls a hardware intrinsic, or names
// a declaration that only such a branch declares, by its `#else` branch, or by nothing when it has none: the software path
// that maxon's code takes where the processor lacks the instructions is the one every minimaxon program takes. A region that
// is dropped makes what it declares (a function, a union case) hardware-only too, so the file is walked until no more drop.
func dropHardwareRegions(lines []sourceLine) {
	names := map[string]bool{}
	for _, name := range hardwareDeclarations {
		names[name] = true
	}
	for dropped := true; dropped; {
		dropped = false
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
			if !namesHardware(first, names) {
				continue
			}
			dropped = true
			for _, l := range first {
				if name := hardwareDeclaredName(l.code); name != "" {
					names[name] = true
				}
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

func namesHardware(lines []sourceLine, names map[string]bool) bool {
	for _, l := range lines {
		if l.removed {
			continue
		}
		text := strings.ReplaceAll(tokenText(l.code), " . ", ".")
		for _, name := range hardwareIntrinsics {
			if strings.Contains(text, name) {
				return true
			}
		}
		for _, t := range l.code {
			if t.kind == tokIdent && names[t.text] {
				return true
			}
		}
	}
	return false
}

// hardwareDeclaredName is the name a line of a dropped region declares: a declaration's, or a union case's (`name` or
// `name(fields)` alone on its line); "" for any other line.
func hardwareDeclaredName(code []token) string {
	k := skipModifiers(code)
	if k < len(code) {
		switch code[k].text {
		case "function", "let", "var", "type", "typealias", "union", "enum":
			return declaredName(code)
		}
	}
	if k != 0 || len(code) == 0 || code[0].kind != tokIdent {
		return ""
	}
	if len(code) == 1 || code[1].text == "(" && code[len(code)-1].text == ")" && !hasToken(code, "then") {
		return code[0].text
	}
	return ""
}

