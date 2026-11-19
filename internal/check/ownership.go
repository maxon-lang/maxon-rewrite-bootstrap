package check

import (
	"fmt"
	"minimaxon/internal/ast"
	"minimaxon/internal/diag"
)

type ownState struct {
	moved map[*ast.Local]ast.Node
	dead  bool
}

var (
	ownUnreachable = &ownState{dead: true}
	ownClean       = &ownState{}
)

func (s *ownState) tryGetMove(l *ast.Local) (ast.Node, bool) {
	if s.moved == nil {
		return nil, false
	}
	site, ok := s.moved[l]
	return site, ok
}

func (s *ownState) with(l *ast.Local, site ast.Node) *ownState {
	if s.dead {
		return s
	}
	m := make(map[*ast.Local]ast.Node, len(s.moved)+1)
	for k, v := range s.moved {
		m[k] = v
	}
	m[l] = site
	return &ownState{moved: m}
}

func (s *ownState) without(l *ast.Local) *ownState {
	if s.dead || s.moved == nil {
		return s
	}
	if _, ok := s.moved[l]; !ok {
		return s
	}
	if len(s.moved) == 1 {
		return ownClean
	}
	m := make(map[*ast.Local]ast.Node, len(s.moved))
	for k, v := range s.moved {
		if k != l {
			m[k] = v
		}
	}
	return &ownState{moved: m}
}

func mergeOwn(a, b *ownState) *ownState {
	if a == b || b.dead || (b.moved == nil && !a.dead) {
		return a
	}
	if a.dead || a.moved == nil {
		return b
	}
	var m map[*ast.Local]ast.Node
	for k, v := range b.moved {
		if _, ok := a.moved[k]; !ok {
			if m == nil {
				m = make(map[*ast.Local]ast.Node, len(a.moved)+1)
				for ak, av := range a.moved {
					m[ak] = av
				}
			}
			m[k] = v
		}
	}
	if m == nil {
		return a
	}
	return &ownState{moved: m}
}

type ownFrame struct {
	target    ast.Node
	breaks    *ownState
	continues *ownState
}

func newOwnFrame(target ast.Node) *ownFrame {
	return &ownFrame{target: target, breaks: ownUnreachable, continues: ownUnreachable}
}

type Ownership struct {
	errors      *[]*diag.CompileError
	frames      []*ownFrame
	moveTargets map[ast.Node]string
}

func NewOwnership(errs *[]*diag.CompileError) *Ownership {
	return &Ownership{errors: errs, moveTargets: map[ast.Node]string{}}
}

func isMovable(l *ast.Local) bool {
	if !l.IsVar || l.IsParam {
		return false
	}
	switch l.Type.(type) {
	case *ast.RecordTy, *ast.UnionTy:
		return true
	}
	return false
}

func (o *Ownership) Check(f *ast.FuncDecl) {
	anyMovable := false
	for _, l := range f.Locals {
		anyMovable = anyMovable || isMovable(l)
	}
	if !anyMovable {
		return
	}
	o.frames = o.frames[:0]
	o.walk(f.Body, ownClean)
}

func (o *Ownership) frameOf(target ast.Node) *ownFrame {
	for i := len(o.frames) - 1; i >= 0; i-- {
		if o.frames[i].target == target {
			return o.frames[i]
		}
	}
	panic(diag.Internal("ownership: a jump has no enclosing frame"))
}

func (o *Ownership) walk(body []ast.Stmt, s *ownState) *ownState {
	for _, st := range body {
		s = o.stmt(st, s)
	}
	return s
}

func (o *Ownership) stmt(st ast.Stmt, s *ownState) *ownState {
	return ast.VisitStmtState[*ownState, *ownState](o, st, s)
}

func (o *Ownership) VisitLetStmt(st *ast.LetStmt, s *ownState) *ownState {
	return o.move(st.Init, st.Name, o.expr(st.Init, s))
}

func (o *Ownership) VisitAssignStmt(st *ast.AssignStmt, s *ownState) *ownState {
	s = o.expr(st.Value, s)
	if target, ok := st.Target.(*ast.NameExpr); ok && target.Kind == ast.NameLocal {
		return o.move(st.Value, target.Local.Name, s.without(target.Local))
	}
	if member, ok := st.Target.(*ast.MemberExpr); ok {
		return o.expr(member.Obj, s)
	}
	return s
}

func (o *Ownership) VisitDiscardStmt(st *ast.DiscardStmt, s *ownState) *ownState {
	return o.expr(st.Value, s)
}

func (o *Ownership) VisitExprStmt(st *ast.ExprStmt, s *ownState) *ownState {
	s = o.expr(st.E, s)
	if _, ok := st.E.(*ast.PanicExpr); ok {
		return ownUnreachable
	}
	return s
}

func (o *Ownership) VisitIfStmt(st *ast.IfStmt, s *ownState) *ownState {
	s = o.expr(st.Cond, s)
	a := o.walk(st.Then, s)
	b := s
	if st.HasElse {
		b = o.walk(st.Else, s)
	}
	return mergeOwn(a, b)
}

func (o *Ownership) VisitWhileStmt(st *ast.WhileStmt, s *ownState) *ownState {
	return o.loop(st, s, st.Body, st.Cond)
}

func (o *Ownership) VisitForStmt(st *ast.ForStmt, s *ownState) *ownState {
	return o.loop(st, o.expr(st.To, o.expr(st.From, s)), st.Body, nil)
}

func (o *Ownership) VisitMatchStmt(st *ast.MatchStmt, s *ownState) *ownState {
	return o.match(st.Match, s)
}

func (o *Ownership) VisitReturnStmt(st *ast.ReturnStmt, s *ownState) *ownState {
	if st.Value != nil {
		o.expr(st.Value, s)
	}
	return ownUnreachable
}

func (o *Ownership) VisitBreakStmt(st *ast.BreakStmt, s *ownState) *ownState {
	frame := o.frameOf(st.Target)
	frame.breaks = mergeOwn(frame.breaks, s)
	return ownUnreachable
}

func (o *Ownership) VisitContinueStmt(st *ast.ContinueStmt, s *ownState) *ownState {
	frame := o.frameOf(st.Target)
	frame.continues = mergeOwn(frame.continues, s)
	return ownUnreachable
}

func (o *Ownership) loop(loop ast.Node, s *ownState, body []ast.Stmt, cond ast.Expr) *ownState {
	frame := newOwnFrame(loop)
	o.frames = append(o.frames, frame)
	entry := s
	if cond != nil {
		entry = o.expr(cond, s)
	}
	end := ownUnreachable
	for pass := 0; pass < 2; pass++ {
		end = o.walk(body, entry)
		entry = mergeOwn(mergeOwn(entry, end), frame.continues)
		if cond != nil {
			entry = o.expr(cond, entry)
		}
	}
	o.frames = o.frames[:len(o.frames)-1]
	return mergeOwn(mergeOwn(entry, end), frame.breaks)
}

func (o *Ownership) match(m *ast.MatchCore, s *ownState) *ownState {
	s = o.expr(m.Subject, s)
	frame := newOwnFrame(m)
	o.frames = append(o.frames, frame)
	result := ownUnreachable
	for _, arm := range m.Arms {
		a := s
		if arm.Action != nil {
			a = o.stmt(arm.Action, a)
		} else if arm.Value != nil {
			a = o.expr(arm.Value, a)
			if arm.Kind == ast.ArmPanic {
				a = ownUnreachable
			}
		}
		result = mergeOwn(result, a)
	}
	o.frames = o.frames[:len(o.frames)-1]
	return mergeOwn(result, frame.breaks)
}

func (o *Ownership) move(value ast.Expr, into string, s *ownState) *ownState {
	if s.dead {
		return s
	}
	n, ok := value.(*ast.NameExpr)
	if !ok || n.Kind != ast.NameLocal || !isMovable(n.Local) {
		return s
	}
	o.moveTargets[value] = into
	return s.with(n.Local, value)
}

func (o *Ownership) read(n *ast.NameExpr, s *ownState) *ownState {
	l := n.Local
	if s.dead {
		return s
	}
	site, ok := s.tryGetMove(l)
	if !ok {
		return s
	}
	into, found := o.moveTargets[site]
	if !found {
		into = "another variable"
	}
	*o.errors = append(*o.errors, n.Error(diag.ErrUseAfterMove,
		fmt.Sprintf("'%s' was moved into '%s' at line %d; assign it again before reading it, or bind a clone", n.Name, into, site.P().Line)))
	return s.without(l)
}

func (o *Ownership) expr(e ast.Expr, s *ownState) *ownState {
	return ast.VisitExprState[*ownState, *ownState](o, e, s)
}

func (o *Ownership) VisitIntLit(_ *ast.IntLit, s *ownState) *ownState { return s }

func (o *Ownership) VisitBoolLit(_ *ast.BoolLit, s *ownState) *ownState { return s }

func (o *Ownership) VisitSelfExpr(_ *ast.SelfExpr, s *ownState) *ownState { return s }

func (o *Ownership) VisitStrLit(e *ast.StrLit, s *ownState) *ownState {
	for _, part := range e.Parts {
		if pe, ok := part.(ast.Expr); ok {
			s = o.expr(pe, s)
		}
	}
	return s
}

func (o *Ownership) VisitNameExpr(e *ast.NameExpr, s *ownState) *ownState {
	if e.Kind == ast.NameLocal {
		return o.read(e, s)
	}
	return s
}

func (o *Ownership) VisitBinaryExpr(e *ast.BinaryExpr, s *ownState) *ownState {
	return o.expr(e.R, o.expr(e.L, s))
}

func (o *Ownership) VisitUnaryExpr(e *ast.UnaryExpr, s *ownState) *ownState {
	return o.expr(e.E, s)
}

func (o *Ownership) VisitCondExpr(e *ast.CondExpr, s *ownState) *ownState {
	return o.expr(e.Else, o.expr(e.Then, o.expr(e.Cond, s)))
}

func (o *Ownership) VisitCallExpr(e *ast.CallExpr, s *ownState) *ownState {
	if e.Receiver != nil {
		s = o.expr(e.Receiver, s)
	}
	for _, arg := range e.Ordered {
		s = o.expr(arg, s)
	}
	return s
}

func (o *Ownership) VisitMemberExpr(e *ast.MemberExpr, s *ownState) *ownState {
	if e.Kind == ast.MemberEnumCase || e.Kind == ast.MemberUnionCase {
		return s
	}
	return o.expr(e.Obj, s)
}

func (o *Ownership) VisitStructLitExpr(e *ast.StructLitExpr, s *ownState) *ownState {
	for _, v := range e.Ordered {
		if v != nil {
			s = o.expr(v, s)
		}
	}
	return s
}

func (o *Ownership) VisitCastExpr(e *ast.CastExpr, s *ownState) *ownState {
	return o.expr(e.E, s)
}

func (o *Ownership) VisitMatchExpr(e *ast.MatchExpr, s *ownState) *ownState {
	return o.match(e.Match, s)
}

func (o *Ownership) VisitPanicExpr(e *ast.PanicExpr, s *ownState) *ownState {
	return o.expr(e.Message, s)
}
