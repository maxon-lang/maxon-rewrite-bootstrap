---
name: land
description: How an agent lands a change on main in minimaxon — the focused landing gate and which steps a change needs, the rules about red CI, slow tests, tests that are slow only under the interpreter, gate time and what the landing report must carry. Use it before landing or pushing anything to main, when writing a brief that ends in a landing, and when a landing gate or CI fails.
---

# Landing a change on main

Every agent lands its own work, on every machine. A landing is a fast-forward push of a gated, rebased tip to
`origin/main`. Never force-push, never push a merge commit, never touch `seed/` (a seed renewal goes through the
coordinating session).

## The order of a landing

1. **Commit in your own worktree.** The patch is cut from commits against the merge base, never as "the diff against
   origin/main": main moves, and such a diff reverts what landed meanwhile.
2. **Look at CI first.** `gh run list --branch main --limit 3`: when the last finished run of main is red, nothing
   lands except the fix for it. Do not wait for a run that is in progress.
3. **Rebase onto `origin/main`** and resolve your own conflicts. A generated file in conflict is regenerated, never
   merged by hand.
4. **Run the focused gate on the rebased tip** (below). The full gate (`.\check.ps1`, `./check.sh`) is CI's job on
   main; run it yourself only for what CI cannot see.
5. **Push fast-forward.** A refused push means main moved: rebase, run the focused gate again, push.
6. **Report** (below), then watch your commit's CI run. A red run is fixed forward at once by whoever landed it;
   when the fault is another machine's (a Windows fault found from the Mac), say so to the coordinating session at once.

On the coordinator's Windows machine the landing is one script,
`C:\Users\Eric\.claude\projects\c--Users-Eric-Dev-minimaxon\phase3\land.ps1 -tag <tag> -from <worktree>`, which does
steps 2 to 5 in the one landing worktree (`-resume` after resolving a conflict there, `-abandon` to give the tree up,
`-fixesRed` for the fix of a red main, `-full` for the full gate). Run it to its `LANDED` line; never end a turn while it
runs, and never leave the landing tree held. When the script itself faults, fix nothing in it: abandon and report the
exact error.

## The focused gate

Always, on the rebased tip: build from the seed, `fmt --check self runtime tests stdlib`,
`check self --tests --verify-reads`, `check runtime`, `check stdlib`, `selftest`, the host corpus as the gate runs it
(`minimaxon test --verify-ir`),
the spec step (`test specs --require-passed`), and the chain (C2 identical to C3).

By what the change touches:

| The change touches | Add |
|---|---|
| `internal/`, `cmd/`, any `.go` file | `go vet ./...` for `GOOS=windows`, `linux` and `darwin`, and `go test ./...` |
| `runtime/`, `self/backend`, `self/ir`, `self/interp`, or test programs that run under several worker counts | behaviours (`test --behaviours --worker-counts=1,2,4`) |
| `cmd/convert/`, `self/driver/skips*`, `stdlib/`, `specs/` | the spec regeneration (`specs.ps1` on Windows, `go run ./cmd/gate specs` elsewhere) |
| `specs/`, `stdlib/`, `cmd/convert/`, `self/interp`, `self/driver` | `--interpret` on the spec step, as the full gate has |
| the back end, the IR or the runtime | the self-build comparison when a layout, a lock or a hot path moved |

Not in a focused gate: the cross-target lanes and compiles, fuzz, debug info, cache. CI runs them.

**One spec run per landing.** A regeneration already runs every spec test with the interpreter runs and saves the
results. Record mode does not fail on a listed test that stopped passing, so replay the saved results against the list
instead of running every test again:
`minimaxon test specs --replay-results=<abs>/bin/spec-results.txt --require-passed --env-runner=<abs>/bin/envrun`.
It prints `LOST` and exits 1 for such a test. Only when the results file was written during this gate.

**When the full gate is required:** code whose effect only another lane shows and CI has no job for. Otherwise push
the branch as `cloud/<name>` or `eric-pc/<name>`, which CI runs on every job, and land when the job that matters is
green: a Mac change to the Windows timed wait is the example.

## On the Mac

- **The machine lock.** Several agents share the Mac: every build, gate, test, fuzz, bench and selfab run goes through
  `python3 ~/Dev/agents/minimaxon/out/dev/spec-triage/withlock.py <script>`, heavy steps batched into one script. A
  benchmark runs only under the lock, so nothing else perturbs it.
- **The environment.** `TMPDIR=/private/tmp/claude-502`, `MAXON_STDLIB=$HOME/Dev/maxon/stdlib`,
  `MINIMAXON_CACHE=off`; tests get `< /dev/null`. Nothing ever runs with its working directory inside `~/Dev/maxon`,
  which is read-only: name its files by absolute path from elsewhere.
- **No Rosetta.** x64 targets are compiled (`build self --target=x64-windows`), never run.
- **Which lanes are CI's.** macOS runs no Linux or Windows executable: the arm64-linux, x64-linux and x64-windows
  lanes are CI's (`cloud/<name>` for a change only they can see, such as one to the Windows timed wait). The OrbStack
  VM `ubuntu` is there for a Linux reproduction, never `maxon-linux`.
- **The focused gate script** is `~/Dev/agents/minimaxon/out/dev/scripts/focused-gate.sh <worktree> <log name>`: it picks its steps from the
  paths the branch changes against `origin/main`, as the table above does, and keeps a sampler that records a stalled
  FIFO test's process table. Its logs, with the steps by time, are in `~/Dev/agents/minimaxon/out/dev/scripts/logs/<log name>/`.

## Tests

- **Modest repeats.** A fix lands with a test that fails before and passes after. Once a mechanism is proven, do not
  run hundred-fold campaigns.
- **No long test in the gate.** A test over about 2 s a run on a quiet machine is `// runs: manual` (corpus and
  behaviours), `<name>.manual.maxtest` (module tests) or a `manual` row of `self/driver/skips.tsv` (specs), run by name
  when its area is touched. A test that is slow only because it sleeps or polls too long is shortened instead.
- **Slow only under the interpreter is not manual.** Compute-heavy library code (big numbers, field arithmetic) runs
  40 to 300 times slower there: give the spec file, or the named tests, a `native` row of `self/driver/skips.tsv`.
  Time a new spec both ways before landing it.
- **`// runs: alone` is for a short test that needs a quiet machine**, not for a slow one, and not for a test that
  holds nothing a late wake can break. Each alone run costs its lane a drain.
- **A gate test holds what does not depend on the machine's load.** Bounds on a wake's lateness belong in a manual
  test; the gate's version checks order, counts and grid.
- **A failure that does not reproduce is a finding, not a flake.** Report it with what the test was waiting on, and make
  the test say so itself next time (a watchdog that names the step, inside the runner's deadline).
- **Never loosen a bound, mark a test unsupported, or edit `specs/passed.txt` to get green.** A status changes through
  the conversion's rows.

## The landing report

One short message to the coordinating session, before the push ("landing in 1 minute") and after it:

- the commits and what each does, in a line;
- the gate's **total and its slowest steps**, and the step list when it changed;
- the counts: corpus, behaviours, specs passed, lost and new;
- the state of main's last finished CI run;
- anything that failed once, anything left open.

Gate time is watched: a step more than about 15% over its recent runs on that machine is explained in the report.
A saving found on one machine is sent to the others the same day.

## What still goes to the coordinating session first

Seed renewals; a language rule the plan has not decided; a difference from maxon that is not recorded; a landing on a
failing gate (the owner decides, once, for those commits); deleting a branch that is not an ancestor of main; anything
of logging.
