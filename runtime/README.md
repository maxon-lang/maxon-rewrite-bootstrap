# The runtime

`self/`'s runtime as Maxon source: one module, compiled with every program that `self/` builds and cached with it. The
design (how a compile finds this directory, the `__Raw` intrinsics, how an entry the compiler calls maps to a function
here, the globals, caching) is the root README's section "The runtime". The Go compiler never compiles this directory;
`minimaxon check runtime` checks it, and `.\check.ps1` runs that.

| File | Entries the compiler calls | Holds |
|---|---|---|
| `layout.maxon` | — | the layouts the compiler's code shares with the runtime: the word, the string box, the empty box |
| `heap.maxon` | `alloc`, `free` | boxes on the process heap, and the count of live ones |
| `output.maxon` | `write`, `panicWith`, `panicIndex`, `checkLeaks` | writing to a handle, panics, the leak report |
| `strings.maxon` | `concat`, `itoa`, `streq`, `stringLength`, `stringByteAt`, `stringSlice` | the string entries |

## Rules a runtime file follows

- **Only the runtime calls `__Raw`.** Anywhere else a call of one is `E3152`.
- **An entry the compiler calls has the table's signature** (`runtimeFunctions` in `internal/builtingen/table.go`):
  words only (`Count`, `Byte`, `bool`, `String` as its box address), no `mutates`, no type parameter, no `throws`.
  `check runtime` refuses a missing one (`E3004`) and a different one (`E3005`). A new entry is a row of `entries` and
  of `runtimeFunctions`, then `go generate ./internal/ast`, then its function here.
- **The same code for every build.** The runtime reads no build option: what an option changes is the entry stub's
  (`self/backend/entry.maxon`), as `--count-allocations` makes the stub call `checkLeaks`.
- **No primitives and no throwing.** The runtime implements `Array`, `Map` and the rest, so it uses none of them; a
  failure it cannot pass on is a `panic`, whose message is a literal, or `exitProcess`.
- **A layout is a named constant of `layout.maxon`**, never a number in the code that reads the box.
- **Behaviour is the Go runtime's** (`internal/backend/runtime.go` and its siblings): the same messages, exit codes and
  Win32 calls, until a change is recorded in the root README's "Where it differs from Maxon".
- **A DLL function is called by its name**, a string literal, which must be in the executable's import list
  (`rtImports` in `self/backend/entry.maxon`); `self/backend` panics on one that is not.
