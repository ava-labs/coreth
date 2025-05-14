// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sync"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	avalanchedatabase "github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	avalanchegossip "github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	avalanchecommon "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	atomicstate "github.com/ava-labs/coreth/plugin/evm/atomic/state"
	atomicsync "github.com/ava-labs/coreth/plugin/evm/atomic/sync"
	"github.com/ava-labs/coreth/plugin/evm/atomic/txpool"
	"github.com/ava-labs/coreth/plugin/evm/config"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	"github.com/ava-labs/coreth/plugin/evm/gossip"
	customheader "github.com/ava-labs/coreth/plugin/evm/header"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap5"
	"github.com/ava-labs/coreth/plugin/evm/vmerrors"
	"github.com/ava-labs/coreth/utils"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"
)

var (
	_ secp256k1fx.VM                     = &VM{}
	_ block.ChainVM                      = &VM{}
	_ block.BuildBlockWithContextChainVM = &VM{}
	_ block.StateSyncableVM              = &VM{}
)

const (
	secpCacheSize       = 1024
	defaultMempoolSize  = 4096
	targetAtomicTxsSize = 40 * units.KiB
	// maxAtomicTxMempoolGas is the maximum amount of gas that is allowed to be
	// used by an atomic transaction in the mempool. It is allowed to build
	// blocks with larger atomic transactions, but they will not be accepted
	// into the mempool.
	maxAtomicTxMempoolGas   = ap5.AtomicGasLimit
	maxUTXOsToFetch         = 1024
	atomicTxGossipNamespace = "atomic_tx_gossip"
	avaxEndpoint            = "/avax"
)

type VM struct {
	// TODO: decide if we want to directly import the evm package and VM struct
	extension.InnerVM

	secpCache *secp256k1.RecoverCache
	baseCodec codec.Registry
	mempool   *txpool.Mempool
	fx        secp256k1fx.Fx
	ctx       *snow.Context

	// [atomicTxRepository] maintains two indexes on accepted atomic txs.
	// - txID to accepted atomic tx
	// - block height to list of atomic txs accepted on block at that height
	atomicTxRepository *atomicstate.AtomicRepository
	// [atomicBackend] abstracts verification and processing of atomic transactions
	atomicBackend *atomicstate.AtomicBackend

	atomicTxGossipHandler p2p.Handler
	atomicTxPushGossiper  *avalanchegossip.PushGossiper[*atomic.GossipAtomicTx]
	atomicTxPullGossiper  avalanchegossip.Gossiper

	// [cancel] may be nil until [snow.NormalOp] starts
	cancel     context.CancelFunc
	shutdownWg sync.WaitGroup

	clock mockable.Clock
}

func WrapVM(vm extension.InnerVM) *VM {
	return &VM{InnerVM: vm}
}

// Initialize implements the snowman.ChainVM interface
func (vm *VM) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	db avalanchedatabase.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- avalanchecommon.Message,
	fxs []*avalanchecommon.Fx,
	appSender avalanchecommon.AppSender,
) error {
	vm.ctx = chainCtx

	var extDataHashes map[common.Hash]common.Hash
	// Set the chain config for mainnet/fuji chain IDs
	switch chainCtx.NetworkID {
	case constants.MainnetID:
		extDataHashes = mainnetExtDataHashes
	case constants.FujiID:
		extDataHashes = fujiExtDataHashes
	}
	// Free the memory of the extDataHash map
	fujiExtDataHashes = nil
	mainnetExtDataHashes = nil

	networkCodec, err := message.NewCodec(atomicsync.AtomicSyncSummary{})
	if err != nil {
		return fmt.Errorf("failed to create codec manager: %w", err)
	}

	// Create the atomic extension structs
	// some of them need to be initialized after the inner VM is initialized
	blockExtender := newBlockExtender(extDataHashes, vm)
	syncExtender := &atomicsync.AtomicSyncExtender{}
	syncProvider := &atomicsync.AtomicSummaryProvider{}
	// Create and pass the leaf handler to the atomic extension
	// it will be initialized after the inner VM is initialized
	leafHandler := atomicsync.NewAtomicLeafHandler()
	atomicLeafTypeConfig := &extension.LeafRequestConfig{
		LeafType:   atomicsync.AtomicTrieNode,
		MetricName: "sync_atomic_trie_leaves",
		Handler:    leafHandler,
	}
	vm.mempool = &txpool.Mempool{}

	extensionConfig := &extension.Config{
		NetworkCodec:               networkCodec,
		ConsensusCallbacks:         vm.createConsensusCallbacks(),
		BlockExtender:              blockExtender,
		SyncableParser:             atomicsync.NewAtomicSyncSummaryParser(),
		SyncExtender:               syncExtender,
		SyncSummaryProvider:        syncProvider,
		ExtraSyncLeafHandlerConfig: atomicLeafTypeConfig,
		ExtraMempool:               vm.mempool,
		Clock:                      &vm.clock,
	}
	if err := vm.SetExtensionConfig(extensionConfig); err != nil {
		return fmt.Errorf("failed to set extension config: %w", err)
	}

	// Initialize inner vm with the provided parameters
	if err := vm.InnerVM.Initialize(
		ctx,
		chainCtx,
		db,
		genesisBytes,
		upgradeBytes,
		configBytes,
		toEngine,
		fxs,
		appSender,
	); err != nil {
		return fmt.Errorf("failed to initialize inner VM: %w", err)
	}

	// Now we can initialize the mempool and so
	err = vm.mempool.Initialize(chainCtx, vm.MetricRegistry(), defaultMempoolSize, vm.verifyTxAtTip)
	if err != nil {
		return fmt.Errorf("failed to initialize mempool: %w", err)
	}

	// initialize bonus blocks on mainnet
	var (
		bonusBlockHeights map[uint64]ids.ID
	)
	if vm.ctx.NetworkID == constants.MainnetID {
		bonusBlockHeights, err = readMainnetBonusBlocks()
		if err != nil {
			return fmt.Errorf("failed to read mainnet bonus blocks: %w", err)
		}
	}

	// initialize atomic repository
	lastAcceptedHash, lastAcceptedHeight, err := vm.ReadLastAccepted()
	if err != nil {
		return fmt.Errorf("failed to read last accepted block: %w", err)
	}
	vm.atomicTxRepository, err = atomicstate.NewAtomicTxRepository(vm.VersionDB(), atomic.Codec, lastAcceptedHeight)
	if err != nil {
		return fmt.Errorf("failed to create atomic repository: %w", err)
	}
	vm.atomicBackend, err = atomicstate.NewAtomicBackend(
		vm.ctx.SharedMemory, bonusBlockHeights,
		vm.atomicTxRepository, lastAcceptedHeight, lastAcceptedHash,
		vm.Config().CommitInterval,
	)
	if err != nil {
		return fmt.Errorf("failed to create atomic backend: %w", err)
	}

	// Atomic backend is available now, we can initialize structs that depend on it
	syncProvider.Initialize(vm.atomicBackend.AtomicTrie())
	syncExtender.Initialize(vm.atomicBackend, vm.atomicBackend.AtomicTrie(), vm.Config().StateSyncRequestSize)
	leafHandler.Initialize(vm.atomicBackend.AtomicTrie().TrieDB(), atomicstate.AtomicTrieKeyLength, networkCodec)
	vm.secpCache = secp256k1.NewRecoverCache(secpCacheSize)

	// so [vm.baseCodec] is a dummy codec use to fulfill the secp256k1fx VM
	// interface. The fx will register all of its types, which can be safely
	// ignored by the VM's codec.
	vm.baseCodec = linearcodec.NewDefault()
	return vm.fx.Initialize(vm)
}

func (vm *VM) SetState(ctx context.Context, state snow.State) error {
	switch state {
	case snow.Bootstrapping:
		if err := vm.onBootstrapStarted(); err != nil {
			return err
		}
	case snow.NormalOp:
		if err := vm.onNormalOperationsStarted(); err != nil {
			return err
		}
	}

	return vm.InnerVM.SetState(ctx, state)
}

func (vm *VM) onBootstrapStarted() error {
	return vm.fx.Bootstrapping()
}

func (vm *VM) onNormalOperationsStarted() error {
	if vm.IsBootstrapped() {
		return nil
	}
	if err := vm.fx.Bootstrapped(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.TODO())
	vm.cancel = cancel
	atomicTxGossipMarshaller := atomic.GossipAtomicTxMarshaller{}
	atomicTxGossipClient := vm.NewClient(p2p.AtomicTxGossipHandlerID, p2p.WithValidatorSampling(vm.Validators()))
	atomicTxGossipMetrics, err := avalanchegossip.NewMetrics(vm.MetricRegistry(), atomicTxGossipNamespace)
	if err != nil {
		return fmt.Errorf("failed to initialize atomic tx gossip metrics: %w", err)
	}

	pushGossipParams := avalanchegossip.BranchingFactor{
		StakePercentage: vm.Config().PushGossipPercentStake,
		Validators:      vm.Config().PushGossipNumValidators,
		Peers:           vm.Config().PushGossipNumPeers,
	}
	pushRegossipParams := avalanchegossip.BranchingFactor{
		Validators: vm.Config().PushRegossipNumValidators,
		Peers:      vm.Config().PushRegossipNumPeers,
	}

	if vm.atomicTxPushGossiper == nil {
		vm.atomicTxPushGossiper, err = avalanchegossip.NewPushGossiper[*atomic.GossipAtomicTx](
			atomicTxGossipMarshaller,
			vm.mempool,
			vm.Validators(),
			atomicTxGossipClient,
			atomicTxGossipMetrics,
			pushGossipParams,
			pushRegossipParams,
			config.PushGossipDiscardedElements,
			config.TxGossipTargetMessageSize,
			vm.Config().RegossipFrequency.Duration,
		)
		if err != nil {
			return fmt.Errorf("failed to initialize atomic tx push gossiper: %w", err)
		}
	}

	var p2pValidators p2p.ValidatorSet = &gossip.ValidatorSet{}
	if vm.Config().PullGossipFrequency.Duration > 0 {
		p2pValidators = vm.Validators()
	}

	if vm.atomicTxGossipHandler == nil {
		vm.atomicTxGossipHandler = gossip.NewTxGossipHandler[*atomic.GossipAtomicTx](
			vm.ctx.Log,
			atomicTxGossipMarshaller,
			vm.mempool,
			atomicTxGossipMetrics,
			config.TxGossipTargetMessageSize,
			config.TxGossipThrottlingPeriod,
			config.TxGossipThrottlingLimit,
			p2pValidators,
		)
	}

	if err := vm.AddHandler(p2p.AtomicTxGossipHandlerID, vm.atomicTxGossipHandler); err != nil {
		return fmt.Errorf("failed to add atomic tx gossip handler: %w", err)
	}

	if vm.atomicTxPullGossiper == nil {
		atomicTxPullGossiper := avalanchegossip.NewPullGossiper[*atomic.GossipAtomicTx](
			vm.ctx.Log,
			atomicTxGossipMarshaller,
			vm.mempool,
			atomicTxGossipClient,
			atomicTxGossipMetrics,
			config.TxGossipPollSize,
		)

		vm.atomicTxPullGossiper = &avalanchegossip.ValidatorGossiper{
			Gossiper:   atomicTxPullGossiper,
			NodeID:     vm.ctx.NodeID,
			Validators: vm.Validators(),
		}
	}

	if vm.Config().PushGossipFrequency.Duration > 0 {
		vm.shutdownWg.Add(1)
		go func() {
			avalanchegossip.Every(ctx, vm.ctx.Log, vm.atomicTxPushGossiper, vm.Config().PushGossipFrequency.Duration)
			vm.shutdownWg.Done()
		}()
	}
	if vm.Config().PullGossipFrequency.Duration > 0 {
		vm.shutdownWg.Add(1)
		go func() {
			avalanchegossip.Every(ctx, vm.ctx.Log, vm.atomicTxPullGossiper, vm.Config().PullGossipFrequency.Duration)
			vm.shutdownWg.Done()
		}()
	}
	return nil
}

func (vm *VM) Shutdown(context.Context) error {
	if vm.ctx == nil {
		return nil
	}
	if vm.cancel != nil {
		vm.cancel()
	}
	if err := vm.InnerVM.Shutdown(context.Background()); err != nil {
		log.Error("failed to shutdown inner VM", "err", err)
	}
	vm.shutdownWg.Wait()
	return nil
}

func (vm *VM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	apis, err := vm.InnerVM.CreateHandlers(ctx)
	if err != nil {
		return nil, err
	}
	avaxAPI, err := utils.NewHandler("avax", &AvaxAPI{vm})
	if err != nil {
		return nil, fmt.Errorf("failed to register service for AVAX API due to %w", err)
	}
	log.Info("AVAX API enabled")
	apis[avaxEndpoint] = avaxAPI
	return apis, nil
}

// verifyTxAtTip verifies that [tx] is valid to be issued on top of the currently preferred block
func (vm *VM) verifyTxAtTip(tx *atomic.Tx) error {
	if txByteLen := len(tx.SignedBytes()); txByteLen > targetAtomicTxsSize {
		return fmt.Errorf("tx size (%d) exceeds total atomic txs size target (%d)", txByteLen, targetAtomicTxsSize)
	}
	gasUsed, err := tx.GasUsed(true)
	if err != nil {
		return err
	}
	if gasUsed > maxAtomicTxMempoolGas {
		return fmt.Errorf("tx gas usage (%d) exceeds maximum allowed mempool gas usage (%d)", gasUsed, maxAtomicTxMempoolGas)
	}
	blockchain := vm.Ethereum().BlockChain()
	// Note: we fetch the current block and then the state at that block instead of the current state directly
	// since we need the header of the current block below.
	preferredBlock := blockchain.CurrentBlock()
	preferredState, err := blockchain.StateAt(preferredBlock.Root)
	if err != nil {
		return fmt.Errorf("failed to retrieve block state at tip while verifying atomic tx: %w", err)
	}
	extraConfig := params.GetExtra(blockchain.Config())
	extraRules := params.GetRulesExtra(blockchain.Config().Rules(preferredBlock.Number, params.IsMergeTODO, preferredBlock.Time))
	parentHeader := preferredBlock
	var nextBaseFee *big.Int
	timestamp := uint64(vm.clock.Time().Unix())
	if extraConfig.IsApricotPhase3(timestamp) {
		nextBaseFee, err = customheader.EstimateNextBaseFee(extraConfig, parentHeader, timestamp)
		if err != nil {
			// Return extremely detailed error since CalcBaseFee should never encounter an issue here
			return fmt.Errorf("failed to calculate base fee with parent timestamp (%d), parent ExtraData: (0x%x), and current timestamp (%d): %w", parentHeader.Time, parentHeader.Extra, timestamp, err)
		}
	}

	// We don’t need to revert the state here in case verifyTx errors, because
	// [preferredState] is thrown away either way.
	return vm.verifyTx(tx, parentHeader.Hash(), nextBaseFee, preferredState, *extraRules)
}

// verifyTx verifies that [tx] is valid to be issued into a block with parent block [parentHash]
// and validated at [state] using [rules] as the current rule set.
// Note: verifyTx may modify [state]. If [state] needs to be properly maintained, the caller is responsible
// for reverting to the correct snapshot after calling this function. If this function is called with a
// throwaway state, then this is not necessary.
func (vm *VM) verifyTx(tx *atomic.Tx, parentHash common.Hash, baseFee *big.Int, state *state.StateDB, rules extras.Rules) error {
	parent, err := vm.InnerVM.GetVMBlock(context.TODO(), ids.ID(parentHash))
	if err != nil {
		return fmt.Errorf("failed to get parent block: %w", err)
	}
	atomicBackend := &verifierBackend{
		ctx:          vm.ctx,
		fx:           &vm.fx,
		rules:        rules,
		bootstrapped: vm.IsBootstrapped(),
		blockFetcher: vm,
		secpCache:    vm.secpCache,
	}
	if err := tx.UnsignedAtomicTx.Visit(&semanticVerifier{
		backend: atomicBackend,
		tx:      tx,
		parent:  parent,
		baseFee: baseFee,
	}); err != nil {
		return err
	}
	return tx.UnsignedAtomicTx.EVMStateTransfer(vm.ctx, state)
}

// verifyTxs verifies that [txs] are valid to be issued into a block with parent block [parentHash]
// using [rules] as the current rule set.
func (vm *VM) verifyTxs(txs []*atomic.Tx, parentHash common.Hash, baseFee *big.Int, height uint64, rules extras.Rules) error {
	// Ensure that the parent was verified and inserted correctly.
	if !vm.Ethereum().BlockChain().HasBlock(parentHash, height-1) {
		return errRejectedParent
	}

	ancestorID := ids.ID(parentHash)
	// If the ancestor is unknown, then the parent failed verification when
	// it was called.
	// If the ancestor is rejected, then this block shouldn't be inserted
	// into the canonical chain because the parent will be missing.
	ancestor, err := vm.GetVMBlock(context.TODO(), ancestorID)
	if err != nil {
		return errRejectedParent
	}

	// Ensure each tx in [txs] doesn't conflict with any other atomic tx in
	// a processing ancestor block.
	inputs := set.Set[ids.ID]{}
	atomicBackend := &verifierBackend{
		ctx:          vm.ctx,
		fx:           &vm.fx,
		rules:        rules,
		bootstrapped: vm.IsBootstrapped(),
		blockFetcher: vm,
		secpCache:    vm.secpCache,
	}

	for _, atomicTx := range txs {
		utx := atomicTx.UnsignedAtomicTx
		if err := utx.Visit(&semanticVerifier{
			backend: atomicBackend,
			tx:      atomicTx,
			parent:  ancestor,
			baseFee: baseFee,
		}); err != nil {
			return fmt.Errorf("invalid block due to failed semantic verify: %w at height %d", err, height)
		}
		txInputs := utx.InputUTXOs()
		if inputs.Overlaps(txInputs) {
			return errConflictingAtomicInputs
		}
		inputs.Union(txInputs)
	}
	return nil
}

// CodecRegistry implements the secp256k1fx interface
func (vm *VM) CodecRegistry() codec.Registry { return vm.baseCodec }

// Clock implements the secp256k1fx interface
func (vm *VM) Clock() *mockable.Clock { return &vm.clock }

// Logger implements the secp256k1fx interface
func (vm *VM) Logger() logging.Logger { return vm.ctx.Log }

func (vm *VM) createConsensusCallbacks() dummy.ConsensusCallbacks {
	return dummy.ConsensusCallbacks{
		OnFinalizeAndAssemble: vm.onFinalizeAndAssemble,
		OnExtraStateChange:    vm.onExtraStateChange,
	}
}

func (vm *VM) preBatchOnFinalizeAndAssemble(header *types.Header, state *state.StateDB, txs []*types.Transaction) ([]byte, *big.Int, *big.Int, error) {
	for {
		tx, exists := vm.mempool.NextTx()
		if !exists {
			break
		}
		// Take a snapshot of [state] before calling verifyTx so that if the transaction fails verification
		// we can revert to [snapshot].
		// Note: snapshot is taken inside the loop because you cannot revert to the same snapshot more than
		// once.
		snapshot := state.Snapshot()
		rules := vm.rules(header.Number, header.Time)
		if err := vm.verifyTx(tx, header.ParentHash, header.BaseFee, state, rules); err != nil {
			// Discard the transaction from the mempool on failed verification.
			log.Debug("discarding tx from mempool on failed verification", "txID", tx.ID(), "err", err)
			vm.mempool.DiscardCurrentTx(tx.ID())
			state.RevertToSnapshot(snapshot)
			continue
		}

		atomicTxBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, tx)
		if err != nil {
			// Discard the transaction from the mempool and error if the transaction
			// cannot be marshalled. This should never happen.
			log.Debug("discarding tx due to unmarshal err", "txID", tx.ID(), "err", err)
			vm.mempool.DiscardCurrentTx(tx.ID())
			return nil, nil, nil, fmt.Errorf("failed to marshal atomic transaction %s due to %w", tx.ID(), err)
		}
		var contribution, gasUsed *big.Int
		if rules.IsApricotPhase4 {
			contribution, gasUsed, err = tx.BlockFeeContribution(rules.IsApricotPhase5, vm.ctx.AVAXAssetID, header.BaseFee)
			if err != nil {
				return nil, nil, nil, err
			}
		}
		return atomicTxBytes, contribution, gasUsed, nil
	}

	if len(txs) == 0 {
		// this could happen due to the async logic of geth tx pool
		return nil, nil, nil, errEmptyBlock
	}

	return nil, nil, nil, nil
}

// assumes that we are in at least Apricot Phase 5.
func (vm *VM) postBatchOnFinalizeAndAssemble(
	header *types.Header,
	parent *types.Header,
	state *state.StateDB,
	txs []*types.Transaction,
) ([]byte, *big.Int, *big.Int, error) {
	var (
		batchAtomicTxs    []*atomic.Tx
		batchAtomicUTXOs  set.Set[ids.ID]
		batchContribution *big.Int = new(big.Int).Set(common.Big0)
		batchGasUsed      *big.Int = new(big.Int).Set(common.Big0)
		rules                      = vm.rules(header.Number, header.Time)
		size              int
	)

	atomicGasLimit, err := customheader.RemainingAtomicGasCapacity(vm.chainConfigExtra(), parent, header)
	if err != nil {
		return nil, nil, nil, err
	}

	for {
		tx, exists := vm.mempool.NextTx()
		if !exists {
			break
		}

		// Ensure that adding [tx] to the block will not exceed the block size soft limit.
		txSize := len(tx.SignedBytes())
		if size+txSize > targetAtomicTxsSize {
			vm.mempool.CancelCurrentTx(tx.ID())
			break
		}

		var (
			txGasUsed, txContribution *big.Int
			err                       error
		)

		// Note: we do not need to check if we are in at least ApricotPhase4 here because
		// we assume that this function will only be called when the block is in at least
		// ApricotPhase5.
		txContribution, txGasUsed, err = tx.BlockFeeContribution(true, vm.ctx.AVAXAssetID, header.BaseFee)
		if err != nil {
			return nil, nil, nil, err
		}
		// ensure `gasUsed + batchGasUsed` doesn't exceed `atomicGasLimit`
		if totalGasUsed := new(big.Int).Add(batchGasUsed, txGasUsed); !utils.BigLessOrEqualUint64(totalGasUsed, atomicGasLimit) {
			// Send [tx] back to the mempool's tx heap.
			vm.mempool.CancelCurrentTx(tx.ID())
			break
		}

		if batchAtomicUTXOs.Overlaps(tx.InputUTXOs()) {
			// Discard the transaction from the mempool since it will fail verification
			// after this block has been accepted.
			// Note: if the proposed block is not accepted, the transaction may still be
			// valid, but we discard it early here based on the assumption that the proposed
			// block will most likely be accepted.
			// Discard the transaction from the mempool on failed verification.
			log.Debug("discarding tx due to overlapping input utxos", "txID", tx.ID())
			vm.mempool.DiscardCurrentTx(tx.ID())
			continue
		}

		snapshot := state.Snapshot()
		if err := vm.verifyTx(tx, header.ParentHash, header.BaseFee, state, rules); err != nil {
			// Discard the transaction from the mempool and reset the state to [snapshot]
			// if it fails verification here.
			// Note: prior to this point, we have not modified [state] so there is no need to
			// revert to a snapshot if we discard the transaction prior to this point.
			log.Debug("discarding tx from mempool due to failed verification", "txID", tx.ID(), "err", err)
			vm.mempool.DiscardCurrentTx(tx.ID())
			state.RevertToSnapshot(snapshot)
			continue
		}

		batchAtomicTxs = append(batchAtomicTxs, tx)
		batchAtomicUTXOs.Union(tx.InputUTXOs())
		// Add the [txGasUsed] to the [batchGasUsed] when the [tx] has passed verification
		batchGasUsed.Add(batchGasUsed, txGasUsed)
		batchContribution.Add(batchContribution, txContribution)
		size += txSize
	}

	// If there is a non-zero number of transactions, marshal them and return the byte slice
	// for the block's extra data along with the contribution and gas used.
	if len(batchAtomicTxs) > 0 {
		atomicTxBytes, err := atomic.Codec.Marshal(atomic.CodecVersion, batchAtomicTxs)
		if err != nil {
			// If we fail to marshal the batch of atomic transactions for any reason,
			// discard the entire set of current transactions.
			log.Debug("discarding txs due to error marshaling atomic transactions", "err", err)
			vm.mempool.DiscardCurrentTxs()
			return nil, nil, nil, fmt.Errorf("failed to marshal batch of atomic transactions due to %w", err)
		}
		return atomicTxBytes, batchContribution, batchGasUsed, nil
	}

	// If there are no regular transactions and there were also no atomic transactions to be included,
	// then the block is empty and should be considered invalid.
	if len(txs) == 0 {
		// this could happen due to the async logic of geth tx pool
		return nil, nil, nil, errEmptyBlock
	}

	// If there are no atomic transactions, but there is a non-zero number of regular transactions, then
	// we return a nil slice with no contribution from the atomic transactions and a nil error.
	return nil, nil, nil, nil
}

func (vm *VM) onFinalizeAndAssemble(
	header *types.Header,
	parent *types.Header,
	state *state.StateDB,
	txs []*types.Transaction,
) ([]byte, *big.Int, *big.Int, error) {
	if !vm.chainConfigExtra().IsApricotPhase5(header.Time) {
		return vm.preBatchOnFinalizeAndAssemble(header, state, txs)
	}
	return vm.postBatchOnFinalizeAndAssemble(header, parent, state, txs)
}

func (vm *VM) onExtraStateChange(block *types.Block, parent *types.Header, state *state.StateDB, chainConfig *params.ChainConfig) (*big.Int, *big.Int, error) {
	var (
		batchContribution *big.Int = big.NewInt(0)
		batchGasUsed      *big.Int = big.NewInt(0)
		header                     = block.Header()
		// We cannot use chain config from InnerVM since it's not available when this function is called for the first time (bc.loadLastState).
		rules      = chainConfig.Rules(header.Number, params.IsMergeTODO, header.Time)
		rulesExtra = *params.GetRulesExtra(rules)
	)

	txs, err := atomic.ExtractAtomicTxs(customtypes.BlockExtData(block), rulesExtra.IsApricotPhase5, atomic.Codec)
	if err != nil {
		return nil, nil, err
	}

	// If [atomicBackend] is nil, the VM is still initializing and is reprocessing accepted blocks.
	if vm.atomicBackend != nil {
		if vm.atomicBackend.IsBonus(block.NumberU64(), block.Hash()) {
			log.Info("skipping atomic tx verification on bonus block", "block", block.Hash())
		} else {
			// Verify [txs] do not conflict with themselves or ancestor blocks.
			if err := vm.verifyTxs(txs, block.ParentHash(), block.BaseFee(), block.NumberU64(), rulesExtra); err != nil {
				return nil, nil, err
			}
		}
		// Update the atomic backend with [txs] from this block.
		//
		// Note: The atomic trie canonically contains the duplicate operations
		// from any bonus blocks.
		_, err := vm.atomicBackend.InsertTxs(block.Hash(), block.NumberU64(), block.ParentHash(), txs)
		if err != nil {
			return nil, nil, err
		}
	}

	// If there are no transactions, we can return early.
	if len(txs) == 0 {
		return nil, nil, nil
	}

	for _, tx := range txs {
		if err := tx.UnsignedAtomicTx.EVMStateTransfer(vm.ctx, state); err != nil {
			return nil, nil, err
		}
		// If ApricotPhase4 is enabled, calculate the block fee contribution
		if rulesExtra.IsApricotPhase4 {
			contribution, gasUsed, err := tx.BlockFeeContribution(rulesExtra.IsApricotPhase5, vm.ctx.AVAXAssetID, block.BaseFee())
			if err != nil {
				return nil, nil, err
			}

			batchContribution.Add(batchContribution, contribution)
			batchGasUsed.Add(batchGasUsed, gasUsed)
		}
	}

	// If ApricotPhase5 is enabled, enforce that the atomic gas used does not exceed the
	// atomic gas limit.
	if rulesExtra.IsApricotPhase5 {
		chainConfigExtra := params.GetExtra(chainConfig)
		atomicGasLimit, err := customheader.RemainingAtomicGasCapacity(chainConfigExtra, parent, header)
		if err != nil {
			return nil, nil, err
		}

		if !utils.BigLessOrEqualUint64(batchGasUsed, atomicGasLimit) {
			return nil, nil, fmt.Errorf("atomic gas used (%d) by block (%s), exceeds atomic gas limit (%d)", batchGasUsed, block.Hash().Hex(), atomicGasLimit)
		}
	}
	return batchContribution, batchGasUsed, nil
}

// getAtomicTx returns the requested transaction, status, and height.
// If the status is Unknown, then the returned transaction will be nil.
func (vm *VM) getAtomicTx(txID ids.ID) (*atomic.Tx, atomic.Status, uint64, error) {
	if tx, height, err := vm.atomicTxRepository.GetByTxID(txID); err == nil {
		return tx, atomic.Accepted, height, nil
	} else if err != avalanchedatabase.ErrNotFound {
		return nil, atomic.Unknown, 0, err
	}
	tx, dropped, found := vm.mempool.GetTx(txID)
	switch {
	case found && dropped:
		return tx, atomic.Dropped, 0, nil
	case found:
		return tx, atomic.Processing, 0, nil
	default:
		return nil, atomic.Unknown, 0, nil
	}
}

// GetAtomicUTXOs returns the utxos that at least one of the provided addresses is
// referenced in.
func (vm *VM) GetAtomicUTXOs(
	chainID ids.ID,
	addrs set.Set[ids.ShortID],
	startAddr ids.ShortID,
	startUTXOID ids.ID,
	limit int,
) ([]*avax.UTXO, ids.ShortID, ids.ID, error) {
	if limit <= 0 || limit > maxUTXOsToFetch {
		limit = maxUTXOsToFetch
	}

	return avax.GetAtomicUTXOs(
		vm.ctx.SharedMemory,
		atomic.Codec,
		chainID,
		addrs,
		startAddr,
		startUTXOID,
		limit,
	)
}

func (vm *VM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	return vm.BuildBlockWithContext(ctx, nil)
}

func (vm *VM) BuildBlockWithContext(ctx context.Context, proposerVMBlockCtx *block.Context) (snowman.Block, error) {
	blk, err := vm.InnerVM.BuildBlockWithContext(ctx, proposerVMBlockCtx)

	// Handle errors and signal the mempool to take appropriate action
	switch {
	case err == nil:
		// Marks the current transactions from the mempool as being successfully issued
		// into a block.
		vm.mempool.IssueCurrentTxs()
	case errors.Is(err, vmerrors.ErrGenerateBlockFailed), errors.Is(err, vmerrors.ErrBlockVerificationFailed):
		log.Debug("cancelling txs due to error generating block", "err", err)
		vm.mempool.CancelCurrentTxs()
	case errors.Is(err, vmerrors.ErrWrapBlockFailed):
		log.Debug("discarding txs due to error making new block", "err", err)
		vm.mempool.DiscardCurrentTxs()
	}
	return blk, err
}

func (vm *VM) chainConfigExtra() *extras.ChainConfig {
	return params.GetExtra(vm.Ethereum().BlockChain().Config())
}

func (vm *VM) rules(number *big.Int, time uint64) extras.Rules {
	ethrules := vm.Ethereum().BlockChain().Config().Rules(number, params.IsMergeTODO, time)
	return *params.GetRulesExtra(ethrules)
}

// currentRules returns the chain rules for the current block.
func (vm *VM) currentRules() extras.Rules {
	header := vm.Ethereum().BlockChain().CurrentHeader()
	return vm.rules(header.Number, header.Time)
}
