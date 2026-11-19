package interp_test

import (
	"strings"
	"testing"

	"minimaxon/internal/ast"
	"minimaxon/internal/interp"
	"minimaxon/internal/ir"
)

// The programs here are written as IR for the same reason as the ones in interp_test.go: the checker refuses each of the
// ones that break a rule, and the interpreter has to catch it. The others show how behaviours are scheduled.

func (b *builder) text(s string) ir.V {
	d := b.reg(ir.KindManaged)
	b.emit(&ir.StrInstr{Dst: d, Value: s})
	return d
}

func (b *builder) write(s string) {
	handle := b.reg(ir.KindScalar)
	b.emit(&ir.LoadGlobal{Dst: handle, Symbol: ir.RtStdout})
	b.emit(&ir.CallRuntimeInstr{Symbol: ir.RtWrite, Args: []ir.V{handle, b.text(s)}})
}

// cown makes a cown that owns a new box, and gives the register that holds the reference.
func (b *builder) cown(words int) ir.V {
	return b.runtime(ir.RtCownNew, ir.KindManaged, b.alloc(words))
}

func (b *builder) drop(v ir.V) { b.emit(&ir.DropInstr{Src: v, Ty: ast.StringTy}) }

func (b *builder) when(body *ir.IrFunction, cowns []ir.V, captures ...ir.V) {
	b.emit(&ir.WhenInstr{Body: body, Cowns: cowns, Captures: captures})
}

// writer is a behaviour on one cown that writes a word to standard output.
func writer(name, word string) *ir.IrFunction {
	b, _ := newFunction(name, ast.ConvInout)
	b.write(word)
	return b.ret(b.konst(0))
}

func runSeed(main *ir.IrFunction, seed int64, others ...*ir.IrFunction) *interp.Result {
	fns := append([]*ir.IrFunction{main}, others...)
	return interp.RunWith(interp.NewProgram(fns, main.Decl), interp.Options{StepLimit: interp.DefaultStepLimit, Seed: seed})
}

const seedsToTry = 30

// holdsItAWhile is how many instructions main runs while it owns what a behaviour reads.
const holdsItAWhile = 200

func TestABehaviourRunsAfterMainHasReturnedAndMainsExitCodeIsKept(t *testing.T) {
	body := writer("body", "ran;")
	for seed := range int64(seedsToTry) {
		b, _ := newFunction("main")
		c := b.cown(1)
		b.when(body, []ir.V{c})
		b.drop(c)
		r := runSeed(b.ret(b.konst(7)), seed)
		if len(r.Violations) != 0 || r.ExitCode != 7 || string(r.Stdout) != "ran;" || r.Behaviours != 1 {
			t.Fatalf("seed %d: exit %d, %v, %q, %d behaviours", seed, r.ExitCode, r.Violations, r.Stdout, r.Behaviours)
		}
	}
}

// Behaviours on one cown run in the order they were scheduled, whatever the interleaving.
func TestBehavioursOnOneCownRunInTheOrderTheyWereScheduled(t *testing.T) {
	first, second, third := writer("first", "1"), writer("second", "2"), writer("third", "3")
	for seed := range int64(seedsToTry) {
		b, _ := newFunction("main")
		c := b.cown(1)
		b.when(first, []ir.V{c})
		b.when(second, []ir.V{c})
		b.when(third, []ir.V{c})
		b.drop(c)
		r := runSeed(b.ret(b.konst(0)), seed)
		if len(r.Violations) != 0 || string(r.Stdout) != "123" {
			t.Fatalf("seed %d: %v, %q", seed, r.Violations, r.Stdout)
		}
	}
}

// Behaviours on disjoint cowns have no order, so the seeds between them find both.
func TestBehavioursOnDisjointCownsRunInEitherOrder(t *testing.T) {
	left, right := writer("left", "L"), writer("right", "R")
	seen := map[string]bool{}
	for seed := range int64(seedsToTry) {
		b, _ := newFunction("main")
		a, c := b.cown(1), b.cown(1)
		b.when(left, []ir.V{a})
		b.when(right, []ir.V{c})
		b.drop(a)
		b.drop(c)
		r := runSeed(b.ret(b.konst(0)), seed)
		if len(r.Violations) != 0 {
			t.Fatalf("seed %d: %v", seed, r.Violations[0])
		}
		seen[string(r.Stdout)] = true
	}
	if !seen["LR"] || !seen["RL"] || len(seen) != 2 {
		t.Fatalf("the schedules gave %v, wanted both orders", seen)
	}
}

// A behaviour that names two cowns waits for both, so it runs after the earlier behaviours on either one.
func TestABehaviourOnTwoCownsWaitsForEarlierOnesOnEither(t *testing.T) {
	onA, onB := writer("onA", "A"), writer("onB", "B")
	both, _ := newFunction("both", ast.ConvInout, ast.ConvInout)
	both.write("+")
	bothFn := both.ret(both.konst(0))
	for seed := range int64(seedsToTry) {
		b, _ := newFunction("main")
		a, c := b.cown(1), b.cown(1)
		b.when(onA, []ir.V{a})
		b.when(onB, []ir.V{c})
		b.when(bothFn, []ir.V{a, c})
		b.when(onA, []ir.V{a})
		b.drop(a)
		b.drop(c)
		r := runSeed(b.ret(b.konst(0)), seed)
		out := string(r.Stdout)
		if len(r.Violations) != 0 || !(out == "AB+A" || out == "BA+A") {
			t.Fatalf("seed %d: %v, %q", seed, r.Violations, out)
		}
	}
}

func TestTheSameSeedGivesTheSameSchedule(t *testing.T) {
	left, right := writer("left", "L"), writer("right", "R")
	build := func() *ir.IrFunction {
		b, _ := newFunction("main")
		a, c := b.cown(1), b.cown(1)
		b.when(left, []ir.V{a})
		b.when(right, []ir.V{c})
		b.drop(a)
		b.drop(c)
		return b.ret(b.konst(0))
	}
	for seed := range int64(seedsToTry) {
		first, second := runSeed(build(), seed), runSeed(build(), seed)
		if string(first.Stdout) != string(second.Stdout) || first.Steps != second.Steps {
			t.Fatalf("seed %d gave %q and %q", seed, first.Stdout, second.Stdout)
		}
	}
}

func TestABehaviourThatNamesOneCownTwiceTraps(t *testing.T) {
	body, _ := newFunction("body", ast.ConvInout, ast.ConvInout)
	b, _ := newFunction("main")
	c := b.cown(1)
	b.when(body.ret(body.konst(0)), []ir.V{c, c})
	r := runSeed(b.ret(b.konst(0)), 0)
	if r.ExitCode != 1 || len(r.Violations) != 0 || !strings.Contains(string(r.Stderr), "names the same cown twice") {
		t.Fatalf("exit %d, %v, %q", r.ExitCode, r.Violations, r.Stderr)
	}
}

// A behaviour that was handed a value which its scheduler still owns shares it: that is the data race that the capture
// rules of the checker rule out.
func TestACapturedValueThatTheSchedulerStillOwns(t *testing.T) {
	body, _ := newFunction("body", ast.ConvInout, ast.ConvLet)
	b, _ := newFunction("main")
	c := b.cown(1)
	s := b.slot(ir.KindManaged)
	b.store(s, b.alloc(1))
	b.when(body.ret(body.konst(0)), []ir.V{c}, b.load(s))
	r := runSeed(b.ret(b.konst(0)), 0)
	expectViolation(t, r, interp.Race, "captures", "still owns")
}

// A value that was in flight when the behaviour was scheduled and then became main's belongs to main: the behaviour that
// reads it, whenever it runs after that, races with main.
func TestAThreadReadingWhatAnotherThreadOwns(t *testing.T) {
	body, _ := newFunction("body", ast.ConvInout, ast.ConvLet)
	box := body.param(1, ir.KindManaged)
	field := body.reg(ir.KindScalar)
	body.emit(&ir.LoadField{Dst: field, Obj: box})
	bodyFn := body.ret(body.konst(0))
	raced := 0
	for seed := range int64(seedsToTry) {
		b, _ := newFunction("main")
		c := b.cown(1)
		s := b.slot(ir.KindManaged)
		p := b.alloc(1)
		b.when(bodyFn, []ir.V{c}, p)
		b.store(s, p)
		for range holdsItAWhile {
			b.konst(0)
		}
		b.emit(&ir.DropSlotInstr{Slot: s, Ty: ast.StringTy})
		b.drop(c)
		r := runSeed(b.ret(b.konst(0)), seed)
		for _, v := range r.Violations {
			if v.Kind == interp.Race && strings.Contains(v.Message, "belongs to the thread") {
				raced++
			}
		}
	}
	if raced == 0 {
		t.Fatal("no schedule of the program showed the race")
	}
}

func TestChangingAValueThatIsPartOfAFrozenValue(t *testing.T) {
	b, _ := newFunction("main")
	box := b.alloc(1)
	frozen := b.runtime(ir.RtFrozenFreeze, ir.KindManaged, box)
	inner := b.runtime(ir.RtFrozenGet, ir.KindManaged, frozen)
	b.emit(&ir.StoreField{Obj: inner, Src: b.konst(9)})
	expectViolation(t, run(b.ret(b.konst(0))), interp.Immutability, "part of a frozen value")
}

func TestReadingAFrozenValueIsFine(t *testing.T) {
	b, _ := newFunction("main")
	box := b.alloc(1)
	frozen := b.runtime(ir.RtFrozenFreeze, ir.KindManaged, box)
	inner := b.runtime(ir.RtFrozenGet, ir.KindManaged, frozen)
	field := b.reg(ir.KindScalar)
	b.emit(&ir.LoadField{Dst: field, Obj: inner})
	b.drop(frozen)
	r := run(b.ret(b.konst(0)))
	if len(r.Violations) != 0 {
		t.Fatal(r.Violations[0])
	}
}

// One count for a frozen value, however many objects it holds: the value is freed with its last owner.
func TestAFrozenValueIsFreedWithItsLastOwner(t *testing.T) {
	b, _ := newFunction("main")
	outer, inner := b.alloc(1), b.alloc(1)
	b.emit(&ir.StoreField{Obj: outer, Src: inner})
	frozen := b.runtime(ir.RtFrozenFreeze, ir.KindManaged, outer)
	other := b.runtime(ir.RtShare, ir.KindManaged, frozen)
	b.drop(frozen)
	b.emit(&ir.DropInstr{Src: other, Ty: ast.StringTy})
	r := run(b.ret(b.konst(0)))
	if len(r.Violations) != 0 {
		t.Fatal(r.Violations[0])
	}
	want := []string{"frozen value", "record", "record"}
	if len(r.Frees) != len(want) {
		t.Fatalf("freed %v, want %v", r.Frees, want)
	}
	for i := range want {
		if r.Frees[i] != want[i] {
			t.Fatalf("freed %v, want %v", r.Frees, want)
		}
	}
}

func TestAFrozenValueIsStillThereWhileAnOwnerHoldsIt(t *testing.T) {
	b, _ := newFunction("main")
	box := b.alloc(1)
	frozen := b.runtime(ir.RtFrozenFreeze, ir.KindManaged, box)
	other := b.runtime(ir.RtShare, ir.KindManaged, frozen)
	b.drop(frozen)
	inner := b.runtime(ir.RtFrozenGet, ir.KindManaged, other)
	field := b.reg(ir.KindScalar)
	b.emit(&ir.LoadField{Dst: field, Obj: inner})
	b.drop(other)
	if r := run(b.ret(b.konst(0))); len(r.Violations) != 0 {
		t.Fatal(r.Violations[0])
	}
}

func TestADroppedFrozenValueThatAPlaceStillHolds(t *testing.T) {
	b, _ := newFunction("main")
	frozen := b.runtime(ir.RtFrozenFreeze, ir.KindManaged, b.alloc(1))
	s := b.slot(ir.KindManaged)
	b.store(s, frozen)
	b.drop(frozen)
	expectViolation(t, run(b.ret(b.konst(0))), interp.Count, "still referenced")
}

func TestAFrozenValueThatNoOneDropsLeaks(t *testing.T) {
	b, _ := newFunction("main")
	b.runtime(ir.RtFrozenFreeze, ir.KindManaged, b.alloc(1))
	expectViolation(t, run(b.ret(b.konst(0))), interp.Leak, "frozen value")
}

func TestACownThatNoOneDropsLeaks(t *testing.T) {
	b, _ := newFunction("main")
	b.cown(1)
	expectViolation(t, run(b.ret(b.konst(0))), interp.Leak, "cown")
}

func TestDroppingAFrozenValueOnceTooOften(t *testing.T) {
	b, _ := newFunction("main")
	frozen := b.runtime(ir.RtFrozenFreeze, ir.KindManaged, b.alloc(1))
	b.drop(frozen)
	b.drop(frozen)
	expectViolation(t, run(b.ret(b.konst(0))), interp.DoubleFree)
}

// A cown is freed when its last reference is dropped and no behaviour holds it: a behaviour that is still waiting keeps
// it, and the value it owns, alive.
func TestABehaviourKeepsItsCownAliveAfterEveryReferenceIsDropped(t *testing.T) {
	body, _ := newFunction("body", ast.ConvInout)
	addr := body.param(0, ir.KindScalar)
	value := body.reg(ir.KindManaged)
	body.emit(&ir.LoadAt{Dst: value, Addr: addr})
	field := body.reg(ir.KindScalar)
	body.emit(&ir.LoadField{Dst: field, Obj: value})
	bodyFn := body.ret(body.konst(0))
	for seed := range int64(seedsToTry) {
		b, _ := newFunction("main")
		c := b.cown(1)
		b.when(bodyFn, []ir.V{c})
		b.drop(c)
		r := runSeed(b.ret(b.konst(0)), seed)
		if len(r.Violations) != 0 {
			t.Fatalf("seed %d: %s", seed, r.Violations[0])
		}
	}
}

// A behaviour owns what it captures, and frees it when it ends.
func TestACapturedValueIsFreedByTheBehaviour(t *testing.T) {
	body, _ := newFunction("body", ast.ConvInout, ast.ConvSink)
	slot := body.slot(ir.KindManaged)
	body.store(slot, body.param(1, ir.KindManaged))
	body.emit(&ir.DropSlotInstr{Slot: slot, Ty: ast.StringTy})
	bodyFn := body.ret(body.konst(0))
	for seed := range int64(seedsToTry) {
		b, _ := newFunction("main")
		c := b.cown(1)
		b.when(bodyFn, []ir.V{c}, b.alloc(2))
		b.drop(c)
		r := runSeed(b.ret(b.konst(0)), seed)
		if len(r.Violations) != 0 {
			t.Fatalf("seed %d: %s", seed, r.Violations[0])
		}
	}
}

func TestACapturedValueThatTheBehaviourNeverFrees(t *testing.T) {
	body, _ := newFunction("body", ast.ConvInout, ast.ConvSink)
	slot := body.slot(ir.KindManaged)
	body.store(slot, body.param(1, ir.KindManaged))
	bodyFn := body.ret(body.konst(0))
	b, _ := newFunction("main")
	c := b.cown(1)
	b.when(bodyFn, []ir.V{c}, b.alloc(2))
	b.drop(c)
	expectViolation(t, runSeed(b.ret(b.konst(0)), 0), interp.Leak, "slot s0 still owns")
}
