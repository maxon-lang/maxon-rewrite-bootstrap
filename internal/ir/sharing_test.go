package ir

import (
	"strings"
	"testing"

	"minimaxon/internal/diag"
)

const sharingSource = `type Doc
	export var title as String
	export var n as Count
	static function make(title sink String) returns Doc
		return Doc{title: title, n: 1}
	end 'make'
	function read() returns Count
		return n
	end 'read'
end 'Doc'

function glance(d Doc) returns Count
	return d.n
end 'glance'

function frozenFields(f frozen[Doc]) returns Count
	print(f.title)
	return f.n + f.read() + glance(f)
end 'frozenFields'

function sharedAndThawed(f frozen[Doc]) returns Doc
	let other = share(f)
	return thaw(other)
end 'sharedAndThawed'

function copied(f frozen[Doc], c cown[Doc]) returns frozen[Doc]
	let d = c.copy()
	return f.copy()
end 'copied'

function schedule(c cown[Doc], note sink Doc, n Count)
	when c as x 'run'
		x.n = note.n + n
	end 'run'
end 'schedule'

function nested(c cown[Doc], d cown[Doc])
	let mine = share(d)
	when c as x 'outer'
		when mine as y 'inner'
			y.n = 1
		end 'inner'
	end 'outer'
end 'nested'

function filled(c cown[Doc]) returns array[cown[Doc]]
	return array[cown[Doc]].filled(2, value: share(c))
end 'filled'

function main() returns ExitCode
	return 0
end 'main'
`

func TestAFrozenValueIsReadThroughItsContents(t *testing.T) {
	text := irText(lowerNamed(t, frontEnd(t, sharingSource), "frozenFields"))
	// The field of the print, the field of the sum, the method receiver and the argument that a let parameter borrows each
	// see through the frozen value.
	if n := strings.Count(text, "runtime rt.frozen.get("); n != 4 {
		t.Errorf("want 4 reads of the contents, got %d:\n%s", n, text)
	}
	requireInOrder(t, text, "rt.frozen.get(", "field", "rt.write")
}

func TestShareAndThawLowerToTheirEntries(t *testing.T) {
	text := irText(lowerNamed(t, frontEnd(t, sharingSource), "sharedAndThawed"))
	requireInOrder(t, text, "runtime rt.share(", "store s0,", "rt.frozen.get(", "= copy %3 Doc")
	// The share is another owner, so it is dropped like one.
	requireInOrder(t, text, "dropslot s0 frozen[Doc]")
}

func TestCopyOfASharedValueIsAnotherOwner(t *testing.T) {
	text := irText(lowerNamed(t, frontEnd(t, sharingSource), "copied"))
	for _, want := range []string{"= copy %0 cown[Doc]", "= copy %2 frozen[Doc]"} {
		if !strings.Contains(text, want) {
			t.Errorf("a copy of a frozen value and of a cown reference is a copy instruction, missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "alloc") {
		t.Errorf("a copy of a shared value allocates nothing:\n%s", text)
	}
}

// A behaviour's body is a function of its own: the cowns come first as inout parameters, then what it captured, which it
// owns like a sink parameter.
func TestABehaviourBodyIsAFunctionOfItsOwn(t *testing.T) {
	f := lowerNamed(t, frontEnd(t, sharingSource), "schedule")
	text := irText(f)
	requireInOrder(t, text, "function schedule params 3", "when schedule$when0 cowns [%", "] captures [%", "function schedule$when0 params 3")
	if len(f.Behaviours) != 1 || f.Behaviours[0].ParamCount != 3 {
		t.Fatalf("behaviours: %+v", f.Behaviours)
	}
	body := irText(f.Behaviours[0])
	requireInOrder(t, body, "param 1", "store s0,", "param 0", "loadat", "dropslot s0 Doc")
	// The cowns are only read, and the captured value moves.
	requireInOrder(t, text, "move s0")
}

func TestBehavioursNestAndAreNamedByTheirPlace(t *testing.T) {
	f := lowerNamed(t, frontEnd(t, sharingSource), "nested")
	if len(f.Behaviours) != 1 || len(f.Behaviours[0].Behaviours) != 1 {
		t.Fatalf("a behaviour inside a behaviour belongs to the outer one: %+v", f.Behaviours)
	}
	text := irText(f)
	requireInOrder(t, text, "when nested$when0", "function nested$when0", "when nested$when0$when0", "function nested$when0$when0")
}

// Nothing here has native code, so each function that holds one is refused where the first is, and the others stay
// clean.
func TestSharingIsRefusedNatively(t *testing.T) {
	m := frontEnd(t, sharingSource)
	for _, name := range []string{"frozenFields", "sharedAndThawed", "copied", "schedule", "nested"} {
		f := lowerNamed(t, m, name)
		if len(f.Refusals) == 0 || f.Refusals[0].Code != diag.ErrInterpreterOnly {
			t.Errorf("%s: refusals %v", name, f.Refusals)
		}
	}
	if f := lowerNamed(t, m, "glance"); len(f.Refusals) != 0 {
		t.Errorf("a function that shares nothing is not refused: %v", f.Refusals)
	}
}

// Each refusal names the place that needs the missing native code, including a copy that no `copy()` wrote: an array filled
// with a cown reference copies it for each element.
func TestEveryRefusalNamesItsSite(t *testing.T) {
	f := lowerNamed(t, frontEnd(t, sharingSource), "filled")
	if len(f.Refusals) == 0 {
		t.Fatal("filling an array with a cown reference is not refused")
	}
	for _, r := range f.Refusals {
		if r.Site == nil || r.Code != diag.ErrInterpreterOnly {
			t.Errorf("refusal %+v", r)
		}
	}
}
