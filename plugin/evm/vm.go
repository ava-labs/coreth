// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/codec/linearcodec"

	"github.com/ava-labs/coreth"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/node"
	"github.com/ava-labs/coreth/params"
	chainState "github.com/ava-labs/coreth/plugin/evm/state"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"

	avalancheRPC "github.com/gorilla/rpc/v2"

	"github.com/ava-labs/avalanchego/api/admin"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	avalancheJSON "github.com/ava-labs/avalanchego/utils/json"
)

var (
	x2cRate = big.NewInt(1000000000)
)

var (
	ethDBPrefix    = []byte("ethdb")
	atomicTxPrefix = []byte("atomicTxDB")
)

const (
	minBlockTime        = 250 * time.Millisecond
	maxBlockTime        = time.Second
	batchSize           = 250
	maxUTXOsToFetch     = 1024
	chainStateCacheSize = 1024
	defaultMempoolSize  = 1024
	codecVersion        = uint16(0)
)

const (
	txFee = units.MilliAvax
)

var (
	errEmptyBlock                 = errors.New("empty block")
	errCreateBlock                = errors.New("couldn't create block")
	errUnsupportedFXs             = errors.New("unsupported feature extensions")
	errInvalidBlock               = errors.New("invalid block")
	errInvalidAddr                = errors.New("invalid hex address")
	errTooManyAtomicTx            = errors.New("too many pending atomic txs")
	errAssetIDMismatch            = errors.New("asset IDs in the input don't match the utxo")
	errNoImportInputs             = errors.New("tx has no imported inputs")
	errInputsNotSortedUnique      = errors.New("inputs not sorted and unique")
	errPublicKeySignatureMismatch = errors.New("signature doesn't match public key")
	errSignatureInputsMismatch    = errors.New("number of inputs does not match number of signatures")
	errWrongChainID               = errors.New("tx has wrong chain ID")
	errInsufficientFunds          = errors.New("insufficient funds")
	errNoExportOutputs            = errors.New("tx has no export outputs")
	errOutputsNotSorted           = errors.New("tx outputs not sorted")
	errOverflowExport             = errors.New("overflow when computing export amount + txFee")
	errInvalidNonce               = errors.New("invalid nonce")
	errInvalidGas                 = errors.New("invalid block due to low gas")
	errConflictingAtomicInputs    = errors.New("invalid block due to conflicting atomic inputs")
	errUnknownAtomicTx            = errors.New("unknown atomic tx type")
	errFailedChainVerify          = errors.New("block failed chain verify")
)

// buildingBlkStatus denotes the current status of the VM in block production.
type buildingBlkStatus uint8

const (
	dontBuild buildingBlkStatus = iota
	conditionalBuild
	mayBuild
	building
)

// Codec does serialization and deserialization
var Codec codec.Manager

func init() {
	Codec = codec.NewDefaultManager()
	c := linearcodec.NewDefault()

	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&UnsignedImportTx{}),
		c.RegisterType(&UnsignedExportTx{}),
	)
	c.SkipRegistrations(3)
	errs.Add(
		c.RegisterType(&secp256k1fx.TransferInput{}),
		c.RegisterType(&secp256k1fx.MintOutput{}),
		c.RegisterType(&secp256k1fx.TransferOutput{}),
		c.RegisterType(&secp256k1fx.MintOperation{}),
		c.RegisterType(&secp256k1fx.Credential{}),
		c.RegisterType(&secp256k1fx.Input{}),
		c.RegisterType(&secp256k1fx.OutputOwners{}),
		Codec.RegisterCodec(codecVersion, c),
	)

	if errs.Errored() {
		panic(errs.Err)
	}
}

// VM implements the snowman.ChainVM interface
type VM struct {
	ctx *snow.Context
	// ChainState helps to implement the VM interface by wrapping fetching
	// with an efficient cache.
	*chainState.ChainState

	CLIConfig CommandLineConfig

	chainID            *big.Int
	networkID          uint64
	genesisHash        common.Hash
	chain              *coreth.ETHChain
	db                 database.Database
	chaindb            Database
	acceptedAtomicTxDB database.Database

	newBlockChan chan *Block
	// A message is sent on this channel when a new block
	// is ready to be build. This notifies the consensus engine.
	notifyBuildBlockChan chan<- commonEng.Message
	newMinedBlockSub     *event.TypeMuxSubscription

	txPoolStabilizedLock sync.Mutex
	txPoolStabilizedHead common.Hash
	txPoolStabilizedOk   chan struct{}

	// [buildBlockLock] must be held when accessing [buildStatus]
	buildBlockLock sync.Mutex
	// [buildBlockTimer] is a two stage timer handling block production.
	// Stage1 build a block if the batch size has been reached.
	// Stage2 build a block regardless of the size.
	buildBlockTimer *timer.Timer
	// buildStatus signals the phase of block building the VM is currently in.
	// [dontBuild] indicates there's no need to build a block.
	// [conditionalBuild] indicates build a block if the batch size has been reached.
	// [mayBuild] indicates the VM should proceed to build a block.
	// [building] indicates the VM has sent a request to the engine to build a block.
	buildStatus buildingBlkStatus

	baseCodec codec.Registry
	codec     codec.Manager
	clock     timer.Clock
	txFee     uint64
	mempool   *Mempool

	shutdownChan chan struct{}
	shutdownWg   sync.WaitGroup

	fx secp256k1fx.Fx
}

// Codec implements the secp256k1fx interface
func (vm *VM) Codec() codec.Manager { return vm.codec }

// CodecRegistry implements the secp256k1fx interface
func (vm *VM) CodecRegistry() codec.Registry { return vm.baseCodec }

// Clock implements the secp256k1fx interface
func (vm *VM) Clock() *timer.Clock { return &vm.clock }

// Logger implements the secp256k1fx interface
func (vm *VM) Logger() logging.Logger { return vm.ctx.Log }

/*
 ******************************************************************************
 ********************************* Snowman API ********************************
 ******************************************************************************
 */

// Initialize implements the snowman.ChainVM interface
func (vm *VM) Initialize(
	ctx *snow.Context,
	dbManager manager.Manager,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- commonEng.Message,
	fxs []*commonEng.Fx,
) error {
	if vm.CLIConfig.ParsingError != nil {
		return vm.CLIConfig.ParsingError
	}

	if len(fxs) > 0 {
		return errUnsupportedFXs
	}

	db := dbManager.Current()
	vm.ChainState = chainState.NewChainState(db, chainStateCacheSize)
	vm.shutdownChan = make(chan struct{}, 1)
	vm.ctx = ctx
	vm.db = vm.ChainState.ExternalDB()
	vm.chaindb = Database{prefixdb.New(ethDBPrefix, vm.db)}
	vm.acceptedAtomicTxDB = prefixdb.New(atomicTxPrefix, vm.db)
	g := new(core.Genesis)
	if err := json.Unmarshal(genesisBytes, g); err != nil {
		return err
	}

	vm.chainID = g.Config.ChainID
	vm.txFee = txFee

	config := eth.DefaultConfig
	config.ManualCanonical = true
	config.Genesis = g
	// disable the experimental snapshot feature from geth
	config.TrieCleanCache += config.SnapshotCache
	config.SnapshotCache = 0

	config.Miner.ManualMining = true
	config.Miner.DisableUncle = true

	// Set minimum price for mining and default gas price oracle value to the min
	// gas price to prevent so transactions and blocks all use the correct fees
	config.Miner.GasPrice = params.MinGasPrice
	config.RPCGasCap = vm.CLIConfig.RPCGasCap
	config.RPCTxFeeCap = vm.CLIConfig.RPCTxFeeCap
	config.GPO.Default = params.MinGasPrice
	config.TxPool.PriceLimit = params.MinGasPrice.Uint64()
	config.TxPool.NoLocals = true

	if err := config.SetGCMode("archive"); err != nil {
		panic(err)
	}
	nodecfg := node.Config{NoUSB: true}
	chain := coreth.NewETHChain(&config, &nodecfg, nil, vm.chaindb, vm.CLIConfig.EthBackendSettings())
	vm.chain = chain
	vm.networkID = config.NetworkId
	chain.SetOnHeaderNew(func(header *types.Header) {
		hid := make([]byte, 32)
		_, err := rand.Read(hid)
		if err != nil {
			panic("cannot generate hid")
		}
		header.Extra = append(header.Extra, hid...)
	})
	chain.SetOnFinalizeAndAssemble(func(state *state.StateDB, txs []*types.Transaction) ([]byte, error) {
		if tx, exists := vm.mempool.NextTx(); exists {
			if err := tx.UnsignedTx.(UnsignedAtomicTx).EVMStateTransfer(vm, state); err != nil {
				log.Error("Atomic transaction failed verification", "txID", tx.ID(), "error", err)
				// Discard the transaction from the mempool on failed verification.
				vm.mempool.DiscardCurrentTx()
				vm.newBlockChan <- nil
				return nil, err
			}
			atomicTxBytes, err := vm.codec.Marshal(codecVersion, tx)
			if err != nil {
				log.Error("Failed to marshal atomic transaction", "txID", tx.ID(), "error", err)
				// Discard the transaction from the mempool on failed verification.
				vm.mempool.DiscardCurrentTx()
				vm.newBlockChan <- nil
				return nil, err
			}
			return atomicTxBytes, nil
		}

		if len(txs) == 0 {
			// this could happen due to the async logic of geth tx pool
			vm.newBlockChan <- nil
			return nil, errEmptyBlock
		}

		return nil, nil
	})
	chain.SetOnSealFinish(func(block *types.Block) error {
		log.Trace("EVM sealed a block")

		blk := &Block{
			id:       ids.ID(block.Hash()),
			ethBlock: block,
			vm:       vm,
			status:   choices.Processing,
		}
		// Verify is called on a non-wrapped block here, such that this
		// does not add [blk] to the processing blocks map in ChainState.
		// TODO cache verification since Verify() will be called by the
		// consensus engine as well.
		// Note: this is only called when building a new block, so caching
		// verification will only be a significant optimization for nodes
		// that produce a large number of blocks.
		if err := blk.Verify(); err != nil {
			vm.newBlockChan <- nil
			return fmt.Errorf("block failed verification due to: %w", err)
		}
		vm.newBlockChan <- blk
		// TODO clean up tx pool stabilization logic
		vm.txPoolStabilizedLock.Lock()
		vm.txPoolStabilizedHead = block.Hash()
		vm.txPoolStabilizedLock.Unlock()
		return nil
	})
	chain.SetOnQueryAcceptedBlock(func() *types.Block {
		return vm.lastAcceptedEthBlock()
	})
	chain.SetOnExtraStateChange(func(block *types.Block, state *state.StateDB) error {
		tx := vm.extractAtomicTx(block)
		if tx == nil {
			return nil
		}
		return tx.UnsignedTx.(UnsignedAtomicTx).EVMStateTransfer(vm, state)
	})
	vm.newBlockChan = make(chan *Block)
	vm.notifyBuildBlockChan = toEngine

	// buildBlockTimer handles passing PendingTxs messages to the consensus engine.
	vm.buildBlockTimer = timer.NewStagedTimer(vm.buildBlockTwoStageTimer)
	vm.buildStatus = dontBuild
	go ctx.Log.RecoverAndPanic(vm.buildBlockTimer.Dispatch)

	vm.txPoolStabilizedOk = make(chan struct{}, 1)
	// TODO: read size from settings
	vm.mempool = NewMempool(defaultMempoolSize)
	vm.newMinedBlockSub = vm.chain.SubscribeNewMinedBlockEvent()
	vm.shutdownWg.Add(1)
	go ctx.Log.RecoverAndPanic(vm.awaitTxPoolStabilized)
	chain.Start()

	ethGenesisBlock := chain.GetGenesisBlock()
	genesisBlock := &Block{
		ethBlock: ethGenesisBlock,
		id:       ids.ID(ethGenesisBlock.Hash()),
		vm:       vm,
		status:   choices.Accepted,
	}
	vm.ChainState.Initialize(genesisBlock, vm.internalGetBlock, vm.internalParseBlock, vm.internalBuildBlock)

	lastAcceptedBlk := vm.ChainState.LastAcceptedBlock().Block.(*Block)
	vm.chain.SetPreference(lastAcceptedBlk.ethBlock)
	vm.genesisHash = ethGenesisBlock.Hash()
	log.Info("Initializing Coreth VM", "Last Accepted", lastAcceptedBlk.ethBlock.Hash().Hex())

	vm.shutdownWg.Add(1)
	go vm.ctx.Log.RecoverAndPanic(vm.awaitSubmittedTxs)
	vm.codec = Codec

	// The Codec explicitly registers the types it requires from the secp256k1fx
	// so [vm.baseCodec] is a dummy codec use to fulfill the secp256k1fx VM
	// interface. The fx will register all of its types, which can be safely
	// ignored by the VM's codec.
	vm.baseCodec = linearcodec.NewDefault()

	return vm.fx.Initialize(vm)
}

func (vm *VM) lastAcceptedEthBlock() *types.Block {
	return vm.LastAcceptedBlock().Block.(*Block).ethBlock
}

// Bootstrapping notifies this VM that the consensus engine is performing
// bootstrapping
func (vm *VM) Bootstrapping() error { return vm.fx.Bootstrapping() }

// Bootstrapped notifies this VM that the consensus engine has finished
// bootstrapping
func (vm *VM) Bootstrapped() error {
	vm.ctx.Bootstrapped()
	return vm.fx.Bootstrapped()
}

// Shutdown implements the snowman.ChainVM interface
func (vm *VM) Shutdown() error {
	if vm.ctx == nil {
		return nil
	}

	vm.buildBlockTimer.Stop()
	close(vm.shutdownChan)
	vm.chain.Stop()
	vm.shutdownWg.Wait()
	return nil
}

// internalBuildBlock ...
func (vm *VM) internalBuildBlock() (chainState.Block, error) {
	vm.chain.GenBlock()
	block := <-vm.newBlockChan

	// Set the buildStatus before calling Cancel or Issue on
	// the mempool, so that when the mempool adds a new item to Pending
	// it will be handled appropriately by [signalTxsReady]
	vm.buildBlockLock.Lock()
	if vm.needToBuild() {
		vm.buildStatus = conditionalBuild
		vm.buildBlockTimer.SetTimeoutIn(minBlockTime)
	} else {
		vm.buildStatus = dontBuild
	}
	vm.buildBlockLock.Unlock()

	if block == nil {
		// Signal the mempool that if it was attempting to issue an atomic
		// transaction into the next block, block building failed and the
		// transaction should be re-issued unless it was discarded during
		// atomic tx verification.
		vm.mempool.CancelCurrentTx()
		return nil, errCreateBlock
	}

	// Marks the current tx from the mempool as being successfully issued
	// into a block.
	vm.mempool.IssueCurrentTx()
	log.Debug(fmt.Sprintf("Built block %s", block.ID()))
	// make sure Tx Pool is updated
	<-vm.txPoolStabilizedOk
	return block, nil
}

// internalParseBlock
func (vm *VM) internalParseBlock(b []byte) (chainState.Block, error) {
	ethBlock := new(types.Block)
	if err := rlp.DecodeBytes(b, ethBlock); err != nil {
		return nil, err
	}
	if !vm.chain.VerifyBlock(ethBlock) {
		return nil, errFailedChainVerify
	}
	blockHash := ethBlock.Hash()
	// Coinbase must be zero on C-Chain
	if blockHash != vm.genesisHash && ethBlock.Coinbase() != coreth.BlackholeAddr {
		return nil, errInvalidBlock
	}
	block := &Block{
		id:       ids.ID(blockHash),
		ethBlock: ethBlock,
		vm:       vm,
		status:   choices.Processing,
	}
	return block, nil
}

func (vm *VM) internalGetBlock(id ids.ID) (chainState.Block, error) {
	ethBlock := vm.chain.GetBlockByHash(common.Hash(id))
	if ethBlock == nil {
		return nil, fmt.Errorf("block %s not found", id)
	}
	blk := &Block{
		id:       ids.ID(ethBlock.Hash()),
		ethBlock: ethBlock,
		vm:       vm,
		status:   choices.Processing,
	}
	return blk, nil
}

// SetPreference sets what the current tail of the chain is
func (vm *VM) SetPreference(blkID ids.ID) {
	block, err := vm.GetBlockInternal(blkID)
	vm.ctx.Log.AssertTrue(err == nil, "problem setting preferred block, couldn't find blkID %s", blkID)

	vm.chain.SetPreference(block.(*Block).ethBlock)
}

// NewHandler returns a new Handler for a service where:
//   * The handler's functionality is defined by [service]
//     [service] should be a gorilla RPC service (see https://www.gorillatoolkit.org/pkg/rpc/v2)
//   * The name of the service is [name]
//   * The LockOption is the first element of [lockOption]
//     By default the LockOption is WriteLock
//     [lockOption] should have either 0 or 1 elements. Elements beside the first are ignored.
func newHandler(name string, service interface{}, lockOption ...commonEng.LockOption) *commonEng.HTTPHandler {
	server := avalancheRPC.NewServer()
	server.RegisterCodec(avalancheJSON.NewCodec(), "application/json")
	server.RegisterCodec(avalancheJSON.NewCodec(), "application/json;charset=UTF-8")
	server.RegisterService(service, name)

	var lock commonEng.LockOption = commonEng.WriteLock
	if len(lockOption) != 0 {
		lock = lockOption[0]
	}
	return &commonEng.HTTPHandler{LockOptions: lock, Handler: server}
}

// CreateHandlers makes new http handlers that can handle API calls
func (vm *VM) CreateHandlers() map[string]*commonEng.HTTPHandler {
	handler := vm.chain.NewRPCHandler(time.Duration(vm.CLIConfig.APIMaxDuration))
	enabledAPIs := vm.CLIConfig.EthAPIs()
	vm.chain.AttachEthService(handler, vm.CLIConfig.EthAPIs())

	if vm.CLIConfig.SnowmanAPIEnabled {
		handler.RegisterName("snowman", &SnowmanAPI{vm})
		enabledAPIs = append(enabledAPIs, "snowman")
	}
	if vm.CLIConfig.CorethAdminAPIEnabled {
		handler.RegisterName("admin", &admin.Performance{})
		enabledAPIs = append(enabledAPIs, "coreth-admin")
	}
	if vm.CLIConfig.NetAPIEnabled {
		handler.RegisterName("net", &NetAPI{vm})
		enabledAPIs = append(enabledAPIs, "net")
	}
	if vm.CLIConfig.Web3APIEnabled {
		handler.RegisterName("web3", &Web3API{})
		enabledAPIs = append(enabledAPIs, "web3")
	}

	log.Info(fmt.Sprintf("Enabled APIs: %s", strings.Join(enabledAPIs, ", ")))

	return map[string]*commonEng.HTTPHandler{
		"/rpc":  {LockOptions: commonEng.NoLock, Handler: handler},
		"/avax": newHandler("avax", &AvaxAPI{vm}),
		"/ws":   {LockOptions: commonEng.NoLock, Handler: handler.WebsocketHandler([]string{"*"})},
	}
}

// CreateStaticHandlers makes new http handlers that can handle API calls
func (vm *VM) CreateStaticHandlers() map[string]*commonEng.HTTPHandler {
	handler := rpc.NewServer()
	handler.RegisterName("static", &StaticService{})
	return map[string]*commonEng.HTTPHandler{
		"/rpc": {LockOptions: commonEng.NoLock, Handler: handler},
		"/ws":  {LockOptions: commonEng.NoLock, Handler: handler.WebsocketHandler([]string{"*"})},
	}
}

/*
 ******************************************************************************
 *********************************** Helpers **********************************
 ******************************************************************************
 */
// extractAtomicTx returns the atomic transaction in [block] if
// one exists.
func (vm *VM) extractAtomicTx(block *types.Block) *Tx {
	extdata := block.ExtraData()
	atx := new(Tx)
	if _, err := vm.codec.Unmarshal(extdata, atx); err != nil {
		return nil
	}
	atx.Sign(vm.codec, nil)
	return atx
}

// getAcceptedAtomicTx attempts to get [txID] from the database.
func (vm *VM) getAcceptedAtomicTx(txID ids.ID) (*Tx, uint64, error) {
	indexedTxBytes, err := vm.acceptedAtomicTxDB.Get(txID[:])
	if err != nil {
		return nil, 0, err
	}

	packer := wrappers.Packer{Bytes: indexedTxBytes}
	height := packer.UnpackLong()
	txBytes := packer.UnpackBytes()

	tx := &Tx{}
	if _, err := vm.codec.Unmarshal(txBytes, tx); err != nil {
		return nil, 0, fmt.Errorf("problem parsing atomic transaction from db: %w", err)
	}
	if err := tx.Sign(vm.codec, nil); err != nil {
		return nil, 0, fmt.Errorf("problem initializing atomic transaction from db: %w", err)
	}

	return tx, height, nil
}

// getAtomicTx returns the requested transaction, status, and height.
// If the status is Unknown, then the returned transaction will be nil.
func (vm *VM) getAtomicTx(txID ids.ID) (*Tx, Status, uint64, error) {
	if tx, height, err := vm.getAcceptedAtomicTx(txID); err == nil {
		return tx, Accepted, height, nil
	} else if err != database.ErrNotFound {
		return nil, Unknown, 0, err
	}

	tx, dropped, found := vm.mempool.GetTx(txID)
	switch {
	case found && dropped:
		return tx, Dropped, 0, nil
	case found:
		return tx, Processing, 0, nil
	default:
		return nil, Unknown, 0, nil
	}
}

// writeAtomicTx writes indexes [tx] in [blk]
func (vm *VM) writeAtomicTx(blk *Block, tx *Tx) error {
	// 8 bytes
	height := blk.ethBlock.NumberU64()
	// 4 + len(txBytes)
	txBytes := tx.Bytes()
	packer := wrappers.Packer{Bytes: make([]byte, 12+len(txBytes))}
	packer.PackLong(height)
	packer.PackBytes(txBytes)
	txID := tx.ID()

	return vm.acceptedAtomicTxDB.Put(txID[:], packer.Bytes)
}

// awaitTxPoolStabilized waits for a txPoolHead channel event
// and notifies the VM when the tx pool has stabilized to the
// expected block hash
// Waits for signal to shutdown from [vm.shutdownChan]
func (vm *VM) awaitTxPoolStabilized() {
	defer vm.shutdownWg.Done()
	for {
		select {
		case e, ok := <-vm.newMinedBlockSub.Chan():
			if !ok {
				return
			}
			if e == nil {
				continue
			}
			switch h := e.Data.(type) {
			case core.NewMinedBlockEvent:
				vm.txPoolStabilizedLock.Lock()
				if vm.txPoolStabilizedHead == h.Block.Hash() {
					vm.txPoolStabilizedOk <- struct{}{}
					vm.txPoolStabilizedHead = common.Hash{}
				}
				vm.txPoolStabilizedLock.Unlock()
			default:
			}
		case <-vm.shutdownChan:
			return
		}
	}
}

// needToBuild returns true if there are outstanding transactions to be issued
// into a block.
func (vm *VM) needToBuild() bool {
	size, err := vm.chain.PendingSize()
	if err != nil {
		log.Error("Failed to get chain pending size", "error", err)
		return false
	}
	return size > 0 || vm.mempool.Len() > 0
}

// buildEarly returns true if there are sufficient outstanding transactions to
// be issued into a block to build a block early.
func (vm *VM) buildEarly() bool {
	size, err := vm.chain.PendingSize()
	if err != nil {
		log.Error("Failed to get chain pending size", "error", err)
		return false
	}
	return size > batchSize || vm.mempool.Len() > 1
}

// buildBlockTwoStageTimer is a two stage timer that sends a notification
// to the engine when the VM is ready to build a block.
// If it should be called back again, it returns the timeout duration at
// which it should be called again.
func (vm *VM) buildBlockTwoStageTimer() (time.Duration, bool) {
	vm.buildBlockLock.Lock()
	defer vm.buildBlockLock.Unlock()

	switch vm.buildStatus {
	case dontBuild:
		return 0, false
	case conditionalBuild:
		if !vm.buildEarly() {
			vm.buildStatus = mayBuild
			return (maxBlockTime - minBlockTime), true
		}
	case mayBuild:
	case building:
		// If the status has already been set to building, there is no need
		// to send an additional request to the consensus engine until the call
		// to BuildBlock resets the block status.
		return 0, false
	default:
		// Log an error if an invalid status is found.
		log.Error("Found invalid build status in build block timer", "buildStatus", vm.buildStatus)
	}

	select {
	case vm.notifyBuildBlockChan <- commonEng.PendingTxs:
		vm.buildStatus = building
	default:
		log.Error("Failed to push PendingTxs notification to the consensus engine.")
	}

	// No need for the timeout to fire again until BuildBlock is called.
	return 0, false
}

// signalTxsReady sets the initial timeout on the two stage timer if the process
// has not already begun from an earlier notification. If [buildStatus] is anything
// other than [dontBuild], then the attempt has already begun and this notification
// can be safely skipped.
func (vm *VM) signalTxsReady() {
	vm.buildBlockLock.Lock()
	defer vm.buildBlockLock.Unlock()

	// Set the build block timer in motion if it has not been started.
	if vm.buildStatus == dontBuild {
		vm.buildStatus = conditionalBuild
		vm.buildBlockTimer.SetTimeoutIn(minBlockTime)
	}
}

// awaitSubmittedTxs waits for new transactions to be submitted
// and notifies the VM when the tx pool has transactions to be
// put into a new block.
func (vm *VM) awaitSubmittedTxs() {
	defer vm.shutdownWg.Done()
	txSubmitChan := vm.chain.GetTxSubmitCh()
	for {
		select {
		case <-txSubmitChan:
			log.Trace("New tx detected, trying to generate a block")
			vm.signalTxsReady()
		case <-vm.mempool.Pending:
			log.Trace("New atomic Tx detected, trying to generate a block")
			vm.signalTxsReady()
		case <-vm.shutdownChan:
			return
		}
	}
}

// ParseAddress takes in an address and produces the ID of the chain it's for
// the ID of the address
func (vm *VM) ParseAddress(addrStr string) (ids.ID, ids.ShortID, error) {
	chainIDAlias, hrp, addrBytes, err := formatting.ParseAddress(addrStr)
	if err != nil {
		return ids.ID{}, ids.ShortID{}, err
	}

	chainID, err := vm.ctx.BCLookup.Lookup(chainIDAlias)
	if err != nil {
		return ids.ID{}, ids.ShortID{}, err
	}

	expectedHRP := constants.GetHRP(vm.ctx.NetworkID)
	if hrp != expectedHRP {
		return ids.ID{}, ids.ShortID{}, fmt.Errorf("expected hrp %q but got %q",
			expectedHRP, hrp)
	}

	addr, err := ids.ToShortID(addrBytes)
	if err != nil {
		return ids.ID{}, ids.ShortID{}, err
	}
	return chainID, addr, nil
}

// issueTx adds [tx] to the mempool and signals the goroutine waiting on
// atomic transactions that there is an atomic transaction ready to be
// put into a block.
func (vm *VM) issueTx(tx *Tx) error {
	return vm.mempool.AddTx(tx)
}

// GetAtomicUTXOs returns the utxos that at least one of the provided addresses is
// referenced in.
func (vm *VM) GetAtomicUTXOs(
	chainID ids.ID,
	addrs ids.ShortSet,
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

// GetSpendableFunds returns a list of EVMInputs and keys (in corresponding order)
// to total [amount] of [assetID] owned by [keys]
// TODO switch to returning a list of private keys
// since there are no multisig inputs in Ethereum
func (vm *VM) GetSpendableFunds(keys []*crypto.PrivateKeySECP256K1R, assetID ids.ID, amount uint64) ([]EVMInput, [][]*crypto.PrivateKeySECP256K1R, error) {
	// NOTE: should we use HEAD block or lastAccepted?
	state, err := vm.chain.BlockState(vm.lastAcceptedEthBlock())
	if err != nil {
		return nil, nil, err
	}
	inputs := []EVMInput{}
	signers := [][]*crypto.PrivateKeySECP256K1R{}
	// NOTE: we assume all keys correspond to distinct accounts here (so the
	// nonce handling in export_tx.go is correct)
	for _, key := range keys {
		if amount == 0 {
			break
		}
		addr := GetEthAddress(key)
		var balance uint64
		if assetID == vm.ctx.AVAXAssetID {
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
		nonce, err := vm.GetAcceptedNonce(addr)
		if err != nil {
			return nil, nil, err
		}
		inputs = append(inputs, EVMInput{
			Address: addr,
			Amount:  balance,
			AssetID: assetID,
			Nonce:   nonce,
		})
		signers = append(signers, []*crypto.PrivateKeySECP256K1R{key})
		amount -= balance
	}

	if amount > 0 {
		return nil, nil, errInsufficientFunds
	}

	return inputs, signers, nil
}

// GetAcceptedNonce returns the nonce associated with the address at the last accepted block
func (vm *VM) GetAcceptedNonce(address common.Address) (uint64, error) {
	state, err := vm.chain.BlockState(vm.lastAcceptedEthBlock())
	if err != nil {
		return 0, err
	}
	return state.GetNonce(address), nil
}

// ParseLocalAddress takes in an address for this chain and produces the ID
func (vm *VM) ParseLocalAddress(addrStr string) (ids.ShortID, error) {
	chainID, addr, err := vm.ParseAddress(addrStr)
	if err != nil {
		return ids.ShortID{}, err
	}
	if chainID != vm.ctx.ChainID {
		return ids.ShortID{}, fmt.Errorf("expected chainID to be %q but was %q",
			vm.ctx.ChainID, chainID)
	}
	return addr, nil
}

// FormatLocalAddress takes in a raw address and produces the formatted address
func (vm *VM) FormatLocalAddress(addr ids.ShortID) (string, error) {
	return vm.FormatAddress(vm.ctx.ChainID, addr)
}

// FormatAddress takes in a chainID and a raw address and produces the formatted
// address
func (vm *VM) FormatAddress(chainID ids.ID, addr ids.ShortID) (string, error) {
	chainIDAlias, err := vm.ctx.BCLookup.PrimaryAlias(chainID)
	if err != nil {
		return "", err
	}
	hrp := constants.GetHRP(vm.ctx.NetworkID)
	return formatting.FormatAddress(chainIDAlias, hrp, addr.Bytes())
}

// ParseEthAddress parses [addrStr] and returns an Ethereum address
func ParseEthAddress(addrStr string) (common.Address, error) {
	if !common.IsHexAddress(addrStr) {
		return common.Address{}, errInvalidAddr
	}
	return common.HexToAddress(addrStr), nil
}

// FormatEthAddress formats [addr] into a string
func FormatEthAddress(addr common.Address) string {
	return addr.Hex()
}

// GetEthAddress returns the ethereum address derived from [privKey]
func GetEthAddress(privKey *crypto.PrivateKeySECP256K1R) common.Address {
	return PublicKeyToEthAddress(privKey.PublicKey())
}

// PublicKeyToEthAddress returns the ethereum address derived from [pubKey]
func PublicKeyToEthAddress(pubKey crypto.PublicKey) common.Address {
	return ethcrypto.PubkeyToAddress(
		(*pubKey.(*crypto.PublicKeySECP256K1R).ToECDSA()))
}
