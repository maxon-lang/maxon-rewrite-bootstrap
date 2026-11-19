// The syntax of the language minimaxon compiles, and the AST it parses into. internal/grammargen turns this file
// into the parser (parser_gen.go), the token kinds and the lexer's DFA (tokens_gen.go), and the AST nodes,
// keywords and visitors (internal/ast/nodes_gen.go). Regenerate with `go generate ./internal/syntax` after editing it.
//
// The notation is a subset of ANTLR 4. What each construct means here:
//
//   x=elem, x+=elem    A field of the node being built. Its type follows from the element: a node rule gives a
//                      pointer to its node, a set rule its interface, a helper rule its return type, a token or
//                      a group of literals its text, and a single literal a bool that says whether it was present.
//                      += appends to a slice.
//   node rule          A rule with no alternative labels and no `returns` builds the node of its capitalized name.
//   set rule           A rule whose alternatives are each a single node or set rule, or carry `# Node`, is a
//                      closed set of nodes: the AST interface of its capitalized name, with a visitor per set.
//                      A left-recursive set rule is an operator expression: earlier operator alternatives bind
//                      tighter, and `<assoc=right>` makes one right-associative.
//   helper rule        A rule with `returns [Type name]` assigns its labels to that value and returns it.
//   @ast::Node {...}   Fields of a node that parsing does not set, written `Name Type` or `Name Type = initial`;
//                      a field with a label's name gives that label its type instead. @ast::SetBase holds the
//                      fields every member of the set shares. A token label whose field is not a string is
//                      converted by the hand-written parser method <node><Field>(Token).
//   /** text */        Describes the rule or token after it in syntax errors: "expected a statement", and in
//                      the lexer's error for input that stops partway through it: "incomplete label".
//   Token : regex ;    A token rule: literals, [sets] with ranges, ~ to negate a set or one character, . for any
//                      byte, groups, and ? * +. `-> skip` drops what it matches; `-> scan` has the DFA match only
//                      the token's first byte and the hand-written lexer method scan<Token> read the rest, for a
//                      token that is not regular. `-> noSpaceBefore`, `-> noSpaceAfter` and `-> noSpace` join the
//                      token to its neighbours wherever it appears.
//   ^x, x^             Join a literal or token to the one before or after it, here only: a call's `^'('`.
//
// Lookahead is one token, or two where one does not decide; where two alternatives share a leading rule, it is
// parsed once and the token after it decides. Otherwise the earlier alternative wins, as in ANTLR.
//
// The lexer takes the longest match at each position, a keyword before a token rule and an earlier token rule
// before a later one. It never backs off to a shorter match: input that stops partway through a token is an error.
//
// The layout is part of the syntax. A line is indented with one tab per level; the lexer makes the line after a
// deeper one start with Indent, and a shallower one with a Dedent per level, which the rules below place. Tokens
// on a line are separated by one space, or none where a join says so. A comment is a // line indented like the
// line after it, or follows code after one space. One blank line separates top-level declarations, and there are
// no others.

grammar Maxon;

tokens { FileEnd, /** an indented line */ Indent, /** the end of the indented lines */ Dedent }

// Declarations

/** a declaration */
decl
    : funcDecl
    | constDecl
    | typeDeclNode
    ;

@ast::DeclBase {
	Name string
	Visibility string
	Module *Module
}

/** a type declaration */
typeDeclNode
    : typeDecl
    | enumDecl
    | unionDecl
    | aliasDecl
    ;

@ast::TypeDeclNodeBase {
	Ty Ty
}

funcDecl
    : visibility=('export' | 'public')? isStatic='static'? 'function' name=Name
      ^'(' ( receiver=('mutable' | 'immutable' | 'isolated' | 'consuming') 'self' (',' params+=param)*
          | params+=param (',' params+=param)*
          )?
      ')' ('returns' returns=typeRef)?
      body=block
      'end' end=endLabel?
    ;

@ast::FuncDecl {
	Owner NominalTy

	ParamTypes []Ty
	Specs []ParamSpec
	ReturnType Ty = VoidTy
	SelfLocal *Local
	Locals []*Local
}

/** a parameter */
param
    : name=(Name | '_') mutable='mutable'? type=typeRef
    ;

@ast::Param {
	Sym *Local
}

constDecl
    : visibility=('export' | 'public')? 'let' name=Name '=' value=expr
    ;

@ast::ConstDecl {
	Type Ty
}

typeDecl
    : visibility=('export' | 'public')? 'type' name=Name Newline
      (Indent ((fields+=fieldDecl | methods+=funcDecl) Newline)+ Dedent)?
      'end' end=endLabel?
    ;

fieldDecl
    : visibility=('export' | 'public')? ('let' | isVar='var') name=Name ('as' type=typeRef | '=' default=expr)
    ;

enumDecl
    : visibility=('export' | 'public')? 'enum' name=Name Newline
      (Indent ((cases+=enumCaseDecl | methods+=funcDecl) Newline)+ Dedent)?
      'end' end=endLabel?
    ;

enumCaseDecl
    : name=Name ('=' raw=intLit)?
    ;

unionDecl
    : visibility=('export' | 'public')? 'union' name=Name Newline
      (Indent ((cases+=unionCaseDecl | methods+=funcDecl) Newline)+ Dedent)?
      'end' end=endLabel?
    ;

unionCaseDecl
    : name=Name (^'(' (fields+=param (',' fields+=param)*)? ')')?
    ;

aliasDecl
    : visibility=('export' | 'public')? 'typealias' name=Name '=' 'int'
      ^'(' low=bound (inclusive='to' | 'upto') high=bound ')'
    ;

/** a range bound */
bound
    : negative='-'^? (value=Integer | sized=Name '.' limit=Name)
    ;

@ast::Bound {
	Value I128
}

/** a type */
typeRef
    : (qualifier+=Name '.')* name=(Name | 'Self')
    ;

@ast::TypeRef {
	Target Decl
}

endLabel
    : name=Label
    ;

// Statements

/** a block of statements */
block returns [[]Stmt stmts]
    : Newline Indent (stmts+=stmt Newline)+ Dedent
    ;

/** a statement */
stmt
    : letStmt
    | discardStmt
    | ifStmt
    | whileStmt
    | forStmt
    | matchStmt
    | returnStmt
    | breakStmt
    | continueStmt
    | assignStmt
    | exprStmt
    ;

letStmt
    : ('let' | isVar='var') name=Name '=' init=expr
    ;

@ast::LetStmt {
	Sym *Local
}

discardStmt
    : '_' '=' value=expr
    ;

assignStmt
    : target=expr '=' value=expr
    ;

exprStmt
    : e=expr
    ;

ifStmt
    : 'if' cond=expr label=Label
      then=block
      'end' end=endLabel?
      ( hasElse='else'
        ( else+=ifStmt
        | elseLabel=Label else=block 'end' elseEnd=endLabel?
        )
      )?
    ;

@ast::IfStmt {
	Else []Stmt
}

whileStmt
    : 'while' cond=expr label=Label
      body=block
      'end' end=endLabel?
    ;

forStmt
    : 'for' var=(Name | '_') 'in' from=expr (inclusive='to' | 'upto') to=expr label=Label
      body=block
      'end' end=endLabel?
    ;

@ast::ForStmt {
	Sym *Local
}

returnStmt
    : 'return' value=expr?
    ;

breakStmt
    : 'break' label=Label?
    ;

@ast::BreakStmt {
	Target Node
}

continueStmt
    : 'continue' label=Label?
    ;

@ast::ContinueStmt {
	Target Node
}

matchStmt
    : match=matchCore
    ;

// Match

matchCore
    : 'match' subject=expr label=Label Newline
      (Indent (arms+=matchArm Newline)+ Dedent)?
      'end' end=endLabel?
    ;

/** a match arm */
matchArm
    : ( isDefault='default'
      | patterns+=pattern ('or' Newline patterns+=pattern)*
      )
      ( 'then' action=stmt
      | gives='gives' value=expr
      | value=panicExpr
      )
    ;

@ast::MatchArm {
	Value Expr
}

/** a pattern */
pattern
    : caseName=Name (hasBindings=^'(' (bindings+=(Name | '_') (',' bindings+=(Name | '_'))*)? ')')?
    | value=patternValue ((inclusive='to' | 'upto') high=patternValue)?
    ;

@ast::Pattern {
	CaseIndex int = -1
	BindingLocals []*Local
}

/** a literal pattern */
patternValue returns [Expr value]
    : value=intLit
    | value=strLit
    | value=boolLit
    ;

// Expressions

/** an expression */
expr
    : intLit
    | strLit
    | boolLit
    | structLitExpr
    | selfExpr
    | nameExpr
    | panicExpr
    | matchExpr
    | '(' expr ')'
    | obj=expr '.' name=Name                                         # MemberExpr
    | callee=expr ^'(' (args+=arg (',' args+=arg)*)? ')'               # CallExpr
    | op=('-'^ | 'not') e=expr                                       # UnaryExpr
    | e=expr 'as' target=typeRef                                     # CastExpr
    | l=expr op=('*' | '/' | 'mod') r=expr                           # BinaryExpr
    | l=expr op=('+' | '-') r=expr                                   # BinaryExpr
    | l=expr op=('shl' | 'shr') r=expr                               # BinaryExpr
    | l=expr op=('==' | '!=' | '<' | '<=' | '>' | '>=') r=expr       # BinaryExpr
    | l=expr op='and' r=expr                                         # BinaryExpr
    | l=expr op='xor' r=expr                                         # BinaryExpr
    | l=expr op='or' r=expr                                          # BinaryExpr
    | <assoc=right> then=expr 'if' cond=expr 'else' else=expr        # CondExpr
    ;

@ast::ExprBase {
	Type Ty = VoidTy
}

@ast::MemberExpr {
	Kind MemberKind
	Field *FieldInfo
	CaseIndex int = -1
}

@ast::CallExpr {
	Kind CallKind
	Target *FuncDecl
	Receiver Expr
	Union *UnionTy
	CaseIndex int = -1
	Ordered []Expr
}

@ast::BinaryExpr {
	Op string
	OperandType Ty = VoidTy
}

/** an integer */
intLit
    : negative='-'^? magnitude=Integer
    ;

@ast::IntLit {
	Magnitude I128
}

strLit
    : parts=String
    ;

@ast::StrLit {
	Parts []any
}

boolLit
    : value='true'
    | 'false'
    ;

nameExpr
    : name=(Name | 'Self')
    ;

@ast::NameExpr {
	Target Decl
	Kind NameKind
	Local *Local
	Const *ConstDecl
	Field *FieldInfo
}

selfExpr
    : 'self'
    ;

@ast::SelfExpr {
	Local *Local
}

structLitExpr
    : typeName=(Name | 'Self') '{' (fields+=structField (',' fields+=structField)*)? '}'
    ;

@ast::StructLitExpr {
	Target Decl
	Record *RecordTy
	Ordered []Expr
}

structField
    : name=Name ':' value=expr
    ;

/** an argument */
arg
    : (label=Name ':')? value=expr
    ;

panicExpr
    : 'panic' ^'(' message=expr ')'
    ;

matchExpr
    : match=matchCore
    ;

// Tokens. The token rules below and the keywords the rules above use compile into one DFA (tokens_gen.go).

/** a name */
Name : [A-Za-z_] [A-Za-z0-9_]* ;

/** an integer */
Integer
    : [0-9] [0-9_]*
    | '0x' '_'* [0-9A-Fa-f] [0-9A-Fa-f_]*
    | '0b' '_'* [01] [01_]*
    | '0o' '_'* [0-7] [0-7_]*
    ;

/** a string */
String : '"' (~["\\{}\n] | '\\' . | '{' ~[}\n]* '}')* '"' -> scan ;

/** a label */
Label : '\'' ~['\n]* '\'' ;

/** the end of the line */
Newline : '\n' ;

Whitespace : [ \t\r]+ -> skip ;

/** a comment */
LineComment : '//' ~[\n]* -> skip ;

/** a block comment */
BlockComment : '/*' (~'*' | '*'+ ~[*/])* '*'+ '/' -> skip ;

LParen : '(' -> noSpaceAfter ;
RParen : ')' -> noSpaceBefore ;
LBrace : '{' -> noSpace ;
RBrace : '}' -> noSpaceBefore ;
Comma : ',' -> noSpaceBefore ;
Colon : ':' -> noSpaceBefore ;
Dot : '.' -> noSpace ;
Plus : '+' ;
Minus : '-' ;
Star : '*' ;
Slash : '/' ;
Assign : '=' ;
EqEq : '==' ;
NotEq : '!=' ;
Lt : '<' ;
Le : '<=' ;
Gt : '>' ;
Ge : '>=' ;
