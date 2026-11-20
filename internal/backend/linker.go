package backend

import (
	"encoding/binary"
	"minimaxon/internal/diag"
	"minimaxon/internal/ir"
	"minimaxon/internal/par"
	"sort"
	"sync"
	"sync/atomic"
)

const (
	linkCodeAlignment = 16
	linkDataAlignment = 8
	importSlotBytes   = 8
	// The mark takes the sections it has found in chunks, each a work item, so that one function with a great many
	// callees does not hold up the others; a chunk is small enough that every job gets several.
	markChunkMax     = 256
	markChunksPerJob = 8
)

func linkAlign(v, a int) int {
	return (v + a - 1) / a * a
}

type linkTargetKind uint8

const (
	linkUndefined linkTargetKind = iota
	linkCode
	linkData
	linkImageDataStart
	linkImageDataEnd
)

// linkTarget is where a symbol is defined: a section of code or a piece of data of a unit, and its offset in the unit,
// or one of the bounds of the data section.
type linkTarget struct {
	kind   linkTargetKind
	unit   int32
	part   int32
	offset int32
}

// linkUnit is a unit as the link sees it: its sections and pieces, which of them the entry reaches, and where those go.
type linkUnit struct {
	u    *CodeUnit
	data []byte
	// sections and pieces end with one past the last, so part i runs to the start of part i+1.
	sections []Section
	pieces   []int
	liveCode []atomic.Bool
	liveData []atomic.Bool
	// codeAt and dataAt are where each live section and piece go, counted from codeBase and dataBase, where the layout
	// puts the unit; codeSize and dataSize are how much the unit's live parts take.
	codeAt, dataAt     []int
	codeBase, dataBase int
	codeSize, dataSize int
	hasCode, hasData   bool
	// firstID is the id of the unit's first definition: its code symbols, then its data symbols.
	firstID symbolID
	// targets is the id of what each symbol fixup names, and dataPieces the piece each data fixup points into. A section
	// has them once the mark has found it live, which is the only time they are used.
	targets    []symbolID
	dataPieces []int32
}

// liveSection is a section of a unit that the mark has found and not yet read.
type liveSection struct{ unit, index int32 }

// containing is the index of the part whose start is the last at or before an offset.
func containing(starts []int, offset int) int {
	return sort.SearchInts(starts, offset+1) - 1
}

func newLinkUnit(u *CodeUnit) *linkUnit {
	a := u.Asm
	sections := u.Sections
	if len(sections) == 0 {
		sections = []Section{{}}
	} else if sections[0].Code != 0 {
		panic(diag.Internal("link: a unit has code before its first section"))
	}
	sections = append(sections[:len(sections):len(sections)], Section{len(a.Code), len(a.SymbolFixups), len(a.DataFixups), len(a.ImportFixups)})
	data := u.Data.Bytes()
	pieces := u.Data.pieces
	if len(pieces) == 0 {
		pieces = []int{0}
	}
	pieces = append(pieces[:len(pieces):len(pieces)], len(data))
	return &linkUnit{
		u:          u,
		data:       data,
		sections:   sections,
		pieces:     pieces,
		liveCode:   make([]atomic.Bool, len(sections)-1),
		liveData:   make([]atomic.Bool, len(pieces)-1),
		codeAt:     make([]int, len(sections)-1),
		dataAt:     make([]int, len(pieces)-1),
		targets:    make([]symbolID, len(a.SymbolFixups)),
		dataPieces: make([]int32, len(a.DataFixups)),
	}
}

func (l *linkUnit) sectionOf(offset int) int {
	return sort.Search(len(l.sections), func(i int) bool { return l.sections[i].Code > offset }) - 1
}

func (l *linkUnit) definitions() int { return len(l.u.CodeSymbols) + len(l.u.DataSymbols) }

// Link writes the executable. It keeps only the sections of code and the pieces of data that the entry reaches, as
// Go's linker does: every function is compiled, and what nothing calls is dropped here. The layout follows the order of
// the units, and of the sections and pieces in each, so the executable does not depend on the number of jobs. Every step
// runs on all jobs: the definitions are entered into one table, the mark follows the calls a round at a time, each
// unit finds its own places and then the units are given theirs by a running sum, and each unit copies and patches
// its own code into the image.
func Link(out string, units []*CodeUnit, entry string, jobs int) {
	output := OpenPeOutput(out)
	defer output.Abandon()
	links := make([]*linkUnit, len(units))
	par.ForEachIndex(len(units), jobs, func(i int) { links[i] = newLinkUnit(units[i]) })

	// Ids count from 1, so that 0 is no definition, and end with the bounds of the data section.
	next := symbolID(1)
	for _, l := range links {
		l.firstID = next
		next += symbolID(l.definitions())
	}
	imageDataStart, imageDataEnd := next, next+1
	table := newSymbolTable(int(imageDataEnd))
	defined := make([]linkTarget, imageDataEnd+1)
	par.ForEachIndex(len(links), jobs, func(i int) { links[i].define(int32(i), table, defined) })
	defined[imageDataStart], defined[imageDataEnd] = linkTarget{kind: linkImageDataStart}, linkTarget{kind: linkImageDataEnd}
	table.define(imageDataStart, ir.RtImageDataStart.String(), false)
	table.define(imageDataEnd, ir.RtImageDataEnd.String(), false)

	start := defined[table.lookup(entry)]
	if start.kind != linkCode {
		panic(diag.Internal("no entry symbol %s", entry))
	}
	markLive(links, table, defined, start, jobs)
	codeSize, dataSize := layOut(links, jobs)

	image := NewPeImage(codeSize, dataSize, ir.RtDll, ir.RtImports, finalOffset(links, start))
	output.Start(image)
	progress := &codeProgress{done: make([]bool, len(links))}
	par.ForEachIndex(len(links), jobs, func(i int) {
		links[i].copyAndPatch(links, defined, image)
		progress.finish(i, links, codeSize, output)
	})
	output.Finish()
}

// codeProgress follows which units have put their code in the image. The units' code is in unit order, so the code
// before the first unit that is not finished is final, and the output can write it.
type codeProgress struct {
	mu    sync.Mutex
	done  []bool
	first int
}

func (c *codeProgress) finish(unit int, links []*linkUnit, codeSize int, output *PeOutput) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.done[unit] = true
	for c.first < len(c.done) && c.done[c.first] {
		c.first++
	}
	if c.first == len(c.done) {
		output.CodeFinal(codeSize)
	} else {
		output.CodeFinal(links[c.first].codeBase)
	}
}

// define enters what the unit defines into the table, and where each definition is. An instance that another unit
// defines too is entered as well: the table finds only the first unit's, so the others' code is never reached and the
// mark drops it.
func (l *linkUnit) define(unit int32, table *symbolTable, defined []linkTarget) {
	id := l.firstID
	for _, d := range l.u.CodeSymbols {
		defined[id] = linkTarget{kind: linkCode, unit: unit, part: int32(l.sectionOf(d.Offset)), offset: int32(d.Offset)}
		table.define(id, d.Symbol, d.Instance)
		id++
	}
	for _, d := range l.u.DataSymbols {
		defined[id] = linkTarget{kind: linkData, unit: unit, part: int32(containing(l.pieces, d.Offset)), offset: int32(d.Offset)}
		table.define(id, d.Symbol, d.Instance)
		id++
	}
}

// setLive marks a section or a piece, and says whether this call was the one that did, which is how a section is
// taken by exactly one job.
func setLive(flag *atomic.Bool) bool {
	return !flag.Load() && flag.CompareAndSwap(false, true)
}

// markLive finds the sections and pieces that the entry reaches through the symbols and data their code refers to. It
// reads the sections found so far in parallel and gathers the ones they newly reach into the next round, in chunk order.
// Which sections are reached does not depend on the order they are read in, so neither does the layout.
func markLive(links []*linkUnit, table *symbolTable, defined []linkTarget, start linkTarget, jobs int) {
	links[start.unit].liveCode[start.part].Store(true)
	round := []liveSection{{start.unit, start.part}}
	for len(round) > 0 {
		size := min(markChunkMax, max(1, len(round)/(jobs*markChunksPerJob)))
		chunks := (len(round) + size - 1) / size
		found := make([][]liveSection, chunks)
		par.ForEachIndex(chunks, jobs, func(c int) {
			var reached []liveSection
			for _, s := range round[c*size : min((c+1)*size, len(round))] {
				reached = links[s.unit].mark(links, table, defined, s.index, reached)
			}
			found[c] = reached
		})
		var next []liveSection
		for _, reached := range found {
			next = append(next, reached...)
		}
		round = next
	}
}

// mark reads one live section: the pieces of data and the sections of code it refers to. It appends the sections that
// no one had reached before.
func (l *linkUnit) mark(links []*linkUnit, table *symbolTable, defined []linkTarget, section int32, reached []liveSection) []liveSection {
	a := l.u.Asm
	from, to := l.sections[section], l.sections[section+1]
	for j := from.DataFixups; j < to.DataFixups; j++ {
		p := containing(l.pieces, a.DataFixups[j].DataOffset)
		l.dataPieces[j] = int32(p)
		setLive(&l.liveData[p])
	}
	for j := from.SymbolFixups; j < to.SymbolFixups; j++ {
		id := table.lookup(a.SymbolFixups[j].Symbol)
		l.targets[j] = id
		switch t := defined[id]; t.kind {
		case linkUndefined:
			panic(diag.Internal("undefined symbol %s", a.SymbolFixups[j].Symbol))
		case linkData:
			setLive(&links[t.unit].liveData[t.part])
		case linkCode:
			if setLive(&links[t.unit].liveCode[t.part]) {
				reached = append(reached, liveSection{t.unit, t.part})
			}
		case linkImageDataStart, linkImageDataEnd:
		default:
			panic(diag.Internal("link: unknown target kind %d", int(t.kind)))
		}
	}
	return reached
}

// layOut places the live sections and pieces, unit after unit, and gives the sizes of the code and the data. Each unit
// places its own parts from its own start, which is aligned, so the units can do it at once; the running sum over the
// units then gives each its base.
func layOut(links []*linkUnit, jobs int) (codeSize, dataSize int) {
	par.ForEachIndex(len(links), jobs, func(i int) { links[i].place() })
	for _, l := range links {
		if l.hasCode {
			codeSize = linkAlign(codeSize, linkCodeAlignment)
		}
		l.codeBase = codeSize
		codeSize += l.codeSize
		if l.hasData {
			dataSize = linkAlign(dataSize, linkDataAlignment)
		}
		l.dataBase = dataSize
		dataSize += l.dataSize
	}
	return codeSize, dataSize
}

// place gives each live section and piece its offset from the start of the unit's code and its data.
func (l *linkUnit) place() {
	for s := range l.liveCode {
		if l.liveCode[s].Load() {
			l.hasCode = true
			l.codeAt[s] = l.codeSize
			l.codeSize += l.sections[s+1].Code - l.sections[s].Code
		}
	}
	for p := range l.liveData {
		if l.liveData[p].Load() {
			l.hasData = true
			l.dataSize = linkAlign(l.dataSize, linkDataAlignment)
			l.dataAt[p] = l.dataSize
			l.dataSize += l.pieces[p+1] - l.pieces[p]
		}
	}
}

// finalOffset is where a symbol's definition ends up in the code or the data section.
func finalOffset(links []*linkUnit, t linkTarget) int {
	l := links[t.unit]
	switch t.kind {
	case linkData:
		return l.dataBase + l.dataAt[t.part] + int(t.offset) - l.pieces[t.part]
	case linkCode:
		return l.codeBase + l.codeAt[t.part] + int(t.offset) - l.sections[t.part].Code
	}
	panic(diag.Internal("link: a target of kind %d has no offset", int(t.kind)))
}

// copyAndPatch puts the live sections and pieces of a unit into the image and fills in the addresses they refer to. It
// writes only the unit's own place in the image, and reads where the other units went, which the layout has settled.
func (l *linkUnit) copyAndPatch(links []*linkUnit, defined []linkTarget, image *PeImage) {
	a := l.u.Asm
	code, data := image.Code, image.Data
	for p := range l.liveData {
		if l.liveData[p].Load() {
			copy(data[l.dataBase+l.dataAt[p]:], l.data[l.pieces[p]:l.pieces[p+1]])
		}
	}
	for s := range l.liveCode {
		if !l.liveCode[s].Load() {
			continue
		}
		from, to := l.sections[s], l.sections[s+1]
		at := l.codeBase + l.codeAt[s]
		copy(code[at:], a.Code[from.Code:to.Code])
		shift := at - from.Code
		for j := from.DataFixups; j < to.DataFixups; j++ {
			f, p := a.DataFixups[j], l.dataPieces[j]
			linkPatch(code, f.Pos+shift, image.TextRva, image.DataRva+uint32(l.dataBase+l.dataAt[p]+f.DataOffset-l.pieces[p]))
		}
		for _, f := range a.ImportFixups[from.ImportFixups:to.ImportFixups] {
			linkPatch(code, f.Pos+shift, image.TextRva, image.IatRva+uint32(importSlotBytes*f.Import))
		}
		for j := from.SymbolFixups; j < to.SymbolFixups; j++ {
			var target uint32
			switch t := defined[l.targets[j]]; t.kind {
			case linkImageDataStart:
				target = image.DataRva
			case linkImageDataEnd:
				target = image.DataRva + uint32(len(data))
			case linkData:
				target = image.DataRva + uint32(finalOffset(links, t))
			case linkCode:
				target = image.TextRva + uint32(finalOffset(links, t))
			default:
				panic(diag.Internal("link: a live fixup has a target of kind %d", int(t.kind)))
			}
			linkPatch(code, a.SymbolFixups[j].Pos+shift, image.TextRva, target)
		}
	}
}

func linkPatch(code []byte, pos int, textRva, target uint32) {
	binary.LittleEndian.PutUint32(code[pos:], target-(textRva+uint32(pos)+4))
}
