package resolve

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
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
	scopes  []map[string]bool
	fields  map[string]bool
}

func NewResolver(p *ast.Project, m *ast.Module) *Resolver {
	return &Resolver{project: p, module: m, fields: map[string]bool{}}
}

func (r *Resolver) Run() {
	for _, d := range r.module.Decls {
		diag.Guard(&r.module.Errors, func() { r.resolveDecl(d) })
	}
}

func (r *Resolver) resolveDecl(d ast.Decl) {
	ast.VisitDecl(r, d)
}

func (r *Resolver) VisitFuncDecl(d *ast.FuncDecl) {
	r.resolveFunction(d, nil)
}

func (r *Resolver) VisitTypeDecl(d *ast.TypeDecl) {
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
}

func (r *Resolver) VisitEnumDecl(d *ast.EnumDecl) {
	for _, m := range d.Methods {
		r.resolveFunction(m, nil)
	}
}

func (r *Resolver) VisitUnionDecl(d *ast.UnionDecl) {
	for _, c := range d.Cases {
		for _, p := range c.Fields {
			r.resolveType(p.Type)
		}
	}
	for _, m := range d.Methods {
		r.resolveFunction(m, nil)
	}
}

func (r *Resolver) VisitConstDecl(d *ast.ConstDecl) {
	r.enter(map[string]bool{})
	d.Value = r.expr(d.Value)
}

func (r *Resolver) VisitAliasDecl(*ast.AliasDecl) {}

func (r *Resolver) enter(fieldNames map[string]bool) {
	r.scopes = []map[string]bool{{}}
	r.fields = fieldNames
}

func (r *Resolver) resolveFunction(f *ast.FuncDecl, instanceOf *ast.TypeDecl) {
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
		diag.Guard(&r.module.Errors, func() { r.stmt(s) })
	}
	r.popScope()
}

func (r *Resolver) stmt(s ast.Stmt) {
	ast.VisitStmt(r, s)
}

func (r *Resolver) VisitLetStmt(s *ast.LetStmt) {
	s.Init = r.expr(s.Init)
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
	s.Cond = r.expr(s.Cond)
	r.block(s.Then)
	if s.HasElse {
		r.block(s.Else)
	}
}

func (r *Resolver) VisitWhileStmt(s *ast.WhileStmt) {
	s.Cond = r.expr(s.Cond)
	r.block(s.Body)
}

func (r *Resolver) VisitForStmt(s *ast.ForStmt) {
	s.From = r.expr(s.From)
	s.To = r.expr(s.To)
	r.pushScope(s.Var)
	r.block(s.Body)
	r.popScope()
}

func (r *Resolver) VisitMatchStmt(s *ast.MatchStmt) {
	r.match(s.Match)
}

func (r *Resolver) VisitReturnStmt(s *ast.ReturnStmt) {
	if s.Value != nil {
		s.Value = r.expr(s.Value)
	}
}

func (r *Resolver) VisitBreakStmt(*ast.BreakStmt) {}

func (r *Resolver) VisitContinueStmt(*ast.ContinueStmt) {}

func (r *Resolver) match(m *ast.MatchCore) {
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
		e.Target = r.take(r.bare(e.Name), e)
	}
	return e
}

func (r *Resolver) VisitMemberExpr(e *ast.MemberExpr) ast.Expr {
	return r.member(e)
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
	for _, a := range e.Args {
		a.Value = r.expr(a.Value)
	}
	return e
}

func (r *Resolver) VisitStructLitExpr(e *ast.StructLitExpr) ast.Expr {
	if e.TypeName != "Self" {
		e.Target = r.take(r.bare(e.TypeName), e)
	}
	fields := make([]ast.StructField, len(e.Fields))
	for i, f := range e.Fields {
		fields[i] = ast.StructField{Name: f.Name, Value: r.expr(f.Value), Site: f.Site}
	}
	e.Fields = fields
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
			head.Target = r.take(bare, head)
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
	result := &ast.NameExpr{Name: target.Base().Name, Target: target}
	result.File, result.Line, result.Col = head.File, head.Line, head.Col
	var out ast.Expr = result
	for _, s := range segments[used:] {
		p := s.Site.P()
		next := &ast.MemberExpr{Obj: out, Name: s.Name}
		next.File, next.Line, next.Col = p.File, p.Line, p.Col
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
	if len(tr.Qualifier) == 0 {
		if primitiveTypes[tr.Name] {
			return
		}
		found := r.take(r.bare(tr.Name), tr)
		if found != nil && !isType(found) {
			panic(tr.Error(diag.ErrUnknownType, "'"+tr.Name+"' is not a type"))
		}
		tr.Target = found
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
	tr.Target = decl
}

func (r *Resolver) visible(d ast.Decl) bool {
	b := d.Base()
	return b.Module == r.module || b.IsExported()
}

func hidden(d ast.Decl, site ast.Node) *diag.CompileError {
	b := d.Base()
	return site.P().Error("E3008", "'"+b.Name+"' is not exported from module "+b.Module.Display())
}

func (r *Resolver) take(found lookup, site ast.Node) ast.Decl {
	if found.Error != nil {
		p := site.P()
		panic(diag.NewError(found.Error.Code, found.Error.Message, p.File, p.Line, p.Col))
	}
	if found.Decl != nil {
		dm := found.Decl.Base().Module
		if dm != r.module && !dm.IsStdlib {
			if _, seen := r.module.DepSites[dm]; !seen {
				r.module.DepSites[dm] = site
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
