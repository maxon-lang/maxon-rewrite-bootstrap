package main

import "fmt"

type ownState struct {
	moved map[*Local]Node
	dead  bool
}

var (
	ownUnreachable = &ownState{dead: true}
	ownClean       = &ownState{}
)

func (s *ownState) tryGetMove(l *Local) (Node, bool) {
	if s.moved == nil {
		return nil, false
	}
	site, ok := s.moved[l]
	return site, ok
}

func (s *ownState) with(l *Local, site Node) *ownState {
	if s.dead {
		return s
	}
	m := make(map[*Local]Node, len(s.moved)+1)
	for k, v := range s.moved {
		m[k] = v
	}
	m[l] = site
	return &ownState{moved: m}
}

func (s *ownState) without(l *Local) *ownState {
	if s.dead || s.moved == nil {
		return s
	}
	if _, ok := s.moved[l]; !ok {
		return s
	}
	if len(s.moved) == 1 {
		return ownClean
	}
	m := make(map[*Local]Node, len(s.moved))
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
	var m map[*Local]Node
	for k, v := range b.moved {
		if _, ok := a.moved[k]; !ok {
			if m == nil {
				m = make(map[*Local]Node, len(a.moved)+1)
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
	target    Node
	breaks    *ownState
	continues *ownState
}

func newOwnFrame(target Node) *ownFrame {
	return &ownFrame{target: target, breaks: ownUnreachable, continues: ownUnreachable}
}

type Ownership struct {
	errors      *[]*CompileError
	frames      []*ownFrame
	moveTargets map[Node]string
}

func NewOwnership(errs *[]*CompileError) *Ownership {
	return &Ownership{errors: errs, moveTargets: map[Node]string{}}
}

func isMovable(l *Local) bool {
	if !l.IsVar || l.IsParam {
		return false
	}
	switch l.Type.(type) {
	case *RecordTy, *UnionTy:
		return true
	}
	return false
}

func (o *Ownership) Check(f *FuncDecl) {
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

func (o *Ownership) frameOf(target Node) *ownFrame {
	for i := len(o.frames) - 1; i >= 0; i-- {
		if o.frames[i].target == target {
			return o.frames[i]
		}
	}
	panic(Internal("ownership: a jump has no enclosing frame"))
}

func (o *Ownership) walk(body []Stmt, s *ownState) *ownState {
	for _, st := range body {
		s = o.stmt(st, s)
	}
	return s
}

func (o *Ownership) stmt(st Stmt, s *ownState) *ownState {
	switch st := st.(type) {
	case *LetStmt:
		return o.move(st.Init, st.Name, o.expr(st.Init, s))

	case *AssignStmt:
		s = o.expr(st.Value, s)
		if target, ok := st.Target.(*NameExpr); ok && target.Kind == NameLocal {
			return o.move(st.Value, target.Local.Name, s.without(target.Local))
		}
		if member, ok := st.Target.(*MemberExpr); ok {
			return o.expr(member.Obj, s)
		}
		return s

	case *DiscardStmt:
		return o.expr(st.Value, s)

	case *ExprStmt:
		s = o.expr(st.E, s)
		if _, ok := st.E.(*PanicExpr); ok {
			return ownUnreachable
		}
		return s

	case *IfStmt:
		s = o.expr(st.Cond, s)
		a := o.walk(st.Then, s)
		b := s
		if st.HasElse {
			b = o.walk(st.Else, s)
		}
		return mergeOwn(a, b)

	case *WhileStmt:
		return o.loop(st, s, st.Body, st.Cond)

	case *ForStmt:
		return o.loop(st, o.expr(st.To, o.expr(st.From, s)), st.Body, nil)

	case *MatchStmt:
		return o.match(st.Match, s)

	case *ReturnStmt:
		if st.Value != nil {
			o.expr(st.Value, s)
		}
		return ownUnreachable

	case *BreakStmt:
		frame := o.frameOf(st.Target)
		frame.breaks = mergeOwn(frame.breaks, s)
		return ownUnreachable

	case *ContinueStmt:
		frame := o.frameOf(st.Target)
		frame.continues = mergeOwn(frame.continues, s)
		return ownUnreachable
	}
	panic(Internal("ownership: unhandled statement %T", st))
}

func (o *Ownership) loop(loop Node, s *ownState, body []Stmt, cond Expr) *ownState {
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

func (o *Ownership) match(m *MatchCore, s *ownState) *ownState {
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
			if arm.Kind == ArmPanic {
				a = ownUnreachable
			}
		}
		result = mergeOwn(result, a)
	}
	o.frames = o.frames[:len(o.frames)-1]
	return mergeOwn(result, frame.breaks)
}

func (o *Ownership) move(value Expr, into string, s *ownState) *ownState {
	if s.dead {
		return s
	}
	n, ok := value.(*NameExpr)
	if !ok || n.Kind != NameLocal || !isMovable(n.Local) {
		return s
	}
	o.moveTargets[value] = into
	return s.with(n.Local, value)
}

func (o *Ownership) read(n *NameExpr, s *ownState) *ownState {
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
	*o.errors = append(*o.errors, n.Error(ErrUseAfterMove,
		fmt.Sprintf("'%s' was moved into '%s' at line %d; assign it again before reading it, or bind a clone", n.Name, into, site.P().Line)))
	return s.without(l)
}

func (o *Ownership) expr(e Expr, s *ownState) *ownState {
	switch e := e.(type) {
	case *IntLit, *BoolLit, *SelfExpr:
		return s
	case *StrLit:
		for _, part := range e.Parts {
			if pe, ok := part.(Expr); ok {
				s = o.expr(pe, s)
			}
		}
		return s
	case *NameExpr:
		if e.Kind == NameLocal {
			return o.read(e, s)
		}
		return s
	case *BinaryExpr:
		return o.expr(e.R, o.expr(e.L, s))
	case *UnaryExpr:
		return o.expr(e.E, s)
	case *CondExpr:
		return o.expr(e.Else, o.expr(e.Then, o.expr(e.Cond, s)))
	case *CallExpr:
		if e.Receiver != nil {
			s = o.expr(e.Receiver, s)
		}
		for _, arg := range e.Ordered {
			s = o.expr(arg, s)
		}
		return s
	case *MemberExpr:
		if e.Kind == MemberEnumCase || e.Kind == MemberUnionCase {
			return s
		}
		return o.expr(e.Obj, s)
	case *StructLitExpr:
		for _, v := range e.Ordered {
			if v != nil {
				s = o.expr(v, s)
			}
		}
		return s
	case *CastExpr:
		return o.expr(e.E, s)
	case *MatchExpr:
		return o.match(e.Match, s)
	case *PanicExpr:
		return o.expr(e.Message, s)
	}
	panic(Internal("ownership: unhandled expression %T", e))
}
