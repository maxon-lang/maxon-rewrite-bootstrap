package ir

import (
	"strings"
	"testing"
)

const dropsFile = "testdata/drops.maxon"

func lowered(t *testing.T, name string) string {
	t.Helper()
	return irText(lowerNamed(t, frontEnd(t, dropsFile), name))
}

// requireInOrder fails unless each fragment occurs after the one before it.
func requireInOrder(t *testing.T, text string, fragments ...string) {
	t.Helper()
	rest := text
	for _, f := range fragments {
		i := strings.Index(rest, f)
		if i < 0 {
			t.Fatalf("%q does not come next in:\n%s", f, text)
		}
		rest = rest[i+len(f):]
	}
}

// A local is dropped where its block ends, a moved one is emptied where it moves, and the drop of an empty slot does
// nothing, so one drop per local serves every path.
func TestLocalsAreDroppedWhereTheirBlocksEnd(t *testing.T) {
	text := lowered(t, "scopes")
	requireInOrder(t, text, "move s1", "dropslot s1 String", "dropslot s0 String", "ret")
	requireInOrder(t, text, "move s0", "store s2,", "dropslot s2 String", "dropslot s0 String", "ret")
	if n := strings.Count(text, "dropslot s0 String"); n != 2 {
		t.Errorf("a is dropped on each of the two ways out, got %d:\n%s", n, text)
	}
}

func TestBreakAndContinueDropTheLocalsOfTheLoopBody(t *testing.T) {
	text := lowered(t, "loops")
	if n := strings.Count(text, "dropslot s1 String"); n != 3 {
		t.Errorf("s is dropped at the end of the body, before the continue and before the break, got %d:\n%s", n, text)
	}
}

func TestAnAssignmentDropsTheOldValueFirst(t *testing.T) {
	text := lowered(t, "assigns")
	requireInOrder(t, text, "dropslot s0 String", "store s0,")
	requireInOrder(t, text, "fieldaddr", "dropat", "setfield")
	if strings.Count(text, "dropat") != 2 {
		t.Errorf("each of the two fields drops its old value:\n%s", text)
	}
}

func TestATemporaryThatNothingTookIsDroppedAtTheEndOfItsStatement(t *testing.T) {
	text := lowered(t, "temporaries")
	requireInOrder(t, text, "call text(", "runtime rt.write(", "drop %", "String")
	requireInOrder(t, text, "call Pair.make(", "drop %", "Pair")
}

// A consumes parameter belongs to the callee, so it lives in a slot of its own, and a parameter the body does not name is
// dropped like the rest.
func TestConsumedParametersAreOwnedByTheCallee(t *testing.T) {
	text := lowered(t, "eat")
	requireInOrder(t, text, "param 0", "store s0,", "param 1", "store s1,")
	requireInOrder(t, text, "dropslot s1 String", "dropslot s0 Pair", "ret")
}

// A match binding of a payload names part of the subject for the arm: it is stored as a borrow and never dropped.
func TestAMatchBindingIsABorrow(t *testing.T) {
	text := lowered(t, "bound")
	if !strings.Contains(text, " borrow") {
		t.Errorf("the binding is stored as a borrow:\n%s", text)
	}
	if strings.Contains(text, "dropslot") {
		t.Errorf("a borrow is never dropped:\n%s", text)
	}
}

func TestScalarsAreNeverDropped(t *testing.T) {
	results := frontEnd(t, "testdata/scalars-are-never-dropped.maxon")
	text := irText(lowerNamed(t, results, "main"))
	if strings.Contains(text, "drop") || strings.Contains(text, "move") {
		t.Errorf("nothing here has an owner:\n%s", text)
	}
}

// A field default is an expression the type's own code makes for each literal, so it is consumed by the field and never
// dropped as a temporary.
func TestAFieldDefaultIsOwnedByItsField(t *testing.T) {
	results := frontEnd(t, "testdata/field-default-is-owned-by-its-field.maxon")
	text := irText(lowerNamed(t, results, "make"))
	if strings.Contains(text, "drop %") {
		t.Errorf("the default array belongs to the field:\n%s", text)
	}
}

const copiesFile = "testdata/copies.maxon"

// A copy is one instruction that names the type, so the backend makes one routine for each type and a type that contains
// itself needs no code written out where it is wanted. A scalar is copied as itself.
func TestACopyIsOneInstructionThatNamesTheType(t *testing.T) {
	text := irText(lowerNamed(t, frontEnd(t, copiesFile), "copies"))
	for _, want := range []string{"= copy %", " String\n", " Tree\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "rt.array.insert(%") && strings.Contains(text, "alloc") {
		t.Errorf("a copy is not written out:\n%s", text)
	}
	if n := strings.Count(text, "= copy %"); n != 4 {
		t.Errorf("the string, the one given to filled, filled's own copies of it and the tree are copied, got %d:\n%s", n, text)
	}
	if strings.Contains(text, "copy %") && strings.Contains(text, "Count\n") && strings.Contains(text, "= copy %2 Count") {
		t.Errorf("a scalar is copied as itself:\n%s", text)
	}
}

// A function that drops a value holding something that only the interpreter has is refused, though it makes none: the
// backend has no routine to free one. The refusal is at the function, and only one.
func TestDroppingAMapIsRefusedLikeMakingOne(t *testing.T) {
	results := frontEnd(t, copiesFile)
	for _, name := range []string{"takesMap", "takesTreeOfPools"} {
		f := lowerNamed(t, results, name)
		if len(f.Refusals) != 1 || f.Refusals[0].Code != "E9900" {
			t.Errorf("%s: refusals %+v", name, f.Refusals)
		}
	}
	if f := lowerNamed(t, results, "copies"); len(f.Refusals) != 0 {
		t.Errorf("copies: refusals %+v", f.Refusals)
	}
}
