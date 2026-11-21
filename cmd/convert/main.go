package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
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
  convert [--write] [--check] [--out=DIR] [<path>...]

Converts existing Maxon (.maxon) source files to minimaxon syntax and layout.
Unsupported constructs are preserved with inline // TODO comments and warnings on stderr.

Flags:
  -w, --write      rewrite .maxon files in place
  -o, --out=DIR    write converted .maxon files under DIR
  --check          do not write; exit 1 if any file would change or has warnings

With no paths (or '-'), reads from stdin and writes to stdout.
With a single file and neither --write nor --out, writes to stdout.`

func main() {
	os.Exit(runMain(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func runMain(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	var writeInPlace, checkOnly bool
	var outDir string
	var paths []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			fmt.Fprintln(stdout, usageText)
			return exitOK
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
		res := Convert("<stdin>", string(data))
		for _, w := range res.Warnings {
			fmt.Fprintln(stderr, w.String())
		}
		if checkOnly {
			if res.Text != string(data) || len(res.Warnings) > 0 {
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
			if strings.HasSuffix(d.Name(), sourceExt) {
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
		fmt.Fprintln(stderr, "convert: no .maxon files found")
		return exitUsage
	}
	if len(jobs) > 1 && !writeInPlace && outDir == "" && !checkOnly {
		fmt.Fprintln(stderr, "convert: multiple files require --write, --out=DIR, or --check")
		return exitUsage
	}

	changed := false
	hasWarnings := false

	for _, job := range jobs {
		data, err := os.ReadFile(job.srcPath)
		if err != nil {
			fmt.Fprintf(stderr, "convert: %v\n", err)
			return exitUsage
		}
		orig := string(data)
		res := Convert(job.srcPath, orig)
		for _, w := range res.Warnings {
			fmt.Fprintln(stderr, w.String())
			hasWarnings = true
		}
		if res.Text != orig {
			changed = true
		}

		switch {
		case checkOnly:
			if res.Text != orig {
				fmt.Fprintln(stdout, job.srcPath)
			}
		case writeInPlace:
			if res.Text != orig {
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

	if checkOnly && (changed || hasWarnings) {
		return exitCheckFailed
	}
	return exitOK
}
