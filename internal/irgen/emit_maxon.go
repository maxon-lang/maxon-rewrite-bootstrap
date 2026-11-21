package main

import (
	"fmt"
	"slices"
	"strings"
)

// self/ir holds the records as an Instr and a Term of fixed fields, read through a getter per operand of each operation
// (binLeft(x)), with the pools in the hand-written IrFunction. A small field is an Index, and a number a Word.

func maxonConst(o op) string {
	if o.Term {
		return "TermKind.term" + o.Name
	}
	return "Opcode.op" + o.Name
}

func maxonCase(o op) string {
	if o.Term {
		return "term" + o.Name
	}
	return "op" + o.Name
}

func maxonEnumCase(e enum, c enumCase) string { return lowerFirst(e.Prefix) + c.Name }

func maxonFlag(name string) string { return "flag" + name }

func maxonEffect(e effect) string { return "effect" + string(e) }

func maxonRecord(o op) string {
	if o.Term {
		return "Term"
	}
	return "Instr"
}

// maxonVar is the parameter a getter, a printer or a visitor method names its record by.
func maxonVar(o op) string {
	if o.Term {
		return "t"
	}
	return "x"
}

func maxonGetter(o op, x operand) string { return lowerFirst(o.Name) + x.Name }

func maxonHas(o op, x operand) string { return lowerFirst(o.Name) + "Has" + x.Name }

// maxonFieldType is the Maxon type of a field of a record.
var maxonFieldType = map[field]string{
	fDst: "Reg", fA: "Reg", fB: "Reg", fC: "Index", fImm: "Word", fSub: "Byte", fFlags: "Byte",
	fValue: "Reg", fThen: "BlockID", fElse: "BlockID", fFirst: "Index", fCount: "Index",
	fArgs: "Index", fThenArgs: "Index", fElseArgs: "Index",
}

// maxonType is what a getter of an operand gives, and what its constructor takes, but for a list or a pool index.
func maxonType(x operand) string {
	switch x.Kind {
	case kReg, kErrorReg:
		return "Reg"
	case kSlot, kInt, kBody:
		return "Count"
	case kWord:
		return "Word"
	case kSymbol:
		return "Rt"
	case kEnum:
		return x.Enum
	case kFlag:
		return "bool"
	case kBlock, kFail:
		return "BlockID"
	case kText:
		return "String"
	case kType:
		return "ast.TypeID"
	case kCallee, kRoutine:
		return "ast.FuncID"
	case kTable:
		return "WitnessTable"
	}
	fail("operand %s has no single Maxon type", x.Name)
	return ""
}

func isList(x operand) bool {
	return x.Kind == kRegs || x.Kind == kMoreRegs || x.Kind == kCases || isEdgeArgs(x)
}

func toCount(expr, what string) string {
	return fmt.Sprintf("try %s as Count otherwise panic(\"ir: %s is out of range\")", expr, what)
}

// toField is a count converted to the type of the field that holds it.
func toField(f field, expr, what string) string {
	return fmt.Sprintf("try %s as %s otherwise panic(\"ir: %s is out of range\")", expr, maxonFieldType[f], what)
}

func toWord(expr, what string) string {
	return fmt.Sprintf("try %s as Word otherwise panic(\"ir: %s is out of range\")", expr, what)
}

// maxonRead is the value of an operand that is no list, read from the record v and, for a pool, the function f.
func maxonRead(o op, x operand) string {
	fld := maxonVar(o) + "." + string(x.Field)
	what := lowerFirst(o.Name) + " " + lowerFirst(x.Name)
	switch x.Kind {
	case kReg, kBlock, kWord:
		return fld
	case kSlot, kInt, kBody:
		return toCount(fld, what)
	case kSymbol:
		return "rtOf(" + toCount(fld, what) + ")"
	case kEnum:
		return lowerFirst(x.Enum) + "Of(" + fld + ")"
	case kFlag:
		return fmt.Sprintf("(%s and %s) != 0", fld, maxonFlag(x.Flag))
	case kFail:
		return fld + " as BlockID"
	case kText:
		return "f.stringAt(" + fld + ")"
	case kType:
		return "f.typeAt(" + fld + ")"
	case kCallee, kRoutine:
		return "f.calleeAt(" + fld + ")"
	case kTable:
		return "f.tableAt(" + fld + ")"
	case kErrorReg:
		return fmt.Sprintf("try %s as Reg otherwise panic(\"ir: %s is out of range\")", fld, what)
	}
	fail("operand %s has no Maxon read", x.Name)
	return ""
}

// maxonPresent is the condition under which an optional operand is there.
func maxonPresent(o op, x operand) string {
	v := maxonVar(o)
	if x.Kind == kRoutine {
		return fmt.Sprintf("%s.%s != noRoutine", v, x.Field)
	}
	return fmt.Sprintf("(%s.flags and %s) != 0", v, maxonFlag(x.Flag))
}

func emitMaxon() string {
	w := &writer{}
	w.p("%s", header)
	emitMaxonTypes(w)
	emitMaxonRecords(w)
	emitMaxonCtors(w)
	emitMaxonGetters(w)
	emitMaxonQueries(w)
	emitMaxonRegisters(w)
	emitMaxonVisitors(w)
	emitMaxonPrinter(w)
	return strings.TrimRight(w.b.String(), "\n") + "\n"
}

func emitMaxonTypes(w *writer) {
	w.p("// Reg is a virtual register, an index into the registers of its function, or noReg.\nexport typealias Reg = int(-1 to 2147483647)\n\n")
	w.p("// BlockID is a basic block, an index into the blocks of its function, or noBlock.\nexport typealias BlockID = int(-1 to 2147483647)\n\n")
	w.p("// Index is a small field of a record: an index into a pool or a count of one.\nexport typealias Index = int(-1 to 2147483647)\n\n")
	w.p("// Word is a 64-bit number an instruction holds.\nexport typealias Word = int(i64.min to i64.max)\n\n")
	w.p("// noReg is the destination of a call whose value nothing reads.\nexport let noReg = -1 as Reg\n\n")
	w.p("// noBlock is the failure block of an operation that cannot fail.\nexport let noBlock = -1 as BlockID\n\n")
	w.p("// noRoutine is the routine of a runtime call that stores none.\nexport let noRoutine = -1 as Index\n\n")

	w.p("// Opcode says what an Instr does, which is also what each of its fields means.\nexport enum Opcode\n\topNone\n")
	for _, o := range instrOps() {
		w.p("\t%s\n", maxonCase(o))
	}
	w.p("end 'Opcode'\n\n// TermKind says how a block ends.\nexport enum TermKind\n\ttermNone\n")
	for _, o := range termOps() {
		w.p("\t%s\n", maxonCase(o))
	}
	w.p("end 'TermKind'\n\n")

	for _, e := range enums {
		name := lowerFirst(e.Name)
		w.p("// %s\nexport enum %s\n", e.Doc, e.Name)
		for _, c := range e.Cases {
			w.p("\t%s\n", maxonEnumCase(e, c))
		}
		w.p("end '%s'\n\n", e.Name)
		w.p("// %sText is how the printer writes a %s.\nexport function %sText(v %s) returns String\n", name, e.Name, name, e.Name)
		for _, c := range e.Cases {
			w.p("\tif v == %s.%s '%s'\n\t\treturn %q\n\tend '%s'\n", e.Name, maxonEnumCase(e, c), maxonEnumCase(e, c), c.Text, maxonEnumCase(e, c))
		}
		w.p("\tpanic(\"ir: a %s with no text\")\nend '%sText'\n\n", e.Name, name)
		w.p("// %sCode is the number an instruction holds a %s as, the one Go's %s has.\nexport function %sCode(v %s) returns Byte\n", name, e.Name, e.Name, name, e.Name)
		for i, c := range e.Cases {
			w.p("\tif v == %s.%s '%s'\n\t\treturn %d as Byte\n\tend '%s'\n", e.Name, maxonEnumCase(e, c), maxonEnumCase(e, c), i, maxonEnumCase(e, c))
		}
		w.p("\tpanic(\"ir: a %s with no number\")\nend '%sCode'\n\n", e.Name, name)
		w.p("// %sOf is the %s an instruction holds as a number.\nexport function %sOf(code Byte) returns %s\n\treturn match code 'code'\n", name, e.Name, name, e.Name)
		for i, c := range e.Cases {
			w.p("\t\t%d gives %s.%s\n", i, e.Name, maxonEnumCase(e, c))
		}
		w.p("\t\tdefault panic(\"ir: a number that is no %s\")\n\tend 'code'\nend '%sOf'\n\n", e.Name, name)
	}

	for i, f := range flags {
		w.p("// %s\nexport let %s = %d as Byte\n\n", strings.Replace(f.Doc, f.Name, maxonFlag(f.Name), 1), maxonFlag(f.Name), 1<<i)
	}

	all := append(append([]effect{}, effects...), effFails)
	for _, e := range all {
		w.p("export let %s = %d as Count\n\n", maxonEffect(e), 1<<effectBit(e))
	}
	for _, term := range []bool{false, true} {
		fn, typ, sel := "opEffects", "Opcode", "op"
		if term {
			fn, typ, sel = "termEffects", "TermKind", "kind"
		}
		w.p("// %s are what an operation does besides reading its operands and writing its result: a sum of effect bits.\nexport function %s(%s %s) returns Count\n", fn, fn, sel, typ)
		for _, o := range ops {
			if o.Term != term {
				continue
			}
			bits := 0
			for _, e := range opEffects(o) {
				bits |= 1 << effectBit(e)
			}
			w.p("\tif %s == %s '%s'\n\t\treturn %d\n\tend '%s'\n", sel, maxonConst(o), maxonCase(o), bits, maxonCase(o))
		}
		w.p("\tpanic(\"ir: an operation of no kind has no effects\")\nend '%s'\n\n", fn)
	}
}

func emitMaxonRecords(w *writer) {
	for _, term := range []bool{false, true} {
		rec, sel, typ, fields := "Instr", "op", "Opcode", instrFields
		doc := "// Instr is one instruction, a record of fixed fields whose meaning its opcode says; what does not fit in it is in\n// the pools of its function, which c and a range of a and b index.\n"
		if term {
			rec, sel, typ, fields = "Term", "kind", "TermKind", termFields
			doc = "// Term is how a block ends, a record of fixed fields like an instruction.\n"
		}
		w.p("%sexport type %s\n\texport var %s as %s\n", doc, rec, sel, typ)
		var inits []string
		inits = append(inits, sel+": "+sel)
		for _, f := range fields {
			w.p("\texport var %s as %s\n", f, maxonFieldType[f])
			inits = append(inits, fmt.Sprintf("%s: 0 as %s", f, maxonFieldType[f]))
		}
		w.p("\n\texport static function make(%s %s) returns %s\n\t\treturn %s{%s}\n\tend 'make'\nend '%s'\n\n", sel, typ, rec, rec, strings.Join(inits, ", "), rec)
		w.p("export typealias %sList = Array with %s\n\n", rec, rec)
	}
}

// maxonParams are the parameters of a constructor for an operand, and maxonSets what it stores.
func maxonParams(x operand) []string {
	n := lowerFirst(x.Name)
	switch x.Kind {
	case kRegs:
		return []string{n + "Start Reg", n + "Count Reg"}
	case kMoreRegs, kMoreEdgeArgs:
		return []string{n + "Count Index"}
	case kEdgeArgs:
		return []string{n + "Start Index", n + "Count Index"}
	case kCases:
		return []string{n + "First Index", n + "Count Index"}
	case kText, kType, kCallee, kRoutine, kTable:
		return []string{n + " Index"}
	}
	return []string{n + " " + maxonType(x)}
}

func maxonSets(w *writer, o op, x operand) {
	n := lowerFirst(x.Name)
	what := lowerFirst(o.Name) + " " + n
	setFlag := func(cond string) {
		w.p("\tif %s '%s'\n\t\tr.flags = r.flags or %s\n\tend '%s'\n", cond, n, maxonFlag(x.Flag), n)
	}
	switch x.Kind {
	case kReg, kBlock, kWord, kText, kType, kCallee, kRoutine, kTable:
		w.p("\tr.%s = %s\n", x.Field, n)
	case kRegs, kEdgeArgs:
		w.p("\tr.%s = %sStart\n\tr.%s = %sCount\n", x.Field, n, x.Count, n)
	case kCases:
		w.p("\tr.%s = %sFirst\n\tr.%s = %sCount\n", x.Field, n, x.Count, n)
	case kMoreRegs, kMoreEdgeArgs:
		w.p("\tr.%s = %sCount\n", x.Field, n)
	case kSlot, kInt, kBody:
		w.p("\tr.%s = %s\n", x.Field, toField(x.Field, n, what))
	case kSymbol:
		w.p("\tr.%s = %s\n", x.Field, toWord("rtCode("+n+")", what))
	case kEnum:
		w.p("\tr.%s = %sCode(%s)\n", x.Field, lowerFirst(x.Enum), n)
	case kFlag:
		setFlag(n)
	case kFail:
		w.p("\tr.%s = %s as Index\n", x.Field, n)
		if x.Flag != "" {
			setFlag(n + " != noBlock")
		}
	case kErrorReg:
		w.p("\tif %s != noReg '%s'\n\t\tr.flags = r.flags or %s\n\t\tr.%s = %s as Word\n\tend '%s'\n", n, n, maxonFlag(x.Flag), x.Field, n, n)
	default:
		fail("operand %s has no Maxon constructor", x.Name)
	}
}

// emitMaxonCtors writes a constructor of each operation. One whose edges can pass arguments has two: newX, whose edges
// pass none, as almost every edge does, and newXWith, which takes them.
func emitMaxonCtors(w *writer) {
	for _, o := range ops {
		emitMaxonCtor(w, o, "new"+o.Name, false)
		if slices.ContainsFunc(o.Operands, isEdgeArgs) {
			emitMaxonCtor(w, o, "new"+o.Name+"With", true)
		}
	}
}

func emitMaxonCtor(w *writer, o op, name string, withArgs bool) {
	var params []string
	if o.Result != noResult {
		params = append(params, "dst Reg")
	}
	for _, x := range o.Operands {
		if isEdgeArgs(x) && !withArgs {
			continue
		}
		params = append(params, maxonParams(x)...)
	}
	doc := o.Doc
	if slices.ContainsFunc(o.Operands, isEdgeArgs) && !withArgs {
		doc += " Its edges pass no arguments; new" + o.Name + "With takes them."
	}
	w.p("// %s\nexport function %s(%s) returns %s\n\tvar r = %s.make(%s)\n", doc, name, strings.Join(params, ", "), maxonRecord(o), maxonRecord(o), maxonConst(o))
	if o.Result != noResult {
		w.p("\tr.dst = dst\n")
	}
	for _, x := range o.Operands {
		if isEdgeArgs(x) && !withArgs {
			continue
		}
		maxonSets(w, o, x)
	}
	w.p("\treturn r\nend '%s'\n\n", name)
}

func emitMaxonGetters(w *writer) {
	for _, o := range ops {
		v, rec := maxonVar(o), maxonRecord(o)
		for i, x := range o.Operands {
			if isList(x) {
				startField, countField := string(x.Field), string(x.Count)
				start := fmt.Sprintf("%s.%s", v, startField)
				count := fmt.Sprintf("%s.%s", v, countField)
				if x.Kind == kMoreRegs || x.Kind == kMoreEdgeArgs {
					prev := listBefore(o, i)
					start = fmt.Sprintf("%sStart(%s) + %sCount(%s)", maxonGetter(o, prev), v, maxonGetter(o, prev), v)
					count = fmt.Sprintf("%s.%s", v, x.Field)
				}
				g := maxonGetter(o, x)
				what := lowerFirst(o.Name) + " " + lowerFirst(x.Name)
				w.p("// %sStart is where the %s of a%s %s start in their pool.\nexport function %sStart(%s %s) returns Count\n", g, lowerFirst(x.Name), article(o.Name), o.Name, g, v, rec)
				if x.Kind == kMoreRegs || x.Kind == kMoreEdgeArgs {
					w.p("\treturn try %s otherwise panic(\"ir: %s is out of range\")\nend '%sStart'\n\n", start, what, g)
				} else {
					w.p("\treturn %s\nend '%sStart'\n\n", toCount(start, what), g)
				}
				w.p("// %sCount is how many %s a%s %s has.\nexport function %sCount(%s %s) returns Count\n\treturn %s\nend '%sCount'\n\n",
					g, lowerFirst(x.Name), article(o.Name), o.Name, g, v, rec, toCount(count, what), g)
				continue
			}
			g := maxonGetter(o, x)
			params := v + " " + rec
			if usesPool(x) {
				params = "f IrFunction, " + params
			}
			if optional(x) {
				w.p("export function %s(%s) returns bool\n\treturn %s\nend '%s'\n\n", maxonHas(o, x), v+" "+rec, maxonPresent(o, x), maxonHas(o, x))
				w.p("export function %s(%s) returns table.Option with %s\n\tif not %s '%s'\n\t\treturn table.Option.none\n\tend '%s'\n\treturn table.Option.some(%s)\nend '%s'\n\n",
					g, params, maxonType(x), maxonHas(o, x)+"("+v+")", "absent", "absent", maxonRead(o, x), g)
				continue
			}
			w.p("export function %s(%s) returns %s\n\treturn %s\nend '%s'\n\n", g, params, maxonType(x), maxonRead(o, x), g)
		}
	}
}

func emitMaxonQueries(w *writer) {
	w.p("// instrDef is the register an instruction writes, if it writes one.\nexport function instrDef(x Instr) returns table.Option with Reg\n")
	for _, o := range instrOps() {
		w.p("\tif x.op == %s '%s'\n", maxonConst(o), maxonCase(o))
		switch o.Result {
		case valueResult:
			w.p("\t\treturn table.Option.some(x.dst)\n")
		case optionalResult:
			w.p("\t\tif x.dst == noReg 'none'\n\t\t\treturn table.Option.none\n\t\tend 'none'\n\t\treturn table.Option.some(x.dst)\n")
		case noResult:
			w.p("\t\treturn table.Option.none\n")
		}
		w.p("\tend '%s'\n", maxonCase(o))
	}
	w.p("\tpanic(\"ir: an instruction has no opcode\")\nend 'instrDef'\n\n")

	w.p("// instrFail is the block an instruction goes to when its check fails, if it has one.\nexport function instrFail(x Instr) returns table.Option with BlockID\n")
	for _, o := range instrOps() {
		w.p("\tif x.op == %s '%s'\n", maxonConst(o), maxonCase(o))
		x, ok := failOperand(o)
		switch {
		case !ok:
			w.p("\t\treturn table.Option.none\n")
		case optional(x):
			w.p("\t\treturn %s(x)\n", maxonGetter(o, x))
		default:
			w.p("\t\treturn table.Option.some(%s(x))\n", maxonGetter(o, x))
		}
		w.p("\tend '%s'\n", maxonCase(o))
	}
	w.p("\tpanic(\"ir: an instruction has no opcode\")\nend 'instrFail'\n\n")
}

func emitMaxonVisitors(w *writer) {
	for _, term := range []bool{false, true} {
		rec, sel, iface, dispatch, v := "Instr", "op", "InstrVisitor", "visitInstr", "x"
		what := "an instruction has no opcode"
		if term {
			rec, sel, iface, dispatch, v = "Term", "kind", "TermVisitor", "visitTerm", "t"
			what = "a block has no terminator"
		}
		w.p("// %s has a method for every kind of %s, so one added to the table fails the build where it is not handled yet.\nexport interface %s\n", iface, strings.ToLower(rec), iface)
		for _, o := range ops {
			if o.Term == term {
				w.p("\tfunction visit%s(mutates self, f IrFunction, %s %s)\n", o.Name, v, rec)
			}
		}
		w.p("end '%s'\n\n", iface)
		w.p("// %s calls the method of the visitor for the %s of %s.\nexport function %s(mutates visitor V, f IrFunction, %s %s) uses V where V implements %s\n", dispatch, sel, v, dispatch, v, rec, iface)
		for _, o := range ops {
			if o.Term != term {
				continue
			}
			w.p("\tif %s.%s == %s '%s'\n\t\tvisitor.visit%s(f, %s: %s)\n\t\treturn\n\tend '%s'\n", v, sel, maxonConst(o), maxonCase(o), o.Name, v, v, maxonCase(o))
		}
		w.p("\tpanic(\"ir: %s\")\nend '%s'\n\n", what, dispatch)
	}
}

func emitMaxonPrinter(w *writer) {
	for _, term := range []bool{false, true} {
		sel, fn, v := "op", "instrText", "x"
		params := "p ast.Project, f IrFunction, x Instr"
		what := "an instruction has no opcode"
		doc := "// instrText is the text the printer gives an instruction, as Go's InstrString does.\n"
		if term {
			sel, fn, v = "kind", "termText", "t"
			params = "f IrFunction, t Term"
			what = "a block has no terminator"
			doc = "// termText is the text the printer gives a terminator, as Go's TermString does.\n"
		}
		w.p("%sexport function %s(%s) returns String\n", doc, fn, params)
		for _, o := range ops {
			if o.Term != term {
				continue
			}
			w.p("\tif %s.%s == %s '%s'\n", v, sel, maxonConst(o), maxonCase(o))
			maxonPrintOp(w, o)
			w.p("\tend '%s'\n", maxonCase(o))
		}
		w.p("\tpanic(\"ir: %s\")\nend '%s'\n\n", what, fn)
	}
}

// maxonPrintOp writes the text of one operation: one interpolated string when nothing in it is conditional, and
// otherwise a string that grows by each optional part that is there.
func maxonPrintOp(w *writer, o op) {
	v := maxonVar(o)
	var pending strings.Builder
	started := false
	flush := func() {
		if started {
			if pending.Len() > 0 {
				w.p("\t\ttext = \"{text}%s\"\n", pending.String())
			}
		} else {
			w.p("\t\tvar text = \"%s\"\n", pending.String())
			started = true
		}
		pending.Reset()
	}
	if o.Result != noResult {
		pending.WriteString("{resultText(" + v + ".dst)}")
	}
	for _, p := range templateParts(o.Text) {
		if p.operand == "" {
			if strings.ContainsAny(p.text, "{}\"\\") {
				fail("%s prints %q, which a Maxon string cannot hold as it is", o.Name, p.text)
			}
			pending.WriteString(p.text)
			continue
		}
		x := operandOf(o, p.operand)
		switch {
		case x.Kind == kFlag:
			flush()
			w.p("\t\tif %s(%s) '%s'\n\t\t\ttext = \"{text}%s\"\n\t\tend '%s'\n", maxonGetter(o, x), v, lowerFirst(x.Name), x.Text, lowerFirst(x.Name))
		case optional(x):
			flush()
			w.p("\t\tif %s(%s) '%s'\n\t\t\ttext = \"{text}%s%s\"\n\t\tend '%s'\n", maxonHas(o, x), v, lowerFirst(x.Name), x.Text, maxonText(o, x, true), lowerFirst(x.Name))
		case isEdgeArgs(x):
			flush()
			w.p("\t\tif %sCount(%s) > 0 '%s'\n\t\t\ttext = \"{text}(%s)\"\n\t\tend '%s'\n", maxonGetter(o, x), v, lowerFirst(x.Name), maxonText(o, x, false), lowerFirst(x.Name))
		default:
			pending.WriteString(maxonText(o, x, false))
		}
	}
	if !started {
		w.p("\t\treturn \"%s\"\n", pending.String())
		return
	}
	flush()
	w.p("\t\treturn text\n")
}

// maxonText is the interpolation that prints an operand, unwrapping an optional one that is there.
func maxonText(o op, x operand, unwrap bool) string {
	v := maxonVar(o)
	get := fmt.Sprintf("%s(%s)", maxonGetter(o, x), v)
	if usesPool(x) {
		get = fmt.Sprintf("%s(f, %s: %s)", maxonGetter(o, x), v, v)
	}
	if unwrap {
		get += ".unwrap()"
	}
	switch x.Kind {
	case kReg, kErrorReg:
		return "%{" + get + "}"
	case kRegs, kMoreRegs, kEdgeArgs, kMoreEdgeArgs:
		g := maxonGetter(o, x)
		return fmt.Sprintf("{f.regsText(%sStart(%s), count: %sCount(%s))}", g, v, g, v)
	case kCases:
		g := maxonGetter(o, x)
		return fmt.Sprintf("{f.casesText(%sStart(%s), count: %sCount(%s))}", g, v, g, v)
	case kSlot:
		return "s{" + get + "}"
	case kInt, kWord:
		return "{" + get + "}"
	case kSymbol:
		return "{rtText(" + get + ")}"
	case kEnum:
		return "{" + lowerFirst(x.Enum) + "Text(" + get + ")}"
	case kBlock, kFail:
		return "b{" + get + "}"
	case kText:
		return "{quote(" + get + ")}"
	case kType:
		return "{ast.typeName(p.types, t: " + get + ")}"
	case kCallee, kRoutine:
		return "{funcSymbol(p, fid: " + get + ")}"
	case kBody:
		return "{f.bodyName(" + get + ")}"
	case kTable:
		return "{" + get + ".symbol}"
	}
	fail("operand %s has no Maxon text", x.Name)
	return ""
}

// emitMaxonRegisters writes what an analysis of registers asks of each operation: the registers it reads, in the order of
// its operands, and the ones it writes.
func emitMaxonRegisters(w *writer) {
	for _, term := range []bool{false, true} {
		fn, v, rec, what := "instrReads", "x", "Instr", "an instruction has no opcode"
		doc := "// instrReads appends the registers an instruction reads to out, in the order of its operands.\n"
		if term {
			fn, v, rec, what = "termReads", "t", "Term", "a block has no terminator"
			doc = "// termReads appends the registers a terminator reads to out, in the order of its operands: its value and the\n// arguments of its edges.\n"
		}
		w.p("%sexport function %s(f IrFunction, %s %s, mutates out RegList)\n", doc, fn, v, rec)
		for _, o := range ops {
			if o.Term != term {
				continue
			}
			w.p("\tif %s.%s == %s '%s'\n", v, recordSelector(o), maxonConst(o), maxonCase(o))
			for _, x := range o.Operands {
				g := maxonGetter(o, x)
				switch x.Kind {
				case kReg:
					w.p("\t\tout.push(%s(%s))\n", g, v)
				case kRegs, kMoreRegs, kEdgeArgs, kMoreEdgeArgs:
					w.p("\t\tf.regsInto(%sStart(%s), count: %sCount(%s), out: out)\n", g, v, g, v)
				case kCases:
					w.p("\t\tf.caseArgsInto(%sStart(%s), count: %sCount(%s), out: out)\n", g, v, g, v)
				}
			}
			w.p("\t\treturn\n\tend '%s'\n", maxonCase(o))
		}
		w.p("\tpanic(\"ir: %s\")\nend '%s'\n\n", what, fn)
	}

	w.p("// instrWrites appends the registers an instruction writes to out: its result, and the error of a call that throws.\nexport function instrWrites(x Instr, mutates out RegList)\n")
	for _, o := range instrOps() {
		w.p("\tif x.op == %s '%s'\n", maxonConst(o), maxonCase(o))
		switch o.Result {
		case valueResult:
			w.p("\t\tout.push(x.dst)\n")
		case optionalResult:
			w.p("\t\tif x.dst != noReg 'value'\n\t\t\tout.push(x.dst)\n\t\tend 'value'\n")
		case noResult:
		}
		for _, x := range o.Operands {
			if x.Kind == kErrorReg {
				w.p("\t\tif %s(x) 'error'\n\t\t\tout.push(%s(x).unwrap())\n\t\tend 'error'\n", maxonHas(o, x), maxonGetter(o, x))
			}
		}
		w.p("\t\treturn\n\tend '%s'\n", maxonCase(o))
	}
	w.p("\tpanic(\"ir: an instruction has no opcode\")\nend 'instrWrites'\n\n")
}

// recordSelector is the field of a record that says which operation it is.
func recordSelector(o op) string {
	if o.Term {
		return "kind"
	}
	return "op"
}
