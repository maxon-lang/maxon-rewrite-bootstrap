package backend

import (
	"bytes"
	"encoding/binary"
	"minimaxon/internal/diag"
	"os"
)

const (
	peFileAlignment      uint32 = 0x200
	peSectionAlignment   uint32 = 0x1000
	peImageBase          uint64 = 0x140000000
	peTextRva            uint32 = 0x1000
	peDosMagic                  = 0x5A4D
	peSignature          uint32 = 0x00004550
	peMachineX64                = 0x8664
	peOptionalSize              = 240
	peCharacteristics           = 0x22
	peOptionalMagic             = 0x20B
	peLinkerMajor               = 14
	peOsVersionMajor            = 6
	peSubsystemConsole          = 3
	peDllCharacteristics        = 0x8160
	peStackReserve       uint64 = 0x1000000
	peStackCommit        uint64 = 0x10000
	peHeapReserve        uint64 = 0x100000
	peHeapCommit         uint64 = 0x1000
	peDirectoryCount     uint32 = 16
	peImportDirectory           = 1
	peIatDirectory              = 12
	peDosHeaderPad              = 58
	peDosHeaderSize      uint32 = 64
	peSectionHeader      uint32 = 40
	peTextFlags          uint32 = 0x60000020
	peDataFlags          uint32 = 0xC0000040
	peImportDescriptor   uint32 = 40
	peEmptyDataSize             = 8
)

type peSection struct {
	name  string
	bytes []byte
	rva   uint32
	flags uint32
}

type peBuffer struct {
	bytes.Buffer
}

func (w *peBuffer) u16(v uint16) {
	w.Write(binary.LittleEndian.AppendUint16(nil, v))
}

func (w *peBuffer) u32(v uint32) {
	w.Write(binary.LittleEndian.AppendUint32(nil, v))
}

func (w *peBuffer) u64(v uint64) {
	w.Write(binary.LittleEndian.AppendUint64(nil, v))
}

func (w *peBuffer) zeros(n int) {
	w.Write(make([]byte, n))
}

func peAlignUp(v, a uint32) uint32 {
	return (v + a - 1) &^ (a - 1)
}

func WritePe(path string, code, data []byte, dll string, imports []string, entryOffset int, patch func(textRva, dataRva, iatRva uint32)) {
	dataRva := peTextRva + peAlignUp(uint32(max(len(code), 1)), peSectionAlignment)
	idataRva := dataRva + peAlignUp(uint32(max(len(data), 1)), peSectionAlignment)

	idata, iatOffset, iatSize := buildImports(idataRva, dll, imports)
	patch(peTextRva, dataRva, idataRva+iatOffset)

	dataBytes := data
	if len(data) == 0 {
		dataBytes = make([]byte, peEmptyDataSize)
	}
	sections := []peSection{
		{".text", code, peTextRva, peTextFlags},
		{".data", dataBytes, dataRva, peDataFlags},
		{".idata", idata, idataRva, peDataFlags},
	}

	const headerBytes = 64 + 4 + 20 + 240
	headersSize := peAlignUp(headerBytes+peSectionHeader*uint32(len(sections)), peFileAlignment)
	imageSize := idataRva + peAlignUp(uint32(len(idata)), peSectionAlignment)

	var w peBuffer

	w.u16(peDosMagic)
	w.zeros(peDosHeaderPad)
	w.u32(peDosHeaderSize)

	w.u32(peSignature)
	w.u16(peMachineX64)
	w.u16(uint16(len(sections)))
	w.u32(0)
	w.u32(0)
	w.u32(0)
	w.u16(peOptionalSize)
	w.u16(peCharacteristics)

	w.u16(peOptionalMagic)
	w.Write([]byte{peLinkerMajor, 0})
	w.u32(peAlignUp(uint32(len(code)), peFileAlignment))
	w.u32(peAlignUp(uint32(len(data)), peFileAlignment) + peAlignUp(uint32(len(idata)), peFileAlignment))
	w.u32(0)
	w.u32(peTextRva + uint32(entryOffset))
	w.u32(peTextRva)
	w.u64(peImageBase)
	w.u32(peSectionAlignment)
	w.u32(peFileAlignment)
	w.u16(peOsVersionMajor)
	w.u16(0)
	w.u16(0)
	w.u16(0)
	w.u16(peOsVersionMajor)
	w.u16(0)
	w.u32(0)
	w.u32(imageSize)
	w.u32(headersSize)
	w.u32(0)
	w.u16(peSubsystemConsole)
	w.u16(peDllCharacteristics)
	w.u64(peStackReserve)
	w.u64(peStackCommit)
	w.u64(peHeapReserve)
	w.u64(peHeapCommit)
	w.u32(0)
	w.u32(peDirectoryCount)

	for i := range int(peDirectoryCount) {
		switch i {
		case peImportDirectory:
			w.u32(idataRva)
			w.u32(peImportDescriptor)
		case peIatDirectory:
			w.u32(idataRva + iatOffset)
			w.u32(iatSize)
		default:
			w.u32(0)
			w.u32(0)
		}
	}

	raw := headersSize
	for _, s := range sections {
		nameBytes := make([]byte, 8)
		copy(nameBytes, s.name)
		w.Write(nameBytes)
		w.u32(uint32(len(s.bytes)))
		w.u32(s.rva)
		w.u32(peAlignUp(uint32(len(s.bytes)), peFileAlignment))
		w.u32(raw)
		w.u32(0)
		w.u32(0)
		w.u16(0)
		w.u16(0)
		w.u32(s.flags)
		raw += peAlignUp(uint32(len(s.bytes)), peFileAlignment)
	}

	w.zeros(int(headersSize) - w.Len())
	for _, s := range sections {
		w.Write(s.bytes)
		w.zeros(int(peAlignUp(uint32(len(s.bytes)), peFileAlignment)) - len(s.bytes))
	}

	if err := os.WriteFile(path, w.Bytes(), 0o644); err != nil {
		panic(diag.Internal("cannot write %s: %v", path, err))
	}
}

func buildImports(rva uint32, dll string, imports []string) ([]byte, uint32, uint32) {
	n := uint32(len(imports))
	iltOffset := peImportDescriptor
	iatOffset := iltOffset + (n+1)*8
	namesOffset := iatOffset + (n+1)*8

	var names []byte
	nameRvas := make([]uint32, 0, len(imports))
	for _, name := range imports {
		nameRvas = append(nameRvas, rva+namesOffset+uint32(len(names)))
		names = append(names, 0, 0)
		names = append(names, name...)
		names = append(names, 0)
		if len(names)%2 != 0 {
			names = append(names, 0)
		}
	}
	dllRva := rva + namesOffset + uint32(len(names))
	names = append(names, dll...)
	names = append(names, 0)

	var w peBuffer
	w.u32(rva + iltOffset)
	w.u32(0)
	w.u32(0)
	w.u32(dllRva)
	w.u32(rva + iatOffset)
	w.zeros(20)
	for range 2 {
		for _, nr := range nameRvas {
			w.u64(uint64(nr))
		}
		w.u64(0)
	}
	w.Write(names)
	return w.Bytes(), iatOffset, (n + 1) * 8
}
