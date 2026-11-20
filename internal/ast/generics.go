package ast

import (
	"minimaxon/internal/diag"
	"slices"
	"strings"
	"sync"
)

// TypeParamTy is a type parameter as a body sees it. A value of it is owned, movable and not a scalar whatever type
// the parameter is later given, so a body checked against it holds for every instance.
type TypeParamTy struct {
	name string
	// Interfaces are the bounds: the methods a value of the type may call beyond the ones every type has.
	Interfaces []*InterfaceTy
	bounded    bool
}

func newTypeParamTy(name string) *TypeParamTy { return &TypeParamTy{name: name} }

// newBoundedTypeParamTy is the type parameter of a built-in whose signature states a bound, as a declaration does with
// 'where'.
func newBoundedTypeParamTy(name string, bounds ...*InterfaceTy) *TypeParamTy {
	return &TypeParamTy{name: name, Interfaces: bounds, bounded: true}
}

// NewTypeParamOf is the type of a declared type parameter, given the constraints of its declaration. Whether it has
// bounds is known from the syntax, before the bounds themselves are resolved, so a use of the declaration can tell
// whether it has anything to check.
func NewTypeParamOf(p *TypeParam, where []*Constraint) *TypeParamTy {
	bounded := slices.ContainsFunc(where, func(c *Constraint) bool { return c.Param == p.Name })
	return &TypeParamTy{name: p.Name, bounded: bounded}
}

// ParamBound is a bound that a method adds to a parameter of its type. The parameter's own bounds are shared by every
// method and every instance, so this one belongs to the method's signature: a call of the method needs its receiver's
// type argument to meet it, and the body of the method may rely on it.
type ParamBound struct {
	Param *TypeParamTy
	Iface *InterfaceTy
}

// HasOwnBounds is whether the method bounds its type's parameters beyond what the type does. The syntax says so before
// the bounds are resolved, so a call can tell whether it has anything to check while another module still declares them.
func (f *FuncDecl) HasOwnBounds() bool { return len(f.Where) > 0 || len(f.Bounds) > 0 }

// BoundsOn are the interfaces the method adds to the type parameter.
func (f *FuncDecl) BoundsOn(p *TypeParamTy) []*InterfaceTy {
	var out []*InterfaceTy
	for _, b := range f.Bounds {
		if b.Param == p {
			out = append(out, b.Iface)
		}
	}
	return out
}

// Bounded is whether the parameter declares any bound.
func (t *TypeParamTy) Bounded() bool { return t.bounded }

func (t *TypeParamTy) Name() string    { return t.name }
func (t *TypeParamTy) IsManaged() bool { return true }

// InterfaceTy is a set of method signatures. It is a bound on a type parameter, never the type of a value.
type InterfaceTy struct {
	name string
	Decl *InterfaceDecl
	// Self stands for the implementing type in the signatures.
	Self    *TypeParamTy
	Methods map[string]*FuncSig
}

func NewInterfaceTy(d *InterfaceDecl) *InterfaceTy {
	return &InterfaceTy{name: d.Name, Decl: d, Self: newTypeParamTy("Self"), Methods: map[string]*FuncSig{}}
}

func (t *InterfaceTy) Name() string    { return t.name }
func (t *InterfaceTy) IsManaged() bool { return false }

// Passing is how a callee takes what it is passed: the convention of its receiver and its parameters.
type Passing struct {
	SelfConv Convention
	Specs    []ParamSpec
}

// GenericCall is what a call adds when its callee is generic, or when it goes through a bound: the type arguments, and
// the method of the bound. Its Passing has the callee's type parameters replaced.
type GenericCall struct {
	Passing
	Method   *FuncSig
	TypeArgs []Ty
}

// Binding gives type parameters their arguments.
type Binding struct {
	Params []*TypeParamTy
	Args   []Ty
}

// Apply is t with the bound parameters replaced.
func (b Binding) Apply(t Ty) Ty { return subst(t, b.Params, b.Args) }

// applyAll is the list with every type replaced, or the list itself when none changes.
func (b Binding) applyAll(ts []Ty) []Ty {
	out, _ := b.replaceAll(ts)
	return out
}

func (b Binding) replaceAll(ts []Ty) (out []Ty, changed bool) {
	for i, t := range ts {
		r := b.Apply(t)
		if r != t && !changed {
			changed = true
			out = append(make([]Ty, 0, len(ts)), ts[:i]...)
		}
		if changed {
			out = append(out, r)
		}
	}
	if !changed {
		return ts, false
	}
	return out, true
}

// subst replaces the type parameters in t with the matching arguments. A type that mentions none is returned as it
// is, so non-generic code pays a type switch.
func subst(t Ty, params []*TypeParamTy, args []Ty) Ty {
	if len(params) == 0 {
		return t
	}
	b := Binding{Params: params, Args: args}
	switch x := t.(type) {
	case *TypeParamTy:
		for i, p := range params {
			if p == x {
				return args[i]
			}
		}
	case *RecordTy:
		if replaced, changed := b.replaceAll(x.Args); changed {
			return instantiateRecord(x.origin, replaced)
		}
	case *UnionTy:
		if replaced, changed := b.replaceAll(x.Args); changed {
			return instantiateUnion(x.origin, replaced)
		}
	case *BuiltinTy:
		if replaced, changed := b.replaceAll(x.Args); changed {
			return instantiateBuiltin(x.origin, replaced)
		}
	case *IntTy, *ConstIntTy, *PrimTy, *EnumTy, *InterfaceTy:
	default:
		panic(diag.Internal("subst: unknown type %T", t))
	}
	return t
}

// MaxTypeDepth is how deeply instances may nest in one type. A program that needs more is a generic that grows its
// own type argument without end, which has no finite expansion.
const MaxTypeDepth = 16

// TypeDepth is how deeply instances nest in t: a parameter or a plain type is 0, and Box with (Box with Count) is 2. A body
// that calls itself with a growing type argument has no end, which a limit on the depth finds.
func TypeDepth(t Ty) int {
	var args []Ty
	switch x := t.(type) {
	case *RecordTy:
		args = x.Args
	case *UnionTy:
		args = x.Args
	case *BuiltinTy:
		args = x.Args
	case *TypeParamTy, *IntTy, *ConstIntTy, *PrimTy, *EnumTy, *InterfaceTy:
		return 0
	default:
		panic(diag.Internal("depth: unknown type %T", t))
	}
	deepest := 0
	for _, a := range args {
		deepest = max(deepest, TypeDepth(a))
	}
	return deepest + 1
}

func typeNames(args []Ty) []string {
	names := make([]string, len(args))
	for i, a := range args {
		names[i] = a.Name()
	}
	return names
}

// InstanceSpelling is how source writes a generic type given its arguments: 'Box with Count', 'map with (String,
// Count)', and 'Option with (Box with Count)', where an argument that is an instance itself is parenthesized.
func InstanceSpelling(base string, args []string) string {
	if len(args) == 1 && !strings.Contains(args[0], " with ") {
		return base + " with " + args[0]
	}
	return base + " with (" + strings.Join(args, ", ") + ")"
}

// functionInstanceName is the name of an instance of a generic function in the IR and in diagnostics. Source has no
// spelling for it, since a call always infers the type arguments.
func functionInstanceName(base string, args []Ty) string {
	return base + "[" + strings.Join(typeNames(args), ", ") + "]"
}

// InstanceTable interns the instances of one generic declaration, so two modules that write the same type arguments
// get the same type. Modules check in parallel, so it locks.
type InstanceTable struct {
	mu   sync.Mutex
	root instanceNode
}

type instanceNode struct {
	next  map[Ty]*instanceNode
	value any
}

// get is the instance for a list of arguments, made by build the first time it is asked for.
func (t *InstanceTable) get(args []Ty, build func() any) any {
	t.mu.Lock()
	defer t.mu.Unlock()
	node := &t.root
	for _, a := range args {
		child, ok := node.next[a]
		if !ok {
			if node.next == nil {
				node.next = map[Ty]*instanceNode{}
			}
			child = &instanceNode{}
			node.next[a] = child
		}
		node = child
	}
	if node.value == nil {
		node.value = build()
	}
	return node.value
}

// Instantiate is the type a generic declaration names when it is given type arguments. The same arguments always give
// the same type.
func Instantiate(declaration NominalTy, args []Ty) NominalTy {
	switch d := declaration.(type) {
	case *RecordTy:
		return instantiateRecord(d, args)
	case *UnionTy:
		return instantiateUnion(d, args)
	case *BuiltinTy:
		return instantiateBuiltin(d, args)
	}
	panic(diag.Internal("%s is not a generic type", declaration.Name()))
}

func instantiateRecord(d *RecordTy, args []Ty) *RecordTy {
	v := d.instances.get(args, func() any {
		return &RecordTy{NominalBase: d.newInstanceBase(args), Decl: d.Decl, origin: d}
	})
	return v.(*RecordTy)
}

func instantiateUnion(d *UnionTy, args []Ty) *UnionTy {
	v := d.instances.get(args, func() any {
		return &UnionTy{NominalBase: d.newInstanceBase(args), Decl: d.Decl, origin: d}
	})
	return v.(*UnionTy)
}

// SelfType is the type that the declaration's own methods call Self: the declaration, or for a generic declaration
// its instance at its own parameters.
func SelfType(declaration NominalTy) NominalTy {
	base := declaration.Base()
	if !base.IsGeneric() || base.declaring != nil {
		return declaration
	}
	args := make([]Ty, len(base.Params))
	for i, p := range base.Params {
		args[i] = p
	}
	return Instantiate(declaration, args)
}

// Instance is the function a generic declaration becomes for a list of type arguments, with its signature
// substituted. Its body is the declaration's, which is lowered once for each module that needs the instance.
func (f *FuncDecl) Instance(args []Ty) *FuncDecl {
	return f.Instances.get(args, func() any { return f.instantiate(args) }).(*FuncDecl)
}

func (f *FuncDecl) instantiate(args []Ty) *FuncDecl {
	inst := *f
	b := Binding{Params: f.Generics, Args: args}
	inst.Generic = f
	inst.TypeArgs = args
	inst.Generics = nil
	inst.Instances = nil
	inst.Uses = nil
	if f.Owner != nil {
		inst.Owner = b.Apply(f.Owner).(NominalTy)
	}
	inst.ParamTypes = make([]Ty, len(f.ParamTypes))
	for i, t := range f.ParamTypes {
		inst.ParamTypes[i] = b.Apply(t)
	}
	inst.Pass.Specs = b.ApplySpecs(f.Pass.Specs)
	inst.ReturnType = b.Apply(f.ReturnType)
	return &inst
}

// ApplySpecs is the parameters with their types replaced, or the list itself when none changes.
func (b Binding) ApplySpecs(specs []ParamSpec) []ParamSpec {
	var out []ParamSpec
	for i, s := range specs {
		t := b.Apply(s.Type)
		if t != s.Type && out == nil {
			out = append(make([]ParamSpec, 0, len(specs)), specs[:i]...)
		}
		if out != nil {
			out = append(out, ParamSpec{Name: s.Name, Type: t, Conv: s.Conv})
		}
	}
	if out == nil {
		return specs
	}
	return out
}

// Resolved is the function a call runs once the type parameters of the code it is in are bound: the target itself, or
// the instance of a generic one, or for a call through a bound the method of the type that stands in for the
// parameter.
func (c *CallExpr) Resolved(b Binding) *FuncDecl {
	switch c.Kind {
	case CallFunction, CallStatic, CallMethod:
		if len(c.Target.Generics) == 0 {
			return c.Target
		}
		return c.Target.Instance(b.applyAll(c.Generic.TypeArgs))
	case CallBound:
		// A frozen receiver lends its contents, so the method is the one of the type it holds.
		recv, ok := Content(b.Apply(c.Receiver.Ty())).(NominalTy)
		if !ok {
			panic(diag.Internal("a call through a bound has a receiver of %s, which has no methods", b.Apply(c.Receiver.Ty()).Name()))
		}
		method, found := recv.MethodTable()[c.Generic.Method.Name]
		if !found {
			panic(diag.Internal("%s has no method '%s' for the bound that declares it", recv.Name(), c.Generic.Method.Name))
		}
		if len(method.Generics) == 0 {
			return method
		}
		return method.Instance(recv.Base().Args)
	}
	panic(diag.Internal("call kind %d has no function to resolve", int(c.Kind)))
}
