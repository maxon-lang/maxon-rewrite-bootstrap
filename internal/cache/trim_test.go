package cache

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var epoch = time.Date(2026, time.March, 10, 12, 0, 0, 0, time.UTC)

// clockAt is a cache in a temporary directory whose time is whatever the returned pointer holds, so a test says how old
// everything is without waiting.
func clockAt(t *testing.T, now time.Time) (*Cache, *time.Time) {
	t.Helper()
	at := &now
	return Open(t.TempDir(), testKey("compiler")).WithClock(func() time.Time { return *at }), at
}

// storedAge stores an entry and makes it last used the given time before the cache's now.
func storedAge(t *testing.T, c *Cache, name string, age time.Duration) Key {
	t.Helper()
	key := testKey(name)
	if err := c.Put(key, []byte("the code of "+name)); err != nil {
		t.Fatal(err)
	}
	setAge(t, entryFile(t, c, key), c.now(), age)
	return key
}

func setAge(t *testing.T, file string, now time.Time, age time.Duration) {
	t.Helper()
	when := now.Add(-age)
	if err := os.Chtimes(file, when, when); err != nil {
		t.Fatal(err)
	}
}

func modified(t *testing.T, file string) time.Time {
	t.Helper()
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime()
}

func has(c *Cache, key Key) bool {
	_, ok := c.Get(key)
	return ok
}

const day = 24 * time.Hour

func TestTrimRemovesWhatWasNotUsedForFiveDays(t *testing.T) {
	c, _ := clockAt(t, epoch)
	old := storedAge(t, c, "six days", 6*day)
	older := storedAge(t, c, "a month", 30*day)
	recent := storedAge(t, c, "four days", 4*day)
	fresh := storedAge(t, c, "an hour", time.Hour)
	stats := c.Trim()
	if !stats.Ran || stats.Removed != 2 || stats.Kept != 2 || stats.RemovedBytes == 0 || stats.KeptBytes == 0 {
		t.Errorf("trim reports %+v", stats)
	}
	for name, key := range map[string]Key{"six days": old, "a month": older} {
		if has(c, key) {
			t.Errorf("the entry that was last used %s ago is still there", name)
		}
	}
	for name, key := range map[string]Key{"four days": recent, "an hour": fresh} {
		if !has(c, key) {
			t.Errorf("the entry that was last used %s ago was removed", name)
		}
	}
}

// An entry that is used is not trimmed: using it marks it, however long ago it was last used.
func TestAnEntryThatIsUsedIsNotTrimmed(t *testing.T) {
	c, now := clockAt(t, epoch)
	used := storedAge(t, c, "used", 6*day)
	unused := storedAge(t, c, "unused", 6*day)
	if !has(c, used) {
		t.Fatal("the entry that is six days old is not there before trimming")
	}
	if got := modified(t, entryFile(t, c, used)); !got.Equal(*now) {
		t.Errorf("using the entry left its time at %v, want %v", got, *now)
	}
	c.Trim()
	if !has(c, used) || has(c, unused) {
		t.Errorf("after the trim the used entry is there: %v, the unused one: %v", has(c, used), has(c, unused))
	}
}

// Marking an entry costs a write, so an entry that was used within the hour is left as it is.
func TestUsingAnEntryMarksItAtMostOnceAnHour(t *testing.T) {
	c, now := clockAt(t, epoch)
	recent := storedAge(t, c, "recent", 30*time.Minute)
	stale := storedAge(t, c, "stale", 2*time.Hour)
	has(c, recent)
	has(c, stale)
	if got := modified(t, entryFile(t, c, recent)); !got.Equal(now.Add(-30 * time.Minute)) {
		t.Errorf("an entry used half an hour ago was marked: %v", got)
	}
	if got := modified(t, entryFile(t, c, stale)); !got.Equal(*now) {
		t.Errorf("an entry used two hours ago was not marked: %v", got)
	}
	*now = now.Add(61 * time.Minute)
	has(c, recent)
	if got := modified(t, entryFile(t, c, recent)); !got.Equal(*now) {
		t.Errorf("an entry used an hour and a half ago was not marked: %v", got)
	}
}

// A damaged or missing entry is not marked as used, and is not an error.
func TestAnEntryThatIsNotThereIsAMissAndMarksNothing(t *testing.T) {
	c, _ := clockAt(t, epoch)
	key := storedAge(t, c, "gone", 3*time.Hour)
	file := entryFile(t, c, key)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if has(c, key) {
		t.Error("an entry that was removed was found")
	}
	if err := os.WriteFile(file, []byte("not an entry"), 0o644); err != nil {
		t.Fatal(err)
	}
	setAge(t, file, epoch, 3*time.Hour)
	if has(c, key) {
		t.Error("a damaged entry was found")
	}
	if got := modified(t, file); !got.Equal(epoch.Add(-3 * time.Hour)) {
		t.Errorf("a damaged entry was marked as used: %v", got)
	}
}

func TestTrimIfDueTrimsOnceADay(t *testing.T) {
	c, now := clockAt(t, epoch)
	storedAge(t, c, "old", 6*day)
	if stats := c.TrimIfDue(); !stats.Ran || stats.Removed != 1 {
		t.Fatalf("the first trim: %+v", stats)
	}
	if got := c.Usage(); !got.Trimmed || !got.LastTrim.Equal(epoch) || got.Entries != 0 {
		t.Errorf("usage after the first trim: %+v", got)
	}
	// An entry that is old enough is not removed again within the day...
	older := storedAge(t, c, "older", 6*day)
	*now = epoch.Add(23 * time.Hour)
	if stats := c.TrimIfDue(); stats.Ran || stats.Removed != 0 {
		t.Errorf("a trim within the day ran: %+v", stats)
	}
	if got := c.Usage().Entries; got != 1 {
		t.Errorf("a trim within the day left %d entries, want 1", got)
	}
	// ...and is when the day is over.
	*now = epoch.Add(25 * time.Hour)
	setAge(t, entryFile(t, c, older), *now, 6*day)
	if stats := c.TrimIfDue(); !stats.Ran || stats.Removed != 1 {
		t.Errorf("a trim after the day: %+v", stats)
	}
	if got := c.Usage().LastTrim; !got.Equal(*now) {
		t.Errorf("the time of the last trim is %v, want %v", got, *now)
	}
}

// A record that is not a time, or is one that has not come yet, does not stop the cache being trimmed.
func TestARecordOfTheLastTrimThatCannotBeTrustedIsIgnored(t *testing.T) {
	c, _ := clockAt(t, epoch)
	stamp := filepath.Join(c.Dir(), stampName)
	for name, text := range map[string]string{
		"garbage":       "yesterday",
		"empty":         "",
		"in the future": "9999999999",
	} {
		storedAge(t, c, name, 6*day)
		if err := os.WriteFile(stamp, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if stats := c.TrimIfDue(); !stats.Ran || stats.Removed != 1 {
			t.Errorf("%s: %+v", name, stats)
		}
	}
}

func TestTrimDoesNotMakeTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "never made")
	c := Open(dir, testKey("compiler"))
	if stats := c.TrimIfDue(); stats.Ran {
		t.Errorf("a cache that is not there was trimmed: %+v", stats)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("trimming made the directory")
	}
}

func TestTrimRemovesStrayTemporaryFilesOnlyWhenTheyAreOld(t *testing.T) {
	c, _ := clockAt(t, epoch)
	key := storedAge(t, c, "an entry", time.Hour)
	dir := filepath.Dir(entryFile(t, c, key))
	stray := filepath.Join(dir, "tmp-1")
	writing := filepath.Join(dir, "tmp-2")
	for _, f := range []string{stray, writing} {
		if err := os.WriteFile(f, []byte("half an entry"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	setAge(t, stray, epoch, 2*day)
	setAge(t, writing, epoch, time.Minute)
	c.Trim()
	if _, err := os.Stat(stray); err == nil {
		t.Error("a temporary file from two days ago is still there")
	}
	if _, err := os.Stat(writing); err != nil {
		t.Error("a temporary file that was written a minute ago was removed")
	}
}

// What the cache did not make is not the cache's to remove, even in the directory it keeps entries in.
func TestTrimAndCleanLeaveWhatIsNotTheCachesAlone(t *testing.T) {
	c, _ := clockAt(t, epoch)
	key := storedAge(t, c, "old", 30*day)
	bucket := filepath.Dir(entryFile(t, c, key))
	others := []string{
		filepath.Join(c.Dir(), "notes.txt"),
		filepath.Join(bucket, "notes.txt"),
		filepath.Join(bucket, "0123456789"),
		filepath.Join(c.Dir(), "ab", "NOTANENTRY"),
	}
	for _, f := range others {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("mine"), 0o644); err != nil {
			t.Fatal(err)
		}
		setAge(t, f, epoch, 30*day)
	}
	keepDir := filepath.Join(c.Dir(), "projects")
	if err := os.MkdirAll(filepath.Join(keepDir, "ab"), 0o755); err != nil {
		t.Fatal(err)
	}
	c.Trim()
	for _, f := range others {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("trimming removed %s", f)
		}
	}
	if got := c.Clean(); got != 0 {
		t.Errorf("clean removed %d entries after the trim took them all", got)
	}
	for _, f := range others {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("cleaning removed %s", f)
		}
	}
	if _, err := os.Stat(keepDir); err != nil {
		t.Error("cleaning removed a directory that is not the cache's")
	}
}

func TestCleanRemovesEveryEntryAndTheRecord(t *testing.T) {
	c, _ := clockAt(t, epoch)
	keys := []Key{storedAge(t, c, "a", time.Hour), storedAge(t, c, "b", 4*day), storedAge(t, c, "c", 40*day)}
	c.Trim()
	if got := c.Clean(); got != 2 {
		t.Errorf("clean removed %d entries, want the 2 that were left", got)
	}
	for _, k := range keys {
		if has(c, k) {
			t.Error("an entry survived cleaning")
		}
	}
	if got := c.Usage(); got.Entries != 0 || got.Trimmed {
		t.Errorf("usage after cleaning: %+v", got)
	}
	entries, err := os.ReadDir(c.Dir())
	if err != nil || len(entries) != 0 {
		t.Errorf("the directory holds %v after cleaning (%v)", entries, err)
	}
	// A cache that was cleaned is used as one that never was.
	if err := c.Put(keys[0], []byte("again")); err != nil || !has(c, keys[0]) {
		t.Errorf("a cleaned cache cannot be written: %v", err)
	}
}

func TestUsageCountsEntriesAndTheirSize(t *testing.T) {
	c, _ := clockAt(t, epoch)
	if got := c.Usage(); got.Entries != 0 || got.Bytes != 0 || got.Trimmed {
		t.Errorf("an empty cache: %+v", got)
	}
	storedAge(t, c, "a", time.Hour)
	storedAge(t, c, "b", time.Hour)
	got := c.Usage()
	if got.Entries != 2 || got.Bytes <= 0 {
		t.Errorf("two entries: %+v", got)
	}
	if want := c.BytesWritten(); got.Bytes != want {
		t.Errorf("the entries take %d bytes, %d were written", got.Bytes, want)
	}
}

// Compilers that use the cache while another trims it see an entry or a miss, never a damaged entry or an error, and trims
// that overlap are harmless.
func TestTrimBesideReadersAndWritersIsSafe(t *testing.T) {
	c, _ := clockAt(t, epoch)
	const keys = 24
	var all []Key
	for i := range keys {
		name := string(rune('a' + i))
		age := time.Duration(i) * day / 2 // half of them are old enough to go
		all = append(all, storedAge(t, c, name, age))
	}
	payload := func(i int) []byte { return []byte("the code of " + string(rune('a'+i))) }
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 20 {
				c.Trim()
			}
		})
		wg.Go(func() {
			for range 20 {
				for i, key := range all {
					if got, ok := c.Get(key); ok && !bytes.Equal(got, payload(i)) {
						t.Errorf("a reader saw %q for entry %d", got, i)
					}
					_ = c.Put(key, payload(i))
				}
			}
		})
	}
	wg.Wait()
	for i, key := range all {
		if got, ok := c.Get(key); ok && !bytes.Equal(got, payload(i)) {
			t.Errorf("entry %d reads %q after the race", i, got)
		}
	}
}
