#!/bin/bash

set -x

N=1000000
# performance of randwrite
for db in pebble; do
  for t in 1 2 4 8 16 32 64; do
    rm -r bench_$N_0; go run main.go -r 0 -t $t -op randwrite -n $N -S 100 --keys $N --dbn 1 --db $db --handles 100000
  done
done