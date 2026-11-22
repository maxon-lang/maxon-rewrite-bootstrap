// Package target holds the tables of the targets the backend writes code for: each one's register file and calling
// conventions. The x64 instruction table (internal/x64gen/x64table) takes its registers from here, and internal/targetgen
// writes the Maxon declarations the backend reads the conventions from.
package target

import (
	"fmt"
	"slices"
)

// Class is the register file a register belongs to.
type Class int

const (
	General Class = iota // integer and address registers
	Vector               // floating-point and vector registers
)

// Register is one register of a target's register file.
type Register struct {
	Name     string
	Encoding int // the number its instructions encode it by
	Class    Class
}

// Convention is how a call passes its arguments and results, and what it keeps.
type Convention struct {
	// Doc says what the convention is for, in the generated file.
	Doc string
	// Prefix starts the names of the convention's declarations, after the target's.
	Prefix string
	// Args are the registers of the integer arguments in order; the arguments past them go on the stack.
	Args []string
	// Result is the register of the result.
	Result string
	// Error is the register of the error flag of a function that throws, or "" for a convention with none.
	Error string
	// CalleeSaved are the registers a call keeps, and CallerSaved the ones it may leave holding anything; together
	// they are the registers that are not reserved.
	CalleeSaved, CallerSaved []string
	// ShadowBytes is the stack the caller keeps below the stack arguments for the callee to write.
	ShadowBytes int
}

// Target is one target's register file and calling conventions.
type Target struct {
	// Name starts the name of every declaration the generator writes for the target.
	Name string
	// Registers is the register file in its numbering: the general registers by encoding, then the vector ones.
	Registers []Register
	// Reserved are the registers that hold the stack and the frame, which no value is given.
	Reserved []string
	// StackAlignment is the alignment of the stack pointer at a call of the host convention.
	StackAlignment int
	// Internal is the convention between the compiler's own functions, Host the one of the system's libraries.
	Internal, Host Convention
}

// Names are the names of a target's registers of a class, in the order of the register file.
func (t Target) Names(c Class) []string {
	var names []string
	for _, r := range t.Registers {
		if r.Class == c {
			names = append(names, r.Name)
		}
	}
	return names
}

// Number is a register's number in the register file.
func (t Target) Number(name string) (int, error) {
	for i, r := range t.Registers {
		if r.Name == name {
			return i, nil
		}
	}
	return 0, fmt.Errorf("%s: %s is no register", t.Name, name)
}

// Mask is the set of the named registers, a bit per number in the register file.
func (t Target) Mask(names []string) (uint64, error) {
	var m uint64
	for _, n := range names {
		i, err := t.Number(n)
		if err != nil {
			return 0, err
		}
		m |= 1 << i
	}
	return m, nil
}

// Check refuses a table that contradicts itself: a register file out of its numbering, a convention whose saved
// registers do not split the registers that are not reserved, or whose argument, result or error registers are not
// registers it may leave holding anything.
func (t Target) Check() error {
	for c, names := range [][]string{t.Names(General), t.Names(Vector)} {
		for i, n := range names {
			r := t.Registers[slices.IndexFunc(t.Registers, func(r Register) bool { return r.Name == n })]
			if r.Encoding != i {
				return fmt.Errorf("%s: %s of class %d is encoded %d but is register %d of its class", t.Name, n, c, r.Encoding, i)
			}
		}
	}
	if len(t.Names(General))+len(t.Names(Vector)) != len(t.Registers) {
		return fmt.Errorf("%s: a register of no class", t.Name)
	}
	for i, r := range t.Registers {
		if i > 0 && t.Registers[i-1].Class > r.Class {
			return fmt.Errorf("%s: %s comes after a register of a later class", t.Name, r.Name)
		}
	}
	if len(t.Registers) > 64 {
		return fmt.Errorf("%s: a register file past the 64 bits of a mask", t.Name)
	}
	reserved, err := t.Mask(t.Reserved)
	if err != nil {
		return err
	}
	all := uint64(1)<<len(t.Registers) - 1
	for _, c := range []Convention{t.Internal, t.Host} {
		kept, err := t.Mask(c.CalleeSaved)
		if err != nil {
			return err
		}
		lost, err := t.Mask(c.CallerSaved)
		if err != nil {
			return err
		}
		if kept&lost != 0 || kept&reserved != 0 || lost&reserved != 0 || kept|lost|reserved != all {
			return fmt.Errorf("%s %s: the callee-saved, caller-saved and reserved registers do not split the register file", t.Name, c.Prefix)
		}
		moved := slices.Clone(c.Args)
		moved = append(moved, c.Result)
		if c.Error != "" {
			moved = append(moved, c.Error)
		}
		for _, n := range moved {
			m, err := t.Mask([]string{n})
			if err != nil {
				return err
			}
			if m&lost == 0 {
				return fmt.Errorf("%s %s: %s passes a value but is not caller-saved", t.Name, c.Prefix, n)
			}
		}
		if c.Error != "" && (c.Error == c.Result || slices.Contains(c.Args, c.Error)) {
			return fmt.Errorf("%s %s: the error flag shares a register with a value", t.Name, c.Prefix)
		}
	}
	return nil
}

func regs(class Class, names ...string) []Register {
	out := make([]Register, len(names))
	for i, n := range names {
		out[i] = Register{Name: n, Encoding: i, Class: class}
	}
	return out
}

// X64Windows is x64 on Windows. The internal convention is maxon's, the same on every x64 system; the host one is Win64.
var X64Windows = Target{
	Name: "x64",
	Registers: append(
		regs(General, "rax", "rcx", "rdx", "rbx", "rsp", "rbp", "rsi", "rdi", "r8", "r9", "r10", "r11", "r12", "r13", "r14", "r15"),
		regs(Vector, "xmm0", "xmm1", "xmm2", "xmm3", "xmm4", "xmm5", "xmm6", "xmm7", "xmm8", "xmm9", "xmm10", "xmm11", "xmm12", "xmm13", "xmm14", "xmm15")...),
	Reserved:       []string{"rsp", "rbp"},
	StackAlignment: 16,
	Internal: Convention{
		Doc:         "the convention between the compiler's own functions, maxon's",
		Prefix:      "Internal",
		Args:        []string{"rcx", "rdx", "rax", "r9", "rsi", "rdi"},
		Result:      "r8",
		Error:       "r10",
		CalleeSaved: []string{"rbx", "r12", "r13", "r14", "r15", "xmm6", "xmm7", "xmm8", "xmm9", "xmm10", "xmm11", "xmm12", "xmm13", "xmm14", "xmm15"},
		CallerSaved: []string{"rax", "rcx", "rdx", "rsi", "rdi", "r8", "r9", "r10", "r11", "xmm0", "xmm1", "xmm2", "xmm3", "xmm4", "xmm5"},
	},
	Host: Convention{
		Doc:         "Win64, the convention of a DLL's functions",
		Prefix:      "Host",
		Args:        []string{"rcx", "rdx", "r8", "r9"},
		Result:      "rax",
		CalleeSaved: []string{"rbx", "rsi", "rdi", "r12", "r13", "r14", "r15", "xmm6", "xmm7", "xmm8", "xmm9", "xmm10", "xmm11", "xmm12", "xmm13", "xmm14", "xmm15"},
		CallerSaved: []string{"rax", "rcx", "rdx", "r8", "r9", "r10", "r11", "xmm0", "xmm1", "xmm2", "xmm3", "xmm4", "xmm5"},
		ShadowBytes: 32,
	},
}

// Targets are every target's table, in the order their declarations are written.
var Targets = []Target{X64Windows}
