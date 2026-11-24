package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// The compiler's fmt command is minimaxon's one formatter, so converted source is put in its layout by running the
// compiler. A run costs far more than formatting one text, so a conversion of many files first collects every text it
// will format, formats them all in a few runs, and then converts again with the results at hand.

// compilerVar names the compiler whose fmt lays out converted source; without it, the compiler is the gate's, bin/minimaxon
// in the repository, the directory that holds go.mod, found upward from the working directory.
const compilerVar = "MINIMAXON"

const (
	compilerDir = "bin"
	moduleFile  = "go.mod"
	layoutExt   = ".maxon"
	// layoutBatch is how many files one run of fmt is given, which keeps its command line within what Windows allows.
	layoutBatch = 1000
	// fmtRefused is fmt's exit code when a file it was named does not parse; every other file is still formatted.
	fmtRefused = 1
)

// refusal starts what fmt writes of an error in a file it was named and could not parse: the file's number. The message
// follows, up to the newline before the next error or the end, and may hold newlines of its own.
var refusal = regexp.MustCompile(`(?m)^error E[0-9]+: ([0-9]+)\.maxon:[0-9]+:[0-9]+: `)

// laidOut is what fmt made of a text: the text in the layout, or the message of the first error when it does not parse.
type laidOut struct {
	text      string
	formatted bool
	rejected  string
}

// layout formats texts with the compiler and keeps what each became.
type layout struct {
	results map[string]laidOut
	// collecting is set while a conversion only gathers the texts it will format; its output is thrown away.
	collecting bool
	pending    []string
}

// formatter is the layout of this run of convert.
var formatter = &layout{results: map[string]laidOut{}}

// format is text in the layout. While collecting it notes the text and gives the text as it is.
func (l *layout) format(text string) laidOut {
	if r, ok := l.results[text]; ok {
		return r
	}
	if l.collecting {
		l.results[text] = laidOut{text: text}
		l.pending = append(l.pending, text)
		return l.results[text]
	}
	l.run([]string{text})
	return l.results[text]
}

// collect runs convert with only the texts it formats gathered, then formats them all, so that a second conversion
// finds each one done.
func (l *layout) collect(convert func()) {
	l.collecting = true
	convert()
	l.collecting = false
	pending := l.pending
	l.pending = nil
	// fmt runs on one core, so the batches run at once, one for each processor, and no more than layoutBatch texts each.
	size := min(layoutBatch, max(1, (len(pending)+runtime.NumCPU()-1)/runtime.NumCPU()))
	var batches [][]string
	for start := 0; start < len(pending); start += size {
		batches = append(batches, pending[start:min(start+size, len(pending))])
	}
	compilerPath()
	done := make([][]laidOut, len(batches))
	var wg sync.WaitGroup
	slots := make(chan struct{}, runtime.NumCPU())
	for i, batch := range batches {
		wg.Go(func() {
			slots <- struct{}{}
			done[i] = formatBatch(batch)
			<-slots
		})
	}
	wg.Wait()
	for i, batch := range batches {
		for j, text := range batch {
			l.results[text] = done[i][j]
		}
	}
}

// run formats texts in one run of the compiler and keeps what each became.
func (l *layout) run(texts []string) {
	for i, out := range formatBatch(texts) {
		l.results[texts[i]] = out
	}
}

// formatBatch formats texts in one run of the compiler, each as a file of its own in a scratch directory, and gives what
// each became, in their order.
func formatBatch(texts []string) []laidOut {
	dir, err := os.MkdirTemp("", "convert-layout-")
	if err != nil {
		fail(err)
	}
	defer os.RemoveAll(dir)
	names := make([]string, len(texts))
	for i, text := range texts {
		names[i] = strconv.Itoa(i) + layoutExt
		if err := os.WriteFile(filepath.Join(dir, names[i]), []byte(text), 0o644); err != nil {
			fail(err)
		}
	}
	cmd := exec.Command(compilerPath(), append([]string{"fmt"}, names...)...)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err = cmd.Run()
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == fmtRefused) {
		fail(fmt.Errorf("%s fmt: %v\n%s", compilerPath(), err, stderr.String()))
	}
	rejected := map[int]string{}
	report := stderr.String()
	starts := refusal.FindAllStringSubmatchIndex(report, -1)
	for i, m := range starts {
		end := len(report)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		n, _ := strconv.Atoi(report[m[2]:m[3]])
		if _, ok := rejected[n]; !ok {
			rejected[n] = strings.TrimSuffix(report[m[1]:end], "\n")
		}
	}
	outs := make([]laidOut, len(texts))
	for i, text := range texts {
		if message, ok := rejected[i]; ok {
			outs[i] = laidOut{text: text, rejected: message}
			continue
		}
		out, err := os.ReadFile(filepath.Join(dir, names[i]))
		if err != nil {
			fail(err)
		}
		outs[i] = laidOut{text: string(out), formatted: true}
	}
	return outs
}

var compiler string

// executableSuffix ends the name of an executable of the system convert runs on.
func executableSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// compilerPath is the compiler that formats, from compilerVar or else the gate's in the repository.
func compilerPath() string {
	if compiler != "" {
		return compiler
	}
	if named := os.Getenv(compilerVar); named != "" {
		compiler = named
		return compiler
	}
	dir, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, moduleFile)); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			fail(fmt.Errorf("no %s above the working directory, so no compiler to format with: set %s to a compiler", moduleFile, compilerVar))
		}
		dir = parent
	}
	compiler = filepath.Join(dir, compilerDir, "minimaxon"+executableSuffix())
	if _, err := os.Stat(compiler); err != nil {
		fail(fmt.Errorf("%s formats converted source, and there is none: build it with go run ./cmd/gate compiler, or set %s to a compiler", compiler, compilerVar))
	}
	return compiler
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "convert:", err)
	os.Exit(exitUsage)
}
