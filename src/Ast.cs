namespace MiniMaxon;

public abstract class Node {
  public string File = "";
  public int Line;
  public int Col;

  public CompileError Error(string code, string msg) => new(code, msg, File, Line, Col);
}

public sealed class TypeRef : Node {
  public string Name = "";
  public string? Capability;
}

public abstract class Decl : Node {
  public string Name = "";
  public string Visibility = "";
}

public sealed class Param : Node {
  public string Name = "";
  public bool Mutable;
  public TypeRef Type = null!;
  public Local? Sym;
}

public sealed class FuncDecl : Decl {
  public List<Param> Params = [];
  public TypeRef? Returns;
  public List<Stmt> Body = [];
  public bool IsStatic;
  public string? Receiver;
  public NominalTy? Owner;

  public List<Ty> ParamTypes = [];
  public Ty ReturnType = PrimTy.Void;
  public Local? SelfLocal;
  public List<Local> Locals = [];
  public string Symbol => Owner is null ? Name : $"{Owner.Name}.{Name}";
  public bool IsMethod => Owner is not null && !IsStatic;
}

public sealed class FieldDecl : Node {
  public string Name = "";
  public string Visibility = "";
  public bool IsVar;
  public TypeRef? Type;
  public Expr? Default;
}

public sealed class TypeDecl : Decl {
  public List<FieldDecl> Fields = [];
  public List<FuncDecl> Methods = [];
}

public sealed class EnumCaseDecl : Node {
  public string Name = "";
  public Int128? Raw;
}

public sealed class EnumDecl : Decl {
  public List<EnumCaseDecl> Cases = [];
  public List<FuncDecl> Methods = [];
}

public sealed class UnionCaseDecl : Node {
  public string Name = "";
  public List<Param> Fields = [];
}

public sealed class UnionDecl : Decl {
  public List<UnionCaseDecl> Cases = [];
  public List<FuncDecl> Methods = [];
}

public sealed class AliasDecl : Decl {
  public Int128 Min;
  public Int128 Max;
}

public sealed class ConstDecl : Decl {
  public Expr Value = null!;
  public Ty? Type;
}

public sealed class Program {
  public List<Decl> Decls = [];
}

public abstract class Stmt : Node;

public sealed class LetStmt : Stmt {
  public string Name = "";
  public bool IsVar;
  public Expr Init = null!;
  public Local? Sym;
}

public sealed class AssignStmt : Stmt {
  public Expr Target = null!;
  public Expr Value = null!;
}

public sealed class DiscardStmt : Stmt {
  public Expr Value = null!;
}

public sealed class ExprStmt : Stmt {
  public Expr E = null!;
}

public sealed class IfStmt : Stmt {
  public Expr Cond = null!;
  public string Label = "";
  public List<Stmt> Then = [];
  public List<Stmt>? Else;
}

public sealed class WhileStmt : Stmt {
  public Expr Cond = null!;
  public string Label = "";
  public List<Stmt> Body = [];
}

public sealed class ForStmt : Stmt {
  public string Var = "";
  public Expr From = null!;
  public Expr To = null!;
  public bool Inclusive;
  public string Label = "";
  public List<Stmt> Body = [];
  public Local? Sym;
}

public sealed class ReturnStmt : Stmt {
  public Expr? Value;
}

public sealed class BreakStmt : Stmt {
  public string? Label;
  public Node? Target;
}

public sealed class ContinueStmt : Stmt {
  public string? Label;
  public Node? Target;
}

public sealed class MatchStmt : Stmt {
  public MatchCore Match = null!;
}

public enum PatternKind { Literal, Range, Case }

public sealed class Pattern : Node {
  public PatternKind Kind;
  public Expr? Value;
  public Expr? High;
  public bool Inclusive;
  public string CaseName = "";
  public List<string>? Bindings;

  public int CaseIndex = -1;
  public List<Local?> BindingLocals = [];
}

public enum ArmKind { Then, Gives, Panic }

public sealed class MatchArm : Node {
  public List<Pattern> Patterns = [];
  public bool IsDefault;
  public ArmKind Kind;
  public Stmt? Action;
  public Expr? Value;
}

public sealed class MatchCore : Node {
  public Expr Subject = null!;
  public string Label = "";
  public List<MatchArm> Arms = [];
}

public abstract class Expr : Node {
  public Ty Type = PrimTy.Void;
}

public sealed class IntLit : Expr {
  public Int128 Value;
}

public sealed class BoolLit : Expr {
  public bool Value;
}

public sealed class StrLit : Expr {
  public List<object> Parts = [];
}

public enum NameKind { Unresolved, Local, Const, SelfField }

public sealed class NameExpr : Expr {
  public string Name = "";
  public NameKind Kind;
  public Local? Local;
  public ConstDecl? Const;
  public FieldInfo? Field;
}

public sealed class SelfExpr : Expr {
  public Local? Local;
}

public sealed class BinaryExpr : Expr {
  public string Op = "";
  public Expr L = null!;
  public Expr R = null!;
  public Ty OperandType = PrimTy.Void;
}

public sealed class UnaryExpr : Expr {
  public string Op = "";
  public Expr E = null!;
}

public sealed class CondExpr : Expr {
  public Expr Cond = null!;
  public Expr Then = null!;
  public Expr Else = null!;
}

public sealed class Arg : Node {
  public string? Label;
  public Expr Value = null!;
}

public enum CallKind { Unresolved, Function, Method, Static, UnionCtor, Print, PrintError }

public sealed class CallExpr : Expr {
  public Expr Callee = null!;
  public List<Arg> Args = [];

  public CallKind Kind;
  public FuncDecl? Target;
  public Expr? Receiver;
  public UnionTy? Union;
  public int CaseIndex = -1;
  public List<Expr> Ordered = [];
}

public enum MemberKind { Unresolved, Field, EnumCase, UnionCase, EnumName, EnumOrdinal, EnumRaw, UnionName, UnionOrdinal }

public sealed class MemberExpr : Expr {
  public Expr Obj = null!;
  public string Name = "";

  public MemberKind Kind;
  public FieldInfo? Field;
  public int CaseIndex = -1;
}

public sealed class StructLitExpr : Expr {
  public string TypeName = "";
  public List<(string Name, Expr Value, Node Site)> Fields = [];
  public RecordTy? Record;
  public List<Expr?> Ordered = [];
}

public sealed class CastExpr : Expr {
  public Expr E = null!;
  public TypeRef Target = null!;
}

public sealed class MatchExpr : Expr {
  public MatchCore Match = null!;
}

public sealed class PanicExpr : Expr {
  public Expr Message = null!;
}
