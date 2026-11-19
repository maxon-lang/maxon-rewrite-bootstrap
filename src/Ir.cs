using System.Text;

namespace MiniMaxon;

public enum IrKind { Scalar, Managed }

public readonly record struct V(int Id) {
  public override string ToString() => $"%{Id}";
}

public enum BinOp { Add, Sub, Mul, Div, Mod, And, Or, Xor, Shl, Shr }

public enum CmpOp { Eq, Ne, Lt, Le, Gt, Ge }

public enum GuardKind { NonNegative, SignedAtLeast, SignedAtMost, UnsignedAtLeast, UnsignedAtMost }

public abstract record Instr;
public sealed record ConstInstr(V Dst, long Value) : Instr;
public sealed record StrInstr(V Dst, string Value) : Instr;
public sealed record LoadSlot(V Dst, int Slot) : Instr;
public sealed record StoreSlot(int Slot, V Src) : Instr;
public sealed record SlotAddr(V Dst, int Slot) : Instr;
public sealed record LoadParam(V Dst, int Param) : Instr;
public sealed record LoadField(V Dst, V Obj, int Offset) : Instr;
public sealed record StoreField(V Obj, int Offset, V Src) : Instr;
public sealed record FieldAddr(V Dst, V Obj, int Offset) : Instr;
public sealed record LoadAt(V Dst, V Addr) : Instr;
public sealed record StoreAt(V Addr, V Src) : Instr;
public sealed record LoadGlobal(V Dst, string Symbol) : Instr;
public sealed record BinInstr(V Dst, BinOp Op, V A, V B, bool Unsigned, bool Checked) : Instr;
public sealed record CmpInstr(V Dst, CmpOp Op, V A, V B, bool Unsigned) : Instr;
public sealed record NegInstr(V Dst, V Src) : Instr;
public sealed record NotInstr(V Dst, V Src) : Instr;
public sealed record GuardInstr(V Src, GuardKind Kind, long Bound) : Instr;
public sealed record AllocInstr(V Dst, int Bytes) : Instr;
public sealed record CallInstr(V? Dst, FuncDecl Target, V[] Args) : Instr;
public sealed record CallRuntimeInstr(V? Dst, string Symbol, V[] Args) : Instr;

public abstract record Terminator;
public sealed record JumpTerm(IrBlock Target) : Terminator;
public sealed record BranchTerm(V Cond, IrBlock Then, IrBlock Else) : Terminator;
public sealed record SwitchTerm(V Value, long[] Values, IrBlock[] Targets, IrBlock Default) : Terminator;
public sealed record RetTerm(V Value) : Terminator;
public sealed record TrapTerm : Terminator;

public sealed class IrBlock(int id) {
  public int Id { get; set; } = id;
  public List<Instr> Instrs { get; } = [];
  public Terminator? Term { get; set; }
}

public sealed class IrFunction(FuncDecl decl, int paramCount) {
  public FuncDecl Decl { get; } = decl;
  public int ParamCount { get; } = paramCount;
  public List<IrKind> Slots { get; } = [];
  public List<IrKind> Regs { get; } = [];
  public List<IrBlock> Blocks { get; } = [];

  public V NewReg(IrKind kind) {
    Regs.Add(kind);
    return new V(Regs.Count - 1);
  }

  public int NewSlot(IrKind kind) {
    Slots.Add(kind);
    return Slots.Count - 1;
  }

  public IrBlock NewBlock() {
    var b = new IrBlock(Blocks.Count);
    Blocks.Add(b);
    return b;
  }

  public void RemoveUnreachable() {
    var reached = new bool[Blocks.Count];
    var work = new Stack<IrBlock>();
    work.Push(Blocks[0]);
    reached[0] = true;
    while (work.Count > 0) {
      foreach (var next in Successors(work.Pop())) {
        if (!reached[next.Id]) {
          reached[next.Id] = true;
          work.Push(next);
        }
      }
    }
    var kept = Blocks.Where(b => reached[b.Id]).ToList();
    Blocks.Clear();
    Blocks.AddRange(kept);
    for (var i = 0; i < Blocks.Count; i++) {
      Blocks[i].Id = i;
    }
  }

  public static IEnumerable<IrBlock> Successors(IrBlock b) => b.Term switch {
    JumpTerm j => [j.Target],
    BranchTerm br => [br.Then, br.Else],
    SwitchTerm s => [.. s.Targets, s.Default],
    RetTerm or TrapTerm => [],
    _ => throw new InternalError($"block b{b.Id} has no terminator"),
  };
}

public static class IrPrinter {
  static string Kind(IrKind k) => k == IrKind.Managed ? "managed" : "scalar";

  static string Args(V[] args) => string.Join(", ", args.Select(a => a.ToString()));

  static string Quote(string s) => "\"" + s.Replace("\\", "\\\\").Replace("\"", "\\\"").Replace("\n", "\\n") + "\"";

  public static void Print(IrFunction f, StringBuilder sb) {
    sb.Append($"function {f.Decl.Symbol} params {f.ParamCount}");
    if (f.Slots.Count > 0) {
      sb.Append(" slots [").Append(string.Join(", ", f.Slots.Select((k, i) => $"s{i} {Kind(k)}"))).Append(']');
    }
    sb.Append('\n');
    foreach (var b in f.Blocks) {
      sb.Append($"b{b.Id}:\n");
      foreach (var i in b.Instrs) {
        sb.Append("  ").Append(Instr(i)).Append('\n');
      }
      sb.Append("  ").Append(Term(b.Term)).Append('\n');
    }
    sb.Append('\n');
  }

  static string Instr(Instr i) => i switch {
    ConstInstr c => $"{c.Dst} = const {c.Value}",
    StrInstr s => $"{s.Dst} = str {Quote(s.Value)}",
    LoadSlot l => $"{l.Dst} = load s{l.Slot}",
    StoreSlot s => $"store s{s.Slot}, {s.Src}",
    SlotAddr a => $"{a.Dst} = addr s{a.Slot}",
    LoadParam p => $"{p.Dst} = param {p.Param}",
    LoadField l => $"{l.Dst} = field {l.Obj}+{l.Offset}",
    StoreField s => $"setfield {s.Obj}+{s.Offset}, {s.Src}",
    FieldAddr a => $"{a.Dst} = fieldaddr {a.Obj}+{a.Offset}",
    LoadAt l => $"{l.Dst} = loadat {l.Addr}",
    StoreAt s => $"storeat {s.Addr}, {s.Src}",
    LoadGlobal g => $"{g.Dst} = global {g.Symbol}",
    BinInstr b => $"{b.Dst} = {b.Op.ToString().ToLowerInvariant()}{(b.Unsigned ? ".u" : "")}{(b.Checked ? ".checked" : "")} {b.A}, {b.B}",
    CmpInstr c => $"{c.Dst} = cmp.{c.Op.ToString().ToLowerInvariant()}{(c.Unsigned ? ".u" : "")} {c.A}, {c.B}",
    NegInstr n => $"{n.Dst} = neg.checked {n.Src}",
    NotInstr n => $"{n.Dst} = not {n.Src}",
    GuardInstr g => $"guard {g.Src} {g.Kind} {g.Bound}",
    AllocInstr a => $"{a.Dst} = alloc {a.Bytes}",
    CallInstr c => $"{(c.Dst is { } d ? $"{d} = " : "")}call {c.Target.Symbol}({Args(c.Args)})",
    CallRuntimeInstr c => $"{(c.Dst is { } d ? $"{d} = " : "")}runtime {c.Symbol}({Args(c.Args)})",
    _ => throw new InternalError($"printer: unhandled instruction {i.GetType().Name}"),
  };

  static string Term(Terminator? t) => t switch {
    JumpTerm j => $"jump b{j.Target.Id}",
    BranchTerm b => $"branch {b.Cond}, b{b.Then.Id}, b{b.Else.Id}",
    SwitchTerm s => $"switch {s.Value} [{string.Join(", ", s.Values.Select((v, i) => $"{v}: b{s.Targets[i].Id}"))}] default b{s.Default.Id}",
    RetTerm r => $"ret {r.Value}",
    TrapTerm => "trap",
    _ => throw new InternalError("printer: a block has no terminator"),
  };
}
