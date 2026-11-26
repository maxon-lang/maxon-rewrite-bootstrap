package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// cacheVariable names the compiler's environment variable that gives the directory of its build cache, or turns the cache
	// off with cacheOff.
	cacheVariable = "MINIMAXON_CACHE"
	cacheOff      = "off"

	// scratchName is the prefix of the temporary directory that holds the copies of the trees whose modules are touched.
	scratchName = "selfab-touch-"

	generatedSuffix = "_gen.maxon"
	sourceSuffix    = ".maxon"
)

// treeDirectories are the directories of a checkout that a compiler built into its bin/ needs to build self/: its own source,
// and the runtime and library that it finds by walking up from its executable.
var treeDirectories = []string{"self", "runtime", "stdlib"}

// touchCase is a module whose source a measurement touches, one module of the compiler: the one nothing but main names, or one
// that every other module names.
type touchCase struct {
	name   string
	module string
}

var touchCases = []touchCase{{name: "leaf", module: "driver"}, {name: "low", module: "diag"}}

// cacheOffEnvironment is the environment of a compile that neither reads nor writes the build cache.
var cacheOffEnvironment = []string{cacheVariable + "=" + cacheOff}

// incremental is a side's copy of its tree, with a build cache of its own that its compiler has warmed, in which a module's
// source is touched and the rebuild timed. The copy keeps the checkout it was made from, which may be the working tree, as it
// was.
type incremental struct {
	dir      string
	compiler string
	env      []string
	// files, originals and touches are, for each touchCase, the file that is touched, its text before any touch and how often it
	// was touched.
	files     []string
	originals []string
	touches   []int
}

// newIncremental copies the side's tree and compiler into a temporary directory, and builds self/ there once, which fills the
// cache.
func newIncremental(s side) (*incremental, error) {
	dir, err := os.MkdirTemp("", scratchName)
	if err != nil {
		return nil, err
	}
	inc := &incremental{dir: dir, env: []string{cacheVariable + "=" + filepath.Join(dir, "cache")}}
	if err := inc.copyTree(s); err != nil {
		inc.remove()
		return nil, err
	}
	for _, c := range touchCases {
		file, err := touchedFile(filepath.Join(dir, "self", c.module))
		if err != nil {
			inc.remove()
			return nil, err
		}
		text, err := os.ReadFile(file)
		if err != nil {
			inc.remove()
			return nil, err
		}
		inc.files = append(inc.files, file)
		inc.originals = append(inc.originals, string(text))
		inc.touches = append(inc.touches, 0)
	}
	fmt.Fprintf(os.Stderr, "warming the cache of %s\n", s.name)
	if err := inc.build(); err != nil {
		inc.remove()
		return nil, err
	}
	return inc, nil
}

func (inc *incremental) remove() { os.RemoveAll(inc.dir) }

func (inc *incremental) copyTree(s side) error {
	for _, name := range treeDirectories {
		if err := os.CopyFS(filepath.Join(inc.dir, name), os.DirFS(filepath.Join(s.tree, name))); err != nil {
			return err
		}
	}
	inc.compiler = filepath.Join(inc.dir, "bin", compilerName+executableSuffix)
	if err := os.MkdirAll(filepath.Dir(inc.compiler), 0o755); err != nil {
		return err
	}
	text, err := os.ReadFile(s.compiler)
	if err != nil {
		return err
	}
	return os.WriteFile(inc.compiler, text, 0o755)
}

func (inc *incremental) build() error {
	return execute(inc.dir, inc.env, inc.compiler, "build", "self", "-o", filepath.Join(inc.dir, "bin", outputName+executableSuffix))
}

// touchedFile is the first source file of the module's directory by name that no generator writes.
func touchedFile(moduleDir string) (string, error) {
	entries, err := os.ReadDir(moduleDir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasSuffix(name, sourceSuffix) && !strings.HasSuffix(name, generatedSuffix) {
			return filepath.Join(moduleDir, name), nil
		}
	}
	return "", fmt.Errorf("%s has no source file that is not generated", moduleDir)
}

// timeTouch is the wall time of the rebuild after the case's file is written as its original and one comment line that no
// earlier touch wrote, so that of all the modules only this one's source differs from what the last build compiled.
func (inc *incremental) timeTouch(c int) (time.Duration, error) {
	inc.touches[c]++
	text := fmt.Sprintf("%s\n// selfab touch %d\n", inc.originals[c], inc.touches[c])
	if err := os.WriteFile(inc.files[c], []byte(text), 0o644); err != nil {
		return 0, err
	}
	start := time.Now()
	if err := inc.build(); err != nil {
		return 0, err
	}
	return time.Since(start), nil
}

// measureTouches times the rebuild of both sides after each touchCase's module is touched, N runs of each case, with the sides
// interleaved, and says for each case what it measured.
func measureTouches(a, b side, runs int) error {
	sides := make([]*incremental, 0, 2)
	for _, s := range []side{a, b} {
		inc, err := newIncremental(s)
		if err != nil {
			return err
		}
		defer inc.remove()
		sides = append(sides, inc)
	}
	var summary []string
	for c, tc := range touchCases {
		fmt.Fprintf(os.Stderr, "touching the %s module %s\n", tc.name, tc.module)
		timesA, timesB, err := interleave(runs, func(i int) (time.Duration, error) { return sides[i].timeTouch(c) })
		if err != nil {
			return err
		}
		title := fmt.Sprintf("rebuild of self/ after touching the %s module (%s), cache warm", tc.name, tc.module)
		printTimes(title, a, b, timesA, timesB)
		summary = append(summary, fmt.Sprintf("%s B/A %.4f", tc.name, float64(median(timesB))/float64(median(timesA))))
	}
	fmt.Println()
	fmt.Println(strings.Join(summary, ", "))
	return nil
}
