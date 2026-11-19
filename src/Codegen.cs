namespace MiniMaxon;

public sealed class Codegen {
  const int Word = 8;
  const int PageSize = 4096;

  readonly CodeUnit u = new();
  readonly Label rtAlloc = Label.Extern(Rt.Alloc);
  readonly Label panicRange = Label.Extern(Rt.PanicRange), panicOverflow = Label.Extern(Rt.PanicOverflow);
  readonly Label panicDiv = Label.Extern(Rt.PanicDiv), panicShift = Label.Extern(Rt.PanicShift);

  IrFunction f = null!;
  Label[] labels = [];

  Asm a => u.Asm;

  public static CodeUnit EmitModule(IEnumerable<IrFunction> functions) {
    var g = new Codegen();
    foreach (var f in functions) {
      g.Function(f);
    }
    return g.u;
  }

  int SlotOffset(int slot) => -Word * (slot + 1);

  int RegOffset(V v) => -Word * (f.Slots.Count + v.Id + 1);

  int ParamOffset(int index) => 16 + Word * (f.ParamCount - 1 - index);

  void Get(R r, V v) => a.Load(r, R.Rbp, RegOffset(v));

  void Put(V v, R r) => a.Store(R.Rbp, RegOffset(v), r);

  void Function(IrFunction fn) {
    f = fn;
    labels = [.. f.Blocks.Select(_ => new Label())];
    u.DefineCode(f.Decl);
    a.Push(R.Rbp);
    a.Mov(R.Rbp, R.Rsp);
    AllocateFrame((f.Slots.Count + f.Regs.Count) * Word);

    for (var i = 0; i < f.Blocks.Count; i++) {
      var b = f.Blocks[i];
      a.Bind(labels[i]);
      foreach (var instr in b.Instrs) {
        Instr(instr);
      }
      Term(b.Term ?? throw new InternalError($"codegen: block b{b.Id} of {f.Decl.Symbol} has no terminator"),
        i + 1 < f.Blocks.Count ? f.Blocks[i + 1] : null);
    }
  }

  void AllocateFrame(int bytes) {
    var frame = (bytes + 15) & ~15;
    while (frame > PageSize) {
      a.SubImm(R.Rsp, PageSize);
      a.Store(R.Rsp, 0, R.Rax);
      frame -= PageSize;
    }
    if (frame > 0) {
      a.SubImm(R.Rsp, frame);
    }
  }

  void Call(Label target, V[] args, V? dst) {
    foreach (var arg in args) {
      Get(R.Rax, arg);
      a.Push(R.Rax);
    }
    a.Call(target);
    if (args.Length > 0) {
      a.AddImm(R.Rsp, args.Length * Word);
    }
    if (dst is { } d) {
      Put(d, R.Rax);
    }
  }

  void Instr(Instr i) {
    switch (i) {
      case ConstInstr c:
        a.MovImm(R.Rax, c.Value);
        Put(c.Dst, R.Rax);
        break;
      case StrInstr s:
        a.LeaData(R.Rax, u.Data.String(s.Value));
        Put(s.Dst, R.Rax);
        break;
      case LoadSlot l:
        a.Load(R.Rax, R.Rbp, SlotOffset(l.Slot));
        Put(l.Dst, R.Rax);
        break;
      case StoreSlot s:
        Get(R.Rax, s.Src);
        a.Store(R.Rbp, SlotOffset(s.Slot), R.Rax);
        break;
      case SlotAddr sa:
        a.Lea(R.Rax, R.Rbp, SlotOffset(sa.Slot));
        Put(sa.Dst, R.Rax);
        break;
      case LoadParam p:
        a.Load(R.Rax, R.Rbp, ParamOffset(p.Param));
        Put(p.Dst, R.Rax);
        break;
      case LoadField lf:
        Get(R.Rax, lf.Obj);
        a.Load(R.Rax, R.Rax, lf.Offset);
        Put(lf.Dst, R.Rax);
        break;
      case StoreField sf:
        Get(R.Rcx, sf.Obj);
        Get(R.Rax, sf.Src);
        a.Store(R.Rcx, sf.Offset, R.Rax);
        break;
      case FieldAddr fa:
        Get(R.Rax, fa.Obj);
        a.Lea(R.Rax, R.Rax, fa.Offset);
        Put(fa.Dst, R.Rax);
        break;
      case LoadAt la:
        Get(R.Rax, la.Addr);
        a.Load(R.Rax, R.Rax, 0);
        Put(la.Dst, R.Rax);
        break;
      case StoreAt sa:
        Get(R.Rcx, sa.Addr);
        Get(R.Rax, sa.Src);
        a.Store(R.Rcx, 0, R.Rax);
        break;
      case LoadGlobal g:
        a.LoadSymbol(R.Rax, g.Symbol);
        Put(g.Dst, R.Rax);
        break;
      case BinInstr b:
        Get(R.Rax, b.A);
        Get(R.Rcx, b.B);
        Binary(b);
        Put(b.Dst, R.Rax);
        break;
      case CmpInstr c:
        Get(R.Rax, c.A);
        Get(R.Rcx, c.B);
        a.Cmp(R.Rax, R.Rcx);
        a.SetccRax(c.Op switch {
          CmpOp.Eq => Cc.E,
          CmpOp.Ne => Cc.NE,
          CmpOp.Lt => c.Unsigned ? Cc.B : Cc.L,
          CmpOp.Le => c.Unsigned ? Cc.BE : Cc.LE,
          CmpOp.Gt => c.Unsigned ? Cc.A : Cc.G,
          CmpOp.Ge => c.Unsigned ? Cc.AE : Cc.GE,
          _ => throw new InternalError($"codegen: comparison {c.Op}"),
        });
        Put(c.Dst, R.Rax);
        break;
      case NegInstr n:
        Get(R.Rax, n.Src);
        a.Neg(R.Rax);
        a.Jcc(Cc.O, panicOverflow);
        Put(n.Dst, R.Rax);
        break;
      case NotInstr n:
        Get(R.Rax, n.Src);
        a.Not(R.Rax);
        Put(n.Dst, R.Rax);
        break;
      case GuardInstr g:
        Guard(g);
        break;
      case AllocInstr al:
        a.MovImm(R.Rax, al.Bytes);
        a.Push(R.Rax);
        a.Call(rtAlloc);
        a.AddImm(R.Rsp, Word);
        Put(al.Dst, R.Rax);
        break;
      case CallInstr c:
        Call(Label.Extern(c.Target), c.Args, c.Dst);
        break;
      case CallRuntimeInstr c:
        Call(Label.Extern(c.Symbol), c.Args, c.Dst);
        break;
      default:
        throw new InternalError($"codegen: unhandled instruction {i.GetType().Name}");
    }
  }

  void Binary(BinInstr b) {
    switch (b.Op) {
      case BinOp.Add:
        a.Add(R.Rax, R.Rcx);
        Overflow(b);
        break;
      case BinOp.Sub:
        a.Sub(R.Rax, R.Rcx);
        Overflow(b);
        break;
      case BinOp.Mul:
        if (b.Unsigned) {
          a.MulRax(R.Rcx);
          if (b.Checked) {
            a.Jcc(Cc.B, panicOverflow);
          }
        } else {
          a.Imul(R.Rax, R.Rcx);
          if (b.Checked) {
            a.Jcc(Cc.O, panicOverflow);
          }
        }
        break;
      case BinOp.Div or BinOp.Mod:
        a.Test(R.Rcx, R.Rcx);
        a.Jcc(Cc.E, panicDiv);
        if (b.Unsigned) {
          a.Xor(R.Rdx, R.Rdx);
          a.Div(R.Rcx);
        } else {
          a.Cqo();
          a.Idiv(R.Rcx);
        }
        if (b.Op == BinOp.Mod) {
          a.Mov(R.Rax, R.Rdx);
        }
        break;
      case BinOp.And:
        a.And(R.Rax, R.Rcx);
        break;
      case BinOp.Or:
        a.Or(R.Rax, R.Rcx);
        break;
      case BinOp.Xor:
        a.Xor(R.Rax, R.Rcx);
        break;
      case BinOp.Shl or BinOp.Shr:
        Shift(b.Op == BinOp.Shl, b.Unsigned);
        break;
      default:
        throw new InternalError($"codegen: operator {b.Op}");
    }
  }

  void Overflow(BinInstr b) {
    if (b.Checked) {
      a.Jcc(b.Unsigned ? Cc.B : Cc.O, panicOverflow);
    }
  }

  void Shift(bool left, bool unsigned) {
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

  void Guard(GuardInstr g) {
    Get(R.Rax, g.Src);
    if (g.Kind == GuardKind.NonNegative) {
      a.Test(R.Rax, R.Rax);
      a.Jcc(Cc.S, panicRange);
      return;
    }
    a.MovImm(R.Rcx, g.Bound);
    a.Cmp(R.Rax, R.Rcx);
    a.Jcc(g.Kind switch {
      GuardKind.SignedAtLeast => Cc.L,
      GuardKind.SignedAtMost => Cc.G,
      GuardKind.UnsignedAtLeast => Cc.B,
      GuardKind.UnsignedAtMost => Cc.A,
      _ => throw new InternalError($"codegen: guard {g.Kind}"),
    }, panicRange);
  }

  void Term(Terminator t, IrBlock? next) {
    switch (t) {
      case JumpTerm j:
        JumpUnlessNext(j.Target, next);
        break;
      case BranchTerm br:
        Get(R.Rax, br.Cond);
        a.Test(R.Rax, R.Rax);
        a.Jcc(Cc.E, labels[br.Else.Id]);
        JumpUnlessNext(br.Then, next);
        break;
      case SwitchTerm s:
        Get(R.Rax, s.Value);
        for (var i = 0; i < s.Values.Length; i++) {
          a.MovImm(R.Rcx, s.Values[i]);
          a.Cmp(R.Rax, R.Rcx);
          a.Jcc(Cc.E, labels[s.Targets[i].Id]);
        }
        JumpUnlessNext(s.Default, next);
        break;
      case RetTerm r:
        Get(R.Rax, r.Value);
        a.Mov(R.Rsp, R.Rbp);
        a.Pop(R.Rbp);
        a.Ret();
        break;
      case TrapTerm:
        a.Raw(0x0F, 0x0B);
        break;
      default:
        throw new InternalError($"codegen: unhandled terminator {t.GetType().Name}");
    }
  }

  void JumpUnlessNext(IrBlock target, IrBlock? next) {
    if (!ReferenceEquals(target, next)) {
      a.Jmp(labels[target.Id]);
    }
  }
}
