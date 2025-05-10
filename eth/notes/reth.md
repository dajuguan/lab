## Live-sync benchmark
- [samply](https://github.com/mstange/samply)

```sh
# the merge 15,537,394
# current 22119000
reth stage unwind --datadir ./rethdb_full to-block 21973379 
reth_maxperf=/root/test_nodes/op-sepolia/reth/target/maxperf/reth
# samply record -p 3001 $reth_maxperf node --metrics localhost:9001 --authrpc.jwtsecret ../jwt.hex


# start reth with --engine.caching-and-prewarming
RUST_LOG=debug reth node --full --http   --http.api eth,net,debug,web3,txpool,rpc --authrpc.jwtsecret=../jwt.hex --discovery.port 30302 --port 30302 --http.port 7542 --authrpc.port 7552 --datadir ./rethdb_full --engine.caching-and-prewarming

## start reth bench
reth-bench new-payload-fcu --rpc-url http://localhost:7544 --from 21971329 --to 21973329 --jwtsecret ../jwt.hex  --engine-rpc-url http://localhost:7552 
```

### without livesync
```
21973379 -> 21974379
## with RUST_LOG=debug
reth:0.1308 total_duration=140.932709237s total_gas_used=18429809083 blocks_processed=1000
## without RUST_LOG=debug
Total Ggas/s: 0.2045 total_duration=90.110011411s total_gas_used=18429809083 blocks_processed=1000
## without RUST_LOG=debug
acL:
```

> rpc-url 是另外一个同步到最新块的L1 EL节点RPC，不能自己调自己
# reth 目录: /root/test_nodes/op-sepolia/reth


## Access-list
算了主网3万个块儿左右:
```
average addr count per block: 394.41480968858133
average storagekeys count per block: 1406.8443598615918
max addr count of a block: 2499
max storagekeys of a block: 2943
total txs: 4874075
```

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
- [State Root Calculation for Engine Payloads](https://github.com/paradigmxyz/reth/blob/main/crates/engine/tree/docs/root.md#revealing-example)

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

## Reth access-list
- bin/reth-bench/src/valid_payload.rs
    - new_payload_v3_wait
- https://github.com/paradigmxyz/reth/blob/97bc3611db7b4b60758c311e448d26514eb65ca8/crates/rpc/rpc-engine-api/src/engine_api.rs#L648

# Engine API
- [new_payload_v3](https://github.com/paradigmxyz/reth/blob/97bc3611db7b4b60758c311e448d26514eb65ca8/crates/rpc/rpc-engine-api/src/engine_api.rs#L648)
- [send payload by msg passing(channel)](https://github.com/paradigmxyz/reth/blob/e468d4d7c5ab5d4af5a19d9deaf126ab64033f8e/crates/engine/primitives/src/message.rs#L223)
- [run loop](https://github.com/paradigmxyz/reth/blob/75ca54b79039a98701df82a9817cf869e92ef588/crates/engine/tree/src/tree/mod.rs#L778)
- [on_new_payload](https://github.com/paradigmxyz/reth/blob/75ca54b79039a98701df82a9817cf869e92ef588/crates/engine/tree/src/tree/mod.rs#L1441)
    - [insert_block](https://github.com/paradigmxyz/reth/blob/75ca54b79039a98701df82a9817cf869e92ef588/crates/engine/tree/src/tree/mod.rs#L922)
    - [use_caching_and_prewarming](https://github.com/paradigmxyz/reth/blob/75ca54b79039a98701df82a9817cf869e92ef588/crates/engine/tree/src/tree/mod.rs#L2474)
- [advance_persistence](https://github.com/paradigmxyz/reth/blob/75ca54b79039a98701df82a9817cf869e92ef588/crates/engine/tree/src/tree/mod.rs#L797)



# Reth perf optimization

## [DB](erigon: https://www.youtube.com/watch?v=e9S1aPDfYgw)
- [reth DB design](https://github.com/paradigmxyz/reth/blob/2ba54bf1c1f38c7173838f37027315a09287c20a/docs/design/database.md)
- [compression keys](crates/primitives-traits/src/storage.rs)
- snapshot
- flat storage
- history & changesets
- ACD transaction: mdbx db
- write amplication: [etl (extract, tranform, load)](https://github.com/paradigmxyz/reth/blob/cf095a7536d9a21a1c16cfb9dac2654a1889f1e8/crates/etl/src/lib.rs)
- Dupsort (remove duplicate zeros)

## Histotical sync
- [stages](https://github.com/paradigmxyz/reth/blob/3f680fd6ccee1045e40a84917be35d5bd7c5b810/docs/crates/stages.md)
- https://github.com/paradigmxyz/reth/blob/172369afd58b128fd0482dd0c385d9ccfc18f4fc/crates/stages/stages/src/sets.rs

## Live-sync
- [use_caching_and_prewarming](https://github.com/paradigmxyz/reth/blob/75ca54b79039a98701df82a9817cf869e92ef588/crates/engine/tree/src/tree/mod.rs#L2474)
- State Root Calculation for Engine Payloads
    - [State Root Calculation for Engine Payloads](https://github.com/paradigmxyz/reth/blob/main/crates/engine/tree/docs/root.md#state-root-task)
    - [Spawn state root task](https://github.com/paradigmxyz/reth/blob/75ca54b79039a98701df82a9817cf869e92ef588/crates/engine/tree/src/tree/mod.rs#L2443)
    - [erigon ](https://github.com/erigontech/erigon/blob/main/docs/programmers_guide/guide.md)