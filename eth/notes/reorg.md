
- [pos reorg](https://barnabe.substack.com/p/pos-ethereum-reorg)
- [pectra incident](https://github.com/ethereum/pm/tree/master/Pectra)
- [consensus explained](https://www.youtube.com/watch?v=5gfNUVmX3Es)


## Geth reorg pathbased
1. DB schema
- disk DB
    - snapshotDisklayer: key:account address, value: account value
    - trieDisklayer: key: trie path nibbles, value: nodes
    - trieHistory: 
        - schema:
            - parentStateRoot
            - stateRoot
            - blocknumber
            - accounts: map[addr]data // state before state transition for blocknumber
            - storages: map[addr][slot]data // state before state transition for blocknumber
        - interfaces:
            - (h *history) readHistory(dbreader, id) -> (*history, error) // id: layer + blockNumber
            - (h *history) writeHistory(dbwriter, dl *trieDiffLayer)


- in memory cache:
    - trieDiskLayer
        - schema:
            - stateRootHash
            - database
            - dirtyBuffer // Dirty buffer to aggregate writes of nodes and states
        - interfaces:
            - (dl *trieDiskLayer) update(stateRoot, id, blocknumber, dirtyTrieNodes, originTrieNodes) -> *newTrieDifflayer
            - (dl *trieDiskLayer) commit(bottomTrieDiffLayer) -> *newTrieDiskLayer
                - writeHistory
                - writeDirtyNode to trie
            - (dl *trieDiskLayer) revert(*trieHistory) -> *trieDiskLayer
                - apply reverse state changes based on current state, write to disk or buffer
            - loadJournal(diskRoot) -> layer
                - loadDiskLayer
                - loadDifflayers
            - Journal(root common.Hash)
                - write all diff layers to a journal

    - trieDifflayer
        - schema:
            - stateRoot
            - dirtyTrieNodes:
                - accountNodes map[path]*trieNode
	            - storageNodes map[addr]map[path]*trieNode
            - parent: trie(diff/disk)Layer
            - originTrieNodes:  
                - accountNodes map[path][]byte
	            - storageNodes map[addr]map[path][]byte
        - interfaces:
            - (dl *trieDifflayer) node(ownerAddr, path, depth) -> *trieNode
            - (dl *trieDifflayer) update(stateRoot, id, blocknumber, dirtyTrieNodes, originTrieNodes) -> *newTrieDifflayer
            // writing the memory layer contents recursively into a buffer to be stored in the database as the layer journa
            - (dl *diffLayer) journal(dbwriter)
            // persist flushes the diff layer and all its parent layers to disk layer.
            - (dl *trieDifflayer) persist() -> newDiskLayer
    
    - trieLayerTree
        - schema:
            - layers: map[stateRoot]trie(Diff/Disk)layer
        - interfaces:
            - (t *trieLayerTree) get(stateRoot) -> *trieLayer
            - (t *trieLayerTree) add(root, parentRoot, blockNumber, dirtyTrieNodes, originTrieNodes)
            - (t *trieLayerTree) cap(root, layers int)

    - snapshotDisklayer
        - schema:
            - stateRoot
            - *database
            - *trieDB
        - interfaces:
            - (dl* snapshotDisklayer) Update(blockStateRoot, accounts, storages) -> *snapshotDifflayer

    - snapshotDifflayer
        - schema:
            - parent: snapshot(diff/disk)Layer
            - root: stateroot this difflayer belongs to
            - accounts: map[addr]data
            - storages: map[addr][slot]data
            - diffed: bloomfilter
        - interfaces(snapshot):
            - (dl* snapshotDifflayer) Update(blockStateRoot, accounts, storages) -> *snapshotDifflayer
            // recursively write the memory layer start from this layer into a buffer to be stored in the database as the snapshot journal
            - (dl* snapshotDifflayer) Journal(buffer *Buffer) -> baselayerStateRoot
    - snapshotLayerTree
        - schema:
            - layers: map[stateRoot]snapshot
        - interfaces:
            - New(diskdb, triedb, root) -> *snapshotLayerTree
                - loadSnapshot(diskdb, triedb, root) -> snapshotLayer
                - add the above layers
            - (t *snapshotTree) Get(blockStateRoot) -> snapshotDifflayer
            // Update adds a new snapshot into the tree, if that can be linked to an existing old parent.
            - (t* snapshotTree) Add(root, parentRoot, accounts, storages)
            // Cap traverses downwards the snapshot tree from a head block hash until the
            // number of allowed layers are crossed. All layers beyond the permitted number
            // are flattened downwards.
            - (t* snapshotTree) Cap(blockStateRoot, layers int)
            // Journal commits an entire diff hierarchy to disk into a single journal entry.
            - (t *Tree) Journal(root common.Hash) -> baselayerStateRoot
    - loadSnapshot

- journals for difflayers
    - snapshotTreeJournal
    - trieDiffTreeJournal

2. reorg的生命周期,定义rollback function(或者说其API就可以了)

- Beacon API: SetCanonical(b *block)
- triedb.Recover(parentRoot), and fetch pruned blocks until has parentRoot
    - Recover
        - h = readHistory()
        - trieDiskLayer.Revert(h)
    - Add b.parent to pruned blocks
    - insert pruned block
        - trigger snapshotDifflayer.update
        - trigger trieDifflayer.update


Acountaddr A(hashed): aaaaaa
Accountaddr B(hashed):  aaaabb
aaa
|
a,b,
| |
a  b