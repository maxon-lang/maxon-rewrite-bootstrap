// Package cache is the store of the build cache (README, "Build cache"): entries found by a key, in a directory, written
// so that a crash or a second compiler writing at once cannot leave one that is half there, and read so that an entry that
// is not whole is a miss and never an error. It knows nothing of what an entry holds; the driver decides what goes in and
// what the key is made of.
package cache

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Version is the layout of an entry's file. It is written in the entry, and an entry with another is a miss, so a compiler
// that changes the layout never reads what an older one wrote even if the two share a directory. The payload's own layout
// is the caller's, and the compiler's identity in every key covers it.
const Version = 3

// Key names an entry: a hash of everything the entry's content depends on.
type Key [sha256.Size]byte

func (k Key) String() string { return hex.EncodeToString(k[:]) }

// Hasher builds a Key. Every value is written with its length or its width, so two different lists of values never
// write the same bytes.
type Hasher struct {
	sum hash.Hash
	// word is where an integer is written before it is hashed, so that writing one allocates nothing.
	word [8]byte
}

func NewHasher() *Hasher { return &Hasher{sum: sha256.New()} }

func (h *Hasher) Int(v int) {
	binary.LittleEndian.PutUint64(h.word[:], uint64(v))
	h.sum.Write(h.word[:])
}

func (h *Hasher) Bool(v bool) {
	if v {
		h.Int(1)
	} else {
		h.Int(0)
	}
}

func (h *Hasher) String(s string) {
	h.Int(len(s))
	io.WriteString(h.sum, s)
}

func (h *Hasher) Key(k Key) { h.sum.Write(k[:]) }

func (h *Hasher) Sum() Key {
	var k Key
	h.sum.Sum(k[:0])
	return k
}

// Digest is the key of a byte string.
func Digest(data []byte) Key { return sha256.Sum256(data) }

// executable is the hash of the running compiler, which is the same for the life of the process.
var executable = sync.OnceValues(func() (Key, error) {
	path, err := os.Executable()
	if err != nil {
		return Key{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return Key{}, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return Key{}, err
	}
	var k Key
	h.Sum(k[:0])
	return k, nil
})

// Executable is the hash of the compiler that is running. A key that holds it never names an entry that another build
// of the compiler wrote, because what a module compiles to is a property of the compiler as much as of the source.
func Executable() (Key, error) { return executable() }

const (
	envOff     = "off"
	appDirName = "minimaxon"
	subDirName = "cache"
)

// Dir is where the cache is kept, given the value of the MINIMAXON_CACHE environment variable: its default is a
// directory of the user's cache (on Windows, %LocalAppData%\minimaxon\cache), `off` turns the cache off, and any other
// value is the directory. enabled is false when the cache is off or the user has no cache directory.
func Dir(env string) (dir string, enabled bool) {
	switch env {
	case envOff:
		return "", false
	case "":
		base, err := os.UserCacheDir()
		if err != nil {
			return "", false
		}
		return filepath.Join(base, appDirName, subDirName), true
	}
	return env, true
}

// Clock says what time it is. The cache asks it when it notes that an entry was used and when it trims, so a test can
// give it a time of its own instead of waiting.
type Clock func() time.Time

// Cache is a directory of entries, for one compiler.
type Cache struct {
	dir      string
	compiler Key
	now      Clock
	read     atomic.Int64
	written  atomic.Int64
}

// Open is the cache in a directory, for the compiler that has the hash. Nothing is created until an entry is written.
func Open(dir string, compiler Key) *Cache {
	return &Cache{dir: dir, compiler: compiler, now: time.Now}
}

// WithClock makes the cache read the time from now, and is the cache itself.
func (c *Cache) WithClock(now Clock) *Cache {
	c.now = now
	return c
}

// Dir is the directory the cache keeps its entries in.
func (c *Cache) Dir() string { return c.dir }

// Compiler is the hash of the compiler this cache is for, which every key holds.
func (c *Cache) Compiler() Key { return c.compiler }

// BytesRead is how many bytes of entries Get has returned, and BytesWritten how many Put has stored.
func (c *Cache) BytesRead() int64    { return c.read.Load() }
func (c *Cache) BytesWritten() int64 { return c.written.Load() }

func (c *Cache) path(key Key) (dir, file string) {
	name := key.String()
	dir = filepath.Join(c.dir, name[:2])
	return dir, filepath.Join(dir, name[2:])
}

const (
	// dirMode is the permission of the directories the cache makes, and tempPattern names the file an entry is written to
	// before it is renamed, so that a stray one after a crash is told from an entry.
	dirMode     = 0o755
	tempPattern = "tmp-*"

	magic        = "MXCE"
	headerBytes  = len(magic) + 4 + 8
	trailerBytes = 4
)

var checksum = crc32.MakeTable(crc32.Castagnoli)

// Put stores the payload under the key. The entry is written whole to a file of its own and renamed over the name, so a
// reader sees the old entry or the new one and never a part of one, and a crash leaves at most a stray temporary file.
func (c *Cache) Put(key Key, payload []byte) error {
	dir, file := c.path(key)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, tempPattern)
	if err != nil {
		return err
	}
	entry := make([]byte, 0, headerBytes+len(payload)+trailerBytes)
	entry = append(entry, magic...)
	entry = binary.LittleEndian.AppendUint32(entry, Version)
	entry = binary.LittleEndian.AppendUint64(entry, uint64(len(payload)))
	entry = append(entry, payload...)
	entry = binary.LittleEndian.AppendUint32(entry, crc32.Checksum(payload, checksum))
	_, err = tmp.Write(entry)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), file)
	}
	if err != nil {
		os.Remove(tmp.Name())
		return err
	}
	c.written.Add(int64(len(entry)))
	return nil
}

// Get is the payload stored under the key. An entry that is missing, cut short, longer than it says, of another version
// or damaged is no entry, and so is one that trimming removes while it is read. An entry that is used and was last used
// long ago is marked as used now, which is what keeps trimming from removing it.
func (c *Cache) Get(key Key) (payload []byte, ok bool) {
	_, file := c.path(key)
	entry, modified, err := readEntryFile(file)
	if err != nil {
		return nil, false
	}
	payload, err = openEntry(entry)
	if err != nil {
		return nil, false
	}
	c.read.Add(int64(len(entry)))
	c.markUsed(file, modified)
	return payload, true
}

// markUsed gives the entry the modification time of now when it is older than touchInterval. Marking each use would
// write to the disk for every module of every build; marking at most once an interval keeps the time accurate to within
// it, which is all trimming, which counts days, needs. A failure leaves the time as it was and is no failure of the
// compile.
func (c *Cache) markUsed(file string, modified time.Time) {
	now := c.now()
	if now.Sub(modified) > touchInterval {
		_ = os.Chtimes(file, now, now)
	}
}

// readEntryFile is the whole file and when it was last modified.
func readEntryFile(file string) ([]byte, time.Time, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, time.Time{}, err
	}
	entry := make([]byte, info.Size())
	_, err = io.ReadFull(f, entry)
	return entry, info.ModTime(), err
}

var errBadEntry = errors.New("cache: the entry is not whole")

func openEntry(entry []byte) ([]byte, error) {
	if len(entry) < headerBytes+trailerBytes || string(entry[:len(magic)]) != magic {
		return nil, errBadEntry
	}
	if binary.LittleEndian.Uint32(entry[len(magic):]) != Version {
		return nil, errBadEntry
	}
	length := binary.LittleEndian.Uint64(entry[len(magic)+4:])
	if length != uint64(len(entry)-headerBytes-trailerBytes) {
		return nil, errBadEntry
	}
	payload := entry[headerBytes : len(entry)-trailerBytes]
	if binary.LittleEndian.Uint32(entry[len(entry)-trailerBytes:]) != crc32.Checksum(payload, checksum) {
		return nil, errBadEntry
	}
	return payload, nil
}
