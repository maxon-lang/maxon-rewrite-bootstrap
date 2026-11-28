package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The focused gate does not regenerate specs/ for a change of cmd/convert/builtins_gen.go (libraryOnlyTables), because the
// conversion of the spec tests never consults the maps in it. This holds the converter to that, so that the gate's rule cannot
// go on skipping a regeneration that the converter has begun to need: a function that reads a map is reached from the spec
// entry point only through an `if kind == sourceFile` that the spec conversion does not take. The reach is by name, so it
// takes every function, method or value of a name for the one called, and can only over-report.

const (
	convertDir    = "../convert"
	specEntry     = "ConvertSpec"
	libraryEntry  = "Convert"
	generatedFile = "builtins_gen.go"
	libraryKind   = "sourceFile"
)

var builtinTables = []string{"builtinNames", "builtinMethods", "libraryRawMembers"}

type converterSource struct {
	// funcs are the declarations of each function name, and tableUsers the names of those that name a table.
	funcs      map[string][]*ast.FuncDecl
	tableUsers map[string]bool
	// outside is where a table is named by something that is not a function, which runs whatever is converted.
	outside []string
}

func parseConverter(t *testing.T) converterSource {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(convertDir, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no source of cmd/convert in %s: %v", convertDir, err)
	}
	src := converterSource{funcs: map[string][]*ast.FuncDecl{}, tableUsers: map[string]bool{}}
	fset := token.NewFileSet()
	for _, file := range files {
		base := filepath.Base(file)
		if strings.HasSuffix(base, "_test.go") || base == generatedFile {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range parsed.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				src.funcs[fn.Name.Name] = append(src.funcs[fn.Name.Name], fn)
				if namesTable(fn) {
					src.tableUsers[fn.Name.Name] = true
				}
			} else if namesTable(decl) {
				src.outside = append(src.outside, base)
			}
		}
	}
	return src
}

func namesTable(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && slices.Contains(builtinTables, id.Name) {
			found = true
		}
		return !found
	})
	return found
}

// requiresLibrary is whether the condition is true only for a library source file: `kind == sourceFile`, or a conjunction with
// one.
func requiresLibrary(cond ast.Expr) bool {
	bin, ok := cond.(*ast.BinaryExpr)
	if !ok {
		return false
	}
	switch bin.Op {
	case token.EQL:
		id, ok := bin.Y.(*ast.Ident)
		return ok && id.Name == libraryKind
	case token.LAND:
		return requiresLibrary(bin.X) || requiresLibrary(bin.Y)
	}
	return false
}

// writtenNames calls fn with each identifier that a run of the code can write; a run of a spec conversion (spec) does not take the body
// of an if that requires a library file, though it takes the condition and the else.
func writtenNames(n ast.Node, spec bool, fn func(string)) {
	ast.Inspect(n, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			fn(n.Name)
		case *ast.IfStmt:
			if spec && requiresLibrary(n.Cond) {
				for _, part := range []ast.Node{n.Init, n.Cond, n.Else} {
					if part != nil {
						writtenNames(part, spec, fn)
					}
				}
				return false
			}
		}
		return true
	})
}

// reach is the names of the functions that a run from the entry can call, each with the function it was reached from.
func (s converterSource) reach(entry string, spec bool) map[string]string {
	seen := map[string]string{entry: ""}
	for work := []string{entry}; len(work) > 0; {
		from := work[0]
		work = work[1:]
		for _, fn := range s.funcs[from] {
			writtenNames(fn.Body, spec, func(name string) {
				if _, declared := s.funcs[name]; !declared {
					return
				}
				if _, done := seen[name]; !done {
					seen[name] = from
					work = append(work, name)
				}
			})
		}
	}
	return seen
}

// callChain is the calls that lead from the entry to the function.
func callChain(seen map[string]string, name string) string {
	var path []string
	for ; name != ""; name = seen[name] {
		path = append(path, name)
	}
	slices.Reverse(path)
	return strings.Join(path, " -> ")
}

func TestSpecConversionNeverReadsTheBuiltinTables(t *testing.T) {
	src := parseConverter(t)
	if len(src.outside) > 0 {
		t.Errorf("%v name a built-in table outside a function: it is read whatever is converted, so a change of %s calls for the regeneration of specs/ (libraryOnlyTables in cmd/gate/focused.go)", src.outside, generatedFile)
	}
	// The library conversion reads the tables, so a run that finds none is a test that looks at nothing.
	library := src.reach(libraryEntry, false)
	if !slices.ContainsFunc(slices.Collect(maps.Keys(library)), func(n string) bool { return src.tableUsers[n] }) {
		t.Fatalf("%s reaches no function that names %v: this test no longer finds the tables' readers", libraryEntry, builtinTables)
	}
	spec := src.reach(specEntry, true)
	for _, name := range slices.Sorted(maps.Keys(spec)) {
		if src.tableUsers[name] {
			t.Errorf("the conversion of a spec reaches %s, which names a built-in table (%s): a change of %s now calls for the regeneration of specs/, so take libraryOnlyTables out of conversionChange and interpretChange in cmd/gate/focused.go", name, callChain(spec, name), generatedFile)
		}
	}
}
