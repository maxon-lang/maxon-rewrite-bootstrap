using System.Collections.Concurrent;

namespace MiniMaxon;

public static class Driver {
  public static int DefaultJobs => Math.Max(1, Environment.ProcessorCount - 1);

  [ThreadStatic]
  static List<Token>? tokenBuffer;

  public static List<CompileError> Compile(string path, string? output, int jobs, bool timings = false) {
    var errors = new List<CompileError>();
    var clock = System.Diagnostics.Stopwatch.StartNew();
    var allocated = GC.GetTotalAllocatedBytes(precise: true);
    var collections = GC.CollectionCount(0);
    void Mark(string phase) {
      if (timings) {
        var nowAllocated = GC.GetTotalAllocatedBytes(precise: true);
        var nowCollections = GC.CollectionCount(0);
        Console.Error.WriteLine($"{phase,-10} {clock.Elapsed.TotalMilliseconds,8:F1} ms {(nowAllocated - allocated) / 1048576.0,8:F1} MB {nowCollections - collections,4} GCs");
        allocated = nowAllocated;
        collections = nowCollections;
      }
      clock.Restart();
    }
    var options = new ParallelOptions { MaxDegreeOfParallelism = jobs };
    var project = Project.Discover(path, errors);
    Mark("discover");
    if (errors.Count > 0) {
      return Sorted(errors);
    }
    var modules = project.Modules;

    void Phase(Action<Module> work) => ForEach(modules, options, work);

    List<CompileError>? Failed() {
      var all = errors.Concat(modules.SelectMany(m => m.Errors)).ToList();
      return all.Count == 0 ? null : Sorted(all);
    }

    var parsed = new ConcurrentDictionary<string, Program>();
    long readBytes = 0, lexBytes = 0, parseBytes = 0;
    ForEach([.. modules.SelectMany(m => m.Files.Select(file => (m, file)))], options, item => {
      try {
        var a0 = GC.GetAllocatedBytesForCurrentThread();
        var text = File.ReadAllText(item.file);
        var a1 = GC.GetAllocatedBytesForCurrentThread();
        var tokens = new Lexer(item.file, text, buffer: tokenBuffer ??= []).Lex();
        var a2 = GC.GetAllocatedBytesForCurrentThread();
        parsed[item.file] = new Parser(tokens, item.file).ParseProgram();
        var a3 = GC.GetAllocatedBytesForCurrentThread();
        Interlocked.Add(ref readBytes, a1 - a0);
        Interlocked.Add(ref lexBytes, a2 - a1);
        Interlocked.Add(ref parseBytes, a3 - a2);
      } catch (CompileError e) {
        lock (item.m.Errors) {
          item.m.Errors.Add(e);
        }
      }
    });
    if (timings) {
      Console.Error.WriteLine($"  read {readBytes / 1048576.0:F1} MB, lex {lexBytes / 1048576.0:F1} MB, parse {parseBytes / 1048576.0:F1} MB");
    }
    Mark("parse");
    if (Failed() is { } parseErrors) {
      return parseErrors;
    }
    Phase(m => {
      foreach (var file in m.Files) {
        m.Decls.AddRange(parsed[file].Decls);
      }
    });

    project.BuildTables(options);
    Mark("tables");
    if (Failed() is { } tableErrors) {
      return tableErrors;
    }

    Phase(m => new Resolver(project, m).Run());
    Mark("resolve");
    if (Failed() is { } resolveErrors) {
      return resolveErrors;
    }
    if (project.DependencyOrder(errors) is null) {
      return Sorted(errors);
    }
    Mark("cycles");

    FuncDecl? main = null;
    Phase(m => {
      var checker = new Checker(project, m, m.Errors);
      foreach (var d in m.Decls) {
        m.Functions.AddRange(checker.Declare(d));
      }
      if (m == project.Root) {
        main = checker.CheckMain();
      }
    });
    Mark("declare");
    if (Failed() is { } declareErrors) {
      return declareErrors;
    }

    CheckConstants(project, options, errors);
    Mark("constants");
    if (Failed() is { } constErrors) {
      return constErrors;
    }

    Phase(m => {
      var checker = new Checker(project, m, m.Errors);
      foreach (var d in m.Decls.Where(d => d is TypeDecl or UnionDecl)) {
        checker.CheckBody(d);
      }
      foreach (var f in m.Functions) {
        checker.CheckBody(f);
      }
      m.Pending.AddRange(checker.Pending);
    });
    Mark("bodies");
    if (Failed() is { } bodyErrors) {
      return bodyErrors;
    }

    Phase(m => {
      new Checker(project, m, m.Errors).Verify(m.Pending);
      if (m.Errors.Count == 0) {
        var ownership = new Ownership(m.Errors);
        foreach (var f in m.Functions) {
          ownership.Check(f);
        }
      }
    });
    Mark("verify");
    if (Failed() is { } verifyErrors) {
      return verifyErrors;
    }

    if (output is not null) {
      CodeUnit runtime = null!;
      Parallel.Invoke(options,
        () => runtime = Codegen.EmitRuntimeUnit(main!),
        () => Phase(m => m.Unit = Codegen.EmitModule(m.Functions)));
      Mark("codegen");
      Linker.Link(output, [runtime, .. modules.Select(m => m.Unit!)], Rt.Entry, options);
      Mark("link");
    }
    return [];
  }

  static void CheckConstants(Project project, ParallelOptions options, List<CompileError> errors) {
    var consts = project.Modules.SelectMany(m => m.Decls.OfType<ConstDecl>().Select(c => (Module: m, Const: c))).ToList();
    var deps = new List<ConstDecl>[consts.Count];
    Parallel.For(0, consts.Count, options, i => deps[i] = [.. ConstRefs(consts[i].Const.Value).Distinct()]);

    var index = consts.Select((c, i) => (c.Const, i)).ToDictionary(p => p.Const, p => p.i);
    var depth = new int[consts.Count];
    var state = new byte[consts.Count];
    const byte Visiting = 1;
    const byte Done = 2;

    int Depth(int i) {
      if (state[i] == Done) {
        return depth[i];
      }
      if (state[i] == Visiting) {
        throw consts[i].Const.Error("E2012", $"constant '{consts[i].Const.Name}' depends on itself");
      }
      state[i] = Visiting;
      depth[i] = deps[i].Select(d => Depth(index[d]) + 1).DefaultIfEmpty(0).Max();
      state[i] = Done;
      return depth[i];
    }

    for (var i = 0; i < consts.Count; i++) {
      try {
        Depth(i);
      } catch (CompileError e) {
        errors.Add(e);
        return;
      }
    }

    foreach (var wave in Enumerable.Range(0, consts.Count).GroupBy(i => depth[i]).OrderBy(g => g.Key)) {
      var items = wave.ToArray();
      var waveErrors = new List<CompileError>[items.Length];
      Parallel.For(0, items.Length, options, k => {
        var (m, c) = consts[items[k]];
        waveErrors[k] = [];
        new Checker(project, m, waveErrors[k]).CheckConst(c);
      });
      errors.AddRange(waveErrors.SelectMany(e => e));
    }
  }

  static IEnumerable<ConstDecl> ConstRefs(Expr e) => e switch {
    NameExpr { Target: ConstDecl c } => [c],
    BinaryExpr b => ConstRefs(b.L).Concat(ConstRefs(b.R)),
    UnaryExpr u => ConstRefs(u.E),
    CondExpr c => ConstRefs(c.Cond).Concat(ConstRefs(c.Then)).Concat(ConstRefs(c.Else)),
    CastExpr c => ConstRefs(c.E),
    MemberExpr m => ConstRefs(m.Obj),
    StrLit s => s.Parts.OfType<Expr>().SelectMany(ConstRefs),
    _ => [],
  };

  public static void ForEach<T>(IReadOnlyList<T> items, ParallelOptions options, Action<T> work) =>
    Parallel.For(0, items.Count, options, i => work(items[i]));

  static List<CompileError> Sorted(IEnumerable<CompileError> errors) =>
    [.. errors.OrderBy(e => e.File, StringComparer.Ordinal).ThenBy(e => e.Line).ThenBy(e => e.Col).ThenBy(e => e.Code, StringComparer.Ordinal)];
}
