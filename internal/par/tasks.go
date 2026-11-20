package par

import (
	"slices"
	"sync"
)

// Plan is a set of tasks and the order they run in. Tasks are numbered from 0, and a task runs only after every task it
// needs has finished. It is a graph of handles and nothing else, so it is the same for any number of jobs.
type Plan struct {
	// Needs[t] are the tasks that task t waits for.
	Needs [][]int
	// Group[t] is what task t works on, such as a module. A worker that finishes a task goes on to a task that has just become
	// ready in the same group, if there is one, while the data of the group is in its cache.
	Group []int
}

// Run runs every task of the plan on a pool of jobs workers, each with a scratch value of its own as in
// ForEachIndexWith. A worker takes the task it has just made ready in its own group, else the ready task of the lowest
// number, so the numbering is the priority.
//
// A task writes only what belongs to it, and the tasks it needed have finished before it starts, so tasks need no locks
// and a caller that merges their results in task order gets the same for any number of jobs. A task that panics does not
// release the tasks that need it, the others run on, and once they are done Run raises the panic of the lowest task that
// panicked. A plan with a cycle leaves the tasks on it unrun.
func Run[S any](plan *Plan, jobs int, newScratch func() S, work func(scratch S, task int)) {
	s := newScheduler(plan)
	newWorker := func() func(task int) {
		scratch := newScratch()
		return func(task int) { work(scratch, task) }
	}
	if jobs <= 1 || len(plan.Needs) <= 1 {
		s.work(newWorker)
	} else {
		var wg sync.WaitGroup
		for range min(jobs, len(plan.Needs)) {
			wg.Go(func() { s.work(newWorker) })
		}
		wg.Wait()
	}
	s.failures.raise()
}

// none is the absence of a task, where a task number is held.
const none = -1

type scheduler struct {
	group []int
	// dependents[starts[t]:starts[t+1]] are the tasks that need task t, ascending.
	starts     []int
	dependents []int

	mu    sync.Mutex
	wake  sync.Cond
	ready taskHeap
	// waiting[t] is how many of the tasks that task t needs have not finished.
	waiting []int
	// running is how many tasks workers hold, which is every one that has been taken and has not finished.
	running  int
	failures lowestFailure
}

func newScheduler(plan *Plan) *scheduler {
	n := len(plan.Needs)
	s := &scheduler{group: plan.Group, starts: make([]int, n+1), waiting: make([]int, n)}
	s.wake.L = &s.mu
	edges := 0
	for t, needs := range plan.Needs {
		s.waiting[t] = len(needs)
		for _, need := range needs {
			s.starts[need+1]++
		}
		edges += len(needs)
	}
	for t := range n {
		s.starts[t+1] += s.starts[t]
	}
	s.dependents = make([]int, edges)
	filled := slices.Clone(s.starts[:n])
	for t, needs := range plan.Needs {
		for _, need := range needs {
			s.dependents[filled[need]] = t
			filled[need]++
		}
	}
	for t, waiting := range s.waiting {
		if waiting == 0 {
			s.ready.push(t)
		}
	}
	return s
}

// work is one worker's loop: take a task, run it, release the tasks that were waiting for it.
func (s *scheduler) work(newWorker func() func(task int)) {
	do := newWorker()
	task := none
	for {
		s.mu.Lock()
		if task == none {
			for len(s.ready) == 0 && s.running > 0 {
				s.wake.Wait()
			}
			if len(s.ready) == 0 {
				s.mu.Unlock()
				return
			}
			task = s.ready.pop()
			s.running++
		}
		s.mu.Unlock()

		finished := s.run(do, task)

		s.mu.Lock()
		next, queued := none, false
		if finished {
			next, queued = s.release(task)
		}
		s.running--
		if queued || s.running == 0 {
			s.wake.Broadcast()
		}
		s.mu.Unlock()
		task = next
	}
}

// run is whether the task finished, rather than panicking.
func (s *scheduler) run(do func(task int), task int) (finished bool) {
	defer func() {
		if r := recover(); r != nil {
			s.failures.record(task, r)
		}
	}()
	do(task)
	return true
}

// release counts task as done for the tasks that need it, with s.mu held. The first one that becomes ready in the task's
// group is returned for the worker to run next, counted as running; the others that become ready are queued, and queued
// says whether there are any.
func (s *scheduler) release(task int) (next int, queued bool) {
	next = none
	for _, d := range s.dependents[s.starts[task]:s.starts[task+1]] {
		s.waiting[d]--
		if s.waiting[d] > 0 {
			continue
		}
		if next == none && s.group[d] == s.group[task] {
			next = d
			s.running++
			continue
		}
		s.ready.push(d)
		queued = true
	}
	return next, queued
}

// taskHeap is a binary min-heap of task numbers.
type taskHeap []int

func (h *taskHeap) push(t int) {
	*h = append(*h, t)
	heap := *h
	for i := len(heap) - 1; i > 0; {
		parent := (i - 1) / 2
		if heap[parent] <= heap[i] {
			break
		}
		heap[parent], heap[i] = heap[i], heap[parent]
		i = parent
	}
}

func (h *taskHeap) pop() int {
	heap := *h
	top := heap[0]
	last := len(heap) - 1
	heap[0] = heap[last]
	heap = heap[:last]
	for i := 0; ; {
		smallest := i
		for _, child := range [2]int{2*i + 1, 2*i + 2} {
			if child < len(heap) && heap[child] < heap[smallest] {
				smallest = child
			}
		}
		if smallest == i {
			break
		}
		heap[smallest], heap[i] = heap[i], heap[smallest]
		i = smallest
	}
	*h = heap
	return top
}
