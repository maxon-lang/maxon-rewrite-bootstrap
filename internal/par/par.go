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
	if jobs <= 1 || n <= 1 {
		for i := 0; i < n; i++ {
			work(i)
		}
		return
	}
	var next atomic.Int64
	var mu sync.Mutex
	var wg sync.WaitGroup
	var failure any
	workers := min(jobs, n)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
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
			for {
				i := int(next.Add(1) - 1)
				if i >= n {
					return
				}
				work(i)
			}
		}()
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
