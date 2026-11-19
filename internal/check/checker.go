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

// copyMethod is the name of the deep copy that every type has.
const copyMethod = "copy"

type Checker struct {
	project *ast.Project
	module  *ast.Module
	errs    *[]*diag.CompileError

	Pending []ast.PendingCall
	Bounds  []ast.PendingBound
	// Callers are the functions whose bodies need an instance of a generic, so a program without generics has none.
	Callers []*ast.FuncDecl

	fn         *ast.FuncDecl
	owner      ast.NominalTy
	allowCalls bool
	scopes     []map[string]*ast.Local
	blocks     []checkerBlock
	nextSlot   int
	uses       *[]*ast.CallExpr
	// root is the function being checked, which a behaviour in its body is part of: the calls in the body are recorded on
	// it, because its instances are what the body is lowered with.
	root *ast.FuncDecl
	// whens counts the behaviours in the function whose body is being checked, to name each one.
	whens int
	// behaviour is the behaviour whose body is being checked, or nil in an ordinary function; outers are the functions
	// that enclose it, outermost first.
	behaviour *behaviourBuild
	outers    []fnLevel
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
		c.guard(func() { c.checkCownCycle(t.Ty.(ast.NominalTy)) })
		c.guard(func() { c.checkConformance(t, t.Ty.(ast.NominalTy)) })
	case *ast.UnionDecl:
		c.guard(func() { c.checkCycle(t.Ty.(ast.NominalTy)) })
		c.guard(func() { c.checkCownCycle(t.Ty.(ast.NominalTy)) })
		c.guard(func() { c.checkConformance(t, t.Ty.(ast.NominalTy)) })
	case *ast.EnumDecl:
		c.guard(func() { c.checkConformance(t, t.Ty.(ast.NominalTy)) })
	default:
		panic(diag.Internal("no body to check in %T", d))
	}
}

func (c *Checker) Verify(calls []ast.PendingCall) {
	for _, p := range calls {
		c.guard(func() { p.Call.Ordered = c.bindArgs(p.Call, p.Callee, p.Params, true) })
	}
}

// VerifyBounds checks the type arguments that the module wrote or inferred against their parameters' bounds.
func (c *Checker) VerifyBounds(bounds []ast.PendingBound) {
	for _, b := range bounds {
		c.guard(func() {
			for _, iface := range b.Param.Interfaces {
				c.requireConforms(b.Site, b.Arg, iface, b.Param)
			}
		})
	}
}

// resolve is the type a type reference names. self is what 'Self' means where the reference is written.
func (c *Checker) resolve(tr *ast.TypeRef, self ast.Ty) ast.Ty {
	t := c.resolveNamed(tr, self)
	if _, isInterface := t.(*ast.InterfaceTy); isInterface {
		panic(tr.Error(diag.ErrInterfaceAsType, fmt.Sprintf("'%s' is an interface, which only bounds a type parameter: write a type parameter 'T is %s' and use T as the type", tr.Name, tr.Name)))
	}
	return t
}

func (c *Checker) resolveNamed(tr *ast.TypeRef, self ast.Ty) ast.Ty {
	if tr.Param != nil {
		c.requireNoTypeArgs(tr, tr.Name)
		return tr.Param.Ty
	}
	if len(tr.Qualifier) == 0 {
		switch tr.Name {
		case "bool":
			c.requireNoTypeArgs(tr, tr.Name)
			return ast.BoolTy
		case "String":
			c.requireNoTypeArgs(tr, tr.Name)
			return ast.StringTy
		case "Self":
			if self == nil {
				panic(tr.Error(diag.ErrUnknownType, "'Self' names a type only inside one"))
			}
			c.requireNoTypeArgs(tr, tr.Name)
			return self
		case "int", "float":
			panic(tr.Error(diag.ErrTypeMismatch, fmt.Sprintf("a bare '%s' needs a ranged typealias", tr.Name)))
		}
	}
	if tb, ok := tr.Target.(ast.TypeDeclNode); ok {
		if t := tb.TypeBase().Ty; t != nil {
			return c.withTypeArgs(tr, t, self)
		}
	}
	panic(tr.Error(diag.ErrUnknownType, fmt.Sprintf("unknown type '%s'", tr.Name)))
}

func (c *Checker) requireNoTypeArgs(tr *ast.TypeRef, name string) {
	if len(tr.TypeArgs.Items()) > 0 {
		panic(tr.Error(diag.ErrTypeArgumentCount, fmt.Sprintf("'%s' is not generic and takes no type arguments", name)))
	}
}

// withTypeArgs is the type a declaration names where a reference gives it type arguments: a generic declaration
// needs them and no other does.
func (c *Checker) withTypeArgs(tr *ast.TypeRef, t ast.Ty, self ast.Ty) ast.Ty {
	nt, isNominal := t.(ast.NominalTy)
	if !isNominal || !nt.Base().IsGeneric() {
		c.requireNoTypeArgs(tr, tr.Name)
		return t
	}
	return c.instantiate(tr, nt, c.resolveArgs(tr.TypeArgs, self))
}

// resolveArgs resolves a written list of type arguments.
func (c *Checker) resolveArgs(list *ast.TypeArgList, self ast.Ty) []ast.Ty {
	refs := list.Items()
	args := make([]ast.Ty, len(refs))
	for i, r := range refs {
		args[i] = c.resolve(r, self)
	}
	return args
}

// requireTypeArgCount refuses a list of type arguments of the wrong length.
func requireTypeArgCount(site ast.Node, name string, params []*ast.TypeParamTy, given int) {
	if given != len(params) {
		panic(site.P().Error(diag.ErrTypeArgumentCount, fmt.Sprintf("'%s' takes %d type argument(s), given %d: write '%s'",
			name, len(params), given, genericSpelling(name, params))))
	}
}

// instantiate is a generic type given its arguments. Whether they meet the parameters' bounds is checked in the
// verify phase.
func (c *Checker) instantiate(site ast.Node, nt ast.NominalTy, args []ast.Ty) ast.NominalTy {
	params := nt.Base().Params
	requireTypeArgCount(site, nt.Name(), params, len(args))
	if bt, ok := nt.(*ast.BuiltinTy); ok {
		switch bt.Kind {
		case ast.BuiltinMap:
			requireKey(site, args[0])
		case ast.BuiltinFrozen:
			requireFreezable(site, args[0])
		}
	}
	c.queueBounds(site, params, args)
	return ast.Instantiate(nt, args)
}

// requireKey refuses a map whose key is not compared and copied as a word or as text: a record or a union has no
// equality, and a type parameter is not known to have one.
func requireKey(site ast.Node, key ast.Ty) {
	if !ast.IsKey(key) {
		panic(site.P().Error(diag.ErrMapKey, fmt.Sprintf("'%s' cannot be the key of a map: a key is an integer alias, a bool, an enum, a String or a Handle", key.Name())))
	}
}

// callSpelling is how a call writes its type arguments, with a placeholder for each.
func callSpelling(name string, params []*ast.TypeParamTy) string {
	placeholders := make([]string, len(params))
	for i := range params {
		placeholders[i] = "<Type>"
	}
	return name + "[" + strings.Join(placeholders, ", ") + "]"
}

func genericSpelling(name string, params []*ast.TypeParamTy) string {
	names := make([]string, len(params))
	for i, p := range params {
		names[i] = p.Name()
	}
	return name + "[" + strings.Join(names, ", ") + "]"
}

func (c *Checker) queueBounds(site ast.Node, params []*ast.TypeParamTy, args []ast.Ty) {
	for i, p := range params {
		if p.Bounded() {
			c.Bounds = append(c.Bounds, ast.PendingBound{Site: site, Param: p, Arg: args[i]})
		}
	}
}

// resolveBound is the interface a bound or a conformance names.
func (c *Checker) resolveBound(tr *ast.TypeRef) *ast.InterfaceTy {
	if tr.Target == nil && tr.Param == nil {
		panic(tr.Error(diag.ErrUnknownType, fmt.Sprintf("unknown type '%s'", tr.Name)))
	}
	d, ok := tr.Target.(*ast.InterfaceDecl)
	if !ok {
		panic(tr.Error(diag.ErrGenericDeclaration, fmt.Sprintf("'%s' is not an interface, and only an interface can be a bound or be implemented", tr.Name)))
	}
	if len(tr.TypeArgs.Items()) > 0 {
		panic(tr.Error(diag.ErrTypeArgumentCount, fmt.Sprintf("the interface '%s' is not generic and takes no type arguments", tr.Name)))
	}
	return d.Ty.(*ast.InterfaceTy)
}

// declareTypeParams gives each type parameter its bounds. A bound is a pointer to an interface, so it needs nothing
// from the interface's own declaration.
func (c *Checker) declareTypeParams(params []*ast.TypeParam) {
	seen := map[string]bool{}
	for _, p := range params {
		if seen[p.Name] {
			panic(p.Error(diag.ErrDuplicateDefinition, fmt.Sprintf("type parameter '%s' is declared twice", p.Name)))
		}
		seen[p.Name] = true
		if p.Ty == nil {
			p.Ty = ast.NewTypeParamOf(p)
		}
		for _, b := range p.Bounds {
			p.Ty.Interfaces = append(p.Ty.Interfaces, c.resolveBound(b))
		}
	}
}

func (c *Checker) declareConforms(interfaces []*ast.TypeRef, nt ast.NominalTy) {
	base := nt.Base()
	for _, tr := range interfaces {
		base.Conforms = append(base.Conforms, c.resolveBound(tr))
	}
}

func (c *Checker) declareMembers(d ast.Decl) {
	switch t := d.(type) {
	case *ast.TypeDecl:
		rt := t.Ty.(*ast.RecordTy)
		c.declareTypeParams(t.TypeParams.Items())
		c.declareConforms(t.Conforms, rt)
		self := ast.SelfType(rt)
		for _, f := range t.Fields {
			if rt.Field(f.Name) != nil {
				panic(f.Error(diag.ErrDuplicateDefinition, fmt.Sprintf("field '%s' is declared twice", f.Name)))
			}
			var ft ast.Ty = ast.VoidTy
			if f.Type != nil {
				ft = c.resolve(f.Type, self)
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
		c.declareConforms(t.Conforms, et)
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
		c.declareTypeParams(t.TypeParams.Items())
		c.declareConforms(t.Conforms, ut)
		self := ast.SelfType(ut)
		for _, cs := range t.Cases {
			if ut.CaseIndex(cs.Name) >= 0 {
				panic(cs.Error(diag.ErrEnumDuplicateCase, fmt.Sprintf("case '%s' is declared twice", cs.Name)))
			}
			payload := make([]ast.PayloadField, 0, len(cs.Fields))
			for _, f := range cs.Fields {
				if f.Convention != "" {
					panic(f.Error(diag.ErrUnexpectedToken, fmt.Sprintf("a union payload is not a parameter and takes no '%s'", f.Convention)))
				}
				payload = append(payload, ast.PayloadField{Name: f.Name, Type: c.resolve(f.Type, self)})
			}
			ut.Cases = append(ut.Cases, &ast.UnionCase{Name: cs.Name, Payload: payload})
		}
	case *ast.InterfaceDecl:
		it := t.Ty.(*ast.InterfaceTy)
		for _, m := range t.Methods {
			c.guard(func() { c.declareSignature(it, m) })
		}
	}
}

func (c *Checker) declareSignature(it *ast.InterfaceTy, m *ast.FuncSig) {
	if m.Name == copyMethod {
		panic(m.Error(diag.ErrDuplicateDefinition, fmt.Sprintf("'%s' is built in for every type; name the method something else", copyMethod)))
	}
	if _, exists := it.Methods[m.Name]; exists {
		panic(m.Error(diag.ErrDuplicateDefinition, fmt.Sprintf("method '%s' is declared twice in '%s'", m.Name, it.Name())))
	}
	m.Pass.SelfConv = ast.ConventionOf(m.Receiver)
	_, m.Pass.Specs = c.paramSpecs(m.Params, it.Self)
	m.ReturnType = c.returnOf(m.Returns, it.Self)
	it.Methods[m.Name] = m
}

func (c *Checker) addMethods(methods []*ast.FuncDecl, nt ast.NominalTy, declared *[]*ast.FuncDecl) {
	for _, m := range methods {
		c.guard(func() {
			if _, isRecord := nt.(*ast.RecordTy); m.IsStatic && !isRecord {
				panic(m.Error("E2015", "a static function belongs in a 'type'"))
			}
			if m.Name == copyMethod {
				panic(m.Error(diag.ErrDuplicateDefinition, fmt.Sprintf("'%s' is built in for every type; name the method something else", copyMethod)))
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

// paramSpecs resolves a parameter list, which a function and an interface's method are both written with.
func (c *Checker) paramSpecs(params []*ast.Param, self ast.Ty) ([]ast.Ty, []ast.ParamSpec) {
	names := map[string]bool{}
	types := make([]ast.Ty, len(params))
	specs := make([]ast.ParamSpec, len(params))
	for i, prm := range params {
		if prm.Name == "_" && prm.Convention == "set" {
			panic(prm.Error(diag.ErrSetNotInitialized, "a set parameter must be named, so the function can initialise it"))
		}
		if prm.Name != "_" {
			if names[prm.Name] {
				panic(prm.Error(diag.ErrDuplicateDefinition, fmt.Sprintf("parameter '%s' is declared twice", prm.Name)))
			}
			names[prm.Name] = true
		}
		types[i] = c.resolve(prm.Type, self)
		specs[i] = ast.ParamSpec{Name: prm.Name, Type: types[i], Conv: ast.ConventionOf(prm.Convention)}
	}
	return types, specs
}

func (c *Checker) returnOf(returns *ast.TypeRef, self ast.Ty) ast.Ty {
	if returns == nil {
		return ast.VoidTy
	}
	return c.resolve(returns, self)
}

// declareGenerics gives a function its type parameters: its own, or for a method the ones of its type.
func (c *Checker) declareGenerics(f *ast.FuncDecl, nt ast.NominalTy) {
	switch {
	case len(f.TypeParams.Items()) > 0 && nt != nil:
		panic(f.Error(diag.ErrGenericDeclaration, fmt.Sprintf("the method '%s' takes no type parameters of its own: use the type's, or write a function", f.Name)))
	case nt != nil && nt.Base().IsGeneric():
		f.Generics = nt.Base().Params
	case len(f.TypeParams.Items()) > 0:
		c.declareTypeParams(f.TypeParams.Items())
		for _, p := range f.TypeParams.Items() {
			f.Generics = append(f.Generics, p.Ty)
		}
	default:
		return
	}
	f.Instances = &ast.InstanceTable{}
}

func (c *Checker) addFunction(f *ast.FuncDecl, nt ast.NominalTy, declared *[]*ast.FuncDecl) {
	if nt == nil && f.IsStatic {
		panic(f.Error("E2015", "a static function belongs in a 'type'"))
	}
	if f.Receiver != "" && (nt == nil || f.IsStatic) {
		panic(f.Error(diag.ErrUnexpectedToken, "only an instance method takes a receiver parameter"))
	}
	var self ast.Ty
	if nt != nil {
		f.Owner = ast.SelfType(nt)
		self = f.Owner
	}
	c.declareGenerics(f, nt)
	f.Pass.SelfConv = ast.ConventionOf(f.Receiver)
	f.ParamTypes, f.Pass.Specs = c.paramSpecs(f.Params, self)
	f.ReturnType = c.returnOf(f.Returns, self)
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
	self := ast.SelfType(rt)
	c.enterContext(nil, self, false)
	for _, fi := range rt.Fields {
		if fi.Default == nil {
			continue
		}
		c.guard(func() { fi.Type = c.defaultType(fi, self) })
	}
}

func (c *Checker) defaultType(fi *ast.FieldInfo, rt ast.Ty) ast.Ty {
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
		if nt := c.typeNamed(n); nt != nil {
			return c.requireInstance(n, nt)
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
			c.enterContext(nil, ast.SelfType(rt), true)
			c.uses = &fi.Uses
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
	c.root = f
	c.whens = 0
	c.behaviour = nil
	c.outers = nil
	c.owner = nt
	c.allowCalls = calls
	c.scopes = []map[string]*ast.Local{{}}
	c.blocks = nil
	c.nextSlot = 0
	c.uses = nil
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

func (c *Checker) declareLocal(name string, t ast.Ty, isVar bool, site ast.Node) *ast.Local {
	local := ast.NewLocal(name, t, isVar)
	c.bind(local, site)
	local.Slot = c.nextSlot
	c.nextSlot++
	return local
}

func (c *Checker) declareParam(prm *ast.Param, t ast.Ty, index int) *ast.Local {
	local := ast.NewLocal(prm.Name, t, false)
	local.IsParam = true
	local.Conv = ast.ConventionOf(prm.Convention)
	local.ParamIndex = index
	c.bind(local, prm)
	return local
}

func (c *Checker) bind(local *ast.Local, site ast.Node) {
	name := local.Name
	if rt := c.selfRecord(); rt != nil && rt.Field(name) != nil {
		panic(site.P().Error(diag.ErrDuplicateDefinition, fmt.Sprintf("'%s' shadows the field '%s' of '%s'", name, name, rt.Name())))
	}
	top := c.scopes[len(c.scopes)-1]
	if _, exists := top[name]; exists {
		panic(site.P().Error(diag.ErrDuplicateDefinition, fmt.Sprintf("'%s' is declared twice in this scope", name)))
	}
	top[name] = local
	c.fn.Locals = append(c.fn.Locals, local)
}

func (c *Checker) checkFunction(f *ast.FuncDecl) {
	c.enterContext(f, f.Owner, true)
	c.uses = &f.Uses
	index := 0
	if f.IsMethod() {
		self := ast.NewLocal("self", f.Owner, false)
		self.IsParam = true
		self.IsSelf = true
		self.Conv = f.Pass.SelfConv
		self.ParamIndex = index
		index++
		f.SelfLocal = self
		f.Locals = append(f.Locals, self)
	}
	for i, prm := range f.Params {
		if prm.Name != "_" {
			prm.Sym = c.declareParam(prm, f.ParamTypes[i], index)
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

func (termination) VisitWhenStmt(*ast.WhenStmt) bool { return false }

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
		t := ast.Content(c.checkExpr(part))
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
	if len(n.TypeArgs.Items()) > 0 && !c.namesType(n) {
		panic(n.Error(diag.ErrTypeArgumentCount, fmt.Sprintf("'%s' is not generic and takes no type arguments", n.Name)))
	}
	if local := c.useLocal(n); local != nil {
		n.Kind = ast.NameLocal
		n.Local = local
		return local.Type
	}
	if rt := c.selfRecord(); rt != nil {
		if field := rt.Field(n.Name); field != nil {
			n.Kind = ast.NameSelfField
			n.Field = field
			return rt.FieldType(field)
		}
	}
	if c.behaviour != nil && c.selfFieldInBehaviour(n.Name) {
		c.refuseBehaviourSelf(n, "the field '"+n.Name+"'")
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

// namesType is whether a name is a type rather than a variable, a field or a function, so it may take type arguments.
func (c *Checker) namesType(n *ast.NameExpr) bool {
	return n.Name == "Self" || (c.typeNamed(n) != nil)
}

func (c *Checker) VisitSelfExpr(s *ast.SelfExpr) ast.Ty {
	if c.behaviour != nil {
		c.refuseBehaviourSelf(s, "'self'")
	}
	if c.fn == nil || !c.fn.IsMethod() {
		panic(s.Error(diag.ErrUnexpectedToken, "'self' is used only inside an instance method"))
	}
	s.Local = c.fn.SelfLocal
	return c.owner
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
		if equality && l == r && (l == ast.BoolTy || l == ast.StringTy || lEnum || c.isHandle(l)) {
			b.OperandType = l
			return ast.BoolTy
		}
		panic(b.Error(diag.ErrTypeMismatch, fmt.Sprintf("'%s' cannot compare %s and %s", b.Op, l.Name(), r.Name())))
	}

	panic(b.Error(diag.ErrUnsupported, fmt.Sprintf("unknown operator '%s'", b.Op)))
}

// isHandle is whether t is a Handle, which is compared as a word.
func (c *Checker) isHandle(t ast.Ty) bool {
	bt, ok := t.(*ast.BuiltinTy)
	return ok && bt.Kind == ast.BuiltinHandle
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

// orderArgs puts a call's arguments in the order of the parameters, which labels name after the first.
func (c *Checker) orderArgs(call *ast.CallExpr, callee string, ps []ast.ParamSpec) []ast.Expr {
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
	return ordered
}

// checkedArgs orders a call's arguments and types them, before the parameters' types are known: a generic callee's
// type arguments come from them.
func (c *Checker) checkedArgs(call *ast.CallExpr, callee string, ps []ast.ParamSpec) []ast.Expr {
	ordered := c.orderArgs(call, callee, ps)
	for _, v := range ordered {
		c.checkExpr(v)
	}
	return ordered
}

func (c *Checker) bindArgs(call *ast.CallExpr, callee string, ps []ast.ParamSpec, argsChecked bool) []ast.Expr {
	ordered := c.orderArgs(call, callee, ps)
	for i := range ps {
		if !argsChecked {
			c.checkExpr(ordered[i])
		}
		c.coerceArg(ordered[i], callee, ps[i])
	}
	return ordered
}

func (c *Checker) coerceArg(value ast.Expr, callee string, ps ast.ParamSpec) {
	switch {
	case ps.Conv.ByAddress():
		c.checkSameType(value, ps)
	case ast.IsFrozen(value.Ty()) && ast.Content(value.Ty()) == ps.Type:
		// A let parameter borrows, and the contents of a frozen value can be borrowed: the callee reads them and
		// cannot change them. A parameter that changes or keeps them cannot have them.
		if ps.Conv != ast.ConvLet {
			panic(value.P().Error(diag.ErrFrozenChange, fmt.Sprintf("the %s parameter '%s' of '%s' would change or keep the contents of a frozen value, which are immutable and shared; pass an owned copy: 'thaw(...)'", ps.Conv, ps.Name, callee)))
		}
	default:
		c.coerce(value, ps.Type, fmt.Sprintf("argument '%s' of '%s'", ps.Name, callee))
	}
}

func (c *Checker) checkSameType(value ast.Expr, ps ast.ParamSpec) {
	if value.Ty() != ps.Type {
		panic(value.P().Error(diag.ErrTypeMismatch, fmt.Sprintf("the %s parameter '%s' is %s; the argument is %s", ps.Conv, ps.Name, ps.Type.Name(), value.Ty().Name())))
	}
}

// explicitTypeArgs are the type arguments a call writes after the function's name, or nil.
func (c *Checker) explicitTypeArgs(site ast.Node, target *ast.FuncDecl, list *ast.TypeArgList) []ast.Ty {
	if len(list.Items()) == 0 {
		return nil
	}
	if len(target.Generics) == 0 {
		panic(site.P().Error(diag.ErrTypeArgumentCount, fmt.Sprintf("'%s' is not generic and takes no type arguments", target.Symbol())))
	}
	requireTypeArgCount(site, target.Symbol(), target.Generics, len(list.Items()))
	return c.resolveArgs(list, c.owner)
}

// nameable is whether a type can be written as a type argument, so inference may choose it: an integer constant or
// the range of an expression names no alias.
func nameable(t ast.Ty) bool {
	switch x := t.(type) {
	case *ast.ConstIntTy:
		return false
	case *ast.IntTy:
		return !x.IsAnonymous()
	}
	return t != ast.VoidTy && t != ast.NeverTy
}

// infer chooses the type arguments of a call from its arguments' types.
func (c *Checker) infer(site ast.Node, callee string, params []*ast.TypeParamTy, specs []ast.ParamSpec, ordered []ast.Expr) []ast.Ty {
	args := make([]ast.Ty, len(params))
	for i, spec := range specs {
		unifyType(spec.Type, ordered[i].Ty(), params, args)
	}
	for i, a := range args {
		if a == nil {
			panic(site.P().Error(diag.ErrCannotInfer, fmt.Sprintf("the type argument '%s' of '%s' cannot be inferred from the arguments, which name no type for it; write the type arguments: '%s(...)'",
				params[i].Name(), callee, callSpelling(callee, params))))
		}
	}
	return args
}

func unifyType(param, arg ast.Ty, params []*ast.TypeParamTy, args []ast.Ty) {
	switch p := param.(type) {
	case *ast.TypeParamTy:
		if i := slices.Index(params, p); i >= 0 && args[i] == nil && nameable(arg) {
			args[i] = arg
		}
	case *ast.RecordTy:
		if a, ok := arg.(*ast.RecordTy); ok && p.Base().Declaration() == a.Base().Declaration() {
			unifyAll(p.Args, a.Args, params, args)
		}
	case *ast.UnionTy:
		if a, ok := arg.(*ast.UnionTy); ok && p.Base().Declaration() == a.Base().Declaration() {
			unifyAll(p.Args, a.Args, params, args)
		}
	case *ast.BuiltinTy:
		if a, ok := arg.(*ast.BuiltinTy); ok && p.Base().Declaration() == a.Base().Declaration() {
			unifyAll(p.Args, a.Args, params, args)
		}
	}
}

func unifyAll(param, arg []ast.Ty, params []*ast.TypeParamTy, args []ast.Ty) {
	for i := range param {
		unifyType(param[i], arg[i], params, args)
	}
}

// recordUses notes calls that need an instance of a generic, on the function or the field default being checked.
func (c *Checker) recordUses(calls ...*ast.CallExpr) {
	if c.uses == nil {
		panic(diag.Internal("check: a call is checked outside a function or a field default"))
	}
	if c.root != nil && len(*c.uses) == 0 && len(calls) > 0 {
		c.Callers = append(c.Callers, c.root)
	}
	*c.uses = append(*c.uses, calls...)
}

// callGeneric types a call whose callee has type parameters. given are the arguments already known, from the callee's
// own type or written at the call; without them they are inferred. checked is whether given came from a type
// whose arguments were already checked against their bounds.
func (c *Checker) callGeneric(call *ast.CallExpr, target *ast.FuncDecl, given []ast.Ty, checked bool) ast.Ty {
	callee := target.Symbol()
	ordered := c.checkedArgs(call, callee, target.Pass.Specs)
	if target.Intrinsic != ast.NotIntrinsic {
		c.checkIntrinsicArgs(call, target, ordered)
	}
	args := given
	if args == nil {
		args = c.infer(call, callee, target.Generics, target.Pass.Specs, ordered)
	}
	if !checked {
		c.queueBounds(call, target.Generics, args)
	}
	b := ast.Binding{Params: target.Generics, Args: args}
	call.Generic = &ast.GenericCall{
		SelfConv: target.Pass.SelfConv, Specs: b.ApplySpecs(target.Pass.Specs),
		TypeArgs: args,
	}
	call.Pass = &call.Generic.Passing
	call.Ordered = ordered
	if target.Intrinsic == ast.NotIntrinsic {
		c.recordUses(call)
	}
	if !c.declares(target) {
		c.Pending = append(c.Pending, ast.PendingCall{Call: call, Callee: callee, Params: call.Pass.Specs})
	} else {
		for i, spec := range call.Pass.Specs {
			c.coerceArg(ordered[i], callee, spec)
		}
	}
	return b.Apply(c.returnType(target))
}

// declares is whether a call of the function is checked now: its signature is known, because this module declares it
// or because it is built in. Another module may still be declaring its own, so a call into it waits for the verify
// phase.
func (c *Checker) declares(f *ast.FuncDecl) bool {
	return f.Module == c.module || f.Intrinsic != ast.NotIntrinsic
}

func (c *Checker) callFunction(call *ast.CallExpr, kind ast.CallKind, target *ast.FuncDecl, receiver ast.Expr, given []ast.Ty, checked bool) ast.Ty {
	if !c.declares(target) && !target.IsExported() {
		panic(call.Error("E3008", fmt.Sprintf("'%s' is not exported from module %s", target.Symbol(), target.Module.Display())))
	}
	call.Kind = kind
	call.Target = target
	call.Receiver = receiver
	call.Pass = &target.Pass
	if len(target.Generics) > 0 {
		return c.callGeneric(call, target, given, checked)
	}
	if c.declares(target) {
		call.Ordered = c.bindArgs(call, target.Symbol(), target.Pass.Specs, false)
	} else {
		for _, a := range call.Args {
			c.checkExpr(a.Value)
		}
		c.Pending = append(c.Pending, ast.PendingCall{Call: call, Callee: target.Symbol(), Params: target.Pass.Specs})
	}
	return c.returnType(target)
}

// typeNamed is the type that a name written as an expression names, or nil if it names no type. With type arguments it
// is an instance of a generic type. Without them a generic type names its declaration, which only a call that
// infers the arguments accepts; requireInstance refuses it anywhere else.
func (c *Checker) typeNamed(e ast.Expr) ast.NominalTy {
	n, ok := e.(*ast.NameExpr)
	if !ok {
		return nil
	}
	if n.Name == "Self" {
		if c.owner == nil {
			panic(n.Error(diag.ErrUnknownType, "'Self' names a type only inside one"))
		}
		if len(n.TypeArgs.Items()) > 0 {
			panic(n.Error(diag.ErrTypeArgumentCount, "'Self' is not generic and takes no type arguments"))
		}
		return c.owner
	}
	if c.lookupLocal(n.Name) != nil {
		return nil
	}
	if rt := c.selfRecord(); rt != nil && rt.Field(n.Name) != nil {
		return nil
	}
	tb, isType := n.Target.(ast.TypeDeclNode)
	if !isType {
		return nil
	}
	nt, isNominal := tb.TypeBase().Ty.(ast.NominalTy)
	if !isNominal {
		return nil
	}
	if !nt.Base().IsGeneric() {
		if len(n.TypeArgs.Items()) > 0 {
			panic(n.Error(diag.ErrTypeArgumentCount, fmt.Sprintf("'%s' is not generic and takes no type arguments", n.Name)))
		}
		return nt
	}
	if len(n.TypeArgs.Items()) == 0 {
		return nt
	}
	return c.instantiate(n, nt, c.resolveArgs(n.TypeArgs, c.owner))
}

// requireInstance refuses a generic declaration where a type is needed and no call can infer its arguments.
func (c *Checker) requireInstance(site ast.Node, nt ast.NominalTy) ast.NominalTy {
	if nt.Base().IsGeneric() {
		panic(site.P().Error(diag.ErrTypeArgumentCount, fmt.Sprintf("'%s' is generic: write its type arguments, as '%s'",
			nt.Name(), genericSpelling(nt.Name(), nt.Base().Params))))
	}
	return nt
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
			call.Ordered = c.bindArgs(call, n.Name, []ast.ParamSpec{{Name: "text", Type: ast.StringTy, Conv: ast.ConvLet}}, false)
			return ast.VoidTy
		}
		if f, isFunc := n.Target.(*ast.FuncDecl); isFunc {
			return c.callFunction(call, ast.CallFunction, f, nil, c.explicitTypeArgs(n, f, n.TypeArgs), false)
		}
		if td, isType := n.Target.(*ast.TypeDecl); isType && td.Ty == ast.Ty(c.project.Builtins.Cown) {
			// cown(x) is the constructor of the cown type.
			f := c.project.Builtins.CownNew
			return c.callFunction(call, ast.CallFunction, f, nil, c.explicitTypeArgs(n, f, n.TypeArgs), false)
		}
		panic(call.Error(diag.ErrUnknownFunction, fmt.Sprintf("unknown function '%s'", n.Name)))
	}

	if m, ok := call.Callee.(*ast.MemberExpr); ok {
		if nt := c.typeNamed(m.Obj); nt != nil {
			if ut, isUnion := nt.(*ast.UnionTy); isUnion {
				if ci := ut.CaseIndex(m.Name); ci >= 0 {
					return c.unionConstructor(call, ut, ci)
				}
			}
			if sm, found := nt.MethodTable()[m.Name]; found && sm.IsStatic {
				given := nt.Base().Args
				return c.callFunction(call, ast.CallStatic, sm, nil, given, len(given) > 0)
			}
			panic(call.Error(diag.ErrUnknownFunction, fmt.Sprintf("'%s' has no static function or case '%s'", nt.Name(), m.Name)))
		}

		rt := c.checkExpr(m.Obj)
		if m.Name == copyMethod {
			return c.copyCall(call, m.Obj, rt)
		}
		content := ast.Content(rt)
		requireNotCown(call, content, "calling", m.Name)
		if param, isParam := content.(*ast.TypeParamTy); isParam {
			return c.boundCall(call, m, param, rt)
		}
		if recv, isNominal := content.(ast.NominalTy); isNominal {
			if method, found := recv.MethodTable()[m.Name]; found && !method.IsStatic {
				requireNotFrozenChange(call, rt, method.Pass.SelfConv, m.Name)
				return c.callFunction(call, ast.CallMethod, method, m.Obj, recv.Base().Args, true)
			}
		}
		panic(call.Error(diag.ErrUnknownFunction, fmt.Sprintf("%s has no method '%s'", rt.Name(), m.Name)))
	}

	panic(call.Error(diag.ErrUnsupported, "only a named function or method can be called"))
}

// unionConstructor types 'Union.case(payload)'. A generic union written without type arguments takes them from the
// payload.
func (c *Checker) unionConstructor(call *ast.CallExpr, ut *ast.UnionTy, caseIndex int) ast.Ty {
	uc := ut.CaseList()[caseIndex]
	if len(uc.Payload) == 0 {
		panic(call.Error(diag.ErrEnumWrongBindingCount, fmt.Sprintf("case '%s' carries no payload: write '%s.%s'", uc.Name, ut.Name(), uc.Name)))
	}
	call.Kind = ast.CallUnionCtor
	call.CaseIndex = caseIndex
	specsOf := func(u *ast.UnionTy) []ast.ParamSpec {
		payload := u.Payload(caseIndex)
		specs := make([]ast.ParamSpec, len(payload))
		for i, p := range payload {
			specs[i] = ast.ParamSpec{Name: p.Name, Type: p.Type, Conv: ast.ConvSink}
		}
		return specs
	}
	specs := specsOf(ut)
	callee := ut.Name() + "." + uc.Name
	if ut.Base().IsGeneric() {
		ordered := c.checkedArgs(call, callee, specs)
		ut = c.instantiate(call, ut, c.infer(call, ut.Name(), ut.Base().Params, specs, ordered)).(*ast.UnionTy)
		specs = specsOf(ut)
		for i, spec := range specs {
			c.coerceArg(ordered[i], callee, spec)
		}
		call.Ordered = ordered
	} else {
		call.Ordered = c.bindArgs(call, callee, specs, false)
	}
	call.Union = ut
	call.Pass = &ast.Passing{Specs: specs}
	return ut
}

// boundCall types a call of a method that a type parameter's bounds declare. It is the only method a value of a type
// parameter has, and it runs the method of whatever type the parameter becomes.
func (c *Checker) boundCall(call *ast.CallExpr, m *ast.MemberExpr, param *ast.TypeParamTy, receiver ast.Ty) ast.Ty {
	var iface *ast.InterfaceTy
	var sig *ast.FuncSig
	for _, candidate := range param.Interfaces {
		s, found := candidate.Methods[m.Name]
		if !found || candidate == iface {
			continue
		}
		if sig != nil {
			panic(call.Error(diag.ErrAmbiguousBound, fmt.Sprintf("the bounds '%s' and '%s' of '%s' both declare '%s'; keep only one bound that declares it",
				iface.Name(), candidate.Name(), param.Name(), m.Name)))
		}
		iface, sig = candidate, s
	}
	if sig == nil {
		panic(call.Error(diag.ErrUnknownFunction, fmt.Sprintf("%s has no method '%s'%s", param.Name(), m.Name, boundsText(param))))
	}
	requireNotFrozenChange(call, receiver, sig.Pass.SelfConv, m.Name)
	b := ast.Binding{Params: []*ast.TypeParamTy{iface.Self}, Args: []ast.Ty{param}}
	call.Kind = ast.CallBound
	call.Receiver = m.Obj
	call.Generic = &ast.GenericCall{
		SelfConv: sig.Pass.SelfConv, Specs: b.ApplySpecs(sig.Pass.Specs),
		Method: sig,
	}
	call.Pass = &call.Generic.Passing
	call.Ordered = c.bindArgs(call, param.Name()+"."+m.Name, call.Pass.Specs, false)
	c.recordUses(call)
	return b.Apply(sig.ReturnType)
}

// boundsText says what a type parameter can call, for the error that names a method it cannot.
func boundsText(param *ast.TypeParamTy) string {
	if len(param.Interfaces) == 0 {
		return fmt.Sprintf(": a type parameter has only the methods of its bounds, and '%s' has none; write '%s is <Interface>'", param.Name(), param.Name())
	}
	names := make([]string, len(param.Interfaces))
	for i, iface := range param.Interfaces {
		names[i] = iface.Name()
	}
	return fmt.Sprintf(": a type parameter has only the methods of its bounds (%s)", strings.Join(names, ", "))
}

// requireConforms checks one type argument against one bound of its parameter.
func (c *Checker) requireConforms(site ast.Node, arg ast.Ty, iface *ast.InterfaceTy, param *ast.TypeParamTy) {
	switch t := arg.(type) {
	case *ast.TypeParamTy:
		if slices.Contains(t.Interfaces, iface) {
			return
		}
		panic(site.P().Error(diag.ErrBoundNotMet, fmt.Sprintf("the type parameter '%s' is not known to be %s, which the type argument for '%s' must be: add the bound, '%s is %s'",
			t.Name(), iface.Name(), param.Name(), t.Name(), iface.Name())))
	case ast.NominalTy:
		if slices.Contains(t.Base().Declaration().Conforms, iface) {
			return
		}
		if c.project.Builtins.Is(t) {
			panic(site.P().Error(diag.ErrBoundNotMet, fmt.Sprintf("'%s' is built in and implements no interface, so it cannot be %s for '%s'; wrap it in a type of your own that declares 'is %s'",
				t.Name(), iface.Name(), param.Name(), c.spelled(iface))))
		}
		panic(site.P().Error(diag.ErrBoundNotMet, fmt.Sprintf("'%s' must be %s for '%s': declare '%s %s is %s'%s",
			t.Name(), iface.Name(), param.Name(), declarationWord(t), t.Base().Declaration().Name(), c.spelled(iface), missingMethods(t, iface))))
	}
	panic(site.P().Error(diag.ErrBoundNotMet, fmt.Sprintf("'%s' must be %s for '%s', but only a type, enum or union implements an interface",
		arg.Name(), iface.Name(), param.Name())))
}

// spelled is how code in this module names an interface: qualified when another module declares it.
func (c *Checker) spelled(iface *ast.InterfaceTy) string {
	if m := iface.Decl.Module; m != c.module && m.Path != "" {
		return m.Path + "." + iface.Name()
	}
	return iface.Name()
}

func declarationWord(t ast.NominalTy) string {
	switch t.(type) {
	case *ast.RecordTy:
		return "type"
	case *ast.EnumTy:
		return "enum"
	case *ast.UnionTy:
		return "union"
	}
	panic(diag.Internal("%s is not declared as a type, enum or union", t.Name()))
}

// missingMethods names the methods of the interface that the type lacks, for the error that asks it to declare one.
func missingMethods(t ast.NominalTy, iface *ast.InterfaceTy) string {
	var missing []string
	for _, sig := range iface.Decl.Methods {
		if m, found := t.MethodTable()[sig.Name]; !found || m.IsStatic {
			missing = append(missing, sig.Name)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return " and add the method(s) " + strings.Join(missing, ", ")
}

// checkConformance checks a declaration against each interface it says it implements: every method is there with the
// signature the interface gives, so a bound that the declaration meets is never wrong at an instance.
func (c *Checker) checkConformance(site ast.Node, nt ast.NominalTy) {
	self := ast.SelfType(nt)
	for _, iface := range nt.Base().Conforms {
		b := ast.Binding{Params: []*ast.TypeParamTy{iface.Self}, Args: []ast.Ty{self}}
		for _, sig := range iface.Decl.Methods {
			c.requireImplements(site, nt, iface, sig, b)
		}
	}
}

func (c *Checker) requireImplements(site ast.Node, nt ast.NominalTy, iface *ast.InterfaceTy, sig *ast.FuncSig, b ast.Binding) {
	wantSpecs, wantReturn := b.ApplySpecs(sig.Pass.Specs), b.Apply(sig.ReturnType)
	m, found := nt.MethodTable()[sig.Name]
	if !found || m.IsStatic || m.Pass.SelfConv != sig.Pass.SelfConv || !slices.Equal(m.Pass.Specs, wantSpecs) || m.ReturnType != wantReturn {
		problem := "does not have it"
		if found {
			problem = "has it as '" + signatureText(m.Name, m.Pass.SelfConv, m.Pass.Specs, m.ReturnType) + "'"
		}
		panic(site.P().Error(diag.ErrConformance, fmt.Sprintf("'%s' says it is %s, which needs '%s', but '%s' %s: add or change the method to match it",
			nt.Name(), iface.Name(), signatureText(sig.Name, sig.Pass.SelfConv, wantSpecs, wantReturn), nt.Name(), problem)))
	}
}

// signatureText is a method as it is written in a declaration.
func signatureText(name string, selfConv ast.Convention, specs []ast.ParamSpec, ret ast.Ty) string {
	var parts []string
	if selfConv != ast.ConvLet {
		parts = append(parts, selfConv.String()+" self")
	}
	for _, s := range specs {
		word := ""
		if s.Conv != ast.ConvLet {
			word = s.Conv.String() + " "
		}
		parts = append(parts, s.Name+" "+word+s.Type.Name())
	}
	text := "function " + name + "(" + strings.Join(parts, ", ") + ")"
	if ret != ast.Ty(ast.VoidTy) {
		text += " returns " + ret.Name()
	}
	return text
}

func (c *Checker) copyCall(call *ast.CallExpr, receiver ast.Expr, t ast.Ty) ast.Ty {
	if t == ast.VoidTy || t == ast.NeverTy {
		panic(call.Error(diag.ErrTypeMismatch, "an expression with no value cannot be copied"))
	}
	call.Kind = ast.CallCopy
	call.Receiver = receiver
	call.Ordered = c.bindArgs(call, copyMethod, nil, false)
	return t
}

func (c *Checker) VisitMemberExpr(m *ast.MemberExpr) ast.Ty {
	if nt := c.typeNamed(m.Obj); nt != nil {
		c.requireInstance(m.Obj, nt)
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
			if len(t.CaseList()[m.CaseIndex].Payload) > 0 {
				panic(m.Error(diag.ErrEnumWrongBindingCount, fmt.Sprintf("case '%s' carries a payload: construct it with arguments", m.Name)))
			}
			m.Kind = ast.MemberUnionCase
			return t
		}
		panic(m.Error(diag.ErrUnknownField, fmt.Sprintf("'%s.%s' is not a value", nt.Name(), m.Name)))
	}

	t := c.checkExpr(m.Obj)
	requireNotCown(m, ast.Content(t), "reading", m.Name)
	if rt, ok := ast.Content(t).(*ast.RecordTy); ok {
		if f := rt.Field(m.Name); f != nil {
			if rt.Decl.Module != c.module && !f.Exported {
				panic(m.Error("E3014", fmt.Sprintf("field '%s.%s' is not exported", rt.Name(), f.Name)))
			}
			m.Kind = ast.MemberField
			m.Field = f
			return rt.FieldType(f)
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

// VisitIndexExpr types a subscript. An element of a primitive is a projection, never a value of its own: what it is
// used for decides whether it borrows the element to read it or to change it, and the ownership pass refuses to move
// one out.
func (c *Checker) VisitIndexExpr(x *ast.IndexExpr) ast.Ty {
	t := ast.Content(c.checkExpr(x.Obj))
	requireNotCown(x, t, "reading an element", "")
	bt, ok := t.(*ast.BuiltinTy)
	if !ok || !bt.Subscriptable() {
		panic(x.Error(diag.ErrTypeMismatch, fmt.Sprintf("%s has no elements to index: only an array, a map, a list or a pool does", t.Name())))
	}
	if len(x.Args) != 1 {
		panic(x.Error(diag.ErrTypeMismatch, fmt.Sprintf("an index is one value, given %d", len(x.Args))))
	}
	c.checkExpr(x.Args[0])
	c.coerce(x.Args[0], bt.Index(), "the index of "+t.Name())
	return bt.Element()
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
	if rt.Base().IsGeneric() {
		panic(s.Error(diag.ErrTypeArgumentCount, fmt.Sprintf("a literal of the generic '%s' is written 'Self{...}', which gives it its own type parameters", rt.Name())))
	}
	if c.owner != rt || c.fn == nil {
		panic(s.Error(diag.ErrConstructorRestriction, fmt.Sprintf("a '%s' literal is written only inside the methods of '%s'; call a factory", rt.Name(), rt.Name())))
	}
	fields := rt.FieldList()
	s.Record = rt
	s.Ordered = make([]ast.Expr, len(fields))
	for _, sf := range s.Fields {
		f := rt.Field(sf.Name)
		if f == nil {
			panic(sf.Error(diag.ErrUnknownField, fmt.Sprintf("'%s' has no field '%s'", rt.Name(), sf.Name)))
		}
		if s.Ordered[f.Index] != nil {
			panic(sf.Error(diag.ErrDuplicateDefinition, fmt.Sprintf("field '%s' is given twice", sf.Name)))
		}
		c.checkExpr(sf.Value)
		c.coerce(sf.Value, rt.FieldType(f), fmt.Sprintf("field '%s'", sf.Name))
		s.Ordered[f.Index] = sf.Value
	}
	for _, f := range fields {
		if s.Ordered[f.Index] == nil {
			if f.Default == nil {
				panic(s.Error(diag.ErrFieldNotInitialized, fmt.Sprintf("field '%s' of '%s' is not initialized", f.Name, rt.Name())))
			}
			c.recordUses(f.Uses...)
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
	if l.Init == nil {
		t := c.resolve(l.Type, c.owner)
		l.Sym = c.declareLocal(l.Name, t, l.IsVar, l)
		return
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
	l.Sym = c.declareLocal(l.Name, t, l.IsVar, l)
}

func (c *Checker) VisitAssignStmt(a *ast.AssignStmt) {
	var target ast.Ty
	switch t := a.Target.(type) {
	case *ast.NameExpr:
		target = c.VisitNameExpr(t)
		t.SetTy(target)
	case *ast.MemberExpr:
		target = c.VisitMemberExpr(t)
		t.SetTy(target)
		if t.Kind != ast.MemberField {
			panic(a.Error(diag.ErrAssignToImmutable, fmt.Sprintf("'%s' cannot be assigned", t.Name)))
		}
	case *ast.IndexExpr:
		target = c.VisitIndexExpr(t)
		t.SetTy(target)
	default:
		panic(a.Error(diag.ErrUnexpectedToken, "only a variable, a field or an element can be assigned"))
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
	f.Sym = c.declareLocal(f.Var, varType, false, f)
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
			if c.behaviour != nil {
				panic(r.Error(diag.ErrTypeMismatch, "a behaviour returns nothing and nothing waits for it: pass a result on by scheduling another behaviour"))
			}
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
	st := ast.Content(c.checkExpr(m.Subject))
	requireNotCown(m.Subject, st, "matching on it", "")
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
		case *ast.IfStmt, *ast.WhileStmt, *ast.ForStmt, *ast.MatchStmt, *ast.WhenStmt:
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
	if ut, ok := st.(*ast.UnionTy); ok && len(seen) < len(ut.CaseList()) {
		var missing []string
		for i, cs := range ut.CaseList() {
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
			payload := t.Payload(pat.CaseIndex)
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
						bound = c.declareLocal(pat.Bindings[i], payload[i].Type, false, pat)
						bound.Projected = payload[i].Type.IsManaged()
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
	self := ast.SelfType(t)
	var site ast.Node
	switch n := t.(type) {
	case *ast.RecordTy:
		site = n.Decl
	case *ast.UnionTy:
		site = n.Decl
	default:
		panic(diag.Internal("no declaration for %s", t.Name()))
	}
	growing := false
	if !c.reaches(self, self, &path, map[ast.Ty]bool{}, &growing) {
		return
	}
	if growing {
		panic(site.P().Error(diag.ErrTypeCycle, fmt.Sprintf("type '%s' has no finite size: via %s it contains itself with a larger type argument each time",
			t.Name(), path[0])))
	}
	panic(site.P().Error(diag.ErrTypeCycle, fmt.Sprintf("type '%s' contains a reference cycle (via %s)", t.Name(), strings.Join(path, " -> "))))
}

type checkerEdge struct {
	Edge string
	Type ast.Ty
}

func (c *Checker) edges(t ast.Ty) []checkerEdge {
	var result []checkerEdge
	switch n := t.(type) {
	case *ast.RecordTy:
		for _, f := range n.FieldList() {
			result = append(result, checkerEdge{n.Name() + "." + f.Name, n.FieldType(f)})
		}
	case *ast.UnionTy:
		for i, cs := range n.CaseList() {
			for _, p := range n.Payload(i) {
				result = append(result, checkerEdge{n.Name() + "." + cs.Name, p.Type})
			}
		}
	}
	return result
}

// reaches is whether a value of type from contains one of type target. A type that contains itself with a larger type
// argument each time never repeats, so it is found by how deeply its instances nest, and sets growing.
func (c *Checker) reaches(from, target ast.Ty, path *[]string, visited map[ast.Ty]bool, growing *bool) bool {
	for _, e := range c.edges(from) {
		*path = append(*path, e.Edge)
		if e.Type == target {
			return true
		}
		if ast.TypeDepth(e.Type) > ast.MaxTypeDepth {
			*growing = true
			return true
		}
		if !visited[e.Type] {
			visited[e.Type] = true
			if c.reaches(e.Type, target, path, visited, growing) {
				return true
			}
		}
		*path = (*path)[:len(*path)-1]
	}
	return false
}
