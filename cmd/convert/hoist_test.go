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

func TestConvertBindsABlockInBracketsOnLinesOfItsOwn(t *testing.T) {
	in := "function f(s String) returns Count\n\tlet n = (try g(s) otherwise 'bad'\n\t\tpanic(\"no\")\n\tend 'bad') as Count\n\treturn n\nend 'f'\n"
	want := "function f(s String) returns Count\n\tlet nBlock = try g(s) otherwise 'bad'\n\t\tpanic(\"no\")\n\tend 'bad'\n\tlet n = nBlock as Count\n\treturn n\nend 'f'\n"
	res := Convert("x.maxon", in, sourceFile)
	if len(res.Warnings) > 0 || res.Text != want {
		t.Fatalf("warnings %v, got:\n%q\nwant:\n%q", res.Warnings, res.Text, want)
	}
}

func TestConvertKeepsABlockInBracketsThatItsNameTakes(t *testing.T) {
	in := "function f(nBlock Count) returns Count\n\tlet n = (match nBlock 'm'\n\t\t0 gives 1\n\t\tdefault gives 2\n\tend 'm') as Count\n\treturn n\nend 'f'\n"
	res := Convert("x.maxon", in, sourceFile)
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0].Message, "a block inside brackets") {
		t.Fatalf("warnings %v:\n%s", res.Warnings, res.Text)
	}
}

func TestConvertBindsABlockThatIsAnArgumentOnLinesOfItsOwn(t *testing.T) {
	in := "function f(xs Array with Count, k Count)\n\tfor x in xs 'each'\n\t\tout.push(match x 'm'\n\t\t\t0 gives 1\n\t\t\tdefault gives 2\n\t\tend 'm')\n\tend 'each'\nend 'f'\n"
	res := Convert("x.maxon", in, sourceFile)
	if len(res.Warnings) > 0 || !strings.Contains(res.Text, "\t\tlet hoisted3 = match x 'm'") || !strings.Contains(res.Text, "\t\tout.push(hoisted3)\n") {
		t.Fatalf("warnings %v, got:\n%s", res.Warnings, res.Text)
	}
}

func TestConvertKeepsABlockThatAnArmOrAClosureOwns(t *testing.T) {
	arm := "function f(e Count)\n\tmatch e 'k'\n\t\t0 then total = run(match e 'inner'\n\t\t\tdefault gives 1\n\t\tend 'inner')\n\t\tdefault then total = 2\n\tend 'k'\nend 'f'\n"
	closure := "function f(e Count)\n\ttotal = run(function() gives match e 'inner'\n\t\tdefault gives 1\n\tend 'inner')\nend 'f'\n"
	for _, in := range []string{arm, closure} {
		res := Convert("x.maxon", in, sourceFile)
		if strings.Contains(res.Text, "hoisted") {
			t.Fatalf("hoisted a block its arm or closure owns:\n%s", res.Text)
		}
	}
}
