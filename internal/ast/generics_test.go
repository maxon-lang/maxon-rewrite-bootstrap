package ast

import (
	"sync"
	"testing"
)

func newBox() *RecordTy {
	decl := &TypeDecl{TypeParams: &TypeParamList{Params: []*TypeParam{{Name: "T"}}}}
	decl.Name = "Box"
	return NewRecordTy(decl)
}

func TestSameArgumentsGiveTheSameInstance(t *testing.T) {
	box := newBox()
	a := Instantiate(box, []Ty{StringTy})
	b := Instantiate(box, []Ty{StringTy})
	c := Instantiate(box, []Ty{BoolTy})
	if a != b {
		t.Fatal("two instances for the same arguments")
	}
	if a == c {
		t.Fatal("one instance for different arguments")
	}
	if a.Name() != "Box[String]" {
		t.Fatalf("name %q", a.Name())
	}
}

func TestInstancesAreInternedAcrossGoroutines(t *testing.T) {
	box := newBox()
	const workers = 16
	got := make([]NominalTy, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() { got[i] = Instantiate(box, []Ty{StringTy}) })
	}
	wg.Wait()
	for i := 1; i < workers; i++ {
		if got[i] != got[0] {
			t.Fatalf("goroutine %d got another instance", i)
		}
	}
}

func TestSubstReplacesParametersInsideInstances(t *testing.T) {
	box := newBox()
	param := box.Params[0]
	inner := Instantiate(box, []Ty{param})
	nested := Instantiate(box, []Ty{inner})
	got := subst(nested, box.Params, []Ty{StringTy})
	want := Instantiate(box, []Ty{Instantiate(box, []Ty{StringTy})})
	if got != want {
		t.Fatalf("got %s, want %s", got.Name(), want.Name())
	}
	if same := subst(StringTy, box.Params, []Ty{BoolTy}); same != StringTy {
		t.Fatalf("a type with no parameter changed to %s", same.Name())
	}
}

func TestSelfTypeOfAGenericIsItsOwnInstance(t *testing.T) {
	box := newBox()
	self := SelfType(box)
	if self == NominalTy(box) {
		t.Fatal("the declaration is not a type a body can name")
	}
	if args := self.Base().Args; len(args) != 1 || args[0] != Ty(box.Params[0]) {
		t.Fatalf("self type has arguments %v", args)
	}
	if again := SelfType(self); again != self {
		t.Fatal("the self type of an instance is itself")
	}
}

func TestDepthOfNestedInstances(t *testing.T) {
	box := newBox()
	open := Instantiate(box, []Ty{box.Params[0]})
	closed := Instantiate(box, []Ty{Instantiate(box, []Ty{StringTy})})
	if TypeDepth(open) != 1 || TypeDepth(closed) != 2 || TypeDepth(StringTy) != 0 {
		t.Fatalf("depths %d %d %d", TypeDepth(open), TypeDepth(closed), TypeDepth(StringTy))
	}
}
