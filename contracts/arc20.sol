// SPDX-License-Identifier: MIT

pragma solidity >=0.6.0 <0.8.0;

import {NativeAssets} from "./nativeAssets.sol";

contract ARC20 {

    mapping (address => uint256) private _balances;
    mapping(address => mapping(address => uint256)) private _allowances;

    uint256 private _assetID;

    uint256 private _totalSupply;

    string private _name;
    string private _symbol;
    uint8 private _decimals;

    constructor(string memory name_, string memory symbol_, uint8 decimals_, uint256 assetID_) public {
        _name = name_;
        _symbol = symbol_;
        _decimals = decimals_;
        _assetID = assetID_;
    }

    /**
     * @dev Returns the name of the token.
     */
    function name() public view returns (string memory) {
        return _name;
    }

    /**
     * @dev Returns the symbol of the token, usually a shorter version of the
     * name.
     */
    function symbol() public view returns (string memory) {
        return _symbol;
    }

    /**
     * @dev Returns the number of decimals used to represent the token.
     */
    function decimals() public view returns (uint8) {
        return _decimals;
    }

    /**
     * @dev Returns the total supply of `assetID` currently held by
     * this contract.
     */
    function totalSupply() public view returns (uint256) {
        return _totalSupply;
    }

    /**
     * @dev Returns the balance of `account` held in this contract.
     */
    function balanceOf(address account) public view returns (uint256) {
        return _balances[account];
    }

    // Withdrawal/Deposit functionality

    /**
     * @dev Acknowledges the receipt of some amount of an Avalanche Native Token
     * into the contract implementing this interface.
     */
    function deposit() public {
        uint256 updatedBalance = NativeAssets.assetBalance(address(this), _assetID);
        uint256 depositAmount = updatedBalance - _totalSupply;
        assert(depositAmount >= 0);

        _balances[msg.sender] += depositAmount;
        _totalSupply = updatedBalance;
        emit Deposit(msg.sender, depositAmount);
    }

    /**
     * @dev Emitted when `value` tokens are deposited from `depositor`
     */
    event Deposit(address indexed depositor, uint256 value);

    /**
     * @dev Withdraws `value` of the underlying asset to the contract
     * caller.
     */
    function withdraw(uint256 value) public {
        require(_balances[msg.sender] >= value, "Insufficient funds for withdrawal");
        
        _balances[msg.sender] -= value;
        _totalSupply -= value;

        NativeAssets.assetCall(msg.sender, _assetID, value, "");
        emit Withdrawal(msg.sender, value);
    }

    /**
     * @dev Emitted when `value` tokens are withdrawn to `withdrawer`
     */
    event Withdrawal(address indexed withdrawer, uint256 value);

    /**
     * @dev Returns the `assetID` of the underlying asset this contract handles.
     */
    function assetID() external view returns (uint256) {
        return _assetID;
    }

    event Transfer(address indexed from, address indexed to, uint256 value);

    function transfer(address to, uint256 value) public returns (bool success) {
        require(_balances[msg.sender] >= value, "insufficient balance for transfer");

        _balances[msg.sender] -= value;  // deduct from sender's balance
        _balances[to] += value;          // add to recipient's balance
        emit Transfer(msg.sender, to, value);
        return true;
    }

    event Approval(address indexed owner, address indexed spender, uint256 value);

    function approve(address spender, uint256 value)
        public
        returns (bool success)
    {
        _allowances[msg.sender][spender] = value;
        emit Approval(msg.sender, spender, value);
        return true;
    }

    function transferFrom(address from, address to, uint256 value)
        public
        returns (bool success)
    {
        require(value <= _balances[from], "From address has insufficient balance to transfer");
        require(value <= _allowances[from][msg.sender], "Insufficient allowance granted to sender");

        _balances[from] -= value;
        _balances[to] += value;
        _allowances[from][msg.sender] -= value;
        emit Transfer(from, to, value);
        return true;
    }

    function allowance(address owner, address spender) public view returns (uint256) {
        return _allowances[owner][spender];
    }
}
