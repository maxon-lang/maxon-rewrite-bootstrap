package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestConvertHoistsARangedTypealiasOutOfAGenericExtension(t *testing.T) {
	in := "public type Run\n\tpublic var power as Array.Power\nend 'Run'\n\npublic extension Array\n\tpublic typealias Power = int(0 to 64)\n\ttypealias Stack = Array with Run\n\n\tfunction depth() returns Power\n\t\treturn 0\n\tend 'depth'\nend 'Array'\n"
	want := "public type Run\n\tpublic var power as Power\nend 'Run'\n\npublic typealias Power = int(0 to 64)\n\npublic extension Array\n\ttypealias Stack = Array with Run\n\n\tfunction depth() returns Power\n\t\treturn 0\n\tend 'depth'\nend 'Array'\n"
	res := Convert("x.maxon", in, sourceFile)
	if !res.Formatted || len(res.Warnings) > 0 {
		t.Fatalf("warnings %v, formatted %v:\n%s", res.Warnings, res.Formatted, res.Text)
	}
	if res.Text != want {
		t.Fatalf("got:\n%q\nwant:\n%q", res.Text, want)
	}
}

func TestConvertKeepsARangedTypealiasThatNamesATypeParameter(t *testing.T) {
	in := "type Bin uses Element\n\ttypealias Idx = int(0 to u64.max)\n\ttypealias Own = int(0 to Element)\nend 'Bin'\n"
	res := Convert("x.maxon", in, sourceFile)
	for _, w := range res.Warnings {
		if w.Line == 3 {
			return
		}
	}
	t.Fatalf("a range that names a type parameter stays in the type, which warns:\n%s", res.Text)
}

func TestConvertKeepsThePublicNameOfAHelperThatTheRootExposes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "stdlib")
	in := "public extension Array\n\tpublic typealias SortComparator = function(Element, Element) returns Ordering\n\tpublic function internalSort(cmp SortComparator)\n\tend 'internalSort'\n\tpublic function swap(i ElementIndex, j ElementIndex)\n\tend 'swap'\nend 'Array'\n"
	res := Convert(filepath.Join(root, "helpers", "sort", "insertionSort.maxon"), in, sourceFile)
	if !strings.Contains(res.Text, "\tpublic typealias SortComparator") || !strings.Contains(res.Text, "\texport function internalSort") || !strings.Contains(res.Text, "\tpublic function swap") {
		t.Fatalf("the exposed name stays public and the others are export:\n%s", res.Text)
	}
}
