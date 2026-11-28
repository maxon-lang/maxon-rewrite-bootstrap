package main

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"minimaxon/internal/hosts"
	"minimaxon/internal/specsource"
)

// renewSeeds renews the three seeds from self/, and refuses unless each reproduces itself. On x64 Windows the Windows seed
// builds C1, C1 builds C2 and C2 builds C3, which must be C2's bytes; C2 must pass the suites. C2 then builds the Linux
// compiler by cross-compilation, and that compiler is proven under WSL: it builds C1, which must be its own bytes, so that a
// compile does not depend on the host it runs on, C1 builds C2, and C2 builds C3, which must be C2's bytes. C2 builds the
// macOS compiler the same way, which the Mac proves the same way (proveMacSeed). Only then are the seeds replaced, C2 as the
// Windows seed and the cross-built compilers as the others, so the three always come from one commit of self/. The gate
// never builds a C3: this is the only place that checks that a compiler reproduces itself, so a compiler that does not is
// never committed. seed/README.md says when a renewal is needed.
//
// A staged renewal is the one for a change of a layout that the seed bakes into the code it makes and that the runtime
// shares with that code, such as a String's two words: the seed can compile neither the new runtime nor code that agrees
// with it. The seed then first builds C0, the new self/ against the runtime it was renewed with, which is HEAD's while the
// change is not committed (stageSeed), and C0, which makes code of the new layout, builds C1 in the seed's place.
func renewSeeds(h host, staged bool) error {
	if !h.windows() {
		return errors.New("seed: the seeds are renewed on x64 Windows with WSL and the Mac's share, which run and prove all three; another host cannot run the Windows seed")
	}
	if err := os.MkdirAll("bin", 0o755); err != nil {
		return err
	}
	first := abs(h.seed)
	if staged {
		c0 := h.compiler("c0")
		if err := stageSeed(h, c0); err != nil {
			return err
		}
		first = abs(c0)
	}
	c1, c2, c3 := h.compiler("c1"), h.compiler("c2"), h.compiler("c3")
	if err := chain(first, c1, c2, c3, nil); err != nil {
		return err
	}
	if err := suites(h, c2, nil); err != nil {
		return fmt.Errorf("%v\nseed not renewed: C2 does not pass the suites", err)
	}
	if err := assertWsl(); err != nil {
		return err
	}
	linuxSeed := filepath.Join("bin", "seed-x64-linux")
	if err := stage("the Linux compiler", nil, abs(c2), "build", "self", "--target=x64-linux", "-o", linuxSeed); err != nil {
		return err
	}
	l1, l2, l3 := filepath.Join("bin", "c1-x64-linux"), filepath.Join("bin", "c2-x64-linux"), filepath.Join("bin", "c3-x64-linux")
	if err := chain(linuxSeed, l1, l2, l3, wslCommand); err != nil {
		return err
	}
	if same, err := sameBytes(linuxSeed, l1); err != nil || !same {
		return errors.Join(err, errors.New("seed not renewed: the Linux compiler that Windows built differs from the one it builds of itself on Linux, so a compile depends on its host"))
	}
	// Under the seed's own name, which a Mach-O executable is signed under.
	macSeed := filepath.Join("bin", "seed-arm64-macos", hosts.MacOSSeedName)
	if err := os.MkdirAll(filepath.Dir(macSeed), 0o755); err != nil {
		return err
	}
	if err := stage("the macOS compiler", cacheOffEnvironment(), abs(c2), "build", "self", "--target=arm64-macos", "-o", macSeed); err != nil {
		return err
	}
	if err := proveMacSeed(macSeed); err != nil {
		return err
	}
	if err := copyFile(c2, h.seed); err != nil {
		return err
	}
	if err := copyFile(linuxSeed, hosts.SeedPath("x64-linux")); err != nil {
		return err
	}
	if err := copyFile(macSeed, hosts.SeedPath("arm64-macos")); err != nil {
		return err
	}
	// Windows keeps no executable bit for git to commit, so the Linux and macOS seeds are marked executable in the index,
	// without which a clone of the commit cannot start them.
	for _, t := range []string{"x64-linux", "arm64-macos"} {
		if err := quiet(nil, "git", "update-index", "--chmod=+x", hosts.SeedPath(t)); err != nil {
			return fmt.Errorf("%w\nseeds copied, but %s is not marked executable in the index: run git update-index --chmod=+x %s", err, hosts.SeedPath(t), hosts.SeedPath(t))
		}
	}
	fmt.Printf("seeds renewed: %s from %s, %s from %s, %s from %s\n", h.seed, c2, hosts.SeedPath("x64-linux"), linuxSeed, hosts.SeedPath("arm64-macos"), macSeed)
	return nil
}

// stagedRuntimeRevision is the commit whose runtime the seed compiles in a staged renewal: the change of the layout is
// renewed for before it is committed, so HEAD's runtime is the one the seed was renewed with.
const stagedRuntimeRevision = "HEAD"

// stageSeed has the seed build c0 of today's self/ against the runtime of stagedRuntimeRevision. A compiler finds the
// runtime and its own source by its executable's directory (README, "The runtime"), so the seed runs from bin/seed-stage,
// which holds a copy of it, that runtime and today's self/; each build is uncached, since the cache does not know which
// runtime a module was compiled against.
func stageSeed(h host, c0 string) error {
	dir := filepath.Join("bin", "seed-stage")
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := extractTree(stagedRuntimeRevision, "runtime", dir); err != nil {
		return fmt.Errorf("seed not renewed: cannot stage the runtime of %s: %w", stagedRuntimeRevision, err)
	}
	if err := robocopy(context.Background(), "self", filepath.Join(dir, "self"), "/E"); err != nil {
		return fmt.Errorf("seed not renewed: cannot stage self: %w", err)
	}
	seed := filepath.Join(dir, filepath.Base(h.seed))
	if err := copyFile(h.seed, seed); err != nil {
		return err
	}
	if err := stage(filepath.Base(c0)+", the new self/ against the runtime of "+stagedRuntimeRevision, cacheOffEnvironment(), abs(seed), "build", abs(filepath.Join(dir, "self")), "-o", c0); err != nil {
		return fmt.Errorf("%w\na staged renewal is run before the change of the layout is committed, so that the seed compiles the runtime of %s", err, stagedRuntimeRevision)
	}
	return nil
}

// extractTree writes the files under path at a revision of the repository below dir, at their paths in the repository.
func extractTree(revision, path, dir string) error {
	cmd := exec.Command("git", "archive", "--format=tar", revision, path)
	var archive, errs bytes.Buffer
	cmd.Stdout = &archive
	cmd.Stderr = &errs
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s%w", errs.String(), err)
	}
	r := tar.NewReader(&archive)
	for {
		hdr, err := r.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(hdr.Name))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			data, err := io.ReadAll(r)
			if err != nil {
				return err
			}
			if err := os.WriteFile(target, data, 0o644); err != nil {
				return err
			}
		case tar.TypeXGlobalHeader:
		default:
			return fmt.Errorf("%s: an entry of type %q, which a tree of sources does not hold", hdr.Name, hdr.Typeflag)
		}
	}
}

// chain has seed build c1, c1 build c2 and c2 build c3, each uncached and C3 with its IR verified, through wrap when it is
// not nil, and refuses unless c2 and c3 are the same bytes.
func chain(seed, c1, c2, c3 string, wrap func(compiler string, args ...string) (string, []string)) error {
	for _, s := range [][2]string{{seed, c1}, {c1, c2}, {c2, c3}} {
		name, args := s[0], []string{"build", "self", "-o", s[1]}
		if s[1] == c3 {
			args = append(args, "--verify-ir")
		}
		if wrap != nil {
			name, args = wrap(s[0], args...)
		}
		if err := stage(filepath.Base(s[1]), cacheOffEnvironment(), name, args...); err != nil {
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

func stage(name string, env []string, compiler string, args ...string) error {
	if out, err := output(env, "", compiler, args...); err != nil {
		return fmt.Errorf("%s\nseed not renewed: %s failed: %w", out, name, err)
	}
	return nil
}

// wslCommand runs a Linux compiler of the repository under WSL, with its paths the repository's from the root, which WSL
// sees under /mnt. A WSL session started from Windows carries no TMPDIR, and the compiler then uses /tmp, the distribution's
// own directory of temporary files.
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
func regenerateSpecs(h host, maxonSpecs string, advance bool) error {
	pinHashSeed()
	compiler, err := currentCompiler(h)
	if err != nil {
		return err
	}
	if err := convertSpecsIfStale(compiler, maxonSpecs, advance); err != nil {
		return err
	}
	out, replayed, err := specRun(compiler, true, "--record-passed")
	fmt.Print(out)
	if replayed != "" {
		fmt.Println(replayed)
	}
	return err
}

// convertSpecsIfStale converts maxon's spec tests and fixes them with the compiler (convertSpecs).
//
// The conversion and the fix are skipped when what they read and what they wrote are as they were when they last ended:
// maxon's spec files, the converter's source, and the compiler that formats and fixes with the runtime and the library it
// finds beside itself and the skip table, and the specs they wrote, which anything may have edited since. Whatever differs,
// or if the last conversion did not end, they run, and the stamp is written last.
func convertSpecsIfStale(compiler, maxonSpecs string, advance bool) error {
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
		return nil
	}
	os.Remove(conversionStamp)
	if err := convertSpecs(compiler, maxonSpecs, advance); err != nil {
		return err
	}
	// The inputs are read again: a change while the conversion ran leaves it with no stamp.
	readAfter, after, err := stampOf()
	if err != nil {
		return err
	}
	if readAfter == read {
		return os.WriteFile(conversionStamp, []byte(after), 0o644)
	}
	return nil
}

// convertSpecs writes specs/ again: maxon's spec files converted, then fixed. A source of other content than specs/SOURCE
// pins is refused before anything is deleted, unless advance moves the pin (internal/specsource).
func convertSpecs(compiler, maxonSpecs string, advance bool) error {
	if _, err := specsource.Check(maxonSpecs, "specs", advance); err != nil {
		return err
	}
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
	args := []string{"run", "./cmd/convert", "--specs", "--out=specs"}
	if advance {
		args = append(args, specsource.AdvanceFlag)
	}
	if err := run(env, "go", append(args, maxonSpecs)...); err != nil {
		return err
	}
	return run(env, compiler, "fix", "specs")
}
