// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "forge-std/Test.sol";

contract Caller {
    // The name of the variable doesn’t matter, but where it is located in storage.
    uint256 public xx;
    address public callee;

    constructor(address _callee) {
        callee = _callee;
        xx = 0;
    }

    function inc() public returns (uint256) {
        xx += 1;
        console.log("caller called");
        return xx;
    }

    function callIncrement() public {
        callee.call(abi.encodeWithSignature("inc()"));
    }

    function delegateCallIncrement() public {
        callee.delegatecall(abi.encodeWithSignature("inc()"));
    }
}

contract Callee {
    uint256 public x;

    constructor() {
        x = 0;
    }

    function inc() public returns (uint256) {
        x += 1;
        console.log("callee called");
        return x;
    }
}

contract CallTest is Test {
    Caller caller;
    Callee callee;

    function setUp() public {
        callee = new Callee();
        caller = new Caller(address(callee));
    }

    // function testCall() public {
    //     caller.callIncrement();
    //     assertEq(caller.x(), 0);
    //     assertEq(callee.x(), 1);
    // }

    function testDelegateCall() public {
        caller.delegateCallIncrement();
        assertEq(caller.xx(), 1);
        assertEq(callee.x(), 0);
    }
}
