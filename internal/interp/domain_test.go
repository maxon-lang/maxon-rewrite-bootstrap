package interp

import "testing"

// The value of a cown can only be made unreachable by IR that the checker never lets through, so these tests build the
// heap by hand.

func cownWithValue() (cown, inner *object) {
	cown = &object{kind: kindCown, count: 1, holders: map[*cell]bool{}, fields: make([]cell, 1)}
	cown.fields[0].holder = cown
	inner = &object{kind: kindBox, fields: make([]cell, 1)}
	inner.owner = &cown.fields[0]
	cown.fields[0].v = value{ref: inner}
	return cown, inner
}

func reaches(vm *vm, o *object) (violated bool) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(*violationSignal); !ok {
				panic(r)
			}
			violated = true
		}
	}()
	vm.requireReachable(o, "a test")
	return false
}

func TestTheValueOfACownIsReachableOnlyByTheBehaviourThatHoldsIt(t *testing.T) {
	vm := &vm{}
	vm.sched = newSched(0)
	cown, inside := cownWithValue()
	holder := &behaviour{}
	other := &behaviour{}
	cown.running = holder

	vm.cur = &thread{behaviour: holder}
	if reaches(vm, inside) {
		t.Error("the behaviour that holds the cown cannot reach its value")
	}
	vm.cur = &thread{behaviour: other}
	if !reaches(vm, inside) {
		t.Error("a behaviour that does not hold the cown reached its value")
	}
	vm.cur = &thread{name: "main"}
	if !reaches(vm, inside) {
		t.Error("main reached the value of a cown that no behaviour of main's holds")
	}
	cown.running = nil
	vm.cur = &thread{behaviour: holder}
	if !reaches(vm, inside) {
		t.Error("the value of a cown is reachable while no behaviour holds it")
	}
}

func TestWhatAThreadOwnsIsReachableOnlyByThatThread(t *testing.T) {
	vm := &vm{}
	vm.sched = newSched(0)
	mine, theirs := &thread{name: "mine"}, &thread{name: "theirs"}
	vm.cur = mine
	slot := &cell{frame: &frame{thread: mine}}
	owned := &object{kind: kindBox, owner: slot}
	inside := &object{kind: kindBox, owner: &cell{holder: owned}}
	if reaches(vm, owned) || reaches(vm, inside) {
		t.Error("a thread cannot reach what it owns")
	}
	vm.cur = theirs
	if !reaches(vm, owned) || !reaches(vm, inside) {
		t.Error("a thread reached what another thread owns")
	}
}

func TestWhatIsFrozenIsReachableByEveryThread(t *testing.T) {
	vm := &vm{}
	vm.sched = newSched(0)
	frozen := &object{kind: kindFrozen, count: 1, holders: map[*cell]bool{}, fields: make([]cell, 1)}
	frozen.fields[0].holder = frozen
	inside := &object{kind: kindBox, owner: &frozen.fields[0]}
	for _, name := range []string{"main", "behaviour"} {
		vm.cur = &thread{name: name}
		if reaches(vm, inside) {
			t.Errorf("%s cannot read what is frozen", name)
		}
	}
}
