package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// cacheTest holds the build cache to the executable an uncached build makes. A project whose main inlines a leaf of the module
// it names is built cold and warm, then the leaf's body is edited: the order takes the leaf's module to make the summary it
// made before, main's entry is linked, the leaf's lower finds another summary and the compile is done again, and the
// executable must be the uncached one of the edited source and print what that source prints. All of it runs on the
// machine's workers and again on one, each with a cache of its own.
func cacheTest(h host, compiler string) error {
	testCase := filepath.Join("tests", "modules", "a-leaf-of-a-named-module-is-inlined")
	for _, procs := range []string{"", "1"} {
		if err := cacheRun(h, compiler, testCase, procs); err != nil {
			workers := "all workers"
			if procs != "" {
				workers = procs + " worker"
			}
			return fmt.Errorf("cache (%s): %w", workers, err)
		}
	}
	return nil
}

func cacheRun(h host, compiler, testCase, procs string) error {
	root, err := scratchDir("minimaxon-cache-test-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	project := filepath.Join(root, "project")
	if err := copyTree(testCase, project); err != nil {
		return err
	}
	cache := filepath.Join(root, "cache")
	env := []string{cacheVariable + "=" + cache}
	if procs != "" {
		env = append(env, "MAXON_MAX_PROCS="+procs)
	}
	// Later entries of an environment win, so the cache variable of the uncached one is off.
	uncachedEnv := append(slices.Clone(env), cacheOffEnvironment()...)
	// Every build is under one file name, each in a directory of its own: a Mach-O image is signed under its file's name.
	exeOf := func(name string) string { return filepath.Join(root, name, "program"+h.exe) }
	// run is what the compiler wrote building the project, which has the executable at exeOf(name).
	run := func(buildEnv []string, name string, flags ...string) (string, error) {
		exe := exeOf(name)
		if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
			return "", err
		}
		out, err := output(buildEnv, "", compiler, append([]string{"build", project, "-o", exe}, flags...)...)
		if err != nil {
			return "", fmt.Errorf("the %s build failed: %s%w", name, out, err)
		}
		return out, nil
	}
	buildIn := func(buildEnv []string, name string, flags ...string) ([]byte, error) {
		if _, err := run(buildEnv, name, flags...); err != nil {
			return nil, err
		}
		return os.ReadFile(exeOf(name))
	}
	build := func(name string, flags ...string) ([]byte, error) { return buildIn(env, name, flags...) }
	buildUncached := func(name string) ([]byte, error) { return buildIn(uncachedEnv, name) }
	same := func(name string, want []byte, what string) error {
		got, err := build(name)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, want) {
			return errors.New(what)
		}
		return nil
	}
	uncached, err := buildUncached("uncached")
	if err != nil {
		return err
	}
	if err := same("cold", uncached, "the cold build differs from the uncached one"); err != nil {
		return err
	}
	if err := same("warm", uncached, "the warm build differs from the uncached one"); err != nil {
		return err
	}
	if err := runtimeNotCompiled(run, env, uncachedEnv); err != nil {
		return err
	}
	leaf := filepath.Join(project, "gauges", "gauges.maxon")
	text, err := os.ReadFile(leaf)
	if err != nil {
		return err
	}
	edited := strings.Replace(string(text), "\t\treturn i\n\tend 'inside'", "\t\treturn i + 1\n\tend 'inside'", 1)
	if edited == string(text) {
		return errors.New("the leaf to edit is not in the test project")
	}
	if err := os.WriteFile(leaf, []byte(edited), 0o644); err != nil {
		return err
	}
	editedUncached, err := buildUncached("edited-uncached")
	if err != nil {
		return err
	}
	if err := same("edited", editedUncached, "after a misprediction the build differs from the uncached one"); err != nil {
		return err
	}
	if err := same("edited-warm", editedUncached, "the warm build of the edited source differs from the uncached one"); err != nil {
		return err
	}
	printed, err := output(nil, "", exeOf("edited"))
	if err != nil {
		return err
	}
	if strings.TrimSpace(printed) == "5920" {
		return errors.New("the edited leaf did not change what the program prints")
	}
	// Two edits of main's body: the first checks the module main names again and keeps its checked state, the second loads
	// that state instead.
	mainPath := filepath.Join(project, "main.maxon")
	mainText, err := os.ReadFile(mainPath)
	if err != nil {
		return err
	}
	for n := 1; n <= 2; n++ {
		probe := fmt.Sprintf("\tif total == %d 'probe'\n\t\tprint(\"probe\")\n\tend 'probe'\n\tprint(", 999990+n)
		if err := os.WriteFile(mainPath, []byte(strings.ReplaceAll(string(mainText), "\tprint(", probe)), 0o644); err != nil {
			return err
		}
		mainUncached, err := buildUncached(fmt.Sprintf("main%d-uncached", n))
		if err != nil {
			return err
		}
		if err := same(fmt.Sprintf("main%d", n), mainUncached, fmt.Sprintf("after edit %d of main the build differs from the uncached one", n)); err != nil {
			return err
		}
	}
	checked, _ := filepath.Glob(filepath.Join(cache, "*.checked"))
	if len(checked) == 0 {
		return errors.New("no checked state was kept")
	}
	_, err = build("verified", "--verify-cache")
	return err
}

// runtimeCompileReport is the line of --timings that a compile of the runtime's units writes, which a compile of the program
// does not (its goal is the executable).
const runtimeCompileReport = "compile timings, goal units"

// runtimeNotCompiled holds a warm build to find the runtime's code in the cache under the key its source's digest makes, with no
// stage of the runtime run: its timings report no compile of the runtime, where an uncached build's report one, so that the check
// would fail if the report were renamed.
func runtimeNotCompiled(run func([]string, string, ...string) (string, error), env, uncachedEnv []string) error {
	uncached, err := run(uncachedEnv, "uncached-timings", "--timings")
	if err != nil {
		return err
	}
	if !strings.Contains(uncached, runtimeCompileReport) {
		return fmt.Errorf("an uncached build's timings have no %q, so that a warm build has none shows nothing", runtimeCompileReport)
	}
	warm, err := run(env, "warm-timings", "--timings")
	if err != nil {
		return err
	}
	if strings.Contains(warm, runtimeCompileReport) {
		return errors.New("a warm build compiled the runtime: its code was not found in the cache under its source's key")
	}
	return nil
}

// copyTree copies the directory src, and everything under it, to dst.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}
