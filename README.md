# minimaxon

A small Maxon compiler for trying out language changes. It is written in C#, targets x64 Windows only, and
is about 5,000 lines. It compiles modules in parallel and writes a PE executable directly, with no external
assembler, linker or C runtime.

It is not the real compiler and does not share code with `maxon-bin/`. It accepts a core subset of Maxon,
using the same syntax and, where it implements a rule, the same error code as the real compiler. That
way an experiment's diagnostics read the same as the real thing's.

## Use

Needs the .NET 10 SDK.

```powershell
dotnet build -c Release
```

```powershell
dotnet bin/Release/net10.0/minimaxon.dll run tests/run/enums-unions.maxon
```

| Command | What it does |
|---|---|
| `minimaxon build <file\|dir> [-o out.exe]` | compile; a directory is a project, one module per directory |
| `minimaxon run <file\|dir>` | compile to `%TEMP%\minimaxon\` and run; exits with the program's code |
| `minimaxon check <file\|dir>` | diagnostics only |
| `minimaxon test [dir]` | run the corpus (default `tests/`) |

`--jobs=N` sets how many modules compile at once. The default is one fewer than the processor count. The
executable is byte-identical whatever the value. `--timings` prints each phase's wall time, allocation and
garbage collections to stderr.

### Tests

Each `tests/**/*.maxon` is one program, except under `tests/modules/`, where each directory is a project
and its root files carry the directives. Directives in comments say what the program must do:

| Directive | Meaning |
|---|---|
| `//> text` | one expected line of stdout, in order (none means stdout must be empty) |
| `// expect-exit: N` | expected exit code (default 0; a panic exits 1) |
| `// expect-error: E3102` | compiling must fail with this code |

### Errors

A diagnostic is `file:line:col: error E….: message`, and compiling stops at the first error in each
function. Anything else the compiler throws is a bug in minimaxon, not in the program. It prints
`internal compiler error` with a stack trace and exits 3.

## Modules

A module is a directory. The project root is the root module, which holds `main`, and every directory
under it that holds source, at any depth, is a module of its own. Names follow Maxon's rules:

- **Qualified:** `geometry.area()`, `7 as geometry.Length`, `geometry.units.scale(3)`. A path is read
  from the root. `export.X` names the root module's `X` and `stdlib.X` a built-in.
- **Bare:** the module's own declaration, else the root's, else the one exported declaration anywhere.
  Two candidates is `E3095` for a function or binding and `E3063` for a type.
- **Visibility is flat:** an unmarked declaration, field or method is visible only inside its own module.
  `export` and `public` make it visible everywhere. Naming a hidden one is `E3008` (`E3014` for a field).
- **One namespace per module:** a name declared twice across the module's files is `E3006`.
- **No import cycles:** module A depends on B when A names one of B's declarations. A cycle is `E9001`.
- A directory whose name is not an identifier, or a top-level one named `export`, `stdlib`, `runtime` or
  `scriptlib`, is `E3182`.

## Pipeline

[`src/Driver.cs`](src/Driver.cs) runs the phases below in order. Every phase is parallel, and each completes
before the next starts, so a work item only reads another's results from an earlier phase and needs no
locks. Within a module the work is serial; the module is the unit of parallelism, except for parsing (one
file per item), constants (one per item) and the link (one code unit per item). Results are merged in
source order, which keeps the output deterministic.

| Phase | File | Work per module |
|---|---|---|
| discover | `src/Modules.cs` | walks the directory tree, one subdirectory per item |
| parse | `src/Lexer.cs`, `src/Parser.cs` | tokens and AST (`src/Ast.cs`), one file per item; anything outside the subset is `E9000` |
| tables | `src/Modules.cs` | the module's declaration table and an empty type object for each of its types; then the project-wide name index |
| resolve | `src/Resolver.cs` | binds every name to its declaration and records module dependencies |
| declare | `src/Checker.cs` | the module's fields, cases and function signatures |
| constants | `src/Driver.cs` | top-level `let` types, in waves: each wave holds the constants whose dependencies are done (a cycle is `E2012`) |
| bodies | `src/Checker.cs` | types every function body; a call into another module takes the callee's declared return type and is recorded, not checked |
| verify | `src/Checker.cs`, `src/Ownership.cs` | checks the recorded cross-module calls, then moves (`E3102`) |
| codegen | `src/Codegen.cs` | one code unit per module; calls between units are symbolic |
| link | `src/Linker.cs`, `src/PeWriter.cs` | offsets by prefix sum, then each unit copied and patched in parallel; the units go in module-path order after a runtime unit |

`src/X64.cs` is the instruction encoder, and `src/Program.cs` is the CLI and test runner. A phase that
reports an error stops the compile, so later phases never see a broken module. The one serial step is the
import-cycle check between resolve and declare, a walk over the module graph.

Parallel phases are only as fast as allocation allows. The compiler uses server GC (`MiniMaxon.csproj`), and
the lexer reuses one token buffer per thread and interns each file's repeated identifiers. Watch the MB column
of `--timings` when changing a phase.

Codegen is deliberately naive. Every expression leaves its value in `rax`, operands go through the stack,
and every local has a frame slot. Calls use an internal convention: arguments are pushed left to right and
the caller pops them. The Win64 ABI applies only inside the runtime routines that call kernel32.

Records and union values are heap boxes of 8-byte words (a union's first word is its case index), allocated
with `HeapAlloc` and **never freed**. A string is a pointer to `[length][UTF-8 bytes]`. An enum value is its
case index.

## The subset

- `function`, `type` (fields, defaults, methods, `static function`), `enum` (integer raw values), `union`
  (payloads, methods), `typealias X = int(a to|upto b)`, a top-level `let` constant.
- `let`, `var`, assignment to a variable or field, `_ = call()`, `if`/`else if`/`else`, `while`,
  `for i in a to|upto b`, `match` as a statement or expression (case, literal, range and string patterns,
  `or` alternatives, `default`, `panic` arms), `break`/`continue` with labels, `return`.
- Integer, bool and string literals, interpolation, arithmetic and bitwise operators, `shl`/`shr`,
  comparisons, `and`/`or`/`xor`/`not`, `x if c else y`, `as` casts, calls with Maxon's
  positional-then-labelled arguments, `mutable` (by-reference) parameters, `.name`/`.ordinal`/`.rawValue`.
- Built in: `print`, `printError`, `panic`, and the aliases `ExitCode`, `Count`, `FullRangeInt`. There is no
  stdlib.

### Integers

Every typealias is its own type. An integer literal is an untyped constant: it fits any alias whose range
holds it, and constant expressions fold at compile time. A value whose range is known but has no alias,
such as a `for` variable or arithmetic over constants, fits any alias whose range contains it. Mixing two
different aliases is `E3005`. Arithmetic traps on 64-bit overflow, and on leaving the alias's range,
checked where the result is computed. `as` checks at compile time for a constant and at run time otherwise.

### Where it differs from Maxon

- Nothing is freed: there is no reference counting, and no leak check at exit.
- `/` and `mod` panic on a zero divisor instead of throwing `DivisionByZero` under `try`.
- No alias subtyping (`implements`), floats, characters, arrays, tuples, generics, interfaces, closures,
  `try`/`throws`, async, services, region capabilities (`isolated`, `immutable`, `modify`, `freeze`), static
  fields, default parameters, format specifiers, or `#if`. Each of these is parsed far enough to report
  `E9000`.
- Modules differ from Maxon in three ways. Visibility is flat, not "the directory and its subdirectories".
  Import cycles are refused. And the rule that an exported signature may not name a less visible type
  (`E3167`) is not checked.
- It checks only the rules that the generated code depends on. The style rules are left out: an unused
  name (`E3012`), a `var` that is never reassigned (`E3077`), a label naming the innermost block (`E2048`),
  an unneeded cast (`E3010`), self-assignment or `_ =` on a non-call (`E3067`), a one-value range pattern
  (`E2027`), all bindings discarded (`E3081`), and the lint rules (`E3064`, `E3065`, `E3092`, `E3219`).

## Adding a feature

A typical change touches one file per stage:

1. **Syntax:** a node in `Ast.cs` and a branch in `Parser.cs`. If the word was an `Unsupported(...)`
   refusal, that call is where the new production goes. A node holding names needs a case in `Resolver.cs`
   too, which raises an internal compiler error on a node it does not know.
2. **Typing:** a case in `Checker.CheckExpr` or `Checker.CheckStmt`. Store what codegen needs on the node.
   Read another module's types and signatures freely, but never write them: only the declare phase writes
   a module's own declarations.
3. **Ownership:** a case in `Ownership.Stmt` or `Ownership.Expr`. The pass, like codegen, raises an internal
   compiler error on a node it does not know, so it cannot be skipped by accident.
4. **Codegen:** a case in `Codegen.EmitExpr` or `Codegen.EmitStmt`.
5. **Tests:** a `tests/run/` program for the behaviour, and a `tests/errors/` program for each diagnostic.

A rule that is only a diagnostic, such as a new ownership or region check, usually needs steps 2 or 3 and
a test, and nothing else.
