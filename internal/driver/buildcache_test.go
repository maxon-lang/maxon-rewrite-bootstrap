package driver

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minimaxon/internal/cache"
)

// testCompiler stands for the hash of the compiler that a cache is for; a test that builds with another one is a build with
// a compiler that changed.
func testCompiler(name string) cache.Key { return cache.Digest([]byte(name)) }

type cachedResult struct {
	exe, ir []byte
	stats   CacheStats
	errs    string
}

// cachedBuild compiles a project with the cache in dir, or with none if dir is empty.
func cachedBuild(t *testing.T, project, dir, compiler string, jobs int, mutate func(*Options)) cachedResult {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.exe")
	var r cachedResult
	options := Options{Jobs: jobs, EmitIr: true, CacheStats: &r.stats}
	if dir != "" {
		options.Cache = cache.Open(dir, testCompiler(compiler))
	}
	if mutate != nil {
		mutate(&options)
	}
	errs := Compile(project, out, options)
	var text strings.Builder
	for _, e := range errs {
		text.WriteString(e.Error())
		text.WriteByte('\n')
	}
	r.errs = text.String()
	if len(errs) == 0 {
		var err error
		if r.exe, err = os.ReadFile(out); err != nil {
			t.Fatal(err)
		}
		if options.EmitIr {
			if r.ir, err = os.ReadFile(out + ".ir"); err != nil {
				t.Fatal(err)
			}
		}
	}
	return r
}

// requireSameAsWithoutCache refuses a result that differs, in the executable, the IR or the errors, from a build of the same
// sources with no cache.
func requireSameAsWithoutCache(t *testing.T, project string, got cachedResult, jobs int) {
	t.Helper()
	want := cachedBuild(t, project, "", "", jobs, nil)
	if got.errs != want.errs {
		t.Errorf("the errors differ:\n%s\nwithout a cache:\n%s", got.errs, want.errs)
	}
	if !bytes.Equal(got.exe, want.exe) {
		t.Errorf("the executable differs from the build with no cache (%d and %d bytes)", len(got.exe), len(want.exe))
	}
	if !bytes.Equal(got.ir, want.ir) {
		t.Errorf("the IR differs from the build with no cache (%d and %d bytes)", len(got.ir), len(want.ir))
	}
}

func requireStats(t *testing.T, what string, got CacheStats, hits, misses, skipped int) {
	t.Helper()
	if got.Hits != hits || got.Misses != misses || got.ChecksSkipped != skipped {
		t.Errorf("%s: %d hits, %d misses, %d checks skipped; want %d, %d, %d", what, got.Hits, got.Misses, got.ChecksSkipped, hits, misses, skipped)
	}
}

// The root calls user, user calls the generics of lib, and other is a module that nothing uses, so modules are of every
// kind an edit can reach: the root and user depend on lib, nothing depends on the root or on other.
func cacheProject(t *testing.T) string {
	return writeProject(t, map[string]string{
		"main.maxon": instanceMain, "lib/lib.maxon": instanceLibrary, "user/user.maxon": instanceUser,
		"other/other.maxon": instanceOther,
	})
}

func editFile(t *testing.T, project, name, from, to string) {
	t.Helper()
	path := filepath.Join(project, filepath.FromSlash(name))
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), from) {
		t.Fatalf("%s has no %q", name, from)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(text), from, to, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A second compile of the same program takes every module from the cache and runs no check, and makes the executable and the
// IR that the first made, for any number of jobs.
func TestAWarmBuildTakesEveryModuleFromTheCache(t *testing.T) {
	project, dir := cacheProject(t), t.TempDir()
	cold := cachedBuild(t, project, dir, "c1", 1, nil)
	requireStats(t, "cold", cold.stats, 0, 4, 0)
	requireSameAsWithoutCache(t, project, cold, 1)
	for _, jobs := range []int{1, 2, 7} {
		warm := cachedBuild(t, project, dir, "c1", jobs, nil)
		requireStats(t, "warm", warm.stats, 4, 0, 4)
		if warm.stats.BytesRead == 0 || warm.stats.BytesWritten != 0 {
			t.Errorf("jobs=%d: the warm build read %d bytes and wrote %d", jobs, warm.stats.BytesRead, warm.stats.BytesWritten)
		}
		if !bytes.Equal(warm.exe, cold.exe) || !bytes.Equal(warm.ir, cold.ir) {
			t.Errorf("jobs=%d: the warm build differs from the cold one", jobs)
		}
	}
}

// An edit to the body of a function that is not generic makes a miss of its module alone, since no other module can read it, and
// a check runs for a miss and for every module that a miss reaches that shows its check results; the others come from the cache
// and run nothing. An edit to a generic is another module's code too, so it makes a miss of every module that reaches it. The
// result is the build with no cache.
func TestAnEditRebuildsOnlyTheModuleAndTheModulesThatReachIt(t *testing.T) {
	for _, jobs := range []int{1, 5} {
		project, dir := cacheProject(t), t.TempDir()
		cachedBuild(t, project, dir, "c1", jobs, nil)

		// user is edited: it alone is rebuilt, since the root calls it and reads nothing of its body, and lib is checked because
		// user reaches it and has generics. The root and other are neither.
		editFile(t, project, "user/user.maxon", "lib.deep(3 as Count)", "lib.deep(4 as Count)")
		got := cachedBuild(t, project, dir, "c1", jobs, nil)
		requireStats(t, "user edited", got.stats, 3, 1, 2)
		requireSameAsWithoutCache(t, project, got, jobs)

		// other is edited and nothing depends on it: it alone is rebuilt, and lib is checked for it.
		editFile(t, project, "other/other.maxon", "lib.deep(4 as Count)", "lib.deep(5 as Count)")
		got = cachedBuild(t, project, dir, "c1", jobs, nil)
		requireStats(t, "other edited", got.stats, 3, 1, 2)
		requireSameAsWithoutCache(t, project, got, jobs)

		// lib is edited in a generic, which every other module lowers an instance of.
		editFile(t, project, "lib/lib.maxon", "return wrap(x)", "return Box.make(x)")
		got = cachedBuild(t, project, dir, "c1", jobs, nil)
		requireStats(t, "lib edited", got.stats, 0, 4, 0)
		requireSameAsWithoutCache(t, project, got, jobs)

		got = cachedBuild(t, project, dir, "c1", jobs, nil)
		requireStats(t, "nothing edited", got.stats, 4, 0, 4)
	}
}

// Writing the same text again, as an editor that saves or a checkout that touches a file does, changes nothing.
func TestARewrittenFileThatHoldsTheSameTextIsStillAHit(t *testing.T) {
	project, dir := cacheProject(t), t.TempDir()
	cachedBuild(t, project, dir, "c1", 2, nil)
	editFile(t, project, "lib/lib.maxon", "export type Box", "export type Box")
	requireStats(t, "touched", cachedBuild(t, project, dir, "c1", 2, nil).stats, 4, 0, 4)
}

// Adding a module and taking one away move every other module's handle, and change no other module's key.
func TestAddingAndRemovingAModuleChangesNoOtherModulesEntry(t *testing.T) {
	project, dir := cacheProject(t), t.TempDir()
	cachedBuild(t, project, dir, "c1", 2, nil)
	extra := filepath.Join(project, "aaa")
	if err := os.MkdirAll(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extra, "aaa.maxon"), []byte("export function unused() returns Count\n\treturn 1\nend 'unused'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := cachedBuild(t, project, dir, "c1", 2, nil)
	requireStats(t, "added", got.stats, 4, 1, 4)
	requireSameAsWithoutCache(t, project, got, 2)
	if err := os.RemoveAll(extra); err != nil {
		t.Fatal(err)
	}
	got = cachedBuild(t, project, dir, "c1", 2, nil)
	requireStats(t, "removed", got.stats, 4, 0, 4)
	requireSameAsWithoutCache(t, project, got, 2)
}

// The compiler is part of every key, and so is each flag that changes what a module compiles to; the number of jobs is not.
func TestTheCompilerAndTheFlagsThatChangeCodeAreInTheKey(t *testing.T) {
	project, dir := cacheProject(t), t.TempDir()
	cachedBuild(t, project, dir, "c1", 2, nil)
	requireStats(t, "another compiler", cachedBuild(t, project, dir, "c2", 2, nil).stats, 0, 4, 0)
	requireStats(t, "the first compiler again", cachedBuild(t, project, dir, "c1", 2, nil).stats, 4, 0, 4)
	counting := func(o *Options) { o.CountAllocations = true }
	got := cachedBuild(t, project, dir, "c1", 2, counting)
	requireStats(t, "counting allocations", got.stats, 0, 4, 0)
	requireStats(t, "counting allocations again", cachedBuild(t, project, dir, "c1", 3, counting).stats, 4, 0, 4)
	requireStats(t, "not counting", cachedBuild(t, project, dir, "c1", 1, nil).stats, 4, 0, 4)
	if bytes.Equal(got.exe, cachedBuild(t, project, dir, "c1", 2, nil).exe) {
		t.Error("an executable that counts allocations is the one that does not")
	}
}

// What a module is called is in its key and so is the text of every module it reaches, so the same text in another module, or a
// module that reaches other text, is another entry.
func TestAKeyHoldsTheModulesNameAndWhatItReaches(t *testing.T) {
	project, dir := cacheProject(t), t.TempDir()
	cachedBuild(t, project, dir, "c1", 2, nil)
	copied := writeProject(t, map[string]string{
		"main.maxon": instanceMain, "lib/lib.maxon": instanceLibrary, "user/user.maxon": instanceUser,
		"other/other.maxon": instanceOther,
	})
	// The same sources in another directory are the same entries: nothing in a key is where the project is.
	requireStats(t, "the same project elsewhere", cachedBuild(t, copied, dir, "c1", 2, nil).stats, 4, 0, 4)
	// A library that differs makes a miss of everything that reaches it, and only that.
	editFile(t, copied, "lib/lib.maxon", "return wrap(x)", "return Box.make(x)")
	got := cachedBuild(t, copied, dir, "c1", 2, nil)
	requireStats(t, "another library", got.stats, 0, 4, 0)
	requireSameAsWithoutCache(t, copied, got, 2)
}

// An entry that is not whole, or is another version's, is a miss and is written again; the program is the one without a cache.
func TestACorruptEntryIsAMissAndIsWrittenAgain(t *testing.T) {
	damage := map[string]func(entry []byte) []byte{
		"cut short":       func(e []byte) []byte { return e[:len(e)/2] },
		"empty":           func(e []byte) []byte { return nil },
		"a flipped byte":  func(e []byte) []byte { e[len(e)/2] ^= 0x10; return e },
		"another version": func(e []byte) []byte { binary.LittleEndian.PutUint32(e[4:], cache.Version+1); return e },
	}
	for name, damaged := range damage {
		project, dir := cacheProject(t), t.TempDir()
		cachedBuild(t, project, dir, "c1", 2, nil)
		var entries int
		filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			entry, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, damaged(entry), 0o644); err != nil {
				t.Fatal(err)
			}
			entries++
			return nil
		})
		if entries != 4 {
			t.Fatalf("%s: the cache holds %d entries, want 4", name, entries)
		}
		got := cachedBuild(t, project, dir, "c1", 2, nil)
		requireStats(t, name, got.stats, 0, 4, 0)
		requireSameAsWithoutCache(t, project, got, 2)
		requireStats(t, name+", then again", cachedBuild(t, project, dir, "c1", 2, nil).stats, 4, 0, 4)
	}
}

// An entry that is whole but holds something else than a module's code (written by a bug, or by a format that changed
// without a new version) is a miss too: the decoder refuses it instead of linking it.
func TestAnEntryThatHoldsNothingUsableIsAMiss(t *testing.T) {
	project, dir := cacheProject(t), t.TempDir()
	cachedBuild(t, project, dir, "c1", 2, nil)
	store := cache.Open(dir, testCompiler("c1"))
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			name := filepath.Base(filepath.Dir(path)) + filepath.Base(path)
			var key cache.Key
			for i := range key {
				var b byte
				for _, c := range name[2*i : 2*i+2] {
					b = b<<4 | byte(strings.IndexRune("0123456789abcdef", c))
				}
				key[i] = b
			}
			if err := store.Put(key, []byte("not a unit")); err != nil {
				t.Fatal(err)
			}
		}
		return nil
	})
	got := cachedBuild(t, project, dir, "c1", 2, nil)
	requireStats(t, "entries that are not units", got.stats, 0, 4, 0)
	requireSameAsWithoutCache(t, project, got, 2)
}

// A module whose dependency changed so that it no longer compiles is a miss, because the dependency is in its key: the build
// reports the errors of a build with no cache, and does not link what the module made before.
func TestAModuleThatNoLongerCompilesReportsTheErrorsOfABuildWithNoCache(t *testing.T) {
	project, dir := cacheProject(t), t.TempDir()
	cachedBuild(t, project, dir, "c1", 2, nil)
	editFile(t, project, "lib/lib.maxon", "export function deep(consumes x T) uses T returns Box with T", "export function deep(consumes x T, extra Count) uses T returns Box with T")
	for _, jobs := range []int{1, 4} {
		got := cachedBuild(t, project, dir, "c1", jobs, nil)
		if got.errs == "" || got.exe != nil {
			t.Fatalf("jobs=%d: the build of the program that no longer compiles gave an executable, errors %q", jobs, got.errs)
		}
		requireSameAsWithoutCache(t, project, got, jobs)
	}
}

// A build that does not ask for the IR leaves entries without it, so a build that does is a miss for each of them, and writes
// entries that serve both.
func TestABuildThatAsksForTheIrNeedsEntriesThatHoldIt(t *testing.T) {
	project, dir := cacheProject(t), t.TempDir()
	without := func(o *Options) { o.EmitIr = false }
	cachedBuild(t, project, dir, "c1", 2, without)
	requireStats(t, "without the IR, again", cachedBuild(t, project, dir, "c1", 2, without).stats, 4, 0, 4)
	got := cachedBuild(t, project, dir, "c1", 2, nil)
	requireStats(t, "with the IR", got.stats, 0, 4, 0)
	requireSameAsWithoutCache(t, project, got, 2)
	requireStats(t, "with the IR, again", cachedBuild(t, project, dir, "c1", 2, nil).stats, 4, 0, 4)
	requireStats(t, "without the IR, from entries that have it", cachedBuild(t, project, dir, "c1", 2, without).stats, 4, 0, 4)
}

// A module that the backend refuses has no entry, and the program has the errors it has without a cache, every time.
func TestARefusedModuleIsNeverCached(t *testing.T) {
	project := writeProject(t, map[string]string{
		"main.maxon": "function main() returns ExitCode\n\tprint(\"{usemap.run()}\\n\")\n\treturn 0\nend 'main'\n",
		"usemap/usemap.maxon": `typealias Counts = Map with Count, Count

export function run() returns Count
	var m = Counts.create()
	return m.count()
end 'run'
`,
		"fine/fine.maxon": "export function f() returns Count\n\treturn 1\nend 'f'\n",
	})
	dir := t.TempDir()
	// The first build writes what the modules that are not refused made; the module that is refused is a miss every time.
	for round, hits := range []int{0, 2, 2} {
		got := cachedBuild(t, project, dir, "c1", 2, nil)
		if !strings.Contains(got.errs, "E9900") {
			t.Fatalf("the program that uses a map got %q", got.errs)
		}
		requireSameAsWithoutCache(t, project, got, 2)
		if got.stats.Hits != hits || got.stats.Misses != 3-hits {
			t.Errorf("round %d: %+v, want %d hits", round, got.stats, hits)
		}
	}
}

// A program that has errors writes nothing for a module that has one.
func TestAModuleWithAnErrorIsNeverCached(t *testing.T) {
	project := writeProject(t, map[string]string{"main.maxon": useAfterMoveMain, "broken/broken.maxon": bodyErrorModule})
	dir := t.TempDir()
	for range 2 {
		got := cachedBuild(t, project, dir, "c1", 2, nil)
		if got.errs == "" {
			t.Fatal("the program has errors")
		}
		requireSameAsWithoutCache(t, project, got, 2)
	}
	var entries int
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			entries++
		}
		return nil
	})
	if entries != 0 {
		t.Errorf("a program that does not compile left %d entries", entries)
	}
}

// Two modules with the same text are two entries: a unit names what it defines by its module, so one module's entry is
// no other's, however alike their source and what they reach are.
func TestTwoModulesWithTheSameTextHaveEntriesOfTheirOwn(t *testing.T) {
	const same = "export function f() returns Count\n\treturn 7\nend 'f'\n"
	project := writeProject(t, map[string]string{
		"main.maxon": `function main() returns ExitCode
	print("{try one.f() + two.f() otherwise 0}\n")
	return 0
end 'main'
`,
		"one/f.maxon": same, "two/f.maxon": same,
	})
	dir := t.TempDir()
	cold := cachedBuild(t, project, dir, "c1", 2, nil)
	requireStats(t, "cold", cold.stats, 0, 3, 0)
	warm := cachedBuild(t, project, dir, "c1", 2, nil)
	requireStats(t, "warm", warm.stats, 3, 0, 3)
	if !bytes.Equal(warm.exe, cold.exe) || !bytes.Equal(warm.ir, cold.ir) {
		t.Error("the warm build differs from the cold one")
	}
	requireSameAsWithoutCache(t, project, warm, 2)
}
