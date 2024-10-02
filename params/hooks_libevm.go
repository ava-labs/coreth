// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"math/big"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/subnet-evm/constants"
	"github.com/ava-labs/subnet-evm/nativeasset"
	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ava-labs/subnet-evm/precompile/modules"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/subnet-evm/vmerrs"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/libevm"
	gethparams "github.com/ethereum/go-ethereum/params"
)

var PredicateParser = func(extra []byte) (PredicateResults, error) {
	return nil, nil
}

func (r RulesExtra) JumpTable() interface{} {
	// XXX: This does not account for the any possible differences in EIP-3529
	// Do not merge without verifying.
	return nil
}

func (r RulesExtra) CanCreateContract(ac *libevm.AddressContext, gas uint64, state libevm.StateReader) (uint64, error) {
	// IsProhibited
	if ac.Self == constants.BlackholeAddr || modules.ReservedAddress(ac.Self) {
		return gas, vmerrs.ErrAddrProhibited
	}

	return gas, nil
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
		predicateResults, err := PredicateParser(header.Extra)
		if err != nil {
			panic(err) // Should never happen, because predicates are parsed in NewEVMBlockContext.
		}
		// XXX: this should be moved to the precompiles
		var state libevm.StateReader
		if env.ReadOnly() {
			state = env.ReadOnlyState()
		} else {
			state = env.StateDB()
		}
		accessableState := accessableState{
			StateReader: state,
			env:         env,
			chainConfig: GetRulesExtra(env.Rules()).chainConfig,
			blockContext: &BlockContext{
				number:           env.BlockNumber(),
				time:             env.BlockTime(),
				predicateResults: predicateResults,
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
	if _, ok := r.ActivePrecompiles[addr]; !ok {
		return nil, false
	}
	module, ok := modules.GetPrecompileModuleByAddress(addr)
	if !ok {
		return nil, false
	}

	return makePrecompile(module.Contract), true
}

type accessableState struct {
	libevm.StateReader
	env          vm.PrecompileEnvironment
	chainConfig  *gethparams.ChainConfig
	blockContext *BlockContext
}

func (a accessableState) GetStateDB() contract.StateDB {
	// XXX: Whoa, this is a hack
	return a.StateReader.(contract.StateDB)
}

func (a accessableState) GetBlockContext() contract.BlockContext {
	return a.blockContext
}

func (a accessableState) GetChainConfig() precompileconfig.ChainConfig {
	extra := GetExtra(a.chainConfig)
	return extra
}

func (a accessableState) GetSnowContext() *snow.Context {
	return GetExtra(a.chainConfig).SnowCtx
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

	ret, remainingGas, err = a.env.Call(caller, to, callData, remainingGas)

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

type PredicateResults interface {
	GetPredicateResults(txHash common.Hash, address common.Address) []byte
}

type BlockContext struct {
	number           *big.Int
	time             uint64
	predicateResults PredicateResults
}

func NewBlockContext(number *big.Int, time uint64, predicateResults PredicateResults) *BlockContext {
	return &BlockContext{
		number:           number,
		time:             time,
		predicateResults: predicateResults,
	}
}

func (b *BlockContext) Number() *big.Int {
	return b.number
}

func (b *BlockContext) Timestamp() uint64 {
	return b.time
}

func (b *BlockContext) GetPredicateResults(txHash common.Hash, address common.Address) []byte {
	if b.predicateResults == nil {
		return nil
	}
	return b.predicateResults.GetPredicateResults(txHash, address)
}
