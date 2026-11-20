package main

import (
	"fmt"
	"io"
	"minimaxon/internal/cache"
	"os"
)

const (
	cacheTrimCommand  = "trim"
	cacheCleanCommand = "clean"
	bytesPerMegabyte  = 1 << 20
	timeLayout        = "2006-01-02 15:04:05"
)

// cacheCommand is `minimaxon cache`: with no argument it says where the build cache is and what it holds, `trim` removes
// the entries that have not been used for five days now instead of when a build finds it is due, and `clean` removes every
// entry. The commands work on the directory MINIMAXON_CACHE names whichever compiler made the entries, so they need no hash
// of the running compiler.
func cacheCommand(args []string, out, errOut io.Writer) int {
	dir, enabled := cache.Dir(os.Getenv(cacheEnvironment))
	if !enabled {
		fmt.Fprintf(errOut, "the build cache is off (%s=off, or there is no user cache directory)\n", cacheEnvironment)
		return 1
	}
	store := cache.Open(dir, cache.Key{})
	megabytes := func(n int64) float64 { return float64(n) / bytesPerMegabyte }
	switch {
	case len(args) == 0:
		usage := store.Usage()
		fmt.Fprintf(out, "directory: %s\nentries: %d, %.1f MB\n", dir, usage.Entries, megabytes(usage.Bytes))
		if usage.Trimmed {
			fmt.Fprintf(out, "last trimmed: %s\n", usage.LastTrim.Local().Format(timeLayout))
		} else {
			fmt.Fprintln(out, "last trimmed: never")
		}
	case args[0] == cacheTrimCommand:
		stats := store.Trim()
		if !stats.Ran {
			fmt.Fprintf(out, "%s does not exist: nothing to trim\n", dir)
			return 0
		}
		fmt.Fprintf(out, "removed %d files (%.1f MB), kept %d (%.1f MB)\n", stats.Removed, megabytes(stats.RemovedBytes), stats.Kept, megabytes(stats.KeptBytes))
	case args[0] == cacheCleanCommand:
		fmt.Fprintf(out, "removed %d entries\n", store.Clean())
	default:
		fmt.Fprintln(errOut, usage)
		return usageExitCode
	}
	return 0
}
