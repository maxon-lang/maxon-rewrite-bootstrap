# minimaxon

A small Maxon compiler for trying out language changes, and the seed of the next full Maxon compiler. It is
written in Go, targets x64 Windows only, and is about 18,000 hand-written lines in eleven packages, plus the generator
that writes its parser and AST. It compiles modules in parallel and writes a
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
| `minimaxon rewrite --keyword=old=new <path>...` | rename a keyword by token in the `.maxon` files and the Markdown code under the paths (see "Changing the language") |
| `minimaxon interp <file\|dir>` | development only: run under the tracking interpreter, which runs a map, a list, a pool, a frozen value, a cown and a behaviour, and reports a violation of the memory model; exits 4 on a violation |

`--emit-ir` writes the IR of every function beside the executable, as `<out>.ir`. `--jobs=N` sets how many
modules compile at once. The default is one fewer than the processor count, and the executable is byte-identical
whatever the value. `--timings` prints each phase's wall time, allocation, garbage collections and GC pause time
to stderr. `--seed=N` picks the schedule of behaviours that `interp` runs (default 0). `--count-allocations` builds an
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
A program that compiles is run twice, as the executable and under the tracking interpreter (see "The interpreter"),
and both must give the stdout and the exit code the directives say; the interpreter must also find no violation of the
memory model, and the executable, built with `--count-allocations`, must end with no heap box live (a program that
traps skips that check). A program that uses a map, a list, a pool, a frozen value, a cown or a behaviour has no
executable yet: it fails to compile with `E9002`, and the runner then runs it under the interpreter alone. No directive is needed for
that. A program that schedules behaviours runs under 20 schedules (seeds 0 to 19), and every one must give the same
stdout and exit code and no violation, so the directives of such a program must say an output that does not depend on the
schedule: print only from behaviours that the cowns put in order, such as one chain, or behaviours on one cown.
Directives in comments say what the program must do:

| Directive | Meaning |
|---|---|
| `//> text` | one expected line of stdout, in order (none means stdout must be empty) |
| `// expect-exit: N` | expected exit code (default 0; a panic exits 1) |
| `// expect-error: E3102` | compiling must fail with this code; `E3102 at 7:5` also pins the line and column (`E9002`, a program that needs something that has no native code yet, is the one error that is not a failure in a program that expects none) |
| `// memory-limit: N` | the native run may use at most N megabytes: the runner puts the process in a job object with a memory cap, so a program that never gives memory back fails (an allocation fails and it crashes), whatever the machine has |
| `// native-only` | do not run the program under the interpreter; for a program whose point is what the executable does with memory, which the interpreter runs far too slowly |

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
| `srctest` | reads the Maxon source that unit tests keep in `testdata/`; imported only by tests | — |
| `ast` | the program model: AST nodes, keywords and visitors (generated), semantic types, generic instances and their substitution, the built-in types (the runtime primitives, `frozen`, `cown` and `Option`), `I128`, `Module` and `Project` | `diag` |
| `project` | loading a project: discovery, declaration tables, the import-cycle check | `ast`, `diag`, `par` |
| `syntax` | the grammar (`Maxon.g4`), module source buffers, the lexer, the parser generated from the grammar, and refactorings as edits to the source (of Maxon files and of the code in Markdown) | `ast`, `diag` |
| `resolve` | name resolution, and telling a subscript from type arguments | `ast`, `diag` |
| `check` | the type checker, the generic bounds, the ownership pass, and what a behaviour may capture | `ast`, `diag` |
| `ir` | the IR, its printer, collecting generic instances, lowering (the primitives' operations, where each value is dropped, and the body of each behaviour included), and the runtime's symbol names | `ast`, `diag` |
| `backend` | the x64 encoder, codegen, the runtime unit and the array runtime unit, the drop and copy routine of each type, the linker and the PE writer | `ast`, `diag`, `ir`, `par` |
| `interp` | the tracking interpreter: runs the IR of a whole program with the primitives in Go and a simulated scheduler of behaviours, and reports a violation of the memory model | `ast`, `diag`, `ir` |
| `driver` | `Compile`: runs the phases; `Prepare` and `Interpret`: run them as far as IR and hand it to the interpreter | all of the above |

The `minimaxon` command itself (`main.go`) is the CLI and the corpus runner. `internal/grammargen` is the command
that generates code from the grammar; nothing imports it.

### Generated code

Code that only restates a fact is generated from the one place that fact is written, so it cannot drift from it
or be written wrong. That place is `internal/syntax/Maxon.g4`, the grammar, in a subset of ANTLR 4 notation that its
header explains. It says how each construct is written and which AST node it builds: a label such as `cond=expr` is
a field of the node, `@ast::Node { ... }` adds the fields later phases fill in, and a rule whose alternatives are
nodes, such as `stmt` or `expr`, is a closed set of them. `internal/grammargen` turns it into:

| File | Holds |
|---|---|
| `internal/syntax/parser_gen.go` | a recursive-descent parser with one function per rule; operator precedence comes from the order of `expr`'s alternatives |
| `internal/syntax/tokens_gen.go` | the token kinds and the lexer's DFA: every token rule and keyword compiled into one table over byte classes |
| `internal/ast/nodes_gen.go` | the node structs and constructors, the reserved words, and three visitors per set |

The parser decides with one token of lookahead, or two where one is not enough. Where two alternatives start with
the same rule, as an assignment and an expression statement both start with an expression, it parses that rule once
and the token after it decides. A syntax error names what the grammar allows at that point, such as
`expected an expression, found ')'`. The loop that runs the DFA, the scanner for a string literal (`-> scan`, since
interpolations nest), the loop over a module's declarations, and the conversion of a token to a field that is not
its text (`intLitMagnitude`, `strLitParts`, `boundValue`) are hand-written in `syntax`.

The lexer takes the longest token at each position: a keyword wins over a name of the same length, and an earlier
token rule over a later one. It never backs off to a shorter match, so input that stops partway through a token,
such as `0x` or an unclosed `/*`, is an error naming the token from its `/** */` description ("incomplete
integer"). The generator rejects a token rule that matches the empty string or that an earlier token always beats.
Looking keywords up in the DFA instead of a map makes lexing about 1.4 times as fast.

Each set gets three visitor interfaces with one method per member, and a dispatch function for each:

| Visitor | Method | Dispatch |
|---|---|---|
| `StmtVisitor` | `VisitIfStmt(n *IfStmt)` | `VisitStmt(v, n)` |
| `StmtResultVisitor[R]` | `VisitIfStmt(n *IfStmt) R` | `VisitStmtResult(v, n)` |
| `StmtStateVisitor[S, R]` | `VisitIfStmt(n *IfStmt, s S) R` | `VisitStmtState(v, n, s)` |

Code that handles every member of a set goes through a visitor rather than a type switch, so adding a member
fails `go build` in each phase that has not handled it yet. A type switch remains only where code asks about a
few members, such as whether an expression can be assigned to. Regenerate after editing the grammar:

```powershell
go generate ./internal/syntax
```

`go test ./internal/grammargen` fails if a generated file is not what the grammar generates. A package exports only what another
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
| parse | `syntax` | reads the module's files into one buffer, lexes the whole buffer in one pass and parses the token stream into the AST; anything the grammar does not allow is a syntax error |
| tables | `project` | the module's declaration table and an empty type object for each of its types; then the project-wide name index |
| resolve | `resolve` | binds every name to its declaration and records module dependencies; brackets after a name that is a type or a function become its type arguments, and after anything else stay a subscript |
| declare | `check` | the module's fields, cases, function signatures, type parameters with their bounds, and the interfaces its types implement; a type argument is only recorded for the verify phase |
| constants | `driver` | top-level `let` types, in waves: each wave holds the constants whose dependencies are done (a cycle is `E2012`) |
| bodies | `check` | types every function body, a generic one once against its bounds, and the body of a behaviour as a function of its own that captures the variables it uses; a call into another module takes the callee's declared return type and is recorded, not checked; a call that needs an instance of a generic is recorded on its function |
| verify | `check` | checks the recorded cross-module calls and every type argument against its parameter's bounds (`E3117`), then ownership: moves, conventions, borrows, exclusivity, initialisation, frozen values and what a behaviour takes (`E3102`, `E3110` to `E3115`, `E3125`, `E2013`) |
| instances | `ir` | serial and cheap: starts from the calls that the functions which are not generic record, and follows each instance's recorded calls with its type arguments bound, until no new instance turns up (`E3122` if the type arguments grow without end); each instance is listed under the module of its generic, in the order found |
| lower | `ir` | the checked AST of each function, and of each instance of a generic, becomes IR (below), with a drop where each owner dies, and unreachable blocks are pruned; a place that needs something the backend cannot run yet is noted, and the driver refuses it before codegen (`E9002`) |
| codegen | `backend` | IR → x64, one code unit per module, calls between units symbolic; the runtime is its own hand-written unit, and so is the array runtime, which a program that calls an array entry gets. Each unit notes the drop and copy routines its code calls |
| routines | `backend` | serial to start, then parallel: the drop and copy routine of every type the units asked for, and of every type those reach (a record's fields, a union's payloads, an array's elements), made in waves of units of 256 routines; the order of the routines comes from the modules in source order, so it does not depend on `--jobs` |
| link | `backend` | offsets by prefix sum, then each unit copied and patched in parallel; the units go in module-path order after a runtime unit, then the routines, and the array runtime comes last |

`driver.Prepare` (and `Interpret`, which runs the result once) runs the same phases through lower and gives the IR to
the interpreter instead of codegen, which is how a map, a list, a pool, a frozen value, a cown and a behaviour run.

A phase that reports an error stops the compile, so later phases never see a broken module. The one serial step
is the import-cycle check between resolve and declare, a walk over the module graph.

### Reading a module

A module's files are read into one buffer, a string allocated once at the sum of their sizes, as they are written:
offsets in it are the files' own byte offsets, and a byte order mark is an error. Every token's text is a
substring of it, so lexing allocates no text and needs no intern table. A string cannot be read into, so each file
is read into a scratch buffer sized for the module's largest file and copied from there. Comments are `-> skip`
token rules, so the lexer drops them; a `///` doc comment stays in the buffer for the LSP to use later.

A token is 8 bytes: its kind and length packed into one 32-bit word (8 bits of kind, 24 of length, so a token is
at most 16 MB) and its start offset in the buffer. Offsets are 32-bit, so a module's source is at most 4 GB. A
token carries no text, value or position; the parser reads them from the buffer when it needs them. Its text is a
substring of the buffer, an integer's value is read again from its digits, and a string's text runs are substrings
unless an escape forces a copy. A line and column comes from the offset and an index of where each line starts,
which the parser walks from the last line it asked about, so placing a node costs a step or two. Storing the
length is about 15% faster than re-lexing a token to find it, and the packing keeps it at 8 bytes.

The lexer works on the raw UTF-8 bytes, so a column counts bytes. One lexer pass covers the whole buffer. At each
file boundary it resets the bracket depth and the indentation, so no token spans two files, and emits a `FileEnd`
token. It reads a file line by line: it checks each line's indentation and emits `Indent` and `Dedent` where it
changes, checks blank lines and comment lines, and runs the DFA over each code line, stepping over the one space
between two tokens without a round of the DFA. One parser reads the module's stream, naming each file in its
diagnostics. A declaration left open at the
end of a file fails there with `E2007`, and parsing resumes at the next file, so each file reports its own first
syntax error.

### Allocation

Parallel phases are only as fast as allocation allows. During a compile the driver turns the collector off
(`debug.SetGCPercent(-1)`) under a memory limit of 100 bytes per source byte, at least 256 MB, so a compile that
outgrows it still collects. With the collector off no memory is reused, so every byte allocated is a fresh page the
operating system commits and zeroes: on Windows that costs about 0.2 s per GB, a large share of a compile. What
allocates is therefore what costs, and the phases avoid it:

- the lexer reuses token buffers of 8-byte tokens and takes token text from the source string, and an
  interpolation's parser borrows a spare token buffer;
- codegen sizes each unit's code buffer from its instruction count, and reuses its block labels between
  functions, because Go grows a large slice by about a quarter at a time, allocating several times its final size; the
  routines are sized the same way, and a call of a routine names it by an interface value that is made once for each type
  in a unit, not once for each call;
- the PE writer sizes the image before writing it, and the linker reads each unit's data in place;
- the ownership pass shares its state until a move or an initialisation changes it, and reuses its loop and
  match frames.

Watch the MB column of `--timings` when changing a phase.

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
with `HeapAlloc` on the process heap and freed with `HeapFree` where their owner dies (see "Drops"); a box is zeroed when
it is allocated. A string is a pointer to `[length][UTF-8 bytes]`: a literal lives in the executable's data section and is
never freed, any other string is a box on the heap. An enum value is its case index.

### The primitives in the IR

An `array`, a `map`, a `list` and a `pool` (see "Primitives") are heap boxes that only runtime entries touch, so the
IR holds no layout of them: an operation is a `runtime` call of a named entry (`internal/ir/symbols.go`), and the
interpreter and the backend each implement the entries. The element is always visible in the call:

- An entry that takes an element **moves it in** (`rt.array.insert`, `rt.map.insert`, `rt.list.pushBack`,
  `rt.pool.insert`), one that returns an element **moves it out** (`rt.array.remove`, `rt.list.popFront`,
  `rt.pool.remove`), and `rt.<primitive>.elem` returns the **address** of an element: a borrow, valid until the next
  call that changes the primitive. A subscript is that call and a `loadat` or a `storeat`, and a `mutable` argument
  `a[i]` is the address itself. A value that changes the primitive is computed before the address is taken, which
  is what the ownership pass's exclusivity rules guarantee at the source.
- An entry that is given an index, a key or a handle that names nothing traps. The queries that cannot miss
  (`rt.<primitive>.len`, `rt.map.has`, `rt.pool.contains`, `rt.list.contains`) are what lowering tests before it
  calls an entry that would trap, to build an `Option`.
- A handle is a word, never 0. An entry that returns one returns 0 for none, and lowering turns that into `none`.
- An `Option` is a union box of case index and payload (`some` is 0, `none` is 1), built by lowering, so no entry
  knows the layout of a union.
- `copy()` is one instruction, `%r = copy %s T` (see "Drops"), whatever `T` is: a record, a union, a string or a
  primitive. What it does to each part is code that depends on the type, so it is a routine of its own for each type that
  the backend makes (below) and the interpreter does by the shape of the value; a copy of an array copies every element by
  its own type, and a copy of a pool or a list keeps its handles valid.
- A `Handle[T]` is `scalar`; everything else here is `managed`.

**Native.** Only the array has code in the backend. It is a box of three words, the length, the capacity and the
address of its elements, which are 8-byte words; `rt.array.insert` grows it by doubling and shifts what follows, and
frees the buffer it leaves (the elements moved to the new one, so only the buffer goes), and `rt.array.remove` hands the
element it removes to the caller, who owns it. The entries are written in x64 in `internal/backend/arrays.go`,
a unit of their own that the driver links only into a program that calls one. A map, a list and a pool are lowered all
the same, which is what the interpreter runs, but the driver refuses a function that needs one before it generates code:
`E9002`, at the first use in each function. A function that only drops, copies or holds a value that contains one (a
`sink` parameter of type `map[K, V]`, or a record with a pool in it) is refused too, at the function, since there is no
routine to free it.

### Frozen values, cowns and behaviours in the IR

A frozen value and a cown are boxes with a count of owners, so they are the one kind of value that more than one place
owns (`internal/ir/symbols.go`):

| Entry | Does |
|---|---|
| `rt.frozen.freeze(v)` | moves `v` into a new box with a count of 1 |
| `rt.frozen.get(f)` | lends what `f` holds, valid while `f` is held: how every read through a frozen value begins |
| `rt.share(f)` | the same box, with the count raised by 1: another owner, which is `share` and the `copy()` of a frozen value or a cown |
| `rt.cown.new(v)` | moves `v` into a new cown with a count of 1 |

Dropping an owner (`drop`, `dropslot`, `dropat` of a `frozen[T]` or `cown[T]`) lowers the count by 1, and the box is
freed with what it holds when the count reaches 0. A scalar can be held (`cown[Count]`, `frozen[T]` with `T` a scalar in
an instance of a generic).

A `when` is one instruction, `when body cowns [%a, %b] captures [%c]`. The cowns are read, not moved, and each gains a
count of its own for as long as the behaviour is waiting or running. Each capture is a value that moves in: a managed one
was moved out of its owner by the instruction before, so it is in flight. **The body is a function of its own**, in
`IrFunction.Behaviours` of the function around it and printed after it: its parameters are the access to each cown's value
(an address, as a `mutable` parameter has), then the captures, which it owns as a `sink` parameter does and drops at its end.
It returns nothing, and nothing waits for it. Its name is the enclosing function's with `$when` and a number, in source
order (`transfer$when0`, `swap[String]$when0` in an instance, `outer$when0$when0` for one inside another), so a generic
function that holds a behaviour has one for each instance, lowered with the instance's type arguments.

**Native.** None of it has native code: green threads, a scheduler and atomic counts are not built (see
[docs/regions.md](docs/regions.md)). The driver refuses a function that freezes, shares, thaws, makes a cown or schedules a
behaviour, or reads through a frozen value, with `E9002` like a map, a list or a pool. `freeze` alone could be native, since
nothing is counted or freed there yet, but a program that freezes and nothing else cannot show anything about the rules,
and refusing every one keeps the backend free of a half of the feature.

### Drops

The IR says where every owner dies. Lowering emits a drop wherever the rules of "Ownership" free a value, and both the
interpreter and codegen free exactly there.

| Instruction | Drops |
|---|---|
| `dropslot s2 T` | what slot `s2` owns, if anything, and leaves it empty: a local at the end of its block, on every way out of it (fallthrough, `return`, `break`, `continue`), and the old value of a local that is assigned |
| `dropat %a T` | what the address holds, if anything: the old value of a field, an element or a `mutable` or `set` parameter that is assigned |
| `drop %r T` | the value in a register: a temporary that nothing took, an element removed and discarded, the box of an `Option` that was unwrapped, the value given to `array.filled` |
| `%r = move s2` | reads a slot and leaves it empty: a whole local that moves (a binding, a `sink` argument, a return, a field, a branch of a conditional) |
| `%r = moveat %a` | reads the value at an address and leaves it empty: the payload that leaves the box of an `Option` that `unwrap` consumes |
| `%r = copy %s T` | a deep copy of the owned value in `%s`, which stays where it is: a new box for each record, union and array inside it, a new string for each string that is not a literal, and another owner of the same box for a frozen value or a cown |

`T` is the type with an instance's type arguments applied, so codegen can call the drop routine of that type. Dropping a
value drops what it owns, deeply by type: the fields of a record, the payload of a union, the elements of a primitive. A
borrow is never dropped: a `let` or `mutable` parameter, a projection, and a `match` binding of a payload, which is
stored with `store s3, %r borrow`. A place that holds nothing, because it was moved from or never assigned, drops as a
no-op, so one drop at the end of a scope serves every path to it and no analysis of paths is needed. A function that
drops a value that holds a map, a list, a pool, a frozen value or a cown is refused natively like one that makes it.

- **One place decides what moves.** The ownership pass marks each expression whose value changes owner
  (`ast.Expr.IsConsumed`): a binding, an assignment, a `sink` argument, a return, a field or a payload, a branch of a
  conditional or a match. Lowering reads that mark and nothing else. A consumed place is moved out of, and the pass
  refuses every consumed place that is not a whole local; a consumed fresh value goes to its new owner; a fresh value
  that is not consumed is a temporary. The values a declaration holds outside any function, a constant and a field's
  default, are checked by the same pass for the same reason.
- **Scopes.** A block owns the locals declared in it, and each statement, and each branch of a conditional or a match arm,
  owns the temporaries it makes: they are dropped when it ends, newest first, and a jump that leaves several scopes drops
  all of them. A condition drops its temporaries before it branches.
- **Sink parameters** are copied into a slot at entry, which owns them from there, so the callee can move on or drop
  them like a local. A `sink _` parameter is dropped too.
- **A string is owned like any value.** A literal and the name of a case are immortal and never freed (a drop of one is a
  no-op, found by its address natively), but an interpolation makes a new string, and so does `copy()` of a string that is
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
  type those reach, in the "routines" phase of the pipeline. A drop routine takes the value (0 does nothing) and drops
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
  double free or crash, but not that it freed everything.
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
- **Traps** behave as the executable does: `rt.panic`, an overflow, a zero divisor, a negative shift, a failed range
  check, an index out of range, a stale handle, an unwrap of none and a behaviour that names one cown twice write to
  standard error and exit with 1; the same standard output and the same exit code. A trap is not a violation.
- **How tests use it.** `minimaxon test` runs every program under it, under 20 schedules when the program schedules
  behaviours, and requires the same stdout and exit code as the directives and no violation. `internal/interp` tests it
  on IR that is written to break each rule (and to schedule behaviours: their order, their interleaving, the counts), on
  the order in which values are freed, and on programs from a generator (`generator_*_test.go`): random well-formed
  programs over records, unions, strings, the four conventions, calls with overlapping and disjoint arguments, match
  bindings, loops, generics with bounds, arrays, maps, lists and pools with handles, and frozen values, cowns and
  behaviours. The checker refuses most of them, since the generator leans on the edges of the rules: about two
  programs in five are careless and break rules throughout, and two in five of the rest break exactly one rule of
  sharing, once (a behaviour that takes a borrow, a variable used after a behaviour took it, a frozen value changed or
  taken apart, a frozen value's method that changes it, a cown read outside a behaviour), so that a rule the checker
  lacked would be all that stands between that program and the interpreter. Every program it accepts must run clean, under several schedules when it
  schedules a behaviour, and one that does not is a hole. `go test ./internal/interp` runs 300 programs, each under 3
  schedules. `MINIMAXON_GEN_PROGRAMS=N`, `MINIMAXON_GEN_SEED=S`, `MINIMAXON_GEN_SEEDS=K` (schedules per program) and
  `MINIMAXON_GEN_KEEP=dir` (save a failing program) set a long run, and `MINIMAXON_GEN_NATIVE=1` also builds and runs
  each accepted program that has an executable, with its allocations counted, and compares it with the interpreter's: the
  same output and exit code, and no box left live.
  `MINIMAXON_GEN_DUMP=file` writes the program of a seed, and `MINIMAXON_GEN_EXPLAIN=1` lists why programs were refused.

## The subset

- `function`, `type` (fields, defaults, methods, `static function`), `enum` (integer raw values), `union`
  (payloads, methods), `interface`, `typealias X = int(a to|upto b)`, a top-level `let` constant. A function, a
  type and a union can be generic (see "Generics").
- `let`, `var` (with a value, or `var x as T` without one), assignment to a variable, a field or an element,
  `_ = call()`, `if`/`else if`/`else`, `while`, `for i in a to|upto b`, `match` as a statement or expression
  (case, literal, range and string patterns, `or` alternatives, `default`, `panic` arms), `break`/`continue`
  with labels, `return`, `when` (a behaviour, see "Immutable values, cowns and behaviours").
- Integer, bool and string literals, interpolation, arithmetic and bitwise operators, `shl`/`shr`,
  comparisons, `and`/`or`/`xor`/`not`, `x if c else y`, `as` casts, calls with Maxon's
  positional-then-labelled arguments, parameter conventions (`mutable`, `sink`, `set`), `x.copy()`,
  `.name`/`.ordinal`/`.rawValue`, explicit type arguments `swap[Count](a, b: c)`, subscripts `a[i]`.
- Built in: `print`, `printError`, `panic`, `freeze`, `share`, `thaw`, `cown`, the aliases `ExitCode`, `Count`,
  `FullRangeInt`, and the generic types `array`, `map`, `list`, `pool`, `Handle`, `Option` (see "Primitives"),
  `frozen` and `cown` (see "Immutable values, cowns and behaviours"). There is no other stdlib.

### Integers

Every typealias is its own type. An integer literal is an untyped constant: it fits any alias whose range
holds it, and constant expressions fold at compile time. A value whose range is known but has no alias,
such as a `for` variable or arithmetic over constants, fits any alias whose range contains it. Mixing two
different aliases is `E3005`. Arithmetic traps on 64-bit overflow, and on leaving the alias's range,
checked where the result is computed. `as` checks at compile time for a constant and at run time otherwise.

### Ownership

Layer 1 of [docs/regions.md](docs/regions.md), checked in one pass over each function after typing
(`internal/check/ownership.go`, with places in `paths.go`). A value of a record, union, `String` or
type-parameter type has one owner; every other access is a borrow that cannot outlive the call or match that made it.

- **Moves.** Binding a place (`let b = a`), assigning it, passing it to a `sink` parameter, returning it, and
  storing it in a field or a union payload move it, and so does a conditional or a `match` arm that gives it. Using
  it afterwards is `E3102`; assigning the variable again gives it a value again. A scalar is copied and never
  moves, and a fresh value (a call, a literal) needs no move. A conditional's or a match's value is always a fresh
  one, so a place in a branch is moved even where the whole expression is only borrowed.
- **`x.copy()`** is a deep copy that every type has, so no type may declare a method `copy`. It allocates a new
  box for each record or union inside `x`, and a new string for each string, so that every value has an owner of its
  own to free it.
- **Conventions.** A parameter takes its argument as `let` (no marker: an immutable borrow), `mutable` (a mutable
  borrow; the caller sees the change and passes no marker), `sink` (the callee owns it) or `set` (the callee
  initialises it). A method's receiver is `self` (let), `mutable self` or `sink self`. A `sink` parameter is owned
  but not mutable, like a `let` local: move it into a `var` to change it. Assignment, and passing as `mutable`,
  need a mutable place: a `var`, a `mutable` or `set` parameter, or `mutable self`, through fields that are all
  `var` (`E2013`). `self` itself is the receiver and not a place that can be replaced, so it is never a `mutable`
  argument (`E2013`); a field of it can be, and a method can be called on it.
- **Borrows are second-class.** A `let`, `mutable` or `set` parameter, a `match` binding of a payload that is not
  a scalar, and anything reached through one is borrowed: moving it anywhere (a binding, a field, a `sink`
  argument, the return value) is `E3110`. A field of an owned value is never moved out either, because the value
  would be left partly moved (`E3111`), and neither is an element of a collection, which the collection owns, nor a
  part of a temporary, which is dropped when its statement ends; `copy()` is the fix for all of them. A match binding also cannot be assigned, and
  the subject cannot be assigned, moved or passed `mutable` while the arm runs (`E3112`). There are no local
  projection bindings (`mutable r = a.f`): borrows exist only as parameters, match bindings and call arguments.
- **Exclusivity.** A place is a local or parameter followed by field selections and element selections (`a[i]`, see
  "Primitives"), and two places overlap when one is a prefix of the other (`a` and `a.f`; `a.f` and `a.g` do not;
  `a[i]` and `a[j]` do, for every index). An argument passed `mutable`, `sink` or `set`
  must not overlap any other argument of the call, receiver included (`E3112`). The rule applies to every
  argument that names a place, scalars too. An argument that is a borrowed place stays borrowed while the later
  arguments are evaluated, and so does the target of an assignment while its value is, so a call nested there that
  changes an overlapping place is refused too (`E3112`).
- **Initialisation.** `var x as T` declares a variable with no value. Reading it before every path has assigned
  it is `E3113`. A `set` parameter starts without a value and must have one on every path that returns
  (`E3114`); its argument must be such a variable, which has a value after the call (`E3115`), and not before:
  another argument of the same call that reads it is `E3113`.

### Generics

Layer 1's generics ([docs/regions.md](docs/regions.md), step 2): a generic body is checked once, against its bounds,
and each instance is lowered once.

```
interface Shape
	function area() returns Count
	function grow(mutable self, by Count)
end 'Shape'

type Square implements Shape
	var side as Count

	function area() returns Count
		return side * side
	end 'area'

	function grow(mutable self, by Count)
		side = side + by
	end 'grow'
end 'Square'

type Box[T]
	var item as T

	static function make(item sink T) returns Self
		return Self{item: item}
	end 'make'
end 'Box'

function total[T implements Shape](a T, b T) returns Count
	return a.area() + b.area()
end 'total'
```

- **Declaring.** A function, a `type` or a `union` takes type parameters in brackets, `[T, U implements Shape and Other]`;
  `implements` names the bounds a parameter must meet. A type, an enum or a union says which interfaces it implements with
  `implements` too: `type Square implements Shape and Named`. An interface lists method signatures, each with its receiver convention
  (`mutable self`, `sink self`, or none for a `let` receiver), and may name `Self`. It is a bound and nothing else: it
  is not the type of a value (`E3119`), has no type parameters, no static function and no parent interface, and an
  alias, `String` or `bool` cannot implement one. A method uses its type's parameters and takes none of its own
  (`E3123`). `Self` inside a generic type is the type at its own parameters, and a literal of it is `Self{...}`.
- **Using.** Type arguments are written after a name, `Box[Count].make(1)`, `swap[Count](a, b: c)`,
  `lib.Option[Count]`, or inferred from the arguments' types: `swap(a, b: c)`, `Option.some(x)`, `Box.make(x)`. An
  integer constant or the range of an expression names no alias, so it infers nothing and the argument is written
  (`E3118`). Their number is checked (`E3116`). In an expression `Box[Count]` and `a[i]` are written alike, so the
  parser reads brackets after any expression as a subscript, and the resolver, which knows what a name stands for,
  turns them into type arguments when the name is a type or a function; after a variable, a field or anything else
  they stay a subscript.
- **Checked once.** A generic body is typed and checked for ownership once, where it is declared, whether or not
  anything instantiates it, and its errors are reported there and only there. Inside it a value of a type parameter
  has the methods of its bounds (`a.area()`, with `Self` read as the parameter; two bounds that declare one name
  are `E3121`) and what every type has: move, a borrow under any convention, `copy()`. Nothing else: no field, no
  operator, no `match`, no interpolation.
- **Ownership over `T`.** A value of a type parameter is owned, movable and not a scalar, whatever `T` is
  instantiated with, so every rule of "Ownership" applies to it unchanged and an instance cannot break what was
  checked once. Binding it, passing it to `sink`, returning it or storing it moves it (`E3102` after); a `let` or
  `mutable` parameter of type `T`, a payload of one and a field of a generic type are borrowed (`E3110`, `E3111`);
  `copy()` keeps one. So a generic container gives its element out with `.copy()`, or takes it out of a primitive
  with `remove`, `pop` or `unwrap` (see "Primitives"): no value is moved out of a field or a payload.
- **Bounds at the instance.** Writing a type argument is the only place an instance is judged: it must implement
  each interface its parameter is bounded by, which is a declaration (`type Round implements Shape`) and not a structural
  match, and the error names the declaration to write and the methods missing (`E3117`). A declaration that says
  `is Shape` is checked once, for every method with the signature `Shape` gives (`E3120`), so a bound never fails
  later. Inside another generic, a type parameter meets a bound only if it is declared with it. Bounds belong to
  declarations that another module may still be declaring, so they are checked in the verify phase. A bound's
  method is called through the interface, so the implementing method needs no `export`.
- **Lowering.** An instance is lowered by monomorphization: the checked body is lowered again for each list of
  type arguments with the parameters bound, so a slot is `scalar` or `managed` for the instance's type, `copy()`
  copies that type, and a call through a bound calls the method of the type that stands in. The IR names an
  instance `swap[Count]` or `Box[String].make`, and emits it in the unit of the module that declares the generic;
  calls to the types of other modules are symbolic. The `instances` phase finds them serially, so their order does
  not depend on `--jobs`.
- **Interning.** `Box[Count]` is one type wherever it is written: each generic declaration interns its instances,
  keyed by their arguments, behind a lock, which is the only write that parallel phases share. An instance holds no
  members of its own; it reads the declaration's fields, cases and methods and substitutes its arguments.
- **Limits.** A generic type may not contain itself, even with a larger type argument each time (`E4014`), and a
  generic function may not call itself with ever larger type arguments (`E3122`, past a nesting depth of 16).

### Primitives

Layer 2 of [docs/regions.md](docs/regions.md), as far as step 3 goes: their signatures and ownership rules. An
`array[T]`, a `map[K, V]`, a `list[T]` (doubly linked, one node per element) and a `pool[T]` own their elements.
Their operations are generic methods that no source declares (`internal/ast/builtins.go`), typed, borrowed and moved
like any other method from the signature below, so none of the ownership rules has a special case for them. They are
declared in the stdlib module, so a name resolves to them last, after the module's own, the root's and an exported
one anywhere.

| Type | Operations |
|---|---|
| `array[T]` | `array[T].new()`, `array[T].filled(n, value: x)`; `count()`, `isEmpty()`; `append(item)`, `insert(item, at: i)`, `remove(i)` returns `T`, `pop()` returns `Option[T]` |
| `map[K, V]` | `map[K, V].new()`; `count()`, `isEmpty()`, `has(k)`; `insert(k, value: v)`, `remove(k)` returns `Option[V]` |
| `list[T]` | `list[T].new()`; `count()`, `isEmpty()`, `contains(h)`, `first()`, `last()`, `next(h)`, `prev(h)`; `pushFront(item)` and `pushBack(item)` return `Handle[T]`; `popFront()` and `popBack()` return `Option[T]`; `remove(h)` returns `Option[T]`; `moveToFront(h)` |
| `pool[T]` | `pool[T].new()`; `count()`, `isEmpty()`, `contains(h)`; `insert(item)` returns `Handle[T]`; `remove(h)` returns `Option[T]` |
| `Handle[T]` | no operations; `==` and `!=` |
| `Option[T]` | cases `some(value)` and `none`; `isSome()`; `unwrap()`, which consumes the option (`sink self`) and panics on `none` |

An operation that changes the primitive takes `mutable self`, and one that adds an element takes it `sink`. `first`,
`last`, `next` and `prev` return `Option[Handle[T]]`.

- **Collections own their elements.** Adding takes the element `sink`, so it moves in. Taking one out is `remove`,
  `pop`, `popFront`, `popBack` or `unwrap`, which move it out to the caller, owned; the only other way to hold an
  element is `.copy()`. `copy()` of a collection is deep.
- **A subscript is a projection.** `a[i]`, `m[k]`, `l[h]` and `p[h]` borrow the element: the array is indexed by a
  `Count`, a map by its key, a list and a pool by the `Handle` of the element. Read, it is a `let` borrow; assigned
  through (`a[i] = v`, `a[i].count = 3`) or passed to a `mutable` parameter, it is `mutable`. It is never an owned value:
  binding it, returning it or passing it to a `sink` is `E3111`, or `E3110` where the collection itself is borrowed,
  and `copy()` is the fix. A scalar element is copied like any scalar, so `let n = a[i]` is fine. The element of a
  temporary (`make()[0]`) cannot be moved or assigned either.
- **A miss traps**, as an out-of-range index, a missing key or a stale handle: a range-check guard, safe, and it
  never touches freed memory. The operations that cannot miss let a program test first: `count`, `isEmpty`, `has`,
  `contains`, and `first`, `next` and the others that return an `Option`.
- **Exclusivity.** A path gains a second kind of step, an element, and every element of a collection overlaps every
  other one, whatever the indexes say: `a[i]` and `a[j]` overlap, so `pair(a[i], b: a[j])` and `f(a, n: a[0])` are
  refused (`E3112`), while `pair(a[i], b: b[j])` and `pair(r.left[0], b: r.right[0])` are fine. A method with an
  `mutable self` receiver, such as `append`, `insert` and `remove`, overlaps every projection of its collection in the
  same call: `a.append(a[0])` is refused, and `let x = a[0]; a.append(x)` is the fix for a scalar. An index, a key
  or a handle is an ordinary value, read before the call, so `a[a.count() - 1]` and `a.insert(x, at: a.count())`
  are fine.
- **A borrow is held while the rest is evaluated.** An argument that borrows a place, and the target of `x.f = v` and
  `a[i] = v`, stays borrowed while later arguments, or the value, are evaluated, because the address is taken first.
  A call in there that changes an overlapping place is refused (`E3112`): `look(a[0], n: drop(a))`, where `drop`
  takes `a` as `mutable`, and `a[0].n = drop(a)`. A scalar read by a `let` parameter is a value by then and holds
  nothing.
- **Handles are keys.** A `Handle[T]` is a plain word: copy it freely, store it in a field, in a map, in an array or
  in another element, compare it with `==`. It owns nothing. Using one is a lookup, so a handle whose element was
  removed finds nothing, and a slot that was reused does not answer to the old handle, because a handle also
  carries a generation. A handle of one pool used on another pool of the same element type is not a soundness issue:
  the lookup fails or finds that pool's element at the same slot. A handle of another element type is refused
  (`E3005`). A `list` hands out the same kind of handle for its nodes, so a cache is a `list` of entries in order of
  use and a `map` from each key to the handle of its entry, and a linked structure is a `pool` of nodes that name
  each other by handle (see `tests/` and `internal/check/primitives_test.go`).
- **Map keys** are compared and copied as a word or as text: an integer alias, a `bool`, an enum, a `String` or a
  `Handle`. Any other key, a record or a union or a type parameter, is `E3124`. A generic function can therefore take
  a `map[String, V]` with a generic `V`, but not a `map[K, V]` with a generic `K`; a bound for keys would lift that.
  `insert` replaces the value of a key that is there, which is dropped.
- **Generic code** over a primitive is checked once, as in "Generics": a value of type `T` held by an `array[T]` is
  owned and movable, so a generic container gives an element out with `remove` or `pop`, or `.copy()`.
- **Iteration** is `for i in 0 upto a.count()` and a subscript. There is no `for x in a`, and a map has no way to
  walk its keys yet.
- **Running them.** An array runs natively: heap-allocated, bounds-checked, and freed with what it holds where its owner
  dies (see "The primitives in the IR" and "Native freeing"). A map, a list and a pool are checked and lowered, and the interpreter runs them (see "The interpreter"), so a
  program that uses one is refused when it is compiled to an executable (`E9002`) and still passes `check`; the test
  runner runs such a program under the interpreter alone.
- **Limits.** `Option[T]` here is the stdlib's; a module that exports its own `Option` wins the bare name in the
  other modules, and `stdlib.Option[T]` names this one. A primitive implements no interface (`E3117`). A record
  that contains itself through an array (`var kids = array[Tree].new()` in `Tree`) is fine, natively too: its drop and
  its copy are routines that call themselves.

### Immutable values, cowns and behaviours

Layer 3 of [docs/regions.md](docs/regions.md), step 5: how data crosses between threads, after Verona's
behaviour-oriented concurrency. There are no threads in a compiled program yet (see "Native" below); the interpreter
runs behaviours under a simulated scheduler, and the checker's rules are what the interpreter's runs test.

```
type Account
	export var balance as Count
end 'Account'

function transfer(from cown[Account], dest cown[Account], amount Count)
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
| `freeze(x)` | takes `x` (`sink`) and gives a `frozen[T]`, an immutable value; `T` is not a plain value (`E3127`) |
| `share(f)` | another owner of the frozen value or the cown that `f` (a `let` borrow) names; the count goes up by 1 |
| `thaw(f)` | an owned, mutable deep copy of what a frozen value holds |
| `cown(x)` | takes `x` (`sink`) and gives a `cown[T]`, a reference to a cown that owns the value; `T` may be a plain value |
| `when a, b as x, y 'label'` … `end` | a behaviour: runs later, once it has exclusive access to the cowns `a` and `b`, with `mutable` access to their values as `x` and `y` |

- **Frozen values.** The contents are lent and never given: reading a field, calling a method whose receiver is `let`,
  subscripting, matching, interpolating, and passing the frozen value to a `let` parameter of the type it holds all see
  the contents (`d.title`, `d.total()`, `describe(d)`). Anything else is refused: assigning through it, a `mutable`
  argument, a method that takes `mutable self` or `sink self`, or a `sink` parameter is `E3125`, and taking a part out of
  it is `E3111`. The type system tells a frozen value from its contents by the type `frozen[T]`, so a place's path
  gains a step into the contents (`paths.go`), and every rule about places, borrows and exclusivity applies to it as it
  does to a field. `copy()` of a frozen value, of a cown reference, and of anything that holds one, is a `share`: the
  copy is another owner of the same value, never a new value; `thaw` is how to get a mutable copy of the contents.
  There is **one count per frozen value**, however many objects it holds. A frozen value is built from a value that was
  owned (a tree) and given up by `freeze`, so what it holds is either part of that tree or a counted value that existed
  before it, and nothing can be added to it afterwards: counts form a DAG and no cycle of frozen values can exist. A
  string literal and a constant are immortal and uncounted.
- **Cowns.** A `cown[T]` is a counted reference: `share` and `copy()` make another, dropping one lowers the count, and
  the cown and its value are freed when the last reference is gone and no behaviour holds it. The value is touched only in
  a behaviour (`E3132` for a field, an element, a method, a match or an interpolation of a cown outside one). A type that
  can reach itself through a cown, directly or through other types, is refused (`E3131`): a behaviour could store a
  reference in the value, two cowns could refer to each other, and counting never frees a cycle. A handle or a key
  does the same job without the cycle.
- **Behaviours.** `when` names its cowns, which are plain variables or fields of `self` (`as` would read as a cast after
  an expression), and gives one name for each value; the cowns are only read, so naming one does not move it. The body is
  a function of its own that sees the variables around it by capturing them: **a scalar is copied; an owned value moves
  in at the `when`**, so using it afterwards is `E3102` (a loop that schedules a behaviour that takes a variable declared
  outside it fails the same way); a frozen value and a cown reference are owned values too, so one that the enclosing
  code keeps is shared first (`let own = share(f)`). A borrow cannot be captured, because the behaviour outlives it:
  a `let`, `mutable` or `set` parameter, the receiver and its fields, a `match` binding, and `x` or `y` of an enclosing
  behaviour are `E3126`, with the fix named (`share` for a shared value, `copy()` for the rest). A captured value is
  not mutable in the body (it is moved into a `var` to change it), like a `sink` parameter. `x` and `y` are `mutable`
  parameters of the body, so they cannot escape: moving, returning, storing or passing them to `sink` is `E3110`, and
  `copy()` is the way out. A behaviour returns nothing and is not waited for (`return value` is `E3005`); it may
  schedule more behaviours, and passing a result on is how a chain works. Naming one cown twice is `E3130`, and a count
  of names that differs from the count of cowns is `E3129`, as is naming something that is not a cown reference.
- **The one run-time check.** Two different variables can name one cown (`let b = share(a)`), and the checker cannot
  tell, so a behaviour that is scheduled with the same cown twice traps (exit 1, "names the same cown twice") instead of
  giving two `mutable` names to one value. It is a trap, as an index out of range is, and never a race.
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

### Where it differs from Maxon

- A compiled program frees each value where its owner dies, as the IR says (see "Drops" and "Native freeing"), with one
  `HeapFree` and no reference counting; Maxon counts references. There is no green thread, scheduler or atomic count, so
  a frozen value, a cown and a behaviour have no native code: the interpreter counts a frozen value and a cown, schedules
  behaviours, and fails a program that leaks a value. A native program that is not built with `--count-allocations` cannot
  tell that it leaked, and a program that panics or traps does not free what it holds.
- `/` and `mod` panic on a zero divisor instead of throwing `DivisionByZero` under `try`.
- No alias subtyping (an alias implementing another), floats, characters, tuples, closures,
  `try`/`throws`, async, services, region capabilities (`isolated`, `immutable`, `modify`), static fields, default
  parameters, format specifiers, or `#if`. The memory model of [docs/regions.md](docs/regions.md) is implemented up to
  regions: owned values, the primitives, frozen values, cowns and behaviours (see "Ownership", "Primitives" and
  "Immutable values, cowns and behaviours"), with an interpreter that runs all of it and a native backend that runs
  only the array. `freeze` is a function here, `when` is a statement that takes cowns and gives names to their values,
  and a frozen value is a type, `frozen[T]`, where Maxon recovers immutability from facts about a value's history.
  The grammar has none of the rest, so each is a syntax error.
- The primitives are not Maxon's `Array`, `Map` and `List` wrappers: an array, a map, a list and a pool are the
  built-in types themselves, they have no iteration yet, and indexing is a subscript that traps on a miss.
- Generics are checked once against their bounds, never per instantiation; maxon checks a generic function per inferred
  type. Interfaces are bounds only: no generic interface, no static requirement, no default method, no existential
  value; a type implements one by saying so, and `int` aliases, `String` and `bool` implement none.
- The reserved words are exactly the keywords the grammar uses, so a word Maxon reserves for a feature minimaxon
  lacks is an ordinary name, and a word such as `mutable`, `sink` or `set` that Maxon reads by context is reserved.
- Modules differ from Maxon in three ways. Visibility is flat, not "the directory and its subdirectories".
  Import cycles are refused. And the rule that an exported signature may not name a less visible type
  (`E3167`) is not checked.
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
- **`minimaxon test --update-positions`** moves each `expect-error` whose code is now reported at another line or column,
  as after a rename that changes a line's length. It never changes the code a test expects, so a test that now fails
  with another error still fails.
- **Unit tests.** `minimaxon rewrite` over `internal` renames keywords in the unit tests' `testdata/*.maxon` too. The
  only keyword spellings left in Go are the constants in `internal/interp/keywords_test.go`, which the program generator
  builds programs from, and a few assertions that name one keyword, which the Go tests find.
- **`.\check.ps1`** runs every check this repository requires, formatting and applying `go fix` first, and stops at the
  first failure. Arguments go to `minimaxon test`, so `.\check.ps1 --update-positions` works.

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
3. **Typing:** `Visit<Node>` on `Checker`, and on `termination` for a statement. Store what codegen needs on the
   node. Read another module's types and signatures freely, but never write them: only the declare phase writes
   a module's own declarations.
4. **Ownership:** `Visit<Node>` on `Ownership`. An expression whose value goes to a new owner is passed through
   `consume`, which is what marks it for lowering.
5. **Lowering:** `Visit<Node>` on `Lowerer`, built from the existing instructions where it can. A value it makes that
   nothing consumes is a temporary, and a place it declares or assigns drops what it held (see "Drops"). A new
   instruction needs a case in `codegen.instr` (`internal/backend`), `instrText` (`internal/ir`) and `exec` and
   `destination` (`internal/interp`) as well.
6. **Tests:** a unit test in the package that changed, a `tests/run/` program for the behaviour (it runs natively
   and under the interpreter), and a `tests/errors/` program for each diagnostic. A new construct that the memory
   model's rules cover also goes into the program generator (`internal/interp/generator_*_test.go`).

A rule that is only a diagnostic, such as a new ownership or region check, usually needs steps 3 or 4 and
a test, and nothing else.
