// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

// GetPredicateResultBytes returns the predicate result bytes from extraData. If
// extraData is too short to include predicate results, it returns nil.
func GetPredicateResultBytes(extraData []byte) []byte {
	// Prior to Durango, the VM enforces the extra data is smaller than or equal
	// to this size.
	if len(extraData) <= DynamicFeeExtraDataSize {
		return nil
	}
	// After Durango, the extra data past the dynamic fee rollup window represents
	// predicate results.
	return extraData[DynamicFeeExtraDataSize:]
}

// SetPredicateResultBytes sets the predicate results in the extraData in the
// block header. This is used to set the predicate results in a block header
// without modifying the initial portion of the extra data (dynamic fee window
// rollup).
func SetPredicateResultBytes(extraData []byte, predicateResults []byte) []byte {
	return append(extraData[:DynamicFeeExtraDataSize], predicateResults...)
}
