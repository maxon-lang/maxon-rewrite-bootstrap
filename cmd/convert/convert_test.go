package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvertSyntaxAndLayout(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "mutable receiver and parameter conventions",
			in: `type Counter
    var value as Count

    function bump(mutable self, delta Count, out mutable Count) returns Count
        self.value = self.value + delta
        out = self.value

        return self.value
    end 'bump'
end 'Counter'
`,
			want: `type Counter
	var value as Count

	function bump(mutates self, delta Count, mutates out Count) returns Count
		self.value = self.value + delta
		out = self.value
		return self.value
	end 'bump'
end 'Counter'
`,
		},
		{
			name: "string and float raw values of an enum and a union convert without a warning",
			in: `enum ContentType
    json = "application/json"
    html = "text/html"
end 'ContentType'

union Reading
    exact(n Count) = 1.5
    rough = -2.5
end 'Reading'
`,
			want: `enum ContentType
	json = "application/json"
	html = "text/html"
end 'ContentType'

union Reading
	exact(n Count) = 1.5
	rough = -2.5
end 'Reading'
`,
		},
		{
			name: "a bits typealias and a union error convert without a warning",
			in: `typealias Word = bits(64)

union ParseFault implements Error
    bad(code Count)
    worse
end 'ParseFault'
`,
			want: `typealias Word = bits(64)

union ParseFault implements Error
	bad(code Count)
	worse
end 'ParseFault'
`,
		},
		{
			name: "a format specifier in a string interpolation converts without a warning",
			in: `function f(n Count)
    print("{n:04x} {n:8.2}")
end 'f'
`,
			want: `function f(n Count)
	print("{n:04x} {n:8.2}")
end 'f'
`,
		},
		{
			name: "a conditional try converts without a warning",
			in: `function f(s String)
    if let digit = try parse(s) 'parsed'
        print("{digit}")
    end 'parsed' else (e) 'failed'
        print("no")
    end 'failed'
    if try check(s) 'checked'
        print("ok")
    end 'checked'
end 'f'
`,
			want: `function f(s String)
	if let digit = try parse(s) 'parsed'
		print("{digit}")
	end 'parsed' else (e) 'failed'
		print("no")
	end 'failed'
	if try check(s) 'checked'
		print("ok")
	end 'checked'
end 'f'
`,
		},
		{
			name: "a byte string converts without a warning",
			in: `function f() returns Count
    let bytes = b"aÿ"
    return bytes.count()
end 'f'
`,
			want: `function f() returns Count
	let bytes = b"aÿ"
	return bytes.count()
end 'f'
`,
		},
		{
			name: "function typealiases and closures convert without a warning",
			in: `typealias Step = function(n mutable Count, Count) returns Count

typealias Bump = function() returns Count mutates captures

function f(s Step) returns Count
    var x = 1 as Count
    return s(x, 2) + [1].count() + g(function(a, b mutable Count) gives a)
end 'f'
`,
			want: `typealias Step = function(mutates Count, Count) returns Count

typealias Bump = function() returns Count mutates captures

function f(s Step) returns Count
	var x = 1 as Count
	return s(x, 2) + [1].count() + g(function(a, mutates b Count) gives a)
end 'f'
`,
		},
		{
			name: "array and map literals convert without a warning",
			in: `function f() returns Count
    let xs = [1, 2, 3]
    let m = ["a": 1]
    return xs[0] + m.count()
end 'f'
`,
			want: `function f() returns Count
	let xs = [1, 2, 3]
	let m = ["a": 1]
	return xs[0] + m.count()
end 'f'
`,
		},
		{
			name: "consuming self without destruct becomes consumes self",
			in: `type Box
	var n as Count

	function take(consuming self) returns Count
		return self.n
	end 'take'
end 'Box'
`,
			want: `type Box
	var n as Count

	function take(consumes self) returns Count
		return self.n
	end 'take'
end 'Box'
`,
		},
		{
			name: "consuming self with destruct self becomes destructs self",
			in: `type Txn
	var id as Count

	function commit(consuming self)
		print("{id}\n")
		destruct self
	end 'commit'
end 'Txn'
`,
			want: `type Txn
	var id as Count

	function commit(destructs self)
		print("{id}\n")
	end 'commit'
end 'Txn'
`,
		},
		{
			name: "a StringBuilder that only gathers text becomes an interpolated String",
			in: `function text(n Count) returns String
	let b = StringBuilder.create()
	b.append("n = ")
	b.append(n)
	return b.build()
end 'text'
`,
			want: `function text(n Count) returns String
	var b = ""
	b = "{b}n = "
	b = "{b}{n}"
	return b
end 'text'
`,
		},
		{
			name: "is unique becomes is noncopyable",
			in: `type Ticket is unique
	var id as Count
end 'Ticket'
`,
			want: `type Ticket is noncopyable
	var id as Count
end 'Ticket'
`,
		},
		{
			name: "a conformance list and a bare typealias argument list are left as Maxon writes them",
			in: `type Square implements Shape, Named
	var side as Count
end 'Square'

typealias Counts = Map with String, Count
`,
			want: `type Square implements Shape, Named
	var side as Count
end 'Square'

typealias Counts = Map with String, Count
`,
		},
		{
			name: "the associated types of an interface with several are parenthesized",
			in: `interface Duo uses A, B
	function first() returns A
end 'Duo'

interface One uses A
	function first() returns A
end 'One'

type Both implements Duo with Count, String, One with (Count, String), Named where T is Duo with Count, T
	var side as Count
end 'Both'
`,
			want: `interface Duo uses A, B
	function first() returns A
end 'Duo'

interface One uses A
	function first() returns A
end 'One'

type Both implements Duo with (Count, String), One with ((Count, String)), Named where T implements Duo with (Count, T)
	var side as Count
end 'Both'
`,
		},
		{
			name: "an interface that extends others, with a static requirement, is left as Maxon writes it",
			in: `interface Derived extends Base, Other uses Element
	static function make() returns Self
	function get() returns Element
end 'Derived'
`,
			want: `interface Derived extends Base, Other uses Element
	static function make() returns Self
	function get() returns Element
end 'Derived'
`,
		},
		{
			name: "a typealias that implements another is left as Maxon writes it",
			in: `typealias NodeIndex = int(0 to 100) implements ElementIndex
`,
			want: `typealias NodeIndex = int(0 to 100) implements ElementIndex
`,
		},
		{
			name: "a typealias in a type's body is left as Maxon writes it",
			in: `type Bin uses Element
	typealias Items = Array with Element
	var items as Items
end 'Bin'

type Holder
	typealias Index = int(0 to 99)
end 'Holder'
`,
			want: `type Bin uses Element
	typealias Items = Array with Element
	var items as Items
end 'Bin'

type Holder
	typealias Index = int(0 to 99)
end 'Holder'
`,
		},
		{
			name: "where T is Bound becomes where T implements Bound",
			in: `function total(a T, b U) uses T, U returns Count where T is Shape and Named, U is Shape
	return a.area() + b.area()
end 'total'
`,
			want: `function total(a T, b U) uses T, U returns Count where T implements Shape and Named, U implements Shape
	return a.area() + b.area()
end 'total'
`,
		},
		{
			name: "try block end followed by otherwise on next line joins onto one line",
			in: `function calc(a Count, b Count) returns Count
	var total = 0 as Count
	try 'calc'
		total = a / b
	end 'calc'
	otherwise (e) 'failed'
		print("{e}\n")
	end 'failed'
	return total
end 'calc'
`,
			want: `function calc(a Count, b Count) returns Count
	var total = 0 as Count
	try 'calc'
		total = a / b
	end 'calc' otherwise (e) 'failed'
		print("{e}\n")
	end 'failed'
	return total
end 'calc'
`,
		},
		{
			name: "freeze prefix and swap with expressions",
			in: `function demo(mutates slot Box, consumes next Box) returns frozen with Box
	let old = swap slot with next
	return freeze old
end 'demo'
`,
			want: `function demo(mutates slot Box, consumes next Box) returns frozen with Box
	let old = exchange(slot, value: next)
	return freeze(old)
end 'demo'
`,
		},
		{
			name: "block comments become line comments",
			in: `/* header comment
 * second line
 */
function f() returns Count
	let x = 1 /* trailing */
	return /* inline */ x
end 'f'
`,
			want: `// header comment
// second line
//
function f() returns Count
	let x = 1 // trailing
	// inline
	return x
end 'f'
`,
		},
		{
			name: "multiline parentheses and struct literals collapse to single line",
			in: `type Point
	var x as Count
	var y as Count
end 'Point'

function make(
	x Count,
	y Count
) returns Point
	return Point{
		x: x,
		y: y
	}
end 'make'
`,
			want: `type Point
	var x as Count
	var y as Count
end 'Point'

function make(x Count, y Count) returns Point
	return Point{x: x, y: y}
end 'make'
`,
		},
		{
			name: "errors of the program's own",
			in:   "function f() returns Count throws ParseError\n\ttry work() otherwise ignore\n\tthrow ParseError.bad\nend 'f'\n",
			want: "function f() returns Count throws ParseError\n\ttry work() otherwise ignore\n\tthrow ParseError.bad\nend 'f'\n",
		},
		{
			name: "a variable named from, a member named from and a label after a handled try are not conversions",
			in:   "function f(from Count) returns Count\n\tlet text = String.from (bytes)\n\tif (try values.get(0) otherwise panic(\"live\")) != from 'differs'\n\t\treturn from + 1\n\tend 'differs'\n\treturn 0\nend 'f'\n",
			want: "function f(from Count) returns Count\n\tlet text = String.from(bytes)\n\tif (try values.get(0) otherwise panic(\"live\")) != from 'differs'\n\t\treturn from + 1\n\tend 'differs'\n\treturn 0\nend 'f'\n",
		},
		{
			name: "a for loop over a variable named like a view, the cases of an enum and the view of a literal is walked",
			in: `function f(lines Array with String)
	for line in lines 'each'
		print(line)
	end 'each'
	for c in Color.allCases 'cases'
		print("{c}")
	end 'cases'
	for u in "abc".utf16() 'views'
		print("{u}")
	end 'views'
end 'f'
`,
			want: `function f(lines Array with String)
	for line in lines 'each'
		print(line)
	end 'each'
	for c in Color.allCases 'cases'
		print("{c}")
	end 'cases'
	for u in "abc".utf16() 'views'
		print("{u}")
	end 'views'
end 'f'
`,
		},
		{
			name: "a member named from and a literal after a from are not unsupported conversions",
			in:   "function f() returns Count\n\tlet text = String.from (bytes)\n\tlet path = FilePath from \"a.txt\"\n\tlet xs = Ages from [1, 2]\n\treturn 0\nend 'f'\n",
			want: "function f() returns Count\n\tlet text = String.from(bytes)\n\tlet path = FilePath from \"a.txt\"\n\tlet xs = Ages from [1, 2]\n\treturn 0\nend 'f'\n",
		},
		{
			name: "a match arm that throws becomes an arm that runs a throw",
			in:   "function f(c Color) returns Count throws ColorError\n\treturn match c 'which'\n\t\tred throws ColorError.unsupported\n\t\tdefault panic(\"the other colors are handled\")\n\tend 'which'\nend 'f'\n",
			want: "function f(c Color) returns Count throws ColorError\n\treturn match c 'which'\n\t\tred then throw ColorError.unsupported\n\t\tdefault panic(\"the other colors are handled\")\n\tend 'which'\nend 'f'\n",
		},
		{
			name: "a block indented two tabs, and a level indented two tabs deeper, keep one tab per level",
			in:   "function f() returns Count\n\t\tlet a = 1\n\t\tif a == 1 'one'\n\t\t\t\t\treturn 2\n\t\tend 'one'\n\t\treturn a\nend 'f'\n",
			want: "function f() returns Count\n\tlet a = 1\n\tif a == 1 'one'\n\t\treturn 2\n\tend 'one'\n\treturn a\nend 'f'\n",
		},
		{
			name: "a discard binding loses its let",
			in:   "function f(c Color)\n\tlet _ = c\n\t_ = c\n\tmatch c 'm'\n\t\tred then let _ = c\n\t\tdefault then return\n\tend 'm'\nend 'f'\n",
			want: "function f(c Color)\n\t_ = c\n\t_ = c\n\tmatch c 'm'\n\t\tred then _ = c\n\t\tdefault then return\n\tend 'm'\nend 'f'\n",
		},
		{
			name: "tuples convert as they are written",
			in:   "typealias Pair = (Count, Count)\n\nfunction swap(t Pair) returns (Count, Count)\n\tlet (a, b) = t\n\tvar x = a\n\tvar y = b\n\t(x, y) = (b, a)\n\tfor (p, q) in pairs() 'each'\n\t\tprint(\"{p.0}\")\n\tend 'each'\n\treturn (x, t.1)\nend 'swap'\n",
			want: "typealias Pair = (Count, Count)\n\nfunction swap(t Pair) returns (Count, Count)\n\tlet (a, b) = t\n\tvar x = a\n\tvar y = b\n\t(x, y) = (b, a)\n\tfor (p, q) in pairs() 'each'\n\t\tprint(\"{p.0}\")\n\tend 'each'\n\treturn (x, t.1)\nend 'swap'\n",
		},
		{
			name: "CRLF, BOM, trailing spaces, and extra blank lines",
			in:   "\xef\xbb\xbflet a = 1  \r\n\r\n\r\nlet b = 2\t\r\n",
			want: "let a = 1\n\nlet b = 2\n",
		},
		{
			name: "a refused character becomes an escape in a literal and its code point in a comment",
			in:   "// bell\x07 and \xe2\x80\xae\nlet s = \"a\x1bb\xe2\x80\xa8\"\n\nlet t = b\"\x01\xc2\x85\"\n",
			want: "// bellU+0007 and U+202E\nlet s = \"a\\u001bb\\u2028\"\n\nlet t = b\"\\x01\\x85\"\n",
		},
		{
			name: "the alternatives of an or chain stand one under another",
			in:   "function f(x Count) returns Count\n\tmatch x 'm'\n\t\t1 or\n\t\t\t2 or\n\t\t\t3 then return 1\n\t\tdefault then return 0\n\tend 'm'\nend 'f'\n",
			want: "function f(x Count) returns Count\n\tmatch x 'm'\n\t\t1 or\n\t\t2 or\n\t\t3 then return 1\n\t\tdefault then return 0\n\tend 'm'\nend 'f'\n",
		},
		{
			name: "withIterator becomes enumerated, and the iterator's index() the index it binds",
			in:   "function f(xs Row)\n\tfor (iter, x) in xs.withIterator() 'each'\n\t\tfor (iter, y) in xs.withIterator() 'inner'\n\t\t\tprint(\"{iter.index()} {y}\")\n\t\tend 'inner'\n\t\tprint(\"{iter.index()} {x}\")\n\tend 'each'\nend 'f'\n",
			want: "function f(xs Row)\n\tfor (iter, x) in xs.enumerated() 'each'\n\t\tfor (iter, y) in xs.enumerated() 'inner'\n\t\t\tprint(\"{iter} {y}\")\n\t\tend 'inner'\n\t\tprint(\"{iter} {x}\")\n\tend 'each'\nend 'f'\n",
		},
		{
			name: "a library declaration of a built-in name is left out with its comment",
			in:   "// The error of iteration.\npublic enum IterationError implements Error\n\texhausted\nend 'IterationError'\n\npublic typealias Count = int(0 to u64.max)\n\npublic enum Ordering\n\tlessThan\nend 'Ordering'\n",
			want: "public enum Ordering\n\tlessThan\nend 'Ordering'\n",
		},
		{
			name: "Iterable gets the where that relates its iterator to its element, and its extension loses withIterator",
			in:   "public interface Iterable uses Element, Iter\n\tfunction createIterator() returns Iter throws IterationError\nend 'Iterable'\n\npublic extension Iterable\n\tpublic typealias WithIterSelf = WithIterIterator with Iter, Element\n\n\tpublic function count() returns Count\n\t\treturn 0\n\tend 'count'\n\n\tpublic function withIterator() returns WithIterSelf throws IterationError\n\t\tlet iter = try self.createIterator()\n\t\treturn WithIterSelf.create(iter)\n\tend 'withIterator'\nend 'Iterable'\n",
			want: "public interface Iterable uses Element, Iter where Iter implements Iterator with Element\n\tfunction createIterator() returns Iter throws IterationError\nend 'Iterable'\n\npublic extension Iterable\n\tpublic function count() returns Count\n\t\treturn 0\n\tend 'count'\nend 'Iterable'\n",
		},
		{
			name: "Iterator's current() lends its element",
			in:   "public interface Iterator uses Element\n\tfunction current() returns Element\n\tfunction advance(mutable self) throws IterationError\nend 'Iterator'\n",
			want: "public interface Iterator uses Element\n\tfunction current() returns borrowed Element\n\tfunction advance(mutates self) throws IterationError\nend 'Iterator'\n",
		},
		{
			name: "a capability word before a type is left out",
			in:   "typealias Keys = Array with immutable Key\n\ntype Table uses Key, Value\n\ttypealias Entry = (immutable Key, Value)\n\tvar slots as Map with (immutable Key, Array with isolated Value)\n\n\tfunction put(key immutable Key, _ isolated Key) returns immutable Key\n\t\treturn key as isolated Key\n\tend 'put'\nend 'Table'\n",
			want: "typealias Keys = Array with Key\n\ntype Table uses Key, Value\n\ttypealias Entry = (Key, Value)\n\tvar slots as Map with (Key, Array with Value)\n\n\tfunction put(key Key, consumes _ Key) returns Key\n\t\treturn key as Key\n\tend 'put'\nend 'Table'\n",
		},
		{
			name: "immutable before a concrete type or a type parameter's instance is a frozen value",
			in:   "type Table uses Key\n\tfunction put(key immutable Box, keys immutable Array with Key) returns immutable Box\n\t\treturn key\n\tend 'put'\nend 'Table'\n\nfunction pick(a immutable T, b immutable Box) uses T returns immutable T\n\treturn a\nend 'pick'\n",
			want: "type Table uses Key\n\tfunction put(key frozen with Box, keys frozen with Array with Key) returns frozen with Box\n\t\treturn key\n\tend 'put'\nend 'Table'\n\nfunction pick(a T, b frozen with Box) uses T returns T\n\treturn a\nend 'pick'\n",
		},
		{
			name: "a capability word on a parameter's or a result's own type maps, a nested one is left out",
			in:   "function f(a isolated Box, b mutable immutable Box, c Array with immutable Key) returns isolated (immutable Key, Value)\n\tdo()\nend 'f'\n",
			want: "function f(consumes a Box, mutates b frozen with Box, c Array with Key) returns (Key, Value)\n\tdo()\nend 'f'\n",
		},
		{
			name: "a capability word that names a function or a variable is kept",
			in:   "function isolated(x Count) returns bool\n\tlet immutable = x == 4\n\treturn not immutable (x) and isolated (x)\nend 'isolated'\n",
			want: "function isolated(x Count) returns bool\n\tlet immutable = x == 4\n\treturn not immutable(x) and isolated(x)\nend 'isolated'\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Convert("test.maxon", tc.in, sourceFile)
			if len(res.Warnings) > 0 {
				t.Fatalf("unexpected warnings: %v", res.Warnings)
			}
			if !res.Formatted {
				t.Fatalf("expected strict minimaxon formatting to succeed, got:\n%s", res.Text)
			}
			if res.Text != tc.want {
				t.Fatalf("mismatch:\ngot:\n%q\nwant:\n%q", res.Text, tc.want)
			}
		})
	}
}

// TestConvertEarlyDestructIsWarned covers a `destruct self` that is not the last statement of its destructor: a destructor
// ends its receiver when it returns, so removing the statement would leave the code after it, or the next lap of its loop,
// with nothing to test.
func TestConvertEarlyDestructIsWarned(t *testing.T) {
	cases := map[string]string{
		"code after it": "type Q\n\tvar n as Count\n\n\tfunction fin(consuming self)\n\t\tdestruct self\n\t\tprint(\"{self.n}\")\n\tend 'fin'\nend 'Q'\n",
		"in a loop":     "type Q\n\tvar n as Count\n\n\tfunction fin(consuming self)\n\t\twhile n != 0 'w'\n\t\t\tdestruct self\n\t\tend 'w'\n\tend 'fin'\nend 'Q'\n",
	}
	for name, in := range cases {
		res := Convert("test.maxon", in, sourceFile)
		if len(res.Warnings) != 1 || res.Warnings[0].Line != 4 || !strings.Contains(res.Warnings[0].Message, "destruct self") {
			t.Errorf("%s: warnings %v", name, res.Warnings)
		}
	}
}

func TestConvertTupleElementSpelling(t *testing.T) {
	in := "function f(p (Count, Count)) returns Count\n\treturn p._0 + p._1\nend 'f'\n"
	want := "function f(p (Count, Count)) returns Count\n\treturn p.0 + p.1\nend 'f'\n"
	if got := Convert("test.maxon", in, sourceFile).Text; got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestConvertToStringIsInterpolation(t *testing.T) {
	in := "function f(n Count, xs Words) returns String\n\tlet a = n.toString()\n\tlet b = xs[0].count().toString().byteLength()\n\treturn \"{a.toString()} {(n + 1).toString()}\" + self.name.toString()\nend 'f'\n"
	want := "function f(n Count, xs Words) returns String\n\tlet a = \"{n}\"\n\tlet b = \"{xs[0].count()}\".byteLength()\n\treturn \"{a} {(n + 1)}\" + \"{self.name}\"\nend 'f'\n"
	if got := Convert("test.maxon", in, sourceFile).Text; got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestConvertHelperPublicIsExport(t *testing.T) {
	in := "public type Cursor\n\tpublic static function over() returns Count\n\t\treturn 0\n\tend 'over'\nend 'Cursor'\n"
	want := "export type Cursor\n\texport static function over() returns Count\n\t\treturn 0\n\tend 'over'\nend 'Cursor'\n"
	if got := Convert("maxon/stdlib/helpers/sort/Cursor.maxon", in, sourceFile).Text; got != want {
		t.Fatalf("a helper:\ngot:\n%q\nwant:\n%q", got, want)
	}
	if got := Convert("maxon/stdlib/Cursor.maxon", in, sourceFile).Text; got != in {
		t.Fatalf("a root file:\ngot:\n%q\nwant:\n%q", got, in)
	}
}

// TestConvertLibraryRules covers what a file of the library becomes by what the rest of the library declares: a member
// whose parameters or typealias target name a helper's type is export, a type minimaxon builds in keeps its conformances
// alone, and a literal marker leaves the implements clause.
func TestConvertLibraryRules(t *testing.T) {
	root := filepath.Join(t.TempDir(), "stdlib")
	write := func(rel, text string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("helpers/table/slots.maxon", "public enum Slot\n\tempty\nend 'Slot'\n")
	write("Interfaces.maxon", "public interface Hashable\n\tfunction hash() returns HashValue\nend 'Hashable'\n\npublic interface Cloneable\n\tfunction clone() returns Self\nend 'Cloneable'\n\npublic interface BuiltinStringLiteral\n\tstatic function init(value Count) returns Self\nend 'BuiltinStringLiteral'\n\npublic interface Iterable uses Element, Iter\n\tfunction createIterator() returns Iter throws IterationError\nend 'Iterable'\n")
	cases := []struct{ name, file, in, want string }{
		{
			name: "a member naming a helper's type is export, and so is one naming that member",
			file: "Table.maxon",
			in:   "public type Table implements BuiltinDictionaryLiteral, Hashable\n\tpublic typealias Slots = Array with Slot\n\n\tpublic static function make(slots Slots) returns Count\n\t\treturn 0\n\tend 'make'\n\n\tpublic function slot() returns Slot\n\t\treturn Slot.empty\n\tend 'slot'\nend 'Table'\n",
			want: "public type Table implements Hashable\n\texport typealias Slots = Array with Slot\n\n\texport static function make(slots Slots) returns Count\n\t\treturn 0\n\tend 'make'\n\n\tpublic function slot() returns Slot\n\t\treturn Slot.empty\n\tend 'slot'\nend 'Table'\n",
		},
		{
			name: "a built-in type keeps the conformances it can declare, with the methods it lacks",
			file: "String.maxon",
			in:   "let Unused = 1\n\npublic type String implements BuiltinStringLiteral, Hashable, Cloneable, Iterable with (Count, Count)\n\tpublic function hash() returns HashValue\n\t\treturn hashText(self)\n\tend 'hash'\n\n\tpublic function clone() returns String\n\t\treturn self\n\tend 'clone'\nend 'String'\n\nfunction hashText(s String) returns HashValue\n\treturn 0\nend 'hashText'\n",
			want: "extension String implements Hashable, Cloneable\n\tpublic function hash() returns HashValue\n\t\treturn hashText(self)\n\tend 'hash'\nend 'String'\n\nfunction hashText(s String) returns HashValue\n\treturn 0\nend 'hashText'\n",
		},
		{
			name: "an extension of a built-in generic type names its parameter as the table does, keeps its conditional conformance and asks for the storage hash as minimaxon does",
			file: "Array.maxon",
			in:   "public type Array uses Element implements Iterable with (Element, ArrayIter)\n\tpublic typealias ArrayIter = ArrayIterator with Element\n\tpublic typealias ElementMemory = __ManagedMemory with Element\n\n\tpublic function count() returns Count\n\t\treturn 0\n\tend 'count'\n\n\tpublic function appendMemory(source ElementMemory)\n\t\tmanaged.append(source)\n\tend 'appendMemory'\n\n\tpublic function createIterator() returns ArrayIter throws IterationError\n\t\treturn try ArrayIter.create(managed)\n\tend 'createIterator'\n\n\tpublic function cursor() returns ArrayIter throws IterationError\n\t\treturn try ArrayIter.create(managed)\n\tend 'cursor'\nend 'Array'\n\npublic extension Array implements Hashable where Element implements Hashable\n\tpublic function hash() returns HashValue\n\t\tif __Builtins.elementsHashAsStorage() 'storage'\n\t\t\treturn __Builtins.hashArrayStorage(self)\n\t\tend 'storage'\n\t\treturn managed.length() as HashValue\n\tend 'hash'\n\n\tpublic function count() returns Count\n\t\treturn 0\n\tend 'count'\nend 'Array'\n",
			want: "extension Array implements Iterable with (T, ArrayIter)\n\tpublic typealias ArrayIter = ArrayIterator with T\n\tpublic typealias ElementMemory = __ManagedMemory with T\n\n\tpublic function appendMemory(source ElementMemory)\n\t\tself.managed.append(source)\n\tend 'appendMemory'\n\n\tpublic function createIterator() returns ArrayIter throws IterationError\n\t\treturn try ArrayIter.create(self.managed)\n\tend 'createIterator'\n\n\tpublic function cursor() returns ArrayIter throws IterationError\n\t\treturn try ArrayIter.create(self.managed)\n\tend 'cursor'\nend 'Array'\n\npublic extension Array implements Hashable where T implements Hashable\n\tpublic function hash() returns HashValue\n\t\tif self.hashesAsStorage() 'storage'\n\t\t\treturn hashValueOfDigest(self.storageHash(processFoldStart()))\n\t\tend 'storage'\n\t\treturn self.managed.length() as HashValue\n\tend 'hash'\nend 'Array'\n",
		},
		{
			name: "a region that declares a built-in name for each system goes with it",
			file: "Region.maxon",
			in:   "#if os(Windows)\n\tpublic typealias ExitCode = int(0 to u32.max)\n#else\n\tpublic typealias ExitCode = int(0 to 255)\n#endif\n\npublic let Kept = 1\n",
			want: "public let Kept = 1\n",
		},
		{
			name: "a method of a built-in record that calls a raw function the table lacks goes with what only it reaches",
			file: "Process.maxon",
			in:   "public let Limit = 65536\n\npublic let Kept = 1\n\npublic type Process\n\tpublic static function dropped() returns Count\n\t\treturn Limit + helper(__Builtins.notProvided())\n\tend 'dropped'\n\n\tstatic function helper(n Count) returns Count\n\t\treturn n\n\tend 'helper'\n\n\tpublic static function keeps() returns Count\n\t\treturn Kept\n\tend 'keeps'\nend 'Process'\n",
			want: "public let Kept = 1\n\nextension Process\n\tpublic static function keeps() returns Count\n\t\treturn Kept\n\tend 'keeps'\nend 'Process'\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Convert(filepath.Join(root, tc.file), tc.in, sourceFile)
			if len(res.Warnings) > 0 || !res.Formatted {
				t.Fatalf("warnings %v, formatted %v:\n%s", res.Warnings, res.Formatted, res.Text)
			}
			if res.Text != tc.want {
				t.Fatalf("mismatch:\ngot:\n%q\nwant:\n%q", res.Text, tc.want)
			}
		})
	}
}

func TestConvertUnsupportedConstructsEmitTodosAndWarnings(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantTodo string
	}{
		{
			name:     "caller-location default",
			in:       "function check(ok bool, from String = __file__)\n\tprint(from)\nend 'check'\n",
			wantTodo: "the caller-location default '__file__'",
		},
		{
			name:     "typealias in an enum",
			in:       "enum Color\n\ttypealias Tint = int(0 to 3)\n\tred\nend 'Color'\n",
			wantTodo: "typealias declarations in an enum or union",
		},
		{
			name:     "for loop over a string's lines",
			in:       "function f(s String)\n\tfor b in s.lines() 'each'\n\t\tprint(\"{b}\")\n\tend 'each'\nend 'f'\n",
			wantTodo: "for loops over graphemes(), characters(), split() or lines() of a string",
		},
		{
			name:     "byte string spanning lines",
			in:       "let Newline = b\"\n\"\n",
			wantTodo: "a byte string literal that spans lines",
		},
		{
			name:     "block otherwise on try expr inside brackets",
			in:       "function f() returns Count\n\treturn g(try work() otherwise (e) 'fail')\nend 'f'\n",
			wantTodo: "a block handler ('otherwise (e) 'label'') inside brackets",
		},
		{
			name:     "empty array literal",
			in:       "function f() returns Count\n\tlet xs = []\n\treturn 0\nend 'f'\n",
			wantTodo: "an empty array literal",
		},
		{
			name:     "Type from value",
			in:       "function f(text String) returns Path\n\treturn Path from text\nend 'f'\n",
			wantTodo: "'Type from value' takes an array literal or a string literal",
		},
		{
			name:     "a parameter named by a keyword that starts a statement",
			in:       "function f(return Count) returns Count\n\treturn 0\nend 'f'\n",
			wantTodo: "a parameter named 'return' cannot be read",
		},
		{
			name:     "extension of a library type with no counterpart",
			in:       "extension Set\n\tfunction x() returns Count\n\t\treturn 0\n\tend 'x'\nend 'Set'\n",
			wantTodo: "extensions of 'Set'",
		},
		{
			name:     "qualified case names in a handler's match",
			in:       "function f()\n\ttry g() otherwise (e) 'h'\n\t\tmatch e 'k'\n\t\t\tErrA.bad then return\n\t\tend 'k'\n\tend 'h'\nend 'f'\n",
			wantTodo: "combined error of several errors",
		},
		{
			name:     "a block inside brackets",
			in:       "function f(u Count) returns Count\n\tlet n = (match u 'm'\n\t\t0 gives 1\n\t\tdefault gives 2\n\tend 'm') if u != 0 else 0\n\treturn n\nend 'f'\n",
			wantTodo: "a block inside brackets",
		},
		{
			name:     "a case named end",
			in:       "enum Kw\n\talpha\n\tend\nend 'Kw'\n",
			wantTodo: "'end' is a keyword in minimaxon",
		},
		{
			name:     "reply.later",
			in:       "function f()\n\treply.later()\nend 'f'\n",
			wantTodo: "reply.later() is not supported",
		},
		{
			name:     "withIterator's iterator used for more than its index",
			in:       "function f(xs Row)\n\tfor (iter, x) in xs.withIterator() 'each'\n\t\tprint(\"{iter.current()}\")\n\tend 'each'\nend 'f'\n",
			wantTodo: "a use of withIterator()'s iterator other than its index()",
		},
		{
			name:     "the identity operator",
			in:       "function f(a String, b String) returns bool\n\treturn a is not b\nend 'f'\n",
			wantTodo: "minimaxon has no identity operator",
		},
		{
			name:     "the identity operator in an interpolation",
			in:       "function f(a String, b String)\n\tprint(\"{a is b}\")\nend 'f'\n",
			wantTodo: "minimaxon has no identity operator",
		},
		{
			name:     "reserved word collision",
			in:       "function f(when Count)\n\tlet sets = when\nend 'f'\n",
			wantTodo: "keyword in minimaxon",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Convert("test.maxon", tc.in, sourceFile)
			if len(res.Warnings) == 0 {
				t.Fatalf("expected warning containing %q, got none\noutput:\n%s", tc.wantTodo, res.Text)
			}
			foundWarn := false
			for _, w := range res.Warnings {
				if strings.Contains(w.Message, tc.wantTodo) {
					foundWarn = true
					break
				}
			}
			if !foundWarn {
				t.Fatalf("expected warning containing %q, got %v", tc.wantTodo, res.Warnings)
			}
			if !strings.Contains(res.Text, "// TODO: ") || !strings.Contains(res.Text, tc.wantTodo) {
				t.Fatalf("expected inline // TODO containing %q in output:\n%s", tc.wantTodo, res.Text)
			}
		})
	}
}

func TestCheckSummarizesWarningsByKind(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.maxon", "#!/usr/bin/env maxon\nlet a = 1\n")
	write("b.maxon", "#!/usr/bin/env maxon\nlet b = 1\n")
	write("c.maxon", "let c = 3\n")

	var stdout, stderr bytes.Buffer
	code := runMain([]string{"--check", dir}, strings.NewReader(""), &stdout, &stderr)
	want := "     2    2 files  shebang lines are not supported in minimaxon\n2 of 3 files would change\n"
	if code != 1 || stdout.String() != want {
		t.Fatalf("code=%d stdout=%q want %q", code, stdout.String(), want)
	}
}

func TestCLIModes(t *testing.T) {
	dir := t.TempDir()
	srcFile := filepath.Join(dir, "sample.maxon")
	input := "type Box is unique\n    var n as Count\nend 'Box'\n"
	want := "type Box is noncopyable\n\tvar n as Count\nend 'Box'\n"
	if err := os.WriteFile(srcFile, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}

	// Single file to stdout.
	var stdout, stderr bytes.Buffer
	code := runMain([]string{srcFile}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 || stdout.String() != want || stderr.Len() != 0 {
		t.Fatalf("stdout mode: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	// --check fails when changes needed.
	stdout.Reset()
	stderr.Reset()
	code = runMain([]string{"--check", srcFile}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected --check exit 1, got %d", code)
	}

	// --out writes converted file to output directory.
	outDir := filepath.Join(dir, "out")
	stdout.Reset()
	stderr.Reset()
	code = runMain([]string{"--out=" + outDir, srcFile}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("--out failed: code=%d stderr=%q", code, stderr.String())
	}
	gotOut, err := os.ReadFile(filepath.Join(outDir, "sample.maxon"))
	if err != nil || string(gotOut) != want {
		t.Fatalf("--out file: err=%v got=%q want=%q", err, string(gotOut), want)
	}

	// --write updates in place, and subsequent --check passes.
	stdout.Reset()
	stderr.Reset()
	code = runMain([]string{"--write", srcFile}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("--write failed: code=%d stderr=%q", code, stderr.String())
	}
	gotInPlace, err := os.ReadFile(srcFile)
	if err != nil || string(gotInPlace) != want {
		t.Fatalf("--write file: err=%v got=%q want=%q", err, string(gotInPlace), want)
	}
	code = runMain([]string{"--check", srcFile}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected --check exit 0 after --write, got %d", code)
	}
}

// TestConvertRenamesMaxonsSlotAsCell: maxon's library type Slot is Cell here, in its own file (with its comments and
// messages), in the library and in a program that names it, while a program's own Slot, in any of its files, is kept.
func TestConvertRenamesMaxonsSlotAsCell(t *testing.T) {
	root := filepath.Join(t.TempDir(), "stdlib")
	own := Convert(filepath.Join(root, "Slot.maxon"), "/// An empty slot.\npublic type Slot uses T\n\tlet name as String\n\n\tpublic function fail()\n\t\tpanic(\"Slot '{self.name}' is full\")\n\tend 'fail'\nend 'Slot'\n", sourceFile)
	if want := "/// An empty cell.\npublic type Cell uses T\n\tlet name as String\n\n\tpublic function fail()\n\t\tpanic(\"Cell '{self.name}' is full\")\n\tend 'fail'\nend 'Cell'\n"; own.Text != want {
		t.Errorf("the type's own file:\ngot:\n%q\nwant:\n%q", own.Text, want)
	}
	user := Convert(filepath.Join(root, "Server.maxon"), "typealias Pending = Slot with int\n", sourceFile)
	if want := "typealias Pending = Cell with int\n"; user.Text != want {
		t.Errorf("a library file that names it:\ngot %q\nwant %q", user.Text, want)
	}

	spec := "## Tests\n\n" +
		"<!-- test: library -->\n```maxon\ntypealias Held = Slot with int\n```\n\n" +
		"<!-- test: own-in-another-file -->\n```maxon\n// --- file: lib/a.maxon\ntypealias Slot = int(0 to 9)\n\n// --- file: main.maxon\nfunction f(s Slot) returns Slot\n\treturn s\nend 'f'\n```\n\n" +
		"<!-- test: own-parameter -->\n```maxon\ntype Box uses Slot\n\tlet item as Slot\nend 'Box'\n```\n"
	want := "## Tests\n\n" +
		"<!-- test: library -->\n```maxon\ntypealias Held = Cell with int\n```\n\n" +
		"<!-- test: own-in-another-file -->\n```maxon\n// --- file: lib/a.maxon\ntypealias Slot = int(0 to 9)\n\n// --- file: main.maxon\nfunction f(s Slot) returns Slot\n\treturn s\nend 'f'\n```\n\n" +
		"<!-- test: own-parameter -->\n```maxon\ntype Box uses Slot\n\tlet item as Slot\nend 'Box'\n```\n"
	if got := ConvertSpec("demo.md", spec).Text; got != want {
		t.Errorf("spec tests:\ngot:\n%q\nwant:\n%q", got, want)
	}
}

// TestConvertExportsWhatOtherModulesName: an unmarked declaration that a file of another module of the listing, or one it
// does not list, names becomes export, with the types that its signature names; one that only its own module names, or that
// a file outside the directory it is visible to names, stays as it is.
func TestConvertExportsWhatOtherModulesName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "stdlib")
	write := func(rel, text string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Paths.maxon", "")
	write("Sibling.maxon", "function sibling() returns Count\n\treturn sameModule()\nend 'sibling'\n")
	write("Later.maxon", "function later() returns String\n\treturn \"{path.rootLength()}\"\nend 'later'\n")
	write("helpers/area/Inside.maxon", "function inside() returns Count\n\treturn 0\nend 'inside'\n")
	write("helpers/other/Outside.maxon", "function outside() returns Count\n\treturn insideOnly()\nend 'outside'\n")
	saved := libraryListing
	defer func() { libraryListing = saved }()
	libraryListing = map[string]string{"Paths.maxon": "paths", "Sibling.maxon": "paths", "helpers/area/Inside.maxon": "area", "helpers/other/Outside.maxon": "other"}
	in := "enum Rule\n\tkeep\nend 'Rule'\n\n" +
		"type Path\n\tlet text as String\n\n\tfunction rootLength(rule Rule) returns Count\n\t\treturn 0\n\tend 'rootLength'\n\n" +
		"\tfunction sameModule() returns Count\n\t\treturn 0\n\tend 'sameModule'\n\n" +
		"\tpublic function visible() returns Count\n\t\treturn 0\n\tend 'visible'\n\n" +
		"\tfunction hidden() returns Count\n\t\t#if os(Windows)\n\t\t\tfunction local() returns Count\n\t\t\t\treturn 0\n\t\t\tend 'local'\n\t\t#endif\n\t\treturn 0\n\tend 'hidden'\nend 'Path'\n"
	want := "export enum Rule\n\tkeep\nend 'Rule'\n\n" +
		"type Path\n\tlet text as String\n\n\texport function rootLength(rule Rule) returns Count\n\t\treturn 0\n\tend 'rootLength'\n\n" +
		"\tfunction sameModule() returns Count\n\t\treturn 0\n\tend 'sameModule'\n\n" +
		"\tpublic function visible() returns Count\n\t\treturn 0\n\tend 'visible'\n\n" +
		"\tfunction hidden() returns Count\n\t\t#if os(Windows)\n\t\t\tfunction local() returns Count\n\t\t\t\treturn 0\n\t\t\tend 'local'\n\t\t#endif\n\t\treturn 0\n\tend 'hidden'\nend 'Path'\n"
	if got := Convert(filepath.Join(root, "Paths.maxon"), in, sourceFile).Text; got != want {
		t.Errorf("a file of the root:\ngot:\n%s\nwant:\n%s", got, want)
	}
	helper := "function insideOnly() returns Count\n\treturn 0\nend 'insideOnly'\n"
	if got := Convert(filepath.Join(root, "helpers", "area", "Inside.maxon"), helper, sourceFile).Text; got != helper {
		t.Errorf("a helper that only a file outside its directory names:\n%s", got)
	}
}

// TestConvertDeclaresMaxonsSubprocessBesideTheBuiltIns: the types of maxon's Subprocess that minimaxon also builds in are
// declared by the library under the prefix Mx, a method that calls a raw function the table lacks goes with the method
// that calls it, and a cstring of a toString() views the path field, which outlives it.
func TestConvertDeclaresMaxonsSubprocessBesideTheBuiltIns(t *testing.T) {
	root := filepath.Join(t.TempDir(), "stdlib")
	in := "public typealias WaitDeadlineMs = int(1 to u64.max)\n\n" +
		"/// The deadline of a Subprocess.\npublic union WaitDeadline\n\tnone\n\twithin(timeoutMs WaitDeadlineMs)\nend 'WaitDeadline'\n\n" +
		"public type Subprocess\n\tpublic static function run(deadline WaitDeadline) returns Count\n\t\tpanic(\"Subprocess.run: {deadline}\")\n\tend 'run'\n\n" +
		"\tpublic static function runDetached() returns Count\n\t\treturn __Builtins.subprocessDetach(0)\n\tend 'runDetached'\nend 'Subprocess'\n\n" +
		"public type Configuration\n\tpublic function run() returns Count\n\t\treturn Subprocess.run(WaitDeadline.none)\n\tend 'run'\n\n" +
		"\tpublic function runDetached() returns Count\n\t\treturn Subprocess.runDetached()\n\tend 'runDetached'\nend 'Configuration'\n\n" +
		"function operand(path FilePath) returns cstring\n\treturn path.toString().cstr()\nend 'operand'\n"
	want := "public typealias WaitDeadlineMs = int(1 to u64.max)\n\n" +
		"/// The deadline of a MxSubprocess.\npublic union MxWaitDeadline\n\tnone\n\twithin(timeoutMs WaitDeadlineMs)\nend 'MxWaitDeadline'\n\n" +
		"public type MxSubprocess\n\tpublic static function run(deadline MxWaitDeadline) returns Count\n\t\tpanic(\"MxSubprocess.run: {deadline}\")\n\tend 'run'\nend 'MxSubprocess'\n\n" +
		"public type Configuration\n\tpublic function run() returns Count\n\t\treturn MxSubprocess.run(MxWaitDeadline.none)\n\tend 'run'\nend 'Configuration'\n\n" +
		"function operand(path FilePath) returns cstring\n\treturn path.path.cstr()\nend 'operand'\n"
	res := Convert(filepath.Join(root, "Subprocess.maxon"), in, sourceFile)
	if len(res.Warnings) > 0 || !res.Formatted {
		t.Fatalf("warnings %v, formatted %v:\n%s", res.Warnings, res.Formatted, res.Text)
	}
	if res.Text != want {
		t.Fatalf("got:\n%q\nwant:\n%q", res.Text, want)
	}
}
