# minimaxon — instructions for agents

minimaxon is the Go bootstrap of the next Maxon compiler, which replaces the current one (`maxon-bin/` in the maxon
repository). The Go compiler implements only the subset needed to compile `self/`, a port of minimaxon to Maxon; once
`self/` compiles itself, Go work ends and `self/` grows into the full Maxon compiler.
Treat every design choice as the foundation of that compiler, not as experiment code. `README.md` describes the
language subset, the pipeline, the packages and the IR; read it before changing the compiler.

## Direction

- **Finish fast.** Reaching a self-hosted compiler and then full Maxon quickly is the priority. This is a
  re-implementation of a language that already has a compiler and 17,649 spec tests, so do not spend time on extra
  tests, reviews or re-checking beyond what is required below; do what moves the plan forward.
- **Compatible with current Maxon, except for minimaxon's language changes.** Where minimaxon has not deliberately
  changed the language (README, "Where it differs from Maxon"), do what the current compiler does: its syntax, rules,
  error codes, and runtime behaviour such as Win32 calls and command-line splitting. A new difference is a language
  change made on purpose and recorded in that README section, never an accident of implementation. A language change
  gets no shim and no deprecation period.
- **Reuse maxon's code and tests.** Pull code from the maxon repository with `cmd/convert` wherever it fits rather than
  writing it again, and regenerate converted files instead of hand-editing them. Where maxon's code is not converted,
  read it first and follow its behaviour, edge cases and names.
- **Assume the source being compiled is correct.** Modules' stages keep running in parallel, each waiting only for
  what it reads. A check that needs another module's results is deferred to the latest point where the answer is
  actually needed, and done there quickly: a call to a function the module does not declare is assumed to exist
  elsewhere until the call is resolved, and only then is a missing or mismatched function an error. Correct programs
  pay nothing.
- **Keep the compiler Maxon-shaped.** The port to Maxon carries over the architecture, not the Go: per-module stages
  scheduled by what they read, interfaces over closed sets of types (they become Maxon unions), explicit passes, and
  data a Maxon program can own. A reference that is not an owner's hold on its part (a declaration, a caller, an
  instance, an IR block) is a handle or an index, never a shared pointer; code that still shares pointers is converted
  when it is next changed. No reflection, no dependency outside the standard library, and no runtime-specific trick
  that the design depends on. Otherwise write idiomatic Go.
- **Generated code is allowed when its input is the source of truth** (for example the parser's dispatch generated
  from the grammar). The generator is plain Go in this repository, its output is checked in and deterministic, and a
  test regenerates it and fails if the checked-in file differs.
- **Speed is a requirement.** Both the compiler's own build and its compile speed must stay fast. Measure with
  `--timings` only when asked.
- **The design is CPU-cache optimized.** A worker keeps a module on its core from one stage to the next while its data
  is hot, rather than sweeping every module through one phase. Data is flat and indexed by handles: small fixed-size
  records in slices (the 8-byte token is the model), frequently used fields apart from rarely used ones, and no
  pointer graphs or maps on hot paths. Allocation is memory traffic, so keep it down.

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

- **Before a commit, `go build`, `go vet ./...` and both test suites pass, and the code is `gofmt`-formatted.**
  Nothing else is required: no test written first, no test per change, no timing.
- **Add a test only when** a bug would otherwise come back, or nothing in the corpus covers a feature. Unit tests live
  beside their package (`internal/<package>/*_test.go`); `internal/check/check_test.go` runs the front end on a source
  string in memory.
- **Corpus tests** under `tests/` run whole programs (`tests/run/`), diagnostics (`tests/errors/`, with
  `// expect-error: CODE [at L:C]`) and multi-module projects (`tests/modules/<case>/`). After M1, maxon's spec tests
  (`specs/`) are the corpus.
- **Determinism.** Never let Go map iteration order reach output, diagnostics, or the order in which registers, slots,
  blocks, strings or symbols are allocated: iterate slices or sorted keys, and merge parallel results in source order.

## Structure

- **Packages are layers** (`README.md`, "Packages"). Imports only point down the list; a change that needs an import
  cycle is a design problem, not a reason to merge packages. Put new code in the package whose phase it belongs to,
  and export only what another package uses.
- **A module's stage waits only on the modules it names.** Each stage of a module (parse, resolve, aliases, declare,
  constants, check, generate) is a task that waits for the module's stage before it and for the stages of the modules
  it names whose results it reads (README, "The modules phase"); there is no barrier across all modules. A stage reads
  only results of stages it waited on, and never writes another module's declarations or types.
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

- **Automate repetitive work with tools.** A change that repeats across many files (renaming built-ins, renumbering
  error codes, rewriting `tests/` and `self/`, importing maxon code or specs) is done by a tool, not by hand. A tool
  that will be run again goes in `cmd/<name>/` as plain Go with no dependencies, as `cmd/convert` does; a one-off
  goes in the scratch directory.
- **Scratch files stay out of the repository**: logs, generated test projects, diffs and notes go in the system temp
  directory or your session's scratch directory.
- **Commit only when the user asks.** Commit messages describe what changed and why; the first line is a summary.
- **Keep `README.md` true per stage.** When a stage of the plan lands, update the README for what it changed (the
  language subset, commands and flags, packages, pipeline, IR, and "Where it differs from Maxon").
