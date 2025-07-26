package trie

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
)

func TestHashDBShortKey(t *testing.T) {
	hashdb := NewHashDB()

	key := []byte{1, 0, 0, 0}
	val, _ := hex.DecodeString("0a")
	hashdb.Update(key, val, 0)

	_val, _ := hashdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{1, 1, 0, 0}
	val, _ = hex.DecodeString("01")
	hashdb.Update(key, val, 0)
	_val, _ = hashdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{1, 2, 1, 1}
	val, _ = hex.DecodeString("0102")
	hashdb.Update(key, val, 0)
	_val, _ = hashdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{1, 2, 3, 4}
	val, _ = hex.DecodeString("010203")
	hashdb.Update(key, val, 0)
	_val, _ = hashdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{1, 2, 3, 3}
	val, _ = hex.DecodeString("010204")
	hashdb.Update(key, val, 0)
	_val, _ = hashdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{1, 0, 0, 0}
	val, _ = hex.DecodeString("aa")
	hashdb.Update(key, val, 0)
	_val, _ = hashdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{2, 0, 0, 0}
	val, _ = hex.DecodeString("0b")
	hashdb.Update(key, val, 0)
	_val, _ = hashdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{1, 2, 3, 0}
	val, _ = hex.DecodeString("0c")
	hashdb.Update(key, val, 0)
	_val, _ = hashdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{1, 2, 3, 4}
	val, _ = hex.DecodeString("0d")
	hashdb.Update(key, val, 0)
	_val, _ = hashdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{1, 2, 4, 0}
	val, _ = hex.DecodeString("0e")
	hashdb.Update(key, val, 0)
	_val, _ = hashdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{1, 2, 4, 0}
	val, _ = hex.DecodeString("ee")
	hashdb.Update(key, val, 0)
	_val, _ = hashdb.Get(key)
	assert.Equal(t, val, _val)

	fmt.Println("DB keys length:", len(hashdb.disk))
}

func TestHashDBLongKey(t *testing.T) {
	hashdb := NewPathdb()
	for i := range 200000 {
		key, _ := hex.DecodeString(fmt.Sprintf("%v", i))
		key = crypto.Keccak256(key)
		val := make([]byte, len(key))
		copy(val, key)

		hashdb.Update(key, val, 0)
	}

	i := 1020
	key, _ := hex.DecodeString(fmt.Sprintf("%v", i))
	key = crypto.Keccak256(key)
	val, _ := hashdb.Get(key)
	assert.Equal(t, key, val)

	i = 521
	key, _ = hex.DecodeString(fmt.Sprintf("%v", i))
	key = crypto.Keccak256(key)
	val, _ = hashdb.Get(key)
	assert.Equal(t, key, val)

	i = 1000000000
	key, _ = hex.DecodeString(fmt.Sprintf("%v", i))
	key = crypto.Keccak256(key)
	_, err := hashdb.Get(key)
	assert.Error(t, err)

	fmt.Println("DB keys length:", len(hashdb.disk))
}

func TestHashDBShortKeyReorg(t *testing.T) {
	hashdb := NewHashDB()

	key := []byte{1, 0, 0, 0}
	val, _ := hex.DecodeString("0a")
	hashdb.Update(key, val, 0)

	_val, _ := hashdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{1, 1, 0, 0}
	val, _ = hex.DecodeString("01")
	hashdb.Update(key, val, 0)
	_val, _ = hashdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{1, 2, 1, 1}
	val, _ = hex.DecodeString("0102")
	hashdb.Update(key, val, 0)
	_val, _ = hashdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{1, 2, 3, 4}
	val, _ = hex.DecodeString("010203")
	hashdb.Update(key, val, 0)
	_val, _ = hashdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{1, 2, 3, 3}
	val, _ = hex.DecodeString("010204")
	hashdb.Update(key, val, 0)
	_val, _ = hashdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{1, 0, 0, 0}
	val, _ = hex.DecodeString("aa")
	hashdb.Update(key, val, 0)
	_val, _ = hashdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{2, 0, 0, 0}
	val, _ = hex.DecodeString("0b")
	hashdb.Update(key, val, 0)
	_val, _ = hashdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{1, 2, 3, 0}
	val, _ = hex.DecodeString("0c")
	hashdb.Update(key, val, 0)
	_val, _ = hashdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{1, 2, 3, 4}
	val, _ = hex.DecodeString("0d")
	hashdb.Update(key, val, 0)
	_val, _ = hashdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{1, 2, 4, 0}
	prevVal, _ := hex.DecodeString("0e")
	hashdb.Update(key, prevVal, 0)
	_val, _ = hashdb.Get(key)
	assert.Equal(t, prevVal, _val)

	oldRoot := hashdb.root

	key = []byte{1, 2, 4, 0}
	val, _ = hex.DecodeString("ee")
	hashdb.Update(key, val, 0)
	_val, _ = hashdb.Get(key)
	assert.Equal(t, val, _val)

	// revert to root
	hashdb.Revert(oldRoot)
	_val, _ = hashdb.Get(key)
	assert.Equal(t, prevVal, _val)

	fmt.Println("DB keys length:", len(hashdb.disk))
}

func TestHashDBLongKeyReorg(t *testing.T) {
	hashdb := NewHashDB()
	bnToRoot := map[int]common.Hash{}

	var indexToKey = func(bn, i int) int {
		return i*bn + bn
	}
	for bn := range 5 {
		for i := range 100000 {
			key, _ := hex.DecodeString(fmt.Sprintf("%v", indexToKey(bn, i)))
			key = crypto.Keccak256(key)
			val := make([]byte, len(key))
			copy(val, key)

			hashdb.Update(key, val, bn)
		}
		// cache old root
		bnToRoot[bn] = hashdb.root

		fmt.Println("DB keys length after block:", bn, len(hashdb.disk))
	}

	// revert to a block number
	bn := 2
	hashdb.Revert(bnToRoot[bn])

	i := 52
	key, _ := hex.DecodeString(fmt.Sprintf("%v", indexToKey(bn, i)))
	key = crypto.Keccak256(key)
	val, _ := hashdb.Get(key)
	assert.Equal(t, key, val)

	fmt.Println("DB keys length after block:", bn, len(hashdb.disk))
}
