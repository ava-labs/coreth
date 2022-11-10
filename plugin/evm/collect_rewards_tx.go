// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"math/big"

	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/params"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

var (
	_ UnsignedAtomicTx = &UnsignedCollectRewardsTx{}
)

// UnsignedCollectRewardsTx is an internal rewards collection transaction
type UnsignedCollectRewardsTx struct {
	avax.Metadata
	// ID of the network on which this tx was issued
	NetworkID uint32 `serialize:"true" json:"networkID"`
	// ID of this blockchain.
	BlockchainID ids.ID `serialize:"true" json:"blockchainID"`
	// Which chain to send the funds to
	DestinationChain ids.ID `serialize:"true" json:"destinationChain"`
	// Outputs that are exported to the chain
	ExportedOutputs []*avax.TransferableOutput `serialize:"true" json:"exportedOutputs"`

	BlockId        ids.ID `serialize:"true" json:"blockId"`
	BlockTimestamp uint64 `serialize:"true" json:"blockTimestamp"`

	RewardCalculation RewardCalculationResult `serialize:"true" json:"rewardCalculation"`

	Coinbase                   common.Address `serialize:"true" json:"coinbase"`
	ValidatorRewardAddress     common.Address `serialize:"true" json:"validatorRewardAddress"`
	IncentivePoolRewardAddress common.Address `serialize:"true" json:"incentivePoolRewardAddress"`
}

// InputUTXOs returns a set of all the hash(address:nonce) exporting funds.
func (tx *UnsignedCollectRewardsTx) InputUTXOs() ids.Set {
	// Not sure if it will be needed - mock
	return ids.NewSet(0)
}

// Verify this transaction is well-formed
func (tx *UnsignedCollectRewardsTx) Verify(
	ctx *snow.Context,
	rules params.Rules,
) error {
	log.Info("Verify...")
	switch {
	case tx == nil:
		return errNilTx
	case tx.NetworkID != ctx.NetworkID:
		return errWrongNetworkID
	case ctx.ChainID != tx.BlockchainID:
		return errWrongBlockchainID
	}

	calculation := tx.RewardCalculation.Calculation()
	log.Info("Sanity check of RC", "txID", tx.ID(), "calc", calculation)
	if err := tx.RewardCalculation.Verify(); err != nil {
		log.Info("RewardCollectionTx verification failed", "txID", tx.ID(), "error", err)
		return err
	}

	// Make sure that the tx has a valid peer chain ID
	if err := verify.SameSubnet(ctx, tx.DestinationChain); err != nil {
		return errWrongChainID
	}
	if tx.DestinationChain != constants.PlatformChainID {
		return errWrongChainID
	}

	log.Info("Verify completed")
	return nil
}

func (tx *UnsignedCollectRewardsTx) GasUsed(bool) (uint64, error) {
	return 0, nil
}

// Amount of [assetID] burned by this transaction
func (tx *UnsignedCollectRewardsTx) Burned(assetID ids.ID) (uint64, error) {
	// Let me lie here
	return 0, nil
}

// SemanticVerify this transaction is valid.
func (tx *UnsignedCollectRewardsTx) SemanticVerify(
	vm *VM,
	_stx *Tx,
	b *Block,
	_baseFee *big.Int,
	rules params.Rules,
) error {
	log.Info("SemanticVerify...")
	if err := tx.Verify(vm.ctx, rules); err != nil {
		return err
	}

	state, err := vm.blockChain.State()
	if err != nil {
		log.Info("Cannot access current EVM state", "error", err)
		return fmt.Errorf("cannot access current EVM state: %w", err)
	}

	calculation := tx.RewardCalculation.Calculation()

	log.Info("SemanticVerify processing tx", "txID", tx.ID().String())
	currValidatorRewardPayedOut := state.GetState(tx.Coinbase, Slot1).Big()
	if calculation.PrevValidatorRewards.Cmp(currValidatorRewardPayedOut) != 0 {
		log.Info("validator rewards mismatch", "prevFeesBurned", calculation.PrevFeesBurned, "currValidatorRewardPayedOut", currValidatorRewardPayedOut)
		return fmt.Errorf("validator rewards mismatch")
	}

	ipRewardsPayedOut := state.GetState(tx.Coinbase, Slot2).Big()
	if calculation.PrevIncentivePoolRewards.Cmp(ipRewardsPayedOut) != 0 {
		log.Info("Incentive pool rewards mismatch", "prevIncentivePoolRewards", calculation.PrevIncentivePoolRewards, "ipRewardsPayedOut", ipRewardsPayedOut)
		return fmt.Errorf("incentive pool balance mismatch")
	}

	// TODO: Question should we confirm the calculation (by re-calculate) at this point?
	// YES: We need all information to be able to fully verify the tx, inc. recalculation

	log.Info("SemanticVerify completed")
	return nil
}

// AtomicOps returns the atomic operations for this transaction.
func (tx *UnsignedCollectRewardsTx) AtomicOps() (ids.ID, *atomic.Requests, error) {
	log.Info("AtomicOps...")
	txID := tx.ID()

	elems := make([]*atomic.Element, 1)
	out := tx.ExportedOutputs[0]
	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        txID,
			OutputIndex: uint32(0),
		},
		Asset: avax.Asset{ID: out.AssetID()},
		Out:   out.Out,
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

	elems[0] = elem

	log.Info("AtomicOps completed")
	return tx.DestinationChain, &atomic.Requests{PutRequests: elems}, nil
}

func (vm *VM) NewCollectRewardsTx(
	calculation RewardCalculationResult,
	blockId ids.ID,
	blockTimestamp uint64,
	coinbase common.Address,
	validatorRewardAddress common.Address,
	incentivePoolRewardAddress common.Address,

) (*Tx, error) {
	// Create the transaction
	utx := &UnsignedCollectRewardsTx{
		NetworkID:                  vm.ctx.NetworkID,
		BlockchainID:               vm.ctx.ChainID,
		DestinationChain:           constants.PlatformChainID,
		BlockId:                    blockId,
		BlockTimestamp:             blockTimestamp,
		RewardCalculation:          calculation,
		Coinbase:                   coinbase,
		ValidatorRewardAddress:     validatorRewardAddress,
		IncentivePoolRewardAddress: incentivePoolRewardAddress,
	}

	// TODO: Make validatorRewardAddress ShortID typed
	pChainAddress, _ := ids.ToShortID(validatorRewardAddress.Bytes())
	utx.ExportedOutputs = []*avax.TransferableOutput{{
		Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: calculation.ValidatorRewardToExport,
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  0,
				Threshold: 1,
				Addrs:     []ids.ShortID{pChainAddress},
			},
		},
	}}

	tx := &Tx{UnsignedAtomicTx: utx}
	if err := tx.Sign(vm.codec, nil); err != nil {
		return nil, err
	}

	log.Info("New CollectionRewardTx created.",
		"ValidatorRewardExport", calculation.ValidatorRewardToExport,
		"ValidatorReward", calculation.ValidatorRewardAmount,
		"IPReward", calculation.IncentivePoolRewardAmount,
		"Curr Coinbase balance", calculation.PrevValidatorRewards,
		"Curr IP balance", calculation.PrevIncentivePoolRewards,
		"Fees already burned", calculation.PrevFeesBurned,
	)

	return tx, utx.Verify(vm.ctx, vm.currentRules())
}

// EVMStateTransfer executes the state update from the atomic export transaction
func (tx *UnsignedCollectRewardsTx) EVMStateTransfer(ctx *snow.Context, state *state.StateDB) error {
	log.Info("EVMStateTransfer...", "txID", tx.ID().String())
	calculation := tx.RewardCalculation.Calculation()
	state.SubBalance(tx.Coinbase, calculation.CoinbaseAmountToSub)
	validatorRewards := new(big.Int).Add(calculation.PrevValidatorRewards, calculation.ValidatorRewardAmount)

	ipRewards := new(big.Int).Add(calculation.PrevIncentivePoolRewards, calculation.IncentivePoolRewardAmount)
	state.AddBalance(tx.IncentivePoolRewardAddress, calculation.IncentivePoolRewardAmount)

	state.SetState(tx.Coinbase, Slot0, common.BigToHash(new(big.Int).SetUint64(tx.BlockTimestamp)))
	state.SetState(tx.Coinbase, Slot1, common.BigToHash(validatorRewards))
	state.SetState(tx.Coinbase, Slot2, common.BigToHash(ipRewards))

	log.Info("EVMStateTransfer completed")
	return nil
}
