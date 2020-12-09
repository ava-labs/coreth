package evm

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

type testBlock struct {
	id        ids.ID
	bytes     []byte
	height    uint64
	status    choices.Status
	parent    *testBlock
	onAccept  func() error
	onReject  func() error
	verifyErr error
}

func (b *testBlock) ID() ids.ID { return b.id }

func (b *testBlock) Height() uint64 { return b.height }

func (b *testBlock) Accept() error {
	b.status = choices.Accepted
	if b.onAccept == nil {
		return nil
	}
	return b.onAccept()
}

func (b *testBlock) Reject() error {
	b.status = choices.Rejected
	if b.onReject == nil {
		return nil
	}
	return b.onReject()
}

func (b *testBlock) Parent() snowman.Block { return b.parent }

func (b *testBlock) Verify() error { return b.verifyErr }

func (b *testBlock) Status() choices.Status { return b.status }

func (b *testBlock) Bytes() []byte { return b.bytes }

func newTestBlock(i int, parent *testBlock) *testBlock {
	b := []byte{byte(i)}
	id := hashing.ComputeHash256Array(b)
	return &testBlock{
		id:     id,
		bytes:  b,
		status: choices.Processing,
		height: uint64(i),
		parent: parent,
	}
}

func newTestBlocks(numBlocks int) []*testBlock {
	blks := make([]*testBlock, 0, numBlocks)
	var lastBlock *testBlock
	for i := 0; i < numBlocks; i++ {
		blks = append(blks, newTestBlock(i, lastBlock))
		lastBlock = blks[len(blks)-1]
	}

	return blks
}

func cantGetBlock(ids.ID) (snowman.Block, error) {
	return nil, errors.New("can't get block")
}

func cantBuildBlock() (snowman.Block, error) {
	return nil, errors.New("can't build new block")
}

func cantParseBlock([]byte) (snowman.Block, error) {
	return nil, errors.New("can't parse block")
}

func checkWrappedBlock(t *testing.T, blk snowman.Block, expectedID ids.ID, expectedStatus choices.Status) {
	if _, ok := blk.(*BlockWrapper); !ok {
		t.Fatalf("Expected snowman Block to be a wrapped block")
	}

	if blkID := blk.ID(); blkID != expectedID {
		t.Fatalf("Expected block to have ID: %s, but found %s", expectedID, blkID)
	}

	if status := blk.Status(); status != expectedStatus {
		t.Fatalf("Expected block to hav status %s, but found %s", expectedStatus, status)
	}
}

// checkBlock returns an error if [state] returns a block that does not match [blk]
// in any way from GetBlock or ParseBlock
// additionally asserts that the block is correclty uniquified. If [uniqueBlock] is
// non-nil then the returned block must be equivalent to [uniqueBlock]
func checkBlock(state *ChainState, blk *testBlock, uniqueBlock snowman.Block) (snowman.Block, error) {
	// Parse Block first, so that getBlock should be able to get the block
	parseBlk, err := state.ParseBlock(blk.Bytes())
	if err != nil {
		return nil, fmt.Errorf("problem parsing block %s: %w", blk.ID(), err)
	}

	// GetBlock
	getBlk, err := state.GetBlock(blk.ID())
	if err != nil {
		return nil, fmt.Errorf("problem getting block %s: %w", blk.ID(), err)
	}

	if getBlk != parseBlk {
		return nil, errors.New("expected block returned by GetBlock and ParseBlock to be the same")
	}

	if uniqueBlock != nil && getBlk != uniqueBlock {
		return nil, errors.New("expected returned block to match unique block")
	}

	// assert that the status and ID is as expected
	if _, ok := getBlk.(*BlockWrapper); !ok {
		return nil, errors.New("expected snowman Block to be a wrapped block")
	}

	if expectedID, blkID := blk.ID(), getBlk.ID(); blkID != expectedID {
		return nil, fmt.Errorf("expected block to have ID: %s, but found %s", expectedID, blkID)
	}

	if expectedStatus, status := blk.Status(), getBlk.Status(); status != expectedStatus {
		return nil, fmt.Errorf("expected block to hav status %s, but found %s", expectedStatus, status)
	}

	return getBlk, nil
}

func TestChainState(t *testing.T) {
	db := memdb.New()

	vdb := versiondb.New(prefixdb.New([]byte{1}, db))
	blks := make(map[ids.ID]*testBlock)
	testBlks := newTestBlocks(3)
	genesisBlock := testBlks[0]
	blks[genesisBlock.ID()] = genesisBlock
	genesisBlock.Accept()
	blk1 := testBlks[1]
	blk2 := testBlks[2]
	blk3Bytes := []byte{byte(3)}
	blk3ID := hashing.ComputeHash256Array(blk3Bytes)
	blk3 := &testBlock{
		id:     blk3ID,
		bytes:  blk3Bytes,
		status: choices.Processing,
		height: uint64(2),
		parent: blk1,
	}

	// getBlock returns a block if it's already known
	getBlock := func(id ids.ID) (snowman.Block, error) {
		if blk, ok := blks[id]; ok {
			return blk, nil
		}

		return nil, errors.New("unknown block")
	}
	// parseBlock adds the block to known blocks and returns the block
	parseBlock := func(b []byte) (snowman.Block, error) {
		if len(b) > 1 {
			return nil, errors.New("unknown block with bytes length greater than 1")
		}
		var blk *testBlock
		switch b[0] {
		case 0:
			blk = genesisBlock
		case 1:
			blk = blk1
		case 2:
			blk = blk2
		case 3:
			blk = blk3
		}
		if blk == nil {
			return nil, errors.New("tried to parse unexpected block")
		}
		// Put the block in "database" now that it's been parsed
		blks[blk.ID()] = blk
		return blk, nil
	}
	// Check that NewChainState initializes the genesis block as the last accepted block correctly
	chainState, err := NewChainState(vdb, genesisBlock, getBlock, parseBlock, cantBuildBlock, 2)
	if err != nil {
		t.Fatal(err)
	}

	uniqueGenBlock, err := checkBlock(chainState, genesisBlock, nil)
	if err != nil {
		t.Fatalf("problem with genesis block: %s", err)
	}

	if lastAccepted := chainState.LastAccepted(); lastAccepted != genesisBlock.ID() {
		t.Fatal("Expected last accepted block to be genesis block")
	}

	// Check that a cache miss on a block is handled correctly
	if _, err := chainState.GetBlock(blk1.ID()); err == nil {
		t.Fatal("expected GetBlock to return an error for blk1 before it's been parsed")
	}
	if _, err := chainState.GetBlock(blk1.ID()); err == nil {
		t.Fatal("expected GetBlock to return an error for blk1 before it's been parsed")
	}

	uniqueBlk1, err := checkBlock(chainState, blk1, nil)
	if err != nil {
		t.Fatalf("problem with blk1: %s", err)
	}

	uniqueBlk2, err := checkBlock(chainState, blk2, nil)
	if err != nil {
		t.Fatalf("problem with blk2: %s", err)
	}

	// Check that the parsed blocks have been placed in the processing map
	if numProcessing := len(chainState.processingBlocks); numProcessing != 2 {
		t.Fatalf("Expected chain state to have 2 processing blocks, but found: %d", numProcessing)
	}

	// Check that ParseBlock and GetBlock return the unqiue block
	// pinned in memory
	if _, err := checkBlock(chainState, blk1, uniqueBlk1); err != nil {
		t.Fatalf("problem with blk1: %s", err)
	}
	if _, err := checkBlock(chainState, blk2, uniqueBlk2); err != nil {
		t.Fatalf("problem with blk2: %s", err)
	}

	// Parse blk3 which conflicts with blk2
	uniqueBlk3, err := checkBlock(chainState, blk3, nil)
	if err != nil {
		t.Fatalf("problem with blk3: %s", err)
	}

	// Decide the blocks and ensure that the accepted blocks
	// are removed from the processing map and that the genesis
	// block is removed from the decided cache
	if err := uniqueBlk1.Accept(); err != nil {
		t.Fatal(err)
	}
	if err := uniqueBlk2.Accept(); err != nil {
		t.Fatal(err)
	}
	if err := uniqueBlk3.Reject(); err != nil {
		t.Fatal(err)
	}

	// Check that the decided blocks have been removed from the processing
	// blocks map
	if numProcessing := len(chainState.processingBlocks); numProcessing != 0 {
		t.Fatalf("Expected chain state to have 0 processing blocks, but found: %d", numProcessing)
	}

	// Check that the genesis block was evicted from the decided blocks
	// cache of size 2
	getGenBlockPostEviction, err := chainState.GetBlock(genesisBlock.ID())
	if err != nil {
		t.Fatal(err)
	}
	if getGenBlockPostEviction == uniqueGenBlock {
		t.Fatal("Expected genesis block to be evicted from decided cache")
	}

	// Check that ParseBlock and GetBlock return the decided blocks
	// correctly
	if _, err := checkBlock(chainState, blk1, nil); err != nil {
		t.Fatalf("problem with blk1: %s", err)
	}
	if _, err := checkBlock(chainState, blk2, nil); err != nil {
		t.Fatalf("problem with bl2: %s", err)
	}
	if _, err := checkBlock(chainState, blk3, nil); err != nil {
		t.Fatalf("problem with blk3: %s", err)
	}

	// Check that the last accepted block was updated correctly
	if lastAcceptedID := chainState.LastAccepted(); lastAcceptedID != blk2.ID() {
		t.Fatal("Expected last accepted block to be blk2")
	}
	if lastAcceptedID := chainState.LastAcceptedBlock().ID(); lastAcceptedID != blk2.ID() {
		t.Fatal("Expected last accepted block to be blk2")
	}

	// Check that ChainState accurately reflects the decided
	// blocks after a restart
	vdbRestart := versiondb.New(prefixdb.New([]byte{1}, db))
	chainStateRestarted, err := NewChainState(vdbRestart, genesisBlock, getBlock, parseBlock, cantBuildBlock, 2)
	if err != nil {
		t.Fatal(err)
	}

	if lastAccepted := chainStateRestarted.LastAccepted(); lastAccepted != blk2.ID() {
		t.Fatal("Expected last accepted block to be blk2 after restart")
	}
	if lastAcceptedID := chainState.LastAcceptedBlock().ID(); lastAcceptedID != blk2.ID() {
		t.Fatal("Expected last accepted block to be blk2 after restart")
	}
	if _, err := checkBlock(chainState, blk1, nil); err != nil {
		t.Fatalf("problem with blk1: %s", err)
	}
	if _, err := checkBlock(chainState, blk2, nil); err != nil {
		t.Fatalf("problem with bl2: %s", err)
	}
	if _, err := checkBlock(chainState, blk3, nil); err != nil {
		t.Fatalf("problem with blk3: %s", err)
	}
}

func TestBuildBlock(t *testing.T) {
	db := memdb.New()

	vdb := versiondb.New(prefixdb.New([]byte{1}, db))
	blks := make(map[ids.ID]*testBlock)
	testBlks := newTestBlocks(2)
	genesisBlock := testBlks[0]
	blks[genesisBlock.ID()] = genesisBlock
	genesisBlock.Accept()
	blk1 := testBlks[1]

	// getBlock returns a block if it's already known
	getBlock := func(id ids.ID) (snowman.Block, error) {
		if blk, ok := blks[id]; ok {
			return blk, nil
		}

		return nil, errors.New("unknown block")
	}
	// parseBlock adds the block to known blocks and returns the block
	parseBlock := func(b []byte) (snowman.Block, error) {
		if len(b) > 1 {
			return nil, errors.New("unknown block with bytes length greater than 1")
		}
		var blk *testBlock
		switch b[0] {
		case 0:
			blk = genesisBlock
		case 1:
			blk = blk1
		}
		if blk == nil {
			return nil, errors.New("tried to parse unexpected block")
		}
		// Put the block in "database" now that it's been parsed
		blks[blk.ID()] = blk
		return blk, nil
	}
	buildBlock := func() (snowman.Block, error) { return blk1, nil }
	// Check that NewChainState initializes the genesis block as the last accepted block correctly
	chainState, err := NewChainState(vdb, genesisBlock, getBlock, parseBlock, buildBlock, 2)
	if err != nil {
		t.Fatal(err)
	}

	uniqueBlk1, err := chainState.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := checkBlock(chainState, blk1, uniqueBlk1); err != nil {
		t.Fatalf("problem with blk1: %s", err)
	}
}
