// SPDX-License-Identifier: MIT

pragma solidity ^0.8.0;

import "./access.sol";

interface IProxy {
    function setImplementation(address newImplementation) external;
}

contract CaminoAdmin is SimpleAccessControlImpl {
    address private constant ProxyAddress =
        0x010000000000000000000000000000000000000a;

    // Our predefined roles
    uint256 internal constant GAS_FEE_ROLE = 1 << 1;
    uint256 internal constant KYC_ROLE = 1 << 2;
    uint256 internal constant BLACKLIST_ROLE = 1 << 3;
    uint256 internal constant LAST_ROLE = BLACKLIST_ROLE;

    // Slot0 used by SimpleAccess mapping

    // Slot1
    uint256 private baseGasFee;

    uint256 internal constant KYC_APPROVED = 1 << 0;
    uint256 internal constant KYC_EXPIRED = 1 << 1;

    // Slot2
    mapping(address => uint256) private kyc;

    // Slot3
    uint256 internal constant BLACKLISTED = 1 << 0;
    // address.funcSig (20+12bytes) => FuncSigs
    mapping(uint256 => uint256) private blacklist;

    /**********************************/
    /************  Events  ************/
    /**********************************/

    event GasFeeSet(uint256 newGasFee);
    event KycStateChanged(
        address indexed account,
        uint256 oldState,
        uint256 newState
    );

    /**********************************/
    /************  Upgrade  ***********/
    /**********************************/

    function upgrade(address newImplementation) external onlyRole(ADMIN_ROLE) {
        // Call proxy via external call (msg.sender = this address)
        IProxy(payable(ProxyAddress)).setImplementation(newImplementation);
    }

    /**********************************/
    /*************  Access  ***********/
    /**********************************/

    function _lastRole() internal pure override returns (uint256) {
        return LAST_ROLE;
    }

    /**********************************/
    /************  Gas Fees  **********/
    /**********************************/

    function getBaseFee() external view returns (uint256) {
        return baseGasFee;
    }

    function setBaseFee(uint256 newFee) external onlyRole(GAS_FEE_ROLE) {
        baseGasFee = newFee;

        emit GasFeeSet(newFee);
    }

    /**********************************/
    /**************  KYC  *************/
    /**********************************/

    /* @dev Add / Remove KYC states for a signed account */
    function applyKycState(
        address account,
        bool remove,
        uint256 state
    ) external onlyRole(KYC_ROLE) {
        uint256 oldState = kyc[account];
        uint256 newState = remove ? oldState & ~state : oldState | state;

        kyc[account] = newState;
        emit KycStateChanged(account, oldState, newState);
    }

    function getKycState(address account) external view returns (uint256) {
        return kyc[account];
    }

    /**********************************/
    /***********  BlackList  **********/
    /**********************************/

    function setBlacklistState(
        address account,
        bytes4 signature,
        uint256 state
    ) external onlyRole(BLACKLIST_ROLE) {
        blacklist[_packAccountSig(account, signature)] = state;
    }

    function getBlacklistState(address account, bytes4 signature)
        external
        view
        returns (uint256)
    {
        return blacklist[_packAccountSig(account, signature)];
    }

    function _packAccountSig(address account, bytes4 signature)
        internal
        pure
        returns (uint256)
    {
        return uint256(uint160(account)) | (uint256(uint32(signature)) << 20);
    }
}
