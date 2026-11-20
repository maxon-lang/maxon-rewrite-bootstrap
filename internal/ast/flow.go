package ast

// Terminates is whether a statement never completes normally: it returns or panics on every path, so a function whose
// body ends with it needs no return after it.
func Terminates(s Stmt) bool {
	return VisitStmtResult[bool](termination{}, s)
}

// TerminatesBody is whether the last statement of a body terminates.
func TerminatesBody(body StmtList) bool {
	return body.Len() > 0 && Terminates(body.At(body.Len()-1))
}

// Live is the statements of a block that can run: those up to the first that terminates or jumps out of the block. The
// rest are type-checked like any code, but record no calls and are not lowered, so they instantiate nothing. A break or
// a continue ends only its own block's live statements: it may leave an enclosing block that is not a loop, so it never
// makes that block terminate.
func Live(body StmtList) StmtList {
	for i := range body.Len() {
		if VisitStmtResult[bool](blockEnd{}, body.At(i)) {
			return body.Prefix(i + 1)
		}
	}
	return body
}

type termination struct{}

func (termination) VisitReturnStmt(ReturnStmt) bool { return true }

func (termination) VisitExprStmt(t ExprStmt) bool {
	return t.E().Kind() == KindPanicExpr
}

func (termination) VisitIfStmt(t IfStmt) bool {
	return t.HasElse() && TerminatesBody(t.Then()) && TerminatesBody(t.Else())
}

func (termination) VisitMatchStmt(t MatchStmt) bool {
	arms := t.Match().Arms()
	for i := range arms.Len() {
		a := arms.At(i)
		if !(a.Form() == ArmPanic || (!a.Action().IsNil() && Terminates(a.Action()))) {
			return false
		}
	}
	return true
}

// VisitTryStmt: a failure in the body goes to the handler, or out of the function when there is none, so the statement
// terminates when the body does and the handler, if there is one, does too.
func (termination) VisitTryStmt(t TryStmt) bool {
	return TerminatesBody(t.Body()) && (!t.HasOtherwise() || TerminatesBody(t.Handler()))
}

func (termination) VisitLetStmt(LetStmt) bool { return false }

func (termination) VisitAssignStmt(AssignStmt) bool { return false }

func (termination) VisitDiscardStmt(DiscardStmt) bool { return false }

func (termination) VisitWhileStmt(WhileStmt) bool { return false }

func (termination) VisitWhenStmt(WhenStmt) bool { return false }

func (termination) VisitForStmt(ForStmt) bool { return false }

func (termination) VisitBreakStmt(BreakStmt) bool { return false }

func (termination) VisitContinueStmt(ContinueStmt) bool { return false }

// blockEnd is whether control never reaches the statement after this one in its block.
type blockEnd struct{ termination }

func (blockEnd) VisitBreakStmt(BreakStmt) bool { return true }

func (blockEnd) VisitContinueStmt(ContinueStmt) bool { return true }
