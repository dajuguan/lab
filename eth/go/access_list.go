package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"sync"

	"github.com/ethereum/go-ethereum/rpc"
)

type AccessList struct {
	Address     string   `json:"address"`
	StorageKeys []string `json:"storageKeys"`
}

type Config struct {
	RPCURL string `json:"RPC_URL"`
}

type Block struct {
	Number string   `json:"number"`
	Hashes []string `json:"transactions"`
}

// type Transaction struct {
// 	hash string `json:"hash"`
// }

func fetchBlockTxHashes(client *rpc.Client, blockNumber int64) ([]string, error) {

	var raw json.RawMessage
	err := client.Call(&raw, "eth_getBlockByNumber", fmt.Sprintf("0x%x", blockNumber), false)
	if err != nil {
		return nil, err
	}
	// Decode header and transactions.
	var block *Block
	if err := json.Unmarshal(raw, &block); err != nil {
		return nil, err
	}

	return block.Hashes, nil
}

func getTxAccessList(client *rpc.Client, txHash string) ([]AccessList, int, int, error) {
	var result map[string]struct {
		Storage map[string]interface{} `json:"storage"`
	}
	err := client.Call(&result, "debug_traceTransaction", txHash, map[string]interface{}{
		"tracer":       "prestateTracer",
		"tracerConfig": map[string]bool{"disableCode": true},
	})
	if err != nil {
		return nil, 0, 0, err
	}

	var acl []AccessList
	storageKeysCount := 0
	for addr, data := range result {
		keys := []string{}
		for key := range data.Storage {
			keys = append(keys, key)
		}
		storageKeysCount += len(keys)
		acl = append(acl, AccessList{Address: addr, StorageKeys: keys})
	}
	return acl, len(acl), storageKeysCount, nil
}

func main() {
	// Load RPC URL from .env file
	configData, err := ioutil.ReadFile("../.env")
	if err != nil {
		log.Fatalf("Failed to read .env file: %v", err)
	}
	var config Config
	if err := json.Unmarshal(configData, &config); err != nil {
		log.Fatalf("Failed to parse .env file: %v", err)
	}

	client, err := rpc.Dial(config.RPCURL)
	if err != nil {
		log.Fatalf("Failed to connect to Ethereum node: %v", err)
	}

	blockStart := int64(22010000)
	blockEnd := int64(22028159)

	var (
		totalAddrCount, totalStorageKeysCount int
		maxAddrInBlock, maxStorageKeysInBlock int
		maxSizeInBlock                        int
		txsCount                              int
	)

	var wg sync.WaitGroup
	var mu sync.Mutex

	for blockNumber := blockStart; blockNumber < blockEnd; blockNumber++ {
		txHashes, err := fetchBlockTxHashes(client, blockNumber)
		if err != nil {
			log.Printf("Failed to fetch block %d: %v", blockNumber, err)
			continue
		}
		txsCount += len(txHashes)

		sizeInBlock := 0
		storageKeysInBlock := 0
		addrInBlock := 0

		for _, txHash := range txHashes {
			wg.Add(1)
			go func(txHash string) {
				defer wg.Done()
				_, addrCount, storageKeysCount, err := getTxAccessList(client, txHash)
				if err != nil {
					log.Printf("Failed to get access list for tx %s: %v", txHash, err)
					return
				}

				mu.Lock()
				totalAddrCount += addrCount
				totalStorageKeysCount += storageKeysCount

				sizeInBlock += addrCount*20 + storageKeysCount*32
				addrInBlock += addrCount
				storageKeysInBlock += storageKeysCount
				mu.Unlock()
			}(txHash)
		}

		wg.Wait()

		mu.Lock()
		if sizeInBlock > maxSizeInBlock {
			maxSizeInBlock = sizeInBlock
			maxAddrInBlock = addrInBlock
			maxStorageKeysInBlock = storageKeysInBlock
		}
		mu.Unlock()
	}

	fmt.Printf("Average addr count: %d\n", int64(totalAddrCount)/(blockEnd-blockStart))
	fmt.Printf("Average storage keys count: %d\n", int64(totalStorageKeysCount)/(blockEnd-blockStart))
	fmt.Printf("Max addr count in a block: %d\n", maxAddrInBlock)
	fmt.Printf("Max storage keys count in a block: %d\n", maxStorageKeysInBlock)
	fmt.Printf("Total txs: %d\n", txsCount)
}
