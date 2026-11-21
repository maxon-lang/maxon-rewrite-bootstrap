package main

import (
	"fmt"
	"strings"
)

// writer collects generated source.
type writer struct{ b strings.Builder }

func (w *writer) p(format string, args ...any) { fmt.Fprintf(&w.b, format, args...) }

func failOperand(o op) (operand, bool) {
	for _, x := range o.Operands {
		if x.Kind == kFail {
			return x, true
		}
	}
	return operand{}, false
}

func article(name string) string {
	if strings.ContainsRune("AEIOU", rune(name[0])) {
		return "n"
	}
	return ""
}
