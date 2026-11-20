package ast

import (
	"sync"
	"testing"
)

func newProject() *Project { return NewProject(NewModule("", ".", false)) }

func TestEveryProjectDeclaresTheBuiltins(t *testing.T) {
	p := newProject()
	for _, name := range []string{"array", "map", "list", "pool", "Handle", "Option"} {
		d, ok := p.Stdlib.Names[name].(TypeDeclNode)
		if !ok || d.TypeBase().Ty == nil || d.Base().Module != p.Stdlib || !d.Base().IsExported() {
			t.Errorf("stdlib does not declare %s as a type: %v", name, p.Stdlib.Names[name])
		}
	}
	other := newProject()
	if p.Builtins.Array == other.Builtins.Array || p.Builtins.Option == other.Builtins.Option {
		t.Error("two projects share a declaration, so their instances would share a lock and a table")
	}
}

func TestPrimitiveInstancesAreInterned(t *testing.T) {
	b := newProject().Builtins
	a := Instantiate(b.Array, []Ty{StringTy})
	if a != Instantiate(b.Array, []Ty{StringTy}) || a == Instantiate(b.Array, []Ty{BoolTy}) {
		t.Fatal("instances are not interned by their arguments")
	}
	if a.Name() != "array with String" || b.HandleOf(BoolTy).Name() != "Handle with bool" || b.OptionOf(StringTy).Name() != "Option with String" {
		t.Fatalf("names %s %s %s", a.Name(), b.HandleOf(BoolTy).Name(), b.OptionOf(StringTy).Name())
	}
	const workers = 16
	got := make([]NominalTy, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() { got[i] = Instantiate(b.Map, []Ty{StringTy, BoolTy}) })
	}
	wg.Wait()
	for i := 1; i < workers; i++ {
		if got[i] != got[0] {
			t.Fatal("goroutines made different instances")
		}
	}
}

func TestWhatAPrimitiveHoldsAndWhatIndexesIt(t *testing.T) {
	b := newProject().Builtins
	array := Instantiate(b.Array, []Ty{StringTy}).(*BuiltinTy)
	m := Instantiate(b.Map, []Ty{BoolTy, StringTy}).(*BuiltinTy)
	pool := Instantiate(b.Pool, []Ty{BoolTy}).(*BuiltinTy)
	list := Instantiate(b.List, []Ty{BoolTy}).(*BuiltinTy)
	if array.Element() != StringTy || array.Index() != Ty(b.Count) {
		t.Errorf("array with String holds %s by %s", array.Element().Name(), array.Index().Name())
	}
	if m.Element() != StringTy || m.Index() != BoolTy {
		t.Errorf("map[bool, String] holds %s by %s", m.Element().Name(), m.Index().Name())
	}
	if pool.Index() != Ty(b.HandleOf(BoolTy)) || list.Index() != Ty(b.HandleOf(BoolTy)) {
		t.Errorf("a pool and a list are indexed by %s and %s", pool.Index().Name(), list.Index().Name())
	}
	if b.HandleOf(BoolTy).Subscriptable() {
		t.Error("a handle has no elements")
	}
}

func TestOnlyAHandleIsAScalar(t *testing.T) {
	b := newProject().Builtins
	for _, kind := range []*BuiltinTy{b.Array, b.Map, b.List, b.Pool} {
		if !kind.IsManaged() {
			t.Errorf("%s owns its elements, so it is managed", kind.Name())
		}
	}
	if b.HandleOf(StringTy).IsManaged() {
		t.Error("a handle is a key, and owns nothing")
	}
}

func TestSubstitutionReachesTheArgumentsOfAPrimitive(t *testing.T) {
	b := newProject().Builtins
	param := b.Array.Params[0]
	nested := Instantiate(b.Map, []Ty{StringTy, Instantiate(b.Array, []Ty{param})})
	got := Binding{Params: []*TypeParamTy{param}, Args: []Ty{BoolTy}}.Apply(nested)
	want := Instantiate(b.Map, []Ty{StringTy, Instantiate(b.Array, []Ty{BoolTy})})
	if got != want {
		t.Errorf("got %s, want %s", got.Name(), want.Name())
	}
	if TypeDepth(nested) != 2 {
		t.Errorf("depth of %s is %d", nested.Name(), TypeDepth(nested))
	}
}

func TestWhichTypesMayBeMapKeys(t *testing.T) {
	p := newProject()
	b := p.Builtins
	enumDecl := &EnumDecl{}
	enumDecl.Module = p.Root
	enum := NewEnumTy(enumDecl)
	for _, ty := range []Ty{b.Count, BoolTy, StringTy, enum, b.HandleOf(StringTy)} {
		if !IsKey(ty) {
			t.Errorf("%s should be a key", ty.Name())
		}
	}
	for _, ty := range []Ty{Anonymous64, Instantiate(b.Array, []Ty{BoolTy}), b.OptionOf(BoolTy), b.Array.Params[0]} {
		if IsKey(ty) {
			t.Errorf("%s should not be a key", ty.Name())
		}
	}
}

func TestEveryMethodIsAnIntrinsicOfItsOwner(t *testing.T) {
	b := newProject().Builtins
	for _, owner := range []NominalTy{b.Array, b.Map, b.List, b.Pool, b.Option} {
		if len(owner.MethodTable()) == 0 {
			t.Errorf("%s has no methods", owner.Name())
		}
		for name, m := range owner.MethodTable() {
			if m.Intrinsic == NotIntrinsic || m.Name != name || len(m.Generics) != len(owner.Base().Params) || m.Module != b.stdlib {
				t.Errorf("%s.%s is not declared as an intrinsic of its owner", owner.Name(), name)
			}
		}
	}
	if len(b.Handle.MethodTable()) != 0 {
		t.Error("a handle has no methods")
	}
}

func TestFrozenValuesAndCownsAreCountedTypes(t *testing.T) {
	b := newProject().Builtins
	frozen := b.FrozenOf(StringTy)
	cown := b.CownOf(BoolTy)
	if frozen != b.FrozenOf(StringTy) || frozen.Name() != "frozen with String" || cown.Name() != "cown with bool" {
		t.Fatalf("names %s %s", frozen.Name(), cown.Name())
	}
	for _, shared := range []*BuiltinTy{frozen, cown} {
		if !shared.IsManaged() || !shared.Shared() || shared.Subscriptable() {
			t.Errorf("%s is counted, managed and has no elements", shared.Name())
		}
	}
	if frozen.Held() != StringTy || cown.Held() != BoolTy {
		t.Errorf("what they hold: %s %s", frozen.Held().Name(), cown.Held().Name())
	}
	if IsKey(frozen) || IsKey(cown) {
		t.Error("a frozen value and a cown are not map keys")
	}
}

func TestContentSeesThroughEveryFrozenValue(t *testing.T) {
	b := newProject().Builtins
	twice := b.FrozenOf(b.FrozenOf(StringTy))
	if Content(twice) != Ty(StringTy) || Content(StringTy) != Ty(StringTy) {
		t.Errorf("content of %s is %s", twice.Name(), Content(twice).Name())
	}
	if Content(b.CownOf(StringTy)) != Ty(b.CownOf(StringTy)) {
		t.Error("a cown's value is not content that a reference reaches")
	}
}

func TestTheSharingFunctionsAreDeclaredInTheStdlib(t *testing.T) {
	p := newProject()
	for _, name := range []string{"freeze", "share", "thaw"} {
		f, ok := p.Stdlib.Names[name].(*FuncDecl)
		if !ok || f.Intrinsic == NotIntrinsic || len(f.Generics) != 1 || f.Module != p.Stdlib {
			t.Errorf("stdlib does not declare %s as a generic intrinsic: %v", name, p.Stdlib.Names[name])
		}
	}
	// cown is the constructor of the type that has its name, so the name stays the type's.
	if _, isType := p.Stdlib.Names["cown"].(TypeDeclNode); !isType || p.Builtins.CownNew.Intrinsic != IntrinsicCown {
		t.Errorf("cown names %v, and constructs with %v", p.Stdlib.Names["cown"], p.Builtins.CownNew)
	}
}
