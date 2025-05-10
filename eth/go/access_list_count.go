package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"sync"

	"github.com/ethereum/go-ethereum/rpc"
)

type AccessListEntry struct {
	Address     string   `json:"address"`
	StorageKeys []string `json:"storage_keys"`
}

type TraceResponseDiff struct {
	Pre map[string]struct {
		Storage map[string]interface{} `json:"storage"`
	} `json:"pre"`
	Post map[string]struct {
		Storage map[string]interface{} `json:"storage"`
	} `json:"post"`
}

type TraceResponse map[string]struct {
	Storage map[string]string `json:"storage"`
}

type RPCConfig struct {
	RPCURL string `json:"RPC_URL"`
}

type BlockHashes struct {
	// must be capitalized
	Number string   `json:"number"`
	Hashes []string `json:"transactions"`
}

func fetchTxHashesForBlock(client *rpc.Client, blockNumber int64) ([]string, error) {

	var raw json.RawMessage
	err := client.Call(&raw, "eth_getBlockByNumber", fmt.Sprintf("0x%x", blockNumber), false)
	if err != nil {
		return nil, err
	}
	// Decode header and transactions.
	var block *BlockHashes
	if err := json.Unmarshal(raw, &block); err != nil {
		return nil, err
	}

	return block.Hashes, nil
}

func fetchTxAccessList(client *rpc.Client, txHash string) (map[string]interface{}, error) {

	// in diffmode, only changed account's pre and post values are returned, so pre is not complete
	var respDiff TraceResponseDiff
	err := client.Call(&respDiff, "debug_traceTransaction", txHash, map[string]interface{}{
		"tracer":       "prestateTracer",
		"tracerConfig": map[string]bool{"disableCode": true, "diffMode": true},
	})
	if err != nil {
		log.Printf("Failed to trace transaction diff %s: %v", txHash, err)
		return nil, err
	}

	var resp TraceResponse
	err = client.Call(&resp, "debug_traceTransaction", txHash, map[string]interface{}{
		"tracer":       "prestateTracer",
		"tracerConfig": map[string]bool{"disableCode": true, "diffMode": false},
	})
	if err != nil {
		log.Printf("Failed to trace transaction %s: %v", txHash, err)
		return nil, err
	}

	preACL := []AccessListEntry{}
	postACL := []AccessListEntry{}
	preStorageKeysCount := 0
	postStorageKeysCount := 0

	for addr, data := range resp {
		storageKeys := []string{}
		for key := range data.Storage {
			storageKeys = append(storageKeys, key)
		}
		preACL = append(preACL, AccessListEntry{Address: addr, StorageKeys: storageKeys})
		preStorageKeysCount += len(storageKeys)
	}

	for addr, data := range respDiff.Post {
		storageKeys := []string{}
		for key := range data.Storage {
			storageKeys = append(storageKeys, key)
		}
		postACL = append(postACL, AccessListEntry{Address: addr, StorageKeys: storageKeys})
		postStorageKeysCount += len(storageKeys)
	}

	return map[string]interface{}{
		"preACL":               preACL,
		"postACL":              postACL,
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

func deduplicatePreBALCount(pre []AccessListEntry, post []AccessListEntry) (int, int, int, int) {
	postAddressMap := make(map[string]map[string]struct{})
	for _, entry := range post {
		if _, exists := postAddressMap[entry.Address]; !exists {
			postAddressMap[entry.Address] = make(map[string]struct{})
		}
		for _, key := range entry.StorageKeys {
			postAddressMap[entry.Address][key] = struct{}{}
		}
	}

	addrCountMap := make(map[string]int)
	addressExistInPost := make(map[string]bool)

	slotCountMap := make(map[string]map[string]int)
	slotExitInPost := make(map[string]map[string]bool)

	for _, entry := range pre {
		addr := entry.Address
		postSlots, exists := postAddressMap[addr]
		if exists {
			addressExistInPost[addr] = true
		}
		addrCountMap[addr] += 1

		for _, slot := range entry.StorageKeys {
			if slotExitInPost[addr] == nil {
				slotExitInPost[addr] = make(map[string]bool)
			}
			if slotCountMap[addr] == nil {
				slotCountMap[addr] = make(map[string]int)
			}
			if _, exists := postSlots[slot]; exists {
				slotExitInPost[addr][slot] = true
			}
			slotCountMap[addr][slot] += 1
		}
	}

	addrCount := 0
	slotCount := 0
	caAddrCount := 0
	eoaAddrCount := 0
	for addr, count := range addrCountMap {
		// if addr never exists in post, only count same addrs for once
		if _, exists := postAddressMap[addr]; !exists {
			count = 1
		}
		addrCount += count

		// if slot never exists in post, only count same slot under an addr for once
		slotMap := slotCountMap[addr]
		if len(slotMap) == 0 {
			eoaAddrCount += count
		} else {
			caAddrCount += count
		}

		for slot, sCount := range slotMap {
			if val, _ := slotExitInPost[addr][slot]; !val {
				slotCount += 1
			} else {
				slotCount += sCount
			}
		}

	}

	return addrCount, slotCount, eoaAddrCount, caAddrCount
}

func main() {
	// Load RPC URL from .env file
	configData, err := ioutil.ReadFile("../.env")
	if err != nil {
		log.Fatalf("Failed to read .env file: %v", err)
	}
	var config RPCConfig
	if err := json.Unmarshal(configData, &config); err != nil {
		log.Fatalf("Failed to parse .env file: %v", err)
	}

	client, err := rpc.Dial(config.RPCURL)
	if err != nil {
		log.Fatalf("Failed to connect to Ethereum node: %v", err)
	}

	blockStart := int64(22347001)
	blockEnd := int64(22349001)

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

	totalEOAAddrForTxsBal := 0
	totalCAAddrForTxsBal := 0

	var wg sync.WaitGroup

	type ACLWithIndex struct {
		index                int
		preACL               []AccessListEntry
		postACL              []AccessListEntry
		preAddrCount         int
		postAddrCount        int
		preStorageKeysCount  int
		postStorageKeysCount int
	}

	for blockNumber := blockStart; blockNumber < blockEnd; blockNumber++ {
		txHashes, err := fetchTxHashesForBlock(client, blockNumber)
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
				result, err := fetchTxAccessList(client, txHash)

				if err != nil {
					log.Printf("Failed to get access list for tx %s: %v", txHash, err)
					return
				}

				preACL := result["preACL"].([]AccessListEntry)
				postACL := result["postACL"].([]AccessListEntry)
				preAddrCount := len(preACL)
				postAddrCount := result["postAddrCount"].(int)
				preStorageKeysCount := result["preStorageKeysCount"].(int)
				postStorageKeysCount := result["postStorageKeysCount"].(int)

				aclWithIds <- ACLWithIndex{index: i, preACL: preACL, postACL: postACL, preAddrCount: preAddrCount, postAddrCount: postAddrCount, preStorageKeysCount: preStorageKeysCount, postStorageKeysCount: postStorageKeysCount}

			}(txHash, i)
		}
		wg.Wait()

		sizePreForBlockBal := 0
		sizePreForTxsBal := 0
		sizePostForTxsBal := 0
		addrForTxsBal := 0
		storageKeysForTxsBal := 0
		addrForTxsPost := 0
		storageKeysForTxsPost := 0

		aclWithIdsSlice := make([]AccessListEntry, 0, len(txHashes))
		aclWithIdsPostSlice := make([]AccessListEntry, 0, len(txHashes))

		for i := 0; i < len(txHashes); i++ {
			aclWithIndex := <-aclWithIds
			aclWithIdsSlice = append(aclWithIdsSlice, aclWithIndex.preACL...)
			aclWithIdsPostSlice = append(aclWithIdsPostSlice, aclWithIndex.postACL...)

			totalPostAddrForTxs += aclWithIndex.postAddrCount
			totalPostStorageKeysForTxs += aclWithIndex.postStorageKeysCount
			sizePostForTxsBal += aclWithIndex.postAddrCount*20 + aclWithIndex.postStorageKeysCount*32

			// undedupcated pre
			// addrForTxsBal += aclWithIndex.preAddrCount
			// storageKeysForTxsBal += aclWithIndex.preStorageKeysCount

			addrForTxsPost += aclWithIndex.postAddrCount
			storageKeysForTxsPost += aclWithIndex.postStorageKeysCount
		}

		addrForBlockBal, storageKeysForBlockBal := deduplicateAndSortAccessListCount(aclWithIdsSlice)

		addrForTxsBal, storageKeysForTxsBal, eoa, ca := deduplicatePreBALCount(aclWithIdsSlice, aclWithIdsPostSlice)
		totalPreAddrForTxs += addrForTxsBal
		totalPreStorageKeysForTxs += storageKeysForTxsBal
		sizePreForTxsBal = addrForTxsBal*20 + storageKeysForTxsBal*32
		totalEOAAddrForTxsBal += eoa
		totalCAAddrForTxsBal += ca

		totalPreAddrForBlock += addrForBlockBal
		totalPreStorageKeysForBlock += storageKeysForBlockBal
		sizePreForBlockBal += addrForBlockBal*20 + storageKeysForBlockBal*32

		if sizePreForBlockBal > maxSizeForBlockBal {
			maxSizeForBlockBal = sizePreForBlockBal
			maxAddrForBlockBal = addrForBlockBal
			maxStorageKeysForBlockBal = storageKeysForBlockBal
		}

		if sizePreForBlockBal+sizePostForTxsBal > maxSizeForBlockBalDiff {
			maxSizeForBlockBalDiff = sizePreForBlockBal + sizePostForTxsBal
			maxPreAddrForBlockBalDiff = addrForBlockBal
			maxPreStorageKeysForBlockBalDiff = storageKeysForBlockBal
			maxPostAddrForBlockBalDiff = addrForTxsPost
			maxPostStorageKeysForBlockBalDiff = storageKeysForTxsPost
		}

		if sizePreForTxsBal > maxSizeForTxsBal {
			maxSizeForTxsBal = sizePreForTxsBal
			maxAddrForTxsBal = addrForTxsBal
			maxStorageKeysForTxsBal = storageKeysForTxsBal
		}
	}

	// Final calculations and print statements
	fmt.Println("blocks:", blockEnd-blockStart)
	caPercent := float64(totalCAAddrForTxsBal) / float64(totalPreAddrForTxs)
	fmt.Println("ca/total percentage:", caPercent)
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

	// only slot value
	meanSizeBlockKSV := meanAddrForBlockBal*20 + meanKeysForBlockBal*keyValSize
	maxSizeBlockKSV := maxAddrForBlockBal*20 + maxStorageKeysForBlockBal*keyValSize
	fmt.Printf("%40s, mean: %.2f, max: %.2f\n", "3:bal ksv size in bytes", float64(meanSizeBlockKSV)/1000, float64(maxSizeBlockKSV)/1000)

	// acct value and slot value
	meanSizeBlockKVSV := meanAddrForBlockBal*addrValSize + meanKeysForBlockBal*keyValSize
	maxSizeBlockKVSV := maxAddrForBlockBal*addrValSize + maxStorageKeysForBlockBal*keyValSize
	fmt.Printf("%40s, mean: %.2f, max: %.2f\n", "4:bal kvsv size in bytes", float64(meanSizeBlockKVSV)/1000, float64(maxSizeBlockKVSV)/1000)

	meanSizeForBlockKVDiff := meanAddrForBlockBalDiff*addrValSize + meanKeysForBlockBalDiff*keyValSize
	maxSizeForBlockKVDiff := maxAddrForBlockBalDiff*addrValSize + maxStorageKeysForBlockBalDiff*keyValSize
	fmt.Printf("%40s, mean: %.2f, max: %.2f\n", "5:bal kvs + diff kvs size in bytes", float64(meanSizeForBlockKVDiff)/1000, float64(maxSizeForBlockKVDiff)/1000)

	addrValSize = 20 + 8 + int(math.Ceil(32.0*caPercent))
	meanSizeForTxsKV := meanAddrForTxsBal*addrValSize + meanKeysForTxsBal*keyValSize
	maxSizeForTxsKV := maxAddrForTxsBal*addrValSize + maxStorageKeysForTxsBal*keyValSize
	fmt.Printf("%40s, mean: %.2f, max: %.2f\n", "6:txs kvs size in bytes", float64(meanSizeForTxsKV)/1000, float64(maxSizeForTxsKV)/1000)
}
