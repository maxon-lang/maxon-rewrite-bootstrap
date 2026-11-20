package ast

import (
	"testing"

	"minimaxon/internal/diag"
)

func TestPositionsLocateAnOffsetInItsFile(t *testing.T) {
	// a.maxon is "one\ntwo\n" and b.maxon is "three\n", so b starts at offset 8, where a's empty last line would too.
	positions := NewPositions("one\ntwo\nthree\n", []FileSpan{{File: "a.maxon", Begin: 0, End: 8}, {File: "b.maxon", Begin: 8, End: 14}})
	for _, c := range []struct {
		offset     uint32
		file       string
		line, col  int
		wantInFile int
	}{
		{0, "a.maxon", 1, 1, 0},
		{2, "a.maxon", 1, 3, 0},
		{4, "a.maxon", 2, 1, 0},
		{7, "a.maxon", 2, 4, 0},
		{8, "b.maxon", 1, 1, 1},
		{13, "b.maxon", 1, 6, 1},
	} {
		file, line, col := positions.Where(c.offset)
		if file != c.file || line != c.line || col != c.col {
			t.Errorf("offset %d is at %s:%d:%d, want %s:%d:%d", c.offset, file, line, col, c.file, c.line, c.col)
		}
		if got := positions.FileOf(int(c.offset)); got != c.wantInFile {
			t.Errorf("offset %d is in file %d, want %d", c.offset, got, c.wantInFile)
		}
	}
}

func TestPositionsPlaceTheEndOfAFileOnItsLastLine(t *testing.T) {
	// The offset one past a file's last newline is the empty line after it, where "unexpected end of file" is reported.
	positions := NewPositions("one\ntwo\nthree\n", []FileSpan{{File: "a.maxon", Begin: 0, End: 8}, {File: "b.maxon", Begin: 8, End: 14}})
	file, line, col := positions.Locate(0, 8)
	if file != "a.maxon" || line != 3 || col != 1 {
		t.Errorf("the end of a.maxon is at %s:%d:%d", file, line, col)
	}
}

func TestSyntheticPositionsAreAtLineZero(t *testing.T) {
	if file, line, col := stdlibPositions.Where(0); file != "<stdlib>" || line != 0 || col != 0 {
		t.Errorf("the standard library is at %s:%d:%d", file, line, col)
	}
}

// A diagnostic raised at a node names its module by handle and gets its file, line and column only when the project
// locates it, so raising one costs no line index.
func TestAnErrorAtAPositionIsLocatedByTheProject(t *testing.T) {
	root := NewModule("", ".", false)
	text := "one\ntwo\n"
	root.Positions = NewPositions(text, []FileSpan{{File: "a.maxon", End: len(text)}})
	proj := NewProject(root)
	proj.Modules = []*Module{root}
	err := Pos{Mod: root.ID, Off: 5}.Error("E0000", "message")
	if err.Located() {
		t.Fatalf("an error is located before the project has been asked: %v", err)
	}
	stdlib := Pos{Mod: StdlibID}.Error("E0001", "message")
	proj.Locate([]*diag.CompileError{err, stdlib})
	if err.File != "a.maxon" || err.Line != 2 || err.Col != 2 {
		t.Errorf("error at %s:%d:%d, want a.maxon:2:2", err.File, err.Line, err.Col)
	}
	if stdlib.File != "<stdlib>" || stdlib.Line != 0 {
		t.Errorf("standard library error %v", stdlib)
	}
}
