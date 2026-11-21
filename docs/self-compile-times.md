# Compile time of `self/`

One uncached build of `self/` per row (`.\selftime.ps1 -Note <stage>`). Total is the Go compiler's wall time
(`minimaxon build self --timings`); check and generate are its task time summed over the modules. C1 is `self/`
built by the Go compiler and C2 is `self/` built by C1; their columns are the wall time of each building `self/`.

| Date | Commit | Stage | `self/` lines | Total ms | Check ms | Generate ms | C1 ms | C2 ms |
|---|---|---|---:|---:|---:|---:|---:|---:|
| 2026-11-21 | ca214e8 | Before Stage 1.2a | 8708 | 17.8 | 15.8 | 13.7 | — | — |
| 2026-11-21 | fa6af54 | 1.2a syntax realignment | 8718 | 15.2 | 11.0 | 11.0 | — | — |
| 2026-11-21 | 1e781fd | 1.2a diagnostics table | 8806 | 17.3 | 11.5 | 11.9 | — | — |
| 2026-11-21 | 6476c3c | 1.2a built-ins generator | 8948 | 15.6 | 15.1 | 12.5 | — | — |
| 2026-11-21 | 90ecf9f | 1.2a modules | 9021 | 16.1 | 16.0 | 9.0 | — | — |
| 2026-11-21 | 35d0ed5 | 1.2a errors (throws) | 9052 | 16.6 | 16.3 | 12.2 | — | — |
| 2026-11-21 | 46d0574 | 1.2a scheduled front | 9052 | 15.9 | 12.0 | 10.6 | — | — |
| 2026-11-21 | 588462b | 1.2a maxon message text | 9353 | 17.6 | 10.0 | 12.1 | — | — |
| 2026-11-21 | 0a3c110 | 1.2a collection built-ins | 9389 | 16.6 | 12.1 | 11.2 | — | — |
| 2026-11-21 | 26ce7e8 | 1.2a byte arrays | 9394 | 18.7 | 13.6 | 13.8 | — | — |
| 2026-11-21 | 64a224d | 1.2a OS built-ins | 9602 | 17.1 | 10.9 | 13.2 | — | — |
| 2026-11-21 | 8a4afe2 | 1.4 self command line | 9740 | 17.8 | 12.6 | 14.2 | — | — |
| 2026-11-21 | e8db56c | 1.4 PE writer | 10099 | 20.9 | 16.9 | 16.1 | — | — |
| 2026-11-21 | 3423b29 | 1.4 x64 encoder generator | 11044 | 28.8 | 20.5 | 19.8 | — | — |
| 2026-11-21 | 6287bbd | 1.4 IR generator, declare passes | 15876 | 20.7 | 16.9 | 21.4 | — | — |
| 2026-11-21 | d818d1e | 1.3 generated parser | 16874 | 21.0 | 18.6 | 23.0 | — | — |
| 2026-11-21 | 1ff3eb5 | 1.4 codegen and linker | 19488 | 20.7 | 20.5 | 25.7 | — | — |
| 2026-11-21 | c15cbee | 1.4 IR lowering | 24558 | 25.3 | 29.0 | 29.0 | — | — |
| 2026-11-21 | ca244dc | 1.4 bodies check | 26939 | 29.5 | 28.7 | 32.0 | — | — |
| 2026-11-21 | dce0db3 | 1.4 runtime core, behaviours | 28588 | 34.2 | 36.2 | 31.3 | — | — |
| 2026-11-21 | b394ea3 | 1.4 ownership pass | 30889 | 46.1 | 42.2 | 39.8 | — | — |
| 2026-11-21 | 181cfdf | 1.4 runtime arrays, layout table | 30889 | 39.3 | 38.6 | 30.6 | — | — |
| 2026-11-21 | 655806e | 1.4 range analysis, runtime OS | 32961 | 53.9 | 45.8 | 39.6 | — | — |
| 2026-11-21 | 054227e | 1.5 self-compile works | 33606 | 48.2 | 38.8 | 37.7 | 2032 | 4471 |
| 2026-11-21 | 7260842 | 1.5 M1 gate: c1 and c2 test pass | 34043 | 131.0 | 120.0 | 221.6 | 3896 | 7034 |
| 2026-11-21 | 5b316e8 | 1.6 interpreter, 1.7 spec runner | 41083 | 49.7 | 53.4 | 45.5 | 2387 | 5719 |
