// Command bench compares minimaxon with Go on every target: it builds the programs of tests/bench with the compiler and
// their Go counterparts into a batch that a script runs (anywhere, arm64 hardware included), and reports the results.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
)

const usageText = `usage:
  bench build --target=<arch>-<os> --out=<dir> [--compiler=bin/minimaxon.exe] [--root=.]
      Builds each program of tests/bench with the compiler and its Go counterpart (tests/bench/go) for the target into <dir>,
      with a manifest and run.sh (POSIX sh) and run.ps1 (Windows) that run them and write one results file.
  bench run --target=x64-windows|x64-linux [--rounds=7] [--results=bench-<target>.txt] [--compiler=...] [--root=.]
      Builds the batch, runs it on this machine (x64-linux through WSL on Windows) and reports. An arm64 target is refused:
      emulated timings mean nothing, so ship the batch to arm64 hardware.
  bench report [--cpu=<name>] <results-file>...
      Writes the Markdown report of the results of one or more targets, side by side. A machine that cannot tell its CPU
      (a Linux VM on a Mac) leaves it out of its results file; --cpu names it for the files that have none.

Run from the repository's root. Times are the best of the rounds, and a ratio below 1 means minimaxon is faster.`

const (
	exitError = 1
	exitUsage = 2

	defaultRounds = 7
	// defaultCompilerName is the compiler that check.ps1 builds, without its extension.
	defaultCompilerName = "bin/minimaxon"
)

func main() {
	if len(os.Args) < 2 {
		fail(exitUsage, usageText)
	}
	var err error
	switch os.Args[1] {
	case "build":
		err = buildCommand(os.Args[2:])
	case "run":
		err = runCommandLine(os.Args[2:])
	case "report":
		err = reportCommand(os.Args[2:])
	case "-h", "-help", "--help", "help":
		fmt.Println(usageText)
	default:
		fail(exitUsage, usageText)
	}
	if err != nil {
		fail(exitError, "bench: "+err.Error())
	}
}

func fail(code int, message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(code)
}

// buildFlags are the flags that say what a batch is made from and for.
func buildFlags(set *flag.FlagSet) (compiler, root, targetName *string) {
	defaultCompiler := defaultCompilerName
	if runtime.GOOS == "windows" {
		defaultCompiler += ".exe"
	}
	compiler = set.String("compiler", defaultCompiler, "the minimaxon compiler")
	root = set.String("root", ".", "the repository's root")
	targetName = set.String("target", "", "the target: x64-windows, x64-linux, arm64-linux or arm64-macos")
	return compiler, root, targetName
}

func buildCommand(args []string) error {
	set := flag.NewFlagSet("build", flag.ExitOnError)
	compiler, root, targetName := buildFlags(set)
	out := set.String("out", "", "the directory to build the batch into")
	set.Parse(args)
	if *out == "" {
		return fmt.Errorf("build needs --out=<dir>")
	}
	t, err := findTarget(*targetName)
	if err != nil {
		return err
	}
	return buildBatch(buildOptions{root: *root, compiler: *compiler, target: t, out: *out})
}

func runCommandLine(args []string) error {
	set := flag.NewFlagSet("run", flag.ExitOnError)
	compiler, root, targetName := buildFlags(set)
	rounds := set.Int("rounds", defaultRounds, "how many times each program runs with each implementation")
	results := set.String("results", "", "the results file (default bench-<target>.txt)")
	set.Parse(args)
	t, err := findTarget(*targetName)
	if err != nil {
		return err
	}
	if *rounds < 1 {
		return fmt.Errorf("--rounds must be at least 1")
	}
	file := *results
	if file == "" {
		file = "bench-" + t.name + ".txt"
	}
	if err := runLocal(runOptions{build: buildOptions{root: *root, compiler: *compiler, target: t}, rounds: *rounds, results: file}); err != nil {
		return err
	}
	return reportFiles([]string{file}, "")
}

func reportCommand(args []string) error {
	set := flag.NewFlagSet("report", flag.ExitOnError)
	cpu := set.String("cpu", "", "the CPU of the results files that record none")
	set.Parse(args)
	if set.NArg() == 0 {
		return fmt.Errorf("report needs one or more results files")
	}
	return reportFiles(set.Args(), *cpu)
}

// reportFiles writes the report of the results files; unknownCPU, when it is not empty, is the CPU of each that records none.
func reportFiles(files []string, unknownCPU string) error {
	var all []*results
	for _, file := range files {
		r, err := readResults(file)
		if err != nil {
			return err
		}
		if unknownCPU != "" && r.metaValue("cpu") == missing {
			r.setMeta("cpu", unknownCPU)
		}
		all = append(all, r)
	}
	writeReport(os.Stdout, all)
	return nil
}
