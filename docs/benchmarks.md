# Benchmarking redis-server-go against real Redis

How this server compares to `redis-server` 8.0.5 on identical hardware, what
profiling revealed about the gap, and what one fix changed.

Single-command throughput is ~0.9× real Redis. Under pipelining it was
0.16×. a CPU profile showed ~80% of time in per-reply write syscalls; batching
replies behind a buffered writer raised pipelined throughput 4.2–4.4×, to
0.71–0.74× Redis.

## Methodology

Both servers on loopback, one benchmarked at a time, in-memory configurations
(`--save "" --appendonly no` for Redis), driven by `redis-benchmark` 8.0.5.
Medians of 3 runs per cell; CPU governor pinned to `performance` for every run;
request counts scaled so each cell runs multiple seconds. Full environment,
dataset provenance, and raw CSVs: [`benchmarks/`](../benchmarks/environment.md).
Every dataset's `run_metadata.txt` records the exact commit, governor, and date
it was produced from.

- Baseline (pre-fix): [`benchmarks/v1-pinned/`](../benchmarks/), commit `3eeb10a`
- After the fix: [`benchmarks/v2-pinned/`](../benchmarks/), commit `c4e374d`

## Results

**Single command** (`-c 1 -P 1`, median rps):

| | GET | SET |
|---|---|---|
| redis | 43.3k | 43.0k |
| go-redis | 37.9k | 38.8k |
| ratio | 0.88 | 0.90 |

**Pipelining** (`-c 1`, median rps, GET; SET tracks within a few %):

| | P1 | P8 | P32 |
|---|---|---|---|
| redis | 43.3k | 317.8k | 911.4k |
| go-redis before | 39.4k | 109.5k | 147.2k |
| go-redis after | 37.9k | 262.1k | 645.9k |
| ratio before | 0.91 | 0.35 | 0.16 |
| ratio after | 0.88 | 0.82 | 0.71 |

The pre-fix CPU profile under pipelined load (`benchmarks/profiles/`) attributed
~80% of CPU time to the write syscall path.

## Limitations

- Loopback on a single machine: absolute numbers flatter both servers; only the
  *comparison* on the same box is meaningful.
- Load generator and servers share the 6-core CPU.
- GET/SET only, small values, no persistence on either side.
- Frequency boost remained enabled (variance mitigated by multi-second cells).
- Redis ran its default single-threaded command execution (`io-threads 1`).

## Future work

- AOF persistence, then a with/without-persistence benchmark sequel.
- Master–replica replication.

## Reproducing

`benchmarks/run.sh` runs all three sweeps (pins the CPU governor for the
duration and restores it on exit; requires sudo, redis-tools, and both servers
running). See [`benchmarks/environment.md`](../benchmarks/environment.md).
