# Compile time of `self/`

One build of `self/` per row (`.\selftime.ps1 -Note <stage>`).

## Until 2026-11-21: built by the Go compiler

Total is the Go compiler's wall time (`minimaxon build self --timings`, uncached); check and generate are its task time
summed over the modules. C1 is `self/` built by the Go compiler and C2 is `self/` built by C1; their columns are the
wall time of each building `self/`. The Go compiler was removed on 2026-11-21, and its columns end with it.

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

## From 2026-11-21: built from the seed

Seed is the wall time of `seed\minimaxon.exe` building `self/` into C1, C1 that of C1 building it into C2, and C2 that
of C2 building it again, each without the build cache. `.text` bytes is the size of C2's code, read from its PE section
header (`textsize.ps1`); rows before Phase 3 have none. Peak MB and Commit MB are of C2's build: the most physical
memory the compiler held at once (its peak working set) and the most it had committed; rows before 2026-11-22 have
neither.

| Date | Commit | Stage | `self/` lines | Seed ms | C1 ms | C2 ms | `.text` bytes | Peak MB | Commit MB |
|---|---|---|---:|---:|---:|---:|---:|---:|---:|
| 2026-11-21 | 750979c | cutover: seed, Go compiler removed | 49378 | 7723 | 7850 | 7355 | — |
| 2026-11-21 | 8cbb12d | fix command; specs at 2,251 | 50660 | 7972 | 7113 | 7132 | — |
| 2026-11-21 | c840d38 | formatter selftests | 50743 | 7810 | 7145 | 7422 | — |
| 2026-11-21 | 0272a11 | operator chains walked with a loop | 50933 | 7772 | 7497 | 7533 | — |
| 2026-11-21 | e056ae6 | inline collection fast paths | 51031 | 8067 | 7717 | 3436 | — |
| 2026-11-21 | c38fecb | return converts a proved integer | 51113 | 8140 | 7747 | 3664 | — |
| 2026-11-21 | ce3da3f | fix specs skips differing codes | 51232 | 8307 | 7652 | 3493 | — |
| 2026-11-21 | 8041219 | Phase 2: literals, try handler, for over a collection | 52220 | 7730 | 8527 | 4098 | — |
| 2026-11-21 | 58013ee | Phase 2: typealiases in types, built-ins, conditional try | 54696 | 8704 | 8466 | 4214 | — |
| 2026-11-21 | bd64be8 | Phase 2: message texts, Type from value | 55242 | 9102 | 8231 | 4247 | — |
| 2026-11-21 | fa4b30a | seed renewed; closures; one-compiler gate | 56765 | 4217 | 4086 | 4014 | — |
| 2026-11-21 | 98aae37 | Phase 2: closures, floats, tuples, extensions, interfaces, raw values | 64354 | 5114 | 5546 | 5500 | — |
| 2026-11-21 | a65524a | Phase 2: third batch (extensions, sibling calls, project model, fixed arrays, directives) | 66926 | 5506 | — | — | — |
| 2026-11-21 | 6f9999f | Phase 2: fourth batch (interface values, extensions, casts, frame box), seed 6f9999f | 69471 | 5723 | 5843 | 5785 | — |
| 2026-11-21 | 3f5b380 | Phase 3 T1: measurement tools | 69860 | 6804 | 6431 | 6017 | 6443248 |
| 2026-11-21 | 30d6d11 | Phase 3: new allocator on every build, constants in data, structural hash handles, stage signatures part 1 (machine shared with two agents) | 89248 | 8422 | 26111 | 7639 | 3631833 |
| 2026-11-22 | e81e761 | Phase 3: parallel executor and runners, register convention, inlining, element ops, CSE/LICM, selection quality, division by constant, xmm class | 101499 | 2065 | 2044 | 2043 | 2588568 |
| 2026-11-22 | 3fcdf6d | Phase 3 done: build cache, x64-linux, per-thread heap; first row with memory (two agents building alongside) | 103039 | 1480 | 1401 | 1408 | 2611481 | 425.8 | 441.4 |
| 2026-11-22 | 87cc499 | Phase 4: stdlib loader, line-fit code placement, fallback IR on demand (three agents building alongside) | 103870 | 2729 | 1708 | 1411 | 2811949 | 387.9 | 403.5 |
| 2026-11-22 | 84f5f83 | Phase 4: green-thread runtime, async/await, borrowed iterators, interface bounds (one agent building alongside) | 107696 | 2694 | 2593 | 2610 | 2922605 | 390.9 | 402.8 |
| 2026-11-22 | 36944b1 | native Map, neutral backend, behaviours without stacks | 110696 | 1913 | 1882 | 1843 | 3479085 | 440.4 | 453.3 |
| 2026-11-22 | 2c02f3b | arm64 targets, swiss Map, service batching, String intrinsics | 124783 | 2213 | 2216 | 2254 | 4082349 | 482.2 | 500.6 |
| 2026-11-22 | 4997b01 | panic line lookup and ownership borrow cache | 124903 | 2292 | 1799 | 1962 | 4088746 | 470.2 | 497.2 |
| 2026-11-23 | d87df6e | two-word String (stage 1), lower split from inline | 126630 | 2297 | 1495 | 1469 | 4630314 | 527.2 | 554.7 |
