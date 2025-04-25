package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"sync"

	"github.com/ethereum/go-ethereum/rpc"
)

type KV struct {
	Key string `json:"key"`
	Val string `json:"val"`
}
type AccessList struct {
	Address     string `json:"address"`
	StorageKeys []KV   `json:"storageKeys"`
}

type AccessListEntry struct {
	Address     string   `json:"address"`
	StorageKeys []string `json:"storage_keys"`
}

type TraceResponse struct {
	Pre map[string]struct {
		Storage map[string]interface{} `json:"storage"`
	} `json:"pre"`
	Post map[string]struct {
		Storage map[string]interface{} `json:"storage"`
	} `json:"post"`
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

func getTxAccessList(client *rpc.Client, txHash string) (map[string]interface{}, error) {

	var resp TraceResponse
	err := client.Call(&resp, "debug_traceTransaction", txHash, map[string]interface{}{
		"tracer":       "prestateTracer",
		"tracerConfig": map[string]bool{"disableCode": true, "diffMode": true},
	})
	if err != nil {
		log.Printf("Failed to trace transaction %s: %v", txHash, err)
		return nil, err
	}

	preACL := []AccessListEntry{}
	postACL := []AccessListEntry{}
	preStorageKeysCount := 0
	postStorageKeysCount := 0

	for addr, data := range resp.Pre {
		storageKeys := []string{}
		for key := range data.Storage {
			storageKeys = append(storageKeys, key)
		}
		preACL = append(preACL, AccessListEntry{Address: addr, StorageKeys: storageKeys})
		preStorageKeysCount += len(storageKeys)
	}

	for addr, data := range resp.Post {
		storageKeys := []string{}
		for key := range data.Storage {
			storageKeys = append(storageKeys, key)
		}
		postACL = append(postACL, AccessListEntry{Address: addr, StorageKeys: storageKeys})
		postStorageKeysCount += len(storageKeys)
	}

	return map[string]interface{}{
		"preACL":               preACL,
		"preStorageKeysCount":  preStorageKeysCount,
		"postAddrCount":        len(postACL),
		"postStorageKeysCount": postStorageKeysCount,
	}, nil
}

func deduplicateAndSortAccessListCount(acl []AccessListEntry) (int, int) {
	addressMap := make(map[string]map[string]struct{})
	for _, entry := range acl {
		if _, exists := addressMap[entry.Address]; !exists {
			addressMap[entry.Address] = make(map[string]struct{})
		}
		for _, key := range entry.StorageKeys {
			addressMap[entry.Address][key] = struct{}{}
		}
	}

	keysLen := 0
	for _, keysMap := range addressMap {
		keysLen += len(keysMap)
	}

	return len(addressMap), keysLen
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

	blockStart := int64(22328000)
	blockEnd := int64(22328003)

	txsCount := 0
	totalPreAddrForBlock, totalPreStorageKeysForBlock := 0, 0
	totalPreAddrForTxs, totalPreStorageKeysForTxs := 0, 0
	totalPostAddrForTxs, totalPostStorageKeysForTxs := 0, 0
	maxAddrForBlockBal, maxStorageKeysForBlockBal := 0, 0
	maxPreAddrForBlockBalDiff, maxPreStorageKeysForBlockBalDiff := 0, 0
	maxPostAddrForBlockBalDiff, maxPostStorageKeysForBlockBalDiff := 0, 0
	maxAddrForTxsBal, maxStorageKeysForTxsBal := 0, 0
	maxSizeForBlockBal := 0
	maxSizeForBlockBalDiff := 0
	maxSizeForTxsBal := 0

	var wg sync.WaitGroup

	type ACLWithIndex struct {
		index                int
		preACL               []AccessListEntry
		preAddrCount         int
		postAddrCount        int
		preStorageKeysCount  int
		postStorageKeysCount int
	}

	for blockNumber := blockStart; blockNumber < blockEnd; blockNumber++ {
		txHashes, err := fetchBlockTxHashes(client, blockNumber)
		if err != nil {
			log.Printf("Failed to fetch block %d: %v", blockNumber, err)
			continue
		}
		txsCount += len(txHashes)

		aclWithIds := make(chan ACLWithIndex, len(txHashes))
		for i, txHash := range txHashes {
			wg.Add(1)
			go func(txHash string, i int) {
				defer wg.Done()
				result, err := getTxAccessList(client, txHash)

				if err != nil {
					log.Printf("Failed to get access list for tx %s: %v", txHash, err)
					return
				}

				preACL := result["preACL"].([]AccessListEntry)
				preAddrCount := len(preACL)
				postAddrCount := result["postAddrCount"].(int)
				preStorageKeysCount := result["preStorageKeysCount"].(int)
				postStorageKeysCount := result["postStorageKeysCount"].(int)

				aclWithIds <- ACLWithIndex{index: i, preACL: preACL, preAddrCount: preAddrCount, postAddrCount: postAddrCount, preStorageKeysCount: preStorageKeysCount, postStorageKeysCount: postStorageKeysCount}

			}(txHash, i)
		}
		wg.Wait()

		size_pre_for_block_bal := 0
		size_pre_for_txs_bal := 0
		size_post_for_txs_bal := 0
		addr_for_txs_bal := 0
		storagekeys_for_txs_bal := 0
		addr_for_txs_post := 0
		storagekeys_for_txs_post := 0

		aclWithIdsSlice := make([]AccessListEntry, 0, len(txHashes))
		for i := 0; i < len(txHashes); i++ {
			aclWithIndex := <-aclWithIds
			aclWithIdsSlice = append(aclWithIdsSlice, aclWithIndex.preACL...)

			totalPreAddrForTxs += aclWithIndex.preAddrCount
			totalPreStorageKeysForTxs += aclWithIndex.preStorageKeysCount
			totalPostAddrForTxs += aclWithIndex.postAddrCount
			totalPostStorageKeysForTxs += aclWithIndex.postStorageKeysCount

			size_pre_for_txs_bal += aclWithIndex.preAddrCount*20 + aclWithIndex.preStorageKeysCount*32
			size_post_for_txs_bal += aclWithIndex.postAddrCount*20 + aclWithIndex.postStorageKeysCount*32
			addr_for_txs_bal += aclWithIndex.preAddrCount
			storagekeys_for_txs_bal += aclWithIndex.preStorageKeysCount
			addr_for_txs_post += aclWithIndex.postAddrCount
			storagekeys_for_txs_post += aclWithIndex.postStorageKeysCount
		}

		addr_for_block_bal, storagekeys_for_block_bal := deduplicateAndSortAccessListCount(aclWithIdsSlice)
		totalPreAddrForBlock += addr_for_block_bal
		totalPreStorageKeysForBlock += storagekeys_for_block_bal
		size_pre_for_block_bal += addr_for_block_bal*20 + storagekeys_for_block_bal*32

		if size_pre_for_block_bal > maxSizeForBlockBal {
			maxSizeForBlockBal = size_pre_for_block_bal
			maxAddrForBlockBal = addr_for_block_bal
			maxStorageKeysForBlockBal = storagekeys_for_block_bal
		}

		if size_pre_for_block_bal+size_post_for_txs_bal > maxSizeForBlockBalDiff {
			maxSizeForBlockBalDiff = size_pre_for_block_bal + size_post_for_txs_bal
			maxPreAddrForBlockBalDiff = addr_for_block_bal
			maxPreStorageKeysForBlockBalDiff = storagekeys_for_block_bal
			maxPostAddrForBlockBalDiff = addr_for_txs_post
			maxPostStorageKeysForBlockBalDiff = storagekeys_for_txs_post
		}

		if size_pre_for_txs_bal > maxSizeForTxsBal {
			maxSizeForTxsBal = size_pre_for_txs_bal
			maxAddrForTxsBal = addr_for_txs_bal
			maxStorageKeysForTxsBal = storagekeys_for_txs_bal
		}
	}

	// Final calculations and print statements
	meanAddrForBlockBal := totalPreAddrForBlock / int(blockEnd-blockStart)
	meanKeysForBlockBal := totalPreStorageKeysForBlock / int(blockEnd-blockStart)
	fmt.Printf("addr for block bal count, mean: %d, max: %d\n", meanAddrForBlockBal, maxAddrForBlockBal)
	fmt.Printf("storagekeys for bal count, mean: %d, max: %d\n", meanKeysForBlockBal, maxStorageKeysForBlockBal)

	meanAddrForBlockBalDiff := (totalPreAddrForBlock + totalPostAddrForTxs) / int(blockEnd-blockStart)
	meanKeysForBlockBalDiff := (totalPreStorageKeysForBlock + totalPostStorageKeysForTxs) / int(blockEnd-blockStart)

	maxAddrForBlockBalDiff := maxPreAddrForBlockBalDiff + maxPostAddrForBlockBalDiff
	maxStorageKeysForBlockBalDiff := maxPreStorageKeysForBlockBalDiff + maxPostStorageKeysForBlockBalDiff
	fmt.Printf("addr for block bal + diff count, mean: %d, max: %d\n", meanAddrForBlockBalDiff, maxAddrForBlockBalDiff)
	fmt.Printf("storagekeys for bal + diff count, mean: %d, max: %d\n", meanKeysForBlockBalDiff, maxStorageKeysForBlockBalDiff)

	meanAddrForTxsBal := totalPreAddrForTxs / int(blockEnd-blockStart)
	meanKeysForTxsBal := totalPreStorageKeysForTxs / int(blockEnd-blockStart)
	fmt.Printf("addr for txs bal count, mean: %d, max: %d\n", meanAddrForTxsBal, maxAddrForTxsBal)
	fmt.Printf("storagekeys for txs bal count, mean: %d, max: %d\n", meanKeysForTxsBal, maxStorageKeysForTxsBal)

	fmt.Println("----------------------size----------------------")
	meanSizeBlockKey := meanAddrForBlockBal*20 + meanKeysForBlockBal*32
	maxSizeBlockKey := maxAddrForBlockBal*20 + maxStorageKeysForBlockBal*32
	fmt.Printf("%40s, mean: %.2f, max: %.2f\n", "1:bal keys size in bytes", float64(meanSizeBlockKey)/1000, float64(maxSizeBlockKey)/1000)

	meanPostAddrForBlockDiff := totalPostAddrForTxs / int(blockEnd-blockStart)
	meanPostKeysForBlockBalDiff := totalPostStorageKeysForTxs / int(blockEnd-blockStart)
	addrValSize := 20 + 8 + 32 // addr + nonce + balance + codeHash(optional)
	keyValSize := 32 + 32      // key + value
	meanSizeDiff := meanSizeBlockKey + meanPostAddrForBlockDiff*addrValSize + meanPostKeysForBlockBalDiff*keyValSize
	maxPreSizeBlockKey := maxPreAddrForBlockBalDiff*20 + maxPreStorageKeysForBlockBalDiff*32
	maxSizeDiff := maxPreSizeBlockKey + maxPostAddrForBlockBalDiff*addrValSize + maxPostStorageKeysForBlockBalDiff*keyValSize
	fmt.Printf("%40s, mean: %.2f, max: %.2f\n", "2:bal keys + diff vals size in bytes", float64(meanSizeDiff)/1000, float64(maxSizeDiff)/1000)

	meanSizeBlockKV := meanAddrForBlockBal*addrValSize + meanKeysForBlockBal*keyValSize
	maxSizeBlockKV := maxAddrForBlockBal*addrValSize + maxStorageKeysForBlockBal*keyValSize
	fmt.Printf("%40s, mean: %.2f, max: %.2f\n", "3:bal kvs size in bytes", float64(meanSizeBlockKV)/1000, float64(maxSizeBlockKV)/1000)

	meanSizeForBlockKVDiff := meanAddrForBlockBalDiff*addrValSize + meanKeysForBlockBalDiff*keyValSize
	maxSizeForBlockKVDiff := maxAddrForBlockBalDiff*addrValSize + maxStorageKeysForBlockBalDiff*keyValSize
	fmt.Printf("%40s, mean: %.2f, max: %.2f\n", "4:bal kvs + diff kvs size in bytes", float64(meanSizeForBlockKVDiff)/1000, float64(maxSizeForBlockKVDiff)/1000)

	meanSizeForTxsKV := meanAddrForTxsBal*addrValSize + meanKeysForTxsBal*keyValSize
	maxSizeForTxsKV := maxAddrForTxsBal*addrValSize + maxStorageKeysForTxsBal*keyValSize
	fmt.Printf("%40s, mean: %.2f, max: %.2f\n", "5:txs kvs size in bytes", float64(meanSizeForTxsKV)/1000, float64(maxSizeForTxsKV)/1000)
}
