namespace MiniMaxon;

public sealed class Ownership(List<CompileError> errors) {
  sealed class State {
    public static readonly State Unreachable = new(null, dead: true);
    public static readonly State Clean = new(null, dead: false);

    readonly Dictionary<Local, Node>? moved;

    State(Dictionary<Local, Node>? moved, bool dead) {
      this.moved = moved;
      Dead = dead;
    }

    public bool Dead { get; }

    public bool TryGetMove(Local l, out Node site) {
      site = null!;
      return moved is not null && moved.TryGetValue(l, out site!);
    }

    public State With(Local l, Node site) {
      if (Dead) {
        return this;
      }
      var m = moved is null ? new Dictionary<Local, Node>() : new Dictionary<Local, Node>(moved);
      m[l] = site;
      return new State(m, dead: false);
    }

    public State Without(Local l) {
      if (Dead || moved is null || !moved.ContainsKey(l)) {
        return this;
      }
      if (moved.Count == 1) {
        return Clean;
      }
      var m = new Dictionary<Local, Node>(moved);
      m.Remove(l);
      return new State(m, dead: false);
    }

    public static State Merge(State a, State b) {
      if (ReferenceEquals(a, b) || b.Dead || b.moved is null && !a.Dead) {
        return a;
      }
      if (a.Dead || a.moved is null) {
        return b;
      }
      Dictionary<Local, Node>? m = null;
      foreach (var (k, v) in b.moved!) {
        if (!a.moved.ContainsKey(k)) {
          m ??= new Dictionary<Local, Node>(a.moved);
          m[k] = v;
        }
      }
      return m is null ? a : new State(m, dead: false);
    }
  }

  sealed class Frame(Node target) {
    public Node Target { get; } = target;
    public State Breaks { get; set; } = State.Unreachable;
    public State Continues { get; set; } = State.Unreachable;
  }

  readonly List<Frame> frames = [];
  readonly Dictionary<Node, string> moveTargets = [];

  static bool IsMovable(Local l) => l.IsVar && !l.IsParam && l.Type is RecordTy or UnionTy;

  public void Check(FuncDecl f) {
    var anyMovable = false;
    foreach (var l in f.Locals) {
      anyMovable |= IsMovable(l);
    }
    if (!anyMovable) {
      return;
    }
    frames.Clear();
    Walk(f.Body, State.Clean);
  }

  Frame FrameOf(Node target) {
    for (var i = frames.Count - 1; i >= 0; i--) {
      if (ReferenceEquals(frames[i].Target, target)) {
        return frames[i];
      }
    }
    throw new InternalError("ownership: a jump has no enclosing frame");
  }

  State Walk(List<Stmt> body, State s) {
    foreach (var st in body) {
      s = Stmt(st, s);
    }
    return s;
  }

  State Stmt(Stmt st, State s) {
    switch (st) {
      case LetStmt l:
        return Move(l.Init, l.Name, Expr(l.Init, s));

      case AssignStmt a:
        s = Expr(a.Value, s);
        if (a.Target is NameExpr { Kind: NameKind.Local, Local: var target }) {
          return Move(a.Value, target!.Name, s.Without(target));
        }
        return a.Target is MemberExpr member ? Expr(member.Obj, s) : s;

      case DiscardStmt d:
        return Expr(d.Value, s);

      case ExprStmt e:
        s = Expr(e.E, s);
        return e.E is PanicExpr ? State.Unreachable : s;

      case IfStmt i: {
        s = Expr(i.Cond, s);
        var a = Walk(i.Then, s);
        var b = i.Else is null ? s : Walk(i.Else, s);
        return State.Merge(a, b);
      }

      case WhileStmt w:
        return Loop(w, s, w.Body, w.Cond);

      case ForStmt f:
        return Loop(f, Expr(f.To, Expr(f.From, s)), f.Body, null);

      case MatchStmt m:
        return Match(m.Match, s);

      case ReturnStmt r:
        if (r.Value is not null) {
          Expr(r.Value, s);
        }
        return State.Unreachable;

      case BreakStmt b: {
        var frame = FrameOf(b.Target!);
        frame.Breaks = State.Merge(frame.Breaks, s);
        return State.Unreachable;
      }

      case ContinueStmt c: {
        var frame = FrameOf(c.Target!);
        frame.Continues = State.Merge(frame.Continues, s);
        return State.Unreachable;
      }

      default:
        throw new InternalError($"ownership: unhandled statement {st.GetType().Name}");
    }
  }

  State Loop(Node loop, State s, List<Stmt> body, Expr? cond) {
    var frame = new Frame(loop);
    frames.Add(frame);
    var entry = cond is null ? s : Expr(cond, s);
    var end = State.Unreachable;
    for (var pass = 0; pass < 2; pass++) {
      end = Walk(body, entry);
      entry = State.Merge(State.Merge(entry, end), frame.Continues);
      if (cond is not null) {
        entry = Expr(cond, entry);
      }
    }
    frames.RemoveAt(frames.Count - 1);
    return State.Merge(State.Merge(entry, end), frame.Breaks);
  }

  State Match(MatchCore m, State s) {
    s = Expr(m.Subject, s);
    var frame = new Frame(m);
    frames.Add(frame);
    var result = State.Unreachable;
    foreach (var arm in m.Arms) {
      var a = s;
      if (arm.Action is not null) {
        a = Stmt(arm.Action, a);
      } else if (arm.Value is not null) {
        a = Expr(arm.Value, a);
        if (arm.Kind == ArmKind.Panic) {
          a = State.Unreachable;
        }
      }
      result = State.Merge(result, a);
    }
    frames.RemoveAt(frames.Count - 1);
    return State.Merge(result, frame.Breaks);
  }

  State Move(Expr value, string into, State s) {
    if (s.Dead || value is not NameExpr { Kind: NameKind.Local, Local: var l } || !IsMovable(l!)) {
      return s;
    }
    moveTargets[value] = into;
    return s.With(l!, value);
  }

  State Read(NameExpr n, State s) {
    var l = n.Local!;
    if (s.Dead || !s.TryGetMove(l, out var site)) {
      return s;
    }
    var into = moveTargets.GetValueOrDefault(site, "another variable");
    errors.Add(n.Error(Err.UseAfterMove,
      $"'{n.Name}' was moved into '{into}' at line {site.Line}; assign it again before reading it, or bind a clone"));
    return s.Without(l);
  }

  State Expr(Expr e, State s) {
    switch (e) {
      case IntLit or BoolLit or SelfExpr:
        return s;
      case StrLit str:
        foreach (var part in str.Parts) {
          if (part is Expr pe) {
            s = Expr(pe, s);
          }
        }
        return s;
      case NameExpr n:
        return n.Kind == NameKind.Local ? Read(n, s) : s;
      case BinaryExpr b:
        return Expr(b.R, Expr(b.L, s));
      case UnaryExpr u:
        return Expr(u.E, s);
      case CondExpr c:
        return Expr(c.Else, Expr(c.Then, Expr(c.Cond, s)));
      case CallExpr c:
        if (c.Receiver is not null) {
          s = Expr(c.Receiver, s);
        }
        foreach (var arg in c.Ordered) {
          s = Expr(arg, s);
        }
        return s;
      case MemberExpr m:
        return m.Kind is MemberKind.EnumCase or MemberKind.UnionCase ? s : Expr(m.Obj, s);
      case StructLitExpr sl:
        foreach (var v in sl.Ordered) {
          if (v is not null) {
            s = Expr(v, s);
          }
        }
        return s;
      case CastExpr c:
        return Expr(c.E, s);
      case MatchExpr m:
        return Match(m.Match, s);
      case PanicExpr p:
        return Expr(p.Message, s);
      default:
        throw new InternalError($"ownership: unhandled expression {e.GetType().Name}");
    }
  }
}
