package main

import (
	"path/filepath"
	"testing"
)

// TestConvertLibraryHttpRules covers the rules maxon's HTTP files need: what runs a TLS session is left out, with what only it
// reached; an arm on a case minimaxon's StreamError lacks goes; a merged region is the value itself; an open range pattern
// takes the bounds of the matched parameter's typealias; a hand-written member goes; and a builder passed to a function that
// only appends to it is a String there; a local named url is renamed, and a parameter, a label and a member keep the name.
func TestConvertLibraryHttpRules(t *testing.T) {
	cases := []struct{ name, file, in, want string }{
		{
			name: "a session's case, its arms and the functions that name it go",
			file: "T.maxon",
			in:   "public union T\n\tplain(c Conn)\n\tsecure(s TlsStream)\n\n\tpublic function n() returns Count\n\t\tmatch self 'm'\n\t\t\tplain(c) then return 1\n\t\t\tsecure(s) then return helper(s)\n\t\tend 'm'\n\tend 'n'\nend 'T'\n\nfunction helper(s TlsStream) returns Count\n\treturn inner()\nend 'helper'\n\nfunction inner() returns Count\n\treturn 2\nend 'inner'\n\nfunction fault(cause StreamError) returns Count\n\treturn match cause 'c'\n\t\tclosed gives 1\n\t\tsecure(e) gives 2\n\tend 'c'\nend 'fault'\n",
			want: "public union T\n\tplain(c Conn)\n\n\tpublic function n() returns Count\n\t\tmatch self 'm'\n\t\t\tplain(c) then return 1\n\t\tend 'm'\n\tend 'n'\nend 'T'\n\nfunction fault(cause StreamError) returns Count\n\treturn match cause 'c'\n\t\tclosed gives 1\n\tend 'c'\nend 'fault'\n",
		},
		{
			name: "a merge is its value, an open range its typealias's bound",
			file: "R.maxon",
			in:   "public typealias Number = int(100 to 999)\n\nfunction of(number Number, b ByteArray, part ByteArray) returns Count\n\tb.append(merge part)\n\treturn match number 'n'\n\t\tmin upto 200 gives 1\n\t\t200 to max gives 2\n\tend 'n'\nend 'of'\n",
			want: "public typealias Number = int(100 to 999)\n\nfunction of(number Number, b ByteArray, part ByteArray) returns Count\n\tb.append(part)\n\treturn match number 'n'\n\t\t100 upto 200 gives 1\n\t\t200 to 999 gives 2\n\tend 'n'\nend 'of'\n",
		},
		{
			name: "a hand-written member goes, and a builder passed to an appending function is a String",
			file: "HttpClient.maxon",
			in:   "public type HttpClient\n\tpublic static function send() returns Count\n\t\treturn 1\n\tend 'send'\nend 'HttpClient'\n\nfunction line(raw StringBuilder, name String)\n\traw.append(name)\nend 'line'\n\nfunction built(name String) returns String\n\tlet raw = StringBuilder.create()\n\tline(raw, name: name)\n\treturn raw.build()\nend 'built'\n",
			want: "public type HttpClient\nend 'HttpClient'\n\nfunction line(raw String, name String)\n\traw = \"{raw}{name}\"\nend 'line'\n\nfunction built(name String) returns String\n\tlet raw = \"\"\n\tline(raw, name: name)\n\treturn raw\nend 'built'\n",
		},
		{
			name: "a local named as a module is renamed in its function",
			file: "L.maxon",
			in:   "function f(request Request) returns String\n\tlet url = request.url()\n\treturn \"{url.path}\" + make(url: url)\nend 'f'\n",
			want: "function f(request Request) returns String\n\tlet address = request.url()\n\treturn \"{address.path}\" + make(url: address)\nend 'f'\n",
		},
	}
	root := filepath.Join(t.TempDir(), "stdlib")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Convert(filepath.Join(root, tc.file), tc.in, sourceFile)
			if len(res.Warnings) > 0 {
				t.Fatalf("warnings %v:\n%s", res.Warnings, res.Text)
			}
			if res.Text != tc.want {
				t.Fatalf("mismatch:\ngot:\n%q\nwant:\n%q", res.Text, tc.want)
			}
		})
	}
}
