package interp_test

import (
	"reflect"
	"strings"
	"testing"

	"minimaxon/internal/driver"
	"minimaxon/internal/interp"
)

// The order in which values are freed is part of the memory model: a local when its block ends, newest first; a value
// that was moved by its new owner and not twice; a temporary when its statement ends; an overwritten value when it is
// assigned; the elements of a collection after the collection, in their order; and every one exactly once.

func interpret(t *testing.T, path string) *interp.Result {
	t.Helper()
	result, errs := driver.Interpret(path, 1, interp.DefaultStepLimit)
	if len(errs) > 0 {
		t.Fatalf("%v", errs)
	}
	if len(result.Violations) > 0 {
		t.Fatalf("%s", result.Report())
	}
	return result
}

// framed is the labels of the frees whose text starts with a tag character, in order. The strings a program builds to
// be freed carry a tag; the strings its interpolations make on the way do not.
func framed(frees []string, tags string) []string {
	var out []string
	for _, f := range frees {
		if strings.HasPrefix(f, "string \"") && strings.ContainsRune(tags, rune(f[len("string \"")])) {
			out = append(out, strings.TrimPrefix(f, "string "))
			continue
		}
		if !strings.HasPrefix(f, "string") && strings.Contains(tags, "*") {
			out = append(out, f)
		}
	}
	return out
}

func expectFrees(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("freed %v, want %v", got, want)
	}
}

func TestLocalsAreFreedNewestFirst(t *testing.T) {
	r := interpret(t, "testdata/frees/locals-are-freed-newest-first.maxon")
	expectFrees(t, framed(r.Frees, "abc"), []string{`"c3"`, `"b2"`, `"a1"`})
}

func TestAMovedValueIsFreedOnceByItsNewOwner(t *testing.T) {
	r := interpret(t, "testdata/frees/a-moved-value-is-freed-once-by-its-new-owner.maxon")
	// a moves to b, which moves into keep: keep frees it when it returns, after its own local, and main frees only c.
	expectFrees(t, framed(r.Frees, "abcz"), []string{`"z9"`, `"a1"`, `"c3"`})
}

func TestAnOverwrittenValueIsFreedAtTheAssignment(t *testing.T) {
	r := interpret(t, "testdata/frees/an-overwritten-value-is-freed-at-the-assignment.maxon")
	// v1 dies at the assignment; at the end u, which is newer, goes before v, which now holds w2.
	expectFrees(t, framed(r.Frees, "uvw"), []string{`"v1"`, `"u3"`, `"w2"`})
}

func TestATemporaryIsFreedWhenItsStatementEnds(t *testing.T) {
	r := interpret(t, "testdata/frees/a-temporary-is-freed-when-its-statement-ends.maxon")
	// The 2 that the interpolation made is freed once it is joined; the joined text is a temporary of the print.
	expectFrees(t, framed(r.Frees, "abt"), []string{`"t2"`, `"t2\n"`, `"b3"`, `"a1"`})
}

func TestTheElementsOfACollectionAreFreedAfterItInTheirOrder(t *testing.T) {
	r := interpret(t, "testdata/frees/the-elements-of-a-collection-are-freed-after-it-in-their-order.maxon")
	var order []string
	for _, f := range r.Frees {
		if f == "array" || strings.HasPrefix(f, `string "e`) {
			order = append(order, f)
		}
	}
	expectFrees(t, order, []string{"array", `string "e1"`, `string "e2"`, `string "e3"`})
}

func TestAnElementTakenOutIsFreedByTheCallerNotTheCollection(t *testing.T) {
	r := interpret(t, "testdata/frees/an-element-taken-out-is-freed-by-the-caller-not-the-collection.maxon")
	// e1 is dropped by its statement, which ends before the locals do; e2 by main, then z4, then the array and e3.
	var order []string
	for _, f := range r.Frees {
		if f == "array" || strings.HasPrefix(f, `string "e`) || strings.HasPrefix(f, `string "z`) {
			order = append(order, f)
		}
	}
	expectFrees(t, order, []string{`string "e1"`, `string "z4"`, `string "e2"`, "array", `string "e3"`})
}

func TestEveryPathOutOfAFunctionFreesItsLocals(t *testing.T) {
	r := interpret(t, "testdata/frees/every-path-out-of-a-function-frees-its-locals.maxon")
	// Iterations 0 and 1 free b at their end, and the third returns from inside the if: c, then b, then a.
	expectFrees(t, framed(r.Frees, "abc"), []string{`"b2"`, `"b2"`, `"c3"`, `"b2"`, `"a1"`})
}

func TestARemovedPoolElementIsFreedByTheCaller(t *testing.T) {
	r := interpret(t, "testdata/frees/a-removed-pool-element-is-freed-by-the-caller.maxon")
	// again and gone are options that hold p1 or nothing; they are freed newest first, and the pool frees p2 last.
	var order []string
	for _, f := range r.Frees {
		if f == "pool" || strings.HasPrefix(f, `string "p`) {
			order = append(order, f)
		}
	}
	expectFrees(t, order, []string{`string "p1"`, "pool", `string "p2"`})
}

// A copy is a value of its own, with an owner of its own to free it: every box and every string inside it is made again,
// so each is freed once for the original and once for the copy, in the order of the value they belong to. The copy of a
// literal is the literal, which nothing frees.
func TestACopyIsFreedAsAValueOfItsOwn(t *testing.T) {
	r := interpret(t, "testdata/frees/a-copy-is-freed-as-a-value-of-its-own.maxon")
	// the numbers the interpolations made go first; then b, being newer: its box, then its text, then its array and the one string in it; then a the same.
	expectFrees(t, r.Frees, []string{`string "1"`, `string "2"`, "record", `string "a1"`, "array", `string "p2"`, "record", `string "a1"`, "array", `string "p2"`})
}
