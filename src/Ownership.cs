namespace MiniMaxon;

public sealed class Ownership(List<CompileError> errors) {
  sealed class State {
    public Dictionary<Local, Node> Moved { get; private init; } = [];
    public bool Dead { get; private init; }

    public static readonly State Unreachable = new() { Dead = true };

    public State Clone() => Dead ? this : new State { Moved = new Dictionary<Local, Node>(Moved) };

    public static State Merge(State a, State b) {
      if (a.Dead) {
        return b.Clone();
      }
      if (b.Dead) {
        return a.Clone();
      }
      var m = new Dictionary<Local, Node>(a.Moved);
      foreach (var (k, v) in b.Moved) {
        m.TryAdd(k, v);
      }
      return new State { Moved = m };
    }

    public static State MergeAll(IEnumerable<State> states) => states.Aggregate(Unreachable, Merge);
  }

  sealed class Exits {
    public List<State> Breaks { get; } = [];
    public List<State> Continues { get; } = [];
  }

  Dictionary<Node, Exits> exits = [];

  public void Check(FuncDecl f) {
    exits = [];
    Walk(f.Body, new State());
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
        Expr(l.Init, s);
        Move(l.Init, l.Name, s);
        return s;

      case AssignStmt a:
        Expr(a.Value, s);
        if (a.Target is NameExpr { Kind: NameKind.Local, Local: var target }) {
          Move(a.Value, target!.Name, s);
          if (!s.Dead) {
            s.Moved.Remove(target);
          }
        } else if (a.Target is MemberExpr m) {
          Expr(m.Obj, s);
        }
        return s;

      case DiscardStmt d:
        Expr(d.Value, s);
        return s;

      case ExprStmt e:
        Expr(e.E, s);
        return e.E is PanicExpr ? State.Unreachable : s;

      case IfStmt i: {
        Expr(i.Cond, s);
        var a = Walk(i.Then, s.Clone());
        var b = i.Else is null ? s : Walk(i.Else, s.Clone());
        return State.Merge(a, b);
      }

      case WhileStmt w:
        return Loop(w, s, w.Body, entry => Expr(w.Cond, entry));

      case ForStmt f:
        Expr(f.From, s);
        Expr(f.To, s);
        return Loop(f, s, f.Body, _ => { });

      case MatchStmt m:
        return Match(m.Match, s);

      case ReturnStmt r:
        if (r.Value is not null) {
          Expr(r.Value, s);
        }
        return State.Unreachable;

      case BreakStmt b:
        exits[b.Target!].Breaks.Add(s.Clone());
        return State.Unreachable;

      case ContinueStmt c:
        exits[c.Target!].Continues.Add(s.Clone());
        return State.Unreachable;

      default:
        throw new InternalError($"ownership: unhandled statement {st.GetType().Name}");
    }
  }

  State Loop(Node loop, State s, List<Stmt> body, Action<State> header) {
    var ex = new Exits();
    exits[loop] = ex;
    header(s);
    var entry = s.Clone();
    var end = State.Unreachable;
    for (var pass = 0; pass < 2; pass++) {
      end = Walk(body, entry.Clone());
      entry = State.MergeAll([entry, end, .. ex.Continues]);
      header(entry);
    }
    return State.MergeAll([entry, end, .. ex.Breaks]);
  }

  State Match(MatchCore m, State s) {
    Expr(m.Subject, s);
    var ex = new Exits();
    exits[m] = ex;
    var results = new List<State>();
    foreach (var arm in m.Arms) {
      var a = s.Clone();
      if (arm.Action is not null) {
        a = Stmt(arm.Action, a);
      } else if (arm.Value is not null) {
        Expr(arm.Value, a);
        if (arm.Kind == ArmKind.Panic) {
          a = State.Unreachable;
        }
      }
      results.Add(a);
    }
    return State.MergeAll([.. results, .. ex.Breaks]);
  }

  void Move(Expr value, string into, State s) {
    if (s.Dead || value is not NameExpr { Kind: NameKind.Local, Local: var l }) {
      return;
    }
    if (l!.IsVar && !l.IsParam && l.Type is RecordTy or UnionTy) {
      s.Moved[l] = value;
      moveTargets[value] = into;
    }
  }

  readonly Dictionary<Node, string> moveTargets = [];

  void Read(NameExpr n, State s) {
    var l = n.Local!;
    if (!s.Dead && s.Moved.TryGetValue(l, out var site)) {
      var into = moveTargets.GetValueOrDefault(site, "another variable");
      errors.Add(n.Error(Err.UseAfterMove,
        $"'{n.Name}' was moved into '{into}' at line {site.Line}; assign it again before reading it, or bind a clone"));
      s.Moved.Remove(l);
    }
  }

  void Expr(Expr e, State s) {
    switch (e) {
      case IntLit or BoolLit or SelfExpr:
        break;
      case StrLit str:
        foreach (var part in str.Parts.OfType<Expr>()) {
          Expr(part, s);
        }
        break;
      case NameExpr n:
        if (n.Kind == NameKind.Local) {
          Read(n, s);
        }
        break;
      case BinaryExpr b:
        Expr(b.L, s);
        Expr(b.R, s);
        break;
      case UnaryExpr u:
        Expr(u.E, s);
        break;
      case CondExpr c:
        Expr(c.Cond, s);
        Expr(c.Then, s);
        Expr(c.Else, s);
        break;
      case CallExpr c:
        if (c.Receiver is not null) {
          Expr(c.Receiver, s);
        }
        foreach (var arg in c.Ordered) {
          Expr(arg, s);
        }
        break;
      case MemberExpr m:
        if (m.Kind is not (MemberKind.EnumCase or MemberKind.UnionCase)) {
          Expr(m.Obj, s);
        }
        break;
      case StructLitExpr sl:
        foreach (var v in sl.Ordered.OfType<Expr>()) {
          Expr(v, s);
        }
        break;
      case CastExpr c:
        Expr(c.E, s);
        break;
      case MatchExpr m:
        Match(m.Match, s);
        break;
      case PanicExpr p:
        Expr(p.Message, s);
        break;
      default:
        throw new InternalError($"ownership: unhandled expression {e.GetType().Name}");
    }
  }
}
