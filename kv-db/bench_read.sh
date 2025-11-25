#!/bin/bash

set -x

Keys=10000000 # ~1.1G
# pre write some data
if [ ! -d "bench_pebble_${Keys}_0" ]; then
    echo "generating test data..."
    go run cmd/db_bench/main.go -t 64 -op randwrite --keys $Keys -n $Keys -S 100 --dbn 1 --db pebble --handles 100000
fi

# performance of randread
for db in pebble; do
  for t in 1 2 4 8 16 32 64; do
    go run cmd/db_bench/main.go -t $t -op randread -keys $Keys -n 1000000 -S 100 --dbn 1 --db $db --handles 100000
  done
done