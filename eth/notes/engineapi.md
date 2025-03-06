- [newPayLoad](https://github.com/ethereum/go-ethereum/blob/9b68875d68b409eb2efdb68a4b623aaacc10a5b6/eth/catalyst/api.go#L828)
    - api.eth.BlockChain().InsertBlockWithoutSetHead(block, witness)
        - bc.insertChain(types.Blocks{block}, false, makeWitness)
            - bc.processBlock(block, statedb, start, setHead)
                - bc.processor.Process(block, statedb, bc.vmConfig)
                - [EVM processor](https://github.com/ethereum/go-ethereum/blob/67a3b087951a3f3a8e341ae32b6ec18f3553e5cc/core/state_processor.go#L57)

- forkChoiceUpdate的作用:
    1. 包含payloadAttributes: build a block
    2. 不包含: 通知EL同步到最新的块高