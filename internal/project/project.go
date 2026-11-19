package project

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"minimaxon/internal/par"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var reservedTopLevel = map[string]bool{"export": true, "stdlib": true, "runtime": true, "scriptlib": true}

func Discover(path string, errs *[]*diag.CompileError) *ast.Project {
	info, err := os.Stat(path)
	if err != nil {
		panic(diag.NewError(diag.ErrUnexpectedToken, "no such file or directory", path, 1, 1))
	}
	if !info.IsDir() {
		abs, _ := filepath.Abs(path)
		root := ast.NewModule("", filepath.Dir(abs), false)
		root.Files = []string{path}
		p := ast.NewProject(root)
		p.Modules = []*ast.Module{root}
		return p
	}
	p := ast.NewProject(ast.NewModule("", path, false))
	var mu sync.Mutex
	addDirectory(p, p.Root, errs, &mu)
	collect(p, p.Root)
	return p
}

func isQualifier(name string) bool {
	if name == "" || ast.IsReserved(name) {
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

func addDirectory(p *ast.Project, m *ast.Module, errs *[]*diag.CompileError, mu *sync.Mutex) bool {
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

	children := make([]*ast.Module, len(subs))
	var wg sync.WaitGroup
	for i, name := range subs {
		wg.Go(func() {
			childPath := name
			if m.Path != "" {
				childPath = m.Path + "." + name
			}
			child := ast.NewModule(childPath, filepath.Join(m.Dir, name), false)
			if !addDirectory(p, child, errs, mu) {
				return
			}
			if !isQualifier(name) || (m == p.Root && reservedTopLevel[name]) {
				mu.Lock()
				*errs = append(*errs, diag.NewError("E3182",
					"the directory '"+name+"' cannot be written as a qualifier; rename it to an identifier that is not reserved",
					child.Dir, 1, 1))
				mu.Unlock()
				return
			}
			children[i] = child
		})
	}
	wg.Wait()

	for i, child := range children {
		if child != nil {
			m.Children[subs[i]] = child
		}
	}
	return len(m.Files) > 0 || len(m.Children) > 0
}

func collect(p *ast.Project, m *ast.Module) {
	p.Modules = append(p.Modules, m)
	for _, name := range m.ChildNames() {
		collect(p, m.Children[name])
	}
}

func BuildTables(p *ast.Project, jobs int) {
	par.ForEach(p.Modules, jobs, func(m *ast.Module) {
		for _, d := range m.Decls {
			b := d.Base()
			b.Module = m
			for _, method := range ast.Methods(d) {
				method.Module = m
			}
			if tb, ok := d.(ast.TypeDeclNode); ok {
				diag.Guard(&m.Errors, func() { tb.TypeBase().Ty = ast.Shell(tb) })
			}
			if kept, ok := m.Names[b.Name]; ok {
				k := kept.P()
				m.Errors = append(m.Errors, b.Error(diag.ErrDuplicateDefinition,
					"'"+b.Name+"' is already declared in "+m.Display()+" at "+k.File+":"+strconv.Itoa(k.Line)))
				continue
			}
			m.Names[b.Name] = d
		}
	})

	perModule := make([][]ast.Decl, len(p.Modules))
	par.ForEachIndex(len(p.Modules), jobs, func(i int) {
		m := p.Modules[i]
		decls := make([]ast.Decl, 0, len(m.Names))
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

func DependencyOrder(p *ast.Project, errs *[]*diag.CompileError) bool {
	remaining := map[*ast.Module]bool{}
	for _, m := range p.Modules {
		remaining[m] = true
	}
	for len(remaining) > 0 {
		var ready *ast.Module
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
			*errs = append(*errs, cycleError(p, remaining))
			return false
		}
		delete(remaining, ready)
	}
	return true
}

func cycleError(p *ast.Project, remaining map[*ast.Module]bool) *diag.CompileError {
	var start *ast.Module
	for _, m := range p.Modules {
		if remaining[m] {
			start = m
			break
		}
	}
	path := []*ast.Module{start}
	for {
		var deps []*ast.Module
		for d := range path[len(path)-1].DepSites {
			if remaining[d] {
				deps = append(deps, d)
			}
		}
		sort.Slice(deps, func(a, b int) bool { return deps[a].Path < deps[b].Path })
		next := deps[0]
		for at, m := range path {
			if m == next {
				cycle := append(append([]*ast.Module{}, path[at:]...), next)
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
