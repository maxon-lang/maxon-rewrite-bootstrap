---
name: language-change
description: Checklist for changing minimaxon's language — a keyword, syntax, layout, a rule or a diagnostic. Use for any edit to Maxon.g4 or to what programs may say, however small (for example renaming `is` to `implements`).
---

# Changing the language

A language change reaches the grammar, the checker's messages, every program that uses the construct (corpus, unit
test `testdata`, the generator), and the docs. Work through the list; skip a step only when it clearly does not apply.

## 1. Programs first, while the old syntax still lexes

- **A keyword rename:** run the token-aware rewrite before touching the grammar. It covers the corpus, the unit tests'
  `testdata` files, and the code spans and untagged or `maxon` blocks of the docs:

  ```powershell
  go build -o minimaxon.exe . ; .\minimaxon.exe rewrite --keyword=old=new tests internal docs README.md
  ```

  It changes only keyword tokens (not names, labels, comments, string text or Markdown prose) and names what it could
  not lex but that contains the word: fix those by hand. It refuses once `old` is no longer reserved.
- **A layout change** (spaces, joins, indentation, blank lines, comments): change the rule first (`Maxon.g4`'s
  `noSpaceBefore`/`^`, or `gapBefore`, `lineFault` and `endFault` in `internal/syntax/parser.go`), then let the formatter
  rewrite every program in the new layout, and check:

  ```powershell
  go build -o minimaxon.exe . ; .\minimaxon.exe fmt tests internal docs README.md ; .\minimaxon.exe fmt --check tests internal docs README.md
  ```

  `fmt` reads the old layout through the lenient lexer, so a rule the lexer enforces (`E2001`) may need its lenient case
  too; a rule only the parser enforces (`E2010`) is formatted by the same function that checks it. Over a directory it
  skips files that contain `// expect-error: E2001` or `E2010`, the tests of the layout rules: update those by hand to
  break the new rule. A file it cannot parse is listed and left alone.
- **Other syntax changes:** no tool yet. Edit by hand, or write a throwaway script in the scratchpad. Match on
  declaration shapes, never bare words, and read the diff for prose hits in `.md` files.

## 2. The grammar

- Edit `internal/syntax/Maxon.g4`, then `go generate ./internal/syntax`. Never edit `*_gen.go`.
- `go build ./...` names every visitor that lacks a new node's method.

## 3. Words the compiler says

- Grep `internal/check` (and `internal/resolve`, `internal/ir`) for messages that spell the old syntax as a fix:
  `grep -rn "'[^']*old[^']*'" internal --include=*.go`. Diagnostics name the fix, so they quote syntax often.
- Go identifiers named after a keyword (`ConvMutable`): rename them too, so the code reads like the language.
- A new rule gets a new code in `internal/diag` and a `tests/errors` test with `at L:C`.

## 4. Maxon built in Go

- The program generator (`internal/interp/generator_*_test.go`) builds programs in Go; it spells each keyword through
  one named constant, so a rename edits that constant.
- Anything else that still builds Maxon in Go strings: grep for the old form; the Go tests catch what grep misses.

## 5. Tests

- Watch a test fail before the change makes it pass (AGENTS.md).
- Test files named after the old syntax (`inout-argument-is-a-let.maxon`) are renamed with `git mv`.

## 6. Docs

- `README.md`: the subset, layout, "Where it differs from Maxon", examples, reserved words, commands.
- `docs/regions.md` when the memory model's syntax changes.
- Reread every edited prose line: articles ("a `mutable`", not "an `mutable`") and English uses of the word are the
  usual mistakes.

## 7. Finish

```powershell
.\check.ps1 --update-positions
```

It formats and applies `go fix`, then runs vet, the unit tests, the build and the corpus, stopping at the first
failure. `--update-positions` moves `expect-error` positions that a change of line length shifted; read what moved.
Commit only when asked, on `main`.
