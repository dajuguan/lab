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
    return tx_hashs, int(block.baseFeePerGas)


"""
tx_hash = "0x53d1bd19c96660f9f71377b7d66fb8d2d51442213168c236b41d9be4437691cc"
get_tx_access_list(tx_hash)
"""

def get_tx_access_list(tx_hash):
    resp = w3.provider.make_request("debug_traceTransaction", [tx_hash, {"tracer": "prestateTracer", "tracerConfig": {"disableCode": True}}])['result']
    acl = []
    storagekeys_count = 0
    for k, v in resp.items():
        acl.append({
            "address": k,
            "storage_keys":  list(v['storage'].keys()) if 'storage' in v else [],
        })
        storagekeys_count += len(v['storage'].keys()) if 'storage' in v else 0
    addr_count = len(acl)
    return acl,addr_count ,storagekeys_count

total_addr_count, total_storagekeys_count = 0, 0
max_addr_in_block, max_storagekeys_in_block = 0, 0
max_size_in_block = 0
txs_count = 0

# block_start = 16000000
block_start = 21973379
block_end = 21973382


block_acls = {}
for block_number in range(block_start, block_end):
    tx_hashs, next_base_fee = fetch_block_tx_hashs(block_number)
    txs_count += len(tx_hashs)
    acls = []
    size_in_block = 0
    storagekeys_in_block = 0
    addr_in_block = 0
    for h in tx_hashs:
        (acl, addr_count, storagekeys_count) = get_tx_access_list(h)

        for i, item in enumerate(acl):
             # Check if the address already exists in acl
            entry = next((entry for entry in acls if entry['address'] == item['address']), None)
            if entry:
                prev = len(entry['storage_keys']) + len(item['storage_keys'])
                entry['storage_keys'] = (list(set(item['storage_keys'] + entry['storage_keys'])))
                storagekeys_count -= prev - len(entry['storage_keys'])
            else:
                acls.append(item)
        
        storagekeys_in_block += storagekeys_count
    
    addr_in_block = len(acls)
    size_in_block += addr_in_block * 20 + storagekeys_in_block * 32

    total_addr_count += addr_in_block
    total_storagekeys_count += storagekeys_in_block
    

    if size_in_block > max_size_in_block:
        max_size_in_block = size_in_block
        max_addr_in_block = addr_in_block
        max_storagekeys_in_block = storagekeys_in_block

    acls.sort(key=lambda x: x['address'])
    block_acls[block_number] = acls

json.dump(block_acls, open("block_acls.json", "w"), indent=4)

print(f"average addr count: {total_addr_count/(block_end-block_start)}")   
print(f"average storagekeys count: {total_storagekeys_count/(block_end-block_start)}")
print(f"max addr count in a tx: {max_addr_in_block}")
print(f"max storagekeys count in a tx: {max_storagekeys_in_block}")
print("total txs:", txs_count)