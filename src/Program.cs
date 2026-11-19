using System.Diagnostics;

namespace MiniMaxon;

public static class Cli {
  const string Usage = """
    usage:
      minimaxon build <file.maxon|dir> [-o out.exe]   compile to a Windows x64 executable
      minimaxon run <file.maxon|dir>                  compile and run
      minimaxon check <file.maxon|dir>                report diagnostics only
      minimaxon test [dir]                            run the test corpus (default: tests/)

    A directory is a project: each directory under it is a module. --jobs=N sets how many modules
    compile at once (default: one fewer than the processor count). --timings prints each phase's time; --emit-ir writes the IR beside the executable, as <out>.ir.
    """;

  static int jobs = Driver.DefaultJobs;

  static bool timings;

  static bool emitIr;

  static List<CompileError> Compile(string path, string? output) => Driver.Compile(path, output, jobs, timings, emitIr);

  const int InternalErrorExitCode = 3;

  static string DescribeInternal(Exception e) => $"internal compiler error (a bug in minimaxon, not in the program): {e}";

  public static int Main(string[] args) {
    if (args.Length == 0) {
      Console.Error.WriteLine(Usage);
      return 2;
    }

    var jobsFlag = args.FirstOrDefault(a => a.StartsWith("--jobs=", StringComparison.Ordinal));
    if (jobsFlag is not null) {
      if (!int.TryParse(jobsFlag["--jobs=".Length..], out jobs) || jobs < 1) {
        Console.Error.WriteLine("--jobs takes a positive number");
        return 2;
      }
      args = [.. args.Where(a => a != jobsFlag)];
    }

    timings = args.Contains("--timings");
    emitIr = args.Contains("--emit-ir");
    args = [.. args.Where(a => a is not ("--timings" or "--emit-ir"))];

    try {
      return Run(args);
    } catch (Exception e) when (e is not CompileError) {
      Console.Error.WriteLine(DescribeInternal(e));
      return InternalErrorExitCode;
    }
  }

  static int Run(string[] args) {
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

  sealed record Expectation(string? Error, int Exit, string Stdout);

  const string ModulesDir = "modules";

  static Expectation ReadExpectation(string program) {
    string? error = null;
    var exit = 0;
    var stdout = new System.Text.StringBuilder();
    string[] files = Directory.Exists(program) ? [.. Directory.GetFiles(program, "*.maxon").Order(StringComparer.Ordinal)] : [program];
    foreach (var raw in files.SelectMany(File.ReadLines)) {
      var line = raw.TrimStart().TrimEnd('\r');
      if (line.StartsWith("// expect-error:", StringComparison.Ordinal)) {
        error = line["// expect-error:".Length..].Trim();
      } else if (line.StartsWith("// expect-exit:", StringComparison.Ordinal)) {
        exit = int.Parse(line["// expect-exit:".Length..].Trim());
      } else if (line.StartsWith("//>", StringComparison.Ordinal)) {
        stdout.Append(line.Length > 3 && line[3] == ' ' ? line[4..] : line[3..]).Append('\n');
      }
    }
    return new Expectation(error, exit, stdout.ToString());
  }

  static int RunTests(string dir) {
    dir = Path.GetFullPath(dir);
    var modulesRoot = Path.Combine(dir, ModulesDir);
    var files = Directory.GetFiles(dir, "*.maxon", SearchOption.AllDirectories)
      .Where(f => !f.StartsWith(modulesRoot + Path.DirectorySeparatorChar, StringComparison.Ordinal))
      .Concat(Directory.Exists(modulesRoot) ? Directory.GetDirectories(modulesRoot) : [])
      .Order(StringComparer.Ordinal).ToList();
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
      return DescribeInternal(e);
    }
    if (expect.Error is not null) {
      var parts = expect.Error.Split(" at ", 2);
      var code = parts[0];
      var at = parts.Length == 2 ? parts[1] : null;
      if (errors.Any(e => e.Code == code && (at is null || at == $"{e.Line}:{e.Col}"))) {
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
