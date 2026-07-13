# redis-server-go

[![Go](https://github.com/ZaneSchneider/go-redis/actions/workflows/ci.yml/badge.svg)](https://github.com/ZaneSchneider/redis-server-go/actions/workflows/ci.yml)

A Redis-compatible in-memory data store written from scratch in Go, using only the standard library. The wire protocol, concurrency model, and transaction semantics are implemented by hand.

## Implemented

- RESP wire protocol over raw TCP, decoded by a hand-rolled streaming parser (binary-safe bulk strings)
- Core commands: `PING`, `ECHO`, `GET`, `SET` (with `PX` millisecond expiry), `INCR`
- Lazy key expiry, matching real Redis semantics
- Concurrent clients: one goroutine per connection over a mutex-guarded store
- Transactions: `MULTI` / `EXEC` / `DISCARD`, with queue-time command validation and `EXECABORT` semantics
- Optimistic locking: `WATCH` / `UNWATCH` via per-key version counters; a transaction executes atomically inside a single critical section and aborts if any watched key changed

## Run

```sh
go run ./app            # listens on :6379
go run ./app --port 7000
```

Or with Docker (multi-stage build, static binary on a distroless base):

```sh
docker build -t go-redis .
docker run -p 6379:6379 go-redis
```

Talk to it with the standard `redis-cli`:

```sh
redis-cli SET language go
redis-cli GET language
```

Shuts down cleanly on SIGINT/SIGTERM (`Ctrl-C`, `docker stop`).

## Performance

Benchmarked against real `redis-server` 8.0.5 on the same machine — methodology,
raw data, and provenance in [`benchmarks/`](benchmarks/environment.md):

- ~0.9× Redis single-command throughput
- Pipelined throughput was 0.16× Redis; profiling showed ~80% of CPU in
  per-reply write syscalls, and batching replies behind a buffered writer
  raised it 4.4×, to ~0.72× Redis

Full writeup: [docs/benchmarks.md](docs/benchmarks.md)

## Testing

Integration suite speaks raw RESP over TCP against an in-process server:
byte-exact assertions for every command, transaction, and error path, plus a
two-connection WATCH race and an 8-goroutine stress test. Runs under the race
detector locally and in CI on every push.

## Design notes

- **Concurrency model.** Each client connection gets its own goroutine; shared state sits behind a mutex. Single commands lock per operation, while `EXEC` holds the lock for the entire transaction, approximating the atomicity real Redis gets from single-threaded execution.
- **WATCH via version counters.** Every write (including a lazy-expiry delete) bumps a per-key version. `WATCH` snapshots versions; `EXEC` aborts on any mismatch. Counters only increment, so the check is immune to ABA problems.
- **Parsing.** Commands are decoded from a buffered TCP stream with length-prefixed reads.

## Known divergences

- `INCR` is more lenient than real Redis on non-canonical integers (e.g. `"+5"`, `"007"`).
- A watched key that expires during `WATCH` only counts as modified if something
  touches it before `EXEC`; real Redis treats the expiry itself as a modification.

## Roadmap

AOF persistence, master–replica replication, and a proper analysis of
concurrency ceilings with a multi-threaded load generator (see the
[writeup's future work](docs/benchmarks.md#future-work)).

## Attribution

Started from the [CodeCrafters](https://codecrafters.io) "Build Your Own Redis" challenge, which I used as a stage roadmap.
