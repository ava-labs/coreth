// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"math"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/precompile/contract"
	"github.com/ava-labs/coreth/precompile/precompiletest"

	agoUtils "github.com/ava-labs/avalanchego/utils"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

func getBlockchainIDTests(tb testing.TB) []precompiletest.PrecompileTest {
	callerAddr := common.HexToAddress("0x0123")

	defaultSnowCtx := snowtest.Context(tb, snowtest.CChainID)
	blockchainID := defaultSnowCtx.ChainID

	return []precompiletest.PrecompileTest{
		{
			Name:   "getBlockchainID_success",
			Caller: callerAddr,
			InputFn: func(tb testing.TB) []byte {
				input, err := PackGetBlockchainID()
				require.NoError(tb, err)
				return input
			},
			SuppliedGas: GetBlockchainIDGasCost,
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				expectedOutput, err := PackGetBlockchainIDOutput(common.Hash(blockchainID))
				require.NoError(tb, err)
				return expectedOutput
			}(),
		},
		{
			Name:   "getBlockchainID_readOnly",
			Caller: callerAddr,
			InputFn: func(tb testing.TB) []byte {
				input, err := PackGetBlockchainID()
				require.NoError(tb, err)
				return input
			},
			SuppliedGas: GetBlockchainIDGasCost,
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				expectedOutput, err := PackGetBlockchainIDOutput(common.Hash(blockchainID))
				require.NoError(tb, err)
				return expectedOutput
			}(),
		},
		{
			Name:   "getBlockchainID_insufficient_gas",
			Caller: callerAddr,
			InputFn: func(tb testing.TB) []byte {
				input, err := PackGetBlockchainID()
				require.NoError(tb, err)
				return input
			},
			SuppliedGas: GetBlockchainIDGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
	}
}

func TestGetBlockchainID(t *testing.T) {
	tests := getBlockchainIDTests(t)
	precompiletest.RunPrecompileTests(t, Module, tests)
}

func BenchmarkGetBlockchainID(b *testing.B) {
	tests := getBlockchainIDTests(b)
	precompiletest.RunPrecompileBenchmarks(b, Module, tests)
}

func sendWarpMessageTests(tb testing.TB) []precompiletest.PrecompileTest {
	callerAddr := common.HexToAddress("0x0123")

	defaultSnowCtx := snowtest.Context(tb, snowtest.CChainID)
	blockchainID := defaultSnowCtx.ChainID
	sendWarpMessagePayload := agoUtils.RandomBytes(100)

	sendWarpMessageInput, err := PackSendWarpMessage(sendWarpMessagePayload)
	require.NoError(tb, err)
	sendWarpMessageAddressedPayload, err := payload.NewAddressedCall(
		callerAddr.Bytes(),
		sendWarpMessagePayload,
	)
	require.NoError(tb, err)
	unsignedWarpMessage, err := avalancheWarp.NewUnsignedMessage(
		defaultSnowCtx.NetworkID,
		blockchainID,
		sendWarpMessageAddressedPayload.Bytes(),
	)
	require.NoError(tb, err)

	return []precompiletest.PrecompileTest{
		{
			Name:        "send_warp_message_readOnly",
			Caller:      callerAddr,
			InputFn:     func(testing.TB) []byte { return sendWarpMessageInput },
			SuppliedGas: SendWarpMessageGasCost + uint64(len(sendWarpMessageInput[4:])*int(SendWarpMessageGasCostPerByte)),
			ReadOnly:    true,
			ExpectedErr: vm.ErrWriteProtection.Error(),
		},
		{
			Name:        "send_warp_message_insufficient_gas_for_first_step",
			Caller:      callerAddr,
			InputFn:     func(testing.TB) []byte { return sendWarpMessageInput },
			SuppliedGas: SendWarpMessageGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		{
			Name:        "send_warp_message_insufficient_gas_for_payload_bytes",
			Caller:      callerAddr,
			InputFn:     func(testing.TB) []byte { return sendWarpMessageInput },
			SuppliedGas: SendWarpMessageGasCost + uint64(len(sendWarpMessageInput[4:])*int(SendWarpMessageGasCostPerByte)) - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		{
			Name:   "send_warp_message_invalid_input",
			Caller: callerAddr,
			InputFn: func(testing.TB) []byte {
				return sendWarpMessageInput[:4] // Include only the function selector, so that the input is invalid
			},
			SuppliedGas: SendWarpMessageGasCost,
			ReadOnly:    false,
			ExpectedErr: errInvalidSendInput.Error(),
		},
		{
			Name:        "send_warp_message_success",
			Caller:      callerAddr,
			InputFn:     func(testing.TB) []byte { return sendWarpMessageInput },
			SuppliedGas: SendWarpMessageGasCost + uint64(len(sendWarpMessageInput[4:])*int(SendWarpMessageGasCostPerByte)),
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				bytes, err := PackSendWarpMessageOutput(common.Hash(unsignedWarpMessage.ID()))
				require.NoError(tb, err)
				return bytes
			}(),
			AfterHook: func(t testing.TB, state contract.StateDB) {
				var logsTopics [][]common.Hash
				var logsData [][]byte
				for _, log := range state.Logs() {
					logsTopics = append(logsTopics, log.Topics)
					logsData = append(logsData, common.CopyBytes(log.Data))
				}
				require.Len(t, logsTopics, 1)
				topics := logsTopics[0]
				require.Len(t, topics, 3)
				require.Equal(t, topics[0], WarpABI.Events["SendWarpMessage"].ID)
				require.Equal(t, topics[1], common.BytesToHash(callerAddr[:]))
				require.Equal(t, topics[2], common.Hash(unsignedWarpMessage.ID()))

				require.Len(t, logsData, 1)
				logData := logsData[0]
				unsignedWarpMsg, err := UnpackSendWarpEventDataToMessage(logData)
				require.NoError(t, err)
				addressedPayload, err := payload.ParseAddressedCall(unsignedWarpMsg.Payload)
				require.NoError(t, err)

				require.Equal(t, common.BytesToAddress(addressedPayload.SourceAddress), callerAddr)
				require.Equal(t, unsignedWarpMsg.SourceChainID, blockchainID)
				require.Equal(t, addressedPayload.Payload, sendWarpMessagePayload)
			},
		},
	}
}

func TestSendWarpMessage(t *testing.T) {
	tests := sendWarpMessageTests(t)
	precompiletest.RunPrecompileTests(t, Module, tests)
}

func BenchmarkSendWarpMessage(b *testing.B) {
	tests := sendWarpMessageTests(b)
	precompiletest.RunPrecompileBenchmarks(b, Module, tests)
}

func getVerifiedWarpMessageTests(tb testing.TB) []precompiletest.PrecompileTest {
	networkID := uint32(54321)
	callerAddr := common.HexToAddress("0x0123")
	sourceAddress := common.HexToAddress("0x456789")
	sourceChainID := ids.GenerateTestID()
	packagedPayloadBytes := []byte("mcsorley")
	addressedPayload, err := payload.NewAddressedCall(
		sourceAddress.Bytes(),
		packagedPayloadBytes,
	)
	require.NoError(tb, err)
	unsignedWarpMsg, err := avalancheWarp.NewUnsignedMessage(networkID, sourceChainID, addressedPayload.Bytes())
	require.NoError(tb, err)
	warpMessage, err := avalancheWarp.NewMessage(unsignedWarpMsg, &avalancheWarp.BitSetSignature{}) // Create message with empty signature for testing
	require.NoError(tb, err)
	warpMessagePredicate := predicate.New(warpMessage.Bytes())
	getVerifiedWarpMsg, err := PackGetVerifiedWarpMessage(0)
	require.NoError(tb, err)

	// Invalid warp message predicate
	invalidWarpMsgPredicate := predicate.New([]byte{1, 2, 3})

	// Invalid addressed payload predicate and chunk length
	invalidAddrUnsigned, err := avalancheWarp.NewUnsignedMessage(networkID, sourceChainID, []byte{1, 2, 3})
	require.NoError(tb, err)
	invalidAddrWarpMsg, err := avalancheWarp.NewMessage(invalidAddrUnsigned, &avalancheWarp.BitSetSignature{})
	require.NoError(tb, err)
	invalidAddressedPredicate := predicate.New(invalidAddrWarpMsg.Bytes())

	// Invalid predicate packing by corrupting a valid predicate
	invalidPackedPredicate := predicate.Predicate{{}}

	noFailures := set.NewBits()
	require.Empty(tb, noFailures.Bytes())

	return []precompiletest.PrecompileTest{
		{
			Name:       "get_message_success",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{
					Message: WarpMessage{
						SourceChainID:       common.Hash(sourceChainID),
						OriginSenderAddress: sourceAddress,
						Payload:             packagedPayloadBytes,
					},
					Valid: true,
				})
				require.NoError(tb, err)
				return res
			}(),
		},
		{
			Name:   "get_message_out_of_bounds_non_zero_index",
			Caller: callerAddr,
			InputFn: func(tb testing.TB) []byte {
				input, err := PackGetVerifiedWarpMessage(1)
				require.NoError(tb, err)
				return input
			},
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{Valid: false})
				require.NoError(tb, err)
				return res
			}(),
		},
		{
			Name:   "get_message_success_non_zero_index",
			Caller: callerAddr,
			InputFn: func(tb testing.TB) []byte {
				input, err := PackGetVerifiedWarpMessage(1)
				require.NoError(tb, err)
				return input
			},
			Predicates: []predicate.Predicate{{}, warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(set.NewBits(0)).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{
					Message: WarpMessage{
						SourceChainID:       common.Hash(sourceChainID),
						OriginSenderAddress: sourceAddress,
						Payload:             packagedPayloadBytes,
					},
					Valid: true,
				})
				require.NoError(tb, err)
				return res
			}(),
		},
		{
			Name:   "get_message_failure_non_zero_index",
			Caller: callerAddr,
			InputFn: func(tb testing.TB) []byte {
				input, err := PackGetVerifiedWarpMessage(1)
				require.NoError(tb, err)
				return input
			},
			Predicates: []predicate.Predicate{{}, warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(set.NewBits(0, 1)).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{Valid: false})
				require.NoError(tb, err)
				return res
			}(),
		},
		{
			Name:    "get_non_existent_message",
			Caller:  callerAddr,
			InputFn: func(testing.TB) []byte { return getVerifiedWarpMsg },
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{Valid: false})
				require.NoError(tb, err)
				return res
			}(),
		},
		{
			Name:       "get_message_success_readOnly",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{
					Message: WarpMessage{
						SourceChainID:       common.Hash(sourceChainID),
						OriginSenderAddress: sourceAddress,
						Payload:             packagedPayloadBytes,
					},
					Valid: true,
				})
				require.NoError(tb, err)
				return res
			}(),
		},
		{
			Name:    "get_non_existent_message_readOnly",
			Caller:  callerAddr,
			InputFn: func(testing.TB) []byte { return getVerifiedWarpMsg },
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{Valid: false})
				require.NoError(tb, err)
				return res
			}(),
		},
		{
			Name:        "get_message_out_of_gas_for_base_cost",
			Caller:      callerAddr,
			InputFn:     func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates:  []predicate.Predicate{warpMessagePredicate},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		{
			Name:       "get_message_out_of_gas",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)) - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		{
			Name:       "get_message_invalid_predicate_packing",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{invalidPackedPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(invalidPackedPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidPredicateBytes.Error(),
		},
		{
			Name:       "get_message_invalid_warp_message",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{invalidWarpMsgPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(invalidWarpMsgPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidWarpMsg.Error(),
		},
		{
			Name:       "get_message_invalid_addressed_payload",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{invalidAddressedPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(invalidAddressedPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidAddressedPayload.Error(),
		},
		{
			Name:   "get_message_index_invalid_uint32",
			Caller: callerAddr,
			InputFn: func(testing.TB) []byte {
				return append(WarpABI.Methods["getVerifiedWarpMessage"].ID, new(big.Int).SetInt64(math.MaxInt64).Bytes()...)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedErr: errInvalidIndexInput.Error(),
		},
		{
			Name:   "get_message_index_invalid_int32",
			Caller: callerAddr,
			InputFn: func(tb testing.TB) []byte {
				res, err := PackGetVerifiedWarpMessage(math.MaxInt32 + 1)
				require.NoError(tb, err)
				return res
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedErr: errInvalidIndexInput.Error(),
		},
		{
			Name:   "get_message_index_invalid_input_bytes",
			Caller: callerAddr,
			InputFn: func(tb testing.TB) []byte {
				res, err := PackGetVerifiedWarpMessage(1)
				require.NoError(tb, err)
				return res[:len(res)-2]
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedErr: errInvalidIndexInput.Error(),
		},
		{
			Name:       "get_message_success_granite",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + graniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{
					Message: WarpMessage{
						SourceChainID:       common.Hash(sourceChainID),
						OriginSenderAddress: sourceAddress,
						Payload:             packagedPayloadBytes,
					},
					Valid: true,
				})
				require.NoError(tb, err)
				return res
			}(),
		},
		{
			Name:   "get_message_success_non_zero_index_granite",
			Caller: callerAddr,
			InputFn: func(tb testing.TB) []byte {
				input, err := PackGetVerifiedWarpMessage(1)
				require.NoError(tb, err)
				return input
			},
			Predicates: []predicate.Predicate{{}, warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(set.NewBits(0)).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + graniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{
					Message: WarpMessage{
						SourceChainID:       common.Hash(sourceChainID),
						OriginSenderAddress: sourceAddress,
						Payload:             packagedPayloadBytes,
					},
					Valid: true,
				})
				require.NoError(tb, err)
				return res
			}(),
		},
		{
			Name:       "get_message_success_readOnly_granite",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + graniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{
					Message: WarpMessage{
						SourceChainID:       common.Hash(sourceChainID),
						OriginSenderAddress: sourceAddress,
						Payload:             packagedPayloadBytes,
					},
					Valid: true,
				})
				require.NoError(tb, err)
				return res
			}(),
		},
		{
			Name:       "get_message_out_of_gas_granite",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + graniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)) - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		{
			Name:       "get_message_invalid_predicate_packing_granite",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{invalidPackedPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + graniteGasConfig.PerWarpMessageChunk*uint64(len(invalidPackedPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidPredicateBytes.Error(),
		},
		{
			Name:       "get_message_invalid_warp_message_granite",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{invalidWarpMsgPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + graniteGasConfig.PerWarpMessageChunk*uint64(len(invalidWarpMsgPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidWarpMsg.Error(),
		},
		{
			Name:       "get_message_invalid_addressed_payload_granite",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{invalidAddressedPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + graniteGasConfig.PerWarpMessageChunk*uint64(len(invalidAddressedPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidAddressedPayload.Error(),
		},
	}
}

func TestGetVerifiedWarpMessage(t *testing.T) {
	tests := getVerifiedWarpMessageTests(t)
	precompiletest.RunPrecompileTests(t, Module, tests)
}

func BenchmarkGetVerifiedWarpMessage(b *testing.B) {
	tests := getVerifiedWarpMessageTests(b)
	precompiletest.RunPrecompileBenchmarks(b, Module, tests)
}

func getVerifiedWarpBlockHashTests(tb testing.TB) []precompiletest.PrecompileTest {
	networkID := uint32(54321)
	callerAddr := common.HexToAddress("0x0123")
	sourceChainID := ids.GenerateTestID()
	blockHash := ids.GenerateTestID()
	blockHashPayload, err := payload.NewHash(blockHash)
	require.NoError(tb, err)
	unsignedWarpMsg, err := avalancheWarp.NewUnsignedMessage(networkID, sourceChainID, blockHashPayload.Bytes())
	require.NoError(tb, err)
	warpMessage, err := avalancheWarp.NewMessage(unsignedWarpMsg, &avalancheWarp.BitSetSignature{}) // Create message with empty signature for testing
	require.NoError(tb, err)
	warpMessagePredicate := predicate.New(warpMessage.Bytes())
	getVerifiedWarpBlockHash, err := PackGetVerifiedWarpBlockHash(0)
	require.NoError(tb, err)

	// Invalid warp message predicate
	invalidWarpMsgPredicate := predicate.New([]byte{1, 2, 3})

	// Invalid block hash payload predicate
	invalidHashUnsigned, err := avalancheWarp.NewUnsignedMessage(networkID, sourceChainID, []byte{1, 2, 3})
	require.NoError(tb, err)
	invalidHashWarpMsg, err := avalancheWarp.NewMessage(invalidHashUnsigned, &avalancheWarp.BitSetSignature{})
	require.NoError(tb, err)
	invalidHashPredicate := predicate.New(invalidHashWarpMsg.Bytes())

	// Invalid predicate packing by corrupting a valid predicate
	invalidPackedPredicate := predicate.Predicate{{}}

	noFailures := set.NewBits()
	require.Empty(tb, noFailures.Bytes())

	_ = preGraniteGasConfig
	_ = invalidWarpMsgPredicate
	_ = invalidHashPredicate
	_ = invalidPackedPredicate

	return []precompiletest.PrecompileTest{
		{
			Name:       "get_message_success",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{
					WarpBlockHash: WarpBlockHash{
						SourceChainID: common.Hash(sourceChainID),
						BlockHash:     common.Hash(blockHash),
					},
					Valid: true,
				})
				require.NoError(tb, err)
				return res
			}(),
		},
		{
			Name:   "get_message_out_of_bounds_non_zero_index",
			Caller: callerAddr,
			InputFn: func(tb testing.TB) []byte {
				input, err := PackGetVerifiedWarpBlockHash(1)
				require.NoError(tb, err)
				return input
			},
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{Valid: false})
				require.NoError(tb, err)
				return res
			}(),
		},
		{
			Name:   "get_message_success_non_zero_index",
			Caller: callerAddr,
			InputFn: func(tb testing.TB) []byte {
				input, err := PackGetVerifiedWarpBlockHash(1)
				require.NoError(tb, err)
				return input
			},
			Predicates: []predicate.Predicate{{}, warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(set.NewBits(0)).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{
					WarpBlockHash: WarpBlockHash{
						SourceChainID: common.Hash(sourceChainID),
						BlockHash:     common.Hash(blockHash),
					},
					Valid: true,
				})
				require.NoError(tb, err)
				return res
			}(),
		},
		{
			Name:   "get_message_failure_non_zero_index",
			Caller: callerAddr,
			InputFn: func(tb testing.TB) []byte {
				input, err := PackGetVerifiedWarpBlockHash(1)
				require.NoError(tb, err)
				return input
			},
			Predicates: []predicate.Predicate{{}, warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(set.NewBits(0, 1)).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{Valid: false})
				require.NoError(tb, err)
				return res
			}(),
		},
		{
			Name:    "get_non_existent_message",
			Caller:  callerAddr,
			InputFn: func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{Valid: false})
				require.NoError(tb, err)
				return res
			}(),
		},
		{
			Name:       "get_message_success_readOnly",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{
					WarpBlockHash: WarpBlockHash{
						SourceChainID: common.Hash(sourceChainID),
						BlockHash:     common.Hash(blockHash),
					},
					Valid: true,
				})
				require.NoError(tb, err)
				return res
			}(),
		},
		{
			Name:    "get_non_existent_message_readOnly",
			Caller:  callerAddr,
			InputFn: func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{Valid: false})
				require.NoError(tb, err)
				return res
			}(),
		},
		{
			Name:        "get_message_out_of_gas_for_base_cost",
			Caller:      callerAddr,
			InputFn:     func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates:  []predicate.Predicate{warpMessagePredicate},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		{
			Name:       "get_message_out_of_gas",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)) - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		{
			Name:       "get_message_invalid_predicate_packing",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{invalidPackedPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(invalidPackedPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidPredicateBytes.Error(),
		},
		{
			Name:       "get_message_invalid_warp_message",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{invalidWarpMsgPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(invalidWarpMsgPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidWarpMsg.Error(),
		},
		{
			Name:       "get_message_invalid_block_hash_payload",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{invalidHashPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(invalidHashPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidBlockHashPayload.Error(),
		},
		{
			Name:   "get_message_index_invalid_uint32",
			Caller: callerAddr,
			InputFn: func(testing.TB) []byte {
				return append(WarpABI.Methods["getVerifiedWarpBlockHash"].ID, new(big.Int).SetInt64(math.MaxInt64).Bytes()...)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedErr: errInvalidIndexInput.Error(),
		},
		{
			Name:   "get_message_index_invalid_int32",
			Caller: callerAddr,
			InputFn: func(tb testing.TB) []byte {
				res, err := PackGetVerifiedWarpBlockHash(math.MaxInt32 + 1)
				require.NoError(tb, err)
				return res
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedErr: errInvalidIndexInput.Error(),
		},
		{
			Name:   "get_message_invalid_index_input_bytes",
			Caller: callerAddr,
			InputFn: func(tb testing.TB) []byte {
				res, err := PackGetVerifiedWarpBlockHash(1)
				require.NoError(tb, err)
				return res[:len(res)-2]
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedErr: errInvalidIndexInput.Error(),
		},
		{
			Name:       "get_message_success_granite",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + graniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{
					WarpBlockHash: WarpBlockHash{
						SourceChainID: common.Hash(sourceChainID),
						BlockHash:     common.Hash(blockHash),
					},
					Valid: true,
				})
				require.NoError(tb, err)
				return res
			}(),
		},
		{
			Name:   "get_message_success_non_zero_index_granite",
			Caller: callerAddr,
			InputFn: func(tb testing.TB) []byte {
				input, err := PackGetVerifiedWarpBlockHash(1)
				require.NoError(tb, err)
				return input
			},
			Predicates: []predicate.Predicate{{}, warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(set.NewBits(0)).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + graniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{
					WarpBlockHash: WarpBlockHash{
						SourceChainID: common.Hash(sourceChainID),
						BlockHash:     common.Hash(blockHash),
					},
					Valid: true,
				})
				require.NoError(tb, err)
				return res
			}(),
		},
		{
			Name:       "get_message_success_readOnly_granite",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + graniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{
					WarpBlockHash: WarpBlockHash{
						SourceChainID: common.Hash(sourceChainID),
						BlockHash:     common.Hash(blockHash),
					},
					Valid: true,
				})
				require.NoError(tb, err)
				return res
			}(),
		},
		{
			Name:       "get_message_out_of_gas_granite",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + graniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)) - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		{
			Name:       "get_message_invalid_predicate_packing_granite",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{invalidPackedPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + graniteGasConfig.PerWarpMessageChunk*uint64(len(invalidPackedPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidPredicateBytes.Error(),
		},
		{
			Name:       "get_message_invalid_warp_message_granite",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{invalidWarpMsgPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + graniteGasConfig.PerWarpMessageChunk*uint64(len(invalidWarpMsgPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidWarpMsg.Error(),
		},
		{
			Name:       "get_message_invalid_block_hash_payload_granite",
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{invalidHashPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures).AnyTimes()
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + graniteGasConfig.PerWarpMessageChunk*uint64(len(invalidHashPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidBlockHashPayload.Error(),
		},
	}
}

func TestGetVerifiedWarpBlockHash(t *testing.T) {
	tests := getVerifiedWarpBlockHashTests(t)
	precompiletest.RunPrecompileTests(t, Module, tests)
}

func BenchmarkGetVerifiedWarpBlockHash(b *testing.B) {
	tests := getVerifiedWarpBlockHashTests(b)
	precompiletest.RunPrecompileBenchmarks(b, Module, tests)
}

func TestPackEvents(t *testing.T) {
	sourceChainID := ids.GenerateTestID()
	sourceAddress := common.HexToAddress("0x0123")
	payloadData := []byte("mcsorley")
	networkID := uint32(54321)

	addressedPayload, err := payload.NewAddressedCall(
		sourceAddress.Bytes(),
		payloadData,
	)
	require.NoError(t, err)

	unsignedWarpMessage, err := avalancheWarp.NewUnsignedMessage(
		networkID,
		sourceChainID,
		addressedPayload.Bytes(),
	)
	require.NoError(t, err)

	topics, data, err := PackSendWarpMessageEvent(
		sourceAddress,
		common.Hash(unsignedWarpMessage.ID()),
		unsignedWarpMessage.Bytes(),
	)
	require.NoError(t, err)
	require.Equal(
		t,
		[]common.Hash{
			WarpABI.Events["SendWarpMessage"].ID,
			common.BytesToHash(sourceAddress[:]),
			common.Hash(unsignedWarpMessage.ID()),
		},
		topics,
	)

	unpacked, err := UnpackSendWarpEventDataToMessage(data)
	require.NoError(t, err)
	require.Equal(t, unsignedWarpMessage.Bytes(), unpacked.Bytes())
}
