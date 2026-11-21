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
for a missing feature, to the Go compiler), then the file is regenerated.

## Running the tool

Go may not be on `PATH`; it is at `C:\Program Files\Go\bin`. Run from `c:\Users\Eric\Dev\minimaxon`:

```powershell
go run ./cmd/convert --out=C:\temp\conv C:\Users\Eric\Dev\maxon\stdlib\Array.maxon   # one file, to a scratch dir
go run ./cmd/convert --check C:\Users\Eric\Dev\maxon\stdlib                          # list what would change or warn
```

(Build it once with `go build -o <scratchpad>\convert.exe ./cmd/convert` for repeated runs; keep the exe out of the repo.)

- Input: files or directories (directories are walked for `*.maxon`, skipping dot-directories, sorted by path). No path or
  `-` reads stdin and writes stdout. A single file with no flag writes stdout.
- `--out=DIR` (or `-o DIR`) writes each file to `DIR/<path relative to the given directory>`, creating directories.
  Several files need `--out`, `--write` or `--check`.
- `-w`/`--write` rewrites the input in place. Never point it at the maxon repository; it is for files already in
  minimaxon (for example a file converted earlier that the tool now converts differently).
- `--check` writes nothing, prints the paths that would change on stdout, and exits 1 if any file would change or
  warned (exit 2 is a usage error). It cannot be combined with `--write` or `--out`.
- Warnings go to stderr as `file:line: warning: message`; the same message is left in the output as an inline
  `// TODO: message` comment on that line, so a converted file shows what blocks it.
- The output is passed through the strict formatter (`syntax.Format`). If the formatter rejects it, the unformatted
  candidate is written (`Result.Formatted` is false); treat that as a failure to fix in the tool, not as a file to keep.
- The tool handles `.maxon` only. `.md` specs are not converted today (see "Spec tests").

## Where outputs go

- A stdlib file or a self-contained `maxon-bin/` piece goes under `self/<package>/` (for example the PE writer and x64
  tables to `self/backend/`, which does not exist yet and arrives with plan Stage 1.4 step 8; the diagnostic messages to
  `self/diag/`, `stdlib/Map.maxon` in place of `self/table/`).
- Runtime pieces (the command-line splitter, UTF-16 conversion) go to `self/runtime/`. That directory does not exist
  yet; it arrives with plan Stage 1.4 step 8. maxon's emitter form (`maxon-bin/Compiler/Runtime/*.maxon`) must be
  rewritten into plain runtime source, so convert, then adapt in the tool or the compiler, not in the output.
- Convert into a scratch directory with `--out`, read the warnings, and copy into `self/` only a file that converts
  without a warning (or whose warnings are all for features the plan lands before that package is used).

## Recording where a file came from

Name the maxon source file in the commit message.

## Triaging warnings

Each warning is one of two kinds. Decide per message, not per file.

1. **A feature minimaxon lacks.** The messages say "not supported in minimaxon": floats, tuples, closures,
   default parameters, `static let`, `test` declarations, `#if`, custom `throws` types, `is`/`is not`, byte strings,
   char literals, format specifiers, array/map literals, capabilities, `extends`, and so on. Leave the file out, or skip
   that test, until the feature lands in `self/`'s plan. Do not hand-edit the TODO away. The Go compiler implements only
   what `self/` needs; a feature that nothing in `self/` needs is not added to it.
2. **A rewrite the tool should do.** The construct exists in minimaxon in another form, or the tool misses it: a
   reserved word used as a name ("rename it"), a layout the formatter rejects, a shape `convert_test.go` does not
   cover. Fix `cmd/convert`, run `go test ./cmd/convert`, then regenerate. Add a `convert_test.go` case only when the
   construct is not covered or the bug would come back.
   Many files that get a warning for the same construct mean a tool fix, not many edits.

Plan Stage 1.2a realigns minimaxon with maxon, so some "not supported" warnings (error `throws`, `Option`, built-in
names) describe minimaxon as it is now and go away as the stage lands. Re-run `--check` after each stage and update the
tool so it no longer rewrites what now matches.

`--check` over a whole tree is the to-do list: group the warnings by message (`2>&1 | Select-String warning`) to see which
missing feature blocks the most files.

## Spec tests

Exists today:

- maxon's tests are in `c:\Users\Eric\Dev\maxon\specs\*.md` (664 files). Each is front matter, prose, then a
  `## Tests` section of `<!-- test: name -->` followed by a ```` ```maxon ```` block and its expected result in
  ```` ```exitcode ````, ```` ```stdout ````, ```` ```stderr ```` or ```` ```maxoncstderr ```` (a compile error, with
  `<fragment>:L:C` positions) blocks. Other fenced blocks (`text`, `mm`, `log`) and other comments (`tier`, `procs`) are
  not tests.
- minimaxon's own corpus is `tests/run/`, `tests/errors/` (`// expect-error: CODE [at L:C]`) and `tests/modules/`, run by
  `minimaxon.exe test`. It does not read the spec format and there is no `specs/` directory.
- `cmd/convert` takes `.maxon` files only; the code blocks in specs are not extracted.

Planned, Stage 1.7 (after M1; none of this exists yet):

- `cmd/convert` converts `maxon/specs/*.md` into `minimaxon/specs/`, keeping maxon's format. The directory is
  regenerated, never hand-edited.
- `test` reads that format and runs every test that converted without a warning; a test whose conversion warned is
  skipped and counted. Compile-error tests match maxon's stderr exactly, text included, so a diagnostic worded
  differently from maxon's fails until the message or the tool is fixed.
- The runner compiles in-process and in parallel, shares the checked runtime and stdlib, and caps the number of test
  executables running at once.
- `cmd/convert --check` over the specs lists what still blocks the rest; the passing count is the progress measure.

Until then, to use one maxon test now: copy its ```` ```maxon ```` block to a scratch file, convert it, and if it
converts clean, add it under `tests/run/` (exit code from the `exitcode` block) only when nothing in the corpus covers
that feature (AGENTS.md: tests are added when a bug would come back or a feature is uncovered).

## Finish

Before a commit: `go build -o minimaxon.exe .`, `go vet ./...`, `gofmt`, `go test ./...`, `.\minimaxon.exe test`.
`.\check.ps1` formats and applies `go fix` first, then runs the checks and stops at the first failure. Commit only when
asked, on `main`. A change to what `cmd/convert` does updates the README line that describes it.
