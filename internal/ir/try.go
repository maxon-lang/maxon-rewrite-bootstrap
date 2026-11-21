package ir

import (
	"slices"

	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
)

// tryHandler is a try being lowered: where the failures of what it covers go. A failure leaves the scopes and the values in
// flight that the try did not start with, as a jump out of them would, and jumps to the handler, or for a try that passes
// its failures on returns the error.
type tryHandler struct {
	propagates bool
	target     BlockID
	// depth is how many scopes were open where the try began, and mark how many drops the innermost of them held then.
	depth, mark int
	// pending is how many values were in flight where the try began.
	pending int
	// binding is the slot of the error that the handler names, or -1.
	binding int
}

// enterTry starts a try whose failures go to target, naming the error in the slot binding (-1 for none).
func (l *Lowerer) enterTry(target BlockID, binding int) tryHandler {
	h := tryHandler{target: target, depth: len(l.scopes), pending: len(l.pending), binding: binding}
	h.mark = len(l.scopes[h.depth-1].drops)
	l.handlers = append(l.handlers, h)
	return h
}

// enterPropagating starts a try that passes its failures to the caller, and gives whether it added a handler: inside
// another try, its failures go to that one's.
func (l *Lowerer) enterPropagating() bool {
	if len(l.handlers) > 0 {
		return false
	}
	l.handlers = append(l.handlers, tryHandler{propagates: true})
	return true
}

func (l *Lowerer) leaveTry() { l.handlers = l.handlers[:len(l.handlers)-1] }

// failure is the block an operation goes to when it fails with the error errorCase, or with the error in the register err
// (a case plus 1, as a function that throws gives it) when that is not NoReg.
func (l *Lowerer) failure(errorCase int, err V) BlockID {
	if len(l.handlers) == 0 {
		panic(diag.Internal("lower: an operation of %s that can fail is outside every try; the range analysis should have refused it", l.fn.Symbol()))
	}
	h := l.handlers[len(l.handlers)-1]
	stub := l.b.NewBlock()
	resume := l.cur
	l.cur = stub
	if h.propagates {
		l.dropPending(0, len(l.pending))
		l.unwind(0)
		if err == NoReg {
			err = l.konst(int64(errorCase + 1))
		}
		l.b.Terminate(l.cur, Throw(err))
	} else {
		l.dropPending(h.pending, len(l.pending))
		l.unwindTo(h.depth, h.mark)
		if h.binding >= 0 {
			var v V
			if err == NoReg {
				v = l.konst(int64(errorCase))
			} else {
				v = l.reg(KindScalar)
				l.emit(Bin(v, BinSub, err, l.konst(1), false, NoBlock))
			}
			l.emit(StoreSlot(h.binding, v, false))
		}
		l.b.Terminate(l.cur, Jump(h.target))
	}
	l.cur = resume
	return stub
}

// dropPending drops the values in flight from index from to index to, newest first.
func (l *Lowerer) dropPending(from, to int) {
	for _, p := range slices.Backward(l.pending[from:to]) {
		l.emitDrop(p)
	}
}

// unwindTo drops what the scopes from depth inwards hold, and what the scope below them took on after it held mark drops.
func (l *Lowerer) unwindTo(depth, mark int) {
	l.unwind(depth)
	for _, drop := range slices.Backward(l.scopes[depth-1].drops[mark:]) {
		l.emitDrop(drop)
	}
}

// VisitThrowStmt lowers a throw: a failure with the error its value is a case of, which goes to the innermost try's
// handler, or with none out of the function.
func (l *Lowerer) VisitThrowStmt(s ast.ThrowStmt) {
	err := l.reg(KindScalar)
	l.emit(Bin(err, BinAdd, l.expr(s.Value()), l.konst(1), false, NoBlock))
	added := l.enterPropagating()
	l.seal(Jump(l.failure(0, err)))
	if added {
		l.leaveTry()
	}
}

// VisitTryStmt lowers a try block: the body with its failures going to the handler, or, with no handler, onwards.
func (l *Lowerer) VisitTryStmt(s ast.TryStmt) {
	if !s.HasOtherwise() {
		added := l.enterPropagating()
		l.block(s.Body())
		if added {
			l.leaveTry()
		}
		return
	}
	handler := l.b.NewBlock()
	end := l.b.NewBlock()
	binding := -1
	if local := l.fn.LocalAt(l.typings.Of(s.T.Module).TryLocal(s)); local != nil {
		binding = local.Slot
	}
	l.enterTry(handler, binding)
	l.block(s.Body())
	l.leaveTry()
	l.goTo(end)
	l.cur = handler
	l.block(s.Handler())
	l.goTo(end)
	l.cur = end
}

// VisitTryExpr lowers a try that gives a value on a failure: its expression and the value 'otherwise' gives are the two
// branches of a value, as a conditional's are. One that ignores a failure has no value, and its failure goes on after it.
func (l *Lowerer) VisitTryExpr(e ast.TryExpr) V {
	if e.Ignore() {
		end := l.b.NewBlock()
		l.enterTry(end, -1)
		l.scoped(func() { l.expr(e.E()) })
		l.leaveTry()
		l.goTo(end)
		return l.konst(0)
	}
	typ := l.ty(e.Expr())
	result := l.f.NewSlot(l.kind(typ))
	other := l.b.NewBlock()
	end := l.b.NewBlock()
	l.enterTry(other, -1)
	l.scoped(func() { l.store(result, func() V { return l.converted(e.E(), typ) }) })
	l.leaveTry()
	l.goTo(end)
	l.cur = other
	l.scoped(func() { l.store(result, func() V { return l.converted(e.Value(), typ) }) })
	l.goTo(end)
	return l.fetch(result)
}

// through lowers a try whose value is its expression's, which passes a failure on or leaves the statement with a return,
// a break, a continue or a throw.
func (l *Lowerer) through(t ast.TryExpr) V {
	if t.Propagates() {
		added := l.enterPropagating()
		v := l.expr(t.E())
		if added {
			l.leaveTry()
		}
		return v
	}
	exit := l.b.NewBlock()
	h := l.enterTry(exit, -1)
	v := l.expr(t.E())
	l.leaveTry()
	resume := l.cur
	l.cur = exit
	l.atTryStart(h, func() {
		switch {
		case !t.Ret().IsNil():
			l.VisitReturnStmt(t.Ret())
		case !t.Brk().IsNil():
			l.VisitBreakStmt(t.Brk())
		case !t.Cont().IsNil():
			l.VisitContinueStmt(t.Cont())
		case !t.Thr().IsNil():
			l.VisitThrowStmt(t.Thr())
		default:
			panic(diag.Internal("lower: a try with no handler is lowered as one that passes its failures on"))
		}
	})
	l.cur = resume
	return v
}

// atTryStart lowers code that runs after a failure has left what the try covered: it leaves the statement too, so the
// values in flight where the try began are dropped, and what the try's code made, which the failure dropped already, is
// not dropped again.
func (l *Lowerer) atTryStart(h tryHandler, lower func()) {
	inFlight := slices.Clone(l.pending)
	l.dropPending(0, h.pending)
	l.pending = l.pending[:0]
	// The scopes may move while lower opens scopes of its own, so the one below the try is found by its index each time.
	below := h.depth - 1
	made := slices.Clone(l.scopes[below].drops[h.mark:])
	l.scopes[below].drops = l.scopes[below].drops[:h.mark]
	lower()
	l.scopes[below].drops = append(l.scopes[below].drops[:h.mark], made...)
	l.pending = append(l.pending[:0], inFlight...)
}
