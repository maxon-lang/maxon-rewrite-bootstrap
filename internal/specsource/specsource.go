// Package specsource pins the maxon spec tests that specs/ is converted from. The pin is one line in specs/SOURCE: the git
// tree hash of maxon's specs directory, which is its content, and the maxon commit it was taken at, for people to read.
// A conversion from a directory of other content is refused unless it is told that the source is advanced on purpose, so
// two machines never convert from two different checkouts of maxon without anyone saying so. The content is compared, not
// the commit: a later commit of maxon whose specs are the same passes.
package specsource

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// FileName is the name of the pin's file in the converted specs directory.
const FileName = "SOURCE"

// AdvanceFlag is the flag of cmd/convert and of `gate specs` that lets a conversion move the pin to another source.
const AdvanceFlag = "--advance-source"

// unknownCommit stands for the commit of a source that is not a clean git checkout, such as an archive.
const unknownCommit = "unknown"

// Pin is the line of specs/SOURCE: the tree hash of maxon's specs directory, and its commit when it is known.
type Pin struct {
	Tree   string
	Commit string
}

// String is the pin as its file holds it, without the line's end.
func (p Pin) String() string {
	return "tree " + p.Tree + " commit " + p.Commit
}

// Parse reads the line of a pin's file.
func Parse(text string) (Pin, error) {
	f := strings.Fields(text)
	if len(f) != 4 || f[0] != "tree" || f[2] != "commit" || len(f[1]) != 40 {
		return Pin{}, fmt.Errorf("%s: want one line `tree <hash> commit <commit>`, got %q", FileName, strings.TrimSpace(text))
	}
	return Pin{Tree: f[1], Commit: f[3]}, nil
}

// Of is the pin of a directory of maxon's spec files: the git tree hash of what it holds on disk, the same hash as
// `git rev-parse <commit>:specs` for a checkout of that commit, and the commit of the checkout when the directory is in
// one and holds exactly what its HEAD holds there. An archive gets the same hash, computed over its files, and no commit.
func Of(dir string) (Pin, error) {
	tree, err := TreeHash(dir)
	if err != nil {
		return Pin{}, err
	}
	return Pin{Tree: tree, Commit: commitOf(dir, tree)}, nil
}

// commitOf is the HEAD of the git checkout that holds dir when HEAD's tree at dir is the given one, and unknownCommit
// otherwise (no git, no checkout, or a working directory that differs from HEAD).
func commitOf(dir, tree string) string {
	git := func(args ...string) (string, bool) {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
		return strings.TrimSpace(string(out)), err == nil
	}
	prefix, ok := git("rev-parse", "--show-prefix")
	if !ok {
		return unknownCommit
	}
	head, ok := git("rev-parse", "HEAD")
	if !ok {
		return unknownCommit
	}
	if headTree, ok := git("rev-parse", "HEAD:"+strings.TrimSuffix(prefix, "/")); !ok || headTree != tree {
		return unknownCommit
	}
	return head
}

// TreeHash is the git tree hash of the files under dir as git would store them: a blob for each file, a tree for each
// directory that holds one, entries in git's order, a file executable by its owner as mode 100755 (never on Windows,
// which keeps no such bit) and a symbolic link as 120000. A .git entry is left out, as git leaves it out.
func TreeHash(dir string) (string, error) {
	sum, err := treeSum(dir)
	if err != nil {
		return "", err
	}
	if sum == nil {
		return "", fmt.Errorf("%s holds no file", dir)
	}
	return hex.EncodeToString(sum), nil
}

// treeSum is the hash of the tree of dir, or nil when it holds no file, which git keeps no tree for.
func treeSum(dir string) ([]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	type entry struct {
		mode, name, key string
		sum             []byte
	}
	var kept []entry
	for _, e := range entries {
		if e.Name() == ".git" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		switch {
		case info.IsDir():
			sum, err := treeSum(path)
			if err != nil {
				return nil, err
			}
			if sum != nil {
				kept = append(kept, entry{mode: "40000", name: e.Name(), key: e.Name() + "/", sum: sum})
			}
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return nil, err
			}
			kept = append(kept, entry{mode: "120000", name: e.Name(), key: e.Name(), sum: object("blob", []byte(filepath.ToSlash(target)))})
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			mode := "100644"
			if runtime.GOOS != "windows" && info.Mode()&0o100 != 0 {
				mode = "100755"
			}
			kept = append(kept, entry{mode: mode, name: e.Name(), key: e.Name(), sum: object("blob", data)})
		default:
			return nil, fmt.Errorf("%s: neither a file, a directory nor a link", path)
		}
	}
	if len(kept) == 0 {
		return nil, nil
	}
	// Git orders a tree's entries by name, a directory's name compared as if it ended in a slash.
	sort.Slice(kept, func(i, j int) bool { return kept[i].key < kept[j].key })
	var body []byte
	for _, e := range kept {
		body = append(body, e.mode+" "+e.name+"\x00"...)
		body = append(body, e.sum...)
	}
	return object("tree", body), nil
}

// object is the hash of a git object of a kind and content.
func object(kind string, data []byte) []byte {
	h := sha1.New()
	fmt.Fprintf(h, "%s %d\x00", kind, len(data))
	h.Write(data)
	return h.Sum(nil)
}

// Check compares the pin of the source directory with the one recorded in outDir, and gives the pin to record. A source of
// the recorded content passes and keeps the recorded line. Another one is refused, naming both, unless advance says that
// the source is moved on purpose. With no pin recorded yet, the source's pin is the first.
func Check(source, outDir string, advance bool) (Pin, error) {
	pin, err := Of(source)
	if err != nil {
		return Pin{}, err
	}
	data, err := os.ReadFile(filepath.Join(outDir, FileName))
	if errors.Is(err, fs.ErrNotExist) {
		return pin, nil
	}
	if err != nil {
		return Pin{}, err
	}
	recorded, err := Parse(string(data))
	if err != nil {
		return Pin{}, err
	}
	if recorded.Tree == pin.Tree {
		return recorded, nil
	}
	if advance {
		return pin, nil
	}
	return Pin{}, fmt.Errorf("the specs are pinned to maxon's specs tree %s (commit %s) in %s, but %s holds tree %s (commit %s): convert from a checkout of the pinned specs, or pass %s to move the pin to this source on purpose",
		recorded.Tree, recorded.Commit, filepath.Join(outDir, FileName), source, pin.Tree, pin.Commit, AdvanceFlag)
}

// Write records a pin in outDir.
func Write(outDir string, pin Pin) error {
	return os.WriteFile(filepath.Join(outDir, FileName), []byte(pin.String()+"\n"), 0o644)
}
