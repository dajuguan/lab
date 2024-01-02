package main

import (
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/crypto/sha3"
)

func Keccak256(data ...[]byte) []byte {
	b := make([]byte, 32)
	d := sha3.New256().(crypto.KeccakState)
	for _, b := range data {
		d.Write(b)
	}
	d.Read(b)
	return b
}

func main() {
	buf := []byte("CC")
	// A MAC with 32 bytes of output has 256-bit security strength -- if you use at least a 32-byte-long key.
	hash := crypto.Keccak256Hash(buf) // now it's the legency standard
	fmt.Printf("%x\n", hash)
	newhash := Keccak256(buf)
	fmt.Printf("%x\n", newhash)
}
