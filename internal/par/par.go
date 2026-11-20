package par

import (
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

func ForEach[T any](items []T, jobs int, work func(T)) {
	ForEachIndex(len(items), jobs, func(i int) { work(items[i]) })
}

func ForEachIndex(n, jobs int, work func(int)) {
	ForEachIndexWith(n, jobs, func() struct{} { return struct{}{} }, func(_ struct{}, i int) { work(i) })
}

// ForEachIndexWith is ForEachIndex where each worker has a value of its own, made by newScratch on the worker's goroutine
// and handed to every item that worker takes. It is where a phase keeps the buffers it reuses from one item to the next, so
// the items allocate nothing once the buffers have grown; no two goroutines ever share one.
//
// A panic in an item is raised once every worker is done, and is the one of the lowest item that panicked, so what a caller
// sees does not depend on which worker got there first.
func ForEachIndexWith[S any](n, jobs int, newScratch func() S, work func(scratch S, i int)) {
	if jobs <= 1 || n <= 1 {
		scratch := newScratch()
		for i := range n {
			work(scratch, i)
		}
		return
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	var failures lowestFailure
	workers := min(jobs, n)
	for range workers {
		wg.Go(func() {
			at := -1
			defer func() {
				if r := recover(); r != nil {
					failures.record(at, r)
				}
			}()
			scratch := newScratch()
			for {
				at = int(next.Add(1) - 1)
				if at >= n {
					return
				}
				work(scratch, at)
			}
		})
	}
	wg.Wait()
	failures.raise()
}

// WorkerPanic is a panic that happened on a worker's goroutine, with the stack it happened on, which the goroutine that
// raises it again no longer has.
type WorkerPanic struct {
	Value any
	Stack []byte
}

func (w *WorkerPanic) Error() string { return fmt.Sprintf("%v\n%s", w.Value, w.Stack) }

// Wrap is the recovered value of a panic as a WorkerPanic, taking the stack from the panicking goroutine, so it must be
// called by the deferred function that recovered it. A value that is one already is returned as it is.
func Wrap(recovered any) *WorkerPanic {
	if w, ok := recovered.(*WorkerPanic); ok {
		return w
	}
	return &WorkerPanic{Value: recovered, Stack: debug.Stack()}
}

// lowestFailure keeps the panic of the lowest item or task that panicked.
type lowestFailure struct {
	mu    sync.Mutex
	index int
	panic *WorkerPanic
}

func (f *lowestFailure) record(index int, recovered any) {
	w := Wrap(recovered)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.panic == nil || index < f.index {
		f.index, f.panic = index, w
	}
}

// raise panics with the recorded failure, if there is one.
func (f *lowestFailure) raise() {
	if f.panic != nil {
		panic(f.panic)
	}
}
