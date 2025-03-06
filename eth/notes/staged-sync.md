# How staged-sync works?

**Why not use a pipeline?**  
- **Decomposition** enables each stage to be profiled and optimized independently.  
- Except for signature verification, most stages are I/O-bound and cannot benefit from parallelism. In fact, performance may even be degraded by concurrency due to context switching and the loss of data access locality.  
- Before staged sync was implemented, the computation of the state root hash and its verification were performed for every single block, which became a significant performance bottleneck. However, it was observed that nodes using fast-sync or warp-sync only perform such verification on the state snapshot they download and subsequent blocks. A similar approach was adopted in staged sync:  
  - In one stage, all historical transactions were replayed to advance the current state, **with only receipt hashes being verified for each block**.  
  - Once the state was advanced to the head of the chain, the state trie was computed, its root hash was verified, and the process continued from there.  
- To address write amplification caused by random writes in B-tree-based databases (where data is stored in pages), an **ETL (Extract, Transform, Load) framework was implemented**. This framework inserts keys in sorted order, allowing pages to be filled sequentially and reducing inefficiencies.  

**Performance Breakdown:**  
After these optimizations, the wall time was roughly distributed as follows:  
- **~1/3** was taken by the EVM interpreter
- **~1/3** was taken by Golang’s garbage collector (after optimizations targeting memory allocations),  
- **~1/3** was taken by I/O (primarily reads from LMDB, before transitioning to MDBX).  

## Refs:
- [Staged Sync and short history of Silkworm project](https://erigon.substack.com/p/staged-sync-and-short-history-of)
- [Towards the first release of Turbo-geth(now erigon)](https://ledgerwatch.github.io/turbo_geth_release.html)
- [erigon how staged sync works](https://github.com/bobanetwork/op-erigon-interfaces/blob/boba-develop/_docs/staged-sync.md)