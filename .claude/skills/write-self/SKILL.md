---
name: write-self
description: How to write and extend the compiler in self/ (diag, table, ast, syntax, project, resolve, check, ir, backend, interp, fuzz, driver) and runtime/ in the Maxon subset minimaxon compiles. Use when adding to or changing any part of self/ or runtime/, or when fixing self/ code that the compiler refuses.
---

# Writing `self/`

`self/` is minimaxon: the compiler, written in Maxon and compiled by itself from the seed (`seed/README.md`). It
grows from the subset it implements into the full Maxon compiler. `runtime/` is its runtime, Maxon source compiled
with every program.

## 1. Before writing: does maxon already have it?

- Look in `c:\Users\Eric\Dev\maxon\maxon-bin\` and `maxon\stdlib\` for code doing the same job: the PE writer
  (`maxon-bin/Compiler/Targets/Windows/PeWriter.maxon`), the x64 tables (`maxon-bin/Compiler/Targets/X64/`), the CRT
  command-line splitter (`Compiler/Runtime/CommandLineRuntime.maxon`), UTF-8 and UTF-16 conversion
  (`Compiler/Runtime/HostTextRuntime.maxon`), the error messages (`Compiler/ErrorCodeRegistry.maxon`), and stdlib
  files (`stdlib/Map.maxon` in place of `self/table/`).
- If it fits `self/`'s architecture, convert it with `cmd/convert` (skill `convert-from-maxon`). Converted files are
  regenerated, never hand-edited.
- If it does not fit, or needs a feature the compiler lacks, it is still the reference: follow its behaviour, edge
  cases and names unless README "Where it differs from Maxon" records a deliberate change.
- Search maxon's big files (`Parser*.maxon` ~600 KB, `docs/LANGUAGE_REFERENCE.md` ~420 KB) with Grep and line ranges.

## 2. The patterns of the subset

Follow what `self/` does now; the README sections "The subset", "Ownership", "Generics", "Primitives" and "Where it
differs from Maxon" are the rules.

| Need | In `self/` |
|---|---|
| a record | `export type` with `export var` fields and a `static function make(...)` or `new()` returning `Self{...}` (`self/ast/tree.maxon`) |
| a list | `Array with T`, named by a typealias (`typealias Counts = Array with Count`); flat records indexed by handles, as `ast.Tree`'s 16-byte `Record` |
| a map | `table.StringMap with V` / `table.IntMap with V` (`self/table/map.maxon`), until `stdlib/Map.maxon` converts. Never the built-in `map`, `list` or `pool`, nor `frozen`/`cown`/`when`: they have no native code (`E9900`) |
| a closed set of kinds | a `union` with payload cases and methods that `match self` (`ast.Decl` in `self/ast/decls.maxon`), or an `enum`; code over every member matches on every one, or ends an `if` chain in `panic` (`self/resolve/resolve.maxon`) |
| a reference to a shared thing | a handle: `ast.FuncID`, `ast.TypeID`, `ast.NodeID`, `diag.ModuleID`, an index into a table the owner holds |
| a compile error | append to a `mutates errs diag.ErrorList` with `diag.pushSiteError` (or `check.checkFail`) and return |
| a result that can fail | a result union (`LexResult`, `ParseResult`: `ok(...)` / `err(...)`) or `table.Option with T` (`self/table/option.maxon`: nothing is built in), taken with `match consumes` |
| a broken invariant | `panic("what broke")`, also as `try ... otherwise panic("...")` on a cast that cannot fail |
| an integer of a width | typealiased ranges (`diag.U32`, `ast.U16`); arithmetic that can fail takes `try`, `try x as T otherwise ...` |

Programs use maxon's built-in names (`Array`, `Map`, `push`, `append(other)`, `contains`, `clone()`, `ByteArray`,
`File.readText(path)` with `FilePath`, `Subprocess.run`), built-ins that fail throw (`try a.pop() otherwise ...`), error
enums and `throws` exist (README "Errors of the program"), there is no built-in `Option` (`table.Option`), and another
module's names are always qualified (`ast.Tree`; `minimaxon qualify self` writes missing qualifiers). `minimaxon
rename` rewrites built-in names by what the checker resolved. New code follows the error style of the code around it
(result unions in the front end).

## 3. Ownership that compiles

- One owner per value. Shared state is passed down, never held: `mutates p ast.Project, mutates errs diag.ErrorList`.
- Store a value with `consumes` and pass a fresh value or `.copy()`; strings and records into constructors are
  `consumes` parameters.
- Take an owned value out of a field or element with `exchange(place, value: fresh)` (`check.checkerStartMod`), out
  of a union or `Option` with `match consumes`.
- A `consumes` parameter is not mutable: `var mc = c` first.
- `a[i]` borrows: bind a scalar freely (`let n = a[i]`), `.copy()` anything else, assign through `a[i] = v`.
- Exclusivity (`E3186`): an argument passed `mutates`/`consumes`/`sets` may not overlap another argument, and
  `a[i]` overlaps `a[j]`. Read the index or the scalar into a local before the call.
- A borrowed place is held while later operands are evaluated: compute the call that changes it into a local first.
- `sets` for out-parameters; `var x as T` for a value assigned later.
- Every block carries a label (`if c 'name'` ... `end 'name'`); labelled arguments after the first.

## 4. Rules of the design

- **The runtime is Maxon source** in `runtime/`, compiled with every program. The intrinsics (raw load and store,
  address arithmetic, DLL import calls) are legal only there. Only drop and copy routines and the entry stub are
  compiler-emitted. `runtime/README.md` has its rules.
- **Collections over layout descriptors:** runtime arrays, maps and lists take a descriptor of the element's size and
  layout; only thin typed wrappers are per type.
- **One instance per project:** a project-wide instance table keyed by type arguments, claimed once; the claimer
  lowers it, others reference its symbol. Drop and copy routines are made the same way during generate.
- **Generated parts come from the generators, never by hand:** the parser, tokens and nodes from
  `self/syntax/Maxon.g4`; the diagnostics from `self/diag/codes.tsv` and `messages.tsv`; the built-ins, IR records and
  printer, x64 encoder and ABI layouts from the tables in `internal/builtingen`, `irgen`, `x64gen` and `abigen`. Run
  `go generate ./internal/...` after an edit. A generator that is missing is written first, as plain Go, with a test
  that regenerates and diffs.
- **The seed must compile `self/` and `runtime/`.** A language feature reaches them only after `.\seed.ps1` has
  renewed the seed with it (`seed/README.md`), so the feature is written first without using it.

## 5. Testing

- `.\build.ps1` builds `bin\minimaxon.exe` from the seed; `.\bin\minimaxon.exe check self`, `.\bin\minimaxon.exe selftest` and
  `.\bin\minimaxon.exe fmt --check self runtime` are the quick checks, and `.\bin\minimaxon.exe fmt self` fixes the layout.
- A test of a part is a `test 'name'` declaration in a `.maxtest` file of its module's directory, run by
  `.\bin\minimaxon.exe test self/<module>` (all of them by `selftest`); add one only when a bug would come back or nothing
  in the corpus covers the feature. A behaviour is a `tests/run/` program, a diagnostic a
  `tests/errors/` program.
- Before a commit, `.\check.ps1` passes. Do not commit unless asked; then directly on `main`.

## Speed: what the first profile found

`bin\minimaxon.exe check self` took 134 s until three things were fixed (now 1.3 s). Profile before guessing:
`.\bin\minimaxon.exe build self -o bin\prof.exe --emit-map`, then `go run ./cmd/sample bin\prof.exe check self`.

- Never write `try ... otherwise <fallback>` inside a hash or mixing function: the fallback becomes the hash of nearly
  every key. Keep the operands 32-bit so a product cannot overflow, as maxon's stdlib does.
- A fact every node has belongs in an array indexed by the node; keep `IntMap` and `StringMap` for sparse keys.
- Never copy a record out of a table to read one field: read through the element, or add an accessor that reads in
  place (`ast.declName`, `ast.declLevel`). `IntMap.get` copies its value, so keep large values out of hot lookups.
- Every record value is a heap box in the compiled code, so a copy allocates. Pass by `mutates` or read in place on hot
  paths.
- The backend writes the common case of array, byte-array and string length, element read and write, and push at the
  end in place (`backend.emitCallRuntime`); the runtime entry is only the slow path (panic, growth, insert in
  the middle). Keep those entries' behaviour and the inline code in step, and index or `count()` freely.
- Every other runtime call, and every `alloc`/`free` (a kernel32 heap call), is a real cost: after the second profile
  they are about 40% of a build. Never build an array or a list by pushing one element at a time where `clone()` or a
  read in place does (`ast.cloneTypeArgs`, `ast.typeArgAt`): that one fix was 10% of `build self`.
