// precompile: precompiles added in different fork: https://github.com/ethereum/go-ethereum/blob/master/core/vm/contracts.go#L120

// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.13;

import "forge-std/Test.sol";
import {console} from "forge-std/console.sol";

// precompile related functions cannot be pure because the solidity compiler has no way of inferring that a staticcall won’t change the state.
contract PrecompileTest is Test {
    function testPrecompile() public view {
        // Store timestamp and root at expected indexes.
        bytes memory data = hex"FF";
        (bool ok, bytes memory out) = address(2).staticcall(data);
        require(ok);
        bytes32 h = abi.decode(out, (bytes32));
        // https://www.evm.codes/precompiled?fork=cancun#0x02
        require(h == 0xa8100ae6aa1940d0b663bb31cd466142ebbdbd5187131b92d93818987832eb89);
    }
}
