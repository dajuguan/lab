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