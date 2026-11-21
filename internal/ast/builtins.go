package ast

//go:generate go run minimaxon/internal/builtingen

import "minimaxon/internal/diag"

// builtinKindInfo is what the table says about a kind of runtime primitive.
type builtinKindInfo struct {
	name                           string
	managed, subscriptable, shared bool
}

// BuiltinTy is a runtime primitive: Array with T, Map with (K, V), List with T, Pool with T, the Handle with T that
// names an element of a pool or a list, or one of the two types that cross between threads: frozen with T, an
// immutable value, and cown with T, a reference to a cown that owns a value. It is generic like a record: the declaration has Params, each instance has
// Args, and an instance is interned by them. An array, a map, a list and a pool own their elements and are managed; a
// Handle is a key, a plain word that owns nothing, so it is copied like an integer; a frozen value and a cown
// reference are managed and counted, and have many owners.
type BuiltinTy struct {
	NominalBase
	Kind   BuiltinKind
	Decl   *TypeDecl
	origin *BuiltinTy
	lib    *Builtins
}

func (t *BuiltinTy) IsManaged() bool { return builtinKinds[t.Kind].managed }

// args are the type arguments the type is read at: an instance's own, or for the declaration its parameters.
func (t *BuiltinTy) args() []Ty {
	if len(t.Args) > 0 {
		return t.Args
	}
	args := make([]Ty, len(t.Params))
	for i, p := range t.Params {
		args[i] = p
	}
	return args
}

// Subscriptable is whether t[i] names an element: an array, a map, a list or a pool.
func (t *BuiltinTy) Subscriptable() bool { return builtinKinds[t.Kind].subscriptable }

// Shared is whether the type is counted and has many owners: a frozen value or a cown reference. Copying one is another
// owner of the same value, never a new value.
func (t *BuiltinTy) Shared() bool { return builtinKinds[t.Kind].shared }

// Held is the type of the value a frozen value or a cown holds.
func (t *BuiltinTy) Held() Ty {
	if !t.Shared() {
		panic(diag.Internal("%s holds no shared value", t.Name()))
	}
	return t.args()[0]
}

// IsFrozen is whether t is a frozen value.
func IsFrozen(t Ty) bool {
	bt, ok := t.(*BuiltinTy)
	return ok && bt.Kind == BuiltinFrozen
}

// IsCown is whether t is a cown reference.
func IsCown(t Ty) bool {
	bt, ok := t.(*BuiltinTy)
	return ok && bt.Kind == BuiltinCown
}

// DropWithLabel is the label of the argument that names the destructor a cown ends its content with.
const DropWithLabel = "dropWith"

// IsCownConstructor is whether a name refers to the cown type, which a call constructs.
func IsCownConstructor(d Decl) bool {
	td, ok := d.(*TypeDecl)
	if !ok {
		return false
	}
	bt, ok := td.Ty.(*BuiltinTy)
	return ok && bt.Kind == BuiltinCown
}

// Content is the type that reading through a value of type t sees: what a frozen value holds, however many frozen
// values are nested, and t itself for anything else. The contents of a frozen value are lent as they are, immutable.
func Content(t Ty) Ty {
	for IsFrozen(t) {
		t = t.(*BuiltinTy).Held()
	}
	return t
}

// Index is the type of what goes between the brackets: a position for an array, a key for a map, and for a list or a
// pool the handle of the element.
func (t *BuiltinTy) Index() Ty {
	args := t.args()
	switch t.Kind {
	case BuiltinArray:
		return t.lib.Count
	case BuiltinMap:
		return args[0]
	case BuiltinList, BuiltinPool:
		return t.lib.HandleOf(args[0])
	}
	panic(diag.Internal("%s has no elements to index", t.Name()))
}

// Element is the type of t[i]: what the primitive holds.
func (t *BuiltinTy) Element() Ty {
	args := t.args()
	switch t.Kind {
	case BuiltinArray, BuiltinList, BuiltinPool:
		return args[0]
	case BuiltinMap:
		return args[1]
	}
	panic(diag.Internal("%s has no elements", t.Name()))
}

// PacksBytes is whether t is an array whose box holds each element in one byte: an array of an integer type whose range
// fits in a Byte, as Array with Byte (ByteArray) is. Every other array holds a word to an element. Every operation on an
// array asks this, so the two boxes are never mistaken for each other.
func (t *BuiltinTy) PacksBytes() bool {
	if t.Kind != BuiltinArray {
		return false
	}
	elem, isInt := t.args()[0].(*IntTy)
	return isInt && t.lib.Byte.Covers(elem)
}

// IsKey is whether a value of type t may be the key of a map: one that is compared and copied as a word or as text.
func IsKey(t Ty) bool {
	switch k := t.(type) {
	case *IntTy:
		return !k.IsAnonymous()
	case *PrimTy:
		return k == BoolTy || k == StringTy
	case *EnumTy:
		return true
	case *BuiltinTy:
		return k.Kind == BuiltinHandle
	}
	return false
}

// Builtins are the types and functions that every project has before it declares anything, as the table in
// internal/builtingen declares them: the runtime primitives, Option, the result of an operation that can miss, the
// error of arithmetic, and the capabilities. They belong to the stdlib module, so a name resolves to them last.
type Builtins struct {
	builtinDecls
	// Count is the type of a length and of a position in an array.
	Count *IntTy
	// Byte is the type of one UTF-8 byte of a String (0 to 255).
	Byte *IntTy
	// StringMethods are the built-in methods on String.
	StringMethods map[string]*FuncDecl
	stdlib        *Module
}

func newBuiltinDecl(stdlib *Module, name string, params ...string) *TypeDecl {
	d := stdlib.Tree.NewTypeDecl(0)
	d.Name = name
	d.Visibility = "public"
	d.Module = stdlib
	for _, p := range params {
		tp := stdlib.Tree.NewTypeParam(0)
		tp.Name = p
		d.TypeParams = append(d.TypeParams, tp)
	}
	return d
}

func (b *Builtins) newPrimitive(kind BuiltinKind, params ...string) *BuiltinTy {
	d := newBuiltinDecl(b.stdlib, builtinKinds[kind].name, params...)
	t := &BuiltinTy{name: d.Name, link: nominalLinkName(&d.DeclBase), Methods: map[string]*FuncDecl{}, Kind: kind, Decl: d, lib: b}
	t.initGeneric(d.TypeParams, nil)
	d.Ty = t
	b.stdlib.Declare(d)
	return t
}

// NewBuiltins declares the table in the stdlib module.
func NewBuiltins(stdlib *Module, count, byteTy *IntTy) *Builtins {
	b := &Builtins{stdlib: stdlib, Count: count, Byte: byteTy, StringMethods: map[string]*FuncDecl{}}
	b.declareTable()
	return b
}

// declareAlias declares a typealias of an instance, which names the instance itself.
func (b *Builtins) declareAlias(name string, t Ty) {
	alias := b.stdlib.Tree.NewAliasDecl(0)
	alias.Name = name
	alias.Visibility = "public"
	alias.Module = b.stdlib
	alias.Ty = t
	b.stdlib.Declare(alias)
}

// newUnion declares a generic union whose cases the caller gives.
func (b *Builtins) newUnion(name string, params ...string) *UnionTy {
	d := b.stdlib.Tree.NewUnionDecl(0)
	d.Name = name
	d.Visibility = "public"
	d.Module = b.stdlib
	for _, p := range params {
		tp := b.stdlib.Tree.NewTypeParam(0)
		tp.Name = p
		d.TypeParams = append(d.TypeParams, tp)
	}
	u := NewUnionTy(d)
	d.Ty = u
	b.stdlib.Declare(d)
	return u
}

// newEnum declares an enum whose raw values are the indexes of its cases.
func (b *Builtins) newEnum(name string, cases ...string) *EnumTy {
	d := b.stdlib.Tree.NewEnumDecl(0)
	d.Name = name
	d.Visibility = "public"
	d.Module = b.stdlib
	t := NewEnumTy(d)
	t.Cases = cases
	for i := range cases {
		t.Raw = append(t.Raw, I64(int64(i)))
	}
	d.Ty = t
	b.stdlib.Declare(d)
	return t
}

// declareInterface declares an interface with no methods: a capability, which no declaration implements and the checker
// answers from a type's own parts, or a marker such as Error, which a declaration says it implements.
func (b *Builtins) declareInterface(name string) *InterfaceTy {
	d := b.stdlib.Tree.NewInterfaceDecl(0)
	d.Name = name
	d.Visibility = "public"
	d.Module = b.stdlib
	t := NewInterfaceTy(d)
	d.Ty = t
	b.stdlib.Declare(d)
	return t
}

// IsCapability is whether iface is Copyable or Droppable, which a type has by its parts and never by saying so.
func (b *Builtins) IsCapability(iface *InterfaceTy) bool {
	return iface == b.Copyable || iface == b.Droppable
}

// Is is whether t is a type that the builtins declare.
func (b *Builtins) Is(t Ty) bool {
	switch x := t.(type) {
	case *BuiltinTy:
		return true
	case *UnionTy:
		return x.Base().Declaration() == b.Option.Base()
	}
	return false
}

// HandleOf is Handle with t.
func (b *Builtins) HandleOf(t Ty) *BuiltinTy { return instantiateBuiltin(b.Handle, []Ty{t}) }

// FrozenOf is frozen with t.
func (b *Builtins) FrozenOf(t Ty) *BuiltinTy { return instantiateBuiltin(b.Frozen, []Ty{t}) }

// CownOf is cown with t.
func (b *Builtins) CownOf(t Ty) *BuiltinTy { return instantiateBuiltin(b.Cown, []Ty{t}) }

// OptionOf is Option with t.
func (b *Builtins) OptionOf(t Ty) *UnionTy { return instantiateUnion(b.Option, []Ty{t}) }

func (b *Builtins) exitCode() Ty { return b.stdlib.Names["ExitCode"].(*AliasDecl).Ty }

func param(name string, t Ty, conv Convention) ParamSpec {
	return ParamSpec{Name: name, Type: t, Conv: conv}
}

// newFunction is the declaration of a built-in function that takes params and returns ret.
func (b *Builtins) newFunction(op Intrinsic, name string, ret Ty, params []ParamSpec) *FuncDecl {
	f := b.stdlib.Tree.NewFuncDecl(0)
	f.Name = name
	f.Visibility = "public"
	f.Module = b.stdlib
	f.Intrinsic = op
	f.Pass = Passing{Specs: params}
	f.ParamTypes = make([]Ty, len(params))
	for i, p := range params {
		f.ParamTypes[i] = p.Type
	}
	f.ReturnType = ret
	return f
}

// method declares a method of a type of the table, with the type's parameters as its own.
func (b *Builtins) method(owner NominalTy, op Intrinsic, name string, static bool, self Convention, ret Ty, params ...ParamSpec) *FuncDecl {
	f := b.newFunction(op, name, ret, params)
	f.IsStatic = static
	if self != ConvLet {
		f.Receiver = self.String()
	}
	f.Owner = SelfType(owner)
	f.Generics = owner.Base().Params
	f.Pass.SelfConv = self
	owner.MethodTable()[name] = f
	return f
}

// function is a free function, or a method of String, which is a primitive and has no method table of its own.
func (b *Builtins) function(op Intrinsic, name string, ret Ty, params ...ParamSpec) *FuncDecl {
	return b.newFunction(op, name, ret, params)
}

// genericFunction is a free function with a type parameter of its own.
func (b *Builtins) genericFunction(op Intrinsic, name string, t *TypeParamTy, ret Ty, params ...ParamSpec) *FuncDecl {
	f := b.newFunction(op, name, ret, params)
	f.Generics = []*TypeParamTy{t}
	f.Instances = &InstanceTable{}
	return f
}

func instantiateBuiltin(d *BuiltinTy, args []Ty) *BuiltinTy {
	v := d.instances.get(args, func() any {
		return &BuiltinTy{NominalBase: d.newInstanceBase(args), Kind: d.Kind, Decl: d.Decl, origin: d, lib: d.lib}
	})
	return v.(*BuiltinTy)
}
