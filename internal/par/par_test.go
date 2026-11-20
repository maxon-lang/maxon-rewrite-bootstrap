package par

import (
	"sync/atomic"
	"testing"
)

// Every item is done once, each worker makes its scratch on its own and keeps it for all the items it takes, and no
// more scratch is made than there are workers.
func TestEachWorkerHasScratchOfItsOwn(t *testing.T) {
	const items, jobs = 1000, 4
	var made atomic.Int32
	type scratch struct{ uses int }
	done := make([]int, items)
	var total atomic.Int32
	ForEachIndexWith(items, jobs, func() *scratch {
		made.Add(1)
		return &scratch{}
	}, func(s *scratch, i int) {
		s.uses++
		done[i]++
		total.Add(1)
	})
	if made.Load() < 1 || made.Load() > jobs {
		t.Errorf("%d scratch values were made for %d jobs", made.Load(), jobs)
	}
	if total.Load() != items {
		t.Errorf("%d items were done, want %d", total.Load(), items)
	}
	for i, n := range done {
		if n != 1 {
			t.Fatalf("item %d was done %d times", i, n)
		}
	}
}

func TestOneJobMakesOneScratchAndKeepsTheOrder(t *testing.T) {
	made := 0
	var order []int
	ForEachIndexWith(5, 1, func() int { made++; return 0 }, func(_ int, i int) { order = append(order, i) })
	if made != 1 || len(order) != 5 || order[0] != 0 || order[4] != 4 {
		t.Errorf("made %d scratch values, order %v", made, order)
	}
}
