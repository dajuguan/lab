# create access list for each transaction in a block
from web3 import Web3
import json

# Connect to an Ethereum node (use Infura, Alchemy, or local)
RPC_URL = json.load(open("../.env"))["RPC_URL"]
w3 = Web3(Web3.HTTPProvider(RPC_URL))

""" 
curl localhost:7145 -X POST -H "Content-Type: application/json" -d '{ "jsonrpc": "2.0", "id": 0, "method": "trace_replayTransaction", "params": ["0x53d1bd19c96660f9f71377b7d66fb8d2d51442213168c236b41d9be4437691cc", ["Trace","StateDiff", "VmTrace"]] }' > log.json

curl localhost:7145 -X POST -H "Content-Type: application/json" -d '{ "jsonrpc": "2.0", "id": 0, "method": "eth_createAccessList", "params": [{"hash":"0x53d1bd19c96660f9f71377b7d66fb8d2d51442213168c236b41d9be4437691cc","blockNumber":"0x1526ad2", "type": "1"}, "0x1526ad1"] }' > log_acl.json

## geth(最终使用的, 因为access_list不会包含transfer的)
https://geth.ethereum.org/docs/developers/evm-tracing/built-in-tracers#prestate-tracer
 curl http://23.88.77.175:8545 -X POST -H "Content-Type: application/json" -d '{"method":"debug_traceTransaction","params":["0x97e607dadfbe25c166d18bb99ec21111d0afc87d7fe784a900fd63b24fd38015", {"tracer": "prestateTracer", "tracerConfig": {"disableCode": true}}], "id":1,"jsonrpc":"2.0"}' > log_geth_prestate.json
"""

def fetch_block_tx_hashs(block_number):
    block = w3.eth.get_block(block_number)
    tx_hashs = []
    for tx in block.transactions:
        tx_hashs.append("0x"+tx.hex())
    # print("txhashs:", tx_hashs)
    return tx_hashs, int(block.baseFeePerGas)


"""
tx_hash = "0x53d1bd19c96660f9f71377b7d66fb8d2d51442213168c236b41d9be4437691cc"
get_tx_access_list(tx_hash)
"""

def get_tx_access_list(tx_hash):
    resp = w3.provider.make_request("debug_traceTransaction", [tx_hash, {"tracer": "prestateTracer", "tracerConfig": {"disableCode": True, "diffMode": True}}])['result']
    pre_acl = []
    post_acl = []
    pre_storagekeys_count = 0
    post_storagekeys_count = 0
    for k, v in resp['pre'].items():
        pre_acl.append({
            "address": k,
            "storage_keys":  list(v['storage'].keys()) if 'storage' in v else [],
        })
        pre_storagekeys_count += len(v['storage'].keys()) if 'storage' in v else 0

    for k, v in resp['post'].items():
        post_acl.append({
            "address": k,
            "storage_keys":  list(v['storage'].keys()) if 'storage' in v else [],
        })
        post_storagekeys_count += len(v['storage'].keys()) if 'storage' in v else 0

    pre_addr_count = len(pre_acl)
    post_addr_count = len(post_acl)
    return pre_acl,pre_addr_count,pre_storagekeys_count, post_addr_count, post_storagekeys_count

total_pre_addr_for_block, total_pre_storagekeys_for_block = 0, 0
total_pre_addr_for_txs, total_pre_storagekeys_for_txs = 0, 0
total_post_addr_for_txs, total_post_storagekeys_for_txs = 0, 0
max_addr_for_block_bal, max_storagekeys_for_block_bal = 0, 0
max_pre_addr_for_block_bal_diff, max_pre_storagekeys_for_block_bal_diff = 0, 0
max_post_addr_for_block_bal_diff, max_post_storagekeys_for_block_bal_diff = 0, 0
max_addr_for_txs_bal, max_storagekeys_for_txs_bal = 0, 0
max_size_for_block_bal = 0
max_size_for_block_bal_diff = 0
max_size_for_txs_bal = 0
txs_count = 0

# block_start = 16000000
block_start = 22328000
block_end = 22328003


block_acls = {}
for block_number in range(block_start, block_end):
    tx_hashs, next_base_fee = fetch_block_tx_hashs(block_number)
    txs_count += len(tx_hashs)
    acls = []
    size_pre_for_block_bal = 0
    size_pre_for_txs_bal = 0
    size_post_for_txs_bal = 0
    storagekeys_for_block_bal = 0
    addr_for_block_bal = 0
    addr_for_txs_bal = 0
    storagekeys_for_txs_bal = 0
    addr_for_txs_post = 0
    storagekeys_for_txs_post = 0
    
    for h in tx_hashs:
        (pre_acl, pre_addr_count, pre_storagekeys_count, post_addr_count, post_storagekeys_count) = get_tx_access_list(h)

        size_pre_for_txs_bal += pre_addr_count * 20 + pre_storagekeys_count * 32
        size_post_for_txs_bal += post_addr_count * 20 + post_storagekeys_count * 32

        storagekeys_for_txs_bal += pre_storagekeys_count
        addr_for_txs_bal += pre_addr_count
        addr_for_txs_post += post_addr_count
        storagekeys_for_txs_post += post_storagekeys_count

        total_pre_storagekeys_for_txs += pre_storagekeys_count
        total_pre_addr_for_txs += pre_addr_count
        total_post_storagekeys_for_txs += post_storagekeys_count
        total_post_addr_for_txs += post_addr_count

        for i, item in enumerate(pre_acl):
             # Check if the address already exists in acl
            entry = next((entry for entry in acls if entry['address'] == item['address']), None)
            if entry:
                prev = len(entry['storage_keys']) + len(item['storage_keys'])
                entry['storage_keys'] = (list(set(item['storage_keys'] + entry['storage_keys'])))
                pre_storagekeys_count -= prev - len(entry['storage_keys'])
            else:
                acls.append(item)


        storagekeys_for_block_bal += pre_storagekeys_count
    
    addr_for_block_bal = len(acls)
    size_pre_for_block_bal += addr_for_block_bal * 20 + storagekeys_for_block_bal * 32

    total_pre_addr_for_block += addr_for_block_bal
    total_pre_storagekeys_for_block += storagekeys_for_block_bal
    

    if size_pre_for_block_bal > max_size_for_block_bal:
        max_size_for_block_bal = size_pre_for_block_bal
        max_addr_for_block_bal = addr_for_block_bal
        max_storagekeys_for_block_bal = storagekeys_for_block_bal
    
    if size_pre_for_block_bal + size_post_for_txs_bal > max_size_for_block_bal_diff:
        max_size_for_block_bal_diff = size_pre_for_block_bal + size_post_for_txs_bal
        max_pre_addr_for_block_bal_diff = addr_for_block_bal
        max_pre_storagekeys_for_block_bal_diff = storagekeys_for_block_bal
        max_post_addr_for_block_bal_diff = addr_for_txs_post
        max_post_storagekeys_for_block_bal_diff = storagekeys_for_txs_post

    if size_pre_for_txs_bal > max_size_for_txs_bal:
        max_size_for_txs_bal = size_pre_for_txs_bal
        max_addr_for_txs_bal = addr_for_txs_bal
        max_storagekeys_for_txs_bal = storagekeys_for_txs_bal

    # acls.sort(key=lambda x: x['address'])
    # block_acls[block_number] = acls

# json.dump(block_acls, open("block_acls.json", "w"), indent=4)

print("total txs:", txs_count)

mean_addr_for_block_bal = total_pre_addr_for_block/(block_end-block_start)
mean_keys_for_block_bal = total_pre_storagekeys_for_block/(block_end-block_start)
print(f"addr for block bal count, mean:{mean_addr_for_block_bal}, max: {max_addr_for_block_bal}")   
print(f"storagekeys for bal count, mean: {mean_keys_for_block_bal}, max: {max_storagekeys_for_block_bal}")

mean_addr_for_block_bal_diff = (total_pre_addr_for_block + total_post_addr_for_txs)/(block_end-block_start)
mean_keys_for_block_bal_diff = (total_pre_storagekeys_for_block + total_post_storagekeys_for_txs)/(block_end-block_start)

max_addr_for_block_bal_diff = max_pre_addr_for_block_bal_diff + max_post_addr_for_block_bal_diff
max_storagekeys_for_block_bal_diff = max_pre_storagekeys_for_block_bal_diff + max_post_storagekeys_for_block_bal_diff
print(f"addr for block bal + diff count, mean:{mean_addr_for_block_bal_diff}, max: {max_addr_for_block_bal_diff}")   
print(f"storagekeys for bal + diff count, mean: {mean_keys_for_block_bal_diff}, max: {max_storagekeys_for_block_bal_diff}")

mean_addr_for_txs_bal = total_pre_addr_for_txs/(block_end-block_start)
mean_keys_for_txs_bal = total_pre_storagekeys_for_txs/(block_end-block_start)
print(f"addr for txs bal count, mean: {mean_addr_for_txs_bal}, max: {max_addr_for_txs_bal}")
print(f"storagekeys for txs bal count, mean: {mean_keys_for_txs_bal}, max: {max_storagekeys_for_txs_bal}")

print("----------------------size----------------------")
mean_size_block_key = mean_addr_for_block_bal * 20 + mean_keys_for_block_bal * 32
max_size_block_key = max_addr_for_block_bal * 20 + max_storagekeys_for_block_bal * 32
print(f"{'1:bal keys size in bytes'.zfill(40)}, mean: {mean_size_block_key/1000}, max: {max_size_block_key/1000}")

mean_post_addr_for_block_diff = total_post_addr_for_txs/(block_end-block_start)
mean_post_keys_for_block_bal_diff = total_post_storagekeys_for_txs/(block_end-block_start)
ADDR_VAL_SIZE = 20 + 8 + 32 # addr + nonce + balance + codeHash(optional)
KEY_VAL_SIZE = 32 + 32 # key + value
# bal_addr * 20 + bal_key * 32 + diff_addr * ADDR_VAL + diff_key * KEY_VAL
mean_size_diff = mean_size_block_key + mean_post_addr_for_block_diff * ADDR_VAL_SIZE + mean_post_keys_for_block_bal_diff * KEY_VAL_SIZE
# bal_addr * 20 + bal_key * 32 + diff_addr * ADDR_VAL + diff_key * KEY_VAL
max_pre_size_block_key = max_pre_addr_for_block_bal_diff * 20 + max_pre_storagekeys_for_block_bal_diff * 32
max_size_diff =  max_pre_size_block_key + max_post_addr_for_block_bal_diff * ADDR_VAL_SIZE + max_post_storagekeys_for_block_bal_diff * KEY_VAL_SIZE
print(f"{'2:bal keys + diff vals size in bytes'.zfill(40)}, mean: {mean_size_diff/1000}, max: {max_size_diff/1000}")

mean_size_block_kv = mean_addr_for_block_bal * ADDR_VAL_SIZE + mean_keys_for_block_bal * KEY_VAL_SIZE
max_size_block_kv = max_addr_for_block_bal * ADDR_VAL_SIZE + max_storagekeys_for_block_bal * KEY_VAL_SIZE
print(f"{'3:bal kvs size in bytes'.zfill(40)}, mean: {mean_size_block_kv/1000}, max: {max_size_block_kv/1000}")

mean_size_for_block_kv_diff = mean_addr_for_block_bal_diff * ADDR_VAL_SIZE + mean_keys_for_block_bal_diff * KEY_VAL_SIZE
max_size_for_block_kv_diff = max_addr_for_block_bal_diff * ADDR_VAL_SIZE + max_storagekeys_for_block_bal_diff * KEY_VAL_SIZE
print(f"{'4:bal kvs + diff kvs size in bytes'.zfill(40)}, mean: {mean_size_for_block_kv_diff/1000}, max: {max_size_for_block_kv_diff/1000}")

mean_size_for_txs_kv = mean_addr_for_txs_bal * ADDR_VAL_SIZE + mean_keys_for_txs_bal * KEY_VAL_SIZE
max_size_for_txs_kv = max_addr_for_txs_bal * ADDR_VAL_SIZE + max_storagekeys_for_txs_bal * KEY_VAL_SIZE
print(f"{'5:txs kvs size in bytes'.zfill(40)}, mean: {mean_size_for_txs_kv/1000}, max: {max_size_for_txs_kv/1000}")