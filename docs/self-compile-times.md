# Compile time of `self/`

An uncached `minimaxon build self --timings` with the Go compiler, best of several runs, one row per stage
(`.\selftime.ps1 -Note <stage>`). Total is wall time; check and generate are task time summed over the modules.

| Date | Commit | Stage | `self/` lines | Total ms | Check ms | Generate ms |
|---|---|---|---:|---:|---:|---:|
| 2026-11-21 | ca214e8 | Before Stage 1.2a | 8708 | 17.8 | 15.8 | 13.7 |
| 2026-11-21 | fa6af54 | 1.2a syntax realignment | 8718 | 15.2 | 11.0 | 11.0 |
| 2026-11-21 | 1e781fd | 1.2a diagnostics table | 8806 | 17.3 | 11.5 | 11.9 |
| 2026-11-21 | 6476c3c | 1.2a built-ins generator | 8948 | 15.6 | 15.1 | 12.5 |
| 2026-11-21 | 90ecf9f | 1.2a modules | 9021 | 16.1 | 16.0 | 9.0 |
| 2026-11-21 | 35d0ed5 | 1.2a errors (throws) | 9052 | 16.6 | 16.3 | 12.2 |
