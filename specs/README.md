# specs

maxon's spec tests, converted and then fixed. Never edit a file here but `baseline.txt`: regenerate the directory from
the maxon repository, from the root of this one, once `.\check.ps1` has built `bin\c2.exe`.

```powershell
.\specs.ps1 C:\Users\Eric\Dev\maxon\specs
```

`specs.ps1` runs `go run ./cmd/convert --specs --out=specs` and then `bin\c2.exe fix specs`.

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
  lines moved) gets a `<!-- positions: moved -->` line after its marker, and the runner then compares the codes and the
  messages of the errors and not their `<fragment>:line:column`.
- A multi-file test whose `main` is in a subdirectory only is skipped as `language difference: main is not declared in the
  project's root directory, which minimaxon requires`: maxon finds it there.
- Files with no test (maxon's `AGENTS.md`) are left out.

What the fix changes: `fix specs` checks the program of every test that is not skipped and does not expect a compile error
(a ```` ```maxoncstderr ```` test is about a diagnostic), applies the change that each diagnostic's message names (a
`try ... otherwise panic(...)` around arithmetic or a cast that may fail, `as T` where an integer of another alias is
needed, `.clone()` where a borrowed value would be moved, `consumes` on a parameter that is stored whole and cannot be
copied, `mutates` on a parameter that is written, `mutates self` on a method that writes its receiver, `var` for a `let`
that is changed), formats the block and checks again, until it is clean or what is left names no change. These are
minimaxon's language differences (the repository's README, "Where it differs from Maxon"). A test that would need a
`try` and expects a panic on stderr, maxon's run-time trap, is not rewritten: it gets a `<!-- skip: language difference:
... -->` line instead. Prose and expectation blocks are not touched.

`go run ./cmd/convert --specs --check C:\Users\Eric\Dev\maxon\specs` prints the tests converted cleanly and skipped, and
the kinds of warning by the number of tests they block, most blocking first.

## Baseline

`minimaxon test specs` runs the specs. The count of passed tests is the measure of Phases 2 to 4: each feature that
lands unskips or fixes tests, so the numbers below only go up. `baseline.txt` holds the count `.\check.ps1` holds the
specs to; raise it when more pass.

| date | commit | passed | failed | skipped | wall time |
|---|---|---:|---:|---:|---|
| 2026-11-21 | dc8bc40 and the spec runner | 837 | 5393 | 11419 | 11.4 s to 12.5 s |
| 2026-11-21 | the commit that removed the Go compiler | 840 | 5390 | 11419 | 18.8 s |
| 2026-11-21 | the fix command (`specs.ps1`) | 2251 | 3830 | 11568 | 25 s |

Of the skipped tests, 10719 are skipped by the conversion (`<!-- skip: kind -->`, 368 of them the main-in-a-subdirectory
difference) and 700 by the runner (508 `tier`, the rest directives, overlays and result blocks it cannot honour yet).
The failed tests are mostly the stdlib that is not ported (1,064), and the rules in which minimaxon differs from
maxon: an `Integer` where an `ExitCode` is declared, arithmetic that needs a proof or a `try`, implicit copies, `let`
mutability, interfaces as types, and diagnostics that minimaxon has no code for or words differently.

Since the fix, 123 more tests are skipped for maxon's run-time trap, and of the four rules above only what names no change
is left: a move out of a borrowed value whose type has a destructor or is a type parameter (E3900), a change of a match
binding that borrows its subject (E2013), and the moves that `consumes` makes visible to a caller (E3102). The stdlib
(E3004, E3011) and interfaces as types (E3905) remain the largest groups.

