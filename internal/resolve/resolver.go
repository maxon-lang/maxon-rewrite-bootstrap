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
	// whenDepth is how many behaviours the name being resolved is in the body of.
	whenDepth int
}

func NewResolver(p *ast.Project, m *ast.Module, result *ast.ModuleResult) *Resolver {
	return &Resolver{project: p, module: m, result: result, fields: map[string]bool{}}
}

// Run resolves the module's declarations, then records its dependencies in the module's result.
func (r *Resolver) Run() {
	r.result.Resolution.Reset(r.module.Tree.Count())
	for _, d := range r.module.Decls {
		r.typeParams = nil
		diag.Guard(&r.result.Errors, func() { r.resolveDecl(d) })
	}
	r.result.Resolution.Finish(r.module.Tree.Count())
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
		if !f.Type.IsNil() {
			r.resolveType(f.Type)
		}
		if !f.Default.IsNil() {
			r.enter(map[string]bool{})
			r.expr(f.Default)
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
		if !m.Returns.IsNil() {
			r.resolveType(m.Returns)
		}
		if !m.Throws.IsNil() {
			r.resolveType(m.Throws)
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
		for _, b := range c.Bounds.All() {
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

func (r *Resolver) resolveConforms(interfaces ast.TypeRefList) {
	for _, tr := range interfaces.All() {
		r.resolveType(tr)
	}
}

func (r *Resolver) VisitConstDecl(d *ast.ConstDecl) {
	r.enter(map[string]bool{})
	r.expr(d.Value)
}

func (r *Resolver) VisitAliasDecl(d *ast.AliasDecl) {
	if !d.Instance.IsNil() {
		r.resolveType(d.Instance)
	}
}

// checkEnd refuses an 'end' whose label is not the block's.
func checkEnd(label string, end ast.EndLabel) {
	if !end.IsNil() && end.Name() != label {
		panic(end.Error(diag.ErrMismatchedEndLabel, fmt.Sprintf("'end' names '%s', but the block is '%s'", end.Name(), label)))
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
	if !f.Returns.IsNil() {
		r.resolveType(f.Returns)
	}
	if !f.Throws.IsNil() {
		r.resolveType(f.Throws)
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

func (r *Resolver) block(body ast.StmtList) {
	r.pushScope()
	for _, s := range body.All() {
		diag.Guard(&r.result.Errors, func() { r.stmt(s) })
	}
	r.popScope()
}

func (r *Resolver) stmt(s ast.Stmt) {
	ast.VisitStmt(r, s)
}

func (r *Resolver) VisitLetStmt(s ast.LetStmt) {
	if !s.Init().IsNil() {
		r.expr(s.Init())
	} else {
		r.resolveType(s.Type())
	}
	r.scopes[len(r.scopes)-1][s.Name()] = true
}

func (r *Resolver) VisitAssignStmt(s ast.AssignStmt) {
	r.expr(s.Target())
	r.expr(s.Value())
}

func (r *Resolver) VisitDiscardStmt(s ast.DiscardStmt) {
	r.expr(s.Value())
}

func (r *Resolver) VisitExprStmt(s ast.ExprStmt) {
	r.expr(s.E())
}

func (r *Resolver) VisitIfStmt(s ast.IfStmt) {
	checkEnd(s.Label(), s.End())
	checkEnd(s.ElseLabel(), s.ElseEnd())
	r.expr(s.Cond())
	r.block(s.Then())
	if s.HasElse() {
		r.block(s.Else())
	}
}

func (r *Resolver) VisitWhileStmt(s ast.WhileStmt) {
	checkEnd(s.Label(), s.End())
	r.expr(s.Cond())
	r.block(s.Body())
}

func (r *Resolver) VisitForStmt(s ast.ForStmt) {
	checkEnd(s.Label(), s.End())
	r.expr(s.From())
	r.expr(s.To())
	r.pushScope(s.Var())
	r.block(s.Body())
	r.popScope()
}

func (r *Resolver) VisitMatchStmt(s ast.MatchStmt) {
	r.match(s.Match())
}

// VisitWhenStmt resolves the cowns the behaviour names where it is written, and its body with the names that give
// access to their values in scope.
func (r *Resolver) VisitWhenStmt(s ast.WhenStmt) {
	checkEnd(s.Label(), s.End())
	for _, c := range s.Cowns().All() {
		r.VisitNameExpr(c)
	}
	r.pushScope(s.Names().Strings()...)
	r.whenDepth++
	r.block(s.Body())
	r.whenDepth--
	r.popScope()
}

// VisitTryStmt resolves the body, and the handler with the error it binds in scope.
func (r *Resolver) VisitTryStmt(s ast.TryStmt) {
	checkEnd(s.Label(), s.End())
	r.block(s.Body())
	if s.HasOtherwise() {
		checkEnd(s.HandlerLabel(), s.HandlerEnd())
		var names []string
		if s.Binding() != "" {
			names = append(names, s.Binding())
		}
		r.pushScope(names...)
		r.block(s.Handler())
		r.popScope()
	}
}

func (r *Resolver) VisitReturnStmt(s ast.ReturnStmt) {
	if !s.Value().IsNil() {
		r.expr(s.Value())
	}
}

func (r *Resolver) VisitBreakStmt(ast.BreakStmt) {}

func (r *Resolver) VisitContinueStmt(ast.ContinueStmt) {}

func (r *Resolver) match(m ast.MatchCore) {
	checkEnd(m.Label(), m.End())
	r.expr(m.Subject())
	for _, arm := range m.Arms().All() {
		var names []string
		for _, p := range arm.Patterns().All() {
			names = append(names, p.Bindings().Strings()...)
		}
		r.pushScope(names...)
		if !arm.Action().IsNil() {
			r.stmt(arm.Action())
		}
		if !arm.Value().IsNil() {
			r.expr(arm.Value())
		}
		r.popScope()
	}
}

func (r *Resolver) expr(e ast.Expr) {
	ast.VisitExpr(r, e)
}

func (r *Resolver) VisitIntLit(e ast.IntLit) {}

func (r *Resolver) VisitBoolLit(e ast.BoolLit) {}

func (r *Resolver) VisitSelfExpr(e ast.SelfExpr) {}

// VisitNameExpr binds a name that is not a variable. A variable named in the body of a behaviour may be one that the
// behaviour takes from the function around it, once for each behaviour it passes, so the name gets the nodes that stand
// for it in each of them (see ast.Capture); the checker, which knows which names take a variable, needs them made already.
func (r *Resolver) VisitNameExpr(e ast.NameExpr) {
	switch {
	case e.Name() == "Self":
	case !r.isBound(e.Name()):
		r.bindDecl(e.ID, r.take(r.bare(e.Name()), e.Position()))
	case r.whenDepth > 0:
		r.module.Tree.AppendCaptureRefs(e, r.whenDepth)
	}
}

func (r *Resolver) VisitMemberExpr(e ast.MemberExpr) {
	r.member(e)
}

func (r *Resolver) VisitIndexExpr(e ast.IndexExpr) {
	r.expr(e.Obj())
	for _, a := range e.Args().All() {
		r.expr(a)
	}
}

func (r *Resolver) VisitStrLit(e ast.StrLit) {
	for _, part := range e.Parts().All() {
		if !part.Expr.IsNil() {
			r.expr(part.Expr)
		}
	}
}

func (r *Resolver) VisitBinaryExpr(e ast.BinaryExpr) {
	r.expr(e.L())
	r.expr(e.R())
}

func (r *Resolver) VisitUnaryExpr(e ast.UnaryExpr) {
	r.expr(e.E())
}

func (r *Resolver) VisitCondExpr(e ast.CondExpr) {
	r.expr(e.Cond())
	r.expr(e.Then())
	r.expr(e.Else())
}

func (r *Resolver) VisitTryExpr(e ast.TryExpr) {
	r.expr(e.E())
	switch {
	case !e.Value().IsNil():
		r.expr(e.Value())
	case !e.Ret().IsNil():
		r.VisitReturnStmt(e.Ret())
	}
}

func (r *Resolver) VisitCallExpr(e ast.CallExpr) {
	r.expr(e.Callee())
	constructsCown := false
	if n, ok := e.Callee().AsNameExpr(); ok {
		constructsCown = ast.IsCownConstructor(r.result.Resolution.Decl(r.project, n.ID))
	}
	for _, a := range e.Args().All() {
		// The argument of dropWith: is the name of a method of the content's type, which only its type can resolve.
		if constructsCown && a.Label() == ast.DropWithLabel {
			continue
		}
		r.expr(a.Value())
	}
}

func (r *Resolver) VisitStructLitExpr(e ast.StructLitExpr) {
	if e.TypeName() != "Self" {
		r.bindDecl(e.ID, r.take(r.bare(e.TypeName()), e.Position()))
	}
	for _, f := range e.Fields().All() {
		r.expr(f.Value())
	}
}

func (r *Resolver) VisitCastExpr(e ast.CastExpr) {
	r.expr(e.E())
	r.resolveType(e.Target())
}

func (r *Resolver) VisitMatchExpr(e ast.MatchExpr) {
	r.match(e.Match())
}

func (r *Resolver) VisitPanicExpr(e ast.PanicExpr) {
	r.expr(e.Message().Expr())
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
	Site ast.MemberExpr
}

// member resolves a chain of members: the module path at its start, as in 'geometry.units.scale', is one name, which
// the outermost member of the path becomes in place, and the rest of the chain stays what it was.
func (r *Resolver) member(m ast.MemberExpr) {
	var segments []segment
	cur := m.Expr()
	for {
		mm, ok := cur.AsMemberExpr()
		if !ok {
			break
		}
		segments = append(segments, segment{mm.Name(), mm})
		cur = mm.Obj()
	}
	for i, j := 0, len(segments)-1; i < j; i, j = i+1, j-1 {
		segments[i], segments[j] = segments[j], segments[i]
	}
	head, ok := cur.AsNameExpr()
	if !ok || r.isBound(head.Name()) || head.Name() == "Self" {
		r.expr(m.Obj())
		return
	}
	bare := r.bare(head.Name())
	decl := bare.Decl
	if decl != nil {
		_, isConst := decl.(*ast.ConstDecl)
		if isConst || (isType(decl) && declaresMember(decl, segments[0].Name)) || !r.isModuleRoot(head.Name()) {
			r.bindDecl(head.ID, r.take(bare, head.Position()))
			return
		}
	}
	if !r.isModuleRoot(head.Name()) {
		return
	}

	names := make([]string, len(segments))
	sites := make([]ast.Pos, len(segments))
	for i, s := range segments {
		names[i] = s.Name
		sites[i] = s.Site.Position()
	}
	target, used := r.qualified(head.Name(), head.Position(), names, sites)
	result := segments[used-1].Site.BecomeName(head)
	r.bindDecl(result.ID, target)
}

func (r *Resolver) qualified(head string, headSite ast.Pos, rest []string, sites []ast.Pos) (ast.Decl, int) {
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
		panic(headSite.Position().Error(diag.ErrExpressionError, "'"+head+"."+strings.Join(rest, ".")+"' is a module, not a value"))
	}
	decl, ok := mod.Names[rest[i]]
	if !ok {
		panic(sites[i].Position().Error("E2073", "module "+mod.Display()+" declares no '"+rest[i]+"'"))
	}
	found := lookup{Decl: decl}
	if !r.visible(decl) {
		found.Error = hidden(decl, sites[i])
	}
	return r.take(found, sites[i]), i + 1
}

func (r *Resolver) resolveType(tr ast.TypeRef) {
	for _, a := range tr.TypeArgs().All() {
		r.resolveType(a)
	}
	if tr.Qualifier().Len() == 0 {
		if slot, ok := r.typeParams[tr.Name()]; ok {
			r.result.Resolution.BindTypeParam(tr.ID, slot)
			return
		}
		if primitiveTypes[tr.Name()] {
			return
		}
		found := r.take(r.bare(tr.Name()), tr.Position())
		if found != nil && !isType(found) {
			panic(tr.Error(diag.ErrUnknownType, "'"+tr.Name()+"' is not a type"))
		}
		r.bindDecl(tr.ID, found)
		return
	}
	if !r.isModuleRoot(tr.Qualifier().At(0)) {
		panic(tr.Error(diag.ErrUnknownType, "no module '"+tr.Qualifier().At(0)+"'"))
	}
	path := append(tr.Qualifier().Strings()[1:], tr.Name())
	sites := make([]ast.Pos, len(path))
	for i := range sites {
		sites[i] = tr.Position()
	}
	decl, used := r.qualified(tr.Qualifier().At(0), tr.Position(), path, sites)
	if used != len(path) || !isType(decl) {
		panic(tr.Error(diag.ErrUnknownType, "'"+strings.Join(tr.Qualifier().Strings(), ".")+"."+tr.Name()+"' is not a type"))
	}
	r.bindDecl(tr.ID, decl)
}

func (r *Resolver) visible(d ast.Decl) bool {
	b := d.Base()
	return b.Module == r.module || b.IsExported()
}

func hidden(d ast.Decl, site ast.Pos) *diag.CompileError {
	b := d.Base()
	return site.Position().Error("E3008", "'"+b.Name+"' is not exported from module "+b.Module.Display())
}

// bindDecl records that a node names d, unless it names no declaration, as 'print' does not.
func (r *Resolver) bindDecl(n ast.NodeID, d ast.Decl) {
	if d != nil {
		r.result.Resolution.BindDecl(n, d)
	}
}

func (r *Resolver) take(found lookup, site ast.Pos) ast.Decl {
	if found.Error != nil {
		panic(site.Position().Error(found.Error.Code, found.Error.Message))
	}
	if found.Decl != nil {
		dm := found.Decl.Base().Module
		if dm != r.module && !dm.IsStdlib {
			if r.deps == nil {
				r.deps = map[ast.ModuleID]ast.Pos{}
			}
			if _, seen := r.deps[dm.ID]; !seen {
				r.deps[dm.ID] = site.Position()
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
		return lookup{Decl: root, Error: hidden(root, root.Position())}
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
		return lookup{Error: hidden(candidates[0], candidates[0].Position())}
	}
	return lookup{Decl: r.project.Stdlib.Names[name]}
}
