// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

// 7702 delegation contract
contract BatchCallDelegation {
    uint256 public x;
    bool initialized;

    struct Call {
        bytes data;
        address to;
        uint256 value;
    }

    function execute(Call[] calldata calls) external payable {
        require(msg.sender == addr(), "only self-call");
        for (uint256 i = 0; i < calls.length; i++) {
            Call memory call = calls[i];
            (bool success,) = call.to.call{value: call.value}(call.data);
            require(success, "call reverted");
        }
        x += 1;
    }

    function addr() public view returns (address) {
        return address(this);
    }

    // EIP-7702 does not provide developers the opportunity to run initcode and set storage slots during delegation. To secure the account from an observer front-running the initialization of the delegation with an account they control, smart contract wallet developers must verify the initial calldata to the account for setup purposes be signed by the EOA’s key using ecrecover.
    function initialize(bytes32 messageHash, bytes memory signature) public {
        if (!initialized) {
            // Extract r, s and v from the signature
            bytes32 r;
            bytes32 s;
            uint8 v;
            assembly {
                r := mload(add(signature, 0x20))
                s := mload(add(signature, 0x40))
                v := byte(0, mload(add(signature, 0x60)))
            }

            // Recover the signer address
            address signer = ecrecover(messageHash, v, r, s);
            require(signer == address(this), "Invalid signer");

            // Initialization logic here
            initialized = true;
        }
    }

    // caution: this function is vulnerable to transfer the money from the EOA contract
    function transfer(address to) external {
        require(address(this).balance >= 1 ether, "Insufficient balance");
        (bool success,) = to.call{value: 1 ether}("");
        require(success, "Transfer failed");
    }
}
