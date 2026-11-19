package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	bytesAllocatedPerSourceByte = 100
	minNoGcBudget               = int64(256) << 20
	bytesPerMegabyte            = 1048576.0
	initialTokenCapacity        = 1 << 16
	heapAllocsMetric            = "/gc/heap/allocs:bytes"
)

var tokenBuffers = sync.Pool{New: func() any {
	buffer := make([]Token, 0, initialTokenCapacity)
	return &buffer
}}

func DefaultJobs() int { return max(1, runtime.NumCPU()-1) }

func heapAllocated() uint64 {
	sample := []metrics.Sample{{Name: heapAllocsMetric}}
	metrics.Read(sample)
	return sample[0].Value.Uint64()
}

func Compile(path, output string, jobs int, timings, emitIr bool) (errs []*CompileError) {
	defer func() {
		if r := recover(); r != nil {
			ce, ok := r.(*CompileError)
			if !ok {
				panic(r)
			}
			errs = []*CompileError{ce}
		}
	}()
	return compileProject(path, output, jobs, timings, emitIr)
}

func compileProject(path, output string, jobs int, timings, emitIr bool) []*CompileError {
	var errors []*CompileError
	clock := time.Now()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	allocated, collections, paused := stats.TotalAlloc, stats.NumGC, stats.PauseTotalNs
	mark := func(phase string) {
		if timings {
			runtime.ReadMemStats(&stats)
			fmt.Fprintf(os.Stderr, "%-10s %8.1f ms %8.1f MB %4d GCs %7.1f ms paused\n", phase,
				float64(time.Since(clock).Nanoseconds())/1e6,
				float64(stats.TotalAlloc-allocated)/bytesPerMegabyte,
				stats.NumGC-collections,
				float64(stats.PauseTotalNs-paused)/1e6)
			allocated, collections, paused = stats.TotalAlloc, stats.NumGC, stats.PauseTotalNs
		}
		clock = time.Now()
	}

	project := Discover(path, &errors)
	budget, restore := suspendCollection(project, jobs)
	defer restore()
	if timings {
		fmt.Fprintf(os.Stderr, "no-GC budget %d MB\n", budget>>20)
	}
	mark("discover")
	if len(errors) > 0 {
		return sortedErrors(errors)
	}
	modules := project.Modules

	phase := func(work func(*Module)) { ForEach(modules, jobs, work) }

	failed := func() []*CompileError {
		all := append([]*CompileError{}, errors...)
		for _, m := range modules {
			all = append(all, m.Errors...)
		}
		if len(all) == 0 {
			return nil
		}
		return sortedErrors(all)
	}

	var readBytes, lexBytes, parseBytes atomic.Uint64
	parseStart := heapAllocated()
	phase(func(m *Module) {
		Guard(&m.Errors, func() {
			a0 := stepAllocated(timings)
			buffer, slices := LoadModuleSource(m.Files)
			a1 := stepAllocated(timings)
			scratch := tokenBuffers.Get().(*[]Token)
			tokens := Lex(buffer, slices, *scratch, map[string]string{})
			a2 := stepAllocated(timings)
			files := make([]string, len(slices))
			for i, s := range slices {
				files[i] = s.File
			}
			parser := NewParser(tokens, files)
			m.Decls = append(m.Decls, parser.ParseProgram().Decls...)
			m.Errors = append(m.Errors, parser.Errors...)
			a3 := stepAllocated(timings)
			*scratch = tokens[:0]
			tokenBuffers.Put(scratch)
			readBytes.Add(a1 - a0)
			lexBytes.Add(a2 - a1)
			parseBytes.Add(a3 - a2)
		})
	})
	if timings {
		reportParseAllocation(heapAllocated()-parseStart, readBytes.Load(), lexBytes.Load(), parseBytes.Load())
	}
	mark("parse")
	if parseErrors := failed(); parseErrors != nil {
		return parseErrors
	}

	project.BuildTables(jobs)
	mark("tables")
	if tableErrors := failed(); tableErrors != nil {
		return tableErrors
	}

	phase(func(m *Module) { NewResolver(project, m).Run() })
	mark("resolve")
	if resolveErrors := failed(); resolveErrors != nil {
		return resolveErrors
	}
	if !project.DependencyOrder(&errors) {
		return sortedErrors(errors)
	}
	mark("cycles")

	var main *FuncDecl
	phase(func(m *Module) {
		checker := NewChecker(project, m, &m.Errors)
		for _, d := range m.Decls {
			m.Functions = append(m.Functions, checker.Declare(d)...)
		}
		if m == project.Root {
			main = checker.CheckMain()
		}
	})
	mark("declare")
	if declareErrors := failed(); declareErrors != nil {
		return declareErrors
	}

	checkConstants(project, jobs, &errors)
	mark("constants")
	if constErrors := failed(); constErrors != nil {
		return constErrors
	}

	phase(func(m *Module) {
		checker := NewChecker(project, m, &m.Errors)
		for _, d := range m.Decls {
			switch d.(type) {
			case *TypeDecl, *UnionDecl:
				checker.CheckBody(d)
			}
		}
		for _, f := range m.Functions {
			checker.CheckBody(f)
		}
		m.Pending = append(m.Pending, checker.Pending...)
	})
	mark("bodies")
	if bodyErrors := failed(); bodyErrors != nil {
		return bodyErrors
	}

	phase(func(m *Module) {
		NewChecker(project, m, &m.Errors).Verify(m.Pending)
		if len(m.Errors) == 0 {
			ownership := NewOwnership(&m.Errors)
			for _, f := range m.Functions {
				ownership.Check(f)
			}
		}
	})
	mark("verify")
	if verifyErrors := failed(); verifyErrors != nil {
		return verifyErrors
	}

	if output != "" {
		phase(func(m *Module) {
			m.Ir = make([]*IrFunction, len(m.Functions))
			for i, f := range m.Functions {
				m.Ir[i] = Lower(f)
			}
		})
		mark("lower")
		if emitIr {
			var text strings.Builder
			for _, m := range modules {
				for _, fn := range m.Ir {
					PrintIr(fn, &text)
				}
			}
			if err := os.WriteFile(output+".ir", []byte(text.String()), 0o644); err != nil {
				panic(Internal("cannot write %s.ir: %v", output, err))
			}
		}
		var runtimeUnit *CodeUnit
		emitModules := func() { phase(func(m *Module) { m.Unit = EmitModule(m.Ir) }) }
		if jobs <= 1 {
			runtimeUnit = EmitRuntime(main)
			emitModules()
		} else {
			var failure any
			var done sync.WaitGroup
			done.Add(1)
			go func() {
				defer done.Done()
				defer func() { failure = recover() }()
				runtimeUnit = EmitRuntime(main)
			}()
			emitModules()
			done.Wait()
			if failure != nil {
				panic(failure)
			}
		}
		mark("codegen")
		units := make([]*CodeUnit, 0, len(modules)+1)
		units = append(units, runtimeUnit)
		for _, m := range modules {
			units = append(units, m.Unit)
		}
		Link(output, units, RtEntry, jobs)
		mark("link")
	}
	return nil
}

func stepAllocated(timings bool) uint64 {
	if !timings {
		return 0
	}
	return heapAllocated()
}

func reportParseAllocation(phaseTotal, read, lex, parse uint64) {
	sum := read + lex + parse
	scale := 1.0
	if sum > 0 {
		scale = float64(phaseTotal) / float64(sum)
	}
	fmt.Fprintf(os.Stderr, "  read %.1f MB, lex %.1f MB, parse %.1f MB\n",
		float64(read)*scale/bytesPerMegabyte, float64(lex)*scale/bytesPerMegabyte, float64(parse)*scale/bytesPerMegabyte)
}

type constEntry struct {
	module *Module
	decl   *ConstDecl
}

func checkConstants(project *Project, jobs int, errs *[]*CompileError) {
	var consts []constEntry
	for _, m := range project.Modules {
		for _, d := range m.Decls {
			if c, ok := d.(*ConstDecl); ok {
				consts = append(consts, constEntry{m, c})
			}
		}
	}
	deps := make([][]*ConstDecl, len(consts))
	ForEachIndex(len(consts), jobs, func(i int) {
		seen := map[*ConstDecl]bool{}
		for _, c := range ConstRefs(consts[i].decl.Value, nil) {
			if !seen[c] {
				seen[c] = true
				deps[i] = append(deps[i], c)
			}
		}
	})

	index := make(map[*ConstDecl]int, len(consts))
	for i, c := range consts {
		index[c.decl] = i
	}
	depth := make([]int, len(consts))
	state := make([]byte, len(consts))
	const visiting, done = 1, 2

	var depthOf func(i int) int
	depthOf = func(i int) int {
		if state[i] == done {
			return depth[i]
		}
		if state[i] == visiting {
			panic(consts[i].decl.Error("E2012", "constant '"+consts[i].decl.Name+"' depends on itself"))
		}
		state[i] = visiting
		depth[i] = 0
		for _, d := range deps[i] {
			depth[i] = max(depth[i], depthOf(index[d])+1)
		}
		state[i] = done
		return depth[i]
	}

	for i := range consts {
		before := len(*errs)
		Guard(errs, func() { depthOf(i) })
		if len(*errs) > before {
			return
		}
	}

	var waves [][]int
	for i := range consts {
		for len(waves) <= depth[i] {
			waves = append(waves, nil)
		}
		waves[depth[i]] = append(waves[depth[i]], i)
	}
	for _, items := range waves {
		waveErrors := make([][]*CompileError, len(items))
		ForEachIndex(len(items), jobs, func(k int) {
			entry := consts[items[k]]
			NewChecker(project, entry.module, &waveErrors[k]).CheckConst(entry.decl)
		})
		for _, e := range waveErrors {
			*errs = append(*errs, e...)
		}
	}
}

func ConstRefs(e Expr, into []*ConstDecl) []*ConstDecl {
	switch e := e.(type) {
	case *NameExpr:
		if c, ok := e.Target.(*ConstDecl); ok {
			return append(into, c)
		}
	case *BinaryExpr:
		return ConstRefs(e.R, ConstRefs(e.L, into))
	case *UnaryExpr:
		return ConstRefs(e.E, into)
	case *CondExpr:
		return ConstRefs(e.Else, ConstRefs(e.Then, ConstRefs(e.Cond, into)))
	case *CastExpr:
		return ConstRefs(e.E, into)
	case *MemberExpr:
		return ConstRefs(e.Obj, into)
	case *StrLit:
		for _, p := range e.Parts {
			if part, ok := p.(Expr); ok {
				into = ConstRefs(part, into)
			}
		}
	}
	return into
}

func suspendCollection(project *Project, jobs int) (int64, func()) {
	var source atomic.Int64
	ForEach(project.Modules, jobs, func(m *Module) {
		for _, f := range m.Files {
			if info, err := os.Stat(f); err == nil {
				source.Add(info.Size())
			}
		}
	})
	budget := max(source.Load()*bytesAllocatedPerSourceByte, minNoGcBudget)
	oldPercent := debug.SetGCPercent(-1)
	oldLimit := debug.SetMemoryLimit(budget)
	return budget, func() {
		debug.SetGCPercent(oldPercent)
		debug.SetMemoryLimit(oldLimit)
	}
}

func sortedErrors(errs []*CompileError) []*CompileError {
	out := append([]*CompileError{}, errs...)
	sort.SliceStable(out, func(a, b int) bool {
		x, y := out[a], out[b]
		if x.File != y.File {
			return x.File < y.File
		}
		if x.Line != y.Line {
			return x.Line < y.Line
		}
		if x.Col != y.Col {
			return x.Col < y.Col
		}
		return x.Code < y.Code
	})
	return out
}
