package ast

// Terminates is whether a statement never completes normally: it returns or panics on every path, so a function whose
// body ends with it needs no return after it.
func Terminates(s Stmt) bool {
	return VisitStmtResult[bool](termination{}, s)
}

// TerminatesBody is whether the last statement of a body terminates.
func TerminatesBody(body []Stmt) bool {
	return len(body) > 0 && Terminates(body[len(body)-1])
}

// Live is the statements of a block that can run: those up to the first that terminates or jumps out of the block. The
// rest are type-checked like any code, but record no calls and are not lowered, so they instantiate nothing. A break or
// a continue ends only its own block's live statements: it may leave an enclosing block that is not a loop, so it never
// makes that block terminate.
func Live(body []Stmt) []Stmt {
	for i, s := range body {
		if VisitStmtResult[bool](blockEnd{}, s) {
			return body[:i+1]
		}
	}
	return body
}

type termination struct{}

func (termination) VisitReturnStmt(*ReturnStmt) bool { return true }

func (termination) VisitExprStmt(t *ExprStmt) bool {
	_, isPanic := t.E.(*PanicExpr)
	return isPanic
}

func (termination) VisitIfStmt(t *IfStmt) bool {
	return t.HasElse && TerminatesBody(t.Then) && TerminatesBody(t.Else)
}

func (termination) VisitMatchStmt(t *MatchStmt) bool {
	for _, a := range t.Match.Arms {
		if !(a.Kind() == ArmPanic || (a.Action != nil && Terminates(a.Action))) {
			return false
		}
	}
	return true
}

func (termination) VisitLetStmt(*LetStmt) bool { return false }

func (termination) VisitAssignStmt(*AssignStmt) bool { return false }

func (termination) VisitDiscardStmt(*DiscardStmt) bool { return false }

func (termination) VisitWhileStmt(*WhileStmt) bool { return false }

func (termination) VisitWhenStmt(*WhenStmt) bool { return false }

func (termination) VisitForStmt(*ForStmt) bool { return false }

func (termination) VisitBreakStmt(*BreakStmt) bool { return false }

func (termination) VisitContinueStmt(*ContinueStmt) bool { return false }

// blockEnd is whether control never reaches the statement after this one in its block.
type blockEnd struct{ termination }

func (blockEnd) VisitBreakStmt(*BreakStmt) bool { return true }

func (blockEnd) VisitContinueStmt(*ContinueStmt) bool { return true }
