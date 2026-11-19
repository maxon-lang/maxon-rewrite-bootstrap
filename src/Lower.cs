namespace MiniMaxon;

public sealed class Lowerer {
  const int Word = 8;

  readonly FuncDecl fn;
  readonly IrFunction f;
  readonly Dictionary<Node, (IrBlock Break, IrBlock? Continue)> jumps = [];
  IrBlock cur;

  Lowerer(FuncDecl fn) {
    this.fn = fn;
    f = new IrFunction(fn, fn.Params.Count + (fn.IsMethod ? 1 : 0));
    foreach (var l in fn.Locals.Where(l => !l.IsParam).OrderBy(l => l.Slot)) {
      if (l.Slot != f.Slots.Count) {
        throw new InternalError($"lower: local slots of {fn.Symbol} are not dense");
      }
      f.NewSlot(KindOf(l.Type));
    }
    cur = f.NewBlock();
  }

  public static IrFunction Lower(FuncDecl fn) {
    var l = new Lowerer(fn);
    l.Block(fn.Body);
    if (l.cur.Term is null) {
      l.cur.Term = new RetTerm(l.Const(0));
    }
    l.f.RemoveUnreachable();
    return l.f;
  }

  static IrKind KindOf(Ty t) => t.IsManaged ? IrKind.Managed : IrKind.Scalar;

  static long Bits(Int128 v) => v > IntTy.I64Max ? (long)(ulong)v : (long)v;

  void Emit(Instr i) => cur.Instrs.Add(i);

  void Seal(Terminator t) {
    cur.Term ??= t;
    cur = f.NewBlock();
  }

  void Goto(IrBlock target) {
    cur.Term ??= new JumpTerm(target);
    cur = target;
  }

  V Reg(IrKind kind = IrKind.Scalar) => f.NewReg(kind);

  V Const(long value) {
    var d = Reg();
    Emit(new ConstInstr(d, value));
    return d;
  }

  V Str(string value) {
    var d = Reg(IrKind.Managed);
    Emit(new StrInstr(d, value));
    return d;
  }

  V Param(int index, IrKind kind) {
    var d = Reg(kind);
    Emit(new LoadParam(d, index));
    return d;
  }

  V Self() => Param(fn.SelfLocal!.ParamIndex, IrKind.Managed);

  V Runtime(string symbol, IrKind kind, params V[] args) {
    var d = Reg(kind);
    Emit(new CallRuntimeInstr(d, symbol, args));
    return d;
  }

  void Block(List<Stmt> body) {
    foreach (var s in body) {
      Stmt(s);
    }
  }

  void Stmt(Stmt s) {
    switch (s) {
      case LetStmt l:
        Emit(new StoreSlot(l.Sym!.Slot, Expr(l.Init)));
        break;
      case AssignStmt a:
        Assign(a);
        break;
      case DiscardStmt d:
        Expr(d.Value);
        break;
      case ExprStmt e:
        Expr(e.E);
        break;
      case IfStmt i: {
        var then = f.NewBlock();
        var end = f.NewBlock();
        var els = i.Else is null ? end : f.NewBlock();
        cur.Term = new BranchTerm(Expr(i.Cond), then, els);
        cur = then;
        Block(i.Then);
        Goto(end);
        if (i.Else is not null) {
          cur = els;
          Block(i.Else);
          Goto(end);
        }
        cur = end;
        break;
      }
      case WhileStmt w: {
        var top = f.NewBlock();
        var body = f.NewBlock();
        var end = f.NewBlock();
        jumps[w] = (end, top);
        Goto(top);
        cur.Term = new BranchTerm(Expr(w.Cond), body, end);
        cur = body;
        Block(w.Body);
        Goto(top);
        cur = end;
        break;
      }
      case ForStmt fr:
        For(fr);
        break;
      case MatchStmt m:
        Match(m.Match, null);
        break;
      case ReturnStmt r:
        Seal(new RetTerm(r.Value is null ? Const(0) : Expr(r.Value)));
        break;
      case BreakStmt b:
        Seal(new JumpTerm(jumps[b.Target!].Break));
        break;
      case ContinueStmt c:
        Seal(new JumpTerm(jumps[c.Target!].Continue!));
        break;
      default:
        throw new InternalError($"lower: unhandled statement {s.GetType().Name}");
    }
  }

  void Assign(AssignStmt a) {
    switch (a.Target) {
      case NameExpr { Kind: NameKind.Local, Local: var l }: {
        var v = Expr(a.Value);
        if (l!.ByRef) {
          Emit(new StoreAt(Param(l.ParamIndex, IrKind.Scalar), v));
        } else {
          Emit(new StoreSlot(l.Slot, v));
        }
        break;
      }
      case NameExpr { Kind: NameKind.SelfField, Field: var fi }: {
        var v = Expr(a.Value);
        Emit(new StoreField(Self(), Word * fi!.Index, v));
        break;
      }
      case MemberExpr { Kind: MemberKind.Field } m: {
        var obj = Expr(m.Obj);
        var v = Expr(a.Value);
        Emit(new StoreField(obj, Word * m.Field!.Index, v));
        break;
      }
      default:
        throw new InternalError("lower: unhandled assignment target");
    }
  }

  void For(ForStmt s) {
    var slot = s.Sym!.Slot;
    var limit = f.NewSlot(IrKind.Scalar);
    var unsigned = s.Sym.Type is IntTy { Unsigned: true };
    var body = f.NewBlock();
    var cont = f.NewBlock();
    var step = f.NewBlock();
    var exit = f.NewBlock();
    jumps[s] = (exit, cont);

    Emit(new StoreSlot(slot, Expr(s.From)));
    Emit(new StoreSlot(limit, Expr(s.To)));
    var past = Compare(s.Inclusive ? CmpOp.Gt : CmpOp.Ge, Load(slot), Load(limit), unsigned);
    cur.Term = new BranchTerm(past, exit, body);

    cur = body;
    Block(s.Body);
    Goto(cont);

    if (s.Inclusive) {
      cur.Term = new BranchTerm(Compare(CmpOp.Eq, Load(slot), Load(limit), unsigned), exit, step);
      cur = step;
    } else {
      Goto(step);
    }
    var next = Reg();
    Emit(new BinInstr(next, BinOp.Add, Load(slot), Const(1), unsigned, Checked: false));
    Emit(new StoreSlot(slot, next));
    if (s.Inclusive) {
      cur.Term = new JumpTerm(body);
    } else {
      cur.Term = new BranchTerm(Compare(CmpOp.Ge, next, Load(limit), unsigned), exit, body);
    }
    cur = exit;
  }

  V Load(int slot) {
    var d = Reg(f.Slots[slot]);
    Emit(new LoadSlot(d, slot));
    return d;
  }

  V Compare(CmpOp op, V a, V b, bool unsigned) {
    var d = Reg();
    Emit(new CmpInstr(d, op, a, b, unsigned));
    return d;
  }

  V Expr(Expr e) {
    if (e.Type is ConstIntTy c) {
      return Const(Bits(c.Value));
    }
    switch (e) {
      case IntLit i:
        return Const(Bits(i.Value));
      case BoolLit b:
        return Const(b.Value ? 1 : 0);
      case StrLit s:
        return String(s);
      case NameExpr n:
        return Name(n);
      case SelfExpr:
        return Self();
      case BinaryExpr b:
        return Binary(b);
      case UnaryExpr u:
        return Unary(u);
      case CondExpr c2:
        return Choose(c2.Type, Expr(c2.Cond), () => Converted(c2.Then, c2.Type), () => Converted(c2.Else, c2.Type));
      case CallExpr call:
        return Call(call);
      case MemberExpr m:
        return Member(m);
      case StructLitExpr sl:
        return Record([.. sl.Ordered.Select((v, i) => v ?? sl.Record!.Fields[i].Default!)], tag: null);
      case CastExpr cast: {
        var v = Expr(cast.E);
        Fit(v, (IntTy)cast.Type, cast.E.Type);
        return v;
      }
      case MatchExpr m:
        return Match(m.Match, m.Type);
      case PanicExpr p:
        Emit(new CallRuntimeInstr(null, Rt.Panic, [Expr(p.Message)]));
        Seal(new TrapTerm());
        return Const(0);
      default:
        throw new InternalError($"lower: unhandled expression {e.GetType().Name}");
    }
  }

  V Choose(Ty type, V cond, Func<V> then, Func<V> els) {
    var result = f.NewSlot(KindOf(type));
    var a = f.NewBlock();
    var b = f.NewBlock();
    var end = f.NewBlock();
    cur.Term = new BranchTerm(cond, a, b);
    cur = a;
    Emit(new StoreSlot(result, then()));
    Goto(end);
    cur = b;
    Emit(new StoreSlot(result, els()));
    Goto(end);
    return Load(result);
  }

  V Converted(Expr e, Ty target) {
    var v = Expr(e);
    if (target is IntTy ti && e.Type is IntTy) {
      Fit(v, ti, e.Type);
    }
    return v;
  }

  V Name(NameExpr n) {
    switch (n.Kind) {
      case NameKind.Local: {
        var l = n.Local!;
        if (!l.IsParam) {
          return Load(l.Slot);
        }
        if (!l.ByRef) {
          return Param(l.ParamIndex, KindOf(l.Type));
        }
        var d = Reg(KindOf(l.Type));
        Emit(new LoadAt(d, Param(l.ParamIndex, IrKind.Scalar)));
        return d;
      }
      case NameKind.SelfField: {
        var d = Reg(KindOf(n.Field!.Type));
        Emit(new LoadField(d, Self(), Word * n.Field.Index));
        return d;
      }
      case NameKind.Const:
        return Expr(n.Const!.Value);
      default:
        throw new InternalError($"lower: unresolved name '{n.Name}'");
    }
  }

  V Address(Expr e) {
    switch (e) {
      case NameExpr { Kind: NameKind.Local, Local: var l }: {
        if (l!.ByRef) {
          return Param(l.ParamIndex, IrKind.Scalar);
        }
        var d = Reg();
        Emit(new SlotAddr(d, l.Slot));
        return d;
      }
      case NameExpr { Kind: NameKind.SelfField, Field: var fi }: {
        var d = Reg();
        Emit(new FieldAddr(d, Self(), Word * fi!.Index));
        return d;
      }
      case MemberExpr { Kind: MemberKind.Field } m: {
        var obj = Expr(m.Obj);
        var d = Reg();
        Emit(new FieldAddr(d, obj, Word * m.Field!.Index));
        return d;
      }
      default:
        throw new InternalError("lower: no address for this expression");
    }
  }

  V Call(CallExpr c) {
    switch (c.Kind) {
      case CallKind.Print or CallKind.PrintError: {
        var text = Expr(c.Ordered[0]);
        var handle = Reg();
        Emit(new LoadGlobal(handle, c.Kind == CallKind.Print ? Rt.Stdout : Rt.Stderr));
        Emit(new CallRuntimeInstr(null, Rt.Write, [handle, text]));
        return Const(0);
      }
      case CallKind.Function or CallKind.Static or CallKind.Method: {
        var target = c.Target!;
        var args = new List<V>();
        if (c.Receiver is not null) {
          args.Add(Expr(c.Receiver));
        }
        for (var i = 0; i < c.Ordered.Count; i++) {
          args.Add(target.Params[i].Mutable ? Address(c.Ordered[i]) : Converted(c.Ordered[i], target.ParamTypes[i]));
        }
        if (target.ReturnType == PrimTy.Void) {
          Emit(new CallInstr(null, target, [.. args]));
          return Const(0);
        }
        var d = Reg(KindOf(target.ReturnType));
        Emit(new CallInstr(d, target, [.. args]));
        return d;
      }
      case CallKind.UnionCtor:
        return Record(c.Ordered, tag: c.CaseIndex);
      default:
        throw new InternalError("lower: unresolved call");
    }
  }

  V Record(List<Expr> values, int? tag) {
    var baseOffset = tag is null ? 0 : Word;
    var p = Reg(IrKind.Managed);
    Emit(new AllocInstr(p, baseOffset + Word * Math.Max(values.Count, 1)));
    if (tag is int t) {
      Emit(new StoreField(p, 0, Const(t)));
    }
    for (var i = 0; i < values.Count; i++) {
      Emit(new StoreField(p, baseOffset + Word * i, Expr(values[i])));
    }
    return p;
  }

  V Tag(V union) {
    var d = Reg();
    Emit(new LoadField(d, union, 0));
    return d;
  }

  V Member(MemberExpr m) {
    switch (m.Kind) {
      case MemberKind.Field: {
        var obj = Expr(m.Obj);
        var d = Reg(KindOf(m.Field!.Type));
        Emit(new LoadField(d, obj, Word * m.Field.Index));
        return d;
      }
      case MemberKind.EnumCase:
        return Const(m.CaseIndex);
      case MemberKind.UnionCase:
        return Record([], tag: m.CaseIndex);
      case MemberKind.EnumOrdinal:
        return Expr(m.Obj);
      case MemberKind.EnumName:
        return Names(Expr(m.Obj), ((EnumTy)m.Obj.Type).Cases);
      case MemberKind.EnumRaw: {
        var et = (EnumTy)m.Obj.Type;
        var v = Expr(m.Obj);
        return et.HasExplicitRaw ? Select(v, IrKind.Scalar, et.Raw.Count, i => Const(Bits(et.Raw[i]))) : v;
      }
      case MemberKind.UnionOrdinal:
        return Tag(Expr(m.Obj));
      case MemberKind.UnionName:
        return Names(Tag(Expr(m.Obj)), [.. ((UnionTy)m.Obj.Type).Cases.Select(c => c.Name)]);
      default:
        throw new InternalError("lower: unresolved member");
    }
  }

  V Names(V index, List<string> names) => Select(index, IrKind.Managed, names.Count, i => Str(names[i]));

  V Select(V index, IrKind kind, int count, Func<int, V> value) {
    var result = f.NewSlot(kind);
    var end = f.NewBlock();
    var targets = new IrBlock[count];
    for (var i = 0; i < count; i++) {
      targets[i] = f.NewBlock();
    }
    cur.Term = new SwitchTerm(index, [.. Enumerable.Range(0, count - 1).Select(i => (long)i)], targets[..^1], targets[^1]);
    for (var i = 0; i < count; i++) {
      cur = targets[i];
      Emit(new StoreSlot(result, value(i)));
      Goto(end);
    }
    return Load(result);
  }

  V String(StrLit s) {
    if (s.Parts.All(p => p is string)) {
      return Str(string.Concat(s.Parts.Cast<string>()));
    }
    var acc = Part(s.Parts[0]);
    for (var i = 1; i < s.Parts.Count; i++) {
      acc = Runtime(Rt.Concat, IrKind.Managed, acc, Part(s.Parts[i]));
    }
    return acc;
  }

  V Part(object part) {
    if (part is string text) {
      return Str(text);
    }
    var e = (Expr)part;
    var v = Expr(e);
    return e.Type switch {
      var t when t == PrimTy.String => v,
      var t when t == PrimTy.Bool => Names(v, ["false", "true"]),
      ConstIntTy or IntTy => Runtime(Rt.Itoa, IrKind.Managed, v, Const(e.Type is IntTy { Unsigned: true } ? 1 : 0)),
      EnumTy et => Names(v, et.Cases),
      UnionTy ut => Names(Tag(v), [.. ut.Cases.Select(c => c.Name)]),
      _ => throw new InternalError($"lower: cannot interpolate {e.Type}"),
    };
  }

  static readonly Dictionary<string, BinOp> BinOps = new() {
    ["+"] = BinOp.Add, ["-"] = BinOp.Sub, ["*"] = BinOp.Mul, ["/"] = BinOp.Div, ["mod"] = BinOp.Mod,
    ["and"] = BinOp.And, ["or"] = BinOp.Or, ["xor"] = BinOp.Xor, ["shl"] = BinOp.Shl, ["shr"] = BinOp.Shr,
  };

  static readonly Dictionary<string, CmpOp> CmpOps = new() {
    ["=="] = CmpOp.Eq, ["!="] = CmpOp.Ne, ["<"] = CmpOp.Lt, ["<="] = CmpOp.Le, [">"] = CmpOp.Gt, [">="] = CmpOp.Ge,
  };

  V Binary(BinaryExpr b) {
    if (b.Op is "and" or "or" && b.Type == PrimTy.Bool) {
      var result = f.NewSlot(IrKind.Scalar);
      var rhs = f.NewBlock();
      var end = f.NewBlock();
      var l = Expr(b.L);
      Emit(new StoreSlot(result, l));
      cur.Term = b.Op == "and" ? new BranchTerm(l, rhs, end) : new BranchTerm(l, end, rhs);
      cur = rhs;
      Emit(new StoreSlot(result, Expr(b.R)));
      Goto(end);
      return Load(result);
    }

    if (b.OperandType == PrimTy.String) {
      var eq = Runtime(Rt.Streq, IrKind.Scalar, Expr(b.L), Expr(b.R));
      return b.Op == "!=" ? Flip(eq) : eq;
    }

    var a = Expr(b.L);
    var c = Expr(b.R);
    var unsigned = b.OperandType is IntTy { Unsigned: true };
    if (CmpOps.TryGetValue(b.Op, out var cmp)) {
      return Compare(cmp, a, c, unsigned);
    }
    var d = Reg();
    var op = BinOps[b.Op];
    Emit(new BinInstr(d, op, a, c, unsigned, Checked: op is BinOp.Add or BinOp.Sub or BinOp.Mul));
    if (b.Type is IntTy t) {
      RangeCheck(d, t);
    }
    return d;
  }

  V Flip(V boolean) {
    var d = Reg();
    Emit(new BinInstr(d, BinOp.Xor, boolean, Const(1), Unsigned: false, Checked: false));
    return d;
  }

  V Unary(UnaryExpr u) {
    var v = Expr(u.E);
    if (u.Op == "not" && u.Type == PrimTy.Bool) {
      return Flip(v);
    }
    var d = Reg();
    Emit(u.Op == "not" ? new NotInstr(d, v) : new NegInstr(d, v));
    RangeCheck(d, (IntTy)u.Type);
    return d;
  }

  void RangeCheck(V v, IntTy t) => Fit(v, t, t.Unsigned ? new IntTy(null, 0, IntTy.U64Max) : IntTy.Anonymous64);

  void Fit(V v, IntTy target, Ty source) {
    if (source is ConstIntTy) {
      return;
    }
    var src = (IntTy)source;
    if (target.Contains(src) && target.Unsigned == src.Unsigned) {
      return;
    }

    if (target.Unsigned) {
      if (!src.Unsigned && src.Min < 0) {
        Emit(new GuardInstr(v, GuardKind.NonNegative, 0));
      }
      if (target.Min > Int128.Max(src.Min, 0)) {
        Emit(new GuardInstr(v, GuardKind.UnsignedAtLeast, Bits(target.Min)));
      }
      if (target.Max < src.Max) {
        Emit(new GuardInstr(v, GuardKind.UnsignedAtMost, Bits(target.Max)));
      }
      return;
    }

    var srcMax = src.Max;
    if (src.Unsigned && src.Max > IntTy.I64Max) {
      Emit(new GuardInstr(v, GuardKind.NonNegative, 0));
      srcMax = IntTy.I64Max;
    }
    if (target.Min > src.Min) {
      Emit(new GuardInstr(v, GuardKind.SignedAtLeast, Bits(target.Min)));
    }
    if (target.Max < srcMax) {
      Emit(new GuardInstr(v, GuardKind.SignedAtMost, Bits(target.Max)));
    }
  }

  V Match(MatchCore m, Ty? resultType) {
    var st = m.Subject.Type;
    var subject = Expr(m.Subject);
    var end = f.NewBlock();
    jumps[m] = (end, null);
    var result = resultType is null ? -1 : f.NewSlot(KindOf(resultType));

    foreach (var arm in m.Arms) {
      var body = f.NewBlock();
      var next = f.NewBlock();
      if (arm.IsDefault) {
        Goto(body);
      } else {
        foreach (var pat in arm.Patterns) {
          Test(pat, st, subject, body);
        }
        Goto(next);
      }
      cur = body;

      foreach (var pat in arm.Patterns) {
        for (var i = 0; i < pat.BindingLocals.Count; i++) {
          if (pat.BindingLocals[i] is { } local) {
            var d = Reg(KindOf(local.Type));
            Emit(new LoadField(d, subject, Word + Word * i));
            Emit(new StoreSlot(local.Slot, d));
          }
        }
      }

      switch (arm.Kind) {
        case ArmKind.Then:
          Stmt(arm.Action!);
          break;
        case ArmKind.Gives:
          Emit(new StoreSlot(result, Converted(arm.Value!, resultType!)));
          break;
        case ArmKind.Panic:
          Expr(arm.Value!);
          break;
      }
      Goto(end);
      cur = next;
    }
    cur.Term ??= new TrapTerm();
    cur = end;
    return result < 0 ? Const(0) : Load(result);
  }

  void Test(Pattern pat, Ty st, V subject, IrBlock body) {
    var unsigned = st is IntTy { Unsigned: true };
    var fail = f.NewBlock();
    switch (pat.Kind) {
      case PatternKind.Case:
        cur.Term = new BranchTerm(Compare(CmpOp.Eq, st is UnionTy ? Tag(subject) : subject, Const(pat.CaseIndex), false), body, fail);
        break;
      case PatternKind.Literal when st == PrimTy.String:
        cur.Term = new BranchTerm(Runtime(Rt.Streq, IrKind.Scalar, subject, Expr(pat.Value!)), body, fail);
        break;
      case PatternKind.Literal: {
        var value = pat.Value is BoolLit bl ? (bl.Value ? 1 : 0) : Bits(((ConstIntTy)pat.Value!.Type).Value);
        cur.Term = new BranchTerm(Compare(CmpOp.Eq, subject, Const(value), unsigned), body, fail);
        break;
      }
      case PatternKind.Range: {
        var lo = ((ConstIntTy)pat.Value!.Type).Value;
        var hi = ((ConstIntTy)pat.High!.Type).Value - (pat.Inclusive ? 0 : 1);
        var upper = f.NewBlock();
        cur.Term = new BranchTerm(Compare(CmpOp.Ge, subject, Const(Bits(lo)), unsigned), upper, fail);
        cur = upper;
        cur.Term = new BranchTerm(Compare(CmpOp.Le, subject, Const(Bits(hi)), unsigned), body, fail);
        break;
      }
    }
    cur = fail;
  }
}
