package syntax

import (
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minimaxon/internal/diag"
)

const (
	corpusRoot           = "../../tests"
	messSeed             = 7
	expectErrorDirective = "// expect-error:"
)

// The inputs below are written wrongly on purpose, so they cannot live in .maxon files: the lexer refuses them, and
// minimaxon rewrite reads those files. They name no keyword that a language change is likely to rename; the property
// tests further down build their messy input from the corpus instead.
var formatCases = []struct {
	name string
	in   string
	want string
}{
	{"spaces and tabs between tokens",
		"let   x =\t1 +   2\n",
		"let x = 1 + 2\n"},
	{"missing spaces around operators and after commas",
		"let x=a+b*c\nlet y=f(a,b)\n",
		"let x = a + b * c\n\nlet y = f(a, b)\n"},
	{"spaces where tokens join",
		"let y = f (a , b )\nlet z = a . b\nlet w = - 1\n",
		"let y = f(a, b)\n\nlet z = a.b\n\nlet w = -1\n"},
	{"trailing whitespace",
		"let x = 1  \t\n",
		"let x = 1\n"},
	{"carriage returns",
		"let x = 1\r\n\r\nlet y = 2\r\n",
		"let x = 1\n\nlet y = 2\n"},
	{"no final newline",
		"let x = 1",
		"let x = 1\n"},
	{"byte order mark",
		"\xef\xbb\xbflet x = 1\n",
		"let x = 1\n"},
	{"empty and blank files",
		"",
		""},
	{"only blank lines",
		"\n \n\t\n",
		""},
	{"missing blank line between declarations",
		"let a = 1\nlet b = 2\nlet c = 3\n",
		"let a = 1\n\nlet b = 2\n\nlet c = 3\n"},
	{"extra blank lines between declarations",
		"let a = 1\n\n\n\nlet b = 2\n",
		"let a = 1\n\nlet b = 2\n"},
	{"blank lines at the start and the end of the file",
		"\n\nlet a = 1\n\n\n",
		"let a = 1\n"},
	{"blank line with tabs and spaces",
		"let a = 1\n\t \t\nlet b = 2\n",
		"let a = 1\n\nlet b = 2\n"},
	{"blank lines inside a function",
		"function f()\n\n\tlet a = 1\n\n\tlet b = 2\nend 'f'\n",
		"function f()\n\tlet a = 1\n\tlet b = 2\nend 'f'\n"},
	{"blank line between fields and before the first method",
		"type T\n\n\tvar a as T\n\n\tvar b as T\n\n\tfunction f()\n\t\tlet x = 1\n\tend 'f'\nend 'T'\n",
		"type T\n\tvar a as T\n\tvar b as T\n\n\tfunction f()\n\t\tlet x = 1\n\tend 'f'\nend 'T'\n"},
	{"blank lines before methods",
		"type T\n\tfunction f()\n\t\tlet x = 1\n\tend 'f'\n\tfunction g()\n\t\tlet x = 1\n\tend 'g'\n\n\n\tstatic function h()\n\t\tlet x = 1\n\tend 'h'\nend 'T'\n",
		"type T\n\tfunction f()\n\t\tlet x = 1\n\tend 'f'\n\n\tfunction g()\n\t\tlet x = 1\n\tend 'g'\n\n\tstatic function h()\n\t\tlet x = 1\n\tend 'h'\nend 'T'\n"},
	{"an interface has no blank lines",
		"interface I\n\tfunction f()\n\n\tfunction g()\nend 'I'\n",
		"interface I\n\tfunction f()\n\tfunction g()\nend 'I'\n"},
	{"comment lines are indented like the line after them",
		"function f()\n// first\n\t\t// second\n\tlet a = 1\n  // third\n\tlet b = 2\n\t\t// at the end\nend 'f'\n  // before the end label\n",
		"function f()\n\t// first\n\t// second\n\tlet a = 1\n\t// third\n\tlet b = 2\n// at the end\nend 'f'\n\n// before the end label\n"},
	{"comment after code is one space away",
		"let a = 1// no space\nlet b = 2    // many spaces\nlet c = 3\t// a tab\n",
		"let a = 1 // no space\n\nlet b = 2 // many spaces\n\nlet c = 3 // a tab\n"},
	{"comment text keeps its own spacing and loses trailing whitespace",
		"//   spaced   out  \nlet a = 1  \n",
		"//   spaced   out\nlet a = 1\n"},
	{"comment paragraphs keep their blank line, and doubled blank lines collapse",
		"// one\n\n\n// two\nlet a = 1\n\n\n// three\n\n// four\nlet b = 2\n",
		"// one\n\n// two\nlet a = 1\n\n// three\n\n// four\nlet b = 2\n"},
	{"a comment after a declaration gets its blank line",
		"let a = 1\n// then\nlet b = 2\n",
		"let a = 1\n\n// then\nlet b = 2\n"},
	{"a comment before a method goes after its blank line",
		"type T\n\tvar a as T\n\t// about f\n\tfunction f()\n\t\tlet x = 1\n\tend 'f'\nend 'T'\n",
		"type T\n\tvar a as T\n\n\t// about f\n\tfunction f()\n\t\tlet x = 1\n\tend 'f'\nend 'T'\n"},
	{"comments at the end of the file",
		"let a = 1\n// last\n\n\n// really last\n\n",
		"let a = 1\n\n// last\n\n// really last\n"},
	{"a file of comments only",
		"\n\n  // one\n\n\n// two  \n\n",
		"// one\n\n// two\n"},
	{"doc comments and directives are comments",
		"/// docs\nfunction main() returns ExitCode\n\t//> out\n\t// expect-exit:   1\n\treturn 0\nend 'main'\n",
		"/// docs\nfunction main() returns ExitCode\n\t//> out\n\t// expect-exit:   1\n\treturn 0\nend 'main'\n"},
	{"a string keeps its spaces and its slashes",
		"let s =   \"a   b // c\"  // end\n",
		"let s = \"a   b // c\" // end\n"},
	{"interpolations are formatted too",
		"let s = \"{ a+b } and {f( x,y )}\"\n",
		"let s = \"{a + b} and {f(x, y)}\"\n"},
	{"nested interpolations",
		"let s = \"{f(\"{ x+1 }\")}\"\n",
		"let s = \"{f(\"{x + 1}\")}\"\n"},
}

func TestFormatPutsEachProgramInItsOneLayout(t *testing.T) {
	for _, c := range formatCases {
		t.Run(c.name, func(t *testing.T) {
			got, errs := Format("a.maxon", c.in)
			if len(errs) > 0 {
				t.Fatalf("errors: %v", errs)
			}
			if got != c.want {
				t.Errorf("got  %q\nwant %q", got, c.want)
			}
			again, errs := Format("a.maxon", got)
			if len(errs) > 0 || again != got {
				t.Errorf("formatting the output again gives %q, errors %v", again, errs)
			}
		})
	}
}

var formatRefusals = []struct {
	name string
	in   string
	msg  string
}{
	{"indented with spaces", "function f()\n  let a = 1\nend 'f'\n", "indented with a space"},
	{"indented too deep", "function f()\n\t\tlet a = 1\nend 'f'\n", "more than one tab deeper"},
	{"a line break inside parentheses", "let a = f(1,\n2)\n", "breaks inside parentheses"},
	{"a block comment", "/* no */\nlet a = 1\n", "block comment"},
	{"a syntax error", "let = 1\n", "Expected a name but got"},
	{"an unterminated string", "let a = \"x\n", "unterminated string"},
}

func TestFormatReportsWhatItCannotLayOutAndChangesNothing(t *testing.T) {
	for _, c := range formatRefusals {
		t.Run(c.name, func(t *testing.T) {
			got, errs := Format("a.maxon", c.in)
			if len(errs) == 0 || !strings.Contains(errs[0].Message, c.msg) {
				t.Fatalf("errors: %v, want one holding %q", errs, c.msg)
			}
			if got != c.in {
				t.Errorf("the text changed to %q", got)
			}
		})
	}
}

func TestFormatMarkdownFormatsBlocksThatAreProgramsOnly(t *testing.T) {
	const doc = "Prose  with `let  x=1` kept.\n\n" +
		"```maxon\nlet a=1\nlet b = 2\n```\n\n" +
		"```\nlet   c = 3\n```\n\n" +
		"```powershell\nlet   d = 4\n```\n\n" +
		"```maxon\nlet = broken\n```\n\n" +
		"```maxon\nlet e = 5\n```\n"
	const want = "Prose  with `let  x=1` kept.\n\n" +
		"```maxon\nlet a = 1\n\nlet b = 2\n```\n\n" +
		"```\nlet c = 3\n```\n\n" +
		"```powershell\nlet   d = 4\n```\n\n" +
		"```maxon\nlet = broken\n```\n\n" +
		"```maxon\nlet e = 5\n```\n"
	if got := FormatMarkdown("a.md", doc); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestFormatMarkdownKeepsCarriageReturns(t *testing.T) {
	const doc = "text\r\n```maxon\r\nlet a=1\r\n```\r\n"
	const want = "text\r\n```maxon\r\nlet a = 1\r\n```\r\n"
	if got := FormatMarkdown("a.md", doc); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// corpusPrograms is every program of tests/run and tests/modules, which compile and so are in the one layout.
func corpusPrograms(t *testing.T) map[string]string {
	t.Helper()
	programs := map[string]string{}
	for _, dir := range []string{"run", "modules"} {
		err := filepath.WalkDir(filepath.Join(corpusRoot, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, sourceExtension) {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			// A program that expects an error may be one the parser refuses.
			if !strings.Contains(string(data), expectErrorDirective) {
				programs[path] = string(data)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(programs) == 0 {
		t.Fatalf("no program under %s", corpusRoot)
	}
	return programs
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func TestFormatLeavesTheCorpusAlone(t *testing.T) {
	programs := corpusPrograms(t)
	for _, path := range sortedKeys(programs) {
		got, errs := Format(path, programs[path])
		if len(errs) > 0 {
			t.Errorf("%s does not format: %v", path, errs[0])
		} else if got != programs[path] {
			t.Errorf("%s is not in the one layout", path)
		}
	}
}

// TestFormatRestoresTheCorpusFromMessedUpCopies writes each program of the corpus carelessly, with every gap and every
// blank line that the layout leaves no choice about changed, and expects the formatter to give the program back
// byte for byte.
func TestFormatRestoresTheCorpusFromMessedUpCopies(t *testing.T) {
	programs := corpusPrograms(t)
	for i, path := range sortedKeys(programs) {
		original := programs[path]
		rng := rand.New(rand.NewPCG(messSeed, uint64(i)))
		for variant := range 3 {
			messy := messUp(t, path, original, rng)
			switch variant {
			case 1:
				messy = strings.ReplaceAll(messy, "\n", "\r\n")
			case 2:
				messy = strings.TrimSuffix(messy, "\n")
			}
			got, errs := Format(path, messy)
			if len(errs) > 0 {
				t.Fatalf("%s, variant %d: %v\n%q", path, variant, errs[0], messy)
			}
			if got != original {
				t.Fatalf("%s, variant %d: the formatter does not restore the program\nmessy: %q\ngot:   %q", path, variant, messy, got)
			}
		}
	}
}

// messUp rewrites a program in the one layout with a wrong layout that has the same tokens and comments: a random
// gap inside each line, trailing whitespace, comments indented and spaced wrongly, and blank lines where the rules
// allow none, or doubled where they require one.
func messUp(t *testing.T, path, text string, rng *rand.Rand) string {
	t.Helper()
	src := NewSource(text, []FileSlice{{File: path, Begin: 0, End: len(text)}})
	var toks []Token
	var errs []*diag.CompileError
	diag.Guard(&errs, func() { toks = Lex(src, nil) })
	if len(errs) > 0 {
		t.Fatalf("%s: %v", path, errs[0])
	}
	var out strings.Builder
	prevEnd, prevKind := 0, TFileEnd
	for _, tok := range toks {
		kind := tok.Kind()
		start := lexemeStart(tok)
		gap := text[prevEnd:start]
		switch {
		case prevKind == TNewline || prevKind == TFileEnd:
			gap = messUpLines(gap, rng)
		case startsLine(prevKind):
		case kind == TNewline:
			gap = messUpTrailingComment(gap, rng)
		default:
			gap = messUpGap(gap, text[:prevEnd], text[start:], rng)
		}
		out.WriteString(gap)
		out.WriteString(text[start:lexemeEnd(tok)])
		prevEnd, prevKind = lexemeEnd(tok), kind
	}
	return out.String()
}

// messUpGap changes the gap between two tokens on a line: it widens a space, or takes it away where the tokens
// would still be two, and puts a space where there is none.
func messUpGap(gap, before, after string, rng *rand.Rand) string {
	const spaces = " \t"
	switch {
	case gap == " " && canJoin(before, after):
		return []string{"", "  ", "\t", " \t ", " "}[rng.IntN(5)]
	case gap == " ":
		return []string{"  ", "\t", " \t "}[rng.IntN(3)]
	case gap == "" && canSpace(before, after):
		return spaces[:rng.IntN(len(spaces)+1)]
	}
	return gap
}

const punctuation = "(),:.[]{}"

// canJoin is whether the tokens that end before and start after can touch and stay two tokens: one of them is
// punctuation, or an operator beside a word.
func canJoin(before, after string) bool {
	last, first := before[len(before)-1], after[0]
	return strings.IndexByte(punctuation, last) >= 0 || strings.IndexByte(punctuation, first) >= 0 ||
		isWordByte(last) && strings.IndexByte("+-*/", first) >= 0 ||
		strings.IndexByte("+-*/", last) >= 0 && isWordByte(first)
}

// canSpace is whether a space may go between the tokens: any two tokens are still two with a space between.
func canSpace(before, after string) bool {
	return before != "" && after != ""
}

func isWordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '"' || c == '\''
}

// messUpTrailingComment changes the gap before a newline: trailing whitespace, and the space before a comment.
func messUpTrailingComment(gap string, rng *rand.Rand) string {
	comment, ok := strings.CutPrefix(gap, " ")
	if !ok {
		if gap != "" {
			panic(diag.Internal("the gap before a newline is %q", gap))
		}
		return []string{"", "", " ", "\t", "  \t"}[rng.IntN(5)]
	}
	return []string{"", " ", "   ", "\t"}[rng.IntN(4)] + comment + []string{"", "", " ", "\t "}[rng.IntN(4)]
}

// messUpLines changes the blank lines and comment lines before a line of code, and keeps its own indentation, the
// last part of the gap. A comment line gets any indentation and trailing whitespace. A blank line becomes one or two:
// the one that the rules require right after code may also go, and where a gap has no comment, any number of blank
// lines may stand for the one or none it had.
func messUpLines(gap string, rng *rand.Rand) string {
	last := strings.LastIndexByte(gap, '\n') + 1
	lines, indent := strings.SplitAfter(gap[:last], "\n"), gap[last:]
	lines = lines[:len(lines)-1]
	hasComment := false
	for _, line := range lines {
		hasComment = hasComment || line != "\n"
	}
	blanks := func(n int) string {
		var b strings.Builder
		for range n {
			b.WriteString([]string{"", " ", "\t\t"}[rng.IntN(3)])
			b.WriteString("\n")
		}
		return b.String()
	}
	var out strings.Builder
	if !hasComment {
		out.WriteString(blanks(rng.IntN(4)))
	}
	for i, line := range lines {
		switch {
		case hasComment && line == "\n" && i == 0:
			out.WriteString(blanks(rng.IntN(3)))
		case hasComment && line == "\n":
			out.WriteString(blanks(1 + rng.IntN(2)))
		case line != "\n":
			body := strings.TrimLeft(line[:len(line)-1], "\t")
			out.WriteString([]string{"", "\t\t\t", "  ", " \t"}[rng.IntN(4)])
			out.WriteString(body)
			out.WriteString([]string{"", " ", "\t"}[rng.IntN(3)])
			out.WriteString("\n")
		}
	}
	out.WriteString(indent)
	return out.String()
}
