// Command arm64gen writes self/backend/arm64_gen.maxon, the arm64 instruction encoder that package arm64table generates
// from its instruction table. `go generate ./internal/arm64gen` runs it.
package main

//go:generate go run .

import (
	"fmt"
	"os"

	"minimaxon/internal/arm64gen/arm64table"
)

const maxonFile = "../../self/backend/arm64_gen.maxon"

func main() {
	code, err := arm64table.Generate()
	if err != nil {
		fmt.Fprintln(os.Stderr, "arm64gen:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(maxonFile, []byte(code), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "arm64gen:", err)
		os.Exit(1)
	}
}
