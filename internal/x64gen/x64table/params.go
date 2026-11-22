package x64table

import "fmt"

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
	// RoleRip is a [rip + disp] field, which refers to a symbol, data or an import.
	RoleRip
	// RoleRel is a rel32 field, which refers to an edge or a symbol.
	RoleRel
	RoleCond
	RoleImm
)

// ShapeParam is one parameter of an encoder: its name, its role, the field of the machine instruction that holds a
// register (a, b or c), and the Maxon type of a displacement or an immediate.
type ShapeParam struct {
	Name   string
	Role   Role
	Letter string
	Type   string
}

// Shape is what an instruction selection rule may name: a form, or a choice, whose form a generated choose function
// picks by the value of its immediate. Its parameters are in the encoder's order, which is the order a rule gives them.
type Shape struct {
	// Form is the X64Form case of a form, or for a choice the name its choose function is named after.
	Form   string
	Choice bool
	Params []ShapeParam
}

// ShapeOf is the shape of the form or choice of a name, and whether the table has one. A sequence is none: the forms
// of one fix registers, which selection names as copies to and from them.
func ShapeOf(name string) (Shape, bool) {
	for _, f := range forms {
		if f.name == name {
			return Shape{Form: lowerFirst(f.name), Params: f.shapeParams()}, true
		}
	}
	for _, c := range choices {
		if c.name == name {
			ps := formByName(c.alternatives[0]).shapeParams()
			for i := range ps {
				if ps[i].Role == RoleImm {
					ps[i].Type = immediateOf(c.immediateKind()).ty
				}
			}
			return Shape{Form: c.name, Choice: true, Params: ps}, true
		}
	}
	return Shape{}, false
}

// shapeParams are the parameters of a form with their roles, its registers lettered in machine order as machineArgs
// letters them.
func (f form) shapeParams() []ShapeParam {
	letter := 0
	if _, ok := f.defOperand(); ok {
		letter = 1
	}
	useLetter := map[string]string{}
	for i, u := range f.uses() {
		useLetter[u] = machineLetters[letter+i]
	}
	var out []ShapeParam
	for _, o := range f.operands {
		switch o.kind {
		case kindReg:
			switch o.access {
			case read:
				out = append(out, ShapeParam{Name: o.name, Role: RoleUse, Letter: useLetter[o.name]})
			case write:
				out = append(out, ShapeParam{Name: o.name, Role: RoleDef, Letter: "a"})
			case readWrite:
				out = append(out, ShapeParam{Name: o.name, Role: RoleReuse, Letter: "b"})
			default:
				panic(fmt.Sprintf("%s: register %s with no access", f.name, o.name))
			}
		case kindMem:
			out = append(out, ShapeParam{Name: "base", Role: RoleUse, Letter: useLetter["base"]},
				ShapeParam{Name: "disp", Role: RoleDisp, Type: typeDisp})
		case kindRip:
			out = append(out, ShapeParam{Name: o.name, Role: RoleRip})
		case kindRel32:
			out = append(out, ShapeParam{Name: o.name, Role: RoleRel})
		case kindCond:
			out = append(out, ShapeParam{Name: o.name, Role: RoleCond})
		case kindImm8, kindImm32, kindImm32U, kindImm64:
			out = append(out, ShapeParam{Name: o.name, Role: RoleImm, Type: immediateOf(o.kind).ty})
		default:
			panic(fmt.Sprintf("%s: operand kind %d has no role", f.name, o.kind))
		}
	}
	return out
}

// Registers are the names of the general registers, which an instruction selection rule names a fixed register by.
func Registers() []string { return append([]string(nil), registers...) }
