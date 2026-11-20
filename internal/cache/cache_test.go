package cache

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func testKey(name string) Key { return Digest([]byte(name)) }

func entryFile(t *testing.T, c *Cache, key Key) string {
	t.Helper()
	_, file := c.path(key)
	return file
}

func TestAnEntryComesBackAsItWasStored(t *testing.T) {
	c := Open(t.TempDir(), testKey("compiler"))
	key := testKey("module")
	payload := []byte("the code of a module")
	if _, ok := c.Get(key); ok {
		t.Fatal("an entry that was never stored was found")
	}
	if err := c.Put(key, payload); err != nil {
		t.Fatal(err)
	}
	got, ok := c.Get(key)
	if !ok || !bytes.Equal(got, payload) {
		t.Fatalf("got %q, %v", got, ok)
	}
	if c.BytesWritten() != c.BytesRead() || c.BytesWritten() == 0 {
		t.Errorf("wrote %d bytes and read %d", c.BytesWritten(), c.BytesRead())
	}
	if err := c.Put(key, []byte("again")); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Get(key); string(got) != "again" {
		t.Errorf("an entry stored twice reads as %q", got)
	}
}

func TestAnEmptyPayloadIsAnEntry(t *testing.T) {
	c := Open(t.TempDir(), testKey("compiler"))
	key := testKey("empty")
	if err := c.Put(key, nil); err != nil {
		t.Fatal(err)
	}
	if got, ok := c.Get(key); !ok || len(got) != 0 {
		t.Errorf("got %q, %v", got, ok)
	}
}

// An entry that is not whole is a miss, never an error, whichever way it was damaged.
func TestAnEntryThatIsNotWholeIsAMiss(t *testing.T) {
	damage := map[string]func(entry []byte) []byte{
		"cut short":          func(e []byte) []byte { return e[:len(e)-3] },
		"cut in the header":  func(e []byte) []byte { return e[:5] },
		"empty":              func(e []byte) []byte { return nil },
		"a byte of payload":  func(e []byte) []byte { e[headerBytes] ^= 0x40; return e },
		"a byte of checksum": func(e []byte) []byte { e[len(e)-1] ^= 1; return e },
		"longer than it says": func(e []byte) []byte {
			return append(e, 0)
		},
		"another version": func(e []byte) []byte {
			binary.LittleEndian.PutUint32(e[len(magic):], Version+1)
			return e
		},
		"another file": func(e []byte) []byte { e[0] = 'X'; return e },
		"a length that is not the file's": func(e []byte) []byte {
			binary.LittleEndian.PutUint64(e[len(magic)+4:], 1<<40)
			return e
		},
	}
	for name, damaged := range damage {
		c := Open(t.TempDir(), testKey("compiler"))
		key := testKey("module")
		if err := c.Put(key, []byte("some payload that is long enough")); err != nil {
			t.Fatal(err)
		}
		file := entryFile(t, c, key)
		entry, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, damaged(entry), 0o644); err != nil {
			t.Fatal(err)
		}
		if got, ok := c.Get(key); ok {
			t.Errorf("%s: found %q", name, got)
		}
	}
}

// Writers that race on one key leave a whole entry, and no stray file stays behind.
func TestWritersThatRaceLeaveOneWholeEntry(t *testing.T) {
	dir := t.TempDir()
	c := Open(dir, testKey("compiler"))
	key := testKey("module")
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			payload := bytes.Repeat([]byte{byte('a' + i)}, 1<<16)
			for range 20 {
				_ = c.Put(key, payload)
			}
		})
		wg.Go(func() {
			for range 50 {
				if got, ok := c.Get(key); ok && (len(got) != 1<<16 || bytes.Count(got, got[:1]) != len(got)) {
					t.Error("a reader saw an entry that is a mixture of two")
				}
			}
		})
	}
	wg.Wait()
	got, ok := c.Get(key)
	if !ok || len(got) != 1<<16 {
		t.Fatalf("no whole entry: %d bytes, %v", len(got), ok)
	}
	var files []string
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if len(files) != 1 {
		t.Errorf("the directory holds %v, want the one entry", files)
	}
}

func TestPutIntoAPlaceThatIsNotADirectoryFails(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Open(file, testKey("compiler")).Put(testKey("module"), []byte("x")); err == nil {
		t.Error("an entry was stored under a file")
	}
}

func TestDirFollowsTheEnvironmentVariable(t *testing.T) {
	if dir, ok := Dir("off"); ok || dir != "" {
		t.Errorf("off gave %q, %v", dir, ok)
	}
	if dir, ok := Dir(`C:\somewhere`); !ok || dir != `C:\somewhere` {
		t.Errorf("a directory gave %q, %v", dir, ok)
	}
	dir, ok := Dir("")
	if base, err := os.UserCacheDir(); err == nil {
		if !ok || dir != filepath.Join(base, "minimaxon", "cache") {
			t.Errorf("the default is %q, %v", dir, ok)
		}
	} else if ok {
		t.Errorf("there is no user cache directory, yet the default is %q", dir)
	}
}

func TestAKeyHoldsEverythingThatWasWrittenToIt(t *testing.T) {
	key := func(write func(h *Hasher)) Key {
		h := NewHasher()
		write(h)
		return h.Sum()
	}
	base := key(func(h *Hasher) { h.String("a"); h.Int(1); h.Bool(true); h.Key(testKey("k")) })
	if again := key(func(h *Hasher) { h.String("a"); h.Int(1); h.Bool(true); h.Key(testKey("k")) }); again != base {
		t.Error("the same values gave two keys")
	}
	for name, other := range map[string]Key{
		"a string":  key(func(h *Hasher) { h.String("b"); h.Int(1); h.Bool(true); h.Key(testKey("k")) }),
		"an int":    key(func(h *Hasher) { h.String("a"); h.Int(2); h.Bool(true); h.Key(testKey("k")) }),
		"a bool":    key(func(h *Hasher) { h.String("a"); h.Int(1); h.Bool(false); h.Key(testKey("k")) }),
		"a key":     key(func(h *Hasher) { h.String("a"); h.Int(1); h.Bool(true); h.Key(testKey("j")) }),
		"the order": key(func(h *Hasher) { h.Int(1); h.String("a"); h.Bool(true); h.Key(testKey("k")) }),
	} {
		if other == base {
			t.Errorf("a different %s gave the same key", name)
		}
	}
	// Two strings are not their concatenation.
	if key(func(h *Hasher) { h.String("ab"); h.String("c") }) == key(func(h *Hasher) { h.String("a"); h.String("bc") }) {
		t.Error("where one string ends and the next begins is not in the key")
	}
}

func TestTheCompilerIsHashedOnce(t *testing.T) {
	a, err := Executable()
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := Executable(); a != b || a == (Key{}) {
		t.Error("the hash of the executable is not stable")
	}
	if len(a.String()) != 64 {
		t.Errorf("the key reads as %q", a)
	}
}

func TestTheWireFormatRoundTrips(t *testing.T) {
	var e Encoder
	e.Uvarint(0)
	e.Uvarint(1<<63 + 5)
	e.Int(300)
	e.Bool(true)
	e.Bool(false)
	e.Text("héllo")
	e.Blob([]byte{1, 2, 3})
	e.Text("")
	d := NewDecoder(e.Bytes)
	if d.Uvarint() != 0 || d.Uvarint() != 1<<63+5 || d.Int(1000) != 300 || !d.Bool() || d.Bool() ||
		d.Text() != "héllo" || !bytes.Equal(d.Blob(), []byte{1, 2, 3}) || d.Text() != "" {
		t.Fatal("a value did not come back")
	}
	if err := d.Done(); err != nil {
		t.Fatal(err)
	}
}

func TestTheWireFormatRefusesWhatIsNotValid(t *testing.T) {
	var e Encoder
	e.Text("some text")
	e.Int(7)
	whole := e.Bytes
	for cut := range len(whole) {
		d := NewDecoder(whole[:cut])
		d.Text()
		d.Int(10)
		if d.Err() == nil {
			t.Errorf("an encoding cut to %d of %d bytes read without an error", cut, len(whole))
		}
	}
	d := NewDecoder(append(append([]byte{}, whole...), 0))
	d.Text()
	d.Int(10)
	if d.Done() == nil {
		t.Error("bytes after the last value were accepted")
	}
	d = NewDecoder(whole)
	d.Text()
	if d.Int(3) != 0 || d.Err() == nil {
		t.Error("a number above the allowed one was accepted")
	}
	// A length that runs past the end asks for nothing.
	d = NewDecoder([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0x0F, 'x'})
	if len(d.Blob()) != 0 || d.Err() == nil {
		t.Error("a length past the end was accepted")
	}
	d = NewDecoder([]byte{2})
	d.Bool()
	if d.Err() == nil {
		t.Error("a bool of 2 was accepted")
	}
	if d.Uvarint() != 0 || d.Text() != "" {
		t.Error("a read after a failure did not give a zero value")
	}
}
