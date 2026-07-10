# go-redis

[![Go](https://github.com/ZaneSchneider/go-redis/actions/workflows/ci.yml/badge.svg)](https://github.com/ZaneSchneider/go-redis/actions/workflows/ci.yml)

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
go run ./app
```

The server listens on `:6379`. Talk to it with the standard `redis-cli`:

```sh
redis-cli SET language go
redis-cli GET language
```

## Design notes

- **Concurrency model.** Each client connection gets its own goroutine; shared state sits behind a mutex. Single commands lock per operation, while `EXEC` holds the lock for the entire transaction, approximating the atomicity real Redis gets from single-threaded execution.
- **WATCH via version counters.** Every write (including a lazy-expiry delete) bumps a per-key version. `WATCH` snapshots versions; `EXEC` aborts on any mismatch. Counters only increment, so the check is immune to ABA problems.
- **Parsing.** Commands are decoded from a buffered TCP stream with length-prefixed reads.

## Known divergences

`INCR` is more lenient than real Redis on non-canonical integers (e.g. `"+5"`, `"007"`).

## Roadmap

Integration test suite in CI, configuration and graceful shutdown, Docker packaging, benchmarks against `redis-server`, master–replica replication.

## Attribution

Started from the [CodeCrafters](https://codecrafters.io) "Build Your Own Redis" challenge, which I used as a stage roadmap. The implementation and tests are my own.
