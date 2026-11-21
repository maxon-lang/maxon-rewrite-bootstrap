# specs

maxon's spec tests, converted. Never edit a file here: regenerate the directory from the maxon repository, from the
root of this one.

```powershell
Remove-Item specs\*.md -Exclude README.md; go run ./cmd/convert --specs --out=specs C:\Users\Eric\Dev\maxon\specs
```

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

`go run ./cmd/convert --specs --check C:\Users\Eric\Dev\maxon\specs` prints the tests converted cleanly and skipped, and
the kinds of warning by the number of tests they block, most blocking first.

## Baseline

`c1.exe test specs` runs the specs in the self-built compiler. The count of passed tests is the measure of Phases 2 to 4:
each feature that lands unskips or fixes tests, so the numbers below only go up.

| date | commit | passed | failed | skipped | wall time |
|---|---|---:|---:|---:|---|
| 2026-11-21 | dc8bc40 and the spec runner | 837 | 5393 | 11419 | 11.4 s to 12.5 s |

Of the skipped tests, 10719 are skipped by the conversion (`<!-- skip: kind -->`, 368 of them the main-in-a-subdirectory
difference) and 700 by the runner (508 `tier`, the rest directives, overlays and result blocks it cannot honour yet).
The failed tests are mostly the stdlib that is not ported (1,064), and the rules in which minimaxon differs from
maxon: an `Integer` where an `ExitCode` is declared, arithmetic that needs a proof or a `try`, implicit copies, `let`
mutability, interfaces as types, and diagnostics that minimaxon has no code for or words differently.

