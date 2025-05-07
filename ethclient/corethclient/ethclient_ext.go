// (c) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package corethclient

import (
	"encoding/json"
	"fmt"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/coreth/ethclient"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
)

var _ ethclient.BlockHook = (*extBlockHook)(nil)

// extBlockHook is a hook that is called when a block is decoded.
type extBlockHook struct{}

// extRpcBlock is the structure of the block extra data in the RPC response.
// It contains the version and the block extra data.
type extRpcBlock struct {
	Version        uint32         `json:"version"`
	BlockExtraData *hexutil.Bytes `json:"blockExtraData"`
}

// OnBlockDecoded is called when a block is decoded. It unmarshals the
// extra data from the RPC response and sets it in the block with libevm extras.
func (h *extBlockHook) OnBlockDecoded(raw json.RawMessage, block *types.Block) error {
	var body extRpcBlock
	if err := json.Unmarshal(raw, &body); err != nil {
		return fmt.Errorf("failed to unmarshal block extra data: %w", err)
	}
	extra := &customtypes.BlockBodyExtra{
		Version: body.Version,
		ExtData: (*[]byte)(body.BlockExtraData),
	}
	customtypes.SetBlockExtra(block, extra)
	return nil
}
