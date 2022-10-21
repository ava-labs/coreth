// SPDX-License-Identifier: MIT

pragma solidity ^0.8.0;

import "./access.sol";

interface IProxy {
    function setImplementation(address newImplementation) external;
}

contract CaminoIncentives is SimpleAccessControlImpl {
    address private constant ProxyAddress =
        0x010000000000000000000000000000000000000c;

    /**********************************/
    /************  Upgrade  ***********/
    /**********************************/

    function upgrade(address newImplementation) external onlyRole(ADMIN_ROLE) {
        // Call proxy via external call (msg.sender = this address)
        IProxy(payable(ProxyAddress)).setImplementation(newImplementation);
    }
}
