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
