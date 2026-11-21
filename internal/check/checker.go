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
	Node  ast.NodeID
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
	// resolution is what name resolution bound the module's nodes to; the checker only reads it.
	resolution *ast.Resolution
	// typing is where the checker records what it decides about the module's nodes, and what ownership finds.
	typing *ast.Typing
	errs   *[]*diag.CompileError

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
	uses       *[]ast.CallUse
	// omitted is the argument 'dropWith:' of a cown call, which names a destructor and is no value, so the ordering of that
	// call's arguments leaves it out.
	omitted struct {
		call ast.CallExpr
		arg  ast.Arg
	}
	// dead is whether the statement being checked comes after its block's live statements.
	dead bool
	// expect is the type the context of the expression being checked needs, or nil where it needs none in particular:
	// a call on a bare generic type takes from it the type arguments its own arguments do not fix.
	expect ast.Ty
	// root is the function being checked, which a behaviour in its body is part of: the calls in the body are recorded on
	// it, because its instances are what the body is lowered with.
	root *ast.FuncDecl
	// whens counts the behaviours in the function whose body is being checked, to name each one.
	whens int
	// behaviour is the behaviour whose body is being checked, or nil in an ordinary function; outers are the functions
	// that enclose it, outermost first.
	behaviour *behaviourBuild
	outers    []fnLevel
	// bounded is the function being checked when its signature bounds its type's parameters beyond what the type does, or
	// nil. Those bounds hold in its body and nowhere else, so what the checker knows about a type parameter, bounds and
	// capabilities alike, depends on it.
	bounded *ast.FuncDecl
	// caps remembers which types have which capabilities.
	caps capabilities
}

func NewChecker(p *ast.Project, m *ast.Module, resolution *ast.Resolution, typing *ast.Typing, errs *[]*diag.CompileError) *Checker {
	return &Checker{project: p, module: m, resolution: resolution, typing: typing, errs: errs}
}

// ty is the type the checker gave an expression.
func (c *Checker) ty(e ast.Expr) ast.Ty { return c.typing.Type(e) }

// target is the declaration that a node of the module names, or nil if it names none.
func (c *Checker) target(n ast.NodeID) ast.Decl { return c.resolution.Decl(c.project, n) }

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

// DeclareAlias gives a typealias of an instance its type, the instance, once every typealias it names has its own.
func (c *Checker) DeclareAlias(a *ast.AliasDecl) {
	c.guard(func() {
		if a.Instance.TypeArgs().Len() == 0 {
			panic(a.Instance.Error(diag.ErrAliasTarget, fmt.Sprintf("a typealias names an integer range, as 'int(0 to 9)', or an instance of a generic type, as 'Box with Count'; write '%s' itself where '%s' is used",
				a.Instance.Name(), a.Name)))
		}
		a.Ty = c.resolve(a.Instance, nil)
	})
}

func (c *Checker) CheckBody(d ast.Decl) {
	switch t := d.(type) {
	case *ast.FuncDecl:
		c.guard(func() { c.checkFunction(t) })
	case *ast.TypeDecl:
		c.checkFieldDefaults(t)
		c.guard(func() { c.checkCycle(t.Ty.(ast.NominalTy)) })
		c.guard(func() { c.checkCownCycle(t.Ty.(ast.NominalTy)) })
		c.guard(func() { c.checkConformance(t.Position(), t.Ty.(ast.NominalTy)) })
	case *ast.UnionDecl:
		c.guard(func() { c.checkCycle(t.Ty.(ast.NominalTy)) })
		c.guard(func() { c.checkCownCycle(t.Ty.(ast.NominalTy)) })
		c.guard(func() { c.checkConformance(t.Position(), t.Ty.(ast.NominalTy)) })
	case *ast.EnumDecl:
		c.guard(func() { c.checkConformance(t.Position(), t.Ty.(ast.NominalTy)) })
	default:
		panic(diag.Internal("no body to check in %T", d))
	}
}

func (c *Checker) Verify(calls []ast.PendingCall) {
	for _, p := range calls {
		c.guard(func() {
			ordered := c.bindArgs(p.Call, p.Callee, p.Params, true)
			c.typing.Call(p.Call).Ordered = ordered
		})
	}
}

// VerifyFlow follows the values of a module's declarations and functions, once its calls are verified: the ranges of the
// integers, which decide what can fail and must be caught, then ownership, which follows the failures to their handlers.
// Each runs only on a module that has no error yet.
//
// scratch is what a worker reuses from one module to the next.
func (c *Checker) VerifyFlow(decls []ast.Decl, functions []*ast.FuncDecl, scratch *FlowScratch) {
	if len(*c.errs) == 0 {
		ranges := newRanges(c.project, c.typing, c.errs, &scratch.states)
		for _, d := range decls {
			ranges.CheckValues(d)
		}
		for _, f := range functions {
			ranges.Check(f)
		}
	}
	if len(*c.errs) == 0 {
		ownership := NewOwnership(c.errs, c)
		for _, d := range decls {
			ownership.CheckValues(d)
		}
		for _, f := range functions {
			ownership.Check(f)
		}
	}
}

// VerifyBounds checks the type arguments that the module's instances and inferred calls give against their parameters'
// bounds.
func (c *Checker) VerifyBounds(bounds []ast.PendingBound) {
	for _, b := range bounds {
		c.guard(func() {
			c.bounded = b.Within
			interfaces := b.Param.Interfaces
			if b.Method != nil {
				interfaces = b.Method.BoundsOn(b.Param)
			}
			for _, iface := range interfaces {
				c.requireConforms(b.Site, b.Arg, iface, b.Param)
			}
		})
	}
}

// resolve is the type a type reference names. self is what 'Self' means where the reference is written.
func (c *Checker) resolve(tr ast.TypeRef, self ast.Ty) ast.Ty {
	t := c.resolveNamed(tr, self)
	if _, isInterface := t.(*ast.InterfaceTy); isInterface {
		panic(tr.Error(diag.ErrInterfaceAsType, fmt.Sprintf("'%s' is an interface, which only bounds a type parameter: declare a type parameter with 'uses T where T implements %s' and use T as the type", tr.Name(), tr.Name())))
	}
	return t
}

func (c *Checker) resolveNamed(tr ast.TypeRef, self ast.Ty) ast.Ty {
	if param := c.resolution.TypeParam(tr.ID); param != nil {
		c.requireNoTypeArgs(tr, tr.Name())
		return param.Ty
	}
	if tr.Qualifier().Len() == 0 {
		switch tr.Name() {
		case "bool":
			c.requireNoTypeArgs(tr, tr.Name())
			return ast.BoolTy
		case "String":
			c.requireNoTypeArgs(tr, tr.Name())
			return ast.StringTy
		case "Self":
			if self == nil {
				panic(tr.Error(diag.ErrUnknownType, "'Self' names a type only inside one"))
			}
			c.requireNoTypeArgs(tr, tr.Name())
			return self
		case "int", "float":
			panic(tr.Error(diag.ErrTypeMismatch, fmt.Sprintf("a bare '%s' needs a ranged typealias", tr.Name())))
		}
	}
	if tb, ok := c.target(tr.ID).(ast.TypeDeclNode); ok {
		if t := tb.TypeBase().Ty; t != nil {
			return c.withTypeArgs(tr, t, self)
		}
	}
	panic(tr.Error(diag.ErrUnknownType, fmt.Sprintf("unknown type '%s'", tr.Name())))
}

func (c *Checker) requireNoTypeArgs(tr ast.TypeRef, name string) {
	if tr.TypeArgs().Len() > 0 {
		panic(tr.Error(diag.ErrTypeArgumentCount, fmt.Sprintf("'%s' is not generic and takes no type arguments", name)))
	}
}

// withTypeArgs is the type a declaration names where a reference gives it type arguments: a generic declaration
// needs them and no other does.
func (c *Checker) withTypeArgs(tr ast.TypeRef, t ast.Ty, self ast.Ty) ast.Ty {
	nt, isNominal := t.(ast.NominalTy)
	if !isNominal || !nt.Base().IsGeneric() {
		c.requireNoTypeArgs(tr, tr.Name())
		return t
	}
	return c.instantiate(tr.Position(), nt, c.resolveArgs(tr.TypeArgs(), self))
}

// resolveArgs resolves a written list of type arguments.
func (c *Checker) resolveArgs(refs ast.TypeRefList, self ast.Ty) []ast.Ty {
	args := make([]ast.Ty, refs.Len())
	for i, r := range refs.All() {
		args[i] = c.resolve(r, self)
	}
	return args
}

// requireTypeArgCount refuses a list of type arguments of the wrong length.
func requireTypeArgCount(site ast.Pos, name string, params []*ast.TypeParamTy, given int) {
	if given != len(params) {
		panic(site.Position().Error(diag.ErrTypeArgumentCount, fmt.Sprintf("'%s' takes %d type argument(s), given %d: write '%s'",
			name, len(params), given, ast.InstanceSpelling(name, paramNames(params)))))
	}
}

// instantiate is a generic type given its arguments. Whether they meet the parameters' bounds is checked in the
// verify stage.
func (c *Checker) instantiate(site ast.Pos, nt ast.NominalTy, args []ast.Ty) ast.NominalTy {
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
func requireKey(site ast.Pos, key ast.Ty) {
	if !ast.IsKey(key) {
		panic(site.Position().Error(diag.ErrMapKey, fmt.Sprintf("'%s' cannot be the key of a map: a key is an integer alias, a bool, an enum, a String or a Handle", key.Name())))
	}
}

func paramNames(params []*ast.TypeParamTy) []string {
	names := make([]string, len(params))
	for i, p := range params {
		names[i] = p.Name()
	}
	return names
}

func (c *Checker) queueBounds(site ast.Pos, params []*ast.TypeParamTy, args []ast.Ty) {
	for i, p := range params {
		if p.Bounded() {
			c.Bounds = append(c.Bounds, ast.PendingBound{Site: site.Position(), Param: p, Arg: args[i], Within: c.bounded})
		}
	}
}

// queueMethodBounds queues the check of a call's type arguments against the bounds that the method adds to its type's
// parameters. Which parameters those are is known only once the method's module has declared it, so each parameter is
// queued and the verify stage looks at what the method says about it.
func (c *Checker) queueMethodBounds(site ast.Pos, method *ast.FuncDecl, args []ast.Ty) {
	if !method.HasOwnBounds() {
		return
	}
	for i, p := range method.Generics {
		c.Bounds = append(c.Bounds, ast.PendingBound{Site: site.Position(), Param: p, Arg: args[i], Method: method, Within: c.bounded})
	}
}

// enterBounds makes the function's own bounds on its type's parameters the ones that hold.
func (c *Checker) enterBounds(f *ast.FuncDecl) {
	c.bounded = nil
	if f != nil && len(f.Bounds) > 0 {
		c.bounded = f
	}
}

// boundsOf are the bounds a type parameter has in the code being checked: its own, and those the function adds.
func (c *Checker) boundsOf(p *ast.TypeParamTy) []*ast.InterfaceTy {
	if c.bounded == nil {
		return p.Interfaces
	}
	extra := c.bounded.BoundsOn(p)
	if len(extra) == 0 {
		return p.Interfaces
	}
	return append(slices.Clip(p.Interfaces), extra...)
}

// resolveBound is the interface a bound or a conformance names.
func (c *Checker) resolveBound(tr ast.TypeRef) *ast.InterfaceTy {
	target := c.target(tr.ID)
	if target == nil && c.resolution.TypeParam(tr.ID) == nil {
		panic(tr.Error(diag.ErrUnknownType, fmt.Sprintf("unknown type '%s'", tr.Name())))
	}
	d, ok := target.(*ast.InterfaceDecl)
	if !ok {
		panic(tr.Error(diag.ErrGenericDeclaration, fmt.Sprintf("'%s' is not an interface, and only an interface can be a bound or be implemented", tr.Name())))
	}
	if tr.TypeArgs().Len() > 0 {
		panic(tr.Error(diag.ErrTypeArgumentCount, fmt.Sprintf("the interface '%s' is not generic and takes no type arguments", tr.Name())))
	}
	return d.Ty.(*ast.InterfaceTy)
}

// declareTypeParams gives each type parameter the bounds its declaration's constraints name. A bound is a pointer to an
// interface, so it needs nothing from the interface's own declaration.
func (c *Checker) declareTypeParams(params []*ast.TypeParam, where []*ast.Constraint) {
	seen := map[string]bool{}
	for _, p := range params {
		if seen[p.Name] {
			panic(p.Error(diag.ErrDuplicateDefinition, fmt.Sprintf("type parameter '%s' is declared twice", p.Name)))
		}
		seen[p.Name] = true
		if p.Ty == nil {
			p.Ty = ast.NewTypeParamOf(p, where)
		}
	}
	for _, cs := range where {
		for _, b := range cs.Bounds.All() {
			constrained := c.resolution.TypeParam(cs.ID).Ty
			constrained.Interfaces = append(constrained.Interfaces, c.resolveBound(b))
		}
	}
}

func (c *Checker) declareConforms(interfaces ast.TypeRefList, nt ast.NominalTy) {
	base := nt.Base()
	for _, tr := range interfaces.All() {
		iface := c.resolveBound(tr)
		if c.project.Builtins.IsCapability(iface) {
			c.guard(func() { panic(c.structuralCapability(tr.Position(), iface)) })
			continue
		}
		base.Conforms = append(base.Conforms, iface)
	}
}

func (c *Checker) declareMembers(d ast.Decl) {
	switch t := d.(type) {
	case *ast.TypeDecl:
		rt := t.Ty.(*ast.RecordTy)
		c.declareTypeParams(t.TypeParams, t.Where)
		c.declareConforms(t.Conforms, rt)
		self := ast.SelfType(rt)
		for _, f := range t.Fields {
			if rt.Field(f.Name) != nil {
				panic(f.Error(diag.ErrDuplicateDefinition, fmt.Sprintf("field '%s' is declared twice", f.Name)))
			}
			var ft ast.Ty = ast.VoidTy
			if !f.Type.IsNil() {
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
			if !cs.Raw.IsNil() {
				raw = cs.Raw.Value()
			}
			for _, r := range et.Raw {
				if r == raw {
					panic(cs.Error("E3031", fmt.Sprintf("raw value %s is used twice", raw.String())))
				}
			}
			et.HasExplicitRaw = et.HasExplicitRaw || !cs.Raw.IsNil()
			et.Cases = append(et.Cases, cs.Name)
			et.Raw = append(et.Raw, raw)
			next = raw.Add(ast.One)
		}
	case *ast.UnionDecl:
		ut := t.Ty.(*ast.UnionTy)
		c.declareTypeParams(t.TypeParams, t.Where)
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
	m.ErrorType = c.errorOf(m.Throws, m.Pass.Specs)
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
		if prm.Name == "_" && ast.ConventionOf(prm.Convention) == ast.ConvSets {
			panic(prm.Error(diag.ErrSetNotInitialized, "a 'sets' parameter must be named, so the function can initialise it"))
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

func (c *Checker) returnOf(returns ast.TypeRef, self ast.Ty) ast.Ty {
	if returns.IsNil() {
		return ast.VoidTy
	}
	return c.resolve(returns, self)
}

// errorOf is the error a function's 'throws' names, or nil for a function that throws nothing. ArithmeticError is the one
// error there is.
func (c *Checker) errorOf(throws ast.TypeRef, specs []ast.ParamSpec) ast.Ty {
	if throws.IsNil() {
		return nil
	}
	t := c.resolve(throws, nil)
	if t != ast.Ty(c.project.Builtins.ArithmeticError) {
		panic(throws.Error(diag.ErrThrows, fmt.Sprintf("a function throws ArithmeticError, the error of an integer operation, not %s: write 'throws ArithmeticError'", t.Name())))
	}
	for _, spec := range specs {
		if spec.Conv == ast.ConvSets {
			panic(throws.Error(diag.ErrThrows, fmt.Sprintf("a failure would leave the 'sets' parameter '%s' set or not, which its caller cannot tell: return the value instead, or catch the failures in the function", spec.Name)))
		}
	}
	return t
}

// declareGenerics gives a function its type parameters: its own, or for a method the ones of its type, which its
// 'where' may bound further.
func (c *Checker) declareGenerics(f *ast.FuncDecl, nt ast.NominalTy) {
	switch {
	case len(f.TypeParams) > 0 && nt != nil:
		panic(f.Error(diag.ErrGenericDeclaration, fmt.Sprintf("the method '%s' takes no type parameters of its own: use the type's, or write a function", f.Name)))
	case nt != nil && nt.Base().IsGeneric():
		f.Generics = nt.Base().Params
		for _, cs := range f.Where {
			for _, b := range cs.Bounds.All() {
				f.Bounds = append(f.Bounds, ast.ParamBound{Param: c.resolution.TypeParam(cs.ID).Ty, Iface: c.resolveBound(b)})
			}
		}
	case len(f.TypeParams) > 0:
		c.declareTypeParams(f.TypeParams, f.Where)
		for _, p := range f.TypeParams {
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
	f.SetLinkName()
	c.declareGenerics(f, nt)
	if _, isEnum := nt.(*ast.EnumTy); isEnum && f.IsDestructor() {
		panic(f.Error(diag.ErrDestructorMisuse, fmt.Sprintf("'%s' is an enum, whose values are plain numbers with nothing to end; remove 'destructs', or make '%s' a type or a union", nt.Name(), nt.Name())))
	}
	f.Pass.SelfConv = ast.ConventionOf(f.Receiver)
	f.ParamTypes, f.Pass.Specs = c.paramSpecs(f.Params, self)
	f.ReturnType = c.returnOf(f.Returns, self)
	f.ErrorType = c.errorOf(f.Throws, f.Pass.Specs)
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
		if !main.Returns.IsNil() {
			if a, isAlias := c.target(main.Returns.ID).(*ast.AliasDecl); isAlias && a.Name == "ExitCode" {
				returnsExitCode = true
			}
		}
		if !returnsExitCode {
			panic(main.Error(diag.ErrMainWrongReturnType, "'main' must return ExitCode"))
		}
		if !main.Throws.IsNil() {
			panic(main.Throws.Error(diag.ErrThrows, "'main' has no caller to pass a failure to: remove 'throws', and catch the failures in 'main' with 'try ... otherwise'"))
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
		if fi.Default.IsNil() {
			continue
		}
		c.guard(func() { fi.Type = c.defaultType(fi, self) })
	}
}

func (c *Checker) defaultType(fi *ast.FieldInfo, rt ast.Ty) ast.Ty {
	var head ast.Expr
	switch fi.Default.Kind() {
	case ast.KindMemberExpr:
		d := fi.Default.ToMemberExpr()

		head = d.Obj()
	case ast.KindCallExpr:
		d := fi.Default.ToCallExpr()

		if m, ok := d.Callee().AsMemberExpr(); ok {
			head = m.Obj()
		}
	}
	if cast, ok := fi.Default.AsCastExpr(); ok {
		return c.resolve(cast.Target(), rt)
	}
	if _, ok := fi.Default.AsBoolLit(); ok {
		return ast.BoolTy
	}
	if s, ok := fi.Default.AsStrLit(); ok && s.IsPlain() {
		return ast.StringTy
	}
	if n, ok := head.AsNameExpr(); ok {
		if nt := c.typeNamed(n.Expr()); nt != nil {
			return c.requireInstance(n.Position(), nt)
		}
	}
	panic(fi.Default.Position().Error(diag.ErrExpressionError,
		fmt.Sprintf("field '%s' needs a type: write 'var %s as <Type>', or a default that is a cast, a bool, a string, or a case or factory of a type", fi.Name, fi.Name)))
}

func (c *Checker) checkFieldDefaults(td *ast.TypeDecl) {
	rt := td.Ty.(*ast.RecordTy)
	for _, fi := range rt.Fields {
		if fi.Default.IsNil() {
			continue
		}
		c.guard(func() {
			c.enterContext(nil, ast.SelfType(rt), true)
			c.uses = &fi.Uses
			c.checkExpecting(fi.Default, fi.Type)
			c.coerce(fi.Default, fi.Type, fmt.Sprintf("the default of field '%s'", fi.Name))
		})
	}
}

func (c *Checker) CheckConst(cd *ast.ConstDecl) {
	c.guard(func() {
		c.enterContext(nil, nil, false)
		t := c.checkExpr(cd.Value)
		if it, ok := t.(*ast.IntTy); ok && it.IsAnonymous() {
			panic(cd.Error("E3220", fmt.Sprintf("constant '%s' has the range %s, which no type names: give it an alias, 'as <Alias>'", cd.Name, t.Name())))
		}
		if t == ast.VoidTy || t == ast.NeverTy {
			panic(cd.Error("E3220", fmt.Sprintf("constant '%s' must be a value known before the program runs", cd.Name)))
		}
		if s, ok := cd.Value.AsStrLit(); ok && t == ast.StringTy {
			for _, part := range s.Parts().All() {
				if !part.Expr.IsNil() {
					panic(cd.Error("E3220", "a constant string cannot interpolate"))
				}
			}
		}
		if why, fix, lacking := c.whyNot(copyable, t); lacking {
			panic(cd.Error(diag.ErrNotCopyable, fmt.Sprintf("constant '%s' has type '%s', which is not Copyable, and a constant is read any number of times: %s; %s",
				cd.Name, t.Name(), why, fix)))
		}
		cd.Type = t
		newRanges(c.project, c.typing, c.errs, &states{}).Constant(cd)
	})
}

func (c *Checker) enterContext(f *ast.FuncDecl, nt ast.NominalTy, calls bool) {
	c.fn = f
	c.root = f
	c.enterBounds(f)
	c.whens = 0
	c.behaviour = nil
	c.outers = nil
	c.owner = nt
	c.allowCalls = calls
	c.scopes = []map[string]*ast.Local{{}}
	c.blocks = nil
	c.nextSlot = 0
	c.uses = nil
	c.dead = false
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

func (c *Checker) pushBlock(label string, kind checkerBlockKind, node ast.NodeID) {
	c.blocks = append(c.blocks, checkerBlock{Label: label, Kind: kind, Node: node})
}

func (c *Checker) popBlock() {
	c.blocks = c.blocks[:len(c.blocks)-1]
}

func (c *Checker) declareLocal(name string, t ast.Ty, isVar bool, site ast.Pos) *ast.Local {
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
	c.bind(local, prm.Position())
	return local
}

func (c *Checker) bind(local *ast.Local, site ast.Pos) {
	name := local.Name
	if rt := c.selfRecord(); rt != nil && rt.Field(name) != nil {
		panic(site.Position().Error(diag.ErrDuplicateDefinition, fmt.Sprintf("'%s' shadows the field '%s' of '%s'", name, name, rt.Name())))
	}
	top := c.scopes[len(c.scopes)-1]
	if _, exists := top[name]; exists {
		panic(site.Position().Error(diag.ErrDuplicateDefinition, fmt.Sprintf("'%s' is declared twice in this scope", name)))
	}
	top[name] = local
	c.fn.AddLocal(local)
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
		f.AddLocal(self)
	}
	f.ParamLocals = make([]ast.LocalID, len(f.Params))
	for i, prm := range f.Params {
		if prm.Name != "_" {
			f.ParamLocals[i] = c.declareParam(prm, f.ParamTypes[i], index).ID()
		}
		index++
	}

	c.checkBlock(f.Body)
	if c.returnType(f) != ast.VoidTy && !ast.TerminatesBody(f.Body) {
		panic(f.Error(diag.ErrMissingReturn, fmt.Sprintf("'%s' must return a %s on every path", f.Name, c.returnType(f).Name())))
	}
}

func (c *Checker) checkBlock(body ast.StmtList) {
	c.pushScope()
	c.checkStmts(body)
	c.popScope()
}

// checkStmts types the statements of a block. Those after the block's live statements are typed too, but are never
// lowered, so the calls they make record no instance.
func (c *Checker) checkStmts(body ast.StmtList) {
	live := ast.Live(body).Len()
	outer := c.dead
	for i, s := range body.All() {
		c.dead = outer || i >= live
		c.guard(func() { c.checkStmt(s) })
	}
	c.dead = outer
}

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
	src := c.ty(e)
	if c.assignable(src, target) {
		return
	}
	if k, ok := src.(*ast.ConstIntTy); ok {
		if ti, isInt := target.(*ast.IntTy); isInt {
			panic(e.Position().Error(diag.ErrTypeMismatch, fmt.Sprintf("%s: %s does not fit %s (%s to %s)", what, k.Value.String(), ti.Name(), ti.Min.String(), ti.Max.String())))
		}
	}
	if si, ok := src.(*ast.IntTy); ok && si.IsAnonymous() {
		if ta, isInt := target.(*ast.IntTy); isInt {
			panic(e.Position().Error(diag.ErrTypeMismatch, fmt.Sprintf("%s: a value of %s may not fit %s; cast it with 'as %s'", what, src.Name(), ta.Name(), ta.Name())))
		}
	}
	panic(e.Position().Error(diag.ErrTypeMismatch, fmt.Sprintf("%s: expected %s, found %s", what, target.Name(), src.Name())))
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

func (c *Checker) requireSigned64(site ast.Pos, t ast.Ty) {
	if k, ok := t.(*ast.ConstIntTy); ok && k.Value.Gt(ast.I64Max) {
		panic(site.Position().Error(diag.ErrIntegerLiteralRange,
			fmt.Sprintf("Integer literal '%s' is above i64.max, so it needs an alias whose range admits it: write '%s as <alias>'", k.Value.String(), k.Value.String())))
	}
}

func (c *Checker) namedInt(t ast.Ty) *ast.IntTy {
	if x, ok := t.(*ast.IntTy); ok && !x.IsAnonymous() {
		return x
	}
	return nil
}

// requireOneAlias refuses operands of two different aliases: each is its own type, and neither converts to the other.
func (c *Checker) requireOneAlias(site ast.Pos, l, r ast.Ty) {
	la, ra := c.namedInt(l), c.namedInt(r)
	if la != nil && ra != nil && la != ra {
		panic(site.Position().Error(diag.ErrTypeMismatch, fmt.Sprintf("the operands are %s and %s; cast one so both are the same type", la.Name(), ra.Name())))
	}
}

// wordOf is the 64-bit word an operation on two integers computes in, as the anonymous type of all its values: an
// unsigned word when either operand reaches past i64.max, else a signed one. No word holds a value that may be negative
// together with one past i64.max.
func (c *Checker) wordOf(site ast.Pos, l, r ast.Ty) *ast.IntTy {
	lmin, lmax := c.rangeOf(l)
	rmin, rmax := c.rangeOf(r)
	if !lmax.Gt(ast.I64Max) && !rmax.Gt(ast.I64Max) {
		return ast.Anonymous64
	}
	if lmin.IsNeg() || rmin.IsNeg() {
		panic(site.Position().Error(diag.ErrTypeMismatch, fmt.Sprintf("no 64-bit word holds both %s and %s: one may be negative and the other is past i64.max; cast one with 'as <Alias>' to a range that is not negative, or that stays within i64.max", l.Name(), r.Name())))
	}
	return ast.AnonymousU64
}

// operandWord is the word an arithmetic or comparison operator takes its operands in.
func (c *Checker) operandWord(site ast.Pos, l, r ast.Ty) *ast.IntTy {
	c.requireOneAlias(site, l, r)
	return c.wordOf(site, l, r)
}

// exactType is the anonymous type of the values an operation gives, which is the span exact of its exact result cut to
// the word it computes in: a result past the word is what makes the operation fail.
func exactType(word *ast.IntTy, exact span) *ast.IntTy {
	in := exact.clamp(typeSpan(word))
	if in.empty() || (in.lo == word.Min && in.hi == word.Max) {
		return word
	}
	return &ast.IntTy{Min: in.lo, Max: in.hi}
}

// unifyInts is the type of two integers that one type must hold: the alias of either, which must hold the other, or
// else the word they are computed in.
func (c *Checker) unifyInts(site ast.Pos, l, r ast.Ty) *ast.IntTy {
	c.requireOneAlias(site, l, r)
	la := c.namedInt(l)
	ra := c.namedInt(r)
	alias := la
	if alias == nil {
		alias = ra
	}
	if alias == nil {
		return c.wordOf(site, l, r)
	}
	other := r
	if la == nil {
		other = l
	}
	if !c.assignable(other, alias) {
		panic(site.Position().Error(diag.ErrTypeMismatch, fmt.Sprintf("an operand of %s does not fit %s; cast it with 'as %s'", other.Name(), alias.Name(), alias.Name())))
	}
	return alias
}

func (c *Checker) join(site ast.Pos, a, b ast.Ty) ast.Ty {
	if a == ast.NeverTy {
		return b
	}
	if b == ast.NeverTy || a == b {
		return a
	}
	if c.isInt(a) && c.isInt(b) {
		// Branches of one alias give it; an alias and a range it holds give the alias; anything else gives a range that
		// holds both, which one word must.
		c.requireOneAlias(site, a, b)
		if alias, other := c.namedInt(a), b; alias != nil || c.namedInt(b) != nil {
			if alias == nil {
				alias, other = c.namedInt(b), a
			}
			if c.assignable(other, alias) {
				return alias
			}
		}
		c.wordOf(site, a, b)
		amin, amax := c.rangeOf(a)
		bmin, bmax := c.rangeOf(b)
		return &ast.IntTy{Min: ast.MinI(amin, bmin), Max: ast.MaxI(amax, bmax)}
	}
	panic(site.Position().Error(diag.ErrTypeMismatch, fmt.Sprintf("the branches produce %s and %s", a.Name(), b.Name())))
}

func (c *Checker) checkExpr(e ast.Expr) ast.Ty {
	return c.checkExpecting(e, nil)
}

// checkExpecting types an expression whose context needs a value of want; want is nil where the context needs none
// in particular. The expectation reaches only the expression itself, and the branches that give its value.
func (c *Checker) checkExpecting(e ast.Expr, want ast.Ty) ast.Ty {
	outer := c.expect
	c.expect = want
	t := ast.VisitExprResult[ast.Ty](c, e)
	c.expect = outer
	c.typing.SetType(e, t)
	return t
}

func (c *Checker) VisitIntLit(x ast.IntLit) ast.Ty { return &ast.ConstIntTy{Value: x.Value()} }

func (c *Checker) VisitBoolLit(ast.BoolLit) ast.Ty { return ast.BoolTy }

func (c *Checker) VisitMatchExpr(x ast.MatchExpr) ast.Ty { return c.checkMatch(x.Match(), true) }

func (c *Checker) VisitStrLit(s ast.StrLit) ast.Ty {
	for _, p := range s.Parts().All() {
		part := p.Expr
		if part.IsNil() {
			continue
		}
		t := ast.Content(c.checkExpr(part))
		c.requireSigned64(part.Position(), t)
		_, isEnum := t.(*ast.EnumTy)
		_, isUnion := t.(*ast.UnionTy)
		if !(c.isInt(t) || t == ast.StringTy || t == ast.BoolTy || isEnum || isUnion) {
			panic(part.Position().Error(diag.ErrTypeMismatch, fmt.Sprintf("a value of %s cannot be interpolated", t.Name())))
		}
	}
	return ast.StringTy
}

func (c *Checker) VisitNameExpr(n ast.NameExpr) ast.Ty {
	if local := c.useLocal(n); local != nil {
		c.typing.SetName(n, ast.LocalName(local))
		return local.Type
	}
	if rt := c.selfRecord(); rt != nil {
		if field := rt.Field(n.Name()); field != nil {
			c.typing.SetName(n, ast.FieldName(field))
			return rt.FieldType(field)
		}
	}
	if c.behaviour != nil {
		if ft, ok := c.selfFieldInBehaviour(n.Name()); ok {
			c.refuseBehaviourSelf(n.Position(), "the field '"+n.Name()+"'", ft)
		}
	}
	switch d := c.target(n.ID).(type) {
	case *ast.ConstDecl:
		c.typing.SetName(n, c.typing.ConstName(d))
		if d.Type == nil {
			panic(n.Error(diag.ErrExpressionError, fmt.Sprintf("constant '%s' has no value", d.Name)))
		}
		return d.Type
	case *ast.FuncDecl:
		panic(n.Error(diag.ErrUnsupported, "a function value is not supported by minimaxon; call the function"))
	case ast.TypeDeclNode:
		panic(n.Error(diag.ErrExpressionError, fmt.Sprintf("'%s' is a type, not a value", n.Name())))
	}
	if n.Name() == "Self" {
		panic(n.Error(diag.ErrExpressionError, "'Self' is a type, not a value"))
	}
	panic(n.Error(diag.ErrExpressionError, fmt.Sprintf("undefined variable '%s'", n.Name())))
}

func (c *Checker) VisitSelfExpr(s ast.SelfExpr) ast.Ty {
	if c.behaviour != nil {
		c.refuseBehaviourSelf(s.Position(), "'self'", c.owner)
	}
	if c.fn == nil || !c.fn.IsMethod() {
		panic(s.Error(diag.ErrUnexpectedToken, "'self' is used only inside an instance method"))
	}
	return c.owner
}

func (c *Checker) fold(site ast.Pos, op string, a, b ast.I128) ast.I128 {
	var r ast.I128
	switch op {
	case "+":
		r = a.Add(b)
	case "-":
		r = a.Sub(b)
	case "*":
		product, ok := ast.FromBig(new(big.Int).Mul(a.Big(), b.Big()))
		if !ok {
			panic(site.Position().Error(diag.ErrTypeMismatch, "the constant expression overflows 64 bits"))
		}
		r = product
	case "/", "mod":
		if b.IsZero() {
			panic(site.Position().Error(diag.ErrDivisionByZero, "division by a constant zero"))
		}
		var q, m big.Int
		q.QuoRem(a.Big(), b.Big(), &m)
		var v *big.Int = &q
		if op == "mod" {
			v = &m
		}
		quotient, ok := ast.FromBig(v)
		if !ok {
			panic(site.Position().Error(diag.ErrTypeMismatch, "the constant expression overflows 64 bits"))
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
			panic(site.Position().Error("E2054", "a shift count cannot be negative"))
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
		panic(site.Position().Error(diag.ErrUnsupported, fmt.Sprintf("cannot fold '%s'", op)))
	}
	if r.Lt(ast.I64Min) || r.Gt(ast.U64Max) {
		panic(site.Position().Error(diag.ErrTypeMismatch, "the constant expression overflows 64 bits"))
	}
	return r
}

func (c *Checker) VisitBinaryExpr(b ast.BinaryExpr) ast.Ty {
	want := c.expect
	l := c.checkExpr(b.L())
	r := c.checkExpr(b.R())

	if checkerArithmetic[b.Op()] || (checkerLogical[b.Op()] && c.isInt(l)) {
		if !c.isInt(l) || !c.isInt(r) {
			panic(b.Error(diag.ErrTypeMismatch, fmt.Sprintf("'%s' needs integers, found %s and %s", b.Op(), l.Name(), r.Name())))
		}
		lc, lConst := l.(*ast.ConstIntTy)
		rc, rConst := r.(*ast.ConstIntTy)
		if lConst && rConst {
			c.typing.SetOperand(b, ast.OperandOf(ast.Anonymous64))
			return &ast.ConstIntTy{Value: c.fold(b.Position(), b.Op(), lc.Value, rc.Value)}
		}
		if (b.Op() == "/" || b.Op() == "mod") && rConst && rc.Value.IsZero() {
			panic(b.Error(diag.ErrDivisionByZero, "division by a constant zero"))
		}
		if b.Op() == "shl" || b.Op() == "shr" {
			lt := ast.Anonymous64
			if !lConst {
				lt = l.(*ast.IntTy)
			}
			c.typing.SetOperand(b, ast.OperandOf(lt))
			return lt
		}
		if checkerLogical[b.Op()] {
			t := c.unifyInts(b.Position(), l, r)
			c.typing.SetOperand(b, ast.OperandOf(t))
			return t
		}
		word := c.operandWord(b.Position(), l, r)
		c.typing.SetOperand(b, ast.OperandOf(word))
		if expected, ok := want.(*ast.IntTy); ok {
			return expected
		}
		exact, _ := arithmetic(b.Op(), typeSpan(l), typeSpan(r), typeSpan(word))
		return exactType(word, exact)
	}

	if checkerLogical[b.Op()] {
		if l != ast.BoolTy || r != ast.BoolTy {
			panic(b.Error(diag.ErrTypeMismatch, fmt.Sprintf("'%s' needs two bools or two integers, found %s and %s", b.Op(), l.Name(), r.Name())))
		}
		return ast.BoolTy
	}

	if checkerComparison[b.Op()] {
		if c.isInt(l) && c.isInt(r) {
			c.typing.SetOperand(b, ast.OperandOf(c.operandWord(b.Position(), l, r)))
			return ast.BoolTy
		}
		equality := b.Op() == "==" || b.Op() == "!="
		_, lUnion := l.(*ast.UnionTy)
		_, rUnion := r.(*ast.UnionTy)
		if lUnion || rUnion {
			panic(b.Error(diag.ErrEnumNotComparable, "a union cannot be compared; inspect it with 'match'"))
		}
		_, lEnum := l.(*ast.EnumTy)
		if equality && l == r && (l == ast.BoolTy || l == ast.StringTy || lEnum || c.isHandle(l)) {
			c.typing.SetOperand(b, ast.OperandOf(l))
			return ast.BoolTy
		}
		panic(b.Error(diag.ErrTypeMismatch, fmt.Sprintf("'%s' cannot compare %s and %s", b.Op(), l.Name(), r.Name())))
	}

	panic(b.Error(diag.ErrUnsupported, fmt.Sprintf("unknown operator '%s'", b.Op())))
}

// isHandle is whether t is a Handle, which is compared as a word.
func (c *Checker) isHandle(t ast.Ty) bool {
	bt, ok := t.(*ast.BuiltinTy)
	return ok && bt.Kind == ast.BuiltinHandle
}

func (c *Checker) VisitUnaryExpr(u ast.UnaryExpr) ast.Ty {
	want := c.expect
	t := c.checkExpr(u.E())
	if u.Op() == "not" {
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
		if expected, ok := want.(*ast.IntTy); ok {
			return expected
		}
		return exactType(ast.Anonymous64, span{lo: x.Max.Neg(), hi: x.Min.Neg()})
	}
	panic(u.Error(diag.ErrTypeMismatch, fmt.Sprintf("'-' needs an integer, found %s", t.Name())))
}

func (c *Checker) VisitCondExpr(cd ast.CondExpr) ast.Ty {
	if c.checkExpr(cd.Cond()) != ast.BoolTy {
		panic(cd.Cond().Position().Error(diag.ErrTypeMismatch, fmt.Sprintf("a condition must be a bool, found %s", c.ty(cd.Cond()).Name())))
	}
	then := c.checkExpecting(cd.Then(), c.expect)
	els := c.checkExpecting(cd.Else(), c.expect)
	return c.join(cd.Position(), then, els)
}

// VisitTryExpr types a try: the value of its expression, or on a failure the value 'otherwise' gives or the jump it makes.
func (c *Checker) VisitTryExpr(t ast.TryExpr) ast.Ty {
	result := c.checkExpecting(t.E(), c.expect)
	switch {
	case !t.Value().IsNil():
		want := c.expect
		if want == nil {
			want = result
		}
		other := c.checkExpecting(t.Value(), want)
		return c.join(t.Value().Position(), result, other)
	case !t.Ret().IsNil():
		if c.fn == nil {
			panic(t.Ret().Error(diag.ErrUnexpectedToken, "a constant or a default is in no function to return from: give 'otherwise' the value to use"))
		}
		c.VisitReturnStmt(t.Ret())
	case !t.Brk().IsNil():
		c.VisitBreakStmt(t.Brk())
	case !t.Cont().IsNil():
		c.VisitContinueStmt(t.Cont())
	}
	return result
}

func (c *Checker) VisitPanicExpr(p ast.PanicExpr) ast.Ty {
	if c.checkExpr(p.Message()) != ast.StringTy {
		panic(p.Message().Position().Error(diag.ErrTypeMismatch, fmt.Sprintf("panic takes a String, found %s", c.ty(p.Message()).Name())))
	}
	return ast.NeverTy
}

// argSlot is the parameter that the index-th argument given stands for: the first by its place, every other by its label.
// It is -1 for an argument that names no parameter after the first, or that no label names.
func argSlot(index int, arg ast.Arg, ps []ast.ParamSpec) int {
	if index == 0 {
		return 0
	}
	for j, q := range ps {
		if j > 0 && q.Name == arg.Label() {
			return j
		}
	}
	return -1
}

// orderArgs puts a call's arguments in the order of the parameters, which labels name after the first.
func (c *Checker) orderArgs(call ast.CallExpr, callee string, ps []ast.ParamSpec) []ast.Expr {
	given := call.Args().Len()
	left := c.omitted.arg
	if c.omitted.call != call {
		left = ast.Arg{}
	}
	if !left.IsNil() {
		given--
	}
	if given != len(ps) {
		panic(call.Error(diag.ErrCallArgCountMismatch, fmt.Sprintf("'%s' takes %d argument(s), given %d", callee, len(ps), given)))
	}
	ordered := make([]ast.Expr, len(ps))
	i := -1
	for _, arg := range call.Args().All() {
		if arg == left {
			continue
		}
		i++
		var slot int
		if i == 0 {
			if arg.Label() != "" {
				panic(arg.Error(diag.ErrCallFirstArgNamed, "the first argument is positional: drop its label"))
			}
			slot = 0
		} else {
			if arg.Label() == "" {
				panic(arg.Error(diag.ErrCallArgMissingLabel, fmt.Sprintf("argument %d of '%s' needs its label", i+1, callee)))
			}
			slot = argSlot(i, arg, ps)
			if slot <= 0 {
				panic(arg.Error(diag.ErrCallUnknownArgLabel, fmt.Sprintf("'%s' has no parameter '%s' after its first", callee, arg.Label())))
			}
			if !ordered[slot].IsNil() {
				panic(arg.Error(diag.ErrCallDuplicateArg, fmt.Sprintf("'%s' is given twice", arg.Label())))
			}
		}
		ordered[slot] = arg.Value()
	}
	return ordered
}

// checkedArgs orders a call's arguments and types them, before the parameters' types are known: a generic callee's
// type arguments come from them.
func (c *Checker) checkedArgs(call ast.CallExpr, callee string, ps []ast.ParamSpec) []ast.Expr {
	ordered := c.orderArgs(call, callee, ps)
	for _, v := range ordered {
		c.checkExpr(v)
	}
	return ordered
}

func (c *Checker) bindArgs(call ast.CallExpr, callee string, ps []ast.ParamSpec, argsChecked bool) []ast.Expr {
	ordered := c.orderArgs(call, callee, ps)
	for i := range ps {
		if !argsChecked {
			c.checkExpecting(ordered[i], ps[i].Type)
		}
		c.coerceArg(ordered[i], callee, ps[i])
	}
	return ordered
}

func (c *Checker) coerceArg(value ast.Expr, callee string, ps ast.ParamSpec) {
	switch {
	case ps.Conv.ByAddress():
		c.checkSameType(value, ps)
	case ast.IsFrozen(c.ty(value)) && ast.Content(c.ty(value)) == ps.Type:
		// A let parameter borrows, and the contents of a frozen value can be borrowed: the callee reads them and
		// cannot change them. A parameter that changes or keeps them cannot have them.
		if ps.Conv != ast.ConvLet {
			panic(value.Position().Error(diag.ErrFrozenChange, fmt.Sprintf("the %s parameter '%s' of '%s' would change or keep the contents of a frozen value, which are immutable and shared; pass an owned copy: 'thaw(...)'", ps.Conv, ps.Name, callee)))
		}
	default:
		c.coerce(value, ps.Type, fmt.Sprintf("argument '%s' of '%s'", ps.Name, callee))
	}
}

func (c *Checker) checkSameType(value ast.Expr, ps ast.ParamSpec) {
	if c.ty(value) != ps.Type {
		panic(value.Position().Error(diag.ErrTypeMismatch, fmt.Sprintf("the %s parameter '%s' is %s; the argument is %s", ps.Conv, ps.Name, ps.Type.Name(), c.ty(value).Name())))
	}
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

// infer chooses the type arguments of a call from its arguments' types, and the ones they leave open from the type
// the context expects of result, the type the call gives before its type arguments are known.
func (c *Checker) infer(site ast.Pos, callee string, params []*ast.TypeParamTy, specs []ast.ParamSpec, ordered []ast.Expr, result ast.Ty) []ast.Ty {
	args := make([]ast.Ty, len(params))
	for i, spec := range specs {
		unifyType(spec.Type, c.ty(ordered[i]), params, args)
	}
	return c.inferRest(site, callee, params, args, result)
}

// inferRest fills the type arguments that are still open from the type the context expects of result.
func (c *Checker) inferRest(site ast.Pos, callee string, params []*ast.TypeParamTy, args []ast.Ty, result ast.Ty) []ast.Ty {
	if c.expect != nil {
		unifyType(result, c.expect, params, args)
	}
	for i, a := range args {
		if a == nil {
			panic(site.Position().Error(diag.ErrCannotInfer, fmt.Sprintf("the type argument '%s' of '%s' is fixed neither by the arguments nor by where the result goes: pass an argument whose type names it, use the result where its type is declared (a return, an argument, an assignment), or call it through a typealias of the instance",
				params[i].Name(), callee)))
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
// use is what a call records for the instances it needs: what it is, and the object it is made on.
func (c *Checker) use(call ast.CallExpr) ast.CallUse {
	info := c.typing.Call(call)
	return ast.CallUse{Info: info, Receiver: call.ReceiverFor(info.Kind), Site: call.Position()}
}

func (c *Checker) recordUses(calls ...ast.CallUse) {
	if c.uses == nil {
		panic(diag.Internal("check: a call is checked outside a function or a field default"))
	}
	if c.dead {
		return
	}
	if c.root != nil && len(*c.uses) == 0 && len(calls) > 0 {
		c.Callers = append(c.Callers, c.root)
	}
	*c.uses = append(*c.uses, calls...)
}

// callGeneric types a call whose callee has type parameters. given are the arguments of the instance the callee is
// called on, which were checked against their bounds where the instance was written; without them they are inferred.
func (c *Checker) callGeneric(call ast.CallExpr, target *ast.FuncDecl, given []ast.Ty) ast.Ty {
	callee := target.Symbol()
	args := given
	var ordered []ast.Expr
	var specs []ast.ParamSpec
	if given != nil {
		specs = ast.Binding{Params: target.Generics, Args: given}.ApplySpecs(target.Pass.Specs)
		ordered = c.orderArgs(call, callee, specs)
		for i, spec := range specs {
			c.checkExpecting(ordered[i], spec.Type)
		}
	} else {
		ordered = c.checkedArgs(call, callee, target.Pass.Specs)
	}
	if target.Intrinsic != ast.NotIntrinsic {
		c.checkIntrinsicArgs(call, target, ordered)
	}
	if given == nil {
		args = c.infer(call.Position(), callee, target.Generics, target.Pass.Specs, ordered, target.ReturnType)
		c.queueBounds(call.Position(), target.Generics, args)
		specs = ast.Binding{Params: target.Generics, Args: args}.ApplySpecs(target.Pass.Specs)
	}
	c.queueMethodBounds(call.Position(), target, args)
	info := c.typing.Call(call)
	info.Generic = &ast.GenericCall{SelfConv: target.Pass.SelfConv, Specs: specs, TypeArgs: args}
	info.Pass = &info.Generic.Passing
	info.Ordered = ordered
	if target.Intrinsic == ast.NotIntrinsic {
		c.recordUses(c.use(call))
	}
	if !c.declares(target) {
		c.Pending = append(c.Pending, ast.PendingCall{Call: call, Callee: callee, Params: info.Pass.Specs})
	} else {
		for i, spec := range info.Pass.Specs {
			c.coerceArg(ordered[i], callee, spec)
		}
	}
	return ast.Binding{Params: target.Generics, Args: args}.Apply(c.returnType(target))
}

// declares is whether a call of the function is checked now: its signature is known, because this module declares it
// or because it is built in. Another module may still be declaring its own, so a call into it waits for the verify
// phase.
func (c *Checker) declares(f *ast.FuncDecl) bool {
	return f.Module == c.module || f.Intrinsic != ast.NotIntrinsic
}

// requireExported refuses a function of another module that the module does not export.
func (c *Checker) requireExported(site ast.Pos, target *ast.FuncDecl) {
	if !c.declares(target) && !target.IsExported() {
		panic(site.Position().Error("E3008", fmt.Sprintf("'%s' is not exported from module %s", target.Symbol(), target.Module.Display())))
	}
}

func (c *Checker) callFunction(call ast.CallExpr, kind ast.CallKind, target *ast.FuncDecl, given []ast.Ty) ast.Ty {
	c.requireExported(call.Position(), target)
	info := c.typing.Call(call)
	info.Kind = kind
	info.Target = target
	info.Pass = &target.Pass
	if len(target.Generics) > 0 {
		return c.callGeneric(call, target, given)
	}
	if c.declares(target) {
		info.Ordered = c.bindArgs(call, target.Symbol(), target.Pass.Specs, false)
	} else {
		// The call is verified later, but its arguments are typed now: each takes the type of the parameter it stands for
		// as its expected type, as a call in the module does, so an argument means the same whichever module the callee
		// is in. One that stands for no parameter is an error that verify reports.
		specs := target.Pass.Specs
		for i, a := range call.Args().All() {
			var want ast.Ty
			if slot := argSlot(i, a, specs); slot >= 0 && slot < len(specs) && call.Args().Len() == len(specs) {
				want = specs[slot].Type
			}
			c.checkExpecting(a.Value(), want)
		}
		c.Pending = append(c.Pending, ast.PendingCall{Call: call, Callee: target.Symbol(), Params: specs})
	}
	return c.returnType(target)
}

// typeNamed is the type that a name written as an expression names, or nil if it names no type. A generic type names
// its declaration, which only a call or a case that infers the arguments accepts; requireInstance refuses it anywhere
// else. A typealias of an instance names the instance.
func (c *Checker) typeNamed(e ast.Expr) ast.NominalTy {
	n, ok := e.AsNameExpr()
	if !ok {
		return nil
	}
	if n.Name() == "Self" {
		if c.owner == nil {
			panic(n.Error(diag.ErrUnknownType, "'Self' names a type only inside one"))
		}
		return c.owner
	}
	if c.lookupLocal(n.Name()) != nil {
		return nil
	}
	if rt := c.selfRecord(); rt != nil && rt.Field(n.Name()) != nil {
		return nil
	}
	tb, isType := c.target(n.ID).(ast.TypeDeclNode)
	if !isType {
		return nil
	}
	nt, isNominal := tb.TypeBase().Ty.(ast.NominalTy)
	if !isNominal {
		return nil
	}
	return nt
}

// requireInstance refuses a generic declaration where a type is needed and no call can infer its arguments.
func (c *Checker) requireInstance(site ast.Pos, nt ast.NominalTy) ast.NominalTy {
	if nt.Base().IsGeneric() {
		panic(site.Position().Error(diag.ErrTypeArgumentCount, fmt.Sprintf("'%s' is generic: name an instance with a typealias, as 'typealias Name = %s'",
			nt.Name(), ast.InstanceSpelling(nt.Name(), paramNames(nt.Base().Params)))))
	}
	return nt
}

func (c *Checker) VisitCallExpr(call ast.CallExpr) ast.Ty {
	if !c.allowCalls {
		panic(call.Error("E3220", "a constant calls nothing"))
	}

	if n, ok := call.Callee().AsNameExpr(); ok && c.lookupLocal(n.Name()) == nil {
		callee := c.target(n.ID)
		if callee == nil && (n.Name() == "print" || n.Name() == "printError") {
			info := c.typing.Call(call)
			if n.Name() == "print" {
				info.Kind = ast.CallPrint
			} else {
				info.Kind = ast.CallPrintError
			}
			info.Ordered = c.bindArgs(call, n.Name(), []ast.ParamSpec{{Name: "text", Type: ast.StringTy, Conv: ast.ConvLet}}, false)
			return ast.VoidTy
		}
		if f, isFunc := callee.(*ast.FuncDecl); isFunc {
			return c.callFunction(call, ast.CallFunction, f, nil)
		}
		if ast.IsCownConstructor(callee) {
			return c.cownCall(call)
		}
		panic(call.Error(diag.ErrUnknownFunction, fmt.Sprintf("unknown function '%s'", n.Name())))
	}

	if m, ok := call.Callee().AsMemberExpr(); ok {
		if nt := c.typeNamed(m.Obj()); nt != nil {
			if ut, isUnion := nt.(*ast.UnionTy); isUnion {
				if ci := ut.CaseIndex(m.Name()); ci >= 0 {
					return c.unionConstructor(call, ut, ci)
				}
			}
			if sm, found := nt.MethodTable()[m.Name()]; found && sm.IsStatic {
				return c.callFunction(call, ast.CallStatic, sm, nt.Base().Args)
			}
			panic(call.Error(diag.ErrUnknownFunction, fmt.Sprintf("'%s' has no static function or case '%s'", nt.Name(), m.Name())))
		}

		rt := c.checkExpr(m.Obj())
		if m.Name() == copyMethod {
			return c.copyCall(call, rt)
		}
		content := ast.Content(rt)
		requireNotCown(call.Position(), content, "calling", m.Name())
		if param, isParam := content.(*ast.TypeParamTy); isParam {
			return c.boundCall(call, m, param, rt)
		}
		if recv, isNominal := content.(ast.NominalTy); isNominal {
			if method, found := recv.MethodTable()[m.Name()]; found && !method.IsStatic {
				requireNotFrozenChange(call.Position(), rt, method.Pass.SelfConv, m.Name())
				return c.callFunction(call, ast.CallMethod, method, recv.Base().Args)
			}
		}
		if content == ast.StringTy {
			if method, found := c.project.Builtins.StringMethods[m.Name()]; found {
				return c.callFunction(call, ast.CallMethod, method, nil)
			}
		}
		panic(call.Error(diag.ErrUnknownFunction, fmt.Sprintf("%s has no method '%s'", rt.Name(), m.Name())))
	}

	panic(call.Error(diag.ErrUnsupported, "only a named function or method can be called"))
}

// unionConstructor types 'Union.case(payload)'. A generic union written without type arguments takes them from the
// payload.
func (c *Checker) unionConstructor(call ast.CallExpr, ut *ast.UnionTy, caseIndex int) ast.Ty {
	uc := ut.CaseList()[caseIndex]
	if len(uc.Payload) == 0 {
		panic(call.Error(diag.ErrEnumWrongBindingCount, fmt.Sprintf("case '%s' carries no payload: write '%s.%s'", uc.Name, ut.Name(), uc.Name)))
	}
	info := c.typing.Call(call)
	info.Kind = ast.CallUnionCtor
	info.CaseIndex = int32(caseIndex)
	specsOf := func(u *ast.UnionTy) []ast.ParamSpec {
		payload := u.Payload(caseIndex)
		specs := make([]ast.ParamSpec, len(payload))
		for i, p := range payload {
			specs[i] = ast.ParamSpec{Name: p.Name, Type: p.Type, Conv: ast.ConvConsumes}
		}
		return specs
	}
	specs := specsOf(ut)
	callee := ut.Name() + "." + uc.Name
	if ut.Base().IsGeneric() {
		ordered := c.checkedArgs(call, callee, specs)
		ut = c.instantiate(call.Position(), ut, c.infer(call.Position(), ut.Name(), ut.Base().Params, specs, ordered, ast.SelfType(ut))).(*ast.UnionTy)
		specs = specsOf(ut)
		for i, spec := range specs {
			c.coerceArg(ordered[i], callee, spec)
		}
		info.Ordered = ordered
	} else {
		info.Ordered = c.bindArgs(call, callee, specs, false)
	}
	info.Union = ut
	info.Pass = &ast.Passing{Specs: specs}
	return ut
}

// boundCall types a call of a method that a type parameter's bounds declare. It is the only method a value of a type
// parameter has, and it runs the method of whatever type the parameter becomes.
func (c *Checker) boundCall(call ast.CallExpr, m ast.MemberExpr, param *ast.TypeParamTy, receiver ast.Ty) ast.Ty {
	var iface *ast.InterfaceTy
	var sig *ast.FuncSig
	for _, candidate := range c.boundsOf(param) {
		s, found := candidate.Methods[m.Name()]
		if !found || candidate == iface {
			continue
		}
		if sig != nil {
			panic(call.Error(diag.ErrAmbiguousBound, fmt.Sprintf("the bounds '%s' and '%s' of '%s' both declare '%s'; keep only one bound that declares it",
				iface.Name(), candidate.Name(), param.Name(), m.Name())))
		}
		iface, sig = candidate, s
	}
	if sig == nil {
		panic(call.Error(diag.ErrUnknownFunction, fmt.Sprintf("%s has no method '%s'%s", param.Name(), m.Name(), c.boundsText(param))))
	}
	requireNotFrozenChange(call.Position(), receiver, sig.Pass.SelfConv, m.Name())
	b := ast.Binding{Params: []*ast.TypeParamTy{iface.Self}, Args: []ast.Ty{param}}
	info := c.typing.Call(call)
	info.Kind = ast.CallBound
	info.Generic = &ast.GenericCall{
		SelfConv: sig.Pass.SelfConv, Specs: b.ApplySpecs(sig.Pass.Specs),
		Method: sig,
	}
	info.Pass = &info.Generic.Passing
	info.Ordered = c.bindArgs(call, param.Name()+"."+m.Name(), info.Pass.Specs, false)
	c.recordUses(c.use(call))
	return b.Apply(sig.ReturnType)
}

// boundsText says what a type parameter can call, for the error that names a method it cannot.
func (c *Checker) boundsText(param *ast.TypeParamTy) string {
	bounds := c.boundsOf(param)
	if len(bounds) == 0 {
		return fmt.Sprintf(": a type parameter has only the methods of its bounds, and '%s' has none; constrain it with 'where %s implements <Interface>'", param.Name(), param.Name())
	}
	names := make([]string, len(bounds))
	for i, iface := range bounds {
		names[i] = iface.Name()
	}
	return fmt.Sprintf(": a type parameter has only the methods of its bounds (%s)", strings.Join(names, ", "))
}

// requireConforms checks one type argument against one bound of its parameter.
func (c *Checker) requireConforms(site ast.Pos, arg ast.Ty, iface *ast.InterfaceTy, param *ast.TypeParamTy) {
	if k, structural := c.capabilityOf(iface); structural {
		if why, fix, lacking := c.whyNot(k, arg); lacking {
			panic(site.Position().Error(diag.ErrBoundNotMet, fmt.Sprintf("the type argument '%s' for '%s' must be %s, but %s: %s", arg.Name(), param.Name(), iface.Name(), why, fix)))
		}
		return
	}
	switch t := arg.(type) {
	case *ast.TypeParamTy:
		if slices.Contains(c.boundsOf(t), iface) {
			return
		}
		panic(site.Position().Error(diag.ErrBoundNotMet, fmt.Sprintf("the type parameter '%s' is not known to be %s, which the type argument for '%s' must be: add the bound, '%s implements %s'",
			t.Name(), iface.Name(), param.Name(), t.Name(), iface.Name())))
	case ast.NominalTy:
		if slices.Contains(t.Base().Declaration().Conforms, iface) {
			return
		}
		if c.project.Builtins.Is(t) {
			panic(site.Position().Error(diag.ErrBoundNotMet, fmt.Sprintf("'%s' is built in and implements no interface, so it cannot be %s for '%s'; wrap it in a type of your own that declares 'implements %s'",
				t.Name(), iface.Name(), param.Name(), c.spelled(iface))))
		}
		panic(site.Position().Error(diag.ErrBoundNotMet, fmt.Sprintf("'%s' must be %s for '%s': declare '%s %s implements %s'%s",
			t.Name(), iface.Name(), param.Name(), declarationWord(t), t.Base().Declaration().Name(), c.spelled(iface), missingMethods(t, iface))))
	}
	panic(site.Position().Error(diag.ErrBoundNotMet, fmt.Sprintf("'%s' must be %s for '%s', but only a type, enum or union implements an interface",
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
func (c *Checker) checkConformance(site ast.Pos, nt ast.NominalTy) {
	self := ast.SelfType(nt)
	for _, iface := range nt.Base().Conforms {
		b := ast.Binding{Params: []*ast.TypeParamTy{iface.Self}, Args: []ast.Ty{self}}
		for _, sig := range iface.Decl.Methods {
			c.requireImplements(site, nt, iface, sig, b)
		}
	}
}

func (c *Checker) requireImplements(site ast.Pos, nt ast.NominalTy, iface *ast.InterfaceTy, sig *ast.FuncSig, b ast.Binding) {
	wantSpecs, wantReturn := b.ApplySpecs(sig.Pass.Specs), b.Apply(sig.ReturnType)
	m, found := nt.MethodTable()[sig.Name]
	if found && m.HasOwnBounds() {
		panic(site.Position().Error(diag.ErrConformance, fmt.Sprintf("'%s' says it implements %s, but its method '%s' adds a 'where' on the type's parameters, so '%s' would implement %s only for some instances: remove the 'where' from the method, or put the bound on the type",
			nt.Name(), iface.Name(), sig.Name, nt.Name(), iface.Name())))
	}
	if !found || m.IsStatic || m.Pass.SelfConv != sig.Pass.SelfConv || !slices.Equal(m.Pass.Specs, wantSpecs) || m.ReturnType != wantReturn || m.ErrorType != sig.ErrorType {
		problem := "does not have it"
		if found {
			problem = "has it as '" + signatureText(m.Name, m.Pass.SelfConv, m.Pass.Specs, m.ReturnType, m.ErrorType) + "'"
		}
		panic(site.Position().Error(diag.ErrConformance, fmt.Sprintf("'%s' says it implements %s, which needs '%s', but '%s' %s: add or change the method to match it",
			nt.Name(), iface.Name(), signatureText(sig.Name, sig.Pass.SelfConv, wantSpecs, wantReturn, sig.ErrorType), nt.Name(), problem)))
	}
}

// signatureText is a method as it is written in a declaration.
func signatureText(name string, selfConv ast.Convention, specs []ast.ParamSpec, ret, err ast.Ty) string {
	var parts []string
	if selfConv != ast.ConvLet {
		parts = append(parts, selfConv.String()+" self")
	}
	for _, s := range specs {
		word := ""
		if s.Conv != ast.ConvLet {
			word = s.Conv.String() + " "
		}
		parts = append(parts, word+s.Name+" "+s.Type.Name())
	}
	text := "function " + name + "(" + strings.Join(parts, ", ") + ")"
	if ret != ast.Ty(ast.VoidTy) {
		text += " returns " + ret.Name()
	}
	if err != nil {
		text += " throws " + err.Name()
	}
	return text
}

func (c *Checker) copyCall(call ast.CallExpr, t ast.Ty) ast.Ty {
	if t == ast.VoidTy || t == ast.NeverTy {
		panic(call.Error(diag.ErrTypeMismatch, "an expression with no value cannot be copied"))
	}
	if why, fix, lacking := c.whyNot(copyable, t); lacking {
		panic(call.Error(diag.ErrNotCopyable, fmt.Sprintf("a value of '%s' cannot be copied: %s; %s, or move the value instead of copying it", t.Name(), why, fix)))
	}
	info := c.typing.Call(call)
	info.Kind = ast.CallCopy
	info.Ordered = c.bindArgs(call, copyMethod, nil, false)
	return t
}

func (c *Checker) VisitMemberExpr(m ast.MemberExpr) ast.Ty {
	if nt := c.typeNamed(m.Obj()); nt != nil {
		if ut, isUnion := nt.(*ast.UnionTy); isUnion && ut.Base().IsGeneric() && ut.CaseIndex(m.Name()) >= 0 {
			params := ut.Base().Params
			nt = c.instantiate(m.Obj().Position(), ut, c.inferRest(m.Obj().Position(), ut.Name()+"."+m.Name(), params, make([]ast.Ty, len(params)), ast.SelfType(ut)))
		}
		c.requireInstance(m.Obj().Position(), nt)
		switch t := nt.(type) {
		case *ast.EnumTy:
			caseIndex := t.CaseIndex(m.Name())
			if caseIndex < 0 {
				panic(m.Error(diag.ErrEnumUnknownCase, fmt.Sprintf("'%s' has no case '%s'", t.Name(), m.Name())))
			}
			c.typing.SetMember(m, ast.MemberInfo{Kind: ast.MemberEnumCase, CaseIndex: int32(caseIndex)})
			return t
		case *ast.UnionTy:
			caseIndex := t.CaseIndex(m.Name())
			if caseIndex < 0 {
				panic(m.Error(diag.ErrEnumUnknownCase, fmt.Sprintf("'%s' has no case '%s'", t.Name(), m.Name())))
			}
			if len(t.CaseList()[caseIndex].Payload) > 0 {
				panic(m.Error(diag.ErrEnumWrongBindingCount, fmt.Sprintf("case '%s' carries a payload: construct it with arguments", m.Name())))
			}
			c.typing.SetMember(m, ast.MemberInfo{Kind: ast.MemberUnionCase, CaseIndex: int32(caseIndex)})
			return t
		}
		panic(m.Error(diag.ErrUnknownField, fmt.Sprintf("'%s.%s' is not a value", nt.Name(), m.Name())))
	}

	t := c.checkExpr(m.Obj())
	requireNotCown(m.Position(), ast.Content(t), "reading", m.Name())
	if rt, ok := ast.Content(t).(*ast.RecordTy); ok {
		if f := rt.Field(m.Name()); f != nil {
			if rt.Decl.Module != c.module && !f.Exported {
				panic(m.Error("E3014", fmt.Sprintf("field '%s.%s' is not exported", rt.Name(), f.Name)))
			}
			c.typing.SetMember(m, ast.MemberInfo{Kind: ast.MemberField, Field: f})
			return rt.FieldType(f)
		}
	}
	if et, ok := t.(*ast.EnumTy); ok && (m.Name() == "name" || m.Name() == "ordinal" || m.Name() == "rawValue") {
		switch m.Name() {
		case "name":
			c.typing.SetMember(m, ast.MemberInfo{Kind: ast.MemberEnumName})
			return ast.StringTy
		case "ordinal":
			c.typing.SetMember(m, ast.MemberInfo{Kind: ast.MemberEnumOrdinal})
			return c.countTy()
		}
		c.typing.SetMember(m, ast.MemberInfo{Kind: ast.MemberEnumRaw})
		for _, r := range et.Raw {
			if r.IsNeg() {
				return c.fullRangeTy()
			}
		}
		return c.countTy()
	}
	if _, ok := t.(*ast.UnionTy); ok && (m.Name() == "name" || m.Name() == "ordinal") {
		if m.Name() == "name" {
			c.typing.SetMember(m, ast.MemberInfo{Kind: ast.MemberUnionName})
			return ast.StringTy
		}
		c.typing.SetMember(m, ast.MemberInfo{Kind: ast.MemberUnionOrdinal})
		return c.countTy()
	}
	if nom, ok := t.(ast.NominalTy); ok {
		if _, isMethod := nom.MethodTable()[m.Name()]; isMethod {
			panic(m.Error(diag.ErrUnsupported, "a method value is not supported by minimaxon; call the method"))
		}
	}
	panic(m.Error(diag.ErrUnknownField, fmt.Sprintf("%s has no field '%s'", t.Name(), m.Name())))
}

// VisitIndexExpr types a subscript. An element of a primitive is a projection, never a value of its own: what it is
// used for decides whether it borrows the element to read it or to change it, and the ownership pass refuses to move
// one out.
func (c *Checker) VisitIndexExpr(x ast.IndexExpr) ast.Ty {
	t := ast.Content(c.checkExpr(x.Obj()))
	requireNotCown(x.Position(), t, "reading an element", "")
	bt, ok := t.(*ast.BuiltinTy)
	if !ok || !bt.Subscriptable() {
		panic(x.Error(diag.ErrTypeMismatch, fmt.Sprintf("%s has no elements to index: only an array, a map, a list or a pool does", t.Name())))
	}
	if x.Args().Len() != 1 {
		panic(x.Error(diag.ErrTypeMismatch, fmt.Sprintf("an index is one value, given %d", x.Args().Len())))
	}
	c.checkExpecting(x.Args().At(0), bt.Index())
	c.coerce(x.Args().At(0), bt.Index(), "the index of "+t.Name())
	return bt.Element()
}

func (c *Checker) VisitStructLitExpr(s ast.StructLitExpr) ast.Ty {
	var target ast.Ty
	if s.TypeName() == "Self" {
		if c.owner != nil {
			target = c.owner
		}
	} else if tb, ok := c.target(s.ID).(ast.TypeDeclNode); ok {
		target = tb.TypeBase().Ty
	}
	rt, ok := target.(*ast.RecordTy)
	if !ok {
		panic(s.Error(diag.ErrUnknownType, fmt.Sprintf("'%s' is not a type with fields", s.TypeName())))
	}
	if rt.Base().IsGeneric() {
		panic(s.Error(diag.ErrTypeArgumentCount, fmt.Sprintf("a literal of the generic '%s' is written 'Self{...}', which gives it its own type parameters", rt.Name())))
	}
	if c.owner != rt || c.fn == nil {
		panic(s.Error(diag.ErrConstructorRestriction, fmt.Sprintf("a '%s' literal is written only inside the methods of '%s'; call a factory", rt.Name(), rt.Name())))
	}
	fields := rt.FieldList()
	ordered := make([]ast.Expr, len(fields))
	c.typing.SetStructLit(s, ast.StructLitInfo{Record: rt, Ordered: ordered})
	for _, sf := range s.Fields().All() {
		f := rt.Field(sf.Name())
		if f == nil {
			panic(sf.Error(diag.ErrUnknownField, fmt.Sprintf("'%s' has no field '%s'", rt.Name(), sf.Name())))
		}
		if !ordered[f.Index].IsNil() {
			panic(sf.Error(diag.ErrDuplicateDefinition, fmt.Sprintf("field '%s' is given twice", sf.Name())))
		}
		c.checkExpecting(sf.Value(), rt.FieldType(f))
		c.coerce(sf.Value(), rt.FieldType(f), fmt.Sprintf("field '%s'", sf.Name()))
		ordered[f.Index] = sf.Value()
	}
	for _, f := range fields {
		if ordered[f.Index].IsNil() {
			if f.Default.IsNil() {
				panic(s.Error(diag.ErrFieldNotInitialized, fmt.Sprintf("field '%s' of '%s' is not initialized", f.Name, rt.Name())))
			}
			c.recordUses(f.Uses...)
		}
	}
	return rt
}

func (c *Checker) VisitCastExpr(cast ast.CastExpr) ast.Ty {
	target := c.resolve(cast.Target(), c.owner)
	ti, ok := target.(*ast.IntTy)
	if !ok {
		panic(cast.Error(diag.ErrUnsupported, fmt.Sprintf("a cast to %s is not supported by minimaxon", target.Name())))
	}
	src := c.checkExpr(cast.E())
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

func (c *Checker) VisitDiscardStmt(t ast.DiscardStmt) { c.checkExpr(t.Value()) }

func (c *Checker) VisitExprStmt(t ast.ExprStmt) {
	e := t.E()
	if try, ok := e.AsTryExpr(); ok {
		e = try.E()
	}
	switch e.Kind() {
	case ast.KindCallExpr, ast.KindPanicExpr:
	default:
		panic(t.Error(diag.ErrUnexpectedToken, "only a call can stand alone as a statement; bind the value with 'let', or discard it with '_ ='"))
	}
	c.checkExpr(t.E())
}

func (c *Checker) VisitIfStmt(t ast.IfStmt) {
	c.requireBool(t.Cond())
	c.checkBlock(t.Then())
	if t.HasElse() {
		c.checkBlock(t.Else())
	}
}

func (c *Checker) VisitWhileStmt(t ast.WhileStmt) {
	c.requireBool(t.Cond())
	c.pushBlock(t.Label(), checkerLoop, t.ID)
	c.checkBlock(t.Body())
	c.popBlock()
}

func (c *Checker) VisitMatchStmt(t ast.MatchStmt) { c.checkMatch(t.Match(), false) }

// VisitTryStmt types the body of a try block and its handler, which may name the error that failed.
func (c *Checker) VisitTryStmt(t ast.TryStmt) {
	c.checkBlock(t.Body())
	if !t.HasOtherwise() {
		return
	}
	c.pushScope()
	if t.Binding() != "" {
		c.typing.SetTryLocal(t, c.declareLocal(t.Binding(), c.project.Builtins.ArithmeticError, false, t.Position()))
	}
	c.checkStmts(t.Handler())
	c.popScope()
}

func (c *Checker) VisitBreakStmt(t ast.BreakStmt) {
	c.typing.SetBreakTarget(t, c.resolveJump(t.Stmt(), t.Label(), true))
}

func (c *Checker) VisitContinueStmt(t ast.ContinueStmt) {
	c.typing.SetContinueTarget(t, c.resolveJump(t.Stmt(), t.Label(), false))
}

func (c *Checker) requireBool(e ast.Expr) {
	if c.checkExpr(e) != ast.BoolTy {
		panic(e.Position().Error(diag.ErrTypeMismatch, fmt.Sprintf("a condition must be a bool, found %s", c.ty(e).Name())))
	}
}

func (c *Checker) VisitLetStmt(l ast.LetStmt) {
	if l.Name() == "_" {
		panic(l.Error(diag.ErrUnexpectedToken, "write '_ = call()' to discard a result"))
	}
	if l.Init().IsNil() {
		t := c.resolve(l.Type(), c.owner)
		c.typing.SetLetLocal(l, c.declareLocal(l.Name(), t, l.IsVar(), l.Position()))
		return
	}
	t := c.checkExpr(l.Init())
	c.requireSigned64(l.Init().Position(), t)
	if t == ast.VoidTy || t == ast.NeverTy {
		panic(l.Init().Position().Error(diag.ErrTypeMismatch, fmt.Sprintf("'%s' is bound to an expression with no value", l.Name())))
	}
	if l.IsVar() {
		_, isConst := t.(*ast.ConstIntTy)
		it, isInt := t.(*ast.IntTy)
		if isConst || (isInt && it.IsAnonymous()) {
			panic(l.Init().Position().Error(diag.ErrExpressionError, fmt.Sprintf("a var names its alias: write 'var %s = <value> as <Alias>'", l.Name())))
		}
	}
	c.typing.SetLetLocal(l, c.declareLocal(l.Name(), t, l.IsVar(), l.Position()))
}

func (c *Checker) VisitAssignStmt(a ast.AssignStmt) {
	var target ast.Ty
	switch a.Target().Kind() {
	case ast.KindNameExpr:
		t := a.Target().ToNameExpr()

		target = c.VisitNameExpr(t)
		c.typing.SetType(t.Expr(), target)
	case ast.KindMemberExpr:
		t := a.Target().ToMemberExpr()

		target = c.VisitMemberExpr(t)
		c.typing.SetType(t.Expr(), target)
		if c.typing.Member(t).Kind != ast.MemberField {
			panic(a.Error(diag.ErrAssignToImmutable, fmt.Sprintf("'%s' cannot be assigned", t.Name())))
		}
	case ast.KindIndexExpr:
		t := a.Target().ToIndexExpr()

		target = c.VisitIndexExpr(t)
		c.typing.SetType(t.Expr(), target)
	default:
		panic(a.Error(diag.ErrUnexpectedToken, "only a variable, a field or an element can be assigned"))
	}
	c.checkExpecting(a.Value(), target)
	c.coerce(a.Value(), target, "assignment")
}

func (c *Checker) VisitForStmt(f ast.ForStmt) {
	from := c.checkExpr(f.From())
	to := c.checkExpr(f.To())
	if !c.isInt(from) || !c.isInt(to) {
		panic(f.Error(diag.ErrTypeMismatch, fmt.Sprintf("a range needs integer bounds, found %s and %s", from.Name(), to.Name())))
	}
	var varType ast.Ty
	if c.namedInt(from) != nil || c.namedInt(to) != nil {
		varType = c.unifyInts(f.Position(), from, to)
	} else {
		c.requireSigned64(f.From().Position(), from)
		c.requireSigned64(f.To().Position(), to)
		lo, _ := c.rangeOf(from)
		_, hi := c.rangeOf(to)
		if !f.Inclusive() {
			hi = hi.Sub(ast.One)
		}
		varType = &ast.IntTy{Min: lo, Max: ast.MaxI(lo, hi)}
	}

	c.pushBlock(f.Label(), checkerLoop, f.ID)
	c.pushScope()
	c.typing.SetForLocal(f, c.declareLocal(f.Var(), varType, false, f.Position()))
	c.checkStmts(f.Body())
	c.popScope()
	c.popBlock()
}

func (c *Checker) VisitReturnStmt(r ast.ReturnStmt) {
	ret := c.returnType(c.fn)
	if ret == ast.VoidTy {
		if !r.Value().IsNil() {
			if c.behaviour != nil {
				panic(r.Error(diag.ErrTypeMismatch, "a behaviour returns nothing and nothing waits for it: pass a result on by scheduling another behaviour"))
			}
			panic(r.Error(diag.ErrTypeMismatch, fmt.Sprintf("'%s' returns nothing", c.fn.Name)))
		}
		return
	}
	if r.Value().IsNil() {
		panic(r.Error(diag.ErrMissingReturn, fmt.Sprintf("'%s' must return a %s", c.fn.Name, ret.Name())))
	}
	c.checkExpecting(r.Value(), ret)
	c.coerce(r.Value(), ret, "return value")
}

func (c *Checker) resolveJump(s ast.Stmt, label string, isBreak bool) ast.NodeID {
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
		panic(s.Position().Error(diag.ErrUnexpectedToken, fmt.Sprintf("'%s' outside a loop", word)))
	}

	for _, v := range slices.Backward(c.blocks) {
		if v.Label != label {
			continue
		}
		if !isBreak && v.Kind != checkerLoop {
			panic(s.Position().Error(diag.ErrUnexpectedToken, fmt.Sprintf("'continue' names '%s', which is not a loop", label)))
		}
		return v.Node
	}
	panic(s.Position().Error(diag.ErrUnexpectedToken, fmt.Sprintf("no enclosing loop or match is labelled '%s'", label)))
}

func (c *Checker) checkMatch(m ast.MatchCore, isExpr bool) ast.Ty {
	subjectTy := c.checkExpr(m.Subject())
	st := ast.Content(subjectTy)
	if m.Consumes() && ast.IsFrozen(subjectTy) {
		panic(m.Subject().Position().Error(diag.ErrMoveOutOfField, "the contents of a frozen value are shared with its other owners, so 'match consumes' cannot take them apart; match without 'consumes', or use 'thaw(...)' for an owned copy"))
	}
	requireNotCown(m.Subject().Position(), st, "matching on it", "")
	c.requireSigned64(m.Subject().Position(), st)
	_, isEnum := st.(*ast.EnumTy)
	_, isUnion := st.(*ast.UnionTy)
	isCases := isEnum || isUnion
	if !(c.isInt(st) || st == ast.StringTy || st == ast.BoolTy || isCases) {
		panic(m.Subject().Position().Error(diag.ErrTypeMismatch, fmt.Sprintf("cannot match on a value of %s", st.Name())))
	}

	expect := c.expect
	c.pushBlock(m.Label(), checkerMatch, m.ID)
	var result ast.Ty
	hasDefault := false
	seen := map[int]bool{}
	seenBools := map[bool]bool{}

	for _, arm := range m.Arms().All() {
		c.guard(func() {
			if arm.IsDefault() {
				if isCases && arm.Form() != ast.ArmPanic {
					panic(arm.Error(diag.ErrMatchDefaultEnumMustThrow, "on an enum or union, 'default' is 'default panic(...)'"))
				}
				hasDefault = true
			}

			c.pushScope()
			defer c.popScope()
			c.checkArm(arm, st, m.Consumes(), isExpr, seen, seenBools, &result, expect)
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

// checkArm checks one arm of a match. owned is whether the match consumes its subject, so each payload the arm binds is
// owned by the arm instead of borrowed from the subject.
func (c *Checker) checkArm(arm ast.MatchArm, st ast.Ty, owned, isExpr bool, seen map[int]bool, seenBools map[bool]bool, result *ast.Ty, expect ast.Ty) {
	for _, pat := range arm.Patterns().All() {
		c.checkPattern(pat, st, owned, arm.Patterns().Len(), seen, seenBools)
	}

	switch arm.Form() {
	case ast.ArmThen:
		if isExpr {
			panic(arm.Error(diag.ErrUnexpectedToken, "an arm of a match expression 'gives' a value"))
		}
		switch arm.Action().Kind() {
		case ast.KindIfStmt, ast.KindWhileStmt, ast.KindForStmt, ast.KindMatchStmt, ast.KindWhenStmt:
			panic(arm.Action().Position().Error(diag.ErrMatchBlockStatement, "a block statement cannot be a match arm's action; move it into a function and call that"))
		}
		c.checkStmt(arm.Action())
	case ast.ArmGives:
		if !isExpr {
			panic(arm.Error(diag.ErrUnexpectedToken, "an arm of a match statement uses 'then'"))
		}
		t := c.checkExpecting(arm.Value(), expect)
		if *result == nil {
			*result = t
		} else {
			*result = c.join(arm.Value().Position(), *result, t)
		}
	case ast.ArmPanic:
		c.checkExpr(arm.Value())
	}
}

func (c *Checker) checkExhaustive(m ast.MatchCore, st ast.Ty, hasDefault bool, seen map[int]bool, seenBools map[bool]bool) {
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

func (c *Checker) checkPattern(pat ast.Pattern, st ast.Ty, owned bool, altCount int, seen map[int]bool, seenBools map[bool]bool) {
	switch pat.Form() {
	case ast.PatternCase:
		info := c.typing.EditPattern(pat)
		switch t := st.(type) {
		case *ast.EnumTy:
			info.CaseIndex = t.CaseIndex(pat.CaseName())
			if info.CaseIndex < 0 {
				panic(pat.Error(diag.ErrEnumUnknownCase, fmt.Sprintf("'%s' has no case '%s'", t.Name(), pat.CaseName())))
			}
			if pat.HasBindings() {
				panic(pat.Error(diag.ErrEnumWrongBindingCount, "an enum case carries no payload to bind"))
			}
		case *ast.UnionTy:
			info.CaseIndex = t.CaseIndex(pat.CaseName())
			if info.CaseIndex < 0 {
				panic(pat.Error(diag.ErrEnumUnknownCase, fmt.Sprintf("'%s' has no case '%s'", t.Name(), pat.CaseName())))
			}
			payload := t.Payload(info.CaseIndex)
			if pat.HasBindings() {
				if pat.Bindings().Len() != len(payload) {
					panic(pat.Error(diag.ErrEnumWrongBindingCount, fmt.Sprintf("case '%s' carries %d value(s), %d bound", pat.CaseName(), len(payload), pat.Bindings().Len())))
				}
				if altCount > 1 {
					panic(pat.Error(diag.ErrEnumWrongBindingCount, "an arm that binds a payload covers exactly one case"))
				}
				for i := range payload {
					var bound *ast.Local
					if pat.Bindings().At(i) != "_" {
						bound = c.declareLocal(pat.Bindings().At(i), payload[i].Type, false, pat.Position())
						bound.Projected = payload[i].Type.IsManaged() && !owned
					}
					var id ast.LocalID
					if bound != nil {
						id = bound.ID()
					}
					info.Locals = append(info.Locals, id)
				}
			}
		default:
			panic(pat.Error(diag.ErrTypeMismatch, fmt.Sprintf("a case pattern needs an enum or union subject, found %s", st.Name())))
		}
		if seen[info.CaseIndex] {
			panic(pat.Error(diag.ErrDuplicateDefinition, fmt.Sprintf("case '%s' is matched twice", pat.CaseName())))
		}
		seen[info.CaseIndex] = true
	case ast.PatternLiteral:
		t := c.checkExpr(pat.Value())
		if bl, ok := pat.Value().AsBoolLit(); ok && st == ast.BoolTy {
			if seenBools[bl.Value()] {
				word := "False"
				if bl.Value() {
					word = "True"
				}
				panic(pat.Error(diag.ErrDuplicateDefinition, fmt.Sprintf("'%s' is matched twice", word)))
			}
			seenBools[bl.Value()] = true
			return
		}
		if sl, ok := pat.Value().AsStrLit(); ok && st == ast.StringTy && sl.IsPlain() {
			return
		}
		if _, isConst := t.(*ast.ConstIntTy); isConst && c.isInt(st) {
			var expected ast.Ty = st
			if _, stConst := st.(*ast.ConstIntTy); stConst {
				expected = ast.Anonymous64
			}
			c.coerce(pat.Value(), expected, "pattern")
			return
		}
		panic(pat.Error(diag.ErrTypeMismatch, fmt.Sprintf("a pattern of %s cannot match %s", t.Name(), st.Name())))
	case ast.PatternRange:
		lo := c.checkExpr(pat.Value())
		hi := c.checkExpr(pat.High())
		l, loConst := lo.(*ast.ConstIntTy)
		h, hiConst := hi.(*ast.ConstIntTy)
		if !c.isInt(st) || !loConst || !hiConst {
			panic(pat.Error(diag.ErrTypeMismatch, "a range pattern needs integer bounds and an integer subject"))
		}
		top := h.Value
		if !pat.Inclusive() {
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
	var site ast.Pos
	switch n := t.(type) {
	case *ast.RecordTy:
		site = n.Decl.Position()
	case *ast.UnionTy:
		site = n.Decl.Position()
	default:
		panic(diag.Internal("no declaration for %s", t.Name()))
	}
	growing := false
	if !c.reaches(self, self, &path, map[ast.Ty]bool{}, &growing) {
		return
	}
	if growing {
		panic(site.Position().Error(diag.ErrTypeCycle, fmt.Sprintf("type '%s' has no finite size: via %s it contains itself with a larger type argument each time",
			t.Name(), path[0])))
	}
	panic(site.Position().Error(diag.ErrTypeCycle, fmt.Sprintf("type '%s' contains a reference cycle (via %s)", t.Name(), strings.Join(path, " -> "))))
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
