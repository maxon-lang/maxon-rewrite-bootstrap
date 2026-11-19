package check

import (
	"fmt"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"slices"
	"strings"
)

// capability is what a type has unless something in it removes it. A capability is structural: a type never says it has
// one, it says what removes it (`is noncopyable`, a destructor), and a type that holds a part without the capability
// lacks it too. A type parameter has a capability only by bound.
type capability int

const (
	copyable capability = iota
	droppable
)

// capabilityOf is the capability a built-in interface names.
func (c *Checker) capabilityOf(iface *ast.InterfaceTy) (capability, bool) {
	switch iface {
	case c.project.Builtins.Copyable:
		return copyable, true
	case c.project.Builtins.Droppable:
		return droppable, true
	}
	return 0, false
}

// lackReason is why a type without a capability lacks it, when it is not because of a part.
type lackReason int

const (
	// lackDeclared is a type whose own declaration opts out: `is noncopyable`.
	lackDeclared lackReason = iota
	// lackDestructor is a type that declares a destructor, which ends a value in a way a copy or a drop cannot.
	lackDestructor
	// lackUnbounded is a type parameter that no bound gives the capability.
	lackUnbounded
)

// lack is what removes a capability from a type: either the type itself (a declaration that opts out, a parameter
// without the bound), or a part of it, whose own type lacks the capability in turn. It is a chain from the type that was
// asked about down to the root cause, so a message can name each type on the way and the one that has to change.
type lack struct {
	// owner is the type that declares the part, or the type that lacks the capability by itself.
	owner ast.Ty
	// ty is the type of the part, and where says where in owner it is, as 'in its field 'sock''; both are empty at the
	// root of the chain.
	ty    ast.Ty
	where string
	// cause is why the part's type lacks the capability; reason is why the root does.
	cause  *lack
	reason lackReason
}

// root is the end of the chain: the type that has to change for the capability to come back.
func (l *lack) root() *lack {
	for l.cause != nil {
		l = l.cause
	}
	return l
}

// text says why the type lacks the capability, from the type that was asked about down to the root cause. name is how
// the capability is spelled in source.
func (l *lack) text(name string) string {
	var sentences []string
	for ; l.cause != nil; l = l.cause {
		sentences = append(sentences, fmt.Sprintf("'%s' holds '%s' %s", l.owner.Name(), l.ty.Name(), l.where))
	}
	owner := l.owner.Name()
	switch l.reason {
	case lackDeclared:
		sentences = append(sentences, fmt.Sprintf("'%s' is declared 'is noncopyable'", owner))
	case lackDestructor:
		sentences = append(sentences, fmt.Sprintf("'%s' has a destructor", owner))
	case lackUnbounded:
		sentences = append(sentences, fmt.Sprintf("the type parameter '%s' is not known to be %s", owner, name))
	default:
		panic(diag.Internal("unknown reason %d for a missing capability", int(l.reason)))
	}
	return strings.Join(sentences, "; ")
}

// fix says how to give the type the capability back, from the root cause. name is how the capability is spelled in
// source. What else the site could do instead (move the value, end it) depends on the site, which adds it.
func (l *lack) fix(name string) string {
	root := l.root()
	switch root.reason {
	case lackDeclared:
		return fmt.Sprintf("remove 'is noncopyable' from '%s' if its values can be copied", root.owner.Name())
	case lackDestructor:
		return fmt.Sprintf("a '%s' is ended only by calling one of its destructors; remove them if its values may be copied and dropped", root.owner.Name())
	case lackUnbounded:
		return fmt.Sprintf("add the bound 'where %s implements %s'", root.owner.Name(), name)
	}
	panic(diag.Internal("unknown reason %d for a missing capability", int(root.reason)))
}

type capKey struct {
	capability capability
	ty         ast.Ty
	// bounded is the function whose own bounds on its type's parameters the answer relies on, so two functions never
	// share an answer about a type that mentions a parameter they bound differently.
	bounded *ast.FuncDecl
}

// capabilities remembers what the queries found, for one checker: modules check in parallel and each has its own, so
// nothing is shared.
type capabilities struct {
	// known holds the answer for each type asked about: nil where the type has the capability.
	known map[capKey]*lack
	// visiting are the types being judged now. A type that holds itself, through an array or an option, adds no part
	// that lacks the capability, so reaching one of them again counts as having it.
	visiting map[capKey]bool
	// assumed are the types judged to have the capability while a type they reach was still being judged. They hold
	// only if that one does, so they are remembered once the outermost judgement ends well, and forgotten otherwise.
	assumed []capKey
}

// lacks is the part that removes the capability from t, or nil when t has it. A type has a capability unless its
// declaration removes it or a part of it lacks it; the first part in declaration order is the one reported, so the
// answer does not depend on how modules are scheduled.
func (c *Checker) lacks(k capability, t ast.Ty) *lack {
	switch x := t.(type) {
	case *ast.IntTy, *ast.ConstIntTy, *ast.PrimTy, *ast.EnumTy:
		return nil
	case *ast.TypeParamTy:
		if c.hasBound(x, k) {
			return nil
		}
		return &lack{owner: x, reason: lackUnbounded}
	case *ast.RecordTy, *ast.UnionTy, *ast.BuiltinTy:
		return c.remembered(k, x)
	}
	panic(diag.Internal("%s is not a type that a value has", t.Name()))
}

// hasBound is whether a type parameter is bounded by the capability. Copyable includes Droppable: whatever can be
// copied can be dropped.
func (c *Checker) hasBound(p *ast.TypeParamTy, k capability) bool {
	b := c.project.Builtins
	for _, iface := range c.boundsOf(p) {
		if iface == b.Copyable || (k == droppable && iface == b.Droppable) {
			return true
		}
	}
	return false
}

// remembered is the answer of lacks for a nominal type, judged once.
func (c *Checker) remembered(k capability, t ast.Ty) *lack {
	key := capKey{k, t, c.bounded}
	if l, done := c.caps.known[key]; done {
		return l
	}
	if c.caps.visiting[key] {
		return nil
	}
	if c.caps.known == nil {
		c.caps.known = map[capKey]*lack{}
		c.caps.visiting = map[capKey]bool{}
	}
	c.caps.visiting[key] = true
	l := c.judge(k, t)
	delete(c.caps.visiting, key)
	outermost := len(c.caps.visiting) == 0
	switch {
	case l != nil:
		c.caps.known[key] = l
	case outermost:
		c.caps.known[key] = nil
		for _, a := range c.caps.assumed {
			c.caps.known[a] = nil
		}
	default:
		c.caps.assumed = append(c.caps.assumed, key)
	}
	if outermost {
		c.caps.assumed = c.caps.assumed[:0]
	}
	return l
}

// judge looks at a nominal type's declaration and then at its parts, in declaration order.
func (c *Checker) judge(k capability, t ast.Ty) *lack {
	switch x := t.(type) {
	case *ast.RecordTy:
		if reason, removed := removedBy(k, hasDestructors(x), x.Decl.Noncopyable); removed {
			return &lack{owner: x, reason: reason}
		}
		for _, f := range x.FieldList() {
			ft := x.FieldType(f)
			if cause := c.lacks(k, ft); cause != nil {
				return &lack{owner: x, ty: ft, where: fmt.Sprintf("in its field '%s'", f.Name), cause: cause}
			}
		}
		return nil
	case *ast.UnionTy:
		if reason, removed := removedBy(k, hasDestructors(x), x.Decl.Noncopyable); removed {
			return &lack{owner: x, reason: reason}
		}
		for i, uc := range x.CaseList() {
			for _, p := range x.Payload(i) {
				if cause := c.lacks(k, p.Type); cause != nil {
					return &lack{owner: x, ty: p.Type, where: fmt.Sprintf("in the payload '%s' of its case '%s'", p.Name, uc.Name), cause: cause}
				}
			}
		}
		return nil
	case *ast.BuiltinTy:
		return c.builtinLack(k, x)
	}
	panic(diag.Internal("%s is not a nominal type with parts", t.Name()))
}

// builtinLack is the lack of a primitive: an array, a list and a pool own their elements, a map its values, and a
// primitive that crosses between threads or names an element keeps whatever it holds out of the question.
func (c *Checker) builtinLack(k capability, t *ast.BuiltinTy) *lack {
	var where string
	switch t.Kind {
	case ast.BuiltinHandle, ast.BuiltinFrozen, ast.BuiltinCown:
		return nil
	case ast.BuiltinArray, ast.BuiltinList, ast.BuiltinPool:
		where = "as its elements"
	case ast.BuiltinMap:
		where = "as its values"
	default:
		panic(diag.Internal("%s has no capabilities to judge", t.Name()))
	}
	if cause := c.lacks(k, t.Element()); cause != nil {
		return &lack{owner: t, ty: t.Element(), where: where, cause: cause}
	}
	return nil
}

// removedBy is whether the declaration of a type removes the capability by itself, and why.
func removedBy(k capability, destructors, noncopyable bool) (lackReason, bool) {
	switch {
	case destructors:
		return lackDestructor, true
	case k == copyable && noncopyable:
		return lackDeclared, true
	}
	return 0, false
}

// hasDestructors is whether a type declares a destructor, which only its own destructors may take apart.
func hasDestructors(t ast.Ty) bool {
	return slices.ContainsFunc(declaredMethods(t), (*ast.FuncDecl).IsDestructor)
}

// declaredMethods are the methods a type's declaration lists, in declaration order: none for a type that has no
// declaration of its own.
func declaredMethods(t ast.Ty) []*ast.FuncDecl {
	switch x := t.(type) {
	case *ast.RecordTy:
		return x.Decl.Methods
	case *ast.UnionTy:
		return x.Decl.Methods
	}
	return nil
}

// whyNot is what removes the capability from t and how to get it back, for a diagnostic; ok is false when t has it.
func (c *Checker) whyNot(k capability, t ast.Ty) (why, fix string, ok bool) {
	l := c.lacks(k, t)
	if l == nil {
		return "", "", false
	}
	name := c.capabilityInterface(k).Name()
	return l.text(name), l.fix(name), true
}

func (c *Checker) capabilityInterface(k capability) *ast.InterfaceTy {
	if k == copyable {
		return c.project.Builtins.Copyable
	}
	return c.project.Builtins.Droppable
}

// copyAdvice is the fix for a message that would have the reader copy a value of type t. ifCopyable is the copy, said as
// a whole clause, and otherwise is what to do instead, which a type that cannot be copied is told in its place; it
// may be empty, and the advice is then to move the whole value.
func (c *Checker) copyAdvice(t ast.Ty, ifCopyable, otherwise string) string {
	why, _, lacking := c.whyNot(copyable, t)
	if !lacking {
		if otherwise == "" {
			return ifCopyable
		}
		return ifCopyable + ", or " + otherwise
	}
	if otherwise == "" {
		otherwise = "move the whole value instead of copying it"
	}
	return fmt.Sprintf("'%s' cannot be copied (%s), so %s", t.Name(), why, otherwise)
}

// structuralCapability is the error for a declaration that says it implements Copyable or Droppable: it has them
// already, and the way to say otherwise is to opt out or to hold a part that lacks them.
func (c *Checker) structuralCapability(site ast.Node, iface *ast.InterfaceTy) *diag.CompileError {
	return site.P().Error(diag.ErrCapabilityMisuse, fmt.Sprintf("'%s' is not implemented by saying so: every type has it unless it opts out or holds a part that lacks it; remove it from 'implements'%s",
		iface.Name(), optOutAdvice(iface, c.project.Builtins)))
}

func optOutAdvice(iface *ast.InterfaceTy, b *ast.Builtins) string {
	if iface == b.Copyable {
		return ", and write 'is noncopyable' after the name to opt out"
	}
	return ""
}
