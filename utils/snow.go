// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

var (
	testCChainID    = ids.ID{'c', 'c', 'h', 'a', 'i', 'n', 't', 'e', 's', 't'}
	testXChainID    = ids.ID{'t', 'e', 's', 't', 'x'}
	testChainID     = ids.ID{'t', 'e', 's', 't', 'c', 'h', 'a', 'i', 'n'}
	TestAvaxAssetID = ids.ID{1, 2, 3}
)

func TestSnowContext() *snow.Context {
	sk, err := bls.NewSigner()
	if err != nil {
		panic(err)
	}
	pk := sk.PublicKey()
	networkID := constants.UnitTestID
	chainID := testCChainID

	aliaser := ids.NewAliaser()
	_ = aliaser.Alias(testCChainID, "C")
	_ = aliaser.Alias(testCChainID, testCChainID.String())
	_ = aliaser.Alias(testXChainID, "X")
	_ = aliaser.Alias(testXChainID, testXChainID.String())

	ctx := &snow.Context{
		NetworkID:      networkID,
		SubnetID:       ids.Empty,
		ChainID:        chainID,
		AVAXAssetID:    TestAvaxAssetID,
		NodeID:         ids.GenerateTestNodeID(),
		SharedMemory:   TestSharedMemory(),
		XChainID:       testXChainID,
		CChainID:       testCChainID,
		PublicKey:      pk,
		WarpSigner:     warp.NewSigner(sk, networkID, chainID),
		Log:            logging.NoLog{},
		BCLookup:       aliaser,
		Metrics:        metrics.NewPrefixGatherer(),
		ChainDataDir:   "",
		ValidatorState: NewTestValidatorState(),
	}

	return ctx
}

func NewTestValidatorState() *validatorstest.State {
	return &validatorstest.State{
		GetCurrentHeightF: func(context.Context) (uint64, error) {
			return 0, nil
		},
		GetSubnetIDF: func(_ context.Context, chainID ids.ID) (ids.ID, error) {
			subnetID, ok := map[ids.ID]ids.ID{
				constants.PlatformChainID: constants.PrimaryNetworkID,
				testXChainID:              constants.PrimaryNetworkID,
				testCChainID:              constants.PrimaryNetworkID,
			}[chainID]
			if !ok {
				return ids.Empty, errors.New("unknown chain")
			}
			return subnetID, nil
		},
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			return map[ids.NodeID]*validators.GetValidatorOutput{}, nil
		},
		GetCurrentValidatorSetF: func(context.Context, ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
			return map[ids.ID]*validators.GetCurrentValidatorOutput{}, 0, nil
		},
	}
}

func TestSharedMemory() atomic.SharedMemory {
	m := atomic.NewMemory(memdb.New())
	return m.NewSharedMemory(testCChainID)
}
