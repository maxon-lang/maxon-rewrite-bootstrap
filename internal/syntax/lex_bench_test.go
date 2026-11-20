package syntax

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// BenchmarkLex lexes every module of the project in $MINIMAXON_BENCH, a directory of module directories such as
// the generated stress project; it is skipped when the variable is not set.
func BenchmarkLex(b *testing.B) {
	root := os.Getenv("MINIMAXON_BENCH")
	if root == "" {
		b.Skip("set MINIMAXON_BENCH to a project directory")
	}
	var sources []*Source
	total := 0
	dirs, _ := os.ReadDir(root)
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(root, d.Name(), "*.maxon"))
		sort.Strings(files)
		if len(files) == 0 {
			continue
		}
		src := NewScratch().LoadModuleSource(files)
		sources = append(sources, src)
		total += len(src.Text)
	}
	b.SetBytes(int64(total))
	var buffer []Token
	for b.Loop() {
		for _, src := range sources {
			buffer = Lex(src, buffer)
		}
	}
}
