# minimaxon

A small Maxon compiler for trying out language changes, and the seed of the next full Maxon compiler. It is
written in Go, targets x64 Windows only, and is about 18,000 hand-written lines in thirteen packages, plus the generator
that writes its parser and AST. It compiles modules in parallel and writes a
PE executable directly, with no external assembler, linker or C runtime.

It is not the current Maxon compiler (`maxon-bin/` in the maxon repository) and shares no code with it, but it
replaces it. It implements the subset of Maxon needed to compile `self/`, its own port to Maxon; once `self/`
compiles itself, `self/` grows into the full compiler. Built, `self/` is a compiler with the commands `check <file|dir>` (the front end `self/` has so far, with the diagnostics of `minimaxon check`, exit code 1 when there are any) and `selftest` (its own tests); a command it does not have yet exits 6 (not implemented) and a command it does not know exits 2, which `test --with` tells apart. It is compatible with current Maxon except for the language
changes developed here ("Where it differs from Maxon"). Its code stays plain enough to port: structs, interfaces,
slices, and no runtime-specific tricks the design depends on.

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
| `minimaxon build <file\|dir> [-o out.exe]` | compile; a directory is a project, one module per directory; modules that did not change come from the build cache (see "Build cache") |
| `minimaxon run <file\|dir>` | compile to `%TEMP%\minimaxon\` and run; exits with the program's code |
| `minimaxon check <file\|dir>` | diagnostics only; `check runtime` checks the runtime's source and holds it to the functions the compiler calls (see "The runtime") |
| `minimaxon test [--with=<exe>] [dir]` | run the corpus (default: `tests` beside the executable); with `--with`, with another compiler (see "Tests") |
| `minimaxon rewrite --keyword=old=new <path>...` | rename a keyword by token in the `.maxon` files and the Markdown code under the paths (see "Changing the language") |
| `minimaxon qualify <dir>...` | write the module's path before each bare name of another module's declaration in the projects (see "Changing the language") |
| `minimaxon rename [--prelude=f,...] [--rules=file] <rule>... <program>...` | rename a built-in type or rewrite the calls of a built-in method by the declaration the checker resolved, never by text: `Array.append=push`, `Array.insert=insert({at}, value: {item})`, `Array.pop.unwrap=try {call} otherwise panic("empty")`, and a free function's whole call: `readFile().unwrap=try File.readText({path}) otherwise panic("unread")` (the rules are in `rename.go`) |
| `minimaxon fmt [--check] <path>...` | put the `.maxon` files and the Maxon code blocks of the `.md` files under the paths in the one layout; `--check` changes nothing and exits 1 listing the files that are not in it (see "Layout" and "Changing the language") |
| `minimaxon cache [trim\|clean]` | show the build cache's directory, size and last trim; trim it now; remove everything in it (see "Trimming") |
| `minimaxon interp <file\|dir>` | development only: run under the tracking interpreter, which runs a map, a list, a pool, a frozen value, a cown and a behaviour, and reports a violation of the memory model; exits 4 on a violation |
| `go run ./cmd/convert [--write] [--check] [--out=DIR] [<path>...]` | convert existing Maxon `.maxon` source files to minimaxon syntax and layout, marking unsupported constructs with `// TODO` comments and warnings on stderr; `--check` writes nothing and prints each kind of warning with its count and files, most frequent first (run `minimaxon qualify` after converting a project of several modules) |

`--emit-ir` writes the IR of every function beside the executable, as `<out>.ir`. `--jobs=N` sets how many
modules compile at once. The default is one fewer than the processor count, and the executable is byte-identical
whatever the value. `--no-cache` makes a `build` or `run` neither read nor write the build cache, and the environment variable
`MINIMAXON_CACHE` names the directory it lives in, or says `off` (see "Build cache"). `--timings` prints to stderr each phase's wall time, busy cores, the kernel's share of that work,
allocation, garbage collections and GC pause time, then the total with the share of the processors kept busy. Busy
cores are the processor time a phase used over its wall time, so a phase that keeps every core working reads as the
processor count; the kernel's share is mostly committing fresh pages (see "Allocation"). Windows counts processor time
in clock ticks of about 15.6 ms, so a phase needs to last several ticks for its cores to mean anything. With the build
cache on, the `modules` phase also prints a line of how many modules were hits and misses, how many checks were skipped and
how much was read and written.
`--trace=FILE` writes a Go execution trace of a `build`, `run` or `check` to FILE, with a mark where each phase
ends; `go tool trace FILE` shows what each core did over time, so idle cores at a phase's end and serial steps are
visible. `--seed=N` picks the schedule of behaviours that `interp` runs (default 0). `--count-allocations` builds an
executable that counts its live heap boxes (see "Counting allocations"); it is for development, and `minimaxon test` always
uses it. `--update-positions` makes `test` rewrite the `at L:C` of an `expect-error` whose code was reported at another
place, and count the test as passed.

### Tests

There are two kinds. **Unit tests** (`internal/<package>/*_test.go`) test one package with `go test`, which builds
only that package and the ones it imports, so a change is checked in a few hundred milliseconds without rebuilding
the compiler. `internal/check`'s tests show how to run the front end on a program. The Maxon source of unit tests lives in
`.maxon` files under each package's `testdata/`, read with `internal/srctest`: a program per file, or a file of cases
split at `// case: name` lines with `// expect-error: CODE` directives, after a prelude of shared declarations. Every
such file must lex on its own, which `TestEveryFileOfTestSourceLexes` checks, so `minimaxon rewrite` reaches it. **Corpus tests**
run whole programs through `minimaxon test`.

Each `tests/**/*.maxon` is one program, except under `tests/modules/`, where each directory is a project
and its root files carry the directives. The runner compiles every program to an executable, a program that must
fail included, because some errors come after type checking (lowering refuses what has no native code yet).
The runner never uses the build cache, so a test compiles exactly what is in its files and leaves nothing behind in the user's
cache; the driver's own tests use a cache in a temporary directory. A program that compiles is run twice, as the executable and under the tracking interpreter (see "The interpreter"),
and both must give the stdout and the exit code the directives say; the interpreter must also find no violation of the
memory model, and the executable, built with `--count-allocations`, must end with no heap box live (a program that
traps skips that check). A program that uses a map, a list, a pool, a frozen value, a cown or a behaviour has no
executable yet: it fails to compile with `E9900`, and the runner then runs it under the interpreter alone. No directive is needed for
that. A program that schedules behaviours runs under 20 schedules (seeds 0 to 19), and every one must give the same
stdout and exit code and no violation, so the directives of such a program must say an output that does not depend on the
schedule: print only from behaviours that the cowns put in order, such as one chain, or behaviours on one cown.
`minimaxon test --with=<exe>` runs the same corpus against another compiler, the one that `self/` makes, with the same
expectations. Each compile is a subprocess: `<exe> check <path>` for a test that expects an error, `<exe> build <path> -o
<out>` for one that runs, and the diagnostics are read from its stderr lines `error CODE: path:L:C: message` (exit code 1
with no such line is a failure). A run test then runs the executable, and the interpreter has no part in it. A compiler
that exits 6 (`notImplementedExitCode`: it does not have the command yet) does not fail the test: the test is counted
as not yet supported. The summary is `N passed, M failed, K not yet supported`, the failures are listed, and the exit code is 0
when nothing failed.

Directives in comments say what the program must do:

| Directive | Meaning |
|---|---|
| `//> text` | one expected line of stdout, in order (none means stdout must be empty) |
| `// expect-exit: N` | expected exit code (default 0; a panic exits 1) |
| `// expect-error: E3102` | compiling must fail with this code; `E3102 at 7:5` also pins the line and column (`E9900`, a program that needs something that has no native code yet, is the one error that is not a failure in a program that expects none) |
| `// memory-limit: N` | the native run may use at most N megabytes: the runner puts the process in a job object with a memory cap, so a program that never gives memory back fails (an allocation fails and it crashes), whatever the machine has |
| `// native-only` | do not run the program under the interpreter; for a program whose point is what the executable does with memory, which the interpreter runs far too slowly |

### Errors

A diagnostic is `error E….: file:line:col: message`, the layout of the current Maxon compiler, and compiling stops at the first error in each
function. An error raised at a node is located, given its file, line and column, when the compile reports it. A diagnostic is a `*CompileError` panic that `Guard` (`internal/diag`) turns into a list entry; any other
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
verify stage of the module's check. Name resolution reads only the module's own declarations, the standard library and the
declarations of the modules the module names, because another module's names are always qualified.

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

## Packages

The compiler is a set of packages under `internal/`, each a phase or a layer, so each builds and tests on its own.
Dependencies only point down the list, and Go refuses an import cycle, so the layering cannot erode:

| Package | Holds | Imports |
|---|---|---|
| `diag` | `CompileError`, `Guard`, the error codes (generated from `codes.tsv`) | — |
| `par` | `ForEach`: runs a phase over a pool of `--jobs` goroutines, and `ForEachIndexWith`, which gives each worker the scratch buffers it reuses for every item; `Run`: runs a `Plan`, tasks that wait for other tasks, on the pool, preferring the group a worker was just on and then the lowest task, where a running task can make the tasks that wait for it wait for more (`Edges`); a panic is raised by item or task number, not by time | `diag` |
| `cache` | the store of the build cache: entries found by a key in a directory, written whole by a rename and read so that damage is a miss, the hasher that keys are made with, the hash of the running compiler, trimming what has not been used for days, and the wire format an entry is written in (see "Build cache") | — |
| `srctest` | reads the Maxon source that unit tests keep in `testdata/`; imported only by tests | — |
| `ast` | the program model: `Tree`, a module's nodes as 16-byte records with the views that read them and the lists they point into, the structs that stay (the declarations), their kinds and the visitors (generated), `Pos` and `Positions` (where a node is), the handles (`ModuleID`, `NodeID`, `DeclRef`, `LocalID`), which statements of a block are live, semantic types, generic instances and their substitution, the built-in types (the runtime primitives, `frozen`, `cown`, and the records, the union and the errors of the OS functions), `I128`, `Module` and `Project`, and `ModuleResult`, the per-module products of the phases, with `Resolution` and `Typing`, the tables that phases fill in (see "The AST") | `diag` |
| `project` | loading a project: discovery, finding the runtime's source (see "The runtime"), declaration tables, the module graph and its import-cycle check | `ast`, `diag`, `par` |
| `syntax` | the grammar (`Maxon.g4`), module source buffers, the lexer, the parser generated from the grammar and the `Scratch` a worker reuses to read, lex and parse each module, the formatter (the parser in a mode that writes the layout instead of checking it), and refactorings as edits to the source (of Maxon files and of the code in Markdown) | `ast`, `diag` |
| `resolve` | name resolution, and binding each `where` constraint to its type parameter, into the module's `Resolution` | `ast`, `diag` |
| `check` | the type checker, the generic bounds, the range analysis that decides what arithmetic can fail, the ownership pass, and what a behaviour may capture, into the module's `Typing` | `ast`, `diag` |
| `ir` | the IR (fixed-size records in per-worker arenas, with the views, visitors, printer and cache codec generated from the IR table), its builder, finding the generic instances a module needs, lowering (the primitives' operations, where each value is dropped, and the body of each behaviour included), which reads the `Typing` of every module it follows a call, a constant or a field's default into, and the runtime's symbol names | `ast`, `cache`, `diag` |
| `backend` | the x64 encoder, codegen, the runtime unit and the array runtime unit, the drop and copy routine of each type, the linker and the PE writer, and the format a code unit is written in for the build cache | `ast`, `cache`, `diag`, `ir`, `par` |
| `interp` | the tracking interpreter: runs the IR of a whole program with the primitives in Go and a simulated scheduler of behaviours, and reports a violation of the memory model | `ast`, `diag`, `ir` |
| `driver` | `Compile`: discovers the project and runs every module's stages, parse to generate, as one scheduled phase (the modules phase), with the build cache's export summaries, keys and entries, then the routines and the link; `Prepare` and `Interpret`: run them as far as IR and hand it to the interpreter | all of the above |

The `minimaxon` command itself (`main.go`) is the CLI and the corpus runner. `internal/grammargen` is the command
that generates code from the grammar, `internal/builtingen` the one that generates the built-ins from their table, `internal/irgen` the one that generates the IR's records and what reads them from the table of IR operations, `internal/diaggen` the one that generates the error code constants and message functions of `internal/diag` and `self/diag` from `internal/diag/codes.tsv` and `internal/diag/messages.tsv`, `internal/x64gen` the one that generates the x64 encoder of `self/backend` from its instruction table, `internal/abigen` the one that generates the ABI layout constants from their table, and `cmd/convert` is the standalone tool that converts existing Maxon source
to minimaxon syntax and layout; nothing imports either.

### Generated code

Code that only restates a fact is generated from the one place that fact is written, so it cannot drift from it
or be written wrong. That place is `internal/syntax/Maxon.g4`, the grammar, in a subset of ANTLR 4 notation that its
header explains. It says how each construct is written and which AST node it builds: a label such as `cond=expr` is
a field of the node, `@ast::Node { ... }` adds the fields later phases fill in or, written `Name Slot[Counter]`, says that the
nodes of that kind are numbered from a counter after the parse (see "The AST"), `@ast::Structs { ... }` names the nodes that
are Go structs (the declarations) and says by leaving the rest out that every other node is a record in the module's tree, and
a rule whose alternatives are nodes, such as `stmt` or `expr`, is a closed set of them. `internal/grammargen` turns it into:

| File | Holds |
|---|---|
| `internal/syntax/parser_gen.go` | a recursive-descent parser with one function per rule, and the stack for each list the parser builds; operator precedence comes from the order of `expr`'s alternatives |
| `internal/syntax/tokens_gen.go` | the token kinds and the lexer's DFA: every token rule and keyword compiled into one table over byte classes |
| `internal/ast/nodes_gen.go` | the kind of every node, the view of each record with an accessor and a setter for each field, the struct of each declaration, a view of each set, the constructors, the numbering of the slots (`Slots`, `NumberSlots`), the reserved words, and three visitors per set |
| `self/syntax/parser_gen.maxon` | the same parser in Maxon: `emit_parser.go` makes every decision once and writes it through a `lang`, Go's or Maxon's, so the two cannot drift. A parse function takes the parser and fails with `SyntaxError` after recording the error, as the Go one panics; every choice is an `if` chain, and every value is held by a local before it is stored, since Maxon refuses overlapping access |
| `self/syntax/tokens_gen.maxon` | the same token kinds and DFA in Maxon, its tables as strings of base-32 digits (a string literal is in the executable's data, so reading one allocates nothing), and the sets of token kinds as strings of a character per kind |
| `self/ast/nodes_gen.maxon` | the kind of every node, the accessors and setters of each record over the module's tree, the struct of each declaration with the fields its rules assign (a field named by a keyword takes what it holds as a suffix: `typeRef`, `whereList`, `isNoncopyable`) and its constructor, the numbering of the slots, and the reserved words |

The parser decides with one token of lookahead, or two where one is not enough. Where two alternatives start with
the same rule, as an assignment and an expression statement both start with an expression, it parses that rule once
and the token after it decides. A syntax error names what the grammar allows at that point, such as
`Expected an expression but got ')'`. The loop that runs the DFA, the scanner for a string literal (`-> scan`, since
interpolations nest), the loop over a module's declarations, and the conversion of a token to a field that is not
its text (`intLitMagnitude`, `strLitParts`, `boundValue`) are hand-written in `syntax`, and the same in `self/syntax`
(`lexer.maxon`, `parser.maxon`, `lists.maxon`, `source.maxon`).

The lexer takes the longest token at each position: a keyword wins over a name of the same length, and an earlier
token rule over a later one. It never backs off to a shorter match, so input that stops partway through a token,
such as `0x` or an unclosed `/*`, is an error naming the token from its `/** */` description ("incomplete
integer"). The generator rejects a token rule that matches the empty string or that an earlier token always beats.
Looking keywords up in the DFA instead of a map makes lexing about 1.4 times as fast.

Each set gets three visitor interfaces with one method per member, and a dispatch function for each. A set of records
is visited through the view of each node and dispatches on its kind; a set of structs through a pointer, with a type switch:

| Visitor | Method | Dispatch |
|---|---|---|
| `StmtVisitor` | `VisitIfStmt(n IfStmt)` | `VisitStmt(v, n)` |
| `StmtResultVisitor[R]` | `VisitIfStmt(n IfStmt) R` | `VisitStmtResult(v, n)` |
| `StmtStateVisitor[S, R]` | `VisitIfStmt(n IfStmt, s S) R` | `VisitStmtState(v, n, s)` |

Code that handles every member of a set goes through a visitor rather than a switch, so adding a member fails `go build` in
each phase that has not handled it yet. A switch on `Kind()` or a call of `AsCallExpr()` remains only where code asks about a
few members, such as whether an expression can be assigned to. Regenerate after editing the grammar:

```powershell
go generate ./internal/syntax
go generate ./internal/ast
```

The built-ins have one table, `internal/builtingen/table.go`: the built-in types, the functions and methods on them
with their signatures and the intrinsic each lowers to, and the runtime entries with the unit that defines each. From
it `go generate ./internal/ast` writes the declarations (`internal/ast/builtins_gen.go`, and
`self/ast/builtins_gen.maxon` for `self/`), the `Intrinsic` and `Rt` enums, and the dispatch from each intrinsic and
entry to its hand-written body: `check<Intrinsic>Args` in the checker for an intrinsic the table marks checked,
`lower<Intrinsic>` in the lowering, `entry<Entry>` in the interpreter, and `emit<Entry>` on the backend unit that defines
the entry. A dispatch names a method for every row, so a row added to the table fails the build wherever its body is
missing.

The error codes are a table, `internal/diag/codes.tsv`: each row is a constant, its code, the case of maxon's `ErrorCodeRegistry.maxon` that has that number (or `-` for a code of minimaxon's own, numbered from `E3900`) and what it refuses. Edit the table, then `go generate ./internal/diag` writes `internal/diag/codes_gen.go` and `self/diag/codes_gen.maxon`, and `go test ./internal/diaggen` fails if either differs. `go run minimaxon/internal/diaggen --maxon <maxon repository>` checks every row against maxon's registry.

The text of a diagnostic that carries a maxon code is maxon's, so that a spec's expected stderr matches byte for byte. `internal/diag/messages.tsv` holds it, one row per situation maxon words alone: a function name, the code constant and maxon's message with `{placeholders}`. The same `go generate` writes a function per row to `internal/diag/messages_gen.go` (`diag.UndefinedFunction(name)`) and to `self/diag/messages_gen.maxon`, which return the text while the raise site still chooses where the error is. A situation that only minimaxon refuses, or that maxon words for a rule minimaxon changed, keeps its own text at the place that raises it.

The IR has one table, `internal/irgen/table.go`: a row per instruction and terminator with its operands (each a name,
a kind such as register, slot, block, flag or pool index, and the field of the record that holds it), what it gives
(a register, an optional one or nothing), its effects (reads or writes memory, calls, allocates, frees, leaves the
function; one with a failure block may also fail) and the template the printer writes it by, plus the small enums
(`BinOp`, `CmpOp`, `GuardKind`) and the flags. From it `go generate ./internal/ir` writes `internal/ir/ir_gen.go`:
the `Opcode` and `TermKind` enums, the `Instr` and `Term` records, a constructor per row (unexported, for the
builder, when it indexes a pool), a view per row with an accessor per operand (`ir.BinInstr.Left()`), `Def`, `Fail`,
`Effects` and `Successors`, `InstrString` and `TermString` (the `--emit-ir` text), `EncodeInstr`/`DecodeInstr` and
`EncodeTerm`/`DecodeTerm` in the build cache's wire format, and the visitors `InstrVisitor`, `InstrStateVisitor[S, R]`,
`TermVisitor` and `TermStateVisitor[S, R]`, whose dispatch codegen and the interpreter go through. It writes the same
enums, records, constructors, getters (`binLeft(x)`), printer (`instrText`, `termText`) and visitors (interfaces with a
generic dispatch) to `self/ir/ir_gen.maxon`; `builtingen` writes self's `Rt` beside it (`self/ir/runtime_gen.maxon`).
The pools and the texts of a function's parts stay hand-written (`internal/ir/ir.go`, the builder,
`self/ir/function.maxon`). The lowering is ported to `self/ir/` function for function (`lowerer.maxon`, `lower*.maxon`,
`drops.maxon`, `try.maxon`, `sharing.maxon`, `primitives.maxon`, `arrays.maxon`, `os.maxon`, `instances.maxon`, over the
tables of `self/ast/tables.maxon` that the checker fills), entered by `ir.lowerModule` and printed by `ir.printIr`; the dispatch from an
intrinsic to its `lower<Intrinsic>` and `rtUnit` are generated (`self/ir/intrinsics_gen.maxon`, `self/ir/runtime_gen.maxon`). Maxon has no closures,
so what Go passes a function for (a scope, the branches of a conditional, the body of a counted loop, what to drop when a check
fails) is a begin and an end call around the code in `self/ir/`, which emits the same instructions in the same order.

The ABI layouts (what a box of each kind holds, at which offset) are generated from one table, `internal/abigen/table.go`,
so the runtime, the backends and the lowering cannot disagree. Each row is a name, its value (in words or plain), and
which of the files below get it:

| File | Holds |
|---|---|
| `runtime/layout_gen.maxon` | the word, the empty box, the string, array and byte array boxes, and the first capacity of each buffer, as `Count` constants |
| `self/ir/layout_gen.maxon` | `wordBytes` and `tagOffset`, which lowering sizes records and unions with |
| `self/backend/layout_gen.maxon` | `wordBytes`, the string length offset and the array box, which the drop and copy routines read |
| `internal/ir/layout_gen.go`, `internal/backend/layout_gen.go` | the same numbers for the Go compiler |

A byte array's box is an array's box (length, capacity, buffer address), so one set of offsets serves both. `go generate
./internal/abigen` writes them and `go test ./internal/abigen` fails when one is stale.

The x64 encoder is generated from an instruction table, `internal/x64gen/table.go`, into `self/backend/x64_gen.maxon`. The
Go compiler's own backend (`internal/backend/x64.go`) is not generated and goes when `self/` compiles itself; the table
is the encoder of `self/backend` from the start, and a test proves it against `x64.go`. A row is a form, one instruction with
one shape of operands, named as maxon's `X64Backend.maxon` names its encoders (`MovRegReg`, `LoadRegBaseDisp`, `PushReg`):

| Column | Holds |
|---|---|
| `name`, `mnemonic` | the encoder is `encode<name>`, and the case of the `X64Form` enum is the name with its first letter lowered |
| `operands` | in assembly order, each a register, a memory operand `[base + disp]`, a `[rip + disp]` operand, an immediate (`imm8`, `imm32`, `imm32U`, `imm64`), a `rel32` or a condition code, and the part of the encoding that holds it: the reg or r/m field of the ModRM byte, or the low three bits of the opcode (`+r`) |
| `prefix`, `rexW`, `opcode` | the mandatory prefix (`rep`), REX.W, and the opcode with its `0F` escape; a condition code is added to the last opcode byte |
| `modrm`, `digit` | no ModRM byte, one the operands fill, or one whose reg field is the form's `/digit` |
| `setsFlags`, `readsFlags`, `readsMemory`, `writesMemory` | what instruction selection asks of a form; written as `X64Form` predicates of the same names |

A register that is used as an 8-bit register is marked, and its encoder writes a REX prefix whenever the register is `rsp`
or above in number, since `spl` to `dil` exist only under one. The displacement of a memory operand is none, a byte or a
doubleword, as `x64.go` chooses it (a base of `rbp` or `r13` takes at least a byte; one of `rsp` or `r12` takes a SIB
byte), and `rip` and `rel32` operands take a displacement that the backend writes as 0 and patches (`lastField32`,
`rel32To`, `patch32`, `patchRel32`). A choice picks the first of several forms whose immediate holds the value
(`AddRegImm`: a byte, else a doubleword; `MovRegImm`: an unsigned doubleword, a sign-extended doubleword, else a
quadword), and a sequence is forms the backend always emits together (`SetccRax`). The generated file holds the register
and condition code enums (`X64Register`, `X64CondCode`, with maxon's case names), the immediate types, the helpers the
encoders share and one encoder per form, choice and sequence, taking a `mutates code ByteArray` first. Regenerate after
editing the table:

```powershell
go generate ./internal/x64gen
```

`go test ./internal/x64gen` fails if the file is stale, and encodes every form with registers `rax` to `r15`, displacements
from none to 32 bits, bases of `rsp`, `rbp`, `r12` and `r13`, immediates at the limits of each kind and every condition code,
once from the table and once through `internal/backend`'s `Asm`, and compares the bytes. `self/main.maxon`'s `selftest`
runs the generated Maxon on a set of those encodings (`golden` in the test) and compares them with the same bytes.

`go test ./internal/grammargen`, `go test ./internal/builtingen`, `go test ./internal/irgen` and `go test ./internal/x64gen` fail if a
generated file is not what the grammar or the table generates. A package exports only what another
package uses. What resolution and the check of the bodies decide is not in the nodes but in tables beside them (see
"The AST"); the declarations, which are structs, still carry the semantic types that declare fills in, which is why `ast` holds both.
Moving those into tables too, as `go/ast` and `go/types` do, would let the two separate.

An edit rebuilds only its package and the packages that import it: after a change to `check`, the compiler
rebuilds in about 0.4 s, and `go test ./internal/check` runs in about 0.3 s.

## Pipeline

`driver.Compile` discovers the project and then runs one scheduled phase, the modules phase (see "The modules phase"), in which
every module goes through the stages below, then makes the routines and links. A module's stage starts as soon as what it reads
is finished, as in Go's build: it waits only for the module's own stage before it and for the stages of the modules it names
whose results it reads, never for every module to finish a stage, and there is no serial pass in between. A worker that
finishes a stage of a module goes on to the module's next stage while its data is in cache, rather than sweeping every module
through one stage and then the next. Within a module the work is serial; the module is the unit of parallelism, except for the
routines and the link (a live section or a code unit per item). Results are merged in source order, which keeps the output
deterministic and independent of the schedule and of `--jobs`.

| Step | Package | Work per module |
|---|---|---|
| discover | `project` | walks the directory tree, one goroutine per subdirectory; the modules and their names are known from here on |
| parse | `syntax`, `project`, `resolve` | reads the module's files into one buffer, lexes the whole buffer in one pass and parses the token stream into the AST; anything the grammar does not allow is a syntax error. A module that parsed cleanly then gets its declaration table and an empty type object for each of its types, while its AST is in cache (a duplicate declaration is `E3006`, reported only if no module has a syntax error), and finds the modules its qualified names pass through (`resolve.NamedModules`), whose tables its resolve reads |
| resolve | `resolve` | sizes the tables of the module's `Typing` that have an entry for every node (see "The AST"), then binds every name to its declaration and records the modules the module depends on in its result, and each `where` constraint to the type parameter it names (`E3916`); it reads the module's own declarations, the standard library and the tables of the modules the module names, and checks visibility (`E3008`) and that no signature names a less visible type (`E3167`) |
| aliases | `check` | each typealias of an instance gets its type, after the typealiases it names, in source order (a typealias that names itself through others is `E2012`) |
| declare | `check` | the module's fields, cases, function signatures, type parameters with their bounds, and the interfaces its types implement; a type argument is only recorded for the check stage. It also numbers the module's nodes that have a slot, now that resolve has changed the kind of some, and sizes the tables of the `Typing` that only some nodes have. The root module's declare finds `main` |
| constants | `driver`, `check` | top-level `let` types, each after the constants of its module that it names (a cycle is `E2012`) |
| check | `check` | two stages, one straight after the other so the module's AST and types stay in cache. **Bodies** type every function body, a generic one once against its bounds, and the body of a behaviour as a function of its own that captures the variables it uses; a call into another module takes the callee's declared return type and is recorded, not checked; a call that needs an instance of a generic is recorded on its function, unless it comes after its block's live statements. **Verify** checks the recorded cross-module calls and every type argument against its parameter's bounds (`E3017`), then the range analysis, which decides how each operator and cast can fail and requires a `try` around what can (`E3057`, `E3059`; see "Checked arithmetic"), then ownership: moves, conventions, borrows, exclusivity, initialisation, frozen values and what a behaviour takes (`E3102`, `E3900` to `E3904`, `E3197`, `E2013`) |
| generate | `ir`, `backend` | two stages, one straight after the other so the IR is consumed while it is in cache. **Lower:** the module first finds the instances of generic functions that it needs: it starts from the calls recorded by its functions that are not generic, and follows each instance's recorded calls, whichever module declares the generic, with the type arguments bound, until no new instance turns up (`E3906` if the type arguments grow without end), in the order found. The live statements of each function, and of each instance, become IR (below) in the worker's reused buffers, with a drop where each owner dies, and unreachable blocks are pruned; a place that needs something the backend cannot run yet is noted, and the driver refuses it before codegen (`E9900`). **Codegen:** IR → x64, one code unit per module, which holds the instances the module found whoever else needs them, calls between units symbolic; the runtime is its own hand-written unit, and so is the array runtime, which a program that calls an array entry gets. Each unit notes the drop and copy routines its code calls A module that has a refusal is not emitted, and a program with any gets no executable; the runtime unit is emitted beside the modules. A module whose entry the build cache holds is not lowered or emitted at all (see "Build cache") |
| routines | `backend` | serial to start, then parallel: the drop and copy routine of every type the units asked for, and of every type those reach (a record's fields, a union's payloads, an array's elements), made in waves of units of 256 routines; the order of the routines comes from the modules in source order, so it does not depend on `--jobs` |
| link | `backend` | each function and routine is a section, each string and reserved value a piece of data, and only those the entry reaches through symbol and data references are kept (the runtime units are one section each). Every function is still compiled; what nothing calls is dropped here, as Go's linker does. Every step runs on all jobs: each unit enters its definitions into one table (a hash table of ids whose slots are claimed with a compare and swap, so it needs no lock; the ids count over the units in order, each definition having one; a symbol is its text, a link name, see "Build cache"). An instance is defined by every module that needs it, so the symbol of an instance may be defined more than once, and the table keeps the lowest id, which is the definition of the first unit, whichever job entered its own first: a definition that finds a lower id gives way and one that finds a higher id takes the slot. The other copies are never looked up, so the mark never reaches them and they are dropped like any dead section, as a C++ linker does with COMDAT "any". Any other symbol defined twice is an internal error; the mark follows the calls a round at a time, resolving a fixup only where live code uses it, with each round's sections shared out in chunks and the next round gathered in chunk order; each unit places its live parts from its own aligned start, and a running sum over the units in order gives each unit its base; each unit copies and patches its code into the image, which is the file in memory, while the file is created in the background and written in chunks as the code in front becomes final. The units go in module-path order after a runtime unit, then the routines, and the array runtime comes last. The layout depends only on which sections are live, so it is the same for any `--jobs` |

`driver.Prepare` (and `Interpret`, which runs the result once) runs the same phase through the lower stage of `generate` and gives the IR to
the interpreter instead of codegen, which is how a map, a list, a pool, a frozen value, a cown and a behaviour run. `minimaxon check` runs
the modules phase with the instances alone in place of `generate` (a generic that grows its own type argument without end is `E3906`),
so it has the same edges and reports the same errors.

### The modules phase

Each module has a task for each stage of the pipeline from parse to generate, a closure that joins checks, and with a build
cache two tasks more; `par.Run` runs them over the `--jobs` workers. `par.Plan` is the graph of tasks: for each task the tasks it
needs, and its group. A module's dependencies are only known once it is parsed and resolved, so the plan starts with each task
of a module needing the module's task before it, and a running task adds the needs it has found to the later tasks of its own
module (`par.Edges`), which wait for it and so cannot have started.

What each task reads of other modules decides what it waits for. A module's dependencies are the modules whose declarations it
names (the edges of `project.Graph`), known once it is resolved; and since every name of another module is qualified, the
modules its resolve can look into are known once it is parsed: the head of `net.http.parse` names a module, and module names
are known from discovery.

| Task | Needs, besides the module's task before it | Why |
|---|---|---|
| parse | nothing | reads only the module's files; its table and the modules it names come from its own AST |
| resolve | the parse of each module that the module's qualified names pass through (added by its parse) | binding `net.http.parse` reads the table of `net` (is `http` a declaration there, or a module?) and of `net.http`; the head is found among the root's children, which discovery made. Visibility reads the target's modifier and its module's path, both in the table |
| aliases | the aliases of each module it depends on (added by its resolve) | a typealias of an instance can name another module's typealias, whose type is made first; the module's own are ordered by what they name, and a cycle among them is `E2012`. A cycle across modules is an import cycle |
| declare | nothing more | reads the module's own syntax and resolution, the type objects of the tables, which parse made, and the types of typealiases, which its aliases waited for. Another module's declare can still be running: a signature needs only the type object of another module's type, and what it needs of its declaration (a bound, a conformance) is checked in verify |
| constants | the constants of each module it depends on (added by its resolve) | a constant can name another module's constant and read the declarations of what it names; those constants waited for their module's declare, and so on down the graph. The module's own constants are typed after the ones they name, and a cycle is `E2012` |
| check | nothing more (with a cache, the module's probe, and for a hit that shows its check results its demand, added by its probe) | reads the declarations and constants of the modules it reaches, which its constants waited for, and writes only its own module's `Typing` and result; no module reads another's check before its closure |
| closure | the closure of each module it depends on (added by its resolve) | no work but a join: it is done exactly when the checks of the module and of every module it reaches are, so the plan has one edge per dependency and no module holds the set of the modules it reaches |
| generate | nothing more | the instances, and the lowering and codegen, or only the instances for a compile that makes nothing; it reads the bodies and check results of the generics it reaches in other modules and the marks verify leaves on their constants, which are all in modules it reaches, and nothing of a module that depends on it |
| probe (cache) | the module's constants, and the probe of each module it depends on (added by its resolve) | makes the module's export summary, which folds in those of the modules it depends on, and its key, and reads its entry (see "Build cache") |
| demand (cache) | the join of every module's resolve, and the probe and demand of each module that depends on it (added by that join) | whether a miss reaches the module, which says whether a hit that shows its check results is checked. Which modules depend on a module is known only once every module that could name it is resolved, so this is the phase's one join of every module, and only such a hit's check waits for it |

A compile that makes an executable has one more task, after the root's declare, that emits the runtime unit, which calls `main`.
The tasks of a module are numbered together and in module order, and the module is their group: a worker that finishes a task
goes on to a task that has just become ready in the same group (the same module), where its data is in cache, and otherwise
takes the ready task of the lowest number, which is the lowest module's (`par.Run`). A module that depends on nothing is
therefore generated while others are still being parsed, and no stage of a module waits for the modules that depend on it.
Tasks write only the slots of their own module and the phase's results are merged in module order afterwards, so nothing
depends on the schedule.

The errors do not depend on it either. They are those of the first stage, in the order syntax, table, resolve, import cycle,
aliases, declare, constant order, constants, bodies, verify, that has any, from every module, as if each stage were a pass over
every module that stopped the compile when it failed: a duplicate declaration is not reported beside a syntax error, nor a
verify error beside a body error. A stage runs only while no stage before it has failed in any module, so it never reads a
result that has an error and every module runs the first stage that fails; a stage that started before an earlier failure
turned up is dropped with what it found, since each module counts its errors at the end of each stage. The import cycle is a
stage of no task: a cycle leaves the tasks that wait on it unrun, and only then, if resolve found nothing, is the module graph
sorted (Kahn's algorithm, linear in the edges) to report `E2012` where the first module of the cycle names the second, so a
program without a cycle pays nothing for the check. A generate runs only if no stage has failed and the module's own check and
the check of every module it reaches ran verify and found nothing, so a module that has an error is never lowered. After the
errors come the instance errors of the first module that has one, then the refusals of every module, sorted. A task that panics
(a bug) is recorded with the stage it was in and releases the tasks that need it, which then run no stage from that one on;
the panic of the lowest task at the first stage that panicked is raised unless an earlier stage has errors, which are reported
instead, so what is raised does not depend on which worker was where.

An error about a bare name that was not found names the declarations of that name in other modules, which is the one thing
that reads every module's table. A module is checked as soon as the modules it names are, so that hint is written when the
error is located, once every module is parsed (`ast.Project.Locate`).

`--timings` prints one line for the phase, `modules`, with the busy cores of the whole phase, and under it the time each kind
of task took, summed over the workers. A `--trace` has a region for each task kind on each worker, beside the mark at the
phase's end.

A module is named by a handle, `ast.ModuleID`, its index in `Project.Modules` (the root, then each module's children in
name order, depth first), and what the stages produce for a module is not part of `ast.Module` but of its
`ast.ModuleResult`: the errors, the modules it depends on, its functions, the calls and bounds that wait for the
verify stage, and the functions that need instances. The driver owns one result per module, indexed by handle; each
stage fills in the result of the module it works on, and reads another module's result only when it waited for the stage
that made it.

The modules a module depends on are the edges of the module graph (`project.Graph`): resolve records, for each module,
the other modules whose declarations it names, as a sorted list of handles without duplicates, each with the first
place it is named. The graph, with its reverse edges, is made only by the build cache's join and by the cycle check.

A module's tasks have explicit inputs and outputs, which is what the per-module build cache and a language server need of
them. The inputs of check are the module (its syntax tree, resolution and declarations) and the declarations and constants of
the modules it depends on; its outputs are the module's errors, its `Typing` (types and the ownership marks), the calls and
bounds that verify checked, and the functions that need instances. The inputs of generate are the module's results and the
`Typing` and results of the modules it reaches; its outputs are the module's refusals, its IR text and its code unit.

### Reading a module

A module's files are read into one buffer, a string allocated once at the sum of their sizes, as they are written:
offsets in it are the files' own byte offsets, and a byte order mark is an error. Every token's text is a
substring of it, so lexing allocates no text and needs no intern table. A string cannot be read into, so each file
is read into the worker's scratch buffer, which grows to the largest file the worker has read, and copied from there. Comments are `-> skip`
token rules, so the lexer drops them; a `///` doc comment stays in the buffer for the LSP to use later.

A token is 8 bytes: its kind and length packed into one 32-bit word (8 bits of kind, 24 of length, so a token is
at most 16 MB) and its start offset in the buffer. Offsets are 32-bit, so a module's source is at most 4 GB. A
token carries no text, value or position; the parser reads them from the buffer when it needs them. Its text is a
substring of the buffer, an integer's value is read again from its digits, and a string's text runs are substrings
unless an escape forces a copy. Storing the length is about 15% faster than re-lexing a token to find it, and the
packing keeps it at 8 bytes.

A node's position is its token's offset and its module's handle (`ast.Pos`, 8 bytes), and the module's `ast.Positions`
knows where each file starts and, once something asks for a line, where each line starts. Placing a node therefore stores
a 32-bit offset and reads nothing else; the index of line starts is built only when a diagnostic (or a message that names
a line) needs a line and column, so a program that compiles never builds it. `node.Error` raises a diagnostic at a node,
and `Project.Locate` and `Project.Line` are the ways to ask for the line (see "The AST").

The lexer works on the raw UTF-8 bytes, so a column counts bytes. One lexer pass covers the whole buffer. At each
file boundary it resets the bracket depth and the indentation, so no token spans two files, and emits a `FileEnd`
token. It reads a file line by line: it checks each line's indentation and emits `Indent` and `Dedent` where it
changes, checks blank lines and comment lines, and runs the DFA over each code line, stepping over the one space
between two tokens without a round of the DFA. One parser reads the module's stream, naming each file in its
diagnostics. A declaration left open at the
end of a file fails there with `E2007`, and parsing resumes at the next file, so each file reports its own first
syntax error.

### The AST

The AST is a module's `ast.Tree`: its nodes are 16-byte records in one array, and what a record does not hold itself is in
one list array and in the module's text. A phase never writes a node to tell another phase something: what it finds out is a
table it owns, indexed by `NodeID`. Nothing adds a node after the parse except resolution (below), so a table sized by the
node count has an entry for every node. The pipeline leaves the executable and the diagnostics byte for byte as the
pointer-based AST made them.

**Handles and widths.** Each number is 32 bits, and a handle that crosses modules is two of them:

| Type | Is | Width |
|---|---|---|
| `ModuleID` | a module: its index in `Project.Modules` (`StdlibID`, -1, is the standard library, which has no result) | `int32` |
| `NodeID` | a node among the nodes of one module: its index in `Tree.Records`, counted from 1 in the order the parser makes them; 0 is no node | `uint32` |
| `Pos` | where a node begins: its module (`Mod`) and the byte offset `Off` | 8 bytes |
| `Ref` | what every view holds: its tree and a `NodeID`; it is how code names a node it does not own | 16 bytes |
| `DeclRef` | a top-level declaration: its module and its `Ordinal`, its index in `Module.Decls` | 8 bytes |
| `LocalID` | a local variable of a function: its place in the function's `Locals`, plus one; 0 is no local | `uint32` |
| a slot | a node's place among the nodes of its kind that a table has an entry for (see "Side tables"); 0 is no slot | `uint32` |

A module has at most 4 GB of text and a node covers at least one byte of it, so 32 bits count its nodes, and a project has
fewer than 2^31 modules. A reference inside a module is a bare `NodeID`, 4 bytes, and a pair appears only where a reference
crosses modules. An index depends only on the module's text, never on another module, so a module that did not change keeps its
indexes, its tables and its text when a later compile reuses it (the language server, a build cache). What is project-relative is
`ModuleID`, in `Pos.Mod` and in the lists that a table holds: it is the one number to renumber when modules have been added or
removed.

**Records.** A node that is not a declaration is a record:

```
Record { Kind uint8; Flags uint8; Aux uint16; Start uint32; A, B uint32 }   // 16 bytes, four to a cache line
```

`Start` is the node's `Pos.Off`. The grammar says everything else, and the generator lays each node out from its fields: a bool
is a bit of `Flags`, a choice among literals (an operator, a convention) a byte of `Aux`, a name, a label or the start of a
string one word that holds its offset in the module's text plus one (a name is as long as its identifier characters run on, a
label runs to its closing quote, so a record holds no length), an integer of up to 64 bits two words, a child the `NodeID` of its
record, and a list or a block the index of its count in the list array, with 0 the empty list. A node with up to two such words
keeps them in `A` and `B`; a node with more keeps the first in `A` and the rest in a block of the list array that `B` points to.
The list array holds the lists in the order the parser finishes them, each its count followed by that many words, and the
blocks. A string literal's parts are pairs of words: an interpolated expression, a run of the text, or a text that an escape
made (kept in `Tree.Strings`). The `Tree` is sized from the module's tokens (about one record per two tokens and one list word
per two), so a parse allocates it once.

| Nodes | Words | Bytes |
|---|---|---|
| a name, `self`, an end label, a bool, an integer, a string, a member, an index, a call, a cast, a unary, a binary, an argument, a struct field, a struct literal, a `panic`, and the statements `_ =`, assignment, expression, `return`, `break`, `continue` and `match` | 1 or 2 | 16 |
| a type reference, a `let`, a conditional, a match arm | 3 | 16 + 8 |
| a `while`, a match, a pattern | 4 | 16 + 12 |
| a `when`, a `for`, an `if` | 5, 6, 7 | 16 + 16, 20, 24 |

A view of a record, such as `ast.IfStmt` (a `Ref` and nothing else), has an accessor for each field (`n.Cond()`, `n.Then()`)
and a setter that only the parser and resolution call. A set is a view that knows its node's kind: `ast.Expr` has `Kind()`,
`AsCallExpr()` (the view of the node as a call, if it is one) and `ToCallExpr()` (for code that has checked), and a node's view
has `Expr()` to go the other way. A list is a `List[V]` of views, with `Len`, `At`, `All`, and `Prefix` (the live statements of a
block are a prefix); a list of names is a `TextList`. An optional child that is not there is the view of node 0: `IsNil()`. A
view is compared with `==`, and a diagnostic at a node is `n.Error(code, message)`.

**What stays a struct.** The declarations (`@ast::Structs` in the grammar): functions, constants, the type declarations and what
only they hold (parameters, fields, cases, bounds, type parameters, constraints, interface signatures). Later phases fill in
their semantic fields, and other modules read them, so they are structs with the `ast.Head` that gives them their position, their
number and the length of the text they cover (the parser sets it; the build cache's export summary reads a generic's source from it), in chunks of their type, and each has a record in the tree that holds only its kind and where it starts, so that every
node has a number and a position. A struct holds views of the records it has (`Body StmtList`, `Returns TypeRef`) and never a
pointer to one. A function that checking makes, the body of a behaviour, is a struct that is in no tree.

**Resolution rewrites in place.** `a.b.c(x)` where `a.b` is a path of modules parses as members, and resolution makes the member
that ends the path the name of the declaration, by changing its kind in place (`MemberExpr.BecomeName`): it keeps its `NodeID`,
so the member of `c` above it still holds it, and the module has no node more than the parser made. A behaviour that takes a
variable of the function around it moves the variable in through a node that stands for the name at the `when` (`Capture.Ref`);
resolution is the phase that knows which names can be taken, so it makes those, one copy of the name for each behaviour around
it, after the last record (`Tree.AppendCaptureRefs`). It is the only phase that adds a node, and every phase after it finds the
tree complete.

**Side tables.** A table is a slice owned by the per-module result (`ModuleResult`), made by the one phase that fills it and only
read after it. `ModuleResult.Resolution` is indexed by `NodeID`: which declaration each name, struct literal and type reference
names, and which type parameter each type reference and constraint binds. About one node in ten is bound, and to few things, since
a module names a handful of declarations many times, so an entry is 4 bytes: a slot in the list of the distinct things the module
bound anything to, each a `DeclRef` or a type parameter, and slot 0 binds nothing, as for a local, a field or a primitive. The
checker, the driver's constants and the alias pass read a table through `Resolution.Decl(project, node.ID)` and
`Resolution.TypeParam(node.ID)`, which return nil where a node names none; a node outside the table is an internal error, not a
default.

`ModuleResult.Typing` is what the check pass decided about the bodies. What nearly every node has is indexed by its `NodeID`; what
only some kinds have is indexed by a slot, which is the number of the node among the nodes of its kind (the grammar says it:
`Info Slot[Call]` stores nothing, and the generated `Tree.NumberSlots` numbers the nodes after resolution, in one pass over the
records), and the one table `Typing.slots` finds a node's slot by its `NodeID`:

| Table | Indexed by | Entry | Bytes |
|---|---|---|---|
| slots | `NodeID` | the slot, for the kinds below that have one | 4 |
| types | `NodeID` | the `Ty` | 16 |
| consumed | `NodeID` | a bit, which the ownership pass sets | 1/8 |
| arithmetic | `Arith` slot: a binary or unary operator, a cast | how lowering compares the operands of a binary operator (word, unsigned or string), and how the operation can fail, which the range analysis decides (`ast.Fails`) | 2 |
| names | `Name` slot: a name written as an expression | what it stands for, as a handle: a `LocalID`, a field's index, or a constant | 4 |
| members | `Member` slot | the kind, the field, the case | 16 |
| calls | `Call` slot | the kind, target, passing, generic call, union and case, ordered arguments, the destructor call of a `dropWith` | 72 |
| struct literals | `StructLit` slot | the record and the field values in order | 32 |
| patterns | `Pattern` slot | the case and a `LocalID` for each binding | 32 |
| behaviours | `When` slot | the function the body became and what it captures | 32 |
| locals | `Local` slot: a `let`, a `for` | a `LocalID` | 4 |
| jumps | `Jump` slot: a `break`, a `continue` | the `NodeID` of the loop or match | 4 |

A name is a third of the expressions or more and a call a tenth or fewer, so a name's entry is four bytes (its kind and a handle
share a word) and a call's is not paid by the expressions that are not calls; the receiver of a call is not stored, because it is
the object of the member the call names. A parameter's local is not in a table but in its function (`FuncDecl.ParamLocals`), and
the call that a cown's `dropWith:` names is the `CallInfo` of the cown's call, with no node. `Typing.Size` makes the tables that have
an entry for every node (the resolve stage does it, while the module's tree is in cache) and `Typing.Reset` numbers the
slots and sizes the rest after resolution. The types are 16 bytes for every node, the statements, declarations and type references
too, which is more than a table of only the expressions would take; handing the types out as 4-byte handles into a per-module
table of the distinct types was measured and kept out (see "Allocation").

An entry is read with the `Typing` of the module its node is in. The check of a module only needs its own, but lowering follows a
call into another module's generic, a constant into the module that declares it, and a field's default into the record's:
`ast.Typings`, the typings of every module by `ModuleID`, is what the lowerer and the instance finder (`ir.Instances`) hold, and the
node's own module (`node.T.Module`) says which. The check of every module a module reaches has finished before its generate starts (its closure waits for them), so
reading them needs no lock: the typings are the ones in the results the driver owns, and lowering only calls their read methods (the
`Set` and `Edit` ones are the checker's). Reading an entry that does not exist (a node outside the tree, a node of a kind with no
table, or an expression that was never typed) is an internal error and not a default. A local is read through the function whose
body holds the name (`name.Local(fn)`), because it is a position in that function's `Locals`; the local that a capture names in the
function around a behaviour is read through that function. The constants of a module are checked at the same time as each other,
so the tables are sized before the first; naming a constant takes a lock for the one list that the constants share. `Consumed` is a
bitset the ownership pass writes while it verifies the module, and lowering, including that of another module's constant, only reads
it, which is after every module's verify.

**Positions.** `Pos` names its module and does not point at its text: `Module.Positions` holds it, and the line index is built only
when something asks. A view's `Position()` is the module and the record's `Start`. Raising a diagnostic at a node,
`node.Error(code, message)`, records a `diag.Site` (a module handle and an offset), and the driver locates the error
(`Project.Locate` gives it its file, line and column) when it reports, so a compile with no error never builds a line index. A
message that names a line asks the project for it (`Project.Line`). An error made where its place is known, a syntax error, is
located already. A function that takes a place to report at takes an `ast.Pos`, not a node: a view that is passed as an interface is
allocated.

**In Maxon.** A tree is a struct that owns an `Array` of `Record` structs, an `Array` of `U32` and the module's text; a `NodeID` is
an integer, a table is an `Array` of a small struct, and a view is a value that borrows the tree, so nothing in it is a shared
pointer and a program can own every part of it.

**What is left.** What declare writes into declarations is still in them, because other modules read it: a declaration's `Ty`
(`TypeDeclNodeBase`, `TypeParam`, `ConstDecl.Type`), a signature's passing and return type (`FuncSig`), and the semantic fields of a
function (owner, generics, bounds, instances, the calls it records, parameter and return types, passing, its locals and the
receiver's, intrinsic). A `FuncDecl` is 464 bytes: it is most of what a parse allocates now, and its syntax (name, parameters,
body, labels) and its semantic fields could be two parts, the first a record. The pointers that remain are those, the ones inside the
entries of a `Typing` (a call's target, passing, generic call, union and drop; a member's and a struct literal's field and record; a
behaviour's function and captures, whose `Inner` is a local; the constants a module's names stand for), a declaration's `Module`, and
what a result keeps for the check (`PendingCall.Call`, `FuncDecl.Uses`, `FieldInfo.Uses` and `Default`). A method is not a top-level
declaration and has no `DeclRef`, so a call's target becomes a handle together with the function it names.

### Allocation

Parallel phases are only as fast as allocation allows. During a compile the driver turns the collector off
(`debug.SetGCPercent(-1)`) under a memory limit of 100 bytes per source byte, at least 256 MB, so a compile that
outgrows it still collects. With the collector off no memory is reused, so every byte allocated is a fresh page the
operating system commits and zeroes: on Windows that costs about 0.2 s per GB, a large share of a compile. What
allocates is therefore what costs, and the phases avoid it:

- the lexer reuses token buffers of 8-byte tokens and takes token text from the source string, and an
  interpolation's parser borrows a spare token buffer. `syntax.Scratch` holds what a worker reuses for each module it parses: the
  file buffer, the token buffer and the stacks the parser builds lists on;
- the parser writes a module's nodes into one `ast.Tree`: the records and the lists are made once, at a size taken from the module's
  token count (the tree allocates about half a record and 0.6 list words for each token, which a parse of 16 million tokens fills to
  within a tenth), and a declaration's struct comes from a chunk of its type, twice the one before up to 64. A list in the grammar
  (`stmts+=stmt`, `args+=arg`) is pushed on a stack of its own as it is parsed, and copied out once into the list array, where
  `append` would have allocated about twice that. The records are 125 MB of the 396 MB a parse of a large project allocates, the
  lists 36 MB, and the declarations most of the rest (a function is 464 bytes);
- a module's declaration table is made at its final size, and the line index of a module's source is built only when a diagnostic needs it;
- codegen sizes each unit's code buffer and its symbol and data fixups from the IR it is about to emit, and reuses its
  block labels between functions, because Go grows a large slice by about a quarter at a time, allocating several times
  its final size; the routines are sized the same way, a call names its target by the symbol itself (a function, or an
  `ir.Rt`, which is a byte and so makes no interface value) and a call of a routine names it by an interface value that
  is made once for each type in a unit, not once for each call;
- the link fills in the image that is the file, so the code and the data are copied once and nothing is built twice, and it
  reads each unit's data in place; a symbol is defined by a slice append in codegen and found by a table made once,
  not by a map in each unit and another for the link;
- the ownership pass shares its state until a move or an initialisation changes it, and reuses its loop and
  match frames;
- lowering builds each function in a worker's buffers (`ir.Lowering`: a builder, an arena, and the lowerer's own stacks
  of scopes and jump targets) that every function and every module of that worker reuses, because an instruction is a
  record in a slice and not a heap object, so lowering a project allocates a few megabytes instead of about a gigabyte
  for sixty megabytes of source (`par.ForEachIndexWith` gives each worker its scratch, made on its own goroutine).

A type table of 4-byte handles was tried and is not kept: each module interned the types of its expressions in a table (fixed handles
for `bool`, `String`, `void` and `never`, a map for the others, integer constants by value, a lock while the constants of the module
are checked together), which cut what declare allocates by 94 MB on a project of 7.8 million nodes and made the check 11 ms slower,
since every typed expression looked its type up, and the build 7 ms slower (468 ms against 475 ms, the median of 11 alternating
runs). The handles would pay only where the lookup is cheaper than the cache misses of a 16-byte entry.

Watch the MB column of `--timings` when changing a phase.

## Build cache

A rebuild after an edit redoes only the modules the edit can have changed: the rest come from a cache of what `generate` made for each
module, as Go's build does, and the executable is byte for byte the one a build with no cache makes. The front (parse to
constants) always runs for every module, so every error it can report is reported as without a cache, and check and generate are
where a hit saves its work. Only a build that makes an executable uses the cache: `check` and `interp` do not.

### Link names

A code unit names the functions it defines and calls, the data it defines, and the routines it asks for by text, never by an address or
by a number that counts the project's modules, because a unit that is kept must mean the same in a later compile. That text is a
**link name** (`internal/ast/linkname.go`). It starts with the module that declares what it names: its path, `export` for the project
root (a directory cannot be called that) and `stdlib` for the standard library. So two modules may each declare a `helper` or a
`Counter`, and an instance applied to one of them is not the instance applied to the other:

| Names | Link name |
|---|---|
| a function | `geometry::area`, `export::main` |
| a method | `export::Counter.inc`, `(export::Box with stdlib::Count).make` |
| an instance of a generic function | `export::swap[stdlib::Count, export::Counter]` |
| a type | `export::Counter`, `export::Box with (geometry::Shape, Count=int(0 to 18446744073709551615))` |
| a drop or copy routine | `drop export::Counter`, `copy stdlib::Array with export::Tree` |
| a runtime entry or global | `rt.alloc`, `rt.heap` (the text of `ir.Rt`) |

An integer type is named by its alias and its range, since the code for it depends on nothing else. The kinds of symbol cannot meet:
a function's name has `::` before any space, a routine's starts with `drop ` or `copy `, and the runtime's have neither. A function's
link name is made when the function is declared, or when an instance is made, and a type's when the type is, so naming a callee costs a
field read. The text of a function in the IR (`--emit-ir`) and in a message is its plain symbol, as before. The link keeps one definition
of a symbol that a unit marks as an instance, which every module that needs the instance defines, and refuses any other symbol that is
defined twice. `ast.ParseLinkType` reads a type's link name back as the type, in a project whose declarations are made, which is how the
routines a kept unit asks for are found again.

### What an entry holds

The entry of a module is the output of its `generate`: its code unit (the code, the data, the references and definitions to link, the
sections and pieces the link keeps or drops whole, and the drop and copy routines it asks for, each as a kind and the link name of a
type), whether it calls an array entry, and, if the build that wrote it asked for `--emit-ir`, the module's IR text. A module that has an
error or a refusal (`E9900`, a runaway generic) has no entry: nothing a diagnostic holds is kept, so a diagnostic is always made, and made
the same way, by the compile that reports it. A build that does not ask for the IR leaves entries without it, so a build that does is a miss
for each of them and writes entries that serve both. Writing happens in the module's own generate task, on whatever worker made the
unit.

### The key

A module's key is a SHA-256 of: the compiler (the hash of the executable, computed once per process), the flags that change what a module
compiles to (`--count-allocations`; not `--jobs`, `--timings`, `--trace` or `--emit-ir`), the module's link path, a digest of its source
(its text and where each file ends in it; a file's name is only where a diagnostic says a thing is), and **the export summaries of the
modules it depends on**, in module order. A summary is a digest of what the modules that reach a module can depend on (the next section), so
a key holds the module's own source in full and, of everything it reaches, only what the module can read. An edit that no module can see
makes a miss of the edited module alone. The directory a project is in, the handles of the modules and the schedule are not in a key;
adding or removing a module that nothing in a module's closure names changes none of its key (the handles move, and the entries do not
care).

### The export summary

`internal/driver/summary.go` is the one place that says what a module shows to the others. Its digest holds, in the order the source gives
them and with every list's length, nothing that a handle or an address decides: a type is its link name, a module its link path.

| Shown | What is written |
|---|---|
| every type declaration, exported or not | parameters and their bounds, what it implements, `noncopyable`; a record's fields (name, visibility, mutability, type, and the source of a default); an enum's cases and raw values; a union's cases and payloads; a typealias's type; an interface's signatures; and every method's signature, visibility and convention |
| each function that other modules can use | its name, visibility, conventions, parameter names and types, return type, type parameters and bounds; a generic's **source** |
| each constant that other modules can use | its type, which holds an integer constant's value, and its **source** |
| the source of each field's default, and of each generic function and method | which a module that builds a record or calls the generic lowers in its own unit |

Types are shown whole, whoever may name them, because a signature that mentions a type lets a module that cannot name it read its layout,
its fields and what it implements. A function or constant is shown only if another module can use it: an exported one, and, in a module that
has a generic or a field's default, every function, since a generic or a default can call any of them and the module that lowers it reads
the callee's signature; a module that shows a constant shows all of its constants, since an exported one may be written with a private
one. In a module with no generic and no default the signature of a private function is not shown, nor is any body that is not generic,
nor a comment outside a generic, a constant or a default. The summary of a module also says whether the modules that reach it read its
check results (`ReadThrough`): it does if the module has a generic, an exported constant or a field's default.

A summary is **transitive**: a module's summary folds in the summaries of the modules it depends on, in module order, so it changes when
anything the module can show through them does. It has to, for two reasons that an edge of the graph does not give. A signature of a module
mentions the types of the modules it depends on, and a module that calls it reads those types' layouts and members without naming their
modules, so no edge leads to them; and the check results of a generic that another module reads are made from what the generic's own
module reaches. Folding is the same information as putting the summaries of the whole closure in the key, computed in one pass over the
edges instead of one walk per module. What it costs is that a change to what a module shows makes a miss of every module that reaches it,
though one of them may read none of it; folding only the dependencies that a shown declaration names, and only the private declarations a
generic reaches, would be finer, and is left for later.

### What a hit skips, and why that is right

A **hit** is a module whose entry is there, whole, and holds what the build needs. Its `generate` does not run. Its `check` runs only if some
module that is **not** a hit reaches it and its summary says that module reads its check results (it has a generic, an exported constant or
a field's default), since that is all a generate reads of another module's check: a hit that shows none of them is never checked, and a
module that no miss reaches runs neither. A miss does what it always did. A miss reaches the whole closure of the modules it depends on,
not only the ones it names: it can instantiate a generic that a signature of a module it names leads to, so a hit between a miss and a
generic is not a reason to skip the generic's check.

Why a hit is the right output, for each thing `check(m)` and `generate(m)` read of another module d. Each read is of d's summary or of
something that the summary of a module d reaches holds, and the key holds the summaries:

- **Declarations, for the check of m.** Types' fields, cases, raw values, layouts, method tables with their conventions and visibility,
  signatures, bounds, interfaces and conformance, aliases, and constants' types and values are what the front leaves in the declarations,
  and the summary writes each of them. A function or constant that is not exported cannot be named from m (`E3008`, which the front
  reports for every build, cache or none), and one that m reaches without naming is a method, which is in the type. A type that d's
  signature leads m to is in a module d depends on, whose summary is folded into d's.
- **Which names resolve where.** A bare name is the module's own declaration or a built-in, and any other name is qualified by the
  module that declares it, which is then a module m depends on. A declaration that such a module gains, loses or changes the
  visibility of changes that module's summary, since the names a module declares and their visibility are in it; the module's own
  declarations are m's source. So no way for a name to resolve elsewhere leaves m's key as it was.
- **The bodies and check results of generics.** An instance is lowered in the module that needs it, from the generic's body and the
  results of checking it. They are made from the generic's source (in the summary), the declarations of its module (all of them, since
  the module shows every function when it has a generic) and what its module reaches (folded). The same holds for the instances that the
  generic's body needs in turn.
- **Constants.** A constant's value is lowered inline, from its expression (in the summary), the other constants that the expression names
  (all of the module's constants are shown) and the marks that the ownership pass leaves on it (made from the same inputs).
- **Field defaults.** A record literal in a generic method lowers the default of a field it leaves out: the default's source is in the
  summary, and its check results are made from what the generics' are.
- **Layouts, drops, conventions.** The offset of a field, the size of a box, a union's cases, an enum's values, whether a type has a
  destructor and how a callee takes its arguments come from declarations, so from the summary. Callees are named by link names and never
  inlined, so the body of a function that is not generic is not read: that is why an edit to one is not in the summary.
- **The root, the runtime and the standard library.** The root is a module like any other. The runtime unit, and the entry that calls
  `main`, are made by every build and are not cached; the standard library and the code generator are the compiler, which is in the key.
- **The module's own name**, which every symbol in its unit holds, is in its key and its summary: two modules of the same text have two
  entries.

Errors stay as without a cache, because the cache is only consulted for a module that would compile: a module has an entry only if its
check and its generate found nothing, so a module with the same key finds nothing again, since its check reads nothing outside the
summaries it is keyed by and its own text. A module that is unchanged and would now fail, because a module it reaches changed in a way it
can see, has another key, is a miss, and is checked, so the build reports the errors of a build with no cache. A change that it cannot see
cannot make it fail. The front runs in full for every module, so its errors are reported as ever, and an error in the modules phase is
reported only for modules that were checked, which are the ones that can have one.

What an edit costs, in a project whose modules are `core` (nothing generic), `gen` (a generic, a constant and a field's default), `mid`
(calls both) and the root (calls `mid`):

| Edit | Misses |
|---|---|
| the body, a private helper's body or signature, or a comment in a function of `core` (not generic) | `core` |
| a function of `gen` that is not generic, or a comment between its declarations | `gen` |
| the body of `mid` or of the root | that module |
| a signature, an export added or removed, a field, a method's signature or a visibility in `core` | `core`, `mid`, the root |
| the body of a generic, a constant's value, a field's default in `gen` | `gen`, `mid`, the root |
| a private function's signature in `gen`, which has a generic and so shows every function | `gen`, `mid`, the root |
| an export added and then taken away | none: the summaries are as they were, and the entries from before the addition are found again |

### Storage

The cache is a directory, by default `minimaxon\cache` in the user's cache directory (`%LocalAppData%` on Windows). `MINIMAXON_CACHE`
names another directory, or says `off` to turn the cache off, and `--no-cache` turns it off for one `build` or `run`. `minimaxon test` and
`go test` never use the user's: the runner does not use a cache, and the tests that do use a temporary directory. The directory can be
deleted at any time, and a compiler that finds nothing in it makes what it needs.

An entry is a file `<first two hex digits of the key>/<the rest>`. It is written whole to a file of its own in that directory and
renamed over the name, so a reader sees the old entry or the new one and never part of one, a crash leaves at most a stray `tmp-*` file,
and compilers that write at once cannot corrupt each other. A failure to write is counted and never fails the compile. A read that finds a
file that is missing, cut short, longer than it says, of another version, damaged (the checksum is CRC-32C), or not a code unit is a miss,
and the module is made and written again.

The file is `MXCE`, the layout version (`cache.Version`, 32 bits), the payload's length (64 bits), the payload, and the checksum of the
payload (32 bits), all little-endian. The payload is written in the wire format of `internal/cache`, which a program in any language can
read: an unsigned integer is a base-128 varint, a bool is a byte, a string or a byte string is its length and its bytes, and a list is its
count and its items; nothing is aligned and no reflection or `encoding/gob` is involved. An entry's payload is: whether the module uses an
array entry, whether the IR text is there, the IR text, and the unit, whose layout `backend/unitcodec.go` documents. A decoder checks every
count against the bytes that are left, so a damaged entry asks for no memory.

### Trimming

Every build of the compiler makes a fresh set of entries, since the compiler is in each key, so nothing could be kept for ever. The cache is
trimmed as Go's build cache is: an entry that was not used for five days is removed, and a trim runs at most once a day.

- **Use** is the entry file's modification time. Reading an entry whose time is more than an hour old sets it to now, so a build writes to
  the disk for an entry once an hour at most and a trim sees a use to within the hour.
- **When.** A `build` or a `run` that used the cache trims it after the executable is made and its diagnostics are printed, if the cache
  was not trimmed in the last day, which the file `trim.txt` in the directory records (the time, in seconds since 1970; one that is
  garbled or still to come counts as never). A `run` trims while the program runs and waits for the trim when the program ends, a `build`
  at its end, so producing an executable never waits for a trim. `check`, `interp` and `test` do not touch the cache.
- **Safety.** A trim only removes files, and writes the record. A compiler that reads an entry that goes meanwhile has a miss; one that
  writes renames a whole file over a name that may or may not still be there; two trims at once remove the same files. A file that is open
  where the system will not remove it stays for the next trim. Only the cache's own files are touched: an entry (a file of 62 hex digits in
  a directory of two), and a `tmp-*` file a day old, which a compiler that stopped left; anything else in the directory, and the directory
  itself, stay.

| Command | What it does |
|---|---|
| `minimaxon cache` | says where the cache is, how many entries it holds and how big they are, and when it was last trimmed |
| `minimaxon cache trim` | trims now, whenever it was last trimmed, and says how much it removed |
| `minimaxon cache clean` | removes every entry and the record of the last trim |

The commands work on the directory `MINIMAXON_CACHE` names whichever compiler made the entries, and say so when the cache is off.

### Reading the cache

`--timings` prints, above the `modules` line, `cache: N hits, M misses, K checks skipped, X MB read, Y MB written` and the time the lookup
took (summed over the workers). The lookup, which makes the summaries and the keys and reads and decodes every entry, runs on all `--jobs`
before the modules phase, and the writes are part of each module's generate. A compile is the same bytes for any `--jobs`, and a cold, a
warm and a partly warm build make the same executable and the same `--emit-ir`. `driver.CacheStats` also says, for each module, whether it
was a hit and whether its check ran, and its summary, which the tests read.

### What comes next

A hit that a miss reaches and that shows its check results is still checked, to make the results the miss's generate reads. They could be
kept in the cache beside the unit and read instead, if checking a generic's module for each edit that reaches it is too much.

A language server needs more of the same: to keep the front's products of a module that did not change (its tree, resolution and typing
tables are indexed by `NodeID`, which depends only on the module's own text, and only the `ModuleID`s in them need renumbering when
modules are added or removed), to cache a failing module's diagnostics under the same key, and to keep decoded units in memory rather
than read them again. The export summary is what tells it whether a module's dependents have to be looked at again.

## The IR

The checker works on the AST, which mirrors what the author wrote. Everything after it works on the IR in
`internal/ir`, which is what a later pass such as reference counting or an optimization changes.

- A function is a list of basic blocks, each a run of instructions ending in one terminator (`jump`, `branch`,
  `switch`, `ret`, `trap`, `throw`). The instructions of every block are one slice (`IrFunction.Instrs`), block after block,
  and a block is the range of it that it holds (`Blocks[b].First`, `.End`) with its terminator beside the range.
- Instructions read and write virtual registers (`%3`). Locals and temporaries live in numbered slots (`s0`).
  It is not SSA.
- Every slot and register is `scalar` or `managed`, so a pass can find every reference to a heap object.
- Checks are explicit, and only the ones the range analysis could not rule out are there (see "Checked arithmetic"):
  a range check, a divisor that may be 0 and a shift count that may be negative are each a `guard` with its bound, and a
  `bin` or a `neg` whose result may not fit the word is checked. Each of them names the block it goes to when the check
  fails, `else b4`, which is the only way an instruction leaves its block before its end: there the failure drops what it
  leaves and goes to the handler of its `try`, or returns the error with `throw`. A function that throws returns its
  error beside its value, 0 when it returned and the error's case plus 1 when it failed (`rdx` in the executable), and
  a `call` of one names the register that receives it (`error %7`). An allocation is `alloc`.
- An instruction is one fixed-size record (`ir.Instr`, 32 bytes): an opcode, three registers, a small index and an
  immediate, whose meaning the opcode says (the IR table, `internal/irgen/table.go`, has it, and the view of each opcode
  such as `ir.BinInstr` reads the fields by their operands' names), and a terminator is a record of its
  own (`ir.Term`). What does not fit a record (a type, a callee, a string, the arguments of a call, the cases of a
  switch) is in the function's pools, which an instruction indexes; a block, a register, a slot and a case are indexes
  too. The opcodes are a closed set, and a pass that handles them all is an `ir.InstrVisitor` (and `ir.TermVisitor`),
  so an opcode added to the table fails the build there; each dispatch ends in a panic for one it does not know.
  A record per instruction, instead of a struct behind an interface, is what lets a function's IR be built in buffers
  that the next function builds over (below): an interface value is a heap object for each instruction, which is
  allocation the compiler cannot reuse, and with the collector off every allocated byte is a page the operating
  system commits. The record ports to Maxon as a struct with an enum, and `Rt`, the runtime's symbols, as an enum.
- The IR of a function lives in an `ir.Arena`. A worker lowers each function with one `ir.Builder`, which collects
  the instructions in the order they are emitted, whichever block they are for, and `Finish` lays them out block
  after block (a block nothing reaches is dropped and the others are numbered from 0), copies the pools, and gives
  the function as views of the arena. A worker resets its arena before each module, so the IR is valid until the next
  module and a worker allocates nothing for lowering once its buffers have grown to its largest module. The IR that
  has to outlive its module, the interpreter's, is lowered in an arena that is never reset; `--emit-ir` prints a
  module's IR before the arena is reused.

Codegen is deliberately naive: every register and slot has its own frame slot, so each instruction loads its
operands and stores its result. That makes executables about twice the size of keeping values in registers;
register allocation is the next step there. A frame larger than a page touches each page as it grows, as Windows
requires. Calls use an internal convention: arguments are pushed left to right and the caller pops them. The
Win64 ABI applies only inside the runtime routines that call kernel32.

Records and union values are heap boxes of 8-byte words (a union's first word is its case index), allocated
with `HeapAlloc` on the process heap and freed with `HeapFree` where their owner dies (see "Drops"); a box is zeroed when
it is allocated. A string is a pointer to `[length][UTF-8 bytes]`: a literal lives in the executable's data section and is
never freed, any other string is a box on the heap. An enum value is its case index.

### The primitives in the IR

An `Array`, a `Map`, a `List` and a `Pool` (see "Primitives") are heap boxes that only runtime entries touch, so the
IR holds no layout of them: an operation is a `runtime` call of a named entry (`internal/builtingen/table.go`), and the
interpreter and the backend each implement the entries. The element is always visible in the call:

- An entry that takes an element **moves it in** (`rt.array.insert`, `rt.map.insert`, `rt.list.pushBack`,
  `rt.pool.insert`), one that returns an element **moves it out** (`rt.array.remove`, `rt.list.popFront`,
  `rt.pool.remove`), and `rt.<primitive>.elem` returns the **address** of an element: a borrow, valid until the next
  call that changes the primitive. A subscript is that call and a `loadat` or a `storeat`, and a `mutates` argument
  `a[i]` is the address itself. A value that changes the primitive is computed before the address is taken, which
  is what the ownership pass's exclusivity rules guarantee at the source.
- An array whose element is an integer that fits in a byte (`ByteArray`, `Array with Byte`) packs one byte to an
  element, so it calls the `rt.bytes.*` entries instead of `rt.array.*`: `rt.bytes.get` and `rt.bytes.set` in place of
  the address of an element, and its own `insert`, `remove`, `append` (a copy of the bytes), `appendStr` and
  `toString`. `ast.BuiltinTy.PacksBytes` decides it and `arrayBox` (`internal/ir/arrays.go`) is the one place lowering
  asks, so every array operation goes through it. A byte has no word address: a `mutates` argument `a[i]` is a slot
  that holds the byte, which is stored back into the element after the call returns, on its failure path too.
- An entry that is given an index, a key or a handle that names nothing traps. The queries that cannot miss
  (`rt.<primitive>.len`, `rt.map.has`, `rt.pool.contains`, `rt.list.contains`) are what lowering tests before it
  calls an entry that would trap, failing with the operation's error when the test fails (see "Primitives").
- A handle is a word, never 0. An entry that returns one returns 0 for none, where the operation fails.
- **The OS entries** (`rt.os.*`) take and give Strings and arrays, never a record or a union: lowering takes the String
  out of a `FilePath` or an `Executable` and makes the `FilePath`s and the `CollectedOutput` from what the entry gives,
  so no entry knows the layout of a record. An entry that can fail takes last the address of a word, a slot of the
  caller's, which it sets to the case plus 1 of the operation's error or to 0, and lowering branches on it as on the
  error of a throwing call. The runtime tells a failure from the Win32 error under it (`ir.Win32Failures`: not found,
  access denied, busy, already exists, or another), and each operation names the case of its own error that each
  failure is (`ir.FailureCases`); the interpreter reads the same Win32 error out of Go's error.
- `clone()` is one instruction, `%r = copy %s T` (see "Drops"), whatever `T` is: a record, a union, a string or a
  primitive. What it does to each part is code that depends on the type, so it is a routine of its own for each type that
  the backend makes (below) and the interpreter does by the shape of the value; a copy of an array copies every element by
  its own type, and a copy of a pool or a list keeps its handles valid.
- A `Handle with T` is `scalar`; everything else here is `managed`.

**Native.** Only the array has code in the backend. It is a box of three words, the length, the capacity and the address
of its elements, which are 8-byte words; `rt.array.insert` grows it by doubling and shifts what follows, and frees the
buffer it leaves (the elements moved to the new one, so only the buffer goes), and `rt.array.remove` hands the element
it removes to the caller, who owns it. The entries are written in x64 in `internal/backend/arrays.go`, a unit of their
own that the driver links only into a program that calls one. A byte array has the same three words, with a buffer of
one byte to an element, in `internal/backend/bytes.go`, its own unit too; its copy and drop routines copy and free the
buffer whole. A map, a list and a pool are lowered all the same, which
is what the interpreter runs, but the driver refuses a function that needs one before it generates code: `E9900`, at the
first use in each function. A function that only drops, copies or holds a value that contains one (a `consumes`
parameter of type `Map with (K, V)`, or a record with a pool in it) is refused too, at the function, since there is no routine
to free it.

### Frozen values, cowns and behaviours in the IR

A frozen value and a cown are boxes with a count of owners, so they are the one kind of value that more than one place
owns (`internal/builtingen/table.go`):

| Entry | Does |
|---|---|
| `rt.frozen.freeze(v)` | moves `v` into a new box with a count of 1 |
| `rt.frozen.get(f)` | lends what `f` holds, valid while `f` is held: how every read through a frozen value begins |
| `rt.share(f)` | the same box, with the count raised by 1: another owner, which is `share` and the `clone()` of a frozen value or a cown |
| `rt.cown.new(v)` | moves `v` into a new cown with a count of 1; with `routine d` the cown also stores the destructor `d`, which ends `v` when the count reaches 0 instead of a drop |

Dropping an owner (`drop`, `dropslot`, `dropat` of a `frozen with T` or `cown with T`) lowers the count by 1, and the box is
freed with what it holds when the count reaches 0. A scalar can be held (`cown with Count`, `frozen with T` with `T` a scalar in
an instance of a generic).

A `when` is one instruction, `when body cowns [%a, %b] captures [%c]`. The cowns are read, not moved, and each gains a
count of its own for as long as the behaviour is waiting or running. Each capture is a value that moves in: a managed
one was moved out of its owner by the instruction before, so it is in flight. **The body is a function of its own**, in
`IrFunction.Behaviours` of the function around it and printed after it: its parameters are the access to each cown's
value (an address, as a `mutates` parameter has), then the captures, which it owns as a `consumes` parameter does and
drops at its end. It returns nothing, and nothing waits for it. Its name is the enclosing function's with `$when` and a
number, in source order (`transfer$when0`, `swap[String]$when0` in an instance, `outer$when0$when0` for one inside
another), so a generic function that holds a behaviour has one for each instance, lowered with the instance's type
arguments.

**Native.** None of it has native code: green threads, a scheduler and atomic counts are not built (see
[docs/regions.md](docs/regions.md)). The driver refuses a function that freezes, shares, thaws, makes a cown or schedules a
behaviour, or reads through a frozen value, with `E9900` like a map, a list or a pool. `freeze` alone could be native, since
nothing is counted or freed there yet, but a program that freezes and nothing else cannot show anything about the rules,
and refusing every one keeps the backend free of a half of the feature.

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

`T` is the type with an instance's type arguments applied, so codegen can call the drop routine of that type. Dropping a
value drops what it owns, deeply by type: the fields of a record, the payload of a union, the elements of a primitive. A
borrow is never dropped: a `let` or `mutates` parameter, a projection, and a `match` binding of a payload, which is
stored with `store s3, %r borrow`. A place that holds nothing, because it was moved from or never assigned, drops as a
no-op, so one drop at the end of a scope serves every path to it and no analysis of paths is needed. A function that
drops a value that holds a map, a list, a pool, a frozen value or a cown is refused natively like one that makes it.

-  **One place decides what moves.** The ownership pass marks each expression whose value changes owner
  (`ast.Typing.Consumed`): a binding, an assignment, a `consumes` argument, a return, a field or a payload, a branch of
  a conditional or a match. Lowering reads that mark and nothing else. A consumed place is moved out of, and the pass
  refuses every consumed place that is neither a whole local nor a field of one; a consumed fresh value goes to its new owner; a fresh value
  that is not consumed is a temporary. The values a declaration holds outside any function, a constant and a field's
  default, are checked by the same pass for the same reason.
- **Scopes.** A block owns the locals declared in it, and each statement, and each branch of a conditional or a match arm,
  owns the temporaries it makes: they are dropped when it ends, newest first, and a jump that leaves several scopes drops
  all of them. A condition drops its temporaries before it branches.
- **Sink parameters** are copied into a slot at entry, which owns them from there, so the callee can move on or drop
  them like a local. A `consumes _` parameter is dropped too.
- **A string is owned like any value.** A literal and the name of a case are immortal and never freed (a drop of one is a
  no-op, found by its address natively), but an interpolation makes a new string, and so does `clone()` of a string that is
  not a literal; the copy of a literal is the literal.

### Native freeing

The backend turns the drops into code, and the IR needs nothing more than what it says: an empty place is 0, a drop of 0
does nothing.

- **A move empties its source.** `move` and `moveat` load the value and store 0 where it was, and every frame starts with
  its managed slots zeroed (one store each, or `rep stosq` over the slots of a frame with more than six), so a slot that
  was moved from, or never assigned, or whose `dropslot` has run, holds 0. That includes a slot that a loop body
  declares: its `dropslot` at the end of the body leaves it empty for the next pass. `dropslot` and `dropat` leave the
  place 0 too. A register is always written before it is dropped on every path, so a frame does not zero registers; the
  interpreter reports a drop of a register that holds nothing, which is how a hole in that would show.
- **A routine for each type.** `drop` of a type `T` calls the drop routine of `T`, and `copy` its copy routine; each is
  generated once for the program, for each concrete type (after monomorphization) that the code drops or copies and each
  type those reach, in the "routines" phase of the pipeline; the link drops a routine that only unreachable code calls. A drop routine takes the value (0 does nothing) and drops
  each managed word of it by the routine of its type, then frees the box: a record drops its managed fields, a union
  switches on its case index and drops the managed words of that payload, an array drops each element, then frees the
  element buffer and the header, a string frees its buffer unless it is a literal. A copy routine allocates the box the way
  lowering sizes it, and copies each word, a managed one by the copy routine of its type. A routine calls the routines of
  its parts and nothing is inlined, so a type that contains itself through an array (a tree) is a routine that calls itself.
  A drop of a record or a union with nothing managed in it is `rt.free` itself, with no routine; a scalar, an enum and a
  `Handle` have none and are never dropped. The routines are named by their type and ordered by where the modules first
  ask for them, never by a map, so the executable is the same for any `--jobs`.
- **Literals are immortal, found by address.** Every string literal and the name of every case is in the executable's
  data section, and a string on the heap never is, so the drop of a string compares its address with the bounds of that
  section (`g.imageDataStart` and `g.imageDataEnd`, which the linker defines) and does nothing for one inside. The copy of a
  literal is the literal, as in the interpreter. The alternative, a flag in the string's header, would cost a word in
  every string and a mask on every read of its length; a range check costs nothing until a string is dropped, and any
  compiler that puts constants in an image section can do the same.
- **Free** is `HeapFree` on the process heap, behind `rt.free`, which also skips 0 since `HeapFree` does not accept it.
- **Counting allocations.** `--count-allocations` builds the runtime with a count of the heap boxes that are allocated
  and not freed (`g.live`): `rt.alloc` adds 1 and `rt.free` subtracts 1. When `main` returns with the count not 0, the
  executable writes `leaked allocations: N` to standard error and exits with 99, in place of `main`'s own exit code. It is a
  flag, so an executable built without it has no counter and no check; `minimaxon test` and the program generator always
  use it, so every native run of the corpus ends with no box live, a literal not being one. A panic and a trap exit at once
  and skip the check, because they end the program with values still held; a run test that traps still shows no
  double free or crash, but not that it freed everything. `self/`'s runtime counts in every build and the flag changes
  only its entry stub (see "The runtime").
- **Depth.** A routine calls itself, or the routine of a part, once for each level of nesting, so dropping or copying a
  value nested about 300,000 levels deep (a chain of that many nodes, each holding the next in an array) overflows the 16 MB
  stack. A routine keeps only the words it uses in its frame, to push that out.

### The interpreter

`internal/interp` runs the IR of a whole program, with the primitives written in Go (an array, a map, a list and a
pool, each with the generational handles that the README's "Primitives" describes; a frozen value and a cown, with their
counts) and a simulated scheduler of behaviours, and tracks the heap as the memory model describes it. It is for tests and
development: it is never part of a compiled program, and `driver.Prepare` reaches it only from `minimaxon interp` and the
test runner. It has no types; it works from the shape of the values and from the drops the IR says.

- **What it records.** Every heap value's owner (the place that holds it) and whether it was freed; every address with
  the version of its structure; and the borrows that a call is waiting to use. A place has one owner, so storing a
  value that is already owned is an error, and so is overwriting a value that the place owns. A frozen value and a cown
  have many owners: it records the places that hold one, and a count.
- **Violations**, each reported with the function, the block, the instruction and what happened: a use after free, a
  double free, a leak (a value still live when the program ends, or a slot that owns a value when its function
  returns), use of a moved value (a slot that was moved out of or never assigned, read again), a value with two owners
  or one that is freed or returned while another owner holds it, a stale borrow (an element's address used after its
  structure changed), exclusivity (two arguments that reach one place where one of them can be changed, or a change
  to a place that a borrow a call is still waiting to use reaches), and three that only threads can break (below). A
  violation in a program the checker accepted is a hole in the checker or in lowering. It stops the run; a leak is found
  at the end.
- **Threads.** `main` is the first thread; a `when` makes a behaviour that is queued on each cown it names, in the
  order behaviours were scheduled, and that becomes a thread once it is first in all of those queues. A thread is a
  goroutine that runs only while the scheduler has given it the turn, so exactly one thread executes at any moment and a
  run is deterministic for a **seed**: the scheduler picks the next thread at random among every thread that has
  started and every behaviour that can start, and a thread keeps the turn for a random number of instructions (up to 2,
  8 or 64, chosen at random each time), so the schedules include interleavings at instruction granularity and long runs
  of one thread. What is simulated is the interleaving, which is where a data race would show. The program ends when
  `main` has returned and no behaviour is left, with `main`'s exit code; a trap in any thread ends it at once.
- **Who owns what.** A value belongs to the thread whose frame holds it, to the cown whose value it is part of, or to a
  frozen value, found by following owners up to a slot, the value of a cown or the contents of a frozen value; a value
  that nothing owns yet belongs to whoever holds it. Touching a value that belongs to another thread, or to a cown
  while the thread has no behaviour that holds the cown, is a **data race**; changing anything that is part of a frozen
  value is a **change to a frozen value**; and a frozen value or a cown whose count is wrong is a **reference count**
  violation (freed while a place or a waiting behaviour still holds it, or lowered below 0). One that is still live at
  the end is a leak, as any value is. Handing a behaviour a value that its scheduler still owns is a data race at the
  `when`. None of this is tracked until the first `freeze`, `cown` or `when` runs, so a program without them pays a
  branch.
- **A cown's destructor.** A count that reaches 0 can happen deep inside freeing a record or an array that holds the
  reference, where a call cannot be made. The interpreter takes the value out of the cown, frees the cown, and appends
  (destructor, value) to a list; once the instruction that dropped has finished, the thread that ran it calls the
  destructors in list order, which is the order of the frees (`Result.Frees`), so it is deterministic.
- **Traps** behave as the executable does: `rt.panic`, an index out of range, a stale handle, a collection ended with `dropEmpty` while it holds an element and a behaviour that names one cown twice write to
  standard error and exit with 1; the same standard output and the same exit code. A trap is not a violation.
- **How tests use it.** `minimaxon test` runs every program under it, under 20 schedules when the program schedules
  behaviours, and requires the same stdout and exit code as the directives and no violation. `internal/interp` tests it
  on IR that is written to break each rule (and to schedule behaviours: their order, their interleaving, the counts), on
  the order in which values are freed, and on programs from a generator (`generator_*_test.go`): random well-formed
  programs over records, unions, strings, the four conventions, calls with overlapping and disjoint arguments, match
  bindings, loops, generics with bounds, arrays, maps, lists and pools with handles, a borrowed place held while a later
  operand of the same expression changes or consumes what it lies in (a string comparison, an interpolation whose first two parts are a place and a call, a subscript whose base is a
  field or an element and whose index, key or handle is such a call), and frozen values, cowns and behaviours. The
  checker refuses most of them, since the generator leans on the edges of the rules: about two
  programs in five are careless and break rules throughout, and two in five of the rest break exactly one rule of
  sharing, once (a behaviour that takes a borrow, a variable used after a behaviour took it, a frozen value changed or
  taken apart, a frozen value's method that changes it, a cown read outside a behaviour, a borrowed place held across
  a later operand), so that a rule the checker
  lacked would be all that stands between that program and the interpreter. Every program it accepts must run clean, under several schedules when it
  schedules a behaviour, and one that does not is a hole. `go test ./internal/interp` runs 300 programs, each under 3
  schedules. `MINIMAXON_GEN_PROGRAMS=N`, `MINIMAXON_GEN_SEED=S`, `MINIMAXON_GEN_SEEDS=K` (schedules per program) and
  `MINIMAXON_GEN_KEEP=dir` (save a failing program) set a long run, and `MINIMAXON_GEN_NATIVE=1` also builds and runs
  each accepted program that has an executable, with its allocations counted, and compares it with the interpreter's: the
  same output and exit code, and no box left live.
  `MINIMAXON_GEN_DUMP=file` writes the program of a seed, and `MINIMAXON_GEN_EXPLAIN=1` lists why programs were refused.

## The runtime

`self/`'s runtime is Maxon source: the `runtime/` directory at the repository root, beside `self/`, one module that is
compiled with every program and cached, as Go's runtime package is, so a change to it is tested by the next compile with
no rebuild of the compiler. Only what is derived from the program's types stays compiler-emitted: the drop and copy
routines and the entry stub (`self/backend/routines.maxon`, `entry.maxon`). The Go compiler keeps its own emitted
runtime (`internal/backend/runtime.go`, `arrays.go`, `bytes.go`, `os.go`) until Go work ends and never compiles
`runtime/`; it only checks it. `runtime/README.md` lists the files and the rules a runtime file follows.

**Where it is found.** As maxon finds its library tiers: walking up from the compiler's executable, the first directory
that holds a `runtime/` directory (`project.runtimeDir`), so a compiler built in a checkout uses the checkout's and an
installed one the one installed beside it. A project whose root is that directory is the runtime itself
(`ast.Module.IsRuntime`): `minimaxon check runtime` checks it as a library, with no `main`, and `build` and `run` refuse
it as a program with no `main` (`E3001`). The runtime's link path is `runtime` wherever it is compiled, so its functions
are `runtime::alloc` and the like; `runtime` is a reserved top-level module name (`E3182`), so no module of a program
takes it.

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
its message), which `check.callFunction` raises from the rows' runtime-only flag (`ast.Intrinsic.RuntimeOnly`, and
`intrinsicRuntimeOnly` in `self/ast`). They lower to four IR operations that only the runtime's code has: `loadraw.W`
and `storeraw.W`, W the width in bits, at the address plus the offset; `globaladdr` of an `Rt` global; and `import`,
which names the DLL function among the function's strings. The wrapping arithmetic is an unchecked `bin`, `addressOf`
the `addr` of the local's slot, and `stringAddress` and `stringAt` the word itself. `self/backend` writes them
(`visitLoadRaw`, `visitStoreRaw`, `visitGlobalAddr`, and `visitCallImport`, which aligns the stack itself, since a Maxon
function's frame is not aligned, and keeps `rsp` in `rbx`, which the callee preserves, saving `rbx` first). The Go
compiler's lowering, codegen and interpreter refuse each with a `diag.Internal` error that says the Go compiler does not
compile the runtime (`ir.RuntimeSourceOnly`).

**From an entry to a function.** The runtime entries the compiler calls are the rows of the table's `entries`
(`ir.Rt`), and `runtimeFunctions` names the function of `runtime/` that defines each one with its signature
(`alloc(bytes Count) Count`, `write(handle Count, text String)`, ...). From it the generator writes `ir.RtFunction` for
Go, and `self/ir`'s `rtSymbol`, which gives a mapped entry its function's link name (`runtime::alloc`) and an entry that
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
a box is live, and gives the code back otherwise. Behaviour, exit codes and messages are the Go runtime's.

**What runtime source may do.** It is ordinary Maxon apart from the intrinsics: integers, `bool`, loops, `panic` with a
literal (which calls the runtime's own `panicWith`, which allocates nothing), and `String` values, borrowed as a
parameter or owned once `stringAt` makes one. A dropped `String` goes through the compiler-emitted drop routine, which
calls `free`, so no free comes back into the code that started it. It uses none of the primitives (`Array`, `Map`, ...),
which the runtime implements, and throws nothing. Runtime collections are not generic: an array entry takes a
descriptor of its element's size and layout, and only a thin typed wrapper is per type.

**Layouts.** `runtime/layout_gen.maxon` holds, as named constants, the layouts that the compiler's code and the runtime
share: the word, the string box (its length at 0, its bytes at 8), the array and byte array box (its length, its
capacity and the address of its buffer, with a buffer of 8-byte words or of bytes), the first capacities and the empty
box. A generator (`internal/abigen`, plain Go, see "Generated code") writes it, `self/backend`'s and `self/ir`'s layout
constants and the Go compiler's from one table, so the runtime and codegen cannot disagree. The array entries
(`runtime/arrays.maxon`, `bytes.maxon`) are thin functions over one set of routines that take the size of an element
as a word (`collectionReserve`, `collectionExtend`, `collectionOpenGap`, `collectionCloseGap`), as the plan's
"collections over layout descriptors" says.

**Compiled once.** The runtime reads no build option, so one compiled runtime serves every program a compiler builds.
When the unit cache lands, the runtime's units are kept under a key of the compiler's own hash and the hash of every file
of `runtime/` (its name and contents), and a program's link takes them from there; an edit to `runtime/` changes the
key, so the next compile rebuilds the runtime and nothing else.

## The subset

- `function`, `type` (fields, defaults, methods, `static function`), `enum` (integer raw values), `union`
  (payloads, methods), `interface`, `typealias X = int(a to|upto b)`, `typealias X = Box with Count`, a top-level
  `let` constant. A function, a type and a union can be generic (see "Generics"). A `type` or a `union` can say
  `is noncopyable` right after its name (`type Socket is noncopyable`; never an `enum`), which takes `Copyable` from it,
  and a method of either can be a destructor (`function commit(destructs self)`), which takes both (see "Capabilities and
  destructors").
- `let`, `var` (with a value, or `var x as T` without one), assignment to a variable, a field or an element,
  `_ = call()`, `if`/`else if`/`else`, `while`, `for i in a to|upto b`, `match` as a statement or expression
  (case, literal, range and string patterns, `or` alternatives, `default`, `panic` arms), `break`/`continue`
  with labels, `return`, `when` (a behaviour, see "Immutable values, cowns and behaviours"), and a `try` block with or
  without a handler (see "Checked arithmetic").
- Code after a statement that ends its block (`return`, a `panic`, `break`, `continue`, or an `if`/`else` or `match`
  whose every branch returns or panics) is allowed and type-checked, but never compiled (`ast.Live`): its calls make no
  instance of a generic. A `break` or `continue` inside a branch does not end the code after the `if` or `match`,
  since a `break` can leave a `match`.
- Integer, bool and string literals, interpolation, arithmetic and bitwise operators, `shl`/`shr`,
  comparisons, `and`/`or`/`xor`/`not`, `x if c else y`, `as` casts, error enums, `throw`, `throws E`, `try e`,
  `try e otherwise ...` and `try` blocks (see "Errors of the program"), calls with Maxon's
  positional-then-labelled arguments (a label, and a parameter name, may be a keyword: `File.rename(from, to: dest)`), parameter conventions (`mutates`, `consumes`, `sets`), `x.clone()`,
  `.name`/`.ordinal`/`.rawValue`, subscripts `a[i]`, `match consumes x`.
- Built in: `print`, `printError`, `panic` (which takes a string literal or an interpolated string), `freeze`, `share`, `thaw`, `cown` (also `cown(x, dropWith: d)`), `exchange`, the OS functions of maxon's stdlib (see "The OS"), the aliases `ExitCode`, `Count`,
  `FullRangeInt`, `Byte`, `ByteArray` (`Array with Byte`), the `String` methods `byteLength()`, `byteAt(index)`, `sliceBytes(start, endPos: end)` and `toByteArray()` and the static `String.from(bytes)`, the enums `ArithmeticError`, `ArrayError` and `MapError`, the interface `Error`, and the generic types `Array`, `Map`, `List`, `Pool`, `Handle` (see "Primitives"),
  `frozen` and `cown` (see "Immutable values, cowns and behaviours"). There is no other stdlib, and no `Option`: a
  program that wants one declares a generic union (`union Option uses T`).

### Integers

Every typealias is its own type. An integer literal is an untyped constant: it fits any alias whose range
holds it, and constant expressions fold at compile time. A value whose range is known but has no alias,
such as a `for` variable or an intermediate result (below), fits any alias whose range contains it, and goes into
one that does not only through `as`, which checks at compile time for a constant and is otherwise an operation that can
fail. Mixing two different aliases is `E3005`. A `var` and a constant name their alias: `var x = a + b` and
`let Total = a + b` are errors until the value is cast, `as <Alias>`.

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
(`try t as Score`). Comparisons take any two integers one word holds, an alias against an anonymous range or a constant
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

**Guards.** The range analysis (`internal/check/ranges.go`) proves most operations safe, and those need no `try` and
compile to no check at all: lowering emits only the checks it leaves (`ast.Fails`). It is one walk over each function,
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
(`internal/check/ownership.go`, with places in `paths.go`). A value of a record, union, `String` or
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
  A module finds its instances itself, from its own functions and through the generics it reaches (`instances` in `ir`),
  so a module that needs an instance another one needs lowers and emits it too, and the link keeps the first unit's copy.
  The order of a module's instances is the order it found them in, so it does not depend on `--jobs`, and neither does
  which unit keeps an instance. A compile that stops short of generate (`check`) still has each module find its instances,
  which is where `E3906` is raised.
- **Interning.** `Box with Count` is one type wherever it is written: each generic declaration interns its instances,
  keyed by their arguments, behind a lock, which is the only write that parallel phases share. Every module that needs an
  instance gets the same declaration, and the order in which modules interned it is never visible in any output. An instance holds no
  members of its own; it reads the declaration's fields, cases and methods and substitutes its arguments.
- **Limits.** A generic type may not contain itself, even with a larger type argument each time (`E4014`), and a
  generic function may not call itself with ever larger type arguments (`E3906`, past a nesting depth of 16). Each module that
  reaches such a function finds the runaway for itself, and the program gets one error: the one of the first module in module
  order that has it, at the first call in its walk that asks for the too deep instance, which is the same for any `--jobs`. That is
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
`abort(reason: r)`); moving it whole anywhere else is `E3920`. The query is `internal/check/capabilities.go`,
one answer per type and capability, remembered per checker, naming the part that removes the capability so the message
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
| `ArrayError`, `MapError` | maxon's error enums: `indexOutOfBounds` and `emptySlot`; `keyNotFound` and `keyAlreadyExists` |
| `ByteArray` | `typealias ByteArray = Array with Byte`, built in: the same type, with every operation of `Array`; `String.toByteArray()` is a new one of a string's UTF-8 bytes, and `String.from(bytes)` a string of a copy of them |

An array of an integer type whose range fits in a `Byte` holds one byte to an element, natively and under the
interpreter, and any other array a word; the program cannot tell, except by how much memory it takes.

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
  each other by handle (see `tests/` and `internal/check/primitives_test.go`).
- **Map keys** are compared and copied as a word or as text: an integer alias, a `bool`, an enum, a `String` or a
  `Handle`. Any other key, a record or a union or a type parameter, is `E3908`. A generic function can therefore take
  a `Map with (String, V)` with a generic `V`, but not a `Map with (K, V)` with a generic `K`; a bound for keys would lift that.
  `upsert` replaces the value of a key that is there, which is dropped.
- **Generic code** over a primitive is checked once, as in "Generics": a value of type `T` held by an `Array with T` is
  owned and movable, so a generic container gives an element out with `remove` or `pop`, or `.clone()`.
- **Iteration** is `for i in 0 upto a.count()` and a subscript. There is no `for x in a`, and a map has no way to
  walk its keys yet.
- **Running them.** An array runs natively: heap-allocated, bounds-checked, and freed with what it holds where its owner
  dies (see "The primitives in the IR" and "Native freeing"). A map, a list and a pool are checked and lowered, and the interpreter runs them (see "The interpreter"), so a
  program that uses one is refused when it is compiled to an executable (`E9900`) and still passes `check`; the test
  runner runs such a program under the interpreter alone.
- **Limits.** A primitive implements no interface (`E3017`). A record
  that contains itself through an array (`var kids as Array with Tree` in `Tree`) is fine, natively too: its drop and
  its copy are routines that call themselves.

### The OS

The OS functions are maxon's stdlib, names, labels, results and errors, declared as static functions of built-in
records that have no fields (`internal/builtingen/table.go`), so a program calls them as maxon's does:

| Call | Gives | Throws |
|---|---|---|
| `CommandLine.args()` | `Array with String`, the program's own name first | — |
| `Process.environmentVariable(name)` | `String` | `ProcessIntrospectionError.variableUnset` |
| `FilePath.from(path)` | `FilePath`, with `/` made `\` | `FilePathError.invalidCharacter` for a control character or one of `<>"\|?*` |
| `path.toString()` | `String` | — |
| `File.readText(path)` | `String` | `FileReadError` |
| `File.writeText(path, content: s)`, `File.writeBinary(path, content: bytes)` | — (a file that is there is replaced) | `FileWriteError` |
| `File.rename(from, to: dest)` | — (what is at `dest` is replaced) | `FileRenameError` |
| `File.delete(path)` | — (a directory is refused, `accessDenied`) | `FileDeleteError` |
| `Directory.isDirectory(path)` | `bool` | — |
| `Directory.create(path)` | `bool`, whether the directory is there after it; the missing parents are made too | — |
| `Directory.list(path)` | `Array with FilePath`, each the path joined to an entry's name (no `.` or `..`) | `DirectoryListError` |
| `Subprocess.run(executable, arguments: args)` | `CollectedOutput`: `exitCode()`, `succeeded()`, `stdout`, `stderr` | `SubprocessError` |

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
  after doubling the backslashes before it (Go's `syscall.EscapeArg`). The child's standard output and error are pipes,
  which the run drains in turn without waiting on either, so a child that fills one cannot block; its standard input is
  the program's. A name that is not found is `executableNotFound`.
- **Natively**, the Go compiler's entries are x64 over the `A` Win32 calls (`internal/backend/os.go`), and
  `CommandLine.args()` splits the command line simply: space or tab outside quotes, and a token that is quoted whole.
  `self/`'s runtime (`runtime/os.maxon`, `subprocess.maxon`) calls the `W` functions instead, converting the program's
  UTF-8 to UTF-16 and back (`text.maxon`; a byte that is not UTF-8 and a lone surrogate become U+FFFD), and splits the
  command line by the C runtime's rules (`commandline.maxon`): 2n backslashes before a quote are n, 2n+1 are n and a
  literal quote, a doubled quote inside quotes is a literal one, and the program's own name is split by the same rules.
  The interpreter runs the entries through Go's `os` and `os/exec` (`internal/interp/os.go`).

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
  place's path gains a step into the contents (`paths.go`), and every rule about places, borrows and exclusivity applies
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
  `otherwise throws ...` form; there is no `if try`, no block handler `otherwise (e) 'label'` on a `try` expression and
  no `default throws` in a match. Maxon's `E3168` (a `throws` that nothing throws) and `E3083` (a `try` block with
  nothing that throws) are not raised, so a sharper range analysis never breaks a program. A `try` covers every
  operation of its expression, not one call, and a condition that rules a failure out is a proof the compiler checks
  (see "Checked arithmetic").
- No alias subtyping (an alias implementing another), floats, characters, tuples, closures, async, services, region capabilities (`isolated`, `immutable`, `modify`), static fields, default
  parameters, format specifiers, or `#if`. The memory model of [docs/regions.md](docs/regions.md) is implemented: owned
  values, the primitives, frozen values, cowns and behaviours (see "Ownership", "Primitives" and
  "Immutable values, cowns and behaviours"), with an interpreter that runs all of it and a native backend that runs
  only the array. `freeze` is a function here, `when` is a statement that takes cowns and gives names to their values,
  and a frozen value is a type, `frozen with T`, where Maxon recovers immutability from facts about a value's history.
  The grammar has none of the rest, so each is a syntax error.
- `Array`, `Map` and `List` have the names, the signatures and the errors of maxon's stdlib, but they are built-in
  types rather than stdlib code, and keep these differences: a subscript is a place (`a[i].f = x`, `mutates a[i]`) that
  traps on a miss, where only `get` and `set` throw, as maxon's do; `Pool`, `Handle` and the list's handle API (`prepend`
  and `append` return a `Handle`, `first` and `last` return one, `remove(h)`, `next`, `prev`, `moveToFront`) stand where
  maxon has pointers and indexes; `filled`, `Map.replace` and `dropEmpty` are minimaxon's own; there is no iteration
  protocol and no `slice`, `clear`, `reserve` or `sort` yet; `create(n)` refuses an enum element, whose
  zero here is its first case rather than its case of raw value 0, and takes no `zeroed:`; and `String` has only
  `byteLength`, `byteAt`, `sliceBytes`, `toByteArray` and the static `String.from(bytes)`, whose names leave `slice` and
  `count` for maxon's graphemes. There is no built-in `Option`, which maxon's stdlib has no more of either: an
  operation that can miss throws.
- `ByteArray` is a built-in typealias rather than maxon's in `stdlib/File.maxon`, and the minimaxon `Bytes` buffer is
  gone for it. Only an array of an integer type that fits in a byte packs its elements; maxon sizes an enum's and a
  narrower integer's elements by their range too. `String.from(bytes)` copies the bytes where maxon's shares the
  array's buffer, and a `String` here is its bytes, so neither direction scans them.
- The OS functions (see "The OS") have maxon's names, signatures and errors, but are a subset, built in rather than
  stdlib code: `FilePath` has only `from`, `toString` and its `path`, and `FilePath.from` does not resolve a `file://`
  URL (one is taken as a path); `File` has no `readBinary`, `createText`, `exists`, `renameNew`, `info` or `openRead`,
  and `writeText` and `writeBinary` take no `mode:`; `Directory` has no `exists`, `delete` or `removeTree`, and
  `Directory.list` lists in the order the system gives; `Process` has only `environmentVariable`; `Subprocess.run`
  takes no `workingDirectory:` or `deadline:`, and `CollectedOutput` has no `status`, `pid` or `durationMs`;
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

A change to the syntax touches many programs, so tools do the mechanical part:

- **`minimaxon rewrite --keyword=old=new <path>...`** renames a keyword. It lexes each file and changes only keyword
  tokens, so a name, a label, a comment or a string's text that reads the same is left alone, and a keyword inside an
  interpolation is renamed. Run it before editing `Maxon.g4`: once the grammar stops reserving the word, a use of it
  cannot be told from a name, and the command refuses. A file the lexer refuses, such as a test of a lexer error, is
  left alone and listed if it contains the word. In a Markdown file it renames the code: inline code spans, and fenced
  blocks tagged `maxon` or not tagged; prose, and a span or block that does not lex (IR text, a command), are left
  alone, and the first such span holding the word is named.
- **`minimaxon fmt [--check] <path>...`** is the tool for a layout change (a rule about spaces, indentation, blank
  lines or comments): change the rule in the parser or the grammar, run `minimaxon fmt tests internal docs README.md`,
  then check. It rewrites every `.maxon` file under the paths, and the fenced blocks of `.md` files that are tagged
  `maxon` or not tagged and parse as whole programs; other blocks, code spans and prose are left alone. `fmt --check`
  writes nothing and exits 1 listing the files that are not in the layout. Over a directory it skips a file that
  contains `// expect-error: E2001` or `// expect-error: E2010`: those tests are laid out wrongly on purpose, and a
  formatter would put them right. A file that does not parse is reported and left alone (over a directory, a corpus
  holds such files on purpose; a file named on the command line that does not parse is an error and exits 1). The
  unit-test sources under `internal/*/testdata`, case files included, are whole files of declarations and comments,
  so `fmt` handles them like any other.
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
  first, is listed for a hand edit.
- **`minimaxon test --update-positions`** moves each `expect-error` whose code is now reported at another line or column,
  as after a rename that changes a line's length. It never changes the code a test expects, so a test that now fails
  with another error still fails.
- **Unit tests.** `minimaxon rewrite` over `internal` renames keywords in the unit tests' `testdata/*.maxon` too. The
  only keyword spellings left in Go are the constants in `internal/interp/keywords_test.go`, which the program generator
  builds programs from, and a few assertions that name one keyword, which the Go tests find.
- **`.\check.ps1`** runs every check this repository requires, formatting and applying `go fix` first, and stops at the
  first failure; its last step is `minimaxon check runtime`, since neither test suite compiles the runtime. Arguments go
  to `minimaxon test`, so `.\check.ps1 --update-positions` works.

A refactoring is a list of `syntax.Edit`s (a byte range and its new text), applied by `syntax.ApplyEdits`, so a language
server can send the same edits to an editor. `.claude/skills/language-change` holds the checklist for a change to the
language.

## Adding a feature

A typical change touches one file per stage:

1. **Syntax:** a rule in `internal/syntax/Maxon.g4`, listed in the set it belongs to (`stmt`, `expr`, `decl`), with
   an `@ast::Node` block for the fields later phases fill in; then `go generate ./internal/syntax`. That writes the
   node, its parse function and its keywords. `go build` now names every visitor that lacks the node's `Visit`
   method, and any token conversion the grammar asks for; the steps below fill them in.
2. **Resolution:** `Visit<Node>` on `Resolver`, binding any names the node holds.
3. **Typing:** `Visit<Node>` on `Checker`, and on `termination` (`internal/ast/flow.go`) for a statement. Store what codegen needs on the
   node. Read the types and signatures of the modules the module reaches freely (its stages wait for theirs), but never
   write them: only a module's declare stage writes its own declarations.
4. **Ownership:** `Visit<Node>` on `Ownership`. An expression whose value goes to a new owner is passed through
   `consume`, which is what marks it for lowering.
5. **Lowering:** `Visit<Node>` on `Lowerer`, built from the existing instructions where it can. A value it makes that
   nothing consumes is a temporary, and a place it declares or assigns drops what it held (see "Drops"). A new
   instruction is a row of the IR table (`internal/irgen/table.go`, see "Generated code"); after `go generate
   ./internal/ir`, `go build` names its missing `Visit` method in codegen (`internal/backend`) and the interpreter.
6. **Tests:** a unit test in the package that changed, a `tests/run/` program for the behaviour (it runs natively
   and under the interpreter), and a `tests/errors/` program for each diagnostic. A new construct that the memory
   model's rules cover also goes into the program generator (`internal/interp/generator_*_test.go`).

A rule that is only a diagnostic, such as a new ownership or sharing check, usually needs steps 3 or 4 and
a test, and nothing else.
