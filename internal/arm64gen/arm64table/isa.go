package arm64table

import (
	"fmt"

	"minimaxon/internal/isa"
	"minimaxon/internal/targetgen/target"
)

// Table is the arm64 instruction set as the generators of instruction selection and of the machine IR's tables see it.
var Table isa.ISA = instructionSet{}

type instructionSet struct{}

func (instructionSet) Name() string { return target.Arm64.Name }

// formBase is the FormID of the first form: arm64's forms are the range of FormIDs after x64's.
const formBase = isa.FormRange

func (instructionSet) FormBase() int { return formBase }

func (instructionSet) Check() (err error) {
	_, err = Generate()
	return err
}

func (instructionSet) Forms() []isa.Form {
	ms := machineForms()
	out := make([]isa.Form, len(ms))
	for i, f := range ms {
		_, defines := f.defOperand()
		out[i] = isa.Form{
			Name: lowerFirst(f.name), Operands: f.machineOperands(), VectorOperands: f.vectorMask(),
			Defines: defines, Reuses: f.reuses(), Call: f.call, Return: f.ret,
			PatchField: hasPatchField(f), Rematerializable: rematerializable(f),
			SetsFlags: f.setsFlags, ReadsFlags: f.readsFlags, ReadsMemory: f.readsMemory, WritesMemory: f.writesMemory,
			FixedUses: maskOf(f.fixedUses), FixedDefs: maskOf(f.fixedDefs), Clobbers: maskOf(f.clobbers),
		}
	}
	return out
}

func (instructionSet) Named() isa.Named {
	return isa.Named{
		Jump: "b", Trap: "brk", Kernel: "svc",
		XorGeneral: "eorRegReg", XorVector: "eorVec",
		ToVector: "fmovDX", ToGeneral: "fmovXD",
	}
}

func (instructionSet) ShapeOf(name string) (isa.Shape, bool) { return ShapeOf(name) }

func (instructionSet) Registers() []string { return Registers() }

// ShapeOf is the shape of the form of a name, and whether the table has one. A direct form is none: no machine
// instruction holds it.
func ShapeOf(name string) (isa.Shape, bool) {
	for _, f := range machineForms() {
		if f.name == name {
			return isa.Shape{Form: lowerFirst(f.name), Params: f.shapeParams()}, true
		}
	}
	return isa.Shape{}, false
}

// shapeParams are the parameters of a form with their roles, its registers lettered in machine order as machineArgs
// letters them, and its immediates in the fields machineArgs reads them from: an offset and a second immediate are the
// machine instruction's aux, as a displacement is.
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
	immediates := 0
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
		case kindDisp:
			out = append(out, isa.ShapeParam{Name: o.name, Role: isa.RoleDisp, Type: typeDisp})
		case kindImm, kindMask, kindWord:
			role := isa.RoleImm
			if immediates > 0 {
				role = isa.RoleDisp
			}
			immediates++
			out = append(out, isa.ShapeParam{Name: o.name, Role: role, Type: o.params()[0].ty})
		case kindCond:
			out = append(out, isa.ShapeParam{Name: o.name, Role: isa.RoleCond})
		case kindRel:
			out = append(out, isa.ShapeParam{Name: o.name, Role: isa.RoleRel})
		case kindAddress, kindPageOffset:
			out = append(out, isa.ShapeParam{Name: o.name, Role: isa.RoleRip})
		default:
			panic(fmt.Sprintf("%s: operand kind %d has no role", f.name, o.kind))
		}
	}
	return out
}

// Registers are the names of the general registers, which an instruction selection rule names a fixed register by.
func Registers() []string { return append([]string(nil), registers...) }
