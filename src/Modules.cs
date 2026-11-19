using System.Collections.Concurrent;

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
  public List<FuncDecl> Functions { get; } = [];
  public List<PendingCall> Pending { get; } = [];
  public List<IrFunction> Ir { get; set; } = [];
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
    var found = new ConcurrentBag<CompileError>();
    project.AddDirectory(project.Root, found);
    errors.AddRange(found);
    project.CollectModules(project.Root);
    return project;
  }

  static bool IsQualifier(string name) =>
    name.Length > 0 && (char.IsAsciiLetter(name[0]) || name[0] == '_') && name.All(c => char.IsAsciiLetterOrDigit(c) || c == '_')
    && !Parser.IsReserved(name);

  bool AddDirectory(Module m, ConcurrentBag<CompileError> errors) {
    m.Files.AddRange(Directory.GetFiles(m.Dir, "*.maxon").Order(StringComparer.Ordinal));
    var subs = Directory.GetDirectories(m.Dir)
      .Where(sub => Path.GetFileName(sub) is var name && !name.StartsWith('.') && name is not ("bin" or "obj"))
      .Order(StringComparer.Ordinal).ToArray();
    var children = new Module?[subs.Length];

    Parallel.For(0, subs.Length, i => {
      var name = Path.GetFileName(subs[i]);
      var child = new Module(m.Path == "" ? name : $"{m.Path}.{name}", subs[i]);
      if (!AddDirectory(child, errors)) {
        return;
      }
      if (!IsQualifier(name) || (m == Root && ReservedTopLevel.Contains(name))) {
        errors.Add(new CompileError("E3182", $"the directory '{name}' cannot be written as a qualifier; rename it to an identifier that is not reserved", subs[i], 1, 1));
        return;
      }
      children[i] = child;
    });

    foreach (var child in children.OfType<Module>()) {
      m.Children[System.IO.Path.GetFileName(child.Dir)] = child;
    }
    return m.Files.Count > 0 || m.Children.Count > 0;
  }

  void CollectModules(Module m) {
    Modules.Add(m);
    foreach (var child in m.Children.Values) {
      CollectModules(child);
    }
  }

  public void BuildTables(ParallelOptions options) {
    Driver.ForEach(Modules, options, m => {
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
          m.Errors.Add(d.Error(Err.DuplicateDefinition, $"'{d.Name}' is already declared in {m.Display} at {kept.File}:{kept.Line}"));
        }
      }
    });

    var groups = Modules.AsParallel().AsOrdered().WithDegreeOfParallelism(options.MaxDegreeOfParallelism)
      .SelectMany(m => m.Names.Values.OrderBy(d => d.File, StringComparer.Ordinal).ThenBy(d => d.Line))
      .GroupBy(d => d.Name)
      .ToDictionary(g => g.Key, g => g.ToList());
    foreach (var (name, decls) in groups) {
      ByName[name] = decls;
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
