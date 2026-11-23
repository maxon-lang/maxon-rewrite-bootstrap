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

Compiler and tables are the commit `b1877d1` plus this kit, on an AMD Ryzen 7 5800X (8 cores, 16 threads), Windows 11 and
WSL2 on the same machine, seven rounds each. A WSL run is on the same hardware under a Linux kernel, so the difference from
the Windows run is the operating system's. Regenerate the tables with `bench run` and `bench report`.

### x64-windows and x64-linux

Ratio is minimaxon's best time over Go's, so below 1 minimaxon is faster. Every time is the best of the rounds.

#### Systems

| target | CPU | cores | OS | rounds | compiler | Go |
|---|---|---|---|---|---|---|
| x64-windows | AMD Ryzen 7 5800X 8-Core Processor | 16 | Microsoft Windows NT 10.0.26300.0 X64 | 7 | minimaxon.exe at git b1877d1 | go version go1.27.4 windows/amd64 GOOS=windows GOARCH=amd64 |
| x64-linux | AMD Ryzen 7 5800X 8-Core Processor | 16 | Linux Desky 5.15.167.4-microsoft-standard-WSL2 #1 SMP Tue Nov 5 00:21:55 UTC 2024 x86_64 x86_64 x86_64 GNU/Linux | 7 | minimaxon.exe at git b1877d1 | go version go1.27.4 windows/amd64 GOOS=linux GOARCH=amd64 |

#### Summary

| target | geometric-mean ratio of the programs | of the phases of 1 ms or more | worst phases |
|---|---|---|---|
| x64-windows | 1.19 (11) | 1.23 (29) | map-operations / string iterate 2.81; service-messages-to-one-service / work 2.75; map-operations / string miss 1.88 |
| x64-linux | 1.18 (11) | 1.32 (29) | map-operations / string iterate 6.26; map-operations / string miss 2.78; service-messages-to-one-service / work 2.64 |

#### array-indexing

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| fill | 8.12 | 0.00 | - | 3.77 | 0.12 | 32.69 |
| rounds | 415.67 | 275.99 | 1.51 | 336.16 | 225.18 | 1.49 |
| work | 423.83 | 277.00 | 1.53 | 339.95 | 225.35 | 1.51 |


#### counted-loop

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| work | 228.87 | 282.79 | 0.81 | 240.44 | 284.87 | 0.84 |


#### float-calls

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| direct | 55.47 | 56.75 | 0.98 | 57.15 | 58.46 | 0.98 |
| through | 65.54 | 55.14 | 1.19 | 71.77 | 56.18 | 1.28 |
| work | 125.71 | 111.89 | 1.12 | 130.17 | 114.67 | 1.14 |


#### float-dot-and-nbody

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| dot | 18.21 | 21.90 | 0.83 | 18.47 | 14.28 | 1.29 |
| nbody | 72.54 | 56.22 | 1.29 | 70.88 | 52.19 | 1.36 |
| work | 90.78 | 78.12 | 1.16 | 89.99 | 66.57 | 1.35 |


#### map-operations

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| int insert | 173.10 | 148.64 | 1.16 | 180.95 | 121.16 | 1.49 |
| int hit | 80.64 | 49.09 | 1.64 | 94.04 | 55.61 | 1.69 |
| int miss | 66.12 | 41.92 | 1.58 | 56.27 | 42.33 | 1.33 |
| int iterate | 13.44 | 15.67 | 0.86 | 11.39 | 14.63 | 0.78 |
| int remove | 101.23 | 98.72 | 1.03 | 110.68 | 86.74 | 1.28 |
| string build | 134.13 | 250.23 | 0.54 | 137.72 | 331.21 | 0.42 |
| string insert | 431.81 | 277.82 | 1.55 | 512.98 | 352.40 | 1.46 |
| string hit | 148.33 | 96.58 | 1.54 | 157.79 | 94.49 | 1.67 |
| string miss | 133.35 | 71.06 | 1.88 | 149.48 | 53.75 | 2.78 |
| string iterate | 46.76 | 16.65 | 2.81 | 60.17 | 9.61 | 6.26 |
| string remove | 173.86 | 123.06 | 1.41 | 197.14 | 125.98 | 1.56 |
| work | 1633.54 | 1252.77 | 1.30 | 1800.14 | 1388.37 | 1.30 |


#### nested-loops

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| work | 72.83 | 61.38 | 1.19 | 59.25 | 52.86 | 1.12 |


#### records-and-unions

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| build | 0.10 | 0.00 | - | 0.07 | 0.03 | 2.20 |
| match | 37.09 | 50.82 | 0.73 | 32.95 | 43.26 | 0.76 |
| work | 37.26 | 50.82 | 0.73 | 33.11 | 43.33 | 0.76 |


#### recursion

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| work | 255.00 | 257.22 | 0.99 | 231.17 | 256.47 | 0.90 |


#### service-messages-to-many-services

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| spawn | 0.20 | 12.30 | 0.02 | 0.15 | 17.59 | 0.01 |
| send | 93.65 | 52.37 | 1.79 | 120.99 | 56.83 | 2.13 |
| await | 0.61 | 0.00 | - | 0.57 | 0.53 | 1.08 |
| work | 94.49 | 66.23 | 1.43 | 134.59 | 104.82 | 1.28 |


#### service-messages-to-one-service

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| work | 176.72 | 64.27 | 2.75 | 150.41 | 57.06 | 2.64 |


#### string-building

| phase | x64-windows minimaxon ms | x64-windows Go ms | x64-windows ratio | x64-linux minimaxon ms | x64-linux Go ms | x64-linux ratio |
|---|---:|---:|---:|---:|---:|---:|
| work | 59.19 | 62.26 | 0.95 | 57.49 | 63.82 | 0.90 |


#### Peak memory and executable size

Peak memory is the largest resident set of the runs, in MiB; size is the file's, in KiB.

| program | x64-windows minimaxon MiB | x64-windows Go MiB | x64-windows minimaxon KiB | x64-windows Go KiB | x64-linux minimaxon MiB | x64-linux Go MiB | x64-linux minimaxon KiB | x64-linux Go KiB |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| array-indexing | 4.2 | 6.3 | 12 | 2464 | 0.6 | 5.7 | 9 | 2316 |
| counted-loop | 3.6 | 5.9 | 10 | 2462 | 0.3 | 5.7 | 9 | 2315 |
| float-calls | 3.6 | 5.9 | 16 | 2469 | 0.3 | 5.7 | 14 | 2323 |
| float-dot-and-nbody | 3.7 | 5.9 | 20 | 2474 | 0.3 | 5.7 | 18 | 2328 |
| map-operations | 153.7 | 163.4 | 50 | 2476 | 158.1 | 168.1 | 45 | 2329 |
| nested-loops | 3.6 | 5.9 | 12 | 2462 | 0.3 | 5.7 | 9 | 2315 |
| records-and-unions | 3.7 | 6.0 | 13 | 2466 | 0.3 | 5.7 | 14 | 2317 |
| recursion | 3.6 | 5.9 | 10 | 2462 | 0.3 | 5.7 | 9 | 2315 |
| service-messages-to-many-services | 19.0 | 33.5 | 39 | 2466 | 42.9 | 26.3 | 37 | 2321 |
| service-messages-to-one-service | 4.6 | 5.9 | 36 | 2464 | 0.9 | 5.7 | 32 | 2317 |
| string-building | 3.6 | 11.4 | 10 | 2462 | 0.3 | 10.1 | 9 | 2316 |

### arm64-linux

An Apple M3 (8 cores) in an OrbStack Linux VM on a Mac, seven rounds, from the batch that `bench build --target=arm64-linux`
made at the commit `b1877d1` plus this kit. The VM does not tell its CPU, so the table names it with `bench report --cpu="Apple M3"`.
Two things in it are not the compiler's present state. `service-messages-to-many-services` failed on this run (minimaxon's
program exited with a failure and printed nothing, so its times are blank and it is not in the means, which are over ten
programs): a memory-ordering bug of the runtime, under investigation. And the batch predates the faster hash fold, so
`map-operations` and the means are of the older code. Build the batch again for current numbers.

#### Systems

| target | CPU | cores | OS | rounds | compiler | Go |
|---|---|---|---|---|---|---|
| arm64-linux | Apple M3 | 8 | Linux ubuntu 7.2.12-orbstack-00394-gbecdd6130068 #1 SMP PREEMPT Thu Oct 22 22:17:17 UTC 2026 aarch64 GNU/Linux | 7 | minimaxon.exe at git b1877d1 | go version go1.27.4 windows/amd64 GOOS=linux GOARCH=arm64 |

#### Summary

| target | geometric-mean ratio of the programs | of the phases of 1 ms or more | worst phases |
|---|---|---|---|
| arm64-linux | 1.31 (10) | 1.30 (27) | service-messages-to-one-service / work 3.28; map-operations / string iterate 2.35; array-indexing / work 2.22 |

#### array-indexing

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| fill | 3.71 | 0.07 | 54.46 |
| rounds | 218.20 | 99.88 | 2.18 |
| work | 221.99 | 99.97 | 2.22 |


#### counted-loop

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| work | 222.80 | 273.05 | 0.82 |


#### float-calls

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| direct | 69.36 | 69.46 | 1.00 |
| through | 116.32 | 69.42 | 1.68 |
| work | 185.69 | 138.95 | 1.34 |


#### float-dot-and-nbody

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| dot | 12.81 | 12.64 | 1.01 |
| nbody | 27.65 | 17.76 | 1.56 |
| work | 40.49 | 30.49 | 1.33 |


#### map-operations

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| int insert | 70.60 | 50.09 | 1.41 |
| int hit | 31.37 | 25.15 | 1.25 |
| int miss | 20.91 | 18.54 | 1.13 |
| int iterate | 5.59 | 6.51 | 0.86 |
| int remove | 41.94 | 51.79 | 0.81 |
| string build | 42.31 | 78.95 | 0.54 |
| string insert | 193.89 | 94.19 | 2.06 |
| string hit | 68.17 | 39.26 | 1.74 |
| string miss | 53.34 | 25.11 | 2.12 |
| string iterate | 15.50 | 6.60 | 2.35 |
| string remove | 77.24 | 68.61 | 1.13 |
| work | 646.13 | 477.63 | 1.35 |


#### nested-loops

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| work | 44.35 | 41.55 | 1.07 |


#### records-and-unions

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| build | 0.02 | 0.02 | 1.00 |
| match | 25.75 | 27.82 | 0.93 |
| work | 25.79 | 27.84 | 0.93 |


#### recursion

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| work | 101.66 | 102.30 | 0.99 |


#### service-messages-to-many-services

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| spawn | 0.05 | 7.90 | 0.01 |
| send | - | 14.62 | - |
| await | - | 0.25 | - |
| work | - | 25.06 | - |

- arm64-linux: minimaxon exited with a failure (exit codes [1 1 1 1 1 1 1])
- arm64-linux: minimaxon printed [], not the expected ["499500000"]

#### service-messages-to-one-service

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| work | 61.47 | 18.73 | 3.28 |


#### string-building

| phase | arm64-linux minimaxon ms | arm64-linux Go ms | arm64-linux ratio |
|---|---:|---:|---:|
| work | 28.50 | 26.35 | 1.08 |


#### Peak memory and executable size

Peak memory is the largest resident set of the runs, in MiB; size is the file's, in KiB.

| program | arm64-linux minimaxon MiB | arm64-linux Go MiB | arm64-linux minimaxon KiB | arm64-linux Go KiB |
|---|---:|---:|---:|---:|
| array-indexing | 0.5 | 2.6 | 13 | 2297 |
| counted-loop | 0.4 | 2.0 | 9 | 2297 |
| float-calls | 0.4 | 2.1 | 14 | 2299 |
| float-dot-and-nbody | 0.4 | 2.2 | 18 | 2301 |
| map-operations | 156.4 | 152.5 | 49 | 2301 |
| nested-loops | 0.4 | 2.2 | 9 | 2297 |
| records-and-unions | 0.4 | 2.1 | 13 | 2297 |
| recursion | 0.4 | 2.0 | 9 | 2296 |
| service-messages-to-many-services | 10.7 | 23.1 | 36 | 2297 |
| service-messages-to-one-service | 0.7 | 2.0 | 36 | 2297 |
| string-building | 0.4 | 7.3 | 9 | 2297 |
