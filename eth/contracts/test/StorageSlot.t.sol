// https://docs.soliditylang.org/en/latest/internals/layout_in_storage.html#mappings-and-dynamic-arrays

// SPDX-License-Identifier: UNLICENSED
// ref: https://github.com/ralexstokes/4788asm/tree/main
pragma solidity ^0.8.13;

import "forge-std/Test.sol";
import {console} from "forge-std/console.sol";

contract TestStorage is Test {
    // start from 0x20= 32
    uint256 public x;
    uint256 public y;
    uint256[] dynArray;

    function setUp() public {
        dynArray.push(1);
        dynArray.push(2);
        dynArray.push(3);
    }

    function testUintStorage() public {
        uint256 slot;
        assembly {
            slot := y.slot
        }
        console.log("slot y", slot);
        assertEq(slot, 0x20 + 1);
    }

    function testDynArrStorage() public {
        uint256 arraySlot;
        assembly {
            arraySlot := dynArray.slot
        }
        uint256 slot0 = uint256(keccak256(abi.encodePacked(arraySlot)));
        uint256 slot1 = slot0 + 1;
        uint256 value1;
        assembly {
            value1 := sload(slot1)
        }
        console.log("dynamic array index 0 slot:", slot0);
        console.log("dynamic array index 1 slot:", slot1);
        console.log("value at dynamic array index 1 slot:", value1);
        assertEq(value1, dynArray[1]);
    }
}
