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
			name: "a typealias in a type's body is left as Maxon writes it",
			in: `type Pool uses Element
	typealias Items = Array with Element
	var items as Items
end 'Pool'

type Holder
	typealias Index = int(0 to 99)
end 'Holder'
`,
			want: `type Pool uses Element
	typealias Items = Array with Element
	var items as Items
end 'Pool'

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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Convert("test.maxon", tc.in)
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

func TestConvertUnsupportedConstructsEmitTodosAndWarnings(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantTodo string
	}{
		{
			name:     "alias subtyping",
			in:       "typealias NodeIndex = int(0 to 100) implements ElementIndex\n",
			wantTodo: "alias subtyping",
		},
		{
			name:     "ranged typealias in a generic type",
			in:       "type Pool uses Element\n\ttypealias Idx = int(0 to u64.max)\nend 'Pool'\n",
			wantTodo: "ranged typealiases in a generic type",
		},
		{
			name:     "typealias in an interface",
			in:       "interface Seq\n\ttypealias Items = Array with Count\nend 'Seq'\n",
			wantTodo: "typealias declarations in an interface, enum or union",
		},
		{
			name:     "for loop over a string's view",
			in:       "function f(s String)\n\tfor b in s.lines() 'each'\n\t\tprint(\"{b}\")\n\tend 'each'\nend 'f'\n",
			wantTodo: "iterator-based for loops over a string",
		},
		{
			name:     "for loop over a string literal",
			in:       "function f()\n\tfor c in \"abc\" 'each'\n\t\tprint(\"{c}\")\n\tend 'each'\nend 'f'\n",
			wantTodo: "iterator-based for loops over a string",
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
			name:     "union error",
			in:       "union ParseFault implements Error\n\tbad(code Count)\n\tworse\nend 'ParseFault'\n",
			wantTodo: "union errors",
		},
		{
			name:     "default throws",
			in:       "function f(c Color) returns Count throws ColorError\n\treturn match c 'which'\n\t\tred gives 1\n\t\tdefault throws ColorError.unsupported\n\tend 'which'\nend 'f'\n",
			wantTodo: "'default throws' in a match",
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
			name:     "keyword as a function name",
			in:       "type Attr\n\tstatic function int(key String) returns Attr\n\t\treturn Attr{}\n\tend 'int'\nend 'Attr'\n",
			wantTodo: "'int' is a keyword in minimaxon",
		},
		{
			name:     "keyword as a parameter name",
			in:       "function f(otherwise Count) returns Count\n\treturn otherwise\nend 'f'\n",
			wantTodo: "'otherwise' is a keyword in minimaxon",
		},
		{
			name:     "conditional extension",
			in:       "extension Point where Item is Equatable\n\tfunction x() returns Count\n\t\treturn 0\n\tend 'x'\nend 'Point'\n",
			wantTodo: "conditional extensions",
		},
		{
			name:     "extension of an interface",
			in:       "interface Shape\n\tfunction area() returns Count\nend 'Shape'\n\nextension Shape\n\tfunction x() returns Count\n\t\treturn 0\n\tend 'x'\nend 'Shape'\n",
			wantTodo: "extensions of an interface",
		},
		{
			name:     "extension of a library type with no counterpart",
			in:       "extension FixedArray\n\tfunction x() returns Count\n\t\treturn 0\n\tend 'x'\nend 'FixedArray'\n",
			wantTodo: "extensions of 'FixedArray'",
		},
		{
			name:     "hash if",
			in:       "#if os(Windows)\nlet x = 1\n#endif\n",
			wantTodo: "conditional compilation",
		},
		{
			name:     "format specifier in string interpolation",
			in:       "function f(n Count)\n\tprint(\"{n:04x}\")\nend 'f'\n",
			wantTodo: "format specifiers",
		},
		{
			name:     "reserved word collision",
			in:       "function f(when Count)\n\tlet sets = when\nend 'f'\n",
			wantTodo: "keyword in minimaxon",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Convert("test.maxon", tc.in)
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
	write("a.maxon", "#if os(Windows)\nlet a = 1\n#else\nlet a = 2\n#endif\n")
	write("b.maxon", "#if os(Windows)\nlet b = 1\n#endif\n")
	write("c.maxon", "let c = 3\n")

	var stdout, stderr bytes.Buffer
	code := runMain([]string{"--check", dir}, strings.NewReader(""), &stdout, &stderr)
	want := "     5    2 files  conditional compilation directives (#if/#else/#endif) are not supported in minimaxon\n2 of 3 files would change\n"
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
