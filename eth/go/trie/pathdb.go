package trie

import (
	"bytes"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

/* pathdb design
目的: get, insert(update体现不需要新增加节点), rollback（直接采用blockNumber)
定义好接口，再去实现
Trie
- get(path:string) -> value
- update(path, value, blocknumber)
- revert(root)

假设key的长度都一样
fullNode：key:path, value: {nodetype}
shortNode: key: fullpath, value: {nodetype, partialkey}
leafNode: key: path(为上一层的path), node:{nodetype, partialkey, value}

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
if val, ok = history[block-1][key]; update的时候如果不存在,设置为nil; 插入过程中如果碰到了完全一样的leaf节点，并且val为nil，那么将history更新为旧值;
操作过程中记录需要删除:重新插入一遍就可以了，需要记录下需要删除、增加/更新的path
*/

type PathDB struct {
	disk            map[string]Node
	historyPreState map[int]map[string]Node
	rootToBlockNum  map[common.Hash]int
	root            Node
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
	db.root = *db.newFullNode([]byte{})
	return db
}

func (d *PathDB) newFullNode(path Key) *Node {
	node := Node{
		kind: FULL_NODE,
	}
	d.disk[string(path)] = node
	return &node
}

func (d *PathDB) newShortNode(path, partialKey Key) *Node {
	node := Node{
		kind:       SHORT_NODE,
		partialKey: partialKey,
	}
	d.disk[string(path)] = node
	return &node
}

func (d *PathDB) newLeafNode(path, partialKey Key, val []byte) *Node {
	node := Node{
		kind:       LEAF_NODE,
		partialKey: partialKey,
		data:       val,
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
	rootKey := ""
	root := d.disk[rootKey]
	d._update(root, Key(rootKey), key, val, blockNumber)
}

func (d *PathDB) _update(root Node, prefix, key Key, val []byte, blockNumber int) *Node {
	switch root.kind {
	case FULL_NODE:
		{
			// fmt.Println("fullnode:", prefix, root)
			prefix = append(prefix, key[0])
			root, ok := d.disk[string(prefix)]
			// fmt.Println("ok", root, ok)
			if !ok {
				node := d.newLeafNode(prefix, key[1:], val)
				return node
			}
			return d._update(root, prefix, key[1:], val, blockNumber)
		}
	case SHORT_NODE:
		{
			matchLen := prefixLen(root.partialKey, key)
			if matchLen == 0 {
				d.newFullNode(prefix)
				// d.newShortNode(append(prefix, root.partialKey[0]), root.partialKey[1:])
				return d.newLeafNode(append(prefix, key[0]), key[1:], val)
			}

			if matchLen == len(root.partialKey) {
				prefix = append(prefix, root.partialKey[:matchLen]...)
				root, ok := d.disk[string(prefix)]
				if !ok {
					panic(fmt.Errorf("full node at path:%x not exist", prefix))
				}
				return d._update(root, prefix, key[matchLen:], val, blockNumber)
			}
			d.newShortNode(prefix, root.partialKey[:matchLen])
			prefix = append(prefix, root.partialKey[:matchLen]...)
			d.newFullNode(prefix)
			prefix = append(prefix, key[matchLen])
			return d.newLeafNode(prefix, key[matchLen+1:], val)
		}
	case LEAF_NODE:
		{
			matchLen := prefixLen(root.partialKey, key)

			// if leafNode's partialKey lengt is 0, we should check this first
			if matchLen == len(root.partialKey) {
				return d.newLeafNode(prefix, root.partialKey, val)
			}
			if matchLen == 0 {
				d.newFullNode(prefix)
				d.newLeafNode(append(prefix, root.partialKey[0]), root.partialKey[1:], root.data)
				prefix = append(prefix, key[0])
				return d.newLeafNode(prefix, key[1:], val)
			}
			d.newShortNode(prefix, root.partialKey[:matchLen])
			prefix = append(prefix, root.partialKey[:matchLen]...)
			d.newFullNode(prefix)
			prefix = append(prefix, key[matchLen])
			return d.newLeafNode(prefix, key[matchLen+1:], val)
		}
	default:
		panic(fmt.Sprintf("%T invalid node: %v", root, root))
	}
}

func (d *PathDB) Revert(root common.Hash) error {
	return nil
}

var _ backend = (*PathDB)(nil)
