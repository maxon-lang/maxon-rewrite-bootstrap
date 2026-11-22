package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

const (
	sourceExt       = ".maxon"
	exitOK          = 0
	exitCheckFailed = 1
	exitUsage       = 2
)

const usageText = `usage:
  convert [--specs] [--write] [--check] [--out=DIR] [<path>...]

Converts existing Maxon (.maxon) source files to minimaxon syntax and layout.
Unsupported constructs are preserved with inline // TODO comments and warnings on stderr.

Flags:
  -w, --write      rewrite .maxon files in place
  -o, --out=DIR    write converted .maxon files under DIR
  --check          do not write; print each kind of warning with its count and the number of files it
                   appears in, most frequent first; exit 1 if any file would change or has warnings
  --specs          the inputs are maxon spec files (.md): each test's maxon block is converted and a test that
                   warned gets a '<!-- skip: kind; kind -->' line after its test marker; files with no test are
                   left out; the warnings are not printed one by one (each is a // TODO in its test) and the summary
                   counts tests, not warnings

With no paths (or '-'), reads from stdin and writes to stdout.
With a single file and neither --write nor --out, writes to stdout.`

func main() {
	os.Exit(runMain(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func runMain(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	var writeInPlace, checkOnly, specs bool
	var outDir string
	var paths []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			fmt.Fprintln(stdout, usageText)
			return exitOK
		case arg == "--specs":
			specs = true
		case arg == "-w" || arg == "--write":
			writeInPlace = true
		case arg == "--check":
			checkOnly = true
		case arg == "-o":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "convert: missing argument for -o")
				return exitUsage
			}
			i++
			outDir = args[i]
		case strings.HasPrefix(arg, "--out="):
			outDir = strings.TrimPrefix(arg, "--out=")
		case strings.HasPrefix(arg, "-") && arg != "-":
			fmt.Fprintf(stderr, "convert: unknown flag %q\n%s\n", arg, usageText)
			return exitUsage
		default:
			paths = append(paths, arg)
		}
	}

	inputExt := sourceExt
	if specs {
		inputExt = specExt
	}
	if writeInPlace && outDir != "" {
		fmt.Fprintln(stderr, "convert: --write and --out cannot be used together")
		return exitUsage
	}
	if checkOnly && (writeInPlace || outDir != "") {
		fmt.Fprintln(stderr, "convert: --check cannot be combined with --write or --out")
		return exitUsage
	}

	// Read from stdin when no paths or '-' is given.
	if len(paths) == 0 || (len(paths) == 1 && paths[0] == "-") {
		data, err := io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "convert: %v\n", err)
			return exitUsage
		}
		res := convertFile(specs, "<stdin>", string(data))
		for _, w := range res.Warnings {
			fmt.Fprintln(stderr, w.String())
		}
		if checkOnly {
			if res.Changed || len(res.Warnings) > 0 {
				return exitCheckFailed
			}
			return exitOK
		}
		fmt.Fprint(stdout, res.Text)
		return exitOK
	}

	type fileJob struct {
		srcPath string
		relPath string
	}
	var jobs []fileJob
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			fmt.Fprintf(stderr, "convert: %v\n", err)
			return exitUsage
		}
		if !info.IsDir() {
			jobs = append(jobs, fileJob{srcPath: p, relPath: filepath.Base(p)})
			continue
		}
		var dirFiles []fileJob
		err = filepath.WalkDir(p, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") && path != p {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(d.Name(), inputExt) {
				rel, err := filepath.Rel(p, path)
				if err != nil {
					return err
				}
				dirFiles = append(dirFiles, fileJob{srcPath: path, relPath: rel})
			}
			return nil
		})
		if err != nil {
			fmt.Fprintf(stderr, "convert: %v\n", err)
			return exitUsage
		}
		sort.Slice(dirFiles, func(i, j int) bool { return dirFiles[i].srcPath < dirFiles[j].srcPath })
		jobs = append(jobs, dirFiles...)
	}

	if len(jobs) == 0 {
		fmt.Fprintf(stderr, "convert: no %s files found\n", inputExt)
		return exitUsage
	}
	if len(jobs) > 1 && !writeInPlace && outDir == "" && !checkOnly {
		fmt.Fprintln(stderr, "convert: multiple files require --write, --out=DIR, or --check")
		return exitUsage
	}

	formatter.collect(func() {
		for _, job := range jobs {
			if data, err := os.ReadFile(job.srcPath); err == nil {
				convertFile(specs, job.srcPath, string(data))
			}
		}
	})

	changed := false
	hasWarnings := false
	changedFiles := 0
	files := 0
	var tally warningTally
	var counts testCounts

	for _, job := range jobs {
		data, err := os.ReadFile(job.srcPath)
		if err != nil {
			fmt.Fprintf(stderr, "convert: %v\n", err)
			return exitUsage
		}
		res := convertFile(specs, job.srcPath, string(data))
		if specs && len(res.Tests) == 0 {
			continue
		}
		files++
		for _, w := range res.Warnings {
			hasWarnings = true
			if !specs {
				fmt.Fprintln(stderr, w.String())
				tally.add(w)
			}
		}
		counts.add(job.srcPath, res.Tests, &tally)
		if res.Changed {
			changed = true
			changedFiles++
		}

		switch {
		case checkOnly:
		case writeInPlace:
			if res.Changed {
				if err := os.WriteFile(job.srcPath, []byte(res.Text), 0o644); err != nil {
					fmt.Fprintf(stderr, "convert: %v\n", err)
					return exitUsage
				}
			}
		case outDir != "":
			dst := filepath.Join(outDir, job.relPath)
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				fmt.Fprintf(stderr, "convert: %v\n", err)
				return exitUsage
			}
			if err := os.WriteFile(dst, []byte(res.Text), 0o644); err != nil {
				fmt.Fprintf(stderr, "convert: %v\n", err)
				return exitUsage
			}
		default:
			fmt.Fprint(stdout, res.Text)
		}
	}

	summary := stdout
	if !checkOnly && !writeInPlace && outDir == "" {
		summary = stderr
	}
	if checkOnly || specs {
		tally.print(summary)
	}
	if specs {
		counts.print(summary)
	}
	if checkOnly {
		fmt.Fprintf(stdout, "%d of %d files would change\n", changedFiles, files)
	}
	if checkOnly && (changed || hasWarnings) {
		return exitCheckFailed
	}
	return exitOK
}

// fileResult is one converted file, whether a source file or a spec file.
type fileResult struct {
	Text     string
	Warnings []Warning
	Changed  bool
	Tests    []SpecTest
}

func convertFile(specs bool, name, text string) fileResult {
	if specs {
		res := ConvertSpec(name, text)
		return fileResult{Text: res.Text, Warnings: res.Warnings, Changed: res.Changed, Tests: res.Tests}
	}
	res := Convert(name, text, sourceFile)
	return fileResult{Text: res.Text, Warnings: res.Warnings, Changed: res.Text != text}
}

// testCounts is what a run over spec files converted: the tests, and how many of them warned.
type testCounts struct {
	total, skipped int
}

// add counts a file's tests and files each blocked test under every kind that blocks it, once per test, in tally.
func (c *testCounts) add(file string, tests []SpecTest, tally *warningTally) {
	c.total += len(tests)
	for _, t := range tests {
		if len(t.Skips) > 0 {
			c.skipped++
		}
		for _, kind := range t.Skips {
			tally.add(Warning{File: file, Message: kind})
		}
	}
}

func (c testCounts) print(w io.Writer) {
	fmt.Fprintf(w, "%d tests: %d converted cleanly, %d skipped\n", c.total, c.total-c.skipped, c.skipped)
}

// warningTally counts each kind of warning, a kind being its message, and the files it appears in, so a run over a
// tree shows which unsupported construct blocks the most files.
type warningTally struct {
	kinds []*warningKind
}

type warningKind struct {
	message string
	count   int
	files   []string
}

func (t *warningTally) add(w Warning) {
	var kind *warningKind
	for _, k := range t.kinds {
		if k.message == w.Message {
			kind = k
			break
		}
	}
	if kind == nil {
		kind = &warningKind{message: w.Message}
		t.kinds = append(t.kinds, kind)
	}
	kind.count++
	if !slices.Contains(kind.files, w.File) {
		kind.files = append(kind.files, w.File)
	}
}

// print writes one line per kind, most frequent first and by message among equals: the count, the number of files, and
// the message.
func (t *warningTally) print(w io.Writer) {
	slices.SortFunc(t.kinds, func(a, b *warningKind) int {
		if a.count != b.count {
			return b.count - a.count
		}
		return strings.Compare(a.message, b.message)
	})
	for _, k := range t.kinds {
		fmt.Fprintf(w, "%6d %4d files  %s\n", k.count, len(k.files), k.message)
	}
}
