// Package hosts names the systems the repository is built on and the committed compiler that builds self/ on each, which
// the gate and the tools that build compilers share.
package hosts

import (
	"fmt"
	"path/filepath"
	"runtime"
)

// MacOSSeedName is the macOS seed's file name. A Mach-O executable is signed under the name it is built as, so every
// compiler that is compared with it is built under this name too, each in a directory of its own (gate seed).
const MacOSSeedName = "minimaxon-arm64-macos"

// seeds are the committed compilers, one per host target the repository is built on, all built from self/ by the Windows
// one's renewal (gate seed).
var seeds = map[string]string{
	"x64-windows": filepath.Join("seed", "minimaxon.exe"),
	"x64-linux":   filepath.Join("seed", "minimaxon-x64-linux"),
	"arm64-macos": filepath.Join("seed", MacOSSeedName),
}

// SeedPath is the seed of a host target, relative to the repository's root, or "" where there is none (arm64 Linux).
func SeedPath(target string) string {
	return seeds[target]
}

// Target is the compiler's name of the system this program runs on, as --target spells it.
func Target() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "windows/amd64":
		return "x64-windows", nil
	case "linux/amd64":
		return "x64-linux", nil
	case "darwin/arm64":
		return "arm64-macos", nil
	case "linux/arm64":
		return "arm64-linux", nil
	default:
		return "", fmt.Errorf("no host is %s/%s; the hosts are x64 Windows, x64 Linux, arm64 macOS and (without a seed) arm64 Linux", runtime.GOOS, runtime.GOARCH)
	}
}
