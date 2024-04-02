package evm

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/ethclient"
	"github.com/stretchr/testify/require"
)

// This test is to reproduce the issue 522
func TestIssue522(t *testing.T) {
	ctx := context.Background()
	require := require.New(t)
	client, err := ethclient.Dial("https://api.avax.network/ext/bc/C/rpc")
	require.NoError(err)
	block, err := client.BlockByNumber(ctx, big.NewInt(0x29a0eb5))
	require.NoError(err)
	isApricotPhase5 := true
	atomicTxs, err := ExtractAtomicTxs(block.ExtData(), isApricotPhase5, Codec)
	require.NoError(err)
	require.Len(atomicTxs, 1)

	tx := atomicTxs[0]
	fmt.Println(tx.ID())
	expected := ids.FromStringOrPanic("2VCX1fzRDDxayrrfi7nUuUr8GboguT2o8ZuDSH2aPfYxUZKJ4E")
	require.Equal(expected, tx.ID())
}
