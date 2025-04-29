## DB
- 核心代码在[insertChain](https://github.com/ethereum/go-ethereum/blob/e6f3ce7b168b8f346de621a8f60d2fa57c2ebfb0/core/blockchain.go#L1609)
    - bc.processBlock(block, statedb, start, setHead)
        - bc.processor.Process(block, statedb, bc.vmConfig)
        - [EVM processor](https://github.com/ethereum/go-ethereum/blob/67a3b087951a3f3a8e341ae32b6ec18f3553e5cc/core/state_processor.go#L57)
        - [Read state](https://github.com/dajuguan/go-ethereum/blob/851542857cca75c731bd82bfa49fd4eadea033aa/core/vm/instructions.go#L521)
            - EVM和DB交互的所有接口都在`type StateDB interface`中

[以太坊的数据结构](https://s1na.substack.com/p/the-tale-of-5-dbs-24-07-26),主要包括5种DB:
- ethdb: 定义了持久层的接口和leveldb/pebbledb/memorydb/remotedb对其接口的具体实现
- triedb: 介于trie和持久成之间，包括两种后端
    - hashdb: key为node的hash，value为node的值
    - pathdb: key为节点的path
- statedb: 在执行一个区块交易的时候需要从ethdb或triedb中读取contract和storage trie，他提供了一个thin layer来读取这些数据
    - 生命周期为一个区块
    - L2魔改EVM主要就是要修改statedb
- rawdb: 可以看做KV数据的schema，他定义了实际的数据在DB中的key具体是如何定义的

### hashdb
```
var CodePrefix = []byte("c") // CodePrefix + code hash -> account code

// codeKey = CodePrefix + hash
func codeKey(hash common.Hash) []byte {
 return append(CodePrefix, hash.Bytes()...)
}

// ReadCodeWithPrefix retrieves the contract code of the provided code hash.
func ReadCodeWithPrefix(db ethdb.KeyValueReader, hash common.Hash) []byte {
 data, _ := db.Get(codeKey(hash))
 return data
}

func ReadLegacyTrieNode(db ethdb.KeyValueReader, hash common.Hash) []byte {
	data, err := db.Get(hash.Bytes())
	if err != nil {
		return nil
	}
	return data
}
```

### pathdb
```
 func ReadStorageTrieNode(db ethdb.KeyValueReader, accountHash common.Hash, path []byte) []byte {
	data, _ := db.Get(storageTrieNodeKey(accountHash, path))
	return data
}
func storageTrieNodeKey(accountHash common.Hash, path []byte) []byte {
	buf := make([]byte, len(TrieNodeStoragePrefix)+common.HashLength+len(path))
	n := copy(buf, TrieNodeStoragePrefix)
	n += copy(buf[n:], accountHash.Bytes())
	copy(buf[n:], path)
	return buf
}
```

### `pathdb` vs `hashdb`
- 仔细研究了`pathdb`和`hashdb`的[区别](https://github.com/ethereum/go-ethereum/issues/23427)
- [path-based state scheme的具体实现和benchmark](https://github.com/ethereum/go-ethereum/pull/25963)
- 继续深入学习`pathdb`和`hashdb`的相关代码
    - hashdb实际上存的是hash=> value，所以实际上不同合约地址如果存储相同的storage，那么他们有可能会引用相同的key及相应的数据(即使他们的contract地址不一样!，因为不是按照前缀地址存储数据的)，这样导致不好删除数据，即使一个合约destroy了，他内部的hash对应的数据可能被其他合约引用，导致没法直接删除，所以即使是full node也可能会保存不需要的过期数据(比如hash对应的parent root已经被gc了)
    - pathdb则在具体的合约上加了contract address前缀，这样不同的合约确保不会引用相同的hash对应的数据，更容易prune，所以在内存和实际数据库中(fullnode)只需要维护一个trie即可
    - 因此pathdb没法做archive node，因为他需要维护所有的增量数据，这个成本很高


## rewind and import chain
stop CL first
```
# set maxPeers = 1, and use trusted peer
rm dump.dat (每次export之后的数据是追加的，因此需要先remove掉)
geth export  --datadir ./geth_full/snap/ dump.dat start+1 end 

geth attach ./geth_full/geth.ipc
num=22250601
debug.setHead('0x'+(num).toString(16))  # hex formated blocknumber with ""
for ( p of admin.peers) {admin.removePeer(p.enode)}; console.log(admin.peers.length)

# 和engine API一样，核心都是调用的insertChain，不过需要确认的是engine API里调用的block.Hash的时间占用
geth_std import --nocompaction  --cache.noprefetch --datadir ./geth_snap/ --snapshot false --pprof.cpuprofile cpu1.prof   --go-execution-trace trace1.out dump.dat
 geth   --cache.noprefetch   --pprof.cpuprofile cpu1.prof   --go-execution-trace ./trace1.out --snapshot --datadir ./geth_snap import --nocompaction true   dump.100.dat
geth_std import --nocompaction  --cache.noprefetch --datadir ./geth_full/ dump.dat
geth import --nocompaction  --cache.noprefetch --datadir ./geth_full/ dump.dat
```

## metric


### Execution
- accountRead
- storageRead
- blockExecution

### Validation
- Account/Storage Update
- AccountHash
- BlockValidation计算

### Commit
- Account/Storage Commits:并发执行，把更改后的状态保存到数据库
- TrieDBCommits: 内存中会保存trie diff
- Snapshot commit:
- BlockWriteTime: 写区块，receipts等时间

### Total: BlockInsertTime