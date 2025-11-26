#!/bin/bash

set -x

Keys=1000000 # ~11G
PooledHash=false
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
    go run cmd/db_bench/main.go -t 64 -op randwrite --keys $Keys -n $Keys -S 100 --dbn 1 --db pebble --handles 100000 $PoolHashFlag
fi

# performance of randread
for db in pebble; do
#   for t in 1 2 4 8 16 32 64; do
    for t in 32; do
        echo 3 | sudo tee /proc/sys/vm/drop_caches
        go run cmd/db_bench/main.go -t $t -op randread -keys $Keys -n 1000000 -S 100 --dbn 1 --db $db --handles 100000 $PoolHashFlag
    done
done