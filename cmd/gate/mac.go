package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// macShare is the folder that the Windows machine and the Mac both mount, the Mac's ~/Dev/agents/minimaxon, through which
// the gate reaches the Mac.
const macShare = `M:\agents\minimaxon`

// robocopyFailed is the least exit code of robocopy that says it failed; below it, the code says what it copied.
const robocopyFailed = 8

// macLane runs the arm64-macos corpus on the Mac: the gate compiles every program and the reruns of those that schedule
// behaviours, keeping each executable through cmd/batchrun, and when the share holds the Mac's results of exactly those
// executables (the stamp of their rows), replays them through the test runner, which compares them, and fails on any
// failure. Otherwise it stages the batch at in\gate for the Mac session to run (bash ~/Dev/agents/minimaxon/in/gate/run.sh)
// and skips, as it does when the share is not mounted: the Mac is not the gate's machine.
func macLane(h host, compiler string, behavioursArgs, testArgs []string) error {
	if !h.windows() {
		skip("corpus on arm64-macos", "the Mac is reached through the share "+macShare+" that only the Windows machine mounts")
		return nil
	}
	if !isDir(filepath.Join(macShare, "in")) {
		fmt.Printf("mac lane: skipped, %s is not mounted\n", macShare)
		return nil
	}
	root, err := scratchDir("minimaxon-mac-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	batchrun := filepath.Join(root, "batchrun.exe")
	if err := quiet(nil, "go", "build", "-o", batchrun, "./cmd/batchrun"); err != nil {
		return err
	}
	stage := filepath.Join(root, "stage")
	_, _ = macCorpus(compiler, batchrun, "record", stage, behavioursArgs, testArgs)
	stamp, err := rowStamp(filepath.Join(stage, "rows"))
	if err != nil {
		return err
	}
	results := filepath.Join(macShare, "out", "gate")
	if have, err := os.ReadFile(filepath.Join(results, "stamp.txt")); err == nil && strings.TrimRight(string(have), "\r\n") == stamp {
		replay := filepath.Join(root, "results")
		if err := robocopy(filepath.Join(results, "out"), filepath.Join(replay, "out"), "/E"); err != nil {
			return fmt.Errorf("mac lane: cannot copy the Mac's results from %s: %w", results, err)
		}
		out, err := macCorpus(compiler, batchrun, "replay", replay, behavioursArgs, testArgs)
		for line := range strings.SplitSeq(out, "\n") {
			if line != "" && !strings.HasPrefix(line, "PASS ") {
				fmt.Println(line)
			}
		}
		return err
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(stage, "batchrun"), "./cmd/batchrun")
	cmd.Env = append(os.Environ(), "GOOS=darwin", "GOARCH=arm64")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s%w", out, err)
	}
	if err := copyFile(filepath.Join("cmd", "batchrun", "run.sh"), filepath.Join(stage, "run.sh")); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, "stamp.txt"), []byte(stamp+"\n"), 0o644); err != nil {
		return err
	}
	// A mirror, so no row or executable of an older batch stays for the Mac to run: the share refuses some listings that a
	// recursive delete makes, and robocopy's purge does not.
	batch := filepath.Join(macShare, "in", "gate")
	if err := robocopy(stage, batch, "/MIR"); err != nil {
		return fmt.Errorf("mac lane: cannot stage the batch at %s: %w", batch, err)
	}
	fmt.Println("mac lane: skipped, the Mac has no results of this corpus: run bash ~/Dev/agents/minimaxon/in/gate/run.sh there and the gate again")
	return nil
}

// macCorpus runs the arm64-macos corpus and the reruns of the programs that schedule behaviours through cmd/batchrun in a
// mode, and gives what they wrote and the first failure.
func macCorpus(compiler, batchrun, mode, stage string, behavioursArgs, testArgs []string) (string, error) {
	runner := "--runner=" + batchrun + "," + mode + "," + stage
	out, first := output(nil, "", compiler, slices.Concat([]string{"test", "--target=arm64-macos", "--verify-ir", runner}, testArgs)...)
	more, err := output(nil, "", compiler, slices.Concat([]string{"test", "--behaviours", "--target=arm64-macos", runner}, behavioursArgs, testArgs)...)
	if first == nil {
		first = err
	}
	return out + more, first
}

// rowStamp names the rows of a recorded batch, in order, which the Mac's results carry to say which batch they are of.
func rowStamp(rows string) (string, error) {
	entries, err := os.ReadDir(rows)
	if err != nil {
		return "", err
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return strings.Join(names, "\n"), nil
}

func robocopy(src, dst, mode string) error {
	cmd := exec.Command("robocopy", src, dst, mode, "/NFL", "/NDL", "/NJH", "/NJS", "/NP")
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() < robocopyFailed {
		return nil
	}
	return err
}
