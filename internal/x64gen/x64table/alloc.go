package x64table

import (
	"fmt"
	"slices"
	"strings"
)

// The allocation columns of the table become what the machine IR (self/backend/mir.maxon) asks of a form. A machine
// instruction names a form's register operands in one order: the register it defines first, if it defines one, then
// the register a two-address form reads and writes (which the definition reuses), then the other registers it reads,
// in the order of the encoder's parameters.

// maxMachineOperands is how many register operands a machine instruction has room for (its a, b and c).
const maxMachineOperands = 3

// reservedRegisters are the stack and frame pointers, which no value is given and no form fixes.
var reservedRegisters = []string{"rsp", "rbp"}

// machineLetters are the fields of a machine instruction that hold its register operands, in order.
var machineLetters = []string{"a", "b", "c"}

// registerNumber is a register's number in the allocator's register file: the general registers, then the xmm ones.
func registerNumber(name string) int {
	if i := slices.Index(registers, name); i >= 0 {
		return i
	}
	if i := slices.Index(vectorRegisters, name); i >= 0 {
		return len(registers) + i
	}
	fail("%s is no register", name)
	return 0
}

func maskOf(names []string) uint64 {
	var m uint64
	for _, n := range names {
		m |= 1 << registerNumber(n)
	}
	return m
}

// defOperand is the register operand a form writes, which check has made sure there is at most one of.
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
// order, a memory operand's base among them.
func (f form) uses() []string {
	var out []string
	if d, ok := f.defOperand(); ok && d.access == readWrite {
		out = append(out, d.name)
	}
	for _, o := range f.operands {
		switch {
		case o.kind == kindReg && o.access == read:
			out = append(out, o.name)
		case o.kind == kindMem:
			out = append(out, "base")
		}
	}
	return out
}

func (f form) machineOperands() int {
	n := len(f.uses())
	if _, ok := f.defOperand(); ok {
		n++
	}
	return n
}

// checkAlloc refuses a form whose allocation columns contradict its operands or one another.
func checkAlloc(f form) {
	defs := 0
	for _, o := range f.operands {
		switch {
		case o.kind == kindReg && o.access == accessNone:
			fail("%s: register %s says nothing of whether it is read or written", f.name, o.name)
		case o.kind != kindReg && o.access != accessNone:
			fail("%s: %s is no register and has an access", f.name, o.name)
		case o.access == write || o.access == readWrite:
			defs++
		}
	}
	if defs > 1 {
		fail("%s writes two register operands, and a machine instruction defines one", f.name)
	}
	if f.machineOperands() > maxMachineOperands {
		fail("%s has more register operands than a machine instruction holds", f.name)
	}
	if f.call && f.ret {
		fail("%s is both a call and a return", f.name)
	}
	for _, set := range [][]string{f.fixedUses, f.fixedDefs, f.clobbers} {
		seen := map[string]bool{}
		for _, r := range set {
			registerNumber(r)
			if slices.Contains(reservedRegisters, r) {
				fail("%s fixes %s, which holds the stack or the frame", f.name, r)
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
	_, hasRip := f.withKind(kindRip)
	_, hasRel := f.withKind(kindRel32)
	if hasRip || hasRel {
		for _, o := range f.operands {
			if isImmediate(o.kind) {
				fail("%s has an immediate after the field a reference patches", f.name)
			}
		}
	}
}

func hasPatchField(f form) bool {
	_, rip := f.withKind(kindRip)
	_, rel := f.withKind(kindRel32)
	return rip || rel
}

// allocPredicates are the allocation columns that are a yes or no of each form.
var allocPredicates = []struct {
	name string
	of   func(form) bool
}{
	{"definesOperand", func(f form) bool { _, ok := f.defOperand(); return ok }},
	{"reusesOperand", form.reuses},
	{"isCall", func(f form) bool { return f.call }},
	{"isReturn", func(f form) bool { return f.ret }},
	{"hasPatchField", hasPatchField},
	{"rematerializable", rematerializable},
}

// rematerializable is whether a form makes its result from its immediate alone: it reads no register, no memory and no
// flag, writes nothing but its result and refers to nothing, so the allocator may write it again before a use instead
// of keeping its result in a register or a spill slot.
func rematerializable(f form) bool {
	d, ok := f.defOperand()
	return ok && d.access == write && len(f.uses()) == 0 && f.effects == noEffects &&
		len(f.fixedUses) == 0 && len(f.fixedDefs) == 0 && len(f.clobbers) == 0 && !f.call && !f.ret && !hasPatchField(f)
}

// allocMasks are the allocation columns that are a set of registers.
var allocMasks = []struct {
	name, doc string
	of        func(form) []string
}{
	{"fixedUses", "the registers a form reads that no operand names", func(f form) []string { return f.fixedUses }},
	{"fixedDefs", "the registers a form writes a result to that no operand names", func(f form) []string { return f.fixedDefs }},
	{"clobbers", "the registers a form leaves holding nothing, besides a call's", func(f form) []string { return f.clobbers }},
}

func emitAlloc(b *strings.Builder) {
	fmt.Fprintf(b, "// x64FirstVector is the number of xmm0 in the register file, after the general registers.\nexport let x64FirstVector = %d\n\n", len(registers))
	fmt.Fprintf(b, "export let x64RegisterFileSize = %d\n\n", len(registers)+len(vectorRegisters))
	fmt.Fprintf(b, "export let x64FormCount = %d\n\n", len(forms))
	b.WriteString("// x64RegisterOf is the general register with a number of the register file.\n")
	b.WriteString("export function x64RegisterOf(number RegNum) returns X64Register\n\treturn match number 'number'\n")
	for i, r := range registers {
		fmt.Fprintf(b, "\t\t%d gives X64Register.%s\n", i, r)
	}
	b.WriteString("\t\tdefault panic(\"backend: a register number that is no general register\")\n\tend 'number'\nend 'x64RegisterOf'\n\n")
	b.WriteString("// x64FormOf is the form with a number, its rawValue.\n")
	b.WriteString("export function x64FormOf(number Count) returns X64Form\n\treturn match number 'number'\n")
	for i, f := range forms {
		fmt.Fprintf(b, "\t\t%d gives X64Form.%s\n", i, lowerFirst(f.name))
	}
	b.WriteString("\t\tdefault panic(\"backend: a number that is no x64 form\")\n\tend 'number'\nend 'x64FormOf'\n\n")
	b.WriteString("// machineOperandCount is how many register operands a machine instruction of a form has.\n")
	emitFormTable(b, "machineOperandCount", "Count", form.machineOperands)
	for _, p := range allocPredicates {
		emitEffect(b, p.name, p.of)
	}
	for _, m := range allocMasks {
		emitGrouped(b, m.name, "RegMask", m.doc, func(f form) string {
			return fmt.Sprintf("0x%X", maskOf(m.of(f)))
		})
	}
	for _, c := range choices {
		emitChooser(b, c)
	}
	emitMachineEncoder(b)
}

// emitGrouped writes a match over every form that gives what of says, with the forms of one value in one arm.
func emitGrouped(b *strings.Builder, name, ty, doc string, of func(form) string) {
	var values []string
	byValue := map[string][]string{}
	for _, f := range forms {
		v := of(f)
		if _, ok := byValue[v]; !ok {
			values = append(values, v)
		}
		byValue[v] = append(byValue[v], lowerFirst(f.name))
	}
	fmt.Fprintf(b, "// %s is %s.\n", name, doc)
	fmt.Fprintf(b, "export function %s(form X64Form) returns %s\n", name, ty)
	b.WriteString("\treturn match form 'form'\n")
	for _, v := range values {
		fmt.Fprintf(b, "\t\t%s gives %s\n", strings.Join(byValue[v], " or\n\t\t"), v)
	}
	b.WriteString("\tend 'form'\n")
	fmt.Fprintf(b, "end '%s'\n\n", name)
}

// emitChooser writes the form a choice encodes for an immediate, for instruction selection to put in a machine
// instruction.
func emitChooser(b *strings.Builder, c choice) {
	wide := immediateOf(c.immediateKind()).ty
	fmt.Fprintf(b, "// choose%s is the first of %s whose immediate holds the value.\n", c.name, strings.Join(c.alternatives, ", "))
	fmt.Fprintf(b, "export function choose%s(imm %s) returns X64Form\n", c.name, wide)
	last := len(c.alternatives) - 1
	for i, name := range c.alternatives {
		f := formByName(name)
		if i == last {
			fmt.Fprintf(b, "\treturn X64Form.%s\n", lowerFirst(f.name))
			continue
		}
		label := lowerFirst(name)
		fmt.Fprintf(b, "\tif %s '%s'\n\t\treturn X64Form.%s\n\tend '%s'\n", guard(immediateOf(immediateKindOf(f))), label, label, label)
	}
	fmt.Fprintf(b, "end 'choose%s'\n\n", c.name)
}

// machineArgs are the arguments that encodeMachineForm gives a form's encoder, in the order of its parameters.
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
	for _, o := range f.operands {
		switch o.kind {
		case kindReg:
			if o.access == read {
				args = append(args, useLetter[o.name])
			} else {
				args = append(args, "a")
			}
		case kindMem:
			args = append(args, useLetter["base"], "aux")
		case kindRip, kindRel32:
			args = append(args, "0")
		case kindCond:
			args = append(args, "cond")
		case kindImm64:
			args = append(args, "imm")
		case kindImm8, kindImm32, kindImm32U:
			args = append(args, fmt.Sprintf("try imm as %s otherwise panic(\"backend: the immediate of %s does not fit it\")", immediateOf(o.kind).ty, f.name))
		default:
			panic(fmt.Sprintf("operand kind %d has no machine argument", o.kind))
		}
	}
	return args
}

// emitMachineEncoder writes the encoder of a machine instruction whose registers are allocated: the form's encoder,
// given the registers in machine order, the displacement of a memory operand, the immediate and the condition. A field
// that a reference patches is written as 0.
func emitMachineEncoder(b *strings.Builder) {
	b.WriteString("// encodeMachineForm encodes a form from the fields of a machine instruction: a defines, a reused register is\n")
	b.WriteString("// the same as a, and the registers a form does not have are not read.\n")
	b.WriteString("export function encodeMachineForm(mutates code ByteArray, form X64Form, a X64Register, b X64Register, c X64Register, aux Imm32, imm FullRangeInt, cond X64CondCode)\n")
	b.WriteString("\tmatch form 'form'\n")
	for _, f := range forms {
		fmt.Fprintf(b, "\t\t%s then %s\n", lowerFirst(f.name), call(f.name, f.params(), machineArgs(f)))
	}
	b.WriteString("\tend 'form'\nend 'encodeMachineForm'\n")
}
