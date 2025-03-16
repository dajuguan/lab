// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

// 7702 delegation contract 
contract BatchCallDelegation {
  struct Call {
    bytes data;
    address to;
    uint256 value;
  }
 
  function execute(Call[] calldata calls) external payable {
    for (uint256 i = 0; i < calls.length; i++) {
      Call memory call = calls[i];
      (bool success, ) = call.to.call{value: call.value}(call.data);
      require(success, "call reverted");
    }
  }
}

/*
anvil --hardfork prague -p 6601

ADDR=0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266
PK=0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80
SPONSOR_PK=0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d
SPONSOR=0x70997970C51812dc3A010C7d01b50e0d17dc79C8
L1=http://localhost:6601

# deploy delegation contract
forge create src/BatchCallDelegation.sol:BatchCallDelegation --rpc-url $L1 --private-key $PK --broadcast
CA=0x9fE46736679d2D9a65F0992F2272dE9f3c7fa6e0
// cast rpc anvil_setBalance $ADDR 0 -r $L1

cast balance $ADDR -r $L1

# sponsored by others
nonce=$(cast nonce $ADDR -r $L1)
cast send $(cast az) --private-key $SPONSOR_PK  --auth $(cast wallet sign-auth $CA --nonce $nonce --chain 31337 --private-key $PK) -r $L1
# self sponsor, az
cast send $(cast az) --private-key $SPONSOR_PK --auth $CA -r $L1  

cast code $ADDR -r $L1

# call Smart EOA
R1=0xcb98643b8786950F0461f3B0edf99D88F274574D
R2=0xd2135CfB216b74109775236E36d4b433F1DF507B
cast balance $ADDR -r $L1
cast send $ADDR "execute((bytes,address,uint256)[] calldata calls)" "[(0x,$R1,10000),(0x,$R2,10000)]" --private-key $PK  -r $L1
cast balance $R1 -r $L1
cast balance $R2 -r $L1
cast balance $ADDR -r $L1
*/