# minimaxon

A Maxon compiler for trying out language changes, and the seed of the next full Maxon compiler. It is written in
Maxon (`self/`), compiles itself, targets x64 Windows and x64 Linux (`--target`), and writes a PE executable or a static ELF
one directly, with no external assembler, linker or C runtime.

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
itself, with today's code generator even where the seed's is older. It finds `runtime/` by walking up from its own executable, so it stays in the tree.

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
| `minimaxon build <file\|dir> [-o out.exe] [--target=<arch>-<os>]` | compile; the path given is the project's root, and a directory is a project, one module per directory (see "Modules"); `--target` names the target, `x64-windows` or `x64-linux` (see "Targets"), the system the compiler runs on by default |
| `minimaxon run <file\|dir>` | compile to the temporary directory, as `minimaxon-run-<process id>-<name>.exe`, and run; exits with the program's code |
| `minimaxon check [--tests] <file\|dir>` | diagnostics only, exit code 1 when there are any; `check runtime` checks the runtime's source and holds it to the functions the compiler calls (see "The runtime"); `--tests` checks every module's files of tests (`.maxtest`) with its source |
| `minimaxon test [--update-positions] [--behaviours] [--repeat=N] [--executables=N] [--target=<arch>-<os>] [dir]` | run the corpus (default: the nearest `tests` directory from the executable upward; see "Tests"), compiled for the target and run through its runner |
| `minimaxon test <specs-dir\|file.md> [--verbose] [--require-passed\|--skip-passed] [--record-passed] [--executables=N]` | run maxon's spec tests (see "Tests") |
| `minimaxon test <module dir>` | run the tests of the modules under the directory: the `test` declarations of their files of tests (`.maxtest`), compiled with the modules they name and the runtime into one executable whose entry calls each, which is then run (see "Tests") |
| `minimaxon selftest` | run the compiler's own tests: `test self`, from the root of the repository |
| `minimaxon cache [trim\|clear]` | say where the build cache is and how many files and bytes it holds; `trim` deletes what a trim would (the files unused for five days) whether one is due or not, `clear` every file the cache wrote (see "Build cache") |
| `minimaxon rewrite --keyword=old=new <path>...` | rename a keyword by token in the `.maxon` files and the Markdown code under the paths (see "Changing the language") |
| `minimaxon qualify <dir>...` | write the module's path before each bare name of another module's declaration in the projects (see "Changing the language") |
| `minimaxon rename [--prelude=f,...] [--rules=file] <rule>... <program>...` | rename a built-in type or rewrite the calls of a built-in method by the declaration the checker resolved, never by text: `Array.append=push`, `Array.insert=insert({at}, value: {item})`, `Array.pop.unwrap=try {call} otherwise panic("empty")`, and a free function's whole call: `readFile().unwrap=try File.readText({path}) otherwise panic("unread")` (the rules are in `self/driver/rename.maxon`) |
| `minimaxon fmt [--check] <path>...` | put the `.maxon` files and the Maxon code blocks of the `.md` files under the paths in the one layout; `--check` changes nothing and exits 1 listing the files that are not in it (see "Layout" and "Changing the language") |
| `minimaxon interp <file\|dir> [--seed=N] [--frees]` | development only: run under the tracking interpreter, which runs a map, a list, a pool, a frozen value, a cown and a behaviour, and reports a violation of the memory model; `--frees` lists the values freed, in order |
| `minimaxon fuzz [--seed=N] [--count=N] [--schedules=N] [--keep=DIR] [--native] [--dump]` | development only: write random programs, check them and run them under the interpreter (`self/fuzz`); `--native` also runs the executable of each that compiles and compares it with the interpreter (see "Tests") |
| `go run ./cmd/convert [--specs] [--write] [--check] [--out=DIR] [<path>...]` | convert existing Maxon `.maxon` source files to minimaxon syntax and layout, marking unsupported constructs with `// TODO` comments and warnings on stderr; `--check` writes nothing and prints each kind of warning with its count and files, most frequent first (run `minimaxon qualify` after converting a project of several modules); `--specs` converts maxon's spec files, which is how `specs/` is made. A source file's top-level declaration of a name that minimaxon has built in (`internal/builtingen`'s table, which writes `cmd/convert/builtins_gen.go`) is left out, with the comment above it, since the built-in is the declaration every other module uses; a spec test's program keeps one, as a program may declare such a name. A capability word before a type argument (`Array with immutable Key`) is left out (see "Where it differs from Maxon"). It lays out what it converts with the compiler's `fmt`: `minimaxon.exe` in the root of the repository, or the compiler the environment variable `MINIMAXON` names |
| `go run ./cmd/sample [-interval D] [-threads] [-top N] [-callers N] [-focus TEXT,...] [-o FILE] <exe> [arg...]` | profile an executable that `build --emit-map` made: runs it, samples its main thread's instruction pointer and its `rbp` frames (1 ms by default), and prints the functions by self and by total samples, with the callers of the top ones and of those whose symbol contains a `-focus` text, and the wall time; code outside the image (a Windows DLL) is charged to the function that called it, found by scanning the stack, and exits with the program's code |

`build` and `test <module dir>` keep each module's code in the build cache and link the code of a module whose key did not
change from it (see "Build cache"); `--no-cache` compiles without it, and `--verify-cache` (development only) compiles every module
and stops with an internal error at the first whose code or summary differs from the entry the cache holds under its key. The
compiler has no `cache` command and no `--jobs=` or `--with=`: it says so, prints its usage and exits 2, as it does for a command it does not know. `run` and the test
runner (the corpus and the specs) build their executables in the temporary directory under names that hold the process id
(and, for the runners, the test's number in the run), so two tests and two compilers running at once never write the same
file. A runner deletes each test's
executable when it can, tries again for the others when the run ends, and says in one line on standard error how many it could
not, which never changes the exit code.

`specs/` holds maxon's spec tests converted by `cmd/convert --specs`, in maxon's own format: `<!-- test: name -->` and the
fenced blocks ```` ```maxon ````, ```` ```exitcode ````, ```` ```stdout ````, ```` ```stderr ```` and ```` ```maxoncstderr ````. The tool
rewrites only each test's `maxon` block (every `// --- file:` section of it) through the source conversion, and copies
the rest, with line endings made LF. A test whose conversion warned, or whose source minimaxon cannot parse, carries one
`<!-- skip: kind -->` line per kind of warning after its test marker; the runner skips and counts it, and every other test
runs. `--check` over the specs, and the summary at the end of a conversion, give the tests converted cleanly and skipped and
the kinds by tests blocked, most blocking first. Then `fix specs` applies the fixes that minimaxon's language differences
name (a `try`, a cast, `.clone()`, `consumes`, `mutates`, `mutates self`, `var`, the bound `where T implements Copyable` or `Droppable` that a generic body needs, written in the function's header, the module written before a bare name that means one declaration of another module, as `qualify` writes it) to every test that runs a program; a test
that expects maxon's run-time trap where the fix adds a `try` gets a `<!-- skip: language difference: ... -->` line
instead, and so does a test that expects a compile error which minimaxon reports first under another code for a known reason
(the pairs of `self/driver/skips.tsv`, generated into `codeSkipTable`: ownership refused by another rule, the syntax error, operand
types), a test that expects a code whose rule minimaxon changed, whatever it reports (the table's `expect` rows: `E3220`, since a
constant may call a literal factory), and a test that runs a program which minimaxon refuses, after the fixes, by a rule it
changed (its `run` rows: `E3925`, a closure that borrows does not escape), and a test that the table names by its file and
name (its `test` rows), whose program minimaxon runs to another answer by a language change (a float division by 0 is
IEEE's). A test that expects a compile error and is first refused under another code by a fixable language difference
(arithmetic that needs a `try`, an integer cast, a clone, `mutates`, `var`, a bare name of another module) gets the fixes of the errors that are not the
expected one, round by round, until the first error is the expected one, and a `<!-- positions: moved -->` line, since the
fixes move text; if that is never reached, it is skipped when the code left after the fixes and the expected one are a
pair of the table, or when the first code left after the fixes is one of the table's `first` rows (`E3057`: arithmetic
that needs a proof is asked for before what maxon reports), and otherwise left as it was; a program that checks clean after the fixes is never skipped: it gets the fixes and a `<!-- positions: moved -->` line, and the test fails for the diagnostic that minimaxon lacks. A test whose program changes the collection that its `for`
walks gets `<!-- skip: language difference: a loop holds the collection it walks -->`. `specs.ps1` runs both and then the new tests (see "Tests"); `specs/README.md` has the command, and the directory is
never edited by hand, except that the runner writes `specs/passed.txt` (see "Tests").

`--emit-ir` writes the IR of every function beside the executable, as `<out>.ir`, in SSA form as instruction selection gets it, and `--emit-map` the map of its code,
as `<out>.map`: a line per function and routine the link kept, in address order, with its RVA and size in hexadecimal
and its symbol, which `cmd/sample` reads. A profile of `self/` is two commands:
`.\minimaxon.exe build self -o bin\prof.exe --emit-map`, then `go run ./cmd/sample bin\prof.exe check self`.
`--verify-ir` checks the IR of every function with the IR verifier (see "The IR") as lowered and again in SSA form, before code is made, and holds
the register allocation of every function to its invariants (spilling's to `verifyPressure`, colouring's to `checkColouring`), and stops the compile
with an internal error naming the rules the first broken function breaks. `--timings` (on `build`, `run` and `check`) prints on standard error, once the stages have run, a table with one line for
each stage of the task graph: the wall time its tasks took, in milliseconds, and its share of the total, and the five modules whose
tasks took longest; a `build` prints it for the runtime's compile, which every build does first, and for the program's. A
`build` or `check` runs its tasks on every core, so the stages' times add up to the work the compile did, which is more than
its wall time. The clock is read twice per task and only when the flag is on.
Beneath the stages, a compile that emitted code splits the emit stage by phase (`backend.EmitPhase`): selection, the blocks'
layout, and per spilling round the graph, liveness and pressure, then spilling's sweeps and edits, colouring, the rewrite out of
SSA, `--verify-ir`'s checks and encoding, each by its share of the stage; the clock is read at each boundary of a phase.
`--trace=<file>` (on `build` and `check`) writes the timeline of the compile's tasks to the file in Chrome's trace format
(`chrome://tracing`, Perfetto; `self/driver/trace.maxon`): a span for what ran before the tasks (the runtime's compile and the
discovery), one for each task that ran, named by its stage and module, with its number in the graph and the tasks it waited on
in its arguments, and the idle time between them. A behaviour cannot tell which worker runs it, so the spans are laid on lanes,
each on the lowest lane free when it starts: as many lanes as tasks ran at once, never more than the workers, and a gap on a
lane is a worker idle. The clock is read twice per task and the spans kept only when the flag is on. `go run ./cmd/sample
-threads` samples every thread that ran since its last sample, rather than the main thread only.
`--count-allocations` builds an executable that counts its live heap boxes (see "Counting allocations"); it is for
development, and `minimaxon test` always uses it. `--reverse-modules` (development only) runs the modules of each stage
in another order that still puts each after the modules it names: the modules that name nothing of each other in the
reverse of their usual order. The executable must be byte for byte the one a plain build makes, since nothing a module
makes depends on when it runs. `--verify-reads` (development only) names the task, in the internal error, that reads a type or
function entry its view of the project does not hold (each task of a module always runs in a view that holds only the
products of the stages it waited on, as an executor of behaviours gives it); the output must be the same as without it
(`check.ps1` checks `self` with it). `--update-positions` makes `test` rewrite the `at L:C` of an
`expect-error` whose code was reported at another place, and count the test as passed.

### Targets

`--target=<arch>-<os>` (on `build`, `run`, `check` and `test`) names what a compile is for, as maxon spells it: `x64-windows`, a PE
executable that imports kernel32, or `x64-linux`, a static ELF executable (`ET_EXEC` at maxon's fixed load address,
0x400000, with no interpreter, no dynamic section and no relocation) that calls the kernel itself. A name of no target is refused
with the list of the targets and exit code 1, as maxon's is. The default is the system the compiler runs on (`hostTarget`, a
`#if` of `self/driver/targets.maxon`), and a `build` with no `-o` names its executable with the target's extension, `.exe` or
none. The target picks the branches of every `#if` of the compile, the runtime's among them (see "The runtime"), and is part of
the key of every module in the build cache. Code generation is the same for both: one IR, one x64 selection, one internal
convention; what differs is stated in tables (`internal/targetgen`'s systems, `internal/abigen`'s imports and Linux
numbers) and behind one choice per place, by `ast.TargetSystem`: the entry stub (`entry.maxon`, `entry_linux.maxon`), the
writer of the executable (`buildImage`: `pe.maxon` or `elf.maxon`), the read of a thread's context (`x64.rules`) and the way
the runtime reaches the system (`import` or `syscall`, each refused by the backend on the other system). `minimaxon test
--target=x64-linux` on Windows compiles each test for Linux and runs its executable through WSL (`wsl.exe --exec` and the
executable's path under `/mnt/`, in the default distribution), as maxon's `targets.tsv` names a runner for each target; the
executables' count of workers reaches WSL through `WSLENV=MAXON_MAX_PROCS`, which the caller sets. A program that the
target cannot run says so with `// unsupported-targets: x64-linux` (maxon's directive of a spec test), and a run for that target
prints it as `SKIP` and counts it as not supported; `os-io`, `subprocess`, `file-permission` and `conditional-compilation` are
Windows' and `os-io-on-linux`, `subprocess-on-linux` and `file-permission-on-linux` Linux's.
The compiler cross-compiles itself: `build self --target=x64-linux` makes a Linux compiler, which checks and builds programs
under WSL, and builds itself there to the same bytes. On Linux a compiler finds `runtime/` as on Windows and joins paths
with `/` (`nativeSeparator` of `self/project`); its build cache is under `MINIMAXON_CACHE` only, since there is no
`LOCALAPPDATA`, and `run` and `test` still put their executables under `TEMP`.

### Tests

`.\check.ps1` is the gate. In order, it formats, `go fix`es and vets the Go of the generators and tools; runs the
generators and fails if a generated file (`*_gen.*`) changed; has the seed build `bin\minimaxon.exe`; and then runs
`suites.ps1` on it, which runs the Go tests of the generators and tools (`go test ./...`, with that compiler as
`cmd/convert`'s formatter) and, with the compiler: `selftest`, `check runtime` for each target, `check self --tests` (the compiler's own source and its files of tests under the rules it has just been given), `fmt --check self runtime tests`, the corpus (`test`, with no standard
input), the programs that schedule behaviours again under one worker and under two (`test --behaviours` with
`MAXON_MAX_PROCS` set), the same corpus and the same two reruns for the `x64-linux` target (`test --target=x64-linux`, the
executables run under WSL, to which `MAXON_MAX_PROCS` is passed by `WSLENV`; a machine with no WSL or no distribution fails these
steps with what it lacks, and never skips them), the build cache (a project whose main inlines a leaf of the module it names, built cold, warm and after
an edit of the leaf that the order mispredicts, on every worker and on one, each executable held to the uncached build of the same
source, and `--verify-cache`), every spec test (`test specs --require-passed`, which fails when a test that `specs/passed.txt` lists does not pass, and
counts the new passes), and `fuzz` (300 generated programs, each one the checker accepts run clean under the interpreter). It stops at the
first step that fails. That the compiler reproduces itself (the seed builds C1, C1 builds C2, C2 builds C3, and C3 is
byte-identical to C2) is checked where the seed is renewed, `.\seed.ps1`, which then runs the same `suites.ps1` on C2.

There are three kinds of test. **Corpus tests** run whole programs through `minimaxon test`. **The compiler's own
tests** belong to the modules they test, so a change to one module is tested by compiling that module, the modules it names
and the runtime, never the whole compiler (`test self/table` compiles `table` and the runtime). A module's tests may use only
what the module may name; a test that needs a module above its own is a layering fault, kept in the lowest module that may name
all it needs: the tests of the IR verifier on hand-built IR (`self/interp/verify.maxtest`) and of SSA construction and the fold
pass (`self/interp/ssa.maxtest`, `fold.maxtest`) run their IR on the interpreter and through the backend, so they are
`interp`'s; the declare's and the bodies' tests (`self/driver/declare.maxtest`, `bodies.maxtest`) and the chain of 200000
terms (`self/driver/codegen.maxtest`) compile source through the front end, so they are the driver's, as are its timings.
Helpers that the tests of several modules use have one home, the lowest module that may name all they need
(`self/table/testing.maxon`, `self/ir/testing.maxon`, `self/backend/testing.maxon`), since a module's tests may name only
what the module may; they are exported source of that module, which the linker leaves out of every executable that does not
call them. The tests (`test` declarations in each module's `.maxtest` files, run by `minimaxon test self/<module>` or all of them by `minimaxon selftest`) test the compiler's parts on programs and IR built by hand. **Go tests**
test the generators (each regenerates its outputs and fails if the checked-in ones differ; `internal/x64gen` proves the
instruction table against the bytes in its `testdata`) and `cmd/convert`.

Each `tests/**/*.maxon` is one program, except under `tests/modules/`, where each directory is a project and its files,
at any depth, carry the directives. The runner (`self/driver/test.maxon`) compiles every program to an executable, a program that
must fail included, because some errors come after type checking (lowering refuses what has no native code yet), in
the same process; the runtime is loaded, checked and compiled once per run and frozen, and each test is added to a copy of
that project and linked with its code, which every compile only reads (`SharedRuntime` in `self/driver/compile.maxon`; the
link lays the frozen units out between the program's own, `backend.LinkUnits`). The corpus and the spec tests run as
behaviours, on every core (`self/driver/runner.maxon`): each test is compiled on a cown of its own, one execute of the task
graph per test, and runs its executable there; the board, a cown, keeps a slot for each test's result, starts tests while
fewer than twice the workers are under way, lets at most a cap of executables run at once (half the workers, at least 1 and
at most 8, or `--executables=N`; a worker waits while the executable it started runs, so the cap keeps the others
compiling, and the machine's load bounded, since each executable may start a worker per processor), and takes the results in, in the
order of the tests, as soon as every one before them is in: the lines print as the run goes and are, with the summary, the
same at any number of workers (`MAXON_MAX_PROCS`, which the executables inherit). A program under `tests/check/` is only
compiled: one that expects no error must compile clean (a program that only the interpreter can run counts as clean),
and none is run. Any other program that compiles is run twice, as the executable and under the tracking interpreter
(see "The interpreter"), from the IR that the native build lowered, and both must give the stdout and the exit code the
directives say; the interpreter must also find no violation of the memory model, and the executable, built with
`--count-allocations`, must end with no heap box live (a program that traps skips that check). A program that uses a
map, a list or a pool has no executable yet: it fails to compile with `E9900`, and the runner then runs it under the
interpreter alone. A program that schedules behaviours runs under 20 schedules of the interpreter
(seeds 0 to 19), and every one must give the same stdout and exit code and no violation, as must the executable, whose
workers schedule it as the machine does, so the directives of such a program must say an output that does not depend
on the schedule: print only from behaviours that the cowns put in order, such as one chain, or behaviours on one cown.
A program that schedules so many behaviours that 20 schedules of the interpreter would cost seconds says how many it
runs under (`// schedules: N`).

A program that runs the scheduler runs natively more than once, each run another schedule, and every run must give
what the directives say: the runner knows it does from the link, which gives the scheduler only to a program whose code
reaches it (see "Main with or without the scheduler"), a test of the runtime that runs it by hand among them. It runs 2 times in a
run of the corpus, or N with `--repeat=N`, under the worker count of the runner's environment, which every executable
it starts inherits (`MAXON_MAX_PROCS`, else the machine's processors). `--behaviours` runs only those programs, and
only natively, which is how a program meets other worker counts: `suites.ps1` runs the corpus with the machine's count,
which holds every program to the interpreter too, and then `test --behaviours` with `MAXON_MAX_PROCS` at 1 and at 2,
so the gate runs each such program 6 times natively under three counts, for about two seconds; it compiles only the
programs that have one of the words `when`, `async`, `sleep` and `Scheduler` in a file, or are tests of the runtime, and
leaves out the rest uncounted. A soak
is the same flags with `--repeat=300` at each count. A failure names the run and the worker count. The stress
programs are `behaviours-over-a-ring-of-cowns` and `behaviours-on-one-to-eight-cowns-and-pushers-handing-off` (1 to 8
cowns per `when` and per-sender order across hand-offs, after maxon's `cown-multi-torture` and
`cown-handoff-torture`); the others named `a-…` and `behaviours-…` each hold one path of the runtime to it (a function
value or a frozen value shared into behaviours on many workers, a cown's last owner dropped inside a behaviour while
others hold it, a generic function's nested `when`, behaviours scheduled from behaviours to a depth, `Process.setExitCode`
ordered by a cown).

A program under `tests/library/` is staged in the library tier, as maxon's spec runner stages a case marked `tier: stdlib`,
so it may go through the doors of maxon's raw surface (see "Modules"), the buffer cursor of "Borrowing values" among them;
it is run as any other program is, natively and under the interpreter.

A program under `tests/runtime/` is a test of the runtime: it is
compiled as one more file of the runtime's module (`compileRuntimeTest`), so it may call the runtime's functions and the
`__Raw` intrinsics, and holds `main`; it is run natively only, since the interpreter does not run the runtime's source.
`tests/runtime/scheduler.maxon` tests the scheduler alone: it builds behaviour records by hand. A test executable runs under a 10 second deadline, and a program
that outlives it fails the test with `timed out`. The summary is `N passed, M failed, K not yet supported`, the
failures are listed, and the exit code is 0 when nothing failed. With `--verbose` a `RUN` line comes when a test starts, so a crash of the compiler, which ends the run, names the tests that were under way; those lines fall between the others as the timing does.

`minimaxon test <specs-dir|file.md> [--verbose] [--skip-passed] [--record-passed]` runs maxon's spec tests of `specs/` (`self/driver/spec.maxon`; the format is that of maxon's `docs/SPECS.md`, see `specs/README.md`) in the same process, as behaviours, as the corpus runs. A test is compiled from memory (a multi-file test as a project of files held in memory, `project.loadFilesInto`) and run once, from an executable of its own in the temporary directory. A compile-error test compares the errors one for one by code and place, after the fragment's path is made `<fragment>` (and, for a multi-file test, its lines the merged block's): the messages, the notes and the lines that continue a message are left out (see "Where it differs from Maxon"), an error that maxon reports at no place matches its code at any place, and the line and column are compared only when the test has no `<!-- positions: moved -->` line; a run test compares the exit code when a block gives one and always stdout and stderr, trimmed (a block that is not there asserts the stream is empty), as maxon's runner does. It prints one line for each failed test (`FAIL file: test: what differed`), the counts, and the skipped tests by reason, the most first; `--verbose` also prints a line for each test (a `RUN` line when it starts and its result in order, so a crash names the tests under way) and every reason. A test is skipped, with a reason it counts, when its source has a `<!-- skip: kind -->` line (the conversion's), when a directive needs what the runner or the compiler lacks (a `tier` other than `stdlib`, `procs`, `preempt`, `process`, `runs`, `network`, `stdin`, `Args`, `env`, the trace and debug directives, `unsupported-targets` naming `x64-windows`), when a result block is a trace or is for another target, when its source stages an overlay of maxon's stdlib or runtime, and when only the interpreter runs the program. `specs/passed.txt` lists the tests that have passed (one `file.md: test name` per line, sorted). `--require-passed` runs every test and fails the run when a listed one does not pass, printing `LOST file.md: test: what` for it (a failure, or `skipped: reason`) in place of its `FAIL` line, the count of them at the end, and `NEW file.md: test` for a test that passes and is not listed. `--skip-passed` leaves out, without compiling them, the listed tests (skipped under the reason `passed before (specs/passed.txt)`), runs the others, and prints `NEW` lines too; it cannot be given with `--require-passed`. A missing list is an error for both. `--record-passed` writes the list, and starts it when there is none: the tests that passed in the run, and the listed tests that the run left out, failed or skipped for a reason of the runner, so a regression stays listed and goes on failing the gate; a listed test that the conversion now skips (a `<!-- skip: -->` line, which a language change or the conversion put there) is dropped, with a `DROPPED file.md: test: reason` line. `check.ps1` runs `--require-passed`, and `specs.ps1` ends with a full run that records; both run it through `spec-run.ps1`, from `.spec-run-<pid>`, a directory directly under the checkout, which maxon's runner runs every test from: a test's relative paths are written against it (`../stdlib` is the checkout's library), and a compiled test inherits the runner's working directory. `test specs` alone reads and writes nothing. The compiled program inherits the runner's standard input. A test marked `<!-- tier: stdlib -->` is compiled with its modules in the library tier (`ast.Module.stagedInLibrary`), as maxon's runner compiles it as a file of `stdlib/`: it may name maxon's raw surface (see "Modules").

`tests/bench/` holds the benchmarks: a counted loop, nested loops over an array, string building, records and unions, and a
recursive function, each deterministic and ending in the checksum it prints (its `//>` line). The corpus runner leaves
the directory out, since the programs run long enough to time. Each times its work with `Clock.nowNanos` and prints it last,
as a `work ns` line. `.\bench.ps1 [-Compiler bin\minimaxon.exe] [-Target x64-windows|x64-linux] [-Runs 7]` builds each with
the compiler and prints a table of the executable's code size (`.text` bytes, read from the PE section header, or the ELF
code segment, by `textsize.ps1`), the best time of its work and of its process over the runs, each with the spread of the
runs, and reports a program whose output is not its checksum; it is how a change to the code generator is measured. The
process is started with `CreateProcessW`, its output going to a file, and for `x64-linux` from a copy in WSL's `/tmp` by a
shell there, so neither PowerShell nor the start of WSL is in the time. `.\selftime.ps1` records the `.text` bytes of the compiler it built in `docs/self-compile-times.md`.

Directives in comments say what the program must do:

| Directive | Meaning |
|---|---|
| `//> text` | one expected line of stdout, in order (none means stdout must be empty) |
| `// expect-exit: N` | expected exit code (default 0; a panic exits 1) |
| `// expect-error: E3102` | compiling must fail with this code; `E3102 at 7:5` also pins the line and column. A program may have several, and each must be reported (`E9900`, a program that needs something that has no native code yet, is the one error that is not a failure in a program that expects none) |
| `// expect-free: label` | the interpreter must free the program's values in the order of these lines, one value each, as `minimaxon interp --frees` prints them (`string "a1"`, `array`, `record`, `pool`) |
| `// memory-limit: N` | the native run may use at most N megabytes; the runner does not enforce it yet |
| `// native-only` | do not run the program under the interpreter; for a program whose point is what the executable does with memory, which the interpreter runs far too slowly |
| `// schedules: N` | run the program under N schedules of the interpreter (seeds 0 to N - 1) instead of 20; for a program of thousands of behaviours |
| `// expect-scheduler: used` or `none` | the link must give the program the scheduler, or must not (see "Main with or without the scheduler"); what a program does cannot show it |

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
that found no error there. Each stage of a module is a task that waits only for the module's stage before it and for the
stages of the modules it names (see "The modules phase"); the deferral is what lets a module go on to its next stage as soon as
the modules it names have finished theirs, which is what parallel compilation, planned, will use.

## Modules

A module is a directory. The project root is the root module, and every directory under it that holds source, at any
depth, is a module of its own. `main` may be declared in any module: a project has one, in whatever directory, and a
second `main` anywhere is `E3006`, reported at the later one in the byte order of the files' paths, as maxon reports two. The program's link name for it
is that of its module (`app::main`), and a project with no `main` in any module is `E3001`.

- **The path given to `build`, `run` or `check` is the project's root**, as maxon's: only what lies beneath it is compiled, and
  no project file above it changes that. A `.maxproj` file is only a marker for now: minimaxon does not compile or run it as a
  program, and it has no `Build` library or targets, so `build` with no path is still a usage error. A directory of the walk,
  the root included, that holds a `.maxproj` file is `E2074` when the directory above it, or one higher, holds another
  `.maxproj` file, whichever of the two projects is built; it is reported at the nested file with maxon's message. A directory
  that holds a `.maxonignore` file is left out of the walk, with everything below it, unless it is the root of the compile. A
  project held in memory (the spec runner) has no `.maxproj`: its files are the project. The compiler finds `runtime/` and
  `stdlib/` from its own executable, so none of this changes where they are looked for.

- **Another module's names are always qualified**, by the module's path read from the root: `geometry.area()`,
  `7 as geometry.Length`, `net.http.parse()`, `geometry.Shape.circle(2)`. The root module's own declarations are
  `export.X` (`export` is a name only as the start of a qualified name), and `stdlib.X` is a built-in.
- **A bare name** is a local, a field, a type parameter, the module's own declaration, a built-in or, last, a public
  declaration of the standard library's root (below), and nothing else, so adding a declaration to one module never changes
  how another resolves, and resolving a module reads only the declarations of the modules it names. A bare name that only
  another module declares is unknown (`E3004` for a call, `E3011` for a type, `E2004` for a value), and the message names
  the qualified spellings.
- **The standard library** is Maxon source in `stdlib/`, found as `runtime/` is (the `stdlib` directory of the nearest
  directory, from the compiler's executable's upward, that has one), converted from maxon's `stdlib/` by `cmd/convert`:
  `stdlib/files.txt` lists the files converted so far, each with the library module it goes to (its directory under
  `stdlib/`, `.` for the root), and `.\stdlib.ps1` regenerates them, then applies the compiler's `fix` to them as `specs.ps1`
  does to the spec tests (a `let` that is changed becomes a `var`, a name of another library module is qualified); none is
  edited here. The modules are layers, as a program's are: maxon's library names any of its declarations from any file, so
  the list puts files that name each other in one module and each other module above the ones it names. Every
  program is compiled with it (`project.addLibrary`): its directories are modules after the program's, flagged `isLibrary`,
  the root's path being `stdlib` and each one under it `stdlib.<path>`, so no program module shares one, and
  `Project.library` names the root. A program names the public declarations of every library module bare
  (`Unicode.isWhitespace(32)`), after its own names and the built-ins, so the layering is the library's own; a public name
  is declared by one library module only (`E3006` at the later one). A library module names the library's other modules
  qualified from the library's root (`internal.hashtable.X`, `export.X` for the root's), as any module names another, and its `export` reaches the tree rooted at its parent within the
  library, which for the root and a top-level module is the whole library and never a program. A program sees nothing of
  the library short of `public`, and nothing at all of a module under `stdlib/internal/`, where maxon's `stdlib/helpers/`
  files go (`stdlib/files.txt` maps each to its module; one that the root names and that names the root, `Hash.maxon`,
  is a file of the root): the internal rule below holds for the library as for any project,
  so only the library, and a program the spec runner stages in it, names them. Only the library modules a program names, directly or through others, are compiled past
  their resolve (`reachedModules`), so a program pays for no other; each is a module of the build cache like any other,
  with its own key, and is compiled for the compile's target. A project that is the runtime's source or `stdlib/` itself is
  compiled without a second library, and the compiler's own source, `self/` (found as `runtime/` is,
  `project.isCompilerDirectory`), without any: it names nothing of the library, so its build never reads `stdlib/` and a
  library file may use syntax that the compiler building `self/` (the seed) does not parse once that compiler has this rule;
  a seed from before it still parses every library module when it builds `self/`. A compile of `stdlib/` compiles every
  module of it, which is how the gate checks the library.
- **The raw surface is the library tiers'.** Maxon's library is written on a raw surface that reads and writes memory past the
  ownership model's bookkeeping: the static functions of `__Builtins`, the type `__ManagedMemory` and its methods, and an
  array's `.managed` (see "Primitives"). Only a module of the runtime or of the library, or a program the spec runner stages
  in the library, may go through one of these doors; anywhere else each is `E3185` at the door, in maxon's words, with
  what a program uses instead (`Process.id()` for `__Builtins.currentProcessId()`, the `Array` methods for `.managed`),
  as a table of `internal/builtingen` says. A refused type stops the declaration that names it, so its body reports nothing
  more of it. `__Raw` is narrower still: the runtime's alone (`E3152`).
- **Visibility has three levels**, for a declaration, a field or a method:
  - no modifier: the declaring module only;
  - `export`: the tree rooted at the declaring module's parent, as Go's `internal/`. An `export` in `app/net/http` is
    visible to `app/net` and every module under it (`app/net/tcp` too), not to `app` or `app/ui`. In a top-level
    module or the root module that tree is the whole project. It never reaches past the project;
  - `public`: everyone, programs that use the project as a library included.

  Naming a declaration from a module its visibility does not reach is `E3008` (`E3014` for a field), and the message
  says which modifier would reach.
- **A directory named `internal` scopes a whole module**, as Go's `internal/` does: a module whose path has a directory
  named `internal` may be named only by the modules of the tree rooted at that directory's parent (the last such
  directory, if there are several). `app/net/internal/wire` is named from `app/net`, `app/net/http` and anything under
  `app/net`, not from `app` or `app/ui`; a top-level `internal/x` from the whole project. Naming it from elsewhere, by a
  qualified name or by a library name that would otherwise be bare, is `E3008`, whatever its declarations' visibility,
  and the message names the tree that may. `export` scopes one declaration and `internal/` every declaration of the
  module: a `public` declaration of an internal module is visible wherever the module may be named, and never becomes a
  bare library name to a program. A value of an internal module's type that a function outside it returns is used as any
  value is; only naming the module is refused.
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
| `table` | `StringMap` and `IntMap` (open addressing) and `Option`, which nothing built in provides; `Digest`, `Hasher`, `ByteWriter` and `ByteReader` (`digest.maxon`), which the build cache names its entries by and stores them with, and `CacheWriter` and `CacheReader` (`cache.maxon`), which the generated functions store a module's checked state with | — |
| `ast` | the program model: `Tree`, a module's nodes as flat `Record`s, the generated node kinds, accessors and declaration structs, `Module` and `Project`, the project's `TypeTable` and `FuncTable` of per-module slots and the accessors every read of an entry goes through (`typeEntry`, `funcEntry`), the `ModuleState` a module's task writes, which holds its module's slots (`state.maxon`), the built-in types and functions (generated), `Resolution` and `Typing`, the per-module tables the phases fill in (see "The AST"), link names, locating an error, `I128` | `diag`, `table` |
| `syntax` | the grammar (`Maxon.g4`), the generated tokens and parser, the lexer loop, the layout rules, the formatter (the parser in a mode that writes the layout instead of checking it) and the edits that rename a keyword | `ast`, `diag`, `table` |
| `project` | loading a project: discovery, finding the runtime's source (see "The runtime") and the standard library's (see "Modules"), reading and parsing each module, the declaration tables, the module graph and its import-cycle check | `ast`, `diag`, `syntax`, `table` |
| `resolve` | name resolution, into the module's `Resolution`: which declaration each name and type reference names, which type parameter each constraint binds, which modules the module depends on, visibility | `ast`, `diag`, `table` |
| `check` | the stages of the type checker (`stages.maxon`), the generic bounds, the range analysis that decides what arithmetic can fail, the ownership pass, and what a behaviour may capture, into the module's `Typing` | `ast`, `diag`, `table` |
| `ir` | the IR (fixed-size records, with the printer and the visitors generated from the IR table), the graph algorithms (reverse postorder, dominators, natural loops) that its flow and the machine IR share, what its arithmetic computes (`evaluate_gen.maxon`, generated from the IR table: `evalBin`, `evalCmp`, `evalGuard` and the rest, one per operation that computes a word from its operands, which the interpreter and the fold pass both call; `evaluate.maxon` has the algorithms the table names: the 64-bit wraps, the wide product, the shifts, the overflow tests and the float conversions), its builder, finding the generic instances a module needs, lowering (the primitives' operations, where each value is dropped, the body of each behaviour), which reads the `Typing` of every module it follows a call, a constant or a field's default into, and the runtime's symbol names | `ast`, `diag`, `table` |
| `opt` | the optimizer: leaf inlining (`Leaves`, `Inliner`, `self/opt/inline.maxon`), its analyses (`Analysis`: over `ir.Flow`, the dominator tree, dominance frontiers, loop nesting depth and use counts per register), the rewrite rules' matcher generated from `internal/rulegen/generic.rules` (`rewriteInstr`, reading the definitions of `ir.Defs`) and the helpers the rules call; SSA construction (`Ssa`, `self/opt/ssa.maxon`: the slots nothing takes the address of become registers and block parameters) and the fold pass (`Fold`, `self/opt/fold.maxon`), which `optimizeModule` (`self/opt/optimizer.maxon`) runs in that order on every function a native build compiles; after leaf inlining, loop rotation (`Rotator`, `rotate.maxon`), loop-invariant code motion (`Licm`, `licm.maxon`) and common subexpression elimination (`Cse`, `cse.maxon`), over what may be merged or moved of each operation (`motionOf`, `motion.maxon`), the loops' facts (`LoopFacts`, `loops.maxon`) and the edits they share (`Editor`, `edit.maxon`) | `ast`, `ir`, `table` |
| `backend` | the x64 encoder (generated), the assembler and code unit, instruction selection (`isel.maxon`: the module's functions into machine IR by the rules of `internal/rulegen/x64.rules`, generated into `lower_x64_gen.maxon`, and the helpers they call, in `isel.maxon` and `rules.maxon`), the machine IR (`mir.maxon`), x64's register file, conventions and instruction constraints (`regs_x64.maxon`, over the target table's `regs_x64_gen.maxon`), liveness, register pressure and spilling with the E5001 test (`liveness.maxon`, `pressure.maxon`, `spill.maxon`), block parameters in memory (`params_memory.maxon`), colouring, SSA destruction and the colouring's checker (`regalloc.maxon`, `ssa_destruction.maxon`, `colour_check.maxon`) and the machine IR's encoder (`encode.maxon`), the drop and copy routine of each type, the entry stub of each system (`entry.maxon`, `entry_linux.maxon`) and the two ways it starts main (`start.maxon`), the linker, which decides between them by what the program reaches, and the writers of the executable's formats behind one image (`image.maxon`: the PE writer `pe.maxon` and the ELF writer `elf.maxon`) | `ast`, `diag`, `ir`, `table` |
| `interp` | the tracking interpreter: runs the IR of a whole program with the primitives written in Maxon and a simulated scheduler of behaviours, and reports a violation of the memory model | `ast`, `ir`, `table` |
| `fuzz` | the random program generator the `fuzz` command runs | `table` |
| `driver` | the commands (`driver.maxon` is the command line); `compile.maxon` runs the pipeline as the task graph of `graph.maxon`; `cache.maxon` is the build cache, `unittests.maxon` the compile of a module's tests; the corpus and spec runners, `interp`, `fuzz`, `fmt`, `rewrite`, `qualify`, `rename` and `update_positions` | all of the above |

`main.maxon`, in the root module, runs `driver.run`. Each module's tests are its own, in its `.maxtest` files (see "Tests"). The generators are Go, in `internal/`, and `cmd/convert` and `cmd/sample` are standalone tools (see
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
| `internal/builtingen` | `internal/builtingen/table.go` | `self/ast/builtins_gen.maxon`, `self/ast/builtins_record_gen.maxon` (the `Builtins` record, its `empty()` and `isCapability`: a field for each type row, function handle and primitive of the table, and the few fields `extraFields` lists), `self/ir/runtime_gen.maxon`, `self/ir/intrinsics_gen.maxon`, `self/check/builtins_gen.maxon`, `self/interp/builtins_gen.maxon`, and the top-level names of the built-ins, which `cmd/convert` leaves out of a converted library file (`cmd/convert/builtins_gen.go`) |
| `internal/diaggen` | `self/diag/codes.tsv`, `self/diag/messages.tsv`, `self/driver/skips.tsv` (the code pairs and the tests `fix specs` skips; it names codes of the first) | `self/diag/codes_gen.maxon`, `self/diag/messages_gen.maxon`, `self/driver/skips_gen.maxon` |
| `internal/irgen` | `internal/irgen/irtable/table.go` and `semantics.go` | `self/ir/ir_gen.maxon`, `self/ir/evaluate_gen.maxon` |
| `internal/x64gen` | `internal/x64gen/x64table/table.go` | `self/backend/x64_gen.maxon` |
| `internal/targetgen` | `internal/targetgen/target/target.go`: each processor's register file (names, encodings, classes), reserved registers and the internal calling convention, and the conventions of each system it runs (a `System`: its host convention, and the system call convention of one whose programs call the kernel themselves): argument registers in order, result, error flag, a system call's number, callee-saved and caller-saved, checked to split the register file; `x64table` takes its registers from it | `self/backend/regs_x64_gen.maxon` (`x64Reserved`, `x64InternalArg`, `x64InternalResult`, `x64InternalError`, `x64InternalCalleeSaved`, the `x64WindowsHost...` ones for Win64, the `x64LinuxHost...` ones for System V and the `x64LinuxSyscall...` ones for a Linux system call, `x64StackAlignment`) |
| `internal/rulegen` | `internal/rulegen/generic.rules`, `internal/rulegen/pressure.rules` and `internal/rulegen/x64.rules`, checked against the IR table and the x64 rules against the instruction table | `self/opt/rewrite_gen.maxon`, `self/backend/lower_x64_gen.maxon` |
| `internal/abigen` | `internal/abigen/table.go`, `internal/abigen/linux.go` (the Linux x64 kernel's system call numbers, flags and record layouts that the runtime's Linux source and the Linux entry stub pass), `internal/abigen/imports.go` (the kernel32 functions of the import table, in slot order; a test checks that every name the runtime and the entry stub call is listed) | `runtime/layout_gen.maxon`, `self/ir/layout_gen.maxon`, `self/backend/layout_gen.maxon`, `self/backend/imports_gen.maxon` (`rtDll`, a slot constant `imp<Name>` for each import, `rtImports`) |
| `internal/textgen` | `internal/textgen/ucd/*.txt` (Unicode 16.0.0) | `runtime/unicode_gen.maxon`, `self/interp/unicode_gen.maxon` |
| `internal/cachegen` | the `type`, `union`, `enum` and `typealias` declarations of `table`, `ast`, `ir`, `opt` and `check` themselves, from the types the build cache stores of a module's checked state (`ast.TypeSlot`, `FuncSlot`, `ModuleTables`, `Tree`, `Typing`, `ir.IrFunction`, `opt.Leaves`, `check.ModuleChecked`) through every type their fields reach; a field of a type it cannot store fails generation, naming it | `self/<module>/cache_gen.maxon` of each of those modules: `store<T>` and, for a record, a `cacheLoad` that an extension adds (only a record's own methods make one), for a union or an array `load<T>` |

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

**The IR** has one table, `internal/irgen/irtable/table.go`: a row per instruction and terminator with its operands (each a name, a
kind such as register, slot, block, flag or pool index, and the field of the record that holds it), what it gives (a register,
an optional one or nothing), its effects (reads or writes memory, calls, allocates, frees, leaves the function; one with a
failure block may also fail) and the template the printer writes it by, plus the small enums (`BinOp`, `CmpOp`, `GuardKind`,
`RawWidth`) and the flags. A row also says what the operation computes from its operands' words (`Eval`, in the closed forms of `irtable/semantics.go`): nothing (`noEval`: memory, calls, addresses, control), a hand-written function (`call`), or what the case of its enum operand says, each case stating its own (wrapping with the functions that say whether the exact result fits, a quotient or a remainder, a bit operation, a shift, the orders a comparison holds for, a function of doubles, a range of a bound); a row that says nothing fails `go generate`, and a case of an enum that an operation computes by without semantics does too. The generator writes `self/ir/evaluate_gen.maxon` from it, and `self/ir/ir_gen.maxon`: the `Opcode` and `TermKind` enums, the `Instr`
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

**The x64 encoder** is generated from an instruction table, `internal/x64gen/x64table/table.go`. A row is a form, one instruction with
one shape of operands, named as maxon's `X64Backend.maxon` names its encoders (`MovRegReg`, `LoadRegBaseDisp`, `PushReg`):

| Column | Holds |
|---|---|
| `name`, `mnemonic` | the encoder is `encode<name>`, and the case of the `X64Form` enum is the name with its first letter lowered |
| `operands` | in assembly order, each a register, a memory operand `[base + disp]` or, through a SIB byte, `[base + index*scale + disp]` (an element of a buffer: a word at `index*8`, a byte at `index`), a `[rip + disp]` operand, an immediate (`imm8`, `imm32`, `imm32U`, `imm64`), a `rel32` or a condition code, and the part of the encoding that holds it: the reg or r/m field of the ModRM byte, or the low three bits of the opcode (`+r`) |
| `prefix`, `rexW`, `opcode` | the mandatory prefix (`rep`), REX.W, and the opcode with its `0F` escape; a condition code is added to the last opcode byte |
| `modrm`, `digit` | no ModRM byte, one the operands fill, or one whose reg field is the form's `/digit` |
| `setsFlags`, `readsFlags`, `readsMemory`, `writesMemory` | what instruction selection asks of a form; written as `X64Form` predicates of the same names |
| `access` of each register operand | read, written, or both (a two-address form, whose definition reuses the operand's register); the base and the index of a memory operand are read |
| `fixedUses`, `fixedDefs`, `clobbers` | the registers a form reads, writes a result to, or leaves holding nothing, that no operand names (`idiv`: rdx:rax in, rax and rdx out; `mul` and `imul` of one operand: rax in, rdx:rax out; a shift by `cl`: rcx; `rep movsb`: rsi, rdi and rcx) |
| `vector` of a register operand | the register is an xmm one: the SSE2 forms name xmm registers in their ModRM fields as the general forms name general ones, the encoder takes one as the general register of its encoding number, and `vectorOperands` says which of a form's machine operands are xmm registers |
| `call`, `ret` | a call clobbers what its calling convention does not keep, and a return reads the convention's result registers |

A register that is used as an 8-bit register is marked, and its encoder writes a REX prefix whenever the register is `rsp` or
above in number, since `spl` to `dil` exist only under one. The displacement of a memory operand is none, a byte or a
doubleword (a base of `rbp` or `r13` takes at least a byte; one of `rsp` or `r12` takes a SIB byte), and `rip` and `rel32`
operands take a displacement that the backend writes as 0 and patches (`lastField32`, `rel32To`, `patch32`, `patchRel32`). A
choice picks the first of several forms whose immediate holds the value (`AddRegImm`: a byte, else a doubleword;
`MovRegImm`: an unsigned doubleword, a sign-extended doubleword, else a quadword), and a sequence is forms the backend always
emits together (`SetccRax`). The generated file holds the register and condition code enums (`X64Register`, `X64CondCode`),
the immediate types, the helpers the encoders share and one encoder per form, choice and sequence, taking a `mutates code
ByteArray` first. `go test ./internal/x64gen/...` also encodes every form with registers `rax` to `r15`, displacements from none
to 32 bits, bases of `rsp`, `rbp`, `r12` and `r13`, immediates at the limits of each kind and every condition code, with a
reference encoder in the test, and compares the bytes with `internal/x64gen/x64table/testdata/assembler.golden`; `selftest` runs the
generated Maxon on a set of those encodings (in `self/backend/pe.maxtest`) and compares them with the same bytes. A form added after
that assembler was removed (the indexed and atomic forms, a word load and store at a base plus an index, `or` and `xor`
with an immediate, the three-address `imul` with an
immediate, the shifts by a constant, `cmp` of a register with memory, the 32-bit zeroing `xor`, `leave`) is compared with
encodings written from the Intel manual and read back with LLVM's disassembler (`literal` in the test); a rule picks among
its immediate sizes by a condition (`fitsImm8`), since a choice is compared with the golden file at every value.

The allocation columns are what the machine IR asks of a form (see "The machine IR"), and the generator refuses a table in
which they contradict the operands: a register operand with no access, two written operands, more than three register operands
in machine order, a fixed `rsp` or `rbp`, a register both a result and a clobber, an immediate after a field a reference
patches, or a form marked commutative that is not two-address with two uses. From them it writes `machineOperandCount`,
`definesOperand`, `reusesOperand`, `commutesOperands` (add, and, or, xor and imul, whose uses the allocator may swap),
`isCall`, `isReturn`, `hasPatchField` and the masks `fixedUses`, `fixedDefs` and `clobbers` (a `RegMask` over the register file: the general registers, then xmm0
to xmm15 from `x64FirstVector`), `x64RegisterOf` and `x64FormOf` from a number, a `choose` function per choice that names
the form an immediate selects (`chooseAddRegImm`), and `encodeMachineForm`, which encodes a form from the fields of a machine
instruction.

The IR table and the x64 instruction table are Go packages of their own (`internal/irgen/irtable`,
`internal/x64gen/x64table`), so that `rulegen` reads the same tables as the generators of the records and the encoder.

**The rewrite rules** are written in a small language after the Go compiler's `_gen/*.rules`, one file per target:
`internal/rulegen/generic.rules` rewrites IR to IR, and an instruction selection file (`internal/rulegen/x64.rules`) rewrites IR
to x64 forms. A rule is a pattern, a condition after `&&` if it has one, and after `=>` what the instruction is rewritten to;
a line that starts with `&&` or `=>` goes on with the rule before it, and `//` starts a comment:

```
(bin.add x (const [0]))                                   => x
(bin.mul.!checked x (const [c])) && isPowerOfTwo(c)       => (bin.shl x (const [log2(c)]))
(bin.div.!unsigned.checked x (const [-1]) [fail])         => (neg x [fail])
(loadField obj [off])                                     => (LoadRegBaseDisp r obj [off])     // x64.rules
(bin.add.unsigned.checked x y [f])                        => (AddRegReg x y) (JccRel32 [X64CondCode.below] [f])
(branch c [yes] [no])                                     => (TestRegReg c c) (JccRel32 [X64CondCode.equal] [no]) (JmpRel32 [yes])
(bin.mul.unsigned.checked x y [f])                        => (Copy rax x) (MulReg y) (Copy r rax) (JccRel32 [X64CondCode.below] [f])
(call)                                                    => (emitCall)
```

- An operation is its name in the IR table with the first letter lowered (`bin`, `loadField`), then suffixes: the case of
  its enum operand as the printer writes it (`.add`, `.lt`), and each flag it must have (`.unsigned`, `.borrow`, and
  `.checked` and `.throws` for the flags that say a failure block or an error register is there) or must not have
  (`.!checked`). A flag the pattern does not name may be either.
- The operands follow in the table's order, leaving out the enum and the flags. A register is a variable, `_`, or the
  operation that defines it, which `name:` binds the register of; every other operand is in brackets: `[_]`, a variable or a
  number in a pattern, a Maxon expression of the rule's variables in a result. Operands left off the end of a pattern match
  anything; a result leaves off only an optional one (a failure block, an error register, a routine), which is then absent.
  A terminator's pattern names its targets and never its edge arguments (`jump`'s `Args`, `branch`'s `TrueArgs` and
  `FalseArgs`, `switch`'s `FallbackArgs`): they are no operands of a rule, and follow the edge that the rule keeps.
- A variable named twice matches only the same register or value. A condition is a Maxon expression of the variables that
  calls the helpers in `self/opt/rules.maxon` and `self/opt/division.maxon`.
- `bin` with add, mul, and, or or xor and `cmp` with eq or ne are commutative: each rule over them is also tried with the
  two register operands swapped.
- A generic rule rewrites to a register of its pattern, which then replaces the instruction's register, or to one
  instruction, whose nested operations become new instructions before it, each defining a new register. A nested
  operation that `name:` binds may be read again by the operands built after it (operands before their operation, left
  to right), as `(bin.sub x h:(bin.mulhi ...))` and then `h` do. The instructions
  the pattern matched below the root stay for dead code removal, so a check they make still runs; `generic.rules` states the
  invariant its rules keep (a rewritten program fails exactly where and as it did).
- An instruction selection rule rewrites to a sequence of x64 forms and choices of the instruction table, each with its
  operands in the order of its encoder's parameters, and `(Copy dst src)`, the machine IR's copy; or to one name in
  lowercase, a hand-written helper of `self/backend/isel.maxon` called with the selector, the function and the instruction
  (the calls and their argument passing, the operations whose code branches, the boxes). A register operand is a variable or
  a register's name (`rax`, `rbp`): a form defines a new register for the variable it writes, and a two-address form one
  for the variable it reads and writes, which the variable names from there on; the operation's result is the last
  definition, or the last definition of the variable that `d:` binds the result to. A `rel32` given a block of the pattern
  goes along that edge, with the arguments the IR passes it; any other bracket is a Maxon expression, which may name the
  selector (`sel`), the function (`fn`), the instruction (`ins`) and the backend's constants and the helpers of
  `self/backend/rules.maxon`. A sequence of forms that fixes registers (`SetccRax`) is none a rule names: it writes the
  copies to and from them. The rules of a target must match every IR operation with every case of its enum and every
  value of its flags: for each, some rule with no condition matches any operands. The instructions a selection pattern
  matched below the root are selected at their own place all the same; a rule that makes one's value itself, as a branch
  does a comparison it alone reads right before it (`sel.fuses`), leaves that one's code with no reader, which the machine
  IR's dead code removal takes out (see "The machine IR"). So `x64.rules` fuses a comparison into the branch on it (`cmp`
  and `jcc`, `test` against 0, a constant as an immediate), takes a constant as an immediate where a form has one (`add`,
  `sub`, `and`, `or`, `xor`, `cmp`, the three-address `imul`, the shifts), makes an address that a load or a store alone
  reads, a base plus a constant or plus a register, the access's own `[base + disp]` or `[base + index + disp]`, and
  `emitBounds` compares an index with the length in the box where the check alone reads it.

`rulegen` refuses, naming the rule and its line, a rule that names an operation, a case, a flag or an x64 form the tables do
not have, gives an operation the wrong number or kind of operands, names a variable that its pattern does not bind, or can
never fire because an earlier rule with no condition matches everything it matches (with the swaps of commutative
operations). It writes `self/opt/rewrite_gen.maxon`: `rewriteInstr(fn, at, defs, out)` dispatches on the opcode to a
function per operation that has rules, which tries them in the order of the file, each a function of flat `if`s that
returns at the first thing that does not match. It reads the operands in place through the generated getters and the
defining instruction of a register from `defs` (the `ir.Defs` of the function's `ir.Flow`), allocates nothing until a rule fires, and leaves the result
in a reused `opt.Rewrite`; the fold pass (`opt.Fold`) applies it to each instruction it visits. `selftest` runs it on instructions built by
hand (`self/opt/opt.maxtest`).

It writes `self/backend/lower_x64_gen.maxon` from `x64.rules`: `selectInstr(sel, fn, ins)` and `selectTerm(sel, fn, ins)`
dispatch on the opcode to `select<Op>`, which tries the operation's rules in the order of the file and ends in a panic
that the rule with no condition never lets it reach; each rule is a function of flat `if`s over the instruction and the
instructions that define its operands (`Selector.definedBy`, `defOf`) that returns false at the first thing that does not
match, and otherwise writes the machine instructions (`newTarget`, `newCopy`, a `choose` function for a choice) into the
selector's function, its own register for every IR register (`vregOf`) and a new one for every other definition.

## Pipeline

`self/driver/compile.maxon` runs the pipeline for `build`, `run`, `check`, the interpreter and the test runner as the task graph
of "The modules phase" (`self/driver/graph.maxon`): the `build` and `check` commands as behaviours, on every core
(`buildPath`, `self/driver/executor.maxon`), and `run`, the interpreter, the test runner and the tools that read the compile's
result on one thread (`execute`; the test runner runs many such compiles at once, one per test, as behaviours: see "Tests");
both make the same executable and the same diagnostics, whatever the number of workers. The
errors reported are those of the first stage that has any, from every module. A `check` stops short of code generation but
runs the stages that can report an error.

| Step | Module | Work |
|---|---|---|
| runtime | `driver`, `project` | for a `build`, `run` or test, the runtime (`runtime/`, see "The runtime") is loaded as module 0, run through the steps below on one thread and emitted once (`prepareRuntime`); a test run keeps it frozen and adds each program to a copy of the project (`SharedRuntime`) |
| discover | `project` | `discoverProjectInto` scans the directory tree (names in sorted order; `E3182` for a directory that is no module name) and makes a `Module` for each directory; a single file is one root module. Then the standard library's modules follow (see "Modules"). Its errors count as the parse's |
| parse | `syntax`, `project` | `parseModule` reads a module's files into one buffer, lexes it and parses it into the module's `Tree` and `decls`; a lexical error ends the module's lexing, a syntax error abandons the rest of its file. Then `buildModuleTable` gives the module's declarations their names (a duplicate is `E3006`) and a shell type (a `TypeID` with no members yet) for each type declaration |
| resolve | `resolve` | `resolveModule` binds every name, struct literal and type reference to its declaration, and each `where` constraint to the type parameter it names (`E3916`), into the module's `Resolution`; it records the modules the module depends on, checks visibility (`E3008`) and that no signature names a less visible type (`E3167`), and rewrites a qualified name in place (see "The AST"). It reads only the module's own declarations, the standard library and the declarations of the modules the module names |
| module graph | `project` | `newGraph` turns each module's recorded dependencies into edges; `checkAcyclic` sorts the graph (Kahn's algorithm, linear in the edges) and reports `E2012` where the first module of a cycle names the second; `topoOrder` is the order every later stage visits the modules in, and the driver adds the tasks of the stages below |
| aliases | `check` | `moduleAliases`: the bounds of each type's type parameters, the typealiases that the module's extensions declare under the types they extend, each type written `Type.name` bound to the typealias that the type's body or an extension the module sees declares, then each typealias of an instance, after the typealiases it names (a cycle is `E2012`) |
| declare | `check` | `moduleDeclare`: the module's fields, cases, function signatures, type parameters and the interfaces its types implement, into the module's slots of the `TypeTable` and `FuncTable`; the declare finds `main`, in the first module that declares it (`E3001` when none does, a task of the project: `projectEntry`; `E3006` for a second) |
| constants | `check` | `moduleConstantOrder`, then `moduleConstants`, two stages: types the top-level `let`s and the raw values that enums and unions write that are no literal (see "Raw values"), each after the constants and raw values of its module that it names (a cycle is `E2012`) |
| bodies | `check` | `moduleBodies` types every type declaration and function body, a generic one once against its bounds, and the body of a behaviour as a function of its own that captures the variables it uses. A call into another module is recorded, not bound (`PendingCall`); a call that needs an instance of a generic is recorded on its function (`callers`) |
| verify | `check` | `moduleVerify`: binds the recorded cross-module calls (`verifyCalls`) and checks every type argument against its parameter's bounds (`E3017`); then, for a module that found no error in this stage, the range analysis, which decides how each operator and cast can fail and requires a `try` around what can (`E3057`, `E3059`; see "Checked arithmetic"), and the ownership pass: moves, conventions, borrows, exclusivity, initialisation, frozen values and what a behaviour takes (`E3102`, `E3900` to `E3904`, `E3197`, `E2013`) |
| lower | `ir` | after the check of every module has ended (`endCheck`, which waits on every verify, each of which made its module's typing its product), `lowerModule` for each module in order: it first finds the instances of generic functions that the module needs (`findInstances`: it starts from the calls recorded by its functions that are not generic, and follows each instance's recorded calls, whichever module declares the generic, with the type arguments bound, until no new instance turns up; `E3906` if the type arguments grow without end), then lowers the module's functions that are not generic, in source order, and the instances, in the order found. A `check` only finds the instances. A place that needs something the backend cannot run yet is noted as a refusal, and the driver reports the first of each function (`E9900`) before the backend, in a task of the project that first holds the runtime to the functions the compiler calls (`checkRuntimeFunctions`) and writes `--emit-ir` |
| optimize | `opt` | for a native build, in the task that lowers the module, right after it: `opt.optimizeModule` puts each function in SSA form and folds it (see "The IR"), then `opt.findLeaves` finds the module's leaves and `opt.inlineModule` replaces the calls of leaves, its own and those of the modules it names, by copies of their bodies and folds again what got one (see "Leaf inlining"), then rotates the loops of each function, moves what does not change out of them and merges what is computed twice (see "The passes after inlining"); `--emit-ir` and `--verify-ir` see the result. A test keeps the IR as lowered for the interpreter (`Options.keepsLowered`), which tracks borrows by the loads of slots and reports a drop of a register that holds nothing |
| emit | `backend` | `emitModule`: IR to x64, one code unit per module, in module order, calls between units symbolic; an instance that several modules need is emitted by each, and the link keeps one. The emit then makes in the unit the drop and copy routine of every type its code asks for, and of every type those reach (a record's fields, a union's payloads, an array's elements), in the order they are first asked for (`backend.carryRoutines`), so a unit asks the link for nothing and holds no handle, which is what lets the build cache keep it; a routine that several units make is kept once, as an instance is. In the link (`linkProgram`) the entry stub and the runtime's unit come first; `emitTypeRoutines` makes there, in waves, only what units made without the driver ask for (the backend's own tests) |
| link | `backend` | `link` keeps only the sections of code and the pieces of data that the entry reaches through symbol and data references (every function is compiled; what nothing calls is dropped here), lays the units out in order, patches the references and `writePeImage` writes the executable. A symbol is its link name (see "Link names"); the one symbol that may be defined more than once is an instance of a generic function, and the table keeps the first unit's definition, so the copies are never reached and drop out like any dead section. Any other symbol defined twice is an internal error |

**Errors.** Each task's errors are kept with its stage, and the compile reports those of the earliest stage that has any, from
every module; a task one of whose inputs failed in an earlier stage, directly or through the tasks it waited on, or was
skipped, is not started (`Outcome.mustSkip`), since its errors would not be reported. Which tasks run is decided by their
inputs alone, never by what else has run, so it is the same for every executor and number of workers. Within a stage every
module is visited, so one run reports the errors of all modules. Of the lower, only the first module in module
order that failed is reported, since an instance whose type arguments nest without end fails every module that reaches it. An error raised at a node is a `diag.Site` (module handle, offset); when the compile ends
`ast.locateAllErrors` gives each its file, line and column, and then sorts them by file, line, column, code and message, so
the output does not depend on the order the modules were visited in. A compile that finds no error never builds a line
index. The message of an error about a bare name that was not found names the other modules that declare that name, which only the
project as a whole knows, so it is written when the error is located (`ast.locateError`). A `panic` that is not a
`diag.CompileError` is an internal compiler error.

`interp` runs the same stages through lower, with no runtime module and without refusing what only the interpreter runs
(`lowerForInterpreter` in `self/driver/interp.maxon`), and gives the IR to the interpreter instead of the backend. This is how a map,
a list, a pool, a frozen value, a cown and a behaviour run.

### The modules phase

The driver runs each module's stages, from parse to emit, as tasks of one graph (`self/driver/graph.maxon`), as Go's build
does. A task is a stage of one module (`Task`: the stage, the module and a range of the graph's flat list of inputs), or a
stage of the project as a whole: `parsed` joins the parses, since resolve may read the declaration table of any module that a
qualified name passes through; `order` makes the module graph and refuses a cycle, and only then adds the tasks of the check
and after it, since a module's dependencies are known once it is resolved; `entry` reports a program with no `main`;
`endCheck` waits on every module's verify and on `entry`, so that no module is lowered before every module's check ended;
`program` holds the runtime and reports refusals; `link` links the units. A task waits for its module's stage before it (or the task of the project before
it) and, from aliases to lower, for the same stage of each module it names, which comes before it in the module order and
covers what the stage reads of those modules: resolve reads only the declaration tables of the modules a qualified name passes
through; declare needs only the type objects of other modules' types, since what it needs of their declarations is checked in
verify; a constant waits for the constants of the modules it names; a check reads the declarations and constants of the modules
it reaches; the lower of a module reads the optimized IR and the leaves of the modules it names, whose calls it inlines. A task
that waits for one that failed in its own stage still runs, as every module of a stage is visited; one whose inputs failed in
an earlier stage, directly or through the tasks they waited on, or were skipped, does not (a module whose dependency failed
stops; see "Errors" in "Pipeline").

**What a stage is given.** A stage of a module takes its module's state `mutates`, the project `p` read-only, and the error
list of its task: the parse (`project.parseModule`), the resolve (`resolve.resolveModule`) and the lower (`ir.lowerModule`,
`ir.findInstances`) an `ast.ModuleState`, a stage of the check (`check.moduleAliases` to `check.moduleVerify`) a
`check.ModuleState`, which holds one (`unit`) with the checker's state of the module. An `ast.ModuleState` holds what the task
writes of the model, which the driver lends it out of the module's home for the task and takes back when it ends
(`ast.lendHome`, `ast.returnHome`). A module's home (`ast.ModuleHome`) is its slots of the tables, its tables, its record and
its resolution and typing, which a compile takes out of the project for each module it compiles when it begins and gives back
when it ends (`ast.takeHome`, `ast.putHome`), and holds in the module's work meanwhile (`ModuleWork` in the driver, with the
check the module's stages carry from one to the next and what its lower and its emit make), so that the project holds no part
of a module that a task writes; `ast.lendModule` and `ast.giveBack` lend the same out of the project, for work outside the
tasks. The task is lent its module's slot of each table (`ast.TypeSlot`, `ast.FuncSlot`: the nominal types, the shapes and their
index, the functions and the cache of instances), the only slot it can make or fill an entry in, so that no stage can write
another module's types or functions; the work of the project holds slot 0 (`ast.lendProject`), the built-ins' and the
standard library's, which only it and what runs before the tasks write. While a task runs, `p` is its view of the project
(`taskView` in the driver, `ast.viewFrom`), made only of shares, which is what a behaviour that runs the task captures: of what
ran before the compile, which nothing writes while it runs, frozen when it begins (`ast.baseOf`: the built-ins, the standard
library, the index of paths and the modules an earlier compile compiled), and of the products of the stages it waited on
(`ast.ModuleProducts`, `ast.publish`): of each module, its record with the tree the latest of those stages made, its
declarations if it waited on the parse and the parts of its tables it waited on (`ModuleTables.viewOf`, `ast.Module.viewOf`),
its resolution if it waited on the resolve and its typing if it waited on the verify; of the tables of types and functions, of
every slot but its own a share of each frozen list of the segments it waited on and of each later list the shell the last of
those stages left (`ast.typeListsSeen`, `ast.viewTypes`, `ast.viewFuncs`). What a stage it did not wait on made is not there. Every read of an entry goes through one accessor
family that takes the slot the task holds and the project's table, and finds the entry in one or the other:
`ast.typeEntry(ms.types, reads: p.types, t: t)` and `ast.funcEntry(ms.funcs, funcReads: p.funcs, f: f)`, functions that return
a borrowed place (see "Ownership"), and `ast.typeKind`; a write indexes the task's own slot,
`ms.types.lists[ast.typeList(ms.types, t: t)][ast.typeIndex(ms.types, t: t)]` or
`ms.funcs.lists[ast.funcList(ms.funcs, f: f)][ast.funcIndex(ms.funcs, f: f)]`, whose accessors stop the compile with an internal
error for an entry of another slot or of a segment that is complete (below). Every function of `ast` and of the stages that
reads entries takes the pair (`tt`, `reads`; `ft`, `funcReads`), and one that makes entries takes the slot `mutates`. The task
also holds the module's `ModuleTables` (the type, function and constant of each declaration, the locals its bodies bind, the
typealiases a type written `Owner.member` names, and what its extensions add to types, each in the part of the stage that
decided it); the resolve alone holds its `Resolution`, which every later stage reads in the project as the resolve's product
(`ast.resolutionOf`, and `ast.nodeDecl` for what a name or a type is bound to); and what else of the module its stage writes
(`ast.Lent`): the module it makes for the parse (`source`, what the parse writes being an `ast.ParsedModule` in `ms.parsed`),
the tree and the resolution for the resolve (`resolution`), the tree for the check (`tree`), nothing more for the lower and the
emit (`tables`). A stage reads its module's tree through `ms.tree()`: the resolve, which rewrites qualified names, and the
bodies, which desugar, write a copy of the tree the stage before made (`ms.ownTree`, `ast.lendTree`), and every other stage
shares that tree, frozen. A stage reads its own module's lent parts in its state and every other module's in the project,
through the accessors of the module tables (`ast.declType`, `ast.declFunc`, `ast.paramType`, `ast.nodeFunc`, `ast.declConst`,
`ast.rawTypeOf`, `ast.receiverLocal`, `ast.paramLocal`), of the extensions (`ast.extensionMethod` and the rest) and of the
modules a module names (`ast.namedCount`, `ast.namedModule`), each taking the state; the tables a project holds while a task
holds them refuse a read (`ModuleTables.requireHeld`), and a setter refuses a module other than the state's (`requireOwn`).
Every function that writes takes the state `mutates` (`ms`) and the project read-only; one that only reads takes the state
read-only. What runs outside the tasks and reads the tables (the IR printer and verifier after the lower, the interpreter,
`ast.typeLinkName`) passes `ast.TypeSlot.none()` and `ast.FuncSlot.none()`, the slots of no module, with the project's
tables, which then hold every slot; work outside the tasks that makes an entry lends the slot it makes it in
(`ast.lendProject`, `ast.makeType`, `ast.makeFunc`), in a segment after every one the slot has (`ast.laterSegment`), which
`makeType` and `makeFunc` keep open for the next entry (`ast.outsideSegment`, `ast.keepTypeSlot`). The emit
(`backend.emitModule`) takes its module's state as the lower does, and the link the state of the project. The order reads its
view of the products of every module's resolve, as a task of a module does, and decides how each module's check starts
(`CheckStart`: the slots whose shapes its stages read, and what its declare does about `main`), which the module's work takes
in (`ModuleWork.startCheck`); the program and the link read the project whole, which then holds every module's home: each
module's last task before them gives them its home (and the emit its unit), and the program gives each home back to the
module's emit. The code of the stages of the project is `orderTask`, `holdProgram` and `linkUnits` in
`self/driver/tasks.maxon`, and of the stages of a module `moduleTask`, which both executors run.

**What a stage makes.** Each stage's product is a set of values that are complete when its task ends and that no later
task writes, and each is frozen then, so that a task that waited on the stage shares it. A slot's entries are kept by segment
(`ast.Phase`: one for each stage of a module, at the phase's ordinal; in slot 0 the built-ins' and, from `Phase.project` on,
one for each task of the project that made entries): a list of the nominal types, one of the shapes and one of the functions
whose entries the stage completes, which a handle names in its bits 48-55. The lists of the segments that are complete are
frozen (`TypeSlot.sealed`, `FuncSlot.sealed`): a slot is sealed up to the segment its task writes when it is lent, and up to
the end of that segment when it is given back (`returnTypeSlot`, `returnFuncSlot`), and a compile seals slot 0, the standard
library's tables and the slots and tables of the modules an earlier compile compiled when it begins (`ast.sealProject`); the
lists after them (`lists`) are the ones the holder may still write. A stage makes its entries in its own segment, except the
parse, which makes the shell of a declared type, and of its type parameters, in the segment of the stage that fills it in
(`ast.allocShell`: the declare, or the aliases for a ranged, bits or float alias), so that no write reaches an entry of a
segment whose stage has ended (`requireOpen`, an internal error). What the parse leaves of those later segments is its
product too: at the end of each task the slot keeps a frozen copy of each later list as the task left it (`ast.Shell`,
`returnTypeSlot`), which a task that waited on the parse and not on the declare reads in its place, as the aliases of a module
read the shells of the generics of the modules it names. A module's `ModuleTables` are kept by part, one for each stage at the
phase's ordinal, which only that stage writes and which is frozen when it ends (`ModuleTables.sealed`, `sealWritten`), and a
read finds the latest part that holds the entry. The parse's product is the module's files, tree, declarations, their names
and nested typealiases (`Module.takeParsed`), the resolve's its resolution and the tree it rewrote, the bodies' the tree they
desugared, each frozen in the module's record (`ast.Module`: `files`, `tree`, `decls`, `names` and `nested` are `frozen with`);
the typing is the verify's, which its task makes its home's when the module's check ends (`check.endModule`, which also gives
the functions and the callers the lower reads, and the entry function the link reads); `p.resolutions` and `p.typings` hold
frozen values, and so do `p.builtins` and `p.byPath`, which only the making of the project and the discovery write. At the end
of each task of a module the products of its stages up to the task's are published (`ModuleWork.product`, `Product` in the
driver: the module's `ast.ModuleProducts`, its code and leaves once its lower ended, and the entry function if its check found
it), frozen. The errors are the task's own list, which the compile's list takes in task order when the compile ends
(`keepEarliestErrors`), so the diagnostics do not depend on the order the tasks ran in, and a refusal of the register
allocator that two modules' instances of one generic meet is kept once (`refusedAlready`). What a task knows (`Known` in the
driver) is, of each module the compile compiles, the product of the latest of its stages that the task waited on, directly or
through the tasks it waited on, or what the module held when the compile began (the parse's, for a compile of modules parsed
already: `Compile.begun`): each task that ends gives the tasks that wait on it what it knew, with its own product, and whether
it or a task before it failed or was skipped (`Outcome`), and a task merges what its inputs gave it (`Outcome.merge`). The
segments of each slot that it waited on follow from it (`ast.Inputs`, `inputsOf`); its slot's index of shapes takes in the
shapes of those segments when the task is lent its slot (`importShapes`), so a read is one probe. `--verify-reads` names the
reader of an entry of the tables of types or functions that its view does not hold, where a read past the view stops the
compile anyway. The project's `byPath` is written only by the discovery, before the tasks; a module's home only by its tasks;
the project tasks `order`, `endCheck`, `program` and `link` run after every task they read (barriers), and the link's drop and
copy routines make their shapes in a segment of slot 0 of their own.

Two executors run the graph. `execute` (`self/driver/graph.maxon`) runs the tasks on one thread, one at a time, in the order
they were added: stage by stage, and within a stage in the module order (the dependency order from the check on, module order
for parse, resolve and emit), so every task's inputs have run when it starts; the interpreter, `run`, the test runner (whose
tests run at once, each its own execute) and the tools that read what a compile made (`frontEnd`, `lowerLoaded`) use it, since
they go on from the compile's result. The `build`
and `check` commands run the tasks as behaviours (`startCompile`, `self/driver/executor.maxon`), on every core: each module
the compile compiles has a cown that holds its work and the gate of each of its tasks (`Station`), and the project has one
that holds the project, the gates of the tasks of the project and what became of every task (`ProjectStation`). A task that
ends reports its state and errors on the project's cown, then sends each task that waits on it its outcome (`Arrival`, with
the module's home or unit where the task is the program or the link) as a behaviour on that task's cown, which takes it in at
the task's gate and runs the task there when its last input arrived; so a task's counter lives in its own cown and the decision
is ordered by the cown, not by timing, and since a worker runs the behaviour it schedules next, a module's next stage stays on
the core that has its data. The behaviours read one frozen context (`Context`: the compile, the graph and who waits on each
task, and the cowns), which the order replaces by one of the graph it extended. The last task of the graph, a task of the
project, ends the compile (`finishCompile`): every task reported before it, so it keeps the errors in task order, prints them
and sets the code the process exits with (`Process.setExitCode`), which replaces `main`'s. Which tasks run depends only on
their inputs, and every product only on the inputs of the task that made it, so `MAXON_MAX_PROCS=1` and every number of workers
give the bytes and the diagnostics that `execute` gives.

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
| `ast.TypeID`, `ast.FuncID` | a type or a function. A nominal type's handle and every function's is a word whose bits 32-47 name its slot of the `TypeTable` or `FuncTable`, bits 48-55 the list of the slot it is in (by segment, see "The modules phase") and bits 0-31 its index there: slot 0 holds the built-ins, the stdlib and what the stages of the project make, and slot 1 + m the types and functions module m declares, the closures and behaviours its bodies make and the instances of generic functions its stages ask for, which only m's stages make, so their handles depend on m's source alone. A structural type's handle (an instance, a range, a constant, a function type's origin, a tuple's record and its type parameters) has bit 62 set, its `TypeKind` in bits 57-61 and a 57-bit hash of its structure below (`ast/shapes.maxon`), so it is one word in every module. Each slot holds the shapes its stages made or took from another slot, and a lookup goes through the task's own slot, slot 0 and the slots of the modules it names, directly or through others, which it waited on (`TypeSlot.reads`): when a task is lent its slot, the slot's index takes in the places of the shapes of the segments of those slots that the task waited on (`ast.Inputs`, `importShapes`), so a read is one probe and what a later stage of another module makes is never seen, and a kind is read from the handle (`ast.typeKind`); a task holds only its own slot `mutates`, so it cannot write another. Two structures under one hash are an internal error that names both. 0 is none (`noType`, `noFunc`) |
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
| `indexes` | a `for (i, x) in xs.enumerated()` | the `LocalID` of `i`, which the lowering stores each pass's index in |
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
as `Box with Count` is one type wherever it is written: its handle is the hash of its origin and arguments
(`instantiateType`), and it reads the declaration's fields, cases and methods and substitutes the arguments. Integer ranges
are hashed by their name and bounds. A function, a method and an instance of a generic function are `FuncEntry`s in the
`FuncTable`; an instance is interned in the slot of the module that asks for it, by its origin, arguments and conformances. Declare fills them in; every later phase reads them.

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
| a function | `geometry::area`, `export::helper`, `app::main` |
| a method | `export::Counter.inc`, `(export::Box with stdlib::Count).make` |
| an instance of a generic function | `export::swap[stdlib::Count, export::Counter]` |
| an instance given a conformance outside its type argument's module | `lib::describe[shapes::Meter] using app`, the module of each conformance after `using` |
| an instance of a method that an extension of an interface adds | `app::extension shapes::Shape.doubled[export::Square]`, by the extension's module and the interface, never the type's name |
| a type | `export::Counter`, `export::Box with (geometry::Shape, Count=int(0 to 18446744073709551615))`, `stdlib::FixedArray with 3 Int=int(...)` |
| a drop or copy routine | `drop export::Counter`, `copy stdlib::Array with export::Tree` |
| the table of an interface value | `table export::Square as shapes::Shape by export`, with the module of the conformance after `by` |
| the word that names a type at run time | `type export::Square` |
| a constant of the data section | `const export::Table`, `const export::Pt.ORIGIN`, `raw export::Op.add` |
| a runtime entry or global | `runtime::alloc` (the link name of the runtime function that defines the entry), `rt.entry` and `g.heap` (what the entry stub and the linker define) |

An integer type is named by its alias and its range, since the code for it depends on nothing else. The kinds of symbol cannot
meet: a function's name has `::` before any space, and a routine's starts with `drop ` or `copy `, a table's with `table `,
the name of a type with `type ` and a constant's with `const ` or `raw `. The text of a function in
the IR (`--emit-ir`) and in a message is its plain symbol (`ir.funcSymbol`).

## Build cache

`build` and `test <module dir>` keep each module's code unit on disk under a key that holds everything the unit is made from
(`self/driver/cache.maxon`), so that a module whose key did not change is linked from what an earlier compile made. The cache is
the directory `MINIMAXON_CACHE` names, or `%LOCALAPPDATA%\minimaxon\cache` when it names none, and none at all when it is `off` or
the command line says `--no-cache`; one directory serves every project, since a key names what made its entry. `check`, `run`,
the test runners and a compile with `--emit-ir` or `--verify-ir`, which need every module's IR, use none.

- **Digests.** `table.Hasher` makes a 128-bit `table.Digest`: two lanes of 64 bits read the same words, each with xxHash64's
  round under its own constants and rotation, and each is finished by MurmurHash3's fmix64 with the length. For inputs nobody
  made to collide, two of n entries share a digest with a chance of about n² / 2^129, so a digest is taken for what it stands for.
- **A module's digests** are made by its parse (`ModuleDigests`): its *source*, its path and each file's name and bytes; and its
  *shown text*, the same without its comment lines and without the body of each function, method or test that no other module
  copies (`publicDigest`): one that has no `uses` and stands in no `uses` type, no `interface` and no `extension`. The layout is
  part of the language, so a header is one line and its body the lines below it to the `end` at the header's indentation.
- **The summary** of a module, made by its lower, is the digest of its shown text, of the IR of its leaves (`opt.findLeaves`; the
  IR printer's text, which names functions and types by link name) and of the summaries of the modules it names: what another
  module copies of it, its declarations, layouts, constants, generic and extension bodies and the leaves it inlines, and,
  folded in, what those depend on. Editing any other body changes the module's key and not its summary.
- **The key** of a module (`keyOf`) is the digest of the compile's base (the digest of the compiler's own executable, the target,
  `--count-allocations`, and for a program the runtime's source), the module's path, its source and the summary of each module it
  names. Handles, the project's directory, the schedule and the number of workers are in no key.
- **An entry** (`<key>.entry`) holds a magic text, the layout's version, the key, the module's summary, the digest of the unit's
  bytes and the unit (`backend.writeUnit`: its code, fixups, data, pieces, pointers, symbols and sections, each array written
  straight). A unit carries its drop and copy routines (`backend.carryRoutines`), so it holds no handle and nothing is relocated
  when it is read back (`backend.readUnit`). Entries are written to a file of their own and renamed over their name, so a reader
  never meets half of one; a file that is no whole entry of this layout under its key is no entry, and one whose unit's bytes
  differ from their digest stops the compile, naming the file to delete.
- **Eviction** is Go's: a file's time on the file system says when a compile last used it. A compile that reads an entry or
  a summary whose time is more than an hour old writes it again whole (`markUsed`), so a use costs one `File.info` and, at
  most once an hour, a write; the end of a compile that used the cache, when `trim.txt` is a day old, writes `trim.txt`
  first and then deletes every entry, summary and temporary file unused for five days (`trimWhenDue`; the periods are named
  constants of `cache.maxon`). Every time is the file system's own: a compile reads "now" as the time of a file it writes
  and deletes, so no clock of the machine is held against a file's. Several compilers at once are safe: a write is a rename
  over the name, an entry is read whole when it is probed, and a delete that meets a file another process holds open, or
  one that is gone, is left; at worst a compile misses an entry and makes it again. `minimaxon cache` reports, `cache trim`
  trims now and `cache clear` deletes everything.
- **The order decides** (`planModules`), in the modules' order: a module is *settled* when every module it names is and the
  cache holds an entry under its key, whose summary it then has. A module that is not settled, and every module it names,
  directly or not, read what one another's check and lower made, since it may instantiate a generic or inline a leaf of any
  of them: one of those that is settled and whose checked state the cache holds loads it (`Reach.restored`), and the others
  run their check and lower; a settled module that none of them names runs nothing after its constants (`Reach.loaded`: its
  check ends where its constants did, which gives the entry function and the tests), and its emit links the entry's unit.
- **The checked state** of a module (`<key'>.checked`) is what its stages after its constants made, as its lower left
  them: its slots of the type and function tables whole (every segment's lists and the shells each stage left), its
  `ModuleTables`, its tree as the bodies desugared it, its `Typing`, its `check.ModuleChecked`, and its leaves with their IR
  (a function of its code that is no leaf is kept as its handle, since what reads the code of a module it names reads its
  leaves alone). `publish` cuts a slot by segment and picks shells by stage, so the home as the lower left it gives each of
  those stages' products exactly: a restored module's bodies load it (`restoreChecked`), its verify gives what its check
  ended with and its lower its leaves, and none of them checks or lowers anything. It is written only by a settled module
  that ran its check because a module that names it did not settle, the one place it is needed, so a compile of new source,
  one with an empty cache among them, writes none. Its values are stored by the functions `internal/cachegen` generates from
  the types' own declarations (`cache_gen.maxon`, over `table.CacheWriter` and `CacheReader`: each integer in the bytes its
  range takes, a string as its place in the entry's table of strings, a map as its slots' states and its occupied entries),
  and loaded as the same frozen values the stages publish. A handle names a module by its place, so the key adds to the
  module's key the paths of the modules before the compiled ones and the place and path of the module and of each module it
  reaches (`checkedKey`): a state is read back only where every place it holds is the same, and nothing is relocated; adding
  or removing a module moves the places after it, and a module whose layout moved runs its check again. Two fields are no
  part of the state, as their values are no function of the key: a slot's index of shapes and what it imported, which also
  hold the shapes of the slots its tasks waited on as that compile made them; the loaded slot indexes its own shapes
  (`ast.indexOwnShapes`). A tree's text is the module's source, which its parse read. Any other module's emit computes its key once
  the modules it names have their summaries, and links the entry if there is one, or emits and writes one. A module whose stages
  found an error never gets one, and every stage that can report an error, before the bodies, always runs.
- **Speculation.** A module whose source changed has no entry, so the modules that name it could not know their keys before its
  lower. The order takes for its summary the one it made the last time its shown text and the summaries of the modules it names
  were what they are (`speculationKey`, a `<key>.summary` file each lower writes), so they find their entries at once; if its
  lower then makes another summary (the IR of a leaf changed), the compile is done again without speculating (`finishCompile`),
  and its output is that second compile's.
- **Correctness.** A unit is a function of its key, and the link lays the units out in module order whatever made them, so a
  cached build's executable and diagnostics are byte for byte an uncached build's, at any number of workers and with
  `--reverse-modules`. `--verify-cache` checks every module and holds the unit and the summary it makes to the entry under its key,
  and the checked state it makes to the one under its checked key, byte for byte, an internal error naming the module where
  they differ.

What an edit costs (`build self`, best of three, 16 cores): with an empty cache 1.9 s; nothing changed 0.38 s (the front of every
module and the runtime, and the link); a body edited in `diag`, which every module names, 0.39 s; a body in `check` 1.35 s, as
`check`'s check and lower need those of `ast`, `diag` and `table` again; an export added to `diag` 1.83 s, everything. `test
self/ir` takes 1.1 s with an empty cache and 0.34 s warm. What is not cached yet is a module's checked state and lowered IR: with
them, a module that an unsettled one names would load its check and lower instead of running them, so an edit in a middle module
would cost its own module and no more. A language server needs the same: a module's products kept while it does not change (its
tables are indexed by `NodeID`, which depends only on its text), its diagnostics under its key, and its summary to tell whether
the modules that name it have to be looked at again.
## The IR

The checker works on the AST, which mirrors what the author wrote. Everything after it works on the IR in `self/ir`, which is
what a later pass such as reference counting or an optimization changes.

- A function (`ir.IrFunction`) is a list of basic blocks, each a run of instructions ending in one terminator (`jump`,
  `branch`, `switch`, `ret`, `trap`, `throw`). The instructions of every block are one list (`instrs`), block after block, and
  a block is the range of it that it holds (`Block.first`, `endAt`) with its terminator beside the range.
- Instructions read and write virtual registers (`%3`), each defined once: by one instruction, or as a parameter of a block.
  A block's parameters are a range of the function's `blockParams` pool (`Block.params`, `paramCount`), printed after its
  label (`b1(%2, %3):`); every edge into the block passes one argument for each, of the same kind, and sets them all at once,
  so an edge that passes `(%3, %2)` to `b1(%2, %3)` swaps them. An edge's arguments are a range of `args`: a `jump`'s
  (`jump b1(%0)`), a `branch`'s two (the true edge's `targetArgs` from `args` on, then the false edge's `otherArgs` right
  after), a `switch`'s fallback in the terminator's record and each case's in its `SwitchCase`; `newJump`, `newBranch` and
  `newSwitch` make edges that pass none, and `Builder.jumpWith`, `branchWith` and `switchWith` edges that pass some. Lowering puts locals, and
  every value that it carries from one block to another, in numbered slots (`s0`), and makes no block parameters. For a
  native build, SSA construction (`opt.Ssa`) then promotes every slot that no `addr` names and that each instruction naming
  it loads, stores, moves or drops whole, of its own kind: pruned SSA, a parameter at each join of the iterated dominance
  frontier of the slot's writes where the slot is live, then renaming along the dominator tree, linear in the function plus
  the live range and frontiers of each slot, on arrays kept from one function to the next. A slot holds nothing until its
  first store and after a `move` or a `dropslot`; a read of such a slot is a register that holds 0 (a `const 0` at the
  entry), its `dropslot` is removed, and the `dropslot` of a slot that holds a value is a `drop` of that value, so a value
  that is empty only on some paths reaches its drop as a 0, which drops as nothing. An entry block that a back edge enters
  gets a new entry that jumps to it, since the entry takes no parameters. The slots that stay are numbered again from 0,
  so selection reserves and zeroes only those. On `self`, 17,784 of 17,923 slots are promoted.
- The fold pass (`opt.Fold`, `self/opt/fold.maxon`) runs next, on every function of a native build: one worklist that
  folds constants (by the IR's own meaning, `ir.evalBin` and the rest in `self/ir/evaluate_gen.maxon`, which the interpreter
  runs too), propagates copies, applies the rules of `generic.rules` (and, run again past a function's fallback, those of
  `pressure.rules`, which may make an instruction hold more values at once than the one it replaces: maxon's
  `strengthReduceDivision`, where a division or a remainder by a constant other than 0, 1 and -1 is a shift for a power of
  two, with the bias that makes a signed one truncate, and otherwise the high word of a product with a multiplier that
  `self/opt/division.maxon` derives, shifted: `bin.mulhi`, `mul` or `imul` of one operand on x64; an unsigned divisor of
  2^63 or more is a comparison), turns a `branch` or a `switch` on a constant, and a
  `branch` whose two edges are the same, into a `jump`, decides the guards and checked operations whose operands are
  constants, removes the `drop` of a register that holds 0, and makes a block parameter that every edge passes one value
  (or itself) that value, and one that every edge passes the same constant a `const`; then it removes the blocks nothing
  reaches, the parameters nothing reads and every instruction whose value nothing reads that has no effect in the IR table
  and cannot fail. Its invariant is that a folded program fails exactly where and as it failed before: a check goes only
  where its constants pass it, an instruction whose constants fail its check becomes the jump to its failure block (and
  what followed it in its block goes), an operation with no meaning on its constants (a division by 0, a negative shift
  count) is left to its guard, and nothing that can fail or has an effect is removed, moved or merged. It is linear in the
  function plus its rewrites: a register is learnt constant or a copy at most once, only its readers are visited again,
  through use lists that a copy splices into its value's, and an edge is dropped at most once. On `self` it takes 287,030
  instructions to 269,402, folds 816 branches and removes 786 parameters; it decides no check there, since the range
  analysis leaves none whose operands are constants.
- **The expansion of the primitives** (`self/opt/expand.maxon`) runs after the fold, on each module, before leaf
  inlining and on every function first, so every copy of a leaf is expanded alike: the calls that read a length or an
  element become the operations of their common case (see "The primitives in the IR"). Right after it, each function that
  holds a division or a remainder (`opt.mayRaise`) is folded again with the rules of `pressure.rules`.
- **Leaf inlining** (`self/opt/inline.maxon`) runs after the expansion, on each module: a call of a leaf is replaced by a copy
  of its body. The policy is maxon's leaf rule (`InlineLeaves`): a leaf calls nothing on its hot path (its blocks that do
  not end in a `trap`), but for a runtime entry the backend writes in place (`ir.writtenInPlace`: lengths, element reads
  and writes, a push) and a `drop`, which stays a call in the copy; it has at most `maxLeafOps` (24) instructions and
  terminators on its hot path, a parameter's read aside, or `maxColdArmLeafOps` (28) when it has cold blocks, which hold
  at most `maxLeafColdOps` (12); it keeps no slot (a frame zeroes its slots once), holds no closure or behaviour body and
  no refusal. Calling nothing, it is never recursive. The leaves are decided on the bodies as the fold left them and a call
  is replaced only outside a cold block, so no leaf changes and every copy is the same. A copy of one block that returns a
  value it makes goes into the caller's block with that value made in the call's register; any other copy is blocks of
  its own after the function's (`ir.relocationInto` appends the callee's pools, and the generated `relocateInstr` and
  `relocateTerm` move each operation's registers, blocks and pool indexes), entered by a jump that ends the call's
  block, and its `ret` and `throw` jump to a new block holding the rest of it, passing the call's value and error (0
  for the one that is not there), so a throwing callee joins the caller's failure handling where the call did. A module's
  calls of the leaves of the modules it names are inlined as its own: its lower task waited for theirs (see "The modules
  phase"). On `self`, 7,226 calls are inlined, and the build of `self` by itself takes about 6% less time.
- **The passes after inlining** run on each function of a module once every call of a leaf in it is replaced, in this
  order. What each may do with an operation is decided in one place, `opt.motionOf` (`self/opt/motion.maxon`), which names
  every operation: a constant, a pure operation, a division (pure but faulting on 0), a check, a read of memory that only
  the function's own writes and calls change, a read of memory another thread may write (a runtime global, a raw word, the
  thread's context: never merged or moved), an operation that changes nothing a read sees (an allocation, a copy, a box, a
  closure, a drop of a register, whose boxes unique ownership gives to the dropped value alone), and a clobber (a store, a
  move out of a place, a `dropslot` or `dropat`, an atomic operation, a frame box, and every call: a `mutates` argument is
  the address of the caller's place and a push or an insert is a runtime call that changes its array's length and buffer,
  and the IR keeps no types or conventions to say which box a call may change, so a call ends every read).
  - **Loop rotation** (`self/opt/rotate.maxon`) turns a loop whose header tests whether to leave, as a `while` is lowered,
    into the test once before the loop and a copy of it at the end of each iteration, so an iteration ends in one
    conditional branch instead of a jump back to the test. It copies only a header of one segment and at most
    `maxRotatedTest` (8) operations that can be computed again, entered from the loop only by jumps, so a loop grows by at
    most `maxRotationCopies` (16) operations; the header's parameters become parameters of the block it entered the loop by
    and, where the code after the loop reads them, of the exit, each read renamed by the region of the dominator tree it
    is in, and a loop with a read anywhere else is left as it is. `for` loops are lowered rotated already.
  - **Loop-invariant code motion** (`self/opt/licm.maxon`, maxon's `LoopInvariantCodeMotion`) moves to a loop's preheader
    (the block that enters it, or one made on the edges that enter its header) a pure operation whose operands do not
    change in the loop; a read, when the loop holds no clobber and the first iteration runs it anyway (its segment
    dominates every exit and every edge back), or when a read of the same array's box moved before it; and a check or a
    division only from the header's prefix, the operations before the first that stays, so it fails in the preheader
    exactly when, and after the same effects as, it would have at the start of the first iteration, and only when its
    failure path reads nothing the loop defines. An element read that stays in such a loop is split into the read of the
    array's buffer (`buffer`, which leaves) and the access at the index (`wordat`, `byteat`), which only a native build
    makes. Loops are taken innermost first, so a value leaves a whole nest; a constant is copied, not moved.
  - **Common subexpression elimination** (`self/opt/cse.maxon`, maxon's `CommonSubexpressionElimination`) walks the
    dominator tree with a scoped hash table: an operation that computes what an operation dominating it computed goes, a
    check that an identical dominating one passed goes with its failure block, and a read is merged only with one that
    sees the same version of memory, a number that changes at each clobber, at a join whose ways in disagree and at the
    header of a loop that holds a clobber, so no read is merged across a write on any path.

  They share one analysis, found again only after a pass that changed the function, and an edit that only removes
  instructions moves the rest down in place. On `self` they rotate 598 loops, move 13,032 operations out of loops (once
  for each loop an operation leaves) and remove 62,900, for about 150 ms of the build's task time (the analyses 50, rotation
  20, code motion 45, elimination 40), which the emit stage of the compiler they build gives back
  in part; the instruction selection's push also reads an array's length once when the index is that length.
- **E5001 and the optimizations.** A copy must not make a loop fail to compile: with the call, the values live across it
  were in memory across it (the call tier of spilling), across the copy they are not, and a value moved out of a loop or
  merged with one before it lives across the loop. Maxon checks a pressure budget only for its wrapper and called-once
  rules and none for leaves. Here a function that may hold a loop (an edge to a block not after its own) has as its
  fallback the IR it had before the expansion, the rules of `pressure.rules`, the copies and the passes after them
  (`self/opt/optimizer.maxon` says which passes run before the fallback, which never make an instruction hold more values
  at once than the code it replaces, and which after), and the emit compiles that in its place where it refuses a hot
  loop of the optimized one (`selectOrFallBack`), so a program that compiles without them compiles with them, exactly.
  No copy is kept: only where the emit refuses a hot loop does it ask the driver (`backend.FallbackSource`) for the
  fallback, which `opt.fallbackOf` makes by lowering the function again from its declaration and running the passes
  before the fallback on it, which give the IR they gave at the lower (a cache hit links a unit and runs no emit, so an
  emit always has its module's checked state). `--timings` prints how many fallbacks were taken.
  `tests/run/a-hot-loop-that-calls-a-leaf-compiles-as-with-the-call.maxon` is a loop that needs the fallback, and so is a
  loop of the `main` of `tests/run/array-methods.maxon`, which holds fifteen values once its element reads are expanded,
  and the loops of `tests/run/a-loop-at-the-pools-edge-divides-by-a-constant.maxon`, which fit with the division and not
  with its multiply.
- Every slot and register is `scalar` or `managed`, so a pass can find every reference to a heap object.
- Checks are explicit, and only the ones the range analysis could not rule out are there (see "Checked arithmetic"): a range
  check, a divisor that may be 0 and a shift count that may be negative are each a `guard` with its bound, an index of an
  array, a byte array or a string is a `bounds` against its length, and a `bin` or a `neg` whose result may not fit the
  word is checked. Each of them names the block it goes to when the check fails, `else b4`,
  which is the only way an instruction leaves its block before its end: there the failure drops what it leaves and goes to the
  handler of its `try`, or returns the error with `throw`. A function that throws returns its error beside its value, 0 when it
  returned and the error's case plus 1 when it failed (`r10` in the executable), and a `call` of one names the register that
  receives it (`error %7`). An allocation is `alloc`.
- An instruction is one fixed-size record (`ir.Instr`): an opcode, three registers, a small index, an immediate and two bytes
  (`sub` and `flags`) whose meaning the opcode says (the IR table, `internal/irgen/irtable/table.go`, has it, and the getters of each
  opcode such as `binLeft(x)` read the fields by their operands' names), and a terminator is a record of its own (`ir.Term`).
  What does not fit a record (a type, a callee, a string, the arguments of a call, the cases of a switch) is in the function's
  pools (`args`, `types`, `callees`, `strings`, `cases`), which an instruction indexes; a block, a register, a slot and a case
  are indexes too. The opcodes are a closed set, and a pass that handles them all implements `ir.InstrVisitor` and
  `ir.TermVisitor` (`ir.visitInstr`, `ir.visitTerm`), so an opcode added to the table fails the build there.
- A function's IR is made by an `ir.Builder`, which collects the instructions in the order they are emitted, whichever block
  they are for, and `finish` lays them out block after block (a block nothing reaches is dropped and the others are numbered
  from 0). A failure edge into a block that another edge also enters, such as the two guards of a cast that share one failure,
  gets a block of its own at the end that jumps there, so the verifier's last rule holds of all lowered IR. A `when` body and a closure's body are each an `IrFunction` of its own in `bodies` of the function around it.
- `--emit-ir` prints a module's IR (`ir.moduleIrText`).
- The verifier (`ir.verifyIr`, `self/ir/verify.maxon`) checks the rules every pass must keep: each register is defined once;
  every definition dominates its uses; an edge passes as many arguments as its target has parameters, each of the parameter's
  kind, and the entry has none; and an edge that leaves its block before its end (an `else`) passes no arguments and is the
  only edge into its target. It checks them over `ir.Flow` (`self/ir/flow.maxon`), the analysis that the optimizer builds on
  too: the blocks cut into segments after each instruction that can fail (whose failure edge leaves from there, and whose
  result is defined only at the start of the next segment), where each register is defined (`Defs`), the edges between the
  segments as successor and predecessor lists (`buildCsr`), the reverse postorder with the retreating edges of the same walk,
  and the immediate dominators (Cooper, Harvey and Kennedy: `commonDominatorOf`, `dominates`). Its arrays are reused from
  one function to the next. `opt.Analysis` adds the dominator tree, the dominance frontiers, the loop nesting depth of each
  segment (natural loops from the retreating edges, each of which must go to a segment that dominates its source: lowering
  makes no irreducible flow, having no goto) and how often each register is read. `selftest` runs the verifier on IR built by hand
  (`self/interp/verify.maxtest`), and `--verify-ir` on everything a build lowers, before SSA construction and after the fold pass. The interpreter and the backend read every
  argument of an edge before they set a parameter: colouring copies an edge's arguments to the target's parameters as one
  parallel copy.
- What an analysis asks of every operation is generated from the IR table: the registers it reads (`instrReads`, `termReads`,
  the edges' arguments included) and writes (`instrWrites`, a call's error register included). `SuccessorVisitor` gives a
  terminator's edges with their arguments.

| Group | Instructions |
|---|---|
| values | `const`, `str`, `datum` (a constant of the executable's data, see "Constants in the data section"), `load` and `store` (a slot; `store ... borrow` for a borrow), `addr` (a slot's address), `param`, `field`, `setfield` (`setfield ... borrow` for a field that borrows what it holds, a buffer cursor's, which its box neither drops nor copies), `fieldaddr`, `loadat`, `storeat`, `global` |
| arithmetic | `bin` (add, sub, mul, div, mod, and, or, xor, shl, shr; `.u` for unsigned; checked ones have an `else` block), `cmp`, `neg`, `not`, `guard` |
| collections | `length.array`, `length.string`, `bounds`, `elemaddr`, `elem`, `byte.array`, `byte.string` (see "The primitives in the IR") |
| floats | `fadd`, `fsub`, `fmul`, `fdiv`, `fcmp`, `fsqrt`, `fround`, `ffloor`, `fceil`, `itof` (`.u` for unsigned), `ftoi` (see "Floats") |
| calls | `call`, `runtime` (a runtime entry, with the `routine` a cown's entry stores), `alloc`, `when`, `async` (see "Coroutines: async and await"), `callvalue` (see "Function values in the IR") |
| function values | `closure`, `funcvalue` (see "Function values in the IR") |
| interface values | `box`, `framebox`, `callwitness`, `istype` (see "Interface values in the IR") |
| ownership | `drop`, `dropslot`, `dropat`, `move`, `moveat`, `copy` (see "Drops") |
| the runtime only | `loadraw.W`, `storeraw.W`, `globaladdr`, `import` (see "The runtime") |

Native code goes from the IR, in SSA form, to machine IR (see "The machine IR") by instruction selection, which the rules of
`internal/rulegen/x64.rules` and the helpers of `self/backend/isel.maxon` write; register allocation then spills where the
registers do not suffice, refusing a hot loop that needs more registers than the target has (E5001), and gives every
virtual register a register (see "The machine IR"). A frame larger than a page touches each page as it grows, as Windows
requires. Calls use the internal convention of the target table (`internal/targetgen`, maxon's): the integer arguments
in `rcx`, `rdx`, `rax`, `r9`, `rsi` and `rdi` and the float arguments in `xmm0` to `xmm5`, each class counted apart as
maxon counts them (the third float is in `xmm2` however many integers come before it), and the arguments past their
class's registers in one run on the stack in the order of the arguments, pushed right to left, so that the first of them
is nearest the return address, which the caller pops (`placeArgs`); the result in `r8`, a float result in `xmm0`, and the
error flag of a function that throws in `r10`; `rbx`, `r12` to `r15` and `xmm6` to `xmm15` are callee-saved, and a
function pushes those it uses. A function copies its parameters out of the argument registers in its prologue
(`Selector.takeParams`), and reads one passed on the stack from the frame. The Win64 ABI applies only to the DLL imports
that the runtime calls on Windows (`import`), none of which takes or returns a float, and the system call convention
(the number in `rax`, up to six words in `rdi`, `rsi`, `rdx`, `r10`, `r8` and `r9`, the result in `rax`, `rcx` and `r11` lost) only to the
system calls that it makes on Linux (`syscall`, `emitSyscall`); System V's host convention is in the table for a later call of a
shared library, which no Linux executable makes.

**Floats across calls.** Which arguments of a call and whether its result are floats is its float signature
(`ir.FloatSignature`, `self/ir/convention.maxon`): a bit for each argument by its place (an `ir.FloatMask` of 64) and a
flag (`FloatResult`) of a `call`, a `callvalue` and a `callwitness`, which the printer writes as ` floats [0, 2] returns
float`, and `floatParams` and `floatResult` on each `IrFunction`. The mask is no field of the instruction: the function's
float pool (`floatStarts`, `floatMasks`) holds it by where the call's arguments start in `args`, in their order, only for
a call that passes a float, and `ir.callFloats(f, x)` is its one reader; inlining moves it with the arguments
(`relocationInto`). The interpreter never reads them; selection passes and takes each value in the class they say, and
`--verify-ir` holds each call to the code it reaches where the function shows that code: a `call` to its callee's
declaration, a `callvalue` of a closure or a named function that the function makes, and a `callwitness` on an interface
value that it boxes, to the method of the box's table. A call and every function it can reach read them off one static
signature: a `call` and the function it calls off the function's declaration (`ir.funcSignature`; an instance of a generic
has its types substituted there), a `callvalue` and a closure's body off the function type, with the type arguments of
the code around them applied (`ir.valueSignature`), and a `callwitness` off the requirement as the call sees it, the
interface value's types bound, which the declaration of every method that meets it matches (and the value the box holds,
never a float). A function value of a named function calls its code, whose declaration matches the function type. A
parameter passed by address (`mutates`, `sets`) and a result that `returns borrowed` lends are words. A behaviour's body
takes words, as its thunk passes the words of the behaviour's record, and so do a runtime entry (`runtime`), whose
signature `check runtime` holds to words, `callcode`, the drop and copy routines, the thread start and the entry stub.

Records and union values are heap boxes of 8-byte words (a union's first word is its case index), allocated by the running
thread's heap (`runtime::alloc`, see "Native freeing") and freed by it (`runtime::free`) where their owner dies (see "Drops"); a box is
zeroed when it is allocated. A string is a pointer to `[length][UTF-8 bytes]`: a literal lives in the executable's data section
and is never freed, any other string is a box on the heap. An enum value is its case index. A constant whose value is a box
lives in the data section too (see "Constants in the data section").

### Constants in the data section

A constant whose value is a box built only of what is known before the program runs (a top-level or `static let`, and each
raw value of an enum or a union whose raw values are records) is laid out once, at compile time, in the executable's data,
and read from there (`ir.Datum`, the `datum` instruction). `ir.datumOf` (`self/ir/datums.maxon`) is the one place that decides
it: it lays out integers and floats that the checker knows, `bool`s, enum cases, plain strings, records and tuples (with
their fields' defaults), cases of a union with no payload, array literals (a packed one as bytes), `.clone()` of these, the
other constants they name (laid out again inside this one) and calls of a literal factory, whose body is read with its
parameters bound to the arguments. Everything else is built where it is read, as before: a number, a string or a function value
as the whole constant (a number is a constant of the code and a string a literal already), and a constant that holds an
interpolated string, a byte string, a map or a list, a function value or a closure, a value boxed into an interface, a
conditional or a match, or an operator or a cast the checker does not fold. The raw values of an enum or a union are data
only when every case's is, so a read has the same owner whatever the case.

A datum is one piece of the data, its boxes laid out as lowering would build them on the heap (a record's words padded as
`boxBytes` pads them, an array's header and buffer) followed by its strings, named `const <module>::<name>` or
`raw <type>.<case>`; every unit that reads it defines it and the link keeps one, as it does an instance. The words that hold an
address of the piece are written by the link and listed in the image's base relocations (`.reloc`, `IMAGE_REL_BASED_DIR64`
entries in blocks of a page, in address order), so the loader moves them with the image. The data section stays writable and a
constant has no count and no marker: the checker refuses writing a constant or moving a part out of it (`E2013`, `E3901`), a
read that only borrows reads the data, and a read that takes the value (a `let`, a `consumes` argument, a box of an
interface) takes a copy (`settle`), so nothing ever drops, counts or writes a datum, and the strings it holds are immortal by
the address check every literal has. The interpreter makes each datum once by its symbol, as immortal objects, and reports a
drop of one, a store into one or a move out of one as a violation.

### The primitives in the IR

An `Array`, a `Map`, a `List` and a `Pool` (see "Primitives") are heap boxes that only runtime entries touch, so the IR holds no
layout of them: an operation is a `runtime` call of a named entry (`internal/builtingen/table.go`), and the interpreter and the
runtime each implement the entries. The element is always visible in the call:

- An entry that takes an element **moves it in** (`rt.array.insert`, `rt.map.insert`, `rt.list.pushBack`, `rt.pool.insert`), one
  that returns an element **moves it out** (`rt.array.remove`, `rt.list.popFront`, `rt.pool.remove`), and `rt.<primitive>.elem`
  returns the **address** of an element: a borrow, valid until the next call that changes the primitive. A subscript is that
  call and a `loadat` or a `storeat`, and a `mutates` argument `a[i]` is the address itself. A value that changes the primitive
  is computed before the address is taken, which is what the ownership pass's exclusivity rules guarantee at the source.
- **The expansion of the primitives** (`self/opt/expand.maxon`, maxon's `inlineManagedPrimitives`) writes the common case of
  the entries that read a length or an element of an array, a byte array or a string as operations of the IR, so that the
  optimizer, instruction selection and the register allocator see loads and no call: `length.array %a` and `length.string
  %s` read the count (`rt.array.len`, `rt.bytes.len`, `rt.str.len`), `bounds %i, %n else b7` goes to `b7` unless `%i` is
  below `%n` (unsigned, so a negative index is past every length), `elemaddr %a, %i` is the address of the element of an
  array of words (`rt.array.elem`), `elem %a, %i` reads it where only the `loadat` right after the call reads the address (a
  scalar, or a managed element lent as `loadat` lends it), and `byte.array %a, %i` and `byte.string %s, %i` read a byte
  (`rt.bytes.get`, `rt.str.byteAt`). A read of an element is a `length`, a `bounds` and the read; the failure block of the
  `bounds`, one per read after the function's blocks, makes the call that was there and a `trap`: called there, the entry
  traps with its own message and exit code, natively and under the interpreter, so a miss fails exactly as before.
  Selection writes a `length` as a load, a `bounds` as a compare and a branch that ends its block (`emitBounds`: an edge
  in the middle of a block would keep what the trap block reads live to the block's end), and an element as a load of the
  buffer's address and an indexed access (`[data + index*8]`, `[data + index]`; a string's bytes are in its box, so a
  byte of one is the access alone). Every
  other entry stays a call: growth, insertion, removal, the byte write, maps, lists and pools. Where an index is proved
  (inside a `FixedArray` by the checker, or by the test of a `try` around `get` and `set`), lowering writes the entry's call
  `proven` (`runtime rt.array.elem(%a, %i) proven`), which the expansion makes the access alone, with no `length` or
  `bounds`: lowering writes calls only, so the fallback holds every call the expansion takes out. The interpreter runs the expanded form (the driver expands the IR it keeps for it, as
  lowered), each operation on its model of the collection: a `bounds` by the IR's own meaning (`ir.evalBounds`), as the fold
  decides one whose operands are constants, and an element by the cell its entry's address would name, so its checks of a
  stale or freed box and of a borrow a call is still waiting to use (`elemaddr` and `elem` open a window as `loadat` does)
  are those of the call; none of the passes after inlining run on the interpreter's IR, so the `buffer`, `wordat` and
  `byteat` that code motion splits an element read into reach only the backend. A native build expands each function after the fold, before leaf inlining; with no call left in
  a loop, a loop that the call let compile may need more registers than x64 has, so the function's IR before the expansion
  is its fallback (see "E5001 and the optimizations"), whose calls the backend writes in place as it always did.
- An array whose element is an integer that fits in a byte (`ByteArray`, `Array with Byte`) packs one byte to an element, so it
  calls the `rt.bytes.*` entries instead of `rt.array.*`: `rt.bytes.get` and `rt.bytes.set` in place of the address of an
  element, and its own `insert`, `remove`, `append` (a copy of the bytes), `appendStr` and `toString`. `ast.packsBytes` decides
  it and `arrayPacked` (`self/ir/arrays.maxon`) is the one place lowering asks, so every array operation goes through it. A byte
  has no word address: a `mutates` argument `a[i]` is a slot that holds the byte, which is stored back into the element after the
  call returns, on its failure path too.
- A function that returns a borrowed place (`returns borrowed T`) returns the word its `let` borrow would be passed as:
  the box of a managed value (for the contents of a frozen value, the box `rt.frozen.get` reads out of it), which
  neither it nor its caller drops (`IrFunction.lendsResult`), and the value of a scalar. It is not an address, which a
  byte of a packed array and a `let` parameter do not have beyond the call. A call of it is an ordinary call that
  returns a word; the caller reads through the box as through a `let` parameter, and the optimizer and the register
  allocator see nothing new. The interpreter holds such a return to giving a live value that an owner still holds, where
  any other return must give one that nothing owns.
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
`rt.array.remove` hands the element it removes to the caller, who owns it. `rt.array.shrink` sets the length of an array or a byte array and keeps the buffer: `clear` and `truncate` of an element that needs no drop are that one call, and of an element that owns something they drop each element in place, last first, in one loop in the IR (`arrayShrinkTo`), and then call it. A byte array has the same three words, with a buffer
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
of its owner by the instruction before, so it is in flight. **The body is a function of its own**, in `IrFunction.bodies` of
the function around it and printed after it: its parameters are the access to each cown's value (an address, as a `mutates`
parameter has), then the captures, which it owns as a `consumes` parameter does and drops at its end. It returns nothing, and
nothing waits for it. Its name is the enclosing function's with `$when` and a number, in source order (`transfer$when0`,
`swap[String]$when0` in an instance, `outer$when0$when0` for one inside another; `ast.behaviourName`), so a generic function that
holds a behaviour has one for each instance, lowered with the instance's type arguments.

**Native.** All of it has native code. `rt.frozen.freeze` and `rt.cown.new` are functions of `runtime/frozen.maxon`: one allocation and
the stores of the header and the value, whatever the value holds (layouts `frozen*Offset` and `cown*Offset` of
`internal/abigen`). `rt.frozen.get` is written in place by the backend, a load of the box's value that leaves the count
alone, so a read or a borrow through a frozen value costs nothing more. `rt.share` (and the copy routine of
`frozen with T`, `cown with T` and a function type, `backend.copyShared`) adds 1 to the count with one `lock xadd`. The
backend passes `rt.cown.new` the address of the code of the instruction's routine, or 0, as a second word, stored as the
cown's `ending`. The drop routine of `frozen with T` and of `cown with T` (`backend.dropFrozen`, `dropCown`) subtracts 1
with one `lock xadd`, and the owner that found 1 there frees the box and then ends the value, which is the order the
interpreter frees them in: the cown's `ending` with the value when it has one and the value is there, else the drop
routine of `T`. A string literal stays immortal inside a frozen value, since dropping it is the string's drop.

A `when` allocates the behaviour's **record** (`backend.emitWhen`; layouts `behaviour*Offset` and `request*Offset`
of `internal/abigen`): `pending`, `run` (the address of the body's thunk), `n` (how many cowns it names), `n` request slots
`{cown, next, blocked, owner}` that the runtime fills in, then the cowns in source order and the captures, a word each,
which move into the record. It hands the record to `rt.cown.acquire` (`runtime/cown.maxon`, Verona's two-phase protocol,
Cheeseman et al., OOPSLA 2023): the slots are sorted by cown address (the same cown twice traps with
`a behaviour names the same cown twice`, as the interpreter does), the behaviour takes a hold on each cown, and in
address order each slot is exchanged into its cown's `tail`, waiting out a predecessor that is still queuing before it
links behind it; once queued on all of them, one locked add on `pending` of the cowns acquired at once, plus 1 for the
queuing, leaves it runnable or waiting for the releases of the behaviours ahead of it, and whoever brings `pending` to 0
pushes it on its own thread's queue (`schedPush`). There is no lock word: a cown's queue is the chain of request slots.
The **thunk** (`run <body>`, `backend.emitBehaviourThunk`, written with each behaviour body) calls the body with the
address of each cown's `value` and the captures, which the body drops as it drops `consumes` parameters; then
`rt.cown.release` hands each cown to the slot linked after it (or sets its `tail` back to 0 by a compare-exchange when
none is), lowering that behaviour's `pending`; then the thunk drops the behaviour's hold on each cown in source order,
which may end a cown and run its destructor on that worker, and frees the record. Behaviours that one thread schedules
on one cown run in that order; behaviours on disjoint cowns run in parallel.

### Function values in the IR

A function value is one word, the address of a **closure**: a box of a count of the values that share it, the address of
the code to call, the address of the routine that drops what it owns (0 for none), and then what it captured, a word each
(`internal/abigen`'s `closure*Offset` rows). A function named as a value is a closure too, with nothing captured and nothing
to drop: one representation means a call through any value is the same indirect call, at the price of an allocation where
a function is named as a value; a static, immortal box for it would save that and is not built.

| Instruction | Does |
|---|---|
| `%r = closure body [%a, %b]` | a new closure of the body `body` of the function, holding the captures `%a` and `%b`: it owns each that the body's `captureOwned` says it owns, which was moved out of its owner by the instruction before, and holds the others as they are, a scalar it copied, a borrow, or the address of a variable that a mutating closure changes |
| `%r = funcvalue f` | a new closure of the function `f`, which captures nothing |
| `%r = callvalue(%c, %x, %y)` | calls the code that the closure `%c` holds with `%c` first and then the arguments |

**The body is a function of its own**, in `bodies` of the function around it and printed after it, named for it with `$fn`
and its number among the bodies (`main$fn0`). It takes the closure first and its parameters after it, and reads what it
captured from the closure (`field %c+24`), which keeps it for every call: a capture is a borrow in the body, and one that a
mutating closure changes is the address of the variable, which the body reads and writes through as it does a `mutates`
parameter. Native code passes the closure last (`emitCallValue`, and `paramPosition` in the body), after the arguments in
the registers or farthest from the return address on the stack, so the code of a named function, which takes only its
parameters, never reaches it: `callvalue` needs no case for it, and the interpreter skips it for one.

Copying a function value (`copy %s F`, which `clone()`, a consumed place of a function type and the copy of what holds one are) adds 1 to the count and gives the
same box; dropping one subtracts 1, and the last runs the closure's drop routine and frees the box. Every function type has
the same two routines (`backend.dropClosure`, `copyClosure`), and the drop routine of a closure's captures is made with its
body (`backend.emitCaptureDrop`), named `captures` and the body's symbol, since its types are the body's and no function
type names them. Natively the count is stepped by one locked add (`lock xadd`), as a frozen value's is: a function value
is immutable, so a copy of it may reach another thread, as the capture of a behaviour will. A closure that owns a value
which only the interpreter can drop (a map, a list, a pool) is refused natively like one that drops it.

### Interface values in the IR

An interface value is one word, the address of a box of two words: the address of a **table** and the value, a word as any
value is (`internal/abigen`'s `iface*` rows). The table belongs to the concrete type, the interface and the module of the
conformance the value was boxed with, so its link name says all three (`table export::Square as shapes::Shape by
export`), and it is static code in the image: an entry of a word for each of the routine that drops the value, the one
that copies it (where the interface extends `Copyable`), the one that names its type, and then each method the
interface requires, its own and then those of the interfaces it extends (`ast.requirementsOf`). An entry is a jump to the
routine or the method, so calling the entry is calling it with the arguments as they are. The program is closed, so no
table is made at run time: every module that boxes a value with a conformance writes the table in its unit, and the link
keeps one, as it does an instance.

| Instruction | Does |
|---|---|
| `%r = box table, %v` | a new interface value of the table and `%v`, in a box on the heap, which owns the value: one that is consumed (a binding, a `consumes` parameter, a field, an element, a `return`) |
| `%r = framebox table, %v in frame box n` | an interface value whose box is the function's frame box `n`, two words of its frame, which owns nothing: `%v` lent to what borrows it (a `let` parameter, a receiver), which its owner keeps, or a copy of a scalar |
| `%r = callwitness.lent n(%b, %x)` | calls the method in place `n` of the table of `%b` with the value it holds and the arguments; a receiver that is `let` or `mutates` is lent the value, as a record's is |
| `%r = callwitness.moved n(%b, %x)` | the same for a receiver that is `consumes` or `destructs`: the value leaves the box, which is freed |
| `%r = istype %b T` | 1 when `%b` holds a value of the type `T`: the entry that names its type gives the address of the word of data named for `T` (`type` and its link name), which every unit that names it defines and the link keeps once |

A value only borrowed as an interface value is boxed in the caller's frame, so passing one to a `let` parameter allocates
nothing: a borrow cannot outlive the call it is lent to (`E3900`, `E3925`), the frame outlives the call, and a callee that
passes its parameter on to another borrow passes the same box. Each `framebox` has a frame box of its own, which the
backend reserves below the function's registers (`IrFunction.frameBoxes`), and which a `framebox` reached again, in a loop,
fills again.

Dropping an interface value calls its table's drop entry with the value and frees the box, and copying it makes a box on
the heap of the same table and the copy its copy entry makes, whichever box it copies: one routine for every interface
type, since the table knows the type. A scalar is dropped by nothing and copied as it is. The interpreter keeps the box as
a record of two words, the table as its index among the program's tables, and owns, lends, drops and copies it by shape,
as it does any record. A frame box is a record its frame makes the first time it reaches the `framebox` and that ends
with the frame: it is no heap allocation, so it is in no order of frees, and a use of it after its frame has returned is a
use after free. The value of a
type that only the interpreter runs is refused natively where it is boxed.

### Drops

The IR says where every owner dies. Lowering emits a drop wherever the rules of "Ownership" free a value, and both the
interpreter and native code free exactly there.

| Instruction | Drops |
|---|---|
| `dropslot s2 T` | what slot `s2` owns, if anything, and leaves it empty: a local at the end of its block, on every way out of it (fallthrough, `return`, `break`, `continue`), and the old value of a local that is assigned |
| `dropat %a T` | what the address holds, if anything: the old value of a field, an element or a `mutates` or `sets` parameter that is assigned |
| `drop %r T` | the value in a register: a temporary that nothing took, an element removed and discarded, the key and the value that a failed `Map.insert` or `Map.replace` was given, the value given to `array.filled` |
| `%r = move s2` | reads a slot and leaves it empty: a whole local that moves (a binding, a `consumes` argument, a return, a field, a branch of a conditional) |
| `%r = moveat %a` | reads the value at an address and leaves it empty: the payload that leaves the box of a union that `match consumes` takes apart, the String that leaves each element of the array `Directory.list` gives, for the `FilePath` made of it to take its place, and the value that `exchange` takes from a place |
| `%r = copy %s T` | a deep copy of the owned value in `%s`, which stays where it is: a new box for each record, union and array inside it, a new string for each string that is not a literal, and another owner of the same box for a frozen value or a cown |

`T` is the type with an instance's type arguments applied, so the backend can call the drop routine of that type. Dropping a value
drops what it owns, deeply by type: the fields of a record, the payload of a union, the elements of a primitive. A borrow is never
dropped: a `let` or `mutates` parameter, a projection, and a `match` binding of a payload, which is stored with
`store s3, %r borrow`. A place that holds nothing, because it was moved from or never assigned, drops as a no-op, so one drop at
the end of a scope serves every path to it and no analysis of paths is needed. A function that drops a value that holds a map, a
list or a pool is refused natively like one that makes it.

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
  from, or never assigned, or whose `dropslot` has run, holds 0. Only the slots that SSA construction leaves in the frame are
  there to zero: a promoted slot's emptiness is a `const 0` where it is read, and a `drop` of a register that a 0 reaches
  through a block parameter does nothing. That includes a slot that a loop body declares: its `dropslot`
  at the end of the body leaves it empty for the next pass. `dropslot` and `dropat` leave the place 0 too. A register is always
  written before it is dropped on every path, so a frame does not zero registers; the interpreter, which runs the IR as
  lowered, reports a drop of a register that holds nothing, which is how a hole in that would show.
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
- **The heap** is `runtime/heap.maxon`, one per thread: a record that the thread's context names and its first alloc makes,
  so `runtime::alloc` and `runtime::free` take no lock and write no word that another thread writes (the process heap's lock
  serialized the workers, and its calls were about 40% of a build). A box of up to 8 KB is a slot of a size class (one per
  word up to 256 bytes, then four per power of two up to 8 KB), carved from 1 MB chunks of the process heap that the heap's
  classes share, and a larger box is a block of the process heap of its own. A word before every box holds its heap's
  record, aligned to 512 bytes, plus its class's key, so `free` (which skips 0) finds both from the box alone and pushes the
  box's address on the class's stack, which `alloc` pops and zeroes; the stacks are apart from the slots, so an alloc reads
  nothing of a slot that may have left the cache. A behaviour may free what another worker allocated: a box of another heap
  goes on that heap's list of remote frees with a compare-exchange, linked through the dead box, and the owner takes the
  whole list with one exchange when a class of its runs out, as mimalloc's thread-free list works; only the owner takes, and
  takes everything, so no push meets a box taken and pushed again. A mask over the address would save the header word, but
  HeapAlloc gives no alignment to a chunk's size, so a large box would carry up to a chunk of padding or every free a lookup
  in a map of the address space. On Linux a large box's block is pages of its own, whole pages long, and a freed one is kept
  by the freeing thread's heap (up to 32 blocks and 4 MB, the one kept longest given back first to make room), and the
  shortest kept block that holds a large box and is at most a quarter longer serves it, its box zeroed; so a program that
  grows and drops buffers maps and unmaps no pages for each (the array-indexing benchmark made 1,875 mmaps, now 61, and
  `build self` 6,782, now about 3,800). Windows' process heap keeps freed blocks itself, and a heap of its own showed no
  gain there.
- **Counting allocations.** The runtime knows the heap boxes that are allocated and not freed in every executable, so the
  runtime is the same code whatever the build: each heap counts the slots it carves and the large boxes it holds, less those
  it frees, and the live boxes are the sum over the threads' heaps less the slots on their stacks and lists
  (`runtime::liveBoxes`), so a box of a class costs no count; `checkLeaks` sums them when no behaviour is left.
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
  behaviours, and requires the same stdout and exit code as the directives and no violation. `self/interp/interp.maxtest` tests it on
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
  The programs with no map, list or pool, which are the ones with an executable, share in half the seeds, and those are
  quiet: only main's own statements print (a print anywhere else only evaluates its string) and only they schedule
  behaviours, so the behaviours on each cown run in main's order and the output and the exit code are the same under
  every schedule, the executable's and the interpreter's alike. Such a program's executable runs 3 times, under the
  worker count of the fuzzer's environment; when the run traps, a behaviour may have trapped while main was still
  printing, so the two outputs need only agree up to where the shorter ends. A program that schedules behaviours and is
  not quiet is not compared.

### The machine IR

A machine IR lies between the IR and the encoder (`self/backend/mir.maxon`): `emitModule` selects each function into it,
allocates it and encodes it, and `selftest` also runs it on functions written by hand. A function (`MirFunction`) is blocks
of fixed-size instructions over virtual registers, in SSA form: a block is a range of `instrs` with a range of
`blockParams`, a branch names an edge (`MEdge`: a target block and a range of `edgeArgs`), and the encoder lays the blocks
out in `order`, the order they are written in. A block can be made before it is written (`addBlock`, then `writeBlock`),
so that IR block b is machine block b and a branch can name a block that comes later; selection writes a prologue block
first, so that a branch to the IR's first block does not run it again, and the blocks a helper's branches need where they
are. Every register of the IR is the virtual register of its number (`vregOf`), so a block's parameters and an edge's
arguments are those. A conditional branch whose edge has arguments ends its block, before the jump that does; one in the
middle of a block goes to a failure block, with no arguments, as the IR's own do. The frame below the frame pointer holds
first the `fixedSlots` that selection gives the IR's slots and frame boxes, and then the spill slots allocation adds. Each
block has a loop depth and a likelihood (`cold` for a failing check) for the passes that will read them. A virtual register's class
(`RegClass`: `gpr` or `xmm`) is in `classes`, from `firstVirtual` (64) on;
an operand below that is a physical register by its number in the register file, which is how selection fixes a value to a
register: it copies the value into, say, rcx before a shift by `cl`, and the allocator sees the fixed register at the copy.

An instruction (`MInstr`, 32 bytes when packed: kind, reference kind, form, condition, three register operands `a b c`, a
32-bit `aux` and a 64-bit `imm`) is an x64 form (`MKind.target`) or a pseudo-instruction: `copy`, `parallelCopy` (a range
of `copies`, every source read before any target is written), `spill` and `reload` against a spill slot, `restore` (the end of a bracket that a `spill` of the same register opened:
the value comes back into its own register, which the instructions between were lent), and `frameSetup`
and `frameTeardown`. A form's register operands are in one order: the register it defines, then the one a two-address form
reads and writes (which the definition must be given the register of: `reuses`), then the ones it reads. `aux` is a
memory operand's displacement, or what the reference of a form's last 32-bit field names (`MRef`: an edge, a symbol of the
function's `symbols`, a data offset, an import), which the encoder turns into the same label uses and fixups as the rest
of the backend. `regs_x64.maxon` holds the constraints the allocator reads, never the encodings: `operandCount`,
`defines`, `reuses`, `implicitUses` (a form's fixed uses, and the result registers at a return) and `implicitDefs` (fixed
results, clobbers and a call's convention), the register file's masks (`classMask`, `allocatable`, `reservedMask`) and the
calling conventions (`CallConvention`: the argument registers a call reads, and what it keeps), built from the
target table's declarations (`regs_x64_gen.maxon`): the internal one, Win64 for an import and Linux's for a `syscall`, whose
form is a call of that convention (`callConvention`). A call's `imm` is the
registers its arguments are in, which `implicitUses` counts as read by it (`newCall`), and a return reads the result
register of its function's class (`MirFunction.floatResult`).

Before allocation, `DeadCode` (`dead.maxon`) has every read of a virtual register that a copy of another one defines read
that one instead (in SSA form the two hold one value wherever the copy's is live, so no point holds more values; a read of
a parameter is such a copy), then removes the instructions whose only effect is a virtual register nothing reads, or
flags that nothing reads before they are written again (the flags are live into a block whose first instruction that
touches them reads them, or that touches none), and no other effect: no store, call, return, fixed register or branch. A selection rule that fuses an operation into its one reader (`Selector.fuses`: the reader is the only one, and the
operation's instruction comes right before it in its block, so nothing between writes the flags or memory and no operand
lives longer) leaves the operation's own code, selected at its place first, with no reader, and this pass takes it out: a
constant made an immediate, a comparison made into a branch's flags, a length compared in its box, an address made the
operand of its load or store.

**Register classes** (`classes.maxon`): the IR keeps no types, so selection gives each IR register its class by what reads
and writes it (`ClassChoice`): a float operation wants its operands and its result in xmm registers, a constant, a load
and a store take either, a call's arguments and result, a parameter and a returned value the class their float signature
passes them in, and every other operation a general one; a block's parameters and the arguments passed to them are one
class, the one more of their reads and writes want, a tie general.
The rules then pick the xmm forms of a constant (`movsd` from a word of the unit's data, `DataSection.wordOffset`), a load
and a store (`movsd`) for a value in an xmm register. Before the dead code removal, `legalizeClasses` moves each value that
an operand of the other class reads or writes after all (a float's bits that an integer operation reads, a float that a
runtime entry takes as a word) by `movq`, so the analysis decides how many moves there are, never what the code computes. Liveness, pressure, spilling, E5001 and the colouring and its repair are per class, each against its
own registers; a float is spilled to a word slot by `movsd`, an xmm copy is `movaps`, SSA destruction exchanges two xmm
registers with `xorps`, and the callee-saved `xmm6` to `xmm15` that a function's values get are saved whole by `movups`
below its slots in the prologue and restored before each return. A float argument, result or parameter is in the internal
convention's float registers (see "Floats across calls"), and a float live across a call stays in `xmm6` to `xmm15` as
an integer does in `rbx` and `r12` to `r15`.

`emitModule` allocates each function it selects with an `Allocator` the module's functions share (`regalloc.maxon`):
spilling, then colouring, below. The frame holds, from the frame pointer down, the slots given before allocation
(`slotsGiven`: the `fixedSlots`, then the slots of block parameters in memory and those spilling gave out), then the
callee-saved registers the function's values were given (`savedRegs`), pushed below them so that `rsp` is 16-byte aligned at
a call. A leaf with no slots, no spill and no call that names neither `rbp` nor `rsp` has no frame: its prologue only pushes
those registers and its epilogue pops them. `encodeMachineFunction` writes an allocated function into a code unit, laid out
by a `BlockLayout` (`branches.maxon`): a block that only jumps on is left out and every branch to it goes where its chain of
jumps ends; the cold blocks (those of IR blocks that end in a trap: a failing check's) come after the others, so the hot path runs
straight on; a jump to the block laid out next is left out, and a conditional branch to it over a jump becomes one branch
the other way. Where the flags are dead after it, a constant 0 is the 32-bit `xor` of the register with itself, and a copy
followed by the addition or subtraction of a constant to the copied register (the copy allocation makes of a two-address
form's operand that lives on) is one `lea`.

Code is placed by its lines (`align.maxon`): x64 fetches code and caches its decoded operations by 64-byte lines, and code
that runs through one more line than its length needs costs that line each time it runs (on a Zen 3 the same loop took 150
or 195 ms, and the same recursive function 140 or 152 ms, by where the line breaks fell). The head of a loop as the code runs
it (a block that a hot block laid out at or after it branches back to) is padded with the fewest of the x64 table's
recommended multi-byte nops to the start of the next line when that has the loop, from its head to the end of the last block
that branches back, span one line fewer (`fitLine`); a function with a loop is encoded twice for that, first without padding
to measure its loops, and its section starts on a line, which the padding is computed from. The link starts every section
on 16 bytes, and moves one to the next line when that has it span one line fewer; it starts each unit, and the image its
code, on a line, and fills the gaps with `int3`. A section carries the boundary it asks for (`CodeSection.alignment`), which
the build cache keeps with it. Against none of this, the code of `self` is 7% larger and the spread of a loop's time over
where it lands went from 48 ms to 3 ms.

#### Liveness, spilling and E5001

The first half of the register allocator (`liveness.maxon`, `pressure.maxon`, `spill.maxon`) brings every instruction of a
function under the registers free there, which the second half, colouring in dominance order, needs. Every function a
native build compiles goes through it; `selftest` also runs it on hand-built functions (`self/backend/spill.maxtest`), which
colouring then finishes and which run.

- **Blocks** (`prepareBlocks`): the block laid out first, selection's prologue, becomes block 0, the graph's entry; the
  layout becomes the reverse postorder, which keeps it first and leaves each block's instructions as they are, so a
  conditional branch with arguments still ends its block before its jump; an edge out of a block with more than one
  successor is split into a block of its own when its target has more than one predecessor or it has arguments; every
  loop gets a preheader; then the instructions are written in the layout's order (`writeLayout`), so that an
  instruction's index orders it as the layout does, which the walks along a block and the use lists read. Dominators and
  natural loops come from `self/ir/graph.maxon`, the graph algorithms that
  `ir.Flow` uses too. A block is cold when it is marked so (a failing check) or a cold block dominates it; an ordinary
  `if` arm is not, however rarely it runs, since nothing says so.
- **Block parameters in memory** (`paramsToMemory`): a block with more parameters of a class than the class has registers
  could neither start with them in registers nor have an edge into it read all its arguments at its jump. The parameters
  past the registers, those whose uses weigh least by loop depth, are taken off it, each with a frame slot of its own:
  every edge into the block stores the argument into the slot before its jump, and the block starts by reloading the
  parameter's value from it, a value spilling takes as stored, so it stays in a register while there is room and is reloaded
  where there is not, and a reload nothing reads is dropped. An edge of a loop that passes one of its header's memory
  parameters copies it first, so no store overwrites a slot that a later reload of the same edge reads. So an argument goes
  register to slot at its jump and slot to register in the block, and from slot to slot through a register.
- **Liveness** (`Liveness`): per value, a walk back from each use through predecessors to its definition, with no
  fixpoint (SSA: the definition dominates the uses). An edge's arguments are uses at its branch and a block's
  parameters definitions where it starts. It gives each value's uses in layout order and each block's live-in values.
- **Pressure** (`BlockScan`): at each instruction, the values live across it plus the larger of those it reads last and
  those it defines (a definition may take a dying operand's register; a dead definition still needs one), or both where
  a two-address definition, a copy of its reused operand before the instruction, may take no dying operand's register:
  the reused operand lives on, and the form does not commute or its other use lives on too (`sharesDying`). That is per
  class, against the class's registers no fixed value holds around it; the values live across it
  must also fit outside the registers it writes (a call keeps only its convention's callee-saved registers, read from
  the convention table).
- **Tiers**, each one placement and no search: (1) a loop over its class's whole pool sends out the values it holds idle
  (live across it, neither defined nor read on its hot path), farthest next use first, outermost loop first, so a value
  that only an outer loop reads leaves the inner loop and is reloaded in the outer one; (2) constants over a hot point
  are made again before their uses, but one the next instruction reads, which would be made again at the same point;
  (3) on a hot path, the values live across a call past the registers of their class its convention keeps are spilled
  at the call, or at a point over the whole pool before the call where their next use is past it (a later call in the
  loop, by the layout) while more of them are in registers than the call keeps: they are in memory across the call
  whatever the program does, so that spill code costs nothing the call does not and is no reason to refuse; (4) the E5001 test; (5) everywhere, a point still over its free registers spills by
  farthest next use, constants first, then values already stored or defined outside the loop; in a cold block, a value
  live across a call, or one the loop's hot path reads, is bracketed instead: stored before the point and restored into
  its own register after it, which lends its register to the instructions between, so the hot path keeps it.
- **A split** stores the value once, after its definition, and gives each use the cut reaches a reload (or the constant
  made again), one per block and stretch of uses, hoisted to the preheader of every loop that holds neither the cut nor
  the definition. A reload defines a new value that dominates its uses, so the function stays SSA with no new
  parameter. The work is in rounds of analysis and one sweep each; an analysis is reused while nothing changed, and a
  function that fits costs one. A round's analysis after the first starts from the one before, since edits change only
  instructions within blocks: the graph is kept and only the block of each instruction found again; the operands of an
  instruction no edit touched, and the live blocks of a value whose definition and uses no edit touched, are taken over;
  and only a block whose instructions or live-out values changed is surveyed for pressure again.
- **The invariant** (`verifyPressure`, which `--verify-ir` holds every function to): every virtual register has one
  definition; at every instruction each class's demand fits its free registers; and spill code in a hot block lies only
  in loops that define or read its value (a slot is one value). So a store runs where the definition does and a reload
  no deeper than its uses.
- **E5001** (`refuseHotLoops`, a `PressureRefusal` per loop nest and class, which the emit stage reports as a compile error
  at the function's declaration, since the IR keeps no place of a loop, and an instance's at its generic's): after tiers 1
  to 3, a point of a loop's hot path over its class's whole pool. It names the loop,
  the target, the class's register count, the registers over at the worst point, and how many of the source's variables
  (values joined by edges) must leave the loop: found by taking out, one at a time, the variable that relieves the most
  excess, where a variable out of registers still needs one at each instruction that reads or writes it. That can be
  more than the registers over: sixteen accumulators, a counter and a bound are 19 registers at the worst point, five
  over x64's 14, yet six must go, since an accumulator in memory frees nothing at its own update. A fixed register, a
  call or a two-address definition's register of its own never refuses (the test counts values, the larger of those
  read last and defined); it is spilled around.

#### Colouring, constraints and SSA destruction

The second half (`regalloc.maxon`, `ssa_destruction.maxon`) gives every virtual register of a spilled function one physical
register and takes the function out of SSA form; `Allocator.allocate` runs both halves, `colourFunction` after
`spillFunction`, on every function a native build compiles. `selftest` also runs it on the kernels of
`self/backend/colour.maxtest` and on every function of the machine IR's and spilling's tests, each held to the checker and run.

- **What a value may not be in** (`survey`, one walk back through each block): the registers written by an instruction it
  is live across (a call's caller-saved ones, from the convention table; a division's rax and rdx) and the physical
  registers holding a fixed value while it is live (rcx from the copy of a shift's count to the shift). Inside a bracket the
  bracketed value is out of its register, so nothing there confines it; a value defined inside a bracket and live past its
  restore may not take the register the bracket lends.
- **The sweep** (`sweepBlock`): blocks in the layout's reverse postorder, the values live in in their registers, then the
  parameters, then each instruction's definitions; a definition may take the register of a value that dies at it. Since the
  definitions dominate their uses, the values live where one is defined are coloured already, and the sweep needs no more
  registers than the most live at a point.
- **The choice** (`choose`), strongest first: the fixed register the value is copied to or from; the register of a value it
  is copied to or from (an edge's argument and its parameter, a two-address operand, a copy), so the copy disappears; that
  partner's fixed register; then the lowest free caller-saved register that values defined in its life do not want as their
  fixed register, and only then a callee-saved one, which a value live across a call is confined to anyway.
- **Two-address forms** (`colourReuse`): the definition takes the reused operand's register when that operand dies there; for
  a commutative form, the other use's when that one dies (the uses are swapped); otherwise another register, never one the
  instruction reads, and a copy of the operand before it, which pressure has counted beside the dying operands.
- **The repair** (`repair`): when every register a value may be in is held (the count fits, but the values about it are
  confined to different sets: Hall's condition), first a breadth-first search from those registers to a free one, through
  values that die in this block, each of which may move to the next register for the rest of its life in the block and
  gives its register up to the one before it on the path. Each move is one copy before the instruction and a window in
  which the value is in the new register.
- **Homes and windows** (`homeElsewhere`, `relocate`, `reconcile`): failing the search, which the confinements of values
  that live on past the block can make fail though the point fits, the value is given as its register (its home) one that
  no value live there has, which the count guarantees, preferring one it may be in: it starts its life in another register
  while its home is held, by a value that gave its own up, or a two-address definition takes its dying operand's register
  though it may not be in it somewhere. A value outside its home, or whose home is a register it may not be in somewhere,
  is watched: before an instruction that fixes or clobbers its register while it lives across it, it moves, by one parallel
  copy with the others that must, to a free register that suits, or to the register of a value that dies there, which moves
  out of the way; before every branch each value out of its home goes back, by one parallel copy, and a value that gave its
  home up moves out of a home it is in. So every block is entered with every value in its home, no parameter is added, and
  spilling's bound (the values live across an instruction fit the registers it leaves) makes every move possible: a value
  with nowhere to go is an internal error, never a program's.
- **SSA destruction**: each edge's arguments become one parallel copy before its jump into the registers of the target's
  parameters (a parameter nothing reads gets none), sequenced so no copy overwrites a source another still reads; a cycle
  is broken through a register that holds nothing the copy or the target's live values need, caller-saved first, or, when
  every register holds one, by exchanging two registers with three xors. A copy whose source and target got one register is
  dropped, and so is a `copy` whose two sides did.
- **The frame**: the callee-saved registers the function's values were given are `savedRegs`; the encoder pushes them below
  the spill slots in the prologue and pops them before each epilogue, which then drops the frame with `leave`.
- **The checker** (`checkColouring`, one walk): it runs the coloured function beside the original on tokens: a register holds
  a token, a copy moves one, an exchange swaps two, and what an instruction writes without a value gets a new one. Wherever
  the original reads a value, a fixed register or an edge's argument, or a block's live values and parameters are entered,
  the register the coloured function reads must hold the same token. That says that no two live values share a register,
  every fixed constraint holds and every edge delivers its arguments.

Measured on the kernels of the allocator study (moves and stack operations per iteration of the inner loop; Maxon's figures
from the study's kernels, ours from the same shapes in machine IR): a counted loop 0 moves (Maxon 1, Go 1); nested loops 0;
seven accumulators with the counter, the bound and the half, ten live, 7 moves, one per shift of an accumulator that lives
on (Maxon 12, Go 4); the branchy loop 1 move on each arm, the test of the counter's bit (Maxon 4 and 7, Go 4 and 6); a
division and a variable shift 2 moves; two parameters that swap at the back edge 3; a call with 2, 4 and 8 values live
across it under the internal convention, which keeps five callee-saved registers, 0, 0 and 5 stack operations with 1, 1
and 3 moves, the one the copy of the argument into its register (Maxon 0, 0 and 6 stack operations, 3, 5 and 14 moves), and
under a convention that keeps nothing 3, 7 and 15 stack operations.

Against the trivial allocator that every build used before (a frame slot per virtual register), the programs of
`tests/bench` have 42 to 44% less code and run 1.0 to 2.9 times as fast (the counted loop is the division's), the
compiler's own code is 44% smaller (3.57 MB of `.text`), and a build of `self/` spends about 3.8 s of its emit stage
allocating, against about 0.9 s for the whole emit stage before, which the faster code it runs on more than half repays.

## The runtime

The runtime is Maxon source: the `runtime/` directory at the repository root, beside `self/`, one module that is compiled
with every program, so a change to it is tested by the next compile with no rebuild of the compiler. Only what is derived from
the program's types stays compiler-emitted: the drop and copy routines and the entry stub (`self/backend/routines.maxon`,
`entry.maxon`). Both are written in x64 by the internal convention: a routine takes its value in the first argument
register, which its prologue stores in its frame, returns a copy in the result register, and works only in registers a
call may leave holding anything, so it saves none; the stub passes `main`'s exit code to `checkLeaks`, and its result on,
in the same registers. `runtime/README.md` lists the files and the rules a runtime file follows.

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
| `__Raw.heapAddress()`, `stdoutAddress()`, `stderrAddress()` | the address of a global of the entry stub |
| `__Raw.callImport0(name)` to `__Raw.callImport10(name, arg1: a, ..., arg10: j)` | calls the DLL function `name`, a string literal (`E2004` otherwise), with that many words by the Win64 convention, and gives what it returns; Windows only |
| `__Raw.syscall0(number)` to `__Raw.syscall6(number, arg1: a, ..., arg6: f)` | makes the system call of the number with that many words by Linux's system call convention, and gives what the kernel returns, `-errno` as a word when it failed; Linux only |
| `__Raw.processStartAddress()` | the address of `g.processStart`, where the Linux entry keeps the stack pointer the process started with: the argument count, the arguments, a null, the environment |
| `__Raw.atomicAdd(address, offset: o, value: v)`, `__Raw.atomicExchange(address, offset: o, value: v)` | adds `v` to the word at `address + o`, or puts `v` there, as one locked step (a full fence), and gives the word it found |
| `__Raw.compareExchange(address, offset: o, expected: e, desired: d)` | puts `d` in the word at `address + o` when `e` is there, as one locked step, and gives the word it found: it succeeded when that is `e` |
| `__Raw.spinPause()` | `pause`, in a loop that waits for another thread |
| `__Raw.threadContext()` | the running thread's context: the word in the runtime's thread-local slot |
| `__Raw.callCode1(code, arg1: a)` | calls the code at the address `code` with one word by the internal convention, as a behaviour's thunk is called with its record, and gives what it returns |
| `__Raw.callHost2(code, arg1: a, arg2: b)` | calls the code at the address `code` with two words by the host convention, Win64, as a DLL import is called: a function of a library that the import table does not name, which the runtime found by its name (`LoadLibraryA`, `GetProcAddress`); Windows only |
| `__Raw.threadStartAddress()` | the address of `rt.threadStart`: on Windows the code a thread that `CreateThread` makes starts at, and on Linux the routine that clones the worker's thread (see "Threads") |
| `__Raw.codeAddress(name)` | the address of the code of the function of the module that `name`, a string literal, names (`E2004` otherwise, and `E3004` when there is none): what a `callCode1` calls, as a test of the scheduler builds a behaviour's `run` |
| `__Raw.stackSwitchAddress()`, `greenStartAddress()`, `asyncPreemptAddress()` | the address of the entry stub's `rt.stackSwitch`, `rt.greenStart` and `rt.asyncPreempt` (see "Green threads") |
| `__Raw.preemptHandlerAddress()`, `signalReturnAddress()` | on Linux, the address of the stub's handler of the preemption signal, `rt.preemptHandler`, and of the restorer it returns through, `rt.sigreturn` |
| `__Raw.preemptibleCodeAddress()` | the address of the link's table of the code a green thread may be stopped in, `g.preemptibleCode` (see "Green threads") |

Only the runtime module may call them: a call anywhere else is `E3152` (maxon's `rawIntrinsicOutsideRuntimeTier`, with
its message), which `callFunction` in `self/check/calls.maxon` raises from the rows' runtime-only flag
(`ast.intrinsicRuntimeOnly`). They lower to IR operations that only the runtime's code has: `loadraw.W`
and `storeraw.W`, W the width in bits, at the address plus the offset; `globaladdr` of an `Rt` global; `import`,
which names the DLL function among the function's strings; `syscall`, a call of the system call convention; `atomicadd`, `atomicxchg` and `cmpxchg` at the address plus
the offset (`lock xadd`, `xchg`, `lock cmpxchg`); `spinpause`; `threadcontext`, which on Windows reads the slot straight from the
TEB (`mov rax, gs:[rcx + 0x1480]`, `rcx` the slot's offset in `g.tlsOffset`) and on Linux the word that `fs` points at
(`mov rax, fs:[0]`), maxon's thread pointer; `callcode`, an indirect call; and `callhost`, an indirect call by the host convention, which
`emitHostCall` sets up as it does an import's. Each
atomic operation and `spinpause` reads and writes memory, so no pass moves a load or a store across one; on x64 a plain
load is an acquire and a plain store a release, so these are all the scheduler needs. The interpreter never runs the
runtime's source, so it panics on each of them. The wrapping arithmetic is an unchecked `bin`, `addressOf`
the `addr` of the local's slot, and `stringAddress` and `stringAt` the word itself. `self/backend` writes them
(`visitLoadRaw`, `visitStoreRaw`, `visitGlobalAddr`, `emitSyscall`, and `emitCallImport`, which aligns the stack itself, since a Maxon
function's frame is not aligned, and keeps `rsp` in a value that allocation puts in a register Win64 keeps).

**From an entry to a function.** The runtime entries the compiler calls are the rows of the table's `entries`
(`ir.Rt`), and `runtimeFunctions` names the function of `runtime/` that defines each one with its signature
(`alloc(bytes Count) Count`, `write(handle Count, text String)`, ...). From it the generator writes `ir.rtFunctions` and
`ir.rtSymbol`, which gives a mapped entry its function's link name (`runtime::alloc`) and an entry that
the stub or the linker defines its own symbol (`rt.entry`, `g.heap`); the linker resolves `rtSymbol(x)` by that text as
it does any call. The two calling conventions are one by construction: a `callruntime` and a `call` of a Maxon function
both pass their arguments through `Selector.callForm` (`self/backend/isel.maxon`) by the internal convention, a
`callruntime` all of them as words (`Selector.callWords`), and the callee returns a word in `r8`. So a runtime function
takes only words (an integer, a `bool`, a `String` as
its box address), takes none `mutates`, is not generic and throws nothing, since a function that throws returns its
error in `r10` too. `minimaxon check runtime` holds the runtime to the table: each
mapped function exists with exactly that signature (`E3004` when it is missing, `E3005` when it differs) and links as
`rtSymbol` says. A new entry is a row of each table and a function in `runtime/`.

**Globals.** The entry stub keeps the process heap and the two standard handles (`g.heap`, `g.stdout`, `g.stderr`),
which it sets before `main`, on Linux the standard handles, fds 1 and 2, and the stack pointer the process started with
(`g.processStart`), and no heap; the linker defines `g.imageDataStart` and `g.imageDataEnd`. Runtime source reaches a global through its address intrinsic.
The runtime is the same code whatever the build: each thread's heap always knows its live boxes, and `--count-allocations` changes only
the stub, which then passes the exit code to the runtime's `checkLeaks`, which sums the live boxes of the threads' heaps;
that exits with the leak report and 99 when
a box is live, and gives the code back otherwise. Behaviour, exit codes and messages are maxon's runtime's.

**Threads.** Each thread keeps its context, a record of `contextBytes` (`abigen`), in one thread-local slot. Before
`main` the stub takes it with `TlsAlloc` into `g.tlsSlot`, panics when it is not one of the TEB's 64 inline slots (as
maxon's `emitRequireTebTlsSlot` does, `TLS_OUT_OF_INDEXES` included), keeps its offset in the TEB's slot array in
`g.tlsOffset`, and stores the main thread's context, the global `g.mainContext`, with `TlsSetValue`; a program without
the scheduler needs it too, since the heap is the thread's. Then it calls `rt.runMain` (see "Main with or without the
scheduler"). A thread the runtime makes starts at `rt.threadStart`, which `CreateThread` calls by Win64 with the thread's
context: it saves the registers Win64 keeps that the internal convention does not (`rsi`, `rdi`) and the one it holds
the context in (`rbx`), keeps the fault handler's stack (`SetThreadStackGuarantee`), stores the
context with `TlsSetValue` and calls the runtime's `workerMain` with it. `abigen` also has the layouts of a frozen
value's box, a cown and a behaviour's record with its request slots, the scheduler's record, a processor, a green thread's
record, a pooled stack and a thread's block.

**Threads on Linux.** The context of a thread is the word that its `fs` points at, as maxon's runtime keeps its slot block: the entry
makes `fs` point at `g.tlsSlot`, which holds the address of `g.mainContext` (`arch_prctl(ARCH_SET_FS)`), and moves the main
thread to a stack of the same 256 MB reserve that a worker has (`mmap` with `MAP_NORESERVE`, its lowest page the guard), since
the kernel's is as large as the shell's soft limit. To start a worker the runtime maps its stack the same way, stores the
context in the stack's top word and calls `rt.threadStart` with that word's address (`callCode1`), which clones the thread
(`CLONE_VM | CLONE_FS | CLONE_FILES | CLONE_SIGHAND | CLONE_THREAD | CLONE_SETTLS`, maxon's worker flags but the join) with the
word as both its stack and its thread pointer; the new thread starts after the `clone`, gives itself a signal stack and calls
`workerMain` with the word it finds at its stack pointer. A thread with no processor waits on a futex on its context's stopped
flag in place of an event (`FUTEX_WAIT_PRIVATE` while the flag is 1, and a waker's `FUTEX_WAKE_PRIVATE` after it puts 0 there, so
a wake before the wait makes the wait return at once, as a set event does), yields with `sched_yield`, and counts the
processors by its affinity mask (`sched_getaffinity`), as maxon's does. The process ends with `exit_group`.

**Green threads.** The compiler holds what green threads need of it, and the runtime schedules them (`runtime/README.md`,
"The scheduler and Go's"). A thread's context is Go's M, the OS thread: beside its scheduler, the link of every such
thread, its event, heap and random state it has the processor it runs (Go's P), the green thread it runs, its block (the
switch record of its scheduler, Go's g0, and the room its clock and its poll go through), the pair of switch records a
switch is given, whether it is in a blocking call, whether it spins, whether it is the main thread's drain, the pending
preemption, what stops the thread (a handle on Windows, the thread id on Linux), the code `rt.asyncPreempt` and the
preemption signal call, the processor it is handed, the idle list's link, and the code a thread the runtime starts runs
(`contextStartCodeOffset`, which `workerMain` calls with the context in place of the scheduler when it is set).
`contextBytes` is 22 words, which the entry reserves for the main thread, so the stub of the compiler that built a
program fixes it and the offsets the stub reads. The entry stub of either system has three routines for them, each a section that the
link keeps only when the runtime's code names it (`self/backend/green.maxon`):

- `rt.stackSwitch`, called by the internal convention with the address of the context's pair of switch records, saves
  the registers the internal convention keeps (rbx, r12 to r15, xmm6 to xmm15) and rbp and rsp into the first, on Windows
  also the TEB's bounds of the running stack (StackBase, StackLimit and DeallocationStack, which the kernel grows a stack
  and raises its overflow by), loads the second's, and returns on its stack with the first record: to the switch that
  left it, or to `rt.greenStart` on a stack that has not run. A switch is a call to the code that makes it, so it keeps
  what a call keeps and no more, as maxon's `__gt_context_switch` does; the switch record's layout (`switch*Offset`) is
  held to the convention's kept registers when the stub is written.
- `rt.greenStart` calls the code in r12 with the word in rbx, which the runtime seeds a new stack's record with.
- `rt.asyncPreempt` is where the monitor makes a green thread it stops return to, with the stack pointer a word below
  the one it stopped at (Go's `asyncPreempt`): it saves the flags and every register, xmm0 to xmm15 whole, calls the code
  at `contextPreemptCodeOffset` with the thread's context, and returns with every register as it was, through that word.
  No code writes below the stack pointer, so the word holds nothing. The stop does not write it: it keeps the address it
  stopped at in the green thread's record (`resumeAt`), and the code the stub calls writes it, on the stopped thread,
  once the stub's pushes below it have grown the stack (`asyncPreempted`). The word may be in the stack's guard page,
  and Windows grows a stack through its guard page only for the thread whose stack it is, by its TEB's bounds: the
  monitor's own write there raised a guard page violation on the monitor's thread, which ended the process with
  0x80000001. On Linux the stub also has `rt.preemptHandler`, the
  handler of the preemption signal (SIGURG, as Go's), which calls the code at `contextSignalCodeOffset` with the signal's
  context, which may rewrite its rsp and rip to return into `rt.asyncPreempt`; on Windows the monitor suspends the thread
  and sets its registers (`SuspendThread`, `GetThreadContext`, `SetThreadContext`, `ResumeThread`, kernel32 imports).

The monitor may stop a green thread only in code that holds nothing of the OS thread it runs on, which is maxon's and
Go's rule that the runtime's own functions are not asynchronously preemptible: a section of code is preemptible when it
is a function of a program's module, or of a test of the runtime, and not when it is of the runtime's own source, a
drop or copy routine, or the stub (`CodeSection.preemptible`, set by `ast.preemptibleFunction` and kept in the build
cache). The leaf inliner holds to the same rule: it copies no leaf of the runtime's own source into a preemptible function
(`opt.Preemptible`), whose copy would make that code preemptible.
Code outside the image, a DLL's, never is. When live code names `g.preemptibleCode`, the link writes after the data of
every unit the table of the preemptible code: the count of ranges, then each range's start and end as how far below the
table they are, sections next to each other in the layout as one range, which the monitor searches with the address it
stopped a thread at. `tests/runtime/stack-switch.maxon` switches between two stacks by hand, reads the table, and stops
on both systems a thread of its own, which holds no processor and so is never stopped by the monitor.

**The scheduler** (`runtime/sched.maxon`, `green.maxon`, `monitor.maxon`, `timers.maxon`, `poll.maxon`) is Go's: green
threads (G) run by threads (M) that each hold a processor (P) while they run one. There are as many processors as
`GetActiveProcessorCount(ALL_PROCESSOR_GROUPS)` says (1 when it says 0) or as `MAXON_MAX_PROCS` asks (a decimal count from 1
to 2^31 - 1 with an optional `+`, as maxon reads it; any other value is ignored); `Scheduler.processorCount()` is that count
(`osProcessorCount`), the same before the first behaviour as after it. A processor's record (2 KB, most of it its run queue)
is made when a thread first takes it (`makeProc`), so a program that keeps one busy holds one; those not yet made count as
idle. The main thread is thread 0 with processor 0 from `mainRun` on, which also starts the monitor; other threads are
started by work, each with the main thread's 256 MB stack reserve. A behaviour is a green thread:
`schedPush` gives its record a green thread that calls its `run` with the record, and counts it finished when that returns,
so the code at `run` must have released and freed what the behaviour holds by then.

- **Run queues.** Each processor has Go's local run queue, a ring of 256 that its owner puts at the tail of and that the
  owner and thieves take from the head of with a compare-exchange, and runnext, the green thread readied last, which runs
  next, so the work a green thread just made runs on the core that has its data; a full ring sends half of itself to the
  global run queue, under the scheduler's lock, which a processor takes from once in 61 schedules and when its own is
  empty. A thread with nothing to run spins, stealing half of another processor's ring (at most half the busy processors'
  worth of threads spin), and then gives its processor back and stops on its event, or waits in the poller when a timer
  or a poll descriptor is pending. A green thread readied while a processor is idle and no thread spins starts one.
- **Stacks.** A green thread takes a stack from a pool when it first runs and gives it back when it finishes: a reserve of
  256 MB that the system commits a page at a time, with a guard page, never moved. A thread runs one by `rt.stackSwitch`
  from its own stack (its scheduler's, Go's g0), and the green thread comes back by the same switch with what it asks: to
  park, to be requeued (a yield or a preemption, onto the global queue as Go's are), to wait for a processor it lost in a
  blocking call, or to finish, which its thread's scheduler then does on its own stack.
- **The monitor** (Go's sysmon) is a thread of its own that holds no processor and allocates nothing. It wakes every 20 µs
  to 10 ms; asks a green thread that has run 10 ms to stop and stops its thread from outside, on Windows by
  `SuspendThread`, `GetThreadContext` and `SetThreadContext`, on Linux by SIGURG, when the thread is in the program's code,
  on the green thread's stack and not in a blocking call; takes a processor from a thread in a bracketed blocking call
  (`enterBlocking`, `exitBlocking`, Go's entersyscall and exitsyscall, around file I/O, `write` and the wait for a child)
  and hands it to another thread; and polls a poller no thread has polled for 10 ms. On Windows it holds the exit claim
  while a thread it stopped is suspended, which a thread that ends the process takes for good first (the runtime's
  `exitProcess`, `schedDrain` and the stub's fault handler), so `ExitProcess` never meets a suspended thread (Go's
  suspendLock).
- **Timers and the poller.** A sleeping green thread (`greenSleep`) is on one heap under the scheduler's lock; the thread
  that waits in the poller waits until the earliest timer, and a new earlier one breaks its wait. The poller is a
  completion port on Windows (`GetQueuedCompletionStatusEx`, broken by `PostQueuedCompletionStatus`) and epoll with an
  eventfd on Linux; a completion whose key is a poll descriptor readies the green thread that waits on it.

**Main with or without the scheduler** (`self/backend/start.maxon`). The link decides by reachability, with no flag,
whether a program runs the scheduler (`SchedulerChoice`): it marks the live code from the entry as for a program of one
thread, with each of the scheduler's routines that code outside it names linked to its single-thread form, and the
program uses the scheduler when that code reaches `rt.stackSwitch`. Running a green thread and leaving one are the only
switches of a stack, so a `when`, an `async` call, an `await`, `sleep`, `Scheduler.yield()` and whatever comes to start
or park a green thread reach it, in whichever function or module they are, and nothing else does: printing, files,
processes, the environment, allocation, `Process.setExitCode`, `Scheduler.processorCount()`, panics and the leak count
do not. The single-thread forms (the substitutes) are the blocking call's bracket (`enterBlocking`, `exitBlocking`), the
exit claim (`takeExitClaim`), each a routine that does nothing (`rt.unbracketed`) since no other thread can take a
processor or stop one, and `Process.setExitCode`, which keeps its code in the stub's `g.exitCode` (`rt.setExitCodeAlone`)
rather than in the scheduler's record; `exitBlocking` may switch to the scheduler, so without its substitute every program
that prints would reach the witness. The link chooses the routine rather than the runtime testing a mode on each call: the
bracket costs a call to a `ret` around a write, and the scheduler's code is not in the executable at all. The entry calls
`rt.runMain` either way:

- **Without the scheduler** it is `rt.mainAlone`, which calls `main` (or each test of an executable of tests) by
  `rt.mainGreen` on the main thread's own stack, as the entry did before green threads, and gives its code, or the code
  `Process.setExitCode` set last. The process has one thread; no scheduler, processor, monitor, poller or green stack is
  made, the main thread's context never names a scheduler, and a panic, a fault, the leak count (`liveBoxes`) and the exit
  read nothing of one. An empty program is about 5 KB, its code about 1.3 KB.
- **With it**, `rt.runMain` calls the runtime's `mainRun` with the address of `rt.mainGreen`, and `main` runs as a green
  thread, as Go runs it as a goroutine, so that it can wait: `mainRun` makes the green thread, has the main thread run the
  scheduler until no behaviour or green thread is left (the last to finish wakes it), and gives back `main`'s code, or the
  code that `Process.setExitCode` set last; then `ExitProcess` ends the other threads wherever they are. Its `main` may be
  preempted.

`main` has the same stack in both: the main thread's is the 256 MB reserve of the executable's header on Windows and the
stub's mapping of that size on Linux (see "Faults and the stack"), and a green thread's is a reserve of the same size. A
test of the corpus says which it expects with `// expect-scheduler: used` or `none`, which the runner holds to the link's
answer, and runs the programs the link gave the scheduler several times, and again under one and two workers. A panic or a
trap on any thread writes its line and exits with 1 at once.

- **Strands.** The coroutines that `async` starts run in strands, as maxon's do (`runtime/strand.maxon`): a strand is a
  green thread that started a coroutine, its root, and the coroutines it and they start, and only the member that holds the
  strand runs program code. A member gives the strand to the member that waits to run first where it parks (an await, a
  sleep, the poller) or yields, and when it finishes; a preemption and a blocking call keep it. A member readied by a timer,
  a finish or the poller takes the strand again before it goes on, on whatever thread readied it, so a strand moves between
  threads between its waits and its coroutines never run at once. The strand is the root's record (its holder, the queue of
  members that wait to run and their lock), which outlives every member, since each coroutine is awaited in a frame of the
  strand. An async call makes the coroutine's green thread with the address of the thunk of its function
  (`rt.async.new`), writes its argument words, and queues it last in the strand (`rt.async.start`), so it runs once the
  caller waits; the thunk calls the function with the words, keeps what it failed with (`greenErrorOffset`) and gives back
  what it returned. The tests of `tests/runtime/` that start
with `green-` hold the scheduler to many green threads ping-ponging, a busy loop stopped for another, the float state
across a preemption, a blocking call handed off, timers and a completion of the poller.

**Faults and the stack.** The executable's main thread has a 256 MB stack reserve (`peStackReserve` in
`self/backend/pe.maxon`): the compiler's passes recurse once per level of nesting of an expression, except down a chain of
operators, which each pass walks with a loop (`ast.leftChain`), so a sum of 200,000 terms needs no deep stack. A reserve is address space, committed a page at a time as the stack grows:
the header has the system commit one page when a thread starts (`peStackCommit`, maxon's 4 KB), and a green thread's
stack starts with one page and its guard. Before `main` the stub keeps 32 KB
past the stack's guard page (`SetThreadStackGuarantee`) and registers a vectored exception handler (`rt.fault`), as
maxon's runtime does: a stack overflow, an access violation or an integer divide fault writes `panic: stack overflow at
rip=0x<address>` (or `nil pointer or invalid memory access`, `integer divide by zero`, `integer overflow`) to standard
error and exits with 1, the panic code, so a program, the compiler among them, never dies without a word. On Linux the entry
makes the handler (`rt.fault` of `entry_linux.maxon`) the action of `SIGSEGV`, `SIGBUS` and `SIGFPE` (`rt_sigaction`, with`SA_ONSTACK` and the
restorer `rt.sigreturn` that x86-64 requires), and each thread maps a signal stack of the same 32 KB (`sigaltstack`); a
`SIGSEGV` or `SIGBUS` within 64 KB of the faulting `rsp` is a stack overflow (maxon's `StackFaultReachBytes`), any other an
access violation, and a `SIGFPE` an integer divide fault, an integer overflow when its `si_code` says so. The handler
calls only kernel32 and allocates nothing; it prints no backtrace, which maxon's does. Every other exception goes on to
the system. A `panic(message)` of the program, or of the runtime's source, writes maxon's line, `panic at <file>:<line>:
<message>`, with the last component of the file's path (`runtime::panicAt`, the location a constant of the call), and
no backtrace either; a panic of an operation the runtime refuses (an index past the end) writes `panic: <message>`.

**What runtime source may do.** It is ordinary Maxon apart from the intrinsics: integers, `bool`, loops, `panic` with a
literal (which calls the runtime's own `panicAt`, which allocates nothing), and `String` values, borrowed as a
parameter or owned once `stringAt` makes one. A dropped `String` goes through the compiler-emitted drop routine, which
calls `free`, so no free comes back into the code that started it. It uses none of the primitives (`Array`, `Map`, ...),
which the runtime implements, and throws nothing. Runtime collections are not generic: an array entry takes a
descriptor of its element's size and layout, and only a thin typed wrapper is per type.

**Layouts.** `runtime/layout_gen.maxon` holds, as named constants, the layouts that the compiler's code and the runtime
share: the word, the string box (its length at 0, its bytes at 8), the array and byte array box (its length, its
capacity and the address of its buffer, with a buffer of 8-byte words or of bytes), the first capacities and the empty
box. A generator (`internal/abigen`, plain Go, see "Generated code") writes it, and `self/backend`'s and `self/ir`'s layout
constants, from one table, so the runtime and the backend cannot disagree. The array entries
(`runtime/arrays.maxon`, `bytes.maxon`) are thin functions over one set of routines that take the size of an element
as a word (`collectionReserve`, `collectionExtend`, `collectionOpenGap`, `collectionCloseGap`): collections take a layout
descriptor, and only a thin typed wrapper is per type.

**Compiled once.** The runtime reads no build option, so one compiled runtime serves every program a compiler builds in a
run: the test runner does it once (`SharedRuntime`). A `build` keeps its unit in the build cache (see "Build cache") under a key
of the compiler's digest and the runtime's source, and a program's modules hold the runtime's source in their keys too, so an
edit to `runtime/` rebuilds everything.

## The subset

- `function` (a body may have no statement; every other block with none is `E3082`), `type` (fields, defaults, methods, `static function`, `static let`; a function parameter may have a default, see "Default parameter values, static fields, default throws and fallthrough"), `enum` (integer, float, string, record and function raw values, see
  "Raw values"), `union` (payloads, methods, explicit tags), `interface`, `typealias X = int(a to|upto b)`,
  `typealias X = bits(n)` (see "Bit patterns"), `typealias X = float(a to b)` (see "Floats"), either range with `implements Parent` (see "Integers"), `typealias X = Box with Count` (also in the body of a type, an interface or an extension), a top-level
  `let` constant. A function, a type and a union can be generic (see "Generics"). A `type` or a `union` can say
  `is noncopyable` right after its name (`type Socket is noncopyable`; never an `enum`), which takes `Copyable` from it,
  and a method of either can be a destructor (`function commit(destructs self)`), which takes both (see "Capabilities and
  destructors").
- Function values (see "Function values"): a function type, named by a typealias
  (`typealias Step = function(Count, mutates Box) returns Count`, also in the body of a type, an interface or an extension, and `... mutates captures` for
  one whose closures change what they capture), a function named without a call, a closure `function(n Count) gives n +
  offset` (its parameters' types and its result may be left to the function type it is written for), a call through a
  value (`f(x)`, `self.op(x)`, `ops[i](x)`, a module-level `let`), and a cast to another function type (`h as Callback`).
- A cast to a type that is not a number is the value itself, moved or borrowed as the operand is: to the type the value
  has (a literal is written as the target, `[1, 2] as Ages`), or between two instances of one generic whose type
  arguments are one type or integer aliases of one name, range and domain (the library's `Byte` and the author's), as
  maxon retags a container. A cast to another instance is `E3131`, and to any other type that is not the value's `E3009`.
- `extension` blocks (see "Extensions"): methods, static functions of a type and conformances added to a type, an enum, a
  union, `Array`, `Map`, `List`, `String`, `bool` or `int` from outside its body.
- `let`, `var` (with a value, or `var x as T` without one), assignment to a variable, a field or an element,
  `_ = call()`, `if`/`else if`/`else`, `if let x = try call()` and `if try call()` with an `else`, `while`, `for i in a to|upto b`, `for x in xs` over an array or a type with
  maxon's iteration protocol, `for (i, x) in xs.enumerated()` over an array with its indexes, `for (k, v) in m` over a map and `for c in E.allCases` (see "Primitives"), `match` as a statement or expression
  (case, literal, range and string patterns, `or` alternatives, `default`, `panic` arms, `default throws E`, and `stmt and fallthrough`), `break`/`continue`
  with labels, `return`, `when` (a behaviour, see "Immutable values, cowns and behaviours"), `await p` of a promise of a
  function that returns nothing, and a `try` block with or without a handler (see "Checked arithmetic").
- `async f(args)` and `await p`, `Promise with T` and `Promise with (T, E)`, `sleep(ms)` and `Scheduler.yield()` (see
  "Coroutines: async and await").
- A statement after a `return`, a `throw`, a `break`, a `continue` or a `panic(...)` in its block is `E3071`, once for each
  such statement, at the statement that follows it; a call of a function that never returns does not count, as in maxon.
  Code after an `if`/`else` or `match` whose every branch returns or panics is allowed and type-checked, but never compiled
  (`ast.Live`): its calls make no instance of a generic. A `break` or `continue` inside a branch does not end the code after
  the `if` or `match`, since a `break` can leave a `match`.
- A block's label may not be the label of a block that encloses it, or the name of the function, method or type that holds
  it (`E2081`, at the inner label): an `end` names the innermost one, so a repeat leaves it ambiguous. Blocks that do not
  enclose one another share a label.
- A `default` arm is the last arm of its match (`E2029`, at the first arm after it), the action of an arm is one statement, so
  a block-form `try` or a `try` with a block handler there is `E2049` as an `if` is, and the operand of a cast is no cast, with
  or without parentheses (`E2079`, at the outer `as`).
- Tuples (see "Tuples"): the type `(A, B)`, the value `(a, b)`, `t.0`, `typealias Span = (A, B)`, a tuple as a parameter, a result or a
  field, `let (a, b) = t`, `(a, let b) = t` and `for (a, b) in xs`.
- Integer, float, bool, string and byte string literals (`b"..."`, a new `ByteArray` of the characters up to U+00FF, one byte each, with `\xNN` for a raw byte; a character above U+00FF is `E1004`), array literals `[a, b]`, map literals `[k: v, k: w]` and `Type from value` over a literal (see "Primitives"), interpolation, arithmetic and bitwise operators, `shl`/`shr`,
  comparisons, `a is b` and `a is not b` (see "Identity"), `and`/`or`/`xor`/`not`, `x if c else y`, `as` casts, error enums, `throw`, `throws E`, `try e`,
  `try e otherwise ...` and `try` blocks (see "Errors of the program"), calls with Maxon's
  positional-then-labelled arguments (a label, and a parameter name, may be a keyword: `File.rename(from, to: dest)`), parameter conventions (`mutates`, `consumes`, `sets`), `x.clone()`,
  `.name`/`.ordinal`/`.rawValue`, subscripts `a[i]`, `match consumes x`.
- Built in: `print`, `printError`, `panic` (which takes a string literal or an interpolated string), `freeze`, `share`, `thaw`, `cown` (also `cown(x, dropWith: d)`), `exchange`, the OS functions of maxon's stdlib (see "The OS"), the float functions (see "Floats"), the aliases `ExitCode`, `Count`,
  `ElementIndex` (an index that a `Count` goes where one is taken, and the other way only by `as`), `FullRangeInt`, `FullRangeFloat`, `Byte`, `ByteArray` (`Array with Byte`), `StringArray` (`Array with String`), the limits of the sized integers as constants (`u64.max`, `i32.min`, ...), the `String` methods `byteLength()`, `byteAt(index)`, `sliceBytes(start, endPos: end)` and `toByteArray()` and the static `String.from(bytes)`, and maxon's `isEmpty`, `equals`, `append`, `count`, `contains`, `startsWith`, `endsWith`, `findFirst`, `findLast`, `startIndex`, `endIndex`, `slice`, `sliceLength`, `replace`, `replaceFirst`, `trim`, `trimStart`, `trimEnd`, `toLower` and `toUpper` with the type `StringIndex` (`charIndex()`, `bytePos()`) and the error `StringError`, the enums `ArithmeticError`, `ArrayError`, `MapError` and `IterationError`, the interface `Error`, and the generic types `Array`, `FixedArray`, `Map`, `List`, `Pool`, `Handle` (see "Primitives") with `sizeof(T)` and `countof(T)`,
  `frozen` and `cown` (see "Immutable values, cowns and behaviours"). Every built-in is public, so a public signature names
  one. The rest of the stdlib is the library source of `stdlib/` (see "Modules"), and there is no `Option`: a
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
  operand. `not` keeps its operand's alias where the alias is closed under it (a pattern, or a range that flipping maps
  onto itself, as `int(i64.min to i64.max)` and `Count`), and is otherwise typed as `-` is: by its context, or else the
  exact range of the flipped bits, so `not b` of an `int(0 to 100)` is `int(-101 to -1)`.

An anonymous range goes into an alias only if the alias holds all of it, else it is `E3005` and asks for a cast
(`try t as Score`). A `return` of an integer of another type, an anonymous range included, converts it to the declared
type with no cast, as `value as T` does, where the range analysis proves that it fits, as after
`if count >= 0 and count <= 255` for `return count` in a function that returns an alias of 0 to 255; where it does
not, it is `E3057` ("the value may not fit"), and the fix is `try <value> as T otherwise panic("out of range")`. A
literal that does not fit is `E3005`, and an assignment, an argument and an initialiser still need the cast. Comparisons take any two integers one word holds, an alias against an anonymous range or a constant
included; only two different aliases are `E3005`. The branches of a conditional or a `match` with no type to
take give their alias when both are of it, or one is and holds the other, and otherwise a range that holds both. A
`try` is that of the call when its fallback is a literal, as in maxon, where a literal outside the alias of the call's
result is `E3005` (`try score() otherwise 101` of an `int(0 to 100)`); any other fallback of an integer joins with the
result as a conditional's branch does.

**Subtypes.** A ranged typealias may name a parent after its range, `typealias BlockId = int(0 to u64.max) implements
ElementIndex`, as maxon's does: it is then a subtype of the parent and of every alias the parent implements, and goes
wherever one of them is declared with no cast; `as` converts the other way, with its check. Two subtypes of one parent
are two types, but they meet in an operator and in the branches of a conditional at their nearest common ancestor
rather than being `E3005`, and an operation whose context expects one of them is that one, as any operation is. A
`mutates` or `sets` parameter takes exactly its type, so a subtype's variable is refused there: the callee could write
any value of the parent. The parent is a ranged typealias, `int` or `float` (`E2003`), possibly of another module
(`implements shapes.Index`); the subtype's range lies inside the parent's and is over the same primitive, a signed range
(one that holds a negative value) only under a signed one (`E3178`); a chain that comes back to the alias is `E3091`. The
built-in `Count` is a subtype of `ElementIndex`.

### Bit patterns

`typealias Word = bits(64)` declares a raw bit pattern: a hash, a mask, an address. It is an unsigned integer alias of the
width's range (`int(0 to 2^n - 1)`) with maxon's rules for a pattern, which differ from a quantity's where a quantity has a
door that refuses what a pattern admits. The widths are 1, 2, 4, 8, 16, 32 and 64, and any other is `E3148`; a `bits` with
no `(` is the unknown type it names, and a literal below 0 is `E3005` (`Value -1 is outside the range of 'Word'
(bits(64))`).

- `+`, `-`, `*`, `shl` and unary `-` over a `bits(64)` wrap modulo 2^64 and never fail, so they need no `try` (the
  operands are one alias or a literal, as always); `/` and `mod` have their divisor's failure; `not`, `and`, `or`, `xor`
  and `shr` keep the alias, and `shr` fills with zeros. An operation that is given another integer type to produce is
  checked against it as for any alias.
- A cast to a `bits(64)` from any integer never fails and keeps the bits (`-1 as Word` of a variable is `u64.max`), and a
  cast from a `bits(64)` to an integer type that holds every signed word reinterprets them (`u64.max as Word as Integer` is
  `-1`); a cast to a narrower pattern or to a smaller range is checked like any other, so `bits(64)` into
  `int(0 to i64.max)` fails for a value with bit 63 set.
- A pattern narrower than a word has the checked arithmetic of the unsigned range of its width.

### Floats

`typealias Ratio = float(0.0 to 1.0)` declares a float alias: its bounds are float or integer literals or `f64.min`,
`f64.max`, `f32.min` and `f32.max`, and it includes both. `FullRangeFloat` (`float(f64.min to f64.max)`) is built in and
holds every double, the infinities and NaN among them. A float is an IEEE-754 double, maxon's: a literal has a point
(`1.5`, `2.0`, `1.5e3`; `1e3` is an integer), and one past `f64.max` is `E2011`.

- A float literal is a constant, and constant arithmetic folds as the hardware would compute it; a constant fits any float
  alias whose range holds it (`E3005` names the value otherwise). An integer constant where a float is expected is its
  double, and an integer beside a float in `+ - * /` or a comparison converts; two different float aliases are `E3005`.
- Any other float operation gives `FullRangeFloat`, which fits any alias of the whole range; a narrower alias takes it
  through `as`, which fails with `overflow` where the value may lie outside (`try x as Ratio otherwise 0.0`), and so does a
  `return` that converts. A float never becomes an integer implicitly (`E3009`) or by `as` (`E3009`): `trunc` does.
- `/` is IEEE-754's and never fails, so it needs no `try`: `x / 0.0` is an infinity of the sign of x and of the zero, and
  `0.0 / 0.0` is NaN, as constants fold and as the hardware computes. `inf` and NaN are ordinary values, and a comparison
  with a NaN holds only for `!=`. `-x` flips the sign bit. There is no float `mod`.
- The built-in functions `trunc(x)` (a `FullRangeInt`, toward zero), `round` (ties to even), `floor`, `ceil`, `sqrt`,
  `abs`, `min(a, b)` and `max(a, b)` take their arguments by place, as maxon's do (`E2067` for a label).
- A float prints as maxon prints one: the shortest decimal that reads back as the same double, always with a fraction and
  never with an exponent (`100.0`, `0.1`, `-0.0`, `inf`, `nan`); a diagnostic writes maxon's notation (`2.5`, `1E+20`).

The compiler has no floats of its own, so `self/ast/floats.maxon` computes on the 64 bits of a double with exact integer
arithmetic: a value is a ratio of big integers times a power of two, rounded once, ties to even. It reads literals, folds
constants, runs the interpreter's float operations and writes the text; `runtime/floats.maxon` writes the text natively by
the same algorithm, maxon's: the exact decimal of the double rounded to 1, 2, ... significant digits, the first that lies
between the midpoints to its neighbours. Natively a float is in an xmm register, computed with SSE2 (and SSE4.1's
`roundsd`) on any of `xmm0` to `xmm15`, and passed to and returned from a call in one (see "Floats across calls"); it is
a word like any other in memory, and to the integer operations that read its bits (negation, `abs`), which `movq` moves
it to (see "The machine IR").

### Errors of the program

A failure that a caller should handle is an error: a case of an enum that implements the built-in interface `Error`, or a
value of a union that does, which carries the data of the failure. A function or an interface's method declares the one error
it throws after its `returns`, and fails with `throw`:

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
- `throws` names an enum or a union, and not an interface or anything else (`E3113`); a value that is neither cannot be
  thrown (`E3005`). `otherwise ignore` gives no
  value, so its `try` is a statement of its own (`E3059`).
- An error comes back from the call as a value: the function returns it beside its result, its case plus 1 in a
  register and 0 for none (`rdx` in the executable), and the call is followed by one branch on it. There are no unwind
  tables and nothing is allocated for an error of an enum; a failure drops what it leaves as any other exit does.
- A union error is a heap box like any union value, and the register holds the box, which 0 means none: a `throw` of a
  value moves it out of the function, and the caller owns it from the call. A handler that names it (`(e)`) owns the
  box as a local of its block, which it may `match` on (the payloads are borrowed there), pass on, wrap
  (`throw Outer.wrapped(e)`) or `throw e` again; every other handler (`otherwise 0`, `ignore`, a `try` block with no name,
  an `if try` with no binding) drops it where the failure leaves. `throw` moves its value, as `return` does, so throwing a
  field that stays in use needs `.clone()`.
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
  local projection bindings (`mutates r = a.f`): borrows exist only as parameters, match bindings, call arguments and
  the results of functions that return a borrowed place.
- **A function can return a borrowed place.** `typeEntry(tt TypeSlot, reads TypeTable, t TypeID) returns borrowed
  TypeEntry` gives a place of type `TypeEntry` that it borrows from its arguments, where `returns TypeEntry` gives a
  value the caller owns. `borrowed` is a word only between `returns` and a type, so it stays a name everywhere else.
  Every `return` of such a function names a place in what the caller lent it: a `let` or `mutates` parameter or a `self`
  that is not `consumes`, a field or a subscript of one at any depth, or a call of another such function whose lenders
  are such places (a scalar argument lends nothing to a place that is not a scalar). The place may be a frozen value
  that holds a `T` (`frozen with T`, at any depth): the function returns its contents, a read-only place rooted where
  the frozen value lies, as a `let` parameter and a `let` receiver read the contents of a frozen value they are given
  (one rule: `check.lendsContents`, and `ir.contentsLent` in lowering, the word `rt.frozen.get` gives). A local, a match
  binding, the variable of a for, a value made in the function (a call, a literal, a string with parts, a conditional),
  a `consumes` or `sets` parameter, and a place that would be boxed to be an interface it returns are `E3926`. A miss
  inside, such as an index out of range, traps as it does anywhere. It may be a method, generic, exported and called
  from another module, and added by an extension. It may be the `current()` of a for, which lends each element to the
  variable (see "Primitives"). For now it may not throw, be a function value, be the `createIterator()` of a for, or be
  the `init` of `Type from "text"` (each `E9000`); a function type and a closure have no syntax for it.
- **A requirement can lend a place.** `function label() returns borrowed String` in an interface is met by a method that
  lends one, and by one that gives a value only where the value is a scalar that is not a float, the one kind whose
  borrow and value are the same word (`E3016`, naming `returns borrowed`, for any other: a call through the requirement
  would own nothing it could drop). A call through it, on a value of a type parameter or of the interface, is a
  projection as a direct call is (`check.requirementCall` gives every such call the requirement's result), so it borrows
  the receiver and the lent arguments for as long as the caller uses the place. A requirement that lends and throws is
  `E9000`, as a function is.
- **Its call is a projection**, under the rules of a subscript (see "Primitives"), through one notion of a place in
  the checker (`projectionOf` and `lentPaths`, `self/check/paths.maxon`) and in lowering (`ast.isStorage`): read, the
  result is a `let` borrow, a base of a field, a subscript, a method with a `let` receiver, and an argument to a `let`
  parameter; binding it, returning it from an ordinary function or passing it to `consumes` is `E3901`, or `E3900` where
  what it borrows from is borrowed itself, and `clone()` is the fix; a scalar result is copied like any scalar.
  Assigning through it, passing it to `mutates`, calling a `mutates self` method on it and moving it on as a for's cursor
  are `E2013` in this form, which lends a place only to be read. Which argument the place lies in is the callee's to
  decide, so, as a `let` projection in Hylo borrows its `let` arguments, the result borrows every argument the call
  passes to a `let` or `mutates` parameter, the receiver among them, for as long as the caller uses it: everywhere a
  subscript holds its collection (an argument while later ones are evaluated, an operand, a for over it, a match binding
  of it) it holds each of those, and an argument passed `mutates`, `consumes` or `sets` beside it that overlaps one is
  `E3186`.
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

### Borrowing values

A cursor of a raw buffer, `__ManagedMemoryCursor with T` (see "Primitives"), points into the buffer it was made from: it
borrows it. So does every value of a type that holds one: a record with a field of such a type, a union with such a payload,
a tuple or a collection of such elements. Holding a borrow is a property of the type, structural as `Copyable` and
`Droppable` are (`check.holdsBorrow`), as Swift's non-escapable types and Hylo's remote parts are, and it is checked in the
ownership pass (`self/check/ownership_borrows.maxon`), so a correct program pays nothing at run time: a cursor is the buffer
and an index, and nothing counts references.

- **What a value borrows.** A call that gives one borrows each managed place it is lent (`let` or `mutates`, the receiver
  among them), as `names.managed.createCursor()` borrows `names`, and carries what each argument that holds a borrow
  borrows; a record literal carries what its fields borrow. `createIterator()` and `cursor()` return one as a function
  that `returns borrowed` returns a place, with no word for it in the signature.
- **Where it may go.** It is bound to a local with `let` or `var`, which holds each place it borrows for the local's scope:
  changing or moving one there is `E3186`. It is passed to any parameter, which lends it or hands it on to what the call
  gives. It is returned only where everything it borrows is lent by the caller: a `let` or `mutates` parameter or `self`,
  or what a parameter of a type that holds a borrow carries (`E3927` otherwise, for a local or a `consumes` parameter it
  borrows). It is assigned to a variable or a field, or passed to a call beside a `mutates` argument of a type that holds a
  borrow, only where that place borrows the same already, or is a parameter and it borrows only what the caller lends
  (`E3927`).
- **What it may not do** (`E3927`): borrow a temporary, which ends with its statement; be taken over by a closure or a
  behaviour, be put in an interface value, be bound by `if try` or `match consumes`; or stand for a type parameter. A type
  parameter stands only for a type that holds no borrow, so that code over it, checked once, may store and return its values,
  except where that code is checked as code over a value that holds one: `Self` in an extension of an interface, and an
  associated type that the interface's `where` bounds (`Iterable`'s `Iter`). The check of a type argument waits for the
  verify stage, when every type's fields are declared.

### Function values

A function type is written with `function` and always named by a typealias, as in Maxon:
`typealias UnaryOp = function(Score) returns Score`, with a convention before a parameter's type where it has one
(`function(mutates Tally)`), no `returns` for one that returns nothing, and `mutates captures` at the end for one whose
values may change what they captured (`function() returns Count mutates captures`). A function value is a value of one: a
function named without a call (`apply(double, x: 4)`), or a closure, an anonymous function whose body is one expression,
`function(n Score) gives n + offset`. A function type has no `throws`, so a function that throws is not a value (`E3101`),
and neither is a generic or built-in function or a method: pass a closure that calls it. A value fits a function type with
the same conventions whose parameters it takes at least as wide (exactly, for one passed by address), whose result
takes what it returns, and that mutates its captures only if the type allows it (`E3005`). A closure written where a
function type is expected (an argument, a `return`, the value of a typed place, a cast) may leave out its parameters'
types and its result, which it takes from that type; anywhere else a parameter without a type is `E2015`.

**Brands.** A typealias of a function type at the top of a module is a type of its own, a brand, as in Maxon: two of one
shape (`Handler` and `Callback`, each `function(Count) returns Count`) do not take each other's values (`E3005`, naming
both), at any depth (a parameter of type `Handler` in a function type is not one of type `Callback`). A closure and a named
function carry no brand and fit every alias of their shape, and so does a function typealias in the body of a generic type, which
spells the signature that its instances share. `h as Callback` re-brands a value of the same shape (another shape is `E3005`), and so does
a `return` without a cast. A conditional of a branded value and an unbranded one of its shape is of the brand; of two
brands it is `E2028`. A message names a function type by its brand where only the brands differ, and by its shape
(`fn(Count) returns Count`) where more does, as Maxon's do.

**What a closure captures.** A closure's body is a function of its own, checked as a behaviour's is (see "Immutable
values, cowns and behaviours"), that sees the variables around it by capturing them when the closure is made. A closure
never copies what it captures on its own:

| Captured | Effect |
|---|---|
| a scalar | copied into the closure, as any scalar is |
| a local that owns its value (or a `consumes` parameter) | **moved** into the closure, which owns it from then on: using the local afterwards is `E3102` (`its ownership moved to the closure that captures it`), and so is capturing it in a loop; a value with destructors cannot be captured (`E3919`) |
| a borrow: a `let`, `mutates` or `sets` parameter, `self` in a method, a match binding, the variable of a loop over a collection, or what an enclosing closure captured | **borrowed** by the closure, which holds the borrowed reference and neither copies nor drops it, so the closure sees every later change to the value's parts |
| in a mutating closure (one written for a type that `mutates captures`), a variable that can be changed: a `var`, a `mutates` parameter, `mutates self` | **borrowed to be changed**: the closure holds the variable's address and its writes are the variable's |

In a mutating closure, what the closure owns (a scalar it copied, a local that moved in) is its own state: the body
changes it in place, as a `mutates` parameter, the value lives in the closure, and each call sees the writes of the one
before, so a function may return a counter (`let count = 0 as Count` and `function() gives increment(count)`). Replacing
it drops the value it held. Such a closure borrows nothing because of that state and may escape; one that also borrows to
change a `var` does not, and `E3925` then says to declare the variable with `let` so that the closure owns it. The body
reads every other capture as a `let` borrow of what the closure holds, since it may be called again: it cannot move it,
and changing it, or passing it to `mutates`, is `E3210`, Maxon's refusal of a function value that writes what it captured
where its type does not declare `mutates captures` (in a mutating closure, of a capture it borrows, `E2013`). A field named
bare in a method (`count` for `self.count`) is not captured yet: write `self.count`.

**A closure that borrows does not escape.** A function value holds a borrow when its closure captured one, when it took
over a value that holds one, and when it is borrowed itself and its origin is unknown: a `let`, `mutates` or `sets`
parameter of a function type, or a function value a closure captured. One that is owned (a `consumes` parameter, a field,
an element, a call's result) holds none. A value that holds a borrow may be called, bound to a local with `let` or `var`
(which ends with its scope, inside the scope of what it borrows), given on by a conditional or a cast, captured by a
closure that does the same, and passed to a parameter that borrows it. Storing it in a field, an element or a payload,
returning it, assigning it to a variable declared before it, passing it to `consumes`, copying it with `clone()` and
capturing it in a behaviour (`when`) are `E3925` at the place it escapes, naming the captured borrow and the fix: clone the
value into a local first (`let copy = value.clone()`) and capture that. For a borrowed parameter of a function type the fix
is to declare it `consumes`, which `minimaxon fix` writes: a `consumes` parameter receives only values that own what they
hold, which may be stored and shared.

While a function value that borrows lives (the scope of the local it is bound to, or the rest of the call it is an
argument of), what it borrows is held as a match subject is (`E3186` otherwise): a variable it reads may not be replaced as
a whole, which only a `mutates` or `sets` parameter could be, though its parts may change, and a variable that a mutating
closure changes may not be read, changed, moved or captured again by anything else. Within one call, a closure argument
that borrows a place may not be passed beside an earlier argument that passes that place `mutates`, `consumes` or `sets`,
and one that changes it beside any earlier argument that names it.

**Ownership of a function value.** A closure of a type without `mutates captures` is immutable, so its value is never
moved: binding it, storing it, passing it to `consumes` and returning it give another value that shares the closure (the
closure's count says how many), and the place keeps its own, as Maxon's copies share (`var g = f`, `f.clone()`). Each
value is dropped where its owner dies, and the last one frees the closure, which drops what it owns and nothing it borrows.

A value of a type that `mutates captures` has one owner, as a record has, since two values sharing its closure would be
two names for one changing state: binding it, storing it, passing it to `consumes` and returning it move it (using it
afterwards is `E3102`), and `clone()` of it, or of a value that holds one, is `E3918`. Calling it changes it, so the
callee must be a place the caller may change: a `var`, a `mutates` parameter, a field or an element reached through one,
or `mutates self`; anywhere else (a `let`, a borrowing or `consumes` parameter) the call is `E2013`, which `fix` resolves
by declaring the `let` with `var` or the parameter `mutates`, as for a change (a `consumes` parameter is moved into a `var`
first), and through a capture of a closure whose type does not mutate its captures it is `E3210`. A new value, a closure or a call's
result, may be called where it is made and passed to a `mutates` parameter, which changes it for that call alone. A value
of a type without the clause fits a mutating type; held there, it follows that type's rules, though the closure it shares
changes nothing.

Not yet: the purity and capability rules of closures (`E3207`, `E3210` beyond writes to captures, `E3064` through a
closure, the cycle check `E3183`), which Phase 3 adds; `returns immutable`; a field named bare in a closure; and the
collection methods that take a closure (`map`, `filter`, `sortBy`).

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
  bounds a type parameter, and is the type of a value as well (see "Interface values"), unless a requirement names `Self`
  or is static (`E3905`); an alias, `String` or `bool` cannot implement one in its declaration. A requirement may be `static function`, which the type provides as a static function (a method in its
  place is `E3016`, as is the other way round); as in maxon it has no receiver to dispatch on, so a call of it through a
  type parameter, `T.make()` or `x.make()`, is `E3116`, and `T.make` as a value is `E2078`: it is called on a concrete
  type. A type parameter's name stands for the parameter in an expression, over a declaration of that name. A method
  uses its type's parameters and takes none of its own (`E3907`). `Self` inside a generic type is the type at its own
  parameters, and a literal of it is `Self{...}`.
- **Inheritance.** `interface Derived extends Base, Other` (before or after its `uses`) has the requirements of the
  interfaces it extends besides its own: a type that implements `Derived` provides them all (a missing one is listed
  `(from Base)` in `E3016`), and meets a bound of `Base`, as does a type parameter bounded by `Derived`, whose calls
  reach `Base`'s methods. An interface that requires again a method of one it extends makes a call of it through a
  bound `E3114`. A chain that comes back to the interface is `E3091`. A `Base` with associated types gives `Derived` each
  one whose name `Derived` does not declare, after its own, so `interface BiCursor extends Cursor` has `Cursor`'s
  `Element` and `implements BiCursor with Pos` binds it; one that `Derived` declares binds `Base`'s of its name, as maxon
  binds them (`ast.inheritedBindings`). The parents are declared before anything in the module names the interface
  (`check.declareParentInterfaces`), so its associated types are known wherever it is instantiated. A clause that names
  `Derived` with no `with` beside one that binds `Base`, `implements Cursor with Pos, BiCursor`, binds `Derived`'s inherited
  types from it, as maxon searches the clause. A bound or a conformance of `Derived with X` meets one of `Base` at the
  types it binds there, and a call infers through it.
- **`clone()` as a requirement.** `function clone() returns Self`, maxon's `Cloneable`, is met by the built-in `clone()`:
  the interface extends `Copyable` instead of requiring a method, so a type that implements it must be Copyable
  (`E3918`), and a parameter bounded by it, or by any interface that extends `Copyable`, may be cloned. Any other `clone`
  requirement is `E3006`, as a declared `clone` is.
- **Associated types.** An interface declares associated types with `uses` after its name (`interface Cursor uses
  Element`), which its signatures name. A type binds them where it implements it, one type bare and several in
  parentheses, as any type arguments: `type IntCur implements Cursor with Integer`, `type Both implements Duo with
  (Score, Weight)`, `type Holder uses E implements Cursor with E`. A conformance that binds none is `E3016` (`Type
  'Broken' does not define required associated type 'Element' ...`), any other count `E2056`, and each method is
  checked against the requirement with the bound types in place of the associated ones. A bound binds them all
  (`where S implements Cursor with E`, `E2056` otherwise), so a call through it is typed in the generic body once:
  `s.current()` is an `E`. A call infers a type parameter that a bound binds from what the argument's declaration binds
  (`first(IntCur.create(5))` with `where S implements Cursor with E` takes `E` as `Integer`), and an argument whose
  declaration binds the interface otherwise than the bound says is `E3133`.
- **An interface's `where`** bounds its associated types: `interface Iterable uses Element, Iter where Iter implements
  Iterator with Element`, after the `uses` and any `extends`, naming only the interface's own associated types (`E3916`
  otherwise). Code over the interface knows the bound without being told it: in an extension of the interface a value of
  `Iter` has `Iterator`'s methods, typed by `Element`, and a generic bound `where S implements Iterable with (E, I)` gives
  `I` the bound `Iterator with E` as well. A conformance that binds an associated type to a type that breaks the bound
  is refused at the conformance (`E3017` where the type does not implement the bound's interface, `E3133` where it binds
  it otherwise), so the bound holds wherever the conformance is used.
- **Instances.** A type names an instance with `with`: `Box with Count`, `Map with (String, Count)`, `lib.Option
  with Count`, and a `FixedArray` its count before the element, `FixedArray with 3 Int`, which only it takes (`E2056`). One argument is written bare, several in parentheses, and an argument that is an instance itself is
  parenthesized by convention: `Option with (Box with T)`. Their number is checked (`E2056`). A `typealias` names an
  instance, `typealias Counts = Array with Count`, or, with several arguments, `typealias Counts = Map with String, Count` (a bare list, which only a typealias takes), and is that instance, not a type of its own: `Counts` and `array
  with Count` are one type. Its right-hand side is an instance (`E3917`), it may name other typealiases in any module,
  and one that names itself is `E2012`.
- **Nested typealiases.** A `type`'s body can declare a typealias, before or among its fields and methods:
  `typealias Items = Array with Element`, or `typealias Name = int(0 to 99)`. It is a declaration of the module like a
  top-level one, named `Pool.Items` in messages, with the type as its scope: the bare name spells it in the type's
  own body (a field, a signature, an expression such as `Items.create(0)`) and shadows a module typealias of the same name
  there, and the type's parameters are in scope in its target, so `Items` of `Pool with Element` is the instance at the
  type's own parameter. Declaring one twice in a body is `E3061`. A field that holds the type again over a type built from its parameters (`var kids as Array with (Nest with (T, Cell with U))`) is `E3184`, since its instances never end. An `interface`'s body declares
  them the same way, over its own parameters, and a requirement spells one by its bare name; the interface's typealias is no member that
  `Holder.Items` names (`E3011`), and a type that implements the interface declares its own. Not supported: a typealias in an `enum`
  or a `union` (the parser's generic `E2010`, as in Maxon), and a ranged one in a generic type (a type for each instance, `E2015`).
- **Typealiases of an extension.** An `extension` body may declare typealiases as a `type`'s does, `typealias Level = int(0 to 64)` or
  `typealias Pairs = Array with T`, over the type parameters of the type it extends, with `Self` the type. The bodies of
  the extensions of the type that its module writes spell one by its bare name, and those of any module's extensions spell the typealiases that
  the type's own body declares as well. It is named from anywhere as `Type.Level` (`shapes.Counter.Level` for another module's type),
  in the places that take a type, and is a declaration of the extension's module, named `Counter.Level` in messages
  (`Array.Level` for an extension of the built-in `Array`): its own visibility, never the extension's. It is scoped as the
  extension's methods are: seen from the module that writes the extension and from the modules that name it, and two
  extensions seen from one module that add one name to a type are ambiguous at the use, `E3942`, whichever modules they are in.
  The same name twice in the extensions of one module, or in an extension and in the body of its type, is `E3061` at
  the extension's declaration. An extension of an `enum` or a `union` declares none (`E2010`), and a ranged typealias of an
  extension of a generic type is `E2015`, as in the body of one. `Type.Level` does not name a typealias of a generic type
  (`E2015`: write it inside the type or an extension of it).
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
  declaration says `where T implements Droppable` (`E3919`). Maxon copies and drops a `T` without a bound; here each of
  these diagnostics names the bound it needs, and `minimaxon fix` writes it in the `where` of the function whose body
  needs it, a method's own for a parameter of its type (`E3900`, `E3918`, `E3919`, `E3017`). So a generic container gives its element out with `.clone()` where `T` is `Copyable`, or takes it out of a primitive with `remove` or `pop`
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
- **Interning.** `Box with Count` is one type wherever it is written: an instance's handle is the hash of its origin and
  arguments (`ast.instantiateType`), so every module that needs an instance gets the same type, each holding its own copy
  of the entry, and the order in which modules interned it is never visible in any output. An instance reads the declaration's fields, cases and
  methods and substitutes its arguments.
- **Limits.** A generic type may not contain itself, even with a larger type argument each time (`E4014`), and a
  generic function may not call itself with ever larger type arguments (`E3906`, past a nesting depth of 16). Each module that
  reaches such a function finds the runaway for itself, and the program gets one error: the one of the first module in module
  order that has it, at the first call in its walk that asks for the too deep instance. That is
  the recursive call inside the generic whenever the growth is in one call, the case that one error covers however many modules use it.

### Interface values

An interface is the type of a value as well as a bound: a value of `Shape` holds a value of any type that implements it,
and a call of a method that `Shape` requires runs the method of the type it holds, as in Go. It has Hylo's ownership: the
interface value owns what it holds and moves like a record.

```
typealias Shapes = Array with Shape

function describe(item Shape) returns Count
	return item.area()
end 'describe'

function main() returns ExitCode
	var shapes = Shapes.create(0)
	shapes.push(Square.create(2))
	let sq = Square.create(3)
	print("{describe(sq)}\n")
	let back = try shapes.pop() otherwise panic("empty")
	let square = try back as Square otherwise panic("not a square")
	return 0
end 'main'
```

- **Putting a value in one.** A value of a type that implements the interface goes where a value of it is expected (a
  parameter, a field, an element, a `return`, an assignment, a branch) and is put in one there, and so does a cast to
  the interface (`sq as Shape`). Whether the type implements the interface is the conformance seen from the module where
  the value is put in it (see "Extensions"), checked in the verify stage (`E3005` with maxon's wording, `E3941` for two),
  and the interface value keeps that conformance wherever it goes. A float cannot be put in one, an interface value is
  not put in a value of another interface, even one it extends (`E3005`; a cast between two is maxon's `E3009`), and a
  constant of an interface type is `E2015`.
- **Ownership.** A value that is consumed there (a binding, a `consumes` parameter, a field, a `return`) moves into the
  interface value under the rules of "Ownership", so a borrowed one is `E3900`; a `let` parameter is lent the caller's
  value for the call, in a box in the caller's frame, and the caller keeps it. A `mutates` or `sets` parameter of an interface type takes only an
  interface value (`E3005`), since the callee could put a value of another type there. Dropping an interface value drops
  what it holds, so the type must be `Droppable` (`E3919`). `clone()` of one is allowed where the interface extends
  `Copyable` (`interface Shape extends Copyable`), which every type that implements it must then be (`E3918`); otherwise
  it is `E3918`, as for any value that cannot be copied. A method that `mutates self` is called through a place the code
  may change, as on a record (`E2013`). Interface values are not compared (`E3005`).
- **Getting the value back.** `x as Square` takes the value out of the interface value `x`, which the cast consumes, when
  it holds a `Square`, and fails with `CastError.otherType` when it holds a value of another type, so a `try` covers it
  (`E3057`), as any operation that can fail. A type that does not implement the interface is `E3009`.
- **Which interfaces.** One whose requirement names `Self`, or is static, is a bound and nothing else (`E3905`, in the
  verify stage, when every interface has its requirements); one with associated types is a value type with them bound
  (`Seq with Integer`).
- **In a generic.** An interface value meets a bound of its own interface (another, even one it extends, is `E3017`), so
  a generic instantiated at one (`twice(a)` with `a` a `Shape` value, `Holder with Shape`) calls through its table where
  the body calls through the bound. A call with a value of the concrete type is an instance of its own, monomorphized as
  any instance is. A parameter written with an interface type is a value of it, whatever the argument: a method and a
  requirement take no type parameters of their own, so one mechanism serves functions, methods and requirements.

### Tuples

A tuple is a record whose fields are named by their places, `0`, `1` and so on: `(Count, String)` is the instance of the record
`Tuple2` at its two elements, found as a generic instance is (`ast.tupleType`, interned by the element types, so it is one type however it
is written), with a type parameter and a field for each element, and every other fact about it follows from that. It has the layout, the
copy and drop routines and the ownership rules of a record with those fields (`ast.TypeTable.tupleOrigin`; a number of elements no
program uses has no record), and an element is a field: `t.0` borrows it, assigns it where `t` is a `var`, and moves it out where it is
consumed, as a field is moved out of an owned local. `Droppable` and `Copyable` hold of a tuple when they hold of every element.

```
typealias Span = (Count, Count)

typealias Spans = Array with Span

function widen(consumes pair (Count, String)) returns (Count, String)
	return (pair.0, pair.1)
end 'widen'

function main() returns ExitCode
	let (low, high) = widen((1 as Count, "one"))
	var spans = Spans.create(0)
	spans.push((low, high.byteLength()))
	for (a, b) in spans 'each'
		print("{a} {b}\n")
	end 'each'
	return 0
end 'main'
```

- **Syntax.** `(a, b)` has at least two elements, and `(a)` is a parenthesis. A tuple type is a parenthesized list of two or more
  types, in a signature, a field, a cast and the right-hand side of a `typealias`. As a type argument it takes its own parentheses,
  `Array with ((Count, Count))`, since `Array with (Count, Count)` is two arguments (`E2056`), as in Maxon. An element is `.0`,
  `.1` and so on, `t.0.1` reaching into a tuple that holds one. The parser tells a tuple from a parenthesis, and a tuple
  assignment from an expression that starts with a parenthesis, by looking for a comma inside the parentheses before it parses them
  (`{atTuple}?` and `{atTupleAssignment}?`, predicates of the grammar that `Parser` decides).
- **Typing.** A tuple literal takes the type of its context when that is a tuple of as many elements, and checks each element against
  its own; elsewhere each element has the type it is written with, and a bare number keeps the range it spans, as an element of an array
  literal does, or takes the whole word inside another literal. A `var` that starts from a tuple of bare numbers is `E2004`. An element
  keeps its own alias, so `(Tally, Tally)` and `(Score, Score)` are two types, and an element is held to its alias's range.
- **Taking a tuple apart.** A destructuring declaration (`let (a, b) = e`, `var (a, b) = e`, `_` for an element it does not name), a tuple
  assignment (`(a, let b) = e`: a name with no keyword is an existing `var`, `let` and `var` declare one, `_` discards) and a `for` over a
  collection whose elements are tuples (`for (a, b) in xs`) are made up by the check of ordinary statements, which it writes into the
  tree (`check/destructure.maxon`, `ast.Typing.expansions`): a `let` that holds the tuple, unless it is a variable, a field or an element at a
  simple index, which is read again for each element; then a `let` or an assignment for each name, whose value is that element read as a
  field. The ranges, the ownership pass and the lowering run those statements as they run any, so a scalar element is copied, a managed
  one is moved out of an owned tuple and a borrowed one is `E3900` (`.clone()` is the fix), and no pass has a rule of its own for
  tuples. The names count against the elements: `E2015` for a declaration and `E3005` for an assignment.

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
  nothing else and returns nothing, as a method name (a method is not a function value), and the
  thread that drops the last reference runs it. `dropWith:` on `Droppable` content has nothing to decide, and naming
  anything but such a destructor is `E3921` too. This is the only place a destructor runs without being called.
- **Behaviours.** The captures move into the body, so a captured value that is not `Droppable` is ended there like any
  owned local.
- **Closures.** A closure drops what it owns with its last copy, so a value it owns must be `Droppable` (`E3919`); what it
  borrows it does not drop.
- A message that would have the reader write `.clone()` suggests it only for a type that has `Copyable`; for another it
  names what removes it and says to move the value instead.

### Extensions

```
extension Meter implements Sizer
	function raise(mutates self, by Count)
		level = try level + by otherwise panic("overflow")
	end 'raise'
end 'Meter'

extension int
	function doubled() returns FullRangeInt
		return try self * 2 otherwise panic("overflow")
	end 'doubled'
end 'int'
```

An `extension` adds the methods it declares, and the interfaces it `implements`, to the type it names: a `type`, an `enum`
or a `union` of any module (`extension shapes.Counter`), the built-in `Array`, `Map` and `List`, `String`, `bool`, `int`,
which stands for every integer type (`self` has the range of `FullRangeInt`), and `float`, which stands for every float type
(`self` is a `FullRangeFloat`), as maxon's `PrimitiveExtensions.maxon` extends them. The end label is the extended type's name.
Inside, `self` and the bare names of the type's fields and type parameters mean what they do in its body, and `Self` is
the type. A static function is allowed only in an extension of a `type` or of a built-in generic type. An extension
declares no field or destructor. Its typealiases are scoped as its methods are (see "Typealiases of an extension" under "Generics"). A method of an extension is called as one of
the type's, and is `E3006` if the type's body or another extension of the same module declares the name.

An extension's declarations belong to the module that writes it, never to the extended type's module, so each module's
stages stay independent. As in Hylo, an extension and the conformances it declares are scoped: they are seen from the
module that writes the extension and from the modules that name it (the ones its resolve recorded as dependencies), and
the standard library sees none. A call `x.m()` finds the type's own method, then the methods that the extensions seen
from the module of the call add. Two of those that add one name are ambiguous, `E3942` at the call, whichever module
they are in: no order of search chooses, and since a call cannot name a method through its module, one of them has to be
removed or renamed. A method has its own visibility (`export`, `public`, or its module's only); a modifier on the block
(`export extension`) is accepted and has no meaning. A method that an extension adds to a type of another module has a
link name of its own module (see "Link names"), so two modules may add one name to a type.

A type implements an interface where a declaration that says so is seen: its own body, seen wherever the type is, or an
extension seen as above. A bound (`where T implements I`) is met by the conformance seen from the module that makes the
call, which instantiates the generic, and that conformance travels with the instance: the methods that meet the bound
are the ones the module of the conformance sees, and a generic that passes its `T` on to another passes the
conformance it was given, not one its own module sees. So a module may give a generic of a module that cannot see its
extension the type that the extension makes conform. Two conformances of one type to one interface seen at one place
are `E3941`: at an extension whose module sees another (the type's body among them), at the second of two extensions of
one module that say it, and at a call that instantiates a generic where the module sees two. As in Hylo, a module gives a
conformance to other modules only if it declares the type or the interface: one of a type of another module to an
interface of another module (an orphan) holds in the module that writes it alone, so it cannot change what a type means
to an interface where neither is its own. A built-in type and a built-in interface are declared by no module of the
program, so a conformance of one to the other never leaves its module. Two bounds of one type parameter that both
extend one interface, met by conformances of two modules, are `E3941` at the call that instantiates the generic, since
each module sees its own methods for the interface they share. An instance is its generic, its type arguments and the module of each conformance
it was given: two modules that instantiate one generic at one type with conformances of their own get an instance
each, with a link name each (see "Link names"), and with one conformance they share it. A conformance that a method of
a generic type needs for a bound of the type is the one seen from the module of the call, and a call through a bound
that reaches such a method where that module sees none is `E3017` when the module's instances are found.

Inside a type's methods, and in an extension's, a bare call `helper(...)` names the method `helper` of the enclosing type, as
a bare field name names a field: an instance method is called on `self`, a static one on the type, and either outranks a
function of the same name. A static method has no `self`, so a bare call of an instance method in it, and a call of one
by its type's name (`Counter.bump()`), is `E3193`. A local variable of that name comes first, and a closure or a behaviour inside the
method does not see the method.

An extension of a built-in generic type names its parameters as the built-in table does (`T` for `Array`, not maxon's
`Element`). An extension of a typealias is `E2015`, one of an unknown name is `E3011`.

```
extension Valued
	function doubled() returns Score
		return try self.value() * 2 otherwise 0
	end 'doubled'
end 'Valued'

extension Box where Item implements Sizer
	function total() returns ExitCode
		return item.size()
	end 'total'
end 'Box'
```

An extension of an interface (`extension Valued`) adds its methods to every type that implements the interface where the
extension is seen, to a type parameter bounded by it and to a value of it. As in Hylo and Swift, each method is generic over
the type that implements the interface, which is `Self` inside, and over the interface's associated types, which are the
conformer's bindings (`Element` in `extension Holder` of `interface Holder uses Element`). Its body is checked once,
against the interface: it calls what the interface requires and what extensions of it add, on `self` or bare, and names
no field of a conformer (`E2004`). It is no requirement, and a call of it is resolved where it is written: on a value whose
type is known the type's own method comes first, then those its extensions add, then those that extensions of the
interfaces it implements add, so a conformer's own method of the name wins there, while a call through a bound, on an
interface value or inside the extension runs the extension's. A method of an extension of an interface outranks one of an
interface it extends; two that neither outranks are `E3942`, as two extensions of a type are. On an interface value the
method is lent the box: its instance at the interface's type reaches the requirements through the table. A type that has
no method of a requirement's name meets the requirement with a method that an extension of an interface it implements
adds (`extension Forward` adds `back`, which `interface Both extends Forward` requires), in a table too. Its instances are
named by the extension's module and the interface (see "Link names"). An extension of an interface declares no
conformance and no static function (`E2015`). `for item in self` in it walks the conformer through the iteration protocol
of the interface's bounds (see "Iteration" under "Primitives"): `createIterator()` of `Iterable`, and `current()` and
`advance()` of the bound that the interface's `where` gives its iterator, checked once and lowered for each instance.

A `where` on an extension is the `where` of each method it declares, before the method's own: the methods are those of the
instances, or of the conformers, whose type arguments meet it, and the bound holds in their bodies. A call on a value
that does not meet it is maxon's `E4006`, in the verify stage: `Type 'Box' has no field named 'total' ('total' is
available as a conditional extension where Item implements Sizer, but 'Plain' does not implement 'Sizer')`. A `where` that
names no type parameter of the extended declaration is `E2015`. A method with such a bound does not meet a requirement
(`E3016`), so an extension that says `implements` and has a `where` gives no conditional conformance.

### Primitives

Layer 2 of [docs/regions.md](docs/regions.md), as far as step 3 goes: their signatures and ownership rules. An
`Array with T`, a `Map with (K, V)`, a `List with T` (doubly linked, one node per element) and a `Pool with T` own their elements.
Their operations are generic methods that no source declares (`internal/builtingen/table.go`), typed, borrowed and moved
like any other method from the signature below, so none of the ownership rules has a special case for them. They are
declared in the stdlib module, so a name resolves to them last, after the module's own, the root's and an exported
one anywhere.

| Type | Operations |
|---|---|
| `FixedArray with N T`, `FixedArray with T` | maxon's fixed-size array (`stdlib/FixedArray.maxon`): an array whose count never changes, which its type states (`with 3 Int`, a literal from 1 to 4294967295, `E2069`) or, runtime-sized, its value. `create()` and `create(value: x)` where the type states the count, `create(n)` and `create(n, value: x)` where it does not (`E3194` for the other form), each zeroed where no `value:` is given and `T` has a zero value (`E3106`), and a constant `n` gives the type that states it; `Alias from [a, b]`, of exactly the count a counted alias states, or stating the literal's; `count()`; `get(i)` where `T` is `Copyable` and `set(i, value: v)` where `T` is `Droppable`; `refill(x)`, copies of `x` over every element. A get or a set of a type that states its count at an index the range analysis proves inside it cannot fail and compiles to the access with no check; a constant index outside it is `E3005`, and any other index throws `ArrayError.indexOutOfBounds` as an array's does, so it needs a `try` (`E3057`, naming the ranged type that would prove it). A `FixedArray with N T` is a subtype of `FixedArray with T`, so it goes where one is taken and a join of the two is the runtime-sized type. `for x in xs` walks it as an array; inside an `extension FixedArray` its `Self` states no count, and `countof(Self)` in a method is the receiver's |
| `Array with T` | `create(n)`, `n` zero elements, where `T` has a zero value or `n` is the literal `0` (`E3106`); `filled(n, value: x)` where `T` is `Copyable`; `count()`, `isEmpty()`; `get(i)`, a copy of the element, where `T` is `Copyable`, and `set(i, value: v)`, where `T` is `Droppable`, both throwing `ArrayError.indexOutOfBounds` past the end; `push(value)`; `append(other)`, a copy of each element of `other`, where `T` is `Copyable`; `insert(at, value: v)`, which clamps `at` to the count; `remove(at)` and `pop()` return `T` and throw `ArrayError`; `dropEmpty()` |
| `Map with (K, V)` | `create()`; `count()`, `isEmpty()`, `contains(k)`; `insert(k, value: v)`, which throws `MapError.keyAlreadyExists` for a key that is there, and `upsert(k, value: v)`, which replaces its value, both where `V` is `Droppable`; `replace(k, value: v)` returns the old value and throws `MapError.keyNotFound` for a key that is not there; `remove(k)` drops the value and returns whether the key was there, where `V` is `Droppable`; `dropEmpty()` |
| `List with T` | `create()`; `count()`, `isEmpty()`, `contains(h)`; `first()`, `last()`, `next(h)` and `prev(h)` return `Handle with T` and throw `ArrayError.indexOutOfBounds` where there is no such node; `prepend(value)` and `append(value)` return `Handle with T`; `removeFirst()` and `removeLast()` return `T` and throw `ArrayError`; `remove(h)` returns `T` and throws `ArrayError.emptySlot` for a handle that names no element; `moveToFront(h)`; `dropEmpty()` |
| `Pool with T` | `create()`; `count()`, `isEmpty()`, `contains(h)`; `insert(value)` returns `Handle with T`; `remove(h)` returns `T` and throws `ArrayError.emptySlot` for a handle that names no element; `dropEmpty()` |
| `Handle with T` | no operations; `==` and `!=` |
| `__ManagedMemory with T` | maxon's raw buffer, which only the library tiers name (`E3185`): the box of an array, seen through maxon's buffer operations, each the array operation that does the same, so there is one implementation of each on either target. `length()`; `get(i)`, a copy of the element, where `T` is `Copyable`, throwing `__ManagedMemoryError.indexOutOfBounds` past the end and `emptySlot` at an empty slot (see `Array.clearSlot`). `xs.managed` is the buffer of an array: the array itself seen as one, so it is borrowed, moved and dropped as `xs` is (`let m = xs.managed` moves `xs`) |
| `__Builtins` | maxon's raw intrinsics, which only the library tiers call (`E3185`), each the built-in that does the same: `currentProcessId()` is `Process.id()`, `currentTimeNanos()` `Clock.nowNanos()`; `floatToBits(x)` is the 64 bits of a double as a `FullRangeInt`, a reinterpretation that the library's float hash folds; `hashWord(word, seed)` and `hashBytes(bytes, at, count, seed)` are maxon's seeded hash, its software fold of a word or of a span of a `ByteArray` under a `HashDigest` seed (`runtime/hashfold.maxon`, which the interpreter runs too, as `hashfold_gen.maxon`), a span past the end a panic; `readBigEndian(bytes, at, width)` and `readLittleEndian` the `OrderedWord` of the 2, 4 or 8 bytes at an index, the first the most or the least significant, throwing `ArrayError.indexOutOfBounds` when they are not all inside, and `appendBigEndian(bytes, word, width)` and `appendLittleEndian` the append of a word's low bytes in that order; the width is the literal 2, 4 or 8 (`E3005` otherwise, in maxon's words). `OrderedWord` and `HashDigest` are built-in `bits(64)` aliases, as maxon's library declares them (`stdlib/Builtins.maxon`, `Hasher.maxon`). `hashSeed()` is the fold of no bytes under the process's secret, which a `Hasher` starts from, and `drawHashSeed()` a fresh secret (`HashSeed.random()`): the first call makes the process's hash state, as maxon's runtime does, from the 16 hexadecimal digits that `MAXON_HASH_SEED` pins (a pin that is not exits 1 with maxon's message, an empty one is none) or from 8 bytes of the system's entropy (Windows' `ProcessPrng`, loaded by name only then, and Linux's `getrandom`), and a pinned process draws a fixed sequence of seeds from it; the interpreter draws an unpinned secret from its own random numbers, so a run stays deterministic. A program that never asks for the seed reads neither the variable nor the entropy. The test runners pin `MAXON_HASH_SEED` for every program they start (`hashseed.ps1`), as maxon's harness hands its programs the run's seed |
| `__ManagedMemoryCursor with T` | maxon's position in a raw buffer, which only the library tiers name (`E3185`), made by a buffer's `createCursor()` at its first element, which fails with `__ManagedMemoryError.indexOutOfBounds` on an empty buffer. It holds a borrow of the buffer (see "Borrowing values"), so the buffer cannot change while it lives and it always names an element: `current()` lends it, `index()` is its index, `advance()`, `retreat()` and `seek(index)` move it (`mutates self`) and fail, leaving it where it was, past either end, and `peek(ahead)` is a copy of the element so many places on, where `T` is `Copyable` |
| `ArrayError`, `MapError`, `IterationError`, `__ManagedMemoryError` | maxon's error enums: `indexOutOfBounds` and `emptySlot`; `keyNotFound` and `keyAlreadyExists`; `exhausted` and `atStart`, which the iteration protocol throws; a buffer's, whose first two cases are an array's |
| `ByteArray` | `typealias ByteArray = Array with Byte`, built in: the same type, with every operation of `Array`; `String.toByteArray()` is a new one of a string's UTF-8 bytes, and `String.from(bytes)` a string of a copy of them |

A fixed array is an array's box with its count of elements: the instance states the count as one more type argument after the
element, the integer constant (`ast.countedType`, `ast.statedCount`), spelled before the element in a type's name and its link
name (`FixedArray with 3 Int`), and a count the type states is a constant wherever the lowering reads it.

**`sizeof(T)` and `countof(T)`.** Both take a type. `countof` of a fixed array that states its count is that count, an integer
constant, and of any other type `E2015`. `sizeof` is the bytes of a value as minimaxon lays it out: 1 for a `bool`, a word (8)
for an integer, a float, an enum, a handle and a function, a word to each field for a record or a tuple (its box), and a word for
any other value, which a place holds as the address of its box. Of a concrete type it is an integer constant; of a type that a
generic's parameters build it is a `Count` that each instance computes from its arguments.

An array of an integer type whose range fits in a `Byte` holds one byte to an element, natively and under the
interpreter, and any other array a word; the program cannot tell, except by how much memory it takes.

**Array and map literals.** `[a, b, c]` is an `Array` and `[k: v, l: w]` a `Map`, written on one line with at least one element:
`[]` is `E2015`, as in maxon (its element type cannot be inferred). A literal is what the explicit code
does: an empty collection, then a `push` or an `upsert` of each element in the order written, so each element is checked against
`value` (and `key`) as that call's argument is, moved in under the same rules, and a later `upsert` of a key replaces its value. The
element type is the one the context expects when it is an `Array with T` or a `Map with (K, V)` (an argument, a return, a field, an
assignment, a typealias'd `let`), else the first element's, with a later integer joined to it as the branches of a conditional are
(`E3005` for two types that do not join; for a negative number and one above `i64.max`, "no common integer type"). Bare numbers have
no alias, so a literal of them keeps the range it spans, `[1, 2, 3]` being `Array with int(1 to 3)`, which a `let` reads at any alias that holds it
(`return try [10, 20, 30].get(0) otherwise 0` is an `ExitCode`); a literal inside another takes the whole word instead. A `var` cannot
start from a collection of bare numbers, which could not take another later (`E2004`, as for `var x = 5`): `var xs = [5 as Count, 6]`. A
module-level `let` of a literal is a constant that each use builds again. `Items{}` over an alias of an array is maxon's
literal of an `Array` with every field at its default: a new empty array of the alias's element, as `Items.create(0)` is,
written anywhere (a field it names is `E3011`).

**`Type from value`.** `Alias from [a, b]` is the array literal written as the alias's array: its element type is the alias's
(`Ages from [1, 2]` over `typealias Ages = Array with Age`), so each element must fit it, a `var` may hold it, and the
elements are checked as an argument is. Over a `List` alias (or the bare `List`) it is the list that the literal builds, with
each element appended in order; the bare `Array from [...]` and `List from [...]` take the element type from the elements, as
a bare literal does. Any other type is written from a string literal, `Type from "text"`, which is `Type.init("text")`:
the type says `implements InitableFromStringLiteral` (`E3005`, "Type 'T' does not conform to InitableFromStringLiteral",
at the type's name, when it does not) and declares `static function init(value String) returns Self`. `FilePath` is one:
`FilePath from "a/b"` is `FilePath.from` that stops the program with "FilePath: invalid path" where `FilePath.from` would
throw.

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
  `for (i, x) in a.enumerated()` walks it the same way and also binds each element's index, an `ElementIndex`, to `i`
  (`_` for the one the body does not use): the loop is the one over `a` with the index it counts given to `i`, so no
  pair and no iterator is made. `enumerated()` is a form of the `for` and not a method: it names the elements of an
  `Array` or a `FixedArray`, the only collections whose elements have an index, and over anything else, or with one name,
  it is `E2015`.
  A type walks with maxon's iteration protocol, found on its concrete type, or for a type parameter on the requirements of
  its bounds (one bound that requires each method): a cursor declares `current()`, which gives
  the element and cannot fail, and `advance(mutates self) throws IterationError`, which fails past the last one; a
  collection declares `createIterator()`, which returns a cursor and may throw `IterationError` where it is empty. A
  loop over a cursor moves it on in place (a place it is must be mutable, and is held while the body runs); a loop
  over a collection calls `createIterator()` once and owns the cursor, and holds the collection while the body runs where
  the cursor borrows it (see "Borrowing values"). Each `current()` is a value of its own, owned by
  `x` until the pass ends, or, where `current()` returns borrowed (a requirement or a method), lent to `x` as an array's
  element is: a borrow of the cursor, which the loop holds, so moving it is `E3900` and `.clone()` the fix. A throw of `createIterator()` or `advance()` ends the loop, and a `createIterator()` that
  throws another error is `E2015`, as is a loop over anything else: a string, a list, a pool. A string's `bytes()`,
  `codepoints()` and `utf16()` views are such collections; a loop over the string itself waits for `Character`.
  The order in which a map is walked is not specified, and the implementation walks it as is most efficient (two
  loops over a map that is not changed see the same order). `for (key, value) in m` binds the key and the value of each
  entry, `_` for the one the body does not use, and each is lent as an array's element is, a scalar copied and anything else a borrow (`E3900` to move it, `.clone()`
  is the fix). Both are `let`s (`E2013`), the key is read-only, and the value's fields are as writable as the map's. The
  map is held while the body runs as an array is (`E3186`). A map holds no pair of a key and a value, so a loop that
  names one variable, `for entry in m`, is `E2015`. The lowering is a loop over the entries' positions, reading the key
  and the value in place with `rt.map.keyAt` and `rt.map.valueAt`, so a program that walks a map runs under the
  interpreter, as every use of a map does.
  `E.allCases` is a new `Array` of an enum's cases and `E.allCaseNames` one of the names of the cases of an enum or a
  union, `Array with String`, in declaration order; both are read before a case of the same name. `allCases` of a union
  is `E4006`. A loop over either walks the array as any other (`for c in E.allCases` is a loop over a fresh array).
- **Running them.** An array runs natively: heap-allocated, bounds-checked, and freed with what it holds where its owner
  dies (see "The primitives in the IR" and "Native freeing"). A map, a list and a pool are checked and lowered, and the interpreter runs them (see "The interpreter"), so a
  program that uses one is refused when it is compiled to an executable (`E9900`) and still passes `check`; the test
  runner runs such a program under the interpreter alone.
- **Limits.** A primitive implements no interface (`E3017`). A record
  that contains itself through an array (`var kids as Array with Tree` in `Tree`) is fine, natively too: its drop and
  its copy are routines that call themselves.

### Raw values

A case of an enum, and of a union, may write a raw value after `=`: an integer, a float or a string with no interpolation.
The first case settles what they are and every later one agrees (`E3032`: `raw value type mismatch: 'expected int, got
float'`), where a case that writes none is an integer, the one after the case before it, from 0; no two cases have the same
value (`E3031`). `.rawValue` has the type of the values: a `Count` or a `FullRangeInt` for integers, a `FullRangeFloat`, a
`String`. An enum is stored as its case's index, so a raw value is found by a switch on it, and a union value's tag is its
case's index while its `.rawValue` is the tag the case writes (`add(dest Id) = 5`), and its `.ordinal` the index.
Interpolating an enum value that has raw values writes the raw value, and an enum with none the name of the case.

**Records and functions.** A raw value that is no literal is a **constant of the program**, checked, ordered and lowered as a
top-level `let` is (`static let` and module `let` alike): a record literal whose fields are constants (`OpMeta{latency: 1}`,
records nested in records, a module's constants), a function named without a call (`doubleFn`, `alpha.doubleFn`), a closure
that captures nothing, or a call of a **literal factory**. A literal factory is a static function of a type that is not
generic, that is itself not generic and throws nothing, and whose body is one `return` of a record literal whose fields run no
code (its parameters, constants and `.clone()` of them: `return Self{latency: latency, memory: memory}`); its arguments are
constants too, so `OpMeta.create(4, isMemory: true)` is the literal with the arguments written in, and a constant of the module
may call one as a raw value does. Any other call is refused as in a constant (`E2045` for a call whose result is a number, an
enum or a bool, `E3220` for any other, which names what may be called), and so is a factory whose body does anything else, a
`print` among it: what a raw value is never depends on code that runs. Every case writes the same type, which is the type of
`.rawValue` and must be `Copyable`, compared once the expressions are typed; a case of another type or one that writes a
literal or none beside them is `E3032` (`expected Meta, got Other`). The constants stage types the raw values of a type after
the constants and raw values of its module that they name, and a constant after the raw values it reads, so a constant may
read `Flags.first.on` and a raw value another type's, and only a cycle is `E2012`. The expression may build any record,
whatever its constructors say, since it belongs to the type. The type holds no value of its own, only the case's index,
and the read is a switch on it. When every case's raw value is laid out in the data section (see "Constants in the data
section") the arms give the datum of each case, which the reader borrows and a use that takes it copies; otherwise the arms are
the expressions, built where they are read, so a read gives a value that the reader owns (a record is dropped with the
statement, a `let` binds it, a function value is a new closure of the function). Either way the raw value of a case can never
be written through, and since it calls only literal factories no read runs code of the program. The type of `.rawValue` of a union is the same, and its tag stays the case's index.

A field of a record raw value that holds no value of its own (an integer, a float, a `bool`, an enum) reads straight off
the enum or union value, as `op.latency` for `op.rawValue.latency`, also off a case that a union names even when it has a
payload (`Op.add.latency`, with `.name`, `.ordinal` and `.rawValue`, which need no value of the union): a field named
`name`, `ordinal` or `rawValue` is reached only through `.rawValue`. A field that holds a `String`, a record or any other
value is read as `op.rawValue.label`. Interpolating such an enum writes the name of the case.

### Format specifiers

`{value:spec}` in an interpolation writes an integer, a float or a `String` in a field, as maxon does; the specifier is the
bytes up to the `}` and is read once, by `ast.parseFormatSpec`. For an integer it is `[0][width][base]`: a leading `0` fills
with zeros after the sign, the width is a minimum, and the base is `x`, `X`, `o` or `b`, where decimal (any other letter, `d`
among them) writes a sign only for a type that may be negative and the others write the bit pattern of the 64-bit word; for
a float `[0][width][.precision]`: the places are rounded half to even from the exact value of the double, and without them
the shortest text that reads back as it, with the sign before the fill and `inf` and `nan` as they are; for a `String`
`[width]q` writes it as a literal with the escapes `\"`, `\\`, `\{`, `\}`, `\n`, `\r`, `\t` and `\u00XX` of the other control
characters, padded on the left with spaces. A width or a precision past `u32.max` is `E2011`. The text is made by
`formatInteger`, `formatPadded`, `formatQuoted` and `formatFixed` of `runtime/textops.maxon`, which the interpreter shares,
from the exact decimal of the double that `ftoaExact` writes (`runtime/floats.maxon`, `ast.floatExactText`).

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

Clusters follow the rules of UAX #29 (GB1 to GB9b, GB11, GB12 and GB13) from the Grapheme_Cluster_Break and
Extended_Pictographic values of Unicode 16.0.0, which `internal/textgen` writes from the data files in its `ucd/`
directory to `runtime/unicode_gen.maxon` and `self/interp/unicode_gen.maxon`: a string of the first code point of each run
of one class and its class, searched by halves, with the Hangul syllables computed from their place in the block. Maxon
joins only the marks in a few ranges; here every Extend, SpacingMark and Prepend code point joins. The Indic conjunct rule
GB9c is not implemented. `StringIndex` has no `equals` or `compare`; `String` has no `split`, `hash`, `charAt` or
`Character` yet.

`bytes()`, `codepoints()` and `utf16()` give maxon's views `ByteView`, `CodepointView` and `UTF16View`, whose elements are
a `Byte`, a `Codepoint` (`int(0 to 1114111)`) and a `CodeUnit16` (`int(0 to 65535)`). A view has `count()` and walks with
the iteration protocol (see "Iteration"): a `for` over it, or `createIterator()` and an iterator's `current()` and
`advance()` as maxon's. Unlike maxon's they are not lazy: a view and an iterator each hold a copy of the string, so
changing the string leaves them as they were. A view is no `Array`, as in maxon.
### The OS

The OS functions are maxon's stdlib, names, labels, results and errors, declared as static functions of built-in
records that have no fields (`internal/builtingen/table.go`), so a program calls them as maxon's does:

| Call | Gives | Throws |
|---|---|---|
| `CommandLine.args()` | `Array with String`, the program's own name first | — |
| `Process.environmentVariable(name)` | `String` | `ProcessIntrospectionError.variableUnset` |
| `Process.executablePath()` | `FilePath`, the running executable's full path (`GetModuleFileNameW`) | `ProcessIntrospectionError.pathUnavailable` |
| `Process.id()` | `Count`, the running process's id (`GetCurrentProcessId`); maxon's `Pid` is the same unsigned 64-bit integer | — |
| `Process.setExitCode(code)` | — (the process ends with the last code set in place of `main`'s; see "Where it differs from Maxon") | — |
| `Clock.nowNanos()` | `Count`, the monotonic clock in nanoseconds; only differences of two readings mean anything (see "Where it differs from Maxon") | — |
| `Scheduler.processorCount()` | `Count`, the workers the runtime runs behaviours on: `MAXON_MAX_PROCS` when it holds a count, else the machine's processors (`GetActiveProcessorCount`); maxon's `SchedulerProcessorCount` is the same unsigned 64-bit integer; the interpreter has one worker and answers 1 | — |
| `FilePath.from(path)` | `FilePath`, with `/` made `\` | `FilePathError.invalidCharacter` for a control character or one of `<>"\|?*` |
| `path.toString()` | `String` | — |
| `File.readText(path)` | `String` | `FileReadError` |
| `File.writeText(path, content: s)`, `File.writeBinary(path, content: bytes)`, each with an optional `mode: FilePermission` | — (a file that is there is replaced, and keeps its mode) | `FileWriteError` |
| `File.rename(from, to: dest)` | — (what is at `dest` is replaced) | `FileRenameError` |
| `File.delete(path)` | — (a directory is refused, `accessDenied`) | `FileDeleteError` |
| `File.info(path)` | `FileInfo`: `size`, `modifiedTime`, `createdTime` and `accessedTime` (Unix seconds on the file system's clock, as maxon's), `isDirectory`, `isReadOnly`; read from the directory's record of the file (`FindFirstFileW`), so a file another process holds open is no failure | `FileInfoError` |
| `Directory.isDirectory(path)` | `bool` | — |
| `Directory.create(path)` | `bool`, whether the directory is there after it; the missing parents are made too | — |
| `Directory.currentPath()` | `FilePath`, the working directory; a panic when Windows cannot give it | — |
| `Directory.list(path)` | `Array with FilePath`, each the path joined to an entry's name (no `.` or `..`) | `DirectoryListError` |
| `Subprocess.run(executable, arguments: args)` | `CollectedOutput`: `exitCode()`, `succeeded()`, `stdout`, `stderr` | `SubprocessError` |
| `Subprocess.runWithDeadline(executable, arguments: args, deadline: d)` | as `run`; a child still running when `d` (maxon's `WaitDeadline`: `WaitDeadline.none` or `WaitDeadline.within(ms)`) passes is terminated | `SubprocessError`, `timeout` for the deadline |

- **`FilePath`** is a record holding the path's `String` (`path`), which every `File` and `Directory` function takes. It
  is made with `FilePath.from`, or from a string literal with `FilePath from "text"`, which panics where `from` would throw.
- **Errors** are maxon's enums, cases and all: `FileReadError` (`notFound`, `accessDenied`, `busy`, `failed`),
  `FileWriteError` (`failed`, `notFound`, `accessDenied`, `alreadyExists`, `busy`, `partialFileLeft`),
  `FilePermission` (`normal`, 0666, the default of `mode:`, and `executable`, 0755, which only Linux's runtime has an effect for),
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
- **`Subprocess.runWithDeadline`** is maxon's `Subprocess.run` with its `deadline:`, a function of its own because a built-in's only
  default argument is the case of an enum (`mode:` of the writes). When the deadline passes the run ends the child with `TerminateProcess` and throws `timeout`; it
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
behaviour-oriented concurrency. A compiled program runs behaviours on worker threads (see "Frozen values, cowns and
behaviours in the IR"); the interpreter runs them under a simulated scheduler, and the checker's rules are what the
interpreter's runs test.

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

### Coroutines: async and await

maxon's surface, on the green threads of the runtime (see "Green threads"):

- `async f(args)` starts a direct call of a function or a static function as a **coroutine** of the caller's strand and
  gives its **promise**: `Promise with T` of a function that returns `T` and throws nothing, `Promise with (T, E)` of one
  that throws `E`. The caller goes on; the coroutine runs once the caller waits, and the strand's members never run at once.
  `await p` gives what the function returned, and stands alone as a statement where it returns nothing; a promise whose
  function throws is awaited with `try await p otherwise ...` (every form a call's `try` takes), and a plain `await` of it
  is `E3057`. `sleep(milliseconds)` (a `Milliseconds`) parks the green thread, and `Scheduler.yield()` lets the members of
  its strand that wait run first, else the runnable green threads. `File.exists(path)` is maxon's, as I/O.
- Refused as maxon refuses them (`E2015`): `async` of what is not a call, of a method of a value, of a built-in, of a
  function that returns a float (and, minimaxon's, one that takes a float or more than eight arguments, the words a
  coroutine holds). `await` of what is not a promise is `E3005`. A function the async call starts must be able to wait
  (`E3073`, maxon's inference, `self/check/waits.maxon`): it awaits, sleeps, yields, does file or process I/O, calls
  through a function value or an interface, calls a function of another module, or calls or starts one of its module that
  may.
- **What the coroutine takes of its caller** (`self/check/ownership_async.maxon`), where maxon shares by counts:
  - a scalar is copied, and a value passed to a `consumes` parameter moves in;
  - a place passed to a `let` parameter is lent to be read until the promise is awaited: the caller may read it, but
    changing it, moving it or passing it `mutates` is `E3943`; a temporary there is `E2015` (the statement would drop it),
    a string literal aside;
  - a local `var`, or a field of one, passed to a `mutates` parameter is lent to be changed until the await: any use is
    `E3102`, maxon's message; anything else by reference is `E2015`, and a function value that lives on and borrows
    what is lent is `E3186`;
  - a promise is taken only by a `consumes` parameter (`E3141` at a `let` one).
- **A promise is linear.** It is never copied and never dropped: on every path out of its scope it is awaited, returned
  or passed to a `consumes` parameter, else `E3213`. Awaiting it again is `E3142`, and awaiting inside a loop one started
  outside it `E3100`. A promise whose call lent anything is **tied** to what it borrows: it is bound by the declaration
  of a local, which ends inside the scope of everything it borrows, and awaited there; returning it, storing it, passing
  it on or assigning it to a variable declared before it is `E3212`, and so is an exit that leaves it unawaited. A
  closure that captures a promise is `E3141`, and a behaviour that does `E3909`: the coroutine belongs to its strand.
  So every coroutine ends before the frames that lent it anything do, and its borrows are those frames' own.
- **In the IR** an async call is one instruction, `%p = async f(%a, %b)`, whose arguments are lowered as the call's
  would be; the backend makes the coroutine with the address of the function's thunk (`async <f>`, written in each unit
  that starts `f`, the link keeping one), writes the words into it and queues it in the strand. An await is the
  runtime's: `rt.async.await` for a function that throws nothing, and `rt.async.wait`, `rt.async.error` and
  `rt.async.take` with the failure branch of a call for one that throws. A promise's drop routine meets only the empty
  place an await leaves, and panics on a coroutine.
- **The interpreter** runs no coroutine: an async call ends an interpreted run with `async calls are not supported by the
  interpreter`, so a test that starts one is `// native-only`. Running them needs a strand of its threads (the
  scheduler already keeps each thread's frames apart, so a coroutine is a thread that runs only while it holds its
  strand) and a clock that sleeps advance; `sleep` and `Scheduler.yield()` change nothing there.
- Not yet: `p.cancel()`, `p.isComplete()`, `awaitAny`, promises in containers and fields (a promise is not `Copyable`
  nor `Droppable`, which arrays need), `sendTimeout:`, the deadlock exit 92, and I/O on the poller.

### Services

maxon's surface, on cowns, behaviours and green threads (`self/check/services.maxon`, `self/ir/services.maxon`,
`runtime/service.maxon`):

- `spawn T.f(args)` starts a service of the type `T`, declared with `type` and no type parameters, whose static factory
  `f` returns `T`, and gives its handle, a `T.handle` (`E3134` for any other target). `spawn(capacity: n)` sets how many
  messages its mailbox holds (a `MailboxCapacity`, 1024 by default). A factory that cannot throw runs as the service's
  first message; one that throws runs where the spawn is, and the spawn fails with its error (`try spawn ...`).
- A call through the handle of an export instance method of `T` is a **message** (`E3136` for any other method): the
  service runs it on its own green thread, one at a time, in the order the sends were made, so a handler that sleeps or
  is preempted still holds the service. Every send can fail, with `SendError`: `mailboxFull` when the mailbox holds its
  capacity of messages the service has not started, `serviceStopped` after `h.shutdown()`, which closes the mailbox and
  lets the queued messages run. A send without `try` where nothing catches is `E3057`.
- A handler that returns or throws **replies**: the send gives `Promise with R` (or `Promise with (R, E)`), which the
  sender awaits as an async call's promise (`try await h.total() otherwise 0`, or `let p = try h.total()` and `await p`);
  `_ = try h.total() ...` drops the reply on the service, and a reply sent as a statement is `E3190`. A reply's promise is
  never tied: nothing is lent to a service.
- **What crosses** is one predicate, `check.unsendable`, for every parameter of a message and of the factory, every reply
  and every error a message throws, at the spawn: a scalar, a `String`, an enum, a value that owns its parts and holds only
  what crosses, a `frozen with T` of such a value, a cown and a handle cross; a borrow, a promise, a function value, a
  value at a type parameter or an interface type, and anything that holds one do not (`E3217`, `E3140` for a reply). A
  parameter is `let` or `consumes`, never `mutates` or `sets` (`E3217`); either way its argument moves into the service,
  so the sender's variable is spent (`E3102`), and the contents of a frozen value, which are lent, are not sent (`E3217`).
  The state holds no promise (`E3216`) and reaches no handle to its own service, through fields, containers, cowns and
  other services' states (`E4014`); no parameter is named `sendTimeout` (`E3192`).
- A message is as visible as its service and no more (`E3167`), as maxon holds it: an unexported type that its module
  spawns may name unexported types in its messages. A spawn in another module does not narrow them.
- An idle service holds its state, its cown and its gate (48 bytes), and no green thread or stack: each message is a
  behaviour that takes a green thread's record and a stack from its processor's pools when it runs and gives them back
  when it ends, so a handler that sleeps or awaits holds one of each for that long. maxon runs a service's messages on one
  runner, up to 64 a turn; here each is its own behaviour on the one cown.
- **Holders never wait** (`E3221`): a message or a factory of a type the module spawns that keeps a reply, or starts a
  service whose factory throws, directly or through a function of its module, is refused, so services never wait on each
  other in a cycle. A function of another module, a function value and an interface call are not seen.
- **In the IR** a spawn is `rt.service.new` (a cown whose value is the state, with the mailbox's gate after the cown's
  words) and, for a factory that cannot throw, a `when` on it whose body calls the factory and stores the state. A send
  reads the handle, lowers the arguments as a `consumes` call's, takes a place in the mailbox (`rt.service.reserve`, which
  fails as a call fails, dropping the arguments in flight), makes the reply's cell where the sender keeps one
  (`rt.reply.new`) and queues a `when` on the service's cown whose body (`<f>$when<n>`, made in `ir/services.maxon`) gives
  the place back (`rt.service.taken`), calls the handler with the state and the arguments, drops what a `let` parameter
  only borrowed, and gives the cell what the handler returned and threw (`rt.reply.give`), or drops it. The cell is a
  green thread's record that never runs, so `rt.async.await` awaits it as a coroutine.
- The interpreter runs no service: a spawn or a send ends an interpreted run with `services are not supported by the
  interpreter`, so a test that starts one is `// native-only`.
- Not yet: see "Where it differs from Maxon".

### Identity

`a is b` is true when two names stand for one object, and `a is not b` when they do not (maxon's reference identity; the
operator ranks with the comparisons, below them and above `and`). It takes two operands of one type that has an object: a
`type` or a tuple, a `String`, an `Array`, a `Map` or a `List`. A number, a `bool`, an enum case, a union, a function, a
type parameter and anything else is `E3068` at the `is` (`'is' requires reference types (structs), not primitive values`), and
two operands of different types are `E3005`. What it compares is the two objects' addresses. A record has one owner, so
two names stand for one record only where one is a borrow of the other, or both borrow one owner: `a is a` and a
parameter handed `a` twice are true, two records built alike and a `.clone()` are not, and `let b = a` is a move
(`E3102` where `a` is used again), so nothing like maxon's alias made by `let b = a` exists to compare.

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

### Default parameter values, static fields, default throws and fallthrough

A parameter takes a default with `= expr` after its type (`function connect(port Port = 80, retries Retries = 3)`), on any
parameter that is not `mutates` or `sets`, and on a function or a method but not on an interface requirement, a union case or a
closure (`E2015`). The default is an expression of the module that declares the function, written and resolved there without the
parameters, and a call that leaves the argument out evaluates it, in that module, as an argument of its own (a default naming a
constant, an enum case or a `static let` reads it there). It is typed against the parameter's type where the function is checked
(ownership and ranges too, as the argument of a call), and the call keeps `0` in `CallInfo.ordered` for it, which lowering fills
from `ParamSpec.defaultExpr`. A call may leave out any parameter that has a default; the first argument may be named when the
first parameter has a default (`connect(retries: 5)`), a later argument is always named, and a missing parameter with no default
is `E3036` as before.

`default throws E` in a match, as a statement or an expression, is the statement `throw E` as the whole of an arm (the parser builds a
`ThrowStmt` for it), so the function must declare `throws` and an arm of an expression has no value. Because the arm is a
`throw` statement, `default then throw E` is accepted for the `default` of a match on an enum or a union as well, where maxon says
`E2046`.

`stmt and fallthrough` ends the arm of a match statement: after it the next arm's statement runs without testing that arm's
pattern, and on the last arm it leaves the match. `and` followed by `fallthrough` is not an operator (the `and` of an
expression carries a guard in the grammar, `{notFallthrough}?`), `fallthrough` is a reserved word, and `return ... and
fallthrough` is `E2025`. The arm it falls into must bind no payload name (`E2015`): the ownership and range analyses join the
state the arm leaves with the one the dispatch gives the next arm, and lowering jumps to the body of the next arm. `E2025` is
reported at the arm's statement and not at the `and`, and `and fallthrough` after a `gives` arm is a syntax error.

`static let name = expr` in a type's body declares a constant of the type, read as `Type.name` anywhere and as `name` inside the
type's own body, where its initializer is also written (it may build `Self{...}`, which a constant of the module may not). The
parser makes it a constant of the module named `Type.name`, as it does a `typealias` of a type's body, and the resolver turns
`Type.name` into a name bound to it, so it is typed, ranged, ordered (`E2012` for a cycle) and lowered as a module-level `let`:
its value is read from the data section when it is a box known before the program runs and inlined at each use otherwise
(see "Constants in the data section"), and a call in its initializer is `E3220` unless it is a literal factory (see "Raw
values"). `static var`, and a `static let` of an enum or a union,
are not supported.

### Conditional compilation, tests and annotations

`#if condition`, `#else` and `#endif` include or leave out code by the target, as Maxon's do (`docs/LANGUAGE_REFERENCE.md`,
"Conditional Compilation"). A directive stands on a line of its own, between declarations, between the statements of a
function or between the members of a `type`, `enum`, `union`, `interface` or `extension`, and the regions nest. A condition
combines `os(Windows)`, `os(Linux)`, `os(Macos)`, `os(Wasi)`, `arch(x64)`, `arch(arm64)` and `arch(wasm32)` (an argument of
another name is false, and the case of the argument does not matter) with `not`, `and`, `or` and parentheses (`or` binds
loosest). The target is `ast.Target` (`self/ast/target.maxon`), the one place that names it, which a project holds as its
`target`: x64 Windows, until another target is added there. `testing(...)`, `rcSanitize(...)` and `leakReport(...)` are flags
this compiler does not have, so each holds for `false` only, and so does `debugstream(...)`, since there is no
`--debugstream`. Any other function in a condition is `E2064`; a condition that does not read, or that text follows on its
line, is `E2065`; an `#if` with no `#endif`, or an `#else` or an `#endif` with no `#if`, is `E2063`. The directives are
resolved over the tokens of a module before anything reads them (`resolveDirectives`, `self/syntax/directives.maxon`), so a
branch that the target leaves out is lexed, and so still has to be lexed cleanly, but is not parsed or resolved, and
nothing of it exists. A condition inside such a branch is not read at all. The lexer gives the three directives a token
each, and the branch's own indent and dedent tokens go with them; a body whose every line was in a branch that is left out
has no body.

The layout of a directive is part of the language, as the rest of the layout is (`fmt --check`): the lines of a directive
stand at the indentation of the code around the `#if`, and the code of a branch one tab deeper; no blank line comes after
an `#if` or an `#else` or before an `#else` or an `#endif`; and the blank lines before an `#if` and after an `#endif` are
those the line the `#if` stands for would have (one between top-level declarations, one before a function of a type).
The formatter lays out every branch, as it only moves whitespace: a branch that the target leaves out has to parse too
for `fmt`, and `fmt --check` over a program says the same on every target. A branch holds whole declarations, members or
statements, never part of one.

A `test 'prose name'` declaration (`end 'prose name'` repeats the name) is a function that no program calls, in a file of
tests, whose name ends in `.maxtest`; anywhere else it is `E2058`, and an empty name is `E2059`. `test` is a word of the
language only at the start of a declaration and before a quoted name. A test takes no parameters and returns nothing (the
parser asks for the end of the line after its name), and compiles to a function named `__test_` and its name with every
character that is no letter, digit or underscore made an underscore: two tests of a module that compile to one name are
`E3107`. A test is parsed, resolved and checked, and its body is held to Maxon's rule that it handles every error it raises:
a call that throws needs no `try` in a test body (a closure written in it is an ordinary function), and a bare `try` takes
an error of any type. No program is made of a test: lowering leaves it out. `build`, `run` and `check` read the `.maxon` files
of a directory only, so a `.maxtest` file is no part of a build, and `fmt` lays out `.maxtest` files as it does `.maxon` ones;
a project held in memory, as a spec test's files, names its files itself and its `.maxtest` files are checked with the rest.
`minimaxon test <dir>`, where the directory is a module (it holds source) and it or a module under it holds a file of tests, is
a compile of tests (`Options.runsTests`, `self/driver/unittests.maxon`): the project is the one around the directory (its root
is where the walk up stops finding source, `project.projectRootOf`), the modules under the directory are read with their files
of tests (`project.addTestFiles`), and only they and the modules they name, directly or not, are compiled past the resolve
(`Reach`, which the order decides); each of those modules keeps its tests through lowering (`check.endModule`), no `main` is
looked for, and the link makes an entry that calls every test in module and source order and exits with 0
(`backend.EntryCalls`). The runner runs the executable and prints `ok: N tests of <dir>`, or what the test that panicked
printed and a `FAIL` line. A test fails by a panic: the library's `Expect` and `TestFailure` are not ported, so a body that
throws needs a `try`, as any other function's does, to be lowered.

`@heap` before a `let` or a `var` (`@heap let p = Point.create(1)`) asks for the binding's record on the heap; every record
is a box here already, so it changes nothing in a program. It is held to Maxon's rules: another word after the `@` is
`E2001` (`Unknown annotation '@stack' (expected '@heap')`), a `@heap` that no `let` or `var` follows is `E2001` at the
`@`, and a value that owns no heap (an integer, a float, a `bool`, an enum, a function value) is `E2001` at the name, which
the checker reports where Maxon's parser does, as it needs the type of the value.

### Where it differs from Maxon

- x64-linux: `FilePath.from` turns a backslash into a slash
  and refuses nothing on Linux, as maxon's does. A run of a program found on `PATH` looks for it as `execvp` does, in the run's
  own `PATH`; the run passes the child the run's environment.
- `Process.setExitCode(code)` sets the code the process ends with when `main` has returned and no behaviour is left,
  in place of `main`'s; the last code set wins. Maxon has none: its programs wait for their services with `await`, and a
  behaviour, which nothing awaits, needs this to report the program's result.
- **An async call lends its coroutine what it passes, instead of sharing it** (see "Coroutines: async and await"): a place
  at a `let` parameter is the coroutine's to read until the promise is awaited, so the caller may read it but not change
  it, move it or pass it `mutates` meanwhile (`E3943`, minimaxon's), where maxon lets the caller write a `var` its
  coroutine reads, kept alive by reference counts; a temporary at a `let` parameter is refused (`E2015`), where maxon
  counts it. The spec tests that write such a variable while the call is pending are skipped with that reason. A tied
  promise is any whose call lent a place, not only mutable data, and it may not be assigned to a variable declared before
  it. An async call takes no float and at most eight arguments (`E2015`), where maxon refuses only a float result; maxon's
  `cancel`, `isComplete`, `awaitAny` and promises held in containers and fields are not there yet.

- `Clock` has only `nowNanos()`, which maxon's gives as `InstantNanos`, here a `Count`; maxon's `nowMs`, `osTickMs`,
  `threadCpuTicks`, `elapsedMs`, `elapsedNanos`, `remainingMs` and `WallClock` are not there. Natively a reading is the
  performance counter (`QueryPerformanceCounter`) in nanoseconds; the interpreter's clock moves by 1000 ns at each reading, so a run is deterministic.

- `Scheduler` has only `processorCount()`, which maxon's gives as `SchedulerProcessorCount`, here a `Count`, and `yield()`;
  maxon's `quiesce`, `parallelBoundary` and pins are not there.

- Conditional compilation, tests and `@heap` (see "Conditional compilation, tests and annotations"): a branch of `#if` is indented a tab deeper than its
  directives and each branch holds whole declarations, members or statements (Maxon lets the layout be what it likes and a branch
  be any run of tokens); the targets are x64 Windows and x64 Linux, so `os(Windows)` or `os(Linux)` and `arch(x64)` are the conditions
  that hold, and `debugstream` is never on. A `test` runs nowhere: there is no `maxon test` and no `Expect` or `TestFailure`, a test is
  checked and left out of the program, and a `.maxtest` file is read only where a project names its files itself (a spec
  test). `@heap` on a value that owns no heap is reported
  by the checker and not the parser, and its message names the kind of the value (`int`, `float`, `bool`, `enum`, `function`).
  There is no top-level `default` (`default Key = value`, which Maxon reads with `Key.current()` and `Key.register(value)`): it
  needs a global that the program initialises before `main` and replaces, and the read-only values of Maxon's regions.
- Default parameter values (see "Default parameter values, static fields, default throws and fallthrough"): the caller-location defaults `__line__` and `__file__`
  and a default on a discard parameter `_` (which maxon's calls name with a synthesized label) are not supported, a default that
  trails off is the parser's generic `E2010`, and a default on a closure parameter has no syntax. A `static let` is only a
  `type`'s, and a call in its initializer that names a sibling by its bare name is not resolved to the sibling.
- Values that hold a borrow (see "Borrowing values") are minimaxon's: maxon's cursor retains its buffer by a reference
  count, so a cursor, an iterator and a record that holds one go anywhere, where here they borrow what they were made from and
  stay in its scope. An iterator that escapes, returned past the collection it walks, stored in a field of a value that may
  outlive it, captured, boxed, or given to a type parameter, is refused (`E3927`), and a collection is held while an iterator
  of it lives (`E3186`) where maxon re-reads the live length. A cursor's `peek` gives a copy where maxon lends the element.
- An interface may bound its associated types with `where` (see "Generics"), which maxon's grammar has no place for: maxon
  checks an extension of an interface again for each conformer, so it never needs `Iterable`'s `Iter` to be known as an
  `Iterator`, which an extension checked once does. `cmd/convert` writes the `where` that the interfaces of maxon's library
  need (`interfaceWheres`: `Iterable`'s), and leaves out the members of their extensions that a form of minimaxon's own
  replaces (`replacedMembers`: `Iterable`'s `withIterator()` and its typealias, for which a for walks `enumerated()`).
- An iterator lends its element: `cmd/convert` writes `returns borrowed` on the requirements of maxon's library interfaces
  that hand one out (`lentRequirements`: `Iterator.current()`), so a loop over an iterator, and the code of an extension of
  `Iterable` that walks one, borrow each element where maxon shares it by a reference count. Moving it on is `E3900`, and
  `minimaxon fix` writes the copy, `.clone()`, with the `where ... implements Copyable` the method then needs. A method that
  gives a value meets a requirement that lends only where the value is a scalar that is not a float (`E3016` otherwise).
- maxon's `Cloneable`, `clone() returns Self`, is met by the `clone()` every Copyable type has (see "Generics"): the
  interface extends `Copyable`, so a type that implements it and cannot be copied is `E3918` where maxon would refuse its
  generated `clone()`, and no type declares a `clone()` of its own: `cmd/convert` leaves out a library `clone()` whose body
  is `return self` (`PrimitiveExtensions.maxon`'s), which is that built-in clone.
- `interface Sub extends Base`, where `Base` has associated types, gives `Sub` those it does not declare itself (see
  "Generics"), so `implements Sub with X` binds `Base`'s `Element` too, where maxon counts only `Sub`'s own and compares an
  inherited requirement as written unless the clause binds `Base` beside it (which minimaxon also reads). maxon's
  `E2066` for `implements Sub with (X)` is therefore not raised.
- `returns borrowed T` (see "Ownership") is minimaxon's: maxon has no function that gives a place instead of a value,
  so a lookup through a function there gives a copy. It is Hylo's `subscript` with only a `let` accessor, written as a
  function and restricted so that it needs no coroutine: the place is returned, never yielded, so no code of the callee
  runs after the caller's use of it, and a projection borrows its arguments for as long as the caller uses it, as Hylo's
  does. Its `return` of the contents of a frozen value is minimaxon's too. Not built yet: the `mutates` form, which lends
  a place the caller may change (Hylo's `inout` accessor), and subscript syntax on user types (`v[t]` calling such a
  function).
- `extension`: there is no `typealias` in the extension of an enum or a union, and none of `Set` or other library types
  the compiler does not have. A typealias of an extension of an interface names the interface's associated types as its
  methods do: `typealias ElementArray = Array with Element` in `extension Iterable` is an array of each conformer's
  `Element` where a method that names it is instantiated. An extension of an interface is Hylo's: its body is checked once, generic over the
  conformer, where maxon parses it again for each conformer with the conformer's names in scope, so there it may use what
  one conformer has (a field, a method the interface does not require, an operator or a value of another type for an
  associated type) and its errors name the conformer (`type 'Square1' has no field named 'spare'`, here `'Self'`). Its
  methods are dispatched statically: where maxon's copy for a conformer calls the conformer's own method of a name, a call
  inside the extension, through a bound or on an interface value here runs the extension's. They reach every conformer, an
  enum, a union or an integer type too, where maxon expands them onto `type` conformers alone and refuses one on a
  primitive (`E2015`). A conditional extension's message says `where Item implements Sizer` for maxon's `where Item is
  Sizer`. Extensions and the conformances they
  declare are scoped as Hylo scopes them, where maxon sees every extension from every directory of the program: an
  extension is seen only from its own module and the modules that name it, a bound is met by the conformance seen from
  the module that instantiates the generic and passes it on with the instance, two extensions seen at one call that add
  one method name, or one typealias name, are `E3942`, and two conformances of one type to one interface seen at one place are `E3941`. A module
  that declares neither the type nor the interface gives its conformance to no other module (Hylo's orphan rule). A method
  that an extension declares under the name of a method in the body of a type of another module is `E3006` at the
  extension's method, reported when the modules are verified. A typealias of an extension is a member of the type for the
  modules that see it, where maxon has one member of each name for the whole program: two extensions of different modules that
  add one name are `E3942` at the use that sees both and not `E3061`, and `Type.name` of a generic type, which would need the
  type's arguments, is `E2015`. A ranged typealias of an extension of a generic type is `E2015`, as in its body. A function
  typealias of a non-generic type or extension is a type of its own, as a top-level one is; in the body of a generic type it is not.
- `for` loops (see "Primitives"): a `Map` is walked as `for (key, value) in m`, in an order that neither language specifies (the two differ in practice), and a loop that names one variable for the pair
  (maxon binds a tuple) is `E2015`; the key and the value are
  lent, not copied, and the map is held while the body runs, so a body that changes it is `E3186` where maxon re-reads it. `E.allCases`
  and `E.allCaseNames` are a new array each time they are read, since an array has one owner, and `Shape.unionCases` is not
  supported. Not supported, and skipped by the converter: a loop over the characters of a `String` (there is no `Character`),
  over its grapheme clusters, `split()` or `lines()`. maxon's `for (iter, x) in xs.withIterator()`, whose iterator the
  body asks for `iter.index()`, is `for (i, x) in xs.enumerated()` here, as in Hylo and Swift: the loop binds the index
  itself and there is no iterator object, so an array is walked with its indexes at the cost of the plain loop. The
  converter rewrites the one form into the other, `iter.index()` into the name; a test whose body uses the iterator for
  anything else is skipped.
- Match arms name the case of an enum or a union by its bare name; a qualified `Color.red` is `E3075`, once for the match as
  maxon does (maxon's `Type.case` arms of a handler over the combined error of several errors are not supported, see
  "Errors of the program"). A bare `int` or `float` is the full range of its kind only on the right of a `typealias`
  (`typealias Pair = (int, int)`, `typealias Op = function(int) returns int`); anywhere else it is `E3005`, and as the
  target of a cast it has the cast's own wording. `int.fromString` and the other static functions of `int` are library, which
  minimaxon has not: a call is `E2004`.
- `a is b` (see "Identity") does not refuse a record that packs into one word (maxon's inline record, `E3068`): every
  record here is a heap box with an address. A literal is not interned, so two equal literals are two objects, and the
  tests that read an alias made by `let b = a`, a string's identity after `.clone()` or the identity of a constant do not
  hold here (`.clone()` of a `String` is the same object).
- A keyword is not a case name: maxon's `end` as the case of an enum, a union or an arm is a syntax error here.
- Interface values (see "Interface values"): maxon's interface value refers to the value it was made from, which the
  program shares; here the box owns the value, which moves into it (a borrowed one takes `.clone()` or `consumes`), and a
  `let` parameter is lent the caller's value for the call. It is one word, the box of the value and its table, where
  maxon's is two (the value and the table), so none of maxon's refusals that the second word makes are here: an interface
  type in a function type, a closure's parameter, a tuple element, a requirement's parameter or result, a container's
  element and the result of a function that throws are all allowed. A parameter of an interface type is a value of it
  whatever the argument, where maxon monomorphizes the function for a concrete argument. `clone()` of one needs the
  interface to extend `Copyable`, which an interface may do and a type may not. maxon has no way to get the value back;
  here `try x as Square` takes it out of the box and fails with `CastError.otherType`, an error of minimaxon's own, where
  the box holds another type. A `mutates` parameter of an interface type takes only an interface value.
- Grapheme clusters follow UAX #29 from the Unicode 16.0.0 tables (without GB9c), where maxon's own rules join only the
  marks in a few ranges; a string of other Extend, SpacingMark or Prepend code points counts fewer clusters here. The views
  of a string (`bytes()`, `codepoints()`, `utf16()`) and their iterators hold a copy of the string rather than viewing it.
- `FixedArray` (see "Primitives"): it is a built-in array's box, not `stdlib/FixedArray.maxon`'s record over a raw
  buffer, so it has no `managed` field, no `init(managed)`, no member called through the type's name (`FixedArray.set(v, ...)`)
  and no `__ManagedMemory` refusals. `set` and `refill` take `mutates self`, as an array's writes do, where maxon writes
  through a `let`: a borrowed element would otherwise be dropped under its borrower. A `try` around a get or a set that the
  range analysis proves is allowed, as any `try` that covers nothing that can fail is, where maxon refuses it (`E3055`). A
  generic body is checked once, so `create()` or `create(n)` of a fixed array of a type parameter is `E3106` whatever the
  instances are, and an enum's first case is no zero value, as for `Array.create(n)`. The count of a type is an integer
  literal, so `with K Int` and `with 3.5 Int` are the parser's generic `E2010` rather than `E2069`. `countof(Self)` is only a
  method's (its receiver's count) in an `extension FixedArray`; a static of one has no count to read, and a fixed array is
  not built with `Self{}`. Two typealiases of one fixed array are one type, as every typealias of an instance is.
- `sizeof` and `countof` of a concrete type are integer constants, as a literal is, where maxon types them `Count`, so they
  fold and fit any integer type that holds them. `sizeof` gives minimaxon's layout, which boxes every record and every
  array: it agrees with maxon for a `bool`, an integer, a float, an enum and a record of word fields, and differs where maxon's
  value is its inline layout: a `String` is a word here and 56 bytes there, an array a word and not maxon's 48-byte record,
  and an inline packed record (all `let` fields fitting 64 bits) a word to each field rather than one word. `sizeof(byte)`
  has no meaning, since there is no `byte` type.
- Tuples (see "Tuples"): a tuple is owned by one variable like a `type`'s record and not shared by reference counting, so
  assigning it moves it and `.clone()` copies it, and a two-element tuple of integers is a heap record like any other (no pair
  in two registers). Destructuring a managed element out of a borrowed tuple, which Maxon shares, is `E3900` and takes
  `.clone()`, and so does naming it in a `for (a, b) in xs` over a collection of them. A tuple of bare numbers has the ranges of
  its numbers (`(int(1 to 1), int(2 to 2))`, for the messages that name it), not `(int, int)`, and two tuple literals of
  different numbers are two types. Not supported: a cast between tuple types (`E3009`, `E3226`), a loop over
  a `Map` that names the `(key, value)` pair by one variable (see "Primitives"), the spelling `t._0` of an element that
  some of Maxon's specs use, a tuple of a type parameter read by field in a generic body, a one-element target list `(x) = e`,
  and the `E3064` lint on a tuple assignment of nothing but `_`.
- A compiled program frees each value where its owner dies, as the IR says (see "Drops" and "Native freeing"), with one
  free and no reference counting; Maxon counts references. A panic and a fault print no backtrace after their line
  (the spec runner leaves maxon's `Stack trace:` lines out of what it compares). A frozen value, a cown and a function
  value are the values with a count of owners, always stepped by one locked add, whatever the program. Behaviours run
  as green threads on Go's scheduler, as maxon's do (`runtime/sched.maxon`, `green.maxon`, `monitor.maxon`; see "The
  scheduler"), with `MAXON_MAX_PROCS` and its acquire of several cowns in address order, which minimaxon does with no
  lock per cown. Stacks never move, since a borrow is an
  address in a frame: a green thread's stack is a fixed reserve with a guard page, where maxon's grow by moving up to 1
  GiB. Preemption is asynchronous only, with no check in the compiled code, where maxon's prologues test a guard word.
  The interpreter schedules behaviours too, and fails a program that leaks a value. A native program that is not built with `--count-allocations` cannot
  tell that it leaked, and a program that panics or traps does not free what it holds.
- Errors (see "Errors of the program"): a `try` block's handler names
  one error, not the combined error of several (refused with `E3059`), and has no terminal `otherwise panic(...)` or
  `otherwise throws ...` form; there is no block handler `otherwise (e) 'label'` on a `try` expression
  inside brackets and no `default throws` in a match. Maxon's `E3168` (a `throws` that nothing throws) and
  `E3083` (a `try` block with nothing that throws) are not raised, so a sharper range analysis never breaks a program. A `try` covers every
  operation of its expression, not one call, and a condition that rules a failure out is a proof the compiler checks
  (see "Checked arithmetic"). Maxon's `E3161` (a handler of a union error must match it exactly once) is not raised: the
  handler owns the error as a local and a `try` block's handler still has to `match` it (`E3084`). A `throw` moves its
  value as a `return` does, where maxon shares it, so `throw holder.fault` takes the field out of `holder`.
- Bit patterns (see "Bit patterns"): only a `bits(64)` wraps; `bits(8)` and the other widths have the checked arithmetic of their
  range, and `int(5)` is a syntax error (`E2010`) where `bits(5)` is `E3148`. A cast to the alias a value already has is
  not refused as unneeded (`E3010`), as for every other alias.
- Raw values (see "Raw values"): integer, float and string raw values of an enum, and an integer, float or string tag on a
  union case; a case written with a character has none yet, and `fromRawValue`, `fromName`,
  `unionCases`, comparing a case with its raw value and using it where a number is expected are the library's and are not
  here. An enum whose cases write raw values interpolates its raw value, as maxon's does, and one that writes none its name.
  A record or function raw value is a constant, as maxon's is, where a call of a literal factory is the literal filled by its
  arguments (maxon reads `Meta.create(1)` so too, never running `create`); here the factory is declared and its body must be
  one `return` of a record literal over its parameters and constants, which a constant of the module may call too where maxon
  refuses every call in a module `let` (`E3220`), and a call of a function that is no such factory is `E2045` or `E3220`.
  Maxon refuses a raw value its constant evaluator cannot hold with `E2015` (a `String` or an array in the record, a field
  left out or given twice, a factory with more arguments than fields), `E3095` for a bare function name that two modules
  declare, and `E3155` for a reserved one; here the ordinary rules say what is wrong (`E3005`, `E3086`, `E3006`, `E3036`,
  `E2004`) and a `String` field is fine. A constant, and so a raw value, is built where it is read rather than held once in
  the executable's data, which no program can tell apart since building it runs no code. `.rawValue` of such an enum or union
  is the whole record, which the reader owns, where maxon refuses it without a field
  after it (`E2015`); and the record that the enum or union exports is not held to be at least as visible as it (`E3167`).
- A format specifier (see "Format specifiers") reads the integers, floats and Strings, and leaves any other value as it is:
  there is no `FormattedStringable`, so `{c:verbose}` of a type of the program is not interpolated, and no `Character` to
  quote. An empty specifier (`{x:}`) is no specifier.
- A `return` converts an integer to the function's declared integer type with no cast where the range analysis proves that
  it fits, and the value that may not fit is `E3057` (see "Integers"). Maxon converts every `return` implicitly and checks
  it at run time, trapping where the value does not fit, so a function that returns an `ExitCode` can return any
  integer there; here the conversion costs nothing and is never a trap, and a value that is not proved needs `try ... as T`.
- A `b"..."` literal is a fresh `ByteArray` each time it is evaluated, copied from its bytes in the executable's data, and the
  program may write it: there is no `immutable` image data, so `E3207`, `E3196` and `E3209` do not exist, and a module-level
  `let` of one is built again at each use, as a literal array is. `\{` and `\}` in one are the unknown escape of a string (`E2001`), and a raw newline in one ends it unterminated, as in a string, where maxon keeps the newline as a byte: write `\n`.
- Function values (see "Function values"): a closure that captures a borrow (a parameter, `self`, a match binding)
  holds the borrow, so it may not escape (`E3925`), where Maxon's closure retains a counted reference to the borrowed
  record and may escape with it; a closure that escapes here owns everything it captures. So a borrowed parameter of a
  function type may not be stored, returned or cloned (declare it `consumes`), and a closure writes what it captured
  only when its function type `mutates captures`: what it owns, which it changes in place and keeps between calls, and a
  mutable variable it borrows (a `var`, a `mutates` parameter, `mutates self`), whose address it holds, where Maxon copies
  a scalar and writes through any captured record. Maxon infers whether a closure mutates its captures and refuses one
  that does at a plain function type (`E3210`); here a closure mutates its captures when it is written where a `mutates
  captures` type is expected, and a write in any other is `E3210`. Maxon's closures are shared whatever their type; here
  a value of a `mutates captures` type moves, is never copied (`E3918`), and is called only through a place the caller
  may change (`E2013`), so a borrowing parameter of such a type is declared `mutates`. The collections have no methods that take a closure (`map`,
  `filter`, `sortBy`).
- Floats (see "Floats") have no interval analysis: an operation over a float that is not a constant gives `FullRangeFloat`,
  where maxon carries the interval of its operands, so a value maxon proves fits a narrower float alias needs `try ... as`
  here. A float `as` and a `return` to a narrower float alias that may not fit fail with `ArithmeticError.overflow` (a NaN
  too), as an integer's do, where maxon traps. Float division by 0 is IEEE's (an infinity or NaN), where maxon's throws
  `divisionByZero` and refuses a constant zero divisor (`E3103`). The float functions take a float or an integer constant,
  not a value of an integer alias, which is cast first.
  There is no float `match` pattern or map key, no `float.fromString`, no `Math`, no format
  specifier for a float, and no `f32` storage. `trunc` of a NaN or of a double past the word gives `i64.min`, as x64's
  `cvttsd2si` does.
- A type takes no capability word in a nested position: `cmd/convert` leaves out maxon's `immutable` (or `isolated`)
  before a type argument (`Array with immutable Key`), a tuple's element (`(immutable Key, Value)`), a field's or a
  variable's type, the target of a cast or a typealias and a function type's parts, since an element here has the one
  owner its container gives it, so nothing else changes a key that a map holds. A receiver's `immutable self` and a
  region bound of a `where` are not types, and the converter still marks them unsupported. A function's own parameter
  and result are not nested, and the next item maps their words.
- Services (see "Services") cross by ownership, not by capability word: a message's, a reply's and a factory's values
  move in, or are a `frozen with T` that both sides share, and nothing borrowed crosses. `cmd/convert` turns maxon's
  parameter `x isolated T` into `consumes x T` and `x immutable T` into `x frozen with T`, and `returns isolated T` and
  `returns immutable T` into `returns T` and `returns frozen with T`; a `let` parameter of a message is given its own
  value, which the behaviour drops once the handler returns. What crosses is one rule (`check.unsendable`), so `E3200`,
  `E3207` and `E3214` do not exist and a value that cannot cross is `E3217` at the spawn, or `E3140` for a reply. A
  factory that throws runs on the spawner's green thread, where maxon's runs on the service's while the spawner waits.
  A send's error is the built-in `SendError` (`mailboxFull`, `serviceStopped`) and gives the arguments no way back,
  where maxon's `<Service>.<message>.sendError` carries them. Not yet: `sendTimeout:`, interface handles (`I.handle`),
  `T.request`, `acquire`, `reply.later()`, `SharedValue`, `default`, a generic service, a message with type parameters
  or a float, one `try await h.m()` over a message that throws, and services in the interpreter.
- No alias subtyping (an alias implementing another), characters, region capabilities (`isolated` and `immutable` of a
  local, a payload or a receiver, and `modify`), static fields, default parameters. The memory model of [docs/regions.md](docs/regions.md) is implemented: owned
  values, the primitives, frozen values, cowns and behaviours (see "Ownership", "Primitives" and
  "Immutable values, cowns and behaviours"), with an interpreter that runs all of it and a native backend that runs
  all but the map, the list and the pool. `freeze` is a function here, `when` is a statement that takes cowns and gives names to their values,
  and a frozen value is a type, `frozen with T`, where Maxon recovers immutability from facts about a value's history.
  The grammar has none of the rest, so each is a syntax error.
- Array and map literals (see "Primitives") are the maxon literals without a literal spread over several
  lines or constant image data: a `let` literal is a fresh `Array` or `Map` like any other value, not an immutable image shared by its
  uses, and its bare numbers keep their range as the element type of that one array instead of fitting any alias at each use, so
  passing `let xs = [1, 2]` to a parameter of `Array with Count` is `E3005` until the literal is written where the parameter is.
- `Array`, `Map` and `List` have the names, the signatures and the errors of maxon's stdlib, but they are built-in
  types rather than stdlib code, and keep these differences: a subscript is a place (`a[i].f = x`, `mutates a[i]`) that
  traps on a miss, where only `get` and `set` throw, as maxon's do; `Pool`, `Handle` and the list's handle API (`prepend`
  and `append` return a `Handle`, `first` and `last` return one, `remove(h)`, `next`, `prev`, `moveToFront`) stand where
  maxon has pointers and indexes; `filled`, `Map.replace` and `dropEmpty` are minimaxon's own; only an array, a `Map`'s
  entries and the views of a `String` are walked by `for x in` (a `String` itself, a `List` and a range value are not), and the loop holds the array or the map, so a body that pushes to it or reassigns it is
  refused (`E3186`) where maxon re-reads the live count; the
  iteration protocol is found on the concrete type or through a type parameter's bounds, never through an interface value;
  there is no `sort`, `contains`, `cursor` or `hash` yet, `reserve` does nothing, since a capacity is no part of an array's value here (there is no `capacity()`), and `clear`, `truncate`, `resize`, `slice`, `first` and `last` are maxon's, and `clearSlot(at)` too, which only the library tiers call (`E3185`): it drops the element at an index and leaves the slot empty, a hash table's tombstone. A slot is empty where a library tier grew an array of its type parameter with `resize` (outside the library tiers that is `E3106`, as maxon refuses it) or cleared it; it is maxon's: an empty slot of a managed element is 0, the empty place a drop does nothing with, so dropping the array drops only the full slots, and a copy keeps an empty slot empty, while over a scalar a grown or cleared slot is the element 0. `get`, `first`, `last` and a buffer's `get` or a cursor's `peek` of an empty slot throw `emptySlot`, and a read that lends it (a subscript, a `for`, a cursor's `current()`) panics with maxon's `empty slot: element N of the array was never filled`, so no element that is not there is ever lent; the indexes and counts they take are `ElementIndex`; `create(n)` refuses an enum element, whose
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
  `Directory` has no `exists`, `delete` or `removeTree`, and
  `Directory.list` lists in the order the system gives; `Process` has only `environmentVariable`, `executablePath`, `id` and `setExitCode`; `Subprocess.run`
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
  type. Interfaces are bounds only: no default method, no existential
  value; a type implements one by saying so, and an integer, a float, `String` and `bool` implement what an extension declares (the
  library's `PrimitiveExtensions.maxon`) and nothing else.
- A generic interface's associated types are bound as type arguments are: one bare, several in parentheses (`implements
  Duo with (Score, Weight)`), where maxon also takes a bare list as long as the interface's arity and ignores a surplus
  in it; a parenthesized list against an interface with one associated type is that many arguments (`E2056`), not a
  tuple, which is written in its own parentheses (`with ((A, B))`). A bound binds every associated type (`where S
  implements Cursor with E`): maxon also takes a bare bound and types a call through it for each instance, but a
  generic body here is checked once, so its calls need the types. `cmd/convert` parenthesizes the lists of an
  interface that the file declares.
- An interface extends only interfaces without associated types (`E2015`), since maxon does not inherit their bindings
  either; a type lists such an interface in its own `implements`.
- Generics are written as in Maxon (`uses`, `with`, `where`), with four differences: an instance is written wherever a
  type is (`returns Option with (Box with T)`), not only through a typealias; a bound is `where T implements Shape`,
  the word a type declares conformance with, rather than `where T is Shape`; a union can be generic; and a call whose
  arguments leave a type argument open takes it from where its result goes (`return Option.none`).
- A type or a union opts out of copying with `is noncopyable`, and declares a destructor to opt out of dropping, where
  Maxon has `unique` (so `is`, `noncopyable` and `destructs` are reserved). `unique` is replaced by this: every owned
  value is already moved and never aliased here, so what is left of it is a value that cannot be copied or forgotten.
  `Copyable` and `Droppable` are structural bounds, not interfaces a declaration implements (see "Capabilities and
  destructors"). `exchange(place, value: v)` takes the label `value`.
- `Type from value` (see "Primitives") is written from a literal only: an array literal for an `Array` or a `List`, a string
  literal for a type with `init`. Not supported: `Set` and `FixedArray`, `List from <an array value>` and `Set from <an array
  value>` (the elements are written as a literal), `Type from [...]` through `InitableFromArrayLiteral`, a map literal under an
  alias, and `Type from 'c'`, since there are no character literals. The expression after `from` is a postfix expression,
  where Maxon takes a literal whole: `Alias from [1, 2].count()` is `E3005`, since a call on the literal is not an array
  literal, and the conversion is parenthesized to call on it: `(Alias from [1, 2]).count()`. `from` is still a name where a
  name is required or an operand is (`function from()`, a parameter read as `from`, `String.from(bytes)`), as in Maxon, and
  starts `Type from value` only after an expression. So is `to` (`move(cursor, to: next)`), which closes a range only after a
  bound, a pattern value or the start of a `for`'s range.
- The reserved words are exactly the keywords the grammar uses, so a word Maxon reserves for a feature minimaxon
  lacks is an ordinary name, and a word that could be read by context, such as `mutates`, `consumes` or `sets`, is
  reserved, except `from` and `to`, which Maxon's programs use as names.
- A parameter convention is a verb written before the parameter's name, with the function as its subject:
  `push(consumes item T)`, `swap(mutates a T, mutates b T)`, `parse(sets result Ast)`, and `mutates self` or
  `consumes self` for a receiver, and `destructs self` for a destructor.
- Modules are Go's, not Maxon's. Another module's names are always qualified by its path (`net.http.parse()`, and
  `export.X` for the root's), where Maxon also finds a bare name in the root or as the one exported declaration
  anywhere (`E3095`, `E3063` for two, which minimaxon does not have). `export` reaches the tree rooted at the declaring
  module's parent, as Go's `internal/`, and `public` everyone; a directory named `internal` scopes its modules to the
  tree of its parent, as in Go. Import cycles are refused. The standard library follows
  the same rules within `stdlib/` (see "Modules"): a library file names another library module's declaration by its path
  from the library's root, where maxon's stdlib names any declaration of the library bare; a program names the public
  declarations of every library module bare, as in maxon, and nothing else of the library. maxon's `stdlib/helpers/`
  is `stdlib/internal/` here, so a program names none of it, where maxon's programs see the helpers' exported names bare
  (`readOrderedWord`, `TableCapacity`); the spec tests that name one outside the library tier are skipped with that
  reason. `helpers/hash/Hash.maxon` is the exception: it names the root's `HashValue` and the root's `Hasher` names it,
  so it is a file of the root. The built-ins are public, so a public
  declaration of the library may name `Count` or `ByteArray` (E3167 holds only for a type less visible than public).
- It checks only the rules that the generated code depends on. The style rules are left out: an unused
  name (`E3012`) or typealias (`E3062`), a `var` that is never reassigned (`E3077`), a label naming the innermost block (`E2048`),
  an unneeded cast (`E3010`, so a cast to the type a value has is accepted), self-assignment or `_ =` on a non-call (`E3067`), a one-value range pattern
  (`E2027`), all bindings discarded (`E3081`), and the lint rules (`E3064`, `E3065`, `E3092`, `E3219`).
- A diagnostic with a maxon code has maxon's code and position, except where the language differs; its text is
  minimaxon's own, and the spec runner compares a diagnostic by its code, line and column alone. The text keeps
  minimaxon's own words where it names a keyword or a form that maxon spells otherwise (`mutates` for `mutable`, so a
  `let` passed to a `mutates` parameter and an assignment to a parameter say `mutates`; `default panic(...)` of an enum
  match, which has no `throws` form; `create(n)` of an element with no zero, which has no `zeroed:`; an overlapping
  argument as one name reached through two, `E3186`). A syntax error is the parser's generic message, not maxon's
  expectation of the token. A type is named structurally, so a generic instance written through a typealias is named
  `Array with Byte` where maxon names it `Bytes`, and an index is a `Count` where maxon's is an `ElementIndex`.
- An array's `.managed` is the array itself seen as a `__ManagedMemory`, owned as the array is: binding it or passing it to a
  `consumes` parameter moves the array, and a borrowed array lends its buffer only to a borrow. Maxon's is a second counted
  reference to the array's buffer, which a program may keep. Each door of the raw surface that a body goes through is `E3185`,
  where maxon reports the first of a body only; a `__ManagedMemory` names its element (maxon also has a bare one), and only
  `length()` and `get(i)` of maxon's buffer operations, and two of `__Builtins`, are there yet.

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
  may be bare. `minimaxon fix` writes the same qualifiers (kind `qualify`) at the errors that report such a name, so a
  project or a spec test is qualified together with its other fixes.
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
  and stops at the first failure. The seed builds `self/` as `bin\minimaxon.exe`, which runs every later step, so a change that only the compiled
  `self/` shows (a symbol that two modules
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
   the IR table (`internal/irgen/irtable/table.go`, see "Generated code"); after `go generate ./internal/...`, `rulegen`
   fails until `internal/rulegen/x64.rules` selects it, and the interpreter (`self/interp`) fails to build until it handles it. A new built-in is a row of
   `internal/builtingen/table.go`, and the dispatch names the function for it in the checker, the lowerer and the interpreter.
6. **Tests:** a `tests/run/` program for the behaviour (it runs natively and under the interpreter), a `tests/errors/` program
   for each diagnostic, and a test in the module's `.maxtest` files only when a part needs one. A new construct that the memory model's
   rules cover also goes into the program generator (`self/fuzz`).

A rule that is only a diagnostic, such as a new ownership or sharing check, usually needs steps 3 or 4 and a test, and nothing
else. Before a commit `.\check.ps1` passes (see "Tests").
