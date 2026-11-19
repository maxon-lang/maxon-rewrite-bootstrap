package check

import (
	"testing"

	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"minimaxon/internal/project"
	"minimaxon/internal/resolve"
	"minimaxon/internal/syntax"
)

func checkSource(t *testing.T, src string) []*diag.CompileError {
	t.Helper()
	source := syntax.NewSource(src, []syntax.FileSlice{{File: "t.maxon", Begin: 0, End: len(src)}})
	parser := syntax.NewParser(source, syntax.Lex(source, nil))
	prog := parser.ParseProgram()
	if len(parser.Errors) > 0 {
		t.Fatalf("parse errors: %v", parser.Errors)
	}

	root := ast.NewModule("", ".", false)
	root.Decls = prog.Decls
	proj := ast.NewProject(root)
	proj.Modules = []*ast.Module{root}
	project.BuildTables(proj, 1)
	resolve.NewResolver(proj, root).Run()
	if len(root.Errors) > 0 {
		return root.Errors
	}

	c := NewChecker(proj, root, &root.Errors)
	for _, d := range root.Decls {
		root.Functions = append(root.Functions, c.Declare(d)...)
	}
	c.CheckMain()
	for _, d := range root.Decls {
		switch d.(type) {
		case *ast.TypeDecl, *ast.UnionDecl, *ast.EnumDecl:
			c.CheckBody(d)
		}
	}
	for _, f := range root.Functions {
		c.CheckBody(f)
	}
	c.Verify(c.Pending)
	c.VerifyBounds(c.Bounds)
	if len(root.Errors) == 0 {
		o := NewOwnership(&root.Errors)
		for _, d := range root.Decls {
			o.CheckValues(d)
		}
		for _, f := range root.Functions {
			o.Check(f)
		}
	}
	return root.Errors
}

func expectCodes(t *testing.T, errs []*diag.CompileError, codes ...string) {
	t.Helper()
	if len(errs) != len(codes) {
		t.Fatalf("got %v, want codes %v", errs, codes)
	}
	for i, code := range codes {
		if errs[i].Code != code {
			t.Errorf("error %d: got %v, want %s", i, errs[i], code)
		}
	}
}

const box = `typealias Score = int(i64.min to i64.max)

type Box
	export var v as Score
	static function create(v Score) returns Box
		return Box{v: v}
	end 'create'
end 'Box'
`

func TestAcceptsACorrectProgram(t *testing.T) {
	expectCodes(t, checkSource(t, box+`
function main() returns ExitCode
	let b = Box.create(4)
	print("{b.v}\n")
	return 0
end 'main'
`))
}

func TestMixedAliases(t *testing.T) {
	expectCodes(t, checkSource(t, `typealias Apples = int(0 to 100)

typealias Pears = int(0 to 100)

function main() returns ExitCode
	let a = 3 as Apples
	let p = 4 as Pears
	print("{a + p}\n")
	return 0
end 'main'
`), diag.ErrTypeMismatch)
}

func TestNonExhaustiveMatch(t *testing.T) {
	expectCodes(t, checkSource(t, `enum Color
	red
	green
end 'Color'

function main() returns ExitCode
	match Color.red 'm'
		red then print("r\n")
	end 'm'
	return 0
end 'main'
`), diag.ErrMatchNotExhaustive)
}

func TestUseAfterMove(t *testing.T) {
	expectCodes(t, checkSource(t, box+`
function main() returns ExitCode
	var a = Box.create(1)
	let b = a
	print("{a.v} {b.v}\n")
	a = Box.create(2)
	return 0
end 'main'
`), diag.ErrUseAfterMove)
}

func TestEndLabelMustNameItsBlock(t *testing.T) {
	expectCodes(t, checkSource(t, `function main() returns ExitCode
	return 0
end 'mian'
`), diag.ErrMismatchedEndLabel)
}

const owned = `typealias Score = int(i64.min to i64.max)

type Pair
	export var first as Score
	export var second as Score
	export var label as String
	function bump(inout self, by Score)
		first = first + by
	end 'bump'
	static function create(first Score, second Score) returns Pair
		return Pair{first: first, second: second, label: "pair"}
	end 'create'
end 'Pair'

function change(p inout Pair)
	p.first = 1
end 'change'

function swap(a inout Score, b inout Score)
	let t = a
	a = b
	b = t
end 'swap'

function take(p sink Pair) returns Score
	return p.first
end 'take'

function make(n set Score, flag bool)
	if flag 'early'
		n = 1
	end 'early' else 'late'
		n = 2
	end 'late'
end 'make'

`

func checkBody(t *testing.T, decls, body string) []*diag.CompileError {
	t.Helper()
	return checkSource(t, owned+decls+"function main() returns ExitCode\n"+body+"\treturn 0\nend 'main'\n")
}

func TestOwnershipAccepts(t *testing.T) {
	for name, body := range map[string]string{
		"copy is independent":         "\tvar a = Pair.create(1, second: 2)\n\tvar b = a.copy()\n\tb.first = 9\n\tprint(\"{a.first}\")\n",
		"inout of a var path":         "\tvar a = Pair.create(1, second: 2)\n\tchange(a)\n\tswap(a.first, b: a.second)\n",
		"inout receiver of a var":     "\tvar a = Pair.create(1, second: 2)\n\ta.bump(a.second.copy())\n",
		"sink then reassign":          "\tvar a = Pair.create(1, second: 2)\n\tlet n = take(a)\n\ta = Pair.create(n, second: 2)\n\tprint(\"{a.first}\")\n",
		"set a declared variable":     "\tvar n as Score\n\tmake(n, flag: true)\n\tprint(\"{n}\")\n",
		"move into a let and back":    "\tvar a = Pair.create(1, second: 2)\n\tlet b = a\n\ta = b.copy()\n\tprint(\"{a.first}\")\n",
		"fresh value in a loop":       "\tfor i in 0 upto 2 'each'\n\t\tvar a = Pair.create(i, second: 2)\n\t\tlet b = a\n\t\tprint(\"{b.first}\")\n\tend 'each'\n",
		"copy of a field out":         "\tlet a = Pair.create(1, second: 2)\n\tlet l = a.label.copy()\n\tprint(\"{l}\")\n",
		"scalars copy freely":         "\tlet a = Pair.create(1, second: 2)\n\tlet n = a.first\n\tlet m = n\n\tprint(\"{n} {m}\")\n",
		"conditional of fresh values": "\tlet a = Pair.create(1, second: 2)\n\tlet b = Pair.create(1, second: 1) if a.first == 1 else Pair.create(2, second: 2)\n\tprint(\"{b.first}\")\n",
	} {
		t.Run(name, func(t *testing.T) { expectCodes(t, checkBody(t, "", body)) })
	}
}

func TestOwnershipRejects(t *testing.T) {
	for _, tc := range []struct{ name, decls, body, code string }{
		{"use after move", "", "\tvar a = Pair.create(1, second: 2)\n\tlet b = a\n\tprint(\"{a.first} {b.first}\")\n", diag.ErrUseAfterMove},
		{"use after sink", "", "\tlet a = Pair.create(1, second: 2)\n\tlet n = take(a)\n\tprint(\"{a.first} {n}\")\n", diag.ErrUseAfterMove},
		{"let parameter assigned through a field", "function f(p Pair)\n\tp.first = 1\nend 'f'\n\n", "", diag.ErrAssignToImmutable},
		{"inout argument is a let", "", "\tlet a = Pair.create(1, second: 2)\n\tchange(a)\n", diag.ErrAssignToImmutable},
		{"inout argument rooted at a let parameter", "function f(p Pair)\n\tchange(p)\nend 'f'\n\n", "", diag.ErrAssignToImmutable},
		{"method changes self without inout", "type Cell\n\texport var n as Score\n\tfunction put()\n\t\tn = 1\n\tend 'put'\nend 'Cell'\n\n", "", diag.ErrAssignToImmutable},
		{"let field assigned", "type Cell\n\texport let n = 0 as Score\nend 'Cell'\n\nfunction f(c inout Cell)\n\tc.n = 1\nend 'f'\n\n", "", diag.ErrAssignToImmutable},
		{"return a borrow", "function f(p Pair) returns Pair\n\treturn p\nend 'f'\n\n", "", diag.ErrMoveOutOfBorrow},
		{"bind a borrow", "function f(p inout Pair)\n\tlet q = p\n\tprint(\"{q.first}\")\nend 'f'\n\n", "", diag.ErrMoveOutOfBorrow},
		{"sink a borrow", "function f(p Pair) returns Score\n\treturn take(p)\nend 'f'\n\n", "", diag.ErrMoveOutOfBorrow},
		{"move a field out", "", "\tlet a = Pair.create(1, second: 2)\n\tlet l = a.label\n\tprint(\"{l}\")\n", diag.ErrMoveOutOfField},
		{"swap a variable with itself", "", "\tvar n = 1 as Score\n\tswap(n, b: n)\n", diag.ErrExclusivity},
		{"inout argument and its field", "", "\tvar a = Pair.create(1, second: 2)\n\tchange(a)\n\ta.bump(a.first)\n", diag.ErrExclusivity},
		{"read of an uninitialised variable", "", "\tvar n as Score\n\tprint(\"{n}\")\n", diag.ErrUseBeforeInit},
		{"set parameter left uninitialised", "function f(n set Score, flag bool)\n\tif flag 'x'\n\t\tn = 1\n\tend 'x'\nend 'f'\n\n", "", diag.ErrSetNotInitialized},
		{"set argument already has a value", "", "\tvar n = 1 as Score\n\tmake(n, flag: true)\n", diag.ErrSetArgument},
	} {
		t.Run(tc.name, func(t *testing.T) { expectCodes(t, checkBody(t, tc.decls, tc.body), tc.code) })
	}
}
