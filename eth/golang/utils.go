package main

import (
	"crypto/rand"
	"encoding/hex"
	"math/big"
)

func GenerateRandomHex(length int, randLen bool) (string, error) {
	if randLen {
		nBig, err := rand.Int(rand.Reader, big.NewInt(int64(length)))
		if err != nil {
			return "", err
		}
		length = int(nBig.Int64())
	}

	bytes := make([]byte, length/2)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
