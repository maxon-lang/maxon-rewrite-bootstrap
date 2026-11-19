package interp

import (
	"fmt"
	"minimaxon/internal/ir"
	"strings"
)

// ViolationKind says which rule of the memory model a program broke. A program that the checker accepted must break
// none, so a violation means the checker, or lowering, has a hole.
type ViolationKind int

const (
	// UseAfterFree is a read, a write or a borrow of a value that was freed.
	UseAfterFree ViolationKind = iota
	// DoubleFree is freeing a value that was freed already.
	DoubleFree
	// Leak is a value that is still live when the program ends, or that its owner lost without freeing.
	Leak
	// MovedUse is using a value through an owner that gave it away, or never had one.
	MovedUse
	// Exclusivity is overlapping access: two arguments that reach one place, or a change to a place that a borrow
	// still holds.
	Exclusivity
	// StaleBorrow is using the address of an element after the structure that holds it changed.
	StaleBorrow
	// Ownership is a value with two owners, or one that is freed or returned while another owner holds it.
	Ownership
	// Race is a thread that touches a value another thread owns, or the value of a cown while no behaviour of its own holds
	// the cown: a data race.
	Race
	// Immutability is a change to a value that is part of a frozen value.
	Immutability
	// Count is a frozen value or a cown whose count of owners is wrong: freed while a place still holds it, or lowered
	// past 0.
	Count
)

var violationNames = [...]string{"use after free", "double free", "leak", "use of a moved value", "exclusivity", "stale borrow", "ownership", "data race", "change to a frozen value", "reference count"}

func (k ViolationKind) String() string { return violationNames[k] }

// Violation is a rule broken at one place in the IR.
type Violation struct {
	Kind     ViolationKind
	Function string
	Block    int
	// Instr is the index of the instruction in its block, or -1 for the terminator.
	Instr   int
	Code    string
	Message string
}

func (v Violation) String() string {
	where := fmt.Sprintf("function %s, block b%d, instruction #%d", v.Function, v.Block, v.Instr)
	switch {
	case v.Function == "":
		where = "the end of the program"
	case v.Instr < 0:
		where = fmt.Sprintf("function %s, block b%d, terminator", v.Function, v.Block)
	}
	text := ""
	if v.Code != "" {
		text = " (`" + v.Code + "`)"
	}
	return fmt.Sprintf("memory-model violation: %s in %s%s: %s", v.Kind, where, text, v.Message)
}

// violationSignal carries a violation up to Run.
type violationSignal struct{ v Violation }

func (vm *vm) violate(kind ViolationKind, format string, args ...any) {
	panic(&violationSignal{vm.here(kind, fmt.Sprintf(format, args...))})
}

func (vm *vm) here(kind ViolationKind, message string) Violation {
	f := vm.top
	if f == nil {
		return Violation{Kind: kind, Message: message}
	}
	v := Violation{Kind: kind, Function: f.fn.Symbol(), Block: f.fn.Blocks[f.block].ID, Instr: f.pc, Message: message}
	b := f.fn.Blocks[f.block]
	switch {
	case f.pc >= 0 && f.pc < len(b.Instrs):
		v.Code = ir.InstrString(b.Instrs[f.pc])
	case f.pc < 0 && b.Term != nil:
		v.Code = ir.TermString(b.Term)
	}
	return v
}

func describeLeak(o *object) string {
	owner := "no owner holds it"
	if o.owner != nil {
		owner = "owned by " + o.owner.String()
	}
	return fmt.Sprintf("%s is still live at the end of the program, and %s", o, owner)
}

// Report is the violations of a run, one to a line, for a message.
func (r *Result) Report() string {
	parts := make([]string, len(r.Violations))
	for i, v := range r.Violations {
		parts[i] = v.String()
	}
	return strings.Join(parts, "\n")
}
