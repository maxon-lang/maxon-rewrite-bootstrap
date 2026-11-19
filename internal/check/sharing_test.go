package check

import (
	"testing"

	"minimaxon/internal/diag"
)

const sharingPrelude = "testdata/sharing/prelude.maxon"

func TestFrozenValuesAreReadAndShared(t *testing.T) {
	expectCodes(t, checkFile(t, sharingPrelude, "testdata/sharing/frozen-values-are-read-and-shared.maxon"))
}

func TestFrozenValuesCannotBeChanged(t *testing.T) {
	checkCases(t, "testdata/sharing/frozen-values-cannot-be-changed.maxon", sharingPrelude)
}

func TestSharingNeedsASharedValue(t *testing.T) {
	checkCases(t, "testdata/sharing/sharing-needs-a-shared-value.maxon", sharingPrelude)
}

func TestACownIsSharedAndNamedByABehaviour(t *testing.T) {
	expectCodes(t, checkFile(t, sharingPrelude, "testdata/sharing/a-cown-is-shared-and-named-by-a-behaviour.maxon"))
}

// What the outer body reads into a scalar of its own, the inner one captures.
func TestABehaviourMayScheduleAnotherButNotWithItsAccess(t *testing.T) {
	checkCases(t, "testdata/sharing/a-behaviour-may-schedule-another-but-not-with-its-access.maxon", sharingPrelude)
}

func TestABehaviourCapturesOwnersAndCopiesScalars(t *testing.T) {
	expectCodes(t, checkFile(t, sharingPrelude, "testdata/sharing/a-behaviour-captures-owners-and-copies-scalars.maxon"))
}

func TestACapturedOwnerIsMovedIntoTheBehaviour(t *testing.T) {
	expectCodes(t, checkFile(t, sharingPrelude, "testdata/sharing/a-captured-owner-is-moved-into-the-behaviour.maxon"), diag.ErrUseAfterMove)
}

func TestABehaviourCannotCaptureABorrow(t *testing.T) {
	expectCodes(t, checkFile(t, sharingPrelude, "testdata/sharing/a-behaviour-cannot-capture-a-borrow.maxon"), diag.ErrBehaviourBorrow)
}

func TestTheValueOfACownIsOnlyForABehaviour(t *testing.T) {
	checkCases(t, "testdata/sharing/the-value-of-a-cown-is-only-for-a-behaviour.maxon", sharingPrelude)
}

func TestACownNamedTwiceIsRefused(t *testing.T) {
	expectCodes(t, checkFile(t, sharingPrelude, "testdata/sharing/a-cown-named-twice-is-refused.maxon"), diag.ErrWhenSameCown)
}

// An owned value and a frozen value cannot form a cycle, so a type may hold either of itself.
func TestATypeThatReachesItselfThroughACownIsRefused(t *testing.T) {
	checkCases(t, "testdata/sharing/a-type-that-reaches-itself-through-a-cown-is-refused.maxon")
}

func TestGenericCodeSharesAndFreezesAnyType(t *testing.T) {
	expectCodes(t, checkFile(t, "testdata/sharing/generic-code-shares-and-freezes-any-type.maxon"))
}
