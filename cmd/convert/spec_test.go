package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const specFile = "---\r\nfeature: demo\r\n---\r\n\r\n## Documentation\r\n\r\n```maxon\r\nlet docs = 1.5\r\n```\r\n\r\n" +
	"## Tests\r\n\r\n" +
	"<!-- test: clean -->\r\nWhat the test shows.\r\n```maxon\r\nfunction main() returns ExitCode\r\n\t\tlet x = 3\r\n\r\n\t\treturn x\r\nend 'main'\r\n```\r\n```exitcode\r\n3\r\n```\r\n\r\n" +
	"<!-- test: unsupported -->\r\n<!-- Args: 1 -->\r\n```maxon\r\nfunction main() returns ExitCode\r\n\tlet x = int.fromString(\"1\")\r\n\treturn 0\r\nend 'main'\r\n```\r\n```exitcode\r\n0\r\n```\r\n\r\n" +
	"<!-- test: two-files -->\r\n```maxon\r\n// --- file: lib/a.maxon\r\nexport function a() returns ExitCode\r\n\t\treturn 1\r\nend 'a'\r\n\r\n// --- file: main.maxon\r\nfunction main() returns ExitCode\r\n\treturn a()\r\nend 'main'\r\n```\r\n```exitcode\r\n1\r\n```\r\n\r\n" +
	"<!-- test: syntax-error -->\r\n```maxon\r\nfunction main() returns ExitCode\r\n\treturn (\r\nend 'main'\r\n```\r\n```maxoncstderr\r\nerror E2001: <fragment>:3:1: oops\r\n```\r\n\r\n" +
	"<!-- test: quoted -->\r\n```text\r\n<!-- test: not a test -->\r\n```\r\n"

func TestConvertSpecConvertsEachTestsBlockAndMarksTheOnesThatWarned(t *testing.T) {
	res := ConvertSpec("demo.md", specFile)
	if len(res.Tests) != 5 {
		t.Fatalf("tests = %+v", res.Tests)
	}
	wantSkips := map[string]int{"unsupported": 1}
	for _, test := range res.Tests {
		if len(test.Skips) != wantSkips[test.Name] {
			t.Errorf("test %q has skips %q", test.Name, test.Skips)
		}
	}

	want := "---\nfeature: demo\n---\n\n## Documentation\n\n```maxon\nlet docs = 1.5\n```\n\n" +
		"## Tests\n\n" +
		"<!-- test: clean -->\nWhat the test shows.\n```maxon\nfunction main() returns ExitCode\n\tlet x = 3\n\treturn x\nend 'main'\n```\n```exitcode\n3\n```\n\n" +
		"<!-- test: unsupported -->\n<!-- skip: 'int.fromString' is not supported in minimaxon -->\n<!-- Args: 1 -->\n```maxon\nfunction main() returns ExitCode\n\t// TODO: 'int.fromString' is not supported in minimaxon\n\tlet x = int.fromString(\"1\")\n\treturn 0\nend 'main'\n```\n```exitcode\n0\n```\n\n" +
		"<!-- test: two-files -->\n```maxon\n// --- file: lib/a.maxon\nexport function a() returns ExitCode\n\treturn 1\nend 'a'\n\n// --- file: main.maxon\nfunction main() returns ExitCode\n\treturn a()\nend 'main'\n```\n```exitcode\n1\n```\n\n" +
		"<!-- test: syntax-error -->\n```maxon\nfunction main() returns ExitCode\n\treturn (\nend 'main'\n```\n```maxoncstderr\nerror E2001: <fragment>:3:1: oops\n```\n\n" +
		"<!-- test: quoted -->\n```text\n<!-- test: not a test -->\n```\n"
	if res.Text != want {
		t.Fatalf("got:\n%q\nwant:\n%q", res.Text, want)
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Line != 30 {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
}

func TestConvertSpecIsDeterministicAndReadsItsOwnOutput(t *testing.T) {
	first := ConvertSpec("demo.md", specFile)
	second := ConvertSpec("demo.md", specFile)
	if first.Text != second.Text {
		t.Fatal("converting twice differs")
	}
	again := ConvertSpec("demo.md", first.Text)
	if strings.Count(again.Text, "<!-- skip:") != 1 {
		t.Fatalf("converted text read again holds %d skip lines", strings.Count(again.Text, "<!-- skip:"))
	}
}

func TestConvertSpecBlocksThatNeedNoSkipAreNotMarked(t *testing.T) {
	res := ConvertSpec("demo.md", "## Tests\n\n<!-- test: unparsed -->\n```maxon\nfunction main() returns ExitCode\n\treturn 1 +\nend 'main'\n```\n```exitcode\n1\n```\n")
	if len(res.Tests) != 1 || len(res.Tests[0].Skips) != 1 || !strings.HasPrefix(res.Tests[0].Skips[0], rejectedKindPrefix) {
		t.Fatalf("a block minimaxon does not parse and no syntax error is expected should be skipped with the parser's message: %+v", res.Tests)
	}
}

func TestConvertSpecOfAFileWithoutTestsIsNotASpec(t *testing.T) {
	if res := ConvertSpec("AGENTS.md", "Work here is compiler work.\n"); len(res.Tests) != 0 {
		t.Fatalf("tests = %+v", res.Tests)
	}
}

func TestSpecsCheckCountsTestsPerKind(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "demo.md"), []byte(specFile), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runMain([]string{"--specs", "--check", dir}, strings.NewReader(""), &stdout, &stderr)
	want := "     1    1 files  'int.fromString' is not supported in minimaxon\n5 tests: 4 converted cleanly, 1 skipped\n1 of 1 files would change\n"
	if code != 1 || stdout.String() != want {
		t.Fatalf("code=%d stdout=%q want %q", code, stdout.String(), want)
	}
}

func TestSpecsOutWritesLFFilesAndTheSummary(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in")
	out := filepath.Join(dir, "out")
	if err := os.MkdirAll(in, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(in, "demo.md"), []byte(specFile), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := runMain([]string{"--specs", "--out=" + out, in}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	got, err := os.ReadFile(filepath.Join(out, "demo.md"))
	if err != nil || bytes.Contains(got, []byte("\r")) || !bytes.Contains(got, []byte("<!-- skip: 'int.fromString'")) {
		t.Fatalf("err=%v got=%q", err, got)
	}
	if !strings.HasSuffix(stdout.String(), "5 tests: 4 converted cleanly, 1 skipped\n") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestConvertSpecMarksAnErrorTestWhoseBlockWasLaidOutDifferently(t *testing.T) {
	const kept = "<!-- test: kept -->\n```maxon\nfunction main() returns ExitCode\n\treturn 1\nend 'main'\n```\n```maxoncstderr\nerror E3005: <fragment>:3:9: oops\n```\n\n"
	const moved = "<!-- test: moved -->\n```maxon\nfunction main() returns ExitCode\n\t\tlet x = 1\n\n\t\treturn x\nend 'main'\n```\n```maxoncstderr\nerror E3005: <fragment>:5:10: oops\n```\n\n"
	const runs = "<!-- test: runs -->\n```maxon\nfunction main() returns ExitCode\n\t\tlet x = 1\n\n\t\treturn x\nend 'main'\n```\n```exitcode\n1\n```\n"
	res := ConvertSpec("demo.md", "## Tests\n\n"+kept+moved+runs)
	if got := strings.Count(res.Text, specPositionsMarker); got != 1 || !strings.Contains(res.Text, "<!-- test: moved -->\n"+specPositionsMarker+"\n```maxon") {
		t.Fatalf("only the error test whose layout changed is marked, got %d marks:\n%s", got, res.Text)
	}
	again := ConvertSpec("demo.md", res.Text)
	if again.Text != res.Text {
		t.Fatalf("converting the converted text changes it:\n%s", again.Text)
	}
}
