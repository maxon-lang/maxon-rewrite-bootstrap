package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minimaxon/internal/specsource"
)

// A conversion from specs of other content than specs/SOURCE pins is refused before the converted files are deleted.
func TestConvertSpecsRefusesAnotherSourceBeforeDeleting(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	source := filepath.Join(dir, "maxon-specs")
	for path, text := range map[string]string{"specs/kept.md": "## Tests\n", filepath.Join(source, "new.md"): "## Tests\n"} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pinned := specsource.Pin{Tree: strings.Repeat("0", 40), Commit: "ba3b9618af1ee04753ad46b726d900e7038f8c77"}
	if err := specsource.Write("specs", pinned); err != nil {
		t.Fatal(err)
	}
	err := convertSpecs("no-compiler", source, false)
	if err == nil || !strings.Contains(err.Error(), pinned.Tree) || !strings.Contains(err.Error(), specsource.AdvanceFlag) {
		t.Fatalf("want a refusal naming the pinned tree and the flag, got %v", err)
	}
	if _, err := os.Stat(filepath.Join("specs", "kept.md")); err != nil {
		t.Fatalf("a refused conversion deletes nothing: %v", err)
	}
}
