// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocksync

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/message"
	synccommon "github.com/ava-labs/coreth/sync"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/handlers"
	handlerstats "github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/stretchr/testify/require"
)

func TestConfigValidation(t *testing.T) {
	mockClient := syncclient.NewTestClient(
		message.Codec,
		nil,
		nil,
		nil,
	)
	validHash := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")

	tests := []struct {
		name    string
		config  *Config
		wantErr error
	}{
		{
			name:    "valid config",
			config:  &Config{ChainDB: rawdb.NewMemoryDatabase(), Client: mockClient, FromHash: validHash, FromHeight: 10, ParentsToGet: 5},
			wantErr: nil,
		},
		{
			name:    "nil database",
			config:  &Config{ChainDB: nil, Client: mockClient, FromHash: validHash, FromHeight: 10, ParentsToGet: 5},
			wantErr: errNilDatabase,
		},
		{
			name:    "nil client",
			config:  &Config{ChainDB: rawdb.NewMemoryDatabase(), Client: nil, FromHash: validHash, FromHeight: 10, ParentsToGet: 5},
			wantErr: errNilClient,
		},
		{
			name:    "empty from hash",
			config:  &Config{ChainDB: rawdb.NewMemoryDatabase(), Client: mockClient, FromHash: common.Hash{}, FromHeight: 10, ParentsToGet: 5},
			wantErr: errInvalidFromHash,
		},
		{
			name:    "parents to get exceeds from height",
			config:  &Config{ChainDB: rawdb.NewMemoryDatabase(), Client: mockClient, FromHash: validHash, FromHeight: 2, ParentsToGet: 5},
			wantErr: errParentsToGetExceedsFromHeight,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestWaitBeforeStart(t *testing.T) {
	env := newTestEnvironment(t, 10).withDefaultBlockProvider()

	syncer, err := env.createSyncer(5, 3)
	require.NoError(t, err)
	require.NotNil(t, syncer)

	err = syncer.Wait(context.Background())
	require.ErrorIs(t, err, synccommon.ErrWaitBeforeStart)
}

func TestBlockSyncer_NormalCase(t *testing.T) {
	// Test normal case where all blocks are retrieved from network
	env := newTestEnvironment(t, 10).withDefaultBlockProvider()
	syncer, err := env.createSyncer(5, 3) // Sync blocks 3, 4, 5 (fromHeight=5, parentsToGet=3)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, syncer.Start(ctx))
	require.NoError(t, syncer.Wait(ctx))

	// Verify blocks 3, 4, 5 are in database
	env.verifyBlocksInDB(t, []int{3, 4, 5})
}

func TestBlockSyncer_AllBlocksAlreadyAvailable(t *testing.T) {
	// Test case where all blocks are already on disk
	env := newTestEnvironment(t, 10).withDefaultBlockProvider()
	// Pre-populate blocks 3, 4, 5 in the database
	env.prePopulateBlocks(3, 4, 5)
	syncer, err := env.createSyncer(5, 3) // Sync blocks 3, 4, 5
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, syncer.Start(ctx))
	require.NoError(t, syncer.Wait(ctx))

	// Verify blocks 3, 4, 5 are still in database
	env.verifyBlocksInDB(t, []int{3, 4, 5})

	// Client should not have received any block requests since all blocks were on disk
	require.Equal(t, int32(0), env.client.BlocksReceived())
}

func TestBlockSyncer_SomeBlocksAlreadyAvailable(t *testing.T) {
	// Test case where some blocks are already on disk
	env := newTestEnvironment(t, 10).withDefaultBlockProvider()
	// Pre-populate only blocks 4 and 5 in the database
	env.prePopulateBlocks(4, 5)
	syncer, err := env.createSyncer(5, 3) // Sync blocks 3, 4, 5
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, syncer.Start(ctx))
	require.NoError(t, syncer.Wait(ctx))

	// Verify all blocks 3, 4, 5 are in database
	env.verifyBlocksInDB(t, []int{3, 4, 5})
}

func TestBlockSyncer_MostRecentBlockMissing(t *testing.T) {
	// Test case where only the most recent block is missing
	env := newTestEnvironment(t, 10).withDefaultBlockProvider()
	// Pre-populate blocks 3 and 4, but not 5
	env.prePopulateBlocks(3, 4)
	syncer, err := env.createSyncer(5, 3) // Sync blocks 3, 4, 5
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, syncer.Start(ctx))
	require.NoError(t, syncer.Wait(ctx))

	// Verify all blocks 3, 4, 5 are in database
	env.verifyBlocksInDB(t, []int{3, 4, 5})
}

func TestBlockSyncer_EdgeCaseFromHeight1(t *testing.T) {
	// Test syncing from the first generated block (height 1)
	env := newTestEnvironment(t, 10).withDefaultBlockProvider()
	syncer, err := env.createSyncer(1, 1) // Sync only block 1
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, syncer.Start(ctx))
	require.NoError(t, syncer.Wait(ctx))

	// Verify block 1 is in database
	env.verifyBlocksInDB(t, []int{1})
}

func TestBlockSyncer_SingleBlock(t *testing.T) {
	// Test syncing a single block in the middle of the chain
	env := newTestEnvironment(t, 10).withDefaultBlockProvider()

	syncer, err := env.createSyncer(7, 1) // Sync only block 7
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, syncer.Start(ctx))
	require.NoError(t, syncer.Wait(ctx))

	// Verify only block 7 is in database
	env.verifyBlocksInDB(t, []int{7})
}

func TestBlockSyncer_ContextCancellation(t *testing.T) {
	// Allow some time for startup for cancellation
	env := newTestEnvironment(t, 10).withCustomBlockProvider(func(hash common.Hash, height uint64) *types.Block {
		time.Sleep(100 * time.Millisecond)
		return nil
	})

	syncer, err := env.createSyncer(5, 3)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, syncer.Start(ctx))

	// Cancel context immediately
	cancel()

	err = syncer.Wait(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

// testEnvironment provides an abstraction for setting up block syncer tests
type testEnvironment struct {
	chainDB ethdb.Database
	client  *syncclient.TestClient
	blocks  []*types.Block
}

// newTestEnvironment creates a new test environment with generated blocks
func newTestEnvironment(t *testing.T, numBlocks int) *testEnvironment {
	t.Helper()

	var (
		key1, _        = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _        = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1          = crypto.PubkeyToAddress(key1.PublicKey)
		addr2          = crypto.PubkeyToAddress(key2.PublicKey)
		genesisBalance = big.NewInt(1000000000)
		signer         = types.HomesteadSigner{}
	)

	// Ensure that key1 has some funds in the genesis block.
	gspec := &core.Genesis{
		Config: &params.ChainConfig{HomesteadBlock: new(big.Int)},
		Alloc:  types.GenesisAlloc{addr1: {Balance: genesisBalance}},
	}
	engine := dummy.NewETHFaker()

	_, blocks, _, err := core.GenerateChainWithGenesis(gspec, engine, numBlocks, 0, func(i int, gen *core.BlockGen) {
		// Generate a transaction to create a unique block
		tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(10), params.TxGas, nil, nil), signer, key1)
		gen.AddTx(tx)
	})
	require.NoError(t, err)

	// The genesis block is not include in the blocks slice, so we need to prepend it
	blocks = append([]*types.Block{gspec.ToBlock()}, blocks...)

	return &testEnvironment{
		chainDB: rawdb.NewMemoryDatabase(),
		blocks:  blocks,
	}
}

// withCustomBlockProvider sets up the test client with a custom block provider function
func (e *testEnvironment) withCustomBlockProvider(onGetBlock func(hash common.Hash, height uint64) *types.Block) *testEnvironment {
	blockProvider := &handlers.TestBlockProvider{GetBlockFn: onGetBlock}
	blockHandler := handlers.NewBlockRequestHandler(
		blockProvider,
		message.Codec,
		handlerstats.NewNoopHandlerStats(),
	)
	e.client = syncclient.NewTestClient(
		message.Codec,
		nil,
		nil,
		blockHandler,
	)
	return e
}

// withDefaultBlockProvider sets up the test client with a provider that returns all blocks
func (e *testEnvironment) withDefaultBlockProvider() *testEnvironment {
	return e.withCustomBlockProvider(func(hash common.Hash, height uint64) *types.Block {
		if height > uint64(len(e.blocks)) {
			return nil
		}
		block := e.blocks[height]
		if block.Hash() != hash {
			return nil
		}
		return block
	})
}

// prePopulateBlocks writes some blocks to the database before syncing (by block height)
func (e *testEnvironment) prePopulateBlocks(blockHeights ...int) *testEnvironment {
	batch := e.chainDB.NewBatch()
	for _, height := range blockHeights {
		if height <= len(e.blocks) {
			// blocks[0] is block number 1, blocks[1] is block number 2, etc.
			block := e.blocks[height]
			rawdb.WriteBlock(batch, block)
			rawdb.WriteCanonicalHash(batch, block.Hash(), block.NumberU64())
		}
	}
	require.NoError(nil, batch.Write())
	return e
}

// createSyncer creates a block syncer with the given configuration
func (e *testEnvironment) createSyncer(fromHeight uint64, parentsToGet uint64) (*blockSyncer, error) {
	if fromHeight > uint64(len(e.blocks)) {
		return nil, fmt.Errorf("fromHeight %d exceeds available blocks %d", fromHeight, len(e.blocks))
	}

	config := &Config{
		ChainDB:      e.chainDB,
		Client:       e.client,
		FromHash:     e.blocks[fromHeight].Hash(),
		FromHeight:   fromHeight,
		ParentsToGet: parentsToGet,
	}

	return NewSyncer(config)
}

// verifyBlocksInDB checks that the expected blocks are present in the database (by block height)
func (e *testEnvironment) verifyBlocksInDB(t *testing.T, expectedBlockHeights []int) {
	t.Helper()

	// Verify expected blocks are present
	for _, height := range expectedBlockHeights {
		if height > len(e.blocks) {
			continue
		}
		block := e.blocks[height]
		dbBlock := rawdb.ReadBlock(e.chainDB, block.Hash(), block.NumberU64())
		require.NotNil(t, dbBlock, "Block %d should be in database", height)
		require.Equal(t, block.Hash(), dbBlock.Hash(), "Block %d hash mismatch", height)
	}
}
