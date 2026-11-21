# seed

`minimaxon.exe` here is the compiler that every build of minimaxon starts from: the self-hosted compiler, `self/`,
compiled by itself. Nothing else in the repository compiles Maxon, so `.\build.ps1` and `.\check.ps1` both begin by
having the seed build `self/`; the gate builds one compiler that way and tests it, so the seed's code generator is the
one the gate tests.

It was built from the commit that added it (whose parent is `ecd1797`), the commit at which the Go compiler was
removed: the Go compiler built `self/` into C1, C1 built C2, C2 built a C3 byte-identical to it, and C2 passed the
corpus and became this file.

## Renewing it

The seed has to compile `self/` and `runtime/`, so it is renewed when they are to use a language feature it does not
compile. It is also renewed after a batch of features lands, so that the compiler the gate tests is not far behind
today's code generator. For a language feature, land the feature in `self/` first, written without using it, then run

```powershell
.\seed.ps1
```

which has the seed build `bin\c1.exe`, C1 build `bin\c2.exe` and C2 build `bin\c3.exe`, refuses unless C2 and C3 are
identical (the gate never builds a C3: this is the only check that a compiler reproduces itself, so a compiler that
does not is never committed) and C2 passes the suites the gate runs (`suites.ps1`), and then copies C2 over
`seed\minimaxon.exe`. Commit the new seed on its own, saying in
the message what it can now compile; `self/` and `runtime/` may use the feature from the next commit on.
