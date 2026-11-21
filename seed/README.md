# seed

`minimaxon.exe` here is the compiler that every build of minimaxon starts from: the self-hosted compiler, `self/`,
compiled by itself. Nothing else in the repository compiles Maxon, so `.\build.ps1` and `.\check.ps1` both begin by
having the seed build `self/`.

It was built from the commit that added it (whose parent is `ecd1797`), the commit at which the Go compiler was
removed: the Go compiler built `self/` into C1, C1 built C2, C2 built a C3 byte-identical to it, and C2 passed the
corpus and became this file.

## Renewing it

The seed only has to compile `self/` and `runtime/`, so it is renewed only when they are to use a language feature it
does not compile. Land the feature in `self/` first, written without using it, then run

```powershell
.\seed.ps1
```

which has the seed build `bin\c1.exe`, C1 build `bin\c2.exe` and C2 build `bin\c3.exe`, refuses unless C2 and C3 are
identical and C2 passes `test`, and then copies C2 over `seed\minimaxon.exe`. Commit the new seed on its own, saying in
the message what it can now compile; `self/` and `runtime/` may use the feature from the next commit on.
