package check

import "testing"

const droppablePrelude = "testdata/droppable/prelude.maxon"

func TestDroppableAccepts(t *testing.T) {
	checkCases(t, "testdata/droppable/accepts.maxon", droppablePrelude)
}

func TestDroppableRejects(t *testing.T) {
	checkCases(t, "testdata/droppable/rejects.maxon", droppablePrelude)
}
