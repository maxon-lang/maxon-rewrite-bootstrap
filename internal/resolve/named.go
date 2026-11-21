package resolve

import (
	"minimaxon/internal/ast"
	"slices"
)

// NamedModules are the other modules whose declaration tables resolving the module may read, ascending and without
// duplicates: every module that a qualified name the module writes passes through, as far as the names of child modules
// lead. It reads only the module's syntax and the modules' names from discovery, so it is known once the module is
// parsed, before any other module is, and resolve waits for the tables of these modules alone. A name the resolver
// finds bound to a local, or a path that turns into a declaration before its end, makes the list longer than what is
// read, never shorter.
func NamedModules(p *ast.Project, m *ast.Module) []ast.ModuleID {
	var named []ast.ModuleID
	note := func(mod *ast.Module) {
		if mod != m {
			named = append(named, mod.ID)
		}
	}
	// chains are the members that a path of modules has reached so far, by node, ascending: the parser makes a member
	// after its object, so the member that goes on from one comes later in the tree.
	type link struct {
		member ast.NodeID
		module *ast.Module
	}
	var chains []link
	t := m.Tree
	for i := 1; i <= t.Count(); i++ {
		id := ast.NodeID(i)
		switch t.Records[id].Kind {
		case ast.KindTypeRef:
			tr := ast.TypeRef{T: t, ID: id}
			q := tr.Qualifier()
			if q.Len() == 0 {
				continue
			}
			mod := moduleRoot(p, q.At(0))
			for at := 1; mod != nil; at++ {
				note(mod)
				if at > q.Len() {
					break
				}
				next := tr.Name()
				if at < q.Len() {
					next = q.At(at)
				}
				mod = mod.Children[next]
			}
		case ast.KindMemberExpr:
			member := ast.MemberExpr{T: t, ID: id}
			obj := member.Obj()
			var from *ast.Module
			if head, ok := obj.AsNameExpr(); ok {
				from = moduleRoot(p, head.Name())
			} else if inner, ok := obj.AsMemberExpr(); ok {
				if at, found := slices.BinarySearchFunc(chains, inner.ID, func(l link, n ast.NodeID) int { return int(l.member) - int(n) }); found {
					from = chains[at].module
				}
			}
			if from == nil {
				continue
			}
			note(from)
			if child := from.Children[member.Name()]; child != nil {
				note(child)
				chains = append(chains, link{id, child})
			}
		}
	}
	slices.Sort(named)
	return slices.Compact(named)
}
