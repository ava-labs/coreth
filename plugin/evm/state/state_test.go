package evm

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/stretchr/testify/assert"
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

func (b *testBlock) SetStatus(status choices.Status) { b.status = status }

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

func cantBuildBlock() (Block, error) {
	return nil, errors.New("can't build new block")
}

// what do we want to test?

// test that something being removed from each cache works correctly

func TestChainState(t *testing.T) {
	db := memdb.New()

	blks := make(map[ids.ID]*testBlock)
	testBlks := newTestBlocks(3)
	genesisBlock := testBlks[0]
	blks[genesisBlock.ID()] = genesisBlock
	blk1 := testBlks[1]
	blk2 := testBlks[2]
	// Need to create a block with a different bytes and hash here
	// to generate a conflict with blk2
	blk3Bytes := []byte{byte(3)}
	blk3ID := hashing.ComputeHash256Array(blk3Bytes)
	blk3 := &testBlock{
		id:     blk3ID,
		bytes:  blk3Bytes,
		status: choices.Processing,
		height: uint64(2),
		parent: blk1,
	}

	chainState := NewChainState(prefixdb.New([]byte{1}, db), 2)
	// getBlock returns a block if it's already known
	getBlock := func(id ids.ID) (Block, error) {
		chainState.lock.Lock()
		defer chainState.lock.Unlock()

		if blk, ok := blks[id]; ok {
			return blk, nil
		}

		return nil, errors.New("unknown block")
	}
	// parseBlock adds the block to known blocks and returns the block
	parseBlock := func(b []byte) (Block, error) {
		chainState.lock.Lock()
		defer chainState.lock.Unlock()

		if len(b) != 1 {
			return nil, fmt.Errorf("unknown block with bytes length %d", len(b))
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
		// Save block, so that it will be returned by getBlock in the future
		blks[blk.ID()] = blk
		return blk, nil
	}
	err := chainState.Initialize(genesisBlock, getBlock, parseBlock, cantBuildBlock)
	if err != nil {
		t.Fatal(err)
	}

	if lastAccepted := chainState.LastAccepted(); lastAccepted != genesisBlock.ID() {
		t.Fatal("Expected last accepted block to be the genesis block")
	}

	// Check that a cache miss on a block is handled correctly
	if _, err := chainState.GetBlock(blk1.ID()); err == nil {
		t.Fatal("expected GetBlock to return an error for blk1 before it's been parsed")
	}
	if _, err := chainState.GetBlock(blk1.ID()); err == nil {
		t.Fatal("expected GetBlock to return an error for blk1 before it's been parsed")
	}

	// Parse and verify blk1 and 2
	parsedBlk1, err := chainState.ParseBlock(blk1.Bytes())
	if err != nil {
		t.Fatal("Failed to parse blk1 due to: %w", err)
	}
	if err := parsedBlk1.Verify(); err != nil {
		t.Fatal("Parsed blk1 failed verification unexpectedly due to %w", err)
	}
	parsedBlk2, err := chainState.ParseBlock(blk2.Bytes())
	if err != nil {
		t.Fatalf("Failed to parse blk2 due to: %s", err)
	}
	if err := parsedBlk2.Verify(); err != nil {
		t.Fatalf("Parsed blk2 failed verification unexpectedly due to %s", err)
	}

	// Check that the verified blocks have been placed in the processing map
	if numProcessing := len(chainState.processingBlocks); numProcessing != 2 {
		t.Fatalf("Expected chain state to have 2 processing blocks, but found: %d", numProcessing)
	}

	parsedBlk3, err := chainState.ParseBlock(blk3.Bytes())
	if err != nil {
		t.Fatalf("Failed to parse blk3 due to %s", err)
	}
	getBlk3, err := chainState.GetBlock(blk3.ID())
	if err != nil {
		t.Fatalf("Failed to get blk3 due to %s", err)
	}
	assert.Equal(t, parsedBlk3.ID(), getBlk3.ID(), "ChainState GetBlock returned the wrong block")

	// Check that parsing blk3 does not add it to processing blocks since it has
	// not been verified.
	if numProcessing := len(chainState.processingBlocks); numProcessing != 2 {
		t.Fatalf("Expected ChainState to have 2 processing blocks, but found: %d", numProcessing)
	}

	if err := parsedBlk3.Verify(); err != nil {
		t.Fatalf("Parsed blk3 failed verification unexpectedly due to %s", err)
	}
	// Check that blk3 has been added to processing blocks.
	if numProcessing := len(chainState.processingBlocks); numProcessing != 3 {
		t.Fatalf("Expected chain state to have 3 processing blocks, but found: %d", numProcessing)
	}

	// Decide the blocks and ensure they are removed from the processing blocks map
	if err := parsedBlk1.Accept(); err != nil {
		t.Fatal(err)
	}
	if err := parsedBlk2.Accept(); err != nil {
		t.Fatal(err)
	}
	if err := parsedBlk3.Reject(); err != nil {
		t.Fatal(err)
	}

	if numProcessing := len(chainState.processingBlocks); numProcessing != 0 {
		t.Fatalf("Expected chain state to have 0 processing blocks, but found: %d", numProcessing)
	}

	// Check that the last accepted block was updated correctly
	if lastAcceptedID := chainState.LastAccepted(); lastAcceptedID != blk2.ID() {
		t.Fatal("Expected last accepted block to be blk2")
	}
	if lastAcceptedID := chainState.LastAcceptedBlock().ID(); lastAcceptedID != blk2.ID() {
		t.Fatal("Expected last accepted block to be blk2")
	}

	// Check that ChainState from the same database accurately reflects the accepted chain.
	chainStateRestarted := NewChainState(prefixdb.New([]byte{1}, db), 2)
	err = chainStateRestarted.Initialize(genesisBlock, getBlock, parseBlock, cantBuildBlock)
	if err != nil {
		t.Fatal(err)
	}

	if lastAccepted := chainStateRestarted.LastAccepted(); lastAccepted != blk2.ID() {
		t.Fatal("Expected last accepted block to be blk2 after restart")
	}
	lastAcceptedBlk := chainStateRestarted.LastAcceptedBlock()
	if lastAcceptedBlk.ID() != blk2.ID() {
		t.Fatalf("Expected last accepted block to be blk2 after restart")
	}

	acceptedBlk2, err := chainStateRestarted.GetBlock(blk2.ID())
	assert.NoError(t, err, "ChainState should not have errored retrieving accepted block")

	acceptedBlk1 := acceptedBlk2.Parent()
	if status := acceptedBlk1.Status(); status != choices.Accepted {
		t.Fatalf("blk1 should have been accepted, but had status %s", status)
	}
	if acceptedBlk1.ID() != blk1.ID() {
		t.Fatalf("Found unexpected blkID for the parent of the last accepted block")
	}
	acceptedBlk0 := acceptedBlk1.Parent()
	if status := acceptedBlk0.Status(); status != choices.Accepted {
		t.Fatalf("blk0 should have been accepted, but had status %s", status)
	}
	if acceptedBlk0.ID() != genesisBlock.ID() {
		t.Fatalf("Found unexpected blkID for ancestor of last accepted block")
	}
	rejectedBlk3, err := chainStateRestarted.GetBlock(blk3.ID())
	if err != nil {
		t.Fatalf("Unexpected error retrieving blk3 from restarted ChainState %s", err)
	}

	// Check each block
	checkAcceptedBlock(t, chainState, acceptedBlk2, false)
	checkAcceptedBlock(t, chainState, acceptedBlk1, false)
	checkAcceptedBlock(t, chainState, acceptedBlk0, false)
	checkRejectedBlock(t, chainState, rejectedBlk3, false)
}

func TestBuildBlock(t *testing.T) {
	db := memdb.New()

	blks := make(map[ids.ID]*testBlock)
	testBlks := newTestBlocks(2)
	genesisBlock := testBlks[0]
	blks[genesisBlock.ID()] = genesisBlock
	genesisBlock.Accept()
	blk1 := testBlks[1]

	chainState := NewChainState(prefixdb.New([]byte{1}, db), 2)
	// getBlock returns a block if it's already known
	getBlock := func(id ids.ID) (Block, error) {
		chainState.lock.Lock()
		defer chainState.lock.Unlock()

		if blk, ok := blks[id]; ok {
			return blk, nil
		}

		return nil, errors.New("unknown block")
	}
	// parseBlock adds the block to known blocks and returns the block
	parseBlock := func(b []byte) (Block, error) {
		chainState.lock.Lock()
		defer chainState.lock.Unlock()

		if len(b) != 1 {
			return nil, fmt.Errorf("unknown block with bytes length %d", len(b))
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
	buildBlock := func() (Block, error) {
		chainState.lock.Lock()
		defer chainState.lock.Unlock()

		return blk1, nil
	}

	err := chainState.Initialize(genesisBlock, getBlock, parseBlock, buildBlock)
	if err != nil {
		t.Fatal(err)
	}

	builtBlk, err := chainState.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}
	assert.Len(t, chainState.processingBlocks, 0)

	if err := builtBlk.Verify(); err != nil {
		t.Fatalf("Built block failed verification due to %s", err)
	}
	assert.Len(t, chainState.processingBlocks, 1)

	checkProcessingBlock(t, chainState, builtBlk)

	if err := builtBlk.Accept(); err != nil {
		t.Fatalf("Unexpected error while accepting built block %s", err)
	}

	checkAcceptedBlock(t, chainState, builtBlk, true)
}

// checkProcessingBlock checks that [blk] is of the correct type and is
// correctly uniquified when calling GetBlock and ParseBlock.
func checkProcessingBlock(t *testing.T, c *ChainState, blk snowman.Block) {
	if _, ok := blk.(*BlockWrapper); !ok {
		t.Fatalf("Expected block to be of type (*BlockWrapper)")
	}

	parsedBlk, err := c.ParseBlock(blk.Bytes())
	if err != nil {
		t.Fatalf("Failed to parse verified block due to %s", err)
	}
	if parsedBlk.ID() != blk.ID() {
		t.Fatalf("Expected parsed block to have the same ID as the requested block")
	}
	if !bytes.Equal(parsedBlk.Bytes(), blk.Bytes()) {
		t.Fatalf("Expected parsed block to have the same bytes as the requested block")
	}
	if status := parsedBlk.Status(); status != choices.Processing {
		t.Fatalf("Expected parsed block to have status Processing, but found %s", status)
	}
	if parsedBlk != blk {
		t.Fatalf("Expected parsed block to return a uniquified block")
	}

	getBlk, err := c.GetBlock(blk.ID())
	if err != nil {
		t.Fatalf("Unexpected error during GetBlock for processing block %s", err)
	}
	if getBlk != parsedBlk {
		t.Fatalf("Expected GetBlock to return the same unique block as ParseBlock")
	}
}

// checkDecidedBlock asserts that [blk] is returned with the correct status by ParseBlock
// and GetBlock.
func checkDecidedBlock(t *testing.T, c *ChainState, blk snowman.Block, expectedStatus choices.Status, cached bool) {
	if _, ok := blk.(*BlockWrapper); !ok {
		t.Fatalf("Expected block to be of type (*BlockWrapper)")
	}

	parsedBlk, err := c.ParseBlock(blk.Bytes())
	if err != nil {
		t.Fatalf("Unexpected error parsing decided block %s", err)
	}
	if parsedBlk.ID() != blk.ID() {
		t.Fatalf("ParseBlock returned block with unexpected ID %s, expected %s", parsedBlk.ID(), blk.ID())
	}
	if !bytes.Equal(parsedBlk.Bytes(), blk.Bytes()) {
		t.Fatalf("Expected parsed block to have the same bytes as the requested block")
	}
	if status := parsedBlk.Status(); status != expectedStatus {
		t.Fatalf("Expected parsed block to have status %s, but found %s", expectedStatus, status)
	}
	// If the block should be in the cache, assert that the returned block is identical to [blk]
	if cached && parsedBlk != blk {
		t.Fatalf("Expected parsed block to have been cached, but retrieved non-unique decided block")
	}

	getBlk, err := c.GetBlock(blk.ID())
	if err != nil {
		t.Fatalf("Unexpected error during GetBlock for decided block %s", err)
	}
	if getBlk.ID() != blk.ID() {
		t.Fatalf("GetBlock returned block with unexpected ID %s, expected %s", getBlk.ID(), blk.ID())
	}
	if !bytes.Equal(getBlk.Bytes(), blk.Bytes()) {
		t.Fatalf("Expected block from GetBlock to have the same bytes as the requested block")
	}
	if status := getBlk.Status(); status != expectedStatus {
		t.Fatalf("Expected block from GetBlock to have status %s, but found %s", expectedStatus, status)
	}

	// Since ParseBlock should have triggered a cache hit, assert that the block is identical
	// to the parsed block.
	if getBlk != parsedBlk {
		t.Fatalf("Expected block returned by GetBlock to have been cached, but retrieved non-unique decided block")
	}
}

func checkAcceptedBlock(t *testing.T, c *ChainState, blk snowman.Block, cached bool) {
	checkDecidedBlock(t, c, blk, choices.Accepted, cached)
}

func checkRejectedBlock(t *testing.T, c *ChainState, blk snowman.Block, cached bool) {
	checkDecidedBlock(t, c, blk, choices.Rejected, cached)
}
