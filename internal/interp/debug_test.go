package interp_test

import (
	"os"
	"testing"
)

// TestExplainRejections prints the first checker error of each program the checker refuses, for tuning the generator: a
// generator that is refused too often reaches too little of what the checker accepts.
func TestExplainRejections(t *testing.T) {
	if os.Getenv("MINIMAXON_GEN_EXPLAIN") == "" {
		t.Skip("set MINIMAXON_GEN_EXPLAIN to list why programs are refused")
	}
	dir := t.TempDir()
	first := envInt(seedEnv, 1)
	for seed := first; seed < first+envInt(programsEnv, 20); seed++ {
		out := runGenerated(Program(seed), dir)
		if out.rejected != nil {
			e := out.rejected[0]
			t.Logf("seed %d: %s line %d: %s", seed, e.Code, e.Line, e.Message)
		}
	}
}
