package vm

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/holiman/uint256"
)

// PrecompiledContractsApricot contains the default set of pre-compiled Ethereum
// contracts used in the Istanbul release and the stateful precompiled contracts
// added for the Avalanche Apricot release.
// Apricot is incompatible with the YoloV1 Release since it does not include the
// BLS12-381 Curve Operations added to the set of precompiled contracts
var PrecompiledContractsApricot = map[common.Address]StatefulPrecompiledContract{
	common.BytesToAddress([]byte{1}):                                  newWrappedPrecompiledContract(&ecrecover{}),
	common.BytesToAddress([]byte{2}):                                  newWrappedPrecompiledContract(&sha256hash{}),
	common.BytesToAddress([]byte{3}):                                  newWrappedPrecompiledContract(&ripemd160hash{}),
	common.BytesToAddress([]byte{4}):                                  newWrappedPrecompiledContract(&dataCopy{}),
	common.BytesToAddress([]byte{5}):                                  newWrappedPrecompiledContract(&bigModExp{}),
	common.BytesToAddress([]byte{6}):                                  newWrappedPrecompiledContract(&bn256AddIstanbul{}),
	common.BytesToAddress([]byte{7}):                                  newWrappedPrecompiledContract(&bn256ScalarMulIstanbul{}),
	common.BytesToAddress([]byte{8}):                                  newWrappedPrecompiledContract(&bn256PairingIstanbul{}),
	common.BytesToAddress([]byte{9}):                                  newWrappedPrecompiledContract(&blake2F{}),
	common.HexToAddress("0x0100000000000000000000000000000000000000"): &deprecatedContract{msg: "hardcoded genesis contract has been deprecated"},
	common.HexToAddress("0x0100000000000000000000000000000000000001"): &nativeAssetBalance{gasCost: params.AssetBalanceApricot},
	common.HexToAddress("0x0100000000000000000000000000000000000002"): &nativeAssetCall{gasCost: params.AssetCallApricot},
}

// StatefulPrecompiledContract is the interface for executing a precompiled contract
// This wraps the PrecompiledContracts native to Ethereum and allows adding in stateful
// precompiled contracts to support native Avalanche asset transfers.
type StatefulPrecompiledContract interface {
	// Run executes a precompiled contract in the current state
	// assumes that it has already been verified that [caller] can
	// transfer [value].
	Run(evm *EVM, caller ContractRef, addr common.Address, value *big.Int, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error)
}

// wrappedPrecompiledContract implements StatefulPrecompiledContract by wrapping stateless native precompiled contracts
// in Ethereum.
type wrappedPrecompiledContract struct {
	p PrecompiledContract
}

func newWrappedPrecompiledContract(p PrecompiledContract) StatefulPrecompiledContract {
	return &wrappedPrecompiledContract{p: p}
}

// Run ...
func (w *wrappedPrecompiledContract) Run(evm *EVM, caller ContractRef, addr common.Address, value *big.Int, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	evm.Transfer(evm.StateDB, caller.Address(), addr, value)
	return RunPrecompiledContract(w.p, input, suppliedGas)
}

// nativeAssetBalance is a precompiled contract used to retrieve the native asset balance
type nativeAssetBalance struct {
	gasCost uint64
}

// Run implements StatefulPrecompiledContract
func (b *nativeAssetBalance) Run(evm *EVM, caller ContractRef, addr common.Address, value *big.Int, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	// input: encodePacked(address 20 bytes, assetID 32 bytes)
	if value.Sign() != 0 {
		return nil, suppliedGas, errors.New("cannot transfer value to native asset balance pre-compiled contract")
	}

	if suppliedGas < b.gasCost {
		return nil, 0, ErrOutOfGas
	}
	remainingGas = suppliedGas - b.gasCost

	if len(input) != 52 {
		return nil, remainingGas, fmt.Errorf("input to native asset balance must be 52 bytes, containing address [20 bytes] and assetID [32 bytes], but found length: %d", len(input))
	}
	address := common.BytesToAddress(input[:20])
	assetID := new(common.Hash)
	assetID.SetBytes(input[20:52])

	res, overflow := uint256.FromBig(evm.StateDB.GetBalanceMultiCoin(address, *assetID))
	log.Info("nativeAssetBalance", "address", address, "assetID", assetID.Hex(), "res", res, "overflow", overflow)
	if overflow {
		return nil, remainingGas, errors.New("balance overflow")
	}
	return common.LeftPadBytes(res.Bytes(), 32), remainingGas, nil
}

// nativeAssetCall atomically transfers a native asset to a recipient address as well as calling that
// address
type nativeAssetCall struct {
	gasCost uint64
}

// Run implements StatefulPrecompiledContract
func (c *nativeAssetCall) Run(evm *EVM, caller ContractRef, addr common.Address, value *big.Int, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	// input: encodePacked(address 20 bytes, assetID 32 bytes, assetAmount 32 bytes, callData variable length bytes)
	if suppliedGas < c.gasCost {
		return nil, 0, ErrOutOfGas
	}
	remainingGas = suppliedGas - c.gasCost

	if readOnly {
		return nil, remainingGas, errors.New("cannot execute native asset transfer within read only call")
	}

	if len(input) < 84 {
		return nil, remainingGas, fmt.Errorf("missing inputs to native asset transfer, expected input of length at least 84, but found %d", len(input))
	}
	to := common.BytesToAddress(input[:20])
	assetID := new(common.Hash)
	assetID.SetBytes(input[20:52])
	assetAmount := new(big.Int).SetBytes(input[52:84])
	callData := input[84:]
	log.Info("nativeAssetCall", "input", fmt.Sprintf("0x%x", input), "to", to.Hex(), "assetID", assetID.Hex(), "assetAmount", assetAmount, "callData", fmt.Sprintf("0x%x", callData))

	mcerr := evm.Context.CanTransferMC(evm.StateDB, caller.Address(), to, assetID, assetAmount)
	if mcerr == 1 {
		log.Info("Insufficient balance for mc transfer")
		return nil, remainingGas, ErrInsufficientBalance
	} else if mcerr != 0 {
		log.Info("incompatible account for mc transfer")
		return nil, remainingGas, ErrIncompatibleAccount
	}

	snapshot := evm.StateDB.Snapshot()

	if !evm.StateDB.Exist(to) {
		log.Info("Creating account entry", "address", to.Hex())
		remainingGas -= params.CallNewAccountGas
		evm.StateDB.CreateAccount(to)
	}

	// Send [value] to [to] address
	evm.Transfer(evm.StateDB, caller.Address(), to, value)
	evm.TransferMultiCoin(evm.StateDB, caller.Address(), to, assetID, assetAmount)
	ret, remainingGas, err = evm.Call(caller, to, callData, remainingGas, big.NewInt(0))
	log.Info("Finished TransferMultiCoin and call", "return", ret, "remainingGas", remainingGas, "error", err)

	// When an error was returned by the EVM or when setting the creation code
	// above we revert to the snapshot and consume any gas remaining. Additionally
	// when we're in homestead this also counts for code storage gas errors.
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != ErrExecutionReverted {
			remainingGas = 0
		}
		// TODO: consider clearing up unused snapshots:
		//} else {
		//	evm.StateDB.DiscardSnapshot(snapshot)
	}
	return ret, remainingGas, err
}

type deprecatedContract struct {
	msg string
}

// Run ...
func (d *deprecatedContract) Run(evm *EVM, caller ContractRef, addr common.Address, value *big.Int, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	return nil, suppliedGas, errors.New(d.msg)
}
