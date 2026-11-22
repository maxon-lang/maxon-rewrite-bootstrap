package arm64table

import (
	"fmt"
	"slices"
	"strings"

	"minimaxon/internal/targetgen/target"
)

// The allocation columns of the table become what the machine IR (self/backend/mir.maxon) asks of a form. A machine
// instruction names a form's register operands in one order: the register it defines first, if it defines one, then
// the register it reads and writes (which the definition reuses), then the other registers it reads, in the order of
// the encoder's parameters.

// maxMachineOperands is how many register operands a machine instruction has room for (its a, b and c).
const maxMachineOperands = 3

// machineLetters are the fields of a machine instruction that hold its register operands, in order.
var machineLetters = []string{"a", "b", "c"}

// registerNumber is a register's number in the allocator's register file: the general registers, then the d ones.
func registerNumber(name string) int {
	n, err := target.Arm64.Number(name)
	if err != nil {
		fail("%v", err)
	}
	return n
}

func maskOf(names []string) uint64 {
	var m uint64
	for _, n := range names {
		m |= 1 << registerNumber(n)
	}
	return m
}

// defOperand is the register operand a form writes, which checkAlloc has made sure there is at most one of.
func (f form) defOperand() (operand, bool) {
	for _, o := range f.operands {
		if o.kind == kindReg && (o.access == write || o.access == readWrite) {
			return o, true
		}
	}
	return operand{}, false
}

func (f form) reuses() bool {
	o, ok := f.defOperand()
	return ok && o.access == readWrite
}

// uses are the names of the registers a form reads in machine order: the reused one first, then the others in operand
// order.
func (f form) uses() []string {
	var out []string
	if d, ok := f.defOperand(); ok && d.access == readWrite {
		out = append(out, d.name)
	}
	for _, o := range f.operands {
		if o.kind == kindReg && o.access == read {
			out = append(out, o.name)
		}
	}
	return out
}

// vectorMask has bit i set where register operand i in machine order is a d register.
func (f form) vectorMask() int {
	var vector []bool
	if d, ok := f.defOperand(); ok {
		vector = append(vector, d.vector)
	}
	for _, name := range f.uses() {
		at := slices.IndexFunc(f.operands, func(o operand) bool { return o.kind == kindReg && o.name == name })
		vector = append(vector, f.operands[at].vector)
	}
	m := 0
	for i, v := range vector {
		if v {
			m |= 1 << i
		}
	}
	return m
}

func (f form) machineOperands() int {
	n := len(f.uses())
	if _, ok := f.defOperand(); ok {
		n++
	}
	return n
}

// checkAlloc refuses a form of the machine IR whose allocation columns contradict its operands or one another.
func checkAlloc(f form) {
	defs := 0
	for _, o := range f.operands {
		if o.access == write || o.access == readWrite {
			defs++
		}
	}
	if defs > 1 {
		fail("%s writes two register operands, and a machine instruction defines one: make it direct", f.name)
	}
	if f.machineOperands() > maxMachineOperands {
		fail("%s has more register operands than a machine instruction holds: make it direct", f.name)
	}
	if f.call && f.ret {
		fail("%s is both a call and a return", f.name)
	}
	reserved := target.Arm64.Reserved
	for _, set := range [][]string{f.fixedUses, f.fixedDefs, f.clobbers} {
		seen := map[string]bool{}
		for _, r := range set {
			registerNumber(r)
			if slices.Contains(reserved, r) {
				fail("%s fixes %s, which is reserved", f.name, r)
			}
			if seen[r] {
				fail("%s names %s twice in one column", f.name, r)
			}
			seen[r] = true
		}
	}
	for _, r := range f.fixedDefs {
		if slices.Contains(f.clobbers, r) {
			fail("%s both gives a result in %s and clobbers it", f.name, r)
		}
	}
}

func hasPatchField(f form) bool {
	for _, o := range f.operands {
		if isPatched(o.kind) {
			return true
		}
	}
	return false
}

// rematerializable is whether a form makes its result from its immediates alone: it reads no register, no memory and
// no flag, writes nothing but its result and refers to nothing, so the allocator may write it again before a use
// instead of keeping its result in a register or a spill slot.
func rematerializable(f form) bool {
	d, ok := f.defOperand()
	return ok && d.access == write && len(f.uses()) == 0 && f.effects == noEffects &&
		len(f.fixedUses) == 0 && len(f.fixedDefs) == 0 && len(f.clobbers) == 0 && !f.call && !f.ret && !hasPatchField(f)
}

// machineArgs are the arguments that encodeArm64MachineForm gives a form's encoder, in the order of its parameters:
// the registers by their letters, an offset from aux, the first immediate from imm and a second one from aux, and 0
// for a field a reference patches.
func machineArgs(f form) []string {
	letter := 0
	if _, ok := f.defOperand(); ok {
		letter = 1
	}
	useLetter := map[string]string{}
	for i, u := range f.uses() {
		useLetter[u] = machineLetters[letter+i]
	}
	var args []string
	immediates := 0
	for _, o := range f.operands {
		switch o.kind {
		case kindReg:
			if o.access == read {
				args = append(args, useLetter[o.name])
			} else {
				args = append(args, "a")
			}
		case kindDisp:
			args = append(args, "aux")
		case kindMask:
			args = append(args, "imm")
			immediates++
		case kindImm:
			field := "imm"
			if immediates > 0 {
				field = "aux"
			}
			immediates++
			args = append(args, fmt.Sprintf("try %s as %s otherwise panic(\"backend: the immediate of %s does not fit it\")", field, immType(o.bits), f.name))
		case kindCond:
			args = append(args, "cond")
		case kindRel, kindAddress, kindPageOffset:
			args = append(args, "0")
		default:
			panic(fmt.Sprintf("operand kind %d has no machine argument", o.kind))
		}
	}
	return args
}

// emitAlloc writes what the backend's arm64 code asks of the forms and the registers by number: the FormID of a form,
// the general register of a number, and the encoder of a machine instruction.
func emitAlloc(b *strings.Builder) {
	fmt.Fprintf(b, "// arm64FormBase is the FormID of arm64's first form: its forms are a range of FormIDs of their own.\nlet arm64FormBase = %d\n\n", formBase)
	b.WriteString("// arm64FormID is the FormID of a form.\n")
	b.WriteString("export function arm64FormID(form Arm64Form) returns FormID\n\treturn try (arm64FormBase + form.rawValue) as FormID otherwise panic(\"backend: an arm64 form past the FormIDs\")\nend 'arm64FormID'\n\n")
	b.WriteString("// arm64RegisterOf is the general register with a number of the register file.\n")
	b.WriteString("export function arm64RegisterOf(number RegNum) returns Arm64Register\n\treturn match number 'number'\n")
	for i, r := range registers {
		fmt.Fprintf(b, "\t\t%d gives Arm64Register.%s\n", i, r)
	}
	b.WriteString("\t\tdefault panic(\"backend: a register number that is no general register\")\n\tend 'number'\nend 'arm64RegisterOf'\n\n")
	b.WriteString("// encodeArm64MachineForm encodes a form from the fields of a machine instruction: a defines, a reused register is\n")
	b.WriteString("// the same as a, and the registers a form does not have are not read.\n")
	b.WriteString("export function encodeArm64MachineForm(mutates code ByteArray, form FormID, a Arm64Register, b Arm64Register, c Arm64Register, aux Imm32, imm FullRangeInt, cond MCond)\n")
	b.WriteString("\tmatch form 'form'\n")
	for i, f := range machineForms() {
		fmt.Fprintf(b, "\t\t%d then %s\n", formBase+i, call(f.name, f.params(), machineArgs(f)))
	}
	b.WriteString("\t\tdefault then panic(\"backend: a machine instruction of a form of no arm64 form\")\n")
	b.WriteString("\tend 'form'\nend 'encodeArm64MachineForm'\n")
}
