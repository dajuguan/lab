package trie

import (
	"bytes"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

/*
Trie: use 2^8=256-ary to represent trie arity with simple one byte
DB: - key: hash(path)
	- val:
		- fullNode: nodetype, val: {partialKey: hashOfNexNode's(nodetype, partialKey, val))}
		- shortNode: nodetype, partialKey, val: hashOfNexNode's(nodetype, partialKey, val))
		- leafNode: nodetype, partialKey, val: nodeVal
*/

type HashDB struct {
	disk           map[string]HashNode
	rootToBlockNum map[common.Hash]int
	root           common.Hash
}

type HashNode struct {
	kind       Kind
	partialKey []byte
	children   [256][]byte
	data       []byte
}

func (n *HashNode) hash() []byte {
	content := []byte{byte(n.kind)}
	content = append(content, n.partialKey...)
	for _, child := range n.children {
		if child != nil {
			content = append(content, child...)
		}
	}
	content = append(content, n.data...)
	return crypto.Keccak256(content)
}

func NewHashDB() *HashDB {
	db := &HashDB{
		disk:           map[string]HashNode{},
		rootToBlockNum: map[common.Hash]int{},
	}

	node := HashNode{
		kind:     FULL_NODE,
		children: [256][]byte{},
	}
	db.root = common.Hash(node.hash())
	db.disk[string(db.root[:])] = node
	return db
}

func (d *HashDB) Get(key Key) ([]byte, error) {
	rootNode := d.disk[string(d.root[:])]
	node := d._get(rootNode, key, 0)

	if node != nil {
		return node.data, nil
	} else {
		return nil, fmt.Errorf("node not found")
	}
}

func (d *HashDB) _get(originNode HashNode, path Key, pos int) *HashNode {
	if pos > len(path) {
		panic("node not found")
	}
	switch originNode.kind {
	case FULL_NODE:
		{
			childHash := originNode.children[path[pos]]
			return d._get(d.disk[string(childHash)], path, pos+1)
		}
	case SHORT_NODE:
		{
			pos = pos + len(originNode.partialKey)
			childHash := originNode.data
			return d._get(d.disk[string(childHash)], path, pos)
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

func (d *HashDB) newLeafNode(key Key, val []byte) *HashNode {
	node := HashNode{
		kind:       LEAF_NODE,
		partialKey: key,
		data:       val,
	}
	d.disk[string(node.hash())] = node
	return &node
}

func (d *HashDB) newFullNode(children [256][]byte) *HashNode {
	node := HashNode{
		kind:     FULL_NODE,
		children: children,
	}
	d.disk[string(node.hash())] = node
	return &node
}

func (d *HashDB) newShortNode(partialKey Key, val []byte) *HashNode {
	node := HashNode{
		kind:       SHORT_NODE,
		partialKey: partialKey,
		data:       val,
	}
	d.disk[string(node.hash())] = node
	return &node
}

func (d *HashDB) Update(key Key, val []byte, blockNumber int) {
	rootNode := d.disk[string(d.root[:])]
	newRootNode := d._update(rootNode, rootNode.partialKey, key, val)
	d.root = common.Hash(newRootNode.hash())
}

func (d *HashDB) _update(rootNode HashNode, prefix, key Key, val []byte) *HashNode {
	switch rootNode.kind {
	case FULL_NODE:
		{
			var newNode *HashNode
			childHash := rootNode.children[key[0]]
			// child not exist
			if len(childHash) == 0 {
				newNode = d.newLeafNode(key[1:], val)
				rootNode.children[key[0]] = newNode.hash()
				return d.newFullNode(rootNode.children)
			}
			child := d.disk[string(childHash)]
			newNode = d._update(child, append(prefix, key[0]), key[1:], val)

			// update parent node
			rootNode.children[key[0]] = newNode.hash()
			return d.newFullNode(rootNode.children)
		}
	case SHORT_NODE:
		{
			children := [256][]byte{}
			matchLen := prefixLen(rootNode.partialKey, key)
			if matchLen == 0 {
				newNode := d.newLeafNode(key[1:], val)
				// update parent node
				children[key[0]] = newNode.hash()
				if len(rootNode.partialKey) > 1 {
					newNode = d.newShortNode(rootNode.partialKey[1:], rootNode.data)
				} else {
					newNode = &rootNode
				}
				children[rootNode.partialKey[0]] = newNode.hash()
				return d.newFullNode(children)
			}

			if matchLen == len(rootNode.partialKey) {
				child, ok := d.disk[string(rootNode.data)]
				if !ok {
					panic(fmt.Errorf("full node at path:%x not exist", prefix))
				}
				newNode := d._update(child, append(prefix, key[:matchLen]...), key[matchLen:], val)
				// update parent node
				return d.newShortNode(rootNode.partialKey, newNode.hash())
			}

			newLeaf := d.newLeafNode(key[matchLen+1:], val)
			// update parent node
			children[key[matchLen]] = newLeaf.hash()
			newLeaf = d.newShortNode(rootNode.partialKey[matchLen+1:], rootNode.data)
			children[rootNode.partialKey[matchLen]] = newLeaf.hash()
			newNode := d.newFullNode(children)
			return d.newShortNode(rootNode.partialKey[:matchLen], newNode.hash())
		}
	case LEAF_NODE:
		{
			matchLen := prefixLen(rootNode.partialKey, key)
			// if leafNode's partialKey lengt is 0, we should check this first
			if matchLen == len(rootNode.partialKey) {
				return d.newLeafNode(rootNode.partialKey, val)
			}
			children := [256][]byte{}
			if matchLen == 0 {
				newLeaf := d.newLeafNode(key[1:], val)
				children[key[0]] = newLeaf.hash()
				newLeaf = d.newLeafNode(rootNode.partialKey[1:], rootNode.data)
				children[rootNode.partialKey[0]] = newLeaf.hash()
				return d.newFullNode(children)
			}
			newLeaf := d.newLeafNode(key[matchLen+1:], val)
			// update parent node
			children[key[matchLen]] = newLeaf.hash()
			newLeaf = d.newLeafNode(rootNode.partialKey[matchLen+1:], rootNode.data)
			children[rootNode.partialKey[matchLen]] = newLeaf.hash()
			newNode := d.newFullNode(children)
			return d.newShortNode(rootNode.partialKey[:matchLen], newNode.hash())
		}
	default:
		panic(fmt.Sprintf("%T invalid node: %v", rootNode, rootNode))
	}
}

func (d *HashDB) Revert(root common.Hash) error {
	d.root = root
	return nil
}

var _ backend = (*HashDB)(nil)
