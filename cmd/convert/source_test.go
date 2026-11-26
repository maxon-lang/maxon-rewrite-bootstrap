package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"minimaxon/internal/specsource"
)

// gitIn runs git in dir, failing the test when it fails, and skips the test where there is no git.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, text := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// convertSpecsTo runs `convert --specs --out=out in` with extra flags first, and gives its exit code and standard error.
func convertSpecsTo(out, in string, flags ...string) (int, string) {
	var stdout, stderr bytes.Buffer
	code := runMain(append(append([]string{"--specs", "--out=" + out}, flags...), in), strings.NewReader(""), &stdout, &stderr)
	return code, stderr.String()
}

func readPin(t *testing.T, out string) specsource.Pin {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, specsource.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "\n") != 1 {
		t.Fatalf("the pin is one line, got %q", data)
	}
	pin, err := specsource.Parse(string(data))
	if err != nil {
		t.Fatal(err)
	}
	return pin
}

// The tree hash of a directory is git's, so an archive and a checkout of the same specs give the same one.
func TestSpecSourceTreeHashIsGits(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"demo.md": specFile, "a.md": "a\n", "sub/b.md": "b\n", "sub.md": "s\n", "z/.hidden": "h\n"})
	if err := os.MkdirAll(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "a.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := specsource.TreeHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "config", "core.autocrlf", "false")
	gitIn(t, dir, "add", "-A")
	if want := gitIn(t, dir, "write-tree"); got != want {
		t.Fatalf("tree hash %s, git's %s", got, want)
	}
}

// A conversion pins its source; the same content passes again, other content is refused naming both, and the flag moves the
// pin on purpose.
func TestSpecsConversionRefusesAnotherSourceUnlessAdvanced(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in"), filepath.Join(dir, "out")
	writeFiles(t, in, map[string]string{"demo.md": specFile})
	if code, stderr := convertSpecsTo(out, in); code != 0 {
		t.Fatalf("first conversion: code=%d stderr=%q", code, stderr)
	}
	first := readPin(t, out)
	if want, _ := specsource.TreeHash(in); first.Tree != want || first.Commit != "unknown" {
		t.Fatalf("an archive's pin is its tree with no commit, got %v, tree %s", first, want)
	}
	if code, stderr := convertSpecsTo(out, in); code != 0 {
		t.Fatalf("the same source: code=%d stderr=%q", code, stderr)
	}

	writeFiles(t, in, map[string]string{"more.md": "## Tests\n"})
	moved, _ := specsource.TreeHash(in)
	if err := os.Remove(filepath.Join(out, "demo.md")); err != nil {
		t.Fatal(err)
	}
	code, stderr := convertSpecsTo(out, in)
	if code == 0 || !strings.Contains(stderr, first.Tree) || !strings.Contains(stderr, moved) || !strings.Contains(stderr, specsource.AdvanceFlag) {
		t.Fatalf("another source is refused, naming both trees and the flag: code=%d stderr=%q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(out, "demo.md")); err == nil {
		t.Fatal("a refused conversion writes nothing")
	}
	if readPin(t, out) != first {
		t.Fatal("a refused conversion keeps the pin")
	}

	if code, stderr := convertSpecsTo(out, in, specsource.AdvanceFlag); code != 0 {
		t.Fatalf("an advanced conversion: code=%d stderr=%q", code, stderr)
	}
	if got := readPin(t, out); got.Tree != moved {
		t.Fatalf("an advanced conversion pins the new tree %s, got %v", moved, got)
	}
	if code, _ := convertSpecsTo(out, in, specsource.AdvanceFlag, "--check"); code == 0 {
		t.Fatal("the flag is refused without --out")
	}
}

// The pin compares content: a checkout of a later commit whose specs are the same passes and keeps the line, and a clean
// checkout's pin names its commit.
func TestSpecsConversionPassesALaterCommitOfTheSameSpecs(t *testing.T) {
	repo := t.TempDir()
	specs, out := filepath.Join(repo, "specs"), filepath.Join(t.TempDir(), "out")
	writeFiles(t, repo, map[string]string{"specs/demo.md": specFile, "README.md": "one\n"})
	gitIn(t, repo, "init", "-q")
	gitIn(t, repo, "config", "core.autocrlf", "false")
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "-m", "one")
	head := gitIn(t, repo, "rev-parse", "HEAD")
	if code, stderr := convertSpecsTo(out, specs); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	first := readPin(t, out)
	if first.Commit != head || first.Tree != gitIn(t, repo, "rev-parse", "HEAD:specs") {
		t.Fatalf("a clean checkout's pin is git's tree and HEAD %s, got %v", head, first)
	}
	writeFiles(t, repo, map[string]string{"README.md": "two\n"})
	gitIn(t, repo, "commit", "-q", "-am", "two")
	if code, stderr := convertSpecsTo(out, specs); code != 0 {
		t.Fatalf("a later commit of the same specs: code=%d stderr=%q", code, stderr)
	}
	if readPin(t, out) != first {
		t.Fatal("the same content keeps the recorded line")
	}
	// A working directory that differs from HEAD is pinned by what it holds, with no commit.
	writeFiles(t, repo, map[string]string{"specs/demo.md": specFile + "\n"})
	if code, _ := convertSpecsTo(out, specs, specsource.AdvanceFlag); code != 0 || readPin(t, out).Commit != "unknown" {
		t.Fatalf("code=%d pin=%v", code, readPin(t, out))
	}
}
