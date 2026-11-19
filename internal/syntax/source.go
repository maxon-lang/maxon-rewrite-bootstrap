package syntax

import (
	"bytes"
	"math"
	"minimaxon/internal/diag"
	"os"
	"strings"
)

const fileChangedMessage = "the file changed while it was being read"

var utf8Bom = []byte{0xEF, 0xBB, 0xBF}

// FileSlice is where one file's text is in a module's text.
type FileSlice struct {
	File  string
	Begin int
	End   int
	// firstLine indexes the file's first line in Source.lineStarts.
	firstLine int
}

// Source is a module's text, without comments, and where each of its files and lines starts. Tokens hold offsets
// into Text; a line and column is found from an offset only when a node or a diagnostic needs one.
type Source struct {
	Text       string
	Files      []FileSlice
	lineStarts []uint32
}

// NewSource indexes the lines of text, whose files are files. Offsets are 32-bit, so text is at most 4 GB.
func NewSource(text string, files []FileSlice) *Source {
	if int64(len(text)) > math.MaxUint32 {
		panic(diag.NewError(diag.ErrUnexpectedToken, "the module's source is larger than 4 GB; split the module", files[0].File, 1, 1))
	}
	s := &Source{Text: text, Files: files, lineStarts: make([]uint32, 0, len(files)+strings.Count(text, "\n"))}
	for i := range s.Files {
		f := &s.Files[i]
		f.firstLine = len(s.lineStarts)
		at := f.Begin
		for {
			s.lineStarts = append(s.lineStarts, uint32(at))
			next := strings.IndexByte(text[at:f.End], '\n')
			if next < 0 {
				break
			}
			at += next + 1
		}
	}
	return s
}

// lineCol is the line and column of offset in file. line is the index in lineStarts of a line near offset, which
// lineCol moves to offset's line: callers ask about nearby offsets in turn, so the walk is short.
func (s *Source) lineCol(file, offset int, line *int) (int, int) {
	first := s.Files[file].firstLine
	last := len(s.lineStarts)
	if file+1 < len(s.Files) {
		last = s.Files[file+1].firstLine
	}
	i := min(max(*line, first), last-1)
	for i+1 < last && int(s.lineStarts[i+1]) <= offset {
		i++
	}
	for i > first && int(s.lineStarts[i]) > offset {
		i--
	}
	*line = i
	return i - first + 1, offset - int(s.lineStarts[i]) + 1
}

func (s *Source) errorAt(code, msg string, file, offset int) *diag.CompileError {
	line := s.Files[file].firstLine
	l, c := s.lineCol(file, offset, &line)
	return diag.NewError(code, msg, s.Files[file].File, l, c)
}

// LoadModuleSource reads a module's files into one string sized once at their total. Every token's text is a
// substring of it, so lexing allocates no text of its own. A string cannot be read into, so each file is read into
// a scratch buffer and copied from there.
func LoadModuleSource(files []string) *Source {
	var total, largest int64
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			panic(err)
		}
		total += info.Size()
		largest = max(largest, info.Size())
	}

	scratch := make([]byte, largest)
	var src strings.Builder
	src.Grow(int(total))
	slices := make([]FileSlice, 0, len(files))
	for _, file := range files {
		begin := src.Len()
		src.Write(readSourceFile(file, scratch))
		slices = append(slices, FileSlice{File: file, Begin: begin, End: src.Len()})
	}
	return NewSource(src.String(), slices)
}

// readSourceFile reads file into buffer and returns its text, without a byte order mark.
func readSourceFile(file string, buffer []byte) []byte {
	handle, err := os.Open(file)
	if err != nil {
		panic(err)
	}
	defer handle.Close()

	info, err := handle.Stat()
	if err != nil {
		panic(err)
	}
	length := info.Size()
	if length > int64(len(buffer)) {
		panic(diag.NewError(diag.ErrUnexpectedToken, fileChangedMessage, file, 1, 1))
	}

	size := int(length)
	read := 0
	for read < size {
		n, err := handle.ReadAt(buffer[read:size], int64(read))
		read += n
		if n == 0 && err != nil {
			panic(diag.NewError(diag.ErrUnexpectedToken, fileChangedMessage, file, 1, 1))
		}
	}
	return bytes.TrimPrefix(buffer[:size], utf8Bom)
}
