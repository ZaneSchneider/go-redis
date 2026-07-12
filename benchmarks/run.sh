#!/bin/bash

# 1. cache sudo credentials so the trap's restore never stalls on a password prompt
sudo -v

# 2. save whatever governor you're currently running (will be "schedutil")
orig=$(cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor)

# 3. define how to put it back
restore() {
  echo "$orig" | sudo tee /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor > /dev/null
}

# 4. register it BEFORE changing anything: fires on normal end, Ctrl-C, or kill
trap restore EXIT INT TERM

# 5. now pin performance for the duration of the runs
echo performance | sudo tee /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor > /dev/null

# 6. record provenance next to the CSVs
echo "governor: $(cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor)  date: $(date)" >> run_metadata.txt

# 7. SWEEP 1 — your existing pipelining loops, with two edits
# 8. SWEEP 2 — your existing concurrency loops, with one edit

# 9. nothing goes at the end — the trap restores the governor automatically
mkdir -p pipelining
mkdir -p concurrency

for s in 6379 6380; do
  for run in 1 2 3; do
    for p in 1 8 32; do
      case "$p" in            # NEW: pick -n by depth, right after p gets its value
        1)  n=500000 ;;
        8)  n=2000000 ;;
        *)  n=5000000 ;;
      esac
      for cmd in set get; do
        redis-benchmark -p $s -t $cmd -P $p -n $n -c 1 --csv >> pipelining/${cmd}_${s}_P${p}.csv
      done
    done
  done
done

for s in 6379 6380; do
  for run in 1 2 3; do
    for cmd in set get; do
      for c in 1 10 50 100; do
        redis-benchmark -p $s -t $cmd -P 1 -n 1000000 -c $c --csv >> concurrency/${cmd}_${s}_c${c}.csv
      done
    done
  done
done