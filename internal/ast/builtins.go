package ast

import "minimaxon/internal/diag"

// BuiltinKind says which runtime primitive a BuiltinTy is.
type BuiltinKind int

const (
	BuiltinArray BuiltinKind = iota
	BuiltinMap
	BuiltinList
	BuiltinPool
	BuiltinHandle
	BuiltinFrozen
	BuiltinCown
)

var builtinNames = [...]string{"array", "map", "list", "pool", "Handle", "frozen", "cown"}

// BuiltinTy is a runtime primitive: array with T, map with (K, V), list with T, pool with T, the Handle with T that
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

func (t *BuiltinTy) IsManaged() bool { return t.Kind != BuiltinHandle }

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
func (t *BuiltinTy) Subscriptable() bool { return t.Kind <= BuiltinPool }

// Shared is whether the type is counted and has many owners: a frozen value or a cown reference. Copying one is another
// owner of the same value, never a new value.
func (t *BuiltinTy) Shared() bool { return t.Kind == BuiltinFrozen || t.Kind == BuiltinCown }

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

// Intrinsic names a method that no source declares: the checker types a call of it like any other method, and
// lowering turns it into IR instead of a call.
type Intrinsic int

const (
	NotIntrinsic Intrinsic = iota
	IntrinsicArrayNew
	IntrinsicArrayFilled
	IntrinsicArrayCount
	IntrinsicArrayIsEmpty
	IntrinsicArrayAppend
	IntrinsicArrayInsert
	IntrinsicArrayRemove
	IntrinsicArrayPop
	IntrinsicArrayDropEmpty
	IntrinsicMapNew
	IntrinsicMapCount
	IntrinsicMapIsEmpty
	IntrinsicMapHas
	IntrinsicMapInsert
	IntrinsicMapRemove
	IntrinsicMapReplace
	IntrinsicMapDropEmpty
	IntrinsicListNew
	IntrinsicListCount
	IntrinsicListIsEmpty
	IntrinsicListContains
	IntrinsicListPushFront
	IntrinsicListPushBack
	IntrinsicListPopFront
	IntrinsicListPopBack
	IntrinsicListFirst
	IntrinsicListLast
	IntrinsicListNext
	IntrinsicListPrev
	IntrinsicListRemove
	IntrinsicListMoveToFront
	IntrinsicListDropEmpty
	IntrinsicPoolNew
	IntrinsicPoolCount
	IntrinsicPoolIsEmpty
	IntrinsicPoolContains
	IntrinsicPoolInsert
	IntrinsicPoolRemove
	IntrinsicPoolDropEmpty
	IntrinsicOptionIsSome
	IntrinsicOptionUnwrap
	IntrinsicFreeze
	IntrinsicShare
	IntrinsicThaw
	IntrinsicCown
	IntrinsicExchange
)

// Option cases, in the order the union declares them.
const (
	OptionSome = 0
	OptionNone = 1
)

// Builtins are the generic types and the union that every project has before it declares anything: the runtime
// primitives and Option, the result of an operation that can miss. They belong to the stdlib module, so a name
// resolves to them last.
type Builtins struct {
	Array, Map, List, Pool, Handle, Frozen, Cown *BuiltinTy
	Option                                       *UnionTy
	// Copyable and Droppable are the capabilities every type has unless it opts out or holds a part that lacks them.
	// They are interfaces only so a bound can name them: no declaration implements them, and the checker answers
	// whether a type has one from the type's own parts.
	Copyable, Droppable *InterfaceTy
	// CownNew is the function that cown(x) calls: the constructor of the cown type. CownNewDropWith is the one that
	// cown(x, dropWith: d) calls: its content needs no Droppable bound, because the destructor d ends it.
	CownNew, CownNewDropWith *FuncDecl
	// Count is the type of a length and of a position in an array.
	Count  *IntTy
	stdlib *Module
}

func newBuiltinDecl(stdlib *Module, name string, params ...string) *TypeDecl {
	d := NewTypeDecl()
	d.Name = name
	d.Visibility = "public"
	d.Module = stdlib
	d.Src = stdlibPositions
	for _, p := range params {
		tp := NewTypeParam()
		tp.Name = p
		d.TypeParams = append(d.TypeParams, tp)
	}
	return d
}

func (b *Builtins) newPrimitive(kind BuiltinKind, params ...string) *BuiltinTy {
	d := newBuiltinDecl(b.stdlib, builtinNames[kind], params...)
	t := &BuiltinTy{name: d.Name, Methods: map[string]*FuncDecl{}, Kind: kind, Decl: d, lib: b}
	t.initGeneric(d.TypeParams, nil)
	d.Ty = t
	b.stdlib.Decls = append(b.stdlib.Decls, d)
	b.stdlib.Names[d.Name] = d
	return t
}

// NewBuiltins declares the primitives and Option in the stdlib module.
func NewBuiltins(stdlib *Module, count *IntTy) *Builtins {
	b := &Builtins{stdlib: stdlib, Count: count}
	b.Array = b.newPrimitive(BuiltinArray, "T")
	b.Map = b.newPrimitive(BuiltinMap, "K", "V")
	b.List = b.newPrimitive(BuiltinList, "T")
	b.Pool = b.newPrimitive(BuiltinPool, "T")
	b.Handle = b.newPrimitive(BuiltinHandle, "T")
	b.Frozen = b.newPrimitive(BuiltinFrozen, "T")
	b.Cown = b.newPrimitive(BuiltinCown, "T")
	b.declareOption()
	b.Copyable = b.declareCapability("Copyable")
	b.Droppable = b.declareCapability("Droppable")
	b.declareArray()
	b.declareMap()
	b.declareList()
	b.declarePool()
	b.declareSharing()
	return b
}

func (b *Builtins) declareOption() {
	d := NewUnionDecl()
	d.Name = "Option"
	d.Visibility = "public"
	d.Module = b.stdlib
	d.Src = stdlibPositions
	tp := NewTypeParam()
	tp.Name = "T"
	d.TypeParams = []*TypeParam{tp}
	u := NewUnionTy(d)
	u.Cases = []*UnionCase{
		{Name: "some", Payload: []PayloadField{{Name: "value", Type: u.Params[0]}}},
		{Name: "none"},
	}
	d.Ty = u
	b.Option = u
	b.stdlib.Decls = append(b.stdlib.Decls, d)
	b.stdlib.Names[d.Name] = d

	t := u.Params[0]
	b.method(u, IntrinsicOptionIsSome, "isSome", ConvLet, BoolTy)
	b.method(u, IntrinsicOptionUnwrap, "unwrap", ConvConsumes, t)
}

func (b *Builtins) declareCapability(name string) *InterfaceTy {
	d := NewInterfaceDecl()
	d.Name = name
	d.Visibility = "public"
	d.Module = b.stdlib
	d.Src = stdlibPositions
	t := NewInterfaceTy(d)
	d.Ty = t
	b.stdlib.Decls = append(b.stdlib.Decls, d)
	b.stdlib.Names[name] = d
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

func param(name string, t Ty, conv Convention) ParamSpec {
	return ParamSpec{Name: name, Type: t, Conv: conv}
}

// method declares an instance method of a generic primitive, with the type's parameters as its own.
func (b *Builtins) method(owner NominalTy, op Intrinsic, name string, self Convention, ret Ty, params ...ParamSpec) {
	b.declare(owner, op, name, false, self, ret, params)
}

func (b *Builtins) static(owner NominalTy, op Intrinsic, name string, ret Ty, params ...ParamSpec) {
	b.declare(owner, op, name, true, ConvLet, ret, params)
}

func (b *Builtins) declare(owner NominalTy, op Intrinsic, name string, static bool, self Convention, ret Ty, params []ParamSpec) {
	f := NewFuncDecl()
	f.Name = name
	f.Visibility = "public"
	f.Module = b.stdlib
	f.Src = stdlibPositions
	f.IsStatic = static
	if self != ConvLet {
		f.Receiver = self.String()
	}
	f.Owner = SelfType(owner)
	f.Generics = owner.Base().Params
	f.Pass = Passing{SelfConv: self, Specs: params}
	f.ParamTypes = make([]Ty, len(params))
	for i, p := range params {
		f.ParamTypes[i] = p.Type
	}
	f.ReturnType = ret
	f.Intrinsic = op
	owner.MethodTable()[name] = f
}

// bound adds a bound on the owner's parameter to a method that is already declared.
func (b *Builtins) bound(owner NominalTy, method string, p *TypeParamTy, iface *InterfaceTy) {
	f := owner.MethodTable()[method]
	f.Bounds = append(f.Bounds, ParamBound{Param: p, Iface: iface})
}

func (b *Builtins) declareArray() {
	a := b.Array
	t := a.Params[0]
	self := SelfType(a)
	b.static(a, IntrinsicArrayNew, "new", self)
	b.static(a, IntrinsicArrayFilled, "filled", self, param("count", b.Count, ConvLet), param("value", t, ConvConsumes))
	b.bound(a, "filled", t, b.Copyable)
	b.method(a, IntrinsicArrayCount, "count", ConvLet, b.Count)
	b.method(a, IntrinsicArrayIsEmpty, "isEmpty", ConvLet, BoolTy)
	b.method(a, IntrinsicArrayAppend, "append", ConvMutates, VoidTy, param("item", t, ConvConsumes))
	b.method(a, IntrinsicArrayInsert, "insert", ConvMutates, VoidTy, param("item", t, ConvConsumes), param("at", b.Count, ConvLet))
	b.method(a, IntrinsicArrayRemove, "remove", ConvMutates, t, param("at", b.Count, ConvLet))
	b.method(a, IntrinsicArrayPop, "pop", ConvMutates, b.OptionOf(t))
	b.method(a, IntrinsicArrayDropEmpty, "dropEmpty", ConvConsumes, VoidTy)
}

func (b *Builtins) declareMap() {
	m := b.Map
	k, v := m.Params[0], m.Params[1]
	b.static(m, IntrinsicMapNew, "new", SelfType(m))
	b.method(m, IntrinsicMapCount, "count", ConvLet, b.Count)
	b.method(m, IntrinsicMapIsEmpty, "isEmpty", ConvLet, BoolTy)
	b.method(m, IntrinsicMapHas, "has", ConvLet, BoolTy, param("key", k, ConvLet))
	b.method(m, IntrinsicMapInsert, "insert", ConvMutates, VoidTy, param("key", k, ConvConsumes), param("value", v, ConvConsumes))
	b.bound(m, "insert", v, b.Droppable)
	b.method(m, IntrinsicMapReplace, "replace", ConvMutates, b.OptionOf(v), param("key", k, ConvConsumes), param("value", v, ConvConsumes))
	b.method(m, IntrinsicMapRemove, "remove", ConvMutates, b.OptionOf(v), param("key", k, ConvLet))
	b.method(m, IntrinsicMapDropEmpty, "dropEmpty", ConvConsumes, VoidTy)
}

func (b *Builtins) declareList() {
	l := b.List
	t := l.Params[0]
	h := b.HandleOf(t)
	b.static(l, IntrinsicListNew, "new", SelfType(l))
	b.method(l, IntrinsicListCount, "count", ConvLet, b.Count)
	b.method(l, IntrinsicListIsEmpty, "isEmpty", ConvLet, BoolTy)
	b.method(l, IntrinsicListContains, "contains", ConvLet, BoolTy, param("handle", h, ConvLet))
	b.method(l, IntrinsicListPushFront, "pushFront", ConvMutates, h, param("item", t, ConvConsumes))
	b.method(l, IntrinsicListPushBack, "pushBack", ConvMutates, h, param("item", t, ConvConsumes))
	b.method(l, IntrinsicListPopFront, "popFront", ConvMutates, b.OptionOf(t))
	b.method(l, IntrinsicListPopBack, "popBack", ConvMutates, b.OptionOf(t))
	b.method(l, IntrinsicListFirst, "first", ConvLet, b.OptionOf(h))
	b.method(l, IntrinsicListLast, "last", ConvLet, b.OptionOf(h))
	b.method(l, IntrinsicListNext, "next", ConvLet, b.OptionOf(h), param("handle", h, ConvLet))
	b.method(l, IntrinsicListPrev, "prev", ConvLet, b.OptionOf(h), param("handle", h, ConvLet))
	b.method(l, IntrinsicListRemove, "remove", ConvMutates, b.OptionOf(t), param("handle", h, ConvLet))
	b.method(l, IntrinsicListMoveToFront, "moveToFront", ConvMutates, VoidTy, param("handle", h, ConvLet))
	b.method(l, IntrinsicListDropEmpty, "dropEmpty", ConvConsumes, VoidTy)
}

func (b *Builtins) declarePool() {
	p := b.Pool
	t := p.Params[0]
	h := b.HandleOf(t)
	b.static(p, IntrinsicPoolNew, "new", SelfType(p))
	b.method(p, IntrinsicPoolCount, "count", ConvLet, b.Count)
	b.method(p, IntrinsicPoolIsEmpty, "isEmpty", ConvLet, BoolTy)
	b.method(p, IntrinsicPoolContains, "contains", ConvLet, BoolTy, param("handle", h, ConvLet))
	b.method(p, IntrinsicPoolInsert, "insert", ConvMutates, h, param("item", t, ConvConsumes))
	b.method(p, IntrinsicPoolRemove, "remove", ConvMutates, b.OptionOf(t), param("handle", h, ConvLet))
	b.method(p, IntrinsicPoolDropEmpty, "dropEmpty", ConvConsumes, VoidTy)
}

// declareSharing declares the functions that make and use the values which cross between threads:
//
//	freeze(x)  takes a value and gives an immutable frozen with T, which the last owner drops, so T must be Droppable
//	share(x)   gives another owner of the same frozen value or cown
//	thaw(x)    gives an owned copy of what a frozen value holds, so what it holds must be Copyable
//	cown(x)    takes a value and gives a reference to a cown that owns it, and drops it with the last reference
//	cown(x, dropWith: d)  does the same for a value that is not Droppable, and ends it with its destructor d instead
//
// exchange is declared here as well: it is no sharing function, but it is a generic function with a type parameter of
// its own, which only these are.
//
// share is declared over any type, because it takes a frozen value or a cown reference alike; the checker refuses
// the others. cown is the constructor of the type of the same name, so it is not a name of its own: calling the type
// calls CownNew.
func (b *Builtins) declareSharing() {
	freezeT := newBoundedTypeParamTy("T", b.Droppable)
	shareT := newTypeParamTy("T")
	thawT := newBoundedTypeParamTy("T", b.Copyable)
	cownT := newBoundedTypeParamTy("T", b.Droppable)
	cownDropWithT := newTypeParamTy("T")
	exchangeT := newTypeParamTy("T")
	b.declareGeneric(IntrinsicFreeze, "freeze", freezeT, b.FrozenOf(freezeT), param("x", freezeT, ConvConsumes))
	b.declareGeneric(IntrinsicShare, "share", shareT, shareT, param("x", shareT, ConvLet))
	b.declareGeneric(IntrinsicThaw, "thaw", thawT, thawT, param("x", b.FrozenOf(thawT), ConvLet))
	b.CownNew = b.makeGeneric(IntrinsicCown, "cown", cownT, b.CownOf(cownT), param("x", cownT, ConvConsumes))
	b.CownNewDropWith = b.makeGeneric(IntrinsicCown, "cown", cownDropWithT, b.CownOf(cownDropWithT), param("x", cownDropWithT, ConvConsumes))
	b.declareGeneric(IntrinsicExchange, "exchange", exchangeT, exchangeT, param("place", exchangeT, ConvMutates), param("value", exchangeT, ConvConsumes))
}

func (b *Builtins) declareGeneric(op Intrinsic, name string, t *TypeParamTy, ret Ty, params ...ParamSpec) {
	b.stdlib.Names[name] = b.makeGeneric(op, name, t, ret, params...)
}

func (b *Builtins) makeGeneric(op Intrinsic, name string, t *TypeParamTy, ret Ty, params ...ParamSpec) *FuncDecl {
	f := NewFuncDecl()
	f.Name = name
	f.Visibility = "public"
	f.Module = b.stdlib
	f.Src = stdlibPositions
	f.Generics = []*TypeParamTy{t}
	f.Instances = &InstanceTable{}
	f.Intrinsic = op
	f.Pass = Passing{Specs: params}
	f.ParamTypes = make([]Ty, len(params))
	for i, p := range params {
		f.ParamTypes[i] = p.Type
	}
	f.ReturnType = ret
	return f
}

func instantiateBuiltin(d *BuiltinTy, args []Ty) *BuiltinTy {
	v := d.instances.get(args, func() any {
		return &BuiltinTy{NominalBase: d.newInstanceBase(args), Kind: d.Kind, Decl: d.Decl, origin: d, lib: d.lib}
	})
	return v.(*BuiltinTy)
}
