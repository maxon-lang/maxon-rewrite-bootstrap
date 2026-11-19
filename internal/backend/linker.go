package backend

import (
	"encoding/binary"
	"minimaxon/internal/diag"
	"minimaxon/internal/ir"
	"minimaxon/internal/par"
	"sort"
	"sync"
)

const (
	linkCodeAlignment = 16
	linkDataAlignment = 8
	importSlotBytes   = 8
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
	liveCode []bool
	liveData []bool
	codeAt   []int
	dataAt   []int
	// targets is the index in the link's definitions of what each symbol fixup names, and dataPieces the piece each data
	// fixup points into.
	targets    []int32
	dataPieces []int32
}

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
		u:        u,
		data:     data,
		sections: sections,
		pieces:   pieces,
		liveCode: make([]bool, len(sections)-1),
		liveData: make([]bool, len(pieces)-1),
		codeAt:   make([]int, len(sections)-1),
		dataAt:   make([]int, len(pieces)-1),
	}
}

func (l *linkUnit) sectionOf(offset int) int {
	return sort.Search(len(l.sections), func(i int) bool { return l.sections[i].Code > offset }) - 1
}

// Link writes the executable. It keeps only the sections of code and the pieces of data that the entry reaches, as
// Go's linker does: every function is compiled, and what nothing calls is dropped here. The layout follows the order of
// the units, and of the sections and pieces in each, so the executable does not depend on the number of jobs.
func Link(out string, units []*CodeUnit, entry any, jobs int) {
	links := make([]*linkUnit, len(units))
	par.ForEachIndex(len(units), jobs, func(i int) { links[i] = newLinkUnit(units[i]) })

	symbolCount := 0
	for _, u := range units {
		symbolCount += len(u.CodeSymbols) + len(u.DataSymbols)
	}
	// The map gives an index into defined, which keeps its entries small; index 0 is no definition.
	symbols := make(map[any]int32, symbolCount+2)
	defined := make([]linkTarget, 1, symbolCount+3)
	define := func(sym any, t linkTarget) {
		if _, ok := symbols[sym]; ok {
			panic(diag.Internal("symbol %s is defined in two units", SymbolText(sym)))
		}
		symbols[sym] = int32(len(defined))
		defined = append(defined, t)
	}
	var symbolLock sync.Mutex
	par.ForEachIndex(len(units), jobs, func(i int) {
		l := links[i]
		symbolLock.Lock()
		defer symbolLock.Unlock()
		for sym, off := range l.u.CodeSymbols {
			define(sym, linkTarget{kind: linkCode, unit: int32(i), part: int32(l.sectionOf(off)), offset: int32(off)})
		}
		for sym, off := range l.u.DataSymbols {
			define(sym, linkTarget{kind: linkData, unit: int32(i), part: int32(containing(l.pieces, off)), offset: int32(off)})
		}
	})
	define(ir.RtImageDataStart, linkTarget{kind: linkImageDataStart})
	define(ir.RtImageDataEnd, linkTarget{kind: linkImageDataEnd})
	par.ForEachIndex(len(links), jobs, func(i int) { links[i].resolve(symbols) })

	start := defined[symbols[entry]]
	if start.kind != linkCode {
		panic(diag.Internal("no entry symbol %s", SymbolText(entry)))
	}
	markLive(links, defined, start)
	codeSize, dataSize := layOut(links)
	code := make([]byte, codeSize)
	data := make([]byte, dataSize)
	par.ForEachIndex(len(links), jobs, func(i int) {
		l := links[i]
		for s, live := range l.liveCode {
			if live {
				copy(code[l.codeAt[s]:], l.u.Asm.Code[l.sections[s].Code:l.sections[s+1].Code])
			}
		}
		for p, live := range l.liveData {
			if live {
				copy(data[l.dataAt[p]:], l.data[l.pieces[p]:l.pieces[p+1]])
			}
		}
	})

	WritePe(out, code, data, ir.RtDll, ir.RtImports, finalOffset(links, start), func(textRva, dataRva, iatRva uint32) {
		par.ForEachIndex(len(links), jobs, func(i int) {
			links[i].patch(links, defined, code, uint32(dataSize), textRva, dataRva, iatRva)
		})
	})
}

// resolve finds what each fixup of a unit refers to. A symbol that nothing defines is an error only if live code uses it.
func (l *linkUnit) resolve(symbols map[any]int32) {
	a := l.u.Asm
	l.targets = make([]int32, len(a.SymbolFixups))
	for i, f := range a.SymbolFixups {
		l.targets[i] = symbols[f.Symbol]
	}
	l.dataPieces = make([]int32, len(a.DataFixups))
	for i, f := range a.DataFixups {
		l.dataPieces[i] = int32(containing(l.pieces, f.DataOffset))
	}
}

// markLive finds the sections and pieces that the entry reaches through the symbols and data their code refers to.
func markLive(links []*linkUnit, defined []linkTarget, start linkTarget) {
	type section struct{ unit, index int32 }
	links[start.unit].liveCode[start.part] = true
	queue := []section{{start.unit, start.part}}
	for len(queue) > 0 {
		next := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		l := links[next.unit]
		from, to := l.sections[next.index], l.sections[next.index+1]
		for _, p := range l.dataPieces[from.DataFixups:to.DataFixups] {
			l.liveData[p] = true
		}
		for i := from.SymbolFixups; i < to.SymbolFixups; i++ {
			switch t := defined[l.targets[i]]; t.kind {
			case linkUndefined:
				panic(diag.Internal("undefined symbol %s", SymbolText(l.u.Asm.SymbolFixups[i].Symbol)))
			case linkData:
				links[t.unit].liveData[t.part] = true
			case linkCode:
				if target := links[t.unit]; !target.liveCode[t.part] {
					target.liveCode[t.part] = true
					queue = append(queue, section{t.unit, t.part})
				}
			case linkImageDataStart, linkImageDataEnd:
			default:
				panic(diag.Internal("link: unknown target kind %d", int(t.kind)))
			}
		}
	}
}

// layOut places the live sections and pieces, unit after unit, and gives the sizes of the code and the data.
func layOut(links []*linkUnit) (codeSize, dataSize int) {
	for _, l := range links {
		placed := false
		for s, live := range l.liveCode {
			if !live {
				continue
			}
			if !placed {
				codeSize = linkAlign(codeSize, linkCodeAlignment)
				placed = true
			}
			l.codeAt[s] = codeSize
			codeSize += l.sections[s+1].Code - l.sections[s].Code
		}
		for p, live := range l.liveData {
			if !live {
				continue
			}
			dataSize = linkAlign(dataSize, linkDataAlignment)
			l.dataAt[p] = dataSize
			dataSize += l.pieces[p+1] - l.pieces[p]
		}
	}
	return codeSize, dataSize
}

// finalOffset is where a symbol's definition ends up in the code or the data section.
func finalOffset(links []*linkUnit, t linkTarget) int {
	l := links[t.unit]
	switch t.kind {
	case linkData:
		return l.dataAt[t.part] + int(t.offset) - l.pieces[t.part]
	case linkCode:
		return l.codeAt[t.part] + int(t.offset) - l.sections[t.part].Code
	}
	panic(diag.Internal("link: a target of kind %d has no offset", int(t.kind)))
}

// patch fills in the addresses that the live sections of a unit refer to.
func (l *linkUnit) patch(links []*linkUnit, defined []linkTarget, code []byte, dataSize, textRva, dataRva, iatRva uint32) {
	a := l.u.Asm
	for s, live := range l.liveCode {
		if !live {
			continue
		}
		from, to := l.sections[s], l.sections[s+1]
		shift := l.codeAt[s] - from.Code
		for j := from.DataFixups; j < to.DataFixups; j++ {
			f, p := a.DataFixups[j], l.dataPieces[j]
			linkPatch(code, f.Pos+shift, textRva, dataRva+uint32(l.dataAt[p]+f.DataOffset-l.pieces[p]))
		}
		for _, f := range a.ImportFixups[from.ImportFixups:to.ImportFixups] {
			linkPatch(code, f.Pos+shift, textRva, iatRva+uint32(importSlotBytes*f.Import))
		}
		for j := from.SymbolFixups; j < to.SymbolFixups; j++ {
			var target uint32
			switch t := defined[l.targets[j]]; t.kind {
			case linkImageDataStart:
				target = dataRva
			case linkImageDataEnd:
				target = dataRva + dataSize
			case linkData:
				target = dataRva + uint32(finalOffset(links, t))
			case linkCode:
				target = textRva + uint32(finalOffset(links, t))
			default:
				panic(diag.Internal("link: a live fixup has a target of kind %d", int(t.kind)))
			}
			linkPatch(code, a.SymbolFixups[j].Pos+shift, textRva, target)
		}
	}
}

func linkPatch(code []byte, pos int, textRva, target uint32) {
	binary.LittleEndian.PutUint32(code[pos:], target-(textRva+uint32(pos)+4))
}
