package clienttypes

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/utils"
	ethcore "github.com/ava-labs/libevm/core"
	ethparams "github.com/ava-labs/libevm/params"
)

func TestMarshalGenesis(t *testing.T) {
	bytes, err := MarshalGenesis(&ethcore.Genesis{
		Config: &ethparams.ChainConfig{
			LondonBlock: big.NewInt(1),
		},
	}, &ChainConfigExtra{
		NetworkUpgrades: NetworkUpgrades{
			ApricotPhase1BlockTimestamp: utils.NewUint64(1),
		},
	})
	require.NoError(t, err)

	// Check that the marshalled genesis contains the expected fields
	require.Contains(t, string(bytes), "apricotPhase1BlockTimestamp")
	require.Contains(t, string(bytes), "londonBlock")
}
