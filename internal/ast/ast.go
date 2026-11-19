package ast

import (
	"minimaxon/internal/diag"
)

//go:generate go run minimaxon/internal/astgen

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

type Decl interface {
	Node
	Base() *DeclBase
}

func (d *DeclBase) Base() *DeclBase { return d }

func (d *DeclBase) IsExported() bool { return d.Visibility == "export" || d.Visibility == "public" }

type TypeDeclNode interface {
	Decl
	TypeBase() *TypeDeclBase
}

func (t *TypeDeclBase) TypeBase() *TypeDeclBase { return t }

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

func (s *StrLit) IsPlain() bool {
	for _, p := range s.Parts {
		if _, ok := p.(string); !ok {
			return false
		}
	}
	return true
}
