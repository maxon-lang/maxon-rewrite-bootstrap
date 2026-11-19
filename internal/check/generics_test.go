package check

import (
	"strings"
	"testing"

	"minimaxon/internal/diag"
)

const genericsPrelude = "testdata/generics/prelude.maxon"

func TestGenericsAccept(t *testing.T) {
	checkCases(t, "testdata/generics/accepts.maxon", genericsPrelude)
}

func TestGenericBodyIsCheckedOnce(t *testing.T) {
	expectCodes(t, checkFile(t, genericsPrelude, "testdata/generics/generic-body-is-checked-once.maxon"), diag.ErrUseAfterMove)
}

// Every call passes a scalar, which could be bound twice, but the body is checked for any type.
func TestTypeParameterIsOwnedWhateverItBecomes(t *testing.T) {
	expectCodes(t, checkFile(t, genericsPrelude, "testdata/generics/type-parameter-is-owned-whatever-it-becomes.maxon"), diag.ErrUseAfterMove)
}

func TestGenericsReject(t *testing.T) {
	checkCases(t, "testdata/generics/rejects.maxon", genericsPrelude)
}

func TestBoundIsCheckedAtTheInstance(t *testing.T) {
	errs := checkFile(t, genericsPrelude, "testdata/generics/bound-is-checked-at-the-instance.maxon")
	expectCodes(t, errs, diag.ErrBoundNotMet)
	for _, want := range []string{"Plain", "Shape", "type Plain implements Shape", "area"} {
		if !strings.Contains(errs[0].Message, want) {
			t.Errorf("the message %q does not mention %q", errs[0].Message, want)
		}
	}
}

func TestBoundErrorInsideABodyIsReportedOnce(t *testing.T) {
	expectCodes(t, checkFile(t, genericsPrelude, "testdata/generics/bound-error-inside-a-body-is-reported-once.maxon"), diag.ErrBoundNotMet)
}

func TestConformanceIsCheckedAtTheDeclaration(t *testing.T) {
	checkCases(t, "testdata/generics/conformance.maxon", genericsPrelude)
}

func TestSelfInAnInterfaceIsTheImplementingType(t *testing.T) {
	expectCodes(t, checkFile(t, genericsPrelude, "testdata/generics/self-in-an-interface-is-the-implementing-type.maxon"))
}
