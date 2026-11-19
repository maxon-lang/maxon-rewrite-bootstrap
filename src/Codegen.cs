namespace MiniMaxon;

public sealed class Codegen {
  static readonly string[] Imports = ["GetProcessHeap", "HeapAlloc", "GetStdHandle", "WriteFile", "ExitProcess"];
  const int ImpGetProcessHeap = 0;
  const int ImpHeapAlloc = 1;
  const int ImpGetStdHandle = 2;
  const int ImpWriteFile = 3;
  const int ImpExitProcess = 4;
  const int StdOutputHandle = -11;
  const int StdErrorHandle = -12;
  const int HeapZeroMemory = 8;
  const int PanicExitCode = 1;
  const int Word = 8;

  readonly Asm a = new();
  readonly DataSection data = new();
  readonly int gHeap;
  readonly int gStdout;
  readonly int gStderr;
  readonly Dictionary<FuncDecl, Label> fnLabels = [];
  readonly Label rtAlloc = new(), rtWrite = new(), rtPanic = new(), rtConcat = new(), rtItoa = new(), rtStreq = new();
  readonly Label panicRange = new(), panicOverflow = new(), panicDiv = new(), panicShift = new();

  FuncDecl fn = null!;
  int slotCount;
  Label epilogue = new();
  readonly Dictionary<Node, (Label Break, Label? Continue)> jumps = [];

  Codegen() {
    gHeap = data.Reserve(Word);
    gStdout = data.Reserve(Word);
    gStderr = data.Reserve(Word);
  }

  public static void Compile(Checker checker, string outPath) {
    var g = new Codegen();
    foreach (var f in checker.Functions) {
      g.fnLabels[f] = new Label();
    }
    var entry = g.EmitEntry(checker.Main!);
    foreach (var f in checker.Functions) {
      g.EmitFunction(f);
    }
    g.EmitRuntime();
    PeWriter.Write(outPath, g.a, g.data.ToArray(), "kernel32.dll", Imports, entry);
  }

  static long Bits(Int128 v) => v > IntTy.I64Max ? (long)(ulong)v : (long)v;

  int ParamCount => fn.Params.Count + (fn.IsMethod ? 1 : 0);

  int Off(Local l) => l.IsParam ? 16 + Word * (ParamCount - 1 - l.ParamIndex) : -Word * (l.Slot + 1);

  int NewTemp() => -Word * (++slotCount);

  int EmitEntry(FuncDecl main) {
    var at = a.Pos;
    a.SubImm(R.Rsp, 40);
    a.CallImport(ImpGetProcessHeap);
    a.StoreData(gHeap, R.Rax);
    a.MovImm(R.Rcx, StdOutputHandle);
    a.CallImport(ImpGetStdHandle);
    a.StoreData(gStdout, R.Rax);
    a.MovImm(R.Rcx, StdErrorHandle);
    a.CallImport(ImpGetStdHandle);
    a.StoreData(gStderr, R.Rax);
    a.Call(fnLabels[main]);
    a.Mov(R.Rcx, R.Rax);
    a.CallImport(ImpExitProcess);
    return at;
  }

  void EmitFunction(FuncDecl f) {
    fn = f;
    slotCount = f.Locals.Where(l => !l.IsParam).Select(l => l.Slot + 1).DefaultIfEmpty(0).Max();
    epilogue = new Label();
    jumps.Clear();

    a.Bind(fnLabels[f]);
    a.Push(R.Rbp);
    a.Mov(R.Rbp, R.Rsp);
    var frameAt = a.SubRspPlaceholder();

    EmitBlock(f.Body);
    a.MovImm(R.Rax, 0);

    a.Bind(epilogue);
    a.Mov(R.Rsp, R.Rbp);
    a.Pop(R.Rbp);
    a.Ret();
    a.Patch32(frameAt, (slotCount * Word + 15) & ~15);
  }

  void EmitBlock(List<Stmt> body) {
    foreach (var s in body) {
      EmitStmt(s);
    }
  }

  void CallRuntime(Label l, int args) {
    a.Call(l);
    if (args > 0) {
      a.AddImm(R.Rsp, args * Word);
    }
  }

  void EmitStmt(Stmt s) {
    switch (s) {
      case LetStmt l:
        EmitExpr(l.Init);
        a.Store(R.Rbp, Off(l.Sym!), R.Rax);
        break;
      case AssignStmt asg:
        EmitAssign(asg);
        break;
      case DiscardStmt d:
        EmitExpr(d.Value);
        break;
      case ExprStmt e:
        EmitExpr(e.E);
        break;
      case IfStmt i: {
        var els = new Label();
        var end = new Label();
        EmitExpr(i.Cond);
        a.Test(R.Rax, R.Rax);
        a.Jcc(Cc.E, els);
        EmitBlock(i.Then);
        a.Jmp(end);
        a.Bind(els);
        if (i.Else is not null) {
          EmitBlock(i.Else);
        }
        a.Bind(end);
        break;
      }
      case WhileStmt w: {
        var top = new Label();
        var end = new Label();
        jumps[w] = (end, top);
        a.Bind(top);
        EmitExpr(w.Cond);
        a.Test(R.Rax, R.Rax);
        a.Jcc(Cc.E, end);
        EmitBlock(w.Body);
        a.Jmp(top);
        a.Bind(end);
        break;
      }
      case ForStmt f:
        EmitFor(f);
        break;
      case MatchStmt m:
        EmitMatch(m.Match);
        break;
      case ReturnStmt r:
        if (r.Value is not null) {
          EmitExpr(r.Value);
        } else {
          a.MovImm(R.Rax, 0);
        }
        a.Jmp(epilogue);
        break;
      case BreakStmt b:
        a.Jmp(jumps[b.Target!].Break);
        break;
      case ContinueStmt c:
        a.Jmp(jumps[c.Target!].Continue!);
        break;
      default:
        throw s.Error(Err.Unsupported, $"codegen: unhandled statement {s.GetType().Name}");
    }
  }

  void EmitAssign(AssignStmt asg) {
    switch (asg.Target) {
      case NameExpr { Kind: NameKind.Local, Local: var l }:
        EmitExpr(asg.Value);
        if (l!.ByRef) {
          a.Load(R.Rcx, R.Rbp, Off(l));
          a.Store(R.Rcx, 0, R.Rax);
        } else {
          a.Store(R.Rbp, Off(l), R.Rax);
        }
        break;
      case NameExpr { Kind: NameKind.SelfField, Field: var f }:
        EmitExpr(asg.Value);
        a.Load(R.Rcx, R.Rbp, Off(fn.SelfLocal!));
        a.Store(R.Rcx, Word * f!.Index, R.Rax);
        break;
      case MemberExpr { Kind: MemberKind.Field } m:
        EmitExpr(m.Obj);
        a.Push(R.Rax);
        EmitExpr(asg.Value);
        a.Pop(R.Rcx);
        a.Store(R.Rcx, Word * m.Field!.Index, R.Rax);
        break;
      default:
        throw asg.Error(Err.Unsupported, "codegen: unhandled assignment target");
    }
  }

  void EmitFor(ForStmt f) {
    var varOff = Off(f.Sym!);
    var endOff = NewTemp();
    var unsigned = f.Sym!.Type is IntTy { Unsigned: true };
    var body = new Label();
    var cont = new Label();
    var exit = new Label();
    jumps[f] = (exit, cont);

    EmitExpr(f.From);
    a.Store(R.Rbp, varOff, R.Rax);
    EmitExpr(f.To);
    a.Store(R.Rbp, endOff, R.Rax);

    a.Load(R.Rax, R.Rbp, varOff);
    a.Load(R.Rcx, R.Rbp, endOff);
    a.Cmp(R.Rax, R.Rcx);
    a.Jcc(f.Inclusive ? (unsigned ? Cc.A : Cc.G) : (unsigned ? Cc.AE : Cc.GE), exit);

    a.Bind(body);
    EmitBlock(f.Body);

    a.Bind(cont);
    a.Load(R.Rax, R.Rbp, varOff);
    a.Load(R.Rcx, R.Rbp, endOff);
    if (f.Inclusive) {
      a.Cmp(R.Rax, R.Rcx);
      a.Jcc(Cc.E, exit);
    }
    a.AddImm(R.Rax, 1);
    a.Store(R.Rbp, varOff, R.Rax);
    if (!f.Inclusive) {
      a.Cmp(R.Rax, R.Rcx);
      a.Jcc(unsigned ? Cc.AE : Cc.GE, exit);
    }
    a.Jmp(body);
    a.Bind(exit);
  }

  void EmitExpr(Expr e) {
    if (e.Type is ConstIntTy c) {
      a.MovImm(R.Rax, Bits(c.Value));
      return;
    }

    switch (e) {
      case IntLit i:
        a.MovImm(R.Rax, Bits(i.Value));
        break;
      case BoolLit b:
        a.MovImm(R.Rax, b.Value ? 1 : 0);
        break;
      case StrLit s:
        EmitString(s);
        break;
      case NameExpr n:
        EmitName(n);
        break;
      case SelfExpr:
        a.Load(R.Rax, R.Rbp, Off(fn.SelfLocal!));
        break;
      case BinaryExpr b:
        EmitBinary(b);
        break;
      case UnaryExpr u:
        EmitUnary(u);
        break;
      case CondExpr c2: {
        var els = new Label();
        var end = new Label();
        EmitExpr(c2.Cond);
        a.Test(R.Rax, R.Rax);
        a.Jcc(Cc.E, els);
        EmitConverted(c2.Then, c2.Type);
        a.Jmp(end);
        a.Bind(els);
        EmitConverted(c2.Else, c2.Type);
        a.Bind(end);
        break;
      }
      case CallExpr call:
        EmitCall(call);
        break;
      case MemberExpr m:
        EmitMember(m);
        break;
      case StructLitExpr sl:
        EmitRecord(sl.Ordered.Select((v, i) => v ?? sl.Record!.Fields[i].Default!).ToList(), tag: null);
        break;
      case CastExpr cast:
        EmitExpr(cast.E);
        EmitFitCheck((IntTy)cast.Type, cast.E.Type);
        break;
      case MatchExpr m:
        EmitMatch(m.Match, m.Type);
        break;
      case PanicExpr p:
        EmitExpr(p.Message);
        a.Push(R.Rax);
        a.Call(rtPanic);
        break;
      default:
        throw e.Error(Err.Unsupported, $"codegen: unhandled expression {e.GetType().Name}");
    }
  }

  void EmitConverted(Expr e, Ty target) {
    EmitExpr(e);
    if (target is IntTy ti && e.Type is IntTy) {
      EmitFitCheck(ti, e.Type);
    }
  }

  void EmitName(NameExpr n) {
    switch (n.Kind) {
      case NameKind.Local:
        a.Load(R.Rax, R.Rbp, Off(n.Local!));
        if (n.Local!.ByRef) {
          a.Load(R.Rax, R.Rax, 0);
        }
        break;
      case NameKind.SelfField:
        a.Load(R.Rax, R.Rbp, Off(fn.SelfLocal!));
        a.Load(R.Rax, R.Rax, Word * n.Field!.Index);
        break;
      case NameKind.Const:
        EmitExpr(n.Const!.Value);
        break;
      default:
        throw n.Error(Err.Unsupported, $"codegen: unresolved name '{n.Name}'");
    }
  }

  void EmitAddress(Expr e) {
    switch (e) {
      case NameExpr { Kind: NameKind.Local, Local: var l }:
        if (l!.ByRef) {
          a.Load(R.Rax, R.Rbp, Off(l));
        } else {
          a.Lea(R.Rax, R.Rbp, Off(l));
        }
        break;
      case NameExpr { Kind: NameKind.SelfField, Field: var f }:
        a.Load(R.Rax, R.Rbp, Off(fn.SelfLocal!));
        a.Lea(R.Rax, R.Rax, Word * f!.Index);
        break;
      case MemberExpr { Kind: MemberKind.Field } m:
        EmitExpr(m.Obj);
        a.Lea(R.Rax, R.Rax, Word * m.Field!.Index);
        break;
      default:
        throw e.Error(Err.Unsupported, "codegen: no address for this expression");
    }
  }

  void EmitCall(CallExpr c) {
    switch (c.Kind) {
      case CallKind.Print or CallKind.PrintError:
        EmitExpr(c.Ordered[0]);
        a.LoadData(R.Rcx, c.Kind == CallKind.Print ? gStdout : gStderr);
        a.Push(R.Rcx);
        a.Push(R.Rax);
        CallRuntime(rtWrite, 2);
        break;
      case CallKind.Function or CallKind.Static or CallKind.Method: {
        var target = c.Target!;
        var n = c.Ordered.Count;
        if (c.Receiver is not null) {
          EmitExpr(c.Receiver);
          a.Push(R.Rax);
          n++;
        }
        for (var i = 0; i < c.Ordered.Count; i++) {
          if (target.Params[i].Mutable) {
            EmitAddress(c.Ordered[i]);
          } else {
            EmitConverted(c.Ordered[i], target.ParamTypes[i]);
          }
          a.Push(R.Rax);
        }
        CallRuntime(fnLabels[target], n);
        break;
      }
      case CallKind.UnionCtor:
        EmitRecord(c.Ordered, tag: c.CaseIndex);
        break;
      default:
        throw c.Error(Err.Unsupported, "codegen: unresolved call");
    }
  }

  void EmitRecord(List<Expr> values, int? tag) {
    var baseOff = tag is null ? 0 : Word;
    var temp = NewTemp();
    a.MovImm(R.Rax, baseOff + Word * Math.Max(values.Count, 1));
    a.Push(R.Rax);
    CallRuntime(rtAlloc, 1);
    a.Store(R.Rbp, temp, R.Rax);
    if (tag is int t) {
      a.MovImm(R.Rcx, t);
      a.Store(R.Rax, 0, R.Rcx);
    }
    for (var i = 0; i < values.Count; i++) {
      EmitExpr(values[i]);
      a.Load(R.Rcx, R.Rbp, temp);
      a.Store(R.Rcx, baseOff + Word * i, R.Rax);
    }
    a.Load(R.Rax, R.Rbp, temp);
  }

  void EmitMember(MemberExpr m) {
    switch (m.Kind) {
      case MemberKind.Field:
        EmitExpr(m.Obj);
        a.Load(R.Rax, R.Rax, Word * m.Field!.Index);
        break;
      case MemberKind.EnumCase:
        a.MovImm(R.Rax, m.CaseIndex);
        break;
      case MemberKind.UnionCase:
        EmitRecord([], tag: m.CaseIndex);
        break;
      case MemberKind.EnumOrdinal:
        EmitExpr(m.Obj);
        break;
      case MemberKind.EnumName:
        EmitExpr(m.Obj);
        EmitSelect(((EnumTy)m.Obj.Type).Cases, name => a.LeaData(R.Rax, data.String(name)));
        break;
      case MemberKind.EnumRaw: {
        var et = (EnumTy)m.Obj.Type;
        EmitExpr(m.Obj);
        if (et.HasExplicitRaw) {
          EmitSelect([.. et.Raw], raw => a.MovImm(R.Rax, Bits(raw)));
        }
        break;
      }
      case MemberKind.UnionOrdinal:
        EmitExpr(m.Obj);
        a.Load(R.Rax, R.Rax, 0);
        break;
      case MemberKind.UnionName:
        EmitExpr(m.Obj);
        a.Load(R.Rax, R.Rax, 0);
        EmitSelect(((UnionTy)m.Obj.Type).Cases.Select(c => c.Name).ToList(), name => a.LeaData(R.Rax, data.String(name)));
        break;
      default:
        throw m.Error(Err.Unsupported, "codegen: unresolved member");
    }
  }

  void EmitSelect<TValue>(List<TValue> values, Action<TValue> load) {
    var end = new Label();
    for (var i = 0; i < values.Count - 1; i++) {
      var next = new Label();
      a.CmpImm(R.Rax, i);
      a.Jcc(Cc.NE, next);
      load(values[i]);
      a.Jmp(end);
      a.Bind(next);
    }
    load(values[^1]);
    a.Bind(end);
  }

  void EmitString(StrLit s) {
    if (s.Parts.All(p => p is string)) {
      a.LeaData(R.Rax, data.String(string.Concat(s.Parts.Cast<string>())));
      return;
    }
    for (var i = 0; i < s.Parts.Count; i++) {
      if (i > 0) {
        a.Push(R.Rax);
      }
      EmitPartAsString(s.Parts[i]);
      if (i > 0) {
        a.Pop(R.Rcx);
        a.Push(R.Rcx);
        a.Push(R.Rax);
        CallRuntime(rtConcat, 2);
      }
    }
  }

  void EmitPartAsString(object part) {
    if (part is string text) {
      a.LeaData(R.Rax, data.String(text));
      return;
    }
    var e = (Expr)part;
    EmitExpr(e);
    switch (e.Type) {
      case var t when t == PrimTy.String:
        break;
      case var t when t == PrimTy.Bool:
        EmitSelect(["false", "true"], v => a.LeaData(R.Rax, data.String(v)));
        break;
      case ConstIntTy or IntTy:
        var unsigned = e.Type is IntTy { Unsigned: true };
        a.Push(R.Rax);
        a.MovImm(R.Rax, unsigned ? 1 : 0);
        a.Push(R.Rax);
        CallRuntime(rtItoa, 2);
        break;
      case EnumTy et:
        EmitSelect(et.Cases, name => a.LeaData(R.Rax, data.String(name)));
        break;
      case UnionTy ut:
        a.Load(R.Rax, R.Rax, 0);
        EmitSelect(ut.Cases.Select(c => c.Name).ToList(), name => a.LeaData(R.Rax, data.String(name)));
        break;
      default:
        throw e.Error(Err.Unsupported, $"codegen: cannot interpolate {e.Type}");
    }
  }

  void EmitBinary(BinaryExpr b) {
    if (b.Op is "and" or "or" && b.Type == PrimTy.Bool) {
      var end = new Label();
      EmitExpr(b.L);
      a.Test(R.Rax, R.Rax);
      a.Jcc(b.Op == "and" ? Cc.E : Cc.NE, end);
      EmitExpr(b.R);
      a.Bind(end);
      return;
    }

    if (b.OperandType == PrimTy.String) {
      EmitExpr(b.L);
      a.Push(R.Rax);
      EmitExpr(b.R);
      a.Push(R.Rax);
      CallRuntime(rtStreq, 2);
      if (b.Op == "!=") {
        a.MovImm(R.Rcx, 1);
        a.Xor(R.Rax, R.Rcx);
      }
      return;
    }

    EmitExpr(b.L);
    a.Push(R.Rax);
    EmitExpr(b.R);
    a.Mov(R.Rcx, R.Rax);
    a.Pop(R.Rax);

    var unsigned = b.OperandType is IntTy { Unsigned: true };
    switch (b.Op) {
      case "==" or "!=" or "<" or "<=" or ">" or ">=":
        a.Cmp(R.Rax, R.Rcx);
        a.SetccRax(b.Op switch {
          "==" => Cc.E,
          "!=" => Cc.NE,
          "<" => unsigned ? Cc.B : Cc.L,
          "<=" => unsigned ? Cc.BE : Cc.LE,
          ">" => unsigned ? Cc.A : Cc.G,
          _ => unsigned ? Cc.AE : Cc.GE,
        });
        return;
      case "xor" when b.Type == PrimTy.Bool:
        a.Xor(R.Rax, R.Rcx);
        return;
      case "+":
        a.Add(R.Rax, R.Rcx);
        a.Jcc(unsigned ? Cc.B : Cc.O, panicOverflow);
        break;
      case "-":
        a.Sub(R.Rax, R.Rcx);
        a.Jcc(unsigned ? Cc.B : Cc.O, panicOverflow);
        break;
      case "*":
        if (unsigned) {
          a.MulRax(R.Rcx);
          a.Jcc(Cc.B, panicOverflow);
        } else {
          a.Imul(R.Rax, R.Rcx);
          a.Jcc(Cc.O, panicOverflow);
        }
        break;
      case "/" or "mod":
        a.Test(R.Rcx, R.Rcx);
        a.Jcc(Cc.E, panicDiv);
        if (unsigned) {
          a.Xor(R.Rdx, R.Rdx);
          a.Div(R.Rcx);
        } else {
          a.Cqo();
          a.Idiv(R.Rcx);
        }
        if (b.Op == "mod") {
          a.Mov(R.Rax, R.Rdx);
        }
        break;
      case "and":
        a.And(R.Rax, R.Rcx);
        break;
      case "or":
        a.Or(R.Rax, R.Rcx);
        break;
      case "xor":
        a.Xor(R.Rax, R.Rcx);
        break;
      case "shl" or "shr":
        EmitShift(b.Op == "shl", unsigned);
        break;
      default:
        throw b.Error(Err.Unsupported, $"codegen: operator '{b.Op}'");
    }
    EmitRangeCheck((IntTy)b.Type);
  }

  void EmitShift(bool left, bool unsigned) {
    var big = new Label();
    var done = new Label();
    a.CmpImm(R.Rcx, 0);
    a.Jcc(Cc.L, panicShift);
    a.CmpImm(R.Rcx, 64);
    a.Jcc(Cc.GE, big);
    if (left) {
      a.ShlCl(R.Rax);
    } else if (unsigned) {
      a.ShrCl(R.Rax);
    } else {
      a.SarCl(R.Rax);
    }
    a.Jmp(done);
    a.Bind(big);
    if (!left && !unsigned) {
      a.SarImm(R.Rax, 63);
    } else {
      a.Xor(R.Rax, R.Rax);
    }
    a.Bind(done);
  }

  void EmitUnary(UnaryExpr u) {
    EmitExpr(u.E);
    if (u.Op == "not") {
      if (u.Type == PrimTy.Bool) {
        a.MovImm(R.Rcx, 1);
        a.Xor(R.Rax, R.Rcx);
        return;
      }
      a.Not(R.Rax);
    } else {
      a.Neg(R.Rax);
      a.Jcc(Cc.O, panicOverflow);
    }
    EmitRangeCheck((IntTy)u.Type);
  }

  void EmitRangeCheck(IntTy t) {
    var full = t.Unsigned ? new IntTy(null, 0, IntTy.U64Max) : IntTy.Anonymous64;
    EmitFitCheck(t, full);
  }

  void EmitFitCheck(IntTy target, Ty source) {
    if (source is ConstIntTy) {
      return;
    }
    var src = (IntTy)source;
    if (target.Contains(src) && target.Unsigned == src.Unsigned) {
      return;
    }

    if (target.Unsigned) {
      if (!src.Unsigned && src.Min < 0) {
        a.Test(R.Rax, R.Rax);
        a.Jcc(Cc.S, panicRange);
      }
      if (target.Min > Int128.Max(src.Min, 0)) {
        a.MovImm(R.Rcx, Bits(target.Min));
        a.Cmp(R.Rax, R.Rcx);
        a.Jcc(Cc.B, panicRange);
      }
      if (target.Max < src.Max) {
        a.MovImm(R.Rcx, Bits(target.Max));
        a.Cmp(R.Rax, R.Rcx);
        a.Jcc(Cc.A, panicRange);
      }
      return;
    }

    var srcMax = src.Max;
    if (src.Unsigned && src.Max > IntTy.I64Max) {
      a.Test(R.Rax, R.Rax);
      a.Jcc(Cc.S, panicRange);
      srcMax = IntTy.I64Max;
    }
    if (target.Min > src.Min) {
      a.MovImm(R.Rcx, Bits(target.Min));
      a.Cmp(R.Rax, R.Rcx);
      a.Jcc(Cc.L, panicRange);
    }
    if (target.Max < srcMax) {
      a.MovImm(R.Rcx, Bits(target.Max));
      a.Cmp(R.Rax, R.Rcx);
      a.Jcc(Cc.G, panicRange);
    }
  }

  void EmitMatch(MatchCore m, Ty? result = null) {
    var st = m.Subject.Type;
    EmitExpr(m.Subject);
    var subj = NewTemp();
    a.Store(R.Rbp, subj, R.Rax);
    var end = new Label();
    jumps[m] = (end, null);

    foreach (var arm in m.Arms) {
      var body = new Label();
      var next = new Label();
      if (!arm.IsDefault) {
        foreach (var pat in arm.Patterns) {
          EmitPatternTest(pat, st, subj, body);
        }
        a.Jmp(next);
      }
      a.Bind(body);

      foreach (var pat in arm.Patterns) {
        for (var i = 0; i < pat.BindingLocals.Count; i++) {
          if (pat.BindingLocals[i] is { } local) {
            a.Load(R.Rax, R.Rbp, subj);
            a.Load(R.Rax, R.Rax, Word + Word * i);
            a.Store(R.Rbp, Off(local), R.Rax);
          }
        }
      }

      switch (arm.Kind) {
        case ArmKind.Then:
          EmitStmt(arm.Action!);
          break;
        case ArmKind.Gives:
          EmitConverted(arm.Value!, result!);
          break;
        case ArmKind.Panic:
          EmitExpr(arm.Value!);
          break;
      }
      a.Jmp(end);
      a.Bind(next);
    }
    a.Raw(0x0F, 0x0B);
    a.Bind(end);
  }

  void EmitPatternTest(Pattern pat, Ty st, int subj, Label body) {
    a.Load(R.Rax, R.Rbp, subj);
    var unsigned = st is IntTy { Unsigned: true };
    switch (pat.Kind) {
      case PatternKind.Case:
        if (st is UnionTy) {
          a.Load(R.Rax, R.Rax, 0);
        }
        a.CmpImm(R.Rax, pat.CaseIndex);
        a.Jcc(Cc.E, body);
        break;
      case PatternKind.Literal when st == PrimTy.String:
        a.Push(R.Rax);
        EmitExpr(pat.Value!);
        a.Push(R.Rax);
        CallRuntime(rtStreq, 2);
        a.Test(R.Rax, R.Rax);
        a.Jcc(Cc.NE, body);
        break;
      case PatternKind.Literal:
        a.MovImm(R.Rcx, pat.Value is BoolLit bl ? (bl.Value ? 1 : 0) : Bits(((ConstIntTy)pat.Value!.Type).Value));
        a.Cmp(R.Rax, R.Rcx);
        a.Jcc(Cc.E, body);
        break;
      case PatternKind.Range: {
        var skip = new Label();
        var lo = ((ConstIntTy)pat.Value!.Type).Value;
        var hi = ((ConstIntTy)pat.High!.Type).Value - (pat.Inclusive ? 0 : 1);
        a.MovImm(R.Rcx, Bits(lo));
        a.Cmp(R.Rax, R.Rcx);
        a.Jcc(unsigned ? Cc.B : Cc.L, skip);
        a.MovImm(R.Rcx, Bits(hi));
        a.Cmp(R.Rax, R.Rcx);
        a.Jcc(unsigned ? Cc.BE : Cc.LE, body);
        a.Bind(skip);
        break;
      }
    }
  }

  void Prologue(int locals, bool callsWindows) {
    a.Push(R.Rbp);
    a.Mov(R.Rbp, R.Rsp);
    if (locals > 0) {
      a.SubImm(R.Rsp, locals);
    }
    if (callsWindows) {
      a.AndImm(R.Rsp, -16);
      a.SubImm(R.Rsp, 48);
    }
  }

  void Epilogue() {
    a.Mov(R.Rsp, R.Rbp);
    a.Pop(R.Rbp);
    a.Ret();
  }

  void EmitRuntime() {
    a.Bind(rtAlloc);
    Prologue(0, callsWindows: true);
    a.LoadData(R.Rcx, gHeap);
    a.MovImm(R.Rdx, HeapZeroMemory);
    a.Load(R.R8, R.Rbp, 16);
    a.CallImport(ImpHeapAlloc);
    Epilogue();

    a.Bind(rtWrite);
    Prologue(0, callsWindows: true);
    a.Load(R.Rcx, R.Rbp, 24);
    a.Load(R.Rdx, R.Rbp, 16);
    a.Load(R.R8, R.Rdx, 0);
    a.AddImm(R.Rdx, Word);
    a.Lea(R.R9, R.Rsp, 40);
    a.Xor(R.Rax, R.Rax);
    a.Store(R.Rsp, 32, R.Rax);
    a.CallImport(ImpWriteFile);
    Epilogue();

    a.Bind(rtPanic);
    Prologue(0, callsWindows: false);
    foreach (var piece in new int?[] { data.String("panic: "), null, data.String("\n") }) {
      a.LoadData(R.Rax, gStderr);
      a.Push(R.Rax);
      if (piece is int off) {
        a.LeaData(R.Rax, off);
      } else {
        a.Load(R.Rax, R.Rbp, 16);
      }
      a.Push(R.Rax);
      CallRuntime(rtWrite, 2);
    }
    a.AndImm(R.Rsp, -16);
    a.SubImm(R.Rsp, 32);
    a.MovImm(R.Rcx, PanicExitCode);
    a.CallImport(ImpExitProcess);

    a.Bind(rtConcat);
    Prologue(0, callsWindows: false);
    a.Load(R.Rsi, R.Rbp, 24);
    a.Load(R.Rdi, R.Rbp, 16);
    a.Load(R.Rbx, R.Rsi, 0);
    a.Load(R.Rax, R.Rdi, 0);
    a.Add(R.Rbx, R.Rax);
    a.Lea(R.Rax, R.Rbx, Word);
    a.Push(R.Rax);
    CallRuntime(rtAlloc, 1);
    a.Store(R.Rax, 0, R.Rbx);
    a.Mov(R.R12, R.Rax);
    a.Lea(R.Rdi, R.R12, Word);
    foreach (var argOff in new[] { 24, 16 }) {
      a.Load(R.Rsi, R.Rbp, argOff);
      a.Load(R.Rcx, R.Rsi, 0);
      a.AddImm(R.Rsi, Word);
      a.RepMovsb();
    }
    a.Mov(R.Rax, R.R12);
    Epilogue();

    a.Bind(rtItoa);
    Prologue(64, callsWindows: false);
    var digits = new Label();
    var loop = new Label();
    var noSign = new Label();
    a.Load(R.Rax, R.Rbp, 24);
    a.Load(R.R8, R.Rbp, 16);
    a.Xor(R.R9, R.R9);
    a.Mov(R.Rsi, R.Rbp);
    a.Test(R.R8, R.R8);
    a.Jcc(Cc.NE, digits);
    a.Test(R.Rax, R.Rax);
    a.Jcc(Cc.NS, digits);
    a.Neg(R.Rax);
    a.MovImm(R.R9, 1);
    a.Bind(digits);
    a.MovImm(R.Rcx, 10);
    a.Bind(loop);
    a.Xor(R.Rdx, R.Rdx);
    a.Div(R.Rcx);
    a.AddImm(R.Rdx, '0');
    a.SubImm(R.Rsi, 1);
    a.Raw(0x88, 0x16);
    a.Test(R.Rax, R.Rax);
    a.Jcc(Cc.NE, loop);
    a.Test(R.R9, R.R9);
    a.Jcc(Cc.E, noSign);
    a.SubImm(R.Rsi, 1);
    a.Raw(0xC6, 0x06, (byte)'-');
    a.Bind(noSign);
    a.Mov(R.Rbx, R.Rbp);
    a.Sub(R.Rbx, R.Rsi);
    a.Lea(R.Rax, R.Rbx, Word);
    a.Push(R.Rax);
    CallRuntime(rtAlloc, 1);
    a.Store(R.Rax, 0, R.Rbx);
    a.Mov(R.R12, R.Rax);
    a.Lea(R.Rdi, R.Rax, Word);
    a.Mov(R.Rcx, R.Rbx);
    a.RepMovsb();
    a.Mov(R.Rax, R.R12);
    Epilogue();

    a.Bind(rtStreq);
    Prologue(0, callsWindows: false);
    var yes = new Label();
    var no = new Label();
    var done = new Label();
    a.Load(R.Rsi, R.Rbp, 24);
    a.Load(R.Rdi, R.Rbp, 16);
    a.Load(R.Rcx, R.Rsi, 0);
    a.Load(R.Rax, R.Rdi, 0);
    a.Cmp(R.Rcx, R.Rax);
    a.Jcc(Cc.NE, no);
    a.Test(R.Rcx, R.Rcx);
    a.Jcc(Cc.E, yes);
    a.AddImm(R.Rsi, Word);
    a.AddImm(R.Rdi, Word);
    a.Raw(0xF3, 0xA6);
    a.Jcc(Cc.NE, no);
    a.Bind(yes);
    a.MovImm(R.Rax, 1);
    a.Jmp(done);
    a.Bind(no);
    a.MovImm(R.Rax, 0);
    a.Bind(done);
    Epilogue();

    foreach (var (label, message) in new[] {
      (panicRange, "integer value out of range"),
      (panicOverflow, "integer overflow"),
      (panicDiv, "division by zero"),
      (panicShift, "negative shift count"),
    }) {
      a.Bind(label);
      a.LeaData(R.Rax, data.String(message));
      a.Push(R.Rax);
      a.Call(rtPanic);
    }
  }
}
