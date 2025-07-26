package trie

import (
	"bytes"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

/* pathdb design
目的: get, insert(update体现不需要新增加节点), rollback（直接采用blockNumber)
定义好接口，再去实现
Trie: 简单采用2^8=256-ary trie, 以适合byte表示
DB: - key: path,
	- val:
		- nodetype(fullNode)
		- nodetype+partialKey(shortNode)
		- nodetype+partialKey+val(leafNode)

假设key的长度都一样,path均为node上一层的path
fullNode：key:path, value: {nodetype}
shortNode: key: path, value: {nodetype, partialkey}
leafNode: key: path, node:{nodetype, partialkey, value}

trie的处理
insert: leaf存的是value，中间节点存的是下一层的两个path
- 1.从根节点开始往下遍历，找到公共前缀，如果是空节点，那么插入一个leaf节点，并更新root
- 2.如果是fullNode:
	- 插入key[0]的节点
- 2.如果是short节点（保存了key，以及下一个节点的key作为value)
	- 查找公共前缀
	- prefix=0:
		- 创建fullNode
			- 创建newNode: db
			- 将short节点、newNode插入到对应的children
	- prefix部分包含:
		- 更新prefix对应的shortNode的partialKey为: partialKey[:matchedLen]
		- 创建fullNode
			- db插入新fullNode, path=prefix+matched
			- db创建leafnode，path=prefix+matched+key[matchedLen], key=key[matchedLen+1], val=val
	- prefix完全包含:
		- db查找append shortNode的value中包含的key，对应的节点node
		- 往该节点查找insert(node， path - prefix, value)
- 3.如果是leaf节点：
	- 查找公共前缀：
		- prefix=0:
			- db创建新的fullnode, path=prefix
			- db创建新的leafNode, path=prefix+key[0], key=key[1:], val=val
		- prefix部分包含:
			- 缩短leafnode的key(更新父节点的value,db插入新shortnode)
			- 创建fullNode,更新shortNode的value为fullNode
				- 创建leafnode，db插入新节点
				- 将新shortnode插入作为其children, 无db
		-prefix全部包含，直接全部更新:
			- db直接更新对应的value

搞混了内存和DB中的操作
get:
1.直到找到leaf 节点，就可以了
## multi-version read

## revert
记录修改的数据在该区块之前的状态
直接把DB revert为原来的就可以了
*/

type PathDB struct {
	disk            map[string]Node
	historyPreState map[int]map[string]Node
	rootToBlockNum  map[common.Hash]int
	root            Node
	blockNumber     int
}

func NewPathdb() *PathDB {
	db := &PathDB{
		disk:            map[string]Node{},
		historyPreState: map[int]map[string]Node{},
		rootToBlockNum:  map[common.Hash]int{},
		root: Node{
			kind: FULL_NODE,
		},
	}
	bn := 0
	db.root = *db.newFullNode([]byte{}, bn)
	db.historyPreState[bn] = map[string]Node{}
	return db
}

func (d *PathDB) setBlockNumber(blockNumber int) {
	d.blockNumber = blockNumber
	if d.historyPreState[blockNumber] == nil {
		d.historyPreState[blockNumber] = map[string]Node{}
	}
}

func (d *PathDB) newFullNode(path Key, blockNumber int) *Node {
	node := Node{
		kind: FULL_NODE,
	}
	originNode := d.disk[string(path)]
	// Snapshot prestate only during updates, not during reverts.
	if blockNumber > d.blockNumber && !originNode.Equal(node) {
		_, ok := d.historyPreState[d.blockNumber][string(path)]
		if !ok {
			d.historyPreState[d.blockNumber][string(path)] = originNode
		}
	}
	d.disk[string(path)] = node
	return &node
}

func (d *PathDB) newShortNode(path, partialKey Key, blockNumber int) *Node {
	node := Node{
		kind:       SHORT_NODE,
		partialKey: partialKey,
	}
	originNode := d.disk[string(path)]
	// Snapshot prestate only during updates, not during reverts.
	if blockNumber > d.blockNumber && !originNode.Equal(node) {
		_, ok := d.historyPreState[d.blockNumber][string(path)]
		if !ok {
			d.historyPreState[d.blockNumber][string(path)] = originNode
		}
	}
	d.disk[string(path)] = node
	return &node
}

func (d *PathDB) newLeafNode(path, partialKey Key, val []byte, blockNumber int) *Node {
	node := Node{
		kind:       LEAF_NODE,
		partialKey: partialKey,
		data:       val,
	}
	originNode := d.disk[string(path)]

	// Snapshot prestate only during updates, not during reverts.
	if blockNumber > d.blockNumber && !originNode.Equal(node) {
		_, ok := d.historyPreState[d.blockNumber][string(path)]
		if !ok {
			d.historyPreState[d.blockNumber][string(path)] = originNode
		}
	}
	d.disk[string(path)] = node
	return &node
}

func (d *PathDB) Get(key Key) ([]byte, error) {
	node := d._get(d.root, key, 0)
	if node == nil {
		return nil, fmt.Errorf("key not found: %v", key)
	}
	return node.data, nil
}

func (d *PathDB) _get(originNode Node, path Key, pos int) *Node {
	if pos >= len(path) {
		node, ok := d.disk[string(path)]
		if !ok {
			return nil
		}
		return &node
	}

	switch originNode.kind {
	case FULL_NODE:
		{
			originNode = d.disk[string(path[:pos+1])]
			return d._get(originNode, path, pos+1)
		}
	case SHORT_NODE:
		{
			pos = pos + len(originNode.partialKey)
			originNode = d.disk[string(path[:pos])]
			return d._get(originNode, path, pos)
		}
	case LEAF_NODE:
		{
			if bytes.Equal(originNode.partialKey, path[pos:]) {
				return &originNode
			} else {
				return nil
			}
		}
	default:
		panic(fmt.Sprintf("%T invalid node: %v", originNode, originNode))
	}
}

func (d *PathDB) Update(key Key, val []byte, blockNumber int) {
	if blockNumber < d.blockNumber {
		panic("new blockNumber must > previous blockNumber")
	}

	rootKey := ""
	root := d.disk[rootKey]
	d._update(root, Key(rootKey), key, val, blockNumber)
}

func (d *PathDB) _update(root Node, prefix, key Key, val []byte, blockNumber int) *Node {
	switch root.kind {
	case FULL_NODE:
		{
			prefix = append(prefix, key[0])
			root, ok := d.disk[string(prefix)]
			if !ok {
				node := d.newLeafNode(prefix, key[1:], val, blockNumber)
				return node
			}
			return d._update(root, prefix, key[1:], val, blockNumber)
		}
	case SHORT_NODE:
		{
			matchLen := prefixLen(root.partialKey, key)
			if matchLen == 0 {
				d.newFullNode(prefix, blockNumber)
				return d.newLeafNode(append(prefix, key[0]), key[1:], val, blockNumber)
			}

			if matchLen == len(root.partialKey) {
				prefix = append(prefix, root.partialKey[:matchLen]...)
				root, ok := d.disk[string(prefix)]
				if !ok {
					panic(fmt.Errorf("full node at path:%x not exist", prefix))
				}
				return d._update(root, prefix, key[matchLen:], val, blockNumber)
			}
			d.newShortNode(prefix, root.partialKey[:matchLen], blockNumber)
			prefix = append(prefix, root.partialKey[:matchLen]...)
			d.newFullNode(prefix, blockNumber)
			prefix = append(prefix, key[matchLen])
			return d.newLeafNode(prefix, key[matchLen+1:], val, blockNumber)
		}
	case LEAF_NODE:
		{
			matchLen := prefixLen(root.partialKey, key)

			// if leafNode's partialKey lengt is 0, we should check this first
			if matchLen == len(root.partialKey) {
				return d.newLeafNode(prefix, root.partialKey, val, blockNumber)
			}
			if matchLen == 0 {
				d.newFullNode(prefix, blockNumber)
				d.newLeafNode(append(prefix, root.partialKey[0]), root.partialKey[1:], root.data, blockNumber)
				prefix = append(prefix, key[0])
				return d.newLeafNode(prefix, key[1:], val, blockNumber)
			}
			d.newShortNode(prefix, root.partialKey[:matchLen], blockNumber)
			prefix = append(prefix, root.partialKey[:matchLen]...)
			d.newFullNode(prefix, blockNumber)
			prefix = append(prefix, key[matchLen])
			return d.newLeafNode(prefix, key[matchLen+1:], val, blockNumber)
		}
	default:
		panic(fmt.Sprintf("%T invalid node: %v", root, root))
	}
}

// revert to state after blockNumber
func (d *PathDB) revert(targetBN int) error {
	if targetBN >= d.blockNumber {
		panic(fmt.Sprintf("target block number must be less than current blockNumber:%v", d.blockNumber))
	}
	for bn := d.blockNumber - 1; bn >= targetBN; bn-- {
		history := d.historyPreState[bn]

		for k, v := range history {
			if v.Equal(Node{}) {
				delete(d.disk, k)
			} else {
				d.disk[k] = v
			}
		}
	}
	d.setBlockNumber(targetBN)
	return nil
}

func (d *PathDB) Revert(root common.Hash) error {
	return nil
}

var _ backend = (*PathDB)(nil)
