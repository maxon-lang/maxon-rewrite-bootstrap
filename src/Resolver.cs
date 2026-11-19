namespace MiniMaxon;

public sealed class Resolver(Project project, Module module) {
  sealed record Lookup(Decl? Decl, CompileError? Error);

  static readonly HashSet<string> PrimitiveTypes = ["bool", "String", "Self", "int", "float"];

  List<HashSet<string>> scopes = [];
  HashSet<string> fields = [];

  public void Run() {
    foreach (var d in module.Decls) {
      try {
        ResolveDecl(d);
      } catch (CompileError e) {
        module.Errors.Add(e);
      }
    }
  }

  void ResolveDecl(Decl d) {
    switch (d) {
      case FuncDecl f:
        ResolveFunction(f, null);
        break;
      case TypeDecl t:
        foreach (var f in t.Fields) {
          if (f.Type is not null) {
            Type(f.Type);
          }
          if (f.Default is not null) {
            Enter([]);
            f.Default = E(f.Default);
          }
        }
        foreach (var m in t.Methods) {
          ResolveFunction(m, m.IsStatic ? null : t);
        }
        break;
      case EnumDecl e:
        foreach (var m in e.Methods) {
          ResolveFunction(m, null);
        }
        break;
      case UnionDecl u:
        foreach (var c in u.Cases) {
          foreach (var p in c.Fields) {
            Type(p.Type);
          }
        }
        foreach (var m in u.Methods) {
          ResolveFunction(m, null);
        }
        break;
      case ConstDecl c:
        Enter([]);
        c.Value = E(c.Value);
        break;
      case AliasDecl:
        break;
      default:
        throw new InternalError($"resolver: unhandled declaration {d.GetType().Name}");
    }
  }

  void Enter(HashSet<string> fieldNames) {
    scopes = [[]];
    fields = fieldNames;
  }

  void ResolveFunction(FuncDecl f, TypeDecl? instanceOf) {
    foreach (var p in f.Params) {
      Type(p.Type);
    }
    if (f.Returns is not null) {
      Type(f.Returns);
    }
    Enter(instanceOf is null ? [] : [.. instanceOf.Fields.Select(fd => fd.Name)]);
    foreach (var p in f.Params) {
      scopes[^1].Add(p.Name);
    }
    Block(f.Body);
  }

  bool IsBound(string name) => scopes.Any(s => s.Contains(name)) || fields.Contains(name);

  void Block(List<Stmt> body) {
    scopes.Add([]);
    foreach (var s in body) {
      try {
        Stmt(s);
      } catch (CompileError e) {
        module.Errors.Add(e);
      }
    }
    scopes.RemoveAt(scopes.Count - 1);
  }

  void Stmt(Stmt s) {
    switch (s) {
      case LetStmt l:
        l.Init = E(l.Init);
        scopes[^1].Add(l.Name);
        break;
      case AssignStmt a:
        a.Target = E(a.Target);
        a.Value = E(a.Value);
        break;
      case DiscardStmt d:
        d.Value = E(d.Value);
        break;
      case ExprStmt e:
        e.E = E(e.E);
        break;
      case IfStmt i:
        i.Cond = E(i.Cond);
        Block(i.Then);
        if (i.Else is not null) {
          Block(i.Else);
        }
        break;
      case WhileStmt w:
        w.Cond = E(w.Cond);
        Block(w.Body);
        break;
      case ForStmt f:
        f.From = E(f.From);
        f.To = E(f.To);
        scopes.Add([f.Var]);
        Block(f.Body);
        scopes.RemoveAt(scopes.Count - 1);
        break;
      case MatchStmt m:
        Match(m.Match);
        break;
      case ReturnStmt r:
        if (r.Value is not null) {
          r.Value = E(r.Value);
        }
        break;
      case BreakStmt or ContinueStmt:
        break;
      default:
        throw new InternalError($"resolver: unhandled statement {s.GetType().Name}");
    }
  }

  void Match(MatchCore m) {
    m.Subject = E(m.Subject);
    foreach (var arm in m.Arms) {
      scopes.Add([.. arm.Patterns.SelectMany(p => p.Bindings ?? [])]);
      if (arm.Action is not null) {
        Stmt(arm.Action);
      }
      if (arm.Value is not null) {
        arm.Value = E(arm.Value);
      }
      scopes.RemoveAt(scopes.Count - 1);
    }
  }

  Expr E(Expr e) {
    switch (e) {
      case IntLit or BoolLit or SelfExpr:
        return e;
      case NameExpr n:
        if (!IsBound(n.Name) && n.Name != "Self") {
          n.Target = Take(Bare(n.Name), n);
        }
        return n;
      case MemberExpr m:
        return Member(m);
      case StrLit s:
        for (var i = 0; i < s.Parts.Count; i++) {
          if (s.Parts[i] is Expr part) {
            s.Parts[i] = E(part);
          }
        }
        return s;
      case BinaryExpr b:
        b.L = E(b.L);
        b.R = E(b.R);
        return b;
      case UnaryExpr u:
        u.E = E(u.E);
        return u;
      case CondExpr c:
        c.Cond = E(c.Cond);
        c.Then = E(c.Then);
        c.Else = E(c.Else);
        return c;
      case CallExpr c:
        c.Callee = E(c.Callee);
        foreach (var a in c.Args) {
          a.Value = E(a.Value);
        }
        return c;
      case StructLitExpr s:
        if (s.TypeName != "Self") {
          s.Target = Take(Bare(s.TypeName), s);
        }
        s.Fields = [.. s.Fields.Select(f => (f.Name, E(f.Value), f.Site))];
        return s;
      case CastExpr c:
        c.E = E(c.E);
        Type(c.Target);
        return c;
      case MatchExpr m:
        Match(m.Match);
        return m;
      case PanicExpr p:
        p.Message = E(p.Message);
        return p;
      default:
        throw new InternalError($"resolver: unhandled expression {e.GetType().Name}");
    }
  }

  static bool IsType(Decl d) => d is TypeDeclBase;

  static bool DeclaresMember(Decl d, string name) => d switch {
    TypeDecl t => t.Methods.Any(m => m.IsStatic && m.Name == name),
    EnumDecl e => e.Cases.Any(c => c.Name == name),
    UnionDecl u => u.Cases.Any(c => c.Name == name),
    _ => false,
  };

  bool IsModuleRoot(string name) => name is "export" or "stdlib" || project.Root.Children.ContainsKey(name);

  Expr Member(MemberExpr m) {
    var segments = new List<(string Name, Node Site)>();
    Expr cur = m;
    while (cur is MemberExpr mm) {
      segments.Insert(0, (mm.Name, mm));
      cur = mm.Obj;
    }
    if (cur is not NameExpr head || IsBound(head.Name) || head.Name == "Self") {
      m.Obj = E(m.Obj);
      return m;
    }

    var bare = Bare(head.Name);
    var decl = bare.Decl;
    if (decl is not null && (decl is ConstDecl || (IsType(decl) && DeclaresMember(decl, segments[0].Name)) || !IsModuleRoot(head.Name))) {
      head.Target = Take(bare, head);
      return m;
    }
    if (!IsModuleRoot(head.Name)) {
      return m;
    }

    var (target, used) = Qualified(head.Name, head, [.. segments.Select(s => s.Name)], [.. segments.Select(s => s.Site)]);
    Expr result = new NameExpr { Name = target.Name, Target = target, File = head.File, Line = head.Line, Col = head.Col };
    foreach (var (name, site) in segments.Skip(used)) {
      result = new MemberExpr { Obj = result, Name = name, File = site.File, Line = site.Line, Col = site.Col };
    }
    return result;
  }

  (Decl Decl, int Used) Qualified(string head, Node headSite, List<string> rest, List<Node> sites) {
    var mod = head switch {
      "export" => project.Root,
      "stdlib" => project.Stdlib,
      _ => project.Root.Children[head],
    };
    var i = 0;
    while (i < rest.Count && !mod.Names.ContainsKey(rest[i]) && mod.Children.TryGetValue(rest[i], out var child)) {
      mod = child;
      i++;
    }
    if (i == rest.Count) {
      throw headSite.Error(Err.ExpressionError, $"'{head}.{string.Join('.', rest)}' is a module, not a value");
    }
    if (!mod.Names.TryGetValue(rest[i], out var decl)) {
      throw sites[i].Error("E2073", $"module {mod.Display} declares no '{rest[i]}'");
    }
    return (Take(new Lookup(decl, Visible(decl) ? null : Hidden(decl, sites[i])), sites[i])!, i + 1);
  }

  void Type(TypeRef tr) {
    if (tr.Qualifier.Count == 0) {
      if (PrimitiveTypes.Contains(tr.Name)) {
        return;
      }
      var found = Take(Bare(tr.Name), tr);
      if (found is not null && !IsType(found)) {
        throw tr.Error(Err.UnknownType, $"'{tr.Name}' is not a type");
      }
      tr.Target = found;
      return;
    }
    if (!IsModuleRoot(tr.Qualifier[0])) {
      throw tr.Error(Err.UnknownType, $"no module '{tr.Qualifier[0]}'");
    }
    var path = tr.Qualifier.Skip(1).Append(tr.Name).ToList();
    var (decl, used) = Qualified(tr.Qualifier[0], tr, path, [.. path.Select(_ => (Node)tr)]);
    if (used != path.Count || !IsType(decl)) {
      throw tr.Error(Err.UnknownType, $"'{string.Join('.', tr.Qualifier)}.{tr.Name}' is not a type");
    }
    tr.Target = decl;
  }

  bool Visible(Decl d) => d.Module == module || d.IsExported;

  static CompileError Hidden(Decl d, Node site) =>
    site.Error("E3008", $"'{d.Name}' is not exported from module {d.Module.Display}");

  Decl? Take(Lookup found, Node site) {
    if (found.Error is not null) {
      throw new CompileError(found.Error.Code, found.Error.Message, site.File, site.Line, site.Col);
    }
    if (found.Decl is { } d && d.Module != module && !d.Module.IsStdlib) {
      module.DepSites.TryAdd(d.Module, site);
    }
    return found.Decl;
  }

  Lookup Bare(string name) {
    if (module.Names.TryGetValue(name, out var own)) {
      return new Lookup(own, null);
    }
    if (project.Root.Names.TryGetValue(name, out var root)) {
      return new Lookup(root, Visible(root) ? null : Hidden(root, root));
    }
    var candidates = project.ByName.GetValueOrDefault(name, []).Where(d => d.Module != module && d.Module != project.Root).ToList();
    var visible = candidates.Where(Visible).ToList();
    if (visible.Count == 1) {
      return new Lookup(visible[0], null);
    }
    if (visible.Count > 1) {
      var code = IsType(visible[0]) ? "E3063" : "E3095";
      var where = string.Join(", ", visible.Select(d => $"{d.Module.Path}.{name}"));
      return new Lookup(null, new CompileError(code, $"'{name}' is ambiguous: qualify it as one of {where}", "", 0, 0));
    }
    if (candidates.Count > 0) {
      return new Lookup(null, Hidden(candidates[0], candidates[0]));
    }
    return new Lookup(project.Stdlib.Names.GetValueOrDefault(name), null);
  }
}
