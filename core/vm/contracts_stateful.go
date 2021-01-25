package vm

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/holiman/uint256"
)

// PrecompiledContractsApricot contains the default set of pre-compiled Ethereum
// contracts used in the Istanbul release and the stateful precompiled contracts
// added for the Avalanche Apricot release.
// Apricot is incompatible with the YoloV1 Release since it does not include the
// BLS12-381 Curve Operations added to the set of precompiled contracts

var (
	genesisMulticoinContractAddr = common.HexToAddress("0x0100000000000000000000000000000000000000")
	nativeAssetBalanceAddr       = common.HexToAddress("0x0100000000000000000000000000000000000001")
	nativeAssetCallAddr          = common.HexToAddress("0x0100000000000000000000000000000000000002")
)

var PrecompiledContractsApricot = map[common.Address]StatefulPrecompiledContract{
	common.BytesToAddress([]byte{1}): newWrappedPrecompiledContract(&ecrecover{}),
	common.BytesToAddress([]byte{2}): newWrappedPrecompiledContract(&sha256hash{}),
	common.BytesToAddress([]byte{3}): newWrappedPrecompiledContract(&ripemd160hash{}),
	common.BytesToAddress([]byte{4}): newWrappedPrecompiledContract(&dataCopy{}),
	common.BytesToAddress([]byte{5}): newWrappedPrecompiledContract(&bigModExp{}),
	common.BytesToAddress([]byte{6}): newWrappedPrecompiledContract(&bn256AddIstanbul{}),
	common.BytesToAddress([]byte{7}): newWrappedPrecompiledContract(&bn256ScalarMulIstanbul{}),
	common.BytesToAddress([]byte{8}): newWrappedPrecompiledContract(&bn256PairingIstanbul{}),
	common.BytesToAddress([]byte{9}): newWrappedPrecompiledContract(&blake2F{}),
	genesisMulticoinContractAddr:     &genesisContract{},
	nativeAssetBalanceAddr:           &nativeAssetBalance{gasCost: params.AssetBalanceApricot},
	nativeAssetCallAddr:              &nativeAssetCall{gasCost: params.AssetCallApricot},
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

// Run implements the StatefulPrecompiledContract interface
func (w *wrappedPrecompiledContract) Run(evm *EVM, caller ContractRef, addr common.Address, value *big.Int, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	// [caller.Address()] has already been verified
	// as having a sufficient balance before the
	// precompiled contract runs.
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
		log.Error("cannot call native asset balance with non-zero value", "value", value)
		return nil, suppliedGas, ErrExecutionReverted
	}

	if suppliedGas < b.gasCost {
		return nil, 0, ErrOutOfGas
	}
	remainingGas = suppliedGas - b.gasCost

	if len(input) != 52 {
		log.Error("input to native asset balance must be 52 bytes, containing address [20 bytes] and assetID [32 bytes]", "input length", len(input))
		return nil, remainingGas, ErrExecutionReverted
	}
	address := common.BytesToAddress(input[:20])
	assetID := new(common.Hash)
	assetID.SetBytes(input[20:52])

	res, overflow := uint256.FromBig(evm.StateDB.GetBalanceMultiCoin(address, *assetID))
	log.Debug("native asset balance called", "address", address, "assetID", assetID.Hex(), "res", res, "overflow", overflow)
	if overflow {
		return nil, remainingGas, ErrExecutionReverted
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
		log.Error("cannot execute native asset call within read only call")
		return nil, remainingGas, ErrExecutionReverted
	}

	if len(input) < 84 {
		log.Error("input to native asset call must be >= 84 bytes, containing [20 bytes] to address, [32 bytes] assetID, [32 bytes] assetAmount, [n bytes] calData", "input", fmt.Sprintf("0x%x", input), "input length", len(input))
		return nil, remainingGas, ErrExecutionReverted
	}
	to := common.BytesToAddress(input[:20])
	assetID := new(common.Hash)
	assetID.SetBytes(input[20:52])
	assetAmount := new(big.Int).SetBytes(input[52:84])
	callData := input[84:]
	log.Debug("nativeAssetCall", "caller", caller.Address().Hex(), "to", to.Hex(), "assetID", assetID.Hex(), "assetAmount", assetAmount, "callData", fmt.Sprintf("0x%x", callData))

	mcerr := evm.Context.CanTransferMC(evm.StateDB, caller.Address(), to, assetID, assetAmount)
	if mcerr == 1 {
		log.Error("insufficient balance for mc transfer")
		return nil, remainingGas, ErrExecutionReverted
	} else if mcerr != 0 {
		log.Error("incompatible account for mc transfer")
		return nil, remainingGas, ErrExecutionReverted
	}

	snapshot := evm.StateDB.Snapshot()

	if !evm.StateDB.Exist(to) {
		log.Debug("Creating account", "address", to.Hex())
		if remainingGas < params.CallNewAccountGas {
			return nil, 0, ErrOutOfGas
		}
		remainingGas -= params.CallNewAccountGas
		evm.StateDB.CreateAccount(to)
	}

	// Send [value] to [to] address
	evm.Transfer(evm.StateDB, caller.Address(), to, value)
	evm.TransferMultiCoin(evm.StateDB, caller.Address(), to, assetID, assetAmount)
	ret, remainingGas, err = evm.Call(caller, to, callData, remainingGas, big.NewInt(0))
	log.Debug("native asset call result", "return", ret, "remainingGas", remainingGas, "err", err)

	// When an error was returned by the EVM or when setting the creation code
	// above we revert to the snapshot and consume any gas remaining. Additionally
	// when we're in homestead this also counts for code storage gas errors.
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		// If there is an error, it has already been logged,
		// so we convert it to [ErrExecutionReverted] here
		// to prevent consuming all of the gas. This is the
		// equivalent of adding a conditional revert.
		err = ErrExecutionReverted
		// if err != ErrExecutionReverted {
		// 	remainingGas = 0
		// }
		// TODO: consider clearing up unused snapshots:
		//} else {
		//	evm.StateDB.DiscardSnapshot(snapshot)
	}
	return ret, remainingGas, err
}

var (
	transferSignature   = EncodeSignatureHash("transfer(address,uint256,uint256,uint256)")
	getBalanceSignature = EncodeSignatureHash("getBalance(uint256)")
)

func EncodeSignatureHash(functionSignature string) []byte {
	hash := crypto.Keccak256([]byte(functionSignature))
	return hash[:4]
}

// genesisContract mimics the genesis contract Multicoin.sol in the original
// coreth release. It does this by mapping function identifiers to their new
// implementations via the native asset precompiled contracts.
type genesisContract struct{}

func (g *genesisContract) Run(evm *EVM, caller ContractRef, addr common.Address, value *big.Int, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if len(input) < 4 {
		log.Error("missing function signature")
		return nil, suppliedGas, ErrExecutionReverted
	}

	if value.Sign() != 0 {
		log.Error("cannot call genesis contract with non-zero value", "value", value)
		return nil, suppliedGas, ErrExecutionReverted
	}

	functionSignature := input[:4]
	switch {
	case bytes.Equal(functionSignature, transferSignature):
		if len(input) != 132 {
			log.Error("incorrect input length for transfer")
			return nil, suppliedGas, ErrExecutionReverted
		}

		// address / value1 / assetID / assetAmount
		args := input[4:]
		addressPaddedBytes := args[:32]
		addressBytes := common.TrimLeftZeroes(addressPaddedBytes)
		if len(addressBytes) != common.AddressLength {
			log.Error("address was padded incorrectly")
			return nil, suppliedGas, ErrExecutionReverted
		}
		callAssetArgs := make([]byte, 84)
		copy(callAssetArgs[:20], addressBytes)
		newValue := new(big.Int).SetBytes(args[32:64])
		copy(callAssetArgs[20:52], args[64:96])  // Copy the assetID bytes
		copy(callAssetArgs[52:84], args[96:128]) // Copy the assetAmount to be transferred
		return evm.Call(caller, nativeAssetCallAddr, callAssetArgs, suppliedGas, newValue)
	case bytes.Equal(functionSignature, getBalanceSignature):
		if len(input) != 36 {
			log.Error("incorrect input length for getBalance")
			return nil, suppliedGas, ErrExecutionReverted
		}
		balanceArgs := make([]byte, 52)
		copy(balanceArgs[:20], caller.Address().Bytes())
		copy(balanceArgs[20:52], input[4:36])
		return evm.Call(caller, nativeAssetBalanceAddr, balanceArgs, suppliedGas, value)
	default:
		log.Error("incorrect function signature", "functionSignature", fmt.Sprintf("0x%x", functionSignature))
		return nil, suppliedGas, ErrExecutionReverted
	}
}

type deprecatedContract struct {
	msg string
}

// Run implements StatefulPrecompiledContract
func (d *deprecatedContract) Run(evm *EVM, caller ContractRef, addr common.Address, value *big.Int, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	log.Error(d.msg)
	return nil, suppliedGas, ErrExecutionReverted
}
