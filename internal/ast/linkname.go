package ast

import (
	"fmt"
	"math/big"
	"strings"
)

// A link name is the text that a code unit names a function or a type by. It depends on nothing but the program's own
// declarations: no memory address, and no number that counts the project's modules, so a unit that was made in one
// compile can be linked in another, as long as what the names stand for is the same. A name starts with the module that
// declares what it names, `export` for the project root (a directory cannot have that name) and `stdlib` for the standard
// library; two modules may each declare a `helper` or a `Counter`, and a generic instance that is applied to one of them
// is not the one applied to the other.
//
//	function      geometry::area             export::main
//	method        export::Counter.inc        (export::Box with stdlib::Count).make
//	instance      export::swap[stdlib::Count, export::Counter]
//	type          export::Counter            export::Box with (geometry::Shape, Count=int(0 to 18446744073709551615))
const (
	rootLinkPath   = "export"
	stdlibLinkPath = "stdlib"
	// runtimeLinkPath names the runtime module wherever it is compiled, so that its functions have the symbols the
	// compiler calls them by (ir.RtFunction).
	runtimeLinkPath = "runtime"
	linkSeparator   = "::"

	intLinkAliasMark = "="
	instanceWith     = " with "
)

// LinkPath is how a link name names the module that declares something.
func (m *Module) LinkPath() string {
	switch {
	case m.IsStdlib:
		return stdlibLinkPath
	case m.IsRuntime:
		return runtimeLinkPath
	case m.Path == "":
		return rootLinkPath
	}
	return m.Path
}

// nominalLinkName is the link name of a type declaration.
func nominalLinkName(d *DeclBase) string { return d.Module.LinkPath() + linkSeparator + d.Name }

func linkNames(args []Ty) []string {
	names := make([]string, len(args))
	for i, a := range args {
		names[i] = a.LinkName()
	}
	return names
}

// SetLinkName gives the function the name that its code goes by in a unit and the link. It needs the owner, and for an
// instance the type arguments, so it runs when either is made.
func (f *FuncDecl) SetLinkName() {
	switch {
	case f.Owner != nil:
		owner := f.Owner.LinkName()
		if len(f.Owner.Base().Args) > 0 {
			owner = "(" + owner + ")"
		}
		f.LinkName = owner + "." + f.Name
	case f.Generic != nil:
		f.LinkName = f.Module.LinkPath() + linkSeparator + f.Name + "[" + strings.Join(linkNames(f.TypeArgs), ", ") + "]"
	default:
		f.LinkName = f.Module.LinkPath() + linkSeparator + f.Name
	}
}

// intSpelling is how an integer type's range is written.
func intSpelling(t *IntTy) string { return fmt.Sprintf("int(%s to %s)", t.Min, t.Max) }

// ParseLinkType is the type that a link name names, in a project whose declarations are all made. It is what lets code that
// was generated in an earlier compile ask for the routines of its types without having checked them again. An instance is
// the same type as every other module's instance of the declaration with those arguments; an integer type is a new one,
// since nothing but its range and name is kept of it, and that is all that code generated for it depends on.
func ParseLinkType(p *Project, name string) (Ty, error) {
	r := linkReader{p: p, text: name}
	t, err := r.ty()
	if err == nil && r.at != len(r.text) {
		err = r.fail("text after the type")
	}
	return t, err
}

type linkReader struct {
	p    *Project
	text string
	at   int
}

func (r *linkReader) fail(what string) error {
	return fmt.Errorf("link name %q: %s at %d", r.text, what, r.at)
}

func (r *linkReader) word() string {
	start := r.at
	for r.at < len(r.text) && !strings.ContainsRune(" ,()=:", rune(r.text[r.at])) {
		r.at++
	}
	return r.text[start:r.at]
}

func (r *linkReader) accept(s string) bool {
	if strings.HasPrefix(r.text[r.at:], s) {
		r.at += len(s)
		return true
	}
	return false
}

func (r *linkReader) ty() (Ty, error) {
	first := r.word()
	switch {
	case r.accept(linkSeparator):
		return r.nominal(first)
	case r.accept(intLinkAliasMark):
		return r.integer(first)
	case first == "int" && strings.HasPrefix(r.text[r.at:], "("):
		r.at -= len("int")
		return r.integer("")
	}
	for _, prim := range []*PrimTy{BoolTy, StringTy, VoidTy, NeverTy} {
		if prim.name == first {
			return prim, nil
		}
	}
	return nil, r.fail("not a type")
}

func (r *linkReader) integer(alias string) (Ty, error) {
	if !r.accept("int(") {
		return nil, r.fail("expected int(")
	}
	lo, err := r.number()
	if err != nil || !r.accept(" to ") {
		return nil, r.fail("expected the range of an integer type")
	}
	hi, err := r.number()
	if err != nil || !r.accept(")") {
		return nil, r.fail("expected the end of the range")
	}
	if alias != "" {
		if d, ok := r.p.Stdlib.Names[alias].(*AliasDecl); ok {
			if t, ok := d.Ty.(*IntTy); ok && t.Min == lo && t.Max == hi {
				return t, nil
			}
		}
	}
	return &IntTy{Alias: alias, Min: lo, Max: hi}, nil
}

func (r *linkReader) number() (I128, error) {
	start := r.at
	for r.at < len(r.text) && (r.text[r.at] == '-' || r.text[r.at] >= '0' && r.text[r.at] <= '9') {
		r.at++
	}
	n, ok := new(big.Int).SetString(r.text[start:r.at], 10)
	if !ok {
		return Zero, r.fail("expected a number")
	}
	v, ok := FromBig(n)
	if !ok {
		return Zero, r.fail("a number that does not fit")
	}
	return v, nil
}

func (r *linkReader) nominal(path string) (Ty, error) {
	m := r.p.moduleOfLinkPath(path)
	name := r.word()
	if m == nil || name == "" {
		return nil, r.fail("no such module or type")
	}
	decl, ok := m.Names[name].(TypeDeclNode)
	if !ok {
		return nil, r.fail("no such type")
	}
	t, ok := decl.TypeBase().Ty.(NominalTy)
	if !ok {
		return nil, r.fail("not a type with a body")
	}
	if !r.accept(instanceWith) {
		return t, nil
	}
	var args []Ty
	if r.accept("(") {
		for {
			arg, err := r.ty()
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
			if r.accept(")") {
				break
			}
			if !r.accept(", ") {
				return nil, r.fail("expected , or )")
			}
		}
	} else {
		arg, err := r.ty()
		if err != nil {
			return nil, err
		}
		args = []Ty{arg}
	}
	if !t.Base().IsGeneric() || len(args) != len(t.Base().Params) {
		return nil, r.fail("the wrong number of type arguments")
	}
	return Instantiate(t, args), nil
}

// moduleOfLinkPath is the module a link name's first part names, or nil.
func (p *Project) moduleOfLinkPath(path string) *Module {
	switch path {
	case rootLinkPath:
		return p.Root
	case stdlibLinkPath:
		return p.Stdlib
	}
	m := p.Root
	for part := range strings.SplitSeq(path, ".") {
		m = m.Children[part]
		if m == nil {
			return nil
		}
	}
	return m
}
