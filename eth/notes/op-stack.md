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


```

> So, [archive node](https://docs.optimism.io/chain-operators/guides/management/best-practices#op-proposer-assumes-archive-mode) must be used for L2 withdraw.

### If Deposit transaction revert when _value > msg.value, will the fund be locked in L1 forever?
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

### How to filter invalid msg sent to Batch Inbox Address