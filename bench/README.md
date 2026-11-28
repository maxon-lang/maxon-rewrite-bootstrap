# Benchmarks of whole programs

The micro-benchmarks of the code generator are `tests/bench/` (`bench.ps1`). This directory holds the benchmarks that
need a peer process or the system's limits.

## idle: idle connections

What an idle connection costs a minimaxon server whose connections are each a service armed with a posted message
(`stdlib/net/readiness.maxon`), against Go's goroutine per connection (out/dev/step3-readiness-design.md §6, choice 12 (a):
the scaled lanes, with no root).

- `bench/idle/server.maxon` listens on 127.0.0.1, takes a control connection, then accepts N connections, each into its own
  `Conn` service, which arms `client.notifyReadable(self.handle.post(Conn.Message.readable))` and on a wake reads what came,
  echoes it with `trySend` and arms again. No green thread waits for a connection.
- `bench/idle/peer` (Go, `go build` only) starts the server, opens the N connections, keeping fewer than the listen backlog
  (128) unaccepted, and reports: the server's memory before and after, per connection (`ps -o rss` and `footprint` on
  macOS, `RssAnon` on Linux), its OS threads, the accept rate, its user and system CPU over an idle period with keepalive on
  (both servers probe after 15 s, every 15 s), and the round trip of a 1-byte echo on a random sample of the connections
  while the rest stay idle. `peer goserver` is the Go baseline with the same protocol.
- `bench/idle/run.sh` builds both and runs the minimaxon server and then the Go one; `N`, `WAKE`, `IDLE` and `PORT` set the
  lane (16000 connections fits macOS's 16384 ephemeral ports on 127.0.0.1). It needs no root and changes no limit: the
  minimaxon runtime and Go each raise their soft open-file limit to the hard one. The peer ends each server before it
  resets its own connections, so no port is left in TIME_WAIT for the next run.

The proposed acceptance is a user-space cost of at most 512 bytes a connection and no green thread while idle. A green
thread would show as its stack in the memory per connection: at least the page it has touched, 16 KiB on macOS arm64
and 4 KiB on Linux.

What an idle connection costs (16000 connections, macOS and arm64 Linux): 7 heap boxes with 456 B asked, about 512 B with
their 8-byte headers, and the connection's 128-byte event entry, carved from the scheduler's blocks and not counted by the
heap: about 535 B of RSS a connection (530-544 measured), against Go's 3600 (a goroutine's stack, its netFD and pollDesc).

The Linux wake latency depends on where the kernel puts the two threads, not on the runtime. In a VM (OrbStack, 7 vCPUs)
a wake that crosses to another, halted vCPU costs about 10 µs each way, so a run whose server thread and peer thread
settle on different vCPUs measures p50 about 25 µs, and one where they share a vCPU about 7 µs, for either server: pinned
to different CPUs, minimaxon 25.3-25.9 µs and Go 28.4-30 µs; pinned to one, minimaxon 6.7-7.0 µs and Go 8.2-11 µs. The
server's own path from the epoll return to its send is 1.0-1.6 µs. Unpinned, Go's pair has always settled on one vCPU and
minimaxon's in about a third of the runs, so for a comparison pin the whole run: `taskset -c 2 bench/idle/run.sh`.

### On Windows

`run.sh` needs a shell (Git Bash has one), and its binaries need `.exe`, so the lane is run by hand:

```
bin/minimaxon build bench/idle/server.maxon -o bin/idle-server.exe
go build -o bin/idle-peer.exe ./bench/idle/peer
bin/idle-peer.exe run -n 16000 -port 47001 -wake 10000 -idle 30s -label minimaxon -- bin/idle-server.exe
bin/idle-peer.exe run -n 16000 -port 47002 -wake 10000 -idle 30s -label go -- bin/idle-peer.exe goserver
```

The peer measures the server by its working set (`rss`) and its private bytes (`footprint`), its user and kernel CPU, its threads,
and the system's nonpaged pool (the kernel's memory for each connection's sockets and for the requests of its readiness, which
both ends of every connection charge, so it is the same order for either server). It times a wake by the performance counter,
since Go's monotonic clock on Windows ticks with the system's timer, and opens each connection from a port below the dynamic
range (20000 and up) of one of 127.0.0.2 and 127.0.0.3: a bind of port 0 takes one of the dynamic range's 16384 ports whatever
the address, less those the system holds, which fails a run of 16000 connections.

What an idle connection costs (16000 connections, Windows 11 on x64, 12 logical processors): the server's private bytes grow by
1.1 KB a connection (1.0 KB of working set), 7 heap boxes with 461 B asked, against Go's 10.8 KB (10.7 KB of working set); the
system's nonpaged pool grows by 7.5 KB a connection for either server, 7.6 KB for Go's, each connection being two sockets, and
by 9.9 and 9.3 KB once 10000 of them have each carried a byte. The server holds 3 to 6 threads, Go's 14; each end of an idle
connection costs the system about the same for either (the poll of AFD that arms a minimaxon connection holds about 1.1 KB of
nonpaged pool that the process is charged for, `runtime/README.md`, "The readiness of a socket on Windows", where Go's overlapped
read of no bytes holds about 0.5 KB, but the system's pool as a whole grows by less for the poll, 0.8 KB against 1.3 KB when each is measured
alone). Over 30 idle seconds with keepalive on (a probe after 15 s) the server uses 0.03 s of kernel time and none of user time,
Go's none, and a wake's round trip is p50 67 µs, p99 101 µs for minimaxon and p50 66 µs, p99 96 µs for Go.

## timers: service timers

What a service timer (`stdlib/service/Timer.maxon`) costs on the timers' one heap, a 4-ary heap of 16-byte slots under its
own lock (`runtime/timers.maxon`), against Go's `time.AfterFunc`, `Reset` and `Stop` on its per-P heaps
(out/dev/timers-design.md §5, "Measurement plan").

- `bench/timers/timers.maxon run <count> <workers> <stormMs> <churnServices> <churnTimers> <churnMs>` runs four lanes on
  `workers` services, every timer posting to the service that armed it:
  - **arm**: each worker arms its share of `count` timers 1 to 2 hours out, at random, all at once, so the heap grows to
    `count`; the result is the wall time of the slowest worker over `count`, nanoseconds a timer for the whole set.
  - **reset**: for `stormMs` each worker resets random timers of its own to a random deadline 1 to 2 hours out, so about
    half move later, which takes the timer's lock alone, and half earlier, which takes the heap's; resets a second in all.
    Then **cancel** frees them all.
  - **fire**: `count` timers armed again toward one instant far enough out that every arm is made before it (the result
    says how far past it the arms ran, always 0 here); the time from that instant to the last handler over `count`.
  - **churn**: `churnServices` services each keep `churnTimers` 1 ms one-shots, each re-armed by the handler of its own fire
    (`rearm`), for `churnMs`; fires a second in all. 16 × 4096 timers ask for 65 million fires a second, so the lane measures
    what the runtime can fire, not the timers' period. Each handler counts its window's fires itself, reading the clock
    every 256 fires, and main asks for the count only once the churn has stopped, since a send to a service that its own
    timers keep behind never returns (below).
- `bench/timers/timers.maxon idle <count>` arms `count` timers an hour out toward one service when it reads `a` on its
  standard input, and says what the heap holds then.
- `bench/timers/gobench` (Go, `go build` only) is the baseline: `run` is the same four lanes with `AfterFunc` on goroutines,
  `Reset` and `Stop`, a churn's callback calling `Reset(1ms)` on its own timer; `idlechild` is the idle side; and `idle`
  drives either, reading the program's memory (`ps -o rss` and `footprint`) before and after its arms and its user and
  system CPU over an idle period after them.
- `bench/timers/run.sh` builds both and runs each processor count of `PROCS` (`MAXON_MAX_PROCS` and `GOMAXPROCS`, one worker
  a processor) at each count of `COUNTS`, the churn once a processor count, then the idle lanes; `STORM_MS`, `CHURN_SERVICES`,
  `CHURN_TIMERS`, `CHURN_MS`, `IDLE_N` and `IDLE` set the rest. The whole lane takes about a minute.
- On Windows the lanes are run by hand, as idle's are (`run.sh` needs a shell and its binaries `.exe`): build
  `bin/timers.exe` and `bin/timers-gobench.exe`, then `timers.exe run ...` and `timers-gobench.exe run ...` under
  `MAXON_MAX_PROCS` and `GOMAXPROCS`, and `timers-gobench.exe idle -n N -idle 5s -label minimaxon -- bin/timers.exe idle`.
  The baseline reads the server through `bench/procstat`, which bench/idle's peer shares: the working set and private
  bytes, CPU and threads on Windows, as `ps`, `footprint` and `/proc` give them elsewhere.

Measured on an Apple M3 (arm64 macOS, 4 performance and 4 efficiency cores, 24 GB), one run of `bench/timers/run.sh` with
its defaults; ns are the wall time of the whole set over its count, so they fall as the processors share the work and rise
where they contend:

| lane | procs 1 | procs 2 | procs 4 | procs 8 |
|---|---|---|---|---|
| arm, 1e5 armed, ns a timer: minimaxon / Go | 60 / 178 | 84 / 124 | 117 / 59 | 178 / 55 |
| arm, 1e6 armed | 62 / 165 | 90 / 74 | 118 / 45 | 189 / 46 |
| fire, 1e5 due at once, ns a fire | 166 / 612 | 168 / 483 | 164 / 230 | 186 / 261 |
| fire, 1e6 due at once | 279 / 567 | 244 / 386 | 207 / 325 | 251 / 259 |
| cancel, 1e5 armed, ns a timer | 23 / 6 | 31 / 3 | 55 / 8 | 83 / 8 |
| cancel, 1e6 armed | 34 / 6 | 63 / 3 | 75 / 9 | 160 / 10 |
| reset storm on 1e5 armed, millions a second | 45.5 / 36.1 | 91.4 / 72.9 | 183 / 124 | 295 / 192 |
| reset storm on 1e6 armed | 12.1 / 8.4 | 22.8 / 17.4 | 37.8 / 34.6 | 37.5 / 49.4 |
| churn, 16 services × 4096 1 ms one-shots, millions of fires a second | 7.60 / 3.55 | 6.53 / 4.66 | 5.20 / 6.26 | 2.68 / 6.32 |

Idle, 1e6 timers armed an hour out: minimaxon's RSS grows by 257 B a timer (4 heap boxes, 200 B asked: the 56-byte timer
record, its posted record and message, and the array's 8-byte slot of the handle), Go's by 168-171 B. Over an idle 5 s or
10 s minimaxon used 10 ms of CPU, the same for either period, so once and not while idle, with 4 threads; Go none, with
10-11.

On one processor a timer costs less than Go's to arm, reset and fire (60 against 178 ns an arm, 166 against 612 a fire,
where Go's callback starts a goroutine), and a reset storm scales with the processors, since a reset to later takes
no shared word; at 1e6 timers both are bound by the cache misses of random timers. Arms and cancels from several
processors at once contend for the heap's lock, and so does the churn, which falls as processors are added: 2.68 million
fires a second at 8 processors is 0.35 times the 7.60 of one, where §5 calls for per-P heaps below 4 times. A sample of the
churn at 8 processors (`sample` on a `--debug-info` build) has about 85% of the busy threads' samples in `spinLock`'s
`yieldThread` under `lockTimers`, almost all from `timerArm` of a `rearm` and the rest from `runTimers`, against the 5% of
§5; no build counts the spin rounds of `spinLock`, so that share is sampled, not counted. By §5's rule the churn calls for
per-P heaps; they are not built here.

A bug the churn found, which the runtime has not fixed: a send from main to a service whose own timers keep it behind
never returns: the send is slowed by backpressure until the
service's queue empties, and a service whose handlers re-arm timers that are already due is refilled by their posts as
fast as it drains, so its queue never empties. With one service of n 1 ms one-shots that re-arm themselves, main's send of
an ordinary message after 200 ms returns in 2 ms at n = 16384 and never at n = 24576 or 32768, at 1 or 4 processors; a
`sleep` in main still returns on time.
