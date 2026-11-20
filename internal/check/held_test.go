package check

import "testing"

const heldPrelude = "testdata/held/prelude.maxon"

func TestHeldBorrowsAccept(t *testing.T) {
	checkCases(t, "testdata/held/accepts.maxon", heldPrelude)
}

func TestHeldBorrowsReject(t *testing.T) {
	checkCases(t, "testdata/held/rejects.maxon", heldPrelude)
}
