// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethclient

import (
	"context"
	"math/big"

	"github.com/ava-labs/coreth/accounts/abi/bind"
	"github.com/ava-labs/coreth/interfaces"
	"github.com/ava-labs/coreth/rpc"
	ethereum "github.com/ava-labs/libevm"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/types"

	// Force-load precompiles to trigger registration
	_ "github.com/ava-labs/coreth/precompile/registry"
)

// Verify that [Client] implements required interfaces
var (
	_ bind.AcceptedContractCaller = (*client)(nil)
	_ bind.ContractBackend        = (*client)(nil)
	_ bind.ContractFilterer       = (*client)(nil)
	_ bind.ContractTransactor     = (*client)(nil)
	_ bind.DeployBackend          = (*client)(nil)

	_ ethereum.ChainReader              = (*client)(nil)
	_ ethereum.ChainStateReader         = (*client)(nil)
	_ ethereum.TransactionReader        = (*client)(nil)
	_ ethereum.TransactionSender        = (*client)(nil)
	_ ethereum.ContractCaller           = (*client)(nil)
	_ ethereum.GasEstimator             = (*client)(nil)
	_ ethereum.GasPricer                = (*client)(nil)
	_ ethereum.LogFilterer              = (*client)(nil)
	_ interfaces.AcceptedStateReader    = (*client)(nil)
	_ interfaces.AcceptedContractCaller = (*client)(nil)
)

// Client defines interface for typed wrappers for the Ethereum RPC API.
type Client interface {
	Client() *rpc.Client
	Close()
	ChainID(context.Context) (*big.Int, error)
	BlockByHash(context.Context, common.Hash) (*types.Block, error)
	BlockByNumber(context.Context, *big.Int) (*types.Block, error)
	BlockNumber(context.Context) (uint64, error)
	BlockReceipts(context.Context, rpc.BlockNumberOrHash) ([]*types.Receipt, error)
	HeaderByHash(context.Context, common.Hash) (*types.Header, error)
	HeaderByNumber(context.Context, *big.Int) (*types.Header, error)
	TransactionByHash(context.Context, common.Hash) (tx *types.Transaction, isPending bool, err error)
	TransactionSender(context.Context, *types.Transaction, common.Hash, uint) (common.Address, error)
	TransactionCount(context.Context, common.Hash) (uint, error)
	TransactionInBlock(context.Context, common.Hash, uint) (*types.Transaction, error)
	TransactionReceipt(context.Context, common.Hash) (*types.Receipt, error)
	SyncProgress(ctx context.Context) error
	SubscribeNewAcceptedTransactions(context.Context, chan<- *common.Hash) (ethereum.Subscription, error)
	SubscribeNewPendingTransactions(context.Context, chan<- *common.Hash) (ethereum.Subscription, error)
	SubscribeNewHead(context.Context, chan<- *types.Header) (ethereum.Subscription, error)
	NetworkID(context.Context) (*big.Int, error)
	BalanceAt(context.Context, common.Address, *big.Int) (*big.Int, error)
	BalanceAtHash(ctx context.Context, account common.Address, blockHash common.Hash) (*big.Int, error)
	StorageAt(context.Context, common.Address, common.Hash, *big.Int) ([]byte, error)
	StorageAtHash(ctx context.Context, account common.Address, key common.Hash, blockHash common.Hash) ([]byte, error)
	CodeAt(context.Context, common.Address, *big.Int) ([]byte, error)
	CodeAtHash(ctx context.Context, account common.Address, blockHash common.Hash) ([]byte, error)
	NonceAt(context.Context, common.Address, *big.Int) (uint64, error)
	NonceAtHash(ctx context.Context, account common.Address, blockHash common.Hash) (uint64, error)
	FilterLogs(context.Context, ethereum.FilterQuery) ([]types.Log, error)
	SubscribeFilterLogs(context.Context, ethereum.FilterQuery, chan<- types.Log) (ethereum.Subscription, error)
	AcceptedCodeAt(context.Context, common.Address) ([]byte, error)
	AcceptedNonceAt(context.Context, common.Address) (uint64, error)
	AcceptedCallContract(context.Context, ethereum.CallMsg) ([]byte, error)
	CallContract(context.Context, ethereum.CallMsg, *big.Int) ([]byte, error)
	CallContractAtHash(ctx context.Context, msg ethereum.CallMsg, blockHash common.Hash) ([]byte, error)
	SuggestGasPrice(context.Context) (*big.Int, error)
	SuggestGasTipCap(context.Context) (*big.Int, error)
	FeeHistory(ctx context.Context, blockCount uint64, lastBlock *big.Int, rewardPercentiles []float64) (*ethereum.FeeHistory, error)
	EstimateGas(context.Context, ethereum.CallMsg) (uint64, error)
	EstimateBaseFee(context.Context) (*big.Int, error)
	SendTransaction(context.Context, *types.Transaction) error
}

// SubscribeNewAcceptedTransactions subscribes to notifications about the accepted transaction hashes on the given channel.
func (ec *client) SubscribeNewAcceptedTransactions(ctx context.Context, ch chan<- *common.Hash) (ethereum.Subscription, error) {
	sub, err := ec.c.EthSubscribe(ctx, ch, "newAcceptedTransactions")
	if err != nil {
		// Defensively prefer returning nil interface explicitly on error-path, instead
		// of letting default golang behavior wrap it with non-nil interface that stores
		// nil concrete type value.
		return nil, err
	}
	return sub, nil
}

// SubscribeNewPendingTransactions subscribes to notifications about the pending transaction hashes on the given channel.
func (ec *client) SubscribeNewPendingTransactions(ctx context.Context, ch chan<- *common.Hash) (ethereum.Subscription, error) {
	sub, err := ec.c.EthSubscribe(ctx, ch, "newPendingTransactions")
	if err != nil {
		// Defensively prefer returning nil interface explicitly on error-path, instead
		// of letting default golang behavior wrap it with non-nil interface that stores
		// nil concrete type value.
		return nil, err
	}
	return sub, nil
}

// AcceptedCodeAt returns the contract code of the given account in the accepted state.
func (ec *client) AcceptedCodeAt(ctx context.Context, account common.Address) ([]byte, error) {
	return ec.CodeAt(ctx, account, nil)
}

// AcceptedNonceAt returns the account nonce of the given account in the accepted state.
// This is the nonce that should be used for the next transaction.
func (ec *client) AcceptedNonceAt(ctx context.Context, account common.Address) (uint64, error) {
	return ec.NonceAt(ctx, account, nil)
}

// AcceptedCallContract executes a message call transaction in the accepted
// state.
func (ec *client) AcceptedCallContract(ctx context.Context, msg ethereum.CallMsg) ([]byte, error) {
	return ec.CallContract(ctx, msg, nil)
}

// EstimateBaseFee tries to estimate the base fee for the next block if it were created
// immediately. There is no guarantee that this will be the base fee used in the next block
// or that the next base fee will be higher or lower than the returned value.
func (ec *client) EstimateBaseFee(ctx context.Context) (*big.Int, error) {
	var hex hexutil.Big
	err := ec.c.CallContext(ctx, &hex, "eth_baseFee")
	if err != nil {
		return nil, err
	}
	return (*big.Int)(&hex), nil
}
