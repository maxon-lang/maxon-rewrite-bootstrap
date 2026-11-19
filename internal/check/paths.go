package check

import (
	"strings"

	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
)

// pathStep is one selection from a value: a field, an element of a primitive, or the contents of a frozen value. Two
// fields overlap when they are the same field. Every element overlaps every other element of the same collection,
// whatever their indexes say, because two indexes cannot be told apart before the program runs. The contents of a
// frozen value are one place, and an immutable one.
type pathStep struct {
	field *ast.FieldInfo
	// index is the expression between the brackets, kept to name the element in a message; it is nil for a field.
	index ast.Expr
	// frozen marks the step from a frozen value to what it holds.
	frozen bool
}

func (a pathStep) isElement() bool { return a.field == nil && !a.frozen }

func (a pathStep) overlaps(b pathStep) bool {
	if a.isElement() != b.isElement() || a.frozen != b.frozen {
		panic(diag.Internal("paths: a field, an element and frozen contents select from the same value"))
	}
	return a.isElement() || a.frozen || a.field == b.field
}

// withContents appends the steps from a value of type t to what it lends: one for each frozen value that t nests.
func withContents(steps []pathStep, t ast.Ty) []pathStep {
	for ast.IsFrozen(t) {
		steps = append(steps, pathStep{frozen: true})
		t = t.(*ast.BuiltinTy).Held()
	}
	return steps
}

// crossesFrozen is whether a place lies inside a frozen value, whose contents nothing may change or take.
func (p accessPath) crossesFrozen() bool {
	for _, st := range p.steps {
		if st.frozen {
			return true
		}
	}
	return false
}

// crossesElement is whether a place is, or lies inside, an element of a primitive, which only the primitive owns.
func (p accessPath) crossesElement() bool {
	for _, st := range p.steps {
		if st.isElement() {
			return true
		}
	}
	return false
}

// accessPath names a place: a local or parameter followed by selections from it.
type accessPath struct {
	root  *ast.Local
	steps []pathStep
}

// overlaps is whether two accesses can reach the same storage: one path is a prefix of the other.
func (p accessPath) overlaps(q accessPath) bool {
	if p.root != q.root {
		return false
	}
	for i := range min(len(p.steps), len(q.steps)) {
		if !p.steps[i].overlaps(q.steps[i]) {
			return false
		}
	}
	return true
}

func (p accessPath) text() string {
	var b strings.Builder
	b.WriteString(p.root.Name)
	for _, st := range p.steps {
		switch {
		case st.frozen:
			continue
		case st.isElement():
			b.WriteString("[" + placeText(st.index) + "]")
			continue
		}
		b.WriteByte('.')
		b.WriteString(st.field.Name)
	}
	return b.String()
}

// unnamedPlace stands for an expression that is too long to repeat in a message.
const unnamedPlace = ".."

// placeText is how an index or a collection is written in a message: a name, a literal or a selection from one.
func placeText(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.IntLit:
		return e.Value().String()
	case *ast.NameExpr:
		return e.Name
	case *ast.SelfExpr:
		return "self"
	case *ast.MemberExpr:
		return placeText(e.Obj) + "." + e.Name
	}
	return unnamedPlace
}

// pathOf is the place an expression names, with its steps appended to buf, or false for an expression that
// computes a fresh value. self is the receiver of the function being checked, which a bare field name selects from.
func pathOf(e ast.Expr, self *ast.Local, buf []pathStep) (accessPath, bool) {
	switch e := e.(type) {
	case *ast.NameExpr:
		switch e.Kind {
		case ast.NameLocal:
			return accessPath{root: e.Local, steps: buf}, true
		case ast.NameSelfField:
			return accessPath{root: self, steps: append(buf, pathStep{field: e.Field})}, true
		}
	case *ast.SelfExpr:
		return accessPath{root: e.Local, steps: buf}, true
	case *ast.MemberExpr:
		if e.Kind == ast.MemberField {
			p, ok := pathOf(e.Obj, self, buf)
			if !ok {
				return p, false
			}
			p.steps = append(withContents(p.steps, e.Obj.Ty()), pathStep{field: e.Field})
			return p, true
		}
	case *ast.IndexExpr:
		p, ok := pathOf(e.Obj, self, buf)
		if !ok {
			return p, false
		}
		p.steps = append(withContents(p.steps, e.Obj.Ty()), pathStep{index: e.Args[0]})
		return p, true
	}
	return accessPath{}, false
}

// projectsElement is whether a place is, or lies inside, an element of a primitive, which only the primitive owns.
func projectsElement(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.IndexExpr:
		return true
	case *ast.MemberExpr:
		return e.Kind == ast.MemberField && projectsElement(e.Obj)
	}
	return false
}
