package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// renewSeeds renews both seeds from self/, and refuses unless each reproduces itself. On x64 Windows the Windows seed builds
// C1, C1 builds C2 and C2 builds C3, which must be C2's bytes; C2 must pass the suites. C2 then builds the Linux compiler by
// cross-compilation, and that compiler is proven under WSL: it builds C1, which must be its own bytes, so that a compile does
// not depend on the host it runs on, C1 builds C2, and C2 builds C3, which must be C2's bytes. Only then are both seeds
// replaced, C2 as the Windows seed and the cross-built compiler as the Linux one, so the two always come from one commit of
// self/. The gate never builds a C3: this is the only place that checks that a compiler reproduces itself, so a compiler that
// does not is never committed. seed/README.md says when a renewal is needed.
func renewSeeds(h host) error {
	if !h.windows() {
		return errors.New("seed: the seeds are renewed on x64 Windows with WSL, which runs and proves both; a Linux host cannot run the Windows seed")
	}
	if err := os.MkdirAll("bin", 0o755); err != nil {
		return err
	}
	c1, c2, c3 := h.compiler("c1"), h.compiler("c2"), h.compiler("c3")
	if err := chain(abs(h.seed), c1, c2, c3, nil); err != nil {
		return err
	}
	if err := suites(h, c2, nil); err != nil {
		return fmt.Errorf("%v\nseed not renewed: C2 does not pass the suites", err)
	}
	if err := assertWsl(); err != nil {
		return err
	}
	linuxSeed := filepath.Join("bin", "seed-x64-linux")
	if err := stage("the Linux compiler", abs(c2), "build", "self", "--target=x64-linux", "-o", linuxSeed); err != nil {
		return err
	}
	l1, l2, l3 := filepath.Join("bin", "c1-x64-linux"), filepath.Join("bin", "c2-x64-linux"), filepath.Join("bin", "c3-x64-linux")
	if err := chain(linuxSeed, l1, l2, l3, wslCommand); err != nil {
		return err
	}
	if same, err := sameBytes(linuxSeed, l1); err != nil || !same {
		return errors.Join(err, errors.New("seed not renewed: the Linux compiler that Windows built differs from the one it builds of itself on Linux, so a compile depends on its host"))
	}
	if err := copyFile(c2, h.seed); err != nil {
		return err
	}
	if err := copyFile(linuxSeed, seeds["x64-linux"]); err != nil {
		return err
	}
	fmt.Printf("seeds renewed: %s from %s, %s from %s\n", h.seed, c2, seeds["x64-linux"], linuxSeed)
	return nil
}

// chain has seed build c1, c1 build c2 and c2 build c3, each uncached and C3 with its IR verified, through wrap when it is
// not nil, and refuses unless c2 and c3 are the same bytes.
func chain(seed, c1, c2, c3 string, wrap func(compiler string, args ...string) (string, []string)) error {
	for _, s := range [][2]string{{seed, c1}, {c1, c2}, {c2, c3}} {
		name, args := s[0], []string{"build", "self", "--no-cache", "-o", s[1]}
		if s[1] == c3 {
			args = append(args, "--verify-ir")
		}
		if wrap != nil {
			name, args = wrap(s[0], args...)
		}
		if err := stage(filepath.Base(s[1]), name, args...); err != nil {
			return err
		}
	}
	same, err := sameBytes(c2, c3)
	if err != nil {
		return err
	}
	if !same {
		return fmt.Errorf("seed not renewed: %s and %s differ, so the compiler does not reproduce itself", c2, c3)
	}
	return nil
}

func stage(name, compiler string, args ...string) error {
	if out, err := output(nil, "", compiler, args...); err != nil {
		return fmt.Errorf("%s\nseed not renewed: %s failed: %w", out, name, err)
	}
	return nil
}

// wslCommand runs a Linux compiler of the repository under WSL, with its paths the repository's from the root, which WSL
// sees under /mnt.
func wslCommand(compiler string, args ...string) (string, []string) {
	root := wslPath(abs("."))
	words := []string{"--cd", root, "--exec", "./" + filepath.ToSlash(compiler)}
	for _, a := range args {
		words = append(words, filepath.ToSlash(a))
	}
	return "wsl.exe", words
}

// regenerateSpecs regenerates specs/ from maxon's spec tests: cmd/convert converts their syntax, then the compiler's fix
// command applies the fix of each diagnostic that minimaxon's language differences raise (README, "Where it differs from
// Maxon"), on every target's branches of a test. The compiler is bin/minimaxon, built first when it is not of the sources
// as they are (currentCompiler). The run at the end runs every test (replayed when it can, specRun) and writes specs/passed.txt again: the
// new passes are added, a listed test that the conversion now skips is dropped (the line "DROPPED"), and a listed test that
// fails stays, so the gate goes on failing until it is fixed.
//
// The conversion and the fix are skipped when what they read and what they wrote are as they were when they last ended:
// maxon's spec files, the converter's source, and the compiler that formats and fixes with the runtime and the library it
// finds beside itself and the skip table, and the specs they wrote, which anything may have edited since. Whatever differs,
// or if the last conversion did not end, they run, and the stamp is written last.
func regenerateSpecs(h host, maxonSpecs string) error {
	compiler, err := currentCompiler(h)
	if err != nil {
		return err
	}
	conversionStamp := filepath.Join("bin", "specs-conversion.stamp")
	inputs := []string{maxonSpecs, filepath.Join("cmd", "convert"), "go.mod", compiler, "runtime", "stdlib", filepath.Join("self", "driver", "skips.tsv")}
	stampOf := func() (string, string, error) {
		read, err := pathsDigest(inputs, nil)
		if err != nil {
			return "", "", err
		}
		written, err := pathsDigest([]string{"specs"}, []string{abs(filepath.Join("specs", "passed.txt"))})
		if err != nil {
			return "", "", err
		}
		return read, "inputs " + read + "\nspecs " + written, nil
	}
	read, stamp, err := stampOf()
	if err != nil {
		return err
	}
	if stampHolds(conversionStamp, stamp) {
		fmt.Printf("specs: conversion and fix skipped, the inputs and the specs are those of the last ones (stamp %s)\n", stampID(stamp))
	} else {
		os.Remove(conversionStamp)
		if err := convertSpecs(compiler, maxonSpecs); err != nil {
			return err
		}
		// The inputs are read again: a change while the conversion ran leaves it with no stamp.
		readAfter, after, err := stampOf()
		if err != nil {
			return err
		}
		if readAfter == read {
			if err := os.WriteFile(conversionStamp, []byte(after), 0o644); err != nil {
				return err
			}
		}
	}
	out, replayed, err := specRun(compiler, "--record-passed")
	fmt.Print(out)
	if replayed != "" {
		fmt.Println(replayed)
	}
	return err
}

// convertSpecs writes specs/ again: maxon's spec files converted, then fixed.
func convertSpecs(compiler, maxonSpecs string) error {
	// The conversion lays out what it writes with the same compiler, whatever the environment names.
	env := []string{"MINIMAXON=" + compiler}
	old, err := filepath.Glob(filepath.Join("specs", "*.md"))
	if err != nil {
		return err
	}
	for _, f := range old {
		if !strings.EqualFold(filepath.Base(f), "README.md") {
			if err := os.Remove(f); err != nil {
				return err
			}
		}
	}
	if err := run(env, "go", "run", "./cmd/convert", "--specs", "--out=specs", maxonSpecs); err != nil {
		return err
	}
	return run(env, compiler, "fix", "specs")
}
