// (c) 2025 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/event"
	"github.com/ava-labs/libevm/triedb"
)

func (bc *BlockChain) CurrentHeader() *types.Header {
	return bc.ethBlockChain.CurrentHeader()
}

func (bc *BlockChain) CurrentBlock() *types.Header {
	return bc.ethBlockChain.CurrentBlock()
}

func (bc *BlockChain) CurrentFinalBlock() *types.Header {
	return bc.ethBlockChain.CurrentFinalBlock()
}

func (bc *BlockChain) GetHeader(hash common.Hash, number uint64) *types.Header {
	return bc.ethBlockChain.GetHeader(hash, number)
}

func (bc *BlockChain) GetHeaderByHash(hash common.Hash) *types.Header {
	return bc.ethBlockChain.GetHeaderByHash(hash)
}

func (bc *BlockChain) GetHeaderByNumber(number uint64) *types.Header {
	return bc.ethBlockChain.GetHeaderByNumber(number)
}

func (bc *BlockChain) GetBody(hash common.Hash) *types.Body {
	return bc.ethBlockChain.GetBody(hash)
}

func (bc *BlockChain) HasBlock(hash common.Hash, number uint64) bool {
	return bc.ethBlockChain.HasBlock(hash, number)
}

func (bc *BlockChain) GetBlock(hash common.Hash, number uint64) *types.Block {
	return bc.ethBlockChain.GetBlock(hash, number)
}

func (bc *BlockChain) GetBlockByHash(hash common.Hash) *types.Block {
	return bc.ethBlockChain.GetBlockByHash(hash)
}

func (bc *BlockChain) GetBlockByNumber(number uint64) *types.Block {
	return bc.ethBlockChain.GetBlockByNumber(number)
}

func (bc *BlockChain) GetReceiptsByHash(hash common.Hash) types.Receipts {
	return bc.ethBlockChain.GetReceiptsByHash(hash)
}

func (bc *BlockChain) GetCanonicalHash(number uint64) common.Hash {
	return bc.ethBlockChain.GetCanonicalHash(number)
}

func (bc *BlockChain) GetTransactionLookup(hash common.Hash) (*rawdb.LegacyTxLookupEntry, *types.Transaction, error) {
	return bc.ethBlockChain.GetTransactionLookup(hash)
}

func (bc *BlockChain) HasState(hash common.Hash) bool {
	return bc.ethBlockChain.HasState(hash)
}

func (bc *BlockChain) State() (*state.StateDB, error) {
	return bc.ethBlockChain.State()
}

func (bc *BlockChain) StateAt(root common.Hash) (*state.StateDB, error) {
	return bc.ethBlockChain.StateAt(root)
}

func (b *BlockChain) Config() *params.ChainConfig {
	return b.ethBlockChain.Config()
}

func (b *BlockChain) Engine() consensus.Engine {
	return b.ethBlockChain.Engine()
}

func (bc *BlockChain) Snapshots() *snapshot.Tree {
	return bc.ethBlockChain.Snapshots()
}

func (bc *BlockChain) Processor() Processor {
	return bc.ethBlockChain.Processor()
}

func (bc *BlockChain) Genesis() *types.Block {
	return bc.ethBlockChain.Genesis()
}

func (bc *BlockChain) GetVMConfig() *vm.Config {
	return bc.ethBlockChain.GetVMConfig()
}

func (bc *BlockChain) TrieDB() *triedb.Database {
	return bc.ethBlockChain.TrieDB()
}

func (bc *BlockChain) SubscribeRemovedLogsEvent(ch chan<- RemovedLogsEvent) event.Subscription {
	return bc.ethBlockChain.SubscribeRemovedLogsEvent(ch)
}

func (bc *BlockChain) SubscribeChainEvent(ch chan<- ChainEvent) event.Subscription {
	return bc.ethBlockChain.SubscribeChainEvent(ch)
}

func (bc *BlockChain) SubscribeChainHeadEvent(ch chan<- ChainHeadEvent) event.Subscription {
	return bc.ethBlockChain.SubscribeChainHeadEvent(ch)
}

func (bc *BlockChain) SubscribeChainSideEvent(ch chan<- ChainSideEvent) event.Subscription {
	return bc.ethBlockChain.SubscribeChainSideEvent(ch)
}

func (bc *BlockChain) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return bc.ethBlockChain.SubscribeLogsEvent(ch)
}

// SubscribeChainAcceptedEvent registers a subscription of ChainEvent.
func (bc *BlockChain) SubscribeChainAcceptedEvent(ch chan<- ChainEvent) event.Subscription {
	return bc.scope.Track(bc.chainAcceptedFeed.Subscribe(ch))
}

// SubscribeAcceptedLogsEvent registers a subscription of accepted []*types.Log.
func (bc *BlockChain) SubscribeAcceptedLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return bc.scope.Track(bc.logsAcceptedFeed.Subscribe(ch))
}

// SubscribeAcceptedTransactionEvent registers a subscription of accepted transactions
func (bc *BlockChain) SubscribeAcceptedTransactionEvent(ch chan<- NewTxsEvent) event.Subscription {
	return bc.scope.Track(bc.txAcceptedFeed.Subscribe(ch))
}

// GetLogs fetches all logs from a given block.
func (bc *BlockChain) GetLogs(hash common.Hash, number uint64) [][]*types.Log {
	logs, ok := bc.acceptedLogsCache.Get(hash) // this cache is thread-safe
	if ok {
		return logs
	}
	block := bc.GetBlockByHash(hash)
	if block == nil {
		return nil
	}
	logs = bc.collectUnflattenedLogs(block, false)
	return logs
}
