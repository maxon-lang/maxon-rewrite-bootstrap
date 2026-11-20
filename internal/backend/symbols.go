package backend

import (
	"hash/maphash"
	"math/bits"
	"sync/atomic"

	"minimaxon/internal/diag"
)

// symbolID names a definition in the link, counted from 1 over every unit's definitions in unit order, so it does not
// depend on timing.
type symbolID int32

// noSymbol is what a lookup of a symbol that nothing defines gives.
const noSymbol symbolID = 0

const (
	// symbolTableSlotsPerSymbol keeps the table at most half full, so a probe ends in a step or two.
	symbolTableSlotsPerSymbol = 2
	symbolTableMinimumSlots   = 16
	symbolTagShift            = 32
)

// symbolTable finds the definition of a symbol. Definitions come from all units at once, so a slot is claimed with a
// compare and swap and the table needs no lock; it is only read once every definition is in. A slot holds an id and the
// high half of the symbol's hash, so a probe compares symbols only where the hashes agree. The seed is random for each
// process, so where a symbol lands varies, but what a lookup finds does not.
//
// The instance of a generic function may be defined more than once, by every module that needs it, and the slot ends
// up holding the lowest id, which is the first unit's definition however the goroutines were scheduled: a definition
// that finds a lower id there gives way, and one that finds a higher id takes the slot over. Every definition has an id
// of its own, so the others stay as numbers that no lookup returns.
type symbolTable struct {
	seed  maphash.Seed
	slots []atomic.Uint64
	mask  uint64
	// symbols is the symbol of each id, which a probe compares against; index 0 is no symbol.
	symbols []string
}

func newSymbolTable(count int) *symbolTable {
	size := max(symbolTableMinimumSlots, 1<<bits.Len(uint(count*symbolTableSlotsPerSymbol-1)))
	return &symbolTable{
		seed:    maphash.MakeSeed(),
		slots:   make([]atomic.Uint64, size),
		mask:    uint64(size - 1),
		symbols: make([]string, count+1),
	}
}

func (t *symbolTable) hash(symbol string) uint64 { return maphash.String(t.seed, symbol) }

func symbolSlotEntry(hash uint64, id symbolID) uint64 {
	return hash>>symbolTagShift<<symbolTagShift | uint64(uint32(id))
}

// matches is whether a slot holds the symbol, given the hash it was looked up by.
func (t *symbolTable) matches(entry, hash uint64, symbol string) (id symbolID, ok bool) {
	if entry>>symbolTagShift != hash>>symbolTagShift {
		return noSymbol, false
	}
	id = symbolID(uint32(entry))
	return id, t.symbols[id] == symbol
}

// define records the symbol of an id; each id is defined once, by one goroutine. A symbol that another id defines too is
// an internal error unless it is repeatable: an instance of a generic function, which every unit that needs it defines.
func (t *symbolTable) define(id symbolID, symbol string, repeatable bool) {
	t.symbols[id] = symbol
	hash := t.hash(symbol)
	entry := symbolSlotEntry(hash, id)
	for slot := hash & t.mask; ; slot = (slot + 1) & t.mask {
		held := t.slots[slot].Load()
		for {
			if held == 0 {
				if t.slots[slot].CompareAndSwap(0, entry) {
					return
				}
				held = t.slots[slot].Load()
				continue
			}
			heldID, same := t.matches(held, hash, symbol)
			if !same {
				break
			}
			if !repeatable {
				panic(diag.Internal("symbol %s is defined twice", symbol))
			}
			if heldID < id || t.slots[slot].CompareAndSwap(held, entry) {
				return
			}
			held = t.slots[slot].Load()
		}
	}
}

// lookup is the id of the definition of a symbol, or noSymbol.
func (t *symbolTable) lookup(symbol string) symbolID {
	hash := t.hash(symbol)
	for slot := hash & t.mask; ; slot = (slot + 1) & t.mask {
		held := t.slots[slot].Load()
		if held == 0 {
			return noSymbol
		}
		if id, same := t.matches(held, hash, symbol); same {
			return id
		}
	}
}
