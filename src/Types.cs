namespace MiniMaxon;

public abstract class Ty {
  public abstract string Name { get; }

  public override string ToString() => Name;

  public virtual bool IsManaged => false;
}

public sealed class IntTy(string? alias, Int128 min, Int128 max) : Ty {
  public static readonly Int128 I64Min = long.MinValue;
  public static readonly Int128 I64Max = long.MaxValue;
  public static readonly Int128 U64Max = ulong.MaxValue;

  public static readonly IntTy Anonymous64 = new(null, I64Min, I64Max);

  public string? Alias { get; } = alias;
  public Int128 Min { get; } = min;
  public Int128 Max { get; } = max;

  public bool Unsigned => Max > I64Max;
  public bool IsAnonymous => Alias is null;
  public bool IsFullWidth => Unsigned ? Min == 0 && Max == U64Max : Min == I64Min && Max == I64Max;

  public bool Contains(Int128 v) => v >= Min && v <= Max;
  public bool Contains(IntTy other) => other.Min >= Min && other.Max <= Max;

  public override string Name => Alias ?? $"int({Min} to {Max})";
}

public sealed class ConstIntTy(Int128 value) : Ty {
  public Int128 Value { get; } = value;

  public override string Name => $"integer constant {Value}";
}

public sealed class PrimTy(string name, bool managed = false) : Ty {
  public static readonly PrimTy Bool = new("bool");
  public static readonly PrimTy String = new("String", managed: true);
  public static readonly PrimTy Void = new("void");
  public static readonly PrimTy Never = new("never");

  public override string Name => name;
  public override bool IsManaged => managed;
}

public abstract class NominalTy(string name) : Ty {
  public override string Name => name;
  public Dictionary<string, FuncDecl> Methods { get; } = [];
}

public sealed class FieldInfo(string name, Ty type, bool isVar, Expr? @default, int index) {
  public string Name { get; } = name;
  public Ty Type { get; set; } = type;
  public bool IsVar { get; } = isVar;
  public Expr? Default { get; } = @default;
  public int Index { get; } = index;
}

public sealed class RecordTy(TypeDecl decl) : NominalTy(decl.Name) {
  public TypeDecl Decl { get; } = decl;
  public List<FieldInfo> Fields { get; } = [];

  public FieldInfo? Field(string name) => Fields.Find(f => f.Name == name);

  public override bool IsManaged => true;
}

public sealed class EnumTy(EnumDecl decl) : NominalTy(decl.Name) {
  public EnumDecl Decl { get; } = decl;
  public List<string> Cases { get; } = [];
  public List<Int128> Raw { get; } = [];
  public bool HasExplicitRaw { get; set; }
}

public sealed record UnionCase(string Name, List<(string Name, Ty Type)> Payload);

public sealed class UnionTy(UnionDecl decl) : NominalTy(decl.Name) {
  public UnionDecl Decl { get; } = decl;
  public List<UnionCase> Cases { get; } = [];

  public int CaseIndex(string name) => Cases.FindIndex(c => c.Name == name);

  public override bool IsManaged => true;
}

public sealed class Local(string name, Ty type, bool isVar, Node site) {
  public string Name { get; } = name;
  public Ty Type { get; set; } = type;
  public bool IsVar { get; } = isVar;
  public Node Site { get; } = site;
  public bool IsParam { get; init; }
  public bool IsSelf { get; init; }
  public bool ByRef { get; init; }
  public int ParamIndex { get; init; } = -1;
  public int Slot { get; set; } = -1;
}
