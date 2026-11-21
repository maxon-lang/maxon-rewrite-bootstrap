// The syntax of the language minimaxon compiles, and the AST it parses into. internal/grammargen turns this file
// into the parser (parser_gen.maxon), the token kinds and the lexer's DFA (tokens_gen.maxon), the AST nodes and
// keywords (self/ast/nodes_gen.maxon), and the keywords cmd/convert reserves. Regenerate with `go generate ./internal/...`
// after editing it.
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
//   rule builds Node   A second way to write the node of another rule, where a context takes a different form of it:
//                      the rule parses into that node, and its labels are fields of it.
//   Keyword            In a group, every keyword of the grammar not named in it: (Name | Keyword) is any word.
//   @ast::Node {...}   Fields of a node that parsing does not set, written `Name Type` or `Name Type = initial`;
//                      a field with a label's name gives that label its type instead. @ast::SetBase holds the
//                      fields every member of the set shares. A token label whose field is not a string is
//                      converted by the hand-written parser method <node><Field>(Token). A field of the type
//                      Slot[Counter] stores nothing: it says that the nodes of its kind are numbered from the
//                      counter of that name, 1 for the first, after the parse (ast.Tree.NumberSlots), so that a
//                      table a phase fills in for those nodes is that long (ast.Slots).
//   @ast::Structs {...} The nodes that are Go structs, which are the declarations: later phases fill in their
//                      semantic fields and other modules read them. Every other node is a record in its module's
//                      tree (ast.Tree): a kind, flags, a small field, where it starts and two words, with the rest
//                      of its fields in a block of the tree's list array. A record holds flags, the offsets of
//                      names, labels and strings in the module's text, integers that fit 64 bits, the numbers of
//                      other records, and lists of them; a field of the type Word is one 32-bit word that the
//                      hand-written conversion makes of a token. Code reads a record through a view of its type
//                      (`ast.IfStmt`), which has an accessor for each field, and a set of records is a view that
//                      knows its node's kind. A struct holds views, never a pointer to a record.
//   /** text */        Describes the rule or token after it in syntax errors: "expected a statement", and in
//                      the lexer's error for input that stops partway through it: "incomplete label".
//   Token : regex ;    A token rule: literals, [sets] with ranges, ~ to negate a set or one character, . for any
//                      byte, groups, and ? * +. `-> skip` drops what it matches; `-> scan` has the DFA match only
//                      the token's first byte and the hand-written lexer method scan<Token> read the rest, for a
//                      token that is not regular. `-> noSpaceBefore`, `-> noSpaceAfter` and `-> noSpace` join the
//                      token to its neighbours wherever it appears.
//   ^x, x^             Join a literal or token to the one before or after it, here only: a call's `^'('`.
//   {name}?           A predicate: at the start of the only alternative of an optional group, the group is entered only where the
//                      hand-written parser method name is also true, as `atHandlerBinding` decides what the tokens cannot.
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
// line after it, or follows code after one space. One blank line separates top-level declarations, and one comes
// before each method or static function of a type, an enum or a union that is not the first line of its body (an
// interface's signatures have none between them); there are no others.

grammar Maxon;

tokens { FileEnd, /** an indented line */ Indent, /** the end of the indented lines */ Dedent }

// Declarations

@ast::Structs {
	FuncDecl ConstDecl TypeDecl EnumDecl UnionDecl AliasDecl InterfaceDecl
	Param FieldDecl EnumCaseDecl UnionCaseDecl Bound TypeParam Constraint FuncSig
}

/** a declaration */
decl
    : funcDecl
    | constDecl
    | typeDeclNode
    ;

@ast::DeclBase {
	Ordinal uint32
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
    | interfaceDecl
    ;

@ast::TypeDeclNodeBase {
	Ty Ty
}

funcDecl
    : visibility=('export' | 'public')? isStatic='static'? 'function' name=(Name | 'from')
      ^'(' ( receiver=('mutates' | 'consumes' | 'destructs') 'self' (',' params+=param)*
          | params+=param (',' params+=param)*
          )?
      ')' typeParams=usesClause? ('returns' returns=typeRef)? ('throws' throws=typeRef)? where=whereClause?
      body=block
      'end' end=endLabel?
    ;

@ast::FuncDecl {
	Intrinsic Intrinsic
	Owner NominalTy
	Generics []*TypeParamTy
	Bounds []ParamBound
	Generic *FuncDecl
	TypeArgs []Ty
	Instances *InstanceTable
	Uses []CallUse
	LinkName string

	Pass Passing
	ParamTypes []Ty
	ParamLocals []LocalID
	ReturnType Ty = VoidTy
	ErrorType Ty
	SelfLocal *Local
	Locals []*Local
}

/** a parameter */
param
    : convention=('mutates' | 'consumes' | 'sets')? name=(Name | '_' | Keyword) type=typeRef
    ;

constDecl
    : visibility=('export' | 'public')? 'let' name=(Name | 'from') '=' value=expr
    ;

@ast::ConstDecl {
	Type Ty
	Low I128
	High I128
}

typeDecl
    : visibility=('export' | 'public')? 'type' name=Name ('is' noncopyable='noncopyable')? typeParams=usesClause? conforms=conformsClause? where=whereClause? Newline
      (Indent ((aliases+=aliasDecl | fields+=fieldDecl | methods+=funcDecl) Newline)+ Dedent)?
      'end' end=endLabel?
    ;

fieldDecl
    : visibility=('export' | 'public')? ('let' | isVar='var') name=(Name | 'from') ('as' type=typeRef | '=' default=expr)
    ;

enumDecl
    : visibility=('export' | 'public')? 'enum' name=Name conforms=conformsClause? Newline
      (Indent ((cases+=enumCaseDecl | methods+=funcDecl) Newline)+ Dedent)?
      'end' end=endLabel?
    ;

enumCaseDecl
    : name=(Name | 'from') ('=' raw=intLit)?
    ;

unionDecl
    : visibility=('export' | 'public')? 'union' name=Name ('is' noncopyable='noncopyable')? typeParams=usesClause? conforms=conformsClause? where=whereClause? Newline
      (Indent ((cases+=unionCaseDecl | methods+=funcDecl) Newline)+ Dedent)?
      'end' end=endLabel?
    ;

unionCaseDecl
    : name=(Name | 'from') (^'(' (fields+=param (',' fields+=param)*)? ')')?
    ;

aliasDecl
    : visibility=('export' | 'public')? 'typealias' name=Name '='
      ( 'int' ^'(' low=bound (inclusive='to' | 'upto') high=bound ')'
      | instance=aliasInstance
      )
    ;

// A typealias takes its type arguments as a bare list, which a type elsewhere cannot because the comma would end the
// list that holds it.
aliasInstance builds TypeRef
    : (qualifier+=(Name | 'export') '.')* name=(Name | 'Self') ('with' typeArgs=aliasTypeArgs)?
    ;

aliasTypeArgs returns [[]*TypeRef args]
    : args+=typeRef (',' args+=typeRef)*
    | '(' args+=typeRef (',' args+=typeRef)* ')'
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
    : (qualifier+=(Name | 'export') '.')* name=(Name | 'Self') ('with' typeArgs=typeArgs)?
    ;

/** type arguments */
typeArgs returns [[]*TypeRef args]
    : args+=typeRef
    | '(' args+=typeRef (',' args+=typeRef)* ')'
    ;

usesClause returns [[]*TypeParam params]
    : 'uses' params+=typeParam (',' params+=typeParam)*
    ;

typeParam
    : name=Name
    ;

@ast::TypeParam {
	Ty *TypeParamTy
}

whereClause returns [[]*Constraint constraints]
    : 'where' constraints+=constraint (',' constraints+=constraint)*
    ;

/** a constraint */
constraint
    : param=Name 'implements' bounds+=typeRef ('and' bounds+=typeRef)*
    ;

conformsClause returns [[]*TypeRef interfaces]
    : 'implements' interfaces+=typeRef (',' interfaces+=typeRef)*
    ;

interfaceDecl
    : visibility=('export' | 'public')? 'interface' name=Name Newline
      (Indent (methods+=funcSig Newline)+ Dedent)?
      'end' end=endLabel?
    ;

funcSig
    : 'function' name=(Name | 'from')
      ^'(' ( receiver=('mutates' | 'consumes' | 'destructs') 'self' (',' params+=param)*
          | params+=param (',' params+=param)*
          )?
      ')' ('returns' returns=typeRef)? ('throws' throws=typeRef)?
    ;

@ast::FuncSig {
	Pass Passing
	ReturnType Ty = VoidTy
	ErrorType Ty
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
    | ifTryStmt
    | ifStmt
    | whileStmt
    | forStmt
    | matchStmt
    | whenStmt
    | tryStmt
    | throwStmt
    | returnStmt
    | breakStmt
    | continueStmt
    | assignStmt
    | exprStmt
    ;

letStmt
    : 'let' name=(Name | 'from') '=' init=expr
    | isVar='var' name=(Name | 'from') ('=' init=expr | 'as' type=typeRef)
    ;

@ast::LetStmt {
	Local Slot[Local]
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

// A conditional try runs its block only when its call succeeds, binding the result, and its else block only when the call
// fails, naming the error.
ifTryStmt
    : 'if' (isLet='let' name=Name '=' | isVar='var' name=Name '=')? 'try' call=expr label=Label
      then=block
      'end' end=endLabel?
      ( hasElse='else' ('(' errName=Name ')')? elseLabel=Label else=block 'end' elseEnd=endLabel? )?
    ;

@ast::IfTryStmt {
	Local Slot[Local]
}

whileStmt
    : 'while' cond=expr label=Label
      body=block
      'end' end=endLabel?
    ;

forStmt
    : 'for' var=(Name | 'from' | '_') 'in' from=expr ((inclusive='to' | 'upto') to=expr)? label=Label
      body=block
      'end' end=endLabel?
    ;

@ast::ForStmt {
	Local Slot[Local]
}

tryStmt
    : 'try' label=Label
      body=block
      'end' end=endLabel?
      ( hasOtherwise='otherwise' ('(' binding=(Name | 'from') ')')? handlerLabel=Label
        handler=block
        'end' handlerEnd=endLabel?
      )?
    ;

@ast::TryStmt {
	Local Slot[Local]
}

// A try expression's block handler: its error is named by the parenthesized name before the label, which parses as the expression of `value`.
handlerBlock
    : label=Label
      body=block
      'end' end=endLabel?
    ;

@ast::HandlerBlock {
	Local Slot[Local]
}

throwStmt
    : 'throw' value=expr
    ;

returnStmt
    : 'return' value=expr?
    ;

breakStmt
    : 'break' label=Label?
    ;

@ast::BreakStmt {
	Jump Slot[Jump]
}

continueStmt
    : 'continue' label=Label?
    ;

@ast::ContinueStmt {
	Jump Slot[Jump]
}

matchStmt
    : match=matchCore
    ;

whenStmt
    : 'when' cowns+=nameExpr (',' cowns+=nameExpr)* 'as' names+=Name (',' names+=Name)* label=Label
      body=block
      'end' end=endLabel?
    ;

@ast::WhenStmt {
	Info Slot[When]
}

// Match

matchCore
    : 'match' consumes='consumes'? subject=expr label=Label Newline
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
    : caseName=(Name | 'from') (hasBindings=^'(' (bindings+=(Name | 'from' | '_') (',' bindings+=(Name | 'from' | '_'))*)? ')')?
    | value=patternValue ((inclusive='to' | 'upto') high=patternValue)?
    ;

@ast::Pattern {
	Info Slot[Pattern]
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
    | byteStrLit
    | boolLit
    | structLitExpr
    | collectionLitExpr
    | selfExpr
    | nameExpr
    | panicExpr
    | matchExpr
    | '(' expr ')'
    | obj=expr '.' name=(Name | 'from')                              # MemberExpr
    | obj=expr ^'[' args+=expr (',' args+=expr)* ']'                 # IndexExpr
    | callee=expr ^'(' (args+=arg (',' args+=arg)*)? ')'               # CallExpr
    | head=expr 'from' value=expr                                    # FromExpr
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
    | 'try' e=expr ('otherwise' (ignore='ignore' | value=expr ({atHandlerBinding}? handler=handlerBlock)? | handler=handlerBlock | ret=returnStmt | brk=breakStmt | cont=continueStmt | thr=throwStmt))? # TryExpr
    ;

@ast::MemberExpr {
	Info Slot[Member]
}

@ast::CallExpr {
	Info Slot[Call]
}

@ast::BinaryExpr {
	Op string
	Arith Slot[Arith]
}

@ast::UnaryExpr {
	Arith Slot[Arith]
}

@ast::CastExpr {
	Arith Slot[Arith]
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
	Parts Word
}

// A byte string's text is one byte to each character, so it has no interpolation.
byteStrLit
    : parts=ByteString
    ;

@ast::ByteStrLit {
	Parts Word
}

boolLit
    : value='true'
    | 'false'
    ;

// 'export' is a name only as the start of a qualified name, where it is the root module.
nameExpr
    : name=(Name | 'from' | 'Self' | 'export')
    ;

@ast::NameExpr {
	Info Slot[Name]
}

selfExpr
    : 'self'
    ;

structLitExpr
    : typeName=(Name | 'Self') '{' (fields+=structField (',' fields+=structField)*)? '}'
    ;

@ast::StructLitExpr {
	Info Slot[StructLit]
}

structField
    : name=(Name | 'from') ':' value=expr
    ;

// An array literal [a, b] or a map literal [k: v, k: v]: the first element's following token says which. The empty [] parses
// so that the checker can say why it has no type.
collectionLitExpr
    : '[' (elems+=expr (':' values+=expr (',' elems+=expr ':' values+=expr)* | (',' elems+=expr)*))? ']'
    ;

/** an argument */
arg
    : (label=(Name | Keyword) ':')? value=expr
    ;

panicExpr
    : 'panic' ^'(' message=strLit ')'
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
    | '0x' [0-9A-Fa-f] [0-9A-Fa-f_]*
    | '0b' [01] [01_]*
    | '0o' [0-7] [0-7_]*
    ;

/** a string */
String : '"' (~["\\{}\n] | '\\' . | '{' ~[}\n]* '}')* '"' -> scan ;

/** a byte string */
ByteString : 'b"' (~["\\] | '\\' .)* '"' -> scan ;

/** a label */
Label : '\'' [A-Za-z_] [A-Za-z0-9_]* '\'' ;

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
LBracket : '[' -> noSpaceAfter ;
RBracket : ']' -> noSpaceBefore ;
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
