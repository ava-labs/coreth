package atx

import "errors"

var (
	errEmptyBlock                     = errors.New("empty block")
	errUnsupportedFXs                 = errors.New("unsupported feature extensions")
	errInvalidBlock                   = errors.New("invalid block")
	errInvalidAddr                    = errors.New("invalid hex address")
	errInsufficientAtomicTxFee        = errors.New("atomic tx fee too low for atomic mempool")
	errAssetIDMismatch                = errors.New("asset IDs in the input don't match the utxo")
	errNoImportInputs                 = errors.New("tx has no imported inputs")
	errInputsNotSortedUnique          = errors.New("inputs not sorted and unique")
	errPublicKeySignatureMismatch     = errors.New("signature doesn't match public key")
	errWrongChainID                   = errors.New("tx has wrong chain ID")
	errInsufficientFunds              = errors.New("insufficient funds")
	errNoExportOutputs                = errors.New("tx has no export outputs")
	errOutputsNotSorted               = errors.New("tx outputs not sorted")
	errOutputsNotSortedUnique         = errors.New("outputs not sorted and unique")
	errOverflowExport                 = errors.New("overflow when computing export amount + txFee")
	errInvalidNonce                   = errors.New("invalid nonce")
	errConflictingAtomicInputs        = errors.New("invalid block due to conflicting atomic inputs")
	errUnclesUnsupported              = errors.New("uncles unsupported")
	errRejectedParent                 = errors.New("rejected parent")
	errInsufficientFundsForFee        = errors.New("insufficient AVAX funds to pay transaction fee")
	errNoEVMOutputs                   = errors.New("tx has no EVM outputs")
	errNilBaseFeeApricotPhase3        = errors.New("nil base fee is invalid after apricotPhase3")
	errNilExtDataGasUsedApricotPhase4 = errors.New("nil extDataGasUsed is invalid after apricotPhase4")
	errNilBlockGasCostApricotPhase4   = errors.New("nil blockGasCost is invalid after apricotPhase4")
	errConflictingAtomicTx            = errors.New("conflicting atomic tx present")
	errTooManyAtomicTx                = errors.New("too many atomic tx")
	errMissingAtomicTxs               = errors.New("cannot build a block with non-empty extra data and zero atomic transactions")
	errInvalidHeaderPredicateResults  = errors.New("invalid header predicate results")
	ErrConflictingAtomicInputs        = errConflictingAtomicInputs
	ErrInsufficientAtomicTxFee        = errInsufficientAtomicTxFee
	ErrTooManyAtomicTx                = errTooManyAtomicTx
	ErrConflictingAtomicTx            = errConflictingAtomicTx
	ErrMissingUTXOs                   = errors.New("missing UTXOs")
)
