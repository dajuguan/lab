package trie

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
)

func TestPathDBShortKey(t *testing.T) {
	pathdb := NewPathdb()

	key := []byte{'a', 'b', 'b', 'b'}
	val, _ := hex.DecodeString("d0")
	pathdb.Update(key, val, 0)
	_val, _ := pathdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{'b', 'b', 'b', 'b'}
	val, _ = hex.DecodeString("d1")
	pathdb.Update(key, val, 0)
	_val, _ = pathdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{'a', 'b', 'b', 'c'}
	val, _ = hex.DecodeString("d2")
	pathdb.Update(key, val, 0)
	_val, _ = pathdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{'b', 'c', 'd', 'd'}
	val, _ = hex.DecodeString("d3")
	pathdb.Update(key, val, 0)
	_val, _ = pathdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{'b', 'c', 'd', 'd'}
	val, _ = hex.DecodeString("d4")
	pathdb.Update(key, val, 0)
	_val, _ = pathdb.Get(key)
	assert.Equal(t, val, _val)

	fmt.Println("DB keys length:", len(pathdb.disk))
}

func TestPathDBLongKey(t *testing.T) {
	pathdb := NewPathdb()
	for i := range 100000 {
		key, _ := hex.DecodeString(fmt.Sprintf("%v", i))
		key = crypto.Keccak256(key)
		val := make([]byte, len(key))
		copy(val, key)

		pathdb.Update(key, val, 0)
	}

	i := 10002
	key, _ := hex.DecodeString(fmt.Sprintf("%v", i))
	key = crypto.Keccak256(key)
	val, _ := pathdb.Get(key)
	assert.Equal(t, key, val)

	i = 10002100
	key, _ = hex.DecodeString(fmt.Sprintf("%v", i))
	key = crypto.Keccak256(key)
	_, err := pathdb.Get(key)
	assert.Error(t, err)

	fmt.Println("DB keys length:", len(pathdb.disk))
}

func TestPathDBShortKeyReorg(t *testing.T) {
	pathdb := NewPathdb()

	bn := 0
	fmt.Println("DB keys length after block:", bn, len(pathdb.disk))
	// update blockNumber: the above state is stateAfter blockNumber 0
	bn = 1

	key := []byte{1, 0, 0, 0}
	val, _ := hex.DecodeString("d1")
	pathdb.Update(key, val, bn)
	_val, _ := pathdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{1, 1, 0, 0}
	val, _ = hex.DecodeString("d2")
	pathdb.Update(key, val, bn)
	_val, _ = pathdb.Get(key)
	assert.Equal(t, val, _val)

	key = []byte{1, 2, 1, 1}
	preVal, _ := hex.DecodeString("d3")
	pathdb.Update(key, preVal, bn)
	_val, _ = pathdb.Get(key)
	assert.Equal(t, preVal, _val)

	// update blockNumber: the above state is stateAfter blockNumber 1
	pathdb.setBlockNumber(1)
	fmt.Println("DB keys length after block:", bn, len(pathdb.disk))

	bn = 2
	key = []byte{1, 2, 1, 1}
	postVal, _ := hex.DecodeString("d4")
	pathdb.Update(key, postVal, bn)
	_val, _ = pathdb.Get(key)
	assert.Equal(t, postVal, _val)

	// update blockNumber: the above state is stateAfter blockNumber 2
	pathdb.setBlockNumber(2)
	fmt.Println("DB keys length after reverting toblock:", bn, len(pathdb.disk))

	// revert to state after block 1
	bn = 1
	fmt.Println("revert to block:", bn)
	pathdb.revert(bn)
	_val, _ = pathdb.Get(key)
	assert.Equal(t, preVal, _val)

	fmt.Println("DB keys length after reverting to block:", bn, len(pathdb.disk))

	// revert to state after block 0
	bn = 0
	fmt.Println("revert to block:", bn)
	pathdb.revert(bn)
	key = []byte{1, 1, 0, 0}
	_val, _ = pathdb.Get(key)
	assert.Equal(t, []byte(nil), _val)
	fmt.Println("DB keys length after reverting to block:", bn, len(pathdb.disk))
}

func TestPathDBLongKeyReorg(t *testing.T) {
	pathdb := NewPathdb()
	var indexToKey = func(bn, i int) int {
		return i*bn + bn
	}
	for bn := range 20 {
		for i := range 100000 {
			key, _ := hex.DecodeString(fmt.Sprintf("%v", indexToKey(bn, i)))
			key = crypto.Keccak256(key)
			val := make([]byte, len(key))
			copy(val, key)

			pathdb.Update(key, val, bn)
		}
		pathdb.setBlockNumber(bn)

		fmt.Println("DB keys length after block:", bn, len(pathdb.disk))
	}

	bn := 2
	pathdb.revert(bn)

	i := 521
	key, _ := hex.DecodeString(fmt.Sprintf("%v", indexToKey(bn, i)))
	key = crypto.Keccak256(key)
	val, _ := pathdb.Get(key)
	assert.Equal(t, key, val)

	fmt.Println("DB keys length after block:", bn, len(pathdb.disk))
}
