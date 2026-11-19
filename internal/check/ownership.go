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
	switch st := st.(type) {
	case *ast.LetStmt:
		return o.move(st.Init, st.Name, o.expr(st.Init, s))

	case *ast.AssignStmt:
		s = o.expr(st.Value, s)
		if target, ok := st.Target.(*ast.NameExpr); ok && target.Kind == ast.NameLocal {
			return o.move(st.Value, target.Local.Name, s.without(target.Local))
		}
		if member, ok := st.Target.(*ast.MemberExpr); ok {
			return o.expr(member.Obj, s)
		}
		return s

	case *ast.DiscardStmt:
		return o.expr(st.Value, s)

	case *ast.ExprStmt:
		s = o.expr(st.E, s)
		if _, ok := st.E.(*ast.PanicExpr); ok {
			return ownUnreachable
		}
		return s

	case *ast.IfStmt:
		s = o.expr(st.Cond, s)
		a := o.walk(st.Then, s)
		b := s
		if st.HasElse {
			b = o.walk(st.Else, s)
		}
		return mergeOwn(a, b)

	case *ast.WhileStmt:
		return o.loop(st, s, st.Body, st.Cond)

	case *ast.ForStmt:
		return o.loop(st, o.expr(st.To, o.expr(st.From, s)), st.Body, nil)

	case *ast.MatchStmt:
		return o.match(st.Match, s)

	case *ast.ReturnStmt:
		if st.Value != nil {
			o.expr(st.Value, s)
		}
		return ownUnreachable

	case *ast.BreakStmt:
		frame := o.frameOf(st.Target)
		frame.breaks = mergeOwn(frame.breaks, s)
		return ownUnreachable

	case *ast.ContinueStmt:
		frame := o.frameOf(st.Target)
		frame.continues = mergeOwn(frame.continues, s)
		return ownUnreachable
	}
	panic(diag.Internal("ownership: unhandled statement %T", st))
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
	switch e := e.(type) {
	case *ast.IntLit, *ast.BoolLit, *ast.SelfExpr:
		return s
	case *ast.StrLit:
		for _, part := range e.Parts {
			if pe, ok := part.(ast.Expr); ok {
				s = o.expr(pe, s)
			}
		}
		return s
	case *ast.NameExpr:
		if e.Kind == ast.NameLocal {
			return o.read(e, s)
		}
		return s
	case *ast.BinaryExpr:
		return o.expr(e.R, o.expr(e.L, s))
	case *ast.UnaryExpr:
		return o.expr(e.E, s)
	case *ast.CondExpr:
		return o.expr(e.Else, o.expr(e.Then, o.expr(e.Cond, s)))
	case *ast.CallExpr:
		if e.Receiver != nil {
			s = o.expr(e.Receiver, s)
		}
		for _, arg := range e.Ordered {
			s = o.expr(arg, s)
		}
		return s
	case *ast.MemberExpr:
		if e.Kind == ast.MemberEnumCase || e.Kind == ast.MemberUnionCase {
			return s
		}
		return o.expr(e.Obj, s)
	case *ast.StructLitExpr:
		for _, v := range e.Ordered {
			if v != nil {
				s = o.expr(v, s)
			}
		}
		return s
	case *ast.CastExpr:
		return o.expr(e.E, s)
	case *ast.MatchExpr:
		return o.match(e.Match, s)
	case *ast.PanicExpr:
		return o.expr(e.Message, s)
	}
	panic(diag.Internal("ownership: unhandled expression %T", e))
}
