---
name: convert-from-maxon
description: How to pull code and tests from the maxon repository (c:\Users\Eric\Dev\maxon) into minimaxon with cmd/convert. Use when bringing a stdlib file, a self-contained maxon-bin piece or a spec test into self/ or the corpus, when triaging convert warnings, or when changing cmd/convert itself.
---

# Converting from maxon

Maxon code and tests come through `cmd/convert`, not by hand (plan, "Reuse"). The converter rewrites maxon's syntax and
layout into minimaxon's and marks what minimaxon cannot say. Where a piece is not converted it is still the reference
for behaviour, edge cases and names, unless the difference is a deliberate change recorded in README "Where it differs
from Maxon".

Converted files are regenerated from the maxon repository and never hand-edited. A needed change goes to the tool (or,
for a missing feature, to `self/`), then the file is regenerated.

## Running the tool

Go may not be on `PATH`; it is at `C:\Program Files\Go\bin`. The tool lays out what it converts with the compiler's
`fmt`, so build `bin\minimaxon.exe` first (`.\build.ps1`), or name a compiler in the environment variable `MINIMAXON`. Run
from `c:\Users\Eric\Dev\minimaxon`:

```powershell
go run ./cmd/convert --out=C:\temp\conv C:\Users\Eric\Dev\maxon\stdlib\Array.maxon   # one file, to a scratch dir
go run ./cmd/convert --check C:\Users\Eric\Dev\maxon\stdlib                          # each warning kind, its count and files
```

`stdlib/` as a whole is regenerated from `stdlib/files.txt` by `.\stdlib.ps1 C:\Users\Eric\Dev\maxon\stdlib` (on Linux
`go run ./cmd/gate stdlib <maxon's stdlib directory>`): it converts each listed file into its module, then runs the
compiler's `fix` over `stdlib/`. `gate stdlib --check <directory>` (the gate's step "library regenerates" when
`MAXON_STDLIB` names it) fails when the regeneration differs from what is committed, so a change to `cmd/convert`
regenerates and commits the library in the same change.

(Build it once with `go build -o <scratchpad>\convert.exe ./cmd/convert` for repeated runs; keep the exe out of the repo.)

- Input: files or directories (directories are walked for `*.maxon`, skipping dot-directories, sorted by path). No path or
  `-` reads stdin and writes stdout. A single file with no flag writes stdout.
- `--out=DIR` (or `-o DIR`) writes each file to `DIR/<path relative to the given directory>`, creating directories.
  Several files need `--out`, `--write` or `--check`.
- `-w`/`--write` rewrites the input in place. Never point it at the maxon repository; it is for files already in
  minimaxon (for example a file converted earlier that the tool now converts differently).
- `--check` writes nothing and exits 1 if any file would change or warned (exit 2 is a usage error). It cannot be
  combined with `--write` or `--out`. On stdout it prints one line per kind of warning (its message): the count, the
  number of files it appears in, then the message, most frequent first, and last how many files would change. Each
  warning still goes to stderr, so `2>$null` leaves the summary, the list of what blocks the most files.
- Warnings go to stderr as `file:line: warning: message`; the same message is left in the output as an inline
  `// TODO: message` comment on that line, so a converted file shows what blocks it.
- The output is passed through the strict formatter (`syntax.Format`). If the formatter rejects it, the unformatted
  candidate is written (`Result.Formatted` is false); treat that as a failure to fix in the tool, not as a file to keep.
- Without `--specs` the tool handles `.maxon` only; `--specs` converts maxon's spec files (see "Spec tests").

## Where outputs go

- A stdlib file or a self-contained `maxon-bin/` piece goes under `self/<package>/` (for example the PE writer and x64
  tables to `self/backend/`, the diagnostic messages to `self/diag/`, `stdlib/Map.maxon` in place of `self/table/`).
- Runtime pieces (the command-line splitter, UTF-16 conversion) go to `runtime/`. maxon's emitter form (`maxon-bin/Compiler/Runtime/*.maxon`) must be
  rewritten into plain runtime source, so convert, then adapt in the tool or the compiler, not in the output.
- Convert into a scratch directory with `--out`, read the warnings, and copy into `self/` only a file that converts
  without a warning (or whose warnings are all for features the plan lands before that package is used).

## Recording where a file came from

Name the maxon source file in the commit message.

## Triaging warnings

Each warning is one of two kinds. Decide per message, not per file.

1. **A feature minimaxon lacks.** The messages say "not supported in minimaxon": floats, tuples, closures,
   default parameters, `static let`, `test` declarations, `#if`, union errors, `default throws`, `if try`, a block
   handler (`otherwise (e) 'label'`) inside brackets, iterator `for` loops, byte strings, `CharacterSet` and a trim of one, format
   specifiers, array/map literals, capabilities, `extends`, and so on. README "Where it differs from Maxon" lists what
   minimaxon still lacks and what it spells differently on purpose; the converter rewrites the second and warns of the
   first, and nothing else. Leave the file out, or skip
   that test, until the feature lands in `self/`'s plan. Do not hand-edit the TODO away.
2. **A rewrite the tool should do.** The construct exists in minimaxon in another form, or the tool misses it: a
   reserved word used as a name ("rename it"), a layout the formatter rejects, a shape `convert_test.go` does not
   cover. Fix `cmd/convert`, run `go test ./cmd/convert`, then regenerate. Add a `convert_test.go` case only when the
   construct is not covered or the bug would come back.
   Many files that get a warning for the same construct mean a tool fix, not many edits.

Stage 1.2a realigned minimaxon with maxon: the syntax (`implements`, bare alias type arguments, quoted labels,
`panic` of a string, keyword argument labels), error `throws`, `try ... otherwise`, the built-in names and the
module visibility all match, so the tool neither rewrites nor warns about them. When a stage lands a feature or changes
a spelling, re-run `--check` over `stdlib` and remove the rule or the warning for what now matches; a warning for a
feature that works is a bug in the tool.

`--check` over a whole tree is the to-do list: its summary shows which missing feature blocks the most files. After
converting a multi-module project, run `minimaxon qualify <dir>`: the tool works one file at a time and leaves another
module's names bare as Maxon writes them, where minimaxon requires them qualified.

## Spec tests

maxon's tests are `c:\Users\Eric\Dev\maxon\specs\*.md` (664 files, 17,649 tests). Each is front matter, prose, then a
`## Tests` section of `<!-- test: name -->` followed by optional directive comments, a ```` ```maxon ```` block (several
files when it holds `// --- file:` markers) and its expected result in ```` ```exitcode ````, ```` ```stdout ````,
```` ```stderr ```` or ```` ```maxoncstderr ```` (a compile error, with `<fragment>:L:C` positions) blocks. The format is
maxon's `docs/SPECS.md`.

`specs/` holds them converted, regenerated from maxon, never edited. From the repository root:

```powershell
Remove-Item specs\*.md -Exclude README.md; go run ./cmd/convert --specs --out=specs C:\Users\Eric\Dev\maxon\specs
go run ./cmd/convert --specs --check C:\Users\Eric\Dev\maxon\specs   # what blocks the rest, without writing
```

- `--specs` converts each test's ```` ```maxon ```` block (each `// --- file:` section of it) through the source
  conversion and copies everything else, with line endings made LF. A file with no test (maxon's `AGENTS.md`) is left out.
- **The marker.** A test whose conversion warned, or whose source minimaxon cannot parse (and whose expectation is not a
  syntax error, which is then the point of the test), gets one `<!-- skip: kind -->` line per kind of warning right after
  its `<!-- test: ... -->` line. The kind is the warning's message, or `minimaxon does not parse the converted source:`
  and the parser's message. The runner skips and counts a test that carries the line, and runs every other test. Existing
  `skip` lines in an input are dropped and regenerated.
- The warnings are not printed one by one (there are tens of thousands); each is a `// TODO: message` in its test. The
  summary, which `--check` and a conversion both print, is one line per kind: the number of **tests** it blocks (a test
  counts under each of its kinds), the files, the kind, most blocking first, then `N tests: C converted cleanly, S skipped`.
- Triage as for any file: a feature minimaxon lacks is left (its tests stay skipped until it lands); a construct with
  an equivalent is rewritten by the tool, which regenerates `specs/`.
- `minimaxon.exe test specs` runs them (README "Tests"). Until Phase 3 no script runs them all: `.\check.ps1` runs
  only the tests not in `specs/passed.txt`, and `specs.ps1` adds the new passes to that list. The positions in a `maxoncstderr` block are
  authored against maxon's layout of the block, which the conversion changes (blank lines, indentation), so such a
  test carries `<!-- positions: moved -->` and the runner compares codes and messages only.


## Finish

Before a commit, `.\check.ps1` passes: it formats and applies `go fix` first, then runs every check and stops at the
first failure. Commit only when asked, on `main`. A change to what `cmd/convert` does updates the README line that describes it.
