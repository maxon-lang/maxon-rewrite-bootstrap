package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const timingsText = `compile timings, goal units: total 10.0 ms
  stage                            ms       %
---------------------------------------------
  bodies                          9.0   90.0%
  total                          10.0  100.0%

compile timings, goal executable: total 200.0 ms
  stage                            ms       %
---------------------------------------------
  parse                          40.0   20.0%
  bodies                         30.0   15.0%
  verify                         10.0    5.0%
  emit                          120.0   60.0%
---------------------------------------------
  total                         200.0  100.0%

lower phases
  phase                            ms       %
---------------------------------------------
  lowering                       14.0  100.0%
`

func TestCheckShareIsTheLastTablesBodiesAndVerify(t *testing.T) {
	share, err := checkShare(timingsText)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(share-20) > 1e-9 {
		t.Fatalf("share %v, want 20", share)
	}
}

func TestCheckShareRefusesATableWithNoTotal(t *testing.T) {
	if _, err := checkShare("compile timings, goal executable: total 1 ms\n  parse 1.0 100.0%\n"); err == nil {
		t.Fatal("a table with no total gave a share")
	}
}

func TestCountChecksCountsBoundsAndEmptySlotCalls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.ir")
	ir := "b1:\n  bounds %3, %20 else b5\n  %21 = buffer %9\nb3:\n  runtime rt.panicEmptySlot(%15, %3)\n  trap\n  bounds %1, %2 else b6\n  call rt.panicIndex()\n"
	if err := os.WriteFile(path, []byte(ir), 0o644); err != nil {
		t.Fatal(err)
	}
	bounds, emptySlots, err := countChecks(path)
	if err != nil {
		t.Fatal(err)
	}
	if bounds != 2 || emptySlots != 1 {
		t.Fatalf("bounds %d, empty slots %d, want 2 and 1", bounds, emptySlots)
	}
}

func TestMedianOfOddAndEvenCounts(t *testing.T) {
	odd := []time.Duration{5, 1, 3}
	if got := median(odd); got != 3 {
		t.Fatalf("odd median %v, want 3", got)
	}
	even := []time.Duration{4, 1, 3, 2}
	if got := median(even); got != 2 {
		t.Fatalf("even median %v, want 2 (the mean of 2 and 3, truncated)", got)
	}
}

func TestRatioRangeIsOverPairs(t *testing.T) {
	low, high := ratioRange([]time.Duration{100, 100}, []time.Duration{99, 103})
	if low != 0.99 || high != 1.03 {
		t.Fatalf("range %v-%v, want 0.99-1.03", low, high)
	}
}
