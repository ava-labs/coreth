// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	gconstants "github.com/ava-labs/coreth/constants"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

const xPrecision = uint64(1_000_000_000)

func TestCollectRewardsEVMStateTransfer(t *testing.T) {
	tests := []struct {
		name                   string
		balance                int64
		slotBalance            int64
		amountToDistribute     uint64
		expectedIPReward       uint64
		expectedNewBalance     uint64
		expectedNewSlotBalance uint64
	}{
		{
			name:                   "Happy path: coinbase 10, slot 0 => export 3, incentive 3, coinbase 4, slot 4",
			balance:                10 * int64(units.Avax),
			slotBalance:            0,
			amountToDistribute:     10,
			expectedIPReward:       3 * units.Avax,
			expectedNewBalance:     4 * units.Avax,
			expectedNewSlotBalance: 4 * units.Avax,
		},
		{
			name:                   "Happy path: coinbase 11, slot 0 => export 3, incentive 3, coinbase 5, slot 4",
			balance:                11 * int64(units.Avax),
			slotBalance:            0,
			amountToDistribute:     11,
			expectedIPReward:       3 * units.Avax,
			expectedNewBalance:     5 * units.Avax,
			expectedNewSlotBalance: 4 * units.Avax,
		},
		{
			name:                   "Happy path: bigger numbers, ratio without decimal loss, BH balance == Balance slot",
			balance:                100 * int64(units.Avax),
			slotBalance:            10 * int64(units.Avax),
			amountToDistribute:     90,
			expectedIPReward:       27 * units.Avax,
			expectedNewBalance:     46 * units.Avax, // == 90 - 2 * 27 + 10
			expectedNewSlotBalance: 46 * units.Avax, // == 40% of 90 + 10
		},
		{
			name:                   "Happy path: bigger numbers, ratio with decimal loss, BH balance > Balance slot",
			balance:                90 * int64(units.Avax),
			slotBalance:            6 * int64(units.Avax),
			amountToDistribute:     84,
			expectedIPReward:       25 * units.Avax,
			expectedNewBalance:     40 * units.Avax, // == 84 - 2 * 25 + 6
			expectedNewSlotBalance: 39 * units.Avax, // == 40% of 84 + 6
		},
		{
			name:                   "Simulation block 1: fee increase: 100, coinbase (10)0, slot 0",
			balance:                100 * int64(units.Avax),
			slotBalance:            0 * int64(units.Avax),
			amountToDistribute:     100,
			expectedIPReward:       30 * units.Avax,
			expectedNewBalance:     40 * units.Avax,
			expectedNewSlotBalance: 40 * units.Avax,
		},
		{
			name:                   "Simulation block 2: fee increase: 90, coinbase 130, slot 40",
			balance:                130 * int64(units.Avax),
			slotBalance:            40 * int64(units.Avax),
			amountToDistribute:     90,
			expectedIPReward:       27 * units.Avax,
			expectedNewBalance:     76 * units.Avax,
			expectedNewSlotBalance: 76 * units.Avax,
		},
		{
			name:                   "Simulation block 3: fee increase: 84, coinbase 160, slot 76",
			balance:                160 * int64(units.Avax),
			slotBalance:            76 * int64(units.Avax),
			amountToDistribute:     84,
			expectedIPReward:       25 * units.Avax,
			expectedNewBalance:     110 * units.Avax,
			expectedNewSlotBalance: 109 * units.Avax,
		},
		{
			name:                   "Simulation block 4: fee increase: 85, coinbase 196, slot 76",
			balance:                195 * int64(units.Avax),
			slotBalance:            109 * int64(units.Avax),
			amountToDistribute:     86,
			expectedIPReward:       25 * units.Avax,
			expectedNewBalance:     145 * units.Avax,
			expectedNewSlotBalance: 142 * units.Avax,
		},
		{
			name:                   "Simulation block 5: fee increase: 84, coinbase 196, slot 76",
			balance:                229 * int64(units.Avax),
			slotBalance:            142 * int64(units.Avax),
			amountToDistribute:     87,
			expectedIPReward:       26 * units.Avax,
			expectedNewBalance:     177 * units.Avax,
			expectedNewSlotBalance: 176 * units.Avax,
		},
		{
			name:                   "Simulation block 1-5 in one go: fee increase: 100+90+84+85+84, coinbase 443, slot 0",
			balance:                443 * int64(units.Avax),
			slotBalance:            0 * int64(units.Avax),
			amountToDistribute:     443,
			expectedIPReward:       132 * units.Avax, // -1 to the accumulated rewards (133) from the previous 👆 blocks
			expectedNewBalance:     179 * units.Avax, // which most likely will be aligned, because of +2 in BH balance
			expectedNewSlotBalance: 176 * units.Avax,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, vm, _, _, _ := GenesisVM(t, true, genesisJSONSunrisePhase0, "", "")
			state, err := vm.blockChain.State()
			require.NoError(t, err)

			// Add balance to coinbase address
			state.AddBalance(gconstants.BlackholeAddr, big.NewInt(tt.balance))

			// Add slot balance to coinbase address
			state.SetState(gconstants.BlackholeAddr, BalanceSlot, common.BigToHash(big.NewInt(tt.slotBalance)))

			// Cal the rewards tx
			tx, err := vm.NewCollectRewardsTx(common.Hash{}, tt.amountToDistribute, 1)
			require.NoError(t, err)

			err = tx.EVMStateTransfer(vm.ctx, state)
			require.NoError(t, err)

			// assert incentive balance
			incentiveBalance := state.GetBalance(common.Address(FeeRewardAddressID)).Uint64()
			require.Equal(t, tt.expectedIPReward, incentiveBalance, fmt.Sprintf("expected %d, got (actual) %d", tt.expectedIPReward, incentiveBalance))

			// assert coinbase balance
			newCoinbaseBalance := state.GetBalance(gconstants.BlackholeAddr).Uint64()
			require.Equal(t, tt.expectedNewBalance, newCoinbaseBalance, fmt.Sprintf("expected %d, got (actual) %d", tt.expectedNewBalance, newCoinbaseBalance))

			// assert slot balance
			newSlotBalance := state.GetState(gconstants.BlackholeAddr, BalanceSlot).Big().Uint64()
			require.Equal(t, tt.expectedNewSlotBalance, newSlotBalance, fmt.Sprintf("expected %d, got (actual) %d", tt.expectedNewSlotBalance, newSlotBalance))
		})
	}
}

func TestCollectRewardsSemanticVerify(t *testing.T) {
	amount := uint64(100_000_000_000)
	tests := []struct {
		name                 string
		amountToDistribute   uint64
		txBlockTime          uint64
		balance              uint64
		balanceSlot          uint64
		timestampSlot        uint64
		blockTime            uint64
		minFeeExport         uint64
		minFeeExportInteval  uint64
		expectedTxError      error
		expectedInvalidError error
	}{
		{
			name:               "Too little value 0",
			amountToDistribute: 0,
			expectedTxError:    errors.New("input has no value"),
		},
		{
			name:                 "Time has not passed",
			amountToDistribute:   amount,
			txBlockTime:          1675080000,
			blockTime:            1675081531, // trimmed to full hours
			timestampSlot:        1675082001,
			minFeeExport:         10_000_000_000,
			minFeeExportInteval:  3600, // 1h
			expectedInvalidError: errors.New("time has not passed"),
		},
		{
			name:                "Happy path Tx is valid and in time",
			amountToDistribute:  amount,
			balance:             amount,
			txBlockTime:         1675080000,
			blockTime:           1675081531, // trimmed to full hours
			timestampSlot:       1675080000,
			minFeeExport:        10_000_000_000,
			minFeeExportInteval: 3600, // 1h
		},
		{
			name:                 "Too little to satisfy the distribution limit",
			amountToDistribute:   amount / 100,
			balance:              amount / 100,
			txBlockTime:          1675080000,
			blockTime:            1675081531, // trimmed to full hours
			timestampSlot:        1675080000,
			minFeeExport:         10_000_000_000,
			minFeeExportInteval:  3600, // 1h
			expectedInvalidError: errors.New("export limit not yet reached"),
		},
		{
			name:                 "Calculated reward doesn't match the balance",
			amountToDistribute:   amount + 100,
			balance:              amount,
			txBlockTime:          1675080000,
			blockTime:            1675081531, // trimmed to full hours
			timestampSlot:        1675080000,
			minFeeExport:         10_000_000_000,
			minFeeExportInteval:  3600, // 1h
			expectedInvalidError: errors.New("calculated reward amount mismatch"),
		},
		{
			name:                "Happy path with slot balance",
			amountToDistribute:  amount,
			balance:             amount + amount,
			balanceSlot:         amount,
			txBlockTime:         1675080000,
			blockTime:           1675081531, // trimmed to full hours
			timestampSlot:       1675080000,
			minFeeExport:        10_000_000_000,
			minFeeExportInteval: 3600, // 1h
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			genesisJSON := prepareGenesis(
				tt.balance,
				tt.balanceSlot,
				tt.timestampSlot,
				tt.blockTime,
				0,
				tt.minFeeExport,
				tt.minFeeExportInteval,
			)
			_, vm, _, _, _ := GenesisVM(t, true, genesisJSON, "", "")
			parent := vm.LastAcceptedBlockInternal().(*Block)

			tx, err := vm.NewCollectRewardsTx(parent.ethBlock.Hash(), tt.amountToDistribute, tt.txBlockTime)
			if tt.expectedTxError != nil {
				require.Equal(t, tt.expectedTxError, err)
				return
			}

			require.NoError(t, err)

			err = tx.SemanticVerify(vm, nil, parent, sunriseBaseFee, apricotRulesPhase5)

			if tt.expectedInvalidError != nil {
				require.Equal(t, tt.expectedInvalidError, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestIssueCollectRewardsTxAndBuildBlock(t *testing.T) {
	amount := uint64(100_000_000_000)
	genesisJSON := prepareGenesis(amount, 0, 0, 0, 0, 10_000_000_000, 3600)
	issuer, vm, _, _, _ := GenesisVM(t, true, genesisJSON, "", "")
	parent := vm.LastAcceptedBlockInternal().(*Block)

	tx, err := vm.NewCollectRewardsTx(parent.ethBlock.Hash(), amount, uint64(0))
	require.NoError(t, err)
	if err := vm.issueTx(tx, true /*=local*/); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk, err := vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm.SetPreference(context.Background(), blk.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	state, err := vm.blockChain.State()
	require.NoError(t, err)

	balance := state.GetBalance(gconstants.BlackholeAddr)
	balanceAvax := balance.Div(balance, x2cRate).Uint64()
	expectedBalance := amount - uint64(2*0.3*float64(amount))
	require.Equal(t, expectedBalance, balanceAvax, "expected %d but got %d", amount, balanceAvax)

	TimestampSlot := state.GetState(gconstants.BlackholeAddr, TimestampSlot).Big().Uint64()
	expectedNextTimestampSlot := feeRewardExportMinTimeInterval(vm)
	require.Equal(t, expectedNextTimestampSlot, TimestampSlot, "expected %d but got %d", expectedNextTimestampSlot, TimestampSlot)
}

func TestCollectRewardsAtomicOps(t *testing.T) {
	tests := []struct {
		name               string
		amountToDistribute uint64
		expectedReward     uint64
		expectedError      error
	}{
		{
			name:               "Happy path 10",
			amountToDistribute: 10,
			expectedReward:     3,
		},
		{
			name:               "Happy path 84",
			amountToDistribute: 84,
			expectedReward:     25,
		},
		{
			name:               "Happy path 85",
			amountToDistribute: 85,
			expectedReward:     25,
		},
		{
			name:               "Happy path 86",
			amountToDistribute: 87,
			expectedReward:     26,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, vm, _, _, _ := GenesisVM(t, true, genesisJSONSunrisePhase0, "", "")

			// Cal the rewards tx
			tx, err := vm.NewCollectRewardsTx(common.Hash{}, tt.amountToDistribute, 1)
			require.NoError(t, err)

			destChainID, reqs, err := tx.AtomicOps()
			require.NoError(t, err)
			require.NotNil(t, reqs)
			require.Equal(t, constants.PlatformChainID, destChainID)

			require.Len(t, reqs.PutRequests, 1)
			require.Len(t, reqs.RemoveRequests, 0)
			utxoBytes := reqs.PutRequests[0].Value

			utxo := &avax.TimedUTXO{}
			_, err = vm.codec.Unmarshal(utxoBytes, utxo)
			require.NoError(t, err)
			require.Equal(t, vm.ctx.AVAXAssetID, utxo.AssetID())

			// assert reward value
			out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
			require.True(t, ok)
			require.Equal(t, tt.expectedReward, out.Amt, fmt.Sprintf("expected %d, got (actual) %d", tt.expectedReward, out.Amt))
		})
	}
}

func TestVMStateCanBeDefinedInGenesis(t *testing.T) {
	tests := []struct {
		name                string
		balance             uint64
		balanceSlot         uint64
		timestampSlot       uint64
		blockTime           uint64
		nonce               uint64
		minFeeExport        uint64
		minFeeExportInteval uint64
	}{
		{
			name:                "Zeros everywhere",
			balance:             0 * xPrecision,
			balanceSlot:         0 * xPrecision,
			timestampSlot:       0,
			blockTime:           0,
			nonce:               0,
			minFeeExport:        0,
			minFeeExportInteval: 0,
		},
		{
			name:                "Real looking numbers",
			balance:             10_000 * xPrecision,
			balanceSlot:         0,
			timestampSlot:       1674819410,
			blockTime:           1674444431,
			nonce:               0,
			minFeeExport:        10_000_000_000,
			minFeeExportInteval: 3600, // 1h
		},
		{
			name:                "small numbers",
			balance:             1,
			balanceSlot:         2,
			timestampSlot:       3,
			blockTime:           4,
			nonce:               0,
			minFeeExport:        5,
			minFeeExportInteval: 6,
		},
		{
			name:                "BIG numbers",
			balance:             math.MaxUint64,
			balanceSlot:         math.MaxUint64,
			timestampSlot:       math.MaxUint64,
			blockTime:           math.MaxUint64,
			nonce:               math.MaxUint64,
			minFeeExport:        math.MaxUint64,
			minFeeExportInteval: math.MaxUint64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			genesisJSON := prepareGenesis(
				tt.balance,
				tt.balanceSlot,
				tt.timestampSlot,
				tt.blockTime,
				tt.nonce,
				tt.minFeeExport,
				tt.minFeeExportInteval,
			)
			_, vm, _, _, _ := GenesisVM(t, true, genesisJSON, "", "")
			state, err := vm.blockChain.State()
			require.NoError(t, err)

			nonce := state.GetNonce(gconstants.BlackholeAddr)
			balance := state.GetBalance(gconstants.BlackholeAddr)
			balanceAvax := new(big.Int).Div(balance, x2cRate).Uint64()
			balanceSlot := state.GetState(gconstants.BlackholeAddr, BalanceSlot).Big()
			bSlotAvax := new(big.Int).Div(balanceSlot, x2cRate).Uint64()
			tsSlot := state.GetState(gconstants.BlackholeAddr, TimestampSlot).Big().Uint64()

			require.Equal(t, tt.nonce, nonce)
			require.Equal(t, tt.balance, balanceAvax)
			require.Equal(t, tt.balanceSlot, bSlotAvax)
			require.Equal(t, tt.timestampSlot, tsSlot)

			blk := vm.LastAcceptedBlockInternal().(*Block)
			require.Equal(t, tt.blockTime, blk.ethBlock.Time())
		})
	}
}

func prepareGenesis(
	balance uint64,
	balanceSlot uint64,
	timestampSlot uint64,
	blockTime uint64,
	nonce uint64,
	minFeeExport uint64,
	minTimeInterval uint64,
) string {
	return fmt.Sprintf(
		"{\"config\":{\"chainId\":43111,\"homesteadBlock\":0,\"daoForkBlock\":0,\"daoForkSupport\":true,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0,\"apricotPhase1BlockTimestamp\":0,\"apricotPhase2BlockTimestamp\":0,\"apricotPhase3BlockTimestamp\":0,\"apricotPhase4BlockTimestamp\":0,\"apricotPhase5BlockTimestamp\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x%x\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0100000000000000000000000000000000000000\":{\"code\":\"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033\",\"nonce\":\"0x%x\",\"balance\":\"0x%s\",\"storage\":{\"0x0100000000000000000000000000000000000000000000000000000000000000\":\"%s\", \"0x0200000000000000000000000000000000000000000000000000000000000000\":\"%s\"}}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"feeRewardExportMinAmount\":\"0x%x\",\"feeRewardExportMinTimeInterval\":\"0x%x\"}",
		blockTime,
		nonce,
		/*balance*/ new(big.Int).Mul(x2cRate, new(big.Int).SetUint64(balance)).Text(16),
		/*balanceSlot*/ common.BigToHash(new(big.Int).Mul(x2cRate, new(big.Int).SetUint64(balanceSlot))).Hex(),
		/*timestampSlot*/ common.BigToHash(new(big.Int).SetUint64(timestampSlot)).Hex(),
		minFeeExport,
		minTimeInterval,
	)
}
