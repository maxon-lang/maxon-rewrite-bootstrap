# specs

maxon's spec tests, converted and then fixed. Never edit a file here, and the runner alone writes `passed.txt`: regenerate the directory from
the maxon repository, from the root of this one, once `.\check.ps1` has built `bin\minimaxon.exe`.

```powershell
.\specs.ps1 C:\Users\Eric\Dev\maxon\specs
```

`specs.ps1` runs `go run ./cmd/convert --specs --out=specs`, then `bin\minimaxon.exe fix specs`, and then `bin\minimaxon.exe test specs
--skip-passed --record-passed`, which runs the tests that `passed.txt` does not list and adds those that pass to it.

The files keep maxon's format (`docs/SPECS.md` in the maxon repository): front matter, prose, and under `## Tests` each
test as `<!-- test: name -->`, optional directives, a ```` ```maxon ```` block and the expectation blocks
(```` ```exitcode ````, ```` ```stdout ````, ```` ```stderr ````, ```` ```maxoncstderr ```` and the target-qualified forms).

What the conversion changes:

- Only the ```` ```maxon ```` block of a test is rewritten, section by section for a multi-file test (`// --- file:`
  markers stay). Prose, documentation blocks and expectation blocks are copied as they are, with line endings made LF.
- A test whose source warned, or that minimaxon cannot parse though its expectation is not a syntax error, gets one
  `<!-- skip: kind -->` line per kind of warning right after its test marker. The runner skips and counts these tests and
  runs the others. The line is the conversion's only addition to the prose; the source keeps the `// TODO: kind`
  comments that say where.
- A test that expects a compiler error and whose block was laid out differently (blank lines, indentation or bracket
  lines moved) gets a `<!-- positions: moved -->` line after its marker, and the runner then compares the codes of the
  errors and not their `<fragment>:line:column`.

The runner compares the errors of a compile-error test one for one by code and place (`<fragment>:line:column`), never
by message: minimaxon words its diagnostics in its own way (the repository's README, "Where it differs from Maxon"), so
the notes and the lines that continue a message are left out too, and an error that maxon reports at no place matches
its code wherever minimaxon reports it.
- A multi-file test whose `main` is in a subdirectory only is skipped as `language difference: main is not declared in the
  project's root directory, which minimaxon requires`: maxon finds it there.
- Files with no test (maxon's `AGENTS.md`) are left out.

What the fix changes: `fix specs` checks the program of every test that is not skipped and does not expect a compile error
(a ```` ```maxoncstderr ```` test is about a diagnostic, and is only skipped for another code, below), applies the change that each diagnostic's message names (a
`try ... otherwise panic(...)` around arithmetic or a cast that may fail, `as T` where an integer of another alias is
needed, `.clone()` where a borrowed value would be moved, `consumes` on a parameter that is stored whole and cannot be
copied, `mutates` on a parameter that is written, `mutates self` on a method that writes its receiver, `var` for a `let`
that is changed, and the bound `where T implements Copyable` or `Droppable` in the header of a function whose body copies or
drops a value of the type parameter `T`), formats the block and checks again, until it is clean or what is left names no change. These are
minimaxon's language differences (the repository's README, "Where it differs from Maxon"). A test that would need a
`try` and expects a panic on stderr, maxon's run-time trap, is not rewritten: it gets a `<!-- skip: language difference:
... -->` line instead. A test that expects a compile error (and is not skipped) is compiled as the runner does, and when
the first code its `maxoncstderr` block lists and the first code minimaxon reports are a pair of `codeSkipTable` in
`self/driver/fix.maxon` it gets one skip line, and nothing else is changed: `language difference: ownership is refused by
another rule` (E3102, E3223, E3224 or E3197 expected, where minimaxon refuses the move itself or a write through a `let`
under E3900, E3919 or E2013; E3207 or E3214 expected, where it refuses the move or the write under E3900 or E2013), `differing error code: syntax error` (E2001 expected, E2010 reported) and `differing error
code: operand types` (E2004 expected, E3005 reported). Any other test that
expects a compile error and whose first reported error carries a fix gets the fixes of the errors whose code is not the
expected one, in rounds as a run test does, until the first reported code is the expected one; it then gets one
`<!-- positions: moved -->` line, since the fixes moved the text the expected positions name, and the runner compares the
diagnostics without them. A test whose first error has no fix, or whose fixes never make the expected code first, gets a
skip line when the code left after the fixes makes a pair of `codeSkipTable` with the expected one, or when the code it was
first refused under is one of the table's `first` rows (`language difference: a constant may call a literal factory, and E3220 names what it may call`,
for E3220), and is otherwise left as it was. A test that runs a program but whose `for` loop changes the array it walks (minimaxon's loop holds its
collection, E3186) gets `<!-- skip: language difference: a loop holds the collection it walks -->`. Prose and expectation
blocks are not touched.

`go run ./cmd/convert --specs --check C:\Users\Eric\Dev\maxon\specs` prints the tests converted cleanly and skipped, and
the kinds of warning by the number of tests they block, most blocking first.

## The list of passed tests

`minimaxon test specs` runs every spec test. The count of passed tests is the measure of Phases 2 to 4: each feature that
lands unskips or fixes tests, so the numbers below only go up.

`passed.txt` lists the tests that pass, one `file.md: test name` per line, sorted. Only the runner writes it. Until the
parallel driver of Phase 3 makes the full run fast, a test that has passed is not run again: `.\check.ps1` runs
`test specs --skip-passed`, which skips the listed tests without compiling them (the one reason `passed before
(specs/passed.txt)`), runs the rest and prints `NEW file.md: test` for each of those that passes now. `--record-passed` with
`--skip-passed` then writes the list again as the old one plus those tests (`specs.ps1` does this after a regeneration);
alone it is the full run that writes the list for the first time, and a usage error when the list exists. A regression in a
listed test therefore shows only once Phase 3 brings the full run back, and plain `test specs` (a full run, which reads
and writes nothing) is the way to see it before. The table below is the history of full runs; the gate's runs are shorter.

| date | commit | passed | failed | skipped | wall time |
|---|---|---:|---:|---:|---|
| 2026-11-21 | dc8bc40 and the spec runner | 837 | 5393 | 11419 | 11.4 s to 12.5 s |
| 2026-11-21 | the commit that removed the Go compiler | 840 | 5390 | 11419 | 18.8 s |
| 2026-11-21 | the fix command (`specs.ps1`) | 2251 | 3830 | 11568 | 25 s |
| 2026-11-21 | a return converts an integer that the range analysis proves fits | 2264 | 3817 | 11568 | 37 s |
| 2026-11-21 | tests that expect another code for a known reason are skipped | 2264 | 3450 | 11935 | 29 s |
| 2026-11-21 | array and map literals, and a block handler on a try expression | 2424 | 3845 | 11380 | 30 s |
| 2026-11-21 | a for loop over an array or an iterable type | 2515 | 4063 | 11071 | 30 s |
| 2026-11-21 | a typealias inside a type | 2568 | 4330 | 10751 | 30 s |
| 2026-11-21 | String and Array methods, the conditional try, byte string literals | 3053 | 3970 | 10626 | 35 s |

Of the skipped tests, 10719 are skipped by the conversion (`<!-- skip: kind -->`, 368 of them the main-in-a-subdirectory
difference) and 700 by the runner (508 `tier`, the rest directives, overlays and result blocks it cannot honour yet).
The failed tests are mostly the stdlib that is not ported (1,064), and the rules in which minimaxon differs from
maxon: an `Integer` returned where an `ExitCode` is declared and not proved to fit (the fix writes a `try`), arithmetic that needs a proof or a `try`, implicit copies, `let`
mutability, interfaces as types, and diagnostics that minimaxon has no code for or words differently.

Since the fix, 123 more tests are skipped for maxon's run-time trap and 367 for the code that they expect (285 ownership,
43 syntax error, 39 operand types), and of the four rules above only what names no change
is left: a move out of a borrowed value whose type has a destructor or is a type parameter (E3900), a change of a match
binding that borrows its subject (E2013), and the moves that `consumes` makes visible to a caller (E3102). The stdlib
(E3004, E3011) and interfaces as types (E3905) remain the largest groups.

