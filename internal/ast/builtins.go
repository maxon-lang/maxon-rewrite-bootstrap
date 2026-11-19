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

// BuiltinTy is a runtime primitive: array[T], map[K, V], list[T], pool[T], the Handle[T] that names an element of a
// pool or a list, or one of the two types that cross between threads: frozen[T], an immutable value, and cown[T], a
// reference to a cown that owns a value. It is generic like a record: the declaration has Params, each instance has
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
	IntrinsicMapNew
	IntrinsicMapCount
	IntrinsicMapIsEmpty
	IntrinsicMapHas
	IntrinsicMapInsert
	IntrinsicMapRemove
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
	IntrinsicPoolNew
	IntrinsicPoolCount
	IntrinsicPoolIsEmpty
	IntrinsicPoolContains
	IntrinsicPoolInsert
	IntrinsicPoolRemove
	IntrinsicOptionIsSome
	IntrinsicOptionUnwrap
	IntrinsicFreeze
	IntrinsicShare
	IntrinsicThaw
	IntrinsicCown
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
	// CownNew is the function that cown(x) calls: the constructor of the cown type.
	CownNew *FuncDecl
	// Count is the type of a length and of a position in an array.
	Count  *IntTy
	stdlib *Module
}

func newBuiltinDecl(stdlib *Module, name string, params ...string) *TypeDecl {
	d := NewTypeDecl()
	d.Name = name
	d.Visibility = "public"
	d.Module = stdlib
	d.File = "<stdlib>"
	d.TypeParams = &TypeParamList{}
	for _, p := range params {
		tp := NewTypeParam()
		tp.Name = p
		d.TypeParams.Params = append(d.TypeParams.Params, tp)
	}
	return d
}

func (b *Builtins) newPrimitive(kind BuiltinKind, params ...string) *BuiltinTy {
	d := newBuiltinDecl(b.stdlib, builtinNames[kind], params...)
	t := &BuiltinTy{name: d.Name, Methods: map[string]*FuncDecl{}, Kind: kind, Decl: d, lib: b}
	t.initGeneric(d.TypeParams.Items())
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
	d.File = "<stdlib>"
	tp := NewTypeParam()
	tp.Name = "T"
	d.TypeParams = &TypeParamList{Params: []*TypeParam{tp}}
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
	b.method(u, IntrinsicOptionUnwrap, "unwrap", ConvSink, t)
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

// HandleOf is Handle[t].
func (b *Builtins) HandleOf(t Ty) *BuiltinTy { return instantiateBuiltin(b.Handle, []Ty{t}) }

// FrozenOf is frozen[t].
func (b *Builtins) FrozenOf(t Ty) *BuiltinTy { return instantiateBuiltin(b.Frozen, []Ty{t}) }

// CownOf is cown[t].
func (b *Builtins) CownOf(t Ty) *BuiltinTy { return instantiateBuiltin(b.Cown, []Ty{t}) }

// OptionOf is Option[t].
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
	f.File = "<stdlib>"
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

func (b *Builtins) declareArray() {
	a := b.Array
	t := a.Params[0]
	self := SelfType(a)
	b.static(a, IntrinsicArrayNew, "new", self)
	b.static(a, IntrinsicArrayFilled, "filled", self, param("count", b.Count, ConvLet), param("value", t, ConvSink))
	b.method(a, IntrinsicArrayCount, "count", ConvLet, b.Count)
	b.method(a, IntrinsicArrayIsEmpty, "isEmpty", ConvLet, BoolTy)
	b.method(a, IntrinsicArrayAppend, "append", ConvMutable, VoidTy, param("item", t, ConvSink))
	b.method(a, IntrinsicArrayInsert, "insert", ConvMutable, VoidTy, param("item", t, ConvSink), param("at", b.Count, ConvLet))
	b.method(a, IntrinsicArrayRemove, "remove", ConvMutable, t, param("at", b.Count, ConvLet))
	b.method(a, IntrinsicArrayPop, "pop", ConvMutable, b.OptionOf(t))
}

func (b *Builtins) declareMap() {
	m := b.Map
	k, v := m.Params[0], m.Params[1]
	b.static(m, IntrinsicMapNew, "new", SelfType(m))
	b.method(m, IntrinsicMapCount, "count", ConvLet, b.Count)
	b.method(m, IntrinsicMapIsEmpty, "isEmpty", ConvLet, BoolTy)
	b.method(m, IntrinsicMapHas, "has", ConvLet, BoolTy, param("key", k, ConvLet))
	b.method(m, IntrinsicMapInsert, "insert", ConvMutable, VoidTy, param("key", k, ConvSink), param("value", v, ConvSink))
	b.method(m, IntrinsicMapRemove, "remove", ConvMutable, b.OptionOf(v), param("key", k, ConvLet))
}

func (b *Builtins) declareList() {
	l := b.List
	t := l.Params[0]
	h := b.HandleOf(t)
	b.static(l, IntrinsicListNew, "new", SelfType(l))
	b.method(l, IntrinsicListCount, "count", ConvLet, b.Count)
	b.method(l, IntrinsicListIsEmpty, "isEmpty", ConvLet, BoolTy)
	b.method(l, IntrinsicListContains, "contains", ConvLet, BoolTy, param("handle", h, ConvLet))
	b.method(l, IntrinsicListPushFront, "pushFront", ConvMutable, h, param("item", t, ConvSink))
	b.method(l, IntrinsicListPushBack, "pushBack", ConvMutable, h, param("item", t, ConvSink))
	b.method(l, IntrinsicListPopFront, "popFront", ConvMutable, b.OptionOf(t))
	b.method(l, IntrinsicListPopBack, "popBack", ConvMutable, b.OptionOf(t))
	b.method(l, IntrinsicListFirst, "first", ConvLet, b.OptionOf(h))
	b.method(l, IntrinsicListLast, "last", ConvLet, b.OptionOf(h))
	b.method(l, IntrinsicListNext, "next", ConvLet, b.OptionOf(h), param("handle", h, ConvLet))
	b.method(l, IntrinsicListPrev, "prev", ConvLet, b.OptionOf(h), param("handle", h, ConvLet))
	b.method(l, IntrinsicListRemove, "remove", ConvMutable, b.OptionOf(t), param("handle", h, ConvLet))
	b.method(l, IntrinsicListMoveToFront, "moveToFront", ConvMutable, VoidTy, param("handle", h, ConvLet))
}

func (b *Builtins) declarePool() {
	p := b.Pool
	t := p.Params[0]
	h := b.HandleOf(t)
	b.static(p, IntrinsicPoolNew, "new", SelfType(p))
	b.method(p, IntrinsicPoolCount, "count", ConvLet, b.Count)
	b.method(p, IntrinsicPoolIsEmpty, "isEmpty", ConvLet, BoolTy)
	b.method(p, IntrinsicPoolContains, "contains", ConvLet, BoolTy, param("handle", h, ConvLet))
	b.method(p, IntrinsicPoolInsert, "insert", ConvMutable, h, param("item", t, ConvSink))
	b.method(p, IntrinsicPoolRemove, "remove", ConvMutable, b.OptionOf(t), param("handle", h, ConvLet))
}

// declareSharing declares the functions that make and use the values which cross between threads:
//
//	freeze(x)  takes a value and gives an immutable frozen[T]
//	share(x)   gives another owner of the same frozen value or cown
//	thaw(x)    gives an owned copy of what a frozen value holds
//	cown(x)    takes a value and gives a reference to a cown that owns it
//
// share is declared over any type, because it takes a frozen value or a cown reference alike; the checker refuses
// the others. cown is the constructor of the type of the same name, so it is not a name of its own: calling the type
// calls CownNew.
func (b *Builtins) declareSharing() {
	freezeT := newTypeParamTy("T")
	shareT := newTypeParamTy("T")
	thawT := newTypeParamTy("T")
	cownT := newTypeParamTy("T")
	b.declareGeneric(IntrinsicFreeze, "freeze", freezeT, param("x", freezeT, ConvSink), b.FrozenOf(freezeT))
	b.declareGeneric(IntrinsicShare, "share", shareT, param("x", shareT, ConvLet), shareT)
	b.declareGeneric(IntrinsicThaw, "thaw", thawT, param("x", b.FrozenOf(thawT), ConvLet), thawT)
	b.CownNew = b.makeGeneric(IntrinsicCown, "cown", cownT, param("x", cownT, ConvSink), b.CownOf(cownT))
}

func (b *Builtins) declareGeneric(op Intrinsic, name string, t *TypeParamTy, p ParamSpec, ret Ty) {
	b.stdlib.Names[name] = b.makeGeneric(op, name, t, p, ret)
}

func (b *Builtins) makeGeneric(op Intrinsic, name string, t *TypeParamTy, p ParamSpec, ret Ty) *FuncDecl {
	f := NewFuncDecl()
	f.Name = name
	f.Visibility = "public"
	f.Module = b.stdlib
	f.File = "<stdlib>"
	f.Generics = []*TypeParamTy{t}
	f.Instances = &InstanceTable{}
	f.Intrinsic = op
	f.Pass = Passing{Specs: []ParamSpec{p}}
	f.ParamTypes = []Ty{p.Type}
	f.ReturnType = ret
	return f
}

func instantiateBuiltin(d *BuiltinTy, args []Ty) *BuiltinTy {
	v, _ := d.instances.get(args, func() any {
		return &BuiltinTy{NominalBase: d.newInstanceBase(args), Kind: d.Kind, Decl: d.Decl, origin: d, lib: d.lib}
	})
	return v.(*BuiltinTy)
}
