// Package isa is an instruction set as the generators see it: the forms that instruction selection rules name and the
// roles of their encoders' parameters (internal/rulegen), and what the machine IR asks of each form (internal/targetgen,
// which numbers the forms of every instruction set in one space of FormIDs). Each instruction set's table package
// implements ISA (internal/x64gen/x64table), and internal/machines lists them.
package isa

import "strings"

// Role is what instruction selection gives a parameter of a form's encoder: a register of the machine instruction, or
// one of its other fields.
type Role int

const (
	// RoleDef is a register the form writes and does not read: the machine instruction's definition.
	RoleDef Role = iota
	// RoleReuse is a register a two-address form reads and writes: the definition reuses it.
	RoleReuse
	// RoleUse is a register the form reads, the base of a memory operand among them.
	RoleUse
	// RoleDisp is the displacement of a memory operand.
	RoleDisp
	// RoleRip is a field relative to the instruction that refers to a symbol, data or an import.
	RoleRip
	// RoleRel is a branch's or a call's relative field, which refers to an edge or a symbol.
	RoleRel
	RoleCond
	RoleImm
)

// ShapeParam is one parameter of an encoder: its name, its role, the field of the machine instruction that holds a
// register (a, b or c), whether that register is a vector one, and the Maxon type of a displacement or an immediate.
type ShapeParam struct {
	Name   string
	Role   Role
	Letter string
	Vector bool
	Type   string
}

// Shape is what an instruction selection rule may name: a form, or a choice, whose form a generated choose function
// picks by the value of its immediate. Its parameters are in the encoder's order, which is the order a rule gives them.
type Shape struct {
	// Form is the case of the instruction set's form enum, or for a choice the name its choose function is named after.
	Form   string
	Choice bool
	Params []ShapeParam
}

// Conditions are the conditions a machine instruction tests, the cases of the machine IR's MCond in order: what the
// flags a comparison set say, which each instruction set encodes its own way.
var Conditions = []string{
	"overflow", "noOverflow", "below", "aboveEqual", "equal", "notEqual", "belowEqual", "above",
	"sign", "noSign", "parity", "noParity", "less", "greaterEqual", "lessEqual", "greater",
}

// Form is what the machine IR asks of one form: its register operands in machine order (the definition, then the
// operand a two-address form reuses, then the others it reads), the registers it reads and writes that no operand
// names, as masks over the target's register file, and its effects.
type Form struct {
	// Name is the case of the instruction set's form enum.
	Name string
	// Operands is how many register operands it has; VectorOperands has bit i set where operand i is a vector register.
	Operands, VectorOperands int
	// Defines says that operand a is a definition, Reuses that the definition must be given the register of operand b,
	// and Commutes that a two-address form's two uses may be swapped.
	Defines, Reuses, Commutes bool
	// Call clobbers what its calling convention does not keep; Return reads the convention's result registers.
	Call, Return bool
	// PatchField says that its last field is one a reference patches.
	PatchField bool
	// Rematerializable says that it makes its result from its immediate alone.
	Rematerializable                                 bool
	SetsFlags, ReadsFlags, ReadsMemory, WritesMemory bool
	FixedUses, FixedDefs, Clobbers                   uint64
}

// Named are the forms of an instruction set that the target-neutral passes write or recognize themselves.
type Named struct {
	// Jump goes along an edge unconditionally, which a block that the allocator splits off ends with.
	Jump string
	// Trap stops the program, which ends a block that goes nowhere.
	Trap string
	// Kernel is the call of the system call convention.
	Kernel string
	// XorGeneral and XorVector are the exclusive or of two registers of each class into the first: three exchange two
	// registers, and one whose uses both name the definition's register sets it to 0 whatever it held.
	XorGeneral, XorVector string
	// ToVector and ToGeneral move the bits of a word from a register of the other class.
	ToVector, ToGeneral string
}

// FormRange is how many FormIDs each instruction set has, from its FormBase: the ranges never meet, so that one table
// indexed by FormID holds what the machine IR asks of the forms of every instruction set.
const FormRange = 256

// ISA is one instruction set.
type ISA interface {
	// Name is the case of ast.TargetArch, which starts the names of the instruction set's declarations ("x64": X64Form,
	// X64Register) and names its rules (x64.rules).
	Name() string
	// FormBase is the FormID of its first form, a multiple of FormRange.
	FormBase() int
	// Check refuses a table that contradicts itself, which nothing else is asked of then.
	Check() error
	// Forms are its forms, in the order of its form enum.
	Forms() []Form
	Named() Named
	// ShapeOf is the shape of the form or choice of a name, and whether the instruction set has one.
	ShapeOf(name string) (Shape, bool)
	// Registers are the names of its general registers, which an instruction selection rule names a fixed register by.
	Registers() []string
}

// TypeName is the name of a Maxon type of an instruction set's: its name, capitalized, before what it is ("X64" +
// "Form").
func TypeName(i ISA, what string) string {
	n := i.Name()
	return strings.ToUpper(n[:1]) + n[1:] + what
}
