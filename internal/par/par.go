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
func ForEachIndexWith[S any](n, jobs int, newScratch func() S, work func(scratch S, i int)) {
	if jobs <= 1 || n <= 1 {
		scratch := newScratch()
		for i := range n {
			work(scratch, i)
		}
		return
	}
	var next atomic.Int64
	var mu sync.Mutex
	var wg sync.WaitGroup
	var failure any
	workers := min(jobs, n)
	for range workers {
		wg.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					if _, wrapped := r.(*WorkerPanic); !wrapped {
						r = &WorkerPanic{Value: r, Stack: debug.Stack()}
					}
					mu.Lock()
					if failure == nil {
						failure = r
					}
					mu.Unlock()
				}
			}()
			scratch := newScratch()
			for {
				i := int(next.Add(1) - 1)
				if i >= n {
					return
				}
				work(scratch, i)
			}
		})
	}
	wg.Wait()
	if failure != nil {
		panic(failure)
	}
}

type WorkerPanic struct {
	Value any
	Stack []byte
}

func (w *WorkerPanic) Error() string { return fmt.Sprintf("%v\n%s", w.Value, w.Stack) }
