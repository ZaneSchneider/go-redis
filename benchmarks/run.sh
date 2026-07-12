#!/bin/bash

for s in 6379 6380; do
  for run in 1 2 3; do
    for p in 1 8 32; do
      for cmd in set get; do
        redis-benchmark -p $s -t $cmd -P $p -n 100000 -c 1 --csv >> ${cmd}_${s}_P${p}.csv
      done
    done
  done
done

for s in 6379 6380; do
  for run in 1 2 3; do
    for cmd in set get; do
      for c in 1 10 50 100; do
        redis-benchmark -p $s -t $cmd -P 1 -n 100000 -c $c --csv >> ${cmd}_${s}_c${c}.csv
      done
    done
  done
done