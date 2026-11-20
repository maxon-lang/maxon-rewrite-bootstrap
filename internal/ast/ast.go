package ast

import (
	"minimaxon/internal/diag"
)

// Pos is where a node begins: an offset in the text of its module, which the module's Positions locate. A node the
// compiler makes has the position of the node it stands for. It names the module by handle, so a node holds no pointer
// to the module's text.
type Pos struct {
	Mod ModuleID
	Off uint32
}

func (p *Pos) P() *Pos { return p }

// Site is the position as a diagnostic holds it.
func (p Pos) Site() diag.Site { return diag.Site{Module: int32(p.Mod), Offset: p.Off} }

// Error is a diagnostic at the position. It is located (given a file, line and column) when the compile reports it,
// which is the first time anything needs a line index of the module's text.
func (p Pos) Error(code, msg string) *diag.CompileError {
	return diag.NewSiteError(code, msg, p.Site())
}

// NodeID is the index of a node among the nodes of its module, which its module's Arena counts in the order the nodes
// are made, from 1: the zero value is no node, so a table that is indexed by it starts empty. A side table that a
// phase fills in is a slice indexed by NodeID, so what a phase knows about a node is not part of the node. It is
// 32 bits: a node covers at least one byte of the module's text, which is at most 4 GB.
type NodeID uint32

type Node interface {
	P() *Pos
}

// Head is what every node starts with: where it is and which node it is in its module.
type Head struct {
	Pos
	ID NodeID
}

// Program is what parsing a module makes: its declarations, the arena that holds every node of them, and where their
// text is.
type Program struct {
	Decls     []Decl
	Nodes     *Arena
	Positions *Positions
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
			return functionInstanceName(f.Name, f.TypeArgs)
		}
		return f.Name
	}
	if len(f.Owner.Base().Args) > 0 {
		return "(" + f.Owner.Name() + ")." + f.Name
	}
	return f.Owner.Name() + "." + f.Name
}

func (f *FuncDecl) IsMethod() bool { return f.Owner != nil && !f.IsStatic }

// IsDestructor is whether the method ends its receiver: it is written 'destructs self', so the signature alone says so.
func (f *FuncDecl) IsDestructor() bool { return ConventionOf(f.Receiver) == ConvDestructs }

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
// binding, an assignment, a consumes argument, a return, a field or payload, or a branch of a conditional or a match. A
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
