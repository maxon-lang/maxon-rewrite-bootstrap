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
- Files with no test (maxon's `AGENTS.md`) are left out.

`go run ./cmd/convert --specs --check C:\Users\Eric\Dev\maxon\specs` prints the tests converted cleanly and skipped, and
the kinds of warning by the number of tests they block, most blocking first.
