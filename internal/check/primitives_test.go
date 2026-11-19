package check

import "testing"

const primitivesPrelude = "testdata/primitives/prelude.maxon"

func TestPrimitivesAccept(t *testing.T) {
	checkCases(t, "testdata/primitives/accepts.maxon", primitivesPrelude)
}

func TestPrimitivesRejects(t *testing.T) {
	checkCases(t, "testdata/primitives/rejects.maxon", primitivesPrelude)
}

// A cache that drops the entry used longest ago: a list in order of use, and a map from each key to the handle of its
// node. Neither structure points into the other; the handle is only a key.
func TestAnLruCacheChecks(t *testing.T) {
	expectCodes(t, checkFile(t, "testdata/primitives/an-lru-cache-checks.maxon"))
}

// A stack of nodes that link to each other by handle, which a pool owns.
func TestAStackOfHandlesChecks(t *testing.T) {
	expectCodes(t, checkFile(t, "testdata/primitives/a-stack-of-handles-checks.maxon"))
}

// Generic code over the primitives is checked once, like any other generic code.
func TestGenericCodeOverPrimitivesChecks(t *testing.T) {
	expectCodes(t, checkFile(t, "testdata/primitives/generic-code-over-primitives-checks.maxon"))
}
