package interp

import (
	"fmt"
	"minimaxon/internal/diag"
	"minimaxon/internal/ir"
	"strconv"
)

// The interpreter keeps the heap the way the rules describe it: every value has one owner, which is the place that
// holds it (a slot, a field, an element), and the places that only borrow a value hold the same pointer without
// owning it. A value in flight, such as the result of a call, has no owner yet.

type objectKind uint8

const (
	kindBox objectKind = iota
	kindString
	kindArray
	kindMap
	kindList
	kindPool
	// kindFrozen and kindCown are boxes with many owners: a count of them, not one owner, decides when the box is freed.
	kindFrozen
	kindCown
)

var kindNames = [...]string{"record", "string", "array", "map", "list", "pool", "frozen value", "cown"}

// site is a place in a function, for a report: where a value was made or freed.
type site struct {
	fn    *ir.IrFunction
	block int
	pc    int
}

func (s site) String() string {
	if s.fn == nil {
		return "the start of the program"
	}
	if s.pc < 0 {
		return fmt.Sprintf("%s b%d, terminator", s.fn.Symbol(), s.block)
	}
	return fmt.Sprintf("%s b%d #%d", s.fn.Symbol(), s.block, s.pc)
}

// value is what a register or a place holds: a scalar, a pointer to an object, or an address. A managed register that
// holds no pointer holds a value that was moved out or never assigned.
type value struct {
	n   int64
	ref *object
	adr *address
}

// cell is a place that holds a value: a frame slot, a word of a box, an element of a primitive.
type cell struct {
	v      value
	holder *object
	frame  *frame
	index  int
}

func (c *cell) String() string {
	if c.holder != nil {
		return fmt.Sprintf("a part of %s", c.holder)
	}
	if c.frame != nil {
		return fmt.Sprintf("slot s%d of %s", c.index, c.frame.fn.Symbol())
	}
	return "a place"
}

// address is a borrow of a cell that a callee may change. It is valid until the structure that holds the cell
// changes, as the primitives' contract says, and until the frame that holds it returns.
type address struct {
	cell    *cell
	version uint64
}

type object struct {
	id        int
	kind      objectKind
	immortal  bool
	freed     bool
	owner     *cell
	version   uint64
	allocated site
	freedAt   site
	text      string
	fields    []cell
	elems     []*cell
	table     *table
	mapped    *mapData

	// count is how many owners a frozen value or a cown has: the references that places hold or that are in flight, and for
	// a cown the behaviours that have been scheduled on it and have not finished. holders are the places that hold one.
	count   int
	holders map[*cell]bool
	// queue is the behaviours waiting for a cown, in the order they were scheduled, the first of them being the one that
	// runs once it has every cown it names; running is the behaviour that has the cown now.
	queue   []*behaviour
	running *behaviour
}

// shared is whether the object is a frozen value or a cown, which owners count instead of owning alone.
func (o *object) shared() bool { return o.kind == kindFrozen || o.kind == kindCown }

// heldBy is whether a place is an owner of the object: the one owner of an ordinary value, or one of the many of a shared
// one.
func (o *object) heldBy(c *cell) bool {
	if o.shared() {
		return o.holders[c]
	}
	return o.owner == c
}

func (o *object) String() string {
	if o.kind == kindString {
		return fmt.Sprintf("string#%d %q (made in %s)", o.id, o.text, o.allocated)
	}
	return fmt.Sprintf("%s#%d (made in %s)", kindNames[o.kind], o.id, o.allocated)
}

// freeLabel names a freed value in the order of frees: a string by its text, anything else by its kind.
func (o *object) freeLabel() string {
	if o.kind == kindString {
		return "string " + strconv.Quote(o.text)
	}
	return kindNames[o.kind]
}

// within is whether o is x or is owned, through any number of owners, by x.
func within(o, x *object) bool {
	for o != nil {
		if o == x {
			return true
		}
		if o.owner == nil {
			return false
		}
		o = o.owner.holder
	}
	return false
}

// region is storage that a borrow, or a change, concerns: a cell, with whatever it holds, or an object.
type region struct {
	cell *cell
	obj  *object
}

func (r region) String() string {
	if r.cell != nil {
		return r.cell.String()
	}
	return r.obj.String()
}

// inside is whether a cell is part of x.
func inside(c *cell, x *object) bool { return c.holder != nil && x != nil && within(c.holder, x) }

func held(c *cell) *object { return c.v.ref }

// overlaps is whether two regions can reach the same storage: one is, or lies inside, the other.
func overlaps(a, b region) bool {
	switch {
	case a.cell != nil && b.cell != nil:
		return a.cell == b.cell || inside(a.cell, held(b.cell)) || inside(b.cell, held(a.cell))
	case a.cell != nil:
		return inside(a.cell, b.obj) || (held(a.cell) != nil && within(b.obj, held(a.cell)))
	case b.cell != nil:
		return overlaps(b, a)
	}
	return within(a.obj, b.obj) || within(b.obj, a.obj)
}

type frame struct {
	fn     *ir.IrFunction
	thread *thread
	params []value
	slots  []cell
	regs   []value
	block  int
	pc     int
	active bool
}

func (f *frame) site() site {
	if f == nil {
		return site{}
	}
	return site{fn: f.fn, block: f.block, pc: f.pc}
}

// mapKey is a key as the map compares it: a word, or the text of a string.
type mapKey struct {
	n      int64
	text   string
	isText bool
}

type mapEntry struct {
	key cell
	val cell
}

type mapData struct {
	entries []*mapEntry
	index   map[mapKey]*mapEntry
}

// table holds the elements of a list or a pool in slots that name them by a handle with a generation, so a slot that
// held an element and was reused does not answer to the handle of the old one. A list also keeps its slots in order.
type table struct {
	gens  []uint32
	live  []bool
	cells []*cell
	free  []int
	order []int
}

const (
	generationShift = 32
	handleSlotMask  = 1<<generationShift - 1
)

func handleOf(gen uint32, slot int) int64 {
	return int64(gen)<<generationShift | int64(slot+1)
}

// lookup is the slot a handle names, if its element is still there.
func (t *table) lookup(h int64) (int, bool) {
	slot := int(h&handleSlotMask) - 1
	gen := uint32(uint64(h) >> generationShift)
	if h <= 0 || slot < 0 || slot >= len(t.gens) || !t.live[slot] || t.gens[slot] != gen {
		return 0, false
	}
	return slot, true
}

// acquire puts a cell in a free slot, the one released last first, and gives the handle of it.
func (t *table) acquire(c *cell) (int, int64) {
	var slot int
	if n := len(t.free); n > 0 {
		slot = t.free[n-1]
		t.free = t.free[:n-1]
		t.cells[slot] = c
	} else {
		slot = len(t.gens)
		t.gens = append(t.gens, 0)
		t.live = append(t.live, false)
		t.cells = append(t.cells, c)
	}
	t.gens[slot]++
	t.live[slot] = true
	return slot, handleOf(t.gens[slot], slot)
}

func (t *table) release(slot int) {
	t.live[slot] = false
	t.cells[slot] = nil
	t.free = append(t.free, slot)
}

func (t *table) count() int {
	n := 0
	for _, l := range t.live {
		if l {
			n++
		}
	}
	return n
}

// position is where a slot sits in the order of a list.
func (t *table) position(slot int) int {
	for i, s := range t.order {
		if s == slot {
			return i
		}
	}
	panic(diag.Internal("interp: a live slot is missing from the order of its list"))
}

func (t *table) unlink(slot int) {
	i := t.position(slot)
	t.order = append(t.order[:i], t.order[i+1:]...)
}
