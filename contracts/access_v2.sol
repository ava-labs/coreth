// SPDX-License-Identifier: MIT

pragma solidity ^0.8.0;

abstract contract SimpleAccessControl {
    //address -> role mapping Slot #0
    mapping(address => uint256) private _roles;

    modifier onlyRole(uint256 role) {
        require(_hasRole(msg.sender, role), "Access denied");
        _;
    }

    /**********************************/
    /************  Events  ************/
    /**********************************/

    event SetRole(address addr, uint256 role);
    event DropRole(address addr, uint256 role);

    /**********************************/
    /********  Implementation  ********/
    /**********************************/

    // Return the last possible role
    function _lastRole() internal pure virtual returns (uint256);

    // Apply a new Role to the adress
    function _setRole(address addr, uint256 role) internal {
        require(role <= _lastRole(), "Unknown Role");
        _roles[addr] |= role;

        emit SetRole(addr, role);
    }

    // Drops a Role from the adress
    function _dropRole(address addr, uint256 role) internal {
        _roles[addr] &= ~role;

        emit DropRole(addr, role);
    }

    // Query if role is applied to address
    function _hasRole(address addr, uint256 role) internal view returns (bool) {
        return (_roles[addr] & role) != 0;
    }

    // Query roles applied to address
    function _getRoles(address addr) internal view returns (uint256) {
        return _roles[addr];
    }
}

contract SimpleAccessControlImpl is SimpleAccessControl {
    // Our predefined roles
    uint256 internal constant ADMIN_ROLE = 1 << 0;

    /**********************************/
    /*******  Implementation  *********/
    /**********************************/

    function _lastRole() internal pure virtual override returns (uint256) {
        return ADMIN_ROLE;
    }

    function grantRole(address addr, uint256 role)
    external
    onlyRole(ADMIN_ROLE)
    {
        _setRole(addr, role);
    }

    function revokeRole(address addr, uint256 role)
    external
    onlyRole(ADMIN_ROLE)
    {
        if (addr == msg.sender) {
            require(ADMIN_ROLE != role, "Cannot revoke ADMIN_ROLE from yourself");
        }
        _dropRole(addr, role);
    }

    function hasRole(address addr, uint256 role) external view returns (bool) {
        return _hasRole(addr, role);
    }

    function getRoles(address addr) external view returns (uint256) {
        return _getRoles(addr);
    }
}
