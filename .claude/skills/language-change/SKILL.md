---
name: language-change
description: Checklist for changing minimaxon's language — a keyword, syntax, a rule or a diagnostic. Use for any edit to Maxon.g4 or to what programs may say, however small (for example renaming `is` to `implements`).
---

# Changing the language

A language change reaches the grammar, the checker's messages, every program that uses the construct (corpus, Go
test strings, the generator), and the docs. Work through the list; skip a step only when it clearly does not apply.

## 1. Programs first, while the old syntax still lexes

- **A keyword rename:** run the token-aware rewrite before touching the grammar, over every place Maxon source lives
  in files:

  ```powershell
  go build -o minimaxon.exe . ; .\minimaxon.exe rewrite --keyword=old=new tests docs README.md
  ```

  It changes only keyword tokens (not names, labels, comments or string text) and lists lexer-refused files that
  contain the word: fix those by hand. It refuses once `old` is no longer reserved.
- **Other syntax changes:** no tool yet. Edit by hand, or write a throwaway script in the scratchpad. Match on
  declaration shapes, never bare words, and read the diff for prose hits in `.md` files.

## 2. The grammar

- Edit `internal/syntax/Maxon.g4`, then `go generate ./internal/syntax`. Never edit `*_gen.go`.
- `go build ./...` names every visitor that lacks a new node's method.

## 3. Words the compiler says

- Grep `internal/check` (and `internal/resolve`, `internal/ir`) for messages that spell the old syntax as a fix:
  `grep -rn "'[^']*old[^']*'" internal --include=*.go`. Diagnostics name the fix, so they quote syntax often.
- A new rule gets a new code in `internal/diag` and a `tests/errors` test with `at L:C`.

## 4. Maxon inside Go

- Unit tests and the generator (`internal/interp/generator_*_test.go`) hold Maxon in Go strings, often with `\n`
  escapes. Grep for the old form (e.g. `grep -rn " is Shape" internal --include=*_test.go`) and fix them; the Go tests
  catch what grep misses.

## 5. Tests

- Watch a test fail before the change makes it pass (AGENTS.md).
- Run the corpus with position updates, then read what moved:

  ```powershell
  .\minimaxon.exe test --update-positions
  ```

  Only positions move; a test expecting a different code still fails.

## 6. Docs

- `README.md`: the subset, "Where it differs from Maxon", examples, reserved words, commands.
- `docs/regions.md` when the memory model's syntax changes.
- Reread every edited prose line: replacements in English text are the usual mistake.

## 7. Finish

```powershell
go vet ./... ; go fix -diff ./... ; gofmt -l . ; go test ./... ; go build -o minimaxon.exe . ; .\minimaxon.exe test
```

All must be clean and green. Commit only when asked, on `main`.
