package x64table

import (
	"fmt"

	"minimaxon/internal/isa"
)

// ShapeOf is the shape of the form or choice of a name, and whether the table has one. A sequence is none: the forms
// of one fix registers, which selection names as copies to and from them.
func ShapeOf(name string) (isa.Shape, bool) {
	for _, f := range forms {
		if f.name == name {
			return isa.Shape{Form: lowerFirst(f.name), Params: f.shapeParams()}, true
		}
	}
	for _, c := range choices {
		if c.name == name {
			ps := formByName(c.alternatives[0]).shapeParams()
			for i := range ps {
				if ps[i].Role == isa.RoleImm {
					ps[i].Type = immediateOf(c.immediateKind()).ty
				}
			}
			return isa.Shape{Form: c.name, Choice: true, Params: ps}, true
		}
	}
	return isa.Shape{}, false
}

// shapeParams are the parameters of a form with their roles, its registers lettered in machine order as machineArgs
// letters them.
func (f form) shapeParams() []isa.ShapeParam {
	letter := 0
	if _, ok := f.defOperand(); ok {
		letter = 1
	}
	useLetter := map[string]string{}
	for i, u := range f.uses() {
		useLetter[u] = machineLetters[letter+i]
	}
	var out []isa.ShapeParam
	for _, o := range f.operands {
		switch o.kind {
		case kindReg:
			switch o.access {
			case read:
				out = append(out, isa.ShapeParam{Name: o.name, Role: isa.RoleUse, Letter: useLetter[o.name], Vector: o.vector})
			case write:
				out = append(out, isa.ShapeParam{Name: o.name, Role: isa.RoleDef, Letter: "a", Vector: o.vector})
			case readWrite:
				out = append(out, isa.ShapeParam{Name: o.name, Role: isa.RoleReuse, Letter: "b", Vector: o.vector})
			default:
				panic(fmt.Sprintf("%s: register %s with no access", f.name, o.name))
			}
		case kindMem:
			out = append(out, isa.ShapeParam{Name: "base", Role: isa.RoleUse, Letter: useLetter["base"]},
				isa.ShapeParam{Name: "disp", Role: isa.RoleDisp, Type: typeDisp})
		case kindMemIndex:
			out = append(out, isa.ShapeParam{Name: "base", Role: isa.RoleUse, Letter: useLetter["base"]},
				isa.ShapeParam{Name: "index", Role: isa.RoleUse, Letter: useLetter["index"]},
				isa.ShapeParam{Name: "disp", Role: isa.RoleDisp, Type: typeDisp})
		case kindRip:
			out = append(out, isa.ShapeParam{Name: o.name, Role: isa.RoleRip})
		case kindRel32:
			out = append(out, isa.ShapeParam{Name: o.name, Role: isa.RoleRel})
		case kindCond:
			out = append(out, isa.ShapeParam{Name: o.name, Role: isa.RoleCond})
		case kindImm8, kindImm32, kindImm32U, kindImm64:
			out = append(out, isa.ShapeParam{Name: o.name, Role: isa.RoleImm, Type: immediateOf(o.kind).ty})
		default:
			panic(fmt.Sprintf("%s: operand kind %d has no role", f.name, o.kind))
		}
	}
	return out
}

// Registers are the names of the general registers, which an instruction selection rule names a fixed register by.
func Registers() []string { return append([]string(nil), registers...) }
