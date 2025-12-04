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