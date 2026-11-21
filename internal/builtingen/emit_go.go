package main

import (
	"fmt"
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
	sb.WriteString(header + "package ast\n\n")

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
	for _, u := range unionTypes {
		fmt.Fprintf(&sb, "\t%s *UnionTy\n", u.Key)
	}
	for _, e := range enumTypes {
		fmt.Fprintf(&sb, "\t%s *EnumTy\n", e.Key)
	}
	for _, c := range capabilities {
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
	for _, c := range capabilities {
		fmt.Fprintf(&sb, "\tb.%s = b.declareCapability(%q)\n", c, c)
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
	switch {
	case f.Owner == stringOwner:
		fmt.Fprintf(sb, "\tb.StringMethods[%q] = b.function(%s, %q, %s%s)\n", f.Name, op, f.Name, ret, f.goParams())
	case f.Owner != "":
		made := fmt.Sprintf("b.method(%s, %s, %q, %v, %s, %s%s)", goOwner(f.Owner), op, f.Name, f.Static, goConv(f.Self), ret, f.goParams())
		if len(f.bounds) == 0 {
			fmt.Fprintf(sb, "\t%s\n", made)
			return
		}
		fmt.Fprintf(sb, "\t{\n\t\tf := %s\n", made)
		for _, bd := range f.bounds {
			fmt.Fprintf(sb, "\t\tf.Bounds = append(f.Bounds, ParamBound{Param: %s, Iface: b.%s})\n", f.goType(&tyExpr{name: bd.param}), bd.iface)
		}
		sb.WriteString("\t}\n")
	default:
		place := func(made string) string {
			if f.Field != "" {
				return fmt.Sprintf("b.%s = %s", f.Field, made)
			}
			return fmt.Sprintf("b.stdlib.Declare(%s)", made)
		}
		if f.generic == "" {
			fmt.Fprintf(sb, "\t%s\n", place(fmt.Sprintf("b.function(%s, %q, %s%s)", op, f.Name, ret, f.goParams())))
			return
		}
		tp := fmt.Sprintf("newTypeParamTy(%q)", f.generic)
		if f.gbound != "" {
			tp = fmt.Sprintf("newBoundedTypeParamTy(%q, b.%s)", f.generic, f.gbound)
		}
		fmt.Fprintf(sb, "\t{\n\t\t%s := %s\n", goTypeParamVar, tp)
		fmt.Fprintf(sb, "\t\t%s\n\t}\n", place(fmt.Sprintf("b.genericFunction(%s, %q, %s, %s%s)", op, f.Name, goTypeParamVar, ret, f.goParams())))
	}
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

	sb.WriteString("// intrinsicCall lowers a call of a built-in whose receiver and arguments are lowered.\n")
	sb.WriteString("func (l *Lowerer) intrinsicCall(c call, recv V, args []V) V {\n\tswitch c.Target.Intrinsic {\n")
	for _, in := range t.intrinsics {
		fmt.Fprintf(&sb, "\tcase ast.Intrinsic%s:\n\t\treturn l.lower%s(c, recv, args)\n", in.name, in.name)
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
