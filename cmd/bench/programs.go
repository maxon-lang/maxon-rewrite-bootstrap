package main

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	benchDir        = "tests/bench"
	goCounterparts  = "go"
	sourceExtension = ".maxon"
	// expectPrefix starts a line of a program's expected output, as the corpus's `//>` directive does.
	expectPrefix = "//> "
)

// program is a benchmark: a Maxon source in tests/bench and its Go counterpart in tests/bench/go/<name>.
type program struct {
	name string
	// source is the path of the Maxon file, goPackage the import path of the Go counterpart, relative to the repository.
	source    string
	goPackage string
	// expected is the lines the program prints besides its timings.
	expected []string
}

// findPrograms lists the benchmarks of the repository at root, by name, and fails on a program that has no Go counterpart,
// so that a benchmark added to one language is never silently left out of the comparison.
func findPrograms(root string) ([]program, error) {
	entries, err := os.ReadDir(filepath.Join(root, benchDir))
	if err != nil {
		return nil, err
	}
	var programs []program
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != sourceExtension {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), sourceExtension)
		source := filepath.ToSlash(filepath.Join(benchDir, entry.Name()))
		goPackage := "./" + path.Join(benchDir, goCounterparts, name)
		if _, err := os.Stat(filepath.Join(root, benchDir, goCounterparts, name, "main.go")); err != nil {
			return nil, fmt.Errorf("%s has no Go counterpart: %w", source, err)
		}
		expected, err := readExpected(filepath.Join(root, source))
		if err != nil {
			return nil, err
		}
		programs = append(programs, program{name: name, source: source, goPackage: goPackage, expected: expected})
	}
	sort.Slice(programs, func(i, j int) bool { return programs[i].name < programs[j].name })
	return programs, nil
}

func readExpected(file string) ([]string, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if text, ok := strings.CutPrefix(scanner.Text(), expectPrefix); ok {
			lines = append(lines, text)
		}
	}
	return lines, scanner.Err()
}
