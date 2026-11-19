package main

import (
	"encoding/binary"
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

func linkOffsets(sizes []int, alignment int) ([]int, int) {
	offsets := make([]int, len(sizes))
	at := 0
	for i, size := range sizes {
		offsets[i] = linkAlign(at, alignment)
		at = offsets[i] + size
	}
	return offsets, at
}

func Link(out string, units []*CodeUnit, entry any, jobs int) {
	unitData := make([][]byte, len(units))
	ForEachIndex(len(units), jobs, func(i int) { unitData[i] = units[i].Data.ToArray() })

	codeSizes := make([]int, len(units))
	dataSizes := make([]int, len(units))
	codeSymbolCount := 0
	dataSymbolCount := 0
	for i, u := range units {
		codeSizes[i] = len(u.Asm.Code)
		dataSizes[i] = len(unitData[i])
		codeSymbolCount += len(u.CodeSymbols)
		dataSymbolCount += len(u.DataSymbols)
	}
	codeBase, codeSize := linkOffsets(codeSizes, linkCodeAlignment)
	dataBase, dataSize := linkOffsets(dataSizes, linkDataAlignment)
	code := make([]byte, codeSize)
	data := make([]byte, dataSize)
	codeSymbols := make(map[any]int, codeSymbolCount)
	dataSymbols := make(map[any]int, dataSymbolCount)
	var symbolLock sync.Mutex

	ForEachIndex(len(units), jobs, func(i int) {
		u := units[i]
		copy(code[codeBase[i]:], u.Asm.Code)
		copy(data[dataBase[i]:], unitData[i])
		symbolLock.Lock()
		defer symbolLock.Unlock()
		for sym, off := range u.CodeSymbols {
			if _, ok := codeSymbols[sym]; ok {
				panic(Internal("code symbol %s is defined in two units", SymbolText(sym)))
			}
			codeSymbols[sym] = codeBase[i] + off
		}
		for sym, off := range u.DataSymbols {
			if _, ok := dataSymbols[sym]; ok {
				panic(Internal("data symbol %s is defined in two units", SymbolText(sym)))
			}
			dataSymbols[sym] = dataBase[i] + off
		}
	})

	entryOffset, ok := codeSymbols[entry]
	if !ok {
		panic(Internal("no entry symbol %s", SymbolText(entry)))
	}

	WritePe(out, code, data, RtDll, RtImports, entryOffset, func(textRva, dataRva, iatRva uint32) {
		ForEachIndex(len(units), jobs, func(i int) {
			a := units[i].Asm
			for _, f := range a.DataFixups {
				linkPatch(code, codeBase[i]+f.Pos, textRva, dataRva+uint32(dataBase[i]+f.DataOffset))
			}
			for _, f := range a.ImportFixups {
				linkPatch(code, codeBase[i]+f.Pos, textRva, iatRva+uint32(importSlotBytes*f.Import))
			}
			for _, f := range a.SymbolFixups {
				var target uint32
				if c, ok := codeSymbols[f.Symbol]; ok {
					target = textRva + uint32(c)
				} else if d, ok := dataSymbols[f.Symbol]; ok {
					target = dataRva + uint32(d)
				} else {
					panic(Internal("undefined symbol %s", SymbolText(f.Symbol)))
				}
				linkPatch(code, codeBase[i]+f.Pos, textRva, target)
			}
		})
	})
}

func linkPatch(code []byte, pos int, textRva, target uint32) {
	binary.LittleEndian.PutUint32(code[pos:], target-(textRva+uint32(pos)+4))
}
