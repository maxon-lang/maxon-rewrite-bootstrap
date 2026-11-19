package check

import "testing"

const fieldMovesPrelude = "testdata/fieldmoves/prelude.maxon"

func TestFieldMovesAccepts(t *testing.T) {
	checkCases(t, "testdata/fieldmoves/accepts.maxon", fieldMovesPrelude)
}

func TestFieldMovesRejects(t *testing.T) {
	checkCases(t, "testdata/fieldmoves/rejects.maxon", fieldMovesPrelude)
}
