package main

import (
	"encoding/binary"
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb/pebble"
)

func main() {
	dbSize := 2000000000
	endfix := "pooled"
	cache := 512
	handles := 10000
	db, err := pebble.New(fmt.Sprintf("/root/now/lab/kv-db/bench_pebble_%d_%s_%d", dbSize, endfix, 0), cache, handles, "", false)
	if err != nil {
		panic(err)
	}

	i := 0
	buf := [8]byte{}
	binary.BigEndian.PutUint64(buf[:], uint64(i))
	key := crypto.Keccak256Hash(buf[:]).Bytes()

	value, err := db.Get(key)
	fmt.Println("value:", value)
}
