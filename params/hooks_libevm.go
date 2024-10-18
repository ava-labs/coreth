// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"math/big"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/coreth/constants"
	"github.com/ava-labs/coreth/nativeasset"
	"github.com/ava-labs/coreth/precompile/contract"
	"github.com/ava-labs/coreth/precompile/modules"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ava-labs/coreth/vmerrs"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/libevm"
	"github.com/holiman/uint256"
	"golang.org/x/exp/maps"
)

func (r RulesExtra) CanCreateContract(ac *libevm.AddressContext, gas uint64, state libevm.StateReader) (uint64, error) {
	// IsProhibited
	if ac.Self == constants.BlackholeAddr || modules.ReservedAddress(ac.Self) {
		return gas, vmerrs.ErrAddrProhibited
	}

	return gas, nil
}

func (r RulesExtra) CanExecuteTransaction(_ common.Address, _ *common.Address, _ libevm.StateReader) error {
	return nil
}

var PrecompiledContractsApricotPhase2 = map[common.Address]contract.StatefulPrecompiledContract{
	nativeasset.GenesisContractAddr:    &nativeasset.DeprecatedContract{},
	nativeasset.NativeAssetBalanceAddr: &nativeasset.NativeAssetBalance{GasCost: AssetBalanceApricot},
	nativeasset.NativeAssetCallAddr:    &nativeasset.NativeAssetCall{GasCost: AssetCallApricot},
}

var PrecompiledContractsApricotPhasePre6 = map[common.Address]contract.StatefulPrecompiledContract{
	nativeasset.GenesisContractAddr:    &nativeasset.DeprecatedContract{},
	nativeasset.NativeAssetBalanceAddr: &nativeasset.DeprecatedContract{},
	nativeasset.NativeAssetCallAddr:    &nativeasset.DeprecatedContract{},
}

var PrecompiledContractsApricotPhase6 = map[common.Address]contract.StatefulPrecompiledContract{
	nativeasset.GenesisContractAddr:    &nativeasset.DeprecatedContract{},
	nativeasset.NativeAssetBalanceAddr: &nativeasset.NativeAssetBalance{GasCost: AssetBalanceApricot},
	nativeasset.NativeAssetCallAddr:    &nativeasset.NativeAssetCall{GasCost: AssetCallApricot},
}

var PrecompiledContractsBanff = map[common.Address]contract.StatefulPrecompiledContract{
	nativeasset.GenesisContractAddr:    &nativeasset.DeprecatedContract{},
	nativeasset.NativeAssetBalanceAddr: &nativeasset.DeprecatedContract{},
	nativeasset.NativeAssetCallAddr:    &nativeasset.DeprecatedContract{},
}

func (r RulesExtra) ActivePrecompiles(existing []common.Address) []common.Address {
	var precompiles map[common.Address]contract.StatefulPrecompiledContract
	switch {
	case r.IsBanff:
		precompiles = PrecompiledContractsBanff
	case r.IsApricotPhase6:
		precompiles = PrecompiledContractsApricotPhase6
	case r.IsApricotPhasePre6:
		precompiles = PrecompiledContractsApricotPhasePre6
	case r.IsApricotPhase2:
		precompiles = PrecompiledContractsApricotPhase2
	}

	var addresses []common.Address
	addresses = append(addresses, maps.Keys(precompiles)...)
	addresses = append(addresses, existing...)
	return addresses
}

// precompileOverrideBuiltin specifies precompiles that were activated prior to the
// dynamic precompile activation registry.
// These were only active historically and are not active in the current network.
func (r RulesExtra) precompileOverrideBuiltin(addr common.Address) (libevm.PrecompiledContract, bool) {
	var precompiles map[common.Address]contract.StatefulPrecompiledContract
	switch {
	case r.IsBanff:
		precompiles = PrecompiledContractsBanff
	case r.IsApricotPhase6:
		precompiles = PrecompiledContractsApricotPhase6
	case r.IsApricotPhasePre6:
		precompiles = PrecompiledContractsApricotPhasePre6
	case r.IsApricotPhase2:
		precompiles = PrecompiledContractsApricotPhase2
	}

	precompile, ok := precompiles[addr]
	if !ok {
		return nil, false
	}

	return makePrecompile(precompile), true
}

func makePrecompile(contract contract.StatefulPrecompiledContract) libevm.PrecompiledContract {
	run := func(env vm.PrecompileEnvironment, input []byte, suppliedGas uint64) ([]byte, uint64, error) {
		header, err := env.BlockHeader()
		if err != nil {
			panic(err) // Should never happen
		}
		var predicateResultsBytes []byte
		if len(header.Extra) >= DynamicFeeExtraDataSize {
			predicateResultsBytes = header.Extra[DynamicFeeExtraDataSize:]
		}
		accessableState := accessableState{
			env: env,
			blockContext: &BlockContext{
				number:                env.BlockNumber(),
				time:                  env.BlockTime(),
				predicateResultsBytes: predicateResultsBytes,
			},
		}
		return contract.Run(accessableState, env.Addresses().Caller, env.Addresses().Self, input, suppliedGas, env.ReadOnly())
	}
	return vm.NewStatefulPrecompile(run)
}

func (r RulesExtra) PrecompileOverride(addr common.Address) (libevm.PrecompiledContract, bool) {
	if p, ok := r.precompileOverrideBuiltin(addr); ok {
		return p, true
	}
	if _, ok := r.Precompiles[addr]; !ok {
		return nil, false
	}
	module, ok := modules.GetPrecompileModuleByAddress(addr)
	if !ok {
		return nil, false
	}

	return makePrecompile(module.Contract), true
}

type accessableState struct {
	env          vm.PrecompileEnvironment
	blockContext *BlockContext
}

func (a accessableState) GetStateDB() contract.StateDB {
	// XXX: this should be moved to the precompiles
	var state libevm.StateReader
	if a.env.ReadOnly() {
		state = a.env.ReadOnlyState()
	} else {
		state = a.env.StateDB()
	}
	return state.(contract.StateDB)
}

func (a accessableState) GetBlockContext() contract.BlockContext {
	return a.blockContext
}

func (a accessableState) GetChainConfig() precompileconfig.ChainConfig {
	return GetExtra(a.env.ChainConfig())
}

func (a accessableState) GetSnowContext() *snow.Context {
	return GetExtra(a.env.ChainConfig()).SnowCtx
}

func (a accessableState) NativeAssetCall(caller common.Address, input []byte, suppliedGas uint64, gasCost uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if suppliedGas < gasCost {
		return nil, 0, vmerrs.ErrOutOfGas
	}
	remainingGas = suppliedGas - gasCost

	if readOnly {
		return nil, remainingGas, vmerrs.ErrExecutionReverted
	}

	to, assetID, assetAmount, callData, err := nativeasset.UnpackNativeAssetCallInput(input)
	if err != nil {
		return nil, remainingGas, vmerrs.ErrExecutionReverted
	}

	stateDB := a.GetStateDB()
	// Note: it is not possible for a negative assetAmount to be passed in here due to the fact that decoding a
	// byte slice into a *big.Int type will always return a positive value.
	if assetAmount.Sign() != 0 && stateDB.GetBalanceMultiCoin(caller, assetID).Cmp(assetAmount) < 0 {
		return nil, remainingGas, vmerrs.ErrInsufficientBalance
	}

	snapshot := stateDB.Snapshot()

	if !stateDB.Exist(to) {
		if remainingGas < CallNewAccountGas {
			return nil, 0, vmerrs.ErrOutOfGas
		}
		remainingGas -= CallNewAccountGas
		stateDB.CreateAccount(to)
	}

	// Send [assetAmount] of [assetID] to [to] address
	stateDB.SubBalanceMultiCoin(caller, assetID, assetAmount)
	stateDB.AddBalanceMultiCoin(to, assetID, assetAmount)

	ret, remainingGas, err = a.env.Call(to, callData, remainingGas, new(uint256.Int), vm.WithUNSAFECallerAddressProxying())

	// When an error was returned by the EVM or when setting the creation code
	// above we revert to the snapshot and consume any gas remaining. Additionally
	// when we're in homestead this also counts for code storage gas errors.
	if err != nil {
		stateDB.RevertToSnapshot(snapshot)
		if err != vmerrs.ErrExecutionReverted {
			remainingGas = 0
		}
		// TODO: consider clearing up unused snapshots:
		//} else {
		//	evm.StateDB.DiscardSnapshot(snapshot)
	}
	return ret, remainingGas, err
}

type BlockContext struct {
	number                *big.Int
	time                  uint64
	predicateResultsBytes []byte
}

func NewBlockContext(number *big.Int, time uint64, predicateResultsBytes []byte) *BlockContext {
	return &BlockContext{
		number:                number,
		time:                  time,
		predicateResultsBytes: predicateResultsBytes,
	}
}

func (b *BlockContext) Number() *big.Int {
	return b.number
}

func (b *BlockContext) Timestamp() uint64 {
	return b.time
}

func (b *BlockContext) GetPredicateResultsBytes() []byte {
	return b.predicateResultsBytes
}
