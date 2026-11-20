package cache

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Trimming keeps the cache from growing for ever, as Go's build cache does: an entry that has not been used for
// maxEntryAge is removed, and the cache is trimmed at most once in trimInterval, which a file in the directory records.
// An entry's use is its modification time, which Get brings up to date at most once in touchInterval, so a build that
// reads an entry writes to the disk only the first time in an hour and a trim sees the use of the last hour at worst
// as the use of an hour ago.
//
// Trimming is safe beside other compilers because the only things it does are removing files and writing the
// record of the time. A reader that finds its entry gone has a miss, a writer renames a whole file over a name that may or
// may not still be there, and two trims that run at once remove the same files.
const (
	touchInterval = time.Hour
	trimInterval  = 24 * time.Hour
	maxEntryAge   = 5 * 24 * time.Hour
	// strayAge is how old a temporary file has to be to be one that a compiler that stopped left behind and not one that is
	// being written.
	strayAge = trimInterval
	// stampName is the file that records when the cache was last trimmed, in decimal seconds since 1970.
	stampName = "trim.txt"

	bucketNameLength = 2
	entryNameLength  = 2*sizeBytes - bucketNameLength
	sizeBytes        = len(Key{})
	tempPrefix       = "tmp-"
)

// fileKind says what a file in the cache directory is, by its name and its directory's: the cache removes only what it
// made, so a directory that holds other things as well is not harmed by trimming it or cleaning it.
type fileKind int

const (
	kindOther fileKind = iota
	kindEntry
	kindTemp
)

func isHex(s string, length int) bool {
	if len(s) != length {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' && c < 'a' || c > 'f' {
			return false
		}
	}
	return true
}

func classify(name string) fileKind {
	switch {
	case isHex(name, entryNameLength):
		return kindEntry
	case strings.HasPrefix(name, tempPrefix):
		return kindTemp
	}
	return kindOther
}

// eachFile calls visit for every file of the cache that is an entry or a temporary file, with its path.
func (c *Cache) eachFile(visit func(path string, info fs.FileInfo, kind fileKind)) {
	buckets, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	for _, bucket := range buckets {
		if !bucket.IsDir() || !isHex(bucket.Name(), bucketNameLength) {
			continue
		}
		dir := filepath.Join(c.dir, bucket.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			kind := classify(f.Name())
			if f.IsDir() || kind == kindOther {
				continue
			}
			if info, err := f.Info(); err == nil {
				visit(filepath.Join(dir, f.Name()), info, kind)
			}
		}
	}
}

// TrimStats is what a trim found.
type TrimStats struct {
	// Ran is whether the cache was trimmed: TrimIfDue does nothing when it was trimmed less than a day ago, or when there is
	// no cache directory to trim.
	Ran bool
	// Removed and Kept count the entries and the stray temporary files, and the bytes they take.
	Removed, Kept           int
	RemovedBytes, KeptBytes int64
}

// TrimIfDue trims the cache unless it was trimmed within the last day.
func (c *Cache) TrimIfDue() TrimStats {
	// A record of a time that is still to come is not trusted, or a clock that was wrong once would stop trimming for good.
	now := c.now()
	if last, ok := c.lastTrim(); ok && !last.After(now) && now.Sub(last) < trimInterval {
		return TrimStats{}
	}
	return c.Trim()
}

// Trim removes every entry that has not been used for five days, and every temporary file that a compiler that stopped left
// behind, and records the time. A cache directory that does not exist is not made.
func (c *Cache) Trim() TrimStats {
	if _, err := os.Stat(c.dir); err != nil {
		return TrimStats{}
	}
	now := c.now()
	stats := TrimStats{Ran: true}
	c.eachFile(func(path string, info fs.FileInfo, kind fileKind) {
		limit := maxEntryAge
		if kind == kindTemp {
			limit = strayAge
		}
		if now.Sub(info.ModTime()) > limit && os.Remove(path) == nil {
			stats.Removed++
			stats.RemovedBytes += info.Size()
			return
		}
		stats.Kept++
		stats.KeptBytes += info.Size()
	})
	_ = os.WriteFile(filepath.Join(c.dir, stampName), strconv.AppendInt(nil, now.Unix(), 10), 0o644)
	return stats
}

// lastTrim is when the cache was last trimmed, if the record of it is there and is a time.
func (c *Cache) lastTrim() (time.Time, bool) {
	text, err := os.ReadFile(filepath.Join(c.dir, stampName))
	if err != nil {
		return time.Time{}, false
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(string(text)), 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(seconds, 0), true
}

// Clean removes everything the cache made in its directory, and nothing else that is there, and says how many entries
// it removed.
func (c *Cache) Clean() (removed int) {
	c.eachFile(func(path string, info fs.FileInfo, kind fileKind) {
		if os.Remove(path) == nil && kind == kindEntry {
			removed++
		}
	})
	buckets, _ := os.ReadDir(c.dir)
	for _, bucket := range buckets {
		if bucket.IsDir() && isHex(bucket.Name(), bucketNameLength) {
			// Only an empty directory is removed: a file that is not the cache's, or an entry that a compiler wrote meanwhile,
			// keeps its place.
			_ = os.Remove(filepath.Join(c.dir, bucket.Name()))
		}
	}
	_ = os.Remove(filepath.Join(c.dir, stampName))
	return removed
}

// Usage is what the cache directory holds.
type Usage struct {
	Entries int
	Bytes   int64
	// LastTrim is when the cache was last trimmed, and Trimmed whether it ever was.
	LastTrim time.Time
	Trimmed  bool
}

// Usage counts the entries and their size.
func (c *Cache) Usage() Usage {
	var u Usage
	c.eachFile(func(path string, info fs.FileInfo, kind fileKind) {
		if kind == kindEntry {
			u.Entries++
			u.Bytes += info.Size()
		}
	})
	u.LastTrim, u.Trimmed = c.lastTrim()
	return u
}
