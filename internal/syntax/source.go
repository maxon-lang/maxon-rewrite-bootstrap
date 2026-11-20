package syntax

import (
	"bytes"
	"math"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"os"
	"strings"
)

const fileChangedMessage = "the file changed while it was being read"

var utf8Bom = []byte{0xEF, 0xBB, 0xBF}

// FileSlice is where one file's text is in a module's text.
type FileSlice = ast.FileSpan

// Source is a module's text, without comments, and where each of its files starts. Tokens hold offsets into Text; a
// line and column is found from an offset only when a diagnostic needs one, by Positions.
type Source struct {
	Text      string
	Files     []FileSlice
	Positions *ast.Positions
}

// NewSource wraps text, whose files are files. Offsets are 32-bit, so text is at most 4 GB.
func NewSource(text string, files []FileSlice) *Source {
	if int64(len(text)) > math.MaxUint32 {
		panic(diag.NewError(diag.ErrUnexpectedToken, "the module's source is larger than 4 GB; split the module", files[0].File, 1, 1))
	}
	return &Source{Text: text, Files: files, Positions: ast.NewPositions(text, files)}
}

// lineCol is the line and column of offset in file.
func (s *Source) lineCol(file, offset int) (int, int) {
	_, line, col := s.Positions.Locate(file, offset)
	return line, col
}

func (s *Source) errorAt(code, msg string, file, offset int) *diag.CompileError {
	name, line, col := s.Positions.Locate(file, offset)
	return diag.NewError(code, msg, name, line, col)
}

// LoadModuleSource reads a module's files into one string sized once at their total. Every token's text is a
// substring of it, so lexing allocates no text of its own. A string cannot be read into, so each file is read into
// the scratch's buffer and copied from there.
func (s *Scratch) LoadModuleSource(files []string) *Source {
	var total, largest int64
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			panic(err)
		}
		total += info.Size()
		largest = max(largest, info.Size())
	}

	if int64(cap(s.file)) < largest {
		s.file = make([]byte, largest)
	}
	buffer := s.file[:cap(s.file)]
	var src strings.Builder
	src.Grow(int(total))
	slices := make([]FileSlice, 0, len(files))
	for _, file := range files {
		begin := src.Len()
		src.Write(readSourceFile(file, buffer))
		slices = append(slices, FileSlice{File: file, Begin: begin, End: src.Len()})
	}
	return NewSource(src.String(), slices)
}

// readSourceFile reads file into buffer and returns its text. A byte order mark is an error: source is UTF-8, and
// the mark is an invisible character before the first token.
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
	if bytes.HasPrefix(buffer[:size], utf8Bom) {
		panic(diag.NewError(diag.ErrUnexpectedToken, "the file starts with a byte order mark; save it as UTF-8 without one",
			file, 1, 1))
	}
	return buffer[:size]
}
