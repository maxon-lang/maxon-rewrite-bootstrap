package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestFmtFormatsFilesUnderADirectoryInPlace(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"messy.maxon": "let   a=1\nlet b = 2  // c\n",
		"clean.maxon": "let a = 1\n",
		"doc.md":      "text `let  x=1`\n\n```maxon\nlet   a=1\n```\n",
	})
	if code := formatFiles([]string{dir}, false); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := readFile(t, filepath.Join(dir, "messy.maxon")); got != "let a = 1\n\nlet b = 2 // c\n" {
		t.Errorf("messy.maxon: %q", got)
	}
	if got := readFile(t, filepath.Join(dir, "doc.md")); got != "text `let  x=1`\n\n```maxon\nlet a = 1\n```\n" {
		t.Errorf("doc.md: %q", got)
	}
}

func TestFmtCheckChangesNothingAndFailsOnAnUnformattedFile(t *testing.T) {
	const messy = "let   a=1\n"
	dir := writeFiles(t, map[string]string{"messy.maxon": messy})
	if code := formatFiles([]string{dir}, true); code != 1 {
		t.Errorf("--check exits %d on an unformatted file, want 1", code)
	}
	if got := readFile(t, filepath.Join(dir, "messy.maxon")); got != messy {
		t.Errorf("--check changed the file to %q", got)
	}
	if code := formatFiles([]string{dir}, false); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if code := formatFiles([]string{dir}, true); code != 0 {
		t.Errorf("--check exits %d on formatted files, want 0", code)
	}
}

func TestFmtLeavesLayoutTestsAndUnparsableFilesAlone(t *testing.T) {
	files := map[string]string{
		"lexical.maxon":  "// expect-error: E2001\nlet   a=1\n",
		"layout.maxon":   "// expect-error: E2010 at 2:5\nlet   a=1\n",
		"broken.maxon":   "let   = 1\n",
		"explicit.maxon": "// expect-error: E2010\nlet   a=1\n",
	}
	dir := writeFiles(t, files)
	if code := formatFiles([]string{dir}, false); code != 0 {
		t.Errorf("a directory with files that do not parse exits %d, want 0", code)
	}
	for _, name := range []string{"lexical.maxon", "layout.maxon", "broken.maxon", "explicit.maxon"} {
		if got := readFile(t, filepath.Join(dir, name)); got != files[name] {
			t.Errorf("%s changed to %q", name, got)
		}
	}
	// A file named on the command line is formatted even if it tests the layout, and is an error if it does not parse.
	if code := formatFiles([]string{filepath.Join(dir, "explicit.maxon")}, false); code != 0 {
		t.Errorf("exit %d", code)
	}
	if got := readFile(t, filepath.Join(dir, "explicit.maxon")); !strings.HasSuffix(got, "let a = 1\n") {
		t.Errorf("explicit.maxon: %q", got)
	}
	if code := formatFiles([]string{filepath.Join(dir, "broken.maxon")}, false); code != 1 {
		t.Errorf("a named file that does not parse exits %d, want 1", code)
	}
}
