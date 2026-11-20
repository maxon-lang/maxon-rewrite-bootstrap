package par

import (
	"math/rand/v2"
	"runtime"
	"slices"
	"sync"
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

// When items panic, the panic that is raised is the lowest item's, though a higher item panics first.
func TestTheLowestPanickingItemIsRaisedWhateverTheTiming(t *testing.T) {
	const lowest, higher = 0, 3
	higherDone := make(chan struct{})
	defer func() {
		r := recover()
		w, ok := r.(*WorkerPanic)
		if !ok || w.Value != "item 0" {
			t.Fatalf("raised %v, want the panic of item 0", r)
		}
	}()
	ForEachIndex(10, 2, func(i int) {
		switch i {
		case higher:
			defer close(higherDone)
			panic("item 3")
		case lowest:
			<-higherDone
			panic("item 0")
		}
	})
	t.Fatal("no panic was raised")
}

// order records the tasks of a run in the order they started, and checks at the start of each that what it needs is done.
type order struct {
	mu       sync.Mutex
	plan     *Plan
	started  []int
	finished map[int]bool
	t        *testing.T
}

func newOrder(t *testing.T, plan *Plan) *order {
	return &order{plan: plan, finished: map[int]bool{}, t: t}
}

func (o *order) start(task int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, need := range o.plan.Needs[task] {
		if !o.finished[need] {
			o.t.Errorf("task %d started before task %d, which it needs, finished", task, need)
		}
	}
	o.started = append(o.started, task)
}

func (o *order) finish(task int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.finished[task] = true
}

// randomPlan is a plan of n tasks in which each needs some tasks of lower number, so it has no cycle, with a group for
// every pair of tasks.
func randomPlan(n int, seed uint64) *Plan {
	rng := rand.New(rand.NewPCG(seed, seed))
	plan := &Plan{Needs: make([][]int, n), Group: make([]int, n)}
	for t := range n {
		plan.Group[t] = t / 2
		if t == 0 {
			continue
		}
		for range rng.IntN(4) {
			need := rng.IntN(t)
			if !slices.Contains(plan.Needs[t], need) {
				plan.Needs[t] = append(plan.Needs[t], need)
			}
		}
	}
	return plan
}

func TestATaskStartsOnlyAfterTheTasksItNeedsFinish(t *testing.T) {
	for seed := range uint64(20) {
		plan := randomPlan(200, seed)
		for _, jobs := range []int{1, 2, 7, 32} {
			o := newOrder(t, plan)
			Run(plan, jobs, func() struct{} { return struct{}{} }, func(_ struct{}, task int) {
				o.start(task)
				runtime.Gosched()
				o.finish(task)
			})
			if len(o.started) != 200 {
				t.Fatalf("seed %d jobs %d: %d tasks ran, want 200", seed, jobs, len(o.started))
			}
		}
	}
}

// Of two modules that are each a task and the task that needs it, a worker finishes a module before it starts the next,
// though the other module's first task has the lower number.
func TestAWorkerContinuesWithTheModuleItWasOn(t *testing.T) {
	// 0: a-check, 1: b-check, 2: a-generate, 3: b-generate
	plan := &Plan{Needs: [][]int{nil, nil, {0}, {1}}, Group: []int{0, 1, 0, 1}}
	var ran []int
	Run(plan, 1, func() struct{} { return struct{}{} }, func(_ struct{}, task int) { ran = append(ran, task) })
	if want := []int{0, 2, 1, 3}; !slices.Equal(ran, want) {
		t.Errorf("tasks ran in the order %v, want %v", ran, want)
	}
}

// With nothing to continue with, a worker takes the ready task of the lowest number.
func TestAWorkerTakesTheLowestReadyTaskOtherwise(t *testing.T) {
	// 0 makes 3 and 4 ready, in other groups than its own, and 1 and 2 are ready from the start.
	plan := &Plan{Needs: [][]int{nil, nil, nil, {0}, {0}}, Group: []int{0, 1, 2, 3, 4}}
	var ran []int
	Run(plan, 1, func() struct{} { return struct{}{} }, func(_ struct{}, task int) { ran = append(ran, task) })
	if want := []int{0, 1, 2, 3, 4}; !slices.Equal(ran, want) {
		t.Errorf("tasks ran in the order %v, want %v", ran, want)
	}
}

// A task that depends on the tasks before it gets the same value whatever the number of jobs, which is what a caller
// that merges the results of tasks in task order relies on.
func TestResultsDoNotDependOnTheNumberOfJobs(t *testing.T) {
	plan := randomPlan(500, 7)
	compute := func(jobs int) []uint64 {
		results := make([]uint64, len(plan.Needs))
		Run(plan, jobs, func() struct{} { return struct{}{} }, func(_ struct{}, task int) {
			sum := uint64(task) + 1
			for _, need := range plan.Needs[task] {
				sum = sum*31 + results[need]
			}
			results[task] = sum
		})
		return results
	}
	want := compute(1)
	for _, jobs := range []int{2, 5, 16} {
		if got := compute(jobs); !slices.Equal(got, want) {
			t.Errorf("the results for %d jobs differ from those for 1", jobs)
		}
	}
}

// A panic in a task does not stop the tasks that are independent of it, and releases none that need it; the one raised is
// the lowest task's, though a higher task panics first.
func TestAPanickingTaskIsRaisedByIndexAndReleasesNoDependents(t *testing.T) {
	// 0 and 1 panic, 2 needs 0, 3 needs 1 and 4 is independent.
	plan := &Plan{Needs: [][]int{nil, nil, {0}, {1}, nil}, Group: []int{0, 1, 2, 3, 4}}
	for _, jobs := range []int{1, 2, 4} {
		var ran sync.Map
		oneDone := make(chan struct{})
		func() {
			defer func() {
				r := recover()
				w, ok := r.(*WorkerPanic)
				if !ok || w.Value != "task 0" {
					t.Errorf("jobs %d: raised %v, want the panic of task 0", jobs, r)
				}
			}()
			Run(plan, jobs, func() struct{} { return struct{}{} }, func(_ struct{}, task int) {
				ran.Store(task, true)
				switch task {
				case 1:
					defer close(oneDone)
					panic("task 1")
				case 0:
					if jobs > 1 {
						<-oneDone
					}
					panic("task 0")
				}
			})
		}()
		for _, task := range []int{2, 3} {
			if _, ok := ran.Load(task); ok {
				t.Errorf("jobs %d: task %d ran though a task it needs panicked", jobs, task)
			}
		}
		if _, ok := ran.Load(4); !ok {
			t.Errorf("jobs %d: the independent task did not run", jobs)
		}
	}
}
