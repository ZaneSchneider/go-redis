# Benchmark environment & provenance

Recorded 2026-07-12 on `zane-desktop` for the re-baseline runs. Captures the exact
machine, toolchain, and build that produced the benchmark numbers so the results are
reproducible. (The original 2026-07-10 dataset is archived under `v1-unpinned/` —
see the note at the bottom.)

## Hardware
- CPU: AMD Ryzen 5 3600X — 6 cores / 12 threads (1 socket)
- RAM: 16 GB (15 GiB reported by `free -h`)

## OS / kernel
- Ubuntu 26.04 LTS
- Kernel: 7.0.0-27-generic (x86_64)

## Baseline: real redis
- redis-server v8.0.5 (build 9729964261b8fc0f, malloc=jemalloc-5.3.0, 64-bit)
- Launched in-memory to match go-redis: `redis-server --save "" --appendonly no`
- systemd `redis-server` service stopped during runs so it does not share port 6379 or CPU

## Under test: go-redis
- Go toolchain: go1.26.0 linux/amd64
- Commit: f3897cd
- Build: `go build -o /tmp/go-redis ./app` — plain build, race detector OFF
  (CI builds with `-race`; the benchmark binary must not, or the numbers measure the detector)

## Benchmark tool
- redis-benchmark 8.0.5 (redis-tools)
- Transport: loopback — both servers on localhost
- Test set: SET, GET (the commands the harness sweeps)
- Methodology: median-of-3 per cell; one server benchmarked at a time
- Requests per cell scaled so every run lasts multiple seconds:
  `-n 500000` at P1, `-n 2000000` at P8, `-n 5000000` at P32;
  `-n 1000000` for all concurrency cells

## Stability
- CPU governor: pinned to `performance` for the full duration by `run.sh` — the script
  saves the prior governor, registers a trap that restores it on any exit (normal,
  Ctrl-C, or kill), and records the active governor to `run_metadata.txt` at run time
- Frequency boost: still enabled (CPU range 2200–4409 MHz), so some turbo variance
  remains; mitigated by the longer per-cell runs above

## Prior dataset: v1-unpinned/
The CSVs under `v1-unpinned/` are the original 2026-07-10 runs (commit `5daf870`):
`schedutil` governor with boost, and a flat `-n 100000`, which finished the fastest
cells in under a second and produced wide run-to-run spread. Superseded by the
re-baseline described above; kept for history.
