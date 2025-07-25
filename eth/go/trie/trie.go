package trie

import (
	"bytes"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

type Kind int

const (
	LEAF_NODE Kind = iota
	SHORT_NODE
	FULL_NODE
)

type Node struct {
	kind       Kind
	partialKey []byte
	data       []byte
}

func (n *Node) Equal(other Node) bool {
	return n.kind == other.kind && bytes.Equal(n.partialKey, other.partialKey) && bytes.Equal(n.data, other.data)
}

func (n *Node) String() string {
	return fmt.Sprintf("kind:%v, key:%x, data:%x", n.kind, n.partialKey, n.data)
}

type Key []byte
type backend interface {
	Get(key Key) ([]byte, error)
	Update(key Key, val []byte, blockNumber int)
	Revert(root common.Hash) error
}

type Database struct {
	backend backend
}

func (d *Database) Get(key Key) ([]byte, error) {
	switch b := d.backend.(type) {
	case *PathDB:
		{
			return b.Get(key)
		}
	}
	return nil, nil
}

func (d *Database) Update(key Key, val []byte, blockNumber int) {
	switch b := d.backend.(type) {
	case *PathDB:
		{
			b.Update(key, val, blockNumber)
		}
	}
}

func (d *Database) Revert(root common.Hash) error {
	switch b := d.backend.(type) {
	case *PathDB:
		{
			return b.Revert(root)
		}
	}
	return nil
}
