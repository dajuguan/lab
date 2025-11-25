#!/bin/bash

set -x

# 2B entries with ~110 size to mimic geth usage
db=pebble
Keys=2000000000 # ~285G
if [ ! -d "bench_pebble_${Keys}_0" ]; then
    echo "generating data..."
    # go run cmd/db_bench/main.go -t 32 -op randwrite -n $Keys -S 170 --keys $Keys --dbn 1 --db $db --handles 100000
fi
# for t in 1 2 4 8 16 32 64; do
for t in 8 16 32 64; do
  go run cmd/db_bench/main.go -t $t -op randread -n 10000000 -S 170 --keys $Keys --dbn 1 --db $db --handles 100000
done