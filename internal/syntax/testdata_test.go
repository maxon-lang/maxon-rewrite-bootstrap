package syntax

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minimaxon/internal/diag"
)

const (
	sourceExtension = ".maxon"
	internalRoot    = ".."
)

// minimaxon rewrite renames a keyword by lexing each .maxon file, so the source that unit tests keep in testdata
// directories must lex on its own; one that does not would keep the old keyword when the language changes.
func TestEveryFileOfTestSourceLexes(t *testing.T) {
	files := 0
	err := filepath.WalkDir(internalRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, sourceExtension) {
			return err
		}
		files++
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		var errs []*diag.CompileError
		diag.Guard(&errs, func() {
			Lex(NewSource(text, []FileSlice{{File: path, Begin: 0, End: len(text)}}), nil)
		})
		if len(errs) > 0 {
			t.Errorf("%s does not lex: %v", path, errs[0])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatalf("no %s file under %s", sourceExtension, internalRoot)
	}
}
