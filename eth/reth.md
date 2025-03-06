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