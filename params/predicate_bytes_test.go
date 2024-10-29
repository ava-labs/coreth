// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"testing"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/stretchr/testify/require"
)

func TestPredicateResultsBytes(t *testing.T) {
	require := require.New(t)
	dataTooShort := utils.RandomBytes(DynamicFeeExtraDataSize - 1)
	resultBytes := GetPredicateResultBytes(dataTooShort)
	require.Empty(resultBytes)

	preDurangoData := utils.RandomBytes(DynamicFeeExtraDataSize)
	resultBytes = GetPredicateResultBytes(preDurangoData)
	require.Empty(resultBytes)
	postDurangoData := utils.RandomBytes(DynamicFeeExtraDataSize + 2)
	resultBytes = GetPredicateResultBytes(postDurangoData)
	require.Equal(resultBytes, postDurangoData[DynamicFeeExtraDataSize:])
}
