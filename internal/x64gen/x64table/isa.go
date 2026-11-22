package x64table

import (
	"minimaxon/internal/isa"
	"minimaxon/internal/targetgen/target"
)

// Table is the x64 instruction set as the generators of instruction selection and of the machine IR's tables see it.
var Table isa.ISA = instructionSet{}

type instructionSet struct{}

func (instructionSet) Name() string { return target.X64.Name }

// formBase is the FormID of the first form: x64's forms are the first FormIDs, so a form's FormID is the rawValue of
// its X64Form.
const formBase = 0

func (instructionSet) FormBase() int { return formBase }

func (instructionSet) Check() (err error) {
	_, err = Generate()
	return err
}

func (instructionSet) Forms() []isa.Form {
	out := make([]isa.Form, len(forms))
	for i, f := range forms {
		_, defines := f.defOperand()
		out[i] = isa.Form{
			Name: lowerFirst(f.name), Operands: f.machineOperands(), VectorOperands: f.vectorMask(),
			Defines: defines, Reuses: f.reuses(), Commutes: f.commutes, Call: f.call, Return: f.ret,
			PatchField: hasPatchField(f), Rematerializable: rematerializable(f),
			SetsFlags: f.setsFlags, ReadsFlags: f.readsFlags, ReadsMemory: f.readsMemory, WritesMemory: f.writesMemory,
			FixedUses: maskOf(f.fixedUses), FixedDefs: maskOf(f.fixedDefs), Clobbers: maskOf(f.clobbers),
		}
	}
	return out
}

func (instructionSet) Named() isa.Named {
	return isa.Named{
		Jump: "jmpRel32", Trap: "ud2", Kernel: "syscall",
		XorGeneral: "xorRegReg", XorVector: "xorpsRegReg",
		ToVector: "movqXmmReg", ToGeneral: "movqRegXmm",
	}
}

func (instructionSet) ShapeOf(name string) (isa.Shape, bool) { return ShapeOf(name) }

func (instructionSet) Registers() []string { return Registers() }
