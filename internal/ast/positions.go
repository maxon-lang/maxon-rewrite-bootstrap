package ast

import (
	"sort"
	"strings"
	"sync"
)

// FileSpan is where one file's text is in a module's text.
type FileSpan struct {
	File  string
	Begin int
	End   int
}

// Positions locates the offsets in the text of one module: the files it holds and, once something asks for a line,
// where each line starts. A node holds its module's Positions and an offset in it, not a file, a line and a column, so
// placing a node costs a pointer and a 32-bit offset and the line index is never built for a program that compiles.
type Positions struct {
	text  string
	files []FileSpan
	// synthetic positions belong to nothing a program wrote, such as the standard library: they are at no line.
	synthetic bool

	indexed sync.Once
	// lineStarts holds where every line of every file starts, a file's after the one before; firstLines indexes the
	// first line of each file in it.
	lineStarts []uint32
	firstLines []int
}

func NewPositions(text string, files []FileSpan) *Positions {
	return &Positions{text: text, files: files}
}

// SyntheticPositions are those of something that is in no file's text, which a diagnostic names by file and puts at line 0.
func SyntheticPositions(file string) *Positions {
	return &Positions{files: []FileSpan{{File: file}}, synthetic: true}
}

// index finds where each line starts. It runs once, on the first call that needs a line, which any worker may make
// when a diagnostic names a declaration of another module.
func (p *Positions) index() {
	p.indexed.Do(func() {
		p.lineStarts = make([]uint32, 0, len(p.files)+strings.Count(p.text, "\n"))
		p.firstLines = make([]int, len(p.files))
		for i, f := range p.files {
			p.firstLines[i] = len(p.lineStarts)
			at := f.Begin
			for {
				p.lineStarts = append(p.lineStarts, uint32(at))
				next := strings.IndexByte(p.text[at:f.End], '\n')
				if next < 0 {
					break
				}
				at += next + 1
			}
		}
	})
}

// FileOf is the index of the file that holds offset: the last file that begins at or before it.
func (p *Positions) FileOf(offset int) int {
	return max(0, sort.Search(len(p.files), func(i int) bool { return p.files[i].Begin > offset })-1)
}

// Locate is the file, line and column of an offset in file, a file's index; the first line and column are 1. A column
// counts bytes.
func (p *Positions) Locate(file, offset int) (name string, line, col int) {
	name = p.files[file].File
	if p.synthetic {
		return name, 0, 0
	}
	p.index()
	first := p.firstLines[file]
	last := len(p.lineStarts)
	if file+1 < len(p.files) {
		last = p.firstLines[file+1]
	}
	// The last line of the file that starts at or before offset.
	i := first + sort.Search(last-first, func(i int) bool { return int(p.lineStarts[first+i]) > offset }) - 1
	i = max(i, first)
	return name, i - first + 1, offset - int(p.lineStarts[i]) + 1
}

// Where is the file, line and column of an offset.
func (p *Positions) Where(offset uint32) (file string, line, col int) {
	return p.Locate(p.FileOf(int(offset)), int(offset))
}

// stdlibPositions is the position of everything the standard library declares. It is never written after it is made.
var stdlibPositions = SyntheticPositions("<stdlib>")
