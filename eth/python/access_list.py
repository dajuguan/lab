# create access list for each transaction in a block
from web3 import Web3

# Connect to an Ethereum node (use Infura, Alchemy, or local)
RPC_URL = "http://localhost:7544"
w3 = Web3(Web3.HTTPProvider(RPC_URL))

def fetch_block_tx_hashs(block_number):
    block = w3.eth.get_block(block_number)
    tx_hashs = []
    for tx in block.transactions:
        tx_hashs.append("0x"+tx.hex())
    return tx_hashs, int(block.baseFeePerGas)

skiped_tx = 0
def count_access_list(tx_hash, curr_base_fee):
    tx = w3.eth.get_transaction(tx_hash)
    block_number = tx.blockNumber

    # Prepare call input
    if  hasattr(tx, "maxFeePerGas") and (int(tx['gas']) != 21000):
        params = {
        "from": tx["from"],
        "to": tx["to"],
        # "gas": tx["gas"],
        "value": hex(tx["value"]),
        "data": tx["input"],
        # "maxFeePerGas": tx["maxFeePerGas"],
        # "maxPriorityFeePerGas": tx["maxPriorityFeePerGas"],
        }
    else:
        params = {
        "from": tx["from"],
        "to": tx["to"],
        # should use gasLmit not gas, or it throws intrinsic gas too low
        # "gas": hex(tx["gas"]),
        # "gasPrice": tx["gasPrice"],
        "value": hex(tx["value"]),
        "data": tx["input"],
        }
    
    # if  hasattr(tx, "maxFeePerGas") and (int(tx['gas']) != 21000):
    #     params['maxFeePerGas'] = max(params['maxFeePerGas'], curr_base_fee)
    # else:
    #     params['gasPrice'] = max(params['gasPrice'], curr_base_fee)

    resp = w3.provider.make_request("eth_createAccessList", [params, hex(block_number-1)])
    if "error" in resp:
        # print("txhash:", tx_hash)
        print("tx:", tx)
        print("error:", resp['error'])
        global skiped_tx
        skiped_tx += 1
        return 0, 0
    else:
        access_list = resp['result']['accessList']
        addr_count = len(access_list)
        storagekeys_count = 0
        for acl in access_list:
            storagekeys_count += len(acl['storageKeys'])
        return addr_count, storagekeys_count

block_number = 22028495  
tx_hashs = fetch_block_tx_hashs(block_number)
total_addr_count, total_storagekeys_count = 0, 0
max_addr_count, max_storagekeys_count = 0, 0
txs_count = 0

# block_start = 16000000
block_start = 22028158
block_end = 22028900

(_,curr_base_fee) = tx_hashs = fetch_block_tx_hashs(block_start - 1)

for block_number in range(block_start, block_end):
    tx_hashs, next_base_fee = fetch_block_tx_hashs(block_number)
    txs_count += len(tx_hashs)
    for h in tx_hashs:
        (addr_count, storagekeys_count) = count_access_list(h, curr_base_fee)
        total_addr_count += addr_count
        total_storagekeys_count += storagekeys_count
        max_addr_count = max(max_addr_count, addr_count)
        max_storagekeys_count = max(max_storagekeys_count, storagekeys_count)
    curr_base_fee = next_base_fee

print(f"average addr count: {total_addr_count/(block_end-block_start)}")   
print(f"average storagekeys count: {total_storagekeys_count/(block_end-block_start)}")
print(f"max addr count: {max_addr_count}")
print(f"max storagekeys count: {max_storagekeys_count}")
print("skiped tx:", skiped_tx)
print("total txs:", txs_count)
