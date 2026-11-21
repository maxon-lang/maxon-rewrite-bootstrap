package check

import (
	"fmt"
	"slices"
	"strings"

	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
)

// fnLevel is the state of a function whose body holds a behaviour that is being checked. The body of a behaviour is a
// function of its own, with its own locals and slots, that sees the variables around it only by capturing them.
type fnLevel struct {
	fn        *ast.FuncDecl
	scopes    []map[string]*ast.Local
	blocks    []checkerBlock
	nextSlot  int
	whens     int
	behaviour *behaviourBuild
	tries     []checkerTry
}

// behaviourBuild is a behaviour body being checked: the parameters it has taken so far.
type behaviourBuild struct {
	fn *ast.FuncDecl
	// accesses are the parameters that give access to the values of the cowns, first among the parameters.
	accesses []*ast.Local
	// captured maps a variable of the enclosing function to the parameter that holds it in the body.
	captured map[*ast.Local]*ast.Local
	// captures are the variables taken so far, in the order they were first named.
	captures []*ast.Capture
}

func lookupScopes(scopes []map[string]*ast.Local, name string) *ast.Local {
	for _, scope := range slices.Backward(scopes) {
		if l, ok := scope[name]; ok {
			return l
		}
	}
	return nil
}

// lookupLocal is the variable a name stands for where it is written, or nil. For a name inside a behaviour that is a
// variable of an enclosing function it is that variable, and nothing is captured: this only asks.
func (c *Checker) lookupLocal(name string) *ast.Local {
	if l := lookupScopes(c.scopes, name); l != nil {
		return l
	}
	for _, v := range slices.Backward(c.outers) {
		if l := lookupScopes(v.scopes, name); l != nil {
			return l
		}
		if v.behaviour == nil {
			break
		}
	}
	return nil
}

// useLocal is the variable that a name stands for, for code that uses it. A variable of the enclosing function is
// captured: the behaviour's body gets a parameter of its own that holds the value.
func (c *Checker) useLocal(n ast.NameExpr) *ast.Local {
	if l := lookupScopes(c.scopes, n.Name()); l != nil {
		return l
	}
	if c.behaviour == nil {
		return nil
	}
	return c.captureFrom(c.behaviour, len(c.outers)-1, n)
}

// captureFrom finds the name in the function that encloses the behaviour b, which may itself be the body of a
// behaviour that captures it from the one around it, and captures it into b.
func (c *Checker) captureFrom(b *behaviourBuild, level int, n ast.NameExpr) *ast.Local {
	lv := &c.outers[level]
	outer := lookupScopes(lv.scopes, n.Name())
	if outer == nil && lv.behaviour != nil {
		outer = c.captureFrom(lv.behaviour, level-1, n)
	}
	if outer == nil {
		return nil
	}
	if inner, ok := b.captured[outer]; ok {
		return inner
	}
	c.requireCapturable(n.Position(), outer)
	inner := ast.NewLocal(outer.Name, outer.Type, false)
	inner.IsParam = true
	inner.Conv = ast.ConvConsumes
	inner.ParamIndex = len(b.accesses) + len(b.captures)
	b.fn.AddLocal(inner)
	ref := n.CaptureRef(level)
	c.typing.SetName(ref, ast.LocalName(outer))
	c.typing.SetType(ref.Expr(), outer.Type)
	b.captures = append(b.captures, &ast.Capture{Ref: ref, Inner: inner})
	b.captured[outer] = inner
	return inner
}

// requireCapturable refuses a variable that a behaviour cannot take. The behaviour runs later and in another thread, so
// what it takes must be a value of its own: a scalar is copied, and an owner gives its whole value away. A borrow ends
// when its call or match does, long before the behaviour runs.
func (c *Checker) requireCapturable(site ast.Pos, outer *ast.Local) {
	if !outer.Type.IsManaged() || !outer.Borrowed() {
		return
	}
	what := "a let or 'mutates' parameter"
	switch {
	case outer.Projected:
		what = "a match binding"
	case outer.IsSelf:
		what = "the receiver"
	case outer.IsParam && outer.Conv == ast.ConvSets:
		what = "a 'sets' parameter"
	case outer.IsParam && outer.Conv == ast.ConvMutates && c.isAccess(outer):
		what = "the access that a behaviour has to a cown's value, for as long as it runs"
	}
	var fix string
	if bt, ok := outer.Type.(*ast.BuiltinTy); ok && bt.Shared() {
		fix = fmt.Sprintf("take an owner of it first: 'let own = share(%s)', and use 'own' in the behaviour", outer.Name)
	} else {
		fix = c.copyAdvice(outer.Type, fmt.Sprintf("give the behaviour a copy: 'let own = %s.clone()' before the 'when', and use 'own'", outer.Name), "pass what the behaviour needs through a cown")
	}
	panic(site.Position().Error(diag.ErrBehaviourBorrow,
		fmt.Sprintf("'%s' is %s, so it only borrows its value, and a behaviour runs later in another thread and cannot keep a borrow; %s", outer.Name, what, fix)))
}

// isAccess is whether a local is the parameter that gives a behaviour access to the value of a cown.
func (c *Checker) isAccess(l *ast.Local) bool {
	if c.behaviour != nil && slices.Contains(c.behaviour.accesses, l) {
		return true
	}
	for _, lv := range c.outers {
		if lv.behaviour != nil && slices.Contains(lv.behaviour.accesses, l) {
			return true
		}
	}
	return false
}

// refuseBehaviourSelf is the error for 'self', or a field of it, used inside a behaviour: the receiver is borrowed, and
// a field is a part of it. t is the type of what was named.
func (c *Checker) refuseBehaviourSelf(site ast.Pos, what string, t ast.Ty) {
	panic(site.Position().Error(diag.ErrBehaviourBorrow,
		fmt.Sprintf("%s belongs to the receiver, which a method only borrows, and a behaviour runs later in another thread; %s", what,
			c.copyAdvice(t, "copy what it needs into a variable before the 'when': 'let own = ....clone()'", "pass what the behaviour needs through a cown"))))
}

// selfFieldInBehaviour is the type of the field of the enclosing method's record that a bare name stands for, if there
// is one, so that a behaviour naming it is told why it cannot.
func (c *Checker) selfFieldInBehaviour(name string) (ast.Ty, bool) {
	root := c.outers[0].fn
	if root == nil || !root.IsMethod() {
		return nil, false
	}
	rt, ok := c.owner.(*ast.RecordTy)
	if !ok || rt.Field(name) == nil {
		return nil, false
	}
	return rt.FieldType(rt.Field(name)), true
}

// enterBehaviour starts checking the body of a behaviour as a function of its own, whose first parameters give access
// to the values of the cowns it names.
func (c *Checker) enterBehaviour(w ast.WhenStmt, values []ast.Ty) {
	fn := &ast.FuncDecl{}
	fn.ReturnType = ast.VoidTy
	fn.Pos, fn.ID = w.Position(), w.ID
	fn.Name = ast.BehaviourName(c.fn.Symbol(), c.whens)
	fn.Visibility = c.fn.Visibility
	fn.Module = c.module
	fn.Body = w.Body()
	c.whens++
	c.outers = append(c.outers, fnLevel{fn: c.fn, scopes: c.scopes, blocks: c.blocks, nextSlot: c.nextSlot, whens: c.whens, behaviour: c.behaviour, tries: c.tries})
	b := &behaviourBuild{fn: fn, captured: map[*ast.Local]*ast.Local{}}
	c.behaviour = b
	c.fn, c.scopes, c.blocks, c.nextSlot, c.whens, c.tries = fn, []map[string]*ast.Local{{}}, nil, 0, 0, nil
	for i := range w.Names().Len() {
		name := w.Names().At(i)
		access := ast.NewLocal(name, values[i], false)
		access.IsParam = true
		access.Conv = ast.ConvMutates
		access.ParamIndex = i
		c.bind(access, w.Position())
		b.accesses = append(b.accesses, access)
	}
}

// leaveBehaviour finishes the function of a behaviour body: its parameters are the accesses, then what it captured.
func (c *Checker) leaveBehaviour(w ast.WhenStmt) {
	b := c.behaviour
	fn := b.fn
	add := func(l *ast.Local) {
		p := &ast.Param{}
		p.Pos = w.Position()
		p.Name = l.Name
		p.Convention = l.Conv.String()
		fn.ParamLocals = append(fn.ParamLocals, l.ID())
		fn.Params = append(fn.Params, p)
		fn.ParamTypes = append(fn.ParamTypes, l.Type)
		fn.Pass.Specs = append(fn.Pass.Specs, ast.ParamSpec{Name: l.Name, Type: l.Type, Conv: l.Conv})
	}
	for _, a := range b.accesses {
		add(a)
	}
	for _, cp := range b.captures {
		add(cp.Inner)
	}
	info := c.typing.EditWhen(w)
	info.Fn, info.Captures = fn, b.captures
	outer := c.outers[len(c.outers)-1]
	c.outers = c.outers[:len(c.outers)-1]
	c.fn, c.scopes, c.blocks, c.nextSlot, c.whens, c.behaviour, c.tries = outer.fn, outer.scopes, outer.blocks, outer.nextSlot, outer.whens, outer.behaviour, outer.tries
}

// VisitWhenStmt types a behaviour: it names cowns, and its body runs once it has exclusive access to all of them, with
// the names after 'as' giving mutable access to their values.
func (c *Checker) VisitWhenStmt(w ast.WhenStmt) {
	if w.Cowns().Len() != w.Names().Len() {
		panic(w.Error(diag.ErrWhenCowns, fmt.Sprintf("the behaviour names %d cown(s) but gives %d name(s) to their values after 'as': one name for each", w.Cowns().Len(), w.Names().Len())))
	}
	values := make([]ast.Ty, w.Cowns().Len())
	for i, cn := range w.Cowns().All() {
		t := c.checkExpr(cn.Expr())
		if !ast.IsCown(t) {
			panic(cn.Error(diag.ErrWhenCowns, fmt.Sprintf("'%s' is %s, and a behaviour names cown references: make one with 'cown(value)'", cn.Name(), describeType(t))))
		}
		values[i] = t.(*ast.BuiltinTy).Held()
		for j := range i {
			other := w.Cowns().At(j)
			if c.sameName(other, cn) {
				panic(cn.Error(diag.ErrWhenSameCown, fmt.Sprintf("'%s' is named twice by one behaviour, which would give it two mutable names for one value; name each cown once", cn.Name())))
			}
		}
	}
	c.enterBehaviour(w, values)
	defer c.leaveBehaviour(w)
	c.checkBlock(w.Body())
}

// sameName is whether two cown operands are one variable or one field.
func (c *Checker) sameName(a, b ast.NameExpr) bool {
	x, y := c.typing.Name(a), c.typing.Name(b)
	return x == y && (x.Kind() == ast.NameLocal || x.Kind() == ast.NameSelfField)
}

func describeType(t ast.Ty) string {
	if _, isConst := t.(*ast.ConstIntTy); isConst {
		return "an integer"
	}
	return "a " + t.Name()
}

// requireNotCown refuses to reach into a cown reference: its value is touched only inside a behaviour that holds it, and
// the reference itself is copied, shared and named by a behaviour.
func requireNotCown(site ast.Pos, t ast.Ty, use, name string) {
	if ast.IsCown(t) {
		if name != "" {
			use += " '" + name + "'"
		}
		panic(site.Position().Error(diag.ErrCownAccess,
			fmt.Sprintf("%s needs the value of a %s, which is touched only inside a behaviour that holds the cown: write 'when <name> as x' and use x", use, t.Name())))
	}
}

// requireNotFrozenChange refuses a call that would change or consume the contents of a frozen value.
func requireNotFrozenChange(site ast.Pos, recv ast.Ty, conv ast.Convention, method string) {
	if ast.IsFrozen(recv) && conv != ast.ConvLet {
		panic(site.Position().Error(diag.ErrFrozenChange,
			fmt.Sprintf("'%s' takes its receiver as %s, and the contents of a frozen value are immutable and owned by it; call it on a copy: 'thaw(x).%s(...)' gives an owned value", method, conv, method)))
	}
}

// requireFreezable refuses a type argument of frozen that is a plain value: it is copied freely and has nothing to
// freeze.
func requireFreezable(site ast.Pos, arg ast.Ty) {
	if !arg.IsManaged() {
		panic(site.Position().Error(diag.ErrFreezeScalar,
			fmt.Sprintf("'%s' is a plain value, which is immutable already and copied freely, so there is nothing to freeze: freeze a record, a union, a String or a collection", arg.Name())))
	}
}

// checkFreezeArgs refuses a value to freeze that is no managed value and so cannot be frozen.
func (c *Checker) checkFreezeArgs(_ ast.CallExpr, ordered []ast.Expr) {
	arg, t := c.soleArg(ordered)
	if _, isConst := t.(*ast.ConstIntTy); isConst || (t != ast.NeverTy && t != ast.VoidTy && !t.IsManaged()) {
		requireFreezable(arg.Position(), t)
	}
}

// checkShareArgs refuses a value to share that is neither kind of shared value, which the generic signature allows.
func (c *Checker) checkShareArgs(_ ast.CallExpr, ordered []ast.Expr) {
	arg, t := c.soleArg(ordered)
	if bt, ok := t.(*ast.BuiltinTy); !ok || !bt.Shared() {
		panic(arg.Position().Error(diag.ErrShareTarget,
			fmt.Sprintf("share takes a frozen value or a cown reference, found %s; a value that is not shared has one owner, so %s", t.Name(), c.copyAdvice(t, "move it, or copy it with '.clone()'", "move it"))))
	}
}

// checkThawArgs refuses a value to thaw that is not frozen.
func (c *Checker) checkThawArgs(_ ast.CallExpr, ordered []ast.Expr) {
	arg, t := c.soleArg(ordered)
	if !ast.IsFrozen(t) {
		panic(arg.Position().Error(diag.ErrShareTarget,
			fmt.Sprintf("thaw takes a frozen value, found %s; a value that is not frozen is already owned: %s", t.Name(), c.copyAdvice(t, "use '.clone()' for another one", "move it"))))
	}
}

// soleArg is the one argument of a sharing function, which binding the arguments to its signature has made sure of.
func (c *Checker) soleArg(ordered []ast.Expr) (ast.Expr, ast.Ty) {
	if len(ordered) != 1 {
		panic(diag.Internal("check: a sharing function given %d arguments", len(ordered)))
	}
	return ordered[0], c.ty(ordered[0])
}

// cownCall types cown(x) and cown(x, dropWith: d). The content of the first is dropped with the last reference, so it
// must be Droppable. The content of the second is ended by its destructor d, which is a method name and no value, so
// the argument is taken out of the call before the ordinary arguments are checked.
func (c *Checker) cownCall(call ast.CallExpr) ast.Ty {
	b := c.project.Builtins
	args := call.Args()
	at := -1
	for i := range args.Len() {
		if args.At(i).Label() == ast.DropWithLabel {
			at = i
			break
		}
	}
	if at <= 0 {
		return c.callFunction(call, ast.CallFunction, b.CownNew, nil)
	}
	drop := args.At(at)
	c.omitted.call, c.omitted.arg = call, drop
	defer func() { c.omitted.call, c.omitted.arg = ast.CallExpr{}, ast.Arg{} }()
	t := c.callFunction(call, ast.CallFunction, b.CownNewDropWith, nil)
	c.omitted.call, c.omitted.arg = ast.CallExpr{}, ast.Arg{}
	dropCall := c.dropWithCall(call, drop, c.ty(c.typing.Call(call).Ordered[0]))
	c.typing.Call(call).Drop = dropCall
	c.recordUses(ast.CallUse{Info: dropCall, Site: call.Position()})
	return t
}

// dropWithCall is the call of the destructor that a cown of content ends it with. Only content that is not Droppable
// has something to decide, and the destructor takes nothing else, because the cown has nothing else to give it.
func (c *Checker) dropWithCall(site ast.CallExpr, arg ast.Arg, content ast.Ty) *ast.CallInfo {
	if c.lacks(droppable, content) == nil {
		panic(arg.Error(diag.ErrCapabilityMisuse, fmt.Sprintf("'%s' is Droppable, so the cown drops it with its last reference and there is nothing to decide: remove 'dropWith:'", content.Name())))
	}
	name, isName := arg.Value().AsNameExpr()
	if !isName {
		panic(arg.Error(diag.ErrCapabilityMisuse, "'dropWith:' names a destructor of the content by its name alone, as 'dropWith: rollback'"))
	}
	nt, isNominal := content.(ast.NominalTy)
	if !isNominal {
		panic(arg.Error(diag.ErrCapabilityMisuse, fmt.Sprintf("'%s' is a type parameter and has no destructor to name: a value that is not Droppable cannot go into a cown in generic code; add the bound 'where %s implements Droppable'", content.Name(), content.Name())))
	}
	method, found := nt.MethodTable()[name.Name()]
	if !found || method.IsStatic || !method.IsDestructor() || len(method.Pass.Specs) > 0 || method.ReturnType != ast.Ty(ast.VoidTy) {
		panic(arg.Error(diag.ErrCapabilityMisuse, fmt.Sprintf("'%s' is not a destructor of '%s' that takes no other argument and returns nothing%s", name.Name(), nt.Name(), c.dropWithCandidates(nt))))
	}
	c.requireVisible(arg.Position(), method)
	c.queueMethodBounds(site.Position(), method, nt.Base().Args)
	info := &ast.CallInfo{Kind: ast.CallMethod, Target: method, Pass: &method.Pass}
	if len(method.Generics) > 0 {
		info.Generic = &ast.GenericCall{TypeArgs: nt.Base().Args}
	}
	return info
}

// dropWithCandidates names the destructors that dropWith: may name, in declaration order, for the error that names
// another.
func (c *Checker) dropWithCandidates(t ast.NominalTy) string {
	var names []string
	for _, m := range declaredMethods(t) {
		if m.IsDestructor() && len(m.Pass.Specs) == 0 && m.ReturnType == ast.Ty(ast.VoidTy) {
			names = append(names, m.Name)
		}
	}
	if len(names) == 0 {
		return fmt.Sprintf(": '%s' has no such destructor; declare one, as 'function rollback(destructs self)'", t.Name())
	}
	return ": name " + strings.Join(names, " or ")
}

// checkCownCycle refuses a type that can reach itself through a cown. A cown owns a mutable value, and a behaviour can
// store a cown reference in it, so a type that holds a cown of a type that holds it again lets two cowns refer to each
// other, and counting never frees a cycle. Owned values and frozen values cannot form one: an owner is a tree, and a
// frozen value refers only to values frozen before it.
func (c *Checker) checkCownCycle(t ast.NominalTy) {
	var site ast.Pos
	switch n := t.(type) {
	case *ast.RecordTy:
		site = n.Decl.Position()
	case *ast.UnionTy:
		site = n.Decl.Position()
	default:
		panic(diag.Internal("no declaration for %s", t.Name()))
	}
	self := ast.SelfType(t)
	var path []string
	if !c.reachesThroughCown(self, self, false, &path, map[cownVisit]bool{}) {
		return
	}
	panic(site.Position().Error(diag.ErrCownCycle,
		fmt.Sprintf("type '%s' can reach itself through a cown (via %s), so cowns could refer to each other and counting would never free them; store a handle, or a key into a map, instead of the reference",
			t.Name(), strings.Join(path, " -> "))))
}

type cownVisit struct {
	t   ast.Ty
	via bool
}

// holdingEdge is a type that a value of another type holds: a field, a payload, an element, or what a frozen value or a
// cown holds. A handle holds nothing.
type holdingEdge struct {
	label string
	t     ast.Ty
	cown  bool
}

func holdingEdges(t ast.Ty) []holdingEdge {
	var out []holdingEdge
	switch n := t.(type) {
	case *ast.RecordTy:
		for _, f := range n.FieldList() {
			out = append(out, holdingEdge{label: n.Name() + "." + f.Name, t: n.FieldType(f)})
		}
	case *ast.UnionTy:
		for i, cs := range n.CaseList() {
			for _, p := range n.Payload(i) {
				out = append(out, holdingEdge{label: n.Name() + "." + cs.Name, t: p.Type})
			}
		}
	case *ast.BuiltinTy:
		if n.Kind == ast.BuiltinHandle {
			return nil
		}
		for _, a := range n.Args {
			out = append(out, holdingEdge{label: n.Name(), t: a, cown: n.Kind == ast.BuiltinCown})
		}
	}
	return out
}

func (c *Checker) reachesThroughCown(from, target ast.Ty, via bool, path *[]string, visited map[cownVisit]bool) bool {
	for _, e := range holdingEdges(from) {
		next := via || e.cown
		*path = append(*path, e.label)
		if e.t == target && next {
			return true
		}
		if ast.TypeDepth(e.t) <= ast.MaxTypeDepth {
			key := cownVisit{e.t, next}
			if !visited[key] {
				visited[key] = true
				if c.reachesThroughCown(e.t, target, next, path, visited) {
					return true
				}
			}
		}
		*path = (*path)[:len(*path)-1]
	}
	return false
}
