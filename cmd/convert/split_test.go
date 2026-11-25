package main

import (
	"strings"
	"testing"
)

const splitSource = `/// A holder.

extension Holder implements Hashable
	/// The hash.
	public function hash() returns HashValue
		return 1
	end 'hash'

	public function size() returns int
		return 2
	end 'size'

	static function fold(x int) returns int
		return x
	end 'fold'

	public static function make() returns Holder
		return Holder{}
	end 'make'
end 'Holder'

/// The iterator.
public type HolderIterator uses Element implements Iterator with Element
	var raw as int
end 'HolderIterator'

let Limit = 3
`

func TestSplitMovesMembersAndDeclarations(t *testing.T) {
	routes := []route{
		{file: "Holder.maxon", module: "core", header: "extension Holder implements", members: []string{"hash", "fold"}},
		{file: "Holder.maxon", module: "core", header: "type HolderIterator"},
	}
	rest, order, moved, err := splitDeclarations("Holder.maxon", splitSource, routes)
	if err != nil {
		t.Fatal(err)
	}
	wantRest := "/// A holder.\n\nextension Holder\n\tpublic function size() returns int\n\t\treturn 2\n\tend 'size'\n\n\tpublic static function make() returns Holder\n\t\treturn Holder{}\n\tend 'make'\nend 'Holder'\n\nlet Limit = 3\n"
	if rest != wantRest {
		t.Errorf("what stays:\ngot:\n%q\nwant:\n%q", rest, wantRest)
	}
	wantMoved := "extension Holder implements Hashable\n\t/// The hash.\n\tpublic function hash() returns HashValue\n\t\treturn 1\n\tend 'hash'\n\n\texport static function fold(x int) returns int\n\t\treturn x\n\tend 'fold'\nend 'Holder'\n\n/// The iterator.\npublic type HolderIterator uses Element implements Iterator with Element\n\tvar raw as int\nend 'HolderIterator'\n"
	if len(order) != 1 || order[0] != "core" || moved["core"] != wantMoved {
		t.Errorf("what moves, %v:\ngot:\n%q\nwant:\n%q", order, moved["core"], wantMoved)
	}
}

func TestSplitRefusesARouteThatMatchesNothing(t *testing.T) {
	routes := []route{{file: "Holder.maxon", module: "core", header: "extension Gone implements"}}
	if _, _, _, err := splitDeclarations("Holder.maxon", splitSource, routes); err == nil || !strings.Contains(err.Error(), "fix the split table") {
		t.Errorf("a stale route: %v", err)
	}
	routes = []route{{file: "Holder.maxon", module: "core", header: "extension Holder implements", members: []string{"gone"}}}
	if _, _, _, err := splitDeclarations("Holder.maxon", splitSource, routes); err == nil || !strings.Contains(err.Error(), "no member") {
		t.Errorf("a stale member: %v", err)
	}
}
