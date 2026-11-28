package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// check runs every check AGENTS.md requires, fixing what can be fixed mechanically first (formatting, go fix's modernizers
// and the generated files), so a slip there never costs a second run, and stops at the first that fails. The seed builds
// self/ into bin/, and the suites test that compiler. Whether a compiler reproduces itself is checked only where a seed is
// renewed (gate seed), and by a host-only gate on Windows. Go builds and tests only the generators and the tools.
func check(h host, testArgs []string) error {
	compiler := h.compiler("minimaxon")
	steps := []step{
		// Only this checkout's Go: gofmt walks dot directories too, and .claude/worktrees holds other checkouts that
		// agents are editing.
		{"format", func() error {
			return quiet(nil, "gofmt", "-w", "cmd", "internal", filepath.Join("tests", "bench", "go"))
		}},
		{"go fix", func() error { return quiet(nil, "go", "fix", "./...") }},
		{"vet", func() error { return quiet(nil, "go", "vet", "./...") }},
		{"generators", checkGenerated},
		{"compiler from the seed", func() error { return buildCompiler(h, bySeed) }},
		// Needs maxon's checkout, which the gate cannot assume (maxonStdlibVariable).
		{"library regenerates", func() error { return checkStdlibIfMaxon(h) }},
	}
	if err := runSteps(steps); err != nil {
		return err
	}
	if err := suites(h, compiler, testArgs); err != nil {
		return err
	}
	// A seed renewal is the one place that proves a compiler reproduces itself, and a host-only gate on Windows has no renewal
	// to run it: the seed builds C1, C1 builds C2 and C2 builds C3, the same bytes as C2.
	if h.hostOnly && h.windows() {
		return runSteps([]step{{"compiler reproduces itself", func() error {
			return chain(abs(h.seed), h.compiler("c1"), h.compiler("c2"), h.compiler("c3"), nil)
		}}})
	}
	return nil
}

// generatedRoots are where the generated files are, each named *_gen.*.
var generatedRoots = []string{"self", "runtime", "cmd"}

// checkGenerated runs the generators and fails when a generated file changed, hashing them before and after so that a stale
// one fails the check even where there is no git to show it.
func checkGenerated() error {
	before, err := generatedHashes()
	if err != nil {
		return err
	}
	if err := quiet(nil, "go", "generate", "./internal/..."); err != nil {
		return err
	}
	after, err := generatedHashes()
	if err != nil {
		return err
	}
	var changed []string
	for path, sum := range after {
		if before[path] != sum {
			changed = append(changed, path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			changed = append(changed, path)
		}
	}
	if len(changed) == 0 {
		return nil
	}
	slices.Sort(changed)
	return fmt.Errorf("generated files were stale and are regenerated now; review and commit them:\n  %s", strings.Join(changed, "\n  "))
}

func generatedHashes() (map[string][32]byte, error) {
	sums := map[string][32]byte{}
	for _, root := range generatedRoots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.Contains(d.Name(), "_gen.") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sums[path] = sha256.Sum256(data)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return sums, nil
}

// abs is a path from the repository's root made absolute, so that a command run in another directory finds it.
func abs(path string) string {
	full, err := filepath.Abs(path)
	if err != nil {
		panic(errors.New("gate: the working directory has no absolute path"))
	}
	return full
}
