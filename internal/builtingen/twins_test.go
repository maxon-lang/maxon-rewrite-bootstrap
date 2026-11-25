package main

import (
	"slices"
	"strings"
	"testing"
)

// refuses is whether loading the rows fails with a message that contains want.
func refuses(t *testing.T, rows []function, want string) {
	t.Helper()
	defer func() {
		r := recover()
		te, ok := r.(*tableError)
		if !ok {
			t.Fatalf("loading the rows gave %v, want the table error %q", r, want)
		}
		if !strings.Contains(te.msg, want) {
			t.Errorf("loading the rows gave %q, want one that contains %q", te.msg, want)
		}
	}()
	loadFunctions(rows)
}

// withRows is the table's rows with those that match changed by edit.
func withRows(match func(function) bool, edit func(*function)) []function {
	rows := slices.Clone(functions)
	changed := false
	for i := range rows {
		if match(rows[i]) {
			edit(&rows[i])
			changed = true
		}
	}
	if !changed {
		panic("no row matches")
	}
	return rows
}

func row(owner, name, field string) func(function) bool {
	return func(f function) bool { return f.Owner == owner && f.Name == name && f.Field == field }
}

func intrinsicRows(name string) func(function) bool {
	return func(f function) bool { return f.Intrinsic == name }
}

func TestTwinsOfTheTable(t *testing.T) {
	tb := load()
	twins := map[string]string{}
	for _, f := range tb.fns {
		if f.Proven != "" {
			twins[f.Owner+"."+f.Name] = f.Proven
		}
	}
	for key, want := range map[string]string{
		"Array.get": "ArrayGetProven", "Array.set": "ArraySetProven", "FixedArray.get": "FixedArrayGetProven", "FixedArray.set": "FixedArraySetProven",
	} {
		if twins[key] != want {
			t.Errorf("%s has the twin %q, want %q", key, twins[key], want)
		}
	}
}

func TestColumnsOfTheTable(t *testing.T) {
	tb := load()
	byName := map[string]intrinsic{}
	for _, in := range tb.intrinsics {
		byName[in.name] = in
	}
	for _, c := range []struct{ intrinsic, requires, count, measure string }{
		{"ArrayGet", requiresIndexBelow, countKeep, measureNone},
		{"ArrayGetProven", requiresIndexBelow, countKeep, measureNone},
		{"ArraySetProven", requiresIndexBelow, countKeep, measureNone},
		{"ArrayCount", requiresNone, countKeep, measureCount},
		{"FixedArrayCount", requiresNone, countKeep, measureCount},
		{"ArrayIsEmpty", requiresNone, countKeep, measureIsEmpty},
		{"ArrayPush", requiresNone, countGrowOne, measureNone},
		{"ArrayPop", requiresNone, countShrink, measureNone},
		{"ArrayResize", requiresNone, countChange, measureNone},
		{"ArrayDropEmpty", requiresNone, countChange, measureNone},
		{"EnumHash", requiresNone, countKeep, measureNone},
	} {
		in, ok := byName[c.intrinsic]
		if !ok || in.requires != c.requires || in.count != c.count || in.measure != c.measure {
			t.Errorf("%s has %+v, want requires %s, count %s, measure %s", c.intrinsic, in, c.requires, c.count, c.measure)
		}
	}
}

func TestTwinRefusals(t *testing.T) {
	t.Run("twin throws", func(t *testing.T) {
		refuses(t, withRows(intrinsicRows("ArrayGetProven"), func(f *function) { f.Throws = "ArrayError" }), "twin ArrayGetProven throws")
	})
	t.Run("twin differs", func(t *testing.T) {
		refuses(t, withRows(row("Array", "get", "ArrayGetProven"), func(f *function) { f.Result = "Bool" }), "differs in its owner")
	})
	t.Run("no twin", func(t *testing.T) {
		refuses(t, withRows(row("Array", "get", ""), func(f *function) { f.Proven = "NoSuchTwin" }), "which no row has")
	})
	t.Run("nothing to rely on", func(t *testing.T) {
		rows := withRows(func(f function) bool { return f.Intrinsic == "ArrayGet" || f.Intrinsic == "ArrayGetProven" }, func(f *function) { f.Requires = "" })
		refuses(t, rows, "Requires nothing")
	})
	t.Run("twin needs another fact", func(t *testing.T) {
		refuses(t, withRows(intrinsicRows("ArraySetProven"), func(f *function) { f.Requires = requiresNonEmpty }), "its twin ArraySetProven needs")
	})
	t.Run("twin has another count effect", func(t *testing.T) {
		refuses(t, withRows(intrinsicRows("ArraySetProven"), func(f *function) { f.Count = countChange }), "its twin ArraySetProven needs")
	})
}

func TestColumnRefusals(t *testing.T) {
	t.Run("rows disagree", func(t *testing.T) {
		refuses(t, withRows(row("FixedArray", "get", ""), func(f *function) { f.Requires = requiresNonEmpty }), "disagree on Requires")
	})
	t.Run("unknown value", func(t *testing.T) {
		refuses(t, withRows(row("Array", "count", ""), func(f *function) { f.Measure = "length" }), "which is none of")
	})
	t.Run("count changed by a row that does not mutate", func(t *testing.T) {
		refuses(t, withRows(row("Array", "slice", ""), func(f *function) { f.Count = countShrink }), "no row of it mutates or consumes its receiver")
	})
	t.Run("index requirement without an index", func(t *testing.T) {
		refuses(t, withRows(row("Array", "count", ""), func(f *function) { f.Requires = requiresIndexBelow }), "names argument 0")
	})
	t.Run("measure that is not a count", func(t *testing.T) {
		refuses(t, withRows(row("Array", "isEmpty", ""), func(f *function) { f.Measure = measureCount }), "gives a Count")
	})
	t.Run("column on a free function", func(t *testing.T) {
		refuses(t, withRows(row("", "freeze", ""), func(f *function) { f.Count = countKeep }), "describe a method")
	})
}
