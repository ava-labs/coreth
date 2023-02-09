// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/log"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/ava-labs/coreth/plugin/evm/message"
)

func (n *network) RequestCrossChain(chainID ids.ID, msg []byte, handler message.ResponseHandler) error {
	if handler != nil {
		return fmt.Errorf("ResponseHandler not yet supported")
	}
	log.Debug("sending request to chain", "chainID", chainID, "requestLen", len(msg))
	return n.appSender.SendCrossChainAppRequest(context.TODO(), chainID, 0, msg)
}
