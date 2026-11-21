# minimaxon

A Maxon compiler for trying out language changes, and the seed of the next full Maxon compiler. It is written in
Maxon (`self/`), compiles itself, targets x64 Windows only, and writes a PE executable directly, with no external
assembler, linker or C runtime.

It is not the current Maxon compiler (`maxon-bin/` in the maxon repository) and shares no code with it, but it
replaces it: `self/` grows into the full compiler. It is compatible with current Maxon except for the language changes
developed here ("Where it differs from Maxon"). The runtime is Maxon source too (`runtime/`): it is loaded as a module
of every program, checked, lowered and linked with it. Go is used only for the generators under `internal/` and the
tools under `cmd/`.

## Use

`seed\minimaxon.exe` is a compiler built from `self/` and committed (`seed/README.md` says how it is renewed). The
day-to-day compiler is built from the seed:

```powershell
.\build.ps1
```

which has the seed build `bin\c1.exe` and C1 build `minimaxon.exe` in the root of the repository: `self/` compiled by
itself. It finds `runtime/` by walking up from its own executable, so it stays in the tree.

```powershell
.\minimaxon.exe run tests/run/arithmetic.maxon
```

```powershell
.\check.ps1
```

runs every check (see "Tests"). The generators and the tools need Go 1.27; `go generate ./internal/...` runs every
generator.

| Command | What it does |
|---|---|
| `minimaxon build <file\|dir> [-o out.exe]` | compile; a directory is a project, one module per directory |
| `minimaxon run <file\|dir>` | compile to the temporary directory, as `minimaxon-run-<process id>-<name>.exe`, and run; exits with the program's code |
| `minimaxon check <file\|dir>` | diagnostics only, exit code 1 when there are any; `check runtime` checks the runtime's source and holds it to the functions the compiler calls (see "The runtime") |
| `minimaxon test [--update-positions] [dir]` | run the corpus (default: the nearest `tests` directory from the executable upward; see "Tests") |
| `minimaxon test <specs-dir\|file.md> [--verbose]` | run maxon's spec tests (see "Tests") |
| `minimaxon selftest` | run the compiler's own tests (`self/*_test.maxon`) |
| `minimaxon rewrite --keyword=old=new <path>...` | rename a keyword by token in the `.maxon` files and the Markdown code under the paths (see "Changing the language") |
| `minimaxon qualify <dir>...` | write the module's path before each bare name of another module's declaration in the projects (see "Changing the language") |
| `minimaxon rename [--prelude=f,...] [--rules=file] <rule>... <program>...` | rename a built-in type or rewrite the calls of a built-in method by the declaration the checker resolved, never by text: `Array.append=push`, `Array.insert=insert({at}, value: {item})`, `Array.pop.unwrap=try {call} otherwise panic("empty")`, and a free function's whole call: `readFile().unwrap=try File.readText({path}) otherwise panic("unread")` (the rules are in `self/driver/rename.maxon`) |
| `minimaxon fmt [--check] <path>...` | put the `.maxon` files and the Maxon code blocks of the `.md` files under the paths in the one layout; `--check` changes nothing and exits 1 listing the files that are not in it (see "Layout" and "Changing the language") |
| `minimaxon interp <file\|dir> [--seed=N] [--frees]` | development only: run under the tracking interpreter, which runs a map, a list, a pool, a frozen value, a cown and a behaviour, and reports a violation of the memory model; `--frees` lists the values freed, in order |
| `minimaxon fuzz [--seed=N] [--count=N] [--schedules=N] [--keep=DIR] [--native] [--dump]` | development only: write random programs, check them and run them under the interpreter (`self/fuzz`) |
| `go run ./cmd/convert [--specs] [--write] [--check] [--out=DIR] [<path>...]` | convert existing Maxon `.maxon` source files to minimaxon syntax and layout, marking unsupported constructs with `// TODO` comments and warnings on stderr; `--check` writes nothing and prints each kind of warning with its count and files, most frequent first (run `minimaxon qualify` after converting a project of several modules); `--specs` converts maxon's spec files, which is how `specs/` is made. It lays out what it converts with the compiler's `fmt`: `minimaxon.exe` in the root of the repository, or the compiler the environment variable `MINIMAXON` names |
| `go run ./cmd/sample [-interval D] [-top N] [-callers N] [-focus TEXT,...] [-o FILE] <exe> [arg...]` | profile an executable that `build --emit-map` made: runs it, samples its main thread's instruction pointer and its `rbp` frames (1 ms by default), and prints the functions by self and by total samples, with the callers of the top ones and of those whose symbol contains a `-focus` text, and the wall time; code outside the image (a Windows DLL) is charged to the function that called it, found by scanning the stack, and exits with the program's code |

The compiler has no build cache and no `cache` command, and no `--timings`, `--trace=`, `--jobs=`, `--no-cache` or
`--with=` (parallel compilation and the build cache are planned: see "The modules phase" and "Build cache (planned)"): it says
so, prints its usage and exits 2, as it does for a command it does not know. `run` and the test
runner (the corpus and the specs) build their executables in the temporary directory under names that hold the process id
(and, for the runners, a count), so two compilers running at once never write the same file. A runner deletes each test's
executable when it can, tries again for the others when the run ends, and says in one line on standard error how many it could
not, which never changes the exit code.

`specs/` holds maxon's spec tests converted by `cmd/convert --specs`, in maxon's own format: `<!-- test: name -->` and the
fenced blocks ```` ```maxon ````, ```` ```exitcode ````, ```` ```stdout ````, ```` ```stderr ```` and ```` ```maxoncstderr ````. The tool
rewrites only each test's `maxon` block (every `// --- file:` section of it) through the source conversion, and copies
the rest, with line endings made LF. A test whose conversion warned, or whose source minimaxon cannot parse, carries one
`<!-- skip: kind -->` line per kind of warning after its test marker; the runner skips and counts it, and every other test
runs. `--check` over the specs, and the summary at the end of a conversion, give the tests converted cleanly and skipped and
the kinds by tests blocked, most blocking first. Then `fix specs` applies the fixes that minimaxon's language differences
name (a `try`, a cast, `.clone()`, `consumes`, `mutates`, `mutates self`, `var`) to every test that runs a program; a test
that expects maxon's run-time trap where the fix adds a `try` gets a `<!-- skip: language difference: ... -->` line
instead, and so does a test that expects a compile error which minimaxon reports first under another code for a known reason
(the pairs of `codeSkipTable` in `self/driver/fix.maxon`: ownership refused by another rule, the syntax error, operand
types). `specs.ps1` runs both; `specs/README.md` has the command, and the directory is never edited by hand, except
`specs/baseline.txt` (see "Tests").

`--emit-ir` writes the IR of every function beside the executable, as `<out>.ir`, and `--emit-map` the map of its code,
as `<out>.map`: a line per function and routine the link kept, in address order, with its RVA and size in hexadecimal
and its symbol, which `cmd/sample` reads. A profile of `self/` is two commands:
`.\minimaxon.exe build self -o bin\prof.exe --emit-map`, then `go run ./cmd/sample bin\prof.exe check self`.
`--count-allocations` builds an executable that counts its live heap boxes (see "Counting allocations"); it is for
development, and `minimaxon test` always uses it. `--update-positions` makes `test` rewrite the `at L:C` of an
`expect-error` whose code was reported at another place, and count the test as passed.

### Tests

`.\check.ps1` is the gate. In order, it formats, `go fix`es and vets the Go of the generators and tools; runs the
generators and fails if a generated file (`*_gen.*`) changed; has the seed build `bin\c1.exe` and C1 build
`bin\c2.exe`; runs the Go tests of the generators and tools (`go test ./...`, with C2 as `cmd/convert`'s formatter);
and then, with C2: `selftest`, `check runtime`, `fmt --check self runtime tests`, the corpus (`test`, with no standard
input), the spec tests held to the count of passed tests in `specs/baseline.txt` (which may go up, never down: raise
it when more pass), `fuzz` (300 generated programs, each one the checker accepts run clean under the interpreter), and
a build of `self/` into `bin\c3.exe`, which must be byte-identical to C2. It stops at the first step that fails.

There are three kinds of test. **Corpus tests** run whole programs through `minimaxon test`. **The compiler's own
tests** (`self/*_test.maxon`, run by `minimaxon selftest`) test its parts on programs and IR built by hand. **Go tests**
test the generators (each regenerates its outputs and fails if the checked-in ones differ; `internal/x64gen` proves the
instruction table against the bytes in its `testdata`) and `cmd/convert`.

Each `tests/**/*.maxon` is one program, except under `tests/modules/`, where each directory is a project and its files
carry the directives. The runner (`self/driver/test.maxon`) compiles every program to an executable, a program that
must fail included, because some errors come after type checking (lowering refuses what has no native code yet), in
the same process, one after another; the runtime is loaded, checked and compiled once per run, and each test is added
to a copy of that project (`SharedRuntime` in `self/driver/compile.maxon`). A program under `tests/check/` is only
compiled: one that expects no error must compile clean (a program that only the interpreter can run counts as clean),
and none is run. Any other program that compiles is run twice, as the executable and under the tracking interpreter
(see "The interpreter"), from the IR that the native build lowered, and both must give the stdout and the exit code the
directives say; the interpreter must also find no violation of the memory model, and the executable, built with
`--count-allocations`, must end with no heap box live (a program that traps skips that check). A program that uses a
map, a list, a pool, a frozen value, a cown or a behaviour has no executable yet: it fails to compile with `E9900`, and
the runner then runs it under the interpreter alone. A program that schedules behaviours runs under 20 schedules
(seeds 0 to 19), and every one must give the same stdout and exit code and no violation, so the directives of such a
program must say an output that does not depend on the schedule: print only from behaviours that the cowns put in
order, such as one chain, or behaviours on one cown. A test executable runs under a 10 second deadline, and a program
that outlives it fails the test with `timed out`. The summary is `N passed, M failed, K not yet supported`, the
failures are listed, and the exit code is 0 when nothing failed. With `--verbose` a `RUN` line comes before each test, so a crash of the compiler, which ends the run, names the test it was compiling.

`minimaxon test <specs-dir|file.md> [--verbose]` runs maxon's spec tests of `specs/` (`self/driver/spec.maxon`; the format is that of maxon's `docs/SPECS.md`, see `specs/README.md`) in the same process. A test is compiled from memory (a multi-file test as a project of files held in memory, `project.loadFilesInto`) and run once, from an executable of its own in the temporary directory. A compile-error test compares the whole diagnostics text after the fragment's path is made `<fragment>` (and, for a multi-file test, its lines the merged block's), and compares the positions only when the test has no `<!-- positions: moved -->` line; a run test compares the exit code when a block gives one and always stdout and stderr, trimmed (a block that is not there asserts the stream is empty), as maxon's runner does. It prints one line for each failed test (`FAIL file: test: what differed`), the counts, and the skipped tests by reason, the most first; `--verbose` also prints a line for each test (a `RUN` line before it and its result after, so a crash names its test) and every reason. A test is skipped, with a reason it counts, when its source has a `<!-- skip: kind -->` line (the conversion's), when a directive needs what the runner or the compiler lacks (`tier`, `procs`, `preempt`, `process`, `runs`, `network`, `stdin`, `Args`, `env`, the trace and debug directives, `unsupported-targets` naming `x64-windows`), when a result block is a trace or is for another target, when its source stages an overlay of maxon's stdlib or runtime, and when only the interpreter runs the program. The compiled program inherits the runner's standard input.

Directives in comments say what the program must do:

| Directive | Meaning |
|---|---|
| `//> text` | one expected line of stdout, in order (none means stdout must be empty) |
| `// expect-exit: N` | expected exit code (default 0; a panic exits 1) |
| `// expect-error: E3102` | compiling must fail with this code; `E3102 at 7:5` also pins the line and column. A program may have several, and each must be reported (`E9900`, a program that needs something that has no native code yet, is the one error that is not a failure in a program that expects none) |
| `// expect-free: label` | the interpreter must free the program's values in the order of these lines, one value each, as `minimaxon interp --frees` prints them (`string "a1"`, `array`, `record`, `pool`) |
| `// memory-limit: N` | the native run may use at most N megabytes; the runner does not enforce it yet |
| `// native-only` | do not run the program under the interpreter; for a program whose point is what the executable does with memory, which the interpreter runs far too slowly |

### Errors

A diagnostic is `error E….: file:line:col: message`, the layout of the current Maxon compiler, and compiling stops at the
first error in each function. An error raised at a node is located, given its file, line and column, when the compile
reports it. A diagnostic is a property of the program; any other failure of the compiler is a `panic` in `self/`, a
bug in minimaxon and not in the program.

## Philosophy: assume the source is correct

The compiler assumes the program it compiles is correct, so a phase never waits on another module for a check. A check that
needs another module's results is deferred to the latest point where the compiler actually needs the answer, and done there
quickly. For example, a module that calls a function its own module does not declare assumes the function exists in another
module; when the call is finally needed and no matching function is found, that is the error. Correct programs pay nothing for
the checks, and an incorrect one is still refused before it produces an executable.

The compiler is single-threaded today, and the principle decides where each check sits. Name resolution reads only the module's
own declarations, the standard library and the declarations of the modules the module names, because another module's names are
always qualified. A call into another module is typed from the callee's declared return type and recorded (`check.PendingCall`);
the verify stage binds its arguments to the callee's parameters (`check.verifyCalls`), and the bounds of a type argument wait the
same way (`check.verifyBounds`). The range analysis and the ownership pass run in the verify stage, after those, on a module
that found no error there. Each stage visits the modules in dependency order and runs only if every stage before it found no
error. Parallel compilation is planned (see "The modules phase"); the deferral is what lets a module go on to its next stage as
soon as the modules it names have finished theirs.

## Modules

A module is a directory. The project root is the root module, which holds `main`, and every directory
under it that holds source, at any depth, is a module of its own.

- **Another module's names are always qualified**, by the module's path read from the root: `geometry.area()`,
  `7 as geometry.Length`, `net.http.parse()`, `geometry.Shape.circle(2)`. The root module's own declarations are
  `export.X` (`export` is a name only as the start of a qualified name), and `stdlib.X` is a built-in.
- **A bare name** is a local, a field, a type parameter, the module's own declaration or a built-in, and nothing else, so
  adding a declaration to one module never changes how another resolves, and resolving a module reads only the
  declarations of the modules it names. A bare name that only another module declares is unknown (`E3004` for a call,
  `E3011` for a type, `E2004` for a value), and the message names the qualified spellings.
- **Visibility has three levels**, for a declaration, a field or a method:
  - no modifier: the declaring module only;
  - `export`: the tree rooted at the declaring module's parent, as Go's `internal/`. An `export` in `app/net/http` is
    visible to `app/net` and every module under it (`app/net/tcp` too), not to `app` or `app/ui`. In a top-level
    module or the root module that tree is the whole project. It never reaches past the project;
  - `public`: everyone, programs that use the project as a library included.

  Naming a declaration from a module its visibility does not reach is `E3008` (`E3014` for a field), and the message
  says which modifier would reach.
- **A signature may not name a less visible type** (`E3167`): each type that the parameters, return type, `throws`
  clause, `where` clause or `implements` clause of an `export` or `public` declaration name, the type of a field (at the
  narrower of the field's and its type's visibility), a payload (at its union's) and a typealias's target must be at
  least as visible. A method is held to its own visibility, a requirement of an interface to the interface's.
- **One namespace per module:** a name declared twice across the module's files is `E3006`.
- **No import cycles:** module A depends on B when A names one of B's declarations. A cycle is `E2012`, reported at
  where the first module of the cycle names the second. A module that the root names therefore cannot name `export.X`.
- A directory whose name is not an identifier, or a top-level one named `export`, `stdlib`, `runtime` or
  `scriptlib`, is `E3182`.

A module is named by a handle, `diag.ModuleID`, its index in `ast.Project.modules`: the root, then each module's children in
name order, depth first (`project.loadProjectInto`; a directory with no source and no module below it is no module). A single
`.maxon` file is a project of one root module. In a `build` or `run` the runtime (see "The runtime") is module 0 and the
project's root follows it.

## Packages

The compiler is `self/`, a project of Maxon modules, one directory each, and each a phase or a layer, so each can be changed
and tested on its own. Dependencies only point down the list, and the compiler refuses an import cycle (`E2012`), so the
layering cannot erode:

| Module | Holds | Imports |
|---|---|---|
| `diag` | `CompileError` (a code, a message and a place: a `Site`, a module handle and an offset, until the error is located, then file, line and column), `ErrorList`, `sortErrors`, the error codes (generated from `codes.tsv`) and the message functions (from `messages.tsv`) | — |
| `table` | `StringMap` and `IntMap` (open addressing) and `Option`, which nothing built in provides | — |
| `ast` | the program model: `Tree`, a module's nodes as flat `Record`s, the generated node kinds, accessors and declaration structs, `Module` and `Project`, the project-wide `TypeTable` and `FuncTable`, the built-in types and functions (generated), `Resolution` and `Typing`, the per-module tables the phases fill in (see "The AST"), link names, locating an error, `I128` | `diag`, `table` |
| `syntax` | the grammar (`Maxon.g4`), the generated tokens and parser, the lexer loop, the layout rules, the formatter (the parser in a mode that writes the layout instead of checking it) and the edits that rename a keyword | `ast`, `diag`, `table` |
| `project` | loading a project: discovery, finding the runtime's source (see "The runtime"), reading and parsing each module, the declaration tables, the module graph and its import-cycle check | `ast`, `diag`, `syntax`, `table` |
| `resolve` | name resolution, into the module's `Resolution`: which declaration each name and type reference names, which type parameter each constraint binds, which modules the module depends on, visibility | `ast`, `diag`, `table` |
| `check` | the stages of the type checker (`stages.maxon`), the generic bounds, the range analysis that decides what arithmetic can fail, the ownership pass, and what a behaviour may capture, into the module's `Typing` | `ast`, `diag`, `table` |
| `ir` | the IR (fixed-size records, with the printer and the visitors generated from the IR table), its builder, finding the generic instances a module needs, lowering (the primitives' operations, where each value is dropped, the body of each behaviour), which reads the `Typing` of every module it follows a call, a constant or a field's default into, and the runtime's symbol names | `ast`, `diag`, `table` |
| `backend` | the x64 encoder (generated), the assembler and code unit, codegen, the drop and copy routine of each type, the entry stub, the linker and the PE writer | `ast`, `ir`, `table` |
| `interp` | the tracking interpreter: runs the IR of a whole program with the primitives written in Maxon and a simulated scheduler of behaviours, and reports a violation of the memory model | `ast`, `ir`, `table` |
| `fuzz` | the random program generator the `fuzz` command runs | `table` |
| `driver` | the commands (`driver.maxon` is the command line); `compile.maxon` runs the pipeline; the corpus and spec runners, `interp`, `fuzz`, `fmt`, `rewrite`, `qualify`, `rename` and `update_positions` | all of the above |

`main.maxon`, in the root module, runs `driver.run`, and holds the `selftest` command with the compiler's own tests
(`self/*_test.maxon`). The generators are Go, in `internal/`, and `cmd/convert` and `cmd/sample` are standalone tools (see
"Use"); nothing in `self/` depends on them. A module exports only what another module uses.

### Generated code

Code that only restates a fact is generated from the one place that fact is written, so it cannot drift from it or be
written wrong. Each generator is plain Go under `internal/`, reads its table, and writes Maxon files that are checked in and
carry a `DO NOT EDIT` header. `go generate ./internal/...` runs all of them. Each generator's Go test regenerates its outputs
in memory and fails if the checked-in ones differ, and `.\check.ps1` hashes every `*_gen.*` file before and after the generators
run.

| Generator | Source of truth | Writes |
|---|---|---|
| `internal/grammargen` | `self/syntax/Maxon.g4` | `self/syntax/parser_gen.maxon`, `self/syntax/tokens_gen.maxon`, `self/ast/nodes_gen.maxon`, and the keywords `cmd/convert` reserves (`cmd/convert/reserved_gen.go`) |
| `internal/builtingen` | `internal/builtingen/table.go` | `self/ast/builtins_gen.maxon`, `self/ir/runtime_gen.maxon`, `self/ir/intrinsics_gen.maxon`, `self/check/builtins_gen.maxon`, `self/interp/builtins_gen.maxon` |
| `internal/diaggen` | `self/diag/codes.tsv`, `self/diag/messages.tsv` | `self/diag/codes_gen.maxon`, `self/diag/messages_gen.maxon` |
| `internal/irgen` | `internal/irgen/table.go` | `self/ir/ir_gen.maxon` |
| `internal/x64gen` | `internal/x64gen/table.go` | `self/backend/x64_gen.maxon` |
| `internal/abigen` | `internal/abigen/table.go` | `runtime/layout_gen.maxon`, `self/ir/layout_gen.maxon`, `self/backend/layout_gen.maxon` |

**The grammar.** `Maxon.g4` is a subset of ANTLR 4 notation that its header explains. It says how each construct is written
and which AST node it builds: a label such as `cond=expr` is a field of the node, `@ast::Node { ... }` adds the fields later
phases fill in or, written `Name Slot[Counter]`, says that the nodes of that kind are numbered from a counter after the
parse, `@ast::Structs { ... }` names the nodes that are declarations (a struct each) and says by leaving the rest out that
every other node is a record in the module's tree, and a rule whose alternatives are nodes, such as `stmt` or `expr`, is a
closed set of them. From it:

| File | Holds |
|---|---|
| `self/syntax/parser_gen.maxon` | a recursive-descent parser with one function per rule, and a stack for each kind of list the parser builds; operator precedence comes from the order of `expr`'s alternatives. A parse function takes the parser and fails with `SyntaxError` after recording the error; every choice is an `if` chain |
| `self/syntax/tokens_gen.maxon` | the token kinds and the lexer's DFA: every token rule and keyword compiled into one table over byte classes, its tables held as strings (a string literal is in the executable's data, so reading one allocates nothing) |
| `self/ast/nodes_gen.maxon` | the kind of every node, a constructor, a getter and a setter for each field of each record (`newIfStmt`, `ifStmtCond`, `setIfStmtCond`), the struct of each declaration with its constructor (a field named by a keyword takes what it holds as a suffix: `typeRef`, `whereList`, `isNoncopyable`), `numberSlots` for the `Slot[Counter]` fields (nothing calls it yet; see "The AST"), and the reserved words |

The parser decides with one token of lookahead, or two where one is not enough. Where two alternatives start with the same
rule, as an assignment and an expression statement both start with an expression, it parses that rule once and the token
after it decides. A syntax error names what the grammar allows at that point, such as `Expected an expression but got ')'`.
The loop that runs the DFA, the scanner for a string literal (`-> scan`, since interpolations nest), the loop over a module's
declarations and the conversion of a token to a field that is not its text are hand-written in `self/syntax` (`lexer.maxon`,
`parser.maxon`, `lists.maxon`, `source.maxon`).

The lexer takes the longest token at each position: a keyword wins over a name of the same length, and an earlier token rule
over a later one. It never backs off to a shorter match, so input that stops partway through a token, such as `0x` or an
unclosed `/*`, is an error naming the token from its `/** */` description ("incomplete integer"). The generator rejects a
token rule that matches the empty string or that an earlier token always beats.

Code that handles every member of an IR instruction or terminator implements a generated visitor (`ir.InstrVisitor`,
`ir.TermVisitor`, below), so a row added to the IR table fails the build wherever it is not handled. The tree's node sets
(`Stmt`, `Expr`) have no generated visitor; the code that handles them dispatches on `NodeKind` and ends in a `panic`.

**The built-ins** have one table, `internal/builtingen/table.go`: the built-in types, the functions and methods on them with
their signatures and the intrinsic each lowers to, the runtime entries with the unit that defines each, and the functions of
`runtime/` that define the entries (`runtimeFunctions`). From it the generator writes the declarations
(`self/ast/builtins_gen.maxon`), the `Intrinsic` and `Rt` enums, and the dispatch from each intrinsic and entry to its
hand-written body: `checkIntrinsicArgs` in `self/check` calls `check<Intrinsic>Args` for an intrinsic the table marks checked,
`ir.intrinsicCall` calls `lower<Intrinsic>`, and `interp`'s `runtimeEntry` calls `entry<Entry>`. A dispatch names a function
for every row, so a row added to the table fails the build wherever its body is missing.

**The error codes** are a table, `self/diag/codes.tsv`: each row is a constant, its code, the case of maxon's
`ErrorCodeRegistry.maxon` that has that number (or `-` for a code of minimaxon's own, numbered from `E3900`) and what it
refuses. The text of a diagnostic that carries a maxon code is maxon's, so that a spec's expected stderr matches byte for
byte: `self/diag/messages.tsv` holds it, one row per situation maxon words alone (a function name, the code constant and
maxon's message with `{placeholders}`), and `go generate` writes a function per row (`diag.typeReferenceCycle(name, path:)`)
that returns the text while the raise site still chooses where the error is. A situation that only minimaxon refuses, or
that maxon words for a rule minimaxon changed, keeps its own text at the place that raises it.
`go run . --maxon <maxon repository>`, run in `internal/diaggen`, checks every row against maxon's registry.

**The IR** has one table, `internal/irgen/table.go`: a row per instruction and terminator with its operands (each a name, a
kind such as register, slot, block, flag or pool index, and the field of the record that holds it), what it gives (a register,
an optional one or nothing), its effects (reads or writes memory, calls, allocates, frees, leaves the function; one with a
failure block may also fail) and the template the printer writes it by, plus the small enums (`BinOp`, `CmpOp`, `GuardKind`,
`RawWidth`) and the flags. From it the generator writes `self/ir/ir_gen.maxon`: the `Opcode` and `TermKind` enums, the `Instr`
and `Term` records, a constructor and a getter per operand (`binLeft(x)`), the printer (`instrText`, `termText`) and the
visitors. The pools and the texts of a function's parts stay hand-written (`self/ir/function.maxon`).

**The ABI layouts** (what a box of each kind holds, at which offset) have one table, `internal/abigen/table.go`, so the
runtime, the backend and the lowering cannot disagree. Each row is a name, its value and which of the files below get it:

| File | Holds |
|---|---|
| `runtime/layout_gen.maxon` | the word, the empty box, the string, array and byte array boxes, and the first capacity of each buffer, as `Count` constants |
| `self/ir/layout_gen.maxon` | `wordBytes` and `tagOffset`, which lowering sizes records and unions with |
| `self/backend/layout_gen.maxon` | `wordBytes`, the string length offset and the array box, which the drop and copy routines read |

A byte array's box is an array's box (length, capacity, buffer address), so one set of offsets serves both.

**The x64 encoder** is generated from an instruction table, `internal/x64gen/table.go`. A row is a form, one instruction with
one shape of operands, named as maxon's `X64Backend.maxon` names its encoders (`MovRegReg`, `LoadRegBaseDisp`, `PushReg`):

| Column | Holds |
|---|---|
| `name`, `mnemonic` | the encoder is `encode<name>`, and the case of the `X64Form` enum is the name with its first letter lowered |
| `operands` | in assembly order, each a register, a memory operand `[base + disp]`, a `[rip + disp]` operand, an immediate (`imm8`, `imm32`, `imm32U`, `imm64`), a `rel32` or a condition code, and the part of the encoding that holds it: the reg or r/m field of the ModRM byte, or the low three bits of the opcode (`+r`) |
| `prefix`, `rexW`, `opcode` | the mandatory prefix (`rep`), REX.W, and the opcode with its `0F` escape; a condition code is added to the last opcode byte |
| `modrm`, `digit` | no ModRM byte, one the operands fill, or one whose reg field is the form's `/digit` |
| `setsFlags`, `readsFlags`, `readsMemory`, `writesMemory` | what instruction selection asks of a form; written as `X64Form` predicates of the same names |

A register that is used as an 8-bit register is marked, and its encoder writes a REX prefix whenever the register is `rsp` or
above in number, since `spl` to `dil` exist only under one. The displacement of a memory operand is none, a byte or a
doubleword (a base of `rbp` or `r13` takes at least a byte; one of `rsp` or `r12` takes a SIB byte), and `rip` and `rel32`
operands take a displacement that the backend writes as 0 and patches (`lastField32`, `rel32To`, `patch32`, `patchRel32`). A
choice picks the first of several forms whose immediate holds the value (`AddRegImm`: a byte, else a doubleword;
`MovRegImm`: an unsigned doubleword, a sign-extended doubleword, else a quadword), and a sequence is forms the backend always
emits together (`SetccRax`). The generated file holds the register and condition code enums (`X64Register`, `X64CondCode`),
the immediate types, the helpers the encoders share and one encoder per form, choice and sequence, taking a `mutates code
ByteArray` first. `go test ./internal/x64gen` also encodes every form with registers `rax` to `r15`, displacements from none
to 32 bits, bases of `rsp`, `rbp`, `r12` and `r13`, immediates at the limits of each kind and every condition code, with a
reference encoder in the test, and compares the bytes with `internal/x64gen/testdata/assembler.golden`; `selftest` runs the
generated Maxon on a set of those encodings (in `self/main.maxon`) and compares them with the same bytes.

## Pipeline

`self/driver/compile.maxon` runs the pipeline for `build`, `run`, `check` and the test runner, one stage after the other over
every module, on one thread. A stage is a barrier: it visits the modules in dependency order (a module after the modules it
names), and the next stage runs only if the error list is still empty, so the errors reported are those of the first stage that
has any, from every module. A `check` stops short of code generation but runs the stages that can report an error.

| Step | Module | Work |
|---|---|---|
| runtime | `driver`, `project` | for a `build`, `run` or test, the runtime (`runtime/`, see "The runtime") is loaded as module 0, run through the steps below and emitted once (`prepareRuntime`); a test run keeps it and adds each program to a copy of the project (`SharedRuntime`) |
| discover | `project` | `loadProjectInto` scans the directory tree (names in sorted order; `E3182` for a directory that is no module name) and makes a `Module` for each directory; a single file is one root module |
| parse | `syntax`, `project` | reads each module's files into one buffer, lexes it and parses it into the module's `Tree` and `decls`; a lexical error ends the module's lexing, a syntax error abandons the rest of its file. Then `buildModuleTable` gives each module's declarations their names (a duplicate is `E3006`) and a shell type (a `TypeID` with no members yet) for each type declaration |
| resolve | `resolve` | `resolveFrom` binds every name, struct literal and type reference to its declaration, and each `where` constraint to the type parameter it names (`E3916`), into the module's `Resolution`; it records the modules the module depends on, checks visibility (`E3008`) and that no signature names a less visible type (`E3167`), and rewrites a qualified name in place (see "The AST"). It reads only the module's own declarations, the standard library and the declarations of the modules the module names |
| module graph | `project` | `newGraph` turns each module's recorded dependencies into edges; `checkAcyclic` sorts the graph (Kahn's algorithm, linear in the edges) and reports `E2012` where the first module of a cycle names the second; `topoOrder` is the order every later stage visits the modules in |
| aliases | `check` | `stageAliases`: the bounds of each type's type parameters, then each typealias of an instance, after the typealiases it names (a cycle is `E2012`) |
| declare | `check` | `stageDeclare`: the module's fields, cases, function signatures, type parameters and the interfaces its types implement, into the project's `TypeTable` and `FuncTable`; the root module's declare finds `main` (`E3001`) |
| constants | `check` | `stageConstantOrder`, `stageConstants`: top-level `let` types, each after the constants of its module that it names (a cycle is `E2012`) |
| bodies | `check` | `stageBodies` types every type declaration and function body, a generic one once against its bounds, and the body of a behaviour as a function of its own that captures the variables it uses. A call into another module is recorded, not bound (`PendingCall`); a call that needs an instance of a generic is recorded on its function (`callers`) |
| verify | `check` | `stageVerify`: binds the recorded cross-module calls (`verifyCalls`) and checks every type argument against its parameter's bounds (`E3017`); then, for a module that found no error in this stage, the range analysis, which decides how each operator and cast can fail and requires a `try` around what can (`E3057`, `E3059`; see "Checked arithmetic"), and the ownership pass: moves, conventions, borrows, exclusivity, initialisation, frozen values and what a behaviour takes (`E3102`, `E3900` to `E3904`, `E3197`, `E2013`) |
| lower | `ir` | `lowerModule` for each module in order: it first finds the instances of generic functions that the module needs (`findInstances`: it starts from the calls recorded by its functions that are not generic, and follows each instance's recorded calls, whichever module declares the generic, with the type arguments bound, until no new instance turns up; `E3906` if the type arguments grow without end), then lowers the module's functions that are not generic, in source order, and the instances, in the order found. A `check` only finds the instances. A place that needs something the backend cannot run yet is noted as a refusal, and the driver reports the first of each function (`E9900`) before codegen |
| emit | `backend` | `emitModule`: IR to x64, one code unit per module, calls between units symbolic; an instance that several modules need is emitted by each, and the link keeps one. The entry stub and the runtime's unit come first; `emitTypeRoutines` then makes the drop and copy routine of every type the units asked for, and of every type those reach (a record's fields, a union's payloads, an array's elements), in waves, ordered by where the modules first ask for them |
| link | `backend` | `link` keeps only the sections of code and the pieces of data that the entry reaches through symbol and data references (every function is compiled; what nothing calls is dropped here), lays the units out in order, patches the references and `writePeImage` writes the executable. A symbol is its link name (see "Link names"); the one symbol that may be defined more than once is an instance of a generic function, and the table keeps the first unit's definition, so the copies are never reached and drop out like any dead section. Any other symbol defined twice is an internal error |

**Errors.** A stage that finds an error ends the compile after the stage. Within a stage every module is visited, so one run
reports the errors of all modules. An error raised at a node is a `diag.Site` (module handle, offset); when the compile ends
`ast.locateAllErrors` gives each its file, line and column, and then sorts them by file, line, column, code and message, so
the output does not depend on the order the modules were visited in. A compile that finds no error never builds a line
index. The message of an error about a bare name that was not found names the other modules that declare that name, which only the
project as a whole knows, so it is written when the error is located (`ast.locateError`). A `panic` that is not a
`diag.CompileError` is an internal compiler error.

`interp` runs the same stages through lower, with no runtime module and without refusing what only the interpreter runs
(`lowerForInterpreter` in `self/driver/interp.maxon`), and gives the IR to the interpreter instead of codegen. This is how a map,
a list, a pool, a frozen value, a cown and a behaviour run.

### The modules phase (planned)

Not implemented: the compiler visits modules one stage at a time, on one thread. The design is to run each module's stages from
parse to emit as tasks of one scheduled graph over a pool of workers, as Go's build does (Phase 3 of the plan). A task waits
only for the module's own stage before it and for the stages of the modules it names whose results it reads, never for every
module to finish a stage, and a worker that finishes a task goes on to the module's next stage while its data is in cache,
rather than sweeping every module through one stage and then the next. What each stage reads of another module is what lets
it start early: resolve reads only the declaration tables of the modules a qualified name passes through; declare needs only
the type objects of other modules' types, since what it needs of their declarations is checked in verify; a constant waits for
the constants of the modules it names; a check reads the declarations and constants of the modules it reaches. The module is the
unit of parallelism, tasks write only their own module's slots, and results are merged in module order, so the output and the
errors stay those of the barrier order above and do not depend on the schedule.

### Reading a module

A module's files are read into one buffer, a string allocated once at the sum of their sizes (`syntax.parseModuleFiles`):
an offset in it is where a byte is in the module's text, and `ast.FileSpan` says where each file starts and ends. A token is its kind,
its start offset and its length (`syntax.Token`) and carries no text, value or position; the parser reads them from the
buffer when it needs them, so lexing copies no text and needs no intern table. Offsets are 32-bit, so a module's source is
at most 4 GB (a larger one is an internal error asking to split the module); a token is at most 16 MB.

A node's place is its token's offset and its module's handle (`diag.Site`, from `Tree.posOf`). The line and column are found by
scanning the file's text from its start when something asks (`ast.locateInFile`), so a program that compiles never computes
one. The lexer works on the raw UTF-8 bytes, so a column counts bytes.

One lexer pass covers the whole buffer, file by file: at each file boundary it resets the bracket depth and the indentation,
so no token spans two files, and emits a `fileEnd` token. It reads a file line by line: it checks each line's indentation and
emits `indent` and `dedent` where it changes, checks blank lines and comment lines, and runs the DFA over each code line,
stepping over the one space between two tokens without a round of the DFA. Comments are `-> skip` token rules, so the lexer
drops them. One parser reads the module's stream, naming each file in its diagnostics. A syntax error abandons the rest of its
file (`E2007` for a declaration left open at the end of a file), and parsing resumes at the next file, so each file reports its
own first syntax error. The same parser, in a mode that writes the layout instead of checking it, is the formatter
(`syntax.formatSource`).

### The AST

A module's tree is its `ast.Tree`: its nodes are fixed-size `Record`s in one `Array`, and what a record does not hold itself is
in one `Array` of 32-bit list words and in the module's text. A phase never writes a node to tell another phase something:
what it finds out is a table it owns, indexed by `NodeID`. Nothing adds a node after the parse except resolution (below).

**Handles.** A reference is an index, never a shared pointer, so each part of the model has one owner:

| Type | Is |
|---|---|
| `diag.ModuleID` | a module: its index in `Project.modules`; `diag.stdlibID`, -1, is the standard library, whose declarations (the built-ins) are `Project.stdlib` |
| `ast.NodeID` | a node of one module: its index in `Tree.records`, counted from 1 in the order the parser makes them; 0 is no node |
| `diag.Site` | where a node is: its module and the byte offset of its first token |
| `ast.DeclRef` | a top-level declaration: its module and its `DeclOrdinal`, its index in `Module.decls` |
| `ast.TypeID`, `ast.FuncID` | a type or a function: its index in the project's `TypeTable` or `FuncTable`, which every module shares; 0 is none (`noType`, `noFunc`) |
| `ast.LocalID` | a local variable: its index in the module's `Typing.locals`; 0 is none |

A node's index depends only on its module's text, so a module that did not change keeps its indexes and tables. What is
project-wide is the module handle and the type and function handles.

**Records.** A node that is not a declaration is a record:

```
Record { kind NodeKind; flags Byte; aux U16; start U32; a U32; b U32 }
```

`start` is the node's offset. The grammar says everything else, and the generator lays each node out from its fields: a bool is
a bit of `flags`, a choice among literals (an operator, a convention) a value of `aux`, a name, a label or the start of a
string one word that holds its offset in the module's text plus one (a name runs as long as its identifier characters do, a
label to its closing quote, so a record holds no length), an integer of up to 64 bits two words, a child the `NodeID` of its
record, and a list or a block the index of its count in the list array, with 0 the empty list. A node with up to two such words
keeps them in `a` and `b`; a node with more keeps the first in `a` and the rest in a block of the list array that `b` points
to. The list array holds the lists in the order the parser finishes them, each its count followed by that many words, and the
blocks. A string literal's parts are pairs of words: an interpolated expression, a run of the text, or a text that an escape
made (kept in `Tree.strings`). Code reads and writes a record through the generated functions of its kind
(`ifStmtCond(tree, id)`, `setIfStmtCond(tree, id, value)`) and asks `tree.kindOf(id)` what a node is.

**Declarations are values.** The declarations (`@ast::Structs` in the grammar: functions, constants, the type declarations and
what only they hold, such as parameters, fields, cases, bounds, type parameters, constraints and interface signatures) are
structs, held in the `ast.Decl` union in `Module.decls`; each has the `NodeID` of its record, and holds `NodeID`s of the records
it has and never a pointer to one. A function that checking makes, the body of a behaviour, has a `FuncEntry` and no
declaration.

**Resolution rewrites in place.** `a.b.c(x)` where `a.b` is a path of modules parses as members, and resolution makes the member
that ends the path the name of the declaration, by changing its kind in place (`Tree.becomeName`): it keeps its `NodeID`, so the
member of `c` above it still holds it, and the module has no node more than the parser made. A behaviour that takes a variable
of the function around it moves the variable in through a node that stands for the name at the `when`
(`Tree.appendCaptureRefs`, one copy of the name for each behaviour around it, after the last record). Resolution is the only
phase that adds a node, and every phase after it finds the tree complete.

**Side tables.** A table is owned by the per-module result and filled by one phase. `Project.resolutions[m]` (`ast.Resolution`)
is what resolve decided about module `m`: which declaration each name, struct literal and type reference names, which type
parameter each type reference and constraint binds, the modules the module depends on (sorted by handle, each with the first
place it is named), the variables each behaviour captures and the bare names no declaration has (`unbound`, which `qualify`
reads). `Project.typings[m]` (`ast.Typing`) is what the check decided about the bodies. The type of every node is a flat array
indexed by `NodeID` (`NodeTypes`, `noType` where nothing was recorded); the facts that only some kinds of node have are
`table.IntMap`s keyed by `NodeID`:

| Table | Node | Entry |
|---|---|---|
| `arith` | a binary or unary operator, a cast | the operand and word types, and how the operation can fail, which the range analysis decides (`failOverflow`, `failDivZero`, `failShift`, `failRange`, `failBelow`, `failAbove`) |
| `names` | a name written as an expression | what it stands for: a local, a constant, or a field of `self` |
| `members` | a member access | the kind (field, enum case, union case, ...), the field, the case, the method |
| `calls` | a call | the kind, the target `FuncID`, its type arguments, the parameters with their conventions, the arguments in the order of the parameters, the union and case a constructor builds, the destructor call of a `dropWith`, the intrinsic |
| `structLits` | a struct literal | the record type and the field values in order |
| `patterns`, `arms` | a match pattern, a match arm | the case and a `LocalID` for each binding; the kind of arm |
| `whens` | a `when` | the function the body became, the cowns and what it captures |
| `iterations` | a `for` over a type with the iteration protocol | the calls of `createIterator()`, `current()` and `advance()`, and the cursor's type |
| `nodeLocals`, `locals` | a `let`, a `for`, a `try` | the `LocalID`, and the `LocalEntry` it indexes (name, type, `var` or parameter, convention, its slot in the function's IR) |
| `jumps` | a `break`, a `continue` | the `NodeID` of the loop or match |
| `consumed` | an expression | set by the ownership pass where the value changes owner; lowering reads it and nothing else |
| `funcUses`, `fieldUses` | a function, a field's default | the calls in it that need an instance of a generic, which `findInstances` follows, and the `for`s whose protocol calls do |

The grammar's `Slot[Counter]` fields (`Local`, `Jump`, `When`, `Pattern`, and the members, calls, operators, names and struct
literals) generate `numberSlots`, which numbers the nodes of each kind from 1 so that a table for those kinds could be a dense
array; no table uses it yet. Reading an entry that does not exist (a node of a kind with no table, or an expression that was
never typed) is an internal error and not a default.

The `Typing` of a module is read with the module of the node: lowering follows a call into another module's generic, a constant
into the module that declares it, and a field's default into the record's, and reads each through `ir`'s accessors (`exprType`,
`callInfoAt`, ...), which take the module handle. Every module a module reaches has finished its check before the module is
lowered. `consumed` is written by the ownership pass while it verifies the module, and lowering only reads it.

**Types and functions.** A type is a `TypeEntry` in the `TypeTable`: its kind (`record`, `enumTy`, `unionTy`, `intRange`, the
built-in collections, `typeParam`, ...), name, declaration, fields, cases, methods, bounds and type arguments. An instance such
as `Box with Count` is one type wherever it is written: `instantiateType` interns it by its origin and arguments
(`instCache`), reads the declaration's fields, cases and methods and substitutes the arguments. Integer ranges are interned by
their bounds. A function, a method and an instance of a generic function are `FuncEntry`s in the `FuncTable`, interned the same
way. Declare fills them in; every later phase reads them.

### Allocation

In compiled Maxon every record, union and `String` value is a heap box and an `Array` holds words, so a copy allocates and the
compiler's own data is laid out to keep copies down (`.claude/skills/write-self` has the measurements):

- A module's nodes are small fixed-size `Record`s in one `Array` and its list words in another, referenced by index, and the
  text of a name or a string is a part of the module's buffer that the node points at, not a copy. A list in the grammar
  (`stmts+=stmt`, `args+=arg`) is pushed on a stack of its own as it is parsed and moved out once into the list array.
- A fact that every node has is an array indexed by `NodeID`; `table.IntMap` and `table.StringMap` are for sparse keys.
- A value is read in place where it can be: `ast.declName` and `ast.declLevel` read one field of a declaration where it is,
  because `IntMap.get` and a record read out of a table each copy. Hot paths take `mutates` parameters and borrow.
- A function's IR is one list of fixed-size `ir.Instr` records and a few pools, and a block is a range of that list.

### Link names

A code unit names the functions it defines and calls, the data it defines, and the routines it asks for by text, never by an
address or by a number that counts the project's modules, so a unit means the same in any compile. That text is a **link name**
(`ast.funcLinkName`, `ast.typeLinkName`). It starts with the module that declares what it names: its path, `export` for the
project root (a directory cannot be called that), `runtime` for the runtime and `stdlib` for the standard library. So two
modules may each declare a `helper` or a `Counter`, and an instance applied to one of them is not the instance applied to the
other:

| Names | Link name |
|---|---|
| a function | `geometry::area`, `export::main` |
| a method | `export::Counter.inc`, `(export::Box with stdlib::Count).make` |
| an instance of a generic function | `export::swap[stdlib::Count, export::Counter]` |
| a type | `export::Counter`, `export::Box with (geometry::Shape, Count=int(0 to 18446744073709551615))` |
| a drop or copy routine | `drop export::Counter`, `copy stdlib::Array with export::Tree` |
| a runtime entry or global | `runtime::alloc` (the link name of the runtime function that defines the entry), `rt.entry` and `g.heap` (what the entry stub and the linker define) |

An integer type is named by its alias and its range, since the code for it depends on nothing else. The kinds of symbol cannot
meet: a function's name has `::` before any space, and a routine's starts with `drop ` or `copy `. The text of a function in
the IR (`--emit-ir`) and in a message is its plain symbol (`ir.funcSymbol`).

## Build cache (planned)

Not implemented: every compile redoes every module, and there is no cache directory, `cache` command or `--no-cache` flag (they
are refused, see "Use"). The design is a per-module cache, as Go's build has, for a later phase (Phase 4 of the plan). The
decisions to keep:

- **An entry is a module's code unit**, the output of its emit: the code, the data, the references and definitions to link, the
  sections and pieces the link keeps or drops whole, and the drop and copy routines it asks for, each by link name. A module with
  an error or a refusal has no entry, so a diagnostic is always made, and made the same way, by the compile that reports it. The
  front (parse to constants) always runs for every module, so every error it can report is reported as without a cache; check
  and emit are where a hit saves its work. The executable is byte for byte the one a build with no cache makes.
- **The key** is a SHA-256 of: the compiler (the hash of its executable), the flags that change what a module compiles to
  (`--count-allocations`; not `--emit-ir`), the module's link path, a digest of its source, and **the export summaries of the
  modules it depends on**, in module order. Handles, the directory a project is in and the schedule are not in a key.
- **The export summary** is the one place that says what a module shows to the others, written with link names and no handle:
  every type declaration whole (a signature that mentions a type lets a module that cannot name it read its layout), each
  function and constant other modules can use with its signature or type, and the source of what another module lowers in its own
  unit: a generic's body, a constant's expression, a field's default. A summary is transitive: it folds in the summaries of the
  modules it depends on, because a signature mentions their types and a generic's check results are made from what its module
  reaches. So an edit to a private function body of a module with no generic changes no summary and misses only that module.
- **What a hit skips.** A hit does not emit. Its check runs only if a module that is not a hit reaches it and its summary says
  that module reads its check results (it has a generic, an exported constant or a field's default); a miss reaches the whole
  closure of the modules it depends on, since it can instantiate a generic that a signature of a module it names leads to.
  Errors stay as without a cache because a module has an entry only if its check and emit found nothing, and the key holds
  everything its check reads outside its own text.
- **Storage** is a directory of files named by key, each written whole to a file of its own and renamed over its name, with the
  layout version and a checksum, so a damaged or half-written entry is a miss; entries unused for days are trimmed, as Go's build
  cache does.

A language server needs more of the same: to keep a module's products when it did not change (its tree and tables are indexed by
`NodeID`, which depends only on the module's own text, so only the `ModuleID`s in them need renumbering when modules are added
or removed), to cache a failing module's diagnostics under the same key, and to use the export summary to tell whether a module's
dependents have to be looked at again.

## The IR

The checker works on the AST, which mirrors what the author wrote. Everything after it works on the IR in `self/ir`, which is
what a later pass such as reference counting or an optimization changes.

- A function (`ir.IrFunction`) is a list of basic blocks, each a run of instructions ending in one terminator (`jump`,
  `branch`, `switch`, `ret`, `trap`, `throw`). The instructions of every block are one list (`instrs`), block after block, and
  a block is the range of it that it holds (`Block.first`, `endAt`) with its terminator beside the range.
- Instructions read and write virtual registers (`%3`). Locals and temporaries live in numbered slots (`s0`). It is not SSA.
- Every slot and register is `scalar` or `managed`, so a pass can find every reference to a heap object.
- Checks are explicit, and only the ones the range analysis could not rule out are there (see "Checked arithmetic"): a range
  check, a divisor that may be 0 and a shift count that may be negative are each a `guard` with its bound, and a `bin` or a
  `neg` whose result may not fit the word is checked. Each of them names the block it goes to when the check fails, `else b4`,
  which is the only way an instruction leaves its block before its end: there the failure drops what it leaves and goes to the
  handler of its `try`, or returns the error with `throw`. A function that throws returns its error beside its value, 0 when it
  returned and the error's case plus 1 when it failed (`rdx` in the executable), and a `call` of one names the register that
  receives it (`error %7`). An allocation is `alloc`.
- An instruction is one fixed-size record (`ir.Instr`): an opcode, three registers, a small index, an immediate and two bytes
  (`sub` and `flags`) whose meaning the opcode says (the IR table, `internal/irgen/table.go`, has it, and the getters of each
  opcode such as `binLeft(x)` read the fields by their operands' names), and a terminator is a record of its own (`ir.Term`).
  What does not fit a record (a type, a callee, a string, the arguments of a call, the cases of a switch) is in the function's
  pools (`args`, `types`, `callees`, `strings`, `cases`), which an instruction indexes; a block, a register, a slot and a case
  are indexes too. The opcodes are a closed set, and a pass that handles them all implements `ir.InstrVisitor` and
  `ir.TermVisitor` (`ir.visitInstr`, `ir.visitTerm`), so an opcode added to the table fails the build there.
- A function's IR is made by an `ir.Builder`, which collects the instructions in the order they are emitted, whichever block
  they are for, and `finish` lays them out block after block (a block nothing reaches is dropped and the others are numbered
  from 0). A `when` body is an `IrFunction` of its own in `behaviours` of the function around it.
- `--emit-ir` prints a module's IR (`ir.moduleIrText`).

| Group | Instructions |
|---|---|
| values | `const`, `str`, `load` and `store` (a slot; `store ... borrow` for a borrow), `addr` (a slot's address), `param`, `field`, `setfield`, `fieldaddr`, `loadat`, `storeat`, `global` |
| arithmetic | `bin` (add, sub, mul, div, mod, and, or, xor, shl, shr; `.u` for unsigned; checked ones have an `else` block), `cmp`, `neg`, `not`, `guard` |
| calls | `call`, `runtime` (a runtime entry, with the `routine` a cown's entry stores), `alloc`, `when` |
| ownership | `drop`, `dropslot`, `dropat`, `move`, `moveat`, `copy` (see "Drops") |
| the runtime only | `loadraw.W`, `storeraw.W`, `globaladdr`, `import` (see "The runtime") |

Codegen is deliberately naive: every register and slot has its own frame slot, so each instruction loads its operands and stores
its result. That makes executables about twice the size of keeping values in registers; register allocation is the next step
there. A frame larger than a page touches each page as it grows, as Windows requires. Calls use an internal convention:
arguments are pushed left to right and the caller pops them. The Win64 ABI applies only to the DLL imports that the runtime calls
(`import`).

Records and union values are heap boxes of 8-byte words (a union's first word is its case index), allocated with `HeapAlloc` on
the process heap (`runtime::alloc`) and freed with `HeapFree` (`runtime::free`) where their owner dies (see "Drops"); a box is
zeroed when it is allocated. A string is a pointer to `[length][UTF-8 bytes]`: a literal lives in the executable's data section
and is never freed, any other string is a box on the heap. An enum value is its case index.

### The primitives in the IR

An `Array`, a `Map`, a `List` and a `Pool` (see "Primitives") are heap boxes that only runtime entries touch, so the IR holds no
layout of them: an operation is a `runtime` call of a named entry (`internal/builtingen/table.go`), and the interpreter and the
runtime each implement the entries. The element is always visible in the call:

- An entry that takes an element **moves it in** (`rt.array.insert`, `rt.map.insert`, `rt.list.pushBack`, `rt.pool.insert`), one
  that returns an element **moves it out** (`rt.array.remove`, `rt.list.popFront`, `rt.pool.remove`), and `rt.<primitive>.elem`
  returns the **address** of an element: a borrow, valid until the next call that changes the primitive. A subscript is that
  call and a `loadat` or a `storeat`, and a `mutates` argument `a[i]` is the address itself. A value that changes the primitive
  is computed before the address is taken, which is what the ownership pass's exclusivity rules guarantee at the source.
- An array whose element is an integer that fits in a byte (`ByteArray`, `Array with Byte`) packs one byte to an element, so it
  calls the `rt.bytes.*` entries instead of `rt.array.*`: `rt.bytes.get` and `rt.bytes.set` in place of the address of an
  element, and its own `insert`, `remove`, `append` (a copy of the bytes), `appendStr` and `toString`. `ast.packsBytes` decides
  it and `arrayPacked` (`self/ir/arrays.maxon`) is the one place lowering asks, so every array operation goes through it. A byte
  has no word address: a `mutates` argument `a[i]` is a slot that holds the byte, which is stored back into the element after the
  call returns, on its failure path too.
- An entry that is given an index, a key or a handle that names nothing traps. The queries that cannot miss
  (`rt.<primitive>.len`, `rt.map.has`, `rt.pool.contains`, `rt.list.contains`) are what lowering tests before it calls an entry
  that would trap, failing with the operation's error when the test fails (see "Primitives").
- A handle is a word, never 0. An entry that returns one returns 0 for none, where the operation fails.
- **The OS entries** (`rt.os.*`) take and give Strings and arrays, never a record or a union: lowering takes the String out of a
  `FilePath` or an `Executable` and makes the `FilePath`s and the `CollectedOutput` from what the entry gives, so no entry knows
  the layout of a record. An entry that can fail takes last the address of a word, a slot of the caller's, which it sets to the
  case plus 1 of the operation's error or to 0, and lowering branches on it as on the error of a throwing call. The runtime
  tells a failure from the Win32 error under it (`ir.failureOfWin32`: not found, access denied, busy, already exists, or
  another), and each operation names the case of its own error that each failure is (`ir.failureCase`).
- `clone()` is one instruction, `%r = copy %s T` (see "Drops"), whatever `T` is: a record, a union, a string or a primitive.
  What it does to each part is code that depends on the type, so it is a routine of its own for each type that the backend makes
  (below) and the interpreter does by the shape of the value; a copy of an array copies every element by its own type, and a
  copy of a pool or a list keeps its handles valid.
- A `Handle with T` is `scalar`; everything else here is `managed`.

**Native.** The array, the byte array and the OS entries have code: the entries are functions of `runtime/` (`arrays.maxon`,
`bytes.maxon`, `os.maxon`, `subprocess.maxon`) that the program links like any other function. An array is a box of three
words, the length, the capacity and the address of its elements, which are 8-byte words; `rt.array.insert` grows it by doubling
and shifts what follows, and frees the buffer it leaves (the elements moved to the new one, so only the buffer goes), and
`rt.array.remove` hands the element it removes to the caller, who owns it. A byte array has the same three words, with a buffer
of one byte to an element; its copy and drop routines copy and free the buffer whole. A map, a list and a pool are lowered all
the same, which is what the interpreter runs, but the driver refuses a function that needs one before it generates code:
`E9900`, at the first use in each function. A function that only drops, copies or holds a value that contains one (a `consumes`
parameter of type `Map with (K, V)`, or a record with a pool in it) is refused too, at the function, since there is no routine to
free it.

### Frozen values, cowns and behaviours in the IR

A frozen value and a cown are boxes with a count of owners, so they are the one kind of value that more than one place owns
(`internal/builtingen/table.go`):

| Entry | Does |
|---|---|
| `rt.frozen.freeze(v)` | moves `v` into a new box with a count of 1 |
| `rt.frozen.get(f)` | lends what `f` holds, valid while `f` is held: how every read through a frozen value begins |
| `rt.share(f)` | the same box, with the count raised by 1: another owner, which is `share` and the `clone()` of a frozen value or a cown |
| `rt.cown.new(v)` | moves `v` into a new cown with a count of 1; with `routine d` the cown also stores the destructor `d`, which ends `v` when the count reaches 0 instead of a drop |

Dropping an owner (`drop`, `dropslot`, `dropat` of a `frozen with T` or `cown with T`) lowers the count by 1, and the box is
freed with what it holds when the count reaches 0. A scalar can be held (`cown with Count`, `frozen with T` with `T` a scalar in
an instance of a generic).

A `when` is one instruction, `when body cowns [%a, %b] captures [%c]`. The cowns are read, not moved, and each gains a count of
its own for as long as the behaviour is waiting or running. Each capture is a value that moves in: a managed one was moved out
of its owner by the instruction before, so it is in flight. **The body is a function of its own**, in `IrFunction.behaviours` of
the function around it and printed after it: its parameters are the access to each cown's value (an address, as a `mutates`
parameter has), then the captures, which it owns as a `consumes` parameter does and drops at its end. It returns nothing, and
nothing waits for it. Its name is the enclosing function's with `$when` and a number, in source order (`transfer$when0`,
`swap[String]$when0` in an instance, `outer$when0$when0` for one inside another; `ast.behaviourName`), so a generic function that
holds a behaviour has one for each instance, lowered with the instance's type arguments.

**Native.** None of it has native code: green threads, a scheduler and atomic counts are not built (see
[docs/regions.md](docs/regions.md)). The driver refuses a function that freezes, shares, thaws, makes a cown or schedules a
behaviour, or reads through a frozen value, with `E9900` like a map, a list or a pool. `freeze` alone could be native, since
nothing is counted or freed there yet, but a program that freezes and nothing else cannot show anything about the rules, and
refusing every one keeps the backend free of a half of the feature.

### Drops

The IR says where every owner dies. Lowering emits a drop wherever the rules of "Ownership" free a value, and both the
interpreter and codegen free exactly there.

| Instruction | Drops |
|---|---|
| `dropslot s2 T` | what slot `s2` owns, if anything, and leaves it empty: a local at the end of its block, on every way out of it (fallthrough, `return`, `break`, `continue`), and the old value of a local that is assigned |
| `dropat %a T` | what the address holds, if anything: the old value of a field, an element or a `mutates` or `sets` parameter that is assigned |
| `drop %r T` | the value in a register: a temporary that nothing took, an element removed and discarded, the key and the value that a failed `Map.insert` or `Map.replace` was given, the value given to `array.filled` |
| `%r = move s2` | reads a slot and leaves it empty: a whole local that moves (a binding, a `consumes` argument, a return, a field, a branch of a conditional) |
| `%r = moveat %a` | reads the value at an address and leaves it empty: the payload that leaves the box of a union that `match consumes` takes apart, the String that leaves each element of the array `Directory.list` gives, for the `FilePath` made of it to take its place, and the value that `exchange` takes from a place |
| `%r = copy %s T` | a deep copy of the owned value in `%s`, which stays where it is: a new box for each record, union and array inside it, a new string for each string that is not a literal, and another owner of the same box for a frozen value or a cown |

`T` is the type with an instance's type arguments applied, so codegen can call the drop routine of that type. Dropping a value
drops what it owns, deeply by type: the fields of a record, the payload of a union, the elements of a primitive. A borrow is never
dropped: a `let` or `mutates` parameter, a projection, and a `match` binding of a payload, which is stored with
`store s3, %r borrow`. A place that holds nothing, because it was moved from or never assigned, drops as a no-op, so one drop at
the end of a scope serves every path to it and no analysis of paths is needed. A function that drops a value that holds a map, a
list, a pool, a frozen value or a cown is refused natively like one that makes it.

- **One place decides what moves.** The ownership pass marks each expression whose value changes owner (`ast.Typing.consumed`):
  a binding, an assignment, a `consumes` argument, a return, a field or a payload, a branch of a conditional or a match.
  Lowering reads that mark and nothing else. A consumed place is moved out of, and the pass refuses every consumed place that is
  neither a whole local nor a field of one; a consumed fresh value goes to its new owner; a fresh value that is not consumed is a
  temporary. The values a declaration holds outside any function, a constant and a field's default, are checked by the same pass
  for the same reason.
- **Scopes.** A block owns the locals declared in it, and each statement, and each branch of a conditional or a match arm, owns
  the temporaries it makes: they are dropped when it ends, newest first, and a jump that leaves several scopes drops all of them
  (`ir.pushScope`, `popScope`, `unwind`). A condition drops its temporaries before it branches.
- **Sink parameters** are copied into a slot at entry (`OwnerSlot`), which owns them from there, so the callee can move on or
  drop them like a local. A `consumes _` parameter is dropped too.
- **A string is owned like any value.** A literal and the name of a case are immortal and never freed (a drop of one is a no-op,
  found by its address natively), but an interpolation makes a new string, and so does `clone()` of a string that is not a
  literal; the copy of a literal is the literal.

### Native freeing

The backend turns the drops into code, and the IR needs nothing more than what it says: an empty place is 0, a drop of 0 does
nothing.

- **A move empties its source.** `move` and `moveat` load the value and store 0 where it was, and every frame starts with its
  managed slots zeroed (one store each, or `rep stosq` over the slots of a frame with more than six), so a slot that was moved
  from, or never assigned, or whose `dropslot` has run, holds 0. That includes a slot that a loop body declares: its `dropslot`
  at the end of the body leaves it empty for the next pass. `dropslot` and `dropat` leave the place 0 too. A register is always
  written before it is dropped on every path, so a frame does not zero registers; the interpreter reports a drop of a register
  that holds nothing, which is how a hole in that would show.
- **A routine for each type.** `drop` of a type `T` calls the drop routine of `T`, and `copy` its copy routine; each is generated
  once for the program, for each concrete type (after monomorphization) that the code drops or copies and each type those reach
  (`backend.emitTypeRoutines`); the link drops a routine that only unreachable code calls. A drop routine takes the value (0 does
  nothing) and drops each managed word of it by the routine of its type, then frees the box: a record drops its managed fields,
  a union switches on its case index and drops the managed words of that payload, an array drops each element, then frees the
  element buffer and the header, a string frees its buffer unless it is a literal. A copy routine allocates the box the way
  lowering sizes it, and copies each word, a managed one by the copy routine of its type. A routine calls the routines of its
  parts and nothing is inlined, so a type that contains itself through an array (a tree) is a routine that calls itself. A drop
  of a record or a union with nothing managed in it is `runtime::free` itself, with no routine; a scalar, an enum and a `Handle`
  have none and are never dropped. The routines are named by their type's link name and ordered by where the modules first ask
  for them, never by a map.
- **Literals are immortal, found by address.** Every string literal and the name of every case is in the executable's data
  section, and a string on the heap never is, so the drop of a string compares its address with the bounds of that section
  (`g.imageDataStart` and `g.imageDataEnd`, which the linker defines) and does nothing for one inside. The copy of a literal is
  the literal, as in the interpreter. The alternative, a flag in the string's header, would cost a word in every string and a
  mask on every read of its length; a range check costs nothing until a string is dropped, and any compiler that puts constants
  in an image section can do the same.
- **Free** is `HeapFree` on the process heap, behind `runtime::free`, which also skips 0 since `HeapFree` does not accept it.
- **Counting allocations.** The runtime counts the heap boxes that are allocated and not freed in every executable
  (`g.live`: `runtime::alloc` adds 1 and `runtime::free` subtracts 1), so the runtime is the same code whatever the build.
  `--count-allocations` changes only the entry stub, which then passes `main`'s exit code to the runtime's `checkLeaks`: when
  the count is not 0 the executable writes `leaked allocations: N` to standard error and exits with 99, in place of `main`'s
  own exit code. `minimaxon test` and the program generator always use it, so every native run of the corpus ends with no box
  live, a literal not being one. A panic and a trap exit at once and skip the check, because they end the program with values
  still held; a run test that traps still shows no double free or crash, but not that it freed everything.
- **Depth.** A routine calls itself, or the routine of a part, once for each level of nesting, so dropping or copying a value
  nested a few million levels deep (a chain of that many nodes, each holding the next in an array) overflows the 256 MB stack,
  with a `panic: stack overflow` line. A routine keeps only the words it uses in its frame, to push that out.

### The interpreter

`self/interp` runs the IR of a whole program, with the primitives written in Maxon (an array, a map, a list and a pool, each with
the generational handles that "Primitives" describes; a frozen value and a cown, with their counts) and a simulated scheduler of
behaviours, and tracks the heap as the memory model describes it. It is for tests and development: it is never part of a
compiled program, and the driver reaches it only from `minimaxon interp`, the test runner and `fuzz`. It has no types; it works
from the shape of the values and from the drops the IR says. `minimaxon interp <path>` lowers the project as a build does, with no
runtime module and with what only the interpreter runs not refused, and runs it (`self/driver/interp.maxon`); the program's
output and exit code are its own, a violation exits with 4 and a program that outlives the step limit with 5.

- **What it records.** Every heap value's owner (the place that holds it) and whether it was freed; every address with the
  version of its structure; and the borrows that a call is waiting to use. A place has one owner, so storing a value that is
  already owned is an error, and so is overwriting a value that the place owns. A frozen value and a cown have many owners: it
  records the places that hold one, and a count. A cell (a place), an object (a value), a frame and a thread are handles that
  index the columns of the `Vm`.
- **Violations**, each reported with the function, the block, the instruction and what happened: a use after free, a double free,
  a leak (a value still live when the program ends, or a slot that owns a value when its function returns), use of a moved value
  (a slot that was moved out of or never assigned, read again), a value with two owners or one that is freed or returned while
  another owner holds it, a stale borrow (an element's address used after its structure changed), exclusivity (two arguments
  that reach one place where one of them can be changed, or a change to a place that a borrow a call is still waiting to use
  reaches), and three that only threads can break (below). A violation in a program the checker accepted is a hole in the
  checker or in lowering. It stops the run; a leak is found at the end.
- **Threads.** `main` is the first thread; a `when` makes a behaviour that is queued on each cown it names, in the order
  behaviours were scheduled, and that becomes a thread once it is first in all of those queues. A thread is a stack of frames
  that a loop runs only while the scheduler has given it the turn, so exactly one thread executes at any moment and a run is
  deterministic for a **seed**: the scheduler picks the next thread at random among every thread that has started and every
  behaviour that can start, and a thread keeps the turn for a random number of instructions (up to 2, 8 or 64, chosen at random
  each time), so the schedules include interleavings at instruction granularity and long runs of one thread. What is simulated is
  the interleaving, which is where a data race would show. The seed draws from a PCG generator (`words.maxon`). The program ends
  when `main` has returned and no behaviour is left, with `main`'s exit code; a trap in any thread ends it at once.
- **Who owns what.** A value belongs to the thread whose frame holds it, to the cown whose value it is part of, or to a frozen
  value, found by following owners up to a slot, the value of a cown or the contents of a frozen value; a value that nothing
  owns yet belongs to whoever holds it. Touching a value that belongs to another thread, or to a cown while the thread has no
  behaviour that holds the cown, is a **data race**; changing anything that is part of a frozen value is a **change to a frozen
  value**; and a frozen value or a cown whose count is wrong is a **reference count** violation (freed while a place or a waiting
  behaviour still holds it, or lowered below 0). One that is still live at the end is a leak, as any value is. Handing a
  behaviour a value that its scheduler still owns is a data race at the `when`. None of this is tracked until the first `freeze`,
  `cown` or `when` runs, so a program without them pays a branch.
- **A cown's destructor.** A count that reaches 0 can happen deep inside freeing a record or an array that holds the reference,
  where a call cannot be made. The interpreter takes the value out of the cown, frees the cown, and appends (destructor, value) to
  a list; once the instruction that dropped has finished, the thread that ran it calls the destructors in list order, which is
  the order of the frees, so it is deterministic (`interp --frees` lists them, on standard error).
- **Traps** behave as the executable does: `rt.panic`, an index out of range, a stale handle, a collection ended with `dropEmpty`
  while it holds an element and a behaviour that names one cown twice write to standard error and exit with 1; the same
  standard output and the same exit code. A trap is not a violation. A violation, a trap and a timeout end the run through a
  `Halt` that every function of the run throws.
- **How the tests use it.** `minimaxon test` runs every program under it, under 20 schedules when the program schedules
  behaviours, and requires the same stdout and exit code as the directives and no violation. `self/interp_test.maxon` tests it on
  IR that is written to break each rule (and to schedule behaviours: their order, their interleaving, the counts). `minimaxon fuzz` (`self/fuzz`) writes random well-formed programs over records, unions,
  strings, the four conventions, calls with overlapping and disjoint arguments, match bindings, loops, generics with bounds,
  arrays, maps, lists and pools with handles, a borrowed place held while a later operand of the same expression changes or
  consumes what it lies in, and frozen values, cowns and behaviours. The checker refuses most of them, since the generator leans
  on the edges of the rules: about two programs in five are careless and break rules throughout, and two in five of the rest
  break exactly one rule of sharing, once (a behaviour that takes a borrow, a variable used after a behaviour took it, a frozen
  value changed or taken apart, a cown read outside a behaviour, a borrowed place held across a later operand), so that a rule
  the checker lacked would be all that stands between that program and the interpreter. Every program it accepts must run
  clean, under several schedules when it schedules a behaviour, and one that does not is a hole. A program is a pure function of
  its seed; the generator draws from `self/fuzz/rand.maxon` and the generic library every program starts with is
  `self/fuzz/library.txt`. The command runs its seeds in worker processes, loads each program from memory
  (`project.loadSourceInto`), and with `--native` also builds each accepted program that has an executable, with its
  allocations counted, and compares it with the interpreter's: the same output and exit code, and no box left live.

## The runtime

The runtime is Maxon source: the `runtime/` directory at the repository root, beside `self/`, one module that is compiled
with every program, so a change to it is tested by the next compile with no rebuild of the compiler. Only what is derived from
the program's types stays compiler-emitted: the drop and copy routines and the entry stub (`self/backend/routines.maxon`,
`entry.maxon`). `runtime/README.md` lists the files and the rules a runtime file follows.

**Where it is found.** As maxon finds its library tiers: walking up from the compiler's executable, the first directory
that holds a `runtime/` directory (`project.runtimeDirectory`), so a compiler built in a checkout uses the checkout's and an
installed one the one installed beside it. A project whose root is that directory is the runtime itself
(`ast.Module.isRuntime`): `minimaxon check runtime` checks it as a library, with no `main`, and `build` and `run` refuse
it as a program with no `main` (`E3001`). The runtime's link path is `runtime` wherever it is compiled, so its functions
are `runtime::alloc` and the like; `runtime` is a reserved top-level module name (`E3182`), so no module of a program
takes it. The compiler finds it from `Process.executablePath()` and compares a project's absolute path, made with
`Directory.currentPath()`, without case (`project.isRuntimeDirectory`), so a compiler must sit in or below the directory that
holds `runtime/`. A `build` or `run` first loads the runtime as module 0 (`project.addRuntime`, from `prepareRuntime` in
`self/driver/compile.maxon`) and takes it through resolve, check, lowering and emit alone, and the program's modules are
added after it; no module is its parent, so no program names it. The compile holds it to the table
(`checkRuntimeFunctions` in `self/driver/runtime.maxon`, also run by `check runtime`), and the linker keeps of it only what the
entry reaches. A test run does this once and links every program with that code (`SharedRuntime`).

**The intrinsics.** The runtime is written on `__Raw`, maxon's name for the closed set of raw machine and OS operations,
here the static functions of a built-in record (`rawFunctions` in `internal/builtingen/table.go`). An address and a word
are a `Count`:

| Intrinsic | Gives |
|---|---|
| `__Raw.loadByte(address, offset: o)`, `loadWord16`, `loadWord32`, `loadWord` | the 1, 2, 4 or 8 bytes at `address + o`, zero-extended: a `Byte` for one byte, a `Count` for more |
| `__Raw.storeByte(address, offset: o, value: v)`, `storeWord16`, `storeWord32`, `storeWord` | writes the low 1, 2, 4 or 8 bytes of `v` at `address + o` |
| `__Raw.wrappingAdd(a, right: b)`, `__Raw.wrappingSub(a, right: b)` | address arithmetic: the sum or the difference modulo 2^64, unchecked |
| `__Raw.addressOf(word)` | the address of a local `Count`, passed `mutates`: an out-parameter of a DLL function |
| `__Raw.stringAddress(text)`, `__Raw.stringAt(box)` | the box address of a borrowed `String`; a `String` that owns a box the runtime built |
| `__Raw.heapAddress()`, `stdoutAddress()`, `stderrAddress()`, `liveAddress()` | the address of a global of the entry stub |
| `__Raw.callImport0(name)` to `__Raw.callImport10(name, arg1: a, ..., arg10: j)` | calls the DLL function `name`, a string literal (`E2004` otherwise), with that many words by the Win64 convention, and gives what it returns |

Only the runtime module may call them: a call anywhere else is `E3152` (maxon's `rawIntrinsicOutsideRuntimeTier`, with
its message), which `callFunction` in `self/check/calls.maxon` raises from the rows' runtime-only flag
(`ast.intrinsicRuntimeOnly`). They lower to four IR operations that only the runtime's code has: `loadraw.W`
and `storeraw.W`, W the width in bits, at the address plus the offset; `globaladdr` of an `Rt` global; and `import`,
which names the DLL function among the function's strings. The wrapping arithmetic is an unchecked `bin`, `addressOf`
the `addr` of the local's slot, and `stringAddress` and `stringAt` the word itself. `self/backend` writes them
(`visitLoadRaw`, `visitStoreRaw`, `visitGlobalAddr`, and `visitCallImport`, which aligns the stack itself, since a Maxon
function's frame is not aligned, and keeps `rsp` in `rbx`, which the callee preserves, saving `rbx` first).

**From an entry to a function.** The runtime entries the compiler calls are the rows of the table's `entries`
(`ir.Rt`), and `runtimeFunctions` names the function of `runtime/` that defines each one with its signature
(`alloc(bytes Count) Count`, `write(handle Count, text String)`, ...). From it the generator writes `ir.rtFunctions` and
`ir.rtSymbol`, which gives a mapped entry its function's link name (`runtime::alloc`) and an entry that
the stub or the linker defines its own symbol (`rt.entry`, `g.heap`); the linker resolves `rtSymbol(x)` by that text as
it does any call. The two calling conventions are one by construction: a `callruntime` and a `call` of a Maxon function
are both written by codegen's one `call`, which pushes the words left to right, calls and pops them, and the callee reads
parameter `i` at `rbp + 16 + 8 * (count - 1 - i)` and returns in `rax`. So a runtime function takes only words (an
integer, a `bool`, a `String` as its box address), takes none `mutates`, is not generic and throws nothing, since a
function that throws returns its error in `rdx` too. `minimaxon check runtime` holds the runtime to the table: each
mapped function exists with exactly that signature (`E3004` when it is missing, `E3005` when it differs) and links as
`rtSymbol` says. A new entry is a row of each table and a function in `runtime/`.

**Globals.** The entry stub keeps the process heap and the two standard handles (`g.heap`, `g.stdout`, `g.stderr`),
which it sets before `main`, and the count of live boxes (`g.live`), which it defines, zero, in every executable; the
linker defines `g.imageDataStart` and `g.imageDataEnd`. Runtime source reaches a global through its address intrinsic.
The runtime is the same code whatever the build: `alloc` and `free` always count, and `--count-allocations` changes only
the stub, which then passes `main`'s exit code to the runtime's `checkLeaks`; that exits with the leak report and 99 when
a box is live, and gives the code back otherwise. Behaviour, exit codes and messages are maxon's runtime's.

**Faults and the stack.** The executable's main thread has a 256 MB stack reserve (`peStackReserve` in
`self/backend/pe.maxon`): the compiler's passes recurse once per level of nesting of an expression, except down a chain of
operators, which each pass walks with a loop (`ast.leftChain`), so a sum of 200,000 terms needs no deep stack. A reserve is address space, committed a page at a time as the stack grows. Before `main` the stub keeps 32 KB
past the stack's guard page (`SetThreadStackGuarantee`) and registers a vectored exception handler (`rt.fault`), as
maxon's runtime does: a stack overflow, an access violation or an integer divide fault writes `panic: stack overflow at
rip=0x<address>` (or `nil pointer or invalid memory access`, `integer divide by zero`, `integer overflow`) to standard
error and exits with 1, the panic code, so a program, the compiler among them, never dies without a word. The handler
calls only kernel32 and allocates nothing; it prints no backtrace, which maxon's does. Every other exception goes on to
the system.

**What runtime source may do.** It is ordinary Maxon apart from the intrinsics: integers, `bool`, loops, `panic` with a
literal (which calls the runtime's own `panicWith`, which allocates nothing), and `String` values, borrowed as a
parameter or owned once `stringAt` makes one. A dropped `String` goes through the compiler-emitted drop routine, which
calls `free`, so no free comes back into the code that started it. It uses none of the primitives (`Array`, `Map`, ...),
which the runtime implements, and throws nothing. Runtime collections are not generic: an array entry takes a
descriptor of its element's size and layout, and only a thin typed wrapper is per type.

**Layouts.** `runtime/layout_gen.maxon` holds, as named constants, the layouts that the compiler's code and the runtime
share: the word, the string box (its length at 0, its bytes at 8), the array and byte array box (its length, its
capacity and the address of its buffer, with a buffer of 8-byte words or of bytes), the first capacities and the empty
box. A generator (`internal/abigen`, plain Go, see "Generated code") writes it, and `self/backend`'s and `self/ir`'s layout
constants, from one table, so the runtime and codegen cannot disagree. The array entries
(`runtime/arrays.maxon`, `bytes.maxon`) are thin functions over one set of routines that take the size of an element
as a word (`collectionReserve`, `collectionExtend`, `collectionOpenGap`, `collectionCloseGap`): collections take a layout
descriptor, and only a thin typed wrapper is per type.

**Compiled once.** The runtime reads no build option, so one compiled runtime serves every program a compiler builds in a
run: the test runner does it once (`SharedRuntime`). With the build cache (see "Build cache (planned)") its units would be kept
under a key of the compiler's own hash and the hash of every file of `runtime/`, so an edit to `runtime/` rebuilds the runtime
and nothing else.

## The subset

- `function`, `type` (fields, defaults, methods, `static function`), `enum` (integer raw values), `union`
  (payloads, methods), `interface`, `typealias X = int(a to|upto b)`, `typealias X = Box with Count` (also in a type's body), a top-level
  `let` constant. A function, a type and a union can be generic (see "Generics"). A `type` or a `union` can say
  `is noncopyable` right after its name (`type Socket is noncopyable`; never an `enum`), which takes `Copyable` from it,
  and a method of either can be a destructor (`function commit(destructs self)`), which takes both (see "Capabilities and
  destructors").
- `let`, `var` (with a value, or `var x as T` without one), assignment to a variable, a field or an element,
  `_ = call()`, `if`/`else if`/`else`, `if let x = try call()` and `if try call()` with an `else`, `while`, `for i in a to|upto b`, `for x in xs` over an array or a type with
  maxon's iteration protocol (see "Primitives"), `match` as a statement or expression
  (case, literal, range and string patterns, `or` alternatives, `default`, `panic` arms), `break`/`continue`
  with labels, `return`, `when` (a behaviour, see "Immutable values, cowns and behaviours"), and a `try` block with or
  without a handler (see "Checked arithmetic").
- Code after a statement that ends its block (`return`, a `panic`, `break`, `continue`, or an `if`/`else` or `match`
  whose every branch returns or panics) is allowed and type-checked, but never compiled (`ast.Live`): its calls make no
  instance of a generic. A `break` or `continue` inside a branch does not end the code after the `if` or `match`,
  since a `break` can leave a `match`.
- Integer, bool, string and byte string literals (`b"..."`, a new `ByteArray` of the characters up to U+00FF, one byte each, with `\xNN` for a raw byte; a character above U+00FF is `E1004`), array literals `[a, b]` and map literals `[k: v, k: w]` (see "Primitives"), interpolation, arithmetic and bitwise operators, `shl`/`shr`,
  comparisons, `and`/`or`/`xor`/`not`, `x if c else y`, `as` casts, error enums, `throw`, `throws E`, `try e`,
  `try e otherwise ...` and `try` blocks (see "Errors of the program"), calls with Maxon's
  positional-then-labelled arguments (a label, and a parameter name, may be a keyword: `File.rename(from, to: dest)`), parameter conventions (`mutates`, `consumes`, `sets`), `x.clone()`,
  `.name`/`.ordinal`/`.rawValue`, subscripts `a[i]`, `match consumes x`.
- Built in: `print`, `printError`, `panic` (which takes a string literal or an interpolated string), `freeze`, `share`, `thaw`, `cown` (also `cown(x, dropWith: d)`), `exchange`, the OS functions of maxon's stdlib (see "The OS"), the aliases `ExitCode`, `Count`,
  `ElementIndex` (an index that a `Count` goes where one is taken, and the other way only by `as`), `FullRangeInt`, `Byte`, `ByteArray` (`Array with Byte`), `StringArray` (`Array with String`), the limits of the sized integers as constants (`u64.max`, `i32.min`, ...), the `String` methods `byteLength()`, `byteAt(index)`, `sliceBytes(start, endPos: end)` and `toByteArray()` and the static `String.from(bytes)`, and maxon's `isEmpty`, `equals`, `append`, `count`, `contains`, `startsWith`, `endsWith`, `findFirst`, `findLast`, `startIndex`, `endIndex`, `slice`, `sliceLength`, `replace`, `replaceFirst`, `trim`, `trimStart`, `trimEnd`, `toLower` and `toUpper` with the type `StringIndex` (`charIndex()`, `bytePos()`) and the error `StringError`, the enums `ArithmeticError`, `ArrayError`, `MapError` and `IterationError`, the interface `Error`, and the generic types `Array`, `Map`, `List`, `Pool`, `Handle` (see "Primitives"),
  `frozen` and `cown` (see "Immutable values, cowns and behaviours"). There is no other stdlib, and no `Option`: a
  program that wants one declares a generic union (`union Option uses T`).

### Integers

Every typealias is its own type. An integer literal is an untyped constant: it fits any alias whose range
holds it, and constant expressions fold at compile time. A value whose range is known but has no alias,
such as a `for` variable or an intermediate result (below), fits any alias whose range contains it, and goes into
one that does not only through `as`, which checks at compile time for a constant and is otherwise an operation that can
fail. Mixing two different aliases is `E3005`, except at a `return`, which converts an integer to the function's
declared integer type when the range analysis proves that it fits (below). A `var` and a constant name their alias:
`var x = a + b` and `let Total = a + b` are errors until the value is cast, `as <Alias>`.

**The type of an operation.** `+`, `-`, `*`, `/`, `mod` and unary `-` take their type from where the result goes, and
are computed in a 64-bit word that their operands decide:

- The **word** is unsigned when an operand's type reaches past `i64.max` (a `Count`), and signed otherwise. No word holds
  a type that may be negative together with one past `i64.max`, so the operands are `E3005` and one of them is cast.
- Where the context **expects an integer type** (the target of an assignment, an argument, a return value, a field's
  value or default, an index, a payload, and the branches of a `x if c else y`, a `match` or a `try` that is itself in
  such a place) the result is that type, checked at the operation: `x = x + 1` of a `Score` fails above its maximum. The
  operands need not fit it; only the result must.
- **Anywhere else** (the initialiser of a `let`, an operand of another operator, a comparison, a condition, an
  interpolation, a `for` bound, an argument whose callee is generic, so its parameter's type is not known yet) the
  result is an anonymous range: the exact interval of the operation over the ranges of its operands' types, cut to the
  word. `a + b` of two `Score` values (`int(0 to 1000)`) is `int(0 to 2000)`, so `(a + b) / 2` returned as a `Score`
  needs no check, and `let t = a + b` holds the whole sum. A result past the word is cut to it and the operation can fail
  with `overflow`: `a - b` of two `Count` values is `int(0 to u64.max)` and fails below 0.
- `and`, `or` and `xor` take the alias of an operand, which must hold the other, and a shift the type of its left
  operand.

An anonymous range goes into an alias only if the alias holds all of it, else it is `E3005` and asks for a cast
(`try t as Score`). A `return` of an integer of another type, an anonymous range included, converts it to the declared
type with no cast, as `value as T` does, where the range analysis proves that it fits, as after
`if count >= 0 and count <= 255` for `return count` in a function that returns an alias of 0 to 255; where it does
not, it is `E3057` ("the value may not fit"), and the fix is `try <value> as T otherwise panic("out of range")`. A
literal that does not fit is `E3005`, and an assignment, an argument and an initialiser still need the cast. Comparisons take any two integers one word holds, an alias against an anonymous range or a constant
included; only two different aliases are `E3005`. The branches of a conditional, a `match` or a `try` with no type to
take give their alias when both are of it, or one is and holds the other, and otherwise a range that holds both.

### Errors of the program

A failure that a caller should handle is an error: a case of an enum that implements the built-in interface `Error`.
A function or an interface's method declares the one error it throws after its `returns`, and fails with `throw`:

```maxon
typealias Amount = int(i64.min to i64.max)

enum ParseError implements Error
	empty
	invalidSyntax
end 'ParseError'

enum AppError implements Error
	badInput
end 'AppError'

function parseDigit(s String) returns Amount throws ParseError
	if s.byteLength() == 0 'empty'
		throw ParseError.empty
	end 'empty'
	return match s 'digit'
		"0" gives 0
		"1" gives 1
		default gives -1
	end 'digit'
end 'parseDigit'

function forms(text String) throws AppError
	let a = try parseDigit(text) otherwise 0 // the value on a failure
	let b = try parseDigit(text) otherwise return // or a return, a break, a continue
	let c = try parseDigit(text) otherwise throw AppError.badInput // or a throw of another error
	let d = try parseDigit(text) otherwise panic("not a digit")
	try parseDigit(text) otherwise ignore // a statement drops the error and the value
	let f = try parseDigit(text) otherwise (failure) 'refused' // a block handler, which leaves here
		print("not parsed: {failure == ParseError.empty}\n")
		return
	end 'refused'
	try 'both'
		let n = parseDigit(text) // no try in the block
		print("{n}\n")
	end 'both' otherwise (e) 'failed'
		match e 'kind' // the handler matches on the error it names
			empty then print("no input\n")
			invalidSyntax then print("not a digit\n")
		end 'kind'
	end 'failed'
	print("{a} {b} {c} {d}\n")
end 'forms'
```

- A call of a function that throws, and of an interface's method that does, is covered by a `try` (`E3057`). A `try`
  covers its whole expression, and a `try` block its whole body, except what a `try` inside covers with an `otherwise`
  of its own. A `throw` needs no `try`: it goes to the handler of the `try` block around it, or out of the function.
- What leaves the function, through a `try` without `otherwise` or a `throw`, must be the error the function throws:
  `E3059` otherwise, also where the function throws nothing; convert another error with `otherwise throw`. A handler
  that names the error (`(e)`) takes the one error its block's calls and throws fail with, must `match` on it (`E3084`),
  and refuses anything else that fails in the block (`E3059`), which a `try ... otherwise` of its own handles instead. A
  handler without a name catches every error.
- A conditional try, `if let x = try call() 'label'` (`if var x` for a binding that may be assigned) and `if try call() 'label'` for a
  call that returns nothing, runs its block only when the call succeeds, with the result bound there. An `else (e) 'label'`
  block, whose `(e)` is optional, runs when it fails, and `e` has the one error the call fails with, which need not be
  matched; with no `else` the failure is dropped, and the enclosing function need not throw. `if try` over a call that returns
  a value is `E3124`. The call is covered by the statement as by a `try ... otherwise`, but the blocks are not. There is no
  `else if` after it.
- A block handler, `otherwise (e) 'label'` or `otherwise 'label'` with the block on the lines after it, ends with `end
  'label'` and stands as a statement, as the value of a `let` or a `var`, on the right of an assignment, and after a
  `return`. The `e` it binds has the one error the `try` covers (as in a `try` block's handler, so a second error is
  `E3059`), and need not be matched. A handler that leaves (a `return`, `throw`, `break`, `continue` or `panic`
  on every path) gives the `try` its expression's value; one that runs off its end gives none, so the `try` is a
  statement of its own (`E3059`, as for `otherwise ignore`). A block handler inside brackets, as in an argument, and
  in a constant, is not supported.
- `throws` names an enum (`E3113`); a value that is not one cannot be thrown (`E3005`). `otherwise ignore` gives no
  value, so its `try` is a statement of its own (`E3059`).
- An error comes back from the call as a value: the function returns it beside its result, its case plus 1 in a
  register and 0 for none (`rdx` in the executable), and the call is followed by one branch on it. There are no unwind
  tables and nothing is allocated for an error; a failure drops what it leaves as any other exit does.
- A built-in that can fail throws maxon's error enum for it, which the stdlib declares: `ArrayError` and `MapError` for
  the collections (see "Primitives"), and the errors of the OS functions (see "The OS"). Its call is covered by a `try`
  like any other, and its failure takes the same register and branch.

### Checked arithmetic

An integer operation that can fail says so. Its failures are the cases of the built-in error enum `ArithmeticError`, which
is an error like any other (see "Errors of the program"):

| Case | Of |
|---|---|
| `overflow` | `+`, `-`, `*`, unary `-`, `shl` and `not` whose result is past the range of its type (the type the context expects, or else the 64-bit word, see "Integers"), `i64.min / -1`, and an `as` whose value is past the target's range |
| `divisionByZero` | `/` and `mod` by 0 |
| `negativeShift` | `shl` and `shr` by a negative count |

An operation that can fail must be covered by a `try`, or it is `E3057`, which names how it can fail and a condition
that would rule it out:

```maxon
function sums(a Count, b Count, c Count) returns Count throws ArithmeticError
	let q = try a / b otherwise 0 // the value on a failure
	let r = try a / b otherwise return 0 // or a return, a break or a continue
	let s = try a / b otherwise panic("b is 0")
	let t = try a / b // passes the failure to the caller
	var total = 0 as Count
	try 'calc'
		let p = a * b // no try in the block
		total = total + p / c
	end 'calc' otherwise (e) 'failed'
		match e 'why' // e is the ArithmeticError
			overflow then print("overflow\n")
			divisionByZero then print("division by zero\n")
			negativeShift then print("negative shift\n")
		end 'why'
	end 'failed'
	return total
end 'sums'
```

- A `try` covers every operation in its expression, and a `try` block every operation in its body, except what a `try`
  inside covers with an `otherwise` of its own. A call of a function that throws can fail too.
- A `try` with no `otherwise`, and a `try` block with no handler, pass a failure to the `try` around them, or with none
  to the caller: only a function that throws the failure's error has one (`E3059` otherwise, and in a constant, a field's
  default and a behaviour's body), so an operation that can fail in a function that throws another error is covered by a
  `try ... otherwise`, as `try a / b otherwise throw AppError.badInput`. A handler that names a program's error refuses an
  operation that can fail with `ArithmeticError` (`E3059`); the handler of a `try` block whose body fails only by its
  operations names the `ArithmeticError`. `main` cannot throw, and neither can a function with a `sets` parameter, which
  a failure would leave set or not (`E3924`); a method throws exactly when the interface's signature does (`E3016`).
- A `try` that covers nothing that can fail is allowed, so a sharper analysis never breaks a program.
- A failure leaves what it interrupts, as a jump out of it would: the temporaries of the statement and the locals of
  the blocks it leaves are dropped, and so are the values computed for a call, a record or a union that never runs
  (`ir.Lowerer.pending`). The ownership pass follows each failure to its handler, so a value moved before a failure may
  be gone in the handler (`E3102`), and a value that cannot be dropped may not be in flight where something can fail
  (`E3919`).

**Guards.** The range analysis (`self/check/ranges*.maxon`, with `spans.maxon` and `accumulators.maxon`) proves most operations safe, and those need no `try` and
compile to no check at all: lowering emits only the checks it leaves (`ArithInfo.fails`). It is one walk over each function,
and knows:

- the span of each integer local: its type's, narrowed by what is assigned to it and by the conditions that guard the
  code (`if b != 0`, `while i < n`, the `else` of an `if`, the code after an `if` whose block returns, breaks or
  continues, the right operand of `and` and `or`, the branches of `x if c else y`, the arms of a `match` on ranges),
  and a `for` variable's bounds;
- the span of a field up to two deep (`c.n`, `self.total`, `p.pos.x`) that a condition narrowed;
- that one place is at least another (`a >= b`, `a > b`), which `a - b` reads;
- a constant's exact value.

What may be assigned (a `var`, a `mutates` or `sets` parameter, and their fields) loses what is known of it at the top
of a loop, and a place loses it when it is assigned or passed to a call that may change it, so no loop is walked twice
(but see accumulators, below); a condition says nothing of a place that it changes itself, as in `if a > 5 and reset(a)`. A
divisor that is not 0 may still be -1, so a signed `a / b` needs `b > 0`, or `b != 0` and `b != -1`. Proven code is
faster than it was before checked arithmetic, when every operation was checked.

**Accumulators.** A `for i in a upto b` (or `to b`) loop runs at most `max(0, b.hi - a.lo)` times (one more for `to`),
by the spans of `a` and of `b` where the loop is entered: `b` is evaluated once. A mutable integer local that the body
changes only by `v = v + e`, `v = e + v` and `v = v - e` is an accumulator of the loop, and keeps a span at the top of the
loop: where it was on entry, widened by the trip count times the most that the statements, counted as often as they can
run in one iteration (a statement in an inner `for` runs as often as that loop's trips, and one in an inner `while` is not
bounded), add and subtract. That span holds everywhere in the loop and after it, so a sum of small values over a loop of a
guarded length needs no `try`, and an operation is judged against it too, which is how a sum that fits only because the
loop is short is proven. Any other write to the local in the body (another assignment, a call that takes it as `mutates`
or `sets`, its declaration in the body), and a `when` in the body, make it an ordinary variable again, and so does a bound
that is not smaller than the type's. What `e` adds is found by a probe, a walk of the body from the state that forgetting
gives, which records no error and no `Fails` and leaves no state behind; only a loop that contains such a statement, and
that is not inside a probe, is probed, so the cost is at most one extra walk of each loop's body per loop around it. A
`while` loop has no such bound, and neither has a loop whose `e` names the accumulator itself, since `v = v + v` has no
span that a count of runs decides.

### Ownership

Layer 1 of [docs/regions.md](docs/regions.md), checked in one pass over each function after typing
(`self/check/ownership*.maxon`, with places in `paths.maxon` and `parts.maxon`). A value of a record, union, `String` or
type-parameter type has one owner; every other access is a borrow that cannot outlive the call or match that made it.

- **Moves.** Binding a place (`let b = a`), assigning it, passing it to a `consumes` parameter, returning it, and
  storing it in a field or a union payload move it, and so does a conditional or a `match` arm that gives it. Using
  it afterwards is `E3102`; assigning the variable again gives it a value again. A scalar is copied and never
  moves, and a fresh value (a call, a literal) needs no move. A conditional's or a match's value is always a fresh
  one, so a place in a branch is moved even where the whole expression is only borrowed.
- **Moving fields out.** A field of an owned value (a local, a `consumes` parameter, `consumes self`, or `self` in a
  destructor) is moved out like a whole one, at any depth through fields (`a.b.c`). The local is then partly moved: a fact
  about it lists the fields that are gone. Using it as a whole, and moving or reading a field that overlaps a gone one,
  is `E3102`; its other fields are still read and moved. Assigning a gone field gives it a value again (nothing is
  dropped, because nothing is there), and the local is whole again when none is gone. At the end of the local, the
  fields that are left are dropped, so each must be `Droppable`. Where two paths meet, a local is gone on the union of
  what either path lost, and a part that one path lost and the other still holds must be droppable, else `E3919`.
  Lowering moves the field with `moveat` on its address, which leaves 0 there; every drop routine skips an empty field,
  so the drop at the end of the local needs no knowledge of the path taken. A type with destructors is taken apart only
  inside its own destructors, at every record along the path, so `let note = t.note` cannot end a `Txn` without
  committing it (`E3922`; also for `match consumes`).
- **Destructors.** A type that declares one (`function commit(destructs self)`) cannot be dropped, so a value of it is ended
  by calling a destructor, which moves the receiver in, or moved on; ending a scope, overwriting it or discarding it
  without either is `E3919` (see "Capabilities and destructors").
- **`x.clone()`** is a deep copy that every type has, so no type may declare a method `clone`. It allocates a new
  box for each record or union inside `x`, and a new string for each string, so that every value has an owner of its
  own to free it. A value whose type is not `Copyable` has no `clone()` (`E3918`).
-  **Conventions.** A parameter takes its argument as `let` (no marker: an immutable borrow), `mutates` (a mutable
  borrow; the caller sees the change and passes no marker), `consumes` (the callee owns it) or `sets` (the callee
  initialises it). The marker comes before the name (`consumes item T`). A method's receiver is `self` (let), `mutates
  self`, `consumes self` or `destructs self`. A `consumes` parameter is owned but not mutable, like a `let` local: move it into a `var` to
  change it. Assignment, and passing as `mutates`, need a mutable place: a `var`, a `mutates` or `sets` parameter, or
  `mutates self`, through fields that are all `var` (`E2013`). `self` itself is the receiver and not a place that can be
  replaced, so it is never a `mutates` argument (`E2013`); a field of it can be, and a method can be called on it.
-  **Borrows are second-class.** A `let`, `mutates` or `sets` parameter, a `match` binding of a payload that is not a
  scalar, and anything reached through one is borrowed: moving it anywhere (a binding, a field, a `consumes` argument,
  the return value) is `E3900`. An element of a collection, which the collection owns, and a part of a frozen value or
  of a temporary, which is dropped when its statement ends, are not moved out either (`E3901`); `clone()` is the fix for
  all of them. A match binding also cannot
  be assigned, and the subject cannot be assigned, moved or passed `mutates` while the arm runs (`E3186`). There are no
  local projection bindings (`mutates r = a.f`): borrows exist only as parameters, match bindings and call arguments.
- **Exclusivity.** A place is a local or parameter followed by field selections and element selections (`a[i]`, see
  "Primitives"), and two places overlap when one is a prefix of the other (`a` and `a.f`; `a.f` and `a.g` do not;
  `a[i]` and `a[j]` do, for every index). An argument passed `mutates`, `consumes` or `sets`
  must not overlap any other argument of the call, receiver included (`E3186`). The rule applies to every
  argument that names a place, scalars too. An argument that is a borrowed place stays borrowed while the later
  arguments are evaluated, and so does the target of an assignment while its value is, so a call nested there that
  changes an overlapping place is refused too (`E3186`).
- **Initialisation.** `var x as T` declares a variable with no value. Reading it before every path has assigned
  it is `E3902`. A `sets` parameter starts without a value and must have one on every path that returns
  (`E3903`); its argument must be such a variable, which has a value after the call (`E3904`), and not before:
  another argument of the same call that reads it is `E3902`.

### Generics

Layer 1's generics ([docs/regions.md](docs/regions.md), step 2): a generic body is checked once, against its bounds,
and each instance is lowered once.

```
interface Shape
	function area() returns Count
	function grow(mutates self, by Count)
end 'Shape'

type Square implements Shape
	var side as Count

	function area() returns Count
		return side * side
	end 'area'

	function grow(mutates self, by Count)
		side = side + by
	end 'grow'
end 'Square'

type Box uses T
	var item as T

	static function make(consumes item T) returns Self
		return Self{item: item}
	end 'make'
end 'Box'

typealias SquareBox = Box with Square

union Option uses T
	some(value T)
	none
end 'Option'

function total(a T, b T) uses T returns Count where T implements Shape
	return a.area() + b.area()
end 'total'

function wrap(consumes item T) uses T returns Option with (Box with T)
	return Option.some(Box.make(item))
end 'wrap'
```

-  **Declaring.** A function, a `type` or a `union` declares type parameters with `uses`: after the name of a type or
  a union (`type Pair uses A, B`), after the parameter list of a function (`function swap(mutates a T, mutates b T)
  uses T`). A `where` clause ends the line, after `implements` or `returns`, and names the bounds a parameter must
  meet: `where T implements Shape and Named, U implements Other`. It constrains the declaration's own parameters, and
  for a method those of its type (`E3916` for any other), each once (`E3006`). A type, an enum or a union says which interfaces it implements with `implements`
  too: `type Square implements Shape, Named`. An interface lists method signatures, each
  with its receiver convention (`mutates self`, `consumes self`, `destructs self`, or none for a `let` receiver), and may name `Self`. It
  is a bound and nothing else: it is not the type of a value (`E3905`), has no type parameters, no static function and
  no parent interface, and an alias, `String` or `bool` cannot implement one. A method uses its type's parameters and
  takes none of its own (`E3907`). `Self` inside a generic type is the type at its own parameters, and a literal of it
  is `Self{...}`.
- **Instances.** A type names an instance with `with`: `Box with Count`, `Map with (String, Count)`, `lib.Option
  with Count`. One argument is written bare, several in parentheses, and an argument that is an instance itself is
  parenthesized by convention: `Option with (Box with T)`. Their number is checked (`E2056`). A `typealias` names an
  instance, `typealias Counts = Array with Count`, or, with several arguments, `typealias Counts = Map with String, Count` (a bare list, which only a typealias takes), and is that instance, not a type of its own: `Counts` and `array
  with Count` are one type. Its right-hand side is an instance (`E3917`), it may name other typealiases in any module,
  and one that names itself is `E2012`.
- **Nested typealiases.** A `type`'s body can declare a typealias, before or among its fields and methods:
  `typealias Items = Array with Element`, or `typealias Name = int(0 to 99)`. It is a declaration of the module like a
  top-level one, named `Pool.Items` in messages, with the type as its scope: the bare name spells it in the type's
  own body (a field, a signature, an expression such as `Items.create(0)`) and shadows a module typealias of the same name
  there, and the type's parameters are in scope in its target, so `Items` of `Pool with Element` is the instance at the
  type's own parameter. Declaring one twice in a body is `E3061`. A field that holds the type again over a type built from its parameters (`var kids as Array with (Nest with (T, Cell with U))`) is `E3184`, since its instances never end. Not supported: a typealias in an `interface`, an `enum`
  or a `union`, a ranged one in a generic type (a type for each instance, `E2015`), and naming one from outside the body
  (`Pool.Items`).
- **Calls infer.** An expression writes no type arguments, so brackets are always a subscript. A call takes them from
  its arguments' types (`swap(a, b: c)`, `Option.some(x)`, `Box.make(x)`), and the ones the arguments leave open from
  the type the call's result must have where it is used: a return value, an argument of a function that is not
  generic, the value of an assignment or a field of a literal, and the branches of a conditional or a match that is
  one of these (`return Option.none`, `Shelf{items: Array.create(0)}`). Otherwise a static function or a case is called
  through a typealias of the instance: `Counts.create(0)`. An integer constant or the range of an expression names no
  alias, so it infers nothing; with nothing else to go by the call is `E3179`.
- **Checked once.** A generic body is typed and checked for ownership once, where it is declared, whether or not
  anything instantiates it, and its errors are reported there and only there. Inside it a value of a type parameter
  has the methods of its bounds (`a.area()`, with `Self` read as the parameter; two bounds that declare one name
  are `E3114`) and what every type has: move and a borrow under any convention (a drop needs `Droppable`, see "Capabilities and
  destructors"). `clone()` only if the parameter is
  bounded by `Copyable` (`where T implements Copyable`, `E3918` otherwise). Nothing else: no field, no operator, no
  `match`, no interpolation. A `T` has only its bounds, so copying it and letting it die each need one, and a `T` that
  is not `Droppable` cannot go into a `cown` there, because it has no destructor to name.
-  **Ownership over `T`.** A value of a type parameter is owned, movable and not a scalar, whatever `T` is instantiated
  with, so every rule of "Ownership" applies to it unchanged and an instance cannot break what was checked once. Binding
  it, passing it to a `consumes` parameter, returning it or storing it moves it (`E3102` after); a `let` or `mutates`
  parameter of type `T`, a payload of one and a field of a generic type are borrowed when their owner is (`E3900`); `clone()` of a
  `Copyable` one keeps it. A `T` that is only bounded by interfaces cannot be dropped either: where one dies, the
  declaration says `where T implements Droppable` (`E3919`). So a generic container gives its element out with `.clone()` where `T` is `Copyable`, or takes it out of a primitive with `remove` or `pop`
  (see "Primitives"), or moves the field out of an owned value.
- **Copyable and Droppable.** Two built-in interfaces in the stdlib, with no methods, name capabilities of a type; a
  bound is written like any other (`where T implements Copyable and Droppable`). See "Capabilities and destructors".
- **A method's `where`.** A method may bound its type's parameters further: `function peek() returns T where T
  implements Copyable` inside `type Stack uses T` is a method that only a `Stack` of copyable elements has, so one
  container serves both kinds of element. The bound holds in that method's body, and a call checks the receiver's type
  argument against it in the verify stage (`E3017`); the type's own `where` is the one every method has. Other
  methods of the type do not have it: calling `peek` from one of them needs the bound there too. A method with such a
  `where` cannot be the one that implements an interface's method, because the type would implement the interface only
  for some instances (`E3016`).
- **Bounds at the instance.** An instance is judged only where it is written or a call infers it: each type argument
  must implement each interface its parameter is bounded by, which is a declaration (`type Round implements Shape`)
  and not a structural match, and the error names the declaration to write and the methods missing (`E3017`). A declaration that says
  `implements Shape` is checked once, for every method with the signature `Shape` gives (`E3016`), so a bound never fails
  later. Inside another generic, a type parameter meets a bound only if it is declared with it. Bounds belong to
  declarations that another module may still be declaring, so they are checked in the verify stage. A bound's
  method is called through the interface, so the implementing method needs no `export`.
- **Lowering.** An instance is lowered by monomorphization: the checked body is lowered again for each list of
  type arguments with the parameters bound, so a slot is `scalar` or `managed` for the instance's type, `clone()`
  copies that type, and a call through a bound calls the method of the type that stands in. The IR names an
  instance `swap[Count]` (source has no spelling for an instance of a function) or `(Box with String).make`, and emits it in the unit of each module that needs it, not in the module that declares the generic, so a module's output
  depends only on the module and the declarations of what it imports; calls to the types of other modules are symbolic.
  A module finds its instances itself, from its own functions and through the generics it reaches (`findInstances` in `ir`),
  so a module that needs an instance another one needs lowers and emits it too, and the link keeps the first unit's copy.
  The order of a module's instances is the order it found them in, and modules are lowered in a fixed order, so the same
  unit keeps an instance on every run. A compile that stops short of generate (`check`) still has each module find its
  instances, which is where `E3906` is raised.
- **Interning.** `Box with Count` is one type wherever it is written: the project's `TypeTable` interns each instance,
  keyed by its origin and arguments (`ast.instantiateType`). Every module that needs an instance gets the same type, and the
  order in which modules interned it is never visible in any output. An instance reads the declaration's fields, cases and
  methods and substitutes its arguments.
- **Limits.** A generic type may not contain itself, even with a larger type argument each time (`E4014`), and a
  generic function may not call itself with ever larger type arguments (`E3906`, past a nesting depth of 16). Each module that
  reaches such a function finds the runaway for itself, and the program gets one error: the one of the first module in module
  order that has it, at the first call in its walk that asks for the too deep instance. That is
  the recursive call inside the generic whenever the growth is in one call, the case that one error covers however many modules use it.

### Capabilities and destructors

A value that must be ended in one of several ways (a transaction that commits or rolls back, a socket, a reply token) is
the "Higher RAII" of [Vale's linear types](https://verdagon.dev/blog/higher-raii-uses-linear-types), built here from
two capabilities of a type, `Copyable` and `Droppable`, and from destructors, methods that end a value and may take
arguments and give results. Nothing runs when a value dies (there is no `deinit`): a type that has destructors must have
exactly one of them called on each value, or the value moved on.

```
type Txn
	var id as Count

	static function begin(id Count) returns Txn
		return Txn{id: id}
	end 'begin'

	function commit(destructs self)
		print("commit {id}
")
	end 'commit'

	function rollback(destructs self)
		print("rollback {id}
")
	end 'rollback'
end 'Txn'

type Box uses T
	var item as T

	static function of(consumes item T) returns Self
		return Self{item: item}
	end 'of'

	function take(consumes self) returns T
		return self.item
	end 'take'
end 'Box'
```

A `Txn` has no copy and no drop, so every path that makes one ends it with `commit` or `rollback` (forgetting it is
`E3919`, calling both `E3102`). `Box with Txn` has neither either, because its part does not: it is taken apart with
`b.take().commit()`, and `Box with Count` may simply be dropped. `Box` itself is written once.

`Copyable` and `Droppable` are structural: a type never says it has one (`implements Copyable` on a declaration is
`E3921`), it says what removes it. `Copyable` is lost by `is noncopyable` or by a part that lacks it; a part is a field,
a union payload, an element of an `Array`, `List` or `Pool`, or a value of a `Map`. A `frozen`, a `cown` and a
`Handle` have both whatever they hold, so the loss stops there. A generic type is judged per instance (`Box with Socket`
is not `Copyable`, `Box with Count` is), and a type parameter has a capability only by bound; `Copyable` includes
`Droppable`. A type that declares a destructor, a method whose receiver is `destructs self`, loses both, and so does
whatever holds one. A destructor is allowed on a `type` and a `union`, never on an `enum` (`E3920`), and in an interface's
signatures, so generic code can end a `T` through a bound. Calling one moves the receiver in like `consumes self`, and in
the body `self` is owned and its fields may be moved out. Where the body ends, `self` is destroyed: the fields that were
moved out are skipped and the rest are dropped, so each must be `Droppable` (`E3919`), and no obligation remains. `self`
may be passed whole only to another destructor of its type, which ends it there (`rollback` delegating to
`abort(reason: r)`); moving it whole anywhere else is `E3920`. The query is `check.lacksCap` (`self/check/context.maxon`),
one answer per type and capability, remembered per checker (`capKnown`), naming the part that removes the capability so the message
can say where it comes from and how to get it back. Every copy decision goes through it:

- `x.clone()` of a type that is not `Copyable` is `E3918`, and so is a top-level `let` of such a type (a constant is read
  any number of times).
- Built-ins state what they need as a bound on their signature, checked like any other (`E3017`): `thaw` needs a
  `Copyable` content because it copies it, and `array.filled` needs a `Copyable` element, a bound on the method that
  `Array with T` adds to its own parameter. `freeze` and `cown(x)` need a `Droppable` content, because the last owner
  drops it, and `map.insert` needs a `Droppable` value, because it drops the one it replaces.
- **An implicit drop needs `Droppable`.** These drop a value, and each is `E3919` when its type lacks `Droppable`: the end
  of a scope that still holds an owned local, a `consumes` parameter, a `consumes self` or a payload bound by `match
  consumes` (also through a `return`, `break` or `continue` that leaves it, reported at that statement; at the
  declaration for the end of a block); a `consumes _` parameter; an assignment over a variable, field or element that
  holds a value; a discard `_ = e`; a temporary that nothing takes (a result passed to a `let` parameter, read through
  as in `make().id`, or left by an expression statement); and a payload that an arm of `match consumes` does not name.
  The ownership pass keeps a stack of the owned locals in scope whose type lacks `Droppable`; a fact about one means it
  holds nothing, so it is checked at the end of its scope and removed there. Where two paths meet, such a local must hold
  nothing on both or neither (a partly moved one is judged by what is left of it), else `E3919` at the move that happened on one path only (or at the declaration of a variable
  assigned on one path only). A path that ends in a trap needs no destructor, and a `while true` loop is left by its `break`s alone, so
  only they reach the code after it. In a generic body a `T` without the bound
  is such a type, so code that lets a `T` die writes `where T implements Droppable`. A function whose locals all have
  `Droppable` does no more than one length check per scope and per join.
- `match consumes x` moves an owned `x` into the match and each arm owns the payload it binds, where a plain `match`
  borrows the payload from `x`. Lowering moves each payload out of the box with `moveat` and drops the box alone.
- `exchange(place, value: v)` moves `v` into a place (`mutates`) and gives back what was there, so a place that holds
  a value that cannot be dropped can still be replaced.
- **A cown's content.** `cown(x)` drops the content with the last reference, so it needs `Droppable`. A content that has
  destructors says how it ends instead: `cown(t, dropWith: rollback)` names a destructor of `t`'s type that takes
  nothing else and returns nothing, as a method name (it is not a value, so there are no function values), and the
  thread that drops the last reference runs it. `dropWith:` on `Droppable` content has nothing to decide, and naming
  anything but such a destructor is `E3921` too. This is the only place a destructor runs without being called.
- **Behaviours.** The captures move into the body, so a captured value that is not `Droppable` is ended there like any
  owned local.
- A message that would have the reader write `.clone()` suggests it only for a type that has `Copyable`; for another it
  names what removes it and says to move the value instead.

### Primitives

Layer 2 of [docs/regions.md](docs/regions.md), as far as step 3 goes: their signatures and ownership rules. An
`Array with T`, a `Map with (K, V)`, a `List with T` (doubly linked, one node per element) and a `Pool with T` own their elements.
Their operations are generic methods that no source declares (`internal/builtingen/table.go`), typed, borrowed and moved
like any other method from the signature below, so none of the ownership rules has a special case for them. They are
declared in the stdlib module, so a name resolves to them last, after the module's own, the root's and an exported
one anywhere.

| Type | Operations |
|---|---|
| `Array with T` | `create(n)`, `n` zero elements, where `T` has a zero value or `n` is the literal `0` (`E3106`); `filled(n, value: x)` where `T` is `Copyable`; `count()`, `isEmpty()`; `get(i)`, a copy of the element, where `T` is `Copyable`, and `set(i, value: v)`, where `T` is `Droppable`, both throwing `ArrayError.indexOutOfBounds` past the end; `push(value)`; `append(other)`, a copy of each element of `other`, where `T` is `Copyable`; `insert(at, value: v)`, which clamps `at` to the count; `remove(at)` and `pop()` return `T` and throw `ArrayError`; `dropEmpty()` |
| `Map with (K, V)` | `create()`; `count()`, `isEmpty()`, `contains(k)`; `insert(k, value: v)`, which throws `MapError.keyAlreadyExists` for a key that is there, and `upsert(k, value: v)`, which replaces its value, both where `V` is `Droppable`; `replace(k, value: v)` returns the old value and throws `MapError.keyNotFound` for a key that is not there; `remove(k)` drops the value and returns whether the key was there, where `V` is `Droppable`; `dropEmpty()` |
| `List with T` | `create()`; `count()`, `isEmpty()`, `contains(h)`; `first()`, `last()`, `next(h)` and `prev(h)` return `Handle with T` and throw `ArrayError.indexOutOfBounds` where there is no such node; `prepend(value)` and `append(value)` return `Handle with T`; `removeFirst()` and `removeLast()` return `T` and throw `ArrayError`; `remove(h)` returns `T` and throws `ArrayError.emptySlot` for a handle that names no element; `moveToFront(h)`; `dropEmpty()` |
| `Pool with T` | `create()`; `count()`, `isEmpty()`, `contains(h)`; `insert(value)` returns `Handle with T`; `remove(h)` returns `T` and throws `ArrayError.emptySlot` for a handle that names no element; `dropEmpty()` |
| `Handle with T` | no operations; `==` and `!=` |
| `ArrayError`, `MapError`, `IterationError` | maxon's error enums: `indexOutOfBounds` and `emptySlot`; `keyNotFound` and `keyAlreadyExists`; `exhausted` and `atStart`, which the iteration protocol throws |
| `ByteArray` | `typealias ByteArray = Array with Byte`, built in: the same type, with every operation of `Array`; `String.toByteArray()` is a new one of a string's UTF-8 bytes, and `String.from(bytes)` a string of a copy of them |

An array of an integer type whose range fits in a `Byte` holds one byte to an element, natively and under the
interpreter, and any other array a word; the program cannot tell, except by how much memory it takes.

**Array and map literals.** `[a, b, c]` is an `Array` and `[k: v, l: w]` a `Map`, written on one line with at least one element:
`[]` is `E2015`, as in maxon (its element type cannot be inferred), and there is no `Alias from [...]`. A literal is what the explicit code
does: an empty collection, then a `push` or an `upsert` of each element in the order written, so each element is checked against
`value` (and `key`) as that call's argument is, moved in under the same rules, and a later `upsert` of a key replaces its value. The
element type is the one the context expects when it is an `Array with T` or a `Map with (K, V)` (an argument, a return, a field, an
assignment, a typealias'd `let`), else the first element's, with a later integer joined to it as the branches of a conditional are
(`E3005` for two types that do not join; for a negative number and one above `i64.max`, "no common integer type"). Bare numbers have
no alias, so a literal of them keeps the range it spans, `[1, 2, 3]` being `Array with int(1 to 3)`, which a `let` reads at any alias that holds it
(`return try [10, 20, 30].get(0) otherwise 0` is an `ExitCode`); a literal inside another takes the whole word instead. A `var` cannot
start from a collection of bare numbers, which could not take another later (`E2004`, as for `var x = 5`): `var xs = [5 as Count, 6]`. A
module-level `let` of a literal is a constant that each use builds again.

`create` and `filled` are static functions, called through a typealias of the instance (`Counts.create(0)`) or where
the type is known (`Shelf{items: Array.create(0)}`). An operation that changes the primitive takes `mutates self`, and one that adds an element takes it `consumes`.
`dropEmpty()` takes `consumes self`, ends the collection and traps if an element is left: it is how a collection of values with destructors, which cannot be
dropped, ends after every element was taken out. `Map.replace` lowers to `has`, `elem`, `moveat` and `insert`, so the key
keeps its place and no value is dropped. Nothing built in returns an `Option`, and there is none built in.
- **Operations that fail throw.** `remove`, `pop`, `removeFirst`, `removeLast`, `first`, `last`, `next`, `prev`, `Map.insert` and `Map.replace` throw
  maxon's error, so a call of one is covered by a `try` like any other that throws (`E3057`): `try a.pop() otherwise
  fallback`, or `try a.remove(i) otherwise panic("index out of range")` where a miss is a bug. The lowering tests first
  and fails as a compiled throwing function does, with the case plus 1 in the error register, so a failure allocates
  nothing; `Map.insert` and `Map.replace` drop the key and the value they were given on a failure.

- **Collections own their elements.** Adding takes the element `consumes`, so it moves in. Taking one out is `remove`,
  `pop`, `removeFirst` or `removeLast`, which move it out to the caller, owned; the only other way to hold an
  element is `.clone()`. `clone()` of a collection is deep.
-  **A subscript is a projection.** `a[i]`, `m[k]`, `l[h]` and `p[h]` borrow the element: the array is indexed by a
  `Count`, a map by its key, a list and a pool by the `Handle` of the element. Read, it is a `let` borrow; assigned
  through (`a[i] = v`, `a[i].count = 3`) or passed to a `mutates` parameter, it is `mutates`. It is never an owned
  value: binding it, returning it or passing it to a `consumes` parameter is `E3901`, or `E3900` where the collection
  itself is borrowed, and `clone()` is the fix. A scalar element is copied like any scalar, so `let n = a[i]` is fine.
  The element of a temporary (`make()[0]`) cannot be moved or assigned either.
- **A miss traps**, as an out-of-range index, a missing key or a stale handle: a range-check guard, safe, and it
  never touches freed memory. The operations that cannot miss let a program test first: `count`, `isEmpty` and `contains`.
- **Exclusivity.** A path gains a second kind of step, an element, and every element of a collection overlaps every
  other one, whatever the indexes say: `a[i]` and `a[j]` overlap, so `pair(a[i], b: a[j])` and `f(a, n: a[0])` are
  refused (`E3186`), while `pair(a[i], b: b[j])` and `pair(r.left[0], b: r.right[0])` are fine. A method with an
  `mutates self` receiver, such as `push`, `insert` and `remove`, overlaps every projection of its collection in the
  same call: `a.push(a[0])` is refused, and `let x = a[0]; a.push(x)` is the fix for a scalar. An index, a key
  or a handle is an ordinary value, read before the call, so `a[a.count() - 1]` and `a.insert(a.count(), value: x)`
  are fine.
- **A borrow is held while the rest is evaluated.** Wherever lowering evaluates an operand to a borrow of a managed
  place, then evaluates the next, then uses the borrow, the place stays borrowed through the next. That is an argument
  while later arguments are evaluated, the target of `x.f = v` and `a[i] = v` while the value is, the left operand of a
  string comparison while the right is, the collection of a subscript while its index is, and the first part of an
  interpolated string while the second is (later parts are joined as soon as they are evaluated, so they hold nothing).
  A call in there that changes an overlapping place is refused (`E3186`): `look(a[0], n: drop(a))`, where `drop`
  takes `a` as `mutates`, `a[0].n = drop(a)`, `a[0] == wipe(a)`, `rows[0].cells[shrink(rows)]` (read, assigned,
  passed `mutates` or matched) and `"{p.name}{take(p)}"`. A scalar that is only read is a value by then and holds
  nothing, so `a[0].n + shrink(a)` is fine; so is a copy on the left, `a[0].clone() == wipe(a)`, or the other operand
  computed into a variable first. One helper, `hold`, takes every one of these, and a hold is only a push that is looked
  at when something in the rest changes or moves a place.
- **Handles are keys.** A `Handle with T` is a plain word: copy it freely, store it in a field, in a map, in an array or
  in another element, compare it with `==`. It owns nothing. Using one is a lookup, so a handle whose element was
  removed finds nothing, and a slot that was reused does not answer to the old handle, because a handle also
  carries a generation. A handle of one pool used on another pool of the same element type is not a soundness issue:
  the lookup fails or finds that pool's element at the same slot. A handle of another element type is refused
  (`E3005`). A `List` hands out the same kind of handle for its nodes, so a cache is a `List` of entries in order of
  use and a `Map` from each key to the handle of its entry, and a linked structure is a `Pool` of nodes that name
  each other by handle (see `tests/`).
- **Map keys** are compared and copied as a word or as text: an integer alias, a `bool`, an enum, a `String` or a
  `Handle`. Any other key, a record or a union or a type parameter, is `E3908`. A generic function can therefore take
  a `Map with (String, V)` with a generic `V`, but not a `Map with (K, V)` with a generic `K`; a bound for keys would lift that.
  `upsert` replaces the value of a key that is there, which is dropped.
- **Generic code** over a primitive is checked once, as in "Generics": a value of type `T` held by an `Array with T` is
  owned and movable, so a generic container gives an element out with `remove` or `pop`, or `.clone()`.
- **Iteration.** `for x in a` walks an array: each element in turn is lent to `x`, as `a[i]` lends it, so a scalar is
  copied and anything else is a borrow that cannot be moved out (`E3900`, `.clone()` is the fix). `x` itself is a `let`
  (`E2013`), and its fields are as writable as the element's: `x.n = 3` needs a mutable `a`, through `var` fields.
  While the body runs, a place the loop walks is held as a match subject is: changing or moving it is `E3186`, and
  `for x in a.clone()` is the fix. The lowering is a loop over the indexes, reading the element in place.
  A type walks with maxon's iteration protocol, found on its concrete type: a cursor declares `current()`, which gives
  the element and cannot fail, and `advance(mutates self) throws IterationError`, which fails past the last one; a
  collection declares `createIterator()`, which returns a cursor and may throw `IterationError` where it is empty. A
  loop over a cursor moves it on in place (a place it is must be mutable, and is held while the body runs); a loop
  over a collection calls `createIterator()` once and owns the cursor. Each `current()` is a value of its own, owned by
  `x` until the pass ends. A throw of `createIterator()` or `advance()` ends the loop, and a `createIterator()` that
  throws another error is `E2015`, as is a loop over anything else: a string, a map, a list, a pool. A map has no way to
  walk its keys yet.
- **Running them.** An array runs natively: heap-allocated, bounds-checked, and freed with what it holds where its owner
  dies (see "The primitives in the IR" and "Native freeing"). A map, a list and a pool are checked and lowered, and the interpreter runs them (see "The interpreter"), so a
  program that uses one is refused when it is compiled to an executable (`E9900`) and still passes `check`; the test
  runner runs such a program under the interpreter alone.
- **Limits.** A primitive implements no interface (`E3017`). A record
  that contains itself through an array (`var kids as Array with Tree` in `Tree`) is fine, natively too: its drop and
  its copy are routines that call themselves.

### Text

A `String` is its UTF-8 bytes. Besides the byte operations, it has maxon's methods that read it by grapheme cluster: `count()`
is the number of clusters, a `StringIndex` (built-in record; `charIndex()` and `bytePos()` give its two parts) names the
start of one, and `startIndex()`, `endIndex()`, `findFirst(needle)` and `findLast(needle)` give them, the searches throwing
`StringError.notFound` where the needle is not there; `slice(start, endIndex: end)` and `sliceLength(start, length: n)` cut by
them, `contains`, `startsWith`, `endsWith`, `equals` and `isEmpty` ask, `replace(old, with: new)` and `replaceFirst` leave a
string with an empty or absent `old` as it was, `trim`, `trimStart` and `trimEnd` remove the whitespace and newline clusters of
maxon's `CharacterSet.whitespacesAndNewlines` (they take no set), `toLower` and `toUpper` map the ASCII letters only, as
maxon's do, and `s.append(t)` joins `t` to the string in `s`'s place (a new string, so a loop of appends is quadratic).
Their code is `runtime/textops.maxon`, which the executable links and `builtingen` copies to `self/interp/textops_gen.maxon`
for the interpreter, so one text says what each means; a search gives its position plus 1 so that 0 is no match.

Clusters follow the rules of UAX #29 on the classes that the languages in common use occupy (the Extend ranges of the
combining marks and the variation selectors, the zero width joiner and the pictograph sequences it joins, the pairs of
regional indicators, Hangul syllables, CR LF) with no Unicode table, so a mark outside those ranges is a cluster of its own
where maxon's tables join it. `StringIndex` has no `equals` or `compare`; `String` has no `split`, `hash`, `charAt`,
`codepoints`, `bytes()` or `utf16()` yet.
### The OS

The OS functions are maxon's stdlib, names, labels, results and errors, declared as static functions of built-in
records that have no fields (`internal/builtingen/table.go`), so a program calls them as maxon's does:

| Call | Gives | Throws |
|---|---|---|
| `CommandLine.args()` | `Array with String`, the program's own name first | — |
| `Process.environmentVariable(name)` | `String` | `ProcessIntrospectionError.variableUnset` |
| `Process.executablePath()` | `FilePath`, the running executable's full path (`GetModuleFileNameW`) | `ProcessIntrospectionError.pathUnavailable` |
| `Process.id()` | `Count`, the running process's id (`GetCurrentProcessId`); maxon's `Pid` is the same unsigned 64-bit integer | — |
| `FilePath.from(path)` | `FilePath`, with `/` made `\` | `FilePathError.invalidCharacter` for a control character or one of `<>"\|?*` |
| `path.toString()` | `String` | — |
| `File.readText(path)` | `String` | `FileReadError` |
| `File.writeText(path, content: s)`, `File.writeBinary(path, content: bytes)` | — (a file that is there is replaced) | `FileWriteError` |
| `File.rename(from, to: dest)` | — (what is at `dest` is replaced) | `FileRenameError` |
| `File.delete(path)` | — (a directory is refused, `accessDenied`) | `FileDeleteError` |
| `Directory.isDirectory(path)` | `bool` | — |
| `Directory.create(path)` | `bool`, whether the directory is there after it; the missing parents are made too | — |
| `Directory.currentPath()` | `FilePath`, the working directory; a panic when Windows cannot give it | — |
| `Directory.list(path)` | `Array with FilePath`, each the path joined to an entry's name (no `.` or `..`) | `DirectoryListError` |
| `Subprocess.run(executable, arguments: args)` | `CollectedOutput`: `exitCode()`, `succeeded()`, `stdout`, `stderr` | `SubprocessError` |
| `Subprocess.runWithDeadline(executable, arguments: args, deadline: d)` | as `run`; a child still running when `d` (maxon's `WaitDeadline`: `WaitDeadline.none` or `WaitDeadline.within(ms)`) passes is terminated | `SubprocessError`, `timeout` for the deadline |

- **`FilePath`** is a record holding the path's `String` (`path`), which every `File` and `Directory` function takes. It
  is made with `FilePath.from`; a string literal does not convert to one.
- **Errors** are maxon's enums, cases and all: `FileReadError` (`notFound`, `accessDenied`, `busy`, `failed`),
  `FileWriteError` (`failed`, `notFound`, `accessDenied`, `alreadyExists`, `busy`, `partialFileLeft`),
  `FileRenameError` (`failed`, `notFound`, `accessDenied`, `busy`, `alreadyExists`), `FileDeleteError` (`notFound`,
  `accessDenied`, `busy`, `failed`), `DirectoryListError` (`notFound`, `accessDenied`, `listFailed`),
  `ProcessIntrospectionError` (`pathUnavailable`, `variableUnset`, `environmentUnreadable`), `FilePathError`
  (`invalidCharacter`, `notFileURL`, `malformedURL`, `noParent`) and `SubprocessError` (`executableNotFound`,
  `spawnFailed`, `busy`, `ioFailed`, `timeout`, `treeNotEnded`, `detachRefused`, `inputTooLarge`, `statusLost`). Which
  case a failure is comes from the Win32 error, as maxon's runtime tells them: not found (2, 3), access denied (5),
  busy (32), already exists (80), and the operation's catch-all for any other. Cases that nothing here fails with are
  kept so that a `match` is the one maxon needs.
- **`Subprocess.run`** takes an `Executable`, maxon's union: `Executable.name(n)`, searched for as Windows searches the
  first word of a command line, or `Executable.path(p)`. Each argument is quoted as the C runtime's
  `CommandLineToArgvW` splits it back: one with a space or a tab is quoted, and a quote is escaped with a backslash
  after doubling the backslashes before it (`runtime/commandline.maxon`). The child's standard output and error are pipes,
  which the run drains in turn without waiting on either, so a child that fills one cannot block; its standard input is
  the program's. A name that is not found is `executableNotFound`.
- **`Subprocess.runWithDeadline`** is maxon's `Subprocess.run` with its `deadline:`, a function of its own because built-ins have
  no default arguments. When the deadline passes the run ends the child with `TerminateProcess` and throws `timeout`; it
  ends the child alone, not the process tree it started (maxon's ends the tree), and the output the child had written is
  lost with it.
- **Natively**, the entries are Maxon source in `runtime/` (`os.maxon`, `subprocess.maxon`), which calls the `W` Win32
  functions, converting the program's UTF-8 to UTF-16 and back (`text.maxon`; a byte that is not UTF-8 and a lone surrogate
  become U+FFFD), and splits the command line by the C runtime's rules (`commandline.maxon`): 2n backslashes before a quote
  are n, 2n+1 are n and a literal quote, a doubled quote inside quotes is a literal one, and the program's own name is split by
  the same rules. The interpreter runs the entries through the same OS functions of the compiler's own runtime
  (`self/interp/os.maxon`).

### Immutable values, cowns and behaviours

Layer 3 of [docs/regions.md](docs/regions.md), step 5: how data crosses between threads, after Verona's
behaviour-oriented concurrency. There are no threads in a compiled program yet (see "Native" below); the interpreter
runs behaviours under a simulated scheduler, and the checker's rules are what the interpreter's runs test.

```
type Account
	export var balance as Count
end 'Account'

function transfer(from cown with Account, dest cown with Account, amount Count)
	when from, dest as f, t 'move'
		if f.balance >= amount 'enough'
			f.balance = f.balance - amount
			t.balance = t.balance + amount
		end 'enough'
	end 'move'
end 'transfer'
```

| Written | Means |
|---|---|
| `freeze(x)` | takes `x` (`consumes`) and gives a `frozen with T`, an immutable value; `T` is not a plain value (`E3910`) and must be `Droppable`, because the last owner drops it (`E3017`) |
| `share(f)` | another owner of the frozen value or the cown that `f` (a `let` borrow) names; the count goes up by 1 |
| `thaw(f)` | an owned, mutable deep copy of what a frozen value holds, so `T` must be `Copyable` (`E3017`) |
| `cown(x)` | takes `x` (`consumes`) and gives a `cown with T`, a reference to a cown that owns the value; `T` may be a plain value and must be `Droppable` (`E3017`) |
| `cown(x, dropWith: d)` | the same for a `T` that has destructors: `d` names one that takes nothing else and returns nothing, and the last reference runs it on the value (`E3921`) |
| `when a, b as x, y 'label'` … `end` | a behaviour: runs later, once it has exclusive access to the cowns `a` and `b`, with `mutates` access to their values as `x` and `y` |

-  **Frozen values.** The contents are lent and never given: reading a field, calling a method whose receiver is `let`,
  subscripting, matching, interpolating, and passing the frozen value to a `let` parameter of the type it holds all see
  the contents (`d.title`, `d.total()`, `describe(d)`). Anything else is refused: assigning through it, a `mutates`
  argument, a method that takes `mutates self` or `consumes self`, or a `consumes` parameter is `E3197`, and taking a
  part out of it is `E3901`. The type system tells a frozen value from its contents by the type `frozen with T`, so a
  place's path gains a step into the contents (`self/check/paths.maxon`), and every rule about places, borrows and exclusivity applies
  to it as it does to a field. `clone()` of a frozen value, of a cown reference, and of anything that holds one, is a
  `share`: the copy is another owner of the same value, never a new value; `thaw` is how to get a mutable copy of the
  contents. There is **one count per frozen value**, however many objects it holds. A frozen value is built from a value
  that was owned (a tree) and given up by `freeze`, so what it holds is either part of that tree or a counted value that
  existed before it, and nothing can be added to it afterwards: counts form a DAG and no cycle of frozen values can
  exist. A string literal and a constant are immortal and uncounted.
- **Cowns.** A `cown with T` is a counted reference: `share` and `clone()` make another, dropping one lowers the count, and
  the cown and its value are freed when the last reference is gone and no behaviour holds it; a value with destructors is
  ended by the one that `dropWith:` names instead (see "Capabilities and destructors"). The value is touched only in
  a behaviour (`E3915` for a field, an element, a method, a match or an interpolation of a cown outside one). A type that
  can reach itself through a cown, directly or through other types, is refused (`E3914`): a behaviour could store a
  reference in the value, two cowns could refer to each other, and counting never frees a cycle. A handle or a key
  does the same job without the cycle.
-  **Behaviours.** `when` names its cowns, which are plain variables or fields of `self` (`as` would read as a cast
  after an expression), and gives one name for each value; the cowns are only read, so naming one does not move it. The
  body is a function of its own that sees the variables around it by capturing them: **a scalar is copied; an owned
  value moves in at the `when`**, so using it afterwards is `E3102` (a loop that schedules a behaviour that takes a
  variable declared outside it fails the same way); a frozen value and a cown reference are owned values too, so one
  that the enclosing code keeps is shared first (`let own = share(f)`). A borrow cannot be captured, because the
  behaviour outlives it: a `let`, `mutates` or `sets` parameter, the receiver and its fields, a `match` binding, and `x`
  or `y` of an enclosing behaviour are `E3909`, with the fix named (`share` for a shared value, `clone()` for the rest).
  A captured value is not mutable in the body (it is moved into a `var` to change it), like a `consumes` parameter. `x`
  and `y` are `mutates` parameters of the body, so they cannot escape: moving, returning, storing or passing them to
  `consumes` is `E3900`, and `clone()` is the way out. A behaviour returns nothing and is not waited for (`return value`
  is `E3005`); it may schedule more behaviours, and passing a result on is how a chain works. Naming one cown twice is
  `E3913`, and a count of names that differs from the count of cowns is `E3912`, as is naming something that is not a
  cown reference.
- **The one run-time check.** Two different variables can name one cown (`let b = share(a)`), and the checker cannot
  tell, so a behaviour that is scheduled with the same cown twice traps (exit 1, "names the same cown twice") instead of
  giving two `mutates` names to one value. It is a trap, as an index out of range is, and never a race.
- **Order.** A behaviour is queued on each cown it names when it is scheduled. It runs when it is first in all of those
  queues, so behaviours that share a cown run in the order they were scheduled, behaviours on disjoint cowns have no
  order, and a behaviour that names several cowns acquires them together: there is no lock to hold while waiting, so no
  deadlock.

### Layout

The layout is part of the syntax: a program has one way to be written, and anything else is a syntax error at the
character that breaks the rule.

- A line is indented with tabs, one per level, and a body is one level deeper than the line that opens it. A
  blank line holds nothing, not even tabs.
- Tokens on a line are separated by exactly one space, or by none: after `(` and `{`, before `)`, `}`, `,` and
  `:`, around `.` and `[`, before `]`, before the `(` of a call, a declaration's parameters or `int(...)`, and after a unary `-`.
  There is no trailing whitespace, no tab inside a line, and no carriage return.
- A line break is only where the grammar has one, never inside parentheses.
- A comment is a `//` comment: on its own line, indented like the line after it, or after code, separated from
  it by one space. A `/* */` comment is an error.
- Exactly one blank line separates two top-level declarations, and a comment block after the last one. Inside a
  type, an enum or a union, exactly one blank line comes before each function (a method or a static function), unless
  it is the first line of the body; a comment above the function goes after the blank line. An interface lists its
  functions, one signature per line, with no blank lines between them. There are no other blank lines: none between
  fields or cases, none inside a function, none at the start or end of a file. A file ends with one newline, and has no
  byte order mark.

The lexer checks what does not depend on the grammar (`E2001`). The grammar says where `Indent` and `Dedent` go
and which tokens join (`-> noSpaceBefore` on a token, `^'('` at a call), and the parser checks each gap and the
blank lines between declarations (`E2010`). Holding the layout costs the lexer nothing measurable: it lexes the
bench at the same 49–50 ms as without the checks.

`minimaxon fmt` writes this layout, so a change to a layout rule is "change the rule, run `fmt`". The parser decides the
layout, so the parser formats: `Parser.gapBefore` says what separates a token from the one before it (none, one space,
a line start or the end of a line), and `lineFault` and `endFault` say which blank lines may come before a line. The
checking parser refuses a gap that differs from those, and the formatting parser (`syntax.Format`) writes them instead:
the output is the token stream with the required gaps, a tab per `Indent`, and the comments, which the lexer skips but
which stay in the gaps of the source. A comment on its own line is written indented like the line after it, a comment
after code one space after it, and the text of a comment is never changed. Blank lines between comment paragraphs stay
where a blank line is allowed, and go where it is not. An interpolation is formatted by the same parser, in its own
pass.

The formatter reads source whose layout is wrong but whose meaning is clear, which the lexer reads in a lenient mode:
several spaces or tabs between tokens, trailing whitespace, whitespace on a blank line, a comment spaced or indented as
it likes. Line endings are first made LF, a byte order mark dropped and a missing final newline added. Indentation of
code stays strict, since a space or a wrong depth would change the program, and so do a line break inside parentheses
and a block comment: `fmt` reports the error and leaves the file alone. The lenient checks sit on the lexer's error
paths, off `lexLine`'s own path, so lexing a correct file costs the same (the lex bench measures 25.5 ms before and 25.9 ms
after, and the parse phase of a 25 MB generated project 47 ms in both). Before it writes anything `Format` parses its output strictly and
compares its tokens with the input's, so a formatter bug is an internal error and never a changed program.

### Where it differs from Maxon

- A compiled program frees each value where its owner dies, as the IR says (see "Drops" and "Native freeing"), with one
  `HeapFree` and no reference counting; Maxon counts references. There is no green thread, scheduler or atomic count, so
  a frozen value, a cown and a behaviour have no native code: the interpreter counts a frozen value and a cown, schedules
  behaviours, and fails a program that leaks a value. A native program that is not built with `--count-allocations` cannot
  tell that it leaked, and a program that panics or traps does not free what it holds.
- Errors (see "Errors of the program"): an error is an enum, not a union with payloads; a `try` block's handler names
  one error, not the combined error of several (refused with `E3059`), and has no terminal `otherwise panic(...)` or
  `otherwise throws ...` form; there is no block handler `otherwise (e) 'label'` on a `try` expression
  inside brackets and no `default throws` in a match. Maxon's `E3168` (a `throws` that nothing throws) and
  `E3083` (a `try` block with nothing that throws) are not raised, so a sharper range analysis never breaks a program. A `try` covers every
  operation of its expression, not one call, and a condition that rules a failure out is a proof the compiler checks
  (see "Checked arithmetic").
- A `return` converts an integer to the function's declared integer type with no cast where the range analysis proves that
  it fits, and the value that may not fit is `E3057` (see "Integers"). Maxon converts every `return` implicitly and checks
  it at run time, trapping where the value does not fit, so a function that returns an `ExitCode` can return any
  integer there; here the conversion costs nothing and is never a trap, and a value that is not proved needs `try ... as T`.
- A `b"..."` literal is a fresh `ByteArray` each time it is evaluated, copied from its bytes in the executable's data, and the
  program may write it: there is no `immutable` image data, so `E3207`, `E3196` and `E3209` do not exist, and a module-level
  `let` of one is built again at each use, as a literal array is. `\{` and `\}` in one are the unknown escape of a string (`E2001`), and a raw newline in one ends it unterminated, as in a string, where maxon keeps the newline as a byte: write `\n`.
- No alias subtyping (an alias implementing another), floats, characters, tuples, closures, async, services, region capabilities (`isolated`, `immutable`, `modify`), static fields, default
  parameters, format specifiers, or `#if`. The memory model of [docs/regions.md](docs/regions.md) is implemented: owned
  values, the primitives, frozen values, cowns and behaviours (see "Ownership", "Primitives" and
  "Immutable values, cowns and behaviours"), with an interpreter that runs all of it and a native backend that runs
  only the array. `freeze` is a function here, `when` is a statement that takes cowns and gives names to their values,
  and a frozen value is a type, `frozen with T`, where Maxon recovers immutability from facts about a value's history.
  The grammar has none of the rest, so each is a syntax error.
- Array and map literals (see "Primitives") are the maxon literals without `Alias from [...]`, `Type from [...]`, a literal spread over several
  lines, or constant image data: a `let` literal is a fresh `Array` or `Map` like any other value, not an immutable image shared by its
  uses, and its bare numbers keep their range as the element type of that one array instead of fitting any alias at each use, so
  passing `let xs = [1, 2]` to a parameter of `Array with Count` is `E3005` until the literal is written where the parameter is.
- `Array`, `Map` and `List` have the names, the signatures and the errors of maxon's stdlib, but they are built-in
  types rather than stdlib code, and keep these differences: a subscript is a place (`a[i].f = x`, `mutates a[i]`) that
  traps on a miss, where only `get` and `set` throw, as maxon's do; `Pool`, `Handle` and the list's handle API (`prepend`
  and `append` return a `Handle`, `first` and `last` return one, `remove(h)`, `next`, `prev`, `moveToFront`) stand where
  maxon has pointers and indexes; `filled`, `Map.replace` and `dropEmpty` are minimaxon's own; only an array is walked
  by `for x in` (a `String`, a `Map`'s entries, a `List`, a range value and `withIterator()` are not), and the loop holds
  the array, so a body that pushes to it or reassigns it is refused (`E3186`) where maxon re-reads the live count; the
  iteration protocol is found on the concrete type, never through an interface; there is no `sort`, `contains`, `cursor` or `hash` yet, `reserve` does nothing, since a capacity is no part of an array's value here (there is no `capacity()`), and `clear`, `truncate`, `resize`, `slice`, `first` and `last` are maxon's; the indexes and counts they take are `ElementIndex`; `create(n)` refuses an enum element, whose
  zero here is its first case rather than its case of raw value 0, and takes no `zeroed:`; and `String` has the byte operations `byteLength`, `byteAt`, `sliceBytes`, `toByteArray`, the static `String.from(bytes)`, and maxon's text methods (see "Text").
  There is no built-in `Option`, which maxon's stdlib has no more of either: an
  operation that can miss throws.
- `ByteArray` is a built-in typealias rather than maxon's in `stdlib/File.maxon`, and the minimaxon `Bytes` buffer is
  gone for it. Only an array of an integer type that fits in a byte packs its elements; maxon sizes an enum's and a
  narrower integer's elements by their range too. `String.from(bytes)` copies the bytes where maxon's shares the
  array's buffer, and a `String` here is its bytes, so neither direction scans them.
- The OS functions (see "The OS") have maxon's names, signatures and errors, but are a subset, built in rather than
  stdlib code: `FilePath` has only `from`, `toString` and its `path`, and `FilePath.from` does not resolve a `file://`
  URL (one is taken as a path); `File` has no `readBinary`, `createText`, `exists`, `renameNew`, `info` or `openRead`,
  and `writeText` and `writeBinary` take no `mode:`; `Directory` has no `exists`, `delete` or `removeTree`, and
  `Directory.list` lists in the order the system gives; `Process` has only `environmentVariable` and `executablePath`; `Subprocess.run`
  takes no `workingDirectory:` or `deadline:` (`runWithDeadline` takes the deadline), and `CollectedOutput` has no `status`, `pid` or `durationMs`;
  `SubprocessError` is an enum without maxon's payloads (the name, the reason, the output). Natively the runtime uses
  the `A` Win32 calls, so a path or an argument is bytes in the system's code page rather than UTF-8 turned into
  UTF-16, and `CommandLine.args()` splits only a token quoted whole, not by every `CommandLineToArgvW` rule; the port
  of the runtime to `self/` does what maxon does.
- The runtime's source (see "The runtime") is written on a `__Raw` of minimaxon's own: maxon's `loadByte`, `loadWord`,
  `storeByte` and `storeWord` with the 2- and 4-byte widths beside them, unchecked arithmetic, the addresses of the
  stub's globals and a call of any DLL function by name, where maxon has a row for each OS operation, a scratch area and
  atomics. A runtime file may hold a `String` (maxon refuses a managed value there, `E3153`, because its reference counts
  would call the runtime) and may declare `export` names (maxon's `E3154`), since the runtime is a module of its own whose
  names are always qualified; names need no `__` prefix for the same reason.
- Generics are checked once against their bounds, never per instantiation; maxon checks a generic function per inferred
  type. Interfaces are bounds only: no generic interface, no static requirement, no default method, no existential
  value; a type implements one by saying so, and `int` aliases, `String` and `bool` implement none.
- Generics are written as in Maxon (`uses`, `with`, `where`), with four differences: an instance is written wherever a
  type is (`returns Option with (Box with T)`), not only through a typealias; a bound is `where T implements Shape`,
  the word a type declares conformance with, rather than `where T is Shape`; a union can be generic; and a call whose
  arguments leave a type argument open takes it from where its result goes (`return Option.none`).
- A type or a union opts out of copying with `is noncopyable`, and declares a destructor to opt out of dropping, where
  Maxon has `unique` (so `is`, `noncopyable` and `destructs` are reserved). `unique` is replaced by this: every owned
  value is already moved and never aliased here, so what is left of it is a value that cannot be copied or forgotten.
  `Copyable` and `Droppable` are structural bounds, not interfaces a declaration implements (see "Capabilities and
  destructors"). `exchange(place, value: v)` takes the label `value`.
- The reserved words are exactly the keywords the grammar uses, so a word Maxon reserves for a feature minimaxon
  lacks is an ordinary name, and a word that could be read by context, such as `mutates`, `consumes` or `sets`, is
  reserved.
- A parameter convention is a verb written before the parameter's name, with the function as its subject:
  `push(consumes item T)`, `swap(mutates a T, mutates b T)`, `parse(sets result Ast)`, and `mutates self` or
  `consumes self` for a receiver, and `destructs self` for a destructor.
- Modules are Go's, not Maxon's. Another module's names are always qualified by its path (`net.http.parse()`, and
  `export.X` for the root's), where Maxon also finds a bare name in the root or as the one exported declaration
  anywhere (`E3095`, `E3063` for two, which minimaxon does not have). `export` reaches the tree rooted at the declaring
  module's parent, as Go's `internal/`, and `public` everyone. Import cycles are refused.
- It checks only the rules that the generated code depends on. The style rules are left out: an unused
  name (`E3012`), a `var` that is never reassigned (`E3077`), a label naming the innermost block (`E2048`),
  an unneeded cast (`E3010`), self-assignment or `_ =` on a non-call (`E3067`), a one-value range pattern
  (`E2027`), all bindings discarded (`E3081`), and the lint rules (`E3064`, `E3065`, `E3092`, `E3219`).

## Changing the language

A language change reaches the grammar, the checker's messages, every program that uses the construct (the corpus, `self/`,
`runtime/`, the program generator), `cmd/convert` and the docs, and a difference from current Maxon is recorded in "Where it
differs from Maxon" in the same change. A change to the syntax touches many programs, so tools do the mechanical part:

- **`minimaxon rewrite --keyword=old=new <path>...`** renames a keyword. It lexes each file and changes only keyword
  tokens, so a name, a label, a comment or a string's text that reads the same is left alone, and a keyword inside an
  interpolation is renamed. Run it before editing `Maxon.g4`: once the grammar stops reserving the word, a use of it
  cannot be told from a name, and the command refuses. A file the lexer refuses, such as a test of a lexer error, is
  left alone and listed if it contains the word. In a Markdown file it renames the code: inline code spans, and fenced
  blocks tagged `maxon` or not tagged; prose, and a span or block that does not lex (IR text, a command), are left
  alone, and the first such span holding the word is named.
- **`minimaxon fmt [--check] <path>...`** is the tool for a layout change (a rule about spaces, indentation, blank
  lines or comments): change the rule in the parser or the grammar, run `minimaxon fmt self runtime tests docs README.md`,
  then check. It rewrites every `.maxon` file under the paths, and the fenced blocks of `.md` files that are tagged
  `maxon` or not tagged and parse as whole programs; other blocks, code spans and prose are left alone. `fmt --check`
  writes nothing and exits 1 listing the files that are not in the layout. Over a directory it skips a file that
  contains `// expect-error: E2001` or `// expect-error: E2010`: those tests are laid out wrongly on purpose, and a
  formatter would put them right. A file that does not parse is reported and left alone (over a directory, a corpus
  holds such files on purpose; a file named on the command line that does not parse is an error and exits 1).
- **`minimaxon qualify <dir>...`** qualifies, in each project, every bare name of another module's declaration, as a
  program must write it: it resolves each module and writes the module's path (`export` for the root's) before each
  name that the module, the standard library and its scopes do not declare and another module does. The name is taken
  to mean what Maxon's bare lookup finds, the root's declaration, else the one exported declaration of that name; a
  name that two modules export is listed and left alone. It is the step after converting a Maxon project, whose names
  may be bare.
- **`minimaxon rename <rule>... <program>...`** changes the programs that use a built-in when the table renames it or
  changes its signature, while the old one still compiles: it checks each program and rewrites each type name and each
  call by the declaration the checker resolved, so `append` on an `Array` becomes `push` and `append` on a `List` stays.
  A rule can write a call again from a template, reordering its arguments or wrapping it in a `try`, and write a call of
  a built-in free function again as a whole (`isDir()=Directory.isDirectory({path})`); a file of cases is
  checked case by case after its `--prelude`. What the checker does not reach, in a program that stops at an error
  first, is listed for a hand edit; a program that does not parse has no tree to list, so `rename` leaves it alone and says
  nothing of it.
- **`minimaxon test --update-positions`** moves each `expect-error` whose code is now reported at another line or column,
  as after a rename that changes a line's length. It never changes the code a test expects, so a test that now fails
  with another error still fails.
- **The compiler's own source.** `minimaxon rewrite` over `self` and `runtime` renames keywords in the compiler, its
  tests and its runtime. The program generator spells each keyword it writes through one constant
  (`self/fuzz/keywords.maxon`), so a rename edits that; the only other keyword spellings are in the generators' tests
  (`internal/grammargen`) and `cmd/convert`, which the Go tests find.
- **The seed.** `seed/minimaxon.exe` has the language as it was when it was built, and it must compile `self/` and `runtime/`.
  A change that `self/` or `runtime/` must follow therefore takes two commits: first the compiler accepts the new form while
  `self/` and `runtime/` still use the old one, and `.\seed.ps1` renews the seed (`seed/README.md`); then `self/` and
  `runtime/` move to the new form and the old one goes. A change that they do not use needs no new seed.
- **`.\check.ps1`** runs every check this repository requires (see "Tests"), formatting and applying `go fix` first,
  and stops at the first failure. The seed builds `self/` as `bin\c1.exe` and `c1.exe` compiles `self/` again as
  `bin\c2.exe`, which runs every later step, so a change that only the compiled `self/` shows (a symbol that two modules
  share, a runtime behaviour) fails here. Arguments go to `minimaxon test`, so `.\check.ps1 --update-positions` works.

A refactoring is a list of `syntax.Edit`s (a byte range and its new text), applied by `syntax.applyEdits`, so a language
server can send the same edits to an editor. `.claude/skills/language-change` holds the checklist for a change to the
language: the order of the steps above, the grammar and what it regenerates, the words the compiler says (diagnostics name the
fix, so they quote syntax), `cmd/convert`, the tests and the docs.

## Adding a feature

A typical change touches one file per stage. `.claude/skills/write-self` has the patterns the subset's code follows
(handles for shared things, `consumes` and `exchange` for owned values, result unions for errors):

1. **Syntax:** a rule in `self/syntax/Maxon.g4`, listed in the set it belongs to (`stmt`, `expr`, `decl`), with an `@ast::Node`
   block for the fields later phases fill in; then `go generate ./internal/...`. That writes the node, its parse function and
   its keywords into `self/`; a token conversion the grammar asks for is a function to write in `self/syntax`, and the steps
   below handle the node. A syntax error is the parser's generic one, never a diagnostic of its own.
2. **Resolution:** the resolver (`self/resolve`), binding any names the node holds.
3. **Typing:** the checker (`self/check`), and the flow of `self/ast/flow.maxon` for a statement. Record what lowering needs in
   the module's `Typing` (`self/ast/tables.maxon`). Read the types and signatures of the modules the module reaches freely (the
   stages visit them first), but never write them: only a module's declare stage writes its own declarations.
4. **Ownership:** the ownership pass (`self/check/ownership*.maxon`). An expression whose value goes to a new owner is passed
   through `consume`, which is what marks it for lowering.
5. **Lowering:** the lowerer (`self/ir`), built from the existing instructions where it can. A value it makes that nothing
   consumes is a temporary, and a place it declares or assigns drops what it held (see "Drops"). A new instruction is a row of
   the IR table (`internal/irgen/table.go`, see "Generated code"); after `go generate ./internal/...`, codegen
   (`self/backend`) and the interpreter (`self/interp`) fail to build until they handle it. A new built-in is a row of
   `internal/builtingen/table.go`, and the dispatch names the function for it in the checker, the lowerer and the interpreter.
6. **Tests:** a `tests/run/` program for the behaviour (it runs natively and under the interpreter), a `tests/errors/` program
   for each diagnostic, and a test in `self/*_test.maxon` only when a part needs one. A new construct that the memory model's
   rules cover also goes into the program generator (`self/fuzz`).

A rule that is only a diagnostic, such as a new ownership or sharing check, usually needs steps 3 or 4 and a test, and nothing
else. Before a commit `.\check.ps1` passes (see "Tests").
