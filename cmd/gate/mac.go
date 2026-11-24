package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// macShare is the folder that the Windows machine and the Mac both mount, the Mac's ~/Dev/agents/minimaxon, through which
// the gate reaches the Mac.
const macShare = `M:\agents\minimaxon`

// robocopyFailed is the least exit code of robocopy that says it failed; below it, the code says what it copied.
const robocopyFailed = 8

// macAnswerTime bounds a stat or a read of a small file on the share: a mapped drive whose Mac is asleep, busy or away
// keeps such a call waiting for the network's own timeout, far longer than the gate should wait for a machine that is not
// its own.
const macAnswerTime = 3 * time.Second

// macCopyTime bounds one robocopy to or from the share. The batches are the compiled corpus (a few hundred executables and
// their rows) and, for the seed, the sources and one compiler: tens of megabytes, which a healthy share moves in seconds,
// so two minutes leaves room for a slow network and still ends a share that has stopped answering.
const macCopyTime = 2 * time.Minute

// robocopyKillDelay is how long a killed robocopy gets to let go of its output before Wait gives up on it: a process
// stuck in a call to the share does not die when it is killed, and Wait would otherwise wait for it.
const robocopyKillDelay = time.Second

// shareTimeout is the error of an operation on the Mac's share that did not finish in time.
type shareTimeout struct {
	operation string
	limit     time.Duration
}

func (e *shareTimeout) Error() string {
	return fmt.Sprintf("the Mac's share %s did not answer within %s: %s", macShare, e.limit, e.operation)
}

// shareDo runs an operation on the share and gives up on it after limit. The operation keeps running in its own goroutine
// when it is abandoned, since a call blocked in the network cannot be interrupted; it only writes to its buffered channel.
func shareDo[T any](operation string, limit time.Duration, do func() (T, error)) (T, error) {
	type result struct {
		value T
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := do()
		done <- result{value, err}
	}()
	select {
	case r := <-done:
		return r.value, r.err
	case <-time.After(limit):
		var zero T
		return zero, &shareTimeout{operation, limit}
	}
}

// shareIsDir says whether path on the share is a directory.
func shareIsDir(path string) (bool, error) {
	return shareDo("looking at "+path, macAnswerTime, func() (bool, error) { return isDir(path), nil })
}

// shareReadFile reads a small file of the share.
func shareReadFile(path string) ([]byte, error) {
	return shareDo("reading "+path, macAnswerTime, func() ([]byte, error) { return os.ReadFile(path) })
}

// shareCopy runs robocopy for a copy to or from the share, and kills it when it passes macCopyTime.
func shareCopy(src, dst, mode string) error {
	ctx, cancel := context.WithTimeout(context.Background(), macCopyTime)
	defer cancel()
	err := robocopy(ctx, src, dst, mode)
	if ctx.Err() != nil {
		return &shareTimeout{fmt.Sprintf("copying %s to %s (robocopy %s)", src, dst, mode), macCopyTime}
	}
	return err
}

// isShareTimeout says whether err is, or wraps, a timeout of an operation on the share.
func isShareTimeout(err error) bool {
	var timeout *shareTimeout
	return errors.As(err, &timeout)
}

// macUnavailable is why the Mac cannot be reached through the share now, or "" when it can.
func macUnavailable() string {
	mounted, err := shareIsDir(filepath.Join(macShare, "in"))
	if err != nil {
		return err.Error()
	}
	if !mounted {
		return "the Mac's share " + macShare + " is not mounted"
	}
	return ""
}

// macLane runs the arm64-macos corpus on the Mac: the gate compiles every program and the reruns of those that schedule
// behaviours, keeping each executable through cmd/batchrun, and when the share holds the Mac's results of exactly those
// executables (the stamp of their rows), replays them through the test runner, which compares them, and fails on any
// failure. Otherwise it stages the batch at in\gate for the Mac session to run (bash ~/Dev/agents/minimaxon/in/gate/run.sh)
// and skips, as it does when the Mac cannot be reached: the Mac is not the gate's machine.
func macLane(h host, compiler string, behavioursArgs, testArgs []string) error {
	if !h.windows() {
		skip("corpus on arm64-macos", "the Mac is reached through the share "+macShare+" that only the Windows machine mounts")
		return nil
	}
	if why := macUnavailable(); why != "" {
		skip("corpus on arm64-macos", why)
		return nil
	}
	// A share that stops answering after the first look is the Mac being away, which skips the lane as it does there.
	err := macRun(compiler, behavioursArgs, testArgs)
	if timeout, ok := errors.AsType[*shareTimeout](err); ok {
		skip("corpus on arm64-macos", timeout.Error())
		return nil
	}
	return err
}

// macRun is the lane's work once the share has answered.
func macRun(compiler string, behavioursArgs, testArgs []string) error {
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
	have, err := shareReadFile(filepath.Join(results, "stamp.txt"))
	if isShareTimeout(err) {
		return err
	}
	if err == nil && strings.TrimRight(string(have), "\r\n") == stamp {
		replay := filepath.Join(root, "results")
		if err := shareCopy(filepath.Join(results, "out"), filepath.Join(replay, "out"), "/E"); err != nil {
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
	if err := shareCopy(stage, batch, "/MIR"); err != nil {
		return fmt.Errorf("mac lane: cannot stage the batch at %s: %w", batch, err)
	}
	skip("corpus on arm64-macos", "the Mac has no results of this corpus: run bash ~/Dev/agents/minimaxon/in/gate/run.sh there and the gate again")
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

// robocopy copies a tree, and kills robocopy when ctx ends. Its exit codes below robocopyFailed say what it copied.
func robocopy(ctx context.Context, src, dst, mode string) error {
	cmd := exec.CommandContext(ctx, "robocopy", src, dst, mode, "/NFL", "/NDL", "/NJH", "/NJS", "/NP")
	cmd.WaitDelay = robocopyKillDelay
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() < robocopyFailed {
		return nil
	}
	return err
}
