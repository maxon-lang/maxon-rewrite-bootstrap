package backend

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"minimaxon/internal/diag"
	"os"
	"path/filepath"
	"sync"
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
	peWriteChunk                = 4 << 20
)

type peSection struct {
	name  string
	size  int
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

// PeImage is the executable's file in memory. The code and the data are views into it, at the places they have in the
// file, so the linker writes them there once and nothing is copied again.
type PeImage struct {
	Code, Data []byte
	// The addresses, counted from the image base, of the sections that code refers to.
	TextRva, DataRva, IatRva uint32
	file                     []byte
	// codeOffset is where the code is in the file.
	codeOffset int
}

// NewPeImage lays out the file for sections of the given sizes, with its headers and imports written, and the entry
// point at an offset in the code.
func NewPeImage(codeSize, dataSize int, dll string, imports []string, entryOffset int) *PeImage {
	dataRva := peTextRva + peAlignUp(uint32(max(codeSize, 1)), peSectionAlignment)
	idataRva := dataRva + peAlignUp(uint32(max(dataSize, 1)), peSectionAlignment)
	idata, iatOffset, iatSize := buildImports(idataRva, dll, imports)

	// A section with no data still has bytes in the file.
	dataLength := dataSize
	if dataSize == 0 {
		dataLength = peEmptyDataSize
	}
	sections := []peSection{
		{".text", codeSize, peTextRva, peTextFlags},
		{".data", dataLength, dataRva, peDataFlags},
		{".idata", len(idata), idataRva, peDataFlags},
	}

	const headerBytes = 64 + 4 + 20 + 240
	headersSize := peAlignUp(headerBytes+peSectionHeader*uint32(len(sections)), peFileAlignment)
	imageSize := idataRva + peAlignUp(uint32(len(idata)), peSectionAlignment)

	fileSize := headersSize
	var fileOffsets []uint32
	for _, s := range sections {
		fileOffsets = append(fileOffsets, fileSize)
		fileSize += peAlignUp(uint32(s.size), peFileAlignment)
	}
	file := make([]byte, fileSize)
	image := &PeImage{
		Code:    file[fileOffsets[0] : fileOffsets[0]+uint32(codeSize)],
		Data:    file[fileOffsets[1] : fileOffsets[1]+uint32(dataSize)],
		TextRva: peTextRva, DataRva: dataRva, IatRva: idataRva + iatOffset,
		file: file, codeOffset: int(fileOffsets[0]),
	}
	copy(file[fileOffsets[2]:], idata)

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
	w.u32(peAlignUp(uint32(codeSize), peFileAlignment))
	w.u32(peAlignUp(uint32(dataSize), peFileAlignment) + peAlignUp(uint32(len(idata)), peFileAlignment))
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

	for i, s := range sections {
		nameBytes := make([]byte, 8)
		copy(nameBytes, s.name)
		w.Write(nameBytes)
		w.u32(uint32(s.size))
		w.u32(s.rva)
		w.u32(peAlignUp(uint32(s.size), peFileAlignment))
		w.u32(fileOffsets[i])
		w.u32(0)
		w.u32(0)
		w.u16(0)
		w.u16(0)
		w.u32(s.flags)
	}
	copy(file, w.Bytes())
	return image
}

// PeOutput writes an image while the linker is still filling in the code, so that writing it overlaps the rest of the
// link instead of following it. The linker says how much of the code is final, and what is final is written in chunks:
// the kernel takes a large file in one call about half as fast as in several. The image goes to a file of its own
// beside the executable, which replaces the executable only once it is complete, so a link that fails leaves the old
// executable as it was.
type PeOutput struct {
	mu    sync.Mutex
	moved *sync.Cond
	image *PeImage
	// final is how many bytes of the image will not change again.
	final     int
	abandoned bool
	finished  bool
	done      chan error
}

// OpenPeOutput starts creating the file.
func OpenPeOutput(path string) *PeOutput {
	o := &PeOutput{done: make(chan error, 1)}
	o.moved = sync.NewCond(&o.mu)
	go func() { o.done <- o.run(path) }()
	return o
}

// ready is whether the next chunk after written bytes can be written, which is the case once the image is known and
// that chunk is final.
func (o *PeOutput) ready(written int) bool {
	return o.image != nil && o.final >= min(written+peWriteChunk, len(o.image.file))
}

func (o *PeOutput) run(path string) error {
	file, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("cannot write %s: %v", path, err)
	}
	complete, err := o.fill(file)
	closeErr := file.Close()
	if err == nil && closeErr != nil {
		err = fmt.Errorf("cannot write %s: %v", path, closeErr)
	}
	if err == nil && complete {
		if err = os.Rename(file.Name(), path); err != nil {
			err = fmt.Errorf("cannot write %s: %v", path, err)
		}
	}
	if err != nil || !complete {
		os.Remove(file.Name())
	}
	return err
}

// fill writes the image to the file as it becomes final; it is not complete when the link was abandoned.
func (o *PeOutput) fill(file *os.File) (complete bool, err error) {
	for written := 0; ; {
		o.mu.Lock()
		for !o.abandoned && !o.ready(written) {
			o.moved.Wait()
		}
		abandoned, image := o.abandoned, o.image
		o.mu.Unlock()
		if abandoned {
			return false, nil
		}
		next := min(written+peWriteChunk, len(image.file))
		if _, err := file.Write(image.file[written:next]); err != nil {
			return false, fmt.Errorf("cannot write %s: %v", file.Name(), err)
		}
		written = next
		if written == len(image.file) {
			return true, nil
		}
	}
}

func (o *PeOutput) update(change func()) {
	o.mu.Lock()
	change()
	o.mu.Unlock()
	o.moved.Signal()
}

// Start gives the image to write, whose headers are final.
func (o *PeOutput) Start(image *PeImage) {
	o.update(func() { o.image, o.final = image, image.codeOffset })
}

// CodeFinal says that the first n bytes of the code will not change again.
func (o *PeOutput) CodeFinal(n int) {
	o.update(func() { o.final = max(o.final, o.image.codeOffset+n) })
}

// Finish says that the whole image is final, and returns once it is written.
func (o *PeOutput) Finish() {
	o.update(func() { o.final = len(o.image.file) })
	err := <-o.done
	o.finished = true
	if err != nil {
		panic(diag.Internal("%v", err))
	}
}

// Abandon stops writing, for a link that failed, and closes the file. It does nothing after Finish.
func (o *PeOutput) Abandon() {
	if o.finished {
		return
	}
	o.update(func() { o.abandoned = true })
	<-o.done
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
