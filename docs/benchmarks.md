# Benchmarks against Go

`tests/bench/` holds eleven Maxon programs; `tests/bench/go/<name>/main.go` holds the Go counterpart of each, which does the
same work (the same sizes, keys and scrambling) and prints the same checksum. Both print the time of each phase of the work
as a `<phase> ns <nanoseconds>` line, and the time of the whole program last as `work ns <nanoseconds>`; the Go programs
share `tests/bench/go/phase` for that. `cmd/bench` builds both for a target, runs them, and writes the comparison as the
Markdown tables below. `bench.ps1` is the other use of the same programs: the size and time of minimaxon's own code
generator, over the runs of one compiler.

## Running it

From the repository's root, with `bin\minimaxon.exe` built (`.\build.ps1` or `.\check.ps1`) and Go on the `PATH`:

```powershell
go run ./cmd/bench run --target=x64-windows                 # builds, runs 7 rounds here, reports; leaves bench-x64-windows.txt
go run ./cmd/bench run --target=x64-linux --rounds=11       # the same through WSL's default distribution
go run ./cmd/bench report bench-x64-windows.txt bench-x64-linux.txt bench-arm64-linux.txt > report.md
```

`bench run` refuses an arm64 target on a machine that is not arm64: under WSL's qemu the timings are the emulator's. For
those, and for any machine the repository is not on, build a batch and run it there:

```powershell
go run ./cmd/bench build --target=arm64-linux --out=M:\agents\minimaxon\in\bench-arm64-linux
```

The batch directory holds `minimaxon/` and `go/` (an executable of each program, static, so nothing is installed on the
other side), `manifest.txt` (the target, the toolchains, each program's expected output), `run.sh` and `run.ps1`. On the
machine that has the hardware, in a Linux VM or container that mounts the directory (for Linux, a POSIX `sh`, `awk`, `date`,
`cp`, `chmod`, `mktemp`, `wc`, `getconf` and, optionally, GNU `/usr/bin/time` for the peak memory, which is `apt install
time` where it is missing):

```sh
cd <batch directory>
sh run.sh            # BENCH_ROUNDS=11 sh run.sh for more rounds; BENCH_RESULTS=<file> to say where the results go
```

`run.sh` copies the executables to a temporary directory (a share may not allow them to execute), runs every program once
with each implementation in a round, the one that goes first alternating from round to round, seven rounds by default, and
writes `../../out/bench-<target>.txt`, relative to the batch directory, which `bench report` reads. `run.ps1` does the same
on Windows. Each run's phase times, exit code, wall time (where `date` has nanoseconds) and peak resident set are in the
file, with `uname -a`, the CPU model, the core count and the toolchains. Copy the file back and run `bench report` on it
with the other targets' to put the targets side by side.
A Linux VM on a Mac gives no CPU model, and its file has none: `bench report --cpu=<name>` names it for the files that lack one.

## What the numbers are

- A time is the best of the rounds, per phase, taken from the program's own clock (`Clock.nowNanos`, `time.Now`), so the
  start of the process and the output are not in it. A ratio is minimaxon's time over Go's: below 1 minimaxon is faster.
- The means are geometric. One is of the eleven whole programs, the other of every phase whose best time is at least a
  millisecond for both: shorter phases are decided by the clocks' granularity (Go's runtime clock on Windows ticks at a
  fraction of a millisecond), and are listed but not counted.
- Peak memory is `/usr/bin/time -v`'s maximum resident set on Linux, the peak working set on Windows; the sizes are of the
  executable files (Go's carries its runtime and the symbol tables).
- A program's output is compared with its `//>` line; a run that failed or printed something else is listed under its table
  and its times mean nothing.

### Where the Go counterpart is not like for like

- Maxon's arithmetic is checked (`try ... otherwise panic`) where Go's wraps; Go has no checked form, so the Go programs
  do the plain operation and Go does less work per step.
- `records-and-unions`: Go has no union, so the shapes are an interface and a type switch, each shape boxed in the interface
  where Maxon's union holds its payload inline in the array.
- `float-calls`: Go's compiler may inline `mix` and devirtualize the call through the function value; the cost of passing
  floats may not be measured at all there.
- `float-dot-and-nbody` and `float-calls`: products are converted to `float64` before they are added, so arm64 does not fuse
  them and every target prints the Maxon checksum; natural Go on arm64 would use fused multiply-adds, a few percent faster.
- `service-messages-to-*`: a service is a goroutine that owns its state and reads a buffered channel of 1,024 messages (the
  mailbox); a reply is a channel of its own. Maxon's services run as behaviours on a cown, scheduled onto every core.
- `string-building`: Go strings are immutable, so the append is a concatenation; `map-operations` uses Go's built-in `map`,
  Maxon's is the library's `Map`.

## Results

The x64 tables are of the commit `1d962f4` with the service changes (turns of messages, per-processor live counts,
batched remote frees) not yet committed, which is why the compiler's row names `1d962f4`, on an AMD Ryzen 7 5800X (8
cores, 16 threads), Windows 11 and WSL2 on the same machine, seven rounds each. A WSL run is on the same hardware under a
Linux kernel, so the difference from the Windows run is the operating system's. Regenerate the tables with `bench run` and `bench report`.

### x64-windows and x64-linux

Ratio is minimaxon's best time over Go's, so below 1 minimaxon is faster. Every time is the best of the rounds.

#### Systems

| target | CPU | cores | OS | rounds | compiler | Go |
|---|---|---|---|---|---|---|
| x64-windows | AMD Ryzen 7 5800X 8-Core Processor | 16 | Microsoft Windows NT 10.0.26300.0 X64 | 7 | minimaxon.exe at git 1d962f4 | go version go1.27.4 windows/amd64 GOOS=windows GOARCH=amd64 |
| x64-linux | AMD Ryzen 7 5800X 8-Core Processor | 16 | Linux Desky 5.15.167.4-microsoft-standard-WSL2 #1 SMP Tue Nov 5 00:21:55 UTC 2024 x86_64 x86_64 x86_64 GNU/Linux | 7 | minimaxon.exe at git 1d962f4 | go version go1.27.4 windows/amd64 GOOS=linux GOARCH=amd64 |

#### Summary

| target | geometric-mean ratio of the programs | of the phases of 1 ms or more | worst phases |
|---|---|---|---|
| x64-windows | 1.02 (11) | 1.09 (29) | map-operations / string iterate 3.62; map-operations / string hit 1.77; map-operations / string miss 1.75 |
| x64-linux | 1.01 (11) | 1.13 (29) | map-operations / string iterate 4.27; map-operations / string miss 1.99; map-operations / string hit 1.92 |

#### array-indexing

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| fill | 3.27 | 0.00 | - | 3.27 | 0.06 | 51.29 |
| rounds | 121.50 | 131.09 | 0.93 | 116.10 | 130.41 | 0.89 |
| work | 124.80 | 131.09 | 0.95 | 119.39 | 130.51 | 0.91 |


#### counted-loop

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| work | 208.17 | 255.22 | 0.82 | 211.99 | 257.04 | 0.82 |


#### float-calls

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| direct | 49.89 | 51.97 | 0.96 | 50.20 | 50.64 | 0.99 |
| through | 50.21 | 50.26 | 1.00 | 51.26 | 50.68 | 1.01 |
| work | 100.13 | 102.23 | 0.98 | 101.50 | 101.56 | 1.00 |


#### float-dot-and-nbody

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| dot | 12.32 | 12.66 | 0.97 | 12.43 | 12.43 | 1.00 |
| nbody | 37.16 | 27.94 | 1.33 | 37.65 | 26.99 | 1.39 |
| work | 49.69 | 40.98 | 1.21 | 50.71 | 39.45 | 1.29 |


#### map-operations

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| int insert | 102.45 | 95.01 | 1.08 | 116.90 | 89.30 | 1.31 |
| int hit | 43.31 | 30.70 | 1.41 | 49.03 | 32.18 | 1.52 |
| int miss | 31.21 | 27.17 | 1.15 | 32.76 | 27.00 | 1.21 |
| int iterate | 6.09 | 7.05 | 0.86 | 6.25 | 7.78 | 0.80 |
| int remove | 71.06 | 79.58 | 0.89 | 71.74 | 73.43 | 0.98 |
| string build | 56.45 | 107.46 | 0.53 | 64.07 | 114.07 | 0.56 |
| string insert | 242.07 | 225.03 | 1.08 | 290.67 | 235.37 | 1.23 |
| string hit | 98.11 | 55.48 | 1.77 | 107.34 | 55.86 | 1.92 |
| string miss | 58.02 | 33.07 | 1.75 | 68.70 | 34.57 | 1.99 |
| string iterate | 29.23 | 8.07 | 3.62 | 35.06 | 8.21 | 4.27 |
| string remove | 120.51 | 107.58 | 1.12 | 143.33 | 111.63 | 1.28 |
| work | 931.40 | 816.93 | 1.14 | 1077.87 | 806.94 | 1.34 |


#### nested-loops

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| work | 41.52 | 36.22 | 1.15 | 41.99 | 35.95 | 1.17 |


#### records-and-unions

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| build | 0.06 | 0.00 | - | 0.04 | 0.02 | 2.12 |
| match | 24.63 | 22.95 | 1.07 | 24.84 | 22.48 | 1.10 |
| work | 24.75 | 22.95 | 1.08 | 24.94 | 22.52 | 1.11 |


#### recursion

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| work | 139.69 | 143.15 | 0.98 | 140.50 | 141.40 | 0.99 |


#### service-messages-to-many-services

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| spawn | 0.10 | 5.96 | 0.02 | 0.25 | 4.75 | 0.05 |
| send | 55.91 | 61.43 | 0.91 | 41.87 | 52.45 | 0.80 |
| await | 0.28 | 0.00 | - | 0.21 | 0.33 | 0.63 |
| work | 56.44 | 68.16 | 0.83 | 42.39 | 57.57 | 0.74 |


#### service-messages-to-one-service

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| work | 43.93 | 38.12 | 1.15 | 26.37 | 31.47 | 0.84 |


#### string-building

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| work | 33.27 | 32.31 | 1.03 | 34.25 | 32.10 | 1.07 |


#### Peak memory and executable size

Peak memory is the largest resident set of the runs, in MiB; size is the file's, in KiB.

| program | x64-windows minimaxon MiB | x64-windows Go MiB | x64-windows minimaxon KiB | x64-windows Go KiB | x64-linux minimaxon MiB | x64-linux Go MiB | x64-linux minimaxon KiB | x64-linux Go KiB |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| array-indexing | 4.2 | 6.3 | 12 | 2464 | 0.6 | 5.7 | 9 | 2316 |
| counted-loop | 3.6 | 5.8 | 10 | 2462 | 0.3 | 5.7 | 9 | 2315 |
| float-calls | 3.6 | 5.9 | 17 | 2469 | 0.3 | 5.7 | 18 | 2323 |
| float-dot-and-nbody | 3.7 | 5.9 | 21 | 2474 | 0.3 | 5.7 | 22 | 2328 |
| map-operations | 153.7 | 163.8 | 51 | 2476 | 158.1 | 160.4 | 49 | 2329 |
| nested-loops | 3.6 | 5.8 | 12 | 2462 | 0.3 | 5.7 | 9 | 2315 |
| records-and-unions | 3.7 | 5.9 | 14 | 2466 | 0.3 | 5.7 | 14 | 2317 |
| recursion | 3.6 | 5.9 | 10 | 2462 | 0.3 | 5.7 | 9 | 2315 |
| service-messages-to-many-services | 5.2 | 33.3 | 40 | 2466 | 2.0 | 24.2 | 37 | 2321 |
| service-messages-to-one-service | 4.3 | 5.9 | 38 | 2464 | 0.5 | 5.7 | 32 | 2317 |
| string-building | 3.6 | 11.5 | 10 | 2462 | 0.3 | 12.3 | 9 | 2316 |


### arm64-linux

An Apple M3 (8 cores) in an OrbStack Linux VM on a Mac, seven rounds, from the batch that `bench build --target=arm64-linux`
made from `1d962f4` with the service changes (turns of messages, per-processor live counts, batched remote frees) not yet
committed, which is why the compiler's row names `1d962f4`. The VM does not tell its CPU, so the table names it with
`bench report --cpu="Apple M3"`.

#### Systems

| target | CPU | cores | OS | rounds | compiler | Go |
|---|---|---|---|---|---|---|
| arm64-linux | Apple M3 | 8 | Linux ubuntu 7.2.12-orbstack-00394-gbecdd6130068 #1 SMP PREEMPT Thu Oct 22 22:17:17 UTC 2026 aarch64 GNU/Linux | 7 | minimaxon.exe at git 1d962f4 | go version go1.27.4 windows/amd64 GOOS=linux GOARCH=arm64 |

#### Summary

| target | geometric-mean ratio of the programs | of the phases of 1 ms or more | worst phases |
|---|---|---|---|
| arm64-linux | 1.07 (11) | 1.16 (29) | map-operations / string iterate 2.06; array-indexing / work 1.98; array-indexing / rounds 1.94 |

#### array-indexing

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| fill | 3.78 | 0.07 | 51.44 |
| rounds | 193.54 | 99.70 | 1.94 |
| work | 197.41 | 99.79 | 1.98 |


#### counted-loop

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| work | 222.65 | 273.04 | 0.82 |


#### float-calls

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| direct | 69.26 | 69.45 | 1.00 |
| through | 108.74 | 69.39 | 1.57 |
| work | 178.01 | 138.90 | 1.28 |


#### float-dot-and-nbody

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| dot | 12.37 | 12.61 | 0.98 |
| nbody | 27.75 | 17.70 | 1.57 |
| work | 40.13 | 30.37 | 1.32 |


#### map-operations

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| int insert | 66.90 | 49.96 | 1.34 |
| int hit | 28.99 | 24.57 | 1.18 |
| int miss | 19.54 | 17.83 | 1.10 |
| int iterate | 5.60 | 6.51 | 0.86 |
| int remove | 39.32 | 48.05 | 0.82 |
| string build | 42.73 | 76.72 | 0.56 |
| string insert | 172.51 | 89.22 | 1.93 |
| string hit | 50.32 | 38.87 | 1.29 |
| string miss | 40.11 | 24.83 | 1.62 |
| string iterate | 13.79 | 6.69 | 2.06 |
| string remove | 72.01 | 68.32 | 1.05 |
| work | 570.70 | 460.99 | 1.24 |


#### nested-loops

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| work | 44.02 | 41.46 | 1.06 |


#### records-and-unions

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| build | 0.02 | 0.02 | 1.02 |
| match | 25.73 | 27.77 | 0.93 |
| work | 25.77 | 27.79 | 0.93 |


#### recursion

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| work | 101.82 | 102.42 | 0.99 |


#### service-messages-to-many-services

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| spawn | 0.04 | 6.19 | 0.01 |
| send | 18.50 | 14.87 | 1.24 |
| await | 0.16 | 0.25 | 0.62 |
| work | 19.67 | 24.72 | 0.80 |


#### service-messages-to-one-service

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| work | 13.68 | 18.74 | 0.73 |


#### string-building

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| work | 28.23 | 26.32 | 1.07 |


#### Peak memory and executable size

Peak memory is the largest resident set of the runs, in MiB; size is the file's, in KiB.

| program | arm64-linux minimaxon MiB | arm64-linux Go MiB | arm64-linux minimaxon KiB | arm64-linux Go KiB |
|---|---:|---:|---:|---:|
| array-indexing | 0.5 | 2.6 | 13 | 2297 |
| counted-loop | 0.4 | 2.4 | 9 | 2297 |
| float-calls | 0.4 | 2.1 | 14 | 2299 |
| float-dot-and-nbody | 0.4 | 2.2 | 18 | 2301 |
| map-operations | 156.4 | 159.1 | 49 | 2300 |
| nested-loops | 0.4 | 2.1 | 9 | 2296 |
| records-and-unions | 0.4 | 2.4 | 13 | 2297 |
| recursion | 0.4 | 2.0 | 9 | 2296 |
| service-messages-to-many-services | 45.8 | 23.1 | 37 | 2297 |
| service-messages-to-one-service | 0.4 | 2.0 | 36 | 2297 |
| string-building | 0.4 | 6.9 | 9 | 2297 |
