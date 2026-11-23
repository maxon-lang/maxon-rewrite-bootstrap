package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// stepTime is how long a step took.
type stepTime struct {
	name    string
	elapsed time.Duration
}

// stepTimes are the times of the steps run so far, which every step prints when it ends and the run ends with, the longest
// first: one table for a whole run, the steps of the lanes it started among them.
var (
	stepTimes []stepTime
	runClock  = time.Now()
)

// sinceRun is the time since the run began.
func sinceRun() time.Duration { return time.Since(runClock) }

// bytesTrim is text without the white space around it.
func bytesTrim(text []byte) []byte { return bytes.TrimSpace(text) }

func formatSeconds(d time.Duration) string {
	return fmt.Sprintf("%7.1f s", d.Seconds())
}

// timed runs a step, records its time and prints it.
func timed(name string, run func() error) error {
	start := time.Now()
	err := run()
	elapsed := time.Since(start)
	stepTimes = append(stepTimes, stepTime{name, elapsed})
	fmt.Printf("[%s] %s\n", formatSeconds(elapsed), name)
	return err
}

// printStepTable prints the steps run so far, the longest first, and the time since the run began.
func printStepTable() {
	sorted := slices.Clone(stepTimes)
	slices.SortStableFunc(sorted, func(a, b stepTime) int { return int(b.elapsed - a.elapsed) })
	fmt.Println()
	fmt.Println("steps by time:")
	for _, s := range sorted {
		fmt.Printf("  %s  %s\n", formatSeconds(s.elapsed), s.name)
	}
	fmt.Printf("  %s  total\n", formatSeconds(time.Since(runClock)))
}

// writeStepTimes writes the times of the steps run so far to a file, a line each, for the process that started this one.
func writeStepTimes(path string) error {
	var b strings.Builder
	for _, s := range stepTimes {
		fmt.Fprintf(&b, "%s\t%d\n", s.name, s.elapsed.Nanoseconds())
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// readStepTimes adds to the table the times a lane wrote.
func readStepTimes(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		name, nanos, ok := strings.Cut(line, "\t")
		n, err := strconv.ParseInt(nanos, 10, 64)
		if ok && err == nil {
			stepTimes = append(stepTimes, stepTime{name, time.Duration(n)})
		}
	}
}

// Stamps: how the gate tells that a result it kept (the results of the spec tests, the specs the conversion wrote) is of
// the inputs it has now. A stamp is the digest of every byte an output follows, and a kept result is used only when its
// stamp is equal to the current one; anything else, or no stamp, redoes the work.

// pathsDigest is the SHA-256 of the files at paths (a directory stands for every file below it) in the order of their full
// names, each with its name and length so that a moved or split file changes it. Files named in exclude are left out.
func pathsDigest(paths []string, exclude []string) (string, error) {
	var files []string
	for _, path := range paths {
		full := abs(path)
		info, err := os.Stat(full)
		if err != nil {
			return "", fmt.Errorf("stamp: %s does not exist", path)
		}
		if !info.IsDir() {
			files = append(files, full)
			continue
		}
		err = filepath.WalkDir(full, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && !slices.Contains(exclude, p) {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	slices.Sort(files)
	h := sha256.New()
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", f, len(data))
		h.Write(data)
	}
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil))), nil
}

// fileDigest is the SHA-256 of one file.
func fileDigest(path string) (string, error) {
	return pathsDigest([]string{path}, nil)
}

// stampID is a short name of a stamp, to say which one a result was kept under.
func stampID(stamp string) string {
	sum := sha256.Sum256([]byte(stamp))
	return hex.EncodeToString(sum[:])[:12]
}

// stampHolds is whether the stamp kept in the file is the one given.
func stampHolds(file, stamp string) bool {
	kept, err := os.ReadFile(file)
	return err == nil && string(kept) == stamp
}
