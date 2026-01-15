# handles
```
go run cmd/db_bench/main.go -t 32 -op randread -n 10000000 -S 170 --keys 2000000000 --dbn 1 --db pebble --handles 1000
go run cmd/db_bench/main.go -t 32 -op randread -n 10000000 -S 170 --keys 2000000000 --dbn 1 --db pebble --handles 100000
```
1000:   250K IOPS, CPU 84%
100000: 620K IOPS, CPU 65%

128 kB memory allocation overhead: ~60ns, per IO time: 3 us, so the performance increase is 2%.


# Some designs
READAT => pread sync I/O
I/O size: LevelOptions {BlockSize: 4096}  4kb

## threads impact (max parallization ~2N threads, N is physical cores)
Run on AX101: 32 CORE 126G memory (stoped services which using large io/cpu/memory)

Total ops:
- Init KVs 2,000,000,000
- 10,000,000 ops random read
- file handles 100000

### Keping's results
Code path: https://github.com/ping-ke/research/tree/main/zig_rocksdb/code

Results: https://github.com/QuarkChain/TenGPS-research/issues/17

pebbledb:
- DB size: 236G
- DB files: 16975

rocksdb:
- DB size: 235G
- DB files: 5321

| Benchmark                | bench_go_eth_pebble        | bench_zig_rocksdb                  |
|--------------------------|----------------------------|------------------------------------|
| 32 threads read (Cold)   | 286,286 ops/s (40%)        | 264,207 ops/s (40%)                |
| 64 threads read (Cold)   | 297,291 ops/s (40%)        | 292,888 ops/s ~ 383,616 ops/s (97%)|
| 32 threads read (Warm)   | 349,687 ops/s (50%)        | 322,686 ops/s (60% → 70%)          |
| 64 threads read (Warm)   | 363,662 ops/s (50%)        | 352,660 ops/s ~ 420,651 ops/s (98%)|

thread context switch time(S)、切换次数(N)、单次I/O时间T的关系:
切换成本占比= S*N/T
一般而言SSD单词I/O成本是1000ns, 线程切换成本在250ns左右，因此切换成本占比=N/4，所以当N是CPU物理核心的4倍时已经没有任何额外的增益了。

### ioareana benchmark by me
Code: https://github.com/dajuguan/ioarena
- key: 16bytes
- value: 32 bytes
- entries: 1.6B (~84G rocksdb, ~128G mdbx)

校正数据:
| Threads | DB      | IOPS (Kops/s) | Avg Latency (µs) | CPU Usage (%) |
|--------:|---------|--------------:|-----------------:|--------------:|
|  2      | RocksDB |     12        |       160        |      1.1      |
|  2      | MDBX    |     21        |        85        |      0.8      |
|  4      | RocksDB |     30        |       130        |      2.2      |
|  4      | MDBX    |     48        |        84        |      1.3      |
|  8      | RocksDB |     85        |        92        |      4.5      |
|  8      | MDBX    |     97        |        83        |      2.5      |
| 16      | RocksDB |    180        |        90        |      8        |
| 16      | MDBX    |    180        |        86        |      6        |
| 32      | RocksDB |    300        |       110        |      24       |
| 32      | MDBX    |    360        |        90        |      13       |
| 64      | RocksDB |    350(不准确)        |       100        |      45       |
| 64      | MDBX    |    800        |        80        |      33       |

## cache impact
https://ethresear.ch/t/demystifying-blockchain-kv-lookups-from-o-log-n-to-o-1-disk-i-o

## pebbleV1 read routine 
- [getInternal](https://github.com/cockroachdb/pebble/blob/3622ade60459e2b3b9c6f3b36be3a212fa07b848/db.go#L535)
    - First()
        - [g.Next()](https://github.com/cockroachdb/pebble/blob/b5677d864d3461324526684e6ae6c7711cff0fea/get_iter.go#L64)
        - [SeekPrefixGE](https://github.com/cockroachdb/pebble/blob/1a45921accf7c4422d7214a8e6315c374d0725c6/sstable/reader_iter_single_lvl.go#L752)
            - i.reader.readFilter
            - [i.seekGEHelper](https://github.com/cockroachdb/pebble/blob/1a45921accf7c4422d7214a8e6315c374d0725c6/sstable/reader_iter_single_lvl.go#L626)
                - [i.loadBlock](https://github.com/cockroachdb/pebble/blob/1a45921accf7c4422d7214a8e6315c374d0725c6/sstable/reader_iter_single_lvl.go#L712)
    
cache misses is marked for all data in [`readBlock`](https://github.com/cockroachdb/pebble/blob/a3d91f3d23dd33e79d2ab2612fa6aa8091c5e3b2/sstable/reader.go#L519)'s [shard.Get](https://github.com/cockroachdb/pebble/blob/c8e13d9bd4cc15d8914f7dbce13a43a5fb66348e/internal/cache/clockpro.go#L123), including index block, filter block and data block. 

# Base Triedb


# Monad DB
- [read process](https://github.com/category-labs/monad/blob/045b62b36d35af8317cf894090ec71a9c9bce8fb/category/mpt/find.cpp)
    - node->fnext(idx) determin chunk id (file path) + offset in the file + how many bytes should be read
        - [chunk_offset_t](https://github.com/category-labs/monad/blob/1d4c34ffc4837a91d25b0ea2a3e855631f6d404c/category/async/config.hpp)
    - [read_node_blocking](https://github.com/category-labs/monad/blob/045b62b36d35af8317cf894090ec71a9c9bce8fb/category/mpt/read_node_blocking.cpp)


## storage layout
- chunk: 256M
- each nodes contains multiple 512 bytes pages

```
Branch Node (chunk 1)
+ fnext_data:
    index 0 -> chunk 1, offset 0x300 (leaf)
    index 1 -> chunk 2, offset 0x120 (branch)
    
traverse index 0 -> leaf node：
  leaf node has been in chunk 1's continous page，no need to switch chunk

traverse index 1 -> branch node：
  fnext.idx → chunk 2
  read_node_blocking → switch to chunk 2 to read the node data
```