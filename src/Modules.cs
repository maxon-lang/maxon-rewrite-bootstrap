namespace MiniMaxon;

public sealed class Module(string path, string dir, bool isStdlib = false) {
  public string Path { get; } = path;
  public string Dir { get; } = dir;
  public bool IsStdlib { get; } = isStdlib;
  public List<string> Files { get; } = [];
  public List<Decl> Decls { get; } = [];
  public Dictionary<string, Decl> Names { get; } = [];
  public SortedDictionary<string, Module> Children { get; } = new(StringComparer.Ordinal);
  public Dictionary<Module, Node> DepSites { get; } = [];
  public List<CompileError> Errors { get; } = [];
  public Checker? Checker { get; set; }
  public CodeUnit? Unit { get; set; }

  public string Display => IsStdlib ? "stdlib" : Path == "" ? "the project root" : Path;

  public override string ToString() => Display;
}

public sealed class Project {
  static readonly HashSet<string> ReservedTopLevel = ["export", "stdlib", "runtime", "scriptlib"];

  public Module Root { get; }
  public Module Stdlib { get; } = new("stdlib", "", isStdlib: true);
  public List<Module> Modules { get; } = [];
  public Dictionary<string, List<Decl>> ByName { get; } = [];

  Project(Module root) {
    Root = root;
    foreach (var (name, min, max) in new[] {
      ("ExitCode", (Int128)0, (Int128)uint.MaxValue),
      ("Count", 0, IntTy.U64Max),
      ("FullRangeInt", IntTy.I64Min, IntTy.I64Max),
    }) {
      var alias = new AliasDecl { Name = name, Visibility = "public", Min = min, Max = max, Module = Stdlib, File = "<stdlib>" };
      alias.Ty = new IntTy(name, min, max);
      Stdlib.Decls.Add(alias);
      Stdlib.Names[name] = alias;
    }
  }

  public static Project Discover(string path, List<CompileError> errors) {
    if (File.Exists(path)) {
      var root = new Module("", Path.GetDirectoryName(Path.GetFullPath(path))!);
      root.Files.Add(path);
      var single = new Project(root);
      single.Modules.Add(root);
      return single;
    }
    if (!Directory.Exists(path)) {
      throw new CompileError(Err.UnexpectedToken, "no such file or directory", path, 1, 1);
    }

    var project = new Project(new Module("", path));
    project.AddDirectory(project.Root, errors);
    project.Modules.Sort((a, b) => string.CompareOrdinal(a.Path, b.Path));
    return project;
  }

  static bool IsQualifier(string name) =>
    name.Length > 0 && (char.IsAsciiLetter(name[0]) || name[0] == '_') && name.All(c => char.IsAsciiLetterOrDigit(c) || c == '_')
    && !Parser.IsReserved(name);

  bool AddDirectory(Module m, List<CompileError> errors) {
    m.Files.AddRange(Directory.GetFiles(m.Dir, "*.maxon").Order(StringComparer.Ordinal));
    var hasSource = m.Files.Count > 0;
    foreach (var sub in Directory.GetDirectories(m.Dir).Order(StringComparer.Ordinal)) {
      var name = Path.GetFileName(sub);
      if (name.StartsWith('.') || name is "bin" or "obj") {
        continue;
      }
      var child = new Module(m.Path == "" ? name : $"{m.Path}.{name}", sub);
      if (!AddDirectory(child, errors)) {
        continue;
      }
      if (!IsQualifier(name) || (m == Root && ReservedTopLevel.Contains(name))) {
        errors.Add(new CompileError("E3182", $"the directory '{name}' cannot be written as a qualifier; rename it to an identifier that is not reserved", sub, 1, 1));
        continue;
      }
      m.Children[name] = child;
      hasSource = true;
    }
    Modules.Add(m);
    return hasSource;
  }

  public void BuildTables(List<CompileError> errors) {
    foreach (var m in Modules) {
      foreach (var d in m.Decls) {
        d.Module = m;
        foreach (var method in Methods(d)) {
          method.Module = m;
        }
        if (d is TypeDeclBase tb) {
          tb.Ty = Checker.Shell(tb);
        }
        if (!m.Names.TryAdd(d.Name, d)) {
          var kept = m.Names[d.Name];
          errors.Add(d.Error(Err.DuplicateDefinition, $"'{d.Name}' is already declared in {m.Display} at {kept.File}:{kept.Line}"));
          continue;
        }
        if (!ByName.TryGetValue(d.Name, out var list)) {
          ByName[d.Name] = list = [];
        }
        list.Add(d);
      }
    }
  }

  public static IEnumerable<FuncDecl> Methods(Decl d) => d switch {
    TypeDecl t => t.Methods,
    EnumDecl e => e.Methods,
    UnionDecl u => u.Methods,
    _ => [],
  };

  public List<Module>? DependencyOrder(List<CompileError> errors) {
    var ordered = new List<Module>();
    var remaining = new SortedSet<Module>(Modules, Comparer<Module>.Create((a, b) => string.CompareOrdinal(a.Path, b.Path)));
    while (remaining.Count > 0) {
      var ready = remaining.FirstOrDefault(m => m.DepSites.Keys.All(d => !remaining.Contains(d)));
      if (ready is null) {
        errors.Add(CycleError(remaining));
        return null;
      }
      ordered.Add(ready);
      remaining.Remove(ready);
    }
    return ordered;
  }

  static CompileError CycleError(SortedSet<Module> remaining) {
    var path = new List<Module> { remaining.Min! };
    while (true) {
      var next = path[^1].DepSites.Keys.Where(remaining.Contains).OrderBy(m => m.Path, StringComparer.Ordinal).First();
      var at = path.IndexOf(next);
      if (at >= 0) {
        var cycle = path.Skip(at).Append(next).ToList();
        var site = cycle[0].DepSites[cycle[1]];
        return site.Error("E9001", $"modules depend on each other in a cycle: {string.Join(" -> ", cycle.Select(m => m.Display))}");
      }
      path.Add(next);
    }
  }
}
