package check

import (
	"strings"
	"testing"

	"minimaxon/internal/diag"
)

const generics = `typealias Score = int(i64.min to i64.max)

interface Shape
	function area() returns Score
	function grow(inout self, by Score)
end 'Shape'

type Square implements Shape
	export var side as Score
	static function create(side Score) returns Square
		return Square{side: side}
	end 'create'
	function area() returns Score
		return side * side
	end 'area'
	function grow(inout self, by Score)
		side = side + by
	end 'grow'
end 'Square'

type Plain
	export var n as Score
end 'Plain'

type Box[T]
	export var item as T
	export let tag as Score
	static function make(item sink T, tag Score) returns Self
		return Self{item: item, tag: tag}
	end 'make'
	function peek() returns T
		return item.copy()
	end 'peek'
	function replace(inout self, next sink T)
		item = next
	end 'replace'
end 'Box'

union Slot[T]
	full(value T)
	empty
end 'Slot'

function swap[T](a inout T, b inout T)
	var tmp = a.copy()
	a = b.copy()
	b = tmp
end 'swap'

function consume[T](a sink T) returns Score
	return 1
end 'consume'

function total[T implements Shape](a T, b T) returns Score
	return a.area() + b.area()
end 'total'

`

func checkGeneric(t *testing.T, decls, body string) []*diag.CompileError {
	t.Helper()
	return checkSource(t, generics+decls+"function main() returns ExitCode\n"+body+"\treturn 0\nend 'main'\n")
}

func TestGenericsAccept(t *testing.T) {
	for name, body := range map[string]string{
		"explicit type arguments":     "\tvar a = 1 as Score\n\tvar b = 2 as Score\n\tswap[Score](a, b: b)\n",
		"inferred type arguments":     "\tvar a = Square.create(1)\n\tvar b = Square.create(2)\n\tswap(a, b: b)\n\tprint(\"{total(a, b: b)}\")\n",
		"generic type and its field":  "\tvar b = Box[String].make(\"x\", tag: 1)\n\tb.replace(\"y\")\n\tprint(\"{b.peek()} {b.tag}\")\n",
		"nested instances":            "\tlet b = Box[Box[Score]].make(Box[Score].make(1, tag: 2), tag: 3)\n\tprint(\"{b.peek().tag}\")\n",
		"union instance":              "\tlet s = Slot[String].full(\"x\")\n\tmatch s 'm'\n\t\tfull(v) then print(v)\n\t\tempty then print(\"none\")\n\tend 'm'\n",
		"union inferred from payload": "\tlet s = Slot.full(Square.create(1))\n\tmatch s 'm'\n\t\tfull(v) then print(\"{v.area()}\")\n\t\tempty then print(\"none\")\n\tend 'm'\n",
		"copy of a generic instance":  "\tlet a = Box[String].make(\"x\", tag: 1)\n\tlet b = a.copy()\n\tprint(b.peek())\n",
	} {
		t.Run(name, func(t *testing.T) { expectCodes(t, checkGeneric(t, "", body)) })
	}
}

func TestGenericBodyIsCheckedOnce(t *testing.T) {
	decls := "function twice[T](a sink T) returns Score\n\tlet n = consume(a)\n\treturn consume(a)\nend 'twice'\n\n"
	calls := "\t_ = twice(\"s\")\n\t_ = twice(Square.create(1))\n\t_ = twice(Box[Score].make(1, tag: 1))\n"
	expectCodes(t, checkGeneric(t, decls, calls), diag.ErrUseAfterMove)
}

func TestTypeParameterIsOwnedWhateverItBecomes(t *testing.T) {
	// Every call passes a scalar, which could be bound twice, but the body is checked for any type.
	decls := "function twice[T](a sink T) returns Score\n\tlet first = a\n\tlet second = a\n\treturn 1\nend 'twice'\n\n"
	expectCodes(t, checkGeneric(t, decls, "\t_ = twice(1 as Score)\n"), diag.ErrUseAfterMove)
}

func TestGenericsReject(t *testing.T) {
	for _, tc := range []struct{ name, decls, body, code string }{
		{"wrong number of type arguments", "", "\tlet b = Box[Score, Score].make(1, tag: 1)\n", diag.ErrTypeArgumentCount},
		{"generic type with no arguments", "function f(b Box)\n\tprint(\"x\")\nend 'f'\n\n", "", diag.ErrTypeArgumentCount},
		{"type arguments on a plain type", "function f(p Plain[Score])\n\tprint(\"x\")\nend 'f'\n\n", "", diag.ErrTypeArgumentCount},
		{"type arguments on a plain function", "function plain(n Score) returns Score\n\treturn n\nend 'plain'\n\n", "\t_ = plain[Score](1)\n", diag.ErrTypeArgumentCount},
		{"cannot infer from a constant", "", "\tlet b = Box.make(1, tag: 1)\n", diag.ErrCannotInfer},
		{"interface as a type", "function f(s Shape)\n\tprint(\"x\")\nend 'f'\n\n", "", diag.ErrInterfaceAsType},
		{"unknown method on an unbounded parameter", "function f[T](a T) returns Score\n\treturn a.area()\nend 'f'\n\n", "", diag.ErrUnknownFunction},
		{"field of a type parameter", "function f[T](a T) returns Score\n\treturn a.tag\nend 'f'\n\n", "", diag.ErrUnknownField},
		{"comparing type parameters", "function f[T](a T, b T) returns bool\n\treturn a == b\nend 'f'\n\n", "", diag.ErrTypeMismatch},
		{"interpolating a type parameter", "function f[T](a T) returns String\n\treturn \"{a}\"\nend 'f'\n\n", "", diag.ErrTypeMismatch},
		{"returning a different type parameter", "function f[T, U](a T, b U) returns T\n\treturn b.copy()\nend 'f'\n\n", "", diag.ErrTypeMismatch},
		{"bound that is not an interface", "function f[T implements Square](a T)\n\tprint(\"x\")\nend 'f'\n\n", "", diag.ErrGenericDeclaration},
	} {
		t.Run(tc.name, func(t *testing.T) { expectCodes(t, checkGeneric(t, tc.decls, tc.body), tc.code) })
	}
}

func TestBoundIsCheckedAtTheInstance(t *testing.T) {
	decls := "function f(p Plain) returns Score\n\treturn total(p, b: p)\nend 'f'\n\n"
	errs := checkGeneric(t, decls, "")
	expectCodes(t, errs, diag.ErrBoundNotMet)
	for _, want := range []string{"Plain", "Shape", "type Plain implements Shape", "area"} {
		if !strings.Contains(errs[0].Message, want) {
			t.Errorf("the message %q does not mention %q", errs[0].Message, want)
		}
	}
}

func TestBoundErrorInsideABodyIsReportedOnce(t *testing.T) {
	decls := "function f[T](a T) returns Score\n\treturn total(a, b: a)\nend 'f'\n\nfunction g() returns Score\n\treturn f(Square.create(1)) + f(Square.create(2))\nend 'g'\n\n"
	expectCodes(t, checkGeneric(t, decls, ""), diag.ErrBoundNotMet)
}

func TestConformanceIsCheckedAtTheDeclaration(t *testing.T) {
	const area = "\tfunction area() returns Score\n\t\treturn r\n\tend 'area'\n"
	for name, decl := range map[string]string{
		"missing method":   "type Round implements Shape\n\tvar r as Score\n" + area + "end 'Round'\n",
		"wrong return":     "type Round implements Shape\n\tvar r as Score\n\tfunction area() returns String\n\t\treturn \"r\"\n\tend 'area'\n\tfunction grow(inout self, by Score)\n\t\tr = r + by\n\tend 'grow'\nend 'Round'\n",
		"wrong convention": "type Round implements Shape\n\tvar r as Score\n" + area + "\tfunction grow(by Score)\n\t\tprint(\"x\")\n\tend 'grow'\nend 'Round'\n",
		"wrong parameter":  "type Round implements Shape\n\tvar r as Score\n" + area + "\tfunction grow(inout self, by String)\n\t\tprint(by)\n\tend 'grow'\nend 'Round'\n",
	} {
		t.Run(name, func(t *testing.T) { expectCodes(t, checkGeneric(t, decl+"\n", ""), diag.ErrConformance) })
	}
}

func TestSelfInAnInterfaceIsTheImplementingType(t *testing.T) {
	decls := `interface Scale
	function doubled() returns Self
end 'Scale'

type Mark implements Scale
	export var n as Score
	static function create(n Score) returns Mark
		return Mark{n: n}
	end 'create'
	function doubled() returns Mark
		return Mark{n: n * 2}
	end 'doubled'
end 'Mark'

function twice[T implements Scale](a T) returns T
	return a.doubled().doubled()
end 'twice'

`
	expectCodes(t, checkGeneric(t, decls, "\tlet m = twice(Mark.create(1))\n\tprint(\"{m.n}\")\n"))
}
