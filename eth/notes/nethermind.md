# Performance optimization
- [related PRs: performance]https://github.com/NethermindEth/nethermind/pulls?q=performance+label%3A%22performance+is+good%22
- [Pre-warm intra block cache during block execution](https://github.com/NethermindEth/nethermind/pull/7055)
- [Feature DB File warmer ](https://github.com/NethermindEth/nethermind/pull/7050)
- [Perf/eth getlogs with compact encoding](https://github.com/NethermindEth/nethermind/pull/5569)
- [Perf/HalfPath state db key](https://github.com/NethermindEth/nethermind/pull/6331)
    - https://www.nethermind.io/blog/nethermind-client-3-experimental-approaches-to-state-database-change
- [Perf/Reduce receipts db size](https://github.com/NethermindEth/nethermind/pull/5531)

## CMD Options
```
--Init.StateDbKeyScheme Hash  HalfPath(default)
--blocks-prewarmstateonblockprocessing false true(default)
```
|              |halfpath+prewarm| halfpath| halfpath | hash|
|---------     |----------------|---------|---------|---------|
|Average MGas/s|	435.3 	    |181.8    |198.2 	 |134.8 |
|speed up	   |prewarm only:	2.39 	  |	halfpath only:	1.47 |



### benchmark
```
blockHash=
curl localhost:7245   -X POST   -H "Content-Type: application/json"   --data '{
    "jsonrpc": "2.0",
    "id": 0,
    "method": "debug_resetHead",
    "params": ["'${blockHash}'"]
}'
```

## Notable releases
- [v1.25.0->v1.26.0: 68 to 107](https://github.com/NethermindEth/nethermind/releases/tag/1.26.0)
    - https://x.com/NethermindEth/status/1748338476561354774
    - [half-path](https://github.com/NethermindEth/nethermind/pull/6331)
    - [halfpath explained](https://medium.com/nethermind-eth/nethermind-client-3-experimental-approaches-to-state-database-change-8498e3d89771)
- [v1.26.0->v1.27.0: 107 to 254](https://github.com/NethermindEth/nethermind/releases/tag/1.27.0)
    - [Intra-block cache](https://github.com/NethermindEth/nethermind/pull/7039)
    - [Pre-warm intra block cache during block execution](https://github.com/NethermindEth/nethermind/pull/7055)
- [v1.27.0 -> v1.29.0: 254 to 800](https://github.com/NethermindEth/nethermind/releases/tag/1.29.0)
    - [Prewarm tx addresses in parallel](https://github.com/NethermindEth/nethermind/pull/7423)
- [v1.30.0: 1G, optimizing and parallelizing in-memory pruning](https://github.com/NethermindEth/nethermind/releases/tag/1.30.0)