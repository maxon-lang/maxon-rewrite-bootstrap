package srctest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCasesSplitAtTheirMarkers(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	prelude := write("prelude", "shared\n")
	cases := Cases(t, write("cases", "// case: one\n// expect-error: E1\n// expect-error: E2\nfirst\n\n// case: two\nsecond\nmore\n"), prelude)
	if len(cases) != 2 {
		t.Fatalf("got %d cases", len(cases))
	}
	if c := cases[0]; c.Name != "one" || len(c.Codes) != 2 || c.Codes[0] != "E1" || c.Codes[1] != "E2" ||
		c.Source != "shared\n\n// expect-error: E1\n// expect-error: E2\nfirst\n" {
		t.Errorf("first case: %+v", c)
	}
	if c := cases[1]; c.Name != "two" || len(c.Codes) != 0 || c.Source != "shared\n\nsecond\nmore\n" {
		t.Errorf("second case: %+v", c)
	}
	if got := Read(t, prelude, prelude); got != "shared\n\nshared\n" {
		t.Errorf("Read joined %q", got)
	}
}
