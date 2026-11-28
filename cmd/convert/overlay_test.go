package main

import "testing"

func TestOverlayReplacesAddsAndDrops(t *testing.T) {
	text := "let a = 1\n\n// b doubles.\nfunction b(x Count) returns Count\n\treturn x * 2\nend 'b'\n\nlet c = 3\n"
	overlay := "// overlay drops: c\n\n// b triples.\nfunction b(x Count) returns Count\n\treturn x * 3\nend 'b'\n\nlet d = 4\n"
	got, err := overlaid("f.maxon", text, overlay)
	if err != nil {
		t.Fatal(err)
	}
	want := "let a = 1\n\n// b triples.\nfunction b(x Count) returns Count\n\treturn x * 3\nend 'b'\n\nlet d = 4\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if _, err := overlaid("f.maxon", text, "// overlay drops: e\n"); err == nil {
		t.Error("a drop of a name the file does not declare converted")
	}
	if _, err := overlaid("f.maxon", text, "// a comment alone\n"); err == nil {
		t.Error("a comment that leads no declaration converted")
	}
}
