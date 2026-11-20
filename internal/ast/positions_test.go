package ast

import "testing"

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
		pos := Pos{Src: positions, Off: c.offset}
		file, line, col := pos.Where()
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

func TestSyntheticPositionsAndNoPositionAreAtLineZero(t *testing.T) {
	stdlib := Pos{Src: stdlibPositions}
	if file, line, col := stdlib.Where(); file != "<stdlib>" || line != 0 || col != 0 {
		t.Errorf("the standard library is at %s:%d:%d", file, line, col)
	}
	var none Pos
	if file, line, col := none.Where(); file != "" || line != 0 || col != 0 {
		t.Errorf("no position is at %s:%d:%d", file, line, col)
	}
	err := stdlib.Error("E0000", "message")
	if err.File != "<stdlib>" || err.Line != 0 {
		t.Errorf("error %v", err)
	}
}
