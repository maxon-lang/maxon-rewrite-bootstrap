// Command gate holds the repository's checks in one place for every host the compiler is built on: the gate (check), the
// suites it runs on a compiler (suites), the renewal of the seeds (seed), the regeneration of specs/ (specs) and of stdlib/
// (stdlib) and the day-to-day build (build), each with the one compiler bin/minimaxon (compiler.go). check.ps1, seed.ps1,
// specs.ps1, stdlib.ps1 and build.ps1 on Windows and check.sh on Linux and macOS only call it, so the hosts cannot drift
// apart in what they check. A step that cannot run on the host says so and why, and is skipped; every other step that fails
// stops the command.
//
//	go run ./cmd/gate check [--record-specs=<maxon specs dir>] [test args...]
//	go run ./cmd/gate suites <compiler> [test args...]
//	go run ./cmd/gate seed [--staged]
//	go run ./cmd/gate specs [--advance-source] <maxon specs dir>
//	go run ./cmd/gate stdlib [--check] <maxon stdlib dir>
//	go run ./cmd/gate build
//	go run ./cmd/gate compiler
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"minimaxon/internal/heavylock"
	"minimaxon/internal/hosts"
	"minimaxon/internal/specsource"
)

const usage = `usage:
  gate check [--record-specs=<maxon specs dir>] [test args...]
                                     the gate: Go checks, generators, a compiler from the seed and the suites on it; with the
                                     flag it first converts the specs from maxon's and its spec step also writes specs/passed.txt
  gate suites <compiler> [args...]   the suites a compiler must pass (on arm64 Linux, which has no seed, the lanes it runs natively)
  gate seed [--staged]               renew the seeds, each proven to reproduce itself; --staged for a change of a layout\n                                     the seed bakes in, which it cannot compile the runtime of: run before committing it
  gate specs [--advance-source] <maxon specs dir>
                                     regenerate specs/ from maxon's spec tests and write specs/passed.txt again; specs/SOURCE
                                     pins the content they are converted from, and the flag moves the pin on purpose
  gate stdlib [--check] <maxon stdlib dir>
                                     regenerate stdlib/ from maxon's library; --check fails when that differs from what is committed
  gate build                         build bin/minimaxon, the day-to-day compiler: the seed builds C1, C1 builds self/
  gate compiler                      build bin/minimaxon from the seed unless it is built from the sources as they are
`

func main() {
	gate()
	os.Exit(code)
}

// gate runs the command, holding the machine's lock until it returns.
func gate() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	// Set once for the gate's own process: everything it starts inherits it, and a waiting gate holds no core.
	if err := lowerPriority(); err != nil {
		fail(err)
	}
	root, err := repoRoot()
	if err != nil {
		fail(err)
	}
	if err := os.Chdir(root); err != nil {
		fail(err)
	}
	h, err := currentHost()
	if err != nil {
		fail(err)
	}
	args := os.Args[2:]
	if h.seed == "" && os.Args[1] != "suites" {
		fail(fmt.Errorf("%s: %s has no committed seed to build the compiler from; it runs the lanes of a compiler that another host built: gate suites <compiler> (README, \"Continuous integration\")", os.Args[1], h.target))
	}
	if os.Args[1] != "suites" || !slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, "--lane=") }) {
		release, err := heavylock.Hold()
		if err != nil {
			fail(err)
		}
		defer release()
	}
	switch os.Args[1] {
	case "check":
		err = check(h, args)
		printStepTable()
		if err == nil {
			fmt.Println("all checks passed")
		}
	case "suites":
		if len(args) < 1 {
			fail(errors.New("suites: name the compiler"))
		}
		err = suites(h, args[0], args[1:])
		if !slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, "--lane=") }) {
			printStepTable()
		}
	case "seed":
		staged := len(args) == 1 && args[0] == "--staged"
		if len(args) > 0 && !staged {
			fail(errors.New("seed: the one option is --staged, for a change of a layout that the seed bakes in (seed/README.md)"))
		}
		err = renewSeeds(h, staged)
		printStepTable()
	case "specs":
		advance := len(args) > 0 && args[0] == specsource.AdvanceFlag
		if advance {
			args = args[1:]
		}
		if len(args) != 1 {
			fail(errors.New("specs: name maxon's specs directory"))
		}
		err = regenerateSpecs(h, args[0], advance)
	case "stdlib":
		checkOnly := len(args) > 0 && args[0] == "--check"
		if checkOnly {
			args = args[1:]
		}
		if len(args) != 1 {
			fail(errors.New("stdlib: name maxon's stdlib directory"))
		}
		if checkOnly {
			err = checkStdlib(h, args[0])
		} else {
			err = regenerateStdlib(h, args[0])
		}
	case "build":
		err = build(h)
	case "compiler":
		_, err = currentCompiler(h)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Println(err)
		code = 1
	}
}

// code is what the gate exits with, once main has let the machine's lock go.
var code int

func fail(err error) {
	fmt.Println(err)
	os.Exit(1)
}

// repoRoot is the directory that holds go.mod and self/, found from the working directory upward.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if isFile(filepath.Join(dir, "go.mod")) && isDir(filepath.Join(dir, "self")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("gate: run it inside the repository")
		}
		dir = parent
	}
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// host is the system the gate runs on, as the compiler names targets, and what it has to run another target's executables.
type host struct {
	// target is the compiler's name of the host's target, as --target spells it.
	target string
	// exe ends the name of an executable of the host.
	exe string
	// seed is the committed compiler that builds self/ on this host, "" where there is none (arm64 Linux).
	seed string
	// lanes are the only lanes of the suites that this host runs, nil for all of them. A host without a seed gets its
	// compiler from another host, and runs the lanes that only it can: the other lanes are that host's.
	lanes []string
}

// armLinuxLanes are the lanes that arm64 Linux runs natively on a compiler that x64 Linux cross-built (gate suites): the
// compiler's own suites, and the corpus and the reruns of the programs that schedule behaviours for arm64-linux, which an
// x64 host can run only under qemu and does not.
var armLinuxLanes = []string{"checks", "native", "native-behaviours"}

func currentHost() (host, error) {
	name, err := hosts.Target()
	if err != nil {
		return host{}, fmt.Errorf("gate: %w", err)
	}
	h := host{target: name, seed: hosts.SeedPath(name)}
	if name == "arm64-linux" {
		h.lanes = armLinuxLanes
	}
	if runtime.GOOS == "windows" {
		h.exe = ".exe"
	}
	return h, nil
}

func (h host) windows() bool { return h.target == "x64-windows" }

func (h host) linux() bool { return h.target == "x64-linux" }

func (h host) macos() bool { return h.target == "arm64-macos" }

func (h host) armLinux() bool { return h.target == "arm64-linux" }

// compiler is the path of a compiler called name that the gate builds into bin/.
func (h host) compiler(name string) string {
	return filepath.Join("bin", name+h.exe)
}

// run runs a command with the gate's standard streams, its standard input empty: a program that reads it would otherwise
// wait until a deadline ends it.
func run(env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), env...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// output runs a command and gives what it wrote to both streams, and whether it exited 0.
func output(env []string, dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	err := cmd.Run()
	if err != nil {
		err = fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out.String(), err
}

// quiet runs a command, and shows what it wrote only when it fails.
func quiet(env []string, name string, args ...string) error {
	out, err := output(env, "", name, args...)
	if err != nil {
		return fmt.Errorf("%s%w", out, err)
	}
	return nil
}

// skip says that a step does not run on this host and why.
func skip(step, why string) {
	fmt.Printf("%s: skipped on this host, %s\n", step, why)
}

// step runs one step, and names it when it fails.
type step struct {
	name string
	run  func() error
}

func runSteps(steps []step) error {
	for _, s := range steps {
		if err := timed(s.name, s.run); err != nil {
			return fmt.Errorf("%v\ncheck failed at: %s", err, s.name)
		}
	}
	return nil
}

// scratchDir makes a new directory for a step's files in the compiler's own directory of the system's temporary files
// (ast.temporaryPath), so that what the gate builds is where the compiler's own test executables are.
func scratchDir(prefix string) (string, error) {
	parent := filepath.Join(os.TempDir(), "minimaxon")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	return os.MkdirTemp(parent, prefix)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func sameBytes(a, b string) (bool, error) {
	x, err := os.ReadFile(a)
	if err != nil {
		return false, err
	}
	y, err := os.ReadFile(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(x, y), nil
}
