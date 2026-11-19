package main

type Pos struct {
	File string
	Line int
	Col  int
}

func (p *Pos) P() *Pos { return p }

func (p *Pos) Error(code, msg string) *CompileError {
	return NewError(code, msg, p.File, p.Line, p.Col)
}

type Node interface {
	P() *Pos
}

type TypeRef struct {
	Pos
	Qualifier  []string
	Name       string
	Capability string
	Target     Decl
}

type Decl interface {
	Node
	Base() *DeclBase
}

type DeclBase struct {
	Pos
	Name       string
	Visibility string
	Module     *Module
}

func (d *DeclBase) Base() *DeclBase { return d }

func (d *DeclBase) IsExported() bool { return d.Visibility == "export" || d.Visibility == "public" }

type TypeDeclNode interface {
	Decl
	TypeBase() *TypeDeclBase
}

type TypeDeclBase struct {
	DeclBase
	Ty Ty
}

func (t *TypeDeclBase) TypeBase() *TypeDeclBase { return t }

type Param struct {
	Pos
	Name    string
	Mutable bool
	Type    *TypeRef
	Sym     *Local
}

type FuncDecl struct {
	DeclBase
	Params   []*Param
	Returns  *TypeRef
	Body     []Stmt
	IsStatic bool
	Receiver string
	Owner    NominalTy

	ParamTypes []Ty
	Specs      []ParamSpec
	ReturnType Ty
	SelfLocal  *Local
	Locals     []*Local
}

func (f *FuncDecl) Symbol() string {
	if f.Owner == nil {
		return f.Name
	}
	return f.Owner.Name() + "." + f.Name
}

func (f *FuncDecl) IsMethod() bool { return f.Owner != nil && !f.IsStatic }

type ParamSpec struct {
	Name  string
	Type  Ty
	ByRef bool
}

type FieldDecl struct {
	Pos
	Name       string
	Visibility string
	IsVar      bool
	Type       *TypeRef
	Default    Expr
}

type TypeDecl struct {
	TypeDeclBase
	Fields  []*FieldDecl
	Methods []*FuncDecl
}

type EnumCaseDecl struct {
	Pos
	Name   string
	Raw    I128
	HasRaw bool
}

type EnumDecl struct {
	TypeDeclBase
	Cases   []*EnumCaseDecl
	Methods []*FuncDecl
}

type UnionCaseDecl struct {
	Pos
	Name   string
	Fields []*Param
}

type UnionDecl struct {
	TypeDeclBase
	Cases   []*UnionCaseDecl
	Methods []*FuncDecl
}

type AliasDecl struct {
	TypeDeclBase
	Min I128
	Max I128
}

type ConstDecl struct {
	DeclBase
	Value Expr
	Type  Ty
}

type Program struct {
	Decls []Decl
}

type Stmt interface {
	Node
	stmt()
}

type StmtBase struct{ Pos }

func (*StmtBase) stmt() {}

type LetStmt struct {
	StmtBase
	Name  string
	IsVar bool
	Init  Expr
	Sym   *Local
}

type AssignStmt struct {
	StmtBase
	Target Expr
	Value  Expr
}

type DiscardStmt struct {
	StmtBase
	Value Expr
}

type ExprStmt struct {
	StmtBase
	E Expr
}

type IfStmt struct {
	StmtBase
	Cond    Expr
	Label   string
	Then    []Stmt
	Else    []Stmt
	HasElse bool
}

type WhileStmt struct {
	StmtBase
	Cond  Expr
	Label string
	Body  []Stmt
}

type ForStmt struct {
	StmtBase
	Var       string
	From      Expr
	To        Expr
	Inclusive bool
	Label     string
	Body      []Stmt
	Sym       *Local
}

type ReturnStmt struct {
	StmtBase
	Value Expr
}

type BreakStmt struct {
	StmtBase
	Label  string
	Target Node
}

type ContinueStmt struct {
	StmtBase
	Label  string
	Target Node
}

type MatchStmt struct {
	StmtBase
	Match *MatchCore
}

type PatternKind int

const (
	PatternLiteral PatternKind = iota
	PatternRange
	PatternCase
)

type Pattern struct {
	Pos
	Kind        PatternKind
	Value       Expr
	High        Expr
	Inclusive   bool
	CaseName    string
	Bindings    []string
	HasBindings bool

	CaseIndex     int
	BindingLocals []*Local
}

type ArmKind int

const (
	ArmThen ArmKind = iota
	ArmGives
	ArmPanic
)

type MatchArm struct {
	Pos
	Patterns  []*Pattern
	IsDefault bool
	Kind      ArmKind
	Action    Stmt
	Value     Expr
}

type MatchCore struct {
	Pos
	Subject Expr
	Label   string
	Arms    []*MatchArm
}

type Expr interface {
	Node
	Ty() Ty
	SetTy(Ty)
}

type ExprBase struct {
	Pos
	Type Ty
}

func (e *ExprBase) Ty() Ty     { return e.Type }
func (e *ExprBase) SetTy(t Ty) { e.Type = t }

type IntLit struct {
	ExprBase
	Value I128
}

type BoolLit struct {
	ExprBase
	Value bool
}

type StrLit struct {
	ExprBase
	Parts []any
}

func (s *StrLit) IsPlain() bool {
	for _, p := range s.Parts {
		if _, ok := p.(string); !ok {
			return false
		}
	}
	return true
}

type NameKind int

const (
	NameUnresolved NameKind = iota
	NameLocal
	NameConst
	NameSelfField
)

type NameExpr struct {
	ExprBase
	Name   string
	Target Decl
	Kind   NameKind
	Local  *Local
	Const  *ConstDecl
	Field  *FieldInfo
}

type SelfExpr struct {
	ExprBase
	Local *Local
}

type BinaryExpr struct {
	ExprBase
	Op          string
	L           Expr
	R           Expr
	OperandType Ty
}

type UnaryExpr struct {
	ExprBase
	Op string
	E  Expr
}

type CondExpr struct {
	ExprBase
	Cond Expr
	Then Expr
	Else Expr
}

type Arg struct {
	Pos
	Label string
	Value Expr
}

type CallKind int

const (
	CallUnresolved CallKind = iota
	CallFunction
	CallMethod
	CallStatic
	CallUnionCtor
	CallPrint
	CallPrintError
)

type CallExpr struct {
	ExprBase
	Callee Expr
	Args   []*Arg

	Kind      CallKind
	Target    *FuncDecl
	Receiver  Expr
	Union     *UnionTy
	CaseIndex int
	Ordered   []Expr
}

type MemberKind int

const (
	MemberUnresolved MemberKind = iota
	MemberField
	MemberEnumCase
	MemberUnionCase
	MemberEnumName
	MemberEnumOrdinal
	MemberEnumRaw
	MemberUnionName
	MemberUnionOrdinal
)

type MemberExpr struct {
	ExprBase
	Obj  Expr
	Name string

	Kind      MemberKind
	Field     *FieldInfo
	CaseIndex int
}

type StructField struct {
	Name  string
	Value Expr
	Site  *Pos
}

type StructLitExpr struct {
	ExprBase
	TypeName string
	Target   Decl
	Fields   []StructField
	Record   *RecordTy
	Ordered  []Expr
}

type CastExpr struct {
	ExprBase
	E      Expr
	Target *TypeRef
}

type MatchExpr struct {
	ExprBase
	Match *MatchCore
}

type PanicExpr struct {
	ExprBase
	Message Expr
}
