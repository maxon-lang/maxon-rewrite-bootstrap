# minimaxon

A small Maxon compiler for trying out language changes, and the seed of the next full Maxon compiler. It is
written in Go, targets x64 Windows only, and is about 8,000 lines in ten packages. It compiles modules in parallel and writes a
PE executable directly, with no external assembler, linker or C runtime.

It is not the current Maxon compiler (`maxon-bin/` in the maxon repository) and shares no code with it. It
started from a core subset of Maxon's syntax and error codes, but it does not track the current compiler: an experiment is free to change the
language, its rules and its diagnostics. Compatibility with current Maxon is not a criterion; anything changes
when the change improves the compiler. It is the bootstrap for a compiler that will later be written in Maxon
itself, so its code stays plain enough to port: structs, interfaces, slices, and no runtime-specific tricks the
design depends on.

## Use

Needs Go 1.27.

```powershell
go build -o minimaxon.exe .
```

```powershell
.\minimaxon.exe run tests/run/enums-unions.maxon
```

```powershell
go test ./...
```

| Command | What it does |
|---|---|
| `minimaxon build <file\|dir> [-o out.exe]` | compile; a directory is a project, one module per directory |
| `minimaxon run <file\|dir>` | compile to `%TEMP%\minimaxon\` and run; exits with the program's code |
| `minimaxon check <file\|dir>` | diagnostics only |
| `minimaxon test [dir]` | run the corpus (default: `tests` beside the executable) |

`--emit-ir` writes the IR of every function beside the executable, as `<out>.ir`. `--jobs=N` sets how many
modules compile at once. The default is one fewer than the processor count, and the executable is byte-identical
whatever the value. `--timings` prints each phase's wall time, allocation, garbage collections and GC pause time
to stderr.

### Tests

There are two kinds. **Unit tests** (`internal/<package>/*_test.go`) test one package with `go test`, which builds
only that package and the ones it imports, so a change is checked in a few hundred milliseconds without rebuilding
the compiler. `internal/check`'s tests show how to run the front end on a source string in memory. **Corpus tests**
run whole programs through `minimaxon test`.

Each `tests/**/*.maxon` is one program, except under `tests/modules/`, where each directory is a project
and its root files carry the directives. Directives in comments say what the program must do:

| Directive | Meaning |
|---|---|
| `//> text` | one expected line of stdout, in order (none means stdout must be empty) |
| `// expect-exit: N` | expected exit code (default 0; a panic exits 1) |
| `// expect-error: E3102` | compiling must fail with this code; `E3102 at 7:5` also pins the line and column |

### Errors

A diagnostic is `file:line:col: error E….: message`, and compiling stops at the first error in each
function. A diagnostic is a `*CompileError` panic that `Guard` (`internal/diag`) turns into a list entry; any other
panic is a bug in minimaxon, not in the program. It prints `internal compiler error` with a stack trace and
exits 3.

## Philosophy: assume the source is correct

The compiler assumes the program it compiles is correct, so phases can keep running in parallel without waiting
on each other. A check that needs another module's results is deferred to the latest point where the compiler
actually needs the answer, and done there quickly. For example, a module that calls a function its own module does
not declare assumes the function exists in another module; when the call is finally needed and no matching function
is found, that is the error. Correct programs pay nothing for the checks, and an incorrect one is still refused
before it produces an executable.

Cross-module calls already work this way: they are typed from the callee's declared signature and checked in the
verify phase. Name resolution still builds the project-wide name tables first; it is the next candidate for the
same treatment.

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

## Packages

The compiler is a set of packages under `internal/`, each a phase or a layer, so each builds and tests on its own.
Dependencies only point down the list, and Go refuses an import cycle, so the layering cannot erode:

| Package | Holds | Imports |
|---|---|---|
| `diag` | `CompileError`, `Guard`, the error codes | — |
| `par` | `ForEach`: runs a phase over a pool of `--jobs` goroutines | — |
| `ast` | the program model: AST nodes, semantic types, `I128`, `Module` and `Project`, keywords | `diag` |
| `project` | loading a project: discovery, declaration tables, the import-cycle check | `ast`, `diag`, `par` |
| `syntax` | module source buffers, the lexer and the parser | `ast`, `diag` |
| `resolve` | name resolution | `ast`, `diag` |
| `check` | the type checker and the ownership pass | `ast`, `diag` |
| `ir` | the IR, its printer, lowering, and the runtime's symbol names | `ast`, `diag` |
| `backend` | the x64 encoder, codegen, the runtime unit, the linker and the PE writer | `ast`, `diag`, `ir`, `par` |
| `driver` | `Compile`: runs the phases | all of the above |

The `minimaxon` command itself (`main.go`) is the CLI and the corpus runner. A package exports only what another
package uses. The AST nodes still carry their semantic types directly, which is why `ast` holds both; moving type
information into side tables, as `go/ast` and `go/types` do, would let the two separate.

An edit rebuilds only its package and the packages that import it: after a change to `check`, the compiler
rebuilds in about 0.4 s, and `go test ./internal/check` runs in about 0.3 s.

## Pipeline

`driver.Compile` runs the phases below in order. Every phase is parallel, and each completes before the next
starts, so a work item only reads another's results from an earlier phase and needs no locks. Within a module the
work is serial; the module is the unit of parallelism, except for constants (one per item) and the link (one code
unit per item). Results are merged in source order, which keeps the output deterministic.

| Phase | Package | Work per module |
|---|---|---|
| discover | `project` | walks the directory tree, one goroutine per subdirectory |
| parse | `syntax` | reads the module's files into one buffer, strips comments, lexes the whole buffer in one pass and parses the token stream into the AST; anything outside the subset is `E9000` |
| tables | `project` | the module's declaration table and an empty type object for each of its types; then the project-wide name index |
| resolve | `resolve` | binds every name to its declaration and records module dependencies |
| declare | `check` | the module's fields, cases and function signatures |
| constants | `driver` | top-level `let` types, in waves: each wave holds the constants whose dependencies are done (a cycle is `E2012`) |
| bodies | `check` | types every function body; a call into another module takes the callee's declared return type and is recorded, not checked |
| verify | `check` | checks the recorded cross-module calls, then moves (`E3102`) |
| lower | `ir` | the checked AST of each function becomes IR (below), and unreachable blocks are pruned |
| codegen | `backend` | IR → x64, one code unit per module, calls between units symbolic; the runtime is its own hand-written unit |
| link | `backend` | offsets by prefix sum, then each unit copied and patched in parallel; the units go in module-path order after a runtime unit |

A phase that reports an error stops the compile, so later phases never see a broken module. The one serial step
is the import-cycle check between resolve and declare, a walk over the module graph.

### Reading a module

A module's files are read into one `[]byte`, allocated once at the sum of their sizes, and stripped of comments
in place:

- a `///` doc comment that starts its line is kept, for the LSP to use later; the lexer skips it;
- a `//` comment is dropped;
- a block comment keeps its newlines, and spaces on its last line when code follows, so every line and column
  after it is unchanged;
- an unterminated block comment is left as written, so the lexer reports it.

The lexer works on the raw UTF-8 bytes, so a column counts bytes. One lexer pass covers the whole buffer. At each
file boundary it resets the line, column and bracket depth, so no token spans two files, and emits a `FileEnd`
token. One parser reads the module's stream, naming each file in its diagnostics. A declaration left open at the
end of a file fails there with `E2007`, and parsing resumes at the next file, so each file reports its own first
syntax error.

### Allocation

Parallel phases are only as fast as allocation allows. The lexer reuses token buffers and interns a module's
repeated identifiers, and the ownership pass skips a function with no movable variable and shares its state
until a move changes it. During a compile the driver turns the collector off (`debug.SetGCPercent(-1)`) under a
memory limit of 100 bytes per source byte, at least 256 MB, so a compile that outgrows it still collects. Watch
the MB and pause columns of `--timings` when changing a phase.

## The IR

The checker works on the AST, which mirrors what the author wrote. Everything after it works on the IR in
`internal/ir`, which is what a later pass such as reference counting, region lowering or an optimization changes.

- A function is a list of basic blocks, each a list of instructions ending in one terminator (`jump`, `branch`,
  `switch`, `ret`, `trap`).
- Instructions read and write virtual registers (`%3`). Locals and temporaries live in numbered slots (`s0`).
  It is not SSA.
- Every slot and register is `scalar` or `managed`, so a pass can find every reference to a heap object.
- Checks are explicit: a range check is a `guard` with its bound, an allocation is `alloc`, and only overflow,
  a zero divisor and a negative shift are left to codegen.
- Instructions and terminators are closed sets of structs behind an interface, which port directly to Maxon
  unions.

Codegen is deliberately naive: every register and slot has its own frame slot, so each instruction loads its
operands and stores its result. That makes executables about twice the size of keeping values in registers;
register allocation is the next step there. A frame larger than a page touches each page as it grows, as Windows
requires. Calls use an internal convention: arguments are pushed left to right and the caller pops them. The
Win64 ABI applies only inside the runtime routines that call kernel32.

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

1. **Syntax:** a node in `internal/ast` and a branch in `internal/syntax` (`parseStatement`, `parsePrimary` and
   their helpers). If the word was an `unsupported(...)` refusal, that call is where the new production goes. A node
   holding names needs a case in `Resolver.stmt` or `Resolver.expr` too, which panics with an internal error on
   a node it does not know.
2. **Typing:** a case in `Checker.checkExpr` or `Checker.checkStmt`. Store what codegen needs on the node.
   Read another module's types and signatures freely, but never write them: only the declare phase writes
   a module's own declarations.
3. **Ownership:** a case in `Ownership.stmt` or `Ownership.expr`. The pass, like lowering and codegen, panics
   with an internal error on a node it does not know, so it cannot be skipped by accident.
4. **Lowering:** a case in `Lowerer.expr` or `Lowerer.stmt`, built from the existing instructions where it can.
   A new instruction needs a case in `codegen.instr` (`internal/backend`) and `instrText` (`internal/ir`) as well.
5. **Tests:** a unit test in the package that changed, a `tests/run/` program for the behaviour, and a
   `tests/errors/` program for each diagnostic.

A rule that is only a diagnostic, such as a new ownership or region check, usually needs steps 2 or 3 and
a test, and nothing else.
