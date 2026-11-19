package check

import (
	"testing"

	"minimaxon/internal/diag"
)

// Each program here was accepted once, and broke the memory model when it ran under the interpreter, so each is refused
// now: these are the holes that the tracking interpreter and the program generator found.

const holeTypes = `type Label
	export var text as String
	static function make(text sink String) returns Label
		return Label{text: text}
	end 'make'
	function resetOwn(inout self)
		self.text = "x"
	end 'resetOwn'
	function again(inout self)
		self.resetOwn()
		replaceText(self.text)
	end 'again'
end 'Label'

function replaceText(t inout String)
	t = "z"
end 'replaceText'

function fill(out set String, from String)
	out = from.copy()
end 'fill'

`

func checkHole(t *testing.T, body string) []*diag.CompileError {
	t.Helper()
	return checkSource(t, holeTypes+"function main() returns ExitCode\n"+body+"\treturn 0\nend 'main'\n")
}

// A field of a temporary is not a place that can be moved out of: the temporary is dropped when its statement ends, and
// the field would have two owners.
func TestAFieldOfATemporaryCannotBeMovedOut(t *testing.T) {
	expectCodes(t, checkHole(t, "\tlet s = Label.make(\"a\").text\n\tprint(s)\n"), diag.ErrMoveOutOfField)
	expectCodes(t, checkHole(t, "\tlet s = Label.make(\"a\").text.copy()\n\tprint(s)\n"))
}

// self is the receiver, a pointer that the caller still owns: an inout parameter could assign the whole value, which
// self cannot take. A field of it can be passed, and a method called on it (holeTypes does both).
func TestSelfIsNotAnInoutArgument(t *testing.T) {
	errs := checkSource(t, `type Label
	export var text as String
	function reset(inout self)
		replace(self)
	end 'reset'
end 'Label'

function replace(l inout Label)
	l.text = "y"
end 'replace'

function main() returns ExitCode
	return 0
end 'main'
`)
	expectCodes(t, errs, diag.ErrAssignToImmutable)
	if errs[0].Line != 4 {
		t.Errorf("the error is at the call in reset, got line %d", errs[0].Line)
	}
}

// A variable passed to a set parameter has its value when the call has run, so another argument of the call cannot read it.
func TestASetArgumentHasNoValueUntilTheCallHasRun(t *testing.T) {
	expectCodes(t, checkHole(t, "\tvar s as String\n\tfill(s, from: s.copy())\n"), diag.ErrUseBeforeInit)
	expectCodes(t, checkHole(t, "\tvar s as String\n\tfill(s, from: \"a\")\n\tprint(s)\n"))
}
