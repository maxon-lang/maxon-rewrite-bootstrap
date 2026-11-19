package check

import (
	"fmt"
	"math/big"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"slices"
	"strings"
)

type checkerBlockKind int

const (
	checkerLoop checkerBlockKind = iota
	checkerMatch
)

type checkerBlock struct {
	Label string
	Kind  checkerBlockKind
	Node  ast.Node
}

var (
	checkerArithmetic = map[string]bool{"+": true, "-": true, "*": true, "/": true, "mod": true, "shl": true, "shr": true}
	checkerComparison = map[string]bool{"==": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true}
	checkerLogical    = map[string]bool{"and": true, "or": true, "xor": true}
)

const checkerShiftLimit = 64

type Checker struct {
	project *ast.Project
	module  *ast.Module
	errs    *[]*diag.CompileError

	Pending []ast.PendingCall

	fn         *ast.FuncDecl
	owner      ast.NominalTy
	allowCalls bool
	scopes     []map[string]*ast.Local
	blocks     []checkerBlock
	nextSlot   int
}

func NewChecker(p *ast.Project, m *ast.Module, errs *[]*diag.CompileError) *Checker {
	return &Checker{project: p, module: m, errs: errs}
}

func (c *Checker) countTy() *ast.IntTy {
	return c.project.Stdlib.Names["Count"].(*ast.AliasDecl).Ty.(*ast.IntTy)
}

func (c *Checker) fullRangeTy() *ast.IntTy {
	return c.project.Stdlib.Names["FullRangeInt"].(*ast.AliasDecl).Ty.(*ast.IntTy)
}

func (c *Checker) guard(f func()) {
	diag.Guard(c.errs, f)
}

func (c *Checker) Declare(d ast.Decl) []*ast.FuncDecl {
	declared := []*ast.FuncDecl{}
	c.guard(func() { c.declareMembers(d) })
	switch t := d.(type) {
	case *ast.FuncDecl:
		c.guard(func() { c.addFunction(t, nil, &declared) })
	case ast.TypeDeclNode:
		if nt, ok := t.TypeBase().Ty.(ast.NominalTy); ok {
			c.addMethods(ast.Methods(d), nt, &declared)
		}
	}
	if td, ok := d.(*ast.TypeDecl); ok {
		c.declareFieldDefaults(td)
	}
	return declared
}

func (c *Checker) CheckBody(d ast.Decl) {
	switch t := d.(type) {
	case *ast.FuncDecl:
		c.guard(func() { c.checkFunction(t) })
	case *ast.TypeDecl:
		c.checkFieldDefaults(t)
		c.guard(func() { c.checkCycle(t.Ty.(ast.NominalTy)) })
	case *ast.UnionDecl:
		c.guard(func() { c.checkCycle(t.Ty.(ast.NominalTy)) })
	default:
		panic(diag.Internal("no body to check in %T", d))
	}
}

func (c *Checker) Verify(calls []ast.PendingCall) {
	for _, p := range calls {
		c.guard(func() { p.Call.Ordered = c.bindArgs(p.Call, p.Callee, p.Params, true) })
	}
}

func (c *Checker) resolve(tr *ast.TypeRef, self ast.NominalTy) ast.Ty {
	if len(tr.Qualifier) == 0 {
		switch tr.Name {
		case "bool":
			return ast.BoolTy
		case "String":
			return ast.StringTy
		case "Self":
			if self == nil {
				panic(tr.Error(diag.ErrUnknownType, "'Self' names a type only inside one"))
			}
			return self
		case "int", "float":
			panic(tr.Error(diag.ErrTypeMismatch, fmt.Sprintf("a bare '%s' needs a ranged typealias", tr.Name)))
		}
	}
	if tb, ok := tr.Target.(ast.TypeDeclNode); ok {
		if t := tb.TypeBase().Ty; t != nil {
			return t
		}
	}
	panic(tr.Error(diag.ErrUnknownType, fmt.Sprintf("unknown type '%s'", tr.Name)))
}

func (c *Checker) declareMembers(d ast.Decl) {
	switch t := d.(type) {
	case *ast.TypeDecl:
		rt := t.Ty.(*ast.RecordTy)
		for _, f := range t.Fields {
			if rt.Field(f.Name) != nil {
				panic(f.Error(diag.ErrDuplicateDefinition, fmt.Sprintf("field '%s' is declared twice", f.Name)))
			}
			var ft ast.Ty = ast.VoidTy
			if f.Type != nil {
				ft = c.resolve(f.Type, rt)
			}
			rt.Fields = append(rt.Fields, &ast.FieldInfo{
				Name:     f.Name,
				Type:     ft,
				IsVar:    f.IsVar,
				Default:  f.Default,
				Index:    len(rt.Fields),
				Exported: f.Visibility == "export" || f.Visibility == "public",
			})
		}
	case *ast.EnumDecl:
		et := t.Ty.(*ast.EnumTy)
		next := ast.Zero
		for _, cs := range t.Cases {
			if et.CaseIndex(cs.Name) >= 0 {
				panic(cs.Error(diag.ErrEnumDuplicateCase, fmt.Sprintf("case '%s' is declared twice", cs.Name)))
			}
			raw := next
			if cs.Raw != nil {
				raw = cs.Raw.Value()
			}
			for _, r := range et.Raw {
				if r == raw {
					panic(cs.Error("E3031", fmt.Sprintf("raw value %s is used twice", raw.String())))
				}
			}
			et.HasExplicitRaw = et.HasExplicitRaw || cs.Raw != nil
			et.Cases = append(et.Cases, cs.Name)
			et.Raw = append(et.Raw, raw)
			next = raw.Add(ast.One)
		}
	case *ast.UnionDecl:
		ut := t.Ty.(*ast.UnionTy)
		for _, cs := range t.Cases {
			if ut.CaseIndex(cs.Name) >= 0 {
				panic(cs.Error(diag.ErrEnumDuplicateCase, fmt.Sprintf("case '%s' is declared twice", cs.Name)))
			}
			payload := make([]ast.PayloadField, 0, len(cs.Fields))
			for _, f := range cs.Fields {
				if f.Mutable {
					panic(f.Error(diag.ErrUnexpectedToken, "a union payload is not a parameter and takes no 'mutable'"))
				}
				payload = append(payload, ast.PayloadField{Name: f.Name, Type: c.resolve(f.Type, ut)})
			}
			ut.Cases = append(ut.Cases, &ast.UnionCase{Name: cs.Name, Payload: payload})
		}
	}
}

func (c *Checker) addMethods(methods []*ast.FuncDecl, nt ast.NominalTy, declared *[]*ast.FuncDecl) {
	for _, m := range methods {
		c.guard(func() {
			if _, isRecord := nt.(*ast.RecordTy); m.IsStatic && !isRecord {
				panic(m.Error("E2015", "a static function belongs in a 'type'"))
			}
			table := nt.MethodTable()
			if _, exists := table[m.Name]; exists {
				panic(m.Error(diag.ErrDuplicateDefinition, fmt.Sprintf("method '%s' is declared twice in '%s'", m.Name, nt.Name())))
			}
			table[m.Name] = m
			c.addFunction(m, nt, declared)
		})
	}
}

func (c *Checker) addFunction(f *ast.FuncDecl, nt ast.NominalTy, declared *[]*ast.FuncDecl) {
	if nt == nil && f.IsStatic {
		panic(f.Error("E2015", "a static function belongs in a 'type'"))
	}
	if f.Receiver != "" && (nt == nil || f.IsStatic) {
		panic(f.Error(diag.ErrUnexpectedToken, "only an instance method takes a receiver parameter"))
	}
	f.Owner = nt
	names := map[string]bool{}
	for _, prm := range f.Params {
		if prm.Name != "_" {
			if names[prm.Name] {
				panic(prm.Error(diag.ErrDuplicateDefinition, fmt.Sprintf("parameter '%s' is declared twice", prm.Name)))
			}
			names[prm.Name] = true
		}
		f.ParamTypes = append(f.ParamTypes, c.resolve(prm.Type, nt))
	}
	if f.Returns == nil {
		f.ReturnType = ast.VoidTy
	} else {
		f.ReturnType = c.resolve(f.Returns, nt)
	}
	specs := make([]ast.ParamSpec, len(f.Params))
	for i, prm := range f.Params {
		specs[i] = ast.ParamSpec{Name: prm.Name, Type: f.ParamTypes[i], ByRef: prm.Mutable}
	}
	f.Specs = specs
	*declared = append(*declared, f)
}

func (c *Checker) returnType(f *ast.FuncDecl) ast.Ty {
	if f.ReturnType == nil {
		return ast.VoidTy
	}
	return f.ReturnType
}

func (c *Checker) CheckMain() *ast.FuncDecl {
	var result *ast.FuncDecl
	c.guard(func() {
		main, ok := c.module.Names["main"].(*ast.FuncDecl)
		if !ok {
			panic(diag.NewError(diag.ErrNoMainFunction, "the project root has no 'main' function", c.module.Dir, 1, 1))
		}
		if len(main.Params) > 0 {
			panic(main.Error(diag.ErrMainWrongReturnType, "'main' takes no parameters"))
		}
		returnsExitCode := false
		if main.Returns != nil {
			if a, isAlias := main.Returns.Target.(*ast.AliasDecl); isAlias && a.Name == "ExitCode" {
				returnsExitCode = true
			}
		}
		if !returnsExitCode {
			panic(main.Error(diag.ErrMainWrongReturnType, "'main' must return ExitCode"))
		}
		result = main
	})
	return result
}

func (c *Checker) declareFieldDefaults(td *ast.TypeDecl) {
	rt := td.Ty.(*ast.RecordTy)
	for _, fi := range rt.Fields {
		if fi.Default == nil {
			continue
		}
		c.guard(func() { fi.Type = c.defaultType(fi, rt) })
	}
}

func (c *Checker) defaultType(fi *ast.FieldInfo, rt *ast.RecordTy) ast.Ty {
	var head ast.Expr
	switch d := fi.Default.(type) {
	case *ast.MemberExpr:
		head = d.Obj
	case *ast.CallExpr:
		if m, ok := d.Callee.(*ast.MemberExpr); ok {
			head = m.Obj
		}
	}
	if cast, ok := fi.Default.(*ast.CastExpr); ok {
		return c.resolve(cast.Target, rt)
	}
	if _, ok := fi.Default.(*ast.BoolLit); ok {
		return ast.BoolTy
	}
	if s, ok := fi.Default.(*ast.StrLit); ok && s.IsPlain() {
		return ast.StringTy
	}
	if n, ok := head.(*ast.NameExpr); ok {
		if n.Name == "Self" {
			return rt
		}
		if tb, isType := n.Target.(ast.TypeDeclNode); isType {
			if nt, isNominal := tb.TypeBase().Ty.(ast.NominalTy); isNominal {
				return nt
			}
		}
	}
	panic(fi.Default.P().Error(diag.ErrExpressionError,
		fmt.Sprintf("field '%s' needs a type: write 'var %s as <Type>', or a default that is a cast, a bool, a string, or a case or factory of a type", fi.Name, fi.Name)))
}

func (c *Checker) checkFieldDefaults(td *ast.TypeDecl) {
	rt := td.Ty.(*ast.RecordTy)
	for _, fi := range rt.Fields {
		if fi.Default == nil {
			continue
		}
		c.guard(func() {
			c.enterContext(nil, rt, true)
			c.checkExpr(fi.Default)
			c.coerce(fi.Default, fi.Type, fmt.Sprintf("the default of field '%s'", fi.Name))
		})
	}
}

func (c *Checker) CheckConst(cd *ast.ConstDecl) {
	c.guard(func() {
		c.enterContext(nil, nil, false)
		t := c.checkExpr(cd.Value)
		if it, ok := t.(*ast.IntTy); (ok && it.IsAnonymous()) || t == ast.VoidTy || t == ast.NeverTy {
			panic(cd.Error("E3220", fmt.Sprintf("constant '%s' must be a value known before the program runs", cd.Name)))
		}
		if s, ok := cd.Value.(*ast.StrLit); ok && t == ast.StringTy {
			for _, part := range s.Parts {
				if _, isExpr := part.(ast.Expr); isExpr {
					panic(cd.Error("E3220", "a constant string cannot interpolate"))
				}
			}
		}
		cd.Type = t
	})
}

func (c *Checker) enterContext(f *ast.FuncDecl, nt ast.NominalTy, calls bool) {
	c.fn = f
	c.owner = nt
	c.allowCalls = calls
	c.scopes = []map[string]*ast.Local{{}}
	c.blocks = nil
	c.nextSlot = 0
}

func (c *Checker) lookupLocal(name string) *ast.Local {
	for _, v := range slices.Backward(c.scopes) {
		if l, ok := v[name]; ok {
			return l
		}
	}
	return nil
}

func (c *Checker) selfRecord() *ast.RecordTy {
	if c.fn == nil || !c.fn.IsMethod() {
		return nil
	}
	rt, _ := c.owner.(*ast.RecordTy)
	return rt
}

func (c *Checker) pushScope() {
	c.scopes = append(c.scopes, map[string]*ast.Local{})
}

func (c *Checker) popScope() {
	c.scopes = c.scopes[:len(c.scopes)-1]
}

func (c *Checker) pushBlock(label string, kind checkerBlockKind, node ast.Node) {
	c.blocks = append(c.blocks, checkerBlock{Label: label, Kind: kind, Node: node})
}

func (c *Checker) popBlock() {
	c.blocks = c.blocks[:len(c.blocks)-1]
}

func (c *Checker) declareLocal(name string, t ast.Ty, isVar bool, site ast.Node, isParam, byRef bool, paramIndex int) *ast.Local {
	if rt := c.selfRecord(); rt != nil && rt.Field(name) != nil {
		panic(site.P().Error(diag.ErrDuplicateDefinition, fmt.Sprintf("'%s' shadows the field '%s' of '%s'", name, name, rt.Name())))
	}
	top := c.scopes[len(c.scopes)-1]
	if _, exists := top[name]; exists {
		panic(site.P().Error(diag.ErrDuplicateDefinition, fmt.Sprintf("'%s' is declared twice in this scope", name)))
	}
	local := ast.NewLocal(name, t, isVar)
	local.IsParam = isParam
	local.ByRef = byRef
	local.ParamIndex = paramIndex
	if !isParam {
		local.Slot = c.nextSlot
		c.nextSlot++
	}
	top[name] = local
	c.fn.Locals = append(c.fn.Locals, local)
	return local
}

func (c *Checker) checkFunction(f *ast.FuncDecl) {
	c.enterContext(f, f.Owner, true)
	index := 0
	if f.IsMethod() {
		self := ast.NewLocal("self", f.Owner, false)
		self.IsParam = true
		self.IsSelf = true
		self.ParamIndex = index
		index++
		f.SelfLocal = self
		f.Locals = append(f.Locals, self)
	}
	for i, prm := range f.Params {
		if prm.Name != "_" {
			prm.Sym = c.declareLocal(prm.Name, f.ParamTypes[i], false, prm, true, prm.Mutable, index)
		}
		index++
	}

	c.checkBlock(f.Body)
	if c.returnType(f) != ast.VoidTy && !terminatesBody(f.Body) {
		panic(f.Error(diag.ErrMissingReturn, fmt.Sprintf("'%s' must return a %s on every path", f.Name, c.returnType(f).Name())))
	}
}

func (c *Checker) checkBlock(body []ast.Stmt) {
	c.pushScope()
	for _, s := range body {
		c.guard(func() { c.checkStmt(s) })
	}
	c.popScope()
}

func terminatesBody(body []ast.Stmt) bool {
	return len(body) > 0 && ast.VisitStmtResult[bool](termination{}, body[len(body)-1])
}

// termination answers whether a statement never completes normally, so the code after it is unreachable.
type termination struct{}

func (termination) VisitReturnStmt(*ast.ReturnStmt) bool { return true }

func (termination) VisitExprStmt(t *ast.ExprStmt) bool {
	_, isPanic := t.E.(*ast.PanicExpr)
	return isPanic
}

func (termination) VisitIfStmt(t *ast.IfStmt) bool {
	return t.HasElse && terminatesBody(t.Then) && terminatesBody(t.Else)
}

func (termination) VisitMatchStmt(t *ast.MatchStmt) bool {
	for _, a := range t.Match.Arms {
		if !(a.Kind() == ast.ArmPanic || (a.Action != nil && ast.VisitStmtResult[bool](termination{}, a.Action))) {
			return false
		}
	}
	return true
}

func (termination) VisitLetStmt(*ast.LetStmt) bool { return false }

func (termination) VisitAssignStmt(*ast.AssignStmt) bool { return false }

func (termination) VisitDiscardStmt(*ast.DiscardStmt) bool { return false }

func (termination) VisitWhileStmt(*ast.WhileStmt) bool { return false }

func (termination) VisitForStmt(*ast.ForStmt) bool { return false }

func (termination) VisitBreakStmt(*ast.BreakStmt) bool { return false }

func (termination) VisitContinueStmt(*ast.ContinueStmt) bool { return false }

func (c *Checker) assignable(src, dst ast.Ty) bool {
	if src == dst || src == ast.NeverTy {
		return true
	}
	if di, ok := dst.(*ast.IntTy); ok {
		switch s := src.(type) {
		case *ast.ConstIntTy:
			return di.Holds(s.Value)
		case *ast.IntTy:
			return (s.IsAnonymous() || di.IsAnonymous()) && di.Covers(s)
		}
		return false
	}
	return false
}

func (c *Checker) coerce(e ast.Expr, target ast.Ty, what string) {
	src := e.Ty()
	if c.assignable(src, target) {
		return
	}
	if k, ok := src.(*ast.ConstIntTy); ok {
		if ti, isInt := target.(*ast.IntTy); isInt {
			panic(e.P().Error(diag.ErrTypeMismatch, fmt.Sprintf("%s: %s does not fit %s (%s to %s)", what, k.Value.String(), ti.Name(), ti.Min.String(), ti.Max.String())))
		}
	}
	if si, ok := src.(*ast.IntTy); ok && si.IsAnonymous() {
		if ta, isInt := target.(*ast.IntTy); isInt {
			panic(e.P().Error(diag.ErrTypeMismatch, fmt.Sprintf("%s: a value of %s may not fit %s; cast it with 'as %s'", what, src.Name(), ta.Name(), ta.Name())))
		}
	}
	panic(e.P().Error(diag.ErrTypeMismatch, fmt.Sprintf("%s: expected %s, found %s", what, target.Name(), src.Name())))
}

func (c *Checker) isInt(t ast.Ty) bool {
	switch t.(type) {
	case *ast.IntTy, *ast.ConstIntTy:
		return true
	}
	return false
}

func (c *Checker) rangeOf(t ast.Ty) (ast.I128, ast.I128) {
	switch r := t.(type) {
	case *ast.ConstIntTy:
		return r.Value, r.Value
	case *ast.IntTy:
		return r.Min, r.Max
	}
	panic(diag.Internal("%s is not an integer", t.Name()))
}

func (c *Checker) requireSigned64(site ast.Node, t ast.Ty) {
	if k, ok := t.(*ast.ConstIntTy); ok && k.Value.Gt(ast.I64Max) {
		panic(site.P().Error(diag.ErrIntegerLiteralRange,
			fmt.Sprintf("Integer literal '%s' is above i64.max, so it needs an alias whose range admits it: write '%s as <alias>'", k.Value.String(), k.Value.String())))
	}
}

func (c *Checker) namedInt(t ast.Ty) *ast.IntTy {
	if x, ok := t.(*ast.IntTy); ok && !x.IsAnonymous() {
		return x
	}
	return nil
}

func (c *Checker) unifyInts(site ast.Node, l, r ast.Ty) *ast.IntTy {
	la := c.namedInt(l)
	ra := c.namedInt(r)
	if la != nil && ra != nil && la != ra {
		panic(site.P().Error(diag.ErrTypeMismatch, fmt.Sprintf("the operands are %s and %s; cast one so both are the same type", la.Name(), ra.Name())))
	}
	alias := la
	if alias == nil {
		alias = ra
	}
	if alias == nil {
		c.requireSigned64(site, l)
		c.requireSigned64(site, r)
		return ast.Anonymous64
	}
	other := r
	if la == nil {
		other = l
	}
	if !c.assignable(other, alias) {
		panic(site.P().Error(diag.ErrTypeMismatch, fmt.Sprintf("an operand of %s does not fit %s; cast it with 'as %s'", other.Name(), alias.Name(), alias.Name())))
	}
	return alias
}

func (c *Checker) join(site ast.Node, a, b ast.Ty) ast.Ty {
	if a == ast.NeverTy {
		return b
	}
	if b == ast.NeverTy || a == b {
		return a
	}
	if c.isInt(a) && c.isInt(b) {
		if c.namedInt(a) != nil || c.namedInt(b) != nil {
			return c.unifyInts(site, a, b)
		}
		amin, amax := c.rangeOf(a)
		bmin, bmax := c.rangeOf(b)
		return &ast.IntTy{Min: ast.MinI(amin, bmin), Max: ast.MaxI(amax, bmax)}
	}
	panic(site.P().Error(diag.ErrTypeMismatch, fmt.Sprintf("the branches produce %s and %s", a.Name(), b.Name())))
}

func (c *Checker) checkExpr(e ast.Expr) ast.Ty {
	t := ast.VisitExprResult[ast.Ty](c, e)
	e.SetTy(t)
	return t
}

func (c *Checker) VisitIntLit(x *ast.IntLit) ast.Ty { return &ast.ConstIntTy{Value: x.Value()} }

func (c *Checker) VisitBoolLit(*ast.BoolLit) ast.Ty { return ast.BoolTy }

func (c *Checker) VisitMatchExpr(x *ast.MatchExpr) ast.Ty { return c.checkMatch(x.Match, true) }

func (c *Checker) VisitStrLit(s *ast.StrLit) ast.Ty {
	for _, p := range s.Parts {
		part, ok := p.(ast.Expr)
		if !ok {
			continue
		}
		t := c.checkExpr(part)
		c.requireSigned64(part, t)
		_, isEnum := t.(*ast.EnumTy)
		_, isUnion := t.(*ast.UnionTy)
		if !(c.isInt(t) || t == ast.StringTy || t == ast.BoolTy || isEnum || isUnion) {
			panic(part.P().Error(diag.ErrTypeMismatch, fmt.Sprintf("a value of %s cannot be interpolated", t.Name())))
		}
	}
	return ast.StringTy
}

func (c *Checker) VisitNameExpr(n *ast.NameExpr) ast.Ty {
	if local := c.lookupLocal(n.Name); local != nil {
		n.Kind = ast.NameLocal
		n.Local = local
		return local.Type
	}
	if rt := c.selfRecord(); rt != nil {
		if field := rt.Field(n.Name); field != nil {
			n.Kind = ast.NameSelfField
			n.Field = field
			return field.Type
		}
	}
	switch d := n.Target.(type) {
	case *ast.ConstDecl:
		n.Kind = ast.NameConst
		n.Const = d
		if d.Type == nil {
			panic(n.Error(diag.ErrExpressionError, fmt.Sprintf("constant '%s' has no value", d.Name)))
		}
		return d.Type
	case *ast.FuncDecl:
		panic(n.Error(diag.ErrUnsupported, "a function value is not supported by minimaxon; call the function"))
	case ast.TypeDeclNode:
		panic(n.Error(diag.ErrExpressionError, fmt.Sprintf("'%s' is a type, not a value", n.Name)))
	}
	if n.Name == "Self" {
		panic(n.Error(diag.ErrExpressionError, "'Self' is a type, not a value"))
	}
	panic(n.Error(diag.ErrExpressionError, fmt.Sprintf("undefined variable '%s'", n.Name)))
}

func (c *Checker) VisitSelfExpr(s *ast.SelfExpr) ast.Ty {
	if c.fn == nil || !c.fn.IsMethod() {
		panic(s.Error(diag.ErrUnexpectedToken, "'self' is used only inside an instance method"))
	}
	s.Local = c.fn.SelfLocal
	return c.owner
}

func (c *Checker) typeNamed(e ast.Expr) ast.NominalTy {
	n, ok := e.(*ast.NameExpr)
	if !ok {
		return nil
	}
	if n.Name == "Self" {
		if c.owner == nil {
			panic(n.Error(diag.ErrUnknownType, "'Self' names a type only inside one"))
		}
		return c.owner
	}
	if c.lookupLocal(n.Name) != nil {
		return nil
	}
	if rt := c.selfRecord(); rt != nil && rt.Field(n.Name) != nil {
		return nil
	}
	if tb, isType := n.Target.(ast.TypeDeclNode); isType {
		if nt, isNominal := tb.TypeBase().Ty.(ast.NominalTy); isNominal {
			return nt
		}
	}
	return nil
}

func (c *Checker) fold(site ast.Node, op string, a, b ast.I128) ast.I128 {
	var r ast.I128
	switch op {
	case "+":
		r = a.Add(b)
	case "-":
		r = a.Sub(b)
	case "*":
		product, ok := ast.FromBig(new(big.Int).Mul(a.Big(), b.Big()))
		if !ok {
			panic(site.P().Error(diag.ErrTypeMismatch, "the constant expression overflows 64 bits"))
		}
		r = product
	case "/", "mod":
		if b.IsZero() {
			panic(site.P().Error(diag.ErrDivisionByZero, "division by a constant zero"))
		}
		var q, m big.Int
		q.QuoRem(a.Big(), b.Big(), &m)
		var v *big.Int = &q
		if op == "mod" {
			v = &m
		}
		quotient, ok := ast.FromBig(v)
		if !ok {
			panic(site.P().Error(diag.ErrTypeMismatch, "the constant expression overflows 64 bits"))
		}
		r = quotient
	case "and":
		r = ast.I128{Hi: a.Hi & b.Hi, Lo: a.Lo & b.Lo}
	case "or":
		r = ast.I128{Hi: a.Hi | b.Hi, Lo: a.Lo | b.Lo}
	case "xor":
		r = ast.I128{Hi: a.Hi ^ b.Hi, Lo: a.Lo ^ b.Lo}
	case "shl", "shr":
		if b.IsNeg() {
			panic(site.P().Error("E2054", "a shift count cannot be negative"))
		}
		wide := b.Ge(ast.I64(checkerShiftLimit))
		switch {
		case op == "shl" && wide:
			r = ast.Zero
		case op == "shl":
			r = ast.U64(a.Lo << uint(b.Lo))
		case wide && a.IsNeg():
			r = ast.I64(-1)
		case wide:
			r = ast.Zero
		default:
			shifted, _ := ast.FromBig(new(big.Int).Rsh(a.Big(), uint(b.Lo)))
			r = shifted
		}
	default:
		panic(site.P().Error(diag.ErrUnsupported, fmt.Sprintf("cannot fold '%s'", op)))
	}
	if r.Lt(ast.I64Min) || r.Gt(ast.U64Max) {
		panic(site.P().Error(diag.ErrTypeMismatch, "the constant expression overflows 64 bits"))
	}
	return r
}

func (c *Checker) VisitBinaryExpr(b *ast.BinaryExpr) ast.Ty {
	l := c.checkExpr(b.L)
	r := c.checkExpr(b.R)

	if checkerArithmetic[b.Op] || (checkerLogical[b.Op] && c.isInt(l)) {
		if !c.isInt(l) || !c.isInt(r) {
			panic(b.Error(diag.ErrTypeMismatch, fmt.Sprintf("'%s' needs integers, found %s and %s", b.Op, l.Name(), r.Name())))
		}
		lc, lConst := l.(*ast.ConstIntTy)
		rc, rConst := r.(*ast.ConstIntTy)
		if lConst && rConst {
			b.OperandType = ast.Anonymous64
			return &ast.ConstIntTy{Value: c.fold(b, b.Op, lc.Value, rc.Value)}
		}
		if (b.Op == "/" || b.Op == "mod") && rConst && rc.Value.IsZero() {
			panic(b.Error(diag.ErrDivisionByZero, "division by a constant zero"))
		}
		if b.Op == "shl" || b.Op == "shr" {
			lt := ast.Anonymous64
			if !lConst {
				lt = l.(*ast.IntTy)
			}
			b.OperandType = lt
			return lt
		}
		t := c.unifyInts(b, l, r)
		b.OperandType = t
		return t
	}

	if checkerLogical[b.Op] {
		if l != ast.BoolTy || r != ast.BoolTy {
			panic(b.Error(diag.ErrTypeMismatch, fmt.Sprintf("'%s' needs two bools or two integers, found %s and %s", b.Op, l.Name(), r.Name())))
		}
		return ast.BoolTy
	}

	if checkerComparison[b.Op] {
		if c.isInt(l) && c.isInt(r) {
			b.OperandType = c.unifyInts(b, l, r)
			return ast.BoolTy
		}
		equality := b.Op == "==" || b.Op == "!="
		_, lUnion := l.(*ast.UnionTy)
		_, rUnion := r.(*ast.UnionTy)
		if lUnion || rUnion {
			panic(b.Error(diag.ErrEnumNotComparable, "a union cannot be compared; inspect it with 'match'"))
		}
		_, lEnum := l.(*ast.EnumTy)
		if equality && l == r && (l == ast.BoolTy || l == ast.StringTy || lEnum) {
			b.OperandType = l
			return ast.BoolTy
		}
		panic(b.Error(diag.ErrTypeMismatch, fmt.Sprintf("'%s' cannot compare %s and %s", b.Op, l.Name(), r.Name())))
	}

	panic(b.Error(diag.ErrUnsupported, fmt.Sprintf("unknown operator '%s'", b.Op)))
}

func (c *Checker) VisitUnaryExpr(u *ast.UnaryExpr) ast.Ty {
	t := c.checkExpr(u.E)
	if u.Op == "not" {
		if t == ast.BoolTy {
			return ast.BoolTy
		}
		switch x := t.(type) {
		case *ast.ConstIntTy:
			return &ast.ConstIntTy{Value: ast.I64(^int64(x.Value.Lo))}
		case *ast.IntTy:
			return x
		}
		panic(u.Error(diag.ErrTypeMismatch, fmt.Sprintf("'not' needs a bool or an integer, found %s", t.Name())))
	}
	switch x := t.(type) {
	case *ast.ConstIntTy:
		return &ast.ConstIntTy{Value: x.Value.Neg()}
	case *ast.IntTy:
		if x.Unsigned() {
			panic(u.Error(diag.ErrUnsignedNegation, fmt.Sprintf("cannot negate a value of the unsigned %s", t.Name())))
		}
		if x.IsAnonymous() {
			return ast.Anonymous64
		}
		return x
	}
	panic(u.Error(diag.ErrTypeMismatch, fmt.Sprintf("'-' needs an integer, found %s", t.Name())))
}

func (c *Checker) VisitCondExpr(cd *ast.CondExpr) ast.Ty {
	if c.checkExpr(cd.Cond) != ast.BoolTy {
		panic(cd.Cond.P().Error(diag.ErrTypeMismatch, fmt.Sprintf("a condition must be a bool, found %s", cd.Cond.Ty().Name())))
	}
	then := c.checkExpr(cd.Then)
	els := c.checkExpr(cd.Else)
	return c.join(cd, then, els)
}

func (c *Checker) VisitPanicExpr(p *ast.PanicExpr) ast.Ty {
	if c.checkExpr(p.Message) != ast.StringTy {
		panic(p.Message.P().Error(diag.ErrTypeMismatch, fmt.Sprintf("panic takes a String, found %s", p.Message.Ty().Name())))
	}
	return ast.NeverTy
}

func (c *Checker) bindArgs(call *ast.CallExpr, callee string, ps []ast.ParamSpec, argsChecked bool) []ast.Expr {
	if len(call.Args) != len(ps) {
		panic(call.Error(diag.ErrCallArgCountMismatch, fmt.Sprintf("'%s' takes %d argument(s), given %d", callee, len(ps), len(call.Args))))
	}
	ordered := make([]ast.Expr, len(ps))
	for i, arg := range call.Args {
		var slot int
		if i == 0 {
			if arg.Label != "" {
				panic(arg.Error(diag.ErrCallFirstArgNamed, "the first argument is positional: drop its label"))
			}
			slot = 0
		} else {
			if arg.Label == "" {
				panic(arg.Error(diag.ErrCallArgMissingLabel, fmt.Sprintf("argument %d of '%s' needs its label", i+1, callee)))
			}
			slot = -1
			for j, q := range ps {
				if q.Name == arg.Label {
					slot = j
					break
				}
			}
			if slot <= 0 {
				panic(arg.Error(diag.ErrCallUnknownArgLabel, fmt.Sprintf("'%s' has no parameter '%s' after its first", callee, arg.Label)))
			}
			if ordered[slot] != nil {
				panic(arg.Error(diag.ErrCallDuplicateArg, fmt.Sprintf("'%s' is given twice", arg.Label)))
			}
		}
		ordered[slot] = arg.Value
	}

	for i := range ps {
		value := ordered[i]
		if !argsChecked {
			c.checkExpr(value)
		}
		if ps[i].ByRef {
			c.checkMutableArg(value, ps[i])
		} else {
			c.coerce(value, ps[i].Type, fmt.Sprintf("argument '%s' of '%s'", ps[i].Name, callee))
		}
	}
	result := make([]ast.Expr, len(ordered))
	copy(result, ordered)
	return result
}

func (c *Checker) checkMutableArg(value ast.Expr, ps ast.ParamSpec) {
	writable := false
	switch v := value.(type) {
	case *ast.NameExpr:
		switch v.Kind {
		case ast.NameLocal:
			writable = v.Local.IsVar || v.Local.ByRef
		case ast.NameSelfField:
			writable = v.Field.IsVar
		}
	case *ast.MemberExpr:
		if v.Kind == ast.MemberField {
			writable = v.Field.IsVar
		}
	}
	if !writable {
		panic(value.P().Error(diag.ErrAssignToImmutable, fmt.Sprintf("the mutable parameter '%s' needs a var or a var field", ps.Name)))
	}
	if value.Ty() != ps.Type {
		panic(value.P().Error(diag.ErrTypeMismatch, fmt.Sprintf("the mutable parameter '%s' is %s; the variable is %s", ps.Name, ps.Type.Name(), value.Ty().Name())))
	}
}

func (c *Checker) callFunction(call *ast.CallExpr, kind ast.CallKind, target *ast.FuncDecl, receiver ast.Expr) ast.Ty {
	if target.Module != c.module && !target.IsExported() {
		panic(call.Error("E3008", fmt.Sprintf("'%s' is not exported from module %s", target.Symbol(), target.Module.Display())))
	}
	call.Kind = kind
	call.Target = target
	call.Receiver = receiver
	if target.Module == c.module {
		call.Ordered = c.bindArgs(call, target.Symbol(), target.Specs, false)
	} else {
		for _, a := range call.Args {
			c.checkExpr(a.Value)
		}
		c.Pending = append(c.Pending, ast.PendingCall{Call: call, Callee: target.Symbol(), Params: target.Specs})
	}
	return c.returnType(target)
}

func (c *Checker) VisitCallExpr(call *ast.CallExpr) ast.Ty {
	if !c.allowCalls {
		panic(call.Error("E3220", "a constant calls nothing"))
	}

	if n, ok := call.Callee.(*ast.NameExpr); ok && c.lookupLocal(n.Name) == nil {
		if n.Target == nil && (n.Name == "print" || n.Name == "printError") {
			if n.Name == "print" {
				call.Kind = ast.CallPrint
			} else {
				call.Kind = ast.CallPrintError
			}
			call.Ordered = c.bindArgs(call, n.Name, []ast.ParamSpec{{Name: "text", Type: ast.StringTy, ByRef: false}}, false)
			return ast.VoidTy
		}
		if f, isFunc := n.Target.(*ast.FuncDecl); isFunc {
			return c.callFunction(call, ast.CallFunction, f, nil)
		}
		panic(call.Error(diag.ErrUnknownFunction, fmt.Sprintf("unknown function '%s'", n.Name)))
	}

	if m, ok := call.Callee.(*ast.MemberExpr); ok {
		if nt := c.typeNamed(m.Obj); nt != nil {
			if ut, isUnion := nt.(*ast.UnionTy); isUnion {
				if ci := ut.CaseIndex(m.Name); ci >= 0 {
					uc := ut.Cases[ci]
					if len(uc.Payload) == 0 {
						panic(call.Error(diag.ErrEnumWrongBindingCount, fmt.Sprintf("case '%s' carries no payload: write '%s.%s'", uc.Name, ut.Name(), uc.Name)))
					}
					call.Kind = ast.CallUnionCtor
					call.Union = ut
					call.CaseIndex = ci
					specs := make([]ast.ParamSpec, len(uc.Payload))
					for i, p := range uc.Payload {
						specs[i] = ast.ParamSpec{Name: p.Name, Type: p.Type, ByRef: false}
					}
					call.Ordered = c.bindArgs(call, ut.Name()+"."+uc.Name, specs, false)
					return ut
				}
			}
			if sm, found := nt.MethodTable()[m.Name]; found && sm.IsStatic {
				return c.callFunction(call, ast.CallStatic, sm, nil)
			}
			panic(call.Error(diag.ErrUnknownFunction, fmt.Sprintf("'%s' has no static function or case '%s'", nt.Name(), m.Name)))
		}

		rt := c.checkExpr(m.Obj)
		if recv, isNominal := rt.(ast.NominalTy); isNominal {
			if method, found := recv.MethodTable()[m.Name]; found && !method.IsStatic {
				return c.callFunction(call, ast.CallMethod, method, m.Obj)
			}
		}
		panic(call.Error(diag.ErrUnknownFunction, fmt.Sprintf("%s has no method '%s'", rt.Name(), m.Name)))
	}

	panic(call.Error(diag.ErrUnsupported, "only a named function or method can be called"))
}

func (c *Checker) VisitMemberExpr(m *ast.MemberExpr) ast.Ty {
	if nt := c.typeNamed(m.Obj); nt != nil {
		switch t := nt.(type) {
		case *ast.EnumTy:
			m.CaseIndex = t.CaseIndex(m.Name)
			if m.CaseIndex < 0 {
				panic(m.Error(diag.ErrEnumUnknownCase, fmt.Sprintf("'%s' has no case '%s'", t.Name(), m.Name)))
			}
			m.Kind = ast.MemberEnumCase
			return t
		case *ast.UnionTy:
			m.CaseIndex = t.CaseIndex(m.Name)
			if m.CaseIndex < 0 {
				panic(m.Error(diag.ErrEnumUnknownCase, fmt.Sprintf("'%s' has no case '%s'", t.Name(), m.Name)))
			}
			if len(t.Cases[m.CaseIndex].Payload) > 0 {
				panic(m.Error(diag.ErrEnumWrongBindingCount, fmt.Sprintf("case '%s' carries a payload: construct it with arguments", m.Name)))
			}
			m.Kind = ast.MemberUnionCase
			return t
		}
		panic(m.Error(diag.ErrUnknownField, fmt.Sprintf("'%s.%s' is not a value", nt.Name(), m.Name)))
	}

	t := c.checkExpr(m.Obj)
	if rt, ok := t.(*ast.RecordTy); ok {
		if f := rt.Field(m.Name); f != nil {
			if rt.Decl.Module != c.module && !f.Exported {
				panic(m.Error("E3014", fmt.Sprintf("field '%s.%s' is not exported", rt.Name(), f.Name)))
			}
			m.Kind = ast.MemberField
			m.Field = f
			return f.Type
		}
	}
	if et, ok := t.(*ast.EnumTy); ok && (m.Name == "name" || m.Name == "ordinal" || m.Name == "rawValue") {
		switch m.Name {
		case "name":
			m.Kind = ast.MemberEnumName
			return ast.StringTy
		case "ordinal":
			m.Kind = ast.MemberEnumOrdinal
			return c.countTy()
		}
		m.Kind = ast.MemberEnumRaw
		for _, r := range et.Raw {
			if r.IsNeg() {
				return c.fullRangeTy()
			}
		}
		return c.countTy()
	}
	if _, ok := t.(*ast.UnionTy); ok && (m.Name == "name" || m.Name == "ordinal") {
		if m.Name == "name" {
			m.Kind = ast.MemberUnionName
			return ast.StringTy
		}
		m.Kind = ast.MemberUnionOrdinal
		return c.countTy()
	}
	if nom, ok := t.(ast.NominalTy); ok {
		if _, isMethod := nom.MethodTable()[m.Name]; isMethod {
			panic(m.Error(diag.ErrUnsupported, "a method value is not supported by minimaxon; call the method"))
		}
	}
	panic(m.Error(diag.ErrUnknownField, fmt.Sprintf("%s has no field '%s'", t.Name(), m.Name)))
}

func (c *Checker) VisitStructLitExpr(s *ast.StructLitExpr) ast.Ty {
	var target ast.Ty
	if s.TypeName == "Self" {
		if c.owner != nil {
			target = c.owner
		}
	} else if tb, ok := s.Target.(ast.TypeDeclNode); ok {
		target = tb.TypeBase().Ty
	}
	rt, ok := target.(*ast.RecordTy)
	if !ok {
		panic(s.Error(diag.ErrUnknownType, fmt.Sprintf("'%s' is not a type with fields", s.TypeName)))
	}
	if c.owner != rt || c.fn == nil {
		panic(s.Error(diag.ErrConstructorRestriction, fmt.Sprintf("a '%s' literal is written only inside the methods of '%s'; call a factory", rt.Name(), rt.Name())))
	}
	s.Record = rt
	s.Ordered = make([]ast.Expr, len(rt.Fields))
	for _, sf := range s.Fields {
		f := rt.Field(sf.Name)
		if f == nil {
			panic(sf.Error(diag.ErrUnknownField, fmt.Sprintf("'%s' has no field '%s'", rt.Name(), sf.Name)))
		}
		if s.Ordered[f.Index] != nil {
			panic(sf.Error(diag.ErrDuplicateDefinition, fmt.Sprintf("field '%s' is given twice", sf.Name)))
		}
		c.checkExpr(sf.Value)
		c.coerce(sf.Value, f.Type, fmt.Sprintf("field '%s'", sf.Name))
		s.Ordered[f.Index] = sf.Value
	}
	for _, f := range rt.Fields {
		if s.Ordered[f.Index] == nil && f.Default == nil {
			panic(s.Error(diag.ErrFieldNotInitialized, fmt.Sprintf("field '%s' of '%s' is not initialized", f.Name, rt.Name())))
		}
	}
	return rt
}

func (c *Checker) VisitCastExpr(cast *ast.CastExpr) ast.Ty {
	target := c.resolve(cast.Target, c.owner)
	ti, ok := target.(*ast.IntTy)
	if !ok {
		panic(cast.Error(diag.ErrUnsupported, fmt.Sprintf("a cast to %s is not supported by minimaxon", target.Name())))
	}
	src := c.checkExpr(cast.E)
	switch s := src.(type) {
	case *ast.ConstIntTy:
		if !ti.Holds(s.Value) {
			panic(cast.Error(diag.ErrTypeMismatch, fmt.Sprintf("%s does not fit %s (%s to %s)", s.Value.String(), ti.Name(), ti.Min.String(), ti.Max.String())))
		}
	case *ast.IntTy:
	default:
		panic(cast.Error(diag.ErrTypeMismatch, fmt.Sprintf("cannot cast %s to %s", src.Name(), ti.Name())))
	}
	return ti
}

func (c *Checker) checkStmt(s ast.Stmt) {
	ast.VisitStmt(c, s)
}

func (c *Checker) VisitDiscardStmt(t *ast.DiscardStmt) { c.checkExpr(t.Value) }

func (c *Checker) VisitExprStmt(t *ast.ExprStmt) {
	switch t.E.(type) {
	case *ast.CallExpr, *ast.PanicExpr:
	default:
		panic(t.Error(diag.ErrUnexpectedToken, "only a call can stand alone as a statement; bind the value with 'let', or discard it with '_ ='"))
	}
	c.checkExpr(t.E)
}

func (c *Checker) VisitIfStmt(t *ast.IfStmt) {
	c.requireBool(t.Cond)
	c.checkBlock(t.Then)
	if t.HasElse {
		c.checkBlock(t.Else)
	}
}

func (c *Checker) VisitWhileStmt(t *ast.WhileStmt) {
	c.requireBool(t.Cond)
	c.pushBlock(t.Label, checkerLoop, t)
	c.checkBlock(t.Body)
	c.popBlock()
}

func (c *Checker) VisitMatchStmt(t *ast.MatchStmt) { c.checkMatch(t.Match, false) }

func (c *Checker) VisitBreakStmt(t *ast.BreakStmt) { t.Target = c.resolveJump(t, t.Label, true) }

func (c *Checker) VisitContinueStmt(t *ast.ContinueStmt) { t.Target = c.resolveJump(t, t.Label, false) }

func (c *Checker) requireBool(e ast.Expr) {
	if c.checkExpr(e) != ast.BoolTy {
		panic(e.P().Error(diag.ErrTypeMismatch, fmt.Sprintf("a condition must be a bool, found %s", e.Ty().Name())))
	}
}

func (c *Checker) VisitLetStmt(l *ast.LetStmt) {
	if l.Name == "_" {
		panic(l.Error(diag.ErrUnexpectedToken, "write '_ = call()' to discard a result"))
	}
	t := c.checkExpr(l.Init)
	c.requireSigned64(l.Init, t)
	if t == ast.VoidTy || t == ast.NeverTy {
		panic(l.Init.P().Error(diag.ErrTypeMismatch, fmt.Sprintf("'%s' is bound to an expression with no value", l.Name)))
	}
	if l.IsVar {
		_, isConst := t.(*ast.ConstIntTy)
		it, isInt := t.(*ast.IntTy)
		if isConst || (isInt && it.IsAnonymous()) {
			panic(l.Init.P().Error(diag.ErrExpressionError, fmt.Sprintf("a var names its alias: write 'var %s = <value> as <Alias>'", l.Name)))
		}
	}
	l.Sym = c.declareLocal(l.Name, t, l.IsVar, l, false, false, -1)
}

func (c *Checker) VisitAssignStmt(a *ast.AssignStmt) {
	var target ast.Ty
	switch t := a.Target.(type) {
	case *ast.NameExpr:
		target = c.VisitNameExpr(t)
		switch {
		case t.Kind == ast.NameLocal && t.Local.IsParam && !t.Local.ByRef:
			panic(a.Error(diag.ErrAssignToImmutable, fmt.Sprintf("the parameter '%s' is a let; mark it 'mutable' to reassign it", t.Name)))
		case t.Kind == ast.NameLocal && !t.Local.IsVar && !t.Local.ByRef:
			panic(a.Error(diag.ErrAssignToImmutable, fmt.Sprintf("'%s' is a let and cannot be reassigned", t.Name)))
		case t.Kind == ast.NameSelfField && !t.Field.IsVar, t.Kind == ast.NameConst:
			panic(a.Error(diag.ErrAssignToImmutable, fmt.Sprintf("'%s' is a let and cannot be reassigned", t.Name)))
		}
	case *ast.MemberExpr:
		target = c.VisitMemberExpr(t)
		t.SetTy(target)
		if t.Kind != ast.MemberField {
			panic(a.Error(diag.ErrAssignToImmutable, fmt.Sprintf("'%s' cannot be assigned", t.Name)))
		}
		if !t.Field.IsVar {
			panic(a.Error(diag.ErrAssignToImmutable, fmt.Sprintf("field '%s' is a let and cannot be reassigned", t.Name)))
		}
	default:
		panic(a.Error(diag.ErrUnexpectedToken, "only a variable or a field can be assigned"))
	}
	c.checkExpr(a.Value)
	c.coerce(a.Value, target, "assignment")
}

func (c *Checker) VisitForStmt(f *ast.ForStmt) {
	from := c.checkExpr(f.From)
	to := c.checkExpr(f.To)
	if !c.isInt(from) || !c.isInt(to) {
		panic(f.Error(diag.ErrTypeMismatch, fmt.Sprintf("a range needs integer bounds, found %s and %s", from.Name(), to.Name())))
	}
	var varType ast.Ty
	if c.namedInt(from) != nil || c.namedInt(to) != nil {
		varType = c.unifyInts(f, from, to)
	} else {
		c.requireSigned64(f.From, from)
		c.requireSigned64(f.To, to)
		lo, _ := c.rangeOf(from)
		_, hi := c.rangeOf(to)
		if !f.Inclusive {
			hi = hi.Sub(ast.One)
		}
		varType = &ast.IntTy{Min: lo, Max: ast.MaxI(lo, hi)}
	}

	c.pushBlock(f.Label, checkerLoop, f)
	c.pushScope()
	f.Sym = c.declareLocal(f.Var, varType, false, f, false, false, -1)
	for _, st := range f.Body {
		c.guard(func() { c.checkStmt(st) })
	}
	c.popScope()
	c.popBlock()
}

func (c *Checker) VisitReturnStmt(r *ast.ReturnStmt) {
	ret := c.returnType(c.fn)
	if ret == ast.VoidTy {
		if r.Value != nil {
			panic(r.Error(diag.ErrTypeMismatch, fmt.Sprintf("'%s' returns nothing", c.fn.Name)))
		}
		return
	}
	if r.Value == nil {
		panic(r.Error(diag.ErrMissingReturn, fmt.Sprintf("'%s' must return a %s", c.fn.Name, ret.Name())))
	}
	c.checkExpr(r.Value)
	c.coerce(r.Value, ret, "return value")
}

func (c *Checker) resolveJump(s ast.Stmt, label string, isBreak bool) ast.Node {
	word := "continue"
	if isBreak {
		word = "break"
	}
	if label == "" {
		for _, v := range slices.Backward(c.blocks) {
			if isBreak || v.Kind == checkerLoop {
				return v.Node
			}
		}
		panic(s.P().Error(diag.ErrUnexpectedToken, fmt.Sprintf("'%s' outside a loop", word)))
	}

	for _, v := range slices.Backward(c.blocks) {
		if v.Label != label {
			continue
		}
		if !isBreak && v.Kind != checkerLoop {
			panic(s.P().Error(diag.ErrUnexpectedToken, fmt.Sprintf("'continue' names '%s', which is not a loop", label)))
		}
		return v.Node
	}
	panic(s.P().Error(diag.ErrUnexpectedToken, fmt.Sprintf("no enclosing loop or match is labelled '%s'", label)))
}

func (c *Checker) checkMatch(m *ast.MatchCore, isExpr bool) ast.Ty {
	st := c.checkExpr(m.Subject)
	c.requireSigned64(m.Subject, st)
	_, isEnum := st.(*ast.EnumTy)
	_, isUnion := st.(*ast.UnionTy)
	isCases := isEnum || isUnion
	if !(c.isInt(st) || st == ast.StringTy || st == ast.BoolTy || isCases) {
		panic(m.Subject.P().Error(diag.ErrTypeMismatch, fmt.Sprintf("cannot match on a value of %s", st.Name())))
	}

	c.pushBlock(m.Label, checkerMatch, m)
	var result ast.Ty
	hasDefault := false
	seen := map[int]bool{}
	seenBools := map[bool]bool{}

	for _, arm := range m.Arms {
		c.guard(func() {
			if arm.IsDefault {
				if isCases && arm.Kind() != ast.ArmPanic {
					panic(arm.Error(diag.ErrMatchDefaultEnumMustThrow, "on an enum or union, 'default' is 'default panic(...)'"))
				}
				hasDefault = true
			}

			c.pushScope()
			defer c.popScope()
			c.checkArm(arm, st, isExpr, seen, seenBools, &result)
		})
	}
	c.popBlock()
	c.checkExhaustive(m, st, hasDefault, seen, seenBools)
	if !isExpr {
		return ast.VoidTy
	}
	if result == nil {
		return ast.NeverTy
	}
	return result
}

func (c *Checker) checkArm(arm *ast.MatchArm, st ast.Ty, isExpr bool, seen map[int]bool, seenBools map[bool]bool, result *ast.Ty) {
	for _, pat := range arm.Patterns {
		c.checkPattern(pat, st, len(arm.Patterns), seen, seenBools)
	}

	switch arm.Kind() {
	case ast.ArmThen:
		if isExpr {
			panic(arm.Error(diag.ErrUnexpectedToken, "an arm of a match expression 'gives' a value"))
		}
		switch arm.Action.(type) {
		case *ast.IfStmt, *ast.WhileStmt, *ast.ForStmt, *ast.MatchStmt:
			panic(arm.Action.P().Error(diag.ErrMatchBlockStatement, "a block statement cannot be a match arm's action; move it into a function and call that"))
		}
		c.checkStmt(arm.Action)
	case ast.ArmGives:
		if !isExpr {
			panic(arm.Error(diag.ErrUnexpectedToken, "an arm of a match statement uses 'then'"))
		}
		t := c.checkExpr(arm.Value)
		if *result == nil {
			*result = t
		} else {
			*result = c.join(arm.Value, *result, t)
		}
	case ast.ArmPanic:
		c.checkExpr(arm.Value)
	}
}

func (c *Checker) checkExhaustive(m *ast.MatchCore, st ast.Ty, hasDefault bool, seen map[int]bool, seenBools map[bool]bool) {
	if hasDefault {
		return
	}
	if et, ok := st.(*ast.EnumTy); ok && len(seen) < len(et.Cases) {
		var missing []string
		for i, name := range et.Cases {
			if !seen[i] {
				missing = append(missing, name)
			}
		}
		panic(m.Error(diag.ErrMatchNotExhaustive, "the match is not exhaustive: missing "+strings.Join(missing, ", ")))
	}
	if ut, ok := st.(*ast.UnionTy); ok && len(seen) < len(ut.Cases) {
		var missing []string
		for i, cs := range ut.Cases {
			if !seen[i] {
				missing = append(missing, cs.Name)
			}
		}
		panic(m.Error(diag.ErrMatchNotExhaustive, "the match is not exhaustive: missing "+strings.Join(missing, ", ")))
	}
	if st == ast.BoolTy && len(seenBools) < 2 {
		panic(m.Error(diag.ErrMatchNotExhaustive, "the match is not exhaustive: it needs both true and false, or a default"))
	}
	if c.isInt(st) || st == ast.StringTy {
		panic(m.Error(diag.ErrMatchNotExhaustive, fmt.Sprintf("a match on %s needs a 'default' arm", st.Name())))
	}
}

func (c *Checker) checkPattern(pat *ast.Pattern, st ast.Ty, altCount int, seen map[int]bool, seenBools map[bool]bool) {
	switch pat.Kind() {
	case ast.PatternCase:
		switch t := st.(type) {
		case *ast.EnumTy:
			pat.CaseIndex = t.CaseIndex(pat.CaseName)
			if pat.CaseIndex < 0 {
				panic(pat.Error(diag.ErrEnumUnknownCase, fmt.Sprintf("'%s' has no case '%s'", t.Name(), pat.CaseName)))
			}
			if pat.HasBindings {
				panic(pat.Error(diag.ErrEnumWrongBindingCount, "an enum case carries no payload to bind"))
			}
		case *ast.UnionTy:
			pat.CaseIndex = t.CaseIndex(pat.CaseName)
			if pat.CaseIndex < 0 {
				panic(pat.Error(diag.ErrEnumUnknownCase, fmt.Sprintf("'%s' has no case '%s'", t.Name(), pat.CaseName)))
			}
			payload := t.Cases[pat.CaseIndex].Payload
			if pat.HasBindings {
				if len(pat.Bindings) != len(payload) {
					panic(pat.Error(diag.ErrEnumWrongBindingCount, fmt.Sprintf("case '%s' carries %d value(s), %d bound", pat.CaseName, len(payload), len(pat.Bindings))))
				}
				if altCount > 1 {
					panic(pat.Error(diag.ErrEnumWrongBindingCount, "an arm that binds a payload covers exactly one case"))
				}
				for i := range payload {
					var bound *ast.Local
					if pat.Bindings[i] != "_" {
						bound = c.declareLocal(pat.Bindings[i], payload[i].Type, false, pat, false, false, -1)
					}
					pat.BindingLocals = append(pat.BindingLocals, bound)
				}
			}
		default:
			panic(pat.Error(diag.ErrTypeMismatch, fmt.Sprintf("a case pattern needs an enum or union subject, found %s", st.Name())))
		}
		if seen[pat.CaseIndex] {
			panic(pat.Error(diag.ErrDuplicateDefinition, fmt.Sprintf("case '%s' is matched twice", pat.CaseName)))
		}
		seen[pat.CaseIndex] = true
	case ast.PatternLiteral:
		t := c.checkExpr(pat.Value)
		if bl, ok := pat.Value.(*ast.BoolLit); ok && st == ast.BoolTy {
			if seenBools[bl.Value] {
				word := "False"
				if bl.Value {
					word = "True"
				}
				panic(pat.Error(diag.ErrDuplicateDefinition, fmt.Sprintf("'%s' is matched twice", word)))
			}
			seenBools[bl.Value] = true
			return
		}
		if sl, ok := pat.Value.(*ast.StrLit); ok && st == ast.StringTy && sl.IsPlain() {
			return
		}
		if _, isConst := t.(*ast.ConstIntTy); isConst && c.isInt(st) {
			var expected ast.Ty = st
			if _, stConst := st.(*ast.ConstIntTy); stConst {
				expected = ast.Anonymous64
			}
			c.coerce(pat.Value, expected, "pattern")
			return
		}
		panic(pat.Error(diag.ErrTypeMismatch, fmt.Sprintf("a pattern of %s cannot match %s", t.Name(), st.Name())))
	case ast.PatternRange:
		lo := c.checkExpr(pat.Value)
		hi := c.checkExpr(pat.High)
		l, loConst := lo.(*ast.ConstIntTy)
		h, hiConst := hi.(*ast.ConstIntTy)
		if !c.isInt(st) || !loConst || !hiConst {
			panic(pat.Error(diag.ErrTypeMismatch, "a range pattern needs integer bounds and an integer subject"))
		}
		top := h.Value
		if !pat.Inclusive {
			top = top.Sub(ast.One)
		}
		if l.Value.Gt(top) {
			panic(pat.Error(diag.ErrTypeMismatch, "the range pattern is empty"))
		}
	}
}

func (c *Checker) checkCycle(t ast.NominalTy) {
	var path []string
	if c.reaches(t, t, &path, map[ast.Ty]bool{}) {
		var site ast.Node
		switch n := t.(type) {
		case *ast.RecordTy:
			site = n.Decl
		case *ast.UnionTy:
			site = n.Decl
		default:
			panic(diag.Internal("no declaration for %s", t.Name()))
		}
		panic(site.P().Error(diag.ErrTypeCycle, fmt.Sprintf("type '%s' contains a reference cycle (via %s)", t.Name(), strings.Join(path, " -> "))))
	}
}

type checkerEdge struct {
	Edge string
	Type ast.Ty
}

func (c *Checker) edges(t ast.Ty) []checkerEdge {
	var result []checkerEdge
	switch n := t.(type) {
	case *ast.RecordTy:
		for _, f := range n.Fields {
			result = append(result, checkerEdge{n.Name() + "." + f.Name, f.Type})
		}
	case *ast.UnionTy:
		for _, cs := range n.Cases {
			for _, p := range cs.Payload {
				result = append(result, checkerEdge{n.Name() + "." + cs.Name, p.Type})
			}
		}
	}
	return result
}

func (c *Checker) reaches(from, target ast.Ty, path *[]string, visited map[ast.Ty]bool) bool {
	for _, e := range c.edges(from) {
		*path = append(*path, e.Edge)
		if e.Type == target {
			return true
		}
		if !visited[e.Type] {
			visited[e.Type] = true
			if c.reaches(e.Type, target, path, visited) {
				return true
			}
		}
		*path = (*path)[:len(*path)-1]
	}
	return false
}
