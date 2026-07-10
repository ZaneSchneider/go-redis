# Benchmark environment & provenance

Recorded 2026-07-10 on `zane-desktop`. Captures the exact machine, toolchain, and
build that produced the benchmark numbers so the results are reproducible.

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
- Commit: 5daf870
- Build: `go build -o /tmp/go-redis ./app` — plain build, race detector OFF
  (CI builds with `-race`; the benchmark binary must not, or the numbers measure the detector)

## Benchmark tool
- redis-benchmark 8.0.5 (redis-tools)
- Transport: loopback — both servers on localhost
- Test set: SET, GET, INCR, PING (the commands go-redis implements)
- Methodology: median-of-3 per cell; one server benchmarked at a time

## Stability
- CPU governor: `schedutil` (dynamic frequency scaling — clocks vary with load)
- Frequency boost: enabled. CPU range 2200–4409 MHz; `lscpu` reported "scaling MHz: 58%"
  at capture, confirming cores were not sitting at max clock.
- Action before runs: pin the `performance` governor so cores hold a fixed clock —
  `echo performance | sudo tee /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor`
  (or `sudo cpupower frequency-set -g performance` if `linux-tools` is installed).
  Record the governor used for each run set; a fixed clock is what keeps the median-of-3 tight.
