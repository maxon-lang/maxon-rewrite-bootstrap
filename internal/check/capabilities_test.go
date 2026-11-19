package check

import "testing"

const capabilitiesPrelude = "testdata/capabilities/prelude.maxon"

func TestCapabilitiesAccept(t *testing.T) {
	checkCases(t, "testdata/capabilities/accepts.maxon", capabilitiesPrelude)
}

func TestCapabilitiesReject(t *testing.T) {
	checkCases(t, "testdata/capabilities/rejects.maxon", capabilitiesPrelude)
}
