package project

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
)

// graphOf builds a project of modules named m0, m1, ... whose results depend as edges[i] says. The site of an edge
// from i to j is at line i*100+j, so a diagnostic says which edge it was reported at.
func graphOf(edges [][]int) (*ast.Project, *Graph) {
	root := ast.NewModule("", ".", false)
	proj := ast.NewProject(root)
	results := make([]ast.ModuleResult, len(edges))
	// Every line is empty, so line n starts at offset n-1.
	text := strings.Repeat("\n", 100*len(edges))
	lines := ast.NewPositions(text, []ast.FileSpan{{File: "f.maxon", End: len(text)}})
	for i := range edges {
		m := root
		if i > 0 {
			m = ast.NewModule("m"+string(rune('0'+i)), ".", false)
		}
		m.ID = ast.ModuleID(i)
		proj.Modules = append(proj.Modules, m)
		for _, j := range edges[i] {
			site := &ast.Pos{Src: lines, Off: uint32(i*100 + j - 1)}
			results[i].Deps = append(results[i].Deps, ast.ModuleDep{Module: ast.ModuleID(j), Site: site})
		}
		slices.SortFunc(results[i].Deps, func(a, b ast.ModuleDep) int { return int(a.Module) - int(b.Module) })
	}
	return proj, NewGraph(results)
}

func TestDependentsAreTheReverseEdgesInAscendingOrder(t *testing.T) {
	_, g := graphOf([][]int{{1, 2, 3}, {3}, {1, 3}, {}})
	want := [][]ast.ModuleID{{}, {0, 2}, {0}, {0, 1, 2}}
	for i, w := range want {
		if !slices.Equal(g.Dependents[i], w) {
			t.Errorf("dependents of %d: got %v, want %v", i, g.Dependents[i], w)
		}
	}
}

func TestOrderPutsEveryModuleAfterItsDependencies(t *testing.T) {
	_, g := graphOf([][]int{{1, 2, 3}, {3}, {1, 3}, {}, {0}})
	order := g.order()
	if len(order) != 5 {
		t.Fatalf("order %v leaves modules out of an acyclic graph", order)
	}
	at := map[ast.ModuleID]int{}
	for i, m := range order {
		at[m] = i
	}
	for m, deps := range g.Deps {
		for _, d := range deps {
			if at[d.Module] > at[ast.ModuleID(m)] {
				t.Errorf("module %d is ordered before its dependency %d: %v", m, d.Module, order)
			}
		}
	}
	if again := g.order(); !slices.Equal(again, order) {
		t.Errorf("the order changed between runs: %v then %v", order, again)
	}
}

func TestAnAcyclicGraphHasNoCycleError(t *testing.T) {
	proj, g := graphOf([][]int{{1, 2}, {2}, {}})
	if err := g.CheckAcyclic(proj); err != nil {
		t.Fatalf("got %v", err)
	}
}

func requireCycle(t *testing.T, edges [][]int, message string, line int) {
	t.Helper()
	proj, g := graphOf(edges)
	err := g.CheckAcyclic(proj)
	if err == nil {
		t.Fatalf("no cycle reported for %v", edges)
	}
	if err.Code != "E9001" || err.Message != message || err.Line != line {
		t.Errorf("got %v at line %d, want E9001 %q at line %d", err, err.Line, message, line)
	}
}

func TestACycleThatDoesNotIncludeTheRootIsReportedWithoutTheRoot(t *testing.T) {
	// The root depends on m1, which is in the cycle m1 -> m2 -> m1.
	requireCycle(t, [][]int{{1}, {2}, {1}},
		"modules depend on each other in a cycle: m1 -> m2 -> m1", 1*100+2)
}

func TestACycleOfThreeNamesEveryModuleInIt(t *testing.T) {
	requireCycle(t, [][]int{{}, {2}, {3}, {1}},
		"modules depend on each other in a cycle: m1 -> m2 -> m3 -> m1", 1*100+2)
}

func TestTheCycleFollowsTheFirstDependencyThatIsLeftOut(t *testing.T) {
	// m1 depends on m0, which is done, then on m3, which is not; m2 depends on m1 and on m3, so the walk from m1 goes
	// m1 -> m3 -> m2 -> m1, and m4 that depends on the cycle is not part of it.
	requireCycle(t, [][]int{{}, {0, 3}, {1, 3}, {2}, {1}},
		"modules depend on each other in a cycle: m1 -> m3 -> m2 -> m1", 1*100+3)
}

func TestTheWalkFromTheFirstModuleTakesItsSmallestDependency(t *testing.T) {
	// The root only depends on cycles; m1 comes before m3, so the cycle found is m1's and m3 is left alone.
	requireCycle(t, [][]int{{3, 1}, {2}, {1}, {1}},
		"modules depend on each other in a cycle: m1 -> m2 -> m1", 1*100+2)
}

func TestACycleThroughTheRootNamesTheProjectRoot(t *testing.T) {
	requireCycle(t, [][]int{{1}, {0}},
		"modules depend on each other in a cycle: the project root -> m1 -> the project root", 0*100+1)
}

// A module that names itself is a cycle of one.
func TestAModuleThatDependsOnItselfIsACycle(t *testing.T) {
	requireCycle(t, [][]int{{}, {1}},
		"modules depend on each other in a cycle: m1 -> m1", 1*100+1)
}

func TestHandlesAreInPathOrder(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"a", "a/x", "a/x/y", "a2", "a_b", "B", "b"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, "m.maxon"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var errs []*diag.CompileError
	proj := Discover(root, &errs)
	paths := make([]string, len(proj.Modules))
	for i, m := range proj.Modules {
		paths[i] = m.Path
		if m.ID != ast.ModuleID(i) {
			t.Errorf("module %q has handle %d, want %d", m.Path, m.ID, i)
		}
	}
	if !sort.StringsAreSorted(paths) {
		t.Errorf("modules are not in path order: %q", paths)
	}
}
