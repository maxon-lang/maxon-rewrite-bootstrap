# Compile time of `self/`

One uncached `minimaxon build self --timings` with the Go compiler, one row per stage
(`.\selftime.ps1 -Note <stage>`). Total is wall time; check and generate are task time summed over the modules.

| Date | Commit | Stage | `self/` lines | Total ms | Check ms | Generate ms |
|---|---|---|---:|---:|---:|---:|
| 2026-11-21 | ca214e8 | Before Stage 1.2a | 8708 | 17.8 | 15.8 | 13.7 |
| 2026-11-21 | fa6af54 | 1.2a syntax realignment | 8718 | 15.2 | 11.0 | 11.0 |
| 2026-11-21 | 1e781fd | 1.2a diagnostics table | 8806 | 17.3 | 11.5 | 11.9 |
| 2026-11-21 | 6476c3c | 1.2a built-ins generator | 8948 | 15.6 | 15.1 | 12.5 |
| 2026-11-21 | 90ecf9f | 1.2a modules | 9021 | 16.1 | 16.0 | 9.0 |
| 2026-11-21 | 35d0ed5 | 1.2a errors (throws) | 9052 | 16.6 | 16.3 | 12.2 |
| 2026-11-21 | 46d0574 | 1.2a scheduled front | 9052 | 15.9 | 12.0 | 10.6 |
| 2026-11-21 | 588462b | 1.2a maxon message text | 9353 | 17.6 | 10.0 | 12.1 |
| 2026-11-21 | 0a3c110 | 1.2a collection built-ins | 9389 | 16.6 | 12.1 | 11.2 |
| 2026-11-21 | 26ce7e8 | 1.2a byte arrays | 9394 | 18.7 | 13.6 | 13.8 |
| 2026-11-21 | 64a224d | 1.2a OS built-ins | 9602 | 17.1 | 10.9 | 13.2 |
| 2026-11-21 | 8a4afe2 | 1.4 self command line | 9740 | 17.8 | 12.6 | 14.2 |
| 2026-11-21 | e8db56c | 1.4 PE writer | 10099 | 20.9 | 16.9 | 16.1 |
| 2026-11-21 | 3423b29 | 1.4 x64 encoder generator | 11044 | 28.8 | 20.5 | 19.8 |
| 2026-11-21 | 6287bbd | 1.4 IR generator, declare passes | 15876 | 20.7 | 16.9 | 21.4 |
| 2026-11-21 | d818d1e | 1.3 generated parser | 16874 | 21.0 | 18.6 | 23.0 |
| 2026-11-21 | 1ff3eb5 | 1.4 codegen and linker | 19488 | 20.7 | 20.5 | 25.7 |
