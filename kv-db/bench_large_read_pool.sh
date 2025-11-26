#!/bin/bash

set -x

# 2B entries with ~110 size to mimic geth usage
Keys=2000000000 # ~285G

db=pebble
PooledHash=true
if [ "$PooledHash" = true ]; then
    EndFix="pooled"
    PoolHashFlag="--pooledHash"
else
    EndFix=""
    PoolHashFlag=""
fi
# pre write some data
if [ ! -d "bench_pebble_${Keys}_${EndFix}_0" ]; then
    echo "generating test data..."
    go run cmd/db_bench/main.go -t 64 -op randwrite --keys $Keys -n $Keys -S 170 --dbn 1 --db pebble --handles 100000 $PoolHashFlag
fi

# performance of randread
for db in pebble; do
#   for t in 1 2 4 8 16 32 64; do
    for t in 32; do
        echo 3 | sudo tee /proc/sys/vm/drop_caches
        go run cmd/db_bench/main.go -t $t -op randread -keys $Keys -n 40000000 -S 170 --dbn 1 --db $db --handles 100000 $PoolHashFlag
    done
done