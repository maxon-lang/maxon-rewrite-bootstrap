// Command batchrun runs the executables of a test run on another machine in one batch, as the plan's batch design has
// it: the test runner compiles every program here and starts each executable through `batchrun record`, which keeps the
// executable and a row of the run in a staging directory instead of running it; `batchrun run` runs every row there, on
// the machine of the target, and writes what each printed and how it ended; and a second run of the test runner starts
// each executable through `batchrun replay`, which gives back what the same executable printed there, so that every
// comparison stays the runner's. An executable is known by the digest of its bytes, which a deterministic compile gives
// again (of a Mach-O, the bytes its signature covers, since the signature names the file the compile wrote), and the
// worker count its run was given (MAXON_MAX_PROCS); the runs of one executable under one count are told
// apart by the order they were asked for, which does not matter, since they are the same program under the same count.
//
//	batchrun record <stage> <exe>       keep <exe> in <stage>/bin and a row in <stage>/rows, and the standard input it is
//	                                    given, when there is any, in <stage>/stdin, which its runs read
//	batchrun run [-j N] [-deadline D] [-repeat N] <stage>
//	                                    run every row, writing <stage>/out/<run>.stdout, .stderr and .status
//	batchrun replay <stage> <exe>       print what the row of <exe> printed, and exit as it did
package main

import (
	"bytes"
	"crypto/sha256"
	"debug/macho"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// workerCountVariable is the environment variable the runtime reads its worker count from.
const workerCountVariable = "MAXON_MAX_PROCS"

// stdinDir is the directory of a stage that holds the standard input of each row that was given one, under the row's name.
const stdinDir = "stdin"

// digestChars is how much of an executable's digest names it.
const digestChars = 16

// The exit codes of a replay that has no result to give, of one whose run was killed at its deadline, as timeout(1)
// exits, and of one whose executable could not be started, as a shell's; a run that a signal ended replays as a shell
// reports it, 128 plus the signal.
const (
	noResultExit    = 125
	timedOutExit    = 124
	startFailedExit = 126
	signalExitBase  = 128
	defaultDeadline = 10 * time.Second
	// defaultRepeats is the test runner's runs of a program that schedules behaviours.
	defaultRepeats = 2
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "record":
		err = record(os.Args[2:])
	case "run":
		err = run(os.Args[2:])
	case "replay":
		var code int
		code, err = replay(os.Args[2:])
		if err == nil {
			os.Exit(code)
		}
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "batchrun:", err)
		os.Exit(noResultExit)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: batchrun record <stage> <exe> | run [-j N] [-deadline D] <stage> | replay <stage> <exe>")
	os.Exit(2)
}

// rowKey is the name of the rows of an executable's runs under the worker count of this process's environment.
func rowKey(exe string) (digest, key string, err error) {
	file, err := os.ReadFile(exe)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(signedPart(file))
	digest = hex.EncodeToString(sum[:])[:digestChars]
	procs := os.Getenv(workerCountVariable)
	if procs == "" {
		procs = "default"
	}
	return digest, digest + "-p" + procs, nil
}

// lcCodeSignature is the load command of a Mach-O's code signature, which follows the 64-bit header and holds the
// offset of the signature in the file and its size.
const (
	lcCodeSignature     = 0x1d
	machoHeaderBytes    = 32
	linkeditOffsetField = 8
	linkeditSizeField   = 12
)

// signedPart is the bytes of an executable that name it: those of a Mach-O before its code signature, whose identifier is
// the name of the file it was written to, with the signature's size in its load command left out, since it grows with
// that name; and the whole of any other.
func signedPart(file []byte) []byte {
	f, err := macho.NewFile(bytes.NewReader(file))
	if err != nil {
		return file
	}
	defer f.Close()
	at := machoHeaderBytes
	for _, l := range f.Loads {
		raw := l.Raw()
		if f.ByteOrder.Uint32(raw) == lcCodeSignature {
			end := int(f.ByteOrder.Uint32(raw[linkeditOffsetField:]))
			if end > len(file) || at+linkeditSizeField+4 > end {
				return file
			}
			part := slices.Clone(file[:end])
			clear(part[at+linkeditSizeField : at+linkeditSizeField+4])
			return part
		}
		at += len(raw)
	}
	return file
}

// claim takes the first free occurrence of a key in a directory, by making its file there, which only one process can.
func claim(dir, key string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	for n := 0; ; n++ {
		name := key + "-" + strconv.Itoa(n)
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		return name, f.Close()
	}
}

func record(args []string) error {
	if len(args) != 2 {
		usage()
	}
	stage, exe := args[0], args[1]
	digest, key, err := rowKey(exe)
	if err != nil {
		return err
	}
	bin := filepath.Join(stage, "bin", digest+filepath.Ext(exe))
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(bin); errors.Is(err, os.ErrNotExist) {
		if err := copyFile(exe, bin); err != nil {
			return err
		}
	}
	row, err := claim(filepath.Join(stage, "rows"), key)
	if err != nil {
		return err
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	if len(input) > 0 {
		if err := os.MkdirAll(filepath.Join(stage, stdinDir), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(stage, stdinDir, row), input, 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(stage, "rows", row), []byte(filepath.Base(bin)+"\t"+os.Getenv(workerCountVariable)+"\n"), 0o644)
}

func copyFile(from, to string) error {
	src, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	tmp := to + ".part"
	if err := os.WriteFile(tmp, src, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, to)
}

func replay(args []string) (int, error) {
	if len(args) != 2 {
		usage()
	}
	stage, exe := args[0], args[1]
	_, key, err := rowKey(exe)
	if err != nil {
		return 0, err
	}
	row, err := claim(filepath.Join(stage, "claims"), key)
	if err != nil {
		return 0, err
	}
	out := filepath.Join(stage, "out", row)
	status, err := os.ReadFile(out + ".status")
	if err != nil {
		return 0, fmt.Errorf("no result of %s: %w", row, err)
	}
	for _, stream := range []struct {
		suffix string
		to     io.Writer
	}{{".stdout", os.Stdout}, {".stderr", os.Stderr}} {
		text, err := os.ReadFile(out + stream.suffix)
		if err != nil {
			return 0, err
		}
		if _, err := stream.to.Write(text); err != nil {
			return 0, err
		}
	}
	return exitOf(string(status))
}

// exitOf is the exit code a status file says the run ended with.
func exitOf(status string) (int, error) {
	fields := strings.Fields(status)
	if len(fields) < 1 {
		return 0, fmt.Errorf("an empty status")
	}
	switch fields[0] {
	case "exit", "signal":
		if len(fields) < 2 {
			return 0, fmt.Errorf("a status %q with no number", status)
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil {
			return 0, err
		}
		if fields[0] == "signal" {
			fmt.Fprintf(os.Stderr, "batchrun: the run was ended by signal %d\n", n)
			return signalExitBase + n, nil
		}
		return n, nil
	case "timeout":
		fmt.Fprintln(os.Stderr, "batchrun: the run passed its deadline and was killed")
		return timedOutExit, nil
	}
	return 0, fmt.Errorf("a status %q of no kind", status)
}

func run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	jobs := fs.Int("j", runtime.NumCPU(), "how many rows run at once")
	deadline := fs.Duration("deadline", defaultDeadline, "how long a run may take before it is killed")
	repeat := fs.Int("repeat", defaultRepeats, "how many runs each executable gets under each worker count, at least")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		usage()
	}
	stage := fs.Arg(0)
	entries, err := os.ReadDir(filepath.Join(stage, "rows"))
	if err != nil {
		return err
	}
	recorded := map[string]int{}
	for _, e := range entries {
		at := strings.LastIndexByte(e.Name(), '-')
		if at < 0 {
			return fmt.Errorf("a row %s of no occurrence", e.Name())
		}
		recorded[e.Name()[:at]]++
	}
	var keys []string
	for key := range recorded {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	var rows []job
	for _, key := range keys {
		for n := range max(recorded[key], *repeat) {
			rows = append(rows, job{row: key + "-" + strconv.Itoa(min(n, recorded[key]-1)), out: key + "-" + strconv.Itoa(n)})
		}
	}
	if err := os.MkdirAll(filepath.Join(stage, "out"), 0o755); err != nil {
		return err
	}
	work := make(chan job)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failures []string
	for range max(*jobs, 1) {
		wg.Go(func() {
			for row := range work {
				if err := runRow(stage, row.row, row.out, *deadline); err != nil {
					mu.Lock()
					failures = append(failures, row.out+": "+err.Error())
					mu.Unlock()
				}
			}
		})
	}
	for _, row := range rows {
		work <- row
	}
	close(work)
	wg.Wait()
	fmt.Printf("batchrun: ran %d rows\n", len(rows))
	if len(failures) > 0 {
		slices.Sort(failures)
		return errors.New(strings.Join(failures, "\n"))
	}
	return nil
}

// job is a run of a row, and the name of its results: the runner stops asking for runs of an executable at the first
// that fails, which every run of a record does, so a row recorded once is run as often as -repeat says, and its later
// runs are its later occurrences.
type job struct {
	row, out string
}

// runRow runs one row and writes what it printed and how it ended under the name of the run.
func runRow(stage, row, name string, deadline time.Duration) error {
	text, err := os.ReadFile(filepath.Join(stage, "rows", row))
	if err != nil {
		return err
	}
	fields := strings.Split(strings.TrimRight(string(text), "\n"), "\t")
	if len(fields) != 2 {
		return fmt.Errorf("a row of %d fields", len(fields))
	}
	bin, err := filepath.Abs(filepath.Join(stage, "bin", fields[0]))
	if err != nil {
		return err
	}
	out := filepath.Join(stage, "out", name)
	stdout, err := os.Create(out + ".stdout")
	if err != nil {
		return err
	}
	defer stdout.Close()
	stderr, err := os.Create(out + ".stderr")
	if err != nil {
		return err
	}
	defer stderr.Close()
	cmd := exec.Command(bin)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if input, err := os.Open(filepath.Join(stage, stdinDir, row)); err == nil {
		defer input.Close()
		cmd.Stdin = input
	}
	cmd.Env = os.Environ()
	if fields[1] != "" {
		cmd.Env = append(cmd.Env, workerCountVariable+"="+fields[1])
	}
	inGroup(cmd)
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return os.WriteFile(out+".status", []byte("exit "+strconv.Itoa(startFailedExit)+"\nstart "+err.Error()+"\n"), 0o644)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var status string
	select {
	case <-done:
		status = statusOf(cmd.ProcessState)
	case <-time.After(deadline):
		killGroup(cmd)
		<-done
		status = "timeout"
	}
	status += fmt.Sprintf("\nseconds %.3f\n", time.Since(start).Seconds())
	if !strings.HasPrefix(status, "exit ") {
		writeSystemLog(out+".log", fields[0])
	}
	return os.WriteFile(out+".status", []byte(status), 0o644)
}

// statusOf is how a finished run ended: its exit code, or the signal that ended it.
func statusOf(state *os.ProcessState) string {
	if signal, ok := signalOf(state); ok {
		return "signal " + strconv.Itoa(signal)
	}
	return "exit " + strconv.Itoa(state.ExitCode())
}
