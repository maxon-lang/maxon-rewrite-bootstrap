# seed

The seeds are the compilers that every build of minimaxon starts from: the self-hosted compiler, `self/`, compiled by
itself, one for each host it is built on: `minimaxon.exe` for x64 Windows, `minimaxon-x64-linux` for x64 Linux and
`minimaxon-arm64-macos` for arm64 macOS.
Nothing else in the repository compiles Maxon, so the gate (`go run ./cmd/gate check`, which `.\check.ps1` and
`./check.sh` run) and the day-to-day build both begin by having the host's seed build `self/`; the gate builds one compiler
that way and tests it, so the seed's code generator is the one the gate tests.

The Windows seed was first built from the commit that added it (whose parent is `ecd1797`), the commit at which the Go
compiler was removed: the Go compiler built `self/` into C1, C1 built C2, C2 built a C3 byte-identical to it, and C2 passed
the corpus and became this file.

## Renewing them

A seed has to compile `self/` and `runtime/`, so the seeds are renewed when they are to use a language feature or a
built-in the seeds do not compile. They are also renewed after a batch of features lands, so that the compiler the gate
tests is not far behind today's code generator. For a language feature, land the feature in `self/` first, written without
using it, then run, on x64 Windows with WSL,

```powershell
.\seed.ps1
```

which is `go run ./cmd/gate seed`. It has the Windows seed build `bin\c1.exe`, C1 build `bin\c2.exe` and C2 build
`bin\c3.exe` (each uncached, C3 with its IR verified), refuses unless C2 and C3 are identical (the gate never builds a C3:
this is the only check that a compiler reproduces itself, so a compiler that does not is never committed) and C2 passes the
suites the gate runs. C2 then builds the Linux compiler by cross-compilation (`--target=x64-linux`), and that compiler is
proven under WSL: it builds a C1 that must be its own bytes, so that a compile does not depend on the host it runs on, that
C1 builds a C2, and C2 a C3 identical to it. C2 builds the macOS compiler the same way (`--target=arm64-macos`), which is
proven the same way on the Mac, reached through the folder both machines mount (`M:\agents\minimaxon\`, the Mac's
`~/Dev/agents/minimaxon/`): the renewal stages the compiler with `self/`, `runtime/` and `stdlib/` at `in\seed\` and
refuses; the Mac session runs `bash ~/Dev/agents/minimaxon/in/seed/run.sh` (`cmd/gate/macseed.sh`), which writes the
compiler's digest to `out/seed/proven.txt` when it is proven; and the renewal run again, which makes the same compiler,
finds the proof and goes on. Each macOS compiler of the proof is built under the seed's own file name, in a directory of
its own, since a Mach-O executable is signed under its name. Only then does C2 replace `minimaxon.exe` and the cross-built
compilers `minimaxon-x64-linux` and `minimaxon-arm64-macos`.

The three seeds therefore always come from one commit of `self/` and one renewal: there is no way to renew one alone.
Neither Linux nor macOS can run the Windows seed, so the renewal runs on Windows, and those machines only build from their
seeds. Windows keeps no executable bit, so the renewal marks the Linux and macOS seeds executable in git's index
(`git update-index --chmod=+x`), without which a clone could not start them.
Commit the new seeds on their own, saying in the message what they can now compile; `self/` and `runtime/` may use the
feature from the next commit on.

## A change of a layout the seeds bake in

A few layouts are shared between the code a compiler makes and the runtime that code links, and are baked into the seed's
code generator: a String's two words are one (`internal/abigen`'s String rows, `runtime/inline.maxon`). A change of such a
layout cannot land as an ordinary change, nor as the two commits of a language feature: the seed can compile neither the new
runtime, which reads the words through intrinsics the seed does not know, nor code that agrees with it. It lands as one commit
that carries the change and the seeds renewed for it, by a staged renewal, run on x64 Windows with WSL before the change is
committed:

1. Apply the change to a checkout of `main` and regenerate (`go generate ./internal/...`); commit nothing yet.
2. `.\seed.ps1 --staged` (`go run ./cmd/gate seed --staged`). The seed builds `bin\c0.exe` of today's `self/` against the
   runtime of `HEAD`, which is the one it was renewed with: it runs from `bin\seed-stage\`, which holds a copy of it, that
   runtime (from `git archive`) and `self/`, since a compiler finds the runtime and its own source by its executable's
   directory. C0 makes code of the new layout, so it builds C1 against the new runtime in the seed's place, and the renewal
   goes on as above: C1 builds C2, C2 builds a C3 identical to it and passes the suites, the Linux and macOS compilers are
   proven, the macOS one on the Mac through the share (run the Mac's `run.sh` when the renewal asks, and the renewal again),
   and only then are the three seeds replaced.
3. `.\check.ps1`: the gate builds its compiler from the new seed.
4. Commit the change and the three seeds together, saying in the message that the seeds were renewed for the layout.
