package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// The scripts that run a batch are checked in as files and shipped with it.
var (
	//go:embed run.sh
	runShell []byte
	//go:embed run.ps1
	runPowerShell []byte
)

const (
	manifestName      = "manifest.txt"
	runShellName      = "run.sh"
	runPowerShellName = "run.ps1"
	executableMode    = 0o755
	fileMode          = 0o644
	directoryMode     = 0o755
)

// buildOptions say what a batch is made from and for.
type buildOptions struct {
	// root is the repository: where tests/bench is and where the compiler's relative path starts.
	root     string
	compiler string
	target   target
	out      string
}

// buildBatch compiles every benchmark with minimaxon and with Go for the target into out, with the manifest and the scripts
// that run them.
func buildBatch(o buildOptions) error {
	programs, err := findPrograms(o.root)
	if err != nil {
		return err
	}
	compiler, err := filepath.Abs(o.compiler)
	if err != nil {
		return err
	}
	out, err := filepath.Abs(o.out)
	if err != nil {
		return err
	}
	for _, impl := range impls {
		if err := os.MkdirAll(filepath.Join(out, impl), directoryMode); err != nil {
			return err
		}
	}
	for _, p := range programs {
		fmt.Fprintf(os.Stderr, "building %s for %s\n", p.name, o.target.name)
		exe := p.name + o.target.exeSuffix
		if err := runCommand(o.root, nil, compiler, "build", p.source, "--target="+o.target.name, "-o", filepath.Join(out, implMinimaxon, exe)); err != nil {
			return err
		}
		env := []string{"GOOS=" + o.target.goos, "GOARCH=" + o.target.goarch, "CGO_ENABLED=0"}
		if err := runCommand(o.root, env, goTool(), "build", "-o", filepath.Join(out, implGo, exe), p.goPackage); err != nil {
			return err
		}
	}
	if err := writeManifest(filepath.Join(out, manifestName), o, compiler, programs); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, runShellName), runShell, executableMode); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, runPowerShellName), runPowerShell, fileMode)
}

// goTool is the go command of the PATH, or else the one of the toolchain that built this tool.
func goTool() string {
	if found, err := exec.LookPath("go"); err == nil {
		return found
	}
	name := "go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(runtime.GOROOT(), "bin", name)
}

// runCommand runs a command in dir with the environment's additions, and reports its output if it fails.
func runCommand(dir string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, output.String())
	}
	return nil
}

// commandLine is the first line a command prints.
func commandLine(dir string, name string, args ...string) string {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "unknown (" + err.Error() + ")"
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(line)
}

// writeManifest writes what run.sh and run.ps1 need to know about the batch: its target, its programs and each program's
// expected output, tab-separated, a line for each.
func writeManifest(file string, o buildOptions, compiler string, programs []program) error {
	var b strings.Builder
	line := func(fields ...string) { b.WriteString(strings.Join(fields, "\t") + "\n") }
	line("target", o.target.name)
	line("go", commandLine(o.root, goTool(), "version")+" GOOS="+o.target.goos+" GOARCH="+o.target.goarch)
	line("compiler", filepath.Base(compiler)+" at git "+commandLine(o.root, "git", "rev-parse", "--short", "HEAD"))
	for _, p := range programs {
		line("program", p.name)
	}
	for _, p := range programs {
		for _, expected := range p.expected {
			line("expect", p.name, expected)
		}
	}
	return os.WriteFile(file, []byte(b.String()), fileMode)
}
