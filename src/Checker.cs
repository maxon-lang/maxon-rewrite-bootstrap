namespace MiniMaxon;

public sealed record ParamSpec(string Name, Ty Type, bool ByRef);

public sealed record PendingCall(CallExpr Call, string Callee, List<ParamSpec> Params);

public sealed class Checker(Project project, Module module, List<CompileError> errors) {
  enum BlockKind { Loop, Match }

  sealed record Block(string Label, BlockKind Kind, Node Node);

  public List<PendingCall> Pending { get; } = [];

  FuncDecl? fn;
  NominalTy? owner;
  bool allowCalls;
  List<Dictionary<string, Local>> scopes = [];
  List<Block> blocks = [];
  int nextSlot;

  IntTy CountTy => (IntTy)((AliasDecl)project.Stdlib.Names["Count"]).Ty!;
  IntTy FullRangeTy => (IntTy)((AliasDecl)project.Stdlib.Names["FullRangeInt"]).Ty!;

  public static Ty Shell(TypeDeclBase d) => d switch {
    AliasDecl a => new IntTy(a.Name, a.Min, a.Max),
    TypeDecl t => new RecordTy(t),
    EnumDecl e => new EnumTy(e),
    UnionDecl u => new UnionTy(u),
    _ => throw new InternalError($"no type for {d.GetType().Name}"),
  };

  public List<FuncDecl> Declare(Decl d) {
    var declared = new List<FuncDecl>();
    Guard(() => DeclareMembers(d));
    switch (d) {
      case FuncDecl f:
        Guard(() => AddFunction(f, null, declared));
        break;
      case TypeDeclBase { Ty: NominalTy nt }:
        AddMethods(Project.Methods(d), nt, declared);
        break;
    }
    if (d is TypeDecl td) {
      DeclareFieldDefaults(td);
    }
    return declared;
  }

  public void CheckBody(Decl d) {
    switch (d) {
      case FuncDecl f:
        Guard(() => CheckFunction(f));
        break;
      case TypeDecl td:
        CheckFieldDefaults(td);
        Guard(() => CheckCycle((NominalTy)td.Ty!));
        break;
      case UnionDecl ud:
        Guard(() => CheckCycle((NominalTy)ud.Ty!));
        break;
      default:
        throw new InternalError($"no body to check in {d.GetType().Name}");
    }
  }

  public void Verify(List<PendingCall> calls) {
    foreach (var p in calls) {
      Guard(() => p.Call.Ordered = BindArgs(p.Call, p.Callee, p.Params, argsChecked: true));
    }
  }

  void Guard(Action a) {
    try {
      a();
    } catch (CompileError e) {
      errors.Add(e);
    }
  }

  Ty Resolve(TypeRef tr, NominalTy? self) {
    if (tr.Capability is not null) {
      throw tr.Error(Err.Unsupported, $"the '{tr.Capability}' capability is not supported by minimaxon");
    }
    if (tr.Qualifier.Count == 0) {
      switch (tr.Name) {
        case "bool": return PrimTy.Bool;
        case "String": return PrimTy.String;
        case "Self": return self ?? throw tr.Error(Err.UnknownType, "'Self' names a type only inside one");
        case "int" or "float": throw tr.Error(Err.TypeMismatch, $"a bare '{tr.Name}' needs a ranged typealias");
      }
    }
    return tr.Target is TypeDeclBase { Ty: { } t } ? t : throw tr.Error(Err.UnknownType, $"unknown type '{tr.Name}'");
  }

  void DeclareMembers(Decl d) {
    switch (d) {
      case TypeDecl td: {
        var rt = (RecordTy)td.Ty!;
        foreach (var f in td.Fields) {
          if (rt.Field(f.Name) is not null) {
            throw f.Error(Err.DuplicateDefinition, $"field '{f.Name}' is declared twice");
          }
          var ft = f.Type is null ? PrimTy.Void : Resolve(f.Type, rt);
          rt.Fields.Add(new FieldInfo(f.Name, ft, f.IsVar, f.Default, rt.Fields.Count, f.Visibility is "export" or "public"));
        }
        break;
      }
      case EnumDecl ed: {
        var et = (EnumTy)ed.Ty!;
        Int128 next = 0;
        foreach (var c in ed.Cases) {
          if (et.Cases.Contains(c.Name)) {
            throw c.Error(Err.EnumDuplicateCase, $"case '{c.Name}' is declared twice");
          }
          var raw = c.Raw ?? next;
          if (et.Raw.Contains(raw)) {
            throw c.Error("E3031", $"raw value {raw} is used twice");
          }
          et.HasExplicitRaw |= c.Raw is not null;
          et.Cases.Add(c.Name);
          et.Raw.Add(raw);
          next = raw + 1;
        }
        break;
      }
      case UnionDecl ud: {
        var ut = (UnionTy)ud.Ty!;
        foreach (var c in ud.Cases) {
          if (ut.CaseIndex(c.Name) >= 0) {
            throw c.Error(Err.EnumDuplicateCase, $"case '{c.Name}' is declared twice");
          }
          var payload = c.Fields.Select(f => (f.Name, Resolve(f.Type, ut))).ToList();
          ut.Cases.Add(new UnionCase(c.Name, payload));
        }
        break;
      }
    }
  }

  void AddMethods(IEnumerable<FuncDecl> methods, NominalTy nt, List<FuncDecl> declared) {
    foreach (var m in methods) {
      Guard(() => {
        if (m.IsStatic && nt is not RecordTy) {
          throw m.Error("E2015", "a static function belongs in a 'type'");
        }
        if (!nt.Methods.TryAdd(m.Name, m)) {
          throw m.Error(Err.DuplicateDefinition, $"method '{m.Name}' is declared twice in '{nt.Name}'");
        }
        AddFunction(m, nt, declared);
      });
    }
  }

  void AddFunction(FuncDecl f, NominalTy? nt, List<FuncDecl> declared) {
    f.Owner = nt;
    var names = new HashSet<string>();
    foreach (var prm in f.Params) {
      if (prm.Name != "_" && !names.Add(prm.Name)) {
        throw prm.Error(Err.DuplicateDefinition, $"parameter '{prm.Name}' is declared twice");
      }
      f.ParamTypes.Add(Resolve(prm.Type, nt));
    }
    f.ReturnType = f.Returns is null ? PrimTy.Void : Resolve(f.Returns, nt);
    declared.Add(f);
  }

  public FuncDecl? CheckMain() {
    try {
      if (module.Names.GetValueOrDefault("main") is not FuncDecl main) {
        throw new CompileError(Err.NoMainFunction, "the project root has no 'main' function", module.Dir, 1, 1);
      }
      if (main.Params.Count > 0) {
        throw main.Error(Err.MainWrongReturnType, "'main' takes no parameters");
      }
      if (main.Returns?.Target is not AliasDecl { Name: "ExitCode" }) {
        throw main.Error(Err.MainWrongReturnType, "'main' must return ExitCode");
      }
      return main;
    } catch (CompileError e) {
      errors.Add(e);
      return null;
    }
  }

  void DeclareFieldDefaults(TypeDecl td) {
    var rt = (RecordTy)td.Ty!;
    foreach (var fi in rt.Fields.Where(f => f.Default is not null)) {
      Guard(() => fi.Type = DefaultType(fi, rt));
    }
  }

  Ty DefaultType(FieldInfo fi, RecordTy rt) {
    var head = fi.Default switch {
      MemberExpr { Obj: var o } => o,
      CallExpr { Callee: MemberExpr { Obj: var o } } => o,
      _ => null,
    };
    return fi.Default switch {
      CastExpr c => Resolve(c.Target, rt),
      BoolLit => PrimTy.Bool,
      StrLit s when s.Parts.All(p => p is string) => PrimTy.String,
      _ when head is NameExpr { Name: "Self" } => rt,
      _ when head is NameExpr { Target: TypeDeclBase { Ty: NominalTy nt } } => nt,
      _ => throw fi.Default!.Error(Err.ExpressionError,
        $"field '{fi.Name}' needs a type: write 'var {fi.Name} as <Type>', or a default that is a cast, a bool, a string, or a case or factory of a type"),
    };
  }

  void CheckFieldDefaults(TypeDecl td) {
    var rt = (RecordTy)td.Ty!;
    foreach (var fi in rt.Fields.Where(f => f.Default is not null)) {
      Guard(() => {
        EnterContext(null, rt, calls: true);
        CheckExpr(fi.Default!);
        Coerce(fi.Default!, fi.Type, $"the default of field '{fi.Name}'");
      });
    }
  }

  public void CheckConst(ConstDecl c) => Guard(() => {
    EnterContext(null, null, calls: false);
    var t = CheckExpr(c.Value);
    if (t is IntTy { IsAnonymous: true } || t == PrimTy.Void || t == PrimTy.Never) {
      throw c.Error("E3220", $"constant '{c.Name}' must be a value known before the program runs");
    }
    if (t == PrimTy.String && c.Value is StrLit s && s.Parts.Any(part => part is Expr)) {
      throw c.Error("E3220", "a constant string cannot interpolate");
    }
    c.Type = t;
  });

  void EnterContext(FuncDecl? f, NominalTy? nt, bool calls) {
    fn = f;
    owner = nt;
    allowCalls = calls;
    scopes = [[]];
    blocks = [];
    nextSlot = 0;
  }

  Local? LookupLocal(string name) {
    for (var i = scopes.Count - 1; i >= 0; i--) {
      if (scopes[i].TryGetValue(name, out var l)) {
        return l;
      }
    }
    return null;
  }

  Local Declare(string name, Ty type, bool isVar, Node site, bool isParam = false, bool byRef = false, int paramIndex = -1) {
    if (owner is RecordTy rt && fn is { IsMethod: true } && rt.Field(name) is not null) {
      throw site.Error(Err.DuplicateDefinition, $"'{name}' shadows the field '{name}' of '{rt.Name}'");
    }
    if (scopes[^1].ContainsKey(name)) {
      throw site.Error(Err.DuplicateDefinition, $"'{name}' is declared twice in this scope");
    }
    var local = new Local(name, type, isVar) { IsParam = isParam, ByRef = byRef, ParamIndex = paramIndex };
    if (!isParam) {
      local.Slot = nextSlot++;
    }
    scopes[^1][name] = local;
    fn!.Locals.Add(local);
    return local;
  }

  void CheckFunction(FuncDecl f) {
    EnterContext(f, f.Owner, calls: true);
    var index = 0;
    if (f.IsMethod) {
      f.SelfLocal = new Local("self", f.Owner!, false) { IsParam = true, IsSelf = true, ParamIndex = index++ };
      f.Locals.Add(f.SelfLocal);
    }
    for (var i = 0; i < f.Params.Count; i++) {
      var prm = f.Params[i];
      if (prm.Name != "_") {
        prm.Sym = Declare(prm.Name, f.ParamTypes[i], false, prm, isParam: true, byRef: prm.Mutable, paramIndex: index);
      }
      index++;
    }

    CheckBlock(f.Body);
    if (f.ReturnType != PrimTy.Void && !Terminates(f.Body)) {
      throw f.Error(Err.MissingReturn, $"'{f.Name}' must return a {f.ReturnType} on every path");
    }
  }

  void CheckBlock(List<Stmt> body) {
    scopes.Add([]);
    foreach (var s in body) {
      Guard(() => CheckStmt(s));
    }
    scopes.RemoveAt(scopes.Count - 1);
  }

  static bool Terminates(List<Stmt> body) => body.Count > 0 && Terminates(body[^1]);

  static bool Terminates(Stmt s) => s switch {
    ReturnStmt => true,
    ExprStmt { E: PanicExpr } => true,
    IfStmt i => i.Else is not null && Terminates(i.Then) && Terminates(i.Else),
    MatchStmt m => m.Match.Arms.All(a => a.Kind == ArmKind.Panic || (a.Action is not null && Terminates(a.Action))),
    _ => false,
  };

  bool Assignable(Ty src, Ty dst) {
    if (ReferenceEquals(src, dst) || src == PrimTy.Never) {
      return true;
    }
    if (dst is IntTy di) {
      return src switch {
        ConstIntTy c => di.Contains(c.Value),
        IntTy si => (si.IsAnonymous || di.IsAnonymous) && di.Contains(si),
        _ => false,
      };
    }
    return false;
  }

  void Coerce(Expr e, Ty target, string what) {
    if (Assignable(e.Type, target)) {
      return;
    }
    if (e.Type is ConstIntTy c && target is IntTy ti) {
      throw e.Error(Err.TypeMismatch, $"{what}: {c.Value} does not fit {ti.Name} ({ti.Min} to {ti.Max})");
    }
    if (e.Type is IntTy { IsAnonymous: true } && target is IntTy ta) {
      throw e.Error(Err.TypeMismatch, $"{what}: a value of {e.Type} may not fit {ta.Name}; cast it with 'as {ta.Name}'");
    }
    throw e.Error(Err.TypeMismatch, $"{what}: expected {target}, found {e.Type}");
  }

  static bool IsInt(Ty t) => t is IntTy or ConstIntTy;

  static (Int128 Min, Int128 Max) RangeOf(Ty t) => t switch {
    ConstIntTy c => (c.Value, c.Value),
    IntTy i => (i.Min, i.Max),
    _ => throw new InvalidOperationException($"{t} is not an integer"),
  };

  static void RequireSigned64(Node site, Ty t) {
    if (t is ConstIntTy c && c.Value > IntTy.I64Max) {
      throw site.Error(Err.IntegerLiteralRange,
        $"Integer literal '{c.Value}' is above i64.max, so it needs an alias whose range admits it: write '{c.Value} as <alias>'");
    }
  }

  IntTy UnifyInts(Node site, Ty l, Ty r) {
    var la = l is IntTy { IsAnonymous: false } x ? x : null;
    var ra = r is IntTy { IsAnonymous: false } y ? y : null;
    if (la is not null && ra is not null && !ReferenceEquals(la, ra)) {
      throw site.Error(Err.TypeMismatch, $"the operands are {la} and {ra}; cast one so both are the same type");
    }
    var alias = la ?? ra;
    if (alias is null) {
      RequireSigned64(site, l);
      RequireSigned64(site, r);
      return IntTy.Anonymous64;
    }
    var other = la is null ? l : r;
    if (!Assignable(other, alias)) {
      throw site.Error(Err.TypeMismatch, $"an operand of {other} does not fit {alias}; cast it with 'as {alias}'");
    }
    return alias;
  }

  Ty Join(Node site, Ty a, Ty b) {
    if (a == PrimTy.Never) {
      return b;
    }
    if (b == PrimTy.Never || ReferenceEquals(a, b)) {
      return a;
    }
    if (IsInt(a) && IsInt(b)) {
      if (a is IntTy { IsAnonymous: false } || b is IntTy { IsAnonymous: false }) {
        return UnifyInts(site, a, b);
      }
      var (amin, amax) = RangeOf(a);
      var (bmin, bmax) = RangeOf(b);
      return new IntTy(null, Int128.Min(amin, bmin), Int128.Max(amax, bmax));
    }
    throw site.Error(Err.TypeMismatch, $"the branches produce {a} and {b}");
  }

  Ty CheckExpr(Expr e) {
    e.Type = e switch {
      IntLit i => new ConstIntTy(i.Value),
      BoolLit => PrimTy.Bool,
      StrLit s => CheckStr(s),
      NameExpr n => CheckName(n),
      SelfExpr s => CheckSelf(s),
      BinaryExpr b => CheckBinary(b),
      UnaryExpr u => CheckUnary(u),
      CondExpr c => CheckCond(c),
      CallExpr c => CheckCall(c),
      MemberExpr m => CheckMember(m),
      StructLitExpr s => CheckStructLit(s),
      CastExpr c => CheckCast(c),
      MatchExpr m => CheckMatch(m.Match, isExpr: true),
      PanicExpr p => CheckPanic(p),
      _ => throw new InternalError($"unhandled expression {e.GetType().Name}"),
    };
    return e.Type;
  }

  Ty CheckStr(StrLit s) {
    foreach (var part in s.Parts.OfType<Expr>()) {
      var t = CheckExpr(part);
      RequireSigned64(part, t);
      if (!(IsInt(t) || t == PrimTy.String || t == PrimTy.Bool || t is EnumTy or UnionTy)) {
        throw part.Error(Err.TypeMismatch, $"a value of {t} cannot be interpolated");
      }
    }
    return PrimTy.String;
  }

  Ty CheckName(NameExpr n) {
    if (LookupLocal(n.Name) is { } local) {
      n.Kind = NameKind.Local;
      n.Local = local;
      return local.Type;
    }
    if (owner is RecordTy rt && fn is { IsMethod: true } && rt.Field(n.Name) is { } field) {
      n.Kind = NameKind.SelfField;
      n.Field = field;
      return field.Type;
    }
    switch (n.Target) {
      case ConstDecl c:
        n.Kind = NameKind.Const;
        n.Const = c;
        return c.Type ?? throw n.Error(Err.ExpressionError, $"constant '{c.Name}' has no value");
      case FuncDecl:
        throw n.Error(Err.Unsupported, "a function value is not supported by minimaxon; call the function");
      case TypeDeclBase:
        throw n.Error(Err.ExpressionError, $"'{n.Name}' is a type, not a value");
    }
    if (n.Name == "Self") {
      throw n.Error(Err.ExpressionError, "'Self' is a type, not a value");
    }
    throw n.Error(Err.ExpressionError, $"undefined variable '{n.Name}'");
  }

  Ty CheckSelf(SelfExpr s) {
    if (fn is not { IsMethod: true }) {
      throw s.Error(Err.UnexpectedToken, "'self' is used only inside an instance method");
    }
    s.Local = fn.SelfLocal;
    return owner!;
  }

  NominalTy? TypeNamed(Expr e) {
    if (e is not NameExpr n) {
      return null;
    }
    if (n.Name == "Self") {
      return owner ?? throw n.Error(Err.UnknownType, "'Self' names a type only inside one");
    }
    if (LookupLocal(n.Name) is not null) {
      return null;
    }
    if (owner is RecordTy rt && fn is { IsMethod: true } && rt.Field(n.Name) is not null) {
      return null;
    }
    return n.Target is TypeDeclBase { Ty: NominalTy nt } ? nt : null;
  }

  static Int128 Fold(Node site, string op, Int128 a, Int128 b) {
    Int128 r;
    switch (op) {
      case "+": r = a + b; break;
      case "-": r = a - b; break;
      case "*": r = a * b; break;
      case "/" or "mod":
        if (b == 0) {
          throw site.Error(Err.DivisionByZero, "division by a constant zero");
        }
        r = op == "/" ? a / b : a % b;
        break;
      case "and": r = a & b; break;
      case "or": r = a | b; break;
      case "xor": r = a ^ b; break;
      case "shl" or "shr":
        if (b < 0) {
          throw site.Error("E2054", "a shift count cannot be negative");
        }
        r = op == "shl" ? (b >= 64 ? 0 : (Int128)(ulong)((ulong)(long)a << (int)b)) : (b >= 64 ? (a < 0 ? -1 : 0) : a >> (int)b);
        break;
      default: throw site.Error(Err.Unsupported, $"cannot fold '{op}'");
    }
    if (r < IntTy.I64Min || r > IntTy.U64Max) {
      throw site.Error(Err.TypeMismatch, "the constant expression overflows 64 bits");
    }
    return r;
  }

  static readonly HashSet<string> Arithmetic = ["+", "-", "*", "/", "mod", "shl", "shr"];
  static readonly HashSet<string> Comparisons = ["==", "!=", "<", "<=", ">", ">="];
  static readonly HashSet<string> Logical = ["and", "or", "xor"];

  Ty CheckBinary(BinaryExpr b) {
    var l = CheckExpr(b.L);
    var r = CheckExpr(b.R);

    if (Arithmetic.Contains(b.Op) || (Logical.Contains(b.Op) && IsInt(l))) {
      if (!IsInt(l) || !IsInt(r)) {
        throw b.Error(Err.TypeMismatch, $"'{b.Op}' needs integers, found {l} and {r}");
      }
      if (l is ConstIntTy lc && r is ConstIntTy rc) {
        b.OperandType = IntTy.Anonymous64;
        return new ConstIntTy(Fold(b, b.Op, lc.Value, rc.Value));
      }
      if (b.Op is "/" or "mod" && r is ConstIntTy { Value: var z } && z == 0) {
        throw b.Error(Err.DivisionByZero, "division by a constant zero");
      }
      if (b.Op is "shl" or "shr") {
        var lt = l is ConstIntTy ? IntTy.Anonymous64 : (IntTy)l;
        b.OperandType = lt;
        return lt;
      }
      var t = UnifyInts(b, l, r);
      b.OperandType = t;
      return t;
    }

    if (Logical.Contains(b.Op)) {
      if (l != PrimTy.Bool || r != PrimTy.Bool) {
        throw b.Error(Err.TypeMismatch, $"'{b.Op}' needs two bools or two integers, found {l} and {r}");
      }
      return PrimTy.Bool;
    }

    if (Comparisons.Contains(b.Op)) {
      if (IsInt(l) && IsInt(r)) {
        b.OperandType = UnifyInts(b, l, r);
        return PrimTy.Bool;
      }
      var equality = b.Op is "==" or "!=";
      if (l is UnionTy || r is UnionTy) {
        throw b.Error(Err.EnumNotComparable, "a union cannot be compared; inspect it with 'match'");
      }
      if (equality && ReferenceEquals(l, r) && (l == PrimTy.Bool || l == PrimTy.String || l is EnumTy)) {
        b.OperandType = l;
        return PrimTy.Bool;
      }
      throw b.Error(Err.TypeMismatch, $"'{b.Op}' cannot compare {l} and {r}");
    }

    throw b.Error(Err.Unsupported, $"unknown operator '{b.Op}'");
  }

  Ty CheckUnary(UnaryExpr u) {
    var t = CheckExpr(u.E);
    if (u.Op == "not") {
      return t switch {
        _ when t == PrimTy.Bool => PrimTy.Bool,
        ConstIntTy c => new ConstIntTy(~(long)c.Value),
        IntTy it => it,
        _ => throw u.Error(Err.TypeMismatch, $"'not' needs a bool or an integer, found {t}"),
      };
    }
    return t switch {
      ConstIntTy c => new ConstIntTy(-c.Value),
      IntTy { Unsigned: true } => throw u.Error(Err.UnsignedNegation, $"cannot negate a value of the unsigned {t}"),
      IntTy { IsAnonymous: true } => IntTy.Anonymous64,
      IntTy it => it,
      _ => throw u.Error(Err.TypeMismatch, $"'-' needs an integer, found {t}"),
    };
  }

  Ty CheckCond(CondExpr c) {
    if (CheckExpr(c.Cond) != PrimTy.Bool) {
      throw c.Cond.Error(Err.TypeMismatch, $"a condition must be a bool, found {c.Cond.Type}");
    }
    return Join(c, CheckExpr(c.Then), CheckExpr(c.Else));
  }

  Ty CheckPanic(PanicExpr p) {
    if (CheckExpr(p.Message) != PrimTy.String) {
      throw p.Message.Error(Err.TypeMismatch, $"panic takes a String, found {p.Message.Type}");
    }
    return PrimTy.Never;
  }

  List<Expr> BindArgs(CallExpr call, string callee, List<ParamSpec> ps, bool argsChecked = false) {
    if (call.Args.Count != ps.Count) {
      throw call.Error(Err.CallArgCountMismatch, $"'{callee}' takes {ps.Count} argument(s), given {call.Args.Count}");
    }
    var ordered = new Expr?[ps.Count];
    for (var i = 0; i < call.Args.Count; i++) {
      var arg = call.Args[i];
      int slot;
      if (i == 0) {
        if (arg.Label is not null) {
          throw arg.Error(Err.CallFirstArgNamed, "the first argument is positional: drop its label");
        }
        slot = 0;
      } else {
        if (arg.Label is null) {
          throw arg.Error(Err.CallArgMissingLabel, $"argument {i + 1} of '{callee}' needs its label");
        }
        slot = ps.FindIndex(q => q.Name == arg.Label);
        if (slot <= 0) {
          throw arg.Error(Err.CallUnknownArgLabel, $"'{callee}' has no parameter '{arg.Label}' after its first");
        }
        if (ordered[slot] is not null) {
          throw arg.Error(Err.CallDuplicateArg, $"'{arg.Label}' is given twice");
        }
      }
      ordered[slot] = arg.Value;
    }

    for (var i = 0; i < ps.Count; i++) {
      var value = ordered[i]!;
      if (!argsChecked) {
        CheckExpr(value);
      }
      if (ps[i].ByRef) {
        CheckMutableArg(value, ps[i]);
      } else {
        Coerce(value, ps[i].Type, $"argument '{ps[i].Name}' of '{callee}'");
      }
    }
    return [.. ordered.Select(o => o!)];
  }

  void CheckMutableArg(Expr value, ParamSpec ps) {
    var writable = value switch {
      NameExpr { Kind: NameKind.Local, Local: var l } => l!.IsVar || l.ByRef,
      NameExpr { Kind: NameKind.SelfField, Field: var f } => f!.IsVar,
      MemberExpr { Kind: MemberKind.Field, Field: var f } => f!.IsVar,
      _ => false,
    };
    if (!writable) {
      throw value.Error(Err.AssignToImmutable, $"the mutable parameter '{ps.Name}' needs a var or a var field");
    }
    if (!ReferenceEquals(value.Type, ps.Type)) {
      throw value.Error(Err.TypeMismatch, $"the mutable parameter '{ps.Name}' is {ps.Type}; the variable is {value.Type}");
    }
  }

  static List<ParamSpec> SpecsOf(FuncDecl f) =>
    [.. f.Params.Select((prm, i) => new ParamSpec(prm.Name, f.ParamTypes[i], prm.Mutable))];

  Ty CallFunction(CallExpr c, CallKind kind, FuncDecl target, Expr? receiver) {
    if (target.Module != module && !target.IsExported) {
      throw c.Error("E3008", $"'{target.Symbol}' is not exported from module {target.Module.Display}");
    }
    c.Kind = kind;
    c.Target = target;
    c.Receiver = receiver;
    if (target.Module == module) {
      c.Ordered = BindArgs(c, target.Symbol, SpecsOf(target));
    } else {
      foreach (var a in c.Args) {
        CheckExpr(a.Value);
      }
      Pending.Add(new PendingCall(c, target.Symbol, SpecsOf(target)));
    }
    return target.ReturnType;
  }

  Ty CheckCall(CallExpr c) {
    if (!allowCalls) {
      throw c.Error("E3220", "a constant calls nothing");
    }

    if (c.Callee is NameExpr n && LookupLocal(n.Name) is null) {
      if (n.Target is null && n.Name is "print" or "printError") {
        c.Kind = n.Name == "print" ? CallKind.Print : CallKind.PrintError;
        c.Ordered = BindArgs(c, n.Name, [new ParamSpec("text", PrimTy.String, false)]);
        return PrimTy.Void;
      }
      if (n.Target is FuncDecl f) {
        return CallFunction(c, CallKind.Function, f, null);
      }
      throw c.Error(Err.UnknownFunction, $"unknown function '{n.Name}'");
    }

    if (c.Callee is MemberExpr m) {
      if (TypeNamed(m.Obj) is { } nt) {
        if (nt is UnionTy ut && ut.CaseIndex(m.Name) is var ci and >= 0) {
          var uc = ut.Cases[ci];
          if (uc.Payload.Count == 0) {
            throw c.Error(Err.EnumWrongBindingCount, $"case '{uc.Name}' carries no payload: write '{ut.Name}.{uc.Name}'");
          }
          c.Kind = CallKind.UnionCtor;
          c.Union = ut;
          c.CaseIndex = ci;
          c.Ordered = BindArgs(c, $"{ut.Name}.{uc.Name}", [.. uc.Payload.Select(p => new ParamSpec(p.Name, p.Type, false))]);
          return ut;
        }
        if (nt.Methods.TryGetValue(m.Name, out var sm) && sm.IsStatic) {
          return CallFunction(c, CallKind.Static, sm, null);
        }
        throw c.Error(Err.UnknownFunction, $"'{nt.Name}' has no static function or case '{m.Name}'");
      }

      var rt = CheckExpr(m.Obj);
      if (rt is NominalTy recv && recv.Methods.TryGetValue(m.Name, out var method) && !method.IsStatic) {
        return CallFunction(c, CallKind.Method, method, m.Obj);
      }
      throw c.Error(Err.UnknownFunction, $"{rt} has no method '{m.Name}'");
    }

    throw c.Error(Err.Unsupported, "only a named function or method can be called");
  }

  Ty CheckMember(MemberExpr m) {
    if (TypeNamed(m.Obj) is { } nt) {
      switch (nt) {
        case EnumTy et: {
          m.CaseIndex = et.Cases.IndexOf(m.Name);
          if (m.CaseIndex < 0) {
            throw m.Error(Err.EnumUnknownCase, $"'{et.Name}' has no case '{m.Name}'");
          }
          m.Kind = MemberKind.EnumCase;
          return et;
        }
        case UnionTy ut: {
          m.CaseIndex = ut.CaseIndex(m.Name);
          if (m.CaseIndex < 0) {
            throw m.Error(Err.EnumUnknownCase, $"'{ut.Name}' has no case '{m.Name}'");
          }
          if (ut.Cases[m.CaseIndex].Payload.Count > 0) {
            throw m.Error(Err.EnumWrongBindingCount, $"case '{m.Name}' carries a payload: construct it with arguments");
          }
          m.Kind = MemberKind.UnionCase;
          return ut;
        }
        default:
          throw m.Error(Err.UnknownField, $"'{nt.Name}.{m.Name}' is not a value");
      }
    }

    var t = CheckExpr(m.Obj);
    switch (t) {
      case RecordTy rt when rt.Field(m.Name) is { } f:
        if (rt.Decl.Module != module && !f.Exported) {
          throw m.Error("E3014", $"field '{rt.Name}.{f.Name}' is not exported");
        }
        m.Kind = MemberKind.Field;
        m.Field = f;
        return f.Type;
      case EnumTy et when m.Name is "name" or "ordinal" or "rawValue":
        m.Kind = m.Name switch { "name" => MemberKind.EnumName, "ordinal" => MemberKind.EnumOrdinal, _ => MemberKind.EnumRaw };
        return m.Name == "name" ? PrimTy.String : m.Name == "ordinal" ? CountTy : (et.Raw.Any(r => r < 0) ? FullRangeTy : CountTy);
      case UnionTy when m.Name is "name" or "ordinal":
        m.Kind = m.Name == "name" ? MemberKind.UnionName : MemberKind.UnionOrdinal;
        return m.Name == "name" ? PrimTy.String : CountTy;
      case NominalTy nom when nom.Methods.ContainsKey(m.Name):
        throw m.Error(Err.Unsupported, "a method value is not supported by minimaxon; call the method");
      default:
        throw m.Error(Err.UnknownField, $"{t} has no field '{m.Name}'");
    }
  }

  Ty CheckStructLit(StructLitExpr s) {
    var target = s.TypeName == "Self" ? owner : (s.Target as TypeDeclBase)?.Ty;
    if (target is not RecordTy rt) {
      throw s.Error(Err.UnknownType, $"'{s.TypeName}' is not a type with fields");
    }
    if (!ReferenceEquals(owner, rt) || fn is null) {
      throw s.Error(Err.ConstructorRestriction, $"a '{rt.Name}' literal is written only inside the methods of '{rt.Name}'; call a factory");
    }
    s.Record = rt;
    s.Ordered = [.. rt.Fields.Select(_ => (Expr?)null)];
    foreach (var (name, value, site) in s.Fields) {
      var f = rt.Field(name) ?? throw site.Error(Err.UnknownField, $"'{rt.Name}' has no field '{name}'");
      if (s.Ordered[f.Index] is not null) {
        throw site.Error(Err.DuplicateDefinition, $"field '{name}' is given twice");
      }
      CheckExpr(value);
      Coerce(value, f.Type, $"field '{name}'");
      s.Ordered[f.Index] = value;
    }
    foreach (var f in rt.Fields) {
      if (s.Ordered[f.Index] is null && f.Default is null) {
        throw s.Error(Err.FieldNotInitialized, $"field '{f.Name}' of '{rt.Name}' is not initialized");
      }
    }
    return rt;
  }

  Ty CheckCast(CastExpr c) {
    var target = Resolve(c.Target, owner);
    if (target is not IntTy ti) {
      throw c.Error(Err.Unsupported, $"a cast to {target} is not supported by minimaxon");
    }
    var src = CheckExpr(c.E);
    switch (src) {
      case ConstIntTy k when !ti.Contains(k.Value):
        throw c.Error(Err.TypeMismatch, $"{k.Value} does not fit {ti.Name} ({ti.Min} to {ti.Max})");
      case ConstIntTy or IntTy:
        break;
      default:
        throw c.Error(Err.TypeMismatch, $"cannot cast {src} to {ti.Name}");
    }
    return ti;
  }

  void CheckStmt(Stmt s) {
    switch (s) {
      case LetStmt l: CheckLet(l); break;
      case AssignStmt a: CheckAssign(a); break;
      case DiscardStmt d:
        CheckExpr(d.Value);
        break;
      case ExprStmt e: CheckExpr(e.E); break;
      case IfStmt i:
        RequireBool(i.Cond);
        CheckBlock(i.Then);
        if (i.Else is not null) {
          CheckBlock(i.Else);
        }
        break;
      case WhileStmt w:
        RequireBool(w.Cond);
        blocks.Add(new Block(w.Label, BlockKind.Loop, w));
        CheckBlock(w.Body);
        blocks.RemoveAt(blocks.Count - 1);
        break;
      case ForStmt f: CheckFor(f); break;
      case MatchStmt m: CheckMatch(m.Match, isExpr: false); break;
      case ReturnStmt r: CheckReturn(r); break;
      case BreakStmt b: b.Target = ResolveJump(b, b.Label, isBreak: true); break;
      case ContinueStmt c: c.Target = ResolveJump(c, c.Label, isBreak: false); break;
      default: throw new InternalError($"unhandled statement {s.GetType().Name}");
    }
  }

  void RequireBool(Expr e) {
    if (CheckExpr(e) != PrimTy.Bool) {
      throw e.Error(Err.TypeMismatch, $"a condition must be a bool, found {e.Type}");
    }
  }

  void CheckLet(LetStmt l) {
    if (l.Name == "_") {
      throw l.Error(Err.UnexpectedToken, "write '_ = call()' to discard a result");
    }
    var t = CheckExpr(l.Init);
    RequireSigned64(l.Init, t);
    if (t == PrimTy.Void || t == PrimTy.Never) {
      throw l.Init.Error(Err.TypeMismatch, $"'{l.Name}' is bound to an expression with no value");
    }
    if (l.IsVar && (t is ConstIntTy || t is IntTy { IsAnonymous: true })) {
      throw l.Init.Error(Err.ExpressionError, $"a var names its alias: write 'var {l.Name} = <value> as <Alias>'");
    }
    l.Sym = Declare(l.Name, t, l.IsVar, l);
  }

  void CheckAssign(AssignStmt a) {
    Ty target;
    switch (a.Target) {
      case NameExpr n:
        target = CheckName(n);
        switch (n.Kind) {
          case NameKind.Local when n.Local!.IsParam && !n.Local.ByRef:
            throw a.Error(Err.AssignToImmutable, $"the parameter '{n.Name}' is a let; mark it 'mutable' to reassign it");
          case NameKind.Local when !n.Local!.IsVar && !n.Local.ByRef:
            throw a.Error(Err.AssignToImmutable, $"'{n.Name}' is a let and cannot be reassigned");
          case NameKind.SelfField when !n.Field!.IsVar:
          case NameKind.Const:
            throw a.Error(Err.AssignToImmutable, $"'{n.Name}' is a let and cannot be reassigned");
        }
        break;
      case MemberExpr m:
        target = CheckMember(m);
        m.Type = target;
        if (m.Kind != MemberKind.Field) {
          throw a.Error(Err.AssignToImmutable, $"'{m.Name}' cannot be assigned");
        }
        if (!m.Field!.IsVar) {
          throw a.Error(Err.AssignToImmutable, $"field '{m.Name}' is a let and cannot be reassigned");
        }
        break;
      default:
        throw a.Error(Err.UnexpectedToken, "only a variable or a field can be assigned");
    }
    CheckExpr(a.Value);
    Coerce(a.Value, target, "assignment");
  }

  void CheckFor(ForStmt f) {
    var from = CheckExpr(f.From);
    var to = CheckExpr(f.To);
    if (!IsInt(from) || !IsInt(to)) {
      throw f.Error(Err.TypeMismatch, $"a range needs integer bounds, found {from} and {to}");
    }
    Ty varType;
    if (from is IntTy { IsAnonymous: false } || to is IntTy { IsAnonymous: false }) {
      varType = UnifyInts(f, from, to);
    } else {
      RequireSigned64(f.From, from);
      RequireSigned64(f.To, to);
      var lo = RangeOf(from).Min;
      var hi = RangeOf(to).Max - (f.Inclusive ? 0 : 1);
      varType = new IntTy(null, lo, Int128.Max(lo, hi));
    }

    blocks.Add(new Block(f.Label, BlockKind.Loop, f));
    scopes.Add([]);
    f.Sym = Declare(f.Var, varType, false, f);
    foreach (var st in f.Body) {
      Guard(() => CheckStmt(st));
    }
    scopes.RemoveAt(scopes.Count - 1);
    blocks.RemoveAt(blocks.Count - 1);
  }

  void CheckReturn(ReturnStmt r) {
    if (fn!.ReturnType == PrimTy.Void) {
      if (r.Value is not null) {
        throw r.Error(Err.TypeMismatch, $"'{fn.Name}' returns nothing");
      }
      return;
    }
    if (r.Value is null) {
      throw r.Error(Err.MissingReturn, $"'{fn.Name}' must return a {fn.ReturnType}");
    }
    CheckExpr(r.Value);
    Coerce(r.Value, fn.ReturnType, "return value");
  }

  Node ResolveJump(Stmt s, string? label, bool isBreak) {
    var word = isBreak ? "break" : "continue";
    if (label is null) {
      for (var i = blocks.Count - 1; i >= 0; i--) {
        if (isBreak || blocks[i].Kind == BlockKind.Loop) {
          return blocks[i].Node;
        }
      }
      throw s.Error(Err.UnexpectedToken, $"'{word}' outside a loop");
    }

    for (var i = blocks.Count - 1; i >= 0; i--) {
      if (blocks[i].Label != label) {
        continue;
      }
      if (!isBreak && blocks[i].Kind != BlockKind.Loop) {
        throw s.Error(Err.UnexpectedToken, $"'continue' names '{label}', which is not a loop");
      }
      return blocks[i].Node;
    }
    throw s.Error(Err.UnexpectedToken, $"no enclosing loop or match is labelled '{label}'");
  }

  Ty CheckMatch(MatchCore m, bool isExpr) {
    var st = CheckExpr(m.Subject);
    RequireSigned64(m.Subject, st);
    var isCases = st is EnumTy or UnionTy;
    if (!(IsInt(st) || st == PrimTy.String || st == PrimTy.Bool || isCases)) {
      throw m.Subject.Error(Err.TypeMismatch, $"cannot match on a value of {st}");
    }

    blocks.Add(new Block(m.Label, BlockKind.Match, m));
    Ty? result = null;
    var hasDefault = false;
    var seen = new HashSet<int>();
    var seenBools = new HashSet<bool>();

    foreach (var arm in m.Arms) {
      Guard(() => {
        if (arm.IsDefault) {
          if (isCases && arm.Kind != ArmKind.Panic) {
            throw arm.Error(Err.MatchDefaultEnumMustThrow, "on an enum or union, 'default' is 'default panic(...)'");
          }
          hasDefault = true;
        }

        scopes.Add([]);
        try {
          CheckArm(arm, st, isExpr, seen, seenBools, ref result);
        } finally {
          scopes.RemoveAt(scopes.Count - 1);
        }
      });
    }
    blocks.RemoveAt(blocks.Count - 1);
    CheckExhaustive(m, st, hasDefault, seen, seenBools);
    return isExpr ? result ?? PrimTy.Never : PrimTy.Void;
  }

  void CheckArm(MatchArm arm, Ty st, bool isExpr, HashSet<int> seen, HashSet<bool> seenBools, ref Ty? result) {
    foreach (var pat in arm.Patterns) {
      CheckPattern(pat, st, arm.Patterns.Count, seen, seenBools);
    }

    switch (arm.Kind) {
      case ArmKind.Then:
        if (isExpr) {
          throw arm.Error(Err.UnexpectedToken, "an arm of a match expression 'gives' a value");
        }
        CheckStmt(arm.Action!);
        break;
      case ArmKind.Gives:
        if (!isExpr) {
          throw arm.Error(Err.UnexpectedToken, "an arm of a match statement uses 'then'");
        }
        var t = CheckExpr(arm.Value!);
        result = result is null ? t : Join(arm.Value!, result, t);
        break;
      case ArmKind.Panic:
        CheckExpr(arm.Value!);
        break;
    }
  }

  void CheckExhaustive(MatchCore m, Ty st, bool hasDefault, HashSet<int> seen, HashSet<bool> seenBools) {
    if (!hasDefault) {
      if (st is EnumTy et && seen.Count < et.Cases.Count) {
        var missing = et.Cases.Where((_, i) => !seen.Contains(i));
        throw m.Error(Err.MatchNotExhaustive, $"the match is not exhaustive: missing {string.Join(", ", missing)}");
      }
      if (st is UnionTy ut && seen.Count < ut.Cases.Count) {
        var missing = ut.Cases.Where((_, i) => !seen.Contains(i)).Select(c => c.Name);
        throw m.Error(Err.MatchNotExhaustive, $"the match is not exhaustive: missing {string.Join(", ", missing)}");
      }
      if (st == PrimTy.Bool && seenBools.Count < 2) {
        throw m.Error(Err.MatchNotExhaustive, "the match is not exhaustive: it needs both true and false, or a default");
      }
      if (IsInt(st) || st == PrimTy.String) {
        throw m.Error(Err.MatchNotExhaustive, $"a match on {st} needs a 'default' arm");
      }
    }
  }

  void CheckPattern(Pattern pat, Ty st, int altCount, HashSet<int> seen, HashSet<bool> seenBools) {
    switch (pat.Kind) {
      case PatternKind.Case: {
        if (st is EnumTy et) {
          pat.CaseIndex = et.Cases.IndexOf(pat.CaseName);
          if (pat.CaseIndex < 0) {
            throw pat.Error(Err.EnumUnknownCase, $"'{et.Name}' has no case '{pat.CaseName}'");
          }
          if (pat.Bindings is not null) {
            throw pat.Error(Err.EnumWrongBindingCount, $"an enum case carries no payload to bind");
          }
        } else if (st is UnionTy ut) {
          pat.CaseIndex = ut.CaseIndex(pat.CaseName);
          if (pat.CaseIndex < 0) {
            throw pat.Error(Err.EnumUnknownCase, $"'{ut.Name}' has no case '{pat.CaseName}'");
          }
          var payload = ut.Cases[pat.CaseIndex].Payload;
          if (pat.Bindings is not null) {
            if (pat.Bindings.Count != payload.Count) {
              throw pat.Error(Err.EnumWrongBindingCount, $"case '{pat.CaseName}' carries {payload.Count} value(s), {pat.Bindings.Count} bound");
            }
            if (altCount > 1) {
              throw pat.Error(Err.EnumWrongBindingCount, "an arm that binds a payload covers exactly one case");
            }
            for (var i = 0; i < payload.Count; i++) {
              pat.BindingLocals.Add(pat.Bindings[i] == "_" ? null : Declare(pat.Bindings[i], payload[i].Type, false, pat));
            }
          }
        } else {
          throw pat.Error(Err.TypeMismatch, $"a case pattern needs an enum or union subject, found {st}");
        }
        if (!seen.Add(pat.CaseIndex)) {
          throw pat.Error(Err.DuplicateDefinition, $"case '{pat.CaseName}' is matched twice");
        }
        break;
      }
      case PatternKind.Literal: {
        var t = CheckExpr(pat.Value!);
        if (st == PrimTy.Bool && pat.Value is BoolLit bl) {
          if (!seenBools.Add(bl.Value)) {
            throw pat.Error(Err.DuplicateDefinition, $"'{bl.Value}' is matched twice");
          }
          break;
        }
        if (st == PrimTy.String && pat.Value is StrLit sl && sl.Parts.All(part => part is string)) {
          break;
        }
        if (IsInt(st) && t is ConstIntTy) {
          Coerce(pat.Value!, st is ConstIntTy ? IntTy.Anonymous64 : st, "pattern");
          break;
        }
        throw pat.Error(Err.TypeMismatch, $"a pattern of {t} cannot match {st}");
      }
      case PatternKind.Range: {
        var lo = CheckExpr(pat.Value!);
        var hi = CheckExpr(pat.High!);
        if (!IsInt(st) || lo is not ConstIntTy l || hi is not ConstIntTy h) {
          throw pat.Error(Err.TypeMismatch, "a range pattern needs integer bounds and an integer subject");
        }
        var top = pat.Inclusive ? h.Value : h.Value - 1;
        if (l.Value > top) {
          throw pat.Error(Err.TypeMismatch, "the range pattern is empty");
        }
        break;
      }
    }
  }

  static void CheckCycle(NominalTy t) {
    var path = new List<string>();
    if (Reaches(t, t, path, [])) {
      var site = t is RecordTy r ? (Node)r.Decl : ((UnionTy)t).Decl;
      throw site.Error(Err.TypeCycle, $"type '{t.Name}' contains a reference cycle (via {string.Join(" -> ", path)})");
    }
  }

  static IEnumerable<(string Edge, Ty Type)> Edges(Ty t) => t switch {
    RecordTy r => r.Fields.Select(f => ($"{r.Name}.{f.Name}", f.Type)),
    UnionTy u => u.Cases.SelectMany(c => c.Payload.Select(p => ($"{u.Name}.{c.Name}", p.Type))),
    _ => [],
  };

  static bool Reaches(Ty from, Ty target, List<string> path, HashSet<Ty> visited) {
    foreach (var (edge, t) in Edges(from)) {
      path.Add(edge);
      if (ReferenceEquals(t, target) || (visited.Add(t) && Reaches(t, target, path, visited))) {
        return true;
      }
      path.RemoveAt(path.Count - 1);
    }
    return false;
  }
}
