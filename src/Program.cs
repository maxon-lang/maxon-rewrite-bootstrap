using System.Diagnostics;

namespace MiniMaxon;

public static class Cli {
  const string Usage = """
    usage:
      minimaxon build <file.maxon|dir> [-o out.exe]   compile to a Windows x64 executable
      minimaxon run <file.maxon|dir>                  compile and run
      minimaxon check <file.maxon|dir>                report diagnostics only
      minimaxon test [dir]                            run the test corpus (default: tests/)
    """;

  public static int Main(string[] args) {
    if (args.Length == 0) {
      Console.Error.WriteLine(Usage);
      return 2;
    }

    switch (args[0]) {
      case "build" when args.Length >= 2: {
        var output = args.Length >= 4 && args[2] == "-o" ? args[3] : Path.ChangeExtension(args[1].TrimEnd('/', '\\'), ".exe");
        return Report(Compile(args[1], output)) ? 0 : 1;
      }
      case "check" when args.Length >= 2:
        return Report(Compile(args[1], null)) ? 0 : 1;
      case "run" when args.Length >= 2: {
        var exe = TempExe(args[1]);
        if (!Report(Compile(args[1], exe))) {
          return 1;
        }
        using var proc = Process.Start(new ProcessStartInfo(exe) { UseShellExecute = false })!;
        proc.WaitForExit();
        return proc.ExitCode;
      }
      case "test":
        return RunTests(args.Length >= 2 ? args[1] : Path.Combine(AppContext.BaseDirectory, "..", "..", "..", "tests"));
      default:
        Console.Error.WriteLine(Usage);
        return 2;
    }
  }

  static string TempExe(string source) {
    var dir = Path.Combine(Path.GetTempPath(), "minimaxon");
    Directory.CreateDirectory(dir);
    return Path.Combine(dir, Path.GetFileNameWithoutExtension(source.TrimEnd('/', '\\')) + ".exe");
  }

  static bool Report(List<CompileError> errors) {
    foreach (var e in errors) {
      Console.Error.WriteLine(e);
    }
    return errors.Count == 0;
  }

  public static List<CompileError> Compile(string path, string? output) {
    var files = Directory.Exists(path)
      ? Directory.GetFiles(path, "*.maxon").Order(StringComparer.Ordinal).ToArray()
      : [path];

    var program = new Program();
    var errors = new List<CompileError>();
    foreach (var file in files) {
      try {
        program.Decls.AddRange(Parser.ParseFile(file, File.ReadAllText(file)).Decls);
      } catch (CompileError e) {
        errors.Add(e);
      }
    }
    if (errors.Count > 0) {
      return errors;
    }

    var checker = new Checker();
    checker.Check(program);
    errors.AddRange(checker.Errors);
    if (errors.Count == 0) {
      var ownership = new Ownership();
      foreach (var f in checker.Functions) {
        ownership.Check(f);
      }
      errors.AddRange(ownership.Errors);
    }

    errors = [.. errors.OrderBy(e => e.File, StringComparer.Ordinal).ThenBy(e => e.Line).ThenBy(e => e.Col)];
    if (errors.Count == 0 && output is not null) {
      Codegen.Compile(checker, output);
    }
    return errors;
  }

  sealed record Expectation(string? Error, List<string> Absent, int Exit, string Stdout);

  static Expectation ReadExpectation(string file) {
    string? error = null;
    var absent = new List<string>();
    var exit = 0;
    var stdout = new System.Text.StringBuilder();
    foreach (var raw in File.ReadLines(file)) {
      var line = raw.TrimStart().TrimEnd('\r');
      if (line.StartsWith("// expect-error:", StringComparison.Ordinal)) {
        error = line["// expect-error:".Length..].Trim();
      } else if (line.StartsWith("// expect-no-error:", StringComparison.Ordinal)) {
        absent.Add(line["// expect-no-error:".Length..].Trim());
      } else if (line.StartsWith("// expect-exit:", StringComparison.Ordinal)) {
        exit = int.Parse(line["// expect-exit:".Length..].Trim());
      } else if (line.StartsWith("//>", StringComparison.Ordinal)) {
        stdout.Append(line.Length > 3 && line[3] == ' ' ? line[4..] : line[3..]).Append('\n');
      }
    }
    return new Expectation(error, absent, exit, stdout.ToString());
  }

  static int RunTests(string dir) {
    dir = Path.GetFullPath(dir);
    var files = Directory.GetFiles(dir, "*.maxon", SearchOption.AllDirectories).Order(StringComparer.Ordinal).ToList();
    var failures = new List<string>();

    foreach (var file in files) {
      var name = Path.GetRelativePath(dir, file);
      var expect = ReadExpectation(file);
      var failure = RunTest(file, expect);
      Console.WriteLine($"{(failure is null ? "PASS" : "FAIL")}  {name}");
      if (failure is not null) {
        failures.Add($"{name}: {failure}");
      }
    }

    Console.WriteLine();
    foreach (var f in failures) {
      Console.WriteLine(f);
    }
    Console.WriteLine($"{files.Count - failures.Count}/{files.Count} passed");
    return failures.Count == 0 ? 0 : 1;
  }

  static string? RunTest(string file, Expectation expect) {
    var exe = TempExe(file);
    List<CompileError> errors;
    try {
      errors = Compile(file, expect.Error is null ? exe : null);
    } catch (Exception e) when (e is not CompileError) {
      return $"compiler crashed: {e}";
    }

    if (errors.FirstOrDefault(e => expect.Absent.Contains(e.Code)) is { } unwanted) {
      return $"unexpected {unwanted}";
    }
    if (expect.Error is not null) {
      if (errors.Any(e => e.Code == expect.Error)) {
        return null;
      }
      return errors.Count == 0
        ? $"expected {expect.Error}, but it compiled"
        : $"expected {expect.Error}, got: {string.Join("; ", errors)}";
    }
    if (errors.Count > 0) {
      return string.Join("; ", errors);
    }

    using var proc = Process.Start(new ProcessStartInfo(exe) {
      UseShellExecute = false,
      RedirectStandardOutput = true,
      RedirectStandardError = true,
    })!;
    var stdoutTask = proc.StandardOutput.ReadToEndAsync();
    var stderrTask = proc.StandardError.ReadToEndAsync();
    if (!proc.WaitForExit(10_000)) {
      proc.Kill();
      return "timed out";
    }
    var stdout = stdoutTask.Result.Replace("\r\n", "\n");
    if (proc.ExitCode != expect.Exit) {
      return $"exit {proc.ExitCode}, expected {expect.Exit}; stderr: {stderrTask.Result.Trim()}";
    }
    if (stdout != expect.Stdout) {
      return $"stdout differs\n--- expected\n{expect.Stdout}--- actual\n{stdout}";
    }
    return null;
  }
}
