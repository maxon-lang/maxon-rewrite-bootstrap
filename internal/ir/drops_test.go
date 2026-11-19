package ir

import (
	"strings"
	"testing"
)

const dropSource = `type Pair
	export var left as String
	export var right as String
	static function make(left sink String, right sink String) returns Pair
		return Pair{left: left, right: right}
	end 'make'
end 'Pair'

function text(n Count) returns String
	return "{n}!"
end 'text'

function eat(p sink Pair, _ sink String) returns Count
	return 1
end 'eat'

function scopes(flag bool) returns String
	let a = text(1)
	if flag 'f'
		let b = text(2)
		return b
	end 'f'
	let c = a
	return text(3)
end 'scopes'

function loops(n Count)
	for i in 0 upto n 'l'
		let s = text(i)
		if i == 1 'c'
			continue
		end 'c'
		if i == 2 'b'
			break
		end 'b'
	end 'l'
end 'loops'

function assigns(p inout Pair)
	var local = text(1)
	local = text(2)
	p.left = local
	p.right = text(3)
end 'assigns'

function temporaries() returns Count
	print("{text(1)}\n")
	_ = Pair.make("a", right: "b")
	print(text(2))
	return 1
end 'temporaries'

function unwrapped(o sink Option[String]) returns String
	return o.unwrap()
end 'unwrapped'

function bound(o Option[String]) returns Count
	match o 'm'
		some(v) then print(v)
		none then print("none")
	end 'm'
	return 0
end 'bound'

function main() returns ExitCode
	return 0
end 'main'
`

func lowered(t *testing.T, name string) string {
	t.Helper()
	return irText(lowerNamed(t, frontEnd(t, dropSource), name))
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

// A sink parameter belongs to the callee, so it lives in a slot of its own, and a parameter the body does not name is
// dropped like the rest.
func TestSinkParametersAreOwnedByTheCallee(t *testing.T) {
	text := lowered(t, "eat")
	requireInOrder(t, text, "param 0", "store s0,", "param 1", "store s1,")
	requireInOrder(t, text, "dropslot s1 String", "dropslot s0 Pair", "ret")
}

// The box of an Option is dropped once its payload has left it, and a payload that is owned moves out by address.
func TestUnwrapMovesThePayloadOutAndDropsTheBox(t *testing.T) {
	text := lowered(t, "unwrapped")
	requireInOrder(t, text, "fieldaddr", "moveat", "drop %", "Option[String]")
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
	m := frontEnd(t, `function main() returns ExitCode
	var n = 0 as Count
	let b = true
	n = n + 1
	return 0
end 'main'
`)
	text := irText(lowerNamed(t, m, "main"))
	if strings.Contains(text, "drop") || strings.Contains(text, "move") {
		t.Errorf("nothing here has an owner:\n%s", text)
	}
}

// A field default is an expression the type's own code makes for each literal, so it is consumed by the field and never
// dropped as a temporary.
func TestAFieldDefaultIsOwnedByItsField(t *testing.T) {
	m := frontEnd(t, `type Stack
	var items = array[Count].new()
	static function make() returns Stack
		return Stack{}
	end 'make'
end 'Stack'

function main() returns ExitCode
	return 0
end 'main'
`)
	text := irText(lowerNamed(t, m, "make"))
	if strings.Contains(text, "drop %") {
		t.Errorf("the default array belongs to the field:\n%s", text)
	}
}

const copySource = `type Tree
	var value as Count
	var name as String
	var kids = array[Tree].new()
	static function make() returns Tree
		return Tree{value: 1, name: "n"}
	end 'make'
end 'Tree'

function copies(t Tree, s String, n Count) returns Tree
	let a = s.copy()
	let b = n.copy()
	let c = array[String].filled(3, value: s.copy())
	return t.copy()
end 'copies'

function takesMap(m sink map[Count, Count]) returns Count
	return 0
end 'takesMap'

function takesTreeOfPools(p sink pool[Count], t Tree) returns Count
	return 0
end 'takesTreeOfPools'

function main() returns ExitCode
	return 0
end 'main'
`

// A copy is one instruction that names the type, so the backend makes one routine for each type and a type that contains
// itself needs no code written out where it is wanted. A scalar is copied as itself.
func TestACopyIsOneInstructionThatNamesTheType(t *testing.T) {
	text := irText(lowerNamed(t, frontEnd(t, copySource), "copies"))
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
	m := frontEnd(t, copySource)
	for _, name := range []string{"takesMap", "takesTreeOfPools"} {
		f := lowerNamed(t, m, name)
		if len(f.Refusals) != 1 || f.Refusals[0].Code != "E9002" {
			t.Errorf("%s: refusals %+v", name, f.Refusals)
		}
	}
	if f := lowerNamed(t, m, "copies"); len(f.Refusals) != 0 {
		t.Errorf("copies: refusals %+v", f.Refusals)
	}
}
