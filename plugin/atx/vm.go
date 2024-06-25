package atx

import (
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	x2cRateInt64       int64 = 1_000_000_000
	x2cRateMinus1Int64 int64 = x2cRateInt64 - 1
)

var (
	// x2cRate is the conversion rate between the smallest denomination on the X-Chain
	// 1 nAVAX and the smallest denomination on the C-Chain 1 wei. Where 1 nAVAX = 1 gWei.
	// This is only required for AVAX because the denomination of 1 AVAX is 9 decimal
	// places on the X and P chains, but is 18 decimal places within the EVM.
	x2cRate       = big.NewInt(x2cRateInt64)
	x2cRateMinus1 = big.NewInt(x2cRateMinus1Int64)
)

const (
	codecVersion        = uint16(0)
	maxUTXOsToFetch     = 1024
	secpCacheSize       = 1024
	targetAtomicTxsSize = 40 * units.KiB

	// TODO: take gossip constants as config
	txGossipBloomMinTargetElements       = 8 * 1024
	txGossipBloomTargetFalsePositiveRate = 0.01
	txGossipBloomResetFalsePositiveRate  = 0.05
	txGossipBloomChurnMultiplier         = 3
)

type VM struct {
	ctx          *snow.Context
	bootstrapped bool
	codec        codec.Manager
	baseCodec    codec.Registry
	clock        *mockable.Clock
	blockChain   BlockChain
	chainConfig  *params.ChainConfig
	blockGetter  BlockGetter

	fx        secp256k1fx.Fx
	secpCache secp256k1.RecoverCache

	mempool IMempool

	// [atomicTxRepository] maintains two indexes on accepted atomic txs.
	// - txID to accepted atomic tx
	// - block height to list of atomic txs accepted on block at that height
	atomicTxRepository AtomicTxRepository
	// [atomicBackend] abstracts verification and processing of atomic transactions
	atomicBackend AtomicBackend
}

func NewVM(
	ctx *snow.Context,
	bVM BlockGetter,
	codec codec.Manager,
	clock *mockable.Clock,
	chainConfig *params.ChainConfig,
	sdkMetrics prometheus.Registerer,
	mempoolSize int,
	verify func(*Tx) error,
) (*VM, error) {
	vm := &VM{
		ctx:         ctx,
		codec:       codec,
		clock:       clock,
		chainConfig: chainConfig,
		blockGetter: bVM,
	}
	vm.secpCache = secp256k1.RecoverCache{
		LRU: cache.LRU[ids.ID, *secp256k1.PublicKey]{
			Size: secpCacheSize,
		},
	}
	// The Codec explicitly registers the types it requires from the secp256k1fx
	// so [vm.baseCodec] is a dummy codec use to fulfill the secp256k1fx VM
	// interface. The fx will register all of its types, which can be safely
	// ignored by the VM's codec.
	vm.baseCodec = linearcodec.NewDefault()

	// TODO: read size from settings
	var err error
	vm.mempool, err = NewMempool(ctx, sdkMetrics, mempoolSize, verify)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize mempool: %w", err)
	}

	return vm, vm.fx.Initialize(vm)
}

func (vm *VM) Initialize(
	db *versiondb.Database,
	chain BlockChain,
	lastAcceptedHeight uint64,
	lastAcceptedHash common.Hash,
	bonusBlockHeights map[uint64]ids.ID,
	commitInterval uint64,
) error {
	vm.blockChain = chain
	var err error
	// initialize atomic repository
	vm.atomicTxRepository, err = NewAtomicTxRepository(db, vm.codec, lastAcceptedHeight)
	if err != nil {
		return fmt.Errorf("failed to create atomic repository: %w", err)
	}
	vm.atomicBackend, err = NewAtomicBackend(
		db, vm.ctx.SharedMemory, bonusBlockHeights,
		vm.atomicTxRepository, lastAcceptedHeight, lastAcceptedHash,
		commitInterval,
	)
	if err != nil {
		return fmt.Errorf("failed to create atomic backend: %w", err)
	}
	return nil
}

func (vm *VM) AtomicBackend() AtomicBackend { return vm.atomicBackend }
func (vm *VM) Mempool() IMempool            { return vm.mempool }

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

	addrsList := make([][]byte, addrs.Len())
	for i, addr := range addrs.List() {
		addrsList[i] = addr.Bytes()
	}

	allUTXOBytes, lastAddr, lastUTXO, err := vm.ctx.SharedMemory.Indexed(
		chainID,
		addrsList,
		startAddr.Bytes(),
		startUTXOID[:],
		limit,
	)
	if err != nil {
		return nil, ids.ShortID{}, ids.ID{}, fmt.Errorf("error fetching atomic UTXOs: %w", err)
	}

	lastAddrID, err := ids.ToShortID(lastAddr)
	if err != nil {
		lastAddrID = ids.ShortEmpty
	}
	lastUTXOID, err := ids.ToID(lastUTXO)
	if err != nil {
		lastUTXOID = ids.Empty
	}

	utxos := make([]*avax.UTXO, len(allUTXOBytes))
	for i, utxoBytes := range allUTXOBytes {
		utxo := &avax.UTXO{}
		if _, err := vm.codec.Unmarshal(utxoBytes, utxo); err != nil {
			return nil, ids.ShortID{}, ids.ID{}, fmt.Errorf("error parsing UTXO: %w", err)
		}
		utxos[i] = utxo
	}
	return utxos, lastAddrID, lastUTXOID, nil
}

// GetSpendableFunds returns a list of EVMInputs and keys (in corresponding
// order) to total [amount] of [assetID] owned by [keys].
// Note: we return [][]*secp256k1.PrivateKey even though each input
// corresponds to a single key, so that the signers can be passed in to
// [tx.Sign] which supports multiple keys on a single input.
func (vm *VM) GetSpendableFunds(
	keys []*secp256k1.PrivateKey,
	assetID ids.ID,
	amount uint64,
) ([]EVMInput, [][]*secp256k1.PrivateKey, error) {
	// Note: current state uses the state of the preferred block.
	state, err := vm.blockChain.State()
	if err != nil {
		return nil, nil, err
	}
	inputs := []EVMInput{}
	signers := [][]*secp256k1.PrivateKey{}
	// Note: we assume that each key in [keys] is unique, so that iterating over
	// the keys will not produce duplicated nonces in the returned EVMInput slice.
	for _, key := range keys {
		if amount == 0 {
			break
		}
		addr := GetEthAddress(key)
		var balance uint64
		if assetID == vm.ctx.AVAXAssetID {
			// If the asset is AVAX, we divide by the x2cRate to convert back to the correct
			// denomination of AVAX that can be exported.
			balance = new(big.Int).Div(state.GetBalance(addr), x2cRate).Uint64()
		} else {
			balance = state.GetBalanceMultiCoin(addr, common.Hash(assetID)).Uint64()
		}
		if balance == 0 {
			continue
		}
		if amount < balance {
			balance = amount
		}
		nonce, err := vm.GetCurrentNonce(addr)
		if err != nil {
			return nil, nil, err
		}
		inputs = append(inputs, EVMInput{
			Address: addr,
			Amount:  balance,
			AssetID: assetID,
			Nonce:   nonce,
		})
		signers = append(signers, []*secp256k1.PrivateKey{key})
		amount -= balance
	}

	if amount > 0 {
		return nil, nil, errInsufficientFunds
	}

	return inputs, signers, nil
}

// GetSpendableAVAXWithFee returns a list of EVMInputs and keys (in corresponding
// order) to total [amount] + [fee] of [AVAX] owned by [keys].
// This function accounts for the added cost of the additional inputs needed to
// create the transaction and makes sure to skip any keys with a balance that is
// insufficient to cover the additional fee.
// Note: we return [][]*secp256k1.PrivateKey even though each input
// corresponds to a single key, so that the signers can be passed in to
// [tx.Sign] which supports multiple keys on a single input.
func (vm *VM) GetSpendableAVAXWithFee(
	keys []*secp256k1.PrivateKey,
	amount uint64,
	cost uint64,
	baseFee *big.Int,
) ([]EVMInput, [][]*secp256k1.PrivateKey, error) {
	// Note: current state uses the state of the preferred block.
	state, err := vm.blockChain.State()
	if err != nil {
		return nil, nil, err
	}

	initialFee, err := CalculateDynamicFee(cost, baseFee)
	if err != nil {
		return nil, nil, err
	}

	newAmount, err := math.Add64(amount, initialFee)
	if err != nil {
		return nil, nil, err
	}
	amount = newAmount

	inputs := []EVMInput{}
	signers := [][]*secp256k1.PrivateKey{}
	// Note: we assume that each key in [keys] is unique, so that iterating over
	// the keys will not produce duplicated nonces in the returned EVMInput slice.
	for _, key := range keys {
		if amount == 0 {
			break
		}

		prevFee, err := CalculateDynamicFee(cost, baseFee)
		if err != nil {
			return nil, nil, err
		}

		newCost := cost + EVMInputGas
		newFee, err := CalculateDynamicFee(newCost, baseFee)
		if err != nil {
			return nil, nil, err
		}

		additionalFee := newFee - prevFee

		addr := GetEthAddress(key)
		// Since the asset is AVAX, we divide by the x2cRate to convert back to
		// the correct denomination of AVAX that can be exported.
		balance := new(big.Int).Div(state.GetBalance(addr), x2cRate).Uint64()
		// If the balance for [addr] is insufficient to cover the additional cost
		// of adding an input to the transaction, skip adding the input altogether
		if balance <= additionalFee {
			continue
		}

		// Update the cost for the next iteration
		cost = newCost

		newAmount, err := math.Add64(amount, additionalFee)
		if err != nil {
			return nil, nil, err
		}
		amount = newAmount

		// Use the entire [balance] as an input, but if the required [amount]
		// is less than the balance, update the [inputAmount] to spend the
		// minimum amount to finish the transaction.
		inputAmount := balance
		if amount < balance {
			inputAmount = amount
		}
		nonce, err := vm.GetCurrentNonce(addr)
		if err != nil {
			return nil, nil, err
		}
		inputs = append(inputs, EVMInput{
			Address: addr,
			Amount:  inputAmount,
			AssetID: vm.ctx.AVAXAssetID,
			Nonce:   nonce,
		})
		signers = append(signers, []*secp256k1.PrivateKey{key})
		amount -= inputAmount
	}

	if amount > 0 {
		return nil, nil, errInsufficientFunds
	}

	return inputs, signers, nil
}

// GetCurrentNonce returns the nonce associated with the address at the
// preferred block
func (vm *VM) GetCurrentNonce(address common.Address) (uint64, error) {
	// Note: current state uses the state of the preferred block.
	state, err := vm.blockChain.State()
	if err != nil {
		return 0, err
	}
	return state.GetNonce(address), nil
}

// currentRules returns the chain rules for the current block.
func (vm *VM) currentRules() params.Rules {
	header := vm.blockChain.CurrentHeader()
	return vm.chainConfig.Rules(header.Number, header.Time)
}

// conflicts returns an error if [inputs] conflicts with any of the atomic inputs contained in [ancestor]
// or any of its ancestor blocks going back to the last accepted block in its ancestry. If [ancestor] is
// accepted, then nil will be returned immediately.
// If the ancestry of [ancestor] cannot be fetched, then [errRejectedParent] may be returned.
func (vm *VM) conflicts(inputs set.Set[ids.ID], ancestorID ids.ID) error {
	for {
		// If the ancestor is unknown, then the parent failed
		// verification when it was called.
		// If the ancestor is rejected, then this block shouldn't be
		// inserted into the canonical chain because the parent is
		// will be missing.
		// If the ancestor is processing, then the block may have
		// been verified.
		txs, status, parentID, err := vm.blockGetter.GetBlockAndAtomicTxs(ancestorID)
		if err != nil {
			return errRejectedParent
		}
		if status == choices.Unknown || status == choices.Rejected {
			return errRejectedParent
		}

		if status == choices.Accepted {
			break
		}

		// If any of the atomic transactions in the ancestor conflict with [inputs]
		// return an error.
		for _, atomicTx := range txs {
			if inputs.Overlaps(atomicTx.InputUTXOs()) {
				return errConflictingAtomicInputs
			}
		}

		// Move up the chain.
		ancestorID = parentID
	}
	return nil
}

func (vm *VM) Bootstrapping() error { return vm.fx.Bootstrapping() }
func (vm *VM) Bootstrapped() error {
	vm.bootstrapped = true
	return vm.fx.Bootstrapped()
}

// mergeAtomicOps merges atomic ops for [chainID] represented by [requests]
// to the [output] map provided.
func mergeAtomicOpsToMap(output map[ids.ID]*atomic.Requests, chainID ids.ID, requests *atomic.Requests) {
	if request, exists := output[chainID]; exists {
		request.PutRequests = append(request.PutRequests, requests.PutRequests...)
		request.RemoveRequests = append(request.RemoveRequests, requests.RemoveRequests...)
	} else {
		output[chainID] = requests
	}
}

// CodecRegistry implements the secp256k1fx interface
func (vm *VM) CodecRegistry() codec.Registry { return vm.baseCodec }

// Clock implements the secp256k1fx interface
func (vm *VM) Clock() *mockable.Clock { return vm.clock }

// Logger implements the secp256k1fx interface
func (vm *VM) Logger() logging.Logger { return vm.ctx.Log }

// GetAtomicTx returns the requested transaction, status, and height.
// If the status is Unknown, then the returned transaction will be nil.
func (vm *VM) GetAtomicTx(txID ids.ID) (*Tx, Status, uint64, error) {
	if tx, height, err := vm.atomicTxRepository.GetByTxID(txID); err == nil {
		return tx, Accepted, height, nil
	} else if err != database.ErrNotFound {
		return nil, Unknown, 0, err
	}
	tx, dropped, found := vm.Mempool().GetTx(txID)
	switch {
	case found && dropped:
		return tx, Dropped, 0, nil
	case found:
		return tx, Processing, 0, nil
	default:
		return nil, Unknown, 0, nil
	}
}
func (vm *VM) OnFinalizeAndAssemble(header *types.Header, state StateDB, txs []*types.Transaction) ([]byte, *big.Int, *big.Int, error) {
	if !vm.chainConfig.IsApricotPhase5(header.Time) {
		return vm.preBatchOnFinalizeAndAssemble(header, state, txs)
	}
	return vm.postBatchOnFinalizeAndAssemble(header, state, txs)
}

func (vm *VM) preBatchOnFinalizeAndAssemble(header *types.Header, state StateDB, txs []*types.Transaction) ([]byte, *big.Int, *big.Int, error) {
	for {
		tx, exists := vm.Mempool().NextTx()
		if !exists {
			break
		}
		// Take a snapshot of [state] before calling verifyTx so that if the transaction fails verification
		// we can revert to [snapshot].
		// Note: snapshot is taken inside the loop because you cannot revert to the same snapshot more than
		// once.
		snapshot := state.Snapshot()
		rules := vm.chainConfig.Rules(header.Number, header.Time)
		if err := vm.verifyTx(tx, header.ParentHash, header.BaseFee, state, rules); err != nil {
			// Discard the transaction from the mempool on failed verification.
			log.Debug("discarding tx from mempool on failed verification", "txID", tx.ID(), "err", err)
			vm.Mempool().DiscardCurrentTx(tx.ID())
			state.RevertToSnapshot(snapshot)
			continue
		}

		atomicTxBytes, err := vm.codec.Marshal(codecVersion, tx)
		if err != nil {
			// Discard the transaction from the mempool and error if the transaction
			// cannot be marshalled. This should never happen.
			log.Debug("discarding tx due to unmarshal err", "txID", tx.ID(), "err", err)
			vm.Mempool().DiscardCurrentTx(tx.ID())
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
func (vm *VM) postBatchOnFinalizeAndAssemble(header *types.Header, state StateDB, txs []*types.Transaction) ([]byte, *big.Int, *big.Int, error) {
	var (
		batchAtomicTxs    []*Tx
		batchAtomicUTXOs  set.Set[ids.ID]
		batchContribution *big.Int = new(big.Int).Set(common.Big0)
		batchGasUsed      *big.Int = new(big.Int).Set(common.Big0)
		rules                      = vm.chainConfig.Rules(header.Number, header.Time)
		size              int
	)

	for {
		tx, exists := vm.Mempool().NextTx()
		if !exists {
			break
		}

		// Ensure that adding [tx] to the block will not exceed the block size soft limit.
		txSize := len(tx.SignedBytes())
		if size+txSize > targetAtomicTxsSize {
			vm.Mempool().CancelCurrentTx(tx.ID())
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
		// ensure [gasUsed] + [batchGasUsed] doesnt exceed the [atomicGasLimit]
		if totalGasUsed := new(big.Int).Add(batchGasUsed, txGasUsed); totalGasUsed.Cmp(params.AtomicGasLimit) > 0 {
			// Send [tx] back to the mempool's tx heap.
			vm.Mempool().CancelCurrentTx(tx.ID())
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
			vm.Mempool().DiscardCurrentTx(tx.ID())
			continue
		}

		snapshot := state.Snapshot()
		if err := vm.verifyTx(tx, header.ParentHash, header.BaseFee, state, rules); err != nil {
			// Discard the transaction from the mempool and reset the state to [snapshot]
			// if it fails verification here.
			// Note: prior to this point, we have not modified [state] so there is no need to
			// revert to a snapshot if we discard the transaction prior to this point.
			log.Debug("discarding tx from mempool due to failed verification", "txID", tx.ID(), "err", err)
			vm.Mempool().DiscardCurrentTx(tx.ID())
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
		atomicTxBytes, err := vm.codec.Marshal(codecVersion, batchAtomicTxs)
		if err != nil {
			// If we fail to marshal the batch of atomic transactions for any reason,
			// discard the entire set of current transactions.
			log.Debug("discarding txs due to error marshaling atomic transactions", "err", err)
			vm.Mempool().DiscardCurrentTxs()
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

func (vm *VM) OnExtraStateChange(block *types.Block, state StateDB) (*big.Int, *big.Int, error) {
	var (
		batchContribution *big.Int = big.NewInt(0)
		batchGasUsed      *big.Int = big.NewInt(0)
		header                     = block.Header()
		rules                      = vm.chainConfig.Rules(header.Number, header.Time)
	)

	txs, err := ExtractAtomicTxs(block.ExtData(), rules.IsApricotPhase5, vm.codec)
	if err != nil {
		return nil, nil, err
	}

	// If [atomicBackend] is nil, the VM is still initializing and is reprocessing accepted blocks.
	if vm.atomicBackend != nil {
		if vm.atomicBackend.IsBonus(block.NumberU64(), block.Hash()) {
			log.Info("skipping atomic tx verification on bonus block", "block", block.Hash())
		} else {
			// Verify [txs] do not conflict with themselves or ancestor blocks.
			if err := vm.verifyTxs(txs, block.ParentHash(), block.BaseFee(), block.NumberU64(), rules); err != nil {
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
		if rules.IsApricotPhase4 {
			contribution, gasUsed, err := tx.BlockFeeContribution(rules.IsApricotPhase5, vm.ctx.AVAXAssetID, block.BaseFee())
			if err != nil {
				return nil, nil, err
			}

			batchContribution.Add(batchContribution, contribution)
			batchGasUsed.Add(batchGasUsed, gasUsed)
		}

		// If ApricotPhase5 is enabled, enforce that the atomic gas used does not exceed the
		// atomic gas limit.
		if rules.IsApricotPhase5 {
			// Ensure that [tx] does not push [block] above the atomic gas limit.
			if batchGasUsed.Cmp(params.AtomicGasLimit) == 1 {
				return nil, nil, fmt.Errorf("atomic gas used (%d) by block (%s), exceeds atomic gas limit (%d)", batchGasUsed, block.Hash().Hex(), params.AtomicGasLimit)
			}
		}
	}
	return batchContribution, batchGasUsed, nil
}

// verifyTx verifies that [tx] is valid to be issued into a block with parent block [parentHash]
// and validated at [state] using [rules] as the current rule set.
// Note: verifyTx may modify [state]. If [state] needs to be properly maintained, the caller is responsible
// for reverting to the correct snapshot after calling this function. If this function is called with a
// throwaway state, then this is not necessary.
func (vm *VM) verifyTx(tx *Tx, parentHash common.Hash, baseFee *big.Int, state StateDB, rules params.Rules) error {
	if err := tx.UnsignedAtomicTx.SemanticVerify(vm, tx, ids.ID(parentHash), baseFee, rules); err != nil {
		return err
	}
	return tx.UnsignedAtomicTx.EVMStateTransfer(vm.ctx, state)
}

// verifyTxs verifies that [txs] are valid to be issued into a block with parent block [parentHash]
// using [rules] as the current rule set.
func (vm *VM) verifyTxs(txs []*Tx, parentHash common.Hash, baseFee *big.Int, height uint64, rules params.Rules) error {
	// Ensure that the parent was verified and inserted correctly.
	if !vm.blockChain.HasBlock(parentHash, height-1) {
		return errRejectedParent
	}

	// If the ancestor is unknown, then the parent failed verification when
	// it was called.
	// If the ancestor is rejected, then this block shouldn't be inserted
	// into the canonical chain because the parent will be missing.
	ancestorID := ids.ID(parentHash)
	_, blkStatus, _, err := vm.blockGetter.GetBlockAndAtomicTxs(ancestorID)
	if err != nil {
		return errRejectedParent
	}
	if blkStatus == choices.Unknown || blkStatus == choices.Rejected {
		return errRejectedParent
	}

	// Ensure each tx in [txs] doesn't conflict with any other atomic tx in
	// a processing ancestor block.
	inputs := set.Set[ids.ID]{}
	for _, atomicTx := range txs {
		utx := atomicTx.UnsignedAtomicTx
		if err := utx.SemanticVerify(vm, atomicTx, ancestorID, baseFee, rules); err != nil {
			return fmt.Errorf("invalid block due to failed semanatic verify: %w at height %d", err, height)
		}
		txInputs := utx.InputUTXOs()
		if inputs.Overlaps(txInputs) {
			return errConflictingAtomicInputs
		}
		inputs.Union(txInputs)
	}
	return nil
}
