package ir

import (
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
)

// read lowers an expression that is only read, and sees through a frozen value to what it holds: the receiver of a
// method, the object of a field, the collection of a subscript, a subject and a part of an interpolated string. A value
// that is not frozen is read as it is.
func (l *Lowerer) read(e ast.Expr) V {
	v := l.expr(e)
	for t := l.binding.Apply(l.ty(e)); ast.IsFrozen(t); {
		v, t = l.contentOnce(e.Position(), v, t)
	}
	return v
}

// contentOnce is what one frozen value holds, lent for as long as the frozen value is held, and the type of it.
func (l *Lowerer) contentOnce(site ast.Pos, v V, frozen ast.Ty) (V, ast.Ty) {
	held := frozen.(*ast.BuiltinTy).Held()
	return l.entry(site, RtFrozenGet, kindOf(held), v), held
}

// when schedules the body of a behaviour, which becomes a function of its own. The cowns it names are read, and what it
// captures moves in: the ownership pass marked each capture consumed, so reading it takes it out of its owner.
func (l *Lowerer) VisitWhenStmt(s ast.WhenStmt) {
	info := l.typings.Of(s.T.Module).When(s)
	behaviour := l.behaviour(info)
	cowns := make([]V, s.Cowns().Len())
	for i, c := range s.Cowns().All() {
		cowns[i] = l.expr(c.Expr())
	}
	captures := make([]V, len(info.Captures))
	for i, cp := range info.Captures {
		captures[i] = l.expr(cp.Ref.Expr())
	}
	l.refuse(s.Position(), diag.ErrInterpreterOnly, interpreterOnlyMessage)
	l.emit(l.b.When(behaviour, cowns, captures))
}

// behaviour lowers the body of a `when` as a function, with the type arguments of the function around it.
func (l *Lowerer) behaviour(s ast.WhenInfo) int {
	inner := l.newLowerer(s.Fn, ast.BehaviourName(l.f.Symbol(), len(l.f.Behaviours)))
	body := inner.lowerBody()
	l.f.UsesArrays = l.f.UsesArrays || body.UsesArrays
	l.f.UsesBytes = l.f.UsesBytes || body.UsesBytes
	l.f.UsesOs = l.f.UsesOs || body.UsesOs
	l.f.Refusals = append(l.f.Refusals, body.Refusals...)
	return l.b.Behaviour(body)
}

// intrinsicSharing lowers a call of freeze, share, thaw or cown.
func (l *Lowerer) intrinsicSharing(c call, args []V) V {
	switch c.Target.Intrinsic {
	case ast.IntrinsicFreeze:
		return l.entry(c.CallExpr.Position(), RtFrozenFreeze, KindManaged, args[0])
	case ast.IntrinsicShare:
		return l.entry(c.CallExpr.Position(), RtShare, KindManaged, args[0])
	case ast.IntrinsicThaw:
		held := l.binding.Apply(l.ty(c.CallExpr.Expr()))
		inner := l.entry(c.CallExpr.Position(), RtFrozenGet, kindOf(held), args[0])
		return l.copyValue(c.CallExpr.Position(), inner, held)
	case ast.IntrinsicCown:
		d := l.reg(KindManaged)
		var routine *ast.FuncDecl
		if c.Drop != nil {
			routine = l.callee(ast.CallUse{Info: c.Drop, Site: c.Position()})
		}
		l.emitEntry(c.CallExpr.Position(), d, RtCownNew, args, routine)
		return d
	}
	panic(diag.Internal("lower: %d is not an intrinsic that shares", int(c.Target.Intrinsic)))
}
