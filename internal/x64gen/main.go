// Command x64gen writes self/backend/x64_gen.maxon, the x64 instruction encoder that package x64table generates from its
// instruction table. `go generate ./internal/x64gen` runs it.
package main

//go:generate go run .

import (
	"fmt"
	"os"

	"minimaxon/internal/x64gen/x64table"
)

const maxonFile = "../../self/backend/x64_gen.maxon"

func main() {
	code, err := x64table.Generate()
	if err != nil {
		fmt.Fprintln(os.Stderr, "x64gen:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(maxonFile, []byte(code), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "x64gen:", err)
		os.Exit(1)
	}
}
