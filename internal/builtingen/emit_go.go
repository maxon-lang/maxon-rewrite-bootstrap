package main

import (
	"fmt"
	"slices"
	"strings"
)

// goTypeParamVar is the variable that holds a free function's own type parameter in the generated declarations.
const goTypeParamVar = "t"

func goConv(conv string) string {
	if conv == "" {
		conv = "let"
	}
	return "Conv" + upperFirst(conv)
}

func goOwner(owner string) string { return "b." + owner }

// goType is the Go expression, inside ast.Builtins, of a type the row writes.
func (f fn) goType(t *tyExpr) string {
	switch {
	case t.name == "Self":
		return fmt.Sprintf("SelfType(%s)", goOwner(f.Owner))
	case t.name == f.generic:
		return goTypeParamVar
	case paramIndex(ownerParams(f.Owner), t.name) >= 0:
		return fmt.Sprintf("%s.Params[%d]", goOwner(f.Owner), paramIndex(ownerParams(f.Owner), t.name))
	case primOf(t.name) != nil:
		return primOf(t.name).Go
	case t.arg != nil:
		return fmt.Sprintf("Instantiate(%s, []Ty{%s})", goOwner(t.name), f.goType(t.arg))
	}
	return goOwner(t.name)
}

func (f fn) goParams() string {
	var sb strings.Builder
	for _, p := range f.params {
		fmt.Fprintf(&sb, ", param(%q, %s, %s)", p.name, f.goType(p.ty), goConv(p.conv))
	}
	return sb.String()
}

func emitAst(t table) string {
	var sb strings.Builder
	sb.WriteString(header + "package ast\n\nimport \"minimaxon/internal/diag\"\n\n")

	sb.WriteString("// BuiltinKind says which runtime primitive a BuiltinTy is.\ntype BuiltinKind int\n\nconst (\n")
	for i, b := range builtinTypes {
		if i == 0 {
			fmt.Fprintf(&sb, "\tBuiltin%s BuiltinKind = iota\n", b.Key)
		} else {
			fmt.Fprintf(&sb, "\tBuiltin%s\n", b.Key)
		}
	}
	sb.WriteString(")\n\n")
	sb.WriteString("var builtinKinds = [...]builtinKindInfo{\n")
	for _, b := range builtinTypes {
		fmt.Fprintf(&sb, "\tBuiltin%s: {name: %q, managed: %v, subscriptable: %v, shared: %v},\n", b.Key, b.Name, b.Managed, b.Subscriptable, b.Shared)
	}
	sb.WriteString("}\n\n")

	sb.WriteString("// Intrinsic names a function that no source declares: the checker types a call of it like any other, and\n")
	sb.WriteString("// lowering turns it into IR instead of a call.\ntype Intrinsic int\n\nconst (\n\tNotIntrinsic Intrinsic = iota\n")
	for _, in := range t.intrinsics {
		fmt.Fprintf(&sb, "\tIntrinsic%s\n", in.name)
	}
	sb.WriteString(")\n\n")
	emitRuntimeOnly(&sb, t)
	emitTableType(&sb)

	for _, r := range recordTypes {
		if len(r.Fields) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "// The fields of %s, by index.\nconst (\n", r.Name)
		for i, f := range r.Fields {
			fmt.Fprintf(&sb, "\t%s = %d\n", fieldConst(r, parseField(f)), i)
		}
		sb.WriteString(")\n\n")
	}
	for _, u := range unionTypes {
		fmt.Fprintf(&sb, "// The cases of %s, by index.\nconst (\n", u.Name)
		for i, c := range u.Cases {
			fmt.Fprintf(&sb, "\t%s = %d\n", c.Const, i)
		}
		sb.WriteString(")\n\n")
	}
	for _, e := range enumTypes {
		fmt.Fprintf(&sb, "// The cases of %s, by index.\nconst (\n", e.Name)
		for i, c := range e.Cases {
			fmt.Fprintf(&sb, "\t%s = %d\n", c.Const, i)
		}
		sb.WriteString(")\n\n")
	}

	sb.WriteString("// builtinDecls are the declarations of the table that the rest of the compiler names.\ntype builtinDecls struct {\n")
	for _, b := range builtinTypes {
		fmt.Fprintf(&sb, "\t%s *BuiltinTy\n", b.Key)
	}
	for _, r := range recordTypes {
		fmt.Fprintf(&sb, "\t%s *RecordTy\n", r.Key)
	}
	for _, u := range unionTypes {
		fmt.Fprintf(&sb, "\t%s *UnionTy\n", u.Key)
	}
	for _, e := range enumTypes {
		fmt.Fprintf(&sb, "\t%s *EnumTy\n", e.Key)
	}
	for _, c := range slices.Concat(capabilities, markers) {
		fmt.Fprintf(&sb, "\t%s *InterfaceTy\n", c)
	}
	for _, f := range t.fns {
		if f.Field != "" {
			fmt.Fprintf(&sb, "\t%s *FuncDecl\n", f.Field)
		}
	}
	sb.WriteString("}\n\n")

	sb.WriteString("// declareTable declares every type and function of the table in the stdlib module.\nfunc (b *Builtins) declareTable() {\n")
	for _, bt := range builtinTypes {
		fmt.Fprintf(&sb, "\tb.%s = b.newPrimitive(Builtin%s%s)\n", bt.Key, bt.Key, quotedList(bt.Params))
	}
	for _, r := range recordTypes {
		fmt.Fprintf(&sb, "\tb.%s = b.newRecord(%q)\n", r.Key, r.Name)
	}
	for _, r := range recordTypes {
		if len(r.Fields) == 0 {
			continue
		}
		owner := fn{Owner: r.Key}
		fmt.Fprintf(&sb, "\tb.%s.Fields = []*FieldInfo{\n", r.Key)
		for _, s := range r.Fields {
			f := parseField(s)
			vis := "Public"
			if f.private {
				vis = "Private"
			}
			fmt.Fprintf(&sb, "\t\t{Name: %q, Visibility: %s, Type: %s, IsVar: %v, Index: %s},\n", f.name, vis, owner.goType(f.ty), f.isVar, fieldConst(r, f))
		}
		sb.WriteString("\t}\n")
	}
	for _, u := range unionTypes {
		fmt.Fprintf(&sb, "\tb.%s = b.newUnion(%q%s)\n", u.Key, u.Name, quotedList(u.Params))
		owner := fn{Owner: u.Key}
		fmt.Fprintf(&sb, "\tb.%s.Cases = []*UnionCase{\n", u.Key)
		for _, c := range u.Cases {
			fmt.Fprintf(&sb, "\t\t{Name: %q", c.Name)
			if len(c.Payload) > 0 {
				sb.WriteString(", Payload: []PayloadField{")
				for i, p := range c.Payload {
					name, ty, _ := strings.Cut(p, " ")
					if i > 0 {
						sb.WriteString(", ")
					}
					fmt.Fprintf(&sb, "{Name: %q, Type: %s}", name, owner.goType(parseType(ty)))
				}
				sb.WriteString("}")
			}
			sb.WriteString("},\n")
		}
		sb.WriteString("\t}\n")
	}
	for _, e := range enumTypes {
		var cases []string
		for _, c := range e.Cases {
			cases = append(cases, c.Name)
		}
		fmt.Fprintf(&sb, "\tb.%s = b.newEnum(%q%s)\n", e.Key, e.Name, quotedList(cases))
	}
	for _, c := range slices.Concat(capabilities, markers) {
		fmt.Fprintf(&sb, "\tb.%s = b.declareInterface(%q)\n", c, c)
	}
	for _, a := range typeAliases {
		fmt.Fprintf(&sb, "\tb.declareAlias(%q, %s)\n", a.Name, fn{}.goType(parseType(a.Type)))
	}
	for _, f := range t.fns {
		emitGoFunction(&sb, f)
	}
	sb.WriteString("}\n")
	return sb.String()
}

func emitGoFunction(sb *strings.Builder, f fn) {
	op := "Intrinsic" + f.Intrinsic
	ret := f.goType(f.result)
	var made, place string
	var extra []string
	switch {
	case f.Owner == stringOwner:
		made = fmt.Sprintf("b.function(%s, %q, %s%s)", op, f.Name, ret, f.goParams())
		place = fmt.Sprintf("b.StringMethods[%q] = %%s", f.Name)
		if f.Static {
			extra = append(extra, "f.IsStatic = true")
		}
	case f.Owner != "":
		made = fmt.Sprintf("b.method(%s, %s, %q, %v, %s, %s%s)", goOwner(f.Owner), op, f.Name, f.Static, goConv(f.Self), ret, f.goParams())
		place = "%s"
		for _, bd := range f.bounds {
			extra = append(extra, fmt.Sprintf("f.Bounds = append(f.Bounds, ParamBound{Param: %s, Iface: b.%s})", f.goType(&tyExpr{name: bd.param}), bd.iface))
		}
	default:
		place = "b.stdlib.Declare(%s)"
		if f.Field != "" {
			place = fmt.Sprintf("b.%s = %%s", f.Field)
		}
		made = fmt.Sprintf("b.function(%s, %q, %s%s)", op, f.Name, ret, f.goParams())
		if f.generic != "" {
			made = fmt.Sprintf("b.genericFunction(%s, %q, %s, %s%s)", op, f.Name, goTypeParamVar, ret, f.goParams())
		}
	}
	if f.Throws != "" {
		extra = append(extra, fmt.Sprintf("f.ErrorType = b.%s", f.Throws))
	}
	var lines []string
	if f.generic != "" {
		tp := fmt.Sprintf("newTypeParamTy(%q)", f.generic)
		if f.gbound != "" {
			tp = fmt.Sprintf("newBoundedTypeParamTy(%q, b.%s)", f.generic, f.gbound)
		}
		lines = append(lines, fmt.Sprintf("%s := %s", goTypeParamVar, tp))
	}
	if len(extra) == 0 {
		lines = append(lines, fmt.Sprintf(place, made))
	} else {
		lines = append(lines, "f := "+made)
		lines = append(lines, extra...)
		if place != "%s" {
			lines = append(lines, fmt.Sprintf(place, "f"))
		}
	}
	if len(lines) == 1 {
		fmt.Fprintf(sb, "\t%s\n", lines[0])
		return
	}
	sb.WriteString("\t{\n")
	for _, l := range lines {
		fmt.Fprintf(sb, "\t\t%s\n", l)
	}
	sb.WriteString("\t}\n")
}

func quotedList(xs []string) string {
	var sb strings.Builder
	for _, x := range xs {
		fmt.Fprintf(&sb, ", %q", x)
	}
	return sb.String()
}

func emitCheck(t table) string {
	var sb strings.Builder
	sb.WriteString(header + "package check\n\nimport (\n\t\"minimaxon/internal/ast\"\n\t\"minimaxon/internal/diag\"\n)\n\n")
	sb.WriteString("// checkIntrinsicArgs refuses an argument of a built-in that its signature alone cannot.\n")
	sb.WriteString("func (c *Checker) checkIntrinsicArgs(call ast.CallExpr, target *ast.FuncDecl, ordered []ast.Expr) {\n\tswitch target.Intrinsic {\n")
	var unchecked []string
	for _, in := range t.intrinsics {
		if in.checked {
			fmt.Fprintf(&sb, "\tcase ast.Intrinsic%s:\n\t\tc.check%sArgs(call, ordered)\n", in.name, in.name)
		} else {
			unchecked = append(unchecked, "ast.Intrinsic"+in.name)
		}
	}
	if len(unchecked) > 0 {
		fmt.Fprintf(&sb, "\tcase %s:\n\t\t// The signature checks every argument.\n", strings.Join(unchecked, ",\n\t\t"))
	}
	sb.WriteString("\tdefault:\n\t\tpanic(diag.Internal(\"check: %d is no intrinsic\", int(target.Intrinsic)))\n\t}\n}\n")
	return sb.String()
}

func emitIr(t table) string {
	var sb strings.Builder
	sb.WriteString(header + "package ir\n\nimport (\n\t\"minimaxon/internal/ast\"\n\t\"minimaxon/internal/diag\"\n)\n\n")

	sb.WriteString("// Rt names a symbol the runtime defines: an entry that code calls or a global it reads. The set is closed, so an\n")
	sb.WriteString("// instruction holds one in a byte and the backend keys a symbol by it without making a string.\ntype Rt uint8\n\nconst (\n")
	for i, e := range entries {
		if i == 0 {
			fmt.Fprintf(&sb, "\tRt%s Rt = iota + 1 // %s\n", e.Name, e.Doc)
		} else {
			fmt.Fprintf(&sb, "\tRt%s // %s\n", e.Name, e.Doc)
		}
	}
	sb.WriteString(")\n\nvar rtNames = [...]string{\n")
	for _, e := range entries {
		fmt.Fprintf(&sb, "\tRt%s: %q,\n", e.Name, e.Symbol)
	}
	sb.WriteString("}\n\n")

	sb.WriteString("// RtUnit is where a runtime symbol is defined.\ntype RtUnit uint8\n\nconst (\n")
	for i, u := range units {
		if i == 0 {
			fmt.Fprintf(&sb, "\tRtUnit%s RtUnit = iota + 1 // %s\n", u.Key, u.Doc)
		} else {
			fmt.Fprintf(&sb, "\tRtUnit%s // %s\n", u.Key, u.Doc)
		}
	}
	sb.WriteString(")\n\nvar rtUnits = [...]RtUnit{\n")
	for _, e := range entries {
		fmt.Fprintf(&sb, "\tRt%s: RtUnit%s,\n", e.Name, e.Unit)
	}
	sb.WriteString("}\n\n")
	emitRtFunctions(&sb, t)

	sb.WriteString("// intrinsicCall lowers a call of a built-in whose receiver and arguments are lowered.\n")
	sb.WriteString("func (l *Lowerer) intrinsicCall(c call, recv V, args []V) V {\n\tswitch c.Target.Intrinsic {\n")
	var runtimeOnly []string
	for _, in := range t.intrinsics {
		if in.runtimeOnly {
			runtimeOnly = append(runtimeOnly, "ast.Intrinsic"+in.name)
			continue
		}
		fmt.Fprintf(&sb, "\tcase ast.Intrinsic%s:\n\t\treturn l.lower%s(c, recv, args)\n", in.name, in.name)
	}
	if len(runtimeOnly) > 0 {
		fmt.Fprintf(&sb, "\tcase %s:\n\t\treturn l.lowerRuntimeOnly(c)\n", strings.Join(runtimeOnly, ",\n\t\t"))
	}
	sb.WriteString("\t}\n\tpanic(diag.Internal(\"lower: %d is no intrinsic\", int(c.Target.Intrinsic)))\n}\n\n")

	sb.WriteString("// ownsOperands is whether the lowering of an intrinsic evaluates its receiver and arguments itself.\n")
	sb.WriteString("func ownsOperands(op ast.Intrinsic) bool {\n\tswitch op {\n")
	var own, given []string
	for _, in := range t.intrinsics {
		if in.ownsOperands {
			own = append(own, "ast.Intrinsic"+in.name)
		} else {
			given = append(given, "ast.Intrinsic"+in.name)
		}
	}
	if len(own) > 0 {
		fmt.Fprintf(&sb, "\tcase %s:\n\t\treturn true\n", strings.Join(own, ",\n\t\t"))
	}
	if len(given) > 0 {
		fmt.Fprintf(&sb, "\tcase %s:\n\t\treturn false\n", strings.Join(given, ",\n\t\t"))
	}
	sb.WriteString("\t}\n\tpanic(diag.Internal(\"lower: %d is no intrinsic\", int(op)))\n}\n")
	return sb.String()
}

func emitInterp(table) string {
	var sb strings.Builder
	sb.WriteString(header + "package interp\n\nimport (\n\t\"minimaxon/internal/diag\"\n\t\"minimaxon/internal/ir\"\n)\n\n")
	sb.WriteString("// runEntry runs a call of a runtime entry.\nfunc (vm *vm) runEntry(c *runtimeCall) {\n\tswitch c.Symbol() {\n")
	var others []string
	for _, e := range entries {
		if e.Role == roleCall {
			fmt.Fprintf(&sb, "\tcase ir.Rt%s:\n\t\tvm.entry%s(c)\n", e.Name, e.Name)
		} else {
			others = append(others, "ir.Rt"+e.Name)
		}
	}
	if len(others) > 0 {
		fmt.Fprintf(&sb, "\tcase %s:\n\t\tpanic(diag.Internal(\"interp: %%s is no entry the IR calls\", c.Symbol()))\n", strings.Join(others, ",\n\t\t"))
	}
	sb.WriteString("\tdefault:\n\t\tpanic(diag.Internal(\"interp: %d is no runtime symbol\", int(c.Symbol())))\n\t}\n}\n")
	return sb.String()
}

func emitBackend(table) string {
	var sb strings.Builder
	sb.WriteString(header + "package backend\n\nimport \"minimaxon/internal/ir\"\n")
	for _, u := range units {
		code := codeEntries(u)
		if u.Emitter == "" {
			continue
		}
		fmt.Fprintf(&sb, "\n// defineEntries writes the code of every entry of %s.\nfunc (g *%s) defineEntries() {\n", u.Doc, u.Emitter)
		for _, e := range code {
			fmt.Fprintf(&sb, "\tg.u.DefineCode(ir.Rt%s.String())\n\tg.emit%s()\n", e.Name, e.Name)
		}
		sb.WriteString("}\n")
	}
	return sb.String()
}

// emitRuntimeOnly writes Intrinsic.RuntimeOnly, which the checker asks of every call of a built-in.
func emitRuntimeOnly(sb *strings.Builder, t table) {
	sb.WriteString("// RuntimeOnly is whether only the runtime module may call the intrinsic (E3152).\n")
	sb.WriteString("func (op Intrinsic) RuntimeOnly() bool {\n\tswitch op {\n")
	var only []string
	anywhere := []string{"NotIntrinsic"}
	for _, in := range t.intrinsics {
		if in.runtimeOnly {
			only = append(only, "Intrinsic"+in.name)
		} else {
			anywhere = append(anywhere, "Intrinsic"+in.name)
		}
	}
	if len(only) > 0 {
		fmt.Fprintf(sb, "\tcase %s:\n\t\treturn true\n", strings.Join(only, ",\n\t\t"))
	}
	if len(anywhere) > 0 {
		fmt.Fprintf(sb, "\tcase %s:\n\t\treturn false\n", strings.Join(anywhere, ",\n\t\t"))
	}
	sb.WriteString("\t}\n\tpanic(diag.Internal(\"ast: %d is no intrinsic\", int(op)))\n}\n\n")
}

// emitTableType writes Builtins.TableType, the type that a key of primTypes names, which is how a signature that the
// table writes as text is compared with a declared one.
func emitTableType(sb *strings.Builder) {
	sb.WriteString("// TableType is the type that a key of the built-in table's types names.\n")
	sb.WriteString("func (b *Builtins) TableType(key string) Ty {\n\tswitch key {\n")
	for _, p := range primTypes {
		fmt.Fprintf(sb, "\tcase %q:\n\t\treturn %s\n", p.Key, p.Go)
	}
	sb.WriteString("\t}\n\tpanic(diag.Internal(\"ast: %s is no type of the built-in table\", key))\n}\n\n")
}

// emitRtFunctions writes the functions of the runtime's source that define entries, by entry.
func emitRtFunctions(sb *strings.Builder, t table) {
	sb.WriteString("// RtFunction is the function of the runtime's source that defines an entry (README, \"The runtime\"): code calls the\n")
	sb.WriteString("// entry as it calls any function, so its signature is the entry's calling convention.\ntype RtFunction struct {\n")
	sb.WriteString("\tName string\n\t// Symbol is the function's link name.\n\tSymbol string\n\tParams []RtParam\n")
	sb.WriteString("\t// Result is the key of ast.Builtins.TableType that names what it returns.\n\tResult string\n}\n\n")
	sb.WriteString("// RtParam is a parameter of an RtFunction, its type a key of ast.Builtins.TableType.\ntype RtParam struct {\n")
	sb.WriteString("\tConv ast.Convention\n\tName string\n\tType string\n}\n\n")
	sb.WriteString("var rtFunctions = [...]*RtFunction{\n")
	for _, r := range t.rtFuncs {
		fmt.Fprintf(sb, "\tRt%s: {Name: %q, Symbol: %q, Params: []RtParam{", r.entry.Name, r.name, r.symbol())
		for i, p := range r.params {
			if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(sb, "{Conv: ast.%s, Name: %q, Type: %q}", goConv(p.conv), p.name, p.ty.name)
		}
		fmt.Fprintf(sb, "}, Result: %q},\n", r.result.name)
	}
	sb.WriteString("}\n\n")
	sb.WriteString("// Function is the function of the runtime's source that defines the entry, when one does: self links a call of the\n")
	sb.WriteString("// entry to it, and the Go compiler, whose runtime is its own code, only holds the runtime's source to it.\n")
	sb.WriteString("func (r Rt) Function() (*RtFunction, bool) {\n\tif int(r) >= len(rtFunctions) || rtFunctions[r] == nil {\n\t\treturn nil, false\n\t}\n\treturn rtFunctions[r], true\n}\n\n")
	sb.WriteString("// RtFunctions are the entries that the runtime's source defines, in the table's order.\nvar RtFunctions = []Rt{")
	for i, r := range t.rtFuncs {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(sb, "Rt%s", r.entry.Name)
	}
	sb.WriteString("}\n\n")
}
