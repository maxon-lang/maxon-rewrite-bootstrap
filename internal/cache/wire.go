package cache

import (
	"encoding/binary"
	"errors"
)

// The wire format is what an entry's payload is written in. It is explicit and small, so a program in any language can
// read it: an unsigned integer is a base-128 varint (least significant group first, the high bit says another follows), a
// bool is one byte, and a string or a byte string is its length as an integer and then its bytes. A list is its count as
// an integer and then its items. Nothing is aligned and nothing is reflected over.

// ErrCorrupt is what a Decoder says when the bytes are not what the format allows: it ended in the middle of a value, a
// length ran past the end, or a value was out of the range the caller allows. An entry that fails this way is a miss.
var ErrCorrupt = errors.New("cache: corrupt entry")

// Encoder appends values to a byte slice.
type Encoder struct{ Bytes []byte }

func (e *Encoder) Uvarint(v uint64) { e.Bytes = binary.AppendUvarint(e.Bytes, v) }

func (e *Encoder) Int(v int) {
	if v < 0 {
		panic("cache: a negative number has no encoding")
	}
	e.Uvarint(uint64(v))
}

func (e *Encoder) Bool(v bool) {
	if v {
		e.Bytes = append(e.Bytes, 1)
	} else {
		e.Bytes = append(e.Bytes, 0)
	}
}

func (e *Encoder) Text(s string) {
	e.Int(len(s))
	e.Bytes = append(e.Bytes, s...)
}

func (e *Encoder) Blob(b []byte) {
	e.Int(len(b))
	e.Bytes = append(e.Bytes, b...)
}

// Decoder reads what an Encoder wrote. The first failure is kept and every later read gives a zero value, so a reader
// checks Err once, after the last read.
type Decoder struct {
	data []byte
	err  error
}

func NewDecoder(data []byte) *Decoder { return &Decoder{data: data} }

// Err is the first failure, or nil.
func (d *Decoder) Err() error { return d.err }

// Fail records that the data are not valid, for a value that the format allows but the caller does not.
func (d *Decoder) Fail() {
	if d.err == nil {
		d.err = ErrCorrupt
	}
}

// Done fails unless every byte has been read.
func (d *Decoder) Done() error {
	if d.err == nil && len(d.data) != 0 {
		d.err = ErrCorrupt
	}
	return d.err
}

func (d *Decoder) Uvarint() uint64 {
	if d.err != nil {
		return 0
	}
	v, n := binary.Uvarint(d.data)
	if n <= 0 {
		d.Fail()
		return 0
	}
	d.data = d.data[n:]
	return v
}

// Int is a number that has to be at most max, which a caller sets from what it can still use: a count never exceeds the
// bytes that are left, so a corrupt length cannot ask for gigabytes.
func (d *Decoder) Int(max int) int {
	v := d.Uvarint()
	if v > uint64(max) {
		d.Fail()
		return 0
	}
	return int(v)
}

// Count is the length of a list whose items take at least one byte each, so it is at most the bytes that are left once
// it is read.
func (d *Decoder) Count() int {
	v := d.Uvarint()
	if v > uint64(len(d.data)) {
		d.Fail()
		return 0
	}
	return int(v)
}

func (d *Decoder) Bool() bool {
	if d.err != nil || len(d.data) == 0 || d.data[0] > 1 {
		d.Fail()
		return false
	}
	v := d.data[0] == 1
	d.data = d.data[1:]
	return v
}

// Blob is a byte string that shares the decoder's data.
func (d *Decoder) Blob() []byte {
	n := d.Count()
	if d.err != nil {
		return nil
	}
	b := d.data[:n:n]
	d.data = d.data[n:]
	return b
}

func (d *Decoder) Text() string { return string(d.Blob()) }
