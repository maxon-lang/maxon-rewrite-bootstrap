# The AST nodes, their enums, and the closed sets the phases dispatch over. internal/astgen turns this file into
# nodes_gen.go; regenerate with `go generate ./internal/ast` after editing it.
#
# set <Interface> <noun>        A closed set of nodes. The interface itself is hand-written in ast.go. Each set
#                               gets three visitors, one method per member, and a dispatch function for each:
#                                 <Set>Visitor            Visit<Node>(n *<Node>)
#                                 <Set>ResultVisitor[R]   Visit<Node>(n *<Node>) R
#                                 <Set>StateVisitor[S, R] Visit<Node>(n *<Node>, s S) R
#                               Adding a member therefore fails the build in every phase that visits the set.
# enum <Name>                   An int enum; each indented line is one constant, numbered from zero.
# node <Name> [in <Set>...]     A struct. Each indented line is a field `<Name> <Type>`, or a bare type to embed.
#                               A blank line inside a node is kept, to group its fields.

set Decl declaration
set TypeDeclNode type declaration
set Stmt statement
set Expr expression

node TypeRef
	Pos
	Qualifier []string
	Name string
	Capability string
	Target Decl

# Declarations

node DeclBase
	Pos
	Name string
	Visibility string
	Module *Module

node TypeDeclBase
	DeclBase
	Ty Ty

node Param
	Pos
	Name string
	Mutable bool
	Type *TypeRef
	Sym *Local

node FuncDecl in Decl
	DeclBase
	Params []*Param
	Returns *TypeRef
	Body []Stmt
	IsStatic bool
	Receiver string
	Owner NominalTy

	ParamTypes []Ty
	Specs []ParamSpec
	ReturnType Ty
	SelfLocal *Local
	Locals []*Local

node ParamSpec
	Name string
	Type Ty
	ByRef bool

node FieldDecl
	Pos
	Name string
	Visibility string
	IsVar bool
	Type *TypeRef
	Default Expr

node TypeDecl in Decl TypeDeclNode
	TypeDeclBase
	Fields []*FieldDecl
	Methods []*FuncDecl

node EnumCaseDecl
	Pos
	Name string
	Raw I128
	HasRaw bool

node EnumDecl in Decl TypeDeclNode
	TypeDeclBase
	Cases []*EnumCaseDecl
	Methods []*FuncDecl

node UnionCaseDecl
	Pos
	Name string
	Fields []*Param

node UnionDecl in Decl TypeDeclNode
	TypeDeclBase
	Cases []*UnionCaseDecl
	Methods []*FuncDecl

node AliasDecl in Decl TypeDeclNode
	TypeDeclBase
	Min I128
	Max I128

node ConstDecl in Decl
	DeclBase
	Value Expr
	Type Ty

node Program
	Decls []Decl

# Statements

node StmtBase
	Pos

node LetStmt in Stmt
	StmtBase
	Name string
	IsVar bool
	Init Expr
	Sym *Local

node AssignStmt in Stmt
	StmtBase
	Target Expr
	Value Expr

node DiscardStmt in Stmt
	StmtBase
	Value Expr

node ExprStmt in Stmt
	StmtBase
	E Expr

node IfStmt in Stmt
	StmtBase
	Cond Expr
	Label string
	Then []Stmt
	Else []Stmt
	HasElse bool

node WhileStmt in Stmt
	StmtBase
	Cond Expr
	Label string
	Body []Stmt

node ForStmt in Stmt
	StmtBase
	Var string
	From Expr
	To Expr
	Inclusive bool
	Label string
	Body []Stmt
	Sym *Local

node ReturnStmt in Stmt
	StmtBase
	Value Expr

node BreakStmt in Stmt
	StmtBase
	Label string
	Target Node

node ContinueStmt in Stmt
	StmtBase
	Label string
	Target Node

node MatchStmt in Stmt
	StmtBase
	Match *MatchCore

# Match

enum PatternKind
	PatternLiteral
	PatternRange
	PatternCase

node Pattern
	Pos
	Kind PatternKind
	Value Expr
	High Expr
	Inclusive bool
	CaseName string
	Bindings []string
	HasBindings bool

	CaseIndex int
	BindingLocals []*Local

enum ArmKind
	ArmThen
	ArmGives
	ArmPanic

node MatchArm
	Pos
	Patterns []*Pattern
	IsDefault bool
	Kind ArmKind
	Action Stmt
	Value Expr

node MatchCore
	Pos
	Subject Expr
	Label string
	Arms []*MatchArm

# Expressions

node ExprBase
	Pos
	Type Ty

node IntLit in Expr
	ExprBase
	Value I128

node BoolLit in Expr
	ExprBase
	Value bool

node StrLit in Expr
	ExprBase
	Parts []any

enum NameKind
	NameUnresolved
	NameLocal
	NameConst
	NameSelfField

node NameExpr in Expr
	ExprBase
	Name string
	Target Decl
	Kind NameKind
	Local *Local
	Const *ConstDecl
	Field *FieldInfo

node SelfExpr in Expr
	ExprBase
	Local *Local

node BinaryExpr in Expr
	ExprBase
	Op string
	L Expr
	R Expr
	OperandType Ty

node UnaryExpr in Expr
	ExprBase
	Op string
	E Expr

node CondExpr in Expr
	ExprBase
	Cond Expr
	Then Expr
	Else Expr

node Arg
	Pos
	Label string
	Value Expr

enum CallKind
	CallUnresolved
	CallFunction
	CallMethod
	CallStatic
	CallUnionCtor
	CallPrint
	CallPrintError

node CallExpr in Expr
	ExprBase
	Callee Expr
	Args []*Arg

	Kind CallKind
	Target *FuncDecl
	Receiver Expr
	Union *UnionTy
	CaseIndex int
	Ordered []Expr

enum MemberKind
	MemberUnresolved
	MemberField
	MemberEnumCase
	MemberUnionCase
	MemberEnumName
	MemberEnumOrdinal
	MemberEnumRaw
	MemberUnionName
	MemberUnionOrdinal

node MemberExpr in Expr
	ExprBase
	Obj Expr
	Name string

	Kind MemberKind
	Field *FieldInfo
	CaseIndex int

node StructField
	Name string
	Value Expr
	Site *Pos

node StructLitExpr in Expr
	ExprBase
	TypeName string
	Target Decl
	Fields []StructField
	Record *RecordTy
	Ordered []Expr

node CastExpr in Expr
	ExprBase
	E Expr
	Target *TypeRef

node MatchExpr in Expr
	ExprBase
	Match *MatchCore

node PanicExpr in Expr
	ExprBase
	Message Expr
