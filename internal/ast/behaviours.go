package ast

import "strconv"

// Capture is a variable of the enclosing function that the body of a behaviour uses. The behaviour runs later, in
// another thread, so the body gets a value of its own: the variable's whole value moves into the behaviour at the
// `when`, or is copied if it is a scalar.
type Capture struct {
	// Ref is the use of the variable in the enclosing function, at the `when`. It is an ordinary read marked consumed, so
	// the ownership pass and lowering move it like any other value that changes owner. It is a node that resolution made
	// for the name in the body that takes the variable (see Tree.AppendCaptureRefs).
	Ref NameExpr
	// Inner is the parameter of the behaviour's function that holds the value inside the body.
	Inner *Local
}

// behaviourSuffix joins the name of a function to the number of a behaviour body in it.
const behaviourSuffix = "$when"

// BehaviourName is the name of the function that a behaviour's body becomes, numbered in the order the bodies are
// met in the enclosing function.
func BehaviourName(enclosing string, index int) string {
	return enclosing + behaviourSuffix + strconv.Itoa(index)
}
