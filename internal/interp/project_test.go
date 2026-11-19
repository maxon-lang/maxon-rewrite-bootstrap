package interp_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minimaxon/internal/driver"
)

// TestWriteProject writes a project of MINIMAXON_GEN_MODULES modules to the directory in MINIMAXON_GEN_PROJECT, each one a
// generated program that the checker accepts and the compiler can build. It is a large input for measuring the compiler's phases.
func TestWriteProject(t *testing.T) {
	root := os.Getenv("MINIMAXON_GEN_PROJECT")
	if root == "" {
		t.Skip("MINIMAXON_GEN_PROJECT names the directory to write")
	}
	modules := int(envInt("MINIMAXON_GEN_MODULES", 100))
	scratch := t.TempDir()
	var calls []string
	for seed, written := envInt(seedEnv, 1), 0; written < modules; seed++ {
		source := Program(seed)
		out := runGenerated(source, scratch)
		if out.rejected != nil || out.internal != nil || out.result == nil || len(out.result.Violations) > 0 {
			continue
		}
		if errs := driver.Compile(filepath.Join(scratch, "p.maxon"), filepath.Join(scratch, "p.exe"), driver.Options{Jobs: 1}); len(errs) > 0 {
			continue
		}
		name := fmt.Sprintf("m%d", written)
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		source = strings.Replace(source, kwFunction+" main() "+kwReturns+" ExitCode", kwExport+" "+kwFunction+" run() "+kwReturns+" ExitCode", 1)
		source = strings.Replace(source, kwEnd+" 'main'", kwEnd+" 'run'", 1)
		if err := os.WriteFile(filepath.Join(dir, "lib.maxon"), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, fmt.Sprintf("\t_ = %s.run()", name))
		written++
	}
	main := kwFunction + " main() " + kwReturns + " ExitCode\n" + strings.Join(calls, "\n") + "\n\t" + kwReturn + " 0\n" + kwEnd + " 'main'\n"
	if err := os.WriteFile(filepath.Join(root, "main.maxon"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
}
