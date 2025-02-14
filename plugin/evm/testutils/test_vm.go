package testutils

import (
	"context"
	"testing"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	commoneng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/stretchr/testify/require"
)

func SetupVM(
	t *testing.T,
	finishBootstrapping bool,
	genesisJSON string,
	configJSON string,
	upgradeJSON string,
	vm commoneng.VM,
) (
	chan commoneng.Message,
	database.Database,
	*avalancheatomic.Memory,
	*enginetest.Sender,
	*snow.Context,
) {
	ctx, dbManager, genesisBytes, issuer, m := SetupGenesis(t, genesisJSON)
	appSender := &enginetest.Sender{T: t}
	appSender.CantSendAppGossip = true
	appSender.SendAppGossipF = func(context.Context, commoneng.SendConfig, []byte) error { return nil }
	err := vm.Initialize(
		context.Background(),
		ctx,
		dbManager,
		genesisBytes,
		[]byte(upgradeJSON),
		[]byte(configJSON),
		issuer,
		[]*commoneng.Fx{},
		appSender,
	)
	require.NoError(t, err, "error initializing GenesisVM")

	if finishBootstrapping {
		require.NoError(t, vm.SetState(context.Background(), snow.Bootstrapping))
		require.NoError(t, vm.SetState(context.Background(), snow.NormalOp))
	}

	return issuer, dbManager, m, appSender, ctx
}
