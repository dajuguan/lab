package trie

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

func TestPathDBShortKey(t *testing.T) {
	pathdb := NewPathdb()

	key := []byte{1, 0, 0, 0}
	val, _ := hex.DecodeString("0a")
	pathdb.Update(key, val, 0)
	fmt.Printf("key:%x, val:%x \n", key, pathdb.Get(key))

	key = []byte{1, 1}
	val, _ = hex.DecodeString("01")
	pathdb.Update(key, val, 0)
	fmt.Printf("key:%x, val:%x \n", key, pathdb.Get(key))

	key = []byte{1, 2}
	val, _ = hex.DecodeString("0102")
	pathdb.Update(key, val, 0)
	fmt.Printf("key:%x, val:%x \n", key, pathdb.Get(key))

	key = []byte{1, 2, 3}
	val, _ = hex.DecodeString("010203")
	pathdb.Update(key, val, 0)
	fmt.Printf("key:%x, val:%x \n", key, pathdb.Get(key))

	key = []byte{1, 2, 4}
	val, _ = hex.DecodeString("010204")
	pathdb.Update(key, val, 0)
	fmt.Printf("key:%x, val:%x \n", key, pathdb.Get(key))

	key = []byte{1, 0, 0, 0}
	val, _ = hex.DecodeString("aa")
	pathdb.Update(key, val, 0)
	fmt.Printf("key:%x, val:%x \n", key, pathdb.Get(key))

	key = []byte{2, 0, 0, 0}
	val, _ = hex.DecodeString("0b")
	pathdb.Update(key, val, 0)
	fmt.Printf("key:%x, val:%x \n", key, pathdb.Get(key))

	key = []byte{1, 2, 3, 0}
	val, _ = hex.DecodeString("0c")
	pathdb.Update(key, val, 0)
	fmt.Printf("key:%x, val:%x \n", key, pathdb.Get(key))

	key = []byte{1, 2, 3, 4}
	val, _ = hex.DecodeString("0d")
	pathdb.Update(key, val, 0)
	fmt.Printf("key:%x, val:%x \n", key, pathdb.Get(key))

	key = []byte{1, 2, 4, 0}
	val, _ = hex.DecodeString("0e")
	pathdb.Update(key, val, 0)
	fmt.Printf("key:%x, val:%x \n", key, pathdb.Get(key))

	key = []byte{1, 2, 4, 0}
	val, _ = hex.DecodeString("ee")
	pathdb.Update(key, val, 0)
	fmt.Printf("key:%x, val:%x \n", key, pathdb.Get(key))

	fmt.Println("DB keys length:", len(pathdb.disk))
}

func TestPathDBLoongKey(t *testing.T) {
	pathdb := NewPathdb()

	key := crypto.Keccak256([]byte{1})
	val, _ := hex.DecodeString("0a")
	pathdb.Update(key, val, 0)
	fmt.Printf("key:%x, val:%x \n", key, pathdb.Get(key))

	key = crypto.Keccak256([]byte{1})
	val, _ = hex.DecodeString("0b")
	pathdb.Update(key, val, 0)
	fmt.Printf("key:%x, val:%x \n", key, pathdb.Get(key))

	key = crypto.Keccak256([]byte{2})
	val, _ = hex.DecodeString("02")
	pathdb.Update(key, val, 0)
	fmt.Printf("key:%x, val:%x \n", key, pathdb.Get(key))

	key = crypto.Keccak256([]byte{3})
	val, _ = hex.DecodeString("03")
	pathdb.Update(key, val, 0)
	fmt.Printf("key:%x, val:%x \n", key, pathdb.Get(key))

	fmt.Println("DB keys length:", len(pathdb.disk))
}
