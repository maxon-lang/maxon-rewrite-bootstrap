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
}

func (e *ExprBase) Ty() Ty     { return e.Type }
func (e *ExprBase) SetTy(t Ty) { e.Type = t }

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
