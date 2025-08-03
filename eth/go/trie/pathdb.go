// references:
// how pathdb works: https://github.com/PublicGoodsNode/ethereum-code-walkthrough/blob/main/Geth%E6%BA%90%E7%A0%81%E7%B3%BB%E5%88%97%EF%BC%9A%E5%AD%98%E5%82%A8%E8%AE%BE%E8%AE%A1%E5%8F%8A%E5%AE%9E%E7%8E%B0.md#%E5%AE%9E%E4%BE%8B%E5%85%A8%E8%8A%82%E7%82%B9%E4%B8%8B-hashdb-%E5%92%8C-pathdb-%E5%AE%9E%E9%99%85%E8%90%BD%E7%9B%98%E5%AF%B9%E6%AF%94
// update: https://github.com/ethereum/go-ethereum/blob/5572f2ed229ff1f3aa0967e32af320a4b01be16d/trie/trie.go#L327
// database commit: https://github.com/ethereum/go-ethereum/blob/b6c62d5887e2bea38df0c294077d30ca0f6a3c97/triedb/pathdb/flush.go#L39
// revert: https://github.com/ethereum/go-ethereum/blob/0eb2eeea908d654b971249142fcbb735ba2c6923/triedb/pathdb/disklayer.go#L290
package trie

import (
	"bytes"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

/* pathdb design
setBlockNumber(1)
a）initial trie
root = fullNode("")

b) put(h0,d0), h0=abbb (key with 4byes)
root.children[a]=n0
n0=leaf{key:"bbb", val:d0}
key ''  => root (fullNode{})
key 'a' => n0

history[0]['a'] = null // for reverting

c) put(h1,d1), h1=bbbb
root.children[b]=n1
n1=leaf{key:"bbb", val:d1}
key ''  => root (fullNode{})
key 'a' => n0 (leaf{key:"bbb", val:d0})
key 'b' => n1 (leaf{key:"bbb", val:d1})

history[0]['a'] = null

history[0]['b'] = null

setBlockNumber(2)
d) put(h2,d2), h2=abbc
root.children[a] = n2
n2 = shortNode(n3)
n3 = fullNode
n3.children['b']= n4 = leafNode{key:'', val: d0}
n3.children['c']= n5 = leafNode{key:'', val: d2}

key ''  => root (fullNode{})
key 'a' => n2 (shortNode{key:'bb'})
key 'b' => n1 (leaf{key:"bbb", val:d1})
key 'abb' => n3 (fullNode{})
key 'abbb' => n4 (leafNode{key:'', val: d0})
key 'abbc' => n5 (leafNode{key:'', val: d2})

history[0]['a'] = null
history[0]['b'] = null

history[1]['a'] = leaf{key:"bbb", val:d0}
history[1]['abb'] = null
history[1]['abbb'] = null
history[1]['abbc'] = null

e) put(h3,d3), h3=bcdd
root.children[b] = n6
n6 = fullNode
n6.children['b']=n7
n6.children['c']=n8
n7=leafNode{key:'bb', val: d1}
n8=leafNode{key:'dd', val: d3}

key ''  => root (fullNode{})
key 'a' => n2 (shortNode{key:'bb'})
key 'b' => n6 (fullNode{})
key 'abb' => n3 (fullNode{})
key 'abbb' => n4 (leafNode{key:'', val: d0})
key 'abbc' => n5 (leafNode{key:'', val: d2})
key 'bb' => n7 (leafNode{key:'bb', val: d1})
key 'bc' => n8 (leafNode{key:'dd', val: d3})

history[0]['a'] = null
history[0]['b'] = null
history[1]['a'] = leaf{key:"bbb", val:d0}
history[1]['abb'] = null
history[1]['abbb'] = null
history[1]['abbc'] = null

history[1]['b'] = leaf{key:"bbb", val:d1}
history[1]['bb'] = null
history[1]['bc'] = null


f) put(h3,d4), h3=bcdd
n6.children['c']=n9
n9=leafNode{key:'dd', val: d4}

key ''  => root (fullNode{})
key 'a' => n2 (shortNode{key:'bb'})
key 'b' => n6 (fullNode{})
key 'abb' => n3 (fullNode{})
key 'abbb' => n4 (leafNode{key:'', val: d0})
key 'abbc' => n5 (leafNode{key:'', val: d2})
key 'bb' => n7 (leafNode{key:'bb', val: d1})
key 'bc' => n9 (leafNode{key:'dd', val: d4})

history[0]['a'] = null
history[0]['b'] = null
history[1]['a'] = leaf{key:"bbb", val:d0}
history[1]['abb'] = null
history[1]['abbb'] = null
history[1]['abbc'] = null
history[1]['b'] = leaf{key:"bbb", val:d1}
history[1]['bb'] = null
history[1]['bc'] = null

g) revert to state after blockNumber 1
apply history states with blockNumber>=1 recursively; if val is null delete; else apply

key ''  => root (fullNode{})
key 'a' => leaf{key:"bbb", val:d0}
key 'b' => leaf{key:"bbb", val:d1}

history[0]['a'] = null
history[0]['b'] = null
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
				parentNode := d.newFullNode(prefix, blockNumber)
				d.newLeafNode(append(prefix, key[0]), key[1:], val, blockNumber)
				d.newLeafNode(append(prefix, root.partialKey[0]), root.partialKey[1:], root.data, blockNumber)
				return parentNode
			}

			if matchLen == len(root.partialKey) {
				prefix = append(prefix, root.partialKey[:matchLen]...)
				root, ok := d.disk[string(prefix)]
				if !ok {
					panic(fmt.Errorf("full node at path:%x not exist", prefix))
				}
				return d._update(root, prefix, key[matchLen:], val, blockNumber)
			}
			parentNode := d.newShortNode(prefix, root.partialKey[:matchLen], blockNumber)
			prefix = append(prefix, root.partialKey[:matchLen]...)
			d.newFullNode(prefix, blockNumber)
			prefix = append(prefix, key[matchLen])
			d.newLeafNode(prefix, key[matchLen+1:], val, blockNumber)
			return parentNode
		}
	case LEAF_NODE:
		{
			matchLen := prefixLen(root.partialKey, key)

			// if leafNode's partialKey lengt is 0, we should check this first
			if matchLen == len(root.partialKey) {
				return d.newLeafNode(prefix, root.partialKey, val, blockNumber)
			}
			if matchLen == 0 {
				parentNode := d.newFullNode(prefix, blockNumber)
				d.newLeafNode(append(prefix, root.partialKey[0]), root.partialKey[1:], root.data, blockNumber)
				prefix = append(prefix, key[0])
				d.newLeafNode(prefix, key[1:], val, blockNumber)
				return parentNode
			}
			parentNode := d.newShortNode(prefix, root.partialKey[:matchLen], blockNumber)
			prefix = append(prefix, root.partialKey[:matchLen]...)
			d.newFullNode(prefix, blockNumber)
			d.newLeafNode(append(prefix, key[matchLen]), key[matchLen+1:], val, blockNumber)
			d.newLeafNode(append(prefix, root.partialKey[matchLen]), root.partialKey[matchLen+1:], root.data, blockNumber)
			return parentNode
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
