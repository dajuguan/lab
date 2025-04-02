// Here nibbles are not used to represent the key, the key is simply a string of hex characters.
// so odd/even encoded path is not considered.

/*
refs: https://github.com/Blockchain-for-Developers/merkle-patricia-trie/blob/master/src/trie.py#L300
*/

package main

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

type MPTTrie struct {
	root TrieNode
}

type TrieNode interface {
	Hash() string
}

type (
	EmptyNode  struct{}
	BranchNode struct {
		children [16]TrieNode
		val      string
	}
	LeafNode struct {
		key string
		val string
	}
	ExtentionNode struct {
		key  string
		next TrieNode
	}
)

func (n *EmptyNode) Hash() string {
	return ""
}

func (n *BranchNode) Hash() string {
	hash := sha256.New()
	for _, child := range n.children {
		if child != nil {
			hash.Write([]byte(child.Hash()))
		}
	}
	hash.Write([]byte(n.val))
	return string(hash.Sum(nil))
}

func (n *LeafNode) Hash() string {
	hash := sha256.Sum256([]byte(n.key + n.val))
	return string(hash[:])
}

func (n *ExtentionNode) Hash() string {
	hash := sha256.Sum256([]byte(n.key + n.next.Hash()))
	return string(hash[:])
}

func (mpt *MPTTrie) Insert(key, value string) {
	DPrintln("=====insert key:", key, "value:", value)
	if mpt.root == nil {
		mpt.root = &LeafNode{key: key, val: value}
	} else {
		mpt.root = mpt.insertNode(mpt.root, key, value)
	}
	DPrintf("rootHash:%x\n", mpt.root.Hash())
}

func commonPrefixLen(a, b string) int {
	l := len(a)
	if len(b) < l {
		l = len(b)
	}
	for i := 0; i < l; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return l
}

func str2Int(s byte) int {
	val, _ := strconv.ParseInt(string(s), 16, 64)
	return int(val)
}

func (mpt *MPTTrie) insertNode(node TrieNode, key, value string) TrieNode {
	if node == nil {
		return &LeafNode{key: key, val: value}
	}
	switch n := node.(type) {
	case *LeafNode:
		DPrintln("Leafnode key:", n.key, "insert key:", key, "value:", n.val)
		if key == n.key {
			n.val = value
			return n
		}
		// transform n to extention node and and branch nodes
		commonPrefixLen := commonPrefixLen(key, n.key)
		commonPrefix := key[:commonPrefixLen]
		newBranch := &BranchNode{}
		// add node in slot
		if len(n.key) > commonPrefixLen {
			newBranch.children[str2Int(n.key[commonPrefixLen])] = &LeafNode{key: n.key[commonPrefixLen+1:], val: n.val}
		} else {
			// branch node's value slot
			newBranch.val = n.val
		}
		if len(key) > commonPrefixLen {
			newBranch.children[str2Int(key[commonPrefixLen])] = &LeafNode{key: key[commonPrefixLen+1:], val: value}
		} else {
			newBranch.val = value
		}
		// if commonPrefixLen == 0, leafNode => branch node
		// else branchNode => extention node
		if commonPrefixLen == 0 {
			return newBranch
		} else {
			return &ExtentionNode{key: commonPrefix, next: newBranch}
		}

	case *BranchNode:
		DPrintln("BranchNode:", "insert key:", key, "value:", n.val)
		if len(key) == 0 {
			// insert to value slot
			n.val = value
		} else {
			// insert to child's slot
			index := str2Int(key[0])
			var updatedNode TrieNode
			if len(key) > 1 {
				updatedNode = mpt.insertNode(n.children[index], key[1:], value)
			} else {
				updatedNode = mpt.insertNode(n.children[index], "", value)
			}
			n.children[index] = updatedNode
		}
		return n
	case *ExtentionNode:
		DPrintln("Extension key:", n.key, "insert key:", key, "value:", n.next)
		// if index < common prefix len, create a branch node;
		// else insert to extention node
		commonPrefixLen := commonPrefixLen(key, n.key)
		if commonPrefixLen < len(n.key) {
			newBranch := &BranchNode{}
			newBranch.children[str2Int(n.key[commonPrefixLen])] = n.next
			// check all commonPrefixLen + 1 ?
			if len(key) == commonPrefixLen {
				if commonPrefixLen == 0 { // coomonPrefixLen == 0, go to current level
					newBranch.val = value
				} else { // coomonPrefixLen > 0, split to next level
					newBranch.children[str2Int(key[commonPrefixLen])] = &LeafNode{key: "", val: value}
				}
			} else { // len(key) > commonPrefixLen
				newBranch.children[str2Int(key[commonPrefixLen])] = &LeafNode{key: key[commonPrefixLen+1:], val: value}
			}
			n.key = n.key[:commonPrefixLen]
			// extension => branchNode
			return newBranch
		} else {
			updatedNode := mpt.insertNode(n.next, key[commonPrefixLen:], value)
			n.next = updatedNode
		}
		return n
	}
	return nil
}

func (mpt *MPTTrie) Traverse(root TrieNode, path string) {
	if root == nil {
		return
	}
	switch n := root.(type) {
	case *LeafNode:
		DPrintln("LeafNode:", path+n.key, n.val)
	case *BranchNode:
		for i, child := range n.children {
			if child != nil {
				mpt.Traverse(child, path+strconv.FormatInt(int64(i), 16))
			}
		}
		if n.val != "" {
			DPrintln("BranchNode:", path, n.val)
		}
	case *ExtentionNode:
		mpt.Traverse(n.next, path+n.key)
	}
}

func (mpt *MPTTrie) Get(root TrieNode, key string, count *int) (string, error) {
	*count += 1
	if root == nil {
		return "", fmt.Errorf("key not found")
	}
	switch n := root.(type) {
	case *LeafNode:
		if n.key == key {
			return n.val, nil
		}
		return "", fmt.Errorf("key not found")
	case *BranchNode:
		if len(key) == 0 {
			return n.val, nil
		}
		index := str2Int(key[0])
		if n.children[index] == nil {
			return "", fmt.Errorf("key not found")
		} else {
			return mpt.Get(n.children[index], key[1:], count)
		}
	case *ExtentionNode:
		if commonPrefixLen(key, n.key) == len(n.key) {
			return mpt.Get(n.next, key[len(n.key):], count)
		}
		return "", fmt.Errorf("key not found")
	}
	return "", fmt.Errorf("key not found")
}

func TestMPTTrie(t *testing.T) {
	mpt := &MPTTrie{}
	DPrintln("Adding nodes=====================================================")
	mpt.Insert("a711355", "45.0 ETH")
	mpt.Insert("a77d337", "1.00 WEI")
	mpt.Insert("a7f9365", "1.1  ETH")
	mpt.Insert("a77d397", "0.12 ETH")
	DPrintln("Traversing=====================================================")
	mpt.Traverse(mpt.root, "")
	DPrintln("Adding nodes=====================================================")
	mpt.Insert("abc", "123")
	mpt.Insert("def", "456")
	mpt.Insert("ab", "789")
	mpt.Insert("de", "012")
	mpt.Insert("a", "345")
	mpt.Insert("d", "678")
	mpt.Insert("abf", "901")
	mpt.Insert("deh", "234")
	DPrintln("Traversing=====================================================")
	mpt.Traverse(mpt.root, "")
}

func TestMPTTrieGet(t *testing.T) {
	mpt := &MPTTrie{}
	length := 2000
	hexStr := ""
	for i := 0; i < length; i++ {
		hexStr, _ = GenerateRandomHex(80, true)
		mpt.Insert(hexStr, strconv.Itoa(i))
	}
	key := hexStr
	count := 0
	value, err := mpt.Get(mpt.root, key, &count)
	require.NoError(t, err)
	fmt.Println("found key:", key, ", value:", value, "in steps:", count)

	key, _ = GenerateRandomHex(81, false)
	count = 0
	value, err = mpt.Get(mpt.root, key, &count)
	require.Error(t, err)
	fmt.Println("key:", key, ",", err, "in steps:", count)
}
