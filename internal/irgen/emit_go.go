package main

import (
	"fmt"
	"strings"
)

// writer collects generated source.
type writer struct{ b strings.Builder }

func (w *writer) p(format string, args ...any) { fmt.Fprintf(&w.b, format, args...) }

func goConst(o op) string {
	if o.Term {
		return "Term" + o.Name
	}
	return "Op" + o.Name
}

func goView(o op) string {
	if o.Term {
		return o.Name + "Term"
	}
	return o.Name + "Instr"
}

func goRecord(o op) string {
	if o.Term {
		return "Term"
	}
	return "Instr"
}

// goCtor is the constructor of an operation: exported when the operation indexes no pool, and otherwise unexported,
// for the builder method that appends to the pool first.
func goCtor(o op) string {
	if opUsesPool(o) {
		return "new" + o.Name
	}
	return o.Name
}

func goFlag(name string) string { return "Flag" + name }

func goEffect(e effect) string { return "Effect" + string(e) }

// goFieldType is the Go type of a field of a record.
var goFieldType = map[field]string{
	fDst: "V", fA: "V", fB: "V", fC: "int32", fImm: "int64", fSub: "uint8", fFlags: "InstrFlags",
	fValue: "V", fThen: "BlockID", fElse: "BlockID", fFirst: "int32", fCount: "int32",
}

// goType is the type an operand's accessor gives.
func goType(x operand) string {
	switch x.Kind {
	case kReg, kErrorReg:
		return "V"
	case kRegs, kMoreRegs:
		return "[]V"
	case kSlot, kInt, kBehaviour:
		return "int"
	case kWord:
		return "int64"
	case kSymbol:
		return "Rt"
	case kEnum:
		return x.Enum
	case kFlag:
		return "bool"
	case kBlock, kFail:
		return "BlockID"
	case kText:
		return "string"
	case kType:
		return "ast.Ty"
	case kCallee, kRoutine:
		return "*ast.FuncDecl"
	case kCases:
		return "[]SwitchCase"
	}
	fail("operand %s has no Go type", x.Name)
	return ""
}

// goRead is the value of an operand, read from the view x and, for a pool, the function f.
func goRead(o op, i int) string {
	x := o.Operands[i]
	fld := "x." + string(x.Field)
	switch x.Kind {
	case kReg, kBlock, kWord:
		return fld
	case kRegs:
		return fmt.Sprintf("f.Args[x.%s : x.%s+x.%s]", x.Field, x.Field, x.Count)
	case kMoreRegs:
		prev := o.Operands[i-1]
		start := fmt.Sprintf("x.%s+x.%s", prev.Field, prev.Count)
		return fmt.Sprintf("f.Args[%s : %s+V(%s)]", start, start, fld)
	case kSlot, kInt:
		return "int(" + fld + ")"
	case kSymbol:
		return "Rt(" + fld + ")"
	case kEnum:
		return x.Enum + "(" + fld + ")"
	case kFlag:
		return fld + "&" + goFlag(x.Flag) + " != 0"
	case kFail:
		return "BlockID(" + fld + ")"
	case kText:
		return "f.Strings[" + fld + "]"
	case kType:
		return "f.Types[" + fld + "]"
	case kCallee, kRoutine:
		return "f.Callees[" + fld + "]"
	case kErrorReg:
		return "V(" + fld + ")"
	case kBehaviour:
		return "f.Behaviours[" + fld + "]"
	case kCases:
		return fmt.Sprintf("f.Cases[x.%s : x.%s+x.%s]", x.Field, x.Field, x.Count)
	}
	fail("operand %s has no Go read", x.Name)
	return ""
}

// goMissing is the condition under which an optional operand is not there, and goAbsent what its accessor gives then.
func goMissing(x operand) string {
	if x.Kind == kRoutine {
		return "x." + string(x.Field) + " == NoRoutine"
	}
	return "x.flags&" + goFlag(x.Flag) + " == 0"
}

func goAbsent(x operand) string {
	switch x.Kind {
	case kFail:
		return "NoBlock"
	case kErrorReg:
		return "NoReg"
	case kRoutine:
		return "nil"
	}
	fail("operand %s is never absent", x.Name)
	return ""
}

func goAccessorType(x operand) string {
	if x.Kind == kBehaviour {
		return "*IrFunction"
	}
	return goType(x)
}

// goParams are the constructor's parameters for an operand.
func goParams(x operand) string {
	n := lowerFirst(x.Name)
	switch x.Kind {
	case kRegs:
		return n + "Start, " + n + "Count V"
	case kMoreRegs:
		return n + "Count int32"
	case kText, kType, kCallee, kRoutine:
		return n + " int32"
	case kCases:
		return n + "First, " + n + "Count int32"
	case kBehaviour:
		return n + " int"
	}
	return n + " " + goType(x)
}

// goSets writes what a constructor stores for an operand.
func goSets(w *writer, x operand) {
	n := lowerFirst(x.Name)
	switch x.Kind {
	case kReg, kBlock, kWord, kText, kType, kCallee, kRoutine:
		w.p("\tr.%s = %s\n", x.Field, n)
	case kRegs:
		w.p("\tr.%s, r.%s = %sStart, %sCount\n", x.Field, x.Count, n, n)
	case kCases:
		w.p("\tr.%s, r.%s = %sFirst, %sCount\n", x.Field, x.Count, n, n)
	case kMoreRegs:
		w.p("\tr.%s = %sCount\n", x.Field, n)
	case kSlot, kInt, kSymbol, kBehaviour:
		w.p("\tr.%s = int64(%s)\n", x.Field, n)
	case kEnum:
		w.p("\tr.%s = uint8(%s)\n", x.Field, n)
	case kFlag:
		w.p("\tif %s {\n\t\tr.flags |= %s\n\t}\n", n, goFlag(x.Flag))
	case kFail:
		w.p("\tr.%s = int32(%s)\n", x.Field, n)
		if x.Flag != "" {
			w.p("\tif %s != NoBlock {\n\t\tr.flags |= %s\n\t}\n", n, goFlag(x.Flag))
		}
	case kErrorReg:
		w.p("\tif %s != NoReg {\n\t\tr.flags |= %s\n\t\tr.%s = int64(%s)\n\t}\n", n, goFlag(x.Flag), x.Field, n)
	default:
		fail("operand %s has no Go constructor", x.Name)
	}
}

func emitGo() string {
	w := &writer{}
	w.p("%spackage ir\n\nimport (\n\t\"minimaxon/internal/ast\"\n\t\"minimaxon/internal/cache\"\n\t\"minimaxon/internal/diag\"\n\t\"strconv\"\n)\n", header)
	emitGoEnums(w)
	emitGoRecords(w)
	emitGoQueries(w)
	emitGoCtors(w)
	emitGoViews(w)
	emitGoVisitors(w, false)
	emitGoVisitors(w, true)
	emitGoPrinter(w)
	emitGoCodec(w)
	return w.b.String()
}

func emitGoEnums(w *writer) {
	w.p("\n// Opcode says what an Instr does, which is also what each of its fields means.\ntype Opcode uint8\n\nconst (\n\topInvalid Opcode = iota\n")
	for _, o := range instrOps() {
		w.p("\t%s // %s\n", goConst(o), firstLine(o.Doc))
	}
	w.p("\n\t// numOpcodes is one past the last opcode.\n\tnumOpcodes\n)\n\n")
	w.p("var opcodeNames = [...]string{opInvalid: \"invalid\"")
	for _, o := range instrOps() {
		w.p(", %s: %q", goConst(o), o.Name)
	}
	w.p("}\n\nfunc (o Opcode) String() string { return opcodeNames[o] }\n")

	w.p("\n// TermKind says how a block ends.\ntype TermKind uint8\n\nconst (\n\ttermNone TermKind = iota\n")
	for _, o := range termOps() {
		w.p("\t%s // %s\n", goConst(o), firstLine(o.Doc))
	}
	w.p("\n\t// numTermKinds is one past the last terminator.\n\tnumTermKinds\n)\n\n")
	w.p("var termKindNames = [...]string{termNone: \"none\"")
	for _, o := range termOps() {
		w.p(", %s: %q", goConst(o), o.Name)
	}
	w.p("}\n\nfunc (k TermKind) String() string { return termKindNames[k] }\n")

	for _, e := range enums {
		w.p("\n// %s\ntype %s uint8\n\nconst (\n", e.Doc, e.Name)
		for i, c := range e.Cases {
			if i == 0 {
				w.p("\t%s%s %s = iota\n", e.Prefix, c.Name, e.Name)
			} else {
				w.p("\t%s%s\n", e.Prefix, c.Name)
			}
		}
		w.p(")\n\nvar %sNames = [...]string{", lowerFirst(e.Name))
		for i, c := range e.Cases {
			if i > 0 {
				w.p(", ")
			}
			w.p("%q", c.Text)
		}
		w.p("}\n\nfunc (x %s) String() string { return %sNames[x] }\n", e.Name, lowerFirst(e.Name))
	}

	w.p("\n// InstrFlags are the booleans of an instruction.\ntype InstrFlags uint8\n\nconst (\n")
	for i, f := range flags {
		w.p("\t// %s\n", strings.Replace(f.Doc, f.Name, goFlag(f.Name), 1))
		if i == 0 {
			w.p("\t%s InstrFlags = 1 << iota\n", goFlag(f.Name))
		} else {
			w.p("\t%s\n", goFlag(f.Name))
		}
	}
	w.p("\n\t// allFlags are every flag at once.\n\tallFlags = %s<<1 - 1\n)\n", goFlag(flags[len(flags)-1].Name))

	w.p("\n// Effects are what an operation does besides reading its operands and writing its result, which a pass that moves,\n// removes or merges operations asks.\ntype Effects uint8\n\nconst (\n")
	all := append(append([]effect{}, effects...), effFails)
	for i, e := range all {
		if i == 0 {
			w.p("\t%s Effects = 1 << iota\n", goEffect(e))
		} else {
			w.p("\t%s\n", goEffect(e))
		}
	}
	w.p(")\n\nfunc (e Effects) Has(x Effects) bool { return e&x == x }\n")
	for _, term := range []bool{false, true} {
		typ, list, none, recv := "Opcode", "opEffects", "opInvalid", "o"
		if term {
			typ, list, none, recv = "TermKind", "termEffects", "termNone", "k"
		}
		w.p("\nvar %s = [...]Effects{", list)
		n := 0
		for _, o := range ops {
			if o.Term != term {
				continue
			}
			if n > 0 {
				w.p(", ")
			}
			n++
			w.p("%s: %s", goConst(o), goEffectSet(opEffects(o)))
		}
		w.p("}\n\n// Effects are what the operation does.\nfunc (%s %s) Effects() Effects {\n\tif %s == %s || int(%s) >= len(%s) {\n\t\tpanic(diag.Internal(\"ir: %%d is no %s\", int(%s)))\n\t}\n\treturn %s[%s]\n}\n",
			recv, typ, recv, none, recv, list, strings.ToLower(typ), recv, list, recv)
	}
}

func goEffectSet(es []effect) string {
	if len(es) == 0 {
		return "0"
	}
	parts := make([]string, len(es))
	for i, e := range es {
		parts[i] = goEffect(e)
	}
	return strings.Join(parts, " | ")
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func emitGoRecords(w *writer) {
	w.p("\n// Instr is one instruction: a record of fixed size, so a function's instructions are one slice, read in order. Its\n")
	w.p("// fields mean what the opcode says, and only the view of the opcode (ConstInstr, BinInstr, ...) reads them. What does\n")
	w.p("// not fit in a record (a type, a callee, a string, a list of arguments) is in the function's pools, which c and a range\n// of a and b index.\n")
	w.p("type Instr struct {\n\top    Opcode\n\tsub   uint8\n\tflags InstrFlags\n\tdst   V\n\ta     V\n\tb     V\n\tc     int32\n\timm   int64\n}\n")
	w.p("\n// Term is how a block ends, a record of fixed size like an instruction, read through the view of its kind.\n")
	w.p("type Term struct {\n\tkind   TermKind\n\tvalue  V\n\ttarget BlockID\n\tother  BlockID\n\tfirst  int32\n\tcount  int32\n}\n")
	w.p("\nfunc (i Instr) Op() Opcode { return i.op }\n\nfunc (t Term) Kind() TermKind { return t.kind }\n")
}

// emitGoQueries writes what a pass asks of any instruction or terminator: what it defines, where it fails to and
// where it goes.
func emitGoQueries(w *writer) {
	w.p("\n// Def is the register an instruction writes, if it writes one.\nfunc (i Instr) Def() (V, bool) {\n\tswitch i.op {\n")
	for _, r := range []result{valueResult, optionalResult, noResult} {
		var cases []string
		for _, o := range instrOps() {
			if o.Result == r {
				cases = append(cases, goConst(o))
			}
		}
		if len(cases) == 0 {
			continue
		}
		w.p("\tcase %s:\n", strings.Join(cases, ", "))
		switch r {
		case valueResult:
			w.p("\t\treturn i.dst, true\n")
		case optionalResult:
			w.p("\t\treturn i.dst, i.dst != NoReg\n")
		case noResult:
			w.p("\t\treturn NoReg, false\n")
		}
	}
	w.p("\t}\n\tpanic(diag.Internal(\"ir: unhandled opcode %%d\", int(i.op)))\n}\n")

	var always, flagged, never []string
	flagOfFail := map[string]string{}
	for _, o := range instrOps() {
		x, ok := failOperand(o)
		switch {
		case !ok:
			never = append(never, goConst(o))
		case x.Flag == "":
			always = append(always, goConst(o))
		default:
			flagged = append(flagged, goConst(o))
			flagOfFail[goConst(o)] = x.Flag
		}
	}
	w.p("\n// Fail is the block an instruction goes to when its check fails, if it has one.\nfunc (i Instr) Fail() (BlockID, bool) {\n\tswitch i.op {\n")
	if len(always) > 0 {
		w.p("\tcase %s:\n\t\treturn BlockID(i.c), true\n", strings.Join(always, ", "))
	}
	for _, c := range flagged {
		w.p("\tcase %s:\n\t\treturn BlockID(i.c), i.flags&%s != 0\n", c, goFlag(flagOfFail[c]))
	}
	w.p("\tcase %s:\n\t\treturn NoBlock, false\n\t}\n\tpanic(diag.Internal(\"ir: unhandled opcode %%d\", int(i.op)))\n}\n", strings.Join(never, ", "))
	w.p("\n// setFail changes the block an instruction that can fail goes to.\nfunc (i *Instr) setFail(b BlockID) {\n\tif _, ok := i.Fail(); !ok {\n\t\tpanic(diag.Internal(\"ir: %%s has no failure block\", i.op))\n\t}\n\ti.c = int32(b)\n}\n")

	w.p("\n// Successors calls visit for each block that a terminator can go to, in the order of its operands.\nfunc (f *IrFunction) Successors(t Term, visit func(BlockID)) {\n\tswitch t.kind {\n")
	var exits []string
	for _, o := range termOps() {
		var body []string
		for _, x := range o.Operands {
			switch x.Kind {
			case kBlock:
				body = append(body, fmt.Sprintf("\t\tvisit(t.%s)\n", x.Field))
			case kCases:
				body = append(body, fmt.Sprintf("\t\tfor _, c := range f.Cases[t.%s : t.%s+t.%s] {\n\t\t\tvisit(c.Target)\n\t\t}\n", x.Field, x.Field, x.Count))
			}
		}
		if len(body) == 0 {
			exits = append(exits, goConst(o))
			continue
		}
		w.p("\tcase %s:\n%s", goConst(o), strings.Join(body, ""))
	}
	w.p("\tcase %s:\n\tdefault:\n\t\tpanic(diag.Internal(\"ir: a block has no terminator\"))\n\t}\n}\n", strings.Join(exits, ", "))

	w.p("\n// renumbered is a terminator with the blocks it names in its record, not in the Cases pool, renumbered by newID.\nfunc (t Term) renumbered(newID []BlockID) Term {\n\tswitch t.kind {\n")
	var plain []string
	for _, o := range termOps() {
		var body []string
		for _, x := range o.Operands {
			if x.Kind == kBlock {
				body = append(body, fmt.Sprintf("\t\tt.%s = newID[t.%s]\n", x.Field, x.Field))
			}
		}
		if len(body) == 0 {
			plain = append(plain, goConst(o))
			continue
		}
		w.p("\tcase %s:\n%s", goConst(o), strings.Join(body, ""))
	}
	w.p("\tcase %s:\n\tdefault:\n\t\tpanic(diag.Internal(\"ir: a block has no terminator\"))\n\t}\n\treturn t\n}\n", strings.Join(plain, ", "))
}

func failOperand(o op) (operand, bool) {
	for _, x := range o.Operands {
		if x.Kind == kFail {
			return x, true
		}
	}
	return operand{}, false
}

func emitGoCtors(w *writer) {
	for _, o := range ops {
		var params []string
		if o.Result != noResult {
			params = append(params, "dst V")
		}
		for _, x := range o.Operands {
			params = append(params, goParams(x))
		}
		doc := o.Doc
		if opUsesPool(o) {
			doc = fmt.Sprintf("%s is the %s whose pool operands the builder has appended.", goCtor(o), o.Name)
		}
		w.p("\n// %s\nfunc %s(%s) %s {\n\tr := %s{%s: %s}\n", doc, goCtor(o), strings.Join(params, ", "), goRecord(o), goRecord(o), kindField(o), goConst(o))
		if o.Result != noResult {
			w.p("\tr.dst = dst\n")
		}
		for _, x := range o.Operands {
			goSets(w, x)
		}
		w.p("\treturn r\n}\n")
	}
}

func kindField(o op) string {
	if o.Term {
		return "kind"
	}
	return "op"
}

func emitGoViews(w *writer) {
	for _, o := range ops {
		v := goView(o)
		w.p("\n// %s is the view of a%s %s. %s\ntype %s %s\n", v, article(o.Name), o.Name, o.Doc, v, goRecord(o))
		switch o.Result {
		case valueResult:
			w.p("\nfunc (x *%s) Dst() V { return x.dst }\n", v)
		case optionalResult:
			w.p("\n// Dst is the register that receives the value, if anything reads it.\nfunc (x *%s) Dst() (V, bool) { return x.dst, x.dst != NoReg }\n", v)
		}
		for i, x := range o.Operands {
			recvF := ""
			if usesPool(x) {
				recvF = "f *IrFunction"
			}
			if optional(x) {
				w.p("\nfunc (x *%s) %s(%s) (%s, bool) {\n\tif %s {\n\t\treturn %s, false\n\t}\n\treturn %s, true\n}\n",
					v, x.Name, recvF, goAccessorType(x), goMissing(x), goAbsent(x), goRead(o, i))
				continue
			}
			w.p("\nfunc (x *%s) %s(%s) %s { return %s }\n", v, x.Name, recvF, goAccessorType(x), goRead(o, i))
		}
	}
}

func article(name string) string {
	if strings.ContainsRune("AEIOU", rune(name[0])) {
		return "n"
	}
	return ""
}

func emitGoVisitors(w *writer, state bool) {
	for _, term := range []bool{false, true} {
		rec, enumField, missing, member, added := "Instr", "op", "unhandled opcode %d", "opcode", "an opcode"
		if term {
			rec, enumField, missing, member, added = "Term", "kind", "a block has no terminator, or one of kind %d", "kind of terminator", "a terminator"
		}
		iface, dispatch, tparams, targs, extra, ret := rec+"Visitor", "Visit"+rec, "", "", "", ""
		if state {
			iface, dispatch, tparams, targs, extra, ret = rec+"StateVisitor", "Visit"+rec+"State", "[S, R any]", "[S, R]", ", s S", " R"
		}
		w.p("\n// %s has a method for every %s, so %s added to the table fails the build where it is not handled yet.\ntype %s%s interface {\n",
			iface, member, added, iface, tparams)
		for _, o := range ops {
			if o.Term == term {
				w.p("\tVisit%s(x *%s%s)%s\n", o.Name, goView(o), extra, ret)
			}
		}
		w.p("}\n\n// %s calls the method of v for the %s of x.\nfunc %s%s(v %s%s, x *%s%s)%s {\n\tswitch x.%s {\n", dispatch, enumField, dispatch, tparams, iface, targs, rec, extra, ret, enumField)
		for _, o := range ops {
			if o.Term != term {
				continue
			}
			arg := ""
			if state {
				arg = ", s"
			}
			if state {
				w.p("\tcase %s:\n\t\treturn v.Visit%s((*%s)(x)%s)\n", goConst(o), o.Name, goView(o), arg)
			} else {
				w.p("\tcase %s:\n\t\tv.Visit%s((*%s)(x))\n\t\treturn\n", goConst(o), o.Name, goView(o))
			}
		}
		w.p("\t}\n\tpanic(diag.Internal(\"ir: %s\", int(x.%s)))\n}\n", missing, enumField)
	}
}

func emitGoPrinter(w *writer) {
	for _, term := range []bool{false, true} {
		rec, method, arg, sel, doc := "Instr", "InstrString", "i", "op", "InstrString is the text the printer gives an instruction, for a tool that reports a place in a function."
		missing := "printer: unhandled opcode %d"
		if term {
			rec, method, arg, sel, doc = "Term", "TermString", "t", "kind", "TermString is the text the printer gives a terminator."
			missing = "printer: a block has no terminator, or one of kind %d"
		}
		w.p("\n// %s\nfunc (f *IrFunction) %s(%s %s) string {\n\tswitch %s.%s {\n", doc, method, arg, rec, arg, sel)
		for _, o := range ops {
			if o.Term != term {
				continue
			}
			w.p("\tcase %s:\n", goConst(o))
			if o.Result != noResult || len(o.Operands) > 0 {
				w.p("\t\tx := (*%s)(&%s)\n", goView(o), arg)
			}
			goPrintOp(w, o)
		}
		w.p("\t}\n\tpanic(diag.Internal(%q, int(%s.%s)))\n}\n", missing, arg, sel)
	}
}

// goPrintOp writes the text of one operation: one expression when nothing in it is conditional, and otherwise a string
// that grows by each optional part that is there.
func goPrintOp(w *writer, o op) {
	var pending []string
	started := false
	flush := func() {
		if len(pending) == 0 && started {
			return
		}
		expr := `""`
		if len(pending) > 0 {
			expr = strings.Join(pending, " + ")
		}
		if started {
			w.p("\t\ts += %s\n", expr)
		} else {
			w.p("\t\ts := %s\n", expr)
			started = true
		}
		pending = nil
	}
	if o.Result != noResult {
		pending = append(pending, "resultText(x.dst)")
	}
	for _, p := range templateParts(o.Text) {
		if p.operand == "" {
			pending = append(pending, fmt.Sprintf("%q", p.text))
			continue
		}
		x := operandOf(o, p.operand)
		fArg := ""
		if usesPool(x) {
			fArg = "f"
		}
		get := fmt.Sprintf("x.%s(%s)", x.Name, fArg)
		switch {
		case x.Kind == kFlag:
			flush()
			w.p("\t\tif %s {\n\t\t\ts += %q\n\t\t}\n", get, x.Text)
		case optional(x):
			flush()
			w.p("\t\tif v, ok := %s; ok {\n\t\t\ts += %q + %s\n\t\t}\n", get, x.Text, goText(x, "v"))
		default:
			pending = append(pending, goText(x, get))
		}
	}
	if !started {
		if len(pending) == 0 {
			pending = append(pending, `""`)
		}
		w.p("\t\treturn %s\n", strings.Join(pending, " + "))
		return
	}
	if len(pending) > 0 {
		flush()
	}
	w.p("\t\treturn s\n")
}

// goText is the text of an operand's value.
func goText(x operand, v string) string {
	switch x.Kind {
	case kReg, kErrorReg, kSymbol, kEnum:
		return v + ".String()"
	case kRegs, kMoreRegs:
		return "regList(" + v + ")"
	case kSlot:
		return "slotText(" + v + ")"
	case kInt:
		return "strconv.Itoa(" + v + ")"
	case kWord:
		return "strconv.FormatInt(" + v + ", 10)"
	case kBlock, kFail:
		return "blockText(" + v + ")"
	case kText:
		return "quote(" + v + ")"
	case kType:
		return v + ".Name()"
	case kCallee, kRoutine, kBehaviour:
		return v + ".Symbol()"
	case kCases:
		return "caseList(" + v + ")"
	}
	fail("operand %s has no text", x.Name)
	return ""
}

func emitGoCodec(w *writer) {
	for _, term := range []bool{false, true} {
		rec, sel, enumType, count, none := "Instr", "op", "Opcode", "numOpcodes", "opInvalid"
		if term {
			rec, sel, enumType, count, none = "Term", "kind", "TermKind", "numTermKinds", "termNone"
		}
		w.p("\n// Encode%s writes the fields of %s that its %s uses, in the wire format of the build cache.\nfunc Encode%s(e *cache.Encoder, r %s) {\n\te.Int(int(r.%s))\n\tswitch r.%s {\n",
			rec, strings.ToLower(rec[:1])+rec[1:], sel, rec, rec, sel, sel)
		for _, o := range ops {
			if o.Term != term {
				continue
			}
			w.p("\tcase %s:\n", goConst(o))
			for _, f := range fieldsOf(o) {
				switch f {
				case fSub, fFlags:
					w.p("\t\te.Int(int(r.%s))\n", f)
				default:
					w.p("\t\tputSigned(e, int64(r.%s))\n", f)
				}
			}
		}
		w.p("\tdefault:\n\t\tpanic(diag.Internal(\"ir: %%d is no %s to encode\", int(r.%s)))\n\t}\n}\n", strings.ToLower(enumType), sel)

		w.p("\n// Decode%s reads what Encode%s wrote; data that no %s encodes fails the decoder.\nfunc Decode%s(d *cache.Decoder) %s {\n\tr := %s{%s: %s(d.Int(int(%s) - 1))}\n\tswitch r.%s {\n",
			rec, rec, rec, rec, rec, rec, sel, enumType, count, sel)
		for _, o := range ops {
			if o.Term != term {
				continue
			}
			w.p("\tcase %s:\n", goConst(o))
			for _, f := range fieldsOf(o) {
				switch f {
				case fSub:
					w.p("\t\tr.sub = uint8(d.Int(len(%sNames) - 1))\n", lowerFirst(enumOperand(o).Enum))
				case fFlags:
					w.p("\t\tr.flags = InstrFlags(d.Int(int(allFlags)))\n")
				case fImm:
					w.p("\t\tr.imm = signed(d)\n")
				default:
					w.p("\t\tr.%s = %s(signedIndex(d))\n", f, goFieldType[f])
				}
			}
		}
		w.p("\tcase %s:\n\t\td.Fail()\n\t}\n\treturn r\n}\n", none)
	}
}

func enumOperand(o op) operand {
	for _, x := range o.Operands {
		if x.Kind == kEnum {
			return x
		}
	}
	fail("%s uses sub but has no enum operand", o.Name)
	return operand{}
}
