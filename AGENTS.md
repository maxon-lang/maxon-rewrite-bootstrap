# minimaxon — instructions for agents

minimaxon is the Go bootstrap of the next Maxon compiler. Today it is a small compiler for trying out language
changes; if it succeeds it grows into the full Maxon compiler, which will later be ported to Maxon by hand,
checked by the port producing byte-identical output to this compiler.
Treat every design choice as the foundation of that compiler, not as experiment code. `README.md` describes the
language subset, the pipeline, the packages and the IR; read it before changing the compiler.

## Direction

- **Compatibility with current Maxon is not a criterion.** Change any syntax, rule, diagnostic or error code when
  the change improves the compiler. Never keep something only because the current compiler (the maxon repository)
  does it, and never add a shim or a deprecation period.
- **Assume the source being compiled is correct.** Phases keep running in parallel without waiting on each other.
  A check that needs another module's results is deferred to the latest point where the answer is actually needed,
  and done there quickly: a call to a function the module does not declare is assumed to exist elsewhere until the
  call is resolved, and only then is a missing or mismatched function an error. Correct programs pay nothing.
- **Keep the compiler Maxon-shaped.** The port to Maxon carries over the architecture, not the Go: phases as barriers,
  interfaces over closed sets of types (they become Maxon unions), explicit passes, and data a Maxon program can own.
  A reference that is not an owner's hold on its part (a declaration, a caller, an instance, an IR block) is a handle
  or an index, never a shared pointer; code that still shares pointers is converted when it is next changed. No
  reflection, no dependency outside the standard library, and no runtime-specific trick that the design depends on.
  Otherwise write idiomatic Go.
- **Generated code is allowed when its input is the source of truth** (for example the parser's dispatch generated
  from the grammar). The generator is plain Go in this repository, its output is checked in and deterministic, and a
  test regenerates it and fails if the checked-in file differs.
- **Speed is a requirement.** Both the compiler's own build and its compile speed must stay fast. Measure with
  `--timings` on a large generated project before and after a change to a hot phase, and report the numbers.

## Building and testing

Go 1.27 on Windows x64. Go may not be on `PATH` in a fresh shell; it is at `C:\Program Files\Go\bin`.

```powershell
go build -o minimaxon.exe .
```

```powershell
go test ./...
```

```powershell
.\minimaxon.exe test
```

- **Every change keeps `go vet ./...` clean, `go fix -diff ./...` empty, the code `gofmt`-formatted, and both test
  suites green.** `go fix` runs the modernizers the Go language server suggests; `go fix ./...` applies them.
- **Unit tests** live beside the package they test (`internal/<package>/*_test.go`) and build only that package and
  its imports, so prefer them for anything a package can answer alone. `internal/check/check_test.go` runs the front
  end on a source string in memory; copy its helper for new front-end tests.
- **Corpus tests** under `tests/` run whole programs (`tests/run/`), diagnostics (`tests/errors/`, with
  `// expect-error: CODE [at L:C]`) and multi-module projects (`tests/modules/<case>/`). A behaviour change gets a
  run test; every new diagnostic gets an error test.
- **Watch a test fail before you fix it.** A fix without a test that failed first is not done.
- **Determinism is part of correctness.** The executable must be byte-identical for any `--jobs` value. Never let Go
  map iteration order reach output, diagnostics, or the order in which registers, slots, blocks, strings or symbols
  are allocated: iterate slices or sorted keys, and merge parallel results in source order. When a change should not
  alter output, compare the executable and `--emit-ir` text of a large project before and after.

## Structure

- **Packages are layers** (`README.md`, "Packages"). Imports only point down the list; a change that needs an import
  cycle is a design problem, not a reason to merge packages. Put new code in the package whose phase it belongs to,
  and export only what another package uses.
- **Phases are barriers.** Within a phase, work items (modules) run in parallel and may only read results that an
  earlier phase produced. Never write another module's declarations or types after the declare phase.
- **Data the driver owns stays out of the model.** `ast.Module` holds what describes a module; per-phase products
  such as IR and code units belong to the driver.

## Code quality

- **Fix the class, not the instance.** Before fixing a defect, find where else the same decision is made, and make it
  in one place that every caller goes through.
- **No workarounds.** Fix the cause. A symptom without a mechanism is the next thing to debug, never a known issue to
  describe as unexplained.
- **Never hand-write what the grammar generates.** Syntax, AST nodes, tokens and keywords are edited in
  `internal/syntax/Maxon.g4`, then `go generate ./internal/syntax`; never edit a `*_gen.go` file. Code that handles
  every member of `Decl`, `TypeDeclNode`, `Stmt` or `Expr` implements a generated visitor, never a type switch, so a
  new node fails the build where it is not handled yet.
- **No silent unhandled cases.** A type switch or `if` chain that does not cover every case ends in a panic with a
  `diag.Internal` error, never a default value. No sentinel return values.
- **Diagnostics are deterministic and name the fix.** A compile error is a property of the program, says what is
  wrong and how to fix it, and is raised as a `*diag.CompileError` panic that `diag.Guard` collects. Any other panic
  is an internal compiler error. A syntax error gets the parser's generic error, not a dedicated diagnostic.
- **No duplicated logic.** Factor shared code into one helper, including duplication that already exists.
- **Comments explain why, never how, and never history.** Most code needs none. Git holds the history.
- **No magic values.** Name constants that carry meaning.

## Working in this repository

- **Scratch files stay out of the repository**: logs, generated test projects, diffs and notes go in the system temp
  directory or your session's scratch directory.
- **Commit only when the user asks.** Commit messages describe what changed and why; the first line is a summary.
- **Keep `README.md` true.** A change to the language subset, a command or flag, the packages, the pipeline or the IR
  updates the README in the same commit.
