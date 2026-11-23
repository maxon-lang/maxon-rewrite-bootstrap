package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// runOptions say what `bench run` builds, how many rounds it runs and where it leaves the results.
type runOptions struct {
	build   buildOptions
	rounds  int
	results string
}

// runLocal builds the batch of a target into a temporary directory, runs it on this machine and writes the results file. A
// target this machine can run only under an emulator is refused: an emulator's timings say nothing about the hardware.
func runLocal(o runOptions) error {
	if o.build.target.emulatedHere {
		return fmt.Errorf("%s runs here only under an emulator (WSL's qemu), whose timings mean nothing: build the batch with `bench build --target=%s --out=<dir>`, "+
			"run its run.sh on %s hardware and give the results file to `bench report`", o.build.target.name, o.build.target.name, o.build.target.name)
	}
	dir, err := os.MkdirTemp("", "minimaxon-bench-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	o.build.out = dir
	if err := buildBatch(o.build); err != nil {
		return err
	}
	results, err := filepath.Abs(o.results)
	if err != nil {
		return err
	}
	switch {
	case o.build.target.goos == "windows" && runtime.GOOS == "windows":
		return runWindows(dir, results, o.rounds)
	case o.build.target.goos == "linux" && runtime.GOOS == "windows":
		return runInWSL(dir, results, o.rounds)
	case o.build.target.goos == "linux" && runtime.GOOS == "linux" && o.build.target.goarch == runtime.GOARCH:
		return runShellScript(dir, results, o.rounds)
	}
	return fmt.Errorf("%s cannot run on %s/%s", o.build.target.name, runtime.GOOS, runtime.GOARCH)
}

// streamCommand runs a command with its standard error shown, which is where the scripts say which program they are on.
func streamCommand(env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func runWindows(dir, results string, rounds int) error {
	shell := "pwsh"
	if _, err := exec.LookPath(shell); err != nil {
		shell = "powershell"
	}
	return streamCommand(nil, shell, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(dir, runPowerShellName),
		"-Rounds", strconv.Itoa(rounds), "-Results", results)
}

func runShellScript(dir, results string, rounds int) error {
	return streamCommand([]string{"BENCH_ROUNDS=" + strconv.Itoa(rounds), "BENCH_RESULTS=" + results}, "sh", filepath.Join(dir, runShellName))
}

// wslOutput runs a command in WSL's default distribution and gives what it printed.
func wslOutput(args ...string) (string, error) {
	cmd := exec.Command("wsl.exe", append([]string{"--exec"}, args...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("wsl %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(out.String()), nil
}

// runInWSL runs the batch from a copy in the distribution's own /tmp, so that neither the start of WSL nor its view of the
// Windows drives is timed, and brings the results file back.
func runInWSL(dir, results string, rounds int) error {
	from, err := wslOutput("wslpath", "-a", filepath.ToSlash(dir))
	if err != nil {
		return err
	}
	copied, err := wslOutput("sh", "-c", `d=$(mktemp -d /tmp/minimaxon-bench.XXXXXX) && cp -r "$1/." "$d" && chmod -R u+x "$d/minimaxon" "$d/go" && echo "$d"`, "sh", from)
	if err != nil {
		return err
	}
	defer wslOutput("rm", "-rf", copied)
	remoteResults := copied + "/results.txt"
	if err := streamCommand(nil, "wsl.exe", "--exec", "env", "BENCH_ROUNDS="+strconv.Itoa(rounds), "BENCH_RESULTS="+remoteResults, "sh", copied+"/"+runShellName); err != nil {
		return err
	}
	text, err := wslOutput("cat", remoteResults)
	if err != nil {
		return err
	}
	return os.WriteFile(results, []byte(text+"\n"), fileMode)
}
