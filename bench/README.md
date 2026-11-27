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
