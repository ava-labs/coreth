// SPDX-License-Identifier: MIT

pragma solidity ^0.8.0;

contract MultisigData {
    struct Owners {
        uint256 threshold;
        address[] ctrlGroup;
    }

    mapping (address => Owners) public aliases;

    function getAlias(address key) public view returns (Owners memory) {
        return aliases[key];
    }
}
