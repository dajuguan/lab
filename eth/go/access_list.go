package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"sync"

	"github.com/ethereum/go-ethereum/rpc"
)

type AccessList struct {
	Address     string   `json:"address"`
	StorageKeys []string `json:"storage_keys"`
}

type Config struct {
	RPCURL string `json:"RPC_URL"`
}

type Block struct {
	// must be capitalized
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

func getTxAccessList(client *rpc.Client, txHash string) ([]AccessList, error) {
	var result map[string]struct {
		Storage map[string]interface{} `json:"storage"`
	}
	err := client.Call(&result, "debug_traceTransaction", txHash, map[string]interface{}{
		"tracer":       "prestateTracer",
		"tracerConfig": map[string]bool{"disableCode": true},
	})
	if err != nil {
		return nil, err
	}

	var acl []AccessList
	for addr, data := range result {
		keys := []string{}
		for key := range data.Storage {
			keys = append(keys, key)
		}
		acl = append(acl, AccessList{Address: addr, StorageKeys: keys})
	}
	return acl, nil
}

func deduplicateAndSortAccessList(acl []AccessList) ([]AccessList, int, int) {
	addressMap := make(map[string]map[string]struct{})
	for _, entry := range acl {
		if _, exists := addressMap[entry.Address]; !exists {
			addressMap[entry.Address] = make(map[string]struct{})
		}
		for _, key := range entry.StorageKeys {
			addressMap[entry.Address][key] = struct{}{}
		}
	}

	var deduplicatedACL []AccessList
	keysLen := 0
	for addr, keysMap := range addressMap {
		keys := make([]string, 0, len(keysMap))
		for key := range keysMap {
			keys = append(keys, key)
		}
		deduplicatedACL = append(deduplicatedACL, AccessList{Address: addr, StorageKeys: keys})
		keysLen += len(keys)
	}

	sort.Slice(deduplicatedACL, func(i, j int) bool {
		return deduplicatedACL[i].Address < deduplicatedACL[j].Address
	})

	return deduplicatedACL, len(deduplicatedACL), keysLen
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

	blockStart := int64(21973379)
	blockEnd := int64(21973479)

	var (
		totalAddrCount, totalStorageKeysCount int
		maxAddrInBlock, maxStorageKeysInBlock int
		maxSizeInBlock                        int
		txsCount                              int
	)

	allACL := make(map[int64][]AccessList)

	var wg sync.WaitGroup
	var mu sync.Mutex

	for blockNumber := blockStart; blockNumber < blockEnd; blockNumber++ {
		txHashes, err := fetchBlockTxHashes(client, blockNumber)
		if err != nil {
			log.Printf("Failed to fetch block %d: %v", blockNumber, err)
			continue
		}
		txsCount += len(txHashes)

		for _, txHash := range txHashes {
			wg.Add(1)
			go func(txHash string) {
				defer wg.Done()
				acl, err := getTxAccessList(client, txHash)
				if err != nil {
					log.Printf("Failed to get access list for tx %s: %v", txHash, err)
					return
				}
				mu.Lock()
				allACL[blockNumber] = append(allACL[blockNumber], acl...)
				mu.Unlock()
			}(txHash)
		}

		wg.Wait()
	}

	var addrInBlock, keysInBlock, sizeInBlock int
	// Deduplicate and sort allACL for each block
	for blockNumber := blockStart; blockNumber < blockEnd; blockNumber++ {
		allACL[blockNumber], addrInBlock, keysInBlock = deduplicateAndSortAccessList(allACL[blockNumber])

		addrInBlock = len(allACL[blockNumber])
		sizeInBlock = addrInBlock*20 + keysInBlock*32
		if sizeInBlock > maxSizeInBlock {
			maxSizeInBlock = sizeInBlock
			maxAddrInBlock = addrInBlock
			maxStorageKeysInBlock = keysInBlock
		}
		totalAddrCount += addrInBlock
		totalStorageKeysCount += keysInBlock
	}

	// Save all access lists to a JSON file
	file, err := os.Create("/root/now/lab/eth/python/block_acls.json_")
	if err != nil {
		log.Fatalf("Failed to create JSON file: %v", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(allACL); err != nil {
		log.Fatalf("Failed to write JSON file: %v", err)
	}

	fmt.Println("Access lists saved to block_acls.json_")

	fmt.Printf("Average addr count: %d\n", int64(totalAddrCount)/(blockEnd-blockStart))
	fmt.Printf("Average storage keys count: %d\n", int64(totalStorageKeysCount)/(blockEnd-blockStart))
	fmt.Printf("Max addr count in a block: %d\n", maxAddrInBlock)
	fmt.Printf("Max storage keys count in a block: %d\n", maxStorageKeysInBlock)
	fmt.Printf("Total txs: %d\n", txsCount)
}
