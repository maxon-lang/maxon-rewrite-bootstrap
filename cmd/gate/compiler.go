package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Every command of the gate uses one compiler, bin/minimaxon: the gate builds it from the seed, build builds it through C1,
// and a regeneration builds it first when it is not of the sources as they are, so that nothing is ever made with a
// compiler of other code. compilerStamp, beside it, says how it was built and from what; a build that fails or whose
// sources changed while it ran leaves none, and a compiler without one is not current.
var compilerStamp = filepath.Join("bin", "minimaxon.stamp")

// compilerSources are what the compiler is built from: its own source, the runtime and the library that a build of it
// reads, and the seed.
func compilerSources(h host) []string {
	return []string{"self", "runtime", "stdlib", h.seed}
}

// A buildMethod is how bin/minimaxon is built, which its stamp records.
type buildMethod string

const (
	// bySeed is the seed building self/ once, the gate's compiler.
	bySeed buildMethod = "seed"
	// throughC1 is the seed building C1 and C1 building self/, so that the compiler carries today's code generator.
	throughC1 buildMethod = "c1"
)

func compilerStampOf(h host, method buildMethod) (string, error) {
	sources, err := pathsDigest(compilerSources(h), nil)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("built %s\nsources %s", method, sources), nil
}

// compilerBuiltBy is whether bin/minimaxon is there and was built from the sources as they are by one of the methods.
func compilerBuiltBy(h host, methods ...buildMethod) (bool, error) {
	if !isFile(h.compiler("minimaxon")) {
		return false, nil
	}
	for _, m := range methods {
		stamp, err := compilerStampOf(h, m)
		if err != nil {
			return false, err
		}
		if stampHolds(compilerStamp, stamp) {
			return true, nil
		}
	}
	return false, nil
}

// buildCompiler builds bin/minimaxon by the method and stamps it.
func buildCompiler(h host, method buildMethod) error {
	if err := os.MkdirAll("bin", 0o755); err != nil {
		return err
	}
	before, err := compilerStampOf(h, method)
	if err != nil {
		return err
	}
	os.Remove(compilerStamp)
	compiler := h.compiler("minimaxon")
	switch method {
	case bySeed:
		err = quiet(nil, abs(h.seed), "build", "self", "-o", compiler)
	case throughC1:
		c1 := h.compiler("c1")
		if err = run(nil, h.seed, "build", "self", "-o", c1); err == nil {
			err = run(nil, c1, "build", "self", "-o", compiler)
		}
	default:
		panic(fmt.Sprintf("gate: no build method %q", method))
	}
	if err != nil {
		return err
	}
	// The sources are read again: a change while the build ran leaves the compiler with no stamp, so it is built again.
	after, err := compilerStampOf(h, method)
	if err != nil || after != before {
		return err
	}
	return os.WriteFile(compilerStamp, []byte(after), 0o644)
}

// build makes the day-to-day compiler through C1, unless bin/minimaxon is already that build of the sources as they are.
func build(h host) error {
	current, err := compilerBuiltBy(h, throughC1)
	if err != nil {
		return err
	}
	if current {
		fmt.Printf("build: %s is already built through C1 from the sources as they are\n", h.compiler("minimaxon"))
		return nil
	}
	return buildCompiler(h, throughC1)
}

// currentCompiler is bin/minimaxon made of the sources as they are, by either method, built from the seed first when it is
// missing or of other sources.
func currentCompiler(h host) (string, error) {
	compiler := h.compiler("minimaxon")
	current, err := compilerBuiltBy(h, bySeed, throughC1)
	if err != nil {
		return "", err
	}
	if !current {
		fmt.Printf("%s is missing or not built from %s as they are: building it from the seed\n", compiler, strings.Join(compilerSources(h), ", "))
		if err := buildCompiler(h, bySeed); err != nil {
			return "", err
		}
	}
	return abs(compiler), nil
}
