package main

import (
	"bytes"
	"os"
)

const fileChangedMessage = "the file changed while it was being read"

var utf8Bom = []byte{0xEF, 0xBB, 0xBF}

func LoadModuleSource(files []string) ([]byte, []FileSlice) {
	var total int64
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			panic(err)
		}
		total += info.Size()
	}

	buffer := make([]byte, total)
	slices := make([]FileSlice, 0, len(files))
	at := 0

	for _, file := range files {
		end := readSourceFile(file, buffer, at)
		end = StripComments(buffer, at, end)
		slices = append(slices, FileSlice{File: file, Begin: at, End: end})
		at = end
	}
	return buffer, slices
}

func readSourceFile(file string, buffer []byte, at int) int {
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
	if length > int64(len(buffer)-at) {
		panic(NewError(ErrUnexpectedToken, fileChangedMessage, file, 1, 1))
	}

	size := int(length)
	read := 0
	for read < size {
		n, err := handle.ReadAt(buffer[at+read:at+size], int64(read))
		read += n
		if n == 0 && err != nil {
			panic(NewError(ErrUnexpectedToken, fileChangedMessage, file, 1, 1))
		}
	}

	if bytes.HasPrefix(buffer[at:at+size], utf8Bom) {
		copy(buffer[at:], buffer[at+len(utf8Bom):at+size])
		size -= len(utf8Bom)
	}
	return at + size
}

func StripComments(b []byte, begin, end int) int {
	r := begin
	w := begin
	lineHasCode := false

	for r < end {
		c := b[r]

		if c == '\n' {
			b[w] = c
			w++
			r++
			lineHasCode = false
			continue
		}

		if c == '/' && r+1 < end && b[r+1] == '/' {
			keep := !lineHasCode && r+2 < end && b[r+2] == '/'
			for r < end && b[r] != '\n' {
				if keep {
					b[w] = b[r]
					w++
				}
				r++
			}
			continue
		}

		if c == '/' && r+1 < end && b[r+1] == '*' {
			closing := indexStar(b, r+2, end)
			for closing >= 0 && (closing+1 >= end || b[closing+1] != '/') {
				if closing+1 < end {
					closing = indexStar(b, closing+1, end)
				} else {
					closing = -1
				}
			}
			if closing < 0 {
				for r < end {
					b[w] = b[r]
					w++
					r++
				}
				break
			}
			after := closing + 2
			lastLineStart := r
			for k := r; k < after; k++ {
				if b[k] == '\n' {
					b[w] = '\n'
					w++
					lastLineStart = k + 1
				}
			}
			if after < end && b[after] != '\n' && b[after] != '\r' {
				for k := lastLineStart; k < after; k++ {
					b[w] = ' '
					w++
				}
			}
			r = after
			continue
		}

		if c == '"' {
			stop := skipString(b, r, end)
			for r < stop {
				b[w] = b[r]
				w++
				r++
			}
			lineHasCode = true
			continue
		}

		if c == '\'' {
			b[w] = b[r]
			w++
			r++
			for r < end && b[r] != '\'' && b[r] != '\n' {
				b[w] = b[r]
				w++
				r++
			}
			if r < end && b[r] == '\'' {
				b[w] = b[r]
				w++
				r++
			}
			lineHasCode = true
			continue
		}

		if c != ' ' && c != '\t' && c != '\r' {
			lineHasCode = true
		}
		b[w] = c
		w++
		r++
	}
	return w
}

func indexStar(b []byte, from, end int) int {
	i := bytes.IndexByte(b[from:end], '*')
	if i < 0 {
		return -1
	}
	return from + i
}

func skipString(b []byte, open, end int) int {
	i := open + 1
	for i < end && b[i] != '\n' {
		switch b[i] {
		case '\\':
			i += 2
		case '"':
			return i + 1
		case '{':
			i = skipInterpolation(b, i+1, end)
		default:
			i++
		}
	}
	return min(i, end)
}

func skipInterpolation(b []byte, i, end int) int {
	nest := 0
	for i < end && b[i] != '\n' {
		switch {
		case b[i] == '{':
			nest++
			i++
		case b[i] == '}' && nest == 0:
			return i + 1
		case b[i] == '}':
			nest--
			i++
		case b[i] == '"':
			i++
			for i < end && b[i] != '"' && b[i] != '\n' {
				if b[i] == '\\' {
					i += 2
				} else {
					i++
				}
			}
			i++
		default:
			i++
		}
	}
	return i
}
