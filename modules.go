package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Module struct {
	Path     string
	Dir      string
	IsStdlib bool
	Files    []string
	Decls    []Decl
	Names    map[string]Decl
	Children map[string]*Module
	DepSites map[*Module]Node
	Errors   []*CompileError

	Functions []*FuncDecl
	Pending   []PendingCall
	Ir        []*IrFunction
	Unit      *CodeUnit
}

func NewModule(path, dir string, isStdlib bool) *Module {
	return &Module{
		Path: path, Dir: dir, IsStdlib: isStdlib,
		Names: map[string]Decl{}, Children: map[string]*Module{}, DepSites: map[*Module]Node{},
	}
}

func (m *Module) Display() string {
	switch {
	case m.IsStdlib:
		return "stdlib"
	case m.Path == "":
		return "the project root"
	}
	return m.Path
}

func (m *Module) ChildNames() []string {
	names := make([]string, 0, len(m.Children))
	for n := range m.Children {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

type PendingCall struct {
	Call   *CallExpr
	Callee string
	Params []ParamSpec
}

type Project struct {
	Root    *Module
	Stdlib  *Module
	Modules []*Module
	ByName  map[string][]Decl
}

var reservedTopLevel = map[string]bool{"export": true, "stdlib": true, "runtime": true, "scriptlib": true}

func newProject(root *Module) *Project {
	p := &Project{Root: root, Stdlib: NewModule("stdlib", "", true), ByName: map[string][]Decl{}}
	for _, b := range []struct {
		name     string
		min, max I128
	}{
		{"ExitCode", Zero, U64(0xFFFFFFFF)},
		{"Count", Zero, U64Max},
		{"FullRangeInt", I64Min, I64Max},
	} {
		alias := &AliasDecl{Min: b.min, Max: b.max}
		alias.Name = b.name
		alias.Visibility = "public"
		alias.Module = p.Stdlib
		alias.File = "<stdlib>"
		alias.Ty = &IntTy{Alias: b.name, Min: b.min, Max: b.max}
		p.Stdlib.Decls = append(p.Stdlib.Decls, alias)
		p.Stdlib.Names[b.name] = alias
	}
	return p
}

func Discover(path string, errs *[]*CompileError) *Project {
	info, err := os.Stat(path)
	if err != nil {
		panic(NewError(ErrUnexpectedToken, "no such file or directory", path, 1, 1))
	}
	if !info.IsDir() {
		abs, _ := filepath.Abs(path)
		root := NewModule("", filepath.Dir(abs), false)
		root.Files = []string{path}
		p := newProject(root)
		p.Modules = []*Module{root}
		return p
	}
	p := newProject(NewModule("", path, false))
	var mu sync.Mutex
	p.addDirectory(p.Root, errs, &mu)
	p.collect(p.Root)
	return p
}

func isQualifier(name string) bool {
	if name == "" || IsReserved(name) {
		return false
	}
	for i, c := range name {
		letter := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
		if !letter && !(i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func (p *Project) addDirectory(m *Module, errs *[]*CompileError, mu *sync.Mutex) bool {
	entries, _ := os.ReadDir(m.Dir)
	var subs []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if !strings.HasPrefix(name, ".") && name != "bin" && name != "obj" {
				subs = append(subs, name)
			}
		} else if strings.HasSuffix(name, ".maxon") {
			m.Files = append(m.Files, filepath.Join(m.Dir, name))
		}
	}
	sort.Strings(m.Files)
	sort.Strings(subs)

	children := make([]*Module, len(subs))
	var wg sync.WaitGroup
	for i, name := range subs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			childPath := name
			if m.Path != "" {
				childPath = m.Path + "." + name
			}
			child := NewModule(childPath, filepath.Join(m.Dir, name), false)
			if !p.addDirectory(child, errs, mu) {
				return
			}
			if !isQualifier(name) || (m == p.Root && reservedTopLevel[name]) {
				mu.Lock()
				*errs = append(*errs, NewError("E3182",
					"the directory '"+name+"' cannot be written as a qualifier; rename it to an identifier that is not reserved",
					child.Dir, 1, 1))
				mu.Unlock()
				return
			}
			children[i] = child
		}()
	}
	wg.Wait()

	for i, child := range children {
		if child != nil {
			m.Children[subs[i]] = child
		}
	}
	return len(m.Files) > 0 || len(m.Children) > 0
}

func (p *Project) collect(m *Module) {
	p.Modules = append(p.Modules, m)
	for _, name := range m.ChildNames() {
		p.collect(m.Children[name])
	}
}

func Methods(d Decl) []*FuncDecl {
	switch t := d.(type) {
	case *TypeDecl:
		return t.Methods
	case *EnumDecl:
		return t.Methods
	case *UnionDecl:
		return t.Methods
	}
	return nil
}

func (p *Project) BuildTables(jobs int) {
	ForEach(p.Modules, jobs, func(m *Module) {
		for _, d := range m.Decls {
			b := d.Base()
			b.Module = m
			for _, method := range Methods(d) {
				method.Module = m
			}
			if tb, ok := d.(TypeDeclNode); ok {
				tb.TypeBase().Ty = Shell(tb)
			}
			if kept, ok := m.Names[b.Name]; ok {
				k := kept.P()
				m.Errors = append(m.Errors, b.Error(ErrDuplicateDefinition,
					"'"+b.Name+"' is already declared in "+m.Display()+" at "+k.File+":"+strconv.Itoa(k.Line)))
				continue
			}
			m.Names[b.Name] = d
		}
	})

	perModule := make([][]Decl, len(p.Modules))
	ForEachIndex(len(p.Modules), jobs, func(i int) {
		m := p.Modules[i]
		decls := make([]Decl, 0, len(m.Names))
		for _, d := range m.Decls {
			if m.Names[d.Base().Name] == d {
				decls = append(decls, d)
			}
		}
		sort.SliceStable(decls, func(a, b int) bool {
			pa, pb := decls[a].P(), decls[b].P()
			if pa.File != pb.File {
				return pa.File < pb.File
			}
			return pa.Line < pb.Line
		})
		perModule[i] = decls
	})
	for _, decls := range perModule {
		for _, d := range decls {
			name := d.Base().Name
			p.ByName[name] = append(p.ByName[name], d)
		}
	}
}

func (p *Project) DependencyOrder(errs *[]*CompileError) bool {
	remaining := map[*Module]bool{}
	for _, m := range p.Modules {
		remaining[m] = true
	}
	for len(remaining) > 0 {
		var ready *Module
		for _, m := range p.Modules {
			if !remaining[m] {
				continue
			}
			blocked := false
			for d := range m.DepSites {
				if remaining[d] {
					blocked = true
					break
				}
			}
			if !blocked {
				ready = m
				break
			}
		}
		if ready == nil {
			*errs = append(*errs, p.cycleError(remaining))
			return false
		}
		delete(remaining, ready)
	}
	return true
}

func (p *Project) cycleError(remaining map[*Module]bool) *CompileError {
	var start *Module
	for _, m := range p.Modules {
		if remaining[m] {
			start = m
			break
		}
	}
	path := []*Module{start}
	for {
		var deps []*Module
		for d := range path[len(path)-1].DepSites {
			if remaining[d] {
				deps = append(deps, d)
			}
		}
		sort.Slice(deps, func(a, b int) bool { return deps[a].Path < deps[b].Path })
		next := deps[0]
		for at, m := range path {
			if m == next {
				cycle := append(append([]*Module{}, path[at:]...), next)
				names := make([]string, len(cycle))
				for i, c := range cycle {
					names[i] = c.Display()
				}
				site := cycle[0].DepSites[cycle[1]].P()
				return site.Error("E9001", "modules depend on each other in a cycle: "+strings.Join(names, " -> "))
			}
		}
		path = append(path, next)
	}
}

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
	var next int64 = -1
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
				mu.Lock()
				next++
				i := int(next)
				mu.Unlock()
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
