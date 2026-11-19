# minimaxon

A small Maxon compiler for trying out language changes. It is written in C#, targets x64 Windows only, and
is about 4,000 lines. It writes a PE executable directly, with no assembler, linker or C runtime.

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
| `minimaxon build <file\|dir> [-o out.exe]` | compile; a directory compiles every `*.maxon` in it as one program |
| `minimaxon run <file\|dir>` | compile to `%TEMP%\minimaxon\` and run; exits with the program's code |
| `minimaxon check <file\|dir>` | diagnostics only |
| `minimaxon test [dir]` | run the corpus (default `tests/`) |

### Tests

Each `tests/**/*.maxon` is one program. Directives in comments say what it must do:

| Directive | Meaning |
|---|---|
| `//> text` | one expected line of stdout, in order (none means stdout must be empty) |
| `// expect-exit: N` | expected exit code (default 0; a panic exits 1) |
| `// expect-error: E3102` | compiling must fail with this code |
| `// expect-no-error: E3077` | this code must not be reported |

## Pipeline

| File | Stage |
|---|---|
| `src/Lexer.cs` | tokens; drops newlines inside brackets; splits string interpolation into parts |
| `src/Parser.cs` | recursive descent → `src/Ast.cs`; anything outside the subset is `E9000` |
| `src/Checker.cs` | names, types, ranged integers, calls, matches; writes its results onto the AST nodes |
| `src/Ownership.cs` | a flow-sensitive pass: moves (`E3102`), unused names (`E3012`), a never-reassigned `var` (`E3077`) |
| `src/Codegen.cs` | AST → x64, plus the runtime routines, hand-assembled |
| `src/X64.cs` | instruction encoder, labels, the data section |
| `src/PeWriter.cs` | PE32+ image: `.text`, `.data`, `.idata` (kernel32 only) |
| `src/Program.cs` | CLI and test runner |

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
  fields, default parameters, format specifiers, `#if`, or visibility checks. Each of these is parsed far
  enough to report `E9000`.
- The lint rules (`E3064`, `E3065`, `E3092`, `E3219`) are not implemented.

## Adding a feature

A typical change touches one file per stage:

1. **Syntax:** a node in `Ast.cs` and a branch in `Parser.cs`. If the word was an `Unsupported(...)`
   refusal, that call is where the new production goes.
2. **Typing:** a case in `Checker.CheckExpr` or `Checker.CheckStmt`. Store what codegen needs on the node.
3. **Ownership:** a case in `Ownership.Stmt` or `Ownership.Expr`. The pass throws on a node it does not
   know, so it cannot be skipped by accident.
4. **Codegen:** a case in `Codegen.EmitExpr` or `Codegen.EmitStmt`.
5. **Tests:** a `tests/run/` program for the behaviour, and a `tests/errors/` program for each diagnostic.

A rule that is only a diagnostic, such as a new ownership or region check, usually needs steps 2 or 3 and
a test, and nothing else.
