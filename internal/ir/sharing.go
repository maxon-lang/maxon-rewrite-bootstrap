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
	for t := l.binding.Apply(e.Ty()); ast.IsFrozen(t); {
		v, t = l.contentOnce(e, v, t)
	}
	return v
}

// contentOnce is what one frozen value holds, lent for as long as the frozen value is held, and the type of it.
func (l *Lowerer) contentOnce(site ast.Node, v V, frozen ast.Ty) (V, ast.Ty) {
	held := frozen.(*ast.BuiltinTy).Held()
	return l.entry(site, RtFrozenGet, kindOf(held), v), held
}

// when schedules the body of a behaviour, which becomes a function of its own. The cowns it names are read, and what it
// captures moves in: the ownership pass marked each capture consumed, so reading it takes it out of its owner.
func (l *Lowerer) VisitWhenStmt(s *ast.WhenStmt) {
	body := l.behaviour(s)
	cowns := make([]V, len(s.Cowns))
	for i, c := range s.Cowns {
		cowns[i] = l.expr(c)
	}
	captures := make([]V, len(s.Captures))
	for i, cp := range s.Captures {
		captures[i] = l.expr(cp.Ref)
	}
	l.refuse(s, diag.ErrInterpreterOnly, interpreterOnlyMessage)
	l.emit(&WhenInstr{Body: body, Cowns: cowns, Captures: captures})
}

// behaviour lowers the body of a `when` as a function, with the type arguments of the function around it.
func (l *Lowerer) behaviour(s *ast.WhenStmt) *IrFunction {
	inner := newLowerer(s.Fn, l.binding, ast.BehaviourName(l.f.Symbol(), len(l.f.Behaviours)))
	body := lowerBody(inner)
	l.f.Behaviours = append(l.f.Behaviours, body)
	l.f.UsesArrays = l.f.UsesArrays || body.UsesArrays
	l.f.Refusals = append(l.f.Refusals, body.Refusals...)
	return body
}

// intrinsicSharing lowers a call of freeze, share, thaw or cown.
func (l *Lowerer) intrinsicSharing(c *ast.CallExpr, args []V) V {
	switch c.Target.Intrinsic {
	case ast.IntrinsicFreeze:
		return l.entry(c, RtFrozenFreeze, KindManaged, args[0])
	case ast.IntrinsicShare:
		return l.entry(c, RtShare, KindManaged, args[0])
	case ast.IntrinsicThaw:
		held := l.binding.Apply(c.Ty())
		inner := l.entry(c, RtFrozenGet, kindOf(held), args[0])
		return l.copyValue(c, inner, held)
	case ast.IntrinsicCown:
		return l.entry(c, RtCownNew, KindManaged, args[0])
	}
	panic(diag.Internal("lower: %d is not an intrinsic that shares", int(c.Target.Intrinsic)))
}
