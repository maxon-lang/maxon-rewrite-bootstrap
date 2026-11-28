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

`seed\minimaxon.exe` and `seed/minimaxon-x64-linux` are the compiler built from `self/` and committed, for x64 Windows
and x64 Linux (`seed/README.md` says how both are renewed, together). The day-to-day compiler is built from the host's
seed:

```powershell
.\build.ps1
```

which has the seed build `bin\c1.exe` and C1 build `bin\minimaxon.exe`: `self/` compiled by itself, with today's code
generator even where the seed's is older. It is the one compiler every command of the gate uses: the gate builds it from the
seed, and `go run ./cmd/gate compiler` builds it from the seed unless it is already built from `self/`, `runtime/`, `stdlib/`
and the seed as they are (`bin\minimaxon.stamp` records what it was built from), which the regenerations (`specs.ps1`,
`stdlib.ps1`) do first, so that nothing is regenerated with a compiler of other code; `build.ps1` does nothing when it is
already that build through C1. It finds `runtime/` by walking up from its own executable, so it stays in the tree.

```powershell
.\bin\minimaxon.exe run tests/run/arithmetic.maxon
```

```powershell
.\check.ps1
```

runs every check (see "Tests"). The generators and the tools need Go 1.27; `go generate ./internal/...` runs every
generator. The scripts only call `cmd/gate`, which holds the gate's steps, the seeds' renewal, the regeneration of
`specs/` and of `stdlib/` and the build (`go run ./cmd/gate check|suites|seed|specs|stdlib|build`), so that every host
runs the same steps.

The commands that keep every core busy, `test`, `selftest`, `fuzz`, `build` and `check`, put the compiler's own process at
background priority first (`Process.enterBackgroundPriority()`), so a foreground job preempts them and the compilers and
executables they start inherit it; a lowered process still gets every idle core, so the runtime goes on using all of them.
`run` keeps the priority it was started with, and so does the program it starts. The gate does the same for its own process
(below-normal class on Windows, nice 10 on Linux and macOS), so everything it starts runs at background priority.

### Building on Linux

On x64 Linux the compiler is built and gated from `seed/minimaxon-x64-linux`, with Go 1.27 and a POSIX shell and nothing
from Windows:

```sh
./check.sh
```

is the gate (`go run ./cmd/gate check`), ending `all checks passed`, and `go run ./cmd/gate build` builds `./bin/minimaxon`.
`go run ./cmd/gate specs <maxon's specs directory>` regenerates `specs/` and writes the same files as on Windows: the
conversion's fixes are made on every target's branches of a test (see "Tests"). `go run ./cmd/gate stdlib <maxon's stdlib
directory>` regenerates `stdlib/` the same way. The gate runs every step there that a
Linux host can: the corpus natively for `x64-linux` and, when a `qemu-aarch64` is registered with binfmt_misc
(`qemu-user-binfmt` or `qemu-user-static`), for `arm64-linux`, which the test runner then starts as it starts the host's
own executables; and it says of each step it skips that it skips it and why: the `x64-windows` corpus, the debugger's batch
session and the fault line of an x64 Windows program (Windows only), the `arm64-linux` lanes without qemu, and the Mac lane,
which reaches the Mac through a share only the Windows machine mounts. The compiler reads the directory of temporary files
from `TMPDIR`, and uses `/tmp` when that is unset or empty, as maxon does (the gate's step "TMPDIR unset" holds it to that);
the build cache is under `MINIMAXON_CACHE` when that names a directory, and off otherwise. A Linux host never renews
the seeds, which needs the Windows seed run (see `seed/README.md`). The host's kernel is Linux 5.3 or later (WSL 2's is), which the corpus's runs of child processes need.

Go is the one that `go.mod` names: an older installed Go fetches it itself (`GOTOOLCHAIN=auto`, Go's default) from the
module proxy, `proxy.golang.org`, which a machine that cannot reach `go.dev` still reaches. A test that starts a program of
its system names the host's with `#if os(...)`, as maxon's spec tests do: its shell is `cmd.exe /c` on Windows and `sh -c`
elsewhere, so the same test runs on each host and none starts a Windows program on Linux.

On a Windows machine the Linux gate is checked in WSL as a native Linux host would run it: in a clone on WSL's own file
system (under `~`, not `/mnt/c`), with
WSL's interop with Windows off for the run, so that no Windows program can be started from Linux, and no Windows directory
on `PATH`:

```sh
echo 0 | sudo tee /proc/sys/fs/binfmt_misc/WSLInterop
PATH=$(printf %s "$PATH" | tr : '\n' | grep -v '^/mnt/' | paste -sd:) ./check.sh
echo 1 | sudo tee /proc/sys/fs/binfmt_misc/WSLInterop
```

With interop on, a Linux program that starts `cmd.exe` works under WSL and fails on any other Linux, which the gate would
then miss.

### Building on macOS

On an arm64 Mac the compiler is built and gated from `seed/minimaxon-arm64-macos` in the same way, with Go 1.27 and nothing
else: `./check.sh` is the gate, ending `all checks passed`, and `go run ./cmd/gate build` builds `./minimaxon`. The native
lanes run the `arm64-macos` corpus (with `--verify-ir`, as every arm64 corpus is compiled) and its reruns, so the Mac lane
that Windows runs through the share is not a step here. The gate says of each step it skips that it skips it and why: the
`x64-windows`, `x64-linux` and `arm64-linux` selftest, corpus and reruns, which macOS has no way to run, the debugger's batch session,
and the fault lines of those three targets' programs. `selftest` runs the tests that build and run a program on the host
with the arm64 test runtime (`arm64RuntimeUnit`); the machine IR's tests of hand-written x64 functions are linked into an
x64 Linux executable and not run. The compiler reads the directory of temporary files from `TMPDIR`, or `/tmp` when that is
unset or empty; a Mac never renews the seeds, but proves the macOS one when they are renewed (see `seed/README.md`).

### Continuous integration

`.github/workflows/ci.yml` runs on every push to `main`, `proven-access`, `cloud/**` and `eric-pc/**`, on every pull request and on demand, in four
jobs. `linux-x64` (`ubuntu-latest`) runs `./check.sh`, the whole x64 Linux gate, then has the compiler the gate built
(`bin/minimaxon`) build itself for `arm64-linux` and keeps that executable as an artifact. `linux-arm64` (`ubuntu-24.04-arm`,
GitHub's native arm64 Linux runner) starts when it has ended, and runs that compiler natively with
`go run ./cmd/gate suites bin/minimaxon-arm64-linux`: arm64 Linux has no seed, so `gate check`, `build` and `compiler` refuse there,
and `suites` runs the lanes in `armLinuxLanes` (`checks`, `native` and `native-behaviours`: the compiler's own suites, and the corpus and the
behaviours reruns for `arm64-linux` with `--verify-ir`), each step the same as on every other host, instead of under qemu. The
compiler finds `runtime/` and `stdlib/` by walking up from its executable, which sits in `bin/` of that job's checkout. `macos-arm64`
(`macos-latest`) runs `./check.sh`, the arm64 macOS gate. `windows-x64` (`windows-latest`, two cores and a runner whose WSL has no distribution) runs `go run ./cmd/gate check --host-only`: the
Windows gate on the lanes of x64 Windows alone. `--host-only` is a flag of the gate, not a second list of steps: the lanes of the other
targets' executables (`x64-linux`'s corpus and behaviours under WSL, `arm64-linux`'s under qemu and `arm64-macos`'s on the Mac) are left out, and the run
names every step it leaves out, as `corpus on x64-linux: skipped on this host, --host-only: ...`, where a Windows machine without WSL
fails those steps. The compiles for the other targets (`compiles for x64-linux` and the like) still run, and the job ends with the one check that
only a seed renewal makes otherwise: the compiler builds itself, and that build builds a third that is the same bytes. The runner's Defender has real-time
protection off, so nothing scans the executables the gate builds; its git converts line endings and the job turns that off first. The job takes
19 to 23 minutes on the runner's 2 cores, the spec tests nearly half of it. A job fails on the step that fails, with the gate's own output.

`.github/workflows/nightly-fuzz.yml` runs `fuzz --native` every night, and on demand over a range given as `start` and `count`, on
arm64 macOS and arm64 Linux. By default a night runs 20,000 seeds from the days since 2026-01-01 times 20,000, so no two nights
run the same seeds. Its `prepare` job (`macos-latest`) decides the range, which it prints in its log and summary, and builds the
arm64 Linux compiler as `linux-x64` does in `ci.yml`. Then `macos-arm64` builds its compiler from the seed and `linux-arm64` takes
that one, and each runs `bash cmd/fuzzshards/run.sh bin/minimaxon <start> <count> --native`. The script splits the range into one
`fuzz --seed=S --count=M --keep=DIR` process per core (`FUZZ_SHARDS`, `FUZZ_OUT`) and waits for all of them. It fails when one
does, and names the lowest failing seed and the command that reproduces it in the job's summary. The job keeps the failing
programs as an artifact. The workflow reads the repository and writes nothing to it, and it runs the same locally, as in
`FUZZ_SHARDS=2 bash cmd/fuzzshards/run.sh bin/minimaxon 1 100 --native`.

| Command | What it does |
|---|---|
| `minimaxon build <file\|dir> [-o out.exe] [--target=<arch>-<os>]` | compile; the path given is the project's root, and a directory is a project, one module per directory (see "Modules"); `--target` names the target, `x64-windows`, `x64-linux` or `arm64-linux` (see "Targets"), the system the compiler runs on by default |
| `minimaxon run <file\|dir>` | compile to the compiler's directory of the temporary directory (`minimaxon`, under `TEMP` on Windows and `TMPDIR`, or `/tmp` when that is unset or empty, elsewhere, where every executable the compiler and its tests build goes, so that a scanner can be told to leave that one directory alone), as `minimaxon-run-<process id>-<name>.exe`, and run; exits with the program's code |
| `minimaxon check [--tests] <file\|dir>` | diagnostics only, exit code 1 when there are any; `check runtime` checks the runtime's source and holds it to the functions the compiler calls (see "The runtime"); `--tests` checks every module's files of tests (`.maxtest`) with its source |
| `minimaxon test [--update-positions] [--behaviours] [--manual] [--slowest=N] [--repeat=N] [--worker-counts=N,N,...] [--env-runner=path] [--executables=N] [--target=<arch>-<os>] [dir\|file.maxon]` | run the corpus (default: the nearest `tests` directory from the executable upward; see "Tests"), or the one program at a path, compiled for the target and run through its runner; `--repeat=N` runs each program N times as the jobs `file#1`..`file#N` that share one compile, side by side in the runner's window |
| `minimaxon test <specs-dir\|file.md> [--verbose] [--interpret] [--require-passed\|--skip-passed] [--record-passed] [--executables=N]` | run maxon's spec tests (see "Tests") |
| `minimaxon test <module dir> [--compile-only] [--manual] [--target=<arch>-<os>]` | run the tests of the modules under the directory: the `test` declarations of their files of tests (`.maxtest`), compiled with the modules they name and the runtime into one executable whose entry calls each, which is then run (see "Tests"); `--compile-only` builds that executable for the target, through code generation, and runs nothing, and without it a target other than the host's is refused, as the executable runs only on the host |
| `minimaxon selftest [--compile-only] [--manual]` | run the compiler's own tests: `test self`, from the root of the repository; the files of tests named `*.manual.maxtest` are left out and named unless `--manual` |
| `minimaxon debug --symbolize <exe> <offset\|rip=0x<address>>...` | name the function, `file:line:column` and the inlined calls of code offsets (decimal or `0x` hex) of an executable that `build --debug-info` made, or of the address of a fault line, from the sidecar beside it, refusing one that describes another build (see "Debug info") |
| `minimaxon debug --dump-info <exe> [header\|files\|functions\|types\|lines\|inline\|layout]...` | print the sidecar's sections, every one when none is named |
| `minimaxon debug --batch --commands=<cmd;cmd;...\|@file> [--stop-timeout=<seconds>] <exe> [-- <args>...]` | debug an executable that `build --debug-info` made, on x64 Windows: launch it under the debugger, run the commands and write one JSON event per line, maxon's batch mode (see "Debug info") |
| `minimaxon debug [--stop-timeout=<seconds>] <exe> [-- <args>...]` | debug it in a conversation, maxon's: the commands are read from standard input after maxon's prompt, `(maxon debug) `, and each is answered in maxon's text (see "Debug info") |
| `minimaxon cache [trim\|clear]` | say where the build cache is and how many files and bytes it holds; `trim` deletes what a trim would (the files unused for five days) whether one is due or not, `clear` every file the cache wrote (see "Build cache") |
| `minimaxon rewrite --keyword=old=new <path>...` | rename a keyword by token in the `.maxon` files and the Markdown code under the paths (see "Changing the language") |
| `minimaxon rewrite --services <path>...` | write `service` for the `type` of each type that a `spawn` in the programs under the paths starts (see "Changing the language") |
| `minimaxon ircount <file.ir> (op=NAME\|call=SYMBOL)...` | development only: print how many lines of an `--emit-ir` file are the operation NAME or a call of SYMBOL, as `op=bounds 3`; the counting of the corpus's IR expectations (see "Tests") |
| `minimaxon qualify <dir>...` | write the module's path before each bare name of another module's declaration in the projects (see "Changing the language") |
| `minimaxon rename [--prelude=f,...] [--rules=file] <rule>... <program>...` | rename a built-in type or rewrite the calls of a built-in method by the declaration the checker resolved, never by text: `Array.append=push`, `Array.insert=insert({at}, value: {item})`, `Array.pop.unwrap=try {call} otherwise panic("empty")`, and a free function's whole call: `readFile().unwrap=try File.readText({path}) otherwise panic("unread")` (the rules are in `self/driver/rename.maxon`) |
| `minimaxon fmt [--check] <path>...` | put the `.maxon` files and the Maxon code blocks of the `.md` files under the paths in the one layout; `--check` changes nothing and exits 1 listing the files that are not in it (see "Layout" and "Changing the language") |
| `minimaxon interp <file\|dir> [--seed=N] [--frees] [--race]` | development only: run under the tracking interpreter, which runs a frozen value, a cown and a behaviour, and reports a violation of the memory model; `--frees` lists the values freed, in order; `--race` runs the race oracle, which the test runner and `fuzz` always run |
| `minimaxon fuzz [--seed=N] [--count=N] [--schedules=N] [--keep=DIR] [--native] [--proofs] [--concurrent] [--services] [--dump]` | development only: write random programs, check them and run them under the interpreter (`self/fuzz`), each staged in the library tier since it may write cowns and behaviours; `--native` also runs the executable of each that compiles and compares it with the interpreter; `--proofs` writes programs that aim at the range analysis's proofs instead; `--concurrent` programs of parallel loops, async lends and services, each under six schedules whose outputs must agree; `--services` runs the send graphs of up to four services under the interpreter's oracles of backpressure instead (see "Tests") |
| `go run ./cmd/convert [--specs] [--write] [--check] [--out=DIR] [<path>...]` | convert existing Maxon `.maxon` source files to minimaxon syntax and layout, marking unsupported constructs with `// TODO` comments and warnings on stderr; `--check` writes nothing and prints each kind of warning with its count and files, most frequent first (run `minimaxon qualify` after converting a project of several modules); `--specs` converts maxon's spec files, which is how `specs/` is made. A source file's top-level declaration of a name that minimaxon has built in (`internal/builtingen`'s table, which writes `cmd/convert/builtins_gen.go`) is left out, with the comment above it, since the built-in is the declaration every other module uses; a spec test's program keeps one, as a program may declare such a name. A source file's declaration of a record type that minimaxon builds in and that has members the built-in lacks (`Clock`, `Directory`) is kept as an extension of the built-in, with the members the built-in declares and the fields left out, as is a declaration of an enum that minimaxon builds in and that has members the built-in lacks (the five `File` errors' `failure()`, `FileInfo`'s `create`), with its cases and its `implements Error` left out, and maxon's `WaitDeadlineMs` is written `Count` unless the file declares it (the library's Subprocess does, for its own `MxWaitDeadline`). The types of maxon's Subprocess that the compiler builds in too are declared by the library under the prefix `Mx` (`MxSubprocess`), and a spec test that runs a child through `Subprocess` names them so (see "Where it differs from Maxon"). A method or a top-level function that calls a raw function the table lacks, or names a raw type that it lacks (`__ManagedFileError`), is left out, and so is what only it reached, a method that calls one left out included. A capability word before a type argument (`Array with immutable Key`) is left out (see "Where it differs from Maxon"). A block that is the first operand inside brackets (`let n = (try f() otherwise 'e' ... end 'e') as T`, `xs.push(match x 'm' ... end 'm')`) is bound on lines of its own before the line, since minimaxon reads no line break inside brackets. It lays out what it converts with the compiler's `fmt`: the gate's, `bin\minimaxon.exe` in the repository, or the compiler the environment variable `MINIMAXON` names A library file's cast of a raw buffer to the bare `__ManagedMemory` (maxon's `as isolated __ManagedMemory`) is left out, since the row's buffer is one already, and a field, parameter or local that it names `stream` is written `which`, so that the library's `stream` module can qualify what it declares. |
| `go run ./cmd/sample [-interval D] [-threads] [-top N] [-callers N] [-focus TEXT,...] [-o FILE] <exe> [arg...]` | profile an executable that `build --emit-map` made: runs it, samples its main thread's instruction pointer and its `rbp` frames (1 ms by default), and prints the functions by self and by total samples, with the callers of the top ones and of those whose symbol contains a `-focus` text, and the wall time; code outside the image (a Windows DLL) is charged to the function that called it, found by scanning the stack, and exits with the program's code |
| `go run ./cmd/selfab [--base=REV] [--base-compiler=EXE] [--runs=N] [--full] [--note=TEXT]` | compare the self-build of two versions of the compiler: A (`--base`, a git revision, `HEAD` by default, checked out in a temporary worktree; or `--base-compiler`, a C1 made of its sources) and B (the working tree). Each is built as C1 by its own seed, copied with its C1 into a temporary directory (so the checkout is never touched) with a build cache of its own and built once to warm that cache; then N times (11 by default), a comment line no earlier run wrote is added to the end of one source file of the leaf module `driver` or of the low module `diag`, and the rebuild of `self/` is timed on both, interleaved with the order alternating. The tool prints, for each of the two modules, the medians, their ratio and the range of the ratio over the pairs of runs. `--full` times instead `<C1> build self` with the cache off (`MINIMAXON_CACHE=off`), after one run of each that is not counted. It also prints the share of a build that `--timings` gives the `bodies` and `verify` stages (the check of function bodies), and the count in the IR that `--emit-ir` writes (after the optimizer) of the `bounds` operations and of the calls of `rt.panicEmptySlot` (counted for both versions by the working tree's `ircount`), the run-time tests that a compiler made by each version still holds; the failures that a `try xs.get(i)` takes to its handler are not among them. `--note`, with `--full`, also prints a row for `docs/self-compile-times.md`. It takes the machine-wide lock and works on Windows, Linux and macOS |

`build` and `test <module dir>` keep each module's code in the build cache and link the code of a module whose key did not
change from it (see "Build cache"); `MINIMAXON_CACHE=off` compiles without it, and `--verify-cache` (development only) compiles every module
and stops with an internal error at the first whose code or summary differs from the entry the cache holds under its key. The
compiler has no `cache` command and no `--jobs=` or `--with=`: it says so, prints its usage and exits 2, as it does for a command it does not know, and for an argument that starts with `-` and is no flag of the command (`firstUnknownFlag`, one table of each command's flags; a lone `-` and what follows `--` are not flags). `run` and the test
runner (the corpus and the specs) build their executables in the temporary directory under names that hold the process id
(and, for the runners, the test's number in the run), so two tests and two compilers running at once never write the same
file. A runner deletes each test's
executable when it can, tries again for the others when the run ends, and says in one line on standard error how many it could
not, which never changes the exit code.

`specs/` holds maxon's spec tests converted by `cmd/convert --specs`, from the maxon specs whose content `specs/SOURCE` pins
(a git tree hash; another source is refused unless `--advance-source` moves the pin, `specs/README.md`), in maxon's own format: `<!-- test: name -->` and the
fenced blocks ```` ```maxon ````, ```` ```exitcode ````, ```` ```stdout ````, ```` ```stderr ```` and ```` ```maxoncstderr ````. The tool
rewrites only each test's `maxon` block (every `// --- file:` section of it) through the source conversion, and copies
the rest, with line endings made LF. A test whose conversion warned, or whose source minimaxon cannot parse, carries one
`<!-- skip: kind -->` line per kind of warning after its test marker; the runner skips and counts it, and every other test
runs. `--check` over the specs, and the summary at the end of a conversion, give the tests converted cleanly and skipped and
the kinds by tests blocked, most blocking first. Then `fix specs` applies the fixes that minimaxon's language differences
name (a `try`, a cast, `.clone()`, `consumes`, `match consumes`, `mutates`, `mutates self`, `var`, `borrowed` on a result that a requirement lends, the bound `where T implements Copyable` or `Droppable` that a generic body needs, written in the function's header, the module written before a bare name that means one declaration of another module, as `qualify` writes it) to every test that runs a program; a test
that expects maxon's run-time trap where the fix adds a `try` gets a `<!-- skip: language difference: ... -->` line
instead, and so does a test that expects a compile error which minimaxon reports first under another code for a known reason
(the pairs of `self/driver/skips.tsv`, generated into `codeSkipTable`: ownership refused by another rule, the syntax error, operand
types, a method that waits where it meets a requirement), a test that expects a code whose rule minimaxon changed, whatever it reports (the table's `expect` rows: `E3220`, since a
constant may call a literal factory), and a test that runs a program which minimaxon refuses, after the fixes, by a rule it
changed (its `run` rows: `E3925`, a closure that borrows does not escape), and a test that the table names by its file and
name (its `test` rows), whose program minimaxon runs to another answer by a language change (a float division by 0 is
IEEE's). A test that expects a compile error and is first refused under another code by a fixable language difference
(arithmetic that needs a `try`, an integer cast, a clone, `mutates`, `var`, a bare name of another module) gets the fixes of the errors that are not the
expected one, round by round, until the first error is the expected one, and a `<!-- positions: moved -->` line, since the
fixes move text; if that is never reached, it is skipped when the code left after the fixes and the expected one are a
pair of the table, or when the first code left after the fixes is one of the table's `first` rows (`E3008`: a name
that Go's module rules keep from another module is refused before what maxon reports), and otherwise left as it was; a program that checks clean after the fixes is never skipped: it gets the fixes and a `<!-- positions: moved -->` line, and the test fails for the diagnostic that minimaxon lacks. A test whose first diagnostic left after the fixes has a cause that the table's `cause` rows name, and is not the
code it expects, is skipped with that row's reason: a program that changes the collection its `for` walks (a loop holds
the collection it walks), or a handler that binds a payload of `SendError` or `SubprocessError`. `fix` over spec files fixes
each test as a behaviour, on every core, as the runner runs them (`self/driver/fixrun.maxon`); each check adds the test to a
copy of the library checked once for its target, and the results are taken in in the order of the tests, so the files written
and the lines printed are those of one test after another. `specs.ps1` runs both and then the new tests (see "Tests"); `specs/README.md` has the command, and the directory is
never edited by hand, except that the runner writes `specs/passed.txt` (see "Tests").

`--debug-info` (on `build`) writes the executable's debug info beside it, as `<out>.mxdbg`, and leaves the executable byte for byte the one a build without it makes; `--no-debug-info`, maxon's spelling, writes none, as a build does by default (see "Debug info").
`--emit-ir` writes the IR of every function beside the executable, as `<out>.ir`, in SSA form as instruction selection gets it, and `--emit-map` the map of its code,
as `<out>.map`: a line per function and routine the link kept, in address order, with its RVA and size in hexadecimal
and its symbol, which `cmd/sample` reads. A profile of `self/` is two commands:
`.\bin\minimaxon.exe build self -o bin\prof.exe --emit-map`, then `go run ./cmd/sample bin\prof.exe check self`.
`--verify-ir` checks the IR of every function with the IR verifier (see "The IR") as lowered and again in SSA form, before code is made, and holds
the register allocation of every function to its invariants (spilling's to `verifyPressure`, colouring's to `checkColouring`), and stops the compile
with an internal error naming the rules the first broken function breaks. `--timings` (on `build`, `run` and `check`) prints on standard error, once the stages have run, a table with one line for
each stage of the task graph: the wall time its tasks took, in milliseconds, and its share of the total, a `ranges` line for the part of the `verify` stage that the range analysis took, and the five modules whose
tasks took longest; a `build` prints it for the runtime's compile, which every build does first, and for the program's. A
`build` or `check` runs its tasks on every core, so the stages' times add up to the work the compile did, which is more than
its wall time. The clock is read twice per task and only when the flag is on.
Beneath the stages, a compile that optimized code splits the lower and inline stages by phase (`opt.OptPhase`): the lowering itself, then
each pass of the optimizer (SSA construction, jump threading, the folds, the expansion, inlining, the analyses the passes after
inlining share, rotation, LICM, CSE and copy elision) and the cache's summary, each by its share of the stage. A compile that
emitted code splits the emit stage by phase (`backend.EmitPhase`): selection, the blocks'
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
executable that imports kernel32, or `x64-linux` or `arm64-linux`, a static ELF executable (`ET_EXEC` at maxon's fixed load
address, 0x400000, with no interpreter, no dynamic section and no relocation, `EM_X86_64` or `EM_AARCH64`) that calls the kernel
itself; the one it makes for a program that runs a child process needs Linux 5.3 or later, whose `pidfd_open` the run watches the child's exit with (on a kernel without it the run stops with a message that says so, and never polls instead). A name of no target is refused
with the list of the targets and exit code 1, as maxon's is. The default is the system the compiler runs on (`hostTarget`, the
`#if`s of `self/driver/targets.maxon`: the architecture and the system the compiler was compiled for), and a `build` with no `-o` names its executable with the target's extension, `.exe` or
none. The target picks the branches of every `#if` of the compile, the runtime's among them (see "The runtime"), and is part of
the key of every module in the build cache. Code generation is one IR for every target, and on each architecture one
selection and one internal convention; what differs is stated in tables (`internal/targetgen`'s target model and systems,
`internal/abigen`'s imports and Linux numbers, the arm64 kernel's among them) and behind one choice per place. The target
model is generated (`self/ast/targets_gen.maxon`): an `ast.Target` is an architecture (`ast.TargetArch`: x64 and arm64) on a
system (`ast.TargetSystem`: Windows, Linux and macOS), each system with its format of executable (`imageFormatOf`: PE, ELF, Mach-O), the way its programs
reach it (`systemReach`: imports or system calls, each refused by the backend on a system that reaches it the other way),
the suffix of an executable's name, and `knownTargets`, every architecture on each system its table runs. The backend
matches on the architecture where its code is the processor's (the selection, the encoder, the entry stub) and on the
system's table where it is the system's: the entry stub (`entry.maxon`, `entry_linux.maxon`, `entry_arm64.maxon`,
`entry_macos.maxon`), the writer
of the executable (`buildImage`, `writePointers` and `writeImage`: `pe.maxon`, `elf.maxon` or `macho.maxon`), the host and
system call conventions (`x64HostArg(system, index:)`), and the read of a thread's context (`x64ThreadContext`, which
`x64.rules` reads; on arm64-linux `tpidr_el0` holds the context itself). The runtime's Linux source is one for both
architectures: the arm64 kernel's numbers and layouts that differ are `layout_gen.maxon`'s rows under `#if arch(arm64)`, and
the few calls that arm64's kernel lacks (`mkdir`, `unlink`, `rename`, `epoll_wait`) are made by the one relative to a
directory or with a signal mask in an `#if arch(arm64)` at their place. `minimaxon test --target=x64-linux` on Windows
compiles each test for Linux and runs its executable through WSL (`wsl.exe --exec` and the executable's path under `/mnt/`,
in the default distribution), as maxon's `targets.tsv` names a runner for each target, and `--target=arm64-linux` the same,
its static executables run by the `qemu-aarch64` that WSL has registered with binfmt_misc; the executables' count of workers
reaches WSL through `WSLENV=MAXON_MAX_PROCS`, which the caller sets, and `--verify-ir` has `test` compile every program with
it. A program that the target cannot run says so with `// unsupported-targets: x64-linux` (maxon's directive of a spec
test), and a run for that target prints it as `SKIP` and counts it as not supported; `os-io`, `subprocess`,
`file-permission` and `conditional-compilation` are Windows', `os-io-on-linux`, `subprocess-on-linux` and
`file-permission-on-linux` Linux's (which macOS runs too, its paths and programs being POSIX's), as
`children-started-at-once-each-read-their-own-output-on-posix` is the twin of the cmd.exe one that WSL runs too, and the two
programs that E5001 refuses for x64's register count are not arm64's, which has more registers.
The two programs that listen on IPv6's wildcard, `::`, are `unsupported-targets: x64-windows`, since Windows Firewall asks the
user to allow a program that listens on a non-loopback address, once for each executable path, and each test executable has a path
of its own: no gate run raises a prompt. Every other listener of the corpus, the benchmark and the spec tests binds a loopback
address. Each of the two says in its comment how it is run by hand on Windows.
The compiler cross-compiles itself: `build self --target=x64-linux` makes a Linux compiler, which checks and builds programs
under WSL, and builds itself there to the same bytes. On Linux a compiler finds `runtime/` as on Windows and joins paths
with `/` (`nativeSeparator` of `self/project`); its build cache is under `MINIMAXON_CACHE` only, since there is no
`LOCALAPPDATA`, and `run` and `test` still put their executables under `TEMP`.

`--target=arm64-macos` builds an arm64 macOS executable. Apple's system calls are not a stable interface, so every call of
the system is one of a libSystem function: the runtime's macOS source (`macos.maxon`, `macos_green.maxon`,
`macos_subprocess.maxon`, all of it in `#if os(macOS)`, beside the Linux files they mirror) calls them by
`__Raw.callImportN("name", ...)`, the C name, and the entry stub by `arm64CallImport`. `internal/abigen` lists the functions
a macOS executable may call (`macosImports`, each variadic one with the words it fixes: `open`'s mode goes on the stack, as
Apple's AAPCS64 passes every variadic argument) and macOS's numbers and layouts (`macos.go`: the flags, `struct stat`,
`struct dirent`, the errno values, `struct sigaction` of libSystem's wrapper, `stack_t`, `siginfo_t`, the `ucontext_t` whose
`uc_mcontext` points at the registers, `struct kevent`, the sizes of the pthread objects). A call is `adrp`/`ldr` of its
`__got` slot and `blr`; the link binds only the functions its live code calls, numbered in the order of `macosImports`
(`LinkImports`), as a PE binds only the kernel32 functions its live code calls, in the order of their names. A libSystem function that fails returns -1 with the error in errno,
which the runtime's `checked` turns into -errno, so the macOS source reads its calls as the Linux source reads system calls,
and an `int` result is sign-extended from its lower half (`intResult`), which AAPCS64 leaves the upper half of undefined.
dyld calls the entry, `LC_MAIN`, as C's main: the entry keeps the word before the arguments as where the process started
(the kernel lays out the count, the arguments and the environment as Linux's does), makes the `pthread_key_create` key
whose slot of a thread's TSD array (`tpidrro_el0`) holds its context, gives the main thread its context there, moves to a
stack of the reserve, gives each thread a `sigaltstack` and the fault handler (`SIGSEGV`, `SIGBUS`, `SIGFPE`) through
libSystem's `sigaction`, whose trampoline the handlers return through, and exits by `_exit`. A worker starts in
`rt.threadStart`, which `pthread_create` runs on a stack of the reserve (`pthread_attr_setstacksize`); a parked thread
waits on a mutex and a condition of its own (macOS 13, maxon's floor, has no futex); preemption is `SIGURG` through
`pthread_kill`, as on Linux; the event queue is a kqueue whose user events (`EVFILT_USER`) break its wait and signal each user
event's event entry, and whose read and write filters (`EV_CLEAR`) report a handle's; a run of a program is `posix_spawn` with `POSIX_SPAWN_CLOEXEC_DEFAULT`.

The Mac is not on the gate's machine, so `test --target=arm64-macos` takes `--runner=command,word,...`, which starts each
executable through a command in place of the target's runner: `cmd/batchrun` (plain Go, cross-built for darwin/arm64)
keeps each executable and a row of its run in a staging directory (`batchrun record <stage>`), runs every row on the Mac
at least twice with a deadline each (`batchrun run <stage>`, writing `out/<run>.stdout`, `.stderr` and `.status`, and
what macOS's log says of a run that did not exit), and gives each run's result back to a second run of the test runner
(`batchrun replay <stage>`), so that every comparison stays the runner's. A row is the digest of the executable's signed
bytes, the signature's size left out, since the signature names the file, and its worker count, which a deterministic
compile gives again. The gate's step `corpus on arm64-macos` (`cmd/gate`) does it through the folder that Windows and
the Mac both mount (`M:\agents\minimaxon\`, the Mac's `~/Dev/agents/minimaxon/`): it stages the corpus and its reruns
under one and two workers, and replays them when `out\gate\` holds the Mac's results of exactly those rows (their
stamp), failing on any failure; otherwise it leaves the batch at `in\gate\` with `cmd/batchrun/run.sh` for the Mac
session to run, and prints a skip line, as it does when the folder is not mounted.

The executable is written by `macho.maxon`, after maxon's `MachOWriter.maxon`: a PIE `MH_EXECUTE` for arm64 with 16 KB
pages, `__PAGEZERO` over the low 4 GB, `__TEXT` from the start of the file at 0x100000000 (the headers, `__text` and `__const`, the read-only data),
`__DATA` (`__data`, then `__got` on a page of its own) and `__LINKEDIT`, and 14 load commands: the four segments,
`LC_SYMTAB` and `LC_DYSYMTAB` (with no symbol), `LC_LOAD_DYLINKER`, `LC_MAIN`, `LC_UUID` (the first 16 bytes of the code's
SHA-256), `LC_LOAD_DYLIB` of libSystem, `LC_BUILD_VERSION` (macOS 13.0), `LC_DYLD_CHAINED_FIXUPS`, an empty
`LC_DYLD_EXPORTS_TRIE` and `LC_CODE_SIGNATURE`; maxon's `LC_SOURCE_VERSION` is left out, since a program here has no
version. `buildMachOImage` is given the Mach-O symbols to bind (`_exit`, the C name with the platform's underscore), one 8-byte slot of `__got` each, bound by name
through chained fixups (`DYLD_CHAINED_PTR_64_OFFSET`, `DYLD_CHAINED_IMPORT`), and `writePointers` makes each word of the
data that holds an address a chained rebase of the target's offset from the base, each page of `__DATA` a chain of its
own. `writeImage` signs a Mach-O image under its file's name, as ld64 does (`codesign.maxon`): an ad-hoc, linker-signed
CodeDirectory (version 0x20400, its executable segment `__TEXT`) of the SHA-256 of each 4 KB page of the file before the
signature. The SHA-256 (`sha256.maxon`) is maxon's `stdlib/Sha2.maxon` ported by hand rather than converted, since that file
reaches the processor's SHA instructions and its word readers through the library's raw surface, which `self/` may not
name. The tests of `self/backend` build an executable that calls `_exit(0)` and one that writes through a rebased pointer
and exits with a byte that another one points to, and read them back: the load commands, the segments, the fixup chains
from the page starts, and the signature's hashes.

### Tests

`.\check.ps1` on Windows and `./check.sh` on Linux and macOS are the gate, all `go run ./cmd/gate check`, whose step list
is the one every host runs; a step a host cannot run is skipped with a line that says so and why (see "Building on Linux"
and "Building on macOS"). In
order, it formats, `go fix`es and vets the Go of the generators and tools; runs the generators and fails if a generated
file (`*_gen.*`) changed; has the host's seed build `bin\minimaxon.exe` (`bin/minimaxon` on Linux and macOS); and then runs the
suites on it (`go run ./cmd/gate suites`), which run the Go tests of the generators and tools (`go test ./...`, with that compiler as
`cmd/convert`'s formatter) and, with the compiler: `selftest`, `check runtime` for each target, `check self --tests` (the compiler's own source and its files of tests under the rules it has just been given), `fmt --check self runtime tests`, the corpus (`test`, with no standard
input), the programs that schedule behaviours again under one, two and four workers (`test --behaviours --worker-counts=`, one compile for the
counts; `behaviourWorkers` in `cmd/gate` names them), the same corpus and the same reruns for the `x64-linux` target (`test --target=x64-linux`, the
executables run under WSL, to which `MAXON_MAX_PROCS` is passed by `WSLENV`; a machine with no WSL or no distribution fails these
steps with what it lacks, and never skips them), and again for the `arm64-linux` target (`test --target=arm64-linux --verify-ir`,
every program compiled with arm64's selection and allocation held to their invariants, and its static executable run under WSL by
the `qemu-aarch64` that WSL has registered with binfmt_misc), every program of `tests/bench/` compiled, and none run, with `--verify-ir` for each of the four targets (`test testsench --target=<target> --verify-ir --compile-only`), the compiler's own source
built, and not run, for `arm64-linux` and `arm64-macos` (`build self --target=<target> --verify-ir` with `MINIMAXON_CACHE=off`), the corpus compiled, and none run, for each target whose executables the host does not run (on the Mac `x64-windows`, `x64-linux` and `arm64-linux`; `test --compile-only --target=<target>`, with `--verify-ir` for arm64: a compiler panic fails it), the compiler's own tests built for each of those targets with `--verify-ir`, and none run (`selftest --compile-only --target=<target> --verify-ir`, about two seconds a target on the Mac: the host's `selftest` compiles them for the host alone, and x64's allocator once panicked on a file of tests that arm64 compiled), and every spec test compiled, and none run, for the one foreign target that `specsCompiledFor` in `cmd/gate` names for the host, the one no other lane runs them for (`x64-windows` on the Mac, none on Windows or Linux, since the Mac's gate and CI run `arm64-macos`'s spec tests natively on every push; `test specs --compile-only --target=<target>`: a spec test must compile as its blocks say wherever the host's compile of it does), the build cache (a project whose main inlines a leaf of the module it names, built cold, warm and after
an edit of the leaf that the order mispredicts, on every worker and on one, each executable held to the uncached build of the same
source, and `--verify-cache`), the debug info (programs built for each target and `self` for x64 Windows with and without `--debug-info`, each executable the same and every span verified, and a stack overflow's `rip=` symbolized to its line on x64 Windows, x64 Linux and arm64 Linux; see "Debug info"), every spec test (`test specs --interpret --require-passed`, which fails when a test that `specs/passed.txt` lists does not pass,
counts the new passes, and runs each test that passes under the interpreter too, for a proven access out of range alone), `fuzz` (300 generated programs, each one the checker accepts run clean under the interpreter), and `fuzz --proofs --native` (180 programs that aim at the range analysis's proofs, run under the interpreter and as executables), `fuzz --services --count=200` (200 send graphs of up to four services under the oracles of backpressure). It stops at the
first step that fails. That the compiler reproduces itself (the seed builds C1, C1 builds C2, C2 builds C3, and C3 is
byte-identical to C2) is checked where the seeds are renewed, `.\seed.ps1`, which then runs the same suites on C2 and
proves the Linux seed under WSL and the macOS seed on the Mac (`seed/README.md`).
`--host-only` (`check.ps1 --host-only`, CI's Windows job) is the gate of a host that cannot run the other targets' executables: it runs the lanes of the host's
own target (`ownLanes` in `cmd/gate`), names each step of the other lanes that it leaves out, compiles for the other targets as the hosts that do not run
them do, and on Windows ends with the proof that the compiler reproduces itself that a seed renewal makes.

`--focused` (`go run ./cmd/gate check --focused [--since=<rev>] [--maxon-specs=<dir>] [--maxon-stdlib=<dir>]`, or `check.ps1 --focused`,
`check.sh --focused`) is the gate of a landing, the one every machine and script calls: CI runs the full gate on main, and a landing runs on its rebased
tip the steps that what it changed calls for. It is no list of steps of its own: `focusedRules` in `cmd/gate/focused.go` gives each step of the gate's
lists a rule by its name, and a test holds every rule to a step that exists. Always: the compiler from the seed, `fmt --check self runtime tests stdlib`,
`check self --tests --verify-reads`, `check runtime` and `check stdlib` for the host's target, `selftest`, the host corpus, the spec step
(`test specs --require-passed`) and the proof that the compiler reproduces itself (C2 and C3 are the same bytes). By the paths changed since `--since` (the
merge base with `origin/main` unless given) and in the working tree: Go (`internal/`, `cmd/`, any `.go` file) adds gofmt, `go fix`, `go vet ./...` for
windows, linux and darwin and `go test ./...`; `internal/`, a generator's input outside it (`self/diag/codes.tsv`, `messages.tsv`,
`self/driver/skips.tsv`, `self/syntax/Maxon.g4`, `runtime/hashfold.maxon`, `runtime/textops.maxon`) or a `*_gen.*` file adds the generators,
which fail on a stale generated file; `runtime/`, `self/backend/`, `self/ir/`, `self/interp/` or a test program that uses the scheduler adds the behaviours under
1, 2 and 4 workers; `cmd/convert/`, `self/driver/skips*`, `stdlib/` or `specs/` adds the regeneration of `specs/` (`gate specs`, from maxon's specs
directory, which `--maxon-specs` or `MAXON_SPECS` names), except that `cmd/convert/builtins_gen.go`, which builtingen rewrites for any row of
the table of built-ins (a runtime-only `__Raw` one too), counts for neither it nor `--interpret`: the converter consults its three maps only
for a source file of maxon's library, never for a spec test (`TestSpecConversionNeverReadsTheBuiltinTables` holds it to that), and the plan
says so on the step's line; after a regeneration the spec step replays the regeneration's saved results with `--require-passed`
instead of running every test again (a listed test that stopped passing is `LOST` there, which the regeneration's record mode does not fail); `specs/`,
`stdlib/`, `cmd/convert/` (but not `builtins_gen.go`), `self/interp/` or `self/driver/` puts `--interpret` on the spec step; `stdlib/` or `cmd/convert/` (whose tables only the library's conversion reads) adds the check that the library regenerates from maxon's
(`--maxon-stdlib` or `MAXON_STDLIB`). The run prints the steps it chose and the paths that called for each, names the steps it leaves out in one line (the
other targets' lanes and compiles, fuzz, debug info and the cache: CI runs them), prints each step's time, ends with
`gate time: total X min; slowest: ...` and the time of every step, prints main's last finished CI run as a NOTE when it is red (a red main with a known owner
does not stop a landing), and exits 1 at the first step that fails.

The suites (`cmd/gate`) group their steps in lanes that share nothing but the compiler and the checkout: `checks` (the Go tests, `selftest`, `check runtime`
and `check stdlib`, `check self`, the layout and `fuzz`), `builds` (the benchmarks and `self` for arm64, the cache, the debug info), `specs`, the
corpus of `native`, `x64-linux`, `arm64-linux` and `mac` (each Linux lane first runs `selftest` with the compiler built for
its target, since the host's `selftest` runs the code of the host's target alone: Windows runs the `x64-linux` compiler's
under WSL, and the `arm64-linux` compiler's runs only on an arm64 host, CI's arm64 runner or the Mac's Linux VM, never
under qemu-user, where it ran 43 minutes without ending; the `mac` lane skips when the Mac's share does not answer within a
few seconds), and the reruns of the programs that schedule behaviours of the first three
(`*-behaviours`). Those reruns never run under qemu-user either: their programs claim timings (a ticker's grid, a
backpressured sender's deadline) that the emulator's slowdown breaks, which lost a test in three seed renewals in a row where
600 runs of each on arm64 passed, and a qemu lane is a cheap check only. So `arm64-linux-behaviours` runs where arm64 is
native, CI's arm64 runner and the Mac's Linux VM, and an x64 host keeps only `arm64-linux`'s corpus under qemu. The gate runs them one after another. `--concurrent` on `gate suites`, or `MINIMAXON_GATE_CONCURRENT=1` for the gate, runs
each lane as a process of its own, all at once (the behaviours lanes in a second phase, with two executables at a time each), so that the gate
takes as long as its longest lane of each phase (about 70 s here against about 190 s one after another) and a lane that fails ends the others. It
is not the default because a test with a deadline failed in it now and then when other work shared the machine: a program of timers run on one
worker (`async-a-cancelled-coroutine...`, when the behaviours reruns shared the machine with the other lanes: since moved to the second phase),
and a gate that fails now and then costs more than the minutes it saves. A program whose interpreter runs would each cost seconds says so
with `// schedules: N` (the two programs that wait for four children); that keeps the gate from spending a minute and a half on twenty schedules of them.

Every command of `cmd/gate` (and `cmd/selfab`, and `selftime.ps1` through `lock.ps1`) takes the machine-wide lock for its whole run: on Windows the named mutex
`Global\minimaxon-heavy`, on Linux a lock of `minimaxon-heavy.lock` in the directory of temporary files, so that one gate or build runs on the machine
at a time, in whichever checkout (two at once take every core each and fail the tests that have deadlines). A process started under the holder,
which inherits `MINIMAXON_HEAVY_OWNER`, does not wait for it; a lock that its holder died with is taken over; and a command that has to wait prints
`waiting for another gate/build on this machine`.

Every step prints its time when it ends, and the gate ends,
passed or failed, with its steps by time and the total. The spec step replays instead of running when it can: a full spec run
of the gate or of `gate specs` keeps the result of every test in `bin\spec-results.txt`
(`test specs --save-results=`) and, when it has ended and nothing it read changed while it ran, a stamp of everything the results
follow in `bin\spec-results.stamp`: the compiler executable's bytes, every file of `specs/` but `passed.txt`, of `runtime/` and of
`stdlib/`, `self/driver/skips.tsv`, the runner's flags and the environment its programs inherit (`MAXON_MAX_PROCS`,
`MAXON_HASH_SEED`, `MINIMAXON_CACHE`). The gate's `test specs --require-passed` whose stamp is the same takes the results from the
file (`--replay-results=`) and counts them against the current `specs/passed.txt`, so it fails exactly when a run would, and it
prints `replayed:` and the stamp's id. A missing stamp, any input that differs, a run that did not end, or a file that is not of
the same tests in the same order (the runner refuses it with code 3) runs every test. So the cycle of a merge, `gate specs` and
then the gate on the compiler it built, runs the spec tests once. `gate specs` and the gate pin `MAXON_HASH_SEED` the same way (`pinHashSeed`), since the stamp holds it. `check --record-specs=<maxon specs dir>` is that cycle in one command, the one a landing runs: its first suites step converts the specs from maxon's as `gate specs` does, and its spec step passes `--require-passed` and `--record-passed` together, so a single run (or the replay of one) fails when a listed test does not pass, drops a listed test that the conversion now skips (`DROPPED`), adds the new passes, and keeps a listed test that fails; the plain `check` leaves `specs/passed.txt` as it is. `gate specs` itself skips the conversion and the fix when the stamp of what they read (maxon's spec files, `cmd/convert`, the compiler, the runtime and library beside it, the skip table) and of what they wrote (`specs/`, but `passed.txt`) is the one of the last time they ended (`bin\specs-conversion.stamp`), and then only runs the tests, replayed when it can; any difference runs the conversion again. Neither file is under version control (`bin\` is ignored).

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
test the generators (each regenerates its outputs and fails if the checked-in ones differ; `internal/x64gen` and
`internal/arm64gen` prove their instruction tables against the bytes in their `testdata`) and `cmd/convert`.

Each `tests/**/*.maxon` is one program, except under `tests/modules/`, where each directory is a project and its files,
at any depth, carry the directives. The runner (`self/driver/test.maxon`) compiles every program to an executable, a program that
must fail included, because some errors come after type checking (lowering refuses what has no native code yet), in
the same process; the runtime is loaded, checked and compiled once per run and frozen, and each test is added to a copy of
that project and linked with its code, which every compile only reads (`SharedRuntime` in `self/driver/compile.maxon`, which a run of many spec tests extends with the standard library, see `minimaxon test <specs-dir>`; the
link lays the frozen units out between the program's own, `backend.LinkUnits`). The corpus and the spec tests run as
behaviours, on every core (`self/driver/runner.maxon`): each test is compiled on a cown of its own, one execute of the task
graph per test, and runs its executable there; the board, a cown, keeps a slot for each test's result, starts tests while
fewer than twice the workers are under way, lets at most a cap of executables run at once (half the workers, at least 1 and
at most 8, or `--executables=N`; a worker waits while the executable it started runs, so the cap keeps the others
compiling, and the machine's load bounded, since each executable may start a worker per processor), and takes the results in, in the
order of the tests, as soon as every one before them is in: the lines print as the run goes and are, with the summary, the
same at any number of workers (`MAXON_MAX_PROCS`, which the executables inherit). A program under `tests/check/` is only
compiled: one that expects no error must compile clean, and none is run. Any other program that compiles is run twice, as the executable and under the tracking interpreter
(see "The interpreter"), from the IR that the native build lowered, and both must give the stdout and the exit code the
directives say; the interpreter must also find no violation of the memory model, and the executable, built with
`--count-allocations`, must end with no heap box live (a program that traps skips that check). A program that schedules behaviours runs under 20 schedules of the interpreter
(seeds 0 to 19), and every one must give the same stdout and exit code and no violation, as must the executable, whose
workers schedule it as the machine does, so the directives of such a program must say an output that does not depend
on the schedule: print only from behaviours that the cowns put in order, such as one chain, or behaviours on one cown.
A program that schedules so many behaviours that 20 schedules of the interpreter would cost seconds says how many it
runs under (`// schedules: N`).

A program that runs the scheduler runs natively more than once, each run another schedule, and every run must give
what the directives say: the runner knows it does from the link, which gives the scheduler only to a program whose code
reaches it (see "Main with or without the scheduler"), a test of the runtime that runs it by hand among them. It runs 2 times in a
run of the corpus, under the worker count of the runner's environment, which every executable
it starts inherits (`MAXON_MAX_PROCS`, else the machine's processors). `--repeat=N` makes each program N jobs, `file#1` to
`file#N` in the RUN, PASS and FAIL lines, each of which runs the executable once: the first copy compiles, and the others queue a
run of its executable, so the one compile serves them all, and the runner's window of executables runs the copies side by side, as
N hand-made copies of the program would. It combines with a path (`test tests/runtime/x.maxon --repeat=50`), with a directory
and with `--behaviours` (then it repeats the runs of each program's behaviours, under each worker count). The summary adds
`N runs of each, F failed runs`, and a failed copy's message is listed as any failure's is. A program that says `// runs: alone`
is not run beside anything: its copies run one after another, so `--repeat` is there for repetition and not for load. The
25-copy batch of `tests/runtime/green-preempt-runtime.maxon` is what this was made for: it reproduced in minutes a failure of a
per-processor timer that the gates had seen once. `--compile-only` compiles every program and runs none, for any target (the gate holds `tests/bench/`, the corpus for the targets the host does not run and the spec tests for one of them, to the compiler with it). `--behaviours` runs only those programs, and
only natively, which is how a program meets other worker counts: the gate runs the corpus with the machine's count,
which holds every program to the interpreter too, and then `test --behaviours --worker-counts=1,2,4 --env-runner=bin/envrun`:
the programs are compiled once and each executable is run under each count in turn (twice each), with `cmd/envrun` giving the
executable its `MAXON_MAX_PROCS` for itself alone (and `WSLENV` naming it, for WSL) and ending it a second before the runner's
deadline, so the runner keeps its own width; so the gate runs each such program 8 times natively under four counts; it compiles only the
programs that have one of the words `when`, `async`, `sleep` and `Scheduler` in a file, or are tests of the runtime, and
leaves out the rest uncounted. A soak
is the same flags with `--repeat=300`, which runs 300 copies of each program under each count. A failure names the copy and the worker count. The stress
programs, of `tests/library/` since a program does not name cowns, are `behaviours-over-a-ring-of-cowns` and `behaviours-on-one-to-eight-cowns-and-pushers-handing-off` (1 to 8
cowns per `when` and per-sender order across hand-offs, after maxon's `cown-multi-torture` and
`cown-handoff-torture`); the others named `a-…` and `behaviours-…` each hold one path of the runtime to it (a function
value or a frozen value shared into behaviours on many workers, a cown's last owner dropped inside a behaviour while
others hold it, a generic function's nested `when`, behaviours scheduled from behaviours to a depth, `Process.setExitCode`
ordered by a cown).

A program under `tests/library/` is staged in the library tier, as maxon's spec runner stages a case marked `tier: stdlib`,
so it may go through the doors of maxon's raw surface (see "Modules"), the buffer cursor of "Borrowing values" among them;
it is run as any other program is, natively and under the interpreter.

A `.fix` file under `tests/fix/` is a case of `minimaxon fix`: the program, fixed, must check clean in at most two rounds and be the text
of the `.fixed` file beside it. A file of `// --- file: <path>` sections is a project; when its first line is `// tier: library`, its files
are the standard library's instead, each at its path below the library's directory, so that the first directories are modules of the library
and the fixes only a library module gets apply (the function `<module>Anchor` that names another module, which a module needs to call a member of that module's extension, is added once,
to the module's first file, whichever of its files call: `library-anchor-once`).

A program under `tests/ir/` holds the optimized IR of its functions to claims, so that a regression in generated code fails the
gate (`self/driver/irexpect.maxon`). It must compile clean and is never run. Its `// expect-ir:` directives each name a function by
the symbol the IR text gives it (`sum`, `Counter.bump`, `first[Count]`) and say one of `<function> has no <op>`, `<function> has some <op>`,
`<function> has <N> <op>`, `<function> calls <symbol>` or `<function> does not call <symbol>`, where `<op>` is an IR operation as
`--emit-ir` prints it after the optimizer (`bounds`, `length.array`, `add.u`; written without its dotted ending, as `add` or `cmp`, it
is every operation that begins so) and `<symbol>` a function of the program or a runtime entry (`rt.array.elem`). The runner reads the text
of the IR that the program's own compile made, in memory (`Options.keepsIrText`, `ir.moduleIrText`), with no file written, and always
for the host's target, so that no result depends on the target: a run for another target leaves these programs out as not yet supported.
A directive that names a function that the IR does not have, an operation that the printer has no name for (`ir.printedOpNames`,
generated from the IR table), or a symbol that is neither a function of the program nor a runtime entry (`ir.rtTexts`) fails the test,
as does a program with no directive, or a directive outside `tests/ir/`. A directive reads only the names of operations and symbols, never a
register, a block, the order of the lines or their layout, so that a change in how code is generated that keeps what a test is about does not
fail it: a test says `has no`, `has some`, `calls` or `does not call` where it can, and an exact count only where the number is the property,
which a comment in the test says. The gate needs no step of its own, since `test` runs every program under `tests/`. `minimaxon ircount <file.ir>
(op=NAME|call=SYMBOL)...` counts operations and calls in an `--emit-ir` file by the same code, which `cmd/selfab` uses for its counts of `bounds` and of calls of
`rt.panicEmptySlot`. Run alone (`test tests/ir`) the directory is the corpus root, so its programs are not those of `tests/ir/` and fail as misplaced:
run the whole corpus.

A program under `tests/runtime/` is a test of the runtime: it is
compiled as one more file of the runtime's module (`compileRuntimeTest`), so it may call the runtime's functions and the
`__Raw` intrinsics, and holds `main`; it is run natively only, since the interpreter does not run the runtime's source.
`tests/runtime/scheduler.maxon` tests the scheduler alone: it builds behaviour records by hand. A test executable runs under a 10 second deadline, and a program
that outlives it fails the test with `timed out`. A run under the interpreter (each of a corpus program's schedules, and a spec test's under `--interpret`) has the same 10 seconds, of the wall clock, which `self/interp` reads every 65536 instructions (a step count would not follow the machine's load or the cost of an instruction, and the step limit, 500 million, would take 90 seconds): a run past it fails as `timed out (interpreted)`, and a spec test that does so says `native` in `self/driver/skips.tsv`, where a program of the corpus that does so is shrunk or says `// runs: manual`. The summary is `N passed, M failed, K not yet supported`, the
failures are listed, and the exit code is 0 when nothing failed. With `--verbose` a `RUN` line comes when a test starts, so a crash of the compiler, which ends the run, names the tests that were under way; those lines fall between the others as the timing does.

`minimaxon test <specs-dir|file.md> [--verbose] [--skip-passed] [--record-passed] [--env-runner=<cmd/envrun>]` runs maxon's spec tests of `specs/` (`self/driver/spec.maxon`; the format is that of maxon's `docs/SPECS.md`, see `specs/README.md`) in the same process, as behaviours, as the corpus runs. A test is compiled from memory (a multi-file test as a project of files held in memory, `project.loadFilesInto`) and run once, from an executable of its own in the temporary directory. A compile-error test compares the errors one for one by code and place, after the fragment's path is made `<fragment>` (and, for a multi-file test, its lines the merged block's): the messages, the notes and the lines that continue a message are left out (see "Where it differs from Maxon"), an error that maxon reports at no place matches its code at any place, and the line and column are compared only when the test has no `<!-- positions: moved -->` line; a run test compares the exit code when a block gives one and always stdout and stderr, trimmed (a block that is not there asserts the stream is empty), as maxon's runner does. It prints one line for each failed test (`FAIL file: test: what differed`), the counts, and the skipped tests by reason, the most first; `--verbose` also prints a line for each test (a `RUN` line when it starts and its result in order, so a crash names the tests under way) and every reason. A test is skipped, with a reason it counts, when its source has a `<!-- skip: kind -->` line (the conversion's), when a directive needs what the runner or the compiler lacks (a `tier` other than `stdlib`, `runs: alone`, `network`, `stdin`, `Args`, `env`, the trace and debug directives, `unsupported-targets` naming `x64-windows`, and `procs` and `preempt` in a run not given `--env-runner=<cmd/envrun>`), when a result block is a trace or is for another target, when its source stages an overlay of maxon's stdlib or runtime, `specs/passed.txt` lists the tests that have passed (one `file.md: test name` per line, sorted). `--require-passed` runs every test and fails the run when a listed one does not pass, printing `LOST file.md: test: what` for it (a failure, or `skipped: reason`) in place of its `FAIL` line, the count of them at the end, and `NEW file.md: test` for a test that passes and is not listed. `--skip-passed` leaves out, without compiling them, the listed tests (skipped under the reason `passed before (specs/passed.txt)`), runs the others, and prints `NEW` lines too; it cannot be given with `--require-passed`. A missing list is an error for both. `--record-passed` writes the list, and starts it when there is none: the tests that passed in the run, and the listed tests that the run left out, failed or skipped for a reason of the runner, so a regression stays listed and goes on failing the gate; a listed test that the conversion now skips (a `<!-- skip: -->` line, which a language change or the conversion put there) is dropped, with a `DROPPED file.md: test: reason` line. The gate runs `--require-passed`, and `gate specs` ends with a full run that records. Every program runs in `.spec-run-<pid>` beside the specs directory, the checkout's root, as maxon's runner runs every test (`Subprocess.runInDirectory`), and so does its run under `--interpret`, which reads each relative path the program names against that directory (`self/interp/os.maxon`, `anchored`; the interpreter has no working directory of its own to change), so that a test that writes a file by a relative path leaves nothing in the checkout: a test's relative paths are written against that depth (`../stdlib` is the checkout's library), so nothing depends on the runner's working directory or what lies beside the checkout; the runner makes it empty at the start of the run and removes it at the end. A test that a directive or a result block keeps off the host's target (`unsupported on <target>`, `a result block for the target <target>`) is skipped for the host: a listed one was recorded on another system, and is kept in the list rather than lost. `--save-results=file` writes what became of each test (a header, a line for each test in the run's order and a trailer that counts them, so that a file of a run that did not end is refused), and `--replay-results=file` takes the results from such a file instead of compiling and running the tests, then counts and prints them as a run does, so that `--require-passed` and `--record-passed` apply to them as to a run; both are for a run of every test (not with `--skip-passed`) and the gate decides when a file may be replayed (see "Tests"). A spec test that a `native` row of `self/driver/skips.tsv` names (`native`, the spec file, the test's name or `*` for every test of the file, the reason, apart by tabs) gets a `<!-- native: reason -->` line after its marker from the conversion, never by hand: it is for the tests of a compute-heavy library (bignum arithmetic, curves, key schedules), which the interpreter runs for longer than a test's 10-second deadline, and it runs natively only, so `--interpret` does not run it under the interpreter too and the run says nothing of it. `test specs` alone reads and writes nothing. The compiled program inherits the runner's standard input, and its environment but for what a directive sets for it alone through `--env-runner` (`cmd/envrun`), as maxon's runner does: `procs: N` sets `MAXON_MAX_PROCS=N` and `preempt: off` sets `MAXON_PREEMPT=off`; the gate always gives it. `process: own` needs nothing, since every test's program is a process of its own. A test marked `<!-- tier: stdlib -->` is compiled with its modules in the library tier (`ast.Module.stagedInLibrary`), as maxon's runner compiles it as a file of `stdlib/`: it may name maxon's raw surface (see "Modules"). **The library is compiled once for a run of many tests.** A run that names at least `sharedLibraryFromTests` (100) tests has the standard library compiled once, after the runtime (`SharedRuntime.prepare`, `prepareLibrary`): its modules are added to the runtime's project, checked and compiled to their units (and, under `--interpret`, their IR as lowered is kept for the interpreter), and the project and the units are frozen. Each test is added to a copy of that project (`Options.sharesLibrary`) and linked with those units, so no test adds, parses, resolves, declares or checks a library module, which is nearly all of what a compile of a spec test did. What a library module's stages read is the library's source, the runtime, the target and `--count-allocations` (which the build cache's key of a module holds too, `cacheSetupOf`), and never a program, so what they make is one value for every program of the run: there is no entry to key, and no test can read a stale or foreign one. A test whose compile cannot use that project is compiled as before, on the run's own build cache (`compileSpecSource`): one with `<!-- DebugInfo -->`, whose library would need the spans it keeps, one that stages an overlay of the stdlib or runtime (the runner skips those, and `compileSpecSource` looks at the block again), and every test of a run that verifies the IR (`--verify-ir`), which is of every module of its compile. A library that does not compile leaves the run with none shared, and a test that names the broken module meets its error as it would without it.

`tests/bench/` holds the benchmarks: a counted loop, nested loops over an array, string building, records and unions, and a
recursive function, each deterministic and ending in the checksum it prints (its `//>` line). The corpus runner leaves
the directory out, since the programs run long enough to time. Each times its work with `Clock.nowNanos` and prints it last,
as a `work ns` line. `.\bench.ps1 [-Compiler bin\minimaxon.exe] [-Target x64-windows|x64-linux] [-Runs 7]` builds each with
the compiler and prints a table of the executable's code size (`.text` bytes, read from the PE section header, or the ELF
code segment, by `textsize.ps1`), the best time of its work and of its process over the runs, each with the spread of the
runs, and reports a program whose output is not its checksum; it is how a change to the code generator is measured. The
process is started with `CreateProcessW`, its output going to a file, and for `x64-linux` from a copy in WSL's `/tmp` by a
shell there, so neither PowerShell nor the start of WSL is in the time. `.\selftime.ps1` records the `.text` bytes of the compiler it built in `docs/self-compile-times.md`.

Each program also prints the time of its phases as `<phase> ns <nanoseconds>` lines before its `work ns` line, and has a Go counterpart in `tests/bench/go/<name>/main.go` that does the same work and prints the same lines. `cmd/bench` (`go run ./cmd/bench`) builds both for any target into a batch that a script runs, on arm64 hardware too, and reports minimaxon against Go as Markdown tables (`bench report --cpu=<name>` names the CPU of a results file that has none, as a Linux VM on a Mac writes it); `docs/benchmarks.md` has how to run it and the latest tables.

Directives in comments say what the program must do:

| Directive | Meaning |
|---|---|
| `//> text` | one expected line of stdout, in order (none means stdout must be empty) |
| `// expect-exit: N` | expected exit code (default 0; a panic exits 1) |
| `// expect-error: E3102` | compiling must fail with this code; `E3102 at 7:5` also pins the line and column. A program may have several, and each must be reported |
| `// expect-free: label` | the interpreter must free the program's values in the order of these lines, one value each, as `minimaxon interp --frees` prints them (`string "a1"`, `array`, `record`, `slot map`) |
| `// memory-limit: N` | the native run may use at most N megabytes; the runner does not enforce it yet |
| `// native-only` | do not run the program under the interpreter; for a program whose point is what the executable does with memory, which the interpreter runs far too slowly, one that does I/O the interpreter does not model (files, the environment, children, a debuggee), and one that checks a run-time abort of the executable that the interpreter reports as a violation (a second take of an emptied promise slot) |
| `// runs: alone` | the program's executable runs with the machine to itself, as a spec test of maxon's with `runs: alone` does: the runner starts it once every other test has ended (no executable and no compile of another test goes on beside it), and each program that says so after the one before it, so that the run drains once for all of them and not once for each. For a program that holds every processor, as the preemption tests of `tests/runtime/` do: beside it a program whose run is a chain of short waits on the system, such as `green-events-children.maxon`, which starts a hundred children one after another, waits a scheduling slice at each and outlives its deadline |
| `// runs: manual` | a test that is run by hand: it runs for longer than the gate may spend on one test (over 2 seconds a run on a quiet machine, on any target, which `--slowest=N` finds: it ends a run of the corpus or of the spec tests with the N tests that passed and took longest, the milliseconds of a run of each one's executable, the runs it had, and the interpreter's milliseconds, so that each machine finds its own list by measuring), so no run of a directory runs it unless it is given `--manual`, and the gate gives none. The run names what it left out in its summary, `1477 passed, 0 failed, 16 not yet supported, 4 manual, left out: runtime/a.maxon, ...`, so that a test that is never run is never one that nobody knows is not. A run of its own path (`minimaxon test tests/runtime/a.maxon`) runs it, in the corpus that holds it, and so does `test --compile-only`, which runs nothing. Run them when a change touches what they test (the subprocess rows, the process tree, the runtime's timers) |
| `// stdin: <text>` | a line of the program's standard input, which ends after the last: each directive is one more line, its text after the one space as `//>`'s is. A program without one reads the end of its input at once. The runner gives each executable its input through `__Builtins.runWithInput`, and the interpreter reads the same text |
| `// schedules: N` | run the program under N schedules of the interpreter (seeds 0 to N - 1) instead of 20; for a program of thousands of behaviours |
| `// expect-scheduler: used` or `none` | the link must give the program the scheduler, or must not (see "Main with or without the scheduler"); what a program does cannot show it |

**Manual tests.** The gate has no test that runs for long: a test that takes over 2 seconds a run on a quiet machine, on any target, is
a manual test, which the default runs and the gate leave out, and which is run by name or with `--manual` where a change touches what it tests.
One mechanism marks them for every kind of test. A program of the corpus says `// runs: manual` (table above). A file of a module's tests
(`.maxtest`) is named `<name>.manual.maxtest`, and `selftest` and `test <module dir>` leave it out unless they are given `--manual`; the
summary says `ok: 176 tests of <dir>, 1 manual files, left out: <file>`. A spec test of maxon's is named by a `manual` row of
`self/driver/skips.tsv` (`manual`, the spec file, the test's name, the reason, apart by tabs), which the conversion writes into the spec file as a
`<!-- manual: reason -->` line after the test's marker, never by hand: the test is fixed as any, a run of the spec files leaves it out unless it
is given `--manual`, and the summary names it (`9436 passed, 0 failed, 5906 skipped, 2 manual, left out: sha2.md: sha2.a-million-letters, ...`).
A manual spec test that `specs/passed.txt` lists is left out, listed and not lost: `--require-passed` does not count it among the tests that did
not pass, and `--record-passed` keeps it in the list. A run that leaves a manual test out never leaves it out silently, so a test that nothing runs
is always one that the output names. `test --compile-only` and `check self --tests` still compile every manual test, which costs no run. Run the
manual tests when a change touches what they test; the manual tests of the timers' latency (a ticker's ticks on their grid, a late handler's
skipped ticks) are the ones to run on Windows, and on every target, whenever the event wait or the timed wait of the runtime is touched, since the
tests that stay in the gate hold only what does not depend on how late a wake comes.

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
  `export.X` (`export` is a name only as the start of a qualified name), and `stdlib.X` is a built-in. A value cannot qualify a type, so a
  module's name before a type of the module names the module where a local, a parameter or a field of that name is in scope,
  in a type and in a static member of the type (`url.URL.parse(url)` with a parameter `url String`); where that name's own
  type has a member of the type's name too, the name is refused as ambiguous (`E3964`).
- **A bare name** is a local, a field, a type parameter, the module's own declaration, a built-in or, last, a public
  declaration of any module of the standard library (below), and nothing else, so adding a declaration to one module never changes
  how another resolves, and resolving a module reads only the declarations of the modules it names. A bare name that only
  another module declares is unknown (`E3004` for a call, `E3011` for a type, `E2004` for a value), and the message names
  the qualified spellings; where the visibility of each such declaration keeps it from the module, it is `E3008` at the
  name instead, as maxon refuses a declaration it may not name, except for a module-level `let` or `var`, which stays
  unknown (`E2004`) as in maxon.
- **The standard library** is Maxon source in `stdlib/`, found as `runtime/` is (the `stdlib` directory of the nearest
  directory, from the compiler's executable's upward, that has one). It is laid out by what a programmer uses, which maxon's
  `stdlib/` is not, and its root holds no file (a `.maxon` file there is `E3928`, which names the module to move it to), so
  `stdlib/` lists only modules: `array` (the methods of `Array` and its sorts), `string`, `character`, `characterset`, `map`,
  `set`, `list`, `range`, `parsable`, `json`, `url`, `http` (`HttpClient` and `HttpServer`), `tls`, `filepath`, `stream`, `console`, `math`, `random`, `base64`, `pem`, `clock`,
  `directory`, `process`, `subprocess`, `cell`, `slotmap`, `unicode`, `ascii`, `sha2`, `heap`, `services` (the overload policies, minimaxon's own), the `core` and `_helpers`. The `core` holds the
  interfaces, the conformances that the built-in types need to meet them and the hashing: a module sees an `extension
  <built-in> implements I` only when the module that writes it declares `I` (see "Extensions"), so each conformance of
  `Array`, `String`, `Character`, `FilePath`, the integers and the floats is written in the module that declares the
  interfaces, and what a type has beside them is in its own module (the core also holds the two iterators that those
  conformances return, `ArrayIterator` and `StringIterator`, and `FilePath`'s `comparisonSpelling`, which
  its `equals` and `hash` read). The `core` is the one module that every program reaches, and every other module of the
  library depends on it (`resolve.noteCoreDependency`), but for `_helpers`, which is the library's own: the helpers that the
  core names (`slotScan`, `ByteOrder`), whose every declaration is `export`, so no program names one. The names of the modules
  that the compiler itself looks for are constants in one place (`ast.libraryCoreModule`, `libraryMapModule`,
  `libraryRangeModule`, `libraryHelpersModule`), and `ast.libraryModuleNamed` finds each: the core holds the interfaces that a
  payload-free enum is a key by, the `map` the type that a map literal makes and the `range` those that a range value makes.
  The library is converted from maxon's `stdlib/` by `cmd/convert` but for the files that are minimaxon's own (`Map.maxon`,
  `Set.maxon`, `Stream.maxon`, `Console.maxon`, `Character.maxon`, `StringSplit.maxon`, `StringTrim.maxon`, `Parsable.maxon`,
  `IntText.maxon`, `FloatText.maxon`, `BoolText.maxon`, `CharacterText.maxon`, `StringIterator.maxon`,
  `InitableFromArrayLiteral.maxon`, `Overload.maxon` and the generated `unicodeCategory_gen.maxon`; see "Where it differs from Maxon"), which no
  conversion touches; maxon's `StringBuilder.maxon` is left out, since an interpolation builds a String here.
  `stdlib/files.txt` lists the files converted so far, each with the library module it goes to (its directory under
  `stdlib/`) and, where two files of one module would share a name, the name it takes (`StringHash.maxon`); `stdlib/split.txt`
  lists the declarations of a converted file that go to another module than the file's (`cmd/convert --split`: the
  conformance half of `Array.maxon`, `String.maxon` and `FilePath.maxon` is the core's, the methods stay), by the text its
  first line starts with and, to move only some members of an extension, their names, so a split is a rule that a
  regeneration repeats, and one that no longer fits its file fails. `.\stdlib.ps1 <maxon's stdlib directory>` (`go run
  ./cmd/gate stdlib`, on every host) regenerates them, then applies the compiler's `fix` to them as `specs.ps1` does to the
  spec tests (a `let` that is changed becomes a `var`, a name of another library module is qualified, and a call of a member
  that an extension of another library module adds makes the caller name that module, by a function that has a parameter of one of its types);
  none is edited here, and a declaration of a name the compiler builds in (`BytePos`) is left out. `gate stdlib --check
  <maxon's stdlib directory>` fails when a regeneration differs from what is committed and puts the files back; the gate runs
  it as the step "library regenerates" when `MAXON_STDLIB` names that directory, and skips it otherwise, since it cannot
  assume where maxon is checked out. `Aes` and `AesGcm` take maxon's hardware backing (`AesHardware.maxon`,
  `AesGcmHardware.maxon`, on runtime/aes.maxon) where `__Builtins.cpuHasAes()` says the processor has the instructions, and
  `AesBitsliced` otherwise and in the interpreter; `Sha2`'s SHA-256 compresses its blocks by `__Builtins.sha256Blocks`
  (runtime/sha.maxon) where `__Builtins.cpuHasSha()` says so, and by its software rounds otherwise. An identity comparison of two bare names in the library (AEAD's `aeadSharesOutput`: `nonce is output`) is `false`,
  and `is not` is `true`, since a call cannot borrow an array it writes as another of its arguments here. For the same reason a call of maxon's `limbCarryChain` or `limbBorrowChain` that passes the workspace it writes as an operand too (`limbCarryChain(ws, dstBase: d, a: ws, aBase: x, b: ws, bBase: y, ...)`) calls a form of the function that the conversion adds beside it for those operands (`limbCarryChainSharingAB(ws, dstBase: d, aBase: x, bBase: y, ...)`), which reads the written array where the operand was read, as maxon's shared array does, with no copy of the workspace (`cmd/convert`, `sharedarguments.go`). maxon's `CombCache` keeps the curves' fixed-base tables and Montgomery moduli in its program-wide registry, built at the first use; here they are constants of the module `curves` (`stdlib/curves/CombTables.maxon`, about 118 KB of limbs as data: 24 KB for P-256, 54 KB for P-384, 40 KB for ed25519), which the library's regeneration computes once by running maxon's own build code over the converted library (`cmd/gate`, `combtables.go`), and each `CombCache.table(curve, build: ...)` or `CombCache.moduli` is a call of `combTable(curve)` or `combModuli(curve)`. The ciphers of
  maxon's `helpers/crypto/` (`Aes`, `ChaCha20`, `Poly1305`, `Aead` and their errors) stay public, as programs name them. An overlay (`cmd/convert/overlays/<path in maxon's stdlib>`, written in maxon's language) is minimaxon's own version of some declarations of one of maxon's files, which the conversion puts in place of maxon's before it converts the file: a declaration of the overlay replaces maxon's of the same name, a new one is added at the end, the names its first line lists after `// overlay drops:` are taken out, and a name that does not match fails the conversion. `Poly1305` has one: its `mac` keeps the accumulator and the key in limbs of 64 bits with `__Builtins.mulHigh`, as Go's `sum_generic.go` does, where maxon's five limbs of 26 bits hold 16 words at once in the loop, more than x64 has registers for (`E5001`). Maxon's driftsort grows its merge scratch with `resize`, which no tier may do to an array of a type parameter
  here, and then writes it as a prefix from slot 0 before it reads it; the conversion makes the resize a `reserve`, and a
  function that stores to the scratch by index empties it first (`truncate(0)`) and pushes each element instead. The same holds of an array that a program resizes and then fills from 0 in a loop over a counter, as some of maxon's
  spec tests of the library tier do: the resize is a `truncate(0)` and a `reserve`, and each store a push. It leaves out a conditional region that declares only names the compiler builds in (maxon's `ExitCode`,
  whose range each system's branch sets), and a method that calls a raw function the table lacks, with the constants and
  private methods that only such methods reach. The modules are layers, as a program's are: maxon's library names any of its declarations from any file, so
  the layout puts files that name each other in one module and each other module above the ones it names. Every program is
  compiled with it (`project.addLibrary`): its directories are modules after the program's, flagged `isLibrary`, the root's
  path being `stdlib` and each one under it `stdlib.<path>`, so no program module shares one, and `Project.library` names the
  root. A program names the public declarations of every library module bare (`Unicode.isWhitespace(32)`), after its own
  names and the built-ins, so the layering is the library's own, unless another module of the program declares the name
  where the module sees it: then, as in maxon, the name is the program's, written qualified (`lib.CivilMonth`), so a name the
  library adds never captures one the program declares; a public name is declared by one library module only (`E3006` at the
  later one). A library module names the library's other modules qualified from the library's root (`_helpers.X`, `core.X`),
  as any module names another, and its `export` reaches the tree rooted at its parent within the library, which for a
  top-level module is the whole library and never a program. A program sees nothing of the library short of `public`. A
  module is the unit that a program reaches and pays for whole (its check and its lower), and an extension or a conformance is
  seen only from the modules that name the one that declares it, so a library module that calls a member of another module's
  extension names that module. Each directory of maxon's `stdlib/helpers/` is a module of the library by the conversion
  (`files.txt` maps each file to it) and the conversion makes every `public` of a helper file `export`, so a helper is the
  whole library's and no program's, and so is a `public` member of another library file whose parameters or typealias target
  name a helper's type or such a member (its result may name one). Where maxon's unmarked declaration is visible to the
  library's tree, here it is the declaring module's, so the conversion makes an unmarked declaration `export` when a file
  of another module of `files.txt`, or a file of maxon's library that is not converted yet, writes its name, within the
  directory maxon makes it visible to (`cmd/convert --listing`, `visibility.go`), together with the types of the file that
  its signature names (`E3167`); it reads names as words, so a word that such a file writes for something else exports a
  declaration that nothing names, which reaches no program. A program the spec runner stages in the library is
  compiled as a file of the core, so it sees the library's `export` declarations too, bare. Only the library modules a program
  names, directly or through others, are compiled past their resolve (`reachedModules`), so a program pays for no other, and
  resolves only those it reaches: in a program's compile the library's resolves wait for the program's, and a library module
  resolves only when a module of the program names it or names a library module whose source may name it, directly or
  through others (`resolvesModule`). A library module that the program does not reach is not parsed either, with the build
  cache or without it: what reaches a module is read from the text of its files by a scan of their lines that lexes and parses
  nothing (`project.scanModule`, `libscan.maxon`; the layout says what a line declares, since a declaration starts a line, its
  members are the lines one tab deeper and a branch of a `#if` is one tab deeper than its directives). The scan is a stage of
  each library module (`scan`), which waits for nothing and so runs beside the program's parses, and it gives the module the
  words that reach it: its top-level names, which is how a program names a type, a function or a constant of it, and the
  exported members of its extensions of a type that the module does not declare (a built-in type, or a type of another
  module), which a program calls without naming the module. The members of a type that the module declares are no words: a
  program that has a value of the type got it from a module that names this one, or wrote the type's name. The lines of every
  branch of a `#if` count, whatever the target. The scan also gives the modules its source may name (each module whose
  qualifier's first name it writes before a dot and a name, comments included, and the core, a superset of what its resolve
  records), and keeps the text it read for the module's parse. The
  library's names index (`libindex.maxon`) is a cache of these scans, kept under the digest of the compiler and the path, size and time of last write of every file of
  the library (a stat, not a read; the index holds the file system's time when its compile began and is taken only when every file was last written two seconds before it, since a file system's time has a coarse resolution): a compile that finds it takes each module's words and names from it in place of reading the files, and one that
  scanned them all writes it. A program's parse gives the names its code writes, its interpolations' included and its comments
  and the text of its strings not (`ast.ParsedModule.nameUses`, from the tokens it parsed), and the module's words of the
  library are read from those (`programWords`, with the name of the type that a map literal or a range value makes), for which
  its parse waits on the scans. A library module's parse
  waits for the program's parses and for the scans of every library module, and runs only when a word the program's code
  writes reaches it, closed over what those name (`reachedLibrary`, the one rule of reach, the same whether the scans read the
  files or the index), the core being reached always (its parse waits for its own scan only), a superset of what the resolves
  name (a known over-reach: a word is of a module whatever type the program writes it for, so `FilePath`'s member words `join`,
  `parent`, `isEmpty`, `empty`, `resolve`, `startsWith` and `separator`
  still reach the `filepath` module in a program that uses no `FilePath`). A library file that nothing reaches is no error
  of a build, even with a syntax error in it, since the scan reads the first words of its declaration lines only; a file the
  scan cannot read is reached always, so that its parse reports it. Each parsed module is a module of the build cache like
  any other, with its own key, and is compiled for the compile's target. A project that is the runtime's source or `stdlib/`
  itself is compiled without a second library, and the compiler's
  own source, `self/` (found as `runtime/` is, `project.isCompilerDirectory`), without any: it names nothing of the library, so
  its build never reads `stdlib/` and a library file may use syntax that the compiler building `self/` (the seed) does not
  parse once that compiler has this rule; a seed from before it still parses every library module when it builds `self/`. A
  compile of `stdlib/` compiles every module of it, which is how the gate checks the library.
- **The raw surface is the library tiers'.** Maxon's library is written on a raw surface that reads and writes memory past the
  ownership model's bookkeeping: the static functions of `__Builtins`, the type `__ManagedMemory` and its methods, and an
  array's `.managed` (see "Primitives"). Only a module of the runtime or of the library, a program the spec runner stages
  in the library, or the compiler's own source, may go through one of these doors (the compiler reads its standard input with
  `__Builtins.readStdin` and runs a test's executable with `__Builtins.runWithInput`, what maxon's `Configuration` with an
  `InputSource.bytes` runs, until maxon's Subprocess converts); anywhere else each is `E3185` at the door, in maxon's words, with
  what a program uses instead (`Process.id()` for `__Builtins.currentProcessId()`, the `Array` methods for `.managed`),
  as a table of `internal/builtingen` says. A refused type stops the declaration that names it, so its body reports nothing
  more of it. `__Raw` is narrower still: the runtime's alone (`E3152`). A raw buffer has an element type here, so the
  library's `__ManagedMemory.create(capacity, elementSize:)`, maxon's empty buffer with room for `capacity`, takes it from
  the call, a typealias of the instance or where the result goes, where maxon's buffer has none and is laid out by
  `elementSize`; an `elementSize` other than the bytes an element takes in the buffer (1 for a byte, a word for a word, a
  String's two words) fails `__ManagedMemoryError.invalidAllocation`, and it may be given by its place, as maxon's library
  writes `create(most, 1)`. A static is called through a typealias of an instance, so `cmd/convert` writes maxon's bare
  buffer in a file over `__ManagedFile`, whose rows read and write bytes, as `__ManagedMemory with Byte`, and its create of
  the element size 1 through `ByteMemory`, the alias the library declares beside `__ManagedFile`.
- **Visibility has three levels**, for a declaration, a field or a method:
  - no modifier: the declaring module only;
  - `export`: the tree rooted at the declaring module's parent, as Go's `internal/`. An `export` in `app/net/http` is
    visible to `app/net` and every module under it (`app/net/tcp` too), not to `app` or `app/ui`. In a top-level
    module or the root module that tree is the whole project. It never reaches past the project;
  - `public`: everyone, programs that use the project as a library included.

  Naming a declaration from a module its visibility does not reach is `E3008` (`E3014` for a field), and the message
  says which modifier would reach.
- **A signature may not name a less visible type, except in a result** (`E3167`): each type that the parameters,
  `throws` clause, `where` clause or `implements` clause of an `export` or `public` declaration name, the type of a
  field (at the narrower of the field's and its type's visibility), a payload (at its union's), a typealias's target
  (the result of a function type too) and the type of an `export` or `public` constant, whether written or inferred
  from its value (`export let BIG = Status.big`), must be at least as visible. A method is held to its own visibility, a requirement of
  an interface to the interface's. The result of a function, a method or a requirement is not held to it, as a function
  of Go may return a type that is not exported, and neither is a type nested in the result (`returns Array with
  Hidden`, `returns (Hidden, Count)`, `returns borrowed Hidden`, `Promise with Hidden`). A module that the type's
  visibility does not reach holds such a value like any other: it binds it by inference, passes it on to a generic
  function or to a function whose bound it meets, and uses the members of the type that are visible to it (a member that
  is not is `E3008` or `E3014`). It cannot write the type's name (`E3008`), so no declaration of its own can name it in
  a signature or a field.
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
| `table` | `StringMap` and `IntMap` (open addressing, their values in a `__SlotArray`) and `Option`, which nothing built in provides; `arrange` (`arrange.maxon`), which puts a list in a new order by swaps; `Digest`, `Hasher`, `ByteWriter` and `ByteReader` (`digest.maxon`), which the build cache names its entries by and stores them with, and `CacheWriter` and `CacheReader` (`cache.maxon`), which the generated functions store a module's checked state with | — |
| `mxdbg` | the `.mxdbg` sidecar's format, maxon's (`format.maxon`), its writer (`MxdbgWriter`) and its reader (`MxdbgReader`), which the backend writes and the `debug` command reads (see "Debug info") | `table` |
| `ast` | the program model: `Tree`, a module's nodes as flat `Record`s, the generated node kinds, accessors and declaration structs, `Module` and `Project`, the project's `TypeTable` and `FuncTable` of per-module slots and the accessors every read of an entry goes through (`typeEntry`, `funcEntry`), the `ModuleState` a module's task writes, which holds its module's slots (`state.maxon`), the built-in types and functions (generated), `Resolution` and `Typing`, the per-module tables the phases fill in (see "The AST"), link names, locating an error, `I128` | `diag`, `table` |
| `syntax` | the grammar (`Maxon.g4`), the generated tokens and parser, the lexer loop, the layout rules, the formatter (the parser in a mode that writes the layout instead of checking it) and the edits that rename a keyword | `ast`, `diag`, `table` |
| `project` | loading a project: discovery, finding the runtime's source (see "The runtime") and the standard library's (see "Modules"), reading and parsing each module, the declaration tables, the module graph and its import-cycle check | `ast`, `diag`, `syntax`, `table` |
| `resolve` | name resolution, into the module's `Resolution`: which declaration each name and type reference names, which type parameter each constraint binds, which modules the module depends on, visibility | `ast`, `diag`, `table` |
| `check` | the stages of the type checker (`stages.maxon`), the generic bounds, the range analysis that decides what arithmetic can fail, the ownership pass, and what a behaviour may capture, into the module's `Typing` | `ast`, `diag`, `table` |
| `ir` | the IR (fixed-size records, with the printer and the visitors generated from the IR table), the graph algorithms (reverse postorder, dominators, natural loops) that its flow and the machine IR share, what its arithmetic computes (`evaluate_gen.maxon`, generated from the IR table: `evalBin`, `evalCmp`, `evalGuard` and the rest, one per operation that computes a word from its operands, which the interpreter and the fold pass both call; `evaluate.maxon` has the algorithms the table names: the 64-bit wraps, the wide product, the shifts, the overflow tests and the float conversions), its builder, finding the generic instances a module needs, lowering (the primitives' operations, where each value is dropped, the body of each behaviour), which reads the `Typing` of every module it follows a call, a constant or a field's default into, and the runtime's symbol names | `ast`, `diag`, `table` |
| `opt` | the optimizer: leaf inlining (`Leaves`, `Inliner`, `self/opt/inline.maxon`), its analyses (`Analysis`: over `ir.Flow`, the dominator tree, dominance frontiers, loop nesting depth and use counts per register), the rewrite rules' matcher generated from `internal/rulegen/generic.rules` (`rewriteInstr`, reading the definitions of `ir.Defs`) and the helpers the rules call; SSA construction (`Ssa`, `self/opt/ssa.maxon`: the slots nothing takes the address of become registers and block parameters) and the fold pass (`Fold`, `self/opt/fold.maxon`), which `optimizeModule` (`self/opt/optimizer.maxon`) runs in that order on every function a native build compiles; after leaf inlining, loop rotation (`Rotator`, `rotate.maxon`), loop-invariant code motion (`Licm`, `licm.maxon`) and common subexpression elimination (`Cse`, `cse.maxon`), over what may be merged or moved of each operation (`motionOf`, `motion.maxon`), the loops' facts (`LoopFacts`, `loops.maxon`) and the edits they share (`Editor`, `edit.maxon`) | `ast`, `ir`, `table` |
| `backend` | the line table of the code it writes and the sidecar of an executable (`lines.maxon`, `sidecar.maxon`; see "Debug info"), the x64 and arm64 encoders (generated), the assembler and code unit (`unit.maxon`: labels, fixups of a kind, sections; x64's instructions in `asm_x64.maxon`, arm64's and the patching of their fields in `asm_arm64.maxon`), instruction selection (`isel.maxon`: the module's functions into machine IR by the rules of each instruction set, `internal/rulegen/x64.rules` generated into `lower_x64_gen.maxon` and `arm64.rules` into `lower_arm64_gen.maxon`, the helpers every architecture shares, and the primitives of the machine they call, which `isel_x64.maxon` writes for x64 and `isel_arm64.maxon` for arm64, each with its half of the calls and the helpers only its rules name; `rules.maxon`), the machine IR (`mir.maxon`: forms by `FormID`, conditions by `MCond`), what the allocator asks of every architecture and form (`machine.maxon`, over `machine_gen.maxon`), x64's register file and conventions (`regs_x64.maxon`, over the target table's `regs_x64_gen.maxon`) and arm64's (`regs_arm64.maxon`, over `regs_arm64_gen.maxon`), liveness, register pressure and spilling with the E5001 test (`liveness.maxon`, `pressure.maxon`, `spill.maxon`), block parameters in memory (`params_memory.maxon`), colouring, SSA destruction and the colouring's checker (`regalloc.maxon`, `ssa_destruction.maxon`, `colour_check.maxon`) and each architecture's encoder of the machine IR (`encode_x64.maxon`, `encode_arm64.maxon`, which writes maxon's arm64 frame), the drop and copy routine of each type (`routines.maxon`, written in the instructions of `routine_asm.maxon`, which each architecture's assembler writes, naming its registers by their roles), the entry stub of each system (`entry.maxon`, `entry_linux.maxon`, and arm64's `entry_arm64.maxon`) with the green threads' code (`green.maxon`, `green_arm64.maxon`) and the two ways it starts main (`start.maxon`), the linker, which decides between them by what the program reaches, and the writers of the executable's formats behind one image (`image.maxon`: the PE writer `pe.maxon`, the ELF writer `elf.maxon`, of either machine, and the Mach-O writer `macho.maxon`, with the ad-hoc code signature `codesign.maxon` and the SHA-256 it hashes with, `sha256.maxon`) | `ast`, `diag`, `ir`, `mxdbg`, `table` |
| `interp` | the tracking interpreter: runs the IR of a whole program with the primitives written in Maxon and a simulated scheduler of behaviours, and reports a violation of the memory model | `ast`, `ir`, `table` |
| `fuzz` | the random program generator the `fuzz` command runs | `table` |
| `debugger` | the debugger, maxon's (`maxon-bin/Debug/`): the Windows host over the debug port (`host_windows.maxon`), the engine that plants traps, runs, steps and walks lines (`engine.maxon`), where the sidecar places code and the frames of a stopped thread (`symbols.maxon`), values (`values.maxon`), the commands (`commands.maxon`), the answers and their JSON events (`replies.maxon`) and the batch session (`batch.maxon`) | `mxdbg`, `table` |
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
| `internal/grammargen` | `self/syntax/Maxon.g4` | `self/syntax/parser_gen.maxon`, `self/syntax/tokens_gen.maxon`, `self/ast/nodes_gen.maxon`, and the keywords `cmd/convert` reserves, with those an operand may be (`cmd/convert/reserved_gen.go`) |
| `internal/builtingen` | `internal/builtingen/table.go` | `self/ast/builtins_gen.maxon`, `self/ast/builtins_record_gen.maxon` (the `Builtins` record, its `empty()` and `isCapability`: a field for each type row, function handle and primitive of the table, and the few fields `extraFields` lists), `self/ir/runtime_gen.maxon`, `self/ir/intrinsics_gen.maxon`, `self/check/builtins_gen.maxon`, `self/interp/builtins_gen.maxon`, and the top-level names of the built-ins, which `cmd/convert` leaves out of a converted library file (`cmd/convert/builtins_gen.go`) |
| `internal/diaggen` | `self/diag/codes.tsv`, `self/diag/messages.tsv`, `self/driver/skips.tsv` (the code pairs and the tests `fix specs` skips; it names codes of the first) | `self/diag/codes_gen.maxon`, `self/diag/messages_gen.maxon`, `self/driver/skips_gen.maxon` |
| `internal/irgen` | `internal/irgen/irtable/table.go` and `semantics.go` | `self/ir/ir_gen.maxon`, `self/ir/evaluate_gen.maxon` |
| `internal/x64gen` | `internal/x64gen/x64table/table.go` | `self/backend/x64_gen.maxon`: the encoders, `X64Register`, `X64Form` and `x64FormID` (the form's FormID), the `choose` functions and `encodeMachineForm` |
| `internal/arm64gen` | `internal/arm64gen/arm64table/table.go` | `self/backend/arm64_gen.maxon`: the encoders (`encodeArm64<Form>`), `Arm64Register`, `Arm64Form` and `arm64FormID`, `arm64LogicalImmediate` and `arm64IsLogicalImmediate`, the `arm64<Form>Holds` predicates of the offsets, `encodeArm64MovImm` and `encodeArm64MachineForm` |
| `internal/targetgen` | `internal/targetgen/target/target.go`: each processor's register file (names, encodings, classes), reserved registers and the internal calling convention, and the conventions of each system it runs (a `System`: its host convention, and the system call convention of one whose programs call the kernel themselves): argument registers in order, result, error flag, a system call's number, callee-saved and caller-saved, checked to split the register file, the thread's read of its context on each system; and each operating system (`OS`: its format of executable, its reach, its executable suffix), which `ast.TargetSystem` has a case for whether or not a target runs it; arm64 (maxon's register roles and internal convention, AAPCS64 hosts, Apple's passing every variadic argument on the stack) on Linux and on macOS, a system the table holds pending (`System.Pending`: its declarations are written and `knownTargets` leaves it out), and the pending targets (`target.Pending`, none now), whose declarations it writes and which no architecture of the target model names until instruction selection is written for them (their instruction sets are `machines.Pending`, which a test fits into the machine IR's tables beside the others); `x64table` and `arm64table` take their registers from it, and with the instruction sets of `internal/machines` (`internal/isa`) it writes the machine IR's tables | `self/ast/targets_gen.maxon` (`TargetArch`, `TargetSystem`, `ImageFormat`, `SystemReach`, what each system is asked, `knownTargets`); `self/backend/machine_gen.maxon` (`MCond`, the constraints of every form by FormID, each architecture's register file and the forms the target-neutral passes write: `jumpForm`, `exclusiveOrForm`, `classMoveForm`); `self/backend/regs_x64_gen.maxon` (`x64Reserved`, `x64InternalArg`, `x64InternalResult`, `x64InternalError`, `x64InternalCalleeSaved`, the `x64WindowsHost...` ones for Win64, the `x64LinuxHost...` ones for System V and the `x64LinuxSyscall...` ones for a Linux system call, `x64Host...(system)` and `x64Syscall...(system)` by the system compiled for, `x64ThreadContext`, `x64FirstVector`, `x64RegisterFileSize`, `x64StackAlignment`, and `...VariadicOnStack` of each convention); `self/backend/regs_arm64_gen.maxon`, the same of arm64 |
| `internal/rulegen` | `internal/rulegen/generic.rules`, `internal/rulegen/pressure.rules` and the selection rules of each instruction set of `internal/machines` (`x64.rules`, `arm64.rules`), checked against the IR table and a selection rule against its instruction set through `internal/isa` | `self/opt/rewrite_gen.maxon`, `self/backend/lower_x64_gen.maxon`, `self/backend/lower_arm64_gen.maxon` |
| `internal/abigen` | `internal/abigen/table.go`, `internal/abigen/linux.go` (the Linux x64 kernel's system call numbers, flags and record layouts that the runtime's Linux source and the Linux entry stub pass, and where the arm64 kernel's differ, its own: the runtime's file has them under `#if arch(arm64)`, and the backend's by their names after `arm64`), `internal/abigen/imports.go` (the library a PE imports from, and the libSystem functions a macOS executable may call, with the words each variadic one fixes; a test checks that every libSystem name the runtime and the entry stub call is listed. A PE needs no list: its import table holds the DLL functions its live code calls, which the link collects from the calls' names) | `runtime/layout_gen.maxon`, `self/ir/layout_gen.maxon`, `self/backend/layout_gen.maxon`, `self/backend/imports_gen.maxon` (`rtDll`, `macosImports` and the variadic ones) |
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
after it decides. A syntax error names what the grammar allows at that point, such as `Expected newline but got ')'`; where it
allows an expression and finds none, the error is maxon's `E2004`, `Expected expression but got ')'`.
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
for every row, so a row added to the table fails the build wherever its body is missing. Three columns say what a call needs
and does to a count, per intrinsic (the rows of one intrinsic that say it agree): `Requires` (argument 0 below the count, or
the receiver not empty), `Count` (kept, grown, shrunk, by one or by an unknown amount; a method that mutates its receiver
and says nothing changes it), and `Measure` (a method that reads the count, or whether it is 0). `Proven` on a row names the
row that does the same where the call has proved `Requires`, with no failure exit: `Array` and `FixedArray` each have one for
`get` and `set`, which share an intrinsic each, `Array` one for `first`, `last`, `pop` and `remove`, and `ast.provenTwin` finds
it. The generator holds a twin to its row; a twin may say its row's `shrink` exactly, `shrinkOne`, since the row's failure
exit changes nothing and the twin has none.

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
| `setsFlags`, `readsFlags`, `readsMemory`, `writesMemory` | what instruction selection asks of a form; columns of the machine IR's tables (`machine_gen.maxon`) of the same names |
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
emits together (`SetccRax`). The generated file holds the register enum (`X64Register`); a condition is the machine IR's `MCond`, whose cases are in the order of x64's condition codes, which the generator checks,
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
patches, or a form marked commutative that is not two-address with two uses. The table gives them to the other generators as
an instruction set (`x64table.Table`, an `isa.ISA`): its forms' constraints, the shapes a selection rule names, and the forms
the target-neutral passes write (its jump, its trap, its system call, the exclusive or of each class and the moves between
the classes). `internal/targetgen` writes from every instruction set of `internal/machines` one table per column indexed by
`FormID` (`machine_gen.maxon`: `machineOperandCount`, `definesOperand`, `reusesOperand`, `commutesOperands` (add, and,
or, xor and imul, whose uses the allocator may swap), `isCall`, `isReturn`, `hasPatchField`, `isJump`, `isTrap`,
`isKernelCall`, `isExclusiveOr`, the effects, `vectorOperands` and the masks `fixedUses`, `fixedDefs` and `clobbers`, a
`RegMask` over the register file: the general registers, then xmm0 to xmm15 from `x64FirstVector`), each instruction set's
forms in a range of FormIDs of its own (x64's from 0), so that no question of a form names its architecture. x64gen
writes `x64FormID`, `x64RegisterOf` from a number, a `choose` function per choice that names the form an immediate selects
(`chooseAddRegImm`), and `encodeMachineForm`, which encodes an x64 form, by its FormID, from the fields of a machine
instruction.

**The arm64 encoder** is generated the same way from `internal/arm64gen/arm64table/table.go`, whose opcodes are maxon's
(`Arm64Backend.maxon`'s `Arm64Op` words). Every form is one 32-bit word: its `opcode` with each operand's field added at its
bit, a register (whose 31 is `sp` where the operand names the stack, `stack`, and the zero register elsewhere), an unsigned
field given as its value (`Arm64Imm12`, `Arm64Imm16`), an offset in bytes that its field holds in units of the access
(scaled `imm12`, unscaled `simm9`, a pair's `imm7`), a logical immediate given as the word (`arm64LogicalImmediate`, maxon's
algorithm), a condition (the machine IR's `MCond` mapped to arm64's codes: `below` is `lo`, `parity`, the unordered of a
float comparison, is `vs`; `cset`'s field holds the opposite), and the fields a reference patches: a branch's distance in
words (`imm19`, `imm26`), adr's and adrp's split `immhi:immlo`, and the low 12 bits of an address after an adrp. The forms
are the integer, logical, move-wide, bitfield and conditional-select instructions, loads and stores of 8 to 64 bits and of
d registers at a scaled, an unscaled and a register offset, adrp and the page offsets, the branches, the scalar double
operations and conversions, `ldar`, `stlr`, `ldaxr`, `stlxr`, `casal` (of x0, as x64's `cmpxchg` is of rax), `ldaddal` and
`swpal`, the barriers, `brk`, `svc`, and the reads and writes of `tpidr_el0`, `tpidrro_el0` and `nzcv`. A direct form (the
pairs `ldp` and `stp` at an offset, pre-index and post-index, `madd` and `msub` of four registers) is one only hand-written
code writes: it has an encoder and is no case of `Arm64Form`. `encodeArm64MovImm` (and its steps, `arm64LadderInverted`, `arm64LadderSkips`, `arm64LadderFirst`) sets a register to any word in the fewest
`movz` or `movn` and `movk`; it is the encoder of `MovRegWord`, the one form of more than one word, which the machine IR holds
as one instruction that makes its result from its immediate alone, so the allocator writes it again before a use rather than
spill it. arm64's forms are the FormIDs from 256. `go test ./internal/arm64gen/...` encodes every form at
the registers 0, 1, 30 and `sp`, the limits of each field and every condition with a reference encoder (whose logical
immediates are found by trying every element, run and rotation) and compares the words with
`internal/arm64gen/arm64table/testdata/assembler.golden`, which the Go toolchain's arm64 assembler wrote
(`ARM64_CAPTURE_GOLDEN=1 go test -run TestCaptureGolden` writes it again); the words no assembler syntax gives (adrp, adr, a
write of `sp`) are written from the Arm manual. `selftest` runs the generated Maxon on a set of them (`self/backend/arm64.maxtest`).

The IR table and the instruction tables are Go packages of their own (`internal/irgen/irtable`, `internal/x64gen/x64table`,
`internal/arm64gen/arm64table`), so that `rulegen` reads the same tables as the generators of the records and the encoders.

**The rewrite rules** are written in a small language after the Go compiler's `_gen/*.rules`, one file per target:
`internal/rulegen/generic.rules` rewrites IR to IR, and an instruction selection file (`internal/rulegen/x64.rules`) rewrites IR
to x64 forms. A rule is a pattern, a condition after `&&` if it has one, and after `=>` what the instruction is rewritten to;
a line that starts with `&&` or `=>` goes on with the rule before it, and `//` starts a comment:

```
(bin.add x (const [0]))                                   => x
(bin.mul.!checked x (const [c])) && isPowerOfTwo(c)       => (bin.shl x (const [log2(c)]))
(bin.div.!unsigned.checked x (const [-1]) [fail])         => (neg x [fail])
(loadField obj [off])                                     => (LoadRegBaseDisp r obj [off])     // x64.rules
(bin.add.unsigned.checked x y [f])                        => (AddRegReg x y) (JccRel32 [MCond.below] [f])
(branch c [yes] [no])                                     => (TestRegReg c c) (JccRel32 [MCond.equal] [no]) (JmpRel32 [yes])
(bin.mul.unsigned.checked x y [f])                        => (Copy rax x) (MulReg y) (Copy r rax) (JccRel32 [MCond.below] [f])
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

It writes, for each instruction set of `internal/machines`, `self/backend/lower_<isa>_gen.maxon` from `<isa>.rules`
(`lower_x64_gen.maxon` from `x64.rules`), every name of it after the instruction set's, so that two share the module:
`x64SelectInstr(sel, fn, ins)` and `x64SelectTerm(sel, fn, ins)`, which the selector calls for the architecture compiled
for, dispatch on the opcode to `x64Select<Op>`, which tries the operation's rules in the order of the file and ends in a panic
that the rule with no condition never lets it reach; each rule is a function of flat `if`s over the instruction and the
instructions that define its operands (`Selector.definedBy`, `defOf`) that returns false at the first thing that does not
match, and otherwise writes the machine instructions (`newTarget`, `newCopy`, a `choose` function for a choice) into the
selector's function, its own register for every IR register (`vregOf`) and a new one for every other definition.

`arm64.rules` selects arm64 the same way, in its three-address forms of one word each. A physical operand `sp` in a form whose
operand names no stack is the zero register (a store of 0, a negation), `x29` is the frame pointer, and a value is the
result of a rule by being the last definition, or the definition of the variable the root binds (`r:(bin.mul.checked ...)`,
whose product comes before the high word that says it fits). The address of a symbol or of data is `adrp` and the add or
the load after it, each with its reference, which the link patches by its kind (`page21`, `pageOffset12`,
`pageOffset12Word`), and a branch's distance `branch19` or `branch26`; an immediate or an offset that no field holds goes
through a helper of `isel_arm64.maxon` (a constant by the ladder of `movz` or `movn` and `movk`, a wide addition by an `add` of its
high 12 bits and then its low ones, an access at an added address). The IR writes a constant again at each reader, which costs
nothing where the reader takes it as an immediate or one move sets it; a word that needs the ladder and is written at more than
one place, or inside a loop, is made once (`WordSharing`): at the start of the segment that dominates each place it is written
and that no loop holds, with a copy at each place. Where the registers do not allow it to be kept that far, the allocator writes
the ladder again before the reader, which is the code of a ladder at each place; x64's `mov` of any word is one instruction,
so x64 writes each constant in place. Unsigned conditions are x64's after a comparison, and an
unsigned add's carry is `aboveEqual`; after `fcmp` the negation of an ordered condition holds for an unordered pair. A
checked signed division is the one that branches, for -1, since `sdiv` does not fault; a shift by a count branches past
the word as x64's does, since `lslv` counts modulo it; `casal`, `ldaddal` and `swpal` are the atomics; and on Linux the
thread's context is `tpidr_el0`.

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
| lower | `ir` | once the module's verify ended, which made its typing its product and waited on the verify of each module it names, `lowerModule`: it reads of other modules only the typings of those its calls, constants and instances reach, so it waits on no other module's stage. It first finds the instances of generic functions that the module needs (`findInstances`: it starts from the calls recorded by its functions that are not generic, and follows each instance's recorded calls, whichever module declares the generic, with the type arguments bound, until no new instance turns up; `E3906` if the type arguments grow without end), then lowers the module's functions that are not generic, in source order, and the instances, in the order found. A `check` only finds the instances. A module of the standard library that the program reaches is lowered whole too, with the build cache or without it, and its lower waits for its own verify alone, as a module of the program's does: what nothing calls of it is lowered, optimized and emitted, and the link drops it (`backend.markLive`). A place that needs something the backend cannot run yet is noted as a refusal, and the driver reports the first of each function (`E9000`, for example) before the backend, in a task of the project that first holds the runtime to the functions the compiler calls (`checkRuntimeFunctions`) and writes `--emit-ir` |
| optimize | `opt` | for a native build, in the task that lowers the module, right after it: `opt.optimizeModule` puts each function in SSA form and folds it (see "The IR"); then the module's inline (`inline`, a stage of its own that waits on the module's lower and on the inline of each module it names, since it reads their optimized code and leaves): `opt.findLeaves` finds the module's leaves and `opt.inlineModule` replaces the calls of leaves, its own and those of the modules it names, by copies of their bodies and folds again what got one (see "Leaf inlining"), then turns the conditional updates of each function into unconditional ones, rotates its loops, moves what does not change out of them and merges what is computed twice (see "The passes after inlining"); `--emit-ir` and `--verify-ir` see the result. A test keeps the IR as lowered for the interpreter (`Options.keepsLowered`), which tracks borrows by the loads of slots and reports a drop of a register that holds nothing |
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

`interp` runs the same stages through lower, with no runtime module (`lowerForInterpreter` in `self/driver/interp.maxon`), and
gives the IR to the interpreter instead of the backend.

### The modules phase

The driver runs each module's stages, from parse to emit, as tasks of one graph (`self/driver/graph.maxon`), as Go's build
does. A task is a stage of one module (`Task`: the stage, the module and a range of the graph's flat list of inputs), or a
stage of the project as a whole: in a program's compile each library module's `scan` (words that reach it, modules it may name; no segment) runs beside the parses, which the library's parses wait on, `parsed` joins the parses, since resolve may read the declaration table of any module that a
qualified name passes through, and in a program's compile the resolve of each module of the standard library waits for the
program's resolves too, and runs only for a library module the program reaches (`resolvesModule`); `order` makes the module graph and refuses a cycle, and only then adds the tasks of the check
and after it, since a module's dependencies are known once it is resolved; `entry` reports a program with no `main`;
`endCheck` waits on every module's verify and on `entry`, and the program waits on it, so that no code is made of a program
whose check failed, while no module's lower waits for the check of a module it does not reach;
`program` holds the runtime and reports refusals; `link` links the units. A task waits for its module's stage before it (or the task of the project before
it) and, from aliases to verify and for the inline, for the same stage of each module it names, which comes before it in the module order and
covers what the stage reads of those modules: resolve reads only the declaration tables of the modules a qualified name passes
through; declare needs only the type objects of other modules' types, since what it needs of their declarations is checked in
verify; a constant waits for the constants of the modules it names; a check reads the declarations and constants of the modules
it reaches; the lower of a module reads of other modules only the typings of those it reaches, which its verify waited on,
directly or through the verifies of the modules it names, so it waits for no other module's lower; the inline of a module
reads the optimized code and the leaves of the modules it names, directly or through others (`markNamedClosure`, the set the cache's order
checks and lowers for a module that is not settled, so none is a module the compile did not reach), whose calls it inlines. A lower that runs while the check of
a module it does not reach fails reports nothing, since the errors of the earliest stage are the ones reported. A task
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
the tree and the resolution for the resolve (`resolution`), the tree for the check (`tree`), nothing more for the lower, the
inline and the emit (`tables`). A stage reads its module's tree through `ms.tree()`: the resolve, which rewrites qualified names, and the
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
driver: the module's `ast.ModuleProducts`, its code and leaves once its inline ended, and the entry function if its check found
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
| `drains` | a `for x in xs.drain()` | that the loop moves each element of the array out to its variable |
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
  and a map lends its value (`find` and `valueAt`, `expect`) where `get` would copy it, since every record is a box of its own.
  A map's slots hold no `Option`: the slots without an entry are empty, in a `__SlotArray`. A pass that writes a list of
  instructions in another order moves them in place with `table.arrange`, and one that rewrites them in order drains them
  (`for x in old.drain()`), rather than copying each. Hot paths take `mutates`
  parameters and borrow.
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
the directory `MINIMAXON_CACHE` names, or `%LOCALAPPDATA%\minimaxon\cache` when it names none, and none at all when it is `off`; one directory serves every project, since a key names what made its entry. `check`, `run`, the corpus runner and a compile with `--emit-ir` or `--verify-ir`, which need every module's IR, use none. The spec runner keeps a cache of its own in the run's directory (`.spec-run-<pid>/.build-cache`, emptied at the run's start and removed at its end, `MINIMAXON_CACHE=off` turns it off), for the compiles that the run's shared library does not serve (see "Tests").

- **Digests.** `table.Hasher` makes a 128-bit `table.Digest`: two lanes of 64 bits read the same words, each with xxHash64's
  round under its own constants and rotation, and each is finished by MurmurHash3's fmix64 with the length. For inputs nobody
  made to collide, two of n entries share a digest with a chance of about n² / 2^129, so a digest is taken for what it stands for.
- **A module's digests** are made by its parse (`ModuleDigests`): its *source* (`project.sourceDigest`, which the module keeps as
  `Module.sourceDigest`, so that a program's compile holds the runtime's source in its base after the runtime's body was
  taken), its path and each file's name and bytes; and its
  *shown text*, the same without its comment lines and without the body of each function, method or test that no other module
  copies (`publicDigest`): one that has no `uses` and stands in no `uses` type, no `interface` and no `extension`. The layout is
  part of the language, so a header is one line and its body the lines below it to the `end` at the header's indentation.
- **The summary** of a module, made by its inline, is the digest of its shown text, of the IR of its leaves (`opt.findLeaves`; the
  IR printer's text, which names functions and types by link name) and of the summaries of the modules it names: what another
  module copies of it, its declarations, layouts, constants, generic and extension bodies and the leaves it inlines, and,
  folded in, what those depend on, so the leaf that an instance in the module copies from a module that only a module it
  names names is in its key with nothing added to the key (the inline reads the leaves of exactly the modules the summaries
  fold in). Editing any other body changes the module's key and not its summary.
- **The key** of a module (`keyOf`) is the digest of the compile's base (the compiler's identity, the target,
  `--count-allocations`, and for a program the runtime's source), the module's path, its source and the summary of each module it
  names. Handles, the project's directory, the schedule and the number of workers are in no key.
- **The runtime is not compiled where the cache holds its code.** Before any stage of the runtime's compile, `prepareRuntime`
  (`loadedRuntime`) reads the files of the runtime's directory, makes the digest of their source (`project.sourceDigest`, the one
  the parse makes) and looks for the entry under the key the runtime's own compile finds its unit under (`unitKeyHasher` of the
  base, `runtime` and that digest; the runtime names no module). On a hit it links that unit and keeps the runtime's module as a
  shell: its flags, directory, the names of its files and the digest, which the program's keys hold, and no text, declaration,
  tree, resolution or type, since nothing after the runtime's compile reads them. The files are read and hashed on every build,
  never keyed by their size or time: what decides which code is linked is the hash of its bytes. An entry exists only for a
  compile of the same compiler whose `program` stage (`checkRuntimeFunctions`) passed, so a hit has nothing to check. On a miss
  the runtime is compiled whole, as it is for `--emit-ir`, `--verify-ir`, `--debug-info` and `--verify-cache` (which compiles it
  to hold its unit to the entry), and its entry is written. `cmd/gate`'s cache step holds a warm build's `--timings` to report no
  compile of the runtime.
- **The compiler's identity** (`compilerIdentity`) is its build ID, Go's: the link writes into every image that holds the
  build ID's field (the text `minimaxon build id ` and 32 zeros, which only the compiler's source declares) the digest of the image
  as it is with the zeros, before Mach-O signs it (`backend.stampBuildId`), so the compiler reads its identity from a constant
  instead of hashing its executable, a tenth of a warm build. It is the same for the same image on every machine, so C2 == C3
  holds. A compiler whose link wrote none, as the seeds', hashes its executable whole.
- **An entry** (`<key>.entry`) holds a magic text, the layout's version, the key, the module's summary, the digest of the unit's
  bytes and the unit (`backend.writeUnit`: its code, fixups, data, pieces, pointers, symbols and sections, each array written
  straight). A unit carries its drop and copy routines (`backend.carryRoutines`), so it holds no handle and nothing is relocated
  when it is read back (`backend.readUnit`). Entries are written to a file of their own and renamed over their name, so a reader
  never meets half of one; a file that is no whole entry of this layout under its key is no entry, and one whose unit's bytes
  differ from their digest stops the compile, naming the file to delete.
- **Eviction** is Go's: a file's time on the file system says when a compile last used it. A compile that reads an entry, a
  checked state, a summary or the library's names index whose time is more than an hour old writes it again whole (`markUsed`), so a use costs one `File.info` and, at
  most once an hour, a write; the end of a compile that used the cache, when `trim.txt` is a day old, writes `trim.txt`
  first and then deletes every entry, checked state, summary, names index and temporary file unused for five days (`trimWhenDue`; the periods are named
  constants of `cache.maxon`). Every time is the file system's own: a compile reads "now" as the time of a file it writes
  and deletes, so no clock of the machine is held against a file's. Several compilers at once are safe: a write is a rename
  over the name, an entry is read whole when it is probed, and a delete that meets a file another process holds open, or
  one that is gone, is left; at worst a compile misses an entry and makes it again. `minimaxon cache` reports, `cache trim`
  trims now and `cache clear` deletes everything.
- **The order decides** (`planModules`), in the modules' order: a module is *settled* when every module it names is and the
  cache holds an entry under its key, whose summary it then has. A module that is not settled, and every module it names,
  directly or not, read what one another's check, lower and inline made, since it may instantiate a generic or inline a leaf of any
  of them: one of those that is settled and whose checked state the cache holds loads it (`Reach.restored`), and the others
  run their check and lower; a settled module that none of them names runs nothing after its constants (`Reach.loaded`: its
  check ends where its constants did, which gives the entry function and the tests), and its emit links the entry's unit.
- **The checked state** of a module (`<key'>.checked`) is what its stages after its constants made, as its inline left
  them: its slots of the type and function tables whole (every segment's lists and the shells each stage left), its
  `ModuleTables`, its tree as the bodies desugared it, its `Typing`, its `check.ModuleChecked`, and its leaves with their IR
  (a function of its code that is no leaf is kept as its handle, since what reads the code of a module it names reads its
  leaves alone). `publish` cuts a slot by segment and picks shells by stage, so the home as the inline left it gives each of
  those stages' products exactly: a restored module's bodies load it (`restoreChecked`), its verify gives what its check
  ended with and its lower its leaves, and none of them, nor its inline, checks or lowers anything. It is written only by a settled module
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
## Debug info

The debugger is commands of the compiler, as maxon's is (`maxon debug`): it reads a sidecar that the build writes
beside the executable, maxon's `.mxdbg` format, and drives the unmodified executable from outside. What is here is its first
part: the source positions carried from the lowering to the machine code, the sidecar with its line table, its types and
where each local is, `debug --symbolize` and `--dump-info`, which read it, and the debugger's batch mode on x64 Windows.

- **Only with the flag.** `build --debug-info` writes `<out>.mxdbg` (`foo.exe.mxdbg`, maxon's name) and an executable byte for
  byte the one a build without the flag makes: every input of the sidecar is an observation of code already laid out, and
  nothing of it reaches the code, the data or the headers. A build without the flag keeps no position anywhere: the columns
  below stay empty, a pass that makes or lays out instructions tests a flag of the function that is always false, and a
  build of `self` takes the time it took before, within the noise (1,545, 1,520 and 1,620 against 1,523, 1,528 and 1,606 ms, best of nine in three interleaved rounds, with the names of locals; with the flag it takes about 1.8 s and writes a sidecar of 10 MB). A build with it compiles without the build cache, whose units keep no lines.
- **Spans in the IR.** The lowering gives every instruction the span of the statement it lowers (`positionAt` in
  `lowerStmt`), and the code of no statement (the parameters taken, the return at the end) the declaration's, through the
  builder's `pos`, which `Builder.emit`, the one place instructions are made, writes into the column. A span is one word
  (`ir.spanOf`): the module, the offset in its text, and the inline site, 0 for the function's own body. The passes carry the
  column where they lay instructions out again (`ir.keepSpan`, `ir.takeSpans`): SSA construction, whose zeros of slots have
  no span; the fold, whose rewrites keep the slot and whose instructions put before another take that one's span; the
  expansion of the primitives, whose operations and trap block take the call's; leaf inlining; and the `opt.Editor` that
  rotation, code motion and elimination share, where an instruction put in place of another takes its span and one appended
  to a block takes the span of what it moves or copies. So **hoisted code keeps its own line**, a **merged** one (CSE) keeps
  the line of the one that stays, which dominates it, and a rotated test is the header's line, as maxon's observing emitter
  reports them; `--verify-ir` holds the column to the instructions' count.
- **Inline sites.** Leaf inlining makes the copy of a callee a site of the caller (`ir.inlineInto`): the callee and the
  span of the call, whose site field is the site the call is in. The copy's instructions keep the callee's spans, their sites
  moved after it, so nested copies keep their chain, maxon's inline-site table: a row in a copy names the callee's line, and the
  site says where it was called from.
- **Spans in the machine IR.** Selection gives what it writes for an IR instruction that instruction's span
  (`MirFunction.pos`, written by `MirFunction.emit`), and the passes after it carry the column as they lay the instructions out
  again (`keepSpan`, `padSpans`): the classes' legalization, the dead code removal, the layout, block parameters in memory,
  spilling's edits and the rewrite out of SSA. What allocation adds (a spill, a reload, a copy, the parallel copy of an edge)
  has no span, so it is the code of the statement around it, maxon's rule.
- **Rows.** The encoders note a row where the span of the code changes (`CodeUnit.noteSpan`); the function's first row is at
  its start, so its prologue is its first statement's code. The link places each unit's rows with its live sections
  (`describeLines`), and the sidecar writer places each span in its file by the module's line starts (`writeSidecar`).
- **Names in the IR.** A build that writes debug info gives a function the list of the locals and parameters of its source,
  in the one debug list that holds its inline sites too (`IrFunction.debug`, `self/ir/bindings.maxon`), so a build without
  it makes nothing more per function: each one's name and type as the instance has it, and what holds it as the
  passes go. Lowering names the slot of each local and the place of each parameter; SSA construction names each register a
  store to a promoted slot gives it and each block parameter it places for one, and a slot that stays in the frame; the
  fold, CSE and copy elision rename those registers with the instructions that read them. A register of a value that a
  pass removes names nothing. Selection makes the registers the virtual registers (a String's two, a parameter's the copy
  its prologue takes or its place on the stack), and the dead code removal follows the copies it propagates.
- **Locals** (`self/backend/locals.maxon`) are read after allocation from what it decided, and change nothing of it: a
  value is in the register colouring gave it (or a window's) wherever it is live, from its definition to its last use or
  over a block it is live through, and in its spill slot where no register holds it and a reload or a restore of it is
  still ahead; a reload is the value it reloads, and a bracket has the value out of its register. Where two values of
  one name are live, the name is the one defined last in the layout, which is the order of dominance. A slot that stays in
  the frame, and a parameter on the stack, hold their local from the prologue's end to the function's. A local that is
  nowhere is optimized out over the whole function. The encoder notes where each instruction's code starts, and the link
  places each unit's records with its live sections (`describeLines`), with each function's frame: its size below the
  caller's stack pointer, its parameters, and where its prologue ends.
- **Types** (`self/backend/debugtypes.maxon`, maxon's `MxdbgTypeTable`) are every type a local has, closed over their
  fields, cases and elements, numbered in the order of their names: a value is a word and a String two in place (`stringValueBytes`,
  kind `string`, no fields: a reader takes its words by the layout's constants), a record, a union held in a box and an array the address of a box (`struct` with its fields at their
  offsets, `union` with a field per case at its case index typed by its payload, `array` with its element type), a union held as a word
  that word (`unionWord`, its cases without a payload at the word that stands for each: the case index, or the niche), an enum
  its case index (its cases' offsets), an integer alias 8 bytes and signed when its range is, a bool one byte. **The layout
  table is the source of how every holder keeps a value, for the code generator and for the debug info alike**: the type
  table asks `ir.holding` (`self/ir/layouts.maxon`) for a value of a type in a holder (`ir.Holder`: a variable, a field, an
  element, a payload, a capture) and matches on every `ir.Holding` it answers (a word, a String's two words, the address of
  a box, the address of the words of a record held in place, the words of one inline among the holder's, a packed byte), with
  no default arm, so a new way of holding fails the build of the debug info until it is described. Offsets and sizes come
  from the table's own functions (`ir.recordFieldOffset`, `ir.recordBoxBytes`, `ir.caseBoxBytes`, `ir.holdingBytes`). A
  record held in place inline (a field of an in-place record type, an element of an array of them) has an entry of its own
  beside the struct's, kind `inPlaceStruct` (shown as `Struct`, and `(in place)` in the dump): its fields are the bytes at its
  place, where a struct's place holds the address of its box, which is what a variable of it holds as well. An array's size is
  its elements' stride in its buffer (`ir.holdingBytes` of how the buffer holds an element: a word, a String's two words, the
  words of a record held in place, one byte for a packed byte or small enum). A local is `indirect` when `ir.holding` says a
  variable of its type holds an address, and `byRef` (minimaxon's flag) when it is a parameter taken by `mutates` or `sets`,
  whose location holds the address of the caller's place that holds the value.
- **The sidecar** has maxon's layout (`self/mxdbg/format.maxon`): a 160-byte header (the target, the build id, the root,
  the image base, the compiler), the files (the runtime's as `runtime/<file>`, the library's by its module's path, the
  program's relative to its root), the functions with their code ranges, frames, windows of rows and of locals and origin
  (`authored`, `library`, `runtime`, `synthesized`), the line table (20-byte rows with the `statement` and `line-entry`
  flags), the types and their fields, the locals (a register by its number in the target's register file, a slot by its
  displacement from the frame pointer, `indirect` for the address of a box), the inline sites and the disjoint code ranges
  under each, and the layout figures `ImageTextRva` and `ImageTextBytes`. The build id is the
  first eight bytes of the SHA-256 of the code, maxon's, which `debug` takes again of the executable's code (in a PE image
  the section at `ImageTextRva`, in an ELF or Mach-O image the file at that offset) and refuses a sidecar of another build.
  "Where it differs from Maxon" lists what v1 leaves empty.
- **`debug --dump-info`** prints the sections as maxon's does: each function with its frame and its locals (a register by
  its name, a slot as `[rbp-0x18]` or `[x29+0x20]`, an indirect value marked `*`, a String's second word as such) and the
  types with their fields. A spec test with `<!-- DebugInfo -->` is built with `--debug-info` and its sidecar walked as
  maxon's runner walks it: it loads, names some type, and gives the functions windows of locals with no gap or overlap.
- **`debug --symbolize`** takes code offsets or the `rip=0x<address>` of a fault line (from the image base the sidecar
  records: on Windows the loader may move the image, so an address maps only when it was loaded there) and prints maxon's line
  for each, `0x1290  main.maxon:6:2  (in export::down)`, and under it each inlined call the code is in, innermost first,
  `  inlined export::square called at main.maxon:12:3`.
- **The debugger** (`self/debugger`, maxon's `maxon-bin/Debug/`) drives an executable that a build with `--debug-info` made
  from outside it, on x64 Windows: `debug --batch --commands=<cmd;cmd;...|@file> [--stop-timeout=<seconds>] <exe> [-- <args>...]`
  launches the program under the debug port (`Debuggee`) from a green thread pinned to its machine, parks it at the loader's
  stop before any of its code runs, answers the commands in order with one JSON object per line, maxon's events
  (`breakpoint`, `stop`, `backtrace`, `locals`, `value`, `exit`, `crash`, `timeout`, `error`, with maxon's keys and their order),
  and ends the program with the session. The program's output and error both go to the debugger's error. The session exits 0
  when it completed, the program's exit code being data of its `exit` event, and 1 when it timed out (`--stop-timeout`, 10
  seconds by default), saw a crash, or could not start.
  - **Commands**, maxon's names and aliases: `break` (`b`) and `clear` of `file.maxon:LINE`, a bare `LINE` in the file of
    `main`, `*0x<offset>` or a function's name (its symbol, the symbol past its module such as `Pair.make`, its last name, or a
    prefix, the first that answers; several are `ambiguous` with the candidates, none `no-match`); a line arms its
    `line-entry` rows, past the prologue of a function whose first row it is, and a function the inliner copied into its
    callers every copy's start; code of the runtime or that the compiler synthesized is refused `inside-runtime-symbol`.
    `run` (`r`) and `continue` (`c`) run to the next stop; `step` (`s`) and `next` (`n`) walk a line one instruction at a
    time, every other thread held, until control enters another line of the frame, `step` also stopping past the prologue
    of a function of the program's it calls and `next` running every call to its return; a step that returns from its
    frame stops where it lands in the program's code, or runs on when it lands in the runtime's. `finish` runs the frame to
    its return. `backtrace` (`bt`, `where`), `locals`, `print` (`p`) of a local or a dotted path of fields, `help` (`?`,
    `commands`) and `quit` (`q`, `exit`).
  - **A conversation**, maxon's `DebugRepl`: `debug <exe>` without `--batch` launches the program the same way, its output and
    error its own, prints maxon's prompt `(maxon debug) ` and reads one command a line from standard input (`LineInput`, over
    `__Builtins.readStdin`, as the compiler's modules read it), answering each through the batch's commands in maxon's text (`DebugText`, `textLines`):
    `breakpoint set at locals.maxon:32 in export::named (1 site(s))`, `stopped (breakpoint) at ...` with the source around
    the line (`->` at the current one), frames as `  #0 export::named at locals.maxon:32`, locals and values as
    `name : Type = display` indented by their depth, `error: <message>` for a command that fails. A blank line only prompts
    again. When the input ends, or after `quit`, it prints a newline, clears every breakpoint and lets the program run to its
    end (64 stops at most, maxon's), printing what it still answers (`the program exited with code 0`), and exits as a batch
    does.
  - **Traps.** A breakpoint is int3 written over the code; resuming from one restores the byte, steps the thread with every
    other thread suspended and writes it again, and a stop of another thread at a trap meanwhile is put back on it for later.
    A call that a step runs over goes to a trap at its return address that holds only for the frame that returns there: the
    stack pointer the return leaves, so a recursive call, or another green thread through the same code, runs on; the green
    thread may return on another thread, which the step then follows.
  - **Frames** are unwound by the frame pointer: the top frame by the code of its function (`push rbp; mov rbp, rsp`, or
    the pushes of a leaf with no frame counted from its prologue or epilogue), the callers through rbp, each with the inline
    sites at its code as frames of their own, innermost first. A stop carries the source around its line (two lines either
    side, read from the sidecar's root) and its backtrace.
  - **Values** are read by the sidecar's locals and types: a local in a register of the stopped thread (an xmm register for a
    double) or a frame slot, one of a name with no record at the stop `not-live-here` and an optimized-out one
    `optimized-out`; a String from its two words, as `"text" (len=N)`; a record's box, or its words held in place, with its
    fields as children, an enum's case, a union's case with its payload, an array's count with its first 32 elements each
    at the array's stride, a double from its bits; a `byRef` parameter from the address its location holds.
- **Verified.** `--verify-ir` on a build with the flag holds every span of every function to the source of the function it is
  in, from its header's line to its end, and a span in an inline site to its callee's (`verifySpans`). The gate's
  `debug info` step builds programs on the four targets with and without the flag, compares the executables, verifies the
  spans of `self` and those programs, walks the sidecar of `self`, holds the dump of a program's locals to a register, a frame
  slot, a String's second word and a record's fields on each target, on x64 Windows holds a batch session of that program
  to its events (a breakpoint on a line and on a function, the backtrace, a local in a register and one in a slot, a step
  over a line, a String local, a record's fields, a step out and the run to the exit) and a conversation, fed from a file, to maxon's text of a breakpoint, a stop, its locals, a field, the frames, an unknown command and the program's end, and symbolizes the `rip=` of a stack overflow on x64
  Windows, x64 Linux and arm64 Linux to its line. A second program (the holdings program, `cmd/gate/debuginfo_holdings.go`)
  has one value of each way `ir.holding` keeps a value in each holder: its dump on each target holds the in-place marks, the
  offsets and the strides the table gives, and on x64 Windows the debugger reads back what a local, a field (two levels
  deep), an array's element (a record held in place, a word, a String, a box, a packed enum), a union's payload, and a
  parameter taken by value, `mutates`, `sets` and `consumes` and a result hold. The values are asserted, so a change of how a
  holder keeps a value that the debug info does not follow fails the step. The debugger drives x64 Windows alone; the sidecar
  of the other three targets is held by the dump alone. The sidecar is the only debug info any target writes (no PDB or
  CodeView, no DWARF); a closure's capture, and the locals and parameters of an inlined callee, are not described.
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
- **Jump threading** (`self/opt/thread.maxon`, maxon's `threadConstantBranches`) runs next, and again with the fold after leaf
  inlining: an edge that passes a constant to a block that holds nothing but a branch on its own parameter goes to the side
  the constant takes, as does an edge of a branch on a register that passes that register, whose value the branch decided
  (the short circuit of `a or b` and `a and b`, which becomes the branches on its operands alone), an edge into a block that holds nothing but a jump goes where it jumps, and a block whose jump goes to a
  block that holds nothing, takes no parameters and branches takes that branch. A `match` that gives a bool which
  an `if` tests, and an inlined predicate, become one compare and one branch. A `match` on an enum, a union or a bool with no
  `default` arm takes its last arm untested, since the checker holds it to name every value.
- The fold pass (`opt.Fold`, `self/opt/fold.maxon`) runs next, on every function of a native build: one worklist that
  folds constants (by the IR's own meaning, `ir.evalBin` and the rest in `self/ir/evaluate_gen.maxon`, which the interpreter
  runs too), propagates copies, applies the rules of `generic.rules` (and, run again past a function's source form, those of
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
  of its body. The policy is maxon's leaf rule (`InlineLeaves`): a leaf calls no code of the program on its hot path (its
  blocks that do not end in a `trap`); a call of a runtime entry and a `drop` stay one call in the copy; it has at most `maxLeafOps` (24) instructions and
  terminators on its hot path, a parameter's read aside, or `maxColdArmLeafOps` (28) when it has cold blocks, which hold
  at most `maxLeafColdOps` (12); it keeps no slot (a frame zeroes its slots once), holds no closure or behaviour body and
  no refusal. Calling nothing, it is never recursive. The leaves are decided on the bodies as the fold left them and a call
  is replaced only outside a cold block; a function whose calls were all copies is a leaf from then on (maxon's
  `followPromotedLeaves`), so a module's functions are inlined into callees first (`opt.calleesFirst`), and a predicate over
  a helper, or `String.hash` over the fold of its bytes, is copied whole. A copy of one block that returns a
  value it makes goes into the caller's block with that value made in the call's register; any other copy is blocks of
  its own after the function's (`ir.relocationInto` appends the callee's pools, and the generated `relocateInstr` and
  `relocateTerm` move each operation's registers, blocks and pool indexes), entered by a jump that ends the call's
  block, and its `ret` and `throw` jump to a new block holding the rest of it, passing the call's value and error (0
  for the one that is not there), so a throwing callee joins the caller's failure handling where the call did. A module's
  calls of the leaves of the modules it names, directly or through others, are inlined as its own: its inline task waited for theirs (see "The modules
  phase"), which a generic's instance in the module needs, since its code calls into the module that declares the generic and what that names. On `self`, 7,226 calls are inlined, and the build of `self` by itself takes about 6% less time.
- **The passes after inlining** run on each function of a module once every call of a leaf in it is replaced, in this
  order. What each may do with an operation is decided in one place, `opt.motionOf` (`self/opt/motion.maxon`), which names
  every operation: a constant, a pure operation, a division (pure but faulting on 0), a check, a read of memory that only
  the function's own writes and calls change, a read of memory another thread may write (a runtime global, a raw word, the
  thread's context: never merged or moved), an operation that changes nothing a read sees (an allocation, a copy, a box, a
  closure, a drop of a register, whose boxes unique ownership gives to the dropped value alone), and a clobber (a store, a
  move out of a place, a `dropslot` or `dropat`, an atomic operation, a frame box, and every call but of a runtime entry that only
  reads, such as `rt.streq`, which is quiet: a `mutates` argument is
  the address of the caller's place and a push or an insert is a runtime call that changes its array's length and buffer,
  and the IR keeps no types or conventions to say which box a call may change, so a call ends every read).
  - **If-conversion of conditional updates** (`self/opt/select.maxon`) turns a branch one side of which only adds,
    subtracts, ors or xors constants into values it passes on to the block the other side goes to (`if c 'x' n = n + 1
    end 'x'`) into the updates made on both ways with an amount of the constant where the condition holds and 0 where
    it does not (the comparison's 0 or 1 itself for a constant of 1, else the constant and'ed with its negation), and a
    jump. A branch on data the processor cannot predict costs mispredictions and a taken branch per iteration; on
    arm64 the scan of `tests/bench/array-indexing.maxon` went from twice Go's time to Go's. A checked update keeps its
    check, which an amount of 0 cannot fail, so it fails exactly when the side it came from would have.
  - **The split of the element accesses** (`self/opt/buffers.maxon`) makes each element read, element address and byte
    read of a byte array the read of the array's buffer (`buffer`) and the access at the index of it (`elemat`,
    `elemaddrat`, `byteat`), so the buffer is a value that the passes below merge and move as any read: in
    `a[i] = a[i] + x` the buffer is read once for the read and the store, and a loop that writes no memory reads it in its
    preheader. A byte read of a string needs no split: a String's first word is the address of its bytes, which the
    access indexes as it is. A store of an element changes no array's box, length or buffer (`opt.storesElement`), so
    the passes below keep those reads across the stores of elements: in nbody's inner loop, which writes only elements,
    each array's box, length and buffer are read once an iteration for its two elements' reads and stores.
  - **Loop rotation** (`self/opt/rotate.maxon`) turns a loop whose header tests whether to leave, as a `while` is lowered,
    into the test once before the loop and a copy of it at the end of each iteration, so an iteration ends in one
    conditional branch instead of a jump back to the test. It copies only a header of one segment and at most
    `maxRotatedTest` (8) operations that can be computed again, entered from the loop only by jumps, so a loop grows by at
    most `maxRotationCopies` (16) operations; the header's parameters become parameters of the block it entered the loop by
    and, where the code after the loop reads them, of the exit, each read renamed by the region of the dominator tree it
    is in, and a loop with a read anywhere else is left as it is. `for` loops are lowered rotated already.
  - **Loop-invariant code motion** (`self/opt/licm.maxon`, maxon's `LoopInvariantCodeMotion`) moves to a loop's preheader
    (the block that enters it, or one made on the edges that enter its header) a pure operation whose operands do not
    change in the loop; a read, when the loop holds no clobber, or holds only stores of elements and the read reads no
    element (a length, a buffer, a slot, a word of a box: `opt.readsBeyondElements`), and the first iteration runs it anyway (its segment
    dominates every exit and every edge back), when a read of the same array's box moved before it, or when it reads one
    of the function's slots, which is always there to read; and a check or a
    division only from the header's prefix, the operations before the first that stays, so it fails in the preheader
    exactly when, and after the same effects as, it would have at the start of the first iteration, and only when its
    failure path reads nothing the loop defines. Loops are taken innermost first, so a value leaves a whole nest; a
    constant is copied, not moved.
  - **Common subexpression elimination** (`self/opt/cse.maxon`, maxon's `CommonSubexpressionElimination`) walks the
    dominator tree with a scoped hash table: an operation that computes what an operation dominating it computed goes, a
    check that an identical dominating one passed goes with its failure block, and a read is merged only with one that
    sees the same version of memory, a number that changes at each clobber, at a join whose ways in disagree and at the
    header of a loop that holds a clobber, so no read is merged across a write on any path; the memory outside the
    elements of arrays has versions of its own, which a store of an element does not change, and a read of no element is
    keyed by those. An operand that a constant
    gives is keyed by its number, since constants are never merged and two uses of one number read two registers. A
    block that only one side of a branch enters knows the branch's condition everywhere it dominates, so a branch below it
    on the same condition becomes a jump to the side it takes, and the blocks only the other side reached go. What the
    walk knows also proves an index below a length, as unsigned words: a branch's comparison on the side that says so,
    and a block parameter that every edge into it gives a value so proven below one length defined above the block, as a
    counted loop's index is at its header (below the length at the guard and at each iteration's test); a bounds check
    of a proven index against its length goes with its failure block.

  - **Copy elision** (`self/opt/copies.maxon`) runs last: a copy that is only lent (read by an operation that keeps nothing,
    or by a runtime entry that only reads, `ir.onlyReads`) and dropped on every path, with no clobber and no drop of anything
    else between, is the value it copied, as a `let` bound to `Array.get` that is only compared is; and a copy of a field
    that the field's assignment then drops is a move, as `let old = field.clone()` before `field = new` is.

  They share one analysis, found again only after a pass that changed the function, and an edit that only removes
  instructions moves the rest down in place. On `self` they rotate 598 loops, move 13,032 operations out of loops (once
  for each loop an operation leaves) and remove 62,900, for about 150 ms of the build's task time (the analyses 50, rotation
  20, code motion 45, elimination 40), which the emit stage of the compiler they build gives back
  in part; the instruction selection's push also reads an array's length once when the index is that length.
- **E5001 and the optimizations.** E5001 is a property of the program as written: a hot loop that needs more registers
  than the target has with its calls as calls. A function compiles in one of two ways, optimized or refused with E5001,
  and an optimization never makes a loop fail: with a call, the values live across it were in memory across it (the call
  tier of spilling), across a copy of a leaf or an expanded primitive they are not, a value moved out of a loop or merged
  with one before it lives across the loop, and a rule of `pressure.rules` holds one more value than the division it
  replaces. Maxon checks a pressure budget only for its wrapper and called-once rules and none for leaves. Here two steps
  keep the optimizations within the registers:
  - Code motion and elimination record each value they make live where the program did not hold it
    (`ir.IrFunction.lengthened`: one moved out of a loop, one that a later computation of it was merged into), and
    spilling reloads those first in a hot loop over its pool, before its E5001 test (tier 4 of "Liveness, spilling and
    E5001"). A reload stands where the program computed the value, as one load in place of the program's own read or the
    operation it took out, so a hoist or a merge that does not fit is undone in that loop alone, and the other loops and
    the straight-line code keep theirs. That decision is the allocator's own count of the machine code, so no estimate
    at the IR can disagree with it.
  - Where a hot loop is still over its pool, its function's source form decides: the IR it had before the expansion,
    the rules of `pressure.rules`, the copies and the passes after them, which `opt.sourceOf` makes where the emit asks
    the driver for it (`backend.SourceForms`) by lowering the function again from its declaration and running the passes
    before it, which never make an instruction hold more values at once than the code they replace
    (`self/opt/optimizer.maxon`) and give the IR they gave at the lower (a cache hit links a unit and runs no emit, so an
    emit always has its module's checked state). The emit selects and spills the same body of it only as far as the
    E5001 test, writing nothing (`HotLoops.test`, `selectOrRefuse`): where that refuses a loop, the refusal is the E5001
    reported, which counts the program's own variables; where it does not, the optimized function is compiled with its
    loops spilled as any other code (`HotLoops.spill`), which `--timings` counts as a function with hot loops spilled.

  So a function is refused only where its source form is refused by the same test (`refuseHotLoops`), and a program that
  compiles without the optimizations compiles with them, optimized: no other form of a function is ever compiled, and
  the invariant needs no check of its own. `tests/run/a-hot-loop-that-fits-with-its-calls-compiles-with-their-copies.maxon`
  is a loop whose copies of a leaf need more registers than the loop with its calls, and so is a loop of the `main` of
  `tests/run/array-methods.maxon`, which holds fifteen values once its element reads are expanded, and the loops of
  `tests/run/a-loop-at-the-pools-edge-divides-by-a-constant.maxon`, which fit with the division and not with its
  multiply: each is spilled where the source form fits. On `self`, tier 4 reloads values in the loops of seven
  functions, and no function needs its source form.
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
- **Spans.** A build that writes debug info (`Project.keepsSpans`) keeps the source position of each instruction in a column beside the
  instructions, `IrFunction.spans`, by instruction index, never in the record: one word, the module, the offset in its text and the
  inline site (`self/ir/spans.maxon`). Any other build leaves the column empty and no pass touches it. See "Debug info" for how
  each pass carries it.
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
| arithmetic | `bin` (add, sub, mul, div, mod, and, or, xor, shl, shr; `.u` for unsigned; checked ones have an `else` block), `cmp`, `neg`, `not`, `ctz` (the count of trailing zeros, 64 of 0; `tzcnt` on x64, `rbit` then `clz` on arm64), `guard` |
| collections | `length.array`, `length.string`, `bounds`, `elemaddr`, `elem`, `byte.array`, `byte.string`, `word.8` to `word.64`, and in a native build's passes after inlining `buffer`, `elemat`, `elemaddrat`, `byteat` (see "The primitives in the IR") |
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
is nearest the return address, which the caller pops (`placeArgs`); a String argument as two words in a row, the
address of its bytes and its word; the result in `r8`, the word of a String result in `r9`, a float result in `xmm0`, and the
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

Records, and unions held in a box, are heap boxes of 8-byte words (a union's first word is its case index), allocated by the running
thread's heap (`runtime::alloc`, see "Native freeing") and freed by it (`runtime::free`) where their owner dies (see "Drops"); a box is
zeroed when it is allocated. A String is two words (see "The IR"): a literal's bytes live in the executable's read-only data
and are never freed, and any other string's bytes are a buffer on the heap. An enum value is its case index. A constant
whose value is a box lives in the data section (see "Constants in the data section"). The image keeps what nothing writes
and holds no address, the bytes of literals and of the entry stub's texts and the words of float constants, in read-only
data: PE's `.rdata`, a read-only ELF `PT_LOAD` between the code's and the data's, and Mach-O's `__TEXT,__const`; the link
lays out each piece of a unit's data (`DataSection.readOnly`) in one or the other, so a write through a literal faults.

**The layout table** (`self/ir/layouts.maxon`) is the one place that says how a value of a type is held and where the parts
of a box lie: a value's form (a word of nothing to drop, a managed word whose 0 is the empty place, or a String's two
words), how many words it takes, whether its all-zero value is no value of the type (so an empty slot can be told), its
niche (a bit pattern no value has: a managed word's 0, a ranged integer's word below its least value, an enum's past its
last case, a bool's past true), and the size of a record's or a union case's box and the offset of each field and payload.
Lowering, the drop and copy routines, the datums, a map's groups, the register kinds and the debug info all ask it, and it
computes each answer from the declarations without allocating, so a change of layout is a change of its answers.

A union is held as its form says (`ir.UnionForm`): where no case carries a payload, as its case index, a word like an enum's;
where one case carries one integer, enum or bool that has a niche and the only other case carries nothing (`Option with
Count`, an Option of a slot key, `Option with bool`), as that payload, with the niche standing for the other case (a `Count`'s
-1, an enum's case count, a bool's 2; an integer's niche is the word below its least value where an immediate reaches it, so
the test of the case is one compare); where that one payload is of the union's type parameter and a record or an array in the instance
(`Option with Place`, an Option of an array), as the payload's address, with 0 for the other case (the null form, `ir.heldAsNull`): it is a managed word that
drops as its payload, whose drop skips 0, and copies as it (`copyNullUnion`, which keeps 0), so `Option.some` of a box is
the box and `Option.none` is 0, and the interpreter holds that 0 as one object of its own (`kindNone`, which a comparison
sees as 0), since a managed register that holds 0 holds no value there; a union that names its payload's type keeps its box,
since it may be thrown and the error channel's 0 says no failure (a thrown union in the null form, a generic one, stops
the compile in `ir.errorInBox` with an internal error); and otherwise as a box. A union held as a word holds nothing to drop, so it is a
scalar of the IR: `Option.none` and an Option of a number allocate nothing, an array of them is an array of words, and a
match reads the case from the word. A thrown error of such a union is its word plus a bias that makes it nonzero, as an
enum's case plus 1 is (`ir.errorBias`), and the debug info gives it the kind `unionWord`, each case without a payload at
the word that stands for it.

A record with no destructor whose every field is a word of nothing to drop or a record held in place is held in place
(`ir.heldInPlace`, `self/ir/inplace.maxon`): a register holds the address of its words, a scalar of the IR that nothing drops, and every
holder that keeps the value copies the words. A literal and a `.clone()` are a frame area of their own (`framearea`, the
same words each time the frame reaches it) filled with `setfield`s, with no allocation. A local that owns its value keeps
the words in a frame area of its own, which every store to it copies into (`Lowerer.emit` turns the store of a value into
the copy, `storeLocal`), and the slot holds the area's address; a local that borrows (a binding of a part, a `let` of a lent
result) holds the lender's address. A `let` parameter is the address of the caller's words, and a `mutates` or `sets` one
the address of the caller's place, which holds the address of the words, so an assignment through it copies into them. A
function whose result is held in place, and which does not lend it, takes the address of an area of its caller's frame
after its arguments, copies the result there before its scopes end and returns the address (`resultWords`,
`returnPlaced`); given 0, as an async call's coroutine gives it, it returns a box of the result, which its promise owns.
An array keeps the words of each element in its buffer, the element's size apart (`arrayInPlace`, the `rt.recs` entries,
which take that size): an element read is the address of its words, an insert opens a zeroed room and copies the words in,
a remove copies them out to a frame area and closes the room, a swap, a store and an `exchange` copy words, and the
array's drop and copy routines move its buffer's words and nothing else. The element's place, which a `mutates` argument
is given, is a slot that holds the address of the words; the array's primitives take what they consume as its address,
with no box, and the backend writes an element's address in place, an index shifted or multiplied by the size, which
lowering gives as a constant. An array literal of such records is built where it is read, not laid out as data.
An element whose copy is its bytes (`ir.plainElementBytes`: a byte, a word of nothing to drop, or such a record's words) is
copied with the rest of its span in one copy of the bytes: a slice and an append (`ir.appendSpan`) call `rt.array.appendSpan`,
which makes the room once, and the array's copy routine copies its buffer whole (`copyPlain`). Any other element, a String or
a box, is copied one at a time, a slice's and an append's into room reserved for the span first.
A record of any kind but a tuple keeps the words of a field held in place among its own, one after another in
declaration order (`ir.inlineFieldWords`; `recordFieldOffset` counts them), so records nest at any depth up to
`deepestInline`: a field read is the address of the field's words (`fieldaddr`, a borrow of the record), an assignment
and a literal copy the words in, the field's place is a slot that holds their address, the record's drop routine skips
them and its copy routine copies them as words, and a constant record with such a field is built where it is read. A field
declared as an instance over its record's type parameters (`Wrapper with T`) is a box in every instance. `ir.holding`
(`Holder`, `Holding`) answers, in one place built from these predicates, how a holder keeps a value of a type, and the debug
info takes every one of its answers from it by matching on all of them; the lowering still asks `heldInPlace`,
`inlineFieldWords` and `ast.packsByte` directly, and must agree with it.
Every other holder keeps the value as a managed word, the address of a box of its words that it owns, as every record was
held before: a payload, a half of a map's pair, a capture of a closure or a
behaviour, an argument of a message, the contents of an interface value, a frozen value or a cown, and an argument that a
parameter consumes, since a callee may keep what it consumes past the caller's frame (a coroutine, a behaviour): the value
goes into the holder as a copy of its words into a new box (`boxed`), is read from it as the box's address, which is the address of
its words, and leaves it, by a remove, a take or the start of a function that consumes it, as a copy in a frame area and a
drop of the box (`unboxed`, `adoptPlaced`). `exchange` copies the place's words out and the new ones in.

### Constants in the data section

A constant whose value is a box built only of what is known before the program runs (a top-level or `static let`, and each
raw value of an enum or a union whose raw values are records) is laid out once, at compile time, in the executable's data,
and read from there (`ir.Datum`, the `datum` instruction). `ir.datumOf` (`self/ir/datums.maxon`) is the one place that decides
it: it lays out integers and floats that the checker knows, `bool`s, enum cases, plain strings, records and tuples (with
their fields' defaults), cases of a union with no payload, array literals (a packed one as bytes), `.clone()` of these, the
other constants they name (laid out again inside this one) and calls of a literal factory, whose body is read with its
parameters bound to the arguments. An empty or fixed-length array that a module-level or `static let` makes with `create` of an
`Array` or a `FixedArray` (`Names.create(0)`, `Nums.create(3)`, `Buf.create(4)`, `Buf.create(3, value: 7)`), or with an empty
literal (`Names{}`), and a case of a union with a payload (`Op.add(1)`, `Hold.owned(9)`) are static data too, as maxon's
initializer may make them: neither calls a function the program or the library declares, so the array is laid out as
lowering would make it (a count of zero elements, or of copies of the value, each laid out again; `create(n, zeroed: false)` is
the empty array with room for `n`, zeros in the data, so its `capacity()` is `n`) and the case as its box, with nothing run
before `main`. Everything else is built where it is read, as before: a number, a string or a function value
as the whole constant (a number is a constant of the code and a string a literal already), and a constant that holds an
interpolated string, a byte string, a map or a list, a function value or a closure, a value boxed into an interface, a
conditional or a match, or an operator or a cast the checker does not fold. The raw values of an enum or a union are data
only when every case's is, so a read has the same owner whatever the case.
Not yet: a module-level or `static let` that calls `create` of a `Set`, a `Map` or a `List`, as maxon's initializer may: here it
is refused with `E3220`, since it needs a static layout of a hashed collection.

A datum is one piece of the data, its boxes laid out as lowering would build them on the heap (a record's words padded as
`boxBytes` pads them, an array's header and buffer) followed by its strings, named `const <module>::<name>` or
`raw <type>.<case>`; every unit that reads it defines it and the link keeps one, as it does an instance. The words that hold an
address of the piece are written by the link and listed in the image's base relocations (`.reloc`, `IMAGE_REL_BASED_DIR64`
entries in blocks of a page, in address order), so the loader moves them with the image. The data section stays writable and a
constant has no count and no marker: the checker refuses writing a constant or moving a part out of it (`E2013`, `E3901`), a
read that only borrows reads the data, and a read that takes the value (a `let`, a `consumes` argument, a box of an
interface) takes a copy (`settle`), so nothing ever drops, counts or writes a datum, and the strings it holds own nothing, as
every literal's word says. The interpreter makes each datum once by its symbol, as immortal objects, and reports a
drop of one, a store into one or a move out of one as a violation.

### The primitives in the IR

An `Array` (see "Primitives") is a heap box that only runtime entries touch, so the IR holds no
layout of them: an operation is a `runtime` call of a named entry (`internal/builtingen/table.go`), and the interpreter and the
runtime each implement the entries. The element is always visible in the call:

- An entry that takes an element **moves it in** (`rt.array.insert`), one
  that returns an element **moves it out** (`rt.array.remove`), and `rt.<primitive>.elem`
  returns the **address** of an element: a borrow, valid until the next call that changes the primitive. A subscript is that
  call and a `loadat` or a `storeat`, and a `mutates` argument `a[i]` is the address itself. A value that changes the primitive
  is computed before the address is taken, which is what the ownership pass's exclusivity rules guarantee at the source.
- **The expansion of the primitives** (`self/opt/expand.maxon`, maxon's `inlineManagedPrimitives`) writes the common case of
  the entries that read a length or an element of an array, a byte array or a string as operations of the IR, so that the
  optimizer, instruction selection and the register allocator see loads and no call: `length.array %a` and `length.string
  %s` read the count (`rt.array.len`, `rt.bytes.len`, `rt.str.len`), `bounds %i, %n else b7` goes to `b7` unless `%i` is
  below `%n` (unsigned, so a negative index is past every length), `elemaddr %a, %i shl 3` is the address of the element of an
  array of words (`rt.array.elem`) and `elemaddr %a, %i shl 4` of an array of Strings (`rt.strs.elem`), `elem %a, %i shl s`
  reads it where only the `loadat` right after the call reads the address (a scalar, or a managed element or a String lent
  as `loadat` lends it), and `byte.array %a, %i` and `byte.string %s, %i` read a byte
  (`rt.bytes.get`, `rt.str.byteAt`). A read of an element is a `length`, a `bounds` and the read; the failure block of the
  `bounds`, one per read after the function's blocks, makes the call that was there and a `trap`: called there, the entry
  traps with its own message and exit code, natively and under the interpreter, so a miss fails exactly as before. A little-endian ordered word of a constant width of 1, 2, 4 or 8 bytes (`__Builtins.readLittleEndian(bytes, at, 8)`) is lowered as a `length`, a test that its first and last bytes are below it, and `word.64 %a, %i` (`word.8`, `.16`, `.32`), one load of the bytes, which a native build splits into `buffer`, an add and a `loadraw`; a word outside the array calls `rt.bytes.readWord`, which fails with the index, and a big-endian or variable width always does.
  Selection writes a `length` as a load, a `bounds` as a compare and a branch that ends its block (`emitBounds`: an edge
  in the middle of a block would keep what the trap block reads live to the block's end), and an element as a load of the
  buffer's address and an indexed access (`[data + index*8]`, `[data + index]`; a string's bytes are in its box, so a
  byte of one is the access alone). Every
  other entry stays a call: growth, insertion, removal, the byte write, lists. Where an index is proved
  (inside a `FixedArray` by the checker, or by the test of a `try` around `get` and `set`), lowering writes the entry's call
  `proven` (`runtime rt.array.elem(%a, %i) proven`), which the expansion makes the access alone, with no `length` or
  `bounds`: lowering writes calls only, so the source form holds every call the expansion takes out. The interpreter runs the expanded form (the driver expands the IR it keeps for it, as
  lowered), each operation on its model of the collection: a `bounds` by the IR's own meaning (`ir.evalBounds`), as the fold
  decides one whose operands are constants, and an element by the cell its entry's address would name, so its checks of a
  stale or freed box and of a borrow a call is still waiting to use (`elemaddr` and `elem` open a window as `loadat` does)
  are those of the call; none of the passes after inlining run on the interpreter's IR, so the `buffer`, `elemat`,
  `elemaddrat` and `byteat` that the split of the element accesses makes reach only the backend. A native build expands each function after the fold, before leaf inlining; with no call left in
  a loop, a loop that the call let compile may need more registers than x64 has, so the function's IR before the expansion
  is its source form, whose loops decide E5001 (see "E5001 and the optimizations").
- An array whose element is an integer that fits in a byte (`ByteArray`, `Array with Byte`) or an enum of at most 256 cases,
  whose value is its ordinal, packs one byte to an element, so it
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
- An entry that is given an index, a key or a slot key that names nothing traps. The queries that cannot miss
  (`rt.<primitive>.len`) are what lowering tests before it calls an entry
  that would trap, failing with the operation's error when the test fails (see "Primitives").
- A slot key is a word, never 0. An entry that returns one returns 0 for none, where the operation fails.
- **The OS entries** (`rt.os.*`) take and give Strings and arrays, never a record or a union: lowering takes the String out of a
  `FilePath` or an `Executable` and makes the `FilePath`s and the `CollectedOutput` from what the entry gives, so no entry knows
  the layout of a record. An entry that can fail takes last the address of a word, a slot of the caller's, which it sets to the
  case plus 1 of the operation's error or to 0, and lowering branches on it as on the error of a throwing call. The runtime
  tells a failure from the Win32 error under it (`ir.failureOfWin32`: not found, access denied, busy, already exists, or
  another), and each operation names the case of its own error that each failure is (`ir.failureCase`).
- `clone()` is one instruction, `%r = copy %s T` (see "Drops"), whatever `T` is: a record, a union, a string or a primitive.
  What it does to each part is code that depends on the type, so it is a routine of its own for each type that the backend makes
  (below) and the interpreter does by the shape of the value; a copy of an array copies every element by its own type.
- A `SlotKey with T` is `scalar`; everything else here is `managed`.

**Native.** The array, the byte array and the OS entries have code: the entries are functions of `runtime/` (`arrays.maxon`,
`bytes.maxon`, `os.maxon`, `subprocess.maxon`) that the program links like any other function. An array is a box of three
words, the length, the capacity and the address of its elements, which are 8-byte words; `rt.array.insert` grows it by the growth policy
(see "Text") and shifts what follows, and frees the buffer it leaves (the elements moved to the new one, so only the buffer goes), and
`rt.array.remove` hands the element it removes to the caller, who owns it. `rt.array.fill` appends a number of copies of a word that needs no copy of its own, which an array of a scalar that `create` or `filled` makes, and the words of a buffer of groups, are written by in one pass. `rt.array.shrink` sets the length of an array or a byte array and keeps the buffer: `clear` and `truncate` of an element that needs no drop are that one call, and of an element that owns something they drop each element in place, last first, in one loop in the IR (`arrayShrinkTo`), and then call it. A byte array has the same three words, with a buffer
of one byte to an element; its copy and drop routines copy and free the buffer whole.

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
routine of `T`. A string literal owns nothing inside a frozen value either, since dropping it is the string's drop.

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
is immutable, so a copy of it may reach another thread, as the capture of a behaviour will.

### Interface values in the IR

An interface value is one word, the address of a box of two words: the address of a **table** and the value, a word as any
value is (`internal/abigen`'s `iface*` rows). The table belongs to the concrete type, the interface and the module of the
conformance the value was boxed with, so its link name says all three (`table export::Square as shapes::Shape by
export`), and it is static code in the image: an entry of a word for each of the routine that drops the value, the one
that copies it (where the interface extends `Copyable`), the one that names its type, and then each method the
interface requires, its own and then those of the interfaces it extends (`ast.requirementsOf`). An entry is a jump to the
routine or the method, so calling the entry is calling it with the arguments as they are. The program is closed, so no
table is made at run time: every module that boxes a value with a conformance writes the table in its unit, and the link
keeps one, as it does an instance. The table of a value widened to an interface instance (see "Interface values", "Associated types held at an
interface") ends its link name with `widened to` and the instance with what each interface in it binds, since it calls a
stub, named for the table it boxes the result with, where the conformance's own table calls the method.

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
use after free.

### Drops

The IR says where every owner dies. Lowering emits a drop wherever the rules of "Ownership" free a value, and both the
interpreter and native code free exactly there.

| Instruction | Drops |
|---|---|
| `dropslot s2 T` | what slot `s2` owns, if anything, and leaves it empty: a local at the end of its block, on every way out of it (fallthrough, `return`, `break`, `continue`), and the old value of a local that is assigned |
| `dropat %a T` | what the address holds, if anything: the old value of a field, an element or a `mutates` or `sets` parameter that is assigned |
| `drop %r T` | the value in a register: a temporary that nothing took, an element removed and discarded, the value given to `array.filled` |
| `%r = move s2` | reads a slot and leaves it empty: a whole local that moves (a binding, a `consumes` argument, a return, a field, a branch of a conditional) |
| `%r = moveat %a` | reads the value at an address and leaves it empty: the payload that leaves the box of a union that `match consumes` takes apart, the String that leaves each element of the array `Directory.list` gives, for the `FilePath` made of it to take its place, and the value that `exchange` takes from a place |
| `%r = copy %s T` | a deep copy of the owned value in `%s`, which stays where it is: a new box for each record, union and array inside it, a new string for each string that is not a literal, and another owner of the same box for a frozen value or a cown |

`T` is the type with an instance's type arguments applied, so the backend can call the drop routine of that type. Dropping a value
drops what it owns, deeply by type: the fields of a record, the payload of a union, the elements of a primitive. A borrow is never
dropped: a `let` or `mutates` parameter, a projection, and a `match` binding of a payload, which is stored with
`store s3, %r borrow`. A place that holds nothing, because it was moved from or never assigned, drops as a no-op, so one drop at
the end of a scope serves every path to it and no analysis of paths is needed.

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
- **A string is owned like any value.** A literal and the name of a case are never freed (its word does not own its bytes, so a drop of one is a
  no-op), but an interpolation makes a new string, and so does `clone()` of a string that is not a
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
  element buffer and the header, a string frees its bytes when its word says it owns them. A copy routine allocates the box the way
  lowering sizes it, and copies each word, a managed one by the copy routine of its type. A routine calls the routines of its
  parts and nothing is inlined, so a type that contains itself through an array (a tree) is a routine that calls itself. A drop
  of a record or a union with nothing managed in it is `runtime::free` itself, with no routine; a scalar, an enum and a `SlotKey`
  have none and are never dropped. The routines are named by their type's link name and ordered by where the modules first ask
  for them, never by a map.
- **A String is two words**: the address of its bytes, and a word of its length shifted left by three, whether each byte
  is below 0x80, whether the String owns its bytes, and whether it is a view of another String's (`stringLengthShift`,
  `stringAsciiBit`, `stringOwnedBit`, `stringViewBit`). The IR
  holds one in one register of its own kind (`kindString`), which instruction selection makes two virtual registers; a
  place that holds one (a slot, a field, an element, a capture, an interface value's box) takes two words, and the empty
  place is 0 and 0. A literal points at its bytes in the executable's read-only data and owns none of them, so its drop does
  nothing and its copy is itself; the drop of a String frees its bytes only when the owned bit says so, and its copy
  allocates only then or when the view bit says it is a view (`rt.str.view`, which instruction selection writes in place:
  the address plus the start, and the length, the ASCII bit and the view bit), whose copy owns its bytes. A view needs its
  bit because a copy is made where nothing says whether the String is a literal, whose copy may share bytes that outlive
  every copy, or a view, whose copy may not: a `let` parameter is given either. The length is the word shifted right, with no access to memory. `strptr`, `strword` and
  `strmake` take a String apart and make one of its two words, which only the runtime's String intrinsics write.
- **A String of 1 to 15 bytes holds them in its two words** (`stringInlineBytes`, `runtime/inline.maxon`): the first eight
  in the first word, the lowest first, the rest in the second between `stringLengthShift` and `stringCountShift`, their count
  in the four bits above, and its top bit (`stringInlineShift`) set, which no length reaches, so the word is negative. Neither
  the owned bit nor the view bit is set, so its drop does nothing and its copy is its two words, as a literal's are, and its
  ASCII bit is exact. Every String of so few bytes is held so, and the empty String never is: literals, data constants, slices,
  joins and every String the runtime makes (`ownedString`, which frees the room of a short one) keep to that one form, so two
  Strings of which one is held in its words are equal exactly when their words are, and a short Map key is hashed and compared
  with no access to memory. A short literal is an address and two loads from a 16-byte constant of its own, and the fold pass
  makes the length of a literal a constant. Only a few accessors know the form: the length (`length.string`,
  `__Raw.stringLength`, `rt.str.len`: the count or the word shifted, chosen by `csel` on arm64 and `cmov` on x64), the
  byte at an index (the word shifted down by the index's bytes, with no call), and `strbytes` (`__Raw.stringBytes`), the address
  of the bytes, which writes a String held in its words to a frame box of the function that asks and gives that box: the
  runtime's byte loops read through it, and only `runtime/inline.maxon`, `runtime/characters.maxon` and a few entries read the
  words themselves (`__Raw.stringFirstWord`, `stringSecondWord`, `stringOfWords`). `rt.str.view` stays in place for a long
  view of a String that is not held in its words, and calls `runtime::stringView` otherwise. A join, an append and a
  concatenation of at most 15 bytes are made in the words with no allocation; a buffer that a join or an append grows is
  made with 16 bytes of slack past the bytes, so a part held in its words goes in as its two word stores. The drop of a String
  value tests the owned bit in place and calls the routine only when it is set.
- **The ASCII bit** of a String's word says that every byte is below 0x80 and none is a carriage return, which a line
  feed after it would join in one grapheme cluster: each cluster and each code point is then one byte. It is set only
  where the bytes are examined anyway: a literal's at compile time, a String made of bytes the runtime has just written
  (`runtime::stringOfRoom`: a `ByteArray`'s, a file read, a UTF-16 text converted), the join of two that have it, a slice
  of one that has it, and an integer's digits. A clear bit only means nothing is known. The text operations
  (`runtime/textops.maxon`) take their byte paths where it is set: the count of clusters, of code points and of UTF-16
  units is the length, a cluster's index and the position of the cluster n after another are arithmetic, and a walk over
  the clusters (trim and the iteration of characters among them) goes a byte at a time; the interpreter keeps no bit and
  always takes the general paths, which give the same answers. A build that counts its allocations
  (`--count-allocations`, so every run of the corpus and of the spec tests) checks the bit of each owned String it drops or
  copies (`Project.checksStrings`, `runtime::checkSimpleBytes`), so a bit set over another byte fails the program.
- **The heap** is `runtime/heap.maxon`, one per thread: a record that the thread's context names and its first alloc makes,
  so `runtime::alloc` and `runtime::free` take no lock and write no word that another thread writes (the process heap's lock
  serialized the workers, and its calls were about 40% of a build). A box of up to 8 KB is a slot of a size class (one per
  word up to 256 bytes, then four per power of two up to 8 KB), carved from chunks of the page heap that the heap's classes
  share (16 KB doubling to 1 MB), and a larger box is a run of the page heap of its own. A word before every box holds its heap's
  record, aligned to 512 bytes, plus its class's key, so `free` (which skips 0) finds both from the box alone and pushes the
  box's address on the class's stack, which `alloc` pops and zeroes; the stacks are apart from the slots, so an alloc reads
  nothing of a slot that may have left the cache. A behaviour may free what another worker allocated: a box of another heap
  goes on that heap's list of remote frees, linked through the dead box, and the owner takes the whole list with one
  exchange when a class of its runs out, as mimalloc's thread-free list works; only the owner takes, and takes everything,
  so no push meets a box taken and pushed again. The freeing thread gathers up to 64 boxes of one owner and pushes them with
  one compare-exchange, as snmalloc batches its remote frees, since a service's messages are allocated by the sender and freed
  by every worker that runs them, and a push per box had them all fight for the owner's word. A mask over the address would
  save the header word, but the page heap's runs are not aligned to a chunk's size, so a large box would carry up to a chunk
  of padding or every free a lookup in a map of the address space. A freed large box's run is kept by the freeing thread's
  heap (up to 32 runs and 512 KB, the 64 pages of Go's per-processor page cache, the one kept longest given back to the page heap first to make room, and all of them when the
  thread stops for want of work), and the shortest kept run that holds a large box and is at most a quarter longer serves it;
  so a program that grows and drops buffers takes no lock for each. A large box that grows (an array's, a byte array's or a
  String's buffer, by the one growth policy) takes the free pages after its run where there are some (`growInPlace`), so it is
  neither copied nor faulted in again; else it moves to a buffer that zeroes only what the copy does not write, as Go's
  growslice clears only the tail.
- **The page heap** is `runtime/pages.maxon`, Go's mheap without its spans, the same code on every system over three calls of
  the system's (reserve, commit, release: `VirtualAlloc` and `VirtualFree` on Windows, `mmap` and `madvise` on Linux, `mmap`
  on macOS): the one place memory comes from and goes back to, shared by the threads under a lock that only their slow paths
  take (a chunk, a large box that no kept run serves, a stack of a class, a block the runtime keeps). Its memory is arenas of
  64 MB of reserved address space, with a bitmap per arena of the 8 KB pages given out, those written since the system zeroed
  them, those the system holds no memory for, and those freed since the last release pass; a run is the lowest free one, so
  freed runs merge with their neighbours for nothing and are reused while still committed, and only its written pages are
  zeroed, as Go's needzero spares memory fresh from the system. Free pages go back to the system only by a release pass (Go's
  scavenger in its simplest form), at most once a second, from a free into the page heap and from the monitor's round: it
  gives back the free pages that were free at the pass before too, from the highest down, until the committed free pages are
  a tenth of those in use (Go's `retainExtraPercent`). A busy program keeps its free pages and faults none in again; an idle
  one gives them back within two seconds, so an idle service holds no more than it did.
- **Counting allocations.** The runtime knows the heap boxes that are allocated and not freed in every executable, so the
  runtime is the same code whatever the build: each heap counts the slots it carves and the large boxes it holds, less those
  it frees, and the live boxes are the sum over the threads' heaps less the slots on their stacks and lists
  (`runtime::liveBoxes`), so a box of a class costs no count; `checkLeaks` sums them when no behaviour is left.
  `--count-allocations` changes the entry stub, which then passes `main`'s exit code to the runtime's `checkLeaks`, and the drop
  and the copy of a String, which check its ASCII bit (see "The ASCII bit"): when
  the count is not 0 the executable writes `leaked allocations: N` to standard error and exits with 99, in place of `main`'s
  own exit code. `minimaxon test` and the program generator always use it, so every native run of the corpus ends with no box
  live, a literal not being one. A panic and a trap exit at once and skip the check, because they end the program with values
  still held; a run test that traps still shows no double free or crash, but not that it freed everything.
- **Depth.** A routine calls itself, or the routine of a part, once for each level of nesting, so dropping or copying a value
  nested a few million levels deep (a chain of that many nodes, each holding the next in an array) overflows the 256 MB stack,
  with a `panic: stack overflow` line. A routine keeps only the words it uses in its frame, to push that out.

### The interpreter

`self/interp` runs the IR of a whole program, with the primitives written in Maxon (an array; a frozen value and a cown, with
their counts) and a simulated scheduler of
behaviours, and tracks the heap as the memory model describes it. It is for tests and development: it is never part of a
compiled program, and the driver reaches it only from `minimaxon interp`, the test runner and `fuzz`. It has no types; it works
from the shape of the values and from the drops the IR says. `minimaxon interp <path>` lowers the project as a build does, with no
runtime module, and runs it (`self/driver/interp.maxon`); the program's
output and exit code are its own, a violation exits with 4 and a program that outlives the step limit with 5.

- **What it records.** Every heap value's owner (the place that holds it) and whether it was freed; every address with the
  version of its structure; and the borrows that a call is waiting to use. A place has one owner, so storing a value that is
  already owned is an error, and so is overwriting a value that the place owns. A frozen value and a cown have many owners: it
  records the places that hold one, and a count. A cell (a place), an object (a value), a frame and a thread are handles that
  index the columns of the `Vm`. A frame that returns gives its handle to the next call and its slots to the next frame of
  its function, so a run holds what its running calls need and not every call it made; an address of a slot holds the
  epoch of its frame (its call's number), so one taken in a frame that has returned is a stale borrow even once another
  frame holds the slot. The place of an element that an array gave up goes to its next element, and the places of an array
  that was freed to any array's; versions come from one counter, so an address older than its holder is a use after free.
  A freed object keeps its row, for the report of a use after the free.
- **Violations**, each reported with the function, the block, the instruction and what happened: a use after free, a double free,
  a leak (a value still live when the program ends, or a slot that owns a value when its function returns), use of a moved value
  (a slot that was moved out of or never assigned, read again), a value with two owners or one that is freed or returned while
  another owner holds it, a stale borrow (an element's address used after its structure changed), exclusivity (two arguments
  that reach one place where one of them can be changed, or a change to a place that a borrow a call is still waiting to use
  reaches), a proven access out of range, a read of an empty slot, three that only threads can break (below), and a deadlock. A violation in a program the checker
  accepted is a hole in the checker or in lowering. It stops the run; a leak is found at the end.
- **Proven accesses.** The expanded IR (`self/opt/expand.maxon`) reads an element or a byte with no `bounds` before it where the
  checker proved the index in range; the interpreter keeps the length test on that read (`ElemAddr`, `ElemLoad`, `LoadByte`),
  and an index that fails it is the violation `proven access out of range`, which names the access, the index and the count
  (`a read of an element at index 5, count 5`) at the function, block and instruction. Every read whose check came first
  never reaches it, so the violation is always a wrong proof. A read of an element of an array, which has no empty slot below
  its count (see "Where it differs from Maxon", the array's slot operations), has no test for one either, and its `loadat` or
  `elem` says so (` filled`) where an empty slot is told apart, a managed element or a String: one that finds its slot empty
  is the violation `read of an empty slot`, which names the read and its index, so the array's invariant is tested as a proof is. The test is the interpreter's own, so it costs a compiled program
  nothing. `test specs --interpret` (the gate's spec run passes it) runs each spec test that passes under the interpreter too, but for one that sets the processor count or the preemption (`<!-- procs: N -->`, `<!-- preempt: ... -->`), whose subject is the native scheduler, or one with a `<!-- native: reason -->` line (a compute-heavy library's, which the interpreter runs too slowly),
  compiled uncached since the cache keeps a library module as code, and fails it for a proven access out of range or a read of an empty slot alone: its output and exit code are the executable's to give, and the rules of the memory model are the corpus's to hold programs to. The interpreter
  acts in the directory it runs in, so the gate starts the run from a scratch directory
- **Threads.** `main` is the first thread; a `when` makes a behaviour that is queued on each cown it names, in the order
  behaviours were scheduled, and that becomes a thread once it is first in all of those queues. A thread is a stack of frames
  that a loop runs only while the scheduler has given it the turn, so exactly one thread executes at any moment and a run is
  deterministic for a **seed**: the scheduler picks the next thread at random among every thread that has started and every
  behaviour that can start, and a thread keeps the turn for a random number of instructions (up to 2, 8 or 64, chosen at random
  each time), so the schedules include interleavings at instruction granularity and long runs of one thread. What is simulated is
  the interleaving, which is where a data race would show. The seed draws from a PCG generator (`words.maxon`). The program ends
  when `main` has returned and no behaviour, coroutine or message is left, with `main`'s exit code; a trap in any thread ends
  it at once. `minimaxon interp --seed=N` runs the schedule of seed N, the test runner seeds 0 up to 20 (`// schedules:`).
- **Strands, timers and services** (`strands.maxon`), as the runtime runs them. An async call makes a coroutine, a thread of
  the caller's strand (the first makes the caller the strand's root): a strand's members run one at a time, only the one
  that holds it takes turns, and it gives the strand to the member that waited first only where it waits, yields or
  finishes, never where its burst ends, so async code is never interleaved with itself while separate strands (main, each
  behaviour) are. A wait (an await, `awaitAny`, a sleep, the wait of a drop that cancelled a promise) parks the thread at its
  instruction, which runs again once the thread is readied and holds its strand. A promise is an object whose contents are
  a row of the Vm: what the coroutine's function or the message's handler gave, which the await takes, and its cancel,
  which keeps a coroutine that has not started from calling its function and ends the sleep it is in. A service is a cown
  with the runtime's gate (capacity, the messages sent and not started, closed), and its messages are behaviours. Time is a
  **simulated clock**: a sleep sets a timer on it, and when no thread can run the scheduler moves the clock to the earliest
  timer, so a run of sleeps takes no time and wakes threads in the order of their deadlines (those due at once in the order
  they slept). A timer that posts a message (`Timer`, `Ticker`) is set on the same clock, and a posted message is a behaviour of
  its service that no send queued, queued by its timer's fire, a cancel or a watched service's end (`posted.maxon`): its start
  takes no place in the gate, a mark counts it while it is queued, and a cancel, a reset or a rearm drops a fire that is queued
  and has not started. Timers due at once fire in the order they are due, then in the order they were armed. Members of one
  strand reach each other's data, and a coroutine reaches the cowns its root's behaviour holds.
  When no thread can run and no timer is set while a thread waits or a behaviour is queued, the run is a **deadlock**, reported
  as a violation that names each waiting thread and where it waits. The tests are in `strands.maxtest`.
- **Who owns what.** A value belongs to the thread whose frame holds it, to the cown whose value it is part of, or to a frozen
  value, found by following owners up to a slot, the value of a cown or the contents of a frozen value; a value that nothing
  owns yet belongs to whoever holds it. Touching a value that belongs to another thread, or to a cown while the thread has no
  behaviour that holds the cown, is a **data race**; changing anything that is part of a frozen value is a **change to a frozen
  value**; and a frozen value or a cown whose count is wrong is a **reference count** violation (freed while a place or a waiting
  behaviour still holds it, or lowered below 0). One that is still live at the end is a leak, as any value is. Handing a
  behaviour a value that its scheduler still owns is a data race at the `when`. None of this is tracked until the first `freeze`,
  `cown` or `when` runs, so a program without them pays a branch.
- **The race oracle** (`race.maxon`) checks the checker's claim that an accepted program has no data race inside one owner:
  a parallel loop's body writes only its own element, and a value lent to an async call is not used before the await. It
  is a vector-clock detector (FastTrack's): every read and write of a place by an instruction, and every change and read of
  an array's structure, is attributed to the thread that made it, and two accesses of one place by different threads, one
  of them a write, with no ordering between them, is a failure, which the run makes an internal error naming the place, the
  two threads, the instruction and the seed. What orders them is a fork (an async call, a loop's worker), after what its
  maker did before it, and a join (the await, take or drop of a coroutine's promise, the end of a loop), after which its
  maker has what the joined thread did; the handing over of a strand is none, so a caller that writes a place it lent
  before the await races with the coroutine although the two never run at once. A thread, its coroutines and its workers
  are one family; what families share is the domain rule's above, so an access from another family starts a place's
  history again, and a family whose forks are all joined records nothing. It costs a run that forks about a tenth of its
  time (a parallel loop over a large array: 690 ms to 740 ms) and nothing otherwise, so the test runner, the spec run, the
  oracles of `fuzz` and `fuzz --services` run it and `interp` only with `--race`. The tests are in `race.maxtest`, on
  traces of accesses and on IR that uses a lent place before its await.
- **A cown's destructor.** A count that reaches 0 can happen deep inside freeing a record or an array that holds the reference,
  where a call cannot be made. The interpreter takes the value out of the cown, frees the cown, and appends (destructor, value) to
  a list; once the instruction that dropped has finished, the thread that ran it calls the destructors in list order, which is
  the order of the frees, so it is deterministic (`interp --frees` lists them, on standard error).
- **Traps** behave as the executable does: `rt.panic`, an index out of range, a stale slot key, a collection ended with `dropEmpty`
  while it holds an element and a behaviour that names one cown twice write to standard error and exit with 1; the same
  standard output and the same exit code. A program that uses what the native runtime does and the interpreter does not model
  (an array's capacity, deleting a directory, a child's input) ends the run as `unmodelled`, which `interp` exits with 6 and
  the corpus fails, and the spec run leaves as it is. A trap is not a violation (an index that a check rejects is a trap; one that no
  check guards is a violation, above). A violation, a trap and a timeout end the run through a
  `Halt` that every function of the run throws.
- **What it does not run.** An operation the interpreter has no model of stops the run with a message that names it and says to
  mark the program `// native-only`, and never gives a result: `Directory.delete`, `Subprocess.runWithInput`, the process's
  environment entries, a change of the process's priority, the debug port, the subprocess rows of the raw surface, and **file
  streams** other than the one that a program makes and writes: the rows of an open file that the library's `__ManagedFile` is written over (`__Builtins.fileOpenRead`,
  `fileOpenWrite`, `fileOpenWriteExecutable`, `fileReadAppending` and `fileSync`, `self/interp/handles.maxon`), and so the library's
  `FileWriter.open` and `File.openRead`, since its file layer reads and writes whole files and holds no descriptor. It runs the
  rows of a file that a program makes and writes, `fileOpenCreateNew`, `fileWrite` and `fileClose`, so that `FileWriter.openNew`,
  `writeAllText` and `File.createText` run: the open makes the file where none is there (the check and the write are one step, as
  the interpreter runs one function at a time; a process outside the program that makes the file between them is not excluded, as it
  is by the compiled open), and a write adds to what the file holds; and **sockets**, the rows of a TCP socket that the library's `__ManagedSocket` is written over
  (`__Builtins.socketListen`, `socketConnect`, `socketAccept` and the rest, `self/interp/socket.maxon`). It stops at the operation and not before the run, since a module is lowered whole and a program that never opens
  a file may hold code that does.
  `fuzz` generates none of these: its programs are drawn from `self/fuzz/library.txt`, which names none.
- **How the tests use it.** `minimaxon test` runs every program under it, under 20 schedules when the program schedules
  behaviours, and requires the same stdout and exit code as the directives and no violation. `self/interp/interp.maxtest` tests it on
  IR that is written to break each rule (and to schedule behaviours: their order, their interleaving, the counts). `minimaxon fuzz` (`self/fuzz`) writes random well-formed programs over records, unions,
  strings, the four conventions, calls with overlapping and disjoint arguments, match bindings, loops, generics with bounds,
  arrays, lists through maxon's value API (and a field of an element that get lends, written with a try before the target, often a byte-sized Small, and read again after the list changes shape), slot maps with slot keys, a borrowed place held while a later operand of the same expression changes or
  consumes what it lies in, and frozen values, cowns and behaviours. The checker refuses most of them, since the generator leans
  on the edges of the rules: about two programs in five are careless and break rules throughout, and two in five of the rest
  break exactly one rule of sharing, once (a behaviour that takes a borrow, a variable used after a behaviour took it, a frozen
  value changed or taken apart, a cown read outside a behaviour, a borrowed place held across a later operand), so that a rule
  the checker lacked would be all that stands between that program and the interpreter. Every program it accepts must run
  clean, under several schedules when it schedules a behaviour, and one that does not is a hole. Each accepted program is
  also checked again with a shape planted at the start of main (`self/fuzz/planted.maxon`): a read of a place followed,
  in the same expression or statement, by a call that moves or replaces it while the read still holds it (an interpolated
  part behind text, after another part, nested, of a field, an element or a `FilePath`, or appended to; a compared
  operand; an argument; a receiver; a subscripted collection; an exchanged place; a match binding; a loop's collection).
  The checker must refuse it with an ownership error on the shape's lines, and a planted program it accepts is a hole. A program of `fuzz --proofs` has none planted, since its library has none of the calls a shape makes. A program is a pure function of
  its seed; the generator draws from `self/fuzz/rand.maxon` and the generic library every program starts with is
  `self/fuzz/library.txt`. The generator writes a `try` on every piece of arithmetic, and one that the analysis proves
  cannot fail is `E3944`, which is no fault of the program: a program refused for that alone runs after `fix` removes those
  `try`s and nothing else. The command runs its seeds in worker processes, loads each program from memory
  (`project.loadSourceInto`), and with `--native` also builds each accepted program that has an executable, with its
  allocations counted, and compares it with the interpreter's: the same output and exit code, and no box left live.
  The programs with no slot map, which are the ones with an executable, share in half the seeds, and those are
  quiet: only main's own statements print (a print anywhere else only evaluates its string) and only they schedule
  behaviours, so the behaviours on each cown run in main's order and the output and the exit code are the same under
  every schedule, the executable's and the interpreter's alike. Such a program's executable runs 3 times, under the
  worker count of the fuzzer's environment; when the run traps, a behaviour may have trapped while main was still
  printing, so the two outputs need only agree up to where the shorter ends. A program that schedules behaviours and is
  not quiet is not compared.
- **`fuzz --proofs`** writes another kind of program (`self/fuzz/proofs.maxon`, over the library `self/fuzz/proofs.txt`): a few
  scenario functions that read and store at an index of an array (of numbers and of strings, in a record two deep, and a
  `FixedArray`) under guards, in loops over a count or a bound, in `while` loops that step or drain, some of them requiring
  that the index stays at most its bound, or below a `var` bound that only the invariant keeps at most the count, with a
  branch that moves the bound or changes the array and continues, and at midpoints of two places with and without a guard, each mixed, at the points
  where a fact was just stated, and reads of an end (`first`, `last`, `pop`, once or twice, under guards such as
  `xs.count() >= 2`), with everything that must make the analysis forget it: a built-in that changes the count
  (`pop`, `remove`, `clear`, `truncate`, `push`, `insert`, `set`), an assignment of the array or of the index, a function or a
  method of the record that changes the array, a closure that changes the array or the index, passed, bound or nested, early
  exits, bounds from another array's count, and divisions and conversions under the same guards. Each access is written with
  its own handler (`try xs.get(i) otherwise 7`) so that a program the analysis does not prove still runs, and the compiler's own
  `fix` pass then removes the handler wherever the analysis proves the access (E3944), and an invariant wherever it cannot
  prove it (E3945), so the generator needs no model of what
  the analysis proves. A read or a store at the index of a fact on its array is sometimes a subscript instead, which has no
  handler: a proven one runs with no check, and one the analysis does not prove traps outside. It stands under a guard
  `within(i, count: xs.count())`, a call the analysis does not look into, so it runs only inside and is proven by the facts
  around it alone, and the other arm makes the same access with its handler, so a wrong proof is still caught there. About
  3% of programs end, after their checksums, in an unproven subscript outside its array (a trap that the executable must end
  with too). Each program prints a checksum; an access the analysis proved that the interpreter finds outside its array
  (`proven access out of range`), or an executable whose output is not the interpreter's, is a failure, which names the seed and
  keeps the program under `--keep=DIR` (the compiler's temporary directory, in `minimaxon-fuzz-proofs`, without it). The checker
  may refuse a program for what it knows of the code (a statically known index outside a `FixedArray`, a divisor that is 0), up to
  a quarter of the programs; any other refusal is a hole in the generator. The run fails when the analysis proves fewer than one
  access in eight, which says that the generator no longer writes what the analysis can prove. A run on
  an analysis that does not forget a variable that a closure captures to change it fails within the first ten seeds, and so
  does one whose `pop` keeps the count.
- **`fuzz --concurrent`** writes programs over what the race oracle and the oracles of backpressure check
  (`self/fuzz/concurrent.maxon`, over the library `self/fuzz/concurrent.txt`): a few scenario functions, each a fold of a few
  pieces into one number that main prints, the pieces parallel loops of the three forms whose bodies read what is around
  them, write their own element or call a mutates helper with it, end an iteration with `continue`, send to a service,
  await a coroutine of their own, lend a copy of their element to one, or hold a loop of their own; loops over a grid of
  rows; a helper whose loop throws at an index or where an element passes a limit, whose lowest index's error is folded in;
  async calls that lend a local cell and a number to be changed, or the cell to be read, with something done before the
  awaits; and a service whose handler runs a loop of its own over its state. Every program is written to be accepted once
  `fix` takes out the tries the analysis proves needless, so any other refusal is a failure, and to give one output under
  every schedule and worker count: a loop's results land in its own elements, read in index order; a loop's error is its
  lowest index's; and the service's state does not depend on the order of its messages. Each runs under six schedules of the
  interpreter (whose loops take one to four workers as the seed says), whose outputs must agree, under the race oracle and
  the oracles of backpressure, and with `--native` three times as an executable, under the worker count of the fuzzer's
  environment, whose output must be the interpreter's. The gate runs 40 programs (`--count=40`); the wide run,
  `fuzz --concurrent --native --count=2000`, takes about 14 minutes, and is repeated under `MAXON_MAX_PROCS` of 1, 2, 4 and 8.
- **`fuzz --services`** is the brute force of backpressure (`self/driver/services.maxon`): every send graph of one to four
  services that service 0 reaches, a service's edge to itself and cycles included, one of each class that renaming the others
  gives (6,898), runs `self/fuzz/services.txt` under the interpreter, which reads the graph from its standard input: main
  starts six walks at service 0, and a service passes a walk with hops left (four) on along each of its edges. Each graph runs
  under the seeds 0 to 3, one for each turn of 1 to 4, and once more with main stopping one service after its first start,
  so later sends to it fail "stopped"; the interpreter's oracles of backpressure check each run (see "Services"). All of them
  take about four minutes (34,490 runs, ten million messages); the gate runs 200 graphs spread evenly over them
  (`--count=200`).

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
first the `fixedSlots` that selection gives the IR's slots and frame boxes, and then the spill slots allocation adds (a word for a general register's value, and for an xmm register's two words, 16-aligned on arm64, which hold a vector whole). Each
block has a loop depth and a likelihood (`cold` for a failing check) for the passes that will read them. A virtual register's class
(`RegClass`: `gpr` or `xmm`) is in `classes`, from `firstVirtual` (64) on;
an operand below that is a physical register by its number in the register file, which is how selection fixes a value to a
register: it copies the value into, say, rcx before a shift by `cl`, and the allocator sees the fixed register at the copy.

An instruction (`MInstr`, 32 bytes when packed: kind, reference kind, form, condition, three register operands `a b c`, a
32-bit `aux` and a 64-bit `imm`) is a form of the function's architecture (`MKind.target`, its `FormID`; `MirFunction.arch`) or a pseudo-instruction: `copy`, `parallelCopy` (a range
of `copies`, every source read before any target is written), `spill` and `reload` against a spill slot, `restore` (the end of a bracket that a `spill` of the same register opened:
the value comes back into its own register, which the instructions between were lent), and `frameSetup`
and `frameTeardown`. A form's register operands are in one order: the register it defines, then the one a two-address form
reads and writes (which the definition must be given the register of: `reuses`), then the ones it reads. `aux` is a
memory operand's displacement, or what the reference of a form's last 32-bit field names (`MRef`: an edge, a symbol of the
function's `symbols`, a data offset, an import), which the encoder turns into the same label uses and fixups as the rest
of the backend. `machine.maxon` holds the constraints the allocator reads, never the encodings, over the tables of every
form by FormID and of every architecture (`machine_gen.maxon`): `operandCount`,
`defines`, `reuses`, `implicitUses` (a form's fixed uses, and the result registers at a return) and `implicitDefs` (fixed
results, clobbers and a call's convention), the register file's masks by architecture (`classMask`, `allocatable`, `reservedMask`) and the
calling conventions of the target compiled for (`Conventions.make(target)`, each a `CallConvention`: what a call keeps),
built from the target table's declarations (`regs_x64_gen.maxon`, through `x64KeptBy`): the internal one, the system's host
one for an import (Win64) and its system call's for a `syscall` (Linux's), whose form is the call of that convention
(`isKernelCall`, `callConvention`). A call's `imm` is the
registers its arguments are in, which `implicitUses` counts as read by it (`newCall`), and a return reads the result
register of its function's class (`MirFunction.floatResult`).

A build that writes debug info gives a `MirFunction` a column of spans beside its instructions too (`MirFunction.spans`; see
"Debug info").

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

**arm64's frame** (`encode_arm64.maxon`) is maxon's: every function but a leaf that needs none has a frame record, `stp
x29, x30` and the frame pointer pointed at it (a leaf with no slots, spills, callee-saved registers, stack arguments or
calls, that names neither register, keeps both in their registers and has no prologue, as AAPCS64 allows and as x64's
leaf has no frame); below it the room for the arguments its calls pass past the registers (`MirFunction.outgoingBytes`, which
selection grows), at the stack pointer, where AAPCS64 has a callee find them; above it the slots, the fixed ones and then the
spill slots, at `[x29 + 16 + slot*8]`, and above them the callee-saved registers the function's values were given. The
stack pointer does not move between the prologue and an epilogue. A function that takes arguments on the stack has its
prologue leave their address, its caller's stack pointer, in `x16` (`MirFunction.incomingStack`), which the loads of them
right after it read, since no displacement from the frame pointer is known before the frame is. A frame of more than a page
touches each page on the way down, and an offset past a scaled field goes through `x17`. A `b.cond`, `cbz` or `cbnz` reaches
1 MB either way: one whose block is farther is left unpatched (`Asm.farBranches`), and the function is written again with
each such branch the inverted branch over a `b`, until every branch reaches, as maxon relaxes them. A function under 1 MB is
written once.

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
colouring then finishes and which run: those functions are x64's code, so on a host of another architecture they are linked
into an x64 Linux executable and not run.

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
  whatever the program does, so that spill code costs nothing the call does not and is no reason to refuse; (4) on a hot
  path, at a point over its class's whole pool, the values the optimizer made live there (`MirFunction.lengthened`, from
  `ir.IrFunction.lengthened`: moved out of a loop by code motion, or kept by elimination for a later computation merged
  into it) are split, farthest next use first, which reloads them before their uses in the loop: the program as written
  computed each there, so a hoist or a merge that does not fit is undone in that loop alone and no reason to refuse; (5)
  the E5001 test, which the caller may skip (`HotLoops.spill`) where it knows that the loop as the program wrote it fits
  (see "E5001 and the optimizations"), or ask for alone (`HotLoops.test`); (6) everywhere, a point still over its free registers spills by
  farthest next use, constants first, then values already stored or defined outside the loop; in a cold block, a value
  live across a call, or one the loop's hot path reads, is bracketed instead: stored before the point and restored into
  its own register after it, which lends its register to the instructions between, so the hot path keeps it. A bracket
  whose value nothing reads after its restore (a split whose cut lies past it reloads the value at every later use) is
  dead, and each round's analysis deletes it before the round goes on.
- **A split** stores the value once, after its definition, and gives each use the cut reaches a reload (or the constant
  made again), one per block and stretch of uses, hoisted to the preheader of every loop that holds neither the cut nor
  the definition. A reload defines a new value that dominates its uses, so the function stays SSA with no new
  parameter. The work is in rounds of analysis and one sweep each; an analysis is reused while nothing changed, and a
  function that fits costs one. A round's analysis after the first starts from the one before, since edits change only
  instructions within blocks: the graph is kept and only the block of each instruction found again; the operands of an
  instruction no edit touched, and the live blocks of a value whose definition and uses no edit touched, are taken over;
  and only a block whose instructions or live-out values changed is surveyed for pressure again.
- **The invariant** (`verifyPressure`, which `--verify-ir` holds every function to): every virtual register has one
  definition; at every instruction each class's demand fits its free registers; no bracket is dead; and spill code in a
  hot block lies only in loops that define or read its value (a slot is one value). So a store runs where the definition
  does and a reload no deeper than its uses.
- **E5001** (`refuseHotLoops`, a `PressureRefusal` per loop nest and class, which the emit stage reports as a compile error
  at the function's declaration, since the IR keeps no place of a loop, and an instance's at its generic's): after tiers 1
  to 4, a point of a loop's hot path over its class's whole pool. It names the loop,
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
  instruction reads but the dying reused operand's, and a copy of the operand before it, which pressure has counted beside
  the dying operands. The dying operand's register stays one to start in when the operand was in a window there and another
  value has it as its home: the definition then gets a home of its own and starts in a window.
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
| `__Raw.loadAcquire(address, offset: o)`, `__Raw.storeRelease(address, offset: o, value: v)` | the word at `address + o`, read as an acquire, or `v` written there as a release: no access after the acquire is done before it, and none before the release after it, so a thread whose acquire sees a word another thread released sees what that thread wrote before it; the hand-over of a word that threads share without a lock or an atomic operation |
| `__Raw.wrappingAdd(a, right: b)`, `__Raw.wrappingSub(a, right: b)` | address arithmetic: the sum or the difference modulo 2^64, unchecked |
| `__Raw.mulHigh(a, right: b)` | the high word of the 128-bit product of two words of no sign: one `mul` on x64, `umulh` on arm64 |
| `__Raw.vectorLoad(address, offset: o)`, `vectorStore(address, offset: o, value: v)`, `vectorZero()`, `vectorOfWords(low, high: h)`, `vectorWord(v, lane: n)`, `vectorXor`, `vectorAnd`, `vectorOr`, `vectorAddLanes32(a, right: b)`, `vectorShuffleBytes(v, picks: m)`, `vectorShiftBytesUp`/`Down(v, bytes: n)`, `vectorShiftLanesLeft`/`Right(v, bits: n)` | the 128-bit vectors, of the built-in type `__Vector128`, which only the runtime module names: 16 bytes at any address, byte i of a vector the byte at `address + o + i` and lane 0 its low word; the bitwise operations, the sum of each 32-bit lane, the bytes of `v` that the bytes of `m` pick (one below 16 picks, one of 128 or more gives 0, and `m` holds no other), and the shifts of the whole vector by 1 to 15 bytes toward its high or low end and of each 64-bit lane by 1 to 63 bits, `n` a constant. Each is one IR vector operation (`VecLoad` ... `VecShift`) on a register of kind `vector`, in an xmm register on x64 (SSE2 to SSE4.1) and a q register on arm64; a vector is a local only, never a parameter, an argument, a result, a field or an element (E3965), and is held across no call: one still to be read after a call the function makes is `E5900` at the function, which instruction selection finds by the liveness of the vector registers of the IR it is given (`backend/vectorcalls.maxon`), since the switch of a green thread keeps only the low half of each vector register that arm64's convention has a call keep. `tests/runtime/vectors.maxon` holds each to its words |
| `__Raw.vectorShuffleDwords(v, order: o)`, `vectorAlignBytes(high, low: l, bytes: n)`, `vectorBlendWords(a, right: b, mask: m)`, `vectorAesEncrypt`/`EncryptLast`/`Decrypt`/`DecryptLast(state, key: k)`, `vectorAesInverseMix(v)`, `vectorClmul(a, right: b, select: s)`; arm64's `vectorAese`/`Aesd(state, key: k)`, `vectorAesmc(v)`, `vectorSha256H(abcd, efgh: e, words: w)`, `vectorSha256H2(efgh, abcd: a, words: w)`, `vectorSha256Su0(first, second: s)`, `vectorSha256Su1(first, third: t, fourth: f)`; x64's `vectorSha256Rounds2(state, other: o, words: w)`, `vectorSha256Message1`/`Message2(a, right: b)` and `cpuid(leaf, subleaf: s, register: r)`; `cpuFeaturesAddress()` | the shuffles by a constant (pshufd's 32-bit lanes, lane i picked by bits 2i and 2i + 1 of `o`; the 16 bytes from byte `n`, 1 to 15, of `l` and then `high`, palignr's and ext's; pblendw's 16-bit lanes of `b` where a bit of `m` is set), on arm64 by dup, ext or tbl and by eor and and with the mask's lanes; the AES rounds as x64's instructions do them on both targets (arm64: aese or aesd with a zero key, aesmc or aesimc, and the key's eor), InvMixColumns, and the carry-less product of the 64-bit lanes that `s` picks as pclmulqdq's immediate does (0x00, 0x01, 0x10 or 0x11; pmull, pmull2, with ext for a mixed pair). The instructions of one target are refused by the other back end, so the runtime's source writes them under `#if arch(...)`: arm64's AES rounds, which take the key first, and the SHA-256 instructions of each (x64's sha256rnds2 reads its third vector in xmm0, and arm64's sha256h, sha256h2 and sha256su1 theirs in v0, which instruction selection copies it into), and x64's cpuid, the word of register `r` (0 eax, 1 ebx, 2 ecx, 3 edx), a constant. `cpuFeaturesAddress` is a word of the data, 0 until the runtime writes what it found of the processor's features there. `tests/runtime/vector-shuffles.maxon` and `vector-crypto.maxon` hold them to their definitions, FIPS-197's blocks, a GCM multiply and FIPS 180-4's "abc" |
| `__Raw.addressOf(word)` | the address of a local `Count`, passed `mutates`: an out-parameter of a DLL function |
| `__Raw.stringBytes(text)`, `stringLength(text)`, `stringAscii(text)` | the address of the first byte of a borrowed `String`, its length in bytes, and whether each byte is known to be below 0x80 (false where nothing says so) |
| `__Raw.stringRoom(capacity)`, `__Raw.freeStringRoom(bytes)`, `__Raw.stringOf(bytes, length: n, ascii: a)` | room for that many bytes that a `String` will own, zeroed; its free when no `String` took it; the `String` that owns the room, of its first `n` bytes |
| `__Raw.stringSize()`, `__Raw.storeString(address, offset: o, bytes: b, length: n, ascii: a)`, `__Raw.stringBytesAt(address, offset: o)`, `stringLengthAt(...)` | the bytes a `String` takes in memory (an element of `Array with String`, a slot an entry fills); the `String` that owns room stored there; the first byte and the length of a `String` held there, which stays there |
| `__Raw.heapAddress()`, `stdoutAddress()`, `stderrAddress()` | the address of a global of the entry stub |
| `__Raw.callImport0(name)` to `__Raw.callImport10(name, arg1: a, ..., arg10: j)` | calls the library function `name`, a string literal (`E2004` otherwise), with that many words by the host convention, and gives what it returns: a kernel32 function on Windows, which the executable then imports, a libSystem function of `macosImports` on macOS |
| `__Raw.syscall0(number)` to `__Raw.syscall6(number, arg1: a, ..., arg6: f)` | makes the system call of the number with that many words by Linux's system call convention, and gives what the kernel returns, `-errno` as a word when it failed; Linux only |
| `__Raw.processStartAddress()` | the address of `g.processStart`, where the Linux entry keeps the stack pointer the process started with: the argument count, the arguments, a null, the environment |
| `__Raw.atomicAdd(address, offset: o, value: v)`, `__Raw.atomicExchange(address, offset: o, value: v)` | adds `v` to the word at `address + o`, or puts `v` there, as one locked step (a full fence), and gives the word it found |
| `__Raw.compareExchange(address, offset: o, expected: e, desired: d)` | puts `d` in the word at `address + o` when `e` is there, as one locked step, and gives the word it found: it succeeded when that is `e` |
| `__Raw.spinPause()` | `pause`, in a loop that waits for another thread |
| `__Raw.copyBytes(source, target: t, count: n)` | copies `n` bytes from `source` to `t` with the copy routines' copy (`rCopyBytes`: Go's memmove plan on arm64, `rep movsb` on x64), which the entry stub's `rt.copyBytes` calls by the internal convention. The contract is the caller's to keep, and nothing checks it: `n` is above 0, so a caller that may have nothing to copy decides so with an `if` of its own and does not call; and the two places do not overlap, so it is no move within one buffer: an array's gaps move with the runtime's loops `copyForward` and `copyBackward`. The runtime's `copyBytes` calls it for a copy of more than 64 bytes (`longCopyBytes`) and runs `copyForward` for a shorter one, which a call would only slow (`tests/runtime/raw-copy-bytes.maxon` tests it) |
| `__Raw.threadContext()` | the running thread's context: the word in the runtime's thread-local slot |
| `__Raw.callCode1(code, arg1: a)` | calls the code at the address `code` with one word by the internal convention, as a behaviour's thunk is called with its record, and gives what it returns |
| `__Raw.callHost0(code)` to `__Raw.callHost11(code, arg1: a, ..., arg11: k)` | calls the code at the address `code` with that many words by the host convention, Win64, as a DLL import is called: a function of a library that the import table does not name, which the runtime found by its name (`LoadLibraryA`, `GetProcAddress`); Windows only |
| `__Raw.threadStartAddress()` | the address of `rt.threadStart`: on Windows the code a thread that `CreateThread` makes starts at, and on Linux the routine that clones the worker's thread (see "Threads") |
| `__Raw.codeAddress(name)` | the address of the code of the function of the module that `name`, a string literal, names (`E2004` otherwise, and `E3004` when there is none): what a `callCode1` calls, as a test of the scheduler builds a behaviour's `run` |
| `__Raw.stackSwitchAddress()`, `greenStartAddress()`, `asyncPreemptAddress()` | the address of the entry stub's `rt.stackSwitch`, `rt.greenStart` and `rt.asyncPreempt` (see "Green threads") |
| `__Raw.preemptHandlerAddress()`, `signalReturnAddress()` | on Linux, the address of the stub's handler of the preemption signal, `rt.preemptHandler`, and of the restorer it returns through, `rt.sigreturn` |
| `__Raw.preemptibleCodeAddress()` | the address of the link's table of the code a green thread may be stopped in, `g.preemptibleCode` (see "Green threads") |
| `__Raw.calleeStackDepth()` | the most of the stack that the calls of the function it is written in may take below its stack pointer: a read-only word of the data that the link fills in, from the stack each section of code says it takes (`CodeSection.stackBytes`: a compiled function's frame and what it pushes for a call, a stub's own) through the sections its symbol fixups name; a call that recurses has no bound and fails the link |
| `__Raw.wipeStack(bytes, floor: f)` | `rt.stackWipe`: zeroes the vector registers the internal convention lets a call change, then the words below the caller's stack pointer from the top down to the higher of `f` and that pointer less `bytes`, so that `wipeStack(calleeStackDepth(), floor: ...)` leaves nothing of what the calls before it kept in vectors or frames; AES-GCM's seal and open end with it, floored at `stackFloor()` (a green thread's stack above its guard page, else 0) |
| `__Raw.storeVectorRegisters(at)` | `rt.vectorRegistersStore`: stores the vector registers the internal convention lets a call change at `at`, sixteen bytes each in the order of their numbers, and gives how many; a test reads through it that a wipe zeroed them |

Only the runtime module may call them: a call anywhere else is `E3152` (maxon's `rawIntrinsicOutsideRuntimeTier`, with
its message), which `callFunction` in `self/check/calls.maxon` raises from the rows' runtime-only flag
(`ast.intrinsicRuntimeOnly`). They lower to IR operations that only the runtime's code has: `loadraw.W`
and `storeraw.W`, W the width in bits, at the address plus the offset; `globaladdr` of an `Rt` global; `import`,
which names the DLL function among the function's strings; `syscall`, a call of the system call convention; `loadacquire` and `storerelease` of the word at the address plus the
offset (a plain `mov` on x64, whose loads are acquires and stores releases, and `ldar` and `stlr` on arm64, which
are sequentially consistent between themselves, as Go's `atomic.Load` and `Store` are, though a plain load after a
`stlr` may be done before it); `atomicadd`, `atomicxchg` and `cmpxchg` at the address plus
the offset (`lock xadd`, `xchg`, `lock cmpxchg`; LSE's `ldaddal`, `swpal` and `casal` on arm64); `spinpause`; `threadcontext`, which on Windows reads the slot straight from the
TEB (`mov rax, gs:[rcx + 0x1480]`, `rcx` the slot's offset in `g.tlsOffset`) and on Linux the word that `fs` points at
(`mov rax, fs:[0]`), maxon's thread pointer; `callcode`, an indirect call; `callhost`, an indirect call by the host convention, which
`emitHostCall` sets up as it does an import's; and `copyBytes`, a call of the entry `rt.copyBytes`. Each
ordered access, atomic operation and `spinpause` reads and writes memory, so no pass moves a load or a store across one;
a plain `loadraw` or `storeraw` is ordered on x64 alone, so on arm64 a word that threads hand over without a lock
needs an ordered access. The interpreter never runs the
runtime's source, so it panics on each of them. The wrapping arithmetic is an unchecked `bin`, `addressOf`
the `addr` of the local's slot. The String operations after them
say nothing of a String's layout, so one runtime source serves a compiler of either layout, the seed's among them (the
seed compiles `runtime/` with its own layout when it builds C1): each lowers to the loads, stores, `rt.alloc` and
`rt.free` of the layout of the compiler compiling it, here two words (`strptr` the bytes, the length and `stringAscii` read
from `strword`, room an `rt.alloc` that `strmake` makes an owner of, `stringSize` two words). `self/backend` writes them
(`visitLoadRaw`, `visitStoreRaw`, `visitGlobalAddr`, `emitSyscall`, and `emitCallImport`, which aligns the stack itself, since a Maxon
function's frame is not aligned, and keeps `rsp` in a value that allocation puts in a register Win64 keeps).

**From an entry to a function.** The runtime entries the compiler calls are the rows of the table's `entries`
(`ir.Rt`), and `runtimeFunctions` names the function of `runtime/` that defines each one with its signature
(`alloc(bytes Count) Count`, `write(handle Count, text String)`, ...). From it the generator writes `ir.rtFunctions` and
`ir.rtSymbol`, which gives a mapped entry its function's link name (`runtime::alloc`) and an entry that
the stub or the linker defines its own symbol (`rt.entry`, `g.heap`); the linker resolves `rtSymbol(x)` by that text as
it does any call. The two calling conventions are one by construction: a `callruntime` and a `call` of a Maxon function
both pass their arguments through the architecture's call (`Selector.callSymbol`, on x64 `callForm` of
`self/backend/isel_x64.maxon`) by the internal convention, a
`callruntime` all of them as words (`Selector.callWords`), and the callee returns a word in `r8`. So a runtime function
takes only words (an integer, a `bool`, a `String` as
its box address), takes none `mutates`, is not generic and throws nothing, since a function that throws returns its
error in `r10` too. `minimaxon check runtime` holds the runtime to the table: each
mapped function exists with exactly that signature (`E3004` when it is missing, `E3005` when it differs) and links as
`rtSymbol` says. A new entry is a row of each table and a function in `runtime/`.

**Globals.** The entry stub keeps the process heap and the two standard handles (`g.heap`, `g.stdout`, `g.stderr`; the runtime's heap uses no process heap, but the backend's test runtime does),
which it sets before `main`, on Linux the standard handles, fds 1 and 2, and the stack pointer the process started with
(`g.processStart`), and no heap. Runtime source reaches a global through its address intrinsic.
The runtime is the same code whatever the build: each thread's heap always knows its live boxes, and `--count-allocations` changes only
the stub and the String routines (see "The ASCII bit"), which then passes the exit code to the runtime's `checkLeaks`, which sums the live boxes of the threads' heaps;
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
switch record of its scheduler, Go's g0, and the room its clock and its take of events go through), the pair of switch records a
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

**The scheduler** (`runtime/sched.maxon`, `green.maxon`, `monitor.maxon`, `timers.maxon`, `eventqueue.maxon`) is Go's: green
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
  worth of threads spin), and then gives its processor back and stops on its event, or waits in the event queue when a timer
  or an event entry is pending. A green thread readied while a processor is idle and no thread spins starts one.
- **Stacks.** A green thread takes a stack from a pool when it first runs and gives it back when it finishes: a reserve of
  256 MB that the system commits a page at a time, with a guard page, never moved. A thread runs one by `rt.stackSwitch`
  from its own stack (its scheduler's, Go's g0), and the green thread comes back by the same switch with what it asks: to
  park, to be requeued (a yield or a preemption, onto the global queue as Go's are), to wait for a processor it lost in a
  blocking call, or to finish, which its thread's scheduler then does on its own stack.
- **The monitor** (Go's sysmon) is a thread of its own that holds no processor and allocates nothing. It wakes every 20 µs
  to 10 ms, the sleep short again only after it took a processor (Go's sysmon), and on Windows sleeps on a high-resolution
  waitable timer as Go's does, since `Sleep` counts in the system's 15.6 ms ticks; asks a green thread that has run 10 ms on
  one schedule tick to stop, and stops its thread from outside, on Windows by
  `SuspendThread`, `GetThreadContext` and `SetThreadContext`, on Linux and macOS by SIGURG, which every thread that runs
  green threads unblocks for itself as it starts (Go's `minitSignalMask`), when the thread is in the program's code,
  on the green thread's stack and not in a blocking call; takes a processor from a thread in a bracketed blocking call
  (`enterBlocking`, `exitBlocking`, Go's entersyscall and exitsyscall, around file I/O, path and directory calls, `write`,
  the wait for a child and the clocks a program reads)
  and hands it to another thread; and takes the events no thread has taken for 10 ms. With `MAXON_PREEMPT=off`, read once
  before `main` runs as maxon's runtime reads it, it does neither the stop nor the taking of a processor (unset, empty and
  `on` are the ordinary monitor; any other value is maxon's runtime abort 116, `schedulerPreemptSwitchUnreadable`). A stop asked while the thread is in
  the runtime's code, a DLL's or libSystem's is not taken, and nothing in the compiled code takes it later, so while the tick
  does not move the monitor asks again 100 µs later, then after twice as long each time up to 1 ms (Go re-signals every
  round, its sleep growing): a green thread that spends nearly all its time in the runtime's calls still gives up its
  processor within a few slices, a thread that takes its first ask costs one signal a slice, and one in a long call that
  no signal ends gets at most about 1,000 a second; a thread whose last ask is unanswered is not asked again (its signal
  pending, on Windows its cycle count unmoved since it was suspended, a suspension waiting for the system to run the
  thread and spending that turn). A program's clock read is a system call on Linux (a signal that comes
  during one is handled as it returns, in the runtime's code, so a loop of them takes about one ask in thousands) and a call
  into kernel32 or libSystem elsewhere, so the runtime brackets it and the monitor takes the processor instead (Go's
  sysretake). On Windows it holds the exit claim
  while a thread it stopped is suspended, which a thread that ends the process takes for good first (the runtime's
  `exitProcess`, `schedDrain` and the stub's fault handler), so `ExitProcess` never meets a suspended thread (Go's
  suspendLock).
- **Timers and the event queue.** A sleeping green thread (`greenSleep`) is on one heap under the scheduler's lock; the thread
  that waits in the event queue waits until the earliest timer, and a new earlier one breaks its wait. The event queue is a
  completion port on Windows (`GetQueuedCompletionStatusEx`, broken by `PostQueuedCompletionStatus`), epoll with an
  eventfd on Linux and a kqueue on macOS; a completion whose key is an event entry readies the green thread that
  waits on it. An event entry (Go's pollDesc) is a user event's (`eventUserNew`, signalled from any thread) or a handle's
  (`eventRegister`): an fd or a HANDLE registered once, edge-triggered for reading and writing on Linux and macOS, and on
  Windows with an OVERLAPPED of its own for each, an operation that completes at once posting nothing, or a process's
  (`eventProcessNew`), ready once a child the caller started has exited: its pidfd (`pidfd_open`, Linux 5.3) registered as a
  handle on Linux, a kqueue `EVFILT_PROC` with `NOTE_EXIT` on macOS, and on Windows a job object that holds the child alone
  and posts its "no process left" notification to the completion port, keyed by the entry. A wait
  (`eventWait`) ends at an event, at its deadline (a timer of the same heap, which a wait whose event came first takes
  off it, so a wait allocates nothing), at an eviction (`eventEvict`, which `eventForget` does before it waits out the
  waits in progress and takes the handle off the event queue) or at its coroutine's cancel; on Windows a wait that ends
  otherwise than by the completion cancels its operation and waits for the completion, so no operation is pending once
  the wait returns. Descriptors are kept for good on a free list, so an event the event queue took for one that was closed
  meanwhile finds an entry still, and is at worst a wake whose call finds nothing. `eventTransfer` reads or writes
  through one, and `eventPipe` makes a pipe whose ends it takes (on Windows a named pipe, the one pipe that overlapped
  I/O works on).

**The event queue** is how a green thread waits for the system. Nothing in it polls: a handle is registered once
(`eventRegister`), a green thread that waits on it parks (`eventWait`), and the kernel queues an event, a completion
packet on Windows, an epoll event on Linux, a kqueue event on macOS. A thread with nothing to run blocks in the kernel's
wait until an event comes or the earliest timer is due, takes what was queued (`takeEvents`) and readies the waiters; the
monitor takes the events that no thread has taken for 10 ms, without waiting. The runtime never polls. A run of a child
process (`runChild`, `runtime/subprocess.maxon`) is a few green threads each parked on one entry: the child's pipes are
registered ends read (and the input's written) through `eventTransfer`, the child's exit is a process entry, and all wait until
the run's deadline, at which the child is ended; the run's own green thread parks until they have finished, and the child, which
has exited, is then waited for, which returns at once.

**Main with or without the scheduler** (`self/backend/start.maxon`). The link decides by reachability, with no flag,
whether a program runs the scheduler (`SchedulerChoice`): it marks the live code from the entry as for a program of one
thread, with each of the scheduler's routines that code outside it names linked to its single-thread form, and the
program uses the scheduler when that code reaches `rt.stackSwitch`. Running a green thread and leaving one are the only
switches of a stack, so a `when`, an `async` call, an `await`, `sleep`, `Scheduler.yield()`, a run of a child process (its pipes and its exit are waited for on green threads) and whatever comes to start
or park a green thread reach it, in whichever function or module they are, and nothing else does: printing, files,
the environment, allocation, `Process.setExitCode`, `Scheduler.processorCount()`, panics and the leak count
do not. The single-thread forms (the substitutes) are the blocking call's bracket (`enterBlocking`, `exitBlocking`), the
exit claim (`takeExitClaim`), each a routine that does nothing (`rt.unbracketed`) since no other thread can take a
processor or stop one, `Process.setExitCode`, which keeps its code in the stub's `g.exitCode` (`rt.setExitCodeAlone`)
rather than in the scheduler's record, and the pins (`rt.pinAlone`, `rt.unpinAlone`, `rt.pinDepthAlone`), which count the
one thread's pins in the stub's `g.pinDepth`, an unpin with none being the fatal error the scheduler's is; `exitBlocking` may switch to the scheduler, so without its substitute every program
that prints would reach the witness. The link chooses the routine rather than the runtime testing a mode on each call: the
bracket costs a call to a `ret` around a write, and the scheduler's code is not in the executable at all. The entry calls
`rt.runMain` either way:

- **Without the scheduler** it is `rt.mainAlone`, which calls `main` (or each test of an executable of tests) by
  `rt.mainGreen` on the main thread's own stack, as the entry did before green threads, and gives its code, or the code
  `Process.setExitCode` set last. The process has one thread; no scheduler, processor, monitor, event queue or green stack is
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
  sleep, the event queue) or yields, and when it finishes; a preemption and a blocking call keep it. A member readied by a timer,
  a finish or the event queue takes the strand again before it goes on, on whatever thread readied it, so a strand moves between
  threads between its waits and its coroutines never run at once. The strand is the root's record (its holder, the queue of
  members that wait to run and their lock), which outlives every member, since each coroutine is awaited in a frame of the
  strand. An async call makes the coroutine's green thread with the address of the thunk of its function
  (`rt.async.new`), writes its argument words, and queues it last in the strand (`rt.async.start`), so it runs once the
  caller waits; the thunk calls the function with the words, keeps what it failed with (`greenErrorOffset`) and gives back
  what it returned. The tests of `tests/runtime/` that start
with `green-` hold the scheduler to many green threads ping-ponging, a busy loop stopped for another, a loop in the runtime's calls (the clock, allocation) giving way
about once a slice, the float state
across a preemption, a blocking call handed off, timers and a completion of the event queue.

- **The parallel loop's fork-join** (`runtime/parallel.maxon`, `parallelRun`, which the language's parallel loop lowers to,
  see "The parallel loop"). A loop of `count` indices is cut into chunks of consecutive indices, at most
  8 for each processor, and one job holds a part for the caller and one for each other processor that may help, the
  job being the loop's one allocation. The helpers' parts are queued on the caller's processor as behaviours are, so
  an idle processor steals them as Go's steal, and a helper holds a pooled green thread and stack only while it runs;
  every part takes the next chunk by an atomic add and runs it by the chunk code the compiler makes for the body (the
  indices of the part, `partLowOffset` to `partHighOffset`), the caller on its own stack, so a body that never parks is a
  call and an iteration allocates nothing. The caller parks only once no chunk is left to take, so its processor runs
  other work meanwhile (the helpers not stolen among them), until the last helper readies it. A chunk gives 0 or the
  word of the error of its first index that failed; the lowest index's error is the loop's, every other is dropped by
  the job's drop code, and a chunk whose first index is past it is not run. Off a green thread, with one processor and
  with one index, the loop is one chunk, run in the part area the caller's frame gives it: no job is made, so such a loop
  allocates nothing and is one call of the chunk code. `tests/runtime/parallel-run.maxon` holds it to results, the lowest index's error, nesting and
  bodies that park. Measured on a 16-thread x64 machine (Windows): a join of 8 to 512 empty indices takes 1.4 to 2.1 µs
  across the processors and 25 ns with one, where Go's equivalent (a goroutine per helper claiming the same chunks by an
  atomic add, a `WaitGroup` as the join) takes 2.5 to 4.3 µs and 50 to 80 ns; ten million squares written to an array
  take 3.2 ms against Go's 3.1 ms, both bound by memory.
  The statement itself, `parallel for (i, w) in out.enumerated()` against a plain `for` writing the same elements, with a
  CPU-bound body (100 rounds of a 64-bit multiply, add, shift and xor per element), on the same machine:

  | indices x rounds | plain `for` | 1 processor | 4 processors | 16 processors |
  |---|---|---|---|---|
  | 1 x 1 | 1 ns | 6 ns | 6 ns | 6 ns |
  | 8 x 1 | 7 ns | 12 ns | 735 ns | 1.16 µs |
  | 64 x 10 | 0.43 µs | 0.45 µs | 1.45 µs | 2.77 µs |
  | 1000 x 100 | 123 µs | 125 µs | 43 µs (2.8x) | 35 µs (3.4x) |
  | 100000 x 100 | 12.1 ms | 12.2 ms | 3.2 ms (3.8x) | 1.11 ms (11x) |
  | 1000000 x 100 | 122 ms | 125 ms | 31 ms (3.9x) | 9.9 ms (12.3x) |

  A loop of one index, and any loop with one processor, is one call of the chunk (no job, no allocation): a few
  nanoseconds over the plain loop. Across processors a loop pays the fork and join, about a microsecond, so it gains
  from some thousands of nanoseconds of work up.

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
parameter or owned once `stringOf` makes one. A dropped `String` goes through the compiler-emitted drop routine, which
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
  "Raw values"; methods, `static function`, `static let`), `union` (payloads, methods, explicit tags, `static function`,
  `static let`), `interface`, `typealias X = int(a to|upto b)`,
  `typealias X = bits(n)` (see "Bit patterns"), `typealias X = float(a to b)` (see "Floats"), either range with `implements Parent` (see "Integers"), `typealias X = Box with Count` (also in the body of a type, an interface or an extension), a top-level
  `let` constant. A function, a type and a union can be generic (see "Generics"). A `type` or a `union` can say
  `is noncopyable` right after its name (`type Socket is noncopyable`; never an `enum`), which takes `Copyable` from it,
  and a method of either can be a destructor (`function commit(destructs self)`), which takes both (see "Capabilities and
  destructors"). A `service` is a `type` written with `service` in place of `type`
  (see "Services").
- Function values (see "Function values"): a function type, named by a typealias
  (`typealias Step = function(Count, mutates Box) returns Count`, also in the body of a type, an interface or an extension, and `... mutates captures` for
  one whose closures change what they capture), a function named without a call, a closure `function(n Count) gives n +
  offset` (its parameters' types and its result may be left to the function type it is written for), a call through a
  value (`f(x)`, `self.op(x)`, `ops[i](x)`, a module-level `let`), and a cast to another function type (`h as Callback`).
- A cast to a type that is not a number is the value itself, moved or borrowed as the operand is: to the type the value
  has (a literal is written as the target, `[1, 2] as Ages`), or between two instances of one generic whose type
  arguments are one type or integer aliases of one name, range and domain (the library's `Byte` and the author's), as
  maxon retags a container. A cast to another instance is `E3131`, and to any other type that is not the value's `E3009`.
- `extension` blocks (see "Extensions"): methods, static functions, static lets and conformances added to a type, an enum, a
  union, `Array`, `Map`, `String`, `bool`, `int`, `float` or a built-in record from outside its body.
- `let`, `var` (with a value, or `var x as T` without one), assignment to a variable, a field or an element,
  `_ = call()` (a discard of a value that already exists is `E3067`), `if`/`else if`/`else`, `if let x = try call()` and `if try call()` with an `else`, `while`, `for i in a to|upto b`, `for x in xs` over an array, a `List`, the grapheme clusters of a `String`, a range value, or any value
  whose type has maxon's iteration protocol (a type of the program, a type parameter through its bounds, an interface
  value through what its interface requires), `for (i, x) in xs.enumerated()` over any of these with the index of each element, `for (k, v) in m` over the library's `Map`, whose iterator lends each entry, and `for c in E.allCases` (see "Primitives"), `parallel for` over a range, an `Array` or its `enumerated()` (see "The parallel loop"), `match` as a statement or expression
  (case, literal, range and string patterns, `or` alternatives, `default`, `panic` arms, `default throws E`, and `stmt and fallthrough`), `break`/`continue`
  with labels, `return`, `when` (a behaviour, in the library tiers only, see "Immutable values, cowns and behaviours"), `await p` of a promise of a
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
  comparisons, `and`/`or`/`xor`/`not`, `x if c else y`, range values `a to b`
  and `a upto b` (see "Iteration"), `as` casts, error enums, `throw`, `throws E`, `try e`,
  `try e otherwise ...` and `try` blocks (see "Errors of the program"), calls with Maxon's
  positional-then-labelled arguments (a label may be a keyword: `File.rename(from, to: dest)`), parameter conventions (`mutates`, `consumes`, `sets`), `x.clone()`,
  `.name`/`.ordinal`/`.rawValue`, subscripts `a[i]`, `match consumes x`.
- A keyword may be a name where Maxon takes one as a declared name and nothing else can stand: the name of a function, a
  method or an interface's requirement, a parameter's name, an argument's label and a member after `.`, so `function with(...)`,
  `static function int(...)`, `x.with(n)` and `Log.int(key, value: v)` are code. An operand may be one too, which reads
  a parameter or calls a function so named, except a keyword that starts an expression or a statement (`true`, `false`,
  `self`, `panic`, `match`, `function`, `sizeof`, `countof`, `not`, `async`, `await`, `try`, `let`, `var`, `if`, `while`,
  `for`, `when`, `throw`, `return`, `break`, `continue`, `_`), which keeps that meaning there, and `end`, which a reader
  could not tell from the end of a block: a parameter named by one of these is declared but not read. A variable, a
  field, a type and a case are not named by a keyword, as in Maxon, apart from `from` and `to`, which are names wherever
  a name is.
- Built in: `print`, `printError`, `panic` (which takes a string literal or an interpolated string), `freeze`, `share`, `thaw`, `cown` (also `cown(x, dropWith: d)`, in the library tiers only), `exchange`, the OS functions of maxon's stdlib (see "The OS"), the float functions (see "Floats"), the aliases `ExitCode`, `Count`,
  `ElementIndex` (an index that a `Count` goes where one is taken, and the other way only by `as`), `FullRangeInt`, `FullRangeFloat`, `Byte`, `ByteArray` (`Array with Byte`), `ByteArrays` (`Array with ByteArray`), `StringArray` (`Array with String`), the limits of the sized integers as constants (`u64.max`, `i32.min`, ...), the `String` methods `byteLength()`, `byteAt(index)`, `sliceBytes(start, endPos: end)` and `toByteArray()` and the static `String.from(bytes)`, and maxon's `isEmpty`, `equals`, `append`, `count`, `contains`, `startsWith`, `endsWith`, `findFirst`, `findLast`, `startIndex`, `endIndex`, `slice`, `sliceLength`, `replace`, `replaceFirst`, `trim`, `trimStart`, `trimEnd`, `toLower` and `toUpper` with the type `StringIndex` (`charIndex()`, `bytePos()`) and the error `StringError`, the enums `ArithmeticError`, `ArrayError`, `MapError` and `IterationError`, the interface `Error`, and the generic types `Array`, `FixedArray`, `Map`, `List`, `SlotMap`, `SlotKey` (see "Primitives") with `sizeof(T)` and `countof(T)`,
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
  operand where that is an alias or a pattern; a shift of an anonymous range is, as `+` is outside a typed context, the exact
  interval of the shifted values, cut to the word, so `((rounds + 1) shl 2) as Count` of an `int(10 to 14)` needs no check. An `and` with a non-negative operand is between 0 and that operand, whatever the other, so it keeps the
  alias of its one named operand wherever that alias holds the interval, though the alias does not hold the other
  operand: `(index + 1) and mask` of two `int(0 to 2^63)` values is of their alias, as maxon's hash table writes it. `not` keeps its operand's alias where the alias is closed under it (a pattern, or a range that flipping maps
  onto itself, as `int(i64.min to i64.max)` and `Count`), and is otherwise typed as `-` is: by its context, or else the
  exact range of the flipped bits, so `not b` of an `int(0 to 100)` is `int(-101 to -1)`.

An anonymous range goes into an alias only if the alias holds all of it, else it is `E3005` and asks for a cast
(`try t as Score`). A `return` of an integer of another type, an anonymous range included, converts it to the declared
type with no cast, as `value as T` does, where the range analysis proves that it fits, as after
`if count >= 0 and count <= 255` for `return count` in a function that returns an alias of 0 to 255; where it does
not, it is `E3057` ("the value may not fit"), and the fix is `try <value> as T otherwise panic("out of range")`. A
literal that does not fit is `E3005`, and an assignment, an argument and an initialiser still need the cast. Comparisons take any two integers one word holds, an alias against an anonymous range or a constant
included; only two different aliases are `E3005`. An arithmetic operator or a comparison of an integer whose type may
be negative with one whose type may pass i64.max computes in the unsigned word, and is `E3005` unless the range analysis
proves the first not negative where the operation runs, by the conditions that guard it: `left + first` of a `Count` and
`let first = column - top if column > top else 0` is accepted, as maxon accepts it. The branches of a conditional or a `match` with no type to
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
built-in `Count` is a subtype of `ElementIndex`, and so is `BytePos`, maxon's position in a string's or a buffer's bytes.

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
- The built-in functions `trunc(x)` (a `FullRangeInt`, toward zero, and an integer `x` itself, with its type, as maxon's:
  there is nothing to truncate, and a double would lose the bits past its mantissa), `round` (ties to even), `floor`, `ceil`, `sqrt`,
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
- **Proven accesses.** A `get` or a `set` of an `Array` or a `FixedArray`, and a `remove` of an `Array` (a call whose built-in
  row says `Requires`), whose index the range analysis proves below the collection's count, and a `first`, a `last` or a `pop`
  of an `Array` the analysis proves is not empty, cannot fail: it compiles to its twin that cannot (`ast.provenTwin`), the
  access with no compare and no failure block, and needs no `try`. A proven `pop` or `remove` moves the count down by exactly
  one, so `if xs.count() >= 2` proves two pops and `while not xs.isEmpty()` a pop each iteration, and a `push` moves it up by
  exactly one, so a count nothing bounds is at least 1 after one; a checked one moves it down by an amount not known, which is
  true of both its exits. The analysis follows
  the count of an array that is a local, or a field of one to depth 2, as a place (`count(xs)`), which `xs.count()`
  reads, a comparison relates to an index (`i < xs.count()` gives `count(xs) - i >= 1`) and `isEmpty()` narrows; a call
  that changes the array moves or forgets it by the row's `Count` effect, and anything else that may change the array
  forgets it. A side of a comparison may be a place moved by a constant (`i + 1 < xs.count()` gives `count(xs) - i >= 2`
  and narrows `i`), a binding or an assignment of one relates the local to it (`let n = xs.count()` makes `n` equal to the
  count until either changes, `j = i + 1` puts `j` one above `i`), and `i = i + 1` moves what is known of `i` by one instead
  of forgetting it; none of this holds of a `bits(64)` pattern, whose sums wrap. A question of how far apart two places are
  follows chains of up to three relations (two places between), adding their gaps, and an operand of an arithmetic or an
  index is cut by its relations to places whose spans are known: an index below a count is below 2^62, so `i + 1` cannot
  overflow, and `n - 1` cannot go below 0 where `n` lies above an index. An index may be a place moved by a constant
  (`xs.get(i + 1)`, `xs.get(n - 1)`). A `for i in a upto b` whose bound `b` is a count or a local, or one moved by a
  constant, that the loop's body only raises or leaves alone (a `var` it does not assign, an array it pushes to) gives its
  body `b - i >= 1`, and a start `a` that the body only lowers or leaves alone gives `i - a >= 0`; what is known where the
  loop is entered stays at its top unless the body may falsify it ("Checked arithmetic"). A state keeps at most 32 relations. Every rule is checked by brute force against what the operations do to
  values (`self/check/rules.maxtest`). An access that is not proven where no
  `try` covers it is `E3057`, which says what it needs (`'xs.get(i)' needs i < xs.count()`) and names the guard and the
  `try` that would give it (`'xs.pop()' needs xs.count() > 0`). A `try` that covers nothing that can fail, as one that covers only proven accesses, is `E3944` (see
  "Checked arithmetic"). A subscript `a[i]` of an `Array` or a `FixedArray` asks what a `get` at its index asks, where
  its element is read or its address taken (for an assignment `a[i] = v`, after `v` is computed); where it is proven, the
  element is read or written with no check, and elsewhere it traps outside the array as before, so nothing a program may
  write changes. The typing records each verdict by the subscript's node (`Typing.subscripts`), which lowering reads.
- What leaves the function, through a `try` without `otherwise` or a `throw`, must be the error the function throws:
  `E3059` otherwise, also where the function throws nothing; convert another error with `otherwise throw`. Where what
  fails so is an operation's range (an arithmetic, a cast, an index, a conversion at a return), `fix` gives the
  operation a `try ... otherwise panic(...)` of its own, as it does where no `try` covers one. A handler
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
- **A try before an assignment's target** covers the call that lends the place and passes its failure on before anything
  is written: `try nodes.get(key).visited = true`, where `get` is a `mutates self` method that throws and lends the node
  (see "Ownership"). The target is a field or an element of what the call lends; a try with an `otherwise` there is
  `E2001`, since a handled failure leaves no place to write (a try block around the assignment handles it), and so is a
  try before a variable, which covers no call. The value is computed as any value, with a try of its own where it can fail.
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
- A built-in that can fail throws maxon's error enum for it, which the stdlib declares: `ArrayError` for
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
- **A `try` that covers nothing that can fail is `E3944`**, whatever it covers: an operation the range analysis proves
  cannot fail, an access it proves inside its collection, or a call of a function that does not throw. The code would
  say a failure is possible where it is not, and its handler would be code that never runs. It is the same for a `try`
  block (nothing in its body can fail), an `if try` (its call cannot), and a `try` with no `otherwise` that passes on
  nothing. A `try` inside another that covers something that can fail does not excuse the outer one: each is judged by
  what it covers itself, a `try` with an `otherwise` taking what is inside it. `fix` removes the `try` and its handler
  (a try block's `try` line, its `end` and its handler, keeping the body: the handler never runs, so nothing it holds
  is lost), and gives no fix for an `if try`, which is rewritten as a call and the code that follows. A sharper
  analysis therefore refuses programs the older one accepted, and every commit that sharpens it removes the `try`s it
  makes needless with `fix`, as the conversion of maxon's code does. The error is reported only for a module with no
  other error, since the proof takes the rest of the program to be right, and not for code the analysis proves
  unreachable. A generic body is analysed once, with its type parameters unknown, so a `try` that a body needs is
  needed in every instance, including one whose types could not fail.
- `minimaxon fix` covers an operation that can fail where no `try` covers it with a `try` that passes the failure to the
  caller where the function throws that failure's error itself, as the program would write, and otherwise with one that
  panics (`otherwise panic("out of range")`), a failure the program did not expect.
- A failure leaves what it interrupts, as a jump out of it would: the temporaries of the statement and the locals of
  the blocks it leaves are dropped, and so are the values computed for a call, a record or a union that never runs
  (`ir.Lowerer.pending`). The ownership pass follows each failure to its handler, so a value moved before a failure may
  be gone in the handler (`E3102`), and a value that cannot be dropped may not be in flight where something can fail
  (`E3919`). A handler runs where its expression failed, so what that expression would have given is not in flight there,
  and the arms of a `match consumes` take its subject apart, so the subject is not in flight in them: the handler of
  `let b = try open(2) otherwise ...` may end an `a` opened before it with a destructor that can fail.

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

What is known where a loop is entered stays known at its top unless the loop's body may falsify it, and a place loses
what is known of it when it is assigned, passed to a call that may change it, or captured by a closure that borrows it to
change it (the closure changes it whenever it is called, and the ownership pass lets nothing read it while the closure
lives), so no loop is walked twice (but see accumulators, below); a condition says nothing of a place that it changes
itself, as in `if a > 5 and reset(a)`. Before a loop is walked, a scan of its body (and a `while` loop's condition) notes
every way the walk would see a place change, by the same classifier (`self/check/loops.maxon`): an assignment, a
`mutates` or `sets` argument, a receiver a call takes to change (by its row's `Count` effect, or a change of everything
in it), a closure made that captures a local to change it, a walk of a drain, in the loops nested in the body too; a
call of a function value, an `await` and a `when` are opaque and forget whatever may be assigned. At the top, a local the
body writes has its type's span again; a count the body only grows keeps its lowest and the relations it lies above, one
it only shrinks its highest and the relations it lies below; anything else the body touches, or that holds or lies inside
what it touches, is forgotten. The effects are summed over every statement whatever path runs it, so what is kept holds
at the start of every iteration, after the body's end and after a `continue`: one scan decides the top, with no
iteration.

A `while` loop may state an invariant after its condition, `while lo < hi requires hi <= xs.count() 'search'`: comparisons
joined by `and`, each with `<`, `<=`, `>`, `>=` or `==` between sides that are an integer place or a count, moved by a
constant or not, or a constant (anything else is `E3945`). The analysis proves it where the loop is entered, at the end of
the body and at each `continue` that repeats the loop, from what it knows there; a comparison it cannot prove is `E3945`,
which names it and the point, and whose fix takes the invariant out. So it holds at every top, which knows it beside what
the body cannot falsify, and so do the body and the code after a loop that ends by its condition (a `break` leaves with
what it knows where it breaks). It is never evaluated when the program runs. A midpoint `lo + (hi - lo) / 2` of two places
that the analysis knows have `hi - lo >= 1` lies from `lo` to `hi - 1`, so its sum cannot fail, and a binding or an
assignment of it is at least `lo` and below `hi`; `(lo + hi) / 2` is not one, as its sum may overflow where the midpoint's
does not. A write of a place keeps what two relations through it said of the places on either side (`hi = mid` keeps how
far `mid` lay below a count that `hi` was at most), and a binding or an assignment `v = w + c` gives `v` each relation of
`w`, moved by `c`. A count nothing else bounds is below 2^62 on either side of a comparison, so `i + 3` cannot overflow
where `i < xs.count()`. A binary search, a reverse from both ends, two pointers and a stride need no `try`
(`tests/run/while-loops-that-require-an-invariant-and-midpoints-run-with-no-check.maxon`).

A divisor that is not 0 may still be -1, so a signed `a / b` needs `b > 0`, or `b != 0` and `b != -1`. Proven code is
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
  local projection bindings (`mutates r = a.f`): borrows exist only as parameters, match bindings, call arguments, the
  results of functions that return a borrowed place, and a `let` bound to one (below).
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
  variable (see "Primitives"). It may throw: a failure leaves before any place is lent, since the lend is the `return`
  and nothing runs after it, so a `try` around the call gives the place only where it is lent (a `try` block, an argument,
  propagation, a handler that leaves, `otherwise panic(...)`), and a scalar read out of it takes a value fallback
  (`try p.counter() otherwise 0`). For now it may not be a function value, be the `createIterator()` of a for, or be
  the `init` of `Type from "text"` (each `E9000`); a function type and a closure have no syntax for it.
- **A requirement can lend a place.** `function label() returns borrowed String` in an interface is met by a method that
  lends one, and by one that gives a value only where the value is a scalar that is not a float, the one kind whose
  borrow and value are the same word (`E3016`, naming `returns borrowed`, for any other: a call through the requirement
  would own nothing it could drop). A call through it, on a value of a type parameter or of the interface, is a
  projection as a direct call is (`check.requirementCall` gives every such call the requirement's result), so it borrows
  the receiver and the lent arguments for as long as the caller uses the place. A requirement that lends may throw, as a
  function may.
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
  `E3186`. A scalar argument lends nothing to a result that is not a scalar.
- **A lending method lends with the access its caller has to the receiver**, as Hylo's subscript has a `let` and an
  `inout` accessor, here one body. A method with a `let` receiver whose every returned place lies in `self`, a field of it
  at any depth or what another such call gives of one, which the checker proves from the body (`check.lendsInSelf`, kept in
  the module's typing), lends that place at its address (`ast.lendsByAddress`, the one function that lowering, the
  interpreter and the tables of interface values ask). Called on a receiver the caller may change (a `var`, a `mutates`
  parameter, a writable part of either, a place lent to be changed), the place may be assigned through and passed
  `mutates`, and the receiver is held for as long as the place is used, as any change holds it (`check.lendsWritably`);
  called on a receiver the caller may only read (a `let`, a `let` parameter, a `let` receiver), the place is read-only
  and the receiver is shared, so several such reads may be live at once. The others stay read-only and give the value
  where they lend, as before: a method that meets an interface's requirement (its table calls it as the requirement
  lends), one whose receiver is passed in registers (a scalar, a String: what it lends is a copy, and a change through it
  is `E2013`, saying so), one that lends a String (a view of bytes) or a tuple (a box of borrows), the contents of a
  frozen value, an element of a collection (a byte of a byte array and a record held in place have no word of their own),
  a place in a parameter, and a built-in that lends a view. A read through the address is the load the value's read was.
- **A `mutates self` method lends a place to be changed** (`ast.lendsToChange`), as Hylo's `inout` accessor does: `function
  first(mutates self) returns borrowed Cell` gives a place that the caller assigns through (`p.first().n = 10`), passes to
  a `mutates` parameter (`bump(p.first().n)`), calls a `mutates self` method on, and reads as the form above lets it. The
  place lies in `self` alone (`check.requireLentToChange`): a place in a parameter, the contents of a frozen value and a
  place that another method lends only to be read are `E3926`, while a lend of a place that another `mutates self` method
  lends is one (`h.inner().first().n = 7`). An element of a collection and a tuple are not lent so yet (`E9000`): a byte of
  a byte array and a record an array holds in place have no word of their own that outlives the call. The call passes its
  receiver `mutates`, so the receiver must be a variable that may change, and the place holds the receiver, and nothing
  else, as long as the caller uses it: changing the receiver while an assignment's value or a later argument is computed,
  and two places it lends passed `mutates` to one call, are `E3186`. The callee returns the address of the place's word,
  whatever the place holds, a String or a float included; a read loads the value from it, and a change writes through it.
  A record held in place is the address of its words already, which the callee returns, and the caller's place of it is
  a word of its own that holds that address, as every place of a value held in place is.
- **A place of a byte-sized type that a call lends by address is one byte wide** (`ir.lentInByte`): of a type that an array
  packs into a byte (a byte range, an enum of at most 256 cases; `holding` for an element answers `heldPacked`), wherever
  the place lies. A packed element is lent as the address of its byte, `rt.bytes.elem` as a word's is `rt.array.elem`: the
  same entry with the same check, which the expansion makes the same bounds test and the same address, base plus index
  with no scale (`elemaddr ... shl 0`, `byteElementShift`). A read of the place is `loadbyteat`, and a callee that takes it
  by address is given a slot that holds the byte, stored back with `storebyteat` when the call returns, as an element of a
  byte array passed `mutates` is. A place of such a type that lies in a word, a field that a method lends, is read and
  written one byte wide as well, which is sound: every target is little-endian, so the word's lowest-addressed byte is its
  low byte; the value is from 0 to 255, so that byte is all of it and the upper bytes are 0; and no byte access writes the
  upper bytes, so they stay 0. The interpreter gives each byte address a cell of the byte array that names the byte by its
  index, so a change of the array's structure makes it stale as an element's address is. A place of any other type is its
  word, lowered as before.
- **A `let` bound to a borrowed result borrows it** (`check.bindsLent`): `let t = s.trim()` or `let e = entryOf(table)`
  binds the place, or the view (see "Text"), with no copy, and owns nothing, so nothing drops at its end. It is read as a
  `let` parameter is: moving it anywhere is `E3900`, whose fix is `.clone()`, and a behaviour cannot take it (`E3909`). For
  its scope it holds what the call lends from, so changing or moving that is `E3186`, whose fix copies what the `let` is
  bound to. Each place lent must outlive the statement: one made there, such as `name().trim()`, is `E3927`, fixed the
  same way; a literal and a constant outlive it. A result lent from such a `let` lies in what the `let` holds, so it holds
  that in its place (`check.boundPaths`).
- **A try over a call that lends a managed place, with a value on a failure, lends one or the other** (`ast.isLentTry`):
  `let name = try shelf.name(i) otherwise "none"` binds a borrow of the element on a success and of the fallback on a
  failure, and copies neither. The borrow holds what either may lend from, the call's lenders and the fallback's (a place
  it names, or the local below), so changing either meanwhile is `E3186` (`check.addLentPaths`). A fallback that is a value
  made there, such as `"#{i}"` or a call's result, moves into a local of the enclosing block that the checker declares for
  it (`check.holdFallback`), which is filled only on a failure and dropped where the block ends; a String literal with no
  parts is static and needs none. Either is lent only to be read: assigning through it or passing it `mutates` is `E2013`,
  and a change in place goes in a try that leaves on a failure, as a try block does. Moving it out is `E3900`/`E3901`, whose
  fix copies the whole try, `(try ... otherwise "none").clone()`; a scalar read out is a value, as it is from any place.
- **A `var` bound to a borrowed result is a view** (`ast.LocalEntry.isView`): it borrows as such a `let` does, owns nothing
  and holds what its binding lends from for its scope, and it may be assigned another borrowed result that lends only
  from within what it holds or from itself, as a parser narrows what is left to read with `rest = rest.sliceBytes(n,
  endPos: rest.byteLength())`: no copy, no allocation, and nothing drops. A result lent from another place, which the
  `var` could outlive, is `E3927`, any other value, which would need an owner, is `E3901`, and changing it in place
  (passing it `mutates` or `sets`) is `E2013`; each is fixed by binding the `var` to a copy, `.clone()`, which owns its
  value. The rule is local to the function, as a re-borrow of the same place in Rust is; a projection in Hylo is never
  rebound (assigning a `let` projection is an error and assigning an `inout` one writes through it), so a `var` view
  follows Hylo in everything but that it may be rebound.
- **Exclusivity.** A place is a local or parameter followed by field selections and element selections (`a[i]`, see
  "Primitives"), and two places overlap when one is a prefix of the other (`a` and `a.f`; `a.f` and `a.g` do not;
  `a[i]` and `a[j]` do, for every index). An argument passed `mutates`, `consumes` or `sets`
  must not overlap any other argument of the call, receiver included (`E3186`). The rule applies to every
  argument that names a place, scalars too, except that a value a `consumes` parameter copies (a scalar, a frozen value
  or a cown) is read there as a `let` argument is, so `m.upsert(i, value: i)` is fine. An argument that is a borrowed place stays borrowed while the later
  arguments are evaluated, and so does the target of an assignment while its value is, so a call nested there that
  changes an overlapping place is refused too (`E3186`). An operand is evaluated to a value before a later operand's
  `mutates` access begins, left to right, so an operand that only reads the place first, `sum(s.byteLength(), b: put(s))`
  or `f(s.clone(), n: put(s))`, sees it as it was and is not refused (see "Where it differs from Maxon").
- **Initialisation.** `var x as T` declares a variable with no value. Reading it before every path has assigned
  it is `E3902`. A `sets` parameter starts without a value and must have one on every path that returns
  (`E3903`); its argument must be such a variable, which has a value after the call (`E3904`), and not before:
  another argument of the same call that reads it is `E3902`.

### Borrowing values

A cursor of a raw buffer, `__ManagedMemoryCursor with T` (see "Primitives"), points into the buffer it was made from: it
borrows it. So does a String's, `__StringCursor` (the String's two words and the byte positions of a grapheme cluster,
made by `create(text)`), whose `current()` lends a cluster's view, and `advance()` moves on; the library's `StringIterator`
holds one as `ArrayIterator` and `ListIterator` hold a buffer's. So
does every value of a type that holds one: a record with a field of such a type, a union with such a payload,
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
- **A String literal is no temporary.** One with nothing interpolated, also in parentheses, is bytes of the executable's
  data, which last the whole run: a value or a view made of it, a cursor, an iterator or a slice, borrows nothing, so it
  may be bound, kept and returned as freely as an owned value. An interpolated String is made by its statement, and is one.
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
  type. `a == b` and `a != b` of two values of a type parameter call the `equals` that a bound of it declares
  (`Equatable`'s), and `<`, `<=`, `>` and `>=` the `compare` (`Comparable`'s), testing the case of the `Ordering` it
  returns, as maxon's do; each is `E3005` where no bound declares a method of one parameter that returns `bool` or an
  `Ordering`. Two values of one record type or one array type compare the same way through the type's own `equals` or
  `compare`, which it need not declare a conformance for (an array's `equals` is its conformance to `Equatable`, see
  "Primitives"), and a type without one cannot be compared (`E3005`), as in maxon; two Strings are equal by the built-in
  comparison of their bytes and ordered by the `compare` of their `Comparable` conformance. A type parameter's name stands for the parameter in an expression, over a declaration of that name. A method
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
  parenthesized by convention: `Option with (Box with T)`. Their number is checked (`E2056`), except that inside the
  body, the `implements` clause or an extension of a generic type, another generic type written with no `with` binds
  its type parameters by name to the enclosing type's, as maxon's: `MapIterator` inside `type Map uses Key, Value` is
  `MapIterator with (Key, Value)`, and one whose parameters the enclosing type does not all declare is still `E2056`. A `typealias` names an
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
	let back = shapes.pop()
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
  (`Seq with Integer`), or bare (`Seq`), a value of any type that implements it whatever that type binds them to, through
  which only a requirement whose signature names none of them is called (`E3120`, see "Where it differs from Maxon").
- **Associated types held at an interface.** A value goes in an interface value whose associated type is held at an
  interface it binds otherwise: an `UpBag`, which `implements Bag with (Integer, UpCursor)`, is put in a `Bag with
  (Integer, Cursor with Integer)` where `UpCursor` implements `Cursor with Integer`, as maxon's
  `associated-type-held-at-an-interface` holds it (`ast.widening`). Each type the instance binds is the one the
  conformance binds, or an interface that type implements by the conformance the module sees, widened in turn; each
  requirement takes and throws what it does at the conformance's bindings, and gives the same or, where its result is a
  held associated type itself, a value of the type the conformance binds there, which is put in a value of the interface
  it is held at. The value's table calls, for such a requirement, a stub that calls the conformer's method and puts what
  it gives in a new box with its own type's table (`ast.widenStub`, `ir.lowerWidenStub`), so each value a held result
  gives is boxed as it comes out, and is no view; the other requirements are the methods, and a value of the instance
  the conformance binds is boxed with the conformance's own table, with no stub (`tests/ir`). A position that a
  parameter names, a result that names it inside another type, and a result that is lent are not widened, since the
  box of such a value would have to come from the caller or be made of a value that already exists; a value whose type
  is not widened to the instance is `E3005`, or `E3126` where a type the conformance binds does not implement the
  interface the instance holds it at, and `E2015` where a requirement names a held position otherwise than as its whole
  result (see "Where it differs from Maxon"). A generic's bound is never met by a widening: an instance calls the
  conformer's method directly, which gives the value unboxed.
- **In a generic.** An interface value meets a bound of its own interface (another, even one it extends, is `E3017`), so
  a generic instantiated at one (`twice(a)` with `a` a `Shape` value, `Holder with Shape`) calls through its table where
  the body calls through the bound. A call with a value of the concrete type is an instance of its own, monomorphized as
  any instance is. A parameter written with an interface type is a value of it, whatever the argument: a method and a
  requirement take no type parameters of their own, so one mechanism serves functions, methods and requirements.
- **Crossing.** An interface value crosses between threads as the value in its box does: it is frozen, sent in a message
  or a reply, held by a cown, taken by a behaviour and read by the iterations of a `parallel for`. Putting a value in one
  asks what a message's argument asks (`ast.unsendable`, see "Services"), so a value that holds a promise is `E3919`, as
  one that holds a borrow is `E3927`; what the box holds is judged there and never again, and a function value in it
  crosses, which a message can call only through the interface's methods (`E3221`, unless the interface requires service, see
  "Services"). A frozen interface value is read as
  any frozen value is: a call of a method whose receiver is `let` reads the frozen box and writes nothing. In generic
  code the check cannot see what a type parameter holds, so each instance is judged at its own type arguments where it
  boxes a value of a type that mentions one (`ir.requireBoxCrossing`), as `freeze` and `cown` are, and refused there as
  `E3919`.

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
  one is moved out of an owned tuple, and no pass has a rule of its own for tuples. A tuple that is lent rather than owned (an
  element, the variable of a loop that lends it, a part of a borrowed parameter or receiver, the result of a call that
  returns borrowed) gives each name a borrow of its element instead, a `let` that owns nothing and holds what the element
  lies in for its scope (`check.lentLetOwn`): moving one is `E3900` (`.clone()` is the fix), and a `var` takes a copy, as
  `var x = a[i]` does. The names count against the elements: `E2015` for a declaration and `E3005` for an assignment.
- **A tuple of lent places is lent.** A function that `returns borrowed (K, V)` may return a tuple written of places it may
  lend (`return (keys.current(), values.current())`, `return (a, b)` of the receiver's fields) and scalars, which a borrow of
  one is: each element is held to the law of a borrowed return (`E3926` for one made in the function, a nested tuple among
  them), and the tuple holds the borrows of all of them, as a value of a type that holds a borrow does: it lives in the
  caller's scope, cannot be moved, and holds what it borrows until it ends (Hylo's projections captured by a tuple). It is
  lowered with no box: the caller passes a frame area of a word for each element after the arguments (`ir.FrameArea`,
  `ast.lentTupleElements`), the function writes the borrows there and returns it, and a call through a requirement or an
  interface's table does the same at the instance it calls. A record literal of lent places is not lent: a borrowed record
  is one a parameter holds.

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
a union payload, an element of an `Array` or a `List`. A `frozen`, a `cown` and a
`SlotKey` have both whatever they hold, so the loss stops there. A generic type is judged per instance (`Box with Socket`
is not `Copyable`, `Box with Count` is), and a type parameter has a capability only by bound; `Copyable` includes
`Droppable`. A type that declares a destructor, a method whose receiver is `destructs self`, loses both, and so does
whatever holds one. A destructor is allowed on a `type` and a `union`, never on an `enum` (`E3920`), and in an interface's
signatures, so generic code can end a `T` through a bound. Calling one moves the receiver in like `consumes self`, and in
the body `self` is owned and its fields may be moved out. It is a mutable owned value too, as maxon's finalizer writes its
receiver until its `destruct self` (specs/linear-types.md): its fields may be assigned and `mutates self` methods called on
it, through its own place, so what a destructor changes is what it ends. A `consumes` parameter is not: it is owned but
read-only, like a `let`, and is moved into a `var` to be changed. Where the body ends, `self` is destroyed: the fields that were
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
  drops it.
- **An implicit drop needs `Droppable`.** These drop a value, and each is `E3919` when its type lacks `Droppable`: the end
  of a scope that still holds an owned local, a `consumes` parameter, a `consumes self` or a payload bound by `match
  consumes` (also through a `return`, `break` or `continue` that leaves it, reported at that statement; at the
  declaration for the end of a block); a `consumes _` parameter; an assignment over a variable, field or element that
  holds a value; a discard `_ = e` of a value it makes (a discard of a place drops nothing, and is `E3067`); a temporary that nothing takes (a result passed to a `let` parameter, read through
  as in `make().id`, or left by an expression statement); and a payload that an arm of `match consumes` does not name.
  The ownership pass keeps a stack of the owned locals in scope whose type lacks `Droppable`; a fact about one means it
  holds nothing, so it is checked at the end of its scope and removed there. Where two paths meet, such a local must hold
  nothing on both or neither (a partly moved one is judged by what is left of it), else `E3919` at the move that happened on one path only (or at the declaration of a variable
  assigned on one path only). A path that ends in a trap needs no destructor, and a `while true` loop is left by its `break`s alone, so
  only they reach the code after it. In a generic body a `T` without the bound
  is such a type, so code that lets a `T` die writes `where T implements Droppable`. At a `return`, a `break` or a
  `continue` that leaves a local holding such a value, `minimaxon fix` (kind `ending`) writes the call that ends it before
  the exit, where its type has one way to end it that takes nothing: its one destructor without parameters, or, for a
  record without destructors, that of its one part that cannot be dropped (`h.file.close()`), a destructor that throws
  called `try ... otherwise ignore`. The exit's own line takes the call before it, and the handler of a `try` on one line
  (`otherwise return 1`) becomes a block that ends the value and then leaves. A function whose locals all have
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
or a `union` of any module (`extension shapes.Counter`), the built-in `Array` and `List`, `String`, `bool`, `int`,
which stands for every integer type (`self` has the range of `FullRangeInt`), and `float`, which stands for every float type
(`self` is a `FullRangeFloat`), as maxon's `PrimitiveExtensions.maxon` extends them. The end label is the extended type's name.
Inside, `self` and the bare names of the type's fields and type parameters mean what they do in its body, and `Self` is
the type. As in Swift, an extension of any of these may add static members: a `static function`, and a `static let`, which is
a constant of the type as one its body declares is (see "Default parameter values, static fields, default throws and
fallthrough"), read as `Type.name` (`int.answer`, `float.fromString(text)`, `String.greeting`, or through a typealias of an
integer or a float, `Port.answer`) and as `name` inside the extension. A static function of `int`, `float` or `bool` is
generic over `Self`, a type parameter bounded by the family alone: a call through the family's name binds it to the family's
full type (`int.parse(text)` returns an `int(i64.min to i64.max)`), and one through a typealias binds it to the alias
(`Port.parse(text)` returns a `Port`), each with an instance of its own, made where it is called and never otherwise (see
"Generics"). Its body does with a value of `Self` what a generic does with one of an unbounded parameter, and converts
into it: `return try value as Self otherwise throw ParseError.outOfRange` checks the value against the range of the instance
(a constant too, `7 as Self`), and a float converts from any number. The range analysis takes that conversion as one that
can fail on both sides, so it is written with `try`; `Self.name(...)` and a bare `name(...)` call another static function
of the extension on the same `Self`. A static let is one constant for the family, so one whose type is the family's own
(`static let zero = 0.0 as Self`) is `E2015` when read through a typealias, which says to read it on the family and convert.
A static let of a generic type, in its body or in an extension of it, is one for each instance where its value names the
type's parameters or `Self` (see "Default parameter values, static fields, default throws and fallthrough"). One in an
extension of an interface is `E2015`: there would have to be one for each type that implements the interface, so it is a
static function there. An extension declares no field or destructor. Its typealiases are scoped as its methods are (see "Typealiases of an extension" under "Generics"). A method of an extension is called as one of
the type's, and is `E3006` if the type's body or another extension of the same module declares the name; a static let of an
extension is `E3006` where the type's body declares a member of its name (a method, a field, a case or a static let), or the
module's extensions a method or another static let of it.

An extension's declarations belong to the module that writes it, never to the extended type's module, so each module's
stages stay independent. A module of a program that writes a member which a module of the library declares, by an extension, or on one of its types where an extension of that module gives a conformance
(`arr.sort()`, `5.hash()`, `m.contains(k)`: its resolve reads the names of those methods), names each library module that declares one from there on, so the extensions of `Array`, `String`
and the integers are the program's without naming anything of the library, and a module pays for the library modules whose members it writes and for no other. A method of a type of a module whose extensions give no conformance names nothing: a value of that type comes from a declaration that names its module, which the module that writes the method reaches through it, and it sees nothing more by naming it. As in Hylo, an extension and the conformances it declares are scoped: they are seen from the
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
conformance and no static member (`E2015`). `for item in self` in it walks the conformer through the iteration protocol
of the interface's bounds (see "Iteration" under "Primitives"): `createIterator()` of `Iterable`, and `current()` and
`advance()` of the bound that the interface's `where` gives its iterator, checked once and lowered for each instance.

A `where` on an extension is the `where` of each method it declares, before the method's own: the methods are those of the
instances, or of the conformers, whose type arguments meet it, and the bound holds in their bodies. A call on a value
that does not meet it is maxon's `E4006`, in the verify stage: `Type 'Box' has no field named 'total' ('total' is
available as a conditional extension where Item implements Sizer, but 'Plain' does not implement 'Sizer')`. A `where` that
names no type parameter of the extended declaration is `E2015`.

**Conditional conformance.** An extension that says `implements` and has a `where` declares a conformance that holds at the
instances whose type arguments meet the `where`, in any code, a program's as the library's:

```maxon
extension Pair implements Hashable, Equatable where A implements Hashable and Equatable
	public function hash() returns HashValue
		var hasher = Hasher.create()
		hasher.combine(first.hash() as HashDigest)
		hasher.combine(second.hash() as HashDigest)
		return hasher.finalize()
	end 'hash'

	public function equals(other Self) returns bool
		return first == other.first and second == other.second
	end 'equals'
end 'Pair'
```

makes `Pair with Count` a `Map` key and an argument where `T implements Equatable` is asked, and leaves `Pair with Plain`, of a
record that is neither, without: a bound it does not meet is `E3017`, and its `==` or `equals` is `E4006`, as a method of a
conditional extension is. The condition is judged where the code that asks sees what implements what (`ast.ExtConform.conditions`,
`ast.conditionsMet`); a type parameter meets it, its code being checked against its own bounds. The requirements are met by the
type's own methods and by the methods that extensions add under that `where`; a method with a bound of its own beyond it meets
none (`E3016`). A conformance to an interface that extends `Copyable` (`Cloneable`) holds under conditions that make every type
parameter `Copyable`, as `extension Array implements Cloneable where T implements Cloneable` does. A type has one conformance to an
interface wherever it is used, so a second conditional conformance of it to the interface, whatever its condition, is `E3941`
as a second unconditional one is (with a method of the same name, `E3006` first). The orphan rule above holds for them alike:
a program may declare a conformance of a built-in type to an interface of the library, conditional or not, which holds in the
module that writes it (`extension Array implements Stringable where T implements Stringable`), and one that the library declares
already, `Array`'s to `Hashable`, is `E3941`. maxon's parser and checker accept the same declarations and honour the library's
conditions on `Array` (`E3017`, `E4006`); for a program's own generic type maxon's compiler checks no condition, and a use that does
not meet one stops in its lowering (see "Where it differs from Maxon" for two overlapping ones).

### Primitives

Layer 2 of [docs/regions.md](docs/regions.md), as far as step 3 goes: their signatures and ownership rules. An
`Array with T` owns its elements.
Their operations are generic methods that no source declares (`internal/builtingen/table.go`), typed, borrowed and moved
like any other method from the signature below, so none of the ownership rules has a special case for them. They are
declared in the stdlib module, so a name resolves to them last, after the module's own, the root's and an exported
one anywhere.

| Type | Operations |
|---|---|
| `FixedArray with N T`, `FixedArray with T` | maxon's fixed-size array (`stdlib/FixedArray.maxon`): an array whose count never changes, which its type states (`with 3 Int`, a literal from 1 to 4294967295, `E2069`) or, runtime-sized, its value. `create()` and `create(value: x)` where the type states the count, `create(n)` and `create(n, value: x)` where it does not (`E3194` for the other form), each zeroed where no `value:` is given and `T` has a zero value (`E3106`), and a constant `n` gives the type that states it; `Alias from [a, b]`, of exactly the count a counted alias states, or stating the literal's; `count()`; `get(i)` where `T` is `Copyable` and `set(i, value: v)` where `T` is `Droppable`; `refill(x)`, copies of `x` over every element. A get or a set at an index the range analysis proves inside the count cannot fail and compiles to the access with no check (see "Proven accesses"); for a type that states its count, a constant index outside it is `E3005`, and any other index throws `ArrayError.indexOutOfBounds` as an array's does, so it needs a `try` (`E3057`, naming the ranged type that would prove it). No slot of a fixed array is ever empty, so a read of one has no test for an empty slot. A `FixedArray with N T` is a subtype of `FixedArray with T`, so it goes where one is taken and a join of the two is the runtime-sized type. `for x in xs` walks it as an array; inside an `extension FixedArray` its `Self` states no count, and `countof(Self)` in a method is the receiver's |
| `Array with T` | `create(n)`, `n` zero elements, where `T` has a zero value or `n` is the literal `0` (`E3106`); `filled(n, value: x)` where `T` is `Copyable`; `count()`, `isEmpty()`; `get(i)`, a copy of the element, where `T` is `Copyable`, and `set(i, value: v)`, where `T` is `Droppable`, both throwing `ArrayError.indexOutOfBounds` past the end, except where the range analysis proves the index inside the count (see "Proven accesses"); `push(value)`; `append(other)`, a copy of each element of `other`, where `T` is `Copyable`; `insert(at, value: v)`, which clamps `at` to the count; `remove(at)` and `pop()` return `T` and throw `ArrayError`; `dropEmpty()` |
| `SlotKey with T` | no operations; `==` and `!=`. The library tiers alone (`E3185`) make one of a word, `SlotKey.fromWord(w)` through a typealias of the instance, and read its word, `key.word()`, a `Count`: each is the word itself, which a slot map of the library packs its slot and the slot's generation into |
| `__ManagedMemory with T` | maxon's raw buffer, which only the library tiers name (`E3185`): the box of an array, seen through maxon's buffer operations, each the array operation that does the same, so there is one implementation of each on either target. `length()`, `capacity()`; `get(i)`, a copy of the element, where `T` is `Copyable`, throwing `__ManagedMemoryError.indexOutOfBounds` past the end (a buffer is only ever an array's, which has no empty slot below its count); `set(i, value:)`, `swap(i, j: j)` (an exchange in place, no copy), `append(other)`, `remove(at)`, `clear()`, `slice(start, endIndex:)` (a copy), `grow(n)` (`reserve`), `setLength(n)` (`resize`), which are the array operations of the same name, and `__ManagedMemory.create(capacity, elementSize:)`, an empty buffer with room for `capacity`, typed by the call as "The raw surface is the library tiers'" says (see "Where it differs from Maxon" for what is not provided). `xs.managed` is the buffer of an array: the array itself seen as one, so it is borrowed, moved and dropped as `xs` is (`let m = xs.managed` moves `xs`) |
| `__SlotArray with T` | the slot array of the library's slot tables, which only the library tiers name (`E3185`): an array whose slots below its count may be empty. It is `Array with T` under another name, the same box and drop and copy, and each operation is the array operation of the same name at the same intrinsic: `count()`, `push(value)`, `get(i)` (a copy, where `T` is `Copyable`, throwing `ArrayError.emptySlot` at an empty slot) and `clear()`, as an array's. Its own are the operations that leave or meet an empty slot: `create(n)`, `n` empty slots, legal for every element; `adopt(array)`, the slot array of an array, every slot filled, which consumes the array and takes its box as it is, with no copy, for a library collection that takes an array literal as its slots; `clear()`, which drops the element of each filled slot, skipping the empty ones, and keeps the buffer; `dropEmpty()`, which ends a slot array whose every slot is empty and, unlike an array's, tests nothing, as the slot table that calls it tests its own count of elements; and `isFilled(at)`, whether a slot holds an element, which traps past the end as a subscript does (over a scalar an empty slot is the element 0); `clearSlot(at)`, which drops the element at an index and leaves the slot empty, a hash table's tombstone, and its two other halves, which drop nothing, so a slot table holds an element without `Droppable`: `fillSlot(at, value: v)` moves a value into an empty slot and traps past the end or at a slot that holds an element, and `takeSlot(at)` moves the element out, leaves the slot empty and throws `indexOutOfBounds` past the end and `emptySlot` at an empty slot; `lendSlot(at)`, provisional until the subscript goes and `get` lends, lends the element at an index with the access its caller has to the slot array (a place in it, see "Ownership"), trapping past the end and at an empty slot as a subscript does, the one unchecked access that a SlotMap's and a List's `get` lend through (of byte-sized elements, which it packs, the element's byte, see "Ownership", the bytes lent by address); and `createCursor()`, a `__SlotArrayCursor with T` at its first slot, which a SlotMap's and a List's iterators walk. A slot array has no `.managed`: a buffer is only ever an array's. An empty slot of a managed element is 0, maxon's, the empty place a drop does nothing with, so dropping a slot array drops only the full slots, and a copy keeps an empty slot empty; `get` and a cursor's `peek` of an empty slot throw `emptySlot`, and a read that lends it (`lendSlot`, a cursor's `current()`) panics with maxon's `empty slot: element N of the array was never filled`, so no element that is not there is ever lent. A plain `Array` has none of these operations, so it never has an empty slot below its count |
| `__GroupMemory with (A, B)` | minimaxon's raw buffer of the groups of a swiss table, which only the library tiers name (`E3185`), laid out as Go's map lays out its groups: each group is a control word and then eight pairs, the first half of a pair beside its second. `create(groups, control: w)` makes the groups, each control word `w` and each pair empty; `groups()`; `control(g)` and `setControl(g, value: w)` read and write a group's word as it is; a pair is named by its index over every group, eight to a group: `first(at)` and `second(at)` lend a half (a lent managed half that is empty panics, as an array's empty slot does), `put(at, first: a, second: b)` and `setSecond(at, value: b)` move halves in and drop what they held, `takeFirst(at)` and `takeSecond(at)` move one out and leave it empty, and `clear(at)` drops both. A group or a pair past the end traps. It is an array box of the words (`ir/groups.maxon`), so a read is a load; its drop and its copy go through each half by its type, an empty managed half being 0 |
| `__Builtins` | maxon's raw intrinsics, which only the library tiers call (`E3185`), each the built-in that does the same: `currentProcessId()` is `Process.id()`, `currentTimeNanos()` `Clock.nowNanos()`, `currentTimeMs()` `Clock.nowMs()`, `tickCountMs()` `Clock.osTickMs()`, `threadCpuTicks()` `Clock.threadCpuTicks()` (the calling green thread's CPU time, on whichever threads it ran, as maxon's: cycles on Windows, nanoseconds elsewhere), `currentUnixTimeSeconds()` and `currentUnixTimeNanos()` `WallClock.nowUnixSeconds()` and `WallClock.nowUnixNanos()` (the calendar, signed, from `GetSystemTimePreciseAsFileTime` and `CLOCK_REALTIME`; the interpreter's starts at a fixed second and moves by its clock's steps); `floatToBits(x)` is the 64 bits of a double as a `FullRangeInt`, a reinterpretation that the library's float hash folds, and `bitsToFloat(bits)` the double of 64 bits, the only way to make an infinity or a NaN, which `Math` uses; `fillRandom(buffer)` overwrites every byte of a buffer of bytes with the system's entropy (`rt.random.fill`, drawn as the hash seed is, from the system's entropy in one request, or several where a call is capped) and gives 0, which `Random` uses, a refusal of the system a panic where maxon's answers non-zero; `trailingZeros(word)` is the count of 0 bits below a `Count`'s lowest 1 bit, 64 for 0, the IR's `ctz`, by which `Map` reads the first match of a control word; `mulHigh(left, right)` is the high word of the 128-bit product of two `Count`s, the IR's `mulhi` (one `mul` on x64, `umulh` on arm64), which maxon lacks and minimaxon's `Poly1305` is made of; `hashWord(word, seed)` and `hashBytes(bytes, at, count, seed)` are maxon's seeded hash, its software fold of a word or of a span of a `ByteArray` under a `HashDigest` seed (`runtime/hashfold.maxon`, which the interpreter runs too, as `hashfold_gen.maxon`), a span past the end a panic; `readBigEndian(bytes, at, width)` and `readLittleEndian` the `OrderedWord` of the 2, 4 or 8 bytes at an index, the first the most or the least significant, throwing `ArrayError.indexOutOfBounds` when they are not all inside, and `appendBigEndian(bytes, word, width)` and `appendLittleEndian` the append of a word's low bytes in that order; the width is the literal 2, 4 or 8 (`E3005` otherwise, in maxon's words). `OrderedWord` and `HashDigest` are built-in `bits(64)` aliases, as maxon's library declares them (`stdlib/Builtins.maxon`, `Hasher.maxon`). `hashSeed()` is the fold of no bytes under the process's secret, which a `Hasher` starts from, and `drawHashSeed()` a fresh secret (`HashSeed.random()`): the first call makes the process's hash state, as maxon's runtime does, from the 16 hexadecimal digits that `MAXON_HASH_SEED` pins (a pin that is not exits 1 with maxon's message, an empty one is none) or from 8 bytes of the system's entropy (Windows' `ProcessPrng`, loaded by name only then, and Linux's `getrandom`), and a pinned process draws a fixed sequence of seeds from it; the interpreter draws an unpinned secret from its own random numbers, so a run stays deterministic. A program that never asks for the seed reads neither the variable nor the entropy. The gate pins `MAXON_HASH_SEED` for every program the test runners start (`cmd/gate`), as maxon's harness hands its programs the run's seed. The subprocess rows are maxon's `subprocess*` (`specs/subprocess-builtins.md`), the floor of its `Subprocess.maxon`, as far as their first step goes (`runtime/spawn.maxon`): `subprocessSpawn` takes fifteen words (the argv blob of NUL-ended tokens and `argc`, the working directory as a cstring, the environment block and `envInherit`, the kind and data of the standard input with its length, the kind, data and limit of the output and of the error, and the flags) and gives a handle, or -1 with `subprocessLastErrorCode` (the failed step above 32 bits, a code in Win32's words in the low ones, a POSIX errno mapped to 2, 3, 4, 5 or 31 as maxon's runtime does) and `subprocessLastErrorMessage` (`os error N`, empty while nothing has failed) saying why; `subprocessGetPid(handle)`; `subprocessWaitCollect(handle, ms)`, which parks until the child has exited and its output has ended, or until `ms` milliseconds (0 for none) have passed, which ends the child and keeps the output it had written; the readers of the result it gives, `subprocessResultStatusKind` (exited 0, signalled 1, timedOut 2, treeNotEnded 3, reapedElsewhere 4), `subprocessResultStatusCode`, `subprocessResultStdout` and `subprocessResultStderr` (a fresh buffer each), `subprocessResultDurationMs` and `subprocessResultRelease`; `subprocessReleaseHandle`; and `subprocessResolveOnPath`. A handle and a result are each a word that names a slot of the runtime's table and the generation that slot is in, as a `SlotKey` of the library's `SlotMap` does, so a word that names nothing live (`-1`, one never given, a released one, a child's where a result's is asked for) answers -1 (0 and an empty buffer for the readers of a result), never a fault. A spawn that this step does not serve (see "Where it differs from Maxon") is refused as a spawn that fails, and one whose words are malformed (a blob that is not `argc` tokens, a NUL in the working directory) as `spawnFailed`. `cpuHasAes()` and the AES and AES-GCM routines (`aesExpandKey`, `aesEncryptBlock`, `aesDecryptBlock`, `aesCtrXor`, `aesGcmInit`, `aesGcmSeal`, `aesGcmOpen`) are `runtime/aes.maxon` on the processor's AES and carry-less product instructions (AES-NI and PCLMULQDQ on x64, the cryptographic extension on arm64), probed on the first ask; under `--interpret` `cpuHasAes()` is `false` and a routine stops the program. `cpuHasSha()` and `sha256Blocks(state, data, at, blocks)` are `runtime/sha.maxon`, SHA-256's compression of 64-byte blocks into a 32-byte state of little-endian words on SHA-NI or arm64's SHA2 extension, probed the same way and refused the same way under `--interpret`. Each of their buffers, and `hashBytes`'s, takes a `FixedArray with Byte` of any count as well as a `ByteArray` (a row's parameter type `ByteBuffer` in `internal/builtingen`), as maxon's do |
| `__ManagedMemoryCursor with T` | maxon's position in a raw buffer, which only the library tiers name (`E3185`), made by a buffer's `createCursor()` at its first element, which fails with `__ManagedMemoryError.indexOutOfBounds` on an empty buffer. It holds a borrow of the buffer (see "Borrowing values"), so the buffer cannot change while it lives and it always names an element: `current()` lends it, `index()` is its index, `advance()`, `retreat()` and `seek(index)` move it (`mutates self`) and fail, leaving it where it was, past either end, and `peek(ahead)` is a copy of the element so many places on, where `T` is `Copyable` |
| `__SlotArrayCursor with T` | a position in a slot array, which only the library tiers name (`E3185`), made by a slot array's `createCursor()`: a buffer's cursor under the slot array's name, with the same operations, whose `current()` and `peek(ahead)` meet an empty slot as the slot array's `lendSlot` and `get` do |
| `__ManagedFile` | maxon's owned handle of an open file, which the library declares (`stdlib/file/managedFile.maxon`) where maxon's compiler does, and which only the library tiers name (`E3185`, by a row of the table's raw surface for a type the library declares): a record of the handle's word, the address of the runtime's box of the system's fd or `HANDLE` (`runtime/file.maxon`), whose one destructor is `close` (see "Capabilities and destructors"), so it has no copy and no drop and every path that opens one closes it or moves it on (`E3919`), as a record that holds one must. `__ManagedFile.openRead(path)`, `openWrite(path)` (made where it is not there, emptied where it is), `openWriteExecutable(path)` (the same, a file it makes getting the executable mode) and `openCreateNew(path)` (a file that must not be there, `alreadyExists`) open the file the bytes of a `__ManagedMemory with Byte` name, closed on exec on Linux and macOS and not inherited on Windows, and throw `__ManagedFileError`, maxon's (`notFound`, `accessDenied`, `busy`, `alreadyExists`, else `openFailed`). `readAppending(managed, most: n)` (`mutates self`, `mutates managed`) reads up to `n` bytes after the buffer's length, which grows by them, and gives the count, 0 at the end of the file; bytes that would run past the buffer's capacity are `readFailed` and nothing is read, as in maxon. `write(managed)` (`mutates self`) writes the whole buffer, as many calls as the system takes, and gives its length, `writeFailed` when the system refuses it. `sync()` (`mutates self`) forces the file's data to its device, as Go's `File.Sync` does on each system (`F_FULLFSYNC` on macOS, `fsync` where the file system has no `F_FULLFSYNC`; `fsync` on Linux, made again after a signal; `FlushFileBuffers` on Windows), and throws `writeFailed` with what the system reports, a handle that has no sync (a pipe or a FIFO) refused with the system's error, `EINVAL` on Linux; the file stays open whether or not it throws. Whether they wait on the event queue is decided once, at the open, as Go's `os.File` decides it: a FIFO, a socket or a character device on Linux and macOS, and a named pipe on Windows (a path `\\.\pipe\...`, opened overlapped), park the green thread that reads or writes it until the handle is ready; a regular file, and anything the event queue refuses (`/dev/null` on Linux), is read and written by plain calls in the blocking bracket. One owner holds the handle and a read or a write takes it `mutates`, so no two calls are on it at once. `close()` ends it whether or not it throws, closing the system's handle once, and throws `writeFailed` when the system's close reports an error, as Go's `Close` does, so what was written may not all be in the file; no close runs that the program does not write. The rows under it are `__Builtins.fileOpenRead`, `fileOpenWrite`, `fileOpenWriteExecutable`, `fileOpenCreateNew`, `fileReadAppending`, `fileWrite`, `fileSync` and `fileClose`, over its word. **A close and a sync each make one bracketed kernel call, never park the strand and never yield** (see "The close of an open file" in `runtime/README.md` for what it does on each system). The interpreter stops at each of these rows ("The interpreter") |
| `ArrayError`, `IterationError`, `__ManagedMemoryError` | maxon's error enums: `indexOutOfBounds` and `emptySlot`; `exhausted` and `atStart`, which the iteration protocol throws; a buffer's, whose first two cases are an array's |
| `ByteArray` | `typealias ByteArray = Array with Byte`, built in: the same type, with every operation of `Array`; `String.toByteArray()` is a new one of a string's UTF-8 bytes, and `String.from(bytes)` a string of a copy of them |

A fixed array is an array's box with its count of elements: the instance states the count as one more type argument after the
element, the integer constant (`ast.countedType`, `ast.statedCount`), spelled before the element in a type's name and its link
name (`FixedArray with 3 Int`), and a count the type states is a constant wherever the lowering reads it.

**`sizeof(T)` and `countof(T)`.** Both take a type. `countof` of a fixed array that states its count is that count, an integer
constant, and of any other type `E2015`. `sizeof` is the bytes of a value as minimaxon lays it out: 1 for a `bool`, a word (8)
for an integer, a float, an enum, a slot key and a function, a word to each field for a record or a tuple (its box), and a word for
any other value, which a place holds as the address of its box. Of a concrete type it is an integer constant; of a type that a
generic's parameters build it is a `Count` that each instance computes from its arguments.

An array of an integer type whose range fits in a `Byte`, or of an enum of at most 256 cases, holds one byte to an element,
natively and under the
interpreter, and any other array a word; the program cannot tell, except by how much memory it takes.

**Array and map literals.** `[a, b, c]` is an `Array` and `[k: v, l: w]` the standard library's `Map`, written on one line with at least one element:
`[]` is `E2015`, as in maxon (its element type cannot be inferred). A literal is what the explicit code
does: an empty collection, then a `push` or an `upsert` of each element in the order written, so each element is checked against
`value` (and `key`) as that call's argument is, moved in under the same rules, and a later `upsert` of a key replaces its value. The
element type is the one the context expects when it is an `Array with T` or the library's `Map with (K, V)` (an argument, a return, a field, an
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
a bare literal does. Any other type is written from an array literal or a string literal. `Type from [a, b]` is `Type.init([a, b])`:
the type says `implements InitableFromArrayLiteral with Element` (`E3005`, "Type 'T' does not conform to InitableFromArrayLiteral", at
the type's name, when it does not) and its static `init` takes the array of that element (`E3005` at the type's name when it
takes another). The elements are moved into the array `init` takes, `Set` and a user's type alike, so none is copied. The element
type comes from the head where it states the type's arguments (`Codes from [5, 6]` over `typealias Codes = Set with Code`),
and over the bare name of a generic type from the literal's elements, through the type argument that `with Element` names
(`Set from ["a", "b"]` is a `Set with String`; bare numbers keep their range as they do in a bare array literal), so a type
whose other arguments the literal leaves open is `E3179`. The empty literal gives no element, here as for an array
(`E2015`). A type is written from a string literal, `Type from "text"`, which is `Type.init("text")`:
the type says `implements InitableFromStringLiteral` (`E3005`, "Type 'T' does not conform to InitableFromStringLiteral",
at the type's name, when it does not) and declares `static function init(value String) returns Self`. `FilePath` is one:
`FilePath from "a/b"` is `FilePath.from` that stops the program with "FilePath: invalid path" where `FilePath.from` would
throw.

`create` and `filled` are static functions, called through a typealias of the instance (`Counts.create(0)`) or where
the type is known (`Shelf{items: Array.create(0)}`). An operation that changes the primitive takes `mutates self`, and one that adds an element takes it `consumes`.
`dropEmpty()` takes `consumes self`, ends the collection and traps if an element is left: it is how a collection of values with destructors, which cannot be
dropped, ends after every element was taken out. An array's is a test of its count, which is 0 once every element was
taken out; a library slot array's (`__SlotArray`) makes no test, as the slot table that ends it tests its own count of elements. Nothing built in returns an `Option`, and there is none built in.
- **Operations that fail throw.** `remove`, `pop`, `removeFirst`, `removeLast`, `first`, `last`, `next` and `prev` throw
  maxon's error, so a call of one is covered by a `try` like any other that throws (`E3057`), unless the range analysis
  proves it cannot fail (an `Array`'s `remove`, `pop`, `first` and `last`, see "Proven accesses"): `try a.pop() otherwise
  fallback`, or `try a.remove(i) otherwise panic("index out of range")` where a miss is a bug. The lowering tests first
  and fails as a compiled throwing function does, with the case plus 1 in the error register, so a failure allocates
  nothing.

- **Collections own their elements.** Adding takes the element `consumes`, so it moves in. Taking one out is `remove`,
  `pop`, `removeFirst` or `removeLast`, which move it out to the caller, owned; the only other way to hold an
  element is `.clone()`. `clone()` of a collection is deep.
-  **A subscript is a projection.** `a[i]`, `m[k]` and `l[h]` borrow the element: the array is indexed by a
  `Count`, a list by the `SlotKey` of the element. Read, it is a `let` borrow; assigned
  through (`a[i] = v`, `a[i].count = 3`) or passed to a `mutates` parameter, it is `mutates`. It is never an owned
  value: binding it, returning it or passing it to a `consumes` parameter is `E3901`, or `E3900` where the collection
  itself is borrowed, and `clone()` is the fix. A scalar element is copied like any scalar, so `let n = a[i]` is fine.
  The element of a temporary (`make()[0]`) cannot be moved or assigned either.
- **A miss traps**, as an out-of-range index, a missing key or a stale slot key: a range-check guard, safe, and it
  never touches freed memory. The operations that cannot miss let a program test first: `count`, `isEmpty` and `contains`.
- **Exclusivity.** A path gains a second kind of step, an element, and every element of a collection overlaps every
  other one, whatever the indexes say: `a[i]` and `a[j]` overlap, so `pair(a[i], b: a[j])` and `f(a, n: a[0])` are
  refused (`E3186`), while `pair(a[i], b: b[j])` and `pair(r.left[0], b: r.right[0])` are fine. A method with an
  `mutates self` receiver, such as `push`, `insert` and `remove`, overlaps every projection of its collection in the
  same call: `a.push(a[0])` is refused, and `let x = a[0]; a.push(x)` is the fix for a scalar. An index, a key
  or a slot key is an ordinary value, read before the call, so `a[a.count() - 1]` and `a.insert(a.count(), value: x)`
  are fine.
- **A borrow is held while the rest is evaluated.** Wherever lowering evaluates an operand to a borrow of a managed
  place, then evaluates the next, then uses the borrow, the place stays borrowed through the next. That is an argument
  while later arguments are evaluated, the target of `x.f = v` and `a[i] = v` while the value is, the left operand of a
  string comparison while the right is, the collection of a subscript while its index is, and each part of an
  interpolated string that is joined from what it reads, until the last part is (the parts are measured and joined once
  all are evaluated).
  A call in there that changes an overlapping place is refused (`E3186`): `look(a[0], n: drop(a))`, where `drop`
  takes `a` as `mutates`, `a[0].n = drop(a)`, `a[0] == wipe(a)`, `rows[0].cells[shrink(rows)]` (read, assigned,
  passed `mutates` or matched) and `"{p.name}{take(p)}"`. A scalar that is only read is a value by then and holds
  nothing, so `a[0].n + shrink(a)` is fine; so is a copy on the left, `a[0].clone() == wipe(a)`, or the other operand
  computed into a variable first. One helper, `hold`, takes every one of these, and a hold is only a push that is looked
  at when something in the rest changes or moves a place.
- **Slot keys.** A `SlotMap with T` is the library's keyed store of elements (`stdlib/slotmap/SlotMap.maxon`, minimaxon's
  own), unordered, not a cache of reusable objects: `insert(value)` gives the key of the element it stores, and the key
  finds it until `remove(key)` takes it out. `get(key)` lends the element, with the access its caller has to the map (see
  "Ownership"): through a `var` or a `mutates` parameter it is changed in place (`try nodes.get(key).visited = true`),
  through a `let` it is read; it throws `ArrayError.emptySlot` for a key that names no element, as `remove` does, so every
  `get` is written with `try`. `contains(key)`, `count()`, `isEmpty()`, `dropEmpty()` and a copy (`clone()`, the keys
  still valid in it) are the rest, and `for x in map` walks the elements in the order of their slots, deterministic and
  unspecified: a program must not rely on it, since a SlotMap is unordered. The elements lie in an array of slots, and a
  slot freed by `remove` is reused by a later `insert`. A `SlotKey with T` is a plain word: copy it freely, store it in a
  field, in an array or in another element, compare it with `==`. It owns nothing. It is the slot and the slot's
  generation, odd while the slot holds an element and even while it is free, so a key whose element was removed finds
  nothing, and a reused slot does not answer to the old key. Only `remove` makes a key stale: `insert` never does, though
  it may move the elements' storage, which is why a place `get` lends does not outlive a change of the map (`E3186`). A
  slot whose generation would pass the most a key holds is retired, never reused. A slot key of one slot map used on
  another slot map of the same element type is not a soundness issue: the lookup fails or finds that slot map's element at
  the same slot. A slot key of another element type is refused (`E3005`). A SlotMap of byte-sized elements (a byte range,
  an enum of at most 256 cases) lends each element's byte (see "Ownership"). A linked structure, whose nodes keep their places while others are linked and unlinked around
  them (a linked list, a tree, a graph, a cache in order of use), is a `SlotMap` of nodes that name each other by key, as
  `tests/run/lru-cache.maxon` builds one: a `List` is a sequence, with no key or handle of its own.
- **The List is the library's** (`stdlib/list/List.maxon`, minimaxon's own), with maxon's value API: `List with T` is a
  ring buffer over an array of slots, with O(1) `get(i)` and O(1) work at both ends, `prepend(v)` and `append(v)`
  (amortised: a full ring doubles), `removeFirst()` and `removeLast()`; `insert(at, value: v)` and `remove(at)` are O(n),
  moving the elements on the nearer side of the index. `get(i)`, `first()` and `last()` lend the element with the access
  their caller has to the list, as a SlotMap's `get` does: changed in place through a `var` or a `mutates` parameter
  (`try items.get(i).weight = 0`), read through a `let`; a scalar read out is a value, and anything else is copied only by
  `.clone()`. Each throws `ArrayError.indexOutOfBounds` past the last (or on an empty list), as `removeFirst()`,
  `removeLast()`, `remove(at)` and `insert(at, value: v)` past `count()` do, so each is written with `try`. `count()`,
  `isEmpty()`, `clear()` (which drops every element, so it needs elements that can be dropped: `where T implements
  Droppable`, as `insert` does, since a refused insert drops what it was given), `dropEmpty()` (which ends an empty list,
  as a list of values with destructors ends) and a copy (`clone()`) are the rest. A list grows, inserts and removes only by
  moving elements, so it holds an element of any type. `List from [a, b]` adopts the literal's array as its ring, and
  `for x in list` walks it front to back through the iteration protocol (`ListIterator with T`, a cursor over the ring), each
  element lent, the list held while the body runs (`E3186`). A place `get` lends does not outlive a change of the list. A
  List of byte-sized elements lends each element's byte, as a SlotMap of them does (see "Ownership").
- **The Map is the library's** (`stdlib/map/Map.maxon`), on the design of Go's map: `Map with (K, V)` where `K implements Hashable and Equatable`,
  with maxon's API: `create()`, `insert(k, value: v)` (`MapError.keyAlreadyExists`), `upsert`, `get(k)` (a copy,
  `MapError.keyNotFound`), `contains`, `remove`, `count()`, `getCapacity()` and `createIterator()`. A key's 32-bit `hash()` is
  spread to 64 bits under the map's seed (`__Builtins.hashSeed()`, which `MAXON_HASH_SEED` pins). A map of at most eight entries is
  one group; past that it is tables of at most 1024 slots, each a `__GroupMemory with (K, V)` (see the table above) of groups of a
  control word and eight pairs of a key and its value, and the directory names, for the hash's top bits, the table that holds
  the key. A lookup reads one control word, matches the low seven bits of the hash against its eight bytes at once by word
  arithmetic, and compares the key of each match; a probe goes on through the groups in triangular order until a group with
  an empty slot. A table grows by doubling when its entries and tombstones pass 7/8 of its slots, and one of 1024 slots
  splits into two by the next bit of the hash, the directory doubling where it must, so no insert rehashes more than a table.
  A removal leaves a tombstone only in a full group, and an insert into a table with no growth left first empties the
  tombstones that no probe passes, as Go's does. Its methods carry the bounds its body needs, which `fix` wrote: storing,
  growing and reading a value copy or move it, so `K` and `V` are `Copyable` and `Droppable` for `insert`, `upsert` and `get`.
  A key is `Hashable` and `Equatable`: an integer
  alias and a `float` through `PrimitiveExtensions`, a `String` through the library's extension (its `hash()` folds its bytes
  with `__Builtins.hashBytes`, its `equals` is the built-in one, which meets the requirement in place), and a type of
  the program that declares both (`E3017` otherwise).
- **The Set is the library's too** (`stdlib/set/Set.maxon`): `Set with Element` where `Element implements Hashable and Equatable`, a
  `Map with (Element, bool)` whose every value is true, so it shares the map's table and its probing, growth and
  tombstones instead of maxon's own open-addressing table. It has `create()`, `insert`, `contains`, `remove` (whether the
  element was there), `count()`, `getCapacity()` and `createIterator()`, and `for x in set` walks it in the map's order.
  `Set from [...]` is `Set.init` over the literal (see "Primitives", `Type from value`): the elements are moved into the table in
  order, so of two equal elements the first stays, as `insert` leaves it.
- **`Math`** is maxon's (`stdlib/math/Math.maxon`, converted), over `__Builtins.bitsToFloat` and `floatToBits` for the non-finite doubles.
- **Generic code** over a primitive is checked once, as in "Generics": a value of type `T` held by an `Array with T` is
  owned and movable, so a generic container gives an element out with `remove` or `pop`, or `.clone()`.
- **Iteration.** `for x in a` walks an array: each element in turn is lent to `x`, as `a[i]` lends it, so a scalar is
  copied and anything else is a borrow that cannot be moved out (`E3900`, `.clone()` is the fix). `x` itself is a `let`
  (`E2013`), and its fields are as writable as the element's: `x.n = 3` needs a mutable `a`, through `var` fields.
  While the body runs, a place the loop walks is held as a match subject is: changing or moving it is `E3186`, and
  `for x in a.clone()` is the fix. The lowering is a loop over the indexes, reading the element in place.
  `for (i, x) in a.enumerated()` walks it the same way and also binds each element's index, an `ElementIndex`, to `i`
  (`_` for the one the body does not use): the loop is the one over `a` with the index it counts given to `i`, so no
  pair and no iterator is made. `enumerated()` is a form of the `for` and not a method, and it walks whatever a `for`
  walks, each the way its own loop does: a `List`, a `String`, a `Map`, a `Set`, a range value and anything else with the
  iteration protocol, also through a type parameter's bounds or an interface value. There `i` counts the passes from 0,
  in a variable the loop adds one to as each pass ends. With one name it is `E2015`.
  `for x in xs.drain()` walks an `Array` by moving each element out of its slot to `x`, which owns it for the pass: the
  body may move it on, and a pass that does not drops it. However the walk ends, the array is left at count 0 with its
  buffer, so its capacity takes elements again: a walk that ends early, by a `break`, a `return` or a failure out of the
  body, drops the elements it has not reached, so they must be `Droppable` (`E3919`). `drain()` is a form of the `for`,
  as `enumerated()` is, over an `Array` alone (`E3005` otherwise). The array must be a place the function may change, a
  `var`, a `mutates` parameter or a field of either (`E2013` for a `let`), which the walk holds while the body runs: since the slots it has moved elements out of are empty, the body may not read it, change it or move it, nor
  use what holds it as a whole (`E3186`), though it may use the fields beside it. The lowering is the loop over the
  indexes, each element moved out of its slot, and the scope of the walk empties the array on every way out, dropping
  what is left in place, so a walk allocates nothing.
  A `String` is walked by grapheme cluster, as `count()` counts them (see "Text"): each cluster is lent to `x` as a view
  of the String's bytes, a `String` that owns none, so keeping one is `.clone()` (`E3900` otherwise), and the String is
  held while the body runs (`E3186`). The lowering is a loop over byte positions that reads the String's ASCII bit once:
  where it is set every cluster is one byte and the next starts at the next byte, and elsewhere `rt.str.graphemeBytePos`
  finds where the cluster ends. A pass allocates nothing: the view is two words made in registers.
  Anything else walks with maxon's iteration protocol, wherever its type has it: on a concrete type, through a type
  parameter's bounds (one bound that requires each method), or through an interface value's interface, whose
  requirements the loop calls through the table the value carries, one indirect call each for `current()` and `advance()`
  per pass and one for `createIterator()`, with no box made. A cursor declares `current()`, which gives
  the element and cannot fail, and `advance(mutates self) throws IterationError`, which fails past the last one; a
  collection declares `createIterator()`, which returns a cursor and may throw `IterationError` where it is empty. A
  loop over a cursor moves it on in place (a place it is must be mutable, and is held while the body runs); a loop
  over a collection calls `createIterator()` once and owns the cursor, and holds the collection while the body runs where
  the cursor borrows it (see "Borrowing values"). Each `current()` is a value of its own, owned by
  `x` until the pass ends, or, where `current()` returns borrowed (a requirement or a method), lent to `x` as an array's
  element is: a borrow of the cursor, which the loop holds, so moving it is `E3900` and `.clone()` the fix. A throw of `createIterator()` or `advance()` ends the loop, and a `createIterator()` that
  throws another error is `E2015`, as is a loop over a value whose type has no protocol (a number), in maxon's
  words, which for a type parameter or an interface value name the requirement to add. A string's `bytes()`,
  `codepoints()` and `utf16()` views are such collections.
  A `Map` is walked by its `MapIterator`, table by table in the order the map made them and each in the order of its slots: `current()` lends the entry as a tuple of two
  borrows, the key's and the value's slots (see "Tuples"), so `for (key, value) in m` binds the two as borrows of the
  table, read-only, `_` for one the body does not use, and `for entry in m` binds the pair. The map is held while the
  body runs (`E3186`). The loop is the iteration protocol's: `createIterator()` once, then `current()` and `advance()` per
  entry, `current()` writing the two borrows into two words of the loop's frame, so a pass allocates nothing.
  A range value is maxon's: `a to b` makes the library's `Range` and `a upto b` its `OpenRange` (`stdlib/range/Range.maxon`,
  converted), by a call of the type's static `create` with the two bounds, which are `RangeBound`s
  (`int(i64.min to i64.max)`), so a float or a wider integer bound is refused as an argument of the wrong type is. The
  operator binds looser than `or` and tighter than `x if c else y`, so `1 upto n + 1` is one range over a computed bound
  and `r if c else s` picks one of two ranges; it is the node `RangeExpr`, which resolve binds to the library's type, as a
  map literal is bound to `Map`, so a type of the program named `Range` does not shadow it. Both types implement
  `Iterable` with a `RangeIterator` (`current()`, `index()`, `advance()`, `retreat()`), and have what maxon's library
  gives every `Iterable` (`map`, `filter`, `containsWhere`); maxon's have no `contains`, `count` or clamping either. A
  `for` walks one through the iteration protocol, as any other value whose type has it. At the top of a `for` header, after
  its `in` and outside any bracket, `to` and `upto` are the counted loop's own (the parser's `rangeIsValue`): that loop
  makes no range and is lowered to the plain loop over a counter, so `for i in 0 upto n` costs what it always did, and
  `for i in (0 upto n)` walks a range value.
  `E.allCases` is a new `Array` of an enum's cases and `E.allCaseNames` one of the names of the cases of an enum or a
  union, `Array with String`, in declaration order; both are read before a case of the same name. `allCases` of a union
  is `E4006`. A loop over either walks the array as any other (`for c in E.allCases` is a loop over a fresh array).
- **Running them.** An array runs natively: heap-allocated, bounds-checked, and freed with what it holds where its owner
  dies (see "The primitives in the IR" and "Native freeing").
- **Limits.** A primitive implements no interface (`E3017`). A record
  that contains itself through an array (`var kids as Array with Tree` in `Tree`) is fine, natively too: its drop and
  its copy are routines that call themselves.

### Raw values

A case of an enum, and of a union, may write a raw value after `=`: an integer, a float or a string with no interpolation.
The first case settles what they are and every later one agrees (`E3032`: `raw value type mismatch: 'expected int, got
float'`), where a case that writes none is an integer, the one after the case before it, from 0; no two cases have the same
value (`E3031`). `.rawValue` has the type of the values: a `Count` or a `FullRangeInt` for integers, a `FullRangeFloat`, a
`String`; but the `.rawValue` of a case written out (`Flag.first.rawValue`) of an enum or a union whose raw values are
integers is that integer as a constant with no alias, as maxon types it, so it takes the alias of the operand it meets
(`flags or Flag.first.rawValue` is a `Flags` where `Flags = int(0 to 15)`, and the value must fit it), while the
`.rawValue` of a value that runs stays a `Count` or a `FullRangeInt`. A float or a string raw value is read when the program
runs in maxon too, so it stays a `FullRangeFloat` or a `String`. An enum is stored as its case's index, so a raw value is
found by a switch on it, and a union value's tag is its case's index while its `.rawValue` is the tag the case writes (`add(dest Id) = 5`), and its `.ordinal` the index.
Interpolating an enum value that has raw values writes the raw value, and an enum with none the name of the case.

**`fromRawValue`.** `Color.fromRawValue(raw)` is the case of an enum that writes `raw`, as maxon's is: a call of the type's
name that throws `__EnumLookupError.noSuchRawValue` (maxon's, declared by no library file, with `noSuchCaseName` after it) when
no case writes it, so it needs a `try`. An enum that writes integers, or none, takes an integer of any range, one that writes
floats takes a float and never an integer (compared by its bits, so a zero and its negative are two raw values), and one that
writes strings takes a `String`; any other argument is `E3005` at the argument, as `expected int, got String`. A constant
that no case writes (`HttpStatus.fromRawValue(999)`) is `E3034` at `fromRawValue`, and the call stands for the `try` that
covers it otherwise. The member is read before a case or a function of the same name. A union has none, nor an enum whose
raw values are records (`E3034`, `unknown union case: 'fromRawValue'` or `unknown enum case: ...`, at the type's name), and an
enum whose raw values are functions refuses it as `E2015`. The call is a static call of a function that only it has, of the
intrinsic `EnumFromRawValue`, which lowers to a compare of the argument with the raw value of each case in turn, the
case's index where one is equal and the failure where none is (`ir/enumlookup.maxon`).

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
them, and `sliceBytes(start, endPos: end)` by bytes, `contains`, `startsWith`, `endsWith`, `equals` and `isEmpty` ask,
`replace(old, with: new)` and `replaceFirst` leave a string with an empty or absent `old` as it was, `trim(chars)`,
`trimStart(chars)` and `trimEnd(chars)` remove the clusters a `CharacterSet` holds (`CharacterSet.whitespacesAndNewlines`
when it is left out) at either end, the start or the end, written in the library (`stdlib/characterset/StringTrim.maxon`, whose one scan
forward over the clusters finds both ends, since a String has no index at a byte position to scan back from) and each a
view of the receiver (`returns borrowed String`); the compiler, which is built without the library, trims its own ASCII
text with `table.blankTrimmed`, and the runtime's whitespace scans the seed's `trim` lowers to go with the next seed,
`toLower` and `toUpper` map the ASCII letters only, as maxon's do. `split(delimiter)` gives the pieces between the delimiters as an array of copies, since an array holds no view
(`stdlib/string/StringSplit.maxon`), `indexAfter(index)` the index of the next grapheme cluster (`StringError.invalidIndex` at the
end), and `byteAtOrPanic(i)` is `byteAt(i)` under the name maxon's library calls it. The library tiers' doors to the bytes as
a buffer, `addressableBytes()` and `String.init(managed)`, copy them, where maxon's lend and wrap the String's own buffer: a
String owns its bytes, which the String itself may hold where they are few, so there is no buffer to lend. A slice and a trim are a view: a String of the receiver's
bytes that owns none, made in place with no allocation (`rt.str.view`: the address, and a word of the length, the
receiver's ASCII bit and the view bit), which the call lends as a function that `returns borrowed` lends a place (see
"Ownership"), so it is kept only by a copy, `.clone()`; the copy of a view copies its bytes, where the copy of a literal is
the literal. A `var` bound to a view is given views of the same String as code narrows what it reads, with no copy. A String is built by interpolation: there is no `append` and no `StringBuilder` (see
"Where it differs from Maxon"). An interpolation writes an integer, a float, a bool, a String, an enum, a union and a
`FilePath` itself, and a value of any other type that has a `toString()` giving a String, a type parameter's through its
bound, by calling it (`Typing.hooks`). An interpolation measures its parts (a String's length, a literal's
known one, an integer's digit count by `rt.decimalLength`), allocates once (`rt.str.joinRoom`) and puts each part in place
(`rt.str.joinPut`, and `rt.str.joinDigits`, which writes an integer's digits into the buffer with no String of their own),
its ASCII bit the AND of the parts'; an interpolation that is a part of another with no format of its own gives the outer
one its parts (`self/ir/joins.maxon`). A part that is an unquoted String, or a `FilePath`, is joined from the String it
reads, kept as a handle until the one join after the last part, so the place it reads is held until then; the checker's
ownership pass holds each such part the same way, nested parts included (`strLitOwn`), and both ask the same question of a
part, `ast.textPartBorrows` (with `ast.nestedTextParts` for the parts a nested literal gives), so the two cannot disagree on
which parts are read at the join. A later part that moves or changes the place is refused (`E3186`), whichever part it is:
`"{s} {take(s)}"` as much as `"{s}{take(s)}"`. A part of a type parameter is held as a String would be, since an instance
may make it one. An assignment whose value is an interpolation that starts with the assigned place
itself, `s = "{s}..."` (a local, a field of a place, or an element at a literal or immutable index, through a `mutates`
parameter too), appends: the assignment consumes the old String, so where its owned bit is set (not a literal, whose bytes
are the executable's) and its box has room (`boxCapacity`, the size of the heap's slot or large block, read from the box's
header) the parts go in after its bytes, and otherwise it moves to a buffer the growth policy sizes; the old buffer is
freed only after every part is in, so a later part that reads the place, as in `"{s}{s}"`, reads the old bytes. The
growth policy is maxon's (`__managed_grown_cap`, Go's `nextslicecap`) and the only one, for an array, a byte array and a
String (`grownCapacity`, `runtime/arrays.maxon`): below 256 a buffer doubles, above it the step is
`(capacity + 3 * 256) / 4`, easing toward 1.25, an ask that outruns doubling is taken as it is, and no capacity is below 4.
Their code is `runtime/textops.maxon`, which the executable links and `builtingen` copies to `self/interp/textops_gen.maxon`
for the interpreter, so one text says what each means; a search gives its position plus 1 so that 0 is no match.

Clusters follow the rules of UAX #29 (GB1 to GB9b, GB11, GB12 and GB13) from the Grapheme_Cluster_Break and
Extended_Pictographic values of Unicode 16.0.0, which `internal/textgen` writes from the data files in its `ucd/`
directory to `runtime/unicode_gen.maxon` and `self/interp/unicode_gen.maxon`: a string of the first code point of each run
of one class and its class, searched by halves, with the Hangul syllables computed from their place in the block. Maxon
joins only the marks in a few ranges; here every Extend, SpacingMark and Prepend code point joins. The Indic conjunct rule
GB9c is not implemented. `StringIndex` has no `equals` or `compare`; `String` has no `split` yet.

A `Character` is one grapheme cluster, written `'A'`, `'é'`, `'🎉'`, `'\''` or with a string's escapes but for `\{` and `\}`; a literal
that is not one cluster, or has an escape it does not know, is `E2016`, and a malformed or surrogate escape is `E1004`. The
quotes are read as maxon's one token is, by place: where an operand stands, a quoted word is a character, and where a label or
a test name does, it is that (`try 'x'.m()` calls a method of a character, as a `.` after the quote says, and `try 'x'` opens
a block). A literal that is the whole of an operand beside an integer is its code point (`cp - '0'`, `cp == '-'`, `'A' + 1`
is 66, and a `match` arm on an integer), where its cluster is one code point: a `Codepoint` in an operation that is no
comparison and of which neither operand names an alias, as maxon's is, and otherwise a constant; a Character that is no literal never
converts, and `'a' == "a"` is `E3005`. Characters are equal and ordered by their bytes, interpolate as their text (`{c:q}` in
single quotes), match by literal and by range (`'a' to 'z'`), and have `byteLength()`, `codepoint()`,
`asciiValue()` (`CharacterError.notAscii`), `equals`, and `bytes()` and `codepoints()`, views of a String of its bytes as maxon's are
of its `toString()`, which `"{c}"` is here (see "Where it differs from Maxon"). `advanceBy(n)` (a `CodepointDelta`, `int(-1114111 to 1114111)`) steps through the scalar values, so the
surrogates are no step, and `Character.fromCodepoint(cp)` encodes one; each panics, as maxon's does, on a surrogate, which a
`Codepoint` can name, and `advanceBy` past the first or the last scalar value. `for c in 'a' to 'z'` (or `upto`), both bounds
literals, counts through the scalar values between them and gives each as a Character; with one integer bound the range is
of integers, the literal's code point. A Character is `Hashable`, `Equatable`, `Comparable` and `Cloneable` through the library's
`stdlib/core/Character.maxon` (minimaxon's own, since maxon's declares the type that is built in here; its surrogate code points are `stdlib/character`), so it is a `Map` key, a `Set`
element and sorts; its hash is the hash of the String of its bytes, which for one held in its two words is one fold of the words. `String` has `charAt(index)`, the cluster at an index as a Character, and `containsCharacter(c)`.
maxon's `Ascii` is converted from maxon's library (`stdlib/ascii`), and so is its `CharacterSet` (`stdlib/characterset/CharacterSet.maxon`).
Its predefined sets are static lets whose `Set` literal is built where they are read; the module that reads one makes the
instances the literal calls, since finding a module's instances follows a read of another module's constant to the calls of
its value, in its module (`ir.visitConstant`). What it reads is `unicodeGeneralCategory`, maxon's helper, over the
General_Category table of every code point, which `internal/textgen` writes from `DerivedGeneralCategory.txt` to
`stdlib/characterset/unicodeCategory_gen.maxon`, the helper module of maxon's `stdlib/helpers/string/`. A Character is two words, laid out as a String is: one of 1 to 15
bytes holds them in its two words as every String of so few bytes does (see "The IR"), so its words are the words of the
String of its bytes, it owns nothing, its copy and drop are the two words, and making a String of it copies the words; a
longer one is a String of its bytes, a literal's in the executable's data or a box it owns. `runtime/characters.maxon`'s
entries read one as the String of its words.

`bytes()`, `codepoints()` and `utf16()` give maxon's views `ByteView`, `CodepointView` and `UTF16View`, whose elements are
a `Byte`, a `Codepoint` (`int(0 to 1114111)`) and a `CodeUnit16` (`int(0 to 65535)`). A view has `count()` and walks with
the iteration protocol (see "Iteration"): a `for` over it, or `createIterator()` and an iterator's `current()` and
`advance()` as maxon's. Unlike maxon's they are not lazy: a view and an iterator each hold a copy of the string, so
changing the string leaves them as they were. A view is no `Array`, as in maxon.

A `String` is walked by `for c in s` cluster by cluster, each a view of its bytes (see "Iteration"), and implements the
library's `Iterable with (String, StringIterator)` (`stdlib/core/StringIterator.maxon`, minimaxon's own), so a type parameter
bound by `Iterable` and an `Iterable` interface value walk it too: its `createIterator()` gives a `StringIterator` that
holds a String cursor (`__StringCursor`, see "Borrowing values"): the String's two words, which it borrows and does not
copy, and where the cluster it is at starts and ends. `current()` lends that cluster as a view, as the loop does, and
`advance()` finds where the next one ends with `rt.str.graphemeBytePos`.

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
| `Process.exit(code)` | — (never returns: the process ends at once with the code, nothing dropped and nothing else finishing; the ordinary end is a return from `main`; see "Where it differs from Maxon") | — |
| `Process.enterBackgroundPriority()` | `FullRangeInt` (maxon's `BackgroundPriority`), the priority the system reports after the call lowered the process's own: Windows' priority class, 16384 (`BELOW_NORMAL_PRIORITY_CLASS`, `SetPriorityClass`, read back by `GetPriorityClass`), and a nice value, 10, on Linux (`setpriority(PRIO_PROCESS, 0, 10)`, read back by `getpriority`, which the kernel answers as 20 minus the nice value) and on macOS (libSystem's `setpriority` and `getpriority`); the processes and threads started afterwards inherit it (on Linux a thread already running keeps its value; macOS scopes it to the process). Asking twice sinks no further. The interpreter does not change its host's priority, so a program that asks is `// native-only` | — |
| `Clock.nowNanos()` | `InstantNanos`, the monotonic clock in nanoseconds; only differences of two readings mean anything (see "Where it differs from Maxon"). The library's `stdlib/clock` extends `Clock` with maxon's `nowMs`, `osTickMs`, `threadCpuTicks`, `elapsedMs`, `elapsedNanos` and `remainingMs`, and declares `WallClock` and `CivilDate` | — |
| `Scheduler.processorCount()` | `Count`, the workers the runtime runs behaviours on: `MAXON_MAX_PROCS` when it holds a count, else the machine's processors (`GetActiveProcessorCount`); maxon's `SchedulerProcessorCount` is the same unsigned 64-bit integer; the interpreter has one worker and answers 1 | — |
| `FilePath.from(path)` | `FilePath`, with `/` made `\` | `FilePathError.invalidCharacter` for a control character or one of `<>"\|?*` |
| `path.toString()` | `String` | — |
| `File.readText(path)` | `String` | `FileReadError` |
| `File.readBinary(path)` | `Array with Byte`, the whole content of the file, read as `readText` reads it (the same system calls; the bytes are copied from the read buffer to the array) | `FileReadError` |
| `File.writeText(path, content: s)`, `File.writeBinary(path, content: bytes)`, each with an optional `mode: FilePermission` | — (a file that is there is replaced, and keeps its mode) | `FileWriteError` |
| `File.createText(path, content: s)` | — (maxon's, the library's (`stdlib/file/File.maxon`), over `FileWriter.openNew` and `writeAllText`: makes a file that must not be there, in one step, `CreateFileW` with `CREATE_NEW`, `open` with `O_CREAT \| O_EXCL`; one that is there fails `alreadyExists` and is left as it was; a write that fails after the file was made deletes it, and the error is the write's, or `partialFileLeft` when that delete fails too and not as `notFound`); the interpreter runs it (see "The interpreter") | `FileWriteError` |
| `File.openRead(path)` | `FileStreamReader`, maxon's `stream.StreamReader with FileSource` (`stdlib/file`): lines, exact counts of bytes and the rest of the file, read as the file's reads are (see "An open file", below); it is ended with `reader.source.close()` | `FileReadError` |
| `FileWriter.open(path)`, with an optional `mode: FilePermission`, and `FileWriter.openNew(path)` (a file that must not be there, `alreadyExists`) | `FileWriter`, maxon's (`stdlib/file/fileWriter.maxon`): `write(bytes)` writes all of them; `close()` ends it; `writeAll(bytes)` and `writeAllText(s)` write and then close, and end it too | `FileWriteError` |
| `File.rename(from, to: dest)` | — (what is at `dest` is replaced) | `FileRenameError` |
| `File.delete(path)` | — (a directory is refused, `accessDenied`) | `FileDeleteError` |
| `Directory.delete(path)` | — (removes an empty directory: one that still holds an entry is refused, `notEmpty`, a file or a link there is `deleteFailed`, and a path through a file `notFound`; `RemoveDirectoryW`, `rmdir`, and `unlinkat` with `AT_REMOVEDIR` on arm64 Linux, which has no `rmdir`; the interpreter runs no `Directory.delete`, so a program that calls it is `// native-only`) | `DirectoryDeleteError` |
| `File.info(path)` | `FileInfo`: `size` (maxon's `FileSize`), `modifiedTime` and `accessedTime` (maxon's `Timestamp`: Unix seconds on the file system's clock), `createdTime` (a `FileCreatedTime`, a built-in union of `time(value Timestamp)` and `notReported`, since not every system records when a file was made), `isDirectory`, `isReadOnly`; `createdTime` is `time` of the file's creation time on Windows (`FindFirstFileW`) and of `st_birthtime` on macOS, and on Linux of `stx_btime` from `statx` with `STATX_BTIME`, or `notReported` when `stx_mask` lacks the bit (a file system that records none, such as procfs, sysfs and WSL's `/mnt/c` and the like) or the kernel has no `statx`: the inode change time (`st_ctime`) is never given in its place, and the rest of the `FileInfo` is read from `newfstatat` as before, and the interpreter gives what the host does; `FileSize` and `Timestamp` are built-in aliases of `int(0 to u64.max)`, as `Count` is, but types of their own (a `Count` is cast to one with `as`), and the library's `stdlib/file` gives `FileInfo` maxon's `FileInfo.create(size, modifiedTime:, createdTime:, accessedTime:, isDirectory:, isReadOnly:)`, whose `createdTime` is a `FileCreatedTime`; read from the directory's record of the file (`FindFirstFileW`), so a file another process holds open is no failure | `FileInfoError` |
| `Directory.isDirectory(path)` | `bool` | — |
| `Directory.create(path)` | `bool`, whether the directory is there after it; the missing parents are made too | — |
| `Directory.currentPath()` | `FilePath`, the working directory; a panic when Windows cannot give it | — |
| `Directory.list(path)` | `Array with FilePath`, each the path joined to an entry's name (no `.` or `..`) | `DirectoryListError` |
| `Subprocess.run(executable, arguments: args)` | `CollectedOutput`: `exitCode()`, `succeeded()`, `stdout`, `stderr` | `SubprocessError` |
| `Subprocess.runWithDeadline(executable, arguments: args, deadline: d)` | as `run`; a child still running when `d` (maxon's `WaitDeadline`: `WaitDeadline.none` or `WaitDeadline.within(ms)`) passes is terminated | `SubprocessError`, `timeout` for the deadline |
| `Subprocess.runInDirectory(executable, arguments: args, directory: p, deadline: d)` | as `runWithDeadline`, the child starting in the directory `p`, relative to the caller's working directory: maxon's `run` with `workingDirectory: WorkingDirectory.at(p)` | `SubprocessError`, `spawnFailed` for a directory the child cannot start in |
| `Scheduler.pinToMachine()`, `Scheduler.unpinFromMachine()` | `Bool`, and —: maxon's, Go's `LockOSThread` and `UnlockOSThread`. The calling green thread runs only on the thread running it now, and that thread runs no other green thread, until as many unpins as pins; a green thread that finishes pinned frees its thread, and an unpin with no pin is maxon's fatal error (`fatal error: a machine pin was released by a green thread that holds none`, exit code 70, `runtimeFatal`). A pin answers whether there is a machine to pin to, which in a program without the scheduler is its one thread, whose pins the link's single-thread forms count (see "Main with or without the scheduler"); the interpreter runs one machine, so a pin there holds trivially, and it counts each thread's pins for the depth and the fatal error | — |
| `Debuggee.launch(executable, arguments: args, outputToError: b)` | `Count`, the process id of the program started as the caller's debuggee (`DEBUG_ONLY_THIS_PROCESS`), its standard input the caller's and its output the caller's own or, with `b`, the caller's error, as its error is | `SubprocessError` |
| `Debuggee.waitEvent(budgetMs)` | `Array with Byte`: the `DEBUG_EVENT` that came within the milliseconds as the system wrote it, or none | — |
| `Debuggee.continueEvent(processId, thread: id, disposition: d)` | `Bool`; `d` is maxon's `DebugDisposition`, `handled` or `unhandled` (an exception handed on to the program) | — |
| `Debuggee.readMemory(process, address: a, count: n)`, `Debuggee.writeCode(process, address: a, content: bytes)` | `Array with Byte`, and —: `writeCode` lifts the protection of the code and puts it back, and flushes the instruction cache over it | `DebuggeeError` |
| `Debuggee.readContext(thread)`, `Debuggee.writeContext(thread, context: bytes)` | `Array with Byte`, the thread's `CONTEXT` (control, integer and floating-point registers), and — (`outOfBounds` for bytes of another size) | `DebuggeeError` |
| `Debuggee.suspend(thread)`, `resume(thread)`, `breakIn(process)`, `detach(processId)`, `terminate(process, exitCode: c)`, `closeHandle(handle)` | `Bool` (whether the system did it), and — for `closeHandle` | — |

- **An open file is a linear value.** A `FileWriter`, a `FileSource` and a reader over one hold the library's
  `__ManagedFile`, which has no copy and no drop, so neither has any (see "Capabilities and destructors"): every path that
  opens one ends it with one of its destructors, a close or one of the writes that close, or moves it on, and leaving it
  open is `E3919`. Nothing closes a file that the program does not close. A close throws what the system's close reports
  (`FileWriteError.failed`, `FileReadError.failed`), and the file is closed whether or not it throws. A failure after the
  open must end the file before it goes on, so two writes and a close are written:

  ```
  var writer = try FileWriter.open(path)
  try writer.write(first) otherwise (e) 'first'
  	try writer.close() otherwise ignore
  	throw e
  end 'first'
  try writer.write(second) otherwise (e) 'second'
  	try writer.close() otherwise ignore
  	throw e
  end 'second'
  try writer.close()
  ```

  where a handler's close drops its own error so that the write's is the one thrown, and the last close's error is the
  caller's. One write and a close is `writeAll`, or `writeAllText`, which end the writer either way. A `FileWriter` has a
  `sync()` too, which forces what it has written to the device and throws `FileWriteError.failed` where the system refuses or
  fails it, the writer left open (see "Where it differs from Maxon", a file's sync).
- **`FilePath`** is a record holding the path's `String` (`path`), which every `File` and `Directory` function takes. It
  is made with `FilePath.from`, or from a string literal with `FilePath from "text"`, which panics where `from` would throw.
  The rest of maxon's `FilePath` (`join`, `parent`, `filename`, `fileExtension`, `stem`, `normalize`, `resolve`,
  `relativeTo`, `isInside`, `startsWith`, `equals`, `hash`...) is maxon's, converted as an extension of the built-in record
  (`stdlib/filepath/FilePath.maxon`), less `resolvedPhysically`, which needs a raw directory call there is not.
  The built-in record goes once a seed compiles the library's type: then its `from`, `init` and `toString` rows go, the
  conversion writes the type rather than an extension, the OS rows (`File`, `Directory`, `Process.executablePath`,
  `Subprocess.runInDirectory`, `FilePathArray`) find the library's `FilePath` by its name at the root, as a map literal finds
  `Map`, and `self/`'s `path.toString()` calls become interpolations.
- **`Process`** is a built-in record with the rows above, and the library's `stdlib/process` (`Process.maxon`, converted) extends
  it with maxon's `currentEnvironmentEntries()` (`StringArray`, one `NAME=VALUE` entry per element in the order the system
  reports them, `environmentUnreadable` where it hands over none), `envEntryName(entry)`, `envEntryValue(entry)` (an entry
  divides at its first `=` after byte 0, as a Windows entry `=C:=C:\dir` is named `=C:`), `envNameKey(name)` (lowercased on
  Windows, as given elsewhere), `EnvNameValueSeparator` and `BackgroundPriority`. The walk is `__Builtins.osEnvironmentEntry(index)`
  (`rt.os.environmentEntry`, `runtime/environment.maxon`), a fresh buffer of bytes for the entry at an index, empty past the
  last and one NUL byte where the system has no environment, neither of which an entry is: `GetEnvironmentStringsW` on
  Windows, converted from UTF-16 entry by entry, and on Linux and macOS the vector of entries the process started with.
  maxon's `environmentVariable`, `executablePath`, `id` and `enterBackgroundPriority` stay the built-ins' own, which read the
  system directly where maxon's `environmentVariable` walks the entries. A `__ManagedMemory` of bytes has maxon's `byteAt`, which is its `get`.
- **Errors** are maxon's enums, cases and all: `FileReadError` (`notFound`, `accessDenied`, `busy`, `failed`),
  `FileWriteError` (`failed`, `notFound`, `accessDenied`, `alreadyExists`, `busy`, `partialFileLeft`),
  `FilePermission` (`normal`, 0666, the default of `mode:`, and `executable`, 0755, which only Linux's runtime has an effect for),
  `FileRenameError` (`failed`, `notFound`, `accessDenied`, `busy`, `alreadyExists`), `FileDeleteError` (`notFound`,
  `accessDenied`, `busy`, `failed`), `DirectoryListError` (`notFound`, `accessDenied`, `listFailed`),
  `DirectoryDeleteError` (`notFound`, `notEmpty`, `deleteFailed`),
  `ProcessIntrospectionError` (`pathUnavailable`, `variableUnset`, `environmentUnreadable`), `FilePathError`
  (`invalidCharacter`, `notFileURL`, `malformedURL`, `noParent`) and `SubprocessError` (`executableNotFound`,
  `spawnFailed`, `busy`, `ioFailed`, `timeout`, `treeNotEnded`, `detachRefused`, `inputTooLarge`, `statusLost`). Which
  case a failure is comes from the Win32 error, as maxon's runtime tells them: not found (2, 3), access denied (5),
  busy (32), already exists (80, and 145 for a directory that is not empty), and the operation's catch-all for any other. Cases that nothing here fails with are
  kept so that a `match` is the one maxon needs. Each of the five `File` errors has maxon's `failure()`, which answers a `FileFailure` (`notFound`, `accessDenied`, `alreadyExists`, `busy`, `failed`; `partialFileLeft` is `failed`) whichever operation failed: the library's `stdlib/file`, converted from maxon's `File.maxon`, extends the built-in enums, as `stdlib/directory` extends `Directory`, and declares `FileFailure`; `FileSize` and `Timestamp` are built in, since `FileInfo`'s fields have them.
- **`Subprocess.run`** takes an `Executable`, maxon's union: `Executable.name(n)`, searched for as Windows searches the
  first word of a command line, or `Executable.path(p)`. Each argument is quoted as the C runtime's
  `CommandLineToArgvW` splits it back: one that is empty or has a space, a tab or a quote is quoted, as maxon's runtime
  decides it, and a quote is escaped with a backslash after doubling the backslashes before it (`runtime/commandline.maxon`). The child's standard output and error are pipes,
  which the run drains in turn without waiting on either, so a child that fills one cannot block; its standard input is
  the program's. A name that is not found is `executableNotFound`.
- **`Debuggee`** is the host's debug interface that the compiler's debugger drives a program through from outside it (maxon's
  `DebugPort` of `stdlib/Debuggee.maxon`), on Windows only: on another system each call panics, as maxon's `windowsDebugCall`
  does. Only the thread that launched the debuggee may wait for its events, so the caller pins its green thread first
  (`Scheduler.pinToMachine()`). A process and a thread are named by the handles their debug events give (the event's image
  file handle is the caller's to close), an id by its number. The runtime's entries are `runtime/debuggee.maxon`; the
  interpreter drives no debuggee, so a program that does is `// native-only`. The committed seed links a fixed list of DLL functions,
  which lacks the debug port's, so the entries panic on Windows too until the seed is renewed.
- **`Subprocess.runWithDeadline`** is maxon's `Subprocess.run` with its `deadline:`, a function of its own because a built-in's only
  default argument is the case of an enum (`mode:` of the writes). When the deadline passes the run ends the child with `TerminateProcess` and throws `timeout`; it
  ends the child alone, not the process tree it started (maxon's ends the tree), and the output the child had written is
  lost with it.
- **Natively**, the entries are Maxon source in `runtime/` (`os.maxon`, `subprocess.maxon` and `spawn.maxon`), which calls the `W` Win32
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

**Cowns and `when` are the library tiers' machinery.** A program keeps state that threads share in a service and sends it
messages (see "Services"): states that share an invariant are one service, whose messages change them together. The cown
type, a `cown(...)` call and a `when` are written only in the library tiers (`stdlib/`, `runtime/`, a test of
`tests/library/`, and the compiler's own source, whose task graph runs on them, `self/driver/executor.maxon`); anywhere else
each is `E3185`, the code of the raw surface, with a service named as what to write instead. Services are lowered onto
them (see "Services", "In the IR"), and the rules below hold of the code that writes them. Frozen values (`freeze`,
`share`, `thaw`, `frozen with T`) are a program's.

```
type Account
	export var balance as Count
end 'Account'

// library-tier code: a program keeps the accounts in one service, whose transfer is a message
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
| `freeze(x)` | takes `x` (`consumes`) and gives a `frozen with T`, an immutable value; `T` is not a plain value (`E3910`) and must be `Droppable`, because the last owner drops it (`E3017`), and what it holds crosses between threads (`ast.unsendable`, `E3171`), since every thread that holds the frozen value reads it |
| `share(f)` | another owner of the frozen value or the cown that `f` (a `let` borrow) names; the count goes up by 1 |
| `thaw(f)` | an owned, mutable deep copy of what a frozen value holds, so `T` must be `Copyable` (`E3017`) |
| `cown(x)` | takes `x` (`consumes`) and gives a `cown with T`, a reference to a cown that owns the value; `T` may be a plain value and must be `Droppable` (`E3017`), and crosses between threads (`E3171`), since its behaviours run on any thread; in generic code each instance is judged at its own type arguments (`ir.requireShareable`), as for `freeze` |
| `cown(x, dropWith: d)` | the same for a `T` that has destructors: `d` names one that takes nothing else and returns nothing, and the last reference runs it on the value (`E3921`) |
| `when a, b as x, y 'label'` … `end` | a behaviour: runs later, once it has exclusive access to the cowns `a` and `b`, with `mutates` access to their values as `x` and `y` |

-  **Frozen values.** The contents are lent and never given: reading a field, calling a method whose receiver is `let`,
  subscripting, matching, interpolating, and passing the frozen value to a `let` parameter of the type it holds all see
  the contents (`d.title`, `d.total()`, `describe(d)`). Anything else is refused: assigning through it, a `mutates`
  argument, a method that takes `mutates self` or `consumes self`, or a `consumes` parameter is `E3197` (whose `fix`
  for a `consumes` parameter passes `thaw(...)`, when the contents can be copied), and taking a
  part out of it is `E3901`. The type system tells a frozen value from its contents by the type `frozen with T`: a value
  where `frozen with T` is wanted, or the reverse, is `E3005`, whose `fix` writes `freeze(...)`, or `thaw(...)` when the
  contents can be copied. A
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
  reference in the value, two cowns could refer to each other, and counting never frees a cycle. A slot key or a key
  does the same job without the cycle.
-  **Behaviours.** `when` names its cowns, which are plain variables or fields of `self` (`as` would read as a cast
  after an expression), and gives one name for each value; the cowns are only read, so naming one does not move it. The
  body is a function of its own that sees the variables around it by capturing them: **a scalar is copied; an owned
  value moves in at the `when`**, so using it afterwards is `E3102` (a loop that schedules a behaviour that takes a
  variable declared outside it fails the same way); a frozen value and a cown reference are owned values too, so one
  that the enclosing code keeps is shared first (`let own = share(f)`). The behaviour runs on another thread, so what it
  takes crosses by the rule of a message's arguments (`ast.unsendable`, see "Services"): a promise is `E3909`, and in generic code each instance is judged at its own type arguments, where the lower finds
  it (`ir.requireCrossings`). A borrow cannot be captured, because the
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
  is `E3057`. `sleep(milliseconds)` (a `Milliseconds`) parks the green thread, maxon's library function (`stdlib/sleep`,
  converted, over `__Builtins.sleep`, with `ForeverMs`), and `Scheduler.yield()` lets the members of
  its strand that wait run first, else the runnable green threads. `File.exists(path)` is maxon's, as I/O.
- Refused as maxon refuses them (`E2015`): `async` of what is not a call, of a method of a value, of a built-in, of a
  function that returns a float (and, minimaxon's, one that takes more than eight arguments, the words a coroutine holds,
  or gives a `consumes` parameter a value that is not `Droppable`, a promise aside, which a cancel before the start would
  have to drop).
  A float argument is written into the coroutine as its bits, and the coroutine's thunk passes it in the xmm register the
  function takes it in; a float that an instance of generic code gives back comes back as its bits, the same way.
  `await` of what is not a promise is `E3005`. A function the async call starts must be able to wait (`E3073`, maxon's
  inference, `self/check/waits.maxon`): it awaits, sleeps, yields, does file or process I/O, cancels a promise, calls
  through a function value or an interface, or calls or starts a function that may, one of another module by what its
  module's check published (`ast.Typing.waiting`, which the verify of a module reads of the modules it names).
- **What the coroutine takes of its caller** (`self/check/ownership_async.maxon`), where maxon shares by counts:
  - a scalar is copied, and a value passed to a `consumes` parameter moves in;
  - a place passed to a `let` parameter is lent to be read until the promise is awaited: the caller may read it, but
    changing it, moving it or passing it `mutates` is `E3943`; a temporary there is `E2015` (the statement would drop it),
    a string literal aside;
  - a local `var` or a `mutates` parameter, or a field of one, passed to a `mutates` parameter is lent to be changed
    until the await: any use is `E3102`, maxon's message. A parameter is lent as maxon lends a parameter's cell: its
    promise is tied, so it is awaited before the function returns, while the function's caller still lends it the
    place. Anything else by reference (an element, a field of `self`, a capture) is `E2015`, and a function value that
    lives on and borrows what is lent is `E3186`;
  - a promise is taken only by a `consumes` parameter (`E3141` at a `let` one).
- **A promise in a local is linear.** It is never copied, and never dropped there: on every path out of its scope it is awaited, cancelled,
  returned or passed to a `consumes` parameter, else `E3213`. Awaiting or cancelling it again is `E3142`, and awaiting
  inside a loop one started outside it `E3100` (the loop's paths meeting refuse it no second time). A promise whose call lent anything is **tied** to what it borrows: it is bound by the declaration
  of a local, which ends inside the scope of everything it borrows, and awaited there; returning it, storing it, passing
  it on or assigning it to a variable declared before it is `E3212`, and so is an exit that leaves it unawaited. A
  closure that captures a promise is `E3141` (once: neither its tie nor its drop is refused again), and a behaviour that
  does `E3909`: the coroutine belongs to its strand.
- **`p.cancel()`** consumes the promise as an await does, so it ends what a tied one borrows: the coroutine is marked
  (`rt.async.cancel`), so its thunk does not start a function that has not started, dropping what the function was given
  to own, and a sleep it is in or makes ends at once after a yield (`expireSleep` moves its timer to now); the cancel then
  waits for it as an await does and drops what its function gave, a value or an error. `p.isComplete()` is whether the
  coroutine or the reply has finished (`rt.async.complete`), which neither waits nor consumes the promise.
  So every coroutine ends before the frames that lent it anything do, and its borrows are those frames' own.
- **In the IR** an async call is one instruction, `%p = async f(%a, %b)`, whose arguments are lowered as the call's
  would be; the backend makes the coroutine with the address of the function's thunk (`async <f>`, written in each unit
  that starts `f`, the link keeping one), writes the words into it and queues it in the strand. An await is the
  runtime's: `rt.async.await` for a function that throws nothing, and `rt.async.wait`, `rt.async.error` and
  `rt.async.take` with the failure branch of a call for one that throws. A promise's drop routine cancels it as
  `p.cancel()` does; it meets the empty place an await leaves, and a promise that a coroutine cancelled before it started
  was given to own.
- **The interpreter** runs coroutines, sleeps and yields as the runtime does ("The interpreter", `self/interp/strands.maxon`):
  a coroutine is a thread that runs only while it holds its strand, and a sleep parks its thread on a timer of the
  interpreter's clock.
- **Promises in containers and fields.** A promise held by an array, a record, a tuple or a union's payload is dropped
  with its holder, which cancels it (`partLacks`: a promise part is `Droppable`, a promise of its own in a local is
  must-use); a store into a promise field drops what the field held. A read of a promise out of storage that is consumed
  (awaited, cancelled, stored, passed to a `consumes` parameter, returned) takes it out of its slot, which is left empty
  (`ast.Typing.slotTakes`, lowered by `ir.takeFromSlot`): an element of an array, a field of a `var` the function owns
  or of a `mutates` parameter or receiver, and the variable of a `for` over such an array, which names each element's slot
  in turn and is spent for the pass by its take. The holder stays whole, its drop skips an empty slot, and a second take
  of one slot is maxon's run-time abort 118 (`promiseSlotConsumedTwice`). `pop` and `remove` give a promise outright. A
  drop cancels every promise of an array or a record before it waits for any, so none of them that has not started
  starts. A value that holds a promise is not
  put in an interface value (`E3919`), which crosses with what its box holds, and does not cross (`E3171`, `E3216`,
  `E3217`).
- **`promises.awaitAny()`** on an `Array with Promise with ...` gives the `ElementIndex` of the first promise that has
  finished, an empty slot holding none, and else waits, registered as the waiter of every one through one selector, as
  Go's select shares one sudog among its cases (`rt.async.awaitAny`); it consumes none. It is a wait on another party
  (`E3221` in a message or a factory), as maxon has it; on an array of anything else it is `E3004`.
- Not yet: `get(i)`, `first()` and `last()` of an array of promises as reads of a slot (maxon's), which want `T`
  `Copyable` here (`E3017`), an async call of a built-in such as `File.readText`, the deadlock exit 92,
  and I/O on the event queue.

### The parallel loop

`parallel for` is `for` whose iterations run at once on the processors, one obvious spelling for each need:

- `parallel for i in a upto b` (or `to`) runs the body for each integer of the range;
- `parallel for x in xs` over an `Array` lends each iteration its own element, `x`, to change: parts of it as a plain
  `for` lets them change, and, where the function may change `xs` (a `var` or a `mutates` parameter or receiver),
  the whole element too (`x = f(x)`, or `f(x)` to a `mutates` parameter, which is given the element itself and after which`n  `x` reads what it left), which stores into the array;
- `parallel for (i, x) in xs.enumerated()` adds the index of each element. Results are written into a preallocated
  array's own elements (`var out = Nums.filled(n, value: 0)` and `parallel for (i, r) in out.enumerated()`), since
  Maxon has no loop that gives values to collect.

What the body may do is proved by the checker from the borrow rules, with no run-time check:

- It reads what is around it and changes or moves only its own element and what it declares (`E3962`, which says to
  write each iteration's result into its element): an assignment, a `mutates`, `sets` or `consumes` argument, a
  `mutates self` call, an await of a promise from around it.
- It does not name the array whose elements it lends to be changed (`E3962`): another iteration is changing it.
- What it reads from around it is read from several threads at once, so it is what crosses (`ast.unsharable`: as
  `ast.unsendable`, borrows allowed, since the loop ends before what they borrow from does): no promise (`E3963`); an
  interface value is read freely, since what its box holds crossed when it was put there.
- `continue` of the loop ends one iteration; `break` of it, a jump to a loop around it, and `return` are `E3961`,
  which says to throw an error to stop the loop or to record what was found in the element. A `break` or `continue`
  of a loop inside the body is a plain one.
- A body that throws makes the loop throw the error of the lowest index that failed, and no iteration past it starts.
- It walks a range of integers or an `Array` (`E3960` for a String's clusters, a range of characters, `drain()` and
  a type with the iteration protocol).
- It may await, sleep, send to a service and nest another parallel for; a parallel for in a service's handler waits only
  for its own iterations, and a body that awaits a reply makes the handler `E3221` as any wait in it does.

**Lowering** (`self/ir/parallel.maxon`). The body becomes a chunk, a body of the function that the runtime's fork-join
(`rt.parallelRun`, see "The runtime") calls with a part naming its indices, `[low, high)`, and runs those iterations in
order. The chunk has the function's slots as they are at the loop, so the body is lowered as it would be in place; the
slots and parameters it reads and never stores are copied into an environment, a frame area of the caller, and from
there into the chunk's slots as it starts, each a borrow, since the checker holds the body to reading them. The walked
array is such a slot, from which each element is lent to its iteration. A failure in the body ends the chunk with its
error's word and stores its index in the part; the runtime keeps the lowest index's error, which the loop then fails
with where it stands, and drops the others by a second body of the function. A loop of 0 or 1 indices, one run off a
green thread or with one processor (`MAXON_MAX_PROCS=1`) makes no job and allocates nothing: it is one call of the
chunk, in a part area of the caller's frame. A function with no parallel loop is lowered as before, to the same IR.

Under the interpreter the loop is a seeded model of the runtime's: `rt.parallelRun` cuts the indices into 1 to 8 chunks
and makes 1 to 4 workers, each a thread that takes the next chunk in index order and runs it, which the scheduler
interleaves as any threads, so iterations of different chunks run in an order the seed decides and a body whose result
depended on the order would print another result under some seed. A worker belongs to the caller for the race rule
and for the cowns its behaviour holds. The lowest index's error wins and a chunk taken past it is not run, as natively.

Not yet: parallel fors in the programs `fuzz` writes.

### Services

maxon's surface, on cowns, behaviours and green threads (`self/check/services.maxon`, `self/ir/services.maxon`,
`runtime/service.maxon`):

- `service Name ... end 'Name'` declares a service: it has the body of a `type` (its fields, `static function`s and methods;
  `service Ledger is noncopyable`, `service Conn implements Ends` and `export service` are written as for a type), and its export
  instance methods are **messages by declaration**, so `self.handle`, `self.stop()` and `reply.later()` are written in them
  whether or not any module spawns it, and the rules of a service are checked once, at its declaration, in the module that
  declares it (`self/check/services.maxon`, `checkServiceDecl`): its state holds no promise (`E3216`) and reaches no handle to
  its own service (`E4014`), and implements `Ends` where it holds a value that cannot be dropped (`E3956`, with the fix at the
  declaration); each message takes and gives only what crosses (`E3217`, `E3140`) at the message; no message or factory may wait
  on another party (`E3221`, "Coroutines: async and await"); and the policies of its messages (`E3946` to `E3951`, `E3957`, see "Overload
  policies") are read from the declaration whoever spawns it. A `spawn` of a `service` of another module checks nothing of it
  again: it only holds the factory it calls to what crosses (`E3217`, `E3140`). A service takes no type parameters yet
  (`E2015`). `service` is a reserved word (see "Where it differs from Maxon"). A `type` has no messages: `self.handle`, `self.stop()`,
  `reply.later()` and `@policy` are valid in a service alone (`E3222`, `E3946`).
- `spawn T.f(args)` starts the `service` `T`, whose static factory
  `f` returns `T`, and gives its handle, a `T.handle` (`E3134` for any other target, a type declared with `type` among them,
  whose fix declares it `service` where a file of the program holds the declaration). A mailbox has no capacity. A factory
  that cannot throw runs as the service's first message; one that throws runs where the spawn is, and the spawn fails
  with its error (`try spawn ...`).
- A call through the handle of an export instance method of `T` is a **message** (`E3136` for any other method): the
  service runs it on its own green thread, one at a time, in the order the sends were made, so a handler that sleeps or
  is preempted still holds the service. A send fails, with `SendError`, only as `serviceStopped`, after `h.shutdown()`,
  which closes the mailbox and lets the queued messages run, or another stop (see "Stops"). A send without `try` where nothing catches is `E3057`.
- **Backpressure** is the runtime's, Pony's (`runtime/service.maxon`), and no program chooses a number. A service is
  behind while a send finds a turn's worth (64, the runtime's turn) of its messages queued, counting its own; once a turn
  of its messages ends at 64 with more queued, until a turn empties its queue; and while it is muted. The count and the
  flags are in the gate that every send compare-exchanges already, so one comparison of it decides, and a send to a
  service that is not behind costs what it did. A service that sends to one that is behind is listed on it and muted at
  the end of a handler: it keeps its cown and runs no next message until the receiver lets it go, and nothing waits
  inside a handler or holds a green thread meanwhile. A listed service that is behind itself, or has senders waiting on
  it, drains on until it holds no message and is muted then, so it is not stopped while overloaded but cannot pass on
  a refill; one that drains on by holding 64 is made behind, so its senders stay slowed through the drain. A task (main, a coroutine, a `when` that is no message) parks at the send instead, and a service's sends to
  itself never slow it. A slowed sender waits
  until the receiver has started what was counted ahead of it when it was slowed (its mark: the messages started, the
  places of the gate that are queued or on their way, and the posted messages queued, read under the lock of the
  receiver's list), or until its queue empties, not until it falls under 64, so it is slowed at most once per backlog it
  finds; a service that lets its senders go unmutes them in order, 64 at a time; and a service that is muted lets its own
  senders go, which is what keeps any cycle of muted services from closing. So a service that never gets ahead of its own
  posted messages (timers re-armed in their handlers faster than it runs them) lets a sender go once it has worked
  through what was ahead of it, where its queue never empties; it stays behind until its queue empties, so `underLoad()`
  and the policies see it so. A service listed on another lets its own senders go only by emptying or by its mute, and a
  listed service's send to the same receiver moves its mark there. A service behind on its own posted messages and
  listed on a slow service downstream keeps its senders waiting while it drains on: that is the downstream's
  backpressure reaching them, and it is intended. The places of slowed tasks that have not queued their
  messages are a second count in the gate, which a mark leaves out: such a task may park after one that reserved later,
  and a mark counting its place would wait for a message that waits for it. The queue is bounded in messages: the bound
  of its sent messages, below, plus the posted messages outstanding on it, one for each armed timer or ticker, socket arm
  and watch of a stop, which are never slowed. The sent bound, whatever the speeds where one task sends: main and a slow
  consumer, 65; main, a relay and the consumer, 66 at the relay and 132 at the consumer (2 x 64 + 4: a relay that drains
  on is behind or has main parked on it, so main stays parked until its mute, and it passes on only what it held, at
  most 66; derived as 2 x 64 + 3, for a relay that has no posted messages of its own); N tasks and a slow consumer only
  they send to, max(64, N) + N, a task let go having every message it sent before started, so that it holds at most its
  last one queued and its next one parked. A message still on its way to the queue as its sender is let go (overtaken by
  posted messages) adds nothing: each bound counts a sender's one place on its way already. The header of
  `runtime/service.maxon` derives each. A
  service is listed on one receiver at a time, so a second receiver that is behind gets its next
  handler after each unmute by the first. `h.mostQueued()`, in the library tier, is the most messages a slowed send to
  the service found queued, exact once past 64, which tests hold the bound to. The interpreter models the same rules, with
  a turn of 1 to 4 messages drawn from the seed, and its deadlock report names each service muted with nothing left to
  unmute it. Its oracles of backpressure (`self/interp/oracles.maxon`) check every run that sends: every message whose send
  succeeded is handled exactly once; each sender's messages to a service (a service's from all its handlers, a task's) are
  handled in the order sent; a run never stops with backpressure alone holding it, every waiting thread a task parked at a
  send, with services muted or tasks so parked (progress); and the most a service held stays within the bound above for
  the shapes it is stated for, with no posted messages, which the interpreter does not run (the posted term is 0): a
  service that sends to none and that only tasks send to (the parts of a parallel loop among them), the larger of the
  turn and its tasks, plus one per task; a relay of one task to one service, the turn and 2; that relay's lone consumer,
  two turns and 4, the bound the header proves. The interpreter lets a slowed sender
  go by its mark as the runtime does. An oracle that fails is an internal
  error that names it, the seed of the schedule and the standard input, never a report of the program. `fuzz --services`
  runs the brute force of them (see "Tests").
- **Overload policies** (`stdlib/services/Overload.maxon`, `self/check/policies.maxon`, `lowerPolicySend` in
  `self/ir/services.maxon`): a message kind says what becomes of its work while its service is behind by naming a policy
  before its `export function`, `@policy(Refuse) export function handle(request Request) returns Response`; a kind that
  names none, or `Keep`, compiles as it did, and its senders are slowed. A policy is a type that implements the library's
  `OverloadPolicy with Api.Message`, whose `static function admit(arrival Arrival, message Api.Message) returns Admission`
  runs at the send, only while the service is behind, sees `arrival.count` (the messages the service holds),
  `arrival.turnSize` (the runtime's turn) and the message, and keeps no state, so a program writes its own and tests it as a
  plain function, making a message as `Api.Message.handle(request)`. Its answer is `queue` (the message
  is queued and its sender slowed), `refuse` or `discard` (it is not handled and its sender is not slowed: a message with
  a reply fails its send with `busy`, any other is dropped with what it holds), or `replacePending`. The library's menu
  is `Keep`, `Refuse`, `DropNewest`, `DropOldest` (see "Shedding") and `Latest`. `Latest` implements `CoalescingPolicy`, whose kind has a pending word
  in its service, after the service's own words, which only such a type reserves (`rt.service.newPending`): a message of
  the kind sent while its service is behind waits there in a box of its arguments, with a place in the mailbox that
  never slows its sender, and a later message of the kind, behind or not, replaces it and drops it (one atomic exchange),
  so only the newest that has not run is handled, at the place in the queue of the first it replaced. The send of a kind
  with a policy takes its place by `rt.service.tryReserve`, which is `rt.service.reserve` while the service is not
  behind, so it costs what any send does then: a plain send's IR is unchanged. `@policy` on a function that is no export
  instance method of a service's body is `E3946`, a type that implements no `OverloadPolicy` `E3947`, one that reads another
  type's message union `E3950`, and `Latest`, `DropNewest` or another
  coalescing policy on a kind with a reply `E3948`, since the reply would never come; a shedding policy on such a kind is
  answered `busy` at the sender's await (see "Stops"). A send of a kind with a reply and a
  policy other than `Keep` fails with `SendBusyError`, `serviceStopped` or `busy`, which its sender handles; every other
  send's error is `SendError`. `underLoad()`, the library's, is whether the service whose message runs is behind, for a
  handler that does cheaper work then, and false off a message. The interpreter models the same (its pending words, a
  queue bound one message longer for each), and its oracles hold under every schedule; `fuzz --services` sends the walks
  of each graph as a kind of each policy of the menu in turn, and of a policy with state of its own.
- **The message union** (`messageUnionDecl` in `self/syntax/parser.maxon`, `arrivingMessage` in `self/ir/services.maxon`):
  a service a message of which names a policy, or whose module writes its name before `.Message` (`Conn.Message.readable()`, as a
  posted message is named), has `T.Message`, a union the parser declares with a case for each of its message
  kinds, its export instance methods in source order, whose payload is the message's parameters, so `Api.Message` of
  `handle(request Request)` and `data(sample Sample)` is `handle(request Request)` and `data(sample Sample)`. A policy reads
  a message by matching on it (`match message 'm'`, `handle(request) then ... request.client ...`). It is named
  `Api.Message` wherever a type is written, and `Api.Message.handle(request)` makes one, for a test of a policy's admit. The
  interfaces are generic over it (`OverloadPolicy uses Message`), so a program's own policy for its own service implements
  `OverloadPolicy with Api.Message`, and a library policy is generic, `type DropOldest uses Message implements
  SheddingPolicy with Message`: `@policy(T)` that names a generic policy bare is the instance at the message union of the
  type it is written in, `@policy(DropOldest)` is `DropOldest with Api.Message`, and a field that holds one names it,
  `var fair as PerClientFair with Api.Message`. A policy is lent messages and never owns one: admit's `message` is a `let`
  parameter of what its sender still holds, and `pending.message(i)` `returns borrowed`, a place in the queue, so keeping
  either, in the policy's state, in a collection or by passing it on to own, is `E3900`, and `.clone()` makes a copy that
  is the policy's own. The union is always a box (`ir.unionForm`), laid out as a message's behaviour holds its case and its
  arguments, so the value a view lends is the behaviour's own words, with no copy or allocation, and the one admit is lent is
  a frame area of the sender's that borrows the arguments, made only once the send found the service behind: a plain send
  and a send of a kind with a policy to a service that is not behind compile as they did, and a type with no message that
  names a policy has no union. The library's `PerClientFair` (`PerClientFair.create(clientOf)`, a field of the service) is
  fair to the clients of a service that is behind: it sheds the oldest messages of each client that holds more than its
  share of a turn, the client named by the key function it is given, which reads the message's arguments.
- **Shedding** (`SheddingPolicy`, `PendingView` and `DropOldest` in `stdlib/services/Overload.maxon`, `startShedding` in
  `self/ir/services.maxon`, "Shedding" in `runtime/service.maxon`): a policy that also implements `SheddingPolicy` has
  `function shed(mutates self, pending PendingView with Api.Message)`, which runs on the receiver, single-threaded as a handler, at
  the start of a message while the service is behind, once for the messages queued since its last view ended, so each
  message is in one view at most. Its `pending` holds the queued messages of the kinds that name the policy, the oldest
  first, from the one that starts up to the newest queued when the first view of that start was made: `pending.count` of
  them, `pending.turnSize`, `pending.message(i)` (the message, lent where it waits), `pending.shed(i)`, `pending.keep(i)` and
  `pending.isShed(i)`, which fail with `indexOutOfBounds` past `count`. A message marked shed is not handled: its behaviour drops what it holds when it reaches the head, and it keeps
  its sender's order. `DropOldest` queues every message and sheds the oldest of its kinds until a turn of them is left. A
  policy with state is a field of the service that its factory sets, named by `@policy(self.field)` (`E3018` for a field
  the type lacks, `E3947` for one whose type is no policy); its `shed` changes that field and no other, and a shedding
  type with fields named as a type is `E3949`. A policy that is both a CoalescingPolicy and a SheddingPolicy is `E3951`: a
  message of a coalescing kind waits in its kind's pending word, which the next send replaces, so no view could lend it. A
  shed message with a reply answers `busy` at its sender's await (see "Stops"). A message's policy is declared once every
  type of its module is, so a policy may be declared after its service. Every behaviour of a type with shedding kinds holds
  a mark word as its first capture (its kind's policy, and whether it is shed) and its case of the message union as its
  second, the service a horizon word after its pending words, and its messages start by `rt.service.shedStart`: a load of
  the horizon and of the mark, and, where the service is behind, the views, each an array of the queued messages of its
  policy's kinds that lends each from the behaviour's case on (`rt.service.viewMessages`) and gives them back before it is
  dropped (`rt.service.viewClose`); a type with no shedding kind holds neither and starts its messages as before.
  The interpreter models the same, and makes a service behind at a send of a kind with a policy and at a message's start
  by a coin of its own stream, one in four, so every `admit` and `shed` runs in ordinary programs; a view's messages are
  boxes whose cells hold the behaviour's arguments until it is closed; its oracles count a shed message as delivered once, in
  its sender's order. `fuzz --services` sends walks as kinds of the library's `PerClientFair` and of a program's policy that
  reads their hops in admit and shed. Not yet: `age(i)`.
- **Fed from outside** (`verifyFirstHops` in `self/check/policies.maxon`): a kind that a post names,
  `h.post(Conn.Message.readable())` or `h.post(Sup.Message.childStopped(id))`, is fed by a source that cannot be slowed, the
  event queue or the end of a child, so the work its handler passes on must say what becomes of it while its receiver is
  behind. A send made in the handler of a posted kind, or in a function of the module the handler calls, walked as the
  wait inference walks, is a first hop, and the kind it sends must name a policy: any `@policy(T)` or `@policy(self.field)`,
  `Keep` included when written (`E3957`, at the send, with the menu of the library's policies, one line each). A function
  of another module is judged by what its module's check published (`ast.Typing.unpoliced`, beside `waiting`: a kind
  with no policy it sends, by its body or its calls), and refused at the call of it; the receiver's policy is read from
  its kind's declaration, so the rule is the same where the send is written in another module. A kind is posted where a post
  names it, as a case of the union (`Sup.Message.childStopped(id)`) or, for a value of any other form, every case of the
  union; a post of a kind of another module is refused at the post. Only the first hop needs a policy: a send in the
  handler of a first hop's kind is a second hop and needs none, and the posted kind itself names none (the runtime
  coalesces its posts). `E3948` and `busy` apply to a first hop's policy as to any. A call through a function value or an
  interface, whose target the check cannot see, is taken to send nothing, unless the interface requires service, whose
  requirements are held to this rule where each is met (the next bullet).
- **An interface that requires service** (`interface Route requires service`, after the `extends` and `where` clauses;
  `verifyRequirements` in `self/check/requirements.maxon`): every method that meets one of its requirements meets the rules of
  a message of a service that a source outside the program feeds, so a message may call it through a value of the interface,
  which a router of routes does (`E3221` refuses a call through any other interface). It does not make the type that
  implements the interface a service: the type is declared with `type` as ever (or is an `enum` or a `union`), and nobody
  spawns it. Only the methods that meet a requirement are held to the rules; the type's other methods are not, unless a
  requirement's method calls one. At each conformance, checked in the module that declares it once its bodies are checked,
  each such method (the type's own, or one that an extension adds; one of another module is judged at the conformance by
  what its module published, `ast.Typing.waiting` and `unpoliced`):
  - waits on no other party: it keeps no reply, starts no service whose factory throws, and calls no function that may
    (`E3210`, maxon's code for a requirement that waits, which maxon refuses of every requirement), though it may sleep and do
    I/O;
  - calls nothing the check cannot see into: a call through a function value, through an interface that does not require
    service, or of a method of a type parameter that stays one is `E3221`, as it is in a message; a call through an interface
    that requires service is not one;
  - sends only kinds that name a policy, `@policy(Keep)` among them (`E3957`, at the send, as for a posted kind's first hop):
    its own sends, those of the module's functions it calls, and, for a function of another module, what that module
    published. A send through an `I.handle` names no policy and is refused.

  A call of a requirement of such an interface, through a value of it or a bound `where T implements Route`, may sleep or do
  I/O and nothing more (`noteCallWaits`): it adds no wait on another party and no send. The flag is part of the interface's
  declaration (`ast.TypeEntry.requiresService`), so a module that names the interface reads it from there, and of the public
  text that the build cache's summary of a module digests (`publicDigest`), whose header line holds it. It holds for the
  requirements that the interface declares, and for those that an interface extending it inherits, but an interface that
  extends it holds the requirements it declares itself to it only by saying so. A generic type's conformance is judged once,
  at its declaration, where the methods of its type parameters are not seen: a requirement's method that calls one is
  `E3221`, and one that calls none is accepted for every instance.
- **Stops** (`stdlib/services/Stop.maxon`, `declareAwaitErrors` in `self/check/policies.maxon`, `startStopping` and
  `dropUnhandled` in `self/ir/services.maxon`, "Stops" in `runtime/service.maxon`): a service stops in one of two ways, chosen
  at the stop. `h.shutdown()` by the owner of its handle, or `self.stop()` in one of its messages, closes its mailbox and lets
  the queued messages run, as maxon's shutdown does, so a reply always comes. `self.stopNow()`, or `h.stopNow()`, ends it at
  once, only for a type that declares it may be, `type Worker implements StopsNow` (the library's marker interface; `E3952`
  for any other type): the message that runs as it stops ends as it would, and every message queued is not handled, but drops
  what it holds, the box of a pending word among it, and fails the await of its reply with `serviceStopped`. Its slowed senders
  are let go at once and it slows none again, and its shedding horizon is never read again. A type that declares a method
  named `stop` or `stopNow` keeps the name, and `self.stop()` off a message stops nothing.
  **A state that ends with its service** is one whose type implements the library's `Ends` (`stdlib/services/Ends.maxon`),
  `function finish(consumes self)`, which is no message: the service gives it the state once it has stopped, after the
  messages queued before the stop have run (the behaviour a stop queues, `endState` in `self/ir/services.maxon`), and, where
  nobody stops it, where its last handle goes, in place of the state's drop (the service's ending word, which
  `rt.service.ending` sets at the spawn, as a cown's `dropWith:` destructor; the drop of the last owner calls it with the
  state, which a stop's behaviour has taken out first where one ran). A state that cannot be dropped, one that holds an open
  file, a socket or another value with a destructor, is `E3956` unless its type implements `Ends` (at the declaration of a
  `service`, and of a `type` in its own module that it spawns; at the spawn of a `type` of another module): the service would drop what only a destructor may end. `minimaxon fix`
  (kind `ends`) declares the type `Ends`, after the interfaces it implements, and writes its `finish` last in its body, with a
  line that ends each such field where its type has one way to end it that takes nothing (`try self.out.close() otherwise
  ignore`); the body of `finish` ends each field that cannot be dropped, as any `consumes self` must (`E3919`), and what it
  leaves is dropped where it returns, a Reply failing its sender's await then. The interpreter runs the same IR, and a
  service's ending is its cown's (`entryServiceEnding`).
  **A message's own handle** is `self.handle`, a `T.handle` that is another owner of the service whose message runs, as a
  copy of a handle is (`rt.service.selfHandle`), so a message can pass it to another service that answers by a message of
  its own (`peer.ask(n, back: self.handle)`). It is only in an export instance method of the type (`E3953` elsewhere: pass
  the handle as a parameter), a type that declares a field named `handle` keeps the name, and keeping it in the state is
  `E4014`, as any handle to the service itself is.
  **A task watches a stop** with `let done = h.stopped()`, a promise of no value that the service's end completes: once its
  mailbox is closed and its last message has ended, whichever stop closed it, or once the service is freed, which only one with
  no message and no handle is (`await done`). A watch of a service that has ended already completes at once, and its cancel or
  drop takes it off without waiting. A message never awaits one (`E3221`), as it never awaits a reply. A watch is an entry of
  the scheduler's list (`schedWatchersOffset`, "Watching a stop" in `runtime/service.maxon`), so a service nobody watches holds
  nothing for it, and the end of a service reads the list's one word to find nobody watches.
  **A posted message** is a message the runtime posts to a service, which no sender awaits: `h.post(m)` makes one, the
  library's `PostedMessage` (`stdlib/services/Posted.maxon`), of `m`, a value of the service's own message union that may carry
  a key (`E3954`), of a kind whose handler returns and throws nothing, since nobody awaits it (`E3955`): a value written as a
  case is that case's, and any other value may be any case, so every one must be. It is linear: a source of posted messages
  takes it, and its `cancel()` drops it unhandled. It is a behaviour of the service that no send queued (`postedRecord` and
  `lowerPostedBody` in `self/ir/services.maxon`, "Posted messages" in `runtime/service.maxon`): its record is made with no
  acquire (a `when` that gives its record) and holds a hold on the service, its body dispatches on the union's case to the
  case's handler, and its start (`rt.service.postedStart`) takes no place in the gate, so it is never slowed, and drops what
  it holds where it was cancelled or its service was stopped at once. Since no source of posted messages can be slowed, a
  kind that the handler of a posted kind sends names a policy (`E3957`, "Fed from outside" under "Overload policies").
  **A service watches a stop** with `child.notifyStopped(self.handle.post(Sup.Message.childStopped(id)))`: the runtime posts
  the message to its service once the service of `child` has ended, as it would complete a watch, and at once where it has
  ended already. The watch is an entry of the scheduler's list whose cell is the record, so it is not live: the watched
  service keeps the program alive, not the watch; and a watcher that ends first is told nothing, its watch dropped on it. A
  message posted to a service that has ended, by an arm or a watch made after its end, is dropped too, never handled, so
  nothing runs on an ended service's state, which may be gone. The interpreter runs posted messages, timers and tickers as the runtime does, on its simulated clock (`self/interp/posted.maxon`): a
  record is a behaviour that no send queued, a timer a row that holds the record it is armed with or the one of its fire queued and not started,
  and the clock moves to the earliest armed timer when nothing can run; a socket's readiness stays native-only, as sockets do. A reason
  argument, H4's, can come later.
  **A timer posts a message** (`stdlib/services/Timer.maxon`, "Timers that post a message" in `runtime/timers.maxon`):
  `let t = Timer.notifyAfter(5000, message: self.handle.post(Conn.Message.idleTimeout()))` posts the message once, at least
  that many milliseconds (`clock.DurationMs`, at most about 146 years, as a sleep) later, with no green thread waiting for it:
  the timer is a 56-byte record on the timers' heap that holds the posted record. `t.reset(ms)` moves a fire still pending and
  says whether it did (`false` once the timer is spent); `t.rearm(ms, message: m)` arms it anew with another message in any
  state; `t.cancel()` ends it. A cancel, a reset or a rearm drops a fire that is queued and has not started, as Go 1.23's Stop
  and Reset do, so a handler never sees a fire older than the last reset. A `Timer` is linear, as a `PostedMessage` is
  (`E3919`), so a state that holds one implements `Ends` and its `finish` cancels it; an armed timer is live and keeps its
  service and the program alive, and the end of the service takes the arm back, its message never handled.
  **A ticker posts a message on every tick of a period** (`stdlib/services/Ticker.maxon`, `timerNext` in `runtime/timers.maxon`):
  `let t = Ticker.notifyEvery(10, message: self.handle.post(Poll.Message.tick))` posts its message one period from now, and
  the handler of each tick arms the next with a fresh message, `self.ticker.next(self.handle.post(Poll.Message.tick))`,
  as a socket's readiness is armed anew, so a ticker holds one posted record at a time, as a timer does. The ticks are on a
  grid, the first tick plus a whole number of periods, so they do not drift, and the next tick is worked out when `next` is
  called: the first of the grid past now (`timerGrid`, the last tick plus `late - late mod period` plus a period). A handler
  of a 10 ms ticker that takes 35 ms skips exactly 3 ticks, and its next tick comes on the grid, not at once. Go's
  `time.Ticker` differs: its channel buffers one tick, which a late reader takes at once, then the grid goes on. A ticker
  here has no channel to hold a tick in, and a tick that no handler asked for would be a message the service handles late
  only to learn it is late; the handler that wants to catch up reads the clock. `t.reset(period, message: m)` makes the
  period `period` and arms the ticker on a new grid that starts now, its first tick a period from now, in any state, as a
  timer's rearm does: its armed message and a tick queued and not started are dropped. **A ticker has at most one message
  outstanding, and a reset's message replaces whatever was:** a `next(m)` on a ticker that holds a message already, armed
  or with its tick queued and not started, as it does after a reset in the handler of a tick, drops `m`, never handled, and
  the reset's message is the one handled. A period of 0 ticks at once each time it is armed.
  `t.cancel()` ends it, and a `Ticker` is linear, as a `Timer` is (`E3919`).
  The **error of an await** is the handler's own unless the reply may fail with more: for a message whose policy sheds it
  (`busy`) or of a type that implements StopsNow (`serviceStopped`), the compiler makes an enum, named
  `Worker.work.Error` in messages, of the handler's cases, then `busy`, then `serviceStopped` (one the handler already throws
  is the one given), so the words of the handler's cases, and the happy path, are what they were. It is made where the type
  is declared, so a sender in another module reads it with the message, never with a handler's body; a handler that throws a
  union on such a kind is not supported yet (`E2015`). A send to a type that declares nothing, and its await, compile as they
  did, and its messages start by `rt.service.start`; a message of a StopsNow type starts by `rt.service.stopStart` (or
  `rt.service.shedStart`, which answers the stop too), a load of the gate, whose stopped-at-once flag beside `serviceClosed`
  it reads. The interpreter models both stops, and its oracles count a message dropped at a stop as delivered once and hold
  every reply a sender keeps to one answer; `fuzz --services` stops a service by its shutdown or at once from its own
  message, with replies of a kept and of a shed kind pending. Not yet: naming the error of such an await in a type, so a
  program keeps an array of such replies only as `[first]` and `push` infer it.
- **Deferred replies** (`self/check/replies.maxon`, `lowerReplyLater` and `lowerReplyAnswer` in `self/ir/services.maxon`,
  "Deferred reply" in `runtime/service.maxon`, `stdlib/services/Reply.maxon`), maxon's (`specs/deferred-reply.md`): written as
  the whole initializer of a first-level `let` of a message's body, `let pending = reply.later()` takes the message's reply as a
  `Reply with T`, `T` its reply type, which the message parks in its state, a container or a message argument to another
  service, and which some message answers later with `pending.answer(value)`, which consumes it and never waits: a handler that
  needs another service's answer hands it its Reply rather than awaiting (`E3221`). A message that defers declares
  `throws ReplyStopped`, the library's enum, and throws nothing itself; its `return` carries no value. A Reply is never copied
  and is moved like any owned value, so it is answered at most once (`E3102`); its drop unanswered, alone or with what holds it,
  fails its sender's await with `ReplyStopped.serviceStopped`, so every reply a sender keeps is answered once. The deferral is
  read from the declaration (`ast.defers`): the function's header reply and error are what its senders await, in any module,
  while the function itself returns nothing, throws nothing and takes the cell of its reply after its parameters, which its
  behaviour gives it (0 where the sender dropped the reply) instead of answering the cell itself; a send of a message that does
  not defer, and its await, compile as they did. A Reply is that cell, one word, with no box: a parked Reply is a word of the
  state, never a green thread or a timer, its answer is `rt.reply.answer` (where `rt.reply.held` says a sender waits, and the
  value is dropped otherwise), and its drop routine calls `rt.reply.drop` (`rt.reply.dropString` for a String, whose await
  takes its words out of a box). A stop of a service whose state can hold a Reply, `shutdown()`, `stopNow()` or a message's
  `self.stop()` or `self.stopNow()`, queues a behaviour on its cown after the queued messages that drops the state, so the
  Replies it holds fail their awaits then, as maxon's do when the service finishes, and not only once its last handle goes.
  Of a type that implements `Ends` (see "Stops"), the behaviour gives the state to its `finish` instead, and what finish
  leaves, a Reply among it, is dropped where finish returns, so a Reply fails its await after finish has run.
  The gate counts a message from its send's reservation, before its message is queued, so a send that took its place before
  the stop may queue its message after that behaviour; the state is ended, by finish or by its drop, by the service's last
  behaviour, the one whose end finds the service closed with no message counted still to start (`rt.service.stateEnds`):
  that behaviour, or else the end of the last such message.
  That behaviour, like the factory's, is one of the service's own, as a message is: it starts by `rt.service.behaviourStart`,
  which names the service as the one its green thread runs a message of, so its release, the one that leaves the closed
  service with nothing queued, ends it and completes the watches of its stop on whatever green thread it runs, and where
  the type sheds it holds the mark of no policy, which a view walking the queue reads.
  `E3222` refuses, in maxon's words: `reply.later()` in a closure, inside a block, other than as a whole `let` initializer,
  twice, in a message with no reply or with an interface value as its reply, in a generic type, without `throws ReplyStopped`,
  or in what is no message; a deferring message's `return` with a value, its `throw` or propagating `try`, and a call of it
  other than a send; and every way a Reply would leave the service code that holds it: a function whose result can hold one (a
  factory of a service's state aside), an async call of a function that takes one, a closure that captures one, and a value
  that holds one widened to an interface type. `await` of a Reply is `E3005`. The interpreter models a Reply as an object of
  the promise row it answers, whose free fails it, and its oracles hold every reply to one answer; `fuzz --services` parks
  replies of a deferring kind and answers some, so the stops of its graphs drop the others. A deferring message of a
  `service` is checked at its declaration, whether or not a module spawns it, and `reply.later()` in any other function, a method of a
  `type` among them, is `E3222`, since only an export instance method of a `service` is a message.
- A handler that returns or throws **replies**: the send gives `Promise with R` (or `Promise with (R, E)`), which the
  sender awaits as an async call's promise (`try await h.total() otherwise 0`, or `let p = try h.total()` and `await p`);
  `_ = try h.total() ...` drops the reply on the service, and a reply sent as a statement is `E3190`. A reply's promise is
  never tied: nothing is lent to a service.
- **Interface handles** (`self/check/handles.maxon`, `self/ir/handles.maxon`), maxon's (`specs/service-registry.md`):
  `I.handle`, for an interface `I`, is the handle of a service known only by an interface its type implements, so code sends
  to any service that implements `Logger`. A `T.handle` becomes an `I.handle` wherever one is expected (a binding, an
  argument, a return, a field, an element, a message's argument) or by `h as I.handle`; an interface handle becomes no
  other, not even the handle of an interface its own extends (`E3005`, as an interface value does not), and a call through it of a
  requirement of `I` is a send of the service's message of that name (`E3136` for any other name), whose arguments move, which
  fails with `SendError` and whose reply, where the requirement returns or throws, is a promise of what the requirement
  returns and throws. One send serves every service behind the handle, so each service whose handle becomes an `I.handle`
  declares every requirement as a message (`E3136`), which the conformance holds to the requirement's parameters, reply and
  error, and whose send and await fail with no more than the requirement says (`E3177`): a reply of a kind whose policy may
  refuse it (`busy`) or of a type that implements StopsNow (`serviceStopped`) is sent through the type's own handle. The
  check is made where the handle becomes an `I.handle`, in the module that sees both, never by a search for the services a
  send may reach. A kind without a reply keeps its policy, and its send through the interface handle runs its type's admit.
  `stopped()`, `mostQueued()`, `notifyStopped(m)`, `clone()` and `shutdown()` are every handle's; `stopNow()` is `E3952`, since no
  requirement fails with `serviceStopped`, and `post(m)` is `E3954`, since a posted message is a value of the message union of
  the service's own type. A posted kind's handler that sends through an `I.handle` is `E3957`: the handle does not name the
  policy of each service behind it. An interface handle is an interface value (see "Interface values in the IR"): one word, the
  box of the table of the type's handle as `I.handle` and the type's handle, made where the handle becomes one (in the frame,
  where only lent), so a send allocates nothing. The table's drop and copy entries are the handle's, and its methods are send
  stubs (`ast.sendStub`): for each requirement one that keeps the reply, then one that shuts the service down, ending a state
  that can hold a Reply or whose type implements `Ends` as `h.shutdown()` does, then for each requirement with a reply one whose sender discards it. A stub
  is the send through the type's own handle (`sendMessage`), so a send through an interface handle costs one indirect call
  more, and a send through a `T.handle` compiles as it did. Every module that makes a handle an `I.handle` makes the stubs it
  names, and the link keeps one of each, as of an instance. The interpreter runs the same code; `fuzz --services` sends
  every walk with an odd number of hops left through an interface handle. A service's state that reaches an `I.handle` of an
  interface its own type implements is `E4014`, as one that reaches its own handle. Not yet: an `I.handle` that becomes the
  handle of an interface `I` extends.
- **What crosses** is one predicate, `ast.unsendable` (`self/ast/crossing.maxon`), for every parameter of a message and of
  the factory, every reply and every error a message throws, at the message of a `service` and at the spawn of a `type`, and for what a behaviour takes: a scalar, a
  float, a `String`, an enum, a value that owns its parts and holds only what crosses, a `frozen with T` of such a value, a
  cown, a handle, an interface value and a function value cross; a borrow, a promise, and anything that holds one do
  not (`E3217`, `E3140` for a reply). A function value crosses as a frozen value does: its closure owns what it
  captured and only reads it, or has one owner where it changes it, its count is atomic, and a closure that borrows never
  escapes (`E3925`) nor captures a promise (`E3141`). A service still refuses one, as maxon does (`E3217`,
  `ast.unsendableToService`): a message could call it only by waiting on what the check cannot see (`E3221`). A
  parameter is `let` or `consumes`, never `mutates` or `sets` (`E3217`); either way its argument moves into the service,
  so the sender's variable is spent (`E3102`), and the contents of a frozen value, which are lent, are not sent (`E3217`).
  The state holds no promise (`E3216`) and reaches no handle to its own service, through fields, containers, cowns and
  other services' states (`E4014`).
- A message is as visible as its service and no more (`E3167`), as maxon holds it: an unexported type that its module
  spawns may name unexported types in its messages. A spawn in another module does not narrow them.
- An idle service holds its state, its cown and its gate (48 bytes), and no green thread or stack: each message is a
  behaviour whose record, once it is runnable, is an entry of a run queue, and which takes a green thread's record and a
  stack from its processor's pools only when a processor starts it, and gives them back when it ends, so a handler that
  sleeps or awaits holds one of each for that long and a burst of runnable messages holds their records alone. Measured
  with 100,000 and 1,000,000 idle services spawned in a burst: about 95 bytes each on Windows (private bytes) and 93 on
  Linux (RssAnon) with every core; with one processor, where the spawner runs the whole burst before any factory, about
  180 bytes on Windows, the factories' records freed into the heap's classes, which keeps freed slots for reuse. maxon runs
  a service's messages on one runner, up to 64 a turn; here each is its own behaviour on the one cown.
- **Holders never wait** (`E3221`): a message or a factory of a `service` the module declares, or of a `type` it spawns, that keeps a reply, starts a
  service whose factory throws, calls through a function value or an interface, whose target the check cannot see (an
  interface that requires service excepted), or calls a function that may do one of these, is refused, so services never wait on each other in a cycle. A function of
  another module is judged by what its module's check published (`ast.Typing.waiting`), one that published nothing as one
  that may wait; a `service` is held to the rule at its declaration, in its own module, and a `type` of another module that this one
  spawns here, at the spawn.
- **In the IR** a spawn is `rt.service.new` (a cown whose value is the state, with the mailbox's gate after the cown's
  words) and, for a factory that cannot throw, a `when` on it whose body calls the factory and stores the state. A send
  reads the handle, lowers the arguments as a `consumes` call's, takes a place in the mailbox (`rt.service.reserve`, which
  fails as a call fails, dropping the arguments in flight), makes the reply's cell where the sender keeps one
  (`rt.reply.new`) and queues a `when` on the service's cown whose body (`<f>$when<n>`, made in `ir/services.maxon`) gives
  the place back (`rt.service.start`), calls the handler with the state and the arguments, drops what a `let` parameter
  only borrowed, and gives the cell what the handler returned and threw (`rt.reply.give`), or drops it. The cell is a
  green thread's record that never runs, so `rt.async.await` awaits it as a coroutine. A behaviour's thunk ends with
  `rt.behaviour.release`, which hands the cowns on or keeps them while the behaviour is muted; then the runtime runs the
  thunk's finish (`finish run <body>`) once the behaviour is unmuted, which hands them on through
  `rt.behaviour.resumed` and drops the holds and the record as the thunk would have. `runtime/service.maxon` keeps the
  entries the seed's code calls (`serviceNew` with a capacity it ignores, `serviceTaken`, `cownRelease`), whose messages
  are never muted, until the seed is renewed.
- The interpreter runs services as the runtime does: a service is a cown with its gate, each message a behaviour on it
  whose thread may sleep or await while it holds the cown, and a reply a promise that the message's behaviour finishes.
- Not yet: see "Where it differs from Maxon".

### Source text

A source file is UTF-8, by Zig's rules and two more, each refused where it is:

- Its bytes are well-formed UTF-8: no overlong form, surrogate, code point above U+10FFFF or broken sequence (`E1004`).
  No escape writes a surrogate either: `"\uD800"` is `E1004`, as in maxon, and `�` is the placeholder.
- It holds no control character (U+0000 to U+001F, U+007F) but a tab, a newline, and a carriage return directly before
  a newline (`E1900`); the layout then refuses that carriage return too (`E2001`).
- It holds none of the Unicode line terminators NEL (U+0085), LS (U+2028) and PS (U+2029) (`E1901`).
- A name, and everything outside comments and string literals, is ASCII: a non-ASCII character there is `E1010`, as is
  each of `?;|&$%~^` and the backtick, which start no token of maxon's either; another ASCII character that starts no
  token here, as a backslash, is one that maxon lexes and its parser refuses, so it is `E2001`.
- It holds no bidirectional control (U+202A to U+202E, U+2066 to U+2069), not even in a comment or a literal, where it
  could make the code read otherwise than it compiles (`E1902`).
- It does not start with a byte order mark (`E1903`).

A literal or a token that the source leaves unfinished is refused as maxon refuses it: a string literal, or an
interpolation in one, that its line or the file ends (`E1002`, at the quote, or at the brace of the interpolation), and
an interpolation whose brace a quote follows on its line, which was meant as text and is written `\{` (`E1006`, at the
brace); a block comment that the file ends (`E1007`); and a `#` that starts no directive, as a shebang line anywhere but
at the start of the file (`E1009`).

A literal writes a character it may not hold as an escape (`"\u001b"`, `b"\x85"`). The rules apply in comments and
literals as in code, so the lexer checks every character, but a correct file pays nothing measurable: printable ASCII
and the tab pass on a range check, in code the DFA stops at any other byte, and only then does `syntax.textCharacter`
decode the character and decide (`self/syntax/encoding.maxon`). Each rule is a set of bytes or of byte sequences, which
a vector lexer can test as they are.

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
  it by one space. A `/* */` comment is an error (`E2001`), and one that the file ends `E1007`.
- Exactly one blank line separates two top-level declarations, and a comment block after the last one. Inside a
  type, an enum or a union, exactly one blank line comes before each function (a method or a static function), unless
  it is the first line of the body; a comment above the function goes after the blank line. An interface lists its
  functions, one signature per line, with no blank lines between them. There are no other blank lines: none between
  fields or cases, none inside a function, none at the start or end of a file. A file ends with one newline, and has no
  byte order mark (`E1903`, see "Source text").

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

`static let name = expr` in the body of a type, an enum or a union, or in an extension of one (see "Extensions"), declares a
constant of the type, read as `Type.name` anywhere and as `name` inside the type's own body or the extension, where its
initializer is also written (it may build `Self{...}`, which a constant of the module may not). The
parser makes it a constant of the module named `Type.name`, as it does a `typealias` of a type's body, and the resolver turns
`Type.name` into a name bound to it, so it is typed, ranged, ordered (`E2012` for a cycle) and lowered as a module-level `let`:
its value is read from the data section when it is a box known before the program runs and inlined at each use otherwise
(see "Constants in the data section"), and a call in its initializer is `E3220` unless it is a literal factory (see "Raw
values"), which the initializer may call by its bare name as one of the type's methods may, `create` of an `Array` or a
`FixedArray`, or a case of a union (see "Constants in the data section"). One that an extension adds is filed, as its typealiases are, under the type it extends in the module that writes the
extension (`ast.ExtAlias`), and found as its methods are, where the module sees the extension: the check makes `Type.name` a
name bound to it (`readTypeStatic`), as it does one of the type's body written after a typealias of the type. In the
initializer of a constant, which the constants stage types on a tree it shares, the read is typed there and made a name by
the bodies stage (`Checker.deferredNames`), and the constants stage orders the constant after the static it reads
(`staticReadBy`).

A static let of a generic type whose value names the type's parameters or `Self` (`static let width = sizeof(T)`,
`static let origin = Self{size: 0}`), or reads such a static, is one for each instance of the type (`ConstValue.instanceOf`):
it is read through an instance, a typealias of one (`typealias CountBox = Box with Count`, `CountBox.width`), which binds the
parameters, or by its bare name in the type's own methods, whose instance binds them; through the generic type's own name
nothing says which instance, and it is `E2056`, saying to read it through one. The check records the instance at the read
(`Typing.staticInstances`) and types the read with the parameters bound; lowering builds the value where it is read with
them bound (`lowerInstanceConstant`), and finding instances follows the read to the calls of the value with them bound
(`ir.visitConstant`), so an instance never read is never built, and the value is never laid out in the data section. Any
other static let of a generic type is one value, read through the type itself (`Box.limit`); read through an instance or a
typealias of one it is maxon's `E2073`. A `static var` is not supported.

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
an error of any type. A failure that reaches the end of a test body (an operation that can fail, a call that throws) ends the executable of tests with a panic that names the test. No program is made of a test: lowering leaves it out. `build`, `run` and `check` read the `.maxon` files
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

- **A parallel loop, `parallel for`, which maxon does not have.** `parallel for i in a upto b`, `parallel for x in xs`
  and `parallel for (i, x) in xs.enumerated()` over an `Array` run their body once for each index or element with
  the iterations split among the processors (see "The parallel loop"). It is a statement, not a library function taking
  a closure, so the body is an ordinary block that may throw; `parallel` is a reserved word, and the conversion warns
  of a Maxon name `parallel`.
- **A service is declared with `service`, which maxon does not have.** `service Name ... end 'Name'` has the body of a `type`,
  and its export instance methods are its messages by declaration, where maxon makes any type a service only where a
  `spawn` of it appears (see "Services"). Its rules (`E3216`, `E4014`, `E3956`, `E3217`, `E3140`, `E3221`, the policies) are
  checked once, at its declaration, in its own module, where maxon checks them at a spawn, so a service that no module spawns
  is refused too. `service` is a reserved word, so the library's module that held the overload policies is `services` and
  the conversion warns of a Maxon name `service`, and writes `service` for the `type` of each type that a converted program
  spawns, by its name (the files of one run are all read for the spawns, and the code of a spec test is one program), a generic
  type excepted, which a service cannot be yet.
  `spawn` starts a `service` alone, where maxon's starts any type, so a `spawn` of a `type` is `E3134`.
- **An interface may say `requires service`, which maxon does not have.** The methods that meet its requirements are held to
  a message's rules, so a message may call one through a value of the interface (see "Services"); the types that implement it
  are no services. Where maxon refuses every method that meets a requirement for waiting (`E3210`), this compiler does so for
  those of such an interface alone.
- **A mailbox has no capacity.** A program writes `spawn T.create()` where maxon writes `spawn(capacity: n) T.create()`,
  sends with no `sendTimeout:`, and matches only `serviceStopped`, the one case of `SendError`, where maxon matches
  `mailboxFull` too: a send never fails or waits for room, and fails only once the service has stopped. The runtime slows
  a sender of a service that is behind instead (see "Services"), so no program chooses a number. `MailboxCapacity` and
  `E3192` (a message parameter named `sendTimeout`) do not exist; `cmd/convert` drops a spawn's capacity, a send's
  `sendTimeout:` and a `mailboxFull` arm, and a spec test that is about a full mailbox or a timed send is skipped with
  that reason.
- **A message kind may name an overload policy**, `@policy(T)` before its `export function`, which maxon has no form
  for: what becomes of its work while the service is behind (see "Overload policies"). A send of a kind with a reply and
  a policy other than `Keep` fails with `SendBusyError` (`serviceStopped`, `busy`), where maxon's send of any kind fails
  with the message's `sendError`; every other send fails with `SendError` as before. A policy with state is a field of
  the service, `@policy(self.field)`. A type a message of which names a policy has a message union, `Api.Message`, that the
  compiler declares, which a policy reads its messages as (see "The message union"). The policies are the library's
  (`stdlib/service`), and maxon's spec tests name none.
- **A service may be stopped at once**, which maxon has no form for: `self.stopNow()` or `h.stopNow()` ends a service whose
  type implements the library's `StopsNow`, and its queued messages are not handled, so the awaits of their replies fail
  with `serviceStopped`, a case the compiler adds to the error of every await of such a type's replies; a shed message's
  await fails with `busy` so. maxon's `shutdown()` is kept, and `self.stop()` in a message is the same stop of its own service;
  a task awaits a service's end with `h.stopped()`, which maxon has no form for either (see "Stops"). maxon's spec tests name
  none of them.
- **Poly1305 computes in limbs of 64 bits.** The library's `Poly1305.mac` is an overlay of maxon's (`cmd/convert/overlays`, see "Modules"): three limbs of 64 bits and a 128-bit product by `__Builtins.mulHigh`, which maxon does not have, where maxon's five limbs of 26 bits hold 16 words at once in the loop, more than x64's registers (`E5001`, which never spills). The tags are the same; only the time differs.
- **A discard takes a value something made.** `_ = e` is maxon's `E3067` when nothing in `e` does anything: no call, send,
  await, async call, spawn or panic, outside the body of a closure, which making the closure does not run. So `_ = x`,
  `_ = p.x`, `_ = xs[i]`, `_ = 7`, `_ = Color.red`, `_ = x + 1`, `_ = (x)`, `_ = x as T`, `_ = try x as T otherwise 0`,
  `_ = "{x}"`, `_ = Self{x: 1}`, `_ = Opt.some(x)` (a case of a union is made, not called), a `match` or an `if`
  expression whose branches call nothing, and `_ = function() gives f()` are refused, whatever the type, copyable or not;
  `_ = f()`, `_ = p.moved().x`, `_ = try f() otherwise 0`, `_ = try x as T otherwise panic("...")`, `_ = await p`,
  `_ = try h.send(m) otherwise ...`, `_ = Opt.some(f())` and `_ = x.clone()` are not. A discard of a value that exists does
  nothing, and for an owned value it misleads: it reads as a drop, but a discard of a place drops nothing, so `_ = c` of a
  service's handle keeps the service until the handle's scope ends. maxon admits a bare binding name, `_ = p`, so that
  its unused-name rule (`E3012`) can be satisfied; minimaxon refuses it too, since it does not check `E3012`. maxon also
  decides by the operations the statement emits rather than by what the checker resolved, and words the error
  "expected a function call"; this message names the ways to end a value: pass it to a function that consumes it, let
  its scope end, close a linear one, and for a plain one delete the line, which `fix` does (an arm's `then _ = 0`
  becomes `then break`, which leaves the `match`, maxon's spelling for an arm that does nothing). `cmd/convert` drops
  maxon's `_ = name` and `let _ = value` of a value that exists (names, dots, literals and casts), and an arm's becomes
  `then break`; where a test's expected error is at the line the discard becomes `let ignored1 = value`, which reads the
  value as maxon's does, and so does a `let _ = value` that is the one statement of its block, where a bare `_ = name`
  skips the test with that reason.
- **Two overlapping conditional conformances are refused.** maxon accepts two conditional conformances of one type to one
  interface whose conditions overlap when only one of them declares the member; here the second is `E3941`, as a second
  unconditional one is, since which one holds would otherwise depend on which declares what (see "Extensions").
- **A Character is held in two words.** maxon's `Character` is a record that owns a heap copy of its bytes, so each one
  taken from a String or written as a literal allocates. Here it is two words, laid out as a String is: a cluster of at most
  15 bytes, which is nearly every one, is held in the words themselves and owns nothing, as a String of so few bytes is, and a
  longer one is a String of its bytes (see "Text"). What a program sees is maxon's: the same equality, order, copies and drops.
- **More commands run at background priority.** Maxon lowers only `spec-test`, which puts itself at background priority
  (`Process.enterBackgroundPriority()`) once its worker branch is past, so its workers and test binaries inherit it.
  minimaxon does so for `test`, `selftest`, `fuzz`, `build` and `check` as well, and its gate lowers itself the same way.
- **A copying door bounds its element Copyable.** A method of a collection that hands out a copy of an element (`get`,
  `first`, `last`, `append`, `filter`) takes the element type Copyable, so a promise there is refused by the bound
  (`E3017`), where maxon refuses it at the door (`E3141`). Both refuse the same programs.
- **Frozen and fresh data are different types.** A frozen value has the type `frozen with T` (see "Immutable values, cowns and behaviours"), so a
  match, a ternary, an assignment or a result that joins frozen and fresh data is a type error (`E3005`), where maxon gives
  the joined value the read-only side and refuses a write through it (`E3197`).
- **An operand read before a write in the same statement is a value.** Operands are evaluated left to right, and an
  operand that reads a place, `s.byteLength()` or `s.clone()`, is a value of its own before a later operand's `mutates`
  access to the place begins, as a field that the `mutates` argument does not reach is a place of its own; maxon refuses
  both (`E3186`), since its hold of the root cannot protect a value that the by-reference cell overwrites. An operand that
  still borrows the place when it is written, a `let` argument or the receiver of the enclosing call, is refused here too
  (see "Exclusivity").
- **A String is built by interpolation.** There is no `StringBuilder` and no `String.append`: an interpolation assigned
  back to the String it starts with, `s = "{s}{x}"`, appends in place, and an interpolation is one allocation with each
  part written into place (see "Text"). `fix` (which `specs.ps1` and `stdlib.ps1` run) writes a statement `place.append(x)`
  of maxon's String as that interpolation, and `cmd/convert` writes a builder that only gathers text (made by
  `StringBuilder.create()`, then statements `b.append(x)` and one `b.build()`, its last use) as that String.
- **Source text follows Zig's rules and two more** (see "Source text"). As in maxon, a name is ASCII and a non-ASCII
  character outside comments and string literals is `E1010`. The rest differ: maxon accepts malformed UTF-8 in a comment
  (in a literal it is `E1004` there too), and control characters, NEL, LS, PS and the bidirectional controls in comments
  and literals, and reads a lone carriage return as a line end; it accepts a byte order mark at the start of a file. In
  code maxon reports each of those as `E1010`, where minimaxon gives the rule's own code (`E1004`, `E1900` to `E1903`).
  The conversion writes such a character of a literal as an escape and of a comment as its code point, and drops a byte
  order mark, so the spec test that one is accepted is skipped.
- `Map` is the library's own (`stdlib/map/Map.maxon`), not converted from maxon's: it is laid out on the design of Go's map (`internal/runtime/maps`),
  groups of eight slots with a control word each, keys beside their values, tables of at most 1024 slots found by a
  directory over the hash's top bits, where maxon's probes linearly over columns of keys, values, states and hashes. Its
  API and its errors are maxon's; its capacities (`getCapacity()` is 8 for a map of one entry, where maxon's is 16), the
  order its entries are walked in, which maxon leaves unspecified, and its unmarked fields are not, so the spec tests that
  pin them are skipped (`self/driver/skips.tsv`). The built-in map is gone: a map literal makes it by calling its static
  `init` with an array of the keys and one of the values, the type found by its name in the library's `map` module (`ast.libraryMapName`, `ast.libraryMapModule`)
  where maxon's compiler looks for the type that implements `BuiltinDictionaryLiteral`, which the conversion leaves out of
  the header with the other literal markers (`Builtin*Literal`). maxon's `Map` has no subscript, `replace`, `isEmpty` or
  `dropEmpty`, which the built-in had, and a `SlotKey` is no key, since it is not `Hashable`. A payload-free enum is a key,
  as maxon makes it: the declare stage gives it `Hashable` and `Equatable`, with a `hash()` that is its ordinal and an
  `equals(other)` that compares the ordinals, unless it declares the method itself (`check/enums.maxon`). An enum of a module
  of the library other than its root, which the root depends on, is given neither.
- **A `Map` or `Set` insert consumes what it stores.** maxon's `insert` and `upsert` take `key immutable Key` and keep it by a
  retain; here a value has one owner and a `String` no count, so a borrowed key that the table keeps would have to be
  copied, out of sight of the program. `Map.insert`, `Map.upsert` and `Set.insert` take the key, the value and the element
  `consumes`: a value built for the call moves in with no copy, and a caller that goes on using its key writes
  `key.clone()`. A key that is an element of a collection is `E3901`, and one that is used after the call `E3102`, and
  `fix` writes the `.clone()` for both: for `E3102` at the argument the local moved into, when a `consumes` parameter
  took it and its type can be copied. A frozen value given for a key or value of the type it holds is `E3197`, as at any
  `consumes` parameter, and `fix` writes `thaw(...)` around it when that type can be copied: maxon's map keeps the
  frozen value, and here it keeps an owned copy, which is what the library made before.
- **A `String` is two words in place**, its bytes' address and a word of its length, an ASCII bit, an owned bit and a
  view bit, where maxon's is the address of a counted box, and one of 1 to 15 bytes holds them in the two words (see "The IR"). A literal is its bytes in the executable's read-only data, copied
  by no operation; an owned String's bytes are a heap buffer of their own, freed by the String that owns them; a view's are
  another String's, so it frees none, and its copy copies them. A field, an element, a capture and an interface value hold
  the two words, and a call passes them in two registers.
- **A slice of a String is a view, and keeping one takes a copy.** `sliceBytes`, `slice`, `sliceLength`, `trim`,
  `trimStart` and `trimEnd` lend a view of the bytes of the String they are called on, made with no allocation and no copy,
  where maxon's give a String of their own (see "Ownership" and "Text"). A view is read, compared, interpolated, sliced
  again, passed to a `let` parameter and bound to a `let` or a `var`, which holds the String it views for its scope and,
  for a `var`, may be given another view of it; storing it in a
  field or an element, returning it or passing it `consumes` (a service's message among them) is `E3901`
  (`E3900` from a `let` that holds one, or where the String is itself borrowed), a behaviour taking a `let` that holds one
  is `E3909`, and changing or moving the String while a `let` views it is `E3186`. Each names the copy, `.clone()`, which `fix` (that `specs.ps1` runs)
  writes at the place the message names, so converted maxon code keeps its meaning with one copy where maxon made one.
- **A `cstring` costs a copy in each row that takes one.** `cstring` and `String.cstr()` are the library tiers' (`E3185` elsewhere), as in
  maxon; `cstr()` is not mutating, as there. A `cstring` is the two words of the String it was made of, a type of its own whose
  drop and copy own nothing (the String must outlive it), so `cstr()` costs nothing, but a String has no spare byte for a
  terminator where it is a view, a literal or holds its bytes in its words, so each runtime row that takes one makes the
  zero-terminated copy itself (`cStringOf`) and frees it before it returns, where maxon grows the String's own buffer by a
  byte. The words carry the length, so a row of bytes takes it as it is. `String.fromCString(cs)` is a new String of the
  bytes before the first zero byte, as maxon's. This is a cost, not a language difference.
- `String` implements `Hashable`, `Equatable` and `Cloneable` through the library's extension, which the conversion writes from
  the header of maxon's `String.maxon`, with the methods that the built-in `String` lacks (`hash()`) from maxon's source; the
  built-in `equals` meets `Equatable` and is called in place through a bound, and `Cloneable`'s `clone()` is the copy every
  `Copyable` value has here, which no type declares (a generic type's `Cloneable`, which holds only where its type
  arguments are `Copyable`, is left out). It is `Comparable` by `stdlib/core/StringComparable.maxon`, minimaxon's own, since
  maxon's compiler makes it so with no declaration in its library.
- `name immutable T` on a parameter, or a result's `immutable T`, where `T` is a type parameter alone, is a plain `T`: a
  parameter is read-only already, and a generic body cannot give a frozen value of a `T` it holds. Of any other type it is
  `frozen with T`.

- x64-linux: `FilePath.from` turns a backslash into a slash
  and refuses nothing on Linux, as maxon's does. A run of a program found on `PATH` looks for it as `execvp` does, in the run's
  own `PATH`; the run passes the child the run's environment.
- `Process.setExitCode(code)` sets the code the process ends with when `main` has returned and no behaviour is left,
  in place of `main`'s; the last code set wins. Maxon has none: its programs wait for their services with `await`, and a
  behaviour, which nothing awaits, needs this to report the program's result.
- **There is no `acquire`.** maxon's `acquire a, b 'm' ... end 'm'` takes the states of several services on the caller at
  once and runs a block over them. minimaxon does not have it, and will not: states that share an invariant are one
  service, whose message changes them together (a `Bank` that owns its accounts, with `transfer` a message), and an answer
  comes back as a message or a `Reply`. The spec tests of an `acquire` block are skipped with that reason.
- **There is no program-wide registry (`default`).** maxon's top-level `default Key = <expression>`, with `Key.current()`
  and `Key.register(x)`, declares a global service or value that any thread looks up. minimaxon has no globals of that kind:
  a program creates its services and passes their handles, explicitly, to whatever needs them. The spec tests that declare
  a default are skipped with that reason.
- **There is no `parallelBoundary` and no `currentStackBytes`.** maxon's `Scheduler.parallelBoundary()`
  (`__Builtins.parallelBoundary` in the library) marks a function that only computes as one that `async` may start, and
  `Scheduler.currentStackBytes()` (`__Builtins.gtStackBytes`) reads the size of a green thread's growing stack. minimaxon
  has neither: `async` starts only a function that may wait (`E3073`), and CPU work is spread over the processors by a
  parallel loop or a service; and a green thread's stack is fixed and never moves, so it has no size to read. The spec
  tests that call either are skipped with that reason.
- **An async call lends its coroutine what it passes, instead of sharing it** (see "Coroutines: async and await"): a place
  at a `let` parameter is the coroutine's to read until the promise is awaited, so the caller may read it but not change
  it, move it or pass it `mutates` meanwhile (`E3943`, minimaxon's), where maxon lets the caller write a `var` its
  coroutine reads, kept alive by reference counts; a temporary at a `let` parameter is refused (`E2015`), where maxon
  counts it. The spec tests that write such a variable while the call is pending are skipped with that reason. A tied
  promise is any whose call lent a place, not only mutable data, and it may not be assigned to a variable declared before
  it. An async call takes at most eight arguments, and none that is not `Droppable` at a `consumes` parameter (`E2015`),
  where maxon refuses only a float result. `p.cancel()` waits for a coroutine that has started, as maxon's does for a tied
  promise, where maxon's returns at once for any other and lets the coroutine run on behind it: what the coroutine prints
  after its cancel comes before what the caller prints after it. So does the drop of a promise in a container, which
  cancels it as `p.cancel()` does, where maxon's returns at once; an array's or a record's drop cancels all of its
  promises before it waits for one, so a coroutine there that has not started never starts, but in a container of
  containers a coroutine that has not started may start while the drop waits for one before it that has. A second take
  of one slot is found at run time (abort 118) where maxon's check refuses the ones it can see (`E3141`), and a value
  that holds a promise is not put in an interface value (`E3919`), where maxon walks its conformers at the doors that
  share it.

- `Clock.nowNanos()` is a built-in and the rest of `Clock` the library's (`stdlib/clock`), so `Clock.nowMs()` and the others need no more than naming. Natively a reading is the
  performance counter (`QueryPerformanceCounter`) in nanoseconds; the interpreter's clock moves by 1000 ns at each reading, so a run is deterministic.

- `Scheduler` has `processorCount()` and `machineCpuCount()`, which maxon's give as `SchedulerProcessorCount`, here a `Count`,
  `yield()`, the pins `pinToMachine()`, `unpinFromMachine()` and `pinDepth()`, and `currentMachine()`, the thread running
  the green thread numbered from 1 in the order the threads started; the library tiers have maxon's floors
  `__Builtins.sleep` and `cpuCount`. maxon's `parallelBoundary` and `currentStackBytes` are dropped (above). Not yet:
  maxon's `quiesce`, `counters` and `MachinePin`.

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
  trails off is the parser's generic `E2010`, and a default on a closure parameter has no syntax.
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
- maxon's `Array.maxon` converts to an extension of the built-in `Array` (`keptBuiltinMembers`: `cursor`, `createIterator`, the
  `ArrayIter` typealias, and the `Iterable` conformance with its type arguments), the iterator type, and the extensions that
  `cmd/convert` renames to the built-in table's `T`, with the field `managed` written `self.managed`, the members the
  built-in declares left out, its conditional `implements` kept, and maxon's `__Builtins.elementsHashAsStorage()` and
  `__Builtins.hashArrayStorage(self)` written as the built-in's library-only `self.hashesAsStorage()` and
  `hashValueOfDigest(self.storageHash(processFoldStart()))` (`storageHashCalls`). `String.maxon`
  keeps `BytePos` and `GraphemeIndex` (`keptBuiltinDeclarations`). A ranged typealias that names no type parameter and is declared in a generic type or an extension of one is made a top-level declaration (`hoistRangedTypealiases`, maxon's `Array.NodePower`), a helper's name that a public member of the root names stays `public` (`publicHelperNames`, `SortComparator`).
- An iterator lends its element: `cmd/convert` writes `returns borrowed` on the requirements of maxon's library interfaces
  that hand one out (`lentRequirements`: `Iterator.current()`), so a loop over an iterator, and the code of an extension of
  `Iterable` that walks one, borrow each element where maxon shares it by a reference count. Moving it on is `E3900`, and
  `minimaxon fix` writes the copy, `.clone()`, with the `where ... implements Copyable` the method then needs. A method that
  gives a value meets a requirement that lends only where the value is a scalar that is not a float (`E3016` otherwise).
- maxon's `Cloneable`, `clone() returns Self`, is met by the `clone()` every Copyable type has (see "Generics"): the
  interface extends `Copyable`, so a type that implements it and cannot be copied is `E3918` where maxon would refuse its
  generated `clone()`, and no type declares a `clone()` of its own: `cmd/convert` leaves out a library `clone()` whose body
  is `return self` (`PrimitiveExtensions.maxon`'s), which is that built-in clone. maxon's `Array` is `Cloneable` whatever its
  element; here it is where its element is (`stdlib/core/ArrayCloneable.maxon`), since an array of an element that cannot be
  copied cannot be.
- `interface Sub extends Base`, where `Base` has associated types, gives `Sub` those it does not declare itself (see
  "Generics"), so `implements Sub with X` binds `Base`'s `Element` too, where maxon counts only `Sub`'s own and compares an
  inherited requirement as written unless the clause binds `Base` beside it (which minimaxon also reads). maxon's
  `E2066` for `implements Sub with (X)` is therefore not raised.
- `returns borrowed T` (see "Ownership") is minimaxon's: maxon has no function that gives a place instead of a value,
  so a lookup through a function there gives a copy. It is Hylo's `subscript` with only a `let` accessor, written as a
  function and restricted so that it needs no coroutine: the place is returned, never yielded, so no code of the callee
  runs after the caller's use of it, and a projection borrows its arguments for as long as the caller uses it, as Hylo's
  does. Its `return` of the contents of a frozen value is minimaxon's too. Its `mutates` form is a method with a `mutates self`
  receiver, which lends a place the caller may change (Hylo's `inout` accessor). Not built yet: subscript syntax on user
  types (`v[t]` calling such a function).
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
- `for` loops (see "Primitives"): a `Map`'s entries are lent, not copied, as a tuple of two borrows (see "Tuples"), and the
  map is held while the body runs, so a body that changes it is `E3186` where maxon re-reads it; so is a `List` or a
  `String` that the body changes, where maxon's loop over a list ends when it finds no next node. A loop over a `String`
  gives each grapheme cluster as a `String`, a view of the walked String's bytes, where maxon's gives a `Character`. `E.allCases`
  and `E.allCaseNames` are a new array each time they are read, since an array has one owner, and `Shape.unionCases` is not
  supported. Not supported, and skipped by the converter: a loop over `graphemes()`, `characters()` or `lines()`; a loop over `split(delimiter)` walks the array of its pieces. maxon's `for (iter, x) in xs.withIterator()`, whose iterator the
  body asks for `iter.index()`, is `for (i, x) in xs.enumerated()` here, as in Hylo and Swift: the loop binds the index
  itself and there is no iterator object, so an array is walked with its indexes at the cost of the plain loop, and any
  other collection with the count of its passes, which is what a range's `index()` gives in maxon. The
  converter rewrites the one form into the other, `iter.index()` into the name; a test whose body uses the iterator for
  anything else is skipped. `for x in xs.drain()` is minimaxon's own: a walk that moves each element of an array out and
  leaves the array at count 0 with its buffer, which maxon writes as a loop of `pop()` or `remove(0)` and a `clear()`.
- Match arms name the case of an enum or a union by its bare name; a qualified `Color.red` is `E3075`, once for the match as
  maxon does (maxon's `Type.case` arms of a handler over the combined error of several errors are not supported, see
  "Errors of the program"). A bare `int` or `float` is the full range of its kind only on the right of a `typealias`
  (`typealias Pair = (int, int)`, `typealias Op = function(int) returns int`); anywhere else it is `E3005`, and as the
  target of a cast it has the cast's own wording. `int.fromString` and the other static functions of `int` are library, as
  `float.fromString` is (below).
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
  the box holds another type. A `mutates` parameter of an interface type takes only an interface value. An interface value
  crosses between threads (frozen, sent in a message or a reply, held by a cown, taken by a behaviour, read by a
  `parallel for`) as the value in its box does, which is judged where it is put there (`ast.unsendable`, `E3919` for a
  promise, `E3927` for a borrow, and in generic code at each instance), where maxon walks the conformers the program
  converts to the interface at the doors that share it and refuses one that holds a function value (`E3171`, `E3217`,
  `E3963` are not raised for an interface value; the spec tests that expect them are skipped with that reason). A function
  value in the box crosses into a service, which a message can call only through the interface's methods (`E3221`, unless the
  interface requires service).
- Associated types held at an interface (see "Interface values"): maxon holds an associated type at an interface for the
  whole program wherever a use site writes it so (`Bag with Cursor`), and refuses every conformer of the interface whose
  binding there does not implement that interface (`E3126`), or that binds the interface itself (`E3120`), and the holding
  itself where a requirement takes the associated type as a parameter (`E2015`). Here a widening is a conversion of one
  value, checked where the value is put in the interface value, by the conformance that module sees: `E3126` and `E2015`
  are raised there, a conformer that is never widened is not refused, and a binding that is the interface itself needs
  no widening, since an interface value is one word, so `E3120` is not raised for it (the spec test that expects it is
  skipped with that reason). A bare interface may be held at (`Bag with Cursor` where `Cursor` has associated types of
  its own), and maxon's `E2015` for a holding whose conformers its share walk would follow through ever deeper instances
  is not raised, since no share walk is made (the spec test that expects it is skipped with that reason). A call through a
  value of a bare interface of a requirement whose signature names one of its associated types is `E3120`: the value's
  type must say the binding (`Bag with (Integer, Cursor)`). maxon types such a dispatch by the binding every conformer of
  the program agrees on, refused only where they disagree (`E3119`) or the binding is not a plain word; an agreed binding
  is a search over the conformers, whose answer would change when a module is added, so here the receiver says it, and
  no such fold is made, per module or for the program (the spec tests that dispatch so are skipped with that reason).
- Grapheme clusters follow UAX #29 from the Unicode 16.0.0 tables (without GB9c), where maxon's own rules join only the
  marks in a few ranges; a string of other Extend, SpacingMark or Prepend code points counts fewer clusters here. The views
  of a string (`bytes()`, `codepoints()`, `utf16()`) and their iterators hold a copy of the string rather than viewing it.
- `resize` of an array of a type parameter that may have no zero value is `E3106` in every tier, the library's too, where
  maxon's library grows its slot tables so: a plain `Array` has no empty slot below its count, and a library slot table is
  a `__SlotArray`. Maxon's spec tests whose library program grows one so are skipped with that reason (the cause
  `growsTypeParameter`).
- An `Array` has none of maxon's slot operations, `clearSlot`, `fillSlot`, `takeSlot` and `lendSlot`, in any tier: they
  are a `__SlotArray`'s, which has no `.managed`, so no array or buffer is made of empty slots, and a slot array is walked
  by its own cursor, `__SlotArrayCursor`.
- `FixedArray` (see "Primitives"): it is a built-in array's box, not `stdlib/FixedArray.maxon`'s record over a raw
  buffer, so it has no `managed` field, no `init(managed)`, no member called through the type's name (`FixedArray.set(v, ...)`)
  and no `__ManagedMemory` refusals. `set` and `refill` take `mutates self`, as an array's writes do, where maxon writes
  through a `let`: a borrowed element would otherwise be dropped under its borrower. A `try` around a get or a set that the
  range analysis proves is `E3944`, as any `try` that covers nothing that can fail is (maxon's `E3055` and `E3083` are two of those). A
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
  different numbers are two types. The spelling `t._0` of an element that some of Maxon's specs use is written `t.0` by
  `cmd/convert`. Not supported: a cast between tuple types (`E3009`, `E3226`), a tuple of a type parameter read by field in a
  generic body, a one-element target list `(x) = e`,
  and the `E3064` lint on a tuple assignment of nothing but `_`.
- **The debugger** is maxon's batch mode on x64 Windows only; its interactive REPL, which reads the commands from standard
  input, waits for a built-in that reads it (minimaxon has none yet), and `until`, `pause`, `detach`, the green-thread
  commands (`threads`, `gt`, `gt-backtrace`, `gt-park`, `gt-resume`), `trace` and conditional breakpoints answer an `error`
  until they are built. Its events name a function by its symbol (`export::named`), as the sidecar does, and a `stop` carries
  no `thread` (the runtime publishes no roster of green threads yet). The locals of an inlined body cannot be printed, as the
  sidecar describes none. Stepping finds a call by what the instruction did to the stack (it pushed the address after
  itself), where maxon decodes it with its x64 classifier, whose conversion needs maxon's own code shapes.
- **The debug port is the built-in `Debuggee`**, where maxon's is its library's `DebugPort` (`stdlib/Debuggee.maxon`) over
  `__Builtins.debug*`: the compiler, which is no library tier, calls it, so its Win32 verbs are static functions of a built-in
  record over handles, ids and byte arrays (a `DEBUG_EVENT`, a `CONTEXT`), and the debugger decodes them, where maxon's
  library holds a scratch region and decodes them into `DebugPortEvent` and `RegisterBlock`. A read has no 512-byte limit.
- **Debug info is written only with `--debug-info`**, where maxon writes it by default and turns it off with `--no-debug-info`,
  which minimaxon takes as the default: a build that does not ask for it pays nothing for it (see "Debug info"). Its sidecar
  is maxon's format, version 12, with these sections empty so far: the coverage points, the constant words and the globals of
  the runtime's contract. A function's display name is its symbol; a row is where the span of the code changes (every
  statement's start, and the code of inlined bodies), a line entry where its line differs from the row's before it or it
  starts its function; the layout table adds `ImageTextBytes`, the length of the code the build id hashes, beside maxon's
  `ImageTextRva`; and the header names the compiler as `minimaxon`. The types are minimaxon's layout: a String is two words in
  place, so a String local has two records, the second with the flag `secondWord` (2, minimaxon's) for its word (the length and its bits), and
  the kind `string` is not indirect; an enum's and a union's cases are at their case index, not a raw tag; every integer is
  8 bytes. A register is numbered as the target's register file numbers it (the general registers, then the vector ones),
  and a type is named as the checker spells it. The locals of an inlined body are not described yet (maxon's carry their
  inline site), no local is a constant or a folded record, and a local's records say where it is held, not its lexical scope.
- A compiled program frees each value where its owner dies, as the IR says (see "Drops" and "Native freeing"), with one
  free and no reference counting; Maxon counts references. A panic and a fault print no backtrace after their line
  (the spec runner leaves maxon's `Stack trace:` lines out of what it compares). A frozen value, a cown and a function
  value are the values with a count of owners, always stepped by one locked add, whatever the program. Behaviours run
  as green threads on Go's scheduler, as maxon's do (`runtime/sched.maxon`, `green.maxon`, `monitor.maxon`; see "The
  scheduler"), with `MAXON_MAX_PROCS` and its acquire of several cowns in address order, which minimaxon does with no
  lock per cown. Stacks never move, since a borrow is an
  address in a frame: a green thread's stack is a fixed reserve with a guard page, where maxon's grow by moving up to 1
  GiB. Preemption is asynchronous only, with no check in the compiled code, where maxon's prologues test a guard word:
  the monitor asks again a thread that did not take its ask, and the clocks a program reads are bracketed calls.
  The interpreter schedules behaviours too, and fails a program that leaks a value. A native program that is not built with `--count-allocations` cannot
  tell that it leaked, and a program that panics or traps does not free what it holds.
- Errors (see "Errors of the program"): a `try` block's handler names
  one error, not the combined error of several (refused with `E3059`), and has no terminal `otherwise panic(...)` or
  `otherwise throws ...` form; there is no block handler `otherwise (e) 'label'` on a `try` expression
  inside brackets and no `default throws` in a match. Maxon's `E3168` (a `throws` that nothing throws) is not raised, so a sharper range analysis never breaks a program, but a `try` that covers nothing that can fail is `E3944` (see "Checked arithmetic"). A `try` covers every
  operation of its expression, not one call, and a condition that rules a failure out is a proof the compiler checks
  (see "Checked arithmetic"). Maxon's `E3161` (a handler of a union error must match it exactly once) is not raised: the
  handler owns the error as a local and a `try` block's handler still has to `match` it (`E3084`). A `throw` moves its
  value as a `return` does, where maxon shares it, so `throw holder.fault` takes the field out of `holder`.
- Bit patterns (see "Bit patterns"): only a `bits(64)` wraps; `bits(8)` and the other widths have the checked arithmetic of their
  range, and `int(5)` is a syntax error (`E2010`) where `bits(5)` is `E3148`. A cast to the alias a value already has is
  not refused as unneeded (`E3010`), as for every other alias.
- Raw values (see "Raw values"): integer, float and string raw values of an enum, and an integer, float or string tag on a
  union case; a case written with a character has none yet, and `fromName`, `unionCases` (so no `Shape.unionCases.fromRawValue`
  either), comparing a case with its raw value and using it where a number is expected are the library's and are not here.
  An enum whose cases write raw values interpolates its raw value, as maxon's does, and one that writes none its name.
  A record or function raw value is a constant, as maxon's is, where a call of a literal factory is the literal filled by its
  arguments (maxon reads `Meta.create(1)` so too, never running `create`); here the factory is declared and its body must be
  one `return` of a record literal over its parameters and constants, which a constant of the module may call too, and a call
  of a function that is no such factory is `E2045` or `E3220`, where a raw value's `create` of an array is too (a module-level
  or `static let` may call it, as maxon's may: see "Constants in the data section").
  Maxon refuses a raw value its constant evaluator cannot hold with `E2015` (a `String` or an array in the record, a field
  left out or given twice, a factory with more arguments than fields), `E3095` for a bare function name that two modules
  declare, and `E3155` for a reserved one; here the ordinary rules say what is wrong (`E3005`, `E3086`, `E3006`, `E3036`,
  `E2004`) and a `String` field is fine. A constant, and so a raw value, is built where it is read rather than held once in
  the executable's data, which no program can tell apart since building it runs no code. `.rawValue` of such an enum or union
  is the whole record, which the reader owns, where maxon refuses it without a field
  after it (`E2015`); and the record that the enum or union exports is not held to be at least as visible as it (`E3167`).
- **A result may be less visible than its function**, as a function of Go may return a type that is not exported: `export
  function make() returns Secret` is fine where `Secret` has no modifier, and the caller outside `Secret`'s module holds
  the value without naming the type (see "Modules"). Maxon refuses it (`E3167`), as it does a parameter, a field, a payload,
  a `throws` clause, a `where` or `implements` clause and a typealias's target, which minimaxon still refuses. The spec tests
  that expect `E3167` on a result are skipped with that reason.
- A format specifier (see "Format specifiers") reads the integers, floats and Strings, and leaves any other value as it is:
  there is no `FormattedStringable`, so `{c:verbose}` of a type of the program is not interpolated. An empty specifier (`{x:}`) is no specifier.
- A `get`, a `set` or a `remove` of an array at an index that the range analysis proves inside the count, and a `first`, a
  `last` or a `pop` of an array it proves is not empty, needs no `try` and runs with no check, where maxon's throws and needs one; a `try` around such an access alone is `E3944`, as is any `try` that covers nothing that can fail, whose
  `fix` (and so the conversion of maxon's code, `specs.ps1` and `stdlib.ps1`) removes the `try` with its handler: maxon's
  `E3055` (a `try` on a call that does not throw) and `E3083` (a `try` block with nothing that throws) are two cases of it, and an
  operation the range analysis proves cannot fail, which maxon traps and so never covers, is another. `E3057` for an access that is not
  proven says what the access needs and how to give it, not maxon's text (see "Proven accesses").
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
  `fromString(text)` is maxon's, written in the library (minimaxon's own files, since maxon's are the compiler-owned
  `Builtins.maxon`'s `__int_fromString`, `__float_fromString` and `__bool_fromString`), with the text they accept and
  `ParseError` (`invalidFormat`, and `outOfRange`, see the last item of this section): `int.fromString` is one optional `-` and
  ASCII digits (`stdlib/parsable/IntText.maxon`), with no `+`, no `_`, no prefix and no space; `float.fromString` is a float
  literal's text with an optional `-` (`stdlib/parsable/FloatText.maxon`): digits with `_`, at most one `.` and an exponent with an
  optional sign, whose digits are one big integer that a power of ten scales or divides, so the value is an exact ratio
  rounded once to the nearest double, ties to even, as the compiler reads a literal (`self/ast/floats.maxon`), where maxon
  keeps 768 significant digits and a sticky bit, which rounds the same; `bool.fromString` is `true` or `false`
  (`stdlib/parsable/BoolText.maxon`); and `Character.fromString`, which maxon has not, is a text of exactly one grapheme cluster
  (`stdlib/parsable/CharacterText.maxon`). Text that is not one of these, or has anything after it, is `ParseError.invalidFormat`.
  The integer's and the float's are static functions of `extension int` and `extension float`, generic over `Self` (see
  "Extensions"), so each is read through a typealias as a value of it: `Port.fromString("80")` is a `Port`, and a number
  that is well-formed and outside the range of the alias, or of an integer past `u64.max`, or of a float past `f64.max`,
  is `ParseError.outOfRange`. Each is built on a reader of a prefix of the text from a byte position that returns the
  value and where it stopped (`intTextRead`, `floatTextRead`, not public, in the `parsable` module), which the whole
  text must reach the end of, and which reads the bytes of the String where they are, so a decimal and a `bool` allocate
  nothing. `bool` and `Character` implement the library's `Parsable` (`stdlib/parsable/Parsable.maxon`), whose `fromString` throws
  `ParseError`, where maxon's throws `Error`: a `throws` of an interface is `E3113` here, so a type of the program
  implements `Parsable` by throwing `ParseError`; an integer and a float do not implement it, since an interface's
  `returns Self` is matched against the one type the conformance names and theirs is generic over `Self`; and a
  static requirement cannot be called through a type parameter (`E3116`), so `Parsable` only bounds a parameter.
  There is no float `match` pattern or map key, no `Math`, no format
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
  value, which the behaviour drops once the handler returns. What crosses is one rule (`ast.unsendable`), which a
  behaviour's captures follow too, so `E3200`, `E3207` and `E3214` do not exist and a value that cannot cross is `E3217`
  at the spawn, or `E3140` for a reply. What `freeze` and `cown` are given follows the same rule (`E3171`, maxon's code
  for a value that cannot be shared), since a frozen value is read by every thread that holds it and a cown's value by
  its behaviours. A message that calls through a function value or an interface is `E3221`, where
  maxon's inference counts such a call as no wait: the check cannot see what it runs, so it cannot show that the holder
  never waits on another party (the spec tests that run one are skipped with that reason), unless the interface says
  `requires service`; and so a method that waits may meet a requirement, where maxon refuses it (`E3210`, a requirement
  never waits; the spec tests that expect that are skipped with that reason), unless the requirement's interface says
  `requires service`. A
  factory that throws runs on the spawner's green thread, where maxon's runs on the service's while the spawner waits.
  A send's error is the built-in `SendError` (`serviceStopped`) and gives the arguments no way back,
  where maxon's `<Service>.<message>.sendError` carries them (the spec tests whose handler binds them are skipped with
  that reason). A `Reply with T` is built in, the cell its sender awaits, where maxon's library declares a record that holds
  it, and the state of a service whose state can hold a Reply is dropped once a stop's queued messages have run, where any
  other's is dropped with its last handle (see "Deferred replies"). maxon has no hook that ends a service's state: a state
  that cannot be dropped implements minimaxon's `Ends`, whose `finish` the service calls as it ends (`E3956`; see "Stops"),
  named so since `end` is a keyword. A service is held to an interface's requirements where its handle becomes an interface
  handle, every requirement a message whose send and await fail with no more than the requirement declares, where maxon looks,
  at each send through the interface handle, for the program's services behind it and refuses one that declares the
  requirement another way ("Interface handles"; the spec tests that pin maxon's place for it are skipped with that reason).
  Not yet: `T.request`, a generic service, a message with type parameters,
  one `try await h.m()` over a message that throws, and services in the interpreter. Not yet either, until the design of
  logging is decided: maxon's `Log`, which sits on `SharedValue` there (see "Where it differs from Maxon": there is no
  `SharedValue`); the spec tests that name it are skipped with that reason.
- `stdlib/stream/Stream.maxon` and `stdlib/console/Console.maxon` (`Console.stdin()`, a `StreamReader` over a `StdinSource`) are minimaxon's own,
  written from maxon's (with `helpers/stream/streamBytes.maxon`) until they convert: maxon's name the network's, TLS's, files'
  and child processes' sources, which this library has not got, and spell a constant (`64 * 1024 * 1024 as LineCap`),
  unchecked arithmetic and a receiver that satisfies `mutable self` as maxon's rules allow and these do not. So there is no
  `ChildOutputReader`, `FileStreamReader`, `TcpStreamReader` or `TlsStreamReader` yet, `StreamError` has no `secure`
  case, and a `ByteSource`'s `readSome` takes `mutates self`.
- `Console.stdoutWantsColor()` asks maxon's three questions on Linux too (standard output a character device by `fstat`'s
  `S_IFCHR`, `NO_COLOR` unset, `TERM` not `dumb`), where maxon's runtime answers false there: its Linux lanes have the
  terminal test but not yet the environment, which a libc-less image reaches only through what its entry stub captured
  (maxon's `TerminalRuntime.maxon`), a gap rather than a decision, which this runtime has not, reading the environment from
  the stack the process started with.
- No alias subtyping (an alias implementing another), characters, region capabilities (`isolated` and `immutable` of a
  local, a payload or a receiver, and `modify`), static fields, default parameters. The memory model of [docs/regions.md](docs/regions.md) is implemented: owned
  values, the primitives, frozen values, cowns and behaviours (see "Ownership", "Primitives" and
  "Immutable values, cowns and behaviours"), with an interpreter that runs all of it and a native backend that runs
  all but the list. `freeze` is a function here, `when` is a statement of the library tiers that takes cowns and gives
  names to their values (a program writes a service; cowns and `when` elsewhere are `E3185`),
  and a frozen value is a type, `frozen with T`, where Maxon recovers immutability from facts about a value's history.
  The grammar has none of the rest, so each is a syntax error. Maxon's codes for the rules of its regions (`E3196`,
  `E3198` to `E3203`, `E3205` to `E3209`, `E3211`, `E3214`, `E3226`) are not raised, and the spec tests that expect one
  are skipped with that reason.
- **A value has one owner** (see "Ownership"): binding a place (`let b = a`), storing it, returning it and passing it
  to `consumes` or into an interface value move it, and using the old name afterwards is `E3102`, where maxon's names
  share one record by a reference count and each sees the other's writes; `.clone()` gives a copy that changes alone. A
  plain `match` borrows its subject, so an arm that moves a payload out (into a destructor or a `consumes` parameter)
  needs `match consumes`, which `fix` writes, taking a `let` parameter that is the subject `consumes` too, where maxon's
  arm takes the payload. The spec tests that use a value after it moved are skipped with that reason.
- **A `let` is a read-only place**: a `let` local, a `let` field, a `let` parameter and a match binding are not changed
  through, by a store into a field of it, a `mutates` method or a `mutates` argument (`E2013`), where maxon's `let`
  only forbids rebinding the name and the record it names is written through it. `fix` declares a `let` local `var`
  where that is enough; the spec tests that write through a match binding, a `let` field or a parameter are skipped
  with that reason.
- **A temporary is not changed** by a `mutates self` method or a `mutates` argument (`E2013`), as in Swift and Hylo,
  where maxon changes the temporary and drops it: `X509Reader.over(b).sequenceElements()`. `fix` (kind `bind`) binds it to a
  `var` on the line before its statement (`var x509Reader = X509Reader.over(b)`, then `x509Reader.sequenceElements()`),
  where the line computes it whenever it runs, or where it is a factory's value from places (a static function that makes
  its own type and throws nothing) under a condition (`a and X509Reader.over(b).readBoolean()`).
- **No two names reach one place in a call** (see "Ownership"): a place passed `mutates`, `consumes` or `sets` overlaps
  no other argument, and a place that a call, an operand or an assignment holds while the rest of it is evaluated is
  not changed meanwhile (`E3186`), where maxon's call shares the place by its reference count. The spec tests that
  hand one place to a call twice, or change it while it is held, are skipped with that reason.
- Array and map literals (see "Primitives") are the maxon literals without a literal spread over several
  lines or constant image data: a `let` literal is a fresh `Array` or library `Map` like any other value, not an immutable image shared by its
  uses, and its bare numbers keep their range as the element type of that one array instead of fitting any alias at each use, so
  passing `let xs = [1, 2]` to a parameter of `Array with Count` is `E3005` until the literal is written where the parameter is.
- `Array` and `List` have the names, the signatures and the errors of maxon's stdlib, but they are built-in
  types rather than stdlib code, and keep these differences: a subscript is a place (`a[i].f = x`, `mutates a[i]`) that
  traps on a miss, where only `get` and `set` throw, as maxon's do; `SlotKey` and the list's slot key API (`prepend`
  and `append` return a `SlotKey`, `first` and `last` return one, `remove(h)`, `next`, `prev`, `moveToFront`) stand where
  maxon has pointers and indexes; `filled` and `dropEmpty` are minimaxon's own; the loop holds the array or the list it walks, so a body that pushes to it or reassigns it is
  refused (`E3186`) where maxon re-reads the live count; `sort`, `sortUnstable`, `sortBy`, `sortUnstableBy`, `cursor`, `createIterator`, `map`, `filter` and `hash()` are the library's extensions of `Array` (`stdlib/array/Array.maxon`, converted, and maxon's six sort files converted from `stdlib/helpers/sort/`, driftsort for `sort` and `sortBy` and pdqsort for the unstable ones, as files of `stdlib/array`, so a sort compares in the order maxon's does; a sort needs `T` `Copyable` and `Droppable`, since maxon's `get` copies an element; `withIterator` is not there), `swap(i, j: j)` (maxon's, `smallSort.maxon`: a panic past the end, over the buffer's `swap`, which exchanges two elements where they are, a managed one moved and never copied or dropped) and `appendMemory(buffer)` (over the buffer's `append`) are library members written over `self.managed`, and `create(n, zeroed: false)` is an empty array with room for `n`, legal for every element (`zeroed: true` is `create(n)`; the flag is a literal), `Array` implements `Iterable` there, `Hashable` and `Equatable` where its element is both, as maxon's library declares (see "Extensions", conditional conformance), so such an array is a `Map` key and a `Set` element, and `Cloneable` where its element is: its `hash()` is maxon's, which folds the hash of each element, or for an array of integers held one to a byte (`ast.storageHashes`, `ByteArray` among them) the fold of its bytes, the hash that `Hasher.combineBytes` gives them, as maxon's `elementsHashAsStorage` does (not yet for the other integer types and the payload-free enums that maxon's hashes as storage too, whose arrays hold a word to an element here); its `equals`, and `==` and `!=`, are the built-in comparison in place of an array of integers, enums or Strings, and of any other element the library's `equalsByElements` (`stdlib/core/ArrayEquals.maxon`, minimaxon's own, which an array's `equals` runs, `ast.runsBuiltin`, and no code reaches by its own name), element by element through the element's `equals` as maxon's `Array.equals`; an array whose element is not both, an array of bools among them, has no `equals` (`E4006`), as in maxon; `contains` and `containsSequence` compare bools, integers, enums and Strings in place, and any other element through its `equals` by the library's `containsByElements` and `containsSequenceByElements` (the same file, run the same way), where the element is `Equatable`, as maxon's are, and otherwise do not exist (`E4006`), `reserve(n)` gives the buffer room for `n` elements exactly where it has less, so that pushing up to them moves nothing, though a capacity is no part of an array's value here (there is no `capacity()`), and `clear`, `truncate`, `resize`, `slice`, `first` and `last` are maxon's. An array never has an empty slot below its count, so a read of one (`get`, a subscript, a `for`, `first`, `last`, a buffer's `get` and a buffer cursor's reads) has no test for an empty slot: the operations that empty and fill a slot are a `__SlotArray`'s (see "Primitives"), which has no buffer view to make an array of; `create(n)`, `resize` and a buffer's `setLength` grow only an element that has a zero value (`E3106`, in every tier, a type parameter included, as maxon refuses it), and over a scalar a grown slot is the element 0; `insert`, `remove`, `pop`, `truncate`, `clear`, `swap`, `set` and `exchange` keep every slot below the count filled; no expression moves an element out of an array (`E3901`), so neither a consume, a match nor a destructuring empties one; and a walk of `drain()` borrows the array while it moves elements out, and leaves it at count 0 however it ends. The one exception is an array of promises, whose element an `await` takes out and leaves empty, as maxon's does, so its reads keep the test. The interpreter holds programs to it: a read with no test that finds an empty slot is a violation, `read of an empty slot`, which the corpus, the spec tests run under the interpreter and `fuzz --proofs` report; the indexes and counts they take are `ElementIndex`; `create(n)` and `resize` refuse an enum element
  some case of which writes a raw value or carries a payload, since a slot holds a case's ordinal and its zero is the first case, which is maxon's case of raw value 0 only where no case writes one, and take no `zeroed:`; and `String` has the byte operations `byteLength`, `byteAt`, `sliceBytes`, `toByteArray`, the static `String.from(bytes)`, and maxon's text methods (see "Text").
  There is no built-in `Option`, which maxon's stdlib has no more of either: an
  operation that can miss throws.
- `ByteArray` is a built-in typealias rather than maxon's in `stdlib/File.maxon`, and the minimaxon `Bytes` buffer is
  gone for it. Only an array of an integer type that fits in a byte or of an enum of at most 256 cases packs its
  elements; maxon sizes a narrower integer's elements by their range too. `String.from(bytes)` copies the bytes where maxon's shares the
  array's buffer, and a `String` here is its bytes, so neither direction scans them.
- The OS functions (see "The OS") have maxon's names, signatures and errors, but are a subset, built in rather than
  stdlib code: `FilePath`'s built-in `from` does not resolve a `file://` URL (one is taken as a path); `File` has no `renameNew` or `openRead`,
  `Directory.exists`, `removeTree` and `removeContents` are maxon's (`stdlib/directory` extends the built-in with the last two), and
  `Directory.list` lists in the order the system gives; `Process` has `environmentVariable`, `executablePath`, `id`, `setExitCode`, `exit` and `enterBackgroundPriority` built in, and the library's environment functions (see "The OS"); `Subprocess.run`
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
  Maxon has `unique` (so `is`, which only this header uses here, `noncopyable` and `destructs` are reserved). `unique` is replaced by this: every owned
  value is already moved and never aliased here, so what is left of it is a value that cannot be copied or forgotten.
  `Copyable` and `Droppable` are structural bounds, not interfaces a declaration implements (see "Capabilities and
  destructors"). `exchange(place, value: v)` takes the label `value`.
- `Type from value` (see "Primitives") is written from a literal only: an array literal for an `Array`, a `List`, or a type that implements `InitableFromArrayLiteral`, a string
  literal for a type with `init`. Not supported: `FixedArray`, `List from <an array value>` and `Set from <an array
  value>` (the elements are written as a literal), a map literal under an
  alias, and `Type from 'c'`. The expression after `from` is a postfix expression,
  where Maxon takes a literal whole: `Alias from [1, 2].count()` is `E3005`, since a call on the literal is not an array
  literal, and the conversion is parenthesized to call on it: `(Alias from [1, 2]).count()`. `from` is still a name where a
  name is required or an operand is (`function from()`, a parameter read as `from`, `String.from(bytes)`), as in Maxon, and
  starts `Type from value` only after an expression. So is `to` (`move(cursor, to: next)`), which closes a range only after a
  bound, a pattern value or the start of a `for`'s range.
- A declaration outside the library tier may not take a name that starts with `__` (`E2051`), as in maxon, and the
  parser refuses it where it reads the name (each name a declaration takes is marked `{declares}` in the grammar). maxon
  reserves `maxon_force_segfault` for a free function too, because its runtime's fault probe has that name; minimaxon's
  runtime has no fault probe, so the name is free.
- A generic type named without its type arguments where a type is written (`returns Inner` for `type Inner uses U`) is
  `E2056`, where maxon reads it as the bare base and refuses only a call on it that needs an instance (`E3162`). A static
  called through a generic type's bare name that neither its arguments nor where its result goes fix is `E3162`, as in
  maxon; a generic function that its call does not fix is `E3179`.
- The reserved words are exactly the keywords the grammar uses, so a word Maxon reserves for a feature minimaxon
  lacks is an ordinary name, and a word that could be read by context, such as `mutates`, `consumes` or `sets`, is
  reserved where Maxon reserves it, except `from` and `to`, which Maxon's programs use as names and are names wherever a
  name is.
- A `while` loop may state an invariant, `while i < n requires n <= xs.count() 'walk'`, which the range analysis proves
  where the loop is entered, at the end of its body and at each `continue`, and then knows inside the loop and after it
  (see "Checked arithmetic"); one it cannot prove, or that compares something it does not follow, is `E3945`, a code of
  minimaxon's own. Maxon has no loop invariants, so `requires` is a reserved word here and not in Maxon: the conversion
  warns of a Maxon name `requires`, as of any name minimaxon reserves.
- A parameter convention is a verb written before the parameter's name, with the function as its subject:
  `push(consumes item T)`, `swap(mutates a T, mutates b T)`, `parse(sets result Ast)`, and `mutates self` or
  `consumes self` for a receiver, and `destructs self` for a destructor.
- Modules are Go's, not Maxon's. Another module's names are always qualified by its path (`net.http.parse()`, and
  `export.X` for the project root's), where Maxon also finds a bare name in the root or as the one exported declaration
  anywhere (`E3095`, `E3063` for two, which minimaxon does not have). `export` reaches the tree rooted at the declaring
  module's parent, as Go's `internal/`, and `public` everyone. Import cycles are refused. The standard library follows
  the same rules within `stdlib/` (see "Modules"): a library file names another library module's declaration by its path
  from the library's root (`core.Ordering`), where maxon's stdlib names any declaration of the library bare, and the
  library is laid out by what a programmer uses, as maxon's is not: each module of `stdlib/` is one thing (`array`, `string`,
  `map`, `json`, `filepath`, ...), the `core` holds the interfaces, the conformances that the built-in types need to meet them
  and the hashing, and `_helpers` is the library's own, which no program names; the library's root holds no file. A program
  names the public declarations of every library module bare, as in maxon, and nothing else of the library. What a file of maxon's
  `stdlib/helpers/` declares `public` is `export` here, which reaches the whole library and no program, where maxon's
  programs see the helpers' names bare (`readOrderedWord`, `TableCapacity`); the spec tests that name one outside the
  library tier are skipped with that reason. The built-ins are public, so a public
  declaration of the library may name `Count` or `ByteArray` (E3167 holds only for a type less visible than public).
- It checks only the rules that the generated code depends on. The style rules are left out: an unused
  name (`E3012`) or typealias (`E3062`), a `var` that is never reassigned (`E3077`), a label naming the innermost block (`E2048`),
  an unneeded cast (`E3010`, so a cast to the type a value has is accepted), self-assignment (`E3067`, whose other half, a discard of a value that already exists, is checked, see "A discard takes a value something made"), a one-value range pattern
  (`E2027`), all bindings discarded (`E3081`), and the lint rules (`E3064`, `E3065`, `E3087`, `E3092`, `E3097`, `E3219`).
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
  where maxon reports the first of a body only.
- **Maxon's raw memory surface is mapped onto `Array` where an operation has a counterpart, and is otherwise not provided**,
  because its safety rests on run-time reference counts and shared buffers that minimaxon's ownership model replaced.
  `__ManagedMemory with T` is the array's primitive here, as in maxon, where `Array with T` and `__ManagedMemory with T` are one
  instance: the array is its buffer, uniquely owned, and `xs.managed` is the array seen as one. The public members of the
  library over it are maxon's own code written over `self.managed`, which `cmd/convert` keeps as maxon wrote it, and a new
  capability of arrays enters as a row of the buffer, checked (a bounds failure throws `__ManagedMemoryError`, through the one
  bounds check the array's reads make) and typed, before it is any member of `Array`. A
  `__ManagedMemory with T` has `length`, `capacity`, `get`, `set`, `swap`, `append`, `remove`, `clear`, `slice`, `grow`,
  `setLength`, `elementSize` and `createCursor`, each the array operation that does the same, taking the argument after the first bare or labelled as maxon's roster does
  (`set(i, v)`, `set(i, value: v)`; a built-in's `name?` parameter in the table), with these differences: `set` is bounded by the length where
  maxon's is by the capacity, `slice` is a copy where maxon's is a zero-copy view of its parent, `grow` is `reserve`, which
  changes nothing a program sees, `setLength` is `resize`, and `elementSize()` is what `sizeof` gives for the element, never
  maxon's negative bit width, since no element is bit-packed. Not provided, because no model-sound operation does the same:
  `setByte` (it writes a buffer under another type than its element's), a `__ManagedMemory` of no element type, `fromCString`, `toCString` and `makeCharFromBytes` (a `String` has `cstr()` and `String.fromCString` instead, and a `Character` is no buffer),
  `shiftLeft` and `shiftRight`, `__ManagedList` and its nodes (a node carries its list by a reference count, where a `SlotKey`
  is an index into a list that the caller names), `__ManagedDirectory`, `__ManagedSocket`, `__ManagedPages`,
  `__ManagedHandle` and `__ManagedPin` (the OS records of the runtime take their place), and the four contracts of a counted
  buffer: `.managed` is a second counted reference that a program may keep, a slice is a view of its parent, a write to a
  shared buffer detaches it, and `Array.init(buffer)` adopts a buffer that other names still hold. A spec test whose subject is
  one of these is skipped, by the cause `rawSurface`, which is an error that names `__Managed...` something that is not
  provided; a test that uses a mapped operation as a tool runs.
- **A shift past the width overflows.** `shl` whose result is past the range of its type fails with `overflow`, as `+`
  does (see "Checked arithmetic"), so `7 shl 65` is a failure a `try` handles, where maxon's gives 0. The spec tests that
  shift past the width are skipped with that reason.
- **No `toString` on a built-in type.** `int` and its ranged and sized aliases, `float`, `bool` and `Character` have no `toString`, and
  so are not `Stringable`: the interpolation `"{x}"`, with its format specifiers, is the one way to make the String of a
  value, and the compiler writes each built-in type itself, from the value's own type (an unsigned 64-bit value as its
  unsigned digits), measured and put in the one buffer of the interpolation with no String of its own. A call of
  `x.toString()` on one is the unknown-member error (`E3004`), which `minimaxon fix` repairs to `"{x}"`, and `cmd/convert`
  writes maxon's `x.toString()` as `"{x}"`. A type of the program that has a `toString()` giving a String is interpolated
  by calling it, and may still be called. (`FilePath` keeps its `toString` until the next seed.)
- **HeapStats reads only what the heap counts.** minimaxon's heap is Go's page heap with per-thread size classes
  (`runtime/heap.maxon`, `runtime/pages.maxon`), so maxon's slab, census and raw-allocation figures have no counterpart:
  `HeapStats` (module `heap`) gives the allocations made, freed and live and their bytes, for the running thread and the
  process, each heap counting its own thread's with a plain add and a reading summing the heaps, and `scavenge()`, a release
  pass of the page heap. The counts by tag are 0, as maxon's are outside a `--debugstream` build. `slab()`, `census(mode)`
  and the raw allocations are left out (`cmd/convert` leaves out a library member that calls a `__Builtins` static
  minimaxon does not provide), and the spec tests that read them are skipped with that reason.

- `Cell` (`stdlib/cell`) stores what `fill` and `replace` are given by a copy, as `Array.push` does, so those two take only a
  `Copyable` element (`where T implements Copyable`), where maxon's takes a unique value too and moves it in; `Directory.list`,
  `create`, `delete` and `currentPath` are built in over the runtime, not over `__ManagedDirectory`. `Json` is converted
  (`stdlib/json/Json.maxon`), with maxon's UTF-8 and UTF-16 helpers (`stdlib/json/utf8.maxon`, `stdlib/json/utf16.maxon`, in the same
  module, which read the surrogate constants of maxon's `Character.maxon`, declared by `stdlib/character`).
- An operand is never `if`, `let`, `var`, `when`, `throw`, `return`, `break` or `continue`, which Maxon reads there as the
  name of a parameter or a function (`return 1 + if(5)`): each starts a statement here, so it keeps that meaning at the
  start of every operand, and a free function named by one cannot be called. A bare read of `while`, `for`, `match` or
  `end`, which Maxon refuses with `E2015`, is the parser's syntax error here.
- **A short String hashes as its two words.** A String of 1 to 15 bytes, which holds them in its two words (see "The IR"), and
  so a Character, is hashed as one finish of maxon's fold over the two words (`hashShortFold` in `runtime/hashfold.maxon`,
  which the interpreter runs too, so a Map's order is the same in both), with no access to memory and no rebuilding of the
  words that maxon's fold loads; a longer String hashes as maxon's does. A short String's hash is therefore not maxon's fold of
  its bytes, nor `Hasher.combineBytes` of a byte array of the same bytes.
- **The thread CPU clock leaves out the scheduler's work.** `Clock.threadCpuTicks()` is the green thread's clock, as maxon's
  is: it stands still while the green thread is switched out. It also stands still while the scheduler does its own work on
  the green thread's stack, which Go runs on the system stack: starting or waking a thread for a processor
  (`startThreadFor`, Go's startm, which a ready or a sleep may do) and taking a processor back after a blocking call whose
  processor the monitor took (`exitBlocking`'s slow path, Go's exitsyscall). maxon counts both, so a bracket across a sleep
  could count a clone of a thread, which under WSL on a loaded machine took up to 8 ms of the thread's CPU.
- **Static members follow Swift's rule.** An extension of a type, an enum, a union, a built-in generic type, `String`, `bool`,
  the integers, the floats or a built-in record may add a `static function` and a `static let`, and the body of an enum or a
  union may declare them, where maxon refuses a static function on an enum or a union and any binding in an extension
  (`E2015`). A static let in an extension of an interface is `E2015`: there would have to be one for each type that
  implements it. maxon has no `static var`, nor has minimaxon yet: one is state that every thread reaches, which Swift 6
  refuses too unless an actor isolates it.
- **A static let of a generic type whose value names its parameters is one for each instance.** minimaxon compiles every
  generic per instance, so such a static let (`static let width = sizeof(T)`) is built for each instance it is read through,
  `CountBox.width` for `typealias CountBox = Box with Count`, and its bare read through `Box` is `E2056`; maxon keeps one
  static let for a generic type. One whose value names no parameter is one value, read through the type itself, as maxon's
  is.
- **A record of words is not a heap allocation where a function holds it.** A record whose every field is a word of nothing
  to drop or such a record, and which has no destructor, is held in place (see "The IR"): a literal, a
  `.clone()`, a local, a parameter, a result, an element of an array and a field of a record allocate nothing (an array
  keeps the words in its buffer, a record among its own), where maxon allocates every record that is not inline. A
  program that counts its allocations sees fewer: two back-to-back `HeapStats.thread()` readings, whose `HeapActivity` is
  such a record, count the same.
- **`Process.exit(code)` ends the process at once**, as Rust's `process::exit` and Go's `os.Exit` do: nothing is dropped,
  no other green thread or service finishes, and an executable that counts its boxes reports no leak; what was written to
  standard output and error is out, since every write reaches them unbuffered. A statement that calls it ends its path as a
  `panic` does: code after it in its block is `E3071`, and a function that ends with it needs no return. The ordinary way to end
  a program is still to return from `main`, with `Process.setExitCode` for the code when behaviours run after it. maxon's
  library has none.
- **There is no identity operator.** maxon's `a is b` and `a is not b` are not in the grammar, so after an operand `is`
  is a syntax error (`E2010`). A value has one owner here, so whether two names stand for one object says nothing a
  program can use; compare the values instead (`==`, or the fields of a record).
- **maxon's `Slot` is `Cell`.** The library type that holds one value or none (`fill`, `replace`, `held`, `take`) is
  `Cell with T` (`stdlib/cell`); `cmd/convert` writes maxon's `Slot` as `Cell` where a program or the library names it, and
  keeps a program's own type of that name.
- **`ParseError` has the case `outOfRange`.** maxon's has `invalidFormat` alone, and answers it for a number past the range
  of an integer or of `f64.max` as well. Here a text that is a well-formed number the type cannot hold (a `Port`'s
  `"70000"`, an `int`'s `"9223372036854775808"`, a float's `"1e309"`) is `outOfRange`, and `invalidFormat` is a text that
  is not written as the type's values are, so a program tells the two apart.
- `try p.get(k).n = v` is minimaxon's: a try before an assignment's target covers the call that lends the place and passes
- **`Process` has not `isAlive`, `namespace`, `endDescendants` or `DescendantProcess`**, and `Process.id()` gives a `Count`, not
  a `Pid` (declared in `stdlib/subprocess`, beside the library's Subprocess): the first three need a raw function that lists or
  tests the host's processes, which this runtime has not. The conversion leaves them out with the helpers only they reach.
  `Process.currentEnvironmentEntries()` and the rest of the environment functions are maxon's, but the interpreter has no way to
  walk its own environment that the compiler's seed compiles, so a program that calls one is `// native-only` and halts under
  the interpreter, as one that enters background priority does.
  its failure on, where maxon has no place a call lends to be changed.
- `SlotMap with T` and `SlotKey with T` are minimaxon's (`stdlib/slotmap`), where maxon has no slot map: minimaxon's
  replacement for pointers, a SlotMap of nodes that name each other by generational key.
- **`__ManagedList` and `__ManagedListNode` are not provided, for good.** maxon builds its `List` on them, a node that
  carries its list by a reference count; minimaxon expresses a non-owning reference as the key of a SlotMap instead, so a
  linked structure is a SlotMap of nodes that name each other by key, and `List` is a library type of its own over an array.
- **`List with T` is a ring buffer over an array** (`stdlib/list`, see "Primitives"), where maxon's is a doubly linked
  list over `__ManagedList`: it keeps maxon's API, but `get(i)` is O(1) where maxon walks to the node, and `insert(at, value:)`
  and `remove(at)` in the middle are O(n), moving the elements on the nearer side of the index, where maxon's walk to the
  node is O(n) and the link O(1). `get`, `first` and `last` lend the element (a copy is `.clone()`) where maxon's copy it
  out; `clear()` and `insert` need elements that can be dropped, and `dropEmpty()` ends an empty list of values with
  destructors.
- **The subprocess rows are maxon's `__Builtins.subprocess*`.** The rows
  (`runtime/spawn.maxon`, "Primitives": `__Builtins`) serve a spawn whose environment is this process's or a block of
  `NAME=VALUE` entries (`envInherit` 0, all of the child's environment, a block that is not one refused as Win32's 87), whose standard
  input is none (the null device), inherited, bytes or a file, whose output and error are discarded, inherited, collected (a limit
  caps what is kept, never what is read) or a file (made or emptied), and whose flags are the hidden window (Windows only) and the new
  process group. They refuse, as a spawn that fails with Win32's 50 (not supported), the flag traced. An input that is held open (kind 4)
  is a pipe that the run keeps open and never writes, and closes once the child has exited. A spawn with the flag detach
  (`subprocessDetach` is the same entry, as it is in maxon's lowering) starts a child that nothing waits for, its streams none,
  discarded, inherited or files (a piped one is refused), and answers its process id, -1 for a spawn that failed and -2 where the
  job this program runs in does not allow the breakaway (`SubprocessError.detachRefused`, with Win32's 5 recorded): on Windows
  the child is created with `DETACHED_PROCESS` and `CREATE_BREAKAWAY_FROM_JOB` and no job watches it, on Linux it calls `setsid`
  and on macOS the spawn has `POSIX_SPAWN_SETSID`, and a POSIX child that has ended is waited for by the event queue as it takes the child's exit (the
  child's pidfd on Linux, its `EVFILT_PROC` on macOS), where no green thread waits on it, which would keep the program from ending while it runs. A deadline ends the child and the tree it started and keeps the
  output it had written (the result's kind is `timedOut`, or `treeNotEnded` when part of the tree still ran five seconds after the
  kill), and `subprocessEndTree` does the same for a child that is not waited for (0, -3 for a tree still running after the bound, -1
  for a word that names no child that no wait has taken, so after a wait it is -1). The tree is a Windows job object that holds the
  child and every process it starts but those started with `CREATE_BREAKAWAY_FROM_JOB`, which escape the kill, and on Linux and macOS
  the child's process group (the child leads one of its own), so that a descendant that leads a group of its own (`setsid`,
  `setpgid`) escapes it. A POSIX child that a signal ended is `signalled` (the code is the
  signal's number) in a result, whose record keeps the raw wait status, while `Subprocess.run` goes on giving 128 plus the signal;
  a child whose tree was killed and that the kill ended reports the kill's code (1). `Subprocess.run`'s deadline ends the tree too.
  `subprocessReleaseHandle` closes the handles of a child that no wait took and leaves it running, as maxon's does; on Linux and macOS the event
  queue waits for it once it ends, so that it is no zombie.
  A handle and a result are `generation * 2^32 + slot`, a `SlotKey`'s word, where maxon's handle is `generation * 64 + slot`, and the
  last failure is one word for the process, as maxon's. A spawn copies the input's bytes into the child's record, where maxon borrows
  the caller's until the wait ends: a cstring of a short `String` is two words that hold its bytes, which are gone once the row has
  returned. `subprocessResolveOnPath` on Windows looks where `CreateProcessW` looks, as maxon's does (the directory of the
  program, the working directory unless `NoDefaultCurrentDirectoryInExePath` is set, the system directory, `System` of the Windows
  directory, the Windows directory, `PATH`, each name with the extensions of `PATHEXT` unless its last component has one), and on
  Linux and macOS walks `PATH` as `execvp` does. `subprocessSpawnStreaming` starts a child whose input is a pipe that
  `subprocessWriteStdinAll` and `subprocessWriteStdinBytes` write (parking while it is full: 0, 1 for an input that is closed or a
  child that no longer reads it, 2 while a payload is queued) and whose output and error are pipes that `subprocessReadAvailable`
  reads when the caller chooses: at most the count asked of what was read and not given yet, parking only while there is none,
  empty at the end of the stream, for a count of 0 or less and for a handle that names nothing, which `subprocessStdoutState`
  and `subprocessStderrState` tell apart (`__SubprocessStreamState`: `open`, `atEof` and `readFailed` as the reads latched them,
  `noSuchChild`). `subprocessWaitExit` parks until the child has exited (-2 or -3 for a deadline that ended the tree, as the
  collect's), `subprocessPollExit` and `subprocessExitSignal` ask without waiting, `subprocessCloseStdin` closes the input, and
  `subprocessQueueStdin` copies one payload (a second answers 2), `subprocessPushStdin` writes what the pipe takes in one write that
  never waits (EAGAIN is blocked on Linux and macOS; Windows keeps one overlapped write in flight) and gives how many bytes are not
  written, closing the input once all are, and `subprocessAwaitStdinWritable` waits for the input entry's write readiness where a
  push was blocked; nothing is written before a push, and no green thread holds the payload. A child is the same record for both spawns, so
  `subprocessEndTree`, `subprocessGetPid` and `subprocessReleaseHandle` serve either. The interpreter halts at each row
  (`// native-only`). maxon's `Subprocess.maxon` is converted over them (`stdlib/subprocess`, next bullet).
- **A Windows argument is quoted as maxon's runtime decides it, but its backslashes are written as the C runtime reads them back.**
  An argument is in quotes when it is empty or has a space, a tab or a quote (it was in quotes for a space or a tab alone, a bare
  quote written as `\"`), and a quote is written `\"`; the backslashes before a quote and before the closing quote are doubled,
  where maxon's runtime writes them as they are: its bytes for the argument `C:\my dir\` are `"C:\my dir\"`, whose last quote the C
  runtime reads as a literal one, a broken argument that takes the rest of the line, where these write `"C:\my dir\\"`. This is
  the quoting of `Subprocess.run` and of the debug port's launch too (`tests/runtime/arguments-round-trip-through-a-child.maxon`).
- **maxon's Subprocess is the library's `MxSubprocess`, beside the built-in `Subprocess`.** `stdlib/subprocess` is maxon's
  `Subprocess.maxon` converted over the subprocess rows (`stdlib/files.txt`), with `helpers/file/osErrorCodes.maxon`, and gives
  `MxSubprocess` (`run`, `runConfiguration`), `Configuration` (`create`, `run`) with `MxExecutable`, `WorkingDirectory`,
  `Environment` (`inherit`, `inheritUpdating`, `custom`), `InputSource`, `OutputDestination`, `MxWaitDeadline`,
  `PlatformOptions`, `MxCollectedOutput` with its `TerminationStatus` (`exited` or `signalled`, the signal's number), `Pid` and the
  `MxSubprocessError` union that carries the output and the codes, as maxon's does. The five types that the compiler also builds in
  (`Subprocess`, `Executable`, `CollectedOutput`, `SubprocessError`, `WaitDeadline`) are written with the prefix `Mx` where the
  library declares them (`libraryBesideBuiltins` of `cmd/convert`), so that a program and the compiler's own source go on using
  the built-ins until they are retired; the built-ins take no `Configuration`, `workingDirectory:` or `deadline:` of maxon's, and
  throw an enum. A spec test whose program runs a child (it names `Subprocess`, `Configuration` or another type only the library
  has) names the library's types (`MxSubprocess`, `MxExecutable`, and so on), unless it calls a built-in that takes the built-in
  `Executable` (`Debuggee`, `runWithInput`) or `WaitDeadline` (`Directory.removeTree` and `removeContents`, waiting), and then it
  keeps the built-ins. It also gives `runDetached` (`SubprocessError.detachRefused` when the job refuses the breakaway),
  `InputSource.hold`, and the streaming child: `StreamingSubprocess` (`spawn`, `spawnWithCwd`, `spawnWithEnvironment`,
  `writeStdin`, `writeStdinLine`, `takeStdout` and `takeStderr` as a `ChildOutputReader`, `closeStdin`, `wait`, `endTree`,
  `processId`, `pollExit`, `pollTermination`, `streamHasEnded`, `outputHasEnded`, `release`), `ChildOutput`, `TreeEnd`, `ExitPoll`,
  `TerminationPoll` and `StdioStream`, over the rows and the hand-written `stream` module (`stdlib/subprocess/childOutputReader.maxon`
  declares `ChildOutputReader` here, since maxon's Stream.maxon declares it over `ChildOutput` and the stream module is below
  this one). `Process.currentEnvironmentEntries` and its helpers (`stdlib/process`) build the environment block of `inheritUpdating`.
  Not yet: a traced child (`StreamingSubprocess.spawnTraceable` with `traced` set, whose spawn fails as the rows refuse the flag:
  the debugger's launch is the built-in `Debuggee`); a status that went to another process (`statusLost`, `reapedElsewhere`: no
  row answers them, since only this program waits for its children); maxon's pumps over a streaming child
  (`helpers/stream/childPump.maxon`, `stdinFeed.maxon`), which are not in `stdlib/files.txt`; and maxon's own `subprocessKill` and
  `subprocessSendSignal`, which its library does not call either.
- **An open file is a linear value, and nothing closes it but its close.** maxon closes a `__ManagedFile` in its destructor
  when the value dies, which a close before makes a no-op (maxon's `close()` is idempotent). Here the language runs nothing
  when a value dies (there is no `deinit`): `__ManagedFile` is a record of the library whose one destructor is `close`, so
  it, and the `FileWriter`, the `FileSource` and the reader over one that hold it, have no copy and no drop. Every path that
  opens a file ends it once, by its close or by a write that closes it, or moves it on, and leaving it open, or closing it
  twice, is refused at compile time (`E3919`, `E3102`), as a linear value is. Exclusive access is proven the same way: a
  read or a write takes the file `mutates`, so no two calls are on one at once, where maxon's may be. The spec tests that let a
  file die open, or assert maxon's close on drop (`managed-file.auto-close`, the readers of `stream-reader.md` left open, a
  handle held in an array or a struct and dropped), are skipped with this reason.
- **A socket is a linear value too, and its close throws.** maxon's `TcpClient` and `TcpListener` close their `__ManagedSocket`
  when the value dies, and `close()` throws nothing. Here `__ManagedSocket` (`stdlib/net/managedSocket.maxon`) is a record of the
  library over the socket rows of `__Builtins`, whose one destructor is `close`, as `__ManagedFile`'s is: it, `TcpClient` and
  `TcpListener` have no copy and no drop (`E3919`), and their `close()` throws `NetworkError` (`sendFailed` where the system's close
  reports an error, the socket closed all the same). A read, a send, an accept and a deadline's set take the socket `mutates`, so
  no two calls wait on one socket in one mode. `TcpClient.maxon` and `TcpListener.maxon` are converted from maxon's
  (`cmd/convert`, files.go). A spec test's program is written as maxon's runs it (`cmd/convert`, sockets.go): a socket it lets
  die is closed where it dies, a discarded one at once, a close is `try ... otherwise ignore` (maxon's throws nothing), a
  parameter that the function closes or moves on is passed `consumes` and one it only accepts, sends or reads on `mutates`
  (and is never closed by it), and an exit between an `async` call lent a socket and its await cancels the promise first, as
  maxon's drop of it does; the compiler's fixes do the rest. A spec test that
  names a type of a facility its target lacks is skipped there (`unsupported on <target>: ...`). A part of the library that a
  system lacks is a row of `internal/targetgen/target`'s `Facilities`: a module outside the library that names one of its types in a
  compile for that system is refused at the name (`E3104`, maxon's `targetUnsupportedConstruct`). There is no row now, every system
  having its sockets (Windows's by AFD poll, `runtime/windows_poll.maxon`).
  A host is an address literal, read by the library (IPv4's dotted quad, IPv6's grammar without zone or brackets), or a name,
  resolved to an IPv4 address as maxon's are: by `getaddrinfo` on macOS and Windows (`__Builtins.socketResolve`), and on Linux, which links
  no libc, by the library itself (`stdlib/net/resolver.maxon`): `/etc/hosts`, then a query of type A over TCP to each nameserver
  of `/etc/resolv.conf`. Like maxon's, the Linux resolver is IPv4-only, with no search list and no ndots.
  **A socket's readiness is a message** (`stdlib/net/readiness.maxon`, "Readiness as a message" in `runtime/eventqueue.maxon`,
  the concurrency plan's E5): `client.notifyReadable(h.post(Conn.Message.readable()))`, `notifyWritable` and a listener's
  `notifyAcceptable` arm the socket's mode with a posted message (`h.post(m)`, the library's `PostedMessage`, as notifyStopped
  takes), which the runtime posts to the service once the
  socket may be ready, and the arm is spent; the handler reads with `readAvailable(most)` (`bytes`, `wouldBlock` or `ended`),
  sends with `trySend(data)` (the count taken, 0 where none) and accepts with `tryAccept()` (`accepted` or `wouldBlock`), none of
  which waits, and arms the socket again. `readAvailableInto(buffer, most:)` reads into a buffer the caller lends, after its
  bytes (`read(count)`, `wouldBlock` or `ended`), so a read allocates nothing of its own and a connection holds no buffer between
  requests; a buffer it grows comes from the thread's heap, whose size classes and kept runs are the pool.
  **A server's sends** (`stdlib/net/sending.maxon`, `socketSendPair` and `socketSendFile` in `runtime/socket.maxon`):
  `sendPair(head, body:)` sends two buffers in one vectored send, `sendmsg` on Linux, `writev` on macOS and `WSASend` on Windows, so a response's
  head and body leave together with neither copied behind the other, and the rest as the socket takes it; `sendFile(reader, deadlineMs:)` sends
  what a `File.openRead` reader holds unread and then the file from its position by `sendfile` on Linux and macOS and `TransmitFile` on
  Windows, a slice at a time, each under its own write deadline, and closes the file. macOS's `sendfile` sends from an offset and does not
  move the file's position, so the runtime reads the position (`lseek`) and moves it on by what was sent, which on a socket that does not
  wait may be part of a slice before it fails with EAGAIN. On Windows a `TransmitFile` call is of at most 256 KB, made with an OVERLAPPED
  whose event a wait completion packet ties to the completion port, so the green thread parks on a user event's entry until
  the peer has taken the bytes (a call without an OVERLAPPED blocks the thread, and a call cannot be cancelled in the middle
  without resetting the connection), and the file's position is read and moved on by the runtime, which the call does not
  do; the call's wait ends at the socket's write deadline (`sendFileUntil`, which the socket's transfer gives the deadline, where
  the other systems' `sendFileCall` waits in the transfer), which cancels the call (`CancelIoEx`, which resets the connection),
  waits for its cancelled completion, and sets the socket's timed-out flag as any send's deadline does. No green thread is tied to an idle socket: the arm is the record in the entry's state
  word (the record plus 1, beside a green thread's address), set by one compare-exchange and no system call, since the
  registration stays edge-triggered; an event delivers it without a green thread or a processor, onto the take's list where
  its service is idle and behind the behaviour ahead where it is not. An edge that came before the arm posts the message at
  once, and a message may come with nothing to take, which the first call answers. Every arm is live and holds a hold on its
  service (the step's decisions 2 and 3), so a program ends once its sockets are closed or the services they would tell have
  ended: a close takes the arm back, its message never handled (a record its event queued already finds the socket closed
  as it starts, the socket kept until then), the end of a service takes back each arm of its armed list
  (`serviceArmedOffset`, one word a service), and a call that waits in an armed mode, `readSome`, `send` or `accept`, takes
  the arm back first (decision 5). A posted message takes no place in the gate (decision 6). The interpreter has no sockets.
  **A connection probes an idle peer, as Go's does** (`runtime/socket.maxon`, connectedOptions): a connected or an accepted
  socket has keepalive on, its first probe after 15 s idle, then every 15 s, the connection ended after 9 unanswered (Go's
  defaultTCPKeepAlive* of net/dial.go). `client.setKeepAlivePeriod(milliseconds)` (`stdlib/net/keepalive.maxon`, a
  `KeepAlivePeriodMs` of 0 to 32767000) sets the idle time, rounded up to a second, as Go's `TCPConn.SetKeepAlivePeriod` does, and
  0 turns keepalive off, as a negative `Dialer.KeepAlive` does in Go. **A listener on `::` is dual-stack, as Go's wildcard
  listen is**: it clears IPV6_V6ONLY before the bind, so it takes IPv4 connections too, whatever the system's default; a
  listener of any other IPv6 address keeps the system's.
  Not yet: a service whose state holds a socket (who closes it when the service ends is undecided), and the 1M-connection benchmark.
- **TLS is not brought over yet.** maxon's TLS sessions (`TlsClient`, `TlsListener`, `TlsStream`) are not in the library:
  `stdlib/tls` declares only what maxon's HTTP library names (`TlsError` and the types of its cases, `TlsClientOptions`,
  `TlsServerOptions`, and what a TLS connection reports of its session; `TlsError`'s `certificateRejected` names the `x509`
  module's `X509VerifyError`), with maxon's names and cases, and each of them and of
  maxon's session types is a row of the facility table that every system lacks, so a program that names one is refused at the
  name (`E3104`) and a spec test that does is skipped as unsupported. An `https` URL is refused with
  `HttpError.unsupportedScheme` until TLS is brought over, once its connection is made, where maxon's would begin its
  handshake: no TLS failure is reported for a handshake that never happened. `HttpServer.bindTls` takes options no program
  can make. The `tlsFailed` cases of `HttpError` and `HttpServerError` are kept, for when TLS comes. A converted file of the
  library leaves out what runs a session (`cmd/convert`, tls.go).
- **The HTTP server is minimaxon's own design, not maxon's** (being built; `stdlib/http`). `HttpServer.bind(host, port:)`
  gives a linear server that `close()` or `serve(handler, config:)` ends, `config` a `ServerConfig.create()` where it is left
  out; `serve` gives an `HttpServing`, whose `stop(deadlineMs)`
  closes the listener, closes each connection that holds no request at once, lets one whose request has begun to arrive
  finish its answer, closes those still open at the deadline, and returns once every connection has ended. The accept loop
  is a service on the listener's readiness (`acceptor.maxon`), which keeps each connection's handle in a slot table and
  learns of its end by `conn.notifyStopped(...)`; at `ServerConfig.maxConnections` it leaves the listener unarmed until a
  connection ends, so the kernel's backlog holds the rest. Each connection is a service of its own (`conn.maxon`) on its
  socket's readiness and one `Timer`; a connection that has sent no byte of a head when its head deadline passes is closed with
  no 408, as an idle one is. `ServerConfig` holds the limits and deadlines, sized for clients that misbehave by
  bug, not for attackers. A request's head is parsed in place over the bytes that have come (`requestHead.maxon`,
  `parseRequestHead`, giving a `RequestHead`): its fields are ranges of one copy of the head, their names matched without regard
  to case against the bytes, and a target or a value becomes a String when it is asked for. Framing is strict: a bare CR or LF,
  an obs-fold, whitespace before a colon, a control byte in a value, two Content-Lengths, Content-Length beside
  Transfer-Encoding, and an HTTP/1.1 request with no Host or two are each a 400; an unknown method and any transfer coding,
  chunked included, are a 501, a version other than 1 a 505, an expectation other than 100-continue a 417, a head past its caps
  a 431 and a body past its cap a 413, answered from the head. `Expect: 100-continue` is answered `100 Continue` before the body
  is read, and the answer to a HEAD carries its head alone. A connection is kept alive after an answer (RFC 9112 §9.3), idle
  under `ServerConfig.idleTimeoutMs`, whose next request's head deadline runs from its first byte; it closes after the answer
  only where the request said `Connection: close`, was HTTP/1.0 without `keep-alive` (an HTTP/1.0 one kept is answered
  `Connection: keep-alive`), was refused, or the server is stopping, and a stop closes an idle connection at once. Pipelined
  requests are answered in order: the bytes past a request are parsed as the next one in the same message, before the socket
  is armed again, where they lie (`parseRequestHead(bytes, from:)`), so no request's bytes are copied for those before it;
  the bytes of answered requests are dropped once, when a read leaves a request unfinished behind them or the last is
  answered. A request's body is copied once out of the bytes that have come, as a `ByteArray`. A connection that closes after an answer lingers first (RFC 9112 §9.6): it shuts its sending and reads and
  drops what its client still sends, for at most `lingerMs` and `lingerBytes`, so the close does not reset the answer before
  the client reads it. Not yet: the drain of an unread body after an answer kept alive (`maxDrainBytes`), which waits for a
  handler that can answer before the body is read: every body is read whole before the answer today, and a refusal from the
  head closes. maxon's `accept()`, `HttpServerLimits` and `HttpServeEnd` are not provided; the spec tests of maxon's server
  and responder (`http-server.md`, `http-responder.md`), which are written to its handler, are skipped with this reason.
- **A handler answers through a linear exchange and returns the token the answer gives** (`stdlib/http/exchange.maxon`).
  `HttpHandler`'s one requirement is `handle(consumes exchange HttpExchange) returns HttpAnswered`, and it throws nothing,
  where maxon's is `handle(exchange mutable HttpExchange) throws HttpServerError` beside `faulted(fault)`, which is gone: a
  handler answers its own failures (`return exchange.fail(StatusCode.internalServerError)`). An `HttpExchange` owns the
  request (`request`, a `RequestHead`, and `requestBody`) and the connection's socket and the answer's outcome, a `Wire`,
  which the connection moves out of its state for the call; each way to answer, `respond(response)`,
  `respondWith(code, body:, contentType:)`, `fail(code)` and `abandon()` (a close with no answer), consumes it and gives the
  one `HttpAnswered`, linear, which only the library makes, and from which the connection takes the `Wire` back. So
  answering twice is a use after a move (`E3102`), and a handler that does not answer has no value to return: maxon's
  run-time stage checks (`alreadyResponded`), its 500 in the handler's place and its report of an unanswered request are not
  here. An answer is a parking send under the write deadline that throws nothing: a failed send is the connection's, which
  closes. The connection states each answer's length and its `Connection` field, as its keep-alive says. An answer that
  cannot be sent as it is (a body on a status that has none, a reason, a field or a content type that would end its line) is
  not sent, and the connection closes, where maxon's throws `invalidResponse`. The handler is shared frozen by every
  connection (`server.serve(freeze(Hello.create() as HttpHandler))`), one count per connection, and is read only; a
  connection calls it in its own message, which `HttpHandler`'s `requires service` allows, so a handler waits on no service
  and names a policy for each kind it sends (`E3210`, `E3957`).
- **There is no TEST-NET-1 short-circuit.** minimaxon has no TEST-NET-1 short-circuit; a connect dials as Go's does and the OS's timeout applies, where maxon's runtime refuses `192.0.2.0/24` without dialling (its two spec tests that connect there with no deadline are skipped with this reason).
- **A file's close throws.** maxon's `close()` throws nothing and its drop discards what the system's close reports. Here,
  as Go's `Close` does, `close()` throws it, `__ManagedFileError.writeFailed`, which `FileWriter.close` throws as
  `FileWriteError.failed` and `FileSource.close` as `FileReadError.failed` (`FileFailure.failed`), so a program that must know
  its bytes reached the file learns it from the close as well as from the write. The file is closed whether or not the close
  throws, and a close is never made again (Linux's `EINTR` is reported, as Go reports it). A call of a close is written with
  `try`, which `minimaxon fix` writes in maxon's code.
- **`FileInfo.createdTime` is a `FileCreatedTime`, `time(seconds)` or `notReported`.** maxon's is a `Timestamp`, which on Linux is
  `st_ctime`, the inode's last change, since a struct stat has no birth time. Here a file's birth time is given where the system
  records one, and `notReported` where it does not, so a program never takes a change time for a creation time; `FileInfo.create`
  takes one. The spec test that compares it with a number (`file-info.timestamps`) is skipped with this reason.
- **A file has a sync, which maxon's has not.** maxon's `__ManagedFile` and `FileWriter` have no way to force a file's data to
  its device. Here `__ManagedFile.sync()` (`mutates self`) does, and `FileWriter.sync()` over it, as Go's `File.Sync` does on
  each system: on macOS `fcntl(F_FULLFSYNC)`, which has the device write its own cache, falling back to `fsync` where the file
  system has none (`ENOTSUP`, an SMB mount's); on Linux the `fsync` system call, made again after `EINTR`; on Windows
  `FlushFileBuffers`. It throws what the system reports as `__ManagedFileError.writeFailed`, the case of a failed write or
  close, which `FileWriter.sync` throws as `FileWriteError.failed`, and the file stays open whether or not it throws. A handle
  that the system refuses to sync, as Go's does, fails the same way and is not made to succeed: a pipe or a FIFO gives the
  system's error (`EINVAL` on Linux), and on Windows a handle that was not opened to write, or a console, gives Windows' own.
  A program that must know its bytes are on the device asks for it, as it asks for the bytes to reach the file by its close. The
  interpreter stops at the sync as at every row of an open file but those of a file that a program makes and writes ("The interpreter").
- **There is no `SharedValue`.** maxon's `SharedValue with T` is a cell that every thread reads and writes, and no atomic
  stands in for it: Hylo has no shared mutable cell, and neither has minimaxon. A setting that changes lives in one service,
  and changing it is a message. A reader that needs it on a hot path keeps its own copy, refreshed by a message (the
  `Latest` overload policy fits). The spec tests that name `SharedValue` are skipped with that reason.
- **TraceCapture is not brought over from maxon.** The spec tests that name it are left out of `specs/`.
- **On Windows a socket's readiness is a poll of AFD, which Microsoft does not document.** An idle socket must hold no green
  thread and no request of the runtime's, as it holds none on Linux and macOS, so the event queue polls the socket through
  `\Device\Afd` (`IOCTL_AFD_POLL` on the runtime's completion port, `runtime/windows_poll.maxon`), the interface under Winsock that
  libuv, mio and wepoll use, instead of keeping an overlapped read of no bytes on each socket. The interface is stable from Windows
  Vista on, and an armed idle socket costs about 1.1 KB of nonpaged pool for it, against about 0.5 KB for a zero-byte read
  (`runtime/README.md`, "The readiness of a socket on Windows"). The runtime loads `ws2_32.dll` and `mswsock.dll` at the first socket and finds
  its functions by name, since a program imports from kernel32 alone. A connected or accepted socket's keepalive (15 s, 15 s,
  9 probes) is set by `TCP_KEEPIDLE`, `TCP_KEEPINTVL` and `TCP_KEEPCNT`, which Windows 10 version 1709 has (1703 for the count),
  so on an earlier Windows the keepalive stays as the system has it.
  A connect to the loopback (127/8 or `::1`) first sets `SIO_TCP_INITIAL_RTO` to no SYN retransmission, as Go's `net/fd_windows.go` does, so
  that a connect to a closed port fails at once: Windows sends the SYN again twice after the RST and the connect waits 2 s for it
  (a loopback loses no packet; before Windows 10 version 1709 the system refuses the call and the connect keeps its 2 s).

## Changing the language

A language change reaches the grammar, the checker's messages, every program that uses the construct (the corpus, `self/`,
`runtime/`, the program generator), `cmd/convert` and the docs, and a difference from current Maxon is recorded in "Where it
differs from Maxon" in the same change. A change to the syntax touches many programs, so tools do the mechanical part:

- **`minimaxon rewrite --keyword=old=new <path>...`** renames a keyword. It lexes each file and changes only keyword
  tokens, so a name, a label, a comment or a string's text that reads the same is left alone, and a keyword inside an
  interpolation is renamed. Where the file parses, a keyword that stands as a name (a function or a parameter named by it,
  a call or a read of one) is left alone too. Run it before editing `Maxon.g4`: once the grammar stops reserving the word, a use of it
  cannot be told from a name, and the command refuses. A file the lexer refuses, such as a test of a lexer error, is
  left alone and listed if it contains the word. In a Markdown file it renames the code: inline code spans, and fenced
  blocks tagged `maxon` or not tagged; prose, and a span or block that does not lex (IR text, a command), are left
  alone, and the first such span holding the word is named.
- **`minimaxon rewrite --services <path>...`** writes `service` for the `type` of each type that a `spawn` starts. It
  checks every program under the paths as far as the checker goes (a directory of the corpus of tests is a program in
  each `.maxon` file and a project in each directory of `modules`, any other directory one project, as `self`, `runtime`
  and `stdlib` are), takes the type that owns the static factory of each `spawn` by what the checker resolved and not
  by text, a qualified `lib.T` declared in another module included, and writes `service` at the `type` of its header, with
  `export`, `is noncopyable` and `implements` left as they are. It takes all the roots in one run, so a type declared in
  one root and spawned in another is found once. A type declared in a file outside the paths, and a generic one, is
  reported and not edited, and so is a `spawn` that the checker did not reach (a program that stops at an error, a test of
  a library tier, which the checker takes for a program), whose type is written by hand. It changes nothing in a type
  that is a `service` already, so a second run is a no-op.
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
  `runtime/` move to the new form and the old one goes. A change that they do not use needs no new seed. A change of a layout
  that the seed bakes into the code it makes and the runtime shares with it (a String's two words) lands in one commit with
  seeds renewed for it by `.\seed.ps1 --staged`, run before it is committed (`seed/README.md`).
- **`.\check.ps1`** runs every check this repository requires (see "Tests"), formatting and applying `go fix` first
  (a tracked file either rewrites fails the run, named and left rewritten for committing, as a stale generated file does),
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
