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
				*errs = append(*errs, diag.NewError(diag.ErrBadModuleName,
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
	m.ID = ast.ModuleID(len(p.Modules))
	p.Modules = append(p.Modules, m)
	for _, name := range m.ChildNames() {
		collect(p, m.Children[name])
	}
}

// BuildTables fills in each module's declaration table and the project-wide name index; a duplicate declaration is an
// error in the result of the module that repeats it.
func BuildTables(p *ast.Project, results []ast.ModuleResult, jobs int) {
	declared := make([][]ast.Decl, len(p.Modules))
	par.ForEachIndex(len(p.Modules), jobs, func(i int) {
		declared[i] = BuildModuleTable(p.Modules[i], &results[i].Errors)
	})
	IndexNames(p, declared)
}

// BuildModuleTable fills in the module's own declaration table and an empty type object for each of its types, and
// returns the declarations the table holds, in source order. It reads nothing of another module, so it runs on the
// worker that parsed the module.
func BuildModuleTable(m *ast.Module, errs *[]*diag.CompileError) []ast.Decl {
	// A table that grows by doubling allocates twice its final size, and it holds one entry per declaration.
	m.Names = make(map[string]ast.Decl, len(m.Decls))
	for i, d := range m.Decls {
		b := d.Base()
		b.Module = m
		b.Ordinal = uint32(i)
		for _, method := range ast.Methods(d) {
			method.Module = m
		}
		if tb, ok := d.(ast.TypeDeclNode); ok {
			diag.Guard(errs, func() { tb.TypeBase().Ty = ast.Shell(tb) })
		}
		if kept, ok := m.Names[b.Name]; ok {
			file, line, _ := m.Positions.Where(kept.Position().Off)
			*errs = append(*errs, b.Error(diag.ErrDuplicateDefinition,
				"'"+b.Name+"' is already declared in "+m.Display()+" at "+file+":"+strconv.Itoa(line)))
			continue
		}
		m.Names[b.Name] = d
	}
	decls := make([]ast.Decl, 0, len(m.Names))
	for _, d := range m.Decls {
		if m.Names[d.Base().Name] == d {
			decls = append(decls, d)
		}
	}
	sort.SliceStable(decls, func(a, b int) bool {
		return decls[a].Position().Off < decls[b].Position().Off
	})
	return decls
}

// IndexNames builds the project-wide name index from each module's declarations, as BuildModuleTable returned them,
// modules in order so the index does not depend on how many compile at once.
func IndexNames(p *ast.Project, declared [][]ast.Decl) {
	for _, decls := range declared {
		for _, d := range decls {
			name := d.Base().Name
			p.ByName[name] = append(p.ByName[name], d)
		}
	}
}
