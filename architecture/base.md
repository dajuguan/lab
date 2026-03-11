# Base chain 架构分析

## BaseNodeExtension
在reth的NodeBuilder之上报了一层，底层还是调用的reth的Exex，adds_ons, rpc, node_started这些hooks

## Base的Hybrid proof
- TEE
- SP1 ZK
> 可以对比下UniChain的，因为OP的目前看起来没有官方的说明