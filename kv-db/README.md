# handles
```
go run cmd/db_bench/main.go -t 32 -op randread -n 10000000 -S 170 --keys 2000000000 --dbn 1 --db pebble --handles 1000
go run cmd/db_bench/main.go -t 32 -op randread -n 10000000 -S 170 --keys 2000000000 --dbn 1 --db pebble --handles 100000
```
1000:   250K IOPS, CPU 84%
100000: 620K IOPS, CPU 65%

128 kB memory allocation overhead: ~60ns, per IO time: 3 us, so the performance increase is 2%.
