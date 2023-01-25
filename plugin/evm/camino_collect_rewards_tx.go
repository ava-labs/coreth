// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	gconstants "github.com/ava-labs/coreth/constants"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

const (
	FeeRewardMinAmountToExport = uint64(200_000)
	FeeRewardAddressStr        = "0x010000000000000000000000000000000000000c"
)

var (
	_ UnsignedAtomicTx       = &UnsignedCollectRewardsTx{}
	_ secp256k1fx.UnsignedTx = &UnsignedCollectRewardsTx{}

	FeeRewardAddress      = common.HexToAddress(FeeRewardAddressStr)
	FeeRewardAddressID, _ = ids.ToShortID(FeeRewardAddress.Bytes())

	BalanceSlot   = common.Hash{0x01}
	TimestampSlot = common.Hash{0x02}

	TimeInterval            = uint64(3_600)
	ExportRewardRate        = new(big.Int).SetUint64(300_000)
	IncentivePoolRewardRate = new(big.Int).SetUint64(300_000)
	RateDenominator         = new(big.Int).SetUint64(1_000_000)

	errWrongInputCount  = errors.New("wrong input count")
	errWrongExportCount = errors.New("wrong ExportedOuts count")
	errExportLimit      = errors.New("export limit not yet reached")
	errTimeNotPassed    = errors.New("time has not passed")
)

type UnsignedCollectRewardsTx struct {
	UnsignedExportTx `serialize:"true"`
	BlockHash        common.Hash `serialize:"true"`
	BlockTime        uint64      `serialize:"true"`
}

func (ucx *UnsignedCollectRewardsTx) GasUsed(fixedFee bool) (uint64, error) {
	return 0, nil
}

// SemanticVerify this transaction is valid.
func (ucx *UnsignedCollectRewardsTx) SemanticVerify(
	vm *VM,
	stx *Tx,
	block *Block,
	baseFee *big.Int,
	rules params.Rules,
) error {
	if err := ucx.Verify(vm.ctx, rules); err != nil {
		return err
	}

	// We expect exactly 1 in
	if len(ucx.Ins) != 1 {
		return errWrongInputCount
	}

	// We expect exactly 1 out
	if len(ucx.ExportedOutputs) != 1 {
		return errWrongExportCount
	}
	output, ok := ucx.ExportedOutputs[0].Out.(*secp256k1fx.TransferOutput)
	if !ok {
		return fmt.Errorf("wrong output type")
	}

	if ucx.ExportedOutputs[0].Asset.AssetID() != vm.ctx.AVAXAssetID {
		return errAssetIDMismatch
	}

	// Verify sender of the rewards
	if ucx.Ins[0].Address != gconstants.BlackholeAddr {
		return fmt.Errorf("invalid input address")
	}

	// Verify receiver of the outputs
	if len(output.OutputOwners.Addrs) != 1 || output.OutputOwners.Addrs[0] != FeeRewardAddressID {
		return fmt.Errorf("invalid output owner")
	}

	if ucx.ExportedOutputs[0].Out.Amount() != ucx.Ins[0].Amount {
		return fmt.Errorf("amount mismatch")
	}

	// Get block header
	head := vm.blockChain.GetHeaderByHash(ucx.BlockHash)
	if head == nil {
		return fmt.Errorf("cannot get header")
	}

	blockTime := head.Time - (head.Time % TimeInterval)
	if blockTime != ucx.BlockTime {
		return fmt.Errorf("invalid block time")
	}

	// Verify the block this tx was build from
	state, err := vm.blockChain.StateAt(head.Root)
	if err != nil {
		return err
	}

	triggerTime := state.GetState(gconstants.BlackholeAddr, TimestampSlot).Big().Uint64()
	if blockTime < triggerTime {
		return errTimeNotPassed
	}

	balanceAvax, err := getReward(state)
	if err != nil {
		return err
	}
	if balanceAvax != ucx.Ins[0].Amount {
		return fmt.Errorf("amount mismatch")
	}

	// Verify that parent would not succeed
	head = vm.blockChain.GetHeaderByHash(head.ParentHash)
	if head == nil {
		return nil
	}

	state, err = vm.blockChain.StateAt(head.Root)
	if err != nil {
		return err
	}

	// If parent trigger time != current trigger time, it was executed before
	if triggerTime != state.GetState(gconstants.BlackholeAddr, TimestampSlot).Big().Uint64() {
		return nil
	}

	// Check if parent is before trigger time
	blockTime = head.Time - (head.Time % TimeInterval)
	if blockTime < triggerTime {
		return nil
	}

	// Check if the reward export limit is not reached
	_, err = getReward(state)
	if err == nil {
		return fmt.Errorf("past block would execute")
	}

	return nil
}

// AtomicOps returns the atomic operations for this transaction.
func (ucx *UnsignedCollectRewardsTx) AtomicOps() (ids.ID, *atomic.Requests, error) {
	txID := ucx.ID()

	// Check again
	if len(ucx.ExportedOutputs) != 1 {
		return ids.Empty, nil, errWrongExportCount
	}

	exportOut, ok := ucx.ExportedOutputs[0].Out.(*secp256k1fx.TransferOutput)
	if !ok {
		return ids.Empty, nil, errNoExportOutputs
	}

	// Only export a part of new amount burned
	newOut := &secp256k1fx.TransferOutput{
		Amt:          calculateRate(exportOut.Amt, ExportRewardRate),
		OutputOwners: exportOut.OutputOwners,
	}

	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        txID,
			OutputIndex: 0,
		},
		Asset: avax.Asset{ID: ucx.ExportedOutputs[0].AssetID()},
		Out:   newOut,
	}

	utxoBytes, err := Codec.Marshal(codecVersion, utxo)
	if err != nil {
		return ids.ID{}, nil, err
	}
	utxoID := utxo.InputID()
	elem := &atomic.Element{
		Key:   utxoID[:],
		Value: utxoBytes,
	}
	if out, ok := utxo.Out.(avax.Addressable); ok {
		elem.Traits = out.Addresses()
	}

	return ucx.DestinationChain, &atomic.Requests{PutRequests: []*atomic.Element{elem}}, nil
}

func (vm *VM) NewCollectRewardsTx(
	hash common.Hash,
	amount uint64,
	blockTime uint64,
) (*Tx, error) {
	nonce, err := vm.GetCurrentNonce(gconstants.BlackholeAddr)
	if err != nil {
		return nil, err
	}

	// Create the transaction
	utx := &UnsignedCollectRewardsTx{
		UnsignedExportTx: UnsignedExportTx{
			NetworkID:        vm.ctx.NetworkID,
			BlockchainID:     vm.ctx.ChainID,
			DestinationChain: constants.PlatformChainID,
			Ins: []EVMInput{{
				Address: gconstants.BlackholeAddr,
				Amount:  amount,
				AssetID: vm.ctx.AVAXAssetID,
				Nonce:   nonce,
			}},
			ExportedOutputs: []*avax.TransferableOutput{{
				Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: amount,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{FeeRewardAddressID},
					},
				},
			}},
		},
		BlockHash: hash,
		BlockTime: blockTime,
	}

	tx := &Tx{UnsignedAtomicTx: utx}
	if err := tx.Sign(vm.codec, nil); err != nil {
		return nil, err
	}

	return tx, utx.Verify(vm.ctx, vm.currentRules())
}

func (vm *VM) TriggerRewardsTx(block *Block) {
	blockTimeBN := block.ethBlock.Timestamp()
	// reward distribution only for sunrise configurations
	if !vm.chainConfig.IsSunrisePhase0(blockTimeBN) {
		return
	}
	blockTime := blockTimeBN.Uint64()
	blockTime = blockTime - (blockTime % TimeInterval)

	state, err := vm.blockChain.StateAt(block.ethBlock.Root())
	if err != nil {
		return
	}

	triggerTime := state.GetState(gconstants.BlackholeAddr, TimestampSlot).Big().Uint64()
	if blockTime < triggerTime {
		return
	}

	amount, err := getReward(state)
	if err != nil {
		return
	}

	tx, err := vm.NewCollectRewardsTx(block.ethBlock.Hash(), amount, blockTime)
	if err != nil {
		return
	}
	log.Info("Issue CollectRewardsTx", "amount",
		tx.UnsignedAtomicTx.(*UnsignedCollectRewardsTx).Ins[0].Amount)
	vm.issueTx(tx, true /*=local*/)
}

// EVMStateTransfer executes the state update from the atomic export transaction
func (ucx *UnsignedCollectRewardsTx) EVMStateTransfer(ctx *snow.Context, state *state.StateDB) error {
	// Check again
	if len(ucx.Ins) != 1 {
		return errWrongInputCount
	}

	if len(ucx.ExportedOutputs) != 1 {
		return errWrongExportCount
	}

	from := ucx.Ins[0]

	// Calculate partitial amounts
	amountExport := calculateRate(from.Amount, ExportRewardRate)
	amountIncentive := calculateRate(from.Amount, IncentivePoolRewardRate)

	log.Debug("reward", "amount", from.Amount, "export", amountExport, "incentive", amountIncentive, "assetID", "CAM")
	// We multiply the input amount by x2cRate to convert AVAX back to the appropriate
	// denomination before export.
	amount := new(big.Int).Mul(
		new(big.Int).SetUint64(amountExport+amountIncentive), x2cRate,
	)

	balance := state.GetBalance(from.Address)
	if balance.Cmp(amount) < 0 {
		return errInsufficientFunds
	}
	state.SubBalance(from.Address, amount)
	// Update current balance for the following calculation
	balance.Sub(balance, amount)

	// Step up the balance we already used for fees

	// Evaluate amount to burn from components because
	// of integer division / avax decimals inaccuracies:
	//
	// ((amountExport+amountIncentive) * denom)
	//  ---------------------------------------  - (amountExport+amountIncentive)
	//        (rateExport+rateIncentive)
	//
	amountToBurn := new(big.Int).Div(
		new(big.Int).Mul(
			new(big.Int).SetUint64(amountExport+amountIncentive),
			RateDenominator,
		),
		new(big.Int).Add(ExportRewardRate, IncentivePoolRewardRate),
	).Uint64() - (amountExport + amountIncentive)
	amountToBurnEvm := new(big.Int).Mul(
		new(big.Int).SetUint64(amountToBurn), x2cRate,
	)

	// balance - lastPayoutBalance is the amount we can max distribute
	lastPayoutBalance := state.GetState(ucx.Ins[0].Address, BalanceSlot).Big()
	// This can happen if there was a payout before this TX executes
	if lastPayoutBalance.Add(lastPayoutBalance, amountToBurnEvm).Cmp(balance) > 0 {
		return fmt.Errorf("paid out fees exceed balance")
	}
	state.SetState(ucx.Ins[0].Address, BalanceSlot, common.BigToHash(lastPayoutBalance))

	// Add balances to incentive pool smart contract
	amountIncentiveEVM := new(big.Int).Mul(
		new(big.Int).SetUint64(amountIncentive), x2cRate,
	)
	state.AddBalance(common.Address(FeeRewardAddressID), amountIncentiveEVM)

	// Step up timestamp for the next iteration
	nextBig := new(big.Int).SetUint64(ucx.BlockTime + TimeInterval)
	state.SetState(from.Address, TimestampSlot, common.BigToHash(nextBig))

	if state.GetNonce(from.Address) != from.Nonce {
		return errInvalidNonce
	}
	state.SetNonce(from.Address, from.Nonce+1)

	return nil
}

func calculateRate(amt uint64, rate *big.Int) uint64 {
	bn := new(big.Int).SetUint64(amt)
	bn.Mul(bn, rate)
	bn.Div(bn, RateDenominator)
	return bn.Uint64()
}

func getReward(state *state.StateDB) (uint64, error) {
	// balance - lastPayoutBalance is the amount we can max distribute
	balance := state.GetBalance(gconstants.BlackholeAddr)
	balance.Sub(balance, state.GetState(gconstants.BlackholeAddr, BalanceSlot).Big())
	balanceAvax := balance.Div(balance, x2cRate).Uint64()

	if calculateRate(balanceAvax, ExportRewardRate) < FeeRewardMinAmountToExport {
		return 0, errExportLimit
	}

	return balanceAvax, nil
}
