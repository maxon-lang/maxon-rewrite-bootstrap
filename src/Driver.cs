using System.Collections.Concurrent;

namespace MiniMaxon;

public static class Driver {
  public static int DefaultJobs => Math.Max(1, Environment.ProcessorCount - 1);

  public static List<CompileError> Compile(string path, string? output, int jobs) {
    var errors = new List<CompileError>();
    var project = Project.Discover(path, errors);
    if (errors.Count > 0) {
      return errors;
    }
    var modules = project.Modules;
    var options = new ParallelOptions { MaxDegreeOfParallelism = jobs };

    void Phase(Action<Module> work) => Parallel.ForEach(modules, options, work);

    List<CompileError>? Failed() {
      var all = errors.Concat(modules.SelectMany(m => m.Errors)).ToList();
      return all.Count == 0 ? null : Sorted(all);
    }

    var parsed = new ConcurrentDictionary<string, Program>();
    Parallel.ForEach(modules.SelectMany(m => m.Files.Select(file => (m, file))), options, item => {
      try {
        parsed[item.file] = Parser.ParseFile(item.file, File.ReadAllText(item.file));
      } catch (CompileError e) {
        lock (item.m.Errors) {
          item.m.Errors.Add(e);
        }
      }
    });
    if (Failed() is { } parseErrors) {
      return parseErrors;
    }
    foreach (var m in modules) {
      foreach (var file in m.Files) {
        m.Decls.AddRange(parsed[file].Decls);
      }
    }

    project.BuildTables(errors);
    if (Failed() is { } tableErrors) {
      return tableErrors;
    }

    Phase(m => new Resolver(project, m).Run());
    if (Failed() is { } resolveErrors) {
      return resolveErrors;
    }
    if (project.DependencyOrder(errors) is null) {
      return Sorted(errors);
    }

    foreach (var m in modules) {
      m.Checker = new Checker(project, m);
    }
    Phase(m => m.Checker!.Declare());
    if (Failed() is { } declareErrors) {
      return declareErrors;
    }
    Checker.CheckConsts(project);
    if (Failed() is { } constErrors) {
      return constErrors;
    }

    Phase(m => m.Checker!.Bodies());
    if (Failed() is { } bodyErrors) {
      return bodyErrors;
    }

    Phase(m => {
      m.Checker!.Verify();
      if (m.Errors.Count == 0) {
        var ownership = new Ownership(m.Errors);
        foreach (var f in m.Checker.Functions) {
          ownership.Check(f);
        }
      }
    });
    if (Failed() is { } verifyErrors) {
      return verifyErrors;
    }

    if (output is not null) {
      Phase(m => m.Unit = Codegen.EmitModule(m.Checker!.Functions));
      var units = new List<CodeUnit> { Codegen.EmitRuntimeUnit(project.Root.Checker!.Main!) };
      units.AddRange(modules.Select(m => m.Unit!));
      Linker.Link(output, units, Rt.Entry);
    }
    return [];
  }

  static List<CompileError> Sorted(IEnumerable<CompileError> errors) =>
    [.. errors.OrderBy(e => e.File, StringComparer.Ordinal).ThenBy(e => e.Line).ThenBy(e => e.Col).ThenBy(e => e.Code, StringComparer.Ordinal)];
}
