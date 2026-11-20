package project

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"sort"
	"strings"
)

// Graph is the module graph: an edge from a module to each module it names a declaration of. Both directions are kept,
// by handle, so that a module can be started when everything it depends on is done and its dependents are told when
// it is. It holds nothing but handles and the site of each edge, so it is the same for any --jobs and a cached module
// can contribute its edges without its syntax tree.
type Graph struct {
	// Deps are the edges out of each module, indexed by ModuleID: ascending by module, without duplicates.
	Deps [][]ast.ModuleDep
	// Dependents are the edges into each module, indexed by ModuleID, ascending.
	Dependents [][]ast.ModuleID
}

// NewGraph builds the graph from the dependencies each module's resolve recorded in its result.
func NewGraph(results []ast.ModuleResult) *Graph {
	g := &Graph{Deps: make([][]ast.ModuleDep, len(results)), Dependents: make([][]ast.ModuleID, len(results))}
	counts := make([]int, len(results))
	edges := 0
	for i := range results {
		g.Deps[i] = results[i].Deps
		for _, d := range results[i].Deps {
			counts[d.Module]++
		}
		edges += len(results[i].Deps)
	}
	backing := make([]ast.ModuleID, 0, edges)
	for i, n := range counts {
		g.Dependents[i] = backing[len(backing) : len(backing) : len(backing)+n]
		backing = backing[:len(backing)+n]
	}
	for i, deps := range g.Deps {
		for _, d := range deps {
			g.Dependents[d.Module] = append(g.Dependents[d.Module], ast.ModuleID(i))
		}
	}
	return g
}

// CheckAcyclic reports the import cycle (E9001) that the graph holds, or nil if it holds none.
func (g *Graph) CheckAcyclic(p *ast.Project) *diag.CompileError {
	order := g.order()
	if len(order) == len(g.Deps) {
		return nil
	}
	return g.cycleError(p, order)
}

// order is a topological order of the modules that no cycle depends on, so each module comes after everything it
// depends on (Kahn's algorithm). Modules that are ready go first in index order, and a module that becomes ready
// goes after those already waiting, so the order is the same on every run. It is shorter than the graph exactly
// when there is a cycle.
func (g *Graph) order() []ast.ModuleID {
	waiting := make([]int, len(g.Deps))
	order := make([]ast.ModuleID, 0, len(g.Deps))
	for i, deps := range g.Deps {
		waiting[i] = len(deps)
		if len(deps) == 0 {
			order = append(order, ast.ModuleID(i))
		}
	}
	for next := 0; next < len(order); next++ {
		for _, dependent := range g.Dependents[order[next]] {
			waiting[dependent]--
			if waiting[dependent] == 0 {
				order = append(order, dependent)
			}
		}
	}
	return order
}

// cycleError names one cycle among the modules that order left out: those in a cycle and those that depend on one.
// It starts at the first of them and follows the first dependency that is also left out until a module comes round
// again. Module handles are in path order, so this is the dependency with the smallest path.
func (g *Graph) cycleError(p *ast.Project, order []ast.ModuleID) *diag.CompileError {
	done := make([]bool, len(g.Deps))
	for _, m := range order {
		done[m] = true
	}
	start := ast.ModuleID(0)
	for done[start] {
		start++
	}
	path := []ast.ModuleID{start}
	for {
		var next ast.ModuleID
		found := false
		for _, d := range g.Deps[path[len(path)-1]] {
			if !done[d.Module] {
				next, found = d.Module, true
				break
			}
		}
		if !found {
			panic(diag.Internal("module %s is left out of the dependency order but depends only on modules in it", p.Modules[path[len(path)-1]].Display()))
		}
		for at, m := range path {
			if m == next {
				cycle := append(append([]ast.ModuleID{}, path[at:]...), next)
				names := make([]string, len(cycle))
				for i, c := range cycle {
					names[i] = p.Modules[c].Display()
				}
				return g.site(cycle[0], cycle[1]).Error("E9001", "modules depend on each other in a cycle: "+strings.Join(names, " -> "))
			}
		}
		path = append(path, next)
	}
}

// site is where module from names module to.
func (g *Graph) site(from, to ast.ModuleID) ast.Pos {
	deps := g.Deps[from]
	at := sort.Search(len(deps), func(i int) bool { return deps[i].Module >= to })
	if at == len(deps) || deps[at].Module != to {
		panic(diag.Internal("module %d does not depend on module %d", from, to))
	}
	return deps[at].Site
}
