package check

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"slices"
	"strings"
)

// ownMove is a field that moved out of a local: the fields to select from the local to reach it, its type, and where it
// went.
type ownMove struct {
	fields []*ast.FieldInfo
	ty     ast.Ty
	site   ast.Node
	dest   ownDest
}

// ownParts are the fields gone from a partly moved local, none inside another. States share one, so it is never changed
// once built.
type ownParts struct{ moves []ownMove }

// covers is whether the move takes the place named by fields, or a place that contains it.
func (m ownMove) covers(fields []*ast.FieldInfo) bool {
	return len(m.fields) <= len(fields) && slices.Equal(m.fields, fields[:len(m.fields)])
}

// overlapsPath is whether the move and the place share storage: one contains the other.
func (m ownMove) overlapsPath(p accessPath) bool {
	for i := range min(len(m.fields), len(p.steps)) {
		if p.steps[i].field != m.fields[i] {
			return false
		}
	}
	return true
}

func (m ownMove) text(root *ast.Local) string {
	var b strings.Builder
	b.WriteString(root.Name)
	for _, f := range m.fields {
		b.WriteByte('.')
		b.WriteString(f.Name)
	}
	return b.String()
}

func coveredBy(moves []ownMove, m ownMove) bool {
	return slices.ContainsFunc(moves, func(other ownMove) bool { return other.covers(m.fields) })
}

// fieldsOf is the fields a place selects, which must all be fields: a move never goes through an element or frozen
// contents.
func fieldsOf(p accessPath) []*ast.FieldInfo {
	fields := make([]*ast.FieldInfo, len(p.steps))
	for i, st := range p.steps {
		if st.field == nil {
			panic(diag.Internal("ownership: a move through an element or frozen contents reached the field moves"))
		}
		fields[i] = st.field
	}
	return fields
}

// mergeFacts is what two paths say about one local: gone as a whole on either path, it is gone; partly moved on both, it
// lacks what either path lacks. It returns a when a says as much already, so a state that changes nothing stays the same
// one.
func mergeFacts(a, b ownFact) ownFact {
	switch {
	case a.parts == nil:
		return a
	case b.parts == nil:
		return b
	}
	var extra []ownMove
	for _, m := range b.parts.moves {
		if !coveredBy(a.parts.moves, m) {
			extra = append(extra, m)
		}
	}
	if len(extra) == 0 {
		return a
	}
	moves := make([]ownMove, 0, len(a.parts.moves)+len(extra))
	for _, m := range a.parts.moves {
		if !coveredBy(extra, m) {
			moves = append(moves, m)
		}
	}
	return ownFact{site: a.site, kind: factPartlyMoved, parts: &ownParts{append(moves, extra...)}}
}

// requireMayTakeApart is E3139: a move of a part, or a 'match consumes', takes t apart, which a type with destructors only
// allows inside its own destructors, so that no value of it ends without one.
func (o *Ownership) requireMayTakeApart(site ast.Node, t ast.Ty, what string) bool {
	if !hasDestructors(t) || (o.fn.IsDestructor() && o.fn.SelfLocal.Type == t) {
		return true
	}
	o.fail(site, diag.ErrTakeApartDestructible,
		"%s takes '%s' apart, which ends it without calling a destructor, and only its own destructors may do that; end it by calling one of them, or read the part without moving it",
		what, t.Name())
	return false
}

// requireMayTakeApartFrom checks each record along a field path that a move takes a part from.
func (o *Ownership) requireMayTakeApartFrom(e ast.Expr, what string) bool {
	switch x := e.(type) {
	case *ast.MemberExpr:
		return o.requireMayTakeApartFrom(x.Obj, what) && o.requireMayTakeApart(e, x.Obj.Ty(), what)
	case *ast.NameExpr:
		if x.Kind == ast.NameSelfField {
			return o.requireMayTakeApart(e, o.fn.SelfLocal.Type, what)
		}
	}
	return true
}

// movePart moves a field out of the local that e's path starts at: the local is then partly moved, which a fact says by
// listing the fields that are gone.
func (o *Ownership) movePart(e ast.Expr, p accessPath, dest ownDest, s *ownState) *ownState {
	o.requireNotProjected(p, e)
	if !o.requireMayTakeApartFrom(e, "moving '"+p.text()+"'") {
		return s
	}
	m := ownMove{fields: fieldsOf(p), ty: e.Ty(), site: e, dest: dest}
	moves := []ownMove{m}
	if fact, ok := s.tryGet(p.root); ok && fact.parts != nil {
		for _, old := range fact.parts.moves {
			if !m.covers(old.fields) {
				moves = append(moves, old)
			}
		}
	}
	o.partials++
	return s.with(p.root, ownFact{site: e, kind: factPartlyMoved, parts: &ownParts{moves}})
}

// readPart is a read of a field path of a local, which needs no part of that place to be gone. It is false when the
// local has lost none of its fields, and the caller then reads the local as a whole.
func (o *Ownership) readPart(e ast.Expr, s *ownState) (*ownState, bool) {
	p, ok := pathOf(e, o.fn.SelfLocal, o.steps[:0])
	if !ok || p.crossesElement() {
		return s, false
	}
	fact, has := s.tryGet(p.root)
	if !has || fact.parts == nil {
		return s, false
	}
	for _, m := range fact.parts.moves {
		if m.overlapsPath(p) {
			o.fail(e, diag.ErrUseAfterMove,
				"'%s' overlaps '%s', which was moved into %s at line %d; assign '%s' again before using it",
				p.text(), m.text(p.root), m.dest, m.site.P().Line, m.text(p.root))
			break
		}
	}
	return s, true
}

// assignPart is an assignment to a field path of a local that has lost some of its fields. Assigning a gone field gives
// it a value again, and the local is whole once it has them all. The old value of a field that is partly gone is dropped
// in its remaining parts. It is false when the local has lost nothing that the assignment touches.
func (o *Ownership) assignPart(st *ast.AssignStmt, target ast.Expr, p accessPath, s *ownState) (*ownState, bool) {
	if o.partials == 0 || s.dead || p.crossesElement() {
		return s, false
	}
	fact, has := s.tryGet(p.root)
	if !has || fact.parts == nil {
		return s, false
	}
	var kept, under []ownMove
	reinitialised := false
	for _, m := range fact.parts.moves {
		switch {
		case !m.overlapsPath(p):
			kept = append(kept, m)
		case len(m.fields) == len(p.steps):
			reinitialised = true
		case len(m.fields) > len(p.steps):
			under = append(under, m)
		default:
			o.fail(st, diag.ErrUseAfterMove,
				"'%s' lies inside '%s', which was moved into %s at line %d; assign '%s' again first",
				p.text(), m.text(p.root), m.dest, m.site.P().Line, m.text(p.root))
			return s, true
		}
	}
	switch {
	case reinitialised:
	case len(under) > 0:
		o.requireRemainder(st, target.Ty(), len(p.steps), under, p.text(), dropOverwrite, false)
	default:
		o.requireOverwriteDroppable(st, target, s)
		return s, true
	}
	if len(kept) == 0 {
		return s.without(p.root), true
	}
	return s.with(p.root, ownFact{site: fact.site, kind: factPartlyMoved, parts: &ownParts{kept}}), true
}

// requireRemainder is the drop of what a value of type ty keeps when the moves have taken their fields out of it: each field
// that is left must be droppable, and a field that is partly gone is judged by what is left of it. moves are the moves
// inside the value, each reaching the field at depth. A value that nothing was taken from is dropped as a whole, except the
// self of a destructor, which ends by dropping its fields, so those are judged one by one.
func (o *Ownership) requireRemainder(site ast.Node, ty ast.Ty, depth int, moves []ownMove, name string, kind ownDropKind, destroys bool) {
	if len(moves) == 0 && !destroys {
		o.requireDroppable(site, ty, ownDrop{kind: kind, name: name})
		return
	}
	switch t := ty.(type) {
	case *ast.RecordTy:
		for _, f := range t.FieldList() {
			var gone bool
			var inside []ownMove
			for _, m := range moves {
				switch {
				case m.fields[depth] != f:
				case len(m.fields) == depth+1:
					gone = true
				default:
					inside = append(inside, m)
				}
			}
			if !gone {
				o.requireRemainder(site, t.FieldType(f), depth+1, inside, name+"."+f.Name, kind, false)
			}
		}
	case *ast.UnionTy:
		for i := range t.CaseList() {
			for _, payload := range t.Payload(i) {
				o.requireDroppable(site, payload.Type, ownDrop{kind: kind, name: name + "." + payload.Name})
			}
		}
	default:
		panic(diag.Internal("ownership: '%s' has no fields to move out of", ty.Name()))
	}
}

// requireHeld is the drop of an obliged local at the place where it is dropped, whatever its fact says it lost.
func (o *Ownership) requireHeld(site ast.Node, ob ownObliged, s *ownState, kind ownDropKind) {
	fact, has := s.tryGet(ob.local)
	switch {
	case has && fact.parts == nil:
	case has:
		o.requireRemainder(site, ob.local.Type, 0, fact.parts.moves, ob.local.Name, kind, ob.destroys)
	default:
		o.requireRemainder(site, ob.local.Type, 0, nil, ob.local.Name, kind, ob.destroys)
	}
}

// lostOnOnePath is E3136 for what the path with the fact lost and the other path still holds: the merged state says it is
// gone, so nothing would drop it where the other path holds it, unless what it holds can be dropped.
func (o *Ownership) lostOnOnePath(ob ownObliged, lost ownFact, hasLost bool, other ownFact, hasOther bool) {
	if !hasLost {
		return
	}
	if lost.parts != nil {
		for _, m := range lost.parts.moves {
			var inside []ownMove
			if hasOther {
				if other.parts == nil || coveredBy(other.parts.moves, m) {
					continue
				}
				for _, om := range other.parts.moves {
					if m.covers(om.fields) {
						inside = append(inside, om)
					}
				}
			}
			o.requireRemainder(m.site, m.ty, len(m.fields), inside, m.text(ob.local), dropOtherPath, false)
		}
		return
	}
	switch {
	case hasOther && other.parts == nil:
	case hasOther:
		o.requireRemainder(lost.site, ob.local.Type, 0, other.parts.moves, ob.local.Name, dropOtherPath, ob.destroys)
	case ob.destroys:
		o.requireRemainder(lost.site, ob.local.Type, 0, nil, ob.local.Name, dropOtherPath, true)
	default:
		o.disagreeWhole(ob, lost)
	}
}
