package main

import (
	"minimaxon/internal/cache"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// useCacheDirectory points the build cache at a temporary directory for one test, so that no test ever reads or makes the
// user's.
func useCacheDirectory(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "cache")
	t.Setenv(cacheEnvironment, dir)
	return dir
}

// storeOldEntry puts an entry that was last used the given time ago in the cache, and says where its file is.
func storeOldEntry(t *testing.T, dir string, name string, age time.Duration) string {
	t.Helper()
	store := cache.Open(dir, cache.Key{})
	key := cache.Digest([]byte(name))
	if err := store.Put(key, []byte(name)); err != nil {
		t.Fatal(err)
	}
	hex := key.String()
	file := filepath.Join(dir, hex[:2], hex[2:])
	when := time.Now().Add(-age)
	if err := os.Chtimes(file, when, when); err != nil {
		t.Fatal(err)
	}
	return file
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func captureCache(args ...string) (code int, stdout, stderr string) {
	var out, errOut strings.Builder
	code = cacheCommand(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestCacheShowsWhereTheCacheIsAndWhatItHolds(t *testing.T) {
	dir := useCacheDirectory(t)
	code, out, _ := captureCache()
	if code != 0 || !strings.Contains(out, dir) || !strings.Contains(out, "entries: 0") || !strings.Contains(out, "last trimmed: never") {
		t.Errorf("an empty cache: %d %q", code, out)
	}
	storeOldEntry(t, dir, "one", time.Hour)
	storeOldEntry(t, dir, "two", time.Hour)
	if code, out, _ := captureCache(); code != 0 || !strings.Contains(out, "entries: 2") {
		t.Errorf("two entries: %d %q", code, out)
	}
}

func TestCacheTrimRemovesWhatWasNotUsedForDaysAndCacheCleanRemovesAll(t *testing.T) {
	dir := useCacheDirectory(t)
	old := storeOldEntry(t, dir, "old", 30*24*time.Hour)
	fresh := storeOldEntry(t, dir, "fresh", time.Hour)
	if code, out, _ := captureCache("trim"); code != 0 || !strings.Contains(out, "removed 1 files") {
		t.Errorf("trim: %d %q", code, out)
	}
	if exists(old) || !exists(fresh) {
		t.Errorf("after trim the old entry is there: %v, the fresh one: %v", exists(old), exists(fresh))
	}
	if _, out, _ := captureCache(); strings.Contains(out, "never") {
		t.Errorf("a trim leaves no record: %q", out)
	}
	if code, out, _ := captureCache("clean"); code != 0 || !strings.Contains(out, "removed 1 entries") {
		t.Errorf("clean: %d %q", code, out)
	}
	if exists(fresh) {
		t.Error("clean left an entry")
	}
	if code, _, errOut := captureCache("polish"); code != usageExitCode || !strings.Contains(errOut, "usage") {
		t.Errorf("an unknown command: %d %q", code, errOut)
	}
}

func TestCacheCommandsRefuseACacheThatIsOff(t *testing.T) {
	t.Setenv(cacheEnvironment, "off")
	for _, args := range [][]string{{}, {"trim"}, {"clean"}} {
		if code, _, errOut := captureCache(args...); code != 1 || !strings.Contains(errOut, "off") {
			t.Errorf("cache %v with the cache off: %d %q", args, code, errOut)
		}
	}
}

const trimmedProgram = "function main() returns ExitCode\n\treturn 0\nend 'main'\n"

// A build trims the cache after it makes the executable, at most once a day: what was not used for days goes, what the build
// wrote stays, and an old entry that is there on the next build of the same day is left.
func TestABuildTrimsTheCacheOnceADay(t *testing.T) {
	dir := useCacheDirectory(t)
	source := filepath.Join(writeFiles(t, map[string]string{"main.maxon": trimmedProgram}), "main.maxon")
	out := filepath.Join(t.TempDir(), "out.exe")
	old := storeOldEntry(t, dir, "old", 30*24*time.Hour)
	if code := run([]string{"build", source, "-o", out}); code != 0 {
		t.Fatalf("build exits %d", code)
	}
	if exists(old) {
		t.Error("the build did not trim the entry that was 30 days old")
	}
	if usage := cache.Open(dir, cache.Key{}).Usage(); usage.Entries != 1 || !usage.Trimmed {
		t.Errorf("after the build: %+v, want the module's entry and a record of the trim", usage)
	}
	again := storeOldEntry(t, dir, "again", 30*24*time.Hour)
	if code := run([]string{"build", source, "-o", out}); code != 0 {
		t.Fatalf("second build exits %d", code)
	}
	if !exists(again) {
		t.Error("a second build on the same day trimmed again")
	}
}

func TestARunTrimsTheCacheToo(t *testing.T) {
	dir := useCacheDirectory(t)
	source := filepath.Join(writeFiles(t, map[string]string{"main.maxon": trimmedProgram}), "main.maxon")
	old := storeOldEntry(t, dir, "old", 30*24*time.Hour)
	if code := run([]string{"run", source}); code != 0 {
		t.Fatalf("run exits %d", code)
	}
	if exists(old) {
		t.Error("the run did not trim the entry that was 30 days old")
	}
}

func TestACheckLeavesTheCacheAlone(t *testing.T) {
	dir := useCacheDirectory(t)
	source := filepath.Join(writeFiles(t, map[string]string{"main.maxon": trimmedProgram}), "main.maxon")
	old := storeOldEntry(t, dir, "old", 30*24*time.Hour)
	if code := run([]string{"check", source}); code != 0 {
		t.Fatalf("check exits %d", code)
	}
	if !exists(old) {
		t.Error("check trimmed the cache")
	}
}
