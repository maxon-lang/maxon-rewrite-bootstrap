package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// stdlibListing is the file that names what is converted from maxon's standard library: a line is a file relative to
// maxon's stdlib directory, the library module it goes to (a directory under stdlib/, "." for the root) and, where two
// files of a module would share a name, the name it takes.
var stdlibListing = filepath.Join("stdlib", "files.txt")

// maxonStdlibVariable names maxon's stdlib directory for the gate's own check that the library regenerates as committed
// (checkStdlib); without it the check is skipped, as the gate cannot assume where maxon is checked out.
const maxonStdlibVariable = "MAXON_STDLIB"

// stdlibEntry is one line of stdlib/files.txt.
type stdlibEntry struct {
	source string
	module string
	name   string
}

// target is the path of the converted file under stdlib/.
func (e stdlibEntry) target() string {
	return filepath.Join("stdlib", e.module, e.name)
}

func readStdlibListing() ([]stdlibEntry, error) {
	text, err := os.ReadFile(stdlibListing)
	if err != nil {
		return nil, err
	}
	var entries []stdlibEntry
	for line := range strings.SplitSeq(string(text), "\n") {
		words := strings.Fields(line)
		switch len(words) {
		case 0:
			continue
		case 1:
			return nil, fmt.Errorf("%s: '%s' names no module: write the module's directory after the file, '.' for the root", stdlibListing, line)
		}
		e := stdlibEntry{source: filepath.FromSlash(words[0]), module: words[1], name: filepath.Base(words[0])}
		if len(words) > 2 {
			e.name = words[2]
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// regenerateStdlib writes stdlib/ again from maxon's standard library: cmd/convert converts each file that stdlib/files.txt
// names into its module, then the compiler's fix command applies the fixes of what minimaxon's language differences raise
// in it, as for the spec tests (a `let` that is changed becomes a `var`, a name of another library module is qualified).
// The files are never edited here, and a file of stdlib/ that no line names is minimaxon's own, which the run leaves alone.
// The compiler is bin/minimaxon, built first when it is not of the sources as they are (currentCompiler).
func regenerateStdlib(h host, maxonStdlib string) error {
	compiler, err := currentCompiler(h)
	if err != nil {
		return err
	}
	return convertStdlib(compiler, maxonStdlib, run)
}

// convertStdlib is the conversion and the fix; say runs a command and shows what it writes (run), or only when it fails
// (quiet).
func convertStdlib(compiler, maxonStdlib string, say func(env []string, name string, args ...string) error) error {
	entries, err := readStdlibListing()
	if err != nil {
		return err
	}
	scratch, err := scratchDir("minimaxon-stdlib-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	// The conversion lays out what it writes with the same compiler, whatever the environment names.
	env := []string{"MINIMAXON=" + compiler}
	converter := filepath.Join(scratch, "convert"+filepath.Ext(compiler))
	if err := quiet(nil, "go", "build", "-o", converter, "./cmd/convert"); err != nil {
		return err
	}
	converted := filepath.Join(scratch, "converted")
	for _, e := range entries {
		if err := say(env, converter, "--out="+converted, filepath.Join(maxonStdlib, e.source)); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(e.target()), 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(converted, filepath.Base(e.source)))
		if err != nil {
			return err
		}
		if err := os.WriteFile(e.target(), data, 0o644); err != nil {
			return err
		}
	}
	return say(env, compiler, "fix", "stdlib")
}

// checkStdlib fails when regenerating the library from maxon's differs from what is committed: it regenerates in place,
// compares every file under stdlib/ with how it was, and puts every file back as it was, whatever the outcome.
func checkStdlib(h host, maxonStdlib string) (err error) {
	compiler, err := currentCompiler(h)
	if err != nil {
		return err
	}
	before, err := readTree("stdlib")
	if err != nil {
		return err
	}
	defer func() {
		if restoreErr := restoreTree("stdlib", before); restoreErr != nil {
			err = errors.Join(err, restoreErr)
		}
	}()
	if err := convertStdlib(compiler, maxonStdlib, quiet); err != nil {
		return err
	}
	after, err := readTree("stdlib")
	if err != nil {
		return err
	}
	var differs []string
	for path, data := range after {
		if old, ok := before[path]; !ok || !bytes.Equal(old, data) {
			differs = append(differs, path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			differs = append(differs, path)
		}
	}
	if len(differs) > 0 {
		slices.Sort(differs)
		return fmt.Errorf("stdlib/ is not what regenerating it from %s gives, which differs in: %s\nrun go run ./cmd/gate stdlib %s and commit the result", maxonStdlib, strings.Join(differs, ", "), maxonStdlib)
	}
	return nil
}

// checkStdlibIfMaxon is checkStdlib for the directory MAXON_STDLIB names, and says it is skipped where none is named.
func checkStdlibIfMaxon(h host) error {
	dir := os.Getenv(maxonStdlibVariable)
	if dir == "" {
		skip("library regenerates", "maxon is not checked out here: set "+maxonStdlibVariable+" to maxon's stdlib directory to run it, or run gate stdlib --check <maxon stdlib dir>")
		return nil
	}
	return checkStdlib(h, dir)
}

// readTree is every file below dir, by its path.
func readTree(dir string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		files[path] = data
		return err
	})
	return files, err
}

// restoreTree makes the files below dir what files says: files that were rewritten are written back, and files that were
// added are removed with the directories they leave empty.
func restoreTree(dir string, files map[string][]byte) error {
	now, err := readTree(dir)
	if err != nil {
		return err
	}
	for path, data := range files {
		if current, ok := now[path]; !ok || !bytes.Equal(current, data) {
			if err := os.WriteFile(path, data, 0o644); err != nil {
				return err
			}
		}
	}
	for path := range now {
		if _, ok := files[path]; ok {
			continue
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		for parent := filepath.Dir(path); parent != dir; parent = filepath.Dir(parent) {
			if os.Remove(parent) != nil {
				break
			}
		}
	}
	return nil
}
