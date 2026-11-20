package resolve

import (
	"cmp"
	"fmt"
	"maps"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
	"slices"
	"strings"
)

type lookup struct {
	Decl  ast.Decl
	Error *diag.CompileError
}

var primitiveTypes = map[string]bool{"bool": true, "String": true, "Self": true, "int": true, "float": true}

type Resolver struct {
	project *ast.Project
	module  *ast.Module
	// result is the module's own result: the resolver records its errors, what each node names and, when it is done,
	// its dependencies.
	result *ast.ModuleResult
	// deps holds the first place each other module is named.
	deps   map[ast.ModuleID]ast.Pos
	scopes []map[string]bool
	fields map[string]bool
	// typeParams are the slots, in the module's Resolution, of the type parameters in scope by name.
	typeParams map[string]int
}

func NewResolver(p *ast.Project, m *ast.Module, result *ast.ModuleResult) *Resolver {
	return &Resolver{project: p, module: m, result: result, fields: map[string]bool{}}
}

// Run resolves the module's declarations, then records its dependencies in the module's result.
func (r *Resolver) Run() {
	r.result.Resolution.Reset(r.module.Nodes.Count())
	for _, d := range r.module.Decls {
		r.typeParams = nil
		diag.Guard(&r.result.Errors, func() { r.resolveDecl(d) })
	}
	r.result.Resolution.Finish()
	r.result.Deps = r.sortedDeps()
}

func (r *Resolver) sortedDeps() []ast.ModuleDep {
	if len(r.deps) == 0 {
		return nil
	}
	deps := make([]ast.ModuleDep, 0, len(r.deps))
	for id, site := range r.deps {
		deps = append(deps, ast.ModuleDep{Module: id, Site: site})
	}
	slices.SortFunc(deps, func(a, b ast.ModuleDep) int { return cmp.Compare(a.Module, b.Module) })
	return deps
}

func (r *Resolver) resolveDecl(d ast.Decl) {
	ast.VisitDecl(r, d)
}

func (r *Resolver) VisitFuncDecl(d *ast.FuncDecl) {
	r.resolveFunction(d, nil)
}

func (r *Resolver) VisitTypeDecl(d *ast.TypeDecl) {
	checkEnd(d.Name, d.End)
	outer := r.enterTypeParams(d.Name, d.TypeParams, d.Where)
	r.resolveConforms(d.Conforms)
	for _, f := range d.Fields {
		if f.Type != nil {
			r.resolveType(f.Type)
		}
		if f.Default != nil {
			r.enter(map[string]bool{})
			f.Default = r.expr(f.Default)
		}
	}
	for _, m := range d.Methods {
		if m.IsStatic {
			r.resolveFunction(m, nil)
		} else {
			r.resolveFunction(m, d)
		}
	}
	r.typeParams = outer
}

func (r *Resolver) VisitEnumDecl(d *ast.EnumDecl) {
	checkEnd(d.Name, d.End)
	r.resolveConforms(d.Conforms)
	for _, m := range d.Methods {
		r.resolveFunction(m, nil)
	}
}

func (r *Resolver) VisitUnionDecl(d *ast.UnionDecl) {
	checkEnd(d.Name, d.End)
	outer := r.enterTypeParams(d.Name, d.TypeParams, d.Where)
	r.resolveConforms(d.Conforms)
	for _, c := range d.Cases {
		for _, p := range c.Fields {
			r.resolveType(p.Type)
		}
	}
	for _, m := range d.Methods {
		r.resolveFunction(m, nil)
	}
	r.typeParams = outer
}

func (r *Resolver) VisitInterfaceDecl(d *ast.InterfaceDecl) {
	checkEnd(d.Name, d.End)
	for _, m := range d.Methods {
		for _, p := range m.Params {
			r.resolveType(p.Type)
		}
		if m.Returns != nil {
			r.resolveType(m.Returns)
		}
	}
}

// enterTypeParams puts the type parameters of the declaration named owner in scope and returns the scope to restore
// afterwards. Each constraint of its 'where' clause binds one of them, and its bounds are resolved with the
// parameters in scope. A method's constraint may also bind a parameter of its type, which is in scope already.
func (r *Resolver) enterTypeParams(owner string, params []*ast.TypeParam, where []*ast.Constraint) map[string]int {
	outer := r.typeParams
	if len(params) > 0 {
		r.typeParams = make(map[string]int, len(outer)+len(params))
		maps.Copy(r.typeParams, outer)
		for _, p := range params {
			r.typeParams[p.Name] = r.result.Resolution.AddTypeParam(p)
		}
	}
	for _, c := range where {
		r.result.Resolution.BindTypeParam(c.ID, r.constrained(owner, where, c))
		for _, b := range c.Bounds {
			r.resolveType(b)
		}
	}
	return outer
}

// constrained is the slot of the type parameter a constraint binds: one the declaration itself declares, or one of the
// type the declaration is a method of, which the type parameters in scope hold with the declaration's own first. It
// constrains each once.
func (r *Resolver) constrained(owner string, where []*ast.Constraint, c *ast.Constraint) int {
	target, declared := r.typeParams[c.Param]
	if !declared {
		panic(c.Error(diag.ErrUnknownTypeParam, fmt.Sprintf("'%s' is not a type parameter of '%s': declare it with 'uses %s', or constrain one that '%s' declares",
			c.Param, owner, c.Param, owner)))
	}
	if first := slices.IndexFunc(where, func(o *ast.Constraint) bool { return o.Param == c.Param }); where[first] != c {
		panic(c.Error(diag.ErrDuplicateDefinition, fmt.Sprintf("'%s' is constrained twice: join its bounds with 'and', as 'where %s implements A and B'", c.Param, c.Param)))
	}
	return target
}

func (r *Resolver) resolveConforms(interfaces []*ast.TypeRef) {
	for _, tr := range interfaces {
		r.resolveType(tr)
	}
}

func (r *Resolver) VisitConstDecl(d *ast.ConstDecl) {
	r.enter(map[string]bool{})
	d.Value = r.expr(d.Value)
}

func (r *Resolver) VisitAliasDecl(d *ast.AliasDecl) {
	if d.Instance != nil {
		r.resolveType(d.Instance)
	}
}

// checkEnd refuses an 'end' whose label is not the block's.
func checkEnd(label string, end *ast.EndLabel) {
	if end != nil && end.Name != label {
		panic(end.Error(diag.ErrMismatchedEndLabel, fmt.Sprintf("'end' names '%s', but the block is '%s'", end.Name, label)))
	}
}

func (r *Resolver) enter(fieldNames map[string]bool) {
	r.scopes = []map[string]bool{{}}
	r.fields = fieldNames
}

func (r *Resolver) resolveFunction(f *ast.FuncDecl, instanceOf *ast.TypeDecl) {
	outer := r.enterTypeParams(f.Name, f.TypeParams, f.Where)
	r.resolveBody(f, instanceOf)
	r.typeParams = outer
}

func (r *Resolver) resolveBody(f *ast.FuncDecl, instanceOf *ast.TypeDecl) {
	checkEnd(f.Name, f.End)
	for _, p := range f.Params {
		r.resolveType(p.Type)
	}
	if f.Returns != nil {
		r.resolveType(f.Returns)
	}
	names := map[string]bool{}
	if instanceOf != nil {
		for _, fd := range instanceOf.Fields {
			names[fd.Name] = true
		}
	}
	r.enter(names)
	for _, p := range f.Params {
		r.scopes[len(r.scopes)-1][p.Name] = true
	}
	r.block(f.Body)
}

func (r *Resolver) isBound(name string) bool {
	for _, s := range r.scopes {
		if s[name] {
			return true
		}
	}
	return r.fields[name]
}

func (r *Resolver) pushScope(names ...string) {
	scope := make(map[string]bool, len(names))
	for _, n := range names {
		scope[n] = true
	}
	r.scopes = append(r.scopes, scope)
}

func (r *Resolver) popScope() {
	r.scopes = r.scopes[:len(r.scopes)-1]
}

func (r *Resolver) block(body []ast.Stmt) {
	r.pushScope()
	for _, s := range body {
		diag.Guard(&r.result.Errors, func() { r.stmt(s) })
	}
	r.popScope()
}

func (r *Resolver) stmt(s ast.Stmt) {
	ast.VisitStmt(r, s)
}

func (r *Resolver) VisitLetStmt(s *ast.LetStmt) {
	if s.Init != nil {
		s.Init = r.expr(s.Init)
	} else {
		r.resolveType(s.Type)
	}
	r.scopes[len(r.scopes)-1][s.Name] = true
}

func (r *Resolver) VisitAssignStmt(s *ast.AssignStmt) {
	s.Target = r.expr(s.Target)
	s.Value = r.expr(s.Value)
}

func (r *Resolver) VisitDiscardStmt(s *ast.DiscardStmt) {
	s.Value = r.expr(s.Value)
}

func (r *Resolver) VisitExprStmt(s *ast.ExprStmt) {
	s.E = r.expr(s.E)
}

func (r *Resolver) VisitIfStmt(s *ast.IfStmt) {
	checkEnd(s.Label, s.End)
	checkEnd(s.ElseLabel, s.ElseEnd)
	s.Cond = r.expr(s.Cond)
	r.block(s.Then)
	if s.HasElse {
		r.block(s.Else)
	}
}

func (r *Resolver) VisitWhileStmt(s *ast.WhileStmt) {
	checkEnd(s.Label, s.End)
	s.Cond = r.expr(s.Cond)
	r.block(s.Body)
}

func (r *Resolver) VisitForStmt(s *ast.ForStmt) {
	checkEnd(s.Label, s.End)
	s.From = r.expr(s.From)
	s.To = r.expr(s.To)
	r.pushScope(s.Var)
	r.block(s.Body)
	r.popScope()
}

func (r *Resolver) VisitMatchStmt(s *ast.MatchStmt) {
	r.match(s.Match)
}

// VisitWhenStmt resolves the cowns the behaviour names where it is written, and its body with the names that give
// access to their values in scope.
func (r *Resolver) VisitWhenStmt(s *ast.WhenStmt) {
	checkEnd(s.Label, s.End)
	for _, c := range s.Cowns {
		r.VisitNameExpr(c)
	}
	r.pushScope(s.Names...)
	r.block(s.Body)
	r.popScope()
}

func (r *Resolver) VisitReturnStmt(s *ast.ReturnStmt) {
	if s.Value != nil {
		s.Value = r.expr(s.Value)
	}
}

func (r *Resolver) VisitBreakStmt(*ast.BreakStmt) {}

func (r *Resolver) VisitContinueStmt(*ast.ContinueStmt) {}

func (r *Resolver) match(m *ast.MatchCore) {
	checkEnd(m.Label, m.End)
	m.Subject = r.expr(m.Subject)
	for _, arm := range m.Arms {
		var names []string
		for _, p := range arm.Patterns {
			names = append(names, p.Bindings...)
		}
		r.pushScope(names...)
		if arm.Action != nil {
			r.stmt(arm.Action)
		}
		if arm.Value != nil {
			arm.Value = r.expr(arm.Value)
		}
		r.popScope()
	}
}

func (r *Resolver) expr(e ast.Expr) ast.Expr {
	return ast.VisitExprResult[ast.Expr](r, e)
}

func (r *Resolver) VisitIntLit(e *ast.IntLit) ast.Expr { return e }

func (r *Resolver) VisitBoolLit(e *ast.BoolLit) ast.Expr { return e }

func (r *Resolver) VisitSelfExpr(e *ast.SelfExpr) ast.Expr { return e }

func (r *Resolver) VisitNameExpr(e *ast.NameExpr) ast.Expr {
	if !r.isBound(e.Name) && e.Name != "Self" {
		r.bindDecl(e.ID, r.take(r.bare(e.Name), e))
	}
	return e
}

func (r *Resolver) VisitMemberExpr(e *ast.MemberExpr) ast.Expr {
	return r.member(e)
}

func (r *Resolver) VisitIndexExpr(e *ast.IndexExpr) ast.Expr {
	e.Obj = r.expr(e.Obj)
	for i, a := range e.Args {
		e.Args[i] = r.expr(a)
	}
	return e
}

func (r *Resolver) VisitStrLit(e *ast.StrLit) ast.Expr {
	for i, p := range e.Parts {
		if part, ok := p.(ast.Expr); ok {
			e.Parts[i] = r.expr(part)
		}
	}
	return e
}

func (r *Resolver) VisitBinaryExpr(e *ast.BinaryExpr) ast.Expr {
	e.L = r.expr(e.L)
	e.R = r.expr(e.R)
	return e
}

func (r *Resolver) VisitUnaryExpr(e *ast.UnaryExpr) ast.Expr {
	e.E = r.expr(e.E)
	return e
}

func (r *Resolver) VisitCondExpr(e *ast.CondExpr) ast.Expr {
	e.Cond = r.expr(e.Cond)
	e.Then = r.expr(e.Then)
	e.Else = r.expr(e.Else)
	return e
}

func (r *Resolver) VisitCallExpr(e *ast.CallExpr) ast.Expr {
	e.Callee = r.expr(e.Callee)
	constructsCown := false
	if n, ok := e.Callee.(*ast.NameExpr); ok {
		constructsCown = ast.IsCownConstructor(r.result.Resolution.Decl(r.project, n.ID))
	}
	for _, a := range e.Args {
		// The argument of dropWith: is the name of a method of the content's type, which only its type can resolve.
		if constructsCown && a.Label == ast.DropWithLabel {
			continue
		}
		a.Value = r.expr(a.Value)
	}
	return e
}

func (r *Resolver) VisitStructLitExpr(e *ast.StructLitExpr) ast.Expr {
	if e.TypeName != "Self" {
		r.bindDecl(e.ID, r.take(r.bare(e.TypeName), e))
	}
	for _, f := range e.Fields {
		f.Value = r.expr(f.Value)
	}
	return e
}

func (r *Resolver) VisitCastExpr(e *ast.CastExpr) ast.Expr {
	e.E = r.expr(e.E)
	r.resolveType(e.Target)
	return e
}

func (r *Resolver) VisitMatchExpr(e *ast.MatchExpr) ast.Expr {
	r.match(e.Match)
	return e
}

func (r *Resolver) VisitPanicExpr(e *ast.PanicExpr) ast.Expr {
	e.Message = r.expr(e.Message)
	return e
}

func isType(d ast.Decl) bool {
	_, ok := d.(ast.TypeDeclNode)
	return ok
}

func declaresMember(d ast.Decl, name string) bool {
	return ast.VisitDeclState[string, bool](memberDeclarations{}, d, name)
}

// memberDeclarations answers whether a declaration declares a member that a qualified name can reach through it:
// a static function, or a case.
type memberDeclarations struct{}

func (memberDeclarations) VisitTypeDecl(d *ast.TypeDecl, name string) bool {
	for _, m := range d.Methods {
		if m.IsStatic && m.Name == name {
			return true
		}
	}
	return false
}

func (memberDeclarations) VisitEnumDecl(d *ast.EnumDecl, name string) bool {
	for _, c := range d.Cases {
		if c.Name == name {
			return true
		}
	}
	return false
}

func (memberDeclarations) VisitUnionDecl(d *ast.UnionDecl, name string) bool {
	for _, c := range d.Cases {
		if c.Name == name {
			return true
		}
	}
	return false
}

func (memberDeclarations) VisitInterfaceDecl(*ast.InterfaceDecl, string) bool { return false }

func (memberDeclarations) VisitFuncDecl(*ast.FuncDecl, string) bool { return false }

func (memberDeclarations) VisitConstDecl(*ast.ConstDecl, string) bool { return false }

func (memberDeclarations) VisitAliasDecl(*ast.AliasDecl, string) bool { return false }

func (r *Resolver) isModuleRoot(name string) bool {
	if name == "export" || name == "stdlib" {
		return true
	}
	_, ok := r.project.Root.Children[name]
	return ok
}

type segment struct {
	Name string
	Site ast.Node
}

func (r *Resolver) member(m *ast.MemberExpr) ast.Expr {
	var segments []segment
	var cur ast.Expr = m
	for {
		mm, ok := cur.(*ast.MemberExpr)
		if !ok {
			break
		}
		segments = append(segments, segment{mm.Name, mm})
		cur = mm.Obj
	}
	for i, j := 0, len(segments)-1; i < j; i, j = i+1, j-1 {
		segments[i], segments[j] = segments[j], segments[i]
	}
	head, ok := cur.(*ast.NameExpr)
	if !ok || r.isBound(head.Name) || head.Name == "Self" {
		m.Obj = r.expr(m.Obj)
		return m
	}
	bare := r.bare(head.Name)
	decl := bare.Decl
	if decl != nil {
		_, isConst := decl.(*ast.ConstDecl)
		if isConst || (isType(decl) && declaresMember(decl, segments[0].Name)) || !r.isModuleRoot(head.Name) {
			r.bindDecl(head.ID, r.take(bare, head))
			return m
		}
	}
	if !r.isModuleRoot(head.Name) {
		return m
	}

	names := make([]string, len(segments))
	sites := make([]ast.Node, len(segments))
	for i, s := range segments {
		names[i] = s.Name
		sites[i] = s.Site
	}
	target, used := r.qualified(head.Name, head, names, sites)
	result := r.module.Nodes.NewNameExpr()
	result.Name = target.Base().Name
	result.Pos = head.Pos
	r.bindDecl(result.ID, target)
	var out ast.Expr = result
	for _, s := range segments[used:] {
		next := r.module.Nodes.NewMemberExpr()
		next.Obj, next.Name = out, s.Name
		next.Pos = *s.Site.P()
		out = next
	}
	return out
}

func (r *Resolver) qualified(head string, headSite ast.Node, rest []string, sites []ast.Node) (ast.Decl, int) {
	var mod *ast.Module
	switch head {
	case "export":
		mod = r.project.Root
	case "stdlib":
		mod = r.project.Stdlib
	default:
		mod = r.project.Root.Children[head]
	}
	i := 0
	for i < len(rest) {
		if _, named := mod.Names[rest[i]]; named {
			break
		}
		child, ok := mod.Children[rest[i]]
		if !ok {
			break
		}
		mod = child
		i++
	}
	if i == len(rest) {
		panic(headSite.P().Error(diag.ErrExpressionError, "'"+head+"."+strings.Join(rest, ".")+"' is a module, not a value"))
	}
	decl, ok := mod.Names[rest[i]]
	if !ok {
		panic(sites[i].P().Error("E2073", "module "+mod.Display()+" declares no '"+rest[i]+"'"))
	}
	found := lookup{Decl: decl}
	if !r.visible(decl) {
		found.Error = hidden(decl, sites[i])
	}
	return r.take(found, sites[i]), i + 1
}

func (r *Resolver) resolveType(tr *ast.TypeRef) {
	for _, a := range tr.TypeArgs {
		r.resolveType(a)
	}
	if len(tr.Qualifier) == 0 {
		if slot, ok := r.typeParams[tr.Name]; ok {
			r.result.Resolution.BindTypeParam(tr.ID, slot)
			return
		}
		if primitiveTypes[tr.Name] {
			return
		}
		found := r.take(r.bare(tr.Name), tr)
		if found != nil && !isType(found) {
			panic(tr.Error(diag.ErrUnknownType, "'"+tr.Name+"' is not a type"))
		}
		r.bindDecl(tr.ID, found)
		return
	}
	if !r.isModuleRoot(tr.Qualifier[0]) {
		panic(tr.Error(diag.ErrUnknownType, "no module '"+tr.Qualifier[0]+"'"))
	}
	path := append(append([]string{}, tr.Qualifier[1:]...), tr.Name)
	sites := make([]ast.Node, len(path))
	for i := range sites {
		sites[i] = tr
	}
	decl, used := r.qualified(tr.Qualifier[0], tr, path, sites)
	if used != len(path) || !isType(decl) {
		panic(tr.Error(diag.ErrUnknownType, "'"+strings.Join(tr.Qualifier, ".")+"."+tr.Name+"' is not a type"))
	}
	r.bindDecl(tr.ID, decl)
}

func (r *Resolver) visible(d ast.Decl) bool {
	b := d.Base()
	return b.Module == r.module || b.IsExported()
}

func hidden(d ast.Decl, site ast.Node) *diag.CompileError {
	b := d.Base()
	return site.P().Error("E3008", "'"+b.Name+"' is not exported from module "+b.Module.Display())
}

// bindDecl records that a node names d, unless it names no declaration, as 'print' does not.
func (r *Resolver) bindDecl(n ast.NodeID, d ast.Decl) {
	if d != nil {
		r.result.Resolution.BindDecl(n, d)
	}
}

func (r *Resolver) take(found lookup, site ast.Node) ast.Decl {
	if found.Error != nil {
		panic(site.P().Error(found.Error.Code, found.Error.Message))
	}
	if found.Decl != nil {
		dm := found.Decl.Base().Module
		if dm != r.module && !dm.IsStdlib {
			if r.deps == nil {
				r.deps = map[ast.ModuleID]ast.Pos{}
			}
			if _, seen := r.deps[dm.ID]; !seen {
				r.deps[dm.ID] = *site.P()
			}
		}
	}
	return found.Decl
}

func (r *Resolver) bare(name string) lookup {
	if own, ok := r.module.Names[name]; ok {
		return lookup{Decl: own}
	}
	if root, ok := r.project.Root.Names[name]; ok {
		if r.visible(root) {
			return lookup{Decl: root}
		}
		return lookup{Decl: root, Error: hidden(root, root)}
	}
	var candidates, visible []ast.Decl
	for _, d := range r.project.ByName[name] {
		dm := d.Base().Module
		if dm != r.module && dm != r.project.Root {
			candidates = append(candidates, d)
		}
	}
	for _, d := range candidates {
		if r.visible(d) {
			visible = append(visible, d)
		}
	}
	if len(visible) == 1 {
		return lookup{Decl: visible[0]}
	}
	if len(visible) > 1 {
		code := "E3095"
		if isType(visible[0]) {
			code = "E3063"
		}
		where := make([]string, len(visible))
		for i, d := range visible {
			where[i] = d.Base().Module.Path + "." + name
		}
		return lookup{Error: diag.NewError(code, "'"+name+"' is ambiguous: qualify it as one of "+strings.Join(where, ", "), "", 0, 0)}
	}
	if len(candidates) > 0 {
		return lookup{Error: hidden(candidates[0], candidates[0])}
	}
	return lookup{Decl: r.project.Stdlib.Names[name]}
}
