package counter

import (
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

func TestTxCounter(t *testing.T) {
	tc := NewTxCounter()

	tests := []struct {
		name     string
		tx       *types.Transaction
		expected uint64
	}{
		{
			name:     "First transaction",
			tx:       types.NewTransaction(0, common.Address{}, big.NewInt(100), 21000, big.NewInt(1e9), []byte("test data")),
			expected: 1,
		},
		{
			name:     "Second transaction",
			tx:       types.NewTransaction(1, common.Address{}, big.NewInt(200), 21000, big.NewInt(1e9), []byte("test data 2")),
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txWithNum, txNum := tc.AddTx(tt.tx)

			assert.Equal(t, tt.expected, txNum, "txNum should match expected value")
			assert.Equal(t, tt.tx.Hash(), txWithNum.Hash(), "transaction hash should remain unchanged")
		})
	}
}
