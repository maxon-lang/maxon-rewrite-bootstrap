package ast

import (
	"minimaxon/internal/diag"
)

type Pos struct {
	File string
	Line int
	Col  int
}

func (p *Pos) P() *Pos { return p }

func (p *Pos) Error(code, msg string) *diag.CompileError {
	return diag.NewError(code, msg, p.File, p.Line, p.Col)
}

type Node interface {
	P() *Pos
}

type Program struct {
	Decls []Decl
}

type Decl interface {
	Node
	Base() *DeclBase
}

func (d *DeclBase) Base() *DeclBase { return d }

func (d *DeclBase) IsExported() bool { return d.Visibility == "export" || d.Visibility == "public" }

type TypeDeclNode interface {
	Decl
	TypeBase() *TypeDeclNodeBase
}

func (t *TypeDeclNodeBase) TypeBase() *TypeDeclNodeBase { return t }

func (f *FuncDecl) Symbol() string {
	if f.Owner == nil {
		if f.Generic != nil {
			return instanceName(f.Name, f.TypeArgs)
		}
		return f.Name
	}
	return f.Owner.Name() + "." + f.Name
}

func (f *FuncDecl) IsMethod() bool { return f.Owner != nil && !f.IsStatic }

type Stmt interface {
	Node
	stmt()
}

func (*StmtBase) stmt() {}

type Expr interface {
	Node
	Ty() Ty
	SetTy(Ty)
	IsConsumed() bool
	MarkConsumed()
}

func (e *ExprBase) Ty() Ty     { return e.Type }
func (e *ExprBase) SetTy(t Ty) { e.Type = t }

// IsConsumed is whether the ownership pass found the expression in a position that takes ownership of its value: a
// binding, an assignment, a sink argument, a return, a field or payload, or a branch of a conditional or a match. A
// place there is moved out of; a fresh value there needs no one to drop it. Lowering reads this to place drops, so the
// pass is the only place that decides what a move is.
func (e *ExprBase) IsConsumed() bool { return e.Consumed }

func (e *ExprBase) MarkConsumed() { e.Consumed = true }

func (l *IntLit) Value() I128 {
	if l.Negative {
		return l.Magnitude.Neg()
	}
	return l.Magnitude
}

func (s *StrLit) IsPlain() bool {
	for _, p := range s.Parts {
		if _, ok := p.(string); !ok {
			return false
		}
	}
	return true
}

func (p *Pattern) Kind() PatternKind {
	switch {
	case p.CaseName != "":
		return PatternCase
	case p.High != nil:
		return PatternRange
	}
	return PatternLiteral
}

func (a *MatchArm) Kind() ArmKind {
	switch {
	case a.Action != nil:
		return ArmThen
	case a.Gives:
		return ArmGives
	}
	return ArmPanic
}

// IsStorage is whether an expression reads a value that already lives somewhere, in a variable, a field or an element,
// and so borrows it, as opposed to making a new value. It asks about four members of Expr, so it is a type switch
// and not a visitor.
func IsStorage(e Expr) bool {
	switch e := e.(type) {
	case *NameExpr:
		return e.Kind == NameLocal || e.Kind == NameSelfField
	case *SelfExpr, *IndexExpr:
		return true
	case *MemberExpr:
		return e.Kind == MemberField
	}
	return false
}
