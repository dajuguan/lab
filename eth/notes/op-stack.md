## Key Design Questions
### How to prove withdraw?
- 首先计算出withdrawHash
- 然后计算该storage key对应的slot，通过eth_getProof RPC生成证明
```typescript
const proof = getProof(client, {
    address: contracts.l2ToL1MessagePasser.address,
    storageKeys: [slot],
    blockNumber: l2BlockNumber,
})
```
https://github.com/wevm/viem/blob/a59b5630311249031c7bbfdbcc093dd52586a5bf/src/op-stack/actions/buildProveWithdrawal.ts#L103


- 然后再提供outputroof的proof(hash preimage)，以及其中storage root对应的storag proof, [代码](https://github.com/QuarkChain/optimism/blob/876b6bd8649869a2e00903471821e5a6c9aa69f1/packages/contracts-bedrock/src/L1/OptimismPortal2.sol#L373)

```solidity
    function hashOutputRootProof(Types.OutputRootProof memory _outputRootProof) internal pure returns (bytes32) {
        return keccak256(
            abi.encode(
                _outputRootProof.version,
                _outputRootProof.stateRoot,
                _outputRootProof.messagePasserStorageRoot,
                _outputRootProof.latestBlockhash
            )
        );
    }

    function proveWithdrawalTransaction(
        Types.WithdrawalTransaction memory _tx,
        uint256 _disputeGameIndex,
        Types.OutputRootProof calldata _outputRootProof,
        bytes[] calldata _withdrawalProof
    )
    {
        ...
        // Verify that the output root can be generated with the elements in the proof.
        if (disputeGameProxy.rootClaim().raw() != Hashing.hashOutputRootProof(_outputRootProof)) {
            revert OptimismPortal_InvalidOutputRootProof();
        }
        ...
        if (
            SecureMerkleTrie.verifyInclusionProof({
                _key: abi.encode(storageKey),
                _value: hex"01",
                _proof: _withdrawalProof,
                _root: _outputRootProof.messagePasserStorageRoot
            }) == false
        ) {
            revert OptimismPortal_InvalidMerkleProof();
        }
    }

- disable withdraw solution: https://github.com/QuarkChain/optimism/pull/49
```

> So, [archive node](https://docs.optimism.io/chain-operators/guides/management/best-practices#op-proposer-assumes-archive-mode) must be used for L2 withdraw.

### If Deposit transaction revert when _value > msg.value, will the fund be locked in L1 forever?
No.
- [code](https://github.com/ethereum-optimism/optimism/blob/d48b45954c381f75a13e61312da68d84e9b41418/packages/contracts-bedrock/src/L1/OptimismPortal.sol#L369C1-L380C6)
- [doc](https://specs.optimism.io/protocol/deposits.html#execution)

```solidity
    /// @param _to         Target address on L2.
    /// @param _value      ETH value to send to the recipient.
    /// @param _gasLimit   Amount of L2 gas to purchase by burning gas on L1.
    /// @param _isCreation Whether or not the transaction is a contract creation.
    /// @param _data       Data to trigger the recipient with.
    function depositTransaction(
        address _to,
        uint256 _value,
        uint64 _gasLimit,
        bool _isCreation,
        bytes memory _data
    )
```
- The balance of the from account MUST be increased by the amount of mint (msg.value). This is unconditional, and does not revert on deposit failure.
- address alias is used to prevent l1假冒L2地址的情况，即L1合约地址与L2某个地址一致但是实际上部署的代码完全不一样，这导致如果有其他L2合约需要依赖msg.sender判断就会导致被攻击
    - 通过控制create2的salt来枚举是有可能伪造出地址相同但代码不同的合约

### How to filter invalid msg sent to Batch Inbox Address
- Through batcher address

### Sequencer, Batcher, Proposer, Challenger
- Sequencer: receives L2 transactions from L2 users, creates L2 blocks using them, which it then submits to data availability provider (via a batcher). The sequencer’s address is not recorded on-chain; only the batcher’s address is. Users and node operators typically obtain the sequencer’s RPC endpoint from the chain operator.
    - run op-node with `--sequencer.enabled --rpc.port=8547`
- Batcher(BatchSubmitter): submits batches of transactions to L1 (可以控制99%的L2交易，还有1%可以通过L1 deposit tx来到L2)
    - run op-batcher with `--rollup.rpc=http://localhost:8547` to **pull** unsafe blocks and publish these to L1
- Proposer: 
    - [legency](https://github.com/ethereum-optimism/optimism/pull/13489/changes#diff-54cffe8f94a25ed0cfb98c27cc49d91713dfe2312cd62f2d5f567142687be81c): submit l2 output root
    - with fault proof: creates a dispute game for batch of blocks:
    `create(uint32 _gameType,bytes32 _rootClaim,bytes _extraData)`
- Challenger
    - run op-challenger with a funded private key and submitting attack tx when false outputroots are found.

### Can finalized L2 blocks be reorged?
No.
What is reorg? Local node accept two or more difference forked chains.
What is L2 finalized block? The L1 block that includes the L2 block is finalized.
- 如果首先sequencer在L2 finalized之前广播了错误的区块，其他nodes不会接受（即不会添加到本地的链上），所以不涉及reorg
- 如果sequencer广播了正确的L2区块儿，其所属的L1区块finalize之后又再次提交了L2区块号相同但内容不同的区块儿，也不会被其他节点接受（在derivation的时候就发现了）

### Sequencer采用EL Sync导致出现sequencer重启后RPC node无法sunc的坑
假设A是当前sequencer，B采用EL sync
- 此时A收到了交易并打包为unsafe block提交给B，此时B采用EL sync会把[finalized区块号设置为该unsafe block number N](https://github.com/ethereum-optimism/optimism/blob/c0d1ce8a27e5349c04d258dc3d4619b73cca7685/op-node/rollup/engine/engine_controller.go#L547-L556);
- A宕机(unsafe blocks还没被L1 finalize)，然后B上线切换为sequencer，此时B会从N+1开始提交span batch，但是其他节点此时finalized可能还是n (N>n)，导致B的交易被drop掉

[issue](https://github.com/QuarkChain/pm/issues/110), [alan's note](https://github.com/zhiqiangxu/private_notes/blob/main/misc/elsync_safe_head_drift.md)


### Deployment: genesis.json, rollup.json, systemconfig
- SystemConfig: The SystemConfig contract helps manage configuration of an OP Stack network. Much of the network’s configuration is stored on L1 and picked up by L2 as part of the derivation of the L2 chain. The contract also contains references to all other contract addresses for the chain.
- genesis.json: 用来初始化L2的chain_id，链初始化的EOA和合约等初始状态
- rollup.json: 用来确定L2共识依赖的DA层合约的source of truth，包括batcher地址、systemconfig、区块儿时间等


```json
# rollup.json
{
  "genesis": {
    "l1": {
      "hash": "0xf39446e09aeca67452545d06a6e6a6a11184575ecf421f9306cf3602febf93ba",
      "number": 1
    },
    "l2": {
      "hash": "0x2a92ff72dad302d39fa80ef81522f0ccb27dc903255b618dfc4feddb22a8f80d",
      "number": 0
    },
    "l2_time": 1728358574,
    "system_config": {
      "batcherAddr": "0x3c44cdddb6a900fa2b585dd299e03d12fa4293bc",
      "overhead": "0x0000000000000000000000000000000000000000000000000000000000000834",
      "scalar": "0x00000000000000000000000000000000000000000000000000000000000f4240",
      "gasLimit": 30000000
    }
  },
  "block_time": 2,
  "max_sequencer_drift": 300,
  "seq_window_size": 200,
  "channel_timeout": 120,
  "l1_chain_id": 900,
  "l2_chain_id": 901,
  "regolith_time": 0,
  "canyon_time": 0,
  "delta_time": 0,
  "ecotone_time": 0,
  "fjord_time": 0,
  "batch_inbox_address": "0xff00000000000000000000000000000000000901",
  "deposit_contract_address": "0x55bdfb0bfef1070c457124920546359426153833",
  "l1_system_config_address": "0x3649f526889a918af0a5498706db29e81bc91e0c",
  "protocol_versions_address": "0x0000000000000000000000000000000000000000"
}
```

- op's devnet deployment guide(流程正确，但是部署细节没有): https://docs.optimism.io/index#deployment
    - 部署的时候要先部署[L1 contracts](https://docs.optimism.io/op-stack/protocol/smart-contracts#l1-contract-details)作为source of truth才能启动L2,
- devnet depolyment guide: https://github.com/QuarkChain/pm/blob/6509512378503de6cb4570603bd97743eba22a21/L2/devnet_fault_proof.md
- quarkchain mainnet deployment guide: https://github.com/QuarkChain/pm/pull/101/changes#diff-829716c3dc993a798d256f3be34bbe3c900b545c7b9a9022c05dd444db9b2e94
    - awesome mainnet launch todo list: https://github.com/QuarkChain/pm/issues/31
    - bootnode setup: https://github.com/QuarkChain/pm/blob/main/L2/mainnet_bootnode.md
    - test hardfork: https://github.com/QuarkChain/pm/blob/main/L2/hardfork_devnet_test.md
    - basic tests after launching a new node: https://github.com/QuarkChain/pm/issues/35