## Dependencies
- [samply](https://github.com/mstange/samply)

```sh
# the merge 15,537,394
# current 21971333
reth stage unwind --datadir ./rethdb_full to-block 16000000 
reth_maxperf=/root/test_nodes/op-sepolia/reth/target/maxperf/reth
samply record -p 3001 $reth_maxperf node --metrics localhost:9001 --authrpc.jwtsecret ../jwt.hex
reth-bench new-payload-fcu --rpc-url http://localhost:7542 --from 21970999 --to 21971331 --jwtsecret ../jwt.hex  --engine-rpc-url http://localhost:7552
```

> rpc-url 是另外一个同步到最新块的L1 EL节点RPC，不能自己调自己
# reth 目录: /root/test_nodes/op-sepolia/reth



## [DB](https://github.com/paradigmxyz/reth/blob/main/docs/design/database.md)
```
 reth db --datadir ./rethdb_archive stats

| PlainAccountState          | 281670885  | 48670        | 4233995    | 0              | 16.3 GiB   |
| PlainStorageState          | 1262538615 | 842230       | 26868824   | 0              | 105.7 GiB  |
```

所以一共有1.5G个index (2^30 * 2^2, 4个byte就够了，最多5个byte 2^40, 1100G)
- 链上开销: 
    - 一个块儿最多2943个slots, 那么需要 5*2943 bytes ≈ 15kb (一个blob都不需要)
- 内存开销:
    - 1.5G * (32 + 5 bytes) ≈ 55.5G 内存
    - 假设index增大到9G, 那么需要330G 内存


## 核心优化
尤其是[Georgios的推文](https://x.com/gakonst/status/1777306306598089094)和[reth perf](https://www.paradigm.xyz/2024/04/reth-perf)，主要有几个方面优化，还有周博的[flatDb](https://github.com/paradigmxyz/reth/blob/main/docs/design/database.md):
- JIT编译：2x 提升
- Parallel EVM: 2x提升(文中是5倍，但是实际上由于状态依赖会更小)
- State commitments: 2-3x提升, live sync中80-90%的时间都是在计算这个
    - 并行计算account的state tire
    - prefetch不改变的中间trie nodes
- DB优化
    - https://github.com/ava-labs/firewood 
    - https://docs.monad.xyz/monad-arch/execution/monaddb
    - https://sovereign.mirror.xyz/jfx_cJ_15saejG9ZuQWjnGnG-NfahbazQH98i1J3NN8


涉及到的概念:
- LSM(Log Structured-Merge Tree): 攒攒再写，所以写入会很快；但是读是log(N)的，因为没有索引

### [Monad优化方案](https://docs.monad.xyz/introduction/why-monad#addressing-these-bottlenecks-through-optimization)
- Paralle IO
- Monad DB(缩小log n的查找速度)
- Paralle EVM

- [Erigon Separation of keys and the structure](https://github.com/erigontech/erigon/blob/main/docs/programmers_guide/guide.md#separation-of-keys-and-the-structure)
- [nethermind,geth,reth gas benchmarks](https://github.com/NethermindEth/gas-benchmarks)
- [evmchainbenchmark by 0glabs](https://github.com/0glabs/evmchainbench)
- [monad db](https://docs.monad.xyz/monad-arch/execution/monaddb)