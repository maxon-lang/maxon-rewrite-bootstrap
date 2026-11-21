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

var reservedTopLevel = map[string]bool{ast.RootQualifier: true, ast.StdlibQualifier: true, "runtime": true, "scriptlib": true}

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
	p.Root.IsRuntime = isRuntimeDir(path)
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

// BuildTables fills in each module's declaration table; a duplicate declaration is an error in the result of the module
// that repeats it.
func BuildTables(p *ast.Project, results []ast.ModuleResult, jobs int) {
	par.ForEachIndex(len(p.Modules), jobs, func(i int) {
		BuildModuleTable(p.Modules[i], &results[i].Errors)
	})
}

// BuildModuleTable fills in the module's own declaration table and an empty type object for each of its types. It reads
// nothing of another module, so it runs on the worker that parsed the module.
func BuildModuleTable(m *ast.Module, errs *[]*diag.CompileError) {
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
}

// runtimeDirName is the directory of the runtime's source (README, "The runtime").
const runtimeDirName = "runtime"

// runtimeDir is the runtime's source as maxon finds its library tiers: the runtime directory of the nearest directory,
// from the compiler's executable's upward, that has one. A compiler built in a checkout finds the checkout's, and an
// installed one the one installed beside it.
var runtimeDir = sync.OnceValues(func() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	for dir := filepath.Dir(exe); ; {
		candidate := filepath.Join(dir, runtimeDirName)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
})

// isRuntimeDir is whether a project's directory is the runtime's source, which is then checked as the runtime: it may
// call the __Raw intrinsics, and it is a library with no main.
func isRuntimeDir(path string) bool {
	dir, found := runtimeDir()
	if !found {
		return false
	}
	abs, err := filepath.Abs(path)
	return err == nil && strings.EqualFold(filepath.Clean(abs), dir)
}
