package check

import (
	"testing"

	"minimaxon/internal/diag"
)

// Each program here was accepted once, and broke the memory model when it ran under the interpreter, so each is refused
// now: these are the holes that the tracking interpreter and the program generator found.

const holesPrelude = "testdata/holes/prelude.maxon"

// A field of a temporary is not a place that can be moved out of: the temporary is dropped when its statement ends, and
// the field would have two owners.
func TestAFieldOfATemporaryCannotBeMovedOut(t *testing.T) {
	checkCases(t, "testdata/holes/a-field-of-a-temporary-cannot-be-moved-out.maxon", holesPrelude)
}

// self is the receiver, a pointer that the caller still owns: a mutable parameter could assign the whole value, which
// self cannot take. A field of it can be passed, and a method called on it (the prelude does both).
func TestSelfIsNotAMutableArgument(t *testing.T) {
	errs := checkFile(t, "testdata/holes/self-is-not-a-mutable-argument.maxon")
	expectCodes(t, errs, diag.ErrAssignToImmutable)
	if errs[0].Line != 5 {
		t.Errorf("the error is at the call in reset, got line %d", errs[0].Line)
	}
}

// A variable passed to a set parameter has its value when the call has run, so another argument of the call cannot read it.
func TestASetArgumentHasNoValueUntilTheCallHasRun(t *testing.T) {
	checkCases(t, "testdata/holes/a-set-argument-has-no-value-until-the-call-has-run.maxon", holesPrelude)
}
