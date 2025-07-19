## big picture
目的: 了解不同client的DB相关实现机制，确定其优化空间，确定性能提升100x的可能性
目标: 
- 采用python/Go script写一遍核心工作流
- 能否测试下不同的DB scheme本身的上限
- 可能的EIP(提炼出可能得创新点)

核心技术问题:
- 如何抽象不同的client的scheme
    - 几个work flow如何进行交互
    - 以及组合不同的实现方式(采用interface)
- 如何用直观的方式测试DB的性能差异
    - read/write采用同一组数据及接口

- Reorg
    - short-range reorg
    - long-range reorg
- Snapshot如何做
- DB scheme
    - scheme 如何构成的(big picture)
        - Erigon flatDB
        - FirewoodDB
        - Nethermind DB scheme
    - 怎么单独测read/write的性能？
    - 如何calculate new state root
    - 如何commit dirty node
    - snapSync机制
- Archive node pipeline工作
- LevelDB设计及PebbleDB使用的底层优化技术
- 线程调度 

## Geth
### Difflayer
- statedb commit => trie commit两层的不同点 => freezer的不同

- state trie
    - read
        - account, storage状态存在每层中的缓存中
        - 采用统一的interface，最底层连着DB
    - write
        - diffToDisk用来最终保存
    - update
        - buffer
        - layerTree -> add layer -> diskLayer update
    - rollback/revert
        - 自己想的话: 
            - revert in-memory diff layer
            - revert disklayer with reverse diffs
                - triedb/pathdb/history.go
            - revert frozen
        - writeBlockAndSetHead
        - writeHeadersAndSetHead
        - tests:
            - testShortReorgedSetHead
            - testLongReorgedShallowSetHead
            - testLongReorgedDeepSetHead
            - with/without snapshot
        Recover(root)

        - SetHead: revert to local
        - SetCanonical 
            => recoverAncestors 
                => Recover(root) 
                    => 未超过diff bc.HasState(parent.Root())， journal中有相应的root，不用处理
                        - 1.下次插入新的block，会调用update，自动GC掉无用的linked list
                        - 2.直接stop， journal会根据当前的block root来只缓存对应层数的trie
                    => 超过，readhistory =>revert disklayer
                        => reset layers => reset journal
                => InsertChain
                    => medium: Update snapshot
                    => Stopped: (t *Tree) Journal(root common.Hash)
        
            